package queue

// T2's gates. The transition test is machine.go's discipline (enumerated
// FROM the table, driven by the full cross product — a hand-written list
// of legal transitions passes forever after somebody adds one); the CRUD
// gates walk D1, D2 and D12 against a real store in a temp directory —
// house style, never the real one (§5).

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func openQueue(t *testing.T, root string) (*Queue, *store.Store) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateComponent(store.NewComponent{Name: "q", Slug: "q", Root: root}); err != nil {
		t.Fatalf("seed the component for %s: %v", root, err)
	}
	return New(s), s
}

func mustAppend(t *testing.T, q *Queue, root, goal string) QueueItem {
	t.Helper()
	it, err := q.Append(root, "dev-loop", goal, root, NewItem{})
	if err != nil {
		t.Fatalf("append %q: %v", goal, err)
	}
	return it
}

func mustStatus(t *testing.T, q *Queue, it QueueItem, to Status) QueueItem {
	t.Helper()
	fresh, r := q.Transition(it, to)
	if r != nil {
		t.Fatalf("transition %d %s→%s: %s", it.ID, it.Status, to, r.Reason)
	}
	return fresh
}

func TestTransitionTableIsExhaustiveAndClosed(t *testing.T) {
	// CG1: the table is the whole machine. Every listed pair succeeds;
	// every pair NOT listed (and not a self-transition) is refused —
	// driven from the 7×7 cross product, never from a hand-list.
	table := AllowedTransitions()
	edges := 0
	for from, targets := range table {
		if !ValidStatus(from) {
			t.Errorf("%q in the table is not one of the seven", from)
		}
		for _, to := range targets {
			edges++
			if res := ValidateTransition(from, to); !res.OK {
				t.Errorf("%s → %s is in the table and was refused: %s", from, to, res.Reason)
			}
		}
	}
	if edges != 11 {
		t.Errorf("%d edges in the table — CG1's enumeration plus the four cancellations is 11; a silent eighth value or extra edge needs naming here and in T1's CHECK", edges)
	}
	checked := 0
	for _, from := range Statuses() {
		allowed := map[Status]bool{}
		for _, to := range table[from] {
			allowed[to] = true
		}
		for _, to := range Statuses() {
			if from == to || allowed[to] {
				continue
			}
			checked++
			if res := ValidateTransition(from, to); res.OK {
				t.Errorf("%s → %s is NOT in the table and was accepted", from, to)
			}
		}
	}
	if checked < 25 {
		t.Errorf("only %d forbidden pairs checked — too few for seven statuses (49 minus 7 self minus 11 legal)", checked)
	}
	// The three named refusals, in their own words.
	if res := ValidateTransition(StatusRunning, StatusCancelled); res.OK || !strings.Contains(res.Reason, "loop's own record") {
		t.Errorf("running → cancelled must refuse naming D11, got %q", res.Reason)
	}
	for _, terminal := range []Status{StatusDone, StatusCancelled} {
		if len(table[terminal]) != 0 {
			t.Errorf("%q is terminal and the table carries %v", terminal, table[terminal])
		}
		if res := ValidateTransition(terminal, StatusPending); res.OK || !strings.Contains(res.Reason, "terminal") {
			t.Errorf("%s → pending must refuse naming the terminality, got %q", terminal, res.Reason)
		}
	}
	// Self-transitions are idempotent, not wrong (machine.go's carried
	// rule — it is what makes a retried reactor write safe).
	for _, s := range Statuses() {
		if res := ValidateTransition(s, s); !res.OK {
			t.Errorf("%s → %s (itself) was refused: %s", s, s, res.Reason)
		}
	}
	// CG2's escape hatch: blocked cancels — "if the person instead
	// cancels the queue item (D12), the next item runs" is only true if
	// this edge exists.
	if !ValidateTransition(StatusBlocked, StatusCancelled).OK {
		t.Error("blocked → cancelled refused — CG2's escape hatch is not in the machine")
	}
	// A refusal names the alternative (house refusal rule).
	if res := ValidateTransition(StatusDone, StatusReady); len(res.Legal) != 0 || res.Reason == "" {
		t.Error("a refusal must carry the table's answer even when there is no alternative")
	}
	if sentence := LegalSentence(StatusReady); !strings.Contains(sentence, "running") || !strings.Contains(sentence, "cancelled") {
		t.Errorf("LegalSentence(ready) = %q — must name what CAN be done", sentence)
	}
}

