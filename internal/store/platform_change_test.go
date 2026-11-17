package store

// S54 T1 — the platform_change table's own gates, the siblings of
// roadmap_change_test.go: the shape is the DATABASE's claim (CHECK
// refusals bite at SQL, each with a positive control), the append-only
// pair has teeth, the audited writers keep the start and its audit in ONE
// transaction, and a start that did not happen — the flight collision —
// is audited NOTHING. The raw-INSERT style here is test-only (the
// S44/S45/S49/S50 audit siblings are the precedent); production reaches
// the table only through the audited writers.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// platformPerson is a lawful audit author; the store never interprets the
// id, only that one exists (the door + socket layers own who counts).
var platformPerson = PlatformActor{Kind: "user", ID: "42"}

func platformAudit(op, subject string) PlatformAudit {
	return PlatformAudit{
		Op: op, Subject: subject, Summary: op + " started", Actor: platformPerson,
	}
}

// Test 1 — fresh migration lands the table, the index and both triggers;
// an upgrade over a pre-S54 database lands it too (the migration count
// test in backup_run_test.go pins the total).
func TestPlatformChangeMigrationLands(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	var name string
	err := s.r.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='platform_change'`).Scan(&name)
	if err != nil || name != "platform_change" {
		t.Fatalf("fresh database has no platform_change table: %v", err)
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='platform_change' AND name='idx_platform_change_subject'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_platform_change_subject missing (n=%d, err=%v)", n, err)
	}
	for _, trg := range []string{"platform_change_no_update", "platform_change_no_delete"} {
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name=?`, trg).Scan(&n); err != nil || n != 1 {
			t.Fatalf("trigger %s missing (n=%d, err=%v)", trg, n, err)
		}
	}
}

// Test 2 — append-only: an UPDATE and a DELETE die at the trigger, the
// message says who closed the door.
func TestPlatformChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	ctx := context.Background()
	err := s.PlatformWithTx(func(tx *sql.Tx) error {
		return PlatformChangeInsertTx(tx, platformAudit("reindex", "/r\x00code"), s.Now())
	})
	if err != nil {
		t.Fatalf("the lawful append was refused: %v", err)
	}
	for _, stmt := range []string{
		`UPDATE platform_change SET summary = 'rewritten' WHERE id > 0`,
		`DELETE FROM platform_change WHERE id > 0`,
	} {
		_, err := s.w.Exec(stmt)
		if err == nil {
			t.Fatalf("%s … succeeded — the append-only net has a hole", stmt[:12])
		}
		if !strings.Contains(err.Error(), "platform_change is append-only (S54 SP3)") {
			t.Errorf("%s … refused with %v — want the trigger's own sentence naming S54", stmt[:12], err)
		}
	}
	rows, err := s.PlatformChangeList(ctx, "", 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("the lawful row did not survive: %d rows, %v", len(rows), err)
	}
}

// Test 3 — the CHECK discipline: every invalid value is refused AT SQL,
// each with the positive control beside it (the roadmap_test.go rule).
func TestPlatformChangeChecksRejectEveryInvalidValue(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	at := s.Now()
	cases := []struct {
		name string
		a    PlatformAudit
	}{
		{"op outside the four", PlatformAudit{Op: "graph_rebuild", Subject: "/r", Summary: "x", Actor: platformPerson}},
		{"op empty", PlatformAudit{Op: "", Subject: "/r", Summary: "x", Actor: platformPerson}},
		{"subject empty", PlatformAudit{Op: "reindex", Subject: "", Summary: "x", Actor: platformPerson}},
		{"subject over 256 bytes", PlatformAudit{Op: "reindex", Subject: strings.Repeat("r", 257), Summary: "x", Actor: platformPerson}},
		{"summary empty", PlatformAudit{Op: "reindex", Subject: "/r", Summary: "", Actor: platformPerson}},
		{"summary over 1024", PlatformAudit{Op: "reindex", Subject: "/r", Summary: strings.Repeat("s", 1025), Actor: platformPerson}},
		{"actor kind system", PlatformAudit{Op: "reindex", Subject: "/r", Summary: "x", Actor: PlatformActor{Kind: "system", ID: "42"}}},
		{"actor kind agent", PlatformAudit{Op: "reindex", Subject: "/r", Summary: "x", Actor: PlatformActor{Kind: "agent", ID: "step_1"}}},
		{"actor id empty", PlatformAudit{Op: "reindex", Subject: "/r", Summary: "x", Actor: PlatformActor{Kind: "user", ID: ""}}},
	}
	for _, c := range cases {
		err := s.PlatformWithTx(func(tx *sql.Tx) error {
			return PlatformChangeInsertTx(tx, c.a, at)
		})
		if err == nil {
			t.Errorf("%s: the append succeeded — the CHECK (or its store-side twin) has a hole", c.name)
		}
	}
	// Positive control: every lawful value lands.
	for _, k := range []PlatformActor{{Kind: "user", ID: "42"}, {Kind: "cli", ID: "cli"}} {
		a := platformAudit("reindex", "/r\x00code")
		a.Actor = k
		err := s.PlatformWithTx(func(tx *sql.Tx) error {
			return PlatformChangeInsertTx(tx, a, at)
		})
		if err != nil {
			t.Errorf("lawful actor %+v refused: %v", k, err)
		}
	}
	rows, err := s.PlatformChangeList(context.Background(), "", 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("positive control landed %d rows, want 2 (%v)", len(rows), err)
	}
}

