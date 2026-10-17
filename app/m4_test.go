package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/simple-toggle/internal/automation"
	"github.com/HanlunWang/simple-toggle/internal/engine"
	"github.com/HanlunWang/simple-toggle/internal/store"
)

func TestPanelSceneBar(t *testing.T) {
	q := mainQueue()
	p, b := newTestPanel(sampleViews())
	b.scenes = []engine.SceneView{{Scene: store.Scene{ID: "focus", Name: "专注", Restore: true}}, {Scene: store.Scene{ID: "present", Name: "演示"}, Active: true}}
	tt := ui.NewTester(p.view, 340, 560)
	if !tt.HasText("专注") || !tt.HasText("演示") {
		t.Fatalf("scene bar missing: %q", tt.Texts())
	}
	if err := tt.Click("专注"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	(<-q)()
	if !b.has("toggle-scene focus") {
		t.Fatalf("calls %v", b.calls)
	}
	p.prefs.ShowScenes = false
	p.dirty = true
	tt.Frame()
	if tt.HasText("演示") {
		t.Fatal("scene bar shown while turned off")
	}
}

func newM4Workbench(t *testing.T) (*workbench, *fakeBackend, *fakeAuto, *ui.Tester) {
	t.Helper()
	w, b, tt, _ := newTestWorkbench(t)
	a := &fakeAuto{issues: map[string]automation.ShortcutIssue{}}
	w.auto = a
	w.invalidate()
	tt.Frame()
	return w, b, a, tt
}

func TestSceneEditor(t *testing.T) {
	w, b, a, tt := newM4Workbench(t)
	w.open(secScenes, "")
	tt.Frame()
	if !tt.HasText(tr("scenes_empty")) {
		t.Fatalf("empty state: %q", tt.Texts())
	}
	if err := tt.Click(tr("new_scene_bt")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(b.scenes) != 1 || w.selScene != b.scenes[0].ID {
		t.Fatalf("new scene: %+v", b.scenes)
	}
	sc := b.scenes[0].Scene
	sc.Steps = []store.SceneStep{{Switch: "awake.sh", Action: "on"}, {Switch: "flush.sh", Action: "run"}}
	w.saveScene(sc)
	tt.Frame()
	for _, want := range []string{"Keep Awake", "Flush DNS", tr("step_run"), tr("scene_restore"), tr("scene_show")} {
		if !tt.HasText(want) {
			t.Errorf("missing %q: %q", want, tt.Texts())
		}
	}
	// 第一步改为关闭
	if err := tt.Click(tr("step_off")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := b.scenes[0].Steps[0].Action; got != "off" {
		t.Fatalf("step action %q", got)
	}
	// 录制快捷键 ⌃⌥1
	if err := tt.Click(tr("sc_record")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Ctrl|ui.Alt, ui.Key1)
	tt.Frame()
	if got := b.scenes[0].Shortcut; got != "ctrl+alt+1" {
		t.Fatalf("shortcut %q (recording=%q)", got, w.rec.recording)
	}
	if !strings.Contains(strings.Join(a.calls, ","), "reload") {
		t.Fatal("automation not reloaded after the shortcut changed")
	}
	// 激活
	if err := tt.Click(tr("scene_activate")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("toggle-scene " + sc.ID) {
		t.Fatalf("calls %v", b.calls)
	}
}

func TestRuleEditor(t *testing.T) {
	w, _, a, tt := newM4Workbench(t)
	w.open(secAutomations, "")
	tt.Frame()
	if err := tt.Click(tr("new_rule_bt")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.rules) != 1 || a.rules[0].Trigger.Kind != automation.KindTime {
		t.Fatalf("new rule: %+v", a.rules)
	}
	id := a.rules[0].ID
	if !tt.HasText(tr("days_weekdays") + " 09:00") {
		t.Fatalf("description: %q", tt.Texts())
	}
	// 切到「电源」
	if err := tt.Click(tr("kind_power")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rules[0].Trigger.Kind != automation.KindPower || a.rules[0].Trigger.Event != "unplug" {
		t.Fatalf("kind change: %+v", a.rules[0].Trigger)
	}
	// 切到「应用」：还没选应用，只保留草稿并提示
	if err := tt.Click(tr("kind_app")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rules[0].Trigger.Kind != automation.KindPower || w.ruleNote[id] == "" || w.ruleDraft[id].Trigger.Kind != automation.KindApp {
		t.Fatalf("incomplete rule should stay a draft: %+v note=%q", a.rules[0], w.ruleNote[id])
	}
	// 回到「显示器」后规则完整，保存
	if err := tt.Click(tr("kind_display")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.rules[0].Trigger.Kind != automation.KindDisplay || w.ruleNote[id] != "" {
		t.Fatalf("display: %+v", a.rules[0])
	}
	// 动作改为「关闭」，添加时间段，立即测试
	if err := tt.Click(tr("v_off")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(tr("cond_add")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if r := a.rules[0]; r.Action.Verb != "off" || r.When == nil || r.When.From != "09:00" {
		t.Fatalf("action / condition: %+v", r)
	}
	if err := tt.Click(tr("rule_test")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(strings.Join(a.calls, ","), "test "+id) {
		t.Fatalf("calls %v", a.calls)
	}
	// 全部暂停
	if err := tt.Click(tr("pause_all")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(tr("pause_1h")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.isPaused || !tt.HasText(tr("resume")) {
		t.Fatalf("pause: %v %q", a.isPaused, tt.Texts())
	}
}

func TestInspectorShortcutAndTriggers(t *testing.T) {
	w, b, a, tt := newM4Workbench(t)
	a.rules = []store.Rule{{ID: "r1", Enabled: true, Trigger: store.Trigger{Kind: automation.KindWake, Event: "wake"}, Action: store.RuleAction{Verb: "on", Target: "awake.sh"}}}
	a.issues["switch:awake.sh"] = automation.ShortcutIssue{Kind: "taken"}
	w.invalidate()
	tt.Frame()
	if !tt.HasText(triggerText(a.rules[0].Trigger)) {
		t.Fatalf("trigger list: %q", tt.Texts())
	}
	if err := tt.Click(tr("sc_record")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Super|ui.Shift, ui.KeyK)
	tt.Frame()
	b.mu.Lock()
	got := ""
	for _, v := range b.views {
		if v.ID == "awake.sh" {
			got = v.Meta.Shortcut
		}
	}
	b.mu.Unlock()
	if got != "cmd+shift+k" {
		t.Fatalf("switch shortcut %q", got)
	}
	tt.Frame()
	if !tt.HasText(tr("sc_taken")) {
		t.Fatalf("issue not shown: %q", tt.Texts())
	}
	// 只有 Shift 不是有效的全局快捷键
	tt.Click("⌘⇧K")
	tt.Frame()
	tt.Key(ui.Shift, ui.KeyA)
	tt.Frame()
	if !tt.HasText(tr("sc_bad")) {
		t.Fatalf("bad shortcut not rejected: %q", tt.Texts())
	}
	// 点触发规则跳到自动化
	if err := tt.Click(triggerText(a.rules[0].Trigger)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.section != secAutomations || w.selRule != "r1" {
		t.Fatalf("jump: %s %s", w.section, w.selRule)
	}
}
