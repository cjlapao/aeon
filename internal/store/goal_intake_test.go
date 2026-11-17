package store

// goal_intake_test.go — S61 T1: the six intake-plane tables behave as
// designed and the one-word CHECK widenings are LIVE on upgraded stores, not
// merely present. The tests are the table in docs/aeon/S61-goal-intake/tasks/T1.md;
// the drill NAME (TestGoalIntakeMigrationDrillFreshEqualsUpgraded) carries
// "Migration" so the slice's -run selector reaches it, and its shape is
// S60 T2's drill pattern (prior vintage → seed through the store's own API →
// reopen → data identical, CHECK live, shape equal to fresh).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var giUser = Actor{Kind: "user", ID: "t1"}

// giCharter appends a revision and fails the test on any refusal.
func giCharter(t *testing.T, s *Store, root, body string) CharterRevision {
	t.Helper()
	r, ref, err := s.AppendCharterRevision(NewCharterRevision{
		RootID: root, Body: body, Summary: "Charter v? — 1 objective, 0 boundaries.",
		By: giUser, Source: "person"})
	if err != nil {
		t.Fatalf("append charter (%s): %v", root, err)
	}
	if ref != nil {
		t.Fatalf("append charter refused: %s — %s", ref.Kind, ref.Sentence)
	}
	return r
}

