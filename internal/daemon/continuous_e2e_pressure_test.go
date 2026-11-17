// continuous_e2e_pressure_test.go — S27 T11 scenarios 3, 4, 12 and 13:
// the scheduler under load and beside a person. Capacity is fair and
// visible (CN9), aging kills starvation (CN11), a person's loop is a
// wall the scheduler respects (CN8), and with the scheduler OFF the
// reactor is T1's goldens byte-for-byte with a worker ticking alongside.
package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// addRoot files another component + governed scheduler root beside the
// env's own, queue-governed the same way. Five of them is the capacity
// fixture; the names keep the component slugs honest.
func (e *e2eEnv) addRoot(name string) string {
	e.t.Helper()
	root := e.t.TempDir()
	if _, err := e.s.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: root}); err != nil {
		e.t.Fatalf("seed %s: %v", name, err)
	}
	if _, err := e.s.UpsertSchedulerRoot(e.user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: e.bpPath, Tech: "go", WithJSON: `{"route":"aeon"}`,
	}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.s.SetComponentQueueSettings(root, "auto", "manual"); err != nil {
		e.t.Fatal(err)
	}
	return root
}

// TestE2ECapacityIsFairAndVisible is scenario 3: five repositories, two
// global slots — exactly two run, the waiting is SAID once each (the
// dedupe), finishing frees the slot for the next in CN11's order, and a
// repository whose weight no longer fits the cap is REFUSED once by
// name, never deferred forever.
func TestE2ECapacityIsFairAndVisible(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxSlotsSetting, "2")
	roots := []string{e.root}
	for _, name := range []string{"cap-b", "cap-c", "cap-d", "cap-fat"} {
		roots = append(roots, e.addRoot(name))
	}
	var items []store.QueueItemRow
	for i, root := range roots {
		items = append(items, e.queueReadyOn(root, "one unit of work", nil))
		_ = i
	}
	// cap-fat's component carries weight 3 — it needs three of two slots.
	comp, err := e.s.ComponentForRoot(roots[4])
	if err != nil {
		t.Fatalf("the fat component row is unreadable: %v", err)
	}
	if _, err := e.s.SetComponentMaxParallelAgents(comp.ID, 3); err != nil {
		t.Fatal(err)
	}

	// (Observation note, recorded: the fake lane finishes a loop the
	// moment the engine turns it, so "exactly two are RUNNING" cannot be
	// read after the fact without racing the runner — the arithmetic
	// itself is owned by the pure admission tests. What this end-to-end
	// proof reads is the LEDGER: after the first tick exactly two units
	// have started claims, and the other three have not.)
	e.tick()
	started := map[int64]bool{}
	for _, it := range items {
		if cl, ok := e.claim("queue", it.ID); ok && cl.Outcome == "started" {
			started[it.ID] = true
		}
	}
	if len(started) != 2 {
		t.Fatalf("two slots started %d units on the first tick, want exactly 2: %v", len(started), started)
	}

	// The waiting repositories read capacity — through the SAME preview
	// door, and the ledger says each sentence ONCE however many ticks
	// pass (the dedupe is scenario 3's own clause).
	e.tick()
	e.tick()
	for _, it := range items {
		_, started := e.loopOn(it.ComponentRoot, expectedSlug("queue", it.ID))
		if started {
			continue
		}
		kind := "capacity"
		if it.ComponentRoot == roots[4] {
			kind = "weight-exceeds-cap"
			if n := e.countDecisions("queue", it.ID, "refused"); n != 1 {
				t.Fatalf("the fat repository was refused %d times, want the ONE refusal", n)
			}
		}
		if n := e.countDecisions("queue", it.ID, "deferred"); n > 1 {
			t.Fatalf("the idle repository deferred %d times (%s), want the deduped one", n, kind)
		}
	}

	// Fairness as rotation: the first pair finishes, the next pair starts,
	// and the pair is NEW — every ordinary repository gets its turn.
	firstIDs := map[int64]bool{}
	for _, it := range items {
		if started[it.ID] {
			firstIDs[it.ID] = true
			e.waitLoopSettledOn(it.ComponentRoot, expectedSlug("queue", it.ID))
		}
	}
	e.tick()
	e.tick()
	second := map[int64]bool{}
	for _, it := range items {
		if started[it.ID] {
			continue
		}
		if cl, ok := e.claim("queue", it.ID); ok && cl.Outcome == "started" {
			second[it.ID] = true
		}
	}
	if len(second) == 0 {
		t.Fatal("freed slots started nothing")
	}
	// The fat repository NEVER starts: its weight does not fit, and the
	// refusal is final until a person changes the settings.
	if _, ok := e.loopOn(roots[4], expectedSlug("queue", items[4].ID)); ok {
		t.Fatal("the weight-exceeds-cap repository started anyway")
	}
	// Drain to full service: the ordinary repositories all finish.
	for _, it := range items[:4] {
		cl, ok := e.claim("queue", it.ID)
		if !ok || cl.Outcome != "started" {
			continue // not yet its turn — the loop below will wait for it
		}
		e.waitLoopSettledOn(it.ComponentRoot, expectedSlug("queue", it.ID))
		e.tick()
	}
	for i := 0; i < 6; i++ {
		e.tick()
	}
	for _, it := range items[:4] {
		if got := e.waitQueueStatus(it.ID, "done"); got != "done" {
			t.Fatalf("queue item %d reads %q after the drain, want done", it.ID, got)
		}
	}
}

