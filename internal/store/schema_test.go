package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
	"github.com/cjlapao/go-backend-scaffolding/framework/database/migrations"
	"gorm.io/gorm"

	"github.com/cjlapao/aeon/internal/database"
)

// T2's tests: the schema, the ledger, and the two invariants that bind them.

// frozenSchema0005 is the schema AS IT WAS when migration 0006 was added.
//
// It is still named for 0005 and still seeds the ledger for 0001–0005, so the
// runner now walks it through **0006 and 0007**. That is the property Q10 was
// written for, demonstrated for real the first time a migration was added after
// the test: nobody touched this literal, and it keeps proving what it was
// written to prove.
//
// # Q10 — how the prior version is held, and how a later migration keeps this
// test alive
//
// It is a LITERAL in this file, not a committed binary and not a slice of
// Migrations(). A binary goes stale the first time somebody adds a column and
// then quietly stops testing anything; a derived subset tests the code against
// itself, which is not a database this code did not create.
//
// It is paired with ledger rows for 0001–0005, exactly as a real database of
// that vintage would carry — which is what makes it survive a future
// migration: when 0007 is added, the runner walks this snapshot through 0006
// AND 0007, and the test keeps proving what it was written to prove. Nobody has
// to touch it.
//
// Hand-copied from the migrations at the commit that added 0006. It is
// deliberately NOT regenerated: the point is that it is frozen.
const frozenSchema0005 = `
CREATE TABLE project (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  slug         TEXT NOT NULL,
  remote       TEXT,
  remote_key   TEXT,
  repo_key     TEXT,
  owner        TEXT,
  tags         TEXT NOT NULL DEFAULT '[]',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX idx_project_remote_key ON project (remote_key);
CREATE INDEX idx_project_repo_key   ON project (repo_key);
CREATE TABLE project_root (
  project_root TEXT PRIMARY KEY,
  project_id   TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
  remote_key   TEXT,
  repo_key     TEXT,
  is_primary   INTEGER NOT NULL DEFAULT 0,
  added_at     TEXT NOT NULL
);
CREATE INDEX idx_project_root_project ON project_root (project_id);
CREATE TABLE loop (
  id                 TEXT NOT NULL,
  slug               TEXT NOT NULL,
  title              TEXT NOT NULL,
  goal               TEXT NOT NULL,
  root               TEXT NOT NULL,
  project_root       TEXT NOT NULL,
  project_key        TEXT,
  mode               TEXT NOT NULL,
  phase              TEXT NOT NULL,
  status             TEXT NOT NULL,
  default_branch     TEXT,
  base_branch        TEXT,
  branch_mode        TEXT,
  feature_branch     TEXT,
  total_failed_gates INTEGER NOT NULL DEFAULT 0,
  splits_done        INTEGER NOT NULL DEFAULT 0,
  qa_rounds          INTEGER NOT NULL DEFAULT 0,
  now_task           TEXT,
  now_agent          TEXT,
  now_step           TEXT,
  now_dispatched_at  TEXT,
  started_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  PRIMARY KEY (id),
  UNIQUE (project_root, slug)
);
CREATE INDEX idx_loop_updated ON loop (updated_at DESC);
CREATE TABLE loop_phase (
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  phase        TEXT NOT NULL,
  status       TEXT NOT NULL,
  ord          INTEGER NOT NULL,
  created_at   TEXT,
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (project_root, loop_slug, phase),
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE TABLE task (
  loop_slug         TEXT NOT NULL,
  project_root      TEXT NOT NULL,
  taskid            TEXT NOT NULL,
  ord               INTEGER NOT NULL,
  title             TEXT NOT NULL,
  tech              TEXT NOT NULL,
  engineer          TEXT NOT NULL,
  depends_on        TEXT NOT NULL DEFAULT '[]',
  status            TEXT NOT NULL,
  issue             TEXT,
  pr                TEXT,
  branch            TEXT,
  retries           INTEGER NOT NULL DEFAULT 0,
  security_rounds   INTEGER NOT NULL DEFAULT 0,
  review_rounds     INTEGER NOT NULL DEFAULT 0,
  est_changed_lines INTEGER,
  success_summary   TEXT,
  last_evidence     TEXT,
  started_at        TEXT,
  created_at        TEXT,
  updated_at        TEXT NOT NULL,
  PRIMARY KEY (project_root, loop_slug, taskid),
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE TABLE step (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug         TEXT NOT NULL,
  project_root      TEXT NOT NULL,
  taskid            TEXT,
  phase             TEXT NOT NULL,
  agent             TEXT NOT NULL,
  attempt           INTEGER NOT NULL DEFAULT 1,
  status            TEXT NOT NULL CHECK (status IN ('running','suspended','orphaned','done')),
  started_at        TEXT NOT NULL,
  agent_returned_at TEXT,
  ended_at          TEXT,
  agent_ms          INTEGER,
  wall_ms           INTEGER,
  outcome           TEXT,
  ended_by          TEXT NOT NULL DEFAULT 'open',
  source            TEXT NOT NULL DEFAULT 'live',
  cost_usd          REAL,
  cost_state        TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_state IN ('known','partial','unknown')),
  cost_detail       TEXT,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE INDEX idx_step_loop ON step (project_root, loop_slug, id);
CREATE INDEX idx_step_status ON step (status, id);
CREATE TABLE step_message (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  step_id        INTEGER NOT NULL REFERENCES step (id) ON DELETE CASCADE,
  seq            INTEGER NOT NULL,
  format_version INTEGER NOT NULL,
  role           TEXT NOT NULL,
  content        TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  UNIQUE (step_id, seq)
);
CREATE TABLE event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT,
  project_root TEXT,
  kind         TEXT NOT NULL,
  payload      TEXT NOT NULL,
  created_at   TEXT NOT NULL
);
CREATE INDEX idx_event_scope ON event (project_root, loop_slug, id);
CREATE INDEX idx_event_kind  ON event (kind, id);
CREATE TABLE memory (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  ts           TEXT NOT NULL,
  agent        TEXT,
  taskid       TEXT,
  note         TEXT NOT NULL,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE INDEX idx_memory_loop ON memory (project_root, loop_slug, id);
CREATE TABLE question (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  step_id      INTEGER REFERENCES step (id) ON DELETE CASCADE,
  tool_use_id  TEXT,
  kind         TEXT NOT NULL CHECK (kind IN ('clarify','gate','permission','free')),
  body         TEXT NOT NULL,
  options      TEXT,
  answer       TEXT,
  asked_at     TEXT NOT NULL,
  answered_at  TEXT,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE
);
CREATE INDEX idx_question_pending ON question (project_root, loop_slug, answered_at, id);
CREATE TABLE chat_message (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  loop_slug    TEXT NOT NULL,
  project_root TEXT NOT NULL,
  seq          INTEGER NOT NULL,
  author       TEXT NOT NULL CHECK (author IN ('human','agent','system')),
  kind         TEXT NOT NULL CHECK (kind IN ('note','steer','question','answer','summary')),
  body         TEXT NOT NULL,
  question_id  INTEGER REFERENCES question (id) ON DELETE SET NULL,
  created_at   TEXT NOT NULL,
  FOREIGN KEY (project_root, loop_slug) REFERENCES loop (project_root, slug) ON DELETE CASCADE,
  UNIQUE (project_root, loop_slug, seq)
);
CREATE TABLE loop_document (
  id           TEXT PRIMARY KEY,
  project_root TEXT NOT NULL,
  project_key  TEXT,
  loop_slug    TEXT NOT NULL,
  file         TEXT NOT NULL,
  rev          INTEGER NOT NULL,
  content      BLOB NOT NULL,
  bytes        INTEGER NOT NULL,
  sha256       TEXT NOT NULL,
  written_by   TEXT,
  written_at   TEXT NOT NULL,
  UNIQUE (project_root, loop_slug, file, rev)
);
CREATE INDEX idx_document_head ON loop_document (project_root, loop_slug, file, rev DESC);
CREATE INDEX idx_document_loop ON loop_document (project_root, loop_slug);
CREATE INDEX idx_document_project_key ON loop_document (project_key, loop_slug);
CREATE TABLE loop_archive (
  id           TEXT PRIMARY KEY,
  slug         TEXT NOT NULL,
  project_root TEXT NOT NULL,
  title        TEXT NOT NULL,
  goal         TEXT NOT NULL,
  status       TEXT NOT NULL,
  started_at   TEXT,
  ended_at     TEXT,
  archived_at  TEXT NOT NULL,
  state        BLOB NOT NULL,
  state_bytes  INTEGER NOT NULL,
  file_count   INTEGER NOT NULL,
  file_bytes   INTEGER NOT NULL,
  note         TEXT
);
CREATE INDEX idx_archive_project ON loop_archive (project_root, archived_at);
CREATE TABLE loop_archive_file (
  archive_id TEXT NOT NULL REFERENCES loop_archive (id) ON DELETE CASCADE,
  path       TEXT NOT NULL,
  bytes      INTEGER NOT NULL,
  sha256     TEXT NOT NULL,
  modified   TEXT,
  content    BLOB NOT NULL,
  PRIMARY KEY (archive_id, path)
);
CREATE TABLE policy (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL CHECK (type IN ('tool','path','network','spend','approval')),
  target_kind TEXT NOT NULL CHECK (target_kind IN ('global','project','loop','agent','route')),
  target_id   TEXT,
  action      TEXT NOT NULL CHECK (action IN ('allow','deny')),
  spec        TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL,
  not_after   TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX idx_policy_target ON policy (target_kind, target_id);
CREATE TABLE policy_acceptance (
  policy_id   TEXT NOT NULL REFERENCES policy (id) ON DELETE CASCADE,
  target_kind TEXT NOT NULL,
  target_id   TEXT NOT NULL,
  accepted_by TEXT NOT NULL,
  accepted_at TEXT NOT NULL,
  PRIMARY KEY (policy_id, target_kind, target_id)
);
CREATE TABLE policy_event (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  policy_id    TEXT REFERENCES policy (id) ON DELETE SET NULL,
  subject_kind TEXT NOT NULL,
  subject_id   TEXT NOT NULL,
  outcome      TEXT NOT NULL,
  detail       TEXT,
  at           TEXT NOT NULL
);
CREATE INDEX idx_policy_event_policy ON policy_event (policy_id, at);
`

// migrationsAppliedInFrozenSchema names the ledger rows a database of that
// vintage carries.
var migrationsAppliedInFrozenSchema = []string{
	"0001_core_tables", "0002_steps_and_events", "0003_chat",
	"0004_documents_and_archive", "0005_policy",
}

func TestAFreshSchemaAndAnUpgradedSchemaAreIdentical(t *testing.T) {
	// D4 / I3, in the form that can actually catch the bug.
	//
	// The design asked for "fresh vs migrated" with TWO DDL sources. That
	// comparison cannot fail: two copies edited in the same commit agree with
	// each other and still break an upgraded install. What breaks an upgraded
	// install is a database built by an OLD CREATE plus every ALTER since,
	// differing from one built by today's CREATE — so that is what this
	// compares.
	//
	// It fails exactly when somebody adds a column to an existing migration's
	// CREATE TABLE instead of adding a new migration, which is the real version
	// of this mistake.
	fresh := openTemp(t)
	freshFP, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fingerprint the fresh schema: %v", err)
	}

	upgraded := upgradedStore(t)
	upgradedFP, err := upgraded.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fingerprint the upgraded schema: %v", err)
	}

	if !freshFP.Equal(upgradedFP) {
		t.Errorf("a fresh schema and an upgraded one differ:\n  %s",
			strings.Join(freshFP.Diff(upgradedFP), "\n  "))
	}
	if len(freshFP.Lines) < 150 {
		// A guard on the guard: a fingerprint that describes nothing compares
		// equal to another fingerprint that describes nothing.
		t.Errorf("the fingerprint has only %d lines, which is too few to be describing this schema",
			len(freshFP.Lines))
	}
}

