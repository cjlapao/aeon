package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/plan"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// recordsEnv: a group, a component with a registered workspace, a loop
// running in it, and a step row — the whole world a real step stands on.
type recordsEnv struct {
	s      *store.Store
	svc    *RecordsService
	ctx    context.Context
	proj   string
	comp   store.Component
	stepID int64
	agent  string
}

func newRecordsEnv(t *testing.T, blueprint string) recordsEnv {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := recordsEnv{s: st, agent: "implementer"}
	// A "project" is a root component (S44-extra); alpha is a child of it.
	p, err := st.CreateComponent(store.NewComponent{Name: "org group", Slug: "org-group", Root: "/repo/org-group"})
	if err != nil || !p.OK() {
		t.Fatalf("group: %v", err)
	}
	e.proj = p.Component.ID
	c, err := st.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: "/repo/alpha", ParentID: p.Component.ID})
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	e.comp = c.Component
	in := store.NewLoop{Slug: "walk", ComponentRoot: "/repo/alpha", Title: "W", Goal: "g"}
	if blueprint != "" {
		// The pin table is where a loop's blueprint lives (blueprint.go).
		if _, err := st.CreatePinnedLoop(in, blueprint, 1, "raw blueprint text", "{}"); err != nil {
			t.Fatalf("pinned loop: %v", err)
		}
	} else if _, err := st.CreateLoop(in); err != nil {
		t.Fatalf("loop: %v", err)
	}
	sc := store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}
	step, err := st.CreateStep(sc, store.NewStep{TaskID: "T1", Phase: "build", Agent: e.agent})
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	e.stepID = step.ID
	e.ctx = stepctx.WithStepID(context.Background(), step.ID)
	e.svc = NewRecordsService(st)
	return e
}

func (e recordsEnv) do(t *testing.T, name, input string) (any, *Failure, error) {
	t.Helper()
	return e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, name, json.RawMessage(input))
}

func TestGoalCreateStampsIdentityNotInput(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	// Smuggling identity fields is a PARSE refusal — the schema has no field
	// for them and the decoder refuses one that arrives.
	for _, smuggle := range []string{`"project_id":"prj-other"`, `"created_by":{"kind":"user","id":"ghost"}`, `"source_step":999`, `"loop_slug":"other-loop"`} {
		in := `{"title":"x","type":"chore",` + smuggle + `,"components":[{"component":"alpha"}]}`
		_, f, err := e.do(t, "goal_create", in)
		if err != nil || f == nil || !strings.Contains(f.Sentence, "refused:") {
			t.Fatalf("smuggled %s: %v %+v — identity must be a PARSE refusal", smuggle, err, f)
		}
	}
	// The clean call stamps from the STEP.
	out, f, err := e.do(t, "goal_create", `{"title":"real goal","type":"bug","components":[{"component":"alpha","confidence":"high","basis":["measured"]}]}`)
	if err != nil || f != nil {
		t.Fatalf("goal_create: %v %+v", err, f)
	}
	m := out.(map[string]any)
	g := m["goal"].(store.Goal)
	if g.ComponentID != e.proj || g.SourceLoop != "walk" || g.CreatedByKind != "agent" || g.CreatedByID != e.agent {
		t.Fatalf("stamped wrong: %+v", g)
	}
	if g.SourceStep == nil || *g.SourceStep != e.stepID {
		t.Fatalf("source_step %v, want %d", g.SourceStep, e.stepID)
	}
	if len(g.Components) != 1 || g.Components[0].ComponentID != e.comp.ID || g.Components[0].Confidence == nil || *g.Components[0].Confidence != 3 {
		t.Fatalf("stake set: %+v", g.Components)
	}
	// The brainstorm loop is the ONLY thing that names source_kind brainstorm.
	if g.SourceKind != "manual" {
		t.Fatalf("feature-loop goal source_kind %q, want manual", g.SourceKind)
	}
	e2 := newRecordsEnv(t, "brainstorm")
	out2, f, err := e2.do(t, "goal_create", `{"title":"bs","type":"chore","components":[{"component":"alpha"}]}`)
	if err != nil || f != nil {
		t.Fatalf("brainstorm goal: %v %+v", err, f)
	}
	if g2 := out2.(map[string]any)["goal"].(store.Goal); g2.SourceKind != "brainstorm" {
		t.Fatalf("brainstorm-loop goal source_kind %q", g2.SourceKind)
	}
}

