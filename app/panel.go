package app

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/keys"
)

// panel 是菜单栏弹出面板的状态与视图。所有字段只在主线程读写。
type panel struct {
	b     Backend
	prefs Prefs
	pal   palette

	views     []engine.View
	scenes    []engine.SceneView
	sceneBusy map[string]bool // 已点击、还没执行完的场景
	loadedAt  time.Time
	dirty     bool

	query     string
	searching bool
	expanded  string
	collapsed map[string]bool
	pending   map[string]bool      // 刚发出的 on / off 的目标值，状态回来前用它显示开关
	edits     map[string]string    // 参数输入框的编辑中内容
	flash     map[string]time.Time // action 完成后短暂显示结果
	armed     map[string]time.Time // 「运行前确认」：第一次点击后等待确认的截止时间

	needsAX            bool
	axTrusted          func() bool
	reduceTransparency func() bool // 系统「降低透明度」：面板改为实色

	// 外部动作（测试里可留空）
	openScripts   func()
	openWorkbench func(id string)
	openSettings  func()
	requestAX     func()
	quit          func()
	resize        func(height float32)
	lastHeight    float32
}

func newPanel(b Backend, prefs Prefs) *panel {
	return &panel{
		b: b, prefs: prefs, dirty: true,
		collapsed: map[string]bool{}, pending: map[string]bool{}, edits: map[string]string{}, flash: map[string]time.Time{},
		armed:     map[string]time.Time{},
		sceneBusy: map[string]bool{},
		axTrusted: func() bool { return true }, reduceTransparency: func() bool { return false },
	}
}

// invalidate 标记数据过期（引擎事件到来时调用）。
func (p *panel) invalidate() { p.dirty = true }

func (p *panel) load() {
	countdown := false
	for _, v := range p.views {
		if v.Remaining != nil || v.State == engine.Running {
			countdown = true
		}
	}
	if !p.dirty && !(countdown && time.Since(p.loadedAt) >= 900*time.Millisecond) {
		return
	}
	p.views = sortedViews(p.b.List())
	p.scenes = p.b.Scenes()
	p.loadedAt = time.Now()
	p.dirty = false
	p.needsAX = false
	for _, v := range p.views {
		if v.State != engine.Pending {
			delete(p.pending, v.ID)
		}
		if v.Hidden {
			continue
		}
		for _, pv := range v.Params {
			if pv.Type == contract.ParamKey {
				p.needsAX = true
			}
		}
	}
}

