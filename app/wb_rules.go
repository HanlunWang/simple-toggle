package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 自动化：侧栏列表（含「全部暂停」）与规则编辑页。规则完整时立即保存，
// 不完整时（例如刚切到「应用」还没选应用）只保留草稿并提示缺什么。

var kindIcons = map[string]string{
	automation.KindTime: "clock", automation.KindWiFi: "wifi", automation.KindPower: "battery-charging",
	automation.KindApp: "app-window", automation.KindDisplay: "monitor", automation.KindWake: "sunrise",
	automation.KindFile: "file-diff", automation.KindShortcut: "keyboard",
}

func shortcutLabel(s string) string { return automation.ShortcutSymbols(s) }

func (w *workbench) targetName(verb, id string) string {
	if verb == "scene" {
		if sv := w.sceneByID(id); sv != nil {
			return sv.Name
		}
		return id + tr("rule_missing")
	}
	if v := w.view(id); v != nil {
		return v.Name
	}
	return id + tr("rule_missing")
}

func (w *workbench) ruleByID(id string) (store.Rule, bool) {
	if d, ok := w.ruleDraft[id]; ok {
		return d, true
	}
	for _, r := range w.rules {
		if r.ID == id {
			return r, true
		}
	}
	return store.Rule{}, false
}

// ───────── 侧栏 ─────────

func (w *workbench) pauseRow(c *ui.Context) {
	pal := w.pal
	if w.auto == nil {
		return
	}
	paused, until := w.auto.Paused()
	ui.Row(c.Key("pause")).FillWidth().Padding(8, 8, 4).Gap(6).AlignItems(ui.Center).Children(func() {
		if paused {
			label := tr("paused_forever")
			if !until.IsZero() {
				label = fmt.Sprintf(tr("paused_until"), until.Local().Format("15:04"))
			}
			icon(c, "pause", 12).TextColor(pal.Amber)
			ui.Text(c, label).FontSize(11.5).TextColor(pal.Amber).Grow(1).SingleLine()
			if pal.button(c, "resume", tr("resume"), "", btnGhost, false).Clicked() {
				_ = w.auto.Resume()
			}
			return
		}
		b := pal.button(c, "pause-all", tr("pause_all"), "pause", btnGhost, false)
		if b.Clicked() {
			w.pauseOpen = !w.pauseOpen
		}
		ui.PopoverBase(c, b, &w.pauseOpen, func(panel ui.Element) {
			w.menuPanel(panel)
			panel.Children(func() {
				now := time.Now()
				if w.menuItem(c, "p-1h", "clock", tr("pause_1h")) {
					_ = w.auto.Pause(now.Add(time.Hour))
					w.pauseOpen = false
				}
				if w.menuItem(c, "p-today", "moon", tr("pause_today")) {
					y, m, d := now.Date()
					_ = w.auto.Pause(time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()))
					w.pauseOpen = false
				}
				if w.menuItem(c, "p-forever", "pause", tr("pause_forever")) {
					_ = w.auto.Pause(time.Time{})
					w.pauseOpen = false
				}
			})
		})
	})
}

func (w *workbench) ruleList(c *ui.Context) {
	pal := w.pal
	w.pauseRow(c)
	if len(w.rules) == 0 {
		ui.Text(c, tr("rules_empty")).FontSize(12).TextColor(pal.T3).Margin(12, 8)
		return
	}
	ui.Column(c).FillWidth().Padding(2, 0, 0).Gap(1).Children(func() {
		for _, r := range w.rules {
			r := r
			sel := w.selRule == r.ID
			row := ui.Row(c.Key("rl-"+r.ID)).FillWidth().Padding(7, 8).Radius(7).Gap(8).AlignItems(ui.Start).Label(triggerText(r.Trigger))
			if sel {
				row.Background(pal.L2)
			} else if row.Hovered() {
				row.Background(pal.L1)
			}
			row.Children(func() {
				col := pal.T1
				if !r.Enabled {
					col = pal.T3
				}
				ui.Row(c).Width(16).Margin(2, 0, 0, 0).Justify(ui.Center).Children(func() { icon(c, kindIcons[r.Trigger.Kind], 14).TextColor(pal.T2) })
				ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, triggerText(r.Trigger)).FontSize(12.5).TextColor(col).SingleLine()
					ui.Text(c, "→ "+actionText(r.Action, w.targetName)).FontSize(11.5).TextColor(pal.T3).SingleLine()
				})
				dot := pal.L4
				switch {
				case r.Suspended != "":
					dot = pal.Amber
				case r.Enabled && pal.Ink:
					dot = pal.FG
				case r.Enabled:
					dot = pal.Accent
				}
				ui.Box(c).Size(6, 6).Radius(3).Margin(6, 0, 0, 0).Background(dot)
			})
			if row.Clicked() {
				w.selRule = r.ID
			}
		}
	})
}

