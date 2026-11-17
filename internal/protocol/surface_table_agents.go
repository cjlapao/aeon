// The agent plane of the surface table (S47 T6): the agent-to-agent lifecycle, the
// roster, the plugin ledger, the agent-management family, the S47 tools catalog and the
// surface doors themselves.
package protocol

// surfaceTableAgents is one slice of the closed classification table, built fresh on every
// call: there is no package-level map to append to, so a method cannot enter the surface
// by mutating shared state — only by editing a table, which the completeness tests then
// check both ways.
func surfaceTableAgents() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodAgentCard: {
			Kind:    SurfaceRead,
			Summary: "One agent card as the load path serves it; what is enumerable is what is invokable.",
		},
		MethodAgentCreate: {
			Kind:          SurfaceWrite,
			Summary:       "Creates a first-party-shaped agent row; the row surface is the daemon, the CLI is its client.",
			HumanDoorOnly: true, // S73 AR8 — see the family note below
		},
		MethodAgentDelete: {
			Kind:          SurfaceWrite,
			Summary:       "Deletes an agent row the store allows to go; a dispatching agent is refused by name.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentDisable: {
			Kind:          SurfaceWrite,
			Summary:       "Disables an imported agent; stopping narrows, so no staging is asked of it.",
			HumanDoorOnly: true,
		},
		MethodAgentDiscover: {
			Kind:    SurfaceRead,
			Summary: "Lists invokable agent candidates \u2014 tags, examples, the name spelled the way the door reads them.",
		},
		MethodAgentDuplicate: {
			Kind:          SurfaceWrite,
			Summary:       "Duplicates an agent row under a new slug with its content copied, never its provenance claim.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentEdit: {
			Kind:          SurfaceWrite,
			Summary:       "Edits an agent definition content in place through the one row surface.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentEnable: {
			Kind:          SurfaceWrite,
			Summary:       "Enables an already-imported agent, through the same door the CLI verb calls, re-check included.",
			HumanDoorOnly: true,
		},
		MethodAgentExport: {
			Kind:          SurfaceWrite,
			Summary:       "Writes an agent definition out to a package path the caller names.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentImport: {
			Kind:          SurfaceWrite,
			Summary:       "Imports an agent card — from a filesystem path or from its own contents — verified before anything is stored.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentInvoke: {
			Kind:    SurfaceDiagnostic,
			Summary: "Files an agent-to-agent ask as a WAITING task and returns; suspension is rows, not a blocked call.",
		},
		// S73 AR8 — the tightening, in one place because it is one claim.
		// The CRUD family above (create · edit · delete · duplicate · import ·
		// export · migrate) joins enable/disable/revoke as a HUMAN DOOR. This is a
		// classification, not a wall: nothing here removes a door, it says which
		// doors were never there — and surface.go:36-40 makes the claim prove
		// itself (TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor refuses the mark
		// if the narrow projection, the agent-services enumeration or the tool
		// registry can reach the method). Measured before the mark:
		// MethodAgentCreate appears in three non-test places — the dispatch switch
		// in cmd/aeon, the registration, and this table — and in nothing under
		// internal/tools. The mark is therefore what was already true, written down
		// where surface.list answers it; the day some projection grows a reach, that
		// guard fails and the claim has to be re-earned.
		//
		// The agent-management READS carry the mark too (agent.view, agent.rows,
		// agent.changes). A read marking has siblings, not a precedent-less first:
		// history.export, platform.changes and retrieval.reindexPlan all read and are
		// marked. The line is SP6's — the person is at the ACTS, and what the mark
		// records here is that no agent talks to the management plane at all.
		MethodAgentList: {
			Kind:    SurfaceRead,
			Summary: "S34 T1's one read: the roster as identity \u2014 first-party bodies and imported plugins alike, with provenance and the REAL state vocabulary.",
		},
		MethodAgentMigrate: {
			Kind:          SurfaceWrite,
			Summary:       "Runs the declared migration over agent rows, reporting what moved and what refused.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentPlugins: {
			Kind:    SurfaceRead,
			Summary: "The plugin ledger: what is installed, in which state, from where \u2014 management stays CLI and app only.",
		},
		MethodAgentRevoke: {
			Kind:          SurfaceWrite,
			Summary:       "Revokes an imported agent with a required reason; revocation is terminal at the database itself.",
			HumanDoorOnly: true,
		},
		MethodAgentTaskComplete: {
			Kind:    SurfaceDiagnostic,
			Summary: "The callee settles its task and reports its cost; a settled task answers by naming its state.",
		},
		MethodAgentTaskFail: {
			Kind:    SurfaceDiagnostic,
			Summary: "The callee fails its task with a required reason, recorded verbatim in the task envelope.",
		},
		MethodAgentTaskList: {
			Kind:    SurfaceRead,
			Summary: "Lists the asks and their chain edges as the rows hold them, never the callee toolset.",
		},
		MethodAgentTaskShow: {
			Kind:    SurfaceRead,
			Summary: "One task with its chain and a cost rollup that degrades to partial when a child did not report.",
		},
		MethodAgentView: {
			Kind:          SurfaceRead,
			Summary:       "One agent definition as stored, with its provenance and the state the real code can name.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentRows: {
			Kind:          SurfaceRead,
			Summary:       "One paged, sorted, searched window of the agent rows — the management table's read and the paging convention's first adopter.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodAgentChanges: {
			Kind: SurfaceRead,
			Summary: "One paged window of the agent ledger — newest first, narrowed by one " +
				"agent's slug or not at all; one order, no sort offered.",
			HumanDoorOnly: true, // S73 AR8
		},
		MethodSurfaceGet: {
			Kind:    SurfaceRead,
			Summary: "One method as the surface sees it: its kind, its sentence, and which doors can reach it.",
		},
		MethodSurfaceList: {
			Kind:    SurfaceRead,
			Summary: "Every method this daemon holds, classified read/write/control/diagnostic with the doors that reach each one.",
		},
		MethodToolsByAgent: {
			Kind:    SurfaceRead,
			Summary: "One agent's whole tool view, each state the same sentence the step's gate would give, plus whether it can dispatch at all.",
		},
		MethodToolsGet: {
			Kind:    SurfaceRead,
			Summary: "One tool in full: the code schema, its class and capabilities, the per-agent table and the policy refs that speak about it.",
		},
		MethodToolsGrants: {
			Kind:    SurfaceRead,
			Summary: "The raw S12 authority view for an agent's MCP tools, verbatim from the GrantView \u2014 a catalog, not a re-decision.",
		},
		MethodToolsList: {
			Kind:    SurfaceRead,
			Summary: "Every tool this build exposes \u2014 system and MCP \u2014 filtered daemon-side, with the empty-table sentence when a filter matches nothing.",
		},
		MethodToolsMineList: {
			Kind:    SurfaceRead,
			Summary: "The calling agent's own inventory — the tools it holds with state and reason; a person is pointed at tools.byAgent instead.",
		},
		MethodToolsMineShow: {
			Kind:    SurfaceRead,
			Summary: "One tool from the calling agent's own set in full; an unheld name and a nonexistent one answer the same, so no oracle.",
		},
		MethodToolsStatus: {
			Kind:    SurfaceRead,
			Summary: "Whether the seeded catalog rows still match the code: the seed set state and the drift list, reported and never repaired.",
		},
		MethodToolsVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The catalog's own words: the capability vocabulary with its meanings, beside the family, source and class spellings a filter may offer.",
		},
	}
}
