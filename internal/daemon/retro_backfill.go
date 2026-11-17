package daemon

// retro_backfill.go — S63 T2 (RT13): the optional, human-only backfill of
// outcome facts for loops that predate the recorder. The read source is
// history.Merge — the same merged live-plus-archived view a person reads —
// and a backfill row may claim ONLY what that view carries: status,
// started/ended, the floored cost rollup, and (for a blocked loop) the
// stored reason's kind. Everything the recorder can read but history
// cannot — counters, per-agent rows, attendedness, the pin's coordinates
// — stays NULL and the row says complete=0 (RT6: backfilled rows are the
// thinnest honest rows in the table, and `source: 'backfill'` plus
// coverage (RT9) says so out loud).
//
// It never writes a learning (RT13), never touches a pin, and defaults to
// a dry run: the preview answers "would write N, skip M (already
// recorded)" before anything is written, and only an explicit non-dry
// apply writes. The socket method (retro.backfill, human door) lands in
// T5 over this function; this file is the whole implementation.

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/retro"
	"github.com/cjlapao/aeon/internal/store"
)

// RetroBackfillReport is the preview AND the outcome of one pass — the
// dry run answers every count except Written, which stays 0 there (RT13:
// the dry run writes nothing, verified by test).
type RetroBackfillReport struct {
	Scanned         int            // history entries considered
	DryRun          bool           // what this pass was asked to do
	ByKind          map[string]int // entries per outcome kind (preview and applied alike)
	SkippedRecorded int            // loops that already have fact rows — any kind, any source
	SkippedRule     int            // F15 / retrospective skips (the recorder's own rules)
	SkippedOpen     int            // not terminal yet (running/waiting/idle: no outcome to record)
	SkippedNoScope  int            // archived entries the view cannot place in a component ("" root)
	Written         int            // rows written — 0 on a dry run, always
}

// terminalStatus is the history view's word set for a loop whose outcome
// is decided: done and aborted are outcomes; blocked is the outcome RT2
// names by its reason kind. Every other status (running, waiting, idle,
// "") is an OPEN loop — the backfill records nothing for it, because it
// has not happened yet.
func terminalStatus(status string) (outcomeKind string, terminal bool) {
	switch status {
	case "done":
		return "done", true
	case "aborted":
		return "aborted", true
	case "blocked":
		return "blocked", true
	}
	return "", false
}

// RetroBackfill walks the merged history and writes a thin backfill row
// for each terminal loop with no fact rows. componentRoot empty spans
// every component (history.Options' own `--all` semantics — an explicit
// ask, never a default the daemon invents).
func (d *Daemon) RetroBackfill(ctx context.Context, componentRoot string, dryRun bool) (RetroBackfillReport, error) {
	rep := RetroBackfillReport{DryRun: dryRun, ByKind: map[string]int{}}
	entries, err := history.Merge(d.store, history.Options{Scope: store.ComponentScope{ComponentRoot: componentRoot}})
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		rep.Scanned++
		kind, terminal := terminalStatus(e.Status)
		if !terminal {
			rep.SkippedOpen++
			continue
		}
		if e.ComponentRoot == "" {
			// No root means no scope — the row could not be read back by
			// anyone. An honest skip count, never a guessed root.
			rep.SkippedNoScope++
			continue
		}
		if _, skip := retro.SkipPin(e.ComponentRoot, e.Slug, ""); skip {
			rep.SkippedRule++
			continue
		}
		if e.Kind == history.KindLive {
			// The retrospective's own loops need the pin to spot (RT5);
			// the id is the live entry's own. An unreadable pin skips —
			// the same rule the recorder applies to the same absence.
			pin, err := d.store.ReadPinnedLoop(e.ID)
			if err != nil {
				rep.SkippedOpen++
				continue
			}
			if _, skip := retro.SkipPin(pin.Loop.ComponentRoot, pin.Loop.Slug, pin.BlueprintID); skip {
				rep.SkippedRule++
				continue
			}
		}
		kindStr := kind
		if kind == "blocked" {
			kindStr = retro.NormalizeBlockedKind(e.Reason) // the STORED reason, kind-cut (H4: verbatim, never synthesized)
		}
		has, err := d.store.RetroLoopHasFacts(e.ComponentRoot, e.Slug)
		if err != nil {
			return rep, err
		}
		if has {
			rep.SkippedRecorded++
			continue
		}
		rep.ByKind[kindStr]++
		if dryRun {
			continue
		}
		out := backfillOutcome(e, kindStr)
		if _, _, err := d.store.RecordRetroOutcome(out, nil); err != nil {
			// A failed row is a gap the NEXT dry run will name again —
			// the backfill is idempotent precisely so this is recoverable.
			d.log.Infof("retro: backfill skipped %s/%s: %v", e.ComponentRoot, e.Slug, err)
			continue
		}
		rep.Written++
	}
	return rep, nil
}

// backfillOutcome is RT13's thin row: the four things the history view
// actually carries, the rest NULL, complete=0 always. No agent rows:
// the recorder reads them from step rows, which an archived loop has
// lost (T0 §3 measured the cascade) and history never merged.
func backfillOutcome(e history.Entry, outcomeKind string) store.RetroOutcome {
	out := store.RetroOutcome{
		ComponentRoot: e.ComponentRoot,
		LoopSlug:      e.Slug,
		OutcomeKind:   outcomeKind,
		TerminalKey:   "backfill@" + outcomeKind + "@" + e.Timeline,
		Complete:      false,
		Source:        "backfill",
	}
	if e.StartedAt != "" && e.EndedAt != "" {
		if start, err1 := time.Parse(time.RFC3339Nano, e.StartedAt); err1 == nil {
			if end, err2 := time.Parse(time.RFC3339Nano, e.EndedAt); err2 == nil && !end.Before(start) {
				ms := end.Sub(start).Milliseconds()
				out.WallMS = &ms
			}
		}
	}
	if e.Cost.StepCount > 0 { // zero contributing steps is an honest nothing, not a floor of $0
		floor := e.Cost.Total
		steps, unknown := e.Cost.StepCount, e.Cost.UnknownCount
		out.CostUSDFloor, out.CostSteps, out.CostUnknownSteps = &floor, &steps, &unknown
	}
	return out
}