// newRule 新建一条完整的规则（工作日 09:00 开启 target）并打开它。
func (w *workbench) newRule(target string) {
	if w.auto == nil {
		return
	}
	verb := "on"
	if target == "" && len(w.views) > 0 {
		target = w.views[0].ID
	}
	if v := w.view(target); v != nil && v.Type == contract.Action {
		verb = "run"
	}
	if target == "" && len(w.scenes) > 0 {
		verb, target = "scene", w.scenes[0].ID
	}
	r, err := w.auto.SaveRule(store.Rule{Enabled: true,
		Trigger: store.Trigger{Kind: automation.KindTime, Value: "09:00", Days: []int{1, 2, 3, 4, 5}},
		Action:  store.RuleAction{Verb: verb, Target: target}})
	if err != nil {
		w.setNotice(fmt.Sprintf(tr("rule_invalid"), err), true)
		return
	}
	w.section, w.selRule = secAutomations, r.ID
	w.dirty = true
}

// saveRule 校验后保存；不完整时只更新草稿。
func (w *workbench) saveRule(r store.Rule) {
	if err := automation.Validate(r); err != nil {
		w.ruleDraft[r.ID] = r
		w.ruleNote[r.ID] = err.Error()
		return
	}
	saved, err := w.auto.SaveRule(r)
	if err != nil {
		w.ruleDraft[r.ID] = r
		w.ruleNote[r.ID] = err.Error()
		return
	}
	delete(w.ruleDraft, r.ID)
	delete(w.ruleNote, r.ID)
	for i := range w.rules {
		if w.rules[i].ID == saved.ID {
			w.rules[i] = saved
		}
	}
	w.dirty = true
}

// ───────── 编辑页 ─────────

