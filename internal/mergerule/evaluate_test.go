// evaluate_test.go — the rule's semantics (T5 spec tests 2, 4-10): the
// defaults-equal-baseline grid, the staleness rules, the required-check
// semantics, require_local, require_dod, the review-mode truth table and
// the threads requirement.

package mergerule

import (
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
)

// defaultRow is a row straight out of the migration's DEFAULTS — a row
// with every behavioural field at the baseline's values, wearing the
// identity the store stamps on it.
func defaultRow() Rule {
	r := Baseline()
	r.ComponentID = "comp-x"
	r.Revision = 1
	r.Source = "own"
	return r
}

// gridFacts is the fact sets the grid runs: the fourteen golden sets
// (the gate's whole input space) plus the extension-only ingredients
// (a run, a DoD, a token) the baseline path must simply not read.
func gridFacts(t *testing.T) []Facts {
	t.Helper()
	var out []Facts
	for _, name := range []string{
		"no-reviews", "latest-comment", "latest-request-changes",
		"outstanding-rc-cleared-by-same-author", "outstanding-rc-answered-by-other-author",
		"approve-no-statuslister", "approve-statuses-all-success", "approve-statuses-one-failure",
		"approve-statuses-one-pending", "approve-statuses-not-found", "approve-threads-none",
		"approve-threads-some", "approve-threads-not-found", "approve-threads-unsupported",
	} {
		f, ok := factForCase(name)
		if !ok {
			t.Fatalf("grid: no fact set %s", name)
		}
		out = append(out, f)
	}
	// The extension-only ingredients, on a pass fact set: a stale run,
	// an unmet DoD, a stale token. A default row must behave as no row
	// with or without them.
	f, _ := factForCase("approve-statuses-all-success")
	f.LatestRun = &Run{ID: "vr-x", HeadSHA: "old-sha", State: "pass"}
	f.DoD = []Item{{ID: "d1", State: "unmet", Text: "x"}}
	f.AeonReview = "ok@old-sha"
	out = append(out, f)
	return out
}

// A row with all database defaults behaves exactly as no row (VG10):
// over the whole grid, Evaluate(defaultRow, f) == Evaluate(Baseline(), f)
// — the migration's defaults ARE the baseline, and this is the test
// that keeps it that way.
func TestRuleDefaultsEqualTheBaseline(t *testing.T) {
	for i, f := range gridFacts(t) {
		base := Evaluate(Baseline(), f)
		row := Evaluate(defaultRow(), f)
		if !reflect.DeepEqual(base, row) {
			t.Errorf("grid fact %d: a default row is not the baseline\n base: %+v\n row:  %+v", i, base, row)
		}
	}
}

// A disabled row evaluates as the baseline too: its fields are in the
// record, but "off" means the baseline, and a disabled row with a
// require_local and a green run must still refuse on what the baseline
// refuses on.
func TestDisabledRowIsTheBaseline(t *testing.T) {
	row := defaultRow()
	row.RequireLocal = true
	row.Enabled = false
	f, _ := factForCase("no-reviews")
	f.LatestRun = &Run{ID: "vr-x", HeadSHA: goldenHeadSHA, State: "pass"}
	got := Evaluate(row, f)
	if got.Allowed {
		t.Fatal("a disabled row with a failing review passed — disabled is the baseline, not inert")
	}
	if got.Refusals[0].Code != CodeNoReview {
		t.Fatalf("the disabled row's refusal is %v, the baseline's is %s", got.Refusals, CodeNoReview)
	}
	// The extension evidence (rule_revision & co) must not appear: the
	// disabled path is the baseline path, five keys.
	if len(got.Evidence) != 5 {
		t.Errorf("the disabled row carried %d evidence keys, the baseline carries 5: %v", len(got.Evidence), got.Evidence)
	}
}

// --- staleness (spec test 4) -------------------------------------------------

