package app

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/store"
)

// 工作台窗口：侧栏（开关、活动、脚本库）+ 主区。
// 设计稿：designs/opentoggle-v2/workbench.jsx。场景与自动化随 M4 加入侧栏。

const (
	secSwitches    = "switches"
	secScenes      = "scenes"
	secAutomations = "automations"
	secActivity    = "activity"
	secLibrary     = "library"

	wbDetail = "workbench" // 工作台测试产生的运行记录的 detail，用来把输出放回输出面板
)

type workbench struct {
	b     Backend
	prefs Prefs
	pal   palette

	// 由窗口层注入；测试里用替身
	onMain             func(fn func()) // 回到主线程执行并重绘
	openPath           func(path string)
	reveal             func(path string)
	exportRuns         func(runs []store.Run) // 选择位置并写出（异步，结果用 setNotice 报告）
	closeWin           func()                 // 关闭窗口（处理完未保存的修改之后）
	reduceTransparency func() bool

	section  string
	selected string
	views    []engine.View
	dirty    bool
	docs     map[string]*doc
	fields   map[string]*fieldState

	// 开关编辑器
	inspector  bool
	bottomOpen bool
	bottomTab  int // 0 输出 1 问题 2 历史
	outputs    map[string][]outLine
	outScroll  ui.ScrollState
	savedAt    map[string]time.Time
	notice     string
	noticeErr  bool
	noticeAt   time.Time
	iconOpen   bool
	iconQuery  string
	emoji      string
	paramOpen  int
	newOpen    bool
	moreOpen   bool
	deleteOpen bool
	busy       map[string]bool // 测试按钮执行中

	// 活动
	actFilter string
	runs      []store.Run
	runsDirty bool
	runSel    int
	runList   ui.ListState
	clearOpen bool

	closeAsk bool // 关闭窗口时有未保存的修改

	// 场景、自动化与快捷键
	auto          Automation
	scenes        []engine.SceneView
	rules         []store.Rule
	selScene      string
	selRule       string
	sceneDraft    map[string]store.Scene
	ruleDraft     map[string]store.Rule
	recording     string            // 正在录制快捷键的那个输入
	recorded      map[string]string // 录到的快捷键，下一次构建时交给对应的输入
	recordErr     string
	stepPick      string
	sceneIcon     bool
	ruleNote      map[string]string // 规则 id → 不完整的原因
	pauseOpen     bool
	installedApps func() []appInfo
	pickPath      func(done func(path string))
	network       func() (key, label string)

	// 脚本库
	libCat   string
	libQuery string
	libSel   string
}

type fieldState struct {
	src, text string
	pending   bool
}

type outKind int

const (
	outCmd outKind = iota
	outText
	outOK
	outErr
	outInfo
)

type outLine struct {
	kind outKind
	text string
}

func newWorkbench(b Backend, prefs Prefs) *workbench {
	return &workbench{
		b: b, prefs: prefs, section: secSwitches, dirty: true, runsDirty: true,
		docs: map[string]*doc{}, fields: map[string]*fieldState{}, outputs: map[string][]outLine{},
		savedAt: map[string]time.Time{}, busy: map[string]bool{},
		inspector: true, bottomOpen: true, paramOpen: -1, runSel: 0,
		recorded: map[string]string{}, sceneDraft: map[string]store.Scene{}, ruleDraft: map[string]store.Rule{}, ruleNote: map[string]string{},
		actFilter: "all", libCat: "all",
		onMain:             func(fn func()) { fn() },
		openPath:           func(string) {},
		reveal:             func(string) {},
		reduceTransparency: func() bool { return false },
	}
}

func (w *workbench) invalidate() { w.dirty = true; w.runsDirty = true }

// handle 处理引擎事件（在主线程调用）。
func (w *workbench) handle(ev engine.Event) {
	w.dirty = true
	switch ev.Kind {
	case engine.EventReloaded:
		w.checkDisk()
	case engine.EventRun:
		w.runsDirty = true
	case engine.EventScene:
	}
}

