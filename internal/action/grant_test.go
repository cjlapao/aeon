package action

import "testing"

// TestZeroGrantContextEqualsResolve is the byte-identical-with-no-grants
// guarantee (AL9): for every registry action × attended × allowOutward,
// ResolveWithGrant with a zero GrantContext returns exactly Resolve's
// result. A zero context is the "no grant" case, and the whole point of the
// slice is that with no grant the behaviour is unchanged.
func TestZeroGrantContextEqualsResolve(t *testing.T) {
	for _, name := range Names() {
		for _, attended := range []bool{true, false} {
			for _, allowOutward := range []bool{true, false} {
				base, err := Resolve(name, attended, allowOutward)
				if err != nil {
					t.Fatalf("Resolve(%s): %v", name, err)
				}
				got, err := ResolveWithGrant(name, attended, allowOutward, GrantContext{})
				if err != nil {
					t.Fatalf("ResolveWithGrant(%s): %v", name, err)
				}
				if got.Action != base.Action || got.Class != base.Class ||
					got.Verdict != base.Verdict || got.Sentence != base.Sentence {
					t.Errorf("zero context drifted for %s attended=%v allowOutward=%v:\n  got  %v\n  want %v",
						name, attended, allowOutward, got, base)
				}
			}
		}
	}
}

// TestVetoDeniesUnattendedOutwardEvenWithTheLegacyFlag (AL3/AL7): a veto
// overrides the pin and the flag alike — it is checked before the pin and
// denies even when the person typed allowOutward.
func TestVetoDeniesUnattendedOutwardEvenWithTheLegacyFlag(t *testing.T) {
	for _, name := range []string{ForgeOpenPR, ForgePostReview, ForgeCloseIssue, GitCommitAndPush} {
		g := GrantContext{Pinned: true, InPin: true,
			Live: LiveState{Vetoed: true, VetoID: "ag-veto", GrantID: name, Rev: 1}}
		// unattended + flag set: without a veto the base would run.
		base, _ := Resolve(name, false, true)
		got, err := ResolveWithGrant(name, false, true, g)
		if err != nil {
			t.Fatalf("ResolveWithGrant(%s): %v", name, err)
		}
		if base.Verdict != VerdictRun {
			t.Fatalf("precondition: %s unattended+flag should run, got %v", name, base.Verdict)
		}
		if got.Verdict != VerdictDeny {
			t.Errorf("veto did not override the flag for %s: got %v", name, got)
		}
		if got.Sentence == "" || got.Sentence == base.Sentence {
			t.Errorf("veto sentence must name the veto grant for %s: %q", name, got.Sentence)
		}
	}
}

// TestPinnedCoveredLivePasses (AL9 rule 5): a pinned, covered, live action
// runs — the only path by which an unattended action runs under a grant.
func TestPinnedCoveredLivePasses(t *testing.T) {
	g := GrantContext{Pinned: true, InPin: true,
		Live: LiveState{GrantID: "ag-1", Rev: 5, Conditions: []ConditionStatus{{Name: "requireBudget", State: CondMet}}}}
	got, err := ResolveWithGrant(ForgeOpenPR, false, false, g)
	if err != nil {
		t.Fatalf("ResolveWithGrant: %v", err)
	}
	if got.Verdict != VerdictRun {
		t.Fatalf("live-passed should run: got %v", got)
	}
}

// TestRevokedDenies (AL9 rule 4): a revoked covering grant denies at the
// next action, naming the grant.
func TestRevokedDenies(t *testing.T) {
	g := GrantContext{Pinned: true, InPin: true,
		Live: LiveState{GrantID: "ag-1", Rev: 5, Revoked: true, RevokedBy: "u", RevokedAt: "now"}}
	got, _ := ResolveWithGrant(ForgeOpenPR, false, false, g)
	if got.Verdict != VerdictDeny {
		t.Fatalf("revoked should deny: got %v", got)
	}
}

// TestExpiredDenies (AL9 rule 4): an expired covering grant denies.
func TestExpiredDenies(t *testing.T) {
	g := GrantContext{Pinned: true, InPin: true,
		Live: LiveState{GrantID: "ag-1", Rev: 5, Expired: true}}
	got, _ := ResolveWithGrant(ForgeOpenPR, false, false, g)
	if got.Verdict != VerdictDeny {
		t.Fatalf("expired should deny: got %v", got)
	}
}

