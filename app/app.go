package app

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/api"
	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/paths"
	"github.com/HanlunWang/simple-toggle/internal/platform/darwin"
	"github.com/HanlunWang/simple-toggle/internal/presets"
	"github.com/HanlunWang/simple-toggle/internal/store"
	"github.com/HanlunWang/simple-toggle/internal/version"
)

// Run 启动菜单栏 app，直到用户退出。
func Run() error {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil // 已有实例在运行；它会收到第二个实例的启动
	}
	mygo.App.OnWindowAllClosed(func() {}) // 关掉所有窗口后 app 照常在菜单栏运行
	mygo.App.SetLanguages("en", "zh-Hans")

	st, err := store.Open(paths.Data())
	if err != nil {
		return err
	}
	// 更名前（OpenToggle）的脚本目录：第一次启动时复制过来
	if n, err := paths.MigrateLegacyScripts(); err != nil {
		log.Println("migrate scripts:", err)
	} else if n > 0 {
		log.Printf("copied %d scripts from %s", n, paths.LegacyConfig())
	}
	// SIMPLETOGGLE_NO_V1_IMPORT=1：开发与验收时的隔离实例不读 v1 的期望状态（否则会按它去开关真实系统）
	if os.Getenv("SIMPLETOGGLE_NO_V1_IMPORT") == "1" {
		// 跳过
	} else if v, err := store.ReadV1(store.V1Domain); err == nil {
		if _, err := st.ImportV1(v); err != nil {
			log.Println("v1 import:", err)
		}
	}
	// 语言：设置里选了就用它，否则跟随系统（mygo 自带的文案也随之切换）
	if lang := validLanguage(st.Setting(keyLanguage)); lang != "" {
		mygo.App.SetLocale(lang)
	}
	setLanguage(mygo.App.Locale())
	prefs := loadPrefs(st)
	applyTheme(prefs.Theme)
	if days, err := strconv.Atoi(st.Setting(keyRetention)); err == nil && days > 0 {
		_ = st.SetRetention(time.Duration(days) * 24 * time.Hour)
	}

	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	scriptsDir := paths.Scripts()
	if dir := st.Setting(keyScriptsDir); dir != "" && os.Getenv("SIMPLETOGGLE_HOME") == "" {
		scriptsDir = dir
	}
	timeout, shell := engineDefaults(st)
	eng := engine.New(engine.Config{
		ScriptsDir: scriptsDir, LedgerPath: paths.Ledger(), Bin: exe, Store: st,
		Trash: darwin.Trash, WatchInterval: 2 * time.Second, Guard: true,
		DefaultTimeout: timeout, Shell: shell, ExtraEnv: func() []string { return globalEnv(st) },
		NoRestore: st.Setting(keyRestore) == "false",
	})
	if err := eng.Seed(presets.All()); err != nil {
		log.Println("seed:", err)
	}
	if up := eng.UpgradePresets(presets.Upgrades()); len(up) > 0 {
		log.Println("upgraded preset scripts:", up)
	}
	srv := api.New(eng, api.Options{Port: paths.Port(), DiscoveryPath: paths.Discovery(), Version: version.Version, Store: st})
	// API 先于状态恢复启动：恢复拉起的 daemon 可能立刻调用 `simpletoggle press`
	var apiErr error
	apiOn := st.Setting(keyAPI) != "false"
	if apiOn {
		if apiErr = srv.Start(); apiErr != nil {
			log.Println(apiErr)
			apiOn = false
		}
	}
	desiredBefore := st.DesiredAll()
	if err := eng.Start(); err != nil {
		srv.Close()
		return err
	}
	unclean := beginSession()

	c := &controller{eng: eng, srv: srv, st: st, prefs: prefs, apiErr: apiErr, apiOn: apiOn, exe: exe, extras: map[string]*extraItem{}}
	if unclean {
		c.crashDesired = desiredNames(eng, desiredBefore)
		c.unclean = true
	}
	c.auto = automation.New(automation.Config{
		Engine: eng, Store: st, Host: appHost{},
		Describe:   func(r store.Rule) string { return ruleText(r, c.targetName) },
		FlapNotice: func(r store.Rule) string { return fmt.Sprintf(tr("flap_notice"), ruleText(r, c.targetName)) },
	})
	mygo.App.WhenReady(c.ready)
	mygo.App.OnSecondInstance(func([]string, string) { c.togglePanel() })
	mygo.App.OnOpenURL(func(u string) { mygo.RunOnMain(func() { c.handleDeepLink(u) }) })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { c.shutdown() })
	return mygo.App.Run()
}

