// continuous_e2e_crash_test.go — S27 T11 scenarios 5, 6, 7 and 14: the
// failure half of the scheduler. A blocked loop holds its repository for
// as long as a person takes; a kill mid-pick starts exactly once; a
// refusal is final across ticks AND restarts; an idle loop rests without
// occupying, and --no-recover leaves the scheduler itself unrecovered.
package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// waitDecision polls until the subject's ledger carries at least one row
// with the decision word and answers how many there are. The reactors
// land as goroutines — the ledger row is read when it has landed, never
// guessed at.
func (e *e2eEnv) waitDecision(kind string, id int64, decision string) int {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := e.countDecisions(kind, id, decision)
		if n >= 1 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(time.Millisecond)
	}
}

// blockTheLoop writes the blocked status the engine would have written
// and emits the loop.blocked event with the reason STRUCT — the only
// payload shape that carries a kind (T10's law).
func (e *e2eEnv) blockTheLoop(root, slug, kind, detail string) {
	e.t.Helper()
	l, ok := e.loopOn(root, slug)
	if !ok {
		e.t.Fatalf("the loop %s does not exist to block", slug)
	}
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: root}, "blocked"); err != nil {
		e.t.Fatal(err)
	}
	e.d.publishLoop(protocol.KindLoopBlocked, map[string]any{
		"loopId": l.ID,
		"reason": &loop.BlockedReason{Kind: kind, Detail: detail},
	})
}

// TestE2EBlockedHoldsTheRepositoryForever is scenario 5: a
// scheduler-started loop blocks with `model-error` and holds its
// repository against TEN THOUSAND ticks — no new start there, the other
// repositories keep running, the needs-you row is written ONCE through
// every repetition of the block event — until a person ends the loop,
// and only then does the next unit start.
func TestE2EBlockedHoldsTheRepositoryForever(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxSlotsSetting, "2") // the held loop spends a slot; the other repository needs the second one
	other := e.addRoot("other-repo")
	otherItem := e.queueReadyOn(other, "unaffected work", nil)
	held := e.roadmapReady("the held unit")
	follow := e.roadmapReady("the next unit")

	e.tick()
	e.waitClaimStarted("roadmap", held)
	slug := expectedSlug("roadmap", held)
	e.blockTheLoop(e.root, slug, "model-error", "the route died mid-step")

	// needs-you, once. And again — the event repeating must not repeat
	// the sentence (the dedupe is the scenario's own clause).
	if n := e.waitDecision("roadmap", held, "needs-you"); n != 1 {
		t.Fatalf("the needs-you line was written %d times, want exactly 1", n)
	}
	e.blockTheLoop(e.root, slug, "model-error", "the route died mid-step")
	e.blockTheLoop(e.root, slug, "model-error", "the route died mid-step")
	time.Sleep(50 * time.Millisecond) // let any async re-filing land before counting
	if n := e.countDecisions("roadmap", held, "needs-you"); n != 1 {
		t.Fatalf("the needs-you line grew to %d rows with the block repeating, want the deduped one", n)
	}

	// The unaffected repository runs its own unit despite the hold.
	e.waitClaimStarted("queue", otherItem.ID)

	// 10,000 ticks, one fake minute apart. Nothing starts in the held
	// repository; the hold itself is the only story there.
	for batch := 0; batch < 50; batch++ {
		for i := 0; i < 200; i++ {
			e.tickAt(e.now.Add(time.Minute))
		}
	}
	if _, ok := e.loopOn(e.root, expectedSlug("roadmap", follow)); ok {
		t.Fatal("the held repository started the next unit while a loop sat blocked — CN8 is dead")
	}
	if n := e.countDecisions("roadmap", follow, "started"); n != 0 {
		t.Fatalf("ten thousand ticks produced %d starts in the held repository", n)
	}
	if n := e.countDecisions("roadmap", held, "needs-you"); n != 1 {
		t.Fatalf("the storm grew the needs-you row to %d, want the deduped one", n)
	}

	// The person ends the blocked loop (the S55 abort lane's act, done
	// directly here) — the repository frees and the next unit starts.
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: e.root}, "aborted"); err != nil {
		t.Fatal(err)
	}
	e.tick()
	e.waitClaimStarted("roadmap", follow)
}

