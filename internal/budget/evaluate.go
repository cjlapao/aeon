// Package budget is the pure budget engine behind S55's enforcement (T7).
//
// # What it is, and what it is not
//
// A pure comparison of "spend so far, by dimension" against "the caps", with
// no clock, store, or disk. The store owns the spend rows (budget_spend.go) and
// the audit (budget_event); the loop engine, the runtime, and the daemon each
// call [Evaluate] at their boundary (E1/E2/E3) and act on the returned Verdict.
// Keeping the decision total and side-effect-free is what lets a test drive
// every dimension × policy × known/partial/unknown cell without a provider, a
// store, or a step row (LC9, LC11).
//
// # The one law it never breaks (P31)
//
// An unknown cost is a COUNT, never a zero. A step whose price the rollup does
// not know is carried as `USDUnknown` / `TokensUnknown` (a count of how many
// steps are unpriced), and its cost is NEVER added to the floor as 0. Whether
// that count becomes a HIT or a NOTE is the budget's `unknown_policy`: `stop`
// says "I cannot prove you are under the cap, so I refuse" (a cost-unproven
// hit); `proceed` says "carry on, and say so" (a note). Collapsing the unknown
// into zero is the exact lie a cap exists to stop, so the floor is always a
// FLOOR and the unknown rides beside it, counted.
//
// # A cap is exhausted AT the cap
//
// A dimension breaches when `spendFloor >= cap`. The cap is the boundary the
// NEXT call would cross, so a spend that has reached the cap already forbids
// the next dispatch — the check runs BEFORE the call, and a spend at the cap
// blocks it. A zero cap is a real cap (nothing may be spent) and therefore
// breaches at floor 0; a missing cap (nil) is not a cap at all and is skipped.
package budget

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Budget is one cap per (scope, subject, window), the pure read-only shape the
// store's Budget maps onto. The caps are pointers so a missing dimension is a
// real absence (never the zero value, which is a cap of its own); a nil cap
// means "no cap on that dimension" and the dimension is not evaluated.
type Budget struct {
	ID      string
	Scope   string // workspace | component | epic | loop
	Subject string // 'workspace' / a component id / an epic id / a loop id
	Window  string // lifetime | day

	USD    *float64 // nil = no cap on USD
	Tokens *int64   // nil = no cap on tokens
	WallMs *int64   // nil = no cap on wall time
	Steps  *int64   // nil = no cap on steps

	UnknownPolicy string // proceed | stop
	WarnPct       *int   // nil = no warn line (else 1..99)
}

// Spend is a budget's spend so far, per dimension, as the engine consumes it:
// the store's BudgetSpend mapped to plain numbers. The floors are FLOORS (known
// plus partial summed; a partial is a lower bound) and the unknowns are COUNTS
// of unpriced work, never added to the floor (P31).
type Spend struct {
	USD           float64 // the floor: known + partial
	USDUnknown    int     // unpriced steps, a count
	Tokens        int64   // the floor: known + partial token counts
	TokensUnknown int     // unpriced steps, a count
	WallMs        int64   // the floor
	Steps         int     // the count of steps
}

// Hit is one dimension of one budget that is exhausted. Reason is closed:
// `over-cap` (the floor has reached the cap) or `cost-unproven` (the floor is
// under the cap but the unknowns make it unprovable and the policy is stop).
// [Hit.Sentence] is the one renderer of a hit; every surface prints it
// verbatim, so there is no second truth.
type Hit struct {
	BudgetID   string
	Scope      string
	SubjectID  string
	Dimension  string // usd | tokens | wall-time | steps
	Window     string
	SpendFloor float64
	Cap        float64
	Reason     string // over-cap | cost-unproven
	Unknown    int    // cost-unproven: the count of unpriced steps
	Exact      bool   // the floor was fully priced (no unknowns rode it)
}

// Sentence is the one sentence a hit reads as. It names the budget, the scope
// and subject, the dimension, and — for over-cap — the floor against the cap.
// For cost-unproven it names the unpriced count instead, because the floor is
// not the story: the story is that the cost cannot be proven under the cap.
func (h Hit) Sentence() string {
	switch h.Reason {
	case "cost-unproven":
		return fmt.Sprintf("budget %s on %s %s is cost-unproven (%s: %s of %s, %d step(s) unpriced)",
			h.BudgetID, h.Scope, h.SubjectID, h.Dimension,
			fmtNum(h.SpendFloor), fmtNum(h.Cap), h.Unknown)
	default: // over-cap
		return fmt.Sprintf("budget %s on %s %s is exhausted (%s %s of %s)",
			h.BudgetID, h.Scope, h.SubjectID, h.Dimension,
			fmtNum(h.SpendFloor), fmtNum(h.Cap))
	}
}

