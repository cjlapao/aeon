package daemon

// S24 T5 — the sizing dispatch. `sizingMode: skill` gets its dispatch
// target the way brief §5 locked it: an ORDINARY step through the
// daemon's own step door (`d.Table.RunStepForLoop` — the identical seam
// a conversation turn rides), assigned to the `sizing-estimator` body
// through the handler registry like any other agent. R3 follows for
// free: because it is an ordinary step, the project's policy,
// attendedness and cost-cap gates judge it like any other step — there
// is no sizing lane to forget to govern.
//
// CG5's exemption is structural: NOTHING here can append a queue item.
// The sizing run exists only as a step; the code that reads its handoff
// moves the item `sizing → ready` (or back to `pending`, CG5's edge)
// through internal/queue's doors — and a grep test pins that no door
// from this file, or from report_size, or reachable from the body, is
// the Append door.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// sizingAgent names the registered body (internal/handler/bodies/
// sizing-estimator.md) — the ONLY way the sizing step picks its agent,
// the same way any blueprint step names one.
const sizingAgent = "sizing-estimator"

// sizingBlueprintYAML is the host loop's pinned shape: a name, a schema
// version, one lane. The sizing step is dispatched directly against this
// loop (the runTurn shape), so the pin exists to answer the ordinary
// step plumbing, never to run a ladder — and the loop is deliberately
// NOT the item's materialization: the item's loop_slug stays NULL until
// a real loop starts (D10).
const sizingBlueprintYAML = "id: sizing\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: sizing-estimator}\n"

// startSizingRun claims the LOWEST-position pending item `pending →
// sizing` (the claimed edge makes double-dispatch impossible: a second
// caller finds nothing pending) and hands the step to the door. The
// claim is synchronous — the queue reads `sizing` before this returns —
// and the step itself is the goroutine, so the caller (the reactor's
// advance, T6/T7's future verb) never waits on a model.
func (d *Daemon) startSizingRun(ctx context.Context, componentRoot string) (started bool) {
	_, sizingMode, ok, err := d.store.ComponentQueueSettings(componentRoot)
	if err != nil || !ok || sizingMode != "skill" {
		return false // manual mode is not this door's business (D7)
	}
	row, has, err := d.store.FirstPendingQueueItem(componentRoot)
	if err != nil || !has {
		if err != nil {
			d.log.Infof("queue: the sizing dispatch could not read the backlog for %s: %v", componentRoot, err)
		}
		return false
	}
	q := queue.New(d.store)
	item, err := queue.ItemFromRow(row)
	if err != nil {
		d.log.Infof("queue: a pending item in %s could not be read: %v", componentRoot, err)
		return false
	}
	claimed, r := q.Transition(item, queue.StatusSizing)
	if r != nil {
		return false // moved under the dispatch — the OTHER caller owns this sizing now
	}
	slug := fmt.Sprintf("size-item-%d", claimed.ID)
	host, err := d.store.CreatePinnedLoop(store.NewLoop{
		Slug: slug, ComponentRoot: componentRoot, Root: claimed.WorkspaceRoot,
		Goal: "size queued item " + claimed.Goal, Status: "running",
	}, "sizing", 1, sizingBlueprintYAML, "{}")
	if err != nil {
		d.log.Infof("queue: the sizing host loop for item %d could not be pinned: %v — the item returns to pending", claimed.ID, err)
		if _, rr := q.RevertSizing(claimed); rr != nil {
			d.log.Infof("queue: and even the revert refused: %s", rr.Reason)
		}
		return false
	}
	prompt := fmt.Sprintf("Size this queued unit of work (queue item %d).\nGoal: %s\nBlueprint it will run: %s\nWorkspace root it will live under: %s\n\nInspect the workspace, then call report_size EXACTLY ONCE with the range and its basis (D8: a range with its basis, never a bare number). Your handoff mirrors what you reported: size_min, size_max, size_basis.",
		claimed.ID, claimed.Goal, claimed.Blueprint, claimed.WorkspaceRoot)
	go d.runSizingStep(claimed, host.ID, slug, prompt)
	return true
}

// runSizingStep walks the ordinary step door and lands the report — or
// CG5's edge: a failed, abandoned, or shapeless run leaves the item
// PENDING, eligible to be sized again, never stuck in `sizing`.
func (d *Daemon) runSizingStep(item queue.QueueItem, hostLoopID, hostSlug, prompt string) {
	q := queue.New(d.store)
	revert := func(why string) {
		d.log.Infof("queue: sizing run for item %d did not report (%s) — the item returns to pending (CG5)", item.ID, why)
		if _, r := q.RevertSizing(item); r != nil {
			d.log.Infof("queue: and even the revert refused: %s", r.Reason)
		}
	}
	if d.Table == nil {
		revert("the daemon has no step door wired")
		return
	}
	res, err := d.Table.RunStepForLoop(context.Background(),
		protocol.StepParams{Prompt: prompt, Agent: sizingAgent, WorkspaceRoot: item.WorkspaceRoot, LoopSlug: hostSlug},
		protocol.LoopStepIdentity{LoopID: hostLoopID, Step: "size"},
		func(stepID int64) error { return nil })
	if err != nil {
		revert(fmt.Sprintf("the step door refused: %v", err))
		return
	}
	if res.Outcome != "ok" {
		why := res.Outcome
		if res.Reason != "" {
			why += " · " + res.Reason
		}
		revert("the step came back " + why)
		return
	}
	var handoff struct {
		SizeMin   int    `json:"size_min"`
		SizeMax   int    `json:"size_max"`
		SizeBasis string `json:"size_basis"`
	}
	if len(res.Handoff) == 0 {
		revert("an ok step carried no handoff")
		return
	}
	if err := json.Unmarshal(res.Handoff, &handoff); err != nil {
		revert("the handoff is not the reported shape: " + err.Error())
		return
	}
	if _, r := q.ReportSizing(item, queue.Size{Min: handoff.SizeMin, Max: handoff.SizeMax, Basis: handoff.SizeBasis}); r != nil {
		// The report itself broke D8's law or moved under the run — the
		// item goes back to pending like any failed sizing, and the
		// refusal is spoken in full.
		revert("the report was refused: " + r.Reason)
		return
	}
	// The ready item needs its ADVANCE, and who gives it depends on the
	// project's own mode: `auto` chains into the one materialization
	// door; `ask` STOPS — a sizing report is not a loop finishing, and
	// CG4 raises no question outside terminal events. The person advances
	// explicitly, exactly as the cold start demands (no nagging, no
	// timer, nothing watching).
	advance, _, ok, err := d.store.ComponentQueueSettings(item.ComponentRoot)
	if err == nil && ok && advance == "auto" {
		d.advanceNext(context.Background(), item.ComponentRoot)
	}
}
