package tools

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/search"
)

// withFakeResolver wires the seam for one test and restores the unwired
// state after — the registry's entry condition is this same variable, so
// presence and absence are both testable in-process (the noretrieval build
// says its own half from search_noretrieval_test.go's subprocess).
func withFakeResolver(t *testing.T, r search.SearchResolver) {
	t.Helper()
	prev := searchResolver
	searchResolver = r
	t.Cleanup(func() { searchResolver = prev })
}

func answer(hits ...search.SearchHit) search.SearchAnswer { return search.SearchAnswer{Hits: hits} }

// errIndexEmpty is the resolver's own refusal shape (D2): a named reason,
// spoken by the seam exactly as the real backend speaks it.
var errIndexEmpty = errors.New("retrieval: refused the query at /work (code) — index-empty: nothing is indexed for this corpus yet; run an index rather than concluding there is nothing to find")

func TestSearchAbsentWhenRetrievalIsNot(t *testing.T) {
	// The unwired registry is the no-retrieval registry by construction:
	// nothing in this test binary can wire the seam, and the sole
	// production caller is the entry point's !noretrieval link.
	if searchResolver != nil {
		t.Fatal("a previous test left the resolver wired — cleanup is broken")
	}
	reg := Registry()
	if _, ok := reg["search"]; ok {
		t.Fatal("search is in the unwired registry — V13: absent means it does not exist")
	}
	for _, n := range ToolNames() {
		if n == "search" {
			t.Fatal("ToolNames() lists a tool the registry does not hold")
		}
	}
	if len(reg) < 40 {
		t.Fatalf("the registry holds %d tools — too few to be the real one (floor: the 40-name registry predates search)", len(reg))
	}
}

func TestSearchAppearsWhenResolverIsWired(t *testing.T) {
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return answer(), nil
	})
	tool, ok := Registry()["search"]
	if !ok {
		t.Fatal("search is absent although the resolver is wired — D15: one condition drives registry and seam")
	}
	if tool.Spec.Name != "search" {
		t.Errorf("the tool answers to %q, not its own name", tool.Spec.Name)
	}
}

func TestSearchIsSafeOnRerun(t *testing.T) {
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return answer(), nil
	})
	// Q6 / S7's W16: a pure read, so recovery re-runs it without thought.
	if got := Registry()["search"].RerunClass(); got != RerunSafe {
		t.Fatalf("search class %q, want safe", got)
	}
	// And the committed list says the same (the fixture pair to this line).
	entries := loadClasses(t)
	e, ok := entries["search"]
	if !ok {
		t.Fatal("search is missing from the committed classes (Q6)")
	}
	if e.Class != RerunSafe {
		t.Errorf("committed class %q, want safe", e.Class)
	}
	if !e.RetrievalOnly {
		t.Error("search must be marked retrieval_only — the no-retrieval build legitimately has no such tool (V13)")
	}
}

func TestDescriptionSaysItComplementsGrep(t *testing.T) {
	// O7/V5: the rule governs only from inside the text a model reads.
	desc := Search().Spec.Description
	for _, want := range []string{
		"Complements `grep`; it does not replace it.",
		"use `grep` — it has an exact answer and this does not",
		`Requires a corpus: "code" or "history". There is no default.`,
		"Returns file:line locations, never file contents",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("the description a model receives lacks %q", want)
		}
	}
}

func TestCorpusIsRequired(t *testing.T) {
	wired := false
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		wired = true
		return answer(), nil
	})
	res := Registry()["search"].Run(context.Background(), ToolContext{WorkspaceRoot: t.TempDir()}, json.RawMessage(`{"query":"retry policy"}`))
	if !res.IsError {
		t.Fatalf("a corpus-less query answered with content — O8: it must be a correctable tool error")
	}
	if wired {
		t.Error("the resolver ran for a corpus-less query — the refusal happens before the call")
	}
	for _, want := range []string{`"code"`, `"history"`, "no default"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("the refusal does not name %s (O8: name the two options so the model can correct)", want)
		}
	}
}

var hitLine = regexp.MustCompile(`^\s+[\w./\-]+:\d+-\d+ \(score [\d.]+\)$`)

