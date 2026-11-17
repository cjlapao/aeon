// tighten.go — the tightening order over Rule (S56's propose twin;
// T8's merge.set uses it).
//
// A rule change is a TIGHTENING when every field is the same, tighter,
// or identity — never looser, and never the one incomparable pair.
// Tightens walks Rule's fields in declaration order through the
// classification table below; the table is field-complete, which
// TestTightensIsFieldComplete pins by reflection, so a field added to
// Rule later cannot silently slip through unclassified. An unclassified
// field — the branch the test keeps dead — classifies as LOOSER:
// fail-closed, because a tightening nobody classified is a loosening
// wearing a tightening's diff.
//
// The final table (the one the tests assert):
//
//	Field                 tighter                     looser                    neutral
//	Enabled               false→true                  true→false                same
//	RequireLocal          false→true                  true→false                same
//	RequireDoD            none→all_met                all_met→none              same
//	RequiredChecks        a superset (adds names)     anything else (drops one) equal
//	ChecksPolicy          required_only→reported_green  reported_green→required_only  same
//	NoChecks              allow→refuse                refuse→allow              same
//	ReviewMode            {forge,aeon}→both;          {forge,aeon}→either;      same; forge↔aeon
//	                      either→{forge,aeon,both}    both→{forge,aeon,either}  INCOMPARABLE
//	                                                              (ok=false, absent from the
//	                                                              loosened list)
//	RequireThreads        false→true                  true→false                same
//	AeonReviewAgents      a subset (fewer agents)     a superset (more agents)  equal
//	Method                —                           —                           always
//
// Identity fields (ComponentID, Revision, Source) never classify:
// provenance is not behaviour.
//
// The review mode's argument, in one breath: both is strictest (it
// wants both authorities); either is looser than forge and looser than
// aeon (it wants neither specifically); and forge vs aeon are
// INCOMPARABLE — one is a person's approval on the forge, the other is
// the engine's own verdict, and "stricter" between a person and a
// machine is not a sentence this project will print. A switch between
// them is refused as not-a-tightening, in both directions.

package mergerule

import "reflect"

type fieldVerdict int

const (
	vNeutral fieldVerdict = iota
	vTighter
	vLooser
	vIncomparable
	vIdentity
)

// identity and neutral are the two verdicts every classifier can
// return for "no change here".
func identity(_, _ Rule) fieldVerdict { return vIdentity }
func neutral(_, _ Rule) fieldVerdict  { return vNeutral }

// fieldBool is the verdict for a bool field the ON-way tightens (a
// requirement that appears tightens; one that disappears loosens).
func fieldBool(get func(Rule) bool) func(Rule, Rule) fieldVerdict {
	return func(current, proposed Rule) fieldVerdict {
		switch {
		case get(current) == get(proposed):
			return vNeutral
		case !get(current) && get(proposed):
			return vTighter
		default:
			return vLooser
		}
	}
}

// fieldEnum is the verdict for a two-value field with a known order:
// the first argument is the LOOSER value. Anything outside the closed
// pair classifies as looser (fail-closed; the write door and the
// migration's CHECKs keep the closed set real).
func fieldEnum(loose, tight string, get func(Rule) string) func(Rule, Rule) fieldVerdict {
	return func(current, proposed Rule) fieldVerdict {
		c, p := get(current), get(proposed)
		switch {
		case c == p:
			return vNeutral
		case c == loose && p == tight:
			return vTighter
		case c == tight && p == loose:
			return vLooser
		default:
			return vLooser
		}
	}
}

// fieldSuperset: a list field that tightens when it GROWS — a proper
// superset (every current entry present in the proposed) tightens,
// equality is neutral, and dropping even one entry loosens. A swap
// (neither a superset nor a subset) is fail-closed.
func fieldSuperset(get func(Rule) []string) func(Rule, Rule) fieldVerdict {
	return func(current, proposed Rule) fieldVerdict {
		return listVerdict(get(current), get(proposed), true)
	}
}

