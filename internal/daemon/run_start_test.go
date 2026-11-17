package daemon

import (
	"context"
	"encoding/json"
	"github.com/cjlapao/aeon/internal/runtime"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// run_start_test.go walks S39 T10: the ONE loop action an agent may take.
// The ask that gates it is OPERATOR data — nothing here seeds a policy row
// behind the daemon's back; the rows below are written through the real
// boundary the `aeon policy` verb opens (S11's "nothing seeds automatically").
// The loop a run starts in is the STEP's loop; the tool input cannot name one.

const runHandoff = "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```"

// askRunStart dispatches one chat-agent step that calls run_start on the
// blueprint path, against a rested loop declared as attended says. rows are
// the operator's policy rows, seeded BEFORE the dispatch (a step is judged
// by the set that exists when the block is blocked).
func askRunStart(t *testing.T, attended *bool, turns []gatedTurn, rows ...policy.Policy) (*streamFixture, *gatedModel, protocol.StepResult, string) {
	t.Helper()
	return askRunStartHook(t, attended, turns, nil, rows...)
}

// askRunStartHook is the full door: hook runs after the org exists and
// BEFORE the dispatch, so a test can put an approved goal (or anything else
// the blueprint's requires clause will judge) into the store the gate and
// the run door will read.
func askRunStartHook(t *testing.T, attended *bool, turns []gatedTurn, hook func(t *testing.T, f *streamFixture, projID string), rows ...policy.Policy) (*streamFixture, *gatedModel, protocol.StepResult, string) {
	t.Helper()
	f, m := gatedFixture(t, turns, rows...)
	projID, _ := runOrg(t, f)
	initConn(t, f.d, context.Background(), "c-rs", "ui")
	if hook != nil {
		hook(t, f, projID)
	}
	l := restedLoopWith(t, f, "chat", attended)
	raw, err := json.Marshal(protocol.StepParams{
		// No Tools list: an agent step takes its grants from the handler
		// registry — chat's front-matter, which T10 widened with run_start.
		// The input could not widen it even to name the smuggled loop.
		Prompt: "start the blueprint", Agent: "chat",
		WorkspaceRoot: f.root, LoopSlug: "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.d.Table.Dispatch(context.Background(), "c-rs", protocol.MethodStep, raw)
	if err != nil {
		t.Fatalf("dispatch step: %v", err)
	}
	sr, ok := res.(protocol.StepResult)
	if !ok {
		t.Fatalf("dispatch returned %T", res)
	}
	return f, m, sr, l.ID
}

// runOrg puts a project and a component registered at the fixture root —
// where candidates resolve and loops live.
func runOrg(t *testing.T, f *streamFixture) (string, string) {
	t.Helper()
	// A "project" is a root component (S44-extra); alpha is a child of it.
	pr, err := f.s.CreateComponent(store.NewComponent{Name: "run_start group", Slug: "run_start-group", Root: "/repo/run_start-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha", Root: f.root, ParentID: pr.Component.ID}); err != nil {
		t.Fatal(err)
	}
	return pr.Component.ID, "alpha"
}

// settleRunner is the T8/T9 crash lesson made callable: a run started inside
// a test hands its runner goroutine to the test, and t.Cleanup closes the
// store under it if the test leaves early — the panic lands on a LATER test.
// Every gated run defers this the moment the fixture exists: release once,
// then wait for the runner to be idle.
func settleRunner(t *testing.T, f *streamFixture, id string, release chan struct{}) {
	t.Helper()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		waitLoopSettled(t, f, id)
	})
}

func runStartToolUse(bp string, extra string) model.Block {
	return toolUse("b1", "run_start", `{"blueprint":"`+bp+`"`+extra+`}`)
}

// toolResultB1 reads the b1 tool_result out of the step's stored transcript.
func toolResultB1(t *testing.T, f *streamFixture, stepID int64) (string, bool) {
	t.Helper()
	tr := f.s.ReadTranscript(stepID)
	for _, msg := range tr.Messages {
		for _, b := range msg.Content {
			if b.Kind == model.BlockToolResult && b.ToolUseID == "b1" {
				return b.Content, b.IsError
			}
		}
	}
	return "", false
}

func TestRunStartHasNoRowByDefaultAndIsDenied(t *testing.T) {
	attended := true // even an attended loop denies: there is no row to ask about (PL5)
	bp := runBlueprint(t, "second", "")
	f, _, sr, id := askRunStart(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp, "")}},
		{deltas: []string{runHandoff}},
	})
	defer waitLoopSettled(t, f, id)
	if sr.Kind == "suspended" {
		t.Fatalf("a tool nobody granted produced a question: %+v", sr)
	}
	if _, found, err := f.s.PendingStepQuestion(sr.StepID); err != nil || found {
		t.Fatal("PL5: no row means NO question, not an unanswered one")
	}
	content, isErr := toolResultB1(t, f, sr.StepID)
	want := policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: "chat", Tool: "run_start"}, time.Now().UTC()).Sentence()
	if !isErr || content != want {
		t.Fatalf("tool_result %q (err %v), want PL5's default-deny sentence verbatim %q", content, isErr, want)
	}
	runs, _ := f.s.ListRuns(id)
	if len(runs) != 1 {
		t.Fatalf("an ungranted run_start wrote run rows: %d", len(runs))
	}
}

