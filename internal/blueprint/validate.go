package blueprint

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

type ValidationOptions struct {
	Mode   string
	Agents []string
	// Resolvers names the CLOSED resolver registry (handler.Resolvers in
	// production): names a blueprint may put in `agent:` that are not
	// handlers. `by-tech-tag` is a real check now (D13) — the registry
	// exists, so validation says so instead of deferring to a future slice;
	// a name that is neither a handler nor a resolver is invalid, never
	// unverifiable, because the reserve lists are complete (T10).
	Resolvers []string
	// Actions names the CLOSED code-action registry (action.Names() in
	// production — the CLI and the daemon both pass it, tests pass the
	// committed list): the registry ARRIVED with S16 (D15), so an
	// `action:` name outside it is now `invalid`, not `unverifiable`.
	// nil keeps the honest three-state answer: unverifiable, Slice S16 —
	// which is what a validator at rest without the registry must say,
	// never a guessed verdict.
	Actions []string
	// SkillKnown answers whether the CONFIGURED agent services register a
	// skill target (`<callee>/<skill-id>`). nil means agent services are not
	// configured on this process — a `skill:` target is then unverifiable
	// with a named remedy, never silently valid, and `loop.start` refuses the
	// lane (the structured refusal T11 promises). A configured registry that
	// answers false is invalid: the registry is closed, so this is a real
	// check, not a deferral (same argument as D13's flip).
	SkillKnown func(callee, skill string) bool
	// AgentTools answers a body's GRANTED tool allow-list (handler.ToolsFor
	// in production). It is what makes `emits:` checkable against OL9's
	// promise — a step may only emit what its handler was granted the tool
	// to file. nil is the honest third state (P31): a validator at rest
	// without the registry says Unverifiable emits-registry, never a
	// guessed valid and never a false invalid.
	AgentTools func(agent string) ([]string, bool)
}

// emitTool is the OL9 promise made checkable: the ONE mapping from an
// emitted entity to the catalog action that files it. Validation asks the
// handler's grant list for exactly these names.
var emitTool = map[string]string{
	"goal":  "goal_create",
	"epic":  "epic_create",
	"brief": "brief_create",
	"task":  "task_create",
}

// EmitTool is the ONE type→tool map, exported (S38 T12): the runtime names
// the call to make in its refusal through THIS function — a duplicated map
// is a second truth about what files what.
func EmitTool(entity string) (string, bool) {
	t, ok := emitTool[entity]
	return t, ok
}

// catalogEntities is the closed vocabulary `emits:`/`output:`/`requires:`
// speak. It is the catalog's entity half — a name outside it cannot be
// filed by anything, so load refuses rather than dispatch hoping.
var catalogEntities = []string{"goal", "epic", "brief", "task"}

// Future agent names are reserved by the derived standard blueprints. They
// become runnable only when S13 provides bodies. A misspelling is still invalid.
// The five names below have no body in the S13 corpus at all — they are not
// among the twenty-six originals. The eleven S13-authored agents that once
// sat here became registered bodies in T4-T8 and dropped out: a registered
// agent is checked against the registry (opts.Agents), which is where D13
// pointed. A misspelling is still invalid; these five stay unverifiable
// because their bodies genuinely do not exist yet, and deleting the ones
// that DO is the diff that proves the flip.
var futureAgents = []string{} // the list is the MECHANISM for the next named follow-up, not an inventory of the last one: empty today (all five shipped 2026-10-26), and TestTheThirdStateSurvivesAnEmptyFollowUpList pins that a name ADDED here still produces the unverifiable third state — deleting the branch would delete the state

func FutureAgents() []string { return append([]string(nil), futureAgents...) }

