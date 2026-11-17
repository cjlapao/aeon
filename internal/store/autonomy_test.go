package store

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// autonomy_test.go — the S56 T3 store half. The migration (autonomy.sql) is the
// SQL source; these tests read it back. Every setup INSERT and the assertions
// under test go through g.Exec on the raw handle so the rows are real and the
// per-row triggers fire (an UPDATE that matches zero rows never fires a
// per-row trigger — a false pass). Store-API tests use openTemp (a real store,
// fixed clock); trigger/CHECK tests use openMigrated (a raw handle with every
// migration applied).

// seedGrantRow inserts a minimal live allow grant through the store's writer,
// for the store-API tests that need a component and a grant on it.
func seedRoot(t *testing.T, s *Store, workspace string) Component {
	t.Helper()
	w, err := s.CreateComponent(NewComponent{Name: workspace, Slug: strings.TrimPrefix(workspace, "/"), Root: workspace})
	if err != nil {
		t.Fatalf("create component %s: %v", workspace, err)
	}
	if !w.OK() {
		t.Fatalf("create component %s: %s", workspace, w.Reason)
	}
	return w.Component
}

func (s *Store) seedAutonomyGrant(t *testing.T, nodeID string, in NewAutonomyGrant) AutonomyGrant {
	t.Helper()
	in.NodeID = nodeID
	g, ref, err := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, in)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if ref != nil {
		t.Fatalf("grant refused: %s", ref.Sentence)
	}
	return g
}

func TestMigrationCreatesTheFourTables(t *testing.T) {
	g := openMigrated(t)
	for _, table := range []string{"autonomy_grant", "autonomy_change", "autonomy_use", "loop_autonomy_pin"} {
		if countCatalogueEntries(t, g, "table", table) != 1 {
			t.Errorf("table %s is not present", table)
		}
	}
	for _, index := range []string{"idx_autonomy_grant_node", "idx_autonomy_change_root", "idx_autonomy_use_grant", "idx_autonomy_use_loop"} {
		if countCatalogueEntries(t, g, "index", index) != 1 {
			t.Errorf("index %s is not present", index)
		}
	}
	// the two append-only/immutable trigger pairs and the two component triggers
	for _, trig := range []string{
		"autonomy_change_no_update", "autonomy_change_no_delete",
		"autonomy_use_no_update", "autonomy_use_no_delete",
		"loop_autonomy_pin_no_update", "loop_autonomy_pin_no_delete",
		"autonomy_grant_allow_root_only", "autonomy_grant_no_attach",
	} {
		if countCatalogueEntries(t, g, "trigger", trig) != 1 {
			t.Errorf("trigger %s is not present", trig)
		}
	}
	// the columns the store reads must be there (a drift that drops one reads
	// back as a zero value, which looks like data loss). Columns are not in
	// sqlite_master, so this goes through pragma_table_info.
	for _, col := range []string{"subject_kind", "state", "conditions", "expires_at", "revoked_at", "revoked_by_kind"} {
		if n := countPragmarow(t, g, "autonomy_grant", col); n != 1 {
			t.Errorf("autonomy_grant column %s is missing", col)
		}
	}
}

// countPragmarow counts one named column of a table via pragma_table_info —
// sqlite_master has no 'column' row kind, so a column exists or does not by
// the pragma, not the catalogue.
func countPragmarow(t *testing.T, g *gorm.DB, table, col string) int {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, col).Scan(&n).Error; err != nil {
		t.Fatalf("read columns of %s: %v", table, err)
	}
	return n
}

func TestCreateGrantWritesRowAndChangeInOneTx(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	g := s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{
		SubjectKind: "action", Subject: "git.commitAndPush", State: "allow",
		Note: "a real note", Conditions: `{"requireBudget":true}`,
	})
	if countTable(t, s.gw, "autonomy_grant") != 1 {
		t.Fatalf("the grant row was not written")
	}
	changes, err := s.AutonomyChanges(AutonomyChangeFilter{GrantID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("the change row was not written (or written more than once): %d", len(changes))
	}
	if changes[0].ChangeKind != "grant" || changes[0].GrantID != g.ID || changes[0].RootID != root.ID {
		t.Errorf("change row = %+v, want kind=grant on the root", changes[0])
	}
}

