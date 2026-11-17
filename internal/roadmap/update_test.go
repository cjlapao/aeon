package roadmap

// S50 T2 — RM1's edit door, RM5's formatter and the derived vocabulary.
// The edit's whole point is the NARROW set: what the edit type cannot
// name it cannot change, and the rules below prove the refusals, the
// window, the no-op honesty and the one audit row.

import (
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

func TestUpdateChangesOnlyTheNamedFields(t *testing.T) {
	root := "/repo/update"
	r, s := openRoadmapStore(t, root)
	it := mustCreate(t, r, root, "original", NewItem{
		Title: "original", Epic: "reports",
		Priority: PriorityKnown, PriorityLabel: "P1",
	})
	before, _, err := s.RoadmapItemByID(it.ID)
	if err != nil {
		t.Fatal(err)
	}

	fresh, changed, ref := r.Update(it.ID, UpdateFields{Title: strptr("fixed title")}, doorPerson)
	if ref != nil || !changed {
		t.Fatalf("title edit: changed=%v ref=%v", changed, ref)
	}
	if fresh.Title != "fixed title" || fresh.Epic != "reports" {
		t.Fatalf("title=%q epic=%q — the named field changed, the unnamed rode along", fresh.Title, fresh.Epic)
	}
	after, _, err := s.RoadmapItemByID(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Byte-identical honesty columns.
	if after.PriorityState != before.PriorityState || deref(after.PriorityLabel) != deref(before.PriorityLabel) ||
		deref(after.EstimateMin) != deref(before.EstimateMin) || after.Status != before.Status ||
		after.Provenance != before.Provenance || after.CreatedAt != before.CreatedAt {
		t.Fatal("columns the edit did not name moved")
	}
	// (updated_at is NOT asserted: the harness clock can answer the same
	// instant twice and a flaky clock-pin test is not a rule — the audit
	// row below is the fact that the write happened.)
	rows := auditRows(t, s, it.ID)
	if len(rows) != 1 || rows[0].Op != "update" ||
		!strings.Contains(rows[0].Summary, "title") || strings.Contains(rows[0].Summary, "original") ||
		strings.Contains(rows[0].Summary, "fixed") {
		t.Fatalf("audit after the edit: %+v — want ONE fields-only row", rows)
	}

	// Epic clear + a path.
	if _, changed, ref := r.Update(it.ID, UpdateFields{ClearEpic: true, BriefPath: strptr(root + "/briefs/a.md")}, doorPerson); ref != nil || !changed {
		t.Fatalf("clear+path edit: %v %v", changed, ref)
	}
	fresh, _, _ = r.Show(it.ID)
	if fresh.Epic != "" || fresh.BriefPath != root+"/briefs/a.md" {
		t.Fatalf("epic=%q brief=%q", fresh.Epic, fresh.BriefPath)
	}
	if rows := auditRows(t, s, it.ID); len(rows) != 2 ||
		rows[0].Summary != "edited item 1 (fields: epic, briefPath)" {
		t.Fatalf("summary %q, want the fixed template naming epic then briefPath", rows[0].Summary)
	}

	// epic AND clearEpic is a contradiction.
	if _, _, ref := r.Update(it.ID, UpdateFields{Epic: strptr("x"), ClearEpic: true}, doorPerson); ref == nil {
		t.Fatal("epic AND clearEpic together was accepted")
	}
}

func TestUpdatePathRulesAndStatusWindow(t *testing.T) {
	root := "/repo/window"
	r, _ := openRoadmapStore(t, root)
	it := mustCreate(t, r, root, "window", NewItem{Title: "window"})

	if _, _, ref := r.Update(it.ID, UpdateFields{BriefPath: strptr("relative/brief.md")}, doorPerson); ref == nil ||
		!strings.Contains(ref.Reason, "not absolute") {
		t.Fatalf("relative path: %v", ref)
	}
	if _, _, ref := r.Update(it.ID, UpdateFields{BriefPath: strptr("/etc/passwd")}, doorPerson); ref == nil ||
		!strings.Contains(ref.Reason, "component root") {
		t.Fatalf("outside-root path: %v", ref)
	}
	if _, _, ref := r.Update(it.ID, UpdateFields{PlanPath: strptr("/repo/window/plan.md")}, doorPerson); ref != nil {
		t.Fatalf("in-root absolute path refused: %v", ref)
	}
	// The sibling-prefix trap: /repo/window-evil shares the string prefix
	// but not the directory.
	if _, _, ref := r.Update(it.ID, UpdateFields{PlanPath: strptr("/repo/window-evil/plan.md")}, doorPerson); ref == nil {
		t.Fatal("a sibling directory sharing the root's string prefix was accepted")
	}
	// Walk to running (plain writers; the window test owns the rule).
	mustTransition(t, r, it.ID, StatusIdeating)
	r.SetEstimate(mustID(it), 1, 1, "a day")
	r.SetArtifacts(it.ID, root+"/b.md", root+"/p.md")
	r.SetPriority(it.ID, PriorityKnown, "P1") // D4: readiness needs the priority non-unknown
	r.Transition(it.ID, StatusBriefAgreed)
	r.Transition(it.ID, StatusPlanned)
	r.Transition(it.ID, StatusReady)
	if got, ref := r.Transition(it.ID, StatusRunning); ref != nil {
		t.Fatalf("walk to running stuck at %q: %s", got.Status, ref.Reason)
	}
	if _, _, ref := r.Update(it.ID, UpdateFields{PlanPath: strptr(root + "/rewritten.md")}, doorPerson); ref == nil ||
		!strings.Contains(ref.Reason, "running") || !strings.Contains(ref.Reason, "history") {
		t.Fatalf("path edit after running: %v — want a refusal naming the status", ref)
	}
	// Title still editable after running (the typo fix stays legal)…
	if _, changed, ref := r.Update(it.ID, UpdateFields{Title: strptr("typo fixed")}, doorPerson); ref != nil || !changed {
		t.Fatalf("title edit while running: %v %v", changed, ref)
	}
}

func TestUpdateTerminalItemsAcceptTitleEpicOnly(t *testing.T) {
	root := "/repo/terminal"
	r, _ := openRoadmapStore(t, root)
	it := mustCreate(t, r, root, "done one", NewItem{Title: "done one"})
	r.CancelAudited(it.ID, doorPerson)
	if _, changed, ref := r.Update(it.ID, UpdateFields{Title: strptr("cancelled one"), Epic: strptr("cleanups")}, doorPerson); ref != nil || !changed {
		t.Fatalf("terminal title/epic edit: %v %v", changed, ref)
	}
	if _, _, ref := r.Update(it.ID, UpdateFields{BriefPath: strptr(root + "/backdated.md")}, doorPerson); ref == nil ||
		!strings.Contains(ref.Reason, "cancelled") {
		t.Fatalf("path edit on a cancelled item: %v", ref)
	}
}

func TestUpdateIsAuditedOnceAndNoOpIsNot(t *testing.T) {
	root := "/repo/noop"
	r, s := openRoadmapStore(t, root)
	it := mustCreate(t, r, root, "named", NewItem{Title: "named", Epic: "e"})

	// The no-op: naming the field with its current value.
	fresh, changed, ref := r.Update(it.ID, UpdateFields{Title: strptr("named")}, doorPerson)
	if ref != nil || changed {
		t.Fatalf("unchanged edit answered changed=%v ref=%v", changed, ref)
	}
	if rows := auditRows(t, s, it.ID); len(rows) != 0 {
		t.Fatalf("the no-op audited %+v — changed:false writes nothing", rows)
	}
	// The real edit: exactly one row.
	if _, changed, ref := r.Update(it.ID, UpdateFields{Title: strptr("renamed"), Epic: strptr("e2")}, doorPerson); ref != nil || !changed {
		t.Fatalf("edit: %v %v", changed, ref)
	}
	rows := auditRows(t, s, it.ID)
	if len(rows) != 1 || rows[0].Op != "update" ||
		rows[0].Summary != "edited item 1 (fields: title, epic)" ||
		rows[0].ActorKind != "user" || rows[0].ActorID != "7" {
		t.Fatalf("audit: %+v", rows)
	}
	// Agent and anonymous authors refused, nothing written.
	if _, _, ref := r.Update(it.ID, UpdateFields{Title: strptr("ghost")}, Actor{Kind: "agent", ID: "x"}); ref == nil {
		t.Fatal("an agent author edited the roadmap")
	}
	fresh, _, _ = r.Show(it.ID)
	if fresh.Title != "renamed" {
		t.Fatal("the refused agent edit wrote")
	}
}

func TestDescribeKeepsThreeStatesAndBasis(t *testing.T) {
	cases := []struct {
		in   Item
		want string
	}{
		{Item{Status: StatusIdea, Priority: PriorityKnown, PriorityLabel: "P1"},
			"idea · priority known (P1) · no estimate"},
		{Item{Status: StatusPlanned, Priority: PriorityAssumed, PriorityLabel: "soon",
			Estimate: &Estimate{Min: 2, Max: 3, Basis: "a pairing day each"}},
			"planned · priority assumed (soon) · estimate 2–3 (a pairing day each)"},
		{Item{Status: StatusReady, Priority: PriorityUnknown},
			"ready · priority unknown · no estimate"},
		{Item{Status: StatusRunning, Priority: PriorityUnknown, LoopSlug: "roadmap-item-9"},
			"running · priority unknown · no estimate · loop roadmap-item-9"},
	}
	for _, tc := range cases {
		if got := Describe(tc.in); got != tc.want {
			t.Errorf("Describe = %q, want %q", got, tc.want)
		}
	}
	// An estimate is never a bare number: the basis rides, even an odd one.
	odd := Describe(Item{Status: StatusIdea, Estimate: &Estimate{Min: 1, Max: 1, Basis: "certain"}})
	if !strings.Contains(odd, "(certain)") {
		t.Fatalf("basis vanished from %q", odd)
	}
}

func TestVocabularyEqualsAllowedTransitions(t *testing.T) {
	v := Vocabulary()
	table := AllowedTransitions()
	if len(v.Statuses) != 8 {
		t.Fatalf("%d statuses, want eight", len(v.Statuses))
	}
	for _, st := range v.Statuses {
		want := table[st.Status]
		if len(st.Next) != len(want) {
			t.Fatalf("%s: %d next, want %d", st.Status, len(st.Next), len(want))
		}
		for i := range want {
			if st.Next[i] != want[i] {
				t.Fatalf("%s: next[%d]=%s want %s", st.Status, i, st.Next[i], want[i])
			}
		}
	}
	// The mutation seam: the cancelled edge is present HERE because the
	// table carries it; the pin is that this output IS the table (the
	// table's own cartesian test owns edge changes).
	if got := table[StatusCancelled]; len(got) != 0 {
		t.Fatal("cancelled grew an edge outside this test")
	}
	if v.PathRule == "" || v.EstimateRule == "" || len(v.PathEditable) != 5 {
		t.Fatalf("vocabulary words missing: %+v", v)
	}
	if v.TitleMaxBytes != 200 || v.EpicMaxBytes != 64 {
		t.Fatalf("bounds %d/%d, want 200/64", v.TitleMaxBytes, v.EpicMaxBytes)
	}
}

func mustID(i Item) int64 { return i.ID }

func deref[T any](p *T) T {
	var z T
	if p != nil {
		return *p
	}
	return z
}

// TestNoLifecycleEdgeAdded — T0's promise: S50 moves data and doors, not
// the machine. The eleven edges are pinned as a SET (T0 recorded "golden
// by test, not source text"); if a later slice ever needs an edge, THIS
// test is the diff that says so out loud.
func TestNoLifecycleEdgeAdded(t *testing.T) {
	want := map[Status][]Status{
		StatusIdea:        {StatusIdeating, StatusCancelled},
		StatusIdeating:    {StatusBriefAgreed, StatusCancelled},
		StatusBriefAgreed: {StatusPlanned, StatusCancelled},
		StatusPlanned:     {StatusReady, StatusCancelled},
		StatusReady:       {StatusRunning, StatusCancelled},
		StatusRunning:     {StatusDone},
		StatusDone:        nil,
		StatusCancelled:   nil,
	}
	table := AllowedTransitions()
	total := 0
	for _, next := range table {
		total += len(next)
	}
	if total != 11 {
		t.Fatalf("%d edges, want the eleven T0 walked", total)
	}
	for _, s := range Statuses() {
		got, wantNext := table[s], want[s]
		if len(got) != len(wantNext) {
			t.Fatalf("%s: %v, want %v", s, got, wantNext)
		}
		for i := range wantNext {
			if got[i] != wantNext[i] {
				t.Fatalf("%s edge %d: %s, want %s", s, i, got[i], wantNext[i])
			}
		}
	}
}
