package daemon

// mergequeue_reactor_test.go — the queue's behaviours on the driving
// side: the modes, the landing slot, the base that moves, the three
// staleness states, and the person's merge-now word.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/store"
)

// behaviour 1 + the slot rule: manual mode drives a queued item all the
// way to ready by itself, waits there, and lands only on the person's
// word — ending on origin/main with the worktree settled.
func TestHappyPathEndsOnBaseAndCleansWorktree(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t) // the spec's grants-allowed land fixture (S56 rows, not stubs)
	e.configure(t, "manual", "ask", 3)
	f := e.feature(t, "feat-land", "")
	it := e.enqueue(t, f)

	got := e.waitFor(t, it.ID, "ready")
	if got.Status != "ready" {
		t.Fatalf("status = %s, want ready", got.Status)
	}
	if got.VerifiedSHA == "" || got.IntegrationSHA == "" {
		t.Fatalf("ready without verified/integration facts: %+v", got)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("manual mode merged without the person (%d merge calls)", e.mergeCalls())
	}

	// The item is ATTENDED, so the loop asks for every outward action
	// (S56 rule 1: an attended loop asks; a grant frees only the
	// unattended landing). The person's hands-on walk: approve the
	// publish, approve the PR, then say merge-now at the land-check
	// gate AND ask the queue to land — both halves of the act (the gate
	// authorises the lane, the queue door opens the slot transition; T8
	// fuses them into one command) — and approve the landing itself.
	slug := got.LandingLoopSlug
	e.answerGate(t, e.waitWaiting(t, slug), "approve") // git.publishIntegration
	e.answerGate(t, e.waitWaiting(t, slug), "approve") // forge.openLandingPR
	if n := e.waitForPR(t, it.ID); n == nil || *n <= 0 {
		t.Fatal("no landing PR opened on the forge")
	}
	e.landRequest(it.ID)
	e.answerGate(t, e.waitWaiting(t, slug), "merge-now") // the land-check gate
	e.answerGate(t, e.waitWaiting(t, slug), "approve")   // forge.land
	e.settle(t)

	got = e.item(t, it.ID)
	if got.Status != "merged" {
		t.Fatalf("after the word: status = %s (%s), want merged", got.Status, got.ReasonKind)
	}
	if got.LandedSHA == "" {
		t.Fatal("merged without landed_sha")
	}
	pr, err := e.fake.ReadPR(context.Background(), *got.PRNumber)
	if err != nil || !pr.Merged || pr.MergeSHA != got.LandedSHA {
		t.Fatalf("landed_sha %q is not the forge's merge on PR %v: %+v %v", got.LandedSHA, *got.PRNumber, pr, err)
	}
	if e.mergeCalls() != 1 {
		t.Fatalf("want exactly one merge call, got %d", e.mergeCalls())
	}
	// Worktree gone; ledger settled (no live integration row).
	if !mqWorktreeGone(t, e.ws, it.ID) {
		t.Fatalf("integration worktree still on disk")
	}
	rows, err := e.s.ListLiveLoopWorktrees(store.Scope{ComponentRoot: e.ws})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.OwnerKind == "integration" && r.OwnerID == it.ID {
			t.Fatalf("live integration ledger row survived the merge: %+v", r)
		}
	}
	// The feature branch is the person's — the queue never deletes it.
	if out := mustGitOut(t, e.ws, "ls-remote", "--heads", "origin", f); out == "" {
		t.Fatalf("the queue deleted the feature branch %s", f)
	}
}

// Do-NOT: mode=off holds every item — no loop starts, nothing moves,
// even with everything else in place.
func TestModeOffHolds(t *testing.T) {
	e := newMQEnv(t)
	e.configure(t, "off", "ask", 3)
	f := e.feature(t, "feat-off", "")
	it := e.enqueue(t, f)
	e.landRequest(it.ID) // even a demand cannot start a landing under off
	e.settle(t)
	got := e.item(t, it.ID)
	if got.Status != "queued" {
		t.Fatalf("off mode moved the item to %s", got.Status)
	}
	if got.LandingLoopSlug != "" {
		t.Fatalf("off mode started a landing loop: %s", got.LandingLoopSlug)
	}
}

