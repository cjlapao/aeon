package roadmap

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// MoveTimes reads when an item started and finished from the audit's own rows: nothing before it
// ran, a start once it runs, a finish once it is done — and never a made-up time.
func TestMoveTimesReadTheAudit(t *testing.T) {
	root := "/repo/times"
	r, s := openRoadmapStore(t, root)
	ctx := context.Background()
	it, ref := r.CreateAudited(root, NewItem{Title: "timed", Provenance: ProvenanceAuthored, Priority: PriorityKnown, PriorityLabel: "now"}, doorPerson)
	if ref != nil {
		t.Fatalf("create: %s", ref.Reason)
	}
	if st, fin, err := r.MoveTimes(ctx, it.ID); err != nil || st != "" || fin != "" {
		t.Fatalf("a fresh item: started %q finished %q err %v, want none", st, fin, err)
	}

	r.TransitionAudited(it.ID, StatusIdeating, "ideate", doorPerson)
	r.SetEstimateAudited(it.ID, 2, 3, "a pairing day each", doorPerson)
	r.LandBriefAudited(it.ID, root+"/brief.md", Actor{Kind: "system", ID: "reactor"})
	r.LandPlanAudited(it.ID, root+"/plan.md", Actor{Kind: "system", ID: "reactor"})
	if _, ref := r.Transition(it.ID, StatusReady); ref != nil {
		t.Fatalf("ready: %s", ref.Reason)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "item-1-build", ComponentRoot: root, Goal: "g"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if _, ref := r.TransitionAudited(it.ID, StatusRunning, "materialize", doorPerson); ref != nil {
		t.Fatalf("running: %s", ref.Reason)
	}
	st, fin, err := r.MoveTimes(ctx, it.ID)
	if err != nil || st == "" || fin != "" {
		t.Fatalf("running item: started %q finished %q err %v, want a start and no finish", st, fin, err)
	}

	if _, ref := r.TransitionAudited(it.ID, StatusDone, "transition", SystemReactor); ref != nil {
		t.Fatalf("done: %s", ref.Reason)
	}
	st2, fin2, err := r.MoveTimes(ctx, it.ID)
	if err != nil || st2 != st || fin2 == "" {
		t.Fatalf("done item: started %q (was %q) finished %q err %v, want the same start and a finish", st2, st, fin2, err)
	}
}