func TestTheEquivalenceTestCanFail(t *testing.T) {
	// The negative control. A comparison that cannot fail is not a test, and
	// this one's whole value is that it notices drift — so drift is introduced
	// deliberately and the comparison must see it.
	fresh := openTemp(t)
	freshFP, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	drifted := openTemp(t)
	if _, err := drifted.w.Exec(`ALTER TABLE loop ADD COLUMN drifted TEXT`); err != nil {
		t.Fatalf("introduce drift: %v", err)
	}
	driftedFP, err := drifted.SchemaFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	if freshFP.Equal(driftedFP) {
		t.Fatal("the fingerprint did not notice an added column, so the equivalence test proves nothing")
	}
	diff := freshFP.Diff(driftedFP)
	if len(diff) == 0 || !strings.Contains(strings.Join(diff, "\n"), "drifted") {
		t.Errorf("the diff does not name the drifted column: %v", diff)
	}
}

func TestAPriorVersionDatabaseUpgradesWithRowsIntact(t *testing.T) {
	// D3 / O2. The runner is exercised against a database IT DID NOT CREATE,
	// which is the only way its ALTER path has ever run.
	path := filepath.Join(t.TempDir(), "old.db")
	seedFrozen(t, path)

	// Row counts before, per table, so "every seeded row survived" is a
	// measurement rather than a spot check.
	before := rowCounts(t, path)

	s, err := Open(Options{Dir: filepath.Dir(path), File: filepath.Base(path), Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the prior-version database: %v", err)
	}
	defer s.Close()

	after := rowCounts(t, path)
	// S38 OL1: 0035 RENAMES two of the seeded tables, and a rename MOVES —
	// the rows must be found under the new name, and the old name must be
	// GONE (a table that exists under both names is a copy, not a rename).
	renamed := map[string]string{"project": "component", "project_root": "component_root"}
	for table, n := range before {
		// `_migrations` legitimately GROWS: it is the ledger recording the
		// upgrade that just happened, not data the upgrade had to preserve.
		// Every other table must come through untouched.
		if table == "_migrations" {
			if after[table] <= n {
				t.Errorf("the ledger did not grow (%d rows before, %d after), so no migration ran", n, after[table])
			}
			continue
		}
		if to, moved := renamed[table]; moved {
			if after[to] != n {
				t.Errorf("%s had %d rows before the upgrade and %d under its new name %s after — a RENAME does not delete", table, n, after[to], to)
			}
			// `project` legitimately EXISTS again — the group reclaimed the
			// name (OL1). `project_root` has no successor and must be gone.
			if table == "project_root" {
				if _, gone := after[table]; gone {
					t.Errorf("%s still exists after the upgrade — OL1 is a rename, not a copy", table)
				}
			}
			continue
		}
		if after[table] != n {
			t.Errorf("%s had %d rows before the upgrade and %d after", table, n, after[table])
		}
	}
	if len(before) < 14 {
		t.Errorf("the frozen database only seeded %d tables, which is too few to prove the upgrade "+
			"preserved anything", len(before))
	}

	// The ALTERed column exists, and is NULL for pre-existing rows — which is
	// the shape SQLite requires of an added column and the reason every
	// additive column in this schema is nullable or defaulted.
	var manifest sql.NullString
	if err := s.r.QueryRow(`SELECT context_manifest FROM step ORDER BY id LIMIT 1`).Scan(&manifest); err != nil {
		t.Fatalf("read the ALTERed column: %v", err)
	}
	if manifest.Valid {
		t.Errorf("a pre-existing step has context_manifest = %q; it should be NULL", manifest.String)
	}

	// And the upgrade applied exactly the migrations the snapshot was missing.
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(Migrations()) {
		t.Errorf("the ledger holds %d rows after the upgrade, want %d", len(applied), len(Migrations()))
	}
}

func TestTheUpgradeExercisesAtLeastOneAlterTable(t *testing.T) {
	// DB9's capability is exercised by the suite rather than asserted. Without
	// this, a schema that happened to need no ALTER would leave the runner's
	// ALTER path untested forever — and the first real one would run in
	// production.
	var withAlter []string
	for _, m := range Migrations() {
		if strings.Contains(strings.ToUpper(m.SQL), "ALTER TABLE") {
			withAlter = append(withAlter, m.Name)
		}
	}
	if len(withAlter) == 0 {
		t.Fatal("no migration contains an ALTER TABLE, so DB9's capability is asserted and never run")
	}

	// And it must be one the frozen snapshot does NOT already have, or the
	// upgrade test walks no ALTER.
	pending := 0
	for _, m := range Migrations() {
		if !containsString(migrationsAppliedInFrozenSchema, m.Name) &&
			strings.Contains(strings.ToUpper(m.SQL), "ALTER TABLE") {
			pending++
		}
	}
	if pending == 0 {
		t.Errorf("every ALTER migration (%v) is already applied in the frozen snapshot, so D3's test "+
			"upgrades through no ALTER. Add the frozen snapshot's successor, or freeze a newer one",
			withAlter)
	}
}

func TestEachMigrationRunsExactlyOnceAcrossThreeOpens(t *testing.T) {
	// D2. The ledger is the framework's `_migrations`; this asserts the
	// property, not the mechanism.
	dir := t.TempDir()
	for open := 1; open <= 3; open++ {
		s := openAt(t, dir)
		applied, err := s.AppliedMigrations()
		if err != nil {
			t.Fatalf("open %d: %v", open, err)
		}
		if len(applied) != len(Migrations()) {
			t.Fatalf("open %d: the ledger holds %d rows, want one per migration (%d)",
				open, len(applied), len(Migrations()))
		}
		seen := map[string]int{}
		for _, a := range applied {
			seen[a.Name]++
			if a.Status != "applied" {
				t.Errorf("open %d: %s is recorded as %q", open, a.Name, a.Status)
			}
		}
		for name, n := range seen {
			if n != 1 {
				t.Errorf("open %d: %s is recorded %d times", open, name, n)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatalf("open %d: close: %v", open, err)
		}
	}
}

func TestPreMergeProjectIconDatabaseUpgradesAfterPolicyMerge(t *testing.T) {
	dir := t.TempDir()
	g, closeDB := rawGorm(t, filepath.Join(dir, storeFileName))
	applyMigrations(t, g, Migrations()[:15])
	applyMigrations(t, g, []Migration{{16, "0016_project_icon", "pre-merge icon", migration0017}})
	if err := g.Exec(`INSERT INTO project (id,name,slug,icon,created_at,updated_at) VALUES ('p','project','project','star','t','t')`).Error; err != nil {
		t.Fatal(err)
	}
	// A previous startup may already have recorded the duplicate-column failure.
	if err := g.Exec(`INSERT INTO _migrations (id,name,status) VALUES ('failed-icon','0017_project_icon','failed')`).Error; err != nil {
		t.Fatal(err)
	}
	closeDB()

	s := openAt(t, dir)
	var icon string
	// S38 OL1: the pre-merge fork's icon rides on the table THAT became
	// `component` — `project` after the upgrade is the GROUP, and the row the
	// fork preserved is not in it. The point of the test is unchanged: the
	// forked value survived the merge, wherever the rename filed it.
	if err := s.w.QueryRow(`SELECT icon FROM component WHERE id = 'p'`).Scan(&icon); err != nil {
		t.Fatal(err)
	}
	if icon != "star" {
		t.Fatalf("project icon = %q, want preserved value", icon)
	}
	var applied int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name IN ('0016_project_icon','0016_policy_governance','0017_project_icon','0018_policy_authorship_net') AND status = 'applied'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 4 {
		t.Fatalf("merge migrations applied = %d, want 4", applied)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}

func TestRenumberedPauseReasonDatabaseUpgradesAfterRenumber(t *testing.T) {
	// The pre-renumber branch shipped step.pause_reason as 0019; S11 took that
	// number for policy_action_ask_and_loop_attended and pushed pause_reason
	// to 0020. A database built by the old branch must upgrade, not die on a
	// duplicate column — the same class as the pre-merge icon above, with the
	// failed 0020 ledger row a crashed startup may have left behind.
	dir := t.TempDir()
	g, closeDB := rawGorm(t, filepath.Join(dir, storeFileName))
	applyMigrations(t, g, Migrations()[:19])
	applyMigrations(t, g, []Migration{{19, "0019_step_pause_reason", "pre-renumber pause reason", migration0020}})
	// A previous startup may already have recorded the duplicate-column failure.
	if err := g.Exec(`INSERT INTO _migrations (id,name,status) VALUES ('failed-pause','0020_step_pause_reason','failed')`).Error; err != nil {
		t.Fatal(err)
	}
	closeDB()

	s := openAt(t, dir)
	// The column is there exactly once, and a write through it works.
	var cols int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('step') WHERE name = 'pause_reason'`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 1 {
		t.Fatalf("step.pause_reason columns = %d, want exactly 1", cols)
	}
	var applied int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name IN ('0019_step_pause_reason','0020_step_pause_reason') AND status = 'applied'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 2 {
		t.Fatalf("pause-reason ledger rows applied = %d, want 2 (both the legacy 0019 entry and the 0020 entry)", applied)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}

func TestMigrationsApplyInIDOrderNotArrayOrder(t *testing.T) {
	// With a deliberately MISORDERED set. A runner that happens to apply array
	// order passes every test built from an ordered one — and the failure is a
	// database where 0006's ALTER ran before 0002 created the table.
	misordered := []Migration{
		{3, "m3", "third", `CREATE TABLE third (id INTEGER PRIMARY KEY);`},
		{1, "m1", "first", `CREATE TABLE first (id INTEGER PRIMARY KEY);`},
		{2, "m2", "second", `ALTER TABLE first ADD COLUMN added TEXT;`},
	}

	dir := t.TempDir()
	g, closeDB := rawGorm(t, filepath.Join(dir, "order.db"))
	defer closeDB()

	svc := migrations.NewMigrationService(g)
	svc.SetMaxRetries(1)
	for _, w := range migrationWorkers(g, misordered) {
		svc.Register(w)
	}
	if diag := svc.RunAll(appctx.NewContext(context.Background())); diag != nil && diag.HasErrors() {
		// m2 ALTERs a table m1 creates. In array order it would run first and
		// fail with "no such table: first".
		t.Fatalf("the misordered set failed, so application order is array order: %s", diag.GetSummary())
	}

	var cols int
	if err := g.Raw(`SELECT count(*) FROM pragma_table_info('first')`).Scan(&cols).Error; err != nil {
		t.Fatal(err)
	}
	if cols != 2 {
		t.Errorf("the ALTER did not apply: `first` has %d columns, want 2", cols)
	}
}

func TestAFailingMigrationRollsBackAndTheOpenFailsLoudly(t *testing.T) {
	// D5. A partially migrated database is worse than one that refuses to
	// open, because the caller proceeds against a shape nothing expects.
	//
	// Three properties, all measured: the failing migration's own transaction
	// rolls back, the run STOPS (so the migration after it does not run), and
	// the whole thing reports errors so the open fails.
	set := []Migration{
		{1, "f1", "fine", `CREATE TABLE fine (id INTEGER PRIMARY KEY);`},
		{2, "f2", "broken", `CREATE TABLE broken (id INTEGER PRIMARY KEY); SELECT not_a_function();`},
		{3, "f3", "never", `CREATE TABLE never_runs (id INTEGER PRIMARY KEY);`},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "broken.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	svc := migrations.NewMigrationService(g)
	svc.SetMaxRetries(1)
	for _, w := range migrationWorkers(g, set) {
		svc.Register(w)
	}
	diag := svc.RunAll(appctx.NewContext(context.Background()))
	if diag == nil || !diag.HasErrors() {
		t.Fatal("a failing migration reported no error, so an open would have succeeded against a half schema")
	}

	tables := map[string]bool{}
	rows, err := g.Raw(`SELECT name FROM sqlite_master WHERE type='table'`).Rows()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables[n] = true
	}
	rows.Close()

	if !tables["fine"] {
		t.Error("the migration before the failure did not survive; the runner is not committing per migration")
	}
	if tables["broken"] {
		t.Error("the failing migration's table survived, so its transaction did not roll back")
	}
	if tables["never_runs"] {
		t.Error("the migration after the failure ran, so a failure does not stop the run")
	}
}

func TestTheCheckConstraintsRejectEveryInvalidValue(t *testing.T) {
	// O5. A typo must fail at WRITE time, where the stack names the caller —
	// not at read time, where it names the reader.
	s := openTemp(t)
	sc := seedLoop(t, s, "checks", "/repo")
	st := seedStep(t, s, sc)

	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "an invalid question kind",
			sql:  `INSERT INTO question (loop_slug, component_root, kind, body, asked_at) VALUES (?,?,?,?,?)`,
			args: []any{sc.LoopSlug, sc.ComponentRoot, "clarifyy", "body", "now"},
		},
		{
			name: "an invalid cost_state",
			sql:  `UPDATE step SET cost_state = ? WHERE id = ?`,
			args: []any{"approximate", st.ID},
		},
		{
			name: "an invalid step status",
			sql:  `UPDATE step SET status = ? WHERE id = ?`,
			args: []any{"paused", st.ID},
		},
		{
			name: "an invalid chat_message author",
			sql:  `INSERT INTO chat_message (loop_slug, component_root, seq, author, kind, body, created_at) VALUES (?,?,?,?,?,?,?)`,
			args: []any{sc.LoopSlug, sc.ComponentRoot, 900, "robot", "note", "b", "now"},
		},
		{
			name: "an invalid chat_message kind",
			sql:  `INSERT INTO chat_message (loop_slug, component_root, seq, author, kind, body, created_at) VALUES (?,?,?,?,?,?,?)`,
			args: []any{sc.LoopSlug, sc.ComponentRoot, 901, "human", "shout", "b", "now"},
		},
		{
			// Every column but the named one carries a VALID value, so each case
			// fails on the CHECK it claims to test. 0016 tightened all four of
			// these lists, and a case that puts 'tool' in `type` while testing
			// `action` passes for the wrong reason.
			name: "an invalid policy action",
			sql:  `INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			args: []any{"p1", "tool_access", "agent", "maybe", "operator", "now", "now"},
		},
		{
			name: "an invalid policy type",
			sql:  `INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			args: []any{"p2", "filesystem", "agent", "deny", "operator", "now", "now"},
		},
		{
			name: "an invalid policy target_kind",
			sql:  `INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			args: []any{"p3", "tool_access", "organisation", "deny", "operator", "now", "now"},
		},
		{
			// The CHECK 0005 never had, and the one PL1 depends on: an authorship
			// value outside the closed four has to fail at write time.
			name: "an invalid policy source",
			sql:  `INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			args: []any{"p4", "tool_access", "agent", "deny", "test", "now", "now"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.w.Exec(tc.sql, tc.args...); err == nil {
				t.Fatalf("%s was accepted; the CHECK constraint is missing or wrong", tc.name)
			} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Errorf("%s failed, but not on the CHECK: %v", tc.name, err)
			}
		})
	}
}

func TestEveryValidValueIsAccepted(t *testing.T) {
	// The negative control for the CHECKs: a constraint that rejects
	// everything would pass the test above and break the store.
	s := openTemp(t)
	sc := seedLoop(t, s, "valid", "/repo")

	for _, kind := range QuestionKinds() {
		if _, err := s.Ask(sc, NewQuestion{Kind: kind, Body: "body for " + string(kind)}); err != nil {
			t.Errorf("the CHECK rejected the valid question kind %q: %v", kind, err)
		}
	}
	st := seedStep(t, s, sc)
	for _, status := range StepStatuses() {
		if err := s.SetStepStatus(st.ID, status, ""); err != nil {
			t.Errorf("the CHECK rejected the valid step status %q: %v", status, err)
		}
	}
	for _, action := range []string{"allow", "deny"} {
		if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, "ok-action-"+action, "tool_access", "agent", action, "operator", "now", "now"); err != nil {
			t.Errorf("the CHECK rejected the valid policy action %q: %v", action, err)
		}
	}
	// 0016 added the `source` CHECK 0005 never had. PL1 reads the authorship off
	// it, so all four have to be writable — and the negative control is exactly
	// where a CHECK that rejects everything would otherwise go unnoticed.
	for _, source := range []string{"operator", "agent", "mcp_server", "import"} {
		if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, "ok-source-"+source, "cost_cap", "loop", "deny", source, "now", "now"); err != nil {
			t.Errorf("the CHECK rejected the valid policy source %q: %v", source, err)
		}
	}
}

func TestAPolicyWithANullNotAfterRoundTripsAsNoExpiry(t *testing.T) {
	// `not_after` is NULLABLE and null means NO EXPIRY — not a sentinel date.
	// A far-future sentinel is a policy that silently lapses on a day nobody
	// chose, which is the worst kind of expiry.
	s := openTemp(t)
	if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, action, source, created_at, updated_at)
		VALUES ('no-expiry','tool_access','all-agents','deny','operator','now','now')`); err != nil {
		t.Fatal(err)
	}
	var notAfter sql.NullString
	if err := s.r.QueryRow(`SELECT not_after FROM policy WHERE id = 'no-expiry'`).Scan(&notAfter); err != nil {
		t.Fatal(err)
	}
	if notAfter.Valid {
		t.Errorf("not_after came back as %q; a policy with no expiry must store NULL, never a sentinel date",
			notAfter.String)
	}
}

// ── 0016 — the rebuild to the closed vocabulary ──────────────────────────────
//
// S4 wrote `policy`'s CHECK vocabulary before anyone had chosen one; 0016
// replaces it. These are the tests that say so — and they read the DDL's OWN
// value lists rather than a copy held here, so a CHECK edited without a test
// edit fails instead of agreeing with itself. The other half of that lock
// (these values ⇄ `internal/policy/types.go`) is T2's.
//
// Amended by T18 (loop aeon-s10-policy): 0018 rebuilds this table AGAIN — the
// shipped 0016 is frozen and its const is now the shape of a table that no
// longer exists. A test reading `migration0016` would pin a dead DDL and miss
// a CHECK edited into the live one, so the vocabulary assertions read the
// STORED CREATE out of the catalogue (`livePolicyDDL` below): what the database
// enforces, not what a migration once proposed.

// The closed sets, in the wire form the CHECK stores. The design names the types
// `ToolAccess` … `TriggerAccess` and the targets `all-agents` … `mcp`
// (docs/aeon/S10-policy/design.md); these are those, spelled as rows.
var (
	policyTypes       = []string{"tool_access", "command_access", "route_access", "effect_class", "cost_cap", "skill_access", "trigger_access"}
	policyTargetKinds = []string{"all-agents", "agent", "loop", "lane-type", "mcp"}
	policyActions     = []string{"allow", "deny", "ask"}
	policySources     = []string{"operator", "agent", "mcp_server", "import"}
)

func TestTheRebuiltCHECKAcceptsExactlyTheSevenTypes(t *testing.T) {
	// PL3 — the type set is CLOSED. Seven values are writable and an eighth is
	// refused by the DATABASE, not by a validator somebody might later forget to
	// call. The failure a type CHECK prevents is the worst one this table can
	// have: a policy that matches nothing reading as a policy that permits
	// everything.
	s := openTemp(t)

	// Read off the LIVE definition (0018's shape is the one the database
	// enforces; migration0016's const is the shape of a table that no longer
	// exists).
	if got := checkValues(t, livePolicyDDL(t, s), "type"); !equalStrings(got, policyTypes) {
		t.Errorf("the live policy table's type CHECK is %v, want the seven %v", got, policyTypes)
	}

	for _, typ := range policyTypes {
		if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, target_id, action, source, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)`, "type-"+typ, typ, "agent", "golang-engineer", "deny", "operator", "now", "now"); err != nil {
			t.Errorf("the rebuilt CHECK rejected the valid policy type %q: %v", typ, err)
		}
	}

	// The eighth: a plausible type nobody in the design chose, which is exactly
	// the write the CHECK exists to refuse.
	if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, target_id, action, source, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, "type-chat_log", "chat_log", "agent", "golang-engineer", "deny", "operator", "now", "now"); err == nil {
		t.Fatal("an eighth policy type was accepted, so the CHECK does not close the set (PL3)")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the eighth type failed, but not on the CHECK: %v", err)
	}
}

func TestTheRebuiltCHECKAcceptsExactlyTheFiveTargetKinds(t *testing.T) {
	// The five targets, including `lane-type` — the one that governs by role
	// rather than identity ("no shell in any review lane") and so the one a
	// policy can survive its agent being renamed for.
	s := openTemp(t)

	// Read off the LIVE definition — see the note on the seven-types test.
	if got := checkValues(t, livePolicyDDL(t, s), "target_kind"); !equalStrings(got, policyTargetKinds) {
		t.Errorf("the live policy table's target_kind CHECK is %v, want the five %v", got, policyTargetKinds)
	}

	for _, kind := range policyTargetKinds {
		// `all-agents` is the kind the evaluation answers with NO id, and
		// `target_id` is nullable precisely so it can be NULL rather than a
		// sentinel ''. Every other kind names the thing it governs.
		var targetID any = kind + ":" + kind + "-subject"
		if kind == "all-agents" {
			targetID = nil
		}
		if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, target_id, action, source, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)`, "kind-"+kind, "tool_access", kind, targetID, "deny", "operator", "now", "now"); err != nil {
			t.Errorf("the rebuilt CHECK rejected the valid target kind %q: %v", kind, err)
		}
	}

	var stored sql.NullString
	if err := s.r.QueryRow(`SELECT target_id FROM policy WHERE id = 'kind-all-agents'`).Scan(&stored); err != nil {
		t.Fatalf("read back the all-agents row: %v", err)
	}
	if stored.Valid {
		t.Errorf("all-agents stored target_id %q; the kind means every agent, so it is NULL, never a sentinel",
			stored.String)
	}

	// The sixth — and a pointed choice: `global` is the value 0005 allowed and
	// 0016 refuses. It is the drift being corrected, named in the direction it
	// was corrected.
	if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, target_id, action, source, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, "kind-global", "tool_access", "global", "", "deny", "operator", "now", "now"); err == nil {
		t.Fatal("a sixth target kind was accepted, so the CHECK does not close the set (PL3)")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the sixth target kind failed, but not on the CHECK: %v", err)
	}
}

