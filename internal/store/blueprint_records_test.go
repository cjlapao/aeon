package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// blueprint_records_test.go — S48 T1. The four record tables, the one write
// path, the append-only history, the derived refs and the adoption ledger.
// The DDL is what is under test: raw INSERTs attempt every CHECK, and the
// accessors are driven through BlueprintWithTx — the one write path.

const s48BlueprintMigration = "20261102153836_blueprint"

func bpHash(content string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(content))) }

func bpPut(t *testing.T, s *Store, name, content, origin, op, actorKind, actorID string, refs []RefRow) {
	t.Helper()
	h := bpHash(content)
	err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return BlueprintPutTx(tx,
			BlueprintRow{Name: name, Version: 1, Content: content, ContentHash: h, Origin: origin, UpdatedBy: actorKind + ":" + actorID},
			RevisionRow{Name: name, Version: 1, Content: content, ContentHash: h, Op: op, ActorKind: actorKind, ActorID: actorID},
			refs)
	})
	if err != nil {
		t.Fatalf("put blueprint %q: %v", name, err)
	}
}

func bpRow(t *testing.T, s *Store, name string) BlueprintRow {
	t.Helper()
	b, ok, err := s.BlueprintGet(context.Background(), name)
	if err != nil {
		t.Fatalf("get %q: %v", name, err)
	}
	if !ok {
		t.Fatalf("no blueprint row %q", name)
	}
	return b
}

