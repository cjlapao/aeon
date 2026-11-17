package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T9 step 1 — D8's claim PROVEN, not asserted in prose: archiving a
// loop with real telemetry leaves the component/project/workspace rollups
// unchanged to the FIELD, updated_at included. The archive is the bench's
// Delete door (loop.archive); if its implementation ever grew
// telemetry-specific code that touched the tiers — even a well-meant
// "recompute" — this test would name it.

func TestArchiveLoopRollupsUnchanged(t *testing.T) {
	f, compID, _, _ := teeingStore(t)
	// The loop from teeingStore, by its real id.
	loops, err := f.s.ListLoops(store.ComponentScope{ComponentRoot: "/repo/tee"})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 {
		t.Fatalf("%d loops, want the one the fixture made", len(loops))
	}
	loopID := loops[0].ID

	before := snapshotRollups(t, f, compID)

	raw, _ := json.Marshal(protocol.LoopArchiveParams{ID: loopID, Note: "bench exercise"})
	got, err := f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodLoopArchive, raw)
	if err != nil {
		t.Fatalf("the archive door refused: %v", err)
	}
	// Dispatch hands back the JSON-normalized shape; re-decode into the
	// store's own struct — the same bytes the bench re-decodes.
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("raw archive: %s", b)
	var a store.Archive
	if err := json.Unmarshal(b, &a); err != nil {
		t.Fatalf("the archive came back as %T: %v", got, err)
	}
	// Status is the LOOP's status as archived (the fixture's never ran →
	// "active"), not a literal — the archive row is a snapshot, not a state
	// machine. The facts that matter are here: identity, state bytes, the
	// moment.
	if a.Slug != "tee-walk" || a.StateBytes <= 0 || a.ArchivedAt == "" {
		t.Errorf("the archive row: %+v", a)
	}

	after := snapshotRollups(t, f, compID)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("D8 betrayed: the archive moved the rollups\n before %+v\n  after %+v", before, after)
	}

	// And the wire says the same after as before (the bench's exact path).
	got2 := dispatchJSON(t, f, "c1", protocol.MethodTelemetryComponent, protocol.TelemetryComponentParams{ComponentID: compID})
	if got2["inputTokensTotal"] != 1700.0 || got2["costState"] != "known" {
		t.Errorf("telemetry.component after the archive drifted: %v", got2)
	}
}

func TestArchiveDoorNamesAnUnknownLoop(t *testing.T) {
	f, _, _, _ := teeingStore(t)
	raw, _ := json.Marshal(protocol.LoopArchiveParams{ID: "loop-nope"})
	got, err := f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodLoopArchive, raw)
	if err != nil {
		t.Fatalf("the refusal must travel as DATA, not an error: %v", err)
	}
	var answered struct {
		Refused struct{ Kind, Detail string } `json:"refused"`
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &answered); err != nil {
		t.Fatalf("unknown loop answered %T: %v", got, err)
	}
	if answered.Refused.Kind != "no-such-loop" {
		t.Fatalf("unknown loop answered %v", got)
	}
	if !strings.Contains(answered.Refused.Detail, "loop-nope") {
		t.Errorf("the refusal does not name the loop: %v", answered.Refused)
	}
}

type rollupSnapshot struct {
	comp store.ComponentTelemetry
	// In the component tree a project is a root, so its "project" rollup IS
	// its own component row (a root with no parent carries its children's
	// totals); a root with no parent reads as its own row.
	proj store.ComponentTelemetry
	ws   store.WorkspaceTelemetry
}

func snapshotRollups(t *testing.T, f *streamFixture, compID string) rollupSnapshot {
	t.Helper()
	c, err := f.s.ComponentTelemetry(compID)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := f.s.ReadComponent(compID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.ComponentTelemetry(comp.ID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := f.s.WorkspaceTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	return rollupSnapshot{comp: c, proj: p, ws: w}
}
