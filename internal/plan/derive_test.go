package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// S17 T1 — the plan loader: depends_on validation and a stable topological
// sort. D5 (four classes refused at load, each naming its task), O2/I4
// (stability — a pinned blueprint plus an unstable order is still
// non-deterministic, which is the thing the pin exists to prevent), L31 (the
// order is settled before the walk), Q4 (the sort sits in front of S8's
// iterate, never inside it), and DB19 (a bad plan is a domain outcome, not
// an error).

func tasks(defs ...[2]any) []Task {
	out := make([]Task, 0, len(defs))
	for _, d := range defs {
		t := Task{ID: d[0].(string)}
		if deps, ok := d[1].([]string); ok {
			t.DependsOn = deps
		}
		out = append(out, t)
	}
	return out
}

// TestFourBadDependencyClassesRefused — all four refused at LOAD, each
// naming the offending task (D5, C3). v1's reason is kept in the sentences:
// all four "produce a task that can never complete, and catching them here
// names the plan instead of surfacing a mystery at merge time."
func TestFourBadDependencyClassesRefused(t *testing.T) {
	cases := []struct {
		name     string
		in       []Task
		wantKind string
		wantTask string
	}{
		{"names a task that does not exist", tasks([2]any{"T1", []string{"T9"}}), "depends-on-missing", "T1"},
		{"a self-dependency", tasks([2]any{"T1", []string{"T1"}}), "self-dependency", "T1"},
		{"the same taskid twice", tasks([2]any{"T1", nil}, [2]any{"T1", nil}), "duplicate-id", "T1"},
		{"a cycle", tasks([2]any{"T1", []string{"T2"}}, [2]any{"T2", []string{"T1"}}), "cycle", "T1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, refusal := Load(c.in)
			if refusal == nil {
				t.Fatalf("the plan was accepted; it must be refused at load (%s)", c.wantKind)
			}
			if refusal.Kind != c.wantKind {
				t.Errorf("refusal kind = %q, want %q (sentence: %s)", refusal.Kind, c.wantKind, refusal.Sentence())
			}
			if refusal.Task != c.wantTask {
				t.Errorf("refusal names task %q, want %q", refusal.Task, c.wantTask)
			}
			if s := refusal.Sentence(); !strings.Contains(s, c.wantTask) {
				t.Errorf("the sentence %q does not name the offending task %q", s, c.wantTask)
			}
		})
	}
}

// TestCycleIsNamed — the refusal names the cycle's MEMBERS, not just its
// existence. "There is a cycle" leaves someone reading a twenty-task plan by
// hand.
func TestCycleIsNamed(t *testing.T) {
	_, refusal := Load(tasks(
		[2]any{"T1", []string{"T3"}},
		[2]any{"T2", []string{"T1"}},
		[2]any{"T3", []string{"T2"}},
		[2]any{"T4", []string{"T2"}},
	))
	if refusal == nil || refusal.Kind != "cycle" {
		t.Fatalf("want a cycle refusal, got %v", refusal)
	}
	if len(refusal.Cycle) == 0 {
		t.Fatal("the cycle refusal names no members")
	}
	// Every member of the cycle appears in the sentence.
	s := refusal.Sentence()
	for _, m := range []string{"T1", "T2", "T3"} {
		if !strings.Contains(s, m) {
			t.Errorf("cycle sentence %q omits member %s", s, m)
		}
	}
	// T4 depends on the cycle but is not IN it; naming it as a member would
	// send the reader to the wrong edge.
	if strings.Contains(s, "T4") {
		t.Errorf("cycle sentence %q names T4, which is downstream of the cycle, not in it", s)
	}
}