type extraItem struct {
	tray *mygo.Tray
	key  string // 图标指纹，变了才重设
}

type controller struct {
	auto   *automation.Manager
	eng    *engine.Engine
	srv    *api.Server
	st     *store.Store
	prefs  Prefs
	apiErr error
	apiOn  bool
	exe    string

	tray     *mygo.Tray
	flyout   *mygo.Window
	panel    *panel
	extras   map[string]*extraItem
	trayKey  string
	titleKey string
	phase    bool

	devPanel bool

	unclean      bool     // 上次没有正常退出
	crashDesired []string // 上次退出时开着的开关

	appsOnce sync.Once
	apps     []appInfo

	// 设置、引导与其他窗口
	st_            *settingsWin
	stWin          *mygo.Window
	obWin          *mygo.Window
	windows        int
	appShortcuts   []string
	appShortcutErr map[string]string
	notified       map[string]bool

	wb           *workbench
	wbWin        *mygo.Window
	wbAllowClose bool

	tickMu     sync.Mutex
	ticking    bool
	tickGen    int
	tickPeriod time.Duration
	stop       chan struct{}
	once       sync.Once
}

func (c *controller) ready() {
	c.stop = make(chan struct{})
	c.panel = newPanel(c.eng, c.prefs)
	c.panel.openScripts = func() { _ = mygo.Shell.OpenPath(c.eng.ScriptsDir()) }
	c.panel.openWorkbench = func(id string) { c.openWorkbench(secSwitches, id) }
	c.panel.openSettings = func() { c.openSettings("") }
	c.panel.quit = c.requestQuit
	c.panel.axTrusted = darwin.AccessibilityTrusted
	c.panel.reduceTransparency = func() bool { return mygo.Theme.Preferences().ReduceTransparency }
	c.panel.requestAX = func() {
		if !darwin.RequestAccessibility() {
			_ = mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
		}
		c.panel.invalidate()
	}
	markFlashOnMain = func(fn func()) {
		mygo.RunOnMain(func() {
			fn()
			c.redrawPanel()
		})
	}

	var err error
	c.tray, err = mygo.NewTray(mygo.TrayOptions{
		ID: "main", Icon: trayIconPNG(c.prefs.MenubarStyle, 0, false), IconIsTemplate: true, ToolTip: "SimpleToggle",
	})
	if err != nil {
		log.Println("tray:", err)
		return
	}
	c.tray.OnClick(c.togglePanel)
	c.tray.OnRightClick(c.contextMenu)
	if c.apiErr != nil {
		c.tray.SetToolTip("SimpleToggle — " + fmt.Sprintf(tr("api_failed"), c.apiErr))
	}

	c.startAutomation()
	c.notified = map[string]bool{}
	if c.unclean {
		c.reportUncleanExit(c.crashDesired)
	}
	c.registerAppShortcuts()
	mygo.App.OnLocaleChanged(func(loc string) {
		setLanguage(loc)
		c.redrawAll()
		c.titleKey = "-"
		c.syncMenubar()
	})
	// 第一次启动显示引导（SIMPLETOGGLE_SKIP_ONBOARDING=1 跳过，SIMPLETOGGLE_SHOW_ONBOARDING=1 强制显示）
	if (c.st.Setting(keyOnboarded) != "true" && os.Getenv("SIMPLETOGGLE_SKIP_ONBOARDING") != "1") || os.Getenv("SIMPLETOGGLE_SHOW_ONBOARDING") == "1" {
		go func() {
			time.Sleep(800 * time.Millisecond)
			mygo.RunOnMain(c.openOnboarding)
		}()
	}
	if tab := os.Getenv("SIMPLETOGGLE_SHOW_SETTINGS"); tab != "" {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			mygo.RunOnMain(func() { c.openSettings(tab) })
		}()
	}

	events, cancel := c.eng.Subscribe()
	go func() {
		defer cancel()
		for {
			select {
			case <-c.stop:
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				// 合并一批事件后再刷新一次
				batch := append([]engine.Event{ev}, drain(events)...)
				mygo.RunOnMain(func() {
					c.panel.invalidate()
					c.redrawPanel()
					c.syncMenubar()
					reload := false
					for _, e := range batch {
						switch e.Kind {
						case engine.EventScene, engine.EventReloaded:
							reload = true // 场景的快捷键或开关列表变了
						case engine.EventRun:
							c.notifyRun(e.Run)
						case engine.EventChange:
							c.notifyChange(e.Switch)
						}
					}
					if reload {
						c.auto.Reload()
					}
					if c.stWin != nil {
						c.stWin.Invalidate()
					}
					if c.wb != nil {
						for _, e := range batch {
							c.wb.handle(e)
						}
						if c.wbWin != nil {
							c.wbWin.Invalidate()
						}
					}
				})
			}
		}
	}()
	c.syncMenubar()
	// SIMPLETOGGLE_SHOW_WORKBENCH=1：启动后自动打开工作台（开发时截图用）
	if os.Getenv("SIMPLETOGGLE_SHOW_WORKBENCH") == "1" {
		go func() {
			time.Sleep(1200 * time.Millisecond)
			mygo.RunOnMain(func() { c.openWorkbench(os.Getenv("SIMPLETOGGLE_WB_SECTION"), os.Getenv("SIMPLETOGGLE_WB_ITEM")) })
		}()
	}
	// SIMPLETOGGLE_SHOW_PANEL=1：启动后自动打开面板（开发时截图用，面板不随失焦关闭）
	if os.Getenv("SIMPLETOGGLE_SHOW_PANEL") == "1" {
		c.devPanel = true
		go func() {
			time.Sleep(1500 * time.Millisecond)
			mygo.RunOnMain(c.togglePanel)
		}()
	}
}

