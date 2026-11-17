package protocol

// surface_table_automation.go — S49 T3: the surface table's unified
// automation READ entries.
//
// All ten are classified here, beside their own family's names (the
// split-by-cohesion pattern the export/gates/supervision files set). Reads,
// because that is what they are: they read rows and run read-only
// production code (the clock walk, the grammar, the validator) and write
// nothing. They are not HumanDoorOnly flags — the flag marks writes that
// move something real past a person's authority (P34's door); the automation
// WRITES (T4) will carry their own classification then. AN7 rules the narrow
// projection instead: none of these enters narrow.Actions(), and the narrow
// guard stays pinned exactly as it was.

func surfaceTableAutomation() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodAutomationList: {
			Kind:    SurfaceRead,
			Summary: "One list over both automation families — filters, archived hidden unless asked, two honest empties (AN1/AN5).",
		},
		MethodAutomationShow: {
			Kind:    SurfaceRead,
			Summary: "One automation whole: canonical form, plain-words description, health, its last five fires and five changes (AN5).",
		},
		MethodAutomationFires: {
			Kind:    SurfaceRead,
			Summary: "The unified fire log — both engines' answers newest first, reasons verbatim, outcomes the closed five, cursor paged.",
		},
		MethodAutomationRuns: {
			Kind:    SurfaceRead,
			Summary: "The loops one automation started, by provenance (started_by/trigger_id), with the total the list pages through.",
		},
		MethodAutomationHealth: {
			Kind:    SurfaceRead,
			Summary: "Could it fire right now — blueprint, secret presence, workspace, scope, switches; advice that never gates (AN8).",
		},
		MethodAutomationVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The forms' word lists derived from the validator itself: events, applicable keys, outcomes, limits, zones (AN5).",
		},
		MethodAutomationPreview: {
			Kind:    SurfaceRead,
			Summary: "The next instants a schedule will ask at — the engine's own clock walk, DST answers, no write and no claim (AN8).",
		},
		MethodAutomationTest: {
			Kind:    SurfaceRead,
			Summary: "Would this rule match this sample event — the real grammar's verdict, per-key, with the canonical stored form.",
		},
		MethodAutomationDryRun: {
			Kind:    SurfaceRead,
			Summary: "Which waiting outbox events one trigger would have started — a pure scan; nothing is claimed, the cursor stays (AN8).",
		},
		MethodAutomationChanges: {
			Kind:    SurfaceRead,
			Summary: "Who changed which automation and when, newest first — the audit the write doors stamp (AN4).",
		},
		// T4's two state doors. They are WRITES with a person behind them:
		// archive is the reversible invisibility (AN6), never a delete —
		// there is no delete door to classify and the absence is the rule.
		MethodAutomationArchive: {
			Kind:          SurfaceWrite,
			Summary:       "Sends an automation to the archive — engine-invisible, history intact, reason required, audited once; the second archive changes nothing.",
			HumanDoorOnly: true,
		},
		MethodAutomationUnarchive: {
			Kind:          SurfaceWrite,
			Summary:       "Brings an archived automation back to the engines' sight; enabling it afterwards is a separate act, on its own door.",
			HumanDoorOnly: true,
		},

		// — the one agent door (S49 T7): a write by nature, never a door —
		MethodAutomationProposeDisable: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT proposal to DISABLE one automation; narrowing-only — nothing enforced until a person commits it (AN9, TD2).",
		},
	}
}