// behaviour 2: the landing slot admits one item per repository, in
// position order, across two base branches.
func TestQueueLandsOneAtATimeInOrder(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "auto", "ask", 3)
	f1 := e.feature(t, "feat-one", "")
	f2 := e.feature(t, "feat-two", "")
	// UNATTENDED items: auto mode with standing grants is the queue
	// landing itself — nobody at the keyboard (attended items ASK, which
	// is the happy-path test above).
	i1 := e.enqueueAttended(t, f1, false)
	i2 := e.enqueueAttended(t, f2, false)
	if i1.Position >= i2.Position {
		t.Fatalf("positions out of order: %d vs %d", i1.Position, i2.Position)
	}
	e.settle(t)
	for _, id := range []string{i1.ID, i2.ID} {
		if got := e.item(t, id); got.Status != "merged" {
			t.Fatalf("item %s ended %s (%s), want merged\nqueue: %s\nloops: %s\nlog:\n%s", id, got.Status, got.ReasonKind, e.dump(), e.dumpLoops(), e.w.logLines())
		}
	}
	// Order: the PRs (and merges) reached the forge item-one first.
	var firstMerge int
	for _, c := range e.fake.Calls() {
		if c.Method == "MergePR" && firstMerge == 0 {
			firstMerge = c.Args[0].(int)
		}
	}
	it1 := e.item(t, i1.ID)
	if it1.PRNumber == nil {
		t.Fatalf("the head item merged without a landing PR number: %+v", it1)
	}
	pr1, err := e.fake.ReadPR(context.Background(), *it1.PRNumber)
	if err != nil {
		t.Fatal(err)
	}
	if firstMerge != pr1.Number {
		t.Fatalf("first merge was PR %d, not the head item's PR %d", firstMerge, pr1.Number)
	}
	if e.mergeCalls() != 2 || e.openPRCalls() != 2 {
		t.Fatalf("want 2 opens + 2 merges, got %d + %d", e.openPRCalls(), e.mergeCalls())
	}
}

// behaviour 3: the base moving under a ready item sends it back through
// catch-up, bounded by max_catch_ups; the integrate ref is NEVER deleted.
func TestBaseMovesTriggersCatchUpBounded(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 2) // manual: nothing lands without the word, so the bound is what the test watches
	f := e.feature(t, "feat-base", "")
	it := e.enqueue(t, f)

	e.waitFor(t, it.ID, "ready")
	e.baseMove(t, "move-1")
	e.waitFor(t, it.ID, "ready")
	got := e.item(t, it.ID)
	if got.CatchUps != 1 {
		t.Fatalf("after one base move catch_ups = %d, want 1", got.CatchUps)
	}
	e.baseMove(t, "move-2")
	e.waitFor(t, it.ID, "ready")
	if got = e.item(t, it.ID); got.CatchUps != 2 {
		t.Fatalf("after two base moves catch_ups = %d, want 2", got.CatchUps)
	}
	e.baseMove(t, "move-3") // the third move meets the bound of 2
	got = e.waitFor(t, it.ID, "ready", "failed")
	if got.Status != "failed" {
		t.Fatalf("exhausted catch-ups ended %s, want failed (catch_ups %d)", got.Status, got.CatchUps)
	}
	if got.ReasonKind != string(mergequeue.ReasonStaleExhausted) {
		t.Fatalf("reason = %q, want stale-exhausted", got.ReasonKind)
	}
	if got.CatchUps > 2 {
		t.Fatalf("catch-ups ran past the bound: %d", got.CatchUps)
	}
	// The Do-NOT: the integrate ref still exists (new SHAs ride it;
	// deletion is nobody's business).
	if out := mustGitOut(t, e.ws, "for-each-ref", "refs/heads/"+got.IntegrationBranch); out == "" {
		t.Fatalf("integration branch %s was deleted", got.IntegrationBranch)
	}
}

