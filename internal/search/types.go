// Package search is the shared SHAPE of a retrieval query — the payload
// the `search` tool and the `search.query` socket method both speak. It is
// data and nothing else: no logic, no store, no retrieval import.
//
// # Why it exists as a package of its own
//
// The two doors must call ONE resolver (D15: not a second search
// implementation), which needs one function type — and the architecture
// leaves exactly one place to put it. `internal/tools` and
// `internal/protocol` may not see each other (the four-packages guard,
// I1/T11); neither may import internal/retrieval (the module walk,
// internal/retrieval/index_test.go — the hunter cannot contain the
// hunted). A leaf of pure types is the intersection that remains, and its
// emptiness is the guarantee: a shared shape that cannot grow behaviour is
// one that cannot grow into a third truth.
package search

import "context"

// SearchHit is one fused result: a location and its score. There is no
// text field — D8's "locations, never contents" is a TYPE property here,
// not a rendering convention somebody must remember.
type SearchHit struct {
	Source    string  `json:"source"`
	LineStart int     `json:"lineStart"`
	LineEnd   int     `json:"lineEnd"`
	Score     float64 `json:"score"`
}

// SearchAnswer is the resolver's whole answer. KeywordOnly is D4/V9's
// honesty reaching the model: with no embedding route (or a failed
// query-time embed) the list is lexical only, and the RESULT says so —
// a note in a log reaches nobody who is deciding anything.
type SearchAnswer struct {
	Hits        []SearchHit `json:"hits"`
	KeywordOnly bool        `json:"keywordOnly,omitempty"`

	// Staleness says, in one sentence, how out of date the index answering
	// this query is — and why that is survivable (T8, V18/V4): a stale
	// index points at a line that may have moved, and results are
	// LOCATIONS, so the `read` of the current file self-corrects. A stale
	// index that claims currency is worse than one that says how stale it
	// is; empty means the answer is as current as the index knows how to
	// be — not "we did not look".
	Staleness string `json:"staleness,omitempty"`
}

// SearchResolver is the seam to the index. The production implementation
// is built at the entry point over internal/retrieval's hybrid search; the
// `search` tool and the `search.query` socket method hold THE SAME function
// value (asserted by TestSearchQuerySocketAndToolCallSameResolver), so
// "one implementation, two transports" is a wiring fact. The nil seam makes
// both doors absent (V13); it is never called while nil.
type SearchResolver func(ctx context.Context, root, corpus, query string, limit int) (SearchAnswer, error)

// --- T8: status, staleness, and the explicit reindex -----------------------
//
// These are the SHAPES the `aeon index status` verbs and the
// `retrieval.status` / `retrieval.reindex` socket methods speak — the same
// values through both doors (D15/P34), computed by one implementation.
// They stay in the leaf for the same reason SearchHit does: the protocol
// and the CLI share them, neither may import the hunted.

// CorpusStatus is ONE corpus's own figures for one project root (O4). It is
// deliberately not a global: code churns and history does not, and one
// shared number would report the history corpus as stale because a source
// file changed — wrong, and the kind of wrong that trains people to ignore
// the figure. A stale corpus that claims currency is worse than one that
// says how stale it is; every field here exists to say, never to hide:
// StaleFiles counts files whose chunks drifted or vanished since the index,
// PendingEmbed counts chunks stored without vectors (a half-finished pass is
// staleness, not health), and Reindexing/Percent are the async pass's
// "reindex running, 61%" — with Percent honest at 0 before the pass has
// reported anything, because "running" and "61%" are different claims.
type CorpusStatus struct {
	Corpus       string `json:"corpus"`
	Files        int    `json:"files"`
	StaleFiles   int    `json:"staleFiles"`
	PendingEmbed int    `json:"pendingEmbed"`
	Available    bool   `json:"available"`
	Reason       string `json:"reason,omitempty"`
	Reindexing   bool   `json:"reindexing,omitempty"`
	Percent      int    `json:"percent,omitempty"`

	// Drift is what a reindex pass would change in this corpus and what it
	// would cost — folded into the status so the cost is never an afterthought
	// (G1). Nil for a corpus the walk cannot price or does not walk; the
	// figures and the three-state Cost inside it say so, never a $0.00.
	Drift *DriftFigures `json:"drift,omitempty"`
}

