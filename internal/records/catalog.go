package records

import "encoding/json"

// Catalog is the management surface (S38 T9's list; T0-Q9 settled the
// boundary, and S45 PM1/PM10 amended the sentence — amended 2026-10-30, the
// test that makes it provable is cmd/aeon/policy_amend_test.go's
// TestAuthoringNeverReachesTheNarrowOrAgentSurfaces/RecordsCatalog): no
// commit/reject/add/remove/seed/decide/accept/approve/trigger action exists
// here; policy and proposal READS and PROPOSE-ONLY actions are permitted
// (S45), each restrictive-only. What T0-Q9 ruled stands — the human doors
// are reached from the socket's human half, never from a granted agent; what
// was amended is only the wholesale claim that the policy and proposal
// FAMILIES were absent here, which stopped being true when the reads landed
// (T2/T3/T5) and stops being true of the propose twins when T7 adds them
// (`policy.proposeAdd`/`policy.proposeRemove`: inert rows a human decides at
// the socket's door). A MUTATING spec in these families that is not one of
// those two twins is this catalog lying about itself — the guard fails, it
// is not a TODO. Task actions join in T10. The S56 `autonomy` family obeys
// the same boundary: the `autonomy` reads and the two propose-only,
// restrictive twins (`autonomy.proposeRevoke`, `autonomy.proposeDeny`) are
// permitted; no grant/deny/revoke action exists here — the teeth are
// cmd/aeon/autonomy_guard_test.go's
// TestAutonomyNeverReachesTheNarrowOrAgentSurfaces/RecordsCatalog.
// The S55 `loopcontrol` / `budget` / `watchdog` families obey the
// same boundary: NO commit/reject/stop/cancel/resume/raise/
// budget-write/watchdog-write action exists here — those are human
// doors, reached from the socket's human half, never from a granted
// agent; loop-control and budget READS and the single stop REQUEST
// join in S55 T10, the request inert by construction (LC15) — an
// ask before it is a row.
// The S62 `backup` family obeys the same boundary: (S62) backup/export
// *reads* are permitted, restore never — `backup_status` joins at T6 and
// `export_sets` at T8, both Mutates:false; no run/verify/prune action
// exists here (human doors), and no restore action will (BK9 — the CLI's
// direct door only).
// The S57 `verify` / `mergerule` / `dod` families obey the same boundary
// (T9): no add/update/archive/restore/applyTemplate/try/set/clear
// action exists here — the gate plane's nine writes are human doors,
// reached from the socket's human half, never from a granted agent; what
// is permitted is the gate and merge-rule READS, the PROPOSE-ONLY /
// TIGHTENING actions (`verify.proposeAdd`, `mergerule.proposeTighten` —
// inert rows a person commits at the socket's door, S35 TD3) and the
// EVIDENCE-BOUND `dod.markAsAgent` (met only from a passing run of the
// caller's own loop on the current head). The teeth are
// cmd/aeon/verify_amend_test.go's
// TestGateAuthoringNeverReachesTheNarrowOrAgentSurfaces/RecordsCatalog.
// None of the nine is granted by default (behaviour 7): a handler that
// wants one lists it, and the policy's tool_access rows still decide
// per call.
//
// Reads are safe (a crash may re-run them); writes are detectable (a crash
// recovery may repeat them only by re-checking, which every one of them
// survives — the store's claimed-FROM and unique guards make a replay a
// refusal, not a duplicate).
func Catalog() []Spec {
	// SPLIT 2026-11-04 (S60 T9 landing merge, SIZE1): the literal passed
	// 1,000 lines with the memory/history/merge-queue families landing
	// from three slices, so the families moved to their own files —
	// `catalog_memory.go`, `catalog_history.go`, `catalog_mergequeue.go`
	// — one concern per file, entries byte-identical. What stays here is
	// the base surface every reader opens plus the schema builders.
	// Measured after the split (wc -l): catalog.go 903 · catalog_memory.go
	// 54 · catalog_history.go 36 · catalog_mergequeue.go 58.
	// S54 T4 (2026-11-04): catalog_platform.go joins (28) — the platform
	// family's one read twin, one concern per file like the three above.
	// S27 T8 (2026-11-05): catalog_scheduler.go joins — the scheduler
	// family's four read twins; catalog.go stood at 957 lines, and four
	// more specs would have pushed it past the 1,000-line rule, so the
	// family lands in its own file the way the four above did.
	specs := coreSpecs()
	specs = append(specs, memorySpecs()...)
	specs = append(specs, mergequeueSpecs()...)
	specs = append(specs, historySpecs()...)
	specs = append(specs, platformSpecs()...)
	specs = append(specs, schedulerSpecs()...)
	// S61 T8 (2026-11-05, GI17): catalog_intake.go joins — the goal-intake
	// family's seven (five safe reads + two inert propose asks); catalog.go
	// stood at 962 lines and seven more specs would have pushed it past the
	// 1,000-line rule, so the family lands in its own file the way the six
	// above did.
	specs = append(specs, intakePlaneSpecs()...)
	return specs
}

