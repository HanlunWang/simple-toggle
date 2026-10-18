package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/integrations"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 设置窗口：左侧九个分区，右侧分组卡片（designs/simpletoggle-v2/settings.jsx）。
// 只放真正生效的选项；读写都经 settingsHost，由界面层负责让改动立即生效。

// settingsHost 是设置窗口对外的全部依赖（测试里用替身）。
type settingsHost struct {
	get   func(key string) string
	set   func(key, value string) // 保存并立即生效
	prefs func() Prefs

	views     func() []engine.View
	setHidden func(id string, hidden bool)
	setOrder  func(ids []string)
	scenes    func() []engine.SceneView
	auto      Automation
	autoOn    func() bool
	setAutoOn func(bool)

	apiAddr    string
	apiRunning func() bool
	setAPI     func(on bool) error
	token      func() string
	regenToken func() error

	clients      func() []integrations.Status
	plan         func(client string) (integrations.Plan, error)
	apply        func(p integrations.Plan) error
	snippet      string
	cliPath      func() string
	skill        func() (installed bool, target string)
	canSkill     bool
	installSkill func() error

	loginItem    func() bool
	setLoginItem func(bool) error
	axTrusted    func() bool
	requestAX    func()
	location     func() int // -1 不可用，0 未请求，1 拒绝 / 受限，2 已授权
	requestLoc   func()
	openSys      func(pane string)

	version     string
	updater     bool
	checkUpdate func(done func(latest string, err error))

	scriptsDir   func() string
	chooseDir    func(done func(path string))
	reveal       func(path string)
	relaunch     func()
	exportConfig func(done func(path string, err error))
	importConfig func(done func(scripts, scenes, rules int, err error))
	reset        func()

	copy          func(text string)
	openWorkbench func(section, item string)
	editors       func() []editorChoice
	shortcutIssue func(key string) string // 全局快捷键（打开面板 / 工作台）注册失败的原因
	onMain        func(fn func())
}

type editorChoice struct{ ID, Name string }

var settingsTabs = []struct{ id, iconName string }{
	{"general", "settings-2"}, {"appearance", "palette"}, {"menubar", "panel-top"}, {"shortcuts", "keyboard"},
	{"automation", "zap"}, {"ai", "bot"}, {"privacy", "shield-check"}, {"updates", "refresh-cw"}, {"advanced", "sliders-horizontal"},
}

type settingsWin struct {
	h     *settingsHost
	pal   palette
	prefs Prefs
	tab   string
	rec   recorder

	notice    string
	noticeErr bool
	noticeAt  time.Time
	copiedAt  time.Time

	plan       *integrations.Plan
	planOpen   bool
	regenOpen  bool
	resetOpen  bool
	relaunchTo string
	relaunchOn bool
	updateMsg  string
	checking   bool
	envName    string
	envValue   string
}

func newSettingsWin(h *settingsHost) *settingsWin {
	if h.onMain == nil {
		h.onMain = func(fn func()) { fn() }
	}
	return &settingsWin{h: h, tab: "general"}
}

func (s *settingsWin) setNotice(msg string, isErr bool) {
	s.notice, s.noticeErr, s.noticeAt = msg, isErr, time.Now()
}

func (s *settingsWin) render(c *ui.Context) {
	s.prefs = s.h.prefs()
	base := c.Theme()
	s.pal = newPalette(base.Dark, s.prefs.Accent)
	c.SetTheme(s.pal.theme(base))
	pal := s.pal
	c.Root().Background(ui.Transparent)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c.Key("st-side")).Width(200).Shrink(0).FillHeight()
		if c.Vibrancy() {
			side.Background(alpha(pal.Card, 0.55))
		} else {
			side.Background(pal.Card)
		}
		side.Children(func() {
			bar := c.TitleBar()
			ui.Row(c).FillWidth().Height(max(bar.Height, 46)).Shrink(0).DragWindow()
			ui.Column(c).FillWidth().Padding(0, 8).Gap(1).Children(func() {
				for _, t := range settingsTabs {
					t := t
					if pal.navItem(c, "tab-"+t.id, t.iconName, tr("st_"+t.id), s.tab == t.id, false, nil).Clicked() {
						s.tab = t.id
					}
				}
			})
		})
		ui.Column(c.Key("st-main")).Grow(1).MinWidth(0).FillHeight().Background(pal.BG).BorderWidth(0, 0, 0, 1).BorderColor(pal.L2).Children(func() {
			ui.Row(c).FillWidth().Height(46).Shrink(0).Padding(0, 22).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c.Key("st-title"), tr("st_"+s.tab)).FontSize(13).FontWeight(600).TextColor(pal.T1).Grow(1).
					ContentTransition(ui.ContentTransition{})
				if s.notice != "" {
					col := pal.T2
					if s.noticeErr {
						col = pal.Red
					}
					ui.Text(c.Key("st-notice"), s.notice).FontSize(11.5).TextColor(col).SingleLine().MinWidth(0).
						ContentTransition(ui.ContentTransition{}).
						Transition(ui.ElementTransition{Duration: 180 * time.Millisecond, Enter: &ui.Motion{X: -6}, Exit: &ui.Motion{}})
				}
			})
			// 每个分区一个页面：切换时淡入并轻轻上移
			ui.Scroll(c.Key("st-body-" + s.tab)).FillWidth().Grow(1).MinHeight(0).Transition(motionPage).Children(func() {
				ui.Column(c).FillWidth().Padding(4, 22, 28).Gap(18).Children(func() { s.body(c) })
			})
		})
	})
	s.dialogs(c)
	if s.notice != "" {
		if d := time.Until(s.noticeAt.Add(4 * time.Second)); d > 0 {
			c.After(d)
		} else {
			s.notice = ""
		}
	}
	if d := time.Until(s.copiedAt.Add(1500 * time.Millisecond)); d > 0 {
		c.After(d)
	}
}

