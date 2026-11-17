package protocol

// S26 T5 — the roadmap's protocol-side gates. The behavior lives under
// internal/roadmap (one implementation, CG9's extension) and is exercised
// there; these tests pin what this layer may never do: register a verb
// without the initialize gate, mutate an existing method, answer from a
// half-wired seam, or let a shapeless request reach the door.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cjlapao/aeon/internal/roadmap"
)

func mustRoadmapParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	return b
}

// TestRoadmapMethodsAreAdditiveToProtocolVersion is the standing rule from
// the inside: the roadmap's nine names join the table; nothing that was
// already there moved — a sampled spine keeps its exact requires-initialize.
func TestRoadmapMethodsAreAdditiveToProtocolVersion(t *testing.T) {
	tbl := newTestTable(t)
	have := map[MethodName]bool{}
	for _, n := range tbl.Methods() {
		have[n] = true
	}
	for _, n := range []MethodName{
		MethodRoadmapAdd, MethodRoadmapList, MethodRoadmapShow,
		MethodRoadmapCancel, MethodRoadmapEstimate, MethodRoadmapPriority,
		MethodRoadmapIdeate, MethodRoadmapPlan, MethodRoadmapMaterialize,
		// S27 T3: the graph's edges — additive, and they ride the same gate.
		MethodRoadmapDepend, MethodRoadmapUndepend,
	} {
		if !have[n] {
			t.Errorf("%s is not registered — P34's surface is missing a verb", n)
		}
	}
	before := map[MethodName]bool{
		MethodInitialize: false,
		MethodPing:       true,
		MethodLoopStart:  true,
		MethodQueueList:  true,
		MethodPolicyList: true,
	}
	for n, wantGate := range before {
		m, ok := tbl.methods[n]
		if !ok {
			t.Fatalf("%s vanished from the table — additive means additive", n)
		}
		if m.RequiresInitialize != wantGate {
			t.Errorf("%s RequiresInitialize = %v, want %v (additive must not rewrite)", n, m.RequiresInitialize, wantGate)
		}
	}
}

func TestEveryRoadmapMethodRequiresInitialize(t *testing.T) {
	tbl := newTestTable(t)
	for _, n := range []MethodName{
		MethodRoadmapAdd, MethodRoadmapList, MethodRoadmapShow,
		MethodRoadmapCancel, MethodRoadmapEstimate, MethodRoadmapPriority,
		MethodRoadmapIdeate, MethodRoadmapPlan, MethodRoadmapMaterialize,
		// S27 T3: the graph's edges — additive, and they ride the same gate.
		MethodRoadmapDepend, MethodRoadmapUndepend,
	} {
		m, ok := tbl.methods[n]
		if !ok {
			t.Fatalf("%s is not registered", n)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s answers before Initialize — every roadmap method rides the gate queue.* rides", n)
		}
	}
}

// TestRoadmapHandlersRefuseLoudlyWhenUnwired pins the degradation path for
// BOTH halves: the door-backed reads/writes (t.Roadmap nil) and the three
// daemon seams (nil func fields) all answer ErrStoreUnavailable — a named
// error, never a zero-value item that reads like an empty roadmap.
func TestRoadmapHandlersRefuseLoudlyWhenUnwired(t *testing.T) {
	tbl := newTestTable(t) // Roadmap and the seams nil by construction
	_, err := tbl.handleRoadmapList(context.Background(), nil, mustRoadmapParams(t, RoadmapListParams{ComponentRoot: "/srv/projects/ledger"}))
	if err == nil {
		t.Fatal("an unwired roadmap answered — it must refuse")
	}
	e, ok := err.(*Error)
	if !ok || e.Code != ErrStoreUnavailable.Code {
		t.Fatalf("unwired roadmap.list err = %v, want ErrStoreUnavailable", err)
	}
	for name, call := range map[string]func() error{
		"ideate": func() error {
			_, e := tbl.handleRoadmapIdeate(context.Background(), nil, mustRoadmapParams(t, RoadmapIdeateParams{ID: 1, WorkspaceRoot: "/w"}))
			return e
		},
		"plan": func() error {
			_, e := tbl.handleRoadmapPlan(context.Background(), nil, mustRoadmapParams(t, RoadmapPlanParams{ID: 1, WorkspaceRoot: "/w"}))
			return e
		},
		"materialize": func() error {
			_, e := tbl.handleRoadmapMaterialize(context.Background(), nil, mustRoadmapParams(t, RoadmapMaterializeParams{ID: 1, Blueprint: "dev-loop", Goal: "g", WorkspaceRoot: "/w"}))
			return e
		},
	} {
		err := call()
		if err == nil {
			t.Fatalf("an unwired roadmap.%s answered — the seam must refuse loudly (P34's honest absence)", name)
		}
		if e, ok := err.(*Error); !ok || e.Code != ErrStoreUnavailable.Code {
			t.Errorf("unwired roadmap.%s err = %v, want ErrStoreUnavailable", name, err)
		}
	}
}

