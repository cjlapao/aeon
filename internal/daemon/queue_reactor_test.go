package daemon

// S24 T3 — the reactor's gates. The harness starts a pinned loop the way
// the daemon's own tests already do (CreatePinnedLoop) and fires the
// daemon's REAL emission door (publishLoop), so the wiring at the
// emission point is under test, not just the deciding function. Where a
// decision needs the store, the test seeds the queue through
// internal/queue — the one door — never raw SQL (a test that hand-plants
// rows is testing its own fixtures).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

type reactorFixture struct {
	d    *Daemon
	s    *store.Store
	q    *queue.Queue
	root string
	// starts receives every StartBlueprintLoop the reactor performs; the
	// reactor calls the daemon's own door, and the test replaces only
	// that seam — the same post-wire swap every protocol test uses.
	starts  chan protocol.LoopStartParams
	ids     map[string]string
	clock   *testClock
	spyErr  error
	spySlug func(n int) string
	release chan struct{} // when non-nil, each start waits here
}

func newReactorFixture(t *testing.T) *reactorFixture {
	t.Helper()
	tc := &testClock{}
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: tc.now})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify})
	root := "/repo/queue"
	if _, err := s.CreateComponent(store.NewComponent{Name: "q", Slug: "q", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	f := &reactorFixture{d: d, s: s, q: queue.New(s), root: root, clock: tc,
		starts: make(chan protocol.LoopStartParams, 8),
		ids:    map[string]string{},
		spySlug: func(n int) string {
			return fmt.Sprintf("loop-%d", n)
		}}
	d.Table.StartBlueprintLoop = func(ctx context.Context, p protocol.LoopStartParams) (any, error) {
		if f.release != nil {
			<-f.release
		}
		// Compute the slug BEFORE the send: once p is on the channel the
		// test may drain it at any moment, and len(f.starts) would race
		// to 0 (measured — the stamp landed as "loop-0" that way).
		slug := f.spySlug(len(f.starts) + 1)
		f.starts <- p
		if f.spyErr != nil {
			return nil, f.spyErr
		}
		// The spy stands in for loop.start ENTIRELY — including the row
		// it creates, or the item's pointer would FK-fail on a loop that
		// the door promised exists.
		if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: p.Goal, Status: "running",
			Attended: p.Attended}, p.Blueprint, 1, queueBlueprintYAML, "{}"); err != nil {
			t.Errorf("spy could not pin %s: %v", slug, err)
		}
		return map[string]any{"id": slug, "slug": slug, "status": "running"}, nil
	}
	return f
}

func (f *reactorFixture) appendReady(t *testing.T, goal string, attended *bool) queue.QueueItem {
	t.Helper()
	in := queue.NewItem{Attended: attended}
	it, err := f.q.Append(f.root, "dev-loop", goal, f.root, in)
	if err != nil {
		t.Fatalf("append %q: %v", goal, err)
	}
	it, r := f.q.Transition(it, queue.StatusReady)
	if r != nil {
		t.Fatalf("ready %q: %s", goal, r.Reason)
	}
	return it
}

// seedLoop puts the loop row on the books FIRST — the item's loop_slug FK
// needs it (the materialization pointer references a loop that exists),
// and the pin is what publishLoop resolves against. Emission is the
// separate call below: a loop is born running, its terminal comes later.
func (f *reactorFixture) seedLoop(t *testing.T, slug string) {
	t.Helper()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"},
		"dev-loop", 1, queueBlueprintYAML, "{}")
	if err != nil {
		t.Fatalf("pin loop %s: %v", slug, err)
	}
	// publishLoop resolves the loopId — the REAL id, which the loop
	// engine's emit carries; the slug is what the item's pointer holds.
	f.ids[slug] = l.ID
}

// materialize drives the real emission path: publishLoop is exactly what
// the loop engine's emit calls, hook and all.
func (f *reactorFixture) materialize(t *testing.T, slug, status, reason string) {
	t.Helper()
	fields := map[string]any{"loopId": f.ids[slug], "status": status}
	if reason != "" {
		fields["reason"] = reason
	}
	kind := protocol.KindLoopFinished
	if status == "blocked" {
		kind = protocol.KindLoopBlocked
	}
	f.d.publishLoop(kind, fields)
}

func await(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *reactorFixture) itemByID(t *testing.T, id int64) store.QueueItemRow {
	t.Helper()
	row, ok, err := f.s.QueueItemByID(id)
	if err != nil || !ok {
		t.Fatalf("read item %d: ok=%v err=%v", id, ok, err)
	}
	return row
}

// queueBlueprintYAML is the smallest blueprint the PIN parser accepts
// (loop.Pinned parses it — an unparseable pin never reaches any answer
// door, measured): a name, a schema version, and one lane.
const queueBlueprintYAML = "id: dev-loop\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: dev}\n"

