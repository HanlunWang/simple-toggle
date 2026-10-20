package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyScripts(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "old"), filepath.Join(root, "new")
	os.MkdirAll(from, 0o755)
	os.WriteFile(filepath.Join(from, "a.sh"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(from, "摸鱼.sh"), []byte("#!/bin/sh\n"), 0o700)
	os.Mkdir(filepath.Join(from, "sub"), 0o755)
	n, err := copyScripts(from, to)
	if err != nil || n != 2 {
		t.Fatalf("copied %d %v", n, err)
	}
	if fi, _ := os.Stat(filepath.Join(to, "摸鱼.sh")); fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(from, "a.sh")); err != nil {
		t.Fatal("old scripts must stay")
	}
	// 新目录已有脚本：不再复制
	os.WriteFile(filepath.Join(from, "b.sh"), []byte("x"), 0o755)
	if n, _ := copyScripts(from, to); n != 0 {
		t.Fatal("copied into a non-empty folder")
	}
	// 用户删光了脚本：目录还在，旧脚本不能复活
	entries, _ := os.ReadDir(to)
	for _, e := range entries {
		os.Remove(filepath.Join(to, e.Name()))
	}
	if n, _ := copyScripts(from, to); n != 0 {
		t.Fatal("legacy scripts came back after the user deleted them all")
	}
	if n, err := copyScripts(filepath.Join(root, "missing"), filepath.Join(root, "x")); n != 0 || err != nil {
		t.Fatal("missing legacy folder")
	}
}