// TestAuditSummaryIsATemplate proves SEC-033: the audit summary is a fixed
// template from ids and enums, and the user text (the note) never appears in
// it — even when the note is an injection or a grant-id look-alike.
func TestAuditSummaryIsATemplate(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	nasty := "'); DROP TABLE autonomy_grant; --\nag-" + "fakeid" + " newline"
	g := s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{
		SubjectKind: "action", Subject: "git.commitAndPush", State: "allow",
		Note: nasty, Conditions: `{}`,
	})
	changes, err := s.AutonomyChanges(AutonomyChangeFilter{GrantID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("want one change row, got %d", len(changes))
	}
	summary := changes[0].Summary
	for _, s := range []string{")", "DROP", "newline", "fakeid"} {
		if strings.Contains(summary, s) {
			t.Errorf("the audit summary %q carries user text %q (SEC-033)", summary, s)
		}
	}
	want := changeSummary("grant", "git.commitAndPush", root.ID, "u1")
	if summary != want {
		t.Errorf("summary = %q, want the fixed template %q", summary, want)
	}
}

func TestAutonomyChangeAndUseAreAppendOnly(t *testing.T) {
	g := openMigrated(t)
	seedComponent(t, g, "cmp-1")
	if err := g.Exec(`INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('ag-1','cmp-1','action','git.commitAndPush','allow','user','u1','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	if err := g.Exec(`INSERT INTO autonomy_change (at,root_id,grant_id,change_kind,actor_kind,actor_id,summary)
		VALUES ('2026-11-01T09:00:00Z','cmp-1','ag-1','grant','user','u1','a fixed template')`).Error; err != nil {
		t.Fatalf("seed change: %v", err)
	}
	if err := g.Exec(`INSERT INTO autonomy_use (at,loop_id,grant_id,grant_rev,action,phase,evidence_ref)
		VALUES ('2026-11-01T09:00:00Z','l-1','ag-1',0,'git.commitAndPush','begun','ref')`).Error; err != nil {
		t.Fatalf("seed use: %v", err)
	}
	if err := g.Exec(`UPDATE autonomy_change SET summary = 'tampered' WHERE id = 1`).Error; err == nil {
		t.Errorf("an UPDATE of an append-only change was allowed")
	}
	if err := g.Exec(`DELETE FROM autonomy_change WHERE id = 1`).Error; err == nil {
		t.Errorf("a DELETE of an append-only change was allowed")
	}
	if err := g.Exec(`UPDATE autonomy_use SET action = 'x' WHERE id = 1`).Error; err == nil {
		t.Errorf("an UPDATE of an append-only use was allowed")
	}
	if err := g.Exec(`DELETE FROM autonomy_use WHERE id = 1`).Error; err == nil {
		t.Errorf("a DELETE of an append-only use was allowed")
	}
}

// TestPinIsImmutable proves the pin is written once and never moved: a raw
// UPDATE or DELETE aborts (AL6/AL8), and no Go function updates or deletes it
// (the only writer is the one INSERT in CreatePinnedLoop).
func TestPinIsImmutable(t *testing.T) {
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO loop_autonomy_pin (loop_id,root_id,rev,origin,labels,allowed,pinned_at)
		VALUES ('l-1','cmp-1',1,'schedule','[]','[{"grant":"ag-1","subject":"x","conditions":{}}]','2026-11-01T09:00:00Z')`).Error; err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	if err := g.Exec(`UPDATE loop_autonomy_pin SET rev = 2 WHERE loop_id = 'l-1'`).Error; err == nil {
		t.Errorf("an UPDATE of an immutable pin was allowed")
	}
	if err := g.Exec(`DELETE FROM loop_autonomy_pin WHERE loop_id = 'l-1'`).Error; err == nil {
		t.Errorf("a DELETE of an immutable pin was allowed")
	}
	// the only writer is the one INSERT in CreatePinnedLoop (blueprint.go).
	files := packageFiles(t)
	var writers int
	for name, src := range files {
		if strings.Contains(src, "INSERT INTO loop_autonomy_pin") {
			writers++
			if name != "blueprint.go" {
				t.Errorf("%s writes a pin — only CreatePinnedLoop may", name)
			}
		}
		for _, probe := range []string{"UPDATE loop_autonomy_pin", "DELETE FROM loop_autonomy_pin"} {
			if strings.Contains(src, probe) {
				t.Errorf("%s mutates the pin: %s", name, probe)
			}
		}
	}
	if writers != 1 {
		t.Fatalf("found %d pin writers, want exactly one (blueprint.go)", writers)
	}
}

