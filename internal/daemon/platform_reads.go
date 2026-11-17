package daemon

// platform_reads.go — S54 T2: the adapters behind the platform plane's
// three reads (`platform.status`, `platform.changes`, and the daemon's
// half of `retrieval.reindexPlan`'s wiring point).
//
// SP1 is the whole design: every figure in the panel is a figure the
// daemon can already answer with — the SAME wrapped status seam
// `retrieval.status` serves, the SAME graph service `graph.status`/
// `graph.roots` serve, the SAME telemetry store row `telemetry.workspace`
// serves, the SAME async ledger that makes a corpus answer "reindexing"
// answers flying-or-not here. The composer (internal/platformview) owns
// only the panel shape and the state word; nothing here computes a second
// staleness, a second cost, or a second anything.
//
// The nil discipline is V13 in its purest form: each adapter is built
// only over seams that exist (the typed-nil trap avoided by returning the
// interface only through these constructors), and a daemon with no store
// and no subsystems builds no composer at all — the method is absent, not
// refusing. A subsystem that REFUSES, though, is a state the panel
// carries: the source's sentence rides verbatim into that block's
// "error", and the door still answers 200 — one subsystem half-broken is
// data about the platform, not a failed read of it.

import (
	"context"
	"path/filepath"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/platformview"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/store"
)

// NewPlatformChanges adapts the T1 audit read to the protocol seam. A nil
// store yields a nil seam (typed-nil trap avoided, the
// newTelemetryReadStore convention): no store, no ledger, no door.
func NewPlatformChanges(st *store.Store) protocol.PlatformChangesFunc {
	if st == nil {
		return nil
	}
	return func(ctx context.Context, subject string, limit int) ([]protocol.PlatformChangeView, error) {
		rows, err := st.PlatformChangeList(ctx, subject, limit)
		if err != nil {
			return nil, err
		}
		out := make([]protocol.PlatformChangeView, 0, len(rows))
		for _, r := range rows {
			out = append(out, protocol.PlatformChangeView{
				ID: r.ID, Op: r.Op, Subject: r.Subject, OutcomeRef: r.OutcomeRef,
				Summary: r.Summary, ActorKind: r.ActorKind, ActorID: r.ActorID,
				OccurredAt: r.OccurredAt,
			})
		}
		return out, nil
	}
}

// PlatformDeps are the reads the panel composes over — each nilable, and
// each nil is THAT BLOCK's honest "unwired", not the panel's absence.
// Workers may be nil (a daemon that never built a manager has nothing
// flying, which is also what the ledger says); Status is the WRAPPED
// status seam (the same value the retrieval.status door serves, so the
// panel's reindexing flags and Percent and this adapter's flying-check
// share one source of truth).
type PlatformDeps struct {
	Store     *store.Store
	Status    search.StatusFunc
	Graph     protocol.Graph
	Telemetry protocol.TelemetryStore
	Workers   *asyncworkers.Manager
}

// NewPlatformStatus builds the composed panel read, or nil when the
// daemon has NOTHING to compose (no store and no subsystem seam — V13:
// the door onto an empty panel is a door onto nothing).
func NewPlatformStatus(deps PlatformDeps) protocol.PlatformStatusFunc {
	if deps.Store == nil && deps.Status == nil && deps.Graph == nil && deps.Telemetry == nil {
		return nil
	}
	return func(ctx context.Context) (*platformview.StatusView, error) {
		in := platformview.Inputs{}
		if deps.Store != nil {
			in.EvaluatedAt = deps.Store.Now()
		}
		in.Retrieval = readRetrievalBlock(ctx, deps)
		in.Graph = readGraphBlock(ctx, deps)
		in.Usage = readUsageBlock(deps)
		v := platformview.Compose(in)
		return &v, nil
	}
}

// readRetrievalBlock runs the SAME status seam once per component that
// HAS corpora (the store's own distinct-roots read — the panel never
// names a root the index has never seen). status == nil is the block's
// unwired; a root's read refusing is the block's error carrying that
// root's own sentence.
func readRetrievalBlock(ctx context.Context, deps PlatformDeps) []*platformview.RetrievalInput {
	if deps.Status == nil {
		return nil
	}
	if deps.Store == nil {
		// Wired without a store is not a state this daemon can build (the
		// status seam is always built over the index over the store); if
		// it ever were, the panel says so in the subsystem's own plain
		// words rather than inventing a component list.
		return []*platformview.RetrievalInput{{Err: "the platform panel cannot enumerate components without the store"}}
	}
	roots, err := deps.Store.RetrievalComponentRoots()
	if err != nil {
		return []*platformview.RetrievalInput{{Err: err.Error()}}
	}
	out := make([]*platformview.RetrievalInput, 0, len(roots))
	for _, root := range roots {
		out = append(out, retrievalInputFor(ctx, deps, root))
	}
	return out
}

