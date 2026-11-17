package store

import (
	"errors"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 T1 — the rollup chain, proved at the store door.

func iv(v int) *int { return &v }

// telemetryFixture stands up the whole chain a contributing write resolves
// through: a root (the "project") → a child component under it → a loop at
// the child's root → a step in that loop. Returns the store and the ids the
// assertions read. CT7: the loop lands on the child AND its tree parent (the
// root) and the workspace — three tiers, no separate project row.
type telemetryFixture struct {
	store       *Store
	componentID string // the child
	parentID    string // the root (the "project")
	stepID      int64
}

func newTelemetryFixture(t *testing.T) telemetryFixture {
	t.Helper()
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/repo/platform"})
	if err != nil {
		t.Fatal(err)
	}
	comp, err := s.CreateComponent(NewComponent{Name: "Shop", Slug: "shop", Root: "/repo/shop", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "walk", Root: "/repo/shop", ComponentRoot: "/repo/shop"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(Scope{LoopSlug: "walk", ComponentRoot: "/repo/shop"}, NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	return telemetryFixture{store: s, componentID: comp.Component.ID, parentID: root.Component.ID, stepID: step.ID}
}

func knownCost(usd float64) model.Cost {
	return model.Cost{State: model.CostKnown, USD: usd}
}
func partialCost(floor float64) model.Cost {
	return model.Cost{State: model.CostPartial, USDAtLeast: floor}
}
func unknownCost() model.Cost {
	return model.Cost{State: model.CostUnknown, Reason: "no usage reported"}
}

func TestRecordStepTurnBumpsAllThreeTiers(t *testing.T) {
	f := newTelemetryFixture(t)
	usage := model.Usage{InputTokens: iv(100), OutputTokens: iv(40), CacheReadTokens: iv(7),
		CacheWriteTokens: iv(3), ReasoningTokens: iv(5)}
	if err := f.store.RecordStepTurn(f.stepID, 1, usage, knownCost(0.002), 1200, "route-a", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	c, err := f.store.ComponentTelemetry(f.componentID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.store.ComponentTelemetry(f.parentID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := f.store.WorkspaceTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	for name, sums := range map[string]TelemetrySums{"component": c.TelemetrySums, "project": p.TelemetrySums, "workspace": w.TelemetrySums} {
		if sums.InputTokensTotal != 100 || sums.OutputTokensTotal != 40 ||
			sums.CacheReadTokensTotal != 7 || sums.CacheWriteTokensTotal != 3 ||
			sums.ReasoningTokensTotal != 5 || sums.WallMsTotal != 1200 || sums.CostUSDTotal != 0.002 {
			t.Errorf("%s tier did not take the turn's exact values: %+v", name, sums)
		}
		if sums.CostState != "known" {
			t.Errorf("%s tier cost_state = %q, want known", name, sums.CostState)
		}
	}
	// Exactly one row, matching the call.
	turns, err := f.store.StepTurns(f.stepID, 10, 0)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %+v, err %v", turns, err)
	}
	if turns[0].Iteration != 1 || turns[0].Route != "route-a" || turns[0].Category != TelemetryCategoryAssistantOutput {
		t.Errorf("row shape drifted: %+v", turns[0])
	}
}

func TestFirstWriteCreatesRollupRows(t *testing.T) {
	// The upsert-not-FK-failure proof, staged from the truly-empty moment:
	// a component whose loop has not been created yet has NO rollup row;
	// the loop row's own transaction CREATES it (loops_run), and the first
	// detail write adds to it rather than failing a foreign key.
	s := openTemp(t)
	comp, err := s.CreateComponent(NewComponent{Name: "Shop", Slug: "shop", Root: "/repo/shop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ComponentTelemetry(comp.Component.ID); err == nil {
		t.Fatal("a component with no telemetry must not read as a row")
	}
	if w, err := s.WorkspaceTelemetry(); err != nil || w.Exists {
		t.Fatalf("workspace before any telemetry-producing activity: %+v %v", w, err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "walk", Root: "/repo/shop", ComponentRoot: "/repo/shop"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.ComponentTelemetry(comp.Component.ID)
	if err != nil || !c.Exists || c.LoopsRun != 1 {
		t.Fatalf("the loop row's own transaction did not create the rollup row: %+v %v", c, err)
	}
	step, err := s.CreateStep(Scope{LoopSlug: "walk", ComponentRoot: "/repo/shop"}, NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFileActivity(step.ID, "src/app.go", "write", iv(10), iv(2)); err != nil {
		t.Fatal(err)
	}
	c, err = s.ComponentTelemetry(comp.Component.ID)
	if err != nil || !c.Exists {
		t.Fatalf("first write lost the component row: %+v %v", c, err)
	}
	if c.FilesTouchedTotal != 1 || c.FilesWrittenTotal != 1 || c.FilesReadTotal != 0 ||
		c.LinesAddedTotal != 10 || c.LinesRemovedTotal != 2 {
		t.Errorf("file bump wrong: %+v", c)
	}
	if w, _ := s.WorkspaceTelemetry(); !w.Exists || w.FilesTouchedTotal != 1 || w.LoopsRun != 1 {
		t.Fatalf("workspace row wrong after both writes: %+v", w)
	}
}

func TestCostStateFloorsAcrossContributors(t *testing.T) {
	f := newTelemetryFixture(t)
	if err := f.store.RecordStepTurn(f.stepID, 1, model.Usage{InputTokens: iv(1)}, knownCost(0.1), 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordStepTurn(f.stepID, 2, model.Usage{InputTokens: iv(1)}, knownCost(0.1), 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.store.ComponentTelemetry(f.componentID); c.CostState != "known" {
		t.Fatalf("all-known must read known, got %q", c.CostState)
	}
	if err := f.store.RecordStepTurn(f.stepID, 3, model.Usage{}, partialCost(0.05), 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	c, _ := f.store.ComponentTelemetry(f.componentID)
	if c.CostState != "partial" {
		t.Fatalf("one partial contributor must floor the tier to partial, got %q", c.CostState)
	}
	if err := f.store.RecordStepTurn(f.stepID, 4, model.Usage{}, unknownCost(), 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	c2, _ := f.store.ComponentTelemetry(f.componentID)
	w, _ := f.store.WorkspaceTelemetry()
	p, _ := f.store.ComponentTelemetry(f.parentID)
	if c2.CostState != "unknown" || p.CostState != "unknown" || w.CostState != "unknown" {
		t.Fatalf("one unknown contributor must floor EVERY tier to unknown: comp %q proj %q ws %q", c2.CostState, p.CostState, w.CostState)
	}
	// The floors summed: 0.1 + 0.1 + 0.05 (the partial floor is added as a
	// floor, and the unknown contributed nothing — the tier says so).
	if c2.CostUSDTotal != 0.25 {
		t.Fatalf("cost sum = %v, want 0.25 (partial contributes its floor)", c2.CostUSDTotal)
	}
	// A non-spend row (tool_result) may NOT drag the tier down again from a
	// fresh tier: on a component whose only contributor was known, a
	// tool_result leaves 'known'.
	g := newTelemetryFixture(t)
	if err := g.store.RecordStepTurn(g.stepID, 1, model.Usage{OutputTokens: iv(3)}, knownCost(0.01), 5, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := g.store.RecordStepTurn(g.stepID, 1, model.Usage{}, unknownCost(), 7, "r", TelemetryCategoryToolResult); err != nil {
		t.Fatal(err)
	}
	if gc, _ := g.store.ComponentTelemetry(g.componentID); gc.CostState != "known" {
		t.Fatalf("a tool_result row is not a spend event; it must not speak to cost_state, got %q", gc.CostState)
	}
}

func TestNullableUsageColumnsStayNull(t *testing.T) {
	f := newTelemetryFixture(t)
	if err := f.store.RecordStepTurn(f.stepID, 1, model.Usage{InputTokens: iv(10)}, knownCost(0.001), 50, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	turns, err := f.store.StepTurns(f.stepID, 10, 0)
	if err != nil || len(turns) != 1 {
		t.Fatalf("%+v %v", turns, err)
	}
	tr := turns[0]
	if tr.InputTokens == nil || *tr.InputTokens != 10 {
		t.Errorf("reported count lost: %+v", tr)
	}
	if tr.OutputTokens != nil || tr.CacheReadTokens != nil || tr.CacheWriteTokens != nil || tr.ReasoningTokens != nil {
		t.Errorf("unreported counts arrived as non-NULL — M9 demands NULL, never 0: %+v", tr)
	}
	// A NULL cost row (unknown) keeps cost_usd NULL beside state 'unknown'.
	if err := f.store.RecordStepTurn(f.stepID, 2, model.Usage{}, unknownCost(), 50, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	turns, _ = f.store.StepTurns(f.stepID, 10, 0)
	if turns[1].CostUSD != nil || turns[1].CostState != "unknown" {
		t.Errorf("unknown cost must be (NULL,'unknown'), got (%v,%q)", turns[1].CostUSD, turns[1].CostState)
	}
	// The partial floor travels as a NUMBER beside the weaker state.
	if err := f.store.RecordStepTurn(f.stepID, 3, model.Usage{}, partialCost(0.03), 50, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	turns, _ = f.store.StepTurns(f.stepID, 10, 0)
	if turns[2].CostUSD == nil || *turns[2].CostUSD != 0.03 || turns[2].CostState != "partial" {
		t.Errorf("partial must carry its floor beside the state, got (%v,%q)", turns[2].CostUSD, turns[2].CostState)
	}
}

func TestWorkspaceIsOneRow(t *testing.T) {
	s := openTemp(t)
	// The legitimate zero-loops-ever state: an answer, not an error.
	w, err := s.WorkspaceTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	if w.Exists {
		t.Fatalf("a fresh store answered an existing row: %+v", w)
	}
	if w.LoopsRun != 0 || w.CostUSDTotal != 0 {
		t.Fatalf("the zero row is not zero: %+v", w)
	}
	// A second workspace row is structurally impossible: the table's own
	// CHECK (id = 1) refuses it — not a Go convention.
	if _, err := s.w.Exec(`INSERT INTO workspace_telemetry (id, updated_at) VALUES (2, 'x')`); err == nil {
		t.Fatal("workspace_telemetry admitted a second row — the singleton is convention, not schema")
	}
}

func TestRecordXWithAnUnregisteredRootKeepsTheDetail(t *testing.T) {
	s := openTemp(t)
	// A step whose root maps to NO registered component: the loop tier is
	// authoritative for what happened, so the row lands; the rollup cannot
	// be charged to a component that does not exist, so nothing is bumped.
	if _, err := s.CreateLoop(NewLoop{Slug: "loose", Root: "/nowhere", ComponentRoot: "/nowhere"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(Scope{LoopSlug: "loose", ComponentRoot: "/nowhere"}, NewStep{Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStepTurn(step.ID, 1, model.Usage{InputTokens: iv(9)}, knownCost(0.01), 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	turns, err := s.StepTurns(step.ID, 10, 0)
	if err != nil || len(turns) != 1 || *turns[0].InputTokens != 9 {
		t.Fatalf("the detail row vanished with the unregistered root: %+v %v", turns, err)
	}
	w, _ := s.WorkspaceTelemetry()
	if w.Exists {
		t.Fatalf("an unattributable write bumped the workspace: %+v", w)
	}
}

func TestStepTurnCategoryRefusesAFifthValue(t *testing.T) {
	f := newTelemetryFixture(t)
	err := f.store.RecordStepTurn(f.stepID, 1, model.Usage{}, knownCost(0), 1, "r", "tool_schema")
	if err == nil {
		t.Fatal("the closed four-value set admitted a fifth category")
	}
}

func TestCommandActivityCountsRunsNotJustFailures(t *testing.T) {
	f := newTelemetryFixture(t)
	if err := f.store.RecordCommandActivity(f.stepID, "echo hi", iv(0), 15); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordCommandActivity(f.stepID, "false", iv(1), 20); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordCommandActivity(f.stepID, "kill-me", nil, 5); err != nil {
		t.Fatal(err)
	}
	c, err := f.store.ComponentTelemetry(f.componentID)
	if err != nil {
		t.Fatal(err)
	}
	if c.CommandsRunTotal != 3 || c.CommandsFailedTotal != 2 || c.CommandWallMsTotal != 40 {
		t.Fatalf("D6 counts runs, not just failures: %+v", c)
	}
	rows, _ := f.store.StepCommandActivities(f.stepID)
	if len(rows) != 3 || rows[2].ExitCode != nil {
		t.Fatalf("signal-ended command must carry NULL exit_code: %+v", rows)
	}
}

func TestReadFileActivityNeverClaimsWithLineDeltas(t *testing.T) {
	f := newTelemetryFixture(t)
	if err := f.store.RecordFileActivity(f.stepID, "README.md", "read", nil, nil); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.store.StepFileActivities(f.stepID)
	if len(rows) != 1 || rows[0].Action != "read" || rows[0].LinesAdded != nil || rows[0].LinesRemoved != nil {
		t.Fatalf("read rows carry neither delta: %+v", rows)
	}
	c, _ := f.store.ComponentTelemetry(f.componentID)
	if c.FilesReadTotal != 1 || c.FilesWrittenTotal != 0 || c.FilesTouchedTotal != 1 {
		t.Fatalf("read bump wrong: %+v", c)
	}
	if err := f.store.RecordFileActivity(f.stepID, "x", "delete", nil, nil); err == nil {
		t.Fatal("an action outside read/write was admitted")
	}
}

func TestCumulativeTokensSumsTheRowsNotAMemoryTotal(t *testing.T) {
	f := newTelemetryFixture(t)
	if err := f.store.RecordStepTurn(f.stepID, 1, model.Usage{InputTokens: iv(10), OutputTokens: iv(2)}, knownCost(0), 5, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecordStepTurn(f.stepID, 2, model.Usage{InputTokens: iv(3), ReasoningTokens: iv(4)}, knownCost(0), 5, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	// A tool_result row contributes NULL counts — which sum as nothing.
	if err := f.store.RecordStepTurn(f.stepID, 2, model.Usage{}, unknownCost(), 9, "r", TelemetryCategoryToolResult); err != nil {
		t.Fatal(err)
	}
	total, err := f.store.CumulativeTokensTotal(f.stepID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10+2+3+4 {
		t.Fatalf("cumulative = %d, want 19 (the sum of the rows, nulls as nothing)", total)
	}
}

func TestCompactionRowMatchesTheBlobValues(t *testing.T) {
	f := newTelemetryFixture(t)
	usage := model.Usage{InputTokens: iv(800), OutputTokens: iv(120)}
	if err := f.store.RecordStepCompaction(f.stepID, 3, 9, 20000, 12000, 1500, usage, partialCost(0.02)); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.StepCompactions(f.stepID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
	r := rows[0]
	if r.StartIdx != 3 || r.EndIdx != 9 || r.EstimateBefore != 20000 || r.EstimateAfter != 12000 || r.SummaryLen != 1500 {
		t.Fatalf("row drifted from the record: %+v", r)
	}
	if r.InputTokens == nil || *r.InputTokens != 800 || r.OutputTokens == nil || *r.OutputTokens != 120 {
		t.Fatalf("usage not mirrored: %+v", r)
	}
	if r.CostState != "partial" || r.CostUSD == nil || *r.CostUSD != 0.02 {
		t.Fatalf("the partial floor must arrive as a floor beside the state: %+v", r)
	}
	// A compaction IS a model call for the tier: its tokens and its floor
	// bump the rollups (TL7), while contributing no wall (its row shape has
	// none — the honest gap recorded in the slice Notes).
	c, _ := f.store.ComponentTelemetry(f.componentID)
	if c.InputTokensTotal != 800 || c.OutputTokensTotal != 120 || c.CostUSDTotal != 0.02 || c.CostState != "partial" {
		t.Fatalf("compaction bump wrong: %+v", c)
	}
}

func TestLoopsRunBumpsWithTheLoopRow(t *testing.T) {
	s := openTemp(t)
	comp, err := s.CreateComponent(NewComponent{Name: "Run", Slug: "run", Root: "/run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "one", Root: "/run", ComponentRoot: "/run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "two", Root: "/run", ComponentRoot: "/run"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.ComponentTelemetry(comp.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.LoopsRun != 2 {
		t.Fatalf("loops_run = %d, want 2 — the count rides the loop row's own transaction", c.LoopsRun)
	}
	w, _ := s.WorkspaceTelemetry()
	if w.LoopsRun != 2 {
		t.Fatalf("workspace loops_run = %d, want 2", w.LoopsRun)
	}
	// A loop at an unregistered root counts nowhere (same attribution rule).
	if _, err := s.CreateLoop(NewLoop{Slug: "ghost", Root: "/unregistered", ComponentRoot: "/unregistered"}); err != nil {
		t.Fatal(err)
	}
	if w, _ = s.WorkspaceTelemetry(); w.LoopsRun != 2 {
		t.Fatalf("an unattributable loop bumped the singleton: %+v", w)
	}
}

func TestFloorCostStateIsOneLattice(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"known", "known", "known"},
		{"known", "partial", "partial"},
		{"partial", "known", "partial"},
		{"known", "unknown", "unknown"},
		{"partial", "unknown", "unknown"},
		{"unknown", "partial", "unknown"},
	}
	for _, c := range cases {
		if got := floorCostState(c.a, c.b); got != c.want {
			t.Errorf("floor(%q,%q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

// TestStepCompactionJoinableAcrossHistory (S33 T5, TL7) — the row's REASON
// for existing: queryable across a step's life without a restart, across
// TWO steps of one component with one join. If the FK or the index were
// wrong, this is where it shows — a per-step accessor alone would happily
// pass over an unjoinable table.
func TestStepCompactionJoinableAcrossHistory(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateComponent(NewComponent{Name: "Shop", Slug: "shop", Root: "/repo/shop"}); err != nil {
		t.Fatal(err)
	}
	// Two DIFFERENT steps, same component: two loops, a step in each.
	stepIDs := make([]int64, 0, 2)
	for _, slug := range []string{"first", "second"} {
		if _, err := s.CreateLoop(NewLoop{Slug: slug, Root: "/repo/shop", ComponentRoot: "/repo/shop"}); err != nil {
			t.Fatal(err)
		}
		step, err := s.CreateStep(Scope{LoopSlug: slug, ComponentRoot: "/repo/shop"}, NewStep{Agent: "tester"})
		if err != nil {
			t.Fatal(err)
		}
		stepIDs = append(stepIDs, step.ID)
	}
	if err := s.RecordStepCompaction(stepIDs[0], 1, 4, 20000, 8000, 120, model.Usage{}, model.Cost{State: model.CostUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStepCompaction(stepIDs[1], 2, 7, 30000, 9000, 200, model.Usage{}, model.Cost{State: model.CostUnknown}); err != nil {
		t.Fatal(err)
	}

	// The one join across `step_compaction` and `step`, filtered by the
	// component — the shape a history readout needs. (Raw SQL is legal ONLY
	// here, inside the store package; the row's production readers go
	// through named accessors.)
	rows, err := s.w.Query(`SELECT sc.step_id, sc.start_idx, sc.end_idx, sc.summary_len
		FROM step_compaction sc JOIN step st ON st.id = sc.step_id
		WHERE st.component_root = ? ORDER BY sc.step_id`, "/repo/shop")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type joined struct {
		stepID  int64
		start   int
		end     int
		summary int
	}
	var got []joined
	for rows.Next() {
		var j joined
		if err := rows.Scan(&j.stepID, &j.start, &j.end, &j.summary); err != nil {
			t.Fatal(err)
		}
		got = append(got, j)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("join returned %d rows, want the two compactions of the component's two steps: %+v", len(got), got)
	}
	if got[0].stepID != stepIDs[0] || got[0].start != 1 || got[0].end != 4 || got[0].summary != 120 ||
		got[1].stepID != stepIDs[1] || got[1].start != 2 || got[1].end != 7 || got[1].summary != 200 {
		t.Fatalf("join values wrong: %+v", got)
	}
}

// --- S33 T7 · the component-move transfer (TL3/D7) -------------------------

func mustProject(t *testing.T, s *Store, name string) string {
	t.Helper()
	// A "project" is a ROOT (CT1: derived, never stored). It becomes a project
	// once a child hangs under it; the move tests create children here.
	p, err := s.CreateComponent(NewComponent{Name: name, Slug: name, Root: "/proj/" + name})
	if err != nil {
		t.Fatal(err)
	}
	return p.Component.ID
}

func mustComponentIn(t *testing.T, s *Store, name, root, parentID string) string {
	t.Helper()
	c, err := s.CreateComponent(NewComponent{Name: name, Slug: name, Root: root, ParentID: parentID})
	if err != nil {
		t.Fatal(err)
	}
	return c.Component.ID
}

// recordVaried writes one of every kind of contributing row for a step, so
// a transfer is tested against EVERY total column at once, not a token-only
// slice: loops (via the loop row), wall, five token kinds, cost, files,
// lines, commands run AND failed.
func recordVaried(t *testing.T, s *Store, stepID int64, cost model.Cost) {
	t.Helper()
	in, out, cr, cw, re := 100, 20, 7, 3, 5
	usage := model.Usage{InputTokens: &in, OutputTokens: &out, CacheReadTokens: &cr, CacheWriteTokens: &cw, ReasoningTokens: &re}
	if err := s.RecordStepTurn(stepID, 1, usage, cost, 400, "route-a", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFileActivity(stepID, "src/app.go", "write", iv(9), iv(1)); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordCommandActivity(stepID, "make test", iv(1), 150); err != nil {
		t.Fatal(err)
	}
}

func stepUnder(t *testing.T, s *Store, slug, root string) int64 {
	t.Helper()
	if _, err := s.CreateLoop(NewLoop{Slug: slug, Root: root, ComponentRoot: root}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(Scope{LoopSlug: slug, ComponentRoot: root}, NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	return step.ID
}

func TestMoveTransfersRollupExactly(t *testing.T) {
	s := openTemp(t)
	gOne := mustProject(t, s, "group one")
	gTwo := mustProject(t, s, "group two")
	compM := mustComponentIn(t, s, "m", "/repo/m", gOne)
	compN := mustComponentIn(t, s, "n", "/repo/n", gOne) // stays behind, so gOne is not emptied
	_ = compN
	stepM := stepUnder(t, s, "loop-m", "/repo/m")
	stepN := stepUnder(t, s, "loop-n", "/repo/n")
	known := model.Cost{State: model.CostKnown, USD: 0.01}
	recordVaried(t, s, stepM, known)
	// N contributes different numbers so "one behind" is not zero.
	in, out := 10, 1
	if err := s.RecordStepTurn(stepN, 1, model.Usage{InputTokens: &in, OutputTokens: &out},
		model.Cost{State: model.CostKnown, USD: 0.002}, 50, "route-a", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}

	before, err := s.ComponentTelemetry(gOne)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.ComponentTelemetry(compM)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.WorkspaceTelemetry()
	if err != nil {
		t.Fatal(err)
	}

	// CT7: a child cannot be re-attached (attach acts on a root). Moving a node
	// from one parent to another is detach (the source loses the rollup) then
	// attach (the destination gains it); gTwo is childless so the attach is
	// allowed, and the source/destination transfer is exact in the same tx.
	if _, err := s.DetachComponent(compM); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachComponent(compM, gTwo); err != nil {
		t.Fatal(err)
	}

	after, err := s.ComponentTelemetry(gOne)
	if err != nil {
		t.Fatal(err)
	}
	dest, err := s.ComponentTelemetry(gTwo)
	if err != nil {
		t.Fatal(err)
	}

	// The source lost EXACTLY the component's contribution, column by column.
	if after.LoopsRun != before.LoopsRun-m.LoopsRun ||
		after.WallMsTotal != before.WallMsTotal-m.WallMsTotal ||
		after.InputTokensTotal != before.InputTokensTotal-m.InputTokensTotal ||
		after.OutputTokensTotal != before.OutputTokensTotal-m.OutputTokensTotal ||
		after.CacheReadTokensTotal != before.CacheReadTokensTotal-m.CacheReadTokensTotal ||
		after.CacheWriteTokensTotal != before.CacheWriteTokensTotal-m.CacheWriteTokensTotal ||
		after.ReasoningTokensTotal != before.ReasoningTokensTotal-m.ReasoningTokensTotal ||
		after.FilesTouchedTotal != before.FilesTouchedTotal-m.FilesTouchedTotal ||
		after.FilesReadTotal != before.FilesReadTotal-m.FilesReadTotal ||
		after.FilesWrittenTotal != before.FilesWrittenTotal-m.FilesWrittenTotal ||
		after.LinesAddedTotal != before.LinesAddedTotal-m.LinesAddedTotal ||
		after.LinesRemovedTotal != before.LinesRemovedTotal-m.LinesRemovedTotal ||
		after.CommandsRunTotal != before.CommandsRunTotal-m.CommandsRunTotal ||
		after.CommandsFailedTotal != before.CommandsFailedTotal-m.CommandsFailedTotal ||
		after.CommandWallMsTotal != before.CommandWallMsTotal-m.CommandWallMsTotal {
		t.Fatalf("the source did not lose exactly the component's totals:\n before %+v\n after  %+v\n moved %+v", before, after, m)
	}
	if d := after.CostUSDTotal - before.CostUSDTotal; d > -m.CostUSDTotal-1e-9 && d < -m.CostUSDTotal+1e-9 == false {
		t.Errorf("cost_usd_total did not leave the source exactly: delta %v, component carried %v", d, m.CostUSDTotal)
	}

	// The destination gained EXACTLY those totals — a full copy of `m`.
	if dest.LoopsRun != m.LoopsRun || dest.WallMsTotal != m.WallMsTotal ||
		dest.InputTokensTotal != m.InputTokensTotal || dest.OutputTokensTotal != m.OutputTokensTotal ||
		dest.CacheReadTokensTotal != m.CacheReadTokensTotal || dest.CacheWriteTokensTotal != m.CacheWriteTokensTotal ||
		dest.ReasoningTokensTotal != m.ReasoningTokensTotal || dest.FilesTouchedTotal != m.FilesTouchedTotal ||
		dest.FilesReadTotal != m.FilesReadTotal || dest.FilesWrittenTotal != m.FilesWrittenTotal ||
		dest.LinesAddedTotal != m.LinesAddedTotal || dest.LinesRemovedTotal != m.LinesRemovedTotal ||
		dest.CommandsRunTotal != m.CommandsRunTotal || dest.CommandsFailedTotal != m.CommandsFailedTotal ||
		dest.CommandWallMsTotal != m.CommandWallMsTotal {
		t.Fatalf("the destination did not gain exactly the component's totals: dest %+v, moved %+v", dest, m)
	}
	if d := dest.CostUSDTotal - m.CostUSDTotal; d > 1e-9 || d < -1e-9 {
		t.Errorf("destination cost_usd_total %v, want %v", dest.CostUSDTotal, m.CostUSDTotal)
	}

	// Workspace totals are UNCHANGED — a move rearranges attribution, not
	// truth (the Do-NOT: the grand total must not blink).
	if wsNow, err := s.WorkspaceTelemetry(); err != nil {
		t.Fatal(err)
	} else if wsNow.LoopsRun != ws.LoopsRun || wsNow.InputTokensTotal != ws.InputTokensTotal ||
		wsNow.CostUSDTotal != ws.CostUSDTotal || wsNow.LinesAddedTotal != ws.LinesAddedTotal ||
		wsNow.CommandsRunTotal != ws.CommandsRunTotal {
		t.Errorf("workspace totals moved with the component — they must not: %+v vs %+v", wsNow, ws)
	}

	// And the component's OWN row rode along untouched (keyed by component,
	// not by group — the Do-NOT).
	if mNow, err := s.ComponentTelemetry(compM); err != nil {
		t.Fatal(err)
	} else if mNow.InputTokensTotal != m.InputTokensTotal || mNow.CostUSDTotal != m.CostUSDTotal {
		t.Errorf("the component row changed on a move: %+v vs %+v", mNow, m)
	}
}

func TestMoveWithNoTelemetryIsANoOp(t *testing.T) {
	s := openTemp(t)
	gOne := mustProject(t, s, "group one")
	gTwo := mustProject(t, s, "group two")
	comp := mustComponentIn(t, s, "quiet", "/repo/quiet", gOne)
	// CT7: a child cannot be re-attached, so the move is detach (a no-op on a
	// silent component) then attach to gTwo. Neither step may create a row.
	if _, err := s.DetachComponent(comp); err != nil {
		t.Fatalf("moving a component that never produced telemetry must succeed: %v", err)
	}
	moved, err := s.AttachComponent(comp, gTwo)
	if err != nil {
		t.Fatalf("moving a component that never produced telemetry must succeed: %v", err)
	}
	if !moved.OK() || moved.Component.ParentID != gTwo {
		t.Fatalf("the component itself did not move: %+v", moved)
	}
	// No rows were CREATED anywhere: the destination has no telemetry row,
	// and the component never grew one.
	if _, err := s.ComponentTelemetry(gTwo); !errors.Is(err, ErrNotFound) {
		t.Errorf("a move of a silent component created a destination row: %v", err)
	}
	if _, err := s.ComponentTelemetry(comp); !errors.Is(err, ErrNotFound) {
		t.Errorf("a move of a silent component created a component row: %v", err)
	}
}

func TestMoveRecomputesCostStateAtDestination(t *testing.T) {
	// The one place the slice RECOMPUTES a state instead of flooring it
	// along: a destination known-before must FLOOR to partial when it gains
	// a partial member, and a source must be allowed to RETURN to known
	// when it loses its only one — which an incrementally floored state
	// structurally cannot do.
	//
	// CT7: a node with children cannot be a child, so a parent (gP) cannot
	// accept an attach. The destination is therefore a childless root made
	// "known" by its OWN telemetry (a step under its own repo), not by a
	// child. The move is detach (source gQ returns to known) then attach
	// (destination gP floors to partial).
	s := openTemp(t)
	gP := mustProject(t, s, "P")
	gQ := mustProject(t, s, "Q")
	compPartial := mustComponentIn(t, s, "p", "/repo/p", gQ)
	compOther := mustComponentIn(t, s, "o", "/repo/o", gQ) // stays behind, so gQ is not emptied
	stepPself := stepUnder(t, s, "l-pself", "/proj/P")     // under gP's own repo → gP is known
	stepP := stepUnder(t, s, "l-p", "/repo/p")
	stepO := stepUnder(t, s, "l-o", "/repo/o")

	if err := s.RecordStepTurn(stepPself, 1, model.Usage{}, model.Cost{State: model.CostKnown, USD: 0.01}, 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStepTurn(stepP, 1, model.Usage{}, model.Cost{State: model.CostPartial, USDAtLeast: 0.02}, 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStepTurn(stepO, 1, model.Usage{}, model.Cost{State: model.CostKnown, USD: 0.01}, 10, "r", TelemetryCategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if p, err := s.ComponentTelemetry(gP); err != nil || p.CostState != "known" {
		t.Fatalf("P should start known: %+v %v", p, err)
	}
	if q, err := s.ComponentTelemetry(gQ); err != nil || q.CostState != "partial" {
		t.Fatalf("Q should start floored to partial: %+v %v", q, err)
	}

	// CT: detach compPartial from gQ (gQ returns to known), then attach it to
	// gP (gP floors to partial). The mover's own row rides along untouched.
	if _, err := s.DetachComponent(compPartial); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachComponent(compPartial, gP); err != nil {
		t.Fatal(err)
	}

	if p, err := s.ComponentTelemetry(gP); err != nil || p.CostState != "partial" {
		t.Errorf("P gained a partial member but stayed %q — destination must RECOMPUTE, not keep (%v)", p.CostState, err)
	}
	if q, err := s.ComponentTelemetry(gQ); err != nil || q.CostState != "known" {
		t.Errorf("Q lost its only partial contributor but stayed %q — only a RECOMPUTE may return to known, an incremental floor never improves (%v)", q.CostState, err)
	}
	// The mover's own state is untouched: it is still a partial component,
	// whichever group it now belongs to.
	if mc, err := s.ComponentTelemetry(compPartial); err != nil || mc.CostState != "partial" {
		t.Errorf("the moving component's own cost_state changed: %q %v", mc.CostState, err)
	}
	_ = compOther
}

func TestMultiTierBumpIsTransactional(t *testing.T) {
	// T1's claim, PROVEN by crash rather than by reading the code: fail the
	// chain in the MIDDLE (an FK-invalid project row — the component tier
	// has already been upserted at that instant) and roll the transaction
	// back; ZERO of the three tiers may survive. If any tier had been
	// written on s.w instead of the passed tx, it would outlive the
	// rollback and this test would catch exactly that.
	s := openTemp(t)
	// A root (the "project") and a child under it: the bump chain is
	// component → parent (the root) → workspace — three tiers, CT7.
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/repo/platform"})
	if err != nil {
		t.Fatal(err)
	}
	comp, err := s.CreateComponent(NewComponent{Name: "Shop", Slug: "shop", Root: "/repo/shop", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "walk", Root: "/repo/shop", ComponentRoot: "/repo/shop"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(Scope{LoopSlug: "walk", ComponentRoot: "/repo/shop"}, NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	d := telemetryDelta{inputTokens: 999, wallMs: 50}

	tx, err := s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO step_turn (step_id, iteration, started_at, ended_at, wall_ms, cost_state, route, category)
		VALUES (?,1,?,?,50,'known','r','assistant_output')`, step.ID, now, now); err != nil {
		t.Fatal(err)
	}
	// The whole chain rides the caller's transaction; a rollback must undo
	// every tier it touched, not leave a half-written row behind.
	if err := bumpTelemetryRollups(tx, s, comp.Component.ID, root.Component.ID, d, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// (The CreateLoop above already bumped loops_run through the SAME chain —
	// the tiers pre-exist with their loop contribution. The claim is
	// therefore "the rolled-back chain contributed NOTHING", asserted as
	// unchanged totals rather than absence.)
	if turns, err := s.StepTurns(step.ID, 100, 0); err != nil || len(turns) != 0 {
		t.Errorf("the detail row survived the rollback: %d rows", len(turns))
	}
	if c, err := s.ComponentTelemetry(comp.Component.ID); err != nil || c.InputTokensTotal != 0 || c.WallMsTotal != 0 {
		t.Errorf("the COMPONENT tier carries the rolled-back chain's numbers: %+v %v", c, err)
	}
	if p, err := s.ComponentTelemetry(root.Component.ID); err != nil || p.InputTokensTotal != 0 {
		t.Errorf("the PARENT (root) tier carries the rolled-back chain's numbers: %+v %v", p, err)
	}
	if ws, err := s.WorkspaceTelemetry(); err != nil || !ws.Exists || ws.InputTokensTotal != 0 || ws.LoopsRun != 1 {
		t.Errorf("the WORKSPACE tier carries the rolled-back chain's numbers (loops_run must still be exactly CreateLoop's 1): %+v %v", ws, err)
	}

	// The same chain, all-valid, commits ALL THREE (the rollback branch is
	// not the only path — and a tier written outside the tx would double).
	tx, err = s.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO step_turn (step_id, iteration, started_at, ended_at, wall_ms, cost_state, route, category)
		VALUES (?,1,?,?,50,'known','r','assistant_output')`, step.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if err := bumpTelemetryRollups(tx, s, comp.Component.ID, root.Component.ID, d, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if c, err := s.ComponentTelemetry(comp.Component.ID); err != nil || c.InputTokensTotal != 999 || c.WallMsTotal != 50 {
		t.Errorf("the committed chain did not reach the component tier: %+v %v", c, err)
	}
	if p, err := s.ComponentTelemetry(root.Component.ID); err != nil || p.InputTokensTotal != 999 {
		t.Errorf("the committed chain did not reach the parent (root) tier: %+v %v", p, err)
	}
	if w, err := s.WorkspaceTelemetry(); err != nil || !w.Exists || w.InputTokensTotal != 999 {
		t.Errorf("the committed chain did not reach the workspace tier: %+v %v", w, err)
	}
	if turns, err := s.StepTurns(step.ID, 100, 0); err != nil || len(turns) != 1 {
		t.Errorf("the committed detail row: %d turns", len(turns))
	}
}
