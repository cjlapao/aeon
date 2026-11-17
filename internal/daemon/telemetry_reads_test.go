package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T8 end to end: the socket answers with what the REAL store holds,
// through the production adapter (telemetry_reads.go) wired in
// New()/daemon.go — not the protocol package's fake. This is the test the
// bench page and the CLI verbs will lean on: every field the app renders
// starts its life in a dispatch like these.

func teeingStore(t *testing.T) (*streamFixture, string, int64, []model.Message) {
	t.Helper()
	f := newStreamFixture(t, nil)
	initConn(t, f.d, context.Background(), "c1", "reader")
	root := "/repo/tee"
	comp, err := f.s.CreateComponent(store.NewComponent{Name: "tee", Slug: "tee", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateLoop(store.NewLoop{Slug: "tee-walk", Root: root, ComponentRoot: root}); err != nil {
		t.Fatal(err)
	}
	step, err := f.s.CreateStep(store.Scope{LoopSlug: "tee-walk", ComponentRoot: root}, store.NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	iptr := func(v int) *int { return &v }
	usage := model.Usage{InputTokens: iptr(300), OutputTokens: iptr(40), CacheReadTokens: iptr(50)}
	if err := f.s.RecordStepTurn(step.ID, 1, usage, model.Cost{State: model.CostKnown, USD: 0.003}, 900, "route-a", store.TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordStepTurn(step.ID, 1, model.Usage{}, model.Cost{State: model.CostUnknown}, 12, "route-a", store.TelemetryCategoryToolResult); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordFileActivity(step.ID, "src/tee.go", "write", iptr(9), iptr(2)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordCommandActivity(step.ID, "go test ./...", iptr(1), 410); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordStepCompaction(step.ID, 2, 8, 1500, 200, 320,
		model.Usage{InputTokens: iptr(1400), OutputTokens: iptr(80)}, model.Cost{State: model.CostKnown, USD: 0.005}); err != nil {
		t.Fatal(err)
	}
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("done the thing")},
			// M26: an assistant row in the store names who produced it —
			// AppendMessages refuses one that does not.
			Provenance: &model.Provenance{Route: "route-a", Provider: "anthropic", Model: "claude-test"}},
	}
	if err := f.s.AppendMessages(step.ID, msgs); err != nil {
		t.Fatal(err)
	}
	return f, comp.Component.ID, step.ID, msgs
}

func TestTelemetrySocketReadsTheRealStore(t *testing.T) {
	f, compID, stepID, msgs := teeingStore(t)

	// --- component tier: the store's numbers, field for field, on the wire.
	got := dispatchJSON(t, f, "c1", protocol.MethodTelemetryComponent, protocol.TelemetryComponentParams{ComponentID: compID})
	if got["componentId"] != compID {
		t.Errorf("componentId = %v", got["componentId"])
	}
	// turn(300+40+50) + compaction(1400+80) per column, and the loop's own bump.
	for key, want := range map[string]float64{
		"inputTokensTotal":     300 + 1400,
		"outputTokensTotal":    40 + 80,
		"cacheReadTokensTotal": 50,
		"costUsdTotal":         0.003 + 0.005,
		"filesWrittenTotal":    1,
		"linesAddedTotal":      9,
		"commandsRunTotal":     1,
		"commandsFailedTotal":  1,
		"commandWallMsTotal":   410,
		"loopsRun":             1,
		// the command's 410 rides commandWallMsTotal ALONE — wall_ms_total
		// counts the turn rows, no double-counting (T1's delta shapes).
		"wallMsTotal": 900 + 12,
	} {
		if got[key] != want {
			t.Errorf("component %s = %v, want %v", key, got[key], want)
		}
	}
	if got["costState"] != "known" {
		t.Errorf("costState = %v — every contributor was known", got["costState"])
	}

	// --- the same numbers at the two upper tiers (one component in the store).
	c, err := f.s.ReadComponent(compID)
	if err != nil {
		t.Fatal(err)
	}
	// The group (project) tier is the root's own row — the tee component IS a
	// root, so its project id is its own id.
	proj := dispatchJSON(t, f, "c1", protocol.MethodTelemetryProject, protocol.TelemetryProjectParams{ProjectID: c.ID})
	if proj["inputTokensTotal"] != 1700.0 || proj["costState"] != "known" {
		t.Errorf("the group tier drifted: %v", proj)
	}
	ws := dispatchJSON(t, f, "c1", protocol.MethodTelemetryWorkspace, struct{}{})
	if ws["exists"] != true || ws["loopsRun"] != 1.0 || ws["commandsFailedTotal"] != 1.0 {
		t.Errorf("the workspace singleton: %v", ws)
	}

	// --- the loop tier: both D2 numbers, the detail rows, the composition.
	loop := dispatchJSON(t, f, "c1", protocol.MethodTelemetryLoop, protocol.TelemetryLoopParams{StepID: stepID})
	stepRow, _ := loop["step"].(map[string]any)
	if stepRow["id"] != float64(stepID) {
		t.Errorf("the step anchor: %v", loop["step"])
	}
	cur, haveCur := loop["currentContextTokens"]
	cum, haveCum := loop["cumulativeTokensTotal"]
	if !haveCur || !haveCum {
		t.Fatalf("D2's pair did not BOTH travel: %v / %v", haveCur, haveCum)
	}
	if want := float64(runtime.EstimateTokens(msgs)); cur != want {
		t.Errorf("currentContextTokens = %v, want the runtime's own estimator %v", cur, want)
	}
	// D2/TL11's OWN words: cumulative is "the same number step_turn's rows
	// sum to" — the compaction row's 1400+80 ride the ROLLUPS (asserted
	// above at the component tier) but NOT this number, because the row is
	// not a step_turn row. Asserting the boundary here is the point: a
	// future "fix" that folds compaction in changes D2's meaning, and this
	// line says so out loud.
	if want := 300.0 + 40 + 50; cum != want {
		t.Errorf("cumulativeTokensTotal = %v, want %v — the step_turn sum, per TL11", cum, want)
	}
	if cur == cum {
		t.Error("the two D2 numbers coincided — the fixture stopped proving they are distinct")
	}
	turns, _ := loop["turns"].([]any)
	if len(turns) != 2 {
		t.Fatalf("%d turns, want the 2 recorded rows", len(turns))
	}
	toolRow, _ := turns[1].(map[string]any)
	if _, has := toolRow["inputTokens"]; has {
		t.Errorf("the tool_result row carried inputTokens — absence is the three-state: %v", toolRow)
	}
	if toolRow["costState"] != "unknown" {
		t.Errorf("tool_result costState = %v", toolRow["costState"])
	}
	if _, has := loop["compactions"]; !has {
		t.Error("compactions key absent")
	}
	if cs, _ := loop["compactions"].([]any); len(cs) != 1 {
		t.Errorf("compactions = %v", loop["compactions"])
	}
	if fa, _ := loop["fileActivities"].([]any); len(fa) != 1 {
		t.Errorf("fileActivities = %v", loop["fileActivities"])
	}
	if ca, _ := loop["commandActivities"].([]any); len(ca) != 1 {
		t.Errorf("commandActivities = %v", loop["commandActivities"])
	}
	// The step has no frozen manifest → composition is absent, NOT an
	// empty object pretending to be one.
	if _, has := loop["composition"]; has {
		t.Errorf("composition present though never recorded: %v", loop["composition"])
	}
}

func TestTelemetrySocketRefusesUnknownSubjects(t *testing.T) {
	f, _, _, _ := teeingStore(t)
	raw, _ := json.Marshal(protocol.TelemetryComponentParams{ComponentID: "cmp-nope"})
	_, err := f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodTelemetryComponent, raw)
	e, ok := err.(*protocol.Error)
	if !ok || e.Code != protocol.ErrNoTelemetry.Code {
		t.Fatalf("unknown component answered %T/%v — want the -31801 refusal", err, err)
	}
	// The refusal says WHICH component (TB2's rule at the real door).
	if !strings.Contains(e.Data.What, "cmp-nope") {
		t.Errorf("the refusal does not name the refused id: %s", e.Data.What)
	}
}

func TestTelemetrySocketRefusesMissingArguments(t *testing.T) {
	f, _, stepID, _ := teeingStore(t)
	raw, _ := json.Marshal(protocol.TelemetryLoopParams{StepID: 0})
	_, err := f.d.Table.Dispatch(context.Background(), "c1", protocol.MethodTelemetryLoop, raw)
	e, ok := err.(*protocol.Error)
	if !ok || e.Code != protocol.ErrInvalidScope.Code {
		t.Fatalf("stepId 0 answered %T/%v", err, err)
	}
	_ = stepID
}