// Test 1 — fresh apply + the upgrade half of DDL drift (dynamic location by
// name, the S47 tool_test precedent).
func TestBlueprintMigrationCreatesTablesIndexesTriggers(t *testing.T) {
	s := openTemp(t)
	for _, table := range []string{"blueprint", "blueprint_revision", "blueprint_ref", "blueprint_import"} {
		countTable(t, s.GormDB(), table)
	}
	for _, idx := range []string{"idx_blueprint_revision_name", "idx_blueprint_ref_target", "idx_blueprint_import_hash"} {
		if n := countCatalogueEntries(t, s.GormDB(), "index", idx); n != 1 {
			t.Fatalf("%s: %d entries, want 1", idx, n)
		}
	}
	for _, trg := range []string{"blueprint_revision_no_update", "blueprint_revision_no_delete"} {
		if n := countCatalogueEntries(t, s.GormDB(), "trigger", trg); n != 1 {
			t.Fatalf("%s: %d entries, want 1", trg, n)
		}
	}
	idx := -1
	for i, m := range Migrations() {
		if m.Name == s48BlueprintMigration {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("the migration %s is not in Migrations()", s48BlueprintMigration)
	}
	g, closeDB := rawGorm(t, filepath.Join(t.TempDir(), "upgrade.db"))
	defer closeDB()
	applyMigrations(t, g, Migrations()[:idx])
	if n := countCatalogueEntries(t, g, "table", "blueprint"); n != 0 {
		t.Fatal("the `blueprint` table exists at the pre-S48 vintage — the migration is not the only DDL source")
	}
	applyMigrations(t, g, Migrations()[idx:idx+1])
	if n := countCatalogueEntries(t, g, "table", "blueprint"); n != 1 {
		t.Fatalf("after applying %s: %d blueprint tables, want 1", s48BlueprintMigration, n)
	}
	if n := countCatalogueEntries(t, g, "trigger", "blueprint_revision_no_update"); n != 1 {
		t.Fatal("the upgrade did not create the append-only UPDATE trigger")
	}
}

// Test 2 — the SQL name CHECK is a NET over the engine's grammar, neither
// stricter nor looser (the grammar mirrored here deliberately — I2 forbids
// the store importing internal/blueprint, so this test IS the pin). The one
// deliberate tightening is the 64-byte cap (names are file names on the
// legacy mirror path); the save path (T6) refuses longer names with a
// sentence before SQL ever sees them.
func TestBlueprintNameCheckAgreesWithGoGrammar(t *testing.T) {
	goGrammar := regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	sqlAccepts := func(name string) bool {
		s := openTemp(t)
		_, err := s.w.Exec(`INSERT INTO blueprint (name, version, content, content_hash, origin, created_at, updated_at, updated_by)
			VALUES (?,1,'x',?,'system','t','t','system:t')`, name, bpHash("x"))
		return err == nil
	}
	good := []string{"a", "0", "z9", "two-step", "dev-loop", "development-map-pipeline", "goal-to-brief",
		"tag_only", "a_b-c9", "chat", "research-map", "brainstorm", "9x", strings.Repeat("a", 64)}
	if len(good) < 12 {
		t.Fatal("the good-name corpus must hold ≥ 12 names")
	}
	for _, name := range good {
		if !goGrammar.MatchString(name) || len(name) > 64 {
			t.Fatalf("test bug: %q is not a Go-grammar name within the cap", name)
		}
		if !sqlAccepts(name) {
			t.Fatalf("the Go grammar accepts %q but the SQL CHECK refused it — net-stricter", name)
		}
	}
	bad := []string{"", "A", "-x", "_x", "a.b", "a/b", "a b", "x\ty", "ab\n", "α", "a'", `a"`, "two step", "TWO-STEP", strings.Repeat("a", 65)}
	for _, name := range bad {
		if sqlAccepts(name) {
			t.Fatalf("the SQL CHECK accepted %q — net-looser (or the cap is not at the schema)", name)
		}
	}
}

// Test 3 — content is DATA stored byte-for-byte: comments, odd spacing,
// CRLF, tabs and the trailing newline round-trip exactly.
func TestBlueprintContentIsStoredVerbatim(t *testing.T) {
	content := "# a comment  \t\nid: two-step\r\nversion: 1\r\nlanes:\r\n  - id: author   # who writes\r\n\r\n    step:\r\n      agent: author\r\n\tgoal_hint: keep me\r\n"
	s := openTemp(t)
	bpPut(t, s, "verbatim", content, "user", "create", "user", "u", nil)
	if got := bpRow(t, s, "verbatim").Content; got != content {
		t.Fatalf("content did not round-trip verbatim:\n got %q\nwant %q", got, content)
	}
	rev, ok, err := s.RevisionGet(context.Background(), "verbatim", 1)
	if err != nil || !ok {
		t.Fatalf("revision: %v ok=%v", err, ok)
	}
	if rev.Content != content || rev.ContentHash != bpHash(content) {
		t.Fatal("the revision did not store the bytes (or the hash) verbatim")
	}
}

// Test 4 — the write is ONE transaction: a forced failure in the refs
// insert (an invalid kind refused by the CHECK) leaves no row and no
// revision; the valid form then lands fully.
func TestPutWritesRowRevisionAndRefsAtomically(t *testing.T) {
	s := openTemp(t)
	h := bpHash("id: atom")
	err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return BlueprintPutTx(tx,
			BlueprintRow{Name: "atom", Version: 1, Content: "id: atom", ContentHash: h, Origin: "user", UpdatedBy: "user:u"},
			RevisionRow{Name: "atom", Version: 1, Content: "id: atom", ContentHash: h, Op: "save", ActorKind: "user", ActorID: "u"},
			[]RefRow{{Name: "atom", Kind: "agent", Target: "author"}, {Name: "atom", Kind: "nonsense", Target: "x"}})
	})
	if err == nil {
		t.Fatal("an invalid ref kind must refuse the whole write")
	}
	if _, ok, _ := s.BlueprintGet(context.Background(), "atom"); ok {
		t.Fatal("the row survived a failed refs insert — the write is not atomic")
	}
	if revs, _ := s.RevisionList(context.Background(), "atom", 10); len(revs) != 0 {
		t.Fatal("the revision survived a failed refs insert")
	}
	bpPut(t, s, "atom", "id: atom", "user", "save", "user", "u", []RefRow{{Name: "atom", Kind: "agent", Target: "author"}})
	names, err := s.BlueprintRefsFor(context.Background(), "agent", "author")
	if err != nil || len(names) != 1 || names[0] != "atom" {
		t.Fatalf("the valid write did not land fully: %v %v", names, err)
	}
}

// Test 5 — per-name revision numbering NEVER resets: 1,2 → delete →
// recreate → 3 (history survives, the new row continues the count).
func TestRevisionNumbersContinueAcrossDeleteAndRecreate(t *testing.T) {
	s := openTemp(t)
	bpPut(t, s, "rev", "id: rev", "user", "create", "user", "u", nil)
	bpPut(t, s, "rev", "id: rev\n# edited", "user", "save", "user", "u", nil)
	if err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error { return BlueprintDeleteTx(tx, "rev") }); err != nil {
		t.Fatal(err)
	}
	bpPut(t, s, "rev", "id: rev", "user", "create", "user", "v", nil)
	revs, err := s.RevisionList(context.Background(), "rev", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 || revs[0].Rev != 3 || revs[1].Rev != 2 || revs[2].Rev != 1 {
		t.Fatalf("want newest-first 3,2,1 (numbering continuing past the delete), got %+v", revs)
	}
}

