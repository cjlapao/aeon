package daemon

// records.go — the management catalog's ONE dispatcher (S38 T9, OL2/OL3).
//
// The shape is the rule: the catalog is data (`internal/records`), the tools
// project it, the socket projects it (T15), and THIS switch is the only code
// that talks to the store about goals, briefs, epics, attachments, the
// read-only loop views, the gate reads and the execution-environment reads.
// One switch, one decode per action with unknown fields refused at the door,
// and — the security core — every identity a record carries (project,
// component, source loop, source step, author) is read from the STEP whose id
// rides the context, never from the input the model typed (OL3). The service
// itself, its identity types and its seams are records_service.go; the family
// arms live in records_<family>.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// Do runs one catalog action as `by`. The switch is the whole surface; each
// arm decodes a typed input (unknown fields REFUSED) and calls the store.
func (r *RecordsService) Do(ctx context.Context, by Actor, name string, input json.RawMessage) (any, *Failure, error) {
	if !catalogKnows(name) {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %q is not a catalog action — the catalog is the whole vocabulary", name)}, nil
	}
	var ident Identity
	if by.Kind == "user" {
		// The person's half of the two doors (OL10). A person carries no
		// step — there is no row to resolve identity through — and the
		// person owns the org, so loop-ownership checks stand down and the
		// filters in params do the scoping. What a person may NOT do
		// through this door is refused by name, with the door that answers
		// it, never silently emptied.
		if f := personDoorRefusal(name); f != nil {
			return nil, f, nil
		}
		ident = Identity{User: true, Agent: by.ID}
	} else {
		var f *Failure
		if ident, f = r.Identify(ctx); f != nil {
			return nil, f, nil
		}
	}
	// S58 T6 (SV16): the supervision family — five reads, one file, one
	// own-view rule. The family is named here and answered there, so the
	// scope guard (identity, not parameter) lives beside its five actions
	// and not scattered through this switch.
	if out, f, err, handled := r.supervisionRead(ctx, name, ident, input); handled {
		return out, f, err
	}
	switch name {
	case "component_list":
		var in struct {
			RootID string `json:"root_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		if in.RootID == "" {
			return ok1(r.st.ListComponents(store.ComponentFilter{}))
		}
		return ok1(r.st.ListComponents(store.ComponentFilter{ParentID: in.RootID}))
	case "component_show":
		var in struct {
			ComponentID string `json:"component_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		c, err := r.st.ReadComponent(in.ComponentID)
		if err != nil {
			return nil, notFoundFailure(in.ComponentID, err), nil
		}
		return c, nil, nil
	case "component_create":
		var in struct {
			Name             string `json:"name"`
			Root             string `json:"root"`
			RootID           string `json:"root_id"`
			ShortDescription string `json:"short_description"`
			Description      string `json:"description"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		if in.Name == "" || in.Root == "" {
			return nil, &Failure{Sentence: "refused: a component needs a name and a workspace root"}, nil
		}
		return ok1(r.st.CreateComponent(store.NewComponent{Name: in.Name, Slug: slugify(in.Name), Root: in.Root,
			ParentID: in.RootID, ShortDescription: in.ShortDescription, Description: in.Description}))
	case "component_update":
		var in struct {
			ComponentID      string `json:"component_id"`
			Name             string `json:"name"`
			ShortDescription string `json:"short_description"`
			Description      string `json:"description"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.UpdateComponent(store.ComponentEdit{ID: in.ComponentID, Name: in.Name,
			ShortDescription: in.ShortDescription, Description: in.Description}))
	case "component_promote":
		var in struct {
			ComponentID      string `json:"component_id"`
			Name             string `json:"name"`
			ShortDescription string `json:"short_description"`
			Description      string `json:"description"`
			Icon             string `json:"icon"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.PromoteComponent(in.ComponentID, store.PromoteInput{Name: in.Name, ShortDescription: in.ShortDescription,
			Description: in.Description, Icon: in.Icon}))
	case "component_attach":
		var in struct {
			ComponentID string `json:"component_id"`
			ParentID    string `json:"parent_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.AttachComponent(in.ComponentID, in.ParentID))
	case "component_detach":
		var in struct {
			ComponentID string `json:"component_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.DetachComponent(in.ComponentID))
	case "component_archive":
		var in struct {
			ComponentID string `json:"component_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.ArchiveComponent(in.ComponentID))
	case "component_restore":
		var in struct {
			ComponentID string `json:"component_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.RestoreComponent(in.ComponentID))

	case "epic_list":
		var in struct {
			RootID string `json:"root_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.ListEpics(in.RootID))
	case "epic_show":
		var in struct {
			EpicID string `json:"epic_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		e, err := r.st.GetEpic(in.EpicID)
		if err != nil {
			return nil, notFoundFailure(in.EpicID, err), nil
		}
		return e, nil, nil
	case "epic_create":
		// S55 T8 (LC13): the input no longer names a budget field — the
		// epic's budget is the budget row (scope epic), set through
		// budget.set, and a model-typed budget figure here is a budget
		// write an agent may not make (the decoder refuses the field as
		// unknown before it reaches the store). The view's budgetUsd is
		// derived from the row, not stored beside the epic.
		var in struct {
			RootID     string `json:"root_id"`
			Title      string `json:"title"`
			Objective  string `json:"objective"`
			Owner      string `json:"owner"`
			TargetDate string `json:"target_date"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.CreateEpic(store.NewEpic{ComponentID: in.RootID, Title: in.Title, Objective: in.Objective,
			Owner: in.Owner, TargetDate: in.TargetDate, SourceStep: &ident.StepID}))
	case "epic_update":
		// S55 T8 (LC13): the budget moved to the budget table — an update
		// here changes the plan, never the cap (a budget.set is the cap's
		// door); a budget figure arriving is refused as unknown, not
		// ignored.
		var in struct {
			EpicID     string `json:"epic_id"`
			Title      string `json:"title"`
			Objective  string `json:"objective"`
			Owner      string `json:"owner"`
			TargetDate string `json:"target_date"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.UpdateEpic(store.EpicEdit{ID: in.EpicID, Title: in.Title, Objective: in.Objective,
			Owner: in.Owner, TargetDate: in.TargetDate}))

	case "goal_list":
		var in struct {
			RootID string `json:"root_id"`
			EpicID string `json:"epic_id"`
			State  string `json:"state"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.ListGoals(store.GoalFilter{ComponentID: in.RootID, EpicID: in.EpicID, ReviewState: in.State}))
	case "goal_show":
		var in struct {
			GoalID  string `json:"goal_id"`
			Version *int   `json:"version"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		// A goal that is nobody is a DOMAIN answer, not an infrastructure
		// failure (DB19) — the sentence, not the error channel.
		var (
			g   store.Goal
			err error
		)
		if in.Version != nil {
			g, err = r.st.GetGoalVersion(in.GoalID, *in.Version)
		} else {
			g, err = r.st.GetGoal(in.GoalID)
		}
		if err != nil {
			return nil, notFoundFailure(in.GoalID, err), nil
		}
		return g, nil, nil
	case "goal_create":
		return r.goalCreate(ident, by, input)
	case "goal_update":
		return r.goalUpdate(ident, by, input)
	case "goal_submit_review":
		var in struct {
			GoalID string `json:"goal_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		goal, err := r.st.GetGoal(in.GoalID)
		if err != nil {
			return nil, notFoundFailure(in.GoalID, err), nil
		}
		if !ident.User && goal.ComponentID != ident.RootID {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: goal %s belongs to another root — this step acts within %s", in.GoalID, ident.RootID)}, nil
		}
		g, rf, err := r.st.SubmitGoalForReview(in.GoalID)
		if err != nil {
			return nil, nil, err
		}
		if rf != nil {
			return nil, &Failure{Sentence: rf.Sentence}, nil
		}
		return g, nil, nil

	case "brief_show":
		if ident.User {
			// The person's brief read (S38 T16): it NAMES the loop, because
			// a person carries no loop. Slug or id — the same coordinate the
			// brief decision method takes (one resolver, never two).
			var pin struct {
				Loop      string `json:"loop"`
				Component string `json:"component"`
			}
			if f, err := decode(input, &pin); f != nil || err != nil {
				return nil, f, err
			}
			if pin.Loop == "" {
				return nil, &Failure{Sentence: "refused: a person reading a brief names the loop — slug or id (a person carries no loop to read from)"}, nil
			}
			sc, f := r.personLoopScope(pin.Loop, pin.Component)
			if f != nil {
				return nil, f, nil
			}
			b, err := r.st.LatestBrief(sc)
			if err != nil {
				return nil, &Failure{Sentence: fmt.Sprintf("loop %s has no brief yet — its contract is filed with brief_create", sc.LoopSlug)}, nil
			}
			return b, nil, nil
		}
		var stepIn struct {
			Loop string `json:"loop"`
		}
		if f, err := decode(input, &stepIn); f != nil || err != nil {
			return nil, f, err
		}
		if stepIn.Loop != "" {
			return nil, &Failure{Sentence: "refused: a step reads ITS OWN loop's brief — the loop is stamped, not argued (OL3)"}, nil
		}
		b, err := r.st.LatestBrief(store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot})
		if err != nil {
			return nil, &Failure{Sentence: fmt.Sprintf("loop %s has no brief yet — its contract is filed with brief_create", ident.LoopSlug)}, nil
		}
		return b, nil, nil
	case "brief_create":
		var in struct {
			GoalID      string   `json:"goal_id"`
			Scope       string   `json:"scope"`
			OutOfScope  string   `json:"out_of_scope"`
			Acceptance  []string `json:"acceptance_criteria"`
			Constraints []string `json:"constraints"`
			TestPlan    string   `json:"test_plan"`
			Document    string   `json:"document"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		if in.Scope == "" {
			return nil, &Failure{Sentence: "refused: a brief needs a scope — the one line that says what this loop is for"}, nil
		}
		sc := store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot}
		brief, err := r.st.CreateBrief(sc, store.NewBrief{GoalID: in.GoalID, Scope: in.Scope,
			OutOfScope: in.OutOfScope, Acceptance: in.Acceptance, Constraints: in.Constraints,
			TestPlan: in.TestPlan, SourceStep: &ident.StepID})
		if err != nil {
			return nil, nil, err
		}
		out := map[string]any{"brief": brief}
		if in.Document != "" {
			if f := r.pinWorkspaceDocument(sc, ident, in.Document, by.ID); f != nil {
				return nil, f, nil
			}
			a, rf, err := r.st.Attach(sc, "brief", ident.LoopSlug, in.Document, &ident.StepID)
			if err != nil {
				return nil, nil, err
			}
			if rf != nil {
				return nil, &Failure{Sentence: rf.Sentence}, nil
			}
			out["attachment"] = a
		}
		return out, nil, nil

	case "attach":
		var in struct {
			Subject  subjectInput `json:"subject"`
			Document string       `json:"document"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		if f := r.ownSubject(ident, in.Subject); f != nil {
			return nil, f, nil
		}
		sc := store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot}
		if f := r.pinWorkspaceDocument(sc, ident, in.Document, by.ID); f != nil {
			return nil, f, nil
		}
		a, rf, err := r.st.Attach(sc, in.Subject.Kind, in.Subject.ID, in.Document, &ident.StepID)
		if err != nil {
			return nil, nil, err
		}
		if rf != nil {
			return nil, &Failure{Sentence: rf.Sentence}, nil
		}
		return a, nil, nil
	case "attachments_list":
		var in struct {
			Subject subjectInput `json:"subject"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.ListAttachments(in.Subject.Kind, in.Subject.ID))

	case "task_list":
		var in struct {
			Component string `json:"component"`
			Status    string `json:"status"`
			Manual    *bool  `json:"manual"`
			Assignee  string `json:"assignee"`
			Goal      string `json:"goal"`
			Parent    string `json:"parent"`
			Limit     int    `json:"limit"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.ListTaskRows(store.TaskFilter{
			ComponentID: in.Component, Status: in.Status, Manual: in.Manual,
			Assignee: in.Assignee, GoalID: in.Goal, ParentTaskID: in.Parent, Limit: in.Limit}))
	case "task_show":
		var in struct {
			ID string `json:"id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		t, err := r.st.GetTaskByID(in.ID)
		if err != nil {
			return nil, notFoundFailure(in.ID, err), nil
		}
		return t, nil, nil
	case "task_create":
		return r.taskCreate(ident, by, input)
	case "task_update":
		return r.taskUpdate(ident, input)
	case "task_set_status":
		return r.taskSetStatus(ident, input)
	case "task_bump_counter":
		return r.taskBump(ident, input)

	case "loop_list":
		var in struct {
			RootID string `json:"root_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		loops, err := r.st.ListLoops(store.ComponentScope{ComponentRoot: ident.ComponentRoot})
		if err != nil {
			return nil, nil, err
		}
		if in.RootID == "" || in.RootID == ident.RootID {
			return loops, nil, nil
		}
		return []store.Loop{}, nil, nil // this component answers to one root; another filter matches nothing
	case "loop_get":
		var in struct {
			Slug string `json:"slug"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		return ok1(r.st.GetLoop(store.Scope{LoopSlug: in.Slug, ComponentRoot: ident.ComponentRoot}))

	case "run_start":
		// S39 T10 (CR5/D5): the agent's run door. Identity is the STEP's —
		// the loop is where the step lives, the actor is the handler name —
		// and the input may carry neither, because a schema with
		// additionalProperties:false makes the attempt a parse error.
		if r.run == nil {
			return nil, &Failure{Sentence: "refused: run_start is not available in this process"}, nil
		}
		var in struct {
			Blueprint string            `json:"blueprint"`
			Goal      string            `json:"goal"`
			Task      string            `json:"task"`
			With      map[string]string `json:"with"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		if strings.TrimSpace(in.Blueprint) == "" {
			return nil, &Failure{Sentence: "refused: run_start needs a blueprint to start — the name, not a guess"}, nil
		}
		l, err := r.st.GetLoop(store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot})
		if err != nil {
			return nil, nil, err
		}
		out, err := r.run(ctx, runInput{
			LoopID: l.ID, Blueprint: in.Blueprint, GoalID: in.Goal, TaskID: in.Task,
			With: in.With, ByKind: "agent", ByID: ident.Agent,
		})
		if err != nil {
			return nil, nil, err
		}
		// A domain refusal is the MODEL's words, returned as a spoken
		// sentence — the same map the human door answered, one renderer.
		if m, ok := out.(map[string]any); ok {
			if r, refusedNow := m["refused"]; refusedNow {
				return nil, &Failure{Sentence: runRefusalSentence(r)}, nil
			}
		}
		return out, nil, nil

	case "loop_control_state":
		return r.loopControlState(ctx, ident, input)
	case "loop_request_stop":
		return r.loopRequestStop(ctx, ident, input)
	case "budget_list":
		return r.budgetList(ctx, input)
	case "budget_show":
		return r.budgetShow(ctx, input)
	case "budget_effective":
		return r.budgetEffective(ctx, ident, input)
	case "budget_spend":
		return r.budgetSpend(ctx, input)
	case "watchdog_list":
		return r.watchdogList(ctx, input)

	case "seed_status":
		// S43-extra T6 (SD10): the ONE seed tool, and it is a READ. It reads
		// through the same *seed.Engine the socket's seed.status reads,
		// mapping the result with protocol.SeedStatusView — the one mapping
		// the socket also calls — so the two doors answer identically (the
		// parity test pins it field for field). A nil engine (a seedless
		// mount, V13) refuses, never answers zero. A seed write is never a
		// tool: this is the only seed catalog action, and it reads.
		var in struct {
			Set string `json:"set"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err
		}
		if r.seedEngine == nil {
			return nil, &Failure{Sentence: "refused: seed status needs the seed engine, and this build did not link it — the seed surface is absent for the same reason (V13)"}, nil
		}
		states, err := r.seedEngine.Status(ctx, in.Set)
		if err != nil {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: seed status could not be read: %v", err)}, nil
		}
		return protocol.SeedStatusView(states), nil, nil

	// — policy & proposals (S45 T7, PM9): the seven rows live in
	// records_policy.go — the five reads answer through the socket's own
	// read functions (protocol.PolicyListAnswer & friends, the seed_status
	// precedent generalised), the two proposes through proposal.create's
	// own path: shared validator, then the shared ProposalStore. The switch
	// still names every catalog action; implementation belongs beside its
	// family. —
	case "policy_list":
		return r.policyList(ctx, ident, input)
	case "policy_show":
		return r.policyShow(ctx, ident, input)
	case "policy_why":
		return r.policyWhy(ctx, ident, input)
	case "policy_in_force":
		return r.policyInForce(ctx, ident, input)
	case "proposal_list":
		return r.proposalList(ctx, ident, input)
	case "policy_propose_add":
		return r.policyProposeAdd(ctx, by, input)
	case "policy_propose_remove":
		return r.policyProposeRemove(ctx, by, input)

	// — autonomy (S56 T8, AL11): the family lives in records_autonomy.go.
	// The three reads answer through the SAME AutonomyService the socket's
	// autonomy.* methods use; the two proposes create INERT rows through
	// proposal.create's own path (shared validator, then the shared
	// ProposalStore) — never grants. The switch names every catalog
	// action; implementation belongs beside its family. —
	case "autonomy_list":
		return r.autonomyList(ctx, ident, input)
	case "autonomy_show":
		return r.autonomyShow(ctx, input)
	case "autonomy_effective":
		return r.autonomyEffective(ctx, input)
	case "autonomy_propose_revoke":
		return r.autonomyProposeRevoke(ctx, by, input)
	case "autonomy_propose_deny":
		return r.autonomyProposeDeny(ctx, by, input)

	// — mcp own-view (S46 T8, MG8/MG9/MC2): the family lives in
	// records_mcp.go. The three reads forward to the cmd/aeon seam's
	// SCOPED entries with self stamped from the step (OL3); the two
	// proposes check visibility and shape over there and create INERT
	// rows through THIS file's one create path (caps, shared validator,
	// stamped author). The person half is turned back before the switch
	// by personDoorRefusal — these doors answer the agent, nobody else. —
	case "mcp_servers":
		return r.mcpMineServers(ctx, ident, input)
	case "mcp_grants":
		return r.mcpMineGrants(ctx, ident, input)
	case "mcp_status":
		return r.mcpMineStatus(ctx, ident, input)
	case "mcp_propose_remove_server":
		return r.mcpMineProposeRemove(ctx, by, ident, input)
	case "mcp_propose_revoke_grant":
		return r.mcpMineProposeRevoke(ctx, by, ident, input)

	// — tools own view (S47 T7, TC9/MC2/OL3): the family lives in
	// records_tools.go. Both arms forward to the SAME catalog service the
	// person's tools.byAgent reads through, with self taken from the step's
	// stamp and never from a param. The person half is turned back before the
	// switch by personDoorRefusal — these two doors answer the agent, nobody
	// else, and a person holding the endpoint must not be able to walk them
	// one slug at a time. —
	case "tools_mine":
		return r.toolsMine(ctx, ident, input)
	case "tool_show":
		return r.toolShow(ctx, ident, input)

	// — blueprint (S48 T10, BP9): the family lives in records_blueprint.go.
	// The four reads forward to the SAME services the person's socket doors
	// answer through (the standing validate seam, T7's one read service) —
	// one implementation, the parity walk proves it; the fifth arm files
	// the INERT blueprint_save row through the shared create path (shared
	// validator, per-agent cap, stamped author). There is no save/delete/
	// duplicate/revert arm and there never is one: an agent may PROPOSE,
	// never write (BP9; T8's guard refuses such a tool by name). —
	case "blueprint_list":
		return r.blueprintList(ctx, ident, input)
	case "blueprint_show":
		return r.blueprintShow(ctx, ident, input)
	case "blueprint_validate":
		return r.blueprintValidate(ctx, ident, input)
	case "blueprint_lint":
		return r.blueprintLint(ctx, ident, input)
	case "blueprint_propose":
		return r.blueprintPropose(ctx, by, input)
	// — gate plane (S57 T9, VG19): the family lives in records_verify.go.
	// The six reads forward to the SAME VerifyStore / MergeRuleStore /
	// DoDStore seams the socket's twenty-two human doors answer through,
	// with the scope stamped from the step (OL3) and a foreign scope
	// refused; the three twins are the agent's permitted acts — two
	// proposes that create INERT rows through proposal.create's own path
	// (shared validator, then the shared ProposalStore) and one DoD mark
	// whose met half is bound to a passing run of the loop's own, on the
	// current head. The person half is turned back before the switch by
	// personDoorRefusal — these doors answer the agent, nobody else. —
	case "verify_list":
		return r.verifyList(ctx, ident, input)
	case "verify_show":
		return r.verifyShow(ctx, ident, input)
	case "verify_runs":
		return r.verifyRuns(ctx, ident, input)
	case "mergerule_show":
		return r.mergeruleShow(ctx, ident, input)
	case "mergerule_preview":
		return r.mergerulePreview(ctx, ident, input)
	case "dod_list":
		return r.dodList(ctx, ident, input)
	case "verify_propose_add":
		return r.verifyProposeAdd(ctx, by, ident, input)
	case "mergerule_propose_tighten":
		return r.mergeruleProposeTighten(ctx, by, ident, input)
	case "dod_mark":
		return r.dodMarkAsAgent(ctx, by, ident, input)

	// — execution environment (S59 T7, EV16/EV7): the family lives in
	// records_environment.go. The three environment reads forward to the
	// SAME protocol.EnvironmentService the person's thirteen doors answer
	// through, and secretref_list to the SAME SecretRefService the person's
	// seven answer through — with the answer projected to the type that has
	// no `source` field at all (EV7: a reference is a pointer, and where the
	// credential lives is not a fact a model gets). The scope is stamped from
	// the step, never typed; a foreign component is refused before the read.
	// The fifth arm files the INERT environment_restrict row — a TIGHTENING
	// only, refused by execenv.Restricts at create and again at commit —
	// through proposal.create's own path (shared validator, shared
	// ProposalStore, the per-agent cap); the agent half only, so a person
	// hitting this door is turned back before the switch by
	// personDoorRefusal. There is no create/update/archive/restore/bind/
	// unbind/teardown/secretref-write arm to reach — those doors are the
	// person's alone (T6's guard refuses such a tool by name). —
	case "environment_list":
		return r.environmentList(ctx, ident, input)
	case "environment_show":
		return r.environmentShow(ctx, ident, input)
	case "environment_effective":
		return r.environmentEffective(ctx, ident, input)
	case "secretref_list":
		return r.secretrefList(ctx, ident, input)
	case "environment_propose_restrict":
		return r.environmentProposeRestrict(ctx, by, input)

	// — S49 T7 (AN9): the automation family's five (records_automation.go).
	// The four reads forward through protocol.AnswerAutomation*, the ONE
	// rendering the person's socket doors run (rule 4); the propose arm files
	// the INERT automation_disable row through the shared proposals path
	// (validateProposal = T6's desk-then-store function, TD3; the per-agent
	// cap; the author STAMPED). There is no create/update/archive/
	// pause/resume/fire-now arm to reach — those doors are the person's
	// alone (T4's writes, T5's guards, AU1's locks). —
	case "automation_list":
		return r.automationList(ctx, ident, input)
	case "automation_show":
		return r.automationShow(ctx, ident, input)
	case "automation_fires":
		return r.automationFires(ctx, ident, input)
	case "automation_health":
		return r.automationHealth(ctx, ident, input)
	case "automation_propose_disable":
		return r.automationProposeDisable(ctx, by, input)

	// — S50 T5 (RM8): the roadmap family's three (records_roadmap.go). The
	// two reads forward through protocol.AnswerRoadmap*, the ONE rendering
	// the person's socket doors run (rule 4 — roadmap_list twins
	// roadmap.board, roadmap_show answers the item beside its trail); the
	// propose arm files the INERT roadmap_idea row through the shared
	// proposals path (validateProposal = the seam's desk-then-store
	// function, TD3; the per-agent cap; the author STAMPED). There is no
	// update/cancel/estimate/priority/ideate/plan/materialize arm to reach
	// — those eight doors are the person's alone (T2's writes, T4's
	// guards). —
	case "roadmap_list":
		return r.roadmapList(ctx, ident, input)
	case "roadmap_show":
		return r.roadmapShow(ctx, ident, input)
	case "roadmap_propose_idea":
		return r.roadmapProposeIdea(ctx, by, input)

	// — S27 T3 (CN7, P6): the graph's two edges. Both forward through
	// protocol.AnswerRoadmapDepend/Undepend — the ONE shared answers the
	// person's roadmap.depend/roadmap.undepend doors run (rule 4), the
	// author the step's stamped slug (an agent may say "I wait for X";
	// the store's laws — same root, no cycle, no rung moves — are the
	// door, verbatim, and the LADDER still has no arm to reach). —
	case "roadmap_depend":
		return r.roadmapDepend(ctx, ident, input)
	case "roadmap_undepend":
		return r.roadmapUndepend(ctx, ident, input)

	// — S51 T5 (TM6): the bug family's three (records_triage.go). The two
	// reads forward through the SAME answers the person's socket doors run
	// (rule 4 — bug_list twins triage.board through the very closure the
	// Table seam holds, bug_show answers through protocol.AnswerTriageShow);
	// the filing arm files an UNVALIDATED, priority-unknown claim through
	// the SAME CreateAudited triage.file runs — filer stamped as the agent
	// slug, the counted cap (triage.MaxOpenClaimsPerAgent) refused BEFORE
	// the write, a person session refused by name up in Do's person half.
	// There is no set-priority / confirm-duplicate / validate / handoff arm
	// to reach — those doors are the person's alone (T2's writes, T4's
	// guards, TM6). —
	case "bug_list":
		return r.triageList(ctx, ident, input)
	case "bug_show":
		return r.triageShow(ctx, ident, input)
	case "bug_file":
		return r.triageFile(ctx, ident, input)

	// — S52 T4 (LM5/LM7): the memory family's four (records_learnings.go).
	// learning_query is the agent's browse of its readable set — the SAME
	// MineQuery the learnings.forAgent door answers through, tags as the
	// only extra filter (LM5: one merged set, one ordering, one embedding
	// budget; no Scope or ScopeKey is nameable here, the set is resolved
	// from the stamped identity). learning_show reads one row and refuses
	// out-of-scope with the SAME sentence as nonexistent — a difference
	// would be an oracle. learning_add files observed or assumed into the
	// agent's OWN scope, the key stamped from identity, capped per agent
	// at learnings.MaxSelfLogAddsPerAgent counted off the audit ledger
	// BEFORE the write; told is unsayable here by schema and by LM4.
	// learning_propose_retract files the INERT learning_retract ask —
	// readable-checked at create so an out-of-scope id is refused as
	// "no such learning" exactly like a false one, then the shared
	// proposals path (validateProposal = the seam's desk-then-store
	// function, TD3; the per-agent open cap). There is NO retract,
	// restore, edit or relay arm to reach — retract and restore are the
	// person's doors (LM7/LM8, T3's guards refuse each such name), and
	// told/relayed provenance has no door an agent can knock on (LM4). —
	case "learning_query":
		return r.learningQuery(ctx, ident, input)
	case "learning_show":
		return r.learningShow(ctx, ident, input)
	case "learning_add":
		return r.learningAdd(ctx, ident, input)
	case "learning_propose_retract":
		return r.learningProposeRetract(ctx, by, ident, input)

	// — S53 T3 (HV4): the history family's two (records_history.go). Both
	// reads forward through protocol.AnswerHistory*, the ONE rendering the
	// person's history.list/history.show doors run (rule 4), over the SAME
	// *history.Store facade the Table seam holds. There is no export arm
	// to reach — history.export stays the person's door (HV5, T2's guard
	// refuses a history_export tool by name) — and no coverage or
	// vocabulary arm either (HV4: those two doors stay the app's). —
	case "history_list":
		return r.historyList(ctx, ident, input)
	case "history_show":
		return r.historyShow(ctx, ident, input)
	// — S63 T6 (RT11, standing rule 2): the scorecard family's three
	// (records_scorecard.go). All forward to the SAME ScorecardService and
	// DigestService the person's ten doors answer through (rule 4); the
	// subject is the step's stamped identity (agent slug / root id), the
	// input carries only the window, and the person's per-child breakdown
	// is dropped from the component twin — a roster by the back door would
	// rank (T6.2). A person session is turned back before the switch by
	// personDoorRefusal, pointed at scorecard.agent / scorecard.component /
	// digest.learned. There is no retro/backfill arm to reach — no write
	// exists in this family (RT11). —
	case "scorecard_mine":
		return r.scorecardMine(ctx, ident, input)
	case "scorecard_component":
		return r.scorecardComponent(ctx, ident, input)
	case "digest_learned":
		return r.digestLearned(ctx, ident, input)
	// — S27 T8 (CN23, standing rule 2): the scheduler family's four reads
	// (records_continuous.go). All forward to the SAME ContinuousService the
	// person's four read doors answer through; the repository is the step's
	// stamped component root (Identify), the shared filter grammar is
	// protocol.ParseContinuousDecisionFilters, and the schemas carry no repo
	// field at all — a param that could only be overwritten is a param that
	// lies (OL3), and a foreign repository is absent, never named (MC2). A
	// person session is turned back before the switch by personDoorRefusal,
	// pointed at scheduler.status / .preview / .decisions / .attention.
	// There is NO control arm to reach — enable, disable, pause, resume,
	// root.set, root.disable, enableTree and stopAll are human doors
	// (S27 CN2; cmd/aeon/continuous_guard_test.go holds the wire side). —
	case "scheduler_status":
		return r.schedulerStatus(ctx, ident, input)
	case "scheduler_preview":
		return r.schedulerPreview(ctx, ident, input)
	case "scheduler_decisions":
		return r.schedulerDecisions(ctx, ident, input)
	case "scheduler_attention":
		return r.schedulerAttention(ctx, ident, input)
	// — S60 T9 (MQ17, standing rule 2): the merge-queue family's five
	// (records_mergequeue.go). The three reads forward to the SAME
	// protocol.MergeQueueService the person's ten doors answer through,
	// filtered to the agent's own-view (the roots its steps have run in —
	// S52's structural source); mergequeue_dequeue is the plane's ONE
	// agent-direct write, own item only, queued/conflicted only; the
	// propose arm files the INERT mergequeue_enqueue row through the
	// shared proposals path (validateProposal = desk-then-facts, the ONE
	// ValidateEnqueue included, TD3; the per-agent cap; the ask STAMPED
	// agent, the committed row later stamped proposal, MQ14.3). There is
	// no enqueue/reorder/retry/land/settings arm to reach — those doors
	// are the person's alone (MQ13, MQ17, T8's guards). —
	case "mergequeue_list":
		return r.mergequeueList(ctx, ident, input)
	case "mergequeue_show":
		return r.mergequeueShow(ctx, ident, input)
	case "mergequeue_status":
		return r.mergequeueStatus(ctx, ident, input)
	case "mergequeue_dequeue":
		return r.mergequeueDequeue(ctx, ident, input)
	case "mergequeue_propose_enqueue":
		return r.mergequeueProposeEnqueue(ctx, by, ident, input)

	// — S54 T4 (SP4, T0's visibility ruling): the platform family's ONE arm
	// (records_platform.go). `index_status` is a read of the step's OWN root
	// composed over the same seams the person's `platform.status` composes
	// over, minus the operator-view fields — so the two doors cannot answer
	// one question twice. There is deliberately no control arm: no reindex,
	// no rebuild, no graph build/reparse/delete, and no proposal kind
	// twinned to any of them (SP3 — an operation spends a person's money).
	// T3's guards refuse each such name from this switch, the catalog, the
	// registry and the agent surfaces. —
	case "index_status":
		return r.indexStatus(ctx, ident, input)

	// — S61 T8 (GI17, records_charter.go): the goal-intake family's seven.
	// Five reads forward to the SAME door structs the person rings (rule 4);
	// the two asks file inert rows through the shared validator and the
	// desk's own payload bound, the intake ask's row stamped from the CALLING
	// LOOP's identity (intake_propose has no intakeId field to forge with).
	// The plane's four writes have no case here, no spec, no name in this
	// switch to add later (GI14; cmd/aeon/intake_amend_test.go re-pins it).
	case "charter_list":
		return r.charterListArm(ctx, ident, input)
	case "charter_show":
		return r.charterShowArm(ctx, ident, input)
	case "template_list":
		return r.templateListArm(ctx, ident, input)
	case "template_show":
		return r.templateShowArm(ctx, ident, input)
	case "intake_status":
		return r.intakeStatusArm(ctx, ident, input)
	case "charter_propose":
		return r.charterProposeArm(ctx, by, input)
	case "intake_propose":
		return r.intakeProposeArm(ctx, by, input)

	default:
		// Reachable only if the catalog and this switch disagree — and then
		// LOUDLY, because a silent default would mean a catalogued action
		// nobody implements.
		return nil, nil, fmt.Errorf("daemon: %s is in the catalog but not implemented", name)
	}
}

// Invoker is the tools-facing half: identify from the ctx, run as the
// step's agent, answer with indented JSON or the refusal's sentence. The
// step's identity is resolved ONCE, before Do — a call that cannot name its
// step never reaches the store.
func (r *RecordsService) Invoker() tools.RecordsInvoker {
	return func(ctx context.Context, tool string, input json.RawMessage) (string, bool) {
		by := Actor{Kind: "agent", ID: "agent"}
		if ident, f := r.Identify(ctx); f == nil {
			by = Actor{Kind: "agent", ID: ident.Agent}
		}
		out, f, err := r.Do(ctx, by, tool, input)
		if f != nil {
			return f.Sentence, true
		}
		if err != nil {
			return fmt.Sprintf("the records action %s failed: %v", tool, err), true
		}
		b, merr := json.MarshalIndent(out, "", "  ")
		if merr != nil {
			return fmt.Sprintf("the records action %s answered something that will not encode: %v", tool, merr), true
		}
		return string(b), false
	}
}
