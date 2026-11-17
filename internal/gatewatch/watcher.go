// Package gatewatch is S57 T7's post-merge watch (VG5): the bounded poll
// that answers "did the checks go red on the merged sha?" from ROWS, never
// from memory. It owns no SQL and no triage — the daemon adapts both in
// internal/daemon (gatewatch_wiring.go), so this package tests against a
// fake store and forge.Fake the way the other seams do.
//
// The watch is one-directional (pending/unknown → green | red | expired),
// the poll is bounded (a 2h window, a 30s→5min backoff), and silence is
// never green: a watch that gets no answer expires, which is the honest
// absence the forge already answers with `unknown`.
package gatewatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/forge"
)

// The settings keys the window and the poll interval live under (S44's
// declaration discipline — declared in internal/config, read live per tick).
const (
	KeyWatchWindow   = "gates.watchWindow"
	KeyWatchInterval = "gates.watchInterval"
)

// The package defaults: what a tick uses when the settings store is
// absent, unreadable or holds a value that does not parse. The watcher
// keeps polling on a bad value — a setting that cannot be read is not a
// reason to stop watching, and the log says so.
const (
	DefaultWatchWindow   = 2 * time.Hour
	DefaultWatchInterval = 30 * time.Second
)

// MaxBackoff caps the reschedule growth (the spec's "30 s → 5 min cap").
const MaxBackoff = 5 * time.Minute

// The watch's closed state set. The strings are the ledger's (the store's
// merge_watch CHECK and its constants); TestWatchStatesMatchTheMigration
// holds the two in lockstep.
const (
	StatePending = "pending"
	StateGreen   = "green"
	StateRed     = "red"
	StateUnknown = "unknown"
	StateExpired = "expired"
)

// The event kinds the watch publishes through the daemon's event door
// (declared in internal/protocol's Kinds; the conformance fixtures ride
// the same change).
const (
	EventGreen = "merge.watch.green"
	EventRed   = "merge.watch.red"
)

// Row is a due watch row as the poll sees it: the watch's own columns
// plus the merge event it watches, joined by the store so the poll
// carries the ids (loop slug, PR, rule revision, the measured run) the
// filer and the events name — one read, no second trip.
type Row struct {
	MergeEventID  string
	ComponentID   string
	ComponentRoot string
	BaseRef       string
	SHA           string
	State         string
	Attempts      int
	Detail        string
	LoopSlug      string
	PR            int
	RuleRevision  *int
	VerifyRunID   *string
}

// MergeEventRef is the sweep's view of a merge event without a watch: the
// crash window the startup sweep repairs.
type MergeEventRef struct {
	ID            string
	ComponentRoot string
	ComponentID   string
	PR            int
	MergeSHA      string
	MergedAt      string // ISO UTC (the window's start)
}

// Store is the ledger seam. The daemon adapts *store.Store; tests use a
// fake. Every method reads or writes rows — nothing here is memory.
type Store interface {
	// DueMergeWatches returns the pending/unknown rows scheduled at or
	// before now, oldest first.
	DueMergeWatches(now time.Time, limit int) ([]Row, error)
	// UpdateMergeWatch records one poll's answer and, for a rescheduled
	// one, the next check.
	UpdateMergeWatch(mergeEventID, state, detail string, next time.Time, attempts int) error
	// ExpireStaleMergeWatches turns pending/unknown rows whose window has
	// elapsed into expired. Returns how many it turned.
	ExpireStaleMergeWatches(now time.Time, window time.Duration) (int, error)
	// MergeEventsWithoutWatch reads the crash window.
	MergeEventsWithoutWatch() ([]MergeEventRef, error)
	// EnqueueWatchFromEvent gives a watch to a merge event that has none
	// (idempotent on the merge event; a window already elapsed is born
	// expired, never pending).
	EnqueueWatchFromEvent(ref MergeEventRef, window time.Duration) error
}

