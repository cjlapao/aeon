package daemon

// S11 T12 — the ask suspension through the real composition point. The
// runtime's own tests prove the step shape; THIS file is the adapter-level
// half the plan demands: the question row exists honestly when a step
// suspends on an ask (SEC-049: visibly 'asked' — kind='permission', the
// command quoted, the deciding rule named) and does NOT exist when an ask
// was answered as a deny (the adapter creates question rows only for
// suspended results). The attendedness arrives exactly as T11 left it: the
// adapter's read of the pinned store.LoopAttended, collapsed to the one
// bool the runtime sees — fail-closed for unattended AND never-declared.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// askStepOnDispatchedStep drives the REAL path — dispatch `step`, the real
// daemon gate over seeded command rules, the adapter's operator read — and
// returns the protocol result. The loop row is created by the caller with
// the declaration the case names (attended true/false, or omitted: the
// never-declared NULL that must read as nobody-can-answer, not nobody-is-absent-so-assume-somebody).
func askStepOnDispatchedStep(t *testing.T, dir, root string, attended *bool, turns []modelTurn) (protocol.StepResult, *store.Store) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// One ask rule, written through the real boundary (the door `aeon
	// policy` opens), aimed at the ad-hoc handler's agent and this command.
	at := time.Now().UTC()
	askRule := commandRulePolicy("confirm-git-push", runtime.DefaultHandlerName, "git push*", policy.Ask)
	seedRules(t, s, at, askRule)

	// The loop row carries the PINNED declaration (T11's D2). CreateRunning
	// Step will find this row and keep it — the adapter reads exactly this.
	if _, err := s.CreateLoop(store.NewLoop{
		Slug: "ask-adapter", ComponentRoot: root, Title: "ask adapter", Goal: "T12", Attended: attended,
	}); err != nil {
		t.Fatal(err)
	}

	route := model.Route{ID: "r1", Provider: "anthropic", Model: "test", MaxOutputTokens: 1024}
	d := New(Options{Store: s, Routes: protocol.NewStaticRoutes(route), Models: &scriptedModel{turns: turns}})
	initConn(t, d, context.Background(), "c1", "test")
	raw, err := json.Marshal(protocol.StepParams{
		Prompt: "push it", Tools: []string{"bash"}, WorkspaceRoot: root, LoopSlug: "ask-adapter",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodStep, raw)
	if err != nil {
		t.Fatalf("dispatch step: %v", err)
	}
	res, ok := response.(protocol.StepResult)
	if !ok {
		t.Fatalf("dispatch returned %T", response)
	}
	return res, s
}

func TestAttendedAskCreatesTheHonestQuestionRow(t *testing.T) {
	// SEC-049 + SEC-051 at the adapter level: the attended step suspends, the
	// step row says 'suspended', and a question row EXISTS — kind
	// 'permission', the blocked block's id, the approve/deny options, and a
	// body that names the agent, the QUOTED command, the deciding rule and
	// the verdict sentence verbatim. The command reaching the ask proves
	// loop.go's wiring (the runtime parses it from the block input; without
	// it the gate would have asked the tool question and no command rule
	// could have fired).
	dir := t.TempDir()
	root := t.TempDir()
	attended := true
	const command = "git push origin main"
	res, s := askStepOnDispatchedStep(t, dir, root, &attended, []modelTurn{
		{toolUses: []model.Block{toolUse("b1", "bash", `{"command":"`+command+`"}`)}},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"never reached\"}\n```"},
	})

	if res.Kind != "suspended" || res.Question == nil {
		t.Fatalf("attended ask did not suspend through the adapter: %+v", res)
	}
	q := res.Question
	if q.Kind != "permission" {
		t.Errorf("question kind %q, want permission", q.Kind)
	}
	if q.ToolUseID != "b1" {
		t.Errorf("question ToolUseID %q, want the blocked block's id", q.ToolUseID)
	}
	if !reflect.DeepEqual(q.Options, runtime.PermissionOptions()) {
		t.Errorf("options %v, want the five permission answers %v", q.Options, runtime.PermissionOptions())
	}
	for _, want := range []string{
		strconv.Quote(command), "confirm-git-push", runtime.DefaultHandlerName,
	} {
		if !strings.Contains(q.Body, want) {
			t.Errorf("the question body is missing %q (rule/agent/command — QAP-12):\n%s", want, q.Body)
		}
	}

	// The row itself, from disk — not the in-memory answer.
	row, found, err := s.PendingStepQuestion(res.StepID)
	if err != nil || !found {
		t.Fatalf("no pending question row for the suspended step (found=%v err=%v) — SEC-049: the ask must be visibly ASKED", found, err)
	}
	if row.Kind != string(store.QuestionPermission) {
		t.Errorf("row kind %q, want permission (S6's row pattern for the permission kind)", row.Kind)
	}
	if row.ToolUseID != "b1" || row.Body != q.Body {
		t.Errorf("the row does not mirror the question: %+v", row)
	}
	st, err := s.ReadStep(res.StepID)
	if err != nil || st.Status != "suspended" {
		t.Fatalf("step row status %q (err %v), want suspended — the adapter mapping for a suspended result", st.Status, err)
	}

	// The blocked call is UNANSWERED in the stored transcript — the honest
	// non-dispatch proof at row level: bash was granted, the gate asked, and
	// nothing ran it. The answer (T13's verdict path) will be what answers
	// this block.
	tr := s.ReadTranscript(res.StepID)
	if !tr.OK {
		t.Fatalf("transcript: %s", tr.Reason())
	}
	for _, msg := range tr.Messages {
		for _, blk := range msg.Content {
			if blk.Kind == model.BlockToolResult && blk.ToolUseID == "b1" {
				t.Errorf("the asked call was answered at suspension (%q) — the answer answers it, not the suspension", blk.Content)
			}
		}
	}
}

