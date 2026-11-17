package daemon

// retro_reconcile.go — S63 T3 (RT4 §4): the crash gap between the fact
// row and its learning. The recorder writes the fact, then the learning,
// then the link; a crash anywhere before the link leaves the outcome
// sitting on its milestone UNLINKED, and reconcile — UnlinkedMilestoneOutcomes
// over the generated ladder, then each missing write — closes it exactly
// once. The link row IS the lock: reruns find nothing, and the milestone
// gate inside WriteAt refuses a second learning even if a caller re-presents
// an outcome.
//
// Ordering is load-bearing and it lives in cmd/aeon/serve.go: reconcile
// runs AFTER the inert seed apply (the template rows must exist — rule 6
// refuses a render from code) and BEFORE StartLoops (before the recorder
// can fire, so reconcile and the reactor never count the same milestone
// from two doors at once).

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/retro"
)

// The seams (T2's retroCompose precedent): test-patchable, production
// never reassigns.
var (
	retroWrite   = retro.Write
	retroWriteAt = retro.WriteAt
)

// retroDeps builds retro's world over this daemon's store. The learning
// path is the audited S52 door with the system actor (S52 made AppendAs
// production; T3's spec word `Append` is that path's pre-S52 name).
func (d *Daemon) retroDeps() retro.Deps {
	return retro.Deps{
		S: d.store,
		L: learnings.New(d.store),
		Log: func(s string) {
			d.log.Infof("%s", s)
		},
		RenderCost: retroCostSentence,
	}
}

// retroCostSentence is T3 §3's "call it, do not copy it": the P32
// sentence is history's own renderer, reached through a function VALUE —
// retro never imports history (RT3.2 keeps its imports store + learnings
// + stdlib), and the words here are the same bytes the history surface
// prints, because they come from the same function.
//
// The mapping is exact: the fact row's floor is the Rollup Total; any
// partial/unknown count makes it a Floor; unknown travels as K. A row
// whose floor holds no partial or unknown counts was a total the recorder
// saw — Floor false, a bare number, never "at least".
func retroCostSentence(o retro.Outcome) string {
	if o.CostUSDFloor == nil {
		return ""
	}
	r := history.Rollup{
		Total:        *o.CostUSDFloor,
		StepCount:    derefInt(o.CostSteps),
		UnknownCount: derefInt(o.CostUnknownSteps),
	}
	r.Floor = derefInt(o.CostPartialSteps) > 0 || derefInt(o.CostUnknownSteps) > 0
	return r.Render()
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// ReconcileRetro closes the crash gap: every milestone outcome without a
// learning link gets exactly one write attempt, at its own rank (M3 —
// the count names the milestone the row sits on, not the group's today).
// A store error aborts the pass (logged by the caller); a per-row refusal
// is logged inside WriteAt and the next start retries.
func (d *Daemon) ReconcileRetro(ctx context.Context) (int, error) {
	if d.store == nil {
		return 0, nil // a storeless daemon records nothing and reconciles nothing
	}
	rows, err := d.store.UnlinkedMilestoneOutcomes(retro.Milestones)
	if err != nil {
		return 0, err
	}
	written := 0
	for _, m := range rows {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		if err := retroWriteAt(ctx, d.retroDeps(), m.Outcome, m.Rank); err != nil {
			d.log.Infof("retro: reconcile could not write the learning for outcome %d: %v", m.Outcome.ID, err)
			continue
		}
		written++
	}
	stampRetroReconcile(written)
	return written, nil
}

// The reconcile state retro.status answers with (T5): what the LAST
// completed pass did, kept in memory like the recorder's counters —
// reconcile runs at boot, and a daemon that never completed one says
// neverRun rather than a zero dressed as a run. A pass that aborted
// (ctx cancel or store error) does NOT stamp: an unfinished pass is not
// a pass.
func stampRetroReconcile(written int) {
	retroReconcileAtN.Store(time.Now().UnixNano())
	retroReconcileWrittenN.Store(int64(written))
}

var (
	retroReconcileAtN      atomic.Int64 // unix-nano; 0 = no completed pass since start
	retroReconcileWrittenN atomic.Int64
)

func retroReconcileState() (at time.Time, written int, ran bool) {
	n := retroReconcileAtN.Load()
	if n == 0 {
		return time.Time{}, 0, false
	}
	return time.Unix(0, n).UTC(), int(retroReconcileWrittenN.Load()), true
}
