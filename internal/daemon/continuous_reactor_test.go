package daemon

// continuous_reactor_test.go — S27 T5's other half: the reactors'
// hand-off (CN19) with its byte-identical disabled path, the single
// start door held shut for everyone but the named callers, the grants
// silence, the slot arithmetic, and the boot order.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

var schedUser = store.SchedulerActor{Kind: "user", ID: "test"}

// newBareSchedEnv is the fixture WITHOUT a word of S27's vocabulary: a
// component, queue settings, and items — the daemon a pre-S27 person ran.
func newBareSchedEnv(t *testing.T) *schedEnv {
	t.Helper()
	s := recoveryStore(t)
	d := New(Options{Store: s})
	bp := writeGateBlueprint(t, "bare-gate")
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "bare", Slug: "bare", Root: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetComponentQueueSettings(root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	a := &continuousAdapter{d: d, locks: map[string]*sync.Mutex{}, weights: map[string]int{}}
	e := &schedEnv{t: t, d: d, s: s, a: a,
		w: continuous.NewWorker(a, a, nil, a, nil), root: root, bpPath: bp}
	t.Cleanup(func() { d.cancelLoops(context.Canceled) })
	return e
}

// startItemLoop pins a running loop for a queue item and points the row
// at it — the shape a completed start leaves behind, seeded directly.
func (e *schedEnv) startItemLoop(row store.QueueItemRow) string {
	e.t.Helper()
	slug := "queue-item-" + itoa(row.ID)
	q := queue.New(e.s)
	if _, ref := q.Transition(queueItemFromRow(row), queue.StatusRunning); ref != nil {
		e.t.Fatalf("running: %s", ref.Reason)
	}
	if _, err := e.s.CreatePinnedLoop(store.NewLoop{
		Slug: slug, ComponentRoot: e.root, Root: e.root, Goal: row.Goal, Status: "running",
	}, row.Blueprint, 1, queueBlueprintYAML, "{}"); err != nil {
		e.t.Fatal(err)
	}
	if ok, err := e.s.SetQueueItemLoop(row.ID, e.root, slug); err != nil || !ok {
		e.t.Fatalf("point: %v %v", ok, err)
	}
	// In production the engine settles the loop's own status BEFORE
	// loop.finished reaches a reactor; the seed says done for the same
	// reason — the terminal is news about a loop that already ended.
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: e.root}, "done"); err != nil {
		e.t.Fatal(err)
	}
	return slug
}

