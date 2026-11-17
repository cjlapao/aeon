package graph

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cjlapao/aeon/internal/store"
)

// # The query layer — one implementation, two doors (T5, P34)
//
// The three functions here are the ONLY resolvers of explain, path and
// query. The native tool (internal/tools/graph.go) and the three socket
// methods (internal/protocol/graph.go) both call these exact functions —
// asserted by TestGraphSocketAndToolCallSameResolver, because the P34
// amendment that added the socket door exists precisely so the app cannot
// drift onto a second query implementation. Every result is four-state
// (D8, CG7) and carries raw references only (D7, CG8): there is no field
// anywhere in these result types that could hold a sentence.
//
// project_root is a required argument on every action, never defaulted
// (T5 §5 — the same no-default-corpus reasoning S22's O8 states for its
// own two corpora): a query whose scope depends on a setting the caller
// did not state is a query nobody can trust.

// PathHopBound bounds path's traversal (T5 §2): "the shortest connecting
// chain, IF ONE EXISTS within a bounded hop count — bound the search
// explicitly; an unbounded graph traversal on every `path` call is a cost
// nobody asked for." Six hops joins this repository's own call chains well
// past any question the DEMO walks; a chain longer than six answers
// not-found with the bound named, never an unbounded crawl.
const PathHopBound = 6

// QueryLimit bounds query's listing (T5 §3's browse). Zero means the
// default; it never means unlimited.
const QueryLimit = 100

// notBuilt is D8's fourth state's answer: the root has zero rows, which is
// the observation "never parsed", told apart from "parsed and absent" by
// the very fact that it is a different State value — the collapse the
// result TYPE refuses, not merely the copy that renders it.
func notBuilt() *ExplainResult { return &ExplainResult{State: StateNotBuilt} }

// Explain answers the node plus its direct edges, one hop, BOTH
// directions (T5 §1: what it calls AND what calls it). An exact name with
// multiple candidates answers ambiguous with EVERY candidate listed —
// no traversal happens past this point, and no candidate is picked
// silently (D8, DEMO §4).
// normRoot is the one spelling rule every door passes through: rows are
// keyed by the absolute root, so `./proj`, `/home/dev/proj` and a symlink
// resolved by the caller all answer from the same rows (measured in T4 —
// the write hook and a relative CLI build had keyed two spellings).
func normRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("graph: cannot resolve project_root %s: %w", root, err)
	}
	return abs, nil
}

func (s *Service) Explain(ctx context.Context, root, symbol string) (*ExplainResult, error) {
	if err := requireArgs("explain", root, symbol); err != nil {
		return nil, err
	}
	abs, err := normRoot(root)
	if err != nil {
		return nil, err
	}
	root = abs
	n, err := s.st.CountGraphNodes(root)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return notBuilt(), nil
	}
	cands, err := s.candidatesBySymbol(root, symbol)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return &ExplainResult{State: StateNotFound}, nil
	}
	if len(cands) > 1 {
		res := &ExplainResult{State: StateAmbiguous}
		res.Candidates, err = s.refs(root, cands)
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	node, err := s.ref(root, cands[0].id)
	if err != nil {
		return nil, err
	}
	res := &ExplainResult{State: StateFound, Node: node}
	out, err := s.st.GraphEdgesFrom(root, cands[0].id)
	if err != nil {
		return nil, err
	}
	for _, e := range out {
		res.Outgoing = append(res.Outgoing, EdgeRef{Kind: e.Kind, Resolution: e.Resolution, To: view(e.To)})
	}
	in, err := s.st.GraphEdgesTo(root, cands[0].id)
	if err != nil {
		return nil, err
	}
	for _, e := range in {
		res.Incoming = append(res.Incoming, IncomingRef{Kind: e.Kind, Resolution: e.Resolution, From: view(e.From)})
	}
	return res, nil
}

// Path answers the shortest connecting chain between two named symbols
// within PathHopBound (T5 §2). Either end ambiguous → ambiguous naming the
// candidates (the caller picks and asks again, exactly as DEMO §4's rule
// for explain); either end absent, or no chain within the bound →
// not-found — never an error, and never an "empty found" that pretends a
// walk happened (DEMO §3).
func (s *Service) Path(ctx context.Context, root, from, to string) (*PathResult, error) {
	if err := requireArgs("path", root, from, to); err != nil {
		return nil, err
	}
	abs, err := normRoot(root)
	if err != nil {
		return nil, err
	}
	root = abs
	n, err := s.st.CountGraphNodes(root)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return &PathResult{State: StateNotBuilt}, nil
	}
	starts, err := s.candidatesBySymbol(root, from)
	if err != nil {
		return nil, err
	}
	if len(starts) == 0 {
		return &PathResult{State: StateNotFound}, nil
	}
	if len(starts) > 1 {
		return s.ambiguousPath(ctx, root, starts)
	}
	goals, err := s.candidatesBySymbol(root, to)
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return &PathResult{State: StateNotFound}, nil
	}
	if len(goals) > 1 {
		return s.ambiguousPath(ctx, root, goals)
	}
	goalID := goals[0].id

	// Bounded BFS over the adjacency the store already indexes. Hop count
	// is edges travelled; the bound is checked on expansion, so a chain
	// longer than the bound is never even assembled — refusal by
	// construction, not by post-filter.
	type entry struct {
		id   int64
		hops int
	}
	start := starts[0].id
	visited := map[int64]bool{start: true}
	prev := map[int64]prevHop{}
	queue := []entry{{id: start, hops: 0}}
	found := false
	for len(queue) > 0 && !found {
		f := queue[0]
		queue = queue[1:]
		if f.hops >= PathHopBound {
			continue // expanding further would be the unbounded traversal nobody asked for
		}
		edges, err := s.adjacent(root, f.id)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if visited[e.toID] {
				continue
			}
			visited[e.toID] = true
			prev[e.toID] = prevHop{fromID: f.id, kind: e.kind, resolution: e.resolution}
			if e.toID == goalID {
				found = true
				break
			}
			queue = append(queue, entry{id: e.toID, hops: f.hops + 1})
		}
	}
	if !found {
		return &PathResult{State: StateNotFound}, nil
	}

	node, err := s.ref(root, start)
	if err != nil {
		return nil, err
	}
	res := &PathResult{State: StateFound, Start: node}
	var chain []PathHop
	for id := goalID; id != start; {
		p, ok := prev[id]
		if !ok {
			return &PathResult{State: StateNotFound}, nil // unreachable: found implies the chain
		}
		to, err := s.ref(root, id)
		if err != nil {
			return nil, err
		}
		chain = append([]PathHop{{Kind: p.kind, Resolution: p.resolution, To: *to}}, chain...)
		id = p.fromID
	}
	res.Hops = chain
	return res, nil
}

