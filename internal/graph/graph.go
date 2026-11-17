// Package graph is S30's structural map of a codebase: nodes (a symbol with
// a location) and edges (a typed relation between two symbols), parsed with
// tree-sitter through statically-linked cgo grammars (CG2, CG3) and stored
// as plain relational rows in S4's store (CG1). It answers the structural
// question — what calls this, what imports it, what inherits from it — and
// NEVER the content question, which stays S22's (D11, N6).
//
// # The two properties the whole design rests on
//
//   - Raw structured output only (CG8, D7): every query result carries node
//     and edge references. There is no field anywhere in this package's
//     result types that could hold a model-generated sentence — a
//     query_test asserts the absence, because "no prose" is a TYPE
//     property here, not a rendering convention (search.go's SearchHit is
//     the precedent this copies).
//
//   - Four-state results (CG7, D8): found / ambiguous / not-found /
//     not-built, as a tagged State on every result. `not-built` (no graph
//     exists for this root) is never collapsible into `not-found` (a graph
//     exists; nothing matched) — the same refusal S29 made for unvalidated
//     versus could-not-tell, for the same reason: collapsing them makes
//     "never parsed" indistinguishable from "parsed and absent".
//
// # What this package may never reach
//
// Nothing under the dispatch resolver imports this package, and nothing
// here imports the resolver (D10, CG9) — asserted by boundary_test.go in
// both directions. A graph edge is similarity-shaped enough to tempt a
// routing shortcut; the test, not the prose, is the guard.
package graph

import "context"

// The six relation kinds — Graphify's set, adopted as-is (D3, CG4). The
// database CHECK (migration 20261030061806) is the authority; these are the
// Go names the walkers and resolvers use, and a test locks the two lists to
// each other rather than trusting the comment.
const (
	EdgeCalls     = "calls"
	EdgeImports   = "imports"
	EdgeInherits  = "inherits"
	EdgeMixesIn   = "mixes_in"
	EdgeUses      = "uses"
	EdgeReference = "references"
)

// EdgeKinds is the closed vocabulary, in declaration order.
var EdgeKinds = []string{EdgeCalls, EdgeImports, EdgeInherits, EdgeMixesIn, EdgeUses, EdgeReference}

// The two resolutions (D4, CG5). extracted: the relation is literal in the
// AST and its target resolved inside the same file — an import line, a
// call to a definition in the same file, an embedding written in the
// struct. inferred: the resolution crossed a file boundary or disambiguated
// between same-named candidates — linking a call site to a definition in
// another file, or to one of several methods named Method. The tag is what
// lets a cross-file guess stay visible as a guess; D8's ambiguous state
// reads the same multi-candidate fact at query time.
const (
	Extracted = "extracted"
	Inferred  = "inferred"
)

// Node kinds are an OPEN set at the database (T1's schema deliberately
// CHECKs only edge kind and resolution — node kinds are what grammars
// produce, and a CHECK here would reject a language nobody has met yet).
// These are the kinds the walkers emit today; bulk grammars may add more.
const (
	KindFunction  = "function"
	KindMethod    = "method"
	KindType      = "type"
	KindInterface = "interface"
	KindClass     = "class"
	KindFile      = "file"
	KindPackage   = "package"
)

// State is D8's four states, one value per row of this table:
//
//	found      — the query resolved; the payload is non-empty BY CONSTRUCTION
//	ambiguous  — a name resolved to MULTIPLE exact-match nodes; the
//	             candidates are all listed, none picked silently
//	not-found  — a graph exists for this root; nothing in it matched
//	not-built  — the root has zero graph rows: never parsed, which is a
//	             different observation and the one that tells someone to
//	             run a build rather than to give up
type State string

const (
	StateFound     State = "found"
	StateAmbiguous State = "ambiguous"
	StateNotFound  State = "not-found"
	StateNotBuilt  State = "not-built"
)

