package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/store"
)

type nopLog struct{}

func (nopLog) Infof(string, ...any) {}

func loopContextFixture(t *testing.T) (*store.Store, store.Scope, store.Step) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateLoop(store.NewLoop{Slug: "qa", ComponentRoot: "/r", Title: "Greeting CLI", Goal: "g"}); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "qa", ComponentRoot: "/r"}
	step, err := s.CreateStep(sc, store.NewStep{Phase: "discovery", Agent: "investigator", Attempt: 1, Status: store.StepRunning, Source: "live"})
	if err != nil {
		t.Fatal(err)
	}
	return s, sc, step
}

// An agent starts knowing what the loop holds and where: the documents in the database, the tasks with their spec
// files, the answers the person gave, and the tools it holds.
func TestTheLoopIndexNamesWhatTheLoopHolds(t *testing.T) {
	s, sc, _ := loopContextFixture(t)
	if _, err := s.WriteDocument(sc, "prompt.md", "# brief", "intent-clarifier"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDocument(sc, "tasks/T1.md", "spec", "project-planner"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(sc, store.NewTask{TaskID: "T1", Ord: 1, Title: "greeting core", Tech: "go", Engineer: "golang-engineer", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	stepID := int64(1)
	q, err := s.Ask(sc, store.NewQuestion{StepID: &stepID, Kind: store.QuestionClarify, Body: "Which languages?", Options: []string{"en", "pt"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(q.ID, "English and Portuguese"); err != nil {
		t.Fatal(err)
	}

	out := NewLoopContextService(s, nil).RenderFor(context.Background(), "qa", "/r", "project-planner", []string{"doc_read", "read", "loop_context"})
	for _, want := range []string{
		"in the database, not on disk", "prompt.md · rev 1", "tasks/T1.md",
		"T1 · pending · golang-engineer", "spec: tasks/T1.md",
		"Q: Which languages?", "A: English and Portuguese",
		"doc_read —", "loop_context —", "Greeting CLI",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the index is missing %q:\n%s", want, out)
		}
	}
	if again := NewLoopContextService(s, nil).RenderFor(context.Background(), "qa", "/r", "project-planner", []string{"doc_read", "read", "loop_context"}); again != out {
		t.Error("the index is not deterministic: the same state must give the same bytes")
	}
	if NewLoopContextService(s, nil).RenderFor(context.Background(), "", "/r", "x", nil) != "" {
		t.Error("a step with no loop gets no index")
	}
}

// The person's answers are filed as the loop's own document the moment they are given, so a later agent finds them
// in the database; a permission question is not an answer to a design question and stays out; a rebuild that changes
// nothing writes nothing.
func TestAnswersAreFiledAsADocument(t *testing.T) {
	s, sc, step := loopContextFixture(t)
	q, err := s.Ask(sc, store.NewQuestion{StepID: &step.ID, Kind: store.QuestionClarify, Body: "History file where?", Options: []string{"config dir"}})
	if err != nil {
		t.Fatal(err)
	}
	perm, err := s.Ask(sc, store.NewQuestion{StepID: &step.ID, ToolUseID: "t1", Kind: store.QuestionPermission, Body: "Allow bash?", Options: []string{"approve"}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Answer(perm.ID, "approve")
	if _, err := s.Answer(q.ID, "In os.UserConfigDir"); err != nil {
		t.Fatal(err)
	}
	fileLoopAnswers(s, step.ID, nopLog{})

	doc, err := s.ReadDocument(sc, "answers.md")
	if err != nil {
		t.Fatalf("answers.md was not filed: %v", err)
	}
	for _, want := range []string{"History file where?", "In os.UserConfigDir", "investigator (discovery)"} {
		if !strings.Contains(doc.Content, want) {
			t.Errorf("answers.md is missing %q:\n%s", want, doc.Content)
		}
	}
	if strings.Contains(doc.Content, "Allow bash?") {
		t.Error("a permission question was filed as a decision")
	}
	fileLoopAnswers(s, step.ID, nopLog{})
	again, _ := s.ReadDocument(sc, "answers.md")
	if again.Rev != doc.Rev {
		t.Errorf("an unchanged rebuild wrote revision %d over %d", again.Rev, doc.Rev)
	}
}

func TestReadFirstFollowsTheBlueprintAndTheTask(t *testing.T) {
	have := map[string]bool{"prompt.md": true, "answers.md": true, "discovery.md": true, "plan.md": true, "tasks/T1.md": true, "tasks/T2.md": true, "legal.md": false}
	lanes := []blueprint.Lane{
		{ID: "clarify", Step: &blueprint.Step{Produces: blueprint.Paths{"prompt.md"}}},
		{ID: "discovery", Step: &blueprint.Step{Produces: blueprint.Paths{"discovery.md"}}},
		{ID: "legal", Step: &blueprint.Step{Produces: blueprint.Paths{"legal.md"}}},
		{ID: "plan", Step: &blueprint.Step{Produces: blueprint.Paths{"plan.md", "tasks/"}}},
		{ID: "build", Ladder: []blueprint.Step{{Produces: blueprint.Paths{"security.md"}}}},
	}
	plan := readFirstFrom(have, lanes, "plan", "")
	if got := names(plan); !sameStrings(got, []string{"prompt.md", "answers.md", "discovery.md"}) {
		t.Errorf("the planner reads the brief, the answers and the discovery (legal.md does not exist): %v", got)
	}
	build := readFirstFrom(have, lanes, "build", "T2")
	if got := names(build); !sameStrings(got, []string{"prompt.md", "answers.md", "discovery.md", "plan.md", "tasks/T1.md", "tasks/T2.md"}) {
		t.Errorf("an engineer reads everything before it and its own task spec: %v", got)
	}
	if !strings.Contains(build[len(build)-1], "THIS task's spec") {
		t.Errorf("the task's own spec is marked as its own: %v", build)
	}
	if clarify := readFirstFrom(map[string]bool{}, lanes, "clarify", ""); len(clarify) != 0 {
		t.Errorf("the first step has nothing to read yet: %v", clarify)
	}
}

func names(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.SplitN(l, " — ", 2)[0]
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A gate the person answered (the plan gate's revise) is a decision of the loop too, and is filed beside the answers
// to the agents' questions.
func TestGateAnswersAreFiledToo(t *testing.T) {
	s, sc, step := loopContextFixture(t)
	lp, _ := s.GetLoop(sc)
	gate, err := s.Ask(sc, store.NewQuestion{Kind: store.QuestionGate, Body: "Approve the plan and start building?", Options: []string{"approve", "revise", "abort"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(gate.ID, "revise"); err != nil {
		t.Fatal(err)
	}
	_ = step
	fileGateAnswers(s, lp.ID, "revise", "Split T3 in two and drop the history flag.", nopLog{})
	doc, err := s.ReadDocument(sc, "answers.md")
	if err != nil {
		t.Fatalf("the gate answer was not filed: %v", err)
	}
	for _, want := range []string{"Approve the plan and start building?", "revise", "the blueprint (a gate)", "Revision request 1 (revise)", "Split T3 in two and drop the history flag."} {
		if !strings.Contains(doc.Content, want) {
			t.Errorf("answers.md is missing %q:\n%s", want, doc.Content)
		}
	}
}

// The options a person chose when starting the loop ride every model step of it.
func TestLoopModelOptionsReachEveryStep(t *testing.T) {
	var st loop.LoopState
	st.Route, st.ReasoningEffort, st.Thinking = "qwen", "low", "off"
	p := stepParamsFor(st, blueprint.Step{Agent: "investigator"}, "go", "")
	if p.ReasoningEffort != "low" || p.Thinking != "off" {
		t.Fatalf("the step lost the loop's model options: %+v", p)
	}
	var plain loop.LoopState
	q := stepParamsFor(plain, blueprint.Step{Agent: "investigator"}, "go", "")
	if q.ReasoningEffort != "" || q.Thinking != "" {
		t.Errorf("a loop with no choice must leave the route's own: %+v", q)
	}
}
