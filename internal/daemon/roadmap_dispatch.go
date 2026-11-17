package daemon

// S26 T3 + T4 — the roadmap's dispatch: the daemon's side of the machine.
// Three doors and one reactor, all riding doors that already exist:
//
//   - ideation (T3, D1) starts an ORDINARY loop through the daemon's own
//     `loop.start` door with an EXISTING blueprint that already declares
//     `produces: brief.md`. The check this slice adds is narrow: the door
//     refuses a blueprint that carries no such declaration, because
//     ideation's end is the ENGINE's existing produces-check (missingArtifact
//     at the step runtime), which only exists if the blueprint declares it.
//     No new produces kind, no new gate, no new blueprint field, no file
//     under internal/blueprint — R3's call-site assertion is that ideation
//     completes through loop.finished, the same terminal event every other
//     consumer reads.
//   - planning (T3, D9) dispatches `project-planner` — S13's existing
//     body through the registry — exactly the way S24's T5 dispatched
//     sizing-estimator: an ordinary step through d.Table.RunStepForLoop on
//     a host loop, the handoff's task_count plus plan.md ON DISK deciding
//     `planned`. A failed run leaves the item exactly where it was;
//     nothing here retries silently (T3's "never a silent advance").
//   - materialization (T4, R4) is queue-first: a project whose queue has
//     rows gets a queue.Append and S24's ordering owns when it runs
//     (appending starts NOTHING — CG4); a project without one starts the
//     loop directly through the SAME loop.start door the queue's own
//     advanceNext ends at. Both branches carry the identical
//     LoopStartParams-shaped payload; the durable `with` map (roadmap_item
//     / roadmap_role) is the link the reactor reads, so no column was
//     added to carry it (the item's loop_slug stays D6's meaning: nothing
//     until a loop actually exists).
//   - the reactor (T3/T4) is the terminal consumer, joined at publishLoop
//     exactly where the queue's joined (S24 T3's precedent): done lands
//     done (running→done is the only edge out of running), ideation's done
//     lands the brief pointer and brief-agreed, blocked leaves the item's
//     OWN status untouched (D6 — the loop row is where a block lives) and
//     is spoken, and an untracked loop is not this reactor's business.
//
// CG5's shape holds structurally: NOTHING reachable from these files —
// from a step, a body, or the reactor — can call Transition, Create, or
// queue.Append out of a step's own runtime. Only the doors, the reactor
// reacting to a terminal event, and the socket/CLI callers move items.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

const (
	// plannerAgent is S13's existing body — T3's "assign the step/loop to
	// it through the existing registry, nothing custom".
	plannerAgent = "project-planner"
	// S50 T3 (RM3): the ideation default and the plan host's name were
	// constants here; they are now the DECLARED DEFAULTS of
	// roadmap.ideationBlueprint and roadmap.planBlueprint (their keys are
	// the two consts in roadmap_settings.go), read per dispatch. grep
	// finds no blueprint-name constant in this file on purpose: the
	// registry is the only place those two names are spelled.
)

// initRoadmap fills the Table's roadmap seams — called from initBlueprints
// beside initQueue. Nil-safe by construction: every handler answers
// ErrStoreUnavailable when a field is missing, so a daemon built without
// this call degrades loudly, not silently.
func (d *Daemon) initRoadmap() {
	// S50 T3 (RM3/RM6): the settings seam answers per dispatch (the
	// closure reads d.settings at CALL time, never a captured value),
	// and the health door is the daemon's own read.
	d.roadmapSettingView = func(ctx context.Context, key string) (config.View, error) {
		if d.settings != nil {
			return d.settings.Get(ctx, key)
		}
		decl, ok := config.DefaultRegistry().Lookup(key)
		if !ok {
			return config.View{}, fmt.Errorf("the registry carries no %q", key)
		}
		return config.View{Setting: decl, Value: decl.Default, Source: "default"}, nil
	}
	if d.Table != nil {
		d.Table.RoadmapHealth = d.roadmapHealth
	}
	if d.Table == nil || d.store == nil {
		return
	}
	d.Table.Roadmap = roadmap.New(d.store)
	// S50 T5 (RM8, rule 4): the records service's two read arms answer
	// through the ONE roadmap service instance the socket doors answer
	// through — the same object on both fields, so the board a step reads
	// and the board a person sees are one service's answer, not two that
	// agree by luck. (The propose arm needs nothing from here: it files a
	// proposal row and the COMMIT door is where roadmap is touched, with
	// the committer's identity from the commit context.)
	if d.recordsSvc != nil {
		d.recordsSvc.roadmapPlane = d.Table.Roadmap
	}
	d.Table.RoadmapIdeate = func(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapIdeateParams) (any, error) {
		return d.roadmapIdeate(ctx, actor, p)
	}
	d.Table.RoadmapPlan = func(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapPlanParams) (any, error) {
		return d.roadmapPlan(ctx, actor, p)
	}
	d.Table.RoadmapMaterialize = func(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapMaterializeParams) (any, error) {
		return d.roadmapMaterialize(ctx, actor, p)
	}
}

