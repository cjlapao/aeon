package action

// S60 T6's queue-side tests — the enqueue door (MQ13/MQ14/MQ15) and the
// registry arms that feed it. The sharpest one is the byte-comparison
// against the merge-queue validator's OWN sentence: the human door and
// the blueprint action must refuse an unknown base in the same words,
// and only a test across the boundary keeps that true.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/mergequeue"
)

type recEnq struct {
	in     QueueEnqueueInput
	called int
	res    QueueEnqueueResult
	err    error
}

func (r *recEnq) Enqueue(_ context.Context, in QueueEnqueueInput) (QueueEnqueueResult, error) {
	r.called++
	r.in = in
	return r.res, r.err
}

func TestEnqueuePassesThePinThroughAndRecords(t *testing.T) {
	var ev []Evidence
	e := &recEnq{res: QueueEnqueueResult{ItemID: "mq-abc", Status: "queued", Position: 2, Reused: true}}
	q := &QueueActions{Enq: e, Record: func(x Evidence) { ev = append(ev, x) }}
	res, err := q.Enqueue(context.Background(), QueueEnqueueInput{
		ComponentRoot: "/repo", Branch: "feature/alpha", BaseBranch: "main",
		LoopSlug: "alpha", Attended: true, ActorKind: "blueprint", ActorID: "merge-queue",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing invented, nothing dropped: the action adds no facts of its own.
	if e.in != (QueueEnqueueInput{ComponentRoot: "/repo", Branch: "feature/alpha", BaseBranch: "main",
		LoopSlug: "alpha", Attended: true, ActorKind: "blueprint", ActorID: "merge-queue"}) {
		t.Fatalf("the pin arrives whole: %+v", e.in)
	}
	if res.ItemID != "mq-abc" || !res.Reused {
		t.Fatalf("the queue's answer is the answer: %+v", res)
	}
	if len(ev) != 1 || ev[0].Action != QueueEnqueue || ev[0].Key != "queue_item_id" || ev[0].Value != "mq-abc" {
		t.Fatalf("the item id is the one evidence an enqueue mints: %v", ev)
	}
}

func TestEnqueueRefusalsNeverReachTheQueue(t *testing.T) {
	cases := []struct {
		name   string
		in     QueueEnqueueInput
		wantIn string
	}{
		{"no root", QueueEnqueueInput{Branch: "feature/a", BaseBranch: "main"}, "not attached to one"},
		{"no branch", QueueEnqueueInput{ComponentRoot: "/repo", BaseBranch: "main"}, "nothing to land"},
		{"no base", QueueEnqueueInput{ComponentRoot: "/repo", Branch: "feature/a"}, NoBaseSentence},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &recEnq{}
			q := &QueueActions{Enq: e}
			_, err := q.Enqueue(context.Background(), tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("want refusal naming %q, got %v", tc.wantIn, err)
			}
			if e.called != 0 {
				t.Fatal("refused before the service")
			}
		})
	}
	// Unwired is its own refusal, not a nil dereference.
	if _, err := (&QueueActions{}).Enqueue(context.Background(), QueueEnqueueInput{
		ComponentRoot: "/r", Branch: "feature/a", BaseBranch: "main"}); err == nil {
		t.Fatal("an enqueue with nothing to enqueue into refuses")
	}
	// A service answering with an anonymous row is refused, not recorded.
	e := &recEnq{res: QueueEnqueueResult{Status: "queued"}}
	if _, err := (&QueueActions{Enq: e}).Enqueue(context.Background(), QueueEnqueueInput{
		ComponentRoot: "/r", Branch: "feature/a", BaseBranch: "main"}); err == nil {
		t.Fatal("a row that cannot name itself is not a row")
	}
}

// TestEnqueueUnknownBaseSpeaksTheMQ15Sentence — the byte-comparison. The
// validator owns the sentence; this package reproduces it because it
// cannot import that one; drift between the two fails HERE.
func TestEnqueueUnknownBaseSpeaksTheMQ15Sentence(t *testing.T) {
	err := mergequeue.ValidateEnqueue(
		mergequeue.EnqueueRequest{ComponentRoot: "/repo", Branch: "feature/alpha"},
		mergequeue.RepoFacts{HasPrimaryRoot: true, BranchLocal: true}, // no KnownBases at all
	)
	ref, ok := err.(*mergequeue.Refusal)
	if !ok || ref.Kind != "base-unknown" {
		t.Fatalf("the validator's no-base shape: %v", err)
	}
	if ref.Reason != NoBaseSentence {
		t.Fatalf("the two doors must speak one sentence:\n validator: %s\n   action: %s", ref.Reason, NoBaseSentence)
	}
	e := &recEnq{}
	_, aerr := (&QueueActions{Enq: e}).Enqueue(context.Background(), QueueEnqueueInput{
		ComponentRoot: "/repo", Branch: "feature/alpha"})
	if aerr == nil || !strings.Contains(aerr.Error(), ref.Reason) {
		t.Fatalf("the action's refusal carries the validator's words verbatim: %v", aerr)
	}
}