func TestTheRebuildBeginsOnZeroPreExistingPolicyRows(t *testing.T) {
	// 0016 moves the table's rows by hand, so it is entitled to depend on there
	// being none — but an assumption a migration depends on is an assumption it
	// has to MEASURE, not inherit from a doc comment or from STATUS.md. This
	// counts the rows on a real 0015-vintage database, in the instant before the
	// swap, and again after it.
	//
	// Both halves need the table to EXIST, not merely to be absent: a missing
	// table counts zero too and proves nothing, so countTable fatals rather than
	// returning zero.
	path := filepath.Join(t.TempDir(), "at-0015.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	if len(all) != frozenMigrationCount() {
		// The count moved six times ON PURPOSE and did not drift: T1's ruling
		// R1 added 0016_policy_governance (the slice's migration), the
		// deferred-findings gate added 0018_policy_authorship_net (SEC-022(a)
		// + DEF-027), S11 added 0019_policy_action_ask_and_loop_attended
		// (D1's widened action CHECK and D2's loop.attended, carried in one
		// numbered entry so this lock moved once), 0020_step_pause_reason
		// arrived for S9's stop/continue, unrelated to policy,
		// 0021_step_steer arrived for S31's durable steer queue (D4), equally
		// unrelated to policy, and 0022_agentsvc_tasks arrived for S14's Task
		// rows (AS4/AS5), equally unrelated to policy. A seventh number
		// appearing here means somebody added an entry without amending this
		// pin.
		// 2026-11-02 (S62 T3): the count is COMPUTED (frozen legacy block of 45
		// numbered entries + the timestamped files under migrations/, whose
		// newest is now 20261102085151 — the append-only backup_run ledger).
		// This message is wording, not the pin; the pin is the call.
		t.Fatalf("Migrations() has %d entries, want frozenMigrationCount() = 45 frozen + the timestamped files (0016 by ruling R1, 0018 by the deferred-findings "+
			"gate, 0019 by S11's D1/D2, 0020 by S9's stop/continue, 0021 by S31's steer queue, "+
			"0022 by S14's agent_task/agent_chain, 0023 by S15's plugin states, 0024 by S15's refused Tasks and refusal ledger, "+
			"0025 by S16's action_intent, 0026 by model management metadata, 0027 by S18's provenance and "+
			"0028 by S18's wire capabilities and no-price declaration and 0029 by S18 T3's route_probe probe cache, 0030 by S18 T5's step thinking-drop count, 0031 by S19 T6's loop allow_outward, 0032 by S20 T1's trigger tables, 0033 by S20 T2's fire inputs and loop provenance, 0034 by S20 T4's trigger_gap, 0035 by S38 T1's OL1 rename — project to component, "+
			"and the new project tier with its implicit backfill, 0036 by S38 T5's records tier — goal, epic, brief, "+
			"review, attachment, task_loop, goal_component, loop's goal columns, and task's surrogate-id rebuild, 0037 by S39's loop_run + step.run_seq (CR2: runs without re-keying the pin), 0038 by S39's chat_message.source + step_steer.source (CR6: provenance, by ALTER, no rebuilds), 0039 by S40's automation outbox, cursor, event triggers and fire ledger beside S20's, loop.automation_depth, chat_message.source_ref, and the loop_run CHECK widened for 'event' (AU4/AU5/AU7), 0040 by S22 T1's retrieval index — retrieval_corpus, retrieval_chunk and FTS5; the vec0 table is created on demand because its dimension is data, 0041 by S24 T1's loop queue — the component's queue_advance and sizing_mode by plain ALTER and queue_item with its seven-value status CHECK, 0042 by S35 T1's proposal table — the inert row no guard read path may ever learn the name of, 0043 by S26 T1's roadmap_item — the forward Gantt row with its eight-value status, three-state priority and provenance CHECKs and the estimate CHECK shared verbatim with 0041's size, 0044 by S29 T1's triage row — bug, the validation_state and priority_state as TWO separate closed sets (D2: priority's shape copied verbatim from 0043), with the label-on-unknown CHECK the same sentence 0043 wrote; bug_vec is created on demand because its dimension is data, not in the migration)", len(all))
	}
	prior := all[:15]
	if last := prior[len(prior)-1]; last.Name != "0015_recovery_costs" {
		t.Fatalf("rebuilding from %s does not test 0016's entry point; want 0015_recovery_costs", last.Name)
	}
	if rebuild := all[15]; rebuild.Name != "0016_policy_governance" {
		t.Fatalf("the policy migration is %s, want 0016_policy_governance", rebuild.Name)
	}

	applyMigrations(t, g, prior)

	// Before the swap: the table is there, and it holds nothing. This is the
	// assertion the frozen snapshot's seeded policy row used to stand in for, and
	// could not be — a seeded row makes the precondition false, not untested.
	if n := countTable(t, g, "policy"); n != 0 {
		t.Fatalf("a 0015-vintage database holds %d policy rows; the rebuild's copy step assumes zero", n)
	}
	// And it is the OLD shape at this point, so that "the count is still zero
	// afterwards" cannot be satisfied by 0016 having done nothing at all.
	before := policyTableSQL(t, g)
	if !strings.Contains(before, "'tool'") || strings.Contains(before, "tool_access") {
		t.Fatalf("the 0015-vintage policy table is not the 0005 shape, so this test proves nothing: %s", before)
	}

	applyMigrations(t, g, all[15:16])

	if n := countTable(t, g, "policy"); n != 0 {
		t.Errorf("the rebuilt policy table holds %d rows; the copy was guarded against moving any", n)
	}
	after := policyTableSQL(t, g)
	if strings.Contains(after, "'tool'") || !strings.Contains(after, "tool_access") {
		t.Errorf("the table was not rebuilt — its CHECK still carries the 0005 vocabulary: %s", after)
	}
	// The lookup comes back with the table, under the same name: 0005's index went
	// with the DROP, and T8's EXPLAIN reads through this one.
	if got := countCatalogueEntries(t, g, "index", "idx_policy_target"); got != 1 {
		t.Errorf("idx_policy_target exists %d times after the rebuild, want 1", got)
	}
}