// fieldSubset: the INVERSE order — the Aeon-review agents' list
// tightens when it SHRINKS (fewer agents whose verdict can satisfy the
// aeon half) and loosens when it grows.
func fieldSubset(get func(Rule) []string) func(Rule, Rule) fieldVerdict {
	return func(current, proposed Rule) fieldVerdict {
		return listVerdict(get(current), get(proposed), false)
	}
}

func listVerdict(cur, prop []string, supersetTightens bool) fieldVerdict {
	sup, sub := isSuperset(cur, prop), isSuperset(prop, cur)
	switch {
	case sup && sub:
		return vNeutral
	case sup:
		if supersetTightens {
			return vTighter
		}
		return vLooser
	case sub:
		if supersetTightens {
			return vLooser
		}
		return vTighter
	default:
		return vLooser
	}
}

func isSuperset(sub, super []string) bool {
	for _, s := range sub {
		found := false
		for _, u := range super {
			if u == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// reviewModeVerdict is the mode's order: both strictest, either
// loosest, forge and aeon incomparable (a switch between them is
// vIncomparable — refused, but not a loosening, which is why the
// loosened list stays empty when this is the only problem).
func reviewModeVerdict(current, proposed Rule) fieldVerdict {
	c, p := current.ReviewMode, proposed.ReviewMode
	if c == p {
		return vNeutral
	}
	switch {
	case c == ReviewModeForge && p == ReviewModeAeon,
		c == ReviewModeAeon && p == ReviewModeForge:
		return vIncomparable
	case c == ReviewModeEither:
		return vTighter // everything else wants more than either
	case c == ReviewModeBoth:
		return vLooser // nothing wants less than both
	case p == ReviewModeBoth:
		return vTighter
	case p == ReviewModeEither:
		return vLooser
	default:
		return vLooser // an out-of-set value fails closed
	}
}

// classifiers is the table — every field of Rule, named, in no
// particular order (Tightens walks the struct's declaration order, so
// the loosened list reads in field order regardless).
var classifiers = map[string]func(Rule, Rule) fieldVerdict{
	// Identity: provenance, never behaviour.
	"ComponentID": identity,
	"Revision":    identity,
	"Source":      identity,
	"UpdatedAt":   identity,

	// The switches.
	"Enabled":                fieldBool(func(r Rule) bool { return r.Enabled }),
	"RequireLocal":           fieldBool(func(r Rule) bool { return r.RequireLocal }),
	"RequireThreadsResolved": fieldBool(func(r Rule) bool { return r.RequireThreadsResolved }),

	// The enums, loose value first.
	"RequireDoD":   fieldEnum(RequireDoDNone, RequireDoDAllMet, func(r Rule) string { return r.RequireDoD }),
	"NoChecks":     fieldEnum(NoChecksAllow, NoChecksRefuse, func(r Rule) string { return r.NoChecks }),
	"ChecksPolicy": fieldEnum(ChecksRequired, ChecksReported, func(r Rule) string { return r.ChecksPolicy }),

	// The lists, in their set orders.
	"RequiredChecks":   fieldSuperset(func(r Rule) []string { return r.RequiredChecks }),
	"AeonReviewAgents": fieldSubset(func(r Rule) []string { return r.AeonReviewAgents }),

	// The mode, with its incomparable pair.
	"ReviewMode": reviewModeVerdict,

	// The merge kind: the gate does not read it (T6 asks the forge
	// for it), so it never tightens or loosens anything.
	"Method": neutral,
}

// Tightens reports whether proposed is a tightening of current: every
// field neutral, tighter or identity. ok=false with a NON-empty
// loosened list names the loosened fields (declaration order);
// ok=false with an EMPTY list means the one incomparable pair — a
// forge↔aeon review-mode switch, which no direction may authorise.
func Tightens(current, proposed Rule) (bool, []string) {
	t := reflect.TypeOf(Rule{})
	ok := true
	var loosened []string
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Name
		c, found := classifiers[name]
		if !found {
			// Fail-closed (TestTightensIsFieldComplete keeps this
			// branch dead): a field nobody classified can never be
			// a tightening.
			ok = false
			loosened = append(loosened, name)
			continue
		}
		switch c(current, proposed) {
		case vLooser:
			ok = false
			loosened = append(loosened, name)
		case vIncomparable:
			ok = false
		}
	}
	return ok, loosened
}
