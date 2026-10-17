// Package engine 管理开关：扫描脚本目录、执行脚本、维护状态、监管常驻进程、
// 记录运行、发出事件。它不依赖任何界面框架，GUI、HTTP API、CLI 都通过它工作。
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/runner"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// State 是开关的运行状态。
type State string

const (
	On      State = "on"
	Off     State = "off"
	Error   State = "error"
	Unknown State = "unknown"
	Pending State = "pending" // 正在执行 on / off
	Idle    State = "idle"    // action：空闲
	Running State = "running" // action：执行中
)

// Source 是一次操作的来源，写进运行记录并显示在界面上。
type Source string

const (
	Manual     Source = "manual"
	Shortcut   Source = "shortcut"
	Scene      Source = "scene"
	Automation Source = "automation"
	AI         Source = "ai"
	CLI        Source = "cli"
	API        Source = "api"
	Restore    Source = "restore"
)

var (
	ErrNotFound  = errors.New("no such switch")
	ErrNotToggle = errors.New("switch is an action; run it instead")
	ErrNotAction = errors.New("switch is not an action")
	ErrBusy      = errors.New("switch is already running")
	ErrNoParam   = errors.New("switch has no such param")
)

// LintError 表示脚本没有通过契约检查。
type LintError struct{ Issues []contract.Issue }

func (e *LintError) Error() string { return "script has contract errors" }

// Config 配置引擎。
type Config struct {
	ScriptsDir     string
	LedgerPath     string // daemons.json
	Bin            string // 注入脚本的 OPENTOGGLE_BIN
	Store          *store.Store
	Trash          func(path string) error
	Shell          string        // 无 shebang 脚本用的 shell
	DefaultTimeout time.Duration // on / off / status / action 的超时，默认 30 秒
	WatchInterval  time.Duration // 脚本目录变化检测间隔；0 = 不检测（由界面层负责）
	PollVisible    time.Duration // 面板可见时的状态轮询间隔，默认 5 秒
	PollHidden     time.Duration // 后台轮询间隔，默认 30 秒
	ExtraEnv       func() []string
	NoRestore      bool // 启动时不恢复上次开着的开关
}

// Switch 是一个已解析的脚本。
type Switch struct {
	ID     string
	Path   string
	Header contract.Header
}

type rt struct {
	state       State
	activatedAt time.Time
	daemon      *runner.Proc
	op          sync.Mutex // 同一开关的操作串行执行
	lastRun     *store.Run
	busySince   time.Time // action 开始执行的时刻
	userAt      time.Time // 用户最近一次改动它的时刻（自动化的冲突策略用）
}

// Engine 是开关管理器。所有方法并发安全。
type Engine struct {
	cfg Config

	mu         sync.Mutex
	switches   map[string]*Switch
	rts        map[string]*rt
	generation uint64
	polling    bool
	visible    int
	dirSig     string

	subMu  sync.Mutex
	subs   map[int]chan Event
	nextID int

	wake   chan struct{}
	stopCh chan struct{}
	wg     sync.WaitGroup

	cfgMu sync.Mutex // 保护可在运行中修改的 DefaultTimeout 与 Shell

	sceneOp    sync.Mutex             // 场景串行执行
	sceneState map[string]*sceneState // 激活中的场景（激活前状态与目标状态）
}

func New(cfg Config) *Engine {
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.PollVisible == 0 {
		cfg.PollVisible = 5 * time.Second
	}
	if cfg.PollHidden == 0 {
		cfg.PollHidden = 30 * time.Second
	}
	if cfg.Trash == nil {
		cfg.Trash = os.Remove
	}
	return &Engine{
		cfg:      cfg,
		switches: map[string]*Switch{},
		rts:      map[string]*rt{},
		subs:     map[int]chan Event{},
		wake:     make(chan struct{}, 1),
		stopCh:   make(chan struct{}),

		sceneState: map[string]*sceneState{},
	}
}

func (e *Engine) ScriptsDir() string { return e.cfg.ScriptsDir }