// A gate result or an aeon token for ANOTHER SHA never counts, and the
// refusal names both SHAs.
func TestStaleEvidenceNeverCounts(t *testing.T) {
	head := "deadbeef"
	// (a) require_local: a pass run on the previous head.
	r := Baseline()
	r.ComponentID = "c"
	r.RequireLocal = true
	f, _ := factForCase("approve-statuses-all-success")
	f.HeadSHA = head
	f.LatestRun = &Run{ID: "vr-1", HeadSHA: "oldhead", State: "pass"}
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeEvidenceStale {
		t.Fatalf("a stale run was honoured: %+v", v)
	}
	if want := "the evidence is for oldhead, the PR head is deadbeef — new commits invalidate it"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the stale-run sentence names both SHAs, byte for byte:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}
	// (b) aeon mode: a token for another SHA.
	r2 := Baseline()
	r2.ComponentID = "c"
	r2.ReviewMode = ReviewModeAeon
	f2 := f
	f2.LatestRun = nil
	f2.AeonReview = "ok@oldhead"
	v2 := Evaluate(r2, f2)
	if v2.Allowed || v2.Refusals[0].Code != CodeEvidenceStale {
		t.Fatalf("a stale token was honoured: %+v", v2)
	}
	// (c) either mode: a stale token is simply NOT counted — the forge
	// half carries the verdict, no refusal, no SHA in it.
	r3 := Baseline()
	r3.ComponentID = "c"
	r3.ReviewMode = ReviewModeEither
	v3 := Evaluate(r3, f2)
	if !v3.Allowed {
		t.Fatalf("a stale token refused a merge the forge half approved: %+v", v3)
	}
	// (d) the FRESH pair counts: same-SHA run + token, both green.
	r4 := Baseline()
	r4.ComponentID = "c"
	r4.RequireLocal = true
	r4.ReviewMode = ReviewModeBoth
	f4 := f
	f4.LatestRun = &Run{ID: "vr-1", HeadSHA: head, State: "pass"}
	f4.AeonReview = "ok@" + head
	if v4 := Evaluate(r4, f4); !v4.Allowed {
		t.Fatalf("the fresh pair refused: %+v", v4)
	}
}

// wrapDetail applies the gate's wrapper to a detail (the tests compare
// the wrapped sentence, because that is what the surfaces print).
func wrapDetail(detail string) string { return wrap(detail) }

// --- the required checks (spec test 5) ---------------------------------------

func TestRequiredCheckSemantics(t *testing.T) {
	approve := func() Facts {
		f, _ := factForCase("approve-statuses-all-success")
		return f
	}
	setChecks := func(f Facts, checks ...forge.Check) Facts {
		f.Checks.Checks = checks
		return f
	}

	// (a) the named check is missing: the named sentence, under both
	// policies (it is not a policy question at all).
	r := Baseline()
	r.ComponentID = "c"
	r.RequiredChecks = []string{"test"}
	f := setChecks(approve(), forge.Check{Name: "build", State: forge.CheckSuccess})
	for _, pol := range []string{ChecksReported, ChecksRequired} {
		r.ChecksPolicy = pol
		v := Evaluate(r, f)
		if v.Allowed || v.Refusals[0].Code != CodeCheckMissing {
			t.Fatalf("policy %s: a missing named check passed or misnamed: %+v", pol, v)
		}
		if want := `required check "test" was not reported`; v.Refusals[0].Sentence != wrapDetail(want) {
			t.Errorf("policy %s: the missing sentence drifted:\n got:  %s\nwant: %s", pol, v.Refusals[0].Sentence, wrapDetail(want))
		}
	}

	// (b) the named check is pending: the pending sentence.
	f = setChecks(approve(),
		forge.Check{Name: "build", State: forge.CheckSuccess},
		forge.Check{Name: "test", State: forge.CheckPending})
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeCheckPending {
		t.Fatalf("a pending named check passed or misnamed: %+v", v)
	}
	if want := "check test is pending (still running)"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the pending sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}

	// (c) the named check is unknown: its own sentence (the forge
	// cannot say — never read as green).
	f = setChecks(approve(),
		forge.Check{Name: "build", State: forge.CheckSuccess},
		forge.Check{Name: "test", State: forge.CheckUnknown})
	v = Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeCheckUnknown {
		t.Fatalf("an unknown named check passed or misnamed: %+v", v)
	}
	if want := `required check "test" is unknown — the forge cannot say, and unknown is not green`; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the unknown sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}

	// (d) the named check is red: today's not-green sentence (it is
	// the same fact as an unrelated red, so it is the same sentence).
	f = setChecks(approve(),
		forge.Check{Name: "build", State: forge.CheckSuccess},
		forge.Check{Name: "test", State: forge.CheckFailure})
	v = Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeCheckNotGreen {
		t.Fatalf("a red named check passed or misnamed: %+v", v)
	}
	if want := "PR #3 check test is failure, not green"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the not-green sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}

	// (e) reported_green adds: an UNRELATED red is a refusal.
	f = setChecks(approve(),
		forge.Check{Name: "build", State: forge.CheckFailure},
		forge.Check{Name: "test", State: forge.CheckSuccess})
	r.ChecksPolicy = ChecksReported
	v = Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeCheckNotGreen || len(v.Refusals) != 1 {
		t.Fatalf("reported_green: an unrelated red passed or was double-named: %+v", v)
	}
	// (f) required_only ignores the unrelated red.
	r.ChecksPolicy = ChecksRequired
	v = Evaluate(r, f)
	if !v.Allowed {
		t.Fatalf("required_only: an unrelated red was refused: %+v", v)
	}

	// (g) the dedupe: a named failure under reported_green is said ONCE
	// (the named arm and the reported arm produce the same sentence).
	f = setChecks(approve(),
		forge.Check{Name: "build", State: forge.CheckSuccess},
		forge.Check{Name: "test", State: forge.CheckFailure})
	r.ChecksPolicy = ChecksReported
	v = Evaluate(r, f)
	if len(v.Refusals) != 1 {
		t.Errorf("a named failure was named %d times: %+v", len(v.Refusals), v.Refusals)
	}
}

