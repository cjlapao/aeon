package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// S64 T1 — the async-worker ledger: rows in, rows out. The MEANING (which
// worker types exist, how a runner is invoked, what the state transitions
// mean) lives in internal/asyncworkers; this file is the handle's side, the
// way store/graph.go is for internal/graph. The state vocabulary closes at
// the database (migration 20261030175511's CHECK), so no Go constant is
// load-bearing here — one table, one CHECK, one truth.
//
// Two invariants this file is responsible for and the manager may rely on
// rather than re-check:
//
//   - Single flight is durable. The partial unique index on
//     (worker_type, dedupe_key) WHERE state IN ('queued','running') is what
//     "one graph build per root at a time" MEANS — a map wiped by a crash
//     was the old guarantee; this is the new one. InsertAsyncWorker
//     recognises the violation and reports it typed (ErrAsyncWorkerInFlight)
//     so the caller re-reads the live row instead of starting a second pass.
//   - Terminal is terminal. Every finishing write (FinishAsyncWorker) is
//     UPDATE-guarded on a non-terminal state, so a goroutine that wakes
//     after the daemon's interrupting pass cannot overwrite `interrupted`
//     with `finished` — last-writer-wins is impossible by construction, and
//     the guard REPORTS whether the flip took rather than silently losing.

// AsyncWorkerRow mirrors async_worker's columns exactly. The row type
// carries no behaviour on purpose — behaviour here would be a second place
// that knows what a worker is; internal/asyncworkers owns the meaning.
type AsyncWorkerRow struct {
	ID              int64
	WorkerID        string
	WorkerType      string
	State           string
	CancelRequested bool
	ComponentRoot   string // "" when NULL — not every worker is scoped to a directory
	DedupeKey       string
	Metadata        string // JSON KVP as stored; decoded by the readers that show it
	Progress        *float64
	ProgressNote    string
	Result          string
	Error           string
	QueuedAt        string
	StartedAt       string
	FinishedAt      string
}

// NewAsyncWorker is the InsertAsyncWorker argument: the fields known at
// start time. State, timings and progress are the store's and the
// manager's writes, not the caller's — a caller cannot insert a row that
// claims to have finished.
type NewAsyncWorker struct {
	WorkerID      string
	WorkerType    string
	ComponentRoot string
	DedupeKey     string
	Metadata      string // JSON object; "" becomes "{}"
	QueuedAt      string // RFC3339, store.Now() at the call site
}

// AsyncWorkerFilter is ListAsyncWorkers' narrowing. Zero values mean
// "any" for the optional dimensions; Limit <= 0 means the default of 50 so
// a caller that forgot to page still gets an answer, not the table.
type AsyncWorkerFilter struct {
	Type          string
	DedupeKey     string
	ComponentRoot string
	States        []string
	Limit         int
}

// ErrAsyncWorkerInFlight reports the flight index refusing a second
// queued-or-running worker for the same (type, dedupeKey): there is one,
// read it. A typed sentinel because "another is running" is a branch, not
// an infrastructure failure (DB19's discipline).
var ErrAsyncWorkerInFlight = errors.New("async worker already queued or running for this key")

// NewWorkerID mints the external handle: "aw_" plus 16 random bytes.
// Minted here because the store owns row identity; a caller may pass its
// own, and the UNIQUE index arbitrates.
func NewWorkerID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("async worker id: crypto/rand failed: " + err.Error()) // unrecoverable: no entropy is not a degraded mode
	}
	return "aw_" + hex.EncodeToString(b[:])
}

// clampText bounds a free-text column so a chatty runner cannot bloat a
// row (the progress note is runner-supplied prose, not data).
func clampText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

const asyncWorkerColumns = `id, worker_id, worker_type, state, cancel_requested,
	IFNULL(component_root, ''), dedupe_key, metadata, progress,
	IFNULL(progress_note, ''), IFNULL(result, ''), IFNULL(error, ''),
	queued_at, IFNULL(started_at, ''), IFNULL(finished_at, '')`