func (w *workbench) load() {
	if !w.dirty {
		return
	}
	w.dirty = false
	w.views = w.b.List()
	w.scenes = w.b.Scenes()
	if w.auto != nil {
		w.rules = w.auto.Rules()
	}
	sort.SliceStable(w.views, func(i, j int) bool {
		return strings.ToLower(w.views[i].Name) < strings.ToLower(w.views[j].Name)
	})
	if w.section == secSwitches && w.view(w.selected) == nil && len(w.views) > 0 {
		w.selected = w.views[0].ID
	}
	// 被删除的开关：丢掉没有改动的文档
	for id, d := range w.docs {
		if w.view(id) == nil && !d.modified() {
			delete(w.docs, id)
		}
	}
}

func (w *workbench) view(id string) *engine.View {
	for i := range w.views {
		if w.views[i].ID == id {
			return &w.views[i]
		}
	}
	return nil
}

// open 切到某个分区（和条目）。
func (w *workbench) open(section, item string) {
	if section != "" {
		w.section = section
	}
	if item != "" {
		w.selected = item
		w.paramOpen = -1
		w.iconOpen = false
	}
	w.dirty = true
}

func (w *workbench) setNotice(msg string, isErr bool) {
	w.notice, w.noticeErr, w.noticeAt = msg, isErr, time.Now()
}

// ───────── 根视图 ─────────

func (w *workbench) render(c *ui.Context) {
	w.load()
	base := c.Theme()
	w.pal = newPalette(base.Dark, w.prefs.Accent)
	c.SetTheme(w.pal.theme(base))
	pal := w.pal

	c.Root().Background(ui.Transparent)
	root := ui.Row(c).Fill().AlignItems(ui.Stretch)
	root.OnShortcut(ui.Cmd, ui.KeyS, w.saveCurrent)
	root.Children(func() {
		side := ui.Column(c.Key("side")).Width(236).Shrink(0).FillHeight()
		if c.Vibrancy() && !w.reduceTransparency() {
			side.Background(alpha(pal.Card, 0.55))
		} else {
			side.Background(pal.Card)
		}
		side.Children(func() { w.sidebar(c) })
		// 分隔线画在主区（不透明）的左边框上：画在透明的根视图上会透出底下的颜色
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).FillHeight().Background(pal.BG).
			BorderWidth(0, 0, 0, 1).BorderColor(pal.L2).Children(func() {
			switch w.section {
			case secScenes:
				w.sceneEditor(c)
			case secAutomations:
				w.ruleEditor(c)
			case secActivity:
				w.activity(c)
			case secLibrary:
				w.library(c)
			default:
				w.switchEditor(c)
			}
		})
	})
	if w.closeAsk {
		switch ui.AlertDialog(c.Key("close-dialog"), &w.closeAsk, tr("close_title"), tr("close_msg"), tr("save_all"), tr("discard"), tr("cancel")) {
		case 0:
			if w.saveAll() && w.closeWin != nil {
				w.closeWin()
			}
		case 1:
			for id, d := range w.docs {
				if d.modified() {
					delete(w.docs, id)
				}
			}
			if w.closeWin != nil {
				w.closeWin()
			}
		}
	}
	if w.notice != "" {
		if d := time.Until(w.noticeAt.Add(4 * time.Second)); d > 0 {
			c.After(d)
		} else {
			w.notice = ""
		}
	}
}

// topBar 是主区顶部 46px 的栏，可以拖动窗口。
func (w *workbench) topBar(c *ui.Context, fn func()) {
	ui.Row(c.Key("topbar")).FillWidth().Height(46).Shrink(0).Padding(0, 12, 0, 16).Gap(8).AlignItems(ui.Center).
		DragWindow().Children(fn)
	w.pal.hair(c)
}

// ───────── 侧栏 ─────────

