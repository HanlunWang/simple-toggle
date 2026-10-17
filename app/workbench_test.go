package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/HanlunWang/open-toggle/internal/contract"
	"github.com/HanlunWang/open-toggle/internal/engine"
	"github.com/HanlunWang/open-toggle/internal/store"
)

const awakeScript = `#!/bin/bash
# <switch.name> Keep Awake
# <switch.icon> lucide:coffee
# <switch.type> daemon
# <switch.param> key=mode type=select label=Mode default=d options="Display=d|Both=di"
exec caffeinate -$SWITCH_MODE
`

const flushScript = `#!/bin/bash
# <switch.name> Flush DNS
# <switch.icon> lucide:rotate-cw
# <switch.type> action
echo flushed
`

func newTestWorkbench(t *testing.T) (*workbench, *fakeBackend, *ui.Tester, chan func()) {
	t.Helper()
	b := &fakeBackend{}
	b.setScriptLocked("awake.sh", awakeScript)
	b.setScriptLocked("flush.sh", flushScript)
	w := newWorkbench(b, Prefs{Accent: AccentInk, Layout: "list", Density: "comfortable", ShowGroups: true})
	q := make(chan func(), 16)
	w.onMain = func(fn func()) { q <- fn }
	tt := ui.NewTester(w.render, 1180, 760)
	if w.selected != "flush.sh" {
		t.Fatalf("selected %q, want the first switch by name", w.selected)
	}
	w.open(secSwitches, "awake.sh")
	tt.Frame()
	return w, b, tt, q
}