func (s *settingsWin) body(c *ui.Context) {
	switch s.tab {
	case "appearance":
		s.appearance(c)
	case "menubar":
		s.menubar(c)
	case "shortcuts":
		s.shortcuts(c)
	case "automation":
		s.automationTab(c)
	case "ai":
		s.ai(c)
	case "privacy":
		s.privacy(c)
	case "updates":
		s.updates(c)
	case "advanced":
		s.advanced(c)
	default:
		s.general(c)
	}
}

// ───────── 布局件 ─────────

type rowFn func(label, desc string, ctl func())

// group 是一组设置：可选的小标题，下面一张卡片，卡片里的行之间有分隔线。
func (s *settingsWin) group(c *ui.Context, key, label string, fn func(row rowFn)) {
	pal := s.pal
	ui.Column(c.Key(key)).FillWidth().Gap(6).Children(func() {
		if label != "" {
			pal.caption(c, label).Margin(0, 2)
		}
		bg := pal.Elev
		if pal.Dark {
			bg = pal.Card
		}
		ui.Column(c).FillWidth().Radius(10).Border(1, pal.L2).Background(bg).Children(func() {
			first := true
			n := 0
			fn(func(label, desc string, ctl func()) {
				if !first {
					pal.hair(c)
				}
				first = false
				n++
				ui.Row(c.Key("row-"+itoa(n))).FillWidth().MinHeight(44).Padding(8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
					if label == "" { // 整行由控件自己画（例如带文字的复选框）
						ctl()
						return
					}
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						ui.Text(c, label).FontSize(13).TextColor(pal.T1)
						if desc != "" {
							ui.Text(c, desc).FontSize(11.5).TextColor(pal.T3).Wrap()
						}
					})
					if ctl != nil {
						ctl()
					}
				})
			})
		})
	})
}

func (s *settingsWin) toggle(c *ui.Context, key string, on bool, set func(bool)) {
	v := on
	s.pal.toggle(c, key, &v, false, false).Label(key)
	if v != on {
		set(v)
	}
}

func (s *settingsWin) setPref(name, value string) { s.h.set("ui."+name, value) }

func (s *settingsWin) seg(c *ui.Context, key string, width float32, values []string, labels []string, cur string, set func(string)) {
	idx := slices.Index(values, cur)
	if idx < 0 {
		idx = 0
	}
	ui.Row(c).Width(width).Shrink(0).Children(func() {
		if n := s.pal.segmented(c, key, idx, labels...); n != idx {
			set(values[n])
		}
	})
}

func (s *settingsWin) choose(c *ui.Context, key string, width float32, values, labels []string, cur string, set func(string)) {
	idx := slices.Index(values, cur)
	if idx < 0 {
		idx = 0
	}
	sel := labels[idx]
	ui.Select(c.Key(key), &sel, labels).Width(width).Label(key)
	if sel != labels[idx] {
		if i := slices.Index(labels, sel); i >= 0 {
			set(values[i])
		}
	}
}

func (s *settingsWin) statusBadge(c *ui.Context, ok bool, yes, no string) {
	if ok {
		s.pal.badge(c, yes, "green")
	} else {
		s.pal.badge(c, no, "")
	}
}

// ───────── 通用 ─────────

func (s *settingsWin) general(c *ui.Context) {
	h := s.h
	s.group(c, "g-main", "", func(row rowFn) {
		if h.loginItem != nil {
			row(tr("login_item"), tr("login_item_h"), func() {
				s.toggle(c, "login", h.loginItem(), func(v bool) {
					if err := h.setLoginItem(v); err != nil {
						s.setNotice(err.Error(), true)
					}
				})
			})
		}
		row(tr("language"), "", func() {
			s.choose(c, "lang", 150, []string{"", "zh-Hans", "en"}, []string{tr("lang_system"), "简体中文", "English"}, h.get(keyLanguage), func(v string) { h.set(keyLanguage, v) })
		})
		row(tr("restore_state"), tr("restore_state_h"), func() {
			s.toggle(c, "restore", h.get(keyRestore) != "false", func(v bool) { h.set(keyRestore, boolStr(v)) })
		})
		row(tr("confirm_quit"), tr("confirm_quit_h"), func() {
			s.toggle(c, "confirm-quit", h.get(keyConfirmQuit) != "false", func(v bool) { h.set(keyConfirmQuit, boolStr(v)) })
		})
	})
	s.group(c, "g-feedback", tr("g_feedback"), func(row rowFn) {
		row(tr("notify_runs"), tr("notify_runs_h"), func() {
			cur := h.get(keyNotify)
			if cur == "" {
				cur = "fail"
			}
			s.seg(c, "notify", 230, []string{"never", "fail", "always"}, []string{tr("n_never"), tr("n_fail"), tr("n_always")}, cur, func(v string) { h.set(keyNotify, v) })
		})
	})
}

