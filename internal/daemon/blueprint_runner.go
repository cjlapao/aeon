// blueprint_runner.go — S48 T6 (SIZE1 split of blueprint.go by cohesion):
// the daemon-side loop RUNNER — the workspace bound, the autonomy seams, the
// dispatch/skill step plumbing, goal/task link resolution, loop recovery at
// start (StartLoops), the records reader, the start-time blueprint load
// (rows since S48 T3), the resolver candidates, and the socket actor. A
// reader who opened blueprint.go "to change how a step runs" now opens
// exactly this file.

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func validateLoopWorkspace(root string) error {
	// T7: these refusals are TYPED. The narrow surface distinguishes the
	// four error kinds by type identity (errors.go), and a bare error here
	// would answer transport/503 "retry" to a caller whose resend cannot
	// change anything — the exact plausible-wrong-advice the taxonomy
	// exists to kill. The daemon understood; the daemon declined.
	if !filepath.IsAbs(root) {
		return protocol.ErrWorkspaceNotVisible.WithWhat("workspace path %q must be absolute", root)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return protocol.ErrWorkspaceNotVisible.WithWhat("workspace %q is not visible to the daemon: %v", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return protocol.ErrWorkspaceNotVisible.WithWhat("workspace %q cannot be inspected by the daemon: %v", root, err)
	}
	if !info.IsDir() {
		return protocol.ErrWorkspaceNotVisible.WithWhat("workspace path %q is not a directory", root)
	}
	if _, err := os.ReadDir(resolved); err != nil {
		return fmt.Errorf("workspace %q cannot be listed by the daemon: %w", root, err)
	}
	return nil
}

// autonomySeams is the daemon's wiring of S56 T5's condition seams (AL4, R1, R2).
// The use log (maxPerDay) is backed by the store's CountAutonomyUsesSince; the
// Gates (S57's merge rule, R2) and Budget (S55, R1) seams are nil until those
// slices land. A nil seam is cannot-say, never met (AL4), so a merge or budget
// grant is inert by design while its neighbour is unwired — a fail-toward-deny,
// not a silent allow. A storeless daemon answers zero seams (every grant inert).
func (d *Daemon) autonomySeams() loop.AutonomySeams {
	if d.store == nil {
		return loop.AutonomySeams{}
	}
	return loop.AutonomySeams{
		Seams: autonomy.Seams{
			// R2 (S57 merge rule), wired at S57 T6: the effective rule
			// evaluated over the PR's live facts at the decision site
			// (verify_wiring.go). R1 (S55 budget) is unwired here —
			// a nil seam is cannot-say, never met, so a budget grant
			// stays inert while it is.
			Gates:  d.mergeGateAnswer,
			Budget: nil,
			Uses: func(ctx context.Context, grantID string, since time.Time) (int, error) {
				return d.store.CountAutonomyUsesSince(grantID, since)
			},
		},
	}
}

func (d *Daemon) dispatchSkill(ctx context.Context, st loop.LoopState, step blueprint.Step, prompt, frame string) (loop.ModelResult, error) {
	if d.agentInvoke == nil {
		return loop.ModelResult{}, fmt.Errorf("the model step for %s names skill %s, but agent services are not wired into this daemon — start it through the entry point that carries a card store; the skill was not quietly dropped", step.Agent, step.Skill)
	}
	return d.agentInvoke(ctx, st, step, prompt, frame)
}

// stepParamsFor builds the dispatch params for one loop step. T0: a skill
// lane's target and params ride along; a plain step produces byte-identical
// wire to pre-S14 (Params stays nil, never an empty map). T4 consumes Skill
// and Params at invocation; nothing here interprets them. S68: the frame the
// loop speaks around the prompt rides `taskFrame` (the system half), so the
// prompt the operator typed rides the transcript half alone.
func stepParamsFor(st loop.LoopState, step blueprint.Step, prompt, frame string) protocol.StepParams {
	// S60 T5 (MQ7): the step's workspace is the loop's WORKDIR when the loop
	// has one — after a git.integrate the model steps resolve, edit and test
	// inside the integration worktree, not the person's checkout. WorkDir is
	// empty for every loop that never integrated (and every loop that predates
	// S60), so the empty case is byte-identical to the line this replaced —
	// the S32 parallel-unit case carries it per-unit (runOneUnit).
	ws := st.Root
	if st.WorkDir != "" {
		ws = st.WorkDir
	}
	// A lane's route override (pinned at loop start from a locked blueprint)
	// wins over the loop's own route; no entry means the loop's route.
	route := st.Route
	if o := st.RouteOverrides[st.Position.Lane]; o != "" {
		route = o
	}
	p := protocol.StepParams{Prompt: prompt, Agent: step.Agent, WorkspaceRoot: ws, Route: route, LoopSlug: st.Slug, TaskFrame: frame}
	p.StepMaxIterations = step.MaxIterations
	// The model options the person chose when the loop started.
	p.ReasoningEffort, p.Thinking = st.ReasoningEffort, st.Thinking
	if step.Skill != "" {
		p.Skill = step.Skill
		p.Params = step.Params
	}
	return p
}

// resolveDispatchAgent maps a step's agent to the one that runs it. A name that
// is not the resolver passes through untouched; `by-tech-tag` picks among the
// agents that DECLARE the loop's tech (handler.ResolveByTech) — an agent a
// person creates joins the dispatch by declaring its tags, and there is no
// fallback when none does.
func (d *Daemon) resolveDispatchAgent(st loop.LoopState, step blueprint.Step) (string, error) {
	return resolveDispatchAgent(st, step, d.techAgents)
}

// techAgents reads the agents that declare tech from the store.
func (d *Daemon) techAgents() ([]handler.TechAgent, error) {
	rows, err := d.store.AgentsAll()
	if err != nil {
		return nil, err
	}
	agents := make([]handler.TechAgent, 0, len(rows))
	for _, a := range rows {
		if tags := a.TechTags(); len(tags) > 0 {
			agents = append(agents, handler.TechAgent{Slug: a.Slug, Tech: tags})
		}
	}
	return agents, nil
}

// resolveDispatchAgent is the seam without the store: the agents arrive through
// a reader, so the rule (only the resolver's own name is resolved) is testable
// on its own.
func resolveDispatchAgent(st loop.LoopState, step blueprint.Step, agents func() ([]handler.TechAgent, error)) (string, error) {
	if !handler.IsResolver(step.Agent) {
		return step.Agent, nil
	}
	list, err := agents()
	if err != nil {
		return "", err
	}
	return handler.ResolveByTech(st.With["tech"], list)
}

func (d *Daemon) resolveLinks(goalID, taskID string) (*store.Goal, *store.Task, error) {
	var goal *store.Goal
	var task *store.Task
	if id := strings.TrimSpace(goalID); id != "" {
		g, err := d.store.GetGoal(id)
		if err != nil {
			return nil, nil, protocol.ErrStartLinkUnknown.WithWhat("no goal %q — loop.start was given a goalId that does not resolve", id)
		}
		goal = &g
	}
	if id := strings.TrimSpace(taskID); id != "" {
		t, err := d.store.GetTaskByID(id)
		if err != nil {
			return nil, nil, protocol.ErrStartLinkUnknown.WithWhat("no task %q — loop.start was given a taskId that does not resolve", id)
		}
		// The worker row is for MANUAL tasks (the store refuses
		// otherwise); refusing here, before the row, keeps the refusal
		// from arriving after a loop was already born.
		if !t.Manual {
			return nil, nil, protocol.ErrStartLinkUnknown.WithWhat("task %s is a loop task owned by loop %s — loop.start's taskId names a manual task the person approved, not another loop's work", id, t.LoopSlug)
		}
		task = &t
	}
	return goal, task, nil
}

// goalPromptText is goal title + "\n\n" + its attached body, exactly as
// S38 T13 seeds loop.start's prompt from the goal (the goal IS the frame;
// attached DOCUMENTS live in the archive blob with no content reader yet —
// recorded, not silently skipped).
func (d *Daemon) goalPromptText(g *store.Goal) string {
	if g == nil {
		return ""
	}
	if strings.TrimSpace(g.BodyNote) != "" {
		return g.Title + "\n\n" + g.BodyNote
	}
	return g.Title
}

func (d *Daemon) StartLoops(ctx context.Context) error {
	// The heartbeat that makes a lost last event visible (subscription_tick.go).
	go d.RunSubscriptionTicks(ctx)
	// S20 T2: the clock starts with the loops it starts — a daemon that is
	// up is a daemon whose schedules are being answered (the off switches
	// are pause and policy, TR12, not "is a goroutine running").
	if d.triggers != nil {
		// Startup catch-up BEFORE the ticker (TR3): one fire per schedule
		// with a gap, the middle recorded could-not-say as a range+count —
		// never a burst. A reconcile failure speaks and STARTS ANYWAY: the
		// tick path works from the frontier as stored, and a schedule that
		// cannot reconcile is loud in the log, not silently off.
		if err := d.triggers.Reconcile(ctx, d.triggers.Clock.Now()); err != nil {
			d.log.Infof("trigger reconcile failed (the tick still runs from the stored frontier): %v", err)
		}
		go d.triggers.Run(ctx)
	}
	// S57 T7 (VG5): the post-merge watch starts beside the clock, the same
	// way — a daemon that is up is a daemon whose merged PRs are being
	// answered. The watcher runs even when there is no loop runner: its
	// startup sweep repairs a crash window that the runner cannot know
	// about, and a daemon down for a while owes its merged PRs a bounded
	// catch-up, not a silent one.
	if d.gateWatcher != nil {
		go d.gateWatcher.Run(ctx)
	}
	if d.loopRunner == nil {
		return nil
	}
	pins, err := d.store.PinnedLoopsByStatus("running")
	if err != nil {
		return err
	}
	for _, pin := range pins {
		if strings.TrimSpace(pin.Loop.ID) != "" {
			d.loopRunner.Start(ctx, pin.Loop.ID)
		}
	}
	return nil
}

// recordsReaderFor adapts the store's counting doors to the loop's
// RecordsReader seam (S38 T12). A storeless daemon (store == nil) answers
// nil — and an emits: step then blocks contract-unverifiable, the honest
// third state, rather than passing an unchecked contract.
func recordsReaderFor(s *store.Store) loop.RecordsReader {
	if s == nil {
		return nil
	}
	return recordsReader{store: s}
}

type recordsReader struct{ store *store.Store }

func (r recordsReader) CountByStep(kind string, stepID int64) (int, error) {
	return r.store.CountRecordsByStep(kind, stepID)
}

func (r recordsReader) AttachedByStep(stepID int64) ([]string, error) {
	return r.store.StepAttachments(stepID)
}

// loadStartBlueprint reads and parses the blueprint named at start — the
// daemon's OWN pre-start read, used for nothing but the requires decision
// (loop.Start re-loads and re-validates the durable copy; two readers of
// one file, zero disagreement possible because neither edits it). A file
// that cannot be parsed fails HERE with no row behind it; the same bad
// file would have been refused by Start moments later anyway.
func (d *Daemon) loadStartBlueprint(name string) (*blueprint.Blueprint, error) {
	// S48 T3: fire time reads through the daemon's own source — the row the
	// last write left, never a file, never the embed (no fallback). A PATH
	// argument stays a file read (a person's argument, not the mirror).
	if strings.ContainsAny(name, "/\\") || (strings.HasSuffix(name, ".yaml") && filepath.IsAbs(name)) {
		raw, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			return nil, err
		}
		return &bp, nil
	}
	raw, ok, err := d.bpSrc.Get(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, blueprint.NewErrUnknownBlueprint(name)
	}
	bp, err := blueprint.Load(raw)
	if err != nil {
		return nil, err
	}
	return &bp, nil
}