func TestWorkbenchRendersSidebarAndEditor(t *testing.T) {
	w, _, tt, _ := newTestWorkbench(t)
	for _, want := range []string{tr("nav_switches"), tr("nav_activity"), tr("nav_library"), "Flush DNS", "Keep Awake", "awake.sh", tr("contract_ok"), tr("sec_params"), "mode"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	// 常驻开关显示启动 / 停止
	if !tt.HasText(tr("start")) || !tt.HasText(tr("stop")) {
		t.Fatalf("daemon test buttons missing: %q", tt.Texts())
	}
	if err := tt.Click("Flush DNS"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.selected != "flush.sh" || !tt.HasText(tr("run")) {
		t.Fatalf("switching to the action: selected=%q texts=%q", w.selected, tt.Texts())
	}
}

func TestInspectorWritesHeader(t *testing.T) {
	w, _, tt, _ := newTestWorkbench(t)
	d := w.docs["awake.sh"]
	// 类型分段控件 → 改写 # <switch.type>
	if err := tt.Click(tr("type_action")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(d.text(), "# <switch.type> action\n") {
		t.Fatalf("type not written:\n%s", d.text())
	}
	if !d.modified() || !tt.HasText(tr("unsaved")) {
		t.Fatal("buffer should be modified and marked unsaved")
	}
	// 撤销能回到原文：检查器的修改走的是缓冲区的编辑
	d.st.Buffer.Undo()
	tt.Frame()
	if d.text() != awakeScript {
		t.Fatalf("undo did not restore:\n%s", d.text())
	}
}

func TestEditorChangesShowInInspector(t *testing.T) {
	w, _, tt, _ := newTestWorkbench(t)
	d := w.docs["awake.sh"]
	d.apply(strings.Replace(d.text(), "Keep Awake", "Stay Up", 1))
	tt.Frame()
	if d.header.Name != "Stay Up" || !tt.HasText("Stay Up") {
		t.Fatalf("header not re-parsed: %q / %q", d.header.Name, tt.Texts())
	}
	// 参数：在文本里加一个，检查器里出现
	d.apply(contract.SetParams(d.text(), append(d.header.Params, contract.Param{Key: "duration", Label: "Minutes", Type: contract.ParamNumber})))
	tt.Frame()
	if !tt.HasText("duration") || !tt.HasText("Minutes") {
		t.Fatalf("param not shown: %q", tt.Texts())
	}
}

func TestSaveAndLintBlock(t *testing.T) {
	w, b, tt, _ := newTestWorkbench(t)
	d := w.docs["awake.sh"]
	d.apply(strings.Replace(d.text(), "# <switch.type> daemon", "# <switch.type> bogus", 1))
	tt.Frame()
	if !d.hasErrors() || !tt.HasText(issueText(d.issues[0])) && !tt.HasText("1 个问题") {
		t.Logf("texts %q", tt.Texts())
	}
	tt.ClickAt(500, 300) // 焦点放进编辑器，⌘S 由窗口内的快捷键处理
	tt.Key(ui.Cmd, ui.KeyS)
	tt.Frame()
	if b.has("save awake.sh") {
		t.Fatal("saved a script with contract errors")
	}
	if w.bottomTab != 1 || !tt.HasText(tr("save_blocked")) {
		t.Fatalf("problems tab not shown: tab=%d %q", w.bottomTab, tt.Texts())
	}
	// 修好再保存
	d.apply(strings.Replace(d.text(), "bogus", "daemon", 1))
	d.apply(strings.Replace(d.text(), "Keep Awake", "Awake", 1))
	tt.Frame()
	if err := tt.Click(tr("save") + "  ⌘S"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("save awake.sh") || d.modified() {
		t.Fatalf("save: calls %v modified %v", b.calls, d.modified())
	}
	if s, _ := b.Script("awake.sh"); !strings.Contains(s, "<switch.name> Awake") {
		t.Fatalf("saved content:\n%s", s)
	}
}

func TestTestButtons(t *testing.T) {
	w, b, tt, q := newTestWorkbench(t)
	if err := tt.Click("Flush DNS"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(tr("run")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	(<-q)()
	tt.Frame()
	if !b.has("run flush.sh") {
		t.Fatalf("calls %v", b.calls)
	}
	out := w.outputs["flush.sh"]
	if len(out) < 2 || out[0].text != "$ flush.sh" || out[len(out)-1].kind != outOK {
		t.Fatalf("output %+v", out)
	}
	// 有未保存的修改时，测试会先保存
	d := w.docs["flush.sh"]
	d.apply(strings.Replace(d.text(), "echo flushed", "echo flushed twice", 1))
	tt.Frame()
	if err := tt.Click(tr("run")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	(<-q)()
	if !b.has("save flush.sh") {
		t.Fatalf("test did not save first: %v", b.calls)
	}
}

func TestNewSwitchAndDelete(t *testing.T) {
	w, b, tt, _ := newTestWorkbench(t)
	if err := tt.Click(tr("new")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(tr("new") + " " + tr("new_action")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	id := contract.Slug(tr("new_name"))
	if !b.has("create "+id) || w.selected != id {
		t.Fatalf("create: calls %v selected %q", b.calls, w.selected)
	}
	if d := w.docs[id]; d == nil || d.header.Type != contract.Action || len(d.issues) != 0 {
		t.Fatalf("new action template is not clean: %+v", d)
	}
	w.remove(id)
	tt.Frame()
	if !b.has("delete "+id) || w.selected == id {
		t.Fatalf("delete: %v %q", b.calls, w.selected)
	}
}

func TestLibraryAndActivity(t *testing.T) {
	w, b, tt, _ := newTestWorkbench(t)
	if err := tt.Click(tr("nav_library")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Pomodoro") || !tt.HasText(tr("cat_dev")) {
		t.Fatalf("library: %q", tt.Texts())
	}
	w.libSel = "pomodoro.sh"
	tt.Frame()
	if err := tt.Click(tr("lib_add_long")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !b.has("install pomodoro.sh") || !w.installed("pomodoro.sh") {
		t.Fatalf("install: %v", b.calls)
	}

	b.mu.Lock()
	b.runs = []store.Run{
		{ID: 1, Switch: "flush.sh", Name: "Flush DNS", Action: "run", Source: "manual", OK: true, Output: "flushed"},
		{ID: 2, Switch: "awake.sh", Name: "Keep Awake", Action: "start", Source: "ai", Detail: "mcp/claude", OK: false, ExitCode: 1, Output: "boom"},
	}
	b.mu.Unlock()
	w.handle(engine.Event{Kind: engine.EventRun})
	if err := tt.Click(tr("nav_activity")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{tr("col_switch"), "mcp/claude", sourceLabel("ai"), tr("f_failed")} {
		if !tt.HasText(want) {
			t.Errorf("activity missing %q: %q", want, tt.Texts())
		}
	}
	w.actFilter = "failed"
	tt.Frame()
	if got := w.filteredRuns(); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("failed filter: %+v", got)
	}
}

func TestDiskChangeReloadsCleanDocs(t *testing.T) {
	w, b, tt, _ := newTestWorkbench(t)
	d := w.docs["awake.sh"]
	b.mu.Lock()
	b.setScriptLocked("awake.sh", strings.Replace(awakeScript, "Keep Awake", "From Disk", 1))
	b.mu.Unlock()
	w.handle(engine.Event{Kind: engine.EventReloaded})
	tt.Frame()
	if d.header.Name != "From Disk" || d.modified() {
		t.Fatalf("clean doc not reloaded: %q modified=%v", d.header.Name, d.modified())
	}
	// 有未保存修改时：标记冲突，不覆盖
	d.apply(strings.Replace(d.text(), "From Disk", "Mine", 1))
	b.mu.Lock()
	b.setScriptLocked("awake.sh", strings.Replace(awakeScript, "Keep Awake", "Disk Again", 1))
	b.mu.Unlock()
	w.handle(engine.Event{Kind: engine.EventReloaded})
	tt.Frame()
	if !d.conflict || d.header.Name != "Mine" || !tt.HasText(tr("disk_changed")) {
		t.Fatalf("conflict: %v %q", d.conflict, d.header.Name)
	}
}
