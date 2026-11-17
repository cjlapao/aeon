package daemon

// S60 T7 (MQ5, MQ7): the queue's loop starters. A landing loop and a
// resolution loop are ORDINARY loops — started through the daemon's own
// start path (the same internal door the trigger table uses,
// startLoopWith), pinned like every loop, recovered like every loop. What
// is queue-specific is only the identity they carry: the item's With map
// (ids and enums only — SEC-033) and the slug convention that lets the
// reactor join terminal events back to items without an FK (F20).

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mqControlActor is the reactor's identity on LOOP-control writes
// (resume/stop). The loop-control actor CHECK admits system; the queue
// tables admit daemon — two vocabularies, two actors, one reactor.
var mqControlActor = store.Actor{Kind: "system", ID: "merge-queue-reactor"}

// mqLoopState loads one of the item's loops as the engine's own state.
// found=false says the slug names no loop rooted here (the pin is gone, or
// never started — the crash rung before the loop row commits).
func (d *Daemon) mqLoopState(ctx context.Context, root, slug string) (loop.LoopState, bool) {
	if d.store == nil || slug == "" {
		return loop.LoopState{}, false
	}
	rows, err := d.store.LoopsBySlug(slug)
	if err != nil {
		return loop.LoopState{}, false
	}
	var row *store.Loop
	for i := range rows {
		if rows[i].ComponentRoot == root {
			row = &rows[i]
		}
	}
	if row == nil {
		return loop.LoopState{}, false
	}
	pin, err := d.store.ReadPinnedLoop(row.ID)
	if err != nil {
		return loop.LoopState{}, false
	}
	st, err := loop.FromPinned(pin)
	if err != nil {
		return loop.LoopState{}, false
	}
	return st, true
}

// mqLoopEnum maps a pinned loop's status onto the decision table's view.
// `waiting` with last_reason `stopped` is S55's stopped park (the CAS
// parks running→waiting, last_reason='stopped') — the goldens'
// LoopStopped, not a pending question. cap-hit parks are blocked-visible
// holds (LoopBlocked), never stops.
func mqLoopEnum(st loop.LoopState) mergequeue.LoopState {
	switch st.Status {
	case "done", "idle":
		return mergequeue.LoopDone
	case "aborted", "cancelled":
		return mergequeue.LoopStopped
	case "failed":
		return mergequeue.LoopFailed
	case "blocked":
		return mergequeue.LoopBlocked
	case "running":
		return mergequeue.LoopRunning
	default: // waiting (the stopped park first), pending, anything else
		if st.Status == "waiting" && st.LastReason == "stopped" {
			return mergequeue.LoopStopped
		}
		return mergequeue.LoopPending
	}
}

// mqLoopKilled is the stop-contract's signal (behaviour 10): aborted,
// cancelled, or S55's stopped park — states only a PERSON could bring
// back, which is exactly why the item ends rather than waits.
func mqLoopKilled(st loop.LoopState) bool {
	switch st.Status {
	case "aborted", "cancelled":
		return true
	case "waiting":
		return st.LastReason == "stopped"
	}
	return false
}

// mqDenyWord guards resumes: a denied, person-refused, or forge-refused
// action stays refused until a person changes the world (S56's
// revocation is a person's act; a resume would be a bypass).
func mqDenyWord(st loop.LoopState) bool {
	switch st.BlockKind {
	case "action-denied", "action-denied-by-person", "action-refused":
		return true
	}
	return false
}

// mqNextLoopSlug picks the slug a queue-minted loop will run under,
// deterministic per candidate (land-<item>, land-<item>-2, … /
// resolve-<item>, …). A loop already at the row's slug that a resume can
// drive (a conflict-blocked catch-up whose resolution loop just
// finished) is KEPT — resuming the blocked position re-integrates cleanly
// (git.integrate's already-merged rung). A loop terminal or waiting on
// stale facts gets the next free candidate instead.
func (d *Daemon) mqNextLoopSlug(ctx context.Context, root, base, rowSlug string, existing *loop.LoopState) string {
	reusable := existing != nil && existing.Status == "blocked" && !mqDenyWord(*existing)
	if reusable && rowSlug != "" {
		return rowSlug
	}
	if existing == nil && rowSlug != "" {
		return rowSlug // the row names a slug whose loop never committed
	}
	candidates := make([]string, 0, 11)
	candidates = append(candidates, base)
	for n := 2; n < 12; n++ {
		candidates = append(candidates, fmt.Sprintf("%s-%d", base, n))
	}
	for _, cand := range candidates {
		if _, found := d.mqLoopState(ctx, root, cand); !found {
			return cand
		}
	}
	return base
}

