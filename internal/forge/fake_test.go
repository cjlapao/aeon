package forge

// T1's tests, written first: the fake is the surface every later task's
// assertions land on, so the tests of the fake ARE the tests of the
// discipline — D1 asserts on the CALL LOG (a test that checks a returned
// value passes against code that acted twice and returned the same thing),
// L26 demands BaseRef on every PR the fake can hand back, and O7 demands
// this package never touch a network, a token, or a URL.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newSeededFake() *Fake {
	f := NewFake()
	f.SeedIssue(Issue{Title: "Add a --version flag to cmd/hello", Labels: []string{"dev-loop"}, State: "open"})
	f.SeedPR(PR{Number: 15, Head: "task/4a17/t1-version-flag", BaseRef: "feature/4a17-version", Merged: false})
	return f
}

// TestFakeReportsBaseRef is L26/D5: the phantom-merge guard reads BaseRef,
// so EVERY PR the fake returns carries one — and a scenario can make it
// something other than the expected feature branch (the mis-based PR is
// T6's subject and it must be expressible HERE).
func TestFakeReportsBaseRef(t *testing.T) {
	f := newSeededFake()
	ctx := context.Background()

	pr, err := f.FindOpenPR(ctx, "task/4a17/t1-version-flag")
	if err != nil {
		t.Fatalf("FindOpenPR: %v", err)
	}
	if pr == nil || pr.BaseRef != "feature/4a17-version" {
		t.Fatalf("found PR must carry BaseRef, got %+v", pr)
	}

	read, err := f.ReadPR(ctx, 15)
	if err != nil {
		t.Fatalf("ReadPR: %v", err)
	}
	if read.BaseRef != "feature/4a17-version" {
		t.Fatalf("ReadPR lost BaseRef: %+v", read)
	}

	opened, err := f.OpenPR(ctx, PRRequest{Title: "T", Head: "task/x", Base: "feature/y"})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if opened.BaseRef != "feature/y" {
		t.Fatalf("OpenPR must carry the requested base as BaseRef, got %+v", opened)
	}

	// The mis-based scenario: base is the repository default, not the
	// feature branch — exactly what the guard must refuse before merging.
	mis := NewFake()
	mis.SeedPR(PR{Number: 16, Head: "task/wrong", BaseRef: "main"})
	got, err := mis.FindOpenPR(ctx, "task/wrong")
	if err != nil || got == nil || got.BaseRef != "main" {
		t.Fatalf("a scenario must be able to set a wrong base, got %+v (%v)", got, err)
	}
}

// TestFakeRecordsEveryCall pins the surface D1 asserts on: every call, in
// order, WITH arguments. A scenario where an action acts twice and returns
// the same thing must be distinguishable, by the log, from one that acted
// once.
func TestFakeRecordsEveryCall(t *testing.T) {
	f := newSeededFake()
	ctx := context.Background()
	if _, err := f.FindOpenIssue(ctx, "Add a --version flag to cmd/hello", "dev-loop"); err != nil {
		t.Fatalf("FindOpenIssue: %v", err)
	}
	if _, err := f.OpenIssue(ctx, IssueRequest{Title: "second", Labels: []string{"dev-loop"}}); err != nil {
		t.Fatalf("OpenIssue: %v", err)
	}
	if _, err := f.FindOpenPR(ctx, "task/4a17/t1-version-flag"); err != nil {
		t.Fatalf("FindOpenPR: %v", err)
	}
	if err := f.CloseIssue(ctx, 1); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}

	log := f.Calls()
	var names []string
	for _, c := range log {
		names = append(names, c.Method)
	}
	want := []string{"FindOpenIssue", "OpenIssue", "FindOpenPR", "CloseIssue"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("call log must be every call in order, got %v", names)
	}
	// ...with arguments: the first call carries both of its args.
	first := log[0].Args
	if len(first) != 2 || first[0] != "Add a --version flag to cmd/hello" || first[1] != "dev-loop" {
		t.Fatalf("call log must carry arguments, got %#v", first)
	}

	// Acting twice is VISIBLE: the same call repeated is two log entries —
	// a returned-value check could never tell these apart.
	if _, err := f.FindOpenIssue(ctx, "Add a --version flag to cmd/hello", "dev-loop"); err != nil {
		t.Fatalf("second FindOpenIssue: %v", err)
	}
	if last := f.Calls()[len(f.Calls())-1]; last.Method != "FindOpenIssue" {
		t.Fatalf("the repeat must be recorded, got %v", last.Method)
	}
}