func (p *panel) visible() []engine.View {
	q := strings.ToLower(strings.TrimSpace(p.query))
	var out []engine.View
	for _, v := range p.views {
		if v.Hidden {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(v.Name), q) && !strings.Contains(strings.ToLower(v.ID), q) {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (p *panel) onCount() int {
	n := 0
	for _, v := range p.views {
		if !v.Hidden && v.Type != contract.Action && v.State == engine.On {
			n++
		}
	}
	return n
}

// ───────── 视图 ─────────

func (p *panel) view(c *ui.Context) {
	p.load()
	base := c.Theme()
	p.pal = newPalette(base.Dark, p.prefs.Accent)
	c.SetTheme(p.pal.theme(base))
	pal := p.pal

	c.Root().Background(ui.Transparent)
	root := ui.Column(c).Fill()
	if c.Vibrancy() && p.prefs.Material != "solid" && !p.reduceTransparency() {
		root.Background(alpha(pal.BG, 0.5)) // 在系统材质上叠一层暖色，与设计稿一致
	} else {
		root.Background(pal.BG)
	}
	const maxBody = float32(560)
	var top, inner, bottom ui.Element
	root.Children(func() {
		top = ui.Column(c).FillWidth().Children(func() {
			p.header(c)
			if p.searching {
				p.searchField(c)
			}
			if p.prefs.ShowScenes && len(p.scenes) > 0 && p.query == "" {
				p.sceneBar(c)
			}
			if p.needsAX && !p.axTrusted() {
				p.axBanner(c)
			}
			ui.Column(c).FillWidth().Height(1).Margin(0, 12).Background(pal.L2)
		})
		ui.Scroll(c).FillWidth().Grow(1).Children(func() {
			inner = ui.Column(c).FillWidth().Children(func() { p.body(c) })
		})
		bottom = ui.Column(c).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Height(1).Background(pal.L2)
			p.footer(c)
		})
	})
	// 按内容调整面板高度：顶部 + 列表内容（封顶后在内部滚动）+ 底栏。
	// 读的是上一帧的尺寸，内容不变时不再触发。
	if p.resize != nil && inner.Bounds().H > 0 {
		h := top.Bounds().H + min(inner.Bounds().H, maxBody) + bottom.Bounds().H
		if math.Abs(float64(h-p.lastHeight)) > 0.5 {
			p.lastHeight = h
			p.resize(h)
		}
	}
	for _, v := range p.views {
		if v.Remaining != nil {
			c.After(time.Second)
			break
		}
		if v.State == engine.Running || v.State == engine.Pending {
			c.After(100 * time.Millisecond)
			break
		}
	}
	for id, t := range p.armed {
		if d := time.Until(t); d > 0 {
			c.After(d)
		} else {
			delete(p.armed, id)
		}
	}
	for id, t := range p.flash {
		if d := time.Until(t); d > 0 {
			c.After(d)
		} else {
			delete(p.flash, id)
		}
	}
}

func (p *panel) header(c *ui.Context) {
	pal := p.pal
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(12, 12, 8, 16).Children(func() {
		ui.Text(c, "SimpleToggle").FontSize(13).FontWeight(600).TextColor(pal.T1)
		if n := p.onCount(); n > 0 {
			ui.Textf(c, tr("on_count"), n).Font("monospace").FontSize(11).TextColor(pal.T3)
		}
		ui.Spacer(c)
		b := p.iconButton(c, "btn-search", "search", tr("search_tip"), p.searching)
		if b.Clicked() {
			p.searching = !p.searching
			p.query = ""
		}
	})
}

func (p *panel) iconButton(c *ui.Context, key, name, tip string, active bool) ui.Element {
	pal := p.pal
	b := ui.Row(c.Key(key)).Size(26, 26).Radius(7).AlignItems(ui.Center).Justify(ui.Center).Tooltip(tip).Label(tip)
	if active || b.Hovered() {
		b.Background(pal.L2)
	}
	color := pal.T2
	if active || b.Hovered() {
		color = pal.T1
	}
	b.Children(func() { icon(c, name, 14).TextColor(color) })
	return b
}

func (p *panel) searchField(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(0, 12, 8).Children(func() {
		f := ui.SearchField(c.Key("search-field"), &p.query).FillWidth()
		f.Label(tr("search"))
	})
}

func (p *panel) axBanner(c *ui.Context) {
	pal := p.pal
	ui.Row(c).FillWidth().Margin(0, 12, 8).Padding(8, 10).Radius(10).Gap(8).AlignItems(ui.Center).
		Background(alpha(pal.Amber, 0.12)).Children(func() {
		icon(c, "keyboard", 14).TextColor(pal.Amber)
		ui.Text(c, tr("ax_title")).FontSize(11.5).TextColor(pal.T1).Grow(1).Wrap()
		if ui.Button(c.Key("ax-grant"), tr("ax_button")).Clicked() && p.requestAX != nil {
			p.requestAX()
		}
	})
}

type group struct {
	name  string // 空 = 不显示分组标题
	views []engine.View
}

// groups 按 meta.Group 分组；没有任何分组信息时返回单个无标题分组。
func (p *panel) groups(list []engine.View) []group {
	if !p.prefs.ShowGroups || p.query != "" {
		return []group{{"", list}}
	}
	var order []string
	byGroup := map[string][]engine.View{}
	hasGroup := false
	for _, v := range list {
		g := v.Meta.Group
		if g != "" {
			hasGroup = true
		}
		if _, ok := byGroup[g]; !ok {
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], v)
	}
	if !hasGroup {
		return []group{{"", list}}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i] != "" && order[j] == "" })
	out := make([]group, 0, len(order))
	for _, g := range order {
		name := g
		if name == "" {
			name = tr("other_group")
		}
		out = append(out, group{name, byGroup[g]})
	}
	return out
}

