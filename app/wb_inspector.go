package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
)

// 检查器：契约头里的字段（名称、图标、类型、描述、参数、菜单栏）直接改写脚本文本，
// 编辑器里改了文本，这里下一帧就按新内容显示——双向同步只有一份数据：缓冲区。
// 「用户怎么用它」的设置（分组、确认、超时、隐藏）存在 app 里，立即生效。

// field 是绑定到派生值的文本输入：输入时保留用户的原样文字（哪怕规范化后不同），
// 值在别处被修改时（例如在编辑器里改了脚本）再跟上新值。commit 在每次输入时调用。
func (w *workbench) field(c *ui.Context, key, value, placeholder string, mono bool, commit func(string)) ui.Element {
	fs := w.fields[key]
	switch {
	case fs == nil:
		fs = &fieldState{src: value, text: value}
		w.fields[key] = fs
	case fs.pending:
		fs.src, fs.pending = value, false
	case value != fs.src:
		fs.src, fs.text = value, value
	}
	before := fs.text
	in := ui.TextInput(c.Key(key), &fs.text).FillWidth().FontSize(12.5).Label(placeholder)
	if placeholder != "" {
		in.Placeholder(placeholder)
	}
	if mono {
		in.Font("monospace").FontSize(12)
	}
	if fs.text != before {
		fs.pending = true
		commit(fs.text)
	}
	return in
}

func (w *workbench) inspSection(c *ui.Context, key, title string, trailing func(), body func()) {
	pal := w.pal
	ui.Column(c.Key(key)).FillWidth().Padding(14, 16).Gap(10).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			pal.caption(c, title).Grow(1)
			if trailing != nil {
				trailing()
			}
		})
		body()
	})
	pal.hair(c)
}

func (w *workbench) labeled(c *ui.Context, label string, fn func()) {
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(w.pal.T2)
		fn()
	})
}

func (w *workbench) inspectorView(c *ui.Context, d *doc, v *engine.View) {
	h := d.header
	text := d.text()
	set := func(directive, value string) { d.apply(contract.SetDirective(d.text(), directive, value)) }
	key := func(s string) string { return d.id + "/" + s }

	w.inspSection(c, "s-basic", tr("sec_basic"), nil, func() {
		w.labeled(c, tr("f_name"), func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				w.iconPicker(c, d)
				w.field(c, key("name"), h.Name, tr("f_name"), false, func(s string) {
					if strings.TrimSpace(s) == "" {
						s = " " // 保留这一行，避免指令位置跳动；检查会提示缺少名称
					}
					set("name", s)
				})
			})
		})
		w.labeled(c, tr("f_type"), func() {
			types := []contract.Type{contract.Toggle, contract.Daemon, contract.Action}
			cur := 0
			for i, t := range types {
				if h.Type == t {
					cur = i
				}
			}
			if n := w.pal.segmented(c, "type-seg", cur, tr("type_toggle"), tr("type_daemon"), tr("type_action")); n != cur {
				set("type", string(types[n]))
			}
			w.pal.hint(c, tr("hint_"+string(h.Type)))
		})
		w.labeled(c, tr("f_desc"), func() {
			w.field(c, key("desc"), h.Description, tr("desc_ph"), false, func(s string) { set("description", strings.TrimSpace(s)) })
		})
		if v != nil {
			w.labeled(c, tr("f_group"), func() { w.groupField(c, *v) })
		}
	})

	w.inspSection(c, "s-params", tr("sec_params"), func() { w.pal.mono(c, itoa(len(h.Params)), 11, w.pal.T3) }, func() {
		w.paramsView(c, d)
	})

	w.inspSection(c, "s-menubar", tr("sec_menubar"), nil, func() { w.menubarView(c, d) })

	if v != nil {
		w.inspSection(c, "s-behavior", tr("sec_behavior"), nil, func() { w.behaviorView(c, *v) })
	}
	_ = text
}

// ───────── 图标 ─────────