// testClock is the store's injectable clock (I9): T4's halt gate advances
// TIME through it — the store's rows ride it, and the proof that the halt
// has no timer is made against a clock that actually moved.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.t.IsZero() {
		return time.Now()
	}
	return c.t
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	base := c.t
	if base.IsZero() {
		base = time.Now()
	}
	c.t = base.Add(d)
}

func readFileForTest(t *testing.T, name string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(name) // test binary runs with the package dir as cwd
	return string(b), err
}

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func TestAutoAdvanceMaterializesOnFinished(t *testing.T) {
	// CG2's done branch + CG3's payload + D5's carry-unchanged, all in
	// one walk: `auto`, the first item finishes, the second starts with
	// EXACTLY its own declared fields through the one loop.start door.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	first := f.appendReady(t, "shipped thing", nil)
	second := f.appendReady(t, "next thing", no())
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	claimed, err := f.s.TransitionQueueItem(first.ID, "ready", "running")
	if err != nil || !claimed {
		t.Fatalf("claim first: %v %v", claimed, err)
	}
	f.materialize(t, "loop-one", "done", "")

	var got protocol.LoopStartParams
	select {
	case got = <-f.starts:
	case <-time.After(3 * time.Second):
		t.Fatal("auto advance never reached the loop.start door")
	}
	if got.Blueprint != "dev-loop" || got.Goal != "next thing" || got.WorkspaceRoot != f.root {
		t.Errorf("start carried %+v — CG3 says the item's own payload, verbatim", got)
	}
	if got.Attended == nil || *got.Attended {
		t.Errorf("attended came as %v — the item DECLARED false; the daemon starting it with nobody watching must not flip it (D5)", got.Attended)
	}
	await(t, "second item running with its loop stamped", func() bool {
		row := f.itemByID(t, second.ID)
		return row.Status == "running" && row.LoopSlug != nil && *row.LoopSlug == "loop-1"
	})
	await(t, "first item done", func() bool { return f.itemByID(t, first.ID).Status == "done" })
}

