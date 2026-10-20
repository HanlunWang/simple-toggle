package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 规则的文字说明（侧栏、运行记录、通知）。

// daysText 把星期集合写成「每天 / 工作日 / 周末 / 周一、周三」。
func daysText(days []int) string {
	d := slices.Clone(days)
	slices.Sort(d)
	switch {
	case len(d) == 0 || len(d) == 7:
		return tr("days_every")
	case slices.Equal(d, []int{1, 2, 3, 4, 5}):
		return tr("days_weekdays")
	case slices.Equal(d, []int{0, 6}):
		return tr("days_weekend")
	}
	var names []string
	for _, x := range d {
		names = append(names, tr("dl"+strconv.Itoa(x)))
	}
	sep := "、"
	if english {
		sep = ", "
	}
	return strings.Join(names, sep)
}

// triggerText 是触发条件的说明。
func triggerText(t store.Trigger) string {
	label := t.Label
	switch t.Kind {
	case automation.KindTime:
		return fmt.Sprintf(tr("desc_time"), daysText(t.Days), t.Value)
	case automation.KindWiFi:
		if label == "" {
			label = tr("some_network")
		}
		if t.Event == "leave" {
			return fmt.Sprintf(tr("desc_wifi_leave"), label)
		}
		return fmt.Sprintf(tr("desc_wifi_join"), label)
	case automation.KindPower:
		if t.Event == "low" {
			v := t.Value
			if v == "" {
				v = strconv.Itoa(automation.DefaultLowBattery)
			}
			return fmt.Sprintf(tr("desc_power_low"), v)
		}
		return tr("ev_" + t.Event)
	case automation.KindApp:
		if label == "" {
			label = t.Value
		}
		if label == "" {
			label = tr("some_app")
		}
		return fmt.Sprintf(tr("desc_app"), label, evWord(t.Event))
	case automation.KindDisplay:
		return fmt.Sprintf(tr("desc_display"), evWord(t.Event))
	case automation.KindWake:
		return fmt.Sprintf(tr("desc_wake"), evWord(t.Event))
	case automation.KindFile:
		return fmt.Sprintf(tr("desc_file"), filepath.Base(t.Value))
	case automation.KindShortcut:
		return fmt.Sprintf(tr("desc_shortcut"), automation.ShortcutSymbols(t.Value))
	}
	return t.Kind
}

// actionText 是规则动作的说明；name 返回开关或场景的显示名。
func actionText(a store.RuleAction, name func(verb, id string) string) string {
	return tr("v_"+a.Verb) + " " + name(a.Verb, a.Target)
}

// ruleText 是完整说明：「工作日 18:30 → 激活场景 下班」。
func ruleText(r store.Rule, name func(verb, id string) string) string {
	return triggerText(r.Trigger) + " → " + actionText(r.Action, name)
}

// evWord 是拼进句子里的事件词：英文里小写（「Safari launches」「Display connected」），中文原样。
func evWord(event string) string {
	if english {
		return strings.ToLower(tr("ev_" + event))
	}
	return tr("ev_" + event)
}
