package store

// S50 T1 — the roadmap_change table's own gates: the shape is the
// DATABASE's claim (the CHECK discipline roadmap_test.go carries), the
// append-only rule has teeth, the audited writers keep the write and its
// audit in ONE transaction, and a write that does not happen is audited
// nothing. The raw-INSERT style here is test-only (the S44/S45/S49 audit
// siblings are the precedent); production reaches the table only through
// the audited writers.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// auditPerson is a lawful audit author; the store never interprets the id,
// only that one exists (the door + socket layers own who counts).
var auditPerson = RoadmapActor{Kind: "user", ID: "42"}

func seedRoadmapRow(t *testing.T, s *Store, root, title string) int64 {
	t.Helper()
	row, err := s.CreateRoadmapItem(NewRoadmapItemRow{
		ComponentRoot: root, Title: title, Provenance: "authored",
	})
	if err != nil {
		t.Fatalf("seed roadmap item %q: %v", title, err)
	}
	return row.ID
}

func TestRoadmapChangeChecksRejectEveryInvalidValue(t *testing.T) {
	s := openTemp(t)
	root := "/repo/audit"
	seedRoadmapComponent(t, s, "audit", root)
	id := seedRoadmapRow(t, s, root, "audited item")

	cases := []struct {
		name string
		op   string
		sum  string
		kind string
		who  string
	}{
		{"op outside the nine", "delete", "moved item 1 idea → done", "user", "42"},
		{"actor kind agent (RM4 in the schema)", "transition", "moved item 1 idea → ideating", "agent", "brainstormer"},
		{"actor kind invented", "add", "added item 1", "wizard", "42"},
		{"blank actor id", "add", "added item 1", "user", ""},
		{"empty summary", "add", "", "user", "42"},
		{"summary over the bound", "add", "added item 1 " + strings.Repeat("x", 1024), "user", "42"},
	}
	for _, tc := range cases {
		if _, err := s.w.Exec(insertRoadmapChangeSQL, id, tc.op, tc.sum, tc.kind, tc.who, "now"); err == nil {
			t.Fatalf("%s: the insert was accepted — the CHECK has no teeth", tc.name)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") &&
			!strings.Contains(err.Error(), "constraint failed") {
			t.Fatalf("%s: refused with %v, want a CHECK refusal", tc.name, err)
		}
	}
	// The positive control: the nine ops all land (a CHECK that admits
	// eight while Go says nine is the T2 phantom failure waiting).
	for _, op := range []string{"add", "update", "cancel", "estimate", "priority", "ideate", "plan", "materialize", "transition"} {
		if _, err := s.w.Exec(insertRoadmapChangeSQL, id, op, "control row for "+op, "user", "42", "now"); err != nil {
			t.Fatalf("positive control op %q: %v", op, err)
		}
	}
}

func TestRoadmapChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	root := "/repo/append"
	seedRoadmapComponent(t, s, "append", root)
	id := seedRoadmapRow(t, s, root, "kept forever")
	if err := s.RoadmapWithTx(func(tx *sql.Tx) error {
		return RoadmapChangeInsertTx(tx, RoadmapAudit{
			ItemID: id, Op: "add", Summary: "added item 1", Actor: auditPerson,
		}, s.Now())
	}); err != nil {
		t.Fatalf("seed the audit row: %v", err)
	}
	// UPDATE and DELETE both abort, by trigger, naming the table.
	if _, err := s.w.Exec(`UPDATE roadmap_change SET summary = 'rewritten history' WHERE item_id = ?`, id); err == nil {
		t.Fatal("an UPDATE of the audit was accepted — the append-only trigger has no teeth")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update refused with %v, want the append-only sentence", err)
	}
	if _, err := s.w.Exec(`DELETE FROM roadmap_change WHERE item_id = ?`, id); err == nil {
		t.Fatal("a DELETE of the audit was accepted — the append-only trigger has no teeth")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete refused with %v, want the append-only sentence", err)
	}
	// The row the D6 cancel never deleted is still there — history
	// outlives the item, which is why there is no FK to outlive.
	if _, err := s.w.Exec(`DELETE FROM roadmap_item WHERE id = ?`, id); err != nil {
		t.Fatalf("delete the item (no FK holds it): %v", err)
	}
	changes, err := s.RoadmapChangeList(context.Background(), id, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("the audit outlives the item: %d rows, err %v — want the row kept", len(changes), err)
	}
}

