package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T9 (2/2) — the two measurements the slice owes before its docs flip:
//
//   * the WRITE VOLUME (brief §7): what a scripted multi-loop day actually
//     writes into the four detail tables, and what the capture calls cost.
//     The runtime caps ONE loop at runtime.MaxIterations (12), so an honest
//     "50–100 turn" day is several loops on one component — the shape a
//     working day really has, not one impossible loop;
//   * the THREE-STATE floor across loops and components: known/partial/
//     unknown deliberately mixed, read at every tier AND at the wire, the
//     lattice (known < partial < unknown) holding without a single
//     exception. A state improving — or a partial sum printing as exact —
//     fails HERE.

// runVolumeLoop walks one scripted loop: alternate write/bash tool turns
// for iterations, then the handoff. One fat opening exchange buys at least
// one compaction per loop (the same selection realloop pins).
func runVolumeLoop(t *testing.T, st *store.Store, withTelemetry bool, idx int, sink runtime.TelemetrySink) {
	t.Helper()
	root := "/repo/volume"
	loopSlug := volumeSlug(idx)
	step, err := st.CreateStep(store.Scope{LoopSlug: loopSlug, ComponentRoot: root}, store.NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	iptr := func(v int) *int { return &v }
	var script []measuredTurn
	for i := 0; i < runtime.MaxIterations-1; i++ {
		name, input := "write", `{"path":"a.md","content":"x"}`
		if i%2 == 1 {
			name, input = "bash", `{"command":"make test"}`
		}
		script = append(script, measuredTurn{
			// Every entry carries text: a compaction consumes whatever
			// turn comes next as its summary, and an empty summary
			// BLOCKS the loop. Any turn may be asked — so any turn can
			// answer.
			text:     "plan continues — step " + string(rune('0'+i%10)),
			toolUses: []model.Block{toolUse("t", name, input)},
			usage:    model.Usage{InputTokens: iptr(60 + i), OutputTokens: iptr(15)},
			cost:     model.Cost{State: model.CostKnown, USD: 0.001},
		})
	}
	script = append(script, measuredTurn{
		text:  "```json\n{\"status\":\"ok\",\"summary\":\"done\"}\n```",
		usage: model.Usage{InputTokens: iptr(70), OutputTokens: iptr(5)},
		cost:  model.Cost{State: model.CostKnown, USD: 0.002},
	})
	in := measuredModel{turns: script}
	tools := outcomeTools{outcomes: map[string]runtime.ToolOutcome{
		"write": {Content: "wrote a.md", Path: "a.md", LinesAdded: 3, LinesRemoved: 1},
		"bash":  {Content: "ok", ExitCode: iptr(0), WallMs: 5},
	}}
	handler := runtime.StepHandler{Name: "walk", System: "be terse"}
	msgs := append(runtime.OpeningTranscript(handler, "", "carry the plan", nil),
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock(strings.Repeat("long history ", 700))}},
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("acknowledged")}},
		model.Message{Role: model.RoleAssistant, Content: []model.Block{model.ThinkingBlock("last thought", "signed"), model.TextBlock("continuing")}},
	)
	res := runtime.RunLoop(context.Background(), &runtime.LoopState{
		Request: runtime.StepRequest{
			StepID: step.ID, Handler: handler, WorkspaceRoot: root,
			Route: model.Route{ID: "r", Provider: "anthropic", Model: "m", ContextWindow: 1200},
		},
		Messages: msgs,
		Manifest: &runtime.ContextManifest{MaxIterations: runtime.MaxIterations},
	}, runtime.LoopDeps{
		Client:      &in,
		Tools:       tools,
		Sink:        runtime.DiscardSink{},
		HandoffSpec: runtime.HandoffSpec{Required: []string{"status", "summary"}},
		Telemetry:   sink,
		Clock:       func() time.Time { return time.Now() },
	})
	if res.Outcome != runtime.OutcomeOK {
		t.Fatalf("volume loop %d must finish: %s — %s", idx, res.Outcome, res.Reason)
	}
}

func volumeSlug(i int) string { return "vol-walk-" + string(rune('a'+i)) }