func TestFakeScenarios(t *testing.T) {
	ctx := context.Background()

	t.Run("existing open issue found by exact title and label", func(t *testing.T) {
		f := newSeededFake()
		got, err := f.FindOpenIssue(ctx, "Add a --version flag to cmd/hello", "dev-loop")
		if err != nil || got == nil {
			t.Fatalf("existing issue must be found: %+v %v", got, err)
		}
		// Exactness: a near title is NOT a match — remembering a number is
		// not finding, and a fuzzy title finds the wrong work.
		miss, err := f.FindOpenIssue(ctx, "Add a --version flag", "dev-loop")
		if err != nil || miss != nil {
			t.Fatalf("near title must be absent, got %+v (%v)", miss, err)
		}
		// A closed issue is not open work.
		f.SeedIssue(Issue{Title: "done one", Labels: []string{"dev-loop"}, State: "closed"})
		if got, _ := f.FindOpenIssue(ctx, "done one", "dev-loop"); got != nil {
			t.Fatal("a closed issue must not be found as open")
		}
	})

	t.Run("existing open PR found by head ref", func(t *testing.T) {
		f := newSeededFake()
		got, err := f.FindOpenPR(ctx, "task/4a17/t1-version-flag")
		if err != nil || got == nil || got.Number != 15 {
			t.Fatalf("existing PR must be found by head: %+v %v", got, err)
		}
		if other, _ := f.FindOpenPR(ctx, "task/someone-else"); other != nil {
			t.Fatal("a different head is not the same PR")
		}
		// A merged PR is not an open PR.
		f2 := NewFake()
		f2.SeedPR(PR{Number: 3, Head: "task/m", BaseRef: "feature/m", Merged: true})
		if got, _ := f2.FindOpenPR(ctx, "task/m"); got != nil {
			t.Fatal("a merged PR must not be found as open")
		}
	})

	t.Run("merged PR reports the recorded SHA, never merges twice", func(t *testing.T) {
		f := NewFake()
		f.SeedPR(PR{Number: 4, Head: "task/a", BaseRef: "feature/a"})
		first, err := f.MergePR(ctx, 4, MergeSquash)
		if err != nil || !first.Merged || first.SHA == "" {
			t.Fatalf("merge must produce a SHA: %+v %v", first, err)
		}
		again, err := f.MergePR(ctx, 4, MergeSquash)
		if err != nil {
			t.Fatalf("re-merge must not fail: %v", err)
		}
		if again.SHA != first.SHA {
			t.Fatalf("a merged PR returns the RECORDED sha (the action reads merged==true and reuses it), got %q vs %q", again.SHA, first.SHA)
		}
		// The squash SHA is not the task's original commit — the fake hands
		// back a merge SHA distinct from any head commit the action may hold.
		if strings.HasPrefix(again.SHA, "task") {
			t.Fatalf("merge SHA must be its own: %q", again.SHA)
		}
	})

	t.Run("many reviews are listed in order", func(t *testing.T) {
		f := NewFake()
		for i := 0; i < 3; i++ {
			if _, err := f.PostReview(ctx, 9, "body", VerdictComment); err != nil {
				t.Fatalf("PostReview: %v", err)
			}
		}
		revs, err := f.ListReviews(ctx, 9)
		if err != nil || len(revs) != 3 {
			t.Fatalf("three reviews expected, got %d (%v)", len(revs), err)
		}
		if revs[0].ID == revs[1].ID || revs[1].ID == revs[2].ID {
			t.Fatal("review ids must be distinct — reconciliation is BY ID (T7); identical bodies must not blur them")
		}
		// Identical bodies, distinct reviews: exactly the no-business-key
		// property that makes postReview the receipt-first exception.
		if revs[0].Body != revs[2].Body {
			t.Fatal("the scenario's premise is identical bodies")
		}
	})

	t.Run("unreachable forge is a transport failure on every method", func(t *testing.T) {
		f := NewFake()
		f.SetUnreachable()
		if _, err := f.FindOpenIssue(ctx, "x", "dev-loop"); !errors.As(err, new(*ForgeError)) || err.(*ForgeError).Kind != KindTransport {
			t.Fatalf("unreachable must be transport, got %v", err)
		}
		if _, err := f.ReadPR(ctx, 1); !errors.As(err, new(*ForgeError)) || err.(*ForgeError).Kind != KindTransport {
			t.Fatalf("unreachable must be transport on reads too, got %v", err)
		}
	})

	t.Run("close that silently fails leaves the issue open — the §5 test surface", func(t *testing.T) {
		f := NewFake()
		f.SeedIssue(Issue{Number: 14, Title: "t", Labels: []string{"dev-loop"}, State: "open"})
		f.CloseSilentlyFails = true
		if err := f.CloseIssue(ctx, 14); err != nil {
			t.Fatalf("the silent failure is silent at the call site (that is the whole point): %v", err)
		}
		got, err := f.ReadIssue(ctx, 14)
		if err != nil {
			t.Fatalf("ReadIssue: %v", err)
		}
		if got.State != "open" {
			t.Fatalf("the read-back must disagree: %s", got.State)
		}
	})
}

