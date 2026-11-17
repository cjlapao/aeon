package action

// S60 T6's tests — the landing half of the queue (MQ8/MQ11/MQ12/MQ13).
//
// The instrument of the hour is the Fake's CALL LOG: "refused BEFORE the
// forge is asked" is only true if MergePR is not in the log, so nearly
// every refusal test here ends by walking Calls() for an absence. The
// post-guard reads land on scripted probes (mergeProbe's shape, extended
// to record the REF it asked about) — the real-git ancestry itself is
// integration_test.go's ground, not re-proved here.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

const (
	landSHA  = "1111111111111111111111111111111111111111"
	landMove = "2222222222222222222222222222222222222222"
)

// landProbe records sha@ref for every ancestry question — the ref is half
// the assertion (F7 generalized: the base the queue asks about is the
// ITEM's, prefixed origin/ because the fetch just made it current).
type landProbe struct {
	queries []string
	answers map[string]bool // sha → ancestor? (missing key = true)
	err     error
}

func (p *landProbe) IsAncestor(_ context.Context, _, sha, ref string) (bool, error) {
	if p.err != nil {
		return false, p.err
	}
	p.queries = append(p.queries, sha+"@"+ref)
	if v, ok := p.answers[sha]; ok {
		return v, nil
	}
	return true, nil
}

// recFetch is the BaseFetcher's log: a land that did not fetch has a stale
// guard, so the fetch's arguments are assertable facts.
type recFetch struct {
	calls []string
	err   error
}

func (r *recFetch) FetchBase(_ context.Context, dir, base string) error {
	r.calls = append(r.calls, dir+"|"+base)
	return r.err
}

// landFixture is the landable shape: PR #7 from integrate/<mqItem> into
// main, approved, head at landSHA. The returned args are the happy-path
// ones — each refusal test moves exactly one fact.
func landFixture(t *testing.T) (*forge.Fake, *landProbe, *recFetch, *ForgeActions, *[]Evidence) {
	t.Helper()
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 7, Head: "integrate/" + mqItem, BaseRef: "main", HeadSHA: landSHA})
	approveMerge(t, f, 7) // the gate is a property — walk past it the honest way
	p := &landProbe{}
	fr := &recFetch{}
	var ev []Evidence
	a := &ForgeActions{Forge: f, Merge: p, Fetch: fr,
		Record: func(e Evidence) { ev = append(ev, e) }}
	return f, p, fr, a, &ev
}

func landArgs() LandArgs {
	return LandArgs{
		Dir: "/wt", PRNumber: 7, Item: mqItem,
		IntegrationBranch: "integrate/" + mqItem, BaseBranch: "main",
		VerifiedSHA: landSHA, IntegrationSHA: landSHA,
		ComponentRoot: "/repo", Position: 2, Loop: "alpha",
	}
}

func countMergeCalls(f *forge.Fake) int {
	n := 0
	for _, c := range f.Calls() {
		if c.Method == "MergePR" {
			n++
		}
	}
	return n
}

// TestLandHappyPath walks MQ12 end to end: gate green, rule absent
// (RECORDED as none-configured, not assumed), the three SHAs agree,
// MergePR with the default squash, then fetch + ancestry against
// origin/main with the MERGE sha (never the originals).
func TestLandHappyPath(t *testing.T) {
	ctx := context.Background()
	f, p, fr, a, ev := landFixture(t)
	res, err := a.Land(ctx, landArgs())
	if err != nil {
		t.Fatalf("land: %v", err)
	}
	if res.SHA == "" || res.PR != 7 || res.AlreadyMerged {
		t.Fatalf("a fresh land reports its merge sha: %+v", res)
	}
	if res.Fields["merge_rule"] != "none-configured" {
		t.Fatalf("an unwired merge rule must be RECORDED, not assumed: %v", res.Fields)
	}
	if res.Fields["verified_sha"] != landSHA {
		t.Fatalf("the binding rides the evidence: %v", res.Fields)
	}
	if n := countMergeCalls(f); n != 1 {
		t.Fatalf("MergePR exactly once: %d", n)
	}
	// The method: unset ⇒ squash (OD4), and the configured one when set.
	if got := f.Calls()[len(f.Calls())-1]; got.Method != "MergePR" || len(got.Args) != 2 || got.Args[1] != any(string(forge.MergeSquash)) {
		t.Fatalf("the default land method is squash: %v", got)
	}
	if len(fr.calls) != 1 || fr.calls[0] != "/wt|main" {
		t.Fatalf("the guard fetches the item's base once: %v", fr.calls)
	}
	if len(p.queries) != 1 || p.queries[0] != res.SHA+"@origin/main" {
		t.Fatalf("ancestry asked about %v, want %s@origin/main", p.queries, res.SHA)
	}
	found := false
	for _, e := range *ev {
		if e.Action == ForgeLand && e.Key == "merge_sha" && e.Value == res.SHA {
			found = true
		}
	}
	if !found {
		t.Fatalf("merge_sha recorded as evidence: %v", *ev)
	}
}

