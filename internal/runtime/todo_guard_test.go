package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S75 T5: the nudge and the completion guard, with a scripted model.

type fakeTodoPolicy struct {
	open  []string
	nudge int
	guard bool
	asked int
	list  string
}

func (f *fakeTodoPolicy) ActiveOpen(context.Context) ([]string, bool) {
	f.asked++
	return f.open, f.open != nil
}
func (f *fakeTodoPolicy) ActiveList(context.Context) (string, bool) { return f.list, f.list != "" }
func (f *fakeTodoPolicy) NudgeAfter() int                           { return f.nudge }
func (f *fakeTodoPolicy) GuardApplies() bool                        { return f.guard }

func todoSpecs() []model.ToolSpec { return []model.ToolSpec{{Name: "read"}, {Name: "todo"}} }

func runTodoLoop(t *testing.T, policy TodoPolicy, turns ...scriptedTurn) (StepResult, *fakeClient, *recordingSink) {
	t.Helper()
	client := newFakeClient(turns...)
	sink := &recordingSink{}
	st := testState(testHandler(), todoSpecs())
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read", "todo"), Sink: sink,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Checkpoint:  &recordingWriter{}, Todo: policy,
	})
	return res, client, sink
}

func readTurn(id string) scriptedTurn {
	return scriptedTurn{ToolUses: []model.Block{toolUse(id, "read", `{"path":"a.md"}`)}}
}

func TestTheNudgeFiresOncePerWindowAndOnlyWithOpenItems(t *testing.T) {
	policy := &fakeTodoPolicy{open: []string{"scope", "write"}, nudge: 2}
	res, client, sink := runTodoLoop(t, policy,
		readTurn("t1"), readTurn("t2"), readTurn("t3"), readTurn("t4"), readTurn("t5"),
		scriptedTurn{Text: okHandoff})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %q reason %q", res.Outcome, res.Reason)
	}
	n := 0
	for _, e := range eventsOf(sink.all(), EventTodoNudged) {
		if e.Reason == "stale" {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("expected the nudge to repeat once per window, got %d", n)
	}
	reminders := 0
	for i := range client.requests {
		reminders = max(reminders, countContaining(client.request(i).Messages, "Reminder: your todo list has 2 open item(s) (scope, write)"))
	}
	if reminders < 2 {
		t.Fatalf("the reminder text must be in the transcript the model reads, found %d", reminders)
	}
}

func TestATodoCallRestartsTheNudgeWindow(t *testing.T) {
	policy := &fakeTodoPolicy{open: []string{"a"}, nudge: 3}
	todoTurn := scriptedTurn{ToolUses: []model.Block{toolUse("td", "todo", `{"action":"list"}`)}}
	_, _, sink := runTodoLoop(t, policy,
		readTurn("t1"), readTurn("t2"), todoTurn, readTurn("t3"), readTurn("t4"),
		scriptedTurn{Text: okHandoff})
	if n := countEvents(sink.all(), EventTodoNudged); n != 0 {
		t.Fatalf("a todo call inside the window must prevent the nudge, got %d", n)
	}
}

func TestNoNudgeWhenTheListIsClosedOrTheSettingIsOff(t *testing.T) {
	for name, policy := range map[string]*fakeTodoPolicy{
		"no active list": {open: nil, nudge: 1},
		"setting off":    {open: []string{"a"}, nudge: 0},
	} {
		_, _, sink := runTodoLoop(t, policy, readTurn("t1"), readTurn("t2"), readTurn("t3"), scriptedTurn{Text: okHandoff})
		if n := countEvents(sink.all(), EventTodoNudged); n != 0 {
			t.Errorf("%s: %d nudges, want none", name, n)
		}
	}
}

func TestTheGuardSendsAnOkStepBackTwiceThenCompletesAndSaysSo(t *testing.T) {
	policy := &fakeTodoPolicy{open: []string{"b"}, nudge: 0, guard: true}
	res, client, sink := runTodoLoop(t, policy,
		scriptedTurn{Text: okHandoff}, scriptedTurn{Text: okHandoff}, scriptedTurn{Text: okHandoff})
	if res.Outcome != OutcomeOK {
		t.Fatalf("the third attempt completes as asked: %q %q", res.Outcome, res.Reason)
	}
	if client.calls != 3 {
		t.Fatalf("the model must be called three times (two bounces), was %d", client.calls)
	}
	var completion, closed int
	for _, e := range eventsOf(sink.all(), EventTodoNudged) {
		switch e.Reason {
		case "completion":
			completion++
		case "closed-with-open":
			closed++
		}
	}
	if completion != 2 || closed != 1 {
		t.Fatalf("want 2 completion reminders and 1 closed-with-open fact, got %d and %d", completion, closed)
	}
	if n := countContaining(client.request(2).Messages, "Before you finish: your todo list still has 1 open item(s) (b)"); n != 2 {
		t.Fatalf("the completion reminder must appear twice in the final request, found %d", n)
	}
	_ = strings.TrimSpace
}

func TestTheGuardIgnoresExemptStepsAndHonestStops(t *testing.T) {
	// A converse lane: GuardApplies false.
	exempt := &fakeTodoPolicy{open: []string{"a"}, guard: false}
	_, client, _ := runTodoLoop(t, exempt, scriptedTurn{Text: okHandoff})
	if client.calls != 1 {
		t.Fatalf("an exempt step must finish at once, calls=%d", client.calls)
	}
	// A blocked handoff needs no reminder.
	guarded := &fakeTodoPolicy{open: []string{"a"}, guard: true}
	res, client, _ := runTodoLoop(t, guarded, scriptedTurn{Text: `{"status":"blocked","summary":"cannot","open_questions":"x"}`})
	if client.calls != 1 || res.Outcome == OutcomeOK {
		t.Fatalf("a blocked stop must not be guarded: calls=%d outcome=%q", client.calls, res.Outcome)
	}
}

func TestTheListIsRestoredAfterACompactionFoldsItsCallAway(t *testing.T) {
	// A compaction mid-step takes the todo call with the earlier turns; the
	// runtime puts the list back so the agent continues it (S75 TD7).
	policy := &fakeTodoPolicy{list: "[~] \"a\": read the repo\n0 pending, 1 in progress, 0 done"}
	got := TodoAfterCompactionText(policy.list)
	if !strings.Contains(got, `"a": read the repo`) || !strings.Contains(got, TodoRuleSentence) || !strings.Contains(got, "compacted") {
		t.Fatalf("the restore text must carry the list and the rule: %q", got)
	}
}