func TestGoalCreateRefusesAForeignComponent(t *testing.T) {
	e := newRecordsEnv(t, "")
	other, err := e.s.CreateComponent(store.NewComponent{Name: "elsewhere", Slug: "elsewhere", Root: "/repo/elsewhere"})
	if err != nil || !other.OK() {
		t.Fatalf("other group: %v", err)
	}
	if _, err := e.s.CreateComponent(store.NewComponent{Name: "beta", Slug: "beta", Root: "/repo/beta", ParentID: other.Component.ID}); err != nil {
		t.Fatalf("beta: %v", err)
	}
	_, f, err := e.do(t, "goal_create", `{"title":"cross","type":"chore","components":[{"component":"beta"}]}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "not in this root") {
		t.Fatalf("foreign component: %v %+v", err, f)
	}
	// By id too, and it says the same sentence.
	_, f, _ = e.do(t, "goal_create", `{"title":"cross","type":"chore","components":[{"component":"`+func() string {
		cs, _ := e.s.ListComponents(store.ComponentFilter{ParentID: other.Component.ID})
		return cs[0].ID
	}()+`"}]}`)
	if f == nil || !strings.Contains(f.Sentence, "not in this root") {
		t.Fatalf("foreign component by id: %+v", f)
	}
}

func TestGoalCreateLeavesTheGoalInReview(t *testing.T) {
	e := newRecordsEnv(t, "")
	out, f, err := e.do(t, "goal_create", `{"title":"await","type":"feature","components":[{"component":"alpha"}]}`)
	if err != nil || f != nil {
		t.Fatalf("create: %v %+v", err, f)
	}
	g := out.(map[string]any)["goal"].(store.Goal)
	if g.ReviewState != "in_review" {
		t.Fatalf("an agent's goal reads %q — every goal passes a person (OL7)", g.ReviewState)
	}
}

func TestBriefCreateBumpsVersion(t *testing.T) {
	e := newRecordsEnv(t, "")
	b1, f, err := e.do(t, "brief_create", `{"scope":"first contract","acceptance_criteria":["green"]}`)
	if err != nil || f != nil {
		t.Fatalf("brief 1: %v %+v", err, f)
	}
	if b1.(map[string]any)["brief"].(store.Brief).Version != 1 {
		t.Fatalf("first brief version %d", b1.(map[string]any)["brief"].(store.Brief).Version)
	}
	b2, f, err := e.do(t, "brief_create", `{"scope":"second contract"}`)
	if err != nil || f != nil {
		t.Fatalf("brief 2: %v %+v", err, f)
	}
	if b2.(map[string]any)["brief"].(store.Brief).Version != 2 {
		t.Fatalf("second brief version %d, want 2 — rewrites never overwrite", b2.(map[string]any)["brief"].(store.Brief).Version)
	}
	// The old version keeps meaning.
	_, f, err = e.do(t, "goal_show", `{"goal_id":"goal-nope"}`)
	if f == nil {
		t.Fatal("a goal that is nobody read as success")
	}
}

func TestAttachRefusesAForeignSubject(t *testing.T) {
	e := newRecordsEnv(t, "")
	// A document to pin (through the store's own door).
	sc := store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}
	if _, err := e.s.WriteDocument(sc, "plan.md", "the plan", "agent:x"); err != nil {
		t.Fatalf("document: %v", err)
	}
	other, err := e.s.CreateComponent(store.NewComponent{Name: "elsewhere", Slug: "elsewhere", Root: "/repo/elsewhere"})
	if err != nil || !other.OK() {
		t.Fatalf("other: %v", err)
	}
	og, rf, err := e.s.CreateGoal(store.NewGoal{ComponentID: other.Component.ID, Title: "theirs", Type: "chore",
		SourceKind: "manual", By: store.Actor{Kind: "user", ID: "cj"}})
	if err != nil || rf != nil {
		t.Fatalf("their goal: %v %+v", err, rf)
	}
	if _, f, _ := e.do(t, "attach", `{"subject":{"kind":"goal","id":"`+og.ID+`"},"document":"plan.md"}`); f == nil || !strings.Contains(f.Sentence, "another root") {
		t.Fatalf("attached to another root's goal: %+v", f)
	}
	// And accepts its own.
	out, f, err := e.do(t, "goal_create", `{"title":"mine","type":"chore","components":[{"component":"alpha"}]}`)
	if err != nil || f != nil {
		t.Fatalf("own goal: %v %+v", err, f)
	}
	gid := out.(map[string]any)["goal"].(store.Goal).ID
	if _, f, err := e.do(t, "attach", `{"subject":{"kind":"goal","id":"`+gid+`"},"document":"plan.md"}`); err != nil || f != nil {
		t.Fatalf("own attach: %v %+v", err, f)
	}
	// Another loop's brief is not this step's contract to pin.
	if _, f, _ := e.do(t, "attach", `{"subject":{"kind":"brief","id":"someone-else"},"document":"plan.md"}`); f == nil || !strings.Contains(f.Sentence, "another loop") {
		t.Fatalf("foreign brief: %+v", f)
	}
}

func TestInvokerWithoutAStepFailsClosed(t *testing.T) {
	e := newRecordsEnv(t, "")
	content, isErr := e.svc.Invoker()(context.Background(), "goal_create", json.RawMessage(`{"title":"x","type":"chore","components":[{"component":"alpha"}]}`))
	if !isErr || !strings.Contains(content, "no step identity") {
		t.Fatalf("stepless call: %q isError=%v — must fail CLOSED with a sentence", content, isErr)
	}
	// Zero is not an identity either.
	content, isErr = e.svc.Invoker()(stepctx.WithStepID(context.Background(), 0), "component_list", nil)
	if !isErr {
		t.Fatalf("zero-step call answered %q — zero reads absent", content)
	}
	// Wired and stepped, the same door answers JSON.
	content, isErr = e.svc.Invoker()(e.ctx, "component_list", nil)
	if isErr || !strings.Contains(content, "org group") {
		t.Fatalf("wired listing: %q isError=%v", content, isErr)
	}
}

// ── S38 T10: the task family ────────────────────────────────────────────────

func TestTaskCreateStampsAndRefusesManual(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	// The assignee starts as the agent that planned it (OL14's "assignee that
	// changes as it climbs" starts at the planner's hand).
	out, f, err := e.do(t, "task_create", `{"title":"wire the socket","tech":"go","depends_on":[]}`)
	if err != nil || f != nil {
		t.Fatalf("task_create: %v %+v", err, f)
	}
	row := out.(store.Task)
	if row.Engineer != e.agent || row.TaskID != "T1" {
		t.Fatalf("stamped wrong: engineer=%q taskid=%q", row.Engineer, row.TaskID)
	}
	if _, f, _ := e.do(t, "task_create", `{"title":"second","depends_on":["T1"]}`); f != nil {
		t.Fatalf("second task (dependency exists): %+v", f)
	}
	// manual:true is refused while no chat loop exists — and the refusal
	// NAMES the rule, so the caller learns the boundary from the sentence.
	if _, f, _ := e.do(t, "task_create", `{"title":"hand-made","manual":true,"type":"chore"}`); f == nil || !strings.Contains(f.Sentence, "plain chat") {
		t.Fatalf("manual inside a blueprint loop: %+v", f)
	}
	// Stubbed chat (the spec's rule: test the helper with a stub, never a
	// fake chat): manual is honoured, and a manual task WITHOUT its type is
	// refused by the store's own door.
	prev := isPlainChat
	isPlainChat = func(Identity) bool { return true }
	defer func() { isPlainChat = prev }()
	if _, f, err := e.do(t, "task_create", `{"title":"hand-made","manual":true,"type":"chore","body_note":"because"}`); err != nil || f != nil {
		t.Fatalf("chat-loop manual task: %v %+v", err, f)
	}
	if _, f, _ := e.do(t, "task_create", `{"title":"typeless","manual":true}`); f == nil {
		t.Fatal("a manual task with no type read as created")
	}
}

func TestDependsOnSpeaksPlansSentence(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	want := (&plan.Refusal{Kind: "depends-on-missing", Task: "T1", Depend: "T9", File: "task_create"}).Sentence()
	_, f, _ := e.do(t, "task_create", `{"title":"orphan","depends_on":["T9"]}`)
	if f == nil || f.Sentence != want {
		t.Fatalf("missing dependency said %+v — must speak plan's ONE sentence verbatim: %q", f, want)
	}
	wantSelf := (&plan.Refusal{Kind: "self-dependency", Task: "T1", Depend: "T1", File: "task_create"}).Sentence()
	_, f, _ = e.do(t, "task_create", `{"title":"loop","id":"T1","depends_on":["T1"]}`)
	if f == nil || f.Sentence != wantSelf {
		t.Fatalf("self-dependency said %+v, want %q", f, wantSelf)
	}
	// Duplicate id is plan's duplicate-id class, same discipline.
	if _, f, err := e.do(t, "task_create", `{"title":"first","id":"T1"}`); err != nil || f != nil {
		t.Fatalf("first T1: %v %+v", err, f)
	}
	_, f, _ = e.do(t, "task_create", `{"title":"again","id":"T1"}`)
	wantDup := (&plan.Refusal{Kind: "duplicate-id", Task: "T1", File: "task_create"}).Sentence()
	if f == nil || f.Sentence != wantDup {
		t.Fatalf("duplicate id said %+v, want plan's verbatim %q", f, wantDup)
	}
}

func TestTaskSetStatusOwnLoopOnly(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	out, f, err := e.do(t, "task_create", `{"title":"mine","id":"T1"}`)
	if err != nil || f != nil {
		t.Fatalf("create: %v %+v", err, f)
	}
	mine := out.(store.Task)
	// A second loop in the same component owns its own rows, not mine.
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "other", ComponentRoot: "/repo/alpha", Title: "O", Goal: "g"}); err != nil {
		t.Fatalf("other loop: %v", err)
	}
	if _, err := e.s.CreateTask(store.Scope{LoopSlug: "other", ComponentRoot: "/repo/alpha"}, store.NewTask{TaskID: "T1", Title: "theirs"}); err != nil {
		t.Fatalf("their task: %v", err)
	}
	theirs, err := e.s.GetTask(store.Scope{LoopSlug: "other", ComponentRoot: "/repo/alpha"}, "T1")
	if err != nil {
		t.Fatalf("their row: %v", err)
	}
	if _, f, _ := e.do(t, "task_set_status", `{"id":"`+theirs.ID+`","status":"in-progress"}`); f == nil || !strings.Contains(f.Sentence, "not owned by this loop") {
		t.Fatalf("another loop's task: %+v", f)
	}
	// Own task: the machine decides; the agent only asks.
	out2, f, err := e.do(t, "task_set_status", `{"id":"`+mine.ID+`","status":"in-progress","evidence":"tests run"}`)
	if err != nil || f != nil {
		t.Fatalf("own transition: %v %+v", err, f)
	}
	if got := out2.(store.Task); got.Status != string(store.StatusInProgress) {
		t.Fatalf("status %q after the machine said yes", got.Status)
	}
	// An illegal rung is the machine's refusal, verbatim — the reason and the
	// path it names are what the agent acts on.
	// in-progress → verified is LEGAL (measured against machine.go's ladder);
	// back to pending is not, and the refusal must name both states.
	if _, f, _ := e.do(t, "task_set_status", `{"id":"`+mine.ID+`","status":"verified"}`); f != nil {
		t.Fatalf("the machine refused a legal rung: %+v", f)
	}
	if _, f, _ := e.do(t, "task_set_status", `{"id":"`+mine.ID+`","status":"pending"}`); f == nil {
		t.Fatal("in-progress → pending read as success — the machine was bypassed")
	} else if !strings.Contains(f.Sentence, "pending") {
		t.Fatalf("the refusal must name the state refused: %q", f.Sentence)
	}
}