func (w *workbench) sidebar(c *ui.Context) {
	pal := w.pal
	bar := c.TitleBar()
	ui.Row(c.Key("side-top")).FillWidth().Height(max(bar.Height, 46)).Shrink(0).DragWindow()
	ui.Column(c.Key("nav")).FillWidth().Padding(0, 8, 8).Gap(1).Children(func() {
		nav := []struct{ id, iconName, label string }{
			{secSwitches, "toggle-right", tr("nav_switches")},
			{secScenes, "layers", tr("nav_scenes")},
			{secAutomations, "zap", tr("nav_automations")},
			{secActivity, "activity", tr("nav_activity")},
			{secLibrary, "book-open", tr("nav_library")},
		}
		for _, n := range nav {
			count := ""
			switch n.id {
			case secSwitches:
				count = itoa(len(w.views))
			case secScenes:
				count = itoa(len(w.scenes))
			case secAutomations:
				enabled := 0
				for _, r := range w.rules {
					if r.Enabled {
						enabled++
					}
				}
				count = itoa(enabled)
			}
			item := pal.navItem(c, "nav-"+n.id, n.iconName, n.label, w.section == n.id, false, func() {
				if count != "" {
					pal.mono(c, count, 11, pal.T3)
				}
			})
			if item.Clicked() {
				w.open(n.id, "")
			}
		}
	})
	ui.Box(c).FillWidth().Height(1).Margin(0, 14).Background(pal.L2)
	ui.Scroll(c.Key("side-list")).FillWidth().Grow(1).Padding(2, 8, 8).Children(func() {
		switch w.section {
		case secScenes:
			w.sceneList(c)
		case secAutomations:
			w.ruleList(c)
		case secActivity:
			w.activityFilters(c)
		case secLibrary:
			w.libraryCats(c)
		default:
			w.switchList(c)
		}
	})
	pal.hair(c)
	foot := ui.Row(c.Key("side-foot")).FillWidth().Padding(8).Gap(4).AlignItems(ui.Center)
	foot.Children(func() {
		nb := pal.button(c, "new", tr("new"), "plus", btnGhost, false)
		if nb.Clicked() {
			w.newOpen = !w.newOpen
		}
		ui.PopoverBase(c, nb, &w.newOpen, func(panel ui.Element) {
			w.menuPanel(panel)
			panel.Children(func() { w.newMenu(c) })
		})
		ui.Spacer(c)
		if pal.iconBtn(c, "open-folder", "folder", tr("open_folder"), false).Clicked() {
			w.openPath(w.b.ScriptsDir())
		}
	})
}

// menuPanel 给弹出菜单统一外观。
func (w *workbench) menuPanel(panel ui.Element) {
	pal := w.pal
	panel.Width(210).Padding(4).Radius(10).Background(pal.Elev).Border(1, pal.L2).
		Shadow(0, 8, 24, 0, alpha(ui.Hex("#000000"), 0.18)).Gap(1)
}

func (w *workbench) menuItem(c *ui.Context, key, iconName, label string) bool {
	return w.pal.navItem(c, key, iconName, label, false, false, nil).Clicked()
}

func (w *workbench) newMenu(c *ui.Context) {
	items := []struct {
		key, iconName, label string
		typ                  contract.Type
	}{
		{"new-toggle", "toggle-right", tr("new_toggle"), contract.Toggle},
		{"new-daemon", "repeat", tr("new_daemon"), contract.Daemon},
		{"new-action", "play", tr("new_action"), contract.Action},
	}
	for _, it := range items {
		item := w.pal.navItem(c, it.key, it.iconName, it.label, false, false, nil).Label(tr("new") + " " + it.label)
		if item.Clicked() {
			w.newOpen = false
			w.create(it.typ)
		}
	}
	ui.Box(c).FillWidth().Height(1).Margin(4, 6).Background(w.pal.L2)
	if w.pal.navItem(c, "new-scene", "layers", tr("new_scene"), false, false, nil).Label(tr("new") + " " + tr("new_scene")).Clicked() {
		w.newOpen = false
		w.newScene()
	}
	if w.auto != nil && w.pal.navItem(c, "new-rule", "zap", tr("new_rule"), false, false, nil).Label(tr("new")+" "+tr("new_rule")).Clicked() {
		w.newOpen = false
		w.newRule("")
	}
	ui.Box(c).FillWidth().Height(1).Margin(4, 6).Background(w.pal.L2)
	if w.menuItem(c, "new-lib", "book-open", tr("new_from_library")) {
		w.newOpen = false
		w.open(secLibrary, "")
	}
}