// TestLandConfiguredMethod — the repository's land_method arrives as
// args.Method (the driver read the setting); Land honours it verbatim.
func TestLandConfiguredMethod(t *testing.T) {
	_, _, _, a, _ := landFixture(t)
	args := landArgs()
	args.Method = forge.MergeRebase
	if _, err := a.Land(context.Background(), args); err != nil {
		t.Fatalf("land: %v", err)
	}
}

// TestLandRefusalsMakeNoForgeCall is the spy-at-zero battery: every
// refusal leaves MergePR out of the call log, because "before the forge"
// is only a fact the log can prove.
func TestLandRefusalsMakeNoForgeCall(t *testing.T) {
	cases := []struct {
		name    string
		args    func(a LandArgs) LandArgs
		gateRed bool // run against a fixture WITHOUT the approval review
		wantIn  string
	}{
		{"wrong base", func(a LandArgs) LandArgs { a.BaseBranch = "release"; return a }, false, "based on"},
		{"head not the item", func(a LandArgs) LandArgs { a.IntegrationBranch = "integrate/other-mq-item"; return a }, false, "not the item's integration branch"},
		{"head-moved (verified behind)", func(a LandArgs) LandArgs { a.VerifiedSHA = landMove; return a }, false, "head-moved"},
		{"head-moved (integration behind)", func(a LandArgs) LandArgs { a.IntegrationSHA = landMove; return a }, false, "head-moved"},
		{"never verified", func(a LandArgs) LandArgs { a.VerifiedSHA = ""; return a }, false, "no verified_sha"},
		{"gate red (no approval)", nil, true, "review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f *forge.Fake
			var err error
			if tc.gateRed {
				f = forge.NewFake()
				f.SeedPR(forge.PR{Number: 7, Head: "integrate/" + mqItem, BaseRef: "main", HeadSHA: landSHA})
				g := &ForgeActions{Forge: f, Merge: &landProbe{}, Fetch: &recFetch{}}
				_, err = g.Land(context.Background(), landArgs())
			} else {
				var a *ForgeActions
				f, _, _, a, _ = landFixture(t)
				_, err = a.Land(context.Background(), tc.args(landArgs()))
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("want refusal naming %q, got %v", tc.wantIn, err)
			}
			if n := countMergeCalls(f); n != 0 {
				t.Fatalf("refusal must precede the forge: %v", f.Calls())
			}
		})
	}
}

// TestLandUnwiredInstrumentsRefuse — an irreversible action whose post-guard
// cannot run does not run, and it says so BEFORE any forge read.
func TestLandUnwiredInstrumentsRefuse(t *testing.T) {
	f := forge.NewFake()
	a := &ForgeActions{Forge: f}
	_, err := a.Land(context.Background(), landArgs())
	if err == nil || !strings.Contains(err.Error(), "post-merge guard") {
		t.Fatalf("want the instruments refusal, got %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("the check precedes even the reads: %v", f.Calls())
	}
}

// TestLandAlreadyMergedNeverTwice — MQ12: an already-merged PR answers
// success with the recorded SHA. The re-run is the friendliest crash
// shape and must never merge again.
func TestLandAlreadyMergedNeverTwice(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 7, Head: "integrate/" + mqItem, BaseRef: "main", HeadSHA: landSHA, Merged: true, MergeSHA: "cafe0000"})
	a := &ForgeActions{Forge: f, Merge: &landProbe{}, Fetch: &recFetch{}}
	res, err := a.Land(context.Background(), landArgs())
	if err != nil {
		t.Fatalf("already-merged is success, never a failure: %v", err)
	}
	if !res.AlreadyMerged || res.SHA != "cafe0000" {
		t.Fatalf("the recorded sha comes back: %+v", res)
	}
	if n := countMergeCalls(f); n != 0 {
		t.Fatalf("never merge twice: %v", f.Calls())
	}
}

