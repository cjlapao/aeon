package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// T4/T5 gate tests: the four states from the SERVICE over a real store
// (the store-open here is the plugin test's shape — a temp dir, never the
// real store, per §5), the resolution tagging decided at the resolver,
// the incremental rebuild's blast radius, and the CG8 type property
// asserted by reflection rather than by reading the struct once.

func openService(t *testing.T) (*Service, string) {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewService(st), t.TempDir()
}

func buildDemo(t *testing.T, s *Service) string {
	t.Helper()
	root, err := filepath.Abs(demoFixture)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.Build(context.Background(), root)
	if err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	if rep.Files < 5 || rep.Nodes < 10 || rep.Edges < 5 {
		t.Fatalf("fixture build is thin: %+v — the fixture is the test, and it shrank", rep)
	}
	return root
}

// TestSameFileEdgeIsExtractedCrossFileIsInferred — D4's tag rule, the
// resolver's core sentence made executable: validateOrder is in
// process.go's own package-file neighbourhood (same file: extracted),
// billing.ChargePayment crossed a file (inferred).
func TestSameFileEdgeIsExtractedCrossFileIsInferred(t *testing.T) {
	s, _ := openService(t)
	root := buildDemo(t, s)
	ctx := context.Background()

	// Same-file: validateOrder calls Validate — same file, same package,
	// literal relation: extracted (D4's literal case).
	res, err := s.Explain(ctx, root, "order.validateOrder")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateFound {
		t.Fatalf("validateOrder: %q", res.State)
	}
	var sawSameFile bool
	for _, e := range res.Outgoing {
		if e.Kind == EdgeCalls && nameMatches(e.To.Symbol, "Validate") && e.To.FilePath == "internal/order/validate.go" {
			sawSameFile = true
			if e.Resolution != Extracted {
				t.Errorf("same-file Validate edge tagged %q, want extracted (D4)", e.Resolution)
			}
		}
	}
	if !sawSameFile {
		t.Fatalf("no calls edge to same-file Validate: %+v", res.Outgoing)
	}

	// Cross-file: ProcessOrder's calls reach validateOrder (sibling file,
	// same package) and billing.ChargePayment (other package) — both
	// crossed a file, so both are inferred: D4's tag is about the file
	// boundary, not the package one (the sentence says "crossed files").
	res, err = s.Explain(ctx, root, "order.ProcessOrder")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateFound {
		t.Fatalf("ProcessOrder: %q", res.State)
	}
	var sawSibling, sawCrossFile bool
	for _, e := range res.Outgoing {
		if e.Kind != EdgeCalls {
			continue
		}
		switch {
		case nameMatches(e.To.Symbol, "validateOrder"):
			sawSibling = true
			if e.Resolution != Inferred {
				t.Errorf("cross-file validateOrder edge tagged %q, want inferred", e.Resolution)
			}
		case nameMatches(e.To.Symbol, "ChargePayment"):
			sawCrossFile = true
			if e.Resolution != Inferred {
				t.Errorf("billing.ChargePayment edge tagged %q, want inferred — it crossed a file (D4)", e.Resolution)
			}
		}
	}
	if !sawSibling || !sawCrossFile {
		t.Fatalf("missing expected outgoing edges (sibling=%v cross=%v): %+v", sawSibling, sawCrossFile, res.Outgoing)
	}
}

// TestAmbiguousListsBothCandidates — D8's fixture case: two `Validate`
// functions, and the answer lists BOTH with locations and no pick (CG5
// rendered). This is the DEMO §4 assertion at the service level.
func TestAmbiguousListsBothCandidates(t *testing.T) {
	s, _ := openService(t)
	root := buildDemo(t, s)
	res, err := s.Explain(context.Background(), root, "Validate")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateAmbiguous {
		t.Fatalf("state = %q, want ambiguous — the fixture names two Validate functions on purpose", res.State)
	}
	if len(res.Candidates) < 2 {
		t.Fatalf("candidates = %d, want ≥2: %+v", len(res.Candidates), res.Candidates)
	}
	var sawOrder, sawBilling bool
	for _, c := range res.Candidates {
		switch {
		case strings.HasPrefix(c.FilePath, "internal/order"):
			sawOrder = true
		case strings.HasPrefix(c.FilePath, "internal/billing"):
			sawBilling = true
		}
	}
	if !sawOrder || !sawBilling {
		t.Errorf("candidates must span order and billing, got %+v", res.Candidates)
	}
	// No traversal past the ambiguity (DEMO §4): the answer carries no
	// edges, only candidates.
	if len(res.Outgoing) != 0 || len(res.Incoming) != 0 {
		t.Error("an ambiguous answer walked anyway — edges must be empty until the name is disambiguated")
	}
}

