package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans" // mygo 自带文案（搜索框、编辑器菜单等）的简体中文
	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/integrations"
	"github.com/HanlunWang/simple-toggle/internal/platform/darwin"
	"github.com/HanlunWang/simple-toggle/internal/store"
	"github.com/HanlunWang/simple-toggle/internal/version"
)

// 设置、首次引导、深链接、通知与全局快捷键（M5）。

func applyTheme(theme string) {
	switch theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// engineDefaults 读取默认超时与 shell 设置。
func engineDefaults(st *store.Store) (time.Duration, string) {
	timeout := 30 * time.Second
	if n, err := strconv.Atoi(st.Setting(keyTimeout)); err == nil && n > 0 {
		timeout = time.Duration(n) * time.Second
	}
	return timeout, st.Setting(keyShell)
}

// globalEnv 是设置里的全局环境变量（每次执行脚本时读取，改动立即生效）。
func globalEnv(st *store.Store) []string {
	var out []string
	for _, kv := range envVars(st.Setting(keyEnv)) {
		out = append(out, kv[0]+"="+kv[1])
	}
	return out
}

// ───────── 窗口与菜单 ─────────

// windowOpened / windowClosed 记录打开的窗口数：有窗口时 app 出现在 Dock、有菜单栏，
// 全部关闭后回到纯菜单栏 app。
func (c *controller) windowOpened() {
	c.windows++
	if c.windows == 1 {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
		c.setAppMenu()
	}
}

func (c *controller) windowClosed() {
	if c.windows > 0 {
		c.windows--
	}
	if c.windows == 0 {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	}
}

func (c *controller) setAppMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: tr("menu_file"), Submenu: []*mygo.MenuItem{
			{Label: tr("save"), Accelerator: "CmdOrCtrl+S", Click: func(*mygo.MenuItem, *mygo.Window) {
				if c.wb != nil {
					c.wb.saveCurrent()
					c.redrawAll()
				}
			}},
			{Label: tr("open_settings"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { c.openSettings("") }},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu},
	}))
}

func (c *controller) redrawAll() {
	c.panel.invalidate()
	c.redrawPanel()
	if c.wb != nil {
		c.wb.invalidate()
	}
	for _, w := range []*mygo.Window{c.wbWin, c.stWin, c.obWin} {
		if w != nil {
			w.Invalidate()
		}
	}
}

// ───────── 设置的读写与生效 ─────────

func (c *controller) setSetting(key, value string) {
	_ = c.st.SetSetting(key, value)
	switch {
	case strings.HasPrefix(key, "ui."):
		c.applyPrefs()
	case key == keyLanguage:
		mygo.App.SetLocale(validLanguage(value)) // OnLocaleChanged 里切换文案
	case key == keyTimeout || key == keyShell:
		c.eng.SetDefaults(engineDefaults(c.st))
	case key == keyRetention:
		if days, err := strconv.Atoi(value); err == nil && days > 0 {
			_ = c.st.SetRetention(time.Duration(days) * 24 * time.Hour)
		}
	case key == keyPanelShortcut || key == keyWorkbenchShortcut:
		c.registerAppShortcuts()
	case strings.HasPrefix(key, "automation."):
		c.auto.Reload()
	}
	c.redrawAll()
}

// applyPrefs 重新读取界面偏好并让面板、工作台与菜单栏图标跟上。
func (c *controller) applyPrefs() {
	c.prefs = loadPrefs(c.st)
	applyTheme(c.prefs.Theme)
	c.panel.prefs = c.prefs
	c.panel.lastHeight = 0
	if c.wb != nil {
		c.wb.prefs = c.prefs
	}
	c.trayKey, c.titleKey = "", "-"
	c.syncMenubar()
}

func (c *controller) resetSettings() {
	_ = c.st.ResetSettings()
	mygo.App.SetLocale("")
	c.eng.SetDefaults(engineDefaults(c.st))
	c.registerAppShortcuts()
	c.auto.Reload()
	c.applyPrefs()
	c.redrawAll()
}

