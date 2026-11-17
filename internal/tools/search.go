package tools

// search.go — the tenth tool (W1), and D1's other half (S22).
//
// # Absent, not empty (V13, D1, D2)
//
// `search` is in the registry ONLY while a resolver is wired. The wiring is
// a single entry-point call (cmd/aeon's retrieval link, behind the
// !noretrieval tag), so the no-retrieval build has no caller that can wire
// it and the tool does not exist there — not registered-and-failing, not
// registered-and-empty. The distinction is the whole design: an EMPTY result
// is a claim about the repository (a model that searches "where is the retry
// policy", gets nothing, and moves on has concluded there isn't one), while
// an ABSENT tool sends the model to `grep`, which is what V5 wanted. The
// two states must never collapse into one another, which is also why
// retrieval keeps answering `index-empty` as a NAMED reason (D2) rather
// than an empty list: with the index not yet built the tool EXISTS and
// says so — a different observation, and the one that tells someone to run
// an index rather than to give up.
//
// # The description carries V5's rule (O7)
//
// "so a model does not reach for similarity when it wants a definition"
// governs nobody until it is in the words the model reads. The Description
// below is that rule verbatim in substance — a design-document restatement
// does not reach the decision-maker; the tool spec does.
//
// # Locations only (V4, D8, I5)
//
// The result renders `file:line` ranges with scores. The resolver's own
// shape has no text field to leak (SearchHit below), and the rendering
// adds none back — asserted at this boundary because this is where a model
// would see it.
//
// # No ambient state (I5)
//
// The root is ToolContext.WorkspaceRoot, like every other read tool. The
// resolver receives it as an argument; the tools package reaches no store
// and never imports internal/retrieval (the hunter cannot contain the
// hunted — internal/retrieval/index_test.go's walk is the guard).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/search"
)

// SearchHit is one fused result: a location and its score. There is no
// text field — D8 is a TYPE property here, not a rendering convention.
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
}

// The payload and resolver types live in internal/search — the one leaf
// both doors may see (tools and protocol may not import each other;
// neither may import retrieval). The nil seam makes the tool absent; it is
// never called while nil.
// searchResolver is the wired seam. Package--private so only the exported
// setter moves it, and the setter has exactly one production caller (the
// entry point's tagged retrieval link).
var searchResolver search.SearchResolver

// SetSearchResolver wires the retrieval half at the entry point. It is
// exported because the entry point is the only place allowed to import
// internal/retrieval; every other caller is a test, and the tool-level
// refusal to exist while nil is the guarantee that makes widening safe.
func SetSearchResolver(r search.SearchResolver) { searchResolver = r }

// SearchRegistered reports whether the resolver seam is wired — the same
// condition the registry consults, exposed for the surfaces that must say
// the truth about it (the CLI's "not built" line, tests).
func SearchRegistered() bool { return searchResolver != nil }

// The two corpora, named here for the refusal a MODEL can correct (O8).
// The authority is retrieval.ValidCorpus — the resolver refuses again, by
// name, and that refusal stays the last word; this mirror exists so the
// model-facing error names both options without the tools package importing
// the package it must not contain.
const (
	searchCorpusCode    = "code"
	searchCorpusHistory = "history"
)

// Search is the tool. Class safe (Q6, W16): a pure read, so recovery
// re-runs it without thought.
func Search() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name: "search",
			Description: "Semantic and keyword search over this project's code or its loop history. " +
				"Returns file:line locations, never file contents — read the file to see current truth.\n\n" +
				"Complements `grep`; it does not replace it. For an exact identifier, a literal " +
				"string, or a definition, use `grep` — it has an exact answer and this does not.\n\n" +
				"Requires a corpus: \"code\" or \"history\". There is no default.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "corpus": {"type": "string", "enum": ["code", "history"], "description": "which corpus to search; REQUIRED — there is no default (a default corpus is a query whose scope depends on a setting the caller did not state)"},
    "query": {"type": "string", "description": "the question in words; an empty query is refused rather than answered with everything"},
    "limit": {"type": "integer", "description": "maximum results (default 10)"}
  },
  "required": ["corpus", "query"],
  "additionalProperties": false
}`),
		},
		Run: runSearch,
	}
}

type searchInput struct {
	Corpus string `json:"corpus"`
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
}

func runSearch(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	// Absent is not this function's problem: with no resolver the tool is
	// not in the registry, so Run is unreachable. The guard is the tripwire
	// that says the registry and the seam disagreed — stated, never silent.
	if searchResolver == nil {
		return Errorf("search is not wired in this build — retrieval is not linked; use `grep`")
	}
	var in searchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return Errorf("search input refused: %v", err)
	}
	if in.Corpus != searchCorpusCode && in.Corpus != searchCorpusHistory {
		// O8: a correctable tool error naming both options, never a guess.
		return Errorf("refused corpus %q — search NAMES its corpus: %q or %q (there is no default)", in.Corpus, searchCorpusCode, searchCorpusHistory)
	}
	if strings.TrimSpace(in.Query) == "" {
		return Errorf("refused an empty query — an empty string matches nothing and answers like it found everything")
	}
	// The refusal (a D2 index-empty, a refusal from the resolver itself)
	// comes back as the resolver's own sentence: reason and thing refused,
	// verbatim (the house refusal rule survives the seam).
	ans, err := searchResolver(ctx, tc.WorkspaceRoot, in.Corpus, in.Query, in.Limit)
	if err != nil {
		return Errorf("%v", err)
	}
	if len(ans.Hits) == 0 {
		// Zero hits with the index available is a real answer (the
		// index-empty case refused above); say it plainly and point at grep
		// for the shapes similarity is not owed.
		return Result{Content: fmt.Sprintf("no matches in the %s corpus for %q — for an exact identifier or definition use `grep`", in.Corpus, in.Query)}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d results in the %s corpus for %q:\n", len(ans.Hits), in.Corpus, in.Query)
	for _, h := range ans.Hits {
		// Locations and score only — no text (D8).
		fmt.Fprintf(&b, "  %s:%d-%d (score %.4f)\n", h.Source, h.LineStart, h.LineEnd, h.Score)
	}
	if ans.KeywordOnly {
		// V9/D4: the degraded shape speaks IN THE RESULT, where the
		// deciding model reads it.
		b.WriteString("keyword-only: no embedding route (or the query-time embed failed) — this list is lexical matching only; an exact identifier or definition may still be missed, use `grep` for those")
	}
	res := Result{Content: b.String(), Summary: fmt.Sprintf("search %s: %d hits for %q", in.Corpus, len(ans.Hits), truncate(in.Query, 40))}
	return res
}

// truncate keeps a quoted query inside a one-line summary.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// embedRouteOrder is the deterministic route pick (the first route, by id,
// that speaks embeddings). Lives here rather than in the entry point so
// the selection rule is one function the retrieval link and any later
// surface share. Exported for the tagged wiring.
func EmbedRoute(r interface {
	Route(id string) (model.Route, bool)
	RouteIDs() []string
}) *model.Route {
	ids := append([]string(nil), r.RouteIDs()...)
	sort.Strings(ids)
	for _, id := range ids {
		rt, ok := r.Route(id)
		if !ok {
			continue
		}
		for _, c := range rt.Capabilities {
			if c == "embedding" {
				return &rt
			}
		}
	}
	return nil
}