func TestRoadmapChangeListIsNewestFirstWithABound(t *testing.T) {
	s := openTemp(t)
	root := "/repo/list"
	seedRoadmapComponent(t, s, "list", root)
	id := seedRoadmapRow(t, s, root, "walked")
	for _, op := range []string{"add", "estimate", "priority", "transition"} {
		err := s.RoadmapWithTx(func(tx *sql.Tx) error {
			return RoadmapChangeInsertTx(tx, RoadmapAudit{
				ItemID: id, Op: op, Summary: "walked " + op, Actor: auditPerson,
			}, s.Now())
		})
		if err != nil {
			t.Fatalf("append %s: %v", op, err)
		}
	}
	changes, err := s.RoadmapChangeList(context.Background(), id, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("limit 3 answered %d rows", len(changes))
	}
	if changes[0].Op != "transition" || changes[2].Op != "estimate" {
		t.Fatalf("not newest-first: %s … %s", changes[0].Op, changes[2].Op)
	}
	// limit <= 0 falls back to the default 50, not to an error or an
	// unbounded read — the listing discipline every door carries.
	all, err := s.RoadmapChangeList(context.Background(), id, 0)
	if err != nil || len(all) != 4 {
		t.Fatalf("default limit answered %d rows, err %v — want all 4", len(all), err)
	}
}

func TestCreateRoadmapItemAuditedWritesItsAddRow(t *testing.T) {
	s := openTemp(t)
	root := "/repo/create"
	seedRoadmapComponent(t, s, "create", root)
	created, err := s.CreateRoadmapItemAudited(NewRoadmapItemRow{
		ComponentRoot: root, Title: "born audited", Provenance: "authored",
	}, RoadmapAudit{Actor: auditPerson})
	if err != nil {
		t.Fatalf("audited create: %v", err)
	}
	changes, err := s.RoadmapChangeList(context.Background(), created.ID, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("the create wrote %d rows, want 1 (err %v)", len(changes), err)
	}
	c := changes[0]
	if c.Op != "add" || c.ItemID != created.ID {
		t.Fatalf("row op %q item %d, want add on %d", c.Op, c.ItemID, created.ID)
	}
	if !strings.HasPrefix(c.Summary, "added item ") {
		t.Fatalf("summary %q, want the fixed add template", c.Summary)
	}
	if c.ActorKind != "user" || c.ActorID != "42" || c.OccurredAt == "" {
		t.Fatalf("audit carries %s/%s at %q", c.ActorKind, c.ActorID, c.OccurredAt)
	}
	// A lawless author never reaches SQL.
	if _, err := s.CreateRoadmapItemAudited(NewRoadmapItemRow{
		ComponentRoot: root, Title: "ghost", Provenance: "authored",
	}, RoadmapAudit{Actor: RoadmapActor{Kind: "agent", ID: "x"}}); err == nil {
		t.Fatal("an agent-authored create was accepted")
	}
}

func TestAuditAndWriteAreOneTransaction(t *testing.T) {
	// The rule in one test: force the audit INSERT to fail (a summary past
	// the 1024 CHECK) and prove the ITEM did not move — the write and its
	// audit are one transaction or they are nothing.
	s := openTemp(t)
	root := "/repo/atomic"
	seedRoadmapComponent(t, s, "atomic", root)
	id := seedRoadmapRow(t, s, root, "stays put")
	brief := "/repo/atomic/brief.md"
	claimed, err := s.RoadmapItemLandingAudited(id, &brief, nil, "idea", "idea", RoadmapAudit{
		Op: "transition", Summary: strings.Repeat("far too long ", 200), Actor: auditPerson,
	})
	if claimed {
		t.Fatal("the landing claimed while its audit failed")
	}
	if err == nil {
		t.Fatal("an over-bound audit summary was accepted — the CHECK has no teeth through the writer")
	}
	fresh, found, err := s.RoadmapItemByID(id)
	if err != nil || !found {
		t.Fatalf("re-read: found=%v err %v", found, err)
	}
	if fresh.Status != "idea" {
		t.Fatalf("the item moved to %q while its audit failed — the transaction is not one", fresh.Status)
	}
	changes, err := s.RoadmapChangeList(context.Background(), id, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("failed write left %d audit rows, want 0 (err %v)", len(changes), err)
	}
}

func TestAuditedTransitionWritesExactlyOneRowOnAClaim(t *testing.T) {
	s := openTemp(t)
	root := "/repo/claim"
	seedRoadmapComponent(t, s, "claim", root)
	id := seedRoadmapRow(t, s, root, "moves once")
	claimed, err := s.TransitionRoadmapItemAudited(id, "idea", "ideating", RoadmapAudit{
		Op: "ideate", Summary: "moved item 1 idea → ideating", Actor: auditPerson,
	})
	if err != nil || !claimed {
		t.Fatalf("claim: %v claimed=%v", err, claimed)
	}
	// The loser: same write, wrong FROM — nothing moves and nothing audits.
	lost, err := s.TransitionRoadmapItemAudited(id, "idea", "ideating", RoadmapAudit{
		Op: "ideate", Summary: "moved item 1 idea → ideating", Actor: auditPerson,
	})
	if err != nil || lost {
		t.Fatalf("the loser claimed: %v claimed=%v", err, lost)
	}
	changes, err := s.RoadmapChangeList(context.Background(), id, 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("claim+loss wrote %d rows, want exactly 1 (err %v)", len(changes), err)
	}
	if changes[0].Op != "ideate" {
		t.Fatalf("op %q", changes[0].Op)
	}
}