// ───────── 外观 ─────────

var accentSwatches = []struct {
	id  Accent
	hex string
}{{AccentInk, ""}, {AccentOrange, "#f54e00"}, {AccentBlue, "#2f6fed"}, {AccentGreen, "#1f8a65"}, {AccentViolet, "#7c5cff"}}

func (s *settingsWin) appearance(c *ui.Context) {
	pal := s.pal
	p := s.prefs
	ui.Column(c.Key("themes-g")).FillWidth().Gap(6).Children(func() {
		pal.caption(c, tr("g_theme")).Margin(0, 2)
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			for _, mode := range []string{"light", "dark", "system"} {
				mode := mode
				card := ui.Column(c.Key("theme-" + mode)).Grow(1).Basis(0).Gap(7).AlignItems(ui.Center).Label(tr("theme_" + mode))
				card.Children(func() {
					prev := ui.Row(c).FillWidth().Height(76).Radius(9).Clip()
					if p.Theme == mode {
						if pal.Ink {
							prev.Border(2, pal.FG)
						} else {
							prev.Border(2, pal.Accent)
						}
					} else {
						prev.Border(1, pal.L3)
					}
					prev.Children(func() { themePreview(c, mode) })
					col := pal.T2
					if p.Theme == mode {
						col = pal.T1
					}
					ui.Text(c, tr("theme_"+mode)).FontSize(12).TextColor(col)
				})
				if card.Clicked() {
					s.setPref("theme", mode)
				}
			}
		})
	})
	s.group(c, "g-look", "", func(row rowFn) {
		row(tr("accent"), tr("accent_h"), func() {
			ui.Row(c).Gap(10).Children(func() {
				for _, sw := range accentSwatches {
					sw := sw
					col := pal.FG
					if sw.hex != "" {
						col = ui.Hex(sw.hex)
					}
					dot := ui.Row(c.Key("acc-"+string(sw.id))).Size(22, 22).Radius(11).Background(col).Label(string(sw.id)).Tooltip(string(sw.id))
					if p.Accent == sw.id {
						dot.Border(2, pal.BG).Shadow(0, 0, 0, 2, pal.T1)
					}
					if dot.Clicked() {
						s.setPref("accent", string(sw.id))
					}
				}
			})
		})
		row(tr("material"), tr("material_h"), func() {
			s.seg(c, "material", 150, []string{"glass", "solid"}, []string{tr("mat_glass"), tr("mat_solid")}, p.Material, func(v string) { s.setPref("material", v) })
		})
	})
	s.group(c, "g-editor", tr("g_editor"), func(row rowFn) {
		row(tr("font_size"), "", func() {
			s.seg(c, "fs", 150, []string{"12", "13", "14"}, []string{"12", "13", "14"}, strconv.Itoa(int(p.EditorFontSize)), func(v string) { s.setPref("editorFontSize", v) })
		})
		row(tr("line_numbers"), "", func() {
			s.toggle(c, "ln", p.LineNumbers, func(v bool) { s.setPref("lineNumbers", boolStr(v)) })
		})
		row(tr("ext_editor"), tr("ext_editor_h"), func() {
			values, labels := []string{""}, []string{tr("ext_default")}
			if s.h.editors != nil {
				for _, e := range s.h.editors() {
					values, labels = append(values, e.ID), append(labels, e.Name)
				}
			}
			s.choose(c, "editor", 190, values, labels, p.ExternalEditor, func(v string) { s.setPref("externalEditor", v) })
		})
	})
}

// themePreview 画主题卡片里的缩略图：底色 + 一块小面板。
func themePreview(c *ui.Context, mode string) {
	light, dark := newPalette(false, AccentInk), newPalette(true, AccentInk)
	half := func(p palette) {
		ui.Column(c).Grow(1).Basis(0).FillHeight().Background(p.Card).Padding(12, 10).AlignItems(ui.End).Children(func() {
			ui.Column(c).Width(64).Padding(6).Radius(6).Gap(4).Background(p.Elev).Shadow(0, 3, 8, 0, alpha(ui.Hex("#000000"), 0.18)).Children(func() {
				ui.Box(c).Width(36).Height(5).Radius(3).Background(alpha(p.FG, 0.85))
				ui.Box(c).FillWidth().Height(5).Radius(3).Background(alpha(p.FG, 0.25))
				ui.Box(c).Width(46).Height(5).Radius(3).Background(alpha(p.FG, 0.25))
			})
		})
	}
	switch mode {
	case "light":
		half(light)
	case "dark":
		half(dark)
	default:
		half(light)
		half(dark)
	}
}

// ───────── 菜单栏与面板 ─────────

var trayPreviewCache = map[trayStyle]*ui.Bitmap{}

// trayPreview 把菜单栏模板图标重新着色为白色，放在深色底上预览。
func trayPreview(style trayStyle) *ui.Bitmap {
	if b, ok := trayPreviewCache[style]; ok {
		return b
	}
	img, err := png.Decode(bytes.NewReader(trayIconPNG(style, 2, false)))
	if err != nil {
		return nil
	}
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			out.SetNRGBA(x, y, color.NRGBA{255, 255, 255, uint8(a >> 8)})
		}
	}
	b := ui.NewBitmap(out)
	trayPreviewCache[style] = b
	return b
}

