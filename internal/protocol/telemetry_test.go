package protocol

// telemetry_test.go — S33 T8: the four reads answer with what the store
// says, in the shapes TL10/TL11 promise. The fake below is deliberately
// THICK — it holds real store structs — because these handlers' whole job
// is not mangling them on the way to the wire.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type fakeTelemetryStore struct {
	component    ComponentTelemetry
	componentErr error
	project      ProjectTelemetry
	projectErr   error
	workspace    WorkspaceTelemetry
	loop         TelemetryLoopData
	loopErr      error
	// what the caller named, for the required-argument assertions.
	gotComponentID, gotProjectID string
	gotStepID                    int64
	gotWorkspace                 bool
}

func (f *fakeTelemetryStore) ComponentTelemetry(id string) (ComponentTelemetry, error) {
	f.gotComponentID = id
	return f.component, f.componentErr
}

func (f *fakeTelemetryStore) ProjectTelemetry(id string) (ProjectTelemetry, error) {
	f.gotProjectID = id
	return f.project, f.projectErr
}

func (f *fakeTelemetryStore) WorkspaceTelemetry() (WorkspaceTelemetry, error) {
	f.gotWorkspace = true
	return f.workspace, nil
}

func (f *fakeTelemetryStore) LoopTelemetry(stepID int64) (TelemetryLoopData, error) {
	f.gotStepID = stepID
	return f.loop, f.loopErr
}

func telemetryTable(f *fakeTelemetryStore) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.TelemetryReads = f
	return tbl
}

func callTelemetry(t *testing.T, tbl *Table, m MethodName, params any) (any, error) {
	t.Helper()
	meth, ok := tbl.Lookup(m)
	if !ok {
		t.Fatalf("%s is not registered", m)
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	return meth.Handle(context.Background(), nil, raw)
}

func iv(v int) *int         { return &v }
func fv(v float64) *float64 { return &v }

// TestTelemetryComponentReturnsRealRollup — the handler's response equals
// the store's struct field for field. Anything restated, renamed or
// dropped between store and wire is exactly the drift this pins out.
func TestTelemetryComponentReturnsRealRollup(t *testing.T) {
	want := ComponentTelemetry{
		ComponentID: "cmp-shop",
		TelemetrySums: TelemetrySums{
			LoopsRun: 3, WallMsTotal: 9100, InputTokensTotal: 1200, OutputTokensTotal: 340,
			CacheReadTokensTotal: 800, CacheWriteTokensTotal: 12, ReasoningTokensTotal: 60,
			CostUSDTotal: 0.034, CostState: "known",
			FilesTouchedTotal: 5, FilesReadTotal: 4, FilesWrittenTotal: 2,
			LinesAddedTotal: 90, LinesRemovedTotal: 11,
			CommandsRunTotal: 7, CommandsFailedTotal: 1, CommandWallMsTotal: 4200,
		},
		UpdatedAt: "2026-10-30T09:00:00Z",
		Exists:    true,
	}
	f := &fakeTelemetryStore{component: want}
	got, err := callTelemetry(t, telemetryTable(f), MethodTelemetryComponent, TelemetryComponentParams{ComponentID: "cmp-shop"})
	if err != nil {
		t.Fatalf("the read refused: %v", err)
	}
	if f.gotComponentID != "cmp-shop" {
		t.Fatalf("the store was asked for %q", f.gotComponentID)
	}
	row, ok := got.(ComponentTelemetry)
	if !ok {
		t.Fatalf("the wire carries %T, not the store's own shape", got)
	}
	if !reflect.DeepEqual(row, want) {
		t.Errorf("the wire drifted from the store:\n got %+v\nwant %+v", row, want)
	}
	// And the JSON names are the store's names — the app renders the SAME
	// keys protogen exported, so a rename here breaks the tab silently.
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"componentId"`, `"costState"`, `"commandsFailedTotal"`, `"updatedAt"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("the wire has no %s — the store's tag is the contract", key)
		}
	}
}

