package daemon

// continuous_test.go — S27 T5's behaviour: the claim protocol (claim
// before start, one attempt, reconcile), the authored facts of a
// scheduler start (unattended, provenance), and the pick-time refusals.
// The fixture is the golden harness (recoveryStore + New + the gate
// blueprint that parks a loop without any model), so a "running" loop
// here is as real as the daemon's own.

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

type schedEnv struct {
	t      *testing.T
	d      *Daemon
	s      *store.Store
	a      *continuousAdapter
	w      *continuous.Worker
	root   string
	bpPath string
}

func newSchedEnv(t *testing.T) *schedEnv {
	t.Helper()
	s := recoveryStore(t)
	d := New(Options{Store: s})
	bp := writeGateBlueprint(t, "sched-gate")
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "sched", Slug: "sched", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := s.SetSchedulerState(user, true, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertSchedulerRoot(user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: bp, Tech: "go", WithJSON: `{"route":"aeon"}`,
	}); err != nil {
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

// tick runs one whole tick at the store's now.
func (e *schedEnv) tick() error {
	return e.w.Tick(context.Background(), time.Now().UTC())
}

// queueReady appends an item and moves it to the claimed-FROM state.
func (e *schedEnv) queueReady(goal, blueprint string) store.QueueItemRow {
	e.t.Helper()
	q := queue.New(e.s)
	it, err := q.Append(e.root, blueprint, goal, e.root, queue.NewItem{})
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

// roadmapReady creates an item, lands artifacts and a known priority, and
// walks it to ready through the domain's own gate.
func (e *schedEnv) roadmapReady(title string) int64 {
	e.t.Helper()
	r := roadmap.New(e.s)
	it, ref := r.Create(e.root, roadmap.NewItem{Title: title, Priority: roadmap.PriorityKnown, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		e.t.Fatalf("create: %s", ref.Reason)
	}
	if _, ref := r.SetArtifacts(it.ID, "/briefs/"+title+".md", "/plans/"+title+".md"); ref != nil {
		e.t.Fatalf("artifacts: %s", ref.Reason)
	}
	// Walk the ladder the domain owns — idea to ready is four authored
	// edges, and the scheduler starts only what a person walked here.
	for _, rung := range []roadmap.Status{roadmap.StatusIdeating, roadmap.StatusBriefAgreed,
		roadmap.StatusPlanned, roadmap.StatusReady} {
		if _, ref := r.Transition(it.ID, rung); ref != nil {
			e.t.Fatalf("%s: %s", rung, ref.Reason)
		}
	}
	return it.ID
}

func (e *schedEnv) loop(slug string) (store.Loop, bool) {
	e.t.Helper()
	l, err := e.s.GetLoop(store.Scope{LoopSlug: slug, ComponentRoot: e.root})
	if err != nil {
		return store.Loop{}, false
	}
	return l, true
}

func (e *schedEnv) claim(kind string, id int64) (store.SchedulerStartRow, bool) {
	e.t.Helper()
	r, ok, err := e.s.StartRecord(kind, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return r, ok
}

func (e *schedEnv) decisions(kind string, id int64) []store.SchedulerDecision {
	e.t.Helper()
	ds, err := e.s.Decisions(store.DecisionQuery{SubjectKind: kind, SubjectID: id, HasSubject: true, Limit: 100})
	if err != nil {
		e.t.Fatal(err)
	}
	return ds
}

// TestSchedulerStartIsUnattended: a scheduler start AUTHORITIES
// unattendedness (D1's law from trigger_fire): the loop row reads
// attended=false, and nothing on the start params could have said
// otherwise — the queue row carries no attended column read.
func TestSchedulerStartIsUnattended(t *testing.T) {
	e := newSchedEnv(t)
	row := e.queueReady("walk the queue", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	l, ok := e.loop("queue-item-" + itoa(row.ID))
	if !ok {
		t.Fatal("the tick started no loop")
	}
	pin, err := e.s.ReadPinnedLoop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Attended == nil || *pin.Attended {
		t.Fatalf("the loop is not authored unattended: attended=%v", pin.Attended)
	}
}

// TestStartedBySchedulerReachesTheRun: CN17's provenance is the loop's
// own rows — started_by names the scheduler and trigger_id names the
// DECISION (the audit's join key), and the loop_run carries the widened
// kind.
func TestStartedBySchedulerReachesTheRun(t *testing.T) {
	e := newSchedEnv(t)
	row := e.queueReady("provenance", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	l, ok := e.loop("queue-item-" + itoa(row.ID))
	if !ok {
		t.Fatal("no loop")
	}
	if l.StartedBy != "scheduler" {
		t.Fatalf("started_by = %q, want scheduler", l.StartedBy)
	}
	cl, ok := e.claim("queue", row.ID)
	if !ok {
		t.Fatal("no claim")
	}
	if l.TriggerID != itoa(cl.DecisionID) {
		t.Fatalf("trigger_id = %q, want the decision id %d", l.TriggerID, cl.DecisionID)
	}
	run, err := e.s.CurrentRun(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.StartedByKind != "scheduler" {
		t.Fatalf("loop_run started_by_kind = %q, want scheduler", run.StartedByKind)
	}
	for _, d := range e.decisions("queue", row.ID) {
		if d.Decision == "started" {
			if d.ActorKind != "scheduler" || d.ActorID != "continuous" {
				t.Fatalf("the decision's actor is %s/%s", d.ActorKind, d.ActorID)
			}
			return
		}
	}
	t.Fatal("no started decision was written")
}

// TestClaimBeforeStart: a death between claim and start leaves the open
// claim as the ONLY trace — and Reconcile, holding that claim, starts the
// item exactly once. No second claim, no doubled loop.
func TestClaimBeforeStart(t *testing.T) {
	e := newSchedEnv(t)
	row := e.queueReady("die mid start", e.bpPath)
	q := queue.New(e.s)
	id, err := e.s.AppendDecision(store.SchedulerDecision{
		ComponentRoot: e.root, SubjectKind: "queue", SubjectID: row.ID,
		Decision: "started", Reason: "", Summary: "a decision whose tick died",
		ActorKind: "scheduler", ActorID: "continuous",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := e.s.ClaimStart("queue", row.ID, e.root, id, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if _, ref := q.Transition(queueItemFromRow(row), queue.StatusRunning); ref != nil {
		t.Fatal(ref.Reason)
	}
	// The process died HERE: nothing was started, nothing was stamped.
	if err := e.w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	l, ok := e.loop("queue-item-" + itoa(row.ID))
	if !ok {
		t.Fatal("reconcile started nothing")
	}
	if l.Status != "running" && l.Status != "waiting" {
		t.Fatalf("the reconciled loop reads %q", l.Status)
	}
	cl, _ := e.claim("queue", row.ID)
	if cl.Outcome != "started" {
		t.Fatalf("the claim closed %q, want started", cl.Outcome)
	}
	// A second reconcile must find nothing open and start nothing more.
	if err := e.w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.loop("queue-item-" + itoa(row.ID) + "-1"); ok {
		t.Fatal("a second loop appeared")
	}
}

// TestReconcileStampsAnExistingLoop: the loop EXISTS (the crash landed
// after the door) and only the records lag — reconcile stamps, points,
// and closes as started. It never starts a second loop.
func TestReconcileStampsAnExistingLoop(t *testing.T) {
	e := newSchedEnv(t)
	row := e.queueReady("crash after door", e.bpPath)
	q := queue.New(e.s)
	if _, ref := q.Transition(queueItemFromRow(row), queue.StatusRunning); ref != nil {
		t.Fatal(ref.Reason)
	}
	res, err := e.d.startLoopWith(context.Background(), protocol.LoopStartParams{
		Blueprint: row.Blueprint, Goal: row.Goal, WorkspaceRoot: e.root,
		Slug: "queue-item-" + itoa(row.ID), Attended: attendedByNobody(),
	}, startExtras{StartedBy: "scheduler", TriggerID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, e.s, e.d, res["id"].(string))
	id, err := e.s.AppendDecision(store.SchedulerDecision{
		ComponentRoot: e.root, SubjectKind: "queue", SubjectID: row.ID,
		Decision: "started", Summary: "died before the stamp",
		ActorKind: "scheduler", ActorID: "continuous",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.ClaimStart("queue", row.ID, e.root, id, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	cl, _ := e.claim("queue", row.ID)
	if cl.Outcome != "started" || cl.LoopSlug == "" {
		t.Fatalf("the claim reads outcome=%q slug=%q — reconcile left it", cl.Outcome, cl.LoopSlug)
	}
	after, _, err := e.s.QueueItemByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LoopSlug == nil || *after.LoopSlug == "" {
		t.Fatal("the queue item never got its pointer")
	}
	// The loop that exists is the ONE the reconcile found, not a new one.
	l, ok := e.loop("queue-item-" + itoa(row.ID))
	if !ok || l.ID != res["id"].(string) {
		t.Fatal("reconcile started a different loop")
	}
}

// TestRefusedStartIsNeverRetried: brief §8's finality — the queue item
// blocked with its question, the claim refused, and NEITHER another tick
// NOR a reconcile dares a second attempt.
func TestRefusedStartIsNeverRetried(t *testing.T) {
	e := newSchedEnv(t)
	garbage := e.root + "/gone.yaml"
	if err := os.WriteFile(garbage, []byte("id: gone\nnot: a blueprint: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	row := e.queueReady("refused forever", garbage)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	cl, ok := e.claim("queue", row.ID)
	if !ok || cl.Outcome != "refused" {
		t.Fatalf("the claim reads %+v", cl)
	}
	after, _, err := e.s.QueueItemByID(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "blocked" {
		t.Fatalf("the item reads %q, want blocked (blockUndauntedStart's aftermath)", after.Status)
	}
	refused := 0
	for _, d := range e.decisions("queue", row.ID) {
		if d.Decision == "refused" {
			refused++
		}
	}
	if refused != 1 {
		t.Fatalf("%d refused lines, want exactly 1", refused)
	}
	// More ticks and a restart's reconcile change nothing.
	for i := 0; i < 3; i++ {
		if err := e.tick(); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.w.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.loop("queue-item-" + itoa(row.ID)); ok {
		t.Fatal("a refused start produced a loop on retry")
	}
	if cl2, _ := e.claim("queue", row.ID); cl2.Outcome != "refused" {
		t.Fatal("the refusal moved")
	}
}

// TestRoadmapItemRunsThroughTheTemplate: CN5's shape end to end — the
// scheduler_root's blueprint/tech/with, the goal is the title, the link
// pair and both artifact paths ride `with`, and RecordLoop stamps.
func TestRoadmapItemRunsThroughTheTemplate(t *testing.T) {
	e := newSchedEnv(t)
	id := e.roadmapReady("Ship the scheduler")
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	l, ok := e.loop("roadmap-item-" + itoa(id))
	if !ok {
		t.Fatal("the tick started no roadmap loop")
	}
	pin, err := e.s.ReadPinnedLoop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	rj := string(pin.RuntimeJSON)
	for _, want := range []string{`roadmap_item`, `materialization`, `brief_path`, `plan_path`, `"route":"aeon"`, `"tech":"go"`} {
		if !strings.Contains(rj, want) {
			t.Fatalf("the runtime with map is missing %s: %s", want, rj)
		}
	}
	if pin.Loop.Goal != "Ship the scheduler" {
		t.Fatalf("goal = %q, want the item's title", pin.Loop.Goal)
	}
	it, found, err := roadmap.New(e.s).Show(id)
	if err != nil || !found {
		t.Fatal("the item vanished")
	}
	if it.LoopSlug != l.Slug {
		t.Fatalf("RecordLoop stamped %q, want %q", it.LoopSlug, l.Slug)
	}
}

// TestTemplateInvalidAtPick: the blueprint the root was enabled with is
// gone by tick time — the roadmap item is refused at the pick,
// untouched, and this is the needs-you class of news, not a stall.
func TestTemplateInvalidAtPick(t *testing.T) {
	e := newSchedEnv(t)
	id := e.roadmapReady("Never started")
	if err := os.WriteFile(e.bpPath, []byte("id: broken\nlanes: [not, a, lane\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	it, _, err := roadmap.New(e.s).Show(id)
	if err != nil {
		t.Fatal(err)
	}
	if string(it.Status) != "ready" {
		t.Fatalf("the moved item reads %q — a pick-time refusal leaves it ready (untouched)", it.Status)
	}
	// The snapshot carries the refusal as the plan's step-3 row (T4
	// truth table): the decision log holds one refused line for it.
	sawTemplateInvalid := false
	for _, d := range e.decisions("roadmap", id) {
		if d.Decision == "refused" && d.Reason == "template-invalid" {
			sawTemplateInvalid = true
		}
	}
	if !sawTemplateInvalid {
		// The plan itself refuses BEFORE the claim (step 3), so the
		// refusal line is the RECORD the worker wrote for the refuse
		// verdict; the item never moved and no claim exists.
		for _, d := range e.decisions("roadmap", id) {
			if d.Reason == "template-invalid" {
				sawTemplateInvalid = true
			}
		}
	}
	if !sawTemplateInvalid {
		t.Fatalf("no template-invalid line for roadmap %d: %+v", id, e.decisions("roadmap", id))
	}
	if _, ok := e.claim("roadmap", id); ok {
		t.Fatal("a pick-time refusal claimed anyway")
	}
}

// TestNeedsYouIsDedupedAndVerbatim: the door's sentence reaches the
// ledger word for word, and repeated arrivals of the same news write ONE
// row (the closed claim plus the last-word check make the second call a
// no-op).
func TestNeedsYouIsDedupedAndVerbatim(t *testing.T) {
	e := newSchedEnv(t)
	id := e.roadmapReady("Needs a person")
	sentence := "the repository's work template no longer loads: blueprint gone"
	e.a.refuseRoadmapStart("roadmap", id, e.root, sentence)
	e.a.refuseRoadmapStart("roadmap", id, e.root, sentence)
	var needs []store.SchedulerDecision
	for _, d := range e.decisions("roadmap", id) {
		if d.Decision == "needs-you" {
			needs = append(needs, d)
		}
	}
	if len(needs) != 1 {
		t.Fatalf("%d needs-you rows, want exactly 1", len(needs))
	}
	if needs[0].Summary != sentence {
		t.Fatalf("the summary was paraphrased: %q", needs[0].Summary)
	}
}