type wbGroup struct {
	name  string
	views []engine.View
}

func groupSwitches(views []engine.View) []wbGroup {
	byName := map[string][]engine.View{}
	var names []string
	for _, v := range views {
		g := v.Meta.Group
		if _, ok := byName[g]; !ok {
			names = append(names, g)
		}
		byName[g] = append(byName[g], v)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "") != (names[j] == "") {
			return names[j] == "" // 未分组放最后
		}
		return names[i] < names[j]
	})
	out := make([]wbGroup, 0, len(names))
	for _, n := range names {
		out = append(out, wbGroup{n, byName[n]})
	}
	return out
}

func (w *workbench) switchList(c *ui.Context) {
	pal := w.pal
	groups := groupSwitches(w.views)
	for gi, g := range groups {
		if len(groups) > 1 || g.name != "" {
			name := g.name
			if name == "" {
				name = tr("other_group")
			}
			ui.Row(c.Key("gh-"+itoa(gi))).FillWidth().Padding(12, 8, 4).Children(func() { pal.caption(c, name) })
		}
		for _, v := range g.views {
			v := v
			sel := w.selected == v.ID
			r := ui.Row(c.Key("sw-"+v.ID)).FillWidth().Height(30).Padding(0, 8).Radius(7).Gap(8).AlignItems(ui.Center).Label(v.Name)
			if sel {
				r.Background(pal.L2)
			} else if r.Hovered() {
				r.Background(pal.L1)
			}
			r.Children(func() {
				w.stateDot(c, v)
				col := pal.T1
				if v.Hidden {
					col = pal.T3
				}
				ui.Row(c).Width(16).Justify(ui.Center).Children(func() { switchIcon(c, v.Icon, 14).TextColor(pal.T2) })
				ui.Text(c, v.Name).FontSize(12.5).TextColor(col).SingleLine().Grow(1).MinWidth(0)
				if d := w.docs[v.ID]; d != nil && d.modified() {
					ui.Box(c).Size(6, 6).Radius(3).Background(pal.T2).Tooltip(tr("unsaved"))
				}
			})
			if r.Clicked() {
				w.open(secSwitches, v.ID)
			}
		}
	}
	if len(w.views) == 0 {
		ui.Text(c, tr("empty")).FontSize(12).TextColor(pal.T3).Margin(12, 8)
	}
}

// stateDot 是 6px 的状态点：开启为强调色，出错为红色，其余为灰色。
func (w *workbench) stateDot(c *ui.Context, v engine.View) {
	pal := w.pal
	col := pal.L4
	switch v.State {
	case engine.On:
		col = pal.Accent
		if pal.Ink {
			col = pal.FG
		}
	case engine.Error:
		col = pal.Red
	case engine.Running, engine.Pending:
		col = pal.Amber
	}
	if v.Type == contract.Action && v.State != engine.Running && v.State != engine.Error {
		col = pal.L3
	}
	ui.Box(c).Size(6, 6).Radius(3).Shrink(0).Background(col)
}

func itoa(n int) string { return strconv.Itoa(n) }

// unsaved 报告是否有未保存的修改。
func (w *workbench) unsaved() bool {
	for _, d := range w.docs {
		if d.modified() {
			return true
		}
	}
	return false
}

// saveAll 保存全部修改；有任何一个失败就停在那个开关上。
func (w *workbench) saveAll() bool {
	for id, d := range w.docs {
		if d.modified() && !w.save(d) {
			w.open(secSwitches, id)
			return false
		}
	}
	return true
}

// saveCurrent 保存当前打开的脚本（菜单与 ⌘S）。
func (w *workbench) saveCurrent() {
	if d := w.currentDoc(); d != nil && w.section == secSwitches && d.modified() {
		w.save(d)
	}
}

type appInfo struct{ ID, Name string }
