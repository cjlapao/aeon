package action

// T5's tests, written first. The two actions that create outward-facing
// records, and the two where a duplicate is most visible: two issues for
// one task, or two PRs for one branch, in a repository other people are
// reading. Both find by SEARCHING the forge — neither trusts a stored
// number (D1): the crash between creating and recording is exactly S7's
// case, and a stored number may be stale (closed and reopened by hand).

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

func forgeArgs(t *testing.T) ForgeArgs {
	t.Helper()
	names, err := NewBranchNames("4a17-version", "4a17")
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	return ForgeArgs{
		Dir:       "/repo",
		Names:     names,
		TaskTitle: "Add version endpoint",
		Base:      "feature/4a17-version",
		PRBody:    "## Summary\nwork",
	}
}

// TestEachOpenActionTwiceIsANoOp — per action, on the fake's CALL LOG
// (D1/O6/C2): the second run issues a search and NO create.
func TestEachOpenActionTwiceIsANoOp(t *testing.T) {
	ctx := context.Background()

	t.Run("openIssue", func(t *testing.T) {
		f := forge.NewFake()
		a := &ForgeActions{Forge: f}
		args := forgeArgs(t)
		first, err := a.OpenIssue(ctx, args)
		if err != nil || first.Number <= 0 {
			t.Fatalf("first: %+v (%v)", first, err)
		}
		before := len(f.Calls())
		second, err := a.OpenIssue(ctx, args)
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if second.Number != first.Number || !second.Reused {
			t.Fatalf("second must reuse the found number: %+v vs %+v", second, first)
		}
		for _, c := range f.Calls()[before:] {
			if c.Method == "OpenIssue" {
				t.Fatalf("the second run must search and NOT create; call log: %v", f.Calls())
			}
		}
		if !callSeen(f.Calls()[before:], "FindOpenIssue") {
			t.Fatalf("the second run must SEARCH (the search is the check): %v", f.Calls())
		}
	})

	t.Run("openPR", func(t *testing.T) {
		f := forge.NewFake()
		f.SeedIssue(forge.Issue{Number: 1, Title: "unrelated", Labels: []string{"dev-loop"}}) // noise only
		a := &ForgeActions{Forge: f, Git: gitWith(t, true)}
		args := forgeArgs(t)
		first, err := a.OpenPR(ctx, args)
		if err != nil || first.Number <= 0 {
			t.Fatalf("first: %+v (%v)", first, err)
		}
		before := len(f.Calls())
		second, err := a.OpenPR(ctx, args)
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if second.Number != first.Number || !second.Reused {
			t.Fatalf("second must reuse: %+v vs %+v", second, first)
		}
		for _, c := range f.Calls()[before:] {
			if c.Method == "OpenPR" {
				t.Fatalf("two PRs for one branch is the visible duplicate — second run must NOT create: %v", f.Calls())
			}
		}
		if !callSeen(f.Calls()[before:], "FindOpenPR") {
			t.Fatalf("the second run must search by head ref: %v", f.Calls())
		}
	})
}

func callSeen(calls []forge.Call, method string) bool {
	for _, c := range calls {
		if c.Method == method {
			return true
		}
	}
	return false
}

