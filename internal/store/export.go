package store

import (
	"maps"
	"time"
)

// Export 是「导出配置」的内容：用户对开关的安排、场景、规则与设置；脚本由调用方附上。
type Export struct {
	Format   string                       `json:"format"` // "simpletoggle-config"
	Version  int                          `json:"version"`
	Exported time.Time                    `json:"exported"`
	Settings map[string]string            `json:"settings,omitempty"`
	Hidden   map[string]bool              `json:"hidden,omitempty"`
	Params   map[string]map[string]string `json:"params,omitempty"`
	Meta     map[string]Meta              `json:"meta,omitempty"`
	Scenes   []Scene                      `json:"scenes,omitempty"`
	Rules    []Rule                       `json:"automations,omitempty"`
	Scripts  map[string]string            `json:"scripts,omitempty"`
}

// ExportFormat 标识导出文件。
const ExportFormat = "simpletoggle-config"

// 不导出、导入时也不覆盖的设置（只属于这台机器）。
var localSettings = map[string]bool{"onboarded": true, "advanced.scriptsDir": true}

// ExportState 返回当前的安排、场景、规则与设置（不含脚本）。
func (s *Store) ExportState() Export {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := Export{Format: ExportFormat, Version: 1, Exported: time.Now(),
		Settings: map[string]string{}, Hidden: maps.Clone(s.st.Hidden), Meta: maps.Clone(s.st.Meta),
		Params: map[string]map[string]string{}}
	for k, v := range s.st.Settings {
		if !localSettings[k] {
			x.Settings[k] = v
		}
	}
	for id, p := range s.st.Params {
		x.Params[id] = maps.Clone(p)
	}
	for _, sc := range s.st.Scenes {
		x.Scenes = append(x.Scenes, sc)
	}
	for _, r := range s.st.Rules {
		x.Rules = append(x.Rules, cloneRule(r))
	}
	return x
}

// ImportState 合并导入的内容：同 id 的场景与规则被替换，其余保留；设置逐项覆盖。
func (s *Store) ImportState(x Export) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range x.Settings {
		if !localSettings[k] {
			s.st.Settings[k] = v
		}
	}
	maps.Copy(s.st.Hidden, x.Hidden)
	maps.Copy(s.st.Meta, x.Meta)
	for id, p := range x.Params {
		if s.st.Params[id] == nil {
			s.st.Params[id] = map[string]string{}
		}
		maps.Copy(s.st.Params[id], p)
	}
	for _, sc := range x.Scenes {
		replaced := false
		for i := range s.st.Scenes {
			if s.st.Scenes[i].ID == sc.ID {
				s.st.Scenes[i], replaced = sc, true
			}
		}
		if !replaced {
			s.st.Scenes = append(s.st.Scenes, sc)
		}
	}
	for _, r := range x.Rules {
		r.LastFired = time.Time{}
		replaced := false
		for i := range s.st.Rules {
			if s.st.Rules[i].ID == r.ID {
				s.st.Rules[i], replaced = r, true
			}
		}
		if !replaced {
			s.st.Rules = append(s.st.Rules, r)
		}
	}
	return s.saveLocked()
}

// ResetSettings 清空设置（保留只属于这台机器的几项）。
func (s *Store) ResetSettings() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.st.Settings {
		if !localSettings[k] {
			delete(s.st.Settings, k)
		}
	}
	return s.saveLocked()
}
