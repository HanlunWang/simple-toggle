package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 文件触发器：HintsOnly 监听仍要报告目录里（含子目录）的改动。
func TestWatchReportsChanges(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	fired := make(chan struct{}, 4)
	stop, err := appHost{}.Watch(dir, func() { fired <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("x"), 0o644)
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("change not reported")
	}
}