func (w *workbench) iconPicker(c *ui.Context, d *doc) {
	pal := w.pal
	btn := ui.Row(c.Key("icon-pick")).Size(34, 28).Shrink(0).Radius(7).Border(1, pal.L3).AlignItems(ui.Center).Justify(ui.Center).
		Tooltip(tr("f_icon")).Label(tr("f_icon"))
	if btn.Hovered() || w.iconOpen {
		btn.Background(pal.L1)
	}
	btn.Children(func() { switchIcon(c, d.header.Icon, 15).TextColor(pal.T1) })
	if btn.Clicked() {
		w.iconOpen = !w.iconOpen
		w.iconQuery, w.emoji = "", ""
	}
	ui.PopoverBase(c, btn, &w.iconOpen, func(panel ui.Element) {
		w.menuPanel(panel)
		panel.Width(300).Padding(8).Gap(8)
		panel.Children(func() {
			ui.SearchField(c.Key("icon-q"), &w.iconQuery).FillWidth().Placeholder(tr("icon_search")).AutoFocus()
			names := iconMatches(w.iconQuery, 64)
			ui.Grid(c.Key("icon-grid")).FillWidth().Columns(8).Gap(2).Children(func() {
				for _, n := range names {
					cell := ui.Row(c.Key("ic-" + n)).Height(30).Radius(6).AlignItems(ui.Center).Justify(ui.Center).Tooltip(n).Label(n)
					if cell.Hovered() {
						cell.Background(pal.L2)
					}
					cell.Children(func() { icon(c, n, 15).TextColor(pal.T1) })
					if cell.Clicked() {
						d.apply(contract.SetDirective(d.text(), "icon", "lucide:"+n))
						w.iconOpen = false
					}
				}
			})
			before := w.emoji
			ui.TextInput(c.Key("emoji"), &w.emoji).FillWidth().Placeholder(tr("icon_emoji")).FontSize(13)
			if w.emoji != before && strings.TrimSpace(w.emoji) != "" && !isASCII(w.emoji) {
				d.apply(contract.SetDirective(d.text(), "icon", strings.TrimSpace(w.emoji)))
				w.iconOpen = false
			}
		})
	})
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// commonIcons 是搜索框为空时展示的常用图标。
var commonIcons = []string{
	"moon", "sun", "coffee", "bell-off", "wifi", "globe", "server", "zap", "trash-2", "eye", "eye-off", "volume-x",
	"mic", "mic-off", "monitor", "keyboard", "timer", "rocket", "terminal", "database", "cloud", "lock", "shield", "bluetooth",
	"battery", "camera", "music", "bell", "bot", "plug", "power", "sparkles", "toggle-right", "play", "repeat", "rotate-cw",
	"copy", "container", "folder", "download", "upload", "network", "router", "cpu", "hard-drive", "printer", "headphones", "flame",
	"fish", "target", "layers", "git-branch", "code", "bug", "box", "package", "cast", "airplay", "settings", "wrench", "key", "dock",
}

func iconMatches(q string, limit int) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		out := make([]string, 0, len(commonIcons))
		for _, n := range commonIcons {
			if HasIcon(n) {
				out = append(out, n)
			}
		}
		return out
	}
	var prefix, contains []string
	for n := range lucide() {
		switch {
		case strings.HasPrefix(n, q):
			prefix = append(prefix, n)
		case strings.Contains(n, q):
			contains = append(contains, n)
		}
	}
	sort.Strings(prefix)
	sort.Strings(contains)
	out := append(prefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ───────── 分组 ─────────

func (w *workbench) groupField(c *ui.Context, v engine.View) {
	pal := w.pal
	k := v.ID + "/group"
	fs := w.fields[k]
	if fs == nil || (!fs.pending && fs.src != v.Meta.Group) {
		fs = &fieldState{src: v.Meta.Group, text: v.Meta.Group}
		w.fields[k] = fs
	}
	commit := func(g string) {
		m := v.Meta
		m.Group = strings.TrimSpace(g)
		_ = w.b.SetMeta(v.ID, m)
		fs.src, fs.text, fs.pending = m.Group, m.Group, false
		w.dirty = true
	}
	ui.TextInput(c.Key("group"), &fs.text).FillWidth().FontSize(12.5).Placeholder(tr("group_ph")).Label(tr("f_group")).
		OnSubmit(func() { commit(fs.text) })
	if fs.text != fs.src {
		fs.pending = true
	}
	// 已有的分组：点一下直接选用
	seen := map[string]bool{}
	var groups []string
	for _, x := range w.views {
		if g := x.Meta.Group; g != "" && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	if len(groups) == 0 {
		return
	}
	ui.Row(c).FillWidth().Gap(5).Wrap().Children(func() {
		for _, g := range groups {
			g := g
			active := g == v.Meta.Group
			ch := ui.Row(c.Key("g-"+g)).Height(22).Padding(0, 8).Radius(999).AlignItems(ui.Center)
			if active {
				ch.Background(pal.L3)
			} else {
				ch.Border(1, pal.L3)
				if ch.Hovered() {
					ch.Background(pal.L1)
				}
			}
			ch.Children(func() { ui.Text(c, g).FontSize(11.5).TextColor(pal.T1).SingleLine() })
			if ch.Clicked() {
				if active {
					commit("")
				} else {
					commit(g)
				}
			}
		}
	})
}

// ───────── 参数 ─────────

var paramTypes = []contract.ParamType{contract.ParamSelect, contract.ParamNumber, contract.ParamText, contract.ParamKey}

func (w *workbench) paramsView(c *ui.Context, d *doc) {
	pal := w.pal
	params := d.header.Params
	if len(params) == 0 {
		pal.hint(c, tr("params_hint"))
	}
	write := func(ps []contract.Param) { d.apply(contract.SetParams(d.text(), ps)) }
	for i, p := range params {
		i, p := i, p
		open := w.paramOpen == i
		box := ui.Column(c.Key("param-"+itoa(i))).FillWidth().Radius(8).Border(1, pal.L2)
		box.Children(func() {
			row := ui.Row(c.Key("prow")).FillWidth().Padding(7, 9).Gap(8).AlignItems(ui.Center).Label(p.Key)
			if row.Hovered() {
				row.Background(pal.L1)
			}
			row.Children(func() {
				pal.mono(c, p.Key, 11.5, pal.T1)
				ui.Text(c, p.Label).FontSize(12).TextColor(pal.T3).SingleLine().Grow(1).MinWidth(0)
				pal.badge(c, tr("pt_"+string(p.Type)), "")
				name := "chevron-right"
				if open {
					name = "chevron-down"
				}
				icon(c, name, 13).TextColor(pal.T3)
			})
			if row.Clicked() {
				if open {
					w.paramOpen = -1
				} else {
					w.paramOpen = i
				}
			}
			if open {
				pal.hair(c)
				ui.Column(c.Key("pform")).FillWidth().Padding(10, 10, 12).Gap(8).Children(func() {
					w.paramForm(c, d, i, p, params, write)
				})
			}
		})
	}
	if b := pal.button(c, "add-param", tr("add_param"), "plus", btnGhost, false); b.Clicked() {
		n := len(params) + 1
		k := "param" + itoa(n)
		for _, ok := d.header.Param(k); ok; _, ok = d.header.Param(k) {
			n++
			k = "param" + itoa(n)
		}
		write(append(append([]contract.Param{}, params...), contract.Param{Key: k, Label: k, Type: contract.ParamText}))
		w.paramOpen = len(params)
	}
}

func (w *workbench) paramForm(c *ui.Context, d *doc, i int, p contract.Param, params []contract.Param, write func([]contract.Param)) {
	pal := w.pal
	k := func(s string) string { return d.id + "/p" + itoa(i) + "/" + s }
	update := func(fn func(p *contract.Param)) {
		ps := append([]contract.Param{}, params...)
		fn(&ps[i])
		write(ps)
	}
	two := func(a, b func()) {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(5).Children(a)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(5).Children(b)
		})
	}
	lbl := func(s string) { ui.Text(c, s).FontSize(11).TextColor(pal.T2) }
	two(func() {
		lbl(tr("p_key"))
		w.field(c, k("key"), p.Key, "key", true, func(s string) { update(func(p *contract.Param) { p.Key = strings.TrimSpace(s) }) })
	}, func() {
		lbl(tr("p_label"))
		w.field(c, k("label"), p.Label, p.Key, false, func(s string) { update(func(p *contract.Param) { p.Label = s }) })
	})
	lbl(tr("p_type"))
	cur := 2
	for j, t := range paramTypes {
		if p.Type == t {
			cur = j
		}
	}
	if n := pal.segmented(c, "ptype", cur, tr("pt_select"), tr("pt_number"), tr("pt_text"), tr("pt_key")); n != cur {
		update(func(p *contract.Param) {
			p.Type = paramTypes[n]
			if p.Type == contract.ParamSelect && len(p.Options) == 0 {
				p.Options = []contract.Option{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}}
				p.Default = "a"
			}
			if p.Type == contract.ParamNumber {
				if _, err := strconv.Atoi(p.Default); err != nil {
					p.Default = "0"
				}
			}
			if p.Type == contract.ParamKey {
				p.Default = "f15"
			}
		})
	}
	if p.Type == contract.ParamSelect {
		lbl(tr("p_options"))
		w.field(c, k("options"), contract.FormatOptions(p.Options), tr("p_options_ph"), true, func(s string) {
			update(func(p *contract.Param) { p.Options = contract.ParseOptions(s) })
		})
	}
	two(func() {
		lbl(tr("p_default"))
		w.field(c, k("default"), p.Default, "", true, func(s string) { update(func(p *contract.Param) { p.Default = strings.TrimSpace(s) }) })
	}, func() {
		if p.Type == contract.ParamNumber || p.Type == contract.ParamText {
			lbl(tr("p_presets"))
			w.field(c, k("presets"), contract.FormatOptions(p.Presets), tr("p_options_ph"), true, func(s string) {
				update(func(p *contract.Param) { p.Presets = contract.ParseOptions(s) })
			})
		}
	})
	if p.Type == contract.ParamNumber {
		num := func(v *int) string {
			if v == nil {
				return ""
			}
			return strconv.Itoa(*v)
		}
		parse := func(s string) *int {
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return nil
			}
			return &n
		}
		two(func() {
			lbl(tr("p_min"))
			w.field(c, k("min"), num(p.Min), "", true, func(s string) { update(func(p *contract.Param) { p.Min = parse(s) }) })
		}, func() {
			lbl(tr("p_max"))
			w.field(c, k("max"), num(p.Max), "", true, func(s string) { update(func(p *contract.Param) { p.Max = parse(s) }) })
		})
	}
	lbl(tr("p_hint"))
	w.field(c, k("hint"), p.Hint, "", false, func(s string) { update(func(p *contract.Param) { p.Hint = s }) })
	ui.Row(c).FillWidth().Children(func() {
		ui.Spacer(c)
		if pal.button(c, "p-remove", tr("p_remove"), "trash-2", btnDanger, false).Clicked() {
			ps := append(append([]contract.Param{}, params[:i]...), params[i+1:]...)
			w.paramOpen = -1
			write(ps)
		}
	})
	pal.mono(c, "$"+p.EnvName(), 11, pal.T3)
}