// Cost is the three-state price (O3/M30): known, partial, or unknown. Absent
// (nil) means the pass could not be priced at all — never a fabricated $0.00,
// which would read as "free" for a route nobody configured a price for.
// USD is present only when State is "known" (a partial carries its floor in
// USD; an unknown carries a Reason in words).
type Cost struct {
	USD    *float64 `json:"usd,omitempty"`
	State  string   `json:"state"`
	Reason string   `json:"reason,omitempty"`
}

// NewCost is the leaf's one constructor for a Cost (O3/M30). It lives here —
// not in the entry point — because cost is COMPUTED once, in the model, and a
// third party reaching for a private formula is what O1 forbids. The entry
// point maps a retrieval-local cost into this shape; it never writes the
// composite itself, so the O1 guard (TestCostIsConstructedOnlyInClientAndCost-
// Files) stays closed across the packages it watches.
func NewCost(USD *float64, state, reason string) *Cost {
	return &Cost{USD: USD, State: state, Reason: reason}
}

// DriftFigures is what a reindex pass would change in a corpus and what it
// would cost. Changed/Added/Removed split the stale and vanished files the
// walk found (O4: one corpus's figures, never another's); Cost is three-
// state, nil = could-not-price (O3). It is the cost the status would not
// otherwise say — G1 — so a reader never decides on staleness alone.
type DriftFigures struct {
	Changed int   `json:"changed"`
	Added   int   `json:"added"`
	Removed int   `json:"removed"`
	Cost    *Cost `json:"cost,omitempty"`
}

// StatusAnswer is `aeon index status` / `retrieval.status` in full. The
// embedding identity is stated because staleness without it is half a
// sentence: an index built by another model is unusable, not merely stale,
// and only the route/model/dimension line lets a human see the difference.
type StatusAnswer struct {
	Root       string         `json:"root"`
	Corpora    []CorpusStatus `json:"corpora"`
	EmbedRoute string         `json:"embedRoute,omitempty"`
	EmbedModel string         `json:"embedModel,omitempty"`
	EmbedDim   int            `json:"embedDim,omitempty"`
}

// ReindexOutcome is what one DELIBERATE pass reports — every reindex in the
// system answers with this value, from the CLI verb, from the socket, from
// the loop's async trigger. Nothing about it says "automatic": it exists
// only because someone asked (V21).
type ReindexOutcome struct {
	Corpus        string   `json:"corpus"`
	Files         int      `json:"files"`
	ChunksWritten int      `json:"chunksWritten"`
	Deleted       int      `json:"deleted"`
	Skips         int      `json:"skips"`
	Embedded      int      `json:"embedded"`
	EmbeddedUSD   *float64 `json:"embeddedUsd,omitempty"`
	CostKnown     bool     `json:"costKnown"`
}

// StatusFunc reads the figures. It must not start work: a status read that
// reindexes is the auto-fire V21 forbids.
type StatusFunc func(ctx context.Context, root string) (StatusAnswer, error)

// ReindexFunc runs one deliberate pass over one named corpus (O8: the
// corpus is named, there is no default). progress, when supplied, reports
// (done, total) files as the walk proceeds — the figure behind
// CorpusStatus.Percent; it is called at most once per file and the pass
// never blocks on its receiver.
//
// embedRoute names an embedding route by id, "" meaning the configured
// one; force is the V17 money decision: a pass onto a corpus embedded by
// ANOTHER model is refused (and priced) unless the caller restates the
// intent. An automatic path never passes force, never names a route, and
// above all never arrives unasked (V21).
type ReindexFunc func(ctx context.Context, root, corpus, embedRoute string, force bool, progress func(done, total int)) (ReindexOutcome, error)
