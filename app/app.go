package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/api"
	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/paths"
	"github.com/HanlunWang/open-toggle/internal/platform/darwin"
	"github.com/HanlunWang/open-toggle/internal/presets"
	"github.com/HanlunWang/open-toggle/internal/store"
	"github.com/HanlunWang/open-toggle/internal/version"
)

// Run 启动菜单栏 app，直到用户退出。
func Run() error {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil // 已有实例在运行；它会收到第二个实例的启动
	}
	mygo.App.OnWindowAllClosed(func() {}) // 面板是唯一的窗口，关掉它 app 照常运行
	// 界面语言跟随系统的首选语言（App.Locale 返回的是 app 已翻译的语言，M5 接入 i18n 前恒为英文）
	if langs := mygo.App.PreferredLanguages(); len(langs) > 0 {
		setLanguage(langs[0])
	}

	st, err := store.Open(paths.Data())
	if err != nil {
		return err
	}
	// OPENTOGGLE_NO_V1_IMPORT=1：开发与验收时的隔离实例不读 v1 的期望状态（否则会按它去开关真实系统）
	if os.Getenv("OPENTOGGLE_NO_V1_IMPORT") == "1" {
		// 跳过
	} else if v, err := store.ReadV1(store.V1Domain); err == nil {
		if _, err := st.ImportV1(v); err != nil {
			log.Println("v1 import:", err)
		}
	}
	prefs := loadPrefs(st)
	switch prefs.Theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	}

	exe, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	eng := engine.New(engine.Config{
		ScriptsDir: paths.Scripts(), LedgerPath: paths.Ledger(), Bin: exe, Store: st,
		Trash: darwin.Trash, WatchInterval: 2 * time.Second,
	})
	if err := eng.Seed(presets.All()); err != nil {
		log.Println("seed:", err)
	}
	srv := api.New(eng, api.Options{Port: paths.Port(), DiscoveryPath: paths.Discovery(), Version: version.Version, Store: st})
	// API 先于状态恢复启动：恢复拉起的 daemon 可能立刻调用 `opentoggle press`
	apiErr := srv.Start()
	if apiErr != nil {
		log.Println(apiErr)
	}
	if err := eng.Start(); err != nil {
		srv.Close()
		return err
	}

	c := &controller{eng: eng, srv: srv, st: st, prefs: prefs, apiErr: apiErr, extras: map[string]*extraItem{}}
	mygo.App.WhenReady(c.ready)
	mygo.App.OnSecondInstance(func([]string, string) { c.togglePanel() })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { c.shutdown() })
	return mygo.App.Run()
}

type extraItem struct {
	tray *mygo.Tray
	key  string // 图标指纹，变了才重设
}

type controller struct {
	eng    *engine.Engine
	srv    *api.Server
	st     *store.Store
	prefs  Prefs
	apiErr error

	tray     *mygo.Tray
	flyout   *mygo.Window
	panel    *panel
	extras   map[string]*extraItem
	trayKey  string
	titleKey string
	phase    bool

	devPanel bool

	tickMu  sync.Mutex
	ticking bool
	stop    chan struct{}
	once    sync.Once
}

func (c *controller) ready() {
	c.stop = make(chan struct{})
	c.panel = newPanel(c.eng, c.prefs)
	c.panel.openScripts = func() { _ = mygo.Shell.OpenPath(c.eng.ScriptsDir()) }
	c.panel.quit = mygo.App.Quit
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
		ID: "main", Icon: trayIconPNG(c.prefs.MenubarStyle, 0, false), IconIsTemplate: true, ToolTip: "OpenToggle",
	})
	if err != nil {
		log.Println("tray:", err)
		return
	}
	c.tray.OnClick(c.togglePanel)
	c.tray.OnRightClick(c.contextMenu)
	if c.apiErr != nil {
		c.tray.SetToolTip("OpenToggle — " + fmt.Sprintf(tr("api_failed"), c.apiErr))
	}

	events, cancel := c.eng.Subscribe()
	go func() {
		defer cancel()
		for {
			select {
			case <-c.stop:
				return
			case _, ok := <-events:
				if !ok {
					return
				}
				// 合并一批事件后再刷新一次
				drain(events)
				mygo.RunOnMain(func() {
					c.panel.invalidate()
					c.redrawPanel()
					c.syncMenubar()
				})
			}
		}
	}()
	c.syncMenubar()
	// OPENTOGGLE_SHOW_PANEL=1：启动后自动打开面板（开发时截图用，面板不随失焦关闭）
	if os.Getenv("OPENTOGGLE_SHOW_PANEL") == "1" {
		c.devPanel = true
		go func() {
			time.Sleep(1500 * time.Millisecond)
			mygo.RunOnMain(c.togglePanel)
		}()
	}
}