// TestSortIsStable — two tasks with no dependency between them keep their
// plan order. Run REPEATEDLY: an unstable sort passes a single run (O2, E3).
// Go's map iteration is randomised per run, so a map-based sort passes once
// and fails on the fourth CI run — the worst way to discover it.
func TestSortIsStable(t *testing.T) {
	in := tasks(
		[2]any{"bravo", nil},
		[2]any{"alpha", nil},
		[2]any{"charlie", nil},
		[2]any{"delta", []string{"bravo"}},
	)
	want := []string{"bravo", "alpha", "charlie", "delta"}
	for i := 0; i < 200; i++ {
		sorted, refusal := Load(in)
		if refusal != nil {
			t.Fatalf("run %d: refused a legal plan: %s", i, refusal.Sentence())
		}
		if got := Names(sorted); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: unstable order: %v, want %v (independent tasks keep plan order)", i, got, want)
		}
	}
}

// TestSortRespectsDependencies — a dependent task follows the one it needs,
// in EVERY arrangement of the input.
func TestSortRespectsDependencies(t *testing.T) {
	base := tasks(
		[2]any{"A", nil},
		[2]any{"B", []string{"A"}},
		[2]any{"C", []string{"A", "B"}},
	)
	var all [][]Task
	var perm func(prefix []Task, rest []Task)
	perm = func(prefix []Task, rest []Task) {
		if len(rest) == 0 {
			cp := append([]Task(nil), prefix...)
			all = append(all, cp)
			return
		}
		for i := range rest {
			p := append(append([]Task(nil), prefix...), rest[i])
			r := append(append([]Task(nil), rest[:i]...), rest[i+1:]...)
			perm(p, r)
		}
	}
	perm(nil, base)
	if len(all) != 6 {
		t.Fatalf("expected 6 permutations, got %d", len(all))
	}
	for _, in := range all {
		sorted, refusal := Load(in)
		if refusal != nil {
			t.Fatalf("permutation refused: %s", refusal.Sentence())
		}
		seen := map[string]int{}
		for i, task := range sorted {
			seen[task.ID] = i
		}
		if seen["A"] > seen["B"] {
			t.Errorf("input %v sorted B before A", in)
		}
		if seen["B"] > seen["C"] || seen["A"] > seen["C"] {
			t.Errorf("input %v sorted C before its dependencies", in)
		}
	}
}

// TestSortIsDeterministicAcrossRuns — the same plan sorts identically a
// hundred times; no map iteration in the path.
func TestSortIsDeterministicAcrossRuns(t *testing.T) {
	in := tasks(
		[2]any{"one", nil}, [2]any{"two", []string{"one"}}, [2]any{"three", nil},
		[2]any{"four", []string{"three", "two"}}, [2]any{"five", []string{"four"}},
	)
	first, refusal := Load(in)
	if refusal != nil {
		t.Fatalf("refused a legal plan: %s", refusal.Sentence())
	}
	want := Names(first)
	for i := 0; i < 100; i++ {
		next, _ := Load(in)
		if got := Names(next); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d differs: %v vs %v — map iteration is in the path", i, got, want)
		}
	}
}

// TestIterateIsUnchanged — S8's advance.go is not modified; the sort is
// upstream of it (Q4). The scan asserts no plan import and no sorting code
// reached the walk: the separation is what keeps the position monotonic
// without S8's code changing.
func TestIterateIsUnchanged(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	advancePath := filepath.Join(filepath.Dir(thisFile), "..", "loop", "advance.go")
	src, err := os.ReadFile(advancePath)
	if err != nil {
		t.Fatalf("cannot read advance.go: %v", err)
	}
	text := string(src)
	for _, banned := range []string{"internal/plan", "plan.Load", "plan.Sort", "topolog"} {
		if strings.Contains(text, banned) {
			t.Errorf("advance.go contains %q — the sort must live IN FRONT of iterate, not inside it (Q4)", banned)
		}
	}
}

