package presets

import (
	"testing"

	"github.com/HanlunWang/simple-toggle/internal/contract"
	"github.com/HanlunWang/simple-toggle/internal/engine"
)

func TestCatalog(t *testing.T) {
	lib := Library()
	if len(lib) != len(catalog) {
		t.Fatalf("library has %d of %d scripts", len(lib), len(catalog))
	}
	seeded := 0
	for _, it := range lib {
		if it.Seeded {
			seeded++
		}
		if _, ok := contract.Parse(it.Content); !ok {
			t.Errorf("%s: not a switch", it.File)
		}
		if contract.HasErrors(contract.Lint(it.Content)) {
			t.Errorf("%s: lint errors %v", it.File, contract.Lint(it.Content))
		}
		if it.Desc == "" || it.DescEn == "" || it.Category == "" {
			t.Errorf("%s: missing description or category", it.File)
		}
	}
	if seeded != len(All()) || seeded != 9 {
		t.Fatalf("seeded %d, All %d", seeded, len(All()))
	}
}

func TestUpgradesKnowOnlyOlderVersions(t *testing.T) {
	for _, u := range Upgrades() {
		cur := engine.PresetFingerprint(u.Content)
		for _, h := range u.Previous {
			if h == cur {
				t.Errorf("%s: current version listed as previous", u.File)
			}
		}
	}
}
