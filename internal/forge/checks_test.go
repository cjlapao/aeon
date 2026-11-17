package forge

// S57 T2 — checks, by commit SHA (VG8). Every answer here is a committed
// fixture behind the loopback hub; the suite dials no live host (I7).
//
// The two mapping tables are the whole surface GitHub adds: a check run
// (status × conclusion) and a legacy commit status (state), each to the
// closed four. The rule they serve: an API string nobody compiled is
// `unknown`, never `success` — optimism at this boundary turns a
// misbehaving API into a green gate right before something irreversible.

import (
	"context"
	"strings"
	"testing"
)

// checkrunState is the fixture's expectation for one check run: name →
// the closed State the VG8 table must produce.
func TestReadChecksMapsCheckRunsAndStatuses(t *testing.T) {
	h := newHub(t)
	h.st["/commits/aaa/check-runs"] = "checkruns_map.json"
	// The combined state is DELIBERATELY "success" in the fixture while
	// the statuses array carries a failure (T0 item 15's doc quirk in
	// reverse): reading the combined state instead of the array would
	// answer success here, which is the regression this pins.
	h.st["/commits/aaa/status"] = "status_map.json"
	g := h.client()

	rep, err := g.ReadChecks(context.Background(), "aaa")
	if err != nil {
		t.Fatalf("the read failed: %v", err)
	}
	want := map[string]string{
		// check runs — completed + good conclusion
		"build": CheckSuccess, "lint-neutral": CheckSuccess, "gen-skipped": CheckSuccess,
		// check runs — completed + bad conclusion
		"test-fail": CheckFailure, "cancelled-run": CheckFailure,
		"timed-out-run": CheckFailure, "manual-run": CheckFailure, "stale-run": CheckFailure,
		// check runs — completed without a (known) conclusion
		"no-conclusion": CheckUnknown,
		// check runs — every non-completed status the API names
		"queued-run": CheckPending, "running-run": CheckPending,
		"waiting-run": CheckPending, "requested-run": CheckPending, "api-pending-run": CheckPending,
		// check runs — a status string the API does not name
		"unknown-status-run": CheckUnknown,
		// legacy commit statuses — over the ARRAY, never the combined state
		"ci/unit": CheckSuccess, "ci/integ": CheckFailure, "ci/error-ctx": CheckFailure,
		"ci/wait": CheckPending, "ci/odd": CheckUnknown,
	}
	if len(rep.Checks) != len(want) {
		t.Fatalf("the report carries %d checks, the table names %d: %v", len(rep.Checks), len(want), rep.Checks)
	}
	for _, c := range rep.Checks {
		w, ok := want[c.Name]
		if !ok {
			t.Fatalf("unexpected check %q in the report", c.Name)
		}
		if c.State != w {
			t.Fatalf("check %q maps to %q, the VG8 table says %q", c.Name, c.State, w)
		}
		delete(want, c.Name)
	}
	if !rep.Complete {
		t.Fatal("a single-page report is complete")
	}
	// The aggregate: the failure wins, named. The combined "success"
	// must not have leaked in.
	if rep.State != CheckFailure || rep.Reason != "check test-fail is "+CheckFailure {
		t.Fatalf("the aggregate is %q / %q — a failure beside everything else is a failure, and the combined state is not the array", rep.State, rep.Reason)
	}
}