func TestTheRebuildRefusesANonEmptyPolicyTable(t *testing.T) {
	// The guard is the difference between "provably empty" and "we hoped".
	//
	// The copy step is written so a database with rows moves none of them, and
	// then `DROP TABLE` deletes them. For an ordinary table that is a bug; for
	// the table that records what the system was ALLOWED to do it is a security
	// control vanishing with no error to read it by. So the precondition is a
	// CONSTRAINT, and this test puts a row in to prove the upgrade refuses.
	path := filepath.Join(t.TempDir(), "not-empty.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	applyMigrations(t, g, all[:15])

	// In the vocabulary 0005 allowed, which is the whole point: this is a row
	// somebody could legitimately have written before 0016 existed.
	if err := g.Exec(`INSERT INTO policy (id, type, target_kind, action, spec, source, created_at, updated_at)
		VALUES ('rogue','tool','global','deny','{}','operator','t','t')`).Error; err != nil {
		t.Fatalf("seed a policy at the 0015 vocabulary: %v", err)
	}
	if n := countTable(t, g, "policy"); n != 1 {
		t.Fatalf("seeded %d policy rows, want 1 — the test needs a non-empty table", n)
	}

	if summary := runMigrations(t, g, all[15:16]); summary == "" {
		t.Fatal("0016 applied to a non-empty policy table; it was supposed to refuse the upgrade")
	} else if !strings.Contains(summary, "CHECK constraint failed") {
		t.Errorf("0016 failed, but not on the guard's CHECK: %s", summary)
	}

	// Refused means ROLLED BACK (D5): the rows survive, in the table they were in,
	// and the rebuild leaves no half-built debris behind.
	if n := countTable(t, g, "policy"); n != 1 {
		t.Errorf("the rogue policy row did not survive the refused upgrade: %d rows, want 1", n)
	}
	if sql := policyTableSQL(t, g); !strings.Contains(sql, "'tool'") {
		t.Errorf("the policy table was replaced by a refused rebuild: %s", sql)
	}
	for _, leftover := range []string{"policy_new", "policy_rebuild_guard"} {
		if n := countCatalogueEntries(t, g, "table", leftover); n != 0 {
			t.Errorf("the refused rebuild left %s behind (%d catalogue entries)", leftover, n)
		}
	}
	if n := countCatalogueEntries(t, g, "index", "idx_policy_target"); n != 1 {
		t.Errorf("0005's idx_policy_target did not survive the refused rebuild (found %d, want 1)", n)
	}
}

func TestTheRebuildLeavesTheAcceptanceAndEventTablesAlone(t *testing.T) {
	// Legal constraint 2: 0016 changes `policy` and nothing else.
	//
	// The rebuild DROPS the parent table and renames another into its place, so
	// the risk is not to `policy` — it is to the two tables that point at it by
	// name and were never touched. Their FK clauses resolve at query time, which
	// is what makes them survive; this asserts they survived pointing at the
	// table that came back.
	s := openTemp(t)

	if got, want := tableColumns(t, s, "policy_acceptance"),
		[]string{"policy_id", "target_kind", "target_id", "accepted_by", "accepted_at"}; !equalStrings(got, want) {
		t.Errorf("policy_acceptance holds %v, want exactly %v — no content column may enter either table", got, want)
	}
	if got := tableColumns(t, s, "policy_event"); !containsString(got, "detail") {
		t.Errorf("policy_event has lost its detail column; %v", got)
	}

	// The proof rather than the inspection: foreign keys are enforced on these
	// connections, so a row in each child table that references a policy written
	// AFTER the rebuild only lands if the clause points at the live table.
	if _, err := s.w.Exec(`INSERT INTO policy (id, type, target_kind, target_id, action, source, created_at, updated_at)
		VALUES ('parent','tool_access','lane-type','review','deny','operator','now','now')`); err != nil {
		t.Fatalf("write the parent row: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO policy_acceptance (policy_id, target_kind, target_id, accepted_by, accepted_at)
		VALUES ('parent','lane-type','review','operator','now')`); err != nil {
		t.Errorf("policy_acceptance no longer references the rebuilt policy table: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO policy_event (policy_id, subject_kind, subject_id, outcome, detail, at)
		VALUES ('parent','tool','bash','denied','refused: bash · ToolAccess "no-shell-in-review"','now')`); err != nil {
		t.Errorf("policy_event no longer references the rebuilt policy table: %v", err)
	}
	// And enforcement is real, not assumed — otherwise the two inserts above prove
	// nothing and the FK clause could have been silently severed by the rename.
	if _, err := s.w.Exec(`INSERT INTO policy_acceptance (policy_id, target_kind, target_id, accepted_by, accepted_at)
		VALUES ('no-such-policy','lane-type','review','operator','now')`); err == nil {
		t.Error("a policy_acceptance row referencing no policy was accepted, so foreign keys are OFF here " +
			"and the two inserts above proved nothing")
	}
}

// ── 0018 — the authorship net and the carried children ───────────────────────
//
// The deferred-findings migration (SEC-022(a) + DEF-027). These tests are the
// two halves its header argues for: the pair CHECK the four vocabulary CHECKs
// cannot express, and the rebuild that must not fire its children's ON DELETE
// actions while the parent is swapped.

func TestTheAuthorshipCHECKRefusesAnAllowFromANonOperatorSource(t *testing.T) {
	// SEC-022(a): the net under PL1. The four vocabulary CHECKs close each
	// column ALONE, and `allow` × `agent` is legal against every one of them —
	// before 0018 an allow written by a non-operator was valid SQL, and PL1's
	// refusal was convention (that was the finding: reproduced with an on-disk
	// row, a forged manifest, and the frozen-manifest path). Now the DATABASE
	// refuses the pair. A failure that is not the CHECK is not the assertion,
	// so every case reads the error text (the discipline of
	// TestTheCheckConstraintsRejectEveryInvalidValue).
	s := openTemp(t)

	// The legal direction of the pair FIRST — a CHECK that refuses everything
	// would pass the refusals below and break the store.
	if _, err := s.w.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		VALUES ('allow-operator','tool_access','agent','golang-engineer','allow','{}','operator',NULL,'now','now')`); err != nil {
		t.Errorf("the pair CHECK refused the legal authorship allow/operator: %v", err)
	}

	// Every non-operator source trying to GRANT: refused, and refused on the
	// CHECK.
	for _, src := range []string{"agent", "mcp_server", "import"} {
		_, err := s.w.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,NULL,?,?)`, "allow-"+src, "tool_access", "agent", "golang-engineer",
			"allow", "{}", src, "now", "now")
		if err == nil {
			t.Fatalf("an allow from source %q was accepted; SEC-022(a) is not closed at the database", src)
		}
		if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("allow/%s failed, but not on the CHECK: %v", src, err)
		}
	}

	// And the CHECK is about the PAIR, not a ban on sources: deny lands from all
	// four. (Same rows, same shape, one word different — the discriminating
	// half.)
	for _, src := range []string{"operator", "agent", "mcp_server", "import"} {
		if _, err := s.w.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,NULL,?,?)`, "deny-"+src, "tool_access", "agent", "golang-engineer",
			"deny", "{}", src, "now", "now"); err != nil {
			t.Errorf("the pair CHECK refused the legal deny/%s: %v", src, err)
		}
	}

	// What landed: the five legal rows of the eight attempts, and not one of
	// the three forged grants.
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM policy`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("%d rows survived of 8 attempts (5 legal, 3 illegal)", n)
	}
}

