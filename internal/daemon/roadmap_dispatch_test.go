package daemon

// S26 T3 + T4 — the dispatch and the reactor's gates. The harness follows
// queue_reactor_test.go exactly: the store is real, the queue and roadmap
// doors are the ONLY writers (never raw SQL), the emission path is the
// daemon's REAL publishLoop (so the wiring at the emission point is under
// test), and only the two daemon doors are spies — loop.start and the
// step door — the same post-wire swap every protocol test uses.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

type roadmapFixture struct {
	d    *Daemon
	s    *store.Store
	rm   *roadmap.Roadmap
	w    *wire
	root string
	// starts receives every StartBlueprintLoop the doors perform.
	starts chan protocol.LoopStartParams
	// ids maps a pinned slug to the loop's real id (publishLoop resolves
	// through the id, the item's pointer holds the slug — queue's rule).
	ids map[string]string
	// slugs remembers what exists, so the spy refuses collisions the way
	// the REAL door does — or the double-dispatch guard under test is
	// fiction.
	slugs map[string]bool
}

// seamAnswer unwraps a dispatch seam's map answer — the seams speak `any`
// (the protocol passthrough rule), the daemon answers maps.
func seamAnswer(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("a dispatch seam answered %T — it answers a map", v)
	}
	return m
}

func newRoadmapFixture(t *testing.T) *roadmapFixture {
	t.Helper()
	// A REAL temporary root: the ideation and planning tests put brief.md
	// and plan.md on disk, because the landing law reads the file, not a
	// promise (queue's fixture could keep a fake string only because it
	// never touches the filesystem).
	return newRoadmapFixtureRooted(t, t.TempDir())
}

// newRoadmapFixtureRooted takes the project root as a parameter — the
// either-branch parity test gives TWO stores the SAME root string (a
// materialization touches no file, so the root is a name there; the
// one-loop-start-door comparison then compares like with like).
func newRoadmapFixtureRooted(t *testing.T, root string) *roadmapFixture {
	t.Helper()
	tc := &testClock{}
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }, Clock: tc.now})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Reactor goroutines outlive the last await() by a heartbeat; let
	// them finish BEFORE the store closes under them (Close nils the
	// handles). onRoadmapTerminal's recover is the safety net — this is
	// the courtesy. Registered after the close, so it runs first.
	t.Cleanup(func() { time.Sleep(150 * time.Millisecond) })
	w := &wire{}
	var settings *config.Store
	if roadmapFixtureSettingsHook != nil {
		settings = roadmapFixtureSettingsHook(s)
	}
	d := New(Options{Store: s, Logger: w, Notify: w.notify, Settings: settings})
	if _, err := s.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	// S50 T2 (RM3): the planning host now reads its bytes by NAME from
	// the blueprint rows, so the fixture applies the seed set exactly as
	// initBlueprints does in `serve`.
	if err := SeedStandardBlueprints(s); err != nil {
		t.Fatalf("seed the standard blueprints: %v", err)
	}
	f := &roadmapFixture{d: d, s: s, rm: roadmap.New(s), w: w, root: root,
		starts: make(chan protocol.LoopStartParams, 16),
		ids:    map[string]string{}, slugs: map[string]bool{}}
	// The daemon's own wiring — initBlueprints fills this in `serve`; the
	// test calls it so the seams under test are the REAL door closures.
	d.initRoadmap()
	d.Table.StartBlueprintLoop = func(ctx context.Context, p protocol.LoopStartParams) (any, error) {
		slug := p.Slug
		if slug == "" {
			slug = fmt.Sprintf("loop-spy-%d", len(f.starts)+1)
		}
		if f.slugs[slug] {
			return nil, fmt.Errorf("loop %q already exists", slug)
		}
		runtime := "{}"
		if len(p.With) > 0 {
			raw, merr := json.Marshal(map[string]any{"with": p.With})
			if merr != nil {
				t.Fatalf("marshal the spy's with map: %v", merr)
			}
			runtime = string(raw)
		}
		l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: p.WorkspaceRoot, Goal: p.Goal, Status: "running", Attended: p.Attended},
			p.Blueprint, 1, queueBlueprintYAML, runtime)
		if err != nil {
			t.Fatalf("spy could not pin %s: %v", slug, err)
		}
		f.slugs[slug] = true
		f.ids[slug] = l.ID
		f.starts <- p
		return map[string]any{"id": slug, "slug": slug, "status": "running"}, nil
	}
	// RunStepForLoop is NOT spied — it is a method, and its own door
	// (handleStep) answers ErrStepUnavailable with no Runner: the seam's
	// background goroutine fails HARMLESSLY in tests, and the landing
	// law is proven separately through landPlanning, whose answers the
	// tests supply directly. queue_sizing_test.go's single-call-site pin
	// is the form for the dispatch itself.
	return f
}

