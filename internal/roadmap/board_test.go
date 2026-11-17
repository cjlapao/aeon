package roadmap

// S50 T2 (RM6) — the board's two honesty rules: the blocker is IsReady's
// OWN sentence (byte-for-byte, not a paraphrase), and the board agrees
// with the list — same items, same counts, one assembly.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func TestBoardBlockerIsIsReadysSentence(t *testing.T) {
	root := "/repo/board"
	r, _ := openRoadmapStore(t, root)
	mustCreate(t, r, root, "unready", NewItem{Title: "unready"})

	ans, ref := r.Board(BoardFilter{})
	if ref != nil {
		t.Fatalf("board: %v", ref)
	}
	if len(ans.Rows) != 1 || ans.Rows[0].Ready {
		t.Fatalf("rows: %+v", ans.Rows)
	}
	want, wantWhy := IsReady(ans.Rows[0].Item)
	if ans.Rows[0].Blocker != wantWhy || want {
		t.Fatalf("blocker %q, want IsReady's own sentence %q", ans.Rows[0].Blocker, wantWhy)
	}
	if !strings.HasPrefix(ans.Rows[0].Blocker, "not ready: ") {
		t.Fatalf("blocker %q — the board re-derived a different shape", ans.Rows[0].Blocker)
	}
	// The Describe sentence rides every row.
	if ans.Rows[0].Describe == "" || !strings.HasPrefix(ans.Rows[0].Describe, "idea · priority unknown") {
		t.Fatalf("describe %q", ans.Rows[0].Describe)
	}
}

func TestBoardAndListAgree(t *testing.T) {
	root := "/repo/agree"
	r, s := openRoadmapStore(t, root)
	a := mustCreate(t, r, root, "one", NewItem{Title: "one", Epic: "e1"})
	mustCreate(t, r, root, "two", NewItem{Title: "two", Epic: "e1"})
	mustCreate(t, r, root, "three", NewItem{Title: "three"})
	// Walk one to running so its loop block has something to carry —
	// the recipe audit_test.go walks: estimate + plan + known priority,
	// plain writers, setup not under test.
	r.SetEstimate(a.ID, 1, 2, "small")
	r.SetArtifacts(a.ID, root+"/b.md", root+"/p.md")
	r.SetPriority(a.ID, PriorityKnown, "P1")
	r.Transition(a.ID, StatusIdeating)
	r.Transition(a.ID, StatusBriefAgreed)
	r.Transition(a.ID, StatusPlanned)
	r.Transition(a.ID, StatusReady)
	if _, ref := r.Transition(a.ID, StatusRunning); ref != nil {
		t.Fatalf("walk to running: %s", ref.Reason)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "roadmap-item-"+itoa(a.ID), ComponentRoot: root, Goal: "g"}); err != nil {
		t.Fatalf("seed loop: %v", err)
	}
	if _, ref := r.RecordLoopAudited(a.ID, root, "roadmap-item-"+itoa(a.ID), doorPerson); ref != nil {
		t.Fatalf("record loop: %s", ref.Reason)
	}

	list, err := r.List(root, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	ans, ref := r.Board(BoardFilter{ComponentRoot: root})
	if ref != nil {
		t.Fatal(ref)
	}
	if len(ans.Rows) != len(list) {
		t.Fatalf("board %d rows, list %d items — two answers for one question", len(ans.Rows), len(list))
	}
	for i := range list {
		if ans.Rows[i].ID != list[i].ID || ans.Rows[i].Describe != Describe(list[i]) {
			t.Fatalf("row %d diverges from the list", i)
		}
	}
	if ans.Groups["e1"] != 2 || ans.Groups[""] != 1 || len(ans.Groups) != 2 {
		t.Fatalf("groups: %+v", ans.Groups)
	}
	// The loop block: slug + status read from the loop row itself. The
	// board sorts by STATUS RANK — the running item is not first.
	var mat *BoardRow
	for i := range ans.Rows {
		if ans.Rows[i].ID == a.ID {
			mat = &ans.Rows[i]
		}
	}
	if mat == nil {
		t.Fatal("materialized item absent from the board")
	}
	if mat.Loop == nil || mat.Loop.Status == "" {
		t.Fatalf("materialized item carries no loop block: %+v", mat.Loop)
	}
	// The epic filter narrows rows AND groups together.
	f := BoardFilter{ComponentRoot: root, Epic: "e1"}
	sub, ref := r.Board(f)
	if ref != nil || len(sub.Rows) != 2 || sub.Groups["e1"] != 2 || len(sub.Groups) != 1 {
		t.Fatalf("filtered board: %v %+v", sub.Rows, sub.Groups)
	}
	// An unknown status filter refuses, naming valid values.
	if _, ref := r.Board(BoardFilter{Status: Status("shipped")}); ref == nil ||
		!strings.Contains(ref.Reason, "shipped") || !strings.Contains(ref.Reason, "cancelled") {
		t.Fatalf("unknown status filter: %v", ref)
	}
}

func TestHistoryPagesTheAuditTrail(t *testing.T) {
	root := "/repo/hist"
	r, _ := openRoadmapStore(t, root)
	it := mustCreate(t, r, root, "traced", NewItem{Title: "traced"})
	r.SetEstimateAudited(it.ID, 1, 2, "small", doorPerson)
	r.SetPriorityAudited(it.ID, PriorityKnown, "P2", doorPerson)

	rows, err := r.History(context.Background(), it.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// newest first: priority, then estimate. mustCreate's plain create
	// audited NOTHING — the plain writers are the point of RM2's split.
	if len(rows) != 2 || rows[0].Op != "priority" || rows[1].Op != "estimate" {
		t.Fatalf("trail: %+v", rows)
	}
	page, err := r.History(context.Background(), it.ID, 1)
	if err != nil || len(page) != 1 || page[0].Op != "priority" {
		t.Fatalf("page: %d rows %+v (%v)", len(page), page, err)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