func TestRunStartAskOnAnAttendedLoopSuspendsWithAPermissionQuestion(t *testing.T) {
	attended := true
	bp := runBlueprint(t, "second", "")
	// turn[0] is the step's tool call; the step never reaches its handoff —
	// the ask suspends it. No turn[1]: a handoff would be a lie about reach.
	f, _, sr, id := askRunStart(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp, "")}},
	}, toolRulePolicy("ask-run-start", "chat", "run_start", policy.Ask))
	if sr.Kind != "suspended" || sr.Question == nil {
		t.Fatalf("attended + ask row did not suspend: %+v", sr)
	}
	if sr.Question.Kind != "permission" {
		t.Fatalf("question kind %q, want permission", sr.Question.Kind)
	}
	if !reflect.DeepEqual(sr.Question.Options, runtime.PermissionOptions()) {
		t.Fatalf("options %v, want the five permission answers %v", sr.Question.Options, runtime.PermissionOptions())
	}
	if !strings.Contains(sr.Question.Body, "run_start") || !strings.Contains(sr.Question.Body, "chat") {
		t.Fatalf("the body must name the tool and the handler (SEC-051): %q", sr.Question.Body)
	}
	if _, found, err := f.s.PendingStepQuestion(sr.StepID); err != nil || !found {
		t.Fatalf("the ask must be visibly ASKED (SEC-049): found=%v err=%v", found, err)
	}
	runs, _ := f.s.ListRuns(id)
	if len(runs) != 1 {
		t.Fatalf("the ask started a run before the answer: %d rows (CR5)", len(runs))
	}
}