func Validate(b Blueprint, opts ValidationOptions) Report {
	mode := opts.Mode
	if mode == "" {
		mode = "standard"
	}
	r := Report{Blueprint: b.ID, SchemaVersion: b.SchemaVersion, Version: b.Version, Diagnostics: []Diagnostic{}}
	add := func(state DiagState, kind, path, msg, slice string) {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{State: state, Kind: kind, Path: path, Msg: msg, Slice: slice})
	}
	if b.ID == "" {
		add(Invalid, "required", "id", "id is required", "")
	} else {
		r.ValidCount++
	}
	if b.SchemaVersion != SupportedSchemaVersion {
		add(Invalid, "schema-version", "schemaVersion", fmt.Sprintf("supported schemaVersion is %d", SupportedSchemaVersion), "")
	} else {
		r.ValidCount++
	}
	if len(b.Lanes) == 0 {
		add(Invalid, "required", "lanes", "at least one lane is required", "")
	} else {
		r.ValidCount++
	}
	laneIDs := map[string]int{}
	for i, l := range b.Lanes {
		if l.ID == "" {
			add(Invalid, "required", fmt.Sprintf("lanes[%d].id", i), "lane id is required", "")
		} else if _, duplicate := laneIDs[l.ID]; duplicate {
			add(Invalid, "duplicate-lane", fmt.Sprintf("lanes[%s].id", l.ID), "lane id is duplicated", "")
		} else {
			laneIDs[l.ID] = i
			r.ValidCount++
		}
	}
	if b.OnAbort != "" {
		if _, ok := laneIDs[b.OnAbort]; !ok {
			add(Invalid, "unknown-abort-handler", "on_abort", "abort handler lane is absent", "")
		} else {
			r.ValidCount++
		}
	}
	// S39's three optional keys (CR1, CR3, CR6). Closed vocabularies like
	// every other key here; and `converse:` without `after: converse` is
	// invalid, not merely useless — a blueprint that names the agent who
	// talks after the run, on a run that ends the loop, would be a lie the
	// loader validated. `converse` deliberately does NOT consult
	// opts.Resolvers: a resolver is not an agent body, and the thing that
	// answers a conversation must be a handler.
	switch b.After {
	case "", AfterFinish, AfterConverse:
		if b.After != "" {
			r.ValidCount++
		}
	default:
		add(Invalid, "after-value", "after", fmt.Sprintf("after must be %q or %q, got %q", AfterConverse, AfterFinish, b.After), "")
	}
	if b.Converse != "" {
		if b.After != AfterConverse {
			add(Invalid, "converse-without-after", "converse", "converse names the agent that talks after a run; it needs after: converse", "")
		}
		switch {
		case contains(opts.Agents, b.Converse):
			r.ValidCount++
		case contains(futureAgents, b.Converse):
			add(Unverifiable, "agent-registry", "converse", "agent body not shipped — S13 named it a follow-up (no body file in the registry)", "S13")
		default:
			add(Invalid, "unknown-agent", "converse", fmt.Sprintf("unknown agent %q", b.Converse), "")
		}
	}
	if b.Kickoff != "" {
		if tpl, err := ParseKickoff(b.Kickoff); err != nil {
			add(Invalid, "kickoff-template", "kickoff", err.Error(), "")
		} else if err := tpl.Execute(io.Discard, KickoffData{}); err != nil {
			add(Invalid, "kickoff-field", "kickoff", err.Error(), "")
		} else {
			r.ValidCount++
		}
	}
	for i, l := range b.Lanes {
		base := fmt.Sprintf("lanes[%s]", l.ID)
		if l.ID == "" {
			base = fmt.Sprintf("lanes[%d]", i)
		}
		if (l.Step == nil) == (len(l.Ladder) == 0) {
			add(Invalid, "lane-shape", base, "lane needs exactly one of step or ladder", "")
		} else {
			r.ValidCount++
		}
		if l.Advisory && l.OnReject != nil {
			add(Invalid, "advisory-route", base+".on_reject", "advisory lane records rejects and cannot route them", "")
		}
		if l.Iterate != "" {
			f, ok := FindField(l.Iterate)
			if !ok || f.Type != FieldCollection {
				add(Invalid, "iterate-field", base+".iterate", "iterate must name a collection field", "")
			} else {
				r.ValidCount++
			}
			if len(l.Ladder) == 0 {
				add(Invalid, "iterate-shape", base+".iterate", "iterate needs a ladder", "")
			}
		}
		if len(l.Ladder) > 0 && l.Iterate == "" {
			add(Invalid, "iterate-shape", base+".ladder", "ladder needs iterate", "")
		}
		if l.When != nil {
			r.Diagnostics = append(r.Diagnostics, CheckCondition(*l.When, base+".when")...)
			if len(CheckCondition(*l.When, base+".when")) == 0 {
				r.ValidCount++
			}
		}
		if l.Step != nil {
			validateStep(&r, *l.Step, base+".step", b.Questions, opts, mode)
			checkReviseRoute(&r, *l.Step, l, base+".step", b.Questions, false)
		}
		stepIDs := map[string]int{}
		for j, s := range l.Ladder {
			path := fmt.Sprintf("%s.ladder[%s]", base, s.ID)
			if s.ID == "" {
				path = fmt.Sprintf("%s.ladder[%d]", base, j)
				add(Invalid, "required", path+".id", "ladder step id is required", "")
			} else if _, ok := stepIDs[s.ID]; ok {
				add(Invalid, "duplicate-step", path+".id", "step id is duplicated", "")
			} else {
				stepIDs[s.ID] = j
				r.ValidCount++
			}
			validateStep(&r, s, path, b.Questions, opts, mode)
			checkReviseRoute(&r, s, l, path, b.Questions, true)
		}
		if l.Rework != "" {
			if _, ok := stepIDs[l.Rework]; !ok {
				add(Invalid, "unknown-rework", base+".rework", "rework target is outside this ladder", "")
			} else {
				r.ValidCount++
			}
		}
		if l.OnReject != nil {
			checkEdge(&r, *l.OnReject, base+".on_reject", i, laneIDs)
		}
		for j, s := range l.Ladder {
			if s.OnReject != nil {
				checkStepEdge(&r, *s.OnReject, fmt.Sprintf("%s.ladder[%s].on_reject", base, s.ID), j, stepIDs)
			}
		}
	}
	// — requires (OL8): every term names a catalog entity in an approval
	// state. The state vocabulary is the PERSON'S verdict only: approved
	// exists, the ladder's statuses do not (a blueprint cannot pre-approve
	// a person's yes). —
	for i, req := range b.Requires {
		if len(req.AnyOf) == 0 {
			add(Invalid, "requires-empty", fmt.Sprintf("requires[%d].any_of", i), "a requirement with no terms can never be satisfied", "")
		}
		for j, term := range req.AnyOf {
			tp := fmt.Sprintf("requires[%d].any_of[%d]", i, j)
			// The prerequisite vocabulary is narrower than the catalog: only
			// a GOAL or a TASK can gate a run's start (OL8) — an epic or a
			// brief is context, not a precondition a blueprint declares.
			if term.Type != "goal" && term.Type != "task" {
				add(Invalid, "requires-unknown-type", tp+".type", "requires speaks goal or task", "")
			}
			if term.State != "approved" {
				add(Invalid, "requires-unknown-state", tp+".state", "requires admits only the state a person can leave: approved", "")
			}
		}
	}
	// — output (OL5): a declared deliverable must be REACHABLE — an entity
	// is reachable when some lane step emits it; a file when some step
	// produces it (or pins it). The whole run exists for these names; the
	// load step is where "nobody can deliver it" must surface. —
	{
		emitted := map[string]bool{}
		files := map[string]bool{}
		for _, l := range b.Lanes {
			for _, st := range laneStepSlice(l) {
				for _, em := range st.Emits {
					emitted[em.Type] = true
				}
				for _, pf := range st.Produces {
					files[pf] = true
				}
				for _, em := range st.Emits {
					for _, af := range em.Attach {
						files[af] = true
					}
				}
			}
		}
		for i, o := range b.Output {
			if contains(catalogEntities, o) {
				if !emitted[o] {
					add(Invalid, "output-unreachable", fmt.Sprintf("output[%d]", i), "no lane emits "+o+" — the run declares a deliverable nothing can deliver", "")
				}
				continue
			}
			if !files[o] {
				add(Invalid, "output-unreachable", fmt.Sprintf("output[%d]", i), "no step produces or pins "+o+" — the run declares a deliverable nothing can deliver", "")
			}
		}
	}
	// S32 PA11: a parallel_group is a declared-independent set (Shape A).
	// Overlapping produces across the group is an author-time `invalid`, not a
	// runtime race — the validation is the guard rail, not the runtime's
	// source of truth (PA4). A lane with an empty parallel_group is never
	// checked against anything; it opts out by omission (every other optional
	// field's convention).
	validateParallelGroup(b.Lanes, add)
	sort.Slice(r.Diagnostics, func(i, j int) bool {
		a, b := r.Diagnostics[i], r.Diagnostics[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Kind < b.Kind
	})
	return r
}