// TestConditionUnmetDenies and TestConditionCannotSayDenies (AL9 rule 4,
// AL4): an unmet or cannot-say action-time condition denies. cannot-say is
// never met.
func TestConditionUnmetDenies(t *testing.T) {
	g := GrantContext{Pinned: true, InPin: true,
		Live: LiveState{GrantID: "ag-1", Rev: 5,
			Conditions: []ConditionStatus{{Name: "requireBudget", State: CondUnmet, Sentence: "budget exhausted"}}}}
	got, _ := ResolveWithGrant(ForgeOpenPR, false, false, g)
	if got.Verdict != VerdictDeny {
		t.Fatalf("unmet condition should deny: got %v", got)
	}
}

func TestConditionCannotSayDenies(t *testing.T) {
	g := GrantContext{Pinned: true, InPin: true,
		Live: LiveState{GrantID: "ag-1", Rev: 5,
			Conditions: []ConditionStatus{{Name: "requireGates", State: CondCannotSay, Sentence: "verification not wired"}}}}
	got, _ := ResolveWithGrant(ForgeMerge, false, false, g)
	if got.Verdict != VerdictDeny {
		t.Fatalf("cannot-say condition should deny: got %v", got)
	}
}

// TestUnattendedIrreversibleRunsOnlyThroughRule5: forge.merge runs unattended
// only with a pin + covered + live + conditions; every other case denies.
func TestUnattendedIrreversibleRunsOnlyThroughRule5(t *testing.T) {
	cases := []struct {
		name string
		g    GrantContext
	}{
		{"no pin", GrantContext{}},
		{"not in pin", GrantContext{Pinned: true}},
		{"vetoed", GrantContext{Pinned: true, InPin: true, Live: LiveState{Vetoed: true, VetoID: "ag-v"}}},
		{"revoked", GrantContext{Pinned: true, InPin: true, Live: LiveState{GrantID: "ag-1", Rev: 1, Revoked: true, RevokedBy: "u"}}},
		{"expired", GrantContext{Pinned: true, InPin: true, Live: LiveState{GrantID: "ag-1", Rev: 1, Expired: true}}},
		{"condition unmet", GrantContext{Pinned: true, InPin: true, Live: LiveState{GrantID: "ag-1", Rev: 1,
			Conditions: []ConditionStatus{{Name: "requireGates", State: CondUnmet}}}}},
		{"condition cannot-say", GrantContext{Pinned: true, InPin: true, Live: LiveState{GrantID: "ag-1", Rev: 1,
			Conditions: []ConditionStatus{{Name: "requireGates", State: CondCannotSay}}}}},
	}
	for _, c := range cases {
		got, _ := ResolveWithGrant(ForgeMerge, false, false, c.g)
		if got.Verdict != VerdictDeny {
			t.Errorf("%s: merge should deny, got %v", c.name, got)
		}
	}
	// And the one case that runs.
	ok := GrantContext{Pinned: true, InPin: true,
		Live: LiveState{GrantID: "ag-1", Rev: 1, Conditions: []ConditionStatus{{Name: "requireGates", State: CondMet}}}}
	got, _ := ResolveWithGrant(ForgeMerge, false, false, ok)
	if got.Verdict != VerdictRun {
		t.Fatalf("merge with pin+covered+live+conditions should run: got %v", got)
	}
}

// TestOutwardClassDoesNotCoverMerge (AL2): a class "outward" grant never
// covers the irreversible action; the merge is granted only by its own name.
// The decision site reflects this by the expand in the plan/pin (tested in
// the autonomy package); here the point is that a veto on the outward class
// does not veto the merge, and the merge is not implied by outward.
func TestOutwardClassDoesNotCoverMerge(t *testing.T) {
	// A grant on the outward class that covers openPR but not merge: the
	// merge, pinned but not in the snapshot, returns base (deny unattended).
	g := GrantContext{Pinned: true, InPin: false} // merge not in the snapshot
	got, _ := ResolveWithGrant(ForgeMerge, false, true, g)
	if got.Verdict != VerdictDeny {
		t.Fatalf("merge not in an outward-class snapshot must deny: got %v", got)
	}
}

// TestAttendedAndLocalReturnBase (AL9 rule 1): attended asks and local runs
// are untouched by the grant — a veto does not touch either.
func TestAttendedAndLocalReturnBase(t *testing.T) {
	// attended + veto: returns the ask (base), not a deny.
	g := GrantContext{Pinned: true, InPin: true, Live: LiveState{Vetoed: true, VetoID: "ag-v"}}
	got, _ := ResolveWithGrant(ForgeOpenPR, true, false, g)
	if got.Verdict != VerdictAsk {
		t.Errorf("attended must return the ask, got %v", got)
	}
	// local + veto: returns run (base).
	got, _ = ResolveWithGrant(GitCreateBranch, false, false, g)
	if got.Verdict != VerdictRun {
		t.Errorf("local must run even with a veto, got %v", got)
	}
}