func (p *panel) body(c *ui.Context) {
	pal := p.pal
	list := p.visible()
	ui.Column(c).FillWidth().Padding(4, 6, 6).Gap(0).Children(func() {
		if len(list) == 0 {
			p.empty(c)
			return
		}
		for _, g := range p.groups(list) {
			name, views := g.name, g.views
			if name != "" {
				h := ui.Row(c.Key("grp-"+name)).FillWidth().Padding(10, 10, 4).Gap(6).AlignItems(ui.Center)
				h.Children(func() {
					chev := "chevron-down"
					if p.collapsed[name] {
						chev = "chevron-right"
					}
					icon(c, chev, 11).TextColor(pal.T3)
					ui.Text(c, name).FontSize(11).FontWeight(600).TextColor(pal.T3)
					ui.Spacer(c)
					if p.collapsed[name] {
						on := 0
						for _, v := range views {
							if v.State == engine.On {
								on++
							}
						}
						ui.Textf(c, "%d/%d", on, len(views)).Font("monospace").FontSize(10.5).TextColor(pal.T3)
					}
				})
				if h.Clicked() {
					p.collapsed[name] = !p.collapsed[name]
				}
				if p.collapsed[name] {
					continue
				}
			}
			if p.prefs.Layout == "grid" {
				ui.Grid(c).FillWidth().Columns(2).Gap(6).Padding(4, 6, 6).Children(func() {
					for _, v := range views {
						p.tile(c, v)
					}
				})
				continue
			}
			for _, v := range views {
				p.row(c, v)
			}
		}
	})
}

func (p *panel) empty(c *ui.Context) {
	pal := p.pal
	ui.Column(c).FillWidth().Padding(26, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		if p.query != "" {
			ui.Textf(c, tr("no_match"), p.query).FontSize(12).TextColor(pal.T3)
			return
		}
		ui.Text(c, ">_").Font("monospace").FontSize(19).TextColor(pal.T3)
		ui.Text(c, tr("empty")).FontSize(12.5).TextColor(pal.T1)
		ui.Text(c, tr("empty_hint")).FontSize(11.5).TextColor(pal.T3).Wrap()
		if ui.Button(c.Key("empty-open"), tr("open_scripts")).Clicked() && p.openScripts != nil {
			p.openScripts()
		}
	})
}

// shownOn 是开关当前应显示的开合：刚点过的以点击目标为准，直到状态回来。
func (p *panel) shownOn(v engine.View) bool {
	if t, ok := p.pending[v.ID]; ok {
		return t
	}
	return v.State == engine.On
}

func (p *panel) dot(c *ui.Context, v engine.View) {
	pal := p.pal
	d := ui.Column(c).Size(6, 6).Radius(3)
	switch {
	case v.State == engine.Error || (v.Type == contract.Action && p.lastFailed(v) && p.flashing(v)):
		d.Background(pal.Red)
	case v.State == engine.Running || v.State == engine.Pending:
		d.Background(pal.Accent).Opacity(0.55 + 0.45*float32(math.Abs(math.Sin(float64(time.Now().UnixMilli())/350))))
	case v.State == engine.On:
		d.Background(pal.Accent)
	case v.State == engine.Unknown:
		d.Border(1, pal.L4)
	default:
		d.Background(pal.L4)
	}
}

func (p *panel) lastFailed(v engine.View) bool { return v.LastRun != nil && !v.LastRun.OK }

