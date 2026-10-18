package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradePresetsOnlyUntouched(t *testing.T) {
	f := newFixture(t)
	e, dir := f.e, f.dir
	old := "#!/bin/sh\n# <switch.name> A\necho old\n"
	edited := "#!/bin/sh\n# <switch.name> B\necho mine\n"
	os.WriteFile(filepath.Join(dir, "a.sh"), []byte(old+"\n\n"), 0o755) // 末尾多了空行也算同一版本
	os.WriteFile(filepath.Join(dir, "b.sh"), []byte(edited), 0o755)
	done := e.UpgradePresets([]Upgrade{
		{File: "a.sh", Content: "#!/bin/sh\n# <switch.name> A\necho new\n", Previous: []string{PresetFingerprint(old)}},
		{File: "b.sh", Content: "#!/bin/sh\n# <switch.name> B\necho new\n", Previous: []string{PresetFingerprint(old)}},
		{File: "missing.sh", Content: "x", Previous: []string{PresetFingerprint(old)}},
	})
	if len(done) != 1 || done[0] != "a.sh" {
		t.Fatalf("upgraded %v", done)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.sh")); string(data) != "#!/bin/sh\n# <switch.name> A\necho new\n" {
		t.Fatalf("a.sh = %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "b.sh")); string(data) != edited {
		t.Fatal("edited script was overwritten")
	}
}
