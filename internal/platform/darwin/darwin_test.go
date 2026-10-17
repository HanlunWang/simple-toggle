//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestTrash(t *testing.T) {
	home, _ := os.UserHomeDir()
	f := filepath.Join(t.TempDir(), "opentoggle-trash-probe.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Trash(f); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".Trash", "opentoggle-trash-probe*"))
	for _, m := range matches {
		os.Remove(m) // 清理测试留在废纸篓里的文件
	}
}

func TestFingerprint(t *testing.T) {
	fp, err := NetworkFingerprint()
	if err != nil {
		t.Skip("offline:", err)
	}
	if fp.Interface == "" || fp.Gateway == "" {
		t.Errorf("fingerprint %+v", fp)
	}
}

func TestPowerAndApps(t *testing.T) {
	cases := map[string]Power{
		"Now drawing from 'AC Power'\n -InternalBattery-0 (id=35782755)\t77%; AC attached; not charging present: true\n": {OnAC: true, HasBattery: true, Percent: 77},
		"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t18%; discharging; 1:02 remaining present: true\n": {OnAC: false, HasBattery: true, Percent: 18},
		"Now drawing from 'AC Power'\n": {OnAC: true, Percent: 100},
	}
	for in, want := range cases {
		if got, ok := ParsePmset(in); !ok || got != want {
			t.Errorf("ParsePmset(%q) = %+v, want %+v", in, got, want)
		}
	}
	if _, err := PowerStatus(); err != nil {
		t.Fatal(err)
	}
	apps, err := RunningApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) == 0 {
		t.Skip("no GUI session")
	}
	found := false
	for _, a := range apps {
		if a.BundleID == "com.apple.finder" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Finder not among running apps: %v", apps)
	}
}

// RunningApps 在后台 goroutine 里被反复调用（自动化每 2 秒一次）；
// goroutine 换线程时自动释放池必须在创建它的线程上释放。
func TestRunningAppsConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				if _, err := RunningApps(); err != nil {
					t.Error(err)
					return
				}
				runtime.Gosched()
			}
		}()
	}
	wg.Wait()
}
