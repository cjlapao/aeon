package runtime

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S33 T4 — SectionSpec's required Category, the assembly that enforces it,
// and the tool_result tag at the transcript's append point.

func TestEverySectionSpecSiteDeclaresCategory(t *testing.T) {
	// The compile-time field cannot force a literal to SET it — Go compiles
	// an omitted field to the zero value — so the pin is an AST scan: every
	// SectionSpec composite literal in the tree (production and test, direct
	// or as an element of a []SectionSpec) names Category. A site that
	// forgets is a section whose composition line would have been guessed,
	// and D4 exists so nothing guesses.
	root := filepath.Join("..", "..") // the module root, from internal/runtime
	checked := 0
	hasCategory := func(lit *ast.CompositeLit) bool {
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Category" {
					return true
				}
			}
		}
		return false
	}
	isSectionType := func(e ast.Expr) bool {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name == "SectionSpec"
		case *ast.SelectorExpr:
			return t.Sel.Name == "SectionSpec"
		}
		return false
	}
	isSectionSlice := func(e ast.Expr) bool {
		switch t := e.(type) {
		case *ast.ArrayType:
			return isSectionType(t.Elt)
		case *ast.Ellipsis:
			return isSectionType(t.Elt)
		}
		return false
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "ui", "node_modules", ".dev", "testdata", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if isSectionType(lit.Type) {
				checked++
				if !hasCategory(lit) {
					t.Errorf("%s: a SectionSpec literal omits Category", fset.Position(lit.Pos()))
				}
				return true
			}
			if isSectionSlice(lit.Type) {
				for _, el := range lit.Elts {
					if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
						checked++
						if !hasCategory(inner) {
							t.Errorf("%s: a []SectionSpec element omits Category", fset.Position(inner.Pos()))
						}
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 40 {
		t.Fatalf("only %d SectionSpec literals found — the scan rotted (S33 landed 49+)", checked)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestAssembleRefusesAnUncategorisedSection(t *testing.T) {
	// "Required" with teeth: the empty string is refused by name, before the
	// CHECK would-be equivalent ever hears of it.
	// Built complete and then UNSET, rather than written without the field:
	// this file's own literals are scanned by
	// TestEverySectionSpecSiteDeclaresCategory, and the omission this test
	// proves refused must not itself become a hole in that scan.
	uncategorised := SectionSpec{Title: "Task", Text: "do it", Category: CategorySystemPrompt, Destination: SectionTranscript}
	uncategorised.Category = ""
	res := AssembleContext(context.Background(), []SectionSpec{uncategorised}, ContextDeps{})
	if res.OK {
		t.Fatal("a section with no Category assembled — the readout would have to guess")
	}
	if !strings.Contains(res.Reason, "category") || !strings.Contains(res.Reason, `"Task"`) {
		t.Errorf("the refusal must name the thing refused: %q", res.Reason)
	}
	// The two transcript categories are not declarable.
	for _, c := range []SectionCategory{CategoryAssistantOutput, CategoryToolResult} {
		r := AssembleContext(context.Background(), []SectionSpec{{Title: "T", Text: "x", Category: c}}, ContextDeps{})
		if r.OK {
			t.Errorf("section category %q was accepted; the transcript positions are recorded by the loop, never declared", c)
		}
	}
	// And a fifth value that does not exist is likewise refused (its own
	// non-existence is the store's CHECK's problem too, but assembly refuses
	// first, by name).
	r := AssembleContext(context.Background(), []SectionSpec{{Title: "T", Text: "x", Category: SectionCategory("tool_schema")}}, ContextDeps{})
	if r.OK {
		t.Fatal("tool_schema — the value TL6 measured as unmeasurable — assembled")
	}
}

func TestSystemPromptVsInjectedContextTagging(t *testing.T) {
	// One assembly, both categories, the record proves the distinction — and
	// Chars counts the RESOLVED body (the file's read bytes), not the ask.
	specs := []SectionSpec{
		{Title: "Task", Text: "read the plan and carry it out", Category: CategorySystemPrompt, Destination: SectionTranscript},
		{Title: "Declared file: plan.md", DeclaredFile: "plan.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}
	res := AssembleContext(context.Background(), specs, ContextDeps{
		ReadFile: func(p string) ([]byte, error) { return []byte("# Plan\nline one\nline two\n"), nil },
		Hash:     func(b []byte) string { return "hash" },
	})
	if !res.OK {
		t.Fatalf("assembly refused: %s", res.Reason)
	}
	if len(res.Manifest.Sections) != 2 {
		t.Fatalf("%d composition records, want one per section: %+v", len(res.Manifest.Sections), res.Manifest.Sections)
	}
	if res.Manifest.Sections[0].Category != CategorySystemPrompt || res.Manifest.Sections[0].Chars != len("read the plan and carry it out") {
		t.Errorf("section 0 mis-tagged: %+v", res.Manifest.Sections[0])
	}
	if res.Manifest.Sections[1].Category != CategoryInjectedContext {
		t.Errorf("the declared file is injected_context, not the handler's own text: %+v", res.Manifest.Sections[1])
	}
	if res.Manifest.Sections[1].Chars != len("# Plan\nline one\nline two\n") {
		t.Errorf("injected Chars must count the read bytes the model was shown, got %d", res.Manifest.Sections[1].Chars)
	}
}

func TestSystemCharsCountsWhatWasActuallySent(t *testing.T) {
	// Including the handoff-schema suffix OpeningTranscript embeds — the
	// suffix spends context whether anybody declared it or not.
	h := testHandler()
	compiled := CompileRequest(context.Background(), CompileInput{
		Handler: h, Route: testRoute(), WorkspaceRoot: "/workspace",
		Specs: []SectionSpec{{Title: "Task", Text: "go", Category: CategorySystemPrompt, Destination: SectionTranscript}},
	}, ContextDeps{})
	if !compiled.OK {
		t.Fatalf("compile refused: %s", compiled.Reason)
	}
	want := SystemPromptText(h)
	if want == "" {
		t.Fatal("SystemPromptText is empty — the test proves nothing")
	}
	if compiled.Request.Manifest.SystemChars != len(want) {
		t.Errorf("SystemChars %d, want %d (the system prompt AS SENT, suffix included)", compiled.Request.Manifest.SystemChars, len(want))
	}
	// And OpeningTranscript sends exactly that text — one definition, so the
	// count and the transcript cannot disagree.
	msgs := OpeningTranscript(h, "", "ctx", nil)
	if len(msgs[0].Content) != 1 || msgs[0].Content[0].Text != want {
		t.Errorf("the manifest counts a system prompt the transcript does not carry")
	}
}

func TestToolResultTaggedAtAppend(t *testing.T) {
	// A step with a real tool call produces BOTH kinds of row,
	// distinguishable: the assistant's own output and the tool's result are
	// never collapsed into one category, and the tool row carries no spend
	// (empty usage, unknown cost — the store keeps its cost columns NULL).
	rec := &telemetryRecorder{}
	client := newFakeClient(
		scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"a.md"}`)}, Usage: turnUsage(50, 10)},
		scriptedTurn{Text: okHandoff, Usage: turnUsage(60, 20)},
	)
	st := testState(testHandler("read"), []model.ToolSpec{toolSpec("read")})
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: client, Tools: newSpyTools("read"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Telemetry:   rec,
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	var assistant, tool int
	for _, row := range rec.turns {
		switch row.category {
		case CategoryAssistantOutput:
			assistant++
		case CategoryToolResult:
			tool++
			if row.usage.InputTokens != nil || row.usage.OutputTokens != nil {
				t.Errorf("the tool_result row carries usage — it is not a spend event: %+v", row.usage)
			}
			if row.cost.State != model.CostUnknown {
				t.Errorf("the tool_result row must be cost-unknown, got %+v", row.cost)
			}
			if row.wallMs < 0 {
				t.Errorf("the executed tool must carry its wall, got %d", row.wallMs)
			}
		default:
			t.Errorf("unexpected category %q from the loop — the four are closed and only two append here", row.category)
		}
	}
	if assistant != 2 {
		t.Errorf("%d assistant_output rows for 2 model calls", assistant)
	}
	if tool != 1 {
		t.Errorf("%d tool_result rows for 1 executed tool call", tool)
	}
}