// TestCharterRevisionsAreAppendOnly — P3: the history is written once. The
// UPDATE and DELETE refusals name the rule; the row that survived proves the
// abort bit.
func TestCharterRevisionsAreAppendOnly(t *testing.T) {
	g, s := testStore(t)
	root := seedRoot(t, s, "/repo").ID
	r := giCharter(t, s, root, `{"title":"ship it","objective":"one"}`)
	if r.Revision != 1 {
		t.Fatalf("first revision = %d, want 1", r.Revision)
	}

	if _, err := s.w.Exec(`UPDATE charter_revision SET body = '{}' WHERE id = ?`, r.ID); err == nil {
		t.Fatal("an UPDATE on the charter history succeeded; the history is alterable")
	} else if !strings.Contains(err.Error(), "charter history is append-only") {
		t.Fatalf("the UPDATE refusal does not name the rule: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM charter_revision WHERE id = ?`, r.ID); err == nil {
		t.Fatal("a DELETE on the charter history succeeded; the history is alterable")
	} else if !strings.Contains(err.Error(), "charter history is append-only") {
		t.Fatalf("the DELETE refusal does not name the rule: %v", err)
	}
	if n := countTable(t, g, "charter_revision"); n != 1 {
		t.Fatalf("the history holds %d rows, want the one that was appended", n)
	}
	// Unknown source and bodyless appends are refusals BEFORE SQL.
	if _, ref, err := s.AppendCharterRevision(NewCharterRevision{
		RootID: root, Body: `{}`, Summary: "s", By: giUser, Source: "whim"}); err != nil ||
		ref == nil || ref.Kind != "source-unknown" {
		t.Fatalf("source 'whim': ref=%v err=%v, want a source-unknown refusal", ref, err)
	}
	// Absent is a sentence, not a zero row.
	if _, err := s.CurrentCharter("/nope"); err == nil || !strings.Contains(err.Error(), "no charter yet") {
		t.Fatalf("CurrentCharter of an unknown root: %v, want the no-charter-yet error", err)
	}
}

// TestCharterRevisionNumbersNeverReset — max+1 inside the transaction; the
// UNIQUE(root_id, revision) key is the witness, and since rows cannot die the
// numbers only climb.
func TestCharterRevisionNumbersNeverReset(t *testing.T) {
	g, s := testStore(t)
	root := seedRoot(t, s, "/repo").ID
	for i, body := range []string{`{"objective":"one"}`, `{"objective":"two"}`, `{"objective":"three"}`} {
		r := giCharter(t, s, root, body)
		if r.Revision != i+1 {
			t.Fatalf("revision %d appended as %d", i+1, r.Revision)
		}
	}
	// The key itself refuses a hand-forged duplicate number.
	if err := g.Exec(`INSERT INTO charter_revision (id,root_id,revision,body,summary,actor_kind,actor_id,source,created_at)
		VALUES ('charter-forged',?,2,'{}','forged','user','x','person','2026-11-05T00:00:00Z')`, root).Error; err == nil {
		t.Fatal("a forged revision 2 landed beside the real one; the UNIQUE key is missing")
	}
	cur, err := s.CurrentCharter(root)
	if err != nil || cur.Revision != 3 {
		t.Fatalf("CurrentCharter = %+v (%v), want revision 3", cur, err)
	}
	hs, err := s.CharterRevisions(root)
	if err != nil || len(hs) != 3 || hs[0].Revision != 1 || hs[2].Revision != 3 {
		t.Fatalf("history = %+v (%v), want three oldest-first", hs, err)
	}
	// ListChartersWithCurrent answers one line per root, at its current number.
	rows, err := s.ListChartersWithCurrent()
	if err != nil || len(rows) != 1 || rows[0].Revision != 3 {
		t.Fatalf("ListChartersWithCurrent = %+v (%v), want the one root at 3", rows, err)
	}
}

// TestCharterRefusedOnAChild — GI3: a tree has ONE charter, on the root. The
// refusal names the child's project; the trigger is the net under the store.
func TestCharterRefusedOnAChild(t *testing.T) {
	g, s := testStore(t)
	root := seedRoot(t, s, "/repo").ID
	s57SeedChild(t, g, "child-of-repo", root)
	_, ref, err := s.AppendCharterRevision(NewCharterRevision{
		RootID: "child-of-repo", Body: `{}`, Summary: "s", By: giUser, Source: "person"})
	if err != nil {
		t.Fatalf("child charter append: %v", err)
	}
	if ref == nil || ref.Kind != "charter-on-child" {
		t.Fatalf("child charter append: ref=%v, want a charter-on-child refusal", ref)
	}
	if !strings.Contains(ref.Sentence, "is a component of project") {
		t.Fatalf("the refusal does not name the project: %s", ref.Sentence)
	}
	// The trigger refuses even a hand-written row that skipped the store.
	if err := g.Exec(`INSERT INTO charter_revision (id,root_id,revision,body,summary,actor_kind,actor_id,source,created_at)
		VALUES ('charter-sneak','child-of-repo',1,'{}','sneak','user','x','person','2026-11-05T00:00:00Z')`).Error; err == nil ||
		!strings.Contains(err.Error(), "the charter lives on the project") {
		t.Fatalf("the root-only trigger let a child row through: %v", err)
	}
}

// giSeedGoal creates a goal with two criteria at version 1.
func giSeedGoal(t *testing.T, s *Store, root string) Goal {
	t.Helper()
	goal, ref, err := s.CreateGoal(NewGoal{ComponentID: root, Title: "add criteria",
		Type: "feature", SourceKind: "manual", By: giUser})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	if ref != nil {
		t.Fatalf("create goal refused: %s — %s", ref.Kind, ref.Sentence)
	}
	if ref, err := s.ReplaceGoalCriteria(goal.ID, 1, []Criterion{
		{Text: "the door answers", CheckKind: "command", CheckRef: "aeon intake show x"},
		{Text: "the board renders", CheckKind: "manual"},
	}); ref != nil || err != nil {
		t.Fatalf("replace criteria v1: ref=%v err=%v", ref, err)
	}
	return goal
}

// TestGoalCriteriaCopyForwardOnUpdate — GI5 / P-G: UpdateGoal copies the
// criteria set to the new version exactly like goal_component, so an edit to
// the TEXT never silently redefines the definition of done.
func TestGoalCriteriaCopyForwardOnUpdate(t *testing.T) {
	_, s := testStore(t)
	root := seedRoot(t, s, "/repo").ID
	goal := giSeedGoal(t, s, root)

	v1, err := s.GoalCriteria(goal.ID, 1)
	if err != nil || len(v1) != 2 || v1[0].Ordinal != 1 || v1[0].CriterionID != "ac-1" ||
		v1[0].CheckKind != "command" || v1[1].CheckKind != "manual" {
		t.Fatalf("v1 criteria = %+v (%v)", v1, err)
	}

	edited, ref, err := s.UpdateGoal(GoalEdit{ID: goal.ID, Title: "add criteria, better wording", By: giUser})
	if err != nil || ref != nil {
		t.Fatalf("update goal: ref=%v err=%v", ref, err)
	}
	if edited.Version != 2 {
		t.Fatalf("goal version after edit = %d, want 2", edited.Version)
	}
	v2, err := s.GoalCriteria(goal.ID, 2)
	if err != nil || len(v2) != 2 {
		t.Fatalf("v2 criteria = %+v (%v), want the set copied forward", v2, err)
	}
	for i := range v1 {
		if v2[i] != v1[i] {
			t.Fatalf("criterion %d changed across the bump: %+v → %+v", i, v1[i], v2[i])
		}
	}
	// v1's set is history, not overwritten.
	if again, err := s.GoalCriteria(goal.ID, 1); err != nil || len(again) != 2 {
		t.Fatalf("v1 criteria after the bump = %+v (%v)", again, err)
	}

	// Refusals: unknown check kind, unknown version, too many, empty text.
	if ref, _ := s.ReplaceGoalCriteria(goal.ID, 2, []Criterion{{Text: "x", CheckKind: "vibes"}}); ref == nil || ref.Kind != "check-kind-unknown" {
		t.Fatalf("check kind 'vibes': ref=%v, want check-kind-unknown", ref)
	}
	if ref, _ := s.ReplaceGoalCriteria(goal.ID, 9, []Criterion{{Text: "x"}}); ref == nil || ref.Kind != "goal-version-unknown" {
		t.Fatalf("version 9: ref=%v, want goal-version-unknown", ref)
	}
	var many []Criterion
	for i := 0; i < maxCriteriaPerGoal+1; i++ {
		many = append(many, Criterion{Text: fmt.Sprintf("c%d", i)})
	}
	if ref, _ := s.ReplaceGoalCriteria(goal.ID, 2, many); ref == nil || ref.Kind != "criteria-too-many" {
		t.Fatalf("%d criteria: ref=%v, want criteria-too-many", len(many), ref)
	}
	if ref, _ := s.ReplaceGoalCriteria(goal.ID, 2, []Criterion{{Text: ""}}); ref == nil || ref.Kind != "criterion-text" {
		t.Fatalf("empty criterion: ref=%v, want criterion-text", ref)
	}
	// Replace is whole-set-or-nothing: the refusal left v2's copied set intact.
	if again, err := s.GoalCriteria(goal.ID, 2); err != nil || len(again) != 2 {
		t.Fatalf("after refusals v2 = %+v (%v), want the two copied rows untouched", again, err)
	}
}

// TestGoalCriteriaCascadeOnlyWithGoal — the rows die ONLY with the goal:
// deleting the goal row removes every version's criteria (ON DELETE CASCADE);
// no store method exists to delete criteria of a live goal except replace.
func TestGoalCriteriaCascadeOnlyWithGoal(t *testing.T) {
	g, s := testStore(t)
	root := seedRoot(t, s, "/repo").ID
	goal := giSeedGoal(t, s, root)
	if err := g.Exec(`DELETE FROM goal WHERE id = ?`, goal.ID).Error; err != nil {
		t.Fatalf("delete goal row: %v", err)
	}
	if n := countTable(t, g, "goal_criterion"); n != 0 {
		t.Fatalf("%d criteria survived their goal; the cascade is missing", n)
	}
}

// TestIntakeRootSetOnce — GI6: the session row, person-only, each late field
// written exactly once, the root link un-changeable; refusals BEFORE SQL and
// the trigger's abort as the net under both.
func TestIntakeRootSetOnce(t *testing.T) {
	_, s := testStore(t)
	root1 := seedRoot(t, s, "/repo").ID
	root2 := seedRoot(t, s, "/repo2").ID

	if _, ref, _ := s.CreateIntake("nope", "/state/intake/x", Actor{Kind: "agent", ID: "a"}); ref == nil || ref.Kind != "intake-not-a-person" {
		t.Fatalf("an agent opened an intake: ref=%v, want intake-not-a-person", ref)
	}
	in, ref, err := s.CreateIntake("demo", "/state/intake/int-demo", giUser)
	if err != nil || ref != nil {
		t.Fatalf("create intake: ref=%v err=%v", ref, err)
	}
	if !strings.HasPrefix(in.ID, "int-") || in.RootID != "" || in.TargetPath != "" || in.LoopSlug != "" {
		t.Fatalf("fresh intake = %+v, want the three late fields NULL-empty", in)
	}
	// The loop is recorded once.
	if ref, err := s.SetIntakeLoop(in.ID, "intake-demo"); ref != nil || err != nil {
		t.Fatalf("set loop: ref=%v err=%v", ref, err)
	}
	if ref, _ := s.SetIntakeLoop(in.ID, "intake-other"); ref == nil || ref.Kind != "intake-loop-set" {
		t.Fatalf("second loop slug: ref=%v, want intake-loop-set", ref)
	}
	// The root link lands once, with the target path in the same write.
	if _, err := s.GetIntake("int-nope"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("GetIntake of a stranger: %v", err)
	}
	if ref, _ := s.SetIntakeRoot(in.ID, "comp-ghost", ""); ref == nil || ref.Kind != "component-unknown" {
		t.Fatalf("link to a stranger component: ref=%v, want component-unknown", ref)
	}
	if ref, err := s.SetIntakeRoot(in.ID, root2, "/home/me/projects/demo"); ref != nil || err != nil {
		t.Fatalf("link root: ref=%v err=%v", ref, err)
	}
	if ref, _ := s.SetIntakeRoot(in.ID, root1, ""); ref == nil || ref.Kind != "intake-root-set" {
		t.Fatalf("second link: ref=%v, want intake-root-set", ref)
	}
	// The trigger refuses the hand-written relink and the rename both.
	if _, err := s.w.Exec(`UPDATE intake SET root_id = ? WHERE id = ?`, root1, in.ID); err == nil ||
		!strings.Contains(err.Error(), "exactly once") {
		t.Fatalf("raw relink: %v, want the set-once trigger's abort", err)
	}
	if _, err := s.w.Exec(`UPDATE intake SET name = 'renamed' WHERE id = ?`, in.ID); err == nil ||
		!strings.Contains(err.Error(), "exactly once") {
		t.Fatalf("raw rename: %v, want the trigger's abort", err)
	}
	// A no-op of the link (same root) is allowed by the trigger and refused
	// by the store's pre-read — the caller hears intake-root-set either way.
	// The row reads back complete.
	back, err := s.GetIntake(in.ID)
	if err != nil || back.RootID != root2 || back.TargetPath != "/home/me/projects/demo" ||
		back.LoopSlug != "intake-demo" || back.CreatedByKind != "user" {
		t.Fatalf("intake = %+v (%v)", back, err)
	}
	if ls, err := s.ListIntakes(); err != nil || len(ls) != 1 || ls[0].ID != in.ID {
		t.Fatalf("ListIntakes = %+v (%v)", ls, err)
	}
}

// TestIntakeStepsAppendOnlyAndOrdered — GI13's resume ledger: ordinals climb,
// the outcome set is closed, and a written row never changes or dies.
func TestIntakeStepsAppendOnlyAndOrdered(t *testing.T) {
	g, s := testStore(t)
	in, ref, err := s.CreateIntake("ledger", "/state/intake/l", giUser)
	if err != nil || ref != nil {
		t.Fatalf("create intake: ref=%v err=%v", ref, err)
	}
	for i, step := range []struct{ step, outcome string }{
		{"charter", "done"}, {"scaffold:api", "skipped"}, {"git:api", "failed"},
	} {
		st, ref, err := s.AppendIntakeStep(in.ID, step.step, step.outcome, fmt.Sprintf("step %d", i+1))
		if err != nil || ref != nil {
			t.Fatalf("append step %d: ref=%v err=%v", i+1, ref, err)
		}
		if st.Ordinal != i+1 {
			t.Fatalf("step %d landed at ordinal %d", i+1, st.Ordinal)
		}
	}
	if _, ref, _ := s.AppendIntakeStep(in.ID, "x", "done-with-feelings", "d"); ref == nil || ref.Kind != "outcome-unknown" {
		t.Fatalf("outcome 'done-with-feelings': ref=%v, want outcome-unknown", ref)
	}
	if _, ref, _ := s.AppendIntakeStep("int-ghost", "x", "done", "d"); ref == nil || ref.Kind != "intake-unknown" {
		t.Fatalf("stranger intake: ref=%v, want intake-unknown", ref)
	}
	steps, err := s.IntakeSteps(in.ID)
	if err != nil || len(steps) != 3 {
		t.Fatalf("steps = %+v (%v), want three", steps, err)
	}
	if err := g.Exec(`UPDATE intake_step SET outcome = 'done' WHERE intake_id = ? AND ordinal = 3`, in.ID).Error; err == nil ||
		!strings.Contains(err.Error(), "written once") {
		t.Fatalf("raw step edit: %v, want the append-only abort", err)
	}
	if err := g.Exec(`DELETE FROM intake_step WHERE intake_id = ? AND ordinal = 1`, in.ID).Error; err == nil ||
		!strings.Contains(err.Error(), "written once") {
		t.Fatalf("raw step delete: %v, want the append-only abort", err)
	}
}

// TestTemplateSaveWritesARevisionInOneTransaction — GI14: the mutable row and
// its history move together or not at all; origin survives an operator edit;
// delete dies at the trigger.
func TestTemplateSaveWritesARevisionInOneTransaction(t *testing.T) {
	g, s := testStore(t)
	files := []TemplateFile{{Path: "main.go", Mode: 0o644, Content: "package main\n"}}
	tpl, ref, err := s.SaveTemplate(NewTemplate{Slug: "go-basic", Title: "Go basic",
		StackTags: []string{"golang"}, Files: files, SeedOrigin: "system"}, giUser)
	if err != nil || ref != nil {
		t.Fatalf("save v1: ref=%v err=%v", ref, err)
	}
	if tpl.Version != 1 || tpl.Origin != "system" {
		t.Fatalf("first save = %+v, want version 1 origin system", tpl)
	}
	if revs, err := s.TemplateRevisions("go-basic"); err != nil || len(revs) != 1 ||
		revs[0].ActorID != "t1" || revs[0].Origin != "system" {
		t.Fatalf("revisions after v1 = %+v (%v)", revs, err)
	}

	files2 := append([]TemplateFile{}, files...)
	files2[0] = TemplateFile{Path: "main.go", Mode: 0o644, Content: "// edited\n"}
	tpl2, ref, err := s.SaveTemplate(NewTemplate{Slug: "go-basic", Title: "Go basic",
		StackTags: []string{"golang"}, Files: files2, SeedOrigin: "user"}, giUser)
	if err != nil || ref != nil {
		t.Fatalf("save v2: ref=%v err=%v", ref, err)
	}
	if tpl2.Version != 2 || tpl2.Origin != "system" {
		t.Fatalf("second save = %+v, want version 2 origin STILL system", tpl2)
	}
	if cur, err := s.GetTemplate("go-basic"); err != nil || cur.Version != 2 ||
		cur.Files[0].Content != "// edited\n" || cur.CreatedAt != tpl.CreatedAt {
		t.Fatalf("current after edit = %+v (%v)", cur, err)
	}
	ls, err := s.ListTemplates()
	if err != nil || len(ls) != 1 {
		t.Fatalf("ListTemplates = %+v (%v)", ls, err)
	}

	// Row-and-history atomicity: the pair lives in ONE transaction — a
	// rollback leaves NEITHER the bumped row NOR the phantom revision.
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`UPDATE scaffold_template SET version = 99 WHERE slug = 'go-basic'`); err != nil {
		t.Fatalf("staged bump: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO scaffold_template_revision (slug,version,files,actor_kind,actor_id,origin,at)
		VALUES ('go-basic',99,'[]','user','t1','system','2026-11-05T00:00:00Z')`); err != nil {
		t.Fatalf("staged revision: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if cur, err := s.GetTemplate("go-basic"); err != nil || cur.Version != 2 {
		t.Fatalf("the rolled-back bump moved the row: %+v (%v)", cur, err)
	}
	if revs, err := s.TemplateRevisions("go-basic"); err != nil || len(revs) != 2 {
		t.Fatalf("the rolled-back revision exists: %+v (%v)", revs, err)
	}

	// Never deleted; history never rewritten.
	if err := g.Exec(`DELETE FROM scaffold_template WHERE slug = 'go-basic'`).Error; err == nil ||
		!strings.Contains(err.Error(), "never deleted") {
		t.Fatalf("template delete: %v, want the never-deleted abort", err)
	}
	if err := g.Exec(`UPDATE scaffold_template_revision SET files = '[]' WHERE slug = 'go-basic' AND version = 1`).Error; err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Fatalf("revision rewrite: %v, want the append-only abort", err)
	}
	// Refusals before SQL: bad slug, bad path, no files.
	if _, ref, _ := s.SaveTemplate(NewTemplate{Slug: "Go_Bad", Title: "x", Files: files}, giUser); ref == nil || ref.Kind != "slug-invalid" {
		t.Fatalf("slug 'Go_Bad': ref=%v, want slug-invalid", ref)
	}
	if _, ref, _ := s.SaveTemplate(NewTemplate{Slug: "evil", Title: "x",
		Files: []TemplateFile{{Path: "../escape", Mode: 0o644}}}, giUser); ref == nil || ref.Kind != "template-files" {
		t.Fatalf("path '../escape': ref=%v, want template-files", ref)
	}
	if _, ref, _ := s.SaveTemplate(NewTemplate{Slug: "empty", Title: "x"}, giUser); ref == nil || ref.Kind != "template-files" {
		t.Fatalf("no files: ref=%v, want template-files", ref)
	}
	if _, ref, _ := s.SaveTemplate(NewTemplate{Slug: "ghost", Title: "x", Files: files},
		Actor{Kind: "ghost", ID: "g"}); ref == nil || ref.Kind != "actor-unknown" {
		t.Fatalf("ghost author: ref=%v, want actor-unknown", ref)
	}
	// A fresh slug without SeedOrigin is the operator's own.
	fresh, ref, err := s.SaveTemplate(NewTemplate{Slug: "mine", Title: "Mine", Files: files}, giUser)
	if err != nil || ref != nil || fresh.Origin != "user" {
		t.Fatalf("unseeded save = %+v ref=%v err=%v, want origin user", fresh, ref, err)
	}
}

// giChecksum hashes everything the rebuild must carry across the vintage
// boundary, in one canonical spelling (S50's drill mechanism, the goal and
// roadmap tables this migration rebuilds).
func giChecksum(t *testing.T, s *Store) string {
	t.Helper()
	h := sha256.New()
	for _, q := range []string{
		`SELECT id, component_id, epic_id, version, title, type, body_note, source_kind,
		        source_ref, source_loop, source_step, review_state, created_by_kind,
		        created_by_id, created_at, updated_at FROM goal ORDER BY id, version`,
		`SELECT goal_id, goal_version, component_id, rationale, estimate_usd, confidence, basis
		   FROM goal_component ORDER BY goal_id, goal_version, component_id`,
		`SELECT id, component_root, title, epic, status, priority_state, priority_label,
		        estimate_min, estimate_max, estimate_basis, provenance, brief_path,
		        plan_path, loop_slug, created_at, updated_at FROM roadmap_item ORDER BY id`,
	} {
		rows, err := s.GormDB().Raw(q).Rows()
		if err != nil {
			t.Fatalf("checksum query: %v", err)
		}
		cols, _ := rows.Columns()
		fmt.Fprintf(h, "%s\n", q)
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("checksum scan: %v", err)
			}
			for i, v := range vals {
				fmt.Fprintf(h, "%d:%v|", i, v)
			}
			fmt.Fprintln(h)
		}
		rows.Close()
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestGoalIntakeMigrationDrillFreshEqualsUpgraded — T1's migration-drill:
// seed a PRE-goal_intake store through the store's own API (the tables the
// rebuild touches, with rows: goal, goal_component, roadmap_item), reopen it
// (the migration runs), and demand (a) the data identical, (b) the widened
// CHECKs LIVE — 'intake' lands on both rebuilt tables immediately — and
// (c) the shape equal to a fresh database, catalog included.
func TestGoalIntakeMigrationDrillFreshEqualsUpgraded(t *testing.T) {
	all := Migrations()
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Name, "_goal_intake") {
			idx = i
		}
	}
	if idx != 96 { // LITERAL: this file is the 97th (45 frozen + 52 files ⇒ index 96).
		t.Fatalf("goal_intake sits at index %d; T1 pinned 96 — the list moved, re-check the literal", idx)
	}

	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	applyMigrations(t, old.GormDB(), all[:idx])
	rootID := seedRoadmapComponent(t, old, "before", "/srv/before")
	s57SeedChild(t, old.GormDB(), "kid-of-before", rootID) // so the CT10 probe below is not vacuous
	goal, ref, err := old.CreateGoal(NewGoal{ComponentID: rootID, Title: "a goal from before",
		Type: "chore", SourceKind: "manual", BodyNote: "seeded at the prior vintage",
		Components: []GoalComponent{{ComponentID: rootID, Rationale: "it is the project"}},
		By:         giUser, Submit: true})
	if err != nil || ref != nil {
		t.Fatalf("seed the goal: ref=%v err=%v", ref, err)
	}
	if goal.ReviewState != "in_review" {
		t.Fatalf("Submit:true landed %q", goal.ReviewState)
	}
	epic, basis := "S61", "two tables and a trigger"
	lo, hi := 1, 2
	if _, err := old.CreateRoadmapItem(NewRoadmapItemRow{
		ComponentRoot: "/srv/before", Title: "an item from before", Epic: &epic,
		Provenance: "triaged", EstimateMin: &lo, EstimateMax: &hi, EstimateBasis: &basis,
	}); err != nil {
		t.Fatalf("seed the roadmap item: %v", err)
	}
	before := giChecksum(t, old)
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	s, err := Open(Options{Dir: dir, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("migrating open through goal_intake: %v", err)
	}
	defer func() { _ = s.Close() }()
	if after := giChecksum(t, s); after != before {
		t.Fatalf("the rebuild MOVED data across the vintage boundary:\n before %s\n after  %s", before, after)
	}

	// (b) The widened CHECKs are LIVE, not merely present.
	if _, ref, err := s.CreateGoal(NewGoal{ComponentID: rootID, Title: "filed by an intake",
		Type: "feature", SourceKind: "intake", SourceRef: "int-drill", By: giUser, Submit: true}); err != nil || ref != nil {
		t.Fatalf("goal with source_kind 'intake' after the upgrade: ref=%v err=%v", ref, err)
	}
	if _, err := s.CreateRoadmapItem(NewRoadmapItemRow{
		ComponentRoot: "/srv/before", Title: "filed by the same intake", Provenance: "intake",
	}); err != nil {
		t.Fatalf("roadmap item with provenance 'intake' after the upgrade: %v", err)
	}
	// The CT10 root-only triggers survived their own DROP TABLE.
	g := s.GormDB()
	insertErr := g.Exec(`INSERT INTO goal (id,component_id,version,title,type,source_kind,review_state,created_by_kind,created_by_id,created_at,updated_at)
		SELECT 'goal-child-sneak', id, 1, 'x', 'chore', 'manual', 'draft', 'user', 'x', '2026-11-05T00:00:00Z', '2026-11-05T00:00:00Z'
		FROM component WHERE parent_id IS NOT NULL LIMIT 1`).Error
	if insertErr == nil {
		t.Fatal("a child-rooted goal landed after the rebuild; the root-only trigger died with the DROP TABLE")
	}
	if !strings.Contains(insertErr.Error(), "belong to a root component") {
		t.Fatalf("child-goal insert failed with the wrong words: %v", insertErr)
	}
	// The parked goal_component rows are home again (seeded with the goal).
	if n := countTable(t, g, "goal_component"); n != 1 {
		t.Fatalf("the park-and-restore carried %d component rows, want the seeded one", n)
	}

	// (c) Fresh equals upgraded, and the whole new catalog is present.
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}
	for _, tb := range []string{"charter_revision", "goal_criterion", "intake", "intake_step",
		"scaffold_template", "scaffold_template_revision"} {
		if n := countCatalogueEntries(t, g, "table", tb); n != 1 {
			t.Fatalf("table %s: %d entries, want 1", tb, n)
		}
	}
	for _, ix := range []string{"idx_charter_revision_current", "idx_intake_root"} {
		if n := countCatalogueEntries(t, g, "index", ix); n != 1 {
			t.Fatalf("index %s: %d entries, want 1", ix, n)
		}
	}
	for _, tg := range []string{"charter_revision_no_update", "charter_revision_no_delete",
		"charter_root_only", "intake_root_once", "intake_step_no_update", "intake_step_no_delete",
		"scaffold_template_no_delete", "scaffold_template_revision_no_update",
		"scaffold_template_revision_no_delete", "goal_root_only", "goal_root_only_update"} {
		if n := countCatalogueEntries(t, g, "trigger", tg); n != 1 {
			t.Fatalf("trigger %s: %d entries, want 1 (the rebuild must recreate the goal pair byte-identically)", tg, n)
		}
	}

	// AUTOINCREMENT needs no ceremony: the explicit-id INSERTs raised the
	// sequence, and the next born id cannot reuse a dead one.
	maxBefore, err := s.CreateRoadmapItem(NewRoadmapItemRow{ComponentRoot: "/srv/before",
		Title: "highest so far", Provenance: "authored"})
	if err != nil {
		t.Fatalf("seed the highest id: %v", err)
	}
	next, err := s.CreateRoadmapItem(NewRoadmapItemRow{ComponentRoot: "/srv/before",
		Title: "born after the rebuild", Provenance: "authored"})
	if err != nil {
		t.Fatalf("post-rebuild insert: %v", err)
	}
	if next.ID <= maxBefore.ID {
		t.Fatalf("id %d reused territory below %d after the rebuild", next.ID, maxBefore.ID)
	}

	// The drill's remaining clauses: pragma_foreign_key_check empty after
	// two rebuilt tables, and an idempotent re-open — the migrated store
	// re-opens changing nothing.
	if rows, err := g.Raw(`PRAGMA foreign_key_check`).Rows(); err != nil {
		t.Fatalf("pragma_foreign_key_check: %v", err)
	} else {
		n := 0
		for rows.Next() {
			n++
		}
		rows.Close()
		if n != 0 {
			t.Fatalf("%d rows break a foreign key after the rebuild", n)
		}
	}
	onS := giChecksum(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("close for the re-open: %v", err)
	}
	s2, err := Open(Options{Dir: dir, Env: emptyEnv, Clock: fixedClock()})
	if err != nil {
		t.Fatalf("idempotent re-open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if again := giChecksum(t, s2); again != onS {
		t.Fatalf("the re-open moved data:\n first  %s\n again  %s", onS, again)
	}
}

// giCheckWords extracts the word list of one CHECK clause's IN (...) — the
// live DDL as the only truth the Go mirrors must match.
func giCheckWords(t *testing.T, s *Store, table, column string) []string {
	t.Helper()
	var ddl string
	if err := s.r.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&ddl); err != nil {
		t.Fatalf("read the DDL of %s: %v", table, err)
	}
	re := regexp.MustCompile(column + `[^)]*IN \(([^)]*)\)`)
	m := re.FindStringSubmatch(ddl)
	if m == nil {
		t.Fatalf("%s carries no CHECK over %s: %s", table, column, ddl)
	}
	var words []string
	for _, w := range strings.Split(m[1], ",") {
		words = append(words, strings.Trim(strings.TrimSpace(w), "'"))
	}
	sort.Strings(words)
	return words
}

func giSorted(set map[string]bool) []string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestIntakeVocabulariesMatchTheMigration — the house lock: every closed word
// set the new tables carry is mirrored in Go, and the widened ones read back
// 'intake' where the Go lists grew (F-2, P-C).
func TestIntakeVocabulariesMatchTheMigration(t *testing.T) {
	_, s := testStore(t)
	for _, tc := range []struct {
		table, column string
		want          []string
	}{
		{"goal", "source_kind", giSorted(allSourceKinds)},
		{"roadmap_item", "provenance", []string{"authored", "intake", "triaged", "triggered"}},
		{"charter_revision", "source", giSorted(allCharterSources)},
		{"charter_revision", "actor_kind", []string{"agent", "trigger", "user"}},
		{"intake_step", "outcome", giSorted(allStepOutcomes)},
		{"intake", "created_by_kind", []string{"user"}},
		{"goal_criterion", "check_kind", giSorted(allCriterionChecks)},
		{"scaffold_template", "origin", []string{"system", "user"}},
	} {
		got := giCheckWords(t, s, tc.table, tc.column)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("%s.%s: schema [%s] ≠ Go mirror [%s]", tc.table, tc.column,
				strings.Join(got, ", "), strings.Join(tc.want, ", "))
		}
	}
}