func coreSpecs() []Spec {
	return []Spec{
		// — components (OL1's work-unit tier) —
		{Name: "component_list", Method: "component.list", Family: "component", Class: "safe",
			Description: "List components, optionally only those of one root group (its component and its descendants). Ids, names, roots.",
			Schema:      obj(map[string]any{"root_id": str("filter to one root component id; empty lists every component")}, nil)},
		{Name: "component_show", Method: "component.show", Family: "component", Class: "safe",
			Description: "Show one component by id: name, workspace root, descriptions, branch settings.",
			Schema:      obj(map[string]any{"component_id": str("the component's id")}, []string{"component_id"})},
		{Name: "component_create", Method: "component.create", Family: "component", Class: "detectable", Mutates: true,
			Description: "Create a component as a child of a root component, registering its workspace root. The root is unique.",
			Schema:      obj(map[string]any{"name": str("display name"), "root": str("absolute workspace directory this component lives in"), "root_id": str("the parent root component to file under; empty makes an implicit root"), "short_description": str("one line"), "description": str("the longer prose")}, []string{"name", "root"})},
		{Name: "component_update", Method: "component.update", Family: "component", Class: "detectable", Mutates: true,
			Description: "Update a component's name or descriptions. Empty fields keep their current values (§4c keep-semantics).",
			Schema:      obj(map[string]any{"component_id": str("the component to change"), "name": str("new name, empty keeps current"), "short_description": str("new one-liner, empty keeps current"), "description": str("new prose, empty keeps current")}, []string{"component_id"})},
		{Name: "component_promote", Method: "component.promote", Family: "component", Class: "detectable", Mutates: true,
			Description: "Turn a standalone component into a project: create a new root and make this component its first child; ids, loops and history stay.",
			Schema:      obj(map[string]any{"component_id": str("the standalone component to promote"), "name": str("the new root's name, empty keeps current"), "short_description": str("the new root's one-liner, empty keeps current"), "description": str("the new root's prose, empty keeps current"), "icon": str("the new root's icon, empty keeps current")}, []string{"component_id"})},
		{Name: "component_attach", Method: "component.attach", Family: "component", Class: "detectable", Mutates: true,
			Description: "Make a childless root a child of another root: it keeps its own settings as overrides; ids, loops and history stay.",
			Schema:      obj(map[string]any{"component_id": str("the childless root to attach"), "parent_id": str("the root to make it a child of")}, []string{"component_id", "parent_id"})},
		{Name: "component_detach", Method: "component.detach", Family: "component", Class: "detectable", Mutates: true,
			Description: "Take a child back to being a standalone root; its inherited settings become its own (materialized); ids, loops and history stay.",
			Schema:      obj(map[string]any{"component_id": str("the child to detach")}, []string{"component_id"})},
		{Name: "component_archive", Method: "component.archive", Family: "component", Class: "detectable", Mutates: true,
			Description: "Archive a component and its tree: hidden from listings and refused for new work; telemetry and history stay. Refused with children.",
			Schema:      obj(map[string]any{"component_id": str("the component to archive")}, []string{"component_id"})},
		{Name: "component_restore", Method: "component.restore", Family: "component", Class: "detectable", Mutates: true,
			Description: "Restore an archived component and its tree to live; the archive was hidden, not deleted. Needs the parent restored first.",
			Schema:      obj(map[string]any{"component_id": str("the archived component to restore")}, []string{"component_id"})},

		// — epics (OL6) —
		{Name: "epic_list", Method: "epic.list", Family: "epic", Class: "safe",
			Description: "List the epics of a root group (or every epic), newest first: title, owner, target date.",
			Schema:      obj(map[string]any{"root_id": str("filter to one root component; empty lists every epic")}, nil)},
		{Name: "epic_show", Method: "epic.show", Family: "epic", Class: "safe",
			Description: "Show one epic by id: title, objective, owner, target date, budget so far.",
			Schema:      obj(map[string]any{"epic_id": str("the epic's id")}, []string{"epic_id"})},
		{Name: "epic_create", Method: "epic.create", Family: "epic", Class: "detectable", Mutates: true,
			Description: "Create an epic in a root group: a funded thread of goals with an owner and a target date.",
			// S55 T8 (LC13): no budget field here — the epic's budget is a
			// budget row (scope epic), set only through the budget plane's
			// person door, never an epic field. A budget figure arriving is
			// refused as unknown by the decoder.
			Schema: obj(map[string]any{"root_id": str("the root component the epic hangs on"), "title": str("short headline"), "objective": str("what done looks like"), "owner": str("who answers for it"), "target_date": str("YYYY-MM-DD")}, []string{"root_id", "title"})},
		{Name: "epic_update", Method: "epic.update", Family: "epic", Class: "detectable", Mutates: true,
			Description: "Update an epic's title, objective, owner or target date. Empty fields keep current values. The epic's budget is a budget row set through the budget plane's person door, not an epic field.",
			// S55 T8 (LC13): the budget moved to the budget table — this
			// action no longer names a money figure at all.
			Schema: obj(map[string]any{"epic_id": str("the epic to change"), "title": str("new headline"), "objective": str("new objective"), "owner": str("new owner"), "target_date": str("YYYY-MM-DD")}, []string{"epic_id"})},

		// — goals (OL7: everything an agent proposes passes review) —
		{Name: "goal_list", Method: "goal.list", Family: "goal", Class: "safe",
			Description: "List goals with their review state and version, filtered by root group or epic.",
			Schema:      obj(map[string]any{"root_id": str("filter to one root component"), "epic_id": str("filter to one epic"), "state": str("draft, in_review or approved")}, nil)},
		{Name: "goal_show", Method: "goal.show", Family: "goal", Class: "safe",
			Description: "Show one goal at a version (head by default): fields, stake components, attached documents.",
			Schema:      obj(map[string]any{"goal_id": str("the goal's id"), "version": num("which version; omit for the current one")}, []string{"goal_id"})},
		{Name: "goal_create", Method: "goal.create", Family: "goal", Class: "detectable", Mutates: true,
			Description: "Create a goal FROM THIS STEP'S LOOP: filed in_review for a person to approve. Root, source loop/step and author are stamped, never arguments.",
			Schema: obj(map[string]any{
				"title":     str("what the goal is"),
				"type":      enum("bug|feature|chore|refactor|investigation", "bug", "feature", "chore", "refactor", "investigation"),
				"body_note": str("one-paragraph statement of the goal"),
				"epic_id":   str("the epic it serves, if any"),
				"document":  str("a loop document (path under the allowed dirs) attached as the goal's prose"),
				"components": arr(obj(map[string]any{
					"component":    str("a component of THIS root — its id, or its exact name"),
					"rationale":    str("why this component is at stake"),
					"estimate_usd": num("estimate for this component"),
					"confidence":   enum("low|medium|high", "low", "medium", "high"),
					"basis":        arr(str("one line of the estimate's basis"), ""),
				}, []string{"component"}), "at least one component must be at stake"),
			}, []string{"title", "type", "components"})},
		{Name: "goal_update", Method: "goal.update", Family: "goal", Class: "detectable", Mutates: true,
			Description: "Update this loop's own goal by id: a material edit bumps the version and reopens review. Empty fields keep current values.",
			Schema:      obj(map[string]any{"goal_id": str("the goal to change"), "title": str("new title"), "body_note": str("new statement"), "epic_id": str("new epic"), "reason": str("why the edit — lands in the review trail")}, []string{"goal_id"})},
		{Name: "goal_submit_review", Method: "goal.submit_review", Family: "goal", Class: "detectable", Mutates: true,
			Description: "Move a DRAFT goal of this loop to in_review, so a person can decide it. Never approves anything.",
			Schema:      obj(map[string]any{"goal_id": str("the goal to submit")}, []string{"goal_id"})},

		// — briefs (OL8: the contract a loop works to) —
		{Name: "brief_show", Method: "brief.show", Family: "brief", Class: "safe",
			Description: "Show the current brief of THIS step's loop: scope, out-of-scope, acceptance criteria, version.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "brief_create", Method: "brief.create", Family: "brief", Class: "detectable", Mutates: true,
			Description: "File a new brief VERSION for THIS step's loop (rewrites never overwrite). The old version keeps meaning.",
			Schema: obj(map[string]any{
				"goal_id":             str("the goal this brief contracts to, if known"),
				"scope":               str("what this loop is for — required, the brief's spine"),
				"out_of_scope":        str("what it is explicitly not"),
				"acceptance_criteria": arr(str("one testable criterion"), "how a person judges done"),
				"constraints":         arr(str("one constraint"), "rules the work must respect"),
				"test_plan":           str("how correctness is shown"),
				"document":            str("a loop document attached as the brief's prose"),
			}, []string{"scope"})},

		// — attachments (OL4: pointers to documents, never copies) —
		{Name: "attach", Method: "attachment.attach", Family: "attachment", Class: "detectable", Mutates: true,
			Description: "Pin a loop document REV to a goal, brief, epic or task of this root. The record keeps the pointer, never a copy.",
			Schema: obj(map[string]any{
				"subject": obj(map[string]any{
					"kind": enum("goal|brief|epic|task", "goal", "brief", "epic", "task"),
					"id":   str("the record's id"),
				}, []string{"kind", "id"}),
				"document": str("the loop document to pin, by name under the allowed dirs"),
			}, []string{"subject", "document"})},
		{Name: "attachments_list", Method: "attachment.list", Family: "attachment", Class: "safe",
			Description: "List a record's attachments with each pinned rev and its currency: current, superseded or missing.",
			Schema: obj(map[string]any{
				"subject": obj(map[string]any{
					"kind": enum("goal|brief|epic|task", "goal", "brief", "epic", "task"),
					"id":   str("the record's id"),
				}, []string{"kind", "id"}),
			}, []string{"subject"})},

		// — tasks (S38 T10: the legacy task_* surface, scoped; the engine's
		// ladder stays the machine's — these arms run THROUGH it, never around it) —
		{Name: "task_list", Method: "task.list", Family: "task", Class: "safe",
			Description: "The board read: tasks filtered by component, status, manual, assignee, goal, parent. Costs floor-flagged.",
			Schema: obj(map[string]any{
				"component": str("filter to one component id"),
				"status":    str("one ladder status"),
				"manual":    map[string]any{"type": "boolean", "description": "true = hand-made tasks only, false = loop tasks only, absent = both"},
				"assignee":  str("the assignee's name"),
				"goal":      str("tasks serving one goal id"),
				"parent":    str("children of one parent task id"),
				"limit":     num("rows to return; default 200, max 1000"),
			}, nil)},
		{Name: "task_show", Method: "task.show", Family: "task", Class: "safe",
			Description: "Show one task by its id (the tsk- surrogate): status, assignee, type, dependencies, loops working it.",
			Schema:      obj(map[string]any{"id": str("the task's surrogate id")}, []string{"id"})},
		{Name: "task_create", Method: "task.create", Family: "task", Class: "detectable", Mutates: true,
			Description: "Create a task owned by THIS step's loop. Loop tasks carry the plan's id, tech, engineer and dependencies; manual tasks exist only in a plain chat loop.",
			Schema: obj(map[string]any{
				"id":         str("the task's plan id (T1, T2…); derived when omitted"),
				"title":      str("what the task does — required"),
				"tech":       str("the tech tag that routes an engineer"),
				"engineer":   str("the assigned engineer; the step's agent when omitted"),
				"depends_on": arr(str("a task id in this loop that already exists"), "gates completion until each is done"),
				"parent":     str("a parent task id in this component (decomposition)"),
				"manual":     map[string]any{"type": "boolean", "description": "true ONLY in a plain chat loop — everywhere else a task belongs to its loop"},
				"type":       enum("bug|feature|chore|refactor|investigation — manual tasks only", "bug", "feature", "chore", "refactor", "investigation"),
				"body_note":  str("a manual task's body prose (attached as its document)"),
			}, []string{"title"})},
		{Name: "task_update", Method: "task.update", Family: "task", Class: "detectable", Mutates: true,
			Description: "Update a task this loop owns: title, tech or dependencies. The owning agent's door; people read loop tasks.",
			Schema: obj(map[string]any{
				"id":         str("the task's surrogate id"),
				"title":      str("new title, empty keeps current"),
				"tech":       str("new tech tag, empty keeps current"),
				"depends_on": arr(str("a task id in this loop that already exists"), "REPLACES the list; nil keeps it"),
			}, []string{"id"})},
		{Name: "task_set_status", Method: "task.set_status", Family: "task", Class: "detectable", Mutates: true,
			Description: "Move a task this loop owns (or a manual task this loop works) along the ladder. The machine decides; evidence records what was shown.",
			Schema: obj(map[string]any{
				"id":       str("the task's surrogate id"),
				"status":   str("the ladder status to move to"),
				"evidence": str("what was shown at this rung (tests run, review done)"),
			}, []string{"id", "status"})},
		{Name: "task_bump_counter", Method: "task.bump_counter", Family: "task", Class: "detectable", Mutates: true,
			Description: "Bump one of a task's three counters — retries, security rounds, review rounds — on a task this loop owns.",
			Schema: obj(map[string]any{
				"id":      str("the task's surrogate id"),
				"counter": enum("which counter", "retries", "security_rounds", "review_rounds"),
			}, []string{"id", "counter"})},

		// — loops (read-only here: the engine's rows, OL14's sentence) —
		{Name: "loop_list", Method: "loop.list", Family: "loop", Class: "safe",
			Description: "List the loops this agent's component works: slug, title, status, phase. Reading, never steering.",
			Schema:      obj(map[string]any{"root_id": str("filter to one root group")}, nil)},
		{Name: "loop_get", Method: "loop.get", Family: "loop", Class: "safe",
			Description: "Show one loop by slug in this component: status, phase, blueprint, branch facts. Read-only.",
			Schema:      obj(map[string]any{"slug": str("the loop's slug in this component")}, []string{"slug"})},
		// S39 T10 (CR5): the ONE loop action an agent may take, and it is an
		// ask before it is a row — the person is asked to confirm the start.
		// Two doors, one row: the socket twin loop.run is the person's door
		// and carries no ask (a person typing is the confirmation, CR4).
		{
			Name: "run_start", Method: "loop.run", Family: "loop",
			Description: "Start a blueprint as a new run inside this conversation's loop. Call it only when the person asked for the work. " +
				"The person is asked to confirm before it starts. Returns the new run, or the sentence saying what is missing and the goals or tasks that could satisfy it.",
			Schema: json.RawMessage(`{"type":"object","properties":{"blueprint":{"type":"string","description":"blueprint name, e.g. goal-to-brief"},"goal":{"type":"string","description":"goal id to run on"},"task":{"type":"string","description":"task id to run on"},"with":{"type":"object","additionalProperties":{"type":"string"},"description":"named parameters for the blueprint"}},"required":["blueprint"],"additionalProperties":false}`),
			Class:  "detectable", Mutates: true,
		},
		// S43-extra T6 (SD10): the ONE seed tool, and it is a READ. No tool
		// writes a seed — a seed write is a human door, never a tool. It reads
		// through the same status the socket answers, so the two cannot drift
		// (the parity test pins it field for field). Registered, never granted
		// by default (DefaultGrant unchanged).
		{Name: "seed_status", Method: "seed.status", Family: "seed", Class: "safe",
			Description: "Read the seed ledger and status for a set (or every set): the applied version and the per-record state. A read; a seed write is a human door, never a tool.",
			Schema:      obj(map[string]any{"set": str("the set id; empty lists every set")}, nil)},

		// — policy & proposals (S45 T7, PM9): agents SEE the policy that
		// governs them and PROPOSE that it be tightened — restrictive-only,
		// and nothing more. Each of these seven is registered, not granted
		// (S11's rule inherited whole): tools.DefaultGrant() names none of
		// them, a handler that wants them lists them explicitly, and even
		// then the policy `tool_access` rows decide every call (ask/deny/
		// allow) as for any tool. No commit, reject, discard, add, remove
		// or seed action exists here or can — a proposal is an INERT row
		// (S35 TD2): nothing is enforced until a person commits it at the
		// socket's human door. A propose schema carries no source, actor,
		// createdBy, status or kind field (OL3): the author is stamped by
		// the records service from the step, and a policy row created here
		// is forced to source "agent", so PL1 refuses any allow. The five
		// reads answer through the SAME policy.Store/ProposalStore the
		// socket reads use — one implementation, two doors (the parity
		// tests pin both directions every run). —
		// policy_list's filters are QUESTIONS, never identity — the same
		// filter set policy.list carries (S45 T2, PM5). Registered, not
		// granted; the policy tool_access rows judge each call.
		{Name: "policy_list", Method: "policy.list", Family: "policy", Class: "safe",
			Description: "List the policy rules that govern agents: type, target, action, authoring source, origin (system/user), expiry and the one-sentence description of what each rule does. Optional type/target/source/action/origin filters; the target filter speaks the `kind:name` spelling.",
			Schema: obj(map[string]any{
				"type":   str("filter to one policy type"),
				"target": str("filter to one target in its CLI spelling: agent:NAME, loop:SLUG, lane-type:NAME, mcp:NAME, or all-agents"),
				"source": str("filter to one authoring source"),
				"action": str("filter to one action (allow, deny or ask)"),
				"origin": str("filter to one provenance: system, user or import"),
			}, nil)},
		// policy_show is a read of one whole row — the raw spec rides here,
		// never on the list. Registered, not granted; judging is policy's.
		{Name: "policy_show", Method: "policy.get", Family: "policy", Class: "safe",
			Description: "Show one policy in full by its id: the row, its one-sentence description and the raw spec it carries. A read — a policy is written only at the socket's human door, never by this tool.",
			Schema:      obj(map[string]any{"id": str("the policy's id")}, []string{"id"})},
		// policy_why answers the question the engine decides with — the
		// verdict sentence VERBATIM (D9: one formatter). Registered, not
		// granted; the call itself is judged by tool_access as any tool.
		{Name: "policy_why", Method: "policy.why", Family: "policy", Class: "safe",
			Description: "Ask why a question is allowed, denied or asked: name the policy type and the party (agent/loop/lane-type/mcp) and the thing asked about (tool, command, route, effect, cost, skill, trigger); the verdict sentence comes back verbatim, exactly as a decision prints it. Reads only — a refusal by why has not happened.",
			Schema: obj(map[string]any{
				"type":     str("the policy type the question is asked under — required, and naming it matters (tool_access and command_access are the two real evaluators)"),
				"agent":    str("the agent the question is about"),
				"loop":     str("the loop the question is about"),
				"laneType": str("the lane type the question is about"),
				"mcp":      str("the MCP server the question is about"),
				"tool":     str("the tool name, for a tool_access question"),
				"command":  str("the command string, for a command_access question"),
				"route":    str("the route, for a route_access question"),
				"effect":   str("the effect, for an effect_class question"),
				"cost":     str("the cost, for a cost_cap question"),
				"skill":    str("the skill, for a skill_access question"),
				"trigger":  str("the trigger, for a trigger_access question"),
				"at":       str("answer as of this RFC3339 instant (expiry exploration); absent means now"),
			}, []string{"type"})},
		// policy_in_force renders the three states un-collapsed (D14) — the
		// same InForceAnswer.Render() sentences, verbatim. Registered, not
		// granted; judging is policy's.
		{Name: "policy_in_force", Method: "policy.inForce", Family: "policy", Class: "safe",
			Description: "What is in force for one party — exactly one of agent or loop: one answer per governing rule, the three states un-collapsed (acknowledged, unacknowledged-still-enforced, could-not-determine), each sentence verbatim from the engine.",
			Schema: obj(map[string]any{
				"agent": str("the agent whose rules to list"),
				"loop":  str("the loop whose rules to list; give one of agent/loop, never both"),
			}, nil)},
		// proposal_list reads the staging table — status filter, newest
		// first. Discarding, committing and rejecting stay the person's
		// doors (this catalog carries no such action; see the header).
		{Name: "proposal_list", Method: "proposal.list", Family: "proposal", Class: "safe",
			Description: "List staged proposals, newest first, with an optional status filter (proposed, committed, rejected, discarded): kind, raw payload, author and — once decided — the decider and reason. A read; deciding a proposal is a human door, never a tool.",
			Schema: obj(map[string]any{
				"status": str("filter to one status; empty lists every proposal"),
			}, nil)},
		// policy_propose_add creates an INERT row through the same
		// create path as proposal.create (validator first, store second).
		// The schema carries no source/actor/createdBy/status/kind field:
		// the service forces source "agent" — so PL1 refuses any allow
		// with its own sentence — and stamps created_by from the calling
		// agent; the tool never names either. An agent may hold at most
		// desk.MaxOpenProposalsPerAgent open proposals and each payload is
		// capped, so a runaway loop cannot bury the desk. Registered, not
		// granted; even when granted, the policy tool_access rows judge
		// every call.
		{Name: "policy_propose_add", Method: "policy.proposeAdd", Family: "policy", Class: "detectable", Mutates: true,
			Description: "Propose that a deny or ask policy rule be added, for a person to decide: nothing is enforced until a human commits the inert row. Only deny or ask can be proposed — an allow is not a representable input (PL1); reason is required, identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"id":   str("the id the proposed rule would carry — an authored name, not generated"),
				"type": str("the policy type from the closed set (aeon policy vocabulary lists them; tool_access and command_access are the two real evaluators)"),
				"target": obj(map[string]any{
					"kind": enum("the target kind from the closed set", "all-agents", "agent", "loop", "lane-type", "mcp"),
					"id":   str("the target's name; empty only for kind all-agents"),
				}, []string{"kind"}),
				"action":   enum("the proposed action — deny or ask ONLY; allow is unrepresentable here and refused by PL1 if smuggled", "deny", "ask"),
				"spec":     map[string]any{"type": "object", "description": "the rule's spec, per the type's closed shape (the boundary's own validator refuses a bad one at proposal time, not at commit)", "additionalProperties": true},
				"notAfter": str("expiry as an RFC3339 instant; absent means the rule never expires (PL7)"),
				"reason":   str("why the agent proposes this rule — required, 1 to 512 bytes; it rides in the proposal payload for the person deciding to read"),
			}, []string{"id", "type", "target", "action", "spec", "reason"})},
		// policy_propose_remove proposes a removal by id — also INERT until
		// a person commits; the validator refuses a proposal naming an id
		// that does not exist (TD3). Identity stamped, never typed; same
		// per-agent cap and payload ceiling as its sibling. Registered,
		// not granted; the policy tool_access rows judge every call.
		{Name: "policy_propose_remove", Method: "policy.proposeRemove", Family: "policy", Class: "detectable", Mutates: true,
			Description: "Propose that a policy be removed by id, for a person to decide: nothing changes until a human commits the inert row, and removing loosens — which is exactly why it still needs the human door. Reason is required; identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"id":     str("the id of the policy proposed for removal — it must exist, or the proposal is refused at create"),
				"reason": str("why the agent proposes the removal — required, 1 to 512 bytes; it rides in the proposal payload for the person deciding to read"),
			}, []string{"id", "reason"})},

		// S56 T8 (AL11, 2026-11-01): the autonomy family — the agent SEES
		// what autonomy exists and PROPOSES to narrow it, and nothing more.
		// The three reads reuse T6's standing methods (the dot-name twins
		// registerRecords skips, by the deviation recorded there); the two
		// proposes are this task's new socket methods, answered through
		// this same Do. No schema here carries an actor/createdBy/
		// proposedBy/status/kind/state/conditions field, and no agent-
		// reachable schema in the family carries the word allow anywhere:
		// the propose kinds are the two RESTRICTIVE ones (revoke a grant,
		// add a veto) — the kinds a grant could be are not representable.
		// Same discipline as the policy twins: reason 1–512 bytes,
		// per-agent open-proposal cap and 8 KiB payload bound (S45's named
		// constants), identity stamped by the service, never typed.
		// Registered, not granted; the policy tool_access rows judge every
		// call.
		{Name: "autonomy_list", Method: "autonomy.list", Family: "autonomy", Class: "safe",
			Description: "List the autonomy grants a person has given: the authority per root component, per registry action or the outward class — each grant's subject, conditions, expiry, who proposed it, and whether it stands or was revoked. Optional rootId filter; includeInactive carries revoked rows.",
			Schema: obj(map[string]any{
				"rootId":          str("filter to the grants on one root component; absent lists every root"),
				"includeInactive": boolProp("carry revoked (inactive) rows too; absent lists only the live grants"),
			}, nil)},
		{Name: "autonomy_show", Method: "autonomy.show", Family: "autonomy", Class: "safe",
			Description: "Show one autonomy grant in full by its id: the grant itself, its last change, the count of begun uses, and the running loops pinned with it. A read — grants are written only at the socket's human door, never by a tool.",
			Schema:      obj(map[string]any{"id": str("the grant's id — see it in autonomy_list")}, []string{"id"})},
		{Name: "autonomy_effective", Method: "autonomy.effective", Family: "autonomy", Class: "safe",
			Description: "What an unattended loop started now on a root could do, per registry action: the verdict and the one-sentence why-not, from the same plan the start path pins a loop with — zero seams, so an unanswerable case says so rather than passing. The labels are the start's label set the pin-time conditions match against.",
			Schema: obj(map[string]any{
				"rootId": str("the root component an unattended loop would start on — required"),
				"labels": arr(str("one start label the pin-time conditions would match against"), "the start's label set; absent means an unlabeled start"),
			}, []string{"rootId"})},
		{Name: "autonomy_propose_revoke", Method: "autonomy.proposeRevoke", Family: "autonomy", Class: "detectable", Mutates: true,
			Description: "Propose that an autonomy grant be revoked, for a person to commit: nothing changes until a human commits the inert proposal row. The grant id must exist and be live, and proposing to revoke a veto is refused — a veto is removed by a person, not proposed away. Reason required, 1 to 512 bytes; identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"id":     str("the grant proposed for revocation — it must exist, or the proposal is refused at create"),
				"reason": str("why the agent proposes the revocation — required, 1 to 512 bytes; it rides in the proposal payload for the person deciding to read"),
			}, []string{"id", "reason"})},
		{Name: "autonomy_propose_deny", Method: "autonomy.proposeDeny", Family: "autonomy", Class: "detectable", Mutates: true,
			Description: "Propose a veto on a root or a child for one registry action or the outward class, for a person to commit: nothing is enforced until a human commits the inert row. The subject is the two-shape vocabulary (an action by name, or the one class, named outward) — narrowing only; no input here can widen. Reason required, 1 to 512 bytes; identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"nodeId": str("the node the veto would sit on — a root component or one of its children"),
				"subject": obj(map[string]any{
					"kind": enum("the subject's shape — an action by its registry name, or the one class", "action", "class"),
					"name": str("the action's registry name for kind action; the one class's name (outward) for kind class"),
				}, []string{"kind", "name"}),
				"reason": str("why the agent proposes the veto — required, 1 to 512 bytes; it rides in the proposal payload for the person deciding to read"),
			}, []string{"nodeId", "subject", "reason"})},
		// ─── mcp — S46 T8 (MG8, MG9, MC2) ─────────────────────────────────
		// The agent's OWN view of its MCP situation and the narrowing-only
		// proposes. The socket twins are namespaced `mcp.mine.*` — agent-
		// scoped doors that refuse a person session with a pointer sentence,
		// so they cannot serve as an oracle for anyone but the stamped agent,
		// and the person's unscoped read doors (the S46 T3 five) stay a
		// separate path. Schemas carry NO agent/actor/source/status/kind
		// field: identity is stamped from the step, never typed (OL3). No
		// spec here adds, declares, enables, imports, edits or commits —
		// the two Mutates rows create INERT proposals that can only reduce
		// (MG9), and T7's guards watch the boundary.
		{Name: "mcp_servers", Method: "mcp.mine.servers", Family: "mcp", Class: "safe",
			Description: "List the MCP servers THIS agent may use: its own private servers plus the public ones it declares tools from — each with its transport summary (env NAMES only, never values) and its tools with their rerun/effect classes. A peer's private server does not exist here, not even in a count or an error.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "mcp_grants", Method: "mcp.mine.grants", Family: "mcp", Class: "safe",
			Description: "This agent's grant view: every tool of every server it may use, with its S12 state — available, unavailable, refused, or ask — and the one-sentence reason for the state. The view the gate judges with, served from the same rows; declaring is not granting (MC5), and a peer's grants are invisible.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "mcp_status", Method: "mcp.mine.status", Family: "mcp", Class: "safe",
			Description: "Reachability of THIS agent's MCP servers, read from the cached probes only — it never starts a probe, a process, or a connection. A server with no cached probe answers unchecked, which is its own honest state, not an assumed-down failure.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "mcp_propose_remove_server", Method: "mcp.mine.proposeRemove", Family: "mcp", Class: "detectable", Mutates: true,
			Description: "Propose that an MCP server be removed, for a person to commit: nothing changes until a human commits the inert proposal row. Only a server THIS agent can see — its own private, or a public one it declares — is proposable; an invisible name and a name that never existed get the same refusal, so the door cannot probe existence. Reason required, 1 to 512 bytes; identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"server": str("the server proposed for removal — must be one this agent can see; anything else is refused as no such server"),
				"reason": str("why the agent proposes the removal — required, 1 to 512 bytes; it rides in the proposal payload for the person deciding to read"),
			}, []string{"server", "reason"})},
		{Name: "mcp_propose_revoke_grant", Method: "mcp.mine.proposeRevoke", Family: "mcp", Class: "detectable", Mutates: true,
			Description: "Propose that a grant THIS agent holds be revoked, for a person to commit: nothing changes until a human commits the inert proposal row. The server.tool pair must be currently declared by this agent — a pair it does not hold and a pair nobody holds get the same refusal, and no input here can name a different agent. Reason required, 1 to 512 bytes; identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"server": str("the declared server half of the pair to revoke — the server this agent declares from"),
				"tool":   str("the declared tool half of the pair — the qualified name server.tool must be one this agent currently holds"),
				"reason": str("why the agent proposes the revocation — required, 1 to 512 bytes; it rides in the proposal payload for the person deciding to read"),
			}, []string{"server", "tool", "reason"})},
		// — tools (S47 T7, TC9): the agent's OWN tool situation, and nothing
		// else's. Two safe reads and no Mutates row — the slice adds no write
		// tool (TC1) — and the family is new here: `tools.grants` (S34) is a
		// socket method and never a catalog action, so this is the first pair in
		// it and `TestNoWriteToolInTheToolsFamily` is what keeps it that way.
		//
		// The schemas are `additionalProperties:false` with NO agent/actor
		// field: "self" is the identity stamped on the step (OL3), so no input
		// can name another agent — a smuggled one is a parse refusal at the
		// strict decode, and `TestMineSchemasCarryNoIdentity` counts the keys.
		//
		// A tool the agent was not given is ABSENT from the list, never listed
		// as "not granted", and a name outside the agent's own set gets the SAME
		// sentence as a name that never existed — with nearest-name hints drawn
		// only from the caller's own set. That is what stops these two doors
		// being an existence oracle for a peer's private MCP tool (MC2) or for a
		// grant an operator withheld. The person's unscoped reads (`tools.list` ·
		// `tools.get` · `tools.byAgent`) are a separate path, and
		// `personDoorRefusal` turns a person back from these two by name.
		{Name: "tools_mine", Method: "tools.mine.list", Family: "tools", Class: "safe",
			Description: "List the tools YOU hold: each name, its family, a one-line description, its declared capabilities, your effective state against the policy (granted_allowed, granted_ask, granted_denied or granted_no_rule) and the policy's own reason sentence. A tool you were not given is simply absent from this list — it is never listed as not granted — and another agent's tools or a peer's private MCP tools do not exist here, not even in a count or an error.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "tool_show", Method: "tools.mine.show", Family: "tools", Class: "safe",
			Description: "Show one of YOUR tools in full: its description, the input schema the model is already offered for it, its capabilities, its re-run class, the policy rows that could change whether YOU may call it (only the rows that can speak about this agent — never another agent's), and your state with the engine's reason. A name you do not hold and a name that never existed are refused with the identical sentence, so this door cannot probe what you were not given; nearest-name hints come from your own set only.",
			Schema: obj(map[string]any{
				"name": str("the tool to show — it must be one you hold; a name outside your set and a name that does not exist anywhere get the same refusal, and no input here can name a different agent"),
			}, []string{"name"})},
		// — loop control (S55 T10: reads + the one inert ask) —
		// The reads answer through the SAME service the socket's doors call
		// (one implementation, the parity test pins it field for field). The
		// state read is OWN-VIEW (MC2): the agent sees its own loop; another
		// loop's control state is a person's read through the socket.
		{Name: "loop_control_state", Method: "loop.control", Family: "loopcontrol", Class: "safe",
			Description: "THIS loop's own control state: whether stop, resume, cancel or a raise are eligible now (the raise verdict is the cap-hit one), the open stop requests, and the recent control events. Own loop only — a person reads any loop through the socket's loop.control.",
			Schema:      obj(map[string]any{}, nil)},
		// S55 T10 (LC15): the ONE write an agent may take on loop control,
		// and it is an ask before it is a row — nothing halts until a person
		// acts. The row is inert by construction, the agent is stamped from
		// the step (never an argument), and the desk is counted: 5 open per
		// loop, 20 per agent, the refusal names the limit and the door that
		// shows the open ones.
		{Name: "loop_request_stop", Method: "loop.requestStop", Family: "loopcontrol", Class: "detectable", Mutates: true,
			Description: "Ask a person to stop a loop: files an inert stop request a person sees and acts on (or dismisses). Nothing halts until the person acts. Omit loop_id to ask about this loop's own loop; another loop's id is refused.",
			Schema:      obj(map[string]any{"loop_id": str("the loop's id to ask about — this loop's own when omitted; any other id is refused"), "reason": str("why a person should stop it — required, 1 to 512 bytes")}, []string{"reason"})},

		// — budgets (S55 T10: the read plane only — setting, raising or
		// removing a cap is a human door, never a tool, OD10) —
		{Name: "budget_list", Method: "budget.list", Family: "budget", Class: "safe",
			Description: "List budgets (or those matching one scope, subject or status) with their live status, spend against each capped dimension, and the last change. A read; budget writes are a human door.",
			Schema: obj(map[string]any{
				"scope":      str("filter to one scope (workspace, component, root, epic or loop)"),
				"subject_id": str("filter to one subject id"),
				"enabled":    map[string]any{"type": "boolean", "description": "true = enabled only, false = disabled only, absent = both"},
				"status":     str("filter to one live status (ok, warn or hit)"),
			}, nil)},
		{Name: "budget_show", Method: "budget.show", Family: "budget", Class: "safe",
			Description: "Show one budget by id: caps, floor, the live spend per dimension, the status, the audit's last change, and the worst-case overshoot note. A read; budget writes are a human door.",
			Schema:      obj(map[string]any{"budget_id": str("the budget's id")}, []string{"budget_id"})},
		{Name: "budget_effective", Method: "budget.effective", Family: "budget", Class: "safe",
			Description: "The budgets that would apply to THIS loop's spend, tightest first (loop, epic, component, root, workspace) — the tiers a spend is checked against, listed in the order the engine checks them. A read; budget writes are a human door.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "budget_spend", Method: "spend.report", Family: "budget", Class: "safe",
			Description: "Spend over time or by trigger, component, epic or route: USD floors per bucket with the honesty marks (exact when the floor is a total, unknown when it is not), grouped as asked. A read.",
			Schema:      obj(map[string]any{"group_by": enum("the bucket grain", "day", "trigger", "component", "epic", "route"), "since": str("RFC3339 lower bound, inclusive"), "until": str("RFC3339 upper bound, exclusive")}, []string{"group_by"})},

		// — watchdog (S55 T10: the read twin only — editing, disabling or
		// removing a rule is a human door with a confirmation digest) —
		{Name: "watchdog_list", Method: "watchdog.list", Family: "watchdog", Class: "safe",
			Description: "List the watchdog rules (or those matching one scope, subject or enabled state) with their thresholds in human units, origin, last fire and fire count. A read; rule writes are a human door.",
			Schema: obj(map[string]any{
				"scope":      str("filter to one scope (workspace or component)"),
				"subject_id": str("filter to one subject id ('workspace' or a component id)"),
				"enabled":    map[string]any{"type": "boolean", "description": "true = enabled only, false = disabled only, absent = both"},
			}, nil)},

		// — backup (S62 T6: the read twin only — run/verify/prune are human
		// doors, and restore is the CLI's direct door and never a tool) —
		{Name: "backup_status", Method: "backup.status", Family: "backup", Class: "safe",
			Description: "Report whether the database is being backed up: last success, age, verification state, whether the schedule is on, and the current database size. A read; backup writes are a human door and restore is never a tool.",
			Schema:      obj(map[string]any{}, nil)},

		// S62 T8: the export's read twin — the run door (export.run) is a
		// human door, so only the set listing joins the catalog. It is open
		// (no initialize), data-only, and names no dir.
		{Name: "export_sets", Method: "export.sets", Family: "backup", Class: "safe",
			Description: "List the declared export sets in registry order with each one's state on this schema — which are available, which is the opt-in — and each set's description. A read; running an export is a human door.",
			Schema:      obj(map[string]any{}, nil)},
		// — S57 verification gates (T9) — the agent's nine doors: the six
		// reads scoped to the caller's own loop and component (scope is
		// injected from the step's identity, never a param; a param that
		// names something foreign is a refusal naming the caller's own),
		// and the three mutating specs — the two inert, tightening-only
		// proposals a person commits, and the evidence-bound DoD mark.
		// The nine human writes stay out of this catalog; the guard is
		// cmd/aeon/verify_amend_test.go. None is granted by default.
		{Name: "verify_list", Method: "verify.list", Family: "verify", Class: "safe",
			Description: "List the verification gates in force for the caller's own component — own or inherited, with the source that names each — with requirement, timeout and last-run state. Scoped to the caller's component; it cannot list another component's gates. No default grant.",
			Schema: obj(map[string]any{
				"effective":   boolProp("true (the default) lists the effective set — the component's own rows plus inherited root gates; false lists the own rows only"),
				"requirement": enum("filter to one requirement level", "required", "advisory"),
			}, nil)},
		{Name: "verify_show", Method: "verify.show", Family: "verify", Class: "safe",
			Description: "Show one verification gate of the caller's own component in full: the definition, the policy preview sentence, and the gate's event history. A gate of another component is a refusal that names the caller's own. No default grant.",
			Schema:      obj(map[string]any{"gateId": str("the gate's id — as listed by verify_list")}, []string{"gateId"})},
		{Name: "verify_runs", Method: "verify.runs", Family: "verify", Class: "safe",
			Description: "List the verification runs of the caller's own component: states, gates, head SHAs and digests — never the stdout/stderr tails. The loop filter defaults to the caller's own loop; a foreign loop is a refusal that names the caller's own. No default grant.",
			Schema: obj(map[string]any{
				"loopSlug": str("filter to one loop's runs; absent means the caller's own loop — another loop's slug is refused"),
				"state":    enum("filter to one run state", "pass", "fail", "unverified"),
				"limit":    num("how many recent runs to list; absent means the store's default"),
			}, nil)},
		{Name: "mergerule_show", Method: "mergerule.get", Family: "mergerule", Class: "safe",
			Description: "Show the effective merge rule of the caller's own component — with its source and revision — and the autonomy-floor checklist that names what a merge would still need. No default grant.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "mergerule_preview", Method: "mergerule.preview", Family: "mergerule", Class: "safe",
			Description: "What the merge gate would say for a pull request right now: the verdict, or every refusal sentence — no PR, no forge identity and a failed forge read each say so rather than hide. The component defaults to the caller's own; a foreign one is a refusal naming the caller's own. No default grant.",
			Schema: obj(map[string]any{
				"componentId": str("the component to preview; absent means the caller's own component — a foreign one is refused"),
				"pr":          num("the pull-request number to evaluate; absent is the honest could-not-say"),
			}, nil)},
		{Name: "dod_list", Method: "dod.list", Family: "dod", Class: "safe",
			Description: "List the definition-of-done items of the caller's own loop: each criterion's text and state, and — when met — the evidence (kind and ref) and the actor who marked it. A foreign loop is a refusal that names the caller's own. No default grant.",
			Schema: obj(map[string]any{
				"loopSlug": str("the loop whose criteria to list; absent means the caller's own loop — another loop's slug is refused"),
			}, nil)},
		{Name: "verify_propose_add", Method: "verify.proposeAdd", Family: "verify", Class: "detectable", Mutates: true,
			Description: "Propose adding a verification gate to the caller's own component, for a person to commit: nothing runs until a human commits the inert proposal. Add-only — no input here can update, archive, restore or try. The component is resolved from the caller's own loop; identity and componentId are stamped, never typed. No default grant.",
			Schema: obj(map[string]any{
				"name":        str("the gate's slug name: lowercase alphanumerics and hyphens, 1 to 64; it must be free among the component's live gates"),
				"command":     str("the command the gate runs, verbatim — 4096 bytes or fewer"),
				"requirement": enum("how the gate's result counts", "required", "advisory"),
				"timeoutS":    num("the gate's timeout in seconds, 1 to 600"),
				"reason":      str("why the agent proposes the gate — required, 1 to 512 bytes; the person who decides reads it"),
			}, []string{"name", "command", "requirement", "timeoutS", "reason"})},
		{Name: "mergerule_propose_tighten", Method: "mergerule.proposeTighten", Family: "mergerule", Class: "detectable", Mutates: true,
			Description: "Propose a tightening of the caller's own component's merge rule, for a person to commit: nothing changes until a human commits the inert proposal. Where the domain has an enum with a clear direction the schema admits only the tightening value; the service re-checks every field with Tightens and a loosening is refused with the field named. No default grant.",
			Schema: obj(map[string]any{
				"fields": obj(map[string]any{
					"enabled":                boolProp("whether the rule is on — true tightens, false loosens; the service re-checks"),
					"requireLocal":           boolProp("whether a merge needs a local run — true tightens, false loosens; the service re-checks"),
					"requireDod":             enum("the DoD floor a merge needs — only the tightening value is admitted", "all_met"),
					"requiredChecks":         arr(str("one check a merge must have"), "the required checks — additive only; dropping a name is a loosening the service refuses"),
					"checksPolicy":           enum("which checks must be green — only the tightening value is admitted (every reported check must be success)", "reported_green"),
					"noChecks":               enum("what a merge with no checks does — only the tightening value is admitted", "refuse"),
					"reviewMode":             enum("the review a merge needs — only the strictest value is admitted", "both"),
					"aeonReviewAgents":       arr(str("one agent whose verdict can satisfy the aeon half"), "the agents whose verdict satisfies the aeon half — fewer tightens; the service re-checks"),
					"requireThreadsResolved": boolProp("whether all threads must be resolved — true tightens, false loosens; the service re-checks"),
					"method":                 str("the merge kind (squash, merge, rebase) — neutral: it never tightens or loosens"),
				}, []string{"enabled", "requireLocal", "requireDod", "requiredChecks", "checksPolicy", "noChecks", "reviewMode", "aeonReviewAgents", "requireThreadsResolved", "method"}),
				"reason": str("why the agent proposes the tightening — required, 1 to 512 bytes; the person who decides reads it"),
			}, []string{"fields", "reason"})},
		{Name: "dod_mark", Method: "dod.markAsAgent", Family: "dod", Class: "detectable", Mutates: true,
			Description: "Mark a definition-of-done item of the caller's own loop: unmet or unverifiable freely; met only from a passing verify run of the caller's own loop on the current head SHA — the store re-checks the run and an agent's manual evidence is refused. Every mark writes the ledger's audit row with the agent's actor. No default grant.",
			Schema: obj(map[string]any{
				"itemId":        str("the item's id — as listed by dod_list"),
				"state":         enum("the new state of the item", "met", "unmet", "unverifiable"),
				"evidenceRunId": str("the passing verify run of the caller's own loop the met names — required when the state is met, refused otherwise"),
				"note":          str("the mark's free-text reason, 0 to 512 bytes — audit text the ledger carries"),
			}, []string{"itemId", "state"})},

		// — blueprint (S48 T10, BP9): four reads and ONE propose. The four
		// reads twin the person's standing blueprint.list/show/validate/lint
		// doors — one implementation, the parity walk proves it; only
		// blueprint.propose is a new socket method. There is deliberately no
		// save/delete/duplicate/revert tool: an agent may see, check, and
		// PROPOSE (the inert blueprint_save row a person commits through
		// S45's decide doors), never write (BP9; T8's guard refuses any
		// blueprint write tool by name). —
		{Name: "blueprint_list", Method: "blueprint.list", Family: "blueprint", Class: "safe",
			Description: "List every blueprint: its name, origin (shipped or yours), version, whether a shipped one has been edited, and how many agents, loops and triggers use it. A read of the rows the daemon runs from — the database is the source.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "blueprint_show", Method: "blueprint.show", Family: "blueprint", Class: "safe",
			Description: "One blueprint's full YAML verbatim, its validation report, and what it references — the agents, tools, actions and sub-blueprints its steps name. A read; nothing is written and nothing is enforced by looking.",
			Schema: obj(map[string]any{
				"name": str("the blueprint's name, as blueprint_list shows it"),
			}, []string{"name"})},
		{Name: "blueprint_validate", Method: "blueprint.validate", Family: "blueprint", Class: "safe",
			Description: "Validate YAML text you provide and get the three-state report — errors, warnings, advisories — with nothing saved anywhere. The same validator the save door runs, so a text that validates here validates there; passing here never means a write happened.",
			Schema: obj(map[string]any{
				"content": str("the full candidate YAML, verbatim (at most 1 MiB) — nothing is stored from it"),
			}, []string{"content"})},
		{Name: "blueprint_lint", Method: "blueprint.lint", Family: "blueprint", Class: "safe",
			Description: "Advisories for a stored blueprint or YAML text: which steps' agents the policy would deny or has no rule for, AS OF NOW, each with its dated reason. Advisory only — it gates nothing, saves nothing, and an unavailable policy view is stated, never silently all-clear.",
			Schema: obj(map[string]any{
				"name":    str("a stored blueprint's name to lint (give this or content, not neither)"),
				"content": str("YAML text to lint without storing it (at most 1 MiB)"),
			}, nil)},
		{Name: "blueprint_propose", Method: "blueprint.propose", Family: "blueprint", Class: "detectable", Mutates: true,
			Description: "Propose a new or changed blueprint as an INERT row for a person to decide: validated now (the save door's own report, verbatim, on refusal), committed never by you — nothing changes until a person commits. The whole source rides the proposal, never a patch; identity is stamped, never typed.",
			Schema: obj(map[string]any{
				"name":        str("the blueprint's name (lowercase words joined by single hyphens, at most 64)"),
				"content":     str("the FULL proposed YAML, verbatim — never a patch, never empty, at most 1 MiB"),
				"create_only": map[string]any{"type": "boolean", "description": "true = this is a CREATE: refused at commit if the name already exists (and at create — createOnly never overwrites)"},
				"note":        str("why, for the person deciding — at most 256 characters, carried to the revision when committed"),
			}, []string{"name", "content"})},

		// — supervision (S58 T6, SV16: the attention plane's reads only —
		// ack, snooze, unack are a person's acts and never appear here
		// (SV12), and no channel write, no class edit, no secret in any
		// shape. An agent's three scope verbs answer OWN VIEW: a rootId
		// naming another tree is refused, never answered; the two notify
		// lists are org-descriptor reads carrying no URL path, no secret
		// ref and no subscription detail. —
		{Name: "inbox_list", Method: "inbox.list", Family: "supervision", Class: "safe",
			Description: "What needs a person in YOUR root's tree: live attention items in inbox order with their doors and the source census. Own-view — a rootId outside your root is refused, never answered. A read; acking is a person's act and never a tool.",
			Schema: obj(map[string]any{
				"root_id":       str("your own root's id (the default); any other tree is refused"),
				"severity":      str("filter to one severity class (needs-you, stuck, blocked, stopped, info)"),
				"kinds":         arr(str("exact kind string"), "filter to exact kind strings (any match keeps the item)"),
				"include_acked": map[string]any{"type": "boolean", "description": "true = also show items a person already acked or snoozed (counted, hidden by default)"},
				"limit":         num("maximum items (default 100, at most 500)"),
			}, nil)},
		{Name: "inbox_summary", Method: "inbox.summary", Family: "supervision", Class: "safe",
			Description: "Count what needs a person in YOUR root's tree: totals by severity, how many a person already saw, the oldest waiting item, and the source census — so an empty count can never hide a dead source. Own-view; a read.",
			Schema: obj(map[string]any{
				"root_id": str("your own root's id (the default); any other tree is refused"),
			}, nil)},
		{Name: "mission_status", Method: "mission.status", Family: "supervision", Class: "safe",
			Description: "Your root's overview: live loop counts, the needs-you census (the inbox summary's own words), the spend floor, the cap and next-ready relay — `not-wired` where no seam is wired, never a lying zero, and no composite score anywhere. Own-view; a read.",
			Schema: obj(map[string]any{
				"root_id": str("your own root's id (the default); any other tree is refused"),
			}, nil)},
		{Name: "notify_class_list", Method: "notify.class.list", Family: "supervision", Class: "safe",
			Description: "List the event-class table: every kind and variant, its tone, templates, enabled state and origin — the descriptor of what notifies and how it reads. A read of the plane's shape; class edits are a human door.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "notify_channel_list", Method: "notify.channel.list", Family: "supervision", Class: "safe",
			Description: "List notification channels as an agent may see them: name, host, enabled, last outcome and consecutive failures — never a URL path, a secret reference or a subscription. A read; channel writes are a human door.",
			Schema:      obj(map[string]any{}, nil)},
		// — execution environment (S59 T7, EV16/EV7). Four reads and the ONE
		// ask an agent may make of the plane: that its environment be
		// TIGHTENED. What is deliberately NOT here is everything that makes an
		// environment weaker or real: no create, update, archive, restore, bind,
		// unbind or teardown spec, no secret-reference write, no `backend` field
		// on the propose schema (a backend's strength moves by a person alone —
		// EV16, EV19), and no `source`, `actor`, `status`, `kind` or any value
		// field anywhere. The four read twins ride T6's standing doors, answered
		// by the SAME service the person's panel reads (rule 4); the propose twin
		// is answered by the records service, never the authoring one, and files
		// an INERT environment_restrict row a person commits through S45 — the
		// committer, not this door's author, owns the row when it lands.
		// `secretref_list` answers names and kinds only: the human door's twin
		// carries `source` and this one has no field for it, because a path or a
		// variable name tells a model where to ask (EV7, §4c's two types rather
		// than one redaction).
		{Name: "environment_list", Method: "environment.list", Family: "environment", Class: "safe",
			Description: "List the execution environments a command could run in: backend, what it caps, where each is bound, whether it came from the seed or a person, and when it last changed. Names and settings only — no secret value has anywhere to be (EV7).",
			Schema: obj(map[string]any{
				"backend":         str("narrow to one backend: host-inherit · host-scrubbed · container · vm (the closed set; an unknown word is refused)"),
				"origin":          str("narrow to one origin: system (the seed set) or user (a person's own)"),
				"includeArchived": map[string]any{"type": "boolean", "description": "true = include archived environments too; by default the living ones only"},
				"limit":           map[string]any{"type": "integer", "description": "at most this many rows (the plane caps it; 0 means the plane's own default)"},
			}, nil)},
		{Name: "environment_show", Method: "environment.show", Family: "environment", Class: "safe",
			Description: "One environment in full: its spec as variable NAMES (plus literals the person set), its capability chips, where it is bound, and its revision history. A read — it changes nothing and enforces nothing (EV17).",
			Schema: obj(map[string]any{
				"id":   str("the environment's id (give this or slug — both empty is a refusal, not an empty answer)"),
				"slug": str("the environment's slug, as environment_list shows it"),
			}, nil)},
		{Name: "environment_effective", Method: "environment.effective", Family: "environment", Class: "safe",
			Description: "What a command on YOUR component would actually run in: the backend, the variable names with where each came from, the secret reference NAMES, and whether an unattended loop would be downgraded off the inherited environment. Names, never values (EV5, EV6).",
			Schema: obj(map[string]any{
				"componentId": str("optional: a component in YOUR loop's tree to ask about (defaults to your step's own component; another tree is refused)"),
				"attended":    map[string]any{"type": "boolean", "description": "the loop's attended state, which decides whether an unattended downgrade is noted; omit to read it from your own step's loop"},
			}, nil)},
		{Name: "secretref_list", Method: "secretref.list", Family: "environment", Class: "safe",
			Description: "List the secret REFERENCES — name, the variable each would inject as, and the KIND of source it points at (daemon-env · key-file · store). Never the source itself and never a value: a reference is a pointer, and where the credential lives is not a fact a model gets (EV7).",
			Schema: obj(map[string]any{
				"includeArchived": map[string]any{"type": "boolean", "description": "true = include archived references too; by default the living ones only"},
			}, nil)},
		{Name: "environment_propose_restrict", Method: "environment.proposeRestrict", Family: "environment", Class: "detectable", Mutates: true,
			Description: "Propose that an environment be TIGHTENED — a narrower spec: drop a passed-through name, add a cap, cut egress. Refused at once for any way it is looser than the row today, each loosening named. INERT: nothing changes until a person commits it, and they, not you, own the change. There is no widening, no backend change, and no secret value in any shape here (EV16).",
			Schema: obj(map[string]any{
				"environmentId": str("the environment to tighten — its id or slug, as environment_list shows them"),
				"spec":          str("the FULL proposed spec (JSON or YAML, at most 1 MiB) — never a patch, never empty; an environment is decided whole"),
				"reason":        str("why, for the person deciding — required, 1 to 512 bytes"),
			}, []string{"environmentId", "spec", "reason"})},
		// S49 T7 (AN9, standing rule 2, S40 AU1 lock 1): the automation
		// family's five — four reads and ONE propose. An agent may see what
		// runs on its own and ask a person to switch one off, and nothing
		// else: there is no tool that creates, enables, updates, archives,
		// pauses, resumes or fires (rule 2 — the only write-shaped name here
		// creates a PROPOSAL, never a trigger change, so AU1's lock 1 holds
		// unedited), and no proposal shape exists for any other ask (T6:
		// automation_disable is the kind, narrowing-only). The four reads ride
		// T3's standing doors answered by the SAME services the person's
		// panel reads (rule 4 — one implementation, field for field, health
		// carrying its evaluatedAt); the propose twin is answered by the
		// records service and files an INERT row a person commits through
		// S45's doors — the committer, not this door's author, owns the flip.
		// What no input schema here carries: actor (OL3 stamps the step),
		// status/enabled/archived (state is nobody an agent writes or
		// filters-into-view; the reads answer state, they do not take it as
		// a dial), kind (the kind is implied), and no webhook secret value
		// anywhere — health answers a NAME and a set-bool, never a value (§4c).
		{Name: "automation_list", Method: "automation.list", Family: "automation", Class: "safe",
			Description: "List what starts agents on its own — schedules, webhooks and event triggers in one view: name, source family, state, the blueprint it starts, and a plain-words description of the rhythm. Archived rows never appear (AN6). A read: it changes nothing and enforces nothing.",
			Schema: obj(map[string]any{
				"source":    enum("narrow to one family: schedule · webhook · event (the source is part of the automation's name — AN1; an unknown word is refused)", "schedule", "webhook", "event"),
				"blueprint": str("narrow to the automations that start this blueprint (its slug)"),
				"q":         str("a substring over name, id and the plain-words description — the desk's one search box"),
			}, nil)},
		{Name: "automation_show", Method: "automation.show", Family: "automation", Class: "safe",
			Description: "One automation in full: its canonical form, the plain-words description from the one formatter, its limits, its last five fires and five changes, and whether its blueprint still validates (AN5). A read — the same answer the person's panel gets, field for field.",
			Schema: obj(map[string]any{
				"source": enum("which family the row belongs to — the source is part of the name (AN1): schedule · webhook · event", "schedule", "webhook", "event"),
				"id":     str("the automation's id, as automation_list shows it"),
			}, []string{"source", "id"})},
		{Name: "automation_fires", Method: "automation.fires", Family: "automation", Class: "safe",
			Description: "The unified fire log, newest first: every outcome (started, refused, limited, skipped, could-not-say) with its reason, for one automation or across both families. could-not-say stays honest — a fire the record cannot explain is never rewritten into peace (AN12). A read.",
			Schema: obj(map[string]any{
				"source":   enum("narrow to one family: schedule · webhook · event (omit for both)", "schedule", "webhook", "event"),
				"id":       str("narrow to one automation's id (with source; omit both for the whole log)"),
				"outcome":  enum("narrow to one outcome (AN12's closed five)", "started", "refused", "limited", "skipped", "could-not-say"),
				"limit":    map[string]any{"type": "integer", "description": "at most this many rows (the plane caps it; 0 means the plane's own default)"},
				"beforeId": str("the cursor of the PREVIOUS page's last row (nextBeforeId from the last answer) — paging never re-reads a page"),
			}, nil)},
		{Name: "automation_health", Method: "automation.health", Family: "automation", Class: "safe",
			Description: "Whether an automation COULD fire right now: blueprint present and valid, webhook secret env set or unset (the NAME and a bool, never a value), workspace, scope — as of the evaluatedAt this answer stamps (AN10). Advisory only: health never gates a fire and never changes a row.",
			Schema: obj(map[string]any{
				"source": enum("narrow to one family: schedule · webhook · event (omit for every automation)", "schedule", "webhook", "event"),
				"id":     str("narrow to one automation's id (with source)"),
			}, nil)},
		{Name: "automation_propose_disable", Method: "automation.proposeDisable", Family: "automation", Class: "detectable", Mutates: true,
			Description: "Propose that ONE automation be DISABLED — the only ask an agent may file here. Refused at once if the automation is unknown, archived, or already off, each named (nothing is stored that decides nothing). INERT: nothing pauses, switches or hides until a person commits it, and they, not you, own the change. There is deliberately no propose-enable, -update or -archive: a proposal can only take authority away (AN9).",
			Schema: obj(map[string]any{
				"source": enum("which family the automation is in — the source is part of the name (AN1): schedule · webhook · event", "schedule", "webhook", "event"),
				"id":     str("the automation's id, as automation_list shows it"),
				"reason": str("why, for the person deciding — required, 1 to 256 bytes"),
			}, []string{"source", "id", "reason"})},
		// S50 T5 (RM8, standing rule 2): the roadmap family's three — two
		// reads and ONE propose. An agent may see the roadmap and ask a
		// person to ADD one idea, and nothing else: no tool updates,
		// cancels, estimates, prioritizes, ideates, plans or materializes
		// (T4's guard family pinned those eight doors as the person's
		// alone — the write regex names them; this family carries no such
		// name), and no proposal kind exists for any other ask (there is
		// deliberately no roadmap_update/-cancel/-estimate/-priority/
		// dispatch kind). The two reads ride the SAME shared answers the
		// person's socket doors run (rule 4 — roadmap_list twins
		// roadmap.board, RM8's table: a step asking "the roadmap" gets the
		// honest board with readiness and blocker; roadmap_show answers the
		// item in full WITH the trail the person splits across show and
		// history); the propose twin files an INERT row a person commits
		// through S45's doors — the COMMITTER, not this door's author,
		// owns the item. What no input schema here carries: actor (OL3
		// stamps the step), status/priorityState/estimate/provenance (an
		// idea that arrived ranked would be an idea that arrived decided —
		// the ladder, the size and the origin are the person's to give at
		// add or later; the desk's strict parse refuses a smuggled one by
		// name), and no identity/where-fields anywhere.
		{Name: "roadmap_list", Method: "roadmap.board", Family: "roadmap", Class: "safe",
			Description: "The roadmap as the person's board renders it: every item with its honest status, its three-state priority, its estimate with basis, the plain-words Describe sentence, the readiness answer PAIRED with the exact blocker behind it, and the loop it rode. Rows come in the store's order with per-epic counts; done and cancelled are visible history, not vanishing rows (D7). A read: it files nothing and changes nothing.",
			Schema: obj(map[string]any{
				"componentRoot": str("narrow to one component root (omit for every component the roadmap carries — this is the cross-project view roadmap.list deliberately is not)"),
				"status":        enum("narrow to one status (the closed ladder): idea · ideating · brief-agreed · planned · ready · running · done · cancelled", "idea", "ideating", "brief-agreed", "planned", "ready", "running", "done", "cancelled"),
				"epic":          str("narrow to one epic (an epic is a tag, not a row — R2)"),
			}, nil)},
		{Name: "roadmap_show", Method: "roadmap.show", Family: "roadmap", Class: "safe",
			Description: "One roadmap item in full WITH its trail: the same view the board gives (status, priority, estimate with basis, provenance, brief/plan POINTERS never content, the loop it rode) beside the item's audit history — every op, its summary, its actor kind and id, when it happened, newest first. A read: the same bytes the person's two doors (show and history) answer, in one call.",
			Schema: obj(map[string]any{
				"id": map[string]any{"type": "integer", "description": "the item's row id, as roadmap_list shows them — 0 or negative is refused, never guessed at"},
			}, []string{"id"})},
		{Name: "roadmap_propose_idea", Method: "roadmap.proposeIdea", Family: "roadmap", Class: "detectable", Mutates: true,
			Description: "Propose that ONE idea be ADDED to the roadmap — the only ask an agent may file here (RM8). Refused at once if the component root names no component or the identical OPEN item already stands on the board, each named: an ask that changes nothing is refused at create, not filed. INERT: nothing stands on the roadmap until a person commits it, and they — not you — own the row, its status, its priority and its size. There is deliberately no tool that updates, cancels, estimates, prioritizes or dispatches an item: those doors are the person's alone.",
			Schema: obj(map[string]any{
				"componentRoot": str("the project root the idea belongs to — the roadmap is scoped to one component (N2); it must exist"),
				"title":         str("the title a board row will carry — required, non-blank, at most 300 bytes; a board renders titles, not paragraphs"),
				"epic":          str("the epic to tag it with (optional; an epic is a tag, not a row — at most 200 bytes)"),
				"note":          str("why, for the person deciding — required, 1 to 1024 bytes: the sentence they read before adding your idea is the ask itself"),
			}, []string{"componentRoot", "title", "note"})},
		// S27 T3 (CN7/P6): the graph's two edges. An edge is INTENT — it
		// never moves a rung (no tool writes status, priority or estimate;
		// RM8 stands whole for the ladder) — and readiness is re-read at
		// every attempt, so filing or removing an edge can never start or
		// stop a loop by itself. Registered, never default-granted.
		{Name: "roadmap_depend", Method: "roadmap.depend", Family: "roadmap", Class: "detectable", Mutates: true,
			Description: "Record that one roadmap item WAITS FOR another: an edge of intent on the graph, not a status move — the item keeps its rung and nothing dispatches because of it. Same repository only, no edge onto yourself, no cycle (the refusal names the closure), no edge onto or from a cancelled item. Readiness is decided at each attempt from the CURRENT graph (D4): when the upstream item reaches done, the wait ends by itself. Refused if the edge already stands.",
			Schema: obj(map[string]any{
				"id":        str("the item that will wait — must exist and be live (not done, not cancelled)"),
				"dependsOn": str("the item it waits for — same repository, live, and it must not already wait for the first (a 2-cycle would deadlock the graph)"),
			}, []string{"id", "dependsOn"})},
		{Name: "roadmap_undepend", Method: "roadmap.undepend", Family: "roadmap", Class: "detectable", Mutates: true,
			Description: "Remove exactly ONE dependency edge and nothing else: no cascade, no loop touched, no status moved — an item that is already running may un-wish, because the edge recorded intent only. A cancelled item's edge is removable from either end (the fix the cancelled-dependency refusal points at); removing an edge nobody filed is refused with a named no-such-edge sentence rather than pretending success.",
			Schema: obj(map[string]any{
				"id":        str("the item whose graph is being edited — the SOURCE of the edge; it may be live or cancelled"),
				"dependsOn": str("the item it no longer waits for — the edge must exist, or the refusal names it"),
			}, []string{"id", "dependsOn"})},

		// — S51 T5 (TM6): the bug family's three. Two safe reads and ONE
		// filing. bug_list twins triage.board (the honest backlog — attempts
		// from the audit, scored candidates, hand-off status) and bug_show
		// twins triage.show (the one rendering, rule 4) — both answer the
		// SAME doors the person holds unscoped. bug_file is the agent's ONE
		// permitted filing of the backlog: an UNVALIDATED, priority-unknown
		// claim through the SAME Create triage.file runs, filed_by the
		// STAMPED slug, refused by the counted per-agent cap
		// (triage.MaxOpenClaimsPerAgent) before the write. What no input
		// schema here carries: actor/filedBy (OL3 stamps), validationState
		// (an agent's claim is unvalidated by definition — the store's
		// default is the backstop, D1), priorityState/priorityLabel (an
		// agent's claim reads unknown; priority is the person's D2 three-
		// state to set), duplicateOf/handedOff/any identity or state field
		// (the person's doors, TM6 — the shape itself cannot carry them).
		{Name: "bug_list", Method: "triage.board", Family: "bug", Class: "safe",
			Description: "The triage backlog as the person's board renders it: claims with their honest validation state, their three-state priority, the attempt count from the audit, scored duplicate candidates and the hand-off status, newest first. A read: it files nothing and changes nothing — and an agent's own claims are visible here like everyone's, because the backlog is shared.",
			Schema: obj(map[string]any{
				"componentRoot": str("the project root whose backlog to read — required; triage is scoped to one component's root"),
				"validation":    enum("narrow to one validation state (the closed four): unvalidated · reproduced · not-reproduced · could-not-tell", "unvalidated", "reproduced", "not-reproduced", "could-not-tell"),
				"priority":      enum("narrow to one priority state (the closed three): known · assumed · unknown", "known", "assumed", "unknown"),
				"handedOff":     boolProp("narrow to claims handed to a roadmap item (true), claims not handed (false), or omit for both"),
			}, []string{"componentRoot"})},
		{Name: "bug_show", Method: "triage.show", Family: "bug", Class: "safe",
			Description: "One bug in full: the claim as filed (title, body), its validation state and its three-state priority with label, who filed it, the duplicate it was folded into if any, the roadmap item it was handed to, and its timestamps. A read: the same bytes the person's triage.show answers, item for item.",
			Schema: obj(map[string]any{
				"id": map[string]any{"type": "integer", "description": "the bug's row id, as bug_list shows them — 0 or negative is refused, never guessed at"},
			}, []string{"id"})},
		{Name: "bug_file", Method: "triage.fileAs", Family: "bug", Class: "detectable", Mutates: true,
			Description: "File ONE bug claim into the backlog — the only triage write an agent may make (TM6). What arrives is unvalidated and priority-unknown BY CONSTRUCTION: there is no field for a validation state, a priority, a duplicate fold or a hand-off, because those doors are the person's alone. filed_by is your stamped slug (never a parameter); at most triage.MaxOpenClaimsPerAgent of your claims stay open unvalidated at once, the counted refusal naming the cap and how to list them. A person filing a claim uses triage.file, under themselves.",
			Schema: obj(map[string]any{
				"componentRoot": str("the project root the claim belongs to — the component must exist; a claim without a root has nowhere to be triaged"),
				"title":         str("what breaks, in one line — required, non-blank, at most 300 bytes; the board renders titles, not paragraphs"),
				"body":          str("what you saw — required: the steps, the error, the log lines a person needs to reproduce or reject the claim; at most 16 KiB"),
			}, []string{"componentRoot", "title", "body"})},
		// — S63 T6 (RT11, standing rule 2): the scorecard family's three.
		// RT3 lives here as what these results ARE — a measurement, not an
		// instruction; the full sentence rides every result, rendered once
		// by the plane (internal/protocol/scorecard.go owns the constant; a
		// catalog description paraphrases, it never re-spells it).
		// NO WRITE EXISTS IN THIS FAMILY: retro.backfill is the person's
		// door and joins nothing here — and no score feeds anything back:
		// dispatch, priority, blueprints and settings never learn the word
		// scorecard (T5's guards). The subjects are STAMPED: these schemas
		// carry no agent, no slug, no id, no root — who and what is
		// measured comes from the step's own identity (F13, OL3), so a
		// self-chosen subject is a PARSE error, not a shrug.
		// None of the three is in DefaultGrant(); registered is not granted
		// (S11) — an operator hands them out per tool_access rule.
		{Name: "scorecard_mine", Method: "scorecard.agent", Family: "scorecard", Class: "safe",
			Description: "Your own scorecard: what happened in your finished loops over a window — metrics with sample sizes, coverage states and floors, never an instruction about what to do next. The subject is YOUR stamped slug; there is no agent parameter to aim at anyone else, and another agent's card is not this door. A measurement, not an instruction.",
			Schema: obj(map[string]any{
				"since": str("ISO day or timestamp opening the window — omit for the default lookback"),
				"until": str("ISO day or timestamp closing the window — omit for now"),
				"last":  num("window size in days when since/until are absent, clamped to the plane's floor and ceiling"),
			}, nil)},
		{Name: "scorecard_component", Method: "scorecard.component", Family: "scorecard", Class: "safe",
			Description: "The scorecard of YOUR loop's own root component, children included — the component's aggregate, not a roster: no per-agent rows and no breakdown are ever returned here, so this door cannot rank anyone. The subject is your loop's root, stamped; there is no component parameter to aim elsewhere. A measurement, not an instruction.",
			Schema: obj(map[string]any{
				"since": str("ISO day or timestamp opening the window — omit for the default lookback"),
				"until": str("ISO day or timestamp closing the window — omit for now"),
				"last":  num("window size in days when since/until are absent, clamped to the plane's floor and ceiling"),
			}, nil)},
		{Name: "digest_learned", Method: "digest.learned", Family: "scorecard", Class: "safe",
			Description: "The what-did-we-learn digest for YOUR loop's own root component, in the plane's fixed section order: outcome counts, rework, cost, and the learnings quoted verbatim with their provenance — retracted ones excluded, other agents' private memories never listed (project-scope rows only), and an explicit nothing-needs-you when nothing is. A measurement, not an instruction.",
			Schema: obj(map[string]any{
				"since": str("ISO day or timestamp opening the window — omit for the default lookback"),
				"until": str("ISO day or timestamp closing the window — omit for now"),
			}, nil)},
	}
}

// — schema builders (the strict shapes; additionalProperties:false everywhere
// so an identity field smuggled into input is a PARSE error, not a shrug) —

func obj(props map[string]any, required []string) json.RawMessage {
	m := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return must(m)
}

func arr(items any, description string) map[string]any {
	m := map[string]any{"type": "array", "items": items}
	if description != "" {
		m["description"] = description
	}
	return m
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// boolProp — S56 T8: the one boolean builder the autonomy_list schema needs
// (includeInactive); no boolean shape existed among the builders above.
func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func num(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func enum(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func must(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("records: a catalog schema does not encode: " + err.Error())
	}
	return b
}
