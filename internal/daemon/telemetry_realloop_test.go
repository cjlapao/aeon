package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T7 (TL9) — the chain's WHOLE claim, end to end: a real scripted loop
// (compaction + two executed tools + three model turns) through the REAL
// runtime, the PRODUCTION adapter and the REAL store must leave every
// detail table's sum EQUAL to the component tier's totals. Not T1's
// synthetic RecordX calls: the captures as the loop actually makes them,
// nil-gated brackets and all.

type measuredTurn struct {
	text     string
	toolUses []model.Block
	usage    model.Usage
	cost     model.Cost
}

type measuredModel struct {
	turns []measuredTurn
	calls int
}

func (m *measuredModel) Stream(context.Context, model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error) {
	index := m.calls
	m.calls++
	var turn measuredTurn
	if index < len(m.turns) {
		turn = m.turns[index]
	} else {
		turn = measuredTurn{text: "```json\n{\"status\":\"ok\",\"summary\":\"out of script\"}\n```"}
	}
	deltas := make(chan model.Delta)
	results := make(chan model.StreamResult, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		if turn.text != "" {
			deltas <- model.Delta{Kind: model.DeltaText, Text: turn.text}
		}
		for i := range turn.toolUses {
			block := turn.toolUses[i]
			deltas <- model.Delta{Kind: model.DeltaToolUse, Block: &block}
		}
		results <- model.StreamResult{Attempts: 1, Usage: turn.usage, Cost: turn.cost}
	}()
	return deltas, results, errs
}

type outcomeTools struct {
	outcomes map[string]runtime.ToolOutcome
}

func (o outcomeTools) Call(_ context.Context, name string, _ json.RawMessage) runtime.ToolOutcome {
	if out, ok := o.outcomes[name]; ok {
		return out
	}
	return runtime.ToolOutcome{Content: "no scripted outcome for " + name, IsError: true}
}

func (o outcomeTools) Granted() []string { return []string{"write", "bash"} }