func (w *workbench) ruleEditor(c *ui.Context) {
	pal := w.pal
	if w.auto == nil {
		return
	}
	r, ok := w.ruleByID(w.selRule)
	if !ok && len(w.rules) > 0 {
		w.selRule = w.rules[0].ID
		r, ok = w.ruleByID(w.selRule)
	}
	if !ok {
		w.topBar(c, func() {})
		w.emptyState(c, "zap", tr("rules_empty"), tr("rules_empty_h"), tr("new_rule_bt"), func() { w.newRule("") })
		return
	}
	w.topBar(c, func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
			icon(c, kindIcons[r.Trigger.Kind], 15).TextColor(pal.T1)
			ui.Text(c, ruleText(r, w.targetName)).FontSize(13).FontWeight(500).TextColor(pal.T1).SingleLine().MinWidth(0)
			w.noticeText(c)
		})
		label := tr("rule_off")
		if r.Enabled {
			label = tr("rule_on")
		}
		ui.Text(c, label).FontSize(12).TextColor(pal.T3)
		on := r.Enabled
		w.pal.toggle(c, "rule-on", &on, false, false).Label(tr("rule_on"))
		if on != r.Enabled {
			r.Enabled = on
			if on {
				r.Suspended = ""
			}
			w.saveRule(r)
		}
		more := pal.iconBtn(c, "rl-more", "ellipsis", tr("more"), w.moreOpen)
		if more.Clicked() {
			w.moreOpen = !w.moreOpen
		}
		ui.PopoverBase(c, more, &w.moreOpen, func(panel ui.Element) {
			w.menuPanel(panel)
			panel.Children(func() {
				if w.menuItem(c, "rl-del", "trash-2", tr("rule_delete")) {
					w.moreOpen, w.deleteOpen = false, true
				}
			})
		})
	})
	if w.deleteOpen {
		if ui.AlertDialog(c.Key("rl-del-dialog"), &w.deleteOpen, tr("rule_del_title"), tr("rule_del_msg"), tr("cancel"), tr("delete")) == 1 {
			_ = w.auto.DeleteRule(r.ID)
			delete(w.ruleDraft, r.ID)
			w.selRule = ""
			w.dirty = true
		}
	}
	w.page(c, "rule-"+r.ID, func() {
		if r.Suspended != "" {
			ui.Row(c).FillWidth().Padding(10, 14).Radius(10).Gap(8).AlignItems(ui.Start).Background(alpha(pal.Amber, 0.12)).Children(func() {
				icon(c, "triangle-alert", 14).TextColor(pal.Amber).Margin(1, 0, 0, 0)
				ui.Text(c, tr("rule_suspended")).FontSize(12.5).TextColor(pal.T1).Wrap().Grow(1).MinWidth(0)
			})
		}
		if note := w.ruleNote[r.ID]; note != "" {
			ui.Row(c).FillWidth().Padding(8, 14).Radius(10).Gap(8).AlignItems(ui.Center).Background(alpha(pal.Amber, 0.10)).Children(func() {
				icon(c, "circle-alert", 14).TextColor(pal.Amber)
				ui.Text(c, fmt.Sprintf(tr("rule_invalid"), note)).FontSize(12).TextColor(pal.T1).Wrap().Grow(1)
			})
		}
		w.card(c, "rl-when", func() {
			ui.Text(c, tr("rule_when")).FontSize(12.5).FontWeight(600).TextColor(pal.T1).Margin(12, 14)
			w.kindGrid(c, r)
			pal.hair(c)
			ui.Row(c.Key("detail-"+r.Trigger.Kind)).FillWidth().Padding(14, 16).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
				w.triggerDetail(c, r)
			})
		})
		w.card(c, "rl-do", func() {
			ui.Text(c, tr("rule_do")).FontSize(12.5).FontWeight(600).TextColor(pal.T1).Margin(12, 14, 0)
			ui.Row(c).FillWidth().Padding(10, 16, 14).Gap(10).Wrap().AlignItems(ui.Center).Children(func() { w.actionEditor(c, r) })
		})
		w.card(c, "rl-only", func() {
			ui.Row(c).FillWidth().Padding(12, 14, 0).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("rule_only")).FontSize(12.5).FontWeight(600).TextColor(pal.T1)
				ui.Text(c, tr("rule_only_h")).FontSize(12).TextColor(pal.T3)
			})
			ui.Column(c).FillWidth().Padding(10, 16, 14).Gap(10).Children(func() { w.conditionEditor(c, r) })
		})
		w.card(c, "rl-opts", func() {
			w.cardRow(c, tr("rule_notify"), "", func() {
				on := r.Notify
				w.pal.toggle(c, "rl-notify", &on, false, false).Label(tr("rule_notify"))
				if on != r.Notify {
					r.Notify = on
					w.saveRule(r)
				}
			})
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			icon(c, "history", 13).TextColor(pal.T3)
			last := tr("never")
			if !r.LastFired.IsZero() {
				last = r.LastFired.Local().Format("01-02 15:04")
			}
			ui.Text(c, fmt.Sprintf(tr("rule_last"), last)).FontSize(12).TextColor(pal.T3).Grow(1)
			if pal.button(c, "rl-test", tr("rule_test"), "play", btnPlain, false).Clicked() {
				if err := w.auto.Test(r.ID); err != nil {
					w.setNotice(err.Error(), true)
				} else {
					w.setNotice(tr("rule_tested"), false)
				}
				w.dirty = true
			}
		})
	})
}

func (w *workbench) kindGrid(c *ui.Context, r store.Rule) {
	pal := w.pal
	ui.Grid(c.Key("kinds")).FillWidth().Columns(4).Gap(6).Padding(0, 14, 14).Children(func() {
		for _, k := range automation.Kinds {
			k := k
			sel := r.Trigger.Kind == k
			cell := ui.Column(c.Key("k-" + k)).Padding(10).Radius(10).Gap(6).Label(tr("kind_" + k))
			switch {
			case sel && pal.Ink:
				cell.Border(1.5, pal.FG).Background(pal.L1)
			case sel:
				cell.Border(1.5, pal.Accent).Background(pal.L1)
			default:
				cell.Border(1, pal.L2)
				if cell.Hovered() {
					cell.Background(pal.L1)
				}
			}
			cell.Children(func() {
				icon(c, kindIcons[k], 16).TextColor(pal.T1)
				ui.Text(c, tr("kind_"+k)).FontSize(12.5).FontWeight(500).TextColor(pal.T1).SingleLine()
				ui.Text(c, tr("kh_"+k)).FontSize(11).TextColor(pal.T3).Wrap()
			})
			if cell.Clicked() && !sel {
				r.Trigger = defaultTrigger(k)
				w.saveRule(r)
			}
		}
	})
}

