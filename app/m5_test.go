package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/automation"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/integrations"
	"github.com/HanlunWang/open-toggle/internal/store"
)

type fakeSettings struct {
	kv      map[string]string
	applied []string
	hidden  map[string]bool
	order   []string
	copied  string
	regen   int
	api     bool
	reset   int
}

func newFakeSettings(views []engine.View) (*settingsHost, *fakeSettings) {
	f := &fakeSettings{kv: map[string]string{}, hidden: map[string]bool{}, api: true}
	a := &fakeAuto{issues: map[string]automation.ShortcutIssue{}}
	h := &settingsHost{
		get: func(k string) string { return f.kv[k] },
		set: func(k, v string) { f.kv[k] = v; f.applied = append(f.applied, k+"="+v) },
		prefs: func() Prefs {
			p := Prefs{Theme: "system", Accent: AccentInk, Layout: "list", Density: "comfortable", Material: "glass", MenubarStyle: trayPill,
				MenubarExtra: "countdown", ShowGroups: true, ShowScenes: true, BusyAnim: true, EditorFontSize: 13, LineNumbers: true}
			if v := f.kv["ui.theme"]; v != "" {
				p.Theme = v
			}
			if v := f.kv["ui.layout"]; v != "" {
				p.Layout = v
			}
			return p
		},
		views:      func() []engine.View { return views },
		setHidden:  func(id string, hidden bool) { f.hidden[id] = hidden },
		setOrder:   func(ids []string) { f.order = ids },
		scenes:     func() []engine.SceneView { return nil },
		auto:       a,
		autoOn:     func() bool { return f.kv[automation.SettingEnabled] != "false" },
		setAutoOn:  func(on bool) { f.kv[automation.SettingEnabled] = boolStr(on) },
		apiAddr:    "127.0.0.1:43737",
		apiRunning: func() bool { return f.api },
		setAPI:     func(on bool) error { f.api = on; return nil },
		token:      func() string { return "ot_abcdefghijklmnopqrstuvwx3f9a" },
		regenToken: func() error { f.regen++; return nil },
		clients: func() []integrations.Status {
			return []integrations.Status{
				{Client: integrations.ClaudeCode, Present: true, Connected: true, Current: true},
				{Client: integrations.Cursor, Present: true},
				{Client: integrations.Codex},
			}
		},
		plan: func(client string) (integrations.Plan, error) {
			return integrations.Plan{Client: client, File: "/tmp/mcp.json", After: `{"mcpServers": {}}`}, nil
		},
		apply:        func(p integrations.Plan) error { f.applied = append(f.applied, "apply "+p.Client); return nil },
		snippet:      integrations.Snippet("/bin/opentoggle"),
		cliPath:      func() string { return "/opt/homebrew/bin/opentoggle" },
		skill:        func() (bool, string) { return false, "" },
		axTrusted:    func() bool { return false },
		requestAX:    func() { f.applied = append(f.applied, "ax") },
		location:     func() int { return 0 },
		requestLoc:   func() {},
		openSys:      func(string) {},
		version:      "2.0.0",
		scriptsDir:   func() string { return "/tmp/switches" },
		chooseDir:    func(func(string)) {},
		reveal:       func(string) {},
		relaunch:     func() {},
		exportConfig: func(func(string, error)) {},
		importConfig: func(func(int, int, int, error)) {},
		reset:        func() { f.reset++ },
		copy:         func(t string) { f.copied = t },
	}
	return h, f
}

