// gatewatch_wiring.go — S57 T7's daemon adapters for the post-merge
// watch: the store seam (the ledger rows), the settings seam (the window
// and interval, read live per tick), and the construction that hands the
// watcher the SAME per-root forge the merge action uses (method value —
// one client factory, no second). Started from StartLoops beside the
// triggers, and the record path enqueues through it after every
// RecordMergeEvent (a separate write: a failed enqueue is logged, never
// a failed merge fact).

package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/gatewatch"
	"github.com/cjlapao/aeon/internal/store"
)

// baseRefUnreported is the watch's honest base: the record path makes no
// forge call (the merge event's base_ref stays NULL), so the watch names
// the component's declared default branch when it has one, and the fixed
// placeholder when it does not — absence named, never fabricated.
const baseRefUnreported = "unreported"

// gatewatchStore adapts *store.Store to the watcher's Store seam. Every
// method is a straight pass-through plus a row mapping; the one place it
// DECIDES is EnqueueWatchFromEvent, where the watch's base ref is
// resolved from the component row (the daemon owns that join).
type gatewatchStore struct{ d *Daemon }

// DueMergeWatches implements gatewatch.Store.
func (a gatewatchStore) DueMergeWatches(now time.Time, limit int) ([]gatewatch.Row, error) {
	rows, err := a.d.store.DueMergeWatches(now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gatewatch.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, gatewatch.Row{
			MergeEventID:  r.MergeEventID,
			ComponentID:   r.ComponentID,
			ComponentRoot: r.ComponentRoot,
			BaseRef:       r.BaseRef,
			SHA:           r.SHA,
			State:         r.State,
			Attempts:      r.Attempts,
			Detail:        r.Detail,
			LoopSlug:      r.LoopSlug,
			PR:            r.PR,
			RuleRevision:  r.RuleRevision,
			VerifyRunID:   r.VerifyRunID,
		})
	}
	return out, nil
}

// UpdateMergeWatch implements gatewatch.Store.
func (a gatewatchStore) UpdateMergeWatch(mergeEventID, state, detail string, next time.Time, attempts int) error {
	return a.d.store.UpdateMergeWatch(mergeEventID, state, detail, next, attempts)
}

// ExpireStaleMergeWatches implements gatewatch.Store.
func (a gatewatchStore) ExpireStaleMergeWatches(now time.Time, window time.Duration) (int, error) {
	return a.d.store.ExpireStaleMergeWatches(now, window)
}

// MergeEventsWithoutWatch implements gatewatch.Store.
func (a gatewatchStore) MergeEventsWithoutWatch() ([]gatewatch.MergeEventRef, error) {
	refs, err := a.d.store.MergeEventsWithoutWatch()
	if err != nil {
		return nil, err
	}
	out := make([]gatewatch.MergeEventRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, gatewatch.MergeEventRef{
			ID:            r.ID,
			ComponentRoot: r.ComponentRoot,
			ComponentID:   r.ComponentID,
			PR:            r.PR,
			MergeSHA:      r.MergeSHA,
			MergedAt:      r.MergedAt,
		})
	}
	return out, nil
}

// EnqueueWatchFromEvent implements gatewatch.Store: the sweep's repair
// write. The base ref is the component's declared default branch, else
// the honest placeholder; the window is the caller's (the live setting).
// Idempotent on the merge event: a race with the record path finds the
// row and changes nothing.
func (a gatewatchStore) EnqueueWatchFromEvent(ref gatewatch.MergeEventRef, window time.Duration) error {
	if ref.ComponentID == "" {
		// A merge that stood without a component id is not watched: the
		// watch names a component, and absence is not a component.
		return nil
	}
	base := a.d.watchBaseRef(ref.ComponentRoot)
	mergedAt, err := time.Parse(time.RFC3339, ref.MergedAt)
	if err != nil {
		return fmt.Errorf("gatewatch wiring: the merge event %s carries an unparseable merged_at %q: %w", ref.ID, ref.MergedAt, err)
	}
	_, err = a.d.store.EnqueueMergeWatch(store.MergeWatchEnqueue{
		MergeEventID: ref.ID,
		ComponentID:  ref.ComponentID,
		BaseRef:      base,
		SHA:          ref.MergeSHA,
		MergedAt:     mergedAt,
		Window:       window,
	})
	return err
}

// gatewatchSettings reads the two gates settings live, the S44 T10 seam's
// shape: a per-call read through the daemon's one shared *config.Store —
// the settings service on the table (CS7: the instance the program holds,
// never a second one) — and a storeless daemon or an unparseable value
// the package default (the watcher logs the fallback and keeps polling:
// a bad value is a loud degraded default, never a stopped watch).
type gatewatchSettings struct{ d *Daemon }

// Window implements gatewatch.Settings.
func (s gatewatchSettings) Window(ctx context.Context) (time.Duration, error) {
	return s.d.gateSettingDuration(ctx, gatewatch.KeyWatchWindow, gatewatch.DefaultWatchWindow)
}

