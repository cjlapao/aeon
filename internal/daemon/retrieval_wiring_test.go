package daemon

// S22 T7 — the third door onto one index. The step-declaration resolver must
// be the SAME function value the `search` tool and the `search.query` socket
// method hold (T6's one-resolver rule, D15); a second resolver is a second
// index opinion, which is what V1's whole argument forbids. Function values
// are not comparable in Go, so identity is proven BEHAVIOURALLY: both doors
// call the one closure Options carried, counted.

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/search"
)

func TestTheStepDoorHoldsTheSameResolverAsTheOtherDoors(t *testing.T) {
	var asked int
	resolver := func(_ context.Context, _, _, _ string, _ int) (search.SearchAnswer, error) {
		asked++
		return search.SearchAnswer{}, nil
	}
	d := New(Options{Search: resolver})
	runner, ok := d.Table.Runner.(StepRunner)
	if !ok {
		t.Fatalf("the table's runner is %T, not the daemon's StepRunner", d.Table.Runner)
	}
	if runner.Search == nil {
		t.Fatal("the step door got no resolver while Options carried one — contextRetrievals would be dead on arrival")
	}
	if d.Table.Search == nil {
		t.Fatal("the socket door lost its resolver")
	}
	if _, err := d.Table.Search(context.Background(), "/r", "code", "q", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Search(context.Background(), "/r", "code", "q", 3); err != nil {
		t.Fatal(err)
	}
	if asked != 2 {
		t.Errorf("%d calls reached the Options resolver; both doors must ride the one value (D15)", asked)
	}

	// The no-retrieval daemon: no door carries a resolver, and the step
	// door's refusal of a declaration is pinned in protocol's
	// retrieval_decl_test.go — nothing here may fabricate a resolver.
	d2 := New(Options{})
	r2, ok := d2.Table.Runner.(StepRunner)
	if !ok || r2.Search != nil {
		t.Error("a daemon built without retrieval must carry no step resolver (V13)")
	}
	if d2.Table.Search != nil {
		t.Error("the socket door was fabricated from nothing")
	}
}