func TestRealLoopProducesConsistentRollups(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	root := "/repo/shop"
	comp, err := st.CreateComponent(store.NewComponent{Name: "Shop", Slug: "shop", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "real-walk", Root: root, ComponentRoot: root}); err != nil {
		t.Fatal(err)
	}
	step, err := st.CreateStep(store.Scope{LoopSlug: "real-walk", ComponentRoot: root}, store.NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}

	iptr := func(v int) *int { return &v }
	in := measuredModel{turns: []measuredTurn{
		// The compaction summary (bought by the runtime's own selection).
		{text: "The earlier exchange established the task.",
			usage: model.Usage{InputTokens: iptr(80), OutputTokens: iptr(12)},
			cost:  model.Cost{State: model.CostKnown, USD: 0.002}},
		{text: "", toolUses: []model.Block{toolUse("t1", "write", `{"path":"a.md","content":"x"}`)},
			usage: model.Usage{InputTokens: iptr(50), OutputTokens: iptr(10)},
			cost:  model.Cost{State: model.CostKnown, USD: 0.001}},
		{text: "", toolUses: []model.Block{toolUse("t2", "bash", `{"command":"make test"}`)},
			usage: model.Usage{InputTokens: iptr(60), OutputTokens: iptr(20)},
			cost:  model.Cost{State: model.CostKnown, USD: 0.003}},
		{text: "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```",
			usage: model.Usage{InputTokens: iptr(70), OutputTokens: iptr(5)},
			cost:  model.Cost{State: model.CostKnown, USD: 0.004}},
	}}
	tools := outcomeTools{outcomes: map[string]runtime.ToolOutcome{
		"write": {Content: "wrote a.md (1 bytes)", Path: "a.md", LinesAdded: 4, LinesRemoved: 1},
		"bash":  {Content: "ok", ExitCode: iptr(0), WallMs: 120},
	}}

	handler := runtime.StepHandler{Name: "walk", System: "be terse"}
	msgs := append(runtime.OpeningTranscript(handler, "", "carry the plan", nil),
		// Sized so the FIRST prompt overshoots the budget and the one
		// compaction the selection can find brings it back under — a second
		// admissible range need not exist, because no second attempt happens.
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(strings.Repeat("long history ", 700))}},
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("acknowledged")}},
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.ThinkingBlock("last thought", "signed"), model.TextBlock("continuing")}},
	)
	state := &runtime.LoopState{
		Request: runtime.StepRequest{
			StepID: step.ID, Handler: handler, WorkspaceRoot: root,
			Route: model.Route{ID: "r", Provider: "anthropic", Model: "m", ContextWindow: 1200},
		},
		Messages: msgs,
		Manifest: &runtime.ContextManifest{MaxIterations: runtime.MaxIterations},
	}
	res := runtime.RunLoop(context.Background(), state, runtime.LoopDeps{
		Client:      &in,
		Tools:       tools,
		HandoffSpec: runtime.HandoffSpec{Required: []string{"status", "summary"}},
		Telemetry:   newTelemetrySink(st),
		Clock:       func() time.Time { return time.Now() },
	})
	if res.Outcome != runtime.OutcomeOK {
		t.Fatalf("the walk must finish: %s — %s", res.Outcome, res.Reason)
	}

	// --- gather every detail row once; the rollups must equal THEIR sums.
	turns, err := st.StepTurns(step.ID, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 5 {
		t.Fatalf("%d step_turn rows — 3 assistant + 2 tool_result is the script, nothing else", len(turns))
	}
	comps, err := st.StepCompactions(step.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comps) != 1 {
		t.Fatalf("%d compaction rows, want the one the walk bought", len(comps))
	}

	var sumIn, sumOut, sumWall int64
	var sumCost float64
	for _, row := range turns {
		if row.InputTokens != nil {
			sumIn += int64(*row.InputTokens)
		}
		if row.OutputTokens != nil {
			sumOut += int64(*row.OutputTokens)
		}
		sumWall += row.WallMs
		if row.CostUSD != nil {
			sumCost += *row.CostUSD
		}
	}
	// The compaction row's usage is a contributor too (T1's rule: the
	// summary call is a model call, its usage sums, its wall does not exist).
	sumIn += int64(*comps[0].InputTokens)
	sumOut += int64(*comps[0].OutputTokens)
	sumCost += *comps[0].CostUSD

	got, err := st.ComponentTelemetry(comp.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.InputTokensTotal != sumIn || got.OutputTokensTotal != sumOut {
		t.Errorf("token rollups drifted: comp in=%d out=%d, detail sums in=%d out=%d",
			got.InputTokensTotal, got.OutputTokensTotal, sumIn, sumOut)
	}
	if d := got.CostUSDTotal - sumCost; d > 1e-9 || d < -1e-9 {
		t.Errorf("cost rollup drifted: comp %v, detail sum %v", got.CostUSDTotal, sumCost)
	}
	if got.WallMsTotal != sumWall {
		t.Errorf("wall rollup drifted: comp %d, detail sum %d", got.WallMsTotal, sumWall)
	}
	if got.CostState != "known" {
		t.Errorf("every contributor was known, the tier says %q", got.CostState)
	}
	// File and command tiers come from the tool outcomes' fields.
	if got.FilesWrittenTotal != 1 || got.FilesTouchedTotal != 1 || got.LinesAddedTotal != 4 || got.LinesRemovedTotal != 1 {
		t.Errorf("file rollups: %+v", got)
	}
	if got.CommandsRunTotal != 1 || got.CommandsFailedTotal != 0 || got.CommandWallMsTotal != 120 {
		t.Errorf("command rollups: %+v", got)
	}
	// loops_run rode CreateLoop (T1's wiring), one loop.
	if got.LoopsRun != 1 {
		t.Errorf("loops_run %d, want the loop's 1", got.LoopsRun)
	}

	// The project tier is the root's own row in the component tree (Shop is a
	// root, so its project rollup is its own component row).
	proj, err := st.ComponentTelemetry(comp.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proj.InputTokensTotal != sumIn || proj.CostUSDTotal != got.CostUSDTotal || proj.WallMsTotal != sumWall {
		t.Errorf("the project tier drifted from the component tier: %+v vs %+v", proj, got)
	}
	ws, err := st.WorkspaceTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	if !ws.Exists || ws.InputTokensTotal != sumIn || ws.CommandsRunTotal != 1 {
		t.Errorf("the workspace tier drifted: %+v", ws)
	}
}
