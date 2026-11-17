package tools

// graph.go — the `graph` tool (S30 T5), the step's structural door.
//
// The seam discipline is search.go's, copied deliberately: a
// package-private var, one exported setter with exactly one production
// caller (the daemon's construction — asserted by
// TestGraphToolAndSocketShareOneService in internal/daemon), and the
// tool ABSENT from Registry() while unwired (V13's "absent is not
// refusing": a model offered a tool that can only fail is a model taught
// to waste a turn).
//
// What the tool IS, said to the model in its own description (O7's
// lesson, learned by the search tool next door): STRUCTURE, never
// contents; complements grep, does not replace it. An agent that reads
// this description and then greps for a call site has been sent the wrong
// way — the description is the router, so it carries the routing.
//
// project_root is never defaulted (T5 §5): the step's workspace root is
// DECLARED by the step's manifest, and the tool reads that declaration —
// which is the opposite of guessing an ambient root the caller never
// stated. Every result is four-state data (CG7/D8): found, ambiguous,
// not-found, not-built — and the not-built answer tells the agent the
// index was never built for this project, which is a fact it can act on
// (say so to the person; the bench builds it), rather than an empty list
// it will mistake for "no such function".

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/model"
)

// graphService is the wired seam — the SAME *graph.Service value the
// socket methods hold (internal/protocol/graph.go's Table.Graph), set
// once at daemon construction. The interface (not the concrete pointer)
// keeps tests able to fake it; production passes the one real Service,
// which satisfies both this interface and protocol.Graph from the single
// construction point.
var (
	// graphMu guards the seam. S27 T11 found the race its parallel
	// -race law exposes: every daemon New() wires the seam, and two
	// daemons constructed concurrently (which only tests do — and
	// S27's e2e suite does) raced the write against the write and
	// each read. The one production caller never races; the door
	// still must not.
	graphMu      sync.Mutex
	graphService graph.QueryService
)

// wiredGraphService reads the seam under the guard.
func wiredGraphService() graph.QueryService {
	graphMu.Lock()
	defer graphMu.Unlock()
	return graphService
}

// SetGraphService wires the graph door at the entry point. One production
// caller (TestGraphServiceHasExactlyOneProductionCaller).
func SetGraphService(s graph.QueryService) {
	graphMu.Lock()
	defer graphMu.Unlock()
	graphService = s
}

// GraphRegistered reports the seam, for registries and honest CLI lines.
func GraphRegistered() bool { return wiredGraphService() != nil }

// Graph is the tool. Class safe: three pure reads, RerunSafe for recovery.
func Graph() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name: "graph",
			Description: "Structural questions about this project's code: who calls what, what a " +
				"symbol relates to, where a name is declared. Returns SYMBOLS and file:line " +
				"LOCATIONS, never file contents — read the file for what it says.\n\n" +
				"Complements `grep`; it does not replace it. For an exact string or a literal " +
				"pattern use `grep`. For call relations and cross-file structure, this.\n\n" +
				"Actions: \"explain\" (one symbol's direct edges, both directions), \"path\" " +
				"(the shortest chain between two symbols, bounded), \"query\" (browse nodes by " +
				"kind/name-prefix/file). Answers are four-state: found, ambiguous (candidates " +
				"listed, none picked), not-found, not-built (this project has no index yet — " +
				"say so rather than concluding the symbol is absent).",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["explain", "path", "query"], "description": "which structural question to ask"},
    "symbol": {"type": "string", "description": "for explain: the symbol to explain — bare or qualified (order.Validate)"},
    "from": {"type": "string", "description": "for path: the chain's start symbol"},
    "to": {"type": "string", "description": "for path: the chain's end symbol"},
    "kind": {"type": "string", "description": "for query: filter by node kind (function, method, type, interface, class, file, package)"},
    "prefix": {"type": "string", "description": "for query: symbol name prefix"},
    "file": {"type": "string", "description": "for query: only nodes from this project-relative file"},
    "limit": {"type": "integer", "description": "for query: maximum rows (default 100)"}
  },
  "required": ["action"],
  "additionalProperties": false
}`),
		},
		Run: runGraph,
	}
}

type graphInput struct {
	Action string `json:"action"`
	Symbol string `json:"symbol"`
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Prefix string `json:"prefix"`
	File   string `json:"file"`
	Limit  int    `json:"limit"`
}

func runGraph(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	// Unreachable while the registry and the seam agree; the guard is the
	// tripwire that says they do not (search.go's shape, verbatim reason).
	svc := wiredGraphService()
	if svc == nil {
		return Errorf("graph is not wired in this build — no store, or a build without the graph; use `grep` and `read`")
	}
	var in graphInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Errorf("graph input refused: %v", err)
	}
	// The root is the step's DECLARED workspace (the manifest's component
	// root), not a default guessed here (T5 §5). An empty WorkspaceRoot is
	// a step that was never given a project — refused by name.
	if strings.TrimSpace(tc.WorkspaceRoot) == "" {
		return Errorf("graph refused: this step has no workspace root — structural questions need a project, and none was declared")
	}
	switch in.Action {
	case "explain":
		if strings.TrimSpace(in.Symbol) == "" {
			return Errorf("graph explain refused an empty symbol — name the symbol, bare or qualified")
		}
		res, err := svc.Explain(ctx, tc.WorkspaceRoot, in.Symbol)
		return graphResult(res, err)
	case "path":
		if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
			return Errorf("graph path needs BOTH from and to — a chain has two ends")
		}
		res, err := svc.Path(ctx, tc.WorkspaceRoot, in.From, in.To)
		return graphResult(res, err)
	case "query":
		res, err := svc.Query(ctx, tc.WorkspaceRoot, graph.QueryFilter{
			Kind: in.Kind, Prefix: in.Prefix, File: in.File, Limit: in.Limit,
		})
		return graphResult(res, err)
	default:
		return Errorf("graph refused action %q — action NAMES the question: \"explain\", \"path\" or \"query\" (there is no default)", in.Action)
	}
}

// graphResult passes the four-state payload through VERBATIM (DB19: the
// states are data; `error` here means the disk spoke, and its sentence is
// the house refusal, reason and thing refused).
func graphResult(res any, err error) Result {
	if err != nil {
		return Errorf("%v", err)
	}
	// The four-state payload travels as its JSON, verbatim — the model
	// reads the SAME state strings the socket answers with (no rendering
	// layer between the truth and either door; CG8 survives because there
	// is no field in the payload a sentence could hide in).
	b, jerr := json.Marshal(res)
	if jerr != nil {
		return Errorf("graph: the answer could not be encoded: %v", jerr)
	}
	return Result{Content: string(b), Summary: "graph"}
}

// GraphService returns the wired seam — the value, so the daemon wiring
// test can assert IDENTITY with the socket's Table.Graph (two doors,
// literally one service; P34's "the same function the tool calls" made
// checkable).
func GraphService() graph.QueryService { return wiredGraphService() }
