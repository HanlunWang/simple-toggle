// Package store 持久化 app 的状态与运行记录，全部是普通 JSON 文件：
//
//	state.json   期望状态、隐藏列表、参数值、预置脚本投放记账、开关元数据、设置
//	runs.jsonl   运行记录，一行一条，按保留天数清理
//
// 数据量很小（几十个开关、几千条记录），JSON 比数据库更简单透明，也方便导入导出。
// 写入用「临时文件 + rename」保证原子性；只有引擎一个写入者（app 单实例）。
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	stateFile = "state.json"
	runsFile  = "runs.jsonl"
	// MaxRecentRuns 是内存里保留、供查询的最近记录条数。
	MaxRecentRuns = 2000
	// MaxRunOutput 是每条记录持久化的输出上限。
	MaxRunOutput = 8 << 10
	// DefaultRetention 是运行记录的默认保留时长。
	DefaultRetention = 30 * 24 * time.Hour
)

// Meta 是用户对单个开关的安排（不写进脚本）。
type Meta struct {
	Group    string `json:"group,omitempty"`
	Order    int    `json:"order,omitempty"`
	Shortcut string `json:"shortcut,omitempty"`
	Confirm  bool   `json:"confirm,omitempty"` // 运行前确认
	Notify   string `json:"notify,omitempty"`  // never | fail | always；空 = 跟随全局
	Timeout  int    `json:"timeout,omitempty"` // 秒；0 = 默认
}

type state struct {
	Version    int                          `json:"version"`
	Desired    map[string]bool              `json:"desired"`
	Hidden     map[string]bool              `json:"hidden"`
	Params     map[string]map[string]string `json:"params"`
	Seeded     map[string]bool              `json:"seeded"`
	Meta       map[string]Meta              `json:"meta"`
	Settings   map[string]string            `json:"settings"`
	Scenes     []Scene                      `json:"scenes,omitempty"`
	Rules      []Rule                       `json:"automations,omitempty"`
	V1Imported bool                         `json:"v1Imported,omitempty"`
}

// Run 是一条运行记录。
type Run struct {
	ID       int64     `json:"id"`
	Time     time.Time `json:"time"`
	Switch   string    `json:"switch"`
	Name     string    `json:"name"`
	Action   string    `json:"action"` // on | off | run | start | stop | exit | status
	Source   string    `json:"source"` // manual | shortcut | scene | automation | ai | cli | api | restore
	Detail   string    `json:"detail,omitempty"`
	Duration int64     `json:"durationMs"`
	ExitCode int       `json:"exitCode"`
	OK       bool      `json:"ok"`
	Output   string    `json:"output,omitempty"`
}

// Filter 用于查询运行记录。
type Filter struct {
	Switch     string
	Source     string
	FailedOnly bool
	Limit      int
}

type Store struct {
	mu        sync.Mutex
	dir       string
	st        state
	runs      []Run
	nextID    int64
	retention time.Duration
}

// Open 读取 dir 下的数据；目录或文件不存在时从空状态开始。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, retention: DefaultRetention}
	s.st = state{Version: 1}
	if data, err := os.ReadFile(filepath.Join(dir, stateFile)); err == nil {
		if err := json.Unmarshal(data, &s.st); err != nil {
			// 损坏的状态文件留档后重新开始，不让 app 起不来
			_ = os.Rename(filepath.Join(dir, stateFile), filepath.Join(dir, stateFile+".corrupt-"+time.Now().Format("20060102-150405")))
			s.st = state{Version: 1}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.ensureMaps()
	if err := s.loadRuns(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureMaps() {
	if s.st.Desired == nil {
		s.st.Desired = map[string]bool{}
	}
	if s.st.Hidden == nil {
		s.st.Hidden = map[string]bool{}
	}
	if s.st.Params == nil {
		s.st.Params = map[string]map[string]string{}
	}
	if s.st.Seeded == nil {
		s.st.Seeded = map[string]bool{}
	}
	if s.st.Meta == nil {
		s.st.Meta = map[string]Meta{}
	}
	if s.st.Settings == nil {
		s.st.Settings = map[string]string{}
	}
}

func (s *Store) Dir() string { return s.dir }

// saveLocked 原子写入 state.json；调用方持有锁。
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, stateFile), data, 0o644)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ───────── 状态 ─────────

func (s *Store) Desired(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Desired[id]
}

func (s *Store) DesiredAll() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.st.Desired))
	for k, v := range s.st.Desired {
		out[k] = v
	}
	return out
}