func drain(ch <-chan engine.Event) []engine.Event {
	var out []engine.Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

func (c *controller) redrawPanel() {
	if c.flyout != nil {
		c.flyout.Invalidate()
	}
}

func (c *controller) togglePanel() {
	if c.flyout != nil {
		c.flyout.Close()
		return
	}
	w := c.prefs.width()
	maxH := float32(800)
	if d := mygo.Screen.PrimaryDisplay(); d.WorkArea.Height > 0 {
		maxH = float32(d.WorkArea.Height) - 40
	}
	// 用上次实测的高度打开（内容没变时就是准确的）；第一次用估计值。
	// 窗口先完全透明，等按真实高度画好一帧再淡入，所以不会看到尺寸跳动。
	height := c.panel.lastHeight
	if height <= 0 {
		height = float32(c.estimateHeight())
	}
	height = min(height, maxH)
	p := c.panel
	p.invalidate()
	p.expanded, p.searching, p.query = "", false, ""
	p.edits = map[string]string{} // 上次没提交的参数输入不带到下一次
	p.revealed, p.targetH = false, height
	c.flyout = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray: c.tray, Placement: mygo.PlacementBottom, Gap: 6,
		Width: int(w), Height: int(height + 0.5),
		Focusable: !c.devPanel, Shadow: true, Vibrancy: mygo.VibrancyPopover, CornerRadius: 16,
		Content: ui.View(p.view),
	})
	fl := c.flyout
	fl.SetOpacity(0)
	shown := false
	reveal := func() { // 主线程
		if shown || c.flyout != fl {
			return
		}
		shown = true
		p.revealed = true
		go fadeIn(fl, p.lessMotion)
	}
	p.reveal = func() { go mygo.RunOnMain(reveal) }
	// 兜底：万一没有等到按真实高度画的那一帧（例如窗口透明时系统不让它绘制），也要显示出来
	time.AfterFunc(300*time.Millisecond, func() { mygo.RunOnMain(reveal) })
	p.resize = func(h float32) float32 {
		height := int(min(h, maxH) + 0.5)
		go mygo.RunOnMain(func() {
			if c.flyout == fl {
				fl.SetSize(int(w), height)
			}
		})
		return float32(height)
	}
	fl.OnClosed(func() {
		if c.flyout == fl {
			c.flyout = nil
			c.eng.PanelVisible(false)
		}
	})
	c.eng.PanelVisible(true)
}

