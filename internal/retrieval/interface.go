// Package retrieval is S22: two corpora that never merge (V2), results that
// are locations and never text (V4), an availability model with exactly four
// reasons (V19), and the differentiator — a step arriving with its context
// resolved, saying so, and never re-resolving (V7, V14, K13, K14).
//
// It is also the only OPTIONAL slice in the plan, and that claim is carried
// by the structure of this file set rather than by a README line:
//
//   - this file and noop.go compile under EVERY build configuration — they
//     hold the seam (the Backend interface and the no-op that satisfies it)
//     and import nothing heavier than the standard library;
//   - every file with real behaviour carries `//go:build !noretrieval`, so
//     `make test-noretrieval` (and CI) run the whole product against the
//     no-op while the package itself stays compilable;
//   - no package imports this one except through Backend (I2) — asserted by
//     TestOnlyTheInterfaceIsImported, because an import accumulated by
//     convenience in task eight is exactly how "optional" becomes a lie.
//
// Absent is not empty (V13). A step whose search found nothing learned
// something about the repository; a step with no search at all should reach
// for grep, which is what V5 wanted anyway. So the no-op is not a quieter
// answer, it is a different shape: Built() is false, the tool is not
// registered, and the registry has no entry named "search" — not a tool that
// returns nothing, which a model reads as "the repository has nothing".
package retrieval

import "context"

// Reason is why the index cannot answer — the closed four (V19, D3). V1's
// fifth reason (the extension being absent) died with S4's DB18: the
// extension is compiled into the binary and registered on every connection,
// so the state cannot occur, and a constant for it would be an invitation to
// write a handler for the impossible. A reason is a domain outcome (DB19),
// never an error: "keyword-only" is not an infrastructure failure.
type Reason string

const (
	// ReasonNoProvider: no embedding route (V9). Results still arrive —
	// through FTS5 — and the Availability that carries them says out loud
	// that they are keyword-only. A silent degradation is
	// indistinguishable from an empty index, which is the sentence this
	// constant exists to make impossible.
	ReasonNoProvider Reason = "no-provider"
	// ReasonIndexEmpty: the corpus exists and holds nothing yet (D2). This
	// is the answer that must never be confused with a search that ran and
	// matched nothing — one is a fact about the index, the other a fact
	// about the repository.
	ReasonIndexEmpty Reason = "index-empty"
	// ReasonModelMismatch: the vectors in the index were built by a
	// different model than the route would query with (V6). Comparing them
	// anyway returns the worst failure a retrieval system has: plausible,
	// wrong results. Refusing is the correct answer (V17).
	ReasonModelMismatch Reason = "model-mismatch"
	// ReasonDimensionMismatch: same refusal, arrived at by geometry rather
	// than by name (V6).
	ReasonDimensionMismatch Reason = "dimension-mismatch"
)

// Availability is the three-state answer the daemon surface (T8) and the
// search path (T4/T6) carry. Available means a query may run and its answers
// are worth reading; Reasons is empty exactly then. When Available is false
// the query must not run — answering across two embedding spaces is the
// refusal these reasons exist to make cheap.
//
// KeywordOnly is stated separately from Reasons on purpose: it is not a
// reason a query is unavailable (the query DOES run — through FTS5), it is
// the honest label ON its results (V9, D4). The four reasons answer "why
// can't I?"; KeywordOnly answers "what am I getting?". Collapsing the two
// questions is how the second one stops being answerable.
type Availability struct {
	Available   bool
	KeywordOnly bool
	Reasons     []Reason
}

// Hit is one result: a location and a score, and NOTHING ELSE (V4, D8).
// There is deliberately no Body, Snippet or Excerpt field — quoting a chunk
// "burns context, hands the model a stale snapshot, and disconnects it from
// the real file", and beside an async reindex (V18) a quoted line that has
// since moved is silently wrong, where a read of the current file
// self-corrects. A future change that wants text must fail to compile first.
type Hit struct {
	Source    string
	LineStart int
	LineEnd   int
	Score     float64
}

// Route is the caller's side of the mismatch gate (V16, V17): the embedding
// model name and dimension the SEARCH side would query with, as reported by
// S18's route mechanism — embedding is a route, so this is a reading of
// S18's answer, never a second provider path (I8). nil means no route.
type Route struct {
	Model string
	Dim   int
}

// The two corpora (V2), named. A query names its corpus explicitly (O8) —
// there is no default, because a default is a query whose scope depends on a
// setting the caller did not state, and the two scopes are the whole point.
const (
	CorpusCode    = "code"
	CorpusHistory = "history"
)

// ValidCorpus is the closed set for this slice. Two, not three: the third
// corpus KIND (concluded memory) is Stage 5's, arrives as a row of a new
// name at an existing kind column (T1's reserved columns), and the validator
// that rejects it today is what makes "not yet" a sentence rather than a
// silent acceptance.
func ValidCorpus(name string) bool { return name == CorpusCode || name == CorpusHistory }

// Backend is the whole seam (I2): everything the rest of the product is
// allowed to know about retrieval. It is deliberately small and deliberately
// here rather than in a caller — this package is its only implementer, the
// no-op and the real one are compiled from the same declaration, and a
// drift between them is a compile failure in BOTH build configurations.
//
// ctx threads the store read; root is the project root (V12 — a worktree is
// a distinct root), corpus names the corpus (O8), and the returned error is
// an infrastructure failure (DB19): an availability refusal is an
// Availability, never an error dressed as one.
type Backend interface {
	// Built distinguishes ABSENT from EMPTY (V13/D2) at the seam itself.
	// Callers consult it BEFORE the other two: a no-op's zero Availability
	// is not "unavailable for reasons", it is "there is no index here to
	// have states", and rendering it as any of the four reasons would put
	// words in a slice's mouth that the slice was not built to have.
	Built() bool

	// Availability answers for one corpus (V19): the reasons are per-corpus
	// facts (O4), computed from the corpus row and its chunk count, not
	// cached — the index reports its own state when asked.
	Availability(ctx context.Context, root, corpus string, route *Route) (Availability, error)

	// KeywordSearch is the FTS5 half. route is the query's own embedding
	// configuration (or nil): the availability returned labels THESE
	// results — keyword-only is a fact about this query's route, not a
	// constant of the index — and a mismatched route refuses even the
	// keyword half out loud rather than answering partially by accident.
	// The hybrid door (T4) widens this interface — and Noop widens with it
	// in the same commit, because both files compile under every
	// configuration.
	KeywordSearch(ctx context.Context, root, corpus, query string, route *Route, limit int) ([]Hit, Availability, error)
}