func (p *panel) flashing(v engine.View) bool {
	t, ok := p.flash[v.ID]
	return ok && time.Now().Before(t)
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return tr("just_now")
	case d < time.Hour:
		return fmt.Sprintf(tr("minutes_ago"), int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf(tr("hours_ago"), int(d.Hours()))
	}
	return fmt.Sprintf(tr("days_ago"), int(d.Hours()/24))
}

func countdown(sec int) string {
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// subline 是名称下方的说明：运行状态、倒计时、参数摘要或上次运行。
func (p *panel) subline(v engine.View) (text string, color ui.Color) {
	pal := p.pal
	color = pal.T3
	switch {
	case v.Type == contract.Action && v.State == engine.Running:
		return fmt.Sprintf("%s %.1fs", tr("running"), v.RunningFor.Seconds()), color
	case v.Type == contract.Action && v.LastRun != nil:
		if p.flashing(v) {
			if v.LastRun.OK {
				out := strings.TrimSpace(lastLine(v.LastRun.Output))
				if out != "" {
					return tr("done") + " · " + out, pal.Green
				}
				return tr("done"), pal.Green
			}
			return fmt.Sprintf(tr("failed_exit"), v.LastRun.ExitCode), pal.Red
		}
		if v.LastRun.Time.IsZero() {
			return "", color // 没有时间的记录（不应出现）：不显示「多少天前」
		}
		if !v.LastRun.OK {
			return ago(v.LastRun.Time) + " · " + tr("failed"), pal.Red
		}
		return fmt.Sprintf(tr("last"), ago(v.LastRun.Time)), color
	case v.State == engine.Pending:
		if t, ok := p.pending[v.ID]; ok && !t {
			return tr("turning_off"), color
		}
		return tr("turning_on"), color
	case v.State == engine.Error:
		return tr("error"), pal.Red
	case v.Type == contract.Daemon && v.State == engine.On:
		if v.Remaining != nil {
			return tr("daemon_running") + " · " + fmt.Sprintf(tr("remaining"), countdown(*v.Remaining)), color
		}
		return tr("daemon_running"), color
	}
	return p.paramSummary(v), color
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}

func (p *panel) paramSummary(v engine.View) string {
	var parts []string
	for _, pv := range v.Params {
		val := pv.Value
		switch pv.Type {
		case contract.ParamSelect:
			for _, o := range pv.Options {
				if o.Value == val {
					val = o.Label
				}
			}
		case contract.ParamNumber, contract.ParamText:
			for _, o := range pv.Presets {
				if o.Value == val {
					val = o.Label
				}
			}
		case contract.ParamKey:
			if s, ok := keys.Parse(val); ok && s.Display() != "" {
				val = s.Display()
			}
		}
		if val != "" {
			parts = append(parts, val)
		}
	}
	return strings.Join(parts, " · ")
}

func (p *panel) row(c *ui.Context, v engine.View) {
	pal := p.pal
	compact := p.prefs.Density == "compact"
	hasParams := len(v.Params) > 0
	open := p.expanded == v.ID
	padY := float32(7)
	if compact {
		padY = 4
	}
	ui.Column(c.Key("item-" + v.ID)).FillWidth().Children(func() {
		r := ui.Row(c.Key("row-"+v.ID)).FillWidth().AlignItems(ui.Center).Gap(10).Padding(padY, 10).Radius(10)
		if open || r.Hovered() {
			r.Background(pal.L1)
		}
		r.Children(func() {
			p.dot(c, v)
			iconColor := pal.T1
			if v.Type != contract.Action && v.State != engine.On {
				iconColor = pal.T2
			}
			ui.Row(c).Width(18).Justify(ui.Center).Children(func() { switchIcon(c, v.Icon, 16).TextColor(iconColor) })
			label := ui.Column(c.Key("label-" + v.ID)).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, v.Name).FontSize(13).TextColor(pal.T1).SingleLine().Ellipsis("…")
				if sub, col := p.subline(v); sub != "" && !compact {
					ui.Text(c, sub).FontSize(11).TextColor(col).SingleLine().Ellipsis("…")
				}
			})
			if hasParams {
				chev := ui.Row(c.Key("chev-"+v.ID)).Size(18, 18).Radius(5).AlignItems(ui.Center).Justify(ui.Center).Label("params")
				if chev.Hovered() {
					chev.Background(pal.L2)
				}
				name := "chevron-right"
				if open {
					name = "chevron-down"
				}
				chev.Children(func() { icon(c, name, 13).TextColor(pal.T3) })
				if chev.Clicked() || label.Clicked() {
					if open {
						p.expanded = ""
					} else {
						p.expanded = v.ID
					}
				}
			}
			p.control(c, v)
		})
		if open {
			p.paramsEditor(c, v)
		}
	})
}