// fadeIn 把面板窗口在约 150ms 内从透明淡入（减弱动态效果时直接显示）。
func fadeIn(fl *mygo.Window, instant bool) {
	const d = 150 * time.Millisecond
	start := time.Now()
	for !instant {
		t := float64(time.Since(start)) / float64(d)
		if t >= 1 {
			break
		}
		a := 1 - (1-t)*(1-t)*(1-t) // ease-out
		mygo.RunOnMain(func() { fl.SetOpacity(a) })
		time.Sleep(8 * time.Millisecond)
	}
	mygo.RunOnMain(func() { fl.SetOpacity(1) })
}

// estimateHeight 是面板首次打开时的高度估计；第一帧之后按实际内容校正。
func (c *controller) estimateHeight() int {
	n := 0
	for _, v := range c.eng.List() {
		if !v.Hidden {
			n++
		}
	}
	// 与面板的固定尺寸一致：顶栏 46 + 分隔线 + 列表上下留白 10 + 每行 + 底栏 39
	row := 50
	switch {
	case c.prefs.Layout == "grid":
		return min(600, 96+(max(1, n)+1)/2*84)
	case c.prefs.Density == "compact":
		row = 34
	}
	return min(600, 96+max(1, n)*row)
}

func (c *controller) contextMenu() {
	if c.flyout != nil {
		c.flyout.Close()
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: tr("open_panel"), Click: func(*mygo.MenuItem, *mygo.Window) { c.togglePanel() }},
		{Label: tr("open_workbench"), Click: func(*mygo.MenuItem, *mygo.Window) { c.openWorkbench("", "") }},
		{Label: tr("open_settings"), Click: func(*mygo.MenuItem, *mygo.Window) { c.openSettings("") }},
		{Label: tr("open_scripts"), Click: func(*mygo.MenuItem, *mygo.Window) { _ = mygo.Shell.OpenPath(c.eng.ScriptsDir()) }},
		{Label: tr("refresh"), Click: func(*mygo.MenuItem, *mygo.Window) { c.eng.Refresh() }},
		mygo.Separator(),
		{Label: tr("quit_menu"), Click: func(*mygo.MenuItem, *mygo.Window) { c.requestQuit() }},
	})
	c.tray.SetMenu(menu)
	defer c.tray.SetMenu(nil)
	c.tray.PopUpMenu()
}

// ───────── 菜单栏图标 ─────────

