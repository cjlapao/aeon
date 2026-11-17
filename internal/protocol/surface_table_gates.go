package protocol

// surface_table_gates.go — S57 T8 (CR-TC1): the surface table's twenty-two
// gate-plane entries, split from the parts by cohesion (the gate plane's
// own classification lives with its own names, the backup family's
// precedent).
//
// Thirteen READS are plain reads: the gate set with its sources, the
// runs and their per-gate results (the tails' only door), the closed
// sets the forms render from, the rule with its floor, the rule's
// audit, the DoD ledger, the merge events and the standing watches.
// A read about the gates is a read: nothing about one is a door only a
// person may pass through.
//
// Nine WRITES are HumanDoorOnly (VG17): each stamps the actor from the
// connection and moves something real — a gate row is written or
// moved, a run is spawned, a rule revision is issued, a DoD mark lands.
// They are named here by CONSTANT (the LW8 discipline) and join
// surfaceHumanDoorFamilies in surface.go, so the family — the "what is
// true must be marked" direction — catches a table edit that forgets a
// row. Their propose-only agent twins (S57 T9, VG19 — the three
// verify/mergerule/dod twins in this table's last block) are the
// human-door rule's one named exception: SurfaceWrite WITHOUT
// HumanDoorOnly, each exempted by its one entry in
// surfaceProposeOnlyTwins — inert until a person commits.

func surfaceTableGates() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodVerifyList: {
			Kind:    SurfaceRead,
			Summary: "Lists the component's verification gates (own, or the effective set with each source marked), each with its last run and a policy preview.",
		},
		MethodVerifyShow: {
			Kind:    SurfaceRead,
			Summary: "Shows one gate in full — its fields, its event trail and its last run — the detail a list row links to.",
		},
		MethodVerifyRuns: {
			Kind:    SurfaceRead,
			Summary: "Lists finished verification runs newest first, scoped by component, loop and state, with each run's state and one-line account.",
		},
		MethodVerifyRunShow: {
			Kind:    SurfaceRead,
			Summary: "Shows one run with its per-gate results — exit codes, wall time, digests, and the output tails: the only door that crosses tails.",
		},
		MethodVerifyVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed sets the gate forms render from — requirement, run states, run reasons — the env allowlist names, and the try statement.",
		},
		MethodVerifyTemplates: {
			Kind:    SurfaceRead,
			Summary: "The inert verify templates, grouped by stack — a candidate gate each does nothing of until a person applies it to a component.",
		},
		MethodVerifyAdd: {
			Kind:          SurfaceWrite,
			Summary:       "Adds a verification gate to a component, as the human door — the actor is stamped from the connection, never a param.",
			HumanDoorOnly: true,
		},
		MethodVerifyUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Changes the named fields of one gate, as the human door — the actor is stamped from the connection, never a param.",
			HumanDoorOnly: true,
		},
		MethodVerifyArchive: {
			Kind:          SurfaceWrite,
			Summary:       "Archives one gate — it leaves the live set and its run history is untouched — as the human door, actor from the connection.",
			HumanDoorOnly: true,
		},
		MethodVerifyRestore: {
			Kind:          SurfaceWrite,
			Summary:       "Restores an archived gate to the live set, position and all — as the human door, actor from the connection.",
			HumanDoorOnly: true,
		},
		MethodVerifyApplyTemplate: {
			Kind:          SurfaceWrite,
			Summary:       "Copies the named templates onto a component as origin=template gates, all or nothing — the human door, actor from the connection.",
			HumanDoorOnly: true,
		},
		MethodVerifyTry: {
			Kind:          SurfaceWrite,
			Summary:       "Runs one gate now in the component's primary root, under the policy gate, recorded as a manual run — the response names active loops.",
			HumanDoorOnly: true,
		},
		MethodMergeRuleGet: {
			Kind:    SurfaceRead,
			Summary: "The merge rule for a component — its own row, or the effective one with source, revision, last change and the floor's missing ingredients.",
		},
		MethodMergeRulePreview: {
			Kind:    SurfaceRead,
			Summary: "A read-only dry evaluation: the rule, its source and the autonomy floor, and — with a PR — what the gate would say, refusal by refusal.",
		},
		MethodMergeRuleChanges: {
			Kind:    SurfaceRead,
			Summary: "The rule's audit trail newest first — revision, op, who, when, and the before/after images as raw JSON.",
		},
		MethodMergeRuleVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed sets the rule fields draw from, the refusal and floor codes, and the baseline rule in code — never a row.",
		},
		MethodMergeRuleSet: {
			Kind:          SurfaceWrite,
			Summary:       "Sets the component's merge rule in full, as the human door — any rule is a person's to set; tightening-only is the proposal path's law.",
			HumanDoorOnly: true,
		},
		MethodMergeRuleClear: {
			Kind:          SurfaceWrite,
			Summary:       "Deletes the component's rule row — it returns to its root's rule or the baseline — as the human door, actor from the connection.",
			HumanDoorOnly: true,
		},
		MethodDoDList: {
			Kind:    SurfaceRead,
			Summary: "The loop's definition-of-done items, in brief order, each with its state and — when met — the evidence it names and the mark's actor.",
		},
		MethodDoDMark: {
			Kind:          SurfaceWrite,
			Summary:       "Marks one DoD item, evidence-bound — a met item names its evidence — as the human door, the actor is the connection's person.",
			HumanDoorOnly: true,
		},
		MethodMergeEvents: {
			Kind:    SurfaceRead,
			Summary: "The merge events — the merges that happened and the gate's account of each: evidence, rule revision, the run the rule measured.",
		},
		MethodMergeWatches: {
			Kind:    SurfaceRead,
			Summary: "The standing merge watches — the merges the gate is still checking, by state, with their attempts and last words.",
		},

		// S57 T9 (VG19): the three propose-only agent twins — the human
		// door rule's one named exception, classified SurfaceWrite WITHOUT
		// HumanDoorOnly (they are the agent's doors, and the one entry
		// each in surfaceProposeOnlyTwins is what exempts them, the
		// governance twins' precedent). They file INERT rows; nothing
		// tightens, runs or marks until a person commits.
		MethodVerifyProposeAdd: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT verify_command proposal (op add-only) on the loop's own component — no gate runs until a person commits it.",
		},
		MethodMergeRuleProposeTighten: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT merge_rule proposal, tightening-only against the component's effective rule — inert until a person commits it.",
		},
		MethodDoDMarkAsAgent: {
			Kind:    SurfaceWrite,
			Summary: "Marks a DoD item of the loop's own ledger — unmet/unverifiable freely, met bound to a passing run on the current head.",
		},
	}
}