// TestTelemetryLoopCarriesBothD2Numbers — TL11 at the wire: BOTH numbers,
// always, DISTINCT on a compacted loop. A fixture where they coincide
// proves nothing; here the current estimate is what the next call would
// carry (small, post-compaction) and the cumulative is everything ever
// billed (large).
func TestTelemetryLoopCarriesBothD2Numbers(t *testing.T) {
	f := &fakeTelemetryStore{loop: TelemetryLoopData{
		Step: TelemetryStep{ID: 41, LoopSlug: "walk", ComponentRoot: "/repo/shop", Status: "running"},
		Turns: []TurnRow{
			{ID: 1, StepID: 41, Iteration: 1, StartedAt: "t0", EndedAt: "t1", WallMs: 900,
				InputTokens: iv(400), OutputTokens: iv(60), CostUSD: fv(0.004), CostState: "known", Route: "r", Category: "assistant_output"},
			{ID: 2, StepID: 41, Iteration: 1, StartedAt: "t1", EndedAt: "t2", WallMs: 30,
				CostState: "unknown", Route: "r", Category: "tool_result"},
		},
		Compactions: []CompactionRow{
			{ID: 1, StepID: 41, StartIdx: 2, EndIdx: 14, EstimateBefore: 1800, EstimateAfter: 210,
				SummaryLen: 400, InputTokens: iv(1500), OutputTokens: iv(90), CostUSD: fv(0.006), CostState: "known", OccurredAt: "t1"},
		},
		CurrentContextTokens:  240,
		CumulativeTokensTotal: 2050,
		Composition:           json.RawMessage(`{"sections":[{"title":"Task","category":"system_prompt","chars":120}],"systemChars":340}`),
	}}
	got, err := callTelemetry(t, telemetryTable(f), MethodTelemetryLoop, TelemetryLoopParams{StepID: 41})
	if err != nil {
		t.Fatalf("the read refused: %v", err)
	}
	data := got.(TelemetryLoopData)
	if data.CurrentContextTokens == 0 || data.CumulativeTokensTotal == 0 {
		t.Fatalf("a zero D2 number — present means PRESENT: %+v", data)
	}
	if data.CurrentContextTokens == data.CumulativeTokensTotal {
		t.Fatal("the two D2 numbers collapsed into one value — the wire cannot express the distinction")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"currentContextTokens"`, `"cumulativeTokensTotal"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("the wire lacks %s — D2's pair must always BOTH travel", key)
		}
	}
	// The tool_result row travels WITHOUT token fields (null is "not
	// reported", never 0) and with costState unknown.
	var probe struct {
		Turns []map[string]any `json:"turns"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Turns) != 2 {
		t.Fatalf("%d turns on the wire", len(probe.Turns))
	}
	toolRow := probe.Turns[1]
	if _, has := toolRow["inputTokens"]; has {
		t.Errorf("the tool_result row carried inputTokens — absence is the three-state, 0 is a lie: %v", toolRow)
	}
	if toolRow["costState"] != "unknown" {
		t.Errorf("the tool_result row's costState = %v, want the honest unknown", toolRow["costState"])
	}
}

// TestTelemetryWorkspaceNoArgs — no params of any kind are accepted into
// meaning: the singleton row answers whether the call carried {} or
// nothing at all, and there is no filter param to accrete.
func TestTelemetryWorkspaceNoArgs(t *testing.T) {
	f := &fakeTelemetryStore{workspace: WorkspaceTelemetry{
		TelemetrySums: TelemetrySums{LoopsRun: 9, InputTokensTotal: 5000, CostState: "partial", CommandsRunTotal: 4},
		UpdatedAt:     "2026-10-30T09:30:00Z",
		Exists:        true,
	}}
	for _, p := range []any{nil, TelemetryLoopParams{}} {
		got, err := callTelemetry(t, telemetryTable(f), MethodTelemetryWorkspace, p)
		if err != nil {
			t.Fatalf("the no-args read refused (params %v): %v", p, err)
		}
		row := got.(WorkspaceTelemetry)
		if !row.Exists || row.LoopsRun != 9 || row.CostState != "partial" {
			t.Errorf("the singleton row did not ride through: %+v", row)
		}
	}
	if !f.gotWorkspace {
		t.Fatal("the store was never asked")
	}
	// A store that has recorded nothing answers exists:false AS DATA.
	quiet := &fakeTelemetryStore{}
	got, err := callTelemetry(t, telemetryTable(quiet), MethodTelemetryWorkspace, nil)
	if err != nil {
		t.Fatalf("absence must be data, not an error: %v", err)
	}
	if got.(WorkspaceTelemetry).Exists {
		t.Error("exists:true on a store with no telemetry row — a zero-filled lie")
	}
}

// TestTelemetryHonorsCostStateAtTheWire — a partial component says
// partial. The sum and its state travel together or the sum is a lie.
func TestTelemetryHonorsCostStateAtTheWire(t *testing.T) {
	f := &fakeTelemetryStore{project: ProjectTelemetry{
		ProjectID: "prj-group",
		TelemetrySums: TelemetrySums{
			InputTokensTotal: 800, CostUSDTotal: 0.02, CostState: "partial",
		},
		UpdatedAt: "now", Exists: true,
	}}
	got, err := callTelemetry(t, telemetryTable(f), MethodTelemetryProject, TelemetryProjectParams{ProjectID: "prj-group"})
	if err != nil {
		t.Fatalf("the read refused: %v", err)
	}
	b, _ := json.Marshal(got)
	var probe struct {
		CostState string  `json:"costState"`
		CostUSD   float64 `json:"costUsdTotal"`
		ProjectID string  `json:"projectId"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.CostState != "partial" {
		t.Errorf("costState = %q on the wire — a USDAtLeast sum rendered as known is the D9 betrayal", probe.CostState)
	}
	if probe.CostUSD != 0.02 {
		t.Errorf("costUsdTotal = %v", probe.CostUSD)
	}
}

