package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T5 (TL7/D3) — a compaction lands a step_compaction row AND a durable
// event row, byte-exactly the same fact, readable after a restart. The blob
// stays what it was; this is the second home, driven here through the REAL
// runtime (a compaction the real selection code chose, not a hand-planted
// record) against the REAL store through the production adapter.

func compactionFixture(t *testing.T) (*store.Store, string, int64, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := "/repo/shop"
	if _, err := s.CreateComponent(store.NewComponent{Name: "Shop", Slug: "shop", Root: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "compact-walk", Root: root, ComponentRoot: root}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(store.Scope{LoopSlug: "compact-walk", ComponentRoot: root}, store.NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	return s, root, step.ID, dir
}

// runARealCompaction drives RunLoop over a history whose ContextWindow is
// small enough that the runtime's OWN selection buys a summary — turn 0 of
// the scripted model is that summary, turn 1 closes the step.
func runARealCompaction(t *testing.T, s *store.Store, root string, stepID int64) runtime.StepResult {
	t.Helper()
	h := runtime.StepHandler{Name: "hist", System: "be terse"}
	msgs := append(runtime.OpeningTranscript(h, "", "carry the plan", nil),
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(strings.Repeat("long history ", 200))}},
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("acknowledged")}},
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.ThinkingBlock("last thought", "signed"), model.TextBlock("continuing")}},
	)
	st := &runtime.LoopState{
		Request: runtime.StepRequest{
			StepID: stepID, Handler: h, WorkspaceRoot: root,
			Route: model.Route{ID: "r", Provider: "anthropic", Model: "m", ContextWindow: 400},
		},
		Messages: msgs,
		Manifest: &runtime.ContextManifest{MaxIterations: runtime.MaxIterations},
	}
	client := &scriptedModel{turns: []modelTurn{
		{text: "The earlier exchange established the task."},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```"},
	}}
	res := runtime.RunLoop(context.Background(), st, runtime.LoopDeps{
		Client:      client,
		HandoffSpec: runtime.HandoffSpec{Required: []string{"status", "summary"}},
		Telemetry:   newTelemetrySink(s),
	})
	if res.Outcome != runtime.OutcomeOK {
		t.Fatalf("the compaction walk must finish: %s — %s", res.Outcome, res.Reason)
	}
	if len(res.Compactions) != 1 {
		t.Fatalf("the runtime bought %d compactions, want exactly 1 — fixture rotted", len(res.Compactions))
	}
	return res
}

func TestCompactionProducesRowAndEvent(t *testing.T) {
	s, root, stepID, _ := compactionFixture(t)
	res := runARealCompaction(t, s, root, stepID)
	rec := res.Compactions[0]

	rows, err := s.StepCompactions(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d step_compaction rows, want 1", len(rows))
	}
	row := rows[0]
	// EXACTLY the blob's values — the row is a second home, not a second truth.
	if row.StartIdx != rec.Start || row.EndIdx != rec.End ||
		row.EstimateBefore != rec.EstimateBefore || row.EstimateAfter != rec.EstimateAfter ||
		row.SummaryLen != len(rec.Summary) {
		t.Fatalf("row %d–%d %d→%d len %d, blob says %d–%d %d→%d len %d",
			row.StartIdx, row.EndIdx, row.EstimateBefore, row.EstimateAfter, row.SummaryLen,
			rec.Start, rec.End, rec.EstimateBefore, rec.EstimateAfter, len(rec.Summary))
	}
	// The summary TEXT has no home in the row — length only (one copy).
	if row.CostState == "" {
		t.Error("the row must carry a cost state even when the scripted model charged nothing")
	}

	// The durable event row — the FIRST step-level event this codebase has
	// ever persisted through AppendEvent.
	events, err := s.ReadEvents(0, store.EventFilter{Kinds: []string{"step.compacted"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("%d step.compacted events, want 1 — the live emission alone does not satisfy TL7", len(events))
	}
	ev := events[0]
	if ev.LoopSlug != "compact-walk" || ev.ComponentRoot != root {
		t.Errorf("the event belongs to the step's loop and component, got %q/%q", ev.LoopSlug, ev.ComponentRoot)
	}
	var payload struct {
		StepID         int64 `json:"stepId"`
		Start          int   `json:"start"`
		End            int   `json:"end"`
		EstimateBefore int   `json:"estimateBefore"`
		EstimateAfter  int   `json:"estimateAfter"`
		SummaryChars   int   `json:"summaryChars"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload does not parse: %v (%s)", err, ev.Payload)
	}
	if payload.StepID != stepID || payload.Start != rec.Start || payload.End != rec.End ||
		payload.EstimateBefore != rec.EstimateBefore || payload.EstimateAfter != rec.EstimateAfter ||
		payload.SummaryChars != len(rec.Summary) {
		t.Fatalf("event payload %s disagrees with the compaction %+v", ev.Payload, rec)
	}
	if strings.Contains(string(ev.Payload), rec.Summary) {
		t.Error("the event payload copied the summary prose — length only, same rule as the row")
	}
}

func TestCompactionEventSurvivesRestart(t *testing.T) {
	// The whole complaint TL7 answers was "live-only": close the store,
	// reopen the SAME directory, and the event is still there with its seq.
	s, root, stepID, dir := compactionFixture(t)
	runARealCompaction(t, s, root, stepID)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	events, err := reopened.ReadEvents(0, store.EventFilter{Kinds: []string{"step.compacted"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("%d events after reopening, want the compaction event to persist", len(events))
	}
	if events[0].Seq <= 0 {
		t.Errorf("a durable event has no seq, got %d", events[0].Seq)
	}
	rows, err := reopened.StepCompactions(stepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d rows after reopening, want the row to persist too", len(rows))
	}
}