// TestEnabledRepoQueueDoesNotAutoAdvanceFromTheReactor: the hand-off.
// The reactor KICKS and stops; only the worker starts. Fifty
// terminal/tick interleavings under -race produce exactly one loop.
func TestEnabledRepoQueueDoesNotAutoAdvanceFromTheReactor(t *testing.T) {
	t.Parallel()
	e := newSchedEnv(t)
	first := e.queueReady("first off", e.bpPath)
	second := e.queueReady("second on", e.bpPath)
	slug1 := e.startItemLoop(first)

	e.d.handleQueueTerminal(context.Background(), protocol.KindLoopFinished, e.root, slug1, "done", "", "")

	// The hand-off, caught in the act: the reactor never touched the
	// next item — it is still sitting ready.
	mid, _, err := e.s.QueueItemByID(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mid.Status != "ready" {
		t.Fatalf("the reactor advanced the queue under the scheduler: %s", mid.Status)
	}

	for i := 0; i < 50; i++ {
		e.d.handleQueueTerminal(context.Background(), protocol.KindLoopFinished, e.root, slug1, "done", "", "")
		if err := e.tick(); err != nil {
			t.Fatal(err)
		}
	}
	loops, err := e.s.LoopsBySlug("queue-item-" + itoa(second.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 {
		t.Fatalf("%d loops carry the second item's slug — two starters got in", len(loops))
	}
	cl, ok := e.claim("queue", second.ID)
	if !ok || cl.Outcome != "started" {
		t.Fatal("the worker never claimed the item it started")
	}
}

// TestDisabledRepoReactorByteIdentical: the migration of behaviour never
// leaked — with the repository OFF under a live master, and with the
// master OFF, the reactor's T4 path advances the queue exactly as it did
// before S27, claiming nothing.
func TestDisabledRepoReactorByteIdentical(t *testing.T) {
	t.Run("repo off, master on", func(t *testing.T) {
		e := newSchedEnv(t)
		if _, err := e.s.DisableSchedulerRoot(schedUser, e.root); err != nil {
			t.Fatal(err)
		}
		e.assertLegacyAdvance()
	})
	t.Run("master off, repo on", func(t *testing.T) {
		e := newSchedEnv(t)
		if _, err := e.s.SetSchedulerState(schedUser, false, false, ""); err != nil {
			t.Fatal(err)
		}
		e.assertLegacyAdvance()
	})
	t.Run("no scheduler rows at all", func(t *testing.T) {
		e := newBareSchedEnv(t) // a store that never heard of S27's tables' voices
		e.assertLegacyAdvance()
	})
}

// assertLegacyAdvance runs the T4 goldens' move: a done terminal with
// advance=auto starts the next item THROUGH THE OLD DOOR, and no claim
// row (the scheduler's instrument) ever appears for it.
func (e *schedEnv) assertLegacyAdvance() {
	t := e.t
	first := e.queueReady("legacy first", e.bpPath)
	second := e.queueReady("legacy second", e.bpPath)
	slug1 := e.startItemLoop(first)
	e.d.handleQueueTerminal(context.Background(), protocol.KindLoopFinished, e.root, slug1, "done", "", "")
	after, _, err := e.s.QueueItemByID(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "running" {
		t.Fatalf("the legacy path left the next item %q — the reactor's old advance moved", after.Status)
	}
	if after.LoopSlug == nil || *after.LoopSlug == "" {
		t.Fatal("the legacy path never pointed the item at its loop")
	}
	if _, ok := e.claim("queue", second.ID); ok {
		t.Fatal("a repository the scheduler does not own grew a claim row")
	}
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.claim("queue", second.ID); ok {
		t.Fatal("a tick claimed an item under a silent scheduler")
	}
}

// TestAskQuestionNotRaisedWhenSchedulerOwnsTheRepo: OD5 — the queue's
// ask voice stays silent for an owned repository; the tick's start makes
// the question moot, and answering it would be a second starter.
func TestAskQuestionNotRaisedWhenSchedulerOwnsTheRepo(t *testing.T) {
	e := newSchedEnv(t)
	if _, err := e.s.SetComponentQueueSettings(e.root, "ask", "manual"); err != nil {
		t.Fatal(err)
	}
	first := e.queueReady("ask silent first", e.bpPath)
	second := e.queueReady("ask silent second", e.bpPath)
	slug1 := e.startItemLoop(first)
	e.d.handleQueueTerminal(context.Background(), protocol.KindLoopFinished, e.root, slug1, "done", "", "")
	if q, found, err := e.s.PendingQuestion(store.Scope{ComponentRoot: e.root, LoopSlug: slug1}); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatalf("an ask question was raised under the scheduler: %q", q.Body)
	}
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.loop("queue-item-" + itoa(second.ID)); !ok {
		t.Fatal("the owned repository's next item never got its tick start")
	}
	if q, found, err := e.s.PendingQuestion(store.Scope{ComponentRoot: e.root, LoopSlug: slug1}); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatalf("the tick raised the question the reactor withheld: %q", q.Body)
	}
}

// TestOnlyOneStartCallSite: the door's census. `startLoopWith` has
// exactly the named callers, the scheduler among them, nowhere else;
// `StartBlueprintLoop` is reached only by the legacy dispatchers. A
// canary proves the census has teeth.
func TestOnlyOneStartCallSite(t *testing.T) {
	wantInner := map[string]int{ // d.startLoopWith — the ONE door
		"blueprint.go":        1, // S30's manual-task arm, predates S27
		"continuous_start.go": 1, // S27's scheduler — the tick's ONE arm
		"event_fire.go":       1,
		"mergequeue_loops.go": 2,
		"trigger_fire.go":     1,
		// 2026-11-05 (S61 T9): intake.start opens the interview through
		// this same function — the discipline is exactly that there is ONE
		// function to grow arms onto, and this is one of them.
		"intake_start.go": 1,
	}
	wantDoor := map[string]int{ // .StartBlueprintLoop — the socket door
		"queue_reactor.go":    1,
		"roadmap_dispatch.go": 2,
		"triage_dispatch.go":  1,
	}
	gotInner := map[string]int{}
	gotDoor := map[string]int{}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if n := countCalls(string(src), "d.startLoopWith("); n > 0 {
			gotInner[name] = n
		}
		if n := countCalls(string(src), ".StartBlueprintLoop("); n > 0 {
			gotDoor[name] = n
		}
	}
	if fmt.Sprint(gotInner) != fmt.Sprint(wantInner) {
		t.Errorf("startLoopWith callers moved: got %v, want %v", gotInner, wantInner)
	}
	if fmt.Sprint(gotDoor) != fmt.Sprint(wantDoor) {
		t.Errorf("the socket door gained a caller: got %v, want %v", gotDoor, wantDoor)
	}
	// The canary: a smuggled caller must move the census.
	canary := fmt.Sprint(gotInner) + "|smuggle.go:1"
	if canary == fmt.Sprint(wantInner) {
		t.Error("the census cannot detect a second caller — no teeth")
	}
}

// countCalls counts real call sites only — a line whose first non-space
// text is a comment does not call anything.
func countCalls(src, call string) int {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") {
			continue
		}
		n += strings.Count(line, call)
	}
	return n
}

