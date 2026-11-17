// surface_table_s61.go — S61 T7 (GI8..GI14): the goal-intake plane's own
// classification. Ten reads and five human doors (charter.save,
// template.save, intake.start, intake.commit, intake.abandon — T9's fifth,
// 2026-11-05), and the reads are reads:
// `intake.plan` previews, files
// nothing (GI10), and `charter.*` answers what is saved, running nothing
// (GI8).
package protocol

// surfaceTableS61 is one slice of the closed classification table, built
// fresh on every call, like every other part.
func surfaceTableS61() map[MethodName]Surface {
	return map[MethodName]Surface{

		// — the charter plane (GI8): four reads of what is saved and the
		// one human save. The charter grants NOTHING through these doors.
		MethodCharterGet: {
			Kind:    SurfaceRead,
			Summary: "One root's charter: current revision, the description, body.proposed verbatim — read, runs nothing (GI8).",
		},
		MethodCharterList: {
			Kind:    SurfaceRead,
			Summary: "Every chartered root with its revision, summary line, and whether a proposed arm is present — a details list, never bare ids.",
		},
		MethodCharterRevisions: {
			Kind:    SurfaceRead,
			Summary: "One root's charter history, newest first, paged below the cursor revision.",
		},
		MethodCharterVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The charter package's own closed sets: check kinds, caps, and the autonomy vocabulary when the S56 seam answers it.",
		},
		MethodCharterSave: {
			Kind:          SurfaceWrite,
			Summary:       "Append a charter revision — a person's door (GI8); the actor is the connection's, never a param (P4).",
			HumanDoorOnly: true,
		},
		MethodCharterPropose: {
			Kind:    SurfaceWrite,
			Summary: "File an INERT charter_save proposal — the agent's ask (GI17); decides nothing, saves nothing until a person commits.",
		},

		// — the template plane (GI14, GI9's other half): two reads and the
		// one human save; an edit never moves origin.
		MethodTemplateList: {
			Kind:    SurfaceRead,
			Summary: "The scaffold-template set: origin, version, stack tags, file count, diverged — filterable by stackTag and origin.",
		},
		MethodTemplateGet: {
			Kind:    SurfaceRead,
			Summary: "One template whole at its current version, files and contents verbatim (a template is a document, not a secret).",
		},
		MethodTemplateSave: {
			Kind:          SurfaceWrite,
			Summary:       "Save a template row+revision as one — a person's door (GI14); editing a shipped template never relabels its origin.",
			HumanDoorOnly: true,
		},

		// — the intake plane (GI9..GI13): the wizard's seven doors. Three are
		// writes (start files row+scratch+loop; commit is Engine.Commit;
		// abandon closes a session that will not commit), and four read —
		// plan included, which files nothing (GI10).
		MethodIntakeStart: {
			Kind:          SurfaceWrite,
			Summary:       "File an intake row, its private scratch, and the attended goal-intake loop — a person's door (GI6/GI9).",
			HumanDoorOnly: true,
		},
		MethodIntakeList: {
			Kind:    SurfaceRead,
			Summary: "Every intake with its honest state (idea → committed), proposal id, and root — derived from rows, never a column lie.",
		},
		MethodIntakeStatus: {
			Kind:    SurfaceRead,
			Summary: "One intake's ledger: state, steps with outcomes and details, and where a crashed commit stands — GI11's read side.",
		},
		MethodIntakePlan: {
			Kind:    SurfaceRead,
			Summary: "Preview the commit — files per component plus the rows census — and hand out its digest. Files nothing (GI10).",
		},
		MethodIntakeCommit: {
			Kind:          SurfaceWrite,
			Summary:       "Commit the plan — scaffold, rows, one commit (GI9), resumable (GI11); a person's door, the actor the connection's (P4).",
			HumanDoorOnly: true,
		},
		MethodIntakeVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed sets from the enforcing packages: publish modes, roles, plan caps, templates, the workspace root as configured.",
		},
		MethodIntakePropose: {
			Kind:    SurfaceWrite,
			Summary: "File an INERT intake_plan proposal from inside an intake session — the agent's ask (GI17); inert until a person commits.",
		},
		MethodIntakeAbandon: {
			Kind:          SurfaceWrite,
			Summary:       "Close the interview loop and reject the intake's open plans — a person's door (GI13); the scratch stays on disk, nothing is deleted.",
			HumanDoorOnly: true,
		},
	}
}