// --- ideation (T3, D1) ---------------------------------------------------------

// roadmapIdeate claims `idea → ideating` through the door (claimed-FROM:
// a second caller finds the rung gone) and starts the ideation loop
// through the EXISTING loop.start door. The slug is deterministic
// (`ideate-item-<id>`) so a re-dispatch while a run is in flight collides
// on the loop's own key instead of starting a second conversation; the
// `with` link is what the reactor reads.
func (d *Daemon) roadmapIdeate(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapIdeateParams) (map[string]any, error) {
	r := roadmap.New(d.store)
	it, ok, err := r.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("roadmap item %d does not exist", p.ID)
	}
	// `ideating` re-entry is the stalled-attempt path (the loop never got
	// started the first time — see the loud failure below); the machine's
	// self-edge makes the claim idempotent and the deterministic slug
	// makes a DOUBLE start impossible.
	if it.Status != roadmap.StatusIdea && it.Status != roadmap.StatusIdeating {
		return nil, fmt.Errorf("ideation starts from idea (or re-starts an item whose ideation never started); item %d is %q — the R1 table decides, and this edge is not in it", p.ID, it.Status)
	}
	name := strings.TrimSpace(p.Blueprint)
	if name == "" {
		// The SETTING answers — read now, at this dispatch (RM3). An
		// explicit param above short-circuits it and rides its own raw
		// refusal sentences; a setting-sourced name is fully resolved
		// and validated below before anything claims the item.
		chosen, _, _, err := d.roadmapBlueprintChoice(ctx, roadmapIdeationSetting)
		if err != nil {
			return nil, err
		}
		name = chosen
	}
	// loadStartBlueprint is loop.start's OWN validation (Resolve + read +
	// parse) — the unknown-name sentence rides out RAW, queue's precedent.
	bp, err := d.loadStartBlueprint(name)
	if err != nil {
		return nil, err
	}
	if !declaresBrief(bp) {
		return nil, fmt.Errorf("blueprint %q declares no step that produces brief.md — ideation ends at the engine's existing produces check (R3), which only exists when the blueprint carries the declaration; name a brief-declaring blueprint", name)
	}
	if d.Table == nil || d.Table.StartBlueprintLoop == nil {
		return nil, fmt.Errorf("the daemon has no loop.start door wired")
	}
	if _, ref := r.TransitionAudited(p.ID, roadmap.StatusIdeating, "ideate", actor); ref != nil {
		return nil, fmt.Errorf("%s", ref.Reason)
	}
	res, err := d.Table.StartBlueprintLoop(ctx, protocol.LoopStartParams{
		Blueprint:     name,
		Goal:          fmt.Sprintf("Ideate roadmap item %d — %s. This is the item's whole brief; open with the conversation, end with brief.md written at the workspace root.", p.ID, it.Title),
		WorkspaceRoot: p.WorkspaceRoot,
		Slug:          fmt.Sprintf("ideate-item-%d", p.ID),
		With:          roadmapLink(p.ID, "ideation"),
		Attended:      p.Attended, // the caller's declaration or never-declared, never a guess (D2)
	})
	if err != nil {
		// Loud by design: the item reads `ideating` and no loop runs. The
		// table has no ideating→idea edge (R1's ruling), so the truth is
		// spoken here and the item stays where it is for a person to
		// cancel — never a silent revert that could race a late event.
		return nil, fmt.Errorf("the item claims ideating but the ideation loop was never started — %v; the item stays ideating for a person to decide", err)
	}
	slug := fmt.Sprintf("ideate-item-%d", p.ID)
	if m, ok := res.(map[string]any); ok {
		if got, _ := m["slug"].(string); got != "" {
			slug = got
		}
	}
	d.log.Infof("roadmap: item %d entered ideating — ideation loop %s on blueprint %s", p.ID, slug, name)
	return map[string]any{"started": true, "loopSlug": slug, "status": string(roadmap.StatusIdeating)}, nil
}

