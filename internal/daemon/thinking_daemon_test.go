package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S18 T5 — the last link: the count the loop accumulated files itself into
// the step's row when the step completes (O1: the number must reach the
// record, not a log line). These are the daemon's two doors — the step
// runner's and the ask save's.

// dropModel plays one clean handoff turn and reports the drops the client
// would have collected from the adapter (3 blocks withheld on the way out).
type dropModel struct {
	dropped int
	calls   int
}

func (m *dropModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	m.calls++
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		deltas <- model.Delta{Kind: model.DeltaText, Text: "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```"}
		results <- model.StreamResult{Attempts: 1, ThinkingDropped: m.dropped}
	}()
	return deltas, results, errs
}

func TestThinkingCountFilesItselfWhenTheStepCompletes(t *testing.T) {
	s := recoveryStore(t)
	sc := store.Scope{LoopSlug: "drop-step", ComponentRoot: "/repo"}
	if _, err := s.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Title: "drops"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.CreateStep(sc, store.NewStep{Phase: "build", Agent: "drops"})
	if err != nil {
		t.Fatal(err)
	}

	handler := runtime.DefaultHandler("say ok", nil)
	r := StepRunner{ThinkingCount: ThinkingRecorderFor(s, nil)}
	res := r.Run(context.Background(), runtime.StepRequest{
		Handler:  handler,
		Messages: runtime.OpeningTranscript(handler, "", "say ok", nil),
		Manifest: runtime.ContextManifest{MaxIterations: runtime.MaxIterations, DeadlineAt: time.Now().Add(time.Hour)},
		Route:    model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		StepID:   st.ID,
	}, runtime.RunOpts{Client: &dropModel{dropped: 3}})
	if res.Kind != runtime.ResultComplete || res.Outcome != runtime.OutcomeOK {
		t.Fatalf("the step ended %s/%s, want an ok to read the row from", res.Kind, res.Outcome)
	}

	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThinkingDropped != 3 {
		t.Errorf("the step row says %d, want 3 — the omission never reached the record (O1)", back.ThinkingDropped)
	}
}

func TestNoDropsLeavesTheColumnAlone(t *testing.T) {
	// Zero is NOT written: a step nobody dropped from has nothing to record,
	// and NULL already reads back as 0. Pinning this matters because a
	// writer that filed zeroes would make the column say "recorded zero"
	// for every step ever run, blinding it to the pre-T5 rows (P31's third
	// state, respected by leaving it alone).
	s := recoveryStore(t)
	sc := store.Scope{LoopSlug: "quiet-step", ComponentRoot: "/repo"}
	if _, err := s.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Title: "quiet"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.CreateStep(sc, store.NewStep{Phase: "build", Agent: "quiet"})
	if err != nil {
		t.Fatal(err)
	}

	called := 0
	handler := runtime.DefaultHandler("say ok", nil)
	r := StepRunner{ThinkingCount: func(stepID int64, dropped int) { called++ }}
	r.Run(context.Background(), runtime.StepRequest{
		Handler:  handler,
		Messages: runtime.OpeningTranscript(handler, "", "say ok", nil),
		Manifest: runtime.ContextManifest{MaxIterations: runtime.MaxIterations, DeadlineAt: time.Now().Add(time.Hour)},
		Route:    model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024},
		StepID:   st.ID,
	}, runtime.RunOpts{Client: &dropModel{dropped: 0}})

	if called != 0 {
		t.Errorf("the recorder fired %d times for a clean step; zero must stay unrecorded", called)
	}
	back, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ThinkingDropped != 0 {
		t.Errorf("clean step row says %d", back.ThinkingDropped)
	}
}

func TestThinkingRecorderIsNilWithoutAStore(t *testing.T) {
	// The storeless test daemon has no record to reach; the recorder is nil,
	// the runner's guard skips it, and the count still rides the result for
	// whoever holds it.
	if got := ThinkingRecorderFor(nil, nil); got != nil {
		t.Error("ThinkingRecorderFor(nil) is not nil — the storeless runner would call into nothing")
	}
}