// Warn is one dimension of one budget that has crossed its warn line since the
// last warn for that (budget, dimension) in the current period. It is a signal
// to surface, not a refusal: the spend keeps going. Pct is the percentage the
// floor has reached of the cap.
type Warn struct {
	BudgetID   string
	Scope      string
	SubjectID  string
	Dimension  string
	Window     string
	SpendFloor float64
	Cap        float64
	Pct        int
}

// Verdict is the engine's answer: whether the spend may proceed (OK), the hits
// that forbid it (tightest first), the warns to surface, and the notes that
// name the honest caveats (unknown costs the policy chose to proceed past).
type Verdict struct {
	OK    bool
	Hits  []Hit
	Warns []Warn
	Notes []string
}

// scopeRank orders scopes narrow to wide, so the tightest applicable breach —
// the one that is most specific to the work about to be charged — is reported
// first. A loop budget is the narrowest (one loop); an epic budget covers every
// loop in the epic; a component budget covers a component's subtree; the
// workspace budget covers everything. Hits[0] is therefore the narrowest
// scope, then the lowest budget id within it, so the answer is stable (LC9).
func scopeRank(scope string) int {
	switch scope {
	case "loop":
		return 0
	case "epic":
		return 1
	case "component":
		return 2
	case "workspace":
		return 3
	default:
		return 4 // an unknown scope sorts widest, never first
	}
}

// dimensionRank is the fixed order a single budget's dimensions are reported
// in, so a budget that breaches two dimensions names the same one first every
// time. USD is the headline dimension (the one a person thinks in), then
// tokens, wall-time, steps.
func dimensionRank(dimension string) int {
	switch dimension {
	case "usd":
		return 0
	case "tokens":
		return 1
	case "wall-time":
		return 2
	case "steps":
		return 3
	default:
		return 4
	}
}

// Evaluate compares every budget's spend against its caps and returns the
// verdict. It reads no clock except the `now` handed in (the warn dedup's
// day-window needs a reference instant), touches no store, and is total: the
// same inputs always give the same verdict.
//
// The `spend` map is keyed by budget id; a budget with no entry is treated as
// having spent nothing (the caller — the store-backed seam — always supplies
// one per applicable budget, so a missing entry is a fresh or uncharged
// budget, which is a legitimate zero). The `warned` map is keyed by
// `budgetID+"|"+dimension` and carries the last instant a warn was recorded
// for that pair; it is how "once per period" is honoured without re-reading
// the audit on every call (the store's LastBudgetWarned). `now` anchors the
// day-window's rolling 24 h.
func Evaluate(budgets []Budget, spend map[string]Spend, warned map[string]time.Time, now time.Time) Verdict {
	v := Verdict{OK: true}

	// Evaluate in a stable order (scope narrow→wide, then id) so the notes and
	// the hit list are deterministic before the final sort of hits.
	ordered := make([]Budget, len(budgets))
	copy(ordered, budgets)
	sort.SliceStable(ordered, func(i, j int) bool {
		ri, rj := scopeRank(ordered[i].Scope), scopeRank(ordered[j].Scope)
		if ri != rj {
			return ri < rj
		}
		return ordered[i].ID < ordered[j].ID
	})

	for _, b := range ordered {
		sp := spend[b.ID]

		// Each dimension the budget caps is evaluated in a fixed order; a hit
		// on a dimension forbids the next call, and a warn on one (without a
		// hit) is surfaced once per period.
		evaluateDimension(&v, b, "usd", sp.USD, f64nil(b.USD), b.UnknownPolicy, sp.USDUnknown, b.WarnPct, warned, now)
		evaluateDimension(&v, b, "tokens", float64(sp.Tokens), i64nil(b.Tokens), b.UnknownPolicy, sp.TokensUnknown, b.WarnPct, warned, now)
		evaluateDimension(&v, b, "wall-time", float64(sp.WallMs), i64nil(b.WallMs), "", 0, b.WarnPct, warned, now)
		evaluateDimension(&v, b, "steps", float64(sp.Steps), i64nil(b.Steps), "", 0, b.WarnPct, warned, now)
	}

	// The hits are re-sorted by (scope narrow→wide, budget id, dimension) so
	// Hits[0] is the tightest breach and the ordering is stable across runs.
	sort.SliceStable(v.Hits, func(i, j int) bool {
		ri, rj := scopeRank(v.Hits[i].Scope), scopeRank(v.Hits[j].Scope)
		if ri != rj {
			return ri < rj
		}
		if v.Hits[i].BudgetID != v.Hits[j].BudgetID {
			return v.Hits[i].BudgetID < v.Hits[j].BudgetID
		}
		di, dj := dimensionRank(v.Hits[i].Dimension), dimensionRank(v.Hits[j].Dimension)
		if di != dj {
			return di < dj
		}
		return v.Hits[i].BudgetID < v.Hits[j].BudgetID
	})

	v.OK = len(v.Hits) == 0
	return v
}