func (f *roadmapFixture) create(t *testing.T, title string) roadmap.Item {
	t.Helper()
	it, ref := f.rm.Create(f.root, roadmap.NewItem{Title: title, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		t.Fatalf("create %q: %s", title, ref.Reason)
	}
	return it
}

func (f *roadmapFixture) show(t *testing.T, id int64) roadmap.Item {
	t.Helper()
	it, ok, err := f.rm.Show(id)
	if err != nil || !ok {
		t.Fatalf("read item %d: ok=%v err=%v", id, ok, err)
	}
	return it
}

func (f *roadmapFixture) mustTransition(t *testing.T, id int64, to roadmap.Status) roadmap.Item {
	t.Helper()
	it, ref := f.rm.Transition(id, to)
	if ref != nil {
		t.Fatalf("transition item %d → %s: %s", id, to, ref.Reason)
	}
	return it
}

func (f *roadmapFixture) loopID(t *testing.T, slug string) string {
	t.Helper()
	id, ok := f.ids[slug]
	if !ok {
		t.Fatalf("no loop %s was ever pinned in this fixture", slug)
	}
	return id
}

// walkToReady lands the two pointers and the priority, and walks the whole
// forward path — the D4 re-check at materialization then has real rows
// behind every condition.
func (f *roadmapFixture) walkToReady(t *testing.T, id int64) roadmap.Item {
	t.Helper()
	f.mustTransition(t, id, roadmap.StatusIdeating)
	f.mustTransition(t, id, roadmap.StatusBriefAgreed)
	if _, ref := f.rm.SetArtifacts(id, filepath.Join(f.root, "brief.md"), filepath.Join(f.root, "plan.md")); ref != nil {
		t.Fatalf("land pointers: %s", ref.Reason)
	}
	f.mustTransition(t, id, roadmap.StatusPlanned)
	if _, ref := f.rm.SetPriority(id, roadmap.PriorityKnown, "P1"); ref != nil {
		t.Fatalf("set priority: %s", ref.Reason)
	}
	return f.mustTransition(t, id, roadmap.StatusReady)
}

// walkToBriefAgreed mirrors what the ideation reactor lands, but through
// the doors (planning tests drive doors, never replay a reactor already
// proved by its own test).
func (f *roadmapFixture) walkToBriefAgreed(t *testing.T, id int64) roadmap.Item {
	t.Helper()
	f.mustTransition(t, id, roadmap.StatusIdeating)
	if _, ref := f.rm.SetArtifacts(id, filepath.Join(f.root, "brief.md"), ""); ref != nil {
		t.Fatalf("land the brief: %s", ref.Reason)
	}
	return f.mustTransition(t, id, roadmap.StatusBriefAgreed)
}

// publish fires the daemon's REAL emission door (publishLoop) — the
// reactors run as goroutines, so assertions wait via await().
func (f *roadmapFixture) publish(t *testing.T, loopID, status, reason string) {
	t.Helper()
	fields := map[string]any{"loopId": loopID, "status": status}
	if reason != "" {
		fields["reason"] = reason
	}
	kind := protocol.KindLoopFinished
	if status == "blocked" {
		kind = protocol.KindLoopBlocked
	}
	f.d.publishLoop(kind, fields)
}

func (f *roadmapFixture) seedLoop(t *testing.T, slug, runtime string) string {
	t.Helper()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"},
		"dev-loop", 1, queueBlueprintYAML, runtime)
	if err != nil {
		t.Fatalf("pin loop %s: %v", slug, err)
	}
	f.ids[slug] = l.ID
	return l.ID
}

func (f *roadmapFixture) awaitStatus(t *testing.T, id int64, want roadmap.Status) roadmap.Item {
	t.Helper()
	var it roadmap.Item
	await(t, fmt.Sprintf("item %d to read %s", id, want), func() bool {
		var ok bool
		var err error
		it, ok, err = f.rm.Show(id)
		return err == nil && ok && it.Status == want
	})
	return it
}

