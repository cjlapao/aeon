package store

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T5's tests. The one that matters is negative: cost_state is READ FROM THE
// COLUMN, never inferred.

func TestAllThreeCostStatesRoundTrip(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "cost", "/repo")

	cases := []struct {
		name string
		cost model.Cost
	}{
		{"known", model.Cost{State: model.CostKnown, USD: 0.00081}},
		{"partial", model.Cost{State: model.CostPartial, USDAtLeast: 0.0004,
			Missing: []string{"cacheWriteUsdPerMTok", "reasoningTokens"}}},
		{"unknown", model.Cost{State: model.CostUnknown,
			Reason: "the stream aborted before usage arrived"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := seedStep(t, s, sc)
			if err := s.SetStepCost(st.ID, tc.cost); err != nil {
				t.Fatalf("write: %v", err)
			}
			back, err := s.ReadStep(st.ID)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if back.Cost.State != tc.cost.State {
				t.Fatalf("state came back as %q, want %q", back.Cost.State, tc.cost.State)
			}
			switch tc.cost.State {
			case model.CostKnown:
				if back.Cost.USD != tc.cost.USD {
					t.Errorf("usd %v, want %v", back.Cost.USD, tc.cost.USD)
				}
			case model.CostPartial:
				if back.Cost.USDAtLeast != tc.cost.USDAtLeast {
					t.Errorf("usdAtLeast %v, want %v", back.Cost.USDAtLeast, tc.cost.USDAtLeast)
				}
				if strings.Join(back.Cost.Missing, ",") != strings.Join(tc.cost.Missing, ",") {
					t.Errorf("missing %v, want %v", back.Cost.Missing, tc.cost.Missing)
				}
			case model.CostUnknown:
				if back.Cost.Reason != tc.cost.Reason {
					t.Errorf("reason %q, want %q", back.Cost.Reason, tc.cost.Reason)
				}
			}
		})
	}
}