// evaluateDimension evaluates one capped dimension of one budget: the hit
// (over-cap, or cost-unproven when the policy is stop and the cost is not
// provable), the note (proceed past an unknown), and the warn (crossed the
// warn line since the last warn in the period).
func evaluateDimension(v *Verdict, b Budget, dimension string, floor, cap float64, unknownPolicy string, unknown int, warnPct *int, warned map[string]time.Time, now time.Time) {
	// A missing cap (cap < 0 sentinel from f64nil) is not a cap: skip.
	if cap < 0 {
		return
	}

	// The hit: the floor has reached the cap. A zero cap breaches at floor 0.
	if floor >= cap {
		v.Hits = append(v.Hits, Hit{
			BudgetID: b.ID, Scope: b.Scope, SubjectID: b.Subject,
			Dimension: dimension, Window: b.Window,
			SpendFloor: floor, Cap: cap, Reason: "over-cap",
			Exact: unknown == 0,
		})
		return
	}

	// The cost-unproven hit: the floor is under the cap, but there is unpriced
	// work and the policy is stop — the spend cannot be proven under the cap,
	// so it is refused (P31). Only the priced dimensions (usd, tokens) carry an
	// unknown count; wall-time and steps are always known.
	if unknown > 0 && unknownPolicy == "stop" {
		v.Hits = append(v.Hits, Hit{
			BudgetID: b.ID, Scope: b.Scope, SubjectID: b.Subject,
			Dimension: dimension, Window: b.Window,
			SpendFloor: floor, Cap: cap, Reason: "cost-unproven", Unknown: unknown,
		})
		return
	}

	// The note: the policy is proceed and there is unpriced work — carry on,
	// and say so (the honest third value, P31). The floor is a floor, not a
	// total, and this is where a reader sees that.
	if unknown > 0 && unknownPolicy == "proceed" {
		phrase := "cost-unknown"
		if dimension == "tokens" {
			phrase = "tokens-unknown"
		}
		v.Notes = append(v.Notes, fmt.Sprintf("budget %s: %s (%d step(s) unpriced)", b.ID, phrase, unknown))
	}

	// The warn: the floor has crossed the warn line, but not the cap. Emitted
	// once per (budget, dimension) per period — the `warned` map is the dedup,
	// so a repeat call within the period does not re-warn. A zero cap is
	// excluded (it is already a hit at floor 0, and a percentage of zero is
	// undefined).
	if warnPct == nil || cap <= 0 {
		return
	}
	pct := int(floor * 100 / cap)
	if pct < *warnPct {
		return
	}
	key := b.ID + "|" + dimension
	if last, ok := warned[key]; ok {
		// lifetime: any prior warn means this period is already warned.
		if b.Window == "lifetime" {
			return
		}
		// day: only a warn within the rolling 24 h suppresses a new one.
		if !last.Before(now.Add(-24 * time.Hour)) {
			return
		}
	}
	v.Warns = append(v.Warns, Warn{
		BudgetID: b.ID, Scope: b.Scope, SubjectID: b.Subject,
		Dimension: dimension, Window: b.Window,
		SpendFloor: floor, Cap: cap, Pct: pct,
	})
}

// f64nil renders a nil cap as the -1 sentinel (no cap: skip the dimension) and
// a set cap as its value. -1 is never a legal cap (the store's CHECK is >= 0),
// so it cannot collide with a real zero cap.
func f64nil(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// i64nil is f64nil for the integer dimensions (tokens, wall-time, steps).
func i64nil(p *int64) float64 {
	if p == nil {
		return -1
	}
	return float64(*p)
}

// fmtNum renders a number for a sentence without a trailing .0: a whole number
// reads as an integer, a fractional one keeps its digits. It is the one
// formatter of a hit's numbers, so the sentence is byte-stable.
func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