// Start 扫描脚本、清理上次遗留的常驻进程、恢复期望状态，并开始轮询。
func (e *Engine) Start() error {
	if err := os.MkdirAll(e.cfg.ScriptsDir, 0o755); err != nil {
		return err
	}
	e.reload()
	e.sweepOrphans()
	if !e.cfg.NoRestore {
		e.restoreDesired()
	}
	e.wg.Add(1)
	go e.loop()
	return nil
}

// Shutdown 停止轮询并结束全部常驻进程。
func (e *Engine) Shutdown() {
	select {
	case <-e.stopCh:
		return
	default:
		close(e.stopCh)
	}
	e.wg.Wait()
	e.waitOps(5 * time.Second)
	e.mu.Lock()
	var procs []*runner.Proc
	for _, r := range e.rts {
		if r.daemon != nil {
			procs = append(procs, r.daemon)
		}
	}
	e.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p *runner.Proc) { defer wg.Done(); p.Stop() }(p)
	}
	wg.Wait()
	_ = os.Remove(e.cfg.LedgerPath)
}

// SetDefaults 修改默认超时与没有 shebang 时使用的 shell（设置里改动后立即生效）。
// timeout 为 0 时保持不变。
func (e *Engine) SetDefaults(timeout time.Duration, shell string) {
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()
	if timeout > 0 {
		e.cfg.DefaultTimeout = timeout
	}
	e.cfg.Shell = shell
}

func (e *Engine) defaults() (time.Duration, string) {
	e.cfgMu.Lock()
	defer e.cfgMu.Unlock()
	return e.cfg.DefaultTimeout, e.cfg.Shell
}

func (e *Engine) stepTimeout() time.Duration {
	t, _ := e.defaults()
	return t + 5*time.Second
}

// IsUser 报告来源是否代表用户的直接操作（自动化、恢复、场景内部的步骤不算）。
func IsUser(src Source) bool {
	switch src {
	case Automation, Restore, Scene, Source("system"), Source("poll"):
		return false
	}
	return true
}

func (e *Engine) markUser(r *rt, src Source) {
	if !IsUser(src) {
		return
	}
	e.mu.Lock()
	r.userAt = time.Now()
	e.mu.Unlock()
}