// TestLandPhantomMerge — merged per the API, absent from the base: the F7
// error class with the ITEM's base named in it. A failed fetch is the
// same honest failure (a stale guard is no guard).
func TestLandPhantomMerge(t *testing.T) {
	// The fake computes the merge sha itself, so the test learns it from a
	// first, successful land and then re-runs the shape refusing THAT sha.
	_, _, _, a, _ := landFixture(t)
	res, err := a.Land(context.Background(), landArgs())
	if err != nil {
		t.Fatalf("first land: %v", err)
	}
	f2, p2, _, a2, _ := landFixture(t)
	p2.answers = map[string]bool{res.SHA: false}
	_, err = a2.Land(context.Background(), landArgs())
	var phantom *PhantomMergeError
	if !errors.As(err, &phantom) {
		t.Fatalf("want PhantomMergeError, got %v", err)
	}
	if phantom.Base != "origin/main" || phantom.PR != 7 {
		t.Fatalf("the phantom names the item's base and PR: %+v", phantom)
	}
	if n := countMergeCalls(f2); n != 1 {
		t.Fatalf("the merge happened once and was reported honestly: %d", n)
	}

	// A failed fetch cannot say whether it landed: refused as unverifiable,
	// the merge itself having happened once (never re-tried blind).
	f3, _, fr3, a3, _ := landFixture(t)
	fr3.err = errors.New("network down")
	_, err = a3.Land(context.Background(), landArgs())
	if err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("a failed fetch is an honest failure, not a pass: %v", err)
	}
	if n := countMergeCalls(f3); n != 1 {
		t.Fatalf("never re-merge on a guard failure: %d", n)
	}
}

// TestLandNeverRetriesWithAnotherMethod — N4: a protection refusal is the
// answer, not a hint. Exactly one MergePR call, the forge's sentence
// surfaces, no second attempt with a different method.
func TestLandNeverRetriesWithAnotherMethod(t *testing.T) {
	f, _, _, a, _ := landFixture(t)
	f.FailMethod("MergePR", forge.NewMsg(forge.KindRefused, "branch is protected"))
	_, err := a.Land(context.Background(), landArgs())
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("the forge's refusal surfaces verbatim: %v", err)
	}
	if n := countMergeCalls(f); n != 1 {
		t.Fatalf("exactly one merge attempt — no method roulette: %v", f.Calls())
	}
}

