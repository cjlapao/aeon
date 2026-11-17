package daemon

// records_identity.go — the records door's WHO-and-WHERE half: the one
// vocabulary gate every action passes, the subject-scope and component
// rules that pin an agent inside its own root (OL14), and the refusal
// sentences for the writes a person's door answers and the step's never
// does. Split from records.go at S58 T6 (SIZE1 — the file was at 1,013
// lines when the supervision family landed); the routing switch and the
// tools-facing Invoker stayed with Do, where a reader follows a call.

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
)

// catalogKnows gates Do through the ONE vocabulary (internal/records), so
// the service and the tools can never disagree about what exists.
func catalogKnows(name string) bool {
	for _, sp := range records.Catalog() {
		if sp.Name == name {
			return true
		}
	}
	return false
}

type subjectInput struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ownSubject is the scope guard attach cannot skip: a pointer may only be
// pinned within the step's own project (briefs live in the step's own loop),
// so an agent cannot litter another project's records with its documents.
func (r *RecordsService) ownSubject(ident Identity, s subjectInput) *Failure {
	if ident.User {
		// The person's org is the whole board (OL14): which subjects a
		// person may speak for is decided by which doors exist, not by a
		// loop comparison written for steps.
		return nil
	}
	switch s.Kind {
	case "goal":
		g, err := r.st.GetGoal(s.ID)
		if err != nil {
			return notFoundFailure(s.ID, err)
		}
		if g.ComponentID != ident.RootID {
			return &Failure{Sentence: fmt.Sprintf("refused: goal %s is in another root — attachments pin within the step's own", s.ID)}
		}
	case "epic":
		e, err := r.st.GetEpic(s.ID)
		if err != nil {
			return notFoundFailure(s.ID, err)
		}
		if e.ComponentID != ident.RootID {
			return &Failure{Sentence: fmt.Sprintf("refused: epic %s is in another root — attachments pin within the step's own", s.ID)}
		}
	case "brief":
		// A brief's subject_id IS the loop slug (0036); pinning someone
		// else's loop's brief is not this step's fact to make.
		if s.ID != ident.LoopSlug {
			return &Failure{Sentence: fmt.Sprintf("refused: brief %s is another loop's contract — this step speaks for %s", s.ID, ident.LoopSlug)}
		}
	case "task":
		// Tasks are T10's family; the guard reads through the same door.
		t, err := r.st.GetTaskByID(s.ID)
		if err != nil {
			return notFoundFailure(s.ID, err)
		}
		if t.ComponentID == "" {
			return &Failure{Sentence: fmt.Sprintf("refused: task %s has no component — its root cannot be established, so no attachment can be pinned to it from here", s.ID)}
		}
		root, err := r.st.RootOf(t.ComponentID)
		if err != nil || root != ident.RootID {
			return &Failure{Sentence: fmt.Sprintf("refused: task %s is not in this root", s.ID)}
		}
	default:
		return &Failure{Sentence: fmt.Sprintf("refused: subject kind %q is not goal, brief, epic or task", s.Kind)}
	}
	return nil
}

func (r *RecordsService) resolveComponent(ident Identity, ref string) (store.Component, *Failure) {
	if ref == "" {
		return store.Component{}, &Failure{Sentence: "refused: a stake names a component — id or exact name within the step's root"}
	}
	if c, err := r.st.ReadComponent(ref); err == nil {
		if c.ID == ident.RootID || c.ParentID == ident.RootID {
			return c, nil
		}
		return store.Component{}, &Failure{Sentence: fmt.Sprintf("refused: component %q is not in this root%s", ref, r.rootChoices(ident))}
	}
	// The root is itself a component and, for a workspace with no children, the
	// only one there is: its name answers as well as its id.
	var choices []store.Component
	if root, err := r.st.ReadComponent(ident.RootID); err == nil {
		if root.Name == ref || root.Slug == ref {
			return root, nil
		}
		choices = append(choices, root)
	}
	comps, err := r.st.ListComponents(store.ComponentFilter{ParentID: ident.RootID})
	if err != nil {
		return store.Component{}, &Failure{Sentence: fmt.Sprintf("refused: components could not be listed (%v)", err)}
	}
	for _, c := range comps {
		if c.Name == ref || c.Slug == ref {
			return c, nil
		}
	}
	return store.Component{}, &Failure{Sentence: fmt.Sprintf("refused: component %q is not in this root%s", ref, componentChoices(append(choices, comps...)))}
}

