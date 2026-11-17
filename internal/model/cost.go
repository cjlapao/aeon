package model

import (
	"fmt"
	"sort"
	"strings"
)

// # Cost, in three states (M10)
//
// **A two-state cost is a cost that lies.** Providers do not always report
// usage on a streaming request, and the alternative to a third state is
// reporting `$0.00` for a call that certainly cost something — which reaches
// the UI at S9 looking like a cheap step, and reaches a rollup as a number that
// drags an average down.
//
// # The reasons, enumerated
//
// Every `unknown` carries one of these. A reason set that looks incomplete
// invites somebody to add a fourth state.
const (
	// ReasonNoPriceForRoute — the route has no prices configured. The call
	// happened and cost something; we cannot say what.
	ReasonNoPriceForRoute = "no-price-for-route"
	// ReasonProviderOmittedUsage — the provider reported no token counts.
	ReasonProviderOmittedUsage = "provider-omitted-usage"
	// ReasonStreamAbortedEarly is NOT produced here.
	//
	// It is `client.go`'s to set, when a stream was cut before any usage
	// arrived — because only the caller of the stream knows that a cancellation
	// or a transport failure happened, and this function only sees the Usage
	// that resulted. Named here so the reason set does not look incomplete to
	// the next reader of this file.
	ReasonStreamAbortedEarly = "stream-aborted-early"
	// ReasonNotAttempted — the call never reached the wire. (S22 T3, for
	// embedding refusals: an empty batch or an adapter that speaks no
	// embeddings is refused BEFORE the provider, and `no-price-for-route`
	// would blame the route's pricing for a refusal the route never got to
	// test. A reason set exists to make every unknown speakable; a call
	// that was never made is a distinct unknown, and borrowing another
	// word for it would send a reader checking prices for a refusal that
	// is about providers.)
	ReasonNotAttempted = "call-not-attempted"
)

const tokensPerMillion = 1_000_000.0

// pricedFieldNames is the set of Prices fields ComputeCost actually prices.
//
// Exported to the package's tests so the coverage claim above can be enforced:
// a price field added to `Prices` and not to ComputeCost's `lines` would make
// a priced route report `unknown`, silently.
func pricedFieldNames() []string {
	return []string{
		"InputUSDPerMTok",
		"OutputUSDPerMTok",
		"CacheReadUSDPerMTok",
		"CacheWriteUSDPerMTok",
	}
}

// ComputeCost prices a Usage against a route's Prices.
//
// # Order matters, and this is it
//
//  1. No prices at all → `unknown`, `no-price-for-route`. Checked FIRST: with
//     no prices, what the provider reported is irrelevant.
//  2. No usage at all → `unknown`, `provider-omitted-usage`.
//  3. Otherwise, per priced field: all present → `known`; some → `partial`
//     with a floor and a `Missing` list; none → `unknown`.
//
// # Nothing is rounded
//
// The full float is returned. Presentation rounding belongs to the CLI and the
// UI, and rounding here makes rollups drift as they accumulate — a hundred
// steps each rounded to the cent is a figure that is wrong by design.
func ComputeCost(u Usage, p *Prices) Cost {
	// A cost the provider reported itself is the exact figure for the call: it needs no price on the route, and
	// beats one computed from token counts and a price somebody typed.
	if u.ReportedCostUSD != nil && *u.ReportedCostUSD >= 0 {
		return Cost{State: CostKnown, USD: *u.ReportedCostUSD}
	}
	if !p.Any() {
		return Cost{State: CostUnknown, Reason: ReasonNoPriceForRoute}
	}
	if u.Empty() {
		return Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage}
	}

	// Each priced field, paired with the count that prices it. A field with no
	// price is not considered at all — see `Missing` below.
	type line struct {
		name   string
		tokens *int
		price  *float64
	}
	lines := []line{
		{"inputTokens", u.InputTokens, p.InputUSDPerMTok},
		{"outputTokens", u.OutputTokens, p.OutputUSDPerMTok},
		{"cacheReadTokens", u.CacheReadTokens, p.CacheReadUSDPerMTok},
		{"cacheWriteTokens", u.CacheWriteTokens, p.CacheWriteUSDPerMTok},
	}

	var usd float64
	var missing []string
	present := 0

	for _, l := range lines {
		if l.price == nil {
			// Unpriced. An absent unpriced field is NOT missing information —
			// it is information nobody asked for, and listing it would send a
			// reader looking for a provider problem that does not exist.
			continue
		}
		if l.tokens == nil {
			missing = append(missing, l.name)
			continue
		}
		present++
		usd += (float64(*l.tokens) / tokensPerMillion) * *l.price
	}

	// `priced == 0` is UNREACHABLE and deliberately not handled.
	//
	// `Prices.Any()` returned true, so at least one field is priced, and every
	// field of Prices appears in `lines` above. A branch for it would be dead
	// code, and dead code defended by a comment is worse than none.
	//
	// What keeps it unreachable is a test, not this comment:
	// `TestEveryPriceFieldIsPricedByComputeCost` asserts that `lines` covers
	// every field of `Prices`. **Add a price field without adding a line and
	// that test fails** — which is the failure mode this would otherwise hide,
	// since the cost would silently become `unknown` for a priced route.
	switch {
	case present == 0:
		// Usage was non-empty, but nothing it reported is priced. The counts we
		// have buy us nothing, so this is not a floor — it is unknown.
		return Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage}

	case len(missing) == 0:
		return Cost{State: CostKnown, USD: usd}

	default:
		// A floor, not an estimate. `USDAtLeast` is what the reported counts
		// cost; the real figure is higher by whatever the missing fields add.
		sort.Strings(missing) // stable output, so a test and a UI agree
		return Cost{State: CostPartial, USDAtLeast: usd, Missing: missing}
	}
}