// candidatesFor builds the evaluator's offer: the approved goals and the
// person-approved manual tasks THIS workspace's component can see, bounded.
// No component for the root means no candidates — honestly none, rather
// than a project-blind guess that offers records from somewhere else.
// A goal hangs on a ROOT (CT10), so the offer resolves the workspace
// component's root and lists the goals there; a manual task is filed on the
// component that owns it, so tasks stay component-scoped.
func (d *Daemon) candidatesFor(root string) func() []loop.Candidate {
	return func() []loop.Candidate {
		if d.store == nil {
			return nil
		}
		comp, err := d.store.ComponentForRoot(root)
		if err != nil {
			return nil
		}
		rootID, err := d.store.RootOf(comp.ID)
		if err != nil {
			rootID = comp.ID // a component with no parent IS its own root
		}
		var out []loop.Candidate
		if goals, err := d.store.ListGoals(store.GoalFilter{ComponentID: rootID, ReviewState: "approved"}); err == nil {
			for _, g := range goals {
				out = append(out, loop.Candidate{Kind: "goal", ID: g.ID, Title: g.Title, State: g.ReviewState})
			}
		}
		yes := true
		limit := 20
		if rows, err := d.store.ListTaskRows(store.TaskFilter{ComponentID: comp.ID, Manual: &yes, ApprovalState: "approved", Limit: limit}); err == nil {
			for _, r := range rows {
				out = append(out, loop.Candidate{Kind: "task", ID: r.ID, Title: r.Title, State: r.ApprovalState})
			}
		}
		if len(out) > 20 {
			out = out[:20]
		}
		return out
	}
}

// socketActor names the person behind the endpoint for verdicts the socket
// carries (S38 T14's gate join). P26's trust model is one user per daemon —
// the peer credential and this process share a uid — so the OS username is
// the honest ID for a review row; the fallback names the door, not a guess
// at an identity nobody can read.

func socketActor() store.Actor {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return store.Actor{Kind: "user", ID: u.Username}
	}
	return store.Actor{Kind: "user", ID: "socket-peer"}
}
