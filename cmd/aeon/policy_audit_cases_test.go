package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/runtime"
)

// --- the carried obligations, each with a test ------------------------------------

// DEF-041 root cause: the ad-hoc actor the gate asks with MUST be a subject
// the audit boundary can record — no colon.
func TestAdHocActorMintIsAuditRecordable(t *testing.T) {
	if strings.Contains(runtime.DefaultHandlerName, ":") {
		t.Errorf("runtime.DefaultHandlerName %q carries a colon — the identifier policy refuses it as a subject id and every ad-hoc refusal goes audit-blind (DEF-041/SEC-043)", runtime.DefaultHandlerName)
	}
	if runtime.DefaultHandlerName != "step-ad-hoc" {
		t.Errorf("the ad-hoc mint drifted to %q — DEF-041 mints %q", runtime.DefaultHandlerName, "step-ad-hoc")
	}
	// The boundary itself is the judge (no second validator here): asking
	// ReportEvents with that subject must not be refused.
	spy := &spyRepo{}
	boundary := policy.NewStore(spy)
	if _, _, rej := boundary.ReportEvents(context.Background(), "agent:"+runtime.DefaultHandlerName, 1); rej != nil {
		t.Errorf("the boundary refuses its own minted actor as an audit subject: %s", rej.Reason)
	}
}

// DEF-041 surfacing: the events verb never renders a blind zero as "nothing
// was refused", and says the THIRD state — unaudited — when the ad-hoc actor
// cannot file rows at all.
func TestEventsNeverRendersABlindZeroAsNothingRefused(t *testing.T) {
	t.Run("empty table says why it is not clean", func(t *testing.T) {
		spy := &spyRepo{}
		withSpy(t, spy)
		out := mustPolicy(t, "events")
		if !strings.Contains(out, "NOT \"nothing was refused\"") {
			t.Errorf("an empty events log printed without the honesty line (it read as \"nothing was refused\"):\n%s", out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) < 2 {
			t.Errorf("the empty listing was just the summary line:\n%s", out)
		}
	})

	// F-14: zero rows UNDER a filter is a filter answer, not a claim about the
	// log. After T22 the footer's depth agrees with the filter (it reads 0 here),
	// which is precisely why the sentence is still needed: "depth 0" alone is the
	// truth about the wrong question. The two honesty lines must never be
	// reachable from each other's condition.
	t.Run("filtered-empty says it is a filter answer", func(t *testing.T) {
		spy := &spyRepo{events: []policy.EventRow{
			{ID: 1, SubjectKind: "agent", SubjectID: "bob", Outcome: "refusal",
				Detail: auditRefusal("bob"),
				At:     "2026-09-01T00:00:00Z"},
		}}
		withSpy(t, spy)
		out := mustPolicy(t, "events", "--subject", "agent:nobody")
		if !strings.Contains(out, "depth 0 · cap 90 days · oldest (none)") {
			t.Errorf("the footer does not report the LISTING's depth (T22) for the filtered-empty run:\n%s", out)
		}
		if want := eventsFilteredEmptyHonesty("agent:nobody"); !strings.Contains(out, want) {
			t.Errorf("a filtered-empty listing printed no filter-honesty line (want %q):\n%s", want, out)
		}
		if strings.Contains(out, `NOT "nothing was refused"`) {
			t.Errorf("the filtered-empty run printed the true-empty sentence — the two claims collapsed into one:\n%s", out)
		}
		if strings.Contains(out, "agent:bob") {
			t.Errorf("the filter leaked another subject's row into the filtered-empty listing:\n%s", out)
		}
		if strings.Contains(out, "unaudited") {
			t.Errorf("the recordable mint was reported unaudited on the filtered path:\n%s", out)
		}

		// The line order is part of the contract: rows → footer → honesty line →
		// unaudited line. Drive the fourth line into the SAME run with an
		// unrecordable actor so the order is pinned in one pass, and pin
		// rows-before-footer with a non-empty run on the same spy.
		boundary := policy.NewStore(spy)
		var fb strings.Builder
		if err := runPolicyEvents(&fb, boundary, "agent:nobody", 20, time.Now(), "agent:step:ad-hoc"); err != nil {
			t.Fatal(err)
		}
		joined := strings.TrimSpace(fb.String())
		iFooter, iHon, iUn := strings.Index(joined, "depth 0 ·"), strings.Index(joined, "honesty  no policy_event rows matched"), strings.Index(joined, "unaudited ")
		if !(iFooter >= 0 && iFooter < iHon && iHon < iUn) {
			t.Errorf("the lines did not print in the pinned order (rows → footer → honesty → unaudited):\n%s", joined)
		}
		if n := len(strings.Split(joined, "\n")); n != 3 {
			t.Errorf("the filtered-empty run printed %d lines, want footer + honesty + unaudited and no rows:\n%s", n, joined)
		}
		var nb strings.Builder
		if err := runPolicyEvents(&nb, boundary, "", 20, time.Now(), "agent:step:ad-hoc"); err != nil {
			t.Fatal(err)
		}
		nonEmpty := strings.TrimSpace(nb.String())
		if iRow, iFoot := strings.Index(nonEmpty, "agent:bob"), strings.Index(nonEmpty, "depth 1 ·"); !(iRow >= 0 && iRow < iFoot) {
			t.Errorf("rows did not print above the footer:\n%s", nonEmpty)
		}
		if strings.Contains(nonEmpty, "honesty") {
			t.Errorf("a non-empty listing printed an honesty line (neither zero is present):\n%s", nonEmpty)
		}
	})

	// The complementary half: an unfiltered, genuinely empty store keeps
	// eventsEmptyHonesty verbatim (QAPL-24 (1) unchanged) and never grows the
	// filter sentence — the filter sentence needs a filter.
	t.Run("unfiltered-empty prints the log's sentence, never the filter's", func(t *testing.T) {
		spy := &spyRepo{}
		withSpy(t, spy)
		out := mustPolicy(t, "events")
		if !strings.Contains(out, eventsEmptyHonesty) {
			t.Errorf("the genuinely empty log lost its own sentence:\n%s", out)
		}
		if strings.Contains(out, "no policy_event rows matched") {
			t.Errorf("the filter sentence printed with no filter asked — the third state collapsed the wrong way:\n%s", out)
		}
	})

	t.Run("unrecordable actor prints unaudited", func(t *testing.T) {
		spy := &spyRepo{}
		boundary := policy.NewStore(spy)
		var b strings.Builder
		if err := runPolicyEvents(&b, boundary, "", 20, time.Now(), "agent:step:ad-hoc"); err != nil {
			t.Fatal(err)
		}
		out := b.String()
		if !strings.Contains(out, "unaudited") || !strings.Contains(out, "NO policy_event row") {
			t.Errorf("a blind audit path printed no unaudited line:\n%s", out)
		}
		if !strings.Contains(out, `"agent:step:ad-hoc"`) {
			t.Errorf("the unaudited line does not name the actor (quoted):\n%s", out)
		}
	})

	t.Run("recordable actor prints no unaudited line", func(t *testing.T) {
		spy := &spyRepo{}
		boundary := policy.NewStore(spy)
		var b strings.Builder
		if err := runPolicyEvents(&b, boundary, "", 20, time.Now(), "agent:"+runtime.DefaultHandlerName); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "unaudited") {
			t.Errorf("the recordable mint was reported unaudited (the line must be conditional, not a banner):\n%s", b.String())
		}
	})

	// The production wiring uses the runtime mint — a plant-proof link.
	src, err := os.ReadFile("policy.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "runtime.DefaultHandlerName") {
		t.Error("cmdPolicyEvents does not probe the runtime's actual ad-hoc mint")
	}
}