// TestPinWrittenInTheLoopTransaction proves the pin rides the loop's own
// transaction: a failing pin leaves no loop and no pin, a good one leaves both.
func TestPinWrittenInTheLoopTransaction(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	// a good pin: both the loop and the pin exist.
	good, err := s.CreatePinnedLoop(NewLoop{
		Slug: "good", ComponentRoot: root.ID,
		AutonomyPin: &AutonomyPin{RootID: root.ID, Rev: 1, Origin: "scheduler", Labels: `[]`, Allowed: `[]`},
	}, "bp-1", 1, "content", `{"pinned":true}`)
	if err != nil {
		t.Fatalf("create pinned loop with a good pin: %v", err)
	}
	if countTable(t, s.gw, "loop") == 0 || countTable(t, s.gw, "loop_autonomy_pin") == 0 {
		t.Errorf("the good pin should leave both a loop and a pin")
	}
	if good.ID == "" {
		t.Error("the good pin should return a loop id")
	}
	// a failing pin (a bad origin the CHECK refuses) leaves no loop and no pin.
	if _, err := s.CreatePinnedLoop(NewLoop{
		Slug: "bad", ComponentRoot: root.ID,
		AutonomyPin: &AutonomyPin{RootID: root.ID, Rev: 1, Origin: "not-an-origin", Labels: `[]`, Allowed: `[]`},
	}, "bp-2", 1, "content", `{"pinned":true}`); err == nil {
		t.Fatalf("a failing pin was accepted")
	}
	if n := countTable(t, s.gw, "loop"); n != 1 {
		t.Errorf("a failing pin left %d loop rows, want 1 (only the good loop)", n)
	}
}

// TestAllowOnlyOnARoot proves AL3 from both sides: the store refuses an allow
// on a child with a sentence (the door), and the trigger aborts a raw allow on
// a child (the backstop a store bypassed). A veto (deny) may sit on a child.
func TestAllowOnlyOnARoot(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	child, cerr := s.CreateComponent(NewComponent{Name: "child", Slug: "child", Root: "/child", ParentID: root.ID})
	if cerr != nil {
		t.Fatalf("create child: %v", cerr)
	}
	if !child.OK() {
		t.Fatalf("create child: %s", child.Reason)
	}
	// the store door: an allow on a child is refused before SQL.
	_, ref, err := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, NewAutonomyGrant{
		NodeID: child.Component.ID, SubjectKind: "action", Subject: "git.commitAndPush", State: "allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil {
		t.Errorf("an allow on a child was accepted")
	}
	if countTable(t, s.gw, "autonomy_grant") != 0 {
		t.Errorf("a refused allow wrote a grant row")
	}
	// the trigger backstop: a raw allow on a child aborts; a raw deny does not.
	g := openMigrated(t)
	if err := g.Exec(`INSERT INTO component (id,name,slug,parent_id,created_at,updated_at) VALUES ('r2','r','r',NULL,'t','t')`).Error; err != nil {
		t.Fatalf("seed root: %v", err)
	}
	if err := g.Exec(`INSERT INTO component (id,name,slug,parent_id,created_at,updated_at) VALUES ('c2','c','c','r2','t','t')`).Error; err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if err := g.Exec(`INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('ag-x','c2','action','git.commitAndPush','allow','user','u1','t')`).Error; err == nil {
		t.Errorf("a raw allow on a child was accepted (the trigger backstop did not fire)")
	}
	if err := g.Exec(`INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('ag-y','c2','action','git.commitAndPush','deny','user','u1','t')`).Error; err != nil {
		t.Errorf("a raw veto (deny) on a child was refused: %v", err)
	}
}