func TestAskModeRaisesGateQuestionInsteadOfStarting(t *testing.T) {
	// D3: `ask` surfaces a pending question and advances ONLY on answer
	// (T4 completes the answer half). And D4's independence lives here in
	// the data: `ask` is a component setting consulted per event, never
	// folded into any blueprint or policy.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "ask", "manual"); err != nil {
		t.Fatal(err)
	}
	first := f.appendReady(t, "shipped", nil)
	second := f.appendReady(t, "decide the retry policy", nil)
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	f.materialize(t, "loop-one", "done", "")
	await(t, "the gate question", func() bool {
		q, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
		return err == nil && found && q.Kind == string(store.QuestionGate) && strings.Contains(q.Body, "decide the retry policy")
	})
	// The question hangs on the JUST-FINISHED loop, not the next
	// (not-yet-existent) one — design §2's FK finding.
	q, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
	if !found || err != nil {
		t.Fatal("question is not on the finished loop's slug")
	}
	if !strings.Contains(q.Body, "next queued item:") || !strings.Contains(q.Body, "advance?") {
		t.Errorf("question body %q — must name the next item plainly enough to decide", q.Body)
	}
	select {
	case p := <-f.starts:
		t.Fatalf("ask mode started %q with nobody answering", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
	if row := f.itemByID(t, second.ID); row.Status != "ready" {
		t.Errorf("second item reads %s — ask leaves it ready until answered", row.Status)
	}
}

func TestBlockedAlwaysHaltsRegardlessOfQueueAdvance(t *testing.T) {
	// CG2: `auto` does NOT mean auto-advance past a blocked loop. No
	// sub-classification of the reason (the taxonomy lives in
	// internal/loop), and the halt is a question, not silence.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	first := f.appendReady(t, "stalling thing", nil)
	f.appendReady(t, "waiting thing", nil)
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	f.materialize(t, "loop-one", "blocked", "loop ceiling reached")
	await(t, "the item to read blocked", func() bool { return f.itemByID(t, first.ID).Status == "blocked" })
	q, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
	if !found || err != nil || q.Kind != string(store.QuestionGate) {
		t.Fatalf("blocked under `auto` raised no gate question (found=%v err=%v) — the halt must reach a person", found, err)
	}
	if !strings.Contains(q.Body, "loop ceiling reached") || !strings.Contains(q.Body, "blocked") {
		t.Errorf("question %q must name what blocked and quote the reason", q.Body)
	}
	select {
	case p := <-f.starts:
		t.Fatalf("`auto` advanced past a blocked loop by starting %q", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestUntrackedLoopFinishingIsANoop(t *testing.T) {
	// T3 §3: a person's own loop finishing is not the queue's news —
	// quietly nothing, and no error logged as a claim either.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	f.appendReady(t, "untouched", nil)
	f.seedLoop(t, "someone-elses-loop")
	f.materialize(t, "someone-elses-loop", "done", "")
	select {
	case p := <-f.starts:
		t.Fatalf("the reactor started %q for a loop it never minted", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
	if row := f.itemByID(t, f.appendReady(t, "probe", nil).ID); row.Status != "ready" {
		t.Error("an untracked finish disturbed the queue")
	}
}

func TestColdStartNeverMaterializesWithoutExplicitCall(t *testing.T) {
	// CG4: appending to an idle queue starts NOTHING — the first item of
	// a project is the explicit call's decision (the CLI verb / T7's
	// queue.advance), never the daemon noticing an idle non-empty queue.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	f.appendReady(t, "first ever", nil)
	select {
	case p := <-f.starts:
		t.Fatalf("cold start spontaneously began %q — nothing in Aeon spontaneously starts the first unit of work", p.Goal)
	case <-time.After(200 * time.Millisecond):
	}
	// And the explicit door DOES start it — the same advanceNext, called
	// on purpose (this is the door T6/T7 wrap).
	if !f.d.advanceNext(context.Background(), f.root) {
		t.Fatal("the explicit advance started nothing")
	}
	select {
	case p := <-f.starts:
		if p.Goal != "first ever" {
			t.Errorf("explicit advance started %q", p.Goal)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit advance never reached the loop.start door")
	}
}

func TestFinishedWithAnUnknownStatusIsNotTheQueuesBusiness(t *testing.T) {
	// §2 measured `done` as the only status loop.finished carries — the
	// reactor ASSERTS it rather than trusting the measurement forever.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	item := f.appendReady(t, "watched", nil)
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(item.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(item.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	f.materialize(t, "loop-one", "idle", "")
	select {
	case p := <-f.starts:
		t.Fatalf("an `idle` loop.finished started %q — unknown statuses move nothing", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
	if row := f.itemByID(t, item.ID); row.Status != "running" {
		t.Errorf("the item read %s after a status the reactor does not know — it should still be waiting for its real terminal", row.Status)
	}
}

func TestStartFailureBlocksTheItemAndAsks(t *testing.T) {
	// A claim that could not start is a stall a person sees: CG1 has no
	// running → ready edge (recorded decision), so the honest word is
	// blocked, with the same pending question every halt earns.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	first := f.appendReady(t, "shipped", nil)
	f.appendReady(t, "unstartable", nil)
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	f.spyErr = fmt.Errorf("blueprint dev-loop not found")
	f.materialize(t, "loop-one", "done", "")
	await(t, "the unstartable item to read blocked", func() bool {
		items, err := f.q.List(f.root)
		if err != nil {
			return false
		}
		for _, it := range items {
			if it.Goal == "unstartable" {
				return it.Status == queue.StatusBlocked
			}
		}
		return false
	})
	// D2 survives the failure: only one start attempt was ever made.
	select {
	case <-f.starts:
	default:
		t.Fatal("the failed start never even reached the door")
	}
	select {
	case p := <-f.starts:
		t.Fatalf("after a failed claim the reactor started %q anyway — two starts is a D2 breach", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestQueueNeverDelaysTheEmitPath(t *testing.T) {
	// V18 at the reactor's own door: the emit path returns while the
	// materialization is still in flight — asserted on WALL TIME of
	// publishLoop with the start door held shut, not on lock introspection.
	f := newReactorFixture(t)
	f.release = make(chan struct{})
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	item := f.appendReady(t, "shipped", nil)
	ready := f.appendReady(t, "next", nil)
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(item.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(item.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	f.materialize(t, "loop-one", "done", "")
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("publishLoop took %v with the start door held shut — the queue delayed the emit path", elapsed)
	}
	close(f.release)
	await(t, "the held start to land", func() bool {
		return f.itemByID(t, ready.ID).Status == "running"
	})
}

func TestReactorCallsExistingLoopStartNeverASecondPath(t *testing.T) {
	// Brief §4's "no second execution path", pinned at the source: the
	// reactor reaches loop.start through the daemon's OWN door — the
	// identical seam the socket speaks — and never assembles a loop by
	// hand. A grep pin, per the house way (a behavioural test cannot
	// distinguish calling loop.start from nearly-calling it).
	src, err := readFileForTest(t, "queue_reactor.go")
	if err != nil {
		t.Fatalf("read the reactor source: %v", err)
	}
	if !strings.Contains(src, "d.Table.StartBlueprintLoop(") {
		t.Error("the reactor does not go through d.Table.StartBlueprintLoop — the one loop.start door")
	}
	for _, banned := range []string{"startLoopWith(", "CreateLoop(", "CreatePinnedLoop("} {
		if strings.Contains(src, banned) {
			t.Errorf("the reactor assembles loops itself via %s — second execution path (brief §4)", banned)
		}
	}
}
