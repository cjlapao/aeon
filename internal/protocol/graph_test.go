package protocol

// graph_test.go — the socket doors' behaviour, asserted over a fake
// service: registration is conditional on the seam (V13), refusals name
// the missing argument (T5 §5), and the four-state answer passes through
// VERBATIM — the handler's whole job is marshaling, and these tests are
// the proof that nothing between the wire and the resolver adds a word.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/graph"
)

type fakeGraph struct {
	calls []string
	exp   *graph.ExplainResult
	path  *graph.PathResult
	query *graph.QueryResult
	st    *graph.Status
	err   error
}

func (f *fakeGraph) record(s string) { f.calls = append(f.calls, s) }

func (f *fakeGraph) Explain(ctx context.Context, root, symbol string) (*graph.ExplainResult, error) {
	f.record("explain " + root + " " + symbol)
	return f.exp, f.err
}
func (f *fakeGraph) Path(ctx context.Context, root, from, to string) (*graph.PathResult, error) {
	f.record("path " + root + " " + from + " " + to)
	return f.path, f.err
}
func (f *fakeGraph) Query(ctx context.Context, root string, q graph.QueryFilter) (*graph.QueryResult, error) {
	f.record("query " + root + " " + q.Kind)
	return f.query, f.err
}
func (f *fakeGraph) Build(ctx context.Context, root string) (*graph.BuildReport, error) {
	f.record("build " + root)
	return &graph.BuildReport{Root: root, Files: 1, Nodes: 3, Edges: 2}, f.err
}
func (f *fakeGraph) RebuildFile(ctx context.Context, root, absPath string) (*graph.BuildReport, error) {
	f.record("rebuild " + root + " " + absPath)
	return &graph.BuildReport{Root: root}, f.err
}
func (f *fakeGraph) RebuildFileAt(ctx context.Context, root, absPath string) (*graph.BuildReport, error) {
	f.record("reparse " + root + " " + absPath)
	return &graph.BuildReport{Root: root}, f.err
}
func (f *fakeGraph) Delete(ctx context.Context, root string) (int, error) {
	f.record("delete " + root)
	return 7, f.err
}

// S54 T1 enumerated adaptation: the seam grew the two audited twins the
// operation doors walk. The fake records the same verb PLUS the actor, so
// S54's own tests can assert what the door stamped.
func (f *fakeGraph) RebuildFileAtAudited(ctx context.Context, root, absPath string, a graph.Actor) (*graph.BuildReport, error) {
	f.record("reparse-audited " + root + " " + absPath + " " + a.Kind + ":" + a.ID)
	return &graph.BuildReport{Root: root}, f.err
}
func (f *fakeGraph) DeleteAudited(ctx context.Context, root string, a graph.Actor) (int, error) {
	f.record("delete-audited " + root + " " + a.Kind + ":" + a.ID)
	return 7, f.err
}
func (f *fakeGraph) Status(ctx context.Context, root string) (*graph.Status, error) {
	f.record("status " + root)
	return f.st, f.err
}
func (f *fakeGraph) Roots(ctx context.Context) ([]string, error) {
	f.record("roots")
	return []string{"/a", "/b"}, f.err
}

func graphHas(tab *Table, name MethodName) bool {
	for _, m := range append(tab.Methods(), tab.PublicMethods()...) {
		if m == name {
			return true
		}
	}
	return false
}

func graphCall(t *testing.T, tab *Table, name MethodName, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	switch name {
	case MethodGraphExplain:
		return tab.handleGraphExplain(ctx, nil, raw)
	case MethodGraphPath:
		return tab.handleGraphPath(ctx, nil, raw)
	case MethodGraphQuery:
		return tab.handleGraphQuery(ctx, nil, raw)
	case MethodGraphBuild:
		return tab.handleGraphBuild(ctx, &Session{}, raw)
	case MethodGraphReparse:
		return tab.handleGraphReparse(ctx, &Session{}, raw)
	case MethodGraphDelete:
		return tab.handleGraphDelete(ctx, &Session{}, raw)
	case MethodGraphStatus:
		return tab.handleGraphStatus(ctx, nil, raw)
	case MethodGraphRoots:
		return tab.handleGraphRoots(ctx, nil, raw)
	}
	t.Fatalf("unknown method %s", name)
	return nil, nil
}

func TestGraphMethodsAbsentWithoutSeam(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tab.RegisterGraph() {
		t.Fatal("RegisterGraph reported success with a nil seam")
	}
	for _, name := range []MethodName{MethodGraphExplain, MethodGraphPath, MethodGraphQuery, MethodGraphBuild, MethodGraphReparse, MethodGraphDelete, MethodGraphStatus, MethodGraphRoots} {
		if graphHas(tab, name) {
			t.Errorf("%s is registered without the seam — V13: absent, not registered-and-refusing", name)
		}
	}
}

func TestGraphMethodsRegisterWithSeam(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.Graph = &fakeGraph{}
	// S54 T1 enumerated adaptation: the three OPERATIONS (build, reparse,
	// delete) register only over the seam AND a stamping source (their
	// audit must be able to name a person); the five reads keep the old
	// gate. With both wired, the whole family is in — the absence half is
	// S54's own registration test, not this S30 one.
	tab.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "4242"} }
	if !tab.RegisterGraph() {
		t.Fatal("RegisterGraph refused with a wired seam")
	}
	for _, name := range []MethodName{MethodGraphExplain, MethodGraphPath, MethodGraphQuery, MethodGraphBuild, MethodGraphReparse, MethodGraphDelete, MethodGraphStatus, MethodGraphRoots} {
		if !graphHas(tab, name) {
			t.Errorf("%s missing from the table", name)
		}
	}
}