// TestIssueFoundBySearchNotByMemory — the crash case (stored number
// deleted: the action still finds it) AND the stale case (a wrong number
// in the caller's head changes nothing, because the action never takes
// one: reflection pins the signature has no number to trust).
func TestIssueFoundBySearchNotByMemory(t *testing.T) {
	ctx := context.Background()
	f := forge.NewFake()
	// The issue exists on the forge — as if opened before the crash.
	f.SeedIssue(forge.Issue{Number: 7, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}})
	a := &ForgeActions{Forge: f}
	res, err := a.OpenIssue(ctx, forgeArgs(t))
	if err != nil || res.Number != 7 || !res.Reused {
		t.Fatalf("with the stored number gone (or never there), the search must still find #7: %+v (%v)", res, err)
	}

	// The stale half: somebody closed #7 and opened #9 by hand in between.
	// What the caller REMEMBERS (#7) is irrelevant — the forge decides.
	f2 := forge.NewFake()
	f2.SeedIssue(forge.Issue{Number: 9, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}})
	a2 := &ForgeActions{Forge: f2}
	res, err = a2.OpenIssue(ctx, forgeArgs(t))
	if err != nil || res.Number != 9 || !res.Reused {
		t.Fatalf("a stale remembered number must lose to the search: %+v (%v)", res, err)
	}

	// Structural half: no signature accepts a number at all — the memory
	// cannot be passed IN even by accident.
	if n := reflectParams(&ForgeActions{}, "OpenIssue"); hasIntParam(n) {
		t.Fatalf("OpenIssue must not accept a stored number: %v", n)
	}
	if n := reflectParams(&ForgeActions{}, "OpenPR"); hasIntParam(n) {
		t.Fatalf("OpenPR must not accept a stored number: %v", n)
	}

}