func TestTaskSetStatusManualNeedsWorkerLink(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	mt, rf, err := e.s.CreateManualTask(store.NewManualTask{ComponentID: e.comp.ID, Title: "by hand", Type: "chore",
		AssigneeKind: "agent", Assignee: "someone", By: store.Actor{Kind: "user", ID: "cj"}})
	if err != nil || rf != nil {
		t.Fatalf("manual task: %v %+v", err, rf)
	}
	if _, f, _ := e.do(t, "task_set_status", `{"id":"`+mt.ID+`","status":"in-progress"}`); f == nil || !strings.Contains(f.Sentence, "not a worker") {
		t.Fatalf("an unlinked loop worked a manual task: %+v", f)
	}
	if err := e.s.LinkTaskToLoop(mt.ID, store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}, "worker"); err != nil {
		t.Fatalf("worker link: %v", err)
	}
	if _, f, err := e.do(t, "task_set_status", `{"id":"`+mt.ID+`","status":"in-progress","evidence":"did it"}`); err != nil || f != nil {
		t.Fatalf("linked worker refused: %v %+v", err, f)
	}
}

func TestTaskBumpCounterOwnsItsCounter(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	out, f, err := e.do(t, "task_create", `{"title":"flaky","id":"T1"}`)
	if err != nil || f != nil {
		t.Fatalf("create: %v %+v", err, f)
	}
	mine := out.(store.Task)
	got, f, err := e.do(t, "task_bump_counter", `{"id":"`+mine.ID+`","counter":"retries"}`)
	if err != nil || f != nil {
		t.Fatalf("bump: %v %+v", err, f)
	}
	if m := got.(map[string]any); m["value"] != 1 || m["capped"] != false {
		t.Fatalf("first retry reads %v", m)
	}
	if _, f, _ := e.do(t, "task_bump_counter", `{"id":"`+mine.ID+`","counter":"nonexistent"}`); f == nil {
		t.Fatal("an off-vocabulary counter bumped — the whitelist is the door")
	}
}

