//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
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