// TestRoadmapHandlersRefuseShapelessRequests pins that parameter law is
// decided BEFORE the door and before the seams — the D5 provenance rule
// and D3's all-or-nothing estimate are shape laws here, and the refusal
// must not depend on whether a store exists.
func TestRoadmapHandlersRefuseShapelessRequests(t *testing.T) {
	tbl := newTestTable(t)
	cases := []struct {
		name string
		call func() error
	}{
		{"add without provenance (D5 has no default)", func() error {
			_, e := tbl.handleRoadmapAdd(context.Background(), nil, mustRoadmapParams(t, RoadmapAddParams{ComponentRoot: "/r", Title: "t"}))
			return e
		}},
		{"add without a title", func() error {
			_, e := tbl.handleRoadmapAdd(context.Background(), nil, mustRoadmapParams(t, RoadmapAddParams{ComponentRoot: "/r", Provenance: "authored"}))
			return e
		}},
		{"add with a half estimate (D3)", func() error {
			m := 1
			_, e := tbl.handleRoadmapAdd(context.Background(), nil, mustRoadmapParams(t, RoadmapAddParams{ComponentRoot: "/r", Title: "t", Provenance: "authored", EstimateMin: &m}))
			return e
		}},
		{"list without a root", func() error {
			_, e := tbl.handleRoadmapList(context.Background(), nil, mustRoadmapParams(t, RoadmapListParams{}))
			return e
		}},
		{"show without an id", func() error {
			_, e := tbl.handleRoadmapShow(context.Background(), nil, mustRoadmapParams(t, RoadmapShowParams{}))
			return e
		}},
		{"cancel without an id", func() error {
			_, e := tbl.handleRoadmapCancel(context.Background(), nil, mustRoadmapParams(t, RoadmapCancelParams{}))
			return e
		}},
		{"estimate without a basis (a bare figure, D3)", func() error {
			_, e := tbl.handleRoadmapEstimate(context.Background(), nil, mustRoadmapParams(t, RoadmapEstimateParams{ID: 1, EstimateMin: 1, EstimateMax: 2}))
			return e
		}},
		{"priority without a state (D2 has no default)", func() error {
			_, e := tbl.handleRoadmapPriority(context.Background(), nil, mustRoadmapParams(t, RoadmapPriorityParams{ID: 1}))
			return e
		}},
		{"ideate without a workspace", func() error {
			_, e := tbl.handleRoadmapIdeate(context.Background(), nil, mustRoadmapParams(t, RoadmapIdeateParams{ID: 1}))
			return e
		}},
		{"plan without a workspace", func() error {
			_, e := tbl.handleRoadmapPlan(context.Background(), nil, mustRoadmapParams(t, RoadmapPlanParams{ID: 1}))
			return e
		}},
		{"materialize without a goal", func() error {
			_, e := tbl.handleRoadmapMaterialize(context.Background(), nil, mustRoadmapParams(t, RoadmapMaterializeParams{ID: 1, Blueprint: "dev-loop", WorkspaceRoot: "/w"}))
			return e
		}},
	}
	for _, c := range cases {
		err := c.call()
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s: err = %v, want ErrBadParams — shape must be refused before any seam", c.name, err)
		}
	}
}

// TestRoadmapRefusalIsRegisteredAndSaidInThrees pins that the roadmap
// speaks with a real catalogued code: three-part Data (what/means/todo)
// all non-empty, the code in the registry.
func TestRoadmapRefusalIsRegisteredAndSaidInThrees(t *testing.T) {
	found := false
	for _, e := range ErrorRegistry() {
		if e.Code == ErrRoadmapRefused.Code {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ErrRoadmapRefused (%d) is not in ErrorRegistry", ErrRoadmapRefused.Code)
	}
	r := roadmapRefusal("roadmap item 13 is running — a loop in flight is the loop's own record")
	if r.Data.What == "" || r.Data.Means == "" || r.Data.Todo == "" {
		t.Fatalf("refusal %v is not said in threes", r)
	}
}

// TestUnknownPriorityRendersHonestlyEverywhere is D2's wire law asserted at
// the ONE renderer every surface reads: a never-prioritized item says the
// word "unknown" in priorityState, carries NO label field as a zero
// (pointer absent, not ""), and renders no estimate as three nulls — not
// zero, not blank. The app and the CLI print these values verbatim; this
// is where the honest three states are decided once.
func TestUnknownPriorityRendersHonestlyEverywhere(t *testing.T) {
	v := roadmapItemView(roadmapItemForTest())
	if v.PriorityState != "unknown" {
		t.Errorf("priorityState = %q — the word for never-prioritized is `unknown`, never blank and never a number (D2)", v.PriorityState)
	}
	if v.PriorityLabel != nil {
		t.Errorf("priorityLabel = %q — an unknown-priority item carries no label to render", *v.PriorityLabel)
	}
	if v.EstimateMin != nil || v.EstimateMax != nil || v.EstimateBasis != nil {
		t.Error("an unsized item renders an estimate — D3: the three are all-present-or-all-absent")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["priorityState"] != "unknown" {
		t.Errorf(`wire says %v — "unknown" must survive JSON as the WORD`, m["priorityState"])
	}
	for _, k := range []string{"estimateMin", "estimateMax", "estimateBasis"} {
		got, present := m[k]
		if !present || got != nil {
			t.Errorf("%s = %v (present=%v) — unsized is three JSON nulls, never a bare 0 and never a missing key that a client defaults", k, got, present)
		}
	}
	if p, ok := m["priorityLabel"]; !ok || p != nil {
		t.Errorf("priorityLabel = %v — the label is JSON null for unknown, never an empty string a UI might render as a dash", p)
	}
}

func roadmapItemForTest() roadmap.Item {
	return roadmap.Item{
		ID: 1, ComponentRoot: "/srv/projects/ledger", Title: "export monthly statements",
		Status: roadmap.StatusIdea, Priority: roadmap.PriorityUnknown,
		Provenance: roadmap.ProvenanceAuthored,
		CreatedAt:  "2026-10-29T09:00:00Z", UpdatedAt: "2026-10-29T09:00:00Z",
	}
}