func TestUnattendedAndNeverDeclaredAskDenyWithNoQuestionRow(t *testing.T) {
	// The adapter-level half of "unattended → immediate deny, NO question
	// row": with the declaration false, and — separately — never made (NULL,
	// the fail-closed case T11's accessor exists to express), the step
	// COMPLETES, the model is handed the verdict sentence verbatim, and the
	// question table stays empty for the step. A question row here would be
	// the row that waits for an answerer that was never declared.
	const command = "git push origin main"
	cases := []struct {
		name     string
		attended *bool
	}{
		{"declared-unattended", func() *bool { f := false; return &f }()},
		{"never-declared", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := t.TempDir()
			at := time.Now().UTC()
			askRule := commandRulePolicy("confirm-git-push", runtime.DefaultHandlerName, "git push*", policy.Ask)
			wantSentence := policy.Evaluate([]policy.Policy{askRule},
				policy.Request{Type: policy.CommandAccess, Agent: runtime.DefaultHandlerName, Command: command}, at).Sentence()

			res, s := askStepOnDispatchedStep(t, dir, root, tc.attended, []modelTurn{
				{toolUses: []model.Block{toolUse("b1", "bash", `{"command":"`+command+`"}`)}},
				{text: "```json\n{\"status\":\"ok\",\"summary\":\"denied, reported\"}\n```"},
			})

			if res.Kind != "complete" || res.Outcome != "ok" {
				t.Fatalf("kind %q outcome %q (reason %q), want complete/ok — the ask denied and the step carried on",
					res.Kind, res.Outcome, res.Reason)
			}
			if res.Question != nil {
				t.Errorf("a question reached the protocol result: %+v", res.Question)
			}

			// The transcript carries the verdict sentence VERBATIM as the
			// blocked call's IsError tool_result — the same single formatter
			// rendering the attended question would have carried.
			tr := s.ReadTranscript(res.StepID)
			if !tr.OK {
				t.Fatalf("transcript: %s", tr.Reason())
			}
			var sawDenial bool
			for _, msg := range tr.Messages {
				for _, blk := range msg.Content {
					if blk.Kind == model.BlockToolResult && blk.ToolUseID == "b1" {
						if !blk.IsError || blk.Content != wantSentence {
							t.Errorf("the model was handed (%q, isError=%v), want the verdict sentence verbatim and true:\nwant: %q", blk.Content, blk.IsError, wantSentence)
						}
						sawDenial = true
					}
				}
			}
			if !sawDenial {
				t.Fatal("the denied call was never answered in the stored transcript")
			}

			// NO question row — pending or otherwise.
			if _, found, err := s.PendingStepQuestion(res.StepID); err != nil || found {
				t.Errorf("a pending question row exists for a denied ask (found=%v err=%v) — the adapter creates rows only for suspended results", found, err)
			}
			if _, found, err := s.LatestStepQuestion(res.StepID); err != nil || found {
				t.Errorf("ANY question row exists for a denied ask (found=%v err=%v) — SEC-049's rows record asks that happened, not asks that were answered as denials", found, err)
			}
			st, err := s.ReadStep(res.StepID)
			if err != nil || st.Status != "done" || st.Outcome != "ok" {
				t.Fatalf("step row %+v (err %v), want done/ok", st, err)
			}
		})
	}
}