// Test 6 — append-only BY TRIGGER: raw UPDATE/DELETE abort naming it;
// appends still work.
func TestRevisionIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	bpPut(t, s, "hist", "id: hist", "user", "create", "user", "u", nil)
	if _, err := s.w.Exec(`UPDATE blueprint_revision SET content = 'tampered' WHERE name = 'hist'`); err == nil {
		t.Fatal("UPDATE on blueprint_revision was allowed — the append-only trigger is not armed")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("wrong refusal: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM blueprint_revision WHERE name = 'hist'`); err == nil {
		t.Fatal("DELETE on blueprint_revision was allowed")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("wrong refusal: %v", err)
	}
	bpPut(t, s, "hist", "id: hist\n#2", "user", "save", "user", "u", nil)
}

// Test 7 — delete removes the row and its refs, keeps the revisions.
func TestDeleteRemovesRowAndRefsKeepsRevisions(t *testing.T) {
	s := openTemp(t)
	bpPut(t, s, "gone", "id: gone", "user", "create", "user", "u", []RefRow{{Name: "gone", Kind: "agent", Target: "author"}})
	if err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error { return BlueprintDeleteTx(tx, "gone") }); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.BlueprintGet(context.Background(), "gone"); ok {
		t.Fatal("the row survived the delete")
	}
	if names, _ := s.BlueprintRefsFor(context.Background(), "agent", "author"); len(names) != 0 {
		t.Fatalf("the refs survived the delete: %v", names)
	}
	revs, err := s.RevisionList(context.Background(), "gone", 50)
	if err != nil || len(revs) != 1 || revs[0].Content != "id: gone" {
		t.Fatalf("the history did not survive the row's deletion: %+v %v", revs, err)
	}
}

// Test 8 — the ref replacement is scoped to ONE name.
func TestPutReplacesRefsForTheNameOnly(t *testing.T) {
	s := openTemp(t)
	bpPut(t, s, "bp1", "id: bp1", "user", "create", "user", "u", []RefRow{{Name: "bp1", Kind: "agent", Target: "shared"}})
	bpPut(t, s, "bp2", "id: bp2", "user", "create", "user", "u", []RefRow{{Name: "bp2", Kind: "agent", Target: "shared"}, {Name: "bp2", Kind: "action", Target: "forge.merge"}})
	bpPut(t, s, "bp1", "id: bp1\n#2", "user", "save", "user", "u", []RefRow{{Name: "bp1", Kind: "agent", Target: "solo"}})
	if names, _ := s.BlueprintRefsFor(context.Background(), "agent", "shared"); len(names) != 1 || names[0] != "bp2" {
		t.Fatalf("bp2's ref bled: %v", names)
	}
	if own, err := s.BlueprintOwnRefs(context.Background(), "bp1"); err != nil || len(own) != 1 || own[0].Target != "solo" {
		t.Fatalf("bp1's own refs: %+v %v", own, err)
	}
}

// Test 9 — BlueprintRefsFor answers "who uses this?" exactly.
func TestBlueprintRefsForFindsUsers(t *testing.T) {
	s := openTemp(t)
	bpPut(t, s, "one", "id: one", "user", "create", "user", "u", []RefRow{{Name: "one", Kind: "agent", Target: "golang-engineer"}, {Name: "one", Kind: "converse", Target: "chat"}})
	bpPut(t, s, "two", "id: two", "user", "create", "user", "u", []RefRow{{Name: "two", Kind: "agent", Target: "author"}})
	bpPut(t, s, "three", "id: three", "user", "create", "user", "u", []RefRow{{Name: "three", Kind: "agent", Target: "golang-engineer"}})
	if got, err := s.BlueprintRefsFor(context.Background(), "agent", "golang-engineer"); err != nil || strings.Join(got, ",") != "one,three" {
		t.Fatalf("agent users: %v %v", got, err)
	}
	if got, err := s.BlueprintRefsFor(context.Background(), "converse", "chat"); err != nil || strings.Join(got, ",") != "one" {
		t.Fatalf("converse users: %v %v", got, err)
	}
	if got, err := s.BlueprintRefsFor(context.Background(), "agent", "nobody"); err != nil || len(got) != 0 {
		t.Fatalf("unknown target must be empty: %v %v", got, err)
	}
}