type prevHop struct {
	fromID     int64
	kind       string
	resolution string
}

type adjacency struct {
	toID       int64
	kind       string
	resolution string
}

// adjacent reads one node's neighbours in BOTH directions — a structural
// chain is undirected for "connects" purposes (DEMO §3's
// HandleOrderRequest → ProcessOrder → chargePayment walks a calls-out
// edge; walking INCOMING edges connects what calls-in chains together).
func (s *Service) adjacent(root string, id int64) ([]adjacency, error) {
	var out []adjacency
	from, err := s.st.GraphEdgesFrom(root, id)
	if err != nil {
		return nil, err
	}
	for _, e := range from {
		out = append(out, adjacency{toID: e.To.ID, kind: e.Kind, resolution: e.Resolution})
	}
	to, err := s.st.GraphEdgesTo(root, id)
	if err != nil {
		return nil, err
	}
	for _, e := range to {
		out = append(out, adjacency{toID: e.From.ID, kind: e.Kind, resolution: e.Resolution})
	}
	return out, nil
}

func (s *Service) ambiguousPath(ctx context.Context, root string, cands []resolved) (*PathResult, error) {
	res := &PathResult{State: StateAmbiguous}
	var err error
	res.Candidates, err = s.refs(root, cands)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// QueryFilter is query's browse (T5 §3): filter by node kind, name prefix,
// or file path. An empty filter field means unfiltered — never "match the
// empty string" — and Limit bounds the page, defaulting to QueryLimit so
// "browse the project" cannot cost the whole table.
type QueryFilter struct {
	Kind   string `json:"kind,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	File   string `json:"file,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// Query answers the structural search for a caller that does not yet know
// the exact symbol. Like every action: four-state (a root with no rows is
// not-built, a root whose rows match nothing is not-found) and references
// only — the calling agent decides what to read next, possibly by chaining
// into S22's search on the chosen nodes (D11), which this function knows
// nothing about and must not.
func (s *Service) Query(ctx context.Context, root string, f QueryFilter) (*QueryResult, error) {
	if err := requireArgs("query", root); err != nil {
		return nil, err
	}
	abs, err := normRoot(root)
	if err != nil {
		return nil, err
	}
	root = abs
	n, err := s.st.CountGraphNodes(root)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return &QueryResult{State: StateNotBuilt}, nil
	}
	rows, err := s.st.GraphNodesMatch(root, f.Kind, f.Prefix, f.File, f.Limit)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return &QueryResult{State: StateNotFound}, nil
	}
	res := &QueryResult{State: StateFound, Nodes: make([]NodeRef, 0, len(rows))}
	for _, r := range rows {
		res.Nodes = append(res.Nodes, view(r))
	}
	return res, nil
}

// --- row → wire conversions --------------------------------------------------

func (s *Service) ref(root string, id int64) (*NodeRef, error) {
	r, err := s.st.GraphNodeByID(root, id)
	if err != nil {
		return nil, err
	}
	v := view(r)
	return &v, nil
}

func (s *Service) refs(root string, cands []resolved) ([]NodeRef, error) {
	out := make([]NodeRef, 0, len(cands))
	for _, c := range cands {
		r, err := s.st.GraphNodeByID(root, c.id)
		if err != nil {
			return nil, err
		}
		out = append(out, view(r))
	}
	return out, nil
}

// view converts a stored row to its wire form — field by field, because
// the wire type has no field to leak content into (D7, D8's type
// property; search.go's SearchHit is the same discipline).
func view(r store.GraphNodeRow) NodeRef {
	return NodeRef{
		ID: r.ID, Symbol: r.Symbol, Kind: r.Kind,
		FilePath: r.FilePath, StartLine: r.StartLine, EndLine: r.EndLine,
		Language: r.Language,
	}
}

// requireArgs is T5 §5 at the door: every action names its project root
// and its symbols; an empty one is refused by name, never defaulted and
// never answered with a plausible wrong scope.
func requireArgs(action string, args ...string) error {
	if len(args) == 0 || args[0] == "" {
		return fmt.Errorf("graph %s: refused — project_root is required and never defaulted (T5 §5)", action)
	}
	for _, a := range args[1:] {
		if a == "" {
			return fmt.Errorf("graph %s: refused — every symbol argument is required, never defaulted", action)
		}
	}
	return nil
}
