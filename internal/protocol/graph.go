package protocol

// graph.go — the app's door onto the code graph (S30 T5, P34).
//
// This file adds JSON-RPC marshaling around internal/graph's resolvers
// and NOTHING else (D15's rule, read at the retrieval twin it copies):
// the tool and these methods hold the same *graph.Service value, wired
// once in the daemon's construction, and TestGraphToolAndSocketShareOneService
// (internal/daemon) asserts the identity. A second query implementation
// is exactly what the P34 amendment exists to prevent.
//
// # Why WRITES ride this socket, and the precedent that allows it
//
// PL15 keeps POLICY authoring CLI-only because a write path on the
// endpoint hands a caller the means to widen the guards written to
// constrain it. Graph writes are a different trust tier, and the
// precedent is this package's own roadmap.go: the loop-state writes there
// (goal/task transitions) are ordinary loop CONTENT, judged by the policy
// engine, not by who typed them. Graph rows are none of that: build,
// reparse and delete maintain an INDEX THE OPERATOR OWNS over their own
// working tree — the same category as `retrieval.reindex`, which S22 T8
// already put on the socket as explicit, deliberate maintenance (V21:
// registration puts a door; only a call walks through it). No policy row
// governs which files get parsed, because parsing is not an action an
// agent takes against a resource; it is what an index of a directory IS.
// The graph methods therefore open on the socket, and NO policy authoring
// does — TestNoPolicyAuthoringReachesTheNarrowSurface still stands, and
// nothing here touches the policy tables.
//
// # Four-state, both doors (CG7, D8)
//
// found / ambiguous / not-found / not-built travel as DATA in
// internal/graph's result types. What these handlers add is only: the
// required-argument refusals (T5 §5 — projectRoot is REQUIRED, never
// defaulted, named when refused), and ErrStoreUnavailable when the seam
// is nil (the storeless daemon's honest "no graph here", the same shape
// every other store-backed door answers).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/graph"
)

// Graph is the daemon-injected index service. A field on the Table rather
// than a bag of function fields because internal/graph is a leaf this
// package may import (the roadmap.go / internal-search precedents): the
// methods below are marshaling around its EXACT functions, and naming the
// service type is what makes that identity assertable rather than a
// convention. Nil means the build did not link the graph or the store is
// absent: the methods stay out of the table (the V13 absent-not-refusing
// shape), because a method that can only refuse is a door onto nothing.
type Graph interface {
	Explain(ctx context.Context, root, symbol string) (*graph.ExplainResult, error)
	Path(ctx context.Context, root, from, to string) (*graph.PathResult, error)
	Query(ctx context.Context, root string, f graph.QueryFilter) (*graph.QueryResult, error)
	Build(ctx context.Context, root string) (*graph.BuildReport, error)
	RebuildFileAt(ctx context.Context, root, absPath string) (*graph.BuildReport, error)
	Delete(ctx context.Context, root string) (int, error)
	Status(ctx context.Context, root string) (*graph.Status, error)
	Roots(ctx context.Context) ([]string, error)
	// S54 T1: the audited twins the two inline OPERATIONS walk through —
	// same guards, same answers, plus the platform_change audit row in the
	// SAME transaction as the first store mutation (SP3). The unaudited
	// twins stay: they are the write hook's and the internal callers'
	// doors, and the daemon's own act is not an audited operation (SP4).
	RebuildFileAtAudited(ctx context.Context, root, absPath string, a graph.Actor) (*graph.BuildReport, error)
	DeleteAudited(ctx context.Context, root string, a graph.Actor) (int, error)
}

// --- params (wire keys are componentRoot, post-S38 — the task file predates
// the OL1 rename and says projectRoot, exactly as search.query's did; the
// deviation is recorded in tasks/T5.md's Notes, the same way T6's recorded
// it for retrieval) ---------------------------------------------------------

// GraphExplainParams: {componentRoot, symbol}.
type GraphExplainParams struct {
	ComponentRoot string `json:"componentRoot"`
	Symbol        string `json:"symbol"`
}

// GraphPathParams: {componentRoot, from, to}.
type GraphPathParams struct {
	ComponentRoot string `json:"componentRoot"`
	From          string `json:"from"`
	To            string `json:"to"`
}

