package roadmap

// S50 T1 — the door's audited half (audit.go): every audited verb writes
// EXACTLY ONE row with its op, its fixed-template summary and its actor;
// a write that does not happen is audited nothing; and an agent — or an
// anonymous anyone — never reaches the store through these doors (RM4,
// RM2). The one-transaction rule itself is pinned in internal/store.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

var doorPerson = Actor{Kind: "user", ID: "7"}

func auditRows(t *testing.T, s *store.Store, id int64) []store.RoadmapChange {
	t.Helper()
	rows, err := s.RoadmapChangeList(context.Background(), id, 100)
	if err != nil {
		t.Fatalf("read the audit for item %d: %v", id, err)
	}
	return rows
}

// wantLatest pins the newest row (newest-first) and the running count —
// every audited verb adds EXACTLY one row to its item's history, never a
// second row beside the first and never a silent zero.
func wantLatest(t *testing.T, rows []store.RoadmapChange, id int64, wantCount int, op, actorKind, actorID, summary string) {
	t.Helper()
	if len(rows) != wantCount {
		t.Fatalf("item %d has %d audit rows, want %d: %+v", id, len(rows), wantCount, rows)
	}
	r := rows[0]
	if r.Op != op {
		t.Fatalf("op %q, want %q", r.Op, op)
	}
	if r.ActorKind != actorKind || r.ActorID != actorID {
		t.Fatalf("actor %s/%s, want %s/%s", r.ActorKind, r.ActorID, actorKind, actorID)
	}
	if r.Summary != summary {
		t.Fatalf("summary %q, want %q", r.Summary, summary)
	}
	if r.OccurredAt == "" {
		t.Fatal("audit row without occurred_at")
	}
}

