// continuous_attention.go is CN13's production arm (S27 T11): when a
// scheduler-started loop on a scheduler-owned repository blocks, the unit
// must say "needs you" DURABLY — a decision row, not only the S24 gate
// question, because an unattended loop cannot wait on an answer nobody is
// watching for (G14). The row carries the sibling's taxonomy token
// VERBATIM in its Reason (`model-error`, `cap-hit`, S57's `gate-failed`):
// the scheduler diagnoses nothing (Q2's rule carried from S24 CG2) and the
// kind is displayed, not interpreted. Openness is never stored — the
// socket's Attention arm derives it from the loop row (DB6) — so this file
// only APPENDS, deduplicated to one row per loop block, and says nothing
// else.
//
// What stands OUTSIDE this file, unchanged on purpose:
//   - a queue-sourced item's block is still S24's: CG2's status write and
//     gate question are the ask, and this filer stays quiet for it (the
//     design assigns needs-you to the roadmap-sourced unit, whose item
//     stays `running` — D6/F8 — and whose question alone would be the
//     only news);
//   - a person's own blocked loop (StartedBy != "scheduler") is not the
//     scheduler's news even under an enabled root — the scheduler files
//     news about the units IT started, and nothing else.
package daemon

import (
	"github.com/cjlapao/aeon/internal/store"
)

// blockedNeedsYouSummary is the row's fixed template — computed here,
// never carrying the detail's prose (DB19: the detail rides the question
// the blocked loop already raised; a decision row is a ledger line, and
// the ledger reads in coded words).
const blockedNeedsYouSummary = "the unattended loop blocked — a person is needed"

// refusalNeedsYouSummary is CN13's other half: the roadmap-sourced unit
// whose blueprint died since enable (design: "refused: template-invalid,
// item untouched, needs-you"). A fixed template — ids live in their own
// columns (P3, SEC-033).
const refusalNeedsYouSummary = "the unattended roadmap cannot start — its blueprint is refused"

// fileBlockedNeedsYou files CN13's durable line for one blocked event.
// Every early return below is the daemon declining to speak for something
// that is not its own news: a loop it cannot read, a loop it did not
// start, a unit that is not roadmap-sourced, and a block the ledger has
// already said in exactly this shape.
func (d *Daemon) fileBlockedNeedsYou(componentRoot, loopSlug, blockedKind string) {
	if d.store == nil || loopSlug == "" {
		return
	}
	if blockedKind == "" {
		// The flat emitter arm carries no kind, and the Reason column is
		// a closed vocabulary — the daemon will not invent a token.
		return
	}
	l, err := d.store.GetLoop(store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot})
	if err != nil {
		return // no loop row — the news is about nothing
	}
	if l.StartedBy != "scheduler" {
		return // a person's loop is S24's business, not the ledger's
	}
	item, ok, err := d.store.RoadmapItemByLoopSlug(componentRoot, loopSlug)
	if err != nil || !ok {
		// A queue item's CG2 question is the ask; an orphan loop is
		// nobody's unit. The queue path below the filer is unchanged.
		return
	}
	// Deduped ONE ROW PER LOOP BLOCK: the same blocked episode repeated
	// over the wire (a re-publish, a restart's re-recovery of the same
	// terminal) must not spam the ledger, but any NEW kind after the
	// last one is new news and stands on its own line.
	if last, found, err := d.store.LastDecision("roadmap", item.ID); err == nil && found &&
		last.Decision == "needs-you" && last.Reason == blockedKind && last.LoopSlug == loopSlug {
		return
	}
	if _, err := d.store.AppendDecision(store.SchedulerDecision{
		ComponentRoot: componentRoot, SubjectKind: "roadmap", SubjectID: item.ID,
		Decision: "needs-you", Reason: blockedKind, Summary: blockedNeedsYouSummary,
		ActorKind: schedulerActor.kind, ActorID: schedulerActor.id,
		LoopSlug: loopSlug,
	}); err != nil {
		d.log.Infof("continuous: the needs-you line for roadmap %d could not be written: %v", item.ID, err)
		return
	}
	d.log.Infof("continuous: roadmap item %d needs a person — blocked: %s", item.ID, blockedKind)
}