// TestLandReusesMergeGateVerbatim — F8: land's gate is merge's gate, so
// the SAME ungreen fact produces the SAME sentence through both doors.
// Byte-for-byte; a paraphrase would be a second truth.
func TestLandReusesMergeGateVerbatim(t *testing.T) {
	ctx := context.Background()
	// Both doors, same unapproved PR shape (different head/base spellings
	// are WHAT differs; the missing-review refusal is what must match).
	fm := forge.NewFake()
	names, _ := NewBranchNames("4a17-version", "4a17")
	fm.SeedPR(forge.PR{Number: 8, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version", HeadSHA: landSHA})
	m := &ForgeActions{Forge: fm, Merge: &landProbe{}, Fetch: &recFetch{}}
	_, merr := m.MergePR(ctx, MergeArgs{Dir: "/repo", Names: names, PRNumber: 8})

	fl := forge.NewFake()
	fl.SeedPR(forge.PR{Number: 8, Head: "integrate/" + mqItem, BaseRef: "main", HeadSHA: landSHA})
	l := &ForgeActions{Forge: fl, Merge: &landProbe{}, Fetch: &recFetch{}}
	_, lerr := l.Land(ctx, LandArgs{
		Dir: "/wt", PRNumber: 8, Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		BaseBranch: "main", VerifiedSHA: landSHA, IntegrationSHA: landSHA, ComponentRoot: "/repo", Loop: "alpha",
	})
	if merr == nil || lerr == nil {
		t.Fatalf("both doors refuse an unapproved merge: %v / %v", merr, lerr)
	}
	if merr.Error() != lerr.Error() {
		t.Fatalf("the gate speaks ONE sentence:\n merge: %s\n  land: %s", merr, lerr)
	}
}

// --- the merge-rule seam (MQ12.2) -------------------------------------------

type stubRule struct {
	v   RuleVerdict
	err error
	got *MergeFacts
}

func (s *stubRule) Allow(_ context.Context, m MergeFacts) (RuleVerdict, error) {
	if s.got != nil {
		*s.got = m
	}
	return s.v, s.err
}

func TestMergeRuleSeamFailsClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("allow", func(t *testing.T) {
		var facts MergeFacts
		_, _, _, a, _ := landFixture(t)
		a.Rule = &stubRule{v: RuleVerdict{Allow: true}, got: &facts}
		res, err := a.Land(ctx, landArgs())
		if err != nil {
			t.Fatalf("land under an allowing rule: %v", err)
		}
		if res.Fields["merge_rule"] != "allowed" {
			t.Fatalf("the seam's answer is evidence: %v", res.Fields)
		}
		if facts.VerifiedSHA != landSHA || facts.Base != "main" || facts.Item != mqItem || facts.Gate.PR != 7 {
			t.Fatalf("the seam is asked about the queue's facts: %+v", facts)
		}
	})

	t.Run("deny names the rule", func(t *testing.T) {
		f, _, _, a, _ := landFixture(t)
		a.Rule = &stubRule{v: RuleVerdict{Reason: "dod: the DoD for alpha is unmet"}}
		_, err := a.Land(ctx, landArgs())
		if err == nil || !strings.Contains(err.Error(), "dod: the DoD for alpha is unmet") {
			t.Fatalf("a denial must name its rule: %v", err)
		}
		if n := countMergeCalls(f); n != 0 {
			t.Fatalf("denied before the forge: %v", f.Calls())
		}
	})

	t.Run("error refuses", func(t *testing.T) {
		f, _, _, a, _ := landFixture(t)
		a.Rule = &stubRule{err: errors.New("rule store unreadable")}
		_, err := a.Land(ctx, landArgs())
		if err == nil || !strings.Contains(err.Error(), "could not be consulted") || !strings.Contains(err.Error(), "rule store unreadable") {
			t.Fatalf("an unreadable rule is unverifiable, not green: %v", err)
		}
		if n := countMergeCalls(f); n != 0 {
			t.Fatalf("unverifiable before the forge: %v", f.Calls())
		}
	})
}

// --- the grant seam (MQ12.3, S56) --------------------------------------------

type stubGrants struct {
	allowed bool
	rev     string
	asked   []string
}

func (g *stubGrants) MergeAllowed(root string) (bool, string) {
	g.asked = append(g.asked, root)
	return g.allowed, g.rev
}

func TestLandConsultsTheGrantSeam(t *testing.T) {
	ctx := context.Background()
	f, _, _, a, _ := landFixture(t)
	g := &stubGrants{}
	a.Grants = g
	_, err := a.Land(ctx, landArgs())
	if err == nil || !strings.Contains(err.Error(), "no standing merge grant covers /repo") {
		t.Fatalf("a denied grant refuses, naming the repository: %v", err)
	}
	if len(g.asked) != 1 || g.asked[0] != "/repo" || countMergeCalls(f) != 0 {
		t.Fatalf("asked once, before anything: %v %v", g.asked, f.Calls())
	}
	// An allowing grant passes the door and lands (the engine's own grant
	// bookkeeping is loop's; this is the seam's half).
	_, _, _, a2, ev2 := landFixture(t)
	a2.Grants = &stubGrants{allowed: true, rev: "7"}
	if _, err := a2.Land(ctx, landArgs()); err != nil {
		t.Fatalf("land under a grant: %v", err)
	}
	found := false
	for _, e := range *ev2 {
		if e.Key == "merge_grant_rev" && e.Value == "7" {
			found = true
		}
	}
	if !found {
		t.Fatalf("WHICH grant let it through rides the evidence: %v", *ev2)
	}
}

// TestAllowOutwardNeverGrantsLand / TestUnattendedLandDeniedWithoutGrant —
// the class table itself (the engine gate is Resolve; the seam above is
// the effect-time door). forge.land denied unattended, VERBATIM like
// merge; the allowOutward sentence says the flag did not grant it.
func TestUnattendedLandDeniedWithoutGrant(t *testing.T) {
	r, err := Resolve(ForgeLand, false, false)
	if err != nil || r.Verdict != VerdictDeny {
		t.Fatalf("unattended forge.land denies: %+v %v", r, err)
	}
	if r.Sentence != fmt.Sprintf("%s denied — effect class 'irreversible' resolves to deny (loop is unattended)", ForgeLand) {
		t.Fatalf("the sentence is the G14 shape, verbatim like merge's: %q", r.Sentence)
	}
}

func TestAllowOutwardNeverGrantsLand(t *testing.T) {
	r, err := Resolve(ForgeLand, false, true)
	if err != nil || r.Verdict != VerdictDeny {
		t.Fatalf("allowOutward must not grant forge.land: %+v %v", r, err)
	}
	if !strings.Contains(r.Sentence, "allowOutward does not grant it") {
		t.Fatalf("the sentence names the flag that did not lift it: %q", r.Sentence)
	}
	// And attended, it asks — the ask being MQ12.6's, built by BuildAsk.
	ar, err := Resolve(ForgeLand, true, false)
	if err != nil || ar.Verdict != VerdictAsk {
		t.Fatalf("attended forge.land asks: %+v %v", ar, err)
	}
}

// --- forge.openLandingPR ------------------------------------------------------

func TestOpenLandingPRReuseHeadMissingAndOpen(t *testing.T) {
	ctx := context.Background()
	args := LandingArgs{Dir: "/repo", Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		HeadBranch: "feature/alpha", BaseBranch: "main", Position: 3}

	t.Run("reuse", func(t *testing.T) {
		f := forge.NewFake()
		open := f.SeedPR(forge.PR{Number: 11, Head: "integrate/" + mqItem, BaseRef: "main"})
		a := &ForgeActions{Forge: f, Git: &branchProbe{present: true}}
		res, err := a.OpenLandingPR(ctx, args)
		if err != nil || !res.Reused || res.Number != open.Number {
			t.Fatalf("the open PR keyed on the head ref IS the answer: %+v %v", res, err)
		}
		for _, c := range f.Calls() {
			if c.Method == "OpenPR" {
				t.Fatalf("reuse opens nothing: %v", f.Calls())
			}
		}
	})

	t.Run("head missing", func(t *testing.T) {
		f := forge.NewFake()
		a := &ForgeActions{Forge: f, Git: &branchProbe{present: false}}
		_, err := a.OpenLandingPR(ctx, args)
		var b *BlockedError
		if !errors.As(err, &b) || b.Reason != "head-missing" {
			t.Fatalf("a published ref is a precondition: %v", err)
		}
		if !strings.Contains(b.Detail, "git.publishIntegration") {
			t.Fatalf("the way forward names the queue's own verb, not commitAndPush: %s", b.Detail)
		}
	})

	t.Run("open", func(t *testing.T) {
		f := forge.NewFake()
		a := &ForgeActions{Forge: f, Git: &branchProbe{present: true}}
		res, err := a.OpenLandingPR(ctx, LandingArgs{
			Dir: "/repo", Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
			HeadBranch: "feature/alpha", BaseBranch: "main", Position: 3,
			Summary: "the loop's own words",
		})
		if err != nil || res.Number == 0 || res.Reused {
			t.Fatalf("open lands a number: %+v %v", res, err)
		}
		pr, err := f.ReadPR(ctx, res.Number)
		if err != nil {
			t.Fatal(err)
		}
		body := landingBodyOf(t, f, res.Number)
		for _, want := range []string{mqItem, "integrate/" + mqItem, "feature/alpha", "main", "queue position: 3"} {
			if !strings.Contains(pr.Head+body, want) {
				t.Fatalf("the template must say %q:\n%s", want, body)
			}
		}
		if !strings.Contains(body, "prose, not a queue fact") || !strings.Contains(body, "the loop's own words") {
			t.Fatalf("the summary rides as DECLARED prose:\n%s", body)
		}
	})
}

// landingBodyOf reads the body the fake stored — the assertion reads the
// record, not the builder's memory. (The Fake keeps requests out of the
// PR struct, so it comes from the call log.)
func landingBodyOf(t *testing.T, f *forge.Fake, number int) string {
	t.Helper()
	for _, c := range f.Calls() {
		if c.Method == "OpenPR" {
			req, ok := c.Args[0].(forge.PRRequest)
			if !ok {
				t.Fatalf("OpenPR logged %T", c.Args[0])
			}
			return req.Body
		}
	}
	t.Fatalf("no OpenPR call for PR %d: %v", number, f.Calls())
	return ""
}

// raceForge hides the FIRST existence check, reproducing the race: by the
// time we look, somebody else opened it; by the time we open, it conflicts.
type raceForge struct {
	*forge.Fake
	searches int
}

func (r *raceForge) FindOpenPR(ctx context.Context, head string) (*forge.PR, error) {
	r.searches++
	if r.searches == 1 {
		return nil, nil
	}
	return r.Fake.FindOpenPR(ctx, head)
}

func TestOpenLandingPRConflictIsOneReresearch(t *testing.T) {
	rf := &raceForge{Fake: forge.NewFake()}
	rf.SeedPR(forge.PR{Number: 12, Head: "integrate/" + mqItem, BaseRef: "main"}) // invisible to search #1
	a := &ForgeActions{Forge: rf, Git: &branchProbe{present: true}}
	res, err := a.OpenLandingPR(context.Background(), LandingArgs{
		Dir: "/repo", Item: mqItem, IntegrationBranch: "integrate/" + mqItem,
		HeadBranch: "feature/alpha", BaseBranch: "main",
	})
	if err != nil || !res.Reused || res.Number != 12 {
		t.Fatalf("a conflicted open re-searches ONCE and reuses: %+v %v", res, err)
	}
	if rf.searches != 2 {
		t.Fatalf("exactly two searches (none before the conflict): %d", rf.searches)
	}
}

// TestLandHeadlessPRUnverifiable — a server that reports no head SHA makes
// the MQ11 binding uncheckable, and uncheckable is refused, not passed.
func TestLandHeadlessPRUnverifiable(t *testing.T) {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 7, Head: "integrate/" + mqItem, BaseRef: "main"}) // no HeadSHA
	approveMerge(t, f, 7)
	a := &ForgeActions{Forge: f, Merge: &landProbe{}, Fetch: &recFetch{}}
	_, err := a.Land(context.Background(), landArgs())
	if err == nil || !strings.Contains(err.Error(), "no head SHA") {
		t.Fatalf("a missing head SHA is the binding's absence: %v", err)
	}
	if n := countMergeCalls(f); n != 0 {
		t.Fatalf("refused before the forge: %v", f.Calls())
	}
}