func (f *roadmapFixture) writeFile(t *testing.T, name string) string {
	t.Helper()
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, name)
	if err := os.WriteFile(path, []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- ideation (T3) -------------------------------------------------------------

func TestIdeationClaimsIdeatingAndStartsTheOrdinaryLoop(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "export statements")
	res, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root})
	if err != nil {
		t.Fatalf("ideate: %v", err)
	}
	if started, _ := seamAnswer(t, res)["started"].(bool); !started {
		t.Fatalf("ideate answered %v — it must say the loop started", res)
	}
	var p protocol.LoopStartParams
	select {
	case p = <-f.starts:
	default:
		t.Fatal("ideation started no loop — D1's ordinary loop never appeared at the door")
	}
	if p.Blueprint != "brainstorm" {
		t.Errorf("blueprint = %q — the default ideation lane is the shipped chat-with-tools that already declares brief.md", p.Blueprint)
	}
	if p.Slug != fmt.Sprintf("ideate-item-%d", it.ID) {
		t.Errorf("slug = %q — the deterministic slug is the double-dispatch guard", p.Slug)
	}
	if p.With["roadmap_item"] != fmt.Sprintf("%d", it.ID) || p.With["roadmap_role"] != "ideation" {
		t.Errorf("with = %v — the durable link the reactor reads must name the item and the role", p.With)
	}
	if p.Attended != nil {
		t.Errorf("attended = %v — an absent declaration stays absent (never guessed)", *p.Attended)
	}
	if got := f.show(t, it.ID); got.Status != roadmap.StatusIdeating {
		t.Errorf("item reads %q after ideation — the claim did not land", got.Status)
	}
	// The second dispatch: the item still reads `ideating` (the re-entry
	// path exists for a start that died before its loop row did), and the
	// deterministic slug collides with the loop that EXISTS — one
	// conversation per item, enforced by the loop's own key.
	if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err == nil {
		t.Error("a second ideation dispatched while the loop exists — the slug collision must refuse")
	}
}

func TestIdeationRefusesABlueprintThatDeclaresNoBrief(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "no brief here")
	// two-step is a SHIPPED blueprint whose steps declare no brief.md.
	_, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root, Blueprint: "two-step"})
	if err == nil || !strings.Contains(err.Error(), "brief.md") {
		t.Fatalf("err = %v — ideation must refuse loudly when the engine's produces check would never exist (R3)", err)
	}
	if got := f.show(t, it.ID); got.Status != roadmap.StatusIdea {
		t.Errorf("the refused ideation moved the item to %q — a refusal moves nothing", got.Status)
	}
	select {
	case p := <-f.starts:
		t.Errorf("a refused ideation still started a loop on %q", p.Blueprint)
	default:
	}
}

func TestIdeationTerminalLandsBriefAgreedThroughTheRealEmissionPath(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "export statements")
	if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
		t.Fatalf("ideate: %v", err)
	}
	brief := f.writeFile(t, "brief.md")
	// The engine's missingArtifact already ran by the time `done` fires —
	// the event is the evidence (R3's call-site assertion: THIS event,
	// never a new path).
	f.publish(t, f.loopID(t, fmt.Sprintf("ideate-item-%d", it.ID)), "done", "")
	got := f.awaitStatus(t, it.ID, roadmap.StatusBriefAgreed)
	if got.BriefPath != brief {
		t.Errorf("brief_path = %q — the pointer lands beside the engine-proved file", got.BriefPath)
	}
	// D6: an ideation loop is NOT the item's loop — loop_slug stays unset.
	if got.LoopSlug != "" {
		t.Errorf("loop_slug = %q — an ideation conversation is not a materialization (D6)", got.LoopSlug)
	}
}

func TestBlockedIdeationLeavesTheItemIdeating(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "stalled idea")
	if _, err := f.d.Table.RoadmapIdeate(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapIdeateParams{ID: it.ID, WorkspaceRoot: f.root}); err != nil {
		t.Fatalf("ideate: %v", err)
	}
	f.publish(t, f.loopID(t, fmt.Sprintf("ideate-item-%d", it.ID)), "blocked", "the model refused")
	await(t, "the block to be spoken", func() bool { return strings.Contains(f.w.logLines(), "blocked") })
	if got := f.show(t, it.ID); got.Status != roadmap.StatusIdeating {
		t.Errorf("a blocked ideation moved the item to %q — D6: a block lives in the loop's rows, not the item's status", got.Status)
	}
}

// --- planning (T3) -------------------------------------------------------------

// okStep is the step door's answer for a planning run that worked: the
// planner's own declared handoff shape, verbatim.
func okStep(handoff string) protocol.StepResult {
	return protocol.StepResult{Outcome: "ok", Handoff: json.RawMessage(handoff)}
}

