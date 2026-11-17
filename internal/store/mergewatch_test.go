// mergewatch_test.go — S57 T7's ledger: the migration (fresh and
// upgraded identity, the catalogue, the FK guard, the closed state and
// kind vocabularies) and the cold API the poll and the sweep ride
// (idempotent enqueue, the born-expired window, the due read's join,
// the terminal update, the expiry's detail, the filing's claim cycle).

package store

import (
	"strings"
	"testing"
	"time"
)

// m3AssertCatalogue names the migration's whole content in
// sqlite_master: the two tables, the due index and the FK guard.
func m3AssertCatalogue(t *testing.T, s *Store) {
	t.Helper()
	g := s.GormDB()
	if n := countCatalogueEntries(t, g, "table", "merge_watch"); n != 1 {
		t.Fatalf("merge_watch table: %d entries, want 1", n)
	}
	if n := countCatalogueEntries(t, g, "table", "gate_filing"); n != 1 {
		t.Fatalf("gate_filing table: %d entries, want 1", n)
	}
	if n := countCatalogueEntries(t, g, "index", "idx_merge_watch_due"); n != 1 {
		t.Fatalf("the (state, next_check_at) index: %d entries, want 1", n)
	}
	if n := countCatalogueEntries(t, g, "table", "zz_s57_merge_watch_fk_guard"); n != 1 {
		t.Fatalf("the FK guard: %d entries, want 1", n)
	}
}

// TestMigrationM3 — the 32nd timestamped file (total 77, the count the
// sibling test holds): a fresh database and a database upgraded from the
// previous vintage come out with the SAME shape, the catalogue is whole,
// the FK guard stands, and the closed vocabularies are the store's
// constants (a value the CHECK refuses is a state the poll cannot write).
func TestMigrationM3(t *testing.T) {
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}
	m3AssertCatalogue(t, fresh)

	// The FK guard: a dangling reference is a migration failure, not a
	// row. The connection's FK pragma is on (the DSN's), so the insert
	// itself is refused.
	var fk int
	if err := fresh.GormDB().Raw(`PRAGMA foreign_keys`).Scan(&fk).Error; err != nil {
		t.Fatalf("read the FK pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1 — the guard is only meaningful on an enforcing connection", fk)
	}
	if err := fresh.GormDB().Exec(`INSERT INTO merge_watch (merge_event_id, component_id, base_ref, sha, next_check_at, created_at)
		VALUES ('no-such-event', 'c', 'main', 's', 'x', 'x')`).Error; err == nil {
		t.Fatal("a watch for a merge event that does not exist was accepted — the FK is not enforced")
	}

	// The closed vocabularies: the store's constants are the CHECK's. The
	// watch rows hang off real merge events (the FK is enforced), so the
	// CHECK — not the FK — is what may refuse.
	for _, id := range []string{"ev-pending", "ev-green", "ev-red", "ev-unknown", "ev-expired", "ev-bogus"} {
		if _, err := fresh.RecordMergeEvent(MergeEventRecord{
			ID: id, Scope: Scope{LoopSlug: "dev", ComponentRoot: "/repo/m3"}, PR: 41,
			MergeSHA: "sha-" + id, Evidence: "the vocabulary's own row",
		}); err != nil {
			t.Fatalf("seed the event %s: %v", id, err)
		}
	}
	for _, state := range []string{WatchPending, WatchGreen, WatchRed, WatchUnknown, WatchExpired} {
		if err := fresh.GormDB().Exec(`INSERT INTO merge_watch (merge_event_id, component_id, base_ref, sha, state, next_check_at, created_at)
			VALUES (?,?,?,?,?, 'x','x')`, "ev-"+state, "c", "main", "s", state).Error; err != nil {
			t.Fatalf("the CHECK refuses state %q: %v — the constant and the vocabulary have drifted", state, err)
		}
	}
	if err := fresh.GormDB().Exec(`INSERT INTO merge_watch (merge_event_id, component_id, base_ref, sha, state, next_check_at, created_at)
		VALUES ('ev-bogus', 'c', 'main', 's', 'bogus', 'x', 'x')`).Error; err == nil {
		t.Fatal("the CHECK accepted state 'bogus' — the vocabulary is not closed")
	}
	for _, kind := range []string{GateFilingVerify, GateFilingMergeWatch, GateFilingMergeRefused} {
		if err := fresh.GormDB().Exec(`INSERT INTO gate_filing (subject, kind, first_seen_at) VALUES (?, ?, 'x')`, "sub-"+kind, kind).Error; err != nil {
			t.Fatalf("the CHECK refuses kind %q: %v — the constant and the vocabulary have drifted", kind, err)
		}
	}
	if err := fresh.GormDB().Exec(`INSERT INTO gate_filing (subject, kind, first_seen_at) VALUES ('sub-bogus', 'bogus', 'x')`).Error; err == nil {
		t.Fatal("the CHECK accepted kind 'bogus' — the vocabulary is not closed")
	}

	// The upgrade: a store at the previous vintage (everything but the
	// merge_watch file) migrates to the fresh shape.
	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	all := Migrations()
	// By name, not "the last" (S57 T8 + S48 T1, 2026-11-02): the proposal
	// kinds file and S48's blueprint file both landed after merge_watch in
	// file order, so the positional shortcut `all[:len(all)-1]` dropped the
	// wrong file — the same identity-not-position fix S62 T3 applied for
	// backup_run (the S47 tool_test precedent). A later migration cannot
	// break it.
	applyMigrations(t, old.GormDB(), all[:mergeWatchMigrationIndex()])
	var absent int
	if err := old.GormDB().Raw(`SELECT count(*) FROM merge_watch`).Scan(&absent).Error; err == nil {
		t.Fatal("the vintage store has merge_watch before its migration — the vintage is wrong")
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}
	s, err := Open(Options{Dir: dir, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}
	m3AssertCatalogue(t, s)
}