// TestE2EKillMidPickStartsExactlyOnce is scenario 6: the daemon dies
// after the claim and before the loop exists — Reconcile starts it ONCE,
// with the deterministic slug, and a second restart starts nothing; the
// daemon that dies after the loop but before the stamps gets its stamps
// landed and no second loop.
func TestE2EKillMidPickStartsExactlyOnce(t *testing.T) {
	t.Parallel()

	// Phase 1: kill at "claimed" — the claim row is all the crash left.
	a := newE2ENamed(t, "kill-claimed", "kill-claimed")
	itemA := a.queueReady("claimed then killed")
	var arm struct {
		sync.Mutex
		on bool
	}
	arm.on = true
	continuousCrashHook = func(d *Daemon, stage string) bool {
		arm.Lock()
		defer arm.Unlock()
		return arm.on && d == a.d && stage == "claimed"
	}
	t.Cleanup(func() { continuousCrashHook = nil })
	a.tick()
	if cl, ok := a.claim("queue", itemA.ID); !ok || cl.Outcome != "" {
		t.Fatalf("the claimed-then-killed start left outcome %q, want the open (empty) claim", cl.Outcome)
	}
	if _, ok := a.loopOn(a.root, expectedSlug("queue", itemA.ID)); ok {
		t.Fatal("the kill at `claimed` let a loop row exist")
	}
	arm.Lock()
	arm.on = false
	arm.Unlock()

	if err := a.d.ReconcileContinuous(context.Background()); err != nil {
		t.Fatal(err)
	}
	cl := a.waitClaimStarted("queue", itemA.ID)
	if cl.Outcome != "started" {
		t.Fatalf("Reconcile's outcome reads %q", cl.Outcome)
	}
	if cl.LoopSlug != expectedSlug("queue", itemA.ID) {
		t.Fatalf("Reconcile started under %q, want the deterministic slug", cl.LoopSlug)
	}
	a.waitLoopSettledOn(a.root, expectedSlug("queue", itemA.ID))
	if err := a.d.ReconcileContinuous(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := countLoopRows(t, a, expectedSlug("queue", itemA.ID)); n != 1 {
		t.Fatalf("the second restart grew the loop to %d rows, want exactly 1", n)
	}
	if n := a.countDecisions("queue", itemA.ID, "started"); n != 1 {
		t.Fatalf("the started rows read %d across the crash and Reconcile, want the single row the claim names", n)
	}

	// Phase 2: kill at "started" — the loop exists, the stamps don't.
	b := newE2ENamed(t, "kill-started", "kill-started")
	itemB := b.queueReady("started then killed")
	barm := struct {
		sync.Mutex
		on bool
	}{on: true}
	continuousCrashHook = func(d *Daemon, stage string) bool {
		barm.Lock()
		defer barm.Unlock()
		return barm.on && d == b.d && stage == "started"
	}
	t.Cleanup(func() { continuousCrashHook = nil })
	b.tick()
	if _, ok := b.loopOn(b.root, expectedSlug("queue", itemB.ID)); !ok {
		t.Fatal("the kill at `started` happened before the loop existed")
	}
	if cl, ok := b.claim("queue", itemB.ID); !ok || cl.Outcome != "" {
		t.Fatalf("the kill at `started` left outcome %q, want the open claim", cl.Outcome)
	}
	barm.Lock()
	barm.on = false
	barm.Unlock()

	if err := b.d.ReconcileContinuous(context.Background()); err != nil {
		t.Fatal(err)
	}
	cl = b.waitClaimStarted("queue", itemB.ID)
	if cl.LoopSlug != expectedSlug("queue", itemB.ID) {
		t.Fatalf("the stamps landed on %q, want the deterministic slug", cl.LoopSlug)
	}
	if n := countLoopRows(t, b, expectedSlug("queue", itemB.ID)); n != 1 {
		t.Fatalf("Reconcile started a second loop for the claimed-and-started crash (%d rows), want the one existing loop", n)
	}
}

// countLoopRows counts the loop rows carrying the slug in the env's root.
func countLoopRows(t *testing.T, e *e2eEnv, slug string) int {
	t.Helper()
	n := 0
	occ, err := e.s.OccupyingLoops(e.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range occ {
		if o.Slug == slug {
			n++
		}
	}
	if _, ok := e.loopOn(e.root, slug); ok && n == 0 {
		n = 1 // settled (non-occupying) loops read through the row, not the occupancy view
	}
	return n
}

// TestE2ERefusalIsFinal is scenario 7: a blueprint removed after enable
// refuses `template-invalid` — the item is untouched, needs-you shows
// it, and neither ten more ticks nor a whole daemon restart ever retries
// the start.
func TestE2ERefusalIsFinal(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	item := e.roadmapReady("doomed by its blueprint")
	if err := os.Remove(e.bpPath); err != nil {
		t.Fatalf("the blueprint could not be removed: %v", err)
	}

	e.tick()
	if n := e.waitDecision("roadmap", item, "refused"); n != 1 {
		t.Fatalf("the refusal rows read %d after the first tick, want exactly 1", n)
	}
	if n := e.countDecisions("roadmap", item, "needs-you"); n != 1 {
		t.Fatalf("the needs-you line for the refusal reads %d, want exactly 1 (CN13)", n)
	}
	for i := 0; i < 10; i++ {
		e.tick()
	}
	if _, ok := e.loopOn(e.root, expectedSlug("roadmap", item)); ok {
		t.Fatal("a refused pick started a loop anyway")
	}
	if got := e.roadmapStatus(item); got != "ready" {
		t.Fatalf("the refused item reads %q, want untouched `ready`", got)
	}

	// The restart: a second daemon over the same store, ticking fresh.
	lw := &wire{}
	route := model.Route{ID: e2eRouteID, Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	d2 := New(Options{Store: e.s, Routes: protocol.NewStaticRoutes(route),
		Settings: NewSettings(e.s, config.Options{}), Logger: lw, Notify: lw.notify})
	t.Cleanup(func() {
		d2.Table.WaitContinuations()
		d2.cancelLoops(context.Canceled)
	})
	if err := d2.continuousWorker().Tick(context.Background(), e.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := e.countDecisions("roadmap", item, "refused"); n != 1 {
		t.Fatalf("the restart grew the refusal to %d rows, want never-retried", n)
	}
	if n := e.countDecisions("roadmap", item, "needs-you"); n != 1 {
		t.Fatalf("the restart grew the needs-you to %d rows, want never-retried", n)
	}
	if _, ok := e.loopOn(e.root, expectedSlug("roadmap", item)); ok {
		t.Fatal("the restart started what the refusal already ended")
	}
}

// TestE2EIdleLoopsAndNoRecover is scenario 14: an idle-resting loop
// occupies nothing and is never (re)started by the scheduler, and
// --no-recover leaves the scheduler itself unrecovered — pinned at the
// serve.go gate, the serve_automation precedent's own method.
func TestE2EIdleLoopsAndNoRecover(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxSlotsSetting, "1") // the whole machine, so an idler spending a slot would be visible
	witness := e.addRoot("idle-witness")
	witnessItem := e.queueReadyOn(witness, "starts because idle is not occupancy", nil)

	// The idler: its queue item runs and its loop rests at `idle` — the
	// `after: converse` shape: the conversation is over, nobody is
	// working, nothing is holding the repository or a slot.
	idleItem := e.queueReady("the conversed unit")
	e.tick()
	e.waitClaimStarted("queue", idleItem.ID)
	idleSlug := expectedSlug("queue", idleItem.ID)
	e.waitLoopSettledOn(e.root, idleSlug)
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: idleSlug, ComponentRoot: e.root}, "idle"); err != nil {
		t.Fatal(err)
	}

	// Idle is not occupancy: with the single slot, the OTHER repository's
	// unit starts while the idler rests.
	e.tick()
	e.waitClaimStarted("queue", witnessItem.ID)

	// And the scheduler never (re)starts the idler: its item is running
	// (never re-picked), and its loop is still exactly as idle as it was.
	for i := 0; i < 3; i++ {
		e.tick()
	}
	// The honest re-start witness is the ledger: the item's `started`
	// row was written once by the first start — a re-start writes
	// another. (The claim row itself is the SAME row either way, so it
	// proves nothing after the fact.)
	if n := e.countDecisions("queue", idleItem.ID, "started"); n != 1 {
		t.Fatalf("the idle-resting item carries %d started rows, want the original one", n)
	}
	l, ok := e.loopOn(e.root, idleSlug)
	if !ok || l.Status != "idle" {
		t.Fatalf("the idler reads %q after the ticks, want untouched idle", l.Status)
	}

	// --no-recover: the scheduler's boot lives INSIDE the recovery gate —
	// skipping recovery skips the scheduler. Pinned at the source, the
	// way serve_automation_test.go pins its own dispatcher gate (the
	// placement deviation is recorded: this pin lives with the e2e suite
	// that owns the law).
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", "serve.go"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	iGate := strings.Index(s, "if noRecover {\n\t\t\treturn\n\t\t}\n\t\tselect {\n\t\tcase <-recoveryDone:")
	if iGate < 0 {
		t.Fatal("--no-recover must skip the continuous scheduler too — the gate that returns before recoveryDone owns RunContinuous")
	}
	iRun := strings.Index(s, "d.RunContinuous(recoveryCtx)")
	if iRun < 0 || iRun < iGate {
		t.Fatal("RunContinuous is not gated behind --no-recover")
	}
}

// TestContinuousCrashHookIsTestOnly proves the kill-window hook never
// fires in production: the declaration file is the package's ONLY
// non-test mention of continuousCrashHook, and no non-test file of the
// module assigns it.
func TestContinuousCrashHookIsTestOnly(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	declared := 0
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			tt := strings.TrimSpace(line)
			if !strings.Contains(tt, "continuousCrashHook") || strings.HasPrefix(tt, "//") {
				continue
			}
			// Every non-test mention must live in continuous_start.go —
			// the declaration and its wrapper `continuousCrashed` — and
			// nothing may ASSIGN the hook outside a test: an assignment
			// in shipped code would arm the kill window in production.
			if name != "continuous_start.go" {
				t.Fatalf("non-test file %s mentions the crash hook: %s", name, tt)
			}
			if strings.HasPrefix(tt, "var continuousCrashHook") {
				declared++
				continue
			}
			if strings.Contains(tt, "continuousCrashHook ") && strings.Contains(tt, "=") &&
				!strings.Contains(tt, "!=") {
				t.Fatalf("the hook is assigned outside the declaration: %s", tt)
			}
		}
	}
	if declared != 1 {
		t.Fatalf("the crash hook reads %d var declarations, want the single one", declared)
	}
}
