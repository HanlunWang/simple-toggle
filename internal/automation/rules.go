package automation

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/HanlunWang/open-toggle/internal/store"
)

// 触发器种类。
const (
	KindTime     = "time"
	KindWiFi     = "wifi"
	KindPower    = "power"
	KindApp      = "app"
	KindDisplay  = "display"
	KindWake     = "wake"
	KindFile     = "file"
	KindShortcut = "shortcut"
)

// Kinds 是全部触发器种类（界面按这个顺序展示）。
var Kinds = []string{KindTime, KindWiFi, KindPower, KindApp, KindDisplay, KindWake, KindFile, KindShortcut}

// DefaultLowBattery 是「电量低于」的默认阈值（百分比）。
const DefaultLowBattery = 20

// Event 是一次可能触发规则的事件。
type Event struct {
	Kind  string
	Event string // join / leave、plug / unplug / low、launch / quit、connect / disconnect、login / wake / unlock
	Value string // 网络指纹、bundle id、文件路径、快捷键
	Label string // 给人看的名称（网络名、应用名）
	// 电量变化：低电量规则按各自的阈值判断是否「刚刚跌破」
	Prev, Cur int
}

// parseHM 解析 "HH:MM"，返回从零点起的分钟数。
func parseHM(s string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

func dayOK(days []int, t time.Time) bool {
	return len(days) == 0 || slices.Contains(days, int(t.Weekday()))
}

// ConditionMet 判断「仅当」是否成立：星期几与时间段（From > To 表示跨午夜）。
func ConditionMet(c *store.Condition, now time.Time) bool {
	if c == nil {
		return true
	}
	if !dayOK(c.Days, now) {
		return false
	}
	from, okF := parseHM(c.From)
	to, okT := parseHM(c.To)
	if !okF || !okT || from == to {
		return true
	}
	cur := now.Hour()*60 + now.Minute()
	if from < to {
		return cur >= from && cur < to
	}
	return cur >= from || cur < to
}

// Matches 判断事件是否命中规则的触发条件（不含「仅当」与限流）。
func Matches(t store.Trigger, ev Event) bool {
	if t.Kind != ev.Kind {
		return false
	}
	switch t.Kind {
	case KindPower:
		if t.Event == "low" {
			if ev.Event != "level" {
				return false
			}
			th := DefaultLowBattery
			if n, err := strconv.Atoi(t.Value); err == nil && n > 0 && n < 100 {
				th = n
			}
			return ev.Prev >= th && ev.Cur < th
		}
		return t.Event == ev.Event
	case KindWiFi:
		if t.Event != ev.Event {
			return false
		}
		return t.Value == "" || t.Value == ev.Value
	case KindApp:
		return t.Event == ev.Event && strings.EqualFold(t.Value, ev.Value)
	case KindDisplay, KindWake:
		return t.Event == ev.Event
	case KindFile, KindShortcut:
		return t.Value == ev.Value
	}
	return false
}

// TimeDue 判断时间规则在 now 这一分钟是否应该触发。
func TimeDue(t store.Trigger, now time.Time) bool {
	at, ok := parseHM(t.Value)
	return ok && at == now.Hour()*60+now.Minute() && dayOK(t.Days, now)
}

// Validate 检查规则是否完整。
func Validate(r store.Rule) error {
	switch r.Trigger.Kind {
	case KindTime:
		if _, ok := parseHM(r.Trigger.Value); !ok {
			return fmt.Errorf("time must be HH:MM")
		}
	case KindWiFi:
		if r.Trigger.Event != "join" && r.Trigger.Event != "leave" {
			return fmt.Errorf("wifi event must be join or leave")
		}
	case KindPower:
		if !slices.Contains([]string{"plug", "unplug", "low"}, r.Trigger.Event) {
			return fmt.Errorf("power event must be plug, unplug or low")
		}
	case KindApp:
		if r.Trigger.Value == "" || (r.Trigger.Event != "launch" && r.Trigger.Event != "quit") {
			return fmt.Errorf("app rules need an app and launch or quit")
		}
	case KindDisplay:
		if r.Trigger.Event != "connect" && r.Trigger.Event != "disconnect" {
			return fmt.Errorf("display event must be connect or disconnect")
		}
	case KindWake:
		if !slices.Contains([]string{"login", "wake", "unlock"}, r.Trigger.Event) {
			return fmt.Errorf("wake event must be login, wake or unlock")
		}
	case KindFile:
		if r.Trigger.Value == "" {
			return fmt.Errorf("file rules need a path")
		}
	case KindShortcut:
		if s, err := NormalizeShortcut(r.Trigger.Value); err != nil || s == "" {
			return ErrBadShortcut
		}
	default:
		return fmt.Errorf("unknown trigger kind %q", r.Trigger.Kind)
	}
	if !slices.Contains([]string{"on", "off", "toggle", "run", "scene"}, r.Action.Verb) {
		return fmt.Errorf("action must be on, off, toggle, run or scene")
	}
	if r.Action.Target == "" {
		return fmt.Errorf("action needs a target")
	}
	if r.When != nil {
		if (r.When.From == "") != (r.When.To == "") {
			return fmt.Errorf("time window needs both from and to")
		}
		if r.When.From != "" {
			if _, ok := parseHM(r.When.From); !ok {
				return fmt.Errorf("from must be HH:MM")
			}
			if _, ok := parseHM(r.When.To); !ok {
				return fmt.Errorf("to must be HH:MM")
			}
		}
	}
	return nil
}