// declaresBrief reads the parsed blueprint the way the engine's own
// produces check will: any lane's step (or ladder step) declaring the
// brief file. Read-only over the blueprint package's existing types —
// zero changes under internal/blueprint, which is T3's gate.
func declaresBrief(bp *blueprint.Blueprint) bool {
	has := func(paths blueprint.Paths) bool {
		for _, p := range paths {
			if p == "brief.md" {
				return true
			}
		}
		return false
	}
	for _, lane := range bp.Lanes {
		if lane.Step != nil && has(lane.Step.Produces) {
			return true
		}
		for _, st := range lane.Ladder {
			if has(st.Produces) {
				return true
			}
		}
	}
	return false
}

// --- planning (T3, D9) ----------------------------------------------------------

// roadmapPlan dispatches the planner as an ordinary step on a host loop —
// queue_sizing.go's shape, item-for-item. The claim against double
// dispatch is the host loop's deterministic slug: `plan-item-<id>` exists
// exactly once per root, so a second caller's CreatePinnedLoop collides
// where the machine has no `planning` rung to wear the claim.
func (d *Daemon) roadmapPlan(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapPlanParams) (map[string]any, error) {
	r := roadmap.New(d.store)
	it, ok, err := r.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("roadmap item %d does not exist", p.ID)
	}
	if it.Status != roadmap.StatusBriefAgreed {
		return nil, fmt.Errorf("planning dispatches from brief-agreed; item %d is %q — the R1 table decides, and this edge is not in it", p.ID, it.Status)
	}
	if it.BriefPath == "" {
		return nil, fmt.Errorf("the planner reads the item's brief pointer; item %d has none — ideation lands it when the loop's brief.md lands", p.ID)
	}
	if d.Table == nil {
		return nil, fmt.Errorf("the daemon has no step door wired")
	}
	hostSlug := fmt.Sprintf("plan-item-%d", p.ID)
	// The host's bytes come from the name roadmap.planBlueprint names,
	// read AT THIS DISPATCH and validated before anything is pinned
	// (RM3): a missing or invalid choice refuses with the setting named
	// and starts nothing — never a silent fallback, which would let a
	// mistyped name run a blueprint nobody chose.
	name, raw, bp, err := d.roadmapBlueprintChoice(ctx, roadmapPlanSetting)
	if err != nil {
		return nil, err
	}
	host, err := d.store.CreatePinnedLoop(store.NewLoop{
		Slug: hostSlug, ComponentRoot: it.ComponentRoot, Root: p.WorkspaceRoot,
		Goal: "plan roadmap item " + strconv.FormatInt(p.ID, 10), Status: "running",
	}, name, bp.Version, string(raw), "{}")
	if err != nil {
		return nil, fmt.Errorf("the planning host loop for item %d could not be pinned — a planning run for this item has already been dispatched from this root, or the host failed: %v", p.ID, err)
	}
	prompt := fmt.Sprintf("Plan this roadmap item (roadmap item %d).\nTitle: %s\nRead the agreed brief at: %s\nFile plan.md with doc_write (a document of this loop, not a workspace file) — the Understanding block first, then one detail spec per task.\nYour handoff mirrors what you planned: task_count — the number of tasks in plan.md.",
		p.ID, it.Title, workspaceRelative(p.WorkspaceRoot, it.BriefPath))
	go d.runPlanningStep(it, host.ID, hostSlug, p.WorkspaceRoot, prompt)
	d.log.Infof("roadmap: item %d planning dispatched on host %s (agent %s)", p.ID, hostSlug, plannerAgent)
	return map[string]any{"dispatched": true, "hostLoop": hostSlug}, nil
}

