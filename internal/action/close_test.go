package action

// T7's closeIssue tests, written first. `Closes #n` does not fire:
// GitHub's keyword auto-closes only on a merge into the repository's
// DEFAULT branch, and task PRs target the FEATURE branch — a PR body
// containing "Closes #14" is a comforting no-op. So closeIssue is the
// only thing that closes a task issue when its work lands, and it must
// READ THE ISSUE BACK: a silently-failed close leaves the issue open
// with nothing downstream noticing, indefinitely.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

// TestClosesKeywordDoesNotFire — against the fake (which models the
// measured GitHub behaviour: a merge into a NON-default branch touches
// no issue): a merged task PR whose body says `Closes #4` leaves the
// issue OPEN. This is the existence proof for forge.closeIssue.
func TestClosesKeywordDoesNotFire(t *testing.T) {
	f := forge.NewFake()
	f.SeedIssue(forge.Issue{Number: 4, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}})
	// The comforting no-op body, exactly as the superseded design wrote
	// it, riding the PR that will merge into the FEATURE branch.
	opened, err := f.OpenPR(context.Background(), forge.PRRequest{
		Title: "[4a17] Add version endpoint", Body: "Closes #4",
		Head: "task/4a17-version/4a17", Base: "feature/4a17-version"})
	if err != nil {
		t.Fatal(err)
	}
	approveMerge(t, f, opened.Number) // T8's gate: merges reach the forge only past an approve
	a := &ForgeActions{Forge: f, Merge: &mergeProbe{}}
	if _, err := a.MergePR(context.Background(), mergeArgsFor(opened.Number)); err != nil {
		t.Fatalf("the merge itself must succeed: %v", err)
	}
	iss, err := f.ReadIssue(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if iss.State != "open" {
		t.Fatalf("the fake models the measured behaviour: a feature-branch merge does NOT auto-close — if this ever closes, the fixture drifted from GitHub: %+v", iss)
	}
	// And the action closes it, which is why it exists (D4).
	res, err := a.CloseIssue(context.Background(), CloseArgs{IssueNumber: 4})
	if err != nil || !res.Closed || res.AlreadyClosed {
		t.Fatalf("first close reports the fresh close, not already-closed: %+v (%v)", res, err)
	}
	iss, _ = f.ReadIssue(context.Background(), 4)
	if iss.State != "closed" {
		t.Fatalf("closeIssue is the only thing that closes: %+v", iss)
	}
}

// TestCloseReadsTheIssueBack — THE assertion of this task: a fake that
// reports success WITHOUT changing state makes the action fail. Checking
// the return value of a close passes against a forge that accepted the
// request and did nothing (D4, C4).
func TestCloseReadsTheIssueBack(t *testing.T) {
	f := forge.NewFake()
	f.SeedIssue(forge.Issue{Number: 4, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}})
	f.CloseSilentlyFails = true // reports success, state unchanged — the silent failure
	a := &ForgeActions{Forge: f}
	_, err := a.CloseIssue(context.Background(), CloseArgs{IssueNumber: 4})
	if err == nil {
		t.Fatal("a close that reports success without changing state must FAIL — the loop would otherwise believe a task done that the tracker disagrees with, indefinitely")
	}
	if !strings.Contains(err.Error(), "4") || !strings.Contains(err.Error(), "read") {
		t.Fatalf("the failure must name the issue and the read-back (a refusal that cannot say what it refused...): %v", err)
	}
	// The read-back actually happened after the close call, in that order.
	calls := f.Calls()
	closedAt, readAfter := -1, -1
	for i, c := range calls {
		if c.Method == "CloseIssue" {
			closedAt = i
		}
		if c.Method == "ReadIssue" && closedAt >= 0 && i > closedAt {
			readAfter = i
		}
	}
	if closedAt < 0 || readAfter < 0 {
		t.Fatalf("the close must be followed by a read of the issue: %v", calls)
	}
}

// TestAlreadyClosedIsDone — check-then-act: read first; state closed ⇒
// success with no close call.
func TestAlreadyClosedIsDone(t *testing.T) {
	f := forge.NewFake()
	f.SeedIssue(forge.Issue{Number: 4, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}, State: "closed"})
	a := &ForgeActions{Forge: f}
	before := len(f.Calls())
	res, err := a.CloseIssue(context.Background(), CloseArgs{IssueNumber: 4})
	if err != nil || !res.AlreadyClosed {
		t.Fatalf("closed is done: %+v (%v)", res, err)
	}
	for _, c := range f.Calls()[before:] {
		if c.Method == "CloseIssue" {
			t.Fatalf("an already-closed issue costs a read, not a close: %v", f.Calls()[before:])
		}
	}
}

// TestMissingIssueIsNotFoundNotRefused — the discriminator matters: "no
// such issue" (someone deleted it) is not "the forge said no".
func TestMissingIssueIsNotFoundNotRefused(t *testing.T) {
	a := &ForgeActions{Forge: forge.NewFake()}
	_, err := a.CloseIssue(context.Background(), CloseArgs{IssueNumber: 999})
	if err == nil {
		t.Fatal("a missing issue must fail")
	}
	if !strings.Contains(err.Error(), "not-found") {
		t.Fatalf("must read as not-found (O5 discriminates): %v", err)
	}
}

func TestCloseIsOutwardAndDeniedUnattended(t *testing.T) {
	r, err := Check(ForgeCloseIssue)
	if err != nil || r.Class != EffectOutward || r.Verdict != VerdictAsk {
		t.Fatalf("closeIssue is outward and asks: %+v (%v)", r, err)
	}
	r, err = Resolve(ForgeCloseIssue, false, false)
	if err != nil || r.Verdict != VerdictDeny || !strings.Contains(r.Sentence, "outward") {
		t.Fatalf("denied unattended naming the class: %+v (%v)", r, err)
	}
}