// NodeRef is one node as the query layer answers it — a symbol with a
// location (search.go's SearchHit is the pattern: the shape has no field a
// summary could hide in, so the boundary cannot leak content even by
// accident, and D11's composition stays use-time-only because there is no
// column here to fill from S22).
type NodeRef struct {
	ID        int64  `json:"id"`
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
	FilePath  string `json:"filePath"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Language  string `json:"language"`
}

// EdgeRef is one relation with its resolution tag visible (D4) — a caller
// reading `inferred` knows the link crossed a file boundary or picked among
// same-named candidates, and can decide whether that is good enough for
// what it is doing. The tool does not decide for it.
type EdgeRef struct {
	Kind       string  `json:"kind"`
	Resolution string  `json:"resolution"`
	To         NodeRef `json:"to"`
}

// IncomingRef is one edge entering the explained node — the same fact with
// the FROM end shown (explain answers "what calls it" as well as "what it
// calls").
type IncomingRef struct {
	Kind       string  `json:"kind"`
	Resolution string  `json:"resolution"`
	From       NodeRef `json:"from"`
}

// ExplainResult is explain's four-state answer: the node plus its direct
// edges in both directions (T5 §1). Payload fields are empty by STATE:
// a found result has Node set; an ambiguous one has Candidates and no
// Node; the two empties carry neither.
type ExplainResult struct {
	State      State         `json:"state"`
	Node       *NodeRef      `json:"node,omitempty"`
	Outgoing   []EdgeRef     `json:"outgoing,omitempty"`
	Incoming   []IncomingRef `json:"incoming,omitempty"`
	Candidates []NodeRef     `json:"candidates,omitempty"`
}

// PathHop is one step of a path: an edge and the node it arrives at. The
// Start node of a path is carried separately (PathResult.Start) so every
// hop reads as a transition, never as a dangling half.
type PathHop struct {
	Kind       string  `json:"kind"`
	Resolution string  `json:"resolution"`
	To         NodeRef `json:"to"`
}

// PathResult is path's four-state answer (T5 §2). Hops is the shortest
// connecting chain WITHIN THE BOUND — Path bound is a named constant
// (PathHopBound) and a chain longer than it answers not-found, because an
// unbounded traversal on every call is a cost nobody asked for.
type PathResult struct {
	State      State     `json:"state"`
	Start      *NodeRef  `json:"start,omitempty"`
	Hops       []PathHop `json:"hops,omitempty"`
	Candidates []NodeRef `json:"candidates,omitempty"`
}

// QueryResult is query's four-state answer (T5 §3): a bounded, filtered
// listing — the browse for callers that do not yet know the exact symbol.
type QueryResult struct {
	State State     `json:"state"`
	Nodes []NodeRef `json:"nodes,omitempty"`
}

// The four states as values, for tests and switch exhaustiveness.
var AllStates = []State{StateFound, StateAmbiguous, StateNotFound, StateNotBuilt}

// QueryService is the read/maintenance surface both doors name — tools
// and protocol declare their seams against THIS interface (the leaf both
// may import, internal/search's role at S22), so the one *Service that
// satisfies it here is the value both doors end up holding. An
// implementation drift between the doors is a compile failure at the
// daemon's single wiring point, not a runtime surprise (P34).
type QueryService interface {
	Explain(ctx context.Context, root, symbol string) (*ExplainResult, error)
	Path(ctx context.Context, root, from, to string) (*PathResult, error)
	Query(ctx context.Context, root string, f QueryFilter) (*QueryResult, error)
	Build(ctx context.Context, root string) (*BuildReport, error)
	RebuildFile(ctx context.Context, root, absPath string) (*BuildReport, error)
	RebuildFileAt(ctx context.Context, root, absPath string) (*BuildReport, error)
	Delete(ctx context.Context, root string) (int, error)
	Status(ctx context.Context, root string) (*Status, error)
	Roots(ctx context.Context) ([]string, error)
}

var _ QueryService = (*Service)(nil)