func TestApprovingStartsTheRun(t *testing.T) {
	// Two halves, and the first is the S11 finding (DEMO F-21) carried to the
	// run door: answering `approve` delivers the answer to the MODEL as the
	// blocked call's tool_result — it mints no run. What STARTS the run is
	// the call reaching the tool, and the call reaches the tool when the
	// gate's frozen set says allow (the ask row outranks any allow and asks
	// again — the Do-NOT forbids touching the gate, so the table's "approve
	// starts the run" is recorded as a spec contradiction in Notes, measured
	// against S11's own walked behavior, and both truths are pinned here).
	attended := true
	bp := runBlueprint(t, "second", "")
	release := make(chan struct{})
	f, _, sr, id := askRunStart(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp, "")}}, // the step's call — asked
		{deltas: []string{runHandoff}},                     // after "approve" the model speaks
	}, toolRulePolicy("ask-run-start", "chat", "run_start", policy.Ask))
	if sr.Kind != "suspended" {
		t.Fatalf("no suspension: %+v", sr)
	}
	answer, err := json.Marshal(protocol.AnswerParams{StepID: sr.StepID, Answer: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Table.Dispatch(context.Background(), "c-rs", protocol.MethodAnswer, answer); err != nil {
		t.Fatalf("answer approve: %v", err)
	}
	// 2026-11-15 (the permissions amendment): F-21's gap is closed. Answering approve now RUNS the blocked
	// call, once, through the ordinary dispatch — so the run starts, and the model reads the tool's own
	// result rather than the word "approve".
	waitFor(t, "the approved call to run and its result to reach the model", func() bool {
		content, _ := toolResultB1(t, f, sr.StepID)
		return content != "" && content != "approve"
	})
	waitFor(t, "the approved call to start the run", func() bool {
		runs, err := f.s.ListRuns(id)
		return err == nil && len(runs) == 2
	})
	waitLoopSettled(t, f, id)
	close(release) // never gated in this half; closing it keeps the deferred close honest

	// The second half: with an ALLOW row the same call runs — and the run it
	// starts carries the STEP's identity: agent/chat in the loop of the step.
	bp2 := runBlueprint(t, "second", "")
	release2 := make(chan struct{})
	// The subscription must exist BEFORE the dispatch: with an allow row the
	// tool runs INSIDE the step dispatch, and the run it starts publishes
	// loop.run.started then — a subscriber of the moment would miss the beat.
	var sub protocol.SubscribeResult
	f2, _, sr2, id2 := askRunStartHook(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp2, "")}},
		{deltas: []string{runHandoff}},
		{deltas: []string{runHandoff}, release: release2}, // run 2's gated lane
	}, func(t *testing.T, f *streamFixture, projID string) {
		sub = subscribe(t, f.d, "c-rs", protocol.SubscribeParams{})
	}, toolRulePolicy("allow-run-start", "chat", "run_start", policy.Allow))
	settleRunner(t, f2, id2, release2)
	if sr2.Kind == "suspended" {
		t.Fatalf("an allow row asked: %+v", sr2)
	}
	waitFor(t, "the allowed call to start the run", func() bool {
		runs, err := f2.s.ListRuns(id2)
		return err == nil && len(runs) == 2
	})
	runs2, _ := f2.s.ListRuns(id2)
	r2 := runs2[1]
	if r2.StartedByKind != "agent" || r2.StartedByID != "chat" {
		t.Fatalf("run 2 provenance %q/%q, want agent/chat — the identity came from the step, not the input", r2.StartedByKind, r2.StartedByID)
	}
	pin, _ := f2.s.ReadPinnedLoop(id2)
	if pin.Loop.Status != "running" {
		t.Fatalf("the started run left the loop %q while its gated lane holds it open", pin.Loop.Status)
	}
	var started int
	for _, e := range streamEvents(f2.w, "c-rs", sub.SubscriptionID) {
		if e.Kind == "loop.run.started" {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("loop.run.started fired %d times, want the run announced once", started)
	}
	close(release2)
}

func TestDenyingReturnsToTheModelAndStartsNothing(t *testing.T) {
	// S6's answer path answers the blocked block whatever the verb — the
	// model is TOLD "deny" and must not be handed a run either way.
	attended := true
	bp := runBlueprint(t, "second", "")
	f, _, sr, id := askRunStart(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp, "")}},
		{deltas: []string{runHandoff}}, // after the deny, the step hands off
	}, toolRulePolicy("ask-run-start", "chat", "run_start", policy.Ask))
	if sr.Kind != "suspended" {
		t.Fatalf("no suspension: %+v", sr)
	}
	answer, err := json.Marshal(protocol.AnswerParams{StepID: sr.StepID, Answer: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Table.Dispatch(context.Background(), "c-rs", protocol.MethodAnswer, answer); err != nil {
		t.Fatalf("answer deny: %v", err)
	}
	waitFor(t, "the denied answer to come back to the model", func() bool {
		content, _ := toolResultB1(t, f, sr.StepID)
		return strings.Contains(content, "a person denied this call")
	})
	runs, _ := f.s.ListRuns(id)
	if len(runs) != 1 {
		t.Fatalf("a denied start still started: %d rows", len(runs))
	}
	waitLoopSettled(t, f, id)
}

func TestRunStartIsDeniedAtOnceOnAnUnattendedLoop(t *testing.T) {
	no := false
	for _, tc := range []struct {
		name     string
		attended *bool
	}{{"declared-false", &no}, {"never-declared", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			bp := runBlueprint(t, "second", "")
			f, _, sr, id := askRunStart(t, tc.attended, []gatedTurn{
				{toolUses: []model.Block{runStartToolUse(bp, "")}},
				{deltas: []string{runHandoff}},
			}, toolRulePolicy("ask-run-start", "chat", "run_start", policy.Ask))
			defer waitLoopSettled(t, f, id)
			if sr.Kind == "suspended" {
				t.Fatalf("D5/CR7: an unattended loop waited for an answerer that does not exist: %+v", sr)
			}
			if _, found, err := f.s.PendingStepQuestion(sr.StepID); err != nil || found {
				t.Fatal("no question row may exist for a nobody-can-answer ask")
			}
			// The verdict sentence VERBATIM from the same evaluation the gate
			// ran (runtime/loop.go's deny-now branch returns dec.Sentence):
			// the model is told WHY, and the operator's row governs the words.
			want := policy.Evaluate([]policy.Policy{toolRulePolicy("ask-run-start", "chat", "run_start", policy.Ask)},
				policy.Request{Type: policy.ToolAccess, Agent: "chat", Tool: "run_start"}, time.Now().UTC()).Sentence()
			content, isErr := toolResultB1(t, f, sr.StepID)
			if !isErr || content != want {
				t.Fatalf("tool_result %q (err %v), want the policy sentence verbatim %q", content, isErr, want)
			}
			runs, _ := f.s.ListRuns(id)
			if len(runs) != 1 {
				t.Fatalf("denied-at-once still started a run: %d rows", len(runs))
			}
		})
	}
}