// --- the registry arms --------------------------------------------------------

func queueSvcs(e QueueEnqueuer) (*Services, *[]Evidence) {
	var ev []Evidence
	return &Services{
		Queue: &QueueActions{Enq: e, Record: func(x Evidence) { ev = append(ev, x) }},
	}, &ev
}

func TestDispatchQueueEnqueueMapsThePin(t *testing.T) {
	e := &recEnq{res: QueueEnqueueResult{ItemID: "mq-1", Status: "queued", Position: 3}}
	svc, _ := queueSvcs(e)
	ev, err := Dispatch(context.Background(), svc, QueueEnqueue, Args{
		Root: "/repo", Loop: "alpha", FeatureBranch: "feature/alpha", BaseBranch: "main",
		Attended: true, ActorKind: "blueprint", ActorID: "merge-queue",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.in.LoopSlug != "alpha" || e.in.Branch != "feature/alpha" || !e.in.Attended || e.in.ActorID != "merge-queue" {
		t.Fatalf("Args mapped to the queue's vocabulary: %+v", e.in)
	}
	if ev["queue_item_id"] != "mq-1" || ev["queue_status"] != "queued" {
		t.Fatalf("the queue's answer rides the evidence: %v", ev)
	}
	// The two required fields the arm checks for itself.
	if _, err := Dispatch(context.Background(), svc, QueueEnqueue, Args{Root: "/repo"}); err == nil {
		t.Fatal("an enqueue with no loop or no branch is refused")
	}
}

func TestDispatchLandingArmsReDeriveTheBranch(t *testing.T) {
	// The queue lands only branches it named: a land whose item id and
	// integration branch disagree is refused by the DOOR, before Forge.
	f := forge.NewFake()
	svc := &Services{Forge: &ForgeActions{Forge: f, Merge: &landProbe{}, Fetch: &recFetch{}}}
	_, err := Dispatch(context.Background(), svc, ForgeLand, Args{
		Dir: "/wt", Root: "/repo", Item: mqItem, IntegrationBranch: "integrate/someone-elses-item",
		BaseBranch: "main", Loop: "alpha", PRNumber: 7, VerifiedSHA: landSHA, IntegrationSHA: landSHA,
	})
	if err == nil || !strings.Contains(err.Error(), "only branches it named") {
		t.Fatalf("the re-derived name is the door: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("refused before the forge: %v", f.Calls())
	}
	// And a matching name walks through to the action (the action's own
	// refusals are landing_test.go's; here the door opened).
	f.SeedPR(forge.PR{Number: 7, Head: "integrate/" + mqItem, BaseRef: "main", HeadSHA: landSHA})
	approveMerge(t, f, 7)
	ev, err := Dispatch(context.Background(), svc, ForgeLand, Args{
		Dir: "/wt", Root: "/repo", Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		BaseBranch: "main", Loop: "alpha", PRNumber: 7, VerifiedSHA: landSHA, IntegrationSHA: landSHA,
	})
	if err != nil {
		t.Fatalf("land through the door: %v", err)
	}
	if ev["merge_sha"] == "" {
		t.Fatalf("merge_sha evidence from the door: %v", ev)
	}
	// The required-args battery names the missing one — verified_sha is
	// required by schema, not defaulted: a land that never verified the
	// head must not be dispatchable at all.
	if _, err := Dispatch(context.Background(), svc, ForgeLand, Args{
		Dir: "/wt", Root: "/repo", Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		BaseBranch: "main", Loop: "alpha", PRNumber: 7, IntegrationSHA: landSHA,
	}); err == nil || !strings.Contains(err.Error(), "verified_sha") {
		t.Fatalf("the missing argument is named: %v", err)
	}
}

func TestDispatchOpenLandingPRMapsAndRefuses(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 11, Head: "integrate/" + mqItem, BaseRef: "main"})
	svc := &Services{Forge: &ForgeActions{Forge: f, Git: &branchProbe{present: true}}}
	ev, err := Dispatch(context.Background(), svc, ForgeOpenLandingPR, Args{
		Dir: "/repo", Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		HeadBranch: "feature/alpha", BaseBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev["pr_number"] != "11" {
		t.Fatalf("the reused number is evidence: %v", ev)
	}
	if _, err := Dispatch(context.Background(), svc, ForgeOpenLandingPR, Args{
		Dir: "/repo", Item: mqItem, IntegrationBranch: "integrate/other", HeadBranch: "feature/alpha", BaseBranch: "main",
	}); err == nil || !strings.Contains(err.Error(), "not what the pinned prefix and item compose") {
		t.Fatalf("openLandingPR keeps the same door: %v", err)
	}
}