func (s *settingsWin) menubar(c *ui.Context) {
	pal := s.pal
	p := s.prefs
	ui.Column(c.Key("mb-g")).FillWidth().Gap(6).Children(func() {
		pal.caption(c, tr("g_menubar_icon")).Margin(0, 2)
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			for _, st := range []trayStyle{trayPill, trayDot, trayBars} {
				st := st
				card := ui.Column(c.Key("mbs-" + string(st))).Grow(1).Basis(0).Gap(7).AlignItems(ui.Center).Label(tr("mb_" + string(st)))
				card.Children(func() {
					prev := ui.Row(c).FillWidth().Height(54).Radius(9).Background(ui.Hex("#2a2a2a")).AlignItems(ui.Center).Justify(ui.Center).Gap(6)
					if p.MenubarStyle == st {
						if pal.Ink {
							prev.Border(2, pal.FG)
						} else {
							prev.Border(2, pal.Accent)
						}
					}
					prev.Children(func() {
						if b := trayPreview(st); b != nil {
							bw, bh := b.Size()
							ui.Image(c, b).Size(float32(bw)/2, float32(bh)/2)
						}
						switch p.MenubarExtra {
						case "count":
							ui.Text(c, "2").FontSize(12).TextColor(ui.Hex("#ffffff"))
						case "countdown":
							ui.Text(c, "1:42").Font("monospace").FontSize(12).TextColor(ui.Hex("#ffffff"))
						}
					})
					col := pal.T2
					if p.MenubarStyle == st {
						col = pal.T1
					}
					ui.Text(c, tr("mb_"+string(st))).FontSize(12).TextColor(col)
				})
				if card.Clicked() {
					s.setPref("menubarStyle", string(st))
				}
			}
		})
	})
	s.group(c, "g-mb", "", func(row rowFn) {
		row(tr("mb_extra"), "", func() {
			s.seg(c, "mbx", 250, []string{"none", "count", "countdown"}, []string{tr("mbx_none"), tr("mbx_count"), tr("mbx_countdown")}, p.MenubarExtra, func(v string) { s.setPref("menubarExtra", v) })
		})
		row(tr("busy_anim"), tr("busy_anim_h"), func() {
			s.toggle(c, "busy", p.BusyAnim, func(v bool) { s.setPref("busyAnim", boolStr(v)) })
		})
	})
	s.group(c, "g-panel", tr("g_panel"), func(row rowFn) {
		row(tr("layout"), "", func() {
			s.seg(c, "layout", 150, []string{"list", "grid"}, []string{tr("layout_list"), tr("layout_grid")}, p.Layout, func(v string) { s.setPref("layout", v) })
		})
		row(tr("density"), "", func() {
			s.seg(c, "density", 150, []string{"comfortable", "compact"}, []string{tr("density_comfy"), tr("density_compact")}, p.Density, func(v string) { s.setPref("density", v) })
		})
		row(tr("show_scenes"), "", func() { s.toggle(c, "scenes", p.ShowScenes, func(v bool) { s.setPref("showScenes", boolStr(v)) }) })
		row(tr("show_groups"), "", func() { s.toggle(c, "groups", p.ShowGroups, func(v bool) { s.setPref("showGroups", boolStr(v)) }) })
	})
	s.orderList(c)
}