// TestFourStatesReachableFromOneService — CG7's sentence, one state per
// sub-test, each reached the way a caller reaches it.
func TestFourStatesReachableFromOneService(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	root := buildDemo(t, s)
	other := filepath.Join(t.TempDir(), "never-built")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Run("found", func(t *testing.T) {
		res, err := s.Explain(ctx, root, "order.ProcessOrder")
		if err != nil || res.State != StateFound {
			t.Fatalf("got %v / %v", res.State, err)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		res, err := s.Explain(ctx, root, "Validate")
		if err != nil || res.State != StateAmbiguous {
			t.Fatalf("got %v / %v", res.State, err)
		}
	})
	t.Run("not-found", func(t *testing.T) {
		res, err := s.Explain(ctx, root, "NoSuchSymbolAnywhere")
		if err != nil || res.State != StateNotFound {
			t.Fatalf("got %v / %v", res.State, err)
		}
	})
	t.Run("not-built", func(t *testing.T) {
		res, err := s.Explain(ctx, other, "anything")
		if err != nil || res.State != StateNotBuilt {
			t.Fatalf("got %v / %v", res.State, err)
		}
	})
	// The states are values, not prose: not-built and not-found must be
	// DIFFERENT values (D8's collapse forbidden by type).
	if StateNotFound == StateNotBuilt {
		t.Fatal("the fourth state collapsed into not-found at the type level")
	}
}

// TestPathWalksTheChainAndHonoursTheBound — DEMO §3's chain end to end
// (HandleOrderRequest → ProcessOrder → validateOrder → Validate is three
// hops, within PathHopBound), and a chain longer than the bound answers
// not-found without assembling it (T5 §2's refusal-by-construction).
func TestPathWalksTheChainAndHonoursTheBound(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	root := buildDemo(t, s)

	res, err := s.Path(ctx, root, "api.HandleOrderRequest", "order.Validate")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateFound {
		t.Fatalf("state = %q, want found: the fixture chain is exactly this question", res.State)
	}
	if len(res.Hops) < 3 || len(res.Hops) > PathHopBound {
		t.Fatalf("hops = %d, want 3..%d: %+v", len(res.Hops), PathHopBound, res.Hops)
	}
	for _, h := range res.Hops {
		if h.Resolution != Extracted && h.Resolution != Inferred {
			t.Errorf("hop %s carries resolution %q — every hop names how it was resolved (CG5)", h.To.Symbol, h.Resolution)
		}
	}

	// The bound, measured: a linear chain of PathHopBound+2 functions.
	dir := t.TempDir()
	const n = PathHopBound + 2
	var body strings.Builder
	body.WriteString("package long\n")
	for i := 0; i < n; i++ {
		if i == 0 {
			body.WriteString("func F0() { F1() }\n")
		} else if i == n-1 {
			body.WriteString(fmt.Sprintf("func F%d() { }\n", i))
		} else {
			body.WriteString(fmt.Sprintf("func F%d() { F%d() }\n", i, i+1))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "long.go"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Build(ctx, dir); err != nil {
		t.Fatal(err)
	}
	res, err = s.Path(ctx, dir, "long.F0", fmt.Sprintf("long.F%d", n-1))
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateNotFound {
		t.Fatalf("state = %q, want not-found — the chain exceeds PathHopBound=%d, and an unbounded crawl is the cost T5 §2 refuses", res.State, PathHopBound)
	}
	// The same chain one hop shorter must be found — the bound is the
	// only reason the long one refused (a graph silently disconnected
	// would pass the test above by accident).
	body.Reset()
	body.WriteString("package long\nfunc G0() { G1() }\nfunc G1() { G2() }\nfunc G2() { G3() }\nfunc G3() { }\n")
	if err := os.WriteFile(filepath.Join(dir, "short.go"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildFile(ctx, dir, filepath.Join(dir, "short.go")); err != nil {
		t.Fatal(err)
	}
	res, err = s.Path(ctx, dir, "long.G0", "long.G3")
	if err != nil || res.State != StateFound {
		t.Fatalf("short chain: %v / %v", res.State, err)
	}
}

// TestRebuildFileRelinksIncomingEdges — T4 §3's second half: a file is
// rewritten (its symbols re-emitted with NEW ids), and the edges OTHER
// files held into it must survive the rewrite — re-resolved by symbol,
// which makes them inferred by construction (D4).
func TestRebuildFileRelinksIncomingEdges(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	write := func(p, src string) {
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(a, "package p\n\nfunc Target() int { return 1 }\n")
	write(b, "package p\n\nfunc Caller() int { return Target() }\n")
	if _, err := s.Build(ctx, dir); err != nil {
		t.Fatal(err)
	}
	res, err := s.Explain(ctx, dir, "p.Target")
	if err != nil || res.State != StateFound {
		t.Fatalf("target: %v / %v", res.State, err)
	}
	if len(res.Incoming) != 1 || !nameMatches(res.Incoming[0].From.Symbol, "Caller") {
		t.Fatalf("incoming before rewrite: %+v", res.Incoming)
	}

	// Rewrite a.go — same symbol, different body (new rows, new ids).
	write(a, "package p\n\nfunc Target() int { return 2 }\n")
	if _, err := s.RebuildFile(ctx, dir, a); err != nil {
		t.Fatal(err)
	}
	res, err = s.Explain(ctx, dir, "p.Target")
	if err != nil || res.State != StateFound {
		t.Fatalf("target after rewrite: %v / %v", res.State, err)
	}
	if len(res.Incoming) != 1 || !nameMatches(res.Incoming[0].From.Symbol, "Caller") {
		t.Fatalf("the incoming edge did not survive the rebuild: %+v", res.Incoming)
	}
	if res.Incoming[0].Resolution != Inferred {
		t.Errorf("a re-linked edge claims %q; re-resolution by symbol is disambiguation — inferred (D4)", res.Incoming[0].Resolution)
	}

	// And when the target disappears, the re-link drops instead of
	// dangling: no edge to a node that no longer exists.
	write(a, "package p\n\nfunc Unrelated() int { return 3 }\n")
	if _, err := s.RebuildFile(ctx, dir, a); err != nil {
		t.Fatal(err)
	}
	res, err = s.Explain(ctx, dir, "p.Target")
	if err != nil || res.State != StateNotFound {
		t.Fatalf("deleted target: %v / %v", res.State, err)
	}
}

// TestRebuildTouchesItsFileNotTheProject — D6's scaling claim with its
// counter: rebuild ONE file of a 500-file project and the reported
// touched-rows must be that file's size, not the project's.
func TestRebuildTouchesItsFileNotTheProject(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	dir := t.TempDir()
	const files = 500
	for i := 0; i < files; i++ {
		src := fmt.Sprintf("package big\n\nfunc Worker%d() int {\n\thelper()\n\treturn %d\n}\n", i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.go", i)), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	helper := "package big\n\nfunc helper() int { return 0 }\n"
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte(helper), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	rep, err := s.Build(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	buildElapsed := time.Since(start)
	if rep.Files < files {
		t.Fatalf("built %d of %d files", rep.Files, files)
	}
	if rep.Nodes < files {
		t.Fatalf("project nodes %d should be at least one per file", rep.Nodes)
	}

	// One file re-parsed: touched rows proportional to THAT file (its
	// nodes + its edges + the one helper re-link), never the project.
	f := filepath.Join(dir, "f007.go")
	if err := os.WriteFile(f, []byte("package big\n\nfunc Worker7() int {\n\thelper()\n\treturn 77\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	single, err := s.RebuildFile(ctx, dir, f)
	if err != nil {
		t.Fatal(err)
	}
	singleElapsed := time.Since(start)
	touched := single.Nodes + single.Edges
	if touched > 20 {
		t.Errorf("rebuild of ONE file touched %d rows in a %d-node project — that is a whole rebuild wearing an incremental costume (D6)", touched, rep.Nodes)
	}
	if singleElapsed > buildElapsed {
		t.Errorf("single-file rebuild (%v) slower than the whole build (%v) — the counter would drift but the clock does not lie", singleElapsed, buildElapsed)
	}
}

// TestCG8NoProseFieldAnywhere — D7/CG8 as a TYPE property: the walk says
// "no prose field on the result types (CG8) — asserted structurally, not
// stylistically". A field whose name admits a sentence is a field a
// future edit can fill with one, so the ban is on the name families that
// would hold it, checked over every result type by reflection.
func TestCG8NoProseFieldAnywhere(t *testing.T) {
	bannedSubstrings := []string{"content", "text", "body", "snippet", "summary", "explanation", "prose", "description", "message"}
	types := []any{ExplainResult{}, NodeRef{}, EdgeRef{}, IncomingRef{}, PathHop{}, PathResult{}, QueryResult{}}
	for _, v := range types {
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := strings.ToLower(f.Name)
			for _, b := range bannedSubstrings {
				if strings.Contains(name, b) {
					t.Errorf("%s.%s holds prose-shaped data (%q) — CG8: query answers carry references, never content", rt.Name(), f.Name, b)
				}
			}
			if f.Type.Kind() == reflect.String && (strings.Contains(strings.ToLower(f.Name), "symbol") || strings.Contains(strings.ToLower(f.Name), "kind")) {
				continue // identifiers and kinds: reference data, not sentences
			}
		}
	}
}

// TestRequiredArgsRefusedByName — T5 §5: every door names its missing
// argument; none defaults it.
func TestRequiredArgsRefusedByName(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	cases := []struct {
		call func() error
		want string
	}{
		{func() error { _, err := s.Explain(ctx, "", "X"); return err }, "project_root"},
		{func() error { _, err := s.Explain(ctx, "/r", ""); return err }, "symbol"},
		{func() error { _, err := s.Path(ctx, "", "A", "B"); return err }, "project_root"},
		{func() error { _, err := s.Path(ctx, "/r", "", "B"); return err }, "symbol"},
		{func() error { _, err := s.Query(ctx, "", QueryFilter{}); return err }, "project_root"},
		{func() error { _, err := s.Build(ctx, ""); return err }, "project_root"},
		{func() error { _, err := s.Delete(ctx, ""); return err }, "project_root"},
	}
	for i, c := range cases {
		err := c.call()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("case %d: want a refusal naming %q, got %v", i, c.want, err)
		}
	}
}

// TestDeleteClearsRootAndOthersSurvive — the CRUD door's scoping rule,
// the same one every query enforces: a delete is scoped by root, and the
// root next door does not feel it.
func TestDeleteClearsRootAndOthersSurvive(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	root := buildDemo(t, s)
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "x.go"), []byte("package x\n\nfunc Solo() { Solo() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Build(ctx, dir2); err != nil {
		t.Fatal(err)
	}
	n, err := s.Delete(ctx, root)
	if err != nil || n == 0 {
		t.Fatalf("delete removed %d nodes, err %v", n, err)
	}
	st, err := s.Status(ctx, root)
	if err != nil || st.Built {
		t.Fatalf("root still reports built: %+v / %v", st, err)
	}
	st2, err := s.Status(ctx, dir2)
	if err != nil || !st2.Built {
		t.Fatalf("the neighbouring root felt the delete: %+v / %v", st2, err)
	}
}

// TestGraphBuildsItselfViaStatusGate — the hook's gate (daemon's
// notifyGraph uses StatsForRoot to decide build-vs-rebuild): zero for an
// unbuilt root, non-zero after the first fill — the DEMO's "the graph
// builds itself" rests on exactly this distinction being a cheap COUNT.
func TestGraphBuildsItselfViaStatusGate(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() { }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := s.StatsForRoot(ctx, dir)
	if err != nil || n != 0 {
		t.Fatalf("unbuilt root reads %d nodes (%v) — the hook would skip its first fill", n, err)
	}
	if _, err := s.Build(ctx, dir); err != nil {
		t.Fatal(err)
	}
	n, err = s.StatsForRoot(ctx, dir)
	if err != nil || n == 0 {
		t.Fatalf("built root still reads %d (%v) — every future write would re-build the world", n, err)
	}
}

// TestReparseAcceptsProjectRelativePath — the T8 walk's find: the bench
// and CLI name one file RELATIVE to the root, and the first
// implementation handed that relative name straight to the absolute-path
// RebuildFile, so the door refused its own documented argument. Relative
// joins under the root; `..` still escapes nothing; absolute still works
// (that is what the daemon hook hands).
func TestReparseAcceptsProjectRelativePath(t *testing.T) {
	s, _ := openService(t)
	ctx := context.Background()
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	src := "package pkg\n\nfunc A() int { return B() }\nfunc B() int { return 0 }\n"
	if err := os.WriteFile(filepath.Join(sub, "ab.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Build(ctx, dir); err != nil {
		t.Fatal(err)
	}
	rep, err := s.RebuildFileAt(ctx, dir, filepath.Join("pkg", "ab.go"))
	if err != nil || rep.Files != 1 {
		t.Fatalf("relative reparse: %v / %+v", err, rep)
	}
	// The escape that must stay refused: a relative name climbing out.
	if _, err := s.RebuildFileAt(ctx, dir, "../outside.go"); err == nil {
		t.Error("a ..-escaping reparse was accepted — the blast radius door is open")
	}
	// And the absolute spelling still parses (the hook's shape).
	if _, err := s.RebuildFileAt(ctx, dir, filepath.Join(sub, "ab.go")); err != nil {
		t.Fatalf("absolute reparse regressed: %v", err)
	}
}
