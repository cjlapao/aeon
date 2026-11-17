package store

// S51 T1 (TM2/TM3) — the bug plane's audit at the store level: the append-
// only net, the closed sets including the one the siblings do not carry (an
// agent actor, legal ONLY on `file`), the one-transaction rule, the no-row
// on no-op discipline, and the filed_by name→slug migration. The roadmap's
// roadmap_change_test.go is the shape; the agent's migration test is the
// migration's shape. Deviation of record: BugChangeAppend exists for the
// validate dispatch ALONE — starting an attempt moves no bug row, so that
// op has no pair to ride; every real write pairs through BugWithTx.

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
)

var bugAuditPerson = BugActor{Kind: "user", ID: "42"}

// bugChangeMigrationIndex is the migration's position BY NAME, not by
// "the last" (S57 T7's lesson: any later slice makes the positional
// shortcut drop the wrong file).
func bugChangeMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_bug_change") {
			return i
		}
	}
	panic("the bug_change migration is missing from Migrations()")
}

// TestBugChangeMigrationFreshAndUpgraded — the migration applies fresh and
// on an upgrade, and both arrive at the same shape.
func TestBugChangeMigrationFreshAndUpgraded(t *testing.T) {
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}
	assertBugChangeCatalogue(t, fresh)

	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	all := Migrations()
	applyMigrations(t, old.GormDB(), all[:bugChangeMigrationIndex()])
	var absent int
	if err := old.GormDB().Raw(`SELECT count(*) FROM bug_change`).Scan(&absent).Error; err == nil {
		t.Fatal("the vintage store has bug_change before its migration — the vintage is wrong")
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
	assertBugChangeCatalogue(t, s)
}

// assertBugChangeCatalogue names the table, its index and both triggers in
// sqlite_master — the migration's whole content, counted.
func assertBugChangeCatalogue(t *testing.T, s *Store) {
	t.Helper()
	g := s.GormDB()
	if n := countCatalogueEntries(t, g, "table", "bug_change"); n != 1 {
		t.Fatalf("bug_change table: %d entries, want 1", n)
	}
	if n := countCatalogueEntries(t, g, "index", "idx_bug_change_bug"); n != 1 {
		t.Fatalf("the (bug_id, id) index: %d entries, want 1", n)
	}
	for _, trig := range []string{"bug_change_no_update", "bug_change_no_delete"} {
		if n := countCatalogueEntries(t, g, "trigger", trig); n != 1 {
			t.Fatalf("trigger %s: %d entries, want 1", trig, n)
		}
	}
}

// seedBugComponent registers a component WITH its root — the parent row the
// bug's component_root FK needs (seedRoadmapComponent's shape).
func seedBugComponent(t *testing.T, s *Store, slug, root string) {
	t.Helper()
	if _, err := s.CreateComponent(NewComponent{Name: slug, Slug: slug, Root: root}); err != nil {
		t.Fatalf("seed component %s at %s: %v", slug, root, err)
	}
}

func seedBugPlain(t *testing.T, s *Store, root, title string) BugRow {
	t.Helper()
	row, err := s.CreateBug(NewBugRow{ComponentRoot: root, Title: title, Body: "body", FiledBy: "person"})
	if err != nil {
		t.Fatalf("seed bug %q: %v", title, err)
	}
	return row
}

func TestBugChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	root := "/repo/bcaudit"
	seedBugComponent(t, s, "bcaudit", root)
	bug := seedBugPlain(t, s, root, "audited once")
	err := s.BugWithTx(func(tx *sql.Tx) error {
		return BugChangeInsertTx(tx, BugAudit{BugID: bug.ID, Op: "priority", Summary: "set bug 1 priority to \"known\"", Actor: bugAuditPerson}, s.Now())
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	changes, err := s.BugChangeList(context.Background(), bug.ID, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("read back: %d rows, err %v", len(changes), err)
	}
	if _, err := s.w.Exec(`UPDATE bug_change SET summary = 'rewritten history' WHERE id = ?`, changes[0].ID); err == nil {
		t.Fatal("an UPDATE of an audit row was accepted — the trigger is missing")
	} else if !strings.Contains(err.Error(), "bug_change is append-only (S51 TM2)") {
		t.Errorf("UPDATE refused with the wrong sentence: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM bug_change WHERE id = ?`, changes[0].ID); err == nil {
		t.Fatal("a DELETE of an audit row was accepted — the trigger is missing")
	} else if !strings.Contains(err.Error(), "bug_change is append-only (S51 TM2)") {
		t.Errorf("DELETE refused with the wrong sentence: %v", err)
	}
	// The positive control: appending a second row is lawful.
	if err := s.BugWithTx(func(tx *sql.Tx) error {
		return BugChangeInsertTx(tx, BugAudit{BugID: bug.ID, Op: "transition", Summary: "moved bug 1 from \"unvalidated\" to \"reproduced\"", Actor: BugActor{Kind: "system", ID: "reactor"}}, s.Now())
	}); err != nil {
		t.Fatalf("the second append was refused: %v", err)
	}
}

func TestBugChangeClosedSetsAgentCarriesFileOnly(t *testing.T) {
	s := openTemp(t)
	root := "/repo/bcclosed"
	seedBugComponent(t, s, "bcclosed", root)
	bug := seedBugPlain(t, s, root, "closed sets")

	// The eighth op word, the fifth actor kind, the blank and oversized
	// summary, the blank actor id — each refused by the CHECK, raw SQL.
	cases := []struct {
		why      string
		op, kind string
		summary  string
	}{
		{"an eighth op word", "close", "user", "closed bug 1"},
		{"a fifth actor kind", "file", "robot", "filed bug 1"},
		{"the blank summary", "file", "user", ""},
		{"the oversized summary", "file", "user", strings.Repeat("long ", 300)},
		{"the blank actor id", "file", "", "filed bug 1"},
	}
	for _, c := range cases {
		_, err := s.w.Exec(insertBugChangeSQL, bug.ID, c.op, c.summary, c.kind, "x", s.Now())
		if err == nil {
			t.Fatalf("%s was accepted at the raw writer", c.why)
		}
		if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("%s refused for the wrong reason: %v", c.why, err)
		}
	}
	// TM2's net in the database: agent on any op but file dies here.
	if _, err := s.w.Exec(insertBugChangeSQL, bug.ID, "transition", "moved bug 1", "agent", "golang-engineer", s.Now()); err == nil {
		t.Fatal("an agent carried a transition — the CHECK tying agent to file has no teeth")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("agent-on-transition refused for the wrong reason: %v", err)
	}
	// And the positive controls, so the CHECK tightens ONLY that pair.
	if _, err := s.w.Exec(insertBugChangeSQL, bug.ID, "file", "filed bug 1", "agent", "golang-engineer", s.Now()); err != nil {
		t.Fatalf("the agent's one lawful op was refused: %v", err)
	}
	if _, err := s.w.Exec(insertBugChangeSQL, bug.ID, "validate", "started a validation attempt on bug 1", "system", "reactor", s.Now()); err != nil {
		t.Fatalf("system on validate was refused: %v", err)
	}
}

func TestBugChangeListIsNewestFirstWithABound(t *testing.T) {
	s := openTemp(t)
	root := "/repo/bclist"
	seedBugComponent(t, s, "bclist", root)
	bug := seedBugPlain(t, s, root, "walked")
	for _, op := range []string{"file", "priority", "duplicate", "validate"} {
		if err := s.BugWithTx(func(tx *sql.Tx) error {
			return BugChangeInsertTx(tx, BugAudit{BugID: bug.ID, Op: op, Summary: "walked " + op, Actor: bugAuditPerson}, s.Now())
		}); err != nil {
			t.Fatalf("append %s: %v", op, err)
		}
	}
	changes, err := s.BugChangeList(context.Background(), bug.ID, 3)
	if err != nil || len(changes) != 3 {
		t.Fatalf("limit 3 answered %d rows, err %v", len(changes), err)
	}
	if changes[0].Op != "validate" || changes[2].Op != "priority" {
		t.Fatalf("not newest-first: %s … %s", changes[0].Op, changes[2].Op)
	}
	all, err := s.BugChangeList(context.Background(), bug.ID, 0)
	if err != nil || len(all) != 4 {
		t.Fatalf("default limit answered %d rows, err %v — want all 4", len(all), err)
	}
	none, err := s.BugChangeList(context.Background(), 9999, 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("a bug with no history answered %d rows, err %v", len(none), err)
	}
}

func TestCreateBugAuditedWritesItsFileRow(t *testing.T) {
	s := openTemp(t)
	root := "/repo/bccreate"
	seedBugComponent(t, s, "bccreate", root)
	created, err := s.CreateBugAudited(NewBugRow{ComponentRoot: root, Title: "born audited", Body: "b", FiledBy: "person"},
		BugAudit{Actor: bugAuditPerson})
	if err != nil {
		t.Fatalf("audited create: %v", err)
	}
	changes, err := s.BugChangeList(context.Background(), created.ID, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("the create wrote %d rows, want 1 (err %v)", len(changes), err)
	}
	c := changes[0]
	if c.Op != "file" || c.BugID != created.ID {
		t.Fatalf("row op %q bug %d, want file on %d", c.Op, c.BugID, created.ID)
	}
	if c.Summary != "filed bug "+itoa64(created.ID) {
		t.Fatalf("summary %q — the fixed template names the id the row carries", c.Summary)
	}
	if c.ActorKind != "user" || c.ActorID != "42" || c.OccurredAt == "" {
		t.Fatalf("audit carries %s/%s at %q", c.ActorKind, c.ActorID, c.OccurredAt)
	}
	// An agent files — its own slug, the one op its kind may carry.
	filed, err := s.CreateBugAudited(NewBugRow{ComponentRoot: root, Title: "agent files", Body: "b", FiledBy: "golang-engineer"},
		BugAudit{Actor: BugActor{Kind: "agent", ID: "golang-engineer"}})
	if err != nil {
		t.Fatalf("the agent's lawful filing was refused: %v", err)
	}
	if filed.FiledBy != "golang-engineer" {
		t.Fatalf("filed_by %q", filed.FiledBy)
	}
}

func TestBugAuditAndWriteAreOneTransaction(t *testing.T) {
	// The bug plane's twin of roadmap_change_test.go's TestAuditAndWriteAreOneTransaction
	// — that name is taken in this package by S50, so the bug's proof carries
	// the plane prefix (recorded deviation, T1 Notes).
	// Force the audit INSERT to fail (a summary past the 1024 CHECK) and
	// prove the BUG did not move — the write and its audit are one
	// transaction or they are nothing.
	s := openTemp(t)
	root := "/repo/bcatomic"
	seedBugComponent(t, s, "bcatomic", root)
	bug := seedBugPlain(t, s, root, "stays put")
	label := "P1"
	wrote, err := s.SetBugPriorityAudited(bug.ID, root, "known", &label, BugAudit{
		Summary: strings.Repeat("far too long ", 200), Actor: bugAuditPerson,
	})
	if wrote {
		t.Fatal("the priority wrote while its audit failed")
	}
	if err == nil {
		t.Fatal("an over-bound audit summary was accepted — the CHECK has no teeth through the writer")
	}
	fresh, found, err := s.BugByID(bug.ID)
	if err != nil || !found {
		t.Fatalf("re-read: found=%v err %v", found, err)
	}
	if fresh.PriorityState != "unknown" {
		t.Fatalf("the bug moved to %q while its audit failed — the transaction is not one", fresh.PriorityState)
	}
	changes, err := s.BugChangeList(context.Background(), bug.ID, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("a failed write left %d audit rows, want 0 (err %v)", len(changes), err)
	}
}

func TestAuditedWritersEarnNoRowOnNoOp(t *testing.T) {
	s := openTemp(t)
	root := "/repo/bcnoop"
	seedBugComponent(t, s, "bcnoop", root)
	bug := seedBugPlain(t, s, root, "the honest loser")

	// The CAS loser: wrong FROM — nothing moves, nothing audits, no error.
	claimed, err := s.TransitionBugAudited(bug.ID, "reproduced", "not-reproduced", BugAudit{
		Summary: "moved bug 1 from \"reproduced\" to \"not-reproduced\"", Actor: bugAuditPerson,
	})
	if err != nil || claimed {
		t.Fatalf("a move from a state the row does not hold: claimed=%v err %v", claimed, err)
	}
	// The absent row: the root scope misses — same discipline.
	wrote, err := s.SetBugPriorityAudited(bug.ID, "/repo/elsewhere", "known", nil, BugAudit{
		Summary: "set bug 1 priority to \"known\"", Actor: bugAuditPerson,
	})
	if err != nil || wrote {
		t.Fatalf("a write outside the row's root: wrote=%v err %v", wrote, err)
	}
	changes, err := s.BugChangeList(context.Background(), bug.ID, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("no-op writes left %d audit rows, want 0 (err %v) — an audit of nothing is noise", len(changes), err)
	}
}

func TestAgentActorRefusedOffFileBeforeSQL(t *testing.T) {
	s := openTemp(t)
	root := "/repo/bcagent"
	seedBugComponent(t, s, "bcagent", root)
	bug := seedBugPlain(t, s, root, "no machine sets priority")
	label := "P1"
	_, err := s.SetBugPriorityAudited(bug.ID, root, "known", &label, BugAudit{
		Summary: "set bug 1 priority to \"known\"", Actor: BugActor{Kind: "agent", ID: "golang-engineer"},
	})
	if err == nil || !strings.Contains(err.Error(), "may only FILE") {
		t.Fatalf("an agent-authored priority: err %v — want the Go twin of the CHECK naming the law", err)
	}
	claimed, err := s.TransitionBugAudited(bug.ID, "unvalidated", "reproduced", BugAudit{
		Summary: "moved bug 1", Actor: BugActor{Kind: "agent", ID: "golang-engineer"},
	})
	if err == nil || claimed || !strings.Contains(err.Error(), "may only FILE") {
		t.Fatalf("an agent-authored transition: claimed=%v err %v", claimed, err)
	}
	fresh, _, _ := s.BugByID(bug.ID)
	if fresh.ValidationState != "unvalidated" {
		t.Fatalf("the agent-authored transition moved the bug to %q anyway", fresh.ValidationState)
	}
	// The malformed audits ride the same gate.
	if _, err := s.SetBugDuplicateOfAudited(bug.ID, root, nil, BugAudit{
		Summary: "cleared bug 1's duplicate", Actor: BugActor{Kind: "ghost", ID: "x"}}); err == nil {
		t.Fatal("an actor kind outside the four was accepted")
	}
	if err := s.BugChangeAppend(BugAudit{BugID: bug.ID, Op: "validate", Summary: "", Actor: bugAuditPerson}); err == nil {
		t.Fatal("the blank summary passed the pre-SQL check")
	}
}

func TestBugChangeAppendIsTheValidateException(t *testing.T) {
	// The ONE audit that travels outside a pair: starting an attempt moves
	// no bug row, so `validate` has no write to ride. Nothing else may use
	// this path; the op is still refused for a lawless actor.
	s := openTemp(t)
	root := "/repo/bcvalidate"
	seedBugComponent(t, s, "bcvalidate", root)
	bug := seedBugPlain(t, s, root, "attempt starts")
	before, _, _ := s.BugByID(bug.ID)
	if err := s.BugChangeAppend(BugAudit{BugID: bug.ID, Op: "validate",
		Summary: "started a validation attempt on bug " + itoa64(bug.ID), Actor: bugAuditPerson}); err != nil {
		t.Fatalf("the standalone validate append: %v", err)
	}
	after, _, _ := s.BugByID(bug.ID)
	if after.ValidationState != before.ValidationState || after.UpdatedAt != before.UpdatedAt {
		t.Fatal("the validate append moved the bug row — it must audit an act, not touch a state")
	}
	if err := s.BugChangeAppend(BugAudit{BugID: bug.ID, Op: "validate",
		Summary: "an agent attempted", Actor: BugActor{Kind: "agent", ID: "golang-engineer"}}); err == nil {
		t.Fatal("an agent appended validate — TM6 says an attempt is dispatched by a person")
	}
	if err := s.BugChangeAppend(BugAudit{BugID: bug.ID, Op: "file",
		Summary: "filed bug 1 out of band", Actor: bugAuditPerson}); err == nil {
		t.Fatal("BugChangeAppend carried a paired op — it exists for the validate dispatch ONLY")
	}
}

// itoa64 renders an id for the fixed templates.
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