func TestReadChecksPaginates(t *testing.T) {
	h := newHub(t)
	// Two recorded pages; the first's Link header names the second.
	h.seq["/commits/aaa/check-runs"] = []string{"checkruns_page_next.json", "checkruns_page_last.json"}
	h.st["/commits/aaa/status"] = "status_none.json"
	g := h.client()

	rep, err := g.ReadChecks(context.Background(), "aaa")
	if err != nil {
		t.Fatalf("the read failed: %v", err)
	}
	if !rep.Complete {
		t.Fatal("two pages under the cap is a complete report")
	}
	got := map[string]bool{}
	for _, c := range rep.Checks {
		got[c.Name+"="+c.State] = true
	}
	for _, want := range []string{"page-one-a=" + CheckSuccess, "page-one-b=" + CheckSuccess, "page-two-a=" + CheckSuccess} {
		if !got[want] {
			t.Fatalf("the flattened report misses %q: %v", want, rep.Checks)
		}
	}
	if rep.State != CheckSuccess {
		t.Fatalf("every reported check is success, so the aggregate is: %q", rep.State)
	}
	// The Link was FOLLOWED, not invented: the second request is the
	// next page the header named, including its query.
	if len(h.hits) != 3 {
		t.Fatalf("two check-runs pages plus the status read is three requests, the hub saw %d: %v", len(h.hits), h.hits)
	}
	if !strings.Contains(h.urls[1], "page=2") {
		t.Fatalf("the second request must be the Link header's next URL: %q", h.urls[1])
	}
}

func TestReadChecksBeyondCapIsIncomplete(t *testing.T) {
	h := newHub(t)
	// Ten "next" pages plus one that would end it: past the cap the
	// report is incomplete, and the client stops at the cap — the
	// eleventh page is a fixture nobody may consume.
	pages := make([]string, 0, 11)
	for i := 0; i < 10; i++ {
		pages = append(pages, "checkruns_page_next.json")
	}
	pages = append(pages, "checkruns_page_last.json")
	h.seq["/commits/aaa/check-runs"] = pages
	h.st["/commits/aaa/status"] = "status_none.json"
	g := h.client()

	rep, err := g.ReadChecks(context.Background(), "aaa")
	if err != nil {
		t.Fatalf("an incomplete report is a report, not an error: %v", err)
	}
	if rep.Complete {
		t.Fatal("ten pages still pointing onward is incomplete — a truncation must never read as green")
	}
	if rep.State != CheckUnknown {
		t.Fatalf("an incomplete report aggregates to unknown, got %q (reason %q)", rep.State, rep.Reason)
	}
	if h.seqUsed["/commits/aaa/check-runs"] != 10 {
		t.Fatalf("the client must stop at the 10-page cap; it consumed %d pages", h.seqUsed["/commits/aaa/check-runs"])
	}
}

func TestAggregateTruthTable(t *testing.T) {
	c := func(states ...string) []Check {
		out := make([]Check, 0, len(states))
		for i, s := range states {
			out = append(out, Check{Name: string(rune('a' + i)), State: s})
		}
		return out
	}
	cases := []struct {
		name     string
		checks   []Check
		complete bool
		state    string
		reason   string
	}{
		{"zero checks is the honest none", nil, true, CheckUnknown, "none-reported"},
		{"zero checks are none even when incomplete", []Check{}, false, CheckUnknown, "none-reported"},
		{"a failure wins over everything", c(CheckSuccess, CheckPending, CheckFailure), true, CheckFailure, "check c is " + CheckFailure},
		{"a pending wins over green and unknown", c(CheckSuccess, CheckUnknown, CheckPending), true, CheckPending, "check c is " + CheckPending},
		{"an unknown wins over green", c(CheckSuccess, CheckUnknown), true, CheckUnknown, "check b is " + CheckUnknown},
		{"incomplete wins over all green", c(CheckSuccess, CheckSuccess), false, CheckUnknown, "incomplete — the report exceeded the page cap"},
		{"all green is success", c(CheckSuccess, CheckSuccess), true, CheckSuccess, "all 2 reported checks are " + CheckSuccess},
	}
	for _, tc := range cases {
		state, reason := Aggregate(tc.checks, tc.complete)
		if state != tc.state || reason != tc.reason {
			t.Fatalf("%s: Aggregate = %q / %q, the table says %q / %q", tc.name, state, reason, tc.state, tc.reason)
		}
	}
}

