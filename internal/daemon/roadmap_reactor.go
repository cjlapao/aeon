package daemon

// S26 T3 + T4 — the roadmap's reactor: the terminal consumer, joined at
// the daemon's own emission point beside the queue's (publishLoop), the
// S24 T3 precedent carried exactly. Its whole contract:
//
//   - an item's MATERIALIZED loop finishing `done` lands the item `done`
//     — the machine's only edge out of running (running→cancelled is
//     refused by R1: a loop in flight is the loop's own record; by the
//     same logic a loop FINISHED done is the evidence done means).
//   - an IDEATION loop finishing `done` lands the brief pointer and
//     `brief-agreed`. The engine already refused `done` while the step's
//     declared produces: brief.md was missing (missingArtifact, the
//     EXISTING check R3's call-site assertion points at) — so the event
//     itself is the evidence; the reactor only says where the file is.
//   - a BLOCKED loop leaves the item's own status untouched (D6: an item's
//     status is its own field, not a projection of its loop's) and is
//     spoken. A blocked ideation leaves the item ideating — a person
//     cancels; nothing here silently advances or reverts.
//   - an untracked loop is NOT this reactor's business: the lookups miss
//     and it does nothing, quietly (the queue's own rule, D8's sibling —
//     loop.start with zero roadmap items is untouched by anything here).
//
// Like the queue's reactor it never blocks the emit path: every branch is
// a goroutine off onQueueTerminal's call site.

