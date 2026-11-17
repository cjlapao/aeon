package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/retro"
	"github.com/cjlapao/aeon/internal/store"
)

// retroFixture drives the REAL emission door with a fresh store — the
// gate reactor's harness, kept honest: publishLoop is the production
// path, so what these tests observe is the wiring a terminal event
// actually walks.
type retroFixture struct {
	d    *Daemon
	s    *store.Store
	w    *wire
	root string
}

func newRetroFixture(t *testing.T) *retroFixture {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify})
	d.initTriage()
	root := "/repo/retro-reactor"
	if _, err := s.CreateComponent(store.NewComponent{Name: "rr", Slug: "rr", Root: root}); err != nil {
		t.Fatalf("seed component: %v", err)
	}
	return &retroFixture{d: d, s: s, w: w, root: root}
}

func (f *retroFixture) pin(t *testing.T, slug, blueprint string) string {
	t.Helper()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: f.root, Root: f.root, Goal: "g", Status: "running"},
		blueprint, 1, queueBlueprintYAML, "{}")
	if err != nil {
		t.Fatalf("pin loop %s: %v", slug, err)
	}
	return l.ID
}

func (f *retroFixture) emitFinished(loopID, status string) {
	f.d.publishLoop(protocol.KindLoopFinished, map[string]any{"loopId": loopID, "status": status})
}

func (f *retroFixture) emitBlocked(loopID string, reason any) {
	f.d.publishLoop(protocol.KindLoopBlocked, map[string]any{"loopId": loopID, "reason": reason})
}

