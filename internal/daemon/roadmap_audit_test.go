package daemon

// S50 T1 — the production write path names its author. The dispatch
// verbs carry the CONNECTION's person (the socket handler stamped them);
// everything the reactor writes on its own carries `system/reactor` —
// RM2's whole point in one line: a person can see WHAT MOVED ON ITS OWN.
// The dispatch and reactor BEHAVIOUR suites above stay unedited; these
// tests read only the audit trail they left behind.

import (
	"context"
	"fmt"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

func auditRowsOf(t *testing.T, f *roadmapFixture, id int64) []store.RoadmapChange {
	t.Helper()
	rows, err := f.s.RoadmapChangeList(context.Background(), id, 100)
	if err != nil {
		t.Fatalf("read the audit for item %d: %v", id, err)
	}
	return rows
}

func wantAudit(t *testing.T, f *roadmapFixture, id int64, wantCount int, op, kind, who, summary string) {
	t.Helper()
	rows := auditRowsOf(t, f, id)
	if len(rows) != wantCount {
		t.Fatalf("item %d carries %d audit rows, want %d: %+v", id, len(rows), wantCount, rows)
	}
	r := rows[0] // newest first
	if r.Op != op || r.ActorKind != kind || r.ActorID != who || r.Summary != summary {
		t.Fatalf("newest audit row is %s by %s/%s (%q), want %s by %s/%s (%q)",
			r.Op, r.ActorKind, r.ActorID, r.Summary, op, kind, who, summary)
	}
}

func TestIdeationAuditNamesThePerson(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "audited idea")
	if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
		t.Fatalf("ideate: %v", err)
	}
	wantAudit(t, f, it.ID, 1, "ideate", "user", "test-person",
		fmt.Sprintf("moved item %d idea → ideating", it.ID))
}

func TestReactorTransitionsAreAuditedAsSystemReactor(t *testing.T) {
	t.Run("brief-agreed", func(t *testing.T) {
		f := newRoadmapFixture(t)
		it := f.create(t, "brief lands audited")
		if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
			t.Fatalf("ideate: %v", err)
		}
		f.writeFile(t, "brief.md")
		f.publish(t, f.loopID(t, fmt.Sprintf("ideate-item-%d", it.ID)), "done", "")
		f.awaitStatus(t, it.ID, roadmap.StatusBriefAgreed)
		// The landing used to be TWO writes (pointer, then status); audited it
		// is ONE transaction and EXACTLY ONE row — ideate (the person) plus
		// the landing (the reactor), nothing else.
		wantAudit(t, f, it.ID, 2, "transition", "system", "reactor",
			fmt.Sprintf("moved item %d ideating → brief-agreed (brief landed)", it.ID))
	})
}

func TestMaterializationDirectBranchAuditsThePersonTwiceAndTheReactorNever(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "materialize audited")
	f.walkToReady(t, it.ID)
	attended := true
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "export monthly statements",
		WorkspaceRoot: f.root, Tech: "go", Attended: &attended,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	// ready → running and the loop stamp are BOTH the person's act on the
	// direct branch (the dispatch carried the connection through to the
	// end): two 'materialize' rows, newest the loop stamp.
	wantAudit(t, f, it.ID, 2, "materialize", "user", "test-person",
		fmt.Sprintf("recorded item %d on loop roadmap-item-%d", it.ID, it.ID))
}

func TestReactorDoneReportCarriesTheReactor(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "done lands audited")
	f.walkToReady(t, it.ID)
	attended := true
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "export monthly statements",
		WorkspaceRoot: f.root, Tech: "go", Attended: &attended,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	loopID := f.loopID(t, fmt.Sprintf("roadmap-item-%d", it.ID))
	f.publish(t, loopID, "done", "")
	f.awaitStatus(t, it.ID, roadmap.StatusDone)
	// The done edge belongs to the reactor, whatever the person did to
	// start the loop.
	wantAudit(t, f, it.ID, 3, "transition", "system", "reactor",
		fmt.Sprintf("moved item %d running → done", it.ID))
}
