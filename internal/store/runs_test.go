package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// runs_test.go is the contract of the store layer for runs (S39 T2, CR2):
// BeginRun's one-transaction claim-and-re-pin, EndRun's harmless double
// end, the reads, the step tag, and the P32 floor costs. Nothing in the
// engine calls these yet (T4/T8 do); if a rule here is unexercised later,
// this file is where it was decided.

func pinnedLoop(t *testing.T, s *Store) (Loop, Scope) {
	t.Helper()
	l, err := s.CreatePinnedLoop(NewLoop{Slug: "a", ComponentRoot: "/r", Goal: "g"}, "bp", 1, "id: bp\n", "{}")
	if err != nil {
		t.Fatalf("CreatePinnedLoop: %v", err)
	}
	return l, Scope{LoopSlug: "a", ComponentRoot: "/r"}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return openAt(t, t.TempDir())
}

func TestCreatePinnedLoopWritesRunOne(t *testing.T) {
	s := newStore(t)
	l, _ := pinnedLoop(t, s)
	r, err := s.CurrentRun(l.ID)
	if err != nil {
		t.Fatalf("a pinned loop must be born with run 1: %v", err)
	}
	if r.RunSeq != 1 || r.State != "running" || r.StartedByKind != "user" {
		t.Fatalf("run 1 is seq %d %s by %s, want 1 running user", r.RunSeq, r.State, r.StartedByKind)
	}
	if r.Content != "id: bp\n" {
		t.Fatalf("run 1 must hold its own pinned copy, got %q", r.Content)
	}
	// The trigger case: provenance rides S20's 0033 columns.
	lt, err := s.CreatePinnedLoop(NewLoop{Slug: "t", ComponentRoot: "/r", StartedBy: "trigger:clock", TriggerID: "tr1"}, "bp", 1, "id: bp\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := s.CurrentRun(lt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rt.StartedByKind != "trigger" || rt.StartedByID != "tr1" {
		t.Fatalf("a clock-started loop's run 1 is %s/%q, want trigger/tr1", rt.StartedByKind, rt.StartedByID)
	}
}

