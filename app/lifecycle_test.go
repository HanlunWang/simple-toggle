package app

import (
	"strings"
	"testing"
)

func TestQuitDetail(t *testing.T) {
	daemons, toggles := onSwitches(sampleViews())
	if len(daemons) != 1 || daemons[0] != "Keep Awake" || len(toggles) != 1 || toggles[0] != "Dark Mode" {
		t.Fatalf("on switches: %v %v", daemons, toggles)
	}
	d := quitDetail(daemons, toggles, true)
	if !strings.Contains(d, "Keep Awake") || !strings.Contains(d, "Dark Mode") || !strings.Contains(d, strings.TrimSpace(tr("quit_daemons_restore"))) {
		t.Fatalf("detail: %q", d)
	}
	if d := quitDetail(daemons, nil, false); !strings.Contains(d, strings.TrimSpace(tr("quit_daemons_norestore"))) {
		t.Fatalf("no-restore detail: %q", d)
	}
}

func TestSessionMarker(t *testing.T) {
	t.Setenv("SIMPLETOGGLE_DATA", t.TempDir())
	if beginSession() {
		t.Fatal("first launch reported as unclean")
	}
	// 没有 endSession 就再次启动 = 上次崩溃
	if !beginSession() {
		t.Fatal("crash not detected")
	}
	endSession()
	if beginSession() {
		t.Fatal("clean quit reported as unclean")
	}
	endSession()
}
