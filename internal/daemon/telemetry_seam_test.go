package daemon

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
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// S33 T2 — the production adapter, the production wiring, and the absence.

func TestNewTelemetrySinkNilStoreIsNil(t *testing.T) {
	// I2, at the door: a storeless daemon must get a NIL interface, not a
	// non-nil interface holding a nil store — the second would turn every
	// future call into a nil-pointer panic instead of a skipped capture.
	if s := newTelemetrySink(nil); s != nil {
		t.Fatalf("nil store must answer a nil sink, got %#v", s)
	}
	s := newTelemetrySink(&store.Store{})
	if s == nil {
		t.Fatal("a real store must answer a real adapter")
	}
	var _ runtime.TelemetrySink = s // compile-time: the adapter satisfies the seam
}

func TestAdapterWritesThroughToTheStore(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	comp, err := st.CreateComponent(store.NewComponent{Name: "Shop", Slug: "shop", Root: "/shop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "walk", Root: "/shop", ComponentRoot: "/shop"}); err != nil {
		t.Fatal(err)
	}
	step, err := st.CreateStep(store.Scope{LoopSlug: "walk", ComponentRoot: "/shop"}, store.NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	sink := newTelemetrySink(st)
	ctx := context.Background()
	in := 500
	if err := sink.RecordTurn(ctx, step.ID, 1, model.Usage{InputTokens: &in}, model.Cost{State: model.CostKnown, USD: 0.01}, 400, "route-a", runtime.CategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := sink.RecordCompaction(ctx, step.ID, runtime.CompactionRecord{
		Start: 2, End: 5, EstimateBefore: 20000, EstimateAfter: 9000, Summary: "a summary of length",
		Usage: model.Usage{OutputTokens: &in}, Cost: model.Cost{State: model.CostPartial, USDAtLeast: 0.02},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.RecordFileActivity(ctx, step.ID, "src/app.go", "write", intPtr(9), intPtr(1)); err != nil {
		t.Fatal(err)
	}
	if err := sink.RecordCommandActivity(ctx, step.ID, "make test", intPtr(0), 150); err != nil {
		t.Fatal(err)
	}
	// Every write landed its row AND its bump — the adapter is thin, not
	// lossy: these totals are the SUMS of the four calls above.
	c, err := st.ComponentTelemetry(comp.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.InputTokensTotal != 500 || c.OutputTokensTotal != 500 || c.CostUSDTotal != 0.03 ||
		c.FilesWrittenTotal != 1 || c.LinesAddedTotal != 9 || c.CommandsRunTotal != 1 || c.CommandWallMsTotal != 150 ||
		c.WallMsTotal != 400 || c.CostState != "partial" {
		t.Fatalf("adapter writes did not reach the tiers verbatim: %+v", c)
	}
	rows, _ := st.StepCompactions(step.ID)
	if len(rows) != 1 || rows[0].SummaryLen != len("a summary of length") {
		t.Fatalf("the compaction unroll lost the record: %+v", rows)
	}
}

func intPtr(v int) *int { return &v }

// TestAllProductionSitesWireTheRealAdapter is T2's named guard, AST-based
// rather than line-grepped: every runtime.RunOpts{…} literal in the two
// production packages (tests excluded) must set Telemetry explicitly —
// plan.md's Q2 answer, machine-pinned. An OMITTED field and a deliberate
// `Telemetry: nil` are the same zero value to the compiler and different
// decisions to the reader; only the second is allowed to exist.
func TestAllProductionSitesWireTheRealAdapter(t *testing.T) {
	sites := 0
	for _, dir := range []string{"../protocol", "."} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "RunOpts" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "runtime" {
					return true
				}
				sites++
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Telemetry" {
						return true
					}
				}
				pos := fset.Position(lit.Pos())
				t.Errorf("%s: a production RunOpts literal omits Telemetry — wire the adapter or state `Telemetry: nil` and say why (plan Q2)", pos)
				return true
			})
		}
	}
	if sites < 3 {
		t.Fatalf("found %d production RunOpts literals, want at least 3 (protocol step run+resume, daemon recovery) — did the scan itself rot?", sites)
	}
}

// TestCurrentAndCumulativeAreDistinctNumbers is D2's pair, proven on a step
// that HAS compacted at least once: the transcript is small (the compaction
// replaced a span with a summary — a row in st.Compactions is the evidence),
// while the ledger's sum of every turn's spend is larger and never shrinks.
// One number would be a lie in one direction or the other.
func TestCurrentAndCumulativeAreDistinctNumbers(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	comp, err := st.CreateComponent(store.NewComponent{Name: "Shop", Slug: "shop", Root: "/shop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "walk", Root: "/shop", ComponentRoot: "/shop"}); err != nil {
		t.Fatal(err)
	}
	step, err := st.CreateStep(store.Scope{LoopSlug: "walk", ComponentRoot: "/shop"}, store.NewStep{Agent: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	sink := newTelemetrySink(st)
	ctx := context.Background()

	// The step spent two big turns BEFORE any compaction — captured through
	// the real seam, so the cumulative side is the store's answer (T1's SUM
	// over step_turn rows), never a runtime counter.
	if err := sink.RecordTurn(ctx, step.ID, 1, model.Usage{InputTokens: intPtr(20000), OutputTokens: intPtr(800)},
		model.Cost{State: model.CostKnown, USD: 0.2}, 900, "route-a", runtime.CategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	if err := sink.RecordTurn(ctx, step.ID, 2, model.Usage{InputTokens: intPtr(21000), OutputTokens: intPtr(500)},
		model.Cost{State: model.CostKnown, USD: 0.2}, 700, "route-a", runtime.CategoryAssistantOutput); err != nil {
		t.Fatal(err)
	}
	cumulative, err := st.CumulativeTokensTotal(step.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(20000 + 800 + 21000 + 500); cumulative != want {
		t.Fatalf("cumulative = %d, want the rows' sum %d", cumulative, want)
	}

	// The state now reads as post-compaction: a small live transcript and a
	// compaction row proving the span was replaced by a summary (the
	// accessor's contract is about the pair being distinct, not about
	// re-running Compact itself — compact_test.go owns that).
	state := &runtime.LoopState{
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("Earlier step context, compacted: a summary")}}},
		Compactions: []runtime.CompactionRecord{{
			Start: 1, End: 9, EstimateBefore: 41000, EstimateAfter: 1200, Summary: "a summary",
		}},
	}
	current := state.CurrentContextTokens()
	if current <= 0 {
		t.Fatalf("current = %d, must be present and positive", current)
	}
	if current == int(cumulative) {
		t.Fatalf("D2 collapsed: current %d == cumulative %d — after a compaction these CANNOT be the same number", current, cumulative)
	}
	if current >= 20000 {
		t.Errorf("current %d does not read as a compacted transcript", current)
	}
	_ = comp
}