// rootChoices names what a stake may point at, so a refused guess is answered
// with the valid ones instead of leaving the agent to invent another.
func (r *RecordsService) rootChoices(ident Identity) string {
	var all []store.Component
	if root, err := r.st.ReadComponent(ident.RootID); err == nil {
		all = append(all, root)
	}
	if comps, err := r.st.ListComponents(store.ComponentFilter{ParentID: ident.RootID}); err == nil {
		all = append(all, comps...)
	}
	return componentChoices(all)
}

func componentChoices(all []store.Component) string {
	if len(all) == 0 {
		return ""
	}
	parts := make([]string, 0, len(all))
	for _, c := range all {
		parts = append(parts, fmt.Sprintf("%q (id %s)", c.Name, c.ID))
	}
	return " — a stake names one of: " + strings.Join(parts, ", ")
}

// the door that does answer a person. DB19: sentences, never the error
// channel; an honest "not this door" beats an answer of zero.
func personDoorRefusal(name string) *Failure {
	why, ok := map[string]string{
		"goal_update":  "goal_update rewrites a draft a step is authored — a person's act is the goal decision method",
		"brief_create": "brief_create files a loop's contract from a step's workspace; a person's half is the brief decision method",
		"attach":       "an attachment pins a document a step wrote into a loop's workspace; a person's doors decide what is filed, they do not pin it",

		"task_bump_counter": "a ladder's counters are the engine's; the person's doors for a task are its decision method and task_set_status",
		"loop_list":         "the socket already speaks loop.list for a person",
		"loop_get":          "the socket already speaks loop.show for a person",
		"epic_create":       "epics are authored from a step's project scope; a person decides what one means",
		"epic_update":       "epic_update rewrites what a step authored; the person's act is the decision",

		// S46 T8 (MC2): the mcp.mine.* doors are agent-scoped in BOTH
		// directions — a person session reaching one is pointed at the
		// person's own unscoped door, never served the agent's view (which
		// would make these an oracle for whoever holds the endpoint).
		"mcp_servers":               "this door is agent-scoped; a person uses mcp.servers",
		"mcp_grants":                "this door is agent-scoped; a person uses mcp.grants",
		"mcp_status":                "this door is agent-scoped; a person uses mcp.status",
		"mcp_propose_remove_server": "this door is agent-scoped; a person authors directly — mcp.server.remove is the person's door",
		"mcp_propose_revoke_grant":  "this door is agent-scoped; a person revokes directly — mcp.grant.revoke is the person's door",

		// S63 T6 (RT11, the mcp.mine/LM7 shape): the scorecard family's
		// three are agent-scoped in BOTH directions — a person holding the
		// endpoint already holds the unscoped doors; serving the step's
		// view through them would make a stamped subject an oracle.
		"scorecard_mine":      "this door answers the agent's own view; a person uses scorecard.agent",
		"scorecard_component": "this door is scoped to the step's own root component; a person uses scorecard.component",
		"digest_learned":      "this door is scoped to the step's own root component; a person uses digest.learned",

		// S27 T8 (CN23, the mcp.mine/scorecard shape): the scheduler
		// family's four reads are step-scoped in BOTH directions — a
		// person holding the endpoint already holds the unscoped doors;
		// serving the step's one-repository view through them would make
		// the machine's scheduler an oracle for whoever types a slug.
		"scheduler_status":    "this door is scoped to the step's own repository; a person uses scheduler.status",
		"scheduler_preview":   "this door is scoped to the step's own repository; a person uses scheduler.preview",
		"scheduler_decisions": "this door is scoped to the step's own repository; a person uses scheduler.decisions",
		"scheduler_attention": "this door is scoped to the step's own repository; a person uses scheduler.attention",

		// S52 T4 (LM7, the mcp.mine shape): the memory plane's four are
		// agent-scoped in BOTH directions. A person holding the endpoint
		// already has every view that is legitimately theirs — the board,
		// the unscoped query, show, add, retract. Serving the agent's view
		// to a person would make an agent's private memory an oracle for
		// whoever types its slug, which is the whole thing LM1/LM5 refuse.
		"learning_query":           "this door is agent-scoped; a person uses learnings.query (learnings.board to browse, learnings.forAgent to ask what an agent would be shown)",
		"learning_show":            "this door is agent-scoped; a person uses learnings.show — the whole store is the person's view",
		"learning_add":             "this door is agent-scoped and capped; a person's door is learnings.add, where told is the person's own word (S52 LM4)",
		"learning_propose_retract": "this door is agent-scoped; a person retracts directly — learnings.retract is the person's door, and proposal.create with kind learning_retract is a person's ask too",

		// S47 T7 (TC9/MC2): the two own-view tool doors, agent-scoped in BOTH
		// directions for the same reason the mcp.mine.* five are. A person may
		// read any agent's whole tool situation — that is what tools.byAgent is
		// for — but these two answer SELF, so a person calling one is pointed at
		// the unscoped door rather than served a view they cannot hold.
		// Otherwise the endpoint would answer "what does agent X hold" through a
		// door whose whole point is that only X can ask it.
		"tools_mine": "this door is agent-scoped; a person uses tools.byAgent",
		"tool_show":  "this door is agent-scoped; a person uses tools.get",
		// S55 T10: the loop-control / budget / watchdog arms — a person's half
		// of every one of these is the socket's own door, by name. The agent
		// arm is the only arm; a person calling it is refused to the door
		// that answers a person (the loop_list precedent, carried).
		"loop_control_state": "the socket already speaks loop.control for a person",
		"loop_request_stop":  "the socket already speaks loop.requestStop for a person",
		"budget_list":        "the socket already speaks budget.list for a person",
		"budget_show":        "the socket already speaks budget.show for a person",
		"budget_effective":   "the socket already speaks budget.effective for a person",
		"budget_spend":       "the socket already speaks spend.report for a person",
		"watchdog_list":      "the socket already speaks watchdog.list for a person",

		// S57 T9 (VG19): the nine gate tools are agent-scoped in BOTH
		// directions. The six reads answer the step's OWN scope (the
		// component and loop from the step's stamp) — a person has no step,
		// so the person's half is T8's UNSCOPED socket read, by name. The
		// three twins are the agent's permitted acts, and the person's half
		// of each is the person's OWN door: a gate proposal is filed by a
		// person through proposal.create (the gate kind), and a DoD mark by
		// a person is dod.mark — the person's word, with the person's
		// evidence. Never the agent's tools.
		"verify_list":               "the socket already speaks verify.list for a person (unscoped)",
		"verify_show":               "the socket already speaks verify.show for a person (unscoped)",
		"verify_runs":               "the socket already speaks verify.runs for a person (unscoped)",
		"mergerule_show":            "the socket already speaks mergerule.get for a person (unscoped)",
		"mergerule_preview":         "the socket already speaks mergerule.preview for a person (unscoped)",
		"dod_list":                  "the socket already speaks dod.list for a person (unscoped)",
		"verify_propose_add":        "a gate proposal is a person's filing — proposal.create with kind verify_command is the person's door",
		"mergerule_propose_tighten": "a merge-rule proposal is a person's filing — proposal.create with kind merge_rule is the person's door",
		"dod_mark":                  "the socket already speaks dod.mark for a person",

		// S59 T7 (EV16/EV17): the five environment tools are agent-scoped in
		// BOTH directions, the gate family's shape carried. The three reads
		// answer the step's OWN tree (the component from the step's stamp),
		// and secretref_list answers the agent's names-only projection — a
		// person has no step and holds the unscoped view, so a person
		// reaching one is pointed at T6's door by name rather than served
		// the agent's answer (which would make the endpoint an oracle for
		// the agent's scope, and hide `source` from the one caller entitled
		// to it). The propose twin is the agent's permitted ask; a person
		// tightens directly — environment.update is the person's door — and
		// a person filing the kind themselves files it through
		// proposal.create, never through a tool door.
		"environment_list":             "the socket already speaks environment.list for a person (unscoped, and a person sees origin and legacy in full)",
		"environment_show":             "the socket already speaks environment.show for a person (unscoped)",
		"environment_effective":        "the socket already speaks environment.effective for a person (unscoped — name the component you mean)",
		"secretref_list":               "the socket already speaks secretref.list for a person — a person is the one caller secretref.list's source is written for (EV7)",
		"environment_propose_restrict": "tightening an environment is a person's own edit — environment.update is the person's door; to file the ask as a row, proposal.create with kind environment_restrict is the person's door",

		// S49 T7 (AN9): the five automation tools are agent-scoped in BOTH
		// directions, the environment family's shape carried. The four reads
		// answer the step's plane through the SAME doors a person holds
		// unscoped — a person has no step, and the tool would answer them
		// exactly the socket's own bytes they can already ask the socket
		// for, so they are pointed at the standing door by name instead. The
		// propose twin is the agent's permitted ask; a person who wants an
		// automation off switches it off themselves — trigger.pause (and
		// the family's own off-doors) is the person's door — and to file the
		// kind as a row, proposal.create with kind automation_disable is.
		"automation_list":            "the socket already speaks automation.list for a person (unscoped, and archived is theirs to ask for)",
		"automation_show":            "the socket already speaks automation.show for a person (unscoped)",
		"automation_fires":           "the socket already speaks automation.fires for a person (unscoped)",
		"automation_health":          "the socket already speaks automation.health for a person (unscoped)",
		"automation_propose_disable": "switching an automation off is a person's own act — trigger.pause and the family's off-doors are the person's door; to file the ask as a row, proposal.create with kind automation_disable is the person's door",

		// S50 T5 (RM8): the three roadmap tools are agent-scoped in BOTH
		// directions, the automation family's shape carried. The two reads
		// answer the step's plane through the SAME doors a person holds
		// unscoped — a person has no step, and the tool would answer them
		// exactly the socket's own bytes they can already ask the socket
		// for, so they are pointed at the standing doors by name instead
		// (roadmap.show and roadmap.history, which this tool answers in
		// one call). The propose twin is the agent's permitted ask; a
		// person who wants an idea on the roadmap adds it themselves —
		// roadmap.add is the person's door — and to file the kind as a
		// row, proposal.create with kind roadmap_idea is.
		"roadmap_list":         "the socket already speaks roadmap.board for a person (unscoped — filters are theirs to set)",
		"roadmap_show":         "the socket already speaks roadmap.show and roadmap.history for a person (unscoped — this tool only saves the second call)",
		"roadmap_propose_idea": "adding a roadmap idea is a person's own act — roadmap.add is the person's door; to file the ask as a row, proposal.create with kind roadmap_idea is the person's door",

		// S27 T3 (CN7): the graph's two edges, the same rule in the same
		// words — a person who wants an item to wait says it at their own
		// door, and the edge is recorded under them, not under a step.
		"roadmap_depend":   "making an item wait for another is a person's own act — roadmap.depend is the person's door; use it, and the edge is authored by you, not by a step",
		"roadmap_undepend": "removing a dependency edge is a person's own act — roadmap.undepend is the person's door; use it, and the graph changes under your name, not a step's",

		// S51 T5 (TM6): the three bug tools are agent-scoped in BOTH
		// directions, the roadmap family's shape carried. The two reads
		// answer the step's backlog through the SAME doors a person holds
		// unscoped (triage.board, triage.show), so a person is pointed at
		// them by name. bug_file is the agent's permitted filing; a person
		// who wants a bug filed files it — triage.file is the person's
		// door, under themselves, and the claim reads as theirs.
		"bug_list": "the socket already speaks triage.board for a person (unscoped — filters are theirs to set)",
		"bug_show": "the socket already speaks triage.show for a person (unscoped — the item in full is theirs to ask)",
		"bug_file": "filing a bug claim is a person's own act — triage.file is the person's door; use it, and the claim is filed under you, not under a step (TM6)",

		// S53 T3 (HV4): the two history reads answer the step's question
		// through the SAME answers the person's socket doors run — a person
		// has no step, and history.list/history.show are their own doors
		// unscoped, so a person asking here is pointed at the socket by
		// name (and history.export, the third door of the family, was
		// never a tool at all — HV5 keeps it the person's alone).
		"history_list": "the socket already speaks history.list for a person (unscoped — filters are theirs to set)",
		"history_show": "the socket already speaks history.show for a person (unscoped — the entry in full is theirs to ask)",

		// S54 T4 (SP4, T0's visibility ruling): index_status is the step's
		// own-root view — scope is the step's stamp, not a param (OL3). A
		// person has no step and holds the WHOLE panel: platform.status
		// answers every root the daemon knows, operator fields included.
		// Serving the step's projection to a person would both hide those
		// fields from the one caller entitled to them and invent a scope.
		"index_status": "this door is scoped to the step's own root; a person uses platform.status, which answers every root the daemon knows",
	}[name]
	if !ok {
		return nil
	}
	return &Failure{Sentence: fmt.Sprintf("refused: %s — this is the person's door, not the step's (T15)", why)}
}

// ownTask is OL14's agent-scope rule (identity, stamped not argued): a loop