func scanAsyncWorker(rows *sql.Rows) (*AsyncWorkerRow, error) {
	var w AsyncWorkerRow
	var cancel int
	var progress sql.NullFloat64
	if err := rows.Scan(&w.ID, &w.WorkerID, &w.WorkerType, &w.State, &cancel,
		&w.ComponentRoot, &w.DedupeKey, &w.Metadata, &progress,
		&w.ProgressNote, &w.Result, &w.Error,
		&w.QueuedAt, &w.StartedAt, &w.FinishedAt); err != nil {
		return nil, err
	}
	w.CancelRequested = cancel != 0
	if progress.Valid {
		p := progress.Float64
		w.Progress = &p
	}
	return &w, nil
}

// asyncExecer is the one INSERT's execution surface: *sql.DB (the writer
// pool, the plain path) and *sql.Tx (the audited path's own transaction)
// both satisfy it, so the INSERT exists once on this file (I2).
type asyncExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// insertAsyncWorkerOn is the queued-row INSERT and its collision
// vocabulary — shared verbatim by both entry points below so a flight
// collision means the same thing and reads the same on either path.
func insertAsyncWorkerOn(e asyncExecer, id string, n NewAsyncWorker, meta string) (*AsyncWorkerRow, error) {
	res, err := e.Exec(`INSERT INTO async_worker
		(worker_id, worker_type, component_root, dedupe_key, metadata, queued_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, n.WorkerType, nullable(n.ComponentRoot), n.DedupeKey, meta, n.QueuedAt)
	if err == nil {
		w := AsyncWorkerRow{
			WorkerID: id, WorkerType: n.WorkerType, State: "queued",
			ComponentRoot: n.ComponentRoot, DedupeKey: n.DedupeKey,
			Metadata: meta, QueuedAt: n.QueuedAt,
		}
		w.ID, _ = res.LastInsertId()
		return &w, nil
	}
	if isUniqueViolation(err) {
		if strings.Contains(err.Error(), "async_worker_flight") ||
			strings.Contains(err.Error(), "dedupe_key") {
			return nil, ErrAsyncWorkerInFlight
		}
		return nil, err // worker_id collision: the caller retries with a fresh id
	}
	return nil, fmt.Errorf("insert async worker %s/%s: %w", n.WorkerType, n.DedupeKey, err)
}

func checkAsyncWorkerNew(n NewAsyncWorker) error {
	if n.WorkerType == "" || n.DedupeKey == "" || n.QueuedAt == "" {
		return fmt.Errorf("insert async worker: worker_type, dedupe_key and queued_at are required, got %q/%q/%q", n.WorkerType, n.DedupeKey, n.QueuedAt)
	}
	return nil
}

// InsertAsyncWorker writes the queued row and returns it. A flight-index
// collision comes back as ErrAsyncWorkerInFlight (callers re-read the live
// row); a bare worker_id collision is retried here once with a fresh id,
// which the caller never needs to see.
func (s *Store) InsertAsyncWorker(n NewAsyncWorker) (*AsyncWorkerRow, error) {
	if err := checkAsyncWorkerNew(n); err != nil {
		return nil, err
	}
	if n.WorkerID == "" {
		n.WorkerID = NewWorkerID()
	}
	meta := n.Metadata
	if meta == "" {
		meta = "{}"
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		id := n.WorkerID
		if attempt == 1 {
			id = NewWorkerID() // only a worker_id collision reaches here; a flight collision below returns at once
		}
		w, err := insertAsyncWorkerOn(s.w, id, n, meta)
		if err == nil {
			return w, nil
		}
		if errors.Is(err, ErrAsyncWorkerInFlight) {
			return nil, err
		}
		if !isUniqueViolation(err) {
			return nil, err // the wrapped insert error, worded as before
		}
		lastErr = err
	}
	return nil, fmt.Errorf("insert async worker %s/%s: worker_id collided twice: %w", n.WorkerType, n.DedupeKey, lastErr)
}

// InsertAsyncWorkerAudited writes the queued row AND its platform_change
// audit row in ONE transaction (S54 SP3): the audit names the operation
// that starts here, outcome_ref is the worker id minted for it, and the
// two rows commit together or not at all — an "already running" collision
// rolls the audit back with the refused insert and leaves zero rows, which
// is how SP3's "already running writes nothing" holds by construction
// rather than by discipline. The collision vocabulary and the single
// fresh-id retry match InsertAsyncWorker byte for byte; the audit's
// shape-law refusal happens BEFORE any transaction opens.
func (s *Store) InsertAsyncWorkerAudited(n NewAsyncWorker, audit PlatformAudit, occurredAt string) (*AsyncWorkerRow, error) {
	if err := checkAsyncWorkerNew(n); err != nil {
		return nil, err
	}
	if n.WorkerID == "" {
		n.WorkerID = NewWorkerID()
	}
	meta := n.Metadata
	if meta == "" {
		meta = "{}"
	}
	if err := checkPlatformAudit(audit); err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		id := n.WorkerID
		if attempt == 1 {
			id = NewWorkerID()
		}
		at := audit
		at.OutcomeRef = id // the audit names THIS attempt's worker, always
		var w *AsyncWorkerRow
		err := s.PlatformWithTx(func(tx *sql.Tx) error {
			if err := PlatformChangeInsertTx(tx, at, occurredAt); err != nil {
				return err
			}
			var insErr error
			w, insErr = insertAsyncWorkerOn(tx, id, n, meta)
			return insErr
		})
		if err == nil {
			return w, nil
		}
		if errors.Is(err, ErrAsyncWorkerInFlight) {
			return nil, err
		}
		if !isUniqueViolation(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("insert async worker %s/%s: worker_id collided twice: %w", n.WorkerType, n.DedupeKey, lastErr)
}

// GetAsyncWorker reads one worker by its external id. sql.ErrNoRows means
// no such worker — the caller decides whether that is a 404 or a lie.
func (s *Store) GetAsyncWorker(workerID string) (*AsyncWorkerRow, error) {
	rows, err := s.r.Query(`SELECT `+asyncWorkerColumns+` FROM async_worker WHERE worker_id = ?`, workerID)
	if err != nil {
		return nil, fmt.Errorf("get async worker %s: %w", workerID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get async worker %s: %w", workerID, err)
		}
		return nil, sql.ErrNoRows
	}
	return scanAsyncWorker(rows)
}

// ListAsyncWorkers reads workers newest-queued first, narrowed by the
// filter. This is the door the app asks on page mount — "anything of this
// type running for this root" — so (type, state, root) is the index it
// rides and Limit bounds the read.
func (s *Store) ListAsyncWorkers(f AsyncWorkerFilter) ([]AsyncWorkerRow, error) {
	var conds []string
	var args []any
	if f.Type != "" {
		conds = append(conds, "worker_type = ?")
		args = append(args, f.Type)
	}
	if f.DedupeKey != "" {
		conds = append(conds, "dedupe_key = ?")
		args = append(args, f.DedupeKey)
	}
	if f.ComponentRoot != "" {
		conds = append(conds, "component_root = ?")
		args = append(args, f.ComponentRoot)
	}
	if len(f.States) > 0 {
		marks := strings.Repeat("?,", len(f.States))
		conds = append(conds, "state IN ("+strings.TrimSuffix(marks, ",")+")")
		for _, st := range f.States {
			args = append(args, st)
		}
	}
	query := `SELECT ` + asyncWorkerColumns + ` FROM async_worker`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list async workers: %w", err)
	}
	defer rows.Close()
	var out []AsyncWorkerRow
	for rows.Next() {
		w, err := scanAsyncWorker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// StartAsyncWorker flips queued → running and stamps the start. It is
// UPDATE-guarded on the state it expected and reports whether the flip
// took, so a double-start is visible rather than silent.
func (s *Store) StartAsyncWorker(workerID, startedAt string) (bool, error) {
	res, err := s.w.Exec(`UPDATE async_worker SET state = 'running', started_at = ?
		WHERE worker_id = ? AND state = 'queued'`, startedAt, workerID)
	if err != nil {
		return false, fmt.Errorf("start async worker %s: %w", workerID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("start async worker %s: %w", workerID, err)
	}
	return n == 1, nil
}

// ProgressAsyncWorker writes the throttled progress pair. Guarded on
// running: a worker that just went terminal cannot be un-finished by a
// late progress tick.
func (s *Store) ProgressAsyncWorker(workerID string, progress float64, note string) (bool, error) {
	if progress < 0 || progress > 1 {
		return false, fmt.Errorf("progress async worker %s: %g outside 0..1", workerID, progress)
	}
	res, err := s.w.Exec(`UPDATE async_worker SET progress = ?, progress_note = ?
		WHERE worker_id = ? AND state = 'running'`, progress, clampText(note, 512), workerID)
	if err != nil {
		return false, fmt.Errorf("progress async worker %s: %w", workerID, err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// FinishAsyncWorker writes a terminal state with its payload. The WHERE
// guard is the whole point: only a still-live (queued or running, however
// many progress ticks it left behind) row may finish here, so an
// interrupting pass that got there first stands. Reports whether the flip
// took.
func (s *Store) FinishAsyncWorker(workerID, state, result, errMsg, at string) (bool, error) {
	if state != "finished" && state != "failed" && state != "cancelled" && state != "interrupted" {
		return false, fmt.Errorf("finish async worker %s: %q is not a terminal state", workerID, state)
	}
	res, err := s.w.Exec(`UPDATE async_worker
		SET state = ?, result = ?, error = ?, finished_at = ?, progress_note = NULL
		WHERE worker_id = ? AND state IN ('queued','running')`,
		state, nullable(result), nullable(errMsg), at, workerID)
	if err != nil {
		return false, fmt.Errorf("finish async worker %s as %s: %w", workerID, state, err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// RequestCancelAsyncWorker records the request on a live row (the goroutine
// owns the terminal write) and reports the state it asked in — "running"
// means the manager must also cancel the context, "queued" means the
// scheduler will never launch it. A terminal row is refused with ("", nil):
// nothing to cancel, the state is the answer.
func (s *Store) RequestCancelAsyncWorker(workerID string) (string, error) {
	rows, err := s.r.Query(`SELECT state FROM async_worker WHERE worker_id = ?`, workerID)
	if err != nil {
		return "", fmt.Errorf("cancel async worker %s: %w", workerID, err)
	}
	var state string
	found := rows.Next()
	if found {
		if err := rows.Scan(&state); err != nil {
			rows.Close()
			return "", fmt.Errorf("cancel async worker %s: %w", workerID, err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", fmt.Errorf("cancel async worker %s: %w", workerID, err)
	}
	rows.Close()
	if !found {
		return "", sql.ErrNoRows
	}
	if state != "queued" && state != "running" {
		return "", nil
	}
	if _, err := s.w.Exec(`UPDATE async_worker SET cancel_requested = 1 WHERE worker_id = ?`, workerID); err != nil {
		return "", fmt.Errorf("cancel async worker %s: %w", workerID, err)
	}
	return state, nil
}

// MarkAsyncWorkersInterrupted is the recovery bulk flip: every
// queued-or-running row becomes `interrupted` with its reason spoken. It is
// the daemon saying "I stopped, and this is what I knew" — distinct from
// `failed`, which would blame work that was never given a chance (the third
// state P31 refuses to collapse). Returns how many rows it spoke for.
func (s *Store) MarkAsyncWorkersInterrupted(at string) (int, error) {
	res, err := s.w.Exec(`UPDATE async_worker
		SET state = 'interrupted', finished_at = ?, error = 'daemon stopped before the worker finished'
		WHERE state IN ('queued','running')`, at)
	if err != nil {
		return 0, fmt.Errorf("interrupt async workers: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
