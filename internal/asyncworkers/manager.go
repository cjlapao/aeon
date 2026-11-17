// Package asyncworkers runs long-lived daemon work off the caller's
// clock, with the database as its memory.
//
// # Why this package exists
//
// Some Aeon functions take minutes — the code-graph build on a complex
// tree being the first — and a socket handler that runs one inline holds
// the caller's connection (and the caller's whole UI) hostage for the
// duration, while an in-memory fire-and-forget goroutine (graphBusy,
// reindexTracker) keeps its state in a place a restart wipes. The manager
// splits those two roles cleanly: a worker is a registered Go function the
// daemon runs on its own life, AND a row in async_worker that survives the
// daemon, so a client reconnecting days later can still answer "is
// anything of this type running for this root, and what was the last thing
// it said".
//
// # The contract each side signs
//
// The registering service (wired at daemon construction, before any
// client): a Runner, a per-type timeout, and nothing else. The Runner
// honors its context — cancellation is cooperative, and the manager bounds
// it by timeout regardless; it reports through Reporter, which is
// throttle-safe; it returns a JSON result or an error, never a prose
// summary it hopes someone renders.
//
// The manager: one goroutine per worker, a panic containment net (a
// crashing Runner must not take the daemon — the same rule the graph write
// hook already stated), a durable single flight (the flight index, not a
// map), a bounded concurrency (MaxConcurrent, queued FIFO), and terminal
// writes that only ever land on live rows — so the daemon's own
// interrupting sweep at shutdown/restart can be raced by a winding-down
// goroutine and the honest answer (interrupted) always wins.
//
// # Who may start work
//
// Only code the daemon wired: Start names a registered type, and the
// registry's closed set is the door. The socket carries reads and cancel
// only — a generic start door would let a caller name a function it cannot
// supply arguments for (the V21 discipline: registration puts a door; the
// owning service's own method walks through it, e.g. graph.build).
package asyncworkers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// The event kinds the manager publishes through the daemon's event stream.
// One per state change; the payload is the full View either way (the row
// is the source of truth, the event is the paint trigger — S14's split).
const (
	KindStarted     = "asyncworker.started"
	KindProgress    = "asyncworker.progress"
	KindFinished    = "asyncworker.finished"
	KindFailed      = "asyncworker.failed"
	KindCancelled   = "asyncworker.cancelled"
	KindInterrupted = "asyncworker.interrupted"
)

// defaultTimeout bounds a worker whose registering service asked for no
// bound. "Background" is not "unbounded" (the reindex tracker's own
// conclusion, adopted): 30 minutes is the same figure reindexTimeout uses.
const defaultTimeout = 30 * time.Minute

// progressThrottle is the smallest gap between two progress writes for one
// worker. A Runner that reports every file of 50,000 gets the loop it
// asked for; the database gets one write a second.
const progressThrottle = time.Second

// shutdownGrace is how long Shutdown waits for winding-down goroutines to
// write their own terminal state before sweeping what is left to
// interrupted. Long enough for a Runner that just noticed its context
// died; short enough that shutdown stays prompt. A var (not const) is the
// test seam the stubborn-runner test shortens; production never writes it.
var shutdownGrace = 5 * time.Second

// Job is what a Runner is invoked with — the identity of the work, not
// its arguments-by-name. Everything dynamic (which file, which component)
// rides Metadata, which is why the metadata column is a KVP and not a
// shape; only the worker type itself is a closed set.
type Job struct {
	WorkerID      string
	Type          string
	ComponentRoot string
	Metadata      map[string]string
}

// Reporter is the Runner's way back. Progress takes a fraction 0..1 and a
// short human-readable note; it is safe from any goroutine, and it is
// throttled (a Runner that reports per file does not pay per file).
type Reporter interface {
	Progress(frac float64, note string)
}

// Runner is one unit of background work. Contract: honor ctx (a
// cancellation that is ignored is bounded by the timeout anyway); return a
// JSON result on success — the row's result column is shown to whoever
// asks; return an error for a real failure. A panic is contained and
// recorded as failed, never rethrown.
type Runner func(ctx context.Context, job Job, rep Reporter) (json.RawMessage, error)

// WorkerOpts tunes one registered type. Zero Timeout means defaultTimeout,
// never "forever".
type WorkerOpts struct {
	Timeout time.Duration
}