// runPlanningStep walks the ordinary step door ONCE — the same door every
// blueprint step knocks (queue's sizing dispatch is the precedent, and its
// test pins the single call site) — and hands the answer to the landing
// decision below.
func (d *Daemon) runPlanningStep(item roadmap.Item, hostLoopID, hostSlug, workspaceRoot, prompt string) {
	if d.Table == nil {
		d.landPlanning(item, workspaceRoot, protocol.StepResult{}, fmt.Errorf("the daemon has no step door wired"))
		return
	}
	res, err := d.Table.RunStepForLoop(context.Background(),
		protocol.StepParams{Prompt: prompt, Agent: plannerAgent, WorkspaceRoot: workspaceRoot, LoopSlug: hostSlug},
		protocol.LoopStepIdentity{LoopID: hostLoopID, Step: "plan"},
		func(stepID int64) error { return nil })
	d.landPlanning(item, workspaceRoot, res, err)
}

// landPlanning is T3's whole landing law in one function: `planned` moves
// ONLY on the step's own ok + a handoff whose task_count is positive +
// plan.md existing on disk. The item NEVER advances on failure — it stays
// brief-agreed and the failure is spoken, never silent. The tests drive
// this decision with the door's answers directly; the door itself is the
// ordinary one (queue_sizing_test.go's single-call-site pin is the form).
func (d *Daemon) landPlanning(item roadmap.Item, workspaceRoot string, res protocol.StepResult, stepErr error) bool {
	r := roadmap.New(d.store)
	fail := func(why string) {
		d.log.Infof("roadmap: the planning run for item %d did not land (%s) — the item stays %q; nothing here retries it silently (T3)", item.ID, why, item.Status)
	}
	if stepErr != nil {
		fail("the step door refused: " + stepErr.Error())
		return false
	}
	if res.Outcome != "ok" {
		why := res.Outcome
		if res.Reason != "" {
			why += " · " + res.Reason
		}
		fail("the step came back " + why)
		return false
	}
	var handoff struct {
		TaskCount int `json:"task_count"`
	}
	if len(res.Handoff) == 0 {
		fail("an ok step carried no handoff")
		return false
	}
	if err := json.Unmarshal(res.Handoff, &handoff); err != nil {
		fail("the handoff is not the planned shape: " + err.Error())
		return false
	}
	if handoff.TaskCount <= 0 {
		fail("the handoff's task_count is not a positive count")
		return false
	}
	// The plan is a document of the planning host loop; the daemon exports the
	// hard copy the pointer names (hardcopy.go). A planner that wrote the file
	// itself is honoured the way it always was.
	planPath, exportErr := d.exportHardCopy(workspaceRoot, store.Scope{LoopSlug: fmt.Sprintf("plan-item-%d", item.ID), ComponentRoot: item.ComponentRoot}, "plan.md")
	if exportErr != nil {
		fail("the plan's hard copy could not be written: " + exportErr.Error())
		return false
	}
	if planPath == "" {
		planPath = filepath.Join(workspaceRoot, "plan.md")
	}
	if st, statErr := os.Stat(planPath); statErr != nil || st.IsDir() {
		fail("the handoff claims tasks but plan.md is not the document or file it promised")
		return false
	}
	if _, ref := r.LandPlanAudited(item.ID, planPath, roadmap.SystemReactor); ref != nil {
		fail("the plan landing was refused: " + ref.Reason)
		return false
	}
	d.log.Infof("roadmap: item %d reads planned — plan.md filed (hard copy %s), handoff task_count=%d", item.ID, planPath, handoff.TaskCount)
	return true
}

// --- materialization (T4, R4) -----------------------------------------------------

