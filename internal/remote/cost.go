package remote

import (
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// cost.go — RA10: a remote agent's spend is unknown here unless the peer
// reports it, and an empty field rendered as $0.00 is the precise failure the
// entire cost design exists to prevent (P31). Locally, a missing cost is a
// bug worth chasing; remotely it is the NORMAL case — the peer may not
// report, may not know, may not wish to say. The field is genuinely empty,
// and this file's whole job is to keep "genuinely empty" from wearing the
// costume of a number.
//
// # This extends S14's rollup; it is not a second one (scope 5)
//
// The rollup is agentsvc/cost.go's RollupCost — a query over rows that folds
// every cost in a chain (its own gates prove the parent table: any child
// unknown → parent partial at best). NOTHING in this file walks a graph,
// sums children, or keeps a total — a grep test (TestNoSecondRollupWasAdded)
// reads this file to prove it, in the same spirit as S14's "no package-level
// variable" proof. What lives here is the SUBJECT extension the task
// demands: how a REMOTE child's cost enters that existing fold, decided once,
// at the only place that can decide it honestly — where the peer's claim
// crosses into our record.
//
// # The state a peer's claim may earn (RA10 × RA5)
//
// A reported number is the peer's claim, not our measurement — so the best
// state it can buy is PARTIAL, never known. A known parent resting on a
// stranger's number is P32's lie with a signature; partial says "at least
// this much, and somebody else did the counting" in the same breath. The
// provenance is not a comment beside the number: it rides IN the record
// (Missing names the peer; Reason quotes the peer) — "if the peer's number
// is later shown to be wrong, the record says whose number it was".
//
// # The two silences are two facts (scope 2)
//
// "The peer reported no usage" and "the task has not been answered yet" are
// different facts, and a transcript read next month cannot tell them apart
// otherwise. S14's row-level default for a not-yet-terminal task is
// "cost-not-reported" (agentsvc toTask); the LANDED-without-a-number case
// gets its own token below. Two silences, two names.

// ReasonPeerReportedNoUsage is the kebab-token for a completion that landed
// without a cost report — deliberately distinct from S14's "cost-not-reported"
// (awaiting, nothing asked yet) and mirroring the house vocabulary
// ("provider-omitted-usage": the party who should have reported didn't).
const ReasonPeerReportedNoUsage = "peer-reported-no-usage"

// CostReport is what a peer may attach to a completion: its OWN claim about
// its OWN spend. It rides the signed canonical form (CanonicalCompletion),
// so the number cannot be edited in flight — a cost claim that is not bound
// to the signature is not a claim, it is an suggestion with a decimal point
// (RA10 × RA8: verification proves WHO sent the number; anything unverified
// is noise, and noise never enters a ledger).
type CostReport struct {
	USD      float64 `json:"usd"`
	Currency string  `json:"currency,omitempty"` // "" or "usd" counts; anything else names the claim and does NOT count it
}

// CostFor is the ONE place a remote child's cost state is decided, from the
// peer's identity and its (possibly absent) report. Every refusal to believe
// a number still records WHY the number is not there — an unexplained
// absence is what M10 exists to stop, and validateCost on the other side of
// the seam refuses exactly that.
func CostFor(peer string, report *CostReport) model.Cost {
	if report == nil {
		// The headline (RA10): unknown, never zero. Both number fields stay
		// zero so they OMITE from the record entirely — an absent number and
		// a zero number are different documents, and only one of them is
		// honest here.
		return model.Cost{State: model.CostUnknown, Reason: ReasonPeerReportedNoUsage}
	}
	if report.Currency != "" && !strings.EqualFold(report.Currency, "usd") {
		// The claim is named and NOT converted: silently spending a "40"
		// labelled euros as dollars is the confident wrong answer, and
		// refusing to say which currency was claimed would hide a fact
		// somebody might act on.
		return model.Cost{State: model.CostUnknown, Reason: "peer-reported-unsupported-currency"}
	}
	if report.USD < 0 {
		return model.Cost{State: model.CostUnknown, Reason: "peer-reported-negative-cost"}
	}
	// Reported: partial, with provenance welded in. The peer's total becomes
	// OUR floor ("at least"), because a self-reported cost that we cannot
	// verify is exactly as trustworthy as it needs to be to under-report.
	return model.Cost{
		State:      model.CostPartial,
		USDAtLeast: report.USD,
		Missing:    []string{"remote-cost-unverified:" + peer},
		Reason:     "reported by remote peer " + strconv.Quote(peer) + " — their claim, not our measurement",
	}
}

// RenderCost is the remote surface's own rendering, in the house's three
// shapes (S3's vocabulary, the CLI's wording): "unknown (reason)" carries NO
// digits — the reason tokens are kebab-case words by design, so "unknown
// never renders as a number" is checkable by test, not by hope. Where a
// total keeps a floor (S3's Add preserves what WAS measured), the words
// "at least" and the state word ride together — the total still says
// unknown in words when a child is unknown.
func RenderCost(c model.Cost) string {
	switch c.State {
	case model.CostKnown:
		// Nothing produced here can be known — a peer's claim caps at
		// partial and our own absence is unknown. The branch exists so the
		// shape is total, and a test pins that no path in this file
		// constructs it.
		return model.MoneyString(c.USD) + " (known)"
	case model.CostPartial:
		return "at least " + model.MoneyString(c.USDAtLeast) + " (partial — peer-reported, unverified: " + strings.Join(c.Missing, ", ") + ")"
	case model.CostUnknown:
		return "unknown (" + c.Reason + ")"
	default:
		return `unknown (no state stored: "` + string(c.State) + `")`
	}
}