// ───────── 菜单栏 ─────────

func (w *workbench) menubarView(c *ui.Context, d *doc) {
	pal := w.pal
	h := d.header
	mode := 0
	if h.MenuBar != nil {
		mode = 1
		if h.MenuBar.Mode == contract.MenuBarReplace {
			mode = 2
		}
	}
	write := func(m *contract.MenuBar) {
		if m == nil {
			d.apply(contract.SetDirective(d.text(), "menubar", ""))
			return
		}
		d.apply(contract.SetDirective(d.text(), "menubar", contract.FormatMenuBar(*m)))
	}
	if n := pal.segmented(c, "mb-seg", mode, tr("mb_none"), tr("mb_add"), tr("mb_replace")); n != mode {
		switch n {
		case 0:
			write(nil)
		default:
			m := contract.MenuBar{Mode: contract.MenuBarAdd, Icon: h.Icon}
			if h.MenuBar != nil {
				m = *h.MenuBar
			}
			if n == 2 {
				m.Mode = contract.MenuBarReplace
			} else {
				m.Mode = contract.MenuBarAdd
			}
			if m.Icon == "" || m.Icon == contract.DefaultIcon {
				m.Icon = h.Icon
			}
			write(&m)
		}
	}
	pal.hint(c, tr([]string{"mb_hint_none", "mb_hint_add", "mb_hint_replace"}[mode]))
	if h.MenuBar == nil {
		return
	}
	cd := h.MenuBar.Countdown
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("mb_countdown")).FontSize(12.5).TextColor(pal.T1).Grow(1)
		before := cd
		ui.Switch(c.Key("mb-cd"), &cd).Label(tr("mb_countdown"))
		if cd != before {
			m := *h.MenuBar
			m.Countdown = cd
			write(&m)
		}
	})
	if cd {
		if p, ok := h.Param("duration"); !ok || p.Type != contract.ParamNumber {
			pal.hint(c, tr("mb_need_duration"))
		}
	}
}

