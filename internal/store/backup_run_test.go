package store

// backup_run_test.go — S62 T3's behavioural tests, one per spec row.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// bkAdvancingClock returns a store clock that advances one second per call,
// so every recorded run gets a distinct finished_at and the ordering tests
// are deterministic without sleeps.
func bkAdvancingStore(t *testing.T) *Store {
	t.Helper()
	base := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	var (
		mu sync.Mutex
		n  int
	)
	s, err := Open(Options{
		Dir: t.TempDir(),
		Clock: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			n++
			return base.Add(time.Duration(n) * time.Second)
		},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// bkRecord is RecordBackupRun with the spec's fields defaulted, fatal on any
// error the test did not ask for.
func bkRecord(t *testing.T, s *Store, kind, outcome, started string) BackupRun {
	t.Helper()
	run, err := s.RecordBackupRun(NewBackupRun{
		Kind:           kind,
		Reason:         "manual",
		ActorKind:      "cli",
		StartedAt:      started,
		Outcome:        outcome,
		FreeSpaceState: "ok",
	})
	if err != nil {
		t.Fatalf("record %s/%s: %v", kind, outcome, err)
	}
	return run
}

// Test 1 — the migration applies fresh and on an upgrade, and both arrive at
// the same shape.
func TestBackupRunMigrationFreshAndUpgraded(t *testing.T) {
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}
	bkAssertBackupRunCatalogue(t, fresh)

	// A store at the previous vintage: everything but the backup_run file.
	// S57 T7 (2026-11-02): by name, not "the last" — merge_watch landed after
	// backup_run in file order, and the positional shortcut dropped the wrong file.
	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	all := Migrations()
	applyMigrations(t, old.GormDB(), all[:backupRunMigrationIndex()])
	var absent int
	if err := old.GormDB().Raw(`SELECT count(*) FROM backup_run`).Scan(&absent).Error; err == nil {
		t.Fatal("the vintage store has backup_run before its migration — the vintage is wrong")
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	s, err := Open(Options{Dir: dir})
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
	bkAssertBackupRunCatalogue(t, s)
}

// bkAssertBackupRunCatalogue names the table, its index and both triggers in
// sqlite_master — the migration's whole content, counted.
func bkAssertBackupRunCatalogue(t *testing.T, s *Store) {
	t.Helper()
	g := s.GormDB()
	if n := countCatalogueEntries(t, g, "table", "backup_run"); n != 1 {
		t.Fatalf("backup_run table: %d entries, want 1", n)
	}
	if n := countCatalogueEntries(t, g, "index", "idx_backup_run_kind_outcome"); n != 1 {
		t.Fatalf("the (kind, outcome, finished_at) index: %d entries, want 1", n)
	}
	for _, trig := range []string{"backup_run_no_update", "backup_run_no_delete"} {
		if n := countCatalogueEntries(t, g, "trigger", trig); n != 1 {
			t.Fatalf("trigger %s: %d entries, want 1", trig, n)
		}
	}
}

// Test 2 — the triggers: history is unalterable, and the abort says why.
func TestBackupRunIsAppendOnly(t *testing.T) {
	s := bkAdvancingStore(t)
	run := bkRecord(t, s, "backup", "ok", "2026-11-02T01:00:00.000Z")

	_, err := s.w.Exec(`UPDATE backup_run SET detail = 'rewritten' WHERE id = ?`, run.ID)
	if err == nil {
		t.Fatal("an UPDATE on the ledger succeeded; history is alterable")
	}
	if !strings.Contains(err.Error(), "backup_run is append-only") {
		t.Fatalf("the UPDATE refusal does not name the rule: %v", err)
	}
	_, err = s.w.Exec(`DELETE FROM backup_run WHERE id = ?`, run.ID)
	if err == nil {
		t.Fatal("a DELETE on the ledger succeeded; history is alterable")
	}
	if !strings.Contains(err.Error(), "backup_run is append-only") {
		t.Fatalf("the DELETE refusal does not name the rule: %v", err)
	}
	if n := countTable(t, s.GormDB(), "backup_run"); n != 1 {
		t.Fatalf("the ledger holds %d rows, want the one that was appended", n)
	}
}

// Test 3 — the closed sets: the Go door refuses with a named reason, and the
// SQL CHECKs backstop a raw statement.
func TestBackupRunClosedSets(t *testing.T) {
	s := bkAdvancingStore(t)
	base := NewBackupRun{
		Kind: "backup", Reason: "manual", ActorKind: "cli",
		StartedAt: "2026-11-02T01:00:00.000Z", Outcome: "ok", FreeSpaceState: "ok",
	}
	junk := []struct {
		name     string
		mutate   func(*NewBackupRun)
		refused  string // the value the refusal must name
		admitted bool
	}{
		{name: "kind", mutate: func(in *NewBackupRun) { in.Kind = "vaporize" }, refused: "vaporize"},
		{name: "reason", mutate: func(in *NewBackupRun) { in.Reason = "because" }, refused: "because"},
		{name: "actor user", mutate: func(in *NewBackupRun) { in.ActorKind = "user" }, admitted: true},
		{name: "actor agent", mutate: func(in *NewBackupRun) { in.ActorKind = "agent" }, refused: "agent"},
		{name: "actor daemon", mutate: func(in *NewBackupRun) { in.ActorKind = "daemon" }, admitted: true},
		{name: "actor cli", mutate: func(in *NewBackupRun) { in.ActorKind = "cli" }, admitted: true},
		{name: "outcome", mutate: func(in *NewBackupRun) { in.Outcome = "exploded" }, refused: "exploded"},
		{name: "error_kind", mutate: func(in *NewBackupRun) { in.ErrorKind = "mystery" }, refused: "mystery"},
		{name: "verify_level", mutate: func(in *NewBackupRun) { in.VerifyLevel = "eyeballed" }, refused: "eyeballed"},
		{name: "free_space_state", mutate: func(in *NewBackupRun) { in.FreeSpaceState = "suspicious" }, refused: "suspicious"},
	}
	for _, tc := range junk {
		in := base
		tc.mutate(&in)
		_, err := s.RecordBackupRun(in)
		if tc.admitted {
			if err != nil {
				t.Fatalf("%s should be admitted, got %v", tc.name, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: a junk value was stored; the vocabulary leaked", tc.name)
		}
		var refusal BackupRunRefusal
		if !asRefusal(err, &refusal) {
			t.Fatalf("%s: the door returned a raw error, not a refusal: %v", tc.name, err)
		}
		if !strings.Contains(refusal.Detail, tc.refused) {
			t.Fatalf("%s: the refusal does not name the thing refused (%q): %s", tc.name, tc.refused, refusal)
		}
	}

	// "agent" specifically: agents have no write path here (BK10/P3).
	in := base
	in.ActorKind = "agent"
	if _, err := s.RecordBackupRun(in); err == nil {
		t.Fatal("an agent-acted run was stored; agents have no write path")
	}

	// The SQL backstop: a raw statement that skips the Go door is refused by
	// the CHECK, so a vocabulary cannot leak even from a bypass.
	for _, stmt := range []string{
		`INSERT INTO backup_run (kind, reason, actor_kind, started_at, finished_at, outcome, free_space_state)
		 VALUES ('vaporize', 'manual', 'cli', '2026-11-02T01:00:00.000Z', '2026-11-02T01:00:01.000Z', 'ok', 'ok')`,
		`INSERT INTO backup_run (kind, reason, actor_kind, started_at, finished_at, outcome, free_space_state)
		 VALUES ('backup', 'manual', 'agent', '2026-11-02T01:00:00.000Z', '2026-11-02T01:00:01.000Z', 'ok', 'ok')`,
	} {
		if _, err := s.w.Exec(stmt); err == nil {
			t.Fatalf("the CHECK backstop let through: %s", stmt)
		}
	}
}

// asRefusal reports whether err is a BackupRunRefusal, out it.
func asRefusal(err error, r *BackupRunRefusal) bool {
	if ref, ok := err.(BackupRunRefusal); ok {
		*r = ref
		return true
	}
	return false
}

// Test 4 — the GLOB and the package regex admit and refuse the same names.
func TestBackupRunFileNameGlobMatchesGoRegex(t *testing.T) {
	s := bkAdvancingStore(t)
	const glob = `'aeon-state-????????T??????Z-v*.aeonbak'`
	names := []struct {
		name string
		want bool
		note string
	}{
		{"aeon-state-20261102T120000Z-v74.aeonbak", true, "the canonical name"},
		{"aeon-state-20261102T120000Z-v74.aeonbak.partial", false, "a .partial temp is not a run file"},
		{"aeon-state-2026110T120000Z-v74.aeonbak", false, "seven date characters"},
		{"aeon-state-20261102T12000Z-v74.aeonbak", false, "five time characters"},
		{"aeon-state-20261102T120000Z-x74.aeonbak", false, "x instead of v"},
		{"aeon-state-20261102T120000Z-v.aeonbak", true, "empty run after v: the * is a zero-run"},
		{"state-20261102T120000Z-v74.aeonbak", false, "no aeon- prefix"},
		{"AEON-STATE-20261102T120000Z-v74.aeonbak", false, "case is part of the name"},
		{"aeon-state-20261102T120000Z-v74.aeonbak/", false, "a trailing slash is a path, not a name"},
		{"aeon-state-20261102T120000Z-v74.bak", false, "the wrong extension"},
		{"/home/op/aeon-state-20261102T120000Z-v74.aeonbak", false, "a full path is a detail, never a file_name"},
	}
	for _, tc := range names {
		var sqlSays int
		if err := s.r.QueryRow(`SELECT ? GLOB `+glob, tc.name).Scan(&sqlSays); err != nil {
			t.Fatalf("glob %s: %v", tc.name, err)
		}
		goSays := backupRunFileNamePattern.MatchString(tc.name)
		if (sqlSays == 1) != goSays {
			t.Fatalf("%s (%s): SQL says %d, Go says %v — the two doors drifted", tc.name, tc.note, sqlSays, goSays)
		}
		if (sqlSays == 1) != tc.want {
			t.Fatalf("%s (%s): both doors say %v, the table says %v", tc.name, tc.note, sqlSays == 1, tc.want)
		}
	}
	// The Go door on a bad file_name refuses, naming the column.
	in := NewBackupRun{
		Kind: "backup", Reason: "manual", ActorKind: "cli",
		StartedAt: "2026-11-02T01:00:00.000Z", Outcome: "ok", FreeSpaceState: "ok",
		FileName: "state.db",
	}
	if _, err := s.RecordBackupRun(in); err == nil {
		t.Fatal("a non-.aeonbak file_name was stored")
	}
}

// Test 5 — ordering, filters, and the last-success read.
func TestBackupRunRecordAndListNewestFirst(t *testing.T) {
	s := bkAdvancingStore(t)
	for i, kind := range []string{"backup", "verify", "backup", "prune", "backup"} {
		outcome := "ok"
		if i == 3 {
			outcome = "failed"
		}
		started := fmt.Sprintf("2026-11-02T01:0%d:00.000Z", i)
		if _, err := s.RecordBackupRun(NewBackupRun{
			Kind: kind, Reason: "manual", ActorKind: "daemon", StartedAt: started,
			Outcome: outcome, FreeSpaceState: "ok",
			ErrorKind: nullableError(outcome),
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	all, err := s.ListBackupRuns(BackupRunFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("the ledger holds %d rows, want 5", len(all))
	}
	// Newest first: the last recorded backup (i=4) leads.
	if all[0].Kind != "backup" || all[0].ID != 5 {
		t.Fatalf("the newest row is id %d kind %s, want id 5 kind backup", all[0].ID, all[0].Kind)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].ID < all[i].ID {
			t.Fatalf("the list is not newest first: id %d before %d", all[i-1].ID, all[i].ID)
		}
	}

	backups, err := s.ListBackupRuns(BackupRunFilter{Kind: "backup"})
	if err != nil {
		t.Fatalf("filter by kind: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("kind=backup holds %d rows, want 3", len(backups))
	}
	okOnly, err := s.ListBackupRuns(BackupRunFilter{Kind: "backup", Outcome: "ok"})
	if err != nil {
		t.Fatalf("filter by outcome: %v", err)
	}
	if len(okOnly) != 3 {
		t.Fatalf("backup/ok holds %d rows, want 3 (the failed one is the prune)", len(okOnly))
	}
	// The cap: a limit past 500 is the cap, not an error.
	capped, err := s.ListBackupRuns(BackupRunFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("capped list: %v", err)
	}
	if len(capped) != 5 {
		t.Fatalf("the cap applied to %d rows", len(capped))
	}

	// The same-instant tie is broken by id, not left to the engine's mood.
	tieA := bkRecord(t, s, "verify", "ok", "2026-11-02T01:10:00.000Z")
	tieB := bkRecord(t, s, "verify", "ok", "2026-11-02T01:10:01.000Z")
	if tieA.ID >= tieB.ID {
		t.Fatalf("the tie fixtures did not append in order: %d then %d", tieA.ID, tieB.ID)
	}
	two, err := s.ListBackupRuns(BackupRunFilter{Kind: "verify", Limit: 2})
	if err != nil {
		t.Fatalf("tie list: %v", err)
	}
	if two[0].ID != tieB.ID {
		t.Fatalf("the same-instant tie is not id-broken: newest is %d, want %d", two[0].ID, tieB.ID)
	}
}

func nullableError(outcome string) string {
	if outcome != "ok" {
		return "io"
	}
	return ""
}

// Test 6 — LastBackupRun: the "last success" read, three-state honest.
func TestBackupRunLastSuccess(t *testing.T) {
	s := bkAdvancingStore(t)
	// Nothing recorded: nil, not an error (never, not unknown — P31).
	last, err := s.LastBackupRun("backup", "ok")
	if err != nil {
		t.Fatalf("an empty ledger is an error: %v", err)
	}
	if last != nil {
		t.Fatal("an empty ledger answered a run")
	}

	bkRecord(t, s, "backup", "failed", "2026-11-02T01:00:00.000Z")
	ok1 := bkRecord(t, s, "backup", "ok", "2026-11-02T01:01:00.000Z")
	bkRecord(t, s, "backup", "failed", "2026-11-02T01:02:00.000Z")
	ok2 := bkRecord(t, s, "backup", "ok", "2026-11-02T01:03:00.000Z")

	last, err = s.LastBackupRun("backup", "ok")
	if err != nil {
		t.Fatalf("last ok: %v", err)
	}
	if last == nil || last.ID != ok2.ID {
		t.Fatalf("the last ok is %v, want the id %d", last, ok2.ID)
	}
	if ok1.ID == ok2.ID {
		t.Fatal("the fixtures collapsed; the test proves nothing")
	}

	// A junk pair is a refusal, not a 0-row surprise.
	if _, err := s.LastBackupRun("vaporize", "ok"); err == nil {
		t.Fatal("a junk kind answered")
	}
	if _, err := s.LastBackupRun("backup", "exploded"); err == nil {
		t.Fatal("a junk outcome answered")
	}
}

// Test 7 — the detail tripwire: raw error text with a path is refused.
func TestBackupRunDetailTripwire(t *testing.T) {
	s := bkAdvancingStore(t)
	base := NewBackupRun{
		Kind: "backup", Reason: "manual", ActorKind: "cli",
		StartedAt: "2026-11-02T01:00:00.000Z", Outcome: "failed",
		ErrorKind: "disk-full", FreeSpaceState: "low",
	}
	refused := []struct {
		name   string
		detail string
	}{
		{"a unix path", "failed: open /home/op/aeon/state.db: no space left on device"},
		{"a windows path", `failed: open C:\aeon\state.db: disk full`},
		{"just a slash", "failed: copy dir1/dir2"},
		{"over the cap", strings.Repeat("x", backupRunDetailMax+1)},
	}
	for _, tc := range refused {
		in := base
		in.Detail = tc.detail
		if _, err := s.RecordBackupRun(in); err == nil {
			t.Fatalf("%s: the detail was stored; the tripwire is not armed", tc.name)
		} else if !strings.Contains(err.Error(), "refused:") {
			t.Fatalf("%s: a raw error escaped the door: %v", tc.name, err)
		}
	}
	// The fixed template passes: bounded, separator-free, numbers and enums.
	in := base
	in.Detail = fmt.Sprintf("snapshot %d MiB in %d ms", 76, 271)
	if _, err := s.RecordBackupRun(in); err != nil {
		t.Fatalf("the fixed template was refused: %v", err)
	}
}

// Test 8 — the count pins, bumped by each slice that adds a migration.
func TestMigrationCountAssertionsBumpedOnce(t *testing.T) {
	// S62 T3 (2026-11-02): the backup_run file was the 29th timestamped
	// migration, the total 74 (45 frozen + 29 files). A later slice that adds
	// a migration edits these two numbers with its own dated line — the edit
	// is the count, and a silent drift of the literal is what this test is for.
	// S57 T3 (2026-11-02): the verify_records file is the 30th timestamped
	// migration, the total 75 (45 frozen + 30 files).
	// S57 T5 (2026-11-02): the merge_rules file is the 31st timestamped
	// migration, the total 76 (45 frozen + 31 files).
	// S57 T7 (2026-11-02): the merge_watch file is the 32nd timestamped
	// migration, the total 77 (45 frozen + 32 files).
	// S57 T8 (2026-11-02): the proposal_verify_merge_kinds file is the 33rd
	// timestamped migration, the total 78 (45 frozen + 33 files).
	// S48 T1 (2026-11-02): the blueprint file lands after it in file order
	// (the 34th), the total 79 (45 frozen + 34 files) — the two slices'
	// migrations merge, so both dated lines stand and the assertions carry
	// the merged count.
	// S59 T2 (2026-11-03): the execution_environment file is the 36th
	// timestamped migration, the total 81 (45 frozen + 36 files).
	// S58 T1 (2026-11-03): the supervision file is the 37th timestamped
	// migration, the total 82 (45 frozen + 37 files).
	// S49 T1 (2026-11-03, merged the same day): the automation_management
	// file is the 38th, the total 83 — both landed on 2026-11-03 from
	// separate worktrees, the timestamp ordering (…102500 supervision,
	// …102501 automation_management) is what makes them composable.
	// S59 T7 (2026-11-03): the proposal_environment_restrict file is the
	// 39th timestamped migration, the total 84 (45 frozen + 39 files).
	// S49 T6 (2026-11-03): the proposal_automation_disable file is the 40th,
	// the total 85; S59 T10 (2026-11-03, merged the same hour): the
	// provision_owner_network file is the 41st, the total 86 (45 frozen +
	// 41 files) — a CHECK widened by rebuilding the table, which is the
	// only way SQLite allows. The timestamp order (…193000 mine, …203000
	// S59's) is again what makes two same-day files composable.
	// S50 T1 (2026-11-04): roadmap_change is the 42nd, the total 87; S60 T2
	// (2026-11-04): the merge_queue and loop_worktree_integration files are
	// the 43rd and 44th, the total 89 — (S60 MQ2, 2026-10-30): the queue
	// tables and the ledger CHECK widened by rebuilding, which is again the
	// only way SQLite allows; S50 T5 (2026-11-04): the
	// proposal_roadmap_idea file is the 45th, the total 90 (45 frozen + 45
	// files) — the same proposal-table rebuild S49 T6 ran, restating all
	// fifteen kinds. The timestamp order (…012856 roadmap_change, …044821
	// and …044822 S60's, …101500 S50's) is what makes files from three
	// same-day worktrees composable. S52 T1 (2026-11-04): the
	// learning_management file is the 47th, the total 92 (45 frozen + 47
	// files) — three ALTERs plus the append-only learning_change audit, no
	// rebuild, its …132302 timestamp composes beside S51's …115500 the same
	// way. S54 T1 (2026-11-04): the platform_change file is the 48th, the
	// total 93 (45 frozen + 48 files) — one CREATE TABLE, its append-only
	// pair and an index; the …135327 timestamp composes beside S52's
	// …132302 from the same day the same way. S52 T4 (2026-11-04, paid
	// in-merge the same day): the proposal_learning_retract file is the
	// 49th, the total 94 (45 frozen + 49 files) — the sixteenth kind on the
	// same proposal rebuild pattern, its …171000 timestamp sorting after
	// every kind and audit file of the same day. S60 T9 (2026-11-04, paid
	// IN THE MERGE): the proposal_kind_mergequeue file is the 50th, the
	// total 95 (45 frozen + 50 files) — the seventeenth kind on the same
	// proposal rebuild pattern, its …193000 timestamp sorting after
	// platform_change (…135327) and proposal_learning_retract (…171000) of
	// the same day; both sides paid 49/94 separately on their own branch,
	// the merged truth is one payment. S63 T1 (2026-11-04): retro_facts is
	// the 51st file, the total 96 (45 frozen + 51 files) — its …215735
	// timestamp sorts after every 20261103/20261104 file already landed.
	// S61 T1 (2026-11-05): goal_intake is the 52nd file, the total 97 (45
	// frozen + 52 files) — its …024130 timestamp opens 20261105; the file
	// rebuilds goal and roadmap_item each one CHECK-word wider (F-2, P-C)
	// and adds the six intake-plane tables.
	// S27 T2 (2026-11-05): the scheduler file is the 53rd and the
	// loop_run_scheduler file the 54th, the totals 98 and 99 — one file
	// lays the scheduler's four tables, the dependency graph and the two
	// autonomy_cap ALTERs, the second rebuilds loop_run to widen
	// started_by_kind for 'scheduler' (0039's pattern: copy, count guard,
	// drop, rename, index by name). Their …025313/…025314 timestamps are one
	// second apart, which is what makes the pair an ordered unit rather than
	// two files racing over which comes first.
	// S61 T3 (2026-11-05): the template_revision_system_actor file is the
	// 55th, the total 100 (45 frozen + 55 files) —
	// scaffold_template_revision's actor CHECK widened to admit 'system' so
	// the seed lays shipped templates honestly (GI14), its …093000 timestamp
	// sorting after S27's …025314 of the same day; a copy-guard rebuild,
	// rows untouched, triggers re-created. The two sides each counted to 98
	// in their own tree; this merged truth is the walk that counts all four
	// files, paid IN THE MERGE.
	// S61 T5 (2026-11-05): the proposal_kinds_intake file is the 56th, the
	// total 101 (45 frozen + 56 files) — the proposal table rebuilt again to
	// admit the eighteenth and nineteenth kinds (charter_save, intake_plan),
	// restating all nineteen, the same copy-guard + foreign-key-guard pattern;
	// its …110000 timestamp sorts after every 20261105 file landed so far.
	// S73 T3 (2026-11-06): the agent_change file is the 57th, the total 102
	// (45 frozen + 57 files) — one NEW table and no rebuild: the agent plane's
	// append-only ledger (AR1), carrying the two RAISE(ABORT) triggers and the
	// slug-must-name-a-row CHECK. Its …181435 timestamp opens 20261106.
	// S75 T1 (2026-11-07): the loop_todo file is the 58th, the total 103
	// (45 frozen + 58 files) — two NEW tables (loop_todo_list, loop_todo) and no
	// rebuild: the loop's persisted todo list, with the partial unique index that
	// makes "one active list per loop" the database's rule.
	// 2026-11-08 (documents): the document_owner file is the 59th, the total
	// 104 (45 frozen + 59 files) — one ALTER (loop_document.owner_goal_id) and its
	// partial index: a goal owns the documents its work produced.
	// 2026-11-08 (variables): the env_var file is the 60th, the total 105 (45 frozen +
	// 60 files) — two NEW tables (env_var, env_var_change, append-only by trigger) and
	// no rebuild: the variable manager's ciphertext rows and its ledger.
	// 2026-11-08 (MCP env binding): the mcp_env_from file is the 61st, the total 106
	// (45 frozen + 61 files) — one ALTER (mcp_server.env_from): each env name is bound
	// to a variable of the manager.
	// 2026-11-10 (blueprint override): the blueprint_override file is the 64th, the total 109
	// (45 frozen + 64 files) — one NEW table: what a person overrides on a locked blueprint.
	// 2026-11-09 (MCP title): the mcp_title file is the 63rd, the total 108 (45 frozen +
	// 63 files).
	// 2026-11-08 (MCP share): the mcp_share file is the 62nd, the total 107 (45 frozen +
	// 62 files) — one NEW table: the public servers offered to every agent.
	// 2026-11-15 (loop_access): the loop_access file is the 65th, the total 110 (45 frozen + 65 files) — one NEW
	// table: a loop's permission mode (what a person pre-approved; no row means ask).
	// 2026-11-16 (trigger_event_filter): the 73rd file, the total 118. 2026-11-16 (route_audio_api): the 72nd file, the total 117. 2026-11-16 (work_closure): the 71st file, the total 116. 2026-11-16 (step_context): the 70th file, the total 115. 2026-11-16 (loop_archive_delete): the 69th file, the total 114. 2026-11-16 (loop_model_option): the 68th file, the total 113. 2026-11-16 (step_summary): the step_summary file is the 67th, the total 112 (a table and its backfill). 2026-11-15 (event_journal): the event_journal file is the 66th, the total 111 (45 frozen + 66 files) — two ALTERs
	// (event.loop_id, event.step_id), a backfill and an index: one loop's journal is one indexed read.
	if n := frozenFileCount(); n != 73 {
		t.Fatalf("frozenFileCount() = %d, want 73 (28 pre-S62 + backup_run + verify_records + merge_rules + merge_watch + proposal_verify_merge_kinds + blueprint + proposal_blueprint_save + execution_environment + supervision + automation_management + proposal_environment_restrict + proposal_automation_disable + provision_owner_network + roadmap_change + merge_queue + loop_worktree_integration + proposal_roadmap_idea + bug_change + learning_management + platform_change + proposal_learning_retract + proposal_kind_mergequeue + retro_facts + goal_intake + scheduler + loop_run_scheduler + template_revision_system_actor + proposal_kinds_intake + agent_change + loop_todo + document_owner + env_var + mcp_env_from + mcp_share + mcp_title + blueprint_override + loop_access + event_journal + step_summary + loop_model_option + loop_archive_delete + step_context + work_closure + route_audio_api + trigger_event_filter)", n)
	}
	if n := frozenMigrationCount(); n != 118 {
		t.Fatalf("frozenMigrationCount() = %d, want 118 (45 frozen + 73 files)", n)
	}
}

// Test 9 — I2 for the ledger: one writer, named.
func TestNoOtherWriterOfBackupRun(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	var writers []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".tmp", "node_modules", "ui":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(blob), "INSERT INTO backup_run") {
			rel, _ := filepath.Rel(root, path)
			writers = append(writers, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(writers) != 1 || writers[0] != filepath.Join("internal", "store", "backup_run.go") {
		t.Fatalf("the ledger has writers %v, want exactly internal/store/backup_run.go — a second writer is a second history", writers)
	}
}