func TestEveryRoadmapWriteWritesExactlyOneAuditRow(t *testing.T) {
	root := "/repo/audited"
	r, s := openRoadmapStore(t, root)

	// add — CreateAudited composes its row inside the store (the id is
	// born mid-write); everything else the door composes.
	it, ref := r.CreateAudited(root, NewItem{
		Title: "audited all the way down", Provenance: ProvenanceAuthored,
		Priority: PriorityKnown, PriorityLabel: "now",
	}, doorPerson)
	if ref != nil {
		t.Fatalf("CreateAudited: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 1, "add", "user", "7", "added item 1")

	// ideate (the dispatch verb, walked by the door caller)
	if _, ref := r.TransitionAudited(it.ID, StatusIdeating, "ideate", doorPerson); ref != nil {
		t.Fatalf("ideate: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 2, "ideate", "user", "7", "moved item 1 idea → ideating")

	// estimate
	if _, ref := r.SetEstimateAudited(it.ID, 2, 3, "a pairing day each", doorPerson); ref != nil {
		t.Fatalf("estimate: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 3, "estimate", "user", "7", "estimated item 1 (range with its basis recorded)")

	// brief landing (the reactor's move, ONE row for pointer+status)
	if _, ref := r.LandBriefAudited(it.ID, root+"/brief.md", Actor{Kind: "system", ID: "reactor"}); ref != nil {
		t.Fatalf("brief landing: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 4, "transition", "system", "reactor", "moved item 1 ideating → brief-agreed (brief landed)")

	// plan landing (the planner's move, op plan)
	if _, ref := r.LandPlanAudited(it.ID, root+"/plan.md", Actor{Kind: "system", ID: "reactor"}); ref != nil {
		t.Fatalf("plan landing: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 5, "plan", "system", "reactor", "moved item 1 brief-agreed → planned (plan landed)")

	// the walk to ready rides the plain writers (setup, not under test —
	// the S26 suite pins those doors; T2 gives them their audited twin):
	// planned → ready needs D4 satisfied; SetPriority known was set at
	// create, the estimate and plan pointer are on.
	if _, ref := r.Transition(it.ID, StatusReady); ref != nil {
		t.Fatalf("walk to ready: %s", ref.Reason)
	}

	// materialize transition + loop record (the loop row must EXIST —
	// the composite FK is the store's, queue's rule carried).
	if _, err := s.CreateLoop(store.NewLoop{Slug: "item-1-build", ComponentRoot: root, Goal: "the item's plan"}); err != nil {
		t.Fatalf("seed the loop: %v", err)
	}
	if _, ref := r.TransitionAudited(it.ID, StatusRunning, "materialize", doorPerson); ref != nil {
		t.Fatalf("materialize: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 6, "materialize", "user", "7", "moved item 1 ready → running")
	if _, ref := r.RecordLoopAudited(it.ID, root, "item-1-build", doorPerson); ref != nil {
		t.Fatalf("record loop: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 7, "materialize", "user", "7", "recorded item 1 on loop item-1-build")

	// priority
	if _, ref := r.SetPriorityAudited(it.ID, PriorityUnknown, "", doorPerson); ref != nil {
		t.Fatalf("priority: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, it.ID), it.ID, 8, "priority", "user", "7", "set priority on item 1 to unknown")

	// cancel — its own op (D7's never-delete decision is auditable as
	// itself) — on a second item: R1 carries no running → cancelled edge
	// (a loop in flight is the loop's own record, S24's D11), so cancel
	// rides an item that has not started a loop.
	second, ref := r.CreateAudited(root, NewItem{Title: "cancelled before it started", Provenance: ProvenanceAuthored}, doorPerson)
	if ref != nil {
		t.Fatalf("CreateAudited (second item): %s", ref.Reason)
	}
	if _, ref := r.CancelAudited(second.ID, doorPerson); ref != nil {
		t.Fatalf("cancel: %s", ref.Reason)
	}
	wantLatest(t, auditRows(t, s, second.ID), second.ID, 2, "cancel", "user", "7", "moved item 2 idea → cancelled")
}

func TestWritesRefuseAgentActorAndUnresolvablePeer(t *testing.T) {
	// RM4's refusal carries its own sentence — an agent never writes the
	// roadmap — and the anonymous one names the audit gap; both refuse
	// BEFORE any store touch: the item keeps its state, no row lands, and
	// the create never created anything to audit.
	root := "/repo/refused"
	r, s := openRoadmapStore(t, root)
	seed := mustCreate(t, r, root, "untouched", NewItem{Title: "untouched"})
	if _, ref := r.TransitionAudited(seed.ID, StatusIdeating, "ideate", Actor{Kind: "agent", ID: "brainstormer"}); ref == nil ||
		!strings.Contains(ref.Reason, "agent never writes the roadmap") {
		t.Fatalf("agent actor at TransitionAudited: %v", ref)
	}
	cases := []struct {
		name string
		call func(a Actor) *Refusal
	}{
		{"create", func(a Actor) *Refusal {
			_, ref := r.CreateAudited(root, NewItem{Title: "ghost", Provenance: ProvenanceAuthored}, a)
			return ref
		}},
		{"transition", func(a Actor) *Refusal {
			_, ref := r.TransitionAudited(seed.ID, StatusIdeating, "transition", a)
			return ref
		}},
		{"cancel", func(a Actor) *Refusal { _, ref := r.CancelAudited(seed.ID, a); return ref }},
		{"estimate", func(a Actor) *Refusal {
			_, ref := r.SetEstimateAudited(seed.ID, 1, 2, "a day", a)
			return ref
		}},
		{"priority", func(a Actor) *Refusal {
			_, ref := r.SetPriorityAudited(seed.ID, PriorityKnown, "", a)
			return ref
		}},
		{"brief landing", func(a Actor) *Refusal {
			_, ref := r.LandBriefAudited(seed.ID, root+"/brief.md", a)
			return ref
		}},
		{"plan landing", func(a Actor) *Refusal {
			_, ref := r.LandPlanAudited(seed.ID, root+"/plan.md", a)
			return ref
		}},
		{"record loop", func(a Actor) *Refusal {
			_, ref := r.RecordLoopAudited(seed.ID, root, "item-1", a)
			return ref
		}},
	}
	for _, author := range []Actor{
		{Kind: "agent", ID: "brainstormer"},
		{Kind: "user", ID: ""},
		{Kind: "wizard", ID: "42"},
		{},
	} {
		for _, tc := range cases {
			ref := tc.call(author)
			if ref == nil {
				t.Fatalf("%s accepted author %+v — the door has no gate", tc.name, author)
			}
			if author.Kind == "agent" && author.ID != "" && !strings.Contains(ref.Reason, "agent never writes the roadmap") {
				t.Fatalf("%s refused agent %+v without the RM4 sentence: %s", tc.name, author, ref.Reason)
			}
		}
	}
	// Nothing moved and nothing was audited.
	fresh, ok, err := r.Show(seed.ID)
	if err != nil || !ok || fresh.Status != StatusIdea {
		t.Fatalf("the refusals moved the item: %+v ok=%v err=%v", fresh, ok, err)
	}
	if rows := auditRows(t, s, seed.ID); len(rows) != 0 {
		t.Fatalf("refused writes left %d audit rows, want 0", len(rows))
	}
	if _, found, _ := r.Show(int64(2)); found {
		t.Fatal("the refused creates landed a row")
	}
}

func TestNoOpWritesAreAuditedNothing(t *testing.T) {
	root := "/repo/noop"
	r, s := openRoadmapStore(t, root)
	it := mustCreate(t, r, root, "stays", NewItem{Title: "stays"})

	// A move the machine refuses (idea → done is not an edge) writes
	// nothing — and audits nothing.
	if _, ref := r.TransitionAudited(it.ID, StatusDone, "transition", doorPerson); ref == nil {
		t.Fatal("idea → done was accepted by the audited door")
	}
	// The lost claim: the landing whose FROM no longer matches.
	if _, ref := r.LandBriefAudited(it.ID, root+"/brief.md", Actor{Kind: "system", ID: "reactor"}); ref == nil {
		t.Fatal("a brief landing on an `idea` item was accepted")
	}
	// The loop record off its rung (running only, D6's discipline).
	if _, ref := r.RecordLoopAudited(it.ID, root, "item-1", doorPerson); ref == nil {
		t.Fatal("a loop record landed on a non-running item")
	}
	// The estimate with no basis (D3) — refused before the store.
	if _, ref := r.SetEstimateAudited(it.ID, 1, 2, "", doorPerson); ref == nil {
		t.Fatal("an estimate without basis was accepted")
	}
	// The wrong op for a move.
	if _, ref := r.TransitionAudited(it.ID, StatusIdeating, "landing", doorPerson); ref == nil {
		t.Fatal("an op outside the five move-ops was accepted")
	}
	if rows := auditRows(t, s, it.ID); len(rows) != 0 {
		t.Fatalf("no-op writes left %d audit rows, want 0", len(rows))
	}
}

func TestSummariesAreFixedTemplatesNoUserText(t *testing.T) {
	// SEC-033 carried to this plane: a summary names ids, statuses, enums
	// — NEVER user text. A hostile title must not echo into a single row.
	hostile := "Robert'); DROP TABLE roadmap_item; --\"quoted\" 你好"
	root := "/repo/hostile"
	r, s := openRoadmapStore(t, root)
	it, ref := r.CreateAudited(root, NewItem{Title: hostile, Provenance: ProvenanceAuthored}, doorPerson)
	if ref != nil {
		t.Fatalf("CreateAudited: %s", ref.Reason)
	}
	if _, ref := r.TransitionAudited(it.ID, StatusIdeating, "ideate", doorPerson); ref != nil {
		t.Fatalf("ideate: %s", ref.Reason)
	}
	if _, ref := r.SetEstimateAudited(it.ID, 1, 1, hostile, doorPerson); ref != nil {
		t.Fatalf("estimate: %s", ref.Reason)
	}
	for _, row := range auditRows(t, s, it.ID) {
		if strings.Contains(row.Summary, "DROP") || strings.Contains(row.Summary, "Robert") ||
			strings.Contains(row.Summary, "\u4f60\u597d") || strings.Contains(row.Summary, "quoted") {
			t.Fatalf("summary %q carries user text", row.Summary)
		}
	}
}
