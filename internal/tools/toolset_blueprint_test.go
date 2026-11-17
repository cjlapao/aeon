package tools

// toolset_blueprint_test.go — S48 T10 (BP9): the five blueprint names
// arrive the way every catalog tool arrives (recordTools' projection of
// records.Catalog), they are REGISTERED AND GRANTED TO NO ONE, and the
// count that grew is the counted, dated pin in toolset_test.go's
// TestRegistryExactNames (72 -> 77). What is NOT here matters as much:
// no blueprint_save / blueprint_delete / blueprint_duplicate /
// blueprint_revert name exists in the registry or the catalog — the
// by-name refusal of such a tool is cmd/aeon/blueprint_guard_test.go's
// TestNoBlueprintWriteToolExists (T8's guard, which T10's twin
// registration feeds).

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

func TestRegistryCountedAndNotDefaultGranted(t *testing.T) {
	five := []string{"blueprint_list", "blueprint_show", "blueprint_validate", "blueprint_lint", "blueprint_propose"}
	reg := Registry()
	for _, name := range five {
		if _, ok := reg[name]; !ok {
			t.Errorf("%s is not in the registry — the catalog spec exists and the projection did not carry it", name)
		}
	}
	// The catalog side of the same fact: every blueprint Spec projects, and
	// only ONE of them mutates (the projection derives class from the spec,
	// so a second Mutates spec would be a second write tool — BP9).
	mutators := 0
	for _, sp := range records.Catalog() {
		if sp.Family != "blueprint" {
			continue
		}
		if _, ok := reg[sp.Name]; !ok {
			t.Errorf("catalog spec %s never projected into the registry", sp.Name)
		}
		if sp.Mutates {
			mutators++
		}
	}
	if mutators != 1 {
		t.Errorf("%d mutating blueprint specs projected, want exactly 1 (blueprint_propose)", mutators)
	}
	// Granted to NO ONE: the default grant stays read, list, write —
	// registered says the tool exists, granted says this step may call it,
	// and the person's `tool_access` rows judge every call.
	granted := strings.Join(DefaultGrant(), ",")
	if granted != "read,list,write" {
		t.Errorf("the default grant is %q, want read, list, write — a blueprint tool granted by default is BP9 undone", granted)
	}
	for _, name := range five {
		for _, g := range DefaultGrant() {
			if g == name {
				t.Errorf("%s is in the default grant — an agent may see, check and PROPOSE only because a person granted even that", name)
			}
		}
	}
}