// Options tunes the manager. Publish is the daemon's own publish func
// (event-log append + subscriber fan-out); nil keeps the manager working
// and simply silent (tests build one to watch the rows).
type Options struct {
	MaxConcurrent int
	Publish       func(kind, componentRoot string, payload any)
}

type registered struct {
	runner  Runner
	timeout time.Duration
}

// liveWorker is the manager's bookkeeping for one in-flight worker. ctx
// and done are set before launch and read from the worker's goroutine
// (context is safe for concurrent use; done is a channel); the progress
// throttle fields are guarded by Manager.mu.
type liveWorker struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	lastProgress time.Time
	progressed   bool
}

// Manager owns the worker lifecycle over the store's ledger. One is built
// per daemon (P34's single-value discipline: the socket reads, the
// registering services' Start calls and the recovery hook all hold the
// same pointer).
type Manager struct {
	st            *store.Store
	now           func() time.Time
	pub           func(kind, componentRoot string, payload any)
	maxConcurrent int

	mu         sync.Mutex
	registered map[string]registered
	live       map[string]*liveWorker // worker id → in-flight bookkeeping
	pending    []string               // queued worker ids, FIFO
	running    int                    // reserved slots (launch may still skip)
	closed     bool
}

// New builds a manager over an opened store. MaxConcurrent <= 0 means 2:
// a machine parsing trees should not be parsing five trees at once.
func New(st *store.Store, opts Options) *Manager {
	max := opts.MaxConcurrent
	if max <= 0 {
		max = 2
	}
	return &Manager{
		st:            st,
		now:           time.Now,
		pub:           opts.Publish,
		maxConcurrent: max,
		registered:    map[string]registered{},
		live:          map[string]*liveWorker{},
	}
}

