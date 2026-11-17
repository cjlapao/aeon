package store

// records_schema_test.go — S38 T5's receipts for OL7 and OL14. 0036 is the
// migration with the most ways to be quietly wrong: a rebuild that loses a
// row (which integrity checks will bless — 0035's lesson, written into the
// copy-guard table itself), a CHECK that does not actually close its set,
// and two task populations (loop, manual) that must not be able to impersonate
// each other. The fresh-equals-upgraded drift test (schema_test.go's
// fingerprint comparison) already covers "a fresh database has the same shape
// as an upgraded one" for these seven tables automatically — what these
// tests pin is the row-preservation half and the constraints that only exist
// in this migration, exercised with direct SQL: a CHECK is only proven by
// statements the store API would never write.

import (
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// seedRecords35 populates the parents 0036 needs at the 0035 vintage — one
// group, two components, two loops, and three loop tasks (two on one loop,
// one on the other) with an engineer on two of them, because the rebuild's
// assignee_kind derivation is a CASE on that column and NULL-vs-empty is
// exactly what a real daemon leaves behind.
func seedRecords35(t *testing.T, gormPath string) {
	t.Helper()
	g, closeDB := rawGorm(t, gormPath)
	applyMigrations(t, g, Migrations()[:35])
	seed := []string{
		`INSERT INTO project (id,name,created_at,updated_at) VALUES ('prj-g1','the group','t','t')`,
		`INSERT INTO component (id,name,slug,project_id,created_at,updated_at) VALUES ('c1','alpha','alpha','prj-g1','t','t')`,
		`INSERT INTO component (id,name,slug,project_id,created_at,updated_at) VALUES ('c2','beta','beta','prj-g1','t','t')`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
		   VALUES ('l1','walk','The walk','a goal','/repo1','/repo1','standard','build','active','t','t')`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
		   VALUES ('l2','other','The other','a goal','/repo1','/repo1','standard','build','done','t','t')`,
		`INSERT INTO task (loop_slug,component_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('walk','/repo1','T1',1,'A task','go','eng','pending','t')`,
		`INSERT INTO task (loop_slug,component_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('walk','/repo1','T2',2,'Another task','go','','done','t')`,
		`INSERT INTO task (loop_slug,component_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('other','/repo1','T1',1,'Third task','go','eng2','pending','t')`,
	}
	for _, stmt := range seed {
		if err := g.Exec(stmt).Error; err != nil {
			closeDB()
			t.Fatalf("seed the pre-0036 store: %v\n%s", err, stmt)
		}
	}
	closeDB()
}

// openSQL opens the file through the raw door for the statements that must go
// straight to SQLite.
func openSQL(t *testing.T, path string) *gorm.DB {
	t.Helper()
	g, closeDB := rawGorm(t, path)
	t.Cleanup(closeDB)
	return g
}

func countSQL(t *testing.T, g *gorm.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := g.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// openFreshRaw opens a brand-new store through the real door (which runs all
// migrations) and hands back the raw handle to the same file — the shape a
// FRESH database has is what the direct-SQL tests below judge.
func openFreshRaw(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dir := t.TempDir()
	openAt(t, dir)
	path := filepath.Join(dir, "state.db")
	return openSQL(t, path), path
}

func TestMigration0036UpgradesTasksWithRowsIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	seedRecords35(t, path)

	openUpgraded(t, dir, "state.db") // walks the file through 0036, then closes it
	g := openSQL(t, path)

	if got := countSQL(t, g, `SELECT COUNT(*) FROM task`); got != 3 {
		t.Fatalf("the rebuild kept %d tasks, want 3 — row loss is the failure 0035 proved integrity checks do not see", got)
	}
	// Surrogate ids: every row has one, prefixed per OL14, and no two collide.
	var ids []string
	if err := g.Raw(`SELECT id FROM task ORDER BY id`).Scan(&ids).Error; err != nil {
		t.Fatalf("read ids: %v", err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !strings.HasPrefix(id, "tsk-") || id == "" {
			t.Fatalf("task id %q does not carry the surrogate prefix", id)
		}
		if seen[id] {
			t.Fatalf("two tasks share the surrogate id %q", id)
		}
		seen[id] = true
	}
	// The loop key rode out: the partial unique index is what enforces it now.
	var idx string
	if err := g.Raw(`SELECT COALESCE(MAX(name),'') FROM sqlite_master WHERE type='index' AND name='idx_task_loop_key'`).Scan(&idx).Error; err != nil {
		t.Fatalf("look for the loop-key index: %v", err)
	}
	if idx != "idx_task_loop_key" {
		t.Fatal("the (component_root, loop_slug, taskid) key is no longer enforced by an index")
	}
	// assignee_kind derived honestly from the old engineer column: two agent
	// rows (eng, eng2) and one NULL (the blank engineer must not become an
	// agent with no name).
	var kinds []string
	if err := g.Raw(`SELECT COALESCE(assignee_kind,'') FROM task`).Scan(&kinds).Error; err != nil {
		t.Fatalf("read assignees: %v", err)
	}
	tally := map[string]int{}
	for _, k := range kinds {
		tally[k]++
	}
	if tally["agent"] != 2 || tally[""] != 1 {
		t.Fatalf("assignee_kind derived %v, want two agent and one NULL", tally)
	}
	// Every loop task gained its 'owner' row (OL7), and nothing else did.
	if n := countSQL(t, g, `SELECT COUNT(*) FROM task_loop WHERE role='owner'`); n != 3 {
		t.Fatalf("task_loop carries %d owner rows, want 3", n)
	}
	// loop's new columns exist with the defaults OL7 chose.
	var scope string
	if err := g.Raw(`SELECT scope FROM loop WHERE slug='walk'`).Scan(&scope).Error; err != nil {
		t.Fatalf("read loop scope: %v", err)
	}
	if scope != "component" {
		t.Fatalf("a pre-existing loop reads scope %q, want component — the loops that predate this column were each one workspace's", scope)
	}
	// The copy guard left no debris, and the whole database still hangs together.
	if n := countSQL(t, g, `SELECT COUNT(*) FROM sqlite_master WHERE name='task_copy_guard'`); n != 0 {
		t.Fatal("task_copy_guard survived the migration — the guard is a mechanism, not a table to keep")
	}
	if n := countSQL(t, g, `SELECT COUNT(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Fatalf("foreign_key_check found %d violations after 0036", n)
	}
}

func TestReviewByKindRefusesAgentAndPolicy(t *testing.T) {
	g, _ := openFreshRaw(t)
	for _, kind := range []string{"agent", "policy"} {
		err := g.Exec(`INSERT INTO review (subject_kind,subject_id,subject_version,decision,by_kind,by_id,decided_at)
			VALUES ('goal','g1',1,'approved','` + kind + `','whoever','t')`).Error
		if err == nil {
			t.Fatalf("review accepted by_kind=%q — a review is a person's act until a later rebuild says otherwise", kind)
		}
		if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("by_kind=%q failed, but not on the CHECK: %v", kind, err)
		}
	}
	if err := g.Exec(`INSERT INTO review (subject_kind,subject_id,subject_version,decision,by_kind,by_id,decided_at)
		VALUES ('goal','g1',1,'approved','user','someone','t')`).Error; err != nil {
		t.Fatalf("the only admitted by_kind must pass: %v", err)
	}
}

func TestLoopTaskCannotCarryApprovalState(t *testing.T) {
	g, _ := openFreshRaw(t)
	// A real loop first, so the failure below can only be the CHECK and not a
	// foreign-key miss wearing the same clothes.
	if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
		VALUES ('l9','nobody','Nobody','g','/repo1','/repo1','standard','build','active','t','t')`).Error; err != nil {
		t.Fatalf("seed the loop the task names: %v", err)
	}
	err := g.Exec(`INSERT INTO task (id,loop_slug,component_root,taskid,ord,title,status,manual,approval_state,updated_at)
		VALUES ('tsk-a','nobody','/repo1','T1',1,'a loop task','pending',0,'pending','t')`).Error
	if err == nil {
		t.Fatal("a loop task (manual=0) was accepted with an approval state — OL14's two populations must not blur")
	}
	if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("it failed, but not on the CHECK: %v", err)
	}
	// The same row as a manual task is fine — the CHECK is about populations,
	// not about the column.
	if err := g.Exec(`INSERT INTO task (id,loop_slug,component_root,taskid,ord,title,status,manual,approval_state,updated_at)
		VALUES ('tsk-b','nobody','/repo1','T1',1,'a manual task','pending',1,'pending','t')`).Error; err != nil {
		t.Fatalf("a manual task with an approval state must pass: %v", err)
	}
}

func TestManualTaskNeedsNoLoop(t *testing.T) {
	g, _ := openFreshRaw(t)
	// A manual task with no loop at all — OL14's second population, born free.
	if err := g.Exec(`INSERT INTO task (id,ord,title,status,manual,updated_at)
		VALUES ('tsk-free',1,'write the changelog','todo',1,'t')`).Error; err != nil {
		t.Fatalf("a manual task without a loop must be insertable: %v", err)
	}
	// Two of them, in fact — NULL keys do not collide (and the PARTIAL index
	// says plainly that loop uniqueness governs only loop rows).
	if err := g.Exec(`INSERT INTO task (id,ord,title,status,manual,updated_at)
		VALUES ('tsk-free2',2,'triage the inbox','todo',1,'t')`).Error; err != nil {
		t.Fatalf("a second manual task without a loop must also be insertable: %v", err)
	}
	// A loop task with no loop is a lie about its population.
	err := g.Exec(`INSERT INTO task (id,ord,title,status,manual,updated_at)
		VALUES ('tsk-lie',1,'pretends to be a loop task','todo',0,'t')`).Error
	if err == nil {
		t.Fatal("manual=0 with no loop key was accepted — the CHECK has to keep the populations honest")
	}
	if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("it failed, but not on the CHECK: %v", err)
	}
}

func TestBriefIsVersionedPerLoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	seedRecords35(t, path)
	openUpgraded(t, dir, "state.db")
	g := openSQL(t, path)

	if err := g.Exec(`INSERT INTO brief (id,loop_slug,component_root,version,created_at,updated_at)
		VALUES ('brf-1','walk','/repo1',1,'t','t')`).Error; err != nil {
		t.Fatalf("a first brief version must insert (and its loop exists): %v", err)
	}
	err := g.Exec(`INSERT INTO brief (id,loop_slug,component_root,version,created_at,updated_at)
		VALUES ('brf-1b','walk','/repo1',1,'t','t')`).Error
	if err == nil {
		t.Fatal("a second brief at (component_root, loop_slug, version)=(/repo1, walk, 1) was accepted — rewrites would overwrite review history")
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Errorf("it failed, but not on the UNIQUE: %v", err)
	}
	// The next version of the same loop's brief is exactly what the rewrite
	// path will do (T7), so it must be legal.
	if err := g.Exec(`INSERT INTO brief (id,loop_slug,component_root,version,created_at,updated_at)
		VALUES ('brf-2','walk','/repo1',2,'t','t')`).Error; err != nil {
		t.Fatalf("version 2 of the same loop's brief must be insertable: %v", err)
	}
}

func TestNoExecutionStateColumn(t *testing.T) {
	// OL11's "no stored totals, no derived state" rule as a schema fact:
	// none of the records tables may grow an execution_state or a *_total
	// column — those are computed, and a stored copy is the drift the design
	// refuses.
	g, _ := openFreshRaw(t)
	for _, table := range []string{"goal", "epic", "brief", "task"} {
		var cols []string
		if err := g.Raw(`SELECT name FROM pragma_table_info(?)`, table).Scan(&cols).Error; err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		for _, c := range cols {
			if c == "execution_state" || strings.HasSuffix(c, "_total") {
				t.Errorf("%s carries %q — OL11 keeps derived state out of the schema", table, c)
			}
		}
	}
}