// laneProduces is the union of every artifact a lane declares it will write:
// its single step's produces plus every ladder step's produces. It is what
// S32 PA11 checks for disjointness across a parallel_group.
func laneProduces(l Lane) map[string]bool {
	out := map[string]bool{}
	if l.Step != nil {
		for _, p := range l.Step.Produces {
			out[p] = true
		}
	}
	for _, s := range l.Ladder {
		for _, p := range s.Produces {
			out[p] = true
		}
	}
	return out
}

// validateParallelGroup is S32 PA11's author-time guard: lanes that share a
// non-empty parallel_group must have disjoint produces, or the group could
// never run safely (two concurrent units writing the same artifact is the
// disk-contention collision PA5 prevents at runtime by giving each its own
// worktree — but the author is told it is wrong here, before anything runs).
// A group is a map name → the indices of its member lanes.
func validateParallelGroup(lanes []Lane, add func(state DiagState, kind, path, msg, slice string)) {
	group := map[string][]int{}
	for i, l := range lanes {
		if l.ParallelGroup == "" {
			continue
		}
		group[l.ParallelGroup] = append(group[l.ParallelGroup], i)
	}
	for name, members := range group {
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				a := laneProduces(lanes[members[i]])
				b := laneProduces(lanes[members[j]])
				var overlap []string
				for p := range a {
					if b[p] {
						overlap = append(overlap, p)
					}
				}
				if len(overlap) == 0 {
					continue
				}
				sort.Strings(overlap)
				add(Invalid, "parallel-produces-overlap",
					fmt.Sprintf("parallel_group %q", name),
					fmt.Sprintf("lanes %q and %q share produces %s in parallel_group %q — a group is a declared-independent set, so overlapping writes are refused at author time, not raced at runtime (S32 PA11)",
						lanes[members[i]].ID, lanes[members[j]].ID, strings.Join(overlap, ", "), name), "S32")
			}
		}
	}
}