// --- the watch API -------------------------------------------------------

// m3MergeEvent seeds one merge event the watches can hang off, merged at
// the fixed clock (now).
func m3MergeEvent(t *testing.T, s *Store, id string) {
	t.Helper()
	m3MergeEventAt(t, s, id, time.Time{})
}

// m3MergeEventAt seeds one whose merged_at is named — the expiry's join
// reads the event's merged_at, not the watch row.
func m3MergeEventAt(t *testing.T, s *Store, id string, mergedAt time.Time) {
	t.Helper()
	if _, err := s.RecordMergeEvent(MergeEventRecord{
		ID:          id,
		Scope:       Scope{LoopSlug: "dev", ComponentRoot: "/repo/m3"},
		ComponentID: "c1",
		PR:          41,
		MergeSHA:    "sha-" + id,
		Evidence:    "all 2 reported checks are success",
		MergedAt:    mergedAt,
	}); err != nil {
		t.Fatalf("seed merge event %s: %v", id, err)
	}
}

// TestMergeWatchEnqueueIsIdempotent — the re-entry's twin: a second
// enqueue for the same merge changes nothing and returns the standing row.
func TestMergeWatchEnqueueIsIdempotent(t *testing.T) {
	s := openTemp(t)
	m3MergeEvent(t, s, "ev-1")
	in := MergeWatchEnqueue{
		MergeEventID: "ev-1",
		ComponentID:  "c1",
		BaseRef:      "main",
		SHA:          "sha-ev-1",
		MergedAt:     fixedClock()().Add(-time.Hour),
		Window:       2 * time.Hour,
	}
	first, err := s.EnqueueMergeWatch(in)
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if first.State != WatchPending || first.Detail != "" {
		t.Errorf("first row: state=%q detail=%q, want pending / no detail", first.State, first.Detail)
	}
	second, err := s.EnqueueMergeWatch(in)
	if err != nil {
		t.Fatalf("second enqueue: %v — idempotency is the re-entry's contract", err)
	}
	if second.MergeEventID != first.MergeEventID || second.State != first.State || second.NextCheckAt != first.NextCheckAt {
		t.Errorf("second enqueue returned %+v, want the standing row %+v unchanged", second, first)
	}

	// The validation refusals: a watch names a merge, a component, a base
	// ref, a sha and a positive window — absence is not a watch.
	for name, bad := range map[string]MergeWatchEnqueue{
		"no event":     {ComponentID: "c1", BaseRef: "main", SHA: "s", Window: time.Hour},
		"no component": {MergeEventID: "ev-1", BaseRef: "main", SHA: "s", Window: time.Hour},
		"no base ref":  {MergeEventID: "ev-1", ComponentID: "c1", SHA: "s", Window: time.Hour},
		"no sha":       {MergeEventID: "ev-1", ComponentID: "c1", BaseRef: "main", Window: time.Hour},
		"no window":    {MergeEventID: "ev-1", ComponentID: "c1", BaseRef: "main", SHA: "s"},
	} {
		if _, err := s.EnqueueMergeWatch(bad); err == nil {
			t.Errorf("%s: the enqueue was accepted — the refusal names the absence", name)
		}
	}
}

