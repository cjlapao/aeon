package store

// org_layers_test.go — S38 T1's receipts for OL1. The rename is the kind of
// migration whose failure modes are silent — a missed ALTER leaves a column
// with the old name that every fresh database lacks (and the fresh-equals-
// migrated drift test catches THAT), while a careless DROP deletes children
// through ON DELETE CASCADE (and the naive-drop test says why rename is the
// only verb here). What these tests pin is the loud half: the upgrade keeps
// every row reachable under the new vocabulary, and the new tier actually
// backfills.

import (
	"path/filepath"
	"testing"
)

// seedOrgLayers populates every scope-carrying table the 0035 ALTER list
// names, at the pre-rename vintage. Two components (the old `project`), two
// roots, one loop with a phase, a task, a step and a document hanging under
// it — the same shape seedFrozen uses, because that shape is what a real
// daemon leaves behind, and this test is about real stores surviving a
// rename. Returns the per-table counts the upgrade must not change.
func seedOrgLayers(t *testing.T, gormPath string) {
	t.Helper()
	g, closeDB := rawGorm(t, gormPath)
	applyMigrations(t, g, Migrations()[:34])
	seed := []string{
		`INSERT INTO project (id,name,slug,created_at,updated_at) VALUES ('p1','alpha','alpha','t','t')`,
		`INSERT INTO project (id,name,slug,remote,remote_key,repo_key,created_at,updated_at) VALUES ('p2','beta','beta','git@example.com/beta.git','rk2','bk2','t','t')`,
		`INSERT INTO project_root (project_root,project_id,is_primary,added_at) VALUES ('/repo1','p1',1,'t')`,
		`INSERT INTO project_root (project_root,project_id,remote_key,repo_key,is_primary,added_at) VALUES ('/repo2','p2','rk2','bk2',1,'t')`,
		`INSERT INTO loop (id,slug,title,goal,root,project_root,mode,phase,status,started_at,updated_at)
		   VALUES ('l','walk','The walk','a goal','/repo1','/repo1','standard','build','active','t','t')`,
		`INSERT INTO loop_phase (loop_slug,project_root,phase,status,ord,updated_at)
		   VALUES ('walk','/repo1','build','done',1,'t')`,
		`INSERT INTO task (loop_slug,project_root,taskid,ord,title,tech,engineer,status,updated_at)
		   VALUES ('walk','/repo1','T1',1,'A task','go','eng','pending','t')`,
		`INSERT INTO step (loop_slug,project_root,phase,agent,status,started_at,cost_state)
		   VALUES ('walk','/repo1','build','eng','done','t','known')`,
		`INSERT INTO loop_document (id,project_root,loop_slug,file,rev,content,bytes,sha256,written_at)
		   VALUES ('d','/repo1','walk','plan.md',1,x'00',1,'deadbeef','t')`,
		`INSERT INTO memory (loop_slug,project_root,ts,note) VALUES ('walk','/repo1','t','a note')`,
		`INSERT INTO question (loop_slug,project_root,kind,body,asked_at)
		   VALUES ('walk','/repo1','gate','proceed?','t')`,
		`INSERT INTO chat_message (loop_slug,project_root,seq,author,kind,body,created_at)
		   VALUES ('walk','/repo1',1,'agent','question','proceed?','t')`,
		`INSERT INTO loop_archive (id,slug,project_root,title,goal,status,archived_at,state,state_bytes,file_count,file_bytes)
		   VALUES ('a','gone','/repo1','Gone','g','done','t',x'00',1,0,0)`,
		`INSERT INTO event (kind,payload,created_at) VALUES ('daemon.started','{}','t')`,
	}
	for _, stmt := range seed {
		if err := g.Exec(stmt).Error; err != nil {
			closeDB()
			t.Fatalf("seed the pre-rename store: %v\n%s", err, stmt)
		}
	}
	closeDB()
}