func TestTheRebuiltCHECKStillClosesTheFourVocabularies(t *testing.T) {
	// 0016 closed the four vocabularies; 0018 rebuilt the same table and had to
	// carry all four while adding the pair. This reads EVERY CHECK the live
	// table declares — the four vocabulary lists and the pair — out of the
	// stored CREATE, not out of either migration const. The vocabulary bodies
	// are composed from the same policyTypes/…/policySources lists the other
	// tests pin, so a CHECK edited without a test edit fails here: that lock
	// is the whole point of reading the DDL instead of a copy. The COUNT is
	// asserted too: exactly five CHECK bodies. One vanishing or a sixth
	// arriving without a test here fails rather than passing unobserved.
	s := openTemp(t)
	live := livePolicyDDL(t, s)

	for _, c := range []struct {
		column string
		want   []string
	}{
		{"type", policyTypes},
		{"target_kind", policyTargetKinds},
		{"action", policyActions},
		{"source", policySources},
	} {
		if got := checkValues(t, live, c.column); !equalStrings(got, c.want) {
			t.Errorf("the live policy table's %q CHECK is %v, want %v", c.column, got, c.want)
		}
	}

	vocab := func(column string, values []string) string {
		return column + " IN ('" + strings.Join(values, "','") + "')"
	}
	want := []string{
		vocab("type", policyTypes),
		vocab("target_kind", policyTargetKinds),
		vocab("action", policyActions),
		vocab("source", policySources),
		"action <> 'allow' OR source = 'operator'",
	}
	sort.Strings(want)
	if got := checks(live); !equalStrings(got, want) {
		t.Errorf("the live policy table declares a different CHECK set than the four vocabularies plus the pair:\n got %v\nwant %v", got, want)
	}
}

func TestTheRebuildCarriesRowsAndLeavesTheChildrenIntact(t *testing.T) {
	// THE plant. 0016 could drop its parent because the table was provably
	// empty; 0018 ships while S10's writer is live, so real installs hold
	// governance rows AND two child tables reference them — acceptance
	// ON DELETE CASCADE, events ON DELETE SET NULL, and PRAGMA foreign_keys=OFF
	// unreachable inside the migration's transaction (both measured, both kept
	// true by the two tests below). So the rebuild parks the children, and this
	// test requires the family back ROW FOR ROW: parent values identical
	// (NULL-ness included — p-wide carries the two nullable columns), acceptance
	// rows identical (none cascaded away), audit rows with their AUTOINCREMENT
	// ids, FK links, details and instants intact (nothing NULLed).
	path := filepath.Join(t.TempDir(), "carried.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	prior, step := all[:17], all[17:18]
	if last := prior[len(prior)-1]; last.Name != "0017_project_icon" {
		t.Fatalf("building to %s does not test 0018's entry point; want 0017_project_icon", last.Name)
	}
	if m := step[0]; m.ID != 18 || m.Name != "0018_policy_authorship_net" {
		t.Fatalf("the findings migration is %d/%q, want 18/0018_policy_authorship_net", m.ID, m.Name)
	}
	applyMigrations(t, g, prior)
	plantPolicyFamily(t, g)

	// The pre-state must be the shape the test claims: 0016's policy (no pair
	// CHECK) and no acceptance index. Otherwise "everything survived" could be
	// satisfied by 0018 having done nothing at all.
	if sqlText := policyTableSQL(t, g); strings.Contains(sqlText, "action <> 'allow'") {
		t.Fatal("the 0017-vintage policy table already carries the pair CHECK; this test would pass without 0018")
	}
	if n := countCatalogueEntries(t, g, "index", "idx_policy_acceptance_target"); n != 0 {
		t.Fatalf("idx_policy_acceptance_target exists on the 0017-vintage database (%d); the before-plan "+
			"below and the after-count would not be measuring what 0018 adds", n)
	}
	// The BEFORE plan, measured for the record (quoted in
	// docs/aeon/S10-policy/tasks/T1.md's Notes block): the per-target read has
	// only the PRIMARY KEY's auto-index to walk — DEF-027's full scan.
	t.Logf("EQP acceptance read BEFORE 0018: %v", eqpRaw(t, g, listAcceptanceSQL, "lane-type", "review"))

	parentBefore := readPolicyRowDump(t, g)
	acceptanceBefore := readAcceptanceRowDump(t, g)
	eventsBefore := readEventRowDump(t, g)
	if len(parentBefore) != 2 || len(acceptanceBefore) != 1 || len(eventsBefore) != 2 {
		t.Fatalf("the plant is %d/%d/%d rows, want 2/1/2 — a carry test on an empty family proves nothing",
			len(parentBefore), len(acceptanceBefore), len(eventsBefore))
	}

	applyMigrations(t, g, step)

	// Every row comes back, row for row. Three separate comparisons because the
	// three failures are different crimes: a parent that changed is a governance
	// rule rewritten; an acceptance row that vanished is a CASCADE that fired;
	// an audit row whose id moved or whose policy_id went NULL is SET NULL —
	// the quiet one, since the row itself survives to read as intact.
	if got := readPolicyRowDump(t, g); !reflect.DeepEqual(got, parentBefore) {
		t.Errorf("the parent rows changed across the rebuild:\n before %+v\n  after %+v", parentBefore, got)
	}
	if got := readAcceptanceRowDump(t, g); !reflect.DeepEqual(got, acceptanceBefore) {
		t.Errorf("the acceptance rows changed across the rebuild (a CASCADE would have DELETED them):\n before %+v\n  after %+v", acceptanceBefore, got)
	}
	if got := readEventRowDump(t, g); !reflect.DeepEqual(got, eventsBefore) {
		t.Errorf("the audit rows changed across the rebuild (SET NULL would blank policy_id; a re-insert "+
			"without explicit ids would move the rowids nobody can then quote):\n before %+v\n  after %+v", eventsBefore, got)
	}

	// Integrity asserted, not assumed: no FK ended up dangling (the migration's
	// own fk_guard fires on this too — measured again from outside).
	var dangling int
	if err := g.Raw(`SELECT COUNT(*) FROM pragma_foreign_key_check()`).Scan(&dangling).Error; err != nil {
		t.Fatalf("pragma_foreign_key_check: %v", err)
	}
	if dangling != 0 {
		t.Errorf("%d dangling FK references after the rebuild", dangling)
	}

	// The park and both guards came home/dropped: a migrated database holds no
	// zz_0018_* table and no half-built debris.
	for _, scratch := range []string{"zz_0018_acceptance_park", "zz_0018_event_park", "policy_0018",
		"policy_0018_copy_guard", "policy_0018_fk_guard"} {
		if n := countCatalogueEntries(t, g, "table", scratch); n != 0 {
			t.Errorf("0018 left %s behind (%d catalogue entries)", scratch, n)
		}
	}

	// The lookup that goes with the DROP comes back with the rename, and the new
	// read-index lands — each exactly once.
	for _, idx := range []string{"idx_policy_target", "idx_policy_acceptance_target"} {
		if n := countCatalogueEntries(t, g, "index", idx); n != 1 {
			t.Errorf("%s exists %d times after 0018, want 1", idx, n)
		}
	}
	// The AFTER plan, same statement, same handle (Notes block quotes both).
	t.Logf("EQP acceptance read AFTER 0018: %v", eqpRaw(t, g, listAcceptanceSQL, "lane-type", "review"))

	// The children kept their columns: five and seven, no sixth under cover of
	// moving rows (legal constraints 1 and 2 — detail stays, no free text arrives).
	for _, c := range []struct {
		table string
		want  []string
	}{
		{"policy_acceptance", []string{"policy_id", "target_kind", "target_id", "accepted_by", "accepted_at"}},
		{"policy_event", []string{"id", "policy_id", "subject_kind", "subject_id", "outcome", "detail", "at"}},
	} {
		rows, err := g.Raw(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, c.table).Rows()
		if err != nil {
			t.Fatalf("read the columns of %s: %v", c.table, err)
		}
		var got []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				t.Fatalf("read the columns of %s: %v", c.table, err)
			}
			got = append(got, name)
		}
		rows.Close()
		if !equalStrings(got, c.want) {
			t.Errorf("%s holds %v after the rebuild, want exactly %v", c.table, got, c.want)
		}
	}

	// AUTOINCREMENT honesty: the sequence has not regressed below the rowids it
	// carries, so the next audit row appended after the upgrade cannot collide
	// with one that predates it.
	var seq int
	if err := g.Raw(`SELECT seq FROM sqlite_sequence WHERE name = 'policy_event'`).Scan(&seq).Error; err != nil {
		t.Fatalf("read sqlite_sequence for policy_event: %v", err)
	}
	if seq < 11 {
		t.Errorf("sqlite_sequence sits at %d below the carried rowids (max 11); the next audit row would be "+
			"offered an id that a carried row already holds", seq)
	}
}

func TestAVersion0017InstallUpgradesThroughOpenWithRowsIntact(t *testing.T) {
	// The carry test's twin on the REAL path: TestAPriorVersionDatabaseUpgradesWithRowsIntact
	// walks the frozen 0005 snapshot (whose policy table 0016 refuses to carry —
	// by design, no 0005-vocabulary row can survive the closed CHECKs), so the
	// policy rows' upgrade is measured here instead. A database built at 0017
	// with the planted family is the exact shape a live install holds; Open() is
	// the exact path the new binary takes, runner and ledger included. The rows
	// must survive THAT door too — not just the one-migration apply the carry
	// test exercises.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	func() {
		g, closeDB := rawGorm(t, path)
		defer closeDB()
		all := Migrations()
		applyMigrations(t, g, all[:17])
		plantPolicyFamily(t, g)
	}()
	parentBefore := func() []policyRowDump {
		g, closeDB := rawGorm(t, path)
		defer closeDB()
		return readPolicyRowDump(t, g)
	}()

	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the 0017-vintage store: %v", err)
	}
	defer s.Close()

	if got := readPolicyRowDump(t, s.gw); !reflect.DeepEqual(got, parentBefore) {
		t.Errorf("the parent rows changed across the Open()-driven upgrade:\n before %+v\n  after %+v", parentBefore, got)
	}
	if n := countTable(t, s.gw, "policy_acceptance"); n != 1 {
		t.Errorf("the acceptance row did not survive the Open()-driven upgrade (%d rows, want 1)", n)
	}
	if n := countTable(t, s.gw, "policy_event"); n != 2 {
		t.Errorf("the audit rows did not survive the Open()-driven upgrade (%d rows, want 2)", n)
	}
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(Migrations()) {
		t.Errorf("the ledger holds %d rows after the upgrade, want one per migration (%d)", len(applied), len(Migrations()))
	}
}

// ── 0019 — the ask widening and the attended column ──────────────────────────