// Add combines two costs, and **an aggregate over uncertain values is itself
// uncertain** (P32).
//
// The rules, each of which is the only honest answer:
//
//   - known + known → known
//   - anything + unknown → unknown, because a total that silently omits one
//     call's cost is a wrong total presented as a right one
//   - known + partial, or partial + partial → partial, with the floors summed
//
// Without this, a rollup of a hundred steps where one reported no usage would
// present as `known` and be too low by exactly that step.
func (c Cost) Add(other Cost) Cost {
	switch {
	case c.State == CostUnknown || other.State == CostUnknown:
		reason := c.Reason
		if reason == "" {
			reason = other.Reason
		}
		// The floor that IS known is kept, so "unknown, at least $1.40" can be
		// rendered rather than discarding what was measured.
		return Cost{
			State:      CostUnknown,
			Reason:     reason,
			USDAtLeast: c.floor() + other.floor(),
		}

	case c.State == CostPartial || other.State == CostPartial:
		missing := append(append([]string{}, c.Missing...), other.Missing...)
		sort.Strings(missing)
		return Cost{
			State:      CostPartial,
			USDAtLeast: c.floor() + other.floor(),
			Missing:    dedupe(missing),
		}

	default:
		return Cost{State: CostKnown, USD: c.USD + other.USD}
	}
}

// InterruptedCost keeps measured prior usage as a floor while acknowledging
// that an interrupted provider call may have been billed without usage data.
func InterruptedCost(prior Cost) Cost {
	return prior.Add(Cost{State: CostUnknown, Reason: ReasonStreamAbortedEarly})
}

// floor is what a cost is known to be at least.
func (c Cost) floor() float64 {
	switch c.State {
	case CostKnown:
		return c.USD
	case CostPartial:
		return c.USDAtLeast
	default:
		return c.USDAtLeast // zero unless a prior Add accumulated one
	}
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

// --- the ledger: attribution by route (S18 T7, M26, D5, O4) -----------------
//
// T1's provenance made this possible: a transcript that spans providers can
// now say which turn cost what. A single total over a mixed loop was the S3
// shape, and it is the confident number one level above an uncertain one —
// the same error S9 caught at step level, arriving at the rollup.

// CostRow is one step's cost carrying the provenance the ledger groups by.
// The store builds these (it owns the rows); AttributeCosts computes on them;
// the CLI and the app render the result. One computation, two renderers —
// never a second sum in the UI (D16, P34).
type CostRow struct {
	StepID int64 `json:"stepId"`
	// RouteID, Provider and Model are the provenance of the step's LAST
	// assistant turn — the turn that answered and whose usage was billed.
	// Empty means never recorded: a pre-provenance row, or a step that never
	// reached a model. Those group together as the unattributed line, which
	// is absent spoken aloud, not a row that guesses (P31).
	RouteID  string `json:"routeId,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Cost     Cost   `json:"cost"`
}

// AttributionGroup is one line of the ledger, grouped by ROUTE — not merely
// by provider: two routes can share a provider and a model and carry
// different prices, and the ledger is the one place that difference matters
// (O4).
type AttributionGroup struct {
	RouteID  string `json:"routeId,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Steps    int    `json:"steps"`
	Cost     Cost   `json:"cost"`
}

// Attribution is the ledger: the per-route lines plus the total, both
// computed here so a renderer cannot recompute them differently (D16).
type Attribution struct {
	Groups []AttributionGroup `json:"groups"`
	// Total follows Add's rules over every ROW — a total over an uncertain
	// part is itself uncertain (P32), and Add keeps the measured floor, which
	// is what renders as "at least $X" (D5).
	Total Cost `json:"total"`
}

// AttributeCosts groups rows by (route, provider, model) and folds the total.
// Group order is first-seen, so the ledger reads in the order the loop ran;
// the unattributed line lands last — the unknown belongs at the bottom of a
// bill, not in the middle of it.
func AttributeCosts(rows []CostRow) Attribution {
	byKey := map[string]*AttributionGroup{}
	var order []string
	var unattributed *AttributionGroup
	total := Cost{State: CostKnown}
	any := false

	for _, r := range rows {
		any = true
		total = total.Add(r.Cost)

		key := r.RouteID + "\x00" + r.Provider + "\x00" + r.Model
		g, ok := byKey[key]
		if !ok {
			g = &AttributionGroup{RouteID: r.RouteID, Provider: r.Provider, Model: r.Model}
			byKey[key] = g
			if r.RouteID == "" && r.Provider == "" && r.Model == "" {
				unattributed = g // appended after the order, below
			} else {
				order = append(order, key)
			}
		}
		g.Steps++
		if g.Steps == 1 {
			g.Cost = r.Cost
		} else {
			g.Cost = g.Cost.Add(r.Cost)
		}
	}

	out := make([]AttributionGroup, 0, len(order)+1)
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	if unattributed != nil {
		out = append(out, *unattributed)
	}
	if !any {
		// No steps is not a $0 loop: it is nobody having said anything yet
		// (M9). The reason names that rather than pricing the silence.
		total = Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage}
	}
	return Attribution{Groups: out, Total: total}
}