// TestUnknownIDNamesWhatExists — an unknown id is refused with the id in
// the sentence, never an empty-everything answer.
func TestUnknownIDNamesWhatExists(t *testing.T) {
	f := &fakeTelemetryStore{
		componentErr: fmt.Errorf("store: no telemetry row for component %q: %w", "cmp-typo", ErrNoTelemetryRow),
		projectErr:   fmt.Errorf("store: no telemetry row for project %q: %w", "prj-typo", ErrNoTelemetryRow),
		loopErr:      fmt.Errorf("store: no step %d: %w", 99, ErrNoTelemetryRow),
	}
	tbl := telemetryTable(f)
	cases := []struct {
		m    MethodName
		p    any
		want string
	}{
		{MethodTelemetryComponent, TelemetryComponentParams{ComponentID: "cmp-typo"}, "cmp-typo"},
		{MethodTelemetryProject, TelemetryProjectParams{ProjectID: "prj-typo"}, "prj-typo"},
		{MethodTelemetryLoop, TelemetryLoopParams{StepID: 99}, "99"},
	}
	for _, c := range cases {
		_, err := callTelemetry(t, tbl, c.m, c.p)
		e, ok := err.(*Error)
		if !ok {
			t.Fatalf("%s: want *protocol.Error, got %T: %v", c.m, err, err)
		}
		if e.Code != ErrNoTelemetry.Code {
			t.Errorf("%s: code %d, want %d", c.m, e.Code, ErrNoTelemetry.Code)
		}
		if !strings.Contains(e.Data.What, c.want) {
			t.Errorf("%s: the refusal does not name %q: %s", c.m, c.want, e.Data.What)
		}
	}
}

// The required-argument refusals name what was missing (T5 §5), and a
// storeless daemon answers ErrStoreUnavailable — never a panic, never an
// empty success.
func TestTelemetryReadsRefuseBadArgumentsAndAbsentStores(t *testing.T) {
	f := &fakeTelemetryStore{}
	tbl := telemetryTable(f)
	for _, c := range []struct {
		m      MethodName
		p      any
		needle string
	}{
		{MethodTelemetryLoop, TelemetryLoopParams{StepID: 0}, "step id"},
		{MethodTelemetryComponent, TelemetryComponentParams{ComponentID: "  "}, "component id"},
		{MethodTelemetryProject, TelemetryProjectParams{ProjectID: ""}, "project"},
	} {
		_, err := callTelemetry(t, tbl, c.m, c.p)
		e, ok := err.(*Error)
		if !ok {
			t.Fatalf("%s: want *protocol.Error, got %T", c.m, err)
		}
		if !strings.Contains(strings.ToLower(e.Data.What), c.needle) {
			t.Errorf("%s: refusal %q does not name the missing %q", c.m, e.Data.What, c.needle)
		}
	}
	if f.gotStepID != 0 || f.gotComponentID != "" {
		t.Error("a refused call still reached the store")
	}
	// A TABLE with the field left nil — the storeless shape the daemon
	// builds. (telemetryTable(nil) would store a typed-nil *fake and sail
	// past the ==nil check: the exact trap newTelemetrySink's function gate
	// exists to delete, walked into here and kept out by writing the zero
	// Table instead.)
	bare := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	_, err := callTelemetry(t, bare, MethodTelemetryWorkspace, nil)
	if e, ok := err.(*Error); !ok || e.Code != ErrStoreUnavailable.Code {
		t.Errorf("storeless daemon answered %v — ErrStoreUnavailable is the honest state", err)
	}
}

// TestTelemetryMethodsAreRegisteredNotDebug — TL10: product reads, so they
// are in the table and NOT marked Debug (a Debug method is diagnostics,
// off the app's assumed path).
func TestTelemetryMethodsAreRegisteredNotDebug(t *testing.T) {
	tbl := telemetryTable(&fakeTelemetryStore{})
	for _, m := range []MethodName{MethodTelemetryLoop, MethodTelemetryComponent, MethodTelemetryProject, MethodTelemetryWorkspace} {
		meth, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered", m)
		}
		if meth.Debug {
			t.Errorf("%s is Debug — TL10 names these the PRIMARY surface", m)
		}
		if !meth.RequiresInitialize {
			t.Errorf("%s does not require initialize", m)
		}
	}
	// And NO write method carries the telemetry prefix — this surface reads.
	for _, meth := range tbl.Methods() {
		if strings.HasPrefix(string(meth), "telemetry.") {
			name := string(meth)
			switch name {
			case "telemetry.loop", "telemetry.component", "telemetry.project", "telemetry.workspace":
			default:
				t.Errorf("a fifth telemetry method %q appeared — the surface is four reads", name)
			}
		}
	}
}
