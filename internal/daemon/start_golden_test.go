package daemon

// S27 T1 — the start door's PROVENANCE golden, extending the S40 record
// (TestTheSocketStartPathSuccessShapeIsRecorded) from success SHAPE to
// provenance COLUMNS per source, byte-compared against
// testdata/continuous_golden/start_provenance.json (S27_GOLDEN_UPDATE=1).
//
// WHAT IT RECORDS (F2 verbatim): today the LOOP row carries the raw word
// the caller passed, while the loop_run CHECK admits only
// user/agent/trigger/event — so a caller word outside that list maps to
// `trigger` on the run side. A `StartedBy` of any other word maps to
// `trigger` TODAY. This golden pins that mis-mapping in place.
//
// WHO MAY CHANGE IT AND HOW:
//   - T2 is the task that gives the scheduler its own word; the day
//     `scheduler` stops being laundered into `trigger` on the run side,
//     this golden changes AS THE HEADLINE DIFF of T2, and only there.
//   - Nothing else may move these bytes. In particular T5's kicks must
//     start loops through a path that lands in THIS table's vocabulary —
//     the audit `who_started` is exactly these columns.
//
// T2 LANDED (2026-11-05), and paid exactly as this header instructed: the
// headline diff is the `scheduler` case, which now KEEPS its word on the
// run side (blueprint.go maps it; migration 20261105025314 widened the
// CHECK). The laundering survives for every OTHER unknown word — the case
// below uses a word nobody owns to pin that the fallback did not widen
// along with the scheduler.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestStartDoorProvenanceGolden(t *testing.T) {
	type provCase struct {
		Name          string `json:"name"`
		ExtrasStarted string `json:"extrasStartedBy"`
		ExtrasTrigger string `json:"extrasTriggerId"`
		LoopStarted   string `json:"loopStartedBy"`
		LoopTrigger   string `json:"loopTriggerId"`
		RunKind       string `json:"runStartedByKind"`
	}
	var gold []provCase
	for _, c := range []struct {
		name      string
		startedBy string
		triggerID string
	}{
		{"person_zero_extras", "", ""},
		{"event_fire", "event", "tr-event-1"},
		{"schedule_fire", "trigger", "sched-1"},
		{"scheduler_word_kept", "scheduler", "sched-2"},
		{"any_other_word_laundered_to_trigger", "harvest", "sched-3"},
	} {
		s := recoveryStore(t)
		d := New(Options{Store: s})
		bp := writeGateBlueprint(t, "prov-"+c.name)
		res, err := d.startLoopWith(context.Background(), protocol.LoopStartParams{
			Blueprint: bp, Goal: "provenance under test", WorkspaceRoot: t.TempDir(),
		}, startExtras{StartedBy: c.startedBy, TriggerID: c.triggerID})
		if err != nil {
			t.Fatalf("%s: start: %v", c.name, err)
		}
		id, _ := res["id"].(string)
		waitGateParked(t, s, d, id)
		pin, err := s.ReadPinnedLoop(id)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.CurrentRun(id)
		if err != nil {
			t.Fatal(err)
		}
		gold = append(gold, provCase{
			Name: c.name, ExtrasStarted: c.startedBy, ExtrasTrigger: c.triggerID,
			LoopStarted: pin.Loop.StartedBy, LoopTrigger: pin.Loop.TriggerID,
			RunKind: run.StartedByKind,
		})
	}
	cnCheck(t, "start_provenance", gold)
	// The assertions the golden must not let rot (T2's law, replacing T1's
	// F2 pin of the laundering): the scheduler's word SURVIVES to the run
	// row; any OTHER unknown word still launders to `trigger`.
	want := map[string]string{"scheduler_word_kept": "scheduler", "any_other_word_laundered_to_trigger": "trigger"}
	for _, g := range gold {
		if w, ok := want[g.Name]; ok && g.RunKind != w {
			b, _ := json.Marshal(g)
			t.Errorf("%s: the run row says %q, want %q — this golden's header names who may move these bytes:\n%s", g.Name, g.RunKind, w, b)
		}
	}
}
