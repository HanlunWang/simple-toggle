package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

// 脚本目录检测比轮询更频繁时，定时轮询仍然要按时发生（外部改动要被发现）。
func TestPollingNotStarvedByDirectoryWatch(t *testing.T) {
	f := newFixture(t)
	mark := f.toggle("ext.sh")
	st, _ := store.Open(f.data)
	e := New(Config{
		ScriptsDir: f.dir, LedgerPath: filepath.Join(f.data, "daemons.json"), Bin: "/usr/bin/true", Store: st,
		DefaultTimeout: 5 * time.Second, WatchInterval: 20 * time.Millisecond,
		PollVisible: 150 * time.Millisecond, PollHidden: 150 * time.Millisecond,
	})
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Shutdown()
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil { // 在 app 之外打开
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if v, ok := e.Get("ext.sh"); ok && v.State == On {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("external change never picked up by the status poll")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