// TestMergeWatchBornExpiredWhenWindowElapsed — the daemon was down across
// the window: the row is born expired, never pending ("we will check"
// would be a lie the poll then has to walk back).
func TestMergeWatchBornExpiredWhenWindowElapsed(t *testing.T) {
	s := openTemp(t)
	m3MergeEvent(t, s, "ev-2")
	now := fixedClock()()
	in := MergeWatchEnqueue{
		MergeEventID: "ev-2",
		ComponentID:  "c1",
		BaseRef:      "main",
		SHA:          "sha-ev-2",
		MergedAt:     now.Add(-3 * time.Hour), // the 2-h window elapsed
		Window:       2 * time.Hour,
	}
	w, err := s.EnqueueMergeWatch(in)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if w.State != WatchExpired || w.Detail != "window elapsed before first check" {
		t.Errorf("state=%q detail=%q, want expired / the honest reason", w.State, w.Detail)
	}
	if due, _ := s.DueMergeWatches(now, 10); len(due) != 0 {
		t.Errorf("due rows after a born-expired enqueue: %d, want 0 — a dead row is never due", len(due))
	}

	// A merge inside its window is pending and due now.
	in2 := in
	in2.MergeEventID = "ev-3"
	in2.SHA = "sha-ev-3"
	in2.MergedAt = now.Add(-10 * time.Minute)
	m3MergeEvent(t, s, "ev-3")
	w2, err := s.EnqueueMergeWatch(in2)
	if err != nil {
		t.Fatalf("enqueue the in-window merge: %v", err)
	}
	if w2.State != WatchPending {
		t.Errorf("state=%q, want pending", w2.State)
	}
	if due, _ := s.DueMergeWatches(now, 10); len(due) != 1 || due[0].MergeEventID != "ev-3" {
		t.Errorf("due rows = %+v, want the one pending merge", due)
	}
}