func TestPlanningDispatchPinsTheHostAndRefusesTheSecondDispatch(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "plan me")
	f.walkToBriefAgreed(t, it.ID)
	res, err := f.d.Table.RoadmapPlan(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapPlanParams{ID: it.ID, WorkspaceRoot: f.root})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if host, _ := seamAnswer(t, res)["hostLoop"].(string); host != fmt.Sprintf("plan-item-%d", it.ID) {
		t.Errorf("host = %v — the deterministic host slug carries the double-dispatch guard", seamAnswer(t, res)["hostLoop"])
	}
	// The background run cannot land without a Runner (the ordinary step
	// door answers ErrStepUnavailable) — and neither dispatch nor its
	// failure moves the item: it waits for the evidence.
	if got := f.show(t, it.ID); got.Status != roadmap.StatusBriefAgreed {
		t.Errorf("dispatch moved the item to %q — dispatch is not landing (T3)", got.Status)
	}
	// The second dispatch: the host loop's own key refuses it — this is
	// the guard, not a remembered status.
	if _, err := f.d.Table.RoadmapPlan(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapPlanParams{ID: it.ID, WorkspaceRoot: f.root}); err == nil || !strings.Contains(err.Error(), "already been dispatched") {
		t.Errorf("a second planning run answered %v — the host's key must refuse", err)
	}
}

func TestPlanningLandsPlannedOnlyOnFullEvidence(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "plan me")
	f.walkToBriefAgreed(t, it.ID)
	plan := f.writeFile(t, "plan.md")
	if !f.d.landPlanning(f.show(t, it.ID), f.root, okStep(`{"task_count": 3}`), nil) {
		t.Fatal("full evidence was refused its landing")
	}
	got := f.show(t, it.ID)
	if got.Status != roadmap.StatusPlanned {
		t.Errorf("status = %q", got.Status)
	}
	if got.PlanPath != plan {
		t.Errorf("plan_path = %q — the pointer lands with the status", got.PlanPath)
	}
}

func TestPlanningFailuresNeverAdvance(t *testing.T) {
	cases := []struct {
		name string
		res  protocol.StepResult
		err  error
		plan bool // plan.md exists on disk
	}{
		{"the step door refuses", protocol.StepResult{}, fmt.Errorf("the runtime refused"), true},
		{"the step comes back blocked", protocol.StepResult{Outcome: "blocked", Reason: "discovery missing"}, nil, true},
		{"an ok step carries no handoff", protocol.StepResult{Outcome: "ok"}, nil, true},
		{"the handoff counts nothing", okStep(`{"task_count": 0}`), nil, true},
		{"the handoff is not the planned shape", okStep(`nonsense`), nil, true},
		{"the handoff lies — no plan.md on disk", okStep(`{"task_count": 3}`), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newRoadmapFixture(t)
			it := f.create(t, "plan me")
			f.walkToBriefAgreed(t, it.ID)
			if c.plan {
				f.writeFile(t, "plan.md")
			}
			if f.d.landPlanning(f.show(t, it.ID), f.root, c.res, c.err) {
				t.Fatal("a failed run claimed a landing")
			}
			// The proof of "never silent" is the spoken failure.
			if !strings.Contains(f.w.logLines(), "did not land") {
				t.Error("the failed run was silent — T3 demands it be spoken")
			}
			if got := f.show(t, it.ID); got.Status != roadmap.StatusBriefAgreed {
				t.Errorf("a failed run advanced the item to %q — T3: nothing advances on failure", got.Status)
			}
		})
	}
}

func TestPlanningDispatchReachesTheStepDoorExactlyOnce(t *testing.T) {
	// The ordinary-door pin, in queue_sizing_test.go's form: the planning
	// dispatch reaches a model step exactly once, through
	// d.Table.RunStepForLoop — no side path to the runtime.
	src, err := os.ReadFile("roadmap_dispatch.go")
	if err != nil {
		t.Fatalf("read the dispatch source: %v", err)
	}
	if strings.Count(string(src), "d.Table.RunStepForLoop(") != 1 {
		t.Error("the planning dispatch must reach the step exactly once through d.Table.RunStepForLoop — the ordinary door")
	}
}

func TestProjectPlannerBodyStillDeclaresTaskCount(t *testing.T) {
	// T3 dispatches S13's EXISTING body and reads its declared handoff
	// contract. If the body's front matter stops naming task_count, this
	// test — not the daemon — is the first to say so.
	raw, err := os.ReadFile(filepath.Join("..", "handler", "bodies", "project-planner.md"))
	if err != nil {
		t.Fatalf("read the planner body: %v", err)
	}
	if !strings.Contains(string(raw), "task_count:") {
		t.Error("project-planner.md's handoff no longer declares task_count — the planning landing reads it")
	}
}