// Test 10 — BlueprintReferences counts LIVE rows only (T0 item 3): every
// schedule/event_trigger row (pause/disable is reversible → still live) and
// queue items in live statuses; done/cancelled items and started loops never
// refuse.
func TestBlueprintReferencesCountsOnlyLiveOnes(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	bpPut(t, s, "used", "id: used", "user", "create", "user", "u", nil)
	bpPut(t, s, "free", "id: free", "user", "create", "user", "u", nil)
	seedQueueComponent(t, s, "u", "/r/e")
	seed := func(q string, args ...any) {
		t.Helper()
		if _, err := s.w.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed(`INSERT INTO trigger_schedule (id, kind, blueprint, zone, created_at) VALUES ('sch1','schedule','used','UTC','2026-01-01T00:00:00Z')`)
	seed(`INSERT INTO event_trigger (id,name,match_json,blueprint,max_runs_per_hour,from_outbox_id,created_by_kind,created_by_id,created_at,updated_at)
		VALUES ('et1','night','{}','used',10,0,'user','u','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
	for i, st := range []string{"pending", "ready", "running", "blocked", "done", "cancelled"} {
		seed(`INSERT INTO queue_item (component_root,position,status,blueprint,goal,workspace_root,created_at,updated_at)
			VALUES ('/r/e',?,?,'used','g','/r','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, i, st)
	}
	for i, st := range []string{"done", "cancelled"} {
		seed(`INSERT INTO queue_item (component_root,position,status,blueprint,goal,workspace_root,created_at,updated_at)
			VALUES ('/r/e',?,?,'free','g','/r','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, 100+i, st)
	}
	seedRunLoopRow(t, s, "L1", "used-loop")
	seed(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_at,ended_at)
		VALUES ('L1',1,'used',1,'x','done','user','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`)

	refs, err := s.BlueprintReferences(ctx, "used")
	if err != nil {
		t.Fatal(err)
	}
	if !refs.Any() {
		t.Fatal("live references did not refuse")
	}
	if len(refs.Schedules) != 1 || refs.Schedules[0].ID != "sch1" || refs.Schedules[0].Kind != "schedule" {
		t.Fatalf("schedules: %+v", refs.Schedules)
	}
	if len(refs.Triggers) != 1 || refs.Triggers[0].Name != "night" {
		t.Fatalf("triggers: %+v", refs.Triggers)
	}
	if len(refs.QueueItems) != 4 {
		t.Fatalf("queue items must be pending/ready/running/blocked only: %+v", refs.QueueItems)
	}
	if refs.LoopsStarted != 1 {
		t.Fatalf("loops started (informational): %d, want 1", refs.LoopsStarted)
	}
	free, err := s.BlueprintReferences(ctx, "free")
	if err != nil {
		t.Fatal(err)
	}
	if free.Any() {
		t.Fatalf("done/cancelled items refused a delete: %+v", free)
	}
}

// Test 11 — the ledger: a second 'imported' row per hash is refused (no
// double adoption); 'refused' rows may repeat; ImportByHash answers the
// idempotence question; ImportRefusals lists what never became a row.
func TestImportLedgerUniquePerHash(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	insert := func(sha, result string) error {
		return s.BlueprintWithTx(ctx, func(tx *sql.Tx) error {
			return ImportInsertTx(tx, ImportRow{Path: "/state/blueprints/a.yaml", SHA256: sha, Name: "a", Result: result, Detail: "diagnostics…"})
		})
	}
	good := bpHash("id: a")
	if err := insert(good, "imported"); err != nil {
		t.Fatal(err)
	}
	if err := insert(good, "imported"); err == nil {
		t.Fatal("a second imported row for the same hash was allowed — a file could be adopted twice")
	}
	if err := insert("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "refused"); err != nil {
		t.Fatalf("first refused row: %v", err)
	}
	if err := insert("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "refused"); err != nil {
		t.Fatalf("repeated refused rows must be allowed (the same bad file re-reported): %v", err)
	}
	if _, ok, _ := s.ImportByHash(ctx, good); !ok {
		t.Fatal("ImportByHash missed the imported hash")
	}
	if _, ok, _ := s.ImportByHash(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); ok {
		t.Fatal("a refused hash must not read as imported")
	}
	if refusals, err := s.ImportRefusals(ctx); err != nil || len(refusals) != 2 {
		t.Fatalf("refusals: %d %v", len(refusals), err)
	}
}

// Test 12 — the size cap at the schema: exactly 1 MiB stored, +1 refused,
// empty refused.
func TestContentSizeCap(t *testing.T) {
	s := openTemp(t)
	big := "id: big\n# " + strings.Repeat("x", (1<<20)-len("id: big\n# "))
	if len(big) != 1<<20 {
		t.Fatalf("test bug: %d bytes", len(big))
	}
	bpPut(t, s, "big", big, "user", "create", "user", "u", nil)
	if got := bpRow(t, s, "big").Content; len(got) != 1<<20 {
		t.Fatalf("stored %d bytes, want 1 MiB", len(got))
	}
	try := func(name, content string) error {
		return s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
			return BlueprintPutTx(tx,
				BlueprintRow{Name: name, Version: 1, Content: content, ContentHash: bpHash(content), Origin: "user", UpdatedBy: "user:u"},
				RevisionRow{Name: name, Version: 1, Content: content, ContentHash: bpHash(content), Op: "save", ActorKind: "user", ActorID: "u"}, nil)
		})
	}
	if err := try("over", big+"x"); err == nil {
		t.Fatal("a 1 MiB + 1 content was stored — the cap is not at the schema")
	}
	if err := try("empty", ""); err == nil {
		t.Fatal("empty content was stored")
	}
}

// Test 13 — the closed vocabularies are the store's schema, not Go's
// kindness: raw INSERTs walk every CHECK with its positive control beside it.
func TestBlueprintRecordChecksRefuseBadRows(t *testing.T) {
	s := openTemp(t)
	raw := func(q string, args ...any) error {
		_, err := s.w.Exec(q, args...)
		return err
	}
	h := bpHash("x")
	if err := raw(`INSERT INTO blueprint (name,version,content,content_hash,origin,created_at,updated_at,updated_by)
		VALUES ('o1',1,'x',?,'machine','t','t','user:u')`, h); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("a third origin passed: %v", err)
	}
	if err := raw(`INSERT INTO blueprint (name,version,content,content_hash,origin,created_at,updated_at,updated_by)
		VALUES ('o1',0,'x',?,'user','t','t','user:u')`, h); err == nil {
		t.Fatal("version 0 passed")
	}
	if err := raw(`INSERT INTO blueprint (name,version,content,content_hash,origin,created_at,updated_at,updated_by)
		VALUES ('o1',1,'x',?,'user','t','t','')`, h); err == nil {
		t.Fatal("an empty updated_by passed")
	}
	if err := raw(`INSERT INTO blueprint (name,version,content,content_hash,origin,created_at,updated_at,updated_by)
		VALUES ('o1',1,'x',?,'user','t','t','user:u')`, h); err != nil {
		t.Fatalf("the positive control refused: %v", err)
	}
	if err := raw(`INSERT INTO blueprint (name,version,content,content_hash,origin,created_at,updated_at,updated_by)
		VALUES ('o2',1,'x',?,'system','t','t','system:seed')`, h); err != nil {
		t.Fatalf("the system positive control refused: %v", err)
	}
	rev := func(op string) error {
		return raw(`INSERT INTO blueprint_revision (name,rev,version,content,content_hash,op,note,actor_kind,actor_id,occurred_at)
			VALUES ('o1',9,1,'x',?,?,'','user','u','t')`, h, op)
	}
	for _, bad := range []string{"import", "delete", "", "Save"} {
		if err := rev(bad); err == nil {
			t.Fatalf("op %q passed the CHECK", bad)
		}
	}
	if err := rev("adopt"); err != nil {
		t.Fatalf("the op positive control refused: %v", err)
	}
	if err := raw(`INSERT INTO blueprint_revision (name,rev,version,content,content_hash,op,note,actor_kind,actor_id,occurred_at)
		VALUES ('o1',10,1,'x',?,'save','','robot','u','t')`, h); err == nil {
		t.Fatal("a fourth actor_kind passed")
	}
	if err := raw(`INSERT INTO blueprint_revision (name,rev,version,content,content_hash,op,note,actor_kind,actor_id,occurred_at)
		VALUES ('o1',10,1,'x',?,'save','','user','','t')`, h); err == nil {
		t.Fatal("an empty actor_id passed")
	}
	if err := raw(`INSERT INTO blueprint_ref (name,kind,target) VALUES ('o1','tool','bash')`); err == nil {
		t.Fatal("a sixth ref kind passed")
	}
	if err := raw(`INSERT INTO blueprint_ref (name,kind,target) VALUES ('o1','agent','')`); err == nil {
		t.Fatal("an empty ref target passed")
	}
	if err := raw(`INSERT INTO blueprint_ref (name,kind,target) VALUES ('o1','agent','author')`); err != nil {
		t.Fatalf("the ref positive control refused: %v", err)
	}
	if err := raw(`INSERT INTO blueprint_ref (name,kind,target) VALUES ('o1','agent','author')`); err == nil {
		t.Fatal("a duplicate (name,kind,target) passed the PK")
	}
	// note ≤ 256.
	if err := raw(`INSERT INTO blueprint_revision (name,rev,version,content,content_hash,op,note,actor_kind,actor_id,occurred_at)
		VALUES ('o1',11,1,'x',?,'save',?,'user','u','t')`, h, strings.Repeat("n", 257)); err == nil {
		t.Fatal("a 257-byte note passed")
	}
}