func (p *panel) control(c *ui.Context, v engine.View) {
	pal := p.pal
	if v.Type == contract.Action {
		btn := ui.Row(c.Key("run-"+v.ID)).Height(24).MinWidth(52).Padding(0, 10).Radius(999).Gap(6).
			AlignItems(ui.Center).Justify(ui.Center).Border(1, pal.L3).Label(tr("run") + " " + v.Name)
		switch {
		case v.State == engine.Running:
			btn.Border(1, alpha(pal.Accent, 0.45)).Background(alpha(pal.Accent, 0.08))
			btn.Children(func() {
				ui.Spinner(c).Size(12, 12)
				ui.Textf(c, "%.1fs", v.RunningFor.Seconds()).Font("monospace").FontSize(11.5).TextColor(pal.T1)
			})
		case p.flashing(v) && v.LastRun != nil && v.LastRun.OK:
			btn.Border(1, alpha(pal.Green, 0.4))
			btn.Children(func() {
				icon(c, "check", 13).TextColor(pal.Green)
				ui.Text(c, tr("done")).FontSize(12).TextColor(pal.Green)
			})
		case p.flashing(v) && v.LastRun != nil:
			btn.Border(1, alpha(pal.Red, 0.4))
			btn.Children(func() { icon(c, "x", 13).TextColor(pal.Red); ui.Text(c, tr("failed")).FontSize(12).TextColor(pal.Red) })
		case p.isArmed(v.ID):
			btn.Border(1, alpha(pal.Amber, 0.5)).Background(alpha(pal.Amber, 0.10))
			btn.Children(func() { ui.Text(c, tr("confirm_short")).FontSize(12).FontWeight(500).TextColor(pal.Amber) })
		default:
			if btn.Hovered() {
				btn.Background(pal.L1)
			}
			btn.Children(func() {
				icon(c, "play", 11).TextColor(pal.T1)
				if p.prefs.Density != "compact" {
					ui.Text(c, tr("run")).FontSize(12).FontWeight(500).TextColor(pal.T1)
				}
			})
		}
		if btn.Clicked() && v.State != engine.Running {
			p.press(v)
		}
		return
	}
	on := p.shownOn(v)
	before := on
	ui.Switch(c.Key("sw-"+v.ID), &on).Label(v.Name)
	if on != before {
		p.setSwitch(v, on)
	}
}

func (p *panel) setSwitch(v engine.View, on bool) {
	p.pending[v.ID] = on
	if _, err := p.b.SetSwitch(v.ID, on, engine.Manual, ""); err != nil {
		delete(p.pending, v.ID)
	}
	p.dirty = true
}

// press 是点击按钮型开关：设置了「运行前确认」时第一次点击只进入确认状态（3 秒内再点才运行）。
func (p *panel) press(v engine.View) {
	if v.Meta.Confirm && !p.isArmed(v.ID) {
		p.armed[v.ID] = time.Now().Add(3 * time.Second)
		return
	}
	delete(p.armed, v.ID)
	p.run(v)
}

