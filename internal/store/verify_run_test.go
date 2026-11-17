package store

// verify_run_test.go — S57 T3's VG6 tests: a run is visible only finished
// (the draft lives in memory; a crash before FinishVerifyRun leaves no row),
// the run and its results land in one transaction, the loop slot is unique,
// and the command text is snapshotted on the result.

import (
	"testing"
	"time"
)

var s57System = Actor{Kind: "system", ID: "engine"}

// s57NewDraft creates a run draft on the named node and fails the test on
// any error (a draft creation writes nothing, so any error is a bug).
func s57NewDraft(t *testing.T, s *Store, in NewVerifyRun) *VerifyRunDraft {
	t.Helper()
	draft, err := s.CreateVerifyRun(s57System, in)
	if err != nil {
		t.Fatalf("create the run draft: %v", err)
	}
	return draft
}

// TestRunIsNeverVisibleHalfWritten — the torn-evidence proof: between
// CreateVerifyRun and FinishVerifyRun the store contains NOTHING (a crash
// there leaves no orphan run, no orphan results); at finish the run and all
// its results appear together, in seq order; a second claim of the same
// (loop, root, position, attempt) slot is refused and leaves the first run
// untouched; a different attempt is a different run.
func TestRunIsNeverVisibleHalfWritten(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "rn-root")
	started := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)

	in := NewVerifyRun{
		LoopSlug: "alpha", ComponentRoot: "rn-root", ComponentID: "rn-root", Task: "VT1",
		Position: 0, Attempt: 1, HeadSHA: "abc123", Trigger: "loop", StartedAt: started,
	}

	// A draft with results, never finished: a crash at this moment.
	draft1 := s57NewDraft(t, s, in)
	zero := func() int {
		return countTable(t, g, "verify_run") + countTable(t, g, "verify_run_result")
	}
	if err := draft1.AppendVerifyResult(VerifyRunResult{Name: "build", Requirement: "required", Command: "go build ./...", State: "pass"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := draft1.AppendVerifyResult(VerifyRunResult{Name: "test", Requirement: "advisory", Command: "go test ./...", State: "pass", ExitCode: s57ptr(0)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if n := zero(); n != 0 {
		t.Fatalf("an unfinished draft left %d rows — a run is visible only finished", n)
	}
	// draft1 is dropped without FinishVerifyRun (the crash); the store is
	// still empty of runs.
	_ = draft1
	if n := zero(); n != 0 {
		t.Fatalf("the crash left %d rows", n)
	}

	// The same slot, finished this time: the run and both results appear
	// together, in seq order.
	draft2 := s57NewDraft(t, s, in)
	if err := draft2.AppendVerifyResult(VerifyRunResult{Name: "build", Requirement: "required", Command: "go build ./...", State: "pass", ExitCode: s57ptr(0)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := draft2.AppendVerifyResult(VerifyRunResult{Name: "test", Requirement: "advisory", Command: "go test ./...", State: "pass", ExitCode: s57ptr(0)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	run, err := s.FinishVerifyRun(draft2, "pass", "", "", "", started.Add(time.Minute))
	if err != nil {
		t.Fatalf("finish the run: %v", err)
	}
	if n := countTable(t, g, "verify_run"); n != 1 {
		t.Fatalf("verify_run rows = %d, want 1", n)
	}
	if n := countTable(t, g, "verify_run_result"); n != 2 {
		t.Fatalf("verify_run_result rows = %d, want 2", n)
	}
	got, results, err := s.GetVerifyRun(run.ID)
	if err != nil {
		t.Fatalf("read the finished run: %v", err)
	}
	if got.State != "pass" || len(results) != 2 || results[0].Seq != 0 || results[1].Seq != 1 {
		t.Fatalf("run %s: state %s, %d results, seq %d then %d", got.ID, got.State, len(results), results[0].Seq, results[1].Seq)
	}

	// The reader the engine will ask: is this head of this loop of this
	// root already verified green?
	if vr, ok, err := s.LatestPassingRun("rn-root", "alpha", "abc123"); err != nil || !ok || vr.ID != run.ID {
		t.Errorf("latest passing run: %v ok=%v err=%v — want the finished run", vr.ID, ok, err)
	}
	if _, ok, err := s.LatestPassingRun("rn-root", "alpha", "wrong-sha"); err != nil || ok {
		t.Errorf("latest passing run for another head: ok=%v err=%v — want none", ok, err)
	}
	if _, ok, err := s.LatestPassingRun("rn-root", "alpha", ""); err != nil || ok {
		t.Errorf("latest passing run for an empty head: ok=%v err=%v — want none (an unknown head is not evidence)", ok, err)
	}

	// A second claim of the SAME loop slot is a duplicate dispatch: it
	// errors, and the first run stands untouched.
	draft3 := s57NewDraft(t, s, in)
	if err := draft3.AppendVerifyResult(VerifyRunResult{Name: "build", Requirement: "required", Command: "go build ./...", State: "fail"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := s.FinishVerifyRun(draft3, "fail", "build broke", "", "", started.Add(2*time.Minute)); err == nil {
		t.Fatal("the duplicate loop slot was accepted — the partial unique index is the backstop")
	}
	if n := countTable(t, g, "verify_run"); n != 1 {
		t.Fatalf("the duplicate claim wrote rows: %d runs", n)
	}
	if got, results, err := s.GetVerifyRun(run.ID); err != nil || got.State != "pass" || len(results) != 2 {
		t.Fatalf("the first run was touched: %v state=%s results=%d", err, got.State, len(results))
	}

	// A different attempt is a different run, not a duplicate.
	in4 := in
	in4.Attempt = 2
	draft4 := s57NewDraft(t, s, in4)
	if err := draft4.AppendVerifyResult(VerifyRunResult{Name: "build", Requirement: "required", Command: "go build ./...", State: "pass", ExitCode: s57ptr(0)}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := s.FinishVerifyRun(draft4, "pass", "", "", "", started.Add(3*time.Minute)); err != nil {
		t.Fatalf("the new attempt is not a duplicate: %v", err)
	}
	if n := countTable(t, g, "verify_run"); n != 2 {
		t.Fatalf("verify_run rows = %d, want 2", n)
	}
	// Newest first: the later attempt leads the listing.
	runs, err := s.ListVerifyRuns(VerifyRunFilter{ComponentRoot: "rn-root"})
	if err != nil {
		t.Fatalf("list the runs: %v", err)
	}
	if len(runs) != 2 || runs[0].Attempt != 2 || runs[1].Attempt != 1 {
		t.Fatalf("listing order: %d runs, attempt %d then %d — want newest (attempt 2) first", len(runs), runs[0].Attempt, runs[1].Attempt)
	}
}

// TestCommandSnapshotSurvivesEditAndArchive — VG6's snapshot: the result
// row carries the command text AS IT READ when the gate ran. A later edit
// (and a later archive) of the gate changes the record, never the evidence.
func TestCommandSnapshotSurvivesEditAndArchive(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "sn-root")
	original := "go build ./..."
	cmd := s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "sn-root", Name: "build", Command: original, Requirement: "required", TimeoutS: 300})
	cmdID := cmd.ID

	in := NewVerifyRun{
		LoopSlug: "alpha", ComponentRoot: "sn-root", ComponentID: "sn-root",
		Position: 0, Attempt: 1, HeadSHA: "abc123", Trigger: "loop",
		StartedAt: time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC),
	}
	draft := s57NewDraft(t, s, in)
	if err := draft.AppendVerifyResult(VerifyRunResult{
		Name: "build", Requirement: "required", Command: original,
		CommandID: &cmdID, ExitCode: s57ptr(0), State: "pass",
	}); err != nil {
		t.Fatalf("append the result: %v", err)
	}
	run, err := s.FinishVerifyRun(draft, "pass", "", "", "", in.StartedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("finish the run: %v", err)
	}

	// Now the gate's story moves on: the command is edited, then the gate
	// is archived.
	edited := "go test ./..."
	if w, err := s.UpdateVerifyCommand(s57User, cmd.ID, VerifyCommandUpdate{Command: &edited}); err != nil || !w.OK() {
		t.Fatalf("edit the gate: %v %v", w, err)
	}
	if w, err := s.ArchiveVerifyCommand(s57User, cmd.ID); err != nil || !w.OK() {
		t.Fatalf("archive the gate: %v %v", w, err)
	}

	// The evidence is unchanged: the result still reads the original text,
	// still pointing at the (now archived) gate.
	_, results, err := s.GetVerifyRun(run.ID)
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("the run has %d results, want 1", len(results))
	}
	if results[0].Command != original {
		t.Errorf("the result's command is %q — the snapshot was rewritten by the edit", results[0].Command)
	}
	if results[0].CommandID == nil || *results[0].CommandID != cmdID {
		t.Errorf("the result lost its pointer to the gate it ran")
	}
	// The record, by contrast, moved on — the two truths are distinct.
	now, err := s.GetVerifyCommand(cmd.ID)
	if err != nil {
		t.Fatalf("read the gate: %v", err)
	}
	if now.Command != edited || now.ArchivedAt == nil {
		t.Errorf("the gate record: command %q archived %v — want edited + archived", now.Command, now.ArchivedAt != nil)
	}
}
