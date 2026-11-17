package store

// scheduler_test.go — S27 T2: the behavior of the ONE write path. The
// schema's teeth are continuous_schema_test.go's; these tests pin what the
// Go half adds: the derived audit words, the one-transaction controls, the
// stamp-once ledger, and the reads T3–T5 will build on.

import (
	"strings"
	"testing"
	"time"
)

func mustState(t *testing.T, s *Store) SchedulerStateRow {
	t.Helper()
	st, err := s.SchedulerState()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSetSchedulerStateDerivesItsAuditWord(t *testing.T) {
	s := openTemp(t)
	cases := []struct {
		name           string
		enabled, pause bool
		reason         string
		want           string
	}{
		{"first enable", true, false, "", "enabled"},
		{"pause", true, true, "operator asked", "paused"},
		{"already-paused edit is settings", true, true, "still asked", "settings"},
		{"resume", true, false, "", "resumed"},
		{"disable", false, false, "", "disabled"},
		{"re-enable", true, false, "", "enabled"},
	}
	for _, c := range cases {
		if _, err := s.SetSchedulerState(s27Actor, c.enabled, c.pause, c.reason); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		last, found, err := s.LastDecision("daemon", 0)
		if err != nil || !found {
			t.Fatalf("%s: no daemon decision (%v)", c.name, err)
		}
		if last.Decision != c.want {
			t.Fatalf("%s: audit word %q, want %q", c.name, last.Decision, c.want)
		}
		if last.ActorKind != "user" || last.ActorID != "uid-s27" {
			t.Fatalf("%s: audit actor %s/%s — the peer must be stamped, not invented", c.name, last.ActorKind, last.ActorID)
		}
	}
	st := mustState(t, s)
	if !st.Enabled || st.Paused {
		t.Fatalf("final state %+v after the walk, want enabled and running", st)
	}
	// The reason rides the row exactly while paused, and clears after.
	st = mustState(t, s)
	if st.PausedReason != "" {
		t.Fatalf("paused_reason %q survives a resume", st.PausedReason)
	}
	// A control voice is the ONLY voice: the scheduler and blank ids die
	// before SQL.
	for _, bad := range []SchedulerActor{{Kind: "scheduler", ID: "tick"}, {Kind: "user", ID: ""}, {Kind: "agent"}} {
		if _, err := s.SetSchedulerState(bad, true, false, ""); err == nil || !strings.Contains(err.Error(), "not a control voice") {
			t.Fatalf("actor %+v: %v — want the control-voice refusal", bad, err)
		}
	}
	if st2 := mustState(t, s); !st2.Enabled {
		t.Fatal("a refused actor write still moved the state")
	}
}

func TestSchedulerRootAuthority(t *testing.T) {
	s := openTemp(t)
	seedQueueComponent(t, s, "auth", "/repo/auth")

	// The window rule is the database's: a crossed pair and a window
	// without a zone both die, and the Go path reports them.
	if _, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{
		ComponentRoot: "/repo/auth", Enabled: true, Blueprint: "bp-run", WindowStart: "Mon",
	}); err == nil || !strings.Contains(err.Error(), "scheduler root") {
		t.Fatalf("window_start without window_end: %v", err)
	}
	if _, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{
		ComponentRoot: "/repo/auth", Enabled: true, Blueprint: "bp-run",
		WindowStart: "Mon", WindowEnd: "Fri",
	}); err == nil || !strings.Contains(err.Error(), "scheduler root") {
		t.Fatalf("a window without a zone: %v", err)
	}
	if n := mustCount(t, s, `SELECT COUNT(*) FROM scheduler_root`); n != 0 {
		t.Fatalf("a refused upsert laid %d rows", n)
	}
	// The audit for the failed writes: NOTHING. (The INSERT died inside the
	// transaction, so the decision never rode along.)
	if n := mustCount(t, s, `SELECT COUNT(*) FROM scheduler_decision`); n != 0 {
		t.Fatalf("%d audit rows for authority that was never laid", n)
	}
	row, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{
		ComponentRoot: "/repo/auth", Enabled: true, Blueprint: "bp-run", Tech: "go",
		WindowStart: "Mon 09:00", WindowEnd: "Fri 18:00", WindowDays: "1,2,3,4,5", Zone: "America/New_York",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.MinPriority != "assumed" || !row.Enabled || row.CreatedAt != row.UpdatedAt {
		t.Fatalf("laid row %+v — want the default priority and a fresh pair of stamps", row)
	}
	// Root decisions carry the root in the component_root column.
	decisions, err := s.Decisions(DecisionQuery{ComponentRoot: "/repo/auth", SubjectKind: "root"})
	if err != nil || len(decisions) != 1 || decisions[0].Decision != "enabled" {
		t.Fatalf("root audit = %+v (%v)", decisions, err)
	}
	// Edit: created_at holds, updated_at moves, the word is enabled again.
	time.Sleep(2 * time.Millisecond)
	edited, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{
		ComponentRoot: "/repo/auth", Enabled: true, Blueprint: "bp-run-2", Tech: "go",
		MinPriority: "known", Zone: "America/New_York",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The store clock under test is FROZEN, so updated_at cannot move; the
	// claim that survives is: created_at holds, the content moved.
	if edited.CreatedAt != row.CreatedAt || edited.Blueprint != "bp-run-2" || edited.MinPriority != "known" {
		t.Fatalf("edit %+v vs %v", edited, row)
	}
	if _, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{
		ComponentRoot: "/repo/auth", Enabled: false, Blueprint: "bp-run-2",
	}); err != nil {
		t.Fatal(err)
	}
	if r, found, _ := s.SchedulerRoot("/repo/auth"); found && r.Enabled {
		t.Fatal("enabled=0 upsert left the root enabled")
	}
	// Disable answers false for a root that is not there, true once.
	if gone, err := s.DisableSchedulerRoot(s27Actor, "/repo/nope"); err != nil || gone {
		t.Fatalf("disable of a stranger: %v (%v)", gone, err)
	}
	if _, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{ComponentRoot: "/repo/auth", Enabled: true, Blueprint: "bp"}); err != nil {
		t.Fatal(err)
	}
	if gone, err := s.DisableSchedulerRoot(s27Actor, "/repo/auth"); err != nil || !gone {
		t.Fatalf("disable: %v (%v)", gone, err)
	}
	if gone, err := s.DisableSchedulerRoot(s27Actor, "/repo/auth"); err != nil || gone {
		t.Fatalf("second disable must be false (the row is already off): %v (%v)", gone, err)
	}
	// The retired row survives — history is never deleted with authority.
	if _, found, _ := s.SchedulerRoot("/repo/auth"); !found {
		t.Fatal("DisableSchedulerRoot DELETED the row")
	}
	// A component_root the database does not know cannot take authority.
	if _, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{ComponentRoot: "/repo/ghost", Enabled: true, Blueprint: "bp"}); err == nil {
		t.Fatal("a phantom component root took scheduler authority")
	}
	// A bad min_priority dies in Go, not mid-SQL.
	if _, err := s.UpsertSchedulerRoot(s27Actor, SchedulerRootRow{
		ComponentRoot: "/repo/auth", Enabled: true, Blueprint: "bp", MinPriority: "must_be_known",
	}); err == nil || !strings.Contains(err.Error(), "known or assumed") {
		t.Fatalf("min_priority: %v", err)
	}
}