func drain(ch <-chan engine.Event) {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		default:
			return
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
	c.panel.lastHeight = 0
	c.panel.invalidate()
	c.flyout = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray: c.tray, Placement: mygo.PlacementBottom, Gap: 6,
		Width: int(w), Height: c.estimateHeight(),
		Focusable: !c.devPanel, Shadow: true, Vibrancy: mygo.VibrancyPopover, CornerRadius: 16,
		Content: ui.View(c.panel.view),
	})
	fl := c.flyout
	c.panel.resize = func(h float32) {
		maxH := float32(800)
		if d := mygo.Screen.PrimaryDisplay(); d.WorkArea.Height > 0 {
			maxH = float32(d.WorkArea.Height) - 40
		}
		height := int(min(h, maxH) + 0.5)
		go mygo.RunOnMain(func() {
			if c.flyout == fl {
				fl.SetSize(int(w), height)
			}
		})
	}
	fl.OnClosed(func() {
		if c.flyout == fl {
			c.flyout = nil
			c.eng.PanelVisible(false)
		}
	})
	c.eng.PanelVisible(true)
}

// estimateHeight 是面板首次打开时的高度估计；第一帧之后按实际内容校正。
func (c *controller) estimateHeight() int {
	n := 0
	for _, v := range c.eng.List() {
		if !v.Hidden {
			n++
		}
	}
	return min(600, 110+max(1, n)*46)
}

func (c *controller) contextMenu() {
	if c.flyout != nil {
		c.flyout.Close()
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: tr("open_panel"), Click: func(*mygo.MenuItem, *mygo.Window) { c.togglePanel() }},
		{Label: tr("open_scripts"), Click: func(*mygo.MenuItem, *mygo.Window) { _ = mygo.Shell.OpenPath(c.eng.ScriptsDir()) }},
		{Label: tr("refresh"), Click: func(*mygo.MenuItem, *mygo.Window) { c.eng.Refresh() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
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
	iconKey := fmt.Sprintf("%s|%d|%v|%v", c.prefs.MenubarStyle, min(onCount, 3), busy && c.phase, onCount > 0)
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
			_ = c.tray.SetIcon(trayIconPNG(c.prefs.MenubarStyle, onCount, busy && c.phase), true)
		}
		switch {
		case c.prefs.MenubarExtra == "count" && onCount > 0:
			title = fmt.Sprint(onCount)
		case c.prefs.MenubarExtra == "countdown" && countdownSec != nil:
			title = countdown(*countdownSec)
		}
	}
	c.trayKey = iconKey
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
			opts := mygo.TrayOptions{ID: "extra-" + id, IconIsTemplate: true, ToolTip: v.Name + " — OpenToggle"}
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

	c.setTicking(countdownSec != nil || busy || anyCountdown(wantExtras) || (replace != nil && replace.Remaining != nil))
}

func anyCountdown(m map[string]engine.View) bool {
	for _, v := range m {
		if v.Remaining != nil {
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
func (c *controller) setTicking(on bool) {
	c.tickMu.Lock()
	defer c.tickMu.Unlock()
	if on == c.ticking {
		return
	}
	c.ticking = on
	if !on {
		return
	}
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
				c.tickMu.Lock()
				still := c.ticking
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
		c.eng.Shutdown()
		c.srv.Close()
		for _, x := range c.extras {
			x.tray.Destroy()
		}
		if c.tray != nil {
			c.tray.Destroy()
		}
	})
}
