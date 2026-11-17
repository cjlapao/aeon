package continuous

import (
	"context"
	"sync"
	"time"
)

// Worker is CN19's tick made code: Reader → Plan → apply, in that order,
// one decision at a time. It owns no SQL, no clock read of its own, and
// no policy — Plan is the policy, the Starter is the world. The file sits
// in this package beside the pure core deliberately: the purity tests
// read these imports too, and a worker that could not stay clean is the
// test's failure, not a reviewer's hope.
type Worker struct {
	Reader   Reader
	Starter  Starter
	Clock    Clock
	Settings SettingsReader
	Log      Logger

	mu   sync.Mutex
	kick chan string // coalesced: buffered signals, roots deduped live
	run  bool        // Run owns the loop; Kick only wakes it
	stop chan struct{}
}

// NewWorker wires the tick. A nil Clock reads SystemClock; a nil Log is
// silence. Everything else must be there — a worker without a reader or
// a starter has nothing honest to do with a tick.
func NewWorker(r Reader, st Starter, c Clock, sr SettingsReader, log Logger) *Worker {
	if c == nil {
		c = SystemClock{}
	}
	w := &Worker{Reader: r, Starter: st, Clock: c, Settings: sr, Log: log, kick: make(chan string, 256)}
	return w
}

// Kick asks for a tick for root as soon as the worker is listening
// (CN19's kick half — the reactor's "something finished here" news). It
// NEVER blocks: the calling event handler must not queue behind a slow
// snapshot, and a second Kick for a root already waiting adds nothing.
// A worker not running drops the kick — recovery's Reconcile, not a
// signal into the void, is what restarts anything.
func (w *Worker) Kick(root string) {
	select {
	case w.kick <- root:
	default: // full: the tick will read the world fresh anyway
	}
}

// Tick runs exactly one: snapshot at `at`, plan, apply each decision in
// the order Plan emitted it (decision row → claim → start, CN14). The
// order IS the protocol — applying a later start before an earlier
// record would let the ledger and the world disagree about who spoke
// first. Stopping mid-apply on ctx is legitimate and recoverable: the
// claim protocol is one-attempt, so Reconcile finishes what died here.
func (w *Worker) Tick(ctx context.Context, at time.Time) error {
	s, err := w.Reader.Snapshot(ctx, at)
	if err != nil {
		return err
	}
	for _, d := range Plan(s) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		id, err := w.Starter.Record(ctx, s, d)
		if err != nil {
			w.logf("continuous: the decision for %s %s/%s could not be recorded: %v", d.Root, d.Subject.Kind, d.Subject.ID, err)
			continue // an unrecordable decision gets no start — a start no ledger names is a start nobody can reconcile
		}
		if d.Verdict != VerdictStart {
			continue
		}
		if _, err := w.Starter.Start(ctx, d, s, id); err != nil {
			w.logf("continuous: the start of %s %s/%s ended honestly: %v", d.Root, d.Subject.Kind, d.Subject.ID, err)
		}
	}
	return nil
}

// Run is serve.go's goroutine body (CN19): a ticker at the declared
// cadence and the coalesced kick channel, each firing one Tick at the
// Clock's now. Errors are logged, never fatal — a worker that cannot
// snapshot is a daemon with a bad moment, not a dead daemon (the same
// bargain the automation dispatcher keeps). It returns when ctx ends.
func (w *Worker) Run(ctx context.Context) error {
	every := 15 * time.Second
	if w.Settings != nil {
		if s, err := w.Settings.Load(ctx); err == nil {
			every = s.TickEvery()
		} else {
			w.logf("continuous: settings unread, ticking on the default cadence: %v", err)
		}
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	w.logf("continuous: the scheduler ticks every %s, kicks coalesce", every)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		case <-w.kick:
			w.drainKicks()
		}
		if err := w.Tick(ctx, w.Clock.Now()); err != nil && ctx.Err() == nil {
			w.logf("continuous: a tick ended honestly: %v", err)
		}
		// S27 T8 (CN18): the cadence is re-read after every lap. A person
		// who writes scheduler.tickSeconds mid-run sees the change on the
		// NEXT tick — the ticker is reset, not restarted, and a settings
		// read that fails leaves the current period standing (the same
		// bargain Tick keeps: a bad moment is logged, never fatal).
		if w.Settings != nil {
			if s, err := w.Settings.Load(ctx); err == nil {
				if next := s.TickEvery(); next != every {
					tick.Reset(next)
					every = next
				}
			} else {
				w.logf("continuous: settings unread mid-run, keeping the cadence at %s: %v", every, err)
			}
		}
	}
}

// Reconcile delegates the crash pass to the Starter side (CN14): the
// open claims are the store's to read and the adapter's to finish. The
// Worker only owns the ordering sentence — serve.go runs this once,
// after StartLoops, before Run.
func (w *Worker) Reconcile(ctx context.Context) error {
	r, ok := w.Starter.(Reconciler)
	if !ok {
		return nil // a starter with no recovery half is a seam test double; production always implements it
	}
	return r.Reconcile(ctx)
}

func (w *Worker) drainKicks() {
	for {
		select {
		case <-w.kick:
		default:
			return
		}
	}
}

func (w *Worker) logf(format string, args ...any) {
	if w.Log != nil {
		w.Log.Infof(format, args...)
	}
}