func (p *panel) isArmed(id string) bool {
	t, ok := p.armed[id]
	return ok && time.Now().Before(t)
}

func (p *panel) run(v engine.View) {
	ch, err := p.b.Run(v.ID, engine.Manual, "")
	if err != nil {
		return
	}
	p.dirty = true
	go func() {
		if _, ok := <-ch; ok {
			p.markFlash(v.ID)
		}
	}()
}

// markFlash 由后台 goroutine 调用；flash 在主线程读，所以由界面层通过 Update 回到主线程。
var markFlashOnMain = func(fn func()) { fn() }

func (p *panel) markFlash(id string) {
	markFlashOnMain(func() {
		p.flash[id] = time.Now().Add(2600 * time.Millisecond)
		p.dirty = true
	})
}

// ───────── 参数编辑 ─────────

var commonKeys = []string{"f13", "f14", "f15", "f16", "f17", "f18", "f19", "f20", "mouse:middle", "mouse:right"}

func (p *panel) commit(v engine.View, key, value string) {
	if err := p.b.SetParams(v.ID, map[string]string{key: value}, engine.Manual); err == nil {
		p.dirty = true
	}
}

func (p *panel) paramsEditor(c *ui.Context, v engine.View) {
	pal := p.pal
	ui.Column(c.Key("params-"+v.ID)).FillWidth().Padding(4, 10, 12, 44).Gap(10).Children(func() {
		for _, pv := range v.Params {
			pv := pv
			ui.Column(c.Key("param-" + v.ID + "-" + pv.Key)).FillWidth().Gap(5).Children(func() {
				ui.Row(c).FillWidth().Children(func() {
					ui.Text(c, pv.Label).FontSize(11).TextColor(pal.T3)
					ui.Spacer(c)
					ui.Text(c, contract.EnvName(pv.Key)).Font("monospace").FontSize(10).TextColor(pal.T3)
				})
				switch pv.Type {
				case contract.ParamSelect:
					p.selectParam(c, v, pv)
				case contract.ParamNumber:
					p.numberParam(c, v, pv)
				case contract.ParamKey:
					p.keyParam(c, v, pv)
				default:
					p.textParam(c, v, pv)
				}
			})
		}
	})
}

func (p *panel) selectParam(c *ui.Context, v engine.View, pv engine.ParamView) {
	labels := make([]string, len(pv.Options))
	sel := 0
	for i, o := range pv.Options {
		labels[i] = o.Label
		if o.Value == pv.Value {
			sel = i
		}
	}
	before := sel
	ui.Segmented(c.Key("seg"), &sel, labels...).FillWidth()
	if sel != before && sel < len(pv.Options) {
		p.commit(v, pv.Key, pv.Options[sel].Value)
	}
}

func (p *panel) chip(c *ui.Context, key, label string, active bool) bool {
	pal := p.pal
	ch := ui.Row(c.Key(key)).Height(22).Padding(0, 8).Radius(999).AlignItems(ui.Center)
	if active {
		if pal.Ink {
			ch.Background(pal.FG)
		} else {
			ch.Background(pal.Accent)
		}
	} else {
		ch.Border(1, pal.L3)
		if ch.Hovered() {
			ch.Background(pal.L1)
		}
	}
	ch.Children(func() {
		col := pal.T1
		if active {
			col = pal.OnAccent
		}
		ui.Text(c, label).FontSize(11.5).TextColor(col).SingleLine()
	})
	return ch.Clicked()
}