// defaultTrigger 是切换种类时的初始值（尽量直接可用）。
func defaultTrigger(kind string) store.Trigger {
	switch kind {
	case automation.KindTime:
		return store.Trigger{Kind: kind, Value: "09:00", Days: []int{1, 2, 3, 4, 5}}
	case automation.KindWiFi:
		return store.Trigger{Kind: kind, Event: "join"}
	case automation.KindPower:
		return store.Trigger{Kind: kind, Event: "unplug"}
	case automation.KindApp:
		return store.Trigger{Kind: kind, Event: "launch"}
	case automation.KindDisplay:
		return store.Trigger{Kind: kind, Event: "connect"}
	case automation.KindWake:
		return store.Trigger{Kind: kind, Event: "wake"}
	}
	return store.Trigger{Kind: kind}
}

// eventSeg 是事件的分段选择。
func (w *workbench) eventSeg(c *ui.Context, r store.Rule, width float32, events ...string) {
	cur := slices.Index(events, r.Trigger.Event)
	labels := make([]string, len(events))
	for i, e := range events {
		labels[i] = tr("ev_" + e)
	}
	ui.Row(c).Width(width).Children(func() {
		if n := w.pal.segmented(c, "ev", cur, labels...); n != cur {
			r.Trigger.Event = events[n]
			w.saveRule(r)
		}
	})
}

func (w *workbench) word(c *ui.Context, s string) {
	ui.Text(c, s).FontSize(13).TextColor(w.pal.T3)
}

func (w *workbench) triggerDetail(c *ui.Context, r store.Rule) {
	pal := w.pal
	switch r.Trigger.Kind {
	case automation.KindTime:
		w.word(c, tr("time_at"))
		ui.Row(c).Width(80).Children(func() {
			w.field(c, r.ID+"/at", r.Trigger.Value, "09:00", true, func(s string) {
				r.Trigger.Value = strings.TrimSpace(s)
				w.saveRule(r)
			})
		})
		w.dayChips(c, "days", r.Trigger.Days, func(d []int) {
			r.Trigger.Days = d
			w.saveRule(r)
		})
	case automation.KindWiFi:
		w.eventSeg(c, r, 150, "join", "leave")
		label := r.Trigger.Label
		if r.Trigger.Value == "" {
			label = tr("wifi_any")
		}
		pal.badge(c, label, "")
		if pal.button(c, "wifi-cur", tr("wifi_current"), "locate", btnPlain, w.network == nil).Clicked() && w.network != nil {
			if key, name := w.network(); key != "" {
				r.Trigger.Value, r.Trigger.Label = key, name
				w.saveRule(r)
			} else {
				w.setNotice(tr("wifi_offline"), true)
			}
		}
		if r.Trigger.Value != "" && pal.button(c, "wifi-any", tr("wifi_any"), "", btnGhost, false).Clicked() {
			r.Trigger.Value, r.Trigger.Label = "", ""
			w.saveRule(r)
		}
		pal.hint(c, tr("wifi_h")).FillWidth()
	case automation.KindPower:
		w.eventSeg(c, r, 300, "plug", "unplug", "low")
		if r.Trigger.Event == "low" {
			v := r.Trigger.Value
			if v == "" {
				v = strconv.Itoa(automation.DefaultLowBattery)
			}
			ui.Row(c).Width(56).Children(func() {
				w.field(c, r.ID+"/low", v, "20", true, func(s string) {
					r.Trigger.Value = strings.TrimSpace(s)
					w.saveRule(r)
				})
			})
			w.word(c, tr("low_pct"))
		}
	case automation.KindApp:
		w.appPicker(c, r)
		w.eventSeg(c, r, 140, "launch", "quit")
	case automation.KindDisplay:
		w.eventSeg(c, r, 140, "connect", "disconnect")
	case automation.KindWake:
		w.eventSeg(c, r, 200, "login", "wake", "unlock")
	case automation.KindFile:
		ui.Row(c).Grow(1).MinWidth(220).Children(func() {
			w.field(c, r.ID+"/path", r.Trigger.Value, tr("file_ph"), true, func(s string) {
				r.Trigger.Value = strings.TrimSpace(s)
				w.saveRule(r)
			})
		})
		if pal.button(c, "file-pick", tr("file_pick"), "folder", btnPlain, w.pickPath == nil).Clicked() && w.pickPath != nil {
			w.pickPath(func(path string) {
				if path != "" {
					r.Trigger.Value = path
					w.saveRule(r)
				}
			})
		}
	case automation.KindShortcut:
		ui.Row(c).Width(260).Children(func() {
			if v, changed := w.shortcutField(c, "rl-sc-"+r.ID, r.Trigger.Value, w.issueFor("rule:"+r.ID)); changed {
				r.Trigger.Value = v
				w.saveRule(r)
			}
		})
	}
}

