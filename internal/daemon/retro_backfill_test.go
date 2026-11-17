package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// Backfill rows arrive WITHOUT the recorder: nothing here calls
// publishLoop, so every row in these tests can only be RT13's doing.

func newBackfillFixture(t *testing.T) *retroFixture {
	return newRetroFixture(t)
}

// seedTerminal lays a loop row in a terminal status and returns its id —
// the state a pre-S63 loop would actually be in.
func (f *retroFixture) seedTerminal(t *testing.T, slug, status string) string {
	t.Helper()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: "g", Status: status},
		"dev-loop", 1, queueBlueprintYAML, "{}")
	if err != nil {
		t.Fatalf("seed %s loop: %v", slug, err)
	}
	return l.ID
}

func TestBackfillDryRunWritesNothing(t *testing.T) {
	f := newBackfillFixture(t)
	f.seedTerminal(t, "pre-alpha", "done")
	f.seedTerminal(t, "pre-beta", "blocked")
	if _, err := f.s.AppendEvent(store.NewEvent{
		Kind: "loop.blocked", LoopSlug: "pre-beta", ComponentRoot: f.root,
		Payload: map[string]any{"reason": "cap-hit: the old budget wording"},
	}); err != nil {
		t.Fatal(err)
	}
	f.seedTerminal(t, "pre-running", "running")    // open: nothing has happened
	f.seedTerminal(t, "triage-validate-9", "done") // F15: skipped by the same rule

	rep, err := f.d.RetroBackfill(context.Background(), f.root, true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Written != 0 {
		t.Fatalf("a dry run wrote %d rows (RT13)", rep.Written)
	}
	if rep.ByKind["done"] != 1 || rep.ByKind["blocked/cap-hit"] != 1 {
		t.Errorf("preview kinds %v — the stored reason's kind must land in the preview", rep.ByKind)
	}
	if rep.SkippedOpen != 1 || rep.SkippedRule != 1 {
		t.Errorf("skips open=%d rule=%d, want 1 and 1", rep.SkippedOpen, rep.SkippedRule)
	}
	rows, err := f.s.ListRetroOutcomes(store.RetroFilter{Root: f.root, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("the dry run left %d rows behind", len(rows))
	}
}

func TestBackfillWritesOnlyHistoryFields(t *testing.T) {
	f := newBackfillFixture(t)
	f.seedTerminal(t, "pre-gamma", "done")
	f.seedStep(t, "pre-gamma", "golang-engineer", "done", model.Cost{State: model.CostKnown, USD: 2.0, USDAtLeast: 2.0})
	f.seedStep(t, "pre-gamma", "golang-engineer", "reject", model.Cost{State: model.CostUnknown})

	rep, err := f.d.RetroBackfill(context.Background(), f.root, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Written != 1 {
		t.Fatalf("applied %d rows, want 1", rep.Written)
	}
	rows := f.factsFor(t, "pre-gamma")
	if len(rows) != 1 || rows[0].Source != "backfill" {
		t.Fatalf("rows: %+v", rows)
	}
	r := rows[0]
	if r.Complete {
		t.Error("a backfill row claims completeness it cannot have (RT6)")
	}
	if r.CostUSDFloor == nil || *r.CostUSDFloor != 2.0 || *r.CostUnknownSteps != 1 {
		t.Errorf("cost %v/%v — the rollup's floor verbatim, the unknown a count", r.CostUSDFloor, r.CostUnknownSteps)
	}
	if r.TasksDone != nil || r.UnverifiedSteps != nil || r.Escalations != nil || r.Attended != nil || r.WallMS != nil {
		t.Error("a field history does not carry arrived as a value — RT13 says NULL")
	}
	if !strings.HasPrefix(r.TerminalKey, "backfill@") {
		t.Errorf("terminal key %q — backfill keys are the backfill@ family, never the recorder's", r.TerminalKey)
	}
	agents, err := f.s.RetroAgentsFor(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Errorf("%d agent rows from a backfill — steps are not what history merged", len(agents))
	}
}

func TestBackfillSkipsAlreadyRecorded(t *testing.T) {
	f := newBackfillFixture(t)
	f.seedTerminal(t, "recorded-first", "done")
	if _, _, err := f.s.RecordRetroOutcome(store.RetroOutcome{
		ComponentRoot: f.root, LoopSlug: "recorded-first", OutcomeKind: "done",
		TerminalKey: "done@2026-11-04T00:00:00.000Z", Source: "recorder",
	}, nil); err != nil {
		t.Fatal(err) // the recorder's row, laid BEFORE the backfill walks
	}

	rep, err := f.d.RetroBackfill(context.Background(), f.root, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Written != 0 || rep.SkippedRecorded != 1 {
		t.Errorf("applied %d / skipped %d, want 0 / 1 — RT13 never double-counts a recorded loop", rep.Written, rep.SkippedRecorded)
	}
	if rows := f.factsFor(t, "recorded-first"); len(rows) != 1 || rows[0].Source != "recorder" {
		t.Errorf("the recorder's own row was joined by a second: %+v", rows)
	}
}
