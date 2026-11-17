package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

// S17 T8 — the review-state merge gate (L34, D10) and the deferred-findings
// rule it feeds. The protocol's reasoning is server-agnosticism: "Not every
// server can resolve conversation threads (the granular reference server has
// NO resolve_thread / pending-review workflow). So the loop gates on the
// review STATE, which every server supports." A gate that needs a thread API
// is unreachable on the servers that lack it — and a gate nobody can pass is
// a gate people route around.
//
// The ordering is the property (same shape as S11's AllowToken): the gate
// runs INSIDE ForgeActions.MergePR before the forge is ever asked, so a
// blocked gate means MergePR never appears in the fake's Calls. A check that
// happens to run before an action is a convention; a check the action cannot
// be reached without is a property.

// gateForge stands where a richer server stands: it embeds the fake (all
// required methods, plus the threadless default) and adds the two OPTIONAL
// capabilities the gate honours when present. The plain fake not implementing
// them is exactly the threadless server the protocol names.
type gateForge struct {
	*forge.Fake
	reviews []forge.Review
	checks  []forge.Check
	checksN bool // true = the server reports checks (even an empty list)
	threads []forge.Thread
}

func (g *gateForge) ListReviews(ctx context.Context, pr int) ([]forge.Review, error) {
	if g.reviews != nil {
		return g.reviews, nil
	}
	return g.Fake.ListReviews(ctx, pr)
}

func (g *gateForge) ListStatuses(ctx context.Context, pr int) ([]forge.Check, error) {
	if !g.checksN {
		return nil, forge.NewMsg(forge.KindRefused, "no status API")
	}
	return g.checks, nil
}

func (g *gateForge) ListUnresolvedThreads(ctx context.Context, pr int) ([]forge.Thread, error) {
	return g.threads, nil
}

func gateProbe() *mergeProbe { return &mergeProbe{} }

// seeded returns a fake whose PR #3 is based on the loop's feature branch
// (the base guard runs first; these tests walk past it to the review gate).
func gateFake() *forge.Fake {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	return f
}

func approvedBy(id int64, author, at string) forge.Review {
	return forge.Review{ID: id, PR: 3, Verdict: forge.VerdictApprove, Author: author, SubmittedAt: at}
}

func requestedBy(id int64, author, at string) forge.Review {
	return forge.Review{ID: id, PR: 3, Verdict: forge.VerdictRequestChanges, Author: author, SubmittedAt: at}
}

// TestThreadlessForgeStillReachesTheGate — L34/D10: the server with no
// thread API passes the gate on STATE ALONE. A plain fake (no optional
// capability of any kind) plus one APPROVED review must merge.
func TestThreadlessForgeStillReachesTheGate(t *testing.T) {
	f := gateFake()
	if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
		t.Fatal(err)
	}
	a := &ForgeActions{Forge: f, Merge: gateProbe()}
	res, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatalf("a threadless server is the common case, not a blocker (L34): %v", err)
	}
	if res.SHA == "" {
		t.Fatal("the merge did not run")
	}
	merged := false
	for _, c := range f.Calls() {
		if c.Method == "MergePR" {
			merged = true
		}
	}
	if !merged {
		t.Fatal("the gate let the merge through without the forge being asked?")
	}
}

// TestGateRecordsWhichStateAndWhen — O6/C8: the evidence carries the review
// STATE, the REVIEWER, the TIMESTAMP and the CHECK results. Six months
// later, "why was this merged" must have an answer that is not "the loop
// said so".
func TestGateRecordsWhichStateAndWhen(t *testing.T) {
	g := &gateForge{Fake: gateFake(),
		reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")},
		checks:  []forge.Check{{Name: "build", State: "success"}, {Name: "test", State: "success"}},
		checksN: true,
	}
	a := &ForgeActions{Forge: g, Merge: gateProbe()}
	res, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"review_state": "approve",
		"review_by":    "mira",
		"review_at":    "2026-10-26T09:12:00Z",
	}
	for k, v := range want {
		if got := res.Fields[k]; got != v {
			t.Fatalf("gate evidence %s = %q, want %q — a gate nobody can reconstruct is a gate nobody can audit (O6)", k, got, v)
		}
	}
	if checks := res.Fields["checks"]; !strings.Contains(checks, "build=success") || !strings.Contains(checks, "test=success") {
		t.Fatalf("checks evidence %q must name each check and its state", checks)
	}
}

// TestNoChecksIsNotAFailure — a repository WITHOUT CI is a repository
// without CI: treating that as red blocks every such project. N/A passes;
// the evidence must SAY it was N/A rather than leaving it out.
func TestNoChecksIsNotAFailure(t *testing.T) {
	g := &gateForge{Fake: gateFake(),
		reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")},
		checks:  nil,
		checksN: true, // reports the checks API, and the list is empty
	}
	a := &ForgeActions{Forge: g, Merge: gateProbe()}
	res, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err != nil {
		t.Fatalf("no checks configured is N/A, not a failure: %v", err)
	}
	if checks := res.Fields["checks"]; !strings.Contains(checks, "not-configured") {
		t.Fatalf("checks evidence %q must record N/A explicitly", checks)
	}
}