func (w *workbench) appPicker(c *ui.Context, r store.Rule) {
	var apps []appInfo
	if w.installedApps != nil {
		apps = w.installedApps()
	}
	names := []string{tr("app_pick")}
	cur := tr("app_pick")
	for _, a := range apps {
		names = append(names, a.Name)
		if strings.EqualFold(a.ID, r.Trigger.Value) {
			cur = a.Name
		}
	}
	if cur == tr("app_pick") && r.Trigger.Label != "" {
		names = append(names, r.Trigger.Label)
		cur = r.Trigger.Label
	}
	sel := cur
	ui.Select(c.Key("app"), &sel, names).Width(220).Label(tr("app_pick"))
	if sel != cur {
		for _, a := range apps {
			if a.Name == sel {
				r.Trigger.Value, r.Trigger.Label = a.ID, a.Name
				w.saveRule(r)
				return
			}
		}
	}
}

// dayChips 是七个星期的开关加「每天 / 工作日 / 周末」。
func (w *workbench) dayChips(c *ui.Context, key string, days []int, set func([]int)) {
	pal := w.pal
	ui.Row(c.Key(key)).Gap(4).AlignItems(ui.Center).Children(func() {
		order := []int{1, 2, 3, 4, 5, 6, 0}
		for _, d := range order {
			d := d
			on := len(days) == 0 || slices.Contains(days, d)
			ch := ui.Row(c.Key("d"+itoa(d))).Size(26, 26).Radius(13).AlignItems(ui.Center).Justify(ui.Center).Label(tr("dl" + itoa(d)))
			col := pal.T2
			switch {
			case on && pal.Ink:
				ch.Background(pal.FG)
				col = pal.BG
			case on:
				ch.Background(pal.Accent)
				col = pal.OnAccent
			default:
				ch.Border(1, pal.L3)
				if ch.Hovered() {
					ch.Background(pal.L1)
				}
			}
			ch.Children(func() { ui.Text(c, tr("d"+itoa(d))).FontSize(11.5).FontWeight(500).TextColor(col) })
			if ch.Clicked() {
				cur := days
				if len(cur) == 0 {
					cur = []int{0, 1, 2, 3, 4, 5, 6}
				}
				var next []int
				if slices.Contains(cur, d) {
					next = slices.DeleteFunc(slices.Clone(cur), func(x int) bool { return x == d })
				} else {
					next = append(slices.Clone(cur), d)
				}
				slices.Sort(next)
				if len(next) == 7 {
					next = nil
				}
				if len(next) > 0 || len(cur) > 0 {
					set(next)
				}
			}
		}
		ui.Box(c).Width(6)
		for _, preset := range []struct {
			key  string
			days []int
		}{{"days_every", nil}, {"days_weekdays", []int{1, 2, 3, 4, 5}}, {"days_weekend", []int{0, 6}}} {
			preset := preset
			if pal.button(c, "p-"+preset.key, tr(preset.key), "", btnGhost, false).Clicked() {
				set(preset.days)
			}
		}
	})
}

var verbs = []string{"on", "off", "toggle", "run", "scene"}

func (w *workbench) actionEditor(c *ui.Context, r store.Rule) {
	cur := slices.Index(verbs, r.Action.Verb)
	labels := make([]string, len(verbs))
	for i, v := range verbs {
		labels[i] = tr("v_" + v)
	}
	ui.Row(c).Width(380).Children(func() {
		if n := w.pal.segmented(c, "verb", cur, labels...); n != cur {
			wasScene := r.Action.Verb == "scene"
			r.Action.Verb = verbs[n]
			if (verbs[n] == "scene") != wasScene {
				r.Action.Target = ""
				if verbs[n] == "scene" && len(w.scenes) > 0 {
					r.Action.Target = w.scenes[0].ID
				} else if verbs[n] != "scene" && len(w.views) > 0 {
					r.Action.Target = w.views[0].ID
				}
			}
			w.saveRule(r)
		}
	})
	var names, ids []string
	if r.Action.Verb == "scene" {
		for _, sv := range w.scenes {
			names, ids = append(names, sv.Name), append(ids, sv.ID)
		}
	} else {
		for _, v := range w.views {
			if r.Action.Verb == "run" && v.Type != contract.Action {
				continue
			}
			if r.Action.Verb != "run" && v.Type == contract.Action {
				continue
			}
			names, ids = append(names, v.Name), append(ids, v.ID)
		}
	}
	cur2 := tr("pick_target")
	if i := slices.Index(ids, r.Action.Target); i >= 0 {
		cur2 = names[i]
	} else {
		names = append([]string{cur2}, names...)
		ids = append([]string{""}, ids...)
	}
	sel := cur2
	ui.Select(c.Key("target"), &sel, names).Width(220).Label(tr("pick_target"))
	if sel != cur2 {
		if i := slices.Index(names, sel); i >= 0 && ids[i] != "" {
			r.Action.Target = ids[i]
			w.saveRule(r)
		}
	}
}