func TestEndRunClosesTheRunOnce(t *testing.T) {
	s := newStore(t)
	l, _ := pinnedLoop(t, s)
	if err := s.EndRun(l.ID, 1, "done", "{x}"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	r, _ := s.ReadRun(l.ID, 1)
	if r.State != "done" || r.EndedAt == "" || r.RuntimeJSON != "{x}" {
		t.Fatalf("closed run is %+v, want done/ended/{x}", r)
	}
	if err := s.EndRun(l.ID, 1, "done", "{y}"); !errors.Is(err, ErrRunNotRunning) {
		t.Fatalf("second EndRun = %v, want ErrRunNotRunning (a double end must be harmless)", err)
	}
	if again, _ := s.ReadRun(l.ID, 1); again.RuntimeJSON != "{x}" {
		t.Fatal("the refused second end must not rewrite the snapshot")
	}
}

func TestEndRunRefusesAnUnknownState(t *testing.T) {
	s := newStore(t)
	l, _ := pinnedLoop(t, s)
	if err := s.EndRun(l.ID, 1, "waiting", "{}"); err == nil || !strings.Contains(err.Error(), "done, failed, cancelled") {
		t.Fatalf(`EndRun("waiting") = %v, want a refusal naming the three terminal states — waiting is the LOOP's condition, never a run state (DB6)`, err)
	}
}

func TestBeginRunRefusesALoopThatIsNotIdle(t *testing.T) {
	s := newStore(t)
	l, _ := pinnedLoop(t, s)
	_, err := s.BeginRun(l.ID, NewRun{Blueprint: "two-step", Version: 1, Content: "id: two-step", StartedByKind: "user"}, `{"position":{}}`)
	if !errors.Is(err, ErrLoopNotIdle) {
		t.Fatalf("BeginRun on a running loop = %v, want ErrLoopNotIdle", err)
	}
	runs, _ := s.ListRuns(l.ID)
	if len(runs) != 1 {
		t.Fatalf("the refused claim left %d runs, want only run 1", len(runs))
	}
	pin, err := s.ReadPinnedLoop(l.ID)
	if err != nil || pin.BlueprintID != "bp" {
		t.Fatalf("the refused claim moved the pin to %q, want bp", pin.BlueprintID)
	}
	// A missing loop is ErrNotFound, not ErrLoopNotIdle.
	if _, err := s.BeginRun("nope", NewRun{Blueprint: "b", Content: "c", StartedByKind: "user"}, "{}"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BeginRun on a missing loop = %v, want ErrNotFound", err)
	}
}

func TestBeginRunStartsRunTwoAndRepinsTheLoop(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	if err := s.EndRun(l.ID, 1, "done", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLoopStatus(sc, "idle"); err != nil {
		t.Fatal(err)
	}
	r2, err := s.BeginRun(l.ID, NewRun{Blueprint: "dev-loop", Version: 3, Content: "id: dev-loop\n", StartedByKind: "user"}, `{"position":{"lane":"build"}}`)
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if r2.RunSeq != 2 || r2.State != "running" {
		t.Fatalf("second run is %d/%s, want 2/running", r2.RunSeq, r2.State)
	}
	if r2.Blueprint != "dev-loop" || r2.BlueprintVersion != 3 {
		t.Fatalf("run 2 pins %s@%d, want dev-loop@3", r2.Blueprint, r2.BlueprintVersion)
	}
	// loop.status rode the claim.
	live, err := s.GetLoop(sc)
	if err != nil || live.Status != "running" {
		t.Fatalf("loop status is %q (err %v), want running", live.Status, err)
	}
	// The pin now MEANS run 2.
	pin, err := s.ReadPinnedLoop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.BlueprintID != "dev-loop" || pin.Content != "id: dev-loop\n" || pin.RuntimeJSON != `{"position":{"lane":"build"}}` {
		t.Fatalf("the re-pin reads %s/%q/%q, want dev-loop/dev-loop yaml/position", pin.BlueprintID, pin.Content, pin.RuntimeJSON)
	}
	// Run 1 keeps its own immutable copy.
	if r1, _ := s.ReadRun(l.ID, 1); r1.Content != "id: bp\n" {
		t.Fatalf("run 1's pinned copy drifted to %q", r1.Content)
	}
}

func TestBeginRunIsAtomic(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	if err := s.EndRun(l.ID, 1, "done", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLoopStatus(sc, "idle"); err != nil {
		t.Fatal(err)
	}
	// Sabotage the pin row so step 5 must fail; the claim and the run row
	// must be rolled back with it — no half-started run, no loop claiming
	// to run a blueprint whose pin does not exist.
	if _, err := s.w.Exec(`DELETE FROM loop_blueprint WHERE loop_id=?`, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRun(l.ID, NewRun{Blueprint: "x", Version: 1, Content: "id: x", StartedByKind: "user"}, "{}"); err == nil {
		t.Fatal("BeginRun with no pin row must fail (affectPinnedOne)")
	}
	live, err := s.GetLoop(sc)
	if err != nil || live.Status != "idle" {
		t.Fatalf("the failed claim left the loop %q (err %v), want idle — BeginRun is ONE transaction", live.Status, err)
	}
	runs, _ := s.ListRuns(l.ID)
	if len(runs) != 1 {
		t.Fatalf("the failed BeginRun left %d run rows, want only run 1", len(runs))
	}
}

func TestBeginRunRefusesWhileARunIsStillRunning(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	// Force the inconsistency directly: idle loop, still-running run 1.
	if err := s.SetLoopStatus(sc, "idle"); err != nil {
		t.Fatal(err)
	}
	_, err := s.BeginRun(l.ID, NewRun{Blueprint: "x", Version: 1, Content: "id: x", StartedByKind: "user"}, "{}")
	if err == nil || !strings.Contains(err.Error(), "run 1 is still running") {
		t.Fatalf("BeginRun over a live run = %v, want an error naming run 1 (data corruption, said plainly)", err)
	}
}

func TestListRunsIsAscendingAndNeverNil(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	runs, err := s.ListRuns(l.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("fresh loop: %d runs (err %v), want 1", len(runs), err)
	}
	if _, err := s.w.Exec(`DELETE FROM loop_run WHERE loop_id=?`, l.ID); err != nil {
		t.Fatal(err)
	}
	runs, err = s.ListRuns(l.ID)
	if err != nil || runs == nil || len(runs) != 0 {
		t.Fatalf("no runs: %#v (err %v), want an empty NON-nil slice", runs, err)
	}
	// Two ordered runs, written honestly (the end-CHECK and NOT NULLs are
	// the migration's own contract, pinned in T1; this test owns ordering).
	if err := s.SetLoopStatus(sc, "running"); err != nil {
		t.Fatal(err)
	}
	for seq := 1; seq <= 2; seq++ {
		if _, err := s.w.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_by_id,started_at,ended_at) VALUES (?,?,'b',1,'c','done','user','','now','now')`, l.ID, seq); err != nil {
			t.Fatal(err)
		}
	}
	runs, _ = s.ListRuns(l.ID)
	if len(runs) != 2 || runs[0].RunSeq != 1 || runs[1].RunSeq != 2 {
		t.Fatalf("runs are %v, want ascending 1,2", runs)
	}
}

func TestRunCostIsAFloorAndNamesWhatItDoesNotKnow(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	one, err := s.CreateStep(sc, NewStep{Phase: "build", Agent: "x"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.CreateStep(sc, NewStep{Phase: "build", Agent: "x"})
	if err != nil {
		t.Fatal(err)
	}
	three, err := s.CreateStep(sc, NewStep{Phase: "build", Agent: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(one.ID, model.Cost{State: model.CostKnown, USD: 0.50}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(two.ID, model.Cost{State: model.CostPartial, USDAtLeast: 0.20}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(three.ID, model.Cost{State: model.CostUnknown}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{one.ID, two.ID, three.ID} {
		if err := s.SetStepRun(id, 1); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.RunCost(l.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.USDAtLeast < 0.699 || c.USDAtLeast > 0.701 {
		t.Fatalf("floor is %v, want 0.70 (the partial step's floor counts; the unknown's absence must NOT become a silent zero)", c.USDAtLeast)
	}
	if c.Exact || c.Partial != 1 || c.Unknown != 1 || c.Steps != 3 {
		t.Fatalf("cost honesty is %+v, want exact=false partial=1 unknown=1 steps=3", c)
	}
}

func TestRunCostGroupsByRun(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	a, _ := s.CreateStep(sc, NewStep{Phase: "build", Agent: "x"})
	b, _ := s.CreateStep(sc, NewStep{Phase: "build", Agent: "x"})
	untagged, _ := s.CreateStep(sc, NewStep{Phase: "build", Agent: "x"})
	s.SetStepCost(a.ID, model.Cost{State: model.CostKnown, USD: 0.10})
	s.SetStepCost(b.ID, model.Cost{State: model.CostKnown, USD: 0.30})
	s.SetStepCost(untagged.ID, model.Cost{State: model.CostKnown, USD: 0.25})
	s.SetStepRun(a.ID, 1)
	s.SetStepRun(b.ID, 2)
	if c, _ := s.RunCost(l.ID, 1); c.USDAtLeast != 0.10 || c.Steps != 1 {
		t.Fatalf("run 1 cost %+v, want 0.10/1 step", c)
	}
	if c, _ := s.RunCost(l.ID, 2); c.USDAtLeast != 0.30 || c.Steps != 1 {
		t.Fatalf("run 2 cost %+v, want 0.30/1 step", c)
	}
	c, _ := s.LoopCost(l.ID)
	if c.USDAtLeast < 0.649 || c.USDAtLeast > 0.651 || c.Steps != 3 {
		t.Fatalf("loop cost %+v, want 0.65 over 3 steps (untagged steps ride the loop total — old data keeps costing honestly)", c)
	}
}

func TestSetStepRunRefusesAMissingStep(t *testing.T) {
	s := newStore(t)
	if err := s.SetStepRun(999999, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetStepRun(missing) = %v, want ErrNotFound", err)
	}
}

func TestRunsAreDeletedWithTheirLoop(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	if err := s.EndRun(l.ID, 1, "done", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLoopStatus(sc, "idle"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRun(l.ID, NewRun{Blueprint: "b", Version: 1, Content: "c", StartedByKind: "user"}, "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`DELETE FROM loop WHERE id=?`, l.ID); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListRuns(l.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs survived their loop: %d rows (err %v), want 0 (ON DELETE CASCADE)", len(runs), err)
	}
}

// restLoop parks a pinned loop the way the engine does at the end of a
// converse run: the run closes first, then the loop rests.
func restLoop(t *testing.T, s *Store, l Loop, sc Scope) {
	t.Helper()
	if err := s.EndRun(l.ID, 1, "done", "{}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLoopStatus(sc, "idle"); err != nil {
		t.Fatal(err)
	}
}

func TestCloseIdleLoopClaimsFromIdle(t *testing.T) {
	s := newStore(t)
	l, sc := pinnedLoop(t, s)
	// A running loop refuses first: the claim is the whole door.
	if err := s.CloseIdleLoop(l.ID); !errors.Is(err, ErrLoopNotIdle) {
		t.Fatalf("closing a RUNNING loop = %v, want ErrLoopNotIdle", err)
	}
	if again, _ := s.GetLoop(sc); again.Status == "done" {
		t.Fatal("the refused close ended the loop")
	}
	restLoop(t, s, l, sc)
	if err := s.CloseIdleLoop(l.ID); err != nil {
		t.Fatalf("CloseIdleLoop: %v", err)
	}
	if again, _ := s.GetLoop(sc); again.Status != "done" {
		t.Fatalf("closed loop is %q, want done", again.Status)
	}
	// Closing twice is a refusal, not a second success — done is not idle.
	if err := s.CloseIdleLoop(l.ID); !errors.Is(err, ErrLoopNotIdle) {
		t.Fatalf("second close = %v, want ErrLoopNotIdle", err)
	}
	if err := s.CloseIdleLoop("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("close of a missing loop = %v, want ErrNotFound", err)
	}
}

func TestIdleLoopSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	s := openAt(t, dir)
	l, sc := pinnedLoop(t, s)
	restLoop(t, s, l, sc)
	before, err := s.ListRuns(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := openAt(t, dir)
	again, err := s2.GetLoop(sc)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "idle" {
		t.Fatalf("after reopen the loop is %q, want idle — the resting state is a ROW, not a memory (P2)", again.Status)
	}
	after, err := s2.ListRuns(l.ID)
	if err != nil || len(after) != len(before) {
		t.Fatalf("runs changed across the reopen: %d vs %d (err %v)", len(after), len(before), err)
	}
	if after[0].State != "done" || after[0].EndedAt == "" {
		t.Fatalf("the closed run reopened as %s/ended=%q", after[0].State, after[0].EndedAt)
	}
}

// TestSetRunKickoffLinksTheMessage pins S39 T8's CR6 promise at the row
// level: the run's first message is LINKED, not copied, and a link to a run
// that does not exist refuses instead of silently succeeding.
func TestSetRunKickoffLinksTheMessage(t *testing.T) {
	s := newStore(t)
	l, _ := pinnedLoop(t, s)
	if err := s.EndRun(l.ID, 1, "done", `{"position":{}}`); err != nil {
		t.Fatal(err)
	}
	// The loop rests — engine-side resting is T7's job, and here it is set
	// directly so the claim under test has its idle door.
	if err := s.SetLoopStatus(Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}, "idle"); err != nil {
		t.Fatal(err)
	}
	run, err := s.BeginRun(l.ID, NewRun{Blueprint: "two", Version: 1, Content: "id: two", StartedByKind: "user"}, `{"position":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if run.KickoffMessageID != nil {
		t.Fatalf("a fresh run arrived with a message link: %v", run.KickoffMessageID)
	}
	sc := Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}
	msg, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "start the work", Source: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunKickoff(l.ID, run.RunSeq, msg.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadRun(l.ID, run.RunSeq)
	if err != nil {
		t.Fatal(err)
	}
	if got.KickoffMessageID == nil || *got.KickoffMessageID != msg.ID {
		t.Fatalf("the run does not link its kickoff: %v, want %d", got.KickoffMessageID, msg.ID)
	}
	if err := s.SetRunKickoff(l.ID, 42, msg.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("linking run 42 = %v, want ErrNotFound — zero rows must not pass silently", err)
	}
}
