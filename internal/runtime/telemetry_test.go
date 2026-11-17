package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 T2 — the seam exists, is nil-safe, and its vocabulary is closed.

// telemetryRecorder counts every method call and can be made to fail on command
// — T3/T5/T6's tests extend this shape; it lives here because T2 owns the
// interface.
type telemetryRecorder struct {
	turns       []recordedTelemetryTurn
	compactions int
	files       int
	commands    int
	fileRows    []recordedFileRow
	commandRows []recordedCommandRow
	failTurns   bool
}

// S33 T6 — the activity rows are asserted by VALUE, so the recorder keeps
// what was passed, not just that something arrived.
type recordedFileRow struct {
	stepID       int64
	path, action string
	added        *int
	removed      *int
}

type recordedCommandRow struct {
	stepID   int64
	command  string
	exitCode *int
	wallMs   int64
}

type recordedTelemetryTurn struct {
	stepID    int64
	iteration int
	usage     model.Usage
	cost      model.Cost
	wallMs    int64
	route     string
	category  SectionCategory
}

func (r *telemetryRecorder) RecordTurn(_ context.Context, stepID int64, iteration int, usage model.Usage, cost model.Cost, wallMs int64, route string, category SectionCategory) error {
	if r.failTurns {
		return errors.New("the telemetry write failed (simulated)")
	}
	r.turns = append(r.turns, recordedTelemetryTurn{stepID, iteration, usage, cost, wallMs, route, category})
	return nil
}
func (r *telemetryRecorder) RecordCompaction(_ context.Context, _ int64, _ CompactionRecord) error {
	r.compactions++
	return nil
}
func (r *telemetryRecorder) RecordFileActivity(_ context.Context, stepID int64, path, action string, added, removed *int) error {
	r.files++
	r.fileRows = append(r.fileRows, recordedFileRow{stepID, path, action, added, removed})
	return nil
}
func (r *telemetryRecorder) RecordCommandActivity(_ context.Context, stepID int64, command string, exitCode *int, wallMs int64) error {
	r.commands++
	r.commandRows = append(r.commandRows, recordedCommandRow{stepID, command, exitCode, wallMs})
	return nil
}

func TestNilTelemetryIsANoOp(t *testing.T) {
	// I2, pinned: with Telemetry nil a step runs EXACTLY what it ran before
	// the slice existed — same outcome, same transcript, no panic reaching
	// a nil interface. (The absence of calls is proven positively: T3+ add
	// call sites; T2's fact is that the field's existence changes nothing.)
	res := RunStep(context.Background(), testRequest(), runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil))
	if res.Outcome != OutcomeOK {
		t.Fatalf("a nil-Telemetry step did not run unchanged: %+v", res)
	}
	// And the same step with a wired sink: the outcome is IDENTICAL —
	// attaching a recorder never changes what the step does. (T2 asserted
	// the sink stayed empty because it added no call sites; T3 landed the
	// first capture, so emptiness is no longer the fact here. That a call
	// DOES happen — and lands exactly the call's own accounting — is proven
	// positively in telemetry_capture_test.go's
	// TestPerTurnCaptureFiresOncePerModelCall and its nil-vs-wired positive
	// control, which is the honest home of that claim now that a call site
	// exists.)
	sink := &telemetryRecorder{}
	opts := runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil)
	opts.Telemetry = sink
	res2 := RunStep(context.Background(), testRequest(), opts)
	if res2.Outcome != res.Outcome {
		t.Fatalf("adding a sink changed the outcome: %v vs %v", res2.Outcome, res.Outcome)
	}
	if !reflect.DeepEqual(res2.Messages, res.Messages) {
		t.Fatal("adding a sink changed the transcript — the field must be inert to the step itself")
	}
}

func TestNoFifthCategoryExists(t *testing.T) {
	// TL6's finding, machine-pinned: the vocabulary is FOUR. A fifth value —
	// `tool_schema` specifically, the one the brief named and measurement
	// killed — must not exist in the type.
	got := SectionCategories()
	want := map[string]bool{"system_prompt": true, "injected_context": true, "assistant_output": true, "tool_result": true}
	if len(got) != len(want) {
		t.Fatalf("category set = %v, want exactly the four", got)
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("unexpected category %q", c)
		}
		delete(want, c)
	}
	for c := range want {
		t.Errorf("category %q is missing", c)
	}
}

func TestTelemetrySinkFailureCannotFailTheStep(t *testing.T) {
	// The runtime-side half of "capture is observed, not transactional":
	// a sink whose every write fails must not change a step's outcome. The
	// call sites (T3+) route RecordX errors to the log, never to failed();
	// the seam itself guarantees the RESULT types agree.
	sink := &telemetryRecorder{failTurns: true}
	opts := runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil)
	opts.Telemetry = sink
	res := RunStep(context.Background(), testRequest(), opts)
	if res.Outcome != OutcomeOK {
		t.Fatalf("a failing sink changed the outcome: %+v", res)
	}
}