// TestUnknownAPIStringIsNeverSuccess — the optimistic-mapping guard
// (VG8): a string the tables do not name is unknown, full stop.
func TestUnknownAPIStringIsNeverSuccess(t *testing.T) {
	for status, conclusion := range map[string]string{
		"teleporting": "", "completed": "mystery", "": "", "PENDING": "success",
	} {
		if got := mapCheckRun(status, conclusion); got == CheckSuccess {
			t.Fatalf("mapCheckRun(%q, %q) = success — an unknown API string is never success", status, conclusion)
		}
	}
	if got := mapStatusState("frozen"); got == CheckSuccess {
		t.Fatalf("mapStatusState(%q) = success — an unknown API string is never success", "frozen")
	}
	// And the aggregate agrees: green plus unknown is unknown.
	if state, _ := Aggregate([]Check{{Name: "g", State: CheckSuccess}, {Name: "u", State: CheckUnknown}}, true); state != CheckUnknown {
		t.Fatalf("success beside unknown aggregates to %q — unknown must not be collapsed into green", state)
	}
}

func TestListStatusesGoesThroughReadPR(t *testing.T) {
	h := newHub(t)
	h.st["/pulls/7"] = "pr.json" // head.sha "aaa"
	h.st["/commits/aaa/check-runs"] = "checkruns_small.json"
	h.st["/commits/aaa/status"] = "status_none.json"
	g := h.client()

	checks, err := g.ListStatuses(context.Background(), 7)
	if err != nil {
		t.Fatalf("the read failed: %v", err)
	}
	if len(checks) != 2 || checks[0].Name != "build" || checks[1].Name != "test" {
		t.Fatalf("the flattened checks are wrong: %v", checks)
	}
	// The order is the argument: ReadPR first (the SHA lives on the PR),
	// then the checks by that SHA.
	if len(h.hits) != 3 || h.hits[0] != "GET /repos/acme/widget/pulls/7" {
		t.Fatalf("ListStatuses must go ReadPR → ReadChecks(HeadSHA) → status: %v", h.hits)
	}
	if !strings.HasPrefix(h.hits[1], "GET /repos/acme/widget/commits/aaa/check-runs") {
		t.Fatalf("the checks are read by the PR's head SHA: %v", h.hits[1])
	}

	// A PR whose head SHA is empty: the KindNotFound-shaped N/A — the
	// gate records the absence, and absence is not a red gate.
	h2 := newHub(t)
	h2.st["/pulls/7"] = "pr_opened.json" // no head.sha
	if _, err := h2.client().ListStatuses(context.Background(), 7); err == nil {
		t.Fatal("a PR without a head SHA answers not-found")
	} else if asKind(err) != KindNotFound {
		t.Fatalf("the absence is the read's not-found, not %v", err)
	}
}

func asKind(err error) string {
	var fe *ForgeError
	if asForge(err, &fe) {
		return fe.Kind
	}
	return ""
}

// TestReadChecksErrorKinds — O5 on the checks path: every kind is
// RETURNED, and none of them is folded into "no checks". A transport or
// an auth failure read as an empty report would merge on a forge whose
// network is out.
func TestReadChecksErrorKinds(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		wantKind string
	}{
		{"a SHA the forge does not know", "err_404.json", KindNotFound},
		{"an expired or wrong token", "err_401.json", KindAuth},
		{"a 403 the body cannot disambiguate", "err_403_ambiguous.json", KindRefused},
		{"the server is down", "err_500.json", KindTransport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHub(t)
			h.fail["/commits/aaa/check-runs"] = tc.fixture
			g := h.client()
			rep, err := g.ReadChecks(context.Background(), "aaa")
			if err == nil {
				t.Fatal("the error must be RETURNED — a failed read is not an empty report")
			}
			if got := asKind(err); got != tc.wantKind {
				t.Fatalf("the kind is %q, the classification table says %q (%v)", got, tc.wantKind, err)
			}
			if len(rep.Checks) != 0 {
				t.Fatalf("a failed read carries no checks: %v", rep.Checks)
			}
		})
	}
	// And the status half fails the read too — a truncated two-step
	// answer is still no answer.
	h := newHub(t)
	h.st["/commits/aaa/check-runs"] = "checkruns_small.json"
	h.fail["/commits/aaa/status"] = "err_401.json"
	if _, err := h.client().ReadChecks(context.Background(), "aaa"); asKind(err) != KindAuth {
		t.Fatalf("the status half's auth failure is the read's: %v", err)
	}
}