// mqIntegrationBranch is the item's integration branch, derived through
// the ONE construction rule (F3): the row's own value when the queue has
// recorded one, else the component-prefixed name the action registry
// re-derives and byte-compares (MQ15 — same prefix source, components.go's
// EffectiveForRoot, or the registry refuses the loop for name drift).
func (d *Daemon) mqIntegrationBranch(item store.MergeItem) string {
	if item.IntegrationBranch != "" {
		return item.IntegrationBranch
	}
	eff, err := d.store.EffectiveForRoot(item.ComponentRoot)
	if err != nil {
		return ""
	}
	names, err := action.NewBranchNamesWith(eff.BranchPrefix.Value, "land", "item")
	if err != nil {
		return ""
	}
	br, err := names.IntegrationBranch(item.ID)
	if err != nil {
		return ""
	}
	return br
}

// mqWith is the landing/resolution loop's With map: ids and enums only.
// queue_mode rides so the pin can seed flags.auto_land for auto mode
// (the land-check/land when-conditions read the flag — auto landing was
// decided by a person through the settings door; the loop re-states it,
// never invents it). dir names the integration worktree for the
// RESOLUTION loop only: the landing loop's WorkDir moves with
// git.integrate's own evidence (MQ7's single-writer rule), the resolve
// loop must START there (pin.go honours only the exact composed path).
func mqWith(item store.MergeItem, mode, method, branch, dir string) map[string]string {
	with := map[string]string{
		"item":        item.ID,
		"head_branch": item.Branch,
		"base_branch": item.BaseBranch,
		"land_method": method,
		"position":    strconv.FormatInt(item.Position, 10),
		"queue_mode":  mode,
	}
	if branch != "" {
		with["integration_branch"] = branch
	}
	if dir != "" {
		with["integration_dir"] = dir
	}
	return with
}

// startLandingLoop starts (not resumes) the item's land-feature loop: the
// workspace is the component root (the integration worktree arrives via
// git.integrate's evidence, never a guess at start), attendedness rides
// the item, and allowOutward covers publish/pr — forge.land is
// irreversible and allowOutward NEVER admits it (F1, enforced upstream by
// ResolveWithGrant; the S56 pin the trigger door passes rides the same
// startExtras).
func (d *Daemon) startLandingLoop(ctx context.Context, item store.MergeItem, slug string, cfg mergequeue.Config) error {
	attended := item.Attended
	_, err := d.startLoopWith(ctx, protocol.LoopStartParams{
		Blueprint:     "land-feature",
		Goal:          fmt.Sprintf("merge-queue: land %s into %s (item %s)", item.Branch, item.BaseBranch, item.ID),
		WorkspaceRoot: item.ComponentRoot,
		Mode:          "standard",
		Slug:          slug,
		Tech:          d.mqOriginTech(ctx, item),
		With:          mqWith(item, string(cfg.Mode), string(cfg.LandMethod), d.mqIntegrationBranch(item), ""),
		Attended:      &attended,
		AllowOutward:  true,
	}, startExtras{StartedBy: "trigger", Autonomy: d.mqAutonomyPin(ctx, item.ComponentRoot)})
	return err
}

// startResolutionLoop starts the resolve-conflict loop running IN the
// integration worktree (the conflict is there), zero outward by the
// blueprint's own shape, and stamps resolve_loop_slug on the item under
// the item's own status guard so the F20 join survives a crash.
func (d *Daemon) startResolutionLoop(ctx context.Context, item store.MergeItem, slug string) error {
	attended := item.Attended
	_, err := d.startLoopWith(ctx, protocol.LoopStartParams{
		Blueprint:     "resolve-conflict",
		Goal:          fmt.Sprintf("merge-queue: resolve the integration conflict of item %s", item.ID),
		WorkspaceRoot: item.ComponentRoot,
		Mode:          "standard",
		Slug:          slug,
		Tech:          d.mqOriginTech(ctx, item),
		With:          mqWith(item, "resolve", "squash", d.mqIntegrationBranch(item), action.IntegrationWorktreePath(item.ComponentRoot, item.ID)),
		Attended:      &attended,
	}, startExtras{StartedBy: "trigger", Autonomy: d.mqAutonomyPin(ctx, item.ComponentRoot)})
	if err != nil {
		return err
	}
	if item.ResolveLoopSlug != slug {
		if _, ferr := d.store.SetMergeItemFields(item.ID, item.Status, store.MergeFields{ResolveLoopSlug: slug}, mqMergeActor()); ferr != nil {
			d.logf("merge-queue: item %s: resolve_loop_slug %s not stamped: %v", item.ID, slug, ferr)
		}
	}
	return nil
}