// GraphQueryParams: {componentRoot, kind?, prefix?, file?, limit?}.
type GraphQueryParams struct {
	ComponentRoot string `json:"componentRoot"`
	Kind          string `json:"kind,omitempty"`
	Prefix        string `json:"prefix,omitempty"`
	File          string `json:"file,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// GraphBuildParams: {componentRoot}. Deliberate-only (V21's shape):
// nothing schedules a build; this call is the schedule.
type GraphBuildParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// GraphReparseParams: {componentRoot, path}. path is ONE file, relative to
// the componentRoot — reparse means "that file changed behind the index's
// back and I am saying so", not a second wholesale build.
type GraphReparseParams struct {
	ComponentRoot string `json:"componentRoot"`
	Path          string `json:"path"`
}

// GraphDeleteParams: {componentRoot}.
type GraphDeleteParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// GraphStatusParams: {componentRoot}.
type GraphStatusParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// RegisterGraph puts the eight methods on the table — or none of them
// (nil seam, V13). Returns whether the family is in the table, so the
// daemon's wiring can be asserted rather than trusted.
//
// S54 T1 splits the gate by KIND of method, not by family: the five READS
// keep the Graph seam alone (reads audit nothing, SP6 draws the line at
// the acts), while the three OPERATIONS — build, reparse, delete — also
// require ActorFor. A table without a stamping source has no persons, so
// it has no honest door onto "WHO started this"; those three are absent
// from it, not refusing on it (V13, the MCP/policy/roadmap authoring
// precedent carried to this plane).
func (t *Table) RegisterGraph() bool {
	if t.Graph == nil {
		return false
	}
	for _, m := range []struct {
		name   MethodName
		handle func(context.Context, *Session, json.RawMessage) (any, error)
	}{
		{MethodGraphExplain, t.handleGraphExplain},
		{MethodGraphPath, t.handleGraphPath},
		{MethodGraphQuery, t.handleGraphQuery},
		{MethodGraphStatus, t.handleGraphStatus},
		{MethodGraphRoots, t.handleGraphRoots},
	} {
		t.register(&Method{Name: m.name, RequiresInitialize: true, Handle: m.handle})
	}
	if t.ActorFor == nil {
		return true
	}
	for _, m := range []struct {
		name   MethodName
		handle func(context.Context, *Session, json.RawMessage) (any, error)
	}{
		{MethodGraphBuild, t.handleGraphBuild},
		{MethodGraphReparse, t.handleGraphReparse},
		{MethodGraphDelete, t.handleGraphDelete},
	} {
		t.register(&Method{Name: m.name, RequiresInitialize: true, Handle: m.handle})
	}
	return true
}

// graphRefused is every handler's first sentence: the seam's absence is a
// configuration fact spoken, not a panic tripped (the GitStatus nil shape;
// unreachable while registration is conditional on the same field, so
// reaching it means registration and the seam disagreed).
func graphRefused(method string) error {
	return ErrStoreUnavailable.WithWhat("graph.%s needs the code-graph service, and this daemon did not wire it (no store, or a build without the graph) — the `graph` tool is absent for the same reason", method)
}

// graphRoot is T5 §5 on the wire: a required componentRoot, refused by
// name, never defaulted. There is no ambient root on the socket (I5), and
// a graph answer whose scope was guessed is a graph answer nobody can
// trust — the same argument O8 makes for corpora, applied to trees.
func graphRoot(method, root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", ErrInvalidScope.WithWhat("graph.%s needs a componentRoot — there is no ambient root on the socket (I5), and graph rows hang off exactly one root", method)
	}
	return root, nil
}

func graphNamed(method, what, v string) error {
	if strings.TrimSpace(v) == "" {
		return ErrInvalidScope.WithWhat("graph.%s refused an empty %s — an empty name matches everything and answers like it found something", method, what)
	}
	return nil
}

func (t *Table) handleGraphExplain(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("explain")
	}
	var p GraphExplainParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("explain", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	if err := graphNamed("explain", "symbol", p.Symbol); err != nil {
		return nil, err
	}
	return t.Graph.Explain(ctx, root, p.Symbol)
}

func (t *Table) handleGraphPath(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("path")
	}
	var p GraphPathParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("path", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	if err := graphNamed("path", "from", p.From); err != nil {
		return nil, err
	}
	if err := graphNamed("path", "to", p.To); err != nil {
		return nil, err
	}
	return t.Graph.Path(ctx, root, p.From, p.To)
}

func (t *Table) handleGraphQuery(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("query")
	}
	var p GraphQueryParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("query", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	return t.Graph.Query(ctx, root, graph.QueryFilter{Kind: p.Kind, Prefix: p.Prefix, File: p.File, Limit: p.Limit})
}

// GraphBuildAccepted is graph.build's S64 answer: a handle, not a report.
// The 10-minute pass belongs to the async-worker ledger now (the daemon
// runs it, the row records it, the event stream carries its ending); the
// caller reads the finished BuildReport from the row's result column
// through asyncworkers.get. `started` says whether THIS call began the
// pass: false means one was already flying for this root (the durable
// flight index), and workerId names THAT pass — the same answer twice is
// how a double-clicked button becomes one build.
type GraphBuildAccepted struct {
	WorkerID string `json:"workerId"`
	State    string `json:"state"`
	Started  bool   `json:"started"`
	// StartedBy / StartedAt are S54 T1's additive naming of the flying
	// pass when this call did NOT start it (SP3's "already running —
	// started by WHO"): the audit row behind the worker, as `user:uid` /
	// `cli` and the queue instant. A write-hook pass has no audit row and
	// a failed audit read answers the same way — the honest no-name;
	// started:false never depends on that read.
	StartedBy string `json:"startedBy,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

func (t *Table) handleGraphBuild(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("build")
	}
	var p GraphBuildParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("build", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	// S64: the build runs as an async worker, not on this connection.
	// Deliberate, operator-or-app-initiated maintenance (the roadmap.go /
	// retrieval.reindex tier argued above): registration put this door,
	// and only THIS call walks through it. Nothing on the daemon's start
	// or a step's write calls it — those reach RebuildFileAt through the
	// write signal, and the write hook's unbuilt-root branch starts the
	// SAME worker type through the SAME manager (identity pinned in
	// internal/daemon).
	if t.AsyncWorkers == nil {
		return nil, ErrStoreUnavailable.WithWhat("graph.build needs the async-worker manager to run the pass off this connection, and this daemon did not wire it (no store) — the build would otherwise be a 10-minute hold on your socket")
	}
	// S54 T1 (SP3/SP6): the build is an OPERATION, started by the person at
	// the endpoint — the actor is the connection, and the audit row rides
	// the SAME transaction as the queued ledger row below. "Already
	// running" writes nothing: the manager's audited insert rolls audit and
	// ledger back together on a flight collision.
	actor, err := t.platformActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrInvalidScope.WithWhat("graph.build %s: cannot resolve the root: %v", root, err)
	}
	view, started, err := t.AsyncWorkers.StartViewAudited("graph.build", abs, abs, nil, graphBuildAudit(abs, actor))
	if err != nil {
		return nil, ErrStoreUnavailable.WithWhat("graph.build %s: %v", root, err)
	}
	acc := &GraphBuildAccepted{WorkerID: view.WorkerID, State: view.State, Started: started}
	if !started {
		acc.StartedBy, acc.StartedAt = t.starterWords(ctx, view.WorkerID)
	}
	return acc, nil
}