// Interval implements gatewatch.Settings.
func (s gatewatchSettings) Interval(ctx context.Context) (time.Duration, error) {
	return s.d.gateSettingDuration(ctx, gatewatch.KeyWatchInterval, gatewatch.DefaultWatchInterval)
}

// gateSettingDuration is the one live read the watch's settings use,
// shared by the tick (through gatewatchSettings) and the record path (the
// enqueue's window). Resolution is the S44 one — environment → database
// → default — answered by the View's effective value; a storeless daemon
// (no settings service) is the fallback, and a value that does not parse
// is an error the caller maps to the package default and logs.
func (d *Daemon) gateSettingDuration(ctx context.Context, key string, fallback time.Duration) (time.Duration, error) {
	if d.Table == nil || d.Table.Settings == nil {
		return fallback, nil
	}
	view, err := d.Table.Settings.Get(ctx, key)
	if err != nil {
		return fallback, err
	}
	if view.Value == "" {
		return fallback, nil
	}
	p, err := time.ParseDuration(view.Value)
	if err != nil {
		return 0, fmt.Errorf("%s holds %q, which is not a duration: %w", key, view.Value, err)
	}
	return p, nil
}

// initGateWatch builds the watcher on a mounted store. The Forge is the
// forgeClient METHOD VALUE — the same function actionServices wires into
// the merge action's ForgeActions — so the watcher and the merge poll
// through one client factory (TestWatcherUsesTheSameForgeAsTheMergeAction
// pins the identity). The Events door is the daemon itself (d.Publish).
func (d *Daemon) initGateWatch() {
	if d.store == nil {
		return
	}
	d.gateWatcher = &gatewatch.Watcher{
		Store:    gatewatchStore{d},
		Forge:    d.forgeClient,
		Clock:    gatewatch.SystemClock{},
		Filer:    gateFiler{d},
		Events:   d,
		Settings: gatewatchSettings{d},
		Log:      d.logf,
	}
}

// logf is the watcher's Log seam: the daemon's logger, nil-safe.
func (d *Daemon) logf(format string, args ...any) {
	if d.log != nil {
		d.log.Infof(format, args...)
	}
}

// enqueueMergeWatchForEvent is the record path's half of the watch: a
// SEPARATE write after RecordMergeEvent succeeded. The CheckReader
// decision is the sweep's own (a root whose forge reads no checks is not
// watched — absence is not a red gate); the component-id gate is the
// store's (EnqueueWatchFromEvent skips the unnamed). A failure here is
// logged and returned to the caller's log, never a failed merge fact —
// the startup sweep repairs the row the crash (or this error) left out.
func (d *Daemon) enqueueMergeWatchForEvent(ev store.MergeEvent) {
	if d.gateWatcher == nil || d.store == nil {
		return
	}
	root := ev.ComponentRoot
	if f := d.forgeClient(root); f == nil {
		return
	} else if _, ok := f.(forge.CheckReader); !ok {
		return
	}
	mergedAt, err := time.Parse(time.RFC3339, ev.MergedAt)
	if err != nil {
		d.logf("gatewatch: the merge event %s carries an unparseable merged_at %q: %v", ev.ID, ev.MergedAt, err)
		return
	}
	window := gatewatch.DefaultWatchWindow
	if w, err := d.gateSettingDuration(context.Background(), gatewatch.KeyWatchWindow, gatewatch.DefaultWatchWindow); err == nil && w > 0 {
		window = w
	}
	componentID := derefOr(ev.ComponentID, "")
	if componentID == "" {
		// A merge that stood without a component id is not watched (the
		// sweep makes the same decision): the watch names a component, and
		// absence is not a component.
		return
	}
	_, err = d.store.EnqueueMergeWatch(store.MergeWatchEnqueue{
		MergeEventID: ev.ID,
		ComponentID:  componentID,
		BaseRef:      d.watchBaseRef(root),
		SHA:          ev.MergeSHA,
		MergedAt:     mergedAt,
		Window:       window,
	})
	if err != nil {
		d.logf("gatewatch: the watch enqueue for merge %s failed (the startup sweep repairs it): %v", ev.ID, err)
	}
}

// watchBaseRef is the watch's base ref, resolved once for both write
// sites (the record path and the sweep): the component's declared default
// branch when it has one, the honest placeholder otherwise. An
// unreadable component row is the placeholder — a watch that cannot read
// its base names its absence rather than guessing a branch.
func (d *Daemon) watchBaseRef(componentRoot string) string {
	base := baseRefUnreported
	if comp, err := d.store.ComponentForRoot(componentRoot); err == nil && comp.DefaultBranch != nil {
		base = *comp.DefaultBranch
	}
	return base
}

// derefOr is the nil-safe dereference the record path needs (a merge
// event's component id is nullable by design).
func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
