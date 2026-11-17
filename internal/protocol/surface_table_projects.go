// The project plane of the surface table (S47 T6): blueprints, the code graph, the
// telemetry reads, the learning record, the roadmap, triage, the dispatch queue, the
// async-worker ledger, history and the retrieval doors.
package protocol

// surfaceTableProjects is one slice of the closed classification table, built fresh on every
// call: there is no package-level map to append to, so a method cannot enter the surface
// by mutating shared state — only by editing a table, which the completeness tests then
// check both ways.
func surfaceTableProjects() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodAsyncWorkersCancel: {
			Kind:    SurfaceControl,
			Summary: "Cancels a running async worker job; the ledger records the cancellation as its own outcome.",
		},
		MethodAsyncWorkersGet: {
			Kind:    SurfaceRead,
			Summary: "One async worker job with its progress and its result or refusal.",
		},
		MethodAsyncWorkersList: {
			Kind:    SurfaceRead,
			Summary: "The async-worker ledger: what is running, what finished, what failed and why.",
		},
		MethodBlueprintFields: {
			Kind:    SurfaceRead,
			Summary: "The field vocabulary a blueprint may name, so a form cannot invent one.",
		},
		MethodBlueprintModel: {
			Kind:    SurfaceRead,
			Summary: "A flow's text read into the structure a form edits; it judges nothing.",
		},
		MethodBlueprintRender: {
			Kind:    SurfaceRead,
			Summary: "A flow's structure written back as text, ready to validate and save; it writes nothing.",
		},
		MethodBlueprintVocabulary: {
			Kind:    SurfaceRead,
			Summary: "Every closed set a flow form offers: step kinds, operators, code actions with their arguments.",
		},
		MethodBlueprintList: {
			Kind:    SurfaceRead,
			Summary: "The blueprint catalogue with each lane count and whether it validates today.",
		},
		MethodBlueprintSave: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Saves a blueprint through the one surface; an invalid candidate never reaches a row.",
		},
		// S48 T6: the three write doors beside the rewritten save — same
		// kind (a person's write, the actor stamped by the daemon), and
		// each refuses with its named reason before any row moves.
		MethodBlueprintDuplicate: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Duplicates a blueprint as text (comments kept, version restarts) through the one write path; a copy never merges onto a taken name.",
		},
		MethodBlueprintDelete: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Deletes a user blueprint's row and refs — never its revisions — refusing while live automation names it, and for shipped rows.",
		},
		MethodBlueprintRevert: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Appends a named revision's content back as a NEW revision through the one write path; a stale revision is refused with the report.",
		},
		MethodBlueprintOverride: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Sets or clears one allowed field (today the route) on one lane of a shipped, locked blueprint; the blueprint's own bytes never change.",
		},
		MethodBlueprintShow: {
			Kind:    SurfaceRead,
			Summary: "One blueprint with its lanes, agents and gates as stored.",
		},
		MethodBlueprintValidate: {
			Kind:    SurfaceRead,
			Summary: "Validates a blueprint candidate and reports every refusal \u2014 the narrow surface reads this one.",
		},
		// S48 T7: the read doors — what uses what, the history, one
		// revision's bytes, a diff between two answers, and what policy
		// would say about its steps today. All read-only (BP7/BP8/BP11).
		MethodBlueprintUsage: {
			Kind:    SurfaceRead,
			Summary: "Both directions of use: live schedules, triggers and open queue items naming it, plus the agents, actions and skills its steps name.",
		},
		MethodBlueprintHistory: {
			Kind:    SurfaceRead,
			Summary: "A blueprint's revisions newest-first — content-free index; a deleted blueprint's history stays readable.",
		},
		MethodBlueprintRevision: {
			Kind:    SurfaceRead,
			Summary: "One named revision with its stored bytes verbatim — the diff and restore read from here.",
		},
		MethodBlueprintDiff: {
			Kind:    SurfaceRead,
			Summary: "A unified diff between revisions, current or (system rows only) shipped — computed daemon-side on the stored bytes.",
		},
		MethodBlueprintLint: {
			Kind:    SurfaceRead,
			Summary: "Advisory-only policy lint of a blueprint's tool states, reasons verbatim and dated; never changes validate or save.",
		},
		// S48 T10: the agent's propose door (BP9). A write by nature (it
		// files a row) and a write by classification — but NOT a human
		// door: it creates only the INERT blueprint_save proposal, which
		// constrains nothing until a person commits it (S35 TD2). The
		// five blueprint WRITE doors stay marked humanDoorOnly above;
		// surfaceProposeOnlyTwins carries this name as the family's one
		// exception, and T8's guard refuses any other blueprint tool or
		// method that writes rows.
		MethodBlueprintPropose: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT proposal to add or change a blueprint; it constrains nothing until a person commits it.",
		},
		MethodGraphBuild: {
			Kind:    SurfaceRead,
			Summary: "Reports a graph build worker current state for a root \u2014 the read beside the build, never a start door.",
		},
		MethodGraphDelete: {
			Kind:    SurfaceRead,
			Summary: "Reports the graph rows a root deletion would take with it.",
		},
		MethodGraphExplain: {
			Kind:    SurfaceRead,
			Summary: "Explains one code symbol from the graph: what it is, what calls it, what it calls.",
		},
		MethodGraphPath: {
			Kind:    SurfaceRead,
			Summary: "The path between two symbols, with the edges that carry it.",
		},
		MethodGraphQuery: {
			Kind:    SurfaceRead,
			Summary: "A graph query over nodes and edges, answered from the persisted graph only.",
		},
		MethodGraphReparse: {
			Kind:    SurfaceRead,
			Summary: "Reports what a reparse pass covers and what it refused to parse.",
		},
		MethodGraphRoots: {
			Kind:    SurfaceRead,
			Summary: "The roots the graph holds, for the picker to offer only what exists.",
		},
		MethodGraphStatus: {
			Kind:    SurfaceRead,
			Summary: "The graph state of a root: built, stale, absent, and when it last moved.",
		},
		MethodHistoryCoverage: {
			Kind:    SurfaceRead,
			Summary: "What the history view shows, does not show and cannot say for a scope — S53's honesty read.",
		},
		MethodHistoryExport: {
			// HV5 (S53): the export READS, and it is still a person's door —
			// it takes everything out of the record, so the classification
			// claims what S53 T2's guard proves: absent from narrow, the
			// agent services, the registry and the catalog.
			Kind:          SurfaceRead,
			HumanDoorOnly: true,
			Summary:       "Renders a project history export as data the caller can write anywhere.",
		},
		MethodHistoryList: {
			Kind:    SurfaceRead,
			Summary: "A project's event history, filtered and paged, the app window onto what already happened.",
		},
		MethodHistoryShow: {
			Kind:    SurfaceRead,
			Summary: "One history event with its payload as recorded, not re-derived.",
		},
		MethodHistoryVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed history lanes, outcome words and filter keys, served so no client types one.",
		},
		MethodLearningsAdd: {
			Kind:    SurfaceWrite,
			Summary: "Records a learning against a scope, with its tags and the step that learned it.",
		},
		MethodLearningsEmbed: {
			Kind:    SurfaceWrite,
			Summary: "Runs the embedding pass over one scope learnings so semantic search has vectors to read.",
		},
		MethodLearningsList: {
			Kind:    SurfaceRead,
			Summary: "Lists a scope learnings with their tags and confidence.",
		},
		MethodLearningsQuery: {
			Kind:    SurfaceRead,
			Summary: "Finds learnings by tag patterns across scopes, newest and most trusted first.",
		},
		MethodLearningsShow: {
			Kind:    SurfaceRead,
			Summary: "One learning in full, including the evidence that earned it.",
		},
		MethodLearningsRetract: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Takes one memory back with a reason — a stamp, never a delete; the human door's act (S52).",
		},
		MethodLearningsRestore: {
			Kind:          SurfaceWrite,
			HumanDoorOnly: true,
			Summary:       "Puts a retracted memory back into every default read; the human door's act (S52).",
		},
		MethodLearningsBoard: {
			Kind:    SurfaceRead,
			Summary: "The memory board: filtered daemon-side, every row with its own provenance sentence and retraction state (S52).",
		},
		MethodLearningsHistory: {
			Kind:    SurfaceRead,
			Summary: "One learning's audit career — add, retractions, restores — in the daemon's fixed-template sentences (S52).",
		},
		MethodLearningsVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The learnings vocabulary read from the code: scopes, four provenances in honesty order, limits, M1's law (S52).",
		},
		MethodLearningsMap: {
			Kind:    SurfaceRead,
			Summary: "Where each learning sits when laid out by what it means, and its nearest neighbours — only learnings with a stored vector are placed.",
		},
		MethodLearningsForAgent: {
			Kind:    SurfaceRead,
			Summary: "What would this agent be shown: its resolved scopes through the same Query, each hit with its why (S52).",
		},
		// S52 T4 (LM5/LM7, the S46 mcp.mine classification shape): the
		// agent's OWN four. No row here claims HumanDoorOnly — these are
		// the opposite claim, agent-scoped doors whose person-facing
		// mirror refuses a person by name; the plane's two human-door
		// rows (retract, restore) stand above with their claim, and T3's
		// TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor holds the
		// line between them. All four register only over the records
		// seam (V13) — an unwired mount classifies methods it does not
		// carry, exactly as the mcp.mine five do.
		MethodLearningsMineQuery: {
			Kind:    SurfaceRead,
			Summary: "The agent's own readable set — person word, its projects, its own memory — merged, tag-filtered, rankable (S52 T4).",
		},
		MethodLearningsMineShow: {
			Kind:    SurfaceRead,
			Summary: "One learning by id inside the readable set; out-of-scope answers exactly like nonexistent (S52 LM5's no-oracle law).",
		},
		MethodLearningsMineAdd: {
			Kind:    SurfaceWrite,
			Summary: "The agent's own filing: observed or assumed, scope STAMPED, bounded, capped per agent off the audit ledger (S52 T4).",
		},
		MethodLearningsMineProposeRetract: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert learning_retract ask for a live memory this agent can see; only a person's commit retracts, as the committer (S52 LM8).",
		},
		MethodPlatformChanges: {
			Kind:          SurfaceRead,
			HumanDoorOnly: true,
			Summary:       "Reads the derived-store operation ledger, newest first: who started a reindex or graph act, what it started, when (S54).",
		},
		MethodPlatformStatus: {
			Kind:    SurfaceRead,
			Summary: "The composed platform panel — retrieval, graph, usage — each block read through its own subsystem's seam, unwired stated as such (S54).",
		},
		MethodQueueAdvance: {
			Kind:    SurfaceControl,
			Summary: "Materializes the next queue item explicitly \u2014 asking it IS the human decision.",
		},
		MethodQueueAppend: {
			Kind:    SurfaceWrite,
			Summary: "Appends an item to the component dispatch queue, ordered by priority then arrival.",
		},
		MethodQueueList: {
			Kind:    SurfaceRead,
			Summary: "Lists a queue: pending, running and done, with the size cap in force.",
		},
		MethodQueueRemove: {
			Kind:    SurfaceWrite,
			Summary: "Removes a pending item; a running one is refused rather than interrupted.",
		},
		MethodQueueReorder: {
			Kind:    SurfaceWrite,
			Summary: "Reorders the pending items explicitly; the dispatcher follows the order, it does not invent one.",
		},
		MethodQueueSetSize: {
			Kind:    SurfaceWrite,
			Summary: "Sets the queue sizing cap for a component root.",
		},
		MethodQueueSettings: {
			Kind:    SurfaceWrite,
			Summary: "Reads a root's queue settings, and writes them only when the params carry BOTH knobs \u2014 half a pair refused.",
		},
		MethodRetrievalReindex: {
			Kind:          SurfaceWrite,
			Summary:       "Runs one deliberate indexing pass over a named corpus \u2014 nothing else may reach it, not startup, not a stale query.",
			HumanDoorOnly: true,
		},
		MethodRetrievalReindexPlan: {
			Kind:          SurfaceRead,
			HumanDoorOnly: true,
			Summary:       "Previews a rebuild's stale chunks, tokens and three-state cost — the priced refusal's own walk, no embedder call, no pass started (S54).",
		},
		MethodRetrievalStatus: {
			Kind:    SurfaceRead,
			Summary: "Availability and per-corpus staleness for a root; a status read that reindexed is the auto-fire this refuses.",
		},
		MethodRoadmapAdd: {
			Kind:          SurfaceWrite,
			Summary:       "Adds a roadmap item with its type and its basis, refused before any row if the shape is wrong.",
			HumanDoorOnly: true,
		},
		MethodRoadmapBoard: {
			Kind:    SurfaceRead,
			Summary: "Every item with its describe sentence, readiness answer, blocker, loop and epic group counts.",
		},
		MethodRoadmapCancel: {
			Kind:          SurfaceWrite,
			Summary:       "Cancels a roadmap item; a running materialization is left alone and the item stops being picked.",
			HumanDoorOnly: true,
		},
		// S27 T3 (CN7): the graph's two edges. SurfaceWrite and NOT
		// humanDoorOnly — deliberately, MethodTriageFileAs's mechanism: the
		// roadmap_depend/roadmap_undepend catalog twins reach these doors
		// through the tool door (registered, never default-granted), so an
		// absence claim here would be false. The LADDER writes keep theirs.
		MethodRoadmapDepend: {
			Kind: SurfaceWrite,
			// NOT humanDoorOnly — the roadmap_depend catalog twin reaches it (S27 T3).
			Summary: "Records that one item waits for another — an edge of intent, same repository only; cycles refused naming the closure.",
		},
		MethodRoadmapUndepend: {
			Kind: SurfaceWrite,
			// NOT humanDoorOnly — the roadmap_undepend catalog twin reaches it (S27 T3).
			Summary: "Removes exactly one dependency edge and nothing else — no cascade, no loop touched, no status moved.",
		},
		MethodRoadmapEstimate: {
			Kind:          SurfaceWrite,
			Summary:       "Records an estimate on an item, refused unless the range carries the basis that justifies it.",
			HumanDoorOnly: true,
		},
		MethodRoadmapHistory: {
			Kind:    SurfaceRead,
			Summary: "One item's audit trail, newest first: who moved it, when, in what words — the append-only rows read as they were written.",
		},
		MethodRoadmapIdeate: {
			Kind:          SurfaceControl,
			Summary:       "Runs ideation as an ordinary loop over the item, in the workspace it names.",
			HumanDoorOnly: true,
		},
		MethodRoadmapList: {
			Kind:    SurfaceRead,
			Summary: "Lists roadmap items with state, priority and estimate.",
		},
		MethodRoadmapMaterialize: {
			Kind:          SurfaceControl,
			Summary:       "Turns an item into work, ending at exactly one loop.start call carrying the blueprint and goal.",
			HumanDoorOnly: true,
		},
		MethodRoadmapPlan: {
			Kind:          SurfaceControl,
			Summary:       "Runs the planner: it reads the brief and writes plan.md where the item lives.",
			HumanDoorOnly: true,
		},
		MethodRoadmapPriority: {
			Kind:          SurfaceWrite,
			Summary:       "Sets an item priority, the queue order following it rather than the other way round.",
			HumanDoorOnly: true,
		},
		MethodRoadmapShow: {
			Kind:    SurfaceRead,
			Summary: "One roadmap item in full, with the runs its materialization produced.",
		},
		MethodRoadmapUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Edits an item name, epic and — before it starts — artifact pointers, audited once.",
			HumanDoorOnly: true,
		},
		MethodRoadmapHealth: {
			Kind:    SurfaceRead,
			Summary: "The two blueprint settings named, each ok, missing, invalid or unverifiable — a read, never a gate (RM6).",
		},

		// — the one agent door (S50 T5): a write by nature, never a door —
		MethodTriageFileAs: {
			Kind: SurfaceWrite,
			// NOT humanDoorOnly — deliberately: the bug_file catalog twin
			// reaches it through the tool door, so the absence claim would
			// be false; the human-door claim belongs to triage.file, whose
			// agent twin this is (S51 T5, TM6).
			Summary: "Files an UNVALIDATED, priority-unknown bug claim as the step's agent (filed_by the stamped slug, capped); a person uses triage.file (TM6).",
		},
		MethodRoadmapProposeIdea: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT proposal to ADD one roadmap idea; nothing stands on the board until a person commits it (RM8).",
		},
		MethodRoadmapVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The roadmap vocabulary: statuses with their legal moves, priority, provenance and path rules.",
		},
		MethodSearchQuery: {
			Kind:    SurfaceRead,
			Summary: "Searches a named corpus through the same resolver the search tool calls \u2014 the person's door to what a step can do.",
		},
		MethodTelemetryComponent: {
			Kind:    SurfaceRead,
			Summary: "Component-level counting across its loops, the primary surface for what a component cost.",
		},
		MethodTelemetryLoop: {
			Kind:    SurfaceRead,
			Summary: "A loop rollup: turns, tokens, compactions and tool activity as the capture points recorded them.",
		},
		MethodTelemetryProject: {
			Kind:    SurfaceRead,
			Summary: "Project counting, with the honest partial and unknown states kept distinct from a number.",
		},
		MethodTelemetryWorkspace: {
			Kind:    SurfaceRead,
			Summary: "Workspace counting for the whole workspace, same three-state honesty.",
		},
		MethodTriageBoard: {
			Kind:    SurfaceRead,
			Summary: "The backlog whole: bugs, two state sentences each, attempts, scored candidates, hand-off status — a read that writes nothing.",
		},
		MethodTriageConfirmDuplicate: {
			Kind:          SurfaceWrite,
			Summary:       "Marks a bug a duplicate of another, keeping both rows and the link between them.",
			HumanDoorOnly: true,
		},
		MethodTriageFile: {
			Kind:    SurfaceWrite,
			Summary: "Files a bug with its component, priority and reporter, validated before any row exists.",
		},
		MethodTriageHandoff: {
			Kind:          SurfaceControl,
			Summary:       "Hands a validated bug off to work, the same door the CLI handoff uses.",
			HumanDoorOnly: true,
		},
		MethodTriageHealth: {
			Kind:    SurfaceRead,
			Summary: "The standing of triage.validationBlueprint: effective name, state, analyzer counts; advice, never a gate.",
		},
		MethodTriageHistory: {
			Kind:    SurfaceRead,
			Summary: "One bug's audit history newest-first: the acts, their fixed summaries, and the actor each belonged to.",
		},
		MethodTriageList: {
			Kind:    SurfaceRead,
			Summary: "Lists the filed bugs with their component, priority, state and reporter.",
		},
		MethodTriageSetPriority: {
			Kind:          SurfaceWrite,
			Summary:       "Sets a bug priority and its state, an unknown priority refused by name.",
			HumanDoorOnly: true,
		},
		MethodTriageShow: {
			Kind:    SurfaceRead,
			Summary: "One filed bug in full, with its validation state and its duplicate links.",
		},
		MethodTriageSuggest: {
			Kind:    SurfaceRead,
			Summary: "Lists the bugs that look like this one, ranked, without deciding anything.",
		},
		MethodTriageUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Edits a bug's title and body — the only editable fields (TM1) — audited, an edit of nothing earning no row.",
			HumanDoorOnly: true,
		},
		MethodTriageValidate: {
			Kind:          SurfaceWrite,
			Summary:       "Runs validation over a bug and records its state, the refusal naming what failed.",
			HumanDoorOnly: true,
		},
		MethodTriageVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed sets as their owners answer them: validation states, priority states, and the legal transitions — never restated.",
		},
	}
}