// --- materialization (T4) --------------------------------------------------------

func TestMaterializeDirectBranchEndsAtTheOneLoopStartDoor(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "export statements")
	f.walkToReady(t, it.ID)
	attended := true
	res, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "export monthly statements",
		WorkspaceRoot: f.root, Tech: "go", Attended: &attended,
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if started, _ := seamAnswer(t, res)["started"].(bool); !started {
		t.Fatalf("direct branch answered %v", res)
	}
	var p protocol.LoopStartParams
	select {
	case p = <-f.starts:
	default:
		t.Fatal("the direct branch started no loop — R4's one door was never knocked on")
	}
	if p.Blueprint != "dev-loop" || p.Goal != "export monthly statements" || p.WorkspaceRoot != f.root || p.Tech != "go" {
		t.Errorf("params = %+v — the payload arrives at loop.start whole (R4)", p)
	}
	if p.Attended == nil || !*p.Attended {
		t.Error("attended did not ride through unchanged (D2/D5)")
	}
	if p.With["roadmap_item"] != fmt.Sprintf("%d", it.ID) || p.With["roadmap_role"] != "materialization" {
		t.Errorf("with = %v — the durable link must ride the direct branch too", p.With)
	}
	got := f.show(t, it.ID)
	if got.Status != roadmap.StatusRunning || got.LoopSlug != fmt.Sprintf("roadmap-item-%d", it.ID) {
		t.Errorf("item reads %q loop=%q — the claim and the pointer must both land", got.Status, got.LoopSlug)
	}
}

func TestMaterializeQueueBranchAppendsAndStartsNothing(t *testing.T) {
	f := newRoadmapFixture(t)
	// The project HAS a queue: one ordinary row is enough (the probe reads
	// the project's own rows — S24 is in use here).
	q := queue.New(f.s)
	if _, err := q.Append(f.root, "dev-loop", "an ordinary backlog item", f.root, queue.NewItem{}); err != nil {
		t.Fatalf("seed the queue: %v", err)
	}
	it := f.create(t, "export statements")
	f.walkToReady(t, it.ID)
	res, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "export monthly statements", WorkspaceRoot: f.root,
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if queued, _ := seamAnswer(t, res)["queued"].(bool); !queued {
		t.Fatalf("a project WITH a queue went direct: %v — R4 is queue-first", res)
	}
	select {
	case p := <-f.starts:
		t.Errorf("the queue branch started a loop (%q) — CG4: appending starts nothing", p.Blueprint)
	default:
	}
	got := f.show(t, it.ID)
	if got.Status != roadmap.StatusRunning || got.LoopSlug != "" {
		t.Errorf("item reads %q loop=%q — running-queued, honest until S24 starts it (D6)", got.Status, got.LoopSlug)
	}
	// The appended row carries the link S24 will carry onward.
	rows, err := f.s.ListQueueItems(f.root)
	if err != nil || len(rows) != 2 {
		t.Fatalf("queue rows: len=%d err=%v", len(rows), err)
	}
	id, role := queueRoadmapLink(rows[1])
	if id != it.ID || role != "materialization" {
		t.Errorf("queue row with_json link = (%d,%q) — the durable channel must name the item", id, role)
	}
	// The queued duplicate refuses: the link row answers a second call.
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "export monthly statements", WorkspaceRoot: f.root,
	}); err == nil || !strings.Contains(err.Error(), "already queued") {
		t.Errorf("a second materialize of a queued item answered %v — the link row must refuse", err)
	}
}