func TestSettingsWindow(t *testing.T) {
	h, f := newFakeSettings(sampleViews())
	s := newSettingsWin(h)
	tt := ui.NewTester(s.render, 780, 600)
	for _, tab := range settingsTabs {
		if !tt.HasText(tr("st_" + tab.id)) {
			t.Fatalf("tab %s missing", tab.id)
		}
	}
	// 通用：完成时通知
	if err := tt.Click(tr("n_always")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.kv[keyNotify] != "always" {
		t.Fatalf("notify: %v", f.applied)
	}
	// 外观：主题
	tt.Click(tr("st_appearance"))
	tt.Frame()
	if err := tt.Click(tr("theme_dark")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.kv["ui.theme"] != "dark" {
		t.Fatalf("theme: %v", f.applied)
	}
	// 菜单栏与面板：布局 + 排序 + 隐藏
	tt.Click(tr("st_menubar"))
	tt.Frame()
	tt.Click(tr("layout_grid"))
	tt.Frame()
	if f.kv["ui.layout"] != "grid" {
		t.Fatalf("layout: %v", f.applied)
	}
	tt.Click(tr("hide_in_panel"))
	tt.Frame()
	if len(f.hidden) != 1 {
		t.Fatalf("hide: %v", f.hidden)
	}
	tt.Click(tr("move_down"))
	tt.Frame()
	if len(f.order) != 5 {
		t.Fatalf("order: %v", f.order)
	}
	// AI：删除权限、令牌、接入 Cursor
	tt.Click(tr("st_ai"))
	tt.Frame()
	if !tt.HasText("ot_••••••••3f9a") || tt.HasText("ot_abcdefghijklmnopqrstuvwx3f9a") {
		t.Fatalf("token must be masked: %q", tt.Texts())
	}
	if err := tt.Click(tr("ai_delete")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if f.kv["ai.delete"] != "true" {
		t.Fatalf("ai.delete: %v", f.applied)
	}
	tt.Click(tr("copy"))
	tt.Frame()
	if f.copied != "ot_abcdefghijklmnopqrstuvwx3f9a" {
		t.Fatalf("copy token: %q", f.copied)
	}
	if err := tt.Click(tr("cl_add")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !s.planOpen || !tt.HasText(`{"mcpServers": {}}`) {
		t.Fatalf("plan dialog: %v %q", s.planOpen, tt.Texts())
	}
	tt.Click(tr("cl_apply"))
	tt.Frame()
	if !strings.Contains(strings.Join(f.applied, ","), "apply cursor") {
		t.Fatalf("apply: %v", f.applied)
	}
	tt.Click(tr("regenerate"))
	tt.Frame()
	if !s.regenOpen {
		t.Fatal("regenerate should ask first")
	}
	tt.Key(0, ui.KeyEnter) // 确认框的默认按钮
	tt.Frame()
	if f.regen != 1 || s.regenOpen {
		t.Fatalf("regenerate: %d open=%v", f.regen, s.regenOpen)
	}
	// 自动化：总开关与冲突策略
	tt.Click(tr("st_automation"))
	tt.Frame()
	tt.Click(tr("pause_1h_s"))
	tt.Frame()
	if paused, _ := h.auto.Paused(); !paused {
		t.Fatal("pause from settings")
	}
	// 高级：环境变量
	tt.Click(tr("st_advanced"))
	tt.Frame()
	s.envName, s.envValue = "PROXY_HOST", "127.0.0.1"
	tt.Frame()
	tt.Click(tr("env_add"))
	tt.Frame()
	if f.kv[keyEnv] != `{"PROXY_HOST":"127.0.0.1"}` {
		t.Fatalf("env: %q", f.kv[keyEnv])
	}
	if !tt.HasText("PROXY_HOST") {
		t.Fatalf("env row: %q", tt.Texts())
	}
	// 权限
	tt.Click(tr("st_privacy"))
	tt.Frame()
	tt.Click(tr("ask"))
	tt.Frame()
	if !strings.Contains(strings.Join(f.applied, ","), "ax") {
		t.Fatalf("request AX: %v", f.applied)
	}
}

func TestNotificationPolicy(t *testing.T) {
	fail := store.Run{Source: "automation", OK: false, Action: "run"}
	ok := store.Run{Source: "shortcut", OK: true, Action: "on"}
	cases := []struct {
		policy, override string
		r                store.Run
		want             bool
	}{
		{"", "", fail, true},
		{"never", "", fail, false},
		{"never", "always", ok, true},
		{"fail", "", ok, false},
		{"always", "", ok, true},
		{"always", "", store.Run{Source: "manual", OK: true}, false},
		{"always", "", store.Run{Source: "ai", OK: true, Detail: wbDetail}, false},
		{"always", "", store.Run{Source: "ai", OK: true, Action: "start"}, false},
	}
	for i, c := range cases {
		if got := shouldNotify(c.policy, c.override, c.r); got != c.want {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
}

func TestOnboarding(t *testing.T) {
	installed := map[string]bool{"keep-awake.sh": true}
	prefs := Prefs{Theme: "system", Accent: AccentInk, Layout: "list", MenubarStyle: trayPill}
	var finished, openPanel bool
	var installs []string
	o := newOnboarding(&onboardingHost{
		installed: func(f string) bool { return installed[f] },
		install:   func(f, _ string) error { installs = append(installs, f); installed[f] = true; return nil },
		prefs:     func() Prefs { return prefs },
		setPref: func(name, v string) {
			if name == "theme" {
				prefs.Theme = v
			}
		},
		clients: func() []integrations.Status {
			return []integrations.Status{{Client: integrations.ClaudeCode, Present: true}}
		},
		plan: func(c string) (integrations.Plan, error) {
			return integrations.Plan{Client: c, Commands: [][]string{{"claude", "mcp", "add"}}}, nil
		},
		apply:         func(integrations.Plan) error { return nil },
		panelShortcut: func() string { return "alt+space" },
		finish:        func(p bool) { finished, openPanel = true, p },
	})
	tt := ui.NewTester(o.render, 760, 560)
	if !tt.HasText(tr("ob_h1")) {
		t.Fatalf("step 1: %q", tt.Texts())
	}
	tt.Click(tr("ob_start"))
	tt.Frame()
	if err := tt.Click("Pomodoro"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(strings.Replace(tr("ob_add_n"), "%d", "1", 1)); err != nil {
		t.Fatalf("add button: %v %q", err, tt.Texts())
	}
	tt.Frame()
	if len(installs) != 1 || installs[0] != "pomodoro.sh" {
		t.Fatalf("installs %v", installs)
	}
	tt.Click(tr("theme_dark"))
	tt.Frame()
	if prefs.Theme != "dark" {
		t.Fatal("theme")
	}
	tt.Click(tr("ob_continue"))
	tt.Frame()
	if !tt.HasText(tr("ob_h4")) {
		t.Fatalf("step 4: %q", tt.Texts())
	}
	tt.Click(tr("cl_add"))
	tt.Frame()
	if !o.planOpen {
		t.Fatal("plan dialog")
	}
	o.planOpen = false
	tt.Frame()
	tt.Click(tr("ob_later"))
	tt.Frame()
	if !tt.HasText("⌥Space") {
		t.Fatalf("done step shows the panel shortcut: %q", tt.Texts())
	}
	tt.Click(tr("ob_open_panel"))
	tt.Frame()
	if !finished || !openPanel {
		t.Fatal("finish")
	}
}

func TestPendingChanges(t *testing.T) {
	w, b, tt, _ := newTestWorkbench(t)
	b.mu.Lock()
	b.changes = []store.Change{
		{ID: "chg-1", Kind: "update", Switch: "flush.sh", Name: "Flush DNS", Previous: flushScript,
			Content: strings.Replace(flushScript, "echo flushed", "echo flushed twice", 1), Source: "ai", Detail: "mcp/claude-code", Status: "pending", Created: time.Now()},
		{ID: "chg-2", Kind: "create", Name: "Hello", Content: "#!/bin/sh\n# <switch.name> Hello\n# <switch.type> action\necho hi\n", Source: "ai", Detail: "cli/agent:codex", Status: "pending", Created: time.Now()},
	}
	b.mu.Unlock()
	w.invalidate()
	tt.Frame()
	if !tt.HasText(tr("chg_title")) {
		t.Fatalf("pending nav item: %q", tt.Texts())
	}
	tt.Click(tr("chg_title"))
	tt.Frame()
	if !tt.HasText(strings.Replace(tr("chg_from"), "%s", "claude-code", 1)) {
		t.Fatalf("change header: %q", tt.Texts())
	}
	if err := tt.Click(tr("chg_approve")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("approved chg-1") {
		t.Fatalf("approve: %v", b.calls)
	}
	if s, _ := b.Script("flush.sh"); !strings.Contains(s, "flushed twice") {
		t.Fatal("approved content not written")
	}
	w.selChange = "chg-2"
	tt.Frame()
	if err := tt.Click(tr("chg_reject")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("rejected chg-2") || w.pendingCount() != 0 {
		t.Fatalf("reject: %v", b.calls)
	}
}