// DEF-030/SEC-034: the events verb is the operator-reachable trigger for the
// 90-day cap — TrimEvents on demand, and the cap PRINTED from the const.
func TestEventsWiresTrimAndRendersTheCapFromTheConst(t *testing.T) {
	spy := &spyRepo{events: []policy.EventRow{
		{ID: 1, PolicyID: "no-bash", SubjectKind: "agent", SubjectID: "bob",
			Outcome: "refusal", Detail: `refused: agent:bob · tool_access "no-bash" · target agent:bob · source operator · no expiry`,
			At: "2026-07-01T00:00:00Z"},
		{ID: 2, SubjectKind: "agent", SubjectID: "bob", Outcome: "refusal",
			Detail: auditRefusal("bob"), At: "2026-09-01T00:00:00Z"},
	}}
	withSpy(t, spy)

	out := mustPolicy(t, "events", "--depth", "1")
	if spy.calls.eventTrim != 1 {
		t.Errorf("events ran TrimEvents %d times — the demand-side trigger of the 90-day cap must fire once per run (DEF-030)", spy.calls.eventTrim)
	}
	wantCutoff := time.Now().Add(-policy.PolicyEventAgeCap)
	if d := spy.calls.lastTrim.Sub(wantCutoff); d > time.Minute || d < -time.Minute {
		t.Errorf("the cutoff was measured from the wrong instant: %v (expected ≈ %v)", spy.calls.lastTrim, wantCutoff)
	}
	capText := fmt.Sprintf("%d days", int64(policy.PolicyEventAgeCap/(24*time.Hour)))
	if !strings.Contains(out, "cap "+capText) {
		t.Errorf("the summary line does not render the cap from policy.PolicyEventAgeCap (got %q, const says %q):\n%s", "cap …", capText, out)
	}
	if !strings.Contains(out, "depth 2 ·") {
		t.Errorf("the summary line does not report the whole-table depth:\n%s", out)
	}
	if !strings.Contains(out, "oldest 2026-07-01T00:00:00Z") {
		t.Errorf("the summary line does not name the oldest retained row:\n%s", out)
	}
	if strings.Count(out, "\n2026-") != 1 && strings.Count(out, "refusal") < 1 {
		t.Errorf("--depth 1 did not bound the listing:\n%s", out)
	}

	// A trim failure never hides the report — and never hides ITSELF.
	spy2 := &spyRepo{trimErr: fmt.Errorf("disk said no")}
	boundary := policy.NewStore(spy2)
	var b strings.Builder
	if err := runPolicyEvents(&b, boundary, "", 20, time.Now(), "agent:"+runtime.DefaultHandlerName); err != nil {
		t.Errorf("a failed trim took the report down with it: %v", err)
	}
	if !strings.Contains(b.String(), "trim    FAILED") || !strings.Contains(b.String(), "not enforced") {
		t.Errorf("a skipped trim was not said out loud:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "depth 0") {
		t.Errorf("the report vanished behind the trim failure:\n%s", b.String())
	}
}

// F-13 at the CLI: the footer of a FILTERED listing is that subject's own
// answer. `--subject agent:nobody` prints `depth 0` (the honesty SENTENCE that
// goes with it is T23's — this asserts the number, not new prose), the filter
// travels from the flag through the boundary to the seam, and the unfiltered
// run still prints the whole log's depth (which
// TestEventsWiresTrimAndRendersTheCapFromTheConst guards, unedited).
func TestEventsFilteredListingReportsItsOwnDepth(t *testing.T) {
	spy := &spyRepo{events: []policy.EventRow{
		{ID: 1, PolicyID: "no-bash", SubjectKind: "agent", SubjectID: "bob",
			Outcome: "refusal", Detail: `refused: agent:bob · tool_access "no-bash" · target agent:bob · source operator · no expiry`,
			At: "2026-09-01T00:00:00Z"},
		{ID: 2, SubjectKind: "agent", SubjectID: "bob", Outcome: "refusal",
			Detail: auditRefusal("bob"), At: "2026-09-02T00:00:00Z"},
		{ID: 3, SubjectKind: "agent", SubjectID: "ann", Outcome: "refusal",
			Detail: auditRefusal("ann"), At: "2026-08-01T00:00:00Z"},
	}}
	withSpy(t, spy)

	// bob's listing: two rows, bob's depth, bob's own oldest. ann's row is
	// neither counted nor named.
	out := mustPolicy(t, "events", "--subject", "agent:bob")
	if !strings.Contains(out, "depth 2 ·") {
		t.Errorf("bob's footer is not bob's depth (want `depth 2 ·`):\n%s", out)
	}
	if !strings.Contains(out, "oldest 2026-09-01T00:00:00Z") {
		t.Errorf("bob's footer does not name bob's oldest row:\n%s", out)
	}
	if strings.Contains(out, "agent:ann") || strings.Contains(out, "2026-08-01") {
		t.Errorf("ann's row rode bob's listing or its footer:\n%s", out)
	}
	// The flag reached the seam: the FIRST depth read is the listing's (the
	// later ones belong to the unaudited-actor probe).
	if len(spy.calls.statsSubjects) == 0 || spy.calls.statsSubjects[0] != "agent:bob" {
		t.Errorf("the depth read was asked for %v; the listing asked for agent:bob (F-13)",
			spy.calls.statsSubjects)
	}

	// ann's listing is ann's: one row, her instant, nothing of bob's.
	out = mustPolicy(t, "events", "--subject", "agent:ann")
	if !strings.Contains(out, "depth 1 ·") || !strings.Contains(out, "oldest 2026-08-01T00:00:00Z") {
		t.Errorf("ann's footer is not ann's answer:\n%s", out)
	}

	// The filtered-empty case — the F-14 lie: a subject nobody refused used to
	// print the whole table's depth beside an empty listing. It now states its
	// own emptiness, which is also what lets the honesty line fire here.
	out = mustPolicy(t, "events", "--subject", "agent:nobody")
	if !strings.Contains(out, "depth 0 ·") {
		t.Errorf("a never-refused subject did not print its own depth 0 (before F-13 it printed the "+
			"table's 3):\n%s", out)
	}
	if strings.Contains(out, "depth 3") {
		t.Errorf("the filtered-empty listing borrowed the whole table's depth:\n%s", out)
	}

	// Unfiltered: the D15 number, the whole log — three rows.
	out = mustPolicy(t, "events")
	if !strings.Contains(out, "depth 3 ·") {
		t.Errorf("the unfiltered footer lost the whole-log depth:\n%s", out)
	}
}

func auditRefusal(agent string) string {
	return policy.Evaluate(nil, policy.Request{Type: policy.ToolAccess, Agent: agent}, time.Time{}).Sentence()
}
