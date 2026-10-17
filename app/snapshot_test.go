package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HanlunWang/open-toggle/internal/automation"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/store"
	"github.com/egoist/mygo/ui"
)

// TestSnapshots 把工作台的几个状态渲染成 PNG，供人工核对视觉（默认跳过）：
//
//	OT_SNAPSHOT_DIR=/tmp/shots go test -run TestSnapshots ./app/
func TestSnapshots(t *testing.T) {
	dir := os.Getenv("OT_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set OT_SNAPSHOT_DIR to render snapshots")
	}
	save := func(tt *ui.Tester, name string) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tt.Image())
	}
	for _, dark := range []bool{false, true} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		w, b, tt, q := newTestWorkbench(t)
		tt.SetDark(dark)
		tt.Frame()
		// 参数展开 + 图标选择
		w.paramOpen = 0
		tt.Frame()
		save(tt, "params"+suffix)
		tt.Click(tr("f_icon"))
		tt.Frame()
		save(tt, "icons"+suffix)
		w.iconOpen = false
		// 有错误的脚本 + 问题页
		d := w.docs["awake.sh"]
		d.apply(d.text() + "\n")
		d.apply(replaceOnce(d.text(), "# <switch.type> daemon", "# <switch.type> daemn"))
		w.bottomTab = 1
		tt.Frame()
		save(tt, "problems"+suffix)
		// 按钮测试输出
		w.open(secSwitches, "flush.sh")
		tt.Frame()
		tt.Click(tr("run"))
		tt.Frame()
		(<-q)()
		tt.Frame()
		save(tt, "output"+suffix)
		// 脚本库 + 抽屉
		w.open(secLibrary, "")
		w.libSel = "pomodoro.sh"
		tt.Frame()
		tt.Frame()
		save(tt, "library"+suffix)
		// 活动
		b.mu.Lock()
		b.runs = []store.Run{
			{ID: 1, Switch: "flush.sh", Name: "Flush DNS", Action: "run", Source: "manual", OK: true, Duration: 182, Output: "DNS cache flushed"},
			{ID: 2, Switch: "awake.sh", Name: "Keep Awake", Action: "start", Source: "ai", Detail: "mcp/claude-code", OK: true},
			{ID: 3, Switch: "awake.sh", Name: "Keep Awake", Action: "exit", Source: "system", Detail: "exited unexpectedly", ExitCode: 1, Output: "caffeinate: invalid option -- x\nusage: caffeinate [-disu] [-t timeout]"},
		}
		b.mu.Unlock()
		w.handle(engine.Event{Kind: engine.EventRun})
		w.open(secActivity, "")
		w.runSel = 1
		tt.Frame()
		save(tt, "activity"+suffix)

		// 场景与自动化
		a := &fakeAuto{issues: map[string]automation.ShortcutIssue{"scene:focus": {Kind: "taken"}}}
		w.auto = a
		b.mu.Lock()
		b.scenes = []engine.SceneView{{Scene: store.Scene{ID: "focus", Name: "专注", Icon: "lucide:target", Restore: true, ShowName: true, Shortcut: "ctrl+alt+1",
			Steps: []store.SceneStep{{Switch: "awake.sh", Action: "on"}, {Switch: "flush.sh", Action: "run"}}}, Active: true}}
		b.mu.Unlock()
		a.rules = []store.Rule{
			{ID: "r1", Enabled: true, Trigger: store.Trigger{Kind: "time", Value: "18:30", Days: []int{1, 2, 3, 4, 5}}, Action: store.RuleAction{Verb: "scene", Target: "focus"}, When: &store.Condition{From: "09:00", To: "19:00"}},
			{ID: "r2", Enabled: true, Trigger: store.Trigger{Kind: "power", Event: "unplug"}, Action: store.RuleAction{Verb: "off", Target: "awake.sh"}, Suspended: "flapping"},
			{ID: "r3", Trigger: store.Trigger{Kind: "app", Event: "launch", Value: "com.apple.Keynote", Label: "Keynote"}, Action: store.RuleAction{Verb: "on", Target: "awake.sh"}},
		}
		w.invalidate()
		w.open(secScenes, "")
		tt.Frame()
		tt.Frame()
		save(tt, "scene"+suffix)
		w.open(secAutomations, "")
		w.selRule = "r1"
		tt.Frame()
		tt.Frame()
		save(tt, "rule"+suffix)
		w.selRule = "r2"
		tt.Frame()
		save(tt, "rule-suspended"+suffix)

		// 面板的场景栏
		p := newPanel(b, Prefs{Accent: AccentOrange, Layout: "list", Density: "comfortable", ShowGroups: true, ShowScenes: true})
		pt := ui.NewTester(p.view, 340, 420)
		pt.SetDark(dark)
		pt.Frame()
		save(pt, "panel-scenes"+suffix)
	}
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

// TestSnapshotsM5 渲染设置、引导与待确认修改（默认跳过）。
func TestSnapshotsM5(t *testing.T) {
	dir := os.Getenv("OT_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set OT_SNAPSHOT_DIR to render snapshots")
	}
	save := func(tt *ui.Tester, name string) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tt.Image())
	}
	for _, dark := range []bool{false, true} {
		suffix := "-light"
		if dark {
			suffix = "-dark"
		}
		h, _ := newFakeSettings(sampleViews())
		s := newSettingsWin(h)
		tt := ui.NewTester(s.render, 780, 600)
		tt.SetDark(dark)
		for _, tab := range []string{"general", "appearance", "menubar", "ai", "privacy", "advanced"} {
			s.tab = tab
			tt.Frame()
			save(tt, "settings-"+tab+suffix)
		}
		s.tab = "ai"
		p, _ := h.plan("cursor")
		s.plan, s.planOpen = &p, true
		tt.Frame()
		save(tt, "settings-plan"+suffix)

		o := newOnboarding(&onboardingHost{
			installed: func(f string) bool { return f == "keep-awake.sh" || f == "dark-mode.sh" },
			install:   func(string, string) error { return nil },
			prefs:     func() Prefs { return Prefs{Theme: "system", Accent: AccentInk, Layout: "list", MenubarStyle: trayPill} },
			setPref:   func(string, string) {},
			clients:   h.clients, plan: h.plan, apply: h.apply,
			panelShortcut: func() string { return "alt+space" },
			finish:        func(bool) {},
		})
		ot := ui.NewTester(o.render, 840, 580)
		ot.SetDark(dark)
		for step := 0; step < onboardingSteps; step++ {
			o.step = step
			if step == 1 {
				o.picked["pomodoro.sh"] = true
			}
			ot.Frame()
			save(ot, "onboarding-"+itoa(step+1)+suffix)
		}

		w, b, wt, _ := newTestWorkbench(t)
		wt.SetDark(dark)
		b.mu.Lock()
		b.changes = []store.Change{{ID: "chg-1", Kind: "update", Switch: "flush.sh", Name: "Flush DNS", Previous: flushScript,
			Content: strings.Replace(flushScript, "echo flushed", "dscacheutil -flushcache\nkillall -HUP mDNSResponder\necho flushed", 1),
			Source:  "ai", Detail: "mcp/claude-code", Status: "pending", Created: time.Now()}}
		b.mu.Unlock()
		w.open(secChanges, "")
		w.invalidate()
		wt.Frame()
		wt.Frame()
		save(wt, "changes"+suffix)
	}
}
