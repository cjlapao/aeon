package policy

// ════════════════════════════════════════════════════════════════════════════
// The one list filter (S45 T2, PM5).
//
// # One predicate, both readers
//
// The daemon's `policy.list` and the CLI's offline fallback filter the SAME
// way because they run the SAME code: a filter that reads differently on the
// socket than from a terminal is two filters, and the parity table exists to
// make that gap visible before it is real (CG9's argument, carried onto the
// policy reads). The CLI (T8) calls this; it owns no copy.
//
// # Applied AFTER the fail-closed read (SEC-015)
//
// Filter never touches the store and is never a substitute for it: callers
// hand it what `List`/`ListDetailed` already re-decoded row by row through
// DecodePolicy. A filter that read rows itself would be a second read path —
// the path that drops rows quietly, and dropping a row can drop a Deny.
//
// # Pure (D5)
//
// The clock arrives as a parameter. `IncludeExpired: false` drops exactly
// what the engine's ONE expiry rule (`expired`, evaluate.go) says has
// stopped existing at `at` — no second boundary anywhere: a row listed as
// live by List and unexpired by Evaluate is one answer; a row that flips
// between the two surfaces is the D6/D7 drift this reuses the predicate to
// prevent.
//
// # The empty after a filter is the FILTER's answer
//
// A filter that matches nothing answers through the wire's own statement
// (the two empties never read the same, D4's discipline extended); which
// sentence the surface prints is a rendering question living with the wire
// shape, and `ListFilter.Empty()` is the only question this file answers
// about it: *was a filter even asked?*
// ════════════════════════════════════════════════════════════════════════════

import "time"

// ListFilter is the whole filter, as `policy.list`'s params spell it. The
// value that filters NOTHING (what the wire's zero request means) is every
// criterion field unset AND IncludeExpired true — which is why the wire
// defaults it explicitly to true (D6/D8: an audit view does not hide
// expiry; the UI may default its own toggle and send false deliberately).
type ListFilter struct {
	// Type keeps only rows of this policy type (empty = any).
	Type PolicyType
	// Target keeps only rows whose target equals this one — kind AND id
	// (nil = any). A target is one party; partial targets are not a thing
	// the engine matches, so they are not a thing the filter offers.
	Target *Target
	// Source keeps only rows authored by this source (empty = any).
	Source Source
	// Action keeps only rows with this action (empty = any).
	Action Action
	// IncludeExpired keeps rows the one expiry rule says have stopped
	// existing at the instant Filter was handed. False IS a criterion — it
	// removes rows — which is why Empty counts it as one.
	IncludeExpired bool
	// Origin keeps one PROVENANCE (S45 T12, PM15/PM16): 'system' for rows
	// the seed process wrote, 'user' for operator-authored rows, 'import'
	// reserved; "" (absent) means any. It rides the ONE filter because a
	// person asking "show me what the machine seeded" asks it of the one
	// predicate, not of a second read path — but provenance is not policy
	// content, so it is matched by MatchOrigin on the LISTED row (where the
	// column rides) and never inside Match's closed-vocabulary half, which
	// stays exactly as T2 wrote it (PM11: policy fields answer policy
	// questions; the evaluator consults neither).
	Origin string
}

// Empty reports whether the filter asks for no filtering at all — the
// question the surface asks to decide WHICH empty it is looking at (an
// unfiltered empty corpus is D4's statement; a filtered-empty listing is
// the filter's own answer). An expiry-hiding filter is NOT empty: it
// removed rows, and a corpus whose every row it hides is badly served by
// "nothing is granted until something grants it" — the truth there is
// "what you filtered for is not in front of you".
func (f ListFilter) Empty() bool {
	return f.Type == "" && f.Target == nil && f.Source == "" && f.Action == "" && f.IncludeExpired && f.Origin == ""
}

// Match is the one predicate: dimensions are equality on closed-set values
// (a filter naming a value outside the closed set never matches a stored
// row, because no stored row can carry one — DecodePolicy refuses the whole
// read before any filter runs), the target is whole-target equality, and
// expiry is the engine's own rule applied at the caller's instant.
func (f ListFilter) Match(p Policy, at time.Time) bool {
	if f.Type != "" && p.Type != f.Type {
		return false
	}
	if f.Source != "" && p.Source != f.Source {
		return false
	}
	if f.Action != "" && p.Action != f.Action {
		return false
	}
	if f.Target != nil && (p.Target.Kind != f.Target.Kind || p.Target.ID != f.Target.ID) {
		return false
	}
	if !f.IncludeExpired && expired(p, at) {
		return false
	}
	return true
}

// Filter keeps the rows the predicate matches, in the order the read
// returned them — a filter that reordered would make the list's order a
// property of the filter rather than of the store. Pure: no store, no
// clock, no mutation of the input.
func Filter(ps []Policy, f ListFilter, at time.Time) []Policy {
	out := make([]Policy, 0, len(ps))
	for _, p := range ps {
		if f.Match(p, at) {
			out = append(out, p)
		}
	}
	return out
}

// MatchOrigin is the filter's provenance half (S45 T12): equality on the
// row's origin, any when the criterion is absent. It is a method of the
// same ListFilter and joins the same loop — one filter, one answer — kept
// beside the LISTED row rather than inside Match because Match judges the
// Policy and the Policy, by PM15's ruling, carries no provenance.
func (f ListFilter) MatchOrigin(origin string) bool {
	return f.Origin == "" || origin == f.Origin
}

// FilterListed is Filter over the detail read (S45 T12): the same predicate
// per row plus the provenance half, applied in the same order the read
// returned. It exists so the read doors run ONE filter over the rows they
// already re-decoded — a caller of ListDetailed that filtered origin by
// hand would be the second filter this file exists to prevent.
func FilterListed(rows []Listed, f ListFilter, at time.Time) []Listed {
	out := make([]Listed, 0, len(rows))
	for _, l := range rows {
		if f.Match(l.Policy, at) && f.MatchOrigin(l.Origin) {
			out = append(out, l)
		}
	}
	return out
}