// --- the guard on REAL git (MQ12.5's teeth) ----------------------------------

// realMergeForge merges like a forge does: the merge is a REAL commit made in
// a second clone and pushed to the shared origin — so the post-guard reads
// real ancestry, and a forge that merged into the wrong branch (or an
// implementation that trusted the API's word) is caught by real history,
// not by a scripted yes.
type realMergeForge struct {
	*forge.Fake
	t          *testing.T
	work       string // a clone of the shared origin
	mergeInto  string // the branch the "forge" actually merges into
	mergedInto string // what actually happened (for honesty assertions)
	lastSHA    string
	template   forge.PR
}

func (r *realMergeForge) MergePR(ctx context.Context, number int, method forge.MergeMethod) (*forge.MergeResult, error) {
	// The Fake marks a fresh merge merged when asked, so a real merge must
	// happen FIRST: ask the API's view, and only merge if it is unmerged.
	pr, err := r.Fake.ReadPR(ctx, number)
	if err != nil {
		return nil, err
	}
	if pr.Merged {
		return r.Fake.MergePR(ctx, number, method) // the recorded answer, verbatim
	}
	mustExec(r.t, r.work, "git", "checkout", "-B", r.mergeInto, "origin/"+r.mergeInto)
	if r.mergeInto != "main" {
		// The wrong-branch merge must not hash-coincide with a real merge
		// of the same two commits: give it content of its own.
		writeFile(r.t, r.work, "side-note.txt", "merged here instead\n")
		mustExec(r.t, r.work, "git", "add", "-A")
		mustExec(r.t, r.work, "git", "commit", "-m", "side work")
	}
	mustExec(r.t, r.work, "git", "merge", "--no-ff", "--no-edit", "origin/integrate/"+mqItem)
	mustExec(r.t, r.work, "git", "push", "origin", r.mergeInto)
	sha := strings.TrimSpace(mustExec(r.t, r.work, "git", "rev-parse", "HEAD"))
	r.mergedInto = r.mergeInto
	r.lastSHA = sha
	tpr := r.template
	tpr.Merged, tpr.MergeSHA = true, sha
	r.Fake.SeedPR(tpr) // the API's view: merged, per the recorded SHA
	return &forge.MergeResult{SHA: sha, Merged: true}, nil
}