func TestCostStateIsReadFromTheColumnNotInferredFromNullity(t *testing.T) {
	// D12, and THE test for this file.
	//
	// A `partial` and a `known` both with a NON-NULL cost_usd. An
	// implementation inferring the state from whether cost_usd is null cannot
	// tell them apart — and every positive test above would still pass against
	// it, because each state's own round trip would look fine in isolation.
	//
	// Collapsing `partial` into `known` destroys the lower-bound semantics four
	// slices have preserved: cost rollups and the app's "at least $X" both read
	// this column.
	s := openTemp(t)
	sc := seedLoop(t, s, "distinguishable", "/repo")

	known := seedStep(t, s, sc)
	partial := seedStep(t, s, sc)

	// The SAME number, so nothing but the state can distinguish them.
	const amount = 0.0025
	if err := s.SetStepCost(known.ID, model.Cost{State: model.CostKnown, USD: amount}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(partial.ID, model.Cost{State: model.CostPartial, USDAtLeast: amount,
		Missing: []string{"cacheReadUsdPerMTok"}}); err != nil {
		t.Fatal(err)
	}

	// Both rows hold a non-null cost_usd, which is the premise of the test.
	for _, id := range []int64{known.ID, partial.ID} {
		var usd *float64
		if err := s.r.QueryRow(`SELECT cost_usd FROM step WHERE id = ?`, id).Scan(&usd); err != nil {
			t.Fatal(err)
		}
		if usd == nil {
			t.Fatalf("step %d stored a NULL cost_usd, so this test is not testing what it says", id)
		}
	}

	k, err := s.ReadStep(known.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ReadStep(partial.ID)
	if err != nil {
		t.Fatal(err)
	}

	if k.Cost.State != model.CostKnown {
		t.Errorf("the known cost came back as %q", k.Cost.State)
	}
	if p.Cost.State != model.CostPartial {
		t.Errorf("the partial cost came back as %q — a partial has a number too, it is just a floor",
			p.Cost.State)
	}
	if k.Cost.USD != amount {
		t.Errorf("the known cost's total is %v, want %v", k.Cost.USD, amount)
	}
	if p.Cost.USDAtLeast != amount {
		t.Errorf("the partial cost's floor is %v, want %v — storing NULL would discard the part "+
			"somebody is about to be billed for", p.Cost.USDAtLeast, amount)
	}
	if len(p.Cost.Missing) == 0 {
		t.Error("the partial cost lost its Missing list, so nothing says WHAT could not be priced")
	}
}

func TestAnUnknownCostKeepsItsMeasuredFloor(t *testing.T) {
	// P32: an aggregate over uncertain values is itself uncertain, and it keeps
	// the measured floor. An unknown cost with a floor of zero and one with a
	// floor of $4 are different facts.
	s := openTemp(t)
	sc := seedLoop(t, s, "floor", "/repo")

	withFloor := seedStep(t, s, sc)
	if err := s.SetStepCost(withFloor.ID, model.Cost{State: model.CostUnknown,
		USDAtLeast: 4.20, Reason: "one route in the rollup had no prices"}); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadStep(withFloor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Cost.USDAtLeast != 4.20 {
		t.Errorf("an unknown cost lost its floor: %v", back.Cost.USDAtLeast)
	}
	if back.Cost.Reason == "" {
		t.Error("an unknown cost lost its reason, so nothing says why it is unknown")
	}

	bare := seedStep(t, s, sc)
	if err := s.SetStepCost(bare.ID, model.Cost{State: model.CostUnknown, Reason: "nothing measured"}); err != nil {
		t.Fatal(err)
	}
	var usd *float64
	if err := s.r.QueryRow(`SELECT cost_usd FROM step WHERE id = ?`, bare.ID).Scan(&usd); err != nil {
		t.Fatal(err)
	}
	if usd != nil {
		t.Errorf("an unknown cost with nothing measured stored %v; it must be NULL, because zero would "+
			"read as 'it cost nothing'", *usd)
	}
}

func TestANewStepStartsUnknownRatherThanFree(t *testing.T) {
	// "Nobody has said" and "it cost nothing" are different facts, and only
	// one of them is ever true of a model call.
	s := openTemp(t)
	sc := seedLoop(t, s, "fresh", "/repo")
	st := seedStep(t, s, sc)
	if st.Cost.State != model.CostUnknown {
		t.Errorf("a new step's cost state is %q, want unknown", st.Cost.State)
	}
}

func TestAnInvalidCostStateIsRefusedBeforeItReachesTheColumn(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "invalid", "/repo")
	st := seedStep(t, s, sc)
	err := s.SetStepCost(st.ID, model.Cost{State: "approximately"})
	if err == nil {
		t.Fatal("a cost state outside the three was accepted")
	}
	if !strings.Contains(err.Error(), "known, partial and unknown") {
		t.Errorf("the refusal does not name the three states: %v", err)
	}
}

func TestCostIsWrittenAndReadThroughOneFunctionEach(t *testing.T) {
	// Asserted by grep. A caller that writes `cost_state` directly will
	// eventually write one inconsistent with `cost_usd`, and two call sites
	// cannot be made to agree by review.
	//
	// S33 amendment, re-pointed at what the rule actually protects: the
	// STEP row's cost triple. A bare `cost_state =` grep also fired on
	// telemetry.go's rollup-tier columns — tables whose cost_state is THAT
	// table's own D9 lattice (floorCostState, one definition, pinned by
	// TestFloorCostStateIsOneLattice), not the step's cost the SetStepCost
	// rule constrains. Narrowing the pattern to `UPDATE step SET … cost_*`
	// keeps the original teeth (no file but steps.go may write the step's
	// cost columns) while not inventing a second definition of the rollup's
	// state in SQL just to dodge a name collision.
	columns := regexp.MustCompile(`UPDATE step[\s\S]{0,400}?(cost_state|cost_usd|cost_detail)\s*=`)
	for name, src := range packageFiles(t) {
		if name == "schema.go" || name == "steps.go" {
			continue // the DDL, and the one writer
		}
		if m := columns.FindString(src); m != "" {
			t.Errorf("%s writes the step's cost columns directly (%s); go through SetStepCost", name, m)
		}
	}

	// And within steps.go, exactly one statement writes them.
	writes := regexp.MustCompile(`UPDATE step SET cost_usd`)
	if n := len(writes.FindAllString(packageFiles(t)["steps.go"], -1)); n != 1 {
		t.Errorf("steps.go has %d statements writing the cost columns, want exactly 1", n)
	}
}

func TestContextManifestRoundTripsWithAHashPerFileIterationsAndDeadline(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "manifest", "/repo")
	st := seedStep(t, s, sc)

	cm := ContextManifest{
		Files: []ContextFile{
			{Path: "docs/aeon/S4-store/brief.md", Hash: "sha256:aaaa"},
			{Path: "aeon/internal/store/open.go", Hash: "sha256:bbbb"},
		},
		IterationsUsed: 3,
		DeadlineAt:     "2026-10-15T11:00:00.000Z",
	}
	if err := s.SetContextManifest(st.ID, cm); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ContextManifest == nil {
		t.Fatal("the manifest came back nil")
	}
	if len(back.ContextManifest.Files) != 2 {
		t.Fatalf("files came back as %+v", back.ContextManifest.Files)
	}
	for i, f := range cm.Files {
		if back.ContextManifest.Files[i] != f {
			t.Errorf("file %d came back as %+v, want %+v", i, back.ContextManifest.Files[i], f)
		}
	}
	if back.ContextManifest.IterationsUsed != 3 {
		t.Errorf("iterationsUsed is %d", back.ContextManifest.IterationsUsed)
	}
	if back.ContextManifest.DeadlineAt != cm.DeadlineAt {
		t.Errorf("deadlineAt is %q", back.ContextManifest.DeadlineAt)
	}
}

func TestDeadlineAtIsStoredAsAnAbsoluteTimestampNotADuration(t *testing.T) {
	// P5. A relative deadline cannot survive a suspension that lasts
	// overnight: it would grant the resumed step its whole budget again, and a
	// step could escape its bounds by suspending repeatedly.
	//
	// Asserted on the stored JSON, because the field could hold "30m" and
	// every round-trip test would still pass.
	s := openTemp(t)
	sc := seedLoop(t, s, "deadline", "/repo")
	st := seedStep(t, s, sc)
	if err := s.SetContextManifest(st.ID, ContextManifest{DeadlineAt: "2026-10-15T11:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.r.QueryRow(`SELECT context_manifest FROM step WHERE id = ?`, st.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "2026-10-15T11:00:00.000Z") {
		t.Errorf("the stored manifest does not hold an absolute instant: %s", raw)
	}
	for _, duration := range []string{`"30m"`, `"1h"`, `"PT30M"`} {
		if strings.Contains(raw, duration) {
			t.Errorf("the stored manifest holds a duration (%s): %s", duration, raw)
		}
	}
}

func TestAStepWithNoManifestReadsBackNilRatherThanEmpty(t *testing.T) {
	// "No manifest was frozen" and "a manifest with no files" are different
	// facts — the first is every step until S5 assembles one.
	s := openTemp(t)
	sc := seedLoop(t, s, "nomanifest", "/repo")
	st := seedStep(t, s, sc)
	if st.ContextManifest != nil {
		t.Errorf("a new step has a manifest: %+v", st.ContextManifest)
	}
}

func TestStepStatusDistinguishesRunningSuspendedOrphanedAndDone(t *testing.T) {
	// §3a. A SUSPENDED step has no process BY DESIGN (P2); an ORPHANED one has
	// no process because the daemon died. A reconciler treating "no process" as
	// orphaned would mark every waiting loop as crashed and try to recover it.
	s := openTemp(t)
	sc := seedLoop(t, s, "statuses", "/repo")

	ids := map[StepStatus]int64{}
	for _, status := range StepStatuses() {
		st := seedStep(t, s, sc)
		if err := s.SetStepStatus(st.ID, status, ""); err != nil {
			t.Fatalf("set %s: %v", status, err)
		}
		ids[status] = st.ID
	}

	// S7's query reads `running` ONLY — that is the whole point of the
	// distinction.
	running, err := s.StepsByStatus(StepRunning)
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 || running[0].ID != ids[StepRunning] {
		t.Errorf("StepsByStatus(running) returned %d steps; recovery must see exactly the one", len(running))
	}
	suspended, err := s.StepsByStatus(StepSuspended)
	if err != nil {
		t.Fatal(err)
	}
	if len(suspended) != 1 {
		t.Errorf("StepsByStatus(suspended) returned %d steps", len(suspended))
	}
	for _, st := range running {
		if st.Status == string(StepSuspended) {
			t.Error("a suspended step appeared in the running set; recovery would try to recover a loop " +
				"that is merely waiting for an answer")
		}
	}
}

func TestMarkingAStepDoneStampsItsEndTime(t *testing.T) {
	// A step that is done and has no end time is a step nothing can measure.
	s := openTemp(t)
	sc := seedLoop(t, s, "done", "/repo")
	st := seedStep(t, s, sc)
	if st.EndedAt != "" {
		t.Fatalf("a new step already has an end time: %q", st.EndedAt)
	}
	if err := s.SetStepStatus(st.ID, StepDone, "succeeded"); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.EndedAt == "" {
		t.Error("a done step has no ended_at")
	}
	if back.Outcome != "succeeded" {
		t.Errorf("outcome is %q", back.Outcome)
	}
}

func TestStepsAreScopedToTheirLoop(t *testing.T) {
	// The same slug in two roots again, this time for steps.
	s := openTemp(t)
	a := seedLoop(t, s, "refactor", "/repo-a")
	b := seedLoop(t, s, "refactor", "/repo-b")
	seedStep(t, s, a)
	seedStep(t, s, b)
	seedStep(t, s, b)

	inA, err := s.ListSteps(a)
	if err != nil {
		t.Fatal(err)
	}
	inB, err := s.ListSteps(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(inA) != 1 || len(inB) != 2 {
		t.Errorf("steps crossed projects: /repo-a has %d, /repo-b has %d", len(inA), len(inB))
	}
}

// A manifest field the store's own view does not declare must still survive.
//
// # The defect this is the guard for
//
// `SaveStepContext` unmarshalled the runtime's manifest into this package's
// narrower `ContextManifest` and let the store re-encode it. Every field the
// view did not name was dropped, and two were: `maxIterations` — the whole
// reason the bound is carried on the manifest rather than read from today's
// default — and each file's `bytes`.
//
// It was found by walking S5's demo: `aeon step show 1 --manifest` printed
// `iterations 2 of 0`. No test could have caught it, because every test wrote
// through this package's own type, which cannot lose a field it declares.
//
// So this one writes BYTES and reads BYTES, which is the only way to assert the
// column is not a filter.
func TestAManifestFieldThisPackageDoesNotKnowAboutSurvives(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "manifest-bytes", "/repo")
	st := seedStep(t, s, sc)

	// The runtime's shape, including two fields this package's view has never
	// heard of.
	raw := `{"files":[{"path":"a.md","hash":"sha256:abc","bytes":2048}],` +
		`"iterationsUsed":2,"deadlineAt":"2026-10-16T03:12:41Z","maxIterations":12}`
	if err := s.SetContextManifestJSON(st.ID, []byte(raw)); err != nil {
		t.Fatal(err)
	}

	read, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored := string(read.ContextManifestRaw)
	for _, field := range []string{`"maxIterations":12`, `"bytes":2048`} {
		if !strings.Contains(stored, field) {
			t.Errorf("the stored manifest lost %s:\n%s", field, stored)
		}
	}
	if stored != raw {
		t.Errorf("the manifest was re-encoded.\nwrote %s\nread  %s", raw, stored)
	}
}

// And text that is not JSON is refused rather than stored.
//
// A column holding something that is not JSON is a column every later reader
// has to guard, and that guard gets written by somebody debugging something
// else entirely.
func TestANonJSONManifestIsRefused(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "manifest-bad", "/repo")
	st := seedStep(t, s, sc)

	if err := s.SetContextManifestJSON(st.ID, []byte("not json at all")); err == nil {
		t.Fatal("a non-JSON manifest was stored")
	}
}

// --- pause_reason: a stop, told apart from a real question --------------------

func TestPauseReasonRoundTripsAndDefaultsToEmpty(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "pause-reason", "/repo")
	st := seedStep(t, s, sc)

	// Every step before this column existed, and every step nobody has
	// stopped, reads back empty — `status='suspended'` still means exactly
	// what it meant before this migration unless this is also set.
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.PauseReason != "" {
		t.Errorf("a step nobody stopped has PauseReason %q", back.PauseReason)
	}

	if err := s.SetStepPauseReason(st.ID, "user"); err != nil {
		t.Fatal(err)
	}
	back, err = s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.PauseReason != "user" {
		t.Errorf("PauseReason is %q, want %q", back.PauseReason, "user")
	}
}