// roadmapMaterialize is R4's one door. The claim `ready → running`
// (claimed-FROM) is the single serialization point; then the project's
// own shape decides the branch — a project with queue rows gets a
// queue.Append and S24's ordering owns WHEN the loop runs (CG4: appending
// starts nothing; the item may sit honestly running-queued-until-started,
// D6); a project without one starts directly through the same
// StartBlueprintLoop door the queue's advanceNext ends at (brief §4's "no
// second execution path" — the identical call, the identical payload).
//
// A `running` item without its loop is the retry path — the one that
// exists because the table has no running→back edge: a start that died
// before its loop row existed left the claim true but the loop absent.
// The deterministic slug makes that retry single-winner (a start that
// actually landed its loop collides, which is the refusal), and a queue
// branch already queued is refused by its own link row.
func (d *Daemon) roadmapMaterialize(ctx context.Context, actor roadmap.Actor, p protocol.RoadmapMaterializeParams) (map[string]any, error) {
	r := roadmap.New(d.store)
	it, ok, err := r.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("roadmap item %d does not exist", p.ID)
	}
	if it.Status != roadmap.StatusReady && it.Status != roadmap.StatusRunning {
		return nil, fmt.Errorf("only a ready item materializes; item %d is %q (D4: ready means brief, plan, and a priority that is not unknown — roadmap.show names what is missing)", p.ID, it.Status)
	}
	if it.Status == roadmap.StatusRunning && it.LoopSlug != "" {
		return nil, fmt.Errorf("item %d is already materialized in loop %s — the loop's own record is the truth now (D6)", p.ID, it.LoopSlug)
	}
	if it.Status == roadmap.StatusReady {
		// The D4 re-check at the attempt, decided NOW from the row as it
		// is (the door's ready-gate rides →ready transitions only, so
		// the claim's own re-check lives here): a status that has aged
		// since — a priority pulled back to unknown, a pointer cleared —
		// refuses, naming which.
		deps, err := r.Dependencies(p.ID)
		if err != nil {
			return nil, fmt.Errorf("roadmap: read item %d's dependencies: %w", p.ID, err)
		}
		if ready, why := roadmap.IsReadyWithDeps(it, deps); !ready {
			return nil, fmt.Errorf("item %d is not ready at the attempt — %s (D4 is decided now, never cached)", p.ID, why)
		}
		if _, ref := r.TransitionAudited(p.ID, roadmap.StatusRunning, "materialize", actor); ref != nil {
			return nil, fmt.Errorf("%s", ref.Reason)
		}
		if fresh, ok, err := r.Show(p.ID); err == nil && ok {
			it = fresh
		}
	}
	// The queue-or-direct probe is the project's own queue rows — rows
	// exist means S24 is in use for this project (its ordering, its
	// reactor, its head-of-queue); zero rows means the project has never
	// queued, so materialization is a plain loop.start. Either way the
	// payload below is the same; only WHO starts the loop differs.
	rows, err := d.store.ListQueueItems(it.ComponentRoot)
	if err != nil {
		return nil, err
	}
	with := roadmapLink(p.ID, "materialization")
	for k, v := range p.With {
		with[k] = v
	}
	if len(rows) > 0 {
		return d.materializeViaQueue(r, it, p, with)
	}
	return d.materializeDirect(ctx, r, it, actor, p, with)
}

// materializeViaQueue hands the item to S24's ordering — the queue's
// OWN Append door (this is not a second ordering mechanism; it is the
// first one, called). The with-link rides into the queue row's durable
// with_json, where the reactor reads it when S24 finally starts the loop.
func (d *Daemon) materializeViaQueue(r *roadmap.Roadmap, it roadmap.Item, p protocol.RoadmapMaterializeParams, with map[string]string) (map[string]any, error) {
	rows, err := d.store.ListQueueItems(it.ComponentRoot)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		id, role := queueRoadmapLink(row)
		if role == "materialization" && id == it.ID {
			return nil, fmt.Errorf("item %d is already queued for materialization (queue item %d) — S24's ordering decides when its loop runs (CG4: appending starts nothing)", it.ID, row.ID)
		}
	}
	q := queue.New(d.store)
	qi, qerr := q.Append(it.ComponentRoot, p.Blueprint, p.Goal, p.WorkspaceRoot, queue.NewItem{
		Tech: p.Tech, With: with, Attended: p.Attended,
	})
	if qerr != nil {
		return nil, fmt.Errorf("item %d stays ready — the queue refused the append: %v", it.ID, qerr)
	}
	d.log.Infof("roadmap: item %d materialized into queue item %d — S24's ordering owns the start (CG4)", it.ID, qi.ID)
	return map[string]any{"queued": true, "queueItemId": qi.ID, "status": string(roadmap.StatusRunning)}, nil
}