// ensureLandingLoop is the reactor's door to the landing loop: absent
// (never started, or crashed before its row) → start; blocked without a
// deny word → resume (the crash rung, and the resolve-done
// re-integration ride the resume); everything else is left to the loop
// itself (a waiting loop waits on a gate answer; a running loop runs; a
// terminal loop's facts were already read).
func (d *Daemon) ensureLandingLoop(ctx context.Context, item store.MergeItem) {
	slug := item.LandingLoopSlug
	if slug == "" {
		d.logf("merge-queue: item %s: no landing slug to drive (row says %s)", item.ID, item.Status)
		return
	}
	cfg := d.mqConfig(ctx, item.ComponentRoot)
	st, found := d.mqLoopState(ctx, item.ComponentRoot, slug)
	if !found {
		if err := d.startLandingLoop(ctx, item, slug, cfg); err != nil && !mqSlugTaken(err) {
			d.logf("merge-queue: item %s: landing loop %s could not start: %v", item.ID, slug, err)
		}
		return
	}
	if st.Status == "blocked" && !mqDenyWord(st) {
		d.mqResume(ctx, st, "the merge queue is driving this item again")
	}
}

// mqRetireStaleLanding is the door for a landing loop the engine made
// unresumable: a block at action-conflict has "its own remedy" to the
// engine (LC3 — only stuck/cap parks resume), and that remedy IS the
// resolution loop. Once the worktree says the merge is resolved, the
// parked block is stale: the queue RETIRES it — cancels the parked loop,
// stamps a fresh landing slug, starts a fresh loop. The fresh loop
// replays cold (the crash-rung shape): catch-up finds the merge already
// done and rides straight to a verification of the NEW head from
// scratch (MQ9: the resolver's claim is never the evidence). Deviation
// recorded at T7 — the spec names neither cancel nor restart here, and
// the engine's control doors refuse the only literal reading.
func (d *Daemon) mqRetireStaleLanding(ctx context.Context, item store.MergeItem, st loop.LoopState) {
	d.logf("merge-queue: item %s: the conflict block is stale — the index reports no unmerged path; retiring %s", item.ID, st.Slug)
	if _, refusal, err := d.mqControlCancel(ctx, st.ID, mqControlActor, "merge-queue: conflict resolved — the parked catch-up is retired and re-run cold"); err != nil {
		d.logf("merge-queue: item %s: retiring %s failed: %v", item.ID, st.Slug, err)
		return
	} else if refusal != nil {
		d.logf("merge-queue: item %s: retiring %s refused: %s", item.ID, st.Slug, refusal.Detail)
		return
	}
	slug := d.mqNextLoopSlug(ctx, item.ComponentRoot, "land-"+item.ID, "", nil)
	if _, err := d.store.SetMergeItemFields(item.ID, item.Status, store.MergeFields{LandingLoopSlug: slug}, mqMergeActor()); err != nil {
		d.logf("merge-queue: item %s: fresh landing slug %s not stamped: %v", item.ID, slug, err)
		return
	}
	fresh := item
	fresh.LandingLoopSlug = slug
	if err := d.startLandingLoop(ctx, fresh, slug, d.mqConfig(ctx, item.ComponentRoot)); err != nil && !mqSlugTaken(err) {
		d.logf("merge-queue: item %s: fresh landing loop %s could not start: %v", item.ID, slug, err)
	}
}

// ensureResolutionLoop is the policy=resolve (and answered-resolve) door:
// absent → start; blocked → resume; done/running/waiting → leave it.
func (d *Daemon) ensureResolutionLoop(ctx context.Context, item store.MergeItem) {
	slug := item.ResolveLoopSlug
	if slug == "" {
		slug = d.mqNextLoopSlug(ctx, item.ComponentRoot, "resolve-"+item.ID, "", nil)
		if _, err := d.store.SetMergeItemFields(item.ID, item.Status, store.MergeFields{ResolveLoopSlug: slug}, mqMergeActor()); err != nil {
			d.logf("merge-queue: item %s: resolve slug %s not stamped: %v", item.ID, slug, err)
			return
		}
		item.ResolveLoopSlug = slug
	}
	st, found := d.mqLoopState(ctx, item.ComponentRoot, slug)
	if !found {
		if err := d.startResolutionLoop(ctx, item, slug); err != nil && !mqSlugTaken(err) {
			d.logf("merge-queue: item %s: resolution loop %s could not start: %v", item.ID, slug, err)
		}
		return
	}
	if st.Status == "blocked" && !mqDenyWord(st) {
		d.mqResume(ctx, st, "the merge queue is retrying the resolution")
	}
}

