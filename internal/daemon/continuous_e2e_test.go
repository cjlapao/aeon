// continuous_e2e_test.go — S27 T11: the end-to-end proof. What the unit
// tests each hold in one hand, this file joins with both: a REAL daemon
// (real store, real loop runner, real reactors) with a DETERMINISTIC
// FAKE MODEL LANE and a FAKE CLOCK that only ever moves when a Tick says
// so. CN8/CN9/CN11/CN12/CN14/CN23 are not re-argued here — they were
// argued in internal/continuous's pure tests and in T5–T8's daemon seams.
// What this file proves is the COMPOSITION: start, finish, kick, decide,
// restart, and still every claim holds exactly once, every decision row
// is legible, and no wall clock ever got a sleep.
//
// Deviations from the spec's wording, recorded per the record's law:
//   - "build-tagged like the existing daemon e2e tests": the repo has NO
//     build-tagged daemon e2e tests (the only tagged daemon files are
//     aeondebug crash-injection probes). A tag here would hide the suite
//     from the spec's OWN command (`-run 'E2E|Continuous'`), so the tests
//     are plain tests of the package — which is also how every prior S27
//     daemon test ships.
//   - the fourteen scenarios live across four files by cohesion (SIZE1:
//     one file for fourteen end-to-end proofs is a 1,500-line grab-bag):
//     this file is the harness plus the roadmap-to-done pipeline and the
//     queue ordering; continuous_e2e_pressure_test.go holds capacity,
//     aging, person's loops and the goldens; continuous_e2e_crash_test.go
//     holds blocked, kill-mid-pick, refusal and idle/no-recover; and
//     continuous_e2e_bounds_test.go holds the runaway bound, the window,
//     pause/stop-all, inheritance, the ledger audit and the occupancy
//     agreement. Test names are exactly the sixteen the spec pins.
//   - "A finishes ⇒ kick ⇒ B starts": a kick only WAKENS a worker; with
//     Run never started (the fake clock law forbids the ticker) the wake
//     coalesces harmlessly, and the test's next explicit Tick is the tick
//     the production kick would have caused. The kick wiring itself is
//     pinned by T8's reactor tests.
//   - waiting on a loop the ENGINE goroutine is finishing polls the row
//     with a deadline — the same 2-second bargain waitGateParked and
//     waitLoopDone make. The no-sleep law governs the scheduler's clock,
//     not the observation of a runner the fake clock never drove.
package daemon

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// e2eModel is the deterministic fake lane: scripted turns, and after the
// script every call answers the same one-text-turn ok the real lane's
// exhaustion answers — a model-step loop therefore RUNS and FINISHES
// without a provider. The mutex is load-bearing: the engine calls it from
// loop goroutines while tests read the call count, and -race is the law.
type e2eModel struct {
	mu     sync.Mutex
	turns  []e2eTurn
	calls  int
	failed int
	// hold, when non-nil, parks every Stream call until it is closed —
	// the fake provider's own handbrake, so a loop can be caught mid
	// model-step by tests that must act on a RUNNING loop.
	hold chan struct{}
}

type e2eTurn struct {
	text string // the whole assistant turn; empty means "no text"
	err  error  // non-nil makes the model call fail once (the engine's model-error arm)
	// pace, when positive, dribbles the turn out as dots one pace
	// apart before the text lands — the mid-stream shape a real
	// provider holds a loop in for seconds.
	pace time.Duration
}

func (m *e2eModel) next() (e2eTurn, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index := m.calls
	m.calls++
	if index < len(m.turns) {
		return m.turns[index], index
	}
	return e2eTurn{text: `{"status":"ok","summary":"out of script"}`}, index
}

