package engine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

func TestWriteScriptKeepsSymlinkAndMode(t *testing.T) {
	root := t.TempDir()
	// 软链接：写到它指向的文件，链接本身保留
	real := filepath.Join(root, "real.sh")
	os.WriteFile(real, []byte("old"), 0o700)
	link := filepath.Join(root, "link.sh")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := writeScript(link, "new"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}
	if data, _ := os.ReadFile(real); string(data) != "new" {
		t.Fatalf("target content %q", data)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o700 {
		t.Fatalf("target mode %v", fi.Mode().Perm())
	}
	// 普通文件：保留原有权限；新文件 0755
	plain := filepath.Join(root, "plain.sh")
	os.WriteFile(plain, []byte("x"), 0o750)
	os.Chmod(plain, 0o750)
	writeScript(plain, "y")
	if fi, _ := os.Stat(plain); fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode not preserved: %v", fi.Mode().Perm())
	}
	fresh := filepath.Join(root, "fresh.sh")
	writeScript(fresh, "z")
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o755 {
		t.Fatalf("new file mode %v", fi.Mode().Perm())
	}
}

func TestCreateScriptFileNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.sh")
	if err := createScriptFile(path, "first"); err != nil {
		t.Fatal(err)
	}
	// 检查之后才出现的同名文件：不覆盖，返回 ErrExist
	if err := createScriptFile(path, "second"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "first" {
		t.Fatalf("overwritten: %q", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestSetParamsValidation(t *testing.T) {
	f := newFixture(t)
	f.write("p.sh", "#!/bin/sh\n# <switch.name> P\n# <switch.type> action\n"+
		"# <switch.param> key=mode type=select default=a options=\"A=a|B=b\"\n"+
		"# <switch.param> key=port type=number default=80 min=1 max=65535\n"+
		"# <switch.param> key=note type=text default=x\necho ok\n")
	f.e.Start()
	defer f.e.Shutdown()
	for _, bad := range []map[string]string{
		{"mode": "c"}, {"mode": ""}, {"port": "abc"}, {"port": "0"}, {"port": "70000"}, {"port": "NaN"},
		{"port": "8080", "mode": "zzz"}, // 一个不合法就整体拒绝
	} {
		if err := f.e.SetParams("p.sh", bad, Manual); !errors.Is(err, ErrBadParam) {
			t.Errorf("%v accepted: %v", bad, err)
		}
	}
	if _, ok := f.st.Param("p.sh", "port"); ok {
		t.Fatal("rejected values were stored")
	}
	if err := f.e.SetParams("p.sh", map[string]string{"mode": "b", "port": "8080", "note": "anything"}, Manual); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.st.Param("p.sh", "port"); v != "8080" {
		t.Fatalf("port %q", v)
	}
}

func TestShutdownRefusesNewWork(t *testing.T) {
	f := newFixture(t)
	f.toggle("lamp.sh")
	f.write("ping.sh", "#!/bin/sh\n# <switch.name> Ping\n# <switch.type> action\necho pong\n")
	f.write("serve.sh", "#!/bin/sh\n# <switch.name> Serve\n# <switch.type> daemon\nexec sleep 30\n")
	f.e.Start()
	sc, err := f.e.SaveScene(store.Scene{Name: "S", Steps: []store.SceneStep{{Switch: "lamp.sh", Action: "on"}}})
	if err != nil {
		t.Fatal(err)
	}
	f.e.Shutdown()
	if _, err := f.e.SetSwitch("serve.sh", true, Manual, ""); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("SetSwitch: %v", err)
	}
	if _, err := f.e.Run("ping.sh", Manual, ""); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Run: %v", err)
	}
	if _, err := f.e.ActivateScene(sc.ID, Manual, ""); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("ActivateScene: %v", err)
	}
	// 已经在路上的 spawn（例如改参数后的重启）也不能在 Shutdown 之后留下进程
	sw, r, _ := f.e.lookup("serve.sh")
	r.op.Lock()
	f.e.spawn(sw, r, Manual, "")
	r.op.Unlock()
	f.e.mu.Lock()
	leaked := r.daemon != nil
	f.e.mu.Unlock()
	if leaked {
		r.daemon.Stop()
		t.Fatal("daemon started after shutdown")
	}
}

func TestLedgerMatchesDaemonsUnderConcurrency(t *testing.T) {
	f := newFixture(t)
	const n = 6
	for i := 0; i < n; i++ {
		f.write(fmt.Sprintf("d%d.sh", i), fmt.Sprintf("#!/bin/sh\n# <switch.name> D%d\n# <switch.type> daemon\nexec sleep 30\n", i))
	}
	f.e.Start()
	defer f.e.Shutdown()
	set := func(ids []int, on bool) {
		var wg sync.WaitGroup
		for _, i := range ids {
			done, err := f.e.SetSwitch(fmt.Sprintf("d%d.sh", i), on, Manual, "")
			if err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func() { defer wg.Done(); Wait(done, 5*time.Second) }()
		}
		wg.Wait()
	}
	set([]int{0, 1, 2, 3, 4, 5}, true)
	set([]int{0, 2, 4}, false)
	ledger := readLedger(filepath.Join(f.data, "daemons.json"))
	if len(ledger) != 3 {
		t.Fatalf("ledger %v", ledger)
	}
	for _, id := range []string{"d1.sh", "d3.sh", "d5.sh"} {
		if ledger[id].PID == 0 {
			t.Fatalf("%s missing from ledger %v", id, ledger)
		}
	}
	entries, _ := os.ReadDir(f.data)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".daemons.json") {
			t.Fatalf("temp ledger left behind: %s", e.Name())
		}
	}
}

func TestSweepLedgerEscalatesToKill(t *testing.T) {
	dir := t.TempDir()
	// 忽略 SIGTERM 的遗留进程：宽限期后必须 SIGKILL
	stubborn := exec.Command("/bin/sh", "-c", "trap '' TERM; exec sleep 30")
	stubborn.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := stubborn.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { stubborn.Wait(); close(exited) }()
	defer stubborn.Process.Kill()
	time.Sleep(100 * time.Millisecond) // 等 trap 生效
	path := filepath.Join(dir, "daemons.json")
	os.WriteFile(path, []byte(fmt.Sprintf(`{"x.sh":{"pid":%d,"started":%d}}`, stubborn.Process.Pid, time.Now().Unix())), 0o644)
	if n := SweepLedger(path, dir); n != 1 {
		t.Fatalf("swept %d", n)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("process ignoring SIGTERM survived the sweep")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("ledger not removed")
	}
}