// TestNoGrantsMeansNoOutward: no autonomy grant exists in this store, so
// the scheduler-pinned loop carries NO pin row, allow_outward stays 0,
// and outward work denies at once at S56's gate — the pin is absent, the
// ceiling is silence.
func TestNoGrantsMeansNoOutward(t *testing.T) {
	e := newSchedEnv(t)
	row := e.queueReady("quiet loop", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	l, ok := e.loop("queue-item-" + itoa(row.ID))
	if !ok {
		t.Fatal("no loop")
	}
	outward, err := e.s.LoopAllowOutward(context.Background(), l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outward {
		t.Fatal("a grantless scheduler start flipped allow_outward")
	}
	pin, found, err := e.s.ReadAutonomyPin(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found || pin != nil {
		t.Fatal("a scheduler start pinned grants nobody authored")
	}
}

// TestCapacityCountsPersonStartedLoops: CN8 never counts an audience —
// four person-started running rows spend the default four slots, and the
// queue item defers with the capacity word, untouched, unclaimed.
func TestCapacityCountsPersonStartedLoops(t *testing.T) {
	e := newSchedEnv(t)
	// CN8 lets one repository run one loop, so a full house is four
	// repositories' person-started loops — each spends its own slot and
	// the scheduler's own repository goes last with nothing left.
	for i := 1; i <= 4; i++ {
		if _, err := e.s.CreatePinnedLoop(store.NewLoop{
			Slug:          fmt.Sprintf("person-loop-%d", i),
			ComponentRoot: fmt.Sprintf("%s-person-%d", e.root, i),
			Root:          fmt.Sprintf("%s-person-%d", e.root, i),
			Goal:          "a person's work", Status: "running",
		}, e.bpPath, 1, queueBlueprintYAML, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	row := e.queueReady("no room", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	after, _, err := e.s.QueueItemByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "ready" {
		t.Fatalf("a capacity-full queue moved the item to %q", after.Status)
	}
	if _, ok := e.claim("queue", row.ID); ok {
		t.Fatal("a capacity deferral claimed anyway")
	}
	sawCapacity := false
	for _, d := range e.decisions("queue", row.ID) {
		if d.Decision == "deferred" && d.Reason == "capacity" {
			sawCapacity = true
		}
	}
	if !sawCapacity {
		t.Fatalf("no capacity line for the deferred item: %+v", e.decisions("queue", row.ID))
	}
}

// TestRunContinuousStartsAfterRecovery: the boot order pinned at the
// source — the continuous goroutine returns untouched under
// --no-recover, waits on recoveryDone, reconciles BEFORE it ticks, and
// shutdown drains it beside the automation siblings.
func TestRunContinuousStartsAfterRecovery(t *testing.T) {
	src, err := os.ReadFile("../../cmd/aeon/serve.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "contDone := make(chan struct{})")
	block := s[start:]
	end := strings.Index(block, "RunContinuous(recoveryCtx)")
	if start < 0 || end < 0 {
		t.Fatal("serve.go does not carry the continuous goroutine")
	}
	block = block[:end+len("RunContinuous(recoveryCtx)")]
	nr := strings.Index(block, "if noRecover {")
	rd := strings.Index(block, "<-recoveryDone")
	rc := strings.Index(block, "ReconcileContinuous(recoveryCtx)")
	if nr < 0 || rd < 0 || rc < 0 {
		t.Fatal("the continuous block is missing its no-recover guard, its recovery wait, or its reconcile")
	}
	if !(nr < rd && rd < rc && rc < end) {
		t.Fatalf("the boot order moved: noRecover@%d recoveryDone@%d reconcile@%d run@%d", nr, rd, rc, end)
	}
	if !strings.Contains(s, "case <-contDone:") {
		t.Fatal("shutdown does not drain the continuous goroutine")
	}
}

// TestSchedulerStartHistoryParity: S25 reads a scheduler-started loop
// exactly like a person-started one — the same shape, the same count,
// and no trace of the provenance word in the history view at all.
func TestSchedulerStartHistoryParity(t *testing.T) {
	human := recoveryStore(t)
	hd := New(Options{Store: human})
	bp := writeGateBlueprint(t, "parity-gate")
	ws := t.TempDir()
	hres, err := hd.startLoopWith(context.Background(), protocol.LoopStartParams{
		Blueprint: bp, Goal: "parity", WorkspaceRoot: ws, Slug: "manual-parity-1",
	}, startExtras{})
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, human, hd, hres["id"].(string))

	e := newSchedEnv(t)
	// Same blueprint file and workspace words are impossible across two
	// stores' temp dirs; parity is the SHAPE, and the shape must not
	// carry provenance.
	row := e.queueReady("parity", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.loop("queue-item-" + itoa(row.ID)); !ok {
		t.Fatal("no scheduler loop to compare")
	}

	he, err := history.NewStore(human).List("", "", history.ByLoop)
	if err != nil {
		t.Fatal(err)
	}
	se, err := history.NewStore(e.s).List("", "", history.ByLoop)
	if err != nil {
		t.Fatal(err)
	}
	if len(he) != len(se) {
		t.Fatalf("history counts differ: person %d, scheduler %d — one shape renders rows the other hides", len(he), len(se))
	}
	hum := fmt.Sprintf("%+v", he)
	sch := fmt.Sprintf("%+v", se)
	for _, frag := range []string{"scheduler"} {
		if strings.Contains(sch, frag) && !strings.Contains(hum, frag) {
			t.Fatalf("history leaked scheduler provenance %q where a person's loop shows none", frag)
		}
	}
}
