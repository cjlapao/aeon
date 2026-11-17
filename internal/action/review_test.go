package action

// T7's tests, written first. postReview is the ONE receipt-first action —
// a review has no natural business key: same body, same verdict, two
// reviews, and nothing distinguishes them. Every other action has a key
// (title+label, head ref, branch name, tree), which is why they are
// check-then-act and this one cannot be. The exception is the point.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

// memIntents is the in-memory twin of the store's action_intent table
// (T10 wires the store-backed one; the SHAPE is the contract).
type memIntents struct {
	rows     []Intent
	recorded []struct {
		in Intent
		id int64
	}
	beginErr error
}

func (m *memIntents) BeginIntent(_ context.Context, in Intent) error {
	if m.beginErr != nil {
		return m.beginErr
	}
	for _, r := range m.rows {
		if r.StepID == in.StepID && r.Attempt == in.Attempt && r.Action == in.Action && r.PR == in.PR {
			return fmt.Errorf("intent exists for step %d attempt %d %s pr %d", in.StepID, in.Attempt, in.Action, in.PR)
		}
	}
	m.rows = append(m.rows, in)
	return nil
}

func (m *memIntents) RecordIntent(_ context.Context, in Intent, reviewID int64) error {
	for i := range m.rows {
		r := &m.rows[i]
		if r.StepID == in.StepID && r.Attempt == in.Attempt && r.Action == in.Action && r.PR == in.PR && r.ReviewID == 0 {
			r.ReviewID = reviewID
			m.recorded = append(m.recorded, struct {
				in Intent
				id int64
			}{in, reviewID})
			return nil
		}
	}
	return fmt.Errorf("no open intent to record")
}

func (m *memIntents) OpenIntent(_ context.Context, pr int, stepID int64, attempt int) (*Intent, error) {
	for _, r := range m.rows {
		if r.PR == pr && r.StepID == stepID && r.Attempt == attempt && r.ReviewID == 0 {
			got := r
			return &got, nil
		}
	}
	return nil, nil
}

func reviewArgs(pr int) ReviewArgs {
	names, _ := NewBranchNames("4a17-version", "4a17")
	return ReviewArgs{Names: names, PR: pr, StepID: 88, Attempt: 1, Loop: "4a17-version", Body: "Verified: the version endpoint answers."}
}

// TestReconcilesByIdNeverByBody — after a crash between post and record,
// the intent resolves BY ID. Two identical-bodied reviews present, one a
// decoy: body matching would find the decoy (or merge two), and both are
// wrong in a way nobody can see afterwards.
func TestReconcilesByIdNeverByBody(t *testing.T) {
	f := forge.NewFake()
	// Decoys with OUR body exist BEFORE the attempt (low ids), our real
	// review landed AFTER (high id) but before the crash.
	f.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove) // decoy #1 (id 1)
	f.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove) // decoy #2 (id 2)
	f.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove) // OURS (id 3) — posted, crash before recording
	m := &memIntents{}
	m.rows = []Intent{{Loop: "4a17-version", StepID: 88, Attempt: 1, Action: ForgePostReview, PR: 3, PreReviewID: 2}}
	a := &ForgeActions{Forge: f, Intents: m}

	res, err := a.PostReview(context.Background(), reviewArgs(3))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.ReviewID != 3 || !res.Reconciled {
		t.Fatalf("the review that appeared after the baseline (id 3) is ours — by id, never by body: %+v", res)
	}
	if len(m.recorded) != 1 || m.recorded[0].id != 3 {
		t.Fatalf("the recorded id must be 3, got %v", m.recorded)
	}
	// No repost on the recovery path.
	for _, c := range f.Calls() {
		if c.Method == "PostReview" {
			t.Fatalf("an intent with no id means go and LOOK, not post again: %v", f.Calls())
		}
	}
}

// TestIntentWithNoIdTriggersLookup — the resume path lists reviews; it
// does not post.
func TestIntentWithNoIdTriggersLookup(t *testing.T) {
	f := forge.NewFake()
	f.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove) // ours, post-crash
	m := &memIntents{}
	m.rows = []Intent{{Loop: "4a17-version", StepID: 88, Attempt: 1, Action: ForgePostReview, PR: 3, PreReviewID: 0}}
	a := &ForgeActions{Forge: f, Intents: m}
	res, err := a.PostReview(context.Background(), reviewArgs(3))
	if err != nil || res.ReviewID != 1 || !res.Reconciled {
		t.Fatalf("resume resolves the unrecorded post: %+v (%v)", res, err)
	}
	for _, c := range f.Calls() {
		if c.Method == "PostReview" {
			t.Fatal("lookup, not repost")
		}
	}
}