func TestLandRealGitGuardAncestryAndPhantom(t *testing.T) {
	ctx := context.Background()
	dir, ga := mqRepo(t, false)
	ib := "integrate/" + mqItem
	ires, err := ga.Integrate(ctx, dir, mqItem, ib, "feature/alpha", "main")
	if err != nil || ires.SHA == "" {
		t.Fatalf("integrate: %v", err)
	}
	mustExec(t, dir, "git", "push", "origin", ib)

	for _, tc := range []struct {
		name        string
		into        string // where the real merge actually lands
		wantPhantom bool
	}{
		{"merged on the base — the guard says so", "main", false},
		{"merged somewhere else — real history says so", "side", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantPhantom {
				// The "forge" merges into side, a branch that forked from
				// the same base — plausible, green per the API, NOT landed.
				mustExec(t, dir, "git", "push", "origin", "refs/remotes/origin/main:refs/heads/side")
			}
			// Clone the shared bare origin itself — every branch the queue
			// pushed is there, which the person's checkout does not carry.
			originURL := strings.TrimSpace(mustExec(t, dir, "git", "remote", "get-url", "origin"))
			work := t.TempDir()
			mustExec(t, dir, "git", "clone", "--", originURL, work)
			tpl := forge.PR{Number: 7, Head: ib, BaseRef: "main", HeadSHA: ires.SHA}
			rf := &realMergeForge{Fake: forge.NewFake(), t: t, work: work, mergeInto: tc.into, template: tpl}
			rf.SeedPR(tpl)
			approveMerge(t, rf.Fake, 7)
			a := &ForgeActions{Forge: rf, Merge: gitReal(), Fetch: ga}
			args := landArgs()
			args.Dir, args.VerifiedSHA, args.IntegrationSHA = dir, ires.SHA, ires.SHA
			_, err := a.Land(ctx, args)
			if tc.wantPhantom {
				// Two honest readings, both refusals: the sha exists here
				// and is NOT in the base (the phantom), or the sha never
				// reached this clone at all (cannot verify). What may NOT
				// happen is success — a merge into the wrong branch is not
				// landed, whichever way the guard learns it.
				var phantom *PhantomMergeError
				if !errors.As(err, &phantom) {
					if err == nil || !strings.Contains(err.Error(), "cannot verify") {
						t.Fatalf("real history refuses the unlanded merge: %v", err)
					}
				} else if phantom.Base != "origin/main" || phantom.SHA == "" {
					t.Fatalf("the phantom names the real sha and the real base: %+v", phantom)
				}
				// And it really IS an ancestor of where the "forge" merged
				// it — the test's own claim, checked, not narrated. (The
				// side fetch is the test's, never the guard's: the guard
				// asks only about the base.)
				mustExec(t, dir, "git", "fetch", "origin", "side")
				if out := mustExec(t, dir, "git", "merge-base", "--is-ancestor", rf.lastSHA, "refs/remotes/origin/side"); out != "" {
					t.Fatal("the merge is not even on the side branch the forge used")
				}
			} else {
				if err != nil {
					t.Fatalf("land through real git: %v", err)
				}
				// The base really moved: fetched, current, contains the item.
				if out := mustExec(t, dir, "git", "merge-base", "--is-ancestor", ires.SHA, "origin/main"); out != "" {
					t.Fatal("after a clean land the integration tip is in origin/main")
				}
			}
		})
	}
}