func (p *panel) numberParam(c *ui.Context, v engine.View, pv engine.ParamView) {
	ui.Row(c).FillWidth().Gap(5).Wrap().AlignItems(ui.Center).Children(func() {
		for i, o := range pv.Presets {
			if p.chip(c, "preset-"+strconv.Itoa(i), o.Label, o.Value == pv.Value) {
				p.commit(v, pv.Key, o.Value)
			}
		}
		key := v.ID + "/" + pv.Key
		text, editing := p.edits[key]
		if !editing {
			text = pv.Value
		}
		in := ui.TextInput(c.Key("num"), &text).Width(72).Font("monospace").FontSize(11.5).Label(pv.Label)
		if text != pv.Value || editing {
			p.edits[key] = text
		}
		submit := func() {
			delete(p.edits, key)
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil {
				return
			}
			if pv.Min != nil && n < *pv.Min {
				n = *pv.Min
			}
			if pv.Max != nil && n > *pv.Max {
				n = *pv.Max
			}
			p.commit(v, pv.Key, strconv.Itoa(n))
		}
		in.OnSubmit(submit)
	})
}

func (p *panel) textParam(c *ui.Context, v engine.View, pv engine.ParamView) {
	key := v.ID + "/" + pv.Key
	text, editing := p.edits[key]
	if !editing {
		text = pv.Value
	}
	ui.Row(c).FillWidth().Gap(5).Wrap().Children(func() {
		for i, o := range pv.Presets {
			if p.chip(c, "preset-"+strconv.Itoa(i), o.Label, o.Value == pv.Value) {
				p.commit(v, pv.Key, o.Value)
			}
		}
	})
	in := ui.TextInput(c.Key("text"), &text).FillWidth().Font("monospace").FontSize(12).Label(pv.Label)
	if pv.Hint != "" {
		in.Placeholder(pv.Hint)
	}
	if text != pv.Value || editing {
		p.edits[key] = text
	}
	in.OnSubmit(func() {
		delete(p.edits, key)
		p.commit(v, pv.Key, text)
	})
}

func (p *panel) keyParam(c *ui.Context, v engine.View, pv engine.ParamView) {
	options := append([]string{}, commonKeys...)
	found := false
	for _, k := range options {
		if k == pv.Value {
			found = true
		}
	}
	if !found && pv.Value != "" {
		options = append([]string{pv.Value}, options...)
	}
	sel := pv.Value
	ui.Select(c.Key("key"), &sel, options).Width(160).Label(pv.Label)
	if sel != pv.Value {
		p.commit(v, pv.Key, sel)
	}
}

// ───────── 网格 ─────────

func (p *panel) tile(c *ui.Context, v engine.View) {
	pal := p.pal
	on := p.shownOn(v)
	t := ui.Column(c.Key("tile-"+v.ID)).MinHeight(74).Padding(10, 11).Radius(12).Justify(ui.SpaceBetween)
	switch {
	case on && v.Type != contract.Action && pal.Ink:
		t.Background(pal.FG)
	case on && v.Type != contract.Action:
		t.Background(alpha(pal.Accent, 0.12)).Border(1, alpha(pal.Accent, 0.45))
	case v.State == engine.Running:
		t.Background(pal.L1).Border(1, alpha(pal.Accent, 0.5))
	default:
		t.Background(pal.L1).Border(1, pal.L2)
		if t.Hovered() {
			t.Background(pal.L2)
		}
	}
	fg, sub := pal.T1, pal.T3
	if on && v.Type != contract.Action && pal.Ink {
		fg, sub = pal.BG, alpha(pal.BG, 0.7)
	}
	t.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ic := fg
			if on && !pal.Ink && v.Type != contract.Action {
				ic = pal.Accent
			}
			switchIcon(c, v.Icon, 17).TextColor(ic)
			ui.Spacer(c)
			switch {
			case v.State == engine.Running || v.State == engine.Pending:
				ui.Spinner(c).Size(12, 12)
			case v.Type == contract.Action:
				icon(c, "play", 12).TextColor(sub)
			}
		})
		ui.Column(c).FillWidth().Gap(1).Children(func() {
			ui.Text(c, v.Name).FontSize(13).FontWeight(500).TextColor(fg).SingleLine().Ellipsis("…")
			s, col := p.subline(v)
			if s == "" {
				switch {
				case v.Type == contract.Action:
					s = tr("run")
				case on:
					s = "ON"
				default:
					s = "OFF"
				}
				col = sub
			} else if col == pal.T3 {
				col = sub
			}
			ui.Text(c, s).FontSize(11).TextColor(col).SingleLine().Ellipsis("…")
		})
	})
	if t.Clicked() {
		if v.Type == contract.Action {
			if v.State != engine.Running {
				p.press(v)
			}
		} else {
			p.setSwitch(v, !on)
		}
	}
}