// syncMenubar 根据开关状态更新主图标与单独显示的图标；只在内容变化时调用系统接口。
func (c *controller) syncMenubar() {
	if c.tray == nil {
		return
	}
	views := c.eng.List()
	onCount, busy := 0, false
	var countdownSec *int
	var replace *engine.View
	wantExtras := map[string]engine.View{}
	for i := range views {
		v := views[i]
		if v.Type != contract.Action && v.State == engine.On {
			onCount++ // 含隐藏的开关：主图标回答的是「有没有东西在跑」
		}
		if v.State == engine.Pending || v.State == engine.Running {
			busy = true
		}
		// 已经在自己的菜单栏图标上显示倒计时的开关不再重复显示在主图标上；
		// 其余的取最先到期的那个
		ownCountdown := v.MenuBar != nil && v.MenuBar.Countdown &&
			(v.MenuBar.Mode == contract.MenuBarAdd || v.MenuBar.Mode == contract.MenuBarReplace)
		if v.Remaining != nil && !ownCountdown && (countdownSec == nil || *v.Remaining < *countdownSec) {
			countdownSec = v.Remaining
		}
		if v.MenuBar != nil && v.State == engine.On {
			if v.MenuBar.Mode == contract.MenuBarReplace && replace == nil {
				replace = &views[i]
			} else if v.MenuBar.Mode == contract.MenuBarAdd {
				wantExtras[v.ID] = v
			}
		}
	}

	// 主图标
	blink := busy && c.phase && c.prefs.BusyAnim // 运行中动画可以在设置里关掉
	iconKey := fmt.Sprintf("%s|%d|%v|%v", c.prefs.MenubarStyle, min(onCount, 3), blink, onCount > 0)
	title := ""
	if replace != nil {
		kind, val := contract.ResolveIcon(replace.MenuBar.Icon)
		iconKey = "replace|" + replace.MenuBar.Icon
		if kind == contract.IconEmoji {
			title = val
		}
		if replace.Remaining != nil && replace.MenuBar.Countdown {
			title = joinTitle(title, countdown(*replace.Remaining))
		}
		if iconKey != c.trayKey {
			if kind == contract.IconLucide {
				_ = c.tray.SetIcon(templatePNG(val, 32), true)
			} else {
				_ = c.tray.SetIcon(nil, true)
			}
		}
	} else {
		if iconKey != c.trayKey {
			_ = c.tray.SetIcon(trayIconPNG(c.prefs.MenubarStyle, onCount, blink), true)
		}
		switch {
		case c.prefs.MenubarExtra == "count" && onCount > 0:
			title = fmt.Sprint(onCount)
		case c.prefs.MenubarExtra == "countdown" && countdownSec != nil:
			title = countdown(*countdownSec)
		}
	}
	c.trayKey = iconKey
	for _, sv := range c.eng.Scenes() {
		if sv.Active && sv.ShowName {
			title = joinTitle(sv.Name, title) // 激活中的场景名显示在主图标旁
			break
		}
	}
	if title != c.titleKey {
		c.tray.SetTitle(padTitle(title))
		c.titleKey = title
	}

	// 单独显示的图标
	for id, x := range c.extras {
		if _, ok := wantExtras[id]; !ok {
			x.tray.Destroy()
			delete(c.extras, id)
		}
	}
	for id, v := range wantExtras {
		kind, val := contract.ResolveIcon(v.MenuBar.Icon)
		x := c.extras[id]
		if x == nil {
			opts := mygo.TrayOptions{ID: "extra-" + id, IconIsTemplate: true, ToolTip: v.Name + " — SimpleToggle"}
			if kind == contract.IconLucide {
				opts.Icon = templatePNG(val, 32)
			} else {
				opts.Title = val
			}
			t, err := mygo.NewTray(opts)
			if err != nil {
				continue
			}
			sw := v
			t.OnClick(func() { c.extraMenu(t, sw) })
			x = &extraItem{tray: t}
			c.extras[id] = x
		}
		t := ""
		if kind == contract.IconEmoji {
			t = val
		}
		if v.Remaining != nil && v.MenuBar.Countdown {
			t = joinTitle(t, countdown(*v.Remaining))
		}
		if t != x.key {
			x.tray.SetTitle(padTitle(t))
			x.key = t
		}
	}

	// 只在菜单栏真的显示着倒计时（每秒一次）或运行中动画（每半秒一次）时刷新，其余时间不唤醒
	shownCountdown := (replace == nil && c.prefs.MenubarExtra == "countdown" && countdownSec != nil) ||
		anyCountdown(wantExtras) || (replace != nil && replace.Remaining != nil && replace.MenuBar.Countdown)
	switch {
	case busy && c.prefs.BusyAnim:
		c.setTicking(500 * time.Millisecond)
	case shownCountdown:
		c.setTicking(time.Second)
	default:
		c.setTicking(0)
	}
}

func anyCountdown(m map[string]engine.View) bool {
	for _, v := range m {
		if v.Remaining != nil && v.MenuBar != nil && v.MenuBar.Countdown {
			return true
		}
	}
	return false
}

func joinTitle(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + " " + b
}

// padTitle 让图标与文字之间留一点空隙（空标题时不加）。
func padTitle(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// extraMenu 是单独图标的点击菜单：显示开关名称并提供「关闭」。
func (c *controller) extraMenu(t *mygo.Tray, v engine.View) {
	off := "关闭 " + v.Name
	if strs["quit"] == "Quit" {
		off = "Turn off " + v.Name
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: v.Name, Disabled: true},
		mygo.Separator(),
		{Label: off, Click: func(*mygo.MenuItem, *mygo.Window) { _, _ = c.eng.SetSwitch(v.ID, false, engine.Manual, "menu bar") }},
	})
	t.SetMenu(menu)
	defer t.SetMenu(nil)
	t.PopUpMenu()
}