// ───────── 全局快捷键（打开面板 / 工作台） ─────────

func (c *controller) registerAppShortcuts() {
	for _, a := range c.appShortcuts {
		mygo.GlobalShortcut.Unregister(a)
	}
	c.appShortcuts = nil
	c.appShortcutErr = map[string]string{}
	for key, fn := range map[string]func(){
		keyPanelShortcut:     c.togglePanel,
		keyWorkbenchShortcut: func() { c.openWorkbench("", "") },
	} {
		fn := fn
		accel, err := automation.NormalizeShortcut(c.st.Setting(key))
		if err != nil || accel == "" {
			continue
		}
		if err := mygo.GlobalShortcut.Register(accel, func() { mygo.RunOnMain(fn) }); err != nil {
			c.appShortcutErr[key] = tr("sc_taken")
			continue
		}
		c.appShortcuts = append(c.appShortcuts, accel)
	}
}

// ───────── 深链接 ─────────

func (c *controller) handleDeepLink(raw string) {
	l, err := parseDeepLink(raw)
	if err != nil {
		log.Printf("deep link %q: %v", raw, err)
		return
	}
	src, detail := engine.Source("deeplink"), raw
	switch l.Verb {
	case "panel":
		if c.flyout == nil {
			c.togglePanel()
		}
	case "workbench":
		c.openWorkbench(l.Section, l.Target)
	case "settings":
		c.openSettings(l.Section)
	case "scene":
		_, _ = c.eng.ToggleScene(l.Target, src, detail)
	case "run":
		_, _ = c.eng.Run(l.Target, src, detail)
	default:
		v, ok := c.eng.Get(l.Target)
		if !ok {
			return
		}
		if v.Type == contract.Action {
			_, _ = c.eng.Run(v.ID, src, detail)
			return
		}
		on := l.Verb == "on" || (l.Verb == "toggle" && v.State != engine.On)
		_, _ = c.eng.SetSwitch(v.ID, on, src, detail)
	}
}

// ───────── 通知 ─────────

// shouldNotify 决定一次运行是否发通知：手动、恢复、轮询与工作台测试不提醒；
// 其余按「完成时通知」设置（单个开关可覆盖）。
func shouldNotify(policy, override string, r store.Run) bool {
	if r.Detail == wbDetail || r.Action == "start" || r.Action == "stop" || r.Action == "status" {
		return false // 常驻进程的启停不打扰；意外退出记为 exit
	}
	switch engine.Source(r.Source) {
	case engine.Manual, engine.Restore, engine.Source("poll"):
		return false
	}
	if override != "" {
		policy = override
	}
	if policy == "" {
		policy = "fail"
	}
	return (!r.OK && policy != "never") || (r.OK && policy == "always")
}

// notifyRun 提醒非手动触发的运行结果。
func (c *controller) notifyRun(r *store.Run) {
	if r == nil {
		return
	}
	override := ""
	if !strings.HasPrefix(r.Switch, "scene:") {
		override = c.st.Meta(r.Switch).Notify
	}
	if !shouldNotify(c.st.Setting(keyNotify), override, *r) {
		return
	}
	body := strings.TrimSpace(lastLine(r.Output))
	if r.OK {
		appHost{}.Notify(fmt.Sprintf(tr("notif_done"), r.Name), body)
	} else {
		appHost{}.Notify(fmt.Sprintf(tr("notif_failed"), r.Name, r.ExitCode), body)
	}
}

// notifyChange 提醒有新的 AI 修改等待确认；点击通知打开工作台。
func (c *controller) notifyChange(id string) {
	ch, ok := c.eng.Change(id)
	if !ok || ch.Status != "pending" || c.notified[id] {
		return
	}
	c.notified[id] = true
	verb := tr("chg_verb_update")
	if ch.Kind == "create" {
		verb = tr("chg_verb_create")
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		Title: "SimpleToggle",
		Body:  fmt.Sprintf(tr("chg_notify"), clientLabel(ch), verb, ch.Name),
	})
	n.OnClick(func() {
		mygo.RunOnMain(func() {
			c.openWorkbench(secChanges, "")
			if c.wb != nil {
				c.wb.selChange = id
			}
		})
	})
	_ = n.Show()
}