func TestOperatorPresentForCollapsesFailClosed(t *testing.T) {
	// The adapter's collapse, directly: true only when the loop DECLARED an
	// operator. Unattended, never-declared (NULL), and every read failure —
	// including a step id that has no row — all say nobody can be vouched
	// for, which is the deny direction. The runtime must never suspend on an
	// answerer nobody declared.
	s := recoveryStoreBound(t)
	root := t.TempDir()
	yes, no := true, false
	cases := []struct {
		name     string
		attended *bool
		want     bool
	}{
		{"declared-attended", &yes, true},
		{"declared-unattended", &no, false},
		{"never-declared", nil, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slug := "op-" + strconv.Itoa(i)
			if _, err := s.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Goal: "collapse", Attended: tc.attended}); err != nil {
				t.Fatal(err)
			}
			step, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewStep{Agent: "t", Status: store.StepRunning})
			if err != nil {
				t.Fatal(err)
			}
			if got := OperatorPresentFor(s, nil)(context.Background(), step.ID); got != tc.want {
				t.Errorf("OperatorPresentFor for %s = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	// Read failures collapse the same way: an unreadable step row and a zero
	// id both deny the suspend.
	if OperatorPresentFor(s, nil)(context.Background(), 999999) {
		t.Error("a step row that does not exist vouched for an operator")
	}
	if OperatorPresentFor(s, nil)(context.Background(), 0) {
		t.Error("an unattributable step id vouched for an operator")
	}
	if (StepRunner{}).operatorPresent(context.Background(), 1) {
		t.Error("the storeless seam (no OperatorPresent wired) vouched for an operator")
	}
	// A read failure UNDER a declared-attended loop still says nobody may
	// be vouched for — and says it out loud. The canceled context fails the
	// one read that honors it (LoopAttended runs QueryRowContext; the step
	// and loop rows are already in hand by then), which is the collapse
	// branch the FK cascade denies every other way.
	var spoken []string
	rec := recorderLogger{lines: &spoken}
	yesSlug := "op-cancelled"
	if _, err := s.CreateLoop(store.NewLoop{Slug: yesSlug, ComponentRoot: root, Goal: "collapse", Attended: &yes}); err != nil {
		t.Fatal(err)
	}
	cstep, err := s.CreateStep(store.Scope{LoopSlug: yesSlug, ComponentRoot: root}, store.NewStep{Agent: "t", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if OperatorPresentFor(s, rec)(cctx, cstep.ID) {
		t.Error("a failed operator-declaration read vouched for an operator anyway")
	}
	if len(spoken) == 0 {
		t.Error("the collapse was silent: a read failure that nobody can see is the one that surprises people")
	}
}

// recorderLogger is the Logger seam's spy: it keeps every spoken line so the
// fail-closed branch can be asserted SPOKEN, not just returned.
type recorderLogger struct {
	lines *[]string
}

func (r recorderLogger) Infof(format string, args ...any) {
	*r.lines = append(*r.lines, fmt.Sprintf(format, args...))
}

// A permission question that has been answered must not keep the loop reading "answer requested". The pinned
// loop stays waiting until the resumed step ends, so the view reads it as running once the parked step's own
// question is answered and the step is running again; an unanswered one, or a still-suspended step, stays waiting.
func TestResumedAsRunningOnlyAfterTheAnswerAndTheStepRuns(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	attended := true
	res, s := askStepOnDispatchedStep(t, dir, root, &attended, []modelTurn{
		{toolUses: []model.Block{toolUse("b1", "bash", `{"command":"git push origin main"}`)}},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"never reached\"}\n```"},
	})
	if res.Question == nil {
		t.Fatalf("no question: %+v", res)
	}
	d := New(Options{Store: s})
	var waiting loop.LoopState
	waiting.Status, waiting.QuestionID, waiting.LastStepID = "waiting", res.Question.ID, res.StepID

	if got := d.resumedAsRunning(waiting).Status; got != "waiting" {
		t.Fatalf("unanswered question: status %q, want waiting", got)
	}
	if _, err := s.Answer(res.Question.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if got := d.resumedAsRunning(waiting).Status; got != "waiting" {
		t.Fatalf("answered but step still suspended: status %q, want waiting", got)
	}
	if err := s.SetStepStatus(res.StepID, store.StepRunning, ""); err != nil {
		t.Fatal(err)
	}
	if got := d.resumedAsRunning(waiting).Status; got != "running" {
		t.Fatalf("answered and running: status %q, want running", got)
	}
	stopped := waiting
	stopped.LastReason = "stopped"
	if got := d.resumedAsRunning(stopped).Status; got != "waiting" {
		t.Fatalf("a person-stopped step must stay waiting, got %q", got)
	}
}