// retrievalInputFor runs the SAME status seam once for ONE root and mirrors
// the answer into the composer's input — shared by the panel's per-root
// walk and T4's own-root tool read, so the two doors cannot drift.
func retrievalInputFor(ctx context.Context, deps PlatformDeps, root string) *platformview.RetrievalInput {
	a, err := deps.Status(ctx, root)
	if err != nil {
		return &platformview.RetrievalInput{Root: root, Err: err.Error()}
	}
	ri := &platformview.RetrievalInput{
		Root: a.Root, EmbedRoute: a.EmbedRoute, EmbedModel: a.EmbedModel, EmbedDim: a.EmbedDim,
	}
	for _, c := range a.Corpora {
		ri.Corpora = append(ri.Corpora, platformview.CorpusInput{
			Name: c.Corpus, Files: c.Files, StaleFiles: c.StaleFiles,
			PendingEmbed: c.PendingEmbed, Available: c.Available, Reindexing: c.Reindexing,
		})
	}
	ri.AnyPassFlying = flying(deps.Workers, "retrieval.reindex", root)
	return ri
}

// readGraphBlock runs the SAME service Roots+Status run; graph == nil is
// the block's unwired, a refusal is its error sentence verbatim.
func readGraphBlock(ctx context.Context, deps PlatformDeps) *platformview.GraphInput {
	if deps.Graph == nil {
		return nil
	}
	roots, err := deps.Graph.Roots(ctx)
	if err != nil {
		return &platformview.GraphInput{Err: err.Error()}
	}
	in := &platformview.GraphInput{}
	for _, root := range roots {
		st, err := deps.Graph.Status(ctx, root)
		if err != nil {
			return &platformview.GraphInput{Err: err.Error()}
		}
		in.Roots = append(in.Roots, platformview.GraphRootInput{
			Root: st.Root, Built: st.Built, Files: st.Files, Nodes: st.Nodes, Edges: st.Edges,
		})
		if flying(deps.Workers, "graph.build", root) {
			in.Building = true
		}
	}
	return in
}

// readUsageBlock reads the SAME singleton telemetry row telemetry.workspace
// answers with. exists:false is data (DB19), and the cost state is the
// store's own word read, never re-derived (S33).
func readUsageBlock(deps PlatformDeps) *platformview.UsageInput {
	if deps.Telemetry == nil {
		return nil
	}
	row, err := deps.Telemetry.WorkspaceTelemetry()
	if err != nil {
		return &platformview.UsageInput{Err: err.Error()}
	}
	return &platformview.UsageInput{
		LoopsRun: row.LoopsRun, InputTokensTotal: row.InputTokensTotal,
		OutputTokensTotal: row.OutputTokensTotal, CostUSDTotal: row.CostUSDTotal,
		CostState: row.CostState, Exists: row.Exists,
	}
}