func (t *Table) handleGraphReparse(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("reparse")
	}
	var p GraphReparseParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("reparse", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	if err := graphNamed("reparse", "path", p.Path); err != nil {
		return nil, err
	}
	// S54 T1: the reparse audits its start through the service twin — the
	// audit rides DeleteGraphFile's transaction, and the not-a-claimed-file
	// skip never reaches either (SP3).
	actor, err := t.platformActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.Graph.RebuildFileAtAudited(ctx, root, p.Path, graph.Actor{Kind: actor.Kind, ID: actor.ID})
}

func (t *Table) handleGraphDelete(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("delete")
	}
	var p GraphDeleteParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("delete", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	// S54 T1: the delete audits its start through the service twin (SP3);
	// an unknown root deletes zero rows and still earns the row — the act
	// started, and the audit says who did it.
	actor, err := t.platformActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	removed, err := t.Graph.DeleteAudited(ctx, root, graph.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return nil, err
	}
	return map[string]any{"root": root, "nodesDeleted": removed}, nil
}

func (t *Table) handleGraphStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("status")
	}
	var p GraphStatusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	root, err := graphRoot("status", p.ComponentRoot)
	if err != nil {
		return nil, err
	}
	return t.Graph.Status(ctx, root)
}

func (t *Table) handleGraphRoots(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Graph == nil {
		return nil, graphRefused("roots")
	}
	return t.Graph.Roots(ctx)
}
