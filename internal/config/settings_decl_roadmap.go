package config

// settings_decl_roadmap.go — S50 T3 (RM3) declares the two blueprint choices
// the roadmap's dispatch used to carry as constants: which blueprint
// IDEATION starts (S26 shipped it as a hidden `brainstorm` constant, brief §3's
// founding sin) and which one hosts the PLANNING step. A dispatch reads its
// name from this registry at the moment of dispatch — a person changes it in
// Settings → Roadmap and the very next `roadmap.ideate` starts the new one, no
// restart — and a name that does not exist or does not validate REFUSES the
// dispatch with the setting named. There is deliberately no fallback: a
// fallback would let a mistyped name silently run a blueprint nobody chose,
// which is exactly the invisibility the constants had.
//
// Neither setting gates anything. Policy still gates every call (the agent
// plane's refusals are S43/S45's, untouched here); these choose WHAT RUNS, and
// `roadmap.health` reports the choice's state honestly — ok, missing,
// invalid, or unverifiable when the daemon cannot see the blueprint source at
// all. Neither is Sensitive: a blueprint name is not a credential, and the
// panel confirming its change would be theatre (CS8).
func init() {
	mustDeclare(Setting{
		Key:     "roadmap.ideationBlueprint",
		Area:    "roadmap",
		Type:    TypeString,
		Default: "brainstorm",
		MaxLen:  64,
		Description: "Which blueprint `roadmap.ideate` starts when a roadmap item enters ideation (S50 RM3). " +
			"The default `brainstorm` is the shipped ideation shape: a chat with tools whose capture step produces the brief. " +
			"Any existing validating blueprint that declares a brief.md producer works; a name that does not exist or does not " +
			"validate refuses the dispatch naming this setting — it never silently falls back, and policy still gates every call.",
	})
	mustDeclare(Setting{
		Key:     "roadmap.planBlueprint",
		Area:    "roadmap",
		Type:    TypeString,
		Default: "roadmap-plan",
		MaxLen:  64,
		Description: "Which blueprint hosts the planning step when a roadmap item's planning dispatches (S50 RM3). " +
			"The default `roadmap-plan` is the seeded standard record carrying the single project-planner lane the daemon used " +
			"to hold as a compiled-in string. A name that does not exist or does not validate refuses the dispatch naming this " +
			"setting; the choice changes at the next dispatch, not at a restart.",
	})
}