// TestClosedIssueIsNotReused — `open` is part of the check. A closed
// issue means the task was DONE; reusing a record someone deliberately
// closed resurrects it.
func TestClosedIssueIsNotReused(t *testing.T) {
	f := forge.NewFake()
	f.SeedIssue(forge.Issue{Number: 4, Title: "[4a17] Add version endpoint", Labels: []string{"dev-loop"}, State: "closed"})
	a := &ForgeActions{Forge: f}
	res, err := a.OpenIssue(context.Background(), forgeArgs(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if res.Reused || res.Number == 4 {
		t.Fatalf("a closed issue must not satisfy the check — a new one opens: %+v", res)
	}
	if res.Number <= 4 {
		t.Fatalf("the new issue must be a new number: %+v", res)
	}
}

// TestPRFoundByHeadRef — the head ref is the key. A PR with the same
// title but a different head does NOT match; the head does.
func TestPRFoundByHeadRef(t *testing.T) {
	f := forge.NewFake()
	// Same title, wrong head — the decoy.
	f.SeedPR(forge.PR{Number: 11, Head: "task/other/branch", BaseRef: "feature/4a17-version"})
	// The real one, whatever its title says.
	f.SeedPR(forge.PR{Number: 12, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version"})
	a := &ForgeActions{Forge: f, Git: gitWith(t, true)}
	res, err := a.OpenPR(context.Background(), forgeArgs(t))
	if err != nil {
		t.Fatalf("open PR: %v", err)
	}
	if res.Number != 12 || !res.Reused {
		t.Fatalf("head ref is the key: want #12 reused, got %+v", res)
	}
}

// TestExistingTaskBranchIsReused — the PR action reuses the branch (never
// recreates it), and refuses to open a PR whose head the remote has never
// seen rather than fabricating one.
func TestExistingTaskBranchIsReused(t *testing.T) {
	f := forge.NewFake()
	a := &ForgeActions{Forge: f, Git: gitWith(t, true)}
	res, err := a.OpenPR(context.Background(), forgeArgs(t))
	if err != nil || res.Number <= 0 {
		t.Fatalf("with the branch present the PR opens: %+v (%v)", res, err)
	}
	// Structural: the probe interface FORGES nothing — BranchProbe can only
	// ask about a branch, so "rather than recreating" is a type guarantee,
	// not a behavioural one. The comment in the assertion is the record.

	// Branch absent on the remote: blocked naming what must run first,
	// not a PR against a phantom ref (O8's lesson at the forge boundary).
	a2 := &ForgeActions{Forge: forge.NewFake(), Git: gitWith(t, false)}
	_, err = a2.OpenPR(context.Background(), forgeArgs(t))
	if err == nil {
		t.Fatal("a head the remote has never seen must block, not open a phantom PR")
	}
	if !strings.Contains(err.Error(), "commitAndPush") {
		t.Fatalf("the block must name the action that fixes it: %v", err)
	}
}

// TestEvidenceIsRecorded (Q7) — each action returns its number, and the
// numbers reach loop state and the transcript through the sink T10 wires.
func TestEvidenceIsRecorded(t *testing.T) {
	var state, transcript []string
	f := forge.NewFake()
	a := &ForgeActions{
		Forge: f,
		Git:   gitWith(t, true),
		Record: func(e Evidence) {
			state = append(state, e.Key+"="+e.Value)
			transcript = append(transcript, e.Action+": "+e.Value)
		},
	}
	args := forgeArgs(t)
	issue, err := a.OpenIssue(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	pr, err := a.OpenPR(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number <= 0 || pr.Number <= 0 {
		t.Fatalf("evidence is the return value too: %+v %+v", issue, pr)
	}
	wantIssue := "issue_number=" + itoa(issue.Number)
	wantPR := "pr_number=" + itoa(pr.Number)
	if !contains(state, wantIssue) || !contains(state, wantPR) {
		t.Fatalf("loop state must receive both numbers: %v", state)
	}
	if !strings.Contains(strings.Join(transcript, "\n"), "forge.openIssue: "+itoa(issue.Number)) ||
		!strings.Contains(strings.Join(transcript, "\n"), "forge.openPR: "+itoa(pr.Number)) {
		t.Fatalf("the transcript must name action and number: %v", transcript)
	}
}

func TestBothAreOutwardAndAsk(t *testing.T) {
	for _, name := range []string{ForgeOpenIssue, ForgeOpenPR} {
		r, err := Check(name)
		if err != nil || r.Class != EffectOutward || r.Verdict != VerdictAsk {
			t.Fatalf("%s is outward and asks: %+v (%v)", name, r, err)
		}
		r, err = Resolve(name, false, false)
		if err != nil || r.Verdict != VerdictDeny || !strings.Contains(r.Sentence, "effect class 'outward' resolves to deny (loop is unattended)") {
			t.Fatalf("%s unattended is denied naming the class — an unattended loop cannot open a PR unless someone opted in at start: %+v (%v)", name, r, err)
		}
	}
}

// TestTransportFailureIsNotARefusal (O5) — "could not reach the forge"
// and "the forge said no" are different answers with different retries.
func TestTransportFailureIsNotARefusal(t *testing.T) {
	ctx := context.Background()
	args := forgeArgs(t)

	f := forge.NewFake()
	f.SetUnreachable()
	_, err := (&ForgeActions{Forge: f}).OpenIssue(ctx, args)
	if err == nil {
		t.Fatal("unreachable must fail")
	}
	if !strings.Contains(err.Error(), "transport") {
		t.Fatalf("unreachable must read as TRANSPORT: %v", err)
	}

	f2 := forge.NewFake()
	f2.FailMethod("OpenIssue", forge.NewMsg(forge.KindRefused, "repository archived"))
	_, err = (&ForgeActions{Forge: f2}).OpenIssue(ctx, args)
	if err == nil {
		t.Fatal("refused must fail")
	}
	if !strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "transport") {
		t.Fatalf("a refusal must not read as transport (different retry, different human sentence): %v", err)
	}
}

// --- helpers -----------------------------------------------------------------

func itoa(n int) string { return strconv.Itoa(n) }

// gitWith returns a probe whose remote either has or has not seen the
// branch. BranchProbe is a CHECK-ONLY interface — branch creation is not
// expressible through it, which is the structural half of
// "recreate never happens".
func gitWith(_ *testing.T, present bool) *branchProbe { return &branchProbe{present: present} }

type branchProbe struct{ present bool }

func (p *branchProbe) BranchExistsRemote(_ context.Context, _, _ string) (bool, error) {
	return p.present, nil
}

func reflectParams(recv any, method string) []reflect.Type {
	typ := reflect.TypeOf(recv)
	m, ok := typ.MethodByName(method)
	if !ok {
		panic("no method " + method)
	}
	var out []reflect.Type
	for i := 1; i < m.Type.NumIn(); i++ {
		out = append(out, m.Type.In(i))
	}
	return out
}

func hasIntParam(params []reflect.Type) bool {
	for _, p := range params {
		if p.Kind() == reflect.Int {
			return true
		}
	}
	return false
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
