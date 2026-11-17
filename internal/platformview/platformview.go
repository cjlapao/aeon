// Package platformview composes the platform's derived-store status from the
// figures the subsystems already answer with — it computes nothing of its own
// and imports no store, no retrieval, no graph (SP1: a composed read, one
// implementation). The daemon adapter calls the SAME read functions
// `retrieval.status`, `graph.status`/`graph.roots` and `telemetry.workspace`
// answer with and hands the results in; what lives here is only the panel
// shape and the state word each block carries.
//
// A nil input block is a seam that is not wired: the block answers
// "unwired" — the V13 absence spoken in words, never an empty list that
// reads as "nothing left to index". A failed read carries the source's own
// sentence verbatim under "error": a composed answer that paraphrases a
// subsystem's refusal invents a second voice (the one-formatter law).
package platformview

// State is the closed panel vocabulary every block answers in (SP1). The
// words are a fixed set precisely because each one is a different promise:
// available (queries answer), stale (queries answer, but a pass would
// change rows), building (a pass is flying), unwired (the seam is nil —
// the capability is ABSENT, V13), error (the read itself refused, and the
// subsystem's sentence is quoted).
type State = string

const (
	StateAvailable State = "available"
	StateStale     State = "stale"
	StateBuilding  State = "building"
	StateUnwired   State = "unwired"
	StateError     State = "error"
)

// StatusView is `platform.status`'s answer. The order is the panel's: what
// the index would search, what the graph would trace, what the loops spent.
// evaluatedAt is the instant the adapter took the reads together — stamped
// by the caller (the daemon, at read time), never by a protocol handler and
// never by this package, which owns no clock.
type StatusView struct {
	EvaluatedAt string          `json:"evaluatedAt"`
	Retrieval   *RetrievalBlock `json:"retrieval"`
	Graph       *GraphBlock     `json:"graph"`
	Usage       *UsageBlock     `json:"usage"`
}

// Inputs are the already-read results, one per block; a nil pointer is a
// seam that is not wired (the adapter decides that from its own fields, the
// same V13 check that keeps the subsystem's own doors absent).
type Inputs struct {
	EvaluatedAt string
	// Retrieval is one input per component root the daemon knows; nil
	// (the whole slice absent) is the unwired answer, an empty slice is a
	// wired subsystem with nothing indexed yet.
	Retrieval []*RetrievalInput
	Graph     *GraphInput
	Usage     *UsageInput
}

// --- retrieval ---------------------------------------------------------------

// RetrievalInput is ONE component root's `retrieval.status` answer as
// already read — mirrored field-for-field so this package never imports the
// search leaf. The panel is multi-root because the seams are per-root: the
// adapter runs the SAME function once per component the daemon knows.
type RetrievalInput struct {
	Root       string
	Corpora    []CorpusInput
	EmbedRoute string
	EmbedModel string
	EmbedDim   int
	// Err is the read's own sentence, verbatim; Err carries a failed read.
	Err string
	// AnyPassFlying reports a reindex worker queued or running for this
	// root (the ledger's single-flight, not a second tracker): "building"
	// is the ledger's fact, not this package's guess.
	AnyPassFlying bool
}

// CorpusInput is one corpus's figures, exactly as retrieval.status answered
// them; staleChunks is the corpus's own pending-embed count, never a
// recomputation. (The design's per-corpus lastPassAt and kind have no
// source field on the seam and are NOT invented — T2's Notes record the
// deviation.)
type CorpusInput struct {
	Name         string
	Files        int
	StaleFiles   int
	PendingEmbed int
	Available    bool
	Reindexing   bool
}

// RetrievalBlock is the panel's retrieval block. Availability is the block
// speaking in words about its route ("no embedding route to price the
// pass") — the embedding identity line lives per component, because
// staleness without it is half a sentence (the search leaf's own reason).
type RetrievalBlock struct {
	State        string               `json:"state"`
	Error        string               `json:"error,omitempty"`
	Availability string               `json:"availability,omitempty"`
	Components   []RetrievalComponent `json:"components,omitempty"`
}

// RetrievalComponent is one component root's index view.
type RetrievalComponent struct {
	Component  string       `json:"component"`
	EmbedRoute string       `json:"embedRoute,omitempty"`
	EmbedModel string       `json:"embedModel,omitempty"`
	EmbedDim   int          `json:"embedDim,omitempty"`
	Corpora    []CorpusView `json:"corpora,omitempty"`
}

// CorpusView rides the wire with the corpus's own words only.
type CorpusView struct {
	Name        string `json:"name"`
	Files       int    `json:"files"`
	StaleChunks int    `json:"staleChunks"`
	Reindexing  bool   `json:"reindexing,omitempty"`
}

// --- graph -------------------------------------------------------------------

// GraphInput is the graph block as already read: the roots (graph.roots),
// each one's status (graph.status), and whether a graph.build worker is
// flying. Err carries a failed read verbatim.
type GraphInput struct {
	Roots    []GraphRootInput
	Err      string
	Building bool
}

// GraphRootInput is one root's graph.status answer. Built is the status's
// own fact: a root the panel can list but never built answers stale — the
// honest word for "queries would find nothing", with neverBuilt spoken in
// the block rather than a fabricated lastBuildAt.
type GraphRootInput struct {
	Root  string
	Built bool
	Files int
	Nodes int
	Edges int
}