// Register names one worker type. It is init-time only (called during
// daemon construction, before Serve opens the socket); a re-registration
// is a wiring bug, refused loudly rather than silently shadowed.
func (m *Manager) Register(workerType string, r Runner, o WorkerOpts) error {
	if workerType == "" || r == nil {
		return fmt.Errorf("asyncworkers: Register(%q) needs a non-empty type and a non-nil Runner", workerType)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.registered[workerType]; dup {
		return fmt.Errorf("asyncworkers: worker type %q is already registered — one door per type (P34)", workerType)
	}
	m.registered[workerType] = registered{runner: r, timeout: timeout}
	return nil
}

// Types lists the registered worker types (sorted, for refusal messages
// and the bench page's "what can run here").
func (m *Manager) Types() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.registered))
	for t := range m.registered {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Start queues one worker of a registered type and returns as soon as the
// row exists — fire and forget is the interface. The bool says whether
// THIS call started it: false means a live worker already holds the flight
// for (type, dedupeKey) and the row returned is that one (the same
// reindexTracker.begin semantics, made durable by the flight index).
//
// Metadata may be nil; it is stored as {} — the honest empty object, not a
// NULL nobody decoded.
func (m *Manager) Start(workerType, componentRoot, dedupeKey string, metadata map[string]string) (*store.AsyncWorkerRow, bool, error) {
	if dedupeKey == "" {
		return nil, false, errors.New("asyncworkers: Start needs a dedupeKey — single flight is keyed by fact, never defaulted")
	}
	meta, err := marshalMetadata(metadata)
	if err != nil {
		return nil, false, fmt.Errorf("asyncworkers: start %s: metadata refused: %w", workerType, err)
	}
	if err := m.checkStartable(workerType); err != nil {
		return nil, false, err
	}
	return m.takeFlight(workerType, dedupeKey, func() (*store.AsyncWorkerRow, error) {
		return m.st.InsertAsyncWorker(store.NewAsyncWorker{
			WorkerType: workerType, ComponentRoot: componentRoot,
			DedupeKey: dedupeKey, Metadata: meta, QueuedAt: m.stamp(),
		})
	})
}

// Audit is the S54 SP3 audit half an audited start carries — the manager's
// own shape, not store.PlatformAudit (this package owns the ledger and
// maps the two HERE, so the protocol seam never reaches past the ledger
// into the store's vocabulary). OutcomeRef and OccurredAt are NOT fields:
// the worker id the ref names is minted by the insert attempt the audit
// rides, and the stamp is the queue time of that same attempt — a caller
// cannot write an audit that names a worker it did not start or time-stamp
// a start it did not queue.
type Audit struct {
	Op      string // reindex | graph_build — the two ledger ops (SP3's other two ride the graph service)
	Subject string
	Summary string
	Actor   AuditActor
}

// AuditActor is the audit's author pair: user | cli with a named id.
type AuditActor struct {
	Kind string
	ID   string
}

// StarterView is the S54 SP3 answer to "already running — started by WHO,
// when": the audit row behind a worker id, as the ledger package renders
// it. A nil answer with a nil error is data, not a fault: a write-hook
// pass has no audit row (SP4 — the hook is the daemon's act, not a
// person's), and the door then answers without a name rather than
// inventing one.
type StarterView struct {
	Kind string
	ID   string
	At   string
}

// Starter looks the starting person up from the platform audit by the
// worker the flight names (outcome_ref = worker_id, the S54 T1 twin of
// the ledger read).
func (m *Manager) Starter(ctx context.Context, workerID string) (*StarterView, error) {
	row, err := m.st.PlatformChangeStarter(ctx, workerID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &StarterView{Kind: row.ActorKind, ID: row.ActorID, At: row.OccurredAt}, nil
}

// StartAudited takes the flight AND appends its platform_change audit row
// in the same transaction as the queued ledger row (SP3): a flight
// collision rolls the audit back with the refused insert — "already
// running" writes nothing by construction. The guards and the started
// flag are Start's exactly; only the insert is audited.
func (m *Manager) StartAudited(workerType, componentRoot, dedupeKey string, metadata map[string]string, audit Audit) (*store.AsyncWorkerRow, bool, error) {
	if dedupeKey == "" {
		return nil, false, errors.New("asyncworkers: StartAudited needs a dedupeKey — single flight is keyed by fact, never defaulted")
	}
	meta, err := marshalMetadata(metadata)
	if err != nil {
		return nil, false, fmt.Errorf("asyncworkers: start %s: metadata refused: %w", workerType, err)
	}
	if err := m.checkStartable(workerType); err != nil {
		return nil, false, err
	}
	return m.takeFlight(workerType, dedupeKey, func() (*store.AsyncWorkerRow, error) {
		at := m.stamp()
		return m.st.InsertAsyncWorkerAudited(store.NewAsyncWorker{
			WorkerType: workerType, ComponentRoot: componentRoot,
			DedupeKey: dedupeKey, Metadata: meta, QueuedAt: at,
		}, store.PlatformAudit{
			Op: audit.Op, Subject: audit.Subject, Summary: audit.Summary,
			Actor: store.PlatformActor{Kind: audit.Actor.Kind, ID: audit.Actor.ID},
		}, at)
	})
}

// checkStartable is Start's guard pair — shutdown and the registered set —
// shared verbatim so the audited door refuses in the same words.
func (m *Manager) checkStartable(workerType string) error {
	m.mu.Lock()
	_, ok := m.registered[workerType]
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return fmt.Errorf("asyncworkers: start %s refused — the daemon is shutting down", workerType)
	}
	if !ok {
		return fmt.Errorf("asyncworkers: worker type %q is not registered — the registered set is %v", workerType, m.Types())
	}
	return nil
}

// takeFlight is Start's body after the guards: take the flight or report
// its holder, then arm and dispatch. The insert closure is the only
// difference between the plain and the audited door.
func (m *Manager) takeFlight(workerType, dedupeKey string, newRow func() (*store.AsyncWorkerRow, error)) (*store.AsyncWorkerRow, bool, error) {
	row, started, err := m.insertOrLive(workerType, dedupeKey, newRow)
	if err != nil {
		return nil, false, err
	}
	if !started {
		return row, false, nil
	}

	m.mu.Lock()
	m.pending = append(m.pending, row.WorkerID)
	m.mu.Unlock()
	m.dispatch()
	return row, true, nil
}

// insertOrLive takes the flight or reports its holder. The retry covers
// the race where the holder went terminal between the refused insert and
// the re-read.
func (m *Manager) insertOrLive(workerType, dedupeKey string, newRow func() (*store.AsyncWorkerRow, error)) (*store.AsyncWorkerRow, bool, error) {
	row, err := newRow()
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, store.ErrAsyncWorkerInFlight) {
		return nil, false, err
	}
	live, rerr := m.liveFor(workerType, dedupeKey)
	if rerr != nil {
		return nil, false, rerr
	}
	if live != nil {
		return live, false, nil
	}
	row, err = newRow()
	if errors.Is(err, store.ErrAsyncWorkerInFlight) {
		live, rerr = m.liveFor(workerType, dedupeKey)
		if rerr != nil {
			return nil, false, rerr
		}
		if live == nil {
			return nil, false, fmt.Errorf("asyncworkers: start %s: the flight keeps being held by a row the re-read cannot find", workerType)
		}
		return live, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return row, true, nil
}

// liveFor re-reads the worker currently holding the flight for a key,
// newest first. An empty result is data, not an error: absence is an
// answer.
func (m *Manager) liveFor(workerType, dedupeKey string) (*store.AsyncWorkerRow, error) {
	rows, err := m.st.ListAsyncWorkers(store.AsyncWorkerFilter{
		Type: workerType, DedupeKey: dedupeKey,
		States: []string{"queued", "running"}, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// Get reads one worker by id. sql.ErrNoRows passes through — the protocol
// layer turns it into the refusal that names the id.
func (m *Manager) Get(workerID string) (*store.AsyncWorkerRow, error) {
	return m.st.GetAsyncWorker(workerID)
}

// GetView is Get answered with the wire shape — the socket's door.
func (m *Manager) GetView(workerID string) (*View, error) {
	row, err := m.st.GetAsyncWorker(workerID)
	if err != nil {
		return nil, err
	}
	return ViewOf(row), nil
}

// ListViews is the page-mount door answered in wire shapes: the app's
// "anything of this type running for this root" is one call, and the
// registered set rides along so a screen can offer the honest menu of
// what CAN run rather than a remembered list.
func (m *Manager) ListViews(workerType, componentRoot string, states []string, limit int) (*ListResult, error) {
	rows, err := m.st.ListAsyncWorkers(store.AsyncWorkerFilter{
		Type: workerType, ComponentRoot: componentRoot, States: states, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return &ListResult{Workers: ViewsOf(rows), Types: m.Types()}, nil
}

// StartView is Start answered in the wire shape. It exists for the OWNING
// service's own method (graph.build), not for a generic socket door — the
// asyncworkers family registers list/get/cancel only, and a test says so.
func (m *Manager) StartView(workerType, componentRoot, dedupeKey string, metadata map[string]string) (*View, bool, error) {
	row, started, err := m.Start(workerType, componentRoot, dedupeKey, metadata)
	if err != nil {
		return nil, false, err
	}
	return ViewOf(row), started, nil
}

// StartViewAudited is StartAudited answered in the wire shape — S54 T1's
// audited door for retrieval.reindex and graph.build. Same rule as
// StartView: it exists for the OWNING service's own method, never a
// generic start.
func (m *Manager) StartViewAudited(workerType, componentRoot, dedupeKey string, metadata map[string]string, audit Audit) (*View, bool, error) {
	row, started, err := m.StartAudited(workerType, componentRoot, dedupeKey, metadata, audit)
	if err != nil {
		return nil, false, err
	}
	return ViewOf(row), started, nil
}

// ListResult is the asyncworkers.list answer: the page of workers plus
// the registry's closed set, so one read gives a screen both the running
// truth and the honest menu.
type ListResult struct {
	Workers []View   `json:"workers"`
	Types   []string `json:"types"`
}

// List reads the ledger narrowed by the filter — the page-mount door
// ("anything of this type running for this root").
func (m *Manager) List(f store.AsyncWorkerFilter) ([]store.AsyncWorkerRow, error) {
	return m.st.ListAsyncWorkers(f)
}

// Cancel asks for a worker to stop. Queued workers die before they start
// (the manager writes the terminal row itself — there is no goroutine to
// write it). Running workers get cancel_requested + a context cancellation
// and write their own terminal state from their goroutine, which keeps
// "who wrote the ending" one place. A terminal worker refuses with the
// state it already has, and an unknown id is sql.ErrNoRows.
func (m *Manager) Cancel(workerID string) error {
	prev, err := m.st.RequestCancelAsyncWorker(workerID)
	if err != nil {
		return err
	}
	switch prev {
	case "queued":
		ok, ferr := m.st.FinishAsyncWorker(workerID, "cancelled", "", "cancelled while queued", m.stamp())
		if ferr != nil {
			return ferr
		}
		if ok {
			m.publishFromRow(workerID, KindCancelled)
		}
		return nil
	case "running":
		m.mu.Lock()
		lw := m.live[workerID]
		m.mu.Unlock()
		if lw != nil {
			lw.cancel() // the goroutine owns the terminal write and the event
			return nil
		}
		// The row is running with no goroutine of ours: this manager did
		// not launch it (a stale row from before a crash that Recovery
		// has not swept yet). Say so rather than pretend to cancel it.
		return fmt.Errorf("asyncworkers: cancel %s: the row says running but this daemon holds no goroutine for it — recovery will speak for it at the next start", workerID)
	default: // "" — terminal already; the state is the answer
		row, rerr := m.st.GetAsyncWorker(workerID)
		if rerr != nil {
			return rerr
		}
		return fmt.Errorf("asyncworkers: cancel %s refused — the worker is %s, there is nothing to cancel", workerID, row.State)
	}
}

// Recover is the startup sweep: every queued-or-running row from a
// previous daemon becomes interrupted with a spoken reason. Called from
// serve after the loop recovery, before clients arrive. The count is
// returned for the start log; the interrupted Views are published so the
// durable event log carries the fact to later subscribers.
func (m *Manager) Recover(ctx context.Context) (int, error) {
	// Read the live rows first: after the sweep they are terminal, and the
	// Views worth publishing are exactly the ones the sweep spoke for.
	live, err := m.st.ListAsyncWorkers(store.AsyncWorkerFilter{States: []string{"queued", "running"}, Limit: 1000})
	if err != nil {
		return 0, err
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	n, err := m.st.MarkAsyncWorkersInterrupted(m.stamp())
	if err != nil {
		return 0, err
	}
	for i := range live {
		row := live[i]
		v := ViewOf(&row)
		if v == nil {
			continue
		}
		v.State = "interrupted"
		v.Error = "daemon stopped before the worker finished"
		v.FinishedAt = m.stamp()
		m.publishView(row.ComponentRoot, KindInterrupted, v)
	}
	return n, nil
}

// Shutdown stops accepting starts, cancels every live worker and waits
// shutdownGrace for its goroutine to write its own terminal state, then
// sweeps whatever is left to interrupted. Called from daemon shutdown
// before the socket closes.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	var dones []chan struct{}
	for _, lw := range m.live {
		lw.cancel()
		dones = append(dones, lw.done)
	}
	m.mu.Unlock()

	deadline := time.After(shutdownGrace)
	for _, d := range dones {
		select {
		case <-d:
		case <-deadline:
			_, _ = m.st.MarkAsyncWorkersInterrupted(m.stamp())
			return nil
		case <-ctx.Done():
			_, _ = m.st.MarkAsyncWorkersInterrupted(m.stamp())
			return ctx.Err()
		}
	}
	return nil
}

// dispatch launches queued workers while slots are free. A slot is
// RESERVED under the lock, then claimed outside it by the store's guarded
// queued→running flip; a claim that fails (cancelled while queued,
// unreadable) releases the reservation, which may open the door for the
// rest of the queue.
func (m *Manager) dispatch() {
	m.mu.Lock()
	var toLaunch []string
	for m.running < m.maxConcurrent && len(m.pending) > 0 {
		id := m.pending[0]
		m.pending = m.pending[1:]
		toLaunch = append(toLaunch, id)
		m.running++
	}
	m.mu.Unlock()

	for _, id := range toLaunch {
		row, err := m.st.GetAsyncWorker(id)
		if err != nil || row.State != "queued" {
			m.releaseSlot()
			continue
		}
		ok, err := m.st.StartAsyncWorker(id, m.stamp())
		if err != nil || !ok {
			m.releaseSlot()
			continue
		}
		fresh, ferr := m.st.GetAsyncWorker(id)
		if ferr != nil {
			m.releaseSlot()
			continue
		}
		m.launch(fresh)
	}
}

func (m *Manager) releaseSlot() {
	m.mu.Lock()
	m.running--
	m.mu.Unlock()
	m.dispatch()
}

// launch starts one claimed worker (the queued→running flip has already
// taken) and publishes its start.
func (m *Manager) launch(row *store.AsyncWorkerRow) {
	m.mu.Lock()
	reg, ok := m.registered[row.WorkerType]
	if !ok {
		// The type went missing between claim and launch — impossible
		// while Register is init-time; guard so it cannot spawn nothing
		// silently.
		m.running--
		m.mu.Unlock()
		_, _ = m.st.FinishAsyncWorker(row.WorkerID, "failed", "", "worker type vanished from the registry before launch", m.stamp())
		return
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if reg.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, reg.timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	lw := &liveWorker{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	m.live[row.WorkerID] = lw
	m.mu.Unlock()

	m.publishView(row.ComponentRoot, KindStarted, ViewOf(row))
	go m.run(row, reg, lw)
}

// run is one worker's goroutine: invoke, classify the ending, write the
// terminal row (guarded — the daemon's sweep may have gotten there first),
// publish the event, release the slot.
func (m *Manager) run(row *store.AsyncWorkerRow, reg registered, lw *liveWorker) {
	defer func() {
		if p := recover(); p != nil {
			// Containment, the graph hook's rule: a Runner that panics
			// takes only itself; the daemon and its ledger stay sound.
			m.finish(row, "failed", "", fmt.Sprintf("panic: %v", p))
		}
		close(lw.done)
		lw.cancel()
	}()

	job := Job{WorkerID: row.WorkerID, Type: row.WorkerType, ComponentRoot: row.ComponentRoot}
	if md, err := unmarshalMetadata(row.Metadata); err == nil {
		job.Metadata = md
	}
	rep := &reporter{m: m, row: row, lw: lw}

	result, err := reg.runner(lw.ctx, job, rep)

	switch {
	case errors.Is(lw.ctx.Err(), context.DeadlineExceeded):
		m.finish(row, "failed", "", fmt.Sprintf("timed out after %s", reg.timeout))
	case errors.Is(lw.ctx.Err(), context.Canceled):
		// Cancelled by request (user or shutdown). Even a Runner that
		// returned an error was told to stop first: the ending the user
		// asked for is the honest one.
		m.finish(row, "cancelled", "", "cancelled")
	case err != nil:
		m.finish(row, "failed", "", err.Error())
	default:
		m.finish(row, "finished", rawJSON(result), "")
	}
}

// finish writes the guarded terminal row and publishes the View only if
// THIS write landed — if the sweep got there first, the row's truth (and
// its already-published event) stands.
func (m *Manager) finish(row *store.AsyncWorkerRow, state, result, errMsg string) {
	ok, err := m.st.FinishAsyncWorker(row.WorkerID, state, result, errMsg, m.stamp())
	m.mu.Lock()
	delete(m.live, row.WorkerID)
	m.running--
	m.mu.Unlock()
	if err != nil || !ok {
		m.dispatch()
		return // a late write onto an interrupted row: silent by design, the row already told the truth
	}
	kind, known := map[string]string{
		"finished": KindFinished, "failed": KindFailed, "cancelled": KindCancelled,
	}[state]
	if known {
		if fresh, ferr := m.st.GetAsyncWorker(row.WorkerID); ferr == nil {
			m.publishView(row.ComponentRoot, kind, ViewOf(fresh))
		}
	}
	m.dispatch()
}

func (m *Manager) publishFromRow(workerID, kind string) {
	row, err := m.st.GetAsyncWorker(workerID)
	if err != nil {
		return
	}
	m.publishView(row.ComponentRoot, kind, ViewOf(row))
}

func (m *Manager) publishView(componentRoot, kind string, v *View) {
	if m.pub == nil || v == nil {
		return
	}
	m.pub(kind, componentRoot, v)
}

func (m *Manager) stamp() string { return m.now().UTC().Format("2006-01-02T15:04:05.000Z") }

// reporter is the Runner's throttled voice.
type reporter struct {
	m   *Manager
	row *store.AsyncWorkerRow
	lw  *liveWorker
}

func (r *reporter) Progress(frac float64, note string) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	now := r.m.now()
	r.m.mu.Lock()
	if r.lw.progressed && now.Sub(r.lw.lastProgress) < progressThrottle {
		r.m.mu.Unlock()
		return
	}
	r.lw.progressed = true
	r.lw.lastProgress = now
	r.m.mu.Unlock()

	ok, err := r.m.st.ProgressAsyncWorker(r.row.WorkerID, frac, note)
	if err != nil || !ok {
		return // went terminal under us; a stale tick is not news
	}
	if fresh, ferr := r.m.st.GetAsyncWorker(r.row.WorkerID); ferr == nil {
		r.m.publishView(r.row.ComponentRoot, KindProgress, ViewOf(fresh))
	}
}

func marshalMetadata(md map[string]string) (string, error) {
	if len(md) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(md)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalMetadata(s string) (map[string]string, error) {
	if s == "" || s == "{}" {
		return nil, nil
	}
	var out map[string]string
	err := json.Unmarshal([]byte(s), &out)
	return out, err
}

func rawJSON(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	if !json.Valid(r) {
		b, _ := json.Marshal(string(r))
		return string(b)
	}
	return string(r)
}