func TestOneMaterializationPathEitherBranch(t *testing.T) {
	// R4's diffable promise: the loop.start payload is IDENTICAL whoever
	// knocks — the direct branch's call, and the queue branch's row as
	// S24's own advanceNext finally passes it to the same door.
	shared := "/srv/same-project" // one project, two stores — the parity
	// comparison must compare like with like.
	direct := newRoadmapFixtureRooted(t, shared)
	itD := direct.create(t, "same intent")
	direct.walkToReady(t, itD.ID)
	if _, err := direct.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: itD.ID, Blueprint: "dev-loop", Goal: "the same goal", WorkspaceRoot: direct.root, Tech: "go",
	}); err != nil {
		t.Fatalf("direct: %v", err)
	}
	var viaDirect protocol.LoopStartParams
	select {
	case viaDirect = <-direct.starts:
	default:
		t.Fatal("the direct branch never reached the door")
	}

	viaQueue := newRoadmapFixtureRooted(t, shared)
	q := queue.New(viaQueue.s)
	if _, err := q.Append(viaQueue.root, "dev-loop", "an ordinary backlog item", viaQueue.root, queue.NewItem{}); err != nil {
		t.Fatalf("seed the queue: %v", err)
	}
	itQ := viaQueue.create(t, "same intent")
	viaQueue.walkToReady(t, itQ.ID)
	if _, err := viaQueue.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: itQ.ID, Blueprint: "dev-loop", Goal: "the same goal", WorkspaceRoot: viaQueue.root, Tech: "go",
	}); err != nil {
		t.Fatalf("queued: %v", err)
	}
	// S24 now starts it: bring the materialized row to ready through the
	// queue's OWN door and drive the one materialization code path.
	rows, err := viaQueue.s.ListQueueItems(viaQueue.root)
	if err != nil || len(rows) != 2 {
		t.Fatalf("queue rows: %d %v", len(rows), err)
	}
	qi, cerr := queue.ItemFromRow(rows[1])
	if cerr != nil {
		t.Fatal(cerr)
	}
	if _, qr := q.Transition(qi, queue.StatusReady); qr != nil {
		t.Fatalf("ready the materialized row: %s", qr.Reason)
	}
	if !viaQueue.d.advanceNext(context.Background(), viaQueue.root) {
		t.Fatal("advanceNext started nothing")
	}
	var viaS24 protocol.LoopStartParams
	select {
	case viaS24 = <-viaQueue.starts:
	default:
		t.Fatal("S24 never reached the same door")
	}
	if viaDirect.Blueprint != viaS24.Blueprint || viaDirect.Goal != viaS24.Goal ||
		viaDirect.WorkspaceRoot != viaS24.WorkspaceRoot || viaDirect.Tech != viaS24.Tech {
		t.Errorf("payloads diverged:\n direct %+v\n queue  %+v", viaDirect, viaS24)
	}
	if fmt.Sprint(viaDirect.With) != fmt.Sprint(viaS24.With) {
		t.Errorf("with maps diverged: %v vs %v — the durable link must ride both ways identically", viaDirect.With, viaS24.With)
	}
	if (viaDirect.Attended == nil) != (viaS24.Attended == nil) {
		t.Error("attended diverged between the branches")
	}
}

func TestMaterializeRefusesTheNotReadyAndRechecksAtTheAttempt(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "not ready yet")
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err == nil || !strings.Contains(err.Error(), "ready") {
		t.Errorf("an idea item materialized: %v — only ready materializes", err)
	}
	// D4 at the attempt: fully walked, then the priority is pulled back —
	// the claim re-checks; it does not cache.
	f.mustTransition(t, it.ID, roadmap.StatusIdeating)
	f.mustTransition(t, it.ID, roadmap.StatusBriefAgreed)
	if _, ref := f.rm.SetArtifacts(it.ID, "/b/brief.md", "/b/plan.md"); ref != nil {
		t.Fatal(ref.Reason)
	}
	f.mustTransition(t, it.ID, roadmap.StatusPlanned)
	if _, ref := f.rm.SetPriority(it.ID, roadmap.PriorityKnown, "P1"); ref != nil {
		t.Fatal(ref.Reason)
	}
	f.mustTransition(t, it.ID, roadmap.StatusReady)
	if _, ref := f.rm.SetPriority(it.ID, roadmap.PriorityUnknown, ""); ref != nil {
		t.Fatal(ref.Reason)
	}
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("a downgraded priority still reached ready at the attempt: %v — D4 is decided NOW", err)
	}
	if got := f.show(t, it.ID); got.Status != roadmap.StatusReady {
		t.Errorf("the refused attempt left the item on %q — a refusal moves nothing", got.Status)
	}
}

// --- the reactor (T3/T4) -----------------------------------------------------------

func TestMaterializedLoopDoneLandsTheItemDone(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "export statements")
	f.walkToReady(t, it.ID)
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	f.publish(t, f.loopID(t, fmt.Sprintf("roadmap-item-%d", it.ID)), "done", "")
	f.awaitStatus(t, it.ID, roadmap.StatusDone)
}

func TestBlockedLoopLeavesTheRunningItemRunningAndSpeaks(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "export statements")
	f.walkToReady(t, it.ID)
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	f.publish(t, f.loopID(t, fmt.Sprintf("roadmap-item-%d", it.ID)), "blocked", "out of budget")
	await(t, "the block to be spoken", func() bool { return strings.Contains(f.w.logLines(), "reported itself blocked") })
	if got := f.show(t, it.ID); got.Status != roadmap.StatusRunning {
		t.Errorf("a blocked loop moved the item to %q — D6: an item's status is its own field", got.Status)
	}
}

