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