// GraphBlock is the panel's graph block. The graph plane computes no
// staleness today (T0's finding): this panel therefore NEVER answers stale
// on any hunch — stale means only "never built" (listed, zero rows), and
// that is pinned by a test naming the finding.
type GraphBlock struct {
	State string          `json:"state"`
	Error string          `json:"error,omitempty"`
	Roots []GraphRootView `json:"roots,omitempty"`
}

// GraphRootView rides the wire.
type GraphRootView struct {
	Root       string `json:"root"`
	NeverBuilt bool   `json:"neverBuilt,omitempty"`
	Files      int    `json:"files"`
	Nodes      int    `json:"nodes"`
	Edges      int    `json:"edges"`
}

// --- usage --------------------------------------------------------------------

// UsageInput is the telemetry singleton row as already read. Exists:false
// is data — the workspace has recorded nothing — never an error and never
// a zero-filled claim that loops ran.
type UsageInput struct {
	LoopsRun          int
	InputTokensTotal  int64
	OutputTokensTotal int64
	CostUSDTotal      float64
	CostState         string
	Exists            bool
	Err               string
}

// UsageBlock is the panel's usage block. The cost figures stay in the
// store's own three-state word (computed at write, S33 — the panel reads
// the state, it never re-derives it).
type UsageBlock struct {
	State     string        `json:"state"`
	Error     string        `json:"error,omitempty"`
	Workspace *UsageFigures `json:"workspace,omitempty"`
}

// UsageFigures is the store's singleton row in the panel's words; "recorded"
// is the exists flag — a workspace that has run nothing is a state, not a
// row of zeroes pretending to be history.
type UsageFigures struct {
	LoopsRun          int     `json:"loopsRun"`
	InputTokensTotal  int64   `json:"inputTokensTotal"`
	OutputTokensTotal int64   `json:"outputTokensTotal"`
	CostUSDTotal      float64 `json:"costUsdTotal"`
	CostState         string  `json:"costState"`
	Recorded          bool    `json:"recorded"`
}

// Compose is the whole package: place the read results into the panel and
// say each block's state from the block's own figures. No I/O, no clock.
func Compose(in Inputs) StatusView {
	return StatusView{
		EvaluatedAt: in.EvaluatedAt,
		Retrieval:   composeRetrieval(in.Retrieval),
		Graph:       composeGraph(in.Graph),
		Usage:       composeUsage(in.Usage),
	}
}

func composeRetrieval(in []*RetrievalInput) *RetrievalBlock {
	if in == nil {
		return &RetrievalBlock{State: StateUnwired}
	}
	b := &RetrievalBlock{State: StateAvailable}
	building, stale := false, false
	for _, r := range in {
		if r.Err != "" {
			// One failed read fails the block — the panel does not hide a
			// subsystem half-refusing. The first refusal's sentence rides,
			// verbatim, and the listing a partially-walked root set would
			// have accumulated is DROPPED: a list that cannot claim to be
			// complete is not a list, it is the shape of one.
			b.State, b.Error = StateError, r.Err
			b.Components, b.Availability = nil, ""
			return b
		}
		if r.AnyPassFlying {
			building = true
		}
		comp := RetrievalComponent{Component: r.Root, EmbedRoute: r.EmbedRoute,
			EmbedModel: r.EmbedModel, EmbedDim: r.EmbedDim}
		for _, c := range r.Corpora {
			if c.Reindexing {
				building = true
			}
			if c.StaleFiles > 0 || c.PendingEmbed > 0 || !c.Available {
				stale = true
			}
			comp.Corpora = append(comp.Corpora, CorpusView{
				Name: c.Name, Files: c.Files, StaleChunks: c.PendingEmbed, Reindexing: c.Reindexing,
			})
		}
		if r.EmbedRoute == "" && b.Availability == "" {
			b.Availability = "no embedding route to price the pass"
		}
		b.Components = append(b.Components, comp)
	}
	switch {
	case building:
		b.State = StateBuilding
	case stale:
		b.State = StateStale
	}
	return b
}

func composeGraph(in *GraphInput) *GraphBlock {
	if in == nil {
		return &GraphBlock{State: StateUnwired}
	}
	b := &GraphBlock{State: StateAvailable, Error: in.Err}
	if in.Err != "" {
		b.State = StateError
		return b
	}
	stale := false
	for _, r := range in.Roots {
		if !r.Built {
			stale = true // listed but never built: the ONLY stale the graph block may claim
		}
		b.Roots = append(b.Roots, GraphRootView{
			Root: r.Root, NeverBuilt: !r.Built, Files: r.Files, Nodes: r.Nodes, Edges: r.Edges,
		})
	}
	switch {
	case in.Building:
		b.State = StateBuilding
	case stale:
		b.State = StateStale
	}
	return b
}

func composeUsage(in *UsageInput) *UsageBlock {
	if in == nil {
		return &UsageBlock{State: StateUnwired}
	}
	b := &UsageBlock{State: StateAvailable, Error: in.Err}
	if in.Err != "" {
		b.State = StateError
		return b
	}
	b.Workspace = &UsageFigures{
		LoopsRun: in.LoopsRun, InputTokensTotal: in.InputTokensTotal,
		OutputTokensTotal: in.OutputTokensTotal, CostUSDTotal: in.CostUSDTotal,
		CostState: in.CostState, Recorded: in.Exists,
	}
	return b
}