// flying is the ledger question "is a worker of this type queued or
// running for this root" — the single-flight's own row, not a second
// tracker (T0 §2's finding). The dedupe lives in the ledger; the panel
// only asks it. A nil manager, and a ledger read that itself fails,
// answer false honestly: the status seams' own running flags (the wrapped
// status, the flying merge) remain the primary voice; this is the graph
// side's addition, and "no figure" never dresses as "running" or as
// "not running" louder than the ledger said.
func flying(m *asyncworkers.Manager, workerType, root string) bool {
	if m == nil || root == "" {
		return false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	lr, err := m.ListViews(workerType, abs, []string{"queued", "running"}, 1)
	return err == nil && lr != nil && len(lr.Workers) > 0
}

// --- T4: the agent's own-root freshness read (SP4) --------------------------

// IndexFreshnessView is `index_status`'s answer: the panel's retrieval and
// graph blocks for ONE root — the step's own — with every operator-view
// field dropped (T0's visibility ruling: root, embedRoute, embedModel and
// embedDim are not model-facing; the model-facing tools have never rendered
// them). A block's state word is the composer's, quoted; nothing here
// computes a second anything.
type IndexFreshnessView struct {
	EvaluatedAt string               `json:"evaluatedAt"`
	Retrieval   *IndexFreshnessBlock `json:"retrieval"`
	Graph       *IndexFreshnessGraph `json:"graph"`
}

// IndexFreshnessBlock is the retrieval block minus the multi-root walk: one
// root's corpora, in full (corpus names are not secret — the search tool
// prints them in its refusals; what stays out is OTHER roots and the
// embedder's coordinates).
type IndexFreshnessBlock struct {
	State        string                 `json:"state"`
	Error        string                 `json:"error,omitempty"`
	Availability string                 `json:"availability,omitempty"`
	Corpora      []IndexFreshnessCorpus `json:"corpora,omitempty"`
}

type IndexFreshnessCorpus struct {
	Name        string `json:"name"`
	Files       int    `json:"files"`
	StaleChunks int    `json:"staleChunks"`
	Reindexing  bool   `json:"reindexing,omitempty"`
}

// IndexFreshnessGraph is the same root's graph figures — built/files/nodes/
// edges — beside the composer's state word.
type IndexFreshnessGraph struct {
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	Built      bool   `json:"built"`
	NeverBuilt bool   `json:"neverBuilt,omitempty"`
	Files      int    `json:"files"`
	Nodes      int    `json:"nodes"`
	Edges      int    `json:"edges"`
}

// NewPlatformIndexFreshness builds the tool's read over the SAME deps the
// panel composes over — or nil when neither a retrieval status seam nor a
// graph service exists: freshness OF NOTHING is not a panel of unwired
// blocks, it is no door at all (V13). One implementation for the two doors:
// the reads, the flying question and the composer are the panel's own, run
// for the one root the step has.
func NewPlatformIndexFreshness(deps PlatformDeps) func(context.Context, string) (*IndexFreshnessView, error) {
	if deps.Status == nil && deps.Graph == nil {
		return nil
	}
	return func(ctx context.Context, root string) (*IndexFreshnessView, error) {
		in := platformview.Inputs{}
		if deps.Store != nil {
			in.EvaluatedAt = deps.Store.Now()
		}
		if deps.Status != nil {
			in.Retrieval = []*platformview.RetrievalInput{retrievalInputFor(ctx, deps, root)}
		}
		if deps.Graph != nil {
			in.Graph = graphInputForRoot(ctx, deps.Graph, deps.Workers, root)
		}
		v := platformview.Compose(in)
		out := &IndexFreshnessView{EvaluatedAt: v.EvaluatedAt,
			Retrieval: &IndexFreshnessBlock{State: v.Retrieval.State, Error: v.Retrieval.Error,
				Availability: v.Retrieval.Availability},
			Graph: &IndexFreshnessGraph{State: v.Graph.State, Error: v.Graph.Error}}
		if len(v.Retrieval.Components) == 1 { // the walk ran for exactly this root
			for _, c := range v.Retrieval.Components[0].Corpora {
				out.Retrieval.Corpora = append(out.Retrieval.Corpora, IndexFreshnessCorpus{
					Name: c.Name, Files: c.Files, StaleChunks: c.StaleChunks, Reindexing: c.Reindexing,
				})
			}
		}
		if len(v.Graph.Roots) == 1 {
			g := v.Graph.Roots[0]
			out.Graph.Built, out.Graph.NeverBuilt = !g.NeverBuilt, g.NeverBuilt
			out.Graph.Files, out.Graph.Nodes, out.Graph.Edges = g.Files, g.Nodes, g.Edges
		}
		return out, nil
	}
}

// graphInputForRoot mirrors ONE root's graph figures — the same
// graph.status call `graph.status` itself answers with. A root the graph
// has never LISTED has never been walked: that is the block's own word
// (neverBuilt), and zero figures is the honest shadow of it, never a
// second graph.
func graphInputForRoot(ctx context.Context, g protocol.Graph, m *asyncworkers.Manager, root string) *platformview.GraphInput {
	roots, err := g.Roots(ctx)
	if err != nil {
		return &platformview.GraphInput{Err: err.Error()}
	}
	in := &platformview.GraphInput{Building: flying(m, "graph.build", root)}
	for _, r := range roots {
		if r != root {
			continue
		}
		st, err := g.Status(ctx, root)
		if err != nil {
			return &platformview.GraphInput{Err: err.Error()}
		}
		in.Roots = []platformview.GraphRootInput{{
			Root: st.Root, Built: st.Built, Files: st.Files, Nodes: st.Nodes, Edges: st.Edges,
		}}
		return in
	}
	in.Roots = []platformview.GraphRootInput{{Root: root}}
	return in
}