// A gate offering revise must have somewhere to go. Otherwise validation can
// report a blueprint as runnable even though choosing its visible option
// inevitably blocks the loop after the user has already paid for the step.
func checkReviseRoute(r *Report, step Step, lane Lane, path string, questions map[string]Question, ladder bool) {
	if step.Kind != Gate {
		return
	}
	question, ok := questions[step.Question]
	if !ok || !contains(question.Answers, "revise") {
		return
	}
	target := ""
	if step.OnReject != nil {
		target = step.OnReject.Goto
	} else if lane.OnReject != nil {
		target = lane.OnReject.Goto
	}
	if ladder && lane.Rework != "" {
		target = lane.Rework
	}
	if target == "" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "missing-rework-target", Path: path + ".on_reject", Msg: "gate offers revise but has no rework target"})
	} else {
		r.ValidCount++
	}
}

func validateStep(r *Report, s Step, path string, questions map[string]Question, opts ValidationOptions, mode string) {
	add := func(state DiagState, kind, p, msg, slice string) {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{State: state, Kind: kind, Path: p, Msg: msg, Slice: slice})
	}
	validateSkillTarget(r, s, path, opts, add)
	if s.MaxIterations != nil {
		if s.Kind != ModelStep {
			add(Invalid, "step-shape", path+".max_iterations", "max_iterations bounds a model step's calls; this step is not a model step", "")
		} else if *s.MaxIterations < 0 {
			add(Invalid, "range", path+".max_iterations", "max_iterations cannot be negative; 0 means no bound", "")
		}
	}
	switch s.Kind {
	case ModelStep:
		if s.Agent == "" {
			add(Invalid, "required", path+".agent", "model step needs an agent", "")
		}
		if s.Action != "" || s.Question != "" {
			add(Invalid, "step-shape", path, "model step cannot name action or question", "")
		}
		if s.Agent != "" {
			known := contains(opts.Agents, s.Agent)
			future := contains(futureAgents, s.Agent)
			resolver := contains(opts.Resolvers, s.Agent)
			switch {
			case known:
				r.ValidCount++
			case resolver:
				// D13's flip: the resolver registry EXISTS (T10) — a real
				// check, not `unverifiable, arrives at S13`. The resolver
				// itself is closed (one name, in code); dispatch through it
				// refuses unknown tags rather than falling back.
				r.ValidCount++
			case future:
				add(Unverifiable, "agent-registry", path+".agent", "agent body not shipped — S13 named it a follow-up (no body file in the registry)", "S13")
			default:
				add(Invalid, "unknown-agent", path+".agent", fmt.Sprintf("unknown agent %q", s.Agent), "")
			}
		}
	case CodeAction:
		if s.Action == "" {
			add(Invalid, "required", path+".action", "code action needs action name", "")
		} else if opts.Actions != nil {
			// D15: the registry EXISTS — an unknown action is invalid,
			// not unverifiable. The check is real, so the deferral died.
			known := false
			for _, n := range opts.Actions {
				if n == s.Action {
					known = true
					break
				}
			}
			if known {
				r.ValidCount++
			} else {
				add(Invalid, "unknown-action", path+".action", fmt.Sprintf("unknown code action %q — the registry is closed (D14); its set is `aeon action list`", s.Action), "")
			}
		} else {
			add(Unverifiable, "action-registry", path+".action", "the code action registry exists (S16 shipped) — this validator was not given the closed list; verdict withheld, not guessed", "S16")
		}
		if s.Agent != "" || s.Question != "" || len(s.Produces) > 0 {
			add(Invalid, "step-shape", path, "code action cannot name agent, question or produces", "")
		}
		// S57 T6 (VG9): postReview is registered (class outward, not
		// deleted) but UNRUNNABLE from a blueprint — its step_id and
		// attempt exist only in a model step's result, and a half-working
		// use (the step validates, then blocks or no-ops at run time) is
		// exactly what this sentence exists to prevent.
		if s.Action == "forge.postReview" {
			add(Invalid, "unrunnable-action", path+".action", "forge.postReview cannot be fired from a blueprint — it needs a step id and attempt only a model step has (S57 VG9)", "")
		}
		// S57 T6 (VG7): a code action carrying on_reject is a step the
		// ENGINE routes — and the engine routes exactly one: verify.run,
		// whose outcome decides the route. The other actions' results are
		// facts the loop records, not verdicts it routes, so their
		// on_reject is a dead edge: the validator refuses it rather than
		// the engine silently ignoring it (T0 item 1: today accepted,
		// today inert — this tightens the shape).
		if s.OnReject != nil && s.Action != "verify.run" {
			add(Invalid, "code-action-on-reject", path+".on_reject", "only verify.run may carry on_reject — the engine routes a code action's outcome, and it routes only the verify run's (S57 VG7)", "")
		}
		// S57 T6: verify.run's narrowing params are code-supplied STRINGS
		// (the gate's id-or-name, the gate's agent identity) — type-checked
		// here so a number or a list cannot smuggle a shape the runner
		// would have to guess.
		if v, ok := s.Params["gate"]; ok {
			if _, isStr := v.(string); !isStr {
				add(Invalid, "param-type", path+".params.gate", "gate must be a string — the gate's id or name from the effective set", "")
			} else {
				r.ValidCount++
			}
		}
		if v, ok := s.Params["agent"]; ok {
			if _, isStr := v.(string); !isStr {
				add(Invalid, "param-type", path+".params.agent", "agent must be a string — the gate's agent identity the policy verdicts are checked under", "")
			} else {
				r.ValidCount++
			}
		}
	case Gate:
		if s.Question == "" {
			add(Invalid, "required", path+".question", "gate needs a question", "")
		} else if _, ok := questions[s.Question]; !ok {
			add(Invalid, "unknown-question", path+".question", "question is not declared", "")
		} else {
			r.ValidCount++
		}
		if s.Agent != "" || s.Action != "" || len(s.Produces) > 0 {
			add(Invalid, "step-shape", path, "gate cannot name agent, action or produces", "")
		}
	default:
		add(Invalid, "unknown-kind", path+".kind", "step kind is unsupported", "")
	}
	if s.When != nil {
		issues := CheckCondition(*s.When, path+".when")
		r.Diagnostics = append(r.Diagnostics, issues...)
		if len(issues) == 0 {
			r.ValidCount++
		}
	}
	if s.Sets != "" {
		known := false
		for _, v := range store.TaskStatuses() {
			if string(v) == s.Sets {
				known = true
				break
			}
		}
		if !known {
			add(Invalid, "machine-status", path+".sets", "status is not in the task machine", "")
		} else if !setReachable(s.Sets, mode) {
			// A step statically excluded in this mode is valid; it is for another mode.
			if s.When == nil || !staticallyFalse(*s.When, mode) {
				add(Invalid, "machine-transition", path+".sets", "status cannot occur in this mode", "")
			}
		} else {
			r.ValidCount++
		}
	}
	if s.OnReject != nil && s.OnReject.Then == "" {
		add(Invalid, "missing-then", path+".on_reject.then", "capped edge needs then", "")
	}
	// — emits (OL9): the contract a model step files. The grant check is
	// the load-time promise that a step can file what it declares — and an
	// absent handler or an at-rest validator says UNVERIFIABLE (P31), the
	// third state, never a guessed verdict either way. —
	if len(s.Emits) > 0 {
		if s.Kind != ModelStep {
			add(Invalid, "emits-needs-model-step", path+".emits", "only a model step emits — filing is the agent's act through a granted tool", "")
		}
		produces := map[string]bool{}
		for _, pf := range s.Produces {
			produces[pf] = true
		}
		for i, em := range s.Emits {
			tp := fmt.Sprintf("%s.emits[%d].type", path, i)
			need := []string{}
			if tool, ok := emitTool[em.Type]; ok {
				need = append(need, tool)
			} else {
				add(Invalid, "emits-unknown-type", tp, "emits type must be goal, epic, brief or task", "")
			}
			if em.Min < 0 {
				add(Invalid, "emits-min-negative", fmt.Sprintf("%s.emits[%d].min", path, i), "min counts deliverables and cannot be negative", "")
			}
			for _, af := range em.Attach {
				if !produces[af] {
					add(Invalid, "emits-attach-not-produced", fmt.Sprintf("%s.emits[%d].attach", path, i), "attach pins "+af+" but the step does not produce it — a pointer to a file nobody writes", "")
				}
			}
			if len(em.Attach) > 0 {
				need = append(need, "attach")
			}
			if opts.AgentTools == nil {
				add(Unverifiable, "emits-registry", tp, "no handler registry was offered — the grant cannot be checked, verdict withheld not guessed", "S38")
				continue
			}
			tools, known := opts.AgentTools(s.Agent)
			if !known {
				add(Unverifiable, "emits-agent-unverifiable", tp, "no body is registered for agent "+s.Agent+" — the grant is unverifiable, not false (P31)", "S38")
				continue
			}
			for _, n := range need {
				if !contains(tools, n) {
					add(Invalid, "emits-tool-missing", tp, fmt.Sprintf("agent %q is not granted %q — it cannot file what it declares", s.Agent, n), "S38")
				}
			}
		}
	}
	if s.Decides != "" && (s.Kind != Gate || (s.Decides != "goal" && s.Decides != "brief")) {
		add(Invalid, "decides-unknown-or-misplaced", path+".decides", "decides names goal or brief, and only on a gate — a model step decides nothing (OL3)", "")
	}
}