func openVolumeStore(t *testing.T, loops int) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := "/repo/volume"
	if _, err := st.CreateComponent(store.NewComponent{Name: "Vol", Slug: "vol", Root: root}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < loops; i++ {
		if _, err := st.CreateLoop(store.NewLoop{Slug: volumeSlug(i), Root: root, ComponentRoot: root}); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func countDetailRows(t *testing.T, st *store.Store) (turns, comps, files, cmds int) {
	t.Helper()
	var steps []store.Step
	for _, slug := range []string{volumeSlug(0), volumeSlug(1), volumeSlug(2), volumeSlug(3), volumeSlug(4)} {
		ss, err := st.ListSteps(store.Scope{LoopSlug: slug, ComponentRoot: "/repo/volume"})
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, ss...)
	}
	for _, s := range steps {
		rows, err := st.StepTurns(s.ID, 500, 0)
		if err != nil {
			t.Fatal(err)
		}
		turns += len(rows)
		cs, err := st.StepCompactions(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		comps += len(cs)
		fs, err := st.StepFileActivities(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		files += len(fs)
		ds, err := st.StepCommandActivities(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		cmds += len(ds)
	}
	return
}

// TestWriteVolumeIsCheapAndBounded measures what a scripted day writes and
// what the capture costs, in the open. The plan's call was "no batching";
// this test does not defend that call — it STATES NUMBERS. The only
// assertion beyond "rows were written" is a loose per-row ceiling (5 ms)
// wide enough for any sane machine and narrow enough that a regression
// into per-row fsync-per-statement territory would trip it.
func TestWriteVolumeIsCheapAndBounded(t *testing.T) {
	const loops = 5
	captured := openVolumeStore(t, loops)
	t0 := time.Now()
	for i := 0; i < loops; i++ {
		runVolumeLoop(t, captured, true, i, newTelemetrySink(captured))
	}
	withCapture := time.Since(t0)

	baseline := openVolumeStore(t, loops)
	t0 = time.Now()
	for i := 0; i < loops; i++ {
		runVolumeLoop(t, baseline, false, i, nil)
	}
	withoutCapture := time.Since(t0)

	turns, comps, files, cmds := countDetailRows(t, captured)
	total := turns + comps + files + cmds
	if turns < 50 {
		t.Fatalf("only %d step_turn rows — the scripted day is not the 50-100 turn day this measurement is for", turns)
	}
	if comps == 0 {
		t.Errorf("no compaction rows: the scripted day bought none, so the measurement did not exercise them")
	}
	overhead := withCapture - withoutCapture
	if overhead < 0 {
		overhead = 0 // timer noise on a fast machine is not a finding
	}
	perRow := float64(overhead.Microseconds()) / float64(total)
	t.Logf("S33 write volume: %d loops → step_turn=%d step_compaction=%d step_file_activity=%d step_command_activity=%d (total %d rows); wall with capture %v, without %v, capture overhead %v (%.1f µs/row)",
		loops, turns, comps, files, cmds, total, withCapture.Round(time.Millisecond), withoutCapture.Round(time.Millisecond), overhead.Round(time.Millisecond), perRow)
	if perRow > 5000 {
		t.Errorf("capture costs %.1f ms/row — that is NOT 'a few indexed inserts'; open the batching follow-up this task forbids silently adding", perRow/1000)
	}
}

// TestThreeStatesHoldAtEveryTierAndAtTheWire builds a deliberate mix and
// checks the floor lattice (known < partial < unknown) at the component,
// project and workspace tiers AND on the telemetry.* wire responses.
func TestThreeStatesHoldAtEveryTierAndAtTheWire(t *testing.T) {
	f := newStreamFixture(t, nil)
	initConn(t, f.d, context.Background(), "c1", "reader")
	st := f.s
	iptr := func(v int) *int { return &v }

	mustProjectID := func(name string) string {
		// A "project" is a root component in the tree (S44-extra).
		comp, err := st.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: "/repo/" + name})
		if err != nil {
			t.Fatal(err)
		}
		return comp.Component.ID
	}
	prjA, prjB := mustProjectID("A"), mustProjectID("B")

	type spec struct {
		slug    string
		project string
		states  []model.CostState
	}
	specs := []spec{
		{"cmp-known", prjA, []model.CostState{model.CostKnown, model.CostKnown}},
		{"cmp-mixed", prjA, []model.CostState{model.CostKnown, model.CostPartial}},
		{"cmp-dark", prjB, []model.CostState{model.CostUnknown, model.CostUnknown}},
	}
	ids := map[string]string{}
	for _, sp := range specs {
		comp, err := st.CreateComponent(store.NewComponent{Name: sp.slug, Slug: sp.slug, Root: "/repo/" + sp.slug, ParentID: sp.project})
		if err != nil {
			t.Fatal(err)
		}
		ids[sp.slug] = comp.Component.ID
		if _, err := st.CreateLoop(store.NewLoop{Slug: sp.slug + "-loop", Root: "/repo/" + sp.slug, ComponentRoot: comp.Component.Root}); err != nil {
			t.Fatal(err)
		}
		step, err := st.CreateStep(store.Scope{LoopSlug: sp.slug + "-loop", ComponentRoot: comp.Component.Root}, store.NewStep{Agent: "tester"})
		if err != nil {
			t.Fatal(err)
		}
		for i, cs := range sp.states {
			usage := model.Usage{InputTokens: iptr(100), OutputTokens: iptr(10)}
			cost := model.Cost{State: cs}
			switch cs {
			case model.CostKnown:
				cost.USD = 0.01
			case model.CostPartial:
				cost.USDAtLeast = 0.02
				cost.Missing = []string{"cache_write"}
			}
			if err := st.RecordStepTurn(step.ID, i+1, usage, cost, 50, "r", store.TelemetryCategoryAssistantOutput); err != nil {
				t.Fatal(err)
			}
		}
	}

	wantTier := map[string]string{"cmp-known": "known", "cmp-mixed": "partial", "cmp-dark": "unknown"}
	for _, sp := range specs {
		got, err := st.ComponentTelemetry(ids[sp.slug])
		if err != nil {
			t.Fatal(err)
		}
		if got.CostState != wantTier[sp.slug] {
			t.Errorf("component %s tier: costState %q, want %q (the floor of known/partial/unknown is a lattice, not a preference)", sp.slug, got.CostState, wantTier[sp.slug])
		}
		// And the same fact at the wire, from the daemon's own door.
		wire := dispatchJSON(t, f, "c1", protocol.MethodTelemetryComponent, protocol.TelemetryComponentParams{ComponentID: ids[sp.slug]})
		if wire["costState"] != wantTier[sp.slug] {
			t.Errorf("wire component %s: costState %v", sp.slug, wire["costState"])
		}
	}

	// Projects: A folds known+partial → partial; B is unknown.
	for _, tc := range []struct {
		id, want string
	}{
		{prjA, "partial"}, {prjB, "unknown"},
	} {
		got, err := st.ComponentTelemetry(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if got.CostState != tc.want {
			t.Errorf("project %s: costState %q, want %q", tc.id, got.CostState, tc.want)
		}
		wire := dispatchJSON(t, f, "c1", protocol.MethodTelemetryProject, protocol.TelemetryProjectParams{ProjectID: tc.id})
		if wire["costState"] != tc.want {
			t.Errorf("wire project %s: costState %v", tc.id, wire["costState"])
		}
	}

	// Workspace: the whole board's floor is unknown.
	ws, err := st.WorkspaceTelemetry()
	if err != nil {
		t.Fatal(err)
	}
	if ws.CostState != "unknown" {
		t.Errorf("workspace: costState %q, want unknown — one unknown member holds the whole board honest", ws.CostState)
	}
	wire := dispatchJSON(t, f, "c1", protocol.MethodTelemetryWorkspace, struct{}{})
	if wire["costState"] != "unknown" {
		t.Errorf("wire workspace: costState %v", wire["costState"])
	}
	// The unknown wire response claims NO exact figure (costUsd is only a
	// sum of what was known; with costState unknown the sentence the app
	// prints is "no figure claimed" — the number's presence beside an
	// unknown state is the drift P31 forbids, so pin what IS there).
	if b, err := json.Marshal(wire); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(b), `"costState":"unknown"`) {
		t.Errorf("wire workspace lost its state word: %s", b)
	}
}