// sortedViews 按 meta.Order（0 排在最后）再按名称排序，与面板一致。
func sortedViews(views []engine.View) []engine.View {
	out := slices.Clone(views)
	slices.SortStableFunc(out, func(a, b engine.View) int {
		oa, ob := a.Meta.Order, b.Meta.Order
		if oa == 0 {
			oa = 1 << 30
		}
		if ob == 0 {
			ob = 1 << 30
		}
		if oa != ob {
			return oa - ob
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

func (s *settingsWin) orderList(c *ui.Context) {
	pal := s.pal
	views := sortedViews(s.h.views())
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	move := func(i, j int) {
		ids[i], ids[j] = ids[j], ids[i]
		s.h.setOrder(ids)
	}
	s.group(c, "g-order", tr("g_order"), func(row rowFn) {
		for i, v := range views {
			i, v := i, v
			pal.hair(c)
			ui.Row(c.Key("ord-"+v.ID)).FillWidth().Height(34).Padding(0, 14).Gap(9).AlignItems(ui.Center).Children(func() {
				switchIcon(c, v.Icon, 14).TextColor(pal.T2)
				col := pal.T1
				if v.Hidden {
					col = pal.T3
				}
				ui.Text(c, v.Name).FontSize(12.5).TextColor(col).SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, v.Meta.Group).FontSize(11.5).TextColor(pal.T3).SingleLine()
				if i > 0 && pal.iconBtn(c, "up", "arrow-up", tr("move_up"), false).Clicked() {
					move(i, i-1)
				}
				if i < len(views)-1 && pal.iconBtn(c, "down", "arrow-down", tr("move_down"), false).Clicked() {
					move(i, i+1)
				}
				name, tip := "eye", tr("hide_in_panel")
				if v.Hidden {
					name, tip = "eye-off", tr("show_in_panel")
				}
				if pal.iconBtn(c, "vis", name, tip, false).Clicked() {
					s.h.setHidden(v.ID, !v.Hidden)
				}
			})
		}
		pal.hair(c)
		ui.Row(c.Key("ord-more")).FillWidth().Height(34).Padding(0, 14).AlignItems(ui.Center).Children(func() {
			if pal.button(c, "groups", tr("manage_groups"), "square-terminal", btnGhost, false).Clicked() && s.h.openWorkbench != nil {
				s.h.openWorkbench(secSwitches, "")
			}
		})
	})
}

// ───────── 快捷键 ─────────

func (s *settingsWin) shortcuts(c *ui.Context) {
	pal := s.pal
	h := s.h
	s.group(c, "g-global", tr("g_global"), func(row rowFn) {
		for _, it := range []struct{ key, label string }{{keyPanelShortcut, tr("sc_open_panel")}, {keyWorkbenchShortcut, tr("sc_open_wb")}} {
			it := it
			row(it.label, "", func() {
				ui.Column(c).Width(260).Shrink(0).Children(func() {
					issue := ""
					if h.shortcutIssue != nil {
						issue = h.shortcutIssue(it.key)
					}
					if v, changed := s.rec.field(c, pal, "g-"+it.key, h.get(it.key), issue); changed {
						h.set(it.key, v)
					}
				})
			})
		}
	})
	type item struct {
		owner, name, icon, kind, accel string
	}
	var items []item
	for _, v := range sortedViews(h.views()) {
		if v.Meta.Shortcut != "" {
			kind := tr("kind_switch_toggle")
			if v.Type == contract.Action {
				kind = tr("kind_switch_run")
			}
			items = append(items, item{"switch:" + v.ID, v.Name, v.Icon, kind, v.Meta.Shortcut})
		}
	}
	for _, sv := range h.scenes() {
		if sv.Shortcut != "" {
			items = append(items, item{"scene:" + sv.ID, sv.Name, sceneIcon(sv.Scene), tr("kind_scene"), sv.Shortcut})
		}
	}
	issues := map[string]automation.ShortcutIssue{}
	if h.auto != nil {
		issues = h.auto.ShortcutIssues()
	}
	s.group(c, "g-sws", tr("g_sw_scenes"), func(row rowFn) {
		if len(items) == 0 {
			row(tr("no_shortcuts"), "", nil)
		}
		for _, it := range items {
			it := it
			desc := it.kind
			if is, ok := issues[it.owner]; ok {
				if is.Kind == "taken" {
					desc = tr("sc_taken")
				} else {
					desc = tr("sc_dup")
					desc = fmt.Sprintf(desc, is.Owner)
				}
			}
			row(it.name, desc, func() {
				pal.mono(c, automation.ShortcutSymbols(it.accel), 12.5, pal.T1)
				if pal.button(c, "edit-"+it.owner, tr("edit"), "", btnGhost, false).Clicked() && h.openWorkbench != nil {
					kind, id, _ := strings.Cut(it.owner, ":")
					if kind == "scene" {
						h.openWorkbench(secScenes, id)
					} else {
						h.openWorkbench(secSwitches, id)
					}
				}
			})
		}
	})
}

// ───────── 自动化 ─────────

func (s *settingsWin) automationTab(c *ui.Context) {
	h := s.h
	if h.auto == nil {
		return
	}
	s.group(c, "g-auto", "", func(row rowFn) {
		enabled := 0
		for _, r := range h.auto.Rules() {
			if r.Enabled {
				enabled++
			}
		}
		row(tr("auto_enabled"), fmt.Sprintf(tr("auto_enabled_h"), enabled), func() {
			s.toggle(c, "auto-on", h.autoOn(), h.setAutoOn)
		})
		paused, until := h.auto.Paused()
		cur := "none"
		desc := tr("auto_pause_h")
		if paused {
			cur = "1h"
			if until.IsZero() {
				desc = tr("paused_forever")
			} else {
				desc = fmt.Sprintf(tr("paused_until"), until.Local().Format("01-02 15:04"))
				if until.Sub(time.Now()) > 2*time.Hour {
					cur = "today"
				}
			}
		}
		row(tr("auto_pause"), desc, func() {
			s.seg(c, "pause", 230, []string{"1h", "today", "none"}, []string{tr("pause_1h_s"), tr("pause_today_s"), tr("pause_none")}, cur, func(v string) {
				now := time.Now()
				switch v {
				case "1h":
					_ = h.auto.Pause(now.Add(time.Hour))
				case "today":
					y, m, d := now.Date()
					_ = h.auto.Pause(time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()))
				default:
					_ = h.auto.Resume()
				}
			})
		})
		row(tr("conflict"), tr("conflict_h"), func() {
			cur := h.get(automation.SettingConflict)
			if cur == "" {
				cur = "manual"
			}
			s.choose(c, "conflict", 190, []string{"manual", "rule"}, []string{tr("conflict_manual"), tr("conflict_rule")}, cur, func(v string) { h.set(automation.SettingConflict, v) })
		})
	})
	ui.Row(c).Children(func() {
		if s.pal.button(c, "edit-rules", tr("edit_rules"), "zap", btnPlain, false).Clicked() && h.openWorkbench != nil {
			h.openWorkbench(secAutomations, "")
		}
	})
}

// ───────── AI 与集成 ─────────

var aiPerms = []struct{ key, label string }{{"read", "ai_read"}, {"toggle", "ai_toggle"}, {"write", "ai_write"}, {"delete", "ai_delete"}}
var aiDefaults = map[string]bool{"read": true, "toggle": true, "write": true, "delete": false}

func (s *settingsWin) aiAllowed(perm string) bool {
	switch s.h.get("ai." + perm) {
	case "true":
		return true
	case "false":
		return false
	}
	return aiDefaults[perm]
}

func maskToken(t string) string {
	if len(t) < 8 {
		return "••••"
	}
	return t[:3] + "••••••••" + t[len(t)-4:]
}

func (s *settingsWin) ai(c *ui.Context) {
	pal := s.pal
	h := s.h
	s.group(c, "g-api", tr("g_api"), func(row rowFn) {
		running := h.apiRunning()
		label := tr("api_stopped")
		if running {
			label = tr("api_running") + "  " + h.apiAddr
		}
		row(label, tr("api_h"), func() {
			s.toggle(c, "api", running, func(v bool) {
				if err := h.setAPI(v); err != nil {
					s.setNotice(err.Error(), true)
				}
			})
		})
		row(tr("token"), tr("token_h"), func() {
			pal.mono(c, maskToken(h.token()), 12, pal.T3)
			label := tr("copy")
			if time.Since(s.copiedAt) < 1500*time.Millisecond {
				label = tr("copied")
			}
			if pal.button(c, "copy-token", label, "copy", btnPlain, false).Clicked() {
				h.copy(h.token())
				s.copiedAt = time.Now()
			}
			if pal.button(c, "regen", tr("regenerate"), "", btnPlain, false).Clicked() {
				s.regenOpen = true
			}
		})
	})
	s.group(c, "g-ai-can", tr("g_ai_can"), func(row rowFn) {
		for _, p := range aiPerms {
			p := p
			row("", "", func() {
				on := s.aiAllowed(p.key)
				v := on
				ui.Checkbox(c.Key("perm-"+p.key), &v, tr(p.label)).Grow(1)
				if v != on {
					h.set("ai."+p.key, boolStr(v))
				}
			})
		}
		row(tr("ai_confirm"), tr("ai_confirm_h"), func() {
			s.toggle(c, "confirm", h.get("ai.confirm") != "false", func(v bool) { h.set("ai.confirm", boolStr(v)) })
		})
	})
	pal.hint(c, tr("ai_note")).Margin(-10, 2, 0)
	s.group(c, "g-clients", tr("g_clients"), func(row rowFn) {
		for _, st := range h.clients() {
			st := st
			desc := tr("cl_" + map[string]string{integrations.ClaudeCode: "claude_h", integrations.Cursor: "cursor_h", integrations.Codex: "codex_h"}[st.Client])
			if st.Connected && !st.Current && st.Command != "" {
				desc = tr("cl_outdated") + " · " + st.Command
			}
			row(tr("cl_"+st.Client), desc, func() {
				switch {
				case st.Current:
					pal.badge(c, tr("cl_connected"), "green")
				case !st.Present && !st.Connected:
					pal.badge(c, tr("cl_missing"), "")
				default:
					label := tr("cl_add")
					if st.Connected {
						label = tr("cl_update")
					}
					if pal.button(c, "cl-"+st.Client, label, "", btnPlain, false).Clicked() {
						p, err := h.plan(st.Client)
						if err != nil {
							if err == integrations.ErrNoCLI {
								s.setNotice(tr("cl_no_cli"), true)
							} else {
								s.setNotice(err.Error(), true)
							}
							return
						}
						s.plan, s.planOpen = &p, true
					}
				}
			})
		}
		row(tr("cl_other"), tr("cl_other_h"), func() {
			label := tr("copy")
			if time.Since(s.copiedAt) < 1500*time.Millisecond {
				label = tr("copied")
			}
			if pal.button(c, "copy-snippet", label, "copy", btnPlain, false).Clicked() {
				h.copy(h.snippet)
				s.copiedAt = time.Now()
			}
		})
		pal.hair(c)
		ui.Text(c.Key("snippet"), h.snippet).Font("monospace").FontSize(11.5).TextColor(pal.T2).Selectable().Padding(12, 14).FillWidth().Background(pal.L1)
	})
	s.group(c, "g-tools", tr("g_tools"), func(row rowFn) {
		path := h.cliPath()
		desc := path
		if path == "" {
			desc = tr("cli_missing")
		}
		row(tr("cli_tool"), desc, func() { s.statusBadge(c, path != "", tr("installed"), tr("not_installed")) })
		ok, target := h.skill()
		desc = tr("skill_h")
		if target != "" {
			desc = target
		}
		row(tr("skill"), desc, func() {
			if ok {
				pal.badge(c, tr("installed"), "green")
				return
			}
			if h.canSkill && pal.button(c, "skill", tr("install"), "", btnPlain, false).Clicked() {
				if err := h.installSkill(); err != nil {
					s.setNotice(err.Error(), true)
				}
				return
			}
			if !h.canSkill {
				pal.badge(c, tr("not_installed"), "")
			}
		})
	})
}

// ───────── 权限与隐私 ─────────

func (s *settingsWin) privacy(c *ui.Context) {
	pal := s.pal
	h := s.h
	s.group(c, "g-perms", tr("g_perms"), func(row rowFn) {
		row(tr("perm_ax"), tr("perm_ax_h"), func() {
			ok := h.axTrusted()
			s.statusBadge(c, ok, tr("granted"), tr("not_granted"))
			if !ok && pal.button(c, "ax", tr("ask"), "", btnPlain, false).Clicked() {
				h.requestAX()
			}
		})
		row(tr("perm_loc"), tr("perm_loc_h"), func() {
			st := h.location()
			switch st {
			case 2:
				pal.badge(c, tr("granted"), "green")
			case 0:
				pal.badge(c, tr("not_asked"), "")
				if pal.button(c, "loc", tr("ask"), "", btnPlain, false).Clicked() {
					h.requestLoc()
				}
			default:
				pal.badge(c, tr("not_granted"), "")
				if pal.button(c, "loc-sys", tr("open_sys"), "", btnGhost, false).Clicked() {
					h.openSys("Privacy_LocationServices")
				}
			}
		})
		row(tr("perm_ae"), tr("perm_ae_h"), func() {
			if pal.button(c, "ae-sys", tr("open_sys"), "", btnGhost, false).Clicked() {
				h.openSys("Privacy_Automation")
			}
		})
		row(tr("perm_notif"), tr("perm_notif_h"), func() {
			if pal.button(c, "notif-sys", tr("open_sys"), "", btnGhost, false).Clicked() {
				h.openSys("notifications")
			}
		})
	})
	s.group(c, "g-data", tr("g_data"), func(row rowFn) {
		row(tr("no_telemetry"), tr("no_telemetry_h"), func() { icon(c, "check", 15).TextColor(pal.Green) })
		row(tr("net_requests"), tr("net_requests_h"), func() { icon(c, "check", 15).TextColor(pal.Green) })
		row(tr("retention"), "", func() {
			cur := h.get(keyRetention)
			if cur == "" {
				cur = "30"
			}
			s.choose(c, "ret", 120, []string{"7", "30", "90"}, []string{fmt.Sprintf(tr("days_n"), 7), fmt.Sprintf(tr("days_n"), 30), fmt.Sprintf(tr("days_n"), 90)}, cur, func(v string) { h.set(keyRetention, v) })
		})
	})
}

// ───────── 更新 ─────────

func (s *settingsWin) updates(c *ui.Context) {
	pal := s.pal
	h := s.h
	s.group(c, "g-upd", "", func(row rowFn) {
		desc := s.updateMsg
		if !h.updater {
			desc = tr("update_dev")
		}
		row(fmt.Sprintf(tr("version_n"), h.version), desc, func() {
			if !h.updater {
				return
			}
			label := tr("check_now")
			if s.checking {
				label = tr("checking")
			}
			if pal.button(c, "check", label, "", btnPlain, s.checking).Clicked() && !s.checking {
				s.checking = true
				h.checkUpdate(func(latest string, err error) {
					h.onMain(func() {
						s.checking = false
						switch {
						case err != nil:
							s.updateMsg = err.Error()
						case latest == "":
							s.updateMsg = tr("up_to_date")
						default:
							s.updateMsg = fmt.Sprintf(tr("update_avail"), latest)
						}
					})
				})
			}
		})
		row(tr("auto_update"), tr("auto_update_h"), func() {
			s.toggle(c, "auto-upd", h.get(keyAutoUpdate) != "false", func(v bool) { h.set(keyAutoUpdate, boolStr(v)) })
		})
	})
}

// ───────── 高级 ─────────

func envVars(raw string) [][2]string {
	var m map[string]string
	_ = json.Unmarshal([]byte(raw), &m)
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	return out
}

func encodeEnv(vars [][2]string) string {
	m := map[string]string{}
	for _, kv := range vars {
		if kv[0] != "" {
			m[kv[0]] = kv[1]
		}
	}
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func (s *settingsWin) advanced(c *ui.Context) {
	pal := s.pal
	h := s.h
	s.group(c, "g-adv", "", func(row rowFn) {
		dir := h.scriptsDir()
		row(tr("scripts_dir"), tr("scripts_dir_h"), func() {
			pal.mono(c, shortenHome(dir), 12, pal.T3).MaxWidth(220)
			if pal.iconBtn(c, "reveal-dir", "folder", tr("reveal"), false).Clicked() {
				h.reveal(dir)
			}
			if pal.button(c, "change-dir", tr("change"), "", btnPlain, false).Clicked() {
				h.chooseDir(func(path string) {
					if path != "" && path != dir {
						h.set(keyScriptsDir, path)
						s.relaunchTo, s.relaunchOn = path, true
					}
				})
			}
		})
		row(tr("shell"), tr("shell_h"), func() {
			s.seg(c, "shell", 200, []string{"", "/bin/zsh", "/bin/bash"}, []string{"sh", "zsh", "bash"}, h.get(keyShell), func(v string) { h.set(keyShell, v) })
		})
		row(tr("default_timeout"), "", func() {
			cur := h.get(keyTimeout)
			if cur == "" {
				cur = "30"
			}
			s.choose(c, "timeout", 120, []string{"10", "30", "60", "120"}, []string{tr("t_10"), "30 s", tr("t_60"), tr("t_120")}, cur, func(v string) { h.set(keyTimeout, v) })
		})
		row(tr("poll_interval"), tr("poll_interval_h"), func() { pal.mono(c, "5s / 30s", 12, pal.T3) })
	})
	vars := envVars(h.get(keyEnv))
	s.group(c, "g-env", tr("g_env"), func(row rowFn) {
		for i, kv := range vars {
			i, kv := i, kv
			if i > 0 {
				pal.hair(c)
			}
			ui.Row(c.Key("env-"+kv[0])).FillWidth().Height(36).Padding(0, 14).Gap(10).AlignItems(ui.Center).Children(func() {
				pal.mono(c, kv[0], 12, pal.T1).Width(160)
				pal.mono(c, kv[1], 12, pal.T3).Grow(1).MinWidth(0)
				if pal.iconBtn(c, "rm", "x", tr("remove"), false).Clicked() {
					h.set(keyEnv, encodeEnv(append(vars[:i:i], vars[i+1:]...)))
				}
			})
		}
		if len(vars) > 0 {
			pal.hair(c)
		}
		ui.Row(c.Key("env-new")).FillWidth().Padding(8, 14).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("env-name"), &s.envName).Width(160).Font("monospace").FontSize(12).Placeholder(tr("env_name"))
			ui.TextInput(c.Key("env-value"), &s.envValue).Grow(1).Font("monospace").FontSize(12).Placeholder(tr("env_value"))
			name := strings.TrimSpace(s.envName)
			valid := name != "" && !strings.ContainsAny(name, "= \t")
			if pal.button(c, "env-add", tr("env_add"), "plus", btnPlain, !valid).Clicked() && valid {
				vars = append(vars, [2]string{name, s.envValue})
				h.set(keyEnv, encodeEnv(vars))
				s.envName, s.envValue = "", ""
			}
		})
	})
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if pal.button(c, "export", tr("export_cfg"), "download", btnPlain, false).Clicked() {
			h.exportConfig(func(path string, err error) {
				h.onMain(func() {
					if err != nil {
						s.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
					} else if path != "" {
						s.setNotice(fmt.Sprintf(tr("exported"), shortenHome(path)), false)
					}
				})
			})
		}
		if pal.button(c, "import", tr("import_cfg"), "upload", btnPlain, false).Clicked() {
			h.importConfig(func(scripts, scenes, rules int, err error) {
				h.onMain(func() {
					if err != nil {
						s.setNotice(fmt.Sprintf(tr("import_failed"), err), true)
					} else {
						s.setNotice(fmt.Sprintf(tr("imported"), scripts, scenes, rules), false)
					}
				})
			})
		}
		ui.Spacer(c)
		if pal.button(c, "reset", tr("reset_all"), "", btnDanger, false).Clicked() {
			s.resetOpen = true
		}
	})
}

