package daemon

// agent_mailbox_test.go pins S70's daemon half against a real store: the
// ask door files through the parser and the run's PINNED lanes (MB2),
// refuses a stranger by name with the lane list, files nothing behind a
// refusal, suspends a wait:true ask as a row, and drains a running peer's
// pending queue at the boundary — while a storeless daemon hands out nil
// doors and the pre-mailbox path runs byte-identical (I2).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

const pairBlueprint = `id: pair
schemaVersion: 1
version: 1
output: []
lanes:
  - id: plan
    step: {kind: model-step, agent: planner}
  - id: write
    step: {kind: model-step, agent: writer}
questions: {}
`

func openPairLoop(t *testing.T) (*store.Store, string, int64) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	bp, err := blueprint.Load([]byte(pairBlueprint))
	if err != nil {
		t.Fatalf("the pair blueprint must load: %v", err)
	}
	state, err := (loop.LoopState{RuntimeState: loop.InitialState(bp)}).RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePinnedLoop(store.NewLoop{Slug: "pair", ComponentRoot: root, Root: root, Goal: "ship", Status: "running"},
		bp.ID, bp.Version, pairBlueprint, state); err != nil {
		t.Fatal(err)
	}
	step, err := st.CreateStep(store.Scope{LoopSlug: "pair", ComponentRoot: root},
		store.NewStep{Agent: "planner", Phase: "build", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	return st, root, step.ID
}

func TestAskDoorFilesPinnedLaneAsksAndRefusesStrangers(t *testing.T) {
	st, root, author := openPairLoop(t)
	door := mailboxAskFor(st, nil)(author)

	// wait:false to a pinned lane: one row, honest sentence, thread 1.
	out, err := door.Ask(context.Background(), json.RawMessage(`{"to":"writer","text":"  review the framing\n  verbatim  ","wait":false}`))
	if err != nil {
		t.Fatalf("the door refused a lane ask: %v", err)
	}
	if out.MessageID == 0 || out.Thread != 1 || out.Suspends {
		t.Fatalf("filed outcome wrong: %+v", out)
	}
	if !strings.Contains(out.Result, "message") || !strings.Contains(out.Result, "no answer is awaited") {
		t.Errorf("the answer does not say what happened: %q", out.Result)
	}
	pending, err := st.PendingForAgent(root, "pair", "writer")
	if err != nil {
		t.Fatal(err)
	}
	// The store trims the body AT FILING (T1's shape: empty after trim is
	// refused), so the row carries the trimmed ask; byte-identity (AT7) is
	// filed-row-to-transcript, asserted in the runtime suite.
	if len(pending) != 1 || pending[0].FromAgent != "planner" || pending[0].Body != "review the framing\n  verbatim" {
		t.Fatalf("the filed row is not the ask as spoken: %+v", pending)
	}
	if pending[0].DeliveredAt != "" {
		t.Error("a filed row is not a delivered row")
	}

	// A stranger is refused BY NAME with the lane list, and nothing files.
	_, err = door.Ask(context.Background(), json.RawMessage(`{"to":"hacker","text":"x","wait":false}`))
	if err == nil || !strings.Contains(err.Error(), `"hacker" is not an agent of this loop`) ||
		!strings.Contains(err.Error(), "planner") || !strings.Contains(err.Error(), "writer") {
		t.Fatalf("the stranger refusal must name the stranger and the lanes (MB2): %v", err)
	}
	if p, _ := st.PendingForAgent(root, "pair", "hacker"); len(p) != 0 {
		t.Fatal("a stranger was reached — refused by name, never delivered to")
	}

	// The parser is the door's FIRST statement (AT1): a typo-name fails on
	// the shape, and the error restates the accepted set.
	_, err = door.Ask(context.Background(), json.RawMessage(`{"to":"writer","test":"typo","wait":false}`))
	if err == nil || !strings.Contains(err.Error(), "[test]") || !strings.Contains(err.Error(), "text") {
		t.Fatalf("the parser must name the unknown key and restate the shape: %v", err)
	}
	if p, _ := st.PendingForAgent(root, "pair", "writer"); len(p) != 1 {
		t.Fatal("the unparsed ask filed anyway")
	}
}

func TestWaitTrueSuspendsWithThePeerAndMessageNamed(t *testing.T) {
	st, root, author := openPairLoop(t)
	door := mailboxAskFor(st, nil)(author)
	out, err := door.Ask(context.Background(), json.RawMessage(`{"to":"writer","text":"can you confirm the framing?"}`))
	if err != nil {
		t.Fatalf("default wait must be true (MB4): %v", err)
	}
	if !out.Suspends || out.Question == nil {
		t.Fatalf("wait:true is suspension-as-a-row: %+v", out)
	}
	if out.Question.Kind != "free" {
		t.Errorf("question kind %q", out.Question.Kind)
	}
	if !strings.Contains(out.Question.Body, "writer") || !strings.Contains(out.Question.Body, "message") {
		t.Errorf("the question must name the peer and the row: %q", out.Question.Body)
	}
	if p, _ := st.PendingForAgent(root, "pair", "writer"); len(p) != 1 || p[0].DeliveredAt != "" {
		t.Fatalf("the wait:true ask filed undelivered: %+v", p)
	}
	// The author's own queue drains through the step's row (scope from the
	// row, never a flag): a row addressed to the author arrives at its
	// boundary read, addressed-to-others does not.
	if _, err := st.QueueAgentMessage(store.NewAgentMessage{LoopSlug: "pair", ComponentRoot: root,
		Thread: 1, FromAgent: "writer", ToAgent: "planner", FromStepID: author, Body: "framing fine", ExpectReply: false}); err != nil {
		t.Fatal(err)
	}
	got := stepMailbox{store: st, stepID: author}.Pending(context.Background())
	if len(got) != 1 || got[0].FromAgent != "writer" || got[0].Body != "framing fine" {
		t.Fatalf("the boundary read missed the peer's words: %+v", got)
	}
	// An unreadable step drains NOTHING rather than pretending (the steer
	// source's honest reading): id 99999 has no row.
	if got := (stepMailbox{store: st, stepID: 99999}).Pending(context.Background()); got != nil {
		t.Errorf("a step that cannot be read must drain nothing, got %+v", got)
	}
}

func TestDoorRefusesBeforeFilingWhenTheAuthorCannotBeRead(t *testing.T) {
	st, _, _ := openPairLoop(t)
	door := mailboxAskFor(st, nil)(99999)
	_, err := door.Ask(context.Background(), json.RawMessage(`{"to":"writer","text":"x","wait":false}`))
	if err == nil || !strings.Contains(err.Error(), "99999") {
		t.Fatalf("the refusal must name the unreadable author step: %v", err)
	}
	// The parser still runs FIRST even for a dead author: a malformed ask
	// answers with the shape refusal, not the row refusal (order is the
	// door's promise — AT1 before any read).
	_, err = door.Ask(context.Background(), json.RawMessage(`{"bogus":true}`))
	if err == nil || !strings.Contains(err.Error(), "[bogus]") {
		t.Fatalf("the parser must precede the author read: %v", err)
	}
}

func TestMailboxDoorsAreNilWithoutAStore(t *testing.T) {
	d := New(Options{})
	if d.Table.MailboxDrain != nil {
		t.Error("a storeless daemon handed out a drain — I2 requires the pre-mailbox path")
	}
	if d.Table.MailboxAsk != nil {
		t.Error("a storeless daemon handed out an ask door — I2 requires the pre-mailbox path")
	}
}

// TestAskReplayFilesOneRow is AT8's door half: a crash between the filing
// and the suspension commit asks the SAME question again on replay. The
// same addressee, the same words and the same thread are not a new
// message — the undrained ask already filed is the honest answer to "did I
// ask this?", and the replay reuses its row so the question keeps naming
// the id the dispatcher and the reply ride on.
// The registry-backed pair: lanes speaking for REGISTERED handlers
// ("default", "read-only") so the dispatch test's step can pass the
// handler gate — a step's agent is a named handler, not a free word.
const regPairBlueprint = `id: regpair
schemaVersion: 1
version: 1
output: []
lanes:
  - id: plan
    step: {kind: model-step, agent: default}
  - id: build
    step: {kind: model-step, agent: read-only}
questions: {}
`

func openRegPairLoop(t *testing.T) (*store.Store, string, int64) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	bp, err := blueprint.Load([]byte(regPairBlueprint))
	if err != nil {
		t.Fatalf("the registry pair blueprint must load: %v", err)
	}
	state, err := (loop.LoopState{RuntimeState: loop.InitialState(bp)}).RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreatePinnedLoop(store.NewLoop{Slug: "regpair", ComponentRoot: root, Root: root, Goal: "ship", Status: "running"},
		bp.ID, bp.Version, regPairBlueprint, state); err != nil {
		t.Fatal(err)
	}
	step, err := st.CreateStep(store.Scope{LoopSlug: "regpair", ComponentRoot: root},
		store.NewStep{Agent: "default", Phase: "build", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	return st, root, step.ID
}

// mailboxTestRoutes is the one route the daemon knows, mounted through
// Options exactly as serve mounts its environment routes.
func mailboxTestRoutes() protocol.Routes {
	return protocol.NewStaticRoutes(model.Route{ID: "test", Provider: "fake", Model: "scripted", NoPrice: true})
}

func TestAskReplayFilesOneRow(t *testing.T) {
	st, _, author := openPairLoop(t)
	door := mailboxAskFor(st, nil)(author)
	first, err := door.Ask(context.Background(), json.RawMessage(`{"to":"writer","text":"same words","wait":true}`))
	if err != nil || !first.Suspends || first.MessageID == 0 {
		t.Fatalf("the first ask did not suspend on a row: %+v, %v", first, err)
	}
	replay, err := door.Ask(context.Background(), json.RawMessage(`{"to":"writer","text":"same words","wait":true}`))
	if err != nil {
		t.Fatalf("the replay was refused: %v", err)
	}
	if replay.MessageID != first.MessageID || replay.Thread != first.Thread {
		t.Errorf("the replay filed a NEW row (%d vs %d) — one question, one row (AT8)",
			replay.MessageID, first.MessageID)
	}
	if replay.Question == nil || !strings.Contains(replay.Question.Body, fmt.Sprintf("message %d", first.MessageID)) {
		t.Errorf("the replayed question names the wrong row: %+v", replay.Question)
	}
	pending, err := st.PendingAsksFromStep(author)
	if err != nil || len(pending) != 1 {
		t.Fatalf("the loop asked twice — rows are the truth: %d pending, %v", len(pending), err)
	}
	// A DIFFERENT question is a new message: dedup answers identity, not
	// a blanket "you already asked someone".
	third, err := door.Ask(context.Background(), json.RawMessage(`{"to":"writer","text":"different words","wait":true}`))
	if err != nil || third.MessageID == first.MessageID {
		t.Fatalf("a distinct ask reused the row: %+v, %v", third, err)
	}
}

// TestWaitFalseQueuesForTheBoundaryRead is AT9's queue-back half: the
// undelivered row rides the addressee's boundary read, exactly as a steer
// rides the next turn — the caller walked away (no suspension, no waiting),
// and the words still land.
func TestWaitFalseQueuesForTheBoundaryRead(t *testing.T) {
	st, root, author := openPairLoop(t)
	writer, err := st.CreateStep(store.Scope{LoopSlug: "pair", ComponentRoot: root},
		store.NewStep{Agent: "writer", Phase: "build", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	out, err := mailboxAskFor(st, nil)(author).Ask(context.Background(),
		json.RawMessage(`{"to":"writer","text":"a note, no answer asked","wait":false}`))
	if err != nil || out.Suspends {
		t.Fatalf("wait:false must queue without suspending: %+v, %v", out, err)
	}
	msgs := (stepMailbox{store: st, stepID: writer.ID}).Pending(context.Background())
	if len(msgs) != 1 || msgs[0].ID != out.MessageID || msgs[0].FromAgent != "planner" ||
		msgs[0].Body != "a note, no answer asked" {
		t.Fatalf("the boundary read does not carry the note verbatim: %+v", msgs)
	}
}

// TestOwedReplyFilesAndWakesTheCaller pins the completion hook against a
// real store: a step that received an asking message files its reply
// through the one door (result_of the owed row — the link is the
// settlement), and wakes the caller through the SAME answer door a person
// travels — the caller's question row answers with the reply's words.
func TestOwedReplyFilesAndWakesTheCaller(t *testing.T) {
	st, root, author := openRegPairLoop(t)
	writer, err := st.CreateStep(store.Scope{LoopSlug: "regpair", ComponentRoot: root},
		store.NewStep{Agent: "read-only", Phase: "build", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := st.QueueAgentMessage(store.NewAgentMessage{
		LoopSlug: "regpair", ComponentRoot: root,
		FromAgent: "default", ToAgent: "read-only", FromStepID: author,
		Body: "what is the framing?", ExpectReply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeliverAgentMessage(ask.ID, writer.ID); err != nil {
		t.Fatal(err)
	}
	// The caller is waiting on its question row — the runtime's suspension
	// wrote it in the real flow; the test speaks the same row.
	qid, err := (&QuestionStore{store: st}).AskStep(author, "regpair", root,
		runtime.PendingQuestion{Kind: "free", Body: fmt.Sprintf("waiting for read-only to answer message %d (thread %d)", ask.ID, ask.Thread)})
	if err != nil {
		t.Fatal(err)
	}
	// The suspension is a fact the ROW carries (P2): a suspended step is
	// not running, which is also what keeps P33's one-running-step rule
	// true while the peer's turn dispatches beside it.
	if err := st.SetStepStatus(author, store.StepSuspended, ""); err != nil {
		t.Fatal(err)
	}
	// The caller resumes cold from rows (P2), so the rows carry its
	// standing: the transcript and the request envelope the answer door
	// validates before accepting the answer — the runtime wrote them before
	// suspending in the real flow; the test speaks the same rows.
	if err := st.SetContextManifestJSON(author,
		json.RawMessage(`{"request":{"workspaceRoot":"test"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessages(author, []model.Message{{Role: model.RoleUser,
		Content: []model.Block{{Kind: model.BlockText, Text: "asked words"}}}}); err != nil {
		t.Fatal(err)
	}
	d := New(Options{Store: st, Logger: mailboxTestLogger{t}, Routes: mailboxTestRoutes()})
	t.Cleanup(d.Shutdown)
	d.Table.Runner = mailboxStubRunner{}
	d.mailboxReplyTurns(writer.ID, "the framing is this, verbatim")

	rows, err := st.LoopAgentMessages(root, "regpair")
	if err != nil || len(rows) != 2 {
		t.Fatalf("the mailbox should hold ask + reply: %d rows, %v", len(rows), err)
	}
	reply := rows[1]
	if reply.FromAgent != "read-only" || reply.ToAgent != "default" || reply.ResultOf != ask.ID ||
		reply.Body != "the framing is this, verbatim" || reply.DeliveredToStepID != 0 {
		t.Errorf("the reply row is not a reply: %+v", reply)
	}
	if q, ok, err := st.PendingStepQuestion(author); ok || err != nil {
		t.Errorf("the caller is still waiting (question %d pending, err %v) — the reply must answer it (P31's third state moves)", q.ID, err)
	}
	answered, err := st.ReadQuestion(qid)
	if err != nil || answered.Answer != "the framing is this, verbatim" || answered.AnsweredAt == "" {
		t.Errorf("the question did not answer with the reply's words: %+v, %v", answered, err)
	}
	// The owed list settles through the link, and a second pass files
	// nothing new (idempotence by rows, not memory).
	before, _ := st.LoopAgentMessages(root, "regpair")
	d.mailboxReplyTurns(writer.ID, "again")
	after, _ := st.LoopAgentMessages(root, "regpair")
	if len(after) != len(before) {
		t.Errorf("the hook filed again — the result_of link is the settlement, once")
	}
	// The wake's continuation finishes on its own rows; let it settle
	// before the store closes (the continuation is tracked work, not a
	// stray goroutine — the test waits on the ROW, not a clock).
	waitStepSettled(t, st, author)
}

// TestMailboxFrameNamesCallerLaneGoalAndNothingElse is AT11's shape: the
// MB5 frame names the caller, its lane when the rows still know it, and
// the loop's goal, and tells the callee where its final summary lands —
// and the builder has NO channel for chosen skills: no operator spoke this
// turn, so no operator's picks can ride it (closing S68's step.Skill
// deviation by shape, not by filter).
func TestMailboxFrameNamesCallerLaneGoalAndNothingElse(t *testing.T) {
	ask := store.AgentMessage{ID: 7, Thread: 3, FromAgent: "planner", ToAgent: "writer", FromStepID: 42, Body: "asked words"}
	st := loop.LoopState{Slug: "pair", Goal: "ship the framing",
		RuntimeState: loop.RuntimeState{LastStepID: 42, Position: loop.Position{Lane: "plan"}}}
	frame := mailboxTurnFrame(st, ask)
	for _, want := range []string{"writer", "planner", "lane plan", "ship the framing", "message 7", "thread 3", "final handoff summary"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame does not name %q: %q", want, frame)
		}
	}
	lower := strings.ToLower(frame)
	for _, ban := range []string{"skill", "chosen", "attended"} {
		if strings.Contains(lower, ban) {
			t.Errorf("the frame speaks %q — the builder has no such channel (AT11)", ban)
		}
	}
	// The lane clause is a third state: when the rows no longer name the
	// author as the position's step, the frame says less, not something wrong.
	ask2 := ask
	ask2.FromStepID = 99
	frame2 := mailboxTurnFrame(st, ask2)
	if strings.Contains(frame2, "lane plan") {
		t.Error("the frame asserted a lane the rows no longer stand behind")
	}
	if !strings.Contains(frame2, "planner") {
		t.Error("even without a lane the caller is named")
	}
}

// waitStepSettled polls the ROWS until the step leaves running — the
// continuation's own finish is the done signal (the async continuation is
// the store's tracked work; a test that closes the store mid-flight would
// be racing the very discipline it pins).
func waitStepSettled(t *testing.T, s *store.Store, id int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := s.ReadStep(id)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status != string(store.StepRunning) {
			return
		}
		if evs, err := s.ReadEvents(0, store.EventFilter{}, 0); err == nil {
			for _, ev := range evs {
				if ev.Kind == "step.persistence_failed" {
					t.Fatalf("step %d never settled — the continuation said why: %s %v", id, ev.Kind, ev)
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("step %d never settled — the continuation is still on the rows", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// mailboxStubRunner is the scripted peer: Compile says the frame assembled,
// Run speaks the scripted handoff (its summary is the answer the hook
// files), BuildTools hands back nothing (the scripted Run calls no tool),
// and Resume completes — the wake's continuation has nothing left to say.
// reAsk is the crash-replay shape (AT8): the recovered re-run re-files the
// SAME wait:true ask — the model calls the tool again from the replayed
// transcript — and the door's dedup rule must answer with the row that
// already exists rather than filing a second one.
type mailboxStubRunner struct {
	reAsk bool
	// hold, when set, parks the addressee's turn in Run until it is closed (or
	// the turn's context ends). The crash walk uses it to keep the author's
	// step SUSPENDED for as long as the test needs to observe that state —
	// otherwise the whole chain can finish between two 20ms polls on a loaded
	// machine and the test sees a step that "never suspended".
	hold <-chan struct{}
}

func (mailboxStubRunner) Compile(context.Context, runtime.CompileInput, runtime.ContextDeps) runtime.CompileResult {
	return runtime.CompileResult{OK: true, Request: runtime.StepRequest{
		WorkspaceRoot: "test",
		// The opening checkpoint refuses an empty transcript (the store's
		// rule) — a real compile assembles at least the spoken words.
		Messages: []model.Message{{Role: model.RoleUser,
			Content: []model.Block{{Kind: model.BlockText, Text: "peer question"}}}},
	}}
}

// Run speaks the scripted answer AFTER doing the boundary's own book: it
// reads the mailbox drain (MB4's fold) and folds the peer's words into the
// transcript, then names the delivered rows on ONE checkpoint — the D4
// join the real runtime performs. The stub that skipped the drain would
// complete without owing anything, and the test would prove less.
func (r mailboxStubRunner) Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult {
	if r.hold != nil {
		select {
		case <-r.hold:
		case <-ctx.Done():
		}
	}
	msgs := append([]model.Message(nil), req.Messages...)
	var ids []int64
	if opts.MailboxDrain != nil {
		for _, m := range opts.MailboxDrain.Pending(ctx) {
			ids = append(ids, m.ID)
			msgs = append(msgs, model.Message{Role: model.RoleUser,
				Content: []model.Block{{Kind: model.BlockText,
					Text: "message from " + m.FromAgent + " (mailbox " + fmt.Sprint(m.ID) + "):\n\n" + m.Body}}})
		}
	}
	if opts.Checkpoint != nil && len(ids) > 0 {
		if err := opts.Checkpoint.Save(ctx, runtime.Checkpoint{Request: req, Messages: msgs, MailboxIDs: ids, Iteration: 1}); err != nil {
			panic("the scripted boundary checkpoint was refused: " + err.Error())
		}
	}
	return runtime.StepResult{Kind: runtime.ResultComplete, Outcome: runtime.OutcomeOK,
		Handoff:  &runtime.Handoff{"summary": "the answer, verbatim"},
		Messages: msgs}
}

func (r mailboxStubRunner) BuildTools([]string, string, string, runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error) {
	if r.reAsk {
		// The recovered step's turn is the ASK turn: the tool set it was
		// frozen with held ask_agent (the recovery re-issues the model
		// call, and the offer list is what makes the loop route the call
		// to the mailbox door instead of the tool dispatcher).
		return nil, []model.ToolSpec{{Name: "ask_agent"}}, nil
	}
	return nil, nil, nil
}

func (r mailboxStubRunner) Resume(context.Context, runtime.ResumeRequest, runtime.RunOpts) runtime.StepResult {
	return runtime.StepResult{Kind: runtime.ResultComplete, Outcome: runtime.OutcomeOK,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: "asked words"}}},
			{Role: model.RoleAssistant,
				Provenance: &model.Provenance{Route: "test", Provider: "fake", Model: "scripted"},
				Content:    []model.Block{{Kind: model.BlockText, Text: "answered"}}}}}
}

// TestDispatchRoundTripIsRows drives MB4's not-running arm end to end on a
// real store and a scripted runner: a wait:true ask whose addressee runs
// nowhere is dispatched as that peer's OWN turn (outside the ladder, agent
// from the row), its scripted handoff summary files the reply through the
// hook, and the caller's question row answers with it. No goroutine
// waits — each turn goroutine ran a step and exited (AS5's daemon
// converse pattern, the established fan-out).
func TestDispatchRoundTripIsRows(t *testing.T) {
	st, root, author := openRegPairLoop(t)
	out, err := mailboxAskFor(st, nil)(author).Ask(context.Background(),
		json.RawMessage(`{"to":"read-only","text":"peer question","wait":true}`))
	if err != nil || !out.Suspends {
		t.Fatalf("the ask did not suspend on a row: %+v, %v", out, err)
	}
	qid, err := (&QuestionStore{store: st}).AskStep(author, "regpair", root, *out.Question)
	if err != nil {
		t.Fatal(err)
	}
	// The suspension is a fact the ROW carries (P2) — and P33's rule that
	// a loop runs at most one step is what makes the peer's dispatch legal.
	if err := st.SetStepStatus(author, store.StepSuspended, ""); err != nil {
		t.Fatal(err)
	}
	// The caller resumes cold from rows (P2), so the rows carry its
	// standing: the transcript and the request envelope the answer door
	// validates before accepting the answer — the runtime wrote them before
	// suspending in the real flow; the test speaks the same rows.
	if err := st.SetContextManifestJSON(author,
		json.RawMessage(`{"request":{"workspaceRoot":"test"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessages(author, []model.Message{{Role: model.RoleUser,
		Content: []model.Block{{Kind: model.BlockText, Text: "asked words"}}}}); err != nil {
		t.Fatal(err)
	}
	d := New(Options{Store: st, Logger: mailboxTestLogger{t}, Routes: mailboxTestRoutes()})
	t.Cleanup(d.Shutdown)
	d.Table.Runner = mailboxStubRunner{}

	d.mailboxDispatchTurns(root, "regpair", author)
	// The dispatch is a `go` — wait for the rows it promises, not for a
	// wall clock: the reply row and the answered question ARE the done
	// signal (rows are the state; the test reads them, AS5's own argument).
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, err := st.LoopAgentMessages(root, "regpair")
		if err != nil {
			t.Fatal(err)
		}
		_, answered, aerr := st.PendingStepQuestion(author)
		if len(rows) == 2 && aerr == nil && !answered {
			reply := rows[1]
			if reply.FromAgent != "read-only" || reply.ToAgent != "default" || reply.ResultOf != out.MessageID ||
				reply.Body != "the answer, verbatim" {
				t.Fatalf("the dispatched turn's reply is wrong: %+v", reply)
			}
			break
		}
		if time.Now().After(deadline) {
			q, qerr := st.ReadQuestion(qid)
			t.Fatalf("the round trip stalled on rows: %d rows, caller waiting=%v — last question %+v (%v)", len(rows), answered, q, qerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The dispatched step is a real row OUTSIDE the ladder, speaking for
	// the addressee — the daemon spoke FOR the peer, never AS it.
	var sawPeerStep bool
	for _, s := range mustSteps(t, st, root) {
		if s.Agent == "read-only" {
			sawPeerStep = true
		}
	}
	if !sawPeerStep {
		t.Error("the dispatched turn left no step row for the addressee")
	}
}

func mustSteps(t *testing.T, s *store.Store, root string) []store.Step {
	t.Helper()
	steps, err := s.ListSteps(store.Scope{LoopSlug: "regpair", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	return steps
}

// mailboxTestLogger makes the hooks' swallowed refusions visible in the test
// log — a row that could not wake its caller must speak, not hide (the log
// line is the honest failure channel of a completion hook).
type mailboxTestLogger struct{ t *testing.T }

func (l mailboxTestLogger) Infof(format string, args ...any) {
	l.t.Logf(format, args...)
}

// TestMailboxReadServesTheLoopOwnRows pins MB7's read behind the daemon
// seam: the loop id resolves to its OWN scope (the rows of a same-named
// loop in another root could not leak even if asked), the whole-loop read
// and the thread read answer from the same rows the runtime writes, a
// named thread the loop lacks is the SAID absence, and an unknown loop
// refuses naming itself.
func TestMailboxReadServesTheLoopOwnRows(t *testing.T) {
	st, root, step := openRegPairLoop(t)
	ask, err := st.QueueAgentMessage(store.NewAgentMessage{
		LoopSlug: "regpair", ComponentRoot: root,
		FromAgent: "default", ToAgent: "read-only", FromStepID: step,
		Body: "what framing?", ExpectReply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The reply is AUTHORED by the peer's own step — the store checks the
	// author exists, the same law that makes "fromStepId" mean something.
	peer, err := st.CreateStep(store.Scope{LoopSlug: "regpair", ComponentRoot: root},
		store.NewStep{Agent: "read-only", Phase: "build", Status: store.StepRunning})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := st.QueueAgentMessage(store.NewAgentMessage{
		LoopSlug: "regpair", ComponentRoot: root,
		FromAgent: "read-only", ToAgent: "default", FromStepID: peer.ID,
		Body: "the framing is this", ResultOf: ask.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeliverAgentMessage(ask.ID, peer.ID); err != nil {
		t.Fatal(err)
	}
	threadID, noThread := ask.ID, int64(424242)
	pins, err := st.PinnedLoopsByStatus("running")
	if err != nil || len(pins) != 1 {
		t.Fatalf("one running loop is the fixture: %d pins, err %v", len(pins), err)
	}
	loopID := pins[0].Loop.ID
	read := mailboxReadFor(st)

	rows, absent, err := read(loopID, nil)
	if err != nil || absent {
		t.Fatalf("the whole-loop read: %d rows, absent %v, err %v", len(rows), absent, err)
	}
	if len(rows) != 2 || rows[0].ID != ask.ID || rows[1].ID != reply.ID {
		t.Fatalf("the rows did not ride whole: %+v", rows)
	}
	// Row → view is transport, not invention: the delivery pair and the
	// thread ride exactly as the store settled them.
	if rows[0].DeliveredAt == "" || rows[0].DeliveredToStep != peer.ID || rows[1].ResultOf != ask.ID {
		t.Errorf("the view drifted from the row: %+v / %+v", rows[0], rows[1])
	}
	if rows[0].Thread != ask.ID || rows[1].Thread != ask.ID {
		t.Errorf("the thread is the ask id (MB3) and it must ride: %d/%d vs %d", rows[0].Thread, rows[1].Thread, ask.ID)
	}

	rows, absent, err = read(loopID, &threadID)
	if err != nil || absent || len(rows) != 2 {
		t.Fatalf("the named thread is the WHOLE mailbox here: %d rows, absent %v, err %v", len(rows), absent, err)
	}
	rows, absent, err = read(loopID, &noThread)
	if err != nil {
		t.Fatal(err)
	}
	if !absent || len(rows) != 0 {
		t.Errorf("a thread the loop lacks must be a said absence, got %d rows absent %v", len(rows), absent)
	}

	if _, _, err := read("l-nobody", nil); err == nil || !strings.Contains(err.Error(), "no loop") {
		t.Errorf("an unknown loop must refuse naming itself, got %v", err)
	}
	if mailboxReadFor(nil) != nil {
		t.Error("with no store the seam itself must be nil — the handler then answers its own unavailability (I2)")
	}
}

// TestCrashMidAskRecoversOneRowNotTwo is AT8: the SIGKILL-mid-ask walk.
// The shape of that death, exactly: the door wrote the ask row, and the
// process died BEFORE the suspension was ever recorded — the step row
// still says RUNNING, which is precisely what Recover is for (P29: a
// crashed loop is replayed from rows, not resumed from memory). The new
// daemon replays the turn; the re-run re-files the same ask (the model
// speaks the same tool call from the replayed transcript); and the door's
// dedup rule answers with the row that already exists (T2b's rule
// earning its keep at the worst possible moment). The claims are rows,
// not wall clocks: ONE ask row, byte-identical (same id, same thread),
// and the step SETTLED — suspended on the row it filed, awaiting its
// reply, exactly as if the crash had never happened. A mailbox that
// loses asks on a crash, or doubles them, is a mailbox nobody can trust
// with a conversation; this is the test that says otherwise.
func TestCrashMidAskRecoversOneRowNotTwo(t *testing.T) {
	st, root, author := openRegPairLoop(t)
	// Mid-ask: the row is filed while the step is RUNNING (never
	// suspended — that write is what the crash swallowed).
	out, err := mailboxAskFor(st, nil)(author).Ask(context.Background(),
		json.RawMessage(`{"to":"read-only","text":"question asked before the crash","wait":true}`))
	if err != nil || !out.Suspends || out.MessageID == 0 {
		t.Fatalf("the ask before the crash did not file its row: %+v, %v", out, err)
	}
	if st0, err := st.ReadStep(author); err != nil || st0.Status != string(store.StepRunning) {
		t.Fatalf("the crash shape needs a RUNNING step row: %+v, %v", st0, err)
	}
	// A live turn always froze its manifest before the ask tool ran — the
	// crash leaves the manifest WHERE the turn wrote it. Without it the
	// recoverer calls the step unclassifiable (measured: "case
	// unclassifiable the frozen context manifest is absent"), which is
	// true but not this walk: the crash is mid-ASK, in a real turn.
	// The gate judges the ask exactly as every sibling tool (S70's design:
	// ask_agent lives BEHIND the policy gate), and it judges the HANDLER
	// name — an empty agent is refused by the gate's own defence line
	// ("a name that cannot be seen cannot be audited", measured). So the
	// frozen manifest carries the handler the step really ran under, and
	// the live store (a pre-freeze step: no entries, no marker) answers an
	// operator's allow for ask_agent.
	if err := st.SetContextManifestJSON(author, json.RawMessage(
		`{"request":{"workspaceRoot":"test","handler":{"Name":"default"},"route":{"id":"test","provider":"fake","model":"scripted","noPrice":true},"Tools":[{"Name":"ask_agent"}]},"iterationsUsed":1,"maxIterations":4}`)); err != nil {
		t.Fatal(err)
	}
	boundary := policy.NewStore(newPolicyRepo(st))
	if rej := boundary.Add(context.Background(), policy.Policy{
		ID: "allow-ask-agent-at8", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.AllAgents},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"ask_agent","state":"allow"}`),
	}, time.Now()); rej != nil {
		t.Fatalf("seeding the ask_agent allow: %s", rej.Reason)
	}
	if err := st.AppendMessages(author, []model.Message{{Role: model.RoleUser,
		Content: []model.Block{{Kind: model.BlockText, Text: "the turn the crash interrupted"}}}}); err != nil {
		t.Fatal(err)
	}
	// The crash is a NEW daemon over the same store — the old process is
	// gone, nothing of its memory survives. Recover is the door a real
	// `aeon serve` walks at start. Case B ("no model turn was committed"
	// — the ask tool call never reached a receipt) RE-ISSUES the model
	// call, so the scripted model here speaks exactly what the crashed
	// turn was speaking: one ask_agent tool call, same words, wait:true.
	// That re-issue is the walk: the ask tool runs THROUGH the loop's
	// mailbox arm (loop.go's ask_agent case), the door files or dedups,
	// and the step suspends as it was about to when the process died.
	m := &scriptedModel{turns: []modelTurn{{toolUses: []model.Block{
		{Kind: model.BlockToolUse, ID: "toolu_ask1", Name: "ask_agent",
			Input: json.RawMessage(`{"to":"read-only","text":"question asked before the crash","wait":true}`)},
	}}}}
	d := New(Options{Store: st, Logger: mailboxTestLogger{t}, Routes: mailboxTestRoutes(), Models: m})
	t.Cleanup(d.Shutdown)
	hold := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(hold) }) }
	t.Cleanup(release)
	d.Table.Runner = mailboxStubRunner{reAsk: true, hold: hold}
	if _, err := d.Recover(context.Background(), false); err != nil {
		t.Fatalf("Recover refused the crashed step: %v", err)
	}
	if len(m.requests) == 0 {
		t.Fatal("recovery did not re-issue the model call — Case B walked the wrong arm")
	}
	// Wait on ROWS to QUIESCENCE (store.Close does not wait continuations,
	// and this crash left a living chain: the recovered ask suspends the
	// author, the daemon's dispatcher runs the addressee's turn from the
	// row, the reply files, and the author's question answers — the whole
	// walk finishes its work before the store closes). Mid-walk the step
	// must pass through SUSPENDED: completed straight through its own ask
	// would be the recovery swallowing it.
	deadline := time.Now().Add(15 * time.Second)
	sawSuspended := false
	for {
		srow, err := st.ReadStep(author)
		if err != nil {
			t.Fatal(err)
		}
		if srow.Status == string(store.StepSuspended) {
			sawSuspended = true
			release() // observed: let the addressee's turn run and the chain finish
		}
		rows, err := st.LoopAgentMessages(root, "regpair")
		if err != nil {
			t.Fatal(err)
		}
		_, answered, aerr := st.PendingStepQuestion(author)
		if srow.Status == string(store.StepDone) && len(rows) == 2 && aerr == nil && !answered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the crash walk never quiesced on rows: step %s, %d rows, caller waiting=%v", srow.Status, len(rows), answered)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawSuspended {
		t.Error("the recovered step never sat SUSPENDED on its ask — the crash hid the suspension")
	}
	rows, err := st.LoopAgentMessages(root, "regpair")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("the replay doubled the ask or lost the reply — rows are the state: %d rows %+v", len(rows), rows)
	}
	ask := rows[0]
	// The crash survivor keeps its identity byte for byte (id, thread,
	// words, awaiting-reply standing) AND carries the delivery stamp the
	// walk earned: the addressee's turn folded it (D4's pair stamp) —
	// the only direction any of these fields may have moved.
	if ask.ID != out.MessageID || ask.Thread != out.Thread ||
		ask.Body != "question asked before the crash" || !ask.ExpectReply {
		t.Fatalf("the surviving row is not the one filed before the crash, byte for byte: %+v", ask)
	}
	if ask.DeliveredAt == "" || ask.DeliveredToStepID == 0 {
		t.Fatalf("the crash-surviving ask was never folded into the addressee's turn: %+v", ask)
	}
	if rows[1].FromAgent != "read-only" || rows[1].ResultOf != ask.ID {
		t.Fatalf("the reply did not ride back on the crash-surviving ask's link: %+v", rows[1])
	}
}
