package daemon

// offergolden_test.go — S38 T17 (criterion 12): the offered tool specs a
// handler WITHOUT any catalog grants sees are byte-identical to what it was
// offered before the records projection existed. S38's claim is that the
// catalog mounts only where a step was GRANTED a record; this test is that
// claim as a digest — schema renames, description drift or a leaked record
// tool into an ungranted offer changes the bytes and fails.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/runtime"
)

// The pinned digest of the marshalled offer for the read-only handler
// (granted read+list, no catalog tools anywhere in the list). Measured at
// S38 T17 on the tree at T16; a deliberate schema change re-pins it in the
// commit that changes it, and the diff names the tool.
const readOnlyOfferSHA256 = "cedc1bb3c745645d9f59501ee39ba9d6d5fdb6c5e4e81c1e83afc6d5978246ab"

func TestOfferedToolSpecsForAGrantFreeHandlerAreByteStable(t *testing.T) {
	granted, ok := handler.ToolsFor("read-only")
	if !ok {
		t.Fatal("the read-only handler vanished from the registry")
	}
	for _, g := range granted {
		for _, sp := range records.Catalog() {
			if g == sp.Name {
				t.Fatalf("the read-only handler was granted catalog action %q — the offer under test is no longer grant-free", sp.Name)
			}
		}
	}
	r := StepRunner{}
	_, specs, err := r.BuildTools(granted, t.TempDir(), "read-only", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(specs)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != readOnlyOfferSHA256 {
		t.Errorf("the ungranted offer changed shape: sha256 %s, pinned %s — if this is deliberate (a schema edit), re-pin in the same commit and name the tool; names offered: %s", got, readOnlyOfferSHA256, namesOf(specs))
	}
	for _, sp := range specs {
		for _, cat := range records.Catalog() {
			if sp.Name == cat.Name {
				t.Errorf("catalog tool %q reached an UNGRANTED handler — the projection leaks", cat.Name)
			}
		}
	}
}

func namesOf(specs []model.ToolSpec) string {
	out := make([]string, 0, len(specs))
	for _, sp := range specs {
		out = append(out, sp.Name)
	}
	return strings.Join(out, ", ")
}