// ───────── 外部编辑器 ─────────

var knownEditors = []struct{ ID, Name, App string }{
	{"com.todesktop.230313mzl4w4u92", "Cursor", "Cursor"},
	{"com.microsoft.VSCode", "Visual Studio Code", "Visual Studio Code"},
	{"dev.zed.Zed", "Zed", "Zed"},
	{"com.sublimetext.4", "Sublime Text", "Sublime Text"},
	{"com.panic.Nova", "Nova", "Nova"},
	{"com.apple.dt.Xcode", "Xcode", "Xcode"},
	{"com.apple.TextEdit", "TextEdit", "TextEdit"},
}

func installedEditors() []editorChoice {
	var out []editorChoice
	for _, e := range knownEditors {
		for _, dir := range []string{"/Applications", "/System/Applications", filepath.Join(homeDir(), "Applications")} {
			if _, err := os.Stat(filepath.Join(dir, e.App+".app")); err == nil {
				out = append(out, editorChoice{e.ID, e.Name})
				break
			}
		}
	}
	return out
}

// openInEditor 用设置里选的编辑器打开脚本；没选时用系统默认文本编辑器（不会执行脚本）。
func (c *controller) openInEditor(path string) {
	if id := c.prefs.ExternalEditor; id != "" {
		if err := exec.Command("/usr/bin/open", "-b", id, path).Run(); err == nil {
			return
		}
	}
	_ = exec.Command("/usr/bin/open", "-t", path).Start()
}

// ───────── AI 集成 ─────────

// integrationEnv 是一键接入用的环境。PATH 上的 simpletoggle 指向这个 app 时写它的路径
// （例如 Homebrew 的 /opt/homebrew/bin/simpletoggle，升级后不变），否则写 app 里的二进制。
func (c *controller) integrationEnv() integrations.Env {
	bin := c.exe
	if p := cliOnPath(); p != "" {
		if r, err := filepath.EvalSymlinks(p); err == nil && r == c.exe {
			bin = p
		}
	}
	return integrations.Default(bin)
}

// skillSource 是随 app 发布的 skill 目录（mygo.json 的 resources 打进 Contents/Resources/skills；SIMPLETOGGLE_SKILL_DIR 可覆盖）。
func (c *controller) skillSource() string {
	if d := os.Getenv("SIMPLETOGGLE_SKILL_DIR"); d != "" {
		return d
	}
	if dir, err := mygo.App.Path(mygo.PathResources); err == nil {
		return filepath.Join(dir, "skills", "simpletoggle")
	}
	return filepath.Join(filepath.Dir(c.exe), "..", "Resources", "skills", "simpletoggle")
}

func cliOnPath() string {
	if p, err := exec.LookPath("simpletoggle"); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(homeDir(), ".local", "bin")} {
		p := filepath.Join(dir, "simpletoggle")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ───────── 导入导出 ─────────

func (c *controller) exportConfig(win *mygo.Window, done func(string, error)) {
	x := c.st.ExportState()
	x.Scripts = map[string]string{}
	for _, v := range c.eng.List() {
		if s, err := c.eng.Script(v.ID); err == nil {
			x.Scripts[v.ID] = s
		}
	}
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: win, DefaultPath: filepath.Join(homeDir(), "Downloads", "simpletoggle-config-"+time.Now().Format("20060102")+".json"),
			Filters: []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}},
		})
		if err == nil && path != "" {
			var data []byte
			if data, err = json.MarshalIndent(x, "", "  "); err == nil {
				err = os.WriteFile(path, append(data, '\n'), 0o600)
			}
		}
		done(path, err)
	}()
}