// runningLoops lists the slugs of every OCCUPYING loop across the roots.
func (e *e2eEnv) runningLoops(roots []string) []string {
	e.t.Helper()
	var out []string
	for _, root := range roots {
		occ, err := e.s.OccupyingLoops(root)
		if err != nil {
			e.t.Fatal(err)
		}
		for _, o := range occ {
			if o.Status == "running" {
				out = append(out, o.Slug)
			}
		}
	}
	return out
}

// TestE2ENoStarvation is scenario 4: an assumed-priority repository is
// beaten on every tick by a stream of fresh known-priority work on
// another — until aging (one fake hour here) cuts in, and it starts long
// before tick 200. The fake clock walks a minute per tick; the plan's
// aged-first ordering does the rest.
func TestE2ENoStarvation(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxSlotsSetting, "1")
	e.setSetting(schedulerAgingSetting, "1")      // one fake hour
	e.setSetting(schedulerMaxStartsSetting, "60") // the declared ceiling
	// CN12's rate gate must not starve the stream itself — a throttled
	// minute would hand the slot to the patient item through a gap and
	// prove the wrong law (measured: the 6-per-hour default did exactly
	// that). The fake clock therefore walks TWO minutes per tick: at
	// most ~30 starts fit any fake hour, under the declared 60 ceiling,
	// and the runaway gate keeps its own scenario (8).
	high := e.roadmapReadyPrio("known and endless", roadmap.PriorityKnown)
	lowRoot := e.addRoot("patient")
	lowSaved := e.root
	e.root = lowRoot
	low := e.roadmapReadyPrio("assumed and patient", roadmap.PriorityAssumed)
	e.root = lowSaved

	startedAt := -1
	for i := 0; i < 200; i++ {
		// The stream stays fresh: another ready known item stands in
		// BEFORE the next tick, so the slot is never briefly empty — a
		// gap in the stream would let the patient item through on an
		// empty repository, which is not the starvation being tested.
		if e.roadmapStatus(high) == "done" {
			high = e.roadmapReadyPrio("known and endless again", roadmap.PriorityKnown)
		}
		at := e2eNow.Add(time.Duration(i) * 2 * time.Minute)
		e.tickAt(at)
		if _, ok := e.loopOn(lowRoot, expectedSlug("roadmap", low)); ok {
			startedAt = i
			break
		}
		if cl, ok := e.claim("roadmap", high); ok && cl.Outcome == "started" {
			e.waitLoopSettledOn(e.root, expectedSlug("roadmap", high))
			// Wait for the LANDINGS before the next pre-tick append: an
			// item still reading `running` while its loop is done would
			// leave the repository with no candidate for one tick — a
			// gap in the stream, not the starvation under test.
			e.waitRoadmapStatus(high, "done")
		}
	}
	if startedAt < 0 {
		t.Fatal("the low-priority repository starved across 200 ticks — CN11's aging did not cut in")
	}
	// 30 ticks of two fake minutes make one fake hour — no start before
	// the patient item has actually aged.
	if startedAt < 29 {
		all, err := e.s.Decisions(store.DecisionQuery{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range all {
			t.Logf("ROW: %s %d %s at %s reason=%s", r.SubjectKind, r.SubjectID, r.Decision, r.At, r.Reason)
		}
		t.Fatalf("the low-priority repository started at tick %d, before one fake hour of aging had accrued", startedAt)
	}
	cl := e.waitClaimStarted("roadmap", low)
	if cl.Outcome != "started" {
		t.Fatalf("the aged item's claim reads %q", cl.Outcome)
	}
	if n := e.countDecisions("roadmap", low, "deferred"); n > 1 {
		t.Fatalf("the patient item deferred %d times, want the deduped one", n)
	}
}

// roadmapReadyPrio is roadmapReady with an explicit priority state — the
// known-vs-assumed contest is CN11's whole ordering story.
func (e *e2eEnv) roadmapReadyPrio(title string, prio roadmap.PriorityState) int64 {
	e.t.Helper()
	r := roadmap.New(e.s)
	it, ref := r.Create(e.root, roadmap.NewItem{Title: title, Priority: prio, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		e.t.Fatalf("create %q: %s", title, ref.Reason)
	}
	if _, ref := r.SetArtifacts(it.ID, "/briefs/x.md", "/plans/x.md"); ref != nil {
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

// TestE2EPersonLoopsAreRespected is scenario 12: a person's running loop
// on an enabled repository is a wall — the scheduler reads repo-occupied
// (naming the person's loop), starts nothing, and resumes exactly when
// the person's loop is finished. The person's loop also spends a global
// slot: the repository's occupancy is the unit CN8 holds.
func TestE2EPersonLoopsAreRespected(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	item := e.queueReady("waits politely")

	res, err := e.d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: e.bpPath, Goal: "the person's own", WorkspaceRoot: e.root, Route: e2eRouteID,
	})
	if err != nil {
		t.Fatalf("the person could not start a loop: %v", err)
	}
	personSlug, _ := res.(map[string]any)["slug"].(string)
	if personSlug == "" {
		t.Fatalf("the person's start answered no slug: %#v", res)
	}

	// The scheduler yields — the word is repo-occupied and the detail
	// names the person's loop, not a silent skip.
	e.tick()
	if _, ok := e.loop(expectedSlug("queue", item.ID)); ok {
		t.Fatal("the scheduler started while a person's loop was running")
	}
	view := e.preview()
	if v, why := repoReasonOf(t, view); v != "defer" || why != string(continuous.ReasonRepoOccupied) {
		t.Fatalf("the scheduler reads %s/%s beside the person's loop, want defer/repo-occupied", v, why)
	}

	// When the person's loop finishes, the scheduler resumes.
	e.waitLoopSettled(personSlug)
	e.tick()
	e.waitClaimStarted("queue", item.ID)
}

// TestE2EDisabledPathByteIdentical is scenario 13: with the scheduler
// OFF — the root never filed, and master switched off — the reactor's
// behaviour equals T1's goldens EXACTLY, worker ticking throughout. This
// is the proof the whole slice leaves the disabled path untouched: the
// scheduler's own machinery runs and changes nothing.
func TestE2EDisabledPathByteIdentical(t *testing.T) {
	t.Parallel()

	// (a) The root is not governed: the worker sees no repository at all.
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	w := f.d.continuousWorker()
	w.Tick(context.Background(), e2eNow)
	first := f.appendReady(t, "shipped thing", nil)
	second := f.appendReady(t, "next thing", no())
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	w.Tick(context.Background(), e2eNow.Add(time.Minute))
	f.materialize(t, "loop-one", "done", "")
	// Stronger than the T1 golden's own await on purpose: the stamp of
	// the second item's loop slug lands a beat after its `running`
	// transition, and this drive interleaves worker ticks — the golden
	// compares against the QUIETED state, so the await waits for it.
	// (Never weaker: the same end state, read precisely.)
	await(t, "the auto advance started the next item", func() bool {
		row := f.itemByID(t, second.ID)
		return row.Status == "running" && row.LoopSlug != nil && *row.LoopSlug == "loop-1"
	})
	w.Tick(context.Background(), e2eNow.Add(2*time.Minute))
	cnCheck(t, "queue_auto_done", f.snapshot(t))

	// (b) Master OFF with the root fully filed: same drive, same bytes.
	g := newReactorFixture(t)
	goldenBP := writeGateBlueprint(t, "golden-bp")
	if _, err := g.s.SetComponentQueueSettings(g.root, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.s.UpsertSchedulerRoot(store.SchedulerActor{Kind: "user", ID: "test"}, store.SchedulerRootRow{
		ComponentRoot: g.root, Enabled: true, Blueprint: goldenBP, Tech: "go",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.s.SetSchedulerState(store.SchedulerActor{Kind: "user", ID: "test"}, false, false, ""); err != nil {
		t.Fatal(err)
	}
	w2 := g.d.continuousWorker()
	w2.Tick(context.Background(), e2eNow)
	b1 := g.appendReady(t, "shipped thing", nil)
	b2 := g.appendReady(t, "next thing", no())
	g.seedLoop(t, "loop-one")
	if _, err := g.s.SetQueueItemLoop(b1.ID, g.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.s.TransitionQueueItem(b1.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	w2.Tick(context.Background(), e2eNow.Add(time.Minute))
	g.materialize(t, "loop-one", "done", "")
	await(t, "the auto advance started the next item (master off)", func() bool {
		row := g.itemByID(t, b2.ID)
		return row.Status == "running" && row.LoopSlug != nil && *row.LoopSlug == "loop-1"
	})
	w2.Tick(context.Background(), e2eNow.Add(2*time.Minute))
	cnCheck(t, "queue_auto_done", g.snapshot(t))
}