// TestDenyMaySitOnAChild proves AL3's OD4: a veto may sit on a child (a person
// can say "never on this repository" without touching the project).
func TestDenyMaySitOnAChild(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	child, cerr := s.CreateComponent(NewComponent{Name: "child", Slug: "child", Root: "/child", ParentID: root.ID})
	if cerr != nil {
		t.Fatalf("create child: %v", cerr)
	}
	if !child.OK() {
		t.Fatalf("create child: %s", child.Reason)
	}
	g, ref, err := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, NewAutonomyGrant{
		NodeID: child.Component.ID, SubjectKind: "action", Subject: "git.commitAndPush", State: "deny",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref != nil {
		t.Errorf("a veto on a child was refused: %s", ref.Sentence)
	}
	if g.ID == "" {
		t.Error("a veto on a child should be accepted")
	}
}

// TestAttachRefusedWhileAllowsActive proves AL10's S44-extra interaction: a
// component carrying an active allow may not be attached under another (the
// child would inherit a grant its new root never authorised), and revoking the
// allow releases it.
func TestAttachRefusedWhileAllowsActive(t *testing.T) {
	s := openTemp(t)
	rootA := seedRoot(t, s, "/a")
	rootB := seedRoot(t, s, "/b")
	g := s.seedAutonomyGrant(t, rootA.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "git.commitAndPush", State: "allow"})
	// while the allow is active, attaching rootA under rootB aborts (the trigger).
	if _, err := s.AttachComponent(rootA.ID, rootB.ID); err == nil {
		t.Errorf("attaching a component with an active allow was accepted")
	}
	// revoke the allow, then the attach is free.
	if _, _, ref, err := s.RevokeAutonomyGrant(Actor{Kind: "user", ID: "u1"}, g.ID, "released"); err != nil || ref != nil {
		t.Fatalf("revoke: %v %s", err, ref)
	}
	w, err := s.AttachComponent(rootA.ID, rootB.ID)
	if err != nil {
		t.Fatalf("attach after revoke: %v", err)
	}
	if !w.OK() {
		t.Errorf("attach after revoke was refused: %s", w.Reason)
	}
}

// TestNonUserActorRefusedBeforeSQL proves AU1 lock 3: a non-user/non-cli actor
// is refused before any SQL (no grant row, no change row), and the CHECK is
// the backstop a store bypassed.
func TestNonUserActorRefusedBeforeSQL(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	for _, kind := range []string{"agent", "trigger"} {
		_, ref, err := s.CreateAutonomyGrant(Actor{Kind: kind, ID: "a1"}, NewAutonomyGrant{
			NodeID: root.ID, SubjectKind: "action", Subject: "git.commitAndPush", State: "allow",
		})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if ref == nil {
			t.Errorf("%s was accepted", kind)
		}
	}
	if countTable(t, s.gw, "autonomy_grant") != 0 {
		t.Errorf("a refused actor wrote a grant row (SQL ran before the refusal)")
	}
	if countTable(t, s.gw, "autonomy_change") != 0 {
		t.Errorf("a refused actor wrote a change row (SQL ran before the refusal)")
	}
	// the CHECK is the backstop: a raw insert with a non-user created_by_kind aborts.
	g := openMigrated(t)
	seedComponent(t, g, "cmp-1")
	if err := g.Exec(`INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('ag-1','cmp-1','action','git.commitAndPush','allow','agent','a1','t')`).Error; err == nil {
		t.Errorf("a raw non-user created_by_kind was accepted (the CHECK is the backstop)")
	}
}