// behaviour 12: a missing/unfetchable remote ref reads `unavailable`,
// never a confident zero. Under auto+grant the item WOULD land on a
// zero-behind read — the hold at ready is the proof the read stayed
// three-state.
func TestStaleReadsAreThreeStates(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "auto", "ask", 3)
	f := e.feature(t, "feat-stale", "")
	it := e.enqueueAttended(t, f, false) // unattended: the point is the LANDING that would ride a false-zero read
	e.waitFor(t, it.ID, "ready", "merged")
	if e.item(t, it.ID).Status == "merged" {
		t.Skip("landed before the remote could be cut — fixture timing, not the behaviour")
	}
	severRemote(t, e.ws)
	e.settle(t)
	got := e.item(t, it.ID)
	if got.Status != "ready" {
		t.Fatalf("an unavailable behind read moved the item to %s — false confidence", got.Status)
	}
}

// behaviour 8 + MQ13: auto mode without a grant is autonomy without a
// floor — the landing denies at the forge door and ends failed(denied),
// never a silent bypass.
func TestAutoWithoutGrantDenies(t *testing.T) {
	e := newMQEnv(t)
	// The landing's OUTWARD set is granted (publish, PR) but NOT
	// forge.land, and the item is UNATTENDED (behaviour 8's words: auto
	// and unattended — an attended item would be ASKED). The landing
	// therefore rides to the forge door and is denied THERE.
	e.grant(t, "git.publishIntegration", "forge.openLandingPR")
	e.configure(t, "auto", "ask", 3)
	f := e.feature(t, "feat-nogranted", "")
	it := e.enqueueAttended(t, f, false)
	e.settle(t)
	got := e.waitFor(t, it.ID, "failed")
	if got.ReasonKind != string(mergequeue.ReasonDenied) {
		t.Fatalf("reason = %q, want denied (status %s)", got.ReasonKind, got.Status)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("a denied landing still merged %d times", e.mergeCalls())
	}
}

// behaviour 9's one named leg: an already-merged PR found while landing
// settles to merged WITHOUT a second merge call.
func TestAlreadyMergedPRNotMergedTwice(t *testing.T) {
	e := newMQEnv(t)
	e.configure(t, "manual", "ask", 3)
	f := e.feature(t, "feat-external", "")
	it := e.enqueue(t, f)
	e.waitFor(t, it.ID, "ready")
	if e.mergeCalls() != 0 {
		t.Fatalf("manual mode merged before the person (%d)", e.mergeCalls())
	}
	// ATTENDED loop: the person walks the outward asks to the point
	// where the landing PR exists.
	slug := e.item(t, it.ID).LandingLoopSlug
	e.answerGate(t, e.waitWaiting(t, slug), "approve") // git.publishIntegration
	e.answerGate(t, e.waitWaiting(t, slug), "approve") // forge.openLandingPR
	e.waitForPR(t, it.ID)

	// A person merges the landing PR on the forge directly — the queue
	// has not landed anything; the next read finds it merged. The merge
	// rides the fixture's forge (the git-world base moves with it, as a
	// real merge moves a real base).
	if _, err := e.forger.MergePR(context.Background(), *e.item(t, it.ID).PRNumber, "squash"); err != nil {
		t.Fatal(err)
	}
	before := e.mergeCalls()
	e.settle(t)
	got := e.item(t, it.ID)
	if got.Status != "merged" {
		t.Fatalf("external merge not adopted: status %s (%s)", got.Status, got.ReasonKind)
	}
	if e.mergeCalls() != before {
		t.Fatalf("the queue merged the PR AGAIN (%d new calls)", e.mergeCalls()-before)
	}
	pr, err := e.fake.ReadPR(context.Background(), *got.PRNumber)
	if err != nil || pr.MergeSHA != got.LandedSHA {
		t.Fatalf("landed_sha %q is not the forge's recorded merge: %+v %v", got.LandedSHA, pr, err)
	}
}

// severRemote makes the CLONE's origin unreachable for fetches (pushes
// too) — the queue loses its window onto the world.
func severRemote(t *testing.T, ws string) {
	t.Helper()
	mqGit(t, ws, "remote", "set-url", "origin", filepath.Join(ws, "..", "no-such-remote.git"))
}
