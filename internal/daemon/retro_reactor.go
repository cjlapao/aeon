package daemon

// retro_reactor.go — S63 T2 (RT2): the outcome recorder's door at the
// daemon's own emission point. One goroutine, one fact row per terminal
// event, and the event path pays nothing for it (V18 — the queue,
// roadmap, triage, gate, merge-queue and environment reactors each set
// the precedent; this is the seventh call, not a new mechanism).
//
// RT3 is the reason this file can never grow an action: the recorder
// READS the loop's persisted rows and writes facts. It never starts a
// loop, never edits a setting, never routes anything. A dropped row is
// counted and logged (RT6) — the fact half cannot be replayed from
// events, so the gap is NAMED in retroDropped and in the daemon's
// coverage answer (T5), never smoothed over.

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/retro"
)

// retroDropped is RT6's honest counter: terminal events whose fact row
// could not be written. In-memory and never a table — the same V13 shape
// as the other daemon counters (RT4: no stored tallies; a daemon restart
// resets it, and the coverage answer says since-when, not a total it
// cannot prove). retroLastFacts is the last SUCCESSFUL write, so a
// person can tell "no terminals since" from "terminals, none recorded".
var (
	retroDropped    atomic.Uint64
	retroLastFactsN atomic.Int64 // unix-nano; 0 = no successful write since start
)

// retroDroppedCount / retroLastFacts are T5's coverage reads.
func retroDroppedCount() uint64 { return retroDropped.Load() }
func retroLastFacts() time.Time {
	n := retroLastFactsN.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// retroCompose is the Compose seam: production runs retro.Compose through
// this one variable, and the V18 tests hold it on a gate to prove the
// emit path never waits for the recorder — the only way to watch a
// "stuck recorder" without slowing the store for every other reactor's
// tests too. A swap point, not a feature: it ships initialized to the
// real Compose and nothing else assigns it.
var retroCompose = retro.Compose

// onRetroTerminal is the emit-path half: it never blocks and never
// decides — same four arguments as the six reactors beside it (RT2),
// and it returns as the goroutine departs. A payload without a loop id
// is not a loop event's business; the guard is the cheap one, before
// the goroutine.
func (d *Daemon) onRetroTerminal(kind, componentRoot, loopSlug string, fields map[string]any) {
	loopID, _ := fields["loopId"].(string)
	if loopID == "" {
		return
	}
	f := map[string]any{"status": fields["status"], "reason": fields["reason"]}
	go d.handleRetroTerminal(context.Background(), kind, loopID, f)
}

// handleRetroTerminal does the deciding. Package-visible to the tests —
// they call it synchronously through publishLoop, which is the wired
// path; the seam under test is behaviour, not the goroutine.
func (d *Daemon) handleRetroTerminal(ctx context.Context, kind, loopID string, fields map[string]any) {
	if d.store == nil {
		return
	}
	pin, err := d.store.ReadPinnedLoop(loopID)
	if err != nil {
		// Behaviour §1's last skip: a loop with no readable pin records
		// nothing. The read failure IS the skip — say it at info, the
		// same rank the queue reactor gives its untracked news.
		d.log.Infof("retro: no readable pin for loop %s — nothing recorded", loopID)
		return
	}
	if why, skip := retro.SkipPin(pin.Loop.ComponentRoot, pin.Loop.Slug, pin.BlueprintID); skip {
		d.log.Infof("retro: skipped loop %s: %s", pin.Loop.Slug, why)
		return
	}

	outcomeKind := ""
	switch kind {
	case protocol.KindLoopFinished:
		status, _ := fields["status"].(string)
		if status == "" {
			status = "done" // the engine's own default at endStatus; an empty status field is finished/done
		}
		outcomeKind = retro.OutcomeKindForFinished(status)
	case protocol.KindLoopBlocked:
		outcomeKind = retro.NormalizeBlockedKind(retroReasonKind(fields))
	default:
		return // loop.run.finished and everything else are not in scope (brief §4)
	}

	out, agents, missing, err := retroCompose(ctx, d.store, retro.Event{LoopID: loopID, OutcomeKind: outcomeKind})
	if err != nil {
		retroDropped.Add(1)
		d.log.Infof("retro: dropped the facts of loop %s: %v", loopID, err)
		return
	}
	for _, field := range missing {
		// Behaviour §2's one log line per unreadable field — the names
		// are the whole payload, no values, no text.
		d.log.Infof("retro: loop %s stored a NULL for %s (row marked incomplete)", pin.Loop.Slug, field)
	}
	out.Complete = len(missing) == 0
	id, inserted, err := d.store.RecordRetroOutcome(out, agents)
	if err != nil {
		retroDropped.Add(1)
		d.log.Infof("retro: dropped the facts of loop %s (%s): %v", loopID, pin.Loop.Slug, err)
		return
	}
	retroLastFactsN.Store(time.Now().UnixNano())
	if inserted {
		// T3 (RT4) rides the same goroutine, after the fact row: a
		// milestone check, a template render, one learning through
		// S28's path. A replay (inserted=false) never re-counts and
		// never writes — the row already spoke. A failing write here
		// is LOGGED, not dropped-counted: the FACT stands; only the
		// learning is missing, and reconcile retries it at the next
		// start (RT4 §1's refusal rule).
		if werr := retroWrite(ctx, d.retroDeps(), id); werr != nil {
			d.log.Infof("retro: the milestone learning for outcome %d failed: %v", id, werr)
		}
	}
}

// retroReasonKind extracts the reason KIND from the emit's payload in the
// two shapes the engine actually sends — the *loop.BlockedReason struct
// (whose Kind field the blockTo sites fill) or a plain string (the queue
// reactor's queueReason duck-typing is the precedent; a string arrives
// with free text AFTER the kind token, which NormalizeBlockedKind cuts
// and caps — detail text never reaches a fact row either way).
func retroReasonKind(fields map[string]any) string {
	if r, ok := fields["reason"].(*loop.BlockedReason); ok && r != nil {
		return r.Kind
	}
	if s, ok := fields["reason"].(string); ok {
		return s
	}
	return ""
}