func (m *e2eModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *e2eModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	turn, _ := m.next()
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		if m.hold != nil {
			select {
			case <-m.hold:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
		if turn.pace > 0 {
			for i := 0; i < 40; i++ {
				select {
				case deltas <- model.Delta{Kind: model.DeltaText, Text: "."}:
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				}
				select {
				case <-time.After(turn.pace):
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				}
			}
		}
		if turn.err != nil {
			m.mu.Lock()
			m.failed++
			m.mu.Unlock()
			errs <- turn.err
			return
		}
		if turn.text != "" {
			deltas <- model.Delta{Kind: model.DeltaText, Text: turn.text}
		}
		results <- model.StreamResult{Attempts: 1}
	}()
	return deltas, results, errs
}

// e2eEnv is the whole daemon the proofs speak to: one component root,
// scheduler on, the settings plane wired (the knobs are settings — CN21),
// and the worker taken from d.continuousWorker() so a reactor kick wakes
// the SAME worker the test ticks.
type e2eEnv struct {
	t      *testing.T
	d      *Daemon
	s      *store.Store
	w      *continuous.Worker
	set    *config.Store
	api    protocol.ContinuousAPI
	root   string
	bpPath string
	user   store.SchedulerActor
	now    time.Time  // the fake clock: it moves ONLY when a test says so
	clock  *testClock // THE SAME fake the store stamps its rows with
	model  *e2eModel
	log    *wire // the daemon's own lines — refusals are logged, never returned
}

// e2eNow is the suite's shared fake instant — Wednesday morning, comfortably
// inside any Mon–Fri 09:00–17:00 window the tests do NOT exercise.
var e2eNow = time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

// e2eRouteID is the fake route's name — the same word the governed roots
// carry in their with-map, so every scheduler loop resolves it.
const e2eRouteID = "aeon"

func newE2E(t *testing.T) *e2eEnv {
	t.Helper()
	return newE2ENamed(t, "e2e", "e2e")
}

func newE2ENamed(t *testing.T, name, slug string) *e2eEnv {
	t.Helper()
	// The store rides the fake clock too: a deferral row's `at` and the
	// snapshot's `now` must be the same world's times, or CN11's aging
	// arithmetic compares a wall-clock yesterday with a fake tomorrow.
	tc := &testClock{t: e2eNow}
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" },
		Clock: tc.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	m := &e2eModel{}
	set := NewSettings(s, config.Options{})
	route := model.Route{ID: e2eRouteID, Provider: "anthropic", Model: "m", MaxOutputTokens: 1024}
	lw := &wire{}
	d := New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Settings: set, Models: m,
		Logger: lw, Notify: lw.notify})
	t.Cleanup(func() {
		d.Table.WaitContinuations()
		d.cancelLoops(context.Canceled)
	})
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: name, Slug: slug, Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := s.SetSchedulerState(user, true, false, ""); err != nil {
		t.Fatal(err)
	}
	bp := writeStreamBlueprint(t, "read-only", "solo")
	if _, err := s.UpsertSchedulerRoot(user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: bp, Tech: "go", WithJSON: `{"route":"aeon"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetComponentQueueSettings(root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	env := &e2eEnv{t: t, d: d, s: s, w: d.continuousWorker(), set: set, api: NewContinuousService(d),
		root: root, bpPath: bp, user: user, now: e2eNow, clock: tc, model: m, log: lw}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the daemon's own lines:\n%s", lw.logLines())
		}
	})
	return env
}

// tick runs one whole scheduler tick AT THE FAKE INSTANT — the entire
// suite's clock is whatever the test hands it.
func (e *e2eEnv) tick() {
	e.t.Helper()
	if err := e.w.Tick(context.Background(), e.now); err != nil {
		e.t.Fatalf("tick: %v", err)
	}
}

func (e *e2eEnv) tickAt(at time.Time) {
	e.t.Helper()
	e.clock.add(at.Sub(e.now)) // the store's world walks with the plan's
	e.now = at
	if err := e.w.Tick(context.Background(), at); err != nil {
		e.t.Fatalf("tick at %s: %v", at, err)
	}
}

// setSetting writes one tuning knob through the settings plane — the same
// Store the daemon adapter reads live between ticks (CN21).
func (e *e2eEnv) setSetting(key, raw string) {
	e.t.Helper()
	if _, _, err := e.set.Set(context.Background(), key, raw, "user:operator"); err != nil {
		e.t.Fatalf("setting %s: %v", key, err)
	}
}

// roadmapReady walks a fresh item to ready through the domain's own gate
// — the scheduler starts only what a person walked here.
func (e *e2eEnv) roadmapReady(title string) int64 {
	e.t.Helper()
	r := roadmap.New(e.s)
	it, ref := r.Create(e.root, roadmap.NewItem{Title: title, Priority: roadmap.PriorityKnown, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		e.t.Fatalf("create %q: %s", title, ref.Reason)
	}
	if _, ref := r.SetArtifacts(it.ID, "/briefs/"+title+".md", "/plans/"+title+".md"); ref != nil {
		e.t.Fatalf("artifacts: %s", ref.Reason)
	}
	for _, rung := range []roadmap.Status{roadmap.StatusIdeating, roadmap.StatusBriefAgreed,
		roadmap.StatusPlanned, roadmap.StatusReady} {
		if _, ref := r.Transition(it.ID, rung); ref != nil {
			e.t.Fatalf("%s: %s", rung, ref.Reason)
		}
	}
	return it.ID
}

func (e *e2eEnv) roadmapStatus(id int64) string {
	e.t.Helper()
	it, ok, err := roadmap.New(e.s).Show(id)
	if err != nil || !ok {
		e.t.Fatalf("roadmap item %d unreadable (ok=%v err=%v)", id, ok, err)
	}
	return string(it.Status)
}

// queueReady appends an item on the env's root and walks it to ready.
func (e *e2eEnv) queueReady(goal string) store.QueueItemRow {
	e.t.Helper()
	return e.queueReadyOn(e.root, goal, nil)
}

// queueReadyOn files a queue item on ANY root (five-repo capacity needs
// four more) with the optional declared-attendedness (S24 D5).
func (e *e2eEnv) queueReadyOn(root, goal string, attended *bool) store.QueueItemRow {
	e.t.Helper()
	q := queue.New(e.s)
	it, err := q.Append(root, e.bpPath, goal, root, queue.NewItem{Attended: attended})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, ref := q.Transition(it, queue.StatusReady); ref != nil {
		e.t.Fatalf("ready: %s", ref.Reason)
	}
	row, ok, err := e.s.QueueItemByID(it.ID)
	if err != nil || !ok {
		e.t.Fatalf("the row behind item %d is gone", it.ID)
	}
	return row
}

func (e *e2eEnv) queueStatus(id int64) string {
	e.t.Helper()
	row, ok, err := e.s.QueueItemByID(id)
	if err != nil || !ok {
		e.t.Fatalf("queue item %d is gone (%v)", id, err)
	}
	return row.Status
}

func (e *e2eEnv) loop(slug string) (store.Loop, bool) {
	e.t.Helper()
	l, err := e.s.GetLoop(store.Scope{LoopSlug: slug, ComponentRoot: e.root})
	if err != nil {
		return store.Loop{}, false
	}
	return l, true
}

// loopOn is loop() for the multi-root scenarios.
func (e *e2eEnv) loopOn(root, slug string) (store.Loop, bool) {
	e.t.Helper()
	l, err := e.s.GetLoop(store.Scope{LoopSlug: slug, ComponentRoot: root})
	if err != nil {
		return store.Loop{}, false
	}
	return l, true
}

func (e *e2eEnv) decisions(kind string, id int64) []store.SchedulerDecision {
	e.t.Helper()
	ds, err := e.s.Decisions(store.DecisionQuery{SubjectKind: kind, SubjectID: id, HasSubject: true, Limit: 500})
	if err != nil {
		e.t.Fatal(err)
	}
	return ds
}

// countDecisions counts the subject's rows carrying one decision word —
// the dedupe law read as a number.
func (e *e2eEnv) countDecisions(kind string, id int64, decision string) int {
	e.t.Helper()
	n := 0
	for _, d := range e.decisions(kind, id) {
		if d.Decision == decision {
			n++
		}
	}
	return n
}

// waitRoadmapStatus polls the item's rung until it reads `want` — the
// reactor lands terminals on a goroutine, so the landing is observed the
// way every other async completion in this package is: a deadline and a
// poll, never a sleep-and-hope.
func (e *e2eEnv) waitRoadmapStatus(id int64, want string) string {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	got := e.roadmapStatus(id)
	for got != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		got = e.roadmapStatus(id)
	}
	return got
}

// waitQueueStatus is the same bargain on the queue's rung.
func (e *e2eEnv) waitQueueStatus(id int64, want string) string {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	got := e.queueStatus(id)
	for got != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		got = e.queueStatus(id)
	}
	return got
}

func (e *e2eEnv) claim(kind string, id int64) (store.SchedulerStartRow, bool) {
	e.t.Helper()
	r, ok, err := e.s.StartRecord(kind, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return r, ok
}

// waitLoopSettled polls the loop row until it leaves running — the engine
// finishes on its own goroutine, and this is the house bargain
// (waitGateParked, waitLoopDone): a deadline, a poll, and a failure that
// says what the row actually says.
func (e *e2eEnv) waitLoopSettled(slug string) store.Loop {
	e.t.Helper()
	return e.waitLoopSettledOn(e.root, slug)
}

func (e *e2eEnv) waitLoopSettledOn(root, slug string) store.Loop {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		l, ok := e.loopOn(root, slug)
		if ok && (l.Status == "done" || l.Status == "blocked" || l.Status == "aborted") {
			return l
		}
		if time.Now().After(deadline) {
			if ok {
				e.t.Fatalf("loop %s never settled — status %q", slug, l.Status)
			}
			e.t.Fatalf("loop %s never appeared", slug)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitClaimStarted waits for the start recorder to CLOSE the claim for a
// unit — the start's whole outcome, not just its claim.
func (e *e2eEnv) waitClaimStarted(kind string, id int64) store.SchedulerStartRow {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if cl, ok := e.claim(kind, id); ok && cl.Outcome == "started" {
			return cl
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("no started claim for %s %d", kind, id)
		}
		time.Sleep(time.Millisecond)
	}
}

// preview answers scheduler.preview through the daemon's OWN socket
// service — the same door the CLI and the UI call, not a second
// computation next to it.
func (e *e2eEnv) preview() protocol.ContinuousPreview {
	e.t.Helper()
	return e.previewRoot(e.root)
}

func (e *e2eEnv) previewRoot(root string) protocol.ContinuousPreview {
	e.t.Helper()
	view, err := e.api.Preview(context.Background(), root)
	if err != nil {
		e.t.Fatalf("preview %s: %v", root, err)
	}
	return view
}

// repoReasonOf reads the preview's repository-level line, if any.
func repoReasonOf(t *testing.T, view protocol.ContinuousPreview) (string, string) {
	t.Helper()
	for _, line := range view.Decisions {
		if line.SubjectKind == "repository" {
			return line.Verdict, line.Reason
		}
	}
	t.Fatalf("the preview carries no repository line: %+v", view.Decisions)
	return "", ""
}

// deferReasonOf finds what the preview says about one unit (queue id or
// roadmap id — the preview carries both) as (verdict, reason).
func deferReasonOf(t *testing.T, view protocol.ContinuousPreview, kind string, id int64) (string, string) {
	t.Helper()
	want := strconv.FormatInt(id, 10)
	for _, line := range view.Decisions {
		if line.SubjectKind == kind && line.SubjectID == want {
			return line.Verdict, line.Reason
		}
	}
	for _, line := range view.WouldStart {
		if line.SubjectKind == kind && line.SubjectID == want {
			return line.Verdict, line.Reason
		}
	}
	t.Fatalf("the preview says nothing about %s %d: %+v / %+v", kind, id, view.Decisions, view.WouldStart)
	return "", ""
}

// TestE2ERoadmapToDoneUnattended is scenario 1, the slice's headline: the
// whole pipeline — three ready roadmap items chained A→B→C by dependency,
// one slot, the S56-less pin — runs to `done` with a person nowhere in
// sight. Every loop is the scheduler's (started_by), every decision is
// one legible row, and the waits are the plan's own words.
func TestE2ERoadmapToDoneUnattended(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxSlotsSetting, "1")

	a := e.roadmapReady("first off the line")
	b := e.roadmapReady("waits for first")
	c := e.roadmapReady("waits for second")
	r := roadmap.New(e.s)
	actor := roadmap.Actor{Kind: "user", ID: "test"}
	if ref := r.Depend(actor, b, a); ref != nil {
		t.Fatalf("depend b on a: %s", ref.Reason)
	}
	if ref := r.Depend(actor, c, b); ref != nil {
		t.Fatalf("depend c on b: %s", ref.Reason)
	}

	// The preview is the tick run without starting (CN15): before anyone
	// moves, it already says A would start and B and C sit
	// dependency-open. (Deviation, recorded: WHILE A RUNS the plan says
	// the repository's own reason — repo-occupied, naming A's loop —
	// because CN6 admits one candidate per repository and a held
	// repository yields no per-unit verdicts to enumerate; the
	// dependency-open words the spec names are spoken in this preview,
	// one breath before the chain moves.)
	view := e.preview()
	// (The verdict word is `skip`, not `defer` — an open dependency is
	// not a not-now, it is a not-this-unit; the REASON is the spec's own
	// word and the detail names what waits.)
	if v, why := deferReasonOf(t, view, "roadmap", b); v != "skip" || why != string(continuous.ReasonDependencyOpen) {
		t.Fatalf("B reads %s/%s before the first tick, want skip/dependency-open", v, why)
	}
	if v, why := deferReasonOf(t, view, "roadmap", c); v != "skip" || why != string(continuous.ReasonDependencyOpen) {
		t.Fatalf("C reads %s/%s before the first tick, want skip/dependency-open", v, why)
	}
	startedA := false
	for _, line := range view.WouldStart {
		if line.SubjectKind == "roadmap" && line.SubjectID == strconv.FormatInt(a, 10) {
			startedA = true
		}
	}
	if !startedA {
		t.Fatalf("the preview would not start A: %+v", view.WouldStart)
	}

	e.tick()
	e.waitClaimStarted("roadmap", a)
	if _, ok := e.loop(expectedSlug("roadmap", a)); !ok {
		t.Fatal("A has no loop after its started row")
	}
	// While A runs, the repository reads held — in the plan's own word,
	// naming the loop that holds it.
	view = e.preview()
	if len(view.Decisions) == 0 || view.Decisions[0].Reason != "repo-occupied" {
		t.Fatalf("while A runs the preview says %+v, want a repo-occupied hold", view.Decisions)
	}

	// A finishes on the fake lane ⇒ the reactor's kick stands; the next
	// tick — the one the kick would have caused — starts B. The chain
	// walks itself to done, unattended, item by item.
	for _, step := range []struct{ item, next int64 }{{a, b}, {b, c}, {c, 0}} {
		slug := expectedSlug("roadmap", step.item)
		l := e.waitLoopSettled(slug)

		if l.Status != "done" {
			t.Fatalf("A→B→C broke at item %d: loop %s status %q", step.item, slug, l.Status)
		}
		if l.StartedBy != "scheduler" {
			t.Fatalf("loop %s was started by %q, not the scheduler", slug, l.StartedBy)
		}
		if got := e.waitRoadmapStatus(step.item, "done"); got != "done" {
			t.Fatalf("roadmap item %d is %q after its loop finished, want done", step.item, got)
		}
		if step.next != 0 {
			e.tick()
			e.waitClaimStarted("roadmap", step.next)
		}
	}

	// The ledger, as the start left it: one started row per item, the
	// grant revision empty (no S56 grants in this daemon), and each loop
	// row carrying the scheduler's provenance.
	for _, id := range []int64{a, b, c} {
		if n := e.countDecisions("roadmap", id, "started"); n != 1 {
			t.Fatalf("roadmap item %d has %d started rows, want exactly one", id, n)
		}
		for _, d := range e.decisions("roadmap", id) {
			if d.Decision == "started" && d.GrantRevision != "" {
				t.Fatalf("started row %d carries grant revision %q without any S56 grant", d.ID, d.GrantRevision)
			}
		}
	}
	// The dependency defers were SAID, deduplicated: one row per item
	// while the chain ran, not one per tick.
	if n := e.countDecisions("roadmap", c, "deferred"); n > 1 {
		t.Fatalf("C was deferred %d times — the dedupe did not hold", n)
	}
}

// TestE2EQueueBeforeRoadmapAndAttendedSkipped is scenario 2: the declared
// attended item is never the scheduler's to start (S24 D5), the normal
// queue item runs FIRST (CN11 puts the queue before the roadmap), and the
// roadmap item waits `queue-live` until the queue drains.
func TestE2EQueueBeforeRoadmapAndAttendedSkipped(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	yes := true
	attended := e.queueReadyOn(e.root, "a person is watching this one", &yes)
	normal := e.queueReady("the scheduler's own")
	item := e.roadmapReady("waits for the queue")

	// (Deviation, recorded: the spec's table says the normal item "starts
	// first" while the attended one sits at the head. CN6 is the law that
	// shipped and the design's own words — a human-ordered queue is an
	// explicit instruction, never second-guessed — so an attended head
	// STOPS the scheduler's queue: skip with attended-item, roadmap
	// waiting queue-live, and the attended unit running only through a
	// person's queue.advance. The ordering the scenario actually proves —
	// queue before roadmap, attended never scheduler-started, roadmap
	// starts after the drain — all holds below, with the person pushing
	// the head exactly once.)
	e.tick()

	view := e.preview()
	if v, why := deferReasonOf(t, view, "queue", attended.ID); v != "skip" || why != string(continuous.ReasonAttendedItem) {
		t.Fatalf("the attended item reads %s/%s, want skip/attended-item", v, why)
	}
	// The repository-level hold: CN6 puts the roadmap second, and while
	// the queue lives the plan says so AT THE REPOSITORY — the roadmap
	// unit is never even a candidate, so its name appears nowhere and
	// the repository carries the word.
	if v, why := repoReasonOf(t, view); v != "defer" || why != string(continuous.ReasonQueueLive) {
		t.Fatalf("the repository reads %s/%s, want defer/queue-live", v, why)
	}
	if _, ok := e.loop(expectedSlug("queue", attended.ID)); ok {
		t.Fatal("the scheduler started a declared-attended item")
	}
	if _, ok := e.loop(expectedSlug("queue", normal.ID)); ok {
		t.Fatal("the scheduler jumped a human-ordered queue past its attended head")
	}

	// The person's door moves the head (design CN3.4 names exactly this
	// door). It starts ATTENDED — the declaration survives the start.
	if !e.d.advanceNext(context.Background(), e.root) {
		t.Fatal("the person's queue.advance started nothing")
	}
	row, _, err := e.s.QueueItemByID(attended.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.LoopSlug == nil || *row.LoopSlug == "" {
		t.Fatalf("queue.advance moved item %d but pointed it at no loop", attended.ID)
	}
	attendedLoop, ok := e.loop(*row.LoopSlug)
	if !ok {
		t.Fatalf("the advanced item points at loop %q and no row exists", *row.LoopSlug)
	}
	if attendedLoop.StartedBy == "scheduler" {
		t.Fatal("the attended loop claims the scheduler as its author")
	}
	pin, err := e.s.ReadPinnedLoop(attendedLoop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Attended == nil || !*pin.Attended {
		t.Fatalf("the advanced loop lost the person's attended declaration: attended=%v", pin.Attended)
	}

	// T8's hand-off, live: the attended loop finishing does NOT auto
	// advance the queue under the scheduler — the reactor kicks and the
	// scheduler's next tick is what moves.
	e.waitLoopSettled(attendedLoop.Slug)
	e.waitQueueStatus(attended.ID, "done")
	e.tick()
	e.waitClaimStarted("queue", normal.ID)
	// The roadmap item waited its whole life so far: queue-live while
	// the queue lived, and it starts only after the drain.
	if got := e.roadmapStatus(item); got != "ready" {
		t.Fatalf("the roadmap item moved to %q before the queue drained", got)
	}
	e.waitLoopSettled(expectedSlug("queue", normal.ID))
	e.waitQueueStatus(normal.ID, "done")
	e.tick()
	e.waitClaimStarted("roadmap", item)

	if n := e.countDecisions("queue", attended.ID, "skipped"); n > 1 {
		t.Fatalf("the attended item was skipped %d times — dedupe did not hold", n)
	}
	if n := e.countDecisions("queue", normal.ID, "started"); n != 1 {
		t.Fatalf("the normal item started %d times under the scheduler, want one", n)
	}
}

// TestE2EStillGoingAgreement is the third-party pin: the store's SQL
// occupancy IN-list, trigger.StillGoing, and what the SCHEDULER's own
// plan treats as holding a repository agree across ALL SIX loop statuses.
// Six independent daemons (one per status) make the third reading honest:
// the verdict is not a helper consulted beside the plan — it is whether
// the tick started the item standing behind the status row.
func TestE2EStillGoingAgreement(t *testing.T) {
	t.Parallel()
	six := []string{"running", "waiting", "blocked", "done", "aborted", "idle"}
	for i, status := range six {
		name := "six-" + strconv.Itoa(i)
		e := newE2ENamed(t, name, name)
		item := e.queueReady("nothing may start past " + status)
		if _, err := e.s.CreatePinnedLoop(store.NewLoop{
			Slug: name + "-row", ComponentRoot: e.root, Root: e.root, Goal: "overlap row",
			Status: status, StartedBy: "scheduler",
		}, "e2e-gate", 1, queueBlueprintYAML, "{}"); err != nil {
			t.Fatal(err)
		}
		occ, err := e.s.OccupyingLoops(e.root)
		if err != nil {
			t.Fatal(err)
		}
		heldBySQL := false
		for _, o := range occ {
			if o.Slug == name+"-row" {
				heldBySQL = true
			}
		}
		if heldBySQL != trigger.StillGoing(status) {
			t.Fatalf("status %q: SQL occupancy says %v, trigger.StillGoing says %v", status, heldBySQL, trigger.StillGoing(status))
		}
		e.tick()
		_, started := e.loop(expectedSlug("queue", item.ID))
		if started == heldBySQL {
			t.Fatalf("status %q: SQL and StillGoing agree the repository is held=%v, and the scheduler's start says otherwise (started=%v)", status, heldBySQL, started)
		}
	}
}