func TestSearchReturnsLocationsOnly(t *testing.T) {
	// D8/I5 at the boundary a MODEL reads: the search.SearchHit type has no text
	// field to leak, and this asserts the rendering adds none back.
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return answer(
			search.SearchHit{Source: "internal/policy/evaluate.go", LineStart: 41, LineEnd: 66, Score: 0.0328},
			search.SearchHit{Source: "internal/loop/advance.go", LineStart: 152, LineEnd: 158, Score: 0.0161},
		), nil
	})
	res := Registry()["search"].Run(context.Background(), ToolContext{WorkspaceRoot: "/work"}, json.RawMessage(`{"corpus":"code","query":"where is the retry policy"}`))
	if res.IsError {
		t.Fatalf("a populated search errored: %s", res.Content)
	}
	for _, line := range strings.Split(res.Content, "\n") {
		if hitLine.MatchString(line) {
			continue
		}
		if strings.Contains(line, "evaluate.go:41-66") || strings.Contains(line, "advance.go:152-158") {
			t.Errorf("hit line carries more than location+score: %q", line)
		}
	}
	if !strings.Contains(res.Content, "internal/policy/evaluate.go:41-66") {
		t.Error("the file:line range is not rendered (V4: locations are the whole product)")
	}
	if strings.Contains(res.Content, "func ") || strings.Contains(res.Content, "package ") {
		t.Error("chunk text appeared in a search result — D8 forbids any content at this boundary")
	}
}

func TestKeywordOnlyIsReportedToTheModel(t *testing.T) {
	// V9/D4: the degraded shape must reach the deciding audience — the
	// result, not a log line.
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		a := answer(search.SearchHit{Source: "a.go", LineStart: 1, LineEnd: 2, Score: 1})
		a.KeywordOnly = true
		return a, nil
	})
	res := Registry()["search"].Run(context.Background(), ToolContext{WorkspaceRoot: "/work"}, json.RawMessage(`{"corpus":"code","query":"retry"}`))
	if !strings.Contains(strings.ToLower(res.Content), "keyword-only") {
		t.Fatalf("a keyword-only answer came back indistinguishable from a fused one — V9's honesty never reached the model: %q", res.Content)
	}
}

func TestEmptyIndexIsNotAbsence(t *testing.T) {
	// D2/C3: with retrieval built but nothing indexed, the tool EXISTS and
	// says `index-empty` — the observation that tells someone to run an
	// index, never the confident nothing that reads as "there is none".
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		return search.SearchAnswer{}, errIndexEmpty
	})
	tool, ok := Registry()["search"]
	if !ok {
		t.Fatal("the tool vanished on an empty index — absence and emptiness collapsed (D2)")
	}
	res := tool.Run(context.Background(), ToolContext{WorkspaceRoot: "/work"}, json.RawMessage(`{"corpus":"code","query":"anything"}`))
	if !res.IsError {
		t.Fatal("an index-empty query was reported as success")
	}
	if !strings.Contains(res.Content, "index-empty") {
		t.Fatalf("the refusal did not name the reason (D2): %q", res.Content)
	}
}

func TestSearchRefusesAnEmptyQuery(t *testing.T) {
	called := false
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		called = true
		return answer(), nil
	})
	res := Registry()["search"].Run(context.Background(), ToolContext{WorkspaceRoot: "/work"}, json.RawMessage(`{"corpus":"code","query":"  "}`))
	if !res.IsError || called {
		t.Fatal("an empty query reached the resolver or was not refused — an empty string matches nothing and answers like it found everything")
	}
}

func TestSearchUsesOnlyTheStepsRoot(t *testing.T) {
	// I5: the root is the ToolContext's, handed down — never ambient, and
	// never the caller's to choose: the input schema has no root property.
	got := ""
	withFakeResolver(t, func(ctx context.Context, root, corpus, query string, limit int) (search.SearchAnswer, error) {
		got = root
		return answer(), nil
	})
	Registry()["search"].Run(context.Background(), ToolContext{WorkspaceRoot: "/the/step/root"}, json.RawMessage(`{"corpus":"code","query":"x","limit":3}`))
	if got != "/the/step/root" {
		t.Fatalf("the resolver saw root %q — the step's WorkspaceRoot is the only root (I5)", got)
	}
	if strings.Contains(string(Search().Spec.Parameters), `"componentRoot"`) || strings.Contains(string(Search().Spec.Parameters), `"root"`) {
		t.Error("the input schema offers a root property — a model must not be able to point search elsewhere")
	}
}