// ForgeFor is the per-root forge door — the SAME function the merge
// action wires (actionServices(root).Forge), handed over as a method
// value so the watcher and the merge cannot drift onto two clients.
// A nil answer is a non-forge root (or a test seam): the watch reschedules
// as unknown, never green, never red.
type ForgeFor func(root string) forge.Forge

// Filer is the dedupe-and-file door (VG16). An existing subject files
// nothing (the bug already stands); a new subject files exactly one bug.
// The daemon adapts triage + the gate_filing ledger; tests use a fake.
type Filer interface {
	FileGateBug(ctx context.Context, kind, subject, componentRoot string, facts Facts) (bugID int64, filed bool, err error)
}

// Events is the daemon's published-event door (d.Publish): the watch's
// green/red reach every subscriber that way, ids and enums only.
type Events interface {
	Publish(kind, componentRoot string, payload any)
}

// Settings is the live settings read (S44 T10's seam shape): the window
// and the poll interval, resolved per tick so a settings write takes
// effect on the next tick without a restart. An error is the "use the
// package default" answer — the watcher keeps polling.
type Settings interface {
	Window(ctx context.Context) (time.Duration, error)
	Interval(ctx context.Context) (time.Duration, error)
}

// Clock is the time seam; SystemClock is production.
type Clock interface{ Now() time.Time }

// SystemClock is the wall clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// Watcher is the poll. Run starts the startup sweep and the tick loop;
// Tick is one pass (the tests drive it directly, without a loop);
// CheckOne is one row (the tests drive it directly too).
type Watcher struct {
	Store    Store
	Forge    ForgeFor
	Clock    Clock
	Filer    Filer    // nil ⇒ reds are logged, not filed
	Events   Events   // nil ⇒ no events
	Settings Settings // nil ⇒ the package defaults
	Log      func(format string, args ...any)
}

func (w *Watcher) logf(format string, args ...any) {
	if w.Log != nil {
		w.Log(format, args...)
	}
}

func (w *Watcher) clock() Clock {
	if w.Clock != nil {
		return w.Clock
	}
	return SystemClock{}
}

// window resolves the watch window: the live setting, else the package
// default. A zero/negative answer (a parse failure the adapter reported
// as zero) falls back the same way — a watch with no bound is no watch.
func (w *Watcher) window(ctx context.Context) time.Duration {
	if w.Settings != nil {
		d, err := w.Settings.Window(ctx)
		if err == nil && d > 0 {
			return d
		}
		if err != nil {
			w.logf("gatewatch: the watch window could not be read (%v); using the default %s", err, DefaultWatchWindow)
		}
	}
	return DefaultWatchWindow
}

func (w *Watcher) interval(ctx context.Context) time.Duration {
	if w.Settings != nil {
		d, err := w.Settings.Interval(ctx)
		if err == nil && d > 0 {
			return d
		}
		if err != nil {
			w.logf("gatewatch: the watch interval could not be read (%v); using the default %s", err, DefaultWatchInterval)
		}
	}
	return DefaultWatchInterval
}

// Run is the daemon-lifetime loop: the startup sweep first (the crash
// window is repaired BEFORE the first tick, so a merge that landed while
// the daemon was down is watched from the first pass), then one tick
// every interval until ctx ends. A panic in the loop is recovered and
// logged — the watcher is a daemon service, and a dead watcher is a
// silent one, which is the failure this whole slice exists to make loud.
func (w *Watcher) Run(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			w.logf("gatewatch: the watcher loop panicked: %v", rec)
		}
	}()
	if err := w.Sweep(ctx); err != nil {
		w.logf("gatewatch: the startup sweep failed: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.interval(ctx)):
			w.Tick(ctx)
		}
	}
}

