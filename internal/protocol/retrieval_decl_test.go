package protocol

// S22 T7 — the step door's retrieval declarations. The socket decides what
// is DECLARED; resolution itself belongs to the daemon's Compile closure
// (one instant, N7). What these tests pin is the door's own honesty: an
// unbuildable declaration is REFUSED here, at the door, naming the reason —
// never accepted and then silently skipped downstream (V13, O8).

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/search"
)

func okStepResult() runtime.StepResult {
	return runtime.StepResult{
		Kind:    runtime.ResultComplete,
		Outcome: runtime.OutcomeOK,
		Usage:   model.Usage{InputTokens: model.IntPtr(10), OutputTokens: model.IntPtr(5)},
		Cost:    model.Cost{State: model.CostKnown, USD: 0.002},
	}
}

func TestStepRefusesRetrievalWhenNoResolverIsWired(t *testing.T) {
	// V13 at the step door: the resolver is absent, so the declaration is
	// refused rather than skipped — a skipped section would let the step
	// proceed believing retrieval had simply found nothing, which is a
	// different fact (O6) about a door that does not exist.
	runner := &scriptedRunner{result: okStepResult()}
	tbl, _ := stepTable(t, runner, &spyStepStore{})
	if tbl.Search != nil {
		t.Fatal("fixture shipped a resolver; this test is about its absence")
	}
	_, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws",
		ContextRetrievals: []ContextRetrievalDecl{{Corpus: "code", Query: "the retry policy"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no retrieval resolver wired") {
		t.Fatalf("the unbuilt retrieval was not refused by name: %v", err)
	}
}

func TestStepRetrievalRefusalNamesBothCorpora(t *testing.T) {
	// O8: an un-named corpus is refused, and the refusal names the two it
	// could have been — correctable, not merely rejected.
	runner := &scriptedRunner{result: okStepResult()}
	tbl, _ := stepTable(t, runner, &spyStepStore{})
	tbl.Search = func(_ context.Context, _, _, _ string, _ int) (search.SearchAnswer, error) {
		return search.SearchAnswer{}, nil
	}
	_, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws",
		ContextRetrievals: []ContextRetrievalDecl{{Corpus: "wiki", Query: "q"}},
	})
	if err == nil || !strings.Contains(err.Error(), "code") || !strings.Contains(err.Error(), "history") {
		t.Fatalf("the corpus refusal does not name both options: %v", err)
	}
}

func TestStepRefusesAnEmptyRetrievalQuery(t *testing.T) {
	runner := &scriptedRunner{result: okStepResult()}
	tbl, _ := stepTable(t, runner, &spyStepStore{})
	tbl.Search = func(_ context.Context, _, _, _ string, _ int) (search.SearchAnswer, error) {
		return search.SearchAnswer{}, nil
	}
	_, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws",
		ContextRetrievals: []ContextRetrievalDecl{{Corpus: "code", Query: "   "}},
	})
	if err == nil || !strings.Contains(err.Error(), "empty query") {
		t.Fatalf("the empty query was not refused: %v", err)
	}
}

func TestStepDeclaresRetrievalAsASection(t *testing.T) {
	// The happy door: a well-formed declaration reaches the runner as a
	// retrieval SECTION — the protocol does not resolve, it declares. The
	// resolver here must go UNCALLED: resolution is Compile's one instant
	// (N7), and a protocol-side call would be a second instant.
	var asked int
	runner := &scriptedRunner{result: okStepResult()}
	tbl, _ := stepTable(t, runner, &spyStepStore{})
	tbl.Search = func(_ context.Context, _, _, _ string, _ int) (search.SearchAnswer, error) {
		asked++
		return search.SearchAnswer{}, nil
	}
	if _, err := callStep(t, tbl, StepParams{
		Prompt: "x", WorkspaceRoot: "/ws",
		ContextFiles:      []string{"a.md"},
		ContextRetrievals: []ContextRetrievalDecl{{Corpus: "code", Query: "where is the retry policy"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(runner.compiled) != 1 {
		t.Fatalf("%d compiles", len(runner.compiled))
	}
	var found *runtime.RetrievalSpec
	for _, s := range runner.compiled[0].Handler.Sections {
		if s.Retrieval != nil {
			found = s.Retrieval
			if s.DeclaredFile != "" || s.Text != "" {
				t.Errorf("the retrieval section also declares a file or text: %+v", s)
			}
		}
	}
	if found == nil || found.Corpus != "code" || found.Query != "where is the retry policy" {
		t.Fatalf("the retrieval declaration did not reach the runner as a section: %+v",
			runner.compiled[0].Handler.Sections)
	}
	if asked != 0 {
		t.Errorf("the protocol resolved %d time(s); resolution belongs to Compile alone (N7)", asked)
	}
	// Declared files still ride along unchanged — retrieval extends the
	// declaration, it does not replace it.
	var declared int
	for _, s := range runner.compiled[0].Handler.Sections {
		if s.DeclaredFile == "a.md" {
			declared++
		}
	}
	if declared != 1 {
		t.Error("the declared file section was lost beside the retrieval")
	}
}
