package store

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Change 是一次等待用户确认的脚本修改（AI 提交，用户在 app 里批准或拒绝）。
type Change struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`             // update | create
	Switch   string    `json:"switch,omitempty"` // update 的目标；create 批准后为新开关的 id
	Name     string    `json:"name,omitempty"`   // 脚本里的名称（显示用）
	Content  string    `json:"content"`
	Previous string    `json:"previous,omitempty"` // 提交时磁盘上的内容（update）
	Source   string    `json:"source"`
	Detail   string    `json:"detail,omitempty"` // 例如 mcp/claude-code
	Created  time.Time `json:"created"`
	Status   string    `json:"status"` // pending | approved | rejected | failed
	Decided  time.Time `json:"decided,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// MaxDecidedChanges 是保留的已处理变更条数（供 AI 查询结果）。
const MaxDecidedChanges = 50

// Changes 返回全部变更（新的在前）。
func (s *Store) Changes() []Change {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.st.Changes)
	slices.Reverse(out)
	return out
}

// Change 按 id 返回变更。
func (s *Store) Change(id string) (Change, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.st.Changes {
		if c.ID == id {
			return c, true
		}
	}
	return Change{}, false
}

// AddChange 保存一条新的待确认变更并分配 id。
func (s *Store) AddChange(c Change) (Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 1
	for _, x := range s.st.Changes {
		num, ok := strings.CutPrefix(x.ID, "chg-")
		if !ok {
			continue // 手工改过的 id 不参与编号
		}
		if v, err := strconv.Atoi(num); err == nil && v >= n {
			n = v + 1
		}
	}
	c.ID = "chg-" + strconv.Itoa(n)
	if c.Created.IsZero() {
		c.Created = time.Now()
	}
	c.Status = "pending"
	s.st.Changes = append(s.st.Changes, c)
	s.trimChangesLocked()
	return c, s.saveLocked()
}

// PutChange 更新一条变更（处理结果）。
func (s *Store) PutChange(c Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Changes {
		if s.st.Changes[i].ID == c.ID {
			s.st.Changes[i] = c
			s.trimChangesLocked()
			return s.saveLocked()
		}
	}
	return ErrNoSuchItem
}

// trimChangesLocked 只保留最近的已处理变更；待确认的全部保留。
func (s *Store) trimChangesLocked() {
	decided := 0
	for i := len(s.st.Changes) - 1; i >= 0; i-- {
		if s.st.Changes[i].Status == "pending" {
			continue
		}
		decided++
		if decided > MaxDecidedChanges {
			s.st.Changes = slices.Delete(s.st.Changes, i, i+1)
		}
	}
}
