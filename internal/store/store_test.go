package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatePersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDesired("a.sh", true)
	s.SetHidden("b.sh", true)
	if changed, _ := s.SetParam("a.sh", "mode", "d"); !changed {
		t.Fatal("first set should change")
	}
	if changed, _ := s.SetParam("a.sh", "mode", "d"); changed {
		t.Fatal("same value should not change")
	}
	s.MarkSeeded("a.sh")
	s.SetMeta("a.sh", Meta{Group: "系统", Shortcut: "ctrl+opt+d"})

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := r.Param("a.sh", "mode"); !r.Desired("a.sh") || !r.Hidden("b.sh") || !ok || v != "d" || !r.Seeded("a.sh") || r.Meta("a.sh").Group != "系统" {
		t.Fatal("state not persisted")
	}
	r.Forget("a.sh")
	if r.Desired("a.sh") || r.Meta("a.sh").Group != "" {
		t.Fatal("forget")
	}
}

func TestCorruptStateIsSetAside(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, stateFile), []byte("{not json"), 0o644)
	s, err := Open(dir)
	if err != nil || s.Desired("x") {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, stateFile+".corrupt-*"))
	if len(matches) != 1 {
		t.Fatal("corrupt file not kept aside")
	}
}

func TestRuns(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.AppendRun(Run{Switch: "a.sh", Action: "on", Source: "manual", OK: true})
	s.AppendRun(Run{Switch: "b.sh", Action: "run", Source: "ai", OK: false, Output: strings.Repeat("x", MaxRunOutput+10)})
	s.AppendRun(Run{Switch: "a.sh", Action: "off", Source: "cli", OK: true, Time: time.Now().Add(-40 * 24 * time.Hour)})

	if got := s.Runs(Filter{Switch: "a.sh"}); len(got) != 2 || got[0].Action != "off" {
		t.Fatalf("filter switch: %+v", got)
	}
	if got := s.Runs(Filter{FailedOnly: true}); len(got) != 1 || len(got[0].Output) != MaxRunOutput {
		t.Fatalf("failed only: %d", len(got))
	}
	// 重新打开时清理超过保留期的记录，编号继续递增
	r, _ := Open(dir)
	if got := r.Runs(Filter{}); len(got) != 2 {
		t.Fatalf("retention on reopen: %d", len(got))
	}
	next, _ := r.AppendRun(Run{Switch: "c.sh"})
	if next.ID != 4 {
		t.Fatalf("id continues: %d", next.ID)
	}
	// 崩溃时写了半行
	f, _ := os.OpenFile(filepath.Join(dir, runsFile), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"id":9,"swi`)
	f.Close()
	if q, err := Open(dir); err != nil || len(q.Runs(Filter{})) != 3 {
		t.Fatalf("partial line: %v", err)
	}
}

const v1Plist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>NSWindow Frame manager</key>
	<string>-1269 207 1000 640</string>
	<key>NSBlob</key>
	<data>AAEC</data>
	<key>OpenToggle.desiredStates</key>
	<dict>
		<key>dock-autohide.sh</key>
		<integer>1</integer>
		<key>keep-awake.sh</key>
		<false/>
	</dict>
	<key>OpenToggle.disabledSwitches</key>
	<array>
		<string>wifi.sh</string>
		<string>摸鱼模式.sh</string>
	</array>
	<key>OpenToggle.paramValues</key>
	<dict>
		<key>keep-awake.sh</key>
		<dict>
			<key>duration</key>
			<integer>120</integer>
			<key>mode</key>
			<string>dis</string>
		</dict>
	</dict>
	<key>OpenToggle.seededScripts</key>
	<array><string>keep-awake.sh</string></array>
	<key>OpenToggle.language</key>
	<string>zh-Hans</string>
</dict>
</plist>`

func TestImportV1(t *testing.T) {
	v, err := ParseV1Plist([]byte(v1Plist))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Desired["dock-autohide.sh"] || v.Desired["keep-awake.sh"] || len(v.Hidden) != 2 || v.Params["keep-awake.sh"]["duration"] != "120" || v.Language != "zh-Hans" {
		t.Fatalf("%+v", v)
	}
	s, _ := Open(t.TempDir())
	s.SetParam("keep-awake.sh", "mode", "i") // v2 已有的值优先
	if imported, err := s.ImportV1(v); !imported || err != nil {
		t.Fatal("import", err)
	}
	if m, _ := s.Param("keep-awake.sh", "mode"); m != "i" || !s.Hidden("摸鱼模式.sh") || !s.Seeded("keep-awake.sh") || s.Setting("language") != "zh-Hans" {
		t.Fatal("merge")
	}
	if again, _ := s.ImportV1(v); again {
		t.Fatal("imported twice")
	}
}

func TestScenesAndRules(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	sc := Scene{ID: "focus", Name: "Focus", Steps: []SceneStep{{"a.sh", "on"}, {"b.sh", "run"}}, Restore: true}
	if err := s.PutScene(sc); err != nil {
		t.Fatal(err)
	}
	sc.Name = "Deep Focus"
	s.PutScene(sc)
	r := Rule{ID: "r1", Enabled: true, Trigger: Trigger{Kind: "time", Value: "18:30", Days: []int{1, 2}}, Action: RuleAction{Verb: "scene", Target: "focus"},
		When: &Condition{Days: []int{1}, From: "09:00", To: "18:00"}}
	s.PutRule(r)
	// 返回的是副本：改它不影响存储
	got := s.Rules()
	got[0].Trigger.Days[0] = 6
	got[0].When.Days[0] = 6
	s.MarkFired("r1", time.Unix(100, 0))
	s.Suspend("r1", "flapping")

	s2, _ := Open(dir)
	if scs := s2.Scenes(); len(scs) != 1 || scs[0].Name != "Deep Focus" || len(scs[0].Steps) != 2 {
		t.Fatalf("scenes %+v", scs)
	}
	r2, ok := s2.Rule("r1")
	if !ok || r2.Trigger.Days[0] != 1 || r2.When.Days[0] != 1 || r2.LastFired.Unix() != 100 || r2.Enabled || r2.Suspended != "flapping" {
		t.Fatalf("rule %+v", r2)
	}
	if s2.DeleteScene("focus") != nil || s2.DeleteRule("r1") != nil || s2.DeleteRule("r1") != ErrNoSuchItem {
		t.Fatal("delete")
	}
}

func TestExportImportReset(t *testing.T) {
	a, _ := Open(t.TempDir())
	a.SetSetting("ui.theme", "dark")
	a.SetSetting("onboarded", "true")
	a.SetHidden("x.sh", true)
	a.SetParam("x.sh", "mode", "b")
	a.SetMeta("x.sh", Meta{Group: "Work"})
	a.PutScene(Scene{ID: "s", Name: "S"})
	a.PutRule(Rule{ID: "r", Enabled: true, LastFired: time.Now()})
	x := a.ExportState()
	if x.Settings["onboarded"] != "" || x.Settings["ui.theme"] != "dark" || x.Format != ExportFormat {
		t.Fatalf("export settings %v", x.Settings)
	}
	b, _ := Open(t.TempDir())
	b.PutScene(Scene{ID: "s", Name: "Old"})
	b.PutScene(Scene{ID: "keep", Name: "Keep"})
	if err := b.ImportState(x); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.Param("x.sh", "mode"); v != "b" || !b.Hidden("x.sh") || b.Meta("x.sh").Group != "Work" || b.Setting("ui.theme") != "dark" {
		t.Fatal("import state")
	}
	if scs := b.Scenes(); len(scs) != 2 || scs[0].Name != "S" {
		t.Fatalf("scenes merged: %+v", scs)
	}
	if r, _ := b.Rule("r"); !r.LastFired.IsZero() {
		t.Fatal("last fired imported")
	}
	b.SetSetting("onboarded", "true")
	b.ResetSettings()
	if b.Setting("ui.theme") != "" || b.Setting("onboarded") != "true" {
		t.Fatal("reset")
	}
}

func TestImportV1SystemLanguage(t *testing.T) {
	s, _ := Open(t.TempDir())
	if _, err := s.ImportV1(V1Data{Language: "system"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Setting("language"); got != "" {
		t.Fatalf("v1 \"system\" should mean follow the system, got %q", got)
	}
}

// TestRealV1Import 读取本机 0.5 的偏好（只读），导入到临时目录，打印导入了什么。默认跳过：
//
//	OT_V1_REAL=1 go test -run TestRealV1Import -v ./internal/store/
func TestRealV1Import(t *testing.T) {
	if os.Getenv("OT_V1_REAL") != "1" {
		t.Skip("set OT_V1_REAL=1 to read this Mac's 0.5 preferences")
	}
	v, err := ReadV1(V1Domain)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := Open(t.TempDir())
	imported, err := s.ImportV1(v)
	if err != nil || !imported {
		t.Fatalf("import: %v %v", imported, err)
	}
	t.Logf("desired on: %v", v.Desired)
	t.Logf("hidden: %v", v.Hidden)
	t.Logf("params: %d switches", len(v.Params))
	t.Logf("seeded: %d presets", len(v.Seeded))
	t.Logf("language: %q → setting %q", v.Language, s.Setting("language"))
	if again, _ := s.ImportV1(v); again {
		t.Fatal("import ran twice")
	}
}

// 0.5 的偏好键名必须保持 OpenToggle.*（项目更名不影响旧数据）。
func TestV1KeysKeepOldName(t *testing.T) {
	src, err := os.ReadFile("v1import.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"OpenToggle.desiredStates", "OpenToggle.disabledSwitches", "OpenToggle.seededScripts", "OpenToggle.paramValues", "OpenToggle.language"} {
		if !strings.Contains(string(src), `"`+k+`"`) {
			t.Errorf("v1 key %q missing", k)
		}
	}
	if V1Domain != "io.github.hanlunwang.OpenToggle" {
		t.Errorf("v1 domain %q", V1Domain)
	}
}