func TestAppendNeverDisturbsRunningOrDonePositions(t *testing.T) {
	// D1: appending while the queue runs never disturbs the order of
	// items already materialized or started.
	q, s := openQueue(t, "/repo/append")
	done := mustAppend(t, q, "/repo/append", "shipped")
	done = mustStatus(t, q, done, StatusReady)
	done = mustStatus(t, q, done, StatusRunning)
	done = mustStatus(t, q, done, StatusDone)
	running := mustAppend(t, q, "/repo/append", "in flight")
	running = mustStatus(t, q, running, StatusReady)
	running = mustStatus(t, q, running, StatusRunning)

	for i := 0; i < 5; i++ {
		mustAppend(t, q, "/repo/append", "newcomer")
	}
	items, err := q.List("/repo/append")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 {
		t.Fatalf("%d items, want 7", len(items))
	}
	for _, it := range items {
		switch it.Goal {
		case "shipped":
			if it.Position != 1 || it.Status != StatusDone {
				t.Errorf("the done item moved to %d/%s — its position is history (D12)", it.Position, it.Status)
			}
		case "in flight":
			if it.Position != 2 || it.Status != StatusRunning {
				t.Errorf("the running item moved to %d/%s while five appends landed", it.Position, it.Status)
			}
		default:
			if it.Position < 3 || it.Status != StatusPending {
				t.Errorf("newcomer at %d/%s — appends land at the tail, pending either mode (CG1)", it.Position, it.Status)
			}
		}
	}
	// Positions are a dense tail, no ties, from the store's own list.
	for i, it := range items {
		if it.Position != i+1 {
			t.Errorf("item %d sits at %d — the tail assignment skipped or duplicated a slot", i+1, it.Position)
		}
	}
	// And the CG3 default: workspace_root rides the component root when
	// the appender said nothing.
	if items[2].WorkspaceRoot != "/repo/append" {
		t.Errorf("workspace root %q — CG3 defaults it to the project root", items[2].WorkspaceRoot)
	}
	// The three-state attended triplet survives the round trip (D5).
	yes := true
	no := false
	dyn, err := q.Append("/repo/append", "dev-loop", "declared yes", "", NewItem{Attended: &yes})
	if err != nil {
		t.Fatal(err)
	}
	fno, err := q.Append("/repo/append", "dev-loop", "declared no", "", NewItem{Attended: &no})
	if err != nil {
		t.Fatal(err)
	}
	never := mustAppend(t, q, "/repo/append", "never declared")
	if dyn.Attended == nil || !*dyn.Attended || fno.Attended == nil || *fno.Attended || never.Attended != nil {
		t.Errorf("attended lost a state: %v %v %v", dyn.Attended, fno.Attended, never.Attended)
	}
	// `with` round-trips; a corrupt one REFUSES rather than reading empty.
	// The corruption is planted through the store's OWN door (a raw
	// UPDATE from a sibling package would reach for the handle, which the
	// layering forbids) — AppendQueueItem takes the JSON string verbatim,
	// so it is the honest way a row goes bad under us.
	withIt, err := q.Append("/repo/append", "dev-loop", "with map", "", NewItem{With: map[string]string{"focus": "retry"}})
	if err != nil {
		t.Fatal(err)
	}
	if withIt.With["focus"] != "retry" {
		t.Errorf("with map lost: %v", withIt.With)
	}
	bad := "{oops"
	corrupt, err := s.AppendQueueItem(store.NewQueueItemRow{
		ComponentRoot: "/repo/append", Status: "pending", Blueprint: "dev-loop",
		Goal: "corrupt with", WorkspaceRoot: "/repo/append", WithJSON: &bad,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := q.Show(corrupt.ID); err == nil || !strings.Contains(err.Error(), "corrupt") || ok {
		t.Errorf("a corrupt with map read as ok=%v err=%v — corrupt is refused, never silently empty", ok, err)
	}
}

func TestRemoveIsCancelNeverDelete(t *testing.T) {
	// D12: Remove cancels. The row count before and after is identical;
	// only `status` changed — the same "never silently lose a record"
	// pattern loop_archive and policy_event already follow.
	q, s := openQueue(t, "/repo/remove")
	a := mustAppend(t, q, "/repo/remove", "stay")
	b := mustAppend(t, q, "/repo/remove", "leave")
	before, err := s.ListQueueItems("/repo/remove")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("seed is %d rows", len(before))
	}
	fresh, r := q.Remove(b.ID)
	if r != nil {
		t.Fatalf("remove refused: %s", r.Reason)
	}
	if fresh.Status != StatusCancelled {
		t.Errorf("status %s — Remove must land on cancelled", fresh.Status)
	}
	after, err := s.ListQueueItems("/repo/remove")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Errorf("%d rows after Remove — the row count never changes (D12)", len(after))
	}
	if _, ok, err := s.QueueItemByID(b.ID); !ok || err != nil {
		t.Error("the cancelled row is GONE — that was a delete")
	}
	// Re-Removing a cancelled item is IDEMPOTENT, not wrong — machine.go's
	// carried rule (a retried write is safe), and the honest CLI answer
	// to "remove it twice" is "it is removed". What must stay impossible
	// is the move OUT of cancelled, which the table test drives from the
	// cross product.
	fresh2, r := q.Remove(b.ID)
	if r != nil || fresh2.Status != StatusCancelled {
		t.Errorf("second Remove answered %v/%v — re-asserting cancelled is idempotent (machine.go's rule)", r, fresh2)
	}
	// And a RUNNING item's Remove is a refusal, not silence (D11).
	run := mustAppend(t, q, "/repo/remove", "running")
	run = mustStatus(t, q, run, StatusReady)
	run = mustStatus(t, q, run, StatusRunning)
	if _, r := q.Remove(run.ID); r == nil || r.Moved || !strings.Contains(r.Reason, "loop's own record") {
		t.Errorf("running item's Remove answered %v — D11 demands the refusal name the loop", r)
	}
	if _, ok, _ := q.Show(a.ID); !ok {
		t.Error("the untouched row vanished")
	}
}

