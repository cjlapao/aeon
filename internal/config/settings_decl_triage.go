package config

// settings_decl_triage.go — S51 T3 (TM4) declares the one blueprint choice
// the triage's validation dispatch used to carry as a constant: which
// blueprint `triage.validate` starts when a person asks for an attempt
// (S29 shipped it as the hidden `brainstorm` constant — the same founding
// sin S50's RM3 closed for ideation and planning). A validate reads its
// name from this registry at the moment of dispatch — a person changes it
// in Settings → Triage and the very next attempt starts the new one, no
// restart — and a name that does not exist or does not validate REFUSES the
// dispatch with the setting named. There is deliberately no fallback: a
// fallback would let a mistyped name silently run a blueprint nobody chose.
//
// The setting gates nothing. Policy still gates every call (the loop's own
// gate is untouched here); this chooses WHAT RUNS, and `triage.health`
// reports the choice's state honestly — ok, missing, invalid, or
// unverifiable when the daemon cannot see the blueprint source at all. It
// is not Sensitive: a blueprint name is not a credential, and the panel
// confirming its change would be theatre (CS8).
func init() {
	mustDeclare(Setting{
		Key:     "triage.validationBlueprint",
		Area:    "triage",
		Type:    TypeString,
		Default: "brainstorm",
		MaxLen:  64,
		Description: "Which blueprint `triage.validate` starts when a person asks for a validation attempt (S51 TM4). " +
			"The default `brainstorm` is the shipped chat-with-tools shape S29 used: its lane reads the bug's title and body " +
			"and attempts to reproduce. Any existing validating blueprint works; a name that does not exist or does not " +
			"validate refuses the dispatch naming this setting — it never silently falls back, and policy still gates every call.",
	})
}