func (c *controller) importConfig(win *mygo.Window, done func(int, int, int, error)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Filters: []mygo.FileFilter{{Name: "JSON", Extensions: []string{"json"}}}})
		if err != nil || len(paths) == 0 {
			done(0, 0, 0, err)
			return
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			done(0, 0, 0, err)
			return
		}
		var x store.Export
		if err := json.Unmarshal(data, &x); err != nil || x.Format != store.ExportFormat {
			done(0, 0, 0, errors.New("not a SimpleToggle settings file"))
			return
		}
		scripts := 0
		for file, content := range x.Scripts {
			if c.eng.InstallScript(filepath.Base(file), content) == nil {
				scripts++
			}
		}
		err = c.st.ImportState(x)
		mygo.RunOnMain(func() {
			if lang := validLanguage(c.st.Setting(keyLanguage)); lang != "" {
				mygo.App.SetLocale(lang)
			}
			c.eng.SetDefaults(engineDefaults(c.st))
			c.eng.Reload()
			c.auto.Reload()
			c.registerAppShortcuts()
			c.applyPrefs()
			c.redrawAll()
		})
		done(scripts, len(x.Scenes), len(x.Rules), err)
	}()
}

// ───────── 设置窗口 ─────────

func (c *controller) settingsHost() *settingsHost {
	env := c.integrationEnv()
	return &settingsHost{
		get:   c.st.Setting,
		set:   c.setSetting,
		prefs: func() Prefs { return c.prefs },

		views:     c.eng.List,
		setHidden: func(id string, hidden bool) { _ = c.eng.SetHidden(id, hidden) },
		setOrder: func(ids []string) {
			for i, id := range ids {
				m := c.st.Meta(id)
				m.Order = i + 1
				_ = c.eng.SetMeta(id, m)
			}
		},
		scenes:    c.eng.Scenes,
		auto:      c.auto,
		autoOn:    c.auto.Enabled,
		setAutoOn: func(on bool) { _ = c.auto.SetEnabled(on) },

		apiAddr:    fmt.Sprintf("127.0.0.1:%d", c.srv.Port()),
		apiRunning: func() bool { return c.apiOn },
		setAPI: func(on bool) error {
			_ = c.st.SetSetting(keyAPI, boolStr(on))
			if on == c.apiOn {
				return nil
			}
			if on {
				if err := c.srv.Start(); err != nil {
					return err
				}
			} else {
				c.srv.Close()
			}
			c.apiOn = on
			return nil
		},
		token:      c.srv.Token,
		regenToken: c.srv.RegenerateToken,

		clients:      cached(time.Second, env.Detect),
		plan:         env.Plan,
		apply:        env.Apply,
		snippet:      integrations.Snippet(env.Bin),
		cliPath:      cached(time.Second, cliOnPath),
		skill:        cachedSkill(env.SkillStatus),
		canSkill:     fileExists(filepath.Join(c.skillSource(), "SKILL.md")),
		installSkill: func() error { return env.InstallSkill(c.skillSource()) },

		loginItem:    mygo.App.OpenAtLogin,
		setLoginItem: mygo.App.SetOpenAtLogin,
		axTrusted:    darwin.AccessibilityTrusted,
		requestAX: func() {
			if !darwin.RequestAccessibility() {
				_ = mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
			}
		},
		location: func() int {
			st, err := darwin.LocationStatus()
			switch {
			case err != nil:
				return -1
			case st == 0:
				return 0
			case st == 3 || st == 4:
				return 2
			}
			return 1
		},
		requestLoc: func() { _ = darwin.RequestLocation() },
		openSys: func(pane string) {
			url := "x-apple.systempreferences:com.apple.preference.security?" + pane
			if pane == "notifications" {
				url = "x-apple.systempreferences:com.apple.Notifications-Settings.extension"
			}
			_ = mygo.Shell.OpenExternal(url)
		},

		version: version.Version,
		updater: mygo.Updater.Enabled(),
		checkUpdate: func(done func(string, error)) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				u, err := mygo.Updater.Check(ctx)
				if err != nil || u == nil {
					done("", err)
					return
				}
				done(u.Version, nil)
			}()
		},

		scriptsDir: c.eng.ScriptsDir,
		chooseDir: func(done func(string)) {
			win := c.stWin
			go func() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Directory: true, CreateDirectories: true})
				mygo.RunOnMain(func() {
					if err == nil && len(paths) > 0 {
						done(paths[0])
					}
					c.redrawAll()
				})
			}()
		},
		reveal:       func(p string) { _ = mygo.Shell.OpenPath(p) },
		relaunch:     mygo.App.Relaunch,
		exportConfig: func(done func(string, error)) { c.exportConfig(c.stWin, done) },
		importConfig: func(done func(int, int, int, error)) { c.importConfig(c.stWin, done) },
		reset:        c.resetSettings,

		copy:          func(text string) { mygo.Clipboard.WriteText(text) },
		openWorkbench: c.openWorkbench,
		editors:       cached(5*time.Second, installedEditors),
		shortcutIssue: func(key string) string { return c.appShortcutErr[key] },
		onMain: func(fn func()) {
			mygo.RunOnMain(func() {
				fn()
				c.redrawAll()
			})
		},
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// openSettings 打开（或切到）设置窗口。
func (c *controller) openSettings(tab string) {
	if c.flyout != nil && !c.devPanel {
		c.flyout.Close()
	}
	if c.st_ == nil {
		c.st_ = newSettingsWin(c.settingsHost())
	}
	if tab != "" {
		c.st_.tab = tab
	}
	if c.stWin != nil {
		c.stWin.Show()
		c.stWin.Focus()
		mygo.App.Focus()
		c.stWin.Invalidate()
		return
	}
	c.windowOpened()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "SimpleToggle " + tr("settings"), Width: 780, Height: 600, MinWidth: 680, MinHeight: 480,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancySidebar, StateKey: "settings",
		Content: ui.View(c.st_.render),
	})
	c.stWin = win
	win.OnClosed(func() {
		if c.stWin == win {
			c.stWin = nil
			c.st_ = nil
			c.windowClosed()
		}
	})
	mygo.App.Focus()
}