// waitForFacts blocks until a slug has n outcome rows, or fails — the
// recorder is a goroutine (V18), so the durable effect, not a sleep, is
// what a test may assert on.
func (f *retroFixture) waitForFacts(t *testing.T, slug string, n int) []store.RetroOutcome {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows := f.factsFor(t, slug)
		if len(rows) == n {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("loop %s has %d fact rows, wanted %d", slug, len(rows), n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *retroFixture) factsFor(t *testing.T, slug string) []store.RetroOutcome {
	t.Helper()
	rows, err := f.s.ListRetroOutcomes(store.RetroFilter{Root: f.root, Limit: 100})
	if err != nil {
		t.Fatalf("list facts: %v", err)
	}
	var out []store.RetroOutcome
	for _, r := range rows {
		if r.LoopSlug == slug {
			out = append(out, r)
		}
	}
	return out
}

func (f *retroFixture) logs() string {
	f.w.mu.Lock()
	defer f.w.mu.Unlock()
	return strings.Join(f.w.logs, "\n")
}

// seedStep lays one finished step with an outcome and a cost.
func (f *retroFixture) seedStep(t *testing.T, slug, agent, outcome string, cost model.Cost) {
	t.Helper()
	sc := store.Scope{LoopSlug: slug, ComponentRoot: f.root}
	st, err := f.s.CreateStep(sc, store.NewStep{Phase: "build", Agent: agent, Attempt: 1, Status: store.StepRunning, Source: "engine"})
	if err != nil {
		t.Fatalf("seed step: %v", err)
	}
	if err := f.s.SetStepStatus(st.ID, store.StepDone, outcome); err != nil {
		t.Fatalf("finish step: %v", err)
	}
	if err := f.s.SetStepCost(st.ID, cost); err != nil {
		t.Fatalf("cost step: %v", err)
	}
}

func (f *retroFixture) seedTask(t *testing.T, slug, taskID, status string) {
	t.Helper()
	sc := store.Scope{LoopSlug: slug, ComponentRoot: f.root}
	if _, err := f.s.CreateTask(sc, store.NewTask{TaskID: taskID, Title: taskID, Status: store.TaskStatus(status)}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func TestRecorderWritesOneOutcomePerTerminalEvent(t *testing.T) {
	f := newRetroFixture(t)
	id := f.pin(t, "s63-alpha", "dev-loop")
	if err := f.s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "golang-engineer", Name: "golang-engineer"}, "2026-11-04T17:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	f.seedTask(t, "s63-alpha", "T1", "complete")
	f.seedTask(t, "s63-alpha", "T2", "failed")
	f.seedStep(t, "s63-alpha", "golang-engineer", "done", model.Cost{State: model.CostKnown, USD: 1.25, USDAtLeast: 1.25})
	f.seedStep(t, "s63-alpha", "golang-engineer", "unverified", model.Cost{State: model.CostUnknown})

	f.emitFinished(id, "done")
	rows := f.waitForFacts(t, "s63-alpha", 1)
	r := rows[0]

	pin, err := f.s.ReadPinnedLoop(id)
	if err != nil {
		t.Fatal(err)
	}
	if r.OutcomeKind != "done" {
		t.Errorf("kind %q", r.OutcomeKind)
	}
	if want := "done@" + pin.UpdatedAt; r.TerminalKey != want {
		t.Errorf("terminal key %q, want %q (RT2: kind + @ + pin.updated_at)", r.TerminalKey, want)
	}
	if r.Source != "recorder" || r.BlueprintID != "dev-loop" || *r.BlueprintVersion != 1 {
		t.Errorf("pin coordinates lost: %+v", r)
	}
	if r.Attended != nil {
		t.Errorf("attended %v — an undeclared pin stays NULL (P31)", *r.Attended)
	}
	if *r.TasksDone != 1 || *r.TasksFailed != 1 {
		t.Errorf("task counts %v/%v, want 1/1", *r.TasksDone, *r.TasksFailed)
	}
	if *r.UnverifiedSteps != 1 {
		t.Errorf("unverified %v, want 1 (storedOutcome's literal, T0 §1.9)", *r.UnverifiedSteps)
	}
	if r.VerifiedPass != nil {
		t.Error("verified_pass was invented — no persisted handoff declaration exists")
	}
	if r.Complete {
		t.Error("complete=1 on a row that could not read verified_pass")
	}
	if r.QARounds == nil || *r.QARounds != 0 || r.Escalations == nil || *r.Escalations != 0 {
		t.Errorf("readable zeros arrived as NULL: qa=%v esc=%v", r.QARounds, r.Escalations)
	}
	if r.WallMS != nil {
		t.Errorf("wall %v — a loop with no ended run has NULL, not 0", *r.WallMS)
	}
	if r.CostUSDFloor == nil || *r.CostUSDFloor != 1.25 || *r.CostUnknownSteps != 1 {
		t.Errorf("cost floor %v / unknown %v, want the LoopCost statement", *r.CostUSDFloor, *r.CostUnknownSteps)
	}
	agents, err := f.s.RetroAgentsFor(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Agent != "golang-engineer" || !agents[0].AgentSlugKnown {
		t.Fatalf("agent rows: %+v", agents)
	}
	// finished/aborted is the third word of the event vocabulary (T0 §1.3
	// corrected: the events carry done|aborted; the loop_run STATE word
	// `cancelled` never arrives — OutcomeKindForFinished keeps the mapping
	// defensively, measured in the retro package).
	f.emitFinished(id, "aborted")
	rows = append(rows, f.waitForFacts(t, "s63-alpha", 2)[1])
	sawAborted := false
	for _, r2 := range f.factsFor(t, "s63-alpha") {
		if r2.OutcomeKind == "aborted" {
			sawAborted = true
		}
	}
	if !sawAborted {
		t.Error("no aborted row")
	}
	// The blocked family carries the STRUCT shape the engine emits; the
	// kind is stored, the detail never is (RT2: a fact, not a transcript).
	f2 := newRetroFixture(t)
	id2 := f2.pin(t, "s63-beta", "dev-loop")
	f2.emitBlocked(id2, &loop.BlockedReason{Kind: "cap-hit", Detail: "the whole reason detail text"})
	rows2 := f2.waitForFacts(t, "s63-beta", 1)
	if rows2[0].OutcomeKind != "blocked/cap-hit" {
		t.Errorf("blocked kind %q", rows2[0].OutcomeKind)
	}
	if strings.Contains(fmt.Sprintf("%+v", rows2[0]), "detail text") {
		t.Error("reason DETAIL reached a fact row")
	}
}

func TestRecorderBlockedThenDoneKeepsBoth(t *testing.T) {
	f := newRetroFixture(t)
	id := f.pin(t, "s63-gamma", "dev-loop")
	f.emitBlocked(id, "cap-hit") // the plain-string shape also arrives (queue precedent)
	f.waitForFacts(t, "s63-gamma", 1)
	f.emitFinished(id, "done")
	rows := f.waitForFacts(t, "s63-gamma", 2)
	kinds := map[string]bool{}
	for _, r := range rows {
		kinds[r.OutcomeKind] = true
	}
	if !kinds["blocked/cap-hit"] || !kinds["done"] {
		t.Errorf("kinds %v — the blocked fact and the done fact are BOTH outcomes", kinds)
	}
}

func TestRecorderIsIdempotentOnReplay(t *testing.T) {
	f := newRetroFixture(t)
	id := f.pin(t, "s63-delta", "dev-loop")
	f.emitFinished(id, "done")
	f.waitForFacts(t, "s63-delta", 1)
	f.emitFinished(id, "done") // a re-delivery of the same terminal
	time.Sleep(settle)
	if rows := f.factsFor(t, "s63-delta"); len(rows) != 1 {
		t.Fatalf("%d rows after a replay, want 1 (terminal key + ON CONFLICT)", len(rows))
	}
}

func TestRecorderNeverBlocksTheEventPath(t *testing.T) {
	f := newRetroFixture(t)
	id := f.pin(t, "s63-epsilon", "dev-loop")
	held := make(chan struct{})
	restored := make(chan struct{})
	prev := retroCompose
	retroCompose = func(ctx context.Context, r retro.Reader, ev retro.Event) (store.RetroOutcome, []store.RetroAgent, []string, error) {
		<-held
		out, a, m, err := prev(ctx, r, ev)
		close(restored)
		return out, a, m, err
	}
	defer func() { retroCompose = prev }()

	start := time.Now()
	f.emitFinished(id, "done") // the production path
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("publishLoop waited %v for the recorder (V18)", elapsed)
	}
	evs, err := f.s.ReadEvents(0, store.EventFilter{Kinds: []string{protocol.KindLoopFinished}, ComponentRoot: f.root}, 10)
	if err != nil || len(evs) == 0 {
		t.Fatalf("the event never reached the durable stream while the recorder was stuck: %v", err)
	}
	if rows := f.factsFor(t, "s63-epsilon"); len(rows) != 0 {
		t.Fatal("the recorder wrote through the gate — it was never held")
	}
	close(held)
	select {
	case <-restored:
	case <-time.After(3 * time.Second):
		t.Fatal("the recorder never ran after the gate opened")
	}
	f.waitForFacts(t, "s63-epsilon", 1)
}

func TestRecorderSkipsTriageValidationAndRetrospectiveLoops(t *testing.T) {
	f := newRetroFixture(t)
	f15 := f.pin(t, "triage-validate-42", "triage-validation")
	f.emitFinished(f15, "done")
	retro1 := f.pin(t, "s63-self-review", "retrospective") // RT5: the loop IS the retrospective
	f.emitFinished(retro1, "done")
	time.Sleep(settle)
	if rows := f.factsFor(t, "triage-validate-42"); len(rows) != 0 {
		t.Errorf("F15's validation loop recorded %d rows — it would double-count the bug's own outcomes", len(rows))
	}
	if rows := f.factsFor(t, "s63-self-review"); len(rows) != 0 {
		t.Errorf("a retrospective loop recorded %d rows", len(rows))
	}
	if !strings.Contains(f.logs(), "retro:") || !strings.Contains(f.logs(), "skipped") {
		t.Errorf("the skips were silent; log:\n%s", f.logs())
	}
}

// failingReader passes everything through except the one field a test
// needs unreadable — the store error the daemon must survive as a NULL.
type failingReader struct {
	retro.Reader
	failTasks bool
}

func (r failingReader) ListTasks(sc store.Scope) ([]store.Task, error) {
	if r.failTasks {
		return nil, fmt.Errorf("test: tasks unreadable")
	}
	return r.Reader.ListTasks(sc)
}

func TestUnreadableFieldIsNullAndIncomplete(t *testing.T) {
	f := newRetroFixture(t)
	id := f.pin(t, "s63-zeta", "dev-loop")
	f.seedTask(t, "s63-zeta", "T1", "complete")
	prev := retroCompose
	retroCompose = func(ctx context.Context, r retro.Reader, ev retro.Event) (store.RetroOutcome, []store.RetroAgent, []string, error) {
		return prev(ctx, failingReader{Reader: r, failTasks: true}, ev)
	}
	defer func() { retroCompose = prev }()

	f.emitFinished(id, "done")
	rows := f.waitForFacts(t, "s63-zeta", 1)
	if rows[0].TasksDone != nil {
		t.Error("an unreadable tasks read arrived as a value, not NULL (P31)")
	}
	if rows[0].Complete {
		t.Error("complete=1 with a field missing — RT2 says complete=0")
	}
	if !strings.Contains(f.logs(), "retro:") || !strings.Contains(f.logs(), "tasks") {
		t.Errorf("the missing field was not named in the log:\n%s", f.logs())
	}
}

func TestRecorderFailureCountsDropped(t *testing.T) {
	f := newRetroFixture(t)
	id := f.pin(t, "s63-eta", "dev-loop")
	prev := retroCompose
	retroCompose = func(context.Context, retro.Reader, retro.Event) (store.RetroOutcome, []store.RetroAgent, []string, error) {
		return store.RetroOutcome{}, nil, nil, fmt.Errorf("test: compose refused")
	}
	before := retroDroppedCount()
	f.emitFinished(id, "done")
	deadline := time.Now().Add(3 * time.Second)
	for retroDroppedCount() == before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	retroCompose = prev
	if got := retroDroppedCount(); got <= before {
		t.Errorf("dropped counter %d, want > %d (RT6: never silent)", got, before)
	}
	if !strings.Contains(f.logs(), "dropped") {
		t.Errorf("the drop was not logged:\n%s", f.logs())
	}
	if rows := f.factsFor(t, "s63-eta"); len(rows) != 0 {
		t.Errorf("a failed compose still wrote %d rows", len(rows))
	}
}

// TestRecorderCallIsTheOnlyEditToPublishLoop is RT2's scope lock: the
// reactor lives beside publishLoop; blueprint.go gains exactly one line
// calling it, and it sits in the terminal block (the line before it is
// the merge-queue call the S44 T4 reactor joined).
func TestRecorderCallIsTheOnlyEditToPublishLoop(t *testing.T) {
	for _, site := range productionCallSites(t, "d.onRetroTerminal(") {
		if !strings.HasSuffix(site, "blueprint.go") {
			t.Errorf("the recorder call lives outside publishLoop's file: %s", site)
		}
	}
	srcRaw, err := os.ReadFile("blueprint.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcRaw)
	if n := strings.Count(src, "d.onRetroTerminal("); n != 1 {
		t.Fatalf("blueprint.go calls the recorder %d times, want exactly 1", n)
	}
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if strings.Contains(l, "d.onRetroTerminal(") {
			j := i - 1
			for j >= 0 && strings.HasPrefix(strings.TrimSpace(lines[j]), "//") {
				j-- // the RT2 comment rides between the merge-queue call and this one
			}
			if j < 0 || !strings.Contains(lines[j], "d.onMergeQueueTerminal(") {
				t.Fatalf("the call at line %d is not in the terminal block (preceded by: %q)", i+1, strings.TrimSpace(lines[j]))
			}
		}
	}
}