// TestValidationIsADomainRefusal — not an error: a bad plan is a domain
// outcome the loop reports, not an infrastructure failure (S4's DB19). The
// signatures carry the promise: Load returns no error at all.
func TestValidationIsADomainRefusal(t *testing.T) {
	loadType := reflect.TypeOf(Load)
	if loadType.NumOut() != 2 {
		t.Fatalf("Load returns %d values, want 2 ([]Task, *Refusal)", loadType.NumOut())
	}
	if loadType.Out(1).String() != "*plan.Refusal" {
		t.Errorf("Load's second result is %s, want *plan.Refusal", loadType.Out(1))
	}
	if errType := reflect.TypeOf((*error)(nil)).Elem(); loadType.Out(0).Implements(errType) || loadType.Out(1).Implements(errType) {
		t.Error("a bad plan must not travel as an error (DB19)")
	}
}

// TestEmptyPlanIsValid — zero tasks sorts to zero and does not fail. S8's
// iterate over nothing is already a legitimate no-op.
func TestEmptyPlanIsValid(t *testing.T) {
	sorted, refusal := Load(nil)
	if refusal != nil {
		t.Fatalf("an empty plan was refused: %s", refusal.Sentence())
	}
	if len(sorted) != 0 {
		t.Errorf("an empty plan sorted to %d tasks", len(sorted))
	}
	if Names(sorted) == nil {
		t.Error("Names of an empty plan should be a non-nil empty slice for the engine's walk")
	}
}

// TestParsesTaskDirFrontMatter — the loader boundary is the plan's files:
// task documents with id/title/depends_on front matter, the same shape this
// repository's own tasks/ directory uses. A file whose front matter is
// broken is refused by NAME, at load.
func TestParsesTaskDirFrontMatter(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("T1.md", "---\nid: T1\ntitle: plumbing\ndepends_on: []\n---\n\n# T1 — plumbing\n")
	write("T2.md", "---\nid: T2\ntitle: counters\ndepends_on: [T1]\n---\n\n# T2 — counters\n")

	loaded, refusal := LoadDir(dir)
	if refusal != nil {
		t.Fatalf("a well-formed plan dir was refused: %s", refusal.Sentence())
	}
	if got := Names(loaded); !reflect.DeepEqual(got, []string{"T1", "T2"}) {
		t.Errorf("loaded order = %v, want [T1 T2]", got)
	}

	// A broken front matter names its file — the reader fixes the file the
	// refusal points at, not the whole directory. A bare line inside the
	// region is malformed on purpose: silent acceptance there is how a
	// `depends_on` typo becomes a task that never waits for anything.
	write("T3.md", "---\nid: T3\ntitle: broken\nthis line has no colon at all\ndepends_on: [T2]\n---\n")
	_, refusal = LoadDir(dir)
	if refusal == nil {
		t.Fatal("malformed front matter was accepted")
	}
	if !strings.Contains(refusal.Sentence(), "T3.md") {
		t.Errorf("the refusal %q does not name T3.md", refusal.Sentence())
	}
}

// TestSelfContainedRefusalSentences — every refusal reads as a sentence a
// human can act on: what is wrong, which task, and what to do. A refusal
// that only codes is how a plan gets edited by trial and error.
func TestSelfContainedRefusalSentences(t *testing.T) {
	_, r1 := Load(tasks([2]any{"T1", []string{"T7"}}))
	if r1 == nil || !strings.Contains(r1.Sentence(), "T7") || !strings.Contains(r1.Sentence(), "does not exist") {
		t.Errorf("missing-dependency sentence unreadable: %v", r1)
	}
	_, r2 := Load(tasks([2]any{"T1", nil}, [2]any{"T1", nil}))
	if r2 == nil || !strings.Contains(r2.Sentence(), "twice") {
		t.Errorf("duplicate-id sentence unreadable: %v", r2)
	}
	fmt.Fprint(os.Stderr, "") // keep fmt meaningful for -v output below
	t.Logf("sentences:\n  %s\n  %s", r1.Sentence(), r2.Sentence())
}