// TestForgeErrorKindsAreDistinct is O5: "GitHub is down" and "GitHub said
// no" need different responses, and conflating them sends someone to check
// their permissions when their network is out.
func TestForgeErrorKindsAreDistinct(t *testing.T) {
	kinds := []string{KindTransport, KindAuth, KindNotFound, KindConflict, KindRefused}
	if len(kinds) != 5 {
		t.Fatalf("five kinds, one per loop response (retry, stop-and-tell, absent, reconcile, report)")
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		e := NewMsg(k, "d-" + k)
		if seen[e.Error()] {
			t.Fatalf("kind %s is indistinguishable in text", k)
		}
		seen[e.Error()] = true
		var target *ForgeError
		if !errors.As(error(e), &target) || target.Kind != k {
			t.Fatalf("errors.As must recover the kind %s", k)
		}
	}
	transport := NewMsg(KindTransport, "dial failed")
	refused := NewMsg(KindRefused, "merge refused by repository")
	if transport.Error() == refused.Error() || transport.Kind == refused.Kind {
		t.Fatal("a transport failure is NOT a refusal")
	}
	if errors.Is(transport, refused) {
		t.Fatal("the two must not compare equal")
	}
}

// TestNoNetworkInAnyTest is O7/I7: the automated suite never needs a
// network or a token, and THIS package must be provable of it by grep.
//
// T11 EVOLVED THIS TEST, by the rule the original comment wrote
// ("T11 adds the only one"): the two files T11 was sanctioned to add —
// github.go (the implementation) and github_test.go (its offline,
// fixture-served suite) — are exempt from the walk, and nothing else is.
// fake.go, forge.go and this file keep the ban for good: the fake never
// grows an http import, no test outside the exemption ever reads a token
// env, no string outside the exemption names a URL. What replaced the
// blanket ban is stronger, not weaker: github_test.go carries the
// repository-wide tripwire (TestNoOtherTestGrewANetworkDependency) that
// scans EVERY Go file under aeon/ — so "the network stays out" is now a
// whole-tree property, and github.go itself is pinned by
// TestTokenIsNeverLogged, TestFixturesAreRecordedNotLive and the dial-
// only-BaseURL checks. The old file-name check inverted with the arrival
// it predicted: the files MUST exist now.
func TestNoNetworkInAnyTest(t *testing.T) {
	// The banned tokens are built in pieces so this file does not contain
	// any of them as a literal — a grep that indicts itself tests nothing.
	banned := []string{
		"net/" + "http",
		"http" + ".Client",
		"http" + ".Get",
		"http" + ".Post",
		"https:" + "//",
		"http:" + "//",
		"GITHUB_" + "TOKEN",
		"GH_" + "TOKEN",
		"os." + "Getenv",
	}
	dir := "."
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// The sanctioned exemption — and it is exactly three files,
		// named. (filepath.Join has already cleaned "./" off the walked
		// path.) The golden generator is the third: its one env read is
		// the AEON_WRITE_GOLDEN write-switch — a test harness, not a
		// token (the canary TestForgeNeverLogsTheToken pins that
		// nothing credential-shaped reaches its goldens). S57 T2 named
		// it when T2's full-package run exposed the T1 latent red.
		if d.Name() == "github.go" || d.Name() == "github_test.go" || d.Name() == "github_golden_test.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s contains %q — this package must never reach a network, read a token, or name a URL", path, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	names := testFileNames()
	for _, want := range []string{"github.go", "github_test.go"} {
		if !strings.Contains(names, want) {
			t.Fatalf("%s is missing — T11 arrived and its files are part of the boundary", want)
		}
	}
	// And the exemption is EXACTLY those three: no other file may name
	// the network (walk above enforces; this names the rule at the pin).
}

func testFileNames() string {
	var b []string
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		b = append(b, e.Name())
	}
	return strings.Join(b, ",")
}

// TestInterfaceIsTypedNotGeneric is the anti-discovery assertion: a generic
// name-and-payload call is how tool-name discovery crept into v1's runtime
// MCP path. The Forge interface is ten named methods; no map[string]any
// crosses the boundary, no method is a generic call.
func TestInterfaceIsTypedNotGeneric(t *testing.T) {
	typ := reflect.TypeOf((*Forge)(nil)).Elem()
	if typ.NumMethod() != 10 {
		t.Fatalf("ten named methods (4 issue, 4 PR, 2 review), got %d", typ.NumMethod())
	}
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if strings.Contains(strings.ToLower(m.Name), "call") || strings.Contains(strings.ToLower(m.Name), "invoke") || strings.Contains(strings.ToLower(m.Name), "request") {
			t.Fatalf("method %s reads like a generic call", m.Name)
		}
		for p := 0; p < m.Type.NumIn(); p++ {
			in := m.Type.In(p)
			if in.Kind() == reflect.Map && in.Key().Kind() == reflect.String && in.Elem().Kind() == reflect.Interface {
				t.Fatalf("method %s takes a name-and-payload map — that is how discovery crept in", m.Name)
			}
		}
	}
	// The source itself: no interface{} parameter anywhere in the package.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for name, pkg := range pkgs {
		if name != "forge" && name != "forge.test" {
			continue
		}
		for _, file := range pkg.Files {
			for _, d := range file.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				for _, field := range fn.Type.Params.List {
					if _, ok := field.Type.(*ast.InterfaceType); ok {
						t.Fatalf("%s in %s takes interface{} parameters", fn.Name.Name, file.Name)
					}
				}
			}
		}
	}
}