func (s *Store) SetDesired(id string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.st.Desired[id]; ok && v == on {
		return nil
	}
	s.st.Desired[id] = on
	return s.saveLocked()
}

func (s *Store) Hidden(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Hidden[id]
}

func (s *Store) SetHidden(id string, hidden bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Hidden[id] == hidden {
		return nil
	}
	if hidden {
		s.st.Hidden[id] = true
	} else {
		delete(s.st.Hidden, id)
	}
	return s.saveLocked()
}

// Param 返回已保存的参数值；没保存过时 ok=false（调用方用默认值）。
func (s *Store) Param(id, key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.st.Params[id][key]
	return v, ok
}

// SetParam 保存参数值；值没变时返回 changed=false。
func (s *Store) SetParam(id, key, value string) (changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.st.Params[id]
	if m == nil {
		m = map[string]string{}
		s.st.Params[id] = m
	}
	if old, ok := m[key]; ok && old == value {
		return false, nil
	}
	m[key] = value
	return true, s.saveLocked()
}

func (s *Store) Seeded(file string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Seeded[file]
}

func (s *Store) MarkSeeded(files ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range files {
		s.st.Seeded[f] = true
	}
	return s.saveLocked()
}

func (s *Store) Meta(id string) Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Meta[id]
}

func (s *Store) SetMeta(id string, m Meta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m == (Meta{}) {
		delete(s.st.Meta, id)
	} else {
		s.st.Meta[id] = m
	}
	return s.saveLocked()
}

func (s *Store) Setting(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Settings[key]
}

func (s *Store) SetSetting(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Settings[key] == value {
		return nil
	}
	s.st.Settings[key] = value
	return s.saveLocked()
}

// Forget 删除一个开关的全部记录（脚本被删除时）。
func (s *Store) Forget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.st.Desired, id)
	delete(s.st.Hidden, id)
	delete(s.st.Params, id)
	delete(s.st.Meta, id)
	return s.saveLocked()
}

// ───────── 运行记录 ─────────

func (s *Store) loadRuns() error {
	f, err := os.Open(filepath.Join(s.dir, runsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	cutoff := time.Now().Add(-s.retention)
	stale := false
	for sc.Scan() {
		var r Run
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			stale = true // 截断的半行（崩溃时）：下面重写时丢掉
			continue
		}
		if r.ID >= s.nextID {
			s.nextID = r.ID + 1 // 编号只增不减，被清理掉的记录也算
		}
		if r.Time.Before(cutoff) {
			stale = true
			continue
		}
		s.runs = append(s.runs, r)
	}
	if len(s.runs) > MaxRecentRuns {
		s.runs = s.runs[len(s.runs)-MaxRecentRuns:]
		stale = true
	}
	if stale {
		return s.rewriteRunsLocked()
	}
	return nil
}

func (s *Store) rewriteRunsLocked() error {
	var buf []byte
	for _, r := range s.runs {
		line, _ := json.Marshal(r)
		buf = append(append(buf, line...), '\n')
	}
	return writeAtomic(filepath.Join(s.dir, runsFile), buf, 0o644)
}

// AppendRun 记录一次执行并返回带编号的记录。
func (s *Store) AppendRun(r Run) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextID == 0 {
		s.nextID = 1
	}
	r.ID = s.nextID
	s.nextID++
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	if len(r.Output) > MaxRunOutput {
		r.Output = r.Output[len(r.Output)-MaxRunOutput:]
	}
	s.runs = append(s.runs, r)
	if len(s.runs) > MaxRecentRuns {
		s.runs = s.runs[len(s.runs)-MaxRecentRuns:]
	}
	line, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, runsFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return r, err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return r, err
}

// Runs 按时间倒序返回符合条件的记录。
func (s *Store) Runs(f Filter) []Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Run
	for i := len(s.runs) - 1; i >= 0; i-- {
		r := s.runs[i]
		if f.Switch != "" && r.Switch != f.Switch {
			continue
		}
		if f.Source != "" && r.Source != f.Source {
			continue
		}
		if f.FailedOnly && r.OK {
			continue
		}
		out = append(out, r)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out
}

// ClearRuns 删除全部运行记录。
func (s *Store) ClearRuns() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = nil
	return s.rewriteRunsLocked()
}

// SetRetention 修改保留时长并立即清理。
func (s *Store) SetRetention(d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retention = d
	cutoff := time.Now().Add(-d)
	i := sort.Search(len(s.runs), func(i int) bool { return !s.runs[i].Time.Before(cutoff) })
	if i == 0 {
		return nil
	}
	s.runs = append([]Run(nil), s.runs[i:]...)
	return s.rewriteRunsLocked()
}