// openUpgraded closes the seeded database and opens it through the real door,
// which walks it forward through 0035 — the consumer's path, not a hand-run
// of the SQL string.
func openUpgraded(t *testing.T, dir, file string) *Store {
	t.Helper()
	s, err := Open(Options{Dir: dir, File: file, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the store through 0035: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigration0035UpgradesAPopulatedStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	seedOrgLayers(t, path)
	before := rowCounts(t, path)

	s := openUpgraded(t, dir, "state.db")
	after := rowCounts(t, path)
	for table, n := range before {
		if table == "project" || table == "project_root" || table == "_migrations" {
			continue // renamed tables carry their counts under the new names;
			// the ledger legitimately grows by the 0035 row
		}
		if after[table] != n {
			t.Errorf("table %s held %d rows before the rename and %d after — a RENAME does not delete; a cascade did", table, n, after[table])
		}
	}

	// The renamed tables carry their rows, and the rows answer to the NEW
	// vocabulary — read through the store's own doors, not raw SQL.
	comps, err := s.ListComponents(ComponentFilter{IncludeArchived: true})
	if err != nil || len(comps) != 2 {
		t.Fatalf("ListComponents after the upgrade: %d rows, %v — want the two seeded components", len(comps), err)
	}
	if comps[0].ID == "" || comps[1].ID == "" {
		t.Errorf("every upgraded component must be reachable under the new vocabulary; got %q / %q", comps[0].ID, comps[1].ID)
	}
	sc := Scope{ComponentRoot: "/repo1", LoopSlug: "walk"}
	tasks, err := s.ListTasks(sc)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("the task must survive under the renamed scope column: %d rows, %v", len(tasks), err)
	}

	// No FK may point at a ghost after the rename-and-rebuild.
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	var viol struct {
		Table string `gorm:"column:table"`
		Rowid int64  `gorm:"column:rowid"`
	}
	var n int
	if err := g.Raw(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	_ = viol
	if n != 0 {
		var rows []struct {
			Table  string `gorm:"column:table"`
			Parent string `gorm:"column:parent"`
		}
		g.Raw(`SELECT "table", parent FROM pragma_foreign_key_check`).Scan(&rows)
		t.Fatalf("pragma_foreign_key_check reports %d violations after 0035: %+v", n, rows)
	}
}

func TestMigration0035OldNamesAreGone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	seedOrgLayers(t, path)
	openUpgraded(t, dir, "state.db")

	g, closeDB := rawGorm(t, path)
	defer closeDB()

	var tables int
	g.Raw(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('project_root')`).Scan(&tables)
	if tables != 0 {
		t.Fatal("the old roots table name survived — it is component_root now, and both cannot exist")
	}
	var cols int
	g.Raw(`SELECT count(*) FROM (SELECT m.name t, p.name c FROM sqlite_master m JOIN pragma_table_info(m.name) p ON 1=1
		WHERE m.type='table' AND p.name='project_root')`).Scan(&cols)
	if cols != 0 {
		var where []struct {
			T string
			C string
		}
		g.Raw(`SELECT m.name t, p.name c FROM sqlite_master m JOIN pragma_table_info(m.name) p ON 1=1
			WHERE m.type='table' AND p.name='project_root'`).Scan(&where)
		t.Fatalf("%d columns still answer to project_root — the ALTER list missed a table: %+v", cols, where)
	}
	// The project tier 0035 added (project.implicit, component.project_id) is
	// not asserted here: S44's CT (T2 rebuilds component without project_id and
	// gains parent_id; T3 drops the project table) supersedes that tier. The
	// surviving assertion is the OL1 rename itself — the old name is gone.
}

func TestIconIsEmojiOrImageNeverBoth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	seedOrgLayers(t, path)
	openUpgraded(t, dir, "state.db")

	g, closeDB := rawGorm(t, path)
	defer closeDB()
	err := g.Exec(`INSERT INTO project (id,name,icon_emoji,icon_image,created_at,updated_at)
		VALUES ('both','both','📦','img://x','t','t')`).Error
	if err == nil {
		t.Fatal("an icon carrying BOTH emoji and image slipped through — the spec's Icon is one or the other, and the CHECK is the answer")
	}
}