// TestAGrantedAgentCreatesALoopTask is the criterion-1 walk across every seam
// an agent call crosses: BuildTools' grant list, the toolset's gate, the
// projected tool, the injected Records door, Identify's stamping and the
// store's row. The MODEL turn is not replayed here (runtime/protocol own the
// model lane and its replay fixtures, recorded honestly as the gap); every
// other door between a granted handler name and a row is walked for real.
func TestAGrantedAgentCreatesALoopTask(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	r := StepRunner{Records: e.svc.Invoker()}
	caller, _, err := r.BuildTools([]string{"read", "task_create"}, "/repo/alpha", e.agent, runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build granted: %v", err)
	}
	out := caller.Call(e.ctx, "task_create", json.RawMessage(`{"title":"from the lane","tech":"go","depends_on":[]}`))
	if out.IsError {
		t.Fatalf("granted agent refused: %s", out.Content)
	}
	row, err := e.s.GetTask(store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}, "T1")
	if err != nil {
		t.Fatalf("the row is not there: %v — content was %s", err, out.Content)
	}
	if row.Title != "from the lane" || row.Engineer != e.agent {
		t.Fatalf("row arrived ungrounded: %+v", row)
	}
	// The same lane ungranted: the name is not there to call.
	caller2, specs, err := r.BuildTools([]string{"read"}, "/repo/alpha", e.agent, runtime.FrozenManifest{})
	if err != nil {
		t.Fatalf("build ungranted: %v", err)
	}
	for _, sp := range specs {
		if sp.Name == "task_create" {
			t.Fatal("task_create was OFFERED to an ungranted step — the grant list is decoration")
		}
	}
	res := caller2.Call(e.ctx, "task_create", json.RawMessage(`{"title":"sneaked"}`))
	if !res.IsError {
		t.Fatalf("an ungranted call ran: %s", res.Content)
	}
	if _, err := e.s.GetTask(store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}, "T2"); err == nil {
		t.Fatal("the ungranted call filed a row anyway")
	}
}

