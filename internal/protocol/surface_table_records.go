// The records plane of the surface table (S47 T6): every dot-named catalog action whose
// human twin is a socket method. The kind of each one is the records spec own Mutates, and
// TestSurfaceKindAgreesWithKnownFacts proves the table against that fact rather than
// trusting the typing.
package protocol

// surfaceTableRecords is one slice of the closed classification table, built fresh on every
// call: there is no package-level map to append to, so a method cannot enter the surface
// by mutating shared state — only by editing a table, which the completeness tests then
// check both ways.
func surfaceTableRecords() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodName("attachment.attach"): {
			Kind:    SurfaceWrite,
			Summary: "Pin a loop document REV to a goal, brief, epic or task of this root.",
		},
		MethodName("attachment.list"): {
			Kind:    SurfaceRead,
			Summary: "List a record's attachments with each pinned rev and its currency: current, superseded or missing.",
		},
		MethodName("brief.create"): {
			Kind:    SurfaceWrite,
			Summary: "File a new brief VERSION for THIS step's loop (rewrites never overwrite).",
		},
		MethodBriefDecide: {
			Kind:          SurfaceWrite,
			Summary:       "A person decides a proposed brief; the board read is composed from rows, never stored.",
			HumanDoorOnly: true,
		},
		MethodName("brief.show"): {
			Kind:    SurfaceRead,
			Summary: "Show the current brief of THIS step's loop: scope, out-of-scope, acceptance criteria, version.",
		},
		MethodComponentArchive: {
			Kind:    SurfaceWrite,
			Summary: "Archive a component and its tree: hidden from listings and refused for new work; telemetry and history stay.",
		},
		MethodComponentAttach: {
			Kind:    SurfaceWrite,
			Summary: "Make a childless root a child of another root: it keeps its own settings as overrides; ids, loops and history stay.",
		},
		MethodComponentCreate: {
			Kind:    SurfaceWrite,
			Summary: "Create a component as a child of a root component, registering its workspace root.",
		},
		MethodComponentDetach: {
			Kind:    SurfaceWrite,
			Summary: "Take a child back to being a standalone root; its inherited settings become its own (materialized); ids, loops and history stay.",
		},
		MethodComponentList: {
			Kind:    SurfaceRead,
			Summary: "List components, optionally only those of one root group (its component and its descendants).",
		},
		MethodComponentPromote: {
			Kind:    SurfaceWrite,
			Summary: "Turn a standalone component into a project: create a new root and make this component its first child; ids, loops and history stay.",
		},
		MethodComponentRestore: {
			Kind:    SurfaceWrite,
			Summary: "Restore an archived component and its tree to live; the archive was hidden, not deleted.",
		},
		MethodComponentShow: {
			Kind:    SurfaceRead,
			Summary: "Show one component by id: name, workspace root, descriptions, branch settings.",
		},
		MethodComponentUpdate: {
			Kind:    SurfaceWrite,
			Summary: "Update a component's name or descriptions.",
		},
		MethodName("epic.create"): {
			Kind:    SurfaceWrite,
			Summary: "Create an epic in a root group: a funded thread of goals with an owner and a target date.",
		},
		MethodName("epic.list"): {
			Kind:    SurfaceRead,
			Summary: "List the epics of a root group (or every epic), newest first: title, owner, target date.",
		},
		MethodName("epic.show"): {
			Kind:    SurfaceRead,
			Summary: "Show one epic by id: title, objective, owner, target date, budget so far.",
		},
		MethodName("epic.update"): {
			Kind:    SurfaceWrite,
			Summary: "Update an epic's title, objective, owner or target date.",
		},
		MethodName("goal.create"): {
			Kind:    SurfaceWrite,
			Summary: "Create a goal FROM THIS STEP'S LOOP: filed in_review for a person to approve.",
		},
		MethodGoalDecide: {
			Kind:          SurfaceWrite,
			Summary:       "A person decides a proposed goal \u2014 approve, reject or return it; an agent's twin can only propose.",
			HumanDoorOnly: true,
		},
		MethodGoalList: {
			Kind:    SurfaceRead,
			Summary: "List goals with their review state and version, filtered by root group or epic.",
		},
		MethodGoalShow: {
			Kind:    SurfaceRead,
			Summary: "Show one goal at a version (head by default): fields, stake components, attached documents.",
		},
		MethodName("goal.submit_review"): {
			Kind:    SurfaceWrite,
			Summary: "Move a DRAFT goal of this loop to in_review, so a person can decide it.",
		},
		MethodName("goal.update"): {
			Kind:    SurfaceWrite,
			Summary: "Update this loop's own goal by id: a material edit bumps the version and reopens review.",
		},
		MethodName("loop.get"): {
			Kind:    SurfaceRead,
			Summary: "Show one loop by slug in this component: status, phase, blueprint, branch facts.",
		},
		MethodName("task.bump_counter"): {
			Kind:    SurfaceWrite,
			Summary: "Bump one of a task's three counters \u2014 retries, security rounds, review rounds \u2014 on a task this loop owns.",
		},
		MethodName("task.create"): {
			Kind:    SurfaceWrite,
			Summary: "Create a task owned by THIS step's loop.",
		},
		MethodTaskDecide: {
			Kind:          SurfaceWrite,
			Summary:       "A person decides a staged task; the review that gates it is a human act, not an agent one.",
			HumanDoorOnly: true,
		},
		MethodTaskList: {
			Kind:    SurfaceRead,
			Summary: "The board read: tasks filtered by component, status, manual, assignee, goal, parent.",
		},
		MethodName("task.set_status"): {
			Kind:    SurfaceWrite,
			Summary: "Move a task this loop owns (or a manual task this loop works) along the ladder.",
		},
		MethodName("task.show"): {
			Kind:    SurfaceRead,
			Summary: "Show one task by its id (the tsk- surrogate): status, assignee, type, dependencies, loops working it.",
		},
		MethodName("task.update"): {
			Kind:    SurfaceWrite,
			Summary: "Update a task this loop owns: title, tech or dependencies.",
		},
	}
}