// laneStepSlice flattens a lane's steps for the output sweep.
func laneStepSlice(l Lane) []Step {
	out := []Step{}
	if l.Step != nil {
		out = append(out, *l.Step)
	}
	return append(out, l.Ladder...)
}

func setReachable(target, mode string) bool {
	for _, next := range store.TransitionsFor(store.LoopMode(mode)) {
		for _, candidate := range next {
			if string(candidate) == target {
				return true
			}
		}
	}
	return false
}
func staticallyFalse(c Condition, mode string) bool {
	if c.Field != "mode" {
		return false
	}
	ok, err := c.Evaluate(Values{"mode": mode})
	return err == nil && !ok
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func checkEdge(r *Report, e OnReject, path string, index int, lanes map[string]int) {
	if e.Then == "" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "missing-then", Path: path + ".then", Msg: "capped edge needs then"})
	}
	if e.Goto != "" {
		target, ok := lanes[e.Goto]
		if !ok {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "unknown-goto", Path: path + ".goto", Msg: "target lane is absent"})
		} else if target <= index && (e.Counter == "" || e.Max <= 0) {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "unbounded-back-edge", Path: path + ".goto", Msg: "backward edge needs a counter and max"})
		}
	}
	if e.Counter != "" {
		if _, ok := FindField("counters." + e.Counter); !ok {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "unknown-counter", Path: path + ".counter", Msg: "counter is not in manifest"})
		}
	}
}
func checkStepEdge(r *Report, e OnReject, path string, index int, steps map[string]int) {
	if e.Then == "" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "missing-then", Path: path + ".then", Msg: "capped edge needs then"})
	}
	if e.Goto != "" {
		target, ok := steps[e.Goto]
		if !ok {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "unknown-goto", Path: path + ".goto", Msg: "target step is outside this ladder"})
		} else if target <= index && (e.Counter == "" || e.Max <= 0) {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{State: Invalid, Kind: "unbounded-back-edge", Path: path + ".goto", Msg: "backward edge needs a counter and max"})
		}
	}
}