// TestGraphRefusalsNameTheMissingArgument — T5 §5 on the wire: the root
// is required on every method, the symbols on the reads, each refusal
// naming the method and the thing refused.
func TestGraphRefusalsNameTheMissingArgument(t *testing.T) {
	f := &fakeGraph{}
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.Graph = f
	tab.RegisterGraph()

	cases := []struct {
		name   MethodName
		params any
		want   string
	}{
		{MethodGraphExplain, GraphExplainParams{Symbol: "X"}, "componentRoot"},
		{MethodGraphExplain, GraphExplainParams{ComponentRoot: "/r"}, "symbol"},
		{MethodGraphPath, GraphPathParams{From: "A", To: "B"}, "componentRoot"},
		{MethodGraphPath, GraphPathParams{ComponentRoot: "/r", To: "B"}, "from"},
		{MethodGraphPath, GraphPathParams{ComponentRoot: "/r", From: "A"}, "to"},
		{MethodGraphQuery, GraphQueryParams{}, "componentRoot"},
		{MethodGraphBuild, GraphBuildParams{}, "componentRoot"},
		{MethodGraphReparse, GraphReparseParams{ComponentRoot: "/r"}, "path"},
		{MethodGraphDelete, GraphDeleteParams{}, "componentRoot"},
		{MethodGraphStatus, GraphStatusParams{}, "componentRoot"},
	}
	for i, c := range cases {
		_, err := graphCall(t, tab, c.name, c.params)
		if err == nil {
			t.Errorf("case %d (%s): want refusal, got nil", i, c.name)
			continue
		}
		// The specifics ride Data.What (T10/C9's three-part shape) — the
		// house rule for naming the thing refused.
		var e *Error
		if !errors.As(err, &e) || !strings.Contains(e.Data.What, c.want) {
			t.Errorf("case %d (%s): refusal %v does not name %q in its What", i, c.name, err, c.want)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("the fake service was called for refused params: %v", f.calls)
	}
}

// TestFourStatesPassThroughVerbatim — each state reaches the wire the
// resolver decided it, with the fake able to emit any of them.
func TestFourStatesPassThroughVerbatim(t *testing.T) {
	f := &fakeGraph{}
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.Graph = f
	tab.RegisterGraph()

	f.exp = &graph.ExplainResult{State: graph.StateFound, Node: &graph.NodeRef{ID: 1, Symbol: "order.Foo", Kind: "function", FilePath: "f.go", StartLine: 1, EndLine: 2}}
	res, err := graphCall(t, tab, MethodGraphExplain, GraphExplainParams{ComponentRoot: "/r", Symbol: "Foo"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(*graph.ExplainResult).State; got != graph.StateFound {
		t.Errorf("found state mangled: %q", got)
	}
	f.exp = &graph.ExplainResult{State: graph.StateNotBuilt}
	res, _ = graphCall(t, tab, MethodGraphExplain, GraphExplainParams{ComponentRoot: "/r", Symbol: "Foo"})
	if got := res.(*graph.ExplainResult).State; got != graph.StateNotBuilt {
		t.Errorf("not-built state mangled: %q", got)
	}
	f.path = &graph.PathResult{State: graph.StateAmbiguous, Candidates: []graph.NodeRef{{ID: 2, Symbol: "a.Validate"}, {ID: 3, Symbol: "b.Validate"}}}
	res, _ = graphCall(t, tab, MethodGraphPath, GraphPathParams{ComponentRoot: "/r", From: "A", To: "Validate"})
	if pr := res.(*graph.PathResult); pr.State != graph.StateAmbiguous || len(pr.Candidates) != 2 {
		t.Errorf("ambiguous path mangled: %+v", pr)
	}
	f.query = &graph.QueryResult{State: graph.StateNotFound}
	res, _ = graphCall(t, tab, MethodGraphQuery, GraphQueryParams{ComponentRoot: "/r"})
	if res.(*graph.QueryResult).State != graph.StateNotFound {
		t.Error("not-found query mangled")
	}
}

// TestGraphHandlersCallTheServiceMethods — the same-resolver assertion at
// this door: every method routes to the ONE service value, by its own
// method name and argument order (the tool's twin of this test lives in
// internal/daemon, which can see both doors).
func TestGraphHandlersCallTheServiceMethods(t *testing.T) {
	f := &fakeGraph{}
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.Graph = f
	// S54 T1 enumerated adaptation: the two inline operations now route to
	// the service's AUDITED twins (the audit rides the store transaction),
	// and the fake records the actor the door stamped.
	tab.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "4242"} }
	tab.RegisterGraph()
	f.st = &graph.Status{Root: "/r", Built: true}

	if _, err := graphCall(t, tab, MethodGraphExplain, GraphExplainParams{ComponentRoot: "/r", Symbol: "Foo"}); err != nil {
		t.Fatal(err)
	}
	// S64: graph.build no longer calls the service inline — it starts an
	// async worker. Its door is tested in asyncworkers_test.go
	// (TestGraphBuildStartsAWorkerNotABuildInline); reparse still answers
	// with a BuildReport and is covered by its own case below.
	if _, err := graphCall(t, tab, MethodGraphReparse, GraphReparseParams{ComponentRoot: "/r", Path: "a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graphCall(t, tab, MethodGraphDelete, GraphDeleteParams{ComponentRoot: "/r"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graphCall(t, tab, MethodGraphRoots, struct{}{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"explain /r Foo", "reparse-audited /r a.go user:4242", "delete-audited /r user:4242", "roots"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v — the socket must call the service, not reimplement it", f.calls, want)
	}
}