// mqResume resumes a blocked queue loop. A refusal is logged, never
// forced — the next tick re-reads the pin and tries only while the pin
// still says blocked.
func (d *Daemon) mqResume(ctx context.Context, st loop.LoopState, why string) {
	if _, refusal, err := d.mqControlResume(ctx, st.ID, mqControlActor, "merge-queue: "+why); err != nil {
		d.logf("merge-queue: loop %s resume failed: %v", st.Slug, err)
		return
	} else if refusal != nil {
		d.logf("merge-queue: loop %s resume refused: %s", st.Slug, refusal.Detail)
	}
}

// StopMergeQueueItemLoops stops the item's landing and resolution loops
// (T8's dequeue and the conflict-abort path call it: a dequeued item must
// not keep publishing behind the person's back). Terminals are skipped;
// a refusal is logged — the loops' doors answer the person; the queue
// never forces.
func (d *Daemon) StopMergeQueueItemLoops(ctx context.Context, item store.MergeItem) {
	for _, slug := range []string{item.LandingLoopSlug, item.ResolveLoopSlug} {
		if slug == "" {
			continue
		}
		st, found := d.mqLoopState(ctx, item.ComponentRoot, slug)
		if !found {
			continue
		}
		switch st.Status {
		case "done", "aborted", "cancelled", "failed":
			continue
		}
		if _, refusal, err := d.StopLoop(ctx, st.ID, mqControlActor, "merge-queue: the item left the queue"); err != nil {
			d.logf("merge-queue: loop %s stop failed: %v", st.Slug, err)
		} else if refusal != nil {
			d.logf("merge-queue: loop %s stop refused: %s", st.Slug, refusal.Detail)
		}
	}
}

// mqSettleItemWorktree settles an ENDED item's integration worktree: the
// PA5 settle-once order (RemoveWorktree first, ledger row second), with
// "missing is success" (settleWorktree's own rule). Behaviour 1's tail —
// a merged item must not leave a worktree and a live row behind.
func (d *Daemon) mqSettleItemWorktree(ctx context.Context, item store.MergeItem) {
	if item.LandingLoopSlug == "" {
		return
	}
	rows, err := d.store.ListLiveLoopWorktrees(store.Scope{LoopSlug: item.LandingLoopSlug, ComponentRoot: item.ComponentRoot})
	if err != nil {
		d.logf("merge-queue: item %s: worktree rows unreadable: %v", item.ID, err)
		return
	}
	svc := d.actionServices(item.ComponentRoot)
	for _, row := range rows {
		if row.OwnerKind != "integration" || row.OwnerID != item.ID {
			continue
		}
		if svc != nil && svc.Git != nil && svc.Git.Git != nil {
			if _, err := svc.Git.RemoveWorktree(ctx, item.ComponentRoot, row.Path); err != nil {
				// A missing worktree is success; a real error still
				// settles the row (removed_at is the durable signal).
				d.logf("merge-queue: item %s: worktree %s removal reported: %v (row settles anyway)", item.ID, row.Path, err)
			}
		}
		if err := d.store.RemoveLoopWorktree(row.ID); err != nil {
			d.logf("merge-queue: item %s: worktree row %d not settled: %v", item.ID, row.ID, err)
		}
	}
}

// mqSlugTaken recognises the race where two callers picked the same
// deterministic slug: the loser's start failed on uniqueness, and the
// winner's loop IS the loop — not an error to report.
func mqSlugTaken(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "already")
}

// mqAutonomyPin is the S56 pin the queue's loops start under: the root's
// standing grants (the ceiling). No grants is not an error — a nil pin is
// the same answer the event door gives.
func (d *Daemon) mqAutonomyPin(ctx context.Context, root string) *store.AutonomyPin {
	pin, err := d.resolveAutonomy(ctx, autonomy.OriginEvent, root, nil)
	if err != nil {
		return nil
	}
	return pin
}

// mqOriginTech carries the feature loop's tech onto the resolution loop
// so the resolve lane's by-tech-tag agent resolves in the language the
// feature was written in. No origin loop (a hand-enqueued item) is no
// tech: the lane refuses by name, which holds the queue honestly.
func (d *Daemon) mqOriginTech(ctx context.Context, item store.MergeItem) string {
	if item.LoopSlug == "" {
		return ""
	}
	st, found := d.mqLoopState(ctx, item.ComponentRoot, item.LoopSlug)
	if !found {
		return ""
	}
	return st.With["tech"]
}