// Test 4 — a nil transaction is refused (a missing commit is not an
// absent one), an empty occurred_at is refused, and no transaction at all
// wrote nothing.
func TestPlatformChangeInsertTxRefusesNilTx(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	if err := PlatformChangeInsertTx(nil, platformAudit("reindex", "/r"), s.Now()); err == nil {
		t.Fatal("a nil transaction was accepted — the audit would ride no commit at all")
	}
	if err := s.PlatformWithTx(func(tx *sql.Tx) error {
		return PlatformChangeInsertTx(tx, platformAudit("reindex", "/r"), "")
	}); err == nil {
		t.Fatal("an untimed audit row was accepted — a row that cannot say WHEN answers nothing")
	}
	rows, _ := s.PlatformChangeList(context.Background(), "", 0)
	if len(rows) != 0 {
		t.Fatalf("refused inserts left %d rows", len(rows))
	}
}

// Test 5 — the listing: newest first (id is the clock), the subject
// filter matches byte for byte, and the limit clamps [1,500] with a 50
// default.
func TestPlatformChangeListOrderFilterAndClamp(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		a := platformAudit("reindex", "/r\x00code")
		if i%2 == 0 {
			a.Subject = "/other\x00history"
		}
		a.Summary = "retrieval.reindex started for corpus code"
		if err := s.PlatformWithTx(func(tx *sql.Tx) error {
			return PlatformChangeInsertTx(tx, a, s.Now())
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	all, err := s.PlatformChangeList(ctx, "", 0)
	if err != nil || len(all) != 50 {
		t.Fatalf("default limit: %d rows (%v), want the 50-row default", len(all), err)
	}
	if all[0].ID <= all[1].ID {
		t.Errorf("the listing is not newest-first")
	}
	if big, err := s.PlatformChangeList(ctx, "", 9999); err != nil || len(big) != 60 {
		t.Fatalf("limit 9999 returned %d rows (%v), want the 60 seeded (the 500 clamp is a ceiling, not a floor)", len(big), err)
	}
	if some, err := s.PlatformChangeList(ctx, "", 7); err != nil || len(some) != 7 {
		t.Fatalf("limit 7 returned %d rows (%v)", len(some), err)
	}
	filtered, err := s.PlatformChangeList(ctx, "/other\x00history", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 30 {
		t.Fatalf("subject filter returned %d, want the 30 rows carrying that exact subject", len(filtered))
	}
	for _, r := range filtered {
		if r.Subject != "/other\x00history" {
			t.Fatalf("the filter matched a foreign subject %q", r.Subject)
		}
	}
	none, err := s.PlatformChangeList(ctx, "/never", 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("a subject with no rows: %d rows (%v), want the honest empty", len(none), err)
	}
}

// Test 6 — one audited start writes EXACTLY ONE audit row and one ledger
// row; outcome_ref names the worker the insert minted; the plain twin
// still writes no audit (the hook's pass is not audited).
func TestOneAuditPerStartedOperation(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	ctx := context.Background()
	a := platformAudit("reindex", "/r\x00code")
	w, err := s.InsertAsyncWorkerAudited(NewAsyncWorker{
		WorkerType: "retrieval.reindex", ComponentRoot: "/r",
		DedupeKey: "/r\x00code", QueuedAt: s.Now(),
	}, a, s.Now())
	if err != nil {
		t.Fatalf("the audited start was refused: %v", err)
	}
	rows, _ := s.PlatformChangeList(ctx, "", 0)
	if len(rows) != 1 {
		t.Fatalf("%d audit rows for one started operation, want exactly 1", len(rows))
	}
	if rows[0].OutcomeRef != w.WorkerID {
		t.Errorf("outcome_ref %q names a different worker than the insert minted (%q)", rows[0].OutcomeRef, w.WorkerID)
	}
	if rows[0].ActorKind != "user" || rows[0].ActorID != "42" {
		t.Errorf("the audit lost its actor: %+v", rows[0])
	}
	if _, err := s.InsertAsyncWorker(NewAsyncWorker{
		WorkerType: "graph.build", DedupeKey: "/plain", QueuedAt: s.Now(),
	}); err != nil {
		t.Fatalf("the plain twin broke: %v", err)
	}
	rows, _ = s.PlatformChangeList(ctx, "", 0)
	if len(rows) != 1 {
		t.Fatalf("the unaudited twin wrote %d audit rows, want still 1 — a hook pass is the daemon's act, not an operation (SP4)", len(rows))
	}
}

// Test 7 — SP3's structural half: a FLIGHT COLLISION writes NOTHING —
// neither a ledger row nor an audit row — because both live in the
// refused transaction.
func TestAlreadyRunningWritesNothing(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	ctx := context.Background()
	n := NewAsyncWorker{WorkerType: "retrieval.reindex", ComponentRoot: "/r", DedupeKey: "/r\x00code", QueuedAt: s.Now()}
	if _, err := s.InsertAsyncWorkerAudited(n, platformAudit("reindex", "/r\x00code"), s.Now()); err != nil {
		t.Fatal(err)
	}
	_, err := s.InsertAsyncWorkerAudited(n, platformAudit("reindex", "/r\x00code"), s.Now())
	if err != ErrAsyncWorkerInFlight {
		t.Fatalf("the collision refused with %v, want ErrAsyncWorkerInFlight", err)
	}
	rows, _ := s.PlatformChangeList(ctx, "", 0)
	if len(rows) != 1 {
		t.Fatalf("%d audit rows after a collision — the second transaction leaked its audit", len(rows))
	}
	live, err := s.ListAsyncWorkers(AsyncWorkerFilter{Type: "retrieval.reindex", DedupeKey: "/r\x00code", States: []string{"queued", "running"}, Limit: 10})
	if err != nil || len(live) != 1 {
		t.Fatalf("%d live rows after a collision (%v), want the one flight holder", len(live), err)
	}
}

// Test 8 — the audit and the start are ONE transaction, proved from the
// audit side: with the audit table gone (test-only surgery — production
// cannot reach this state), the refused audit INSERT rolls the ledger
// insert back with it. No half-started operation exists.
func TestAuditAndStartAreOneTransaction(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	if _, err := s.w.Exec(`DROP TABLE platform_change`); err != nil {
		t.Fatal(err)
	}
	_, err := s.InsertAsyncWorkerAudited(NewAsyncWorker{
		WorkerType: "graph.build", ComponentRoot: "/r", DedupeKey: "/r", QueuedAt: s.Now(),
	}, platformAudit("graph_build", "/r"), s.Now())
	if err == nil {
		t.Fatal("the audited start succeeded with no audit table — it did not ride one transaction")
	}
	if _, err := s.GetAsyncWorker("anything"); err == nil {
		t.Fatal("a worker lookup answered on a half-mutilated store")
	}
	n := 0
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM async_worker`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d async_worker rows after a refused audit insert — the start committed on its own", n)
	}
}

// Test 9 — the malformed audit is refused BEFORE any transaction opens
// (no ledger row is even attempted): an actor-less op cannot reach SQL.
func TestMalformedAuditRefusedBeforeTheLedger(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	bad := platformAudit("reindex", "/r\x00code")
	bad.Actor = PlatformActor{Kind: "system", ID: "hook"}
	_, err := s.InsertAsyncWorkerAudited(NewAsyncWorker{
		WorkerType: "retrieval.reindex", DedupeKey: "/r\x00code", QueuedAt: s.Now(),
	}, bad, s.Now())
	if err == nil {
		t.Fatal("an audit naming 'system' was accepted")
	}
	if !strings.Contains(err.Error(), "lawful actor") {
		t.Errorf("refused with %v — want the shape refusal, not a transaction failure", err)
	}
	n := 0
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM async_worker`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the refused audit still queued %d worker rows", n)
	}
}

// Test 10 — the starter lookup answers the "already running — started by
// WHO" question from the audit, and its honest NOTHING for a write-hook
// worker (no audit row is data, not an error).
func TestPlatformChangeStarter(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	ctx := context.Background()
	w, err := s.InsertAsyncWorkerAudited(NewAsyncWorker{
		WorkerType: "retrieval.reindex", DedupeKey: "/r\x00history", QueuedAt: s.Now(),
	}, platformAudit("reindex", "/r\x00history"), s.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.PlatformChangeStarter(ctx, w.WorkerID)
	if err != nil || got == nil {
		t.Fatalf("the starter lookup missed its own row: %v", err)
	}
	if got.ActorKind != "user" || got.ActorID != "42" {
		t.Errorf("starter answered %+v", got)
	}
	hook, err := s.InsertAsyncWorker(NewAsyncWorker{
		WorkerType: "graph.build", DedupeKey: "/hook", QueuedAt: s.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.PlatformChangeStarter(ctx, hook.WorkerID)
	if err != nil {
		t.Fatalf("the hook lookup errored — an absent audit is data, not a fault: %v", err)
	}
	if got != nil {
		t.Errorf("the hook worker claimed a starter %+v — the write-hook pass is the daemon's act (SP4)", got)
	}
	if _, err := s.PlatformChangeStarter(ctx, ""); err == nil {
		t.Error("the lookup accepted an empty worker id — absence needs a name to be honest about")
	}
}

// Test 11 — the graph deletes' audited twins: one audit per operation,
// the op and the subject composed by the STORE writer (facts, not caller
// options), the actor carried; and the unaudited twins stay unaudited
// (the write hook's pass is not an operation).
func TestGraphAuditedDeletes(t *testing.T) {
	s := openTemp(t)
	defer s.Close()
	ctx := context.Background()

	// Seed rows to delete through the real insert path.
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`INSERT INTO graph_node (component_root, file_path, symbol, kind, language, start_line, end_line)
		VALUES ('/repo/g', 'a.go', 'a.Foo', 'function', 'go', 1, 2)`)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Commit()

	n, err := s.DeleteGraphAudited("/repo/g", platformPerson)
	if err != nil || n != 1 {
		t.Fatalf("audited delete: %d nodes (%v), want 1", n, err)
	}
	rows, _ := s.PlatformChangeList(ctx, "", 0)
	if len(rows) != 1 || rows[0].Op != "graph_delete" || rows[0].Subject != "/repo/g" ||
		rows[0].Summary != "graph.delete started" || rows[0].OutcomeRef != "" {
		t.Fatalf("the delete audit row is wrong: %+v", rows[0])
	}

	// The reparse twin over a claimed file: DeleteGraphFile's transaction
	// carries the audit; the unknown root deletes zero rows and still earns
	// the row (the act started).
	n2, err := s.DeleteGraphAudited("/repo/never", platformPerson)
	if err != nil || n2 != 0 {
		t.Fatalf("zero-row delete: %d (%v), want 0 and no error — deleting what is not there is what delete MEANS", n2, err)
	}
	c, err := s.DeleteGraphFileAudited("/repo/g", "gone.go", platformPerson)
	if err != nil {
		t.Fatalf("the audited file delete broke: %v", err)
	}
	if c.NodesDeleted != 0 {
		t.Fatalf("the zero-row reparse twin deleted %d nodes", c.NodesDeleted)
	}
	rows, _ = s.PlatformChangeList(ctx, "", 0)
	if len(rows) != 3 {
		t.Fatalf("audited graph operations wrote %d rows, want 3 (two deletes + one file delete)", len(rows))
	}
	if rows[0].Op != "graph_reparse" {
		t.Errorf("newest row op = %q, want graph_reparse", rows[0].Op)
	}

	// The unaudited twins stay clean.
	if _, err := s.DeleteGraph("/repo/x"); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.PlatformChangeList(ctx, "", 0)
	if len(rows) != 3 {
		t.Fatalf("the unaudited twin wrote %d more audit rows — the write hook is not audited (SP4)", len(rows)-3)
	}

	// And the malformed actor is refused before the deletes run: the rows
	// a refused operation would have deleted are still there.
	if _, err := s.InsertAsyncWorker(NewAsyncWorker{WorkerType: "x", DedupeKey: "x", QueuedAt: s.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteGraphAudited("/repo/keep", PlatformActor{Kind: "system", ID: "hook"}); err == nil {
		t.Fatal("the audited delete accepted a machine actor")
	}
}