import (
	"context"
	"path/filepath"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// onRoadmapTerminal is the subscription, called from publishLoop at the
// daemon's own emission point beside onQueueTerminal. It resolves nothing
// on the caller's thread — dispatch to the goroutine is the whole
// function (V18: a roadmap reaction must never delay the step that just
// finished).
func (d *Daemon) onRoadmapTerminal(kind, componentRoot, loopSlug string, fields map[string]any) {
	if d.store == nil {
		return
	}
	status, _ := fields["status"].(string)
	loopID, _ := fields["loopId"].(string)
	go func() {
		// The store can close under an in-flight terminal — a daemon
		// shutting down mid-event, or a test winding its store: Close
		// nils the handles, and a nil *sql.DB panics a query. A roadmap
		// REACTION must never be the thing that takes the process down:
		// the loop's own rows are the truth, and a reaction lost here is
		// a reaction lost as to any crash — recovery replays from rows
		// (P29), and stamped pointers land on the replayed terminal.
		defer func() {
			if rec := recover(); rec != nil {
				d.log.Infof("roadmap: the terminal reactor for loop %s recovered from %v — the loop's own rows remain the truth (P29)", loopSlug, rec)
			}
		}()
		d.handleRoadmapTerminal(context.Background(), kind, componentRoot, loopSlug, loopID, status)
	}()
}

// handleRoadmapTerminal does the deciding. Package-visible to the tests —
// they call it synchronously through publishLoop, the wired path.
func (d *Daemon) handleRoadmapTerminal(ctx context.Context, kind, componentRoot, loopSlug, loopID, status string) {
	r := roadmap.New(d.store)
	// S27 T5 (CN19): every terminal on a repository the scheduler owns
	// hastens a tick — the done edge is exactly when the next pick is
	// hot, and a block leaves a fact the tick should see early. The kick
	// rides the LANDINGS and the quiet returns alike; for a repository
	// the scheduler does not own, continuousOwns answers false and this
	// reactor behaves byte-identically to S50's.
	defer func() {
		if d.continuousOwns(componentRoot) {
			d.continuousKick(componentRoot)
		}
	}()

	// (1) The stamped path: the item's OWN materialized loop (direct
	// branch — its slug was recorded at start).
	if row, ok, err := d.store.RoadmapItemByLoopSlug(componentRoot, loopSlug); err != nil {
		d.log.Infof("roadmap: the terminal lookup failed for loop %s: %v", loopSlug, err)
		return
	} else if ok {
		it, cerr := roadmap.ItemFromRow(row)
		if cerr != nil {
			d.log.Infof("roadmap: the row behind loop %s could not be read: %v", loopSlug, cerr)
			return
		}
		d.landRoadmapTerminal(r, it, kind, status, "loop")
		return
	}

	// (2) The queue branch: this loop belongs to a QUEUE item whose
	// durable with_json carries the roadmap link — S24 started it, and
	// the item never got its loop pointer (the door could not: the start
	// was S24's). Record, then land the terminal like any other.
	if qrow, ok, err := d.store.QueueItemByLoopSlug(componentRoot, loopSlug); err != nil {
		d.log.Infof("roadmap: the queue-side terminal lookup failed for loop %s: %v", loopSlug, err)
		return
	} else if ok {
		id, role := queueRoadmapLink(qrow)
		if id == 0 || role != "materialization" {
			return // an ordinary queued item — not this reactor's news
		}
		it, found, err := r.Show(id)
		if err != nil || !found {
			if err != nil {
				d.log.Infof("roadmap: the queued item %d behind loop %s could not be read: %v", id, loopSlug, err)
			}
			return
		}
		if it.ComponentRoot != componentRoot || it.Status != roadmap.StatusRunning {
			return
		}
		if _, ref := r.RecordLoopAudited(id, componentRoot, loopSlug, roadmap.SystemReactor); ref != nil {
			d.log.Infof("roadmap: the loop record for item %d was refused: %s", id, ref.Reason)
		}
		if fresh, found, err := r.Show(id); err == nil && found {
			it = fresh
		}
		d.landRoadmapTerminal(r, it, kind, status, "queue loop")
		return
	}

	// (3) The pinned links: ideation loops (and direct-start loops whose
	// pointer write died mid-door) are found through the pin's durable
	// with map — loop.start's OWN channel, no new column.
	if loopID == "" {
		return
	}
	pin, err := d.store.ReadPinnedLoop(loopID)
	if err != nil {
		return // unreadable pin is not a roadmap event
	}
	id, role := pinRoadmapLink(pin.RuntimeJSON)
	if id == 0 {
		return // a loop with no roadmap link — the D8 case, quietly
	}
	it, found, err := r.Show(id)
	if err != nil || !found || it.ComponentRoot != componentRoot {
		if err != nil {
			d.log.Infof("roadmap: the item %d behind pinned loop %s could not be read: %v", id, loopID, err)
		}
		return
	}
	switch role {
	case "ideation":
		if kind == protocol.KindLoopFinished && status == "done" && it.Status == roadmap.StatusIdeating {
			briefPath := d.briefPathFor(pin)
			if _, ref := r.LandBriefAudited(id, briefPath, roadmap.SystemReactor); ref != nil {
				d.log.Infof("roadmap: the brief-agreed landing for item %d was refused: %s", id, ref.Reason)
				return
			}
			d.log.Infof("roadmap: item %d reads brief-agreed — ideation loop %s finished with brief.md (the engine's produces check was the gate)", id, loopSlug)
			return
		}
		d.reportRoadmapBlock(it, loopSlug, kind, status, "ideation")
	case "materialization":
		if it.Status != roadmap.StatusRunning || it.LoopSlug != "" {
			return // stamped already (path 1) or not the running rung
		}
		if _, ref := r.RecordLoopAudited(id, componentRoot, loopSlug, roadmap.SystemReactor); ref != nil {
			d.log.Infof("roadmap: the crash-gap loop record for item %d was refused: %s", id, ref.Reason)
			return
		}
		if fresh, found, err := r.Show(id); err == nil && found {
			it = fresh
		}
		d.landRoadmapTerminal(r, it, kind, status, "pinned loop")
	}
}

// landRoadmapTerminal is the done/blocked verdict for the running rung:
// done (and only done) advances; blocked speaks and leaves the item's
// own status; any other status on a finished event is a wire surprise
// worth a log and no action (the queue reactor's own discipline).
func (d *Daemon) landRoadmapTerminal(r *roadmap.Roadmap, it roadmap.Item, kind, status, what string) {
	if kind != protocol.KindLoopFinished {
		d.reportRoadmapBlock(it, it.LoopSlug, kind, status, what)
		return
	}
	if status != "done" {
		d.log.Infof("roadmap: loop.finished for item %d's %s carried status %q — a status this reactor does not know moves nothing", it.ID, what, status)
		return
	}
	if it.Status != roadmap.StatusRunning {
		return // not on the rung the done edge leaves from — quietly
	}
	if _, ref := r.TransitionAudited(it.ID, roadmap.StatusDone, "transition", roadmap.SystemReactor); ref != nil {
		d.log.Infof("roadmap: the done report for item %d was refused: %s", it.ID, ref.Reason)
		return
	}
	d.log.Infof("roadmap: item %d reads done — its loop finished done", it.ID)
}

// reportRoadmapBlock says the blocked news without touching the item:
// D6's ruling in one sentence — an item's status is its own field, and
// the block lives in the loop's record until a person acts (cancel is
// always there for a pre-loop rung; a running item's loop is the loop's
// own record).
func (d *Daemon) reportRoadmapBlock(it roadmap.Item, loopSlug, kind, status, what string) {
	if kind == protocol.KindLoopBlocked {
		d.log.Infof("roadmap: item %d's %s %s reported itself blocked — the item keeps its own status %q (D6); the loop's rows are where the block lives", it.ID, what, loopSlug, it.Status)
	}
}

// briefPathFor names where the ideation loop's brief.md is. The loop filed it as
// a document; the daemon exports the hard copy to .projects/{slug}/brief.md under
// the loop's workspace and the pointer names that file, so a planner handed the
// pointer can read it. A loop that wrote the file itself (a body that still has
// the workspace `write`) keeps its own path. A pin without a readable root yields
// the honest empty — and SetArtifacts refuses an empty pointer rather than invent
// one.
func (d *Daemon) briefPathFor(pin store.PinnedLoop) string {
	if pin.Loop.Root == "" {
		return ""
	}
	sc := store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot}
	if p, err := d.exportHardCopy(pin.Loop.Root, sc, "brief.md"); err != nil {
		d.log.Infof("roadmap: the hard copy of %s's brief could not be written: %v", pin.Loop.Slug, err)
	} else if p != "" {
		return p
	}
	return filepath.Join(pin.Loop.Root, "brief.md")
}