// setTicking 只在需要（倒计时、运行中的动画）时每半秒刷新菜单栏，其余时间不唤醒。
// period 为 0 表示停止。
func (c *controller) setTicking(period time.Duration) {
	c.tickMu.Lock()
	defer c.tickMu.Unlock()
	if period == c.tickPeriod {
		return
	}
	c.tickPeriod = period
	c.ticking = period > 0
	c.tickGen++ // 关掉又马上打开（或换频率）时，旧的计时 goroutine 看到代数变了就退出，不会两个同时跑
	if period == 0 {
		return
	}
	gen := c.tickGen
	go func() {
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
				c.tickMu.Lock()
				still := c.ticking && c.tickGen == gen
				c.tickMu.Unlock()
				if !still {
					return
				}
				mygo.RunOnMain(func() {
					c.phase = !c.phase
					c.syncMenubar()
				})
			}
		}
	}()
}

func (c *controller) shutdown() {
	c.once.Do(func() {
		if c.stop != nil {
			close(c.stop)
		}
		c.auto.Stop()
		c.eng.Shutdown()
		c.srv.Close()
		for _, x := range c.extras {
			x.tray.Destroy()
		}
		if c.tray != nil {
			c.tray.Destroy()
		}
		endSession()
	})
}

// ───────── 工作台窗口 ─────────

// openWorkbench 打开（或切到）工作台。打开期间 app 显示在 Dock 里、有菜单栏，
// 方便用 ⌘Tab 切换与使用编辑菜单；关闭后回到纯菜单栏 app。
func (c *controller) openWorkbench(section, item string) {
	if c.flyout != nil && !c.devPanel {
		c.flyout.Close()
	}
	if c.wb == nil {
		c.wb = newWorkbench(c.eng, c.prefs)
		c.wb.onMain = func(fn func()) {
			mygo.RunOnMain(func() {
				fn()
				if c.wbWin != nil {
					c.wbWin.Invalidate()
				}
			})
		}
		c.wb.openPath = c.openInEditor
		c.wb.reveal = func(path string) { _ = exec.Command("/usr/bin/open", "-R", path).Start() }
		c.wb.reduceTransparency = func() bool { return mygo.Theme.Preferences().ReduceTransparency }
		c.wb.exportRuns = c.exportRuns
		c.wb.auto = c.auto
		c.wb.revealDir = func(p string) { _ = mygo.Shell.OpenPath(p) }
		c.wb.openSettings = func() { c.openSettings("") }
		c.wb.installedApps = c.installedApps
		c.wb.network = currentNetwork
		c.wb.pickPath = func(done func(string)) {
			win := c.wbWin
			go func() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Directory: true, CreateDirectories: true})
				c.wb.onMain(func() {
					if err == nil && len(paths) > 0 {
						done(paths[0])
					}
				})
			}()
		}
		c.wb.closeWin = func() {
			if c.wbWin != nil {
				c.wbAllowClose = true
				c.wbWin.Close()
			}
		}
	}
	c.wb.prefs = c.prefs
	c.wb.open(section, item)
	c.wb.invalidate()
	if c.wbWin != nil {
		c.wbWin.Show()
		c.wbWin.Focus()
		mygo.App.Focus()
		c.wbWin.Invalidate()
		return
	}
	c.windowOpened()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "SimpleToggle " + tr("workbench"), Width: 1180, Height: 760, MinWidth: 860, MinHeight: 560,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancySidebar, StateKey: "workbench",
		Content: ui.View(c.wb.render),
	})
	c.wbWin, c.wbAllowClose = win, false
	c.eng.PanelVisible(true) // 工作台打开时状态刷新得更勤
	win.OnClose(func(e *mygo.CloseEvent) {
		if !c.wbAllowClose && c.wb.unsaved() {
			e.PreventDefault()
			c.wb.closeAsk = true
			win.Invalidate()
		}
	})
	win.OnClosed(func() {
		if c.wbWin == win {
			c.wbWin = nil
			c.eng.PanelVisible(false)
			c.windowClosed()
		}
	})
	mygo.App.Focus()
}