// TestUnresolvableIntentFailsRatherThanReposting (O3) — an unresolvable
// intent is the honest unknown, and S7 established that failing beats
// guessing when an effect may or may not have happened.
func TestUnresolvableIntentFailsRatherThanReposting(t *testing.T) {
	// Zero candidates after the baseline: did the post happen? Unknown.
	f := forge.NewFake()
	m := &memIntents{}
	m.rows = []Intent{{Loop: "4a17-version", StepID: 88, Attempt: 1, Action: ForgePostReview, PR: 3, PreReviewID: 0}}
	a := &ForgeActions{Forge: f, Intents: m}
	_, err := a.PostReview(context.Background(), reviewArgs(3))
	if err == nil {
		t.Fatal("an unresolvable intent must fail rather than post into a possible duplicate")
	}
	if !strings.Contains(err.Error(), "intent") || !strings.Contains(err.Error(), "88") {
		t.Fatalf("the failure must NAME the intent (step/attempt/pr): %v", err)
	}
	for _, c := range f.Calls() {
		if c.Method == "PostReview" {
			t.Fatalf("failing beats guessing: %v", f.Calls())
		}
	}

	// Two candidates after the baseline: also unknown — which one was
	// ours? Not answerable by body, and guessing is forbidden.
	f2 := forge.NewFake()
	f2.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove)
	f2.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove)
	m2 := &memIntents{}
	m2.rows = []Intent{{Loop: "4a17-version", StepID: 88, Attempt: 1, Action: ForgePostReview, PR: 3, PreReviewID: 0}}
	a2 := &ForgeActions{Forge: f2, Intents: m2}
	if _, err := a2.PostReview(context.Background(), reviewArgs(3)); err == nil {
		t.Fatal("two candidates is also unknown — fail naming it (D7: two identical-bodied reviews are two reviews)")
	}
}

// TestReconciliationIsBounded (O3) — a PR with hundreds of reviews must
// not be listed a review at a time during recovery: at most ONE
// ListReviews per resolve, and no per-review reads.
func TestReconciliationIsBounded(t *testing.T) {
	f := forge.NewFake()
	for i := 0; i < 200; i++ {
		f.SeedReview(3, fmt.Sprintf("noise %d", i), forge.VerdictComment)
	}
	f.SeedReview(3, "Verified: the version endpoint answers.", forge.VerdictApprove)
	m := &memIntents{}
	m.rows = []Intent{{Loop: "4a17-version", StepID: 88, Attempt: 1, Action: ForgePostReview, PR: 3, PreReviewID: 200}}
	a := &ForgeActions{Forge: f, Intents: m}
	res, err := a.PostReview(context.Background(), reviewArgs(3))
	if err != nil || res.ReviewID != 201 {
		t.Fatalf("the window resolves to the one post-baseline review: %+v (%v)", res, err)
	}
	lists, reads := 0, 0
	for _, c := range f.Calls() {
		switch c.Method {
		case "ListReviews":
			lists++
		case "ReadPR":
			reads++
		}
	}
	if lists != 1 || reads != 0 {
		t.Fatalf("bounded: one listing, no per-review reads — lists=%d reads=%d", lists, reads)
	}
}

// TestPostReviewIsTheOnlyReceiptFirstAction — enumeration; the exception
// is the point, so the enumeration is the assertion.
func TestPostReviewIsTheOnlyReceiptFirstAction(t *testing.T) {
	receiptFirst, checkThenAct, natural := 0, 0, 0
	// S60 T5 (dated 2026-11-04): the queue's three join the enumeration —
	// every one CHECK-THEN-ACT (the ancestor check, the clean-tree assert,
	// the new-ref push: each asks git first and answers the re-run itself).
	// verify.run stays out of THIS enumeration exactly as S57 left it (it
	// is a natural, and the natural row here pins Audit alone).
	for _, name := range []string{GitCreateBranch, GitCommitAndPush, GitMergeBranch, GitIntegrate, GitConcludeIntegration, GitPublishIntegration, ForgeOpenIssue, ForgeOpenPR, ForgePostReview, ForgeMerge, ForgeCloseIssue, Audit} {
		switch Strategy(name) {
		case StrategyReceiptFirst:
			receiptFirst++
			if name != ForgePostReview {
				t.Fatalf("%s must not be receipt-first (it has a business key)", name)
			}
		case StrategyCheckThenAct:
			checkThenAct++
			if name == ForgePostReview {
				t.Fatalf("postReview cannot be check-then-act — a review has no natural business key (D7)")
			}
		case StrategyNaturallyIdempotent:
			natural++
			if name != Audit {
				t.Fatalf("%s is a read wearing audit's coat?", name)
			}
		default:
			t.Fatalf("%s has no declared strategy", name)
		}
	}
	if receiptFirst != 1 || checkThenAct != 10 || natural != 1 {
		t.Fatalf("exactly one receipt-first and one natural of the twelve: %d / %d / %d", receiptFirst, checkThenAct, natural)
	}
}

// TestIntentRequiresTheStore — a receipt-first action without its intent
// path must refuse to run (one recovery path or none, never "wing it").
func TestIntentRequiresTheStore(t *testing.T) {
	a := &ForgeActions{Forge: forge.NewFake()}
	_, err := a.PostReview(context.Background(), reviewArgs(3))
	if err == nil || !strings.Contains(err.Error(), "intent") {
		t.Fatalf("no intent store, no postReview: %v", err)
	}
}

func TestReviewIsOutwardAndDeniedUnattended(t *testing.T) {
	r, err := Check(ForgePostReview)
	if err != nil || r.Class != EffectOutward || r.Verdict != VerdictAsk {
		t.Fatalf("postReview is outward and asks: %+v (%v)", r, err)
	}
	r, err = Resolve(ForgePostReview, false, false)
	if err != nil || r.Verdict != VerdictDeny || !strings.Contains(r.Sentence, "effect class 'outward' resolves to deny (loop is unattended)") {
		t.Fatalf("denied unattended naming the class: %+v (%v)", r, err)
	}
}