// TestOutstandingRequestChangesBlocks — even with a LATER APPROVED, an
// outstanding REQUEST_CHANGES holds the gate. Resolution belongs to the
// person who asked for the change: their own later approval clears it;
// somebody else's does not — a second yes does not answer the first no.
func TestOutstandingRequestChangesBlocks(t *testing.T) {
	cases := []struct {
		name    string
		reviews []forge.Review
		wantErr bool
	}{
		{"request_changes then a DIFFERENT reviewer approves",
			[]forge.Review{requestedBy(1, "mira", "2026-10-26T09:00:00Z"), approvedBy(2, "devin", "2026-10-26T10:00:00Z")}, true},
		{"request_changes then the SAME reviewer approves",
			[]forge.Review{requestedBy(1, "mira", "2026-10-26T09:00:00Z"), approvedBy(2, "mira", "2026-10-26T10:00:00Z")}, false},
		{"approve then request_changes (the latest is not approve)",
			[]forge.Review{approvedBy(1, "devin", "2026-10-26T09:00:00Z"), requestedBy(2, "mira", "2026-10-26T10:00:00Z")}, true},
		{"no reviews at all", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &gateForge{Fake: gateFake(), reviews: tc.reviews}
			if tc.reviews == nil {
				g.reviews = []forge.Review{} // the server answers; the answer is "none"
			}
			a := &ForgeActions{Forge: g, Merge: gateProbe()}
			_, err := a.MergePR(context.Background(), mergeArgsFor(3))
			if tc.wantErr && err == nil {
				t.Fatal("the gate passed when it must not have")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("the gate blocked a merge it must let through: %v", err)
			}
			if tc.wantErr {
				low := strings.ToLower(err.Error())
				if !strings.Contains(low, "request_changes") && !strings.Contains(low, "approv") {
					t.Fatalf("the refusal must name the review state that holds it: %v", err)
				}
				// The ORDERING property: the forge was never asked.
				for _, c := range g.Calls() {
					if c.Method == "MergePR" {
						t.Fatalf("a blocked gate must make MergePR unreachable: %v", g.Calls())
					}
				}
			}
		})
	}
}

// TestReportedThreadsAreHonoured — where a forge reports unresolved
// threads, they block; where it does not, they are NOT consulted (the
// threadless case already passes via TestThreadlessForgeStillReachesTheGate).
func TestReportedThreadsAreHonoured(t *testing.T) {
	g := &gateForge{Fake: gateFake(),
		reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")},
		threads: []forge.Thread{{Path: "loop/merge.go", Line: 42, Author: "mira"}},
	}
	a := &ForgeActions{Forge: g, Merge: gateProbe()}
	_, err := a.MergePR(context.Background(), mergeArgsFor(3))
	if err == nil {
		t.Fatal("unresolved threads, where REPORTED, must hold the gate")
	}
	if !strings.Contains(err.Error(), "thread") {
		t.Fatalf("the refusal must name the threads: %v", err)
	}
	// And the honoured pass: no unresolved threads, threads reported.
	g.threads = nil
	if _, err := a.MergePR(context.Background(), mergeArgsFor(3)); err != nil {
		t.Fatalf("threads reported AND resolved must pass: %v", err)
	}
}

// TestChecksFailureBlocks — pending and failing checks are not green.
func TestChecksFailureBlocks(t *testing.T) {
	for _, state := range []string{"failure", "pending"} {
		g := &gateForge{Fake: gateFake(),
			reviews: []forge.Review{approvedBy(1, "mira", "2026-10-26T09:12:00Z")},
			checks:  []forge.Check{{Name: "test", State: state}},
			checksN: true,
		}
		a := &ForgeActions{Forge: g, Merge: gateProbe()}
		_, err := a.MergePR(context.Background(), mergeArgsFor(3))
		if err == nil {
			t.Fatalf("a %s check passed the checks half of the gate", state)
		}
	}
}

// TestGatePrecedesTheIrreversibleAction — ordering, not a boolean: on
// EVERY blocked shape above the fake's MergePR was never called (asserted
// per-case in TestOutstandingRequestChangesBlocks), and here once more
// with the gate refusing while the merge probe — the S16 guard that only
// speaks AFTER a merge — stays silent at zero: no probe query may happen
// before the gate lets the action through.
func TestGatePrecedesTheIrreversibleAction(t *testing.T) {
	p := gateProbe()
	g := &gateForge{Fake: gateFake(),
		reviews: []forge.Review{requestedBy(1, "mira", "2026-10-26T09:00:00Z")},
	}
	a := &ForgeActions{Forge: g, Merge: p}
	if _, err := a.MergePR(context.Background(), mergeArgsFor(3)); err == nil {
		t.Fatal("the blocked case must refuse")
	}
	if len(p.queries) != 0 {
		t.Fatalf("the D5 post-merge guard was consulted %d times on a merge that never ran — the gate must precede the merge, and the merge must precede the guard", len(p.queries))
	}
	for _, c := range g.Calls() {
		if c.Method == "MergePR" {
			t.Fatal("forge.merge reached the irreversible call without the gate passing")
		}
	}
}

// TestThreadsAreNeverResolved — honouring reported threads is in scope;
// RESOLVING them is a write into someone else's conversation and nothing
// in the method needs it. Enforced the way this repository enforces
// closed vocabularies: a grep over the gate's own source, not a promise.
func TestThreadsAreNeverResolved(t *testing.T) {
	raw, err := os.ReadFile("forge_actions.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"ResolveThread", "resolve_thread", "ResolveReview", "DismissReview"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("forge_actions.go mentions %q — Aeon honours threads, it never resolves them (T8 not-in-scope)", banned)
		}
	}
	src, err := os.ReadFile(filepath.Join("..", "forge", "forge.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"ResolveThread", "ResolveReview"} {
		if strings.Contains(string(src), banned) {
			t.Fatalf("the forge boundary declares %q — thread resolution must not even be expressible here", banned)
		}
	}
}