// --- require_local (spec tests 6 and 7) ---------------------------------------

func TestRequireLocalNeedsAPassOnTheHeadSHA(t *testing.T) {
	r := Baseline()
	r.ComponentID = "c"
	r.RequireLocal = true
	f, _ := factForCase("approve-statuses-all-success")
	f.HeadSHA = "head1"

	// (a) a pass run on the head SHA: the only green.
	f.LatestRun = &Run{ID: "vr-1", HeadSHA: "head1", State: "pass"}
	if v := Evaluate(r, f); !v.Allowed {
		t.Fatalf("a pass run on the head refused: %+v", v)
	}
	if v := Evaluate(r, f); v.Evidence["verify_run_id"] != "vr-1" || v.Evidence["verify_head_sha"] != "head1" {
		t.Errorf("the run's evidence keys are not the run: %v", v.Evidence)
	}

	// (b) a FAIL run on the head: the state sentence.
	f.LatestRun = &Run{ID: "vr-1", HeadSHA: "head1", State: "fail", Reason: "1 gate failed"}
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeLocalNotGreen {
		t.Fatalf("a fail run was honoured: %+v", v)
	}
	if want := "local gate verification is not green for head head1 (state: fail)"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the state sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}

	// (c) no run at all: the state sentence with state "no run".
	f.LatestRun = nil
	v = Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeLocalNotGreen {
		t.Fatalf("no run was honoured: %+v", v)
	}
	if want := "local gate verification is not green for head head1 (state: no run)"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the no-run sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}
}

// A run that is unverified — because the effective set has no gates —
// is not green: a required set of zero gates makes the run unverified,
// and unverified is the state, named.
func TestRequireLocalNoGatesIsRefusal(t *testing.T) {
	r := Baseline()
	r.ComponentID = "c"
	r.RequireLocal = true
	f, _ := factForCase("approve-statuses-all-success")
	f.HeadSHA = "head1"
	f.LatestRun = &Run{ID: "vr-1", HeadSHA: "head1", State: "unverified", Reason: "no-gates-configured"}
	f.Gates = EffectiveGates{ComponentID: "c"}
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeLocalNotGreen {
		t.Fatalf("an unverified run was honoured: %+v", v)
	}
	if want := "local gate verification is not green for head head1 (state: unverified)"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the unverified sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}
}

// --- require_dod (spec tests 8 and 9) ------------------------------------------

func TestRequireDoDAllMet(t *testing.T) {
	r := Baseline()
	r.ComponentID = "c"
	r.RequireDoD = RequireDoDAllMet
	f, _ := factForCase("approve-statuses-all-success")

	// (a) every item met.
	f.DoD = []Item{{ID: "d1", State: "met", Text: "a"}, {ID: "d2", State: "met", Text: "b"}}
	if v := Evaluate(r, f); !v.Allowed {
		t.Fatalf("an all-met DoD refused: %+v", v)
	}
	if v := Evaluate(r, f); v.Evidence["dod"] != "2 of 2 met" {
		t.Errorf("the dod evidence is not the count: %v", v.Evidence)
	}

	// (b) one item not met: the count sentence.
	f.DoD = []Item{{ID: "d1", State: "met", Text: "a"}, {ID: "d2", State: "unmet", Text: "b"}, {ID: "d3", State: "unchecked", Text: "c"}}
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeDoDNotMet {
		t.Fatalf("a not-met DoD was honoured: %+v", v)
	}
	if want := "definition of done: 2 of 3 items not met"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the DoD sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}
}

