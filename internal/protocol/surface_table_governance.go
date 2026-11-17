// The governance plane of the surface table (S47 T6): the policy reads and the authoring
// human door, the staging line, the seed engine, the autonomy grants, the budget and
// watchdog planes, the declared settings and the skill catalog.
package protocol

// surfaceTableGovernance is one slice of the closed classification table, built fresh on every
// call: there is no package-level map to append to, so a method cannot enter the surface
// by mutating shared state — only by editing a table, which the completeness tests then
// check both ways.
func surfaceTableGovernance() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodAutonomyChanges: {
			Kind:    SurfaceRead,
			Summary: "The autonomy audit: who granted, denied or revoked what, from which door.",
		},
		MethodAutonomyDeny: {
			Kind:          SurfaceWrite,
			Summary:       "Adds a standing veto over an action or the outward class, judged live above any pin.",
			HumanDoorOnly: true,
		},
		MethodAutonomyEffective: {
			Kind:    SurfaceRead,
			Summary: "What an unattended loop started now on a root could do, per registry action: the verdict and the one-sentence why-not, from the same plan.",
		},
		MethodAutonomyGrant: {
			Kind:          SurfaceWrite,
			Summary:       "Grants an action to a root component as a person on the socket, with the expiry the rule requires.",
			HumanDoorOnly: true,
		},
		MethodAutonomyList: {
			Kind:    SurfaceRead,
			Summary: "The standing autonomy grants with their scope, expiry and who authored each.",
		},
		MethodAutonomyPinned: {
			Kind:    SurfaceRead,
			Summary: "The grants a loop was pinned with at its start \u2014 the ceiling it runs under.",
		},
		MethodAutonomyProposeDeny: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert proposal to add a veto; the enforcement gate never learns the word proposal.",
		},
		MethodAutonomyProposeRevoke: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert proposal to revoke one of this agent grants; no kind here creates an allow.",
		},
		MethodAutonomyRevoke: {
			Kind:          SurfaceWrite,
			Summary:       "Revokes a standing grant; a loop pinned with it is still re-checked against the floor.",
			HumanDoorOnly: true,
		},
		MethodAutonomyShow: {
			Kind:    SurfaceRead,
			Summary: "Show one autonomy grant in full by its id: the grant itself, its last change, the count of begun uses, and the running loops pinned with it.",
		},
		MethodAutonomyUses: {
			Kind:    SurfaceRead,
			Summary: "Which loops and steps actually used a standing grant, so a person sees its reach before renewing it.",
		},
		MethodAutonomyVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed autonomy action and class vocabulary, read from the validator rather than a client copy.",
		},
		MethodBudgetEffective: {
			Kind:    SurfaceRead,
			Summary: "The budgets that would apply to THIS loop's spend, tightest first (loop, epic, component, root, workspace) \u2014 the tiers a spend is checked.",
		},
		MethodBudgetList: {
			Kind:    SurfaceRead,
			Summary: "Every budget with its caps, its live spend per dimension and its last change.",
		},
		MethodBudgetRemove: {
			Kind:          SurfaceWrite,
			Summary:       "Removes a budget cap; the loop that was parked by it still needs the raise door to move.",
			HumanDoorOnly: true,
		},
		MethodBudgetSet: {
			Kind:          SurfaceWrite,
			Summary:       "Sets a budget cap for a scope as the human door, the actor stamped from the connection.",
			HumanDoorOnly: true,
		},
		MethodBudgetShow: {
			Kind:    SurfaceRead,
			Summary: "Show one budget by id: caps, floor, the live spend per dimension, the status, the audit's last change, and the worst-case overshoot note.",
		},
		MethodBudgetVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed budget scopes, actions and signals, so the client builds from the daemon.",
		},
		MethodLoopAccess: {
			Kind:    SurfaceRead,
			Summary: "A loop's permission mode: ask (the default), pre-approved tools, or full access, and who turned it on.",
		},
		MethodLoopSetAccess: {
			Kind:          SurfaceWrite,
			Summary:       "Turns a loop's full access or pre-approved tools on or off as the human door; the actor is the connection peer and the change is published.",
			HumanDoorOnly: true,
		},
		MethodPolicyAdd: {
			Kind:          SurfaceWrite,
			Summary:       "Authors a policy row as the human door; the actor is the connection peer and every write lands one audit row.",
			HumanDoorOnly: true,
		},
		MethodPolicyChanges: {
			Kind:    SurfaceRead,
			Summary: "The authorship audit: who added, removed, seeded or committed a guard, when, and from which door.",
		},
		MethodPolicyEvents: {
			Kind:    SurfaceRead,
			Summary: "The 90-day policy audit: who was refused, by which rule, from which door \u2014 never read as 'nothing happened'.",
		},
		MethodPolicyGet: {
			Kind:    SurfaceRead,
			Summary: "One policy row in full, the raw spec riding here and never on the identity list.",
		},
		MethodPolicyInForce: {
			Kind:    SurfaceRead,
			Summary: "What is in force for one party \u2014 exactly one of agent or loop: one answer per governing rule, the three states un-collapsed (acknowledged.",
		},
		MethodPolicyList: {
			Kind:    SurfaceRead,
			Summary: "S10 (T13): the four read-only policy methods, registered beside the other product reads (ruling R4 \u2014 reads for the app; authoring is.",
		},
		MethodPolicyProposeAdd: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT proposal to tighten policy; it constrains nothing until a person commits it.",
		},
		MethodPolicyProposeRemove: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT proposal to narrow policy; an agent can never widen what a person set.",
		},
		MethodPolicyRemove: {
			Kind:          SurfaceWrite,
			Summary:       "Removes a policy row as the human door, with before/after in-force confirmed in the answer.",
			HumanDoorOnly: true,
		},
		MethodPolicySeedCorpus: {
			Kind:          SurfaceWrite,
			Summary:       "Seeds command_access rows from this repository own agent definitions, authored as operator, never automatic.",
			HumanDoorOnly: true,
		},
		MethodToolsMatchPattern: {
			Kind:    SurfaceRead,
			Summary: "Which tools a ToolAccess pattern names today, matched by the one matcher the gate uses.",
		},
		MethodPolicyVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed policy sets \u2014 types, targets, sources, actions \u2014 so no client has to hardcode them.",
		},
		MethodPolicyWhy: {
			Kind:    SurfaceRead,
			Summary: "Ask why a question is allowed, denied or asked: name the policy type and the party (agent/loop/lane-type/mcp) and the thing asked about.",
		},
		MethodProposalCommit: {
			Kind:          SurfaceWrite,
			Summary:       "Applies a proposal effect through the one desk registry and flips the row, bound to the digest read first.",
			HumanDoorOnly: true,
		},
		MethodProposalCreate: {
			Kind:    SurfaceWrite,
			Summary: "Files an inert proposal row from the socket, because discarding it loosens nothing.",
		},
		MethodProposalDiscard: {
			Kind:    SurfaceWrite,
			Summary: "Discards an inert proposal row the caller authored; nothing enforced was touched.",
		},
		MethodProposalGet: {
			Kind:    SurfaceRead,
			Summary: "One proposal with its digest and an advisory preview of what committing it would do.",
		},
		MethodProposalList: {
			Kind:    SurfaceRead,
			Summary: "List staged proposals, newest first, with an optional status filter (proposed, committed, rejected, discarded): kind, raw payload, author.",
		},
		MethodProposalReject: {
			Kind:          SurfaceWrite,
			Summary:       "Rejects a proposal with a required reason and applies nothing.",
			HumanDoorOnly: true,
		},
		MethodSeedApply: {
			Kind:          SurfaceWrite,
			Summary:       "Applies a seed set deliberately; the actor comes from the connection, never from params.",
			HumanDoorOnly: true,
		},
		MethodSeedPlan: {
			Kind:    SurfaceRead,
			Summary: "The dry-run answer: what applying a set now would create, change, retire or leave alone.",
		},
		MethodSeedRestore: {
			Kind:          SurfaceWrite,
			Summary:       "Restores a seed set records back to the declared state, the actor again the peer at the socket.",
			HumanDoorOnly: true,
		},
		MethodSeedStatus: {
			Kind:    SurfaceRead,
			Summary: "Reports every declared seed set, its version and what the ledger says was applied.",
		},
		MethodSettingsGet: {
			Kind:    SurfaceRead,
			Summary: "One declared setting, its effective value and where that value came from.",
		},
		MethodSettingsHistory: {
			Kind:    SurfaceRead,
			Summary: "A setting's append-only change history, who wrote it and from which door.",
		},
		MethodSettingsList: {
			Kind:    SurfaceRead,
			Summary: "Every declared setting with its value, source and default, read through the config reader.",
		},
		MethodSettingsSet: {
			Kind:          SurfaceWrite,
			Summary:       "Writes a declared setting through the one door; an undeclared or credential-shaped key is refused.",
			HumanDoorOnly: true,
		},
		MethodSettingsUnset: {
			Kind:          SurfaceWrite,
			Summary:       "Clears a stored setting value so the environment or the default speaks again.",
			HumanDoorOnly: true,
		},
		MethodSkillsGet: {
			Kind:    SurfaceRead,
			Summary: "One skill with its front matter and body as the catalog holds them.",
		},
		MethodSkillsInstall: {
			Kind:          SurfaceWrite,
			Summary:       "Installs a skill from a path the person names, validated before the catalog moves.",
			HumanDoorOnly: true,
		},
		MethodSkillsList: {
			Kind:    SurfaceRead,
			Summary: "The SKILL.md catalog: what is available, valid or broken.",
		},
		MethodSkillsRemove: {
			Kind:          SurfaceWrite,
			Summary:       "Removes an installed skill; a skill a step is using is refused by name.",
			HumanDoorOnly: true,
		},
		MethodSkillsValidate: {
			Kind:    SurfaceRead,
			Summary: "Runs the validator over a candidate skill and reports every refusal, touching nothing.",
		},
		MethodSpendReport: {
			Kind:    SurfaceRead,
			Summary: "Spend over time or by trigger, component, epic or route: USD floors per bucket with the honesty marks (exact when the floor is a total.",
		},
		MethodWatchdogList: {
			Kind:    SurfaceRead,
			Summary: "The watchdog rules with their signals, actions and whether each is available now.",
		},
		MethodWatchdogRemove: {
			Kind:          SurfaceWrite,
			Summary:       "Removes a watchdog rule, the loosening confirmed against its rendered statement first.",
			HumanDoorOnly: true,
		},
		MethodWatchdogSet: {
			Kind:          SurfaceWrite,
			Summary:       "Declares a watchdog rule as the human door, validated against the closed signal set.",
			HumanDoorOnly: true,
		},
		MethodWatchdogSetEnabled: {
			Kind:          SurfaceWrite,
			Summary:       "Turns a watchdog rule on or off; a loosening confirms against the rendered canonical statement.",
			HumanDoorOnly: true,
		},
		MethodWatchdogShow: {
			Kind:    SurfaceRead,
			Summary: "One watchdog rule, its trigger and the last thing it did.",
		},
		MethodWatchdogVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed signal and action set WITH availability \u2014 the client builds from the daemon, never a local copy.",
		},
	}
}
