package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HanlunWang/simple-toggle/internal/store"
)

func TestChanges(t *testing.T) {
	f := newFixture(t)
	f.toggle("lamp.sh")
	if err := f.e.Start(); err != nil {
		t.Fatal(err)
	}
	defer f.e.Shutdown()

	// 契约有错误的不会存成变更
	if _, _, err := f.e.ProposeScript("", "#!/bin/sh\necho no name\n", AI, "mcp/test"); err == nil {
		t.Fatal("invalid script proposed")
	}
	created, _, err := f.e.ProposeScript("", "#!/bin/sh\n# <switch.name> Hello\n# <switch.type> action\necho hi\n", AI, "mcp/test")
	if err != nil || created.Status != "pending" || created.Kind != "create" || created.Name != "Hello" {
		t.Fatalf("propose create: %+v %v", created, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "hello.sh")); err == nil {
		t.Fatal("written before approval")
	}
	old, _ := f.e.Script("lamp.sh")
	updated, _, err := f.e.ProposeScript("lamp", strings.Replace(old, "Lamp", "Desk Lamp", 1), AI, "mcp/test")
	if err != nil || updated.Switch != "lamp.sh" || updated.Previous != old {
		t.Fatalf("propose update: %+v %v", updated, err)
	}
	if len(f.e.PendingChanges()) != 2 {
		t.Fatal("pending list")
	}

	got, err := f.e.ApproveChange(created.ID)
	if err != nil || got.Status != "approved" || got.Switch != "hello.sh" {
		t.Fatalf("approve: %+v %v", got, err)
	}
	if _, ok := f.e.Get("hello.sh"); !ok {
		t.Fatal("approved script not loaded")
	}
	if _, err := f.e.ApproveChange(created.ID); err != ErrChangeDecided {
		t.Fatalf("approve twice: %v", err)
	}
	if got, _ := f.e.RejectChange(updated.ID); got.Status != "rejected" {
		t.Fatalf("reject: %+v", got)
	}
	if now, _ := f.e.Script("lamp.sh"); now != old {
		t.Fatal("rejected change was written")
	}
	if runs := f.e.Runs(storeFilter("hello.sh")); len(runs) != 0 {
		t.Fatalf("approval should not run the script: %+v", runs)
	}
	if len(f.e.PendingChanges()) != 0 {
		t.Fatal("pending after decisions")
	}
}

func storeFilter(id string) store.Filter { return store.Filter{Switch: id} }