func TestClearingThePauseReasonIsAWriteNotANoop(t *testing.T) {
	// A continuation clears it so a step running again never carries a stale
	// reason from the time it was not — asserted by actually setting then
	// clearing, rather than trusting an empty string was never written.
	s := openTemp(t)
	sc := seedLoop(t, s, "pause-clear", "/repo")
	st := seedStep(t, s, sc)

	if err := s.SetStepPauseReason(st.ID, "user"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepPauseReason(st.ID, ""); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.PauseReason != "" {
		t.Errorf("clearing left PauseReason %q", back.PauseReason)
	}
}

func TestSetStepPauseReasonRefusesAMissingStep(t *testing.T) {
	s := openTemp(t)
	if err := s.SetStepPauseReason(999999, "user"); err == nil {
		t.Fatal("a missing step accepted a pause reason")
	}
}

func TestPauseReasonDoesNotChangeWhatStatusMeans(t *testing.T) {
	// P2 and the status column's own doc comment say `suspended` means "no
	// process, by design, resumable" — true for a real question AND a user
	// stop alike. This is the one test that would fail if a later change
	// quietly started deriving "awaiting" from PauseReason instead of from
	// the question table — it must not: a user-paused step has no question
	// row, and IsAwaiting must still say so.
	s := openTemp(t)
	sc := seedLoop(t, s, "pause-not-awaiting", "/repo")
	st := seedStep(t, s, sc)

	if err := s.SetStepStatus(st.ID, StepSuspended, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepPauseReason(st.ID, "user"); err != nil {
		t.Fatal(err)
	}

	awaiting, err := s.IsAwaiting(sc)
	if err != nil {
		t.Fatal(err)
	}
	if awaiting {
		t.Error("a user-paused step with no question reads as awaiting a human answer")
	}
}