func TestTheAskWideningRefusesAskBefore0019AndAdmitsItAfter(t *testing.T) {
	// D1, measured at the database door rather than argued from the Go side.
	// A store at 0018 cannot write ask — the CHECK closes the set at two, and
	// that is the shape a live install holds today. Walking it to 0019 must
	// admit the value AND carry every governance row across: the family is
	// planted before the step, and a row lost in the park-copy-restore fails
	// here as a changed dump (the 0018 carry test's standard, reused). The
	// net that must NOT move travels with the one that must: the pair CHECK
	// still refuses a forged allow, and a fourth value is still refused.
	path := filepath.Join(t.TempDir(), "at-0018.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	prior, step := all[:18], all[18:19]
	if last := prior[len(prior)-1]; last.Name != "0018_policy_authorship_net" {
		t.Fatalf("building to %s does not test 0019's entry point; want 0018_policy_authorship_net", last.Name)
	}
	if m := step[0]; m.ID != 19 || m.Name != "0019_policy_action_ask_and_loop_attended" {
		t.Fatalf("the S11 migration is %d/%q, want 19/0019_policy_action_ask_and_loop_attended", m.ID, m.Name)
	}
	applyMigrations(t, g, prior)
	plantPolicyFamily(t, g)

	// The pre-state must be the shape the test claims: two actions in the
	// live CHECK, so "0019 admits ask" measures a change and not a tautology.
	if sqlText := policyTableSQL(t, g); strings.Contains(sqlText, "'ask'") {
		t.Fatal("the 0018-vintage policy table already admits ask; this test would pass without 0019")
	}
	askRow := `INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		VALUES ('ask-before','command_access','agent','golang-engineer','ask','{}','operator',NULL,'now','now')`
	if err := g.Exec(askRow).Error; err == nil {
		t.Fatal("0018's table admitted an ask row; the widened CHECK would be comparing a shape that never refused it")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the pre-0019 ask failed, but not on the CHECK: %v", err)
	}

	parentBefore := readPolicyRowDump(t, g)
	acceptanceBefore := readAcceptanceRowDump(t, g)
	eventsBefore := readEventRowDump(t, g)
	if len(parentBefore) != 2 || len(acceptanceBefore) != 1 || len(eventsBefore) != 2 {
		t.Fatalf("the plant is %d/%d/%d rows, want 2/1/2 — a carry test on an empty family proves nothing",
			len(parentBefore), len(acceptanceBefore), len(eventsBefore))
	}

	applyMigrations(t, g, step)

	// Every row comes back, row for row — the parent unchanged (a governance
	// rule rewritten is the crime), the acceptance row not cascaded away, the
	// audit rows with their AUTOINCREMENT ids and FK links intact.
	if got := readPolicyRowDump(t, g); !reflect.DeepEqual(got, parentBefore) {
		t.Errorf("the parent rows changed across the ask rebuild:\n before %+v\n  after %+v", parentBefore, got)
	}
	if got := readAcceptanceRowDump(t, g); !reflect.DeepEqual(got, acceptanceBefore) {
		t.Errorf("the acceptance rows changed across the ask rebuild (a CASCADE would have DELETED them):\n before %+v\n  after %+v", acceptanceBefore, got)
	}
	if got := readEventRowDump(t, g); !reflect.DeepEqual(got, eventsBefore) {
		t.Errorf("the audit rows changed across the ask rebuild (SET NULL would blank policy_id; a re-insert "+
			"without explicit ids would move the rowids nobody can quote):\n before %+v\n  after %+v", eventsBefore, got)
	}

	// No half-built debris, and both lookups came back exactly once — the
	// fingerprint compares indexes by name and shape, and T8's EXPLAIN reads
	// through them.
	for _, scratch := range []string{"zz_0019_acceptance_park", "zz_0019_event_park", "policy_0019",
		"policy_0019_copy_guard", "policy_0019_fk_guard"} {
		if n := countCatalogueEntries(t, g, "table", scratch); n != 0 {
			t.Errorf("0019 left %s behind (%d catalogue entries)", scratch, n)
		}
	}
	for _, idx := range []string{"idx_policy_target", "idx_policy_acceptance_target"} {
		if n := countCatalogueEntries(t, g, "index", idx); n != 1 {
			t.Errorf("%s exists %d times after 0019, want 1", idx, n)
		}
	}
	var dangling int
	if err := g.Raw(`SELECT COUNT(*) FROM pragma_foreign_key_check()`).Scan(&dangling).Error; err != nil {
		t.Fatalf("pragma_foreign_key_check: %v", err)
	}
	if dangling != 0 {
		t.Errorf("%d dangling FK references after the ask rebuild", dangling)
	}

	// After: the live CHECK is the widened one, and the value is admitted —
	// from a NON-operator source too, because ask is deny-like at the
	// boundary (D1: it takes autonomy away, it never widens permissions).
	live := policyTableSQL(t, g)
	if !strings.Contains(live, "action IN ('allow','deny','ask')") {
		t.Errorf("the live policy table after 0019 is not the widened one:\n%s", live)
	}
	if err := g.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		VALUES ('ask-after','command_access','agent','golang-engineer','ask','{}','agent',NULL,'now','now')`).Error; err != nil {
		t.Errorf("the widened table refused an ask written by an agent (deny-like, any source): %v", err)
	}

	// And the two nets that must NOT have moved: the fourth value is still
	// refused, and the pair CHECK still refuses a forged allow — carried
	// verbatim, not rewritten along with the widening.
	if err := g.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		VALUES ('fourth','command_access','agent','golang-engineer','maybe','{}','operator',NULL,'now','now')`).Error; err == nil {
		t.Fatal("a fourth action was accepted; ask joined the set, it did not open it (PL3)")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the fourth action failed, but not on the CHECK: %v", err)
	}
	if err := g.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		VALUES ('still-forged','command_access','agent','golang-engineer','allow','{}','agent',NULL,'now','now')`).Error; err == nil {
		t.Fatal("the pair CHECK stopped holding: an allow from a non-operator is still illegal SQL (SEC-022(a))")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the forged allow failed, but not on the CHECK: %v", err)
	}
}

func TestLoopAttendedArrivesNullableAndReadsNeverDeclared(t *testing.T) {
	// D2, with P31's three states measured rather than promised. A loop row
	// written before 0019 — a pre-S11 install's row — must read back as NULL
	// ("never declared"), NOT as 0 ("declared unattended"): a migration that
	// backfilled a decision nobody made would collapse the third state on
	// arrival, and the pin UI would tell the operator "you chose unattended"
	// about a loop that predates the question. Nullable is the whole design,
	// so the column's notnull flag and the pre-existing row's NULL are both
	// asserted.
	path := filepath.Join(t.TempDir(), "pre-s11.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	applyMigrations(t, g, all[:18])

	// The pre-state: no attended column at all on the 0018-vintage loop.
	var before int
	if err := g.Raw(`SELECT COUNT(*) FROM pragma_table_info('loop') WHERE name = 'attended'`).Scan(&before).Error; err != nil {
		t.Fatalf("read loop's columns before 0019: %v", err)
	}
	if before != 0 {
		t.Fatalf("loop already holds attended before 0019 (%d); this test would pass without the migration", before)
	}

	// A real pre-S11 loop row, written through the same column list the
	// frozen-snapshot seed uses.
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,project_root,mode,phase,status,started_at,updated_at)
		VALUES ('l-old','pre','Pre S11','a goal','/repo','/repo','standard','build','active','t','t')`).Error; err != nil {
		t.Fatalf("plant the pre-S11 loop: %v", err)
	}

	applyMigrations(t, g, all[18:19])

	// The column exists, is INTEGER, and is NULLABLE — SQLite refuses a NOT
	// NULL ADD COLUMN without a default, and no default was invented either:
	// a default here would be a decision nobody made, written onto every row.
	type colInfo struct {
		Name    string `gorm:"column:name"`
		Type    string `gorm:"column:type"`
		NotNull int    `gorm:"column:notnull"`
	}
	var col colInfo
	// "notnull" is quoted because it is a SQLite keyword — it names the column
	// of pragma_table_info, and only appears bare in a column definition.
	if err := g.Raw(`SELECT name, type, "notnull" FROM pragma_table_info('loop') WHERE name = 'attended'`).Scan(&col).Error; err != nil {
		t.Fatalf("read the attended column: %v", err)
	}
	if col.Name != "attended" {
		t.Fatal("0019 did not add loop.attended")
	}
	if col.Type != "INTEGER" {
		t.Errorf("loop.attended is %q, want INTEGER", col.Type)
	}
	if col.NotNull != 0 {
		t.Error("loop.attended is NOT NULL; three-state honesty (P31) requires NULL to mean never-declared")
	}

	// The pre-S11 row reads back as never-declared. The 0-vs-NULL contrast is
	// the whole point, so a declared-unattended row is planted alongside to
	// make the difference observable.
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,project_root,mode,phase,status,started_at,updated_at,attended)
		VALUES ('l-new','post','Post S11','a goal','/repo','/repo','standard','build','active','t','t',0)`).Error; err != nil {
		t.Fatalf("plant the declared-unattended loop: %v", err)
	}
	var preNull int
	var postVal sql.NullInt64
	if err := g.Raw(`SELECT attended IS NULL FROM loop WHERE id = 'l-old'`).Scan(&preNull).Error; err != nil {
		t.Fatalf("read the pre-S11 row's attended: %v", err)
	}
	if preNull != 1 {
		t.Error("the pre-S11 loop's attended is NOT NULL; a migration backfilled a decision nobody made")
	}
	if err := g.Raw(`SELECT attended FROM loop WHERE id = 'l-new'`).Scan(&postVal).Error; err != nil {
		t.Fatalf("read the declared row's attended: %v", err)
	}
	if !postVal.Valid || postVal.Int64 != 0 {
		t.Errorf("the declared-unattended row reads %v; 0 must survive as itself, distinct from never-declared", postVal)
	}
}

func TestTheRebuildRefusesAForgedGrantFromBefore0018(t *testing.T) {
	// The net's other half: rows that CANNOT cross it. An `allow` written by a
	// non-operator was valid SQL until this migration (that was SEC-022), so a
	// store could exist holding one. The copy is an INSERT…SELECT INTO the
	// pair-CHECKed table, so such a store must REFUSE the upgrade loudly (D5)
	// rather than carry the forged grant across or silently drop it — 0016's
	// guard set the precedent, and the recovery playbook is the same: read the
	// row out, decide what it means, delete it, re-open.
	path := filepath.Join(t.TempDir(), "forged.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	applyMigrations(t, g, all[:17])
	// Legal at 0017 — the four vocabulary CHECKs all pass. That is exactly the
	// finding, written as a row.
	if err := g.Exec(`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		VALUES ('self-grant','tool_access','agent','golang-engineer','allow','{}','agent',NULL,'now','now')`).Error; err != nil {
		t.Fatalf("plant the forged grant (it must be legal at 0017, or this test measures nothing): %v", err)
	}

	summary := runMigrations(t, g, all[17:18])
	if summary == "" {
		t.Fatal("0018 carried a forged allow/agent row across; the pair CHECK is not being enforced during the copy (SEC-022(a))")
	}
	if !strings.Contains(summary, "CHECK constraint failed") {
		t.Errorf("0018 failed, but not on a CHECK: %s", summary)
	}

	// Refused means ROLLED BACK (D5): the forged row survives in the OLD-shape
	// table (still no pair CHECK — the swap never happened), and the rebuild
	// leaves no half-built debris.
	if n := countTable(t, g, "policy"); n != 1 {
		t.Errorf("the forged row did not survive the refused upgrade: %d rows, want 1", n)
	}
	if sqlText := policyTableSQL(t, g); strings.Contains(sqlText, "action <> 'allow'") {
		t.Errorf("the policy table was replaced by a refused rebuild: %s", sqlText)
	}
	for _, leftover := range []string{"policy_0018", "policy_0018_copy_guard", "policy_0018_fk_guard",
		"zz_0018_acceptance_park", "zz_0018_event_park"} {
		if n := countCatalogueEntries(t, g, "table", leftover); n != 0 {
			t.Errorf("the refused rebuild left %s behind (%d catalogue entries)", leftover, n)
		}
	}
	if n := countCatalogueEntries(t, g, "index", "idx_policy_target"); n != 1 {
		t.Errorf("0016's idx_policy_target did not survive the refused rebuild (found %d, want 1)", n)
	}

	// And with the forged row gone, the very same migration applies cleanly —
	// the playbook the header promises (delete it, re-open) actually works.
	// (The runner re-attempts a `failed` ledger row — service.go only marks
	// status == "applied" as applied — so this second run really runs.)
	if err := g.Exec(`DELETE FROM policy WHERE id = 'self-grant'`).Error; err != nil {
		t.Fatal(err)
	}
	if summary := runMigrations(t, g, all[17:18]); summary != "" {
		t.Fatalf("0018 refused a store whose forged row was removed (the documented recovery path is "+
			"broken): %s", summary)
	}
	if sqlText := policyTableSQL(t, g); !strings.Contains(sqlText, "action <> 'allow'") {
		t.Errorf("0018 applied but the table lacks the pair CHECK: %s", sqlText)
	}
}

func TestTheNaiveDropParentWouldHaveDestroyedTheChildren(t *testing.T) {
	// The control that makes the parking real rather than ceremonial: the SAME
	// family as the carry test, planted at 0017, then the naive shape — a bare
	// `DROP TABLE policy`. Measured on this checkout (the numbers are quoted in
	// docs/aeon/S10-policy/tasks/T1.md's Notes block): the acceptance row is
	// taken by CASCADE, and every surviving audit row loses the rule it blames
	// to SET NULL — the row stays, detail and instant intact, which is exactly
	// what makes the failure quiet.
	//
	// This test asserts the DESTRUCTION. Two jobs: the carry test's hazard is
	// proven rather than asserted, and if SQLite ever stops firing FK actions
	// on DROP, this test fails and the migration's parking becomes known dead
	// weight somebody can remove with evidence — rather than an unverifiable
	// ritual nobody dares to question.
	path := filepath.Join(t.TempDir(), "naive.db")
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	all := Migrations()
	applyMigrations(t, g, all[:17])
	plantPolicyFamily(t, g)
	if n := countTable(t, g, "policy_acceptance"); n != 1 {
		t.Fatalf("the plant put %d acceptance rows down, want 1", n)
	}

	if err := g.Exec(`DROP TABLE policy`).Error; err != nil {
		t.Fatalf("the naive DROP TABLE policy failed, so this control cannot measure it: %v", err)
	}

	if n := countTable(t, g, "policy_acceptance"); n != 0 {
		t.Errorf("CASCADE did NOT delete the acceptance record (%d rows survived); the naive shape is not "+
			"the destroyer 0018's parking was written against — re-measure before trusting the parking", n)
	}
	// SET NULL keeps the rows and blanks the blame: both audit rows are still
	// there, both no longer name a policy.
	var total, blaming int
	if err := g.Raw(`SELECT COUNT(*), COUNT(policy_id) FROM policy_event`).Row().Scan(&total, &blaming); err != nil {
		t.Fatal(err)
	}
	if total != 2 || blaming != 0 {
		t.Errorf("the naive DROP left %d audit rows of which %d still name a policy; the measured "+
			"destruction was 2 rows with policy_id NULL (SET NULL fired, rows preserved)", total, blaming)
	}
}

func TestTheForeignKeysPragmaIsInertInsideAMigrationTransaction(t *testing.T) {
	// WHY parking, in one measurement. Each migration runs in exactly ONE
	// transaction (Migrations()'s header says so), and PRAGMA foreign_keys
	// cannot be changed inside one — SQLite's documented no-op, measured here
	// rather than quoted from memory: on the store's own connections (the DSN
	// sets _foreign_keys=1), setting the pragma OFF inside an open transaction
	// leaves it reading 1. So the DROP inside 0018's transaction WOULD fire the
	// children's ON DELETE actions whatever a migration "disables", and making
	// the drop safe by construction — park the children — is the only shape
	// that protects the audit. If this test ever fails, the no-op claim died
	// and the migration's justification must be re-audited.
	s := openTemp(t)

	var outside int
	if err := s.r.QueryRow(`PRAGMA foreign_keys`).Scan(&outside); err != nil {
		t.Fatal(err)
	}
	if outside != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d outside any transaction; these connections are not FK-enforcing "+
			"and the claim below cannot be measured here", outside)
	}

	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatalf("PRAGMA foreign_keys=OFF inside the transaction: %v", err)
	}
	var inside int
	if err := tx.QueryRow(`PRAGMA foreign_keys`).Scan(&inside); err != nil {
		t.Fatal(err)
	}
	if inside != 1 {
		t.Errorf("PRAGMA foreign_keys read %d inside the transaction AFTER being set OFF — the pragma is NOT "+
			"inert there, and 0018's parking argument (a migration cannot disable FKs) needs re-auditing", inside)
	}
}

func TestNothingInThisSliceReadsOrWritesThePolicyTables(t *testing.T) {
	// The policy tables are created early (2b) and S10 brings the evaluator.
	// If something here started using them, the shape would be set by whatever
	// that something needed rather than by the slice that owns the decision.
	//
	// Amended 2026-10-19 by S10's T6 (loop aeon-s10-policy, issue #83): the
	// guard's own condition arrived — S10 owns the decision now, and the seed
	// task names this one as "the first reader and writer" (docs/aeon/
	// S10-policy/tasks/T5.md). policy.go is exempted by name, the way
	// schema.go is: it is the SQL text T6 landed, and EVERYTHING ELSE in this
	// package is still forbidden from touching the tables, so the original
	// claim — shape set by the owner, not by a casual reader — stays enforced.
	for name, src := range packageFiles(t) {
		if name == "schema.go" {
			continue // where they are created
		}
		if name == "policy.go" {
			continue // S10's SQL text for `policy`, the sanctioned first reader/writer
		}
		if name == "agent.go" {
			continue // S43 T2/AM2 — the slug migration re-points agent-kind policy
			// targets from name to slug, and the AM4 deletion check reads
			// whether a policy still names the slug. Both are the one
			// sanctioned cross-slice touch of the policy table the design
			// authorises (AM2: the slug replaces the name as the reference
			// key), so the guard exempts this file the way it exempts
			// policy.go — the shape stays S10's, only the re-point is S43's.
		}
		if name == "policy_change.go" {
			continue // S45 T3/PM8 — the SQL text for the AUTHORING AUDIT.
			// What this guard matches is the substring of this file's own
			// table name ("FROM policy" inside "FROM policy_change"): the
			// file touches NO policy table — it appends to and reads its
			// own append-only table, deliberately SEPARATE from both
			// policy and policy_event (T0 item 3.3's verdict;
			// TestPolicyChangeNotTrimmedByRetention pins the separation,
			// TestEnforcementNeverReadsPolicyChange the blindness). The
			// policy shape stays S10's; exempted the way policy.go and
			// agent.go are, by name, with the reason written down.
		}
		for _, table := range []string{"FROM policy", "INTO policy", "UPDATE policy"} {
			if strings.Contains(src, table) {
				t.Errorf("%s touches the policy tables (%q); nothing outside S10's owner reads or writes them — "+
					"S10 brings the evaluator", name, table)
			}
		}
	}
}

func TestThereIsNoAwaitingUserColumn(t *testing.T) {
	// DB6, asserted against the catalogue rather than against the DDL file. A
	// reader arriving from v1 will look for it; it is not there, deliberately,
	// and the answer is derived (see chat.go's AwaitingSQL).
	s := openTemp(t)
	rows, err := s.r.Query(`
		SELECT m.name, p.name FROM sqlite_master m
		JOIN pragma_table_info(m.name) p
		WHERE m.type = 'table' AND p.name LIKE '%awaiting%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		_ = rows.Scan(&table, &column)
		t.Errorf("%s.%s exists; DB6 says whether a loop is waiting is DERIVED from `question`, because "+
			"a stored flag and an unanswered question can disagree and nothing says which is right",
			table, column)
	}
}

