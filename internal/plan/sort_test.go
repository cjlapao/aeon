package plan

import (
	"reflect"
	"strings"
	"testing"
)

// S17 T1 — the sort itself. A deterministic Kahn pass over the plan's
// DECLARED order, never a map-iteration traversal: an unstable sort makes a
// loop non-reproducible across runs from the same plan, which breaks L6's
// whole reason for pinning the blueprint.

// TestWhenTwoAreReadyPlanOrderDecides — the stable half of Kahn: whenever
// several tasks are simultaneously eligible, the one declared FIRST goes
// next. That is what "keep their plan order" means under dependencies.
func TestWhenTwoAreReadyPlanOrderDecides(t *testing.T) {
	in := tasks(
		[2]any{"zeta", nil},            // ready at once
		[2]any{"alpha", []string{"z"}}, // NOT ready until z lands
		[2]any{"z", nil},
	)
	sorted, refusal := Load(in)
	if refusal != nil {
		t.Fatalf("refused a legal plan: %s", refusal.Sentence())
	}
	got := Names(sorted)
	// zeta is ready first AND declared first; z is ready second. alpha waits.
	want := []string{"zeta", "z", "alpha"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v — the ready set must drain in plan order", got, want)
	}
}

// TestLongChain — a hundred-task chain sorts in chain order regardless of
// how it was declared (the recursion-hazard check: a recursive DFS on this
// would be fine, an accidental O(n²) scan is fine too, but the ORDER is the
// assertion).
func TestLongChain(t *testing.T) {
	const n = 100
	in := make([]Task, 0, n)
	for i := 0; i < n; i++ {
		deps := []string(nil)
		if i > 0 {
			deps = []string{itoh(i - 1)}
		}
		in = append(in, Task{ID: itoh(i), DependsOn: deps})
	}
	// Declared BACKWARDS — the sort must undo the declaration order only
	// where dependencies demand it.
	rev := make([]Task, 0, n)
	for i := n - 1; i >= 0; i-- {
		rev = append(rev, in[i])
	}
	sorted, refusal := Load(rev)
	if refusal != nil {
		t.Fatalf("refused a legal chain: %s", refusal.Sentence())
	}
	got := Names(sorted)
	for i := 0; i < n; i++ {
		if got[i] != itoh(i) {
			t.Fatalf("position %d = %s, want %s — chain order broken", i, got[i], itoh(i))
		}
	}
}

func itoh(i int) string { return "t" + strings.Repeat("0", 3) + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestLoadDoesNotMutateItsInput — the engine walks the sorted list; the
// caller's declaration order is the plan's identity (and stability is
// defined against it). A sort that reorders the input in place corrupts the
// very order the stability guarantee refers to.
func TestLoadDoesNotMutateItsInput(t *testing.T) {
	in := tasks(
		[2]any{"B", []string{"A"}},
		[2]any{"A", nil},
	)
	before := Names(in)
	if _, refusal := Load(in); refusal != nil {
		t.Fatalf("refused: %s", refusal.Sentence())
	}
	if after := Names(in); !reflect.DeepEqual(before, after) {
		t.Errorf("Load mutated its input: %v became %v", before, after)
	}
}

// TestValidationPrecedesSort — the cheap classes are refused before the
// graph is walked, and in a fixed precedence, so the same broken plan always
// reports the SAME refusal. A plan with a duplicate id AND a cycle reports
// the duplicate: fixing one thing must reveal the next refusal, not a race.
func TestValidationPrecedesSort(t *testing.T) {
	in := tasks(
		[2]any{"T1", []string{"T2"}},
		[2]any{"T2", []string{"T1"}},
		[2]any{"T1", nil}, // duplicate, AND the pair is a cycle
	)
	first, r1 := Load(in)
	if r1 == nil {
		t.Fatal("accepted")
	}
	if r1.Kind != "duplicate-id" {
		t.Errorf("first refusal = %q, want duplicate-id (fixed precedence)", r1.Kind)
	}
	if first != nil {
		t.Error("a refused load must not return a sorted list")
	}
	// Remove the duplicate: the SAME plan now reports the cycle — the next
	// refusal, deterministically.
	fixed := tasks([2]any{"T1", []string{"T2"}}, [2]any{"T2", []string{"T1"}})
	_, r2 := Load(fixed)
	if r2 == nil || r2.Kind != "cycle" {
		t.Errorf("second refusal = %v, want cycle", r2)
	}
}

// TestSelfDependencyIsNotTheCycleRefusal — a self-dependency "looks like a
// typo rather than a cycle" (the task's wording); it must refuse as its own
// class so the sentence tells the author which mistake they made.
func TestSelfDependencyIsNotTheCycleRefusal(t *testing.T) {
	_, refusal := Load(tasks([2]any{"solo", []string{"solo"}}))
	if refusal == nil {
		t.Fatal("the self-dependency was accepted")
	}
	if refusal.Kind != "self-dependency" {
		t.Errorf("kind = %q, want self-dependency — the sentence must tell the author which mistake they made", refusal.Kind)
	}
	if !strings.Contains(refusal.Sentence(), "itself") {
		t.Errorf("sentence %q does not say the task depends on itself", refusal.Sentence())
	}
}