// Reject without an explicit edge is a terminal refusal. It never advances to
// the next lane. This lets a one-off producing step stop honestly when its
// artifact is missing without forcing a meaningless self-retry into the YAML.
func HasRejectRoute(s Step, l Lane) bool {
	return s.OnReject != nil || l.OnReject != nil || l.Rework != ""
}

func ConditionSummary(c *Condition) string {
	if c == nil {
		return ""
	}
	if c.Field != "" {
		return fmt.Sprintf("%s %s %v", c.Field, c.Op, c.Value)
	}
	return strings.TrimSpace(fmt.Sprintf("all:%d any:%d", len(c.All), len(c.Any)))
}

// validateSkillTarget is T0's half of the S8 amendment: `skill:` and `params:`
// may only exist as the shape the design states — a model step with a calling
// agent targeting "<callee>/<skill-id>" — and the target itself takes the
// three states against whatever agent services are configured. Parameter
// CONTENTS are deliberately not checked here: the skill's typed request (T2)
// owns them, validating before any model call.
func validateSkillTarget(r *Report, s Step, path string, opts ValidationOptions, add func(DiagState, string, string, string, string)) {
	// T0's rule, scoped: model steps carry params ONLY as skill targets.
	// A CODE-ACTION step's params are authored constants (T12's walk:
	// the feature-branch lane and the task-branch lane are two lanes of
	// one loop, and `params: {which: feature}` is how the author says
	// which) — their CONTENTS are judged by the registry's action at
	// dispatch, never guessed here: the validator cannot import the
	// action package (cycle), so it never pretends to know the values.
	if len(s.Params) > 0 && s.Skill == "" && s.Kind == ModelStep {
		add(Invalid, "params-without-skill", path, "params belong to a skill target; the step names none", "")
	}
	if s.Skill == "" {
		return
	}
	if s.Kind != ModelStep {
		add(Invalid, "step-shape", path+".skill", "only a model step can name a skill target", "")
		return
	}
	if s.Agent == "" {
		add(Invalid, "skill-without-agent", path+".skill", "a skill lane needs the calling agent too", "")
		return
	}
	callee, skill, ok := strings.Cut(s.Skill, "/")
	if !ok || callee == "" || skill == "" || strings.Contains(skill, "/") {
		add(Invalid, "skill-shape", path+".skill", fmt.Sprintf("skill target %q must be <callee>/<skill-id>", s.Skill), "")
		return
	}
	if opts.SkillKnown == nil {
		add(Unverifiable, "agentsvc", path+".skill", fmt.Sprintf(
			"the skill target %q cannot be checked — agent services are not configured on this process; starting a loop with this lane is refused until they are", s.Skill), "S14")
		return
	}
	if opts.SkillKnown(callee, skill) {
		r.ValidCount++
		return
	}
	add(Invalid, "agentsvc", path+".skill", fmt.Sprintf("unknown skill target %q — not registered with the configured agent services", s.Skill), "")
}

