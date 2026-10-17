package store

import (
	"errors"
	"slices"
	"time"
)

// 场景与自动化规则：结构由引擎与 internal/automation 解释，这里只负责保存。

// SceneStep 是场景里的一步：把开关打开、关闭，或运行按钮。
type SceneStep struct {
	Switch string `json:"switch"`
	Action string `json:"action"` // on | off | run
}

// Scene 是一键组合。
type Scene struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Icon     string      `json:"icon,omitempty"`
	Steps    []SceneStep `json:"steps"`
	Restore  bool        `json:"restore"`            // 再次点击时恢复激活前的状态
	ShowName bool        `json:"showName,omitempty"` // 激活期间在菜单栏显示场景名
	Shortcut string      `json:"shortcut,omitempty"`
}

// Trigger 是规则的触发条件。Kind 决定 Event 与 Value 的含义：
//
//	time      Value=HH:MM，Days=星期几（0=周日；空=每天）
//	wifi      Event=join|leave，Value=网络指纹或 ssid:<名称>，Label=显示名
//	power     Event=plug|unplug|low，Value=低电量阈值（百分比，默认 20）
//	app       Event=launch|quit，Value=bundle id，Label=应用名
//	display   Event=connect|disconnect
//	wake      Event=login|wake|unlock
//	file      Value=文件或目录路径
//	shortcut  Value=组合键（如 ctrl+alt+p）
type Trigger struct {
	Kind  string `json:"kind"`
	Event string `json:"event,omitempty"`
	Value string `json:"value,omitempty"`
	Label string `json:"label,omitempty"`
	Days  []int  `json:"days,omitempty"`
}

// RuleAction 是规则要做的事。
type RuleAction struct {
	Verb   string `json:"verb"`   // on | off | toggle | run | scene
	Target string `json:"target"` // 开关 id 或场景 id
}

// Condition 是可选的「仅当」：星期几与时间段（From > To 表示跨午夜）。
type Condition struct {
	Days []int  `json:"days,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// Rule 是一条自动化规则。
type Rule struct {
	ID        string     `json:"id"`
	Enabled   bool       `json:"enabled"`
	Trigger   Trigger    `json:"trigger"`
	Action    RuleAction `json:"action"`
	When      *Condition `json:"when,omitempty"`
	Notify    bool       `json:"notify,omitempty"`
	LastFired time.Time  `json:"lastFired,omitzero"`
	// Suspended 是引擎自动停用的原因（例如被反复来回切换）；用户重新启用时清空。
	Suspended string `json:"suspended,omitempty"`
}

var ErrNoSuchItem = errors.New("no such scene or rule")

// Scenes 返回全部场景的副本。
func (s *Store) Scenes() []Scene {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Scene, len(s.st.Scenes))
	for i, sc := range s.st.Scenes {
		sc.Steps = slices.Clone(sc.Steps)
		out[i] = sc
	}
	return out
}

// Scene 按 id 返回场景。
func (s *Store) Scene(id string) (Scene, bool) {
	for _, sc := range s.Scenes() {
		if sc.ID == id {
			return sc, true
		}
	}
	return Scene{}, false
}

// PutScene 新增或替换场景（按 id）。
func (s *Store) PutScene(sc Scene) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc.Steps = slices.Clone(sc.Steps)
	for i := range s.st.Scenes {
		if s.st.Scenes[i].ID == sc.ID {
			s.st.Scenes[i] = sc
			return s.saveLocked()
		}
	}
	s.st.Scenes = append(s.st.Scenes, sc)
	return s.saveLocked()
}

// DeleteScene 删除场景。
func (s *Store) DeleteScene(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Scenes {
		if s.st.Scenes[i].ID == id {
			s.st.Scenes = slices.Delete(s.st.Scenes, i, i+1)
			return s.saveLocked()
		}
	}
	return ErrNoSuchItem
}

// Rules 返回全部规则的副本。
func (s *Store) Rules() []Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Rule, len(s.st.Rules))
	for i, r := range s.st.Rules {
		out[i] = cloneRule(r)
	}
	return out
}

func cloneRule(r Rule) Rule {
	r.Trigger.Days = slices.Clone(r.Trigger.Days)
	if r.When != nil {
		w := *r.When
		w.Days = slices.Clone(w.Days)
		r.When = &w
	}
	return r
}

// Rule 按 id 返回规则。
func (s *Store) Rule(id string) (Rule, bool) {
	for _, r := range s.Rules() {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// PutRule 新增或替换规则（按 id）。
func (s *Store) PutRule(r Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r = cloneRule(r)
	for i := range s.st.Rules {
		if s.st.Rules[i].ID == r.ID {
			s.st.Rules[i] = r
			return s.saveLocked()
		}
	}
	s.st.Rules = append(s.st.Rules, r)
	return s.saveLocked()
}

// DeleteRule 删除规则。
func (s *Store) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Rules {
		if s.st.Rules[i].ID == id {
			s.st.Rules = slices.Delete(s.st.Rules, i, i+1)
			return s.saveLocked()
		}
	}
	return ErrNoSuchItem
}

// MarkFired 记录规则最近一次触发的时间。
func (s *Store) MarkFired(id string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Rules {
		if s.st.Rules[i].ID == id {
			s.st.Rules[i].LastFired = t
			return s.saveLocked()
		}
	}
	return ErrNoSuchItem
}

// Suspend 停用一条规则并记下原因（引擎发现规则互相打架时）。
func (s *Store) Suspend(id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Rules {
		if s.st.Rules[i].ID == id {
			s.st.Rules[i].Enabled = false
			s.st.Rules[i].Suspended = reason
			return s.saveLocked()
		}
	}
	return ErrNoSuchItem
}
