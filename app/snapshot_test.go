package app

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
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
		tt.Advance(time.Second) // 让过渡动画走完
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
		w.actTab, w.runSel = 1, 1
		tt.Frame()
		save(tt, "activity"+suffix)
		// 概览：用 120 天的模拟记录
		b.mu.Lock()
		b.runs = fakeUsageRuns(time.Now())
		b.mu.Unlock()
		w.handle(engine.Event{Kind: engine.EventRun})
		w.actTab = 0
		tt.Frame()
		save(tt, "stats"+suffix)
		w.statRange, w.statItem = 3, "awake.sh"
		tt.Frame()
		save(tt, "stats-item"+suffix)
		w.statRange, w.statItem = 1, ""
		tt.SetSize(1180, 1300)
		tt.Frame()
		save(tt, "stats-full"+suffix)
		tt.SetSize(1180, 760)

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
		tt.Advance(time.Second) // 让过渡动画走完
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

// TestSnapshotsPanel 渲染面板的几种布局（默认跳过）。
func TestSnapshotsPanel(t *testing.T) {
	dir := os.Getenv("OT_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set OT_SNAPSHOT_DIR to render snapshots")
	}
	save := func(tt *ui.Tester, name string) {
		tt.Advance(time.Second) // 让过渡动画走完
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tt.Image())
	}
	views := sampleViews()
	views[1].Params = append(views[1].Params,
		engine.ParamView{Param: contract.Param{Key: "keep", Label: "防休眠", Type: contract.ParamSelect,
			Options: []contract.Option{{Label: "仅保持屏幕", Value: "d"}, {Label: "仅保持任务", Value: "i"}, {Label: "屏幕与任务", Value: "di"}}}, Value: "d"},
		engine.ParamView{Param: contract.Param{Key: "interval", Label: "间隔（秒）", Type: contract.ParamNumber,
			Presets: []contract.Option{{Label: "1 分钟", Value: "60"}, {Label: "4 分钟", Value: "240"}, {Label: "10 分钟", Value: "600"}}}, Value: "240"})
	for _, dark := range []bool{false, true} {
		suffix := pick(dark, "-dark", "-light")
		for _, accent := range []Accent{AccentInk, AccentBlue} {
			b := &fakeBackend{views: views}
			p := newPanel(b, Prefs{Accent: accent, Layout: "list", Density: "comfortable", ShowGroups: true})
			tt := ui.NewTester(p.view, 340, 560)
			tt.SetDark(dark)
			tt.Frame()
			save(tt, "panel-list-"+string(accent)+suffix)
			p.expanded = "awake.sh"
			tt.Frame()
			tt.Advance(500 * time.Millisecond)
			save(tt, "panel-params-"+string(accent)+suffix)
			p.expanded = ""
			p.prefs.Layout = "grid"
			tt.Frame()
			tt.Advance(500 * time.Millisecond)
			save(tt, "panel-grid-"+string(accent)+suffix)
			p.prefs.Layout, p.prefs.Density = "list", "compact"
			tt.Frame()
			tt.Advance(500 * time.Millisecond)
			save(tt, "panel-compact-"+string(accent)+suffix)
		}
	}
}

// fakeUsageRuns 生成一段看起来真实的使用记录：工作日多、白天多。
func fakeUsageRuns(now time.Time) []store.Run {
	var runs []store.Run
	ids := []struct{ id, name string }{{"awake.sh", "Keep Awake"}, {"flush.sh", "Flush DNS"}, {"dark.sh", "Dark Mode"}, {"scene:focus", "专注"}}
	srcs := []string{"manual", "manual", "shortcut", "automation", "ai", "scene"}
	seed := uint32(7)
	rnd := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	for d := 0; d < 120; d++ {
		day := now.AddDate(0, 0, -d)
		n := rnd(6)
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			n = rnd(3)
		}
		for i := 0; i < n; i++ {
			it := ids[min(rnd(5), 3)]
			h := 8 + rnd(12)
			t := time.Date(day.Year(), day.Month(), day.Day(), h, rnd(60), 0, 0, time.Local)
			runs = append(runs, store.Run{Time: t, Switch: it.id, Name: it.name, Action: "run", Source: srcs[rnd(len(srcs))], OK: rnd(12) != 0})
		}
	}
	return runs
}
