// floor.go — the autonomy floor (VG12).
//
// The floor is the list of ingredients a rule must have before a
// person may grant forge.merge AUTONOMY over the component. It is a
// table of what is MISSING, each missing thing named — an empty list
// is the only "safe", which is the shape the grant's UI can render
// honestly ("these three do not hold: …").
//
// The ingredients, in the order the missing list reports them:
//
//  1. the rule is ENABLED — a disabled row is the baseline, and the
//     baseline is a person's gate, never autonomy's;
//  2. require_local is set AND the effective gate set has at least
//     one enabled REQUIRED gate — a local requirement with nothing to
//     run is a requirement in name only (the VG11 note: a required
//     set of zero gates makes the run unverified, which is not green);
//  3. require_dod is all_met — a done loop is one the criteria say is
//     done, not one the person stopped checking;
//  4. no_checks is refuse AND the refusal has a set to bind: at least
//     one named check, or the reported_green policy (which binds
//     every reported check) — refuse with an empty set and
//     required_only refuses the empty report but passes any reported
//     one, which is not a binding signal;
//  5. the review mode is NOT forge-only — forge-only review is a
//     person's approval, and a person who approves autonomously is a
//     contradiction this project will not print (aeon, either and
//     both all admit autonomy; forge never does).
//
// require_threads_resolved is deliberately NOT an ingredient: threads
// are an honesty signal, and the floor's job is to say the merge has
// something to be verified against — a person may grant autonomy over
// a tree whose forge simply cannot report threads, and the refusal
// when it matters is the threads-unreportable one, which the rule
// already expresses.

package mergerule

// AutonomyFloor is the floor itself: the rule and the effective gate
// set it would run under, and the missing ingredients — empty iff
// every one holds.
func AutonomyFloor(rule Rule, gates EffectiveGates) (bool, []Missing) {
	var missing []Missing
	add := func(code, sentence string) {
		missing = append(missing, Missing{Code: code, Sentence: sentence})
	}

	if !rule.Enabled {
		add(FloorDisabled, "the rule is disabled — nothing in it applies")
	}

	if !rule.RequireLocal {
		add(FloorLocalNotRequired, "require_local is not set — no gate result is demanded of the merge")
	} else if gates.RequiredCount() == 0 {
		add(FloorNoRequiredGates,
			"no required gate in the effective set — a local requirement with nothing to run is a requirement in name only")
	}

	if rule.RequireDoD != RequireDoDAllMet {
		add(FloorDoDNotRequired,
			"require_dod is not all_met — a done loop is one the criteria say is done, not one nobody checked")
	}

	if rule.NoChecks != NoChecksRefuse {
		add(FloorChecksNotBound,
			"no_checks is not refuse — the forge reporting nothing is a green gate")
	} else if len(rule.RequiredChecks) == 0 && rule.ChecksPolicy != ChecksReported {
		add(FloorChecksNotBound,
			"no_checks is refuse but nothing is named and required_only binds no reported check — the refusal has nothing to refuse against")
	}

	if rule.ReviewMode == ReviewModeForge {
		add(FloorForgeOnly,
			"forge-only review is a person's approval — it is never autonomous")
	}

	return len(missing) == 0, missing
}