func TestNextReadyRespectsAtMostOneRunning(t *testing.T) {
	// D2: nothing is next while ANY item in the project is running.
	q, _ := openQueue(t, "/repo/next")
	first := mustAppend(t, q, "/repo/next", "one")
	second := mustAppend(t, q, "/repo/next", "two")
	third := mustAppend(t, q, "/repo/next", "three")

	if it, ok, r := q.NextReady("/repo/next"); ok || r != nil {
		t.Errorf("nothing is ready and NextReady answered %v %v", it.Goal, r)
	}
	for _, it := range []QueueItem{first, second, third} {
		mustStatus(t, q, it, StatusReady)
	}
	// Lowest position first.
	it, ok, r := q.NextReady("/repo/next")
	if !ok || r != nil || it.Goal != "one" {
		t.Fatalf("NextReady answered %v (ok=%v refusal=%v), want the lowest position", it.Goal, ok, r)
	}
	// Start it: now NOTHING is next — the ready twins wait.
	run := mustStatus(t, q, it, StatusRunning)
	if _, ok, r := q.NextReady("/repo/next"); ok || r != nil {
		t.Errorf("an item is running and NextReady still answered (ok=%v refusal=%v) — D2 broken", ok, r)
	}
	// Finish it: next-lowest answers.
	mustStatus(t, q, run, StatusDone)
	it2, ok, r := q.NextReady("/repo/next")
	if !ok || r != nil || it2.Goal != "two" {
		t.Fatalf("after done, NextReady answered %v (ok=%v)", it2.Goal, ok)
	}
	// A BLOCKED item HALTS this read too — amended at the T9 walk (dated
	// 2026-10-29), which found the explicit `queue.advance` door starting
	// the next ready item over a blocked row. The original comment here
	// delegated the halt ("the REACTOR's halt is T3's") while T3 pinned
	// only the question-raising — so the design's own named gap ("the one
	// test that would catch a shortcut") was owned by nobody, exactly as
	// CG2 predicted. The selection read is where "what may start" is
	// answered, so that is where any-blocked-halts-unconditionally lives:
	// all four advancers inherit it from here.
	run2 := mustStatus(t, q, it2, StatusRunning)
	mustStatus(t, q, run2, StatusBlocked)
	thirdReady, ok, r := q.NextReady("/repo/next")
	if r != nil {
		t.Fatal(r.Reason)
	}
	if ok {
		t.Errorf("a blocked row did not halt advancing — CG2 halts regardless of queueAdvance, and NextReady is the read every advancer goes through: answered %q", thirdReady.Goal)
	}
	// The halt resolves the way CG2 says it does: cancel the blocked row
	// (D12's door — cancel, never delete) and the queue advances again.
	// Without this control the refusal above could be a broken query.
	if _, cr := q.Remove(run2.ID); cr != nil {
		t.Fatalf("cancel the blocked row: %s", cr.Reason)
	}
	if thirdReady, ok, r = q.NextReady("/repo/next"); !ok || r != nil || thirdReady.Goal != "three" {
		t.Fatalf("the read stayed shut after the halt was cancelled: ok=%v refusal=%v", ok, r)
	}
	// Cross-project isolation: another root's running item cannot silence
	// this one, and vice versa.
	q2, _ := openQueue(t, "/repo/other")
	other := mustAppend(t, q2, "/repo/other", "mine")
	mustStatus(t, q2, other, StatusReady)
	if _, ok, _ := q2.NextReady("/repo/other"); !ok {
		t.Error("a stranger root's silence reached this queue")
	}
}

