package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// S75 T2/T3: the todo service over a real store, the way a step reaches it.

func todoFixture(t *testing.T) (*TodoService, store.Scope, []int64, *[]string) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	if _, err := st.CreateLoop(store.NewLoop{Slug: "chat-1", ComponentRoot: root, Root: root, Goal: "talk", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "chat-1", ComponentRoot: root}
	var steps []int64
	for i := 0; i < 2; i++ {
		s, err := st.CreateStep(sc, store.NewStep{Agent: "brainstormer", Phase: "converse", Status: store.StepRunning})
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, s.ID)
	}
	svc := NewTodoService(st)
	var mu sync.Mutex
	var kinds []string
	svc.publish = func(kind, _ string, _ any) { mu.Lock(); kinds = append(kinds, kind); mu.Unlock() }
	return svc, sc, steps, &kinds
}

func TestASecondStepOfTheLoopSeesTheFirstStepsListThroughTheTool(t *testing.T) {
	svc, _, steps, kinds := todoFixture(t)
	toolsetFor := func(step int64) (tools.Toolset, context.Context) {
		set, err := tools.BuildToolset([]string{"todo"}, tools.ToolContext{WorkspaceRoot: t.TempDir(), Todo: svc})
		if err != nil {
			t.Fatal(err)
		}
		return set, stepctx.WithStepID(context.Background(), step)
	}
	first, ctx1 := toolsetFor(steps[0])
	res := first.Call(ctx1, "todo", json.RawMessage(`{"action":"set","items":[{"id":"a","content":"read the repo","status":"done"},{"id":"b","content":"ask scope","status":"in_progress"}]}`))
	if res.IsError {
		t.Fatalf("set refused: %s", res.Content)
	}
	second, ctx2 := toolsetFor(steps[1])
	back := second.Call(ctx2, "todo", json.RawMessage(`{"action":"list"}`))
	if back.IsError || !strings.Contains(back.Content, `"b": ask scope`) || !strings.Contains(back.Content, "1 in progress") {
		t.Fatalf("the second step did not see the first step's list: %s", back.Content)
	}
	if len(*kinds) != 1 || (*kinds)[0] != protocol.KindTodoUpdated {
		t.Fatalf("a write must publish todo.updated exactly once, got %v", *kinds)
	}
	// Finishing the list closes it, and the result says so.
	done := second.Call(ctx2, "todo", json.RawMessage(`{"action":"set","items":[{"id":"a","content":"read the repo","status":"done"},{"id":"b","content":"ask scope","status":"done"}]}`))
	if !strings.Contains(done.Content, "List closed: all items done.") {
		t.Fatalf("closing must be said: %s", done.Content)
	}
}

func TestThePersonClosesAndTheReadSaysSo(t *testing.T) {
	svc, sc, steps, _ := todoFixture(t)
	ctx := stepctx.WithStepID(context.Background(), steps[0])
	if _, _, err := svc.Replace(ctx, 0, []tools.TodoEntry{{ID: "a", Content: "x", Status: "pending"}}); err != nil {
		t.Fatal(err)
	}
	open, ok, err := svc.ActiveOpen(context.Background(), sc)
	if err != nil || !ok || len(open) != 1 || open[0] != "a" {
		t.Fatalf("active open: %v ok=%v err=%v", open, ok, err)
	}
	res, changed, err := svc.Close(context.Background(), sc)
	if err != nil || !changed || res.Active || res.ClosedBy != store.TodoClosedPerson {
		t.Fatalf("close: %+v changed=%v err=%v", res, changed, err)
	}
	if _, ok, _ := svc.ActiveOpen(context.Background(), sc); ok {
		t.Fatal("a closed list is not active")
	}
	read, err := svc.Read(context.Background(), sc)
	if err != nil || read.Active || read.Counts.Pending != 1 {
		t.Fatalf("read after close: %+v err=%v", read, err)
	}
}

func TestALoopThatNeverHadAListAnswersTheDaemonsStatement(t *testing.T) {
	svc, sc, _, _ := todoFixture(t)
	res, err := svc.Read(context.Background(), sc)
	if err != nil || res.Active || len(res.Items) != 0 || res.Statement != protocol.TodoNoListStatement {
		t.Fatalf("never had a list: %+v err=%v", res, err)
	}
}

func TestTheQuestionBlockOfAParkedStepComesBackWithItsAnswers(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	l, err := st.CreateLoop(store.NewLoop{Slug: "chat-q", ComponentRoot: root, Root: root, Goal: "talk", Status: "waiting"})
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "chat-q", ComponentRoot: root}
	step, err := st.CreateStep(sc, store.NewStep{Agent: "brainstormer", Phase: "converse", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	sid := step.ID
	first, err := st.Ask(sc, store.NewQuestion{StepID: &sid, Kind: "clarify", Body: "Which scope?", Options: []string{"root", "scoped"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Answer(first.ID, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ask(sc, store.NewQuestion{StepID: &sid, Kind: "clarify", Body: "Which tone?"}); err != nil {
		t.Fatal(err)
	}
	res, err := loopQuestionsAdapter{st: st}.QuestionBlock(context.Background(), l.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Questions) != 2 || res.Questions[0].Answer != "root" || res.Questions[1].Answer != "" || res.Questions[1].Prompt != "Which tone?" {
		t.Fatalf("the block must hold the answered and the pending question in order: %+v", res.Questions)
	}
}

func TestAllQuestionsOfAFinishedLoopComeBackWithTheirAnswersAndCalls(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	l, err := st.CreateLoop(store.NewLoop{Slug: "chat-all", ComponentRoot: root, Root: root, Goal: "talk", Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "chat-all", ComponentRoot: root}
	for _, tool := range []string{"toolu_1", "toolu_2"} {
		step, err := st.CreateStep(sc, store.NewStep{Agent: "brainstormer", Phase: "converse", Status: store.StepRunning})
		if err != nil {
			t.Fatal(err)
		}
		sid := step.ID
		q, err := st.Ask(sc, store.NewQuestion{StepID: &sid, ToolUseID: tool, Kind: "clarify", Body: "Which " + tool + "?"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Answer(q.ID, "ans-"+tool); err != nil {
			t.Fatal(err)
		}
	}
	res, err := loopQuestionsAdapter{st: st}.QuestionBlock(context.Background(), l.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Questions) != 2 || res.Questions[0].ToolUseID != "toolu_1" || res.Questions[1].Answer != "ans-toolu_2" || res.Questions[0].StepID == res.Questions[1].StepID {
		t.Fatalf("every step's question must come back with its call and answer: %+v", res.Questions)
	}
	latest, err := loopQuestionsAdapter{st: st}.QuestionBlock(context.Background(), l.ID, false)
	if err != nil || len(latest.Questions) != 0 {
		t.Fatalf("the default read stays the parked block only: %+v %v", latest, err)
	}
}