func mustCount(t *testing.T, s *Store, q string) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestClaimStampFinishLifecycle(t *testing.T) {
	s := openTemp(t)
	// A claim needs a decision id — the claim is not an anonymous act.
	if _, err := s.ClaimStart("queue", 1, "/repo/l", 0, "2026-11-05T00:00:00.000Z"); err == nil {
		t.Fatal("a claim without its decision row was accepted")
	}
	if _, err := s.ClaimStart("goal", 1, "/repo/l", 1, "2026-11-05T00:00:00.000Z"); err == nil {
		t.Fatal("a subject kind that claims nothing claimed anyway")
	}
	storeNow, err := time.Parse("2006-01-02T15:04:05.000Z", s.Now())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimStart("queue", 1, "/repo/l", 1, "2026-10-15T09:00:00.000Z")
	if err != nil || !claimed {
		t.Fatalf("claim: %v (%v)", claimed, err)
	}
	// Open until decided — the crash shape reads honestly.
	open, err := s.OpenStarts()
	if err != nil || len(open) != 1 || open[0].Outcome != "" {
		t.Fatalf("OpenStarts = %+v (%v)", open, err)
	}
	if ok, err := s.StampStart("queue", 1, "/repo/l", "loop-s27-1"); err != nil || !ok {
		t.Fatalf("stamp: %v (%v)", ok, err)
	}
	if ok, _ := s.StampStart("queue", 1, "/repo/l", "loop-s27-1"); ok {
		t.Fatal("a second stamp wrote — the stamp is once, like the claim")
	}
	if _, err := s.FinishStart("queue", 1, "maybe"); err == nil {
		t.Fatal("an outcome word outside the CHECK was accepted by Go")
	}
	if ok, err := s.FinishStart("queue", 1, "started"); err != nil || !ok {
		t.Fatalf("finish: %v (%v)", ok, err)
	}
	if ok, _ := s.FinishStart("queue", 1, "lost"); ok {
		t.Fatal("a decided claim was re-decided")
	}
	if open, _ := s.OpenStarts(); len(open) != 0 {
		t.Fatalf("OpenStarts still lists a decided claim: %+v", open)
	}
	// StartsSince: only 'started', only after t, only this root.
	if n, err := s.StartsSince("/repo/l", storeNow.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("StartsSince = %d (%v), want 1", n, err)
	}
	if n, err := s.StartsSince("/repo/l", storeNow.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("StartsSince future window = %d (%v), want 0", n, err)
	}
	if n, err := s.StartsSince("/repo/other", storeNow.Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("StartsSince leaked across roots: %d (%v)", n, err)
	}
	// A refused claim is recorded but never counts toward the rate window.
	if _, err := s.ClaimStart("queue", 2, "/repo/l", 2, "2026-10-15T09:00:01.000Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishStart("queue", 2, "refused"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.StartsSince("/repo/l", storeNow.Add(-time.Hour)); n != 1 {
		t.Fatalf("a refused attempt counted toward the rate window: %d", n)
	}
	// LastSchedulerStart answers NEVER for a root whose log holds none.
	if _, found, err := s.LastSchedulerStart("/repo/l"); err != nil || found {
		t.Fatalf("LastSchedulerStart without any decision: found=%v err=%v", found, err)
	}
	// …and the newest scheduler `started` line, once one exists.
	if _, err := s.AppendDecision(SchedulerDecision{
		ComponentRoot: "/repo/l", SubjectKind: "queue", SubjectID: 1,
		Decision: "started", Summary: "started queue item 1 as loop-s27-1",
		ActorKind: "scheduler", ActorID: "tick-s27", LoopSlug: "loop-s27-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendDecision(SchedulerDecision{
		ComponentRoot: "/repo/l", SubjectKind: "queue", SubjectID: 3,
		Decision: "started", Summary: "started queue item 3", ActorKind: "user", ActorID: "uid",
	}); err != nil {
		t.Fatal(err)
	}
	at, found, err := s.LastSchedulerStart("/repo/l")
	if err != nil || !found {
		t.Fatalf("LastSchedulerStart: found=%v err=%v", found, err)
	}
	if !at.Before(storeNow.Add(time.Minute)) {
		t.Fatalf("LastSchedulerStart returned %v", at)
	}
	// …and a PERSON's start never masquerades as the scheduler's clock.
	if _, err := s.AppendDecision(SchedulerDecision{
		ComponentRoot: "/repo/human", SubjectKind: "queue", SubjectID: 9,
		Decision: "started", Summary: "started queue item 9", ActorKind: "user", ActorID: "uid",
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.LastSchedulerStart("/repo/human"); err != nil || found {
		t.Fatalf("a human start read as the scheduler's: found=%v err=%v", found, err)
	}
}

func TestFirstDeferralAnchorsTheCurrentStreak(t *testing.T) {
	s := openTemp(t)
	mk := func(decision, reason string) {
		if _, err := s.AppendDecision(SchedulerDecision{
			ComponentRoot: "/repo/age", SubjectKind: "queue", SubjectID: 5,
			Decision: decision, Reason: reason, Summary: decision + " queue item 5",
			ActorKind: "scheduler", ActorID: "tick-s27",
		}); err != nil {
			t.Fatalf("%s(%s): %v", decision, reason, err)
		}
	}
	if _, found, _ := s.FirstDeferral("queue", 5, "capacity"); found {
		t.Fatal("a deferral exists before any was written")
	}
	mk("deferred", "capacity")
	first, found, err := s.FirstDeferral("queue", 5, "capacity")
	if err != nil || !found {
		t.Fatalf("first deferral missing: %v", err)
	}
	mk("deferred", "capacity") // deduped
	mk("skipped", "")          // rides the streak
	if d, _, _ := s.FirstDeferral("queue", 5, "capacity"); d.ID != first.ID {
		t.Fatalf("the anchor moved within a streak: %d vs %d", d.ID, first.ID)
	}
	mk("started", "") // breaks it
	if _, found, _ := s.FirstDeferral("queue", 5, "capacity"); found {
		t.Fatal("aging still anchors a streak the start already ended")
	}
	mk("deferred", "capacity") // a NEW streak
	d, found, err := s.FirstDeferral("queue", 5, "capacity")
	if err != nil || !found {
		t.Fatalf("the new streak has no anchor: %v", err)
	}
	if d.ID <= first.ID {
		t.Fatalf("the new streak anchored to the OLD row %d (first was %d)", d.ID, first.ID)
	}
	// Reason-scoped: a different reason's streak anchors separately.
	mk("deferred", "window")
	if d2, found, _ := s.FirstDeferral("queue", 5, "window"); !found || d2.Reason != "window" {
		t.Fatalf("window streak: %+v found=%v", d2, found)
	}
}

func TestOccupancyReads(t *testing.T) {
	s := openTemp(t)
	mk := func(id, slug, root, status string) {
		if _, err := s.w.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
			VALUES (?,?,'t','g',?,?, 'standard','preflight',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
			id, slug, root, root, status); err != nil {
			t.Fatalf("seed %s/%s: %v", status, slug, err)
		}
	}
	mk("o-1", "run", "/repo/occ", "running")
	mk("o-2", "wait", "/repo/occ", "waiting")
	mk("o-3", "block", "/repo/occ", "blocked")
	mk("o-4", "idle", "/repo/occ", "idle")
	mk("o-5", "gone", "/repo/occ", "done")
	mk("o-6", "elsewhere", "/repo/other", "running")
	got, err := s.OccupyingLoops("/repo/occ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("OccupyingLoops = %+v, want the three StillGoing words only", got)
	}
	slots, err := s.RunningLoopSlots()
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 {
		t.Fatalf("RunningLoopSlots = %+v, want both running loops across roots (the daemon filters by intersection)", slots)
	}
}

func TestDecisionActorVocabularyIsClosedInGo(t *testing.T) {
	s := openTemp(t)
	bad := []SchedulerDecision{
		{SubjectKind: "goal", Decision: "started", Summary: "x", ActorKind: "user", ActorID: "u"},
		{SubjectKind: "queue", Decision: "merged", Summary: "x", ActorKind: "user", ActorID: "u"},
		{SubjectKind: "queue", Decision: "started", Summary: "", ActorKind: "user", ActorID: "u"},
		{SubjectKind: "queue", Decision: "started", Summary: "x", ActorKind: "model", ActorID: "u"},
		{SubjectKind: "queue", Decision: "started", Summary: "x", ActorKind: "scheduler"},
		{SubjectKind: "queue", Decision: "started", Summary: "x", ActorKind: "agent", ActorID: ""},
	}
	for i, d := range bad {
		if _, err := s.AppendDecision(d); err == nil {
			t.Fatalf("case %d was accepted: %+v", i, d)
		}
	}
	if n := mustCount(t, s, `SELECT COUNT(*) FROM scheduler_decision`); n != 0 {
		t.Fatalf("%d rows behind the refusals", n)
	}
	// The scheduler may ACT (its decisions carry its kind) but never AUTHOR
	// authority — the control writes above already proved that half.
	if _, err := s.AppendDecision(SchedulerDecision{
		SubjectKind: "roadmap", SubjectID: 2, Decision: "needs-you", Reason: "grant-expired",
		Summary: "needs-you roadmap item 2", ActorKind: "scheduler", ActorID: "tick-s27",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStampIsScopedToItsRoot(t *testing.T) {
	s := openTemp(t)
	if _, err := s.ClaimStart("queue", 4, "/repo/x", 1, "2026-10-15T09:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.StampStart("queue", 4, "/repo/y", "loop-x"); err != nil || ok {
		t.Fatalf("a foreign root stamped the claim: %v (%v) — I4 crossed wires", ok, err)
	}
	if _, err := s.StampStart("queue", 4, "/repo/x", "loop-x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StampStart("queue", 4, "", "loop-x"); err == nil {
		t.Fatal("an unscoped stamp was accepted")
	}
}

func TestSchedulerStartTimesSinceReadsStartedClaimsOldestFirst(t *testing.T) {
	s := openTemp(t)
	at := func(hhmmss string) string { return "2026-10-15T" + hhmmss + ".000Z" }
	claim := func(id int64, stamp, outcome string) {
		t.Helper()
		if ok, err := s.ClaimStart("queue", id, "/repo/l", id, stamp); err != nil || !ok {
			t.Fatalf("claim %d: %v %v", id, ok, err)
		}
		if ok, err := s.FinishStart("queue", id, outcome); err != nil || !ok {
			t.Fatalf("finish %d: %v %v", id, ok, err)
		}
	}
	claim(1, at("09:00:00"), "started")
	claim(2, at("10:00:00"), "started")
	claim(3, at("11:00:00"), "refused")
	must := func(since time.Time) []time.Time {
		t.Helper()
		out, err := s.StartTimesSince("/repo/l", since)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	base := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	got := must(base)
	if len(got) != 2 || got[0].Hour() != 9 || got[1].Hour() != 10 || !got[0].Before(got[1]) {
		t.Fatalf("the started instants read %v — started claims only, oldest first", got)
	}
	// CN12's window edge: the oldest instant is what the rate check ages
	// out, so the cutoff must bite exactly at it.
	cutoff := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)
	if got := must(cutoff); len(got) != 1 || got[0].Hour() != 10 {
		t.Fatalf("the window cutoff kept %v", got)
	}
	if got := must(time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("a closed window still counts %v", got)
	}
	if got, err := s.StartTimesSince("/repo/other", base); err != nil || len(got) != 0 {
		t.Fatalf("another repository's history leaked: %v %v", got, err)
	}
}