// ───────── 首次引导 ─────────

func (c *controller) openOnboarding() {
	if c.obWin != nil {
		c.obWin.Show()
		c.obWin.Focus()
		return
	}
	env := c.integrationEnv()
	var win *mygo.Window
	ob := newOnboarding(&onboardingHost{
		installed:     func(file string) bool { _, ok := c.eng.Get(file); return ok },
		install:       c.eng.InstallScript,
		prefs:         func() Prefs { return c.prefs },
		setPref:       func(name, value string) { c.setSetting("ui."+name, value) },
		clients:       cached(time.Second, env.Detect),
		plan:          env.Plan,
		apply:         env.Apply,
		panelShortcut: func() string { return c.st.Setting(keyPanelShortcut) },
		finish: func(openPanel bool) {
			_ = c.st.SetSetting(keyOnboarded, "true")
			if win != nil {
				win.Close()
			}
			if openPanel {
				go func() {
					time.Sleep(300 * time.Millisecond)
					mygo.RunOnMain(func() {
						if c.flyout == nil {
							c.togglePanel()
						}
					})
				}()
			}
		},
	})
	c.windowOpened()
	win = mygo.NewWindow(mygo.WindowOptions{
		Title: "SimpleToggle", Width: 840, Height: 580, DisableResize: true,
		TitleBarStyle: mygo.TitleBarHidden, Content: ui.View(ob.render),
	})
	c.obWin = win
	win.OnClosed(func() {
		if c.obWin == win {
			c.obWin = nil
			_ = c.st.SetSetting(keyOnboarded, "true")
			c.windowClosed()
		}
	})
	mygo.App.Focus()
}

// validLanguage 只放行 app 支持的语言；其他值（例如 v1 的 "system"、导入文件里的任意字符串）
// 视为跟随系统。SetLocale 遇到不是语言代码的值会 panic。
func validLanguage(v string) string {
	switch v {
	case "zh-Hans", "en":
		return v
	}
	return ""
}