// ───────── 底栏 ─────────

func (p *panel) footer(c *ui.Context) {
	pal := p.pal
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(2).Padding(6, 8).Children(func() {
		wb := ui.Row(c.Key("open-workbench")).Height(26).Padding(0, 8).Radius(7).Gap(6).AlignItems(ui.Center).Label(tr("workbench"))
		if wb.Hovered() {
			wb.Background(pal.L2)
		}
		wb.Children(func() {
			icon(c, "square-terminal", 14).TextColor(pal.T2)
			ui.Text(c, tr("workbench")).FontSize(12.5).TextColor(pal.T2)
		})
		if wb.Clicked() && p.openWorkbench != nil {
			p.openWorkbench("")
		}
		ui.Spacer(c)
		if p.iconButton(c, "btn-folder", "folder", tr("open_scripts"), false).Clicked() && p.openScripts != nil {
			p.openScripts()
		}
		if p.iconButton(c, "btn-settings", "settings", tr("settings"), false).Clicked() && p.openSettings != nil {
			p.openSettings()
		}
		if p.iconButton(c, "btn-refresh", "rotate-cw", tr("refresh"), false).Clicked() {
			p.b.Refresh()
		}
		if p.iconButton(c, "btn-quit", "power", tr("quit"), false).Clicked() && p.quit != nil {
			p.quit()
		}
	})
}

// sceneBar 是面板顶部的场景栏：glass 材质的胶囊，激活中的用强调色。
func (p *panel) sceneBar(c *ui.Context) {
	ui.ScrollHorizontal(c.Key("scenes")).FillWidth().Padding(2, 12, 10).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			for _, sv := range p.scenes {
				sv := sv
				chip := p.sceneChip(c, "scene-"+sv.ID, sv, sv.Active)
				if chip.Clicked() && !p.sceneBusy[sv.ID] {
					p.toggleScene(sv)
				}
			}
		})
	})
}

func (p *panel) toggleScene(sv engine.SceneView) {
	ch, err := p.b.ToggleScene(sv.ID, engine.Manual, "")
	if err != nil {
		return
	}
	p.sceneBusy[sv.ID] = true
	go func() {
		<-ch
		markFlashOnMain(func() {
			delete(p.sceneBusy, sv.ID)
			p.dirty = true
		})
	}()
}

func (p *panel) sceneChip(c *ui.Context, key string, sv engine.SceneView, active bool) ui.Element {
	pal := p.pal
	tip := sv.Name
	if sv.Shortcut != "" {
		tip += "  " + automation.ShortcutSymbols(sv.Shortcut)
	}
	chip := ui.Row(c.Key(key)).Height(26).Padding(0, 11).Radius(999).Gap(6).AlignItems(ui.Center).Tooltip(tip).Label(sv.Name)
	if active {
		if pal.Ink {
			chip.Material(glass.Glass{Tint: pal.FG, Interactive: true})
		} else {
			chip.Material(glass.Glass{Tint: pal.Accent, Interactive: true})
		}
	} else {
		chip.Material(glass.Glass{Interactive: true})
	}
	if p.sceneBusy[sv.ID] {
		chip.Opacity(0.6)
	}
	chip.Children(func() {
		col := pal.T1
		if active {
			col = pal.OnAccent
		}
		if sv.Icon != "" {
			switchIcon(c, sv.Icon, 13).TextColor(col)
		}
		ui.Text(c, sv.Name).FontSize(12).FontWeight(500).TextColor(col).SingleLine()
	})
	return chip
}
