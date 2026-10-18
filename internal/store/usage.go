package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// 用量统计：按天汇总的使用次数，单独存成 usage.json。运行记录只保留最近 2000 条、
// 默认 30 天，统计需要更长的时间跨度，所以汇总在写入记录时同步累加，最多保留 400 天。
// 一天一条，体积只有几十 KB。

const (
	usageFile = "usage.json"
	// UsageDays 是用量统计保留的天数。
	UsageDays = 400
	// DayLayout 是统计里「哪一天」的格式（本地时区）。
	DayLayout = "2006-01-02"
)

// UsageDay 是一天的使用次数。
type UsageDay struct {
	Total  int            `json:"total"`
	Failed int            `json:"failed,omitempty"`
	Hours  [24]int        `json:"hours"`
	Items  map[string]int `json:"items"`   // 开关 id（场景为 scene:<id>）→ 次数
	Source map[string]int `json:"sources"` // 来源 → 次数
}

// Usage 是全部用量统计的副本。
type Usage struct {
	Days  map[string]UsageDay `json:"days"`  // DayLayout → 当天
	Names map[string]string   `json:"names"` // 开关 id → 最近一次的名称（删掉的开关也能显示名字）
}

// Countable 报告一条记录是否算作一次「使用」：开、关、运行、启动、停止；
// 状态查询、进程自行退出、启动时恢复与系统动作不算。
func Countable(r Run) bool {
	switch r.Action {
	case "on", "off", "run", "start", "stop":
	default:
		return false
	}
	return r.Source != "restore" && r.Source != "system"
}

func (u *Usage) add(r Run) {
	if !Countable(r) {
		return
	}
	if u.Days == nil {
		u.Days = map[string]UsageDay{}
	}
	if u.Names == nil {
		u.Names = map[string]string{}
	}
	t := r.Time.Local()
	key := t.Format(DayLayout)
	d := u.Days[key]
	if d.Items == nil {
		d.Items = map[string]int{}
	}
	if d.Source == nil {
		d.Source = map[string]int{}
	}
	d.Total++
	if !r.OK {
		d.Failed++
	}
	d.Hours[t.Hour()]++
	d.Items[r.Switch]++
	d.Source[r.Source]++
	u.Days[key] = d
	if r.Name != "" {
		u.Names[r.Switch] = r.Name
	}
}

// prune 丢掉 UsageDays 天以前的数据，以及不再出现在任何一天里的名字。
func (u *Usage) prune(now time.Time) {
	cutoff := now.AddDate(0, 0, -UsageDays).Format(DayLayout)
	used := map[string]bool{}
	for k, d := range u.Days {
		if k < cutoff {
			delete(u.Days, k)
			continue
		}
		for id := range d.Items {
			used[id] = true
		}
	}
	for id := range u.Names {
		if !used[id] {
			delete(u.Names, id)
		}
	}
}

// loadUsage 读取 usage.json；文件不存在（升级后第一次）时用现有的运行记录回填。
func (s *Store) loadUsage() {
	data, err := os.ReadFile(filepath.Join(s.dir, usageFile))
	switch {
	case err == nil && json.Unmarshal(data, &s.usage) == nil:
		s.usage.prune(time.Now())
		return
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return // 读不了（权限等）：本次只在内存里统计，不覆盖文件
	}
	s.usage = Usage{}
	for _, r := range s.runs {
		s.usage.add(r)
	}
	_ = s.saveUsageLocked()
}

func (s *Store) saveUsageLocked() error {
	data, err := json.Marshal(s.usage)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, usageFile), data, 0o644)
}

// Usage 返回用量统计的副本。
func (s *Store) Usage() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Usage{Days: make(map[string]UsageDay, len(s.usage.Days)), Names: make(map[string]string, len(s.usage.Names))}
	for k, d := range s.usage.Days {
		c := d
		c.Items = make(map[string]int, len(d.Items))
		for id, n := range d.Items {
			c.Items[id] = n
		}
		c.Source = make(map[string]int, len(d.Source))
		for src, n := range d.Source {
			c.Source[src] = n
		}
		out.Days[k] = c
	}
	for id, n := range s.usage.Names {
		out.Names[id] = n
	}
	return out
}

// UsageItem 是一个开关在一段时间里的使用次数。
type UsageItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Top 返回 from 到 to（含）之间各开关的使用次数，从多到少。
func (u Usage) Top(from, to time.Time) []UsageItem {
	lo, hi := from.Local().Format(DayLayout), to.Local().Format(DayLayout)
	counts := map[string]int{}
	for k, d := range u.Days {
		if k < lo || k > hi {
			continue
		}
		for id, n := range d.Items {
			counts[id] += n
		}
	}
	out := make([]UsageItem, 0, len(counts))
	for id, n := range counts {
		out = append(out, UsageItem{ID: id, Name: u.Names[id], Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// UsageFromRuns 从运行记录汇总用量统计（测试与导入用）。
func UsageFromRuns(runs []Run) Usage {
	var u Usage
	for _, r := range runs {
		u.add(r)
	}
	return u
}