func TestQueueBranchReactorRecordsTheLoopAndLandsDone(t *testing.T) {
	f := newRoadmapFixture(t)
	// The project HAS a queue, so the door takes R4's queue branch.
	q := queue.New(f.s)
	if _, err := q.Append(f.root, "dev-loop", "an ordinary backlog item", f.root, queue.NewItem{}); err != nil {
		t.Fatalf("give the project a queue: %v", err)
	}
	it := f.create(t, "export statements")
	f.walkToReady(t, it.ID)
	res, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "export monthly statements", WorkspaceRoot: f.root,
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if queued, _ := seamAnswer(t, res)["queued"].(bool); !queued {
		t.Fatal("a queued project's item did not take the queue branch")
	}
	// S24 starts it: the queue's own stamped loop carries the link row.
	rows, err := f.s.ListQueueItems(f.root)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows: %d %v", len(rows), err)
	}
	loopSlug := "loop-s24-started"
	loopID := f.seedLoop(t, loopSlug, "{}")
	stamped, err := f.s.SetQueueItemLoop(rows[1].ID, f.root, loopSlug)
	if err != nil || !stamped {
		t.Fatalf("stamp the queue row: stamped=%v err=%v", stamped, err)
	}
	f.publish(t, loopID, "done", "")
	got := f.awaitStatus(t, it.ID, roadmap.StatusDone)
	if got.LoopSlug != loopSlug {
		t.Errorf("loop_slug = %q — the reactor records S24's loop when it finishes", got.LoopSlug)
	}
}

func TestUntrackedLoopIsNotTheRoadmapsBusiness(t *testing.T) {
	f := newRoadmapFixture(t)
	// D8's sibling property: NO roadmap rows exist at all; a person's own
	// loop finishes done and the roadmap never stirs, loudly or quietly.
	before, err := f.rm.Count(f.root)
	if err != nil {
		t.Fatal(err)
	}
	loopID := f.seedLoop(t, "loop-personal", "{}")
	f.publish(t, loopID, "done", "")
	await(t, "the emission to settle", func() bool {
		n, err := f.rm.Count(f.root)
		return err == nil && n == before
	})
	if before != 0 {
		t.Fatalf("the fixture began with %d roadmap items — the D8 property needs zero", before)
	}
}

func TestRoadmapReactorIsJoinedAtTheEmissionPoint(t *testing.T) {
	// The wiring test: the reactor subscribes inside publishLoop (the
	// loop engine's own emit), not on a side channel — prove it by
	// firing publishLoop with a stamped running item and NO direct call
	// to any handler. If the join at blueprint.go were deleted, this is
	// the test that says so.
	f := newRoadmapFixture(t)
	it := f.create(t, "export statements")
	f.walkToReady(t, it.ID)
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: it.ID, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	fields := map[string]any{"loopId": f.loopID(t, fmt.Sprintf("roadmap-item-%d", it.ID)), "status": "done"}
	f.d.publishLoop(protocol.KindLoopFinished, fields)
	f.awaitStatus(t, it.ID, roadmap.StatusDone)
}

// --- D8's own assertion ------------------------------------------------------------

func TestZeroRoadmapItemsLeaveTheLoopDoorsUnchanged(t *testing.T) {
	// D8: `aeon loop start` works identically in a project that has zero
	// roadmap items. The property has two halves here: the roadmap doors
	// add NO condition to the loop doors (advanceNext and the direct
	// StartBlueprintLoop spy behave exactly as queue_reactor_test.go's
	// fixture shows them behaving with roadmap code absent — that fixture
	// IS this code present), and every roadmap read against an empty
	// board answers honestly empty/zero — never an error, never a guess.
	f := newRoadmapFixture(t)
	items, err := f.rm.List(f.root, roadmap.ListFilter{})
	if err != nil || len(items) != 0 {
		t.Fatalf("empty board: len=%d err=%v", len(items), err)
	}
	n, err := f.rm.Count(f.root)
	if err != nil || n != 0 {
		t.Fatalf("empty count: %d %v", n, err)
	}
	// The queue's own door runs untouched: an advance on a project with
	// neither queue rows nor roadmap items simply starts nothing.
	if f.d.advanceNext(context.Background(), f.root) {
		t.Error("advanceNext started something from nothing")
	}
	// A roadmap item materialize attempt against the empty board is a
	// named not-exist, not a silence.
	if _, err := f.d.Table.RoadmapMaterialize(context.Background(), roadmap.Actor{Kind: "user", ID: "test-person"}, protocol.RoadmapMaterializeParams{
		ID: 999, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: f.root,
	}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("materialize of a missing item answered %v — not-found rides the refusal, said", err)
	}
}

