package engine

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

func waitRun(t *testing.T, ch <-chan store.Run) store.Run {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("scene did not finish")
	}
	return store.Run{}
}

func TestScenes(t *testing.T) {
	f := newFixture(t)
	lampMark := f.toggle("lamp.sh")
	fanMark := f.toggle("fan.sh")
	f.write("ping.sh", "#!/bin/sh\n# <switch.name> Ping\n# <switch.type> action\necho pong\n")
	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}
	defer f.e.Shutdown()
	f.waitState("lamp.sh", Off)
	// fan 事先开着：场景要把它关掉，恢复时再打开
	os.WriteFile(fanMark, []byte("1"), 0o644)
	f.e.Status("fan.sh")
	f.waitState("fan.sh", On)

	sc, err := f.e.SaveScene(store.Scene{Name: "Focus Mode", Restore: true, Steps: []store.SceneStep{
		{Switch: "lamp.sh", Action: "on"}, {Switch: "fan.sh", Action: "off"}, {Switch: "ping.sh", Action: "run"},
	}})
	if err != nil || sc.ID != "focus-mode" {
		t.Fatalf("save: %+v %v", sc, err)
	}
	if sc2, _ := f.e.SaveScene(store.Scene{Name: "Focus Mode"}); sc2.ID != "focus-mode-2" {
		t.Fatalf("duplicate id: %q", sc2.ID)
	}
	f.e.DeleteScene("focus-mode-2")

	rec := waitRun(t, mustScene(t)(f.e.ToggleScene("focus mode", Manual, "")))
	if !rec.OK || rec.Switch != "scene:focus-mode" || !strings.Contains(rec.Output, "✓ ping.sh run") {
		t.Fatalf("activate: %+v", rec)
	}
	if _, err := os.Stat(lampMark); err != nil {
		t.Fatal("lamp not on")
	}
	if sv, _ := f.e.Scene("focus-mode"); !sv.Active {
		t.Fatal("scene should be active")
	}
	// 再点一次：恢复
	rec = waitRun(t, mustScene(t)(f.e.ToggleScene("focus-mode", Manual, "")))
	if !rec.OK || rec.Action != "restore" {
		t.Fatalf("restore: %+v", rec)
	}
	f.waitState("lamp.sh", Off)
	f.waitState("fan.sh", On)
	if sv, _ := f.e.Scene("focus-mode"); sv.Active {
		t.Fatal("scene still active after restore")
	}

	// 激活后用户手动关掉一个成员：场景自动退出激活状态
	before := f.e.UserChangedAt("lamp.sh")
	waitRun(t, mustScene(t)(f.e.ActivateScene("focus-mode", Automation, "rule")))
	if !f.e.UserChangedAt("lamp.sh").Equal(before) {
		t.Fatal("a scene run by automation counted as a user change")
	}
	done, _ := f.e.SetSwitch("lamp.sh", false, Manual, "")
	Wait(done, 5*time.Second)
	if time.Since(f.e.UserChangedAt("lamp.sh")) > time.Second {
		t.Fatal("manual change not recorded")
	}
	if sv, _ := f.e.Scene("focus-mode"); sv.Active {
		t.Fatal("scene should no longer be active")
	}
	if _, err := f.e.ActivateScene("nope", Manual, ""); err != ErrNoScene {
		t.Fatalf("missing scene: %v", err)
	}
}

func mustScene(t *testing.T) func(<-chan store.Run, error) <-chan store.Run {
	return func(ch <-chan store.Run, err error) <-chan store.Run {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return ch
	}
}
