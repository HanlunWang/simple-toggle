// Package arch 检查分层规则：internal/ 下的包不得依赖 mygo（界面框架只属于 app/ 层）。
package arch

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInternalDoesNotImportMygo(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "github.com/HanlunWang/open-toggle/internal/...").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.HasPrefix(p, "github.com/egoist/mygo") {
			t.Errorf("internal/ depends on %s; only app/ may import mygo", p)
		}
	}
}