// TestCliActorCanDenyButNotGrant proves the state-aware actor door (AL13,
// corrected at S56 T9's pre-flight): an ALLOW is a widening and admits only a
// user, a DENY is a veto — the restrictive class of revoke — and admits a
// user or a cli, so the CLI's direct door can pull authority offline. The
// CHECK is the backstop: a raw allow row with a cli creator aborts, a raw
// deny row with one is admitted.
func TestCliActorCanDenyButNotGrant(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	_, ref, err := s.CreateAutonomyGrant(Actor{Kind: "cli", ID: "cli"}, NewAutonomyGrant{
		NodeID: root.ID, SubjectKind: "action", Subject: "forge.merge", State: "allow",
	})
	if err != nil {
		t.Fatalf("cli allow: %v", err)
	}
	if ref == nil || ref.Kind != "actor-not-human" {
		t.Fatalf("cli allow: ref = %+v, want actor-not-human — a widening admits only a user", ref)
	}
	if countTable(t, s.gw, "autonomy_grant") != 0 || countTable(t, s.gw, "autonomy_change") != 0 {
		t.Fatalf("a refused cli allow wrote rows — the refusal is not before the SQL")
	}

	g, ref, err := s.CreateAutonomyGrant(Actor{Kind: "cli", ID: "cli"}, NewAutonomyGrant{
		NodeID: root.ID, SubjectKind: "action", Subject: "forge.merge", State: "deny",
	})
	if err != nil {
		t.Fatalf("cli deny: %v", err)
	}
	if ref != nil {
		t.Fatalf("cli deny was refused: %s", ref.Sentence)
	}
	if g.CreatedByKind != "cli" {
		t.Errorf("cli deny: created_by_kind = %q, want cli", g.CreatedByKind)
	}
	changes, err := s.AutonomyChanges(AutonomyChangeFilter{GrantID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].ChangeKind != "deny" || changes[0].ActorKind != "cli" {
		t.Fatalf("cli deny: change row = %+v, want one deny row by cli", changes)
	}

	// The CHECK backstop, raw: an allow row a cli claims to have created
	// aborts; a deny row is admitted.
	gm := openMigrated(t)
	seedComponent(t, gm, "cmp-1")
	if err := gm.Exec(`INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('ag-cli-a','cmp-1','action','forge.merge','allow','cli','cli','t')`).Error; err == nil {
		t.Errorf("a raw cli-created ALLOW row was accepted — the CHECK backstop is gone")
	}
	if err := gm.Exec(`INSERT INTO autonomy_grant (id,node_id,subject_kind,subject,state,created_by_kind,created_by_id,created_at)
		VALUES ('ag-cli-d','cmp-1','action','forge.merge','deny','cli','cli','t')`).Error; err != nil {
		t.Errorf("a raw cli-created deny row was refused: %v — the direct door's veto has no backstop", err)
	}
}

// TestRevokeIsIdempotentAndReportsAffectedLoops proves AL5/AL6/OD8: a revoke
// reports the running loops pinned under the grant, a second revoke is a no-op
// that writes no change row, and terminal loops are not in the affected list.
func TestRevokeIsIdempotentAndReportsAffectedLoops(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	g := s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "git.commitAndPush", State: "allow"})
	pin := &AutonomyPin{RootID: root.ID, Rev: 1, Origin: "scheduler", Labels: "[]",
		Allowed: fmt.Sprintf(`[{"grant":%q,"subject":"git.commitAndPush","conditions":{}}]`, g.ID)}
	running, err := s.CreatePinnedLoop(NewLoop{Slug: "run", ComponentRoot: root.ID, AutonomyPin: pin}, "bp-1", 1, "content", `{"p":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePinnedLoop(NewLoop{Slug: "done", ComponentRoot: root.ID, Status: "done", AutonomyPin: pin}, "bp-2", 1, "content", `{"p":true}`); err != nil {
		t.Fatal(err)
	}
	// first revoke: affected = the running loop; a revoke change row is written.
	_, affected, ref, err := s.RevokeAutonomyGrant(Actor{Kind: "user", ID: "u1"}, g.ID, "no longer")
	if err != nil || ref != nil {
		t.Fatalf("revoke: %v %s", err, ref)
	}
	if len(affected) != 1 || affected[0] != running.ID {
		t.Errorf("affected = %v, want only the running loop %s", affected, running.ID)
	}
	if n := countTable(t, s.gw, "autonomy_change"); n != 2 {
		t.Errorf("want 2 change rows (create + revoke), got %d", n)
	}
	// second revoke: a no-op — no new change row, no affected list.
	_, aff, ref, err := s.RevokeAutonomyGrant(Actor{Kind: "user", ID: "u1"}, g.ID, "again")
	if err != nil || ref != nil {
		t.Fatalf("second revoke: %v %s", err, ref)
	}
	if len(aff) != 0 {
		t.Errorf("a no-op revoke reported affected loops: %v", aff)
	}
	if n := countTable(t, s.gw, "autonomy_change"); n != 2 {
		t.Errorf("the second revoke wrote a change row (want 2, got %d)", n)
	}
}

// TestAutonomyRevIsMonotonic proves AL8: the rev is MAX(autonomy_change.id)
// over the root. The change ids are a global sequence, so a change on another
// root skips ids and the root's rev need not be contiguous — but it rises with
// every change on the root, never resets, and a change on another root does
// not move it.
func TestAutonomyRevIsMonotonic(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	other := seedRoot(t, s, "/other")
	rev, err := s.AutonomyRev(root.ID)
	if err != nil || rev != 0 {
		t.Fatalf("initial rev = %d (%v), want 0", rev, err)
	}
	// a change on the root raises its rev.
	s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "git.commitAndPush", State: "allow"})
	r1, _ := s.AutonomyRev(root.ID)
	if r1 <= rev {
		t.Errorf("rev did not rise on a root change (%d -> %d)", rev, r1)
	}
	rev = r1
	// a change on another root must not move this root's rev.
	s.seedAutonomyGrant(t, other.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "git.commitAndPush", State: "allow"})
	if r, _ := s.AutonomyRev(root.ID); r != rev {
		t.Errorf("a change on another root moved root's rev (%d -> %d)", rev, r)
	}
	// a further change on the root raises it again (the ids are global, so it
	// may skip the id the other root took — monotonic, not contiguous).
	s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "forge.openPR", State: "allow"})
	r2, _ := s.AutonomyRev(root.ID)
	if r2 <= rev {
		t.Errorf("rev did not rise on a second root change (%d -> %d)", rev, r2)
	}
	rev = r2
	// a revoke is a change too, so it raises the rev.
	g, _, gerr := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, NewAutonomyGrant{
		NodeID: root.ID, SubjectKind: "action", Subject: "forge.openPR", State: "allow",
	})
	if gerr != nil {
		t.Fatal(gerr)
	}
	if _, _, ref, err := s.RevokeAutonomyGrant(Actor{Kind: "user", ID: "u1"}, g.ID, "reason"); err != nil || ref != nil {
		t.Fatalf("revoke: %v %s", err, ref)
	}
	r3, _ := s.AutonomyRev(root.ID)
	if r3 <= rev {
		t.Errorf("rev did not rise on a revoke (%d -> %d)", rev, r3)
	}
}

// pinnedFor builds a pin whose allowed snapshot names the grant for the action.
func pinnedFor(grantID, action string) *AutonomyPin {
	return &AutonomyPin{Origin: "scheduler", Labels: "[]",
		Allowed: fmt.Sprintf(`[{"grant":%q,"subject":%q,"conditions":{}}]`, grantID, action)}
}

func (s *Store) seedChild(t *testing.T, parentID, workspace string) Component {
	t.Helper()
	w, err := s.CreateComponent(NewComponent{Name: "child", Slug: strings.TrimPrefix(workspace, "/"), Root: workspace, ParentID: parentID})
	if err != nil {
		t.Fatalf("create child %s: %v", workspace, err)
	}
	if !w.OK() {
		t.Fatalf("create child %s: %s", workspace, w.Reason)
	}
	return w.Component
}

// TestAutonomyLiveFacts proves AL6/AL9: the live read reports each narrowing —
// revoked, expired, a veto on the root, a veto on the loop's component — and
// ignores a deny on a different action. It returns facts, not a verdict.
func TestAutonomyLiveFacts(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	late := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 15, 9, 41, 12, 0, time.UTC)
	// The outward expansion the decision site passes (from the action registry).
	// The store test may hardcode it; only the store's PRODUCTION code must not
	// encode the registry (AL2) — that is what the new parameter enforces.
	outwardSet := []string{"git.commitAndPush", "forge.openIssue", "forge.openPR", "forge.postReview", "forge.closeIssue"}

	// a live allow on the root, and a loop pinned under it for git.commitAndPush.
	allow := s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "git.commitAndPush", State: "allow"})
	loopA, err := s.CreatePinnedLoop(NewLoop{Slug: "a", ComponentRoot: "/root", AutonomyPin: pinnedFor(allow.ID, "git.commitAndPush")}, "bp-a", 1, "c", `{"p":true}`)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.AutonomyLive(context.Background(), loopA.ID, "git.commitAndPush", outwardSet, now)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Pinned || !v.Covered {
		t.Errorf("the pinned, covered action reads Pinned=%v Covered=%v", v.Pinned, v.Covered)
	}
	if v.Revoked || v.Expired || v.Vetoed {
		t.Errorf("a live grant with no veto reads Revoked=%v Expired=%v Vetoed=%v", v.Revoked, v.Expired, v.Vetoed)
	}

	// revoked: the same grant, now revoked.
	if _, _, ref, err := s.RevokeAutonomyGrant(Actor{Kind: "user", ID: "u1"}, allow.ID, "gone"); err != nil || ref != nil {
		t.Fatalf("revoke: %v %s", err, ref)
	}
	v, _ = s.AutonomyLive(context.Background(), loopA.ID, "git.commitAndPush", outwardSet, now)
	if !v.Revoked {
		t.Errorf("a revoked covering grant reads Revoked=%v, want true", v.Revoked)
	}

	// expired: a grant whose expiry is before the live clock.
	exp := s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.openPR", State: "allow",
		ExpiresAt: func() *time.Time { e := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC); return &e }(),
	})
	loopB, err := s.CreatePinnedLoop(NewLoop{Slug: "b", ComponentRoot: "/root", AutonomyPin: pinnedFor(exp.ID, "forge.openPR")}, "bp-b", 1, "c", `{"p":true}`)
	if err != nil {
		t.Fatal(err)
	}
	v, _ = s.AutonomyLive(context.Background(), loopB.ID, "forge.openPR", outwardSet, late)
	if !v.Expired {
		t.Errorf("an expired covering grant reads Expired=%v, want true", v.Expired)
	}

	// vetoed-on-root: a deny on the root covering the action.
	if _, _, err := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, NewAutonomyGrant{
		NodeID: root.ID, SubjectKind: "action", Subject: "git.commitAndPush", State: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.AutonomyLive(context.Background(), loopA.ID, "git.commitAndPush", outwardSet, now)
	if !v.Vetoed || v.VetoNode != root.ID {
		t.Errorf("a veto on the root reads Vetoed=%v VetoNode=%s, want true/%s", v.Vetoed, v.VetoNode, root.ID)
	}

	// vetoed-on-child: a deny on the loop's component (a child) covering it.
	// A fresh root, so the only git.commitAndPush deny on this loop's path is
	// the one on the component — a deny on /root (the vetoed-on-root step) would
	// otherwise outrank it by id and the check could not prove the component
	// veto fires on its own.
	root2 := seedRoot(t, s, "/root2")
	child2 := s.seedChild(t, root2.ID, "/child2")
	if _, _, err := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, NewAutonomyGrant{
		NodeID: child2.ID, SubjectKind: "action", Subject: "git.commitAndPush", State: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	loopC, err := s.CreatePinnedLoop(NewLoop{Slug: "c", ComponentRoot: "/child2", AutonomyPin: pinnedFor(allow.ID, "git.commitAndPush")}, "bp-c", 1, "c", `{"p":true}`)
	if err != nil {
		t.Fatal(err)
	}
	v, _ = s.AutonomyLive(context.Background(), loopC.ID, "git.commitAndPush", outwardSet, now)
	if !v.Vetoed || v.VetoNode != child2.ID {
		t.Errorf("a veto on the loop's component reads Vetoed=%v VetoNode=%s, want true/%s", v.Vetoed, v.VetoNode, child2.ID)
	}

	// an unrelated deny (a different action) must not veto. /root already carries
	// a deny for git.commitAndPush (the vetoed-on-root step above), so this check
	// uses a fresh root whose only deny is on a DIFFERENT action — the veto query
	// must not match it against git.commitAndPush.
	clean := seedRoot(t, s, "/clean")
	callow := s.seedAutonomyGrant(t, clean.ID, NewAutonomyGrant{SubjectKind: "action", Subject: "git.commitAndPush", State: "allow"})
	loopD, err := s.CreatePinnedLoop(NewLoop{Slug: "d", ComponentRoot: "/clean", AutonomyPin: pinnedFor(callow.ID, "git.commitAndPush")}, "bp-d", 1, "c", `{"p":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateAutonomyGrant(Actor{Kind: "user", ID: "u1"}, NewAutonomyGrant{
		NodeID: clean.ID, SubjectKind: "action", Subject: "forge.merge", State: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.AutonomyLive(context.Background(), loopD.ID, "git.commitAndPush", outwardSet, now)
	if v.Vetoed {
		t.Errorf("a different-action deny (forge.merge) vetoed git.commitAndPush on a root with no matching deny")
	}
}

// TestNoGoOutsideTheStoreNamesAutonomyTables proves I2: the autonomy table names
// live only in internal/store. A table name anywhere else is a second truth.
// (Extends TestNoSQLLivesOutsideTheStore.)
func TestNoGoOutsideTheStoreNamesAutonomyTables(t *testing.T) {
	re := regexp.MustCompile(`\b(autonomy_grant|autonomy_change|autonomy_use|loop_autonomy_pin)\b`)
	storePrefix := filepath.Join("internal", "store")
	var offenders []string
	for _, hit := range grepRepo(t, re) {
		if strings.HasPrefix(hit.file, storePrefix) {
			continue
		}
		offenders = append(offenders, hit.file+": "+hit.line)
	}
	for _, o := range offenders {
		t.Errorf("an autonomy table name outside internal/store:\n    %s", o)
	}
}

// TestHistoryOutlivesTheGrant proves AL10: no FK from the history tables to
// autonomy_grant (a grant is never deleted, and the audit and use log outlive
// it), and no FK from the pin to loop (archive moves loop rows out).
func TestHistoryOutlivesTheGrant(t *testing.T) {
	g := openMigrated(t)
	for _, table := range []string{"autonomy_change", "autonomy_use"} {
		if n := countFkTo(t, g, table, "autonomy_grant"); n != 0 {
			t.Errorf("%s has an FK to autonomy_grant — the history must outlive the grant (AL10)", table)
		}
	}
	if n := countFkTo(t, g, "loop_autonomy_pin", "loop"); n != 0 {
		t.Errorf("loop_autonomy_pin has %d FK(s) to loop — the pin outlives the loop (AL8)", n)
	}
}

func countFkTo(t *testing.T, g *gorm.DB, table, referenced string) int {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM pragma_foreign_key_list(?) WHERE "table" = ?`, table, referenced).Scan(&n).Error; err != nil {
		t.Fatalf("read foreign keys of %s: %v", table, err)
	}
	return n
}

// TestUseLogReturnsABegunUnpaired proves the crash window (AL10, T5 behaviour
// 10): a `begun` row whose outcome never arrived (the daemon died between the
// effect and its receipt) is returned by AutonomyUses UNPAIRED — phase begun,
// no begun_id, and no completed/failed row points at it. That unpaired begun
// is the "effect may have happened" state the reader (T6) renders as outcome:
// unknown; the store's job is to return it as it stands, never to drop it or
// invent an outcome. A begun that silently disappears, or that a reader cannot
// tell apart from a paired one, is the audit hole AL10 exists to close.
func TestUseLogReturnsABegunUnpaired(t *testing.T) {
	s := openTemp(t)
	root := seedRoot(t, s, "/root")
	g := s.seedAutonomyGrant(t, root.ID, NewAutonomyGrant{
		SubjectKind: "action", Subject: "git.commitAndPush", State: "allow",
	})
	begunID, err := s.RecordAutonomyUse("l-1", g.ID, 1, "git.commitAndPush", "begun", 0, "")
	if err != nil {
		t.Fatalf("record begun: %v", err)
	}
	if begunID == 0 {
		t.Fatal("the begun row got no id — its outcome row would have nothing to point at")
	}
	uses, err := s.AutonomyUses(AutonomyUseFilter{GrantID: g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 1 {
		t.Fatalf("the use log has %d rows, want the one begun: %+v", len(uses), uses)
	}
	u := uses[0]
	if u.Phase != "begun" {
		t.Errorf("phase = %q, want begun", u.Phase)
	}
	if u.BegunID != nil {
		t.Errorf("a begun row reads BegunID=%d, want nil — begun is unpaired until an outcome row names it", *u.BegunID)
	}
	// Unpaired: no completed/failed row points at the begun id. (With exactly
	// one row there is none by construction; the loop makes the assertion read
	// as the property rather than the count.)
	for _, row := range uses {
		if row.BegunID != nil && *row.BegunID == begunID {
			t.Errorf("an outcome row (%s) already pairs the begun — the crash window is not this", row.Phase)
		}
	}
}
