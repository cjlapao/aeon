package protocol

// roadmap_graph_test.go — S27 T3 (CN7) at the socket: the graph doors answer
// through the table, refusals ride out as -31900 with the blocking law's
// sentence VERBATIM in `what` (the door adds no poetry), and the item answer
// now CARRIES the graph — `dependencies` with id, status and title, and a
// `dependents` count — because a plan blind to what waits on what is exactly
// the gap the graph exists to close.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func callRoadmapGraph(t *testing.T, tbl *Table, m MethodName, p any) (RoadmapItemResult, error) {
	t.Helper()
	var res any
	var err error
	switch m {
	case MethodRoadmapDepend:
		res, err = tbl.handleRoadmapDepend(context.Background(), nil, roadmapWire(t, p))
	case MethodRoadmapUndepend:
		res, err = tbl.handleRoadmapUndepend(context.Background(), nil, roadmapWire(t, p))
	case MethodRoadmapShow:
		res, err = tbl.handleRoadmapShow(context.Background(), nil, roadmapWire(t, p))
	default:
		t.Fatalf("no test arm for %s", m)
	}
	if err != nil {
		return RoadmapItemResult{}, err
	}
	b, _ := json.Marshal(res)
	var out RoadmapItemResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s answer %s: %v", m, b, err)
	}
	return out, nil
}

func TestDependDoorSentencesAndAnswers(t *testing.T) {
	tbl, _, root := roadmapActorMount(t, func(context.Context, *Session) Actor {
		return Actor{Kind: "user", ID: "1000"}
	})
	ctx := context.Background()
	add := func(title, r string) int64 {
		res, err := tbl.handleRoadmapAdd(ctx, nil, roadmapWire(t, RoadmapAddParams{
			ComponentRoot: r, Title: title, Provenance: "authored", PriorityState: "unknown",
		}))
		if err != nil {
			t.Fatalf("add %q: %v", title, err)
		}
		b, _ := json.Marshal(res)
		var ir RoadmapItemResult
		if err := json.Unmarshal(b, &ir); err != nil {
			t.Fatal(err)
		}
		return ir.Item.ID
	}
	a := add("downstream", root)
	b := add("upstream", root)

	// The shared answer's own law first: an edge with a zero end is not a
	// pair, and the refusal says what an edge is.
	if _, err := callRoadmapGraph(t, tbl, MethodRoadmapDepend, RoadmapDependParams{ID: a}); err == nil {
		t.Fatal("depend with a missing --on end was accepted")
	} else if !strings.Contains(err.Error(), "must both be positive row ids") {
		t.Errorf("positive-id refusal drifted: %v", err)
	}
	// The edge, and BOTH ends carry the graph in their answers.
	got, err := callRoadmapGraph(t, tbl, MethodRoadmapDepend, RoadmapDependParams{ID: a, DependsOn: b})
	if err != nil {
		t.Fatalf("depend: %v", err)
	}
	if len(got.Item.Dependencies) != 1 || got.Item.Dependencies[0].ID != b ||
		got.Item.Dependencies[0].Title != "upstream" || got.Item.Dependencies[0].Status != "idea" {
		t.Fatalf("the depend answer does not carry the graph: %+v", got.Item)
	}
	up, err := callRoadmapGraph(t, tbl, MethodRoadmapShow, RoadmapShowParams{ID: b})
	if err != nil {
		t.Fatalf("show upstream: %v", err)
	}
	if up.Item.Dependents != 1 || len(up.Item.Dependencies) != 0 {
		t.Fatalf("show of the depended-on item does not count its dependents: %+v", up.Item)
	}
	// The cycle, verbatim through -31900.
	_, err = callRoadmapGraph(t, tbl, MethodRoadmapDepend, RoadmapDependParams{ID: b, DependsOn: a})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrRoadmapRefused.Code {
		t.Fatalf("cycle: %v, want -31900", err)
	}
	if e.Data.What != "roadmap_dependency: the reverse edge already exists (a 2-cycle would deadlock the graph)" {
		t.Errorf("the cycle sentence did not ride out verbatim: %q", e.Data.What)
	}
	if !strings.Contains(e.Data.Todo, "roadmap.undepend") {
		t.Errorf("the refusal does not name the way out: %+v", e.Data)
	}
	// Undepend removes the edge, the answer shows it gone, and the second
	// attempt names the missing pair.
	got, err = callRoadmapGraph(t, tbl, MethodRoadmapUndepend, RoadmapUndependParams{ID: a, DependsOn: b})
	if err != nil {
		t.Fatalf("undepend: %v", err)
	}
	if len(got.Item.Dependencies) != 0 {
		t.Fatalf("the undepend answer still carries the edge: %+v", got.Item)
	}
	_, err = callRoadmapGraph(t, tbl, MethodRoadmapUndepend, RoadmapUndependParams{ID: a, DependsOn: b})
	if !errors.As(err, &e) || e.Code != ErrRoadmapRefused.Code ||
		!strings.Contains(e.Data.What, "there is no such edge to remove") {
		t.Fatalf("double undepend: %v", err)
	}
	// An identity key in the params is refused the same way every roadmap
	// write refuses it — before anything is read.
	if _, err := tbl.handleRoadmapDepend(ctx, nil, json.RawMessage(`{"id":1,"dependsOn":2,"actor":"root"}`)); err == nil ||
		!strings.Contains(err.Error(), "actor") {
		t.Fatalf("identity key through roadmap.depend: %v", err)
	}
}

func TestRoadmapShowCarriesDependencies(t *testing.T) {
	// The additive promise both ways: an item with no graph serializes with
	// NO `dependencies` key at all (pre-S27 bytes untouched — show.json stays
	// honest), while an item inside the graph names every open end.
	tbl, s, root := roadmapActorMount(t, func(context.Context, *Session) Actor {
		return Actor{Kind: "user", ID: "1000"}
	})
	ctx := context.Background()
	plain, err := callRoadmapGraph(t, tbl, MethodRoadmapShow, RoadmapShowParams{ID: addShowItem(t, tbl, ctx, root, "no edges")})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(plain)
	if strings.Contains(string(b), `"dependencies"`) {
		t.Errorf("an edgeless item grew a key: %s", b)
	}
	_ = plain
	a := addShowItem(t, tbl, ctx, root, "waits")
	c := addShowItem(t, tbl, ctx, root, "waited on")
	_, err = callRoadmapGraph(t, tbl, MethodRoadmapDepend, RoadmapDependParams{ID: a, DependsOn: c})
	if err != nil {
		t.Fatal(err)
	}
	_ = s
	view, err := callRoadmapGraph(t, tbl, MethodRoadmapShow, RoadmapShowParams{ID: a})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Item.Dependencies) != 1 || view.Item.Dependencies[0].Title != "waited on" {
		t.Fatalf("show without the graph: %+v", view.Item)
	}
	b, _ = json.Marshal(view)
	if !strings.Contains(string(b), `"dependents":0`) && !strings.Contains(string(b), "dependents") {
		t.Logf("dependents omitted on zero — omitempty, deliberate: %s", b)
	}
}

func addShowItem(t *testing.T, tbl *Table, ctx context.Context, root, title string) int64 {
	t.Helper()
	res, err := tbl.handleRoadmapAdd(ctx, nil, roadmapWire(t, RoadmapAddParams{
		ComponentRoot: root, Title: title, Provenance: "authored", PriorityState: "unknown",
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var ir RoadmapItemResult
	if err := json.Unmarshal(b, &ir); err != nil {
		t.Fatal(err)
	}
	return ir.Item.ID
}