func TestEveryTableTheDesignNamesExists(t *testing.T) {
	// By ENUMERATION against the catalogue, not by reading schema.go — a
	// missing table in a migration that was never applied would read correctly
	// in the file.
	want := []string{
		// The catalogue under the tree names — `project` was the GROUP, now
		// DERIVED (a root with a child reads as a project, CT1), so the
		// `project` table is gone. `component` is what `project` always was
		// (one repo at one root).
		"component", "component_root", "loop", "loop_phase", "task",
		"step", "step_message", "event", "memory",
		"question", "chat_message",
		"loop_document", "loop_archive", "loop_archive_file",
		"policy", "policy_acceptance", "policy_event",
	}
	s := openTemp(t)
	have, err := s.Tables()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range want {
		if !containsString(have, table) {
			t.Errorf("the table %s does not exist; the catalogue holds %v", table, have)
		}
	}
	// And the framework's ledger, which is the one table Aeon did not write.
	if !containsString(have, "_migrations") {
		t.Error("_migrations does not exist, so the framework's ledger never ran")
	}
}

func TestTheHistoryQueryIndexesLandWithTheSchema(t *testing.T) {
	// 2a: S25's history view is a read-only projection over data this schema
	// already holds, and an index is free now and a migration on a live store
	// later. Asserted on the catalogue so a migration that dropped one is
	// caught here rather than by a slow query in Stage 5.
	s := openTemp(t)
	for _, index := range []string{"idx_loop_updated", "idx_step_loop", "idx_event_scope"} {
		var n int
		if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name = ?`, index).
			Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("the index %s does not exist; S25's history view reads through it", index)
		}
	}
}

// --- helpers for this file ---------------------------------------------------

// rawGorm opens a GORM handle on a fresh file with NO migrations, for the tests
// whose subject is the runner itself.
func rawGorm(t *testing.T, path string) (*gorm.DB, func()) {
	t.Helper()
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, composeDSN(path, DefaultBusyTimeout, true))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	db.SetMaxOpenConns(1)
	g, err := gorm.Open(database.GormDialector(db), database.GormConfig())
	if err != nil {
		_ = db.Close()
		t.Fatalf("wrap %s: %v", path, err)
	}
	return g, func() { _ = db.Close() }
}

// seedFrozen builds a database at the frozen prior version, with its ledger
// rows and a seeded row in every table the upgrade must preserve.
func seedFrozen(t *testing.T, path string) {
	t.Helper()
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	if err := g.Exec(frozenSchema0005).Error; err != nil {
		t.Fatalf("apply the frozen schema: %v", err)
	}

	// The ledger, exactly as a database of that vintage carries it. Built
	// through the framework's own service so the table's shape is its, not a
	// second guess at it.
	svc := migrations.NewMigrationService(g)
	_ = svc
	for _, name := range migrationsAppliedInFrozenSchema {
		if err := g.Exec(`INSERT INTO _migrations (id, name, description, applied_at, status)
			VALUES (?,?,?,?,'applied')`, newID(), name, "frozen", "2026-10-14T00:00:00.000Z").Error; err != nil {
			t.Fatalf("seed the ledger row for %s: %v", name, err)
		}
	}

	seed := []string{
		`INSERT INTO project (id,name,slug,created_at,updated_at) VALUES ('p','proj','proj','t','t')`,
		`INSERT INTO project_root (project_root,project_id,added_at) VALUES ('/repo','p','t')`,
		`INSERT INTO loop (id,slug,title,goal,root,project_root,mode,phase,status,started_at,updated_at)
		   VALUES ('l','old','Old','a goal','/repo','/repo','standard','build','active','t','t')`,
		`INSERT INTO loop_phase (loop_slug,project_root,phase,status,ord,updated_at)
		   VALUES ('old','/repo','build','done',1,'t')`,
		`INSERT INTO task (loop_slug,project_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('old','/repo','T1',1,'A task','go','eng','pending','t')`,
		`INSERT INTO step (loop_slug,project_root,phase,agent,status,started_at,cost_state)
		   VALUES ('old','/repo','build','eng','done','t','known')`,
		`INSERT INTO step_message (step_id,seq,format_version,role,content,created_at)
		   VALUES (1,1,1,'user','[{"kind":"text","text":"hello from the old version"}]','t')`,
		`INSERT INTO event (kind,payload,created_at) VALUES ('daemon.started','{}','t')`,
		`INSERT INTO memory (loop_slug,project_root,ts,note) VALUES ('old','/repo','t','a note')`,
		`INSERT INTO question (loop_slug,project_root,kind,body,asked_at)
		   VALUES ('old','/repo','gate','proceed?','t')`,
		`INSERT INTO chat_message (loop_slug,project_root,seq,author,kind,body,created_at)
		   VALUES ('old','/repo',1,'agent','question','proceed?','t')`,
		`INSERT INTO loop_document (id,project_root,loop_slug,file,rev,content,bytes,sha256,written_at)
		   VALUES ('d','/repo','old','plan.md',1,x'00',1,'deadbeef','t')`,
		`INSERT INTO loop_archive (id,slug,project_root,title,goal,status,archived_at,state,state_bytes,file_count,file_bytes)
		   VALUES ('a','gone','/repo','Gone','g','done','t',x'00',1,0,0)`,
		// No `policy` row, deliberately, though every other table here carries
		// one. This snapshot is seeded at the 0005 vocabulary and 0016 rebuilds
		// that table against a CLOSED one, so a row written here could not
		// survive the copy: `('pol','tool','global',…)` names a type and a target
		// the rebuilt CHECK refuses, and the migration's own guard
		// (`TestTheRebuildRefusesANonEmptyPolicyTable`) refuses the whole upgrade
		// rather than dropping it. The property "the rebuild begins on nothing" is
		// asserted positively by TestTheRebuildBeginsOnZeroPreExistingPolicyRows,
		// which is what a seeded row would have made unassertable.
	}
	for _, stmt := range seed {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("seed the frozen database: %v\n%s", err, stmt)
		}
	}
}

// upgradedStore builds a frozen database and walks it forward with the runner.
func upgradedStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	seedFrozen(t, path)
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("upgrade the frozen database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// runMigrations applies exactly the given set to a database that has none of
// them, and returns the runner's summary when it fails (empty means it did not).
//
// The raw handle rather than Open(), because these tests' subject is ONE
// migration: 0016 has to be applied to a database sitting at 0015 with nothing
// else pending, so that what it sees is attributable to it and to nothing else.
// The ledger makes a second call idempotent, which is what lets a test build the
// vintage in one call and apply the migration under test in the next.
func runMigrations(t *testing.T, g *gorm.DB, set []Migration) string {
	t.Helper()
	svc := migrations.NewMigrationService(g)
	svc.SetMaxRetries(1)
	for _, w := range migrationWorkers(g, set) {
		svc.Register(w)
	}
	if diag := svc.RunAll(appctx.NewContext(context.Background())); diag != nil && diag.HasErrors() {
		return diag.GetSummary()
	}
	return ""
}

func applyMigrations(t *testing.T, g *gorm.DB, set []Migration) {
	t.Helper()
	if summary := runMigrations(t, g, set); summary != "" {
		t.Fatalf("apply %d migrations: %s", len(set), summary)
	}
}

// countTable counts a table's rows, and FATALS if the table is not there. The
// distinction is the whole point of the zero-row assertions: an absent table
// counts as zero too, and would let a migration that did nothing pass a test
// about a migration that did something.
func countTable(t *testing.T, g *gorm.DB, table string) int {
	t.Helper()
	var n int
	if err := g.Raw(fmt.Sprintf(`SELECT count(*) FROM %q`, table)).Scan(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// countCatalogueEntries counts one named object in sqlite_master by kind — how a
// test says "this index exists, once" or "that scratch table is gone".
func countCatalogueEntries(t *testing.T, g *gorm.DB, kind, name string) int {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM sqlite_master WHERE type = ? AND name = ?`, kind, name).
		Scan(&n).Error; err != nil {
		t.Fatalf("count the %s %s: %v", kind, name, err)
	}
	return n
}

// policyTableSQL returns the stored CREATE for `policy`. The CHECKs live nowhere
// else — no pragma reports them, which is why fingerprint.go parses the text too
// — so this is how a test tells the 0005 shape from the rebuilt one.
func policyTableSQL(t *testing.T, g *gorm.DB) string {
	t.Helper()
	var create string
	if err := g.Raw(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='policy'`).
		Scan(&create).Error; err != nil {
		t.Fatalf("read the stored CREATE for policy: %v", err)
	}
	return create
}

// livePolicyDDL returns the stored CREATE for `policy` FROM A LIVE DATABASE —
// the CURRENT definition after every migration has run. Since 0018 that is no
// longer migration0016's text: the shipped 0016 is frozen and describes a table
// that no longer exists, so a vocabulary test reading the const would pin a
// dead DDL and miss a CHECK edited into the live one. Reading the catalogue is
// reading what the database enforces.
func livePolicyDDL(t *testing.T, s *Store) string {
	t.Helper()
	var create string
	if err := s.r.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='policy'`).
		Scan(&create); err != nil {
		t.Fatalf("read the stored CREATE for policy from the catalogue: %v", err)
	}
	if !strings.Contains(create, "CHECK") {
		t.Fatalf("the stored CREATE for policy carries no CHECK at all: %q", create)
	}
	return create
}

// ── 0018's plant family ──────────────────────────────────────────────────────
//
// The dumps are the rows AS STORED — every column, NULL-ness included — because
// "identical" must be able to fail on a single field. Field order matches the
// SELECT below, which fixes its own order by ORDER BY, so reflect.DeepEqual on
// the slices is a row-for-row comparison.

type policyRowDump struct {
	ID, Type, TargetKind, Action, Spec, Source, CreatedAt, UpdatedAt string
	TargetID, NotAfter                                               sql.NullString
}

type acceptanceRowDump struct {
	PolicyID, TargetKind, TargetID, AcceptedBy, AcceptedAt string
}

type eventRowDump struct {
	ID                              int64
	PolicyID                        sql.NullString
	SubjectKind, SubjectID, Outcome string
	Detail                          sql.NullString
	At                              string
}

// plantPolicyFamily writes the family 0018 must carry: two parents — one with
// a spec and an expiry, one all-agents with BOTH nullable columns NULL, so
// "identical" includes NULL-ness — one acceptance row keyed to
// ('lane-type','review'), and two audit rows on EXPLICIT rowids 7 and 11. Not
// 1 and 2: "the rowids are unchanged" must not be satisfiable by an accident of
// insertion order. The 'p-wide' parent is a deny from source 'agent' — legal
// before AND after the pair CHECK, which is half of what the carry proves.
func plantPolicyFamily(t *testing.T, g *gorm.DB) {
	t.Helper()
	plant := []string{
		`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		   VALUES ('p-keep','tool_access','lane-type','review','deny','{"toolName":"bash"}','operator',
		           '2027-01-01T00:00:00Z','2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z')`,
		`INSERT INTO policy (id,type,target_kind,target_id,action,spec,source,not_after,created_at,updated_at)
		   VALUES ('p-wide','tool_access','all-agents',NULL,'deny','{}','agent',NULL,
		           '2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z')`,
		`INSERT INTO policy_acceptance (policy_id,target_kind,target_id,accepted_by,accepted_at)
		   VALUES ('p-keep','lane-type','review','operator','2026-10-19T11:00:00.123456789Z')`,
		`INSERT INTO policy_event (id,policy_id,subject_kind,subject_id,outcome,detail,at)
		   VALUES (7,'p-keep','lane-type','review','refusal','refused: bash · ToolAccess "no-shell-in-review"','2026-10-19T09:00:00.000000000Z')`,
		`INSERT INTO policy_event (id,policy_id,subject_kind,subject_id,outcome,detail,at)
		   VALUES (11,'p-keep','agent','boss','disagreement','observed contradicts declared','2026-10-19T12:30:00.987654321Z')`,
	}
	for _, stmt := range plant {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("plant: %v\n%s", err, stmt)
		}
	}
}