// ───────── 行为 ─────────

var timeouts = []int{0, 10, 60, 120, 600}

func (w *workbench) behaviorView(c *ui.Context, v engine.View) {
	pal := w.pal
	row := func(label string, ctl func()) {
		ui.Row(c).FillWidth().MinHeight(26).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, label).FontSize(12.5).TextColor(pal.T1).Grow(1)
			ctl()
		})
	}
	if v.Type == contract.Action {
		row(tr("b_confirm"), func() {
			on := v.Meta.Confirm
			ui.Switch(c.Key("b-confirm"), &on).Label(tr("b_confirm"))
			if on != v.Meta.Confirm {
				m := v.Meta
				m.Confirm = on
				_ = w.b.SetMeta(v.ID, m)
				w.dirty = true
			}
		})
	}
	if v.Type != contract.Daemon {
		row(tr("b_timeout"), func() {
			labels := []string{tr("t_default"), tr("t_10"), tr("t_60"), tr("t_120"), tr("t_600")}
			cur := labels[0]
			for i, t := range timeouts {
				if t == v.Meta.Timeout {
					cur = labels[i]
				}
			}
			sel := cur
			ui.Select(c.Key("b-timeout"), &sel, labels).Width(130).Label(tr("b_timeout"))
			if sel != cur {
				for i, l := range labels {
					if l == sel {
						m := v.Meta
						m.Timeout = timeouts[i]
						_ = w.b.SetMeta(v.ID, m)
						w.dirty = true
					}
				}
			}
		})
	}
	row(tr("b_hidden"), func() {
		hidden := v.Hidden
		ui.Switch(c.Key("b-hidden"), &hidden).Label(tr("b_hidden"))
		if hidden != v.Hidden {
			_ = w.b.SetHidden(v.ID, hidden)
			w.dirty = true
		}
	})
}