func (w *workbench) conditionEditor(c *ui.Context, r store.Rule) {
	pal := w.pal
	cond := store.Condition{}
	if r.When != nil {
		cond = *r.When
	}
	write := func(c2 store.Condition) {
		if len(c2.Days) == 0 && c2.From == "" && c2.To == "" {
			r.When = nil
		} else {
			r.When = &c2
		}
		w.saveRule(r)
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("cond_days")).FontSize(12.5).TextColor(pal.T2).Width(64)
		w.dayChips(c, "cdays", cond.Days, func(d []int) {
			cond.Days = d
			write(cond)
		})
	})
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("cond_window")).FontSize(12.5).TextColor(pal.T2).Width(64)
		if cond.From == "" && cond.To == "" {
			ui.Text(c, tr("cond_none")).FontSize(12.5).TextColor(pal.T3)
			if pal.button(c, "cond-add", tr("cond_add"), "plus", btnGhost, false).Clicked() {
				cond.From, cond.To = "09:00", "18:00"
				write(cond)
			}
			return
		}
		ui.Row(c).Width(72).Children(func() {
			w.field(c, r.ID+"/from", cond.From, "09:00", true, func(s string) {
				cond.From = strings.TrimSpace(s)
				write(cond)
			})
		})
		w.word(c, "–")
		ui.Row(c).Width(72).Children(func() {
			w.field(c, r.ID+"/to", cond.To, "18:00", true, func(s string) {
				cond.To = strings.TrimSpace(s)
				write(cond)
			})
		})
		if pal.iconBtn(c, "cond-rm", "x", tr("remove"), false).Clicked() {
			cond.From, cond.To = "", ""
			write(cond)
		}
	})
}

// ───────── 检查器里的「快捷键」与「自动触发」 ─────────

func (w *workbench) shortcutSection(c *ui.Context, v engine.View) {
	hint := tr("sc_hint_toggle")
	if v.Type == contract.Action {
		hint = tr("sc_hint_run")
	}
	if val, changed := w.shortcutField(c, "sw-sc-"+v.ID, v.Meta.Shortcut, w.issueFor("switch:"+v.ID)); changed {
		m := v.Meta
		m.Shortcut = val
		_ = w.b.SetMeta(v.ID, m)
		w.dirty = true
		if w.auto != nil {
			w.auto.Reload()
		}
	}
	w.pal.hint(c, hint)
}

func (w *workbench) triggersSection(c *ui.Context, v engine.View) {
	pal := w.pal
	var mine []store.Rule
	for _, r := range w.rules {
		if r.Action.Verb != "scene" && r.Action.Target == v.ID {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		pal.hint(c, tr("triggers_hint"))
	}
	for _, r := range mine {
		r := r
		row := ui.Row(c.Key("tr-"+r.ID)).FillWidth().Padding(7, 9).Radius(8).Border(1, pal.L2).Gap(8).AlignItems(ui.Center).Label(triggerText(r.Trigger))
		if row.Hovered() {
			row.Background(pal.L1)
		}
		row.Children(func() {
			icon(c, kindIcons[r.Trigger.Kind], 13).TextColor(pal.T2)
			ui.Text(c, triggerText(r.Trigger)+" → "+tr("v_"+r.Action.Verb)).FontSize(12).TextColor(pal.T1).SingleLine().Grow(1).MinWidth(0)
			dot := pal.L4
			if r.Enabled {
				dot = pal.Green
			}
			ui.Box(c).Size(6, 6).Radius(3).Background(dot)
		})
		if row.Clicked() {
			w.section, w.selRule = secAutomations, r.ID
		}
	}
}