// The planner files plan.md as a document of its host loop, not a file at the
// workspace root; the daemon exports the hard copy the pointer names, under
// .projects/{host slug}/ (hardcopy.go), and the root stays clean.
func TestPlanningLandsFromTheHostLoopsDocument(t *testing.T) {
	f := newRoadmapFixture(t)
	it := f.create(t, "plan me")
	f.walkToBriefAgreed(t, it.ID)
	hostSlug := fmt.Sprintf("plan-item-%d", it.ID)
	if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: hostSlug, ComponentRoot: f.root, Root: f.root, Goal: "plan", Status: "running"}, "standard", 1, "raw", "{}"); err != nil {
		t.Fatal(err)
	}
	if res, err := f.s.WriteDocument(store.Scope{LoopSlug: hostSlug, ComponentRoot: f.root}, "plan.md", "# Understanding\nthe plan", "project-planner"); err != nil || !res.OK {
		t.Fatalf("file the plan: %+v %v", res, err)
	}
	if !f.d.landPlanning(f.show(t, it.ID), f.root, okStep(`{"task_count": 2}`), nil) {
		t.Fatal("a plan filed as a document was refused its landing")
	}
	got := f.show(t, it.ID)
	want := filepath.Join(f.root, ".projects", hostSlug, "plan.md")
	if got.Status != roadmap.StatusPlanned || got.PlanPath != want {
		t.Fatalf("status %q plan_path %q, want planned at %q", got.Status, got.PlanPath, want)
	}
	if b, err := os.ReadFile(want); err != nil || string(b) != "# Understanding\nthe plan" {
		t.Fatalf("the hard copy: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "plan.md")); !os.IsNotExist(err) {
		t.Fatalf("the workspace root must stay clean: %v", err)
	}
}

func TestTheIdeationBriefPointerIsTheExportedHardCopy(t *testing.T) {
	f := newRoadmapFixture(t)
	if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "ideate-1", ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"}, "standard", 1, "raw", "{}"); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "ideate-1", ComponentRoot: f.root}
	if res, err := f.s.WriteDocument(sc, "brief.md", "the agreed brief", "brief-author"); err != nil || !res.OK {
		t.Fatalf("file the brief: %+v %v", res, err)
	}
	pin, err := f.s.ReadPinnedLoop(mustLoopID(t, f.s, sc))
	if err != nil {
		t.Fatal(err)
	}
	got := f.d.briefPathFor(pin)
	want := filepath.Join(f.root, ".projects", "ideate-1", "brief.md")
	if got != want {
		t.Fatalf("brief pointer %q, want %q", got, want)
	}
	if b, err := os.ReadFile(got); err != nil || string(b) != "the agreed brief" {
		t.Fatalf("the hard copy: %q %v", b, err)
	}
	// A loop that filed nothing keeps the legacy workspace path.
	if _, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "ideate-2", ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"}, "standard", 1, "raw", "{}"); err != nil {
		t.Fatal(err)
	}
	pin2, _ := f.s.ReadPinnedLoop(mustLoopID(t, f.s, store.Scope{LoopSlug: "ideate-2", ComponentRoot: f.root}))
	if got := f.d.briefPathFor(pin2); got != filepath.Join(f.root, "brief.md") {
		t.Fatalf("legacy pointer %q", got)
	}
}

func TestADocumentOfAnotherLoopsFolderReadsAsAPlainFile(t *testing.T) {
	e := newRecordsEnv(t, "")
	svc := NewDocService(e.s)
	ctx := e.ctx // the step's loop is "walk"
	if name, ok := svc.Claim(ctx, ".projects/walk/brief.md"); !ok || name != "brief.md" {
		t.Fatalf("own folder is claimed: %q %v", name, ok)
	}
	if _, ok := svc.Claim(ctx, ".projects/ideate-1/brief.md"); ok {
		t.Fatal("another loop's folder is a hard copy the daemon exported: it must read as a plain file")
	}
}

func mustLoopID(t *testing.T, s *store.Store, sc store.Scope) string {
	t.Helper()
	l, err := s.GetLoop(sc)
	if err != nil {
		t.Fatal(err)
	}
	return l.ID
}
