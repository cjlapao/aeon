package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

type classedSpy struct {
	*spyTools
	class string
	order *[]string
}

func (s classedSpy) Class(string) string { return s.class }
func (s classedSpy) Call(ctx context.Context, name string, input json.RawMessage) ToolOutcome {
	if s.order != nil {
		*s.order = append(*s.order, "execute")
	}
	return s.spyTools.Call(ctx, name, input)
}

type receiptSpy struct {
	order       *[]string
	beginErr    error
	completeErr error
}

func (s receiptSpy) Begin(_ context.Context, _, _, _ string) error {
	*s.order = append(*s.order, "begin")
	return s.beginErr
}
func (s receiptSpy) Complete(_ context.Context, _, _ string, _ bool) error {
	*s.order = append(*s.order, "complete")
	return s.completeErr
}

type checkpointSpy struct {
	order     *[]string
	snapshots []Checkpoint
}

func (s *checkpointSpy) Save(_ context.Context, cp Checkpoint) error {
	*s.order = append(*s.order, "checkpoint")
	s.snapshots = append(s.snapshots, cp)
	return nil
}

func receiptRun(class string, writer ReceiptWriter, cp CheckpointWriter, order *[]string) (StepResult, *classedSpy) {
	name := "bash"
	if class == "safe" {
		name = "read"
	}
	spy := &classedSpy{spyTools: newSpyTools(name), class: class, order: order}
	client := newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("u1", name, `{}`)}}, scriptedTurn{Text: okHandoff})
	st := testState(testHandler(name), []model.ToolSpec{toolSpec(name)})
	res := RunLoop(context.Background(), st, LoopDeps{Client: client, Tools: spy, Receipts: writer, Checkpoint: cp, HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	return res, spy
}

func TestReceiptWrittenBeforeSpawn(t *testing.T) {
	order := []string{}
	res, spy := receiptRun("unsafe", receiptSpy{order: &order}, nil, &order)
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	if len(spy.recorded()) != 1 || strings.Join(order, ",") != "begin,execute,complete" {
		t.Fatalf("order = %v", order)
	}
}

func TestBeginFailureBlocksExecution(t *testing.T) {
	order := []string{}
	res, spy := receiptRun("unsafe", receiptSpy{order: &order, beginErr: errors.New("disk full")}, nil, &order)
	if res.Outcome != OutcomeFailed || len(spy.recorded()) != 0 || strings.Join(order, ",") != "begin" {
		t.Fatalf("outcome %s, calls %v, order %v", res.Outcome, spy.recorded(), order)
	}
}

func TestSafeToolWritesNoReceipt(t *testing.T) {
	order := []string{}
	res, spy := receiptRun("safe", receiptSpy{order: &order}, nil, &order)
	if res.Outcome != OutcomeOK || len(spy.recorded()) != 1 || strings.Join(order, ",") != "execute" {
		t.Fatalf("outcome %s, order %v", res.Outcome, order)
	}
}

func TestReceiptCompletedBeforeTranscriptCheckpoint(t *testing.T) {
	order := []string{}
	cp := &checkpointSpy{order: &order}
	res, _ := receiptRun("unsafe", receiptSpy{order: &order}, cp, &order)
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	joined := strings.Join(order, ",")
	if !strings.Contains(joined, "checkpoint,begin,execute,complete,checkpoint") {
		t.Fatalf("checkpoint and receipt order = %v", order)
	}
	if len(cp.snapshots) < 3 {
		t.Fatalf("only %d checkpoints", len(cp.snapshots))
	}
	assistantBeforeBegin := cp.snapshots[1].Messages
	if len(assistantBeforeBegin) < 3 || assistantBeforeBegin[2].Role != model.RoleAssistant {
		t.Fatalf("assistant turn was not durable before begin: %+v", assistantBeforeBegin)
	}
}

func TestNilWriterRefusesUnsafeTool(t *testing.T) {
	order := []string{}
	res, spy := receiptRun("unsafe", nil, nil, &order)
	if res.Outcome != OutcomeFailed || len(spy.recorded()) != 0 || !strings.Contains(res.Reason, "without a receipt writer") {
		t.Fatalf("outcome %s, calls %v, reason %q", res.Outcome, spy.recorded(), res.Reason)
	}
}

func TestCompleteErrorDoesNotFailTheStep(t *testing.T) {
	order := []string{}
	res, spy := receiptRun("unsafe", receiptSpy{order: &order, completeErr: errors.New("disk full")}, nil, &order)
	if res.Outcome != OutcomeOK || len(spy.recorded()) != 1 {
		t.Fatalf("outcome %s, calls %v", res.Outcome, spy.recorded())
	}
}