// Zero items is its own refusal: "all of zero met" is the vacuous green
// the ledger exists to prevent (VG13).
func TestRequireDoDZeroItemsRefuses(t *testing.T) {
	r := Baseline()
	r.ComponentID = "c"
	r.RequireDoD = RequireDoDAllMet
	f, _ := factForCase("approve-statuses-all-success")
	f.DoD = nil
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeDoDNone {
		t.Fatalf("a zero-item DoD passed: %+v", v)
	}
	if want := "no acceptance criteria are recorded for this loop"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the zero-item sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}
	if v.Evidence["dod"] != "none-recorded" {
		t.Errorf("the zero-item evidence is not none-recorded: %v", v.Evidence)
	}
}

// --- the review-mode truth table (spec test 11) --------------------------------

func TestReviewModes(t *testing.T) {
	head := "head1"
	approveBy := func() Facts {
		f, _ := factForCase("approve-statuses-all-success")
		f.HeadSHA = head
		return f
	}
	// rcOut: the latest review IS an approve (by the other author), so
	// the forge half's single refusal is the outstanding request_changes.
	rcOut := func() Facts {
		f, _ := factForCase("outstanding-rc-answered-by-other-author")
		f.HeadSHA = head
		return f
	}
	// rcLatest: the latest review is a request_changes — the forge
	// half's TWO refusals (latest-not-approve AND the outstanding one).
	rcLatest := func() Facts {
		f, _ := factForCase("latest-request-changes")
		f.HeadSHA = head
		return f
	}

	// run evaluates a BASELINE-SHAPED rule in the mode: first refusal
	// wins, the gate's early-return shape (what the golden pins).
	// runExt evaluates a NON-baseline rule in the mode — required_only
	// with no named checks is the tweak that leaves the baseline's
	// verdicts untouched — and the extended path collects EVERY
	// refusal.
	run := func(mode string, f Facts) Verdict {
		r := Baseline()
		r.ComponentID = "c"
		r.ReviewMode = mode
		return Evaluate(r, f)
	}
	runExt := func(mode string, f Facts) Verdict {
		r := Baseline()
		r.ComponentID = "c"
		r.ReviewMode = mode
		r.ChecksPolicy = ChecksRequired
		return Evaluate(r, f)
	}

	ok := "ok@" + head

	cases := []struct {
		mode     string
		f        Facts
		token    string
		extended bool // runExt (collect all) instead of run (first wins)
		wantOK   bool
		wantLen  int // the refusal count to expect (0 when allowed)
	}{
		// forge: today's three arms, unchanged by the token.
		{ReviewModeForge, approveBy(), "", false, true, 0},
		{ReviewModeForge, approveBy(), ok, false, true, 0},
		{ReviewModeForge, rcOut(), "", false, false, 1},    // the outstanding request_changes
		{ReviewModeForge, rcOut(), ok, false, false, 1},    // the token is not the forge's authority
		{ReviewModeForge, rcLatest(), "", false, false, 1}, // baseline shape: first refusal wins
		{ReviewModeForge, rcLatest(), "", true, false, 2},  // the extended path names both
		// aeon: the engine half alone — the forge reviews are not read.
		{ReviewModeAeon, rcOut(), ok, false, true, 0}, // an outstanding RC is not the aeon half's problem
		{ReviewModeAeon, approveBy(), ok, false, true, 0},
		{ReviewModeAeon, approveBy(), "", false, false, 1},       // the miss sentence
		{ReviewModeAeon, approveBy(), "nope", false, false, 1},   // a malformed token is a miss
		{ReviewModeAeon, approveBy(), "ok@old", false, false, 1}, // stale: named, in the evidence-stale code
		// either: the forge half OR the engine half.
		{ReviewModeEither, approveBy(), "", false, true, 0},
		{ReviewModeEither, rcOut(), ok, false, true, 0},        // the engine half carries it
		{ReviewModeEither, rcOut(), "", false, false, 1},       // both fail: the forge half's reasons
		{ReviewModeEither, rcOut(), "ok@old", false, false, 1}, // a stale token is not counted
		{ReviewModeEither, rcLatest(), ok, false, true, 0},     // two forge refusals, one engine half
		// both: both halves, both lists.
		{ReviewModeBoth, approveBy(), ok, false, true, 0},
		{ReviewModeBoth, approveBy(), "", false, false, 1}, // the aeon half alone fails
		{ReviewModeBoth, rcOut(), ok, false, false, 1},     // the forge half alone fails
		{ReviewModeBoth, rcOut(), "", false, false, 2},     // 1 forge + 1 aeon
		{ReviewModeBoth, rcLatest(), "", false, false, 3},  // 2 forge + 1 aeon
	}
	for i, c := range cases {
		f := c.f
		f.AeonReview = c.token
		v := run(c.mode, f)
		if c.extended {
			v = runExt(c.mode, f)
		}
		if v.Allowed != c.wantOK {
			t.Errorf("case %d (%s, token %q): allowed=%v, want %v — %+v", i, c.mode, c.token, v.Allowed, c.wantOK, v)
			continue
		}
		if c.wantOK && len(v.Refusals) != 0 {
			t.Errorf("case %d: allowed with refusals: %+v", i, v.Refusals)
		}
		if !c.wantOK && len(v.Refusals) != c.wantLen {
			t.Errorf("case %d: %d refusals, want %d: %+v", i, len(v.Refusals), c.wantLen, v.Refusals)
		}
	}
}