// Sweep repairs the crash window and expires the stale: the merge events
// that stand without a watch get one (the roots whose forge reads no
// checks are skipped — a watch the forge cannot answer is a lie), and
// the pending/unknown rows whose window elapsed are expired, never red.
func (w *Watcher) Sweep(ctx context.Context) error {
	if w.Store == nil {
		return fmt.Errorf("gatewatch: the watcher has no store")
	}
	window := w.window(ctx)
	if n, err := w.Store.ExpireStaleMergeWatches(w.clock().Now(), window); err != nil {
		return fmt.Errorf("gatewatch: the expiry sweep failed: %w", err)
	} else if n > 0 {
		w.logf("gatewatch: the sweep expired %d stale watches", n)
	}
	refs, err := w.Store.MergeEventsWithoutWatch()
	if err != nil {
		return fmt.Errorf("gatewatch: the sweep could not read the unwatched merges: %w", err)
	}
	repairs := 0
	for _, ref := range refs {
		if !w.watchable(ref.ComponentRoot) {
			continue
		}
		if err := w.Store.EnqueueWatchFromEvent(ref, window); err != nil {
			w.logf("gatewatch: the sweep could not watch merge %s: %v", ref.ID, err)
			continue
		}
		repairs++
	}
	if repairs > 0 {
		w.logf("gatewatch: the sweep repaired %d missing watches", repairs)
	}
	return nil
}

// watchable is the CheckReader decision: the root's forge exists and
// reads checks. The same decision the record path makes (a root that
// cannot answer is not watched at all — absence is not a red gate).
func (w *Watcher) watchable(root string) bool {
	if w.Forge == nil {
		return false
	}
	f := w.Forge(root)
	if f == nil {
		return false
	}
	_, ok := f.(forge.CheckReader)
	return ok
}

// Tick is one pass: expire the stale, then check every due row. The
// expiry runs BEFORE the due read, so a row whose window elapsed during
// the pass is expired by this tick and not checked by it.
func (w *Watcher) Tick(ctx context.Context) {
	if w.Store == nil {
		return
	}
	now := w.clock().Now()
	window := w.window(ctx)
	if n, err := w.Store.ExpireStaleMergeWatches(now, window); err != nil {
		w.logf("gatewatch: the expiry sweep failed: %v", err)
	} else if n > 0 {
		w.logf("gatewatch: the tick expired %d stale watches", n)
	}
	rows, err := w.Store.DueMergeWatches(now, 50)
	if err != nil {
		w.logf("gatewatch: the due read failed: %v", err)
		return
	}
	for _, row := range rows {
		w.CheckOne(ctx, row)
	}
}

// CheckOne is one row's poll. The mapping is the one rule:
//
//	success ⇒ green   (terminal; the green event; no bug)
//	failure ⇒ red     (terminal; the red event; the filer fires)
//	pending ⇒ unknown (reschedule, backoff — a pending check may still turn)
//	unknown ⇒ unknown (reschedule, backoff — "none-reported" is absence)
//	error   ⇒ unknown (reschedule, backoff — the error KIND rides in the
//	            detail; a transport or auth error is NEVER green or red)
//
// The detail is the aggregate's one-line reason or the error kind — a
// fixed-template word, never forge output.
func (w *Watcher) CheckOne(ctx context.Context, row Row) {
	f := w.forgeFor(row.ComponentRoot)
	state, detail := StateUnknown, "unreadable"
	var fe *forge.ForgeError
	switch {
	case f == nil:
		detail = "no-forge"
	case func() bool { _, ok := f.(forge.CheckReader); return ok }():
		rep, err := f.(forge.CheckReader).ReadChecks(ctx, row.SHA)
		switch {
		case err != nil:
			if errors.As(err, &fe) {
				detail = "unreadable (" + fe.Kind + ")"
			} else {
				detail = "unreadable (error)"
			}
		case rep.State == forge.CheckSuccess:
			state, detail = StateGreen, rep.Reason
		case rep.State == forge.CheckFailure:
			state, detail = StateRed, rep.Reason
		case rep.State == forge.CheckPending:
			detail = rep.Reason // "check … is pending"
		default:
			detail = rep.Reason // "none-reported" | "check … is unknown" | "incomplete — …"
		}
	default:
		detail = "no-checks-api"
	}

	next := w.clock().Now()
	switch state {
	case StateGreen, StateRed:
		// Terminal: no further schedule matters, but the row keeps a
		// far-future next_check_at for the same reason the born-expired
		// one does — the due read must never pick it up again.
		next = w.clock().Now().Add(365 * 24 * time.Hour)
		if err := w.Store.UpdateMergeWatch(row.MergeEventID, state, detail, next, row.Attempts+1); err != nil {
			w.logf("gatewatch: the terminal write for %s failed: %v", row.MergeEventID, err)
			return
		}
		w.finish(ctx, row, state, detail)
	default: // pending / unknown / unreadable
		next = w.clock().Now().Add(backoff(row.Attempts))
		if err := w.Store.UpdateMergeWatch(row.MergeEventID, StateUnknown, detail, next, row.Attempts+1); err != nil {
			w.logf("gatewatch: the reschedule for %s failed: %v", row.MergeEventID, err)
		}
	}
}

