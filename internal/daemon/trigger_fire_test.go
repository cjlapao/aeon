package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// The daemon half of S20 T2: what the fire RECORDS about itself. The
// trigger package's tests pin that the fire cannot CARRY a hint (the Fire
// struct has no such field); these pin what the start path authors:
// unattended, no outward, and provenance beside the pin — never inside
// the attendedness column (R-3).

type fixedClock struct{ t time.Time }

func (f *fixedClock) Now() time.Time { return f.t }

// awaitSettled waits for a started loop to leave `running`. This is not a
// sleep that MAKES the loop move (D11's ban) — with no model configured the
// loop fails within milliseconds; the poll exists because the runner's
// goroutine would otherwise outlive the test's store cleanup and panic on
// a closed handle (measured). Same discipline as the cmd suite's
// triggerAndAwaitTerminal: observe the terminal state, never sleep toward
// a hoped-for one.
func awaitSettled(t *testing.T, s *store.Store, id string) store.Loop {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	stable := 0
	var last string
	for {
		pin, err := s.ReadPinnedLoop(id)
		if err == nil && pin.Loop.Status != "running" {
			// The runner's LAST act is an emit that reads the store
			// (publishLoop → viewLoop), and it lands just after the status
			// flips — waiting only for the flip let that read race t.Cleanup's
			// Close (measured: a nil-pointer on the closed handle under full-suite
			// load). Settle means: terminal status, every step row terminal, and
			// the status UNCHANGED across three polls — then a grace beat for
			// the final emit to complete while the store is still open.
			steps, serr := s.ListSteps(store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot})
			stepsSettled := serr == nil
			for _, st := range steps {
				if st.Status == "running" {
					stepsSettled = false
				}
			}
			if stepsSettled {
				if pin.Loop.Status == last {
					stable++
				} else {
					stable, last = 0, pin.Loop.Status
				}
				if stable >= 3 {
					time.Sleep(150 * time.Millisecond) // the final emit's grace beat
					return pin.Loop
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s never settled (last err %v)", id, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestClockStartedLoopIsUnattendedAndCarriesProvenance(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	ws := t.TempDir()
	instant := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	loopID, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "nightly-survey", Blueprint: "two-step", Workspace: ws,
		Provenance: trigger.ProvenanceTrigger, Instant: instant, At: instant,
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitSettled(t, s, loopID)
	// The three questions, read from three columns (R-3 made observable).
	attended, err := s.LoopAttended(context.Background(), loopID)
	if err != nil {
		t.Fatal(err)
	}
	if attended == nil || *attended {
		t.Fatalf("a clock-started loop is unattended by the start's own authorship, got %v", attended)
	}
	outward, err := s.LoopAllowOutward(context.Background(), loopID)
	if err != nil {
		t.Fatal(err)
	}
	if outward {
		t.Fatal("a schedule cannot grant outward — there is no field to ask with (D10 stands)")
	}
	startedBy, triggerID, err := s.LoopStartedBy(context.Background(), loopID)
	if err != nil {
		t.Fatal(err)
	}
	if startedBy != "trigger" || triggerID != "nightly-survey" {
		t.Fatalf("provenance %q/%q — the row must say who started it", startedBy, triggerID)
	}
	pin, err := s.ReadPinnedLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	if want := "nightly-survey-" + instant.UTC().Format("20060102-1504"); pin.Loop.Slug != want {
		t.Fatalf("slug %q, want the schedule named beside the instant (%s) — §1's print", pin.Loop.Slug, want)
	}
	if !strings.Contains(pin.Loop.Goal, "nightly-survey") {
		t.Fatalf("the goal sentence must name the schedule that fired: %q", pin.Loop.Goal)
	}
}

func TestFireCannotClaimItsOwnProvenance(t *testing.T) {
	// Provenance is authored by the start path, not declared by the fire:
	// a Fire that arrives naming itself something it is not (operator) is
	// refused — the columns have exactly the spellings the two fire paths
	// author.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	_, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "sneaky", Blueprint: "two-step", Workspace: t.TempDir(),
		Provenance: "operator", Instant: time.Now(), At: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "only schedule and webhook fires") {
		t.Fatalf("bogus provenance accepted: %v", err)
	}
}

func TestWebhookFireCarriesItsOwnProvenanceWord(t *testing.T) {
	// Forward-declared by T6's shape and proved now: the webhook path will
	// stamp "webhook", not "trigger", and the pair reads on the loop row.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	loopID, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "on-push", Blueprint: "two-step", Workspace: t.TempDir(),
		Provenance: trigger.ProvenanceWebhook, Instant: time.Now(), At: time.Now(),
		With: map[string]string{"untrusted_payload": "…"},
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitSettled(t, s, loopID)
	startedBy, triggerID, err := s.LoopStartedBy(context.Background(), loopID)
	if err != nil {
		t.Fatal(err)
	}
	if startedBy != "webhook" || triggerID != "on-push" {
		t.Fatalf("provenance %q/%q", startedBy, triggerID)
	}
}

func TestStartedByAndAttendednessStaySeparateColumns(t *testing.T) {
	// R-3's warning, made into a test that a future collapse could not
	// pass: one loop declared attended (a person's start, attended=true)
	// and one never-declared (attended=nil) BOTH read started_by "" — the
	// provenance column does not move with attendedness, and a fired loop
	// proves the converse (started_by moves with nothing attendedness says).
	s := recoveryStore(t)
	d := New(Options{Store: s})
	ws := t.TempDir()
	yes := true
	res, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{Blueprint: "two-step", Goal: "attended person", WorkspaceRoot: ws, Slug: "attended-person", Attended: &yes})
	if err != nil {
		t.Fatal(err)
	}
	attendedID := res.(map[string]any)["id"].(string)
	res, err = d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{Blueprint: "two-step", Goal: "silent person", WorkspaceRoot: ws, Slug: "silent-person"})
	if err != nil {
		t.Fatal(err)
	}
	silentID := res.(map[string]any)["id"].(string)
	awaitSettled(t, s, attendedID)
	awaitSettled(t, s, silentID)
	for _, tc := range []struct {
		label string
		id    string
	}{
		{"attended", attendedID}, {"never-declared", silentID},
	} {
		startedBy, triggerID, err := s.LoopStartedBy(context.Background(), tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if startedBy != "" || triggerID != "" {
			t.Fatalf("a person's %s loop carries provenance %q/%q — absence must read as a person's own", tc.label, startedBy, triggerID)
		}
	}
	// And the fired loop's pair, read through the same two methods, moves
	// independently of both attended states above.
	loopID, err := d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "two-question-proof", Blueprint: "two-step", Workspace: ws,
		Provenance: trigger.ProvenanceTrigger, Instant: time.Now(), At: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	awaitSettled(t, s, loopID)
	attended, err := s.LoopAttended(context.Background(), loopID)
	if err != nil {
		t.Fatal(err)
	}
	startedBy, _, err := s.LoopStartedBy(context.Background(), loopID)
	if err != nil {
		t.Fatal(err)
	}
	if attended == nil || *attended || startedBy != "trigger" {
		t.Fatalf("attended=%v started_by=%q — the two columns answer two questions", attended, startedBy)
	}
}

func TestSchedulerTickThroughTheDaemonRecordsFiredAndProvenance(t *testing.T) {
	// The whole clock path in the daemon's own body: a row in the store,
	// the scheduler with a fixed clock, the daemon as starter — and both
	// records (the fire answer and the loop's provenance) come back true.
	s := recoveryStore(t)
	d := New(Options{Store: s})
	ws := t.TempDir()
	ctx := context.Background()
	if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "nightly-survey", Kind: "schedule", Blueprint: "two-step",
		Workspace: ws, Expr: "30 3 * * *", Zone: "UTC", CreatedAt: "2026-10-27T00:00:00Z",
		CoveredUntil: time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)
	sc := trigger.NewScheduler(triggerRows{s}, d, &fixedClock{t: at})
	if err := sc.Tick(ctx, at); err != nil {
		t.Fatal(err)
	}
	hist, err := s.TriggerFireHistory(ctx, "nightly-survey", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Outcome != "fired" || hist[0].LoopID == "" {
		t.Fatalf("the tick did not produce one fired answer: %+v", hist)
	}
	awaitSettled(t, s, hist[0].LoopID)
	startedBy, triggerID, err := s.LoopStartedBy(ctx, hist[0].LoopID)
	if err != nil {
		t.Fatal(err)
	}
	if startedBy != "trigger" || triggerID != "nightly-survey" {
		t.Fatalf("provenance %q/%q from the tick path", startedBy, triggerID)
	}
	attended, err := s.LoopAttended(ctx, hist[0].LoopID)
	if err != nil {
		t.Fatal(err)
	}
	if attended == nil || *attended {
		t.Fatalf("tick-started loop attended=%v", attended)
	}
	// And the frontier advanced in the ROW, so the same instant answers
	// nothing on the next tick.
	row, err := s.TriggerSchedule(ctx, "nightly-survey")
	if err != nil {
		t.Fatal(err)
	}
	if row.CoveredUntil == "" || row.CoveredUntil == "2026-10-27T02:00:00Z" {
		t.Fatalf("covered_until still %q", row.CoveredUntil)
	}
}