// MoneyString renders a figure without ever printing the claim this slice
// forbids. `$0.00` is the look of "free" to a human skimming a bill, and
// neither a zero-cost provider nor a five-noughts rounding of a real charge
// is entitled to that look (D9).
func MoneyString(usd float64) string {
	switch {
	case usd == 0:
		// Reachable only when usage was REPORTED and priced to zero — a
		// free-tier model. Spelled out rather than shown as `$0.00`, which
		// is how a real zero and a coerced one would become indistinguishable.
		return "zero cost — usage was reported and priced to nothing"
	case usd < 0.01:
		// Four or five decimal places of a small number start "$0.00x" —
		// digits that read as free to the eye that is skimming. Scientific
		// notation keeps the number honest and the look of zero impossible.
		return fmt.Sprintf("$%.2e", usd)
	default:
		return fmt.Sprintf("$%.5f", usd)
	}
}

// unmeteredSentence names the route in the reason, which is M30's whole
// sentence from the spec: `unknown — route 'local' is unmetered (no price
// declared)`. A reason that does not name what is unknown is a shrug with a
// prefix.
func unmeteredSentence(routeID string) string {
	if routeID == "" {
		return "unknown — the route was never recorded for these steps"
	}
	return fmt.Sprintf("unknown — route '%s' is unmetered (no price declared)", routeID)
}

// unknownReasonSentence turns a reason CODE into the sentence a person reads.
// The codes are the enumerated set in this file; anything else is quoted
// rather than smoothed over, because an unrecognised code is a bug and
// deserves to look like one.
func unknownReasonSentence(reason, routeID string) string {
	switch reason {
	case ReasonNoPriceForRoute:
		return unmeteredSentence(routeID)
	case ReasonProviderOmittedUsage:
		return "unknown — the provider reported no usage"
	case ReasonStreamAbortedEarly:
		return "unknown — the stream ended before any usage arrived"
	case "":
		return "unknown — no reason was recorded"
	default:
		return fmt.Sprintf("unknown — unrecognised reason %q", reason)
	}
}

// Sentence renders one ledger line's figure in all its states — the shape
// from the spec's table: `$0.0412   known`, `unknown — route 'local' is
// unmetered (no price declared)`, `at least $0.0592`. The CLI table and the
// app's footprint panel both print THIS, which is the one-computation rule
// applied to pixels and glyphs (D16, I10).
func (g AttributionGroup) Sentence() string {
	return costSentence(g.Cost, g.RouteID)
}

// TotalSentence is the total line, with the floor reading the total is
// allowed to have (D5).
func (a Attribution) TotalSentence() string {
	if len(a.Groups) == 0 {
		return "nothing to attribute yet"
	}
	return costSentence(a.Total, "")
}

func costSentence(c Cost, routeID string) string {
	switch c.State {
	case CostKnown:
		return fmt.Sprintf("%s  known", MoneyString(c.USD))
	case CostPartial:
		missing := strings.Join(c.Missing, ", ")
		if missing == "" {
			missing = "unpriced fields unrecorded"
		}
		return fmt.Sprintf("at least %s  partial — unpriced: %s", MoneyString(c.USDAtLeast), missing)
	default: // unknown
		if c.USDAtLeast > 0 {
			// P32: the floor survives into `unknown`, and a floor nobody
			// renders is a floor nobody sees.
			return fmt.Sprintf("at least %s — %s", MoneyString(c.USDAtLeast), unknownReasonSentence(c.Reason, routeID))
		}
		return unknownReasonSentence(c.Reason, routeID)
	}
}