// TestDueMergeWatchesAreOldestFirstAndCarryTheEvent — the poll's read:
// pending/unknown at or before now, oldest first, the join carrying the
// merge event's ids (the root the poll forges through, the loop, the PR,
// the rule revision, the measured run) — one read, no second trip.
func TestDueMergeWatchesAreOldestFirstAndCarryTheEvent(t *testing.T) {
	s := openTemp(t)
	m3MergeEvent(t, s, "ev-a")
	m3MergeEvent(t, s, "ev-b")
	now := fixedClock()()
	for id, when := range map[string]time.Time{"ev-a": now.Add(-2 * time.Minute), "ev-b": now.Add(-1 * time.Minute)} {
		if _, err := s.EnqueueMergeWatch(MergeWatchEnqueue{
			MergeEventID: id, ComponentID: "c1", BaseRef: "main", SHA: "sha-" + id, MergedAt: when, Window: 2 * time.Hour,
		}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	// A terminal row: never due.
	if err := s.UpdateMergeWatch("ev-a", WatchGreen, "all 1 reported checks are success", now.Add(365*24*time.Hour), 1); err != nil {
		t.Fatalf("make ev-a terminal: %v", err)
	}
	due, err := s.DueMergeWatches(now, 10)
	if err != nil {
		t.Fatalf("due read: %v", err)
	}
	if len(due) != 1 || due[0].MergeEventID != "ev-b" {
		t.Fatalf("due = %+v, want the one non-terminal merge (ev-a is green)", due)
	}
	r := due[0]
	if r.ComponentRoot != "/repo/m3" || r.LoopSlug != "dev" || r.PR != 41 {
		t.Errorf("join: root=%q loop=%q pr=%d, want the merge event's own ids", r.ComponentRoot, r.LoopSlug, r.PR)
	}
}

// TestMergeWatchUpdateIsTheOnlyWayOut — the poll's answer: the state,
// the one-line detail, the next schedule, the attempt count and the poll
// stamp. A state outside the five and a detail past the one-liner's cap
// are refused (the detail is a word, not a report).
func TestMergeWatchUpdateIsTheOnlyWayOut(t *testing.T) {
	s := openTemp(t)
	m3MergeEvent(t, s, "ev-4")
	now := fixedClock()()
	if _, err := s.EnqueueMergeWatch(MergeWatchEnqueue{
		MergeEventID: "ev-4", ComponentID: "c1", BaseRef: "main", SHA: "s", MergedAt: now.Add(-time.Minute), Window: time.Hour,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := s.UpdateMergeWatch("ev-4", WatchUnknown, "check ci/x is pending", now.Add(30*time.Second), 1); err != nil {
		t.Fatalf("update: %v", err)
	}
	w, err := s.watchByID("ev-4")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if w.State != WatchUnknown || w.Detail != "check ci/x is pending" || w.Attempts != 1 || w.LastCheckedAt == nil {
		t.Errorf("row = %+v, want the poll's answer stamped", w)
	}
	if err := s.UpdateMergeWatch("ev-4", "bogus", "", now, 1); err == nil {
		t.Error("state 'bogus' was accepted — the five-state set is not closed")
	}
	if err := s.UpdateMergeWatch("ev-4", WatchUnknown, strings.Repeat("x", 257), now, 1); err == nil {
		t.Error("a 257-char detail was accepted — the detail is a one-liner, not a report")
	}
	if err := s.UpdateMergeWatch("no-such-event", WatchGreen, "", now, 1); err == nil {
		t.Error("an update for a watch that does not exist was accepted")
	}
}

// TestExpireStaleMergeWatchesKeepsTheLastAnswer — the window is the
// watch's contract: the expired row keeps its last honest word (a
// non-empty detail is preserved), and a detail that was never set gets
// the plain "window elapsed".
func TestExpireStaleMergeWatchesKeepsTheLastAnswer(t *testing.T) {
	s := openTemp(t)
	now := fixedClock()()
	// ev-5: stale, with a last answer. ev-6: stale, never checked. ev-7:
	// fresh, inside its window. The window's start is the EVENT's
	// merged_at (the expiry's join reads it), so the seed names it.
	stale := now.Add(-3 * time.Hour)
	fresh := now.Add(-1 * time.Hour)
	m3MergeEventAt(t, s, "ev-5", stale)
	m3MergeEventAt(t, s, "ev-6", stale)
	m3MergeEventAt(t, s, "ev-7", fresh)
	// The watches themselves were enqueued in time (fresh at the store's
	// clock, so pending); what is stale is the EVENT's merged_at, and that
	// is what the expiry's join reads.
	for _, id := range []string{"ev-5", "ev-6", "ev-7"} {
		if _, err := s.EnqueueMergeWatch(MergeWatchEnqueue{
			MergeEventID: id, ComponentID: "c1", BaseRef: "main", SHA: "sha-" + id, MergedAt: now, Window: 2 * time.Hour,
		}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	if err := s.UpdateMergeWatch("ev-5", WatchUnknown, "check ci/x is pending", now, 1); err != nil {
		t.Fatalf("give ev-5 its last answer: %v", err)
	}
	n, err := s.ExpireStaleMergeWatches(now, 2*time.Hour)
	if err != nil {
		t.Fatalf("expiry: %v", err)
	}
	if n != 2 {
		t.Fatalf("expired = %d, want 2 (ev-5 and ev-6; ev-7 is inside its window)", n)
	}
	w5, _ := s.watchByID("ev-5")
	w6, _ := s.watchByID("ev-6")
	w7, _ := s.watchByID("ev-7")
	if w5.State != WatchExpired || w5.Detail != "check ci/x is pending" {
		t.Errorf("ev-5: state=%q detail=%q, want expired / the last answer KEPT", w5.State, w5.Detail)
	}
	if w6.State != WatchExpired || w6.Detail != "window elapsed" {
		t.Errorf("ev-6: state=%q detail=%q, want expired / the plain window line", w6.State, w6.Detail)
	}
	if w7.State != WatchPending {
		t.Errorf("ev-7: state=%q, want pending (its window had not elapsed)", w7.State)
	}
}

// TestMergeEventsWithoutWatchNamesTheCrashWindow — the sweep's read: a
// merge event with no watch row is the crash window; one with a watch is
// not.
func TestMergeEventsWithoutWatchNamesTheCrashWindow(t *testing.T) {
	s := openTemp(t)
	m3MergeEvent(t, s, "ev-8")
	m3MergeEvent(t, s, "ev-9")
	now := fixedClock()()
	if _, err := s.EnqueueMergeWatch(MergeWatchEnqueue{
		MergeEventID: "ev-8", ComponentID: "c1", BaseRef: "main", SHA: "s", MergedAt: now, Window: time.Hour,
	}); err != nil {
		t.Fatalf("watch ev-8: %v", err)
	}
	refs, err := s.MergeEventsWithoutWatch()
	if err != nil {
		t.Fatalf("read the crash window: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "ev-9" || refs[0].ComponentRoot != "/repo/m3" || refs[0].PR != 41 {
		t.Errorf("without-watch = %+v, want the one unwatched merge with its ids", refs)
	}
}

// --- the gate-filing ledger ------------------------------------------------

// TestGateFilingClaimBumpsAndFilesOnce — the VG16 cycle: a new subject
// claims (count 1, the bug is filed), a repeat bumps (count 2, NOTHING is
// filed — the bug already stands), and the claim's stamp names the first
// bug only.
func TestGateFilingClaimBumpsAndFilesOnce(t *testing.T) {
	s := openTemp(t)
	subject := "/repo/m3:verify"

	existing, count, err := s.ClaimGateFiling(subject, GateFilingVerify)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if existing || count != 1 {
		t.Fatalf("first claim: existing=%v count=%d, want false / 1 (a new subject files)", existing, count)
	}
	existing, count, err = s.ClaimGateFiling(subject, GateFilingVerify)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if !existing || count != 2 {
		t.Fatalf("second claim: existing=%v count=%d, want true / 2 (the bump, no re-filing)", existing, count)
	}
	existing, count, err = s.ClaimGateFiling(subject, GateFilingVerify)
	if err != nil {
		t.Fatalf("third claim: %v", err)
	}
	if !existing || count != 3 {
		t.Fatalf("third claim: existing=%v count=%d, want true / 3", existing, count)
	}

	// The stamp: the first bug stands, a second stamp is refused.
	if err := s.SetGateFilingBug(subject, 7); err != nil {
		t.Fatalf("stamp the bug: %v", err)
	}
	g, err := s.GateFilingBySubject(subject)
	if err != nil {
		t.Fatalf("read the claim: %v", err)
	}
	if g.Kind != GateFilingVerify || g.Count != 3 || g.BugID == nil || *g.BugID != 7 {
		t.Errorf("claim = %+v, want kind verify / count 3 / bug 7", g)
	}
	if err := s.SetGateFilingBug(subject, 8); err == nil {
		t.Error("a second stamp was accepted — the first bug is the one the ledger names")
	}

	// The refusals: the three-kind set and the subject are closed.
	if _, _, err := s.ClaimGateFiling("", GateFilingVerify); err == nil {
		t.Error("a subject-less claim was accepted")
	}
	if _, _, err := s.ClaimGateFiling("sub", "bogus"); err == nil {
		t.Error("kind 'bogus' was accepted — the three-kind set is not closed")
	}
	if _, err := s.GateFilingBySubject("never-claimed"); err == nil {
		t.Error("an absent subject read as present")
	}

	// The ledger read, oldest first.
	if _, _, err := s.ClaimGateFiling("/repo/m3:merge-refused:7", GateFilingMergeRefused); err != nil {
		t.Fatalf("second subject: %v", err)
	}
	all, err := s.ListGateFilings()
	if err != nil {
		t.Fatalf("list the ledger: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ledger = %d rows, want 2", len(all))
	}
}