func TestAHumanChattingIntoAnUnattendedLoopDoesNotMakeItAttended(t *testing.T) {
	no := false
	bp := runBlueprint(t, "second", "")
	// turn[0] serves the CHAT turn the message starts; turn[1] is the later
	// step's tool call; turn[2] hands the step off after the denial.
	f, _ := gatedFixture(t, []gatedTurn{
		{deltas: []string{runHandoff}},                     // the conversation turn
		{toolUses: []model.Block{runStartToolUse(bp, "")}}, // the later step's call
		{deltas: []string{runHandoff}},                     // the step's handoff
	})
	runOrg(t, f)
	initConn(t, f.d, context.Background(), "c-rs", "ui")
	l := restedLoopWith(t, f, "chat", &no)
	// A person chats IN — a message through the human door.
	res := dispatchJSON(t, f, "c-rs", protocol.MethodLoopMessage, map[string]any{"id": l.ID, "text": "hello?"})
	if res["turn"] != "started" {
		t.Fatalf("the chat did not start a turn: %v", res)
	}
	waitTurnIdle(t, f.d, l.ID)
	seedRules(t, f.s, time.Now().UTC(), toolRulePolicy("ask-run-start", "chat", "run_start", policy.Ask))
	sr := dispatchStepRunStart(t, f, bp)
	if sr.Kind == "suspended" {
		t.Fatalf("CR7: chatting in made somebody present to answer: %+v", sr)
	}
	if _, found, _ := f.s.PendingStepQuestion(sr.StepID); found {
		t.Fatal("CR7: a question row appeared because a human once typed — attendedness moved without a declaration")
	}
	pin, _ := f.s.ReadPinnedLoop(l.ID)
	if pin.Attended == nil || *pin.Attended {
		t.Fatalf("CR7: the human message moved attendedness to %v", pin.Attended)
	}
	runs, _ := f.s.ListRuns(l.ID)
	if len(runs) != 1 {
		t.Fatalf("the unattended step started a run anyway: %d rows", len(runs))
	}
	waitLoopSettled(t, f, l.ID)
}