// The brainstormer filed goals against "AGENTS.md", "hello" and a file path and
// every one was refused: it had no way to learn what a stake may name. The root
// answers by its own name too (a workspace with no children has only it), and a
// refusal says what would have been accepted.
func TestGoalCreateAcceptsTheRootByNameAndAnswersARefusalWithTheChoices(t *testing.T) {
	e := newRecordsEnv(t, "")
	out, f, err := e.do(t, "goal_create", `{"title":"root stake","type":"chore","components":[{"component":"org group"}]}`)
	if err != nil || f != nil {
		t.Fatalf("the root's own name must be a valid stake: %v %+v", err, f)
	}
	if g := out.(map[string]any)["goal"].(store.Goal); len(g.Components) != 1 || g.Components[0].ComponentID != e.proj {
		t.Fatalf("stake must be the root: %+v", g.Components)
	}
	_, f, err = e.do(t, "goal_create", `{"title":"guess","type":"chore","components":[{"component":"AGENTS.md"}]}`)
	if err != nil || f == nil {
		t.Fatalf("a file name is not a component: %v %+v", err, f)
	}
	for _, want := range []string{"not in this root", `"org group" (id ` + e.proj + `)`, `"alpha" (id ` + e.comp.ID + `)`} {
		if !strings.Contains(f.Sentence, want) {
			t.Fatalf("the refusal must name the valid choices, missing %q: %s", want, f.Sentence)
		}
	}
}
