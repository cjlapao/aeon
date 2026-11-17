package store

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

// S44-extra T2 (ORDER AMENDMENT, 2026-11-02) — the park/restore order drill.
//
// 20261030200000_component_tree parks 23 child tables of component, clears
// them, rebuilds component, and re-inserts the snapshots. The order it first
// shipped in restored chat_message BEFORE question, and cleared question
// before agent_task — both invisible on an empty store (the census fixture
// carried no chat or agent rows, so every drill passed) and fatal on the first
// live store that had ever asked a chat question: the store's own rows could
// not be re-inserted (chat_message.question_id -> question) and the open
// refused with FOREIGN KEY constraint failed. This drill is the S55 T12 shape
// applied to that vintage: a database parked just before the tree migration,
// seeded with the one data shape the shipped order could not carry, must come
// through with every row intact.

// ctParkSeedVintage plants the pre-tree shape with chat and agent rows in it:
// one implicit project with its one component (the CT5 collapse case), a loop
// on the component's root, a done step, a gate question on that step, the
// agent's question message naming it, a plain note, and a COMPLETED async
// agent task that raised the same question (the NO ACTION clear-order case).
func ctParkSeedVintage(t *testing.T, g *gorm.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO project (id,name,short_description,description,implicit,created_at,updated_at)
			VALUES ('prj-ct','ct project','','',1,'2026-10-30T09:00:00Z','2026-10-30T09:00:00Z')`,
		`INSERT INTO component (id,name,slug,project_id,created_at,updated_at)
			VALUES ('cmp-ct','ct component','ct-component','prj-ct','2026-10-30T09:00:00Z','2026-10-30T09:00:00Z')`,
		`INSERT INTO component_root (component_root,component_id,added_at)
			VALUES ('/r/ct','cmp-ct','2026-10-30T09:00:00Z')`,
		`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id)
			VALUES ('l-ct','ct-drill','t','g','/r/ct','/r/ct','standard','preflight','waiting','2026-10-30T09:00:00Z','2026-10-30T09:00:00Z','person','')`,
		`INSERT INTO step (loop_slug,component_root,phase,agent,status,started_at)
			VALUES ('ct-drill','/r/ct','build','x','done','2026-10-30T09:00:00Z')`,
		`INSERT INTO question (loop_slug,component_root,step_id,kind,body,asked_at)
			VALUES ('ct-drill','/r/ct',1,'gate','proceed?','2026-10-30T09:00:00Z')`,
		`INSERT INTO chat_message (loop_slug,component_root,seq,author,kind,body,question_id,created_at)
			VALUES ('ct-drill','/r/ct',1,'agent','question','proceed?',1,'2026-10-30T09:00:00Z')`,
		`INSERT INTO chat_message (loop_slug,component_root,seq,author,kind,body,created_at)
			VALUES ('ct-drill','/r/ct',2,'agent','note','a note','2026-10-30T09:00:00Z')`,
		`INSERT INTO agent_task (caller_loop_id,caller_lane,caller_step_id,question_id,callee_agent,skill_id,mode,status,policies_json,policies_hash,created_at,completed_at)
			VALUES ('l-ct','build',1,1,'agent-x','s1','async','completed','{}','h','2026-10-30T09:00:00Z','2026-10-30T09:00:00Z')`,
	}
	for _, stmt := range stmts {
		if err := g.Exec(stmt).Error; err != nil {
			t.Fatalf("seed the parked vintage: %v\n%s", err, stmt)
		}
	}
}

// TestCTUpgradeParkOrderSurvivesChatQuestions is the drill: park the database
// at the vintage just before the tree migration, seed the shape the shipped
// order could not carry, open it with the current code, and require every row
// back with its references intact.
func TestCTUpgradeParkOrderSurvivesChatQuestions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, storeFileName)
	g, closeDB := rawGorm(t, path)
	defer closeDB()

	full := Migrations()
	// Cut before the tree migration BY NAME (the S55 drill's merge rule): the
	// premise is "a vintage parked just before the tree migration", whatever
	// lands after it.
	cut := len(full)
	for i, m := range full {
		if m.Name == "20261030200000_component_tree" {
			cut = i
			break
		}
	}
	if cut == len(full) {
		t.Fatal("the component_tree migration is not in Migrations() — the drill's premise is broken")
	}
	applyMigrations(t, g, full[:cut])
	ctParkSeedVintage(t, g)

	// The upgrade: the current code opens the same file and applies the rest.
	// On the shipped (buggy) order this is the line that died with
	// "FOREIGN KEY constraint failed" on a live store.
	s, err := Open(Options{Dir: dir, Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the parked database: %v", err)
	}
	defer s.Close()

	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(full) {
		t.Fatalf("ledger holds %d after the upgrade, want %d", len(applied), len(full))
	}

	// Every seeded row survives, references and all.
	if n := countTable(t, s.GormDB(), "question"); n != 1 {
		t.Errorf("question had 1 row before the upgrade and %d after", n)
	}
	if n := countTable(t, s.GormDB(), "chat_message"); n != 2 {
		t.Errorf("chat_message had 2 rows before the upgrade and %d after", n)
	}
	var qid int
	if err := s.GormDB().Raw(`SELECT question_id FROM chat_message WHERE seq = 1`).Scan(&qid).Error; err != nil {
		t.Fatal(err)
	}
	if qid != 1 {
		t.Errorf("the question message's question_id is %d after the upgrade, want 1 (the question it names must be the question it comes back with)", qid)
	}
	if n := countTable(t, s.GormDB(), "agent_task"); n != 1 {
		t.Errorf("agent_task had 1 row before the upgrade and %d after", n)
	}
	var at struct {
		QuestionID   int `gorm:"column:question_id"`
		CallerStepID int `gorm:"column:caller_step_id"`
	}
	if err := s.GormDB().Raw(`SELECT question_id, caller_step_id FROM agent_task`).Scan(&at).Error; err != nil {
		t.Fatal(err)
	}
	if at.QuestionID != 1 || at.CallerStepID != 1 {
		t.Errorf("the agent task's (question_id, caller_step_id) is (%d, %d) after the upgrade, want (1, 1)", at.QuestionID, at.CallerStepID)
	}
	if n := countTable(t, s.GormDB(), "step"); n != 1 {
		t.Errorf("step had 1 row before the upgrade and %d after", n)
	}
	if n := countTable(t, s.GormDB(), "loop"); n != 1 {
		t.Errorf("loop had 1 row before the upgrade and %d after", n)
	}

	// The tree itself: the implicit one-component project collapses, so the
	// component survives as the one root and the root's child count is whole.
	var roots, parentless int
	if err := s.GormDB().Raw(`SELECT count(*) FROM component`).Scan(&roots).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.GormDB().Raw(`SELECT count(*) FROM component WHERE parent_id IS NULL AND id = 'cmp-ct'`).Scan(&parentless).Error; err != nil {
		t.Fatal(err)
	}
	if roots != 1 || parentless != 1 {
		t.Errorf("component holds %d rows with %d of them the collapsed root, want 1 and 1", roots, parentless)
	}
	if n := countTable(t, s.GormDB(), "component_root"); n != 1 {
		t.Errorf("component_root had 1 row before the upgrade and %d after", n)
	}

	// No foreign key is violated anywhere after the upgrade.
	var fk int
	if err := s.GormDB().Raw(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fk).Error; err != nil {
		t.Fatal(err)
	}
	if fk != 0 {
		t.Errorf("pragma_foreign_key_check found %d violations after the upgrade", fk)
	}
}