func TestReorderMovesMovableAndFixesHistory(t *testing.T) {
	// D12: a pending item can be reordered; running/done positions are
	// fixed. T2's recorded call: mid-`sizing` moves too (its position is
	// independent of its sizing run).
	q, _ := openQueue(t, "/repo/reorder")
	a := mustAppend(t, q, "/repo/reorder", "a")
	b := mustAppend(t, q, "/repo/reorder", "b")
	c := mustAppend(t, q, "/repo/reorder", "c")
	mustAppend(t, q, "/repo/reorder", "d")
	run := mustStatus(t, q, mustAppend(t, q, "/repo/reorder", "running"), StatusReady)
	run = mustStatus(t, q, run, StatusRunning)

	if _, r := q.Reorder(run.ID, 1); r == nil || strings.Contains(r.Reason, "does not exist") {
		t.Errorf("a running item's reorder answered %v — history is fixed (D12)", r)
	}
	if _, r := q.Reorder(mustStatus(t, q, c, StatusReady).ID, 4); r != nil { // c to the back of the movables
		t.Fatalf("moving a ready item refused: %s", r.Reason)
	}
	// Move b ahead of a and the ready c: b to position 1 among movables.
	if _, r := q.Reorder(b.ID, 1); r != nil {
		t.Fatalf("reorder refused: %s", r.Reason)
	}
	items, err := q.List("/repo/reorder")
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, it := range items {
		if it.Status == StatusRunning {
			if it.Position != 5 {
				t.Errorf("the running item drifted to %d while its queue was reordered", it.Position)
			}
			continue
		}
		order = append(order, string(it.Status)+":"+it.Goal)
	}
	// d never moved by hand — it shifts up as b cuts in. c was sent to
	// the back of the movables by the explicit Reorder(c, 4) above, so
	// the order this asserts is b-front, then a, d, and c at the back —
	// measured against the sequence the test itself performed, not
	// against what a fresh queue would say.
	want := []string{"pending:b", "pending:a", "pending:d", "ready:c"}
	if len(order) != len(want) {
		t.Fatalf("movable count %v, want %d", order, len(want))
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("movable order %v — want %v (b to the front shifts the rest up; d rides along)", order, want)
		}
	}
	// A sizing item moves too — the recorded call.
	sizing := mustStatus(t, q, mustAppend(t, q, "/repo/reorder", "sizing"), StatusSizing)
	if _, r := q.Reorder(sizing.ID, 2); r != nil {
		t.Errorf("a mid-sizing item refused to move — the recorded call says its position is independent of its run: %s", r.Reason)
	}
	// Out-of-range answers with the real movable count, not a silent clamp.
	if _, r := q.Reorder(a.ID, 99); r == nil || !strings.Contains(r.Reason, "item(s) can move") {
		t.Errorf("position 99 answered %v", r)
	}
}

func TestTransitionClaimsTheRace(t *testing.T) {
	// The claimed-FROM contract: a row that moved under the caller loses
	// with Moved=true, and the loser's re-read is the honest next step.
	q, s := openQueue(t, "/repo/race")
	it := mustAppend(t, q, "/repo/race", "contested")
	if _, err := s.TransitionQueueItem(it.ID, "pending", "ready"); err != nil {
		t.Fatal(err)
	}
	// `it` still says pending in the caller's hands.
	if _, r := q.Transition(it, StatusReady); r == nil || !r.Moved {
		t.Errorf("the stale caller won: %v — the claimed-FROM update must lose honestly", r)
	}
	if _, err := s.TransitionQueueItem(it.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	if _, r := q.Transition(it, StatusCancelled); r == nil || !strings.Contains(r.Reason, "moved under") {
		t.Errorf("race refusal lost its sentence: %v", r)
	}
}
