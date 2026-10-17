package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HanlunWang/open-toggle/internal/store"
)

func TestWorkbenchCommands(t *testing.T) {
	f := newFixture(t)
	mark := f.toggle("lamp.sh")
	f.write("tail.sh", "#!/bin/sh\n# <switch.name> Tail\n# <switch.type> daemon\necho started\nexec sleep 30\n")
	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}
	defer f.e.Shutdown()
	f.waitState("lamp.sh", Off)

	// Status：立即执行 status 并更新状态
	os.WriteFile(mark, []byte("x"), 0o644)
	res, err := f.e.Status("lamp.sh")
	if err != nil || res.Stdout != "on" {
		t.Fatalf("status: %+v %v", res, err)
	}
	f.waitState("lamp.sh", On)
	if _, err := f.e.Status("tail.sh"); !errors.Is(err, ErrNoStatus) {
		t.Fatalf("status on a daemon: %v", err)
	}

	// DaemonOutput：只有在运行时才有
	if _, ok := f.e.DaemonOutput("tail.sh"); ok {
		t.Fatal("output of a stopped daemon")
	}
	done, _ := f.e.SetSwitch("tail.sh", true, Manual, "workbench")
	Wait(done, 5*time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for {
		out, ok := f.e.DaemonOutput("tail.sh")
		if ok && strings.Contains(out, "started") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon output %q %v", out, ok)
		}
		time.Sleep(20 * time.Millisecond)
	}
	done, _ = f.e.SetSwitch("tail.sh", false, Manual, "workbench")
	Wait(done, 5*time.Second)
	runs := f.e.Runs(store.Filter{Switch: "tail.sh"})
	if len(runs) == 0 || runs[0].Action != "stop" || runs[0].Detail != "workbench" {
		t.Fatalf("runs %+v", runs)
	}

	// SetMeta：保存并反映在视图里
	if err := f.e.SetMeta("lamp.sh", store.Meta{Group: "Home", Timeout: 10}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.e.Get("lamp.sh"); v.Meta.Group != "Home" || v.Meta.Timeout != 10 {
		t.Fatalf("meta %+v", v.Meta)
	}

	// InstallScript：写入、可见、不覆盖、拒绝非法文件名与契约错误
	body := "#!/bin/sh\n# <switch.name> Ping\n# <switch.type> action\necho pong\n"
	f.st.SetHidden("ping.sh", true)
	if err := f.e.InstallScript("ping.sh", body); err != nil {
		t.Fatal(err)
	}
	if v, ok := f.e.Get("ping.sh"); !ok || v.Hidden {
		t.Fatalf("installed script missing or hidden: %+v", v)
	}
	if err := f.e.InstallScript("ping.sh", body); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	if err := f.e.InstallScript("../evil.sh", body); err == nil {
		t.Fatal("accepted a path outside the scripts folder")
	}
	if err := f.e.InstallScript("bad.sh", "#!/bin/sh\necho no header\n"); err == nil {
		t.Fatal("accepted a script without a name")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "bad.sh")); err == nil {
		t.Fatal("wrote an invalid script")
	}

	if err := f.e.ClearRuns(); err != nil || len(f.e.Runs(store.Filter{})) != 0 {
		t.Fatal("clear runs")
	}
}