// materializeDirect ends at the EXISTING loop.start door — the identical
// call (same params the queue branch's row carries for S24 to pass to the
// same door later; TestOneMaterializationPathEitherBranch diffs them).
func (d *Daemon) materializeDirect(ctx context.Context, r *roadmap.Roadmap, it roadmap.Item, actor roadmap.Actor, p protocol.RoadmapMaterializeParams, with map[string]string) (map[string]any, error) {
	if d.Table == nil || d.Table.StartBlueprintLoop == nil {
		return nil, fmt.Errorf("the daemon has no loop.start door wired")
	}
	slug := fmt.Sprintf("roadmap-item-%d", it.ID)
	res, err := d.Table.StartBlueprintLoop(ctx, protocol.LoopStartParams{
		Blueprint:     p.Blueprint,
		Goal:          p.Goal,
		WorkspaceRoot: p.WorkspaceRoot,
		Tech:          p.Tech,
		With:          with,
		Attended:      p.Attended,
		Slug:          slug, // deterministic: the retry guard (see roadmapMaterialize)
	})
	if err != nil {
		return nil, fmt.Errorf("item %d still claims running and started no loop — %v; if a loop for this item DOES exist this refusal is the truth (D6), otherwise the claim waits for a person", it.ID, err)
	}
	if m, ok := res.(map[string]any); ok {
		if got, _ := m["slug"].(string); got != "" {
			slug = got
		}
	}
	if _, ref := r.RecordLoopAudited(it.ID, it.ComponentRoot, slug, actor); ref != nil {
		// The loop EXISTS; the pointer failed — the loop is the truth now
		// (D11's rule, queue's own words). Loud, not rolled back.
		d.log.Infof("roadmap: LOOP %s EXISTS but item %d never got its pointer: %s — the loop is the truth now", slug, it.ID, ref.Reason)
		return map[string]any{"started": true, "loopSlug": slug, "stamped": false, "status": string(roadmap.StatusRunning)}, nil
	}
	d.log.Infof("roadmap: item %d materialized into loop %s", it.ID, slug)
	return map[string]any{"started": true, "loopSlug": slug, "stamped": true, "status": string(roadmap.StatusRunning)}, nil
}

// roadmapLink is the durable link both branches stamp: the with map every
// branch's loop (or queue row) carries, naming the item and the role the
// loop plays. The `with` map is S14's existing durable channel — no new
// column, nothing watching, one record.
func roadmapLink(itemID int64, role string) map[string]string {
	return map[string]string{
		"roadmap_item": strconv.FormatInt(itemID, 10),
		"roadmap_role": role,
	}
}

// queueRoadmapLink reads the link out of a queue row's durable with_json.
// Missing or unparseable is honestly "no link" — an ordinary queued item.
func queueRoadmapLink(row store.QueueItemRow) (itemID int64, role string) {
	if row.WithJSON == nil || *row.WithJSON == "" {
		return 0, ""
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(*row.WithJSON), &m); err != nil {
		return 0, ""
	}
	id, err := strconv.ParseInt(m["roadmap_item"], 10, 64)
	if err != nil {
		return 0, ""
	}
	return id, m["roadmap_role"]
}

// pinRoadmapLink reads the link out of a pinned loop's runtime json — the
// durable `with` channel loop.start pinned (RuntimeState.With). Both the
// nested {"with":{...}} shape and a flat map are read: this is the daemon
// reading its OWN record, and a shape surprise should not strand a loop.
func pinRoadmapLink(runtimeJSON string) (itemID int64, role string) {
	if strings.TrimSpace(runtimeJSON) == "" {
		return 0, ""
	}
	var state struct {
		With map[string]string `json:"with"`
	}
	if err := json.Unmarshal([]byte(runtimeJSON), &state); err == nil && len(state.With) > 0 {
		id, err := strconv.ParseInt(state.With["roadmap_item"], 10, 64)
		if err == nil {
			return id, state.With["roadmap_role"]
		}
	}
	var flat map[string]string
	if err := json.Unmarshal([]byte(runtimeJSON), &flat); err == nil {
		id, err := strconv.ParseInt(flat["roadmap_item"], 10, 64)
		if err == nil {
			return id, flat["roadmap_role"]
		}
	}
	return 0, ""
}

// workspaceRelative turns a path under the workspace into the workspace-relative
// form the file tools accept (they refuse an absolute path); a path outside the
// workspace is left as it was.
func workspaceRelative(root, path string) string {
	if root == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(rel)
}