func readPolicyRowDump(t *testing.T, g *gorm.DB) []policyRowDump {
	t.Helper()
	rows, err := g.Raw(`SELECT id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at
		FROM policy ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("read the policy rows: %v", err)
	}
	defer rows.Close()
	var out []policyRowDump
	for rows.Next() {
		var r policyRowDump
		if err := rows.Scan(&r.ID, &r.Type, &r.TargetKind, &r.TargetID, &r.Action, &r.Spec,
			&r.Source, &r.NotAfter, &r.CreatedAt, &r.UpdatedAt); err != nil {
			t.Fatalf("read the policy rows: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the policy rows: %v", err)
	}
	return out
}

func readAcceptanceRowDump(t *testing.T, g *gorm.DB) []acceptanceRowDump {
	t.Helper()
	rows, err := g.Raw(`SELECT policy_id, target_kind, target_id, accepted_by, accepted_at
		FROM policy_acceptance ORDER BY policy_id, target_kind, target_id`).Rows()
	if err != nil {
		t.Fatalf("read the acceptance rows: %v", err)
	}
	defer rows.Close()
	var out []acceptanceRowDump
	for rows.Next() {
		var r acceptanceRowDump
		if err := rows.Scan(&r.PolicyID, &r.TargetKind, &r.TargetID, &r.AcceptedBy, &r.AcceptedAt); err != nil {
			t.Fatalf("read the acceptance rows: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the acceptance rows: %v", err)
	}
	return out
}

func readEventRowDump(t *testing.T, g *gorm.DB) []eventRowDump {
	t.Helper()
	rows, err := g.Raw(`SELECT id, policy_id, subject_kind, subject_id, outcome, detail, at
		FROM policy_event ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("read the audit rows: %v", err)
	}
	defer rows.Close()
	var out []eventRowDump
	for rows.Next() {
		var r eventRowDump
		if err := rows.Scan(&r.ID, &r.PolicyID, &r.SubjectKind, &r.SubjectID, &r.Outcome, &r.Detail, &r.At); err != nil {
			t.Fatalf("read the audit rows: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the audit rows: %v", err)
	}
	return out
}

// eqpRaw runs EXPLAIN QUERY PLAN on a raw handle — the same measurement
// policy_test.go's eqpDetails makes, for the tests that work at the migration
// layer (the before/after plans of the carry test quote through it).
func eqpRaw(t *testing.T, g *gorm.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := g.Raw("EXPLAIN QUERY PLAN "+query, args...).Rows()
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan EQP row: %v", err)
		}
		out = append(out, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EQP rows: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("EQP produced no rows — this assertion could not fire")
	}
	return out
}

// tableColumns lists a table's columns in declared order.
func tableColumns(t *testing.T, s *Store, table string) []string {
	t.Helper()
	rows, err := s.r.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("read the columns of %s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("read the columns of %s: %v", table, err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the columns of %s: %v", table, err)
	}
	return out
}

// checkValues pulls the literal list out of a `CHECK (column IN (…))` constraint.
//
// The tests compare against the DDL's own values rather than a second copy of
// them in this file: two lists edited in the same commit agree with each other
// and still let a CHECK go wrong, which is the same failure the schema's single
// source of truth exists to prevent.
func checkValues(t *testing.T, sql, column string) []string {
	t.Helper()
	re, err := regexp.Compile(regexp.QuoteMeta(column+" IN (") + `([^)]*)\)`)
	if err != nil {
		t.Fatalf("compile the CHECK pattern for %s: %v", column, err)
	}
	m := re.FindStringSubmatch(sql)
	if m == nil {
		t.Fatalf("migration 0016 carries no CHECK list for %q", column)
	}
	var out []string
	for _, v := range strings.Split(m[1], ",") {
		out = append(out, strings.Trim(strings.TrimSpace(v), "'"))
	}
	return out
}

// equalStrings compares two lists exactly, order included — the order a CHECK's
// values are declared in is part of what a test asserts they are.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rowCounts counts every table's rows, on its own connection.
func rowCounts(t *testing.T, path string) map[string]int {
	t.Helper()
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, composeDSN(path, DefaultBusyTimeout, false))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()

	out := map[string]int{}
	for _, table := range tables {
		var n int
		if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %q", table)).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}