// dispatchStepRunStart sends the chat step whose model calls run_start, for
// tests that scripted the turns themselves.
func dispatchStepRunStart(t *testing.T, f *streamFixture, bp string) protocol.StepResult {
	t.Helper()
	raw, err := json.Marshal(protocol.StepParams{
		// No Tools list: an agent step takes its grants from the handler
		// registry — chat's front-matter, which T10 widened with run_start.
		// The input could not widen it even to name the smuggled loop.
		Prompt: "start the blueprint", Agent: "chat",
		WorkspaceRoot: f.root, LoopSlug: "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.d.Table.Dispatch(context.Background(), "c-rs", protocol.MethodStep, raw)
	if err != nil {
		t.Fatalf("dispatch step: %v", err)
	}
	sr, ok := res.(protocol.StepResult)
	if !ok {
		t.Fatalf("dispatch returned %T", res)
	}
	return sr
}

func TestRunStartUnmetRequiresIsASpokenSentenceWithCandidates(t *testing.T) {
	attended := true
	// The blueprint demands an approved goal; one EXISTS in the org — the
	// refusal must offer it by id and title, because the agent quotes the
	// daemon's words rather than re-deciding anything (D6). The blueprint
	// file is written before the fixture; the goal lands through the hook,
	// which runs after the org and before the dispatch the gate judges.
	bp := runBlueprint(t, "needs-goal", "requires:\n  - any_of:\n      - {type: goal, state: approved}\n")
	f, _, sr, id := askRunStartHook(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp, "")}},
		{deltas: []string{runHandoff}},
	}, func(t *testing.T, f *streamFixture, projID string) {
		orgGoal(t, f.s, projID, "the approved one", "approved")
	}, toolRulePolicy("allow-run-start", "chat", "run_start", policy.Allow))
	defer waitLoopSettled(t, f, id)
	content, isErr := toolResultB1(t, f, sr.StepID)
	if !isErr {
		t.Fatalf("an unmet requires did not come back as an error result: %q", content)
	}
	if !strings.Contains(content, "You could use one of these:") ||
		!strings.Contains(content, "goal goal-") || !strings.Contains(content, "\"the approved one\" (approved)") {
		t.Fatalf("the refusal did not offer the approved goal as the person-readable candidate line: %q", content)
	}
	runs, _ := f.s.ListRuns(id)
	if len(runs) != 1 {
		t.Fatalf("a refused requires wrote a run row: %d (CR5)", len(runs))
	}
}

func TestRunStartIdentityComesFromTheStepNotTheInput(t *testing.T) {
	attended := true
	bp := runBlueprint(t, "second", "")
	f, _, sr, id := askRunStart(t, &attended, []gatedTurn{
		{toolUses: []model.Block{runStartToolUse(bp, `,"loopId":"x"`)}},
		{deltas: []string{runHandoff}},
	}, toolRulePolicy("allow-run-start", "chat", "run_start", policy.Allow))
	defer waitLoopSettled(t, f, id)
	// The schema is additionalProperties:false: the smuggled identity is a
	// PARSE refusal, not a second loop.
	content, isErr := toolResultB1(t, f, sr.StepID)
	if !isErr || !strings.Contains(content, "identity fields") {
		t.Fatalf("the smuggled loopId was not refused as an identity field: %q (err %v)", content, isErr)
	}
	runs, _ := f.s.ListRuns(id)
	if len(runs) != 1 {
		t.Fatalf("the input named a loop and got one: %d rows", len(runs))
	}
}

func TestRefusalSentence(t *testing.T) {
	two := map[string]any{
		"kind":   "requires-unmet",
		"detail": "blueprint \"goal-to-brief\" needs an approved goal — none was given",
		"candidates": []any{
			map[string]any{"kind": "goal", "id": "g1", "title": "Add export", "state": "approved"},
			map[string]any{"kind": "task", "id": "t7", "title": "Ship it", "state": "approved"},
		},
	}
	got := runRefusalSentence(two)
	want := "blueprint \"goal-to-brief\" needs an approved goal — none was given. You could use one of these:" +
		"\n- goal g1 \"Add export\" (approved)" +
		"\n- task t7 \"Ship it\" (approved)"
	if got != want {
		t.Fatalf("the renderer moved:\n got %q\nwant %q", got, want)
	}
	plain := map[string]any{"kind": "not-idle", "detail": "this loop is running; a run can only start while it is idle"}
	if got := runRefusalSentence(plain); got != plain["detail"] {
		t.Fatalf("a non-requires refusal must speak only its detail, got %q", got)
	}
	if got := runRefusalSentence("a bare string"); got != "a bare string" {
		t.Fatalf("a string renders itself, got %q", got)
	}
	// The tool boundary hands over the store's Go types, not JSON maps — the
	// renderer speaks BOTH shapes, or the agent quotes a dead end.
	goShape := map[string]any{
		"kind":       "requires-unmet",
		"detail":     "needs a goal",
		"candidates": []loop.Candidate{{Kind: "goal", ID: "g1", Title: "Add export", State: "approved"}},
	}
	if got := runRefusalSentence(goShape); !strings.Contains(got, `- goal g1 "Add export" (approved)`) {
		t.Fatalf("the Go candidate shape was dropped: %q", got)
	}
}