// finish publishes the terminal answer: green says the job is done, red
// files (through the dedupe door) and says what failed. The events carry
// ids and enums only.
func (w *Watcher) finish(ctx context.Context, row Row, state, detail string) {
	var bugID int64
	if state == StateRed && w.Filer != nil {
		facts := Facts{
			Kind:          KindMergeWatch,
			ComponentRoot: row.ComponentRoot,
			ComponentID:   row.ComponentID,
			PR:            row.PR,
			LoopSlug:      row.LoopSlug,
			MergeSHA:      row.SHA,
			BaseRef:       row.BaseRef,
			CheckReason:   detail,
		}
		if row.RuleRevision != nil {
			facts.RuleRevision = *row.RuleRevision
		}
		if row.VerifyRunID != nil {
			facts.RunID = *row.VerifyRunID
		}
		// The failing check's name: the aggregate's reason names it
		// ("check X is failure"); the filer filters it into the title.
		facts.CheckName = checkNameFromReason(detail)
		var err error
		bugID, _, err = w.Filer.FileGateBug(ctx, KindMergeWatch, SubjectMergeWatch(row.ComponentRoot, row.SHA), row.ComponentRoot, facts)
		if err != nil {
			w.logf("gatewatch: the red filing for %s failed: %v", row.MergeEventID, err)
		}
	}
	if w.Events == nil {
		return
	}
	kind := EventGreen
	if state == StateRed {
		kind = EventRed
	}
	w.Events.Publish(kind, row.ComponentRoot, map[string]any{
		"watch":         row.MergeEventID,
		"componentRoot": row.ComponentRoot,
		"sha":           row.SHA,
		"pr":            row.PR,
		"reason":        detail,
		"bugId":         bugID,
	})
}

func (w *Watcher) forgeFor(root string) forge.Forge {
	if w.Forge == nil {
		return nil
	}
	return w.Forge(root)
}

// backoff is the reschedule growth: 30 s, doubling per attempt, capped at
// 5 min. attempts is the row's count BEFORE this poll.
func backoff(attempts int) time.Duration {
	d := DefaultWatchInterval
	for i := 0; i < attempts && d < MaxBackoff; i++ {
		d *= 2
	}
	if d > MaxBackoff {
		d = MaxBackoff
	}
	return d
}

// checkNameFromReason pulls the check name out of the aggregate's
// one-line reason ("check X is failure"). The name is forge data: the
// filer filters it before it enters a title or a body.
func checkNameFromReason(reason string) string {
	const prefix, suffix = "check ", " is "
	if len(reason) < len(prefix)+len(suffix) {
		return ""
	}
	if len(reason) >= len(prefix) && reason[:len(prefix)] == prefix {
		rest := reason[len(prefix):]
		if i := lastIndex(rest, suffix); i > 0 {
			return rest[:i]
		}
	}
	return ""
}

func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