// UserChangedAt 返回用户最近一次改动这个开关的时刻：在 app 里操作，或在系统里直接改
// （轮询发现状态变了）。自动化据此避免刚被用户手动改过的开关又被规则改回去。
func (e *Engine) UserChangedAt(id string) time.Time {
	id, ok := e.Resolve(id)
	if !ok {
		return time.Time{}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if r := e.rts[id]; r != nil {
		return r.userAt
	}
	return time.Time{}
}

// waitOps 等待进行中的操作（on / off、按钮、改参数后的重跑）结束，最多等 limit：
// 否则它们会在退出之后继续写运行记录与状态。
func (e *Engine) waitOps(limit time.Duration) {
	e.mu.Lock()
	rts := make([]*rt, 0, len(e.rts))
	for _, r := range e.rts {
		rts = append(rts, r)
	}
	e.mu.Unlock()
	deadline := time.Now().Add(limit)
	for _, r := range rts {
		for !r.op.TryLock() {
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		r.op.Unlock()
	}
}

// ───────── 脚本目录 ─────────

// Reload 重新扫描脚本目录。
func (e *Engine) Reload() { e.reload(); e.kick() }

func (e *Engine) reload() {
	entries, _ := os.ReadDir(e.cfg.ScriptsDir)
	found := map[string]*Switch{}
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") || ent.IsDir() {
			continue
		}
		path := filepath.Join(e.cfg.ScriptsDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		h, ok := contract.Parse(string(data))
		if !ok {
			continue
		}
		found[name] = &Switch{ID: name, Path: path, Header: h}
	}
	e.mu.Lock()
	var gone []*runner.Proc
	for id, r := range e.rts {
		if _, ok := found[id]; !ok {
			if r.daemon != nil {
				gone = append(gone, r.daemon)
			}
			delete(e.rts, id)
		}
	}
	for id, sw := range found {
		if _, ok := e.rts[id]; !ok {
			st := Unknown
			if sw.Header.Type == contract.Action {
				st = Idle
			}
			e.rts[id] = &rt{state: st}
		}
	}
	e.switches = found
	e.dirSig = e.signature()
	e.mu.Unlock()
	for _, p := range gone {
		p.Stop()
	}
	e.emit(Event{Kind: EventReloaded})
}

// signature 用文件名、大小、修改时间描述目录现状，用来低成本地发现变化。
func (e *Engine) signature() string {
	entries, _ := os.ReadDir(e.cfg.ScriptsDir)
	var b strings.Builder
	for _, ent := range entries {
		if info, err := ent.Info(); err == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", ent.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// ───────── 查询 ─────────

// Resolve 把用户给的 id 规范化：允许省略 .sh 后缀（与 v1 一致）。
func (e *Engine) Resolve(id string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.switches[id]; ok {
		return id, true
	}
	if _, ok := e.switches[id+".sh"]; ok {
		return id + ".sh", true
	}
	return "", false
}

// List 返回全部开关，按名称排序（与 v1 一致）。
func (e *Engine) List() []View {
	e.mu.Lock()
	ids := make([]string, 0, len(e.switches))
	for id := range e.switches {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := e.switches[ids[i]].Header.Name, e.switches[ids[j]].Header.Name
		if a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	e.mu.Unlock()
	out := make([]View, 0, len(ids))
	for _, id := range ids {
		if v, ok := e.Get(id); ok {
			out = append(out, v)
		}
	}
	return out
}

// Get 返回一个开关的快照。
func (e *Engine) Get(id string) (View, bool) {
	id, ok := e.Resolve(id)
	if !ok {
		return View{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.viewLocked(id), true
}

func (e *Engine) lookup(id string) (*Switch, *rt, error) {
	id, ok := e.Resolve(id)
	if !ok {
		return nil, nil, ErrNotFound
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.switches[id], e.rts[id], nil
}

func (e *Engine) setState(id string, s State, activated time.Time) {
	e.mu.Lock()
	r := e.rts[id]
	if r == nil {
		e.mu.Unlock()
		return
	}
	changed := r.state != s
	r.state = s
	if s == On && r.activatedAt.IsZero() {
		r.activatedAt = activated
	}
	if s != On && s != Pending {
		r.activatedAt = time.Time{}
	}
	e.mu.Unlock()
	if changed {
		e.emitSwitch(EventState, id)
	}
}

// ───────── 环境 ─────────

func (e *Engine) paramValue(sw *Switch, p contract.Param) string {
	if v, ok := e.cfg.Store.Param(sw.ID, p.Key); ok {
		return v
	}
	return p.Default
}

func (e *Engine) env(sw *Switch, src Source) []string {
	env := os.Environ()
	env = withHomebrewPath(env)
	env = append(env, "OPENTOGGLE_BIN="+e.cfg.Bin, "OPENTOGGLE_TRIGGER="+string(src))
	if e.cfg.ExtraEnv != nil {
		env = append(env, e.cfg.ExtraEnv()...)
	}
	for _, p := range sw.Header.Params {
		env = append(env, p.EnvName()+"="+e.paramValue(sw, p))
	}
	return env
}

// withHomebrewPath 给 PATH 补上 Homebrew 目录：从 Finder 或开机启动时 PATH 只有
// /usr/bin:/bin:/usr/sbin:/sbin，脚本会找不到 brew 安装的命令。
func withHomebrewPath(env []string) []string {
	for i, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			continue
		}
		path := strings.TrimPrefix(kv, "PATH=")
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
			if !strings.Contains(":"+path+":", ":"+dir+":") {
				path += ":" + dir
			}
		}
		env[i] = "PATH=" + path
		return env
	}
	return append(env, "PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin")
}

func (e *Engine) spec(sw *Switch, src Source, args ...string) runner.Spec {
	timeout, shell := e.defaults()
	if m := e.cfg.Store.Meta(sw.ID); m.Timeout > 0 {
		timeout = time.Duration(m.Timeout) * time.Second
	}
	return runner.Spec{Path: sw.Path, Args: args, Env: e.env(sw, src), Shell: shell, Timeout: timeout}
}

func (e *Engine) record(sw *Switch, action string, src Source, detail string, res runner.Result) store.Run {
	r, _ := e.cfg.Store.AppendRun(store.Run{
		Switch: sw.ID, Name: sw.Header.Name, Action: action, Source: string(src), Detail: detail,
		Duration: res.Duration.Milliseconds(), ExitCode: res.ExitCode, OK: res.ExitCode == 0 && res.Err == nil,
		Output: res.Output,
	})
	e.mu.Lock()
	if x := e.rts[sw.ID]; x != nil {
		x.lastRun = &r
	}
	e.mu.Unlock()
	e.emit(Event{Kind: EventRun, Switch: sw.ID, Run: &r})
	return r
}

// ───────── 开 / 关 ─────────

// SetSwitch 打开或关闭开关，立即返回；用 Wait 等待结果。
func (e *Engine) SetSwitch(id string, on bool, src Source, detail string) (<-chan struct{}, error) {
	sw, r, err := e.lookup(id)
	if err != nil {
		return nil, err
	}
	if sw.Header.Type == contract.Action {
		return nil, ErrNotToggle
	}
	e.markUser(r, src)
	e.bump()
	_ = e.cfg.Store.SetDesired(sw.ID, on)
	done := make(chan struct{})
	switch sw.Header.Type {
	case contract.Daemon:
		if on {
			go func() { defer close(done); r.op.Lock(); defer r.op.Unlock(); e.spawn(sw, r, src, detail) }()
		} else {
			go func() { defer close(done); r.op.Lock(); defer r.op.Unlock(); e.stopDaemon(sw, r, src, detail) }()
		}
	default:
		e.setState(sw.ID, Pending, time.Time{})
		go func() {
			defer close(done)
			r.op.Lock()
			defer r.op.Unlock()
			e.runToggle(sw, on, src, detail)
		}()
	}
	return done, nil
}

func (e *Engine) runToggle(sw *Switch, on bool, src Source, detail string) {
	arg := "off"
	if on {
		arg = "on"
	}
	res := runner.Run(context.Background(), e.spec(sw, src, arg))
	e.record(sw, arg, src, detail, res)
	if res.ExitCode != 0 {
		e.setState(sw.ID, Error, time.Time{})
		return
	}
	st := runner.Run(context.Background(), e.spec(sw, src, "status"))
	switch {
	case st.ExitCode != 0:
		e.setState(sw.ID, Unknown, time.Time{})
	case st.Stdout == "on":
		e.setState(sw.ID, On, time.Now())
	default:
		e.setState(sw.ID, Off, time.Time{})
	}
}

func (e *Engine) spawn(sw *Switch, r *rt, src Source, detail string) {
	e.mu.Lock()
	running := r.daemon != nil && r.daemon.Running()
	e.mu.Unlock()
	if running {
		return
	}
	p, err := runner.Start(e.spec(sw, src, "run"))
	if err != nil {
		e.record(sw, "start", src, detail, runner.Result{ExitCode: 127, Err: err, Output: err.Error()})
		e.setState(sw.ID, Error, time.Time{})
		return
	}
	e.mu.Lock()
	r.daemon = p
	e.mu.Unlock()
	e.writeLedger()
	e.setState(sw.ID, On, p.Started)
	e.record(sw, "start", src, detail, runner.Result{})
	go e.superviseDaemon(sw.ID, p)
}

// superviseDaemon 等常驻进程结束：自然结束（exit 0）视为开关归位，异常退出标为 error。
func (e *Engine) superviseDaemon(id string, p *runner.Proc) {
	<-p.Done()
	e.mu.Lock()
	r := e.rts[id]
	sw := e.switches[id]
	current := r != nil && r.daemon == p
	if current {
		r.daemon = nil
	}
	e.mu.Unlock()
	if !current || sw == nil {
		return
	}
	e.writeLedger()
	ex := p.Exit()
	if ex.Stopped {
		return
	}
	res := runner.Result{ExitCode: ex.Code, Duration: time.Since(p.Started), Output: p.Output.String()}
	if ex.Code == 0 && !ex.Signaled {
		_ = e.cfg.Store.SetDesired(id, false)
		e.setState(id, Off, time.Time{})
		e.record(sw, "exit", Source("system"), "finished", res)
		return
	}
	if e.cfg.Store.Desired(id) {
		e.setState(id, Error, time.Time{})
	} else {
		e.setState(id, Off, time.Time{})
	}
	e.record(sw, "exit", Source("system"), "exited unexpectedly", res)
}

func (e *Engine) stopDaemon(sw *Switch, r *rt, src Source, detail string) {
	e.mu.Lock()
	p := r.daemon
	r.daemon = nil
	e.mu.Unlock()
	if p != nil {
		e.setState(sw.ID, Pending, time.Time{})
		p.Stop()
		e.writeLedger()
		e.record(sw, "stop", src, detail, runner.Result{Duration: time.Since(p.Started), Output: p.Output.String()})
	}
	e.setState(sw.ID, Off, time.Time{})
}

// ───────── 按钮（action） ─────────

// Run 执行按钮型开关，立即返回；结果在完成后通过事件与运行记录给出。
func (e *Engine) Run(id string, src Source, detail string) (<-chan store.Run, error) {
	sw, r, err := e.lookup(id)
	if err != nil {
		return nil, err
	}
	if sw.Header.Type != contract.Action {
		return nil, ErrNotAction
	}
	if !r.op.TryLock() {
		return nil, ErrBusy
	}
	e.markUser(r, src)
	e.mu.Lock()
	r.busySince = time.Now()
	e.mu.Unlock()
	e.setState(sw.ID, Running, time.Time{})
	out := make(chan store.Run, 1)
	go func() {
		defer r.op.Unlock()
		res := runner.Run(context.Background(), e.spec(sw, src))
		rec := e.record(sw, "run", src, detail, res)
		e.mu.Lock()
		r.busySince = time.Time{}
		e.mu.Unlock()
		e.setState(sw.ID, Idle, time.Time{})
		out <- rec
	}()
	return out, nil
}

// ───────── 可见性、参数、脚本 ─────────

// SetHidden 只改变面板可见性：运行状态完全不受影响（与 v1 一致）。
func (e *Engine) SetHidden(id string, hidden bool) error {
	sw, _, err := e.lookup(id)
	if err != nil {
		return err
	}
	if err := e.cfg.Store.SetHidden(sw.ID, hidden); err != nil {
		return err
	}
	e.emitSwitch(EventState, sw.ID)
	e.kick()
	return nil
}

// SetParams 修改参数；开关开着时用新值重启（daemon）或重跑 on（toggle）。
func (e *Engine) SetParams(id string, values map[string]string, src Source) error {
	sw, r, err := e.lookup(id)
	if err != nil {
		return err
	}
	for k := range values {
		if _, ok := sw.Header.Param(k); !ok {
			return fmt.Errorf("%w %q", ErrNoParam, k)
		}
	}
	changed := false
	for k, v := range values {
		c, err := e.cfg.Store.SetParam(sw.ID, k, v)
		if err != nil {
			return err
		}
		changed = changed || c
	}
	e.bump()
	e.emitSwitch(EventState, sw.ID)
	if changed {
		e.restartIfOn(sw, r, src, "params changed")
	}
	return nil
}

func (e *Engine) restartIfOn(sw *Switch, r *rt, src Source, detail string) {
	e.mu.Lock()
	on := r.state == On
	e.mu.Unlock()
	if !on {
		return
	}
	switch sw.Header.Type {
	case contract.Daemon:
		go func() {
			r.op.Lock()
			defer r.op.Unlock()
			e.stopDaemon(sw, r, src, detail)
			e.spawn(sw, r, src, detail)
		}()
	case contract.Toggle:
		go func() {
			r.op.Lock()
			defer r.op.Unlock()
			e.runToggle(sw, true, src, detail)
		}()
	}
}

// Script 返回脚本原文。
func (e *Engine) Script(id string) (string, error) {
	sw, _, err := e.lookup(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(sw.Path)
	return string(data), err
}

// UpdateScript 校验后覆盖脚本；开关开着时用新内容重启。返回 lint 警告。
func (e *Engine) UpdateScript(id, content string, src Source) ([]contract.Issue, error) {
	sw, _, err := e.lookup(id)
	if err != nil {
		return nil, err
	}
	issues := contract.Lint(content)
	if contract.HasErrors(issues) {
		return issues, &LintError{issues}
	}
	if err := writeScript(sw.Path, content); err != nil {
		return issues, err
	}
	e.bump()
	e.reload()
	if nsw, nr, err := e.lookup(sw.ID); err == nil {
		e.restartIfOn(nsw, nr, src, "script changed")
	}
	e.kick()
	return issues, nil
}

// CreateScript 校验后新建脚本，文件名由 <switch.name> 生成，重名时加序号。
func (e *Engine) CreateScript(content string) (string, []contract.Issue, error) {
	issues := contract.Lint(content)
	if contract.HasErrors(issues) {
		return "", issues, &LintError{issues}
	}
	name := "switch"
	if h, ok := contract.Parse(content); ok {
		name = h.Name
	}
	file := contract.Slug(name)
	path := filepath.Join(e.cfg.ScriptsDir, file)
	base, ext := strings.TrimSuffix(file, filepath.Ext(file)), filepath.Ext(file)
	for n := 2; ; n++ {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
		path = filepath.Join(e.cfg.ScriptsDir, fmt.Sprintf("%s-%d%s", base, n, ext))
	}
	if err := os.MkdirAll(e.cfg.ScriptsDir, 0o755); err != nil {
		return "", issues, err
	}
	if err := writeScript(path, content); err != nil {
		return "", issues, err
	}
	e.reload()
	e.kick()
	return filepath.Base(path), issues, nil
}

// DeleteScript 关闭开关（开着时）后把脚本移到废纸篓。
func (e *Engine) DeleteScript(id string) error {
	sw, r, err := e.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	on := r.state == On || r.daemon != nil
	e.mu.Unlock()
	if on && sw.Header.Type != contract.Action {
		if done, err := e.SetSwitch(sw.ID, false, Manual, "deleting"); err == nil {
			<-done
		}
	}
	if err := e.cfg.Trash(sw.Path); err != nil {
		return err
	}
	_ = e.cfg.Store.Forget(sw.ID)
	e.reload()
	return nil
}

func writeScript(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ───────── 恢复 ─────────

func (e *Engine) restoreDesired() {
	desired := e.cfg.Store.DesiredAll()
	for id, on := range desired {
		if !on {
			continue
		}
		sw, r, err := e.lookup(id)
		if err != nil {
			continue
		}
		switch sw.Header.Type {
		case contract.Daemon:
			r.op.Lock()
			e.spawn(sw, r, Restore, "")
			r.op.Unlock()
		case contract.Toggle:
			// 先查真实状态，已经是 on 就不重复执行（避免无谓的副作用，如重启 Finder）
			go func() {
				r.op.Lock()
				defer r.op.Unlock()
				st := runner.Run(context.Background(), e.spec(sw, Restore, "status"))
				if st.ExitCode == 0 && st.Stdout == "on" {
					e.setState(sw.ID, On, time.Now())
					return
				}
				e.runToggle(sw, true, Restore, "")
			}()
		}
	}
}

// ───────── 倒计时 ─────────

// Remaining 返回倒计时剩余秒数：声明了 countdown、duration 参数（分钟）大于 0 且开关开着时才有值。
func (e *Engine) remainingLocked(sw *Switch, r *rt) *int {
	mb := sw.Header.MenuBar
	if mb == nil || !mb.Countdown || r.state != On || r.activatedAt.IsZero() {
		return nil
	}
	p, ok := sw.Header.Param("duration")
	if !ok {
		return nil
	}
	var minutes int
	if _, err := fmt.Sscan(e.paramValue(sw, p), &minutes); err != nil || minutes <= 0 {
		return nil
	}
	minutes = min(minutes, 1_000_000_000)
	left := max(0, minutes*60-int(time.Since(r.activatedAt).Seconds()))
	return &left
}

func (e *Engine) bump() {
	e.mu.Lock()
	e.generation++
	e.mu.Unlock()
}