// TestFakeImplementsTheInterfaces — the fake stands in for GitHub on the
// checks path: same seeds shape the same answers, the aggregate is the
// ONE shared rule, and a read that fails is a read that fails (O5).
func TestFakeImplementsTheInterfaces(t *testing.T) {
	var _ CheckReader = (*Fake)(nil)
	var _ StatusLister = (*Fake)(nil)

	ctx := context.Background()
	f := NewFake()
	f.SeedPR(PR{Number: 3, Head: "task/x", BaseRef: "feature/x", HeadSHA: "abc"})
	f.SeedChecks("abc", []Check{{Name: "build", State: CheckSuccess}, {Name: "test", State: CheckFailure}}, true)

	// The PR route: the same flattened list GitHub would return.
	checks, err := f.ListStatuses(ctx, 3)
	if err != nil {
		t.Fatalf("the fake's read failed: %v", err)
	}
	if len(checks) != 2 || checks[1].State != CheckFailure {
		t.Fatalf("the fake flattens the seed: %v", checks)
	}
	// And it went ReadPR → ReadChecks, like the real implementation.
	log := f.Calls()
	if len(log) != 3 || log[0].Method != "ListStatuses" || log[1].Method != "ReadPR" || log[2].Method != "ReadChecks" {
		t.Fatalf("the call order is ListStatuses → ReadPR → ReadChecks: %v", log)
	}

	// The report route: the shared aggregate over the seed.
	rep, err := f.ReadChecks(ctx, "abc")
	if err != nil {
		t.Fatalf("the fake's report failed: %v", err)
	}
	if rep.State != CheckFailure || rep.Complete != true || rep.Source != "fake" {
		t.Fatalf("the report shape is wrong: %+v", rep)
	}
	if state, _ := Aggregate(checks, true); state != rep.State {
		t.Fatalf("the fake and the shared rule disagree: %q vs %q", rep.State, state)
	}

	// Zero checks: the honest none (VG8) — Complete=true, unknown,
	// none-reported.
	f.SeedChecks("empty", nil, true)
	rep, err = f.ReadChecks(ctx, "empty")
	if err != nil || rep.State != CheckUnknown || rep.Reason != "none-reported" || !rep.Complete || len(rep.Checks) != 0 {
		t.Fatalf("zero checks is unknown/none-reported with Complete=true: %+v (%v)", rep, err)
	}

	// A PR without a head SHA: the same absence GitHub answers.
	f.SeedPR(PR{Number: 4, Head: "task/y", BaseRef: "feature/x"})
	if _, err := f.ListStatuses(ctx, 4); asKind(err) != KindNotFound {
		t.Fatalf("a PR without a head SHA is the read's not-found: %v", err)
	}
	// An unknown SHA, asked for directly: the same absence.
	if _, err := f.ReadChecks(ctx, "nope"); asKind(err) != KindNotFound {
		t.Fatalf("an unknown SHA is the read's not-found: %v", err)
	}
	// A read that fails is a read that fails — never folded into "no
	// checks" (O5), and the injected error is the one returned.
	f2 := NewFake()
	f2.SeedPR(PR{Number: 3, Head: "task/x", BaseRef: "feature/x", HeadSHA: "abc"})
	injected := NewMsg(KindTransport, "the checks read is down (scenario)")
	f2.FailMethod("ReadChecks", injected)
	if _, err := f2.ListStatuses(ctx, 3); err == nil {
		t.Fatal("the injected failure must surface")
	} else if err != injected {
		t.Fatalf("the injected error is the one returned, got %v", err)
	}
}