// ── S16 T8: the template-slot class (L20, D10) ─────────────────────────────
//
// The eighth class, joining S8's seven with the same contract: a
// machine-readable kind, a path, a state, a slice. A template naming a
// slot no producing handler declares is `invalid` AT LOAD — discovering
// {{pr_summary}} is empty while opening a real pull request means a PR
// with a hole in it, in front of people, and a load-time check runs
// before anything is outward-facing.
//
// The declarations arrive as plain data: the engine that owns templates
// hands its registry in, so blueprint never imports it (the dependency
// runs the honest direction — templates depend on the manifest, never
// the reverse).

// TemplateDecl is one template as the validator needs it.
type TemplateDecl struct {
	Name   string
	Source string
	Slots  map[string]string // slot name → kind (source); kinds are the caller's vocabulary
}

var templateSlot = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
var templateSlotName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateTemplates returns one Invalid diagnostic per undeclared or
// malformed token, and per declared-but-unused slot (a dead declaration
// is the same drift wearing a different hat).
func ValidateTemplates(specs []TemplateDecl) []Diagnostic {
	var out []Diagnostic
	for _, spec := range specs {
		seen := map[string]bool{}
		for _, m := range templateSlot.FindAllStringSubmatch(spec.Source, -1) {
			name := m[1]
			seen[name] = true
			path := "template/" + spec.Name + ".{{" + name + "}}"
			if !templateSlotName.MatchString(name) {
				out = append(out, Diagnostic{State: Invalid, Kind: "undeclared-slot", Path: path,
					Msg: "the token is malformed — slots are lowercase identifiers", Slice: "S16"})
				continue
			}
			if _, ok := spec.Slots[name]; !ok {
				out = append(out, Diagnostic{State: Invalid, Kind: "undeclared-slot", Path: path,
					Msg: "no producing handler declares this slot — the load refuses it before anything is outward-facing (D10)", Slice: "S16"})
			}
		}
		for name := range spec.Slots {
			if !seen[name] {
				out = append(out, Diagnostic{State: Invalid, Kind: "unused-slot",
					Path: "template/" + spec.Name + "." + name,
					Msg:  "declared but absent from the source — dead declarations drift", Slice: "S16"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