func shortenHome(p string) string {
	if home := homeDir(); home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// ───────── 对话框 ─────────

func (s *settingsWin) dialogs(c *ui.Context) {
	pal := s.pal
	h := s.h
	if s.regenOpen {
		if ui.AlertDialog(c.Key("regen-dialog"), &s.regenOpen, tr("regen_title"), tr("regen_msg"), tr("cancel"), tr("regenerate")) == 1 {
			if err := h.regenToken(); err != nil {
				s.setNotice(err.Error(), true)
			}
		}
	}
	if s.resetOpen {
		if ui.AlertDialog(c.Key("reset-dialog"), &s.resetOpen, tr("reset_title"), tr("reset_msg"), tr("cancel"), tr("reset")) == 1 {
			h.reset()
		}
	}
	if s.relaunchOn {
		if ui.AlertDialog(c.Key("relaunch-dialog"), &s.relaunchOn, tr("relaunch_title"), fmt.Sprintf(tr("relaunch_msg"), shortenHome(s.relaunchTo)), tr("later"), tr("relaunch")) == 1 {
			h.relaunch()
		}
	}
	if s.planOpen && s.plan != nil {
		planDialog(c, pal, &s.planOpen, *s.plan, func(p integrations.Plan) {
			if err := h.apply(p); err != nil {
				s.setNotice(err.Error(), true)
			} else {
				s.setNotice(fmt.Sprintf(tr("cl_done"), tr("cl_"+p.Client)), false)
			}
		})
	}
}

// planDialog 在接入 AI 客户端前展示将要写入的内容或执行的命令，确认后才 apply。
func planDialog(c *ui.Context, pal palette, open *bool, p integrations.Plan, apply func(integrations.Plan)) {
	ui.DialogBase(c.Key("plan-dialog"), open, func(backdrop, panel ui.Element) {
		backdrop.Background(alpha(ui.Hex("#000000"), 0.25))
		panel.Width(520).Padding(18).Radius(12).Gap(12).Background(pal.Elev).Shadow(0, 12, 32, 0, alpha(ui.Hex("#000000"), 0.25))
		panel.Children(func() {
			ui.Text(c, fmt.Sprintf(tr("cl_confirm_title"), tr("cl_"+p.Client))).FontSize(14).FontWeight(600).TextColor(pal.T1)
			if len(p.Commands) > 0 {
				ui.Text(c, tr("cl_confirm_cmd")).FontSize(12.5).TextColor(pal.T2)
			} else {
				ui.Text(c, fmt.Sprintf(tr("cl_confirm_file"), shortenHome(p.File))).FontSize(12.5).TextColor(pal.T2).Wrap()
			}
			ui.Scroll(c).FillWidth().MaxHeight(260).Radius(8).Background(pal.L1).Children(func() {
				ui.Text(c, p.Describe()).Font("monospace").FontSize(11.5).TextColor(pal.T1).Selectable().Padding(10, 12)
			})
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if pal.button(c, "plan-cancel", tr("cancel"), "", btnPlain, false).Clicked() {
					*open = false
				}
				if pal.button(c, "plan-apply", tr("cl_apply"), "", btnPrimary, false).Clicked() {
					*open = false
					apply(p)
				}
			})
		})
	})
}

var _ = store.ExportFormat