// exportRuns 让用户选择位置，把运行记录写成 JSON。对话框会阻塞，所以放在后台。
func (c *controller) exportRuns(runs []store.Run) {
	win := c.wbWin
	go func() {
		name := "simpletoggle-runs-" + time.Now().Format("20060102-1504") + ".json"
		home, _ := os.UserHomeDir()
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: win, DefaultPath: filepath.Join(home, "Downloads", name),
			Filters: []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}},
		})
		if err == nil && path != "" {
			var data []byte
			data, err = json.MarshalIndent(runs, "", "  ")
			if err == nil {
				err = os.WriteFile(path, append(data, '\n'), 0o644)
			}
		}
		c.wb.onMain(func() {
			switch {
			case err != nil:
				c.wb.setNotice(fmt.Sprintf(tr("save_failed"), err), true)
			case path != "":
				c.wb.setNotice(fmt.Sprintf(tr("act_exported"), filepath.Base(path)), false)
			}
		})
	}()
}

// ───────── 自动化 ─────────

// startAutomation 启动规则求值，并把 mygo 才有的系统事件送进去。
func (c *controller) startAutomation() {
	c.auto.Start()
	c.auto.Fire(automation.Event{Kind: "displays", Value: strconv.Itoa(len(mygo.Screen.Displays()))})
	mygo.Screen.OnDisplaysChanged(func() {
		c.auto.Fire(automation.Event{Kind: "displays", Value: strconv.Itoa(len(mygo.Screen.Displays()))})
	})
	mygo.Power.OnResume(func() { c.auto.Fire(automation.Event{Kind: automation.KindWake, Event: "wake"}) })
	mygo.Power.OnUnlockScreen(func() { c.auto.Fire(automation.Event{Kind: automation.KindWake, Event: "unlock"}) })
	mygo.Network.OnChanged(func(mygo.NetworkStatus) { c.auto.Fire(automation.Event{Kind: "network"}) })
	if mygo.App.WasOpenedAtLogin() {
		c.auto.Fire(automation.Event{Kind: automation.KindWake, Event: "login"})
	}
}

// targetName 返回规则目标（开关或场景）的显示名。
func (c *controller) targetName(verb, id string) string {
	if verb == "scene" {
		if sv, ok := c.eng.Scene(id); ok {
			return sv.Name
		}
		return id + tr("rule_missing")
	}
	if v, ok := c.eng.Get(id); ok {
		return v.Name
	}
	return id + tr("rule_missing")
}

// installedApps 返回已安装的应用（第一次调用时在后台读取，之后用缓存）。
func (c *controller) installedApps() []appInfo {
	c.appsOnce.Do(func() {
		go func() {
			list, err := mygo.Shell.InstalledApps()
			if err != nil {
				return
			}
			out := make([]appInfo, 0, len(list))
			for _, a := range list {
				out = append(out, appInfo{ID: a.ID, Name: a.Name})
			}
			slices.SortFunc(out, func(a, b appInfo) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
			mygo.RunOnMain(func() {
				c.apps = out
				if c.wbWin != nil {
					c.wbWin.Invalidate()
				}
			})
		}()
	})
	return c.apps
}

// currentNetwork 返回当前网络的指纹与显示名（有定位授权时用 Wi-Fi 名称）。
func currentNetwork() (string, string) {
	fp, err := darwin.NetworkFingerprint()
	if err != nil || fp.Gateway == "" {
		return "", ""
	}
	label := ""
	if _, ssid, err := darwin.WiFi(); err == nil && ssid != "" {
		label = ssid
	} else if fp.Domain != "" {
		label = fp.Domain
	} else if len(fp.DNSDomains) > 0 {
		label = fp.DNSDomains[0]
	} else {
		label = fmt.Sprintf(tr("wifi_named"), fp.Gateway)
	}
	return fp.Key(), label
}