// --- the threads requirement (spec test 12) ------------------------------------

func TestThreadsUnreportableRefusesWhenRequired(t *testing.T) {
	base := func() (Rule, Facts) {
		r := Baseline()
		r.ComponentID = "c"
		f, _ := factForCase("approve-statuses-all-success")
		return r, f
	}

	// (a) the flag is set and the forge CANNOT report: a refusal, both
	// the no-API and the refused-query forms.
	r, f := base()
	r.RequireThreadsResolved = true
	f.Threads = ThreadFacts{Available: false}
	v := Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeThreadsMissing {
		t.Fatalf("a required-but-unreportable thread was honoured (no API): %+v", v)
	}
	if want := "the forge cannot report review threads and the rule requires them resolved"; v.Refusals[0].Sentence != wrapDetail(want) {
		t.Errorf("the unreportable sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
	}
	r, f = base()
	r.RequireThreadsResolved = true
	f.Threads = ThreadFacts{Available: true, Readable: false}
	v = Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeThreadsMissing {
		t.Fatalf("a required-but-unreportable thread was honoured (refused query): %+v", v)
	}

	// (b) the flag is set and the report is 0: green.
	r, f = base()
	r.RequireThreadsResolved = true
	f.Threads = ThreadFacts{Available: true, Readable: true}
	if v := Evaluate(r, f); !v.Allowed {
		t.Fatalf("a required-and-reportable 0-thread forge refused: %+v", v)
	}

	// (c) forge mode WITHOUT the flag: absence passes (today's gate,
	// L34), unresolved refuses (today's gate, honoured).
	r, f = base()
	f.Threads = ThreadFacts{Available: false}
	if v := Evaluate(r, f); !v.Allowed {
		t.Fatalf("forge mode without the flag: an absent thread API refused — that is today's green: %+v", v)
	}
	r, f = base()
	f.Threads = ThreadFacts{Available: true, Readable: true,
		Unresolved: []forge.Thread{{Path: "src/x.go", Line: 1, Author: "mira"}}}
	v = Evaluate(r, f)
	if v.Allowed || v.Refusals[0].Code != CodeThreadsOpen {
		t.Fatalf("forge mode without the flag: unresolved threads passed — that is today's refusal: %+v", v)
	}

	// (d) a non-forge mode WITHOUT the flag does not honour threads
	// (the report is evidence, not a decision).
	r, f = base()
	r.ReviewMode = ReviewModeAeon
	f.AeonReview = "ok@" + f.HeadSHA // the token's SHA must be the PR's head
	f.Threads = ThreadFacts{Available: true, Readable: true,
		Unresolved: []forge.Thread{{Path: "src/x.go", Line: 1, Author: "mira"}}}
	if v := Evaluate(r, f); !v.Allowed {
		t.Fatalf("aeon mode without the flag honoured threads: %+v", v)
	}
	if v.Evidence["threads"] == "" {
		t.Error("the thread report is evidence even when it is not a decision")
	}
}

// --- the zero-report arms (the no_checks field) ---------------------------------

func TestNoChecksField(t *testing.T) {
	// A non-baseline rule with no named checks: the zero report is
	// decided by no_checks — allow passes (today's behaviour), refuse
	// refuses with its own sentence.
	for _, tc := range []struct {
		noChecks string
		wantOK   bool
		code     string
	}{
		{NoChecksAllow, true, ""},
		{NoChecksRefuse, false, CodeNoChecks},
	} {
		r := Baseline()
		r.ComponentID = "c"
		r.NoChecks = tc.noChecks                          // non-baseline now (baseline is allow)
		f, _ := factForCase("approve-statuses-not-found") // a claimed API, an empty report
		v := Evaluate(r, f)
		if v.Allowed != tc.wantOK {
			t.Fatalf("no_checks=%s: allowed=%v, want %v — %+v", tc.noChecks, v.Allowed, tc.wantOK, v)
			continue
		}
		if !tc.wantOK {
			if v.Refusals[0].Code != tc.code {
				t.Errorf("no_checks=refuse: code %s, want %s", v.Refusals[0].Code, tc.code)
			}
			if want := "the forge reports no checks and the rule refuses to merge without them"; v.Refusals[0].Sentence != wrapDetail(want) {
				t.Errorf("the no-checks sentence drifted:\n got:  %s\nwant: %s", v.Refusals[0].Sentence, wrapDetail(want))
			}
		}
	}

	// An empty report with NAMES: the named refusals fire, not the
	// no-checks one (the specific reason wins).
	r := Baseline()
	r.ComponentID = "c"
	r.NoChecks = NoChecksRefuse
	r.RequiredChecks = []string{"build", "test"}
	f, _ := factForCase("approve-statuses-not-found")
	v := Evaluate(r, f)
	if v.Allowed {
		t.Fatal("a zero report with named checks passed")
	}
	for _, rf := range v.Refusals {
		if rf.Code != CodeCheckMissing {
			t.Errorf("a named-missing refusal was replaced by %s: %+v", rf.Code, v.Refusals)
		}
	}
	if len(v.Refusals) != 2 {
		t.Errorf("two named checks, two missing refusals; got %d: %+v", len(v.Refusals), v.Refusals)
	}
}

// --- the extended evidence shape --------------------------------------------------

// A non-baseline verdict carries the five baseline keys AND the six the
// rule adds (VG11) — the keys are pinned by name, the values by a
// spot-check of each.
func TestExtendedEvidenceCarriesTheTenKeys(t *testing.T) {
	r := Baseline()
	r.ComponentID = "c"
	r.Revision = 4
	r.RequireLocal = true
	r.RequireDoD = RequireDoDAllMet
	r.AeonReviewAgents = []string{"code-reviewer"}
	f, _ := factForCase("approve-statuses-all-success")
	f.HeadSHA = "head1"
	f.LatestRun = &Run{ID: "vr-9", HeadSHA: "head1", State: "pass"}
	f.DoD = []Item{{ID: "d1", State: "met", Text: "a"}}
	f.AeonReview = "ok@head1"
	f.Checks.State = forge.CheckSuccess
	f.Gates = EffectiveGates{ComponentID: "c", Source: "own"}

	v := Evaluate(r, f)
	if !v.Allowed {
		t.Fatalf("the all-green extended case refused: %+v", v)
	}
	want := map[string]string{
		// the five baseline keys
		"review_state": "approve",
		"review_by":    "mira",
		"review_at":    "2026-10-26T09:12:00Z",
		"checks":       "build=success test=success",
		"threads":      "reported-resolved (0 unresolved)",
		// the six the rule adds
		"rule_revision":   "4",
		"verify_run_id":   "vr-9",
		"verify_head_sha": "head1",
		"dod":             "1 of 1 met",
		"checks_state":    "success",
		"aeon_review":     "ok@head1",
	}
	for k, w := range want {
		if v.Evidence[k] != w {
			t.Errorf("evidence[%q] = %q, want %q", k, v.Evidence[k], w)
		}
	}
	if len(v.Evidence) != 11 {
		t.Errorf("the extended evidence carries %d keys, the ten plus the baseline's five make eleven: %v", len(v.Evidence), v.Evidence)
	}
	// The honest empties: no run, no token.
	f2 := f
	f2.LatestRun = nil
	f2.AeonReview = ""
	f2.ChecksReadable = false
	v2 := Evaluate(r, f2)
	if v2.Evidence["verify_run_id"] != "none" || v2.Evidence["verify_head_sha"] != "" {
		t.Errorf("the absent-run evidence is not honest: %v", v2.Evidence)
	}
	if v2.Evidence["aeon_review"] != "absent" {
		t.Errorf("the absent token is not 'absent': %v", v2.Evidence)
	}
	if v2.Evidence["checks_state"] != "" {
		t.Errorf("the no-API checks_state is not empty: %v", v2.Evidence)
	}
}
