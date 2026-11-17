package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// loop_control.go — the store layer behind S55's loop control core (T2). It
// moves loop state by compare-and-set in one transaction (LC2), writes the
// append-only audit (LC10), and holds the inert request desk (LC15).
//
// # What this file does NOT do
//
// It never writes a terminal status. `aborted` and `done` are the engine's
// `endStatus` to write (the one-writer rule, LC4); a cancel here sets the
// aborted FLAG in the runtime JSON and returns the state for the engine's
// `endStatus`/`emitIfOver` path to produce the terminal. And it never touches
// an epic's budget — that is the budget file's concern (LC13: the budget row
// is the single truth, the old column dead).
//
// # The compare-and-set
//
// A control act changes state at most once. `ParkLoopStopped` is a CAS on
// `status='running'`; a loop that moved elsewhere meanwhile is a no-op (Changed
// false) and writes NO event (LC10: one row per CHANGING act). `ResumeLoop` and
// `CancelLoop` are likewise guarded, and a lost race is a typed refusal (DB19:
// `error` is for infrastructure; a refused transition is a result, not an
// error) — except the park, whose no-op is a normal person-facing answer, so it
// returns a result, not a refusal.

// ControlRefusal is a refused control transition. It is a result, not an
// error: the caller (the daemon, the socket, loop.show) renders it verbatim.
// Kind is machine-readable (it rides ErrLoopControlRefused.data.kind); Detail
// is the person's sentence (what / why / do).
type ControlRefusal struct {
	Kind   string
	Detail string
}

func (r ControlRefusal) Error() string { return fmt.Sprintf("refused: %s · %s", r.Kind, r.Detail) }

// Parked is the result of a compare-and-set park: whether the state changed and
// the status it now holds (on a lost race, the status the loop already had).
type Parked struct {
	Changed bool
	Status  string
}

// ParkLoopStopped parks a running loop to waiting/stopped, atomically, in one
// transaction (LC2 step 3). It is a compare-and-set on status='running'; if the
// loop moved elsewhere (waiting/blocked/done/aborted) meanwhile it is a no-op
// that changes nothing and writes NO event (LC10). The runtime JSON's lastReason
// is set to "stopped" so the pinned state and the row agree — a JSON merge, not
// a hand-edit of the whole pin (the CommitGate pattern), and it is written only
// when the park changed the status.
//
// actor is the peer identity stamped by the daemon (never from a request); the
// event's actor_kind is derived here. reason is person-authored text, stored in
// its own bounded column, never folded into a summary (SEC-033).
func (s *Store) ParkLoopStopped(loopID, actorKind, actorID, reason string) (Parked, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return Parked{}, fmt.Errorf("store: park stop %s: %w", loopID, err)
	}
	defer tx.Rollback()
	now := s.Now()

	// The compare-and-set: running -> waiting, in one UPDATE. Zero rows means the
	// loop was not running (a lost race, or it never was) — read its real status
	// for the no-op answer, write nothing.
	res, err := tx.Exec(`UPDATE loop SET status='waiting', updated_at=? WHERE id=? AND status='running'`, now, loopID)
	if err != nil {
		return Parked{}, fmt.Errorf("store: park %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Parked{}, fmt.Errorf("store: park %s: %w", loopID, err)
	}
	if n == 0 {
		// Not running: read the current status for the no-op answer (the daemon
		// names the remedy — "waiting on a question — answer it, or cancel").
		var status string
		if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				tx.Rollback()
				return Parked{}, fmt.Errorf("store: park %s: %w", loopID, ErrNotFound)
			}
			return Parked{}, fmt.Errorf("store: read the status of %s: %w", loopID, err)
		}
		// A lost race writes no event (LC10); the status tells the caller what
		// the loop already is. Changed:false.
		tx.Rollback()
		return Parked{Changed: false, Status: status}, nil
	}

	// The park changed the status: rewrite the pinned runtime JSON so the pin and
	// the row agree (lastReason='stopped'). A JSON merge, written in the same
	// transaction; if the pin is absent the park still stands (the row is the
	// truth), but a present pin must agree.
	if err := s.setRuntimeLastReason(tx, loopID, "stopped", now); err != nil {
		return Parked{}, fmt.Errorf("store: park %s: %w", loopID, err)
	}
	// One event row, only because the act changed state.
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "stop",
		ActorKind: actorKind, ActorID: actorID, Reason: reason,
	}); err != nil {
		return Parked{}, fmt.Errorf("store: park %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return Parked{}, fmt.Errorf("store: park %s: %w", loopID, err)
	}
	return Parked{Changed: true, Status: "waiting"}, nil
}

// ResumeLoop resumes a stopped-waiting or stuck-blocked loop to running, by
// compare-and-set (LC3). It refuses every other state with a typed ControlRefusal
// (the same eligibility the engine and loop.show read, so the button and the
// daemon cannot disagree). On a lost race it answers ErrNotFound, never a wrong
// resume.
// ResumableBlock reports whether a blocked loop's reason is a STEP BOUND — the
// step ran out of model calls, of working time, or of room to compact — rather
// than a verdict (a cap, a missing handler, a rejected gate). A bound is the
// person's to lift (raise a setting, answer faster, resume): the step is
// re-dispatched from its position, exactly as for a watchdog's stuck block.
func ResumableBlock(lastReason string) bool {
	return strings.HasPrefix(lastReason, "exceeded ") || strings.HasPrefix(lastReason, "cannot compact")
}

// ResumableStepBlock widens ResumableBlock to a step that FINISHED blocked: it
// reported it could not complete (a tool refused, a prerequisite was missing),
// which a person can fix and then ask it to try again. The outcome is the
// step's own and is cleared by the resume, so it cannot outlive the attempt it
// describes and make some later, different block look resumable.
func ResumableStepBlock(lastReason, lastOutcome string) bool {
	return ResumableBlock(lastReason) || lastOutcome == "blocked"
}

func (s *Store) ResumeLoop(loopID, actorKind, actorID string) (Parked, ControlRefusal, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	defer tx.Rollback()
	// Read the current state in the write transaction, so the eligibility and the
	// compare-and-set see one consistent snapshot: the status from the row, and
	// the lastReason from the pin (it is not a loop column). A lost race is a
	// refused transition, never a wrong resume.
	var status string
	if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, ErrNotFound)
		}
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	lastReason, lastOutcome := "", ""
	if raw, ok, err := readRuntimeJSON(tx, loopID); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	} else if ok {
		lastReason = lastReasonOf(raw)
		lastOutcome, _ = raw["lastOutcome"].(string)
	}
	// Eligibility: the two states the engine and loop.show read, so the button and
	// the daemon cannot disagree — plus the waiting/cap-hit crash window: an
	// E2 raise CAS'd the loop blocked→waiting (the step continues the re-run)
	// and died before the kick, so the loop is waiting with the cap reason
	// still pinned. A resume completes what the raise started: the runner's
	// OnStepContinued picks up the parked step. A gate-waiting loop
	// (lastReason="") is not resolvable here — it has its own door.
	if !(status == "waiting" && lastReason == "stopped") &&
		!(status == "blocked" && lastReason != "cap-hit" && (lastReason == "stuck" || ResumableStepBlock(lastReason, lastOutcome))) &&
		!(status == "waiting" && lastReason == "cap-hit") {
		tx.Rollback()
		return Parked{}, controlRefusalFor(status, lastReason), nil
	}
	now := s.Now()
	// The compare-and-set: only the two eligible statuses move. A loop that moved
	// meanwhile is a lost race — read its real status and refuse.
	res, err := tx.Exec(`UPDATE loop SET status='running', updated_at=? WHERE id=? AND status IN ('waiting','blocked')`, now, loopID)
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	if n == 0 {
		var cur string
		if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&cur); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				tx.Rollback()
				return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, ErrNotFound)
			}
			tx.Rollback()
			return Parked{}, ControlRefusal{}, fmt.Errorf("store: read the status of %s: %w", loopID, err)
		}
		tx.Rollback()
		return Parked{}, controlRefusalFor(cur, ""), nil
	}
	// The pin and the row must agree after a resume: clear the lastReason the
	// stopped/stuck reason carried. A JSON merge in the same transaction.
	if err := s.setRuntimeLastReason(tx, loopID, "", now); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "resume",
		ActorKind: actorKind, ActorID: actorID,
	}); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: resume %s: %w", loopID, err)
	}
	return Parked{Changed: true, Status: "running"}, ControlRefusal{}, nil
}

// RaiseLoopToRunning is the E1 raise's CAS: a loop blocked on a budget cap
// (blocked, lastReason='cap-hit') moves to running, the capHit record is
// cleared from the pin, and a raise control event is written — all in one
// transaction (LC2/LC10). The daemon then starts a fresh runner, which
// re-evaluates the budget at the next dispatch with the raised cap. A loop in
// any other state is refused with its own remedy, and a lost race is a typed
// refusal, never a wrong move. The detail carries the hit's numbers (closed
// keys only, LC10); the reason is the event's own person-authored column —
// person text never rides in the detail.
func (s *Store) RaiseLoopToRunning(loopID, actorKind, actorID, reason string, detail map[string]any) (Parked, ControlRefusal, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, ErrNotFound)
		}
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	lastReason := ""
	if raw, ok, err := readRuntimeJSON(tx, loopID); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	} else if ok {
		lastReason = lastReasonOf(raw)
	}
	// The one eligible state: a cap-hit block. Every other state has its own
	// door — a raise on a running loop would move a loop that is not parked.
	if !(status == "blocked" && lastReason == "cap-hit") {
		tx.Rollback()
		return Parked{}, controlRefusalFor(status, lastReason), nil
	}
	now := s.Now()
	// The compare-and-set: only a blocked loop moves. A loop that moved
	// meanwhile (a person cancelled it, another raise won) is a lost race —
	// read its real status and refuse.
	res, err := tx.Exec(`UPDATE loop SET status='running', updated_at=? WHERE id=? AND status='blocked'`, now, loopID)
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	if n == 0 {
		var cur string
		if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&cur); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				tx.Rollback()
				return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, ErrNotFound)
			}
			tx.Rollback()
			return Parked{}, ControlRefusal{}, fmt.Errorf("store: read the status of %s: %w", loopID, err)
		}
		tx.Rollback()
		return Parked{}, controlRefusalFor(cur, ""), nil
	}
	// The pin and the row must agree after a raise: clear the lastReason and
	// the capHit record the block carried, in one JSON merge in the same
	// transaction (the setRuntimeLastReason pattern, plus the capHit key).
	if err := s.setRuntimeRaiseClear(tx, loopID, now); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "raise",
		ActorKind: actorKind, ActorID: actorID, Reason: reason, Detail: marshalControlDetail(detail),
	}); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	return Parked{Changed: true, Status: "running"}, ControlRefusal{}, nil
}

// RaiseLoopToWaiting is the E2 raise's CAS: a loop blocked on an in-step cap
// hit (blocked, lastReason='cap-hit') moves to waiting — keeping the cap-hit
// reason so loop.show and the resume door still name it — the capHit record is
// cleared from the pin, and a raise control event is written (LC2/LC10). The
// step is NOT completed here: the daemon continues it through the
// step-continue seam (its checkpoint is intact), and the OnStepContinued
// hand-back moves the ladder. A crash between this CAS and the kick leaves the
// loop waiting/cap-hit — recoverable, because CheckResume and ResumeLoop admit
// that state and the resume's runner re-runs the parked step. The reason is the
// event's own person-authored column (LC10: person text never rides in the
// closed-key detail).
func (s *Store) RaiseLoopToWaiting(loopID, actorKind, actorID, reason string, detail map[string]any) (Parked, ControlRefusal, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, ErrNotFound)
		}
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	lastReason := ""
	if raw, ok, err := readRuntimeJSON(tx, loopID); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	} else if ok {
		lastReason = lastReasonOf(raw)
	}
	if !(status == "blocked" && lastReason == "cap-hit") {
		tx.Rollback()
		return Parked{}, controlRefusalFor(status, lastReason), nil
	}
	now := s.Now()
	// The compare-and-set: only a blocked loop moves to waiting. The
	// lastReason stays 'cap-hit' (the row keeps naming what it is); only the
	// status and the capHit record move.
	res, err := tx.Exec(`UPDATE loop SET status='waiting', updated_at=? WHERE id=? AND status='blocked'`, now, loopID)
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	if n == 0 {
		var cur string
		if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&cur); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				tx.Rollback()
				return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, ErrNotFound)
			}
			tx.Rollback()
			return Parked{}, controlRefusalFor(cur, ""), nil
		}
		tx.Rollback()
		return Parked{}, controlRefusalFor(cur, ""), nil
	}
	// Clear the capHit record from the pin (the reason stays). A JSON merge in
	// the same transaction; a missing pin is tolerated (the row is the truth).
	if err := s.clearRuntimeCapHit(tx, loopID, now); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "raise",
		ActorKind: actorKind, ActorID: actorID, Reason: reason, Detail: marshalControlDetail(detail),
	}); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: raise %s: %w", loopID, err)
	}
	return Parked{Changed: true, Status: "waiting"}, ControlRefusal{}, nil
}

// setRuntimeRaiseClear merges lastReason="" and capHit=null into the pin, in
// the transaction: the E1 raise moves the loop to running, so the block's
// reason and its cap record are both cleared and the pin and the row agree.
func (s *Store) setRuntimeRaiseClear(tx *sql.Tx, loopID, now string) error {
	var raw string
	if err := tx.QueryRow(`SELECT runtime_json FROM loop_blueprint WHERE loop_id=?`, loopID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // no pin: the row is the truth; the raise stands
		}
		return fmt.Errorf("read the runtime of %s: %w", loopID, err)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(raw), &rt); err != nil {
		return fmt.Errorf("parse the runtime of %s: %w", loopID, err)
	}
	rt["lastReason"] = ""
	rt["capHit"] = nil
	updated, err := json.Marshal(rt)
	if err != nil {
		return fmt.Errorf("encode the runtime of %s: %w", loopID, err)
	}
	if _, err := tx.Exec(`UPDATE loop_blueprint SET runtime_json=?, updated_at=? WHERE loop_id=?`, string(updated), now, loopID); err != nil {
		return fmt.Errorf("save the runtime of %s: %w", loopID, err)
	}
	return nil
}

// clearRuntimeCapHit merges capHit=null into the pin, in the transaction,
// WITHOUT touching lastReason: the E2 raise moves the loop to waiting but
// keeps the cap-hit reason (the loop is still named by it; a resume from the
// crash window needs it). A missing pin is tolerated.
func (s *Store) clearRuntimeCapHit(tx *sql.Tx, loopID, now string) error {
	var raw string
	if err := tx.QueryRow(`SELECT runtime_json FROM loop_blueprint WHERE loop_id=?`, loopID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("read the runtime of %s: %w", loopID, err)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(raw), &rt); err != nil {
		return fmt.Errorf("parse the runtime of %s: %w", loopID, err)
	}
	rt["capHit"] = nil
	updated, err := json.Marshal(rt)
	if err != nil {
		return fmt.Errorf("encode the runtime of %s: %w", loopID, err)
	}
	if _, err := tx.Exec(`UPDATE loop_blueprint SET runtime_json=?, updated_at=? WHERE loop_id=?`, string(updated), now, loopID); err != nil {
		return fmt.Errorf("save the runtime of %s: %w", loopID, err)
	}
	return nil
}

// marshalControlDetail renders a raise event's closed-key detail map to JSON
// ('{}' for an empty one). The keys are validated against the closed set when
// the event is appended (appendControlEvent), so this only encodes.
func marshalControlDetail(detail map[string]any) string {
	if len(detail) == 0 {
		return "{}"
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// CancelLoop records a cancel's intent: it sets the aborted flag in the runtime
// JSON, clears the position, and returns the state for the engine's endStatus /
// emitIfOver path to produce the terminal (LC4). It does NOT write 'aborted'
// itself — the one-writer rule is the engine's endStatus. A second cancel (the
// loop is already aborted) is refused. reason is person-authored, stored in its
// own column on the control event, never folded into a summary (SEC-033).
func (s *Store) CancelLoop(loopID, actorKind, actorID, reason string) (Parked, ControlRefusal, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, err)
	}
	defer tx.Rollback()
	// Read the current state in the write transaction, so the eligibility and the
	// flag write see one consistent snapshot.
	var status string
	if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, ErrNotFound)
		}
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, err)
	}
	// A loop that is already terminal, or not a cancellable state, is refused
	// with its own remedy.
	switch status {
	case "running", "waiting", "blocked", "idle":
		// cancellable
	default:
		tx.Rollback()
		return Parked{}, controlRefusalFor(status, ""), nil
	}
	// A second cancel (the flag already set) is refused: read it from the pin.
	// The flag is set once, here, by the cancel — the engine's endStatus reads it.
	if raw, ok, err := readRuntimeJSON(tx, loopID); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, err)
	} else if ok {
		if flags, _ := raw["flags"].(map[string]any); flags != nil {
			if ab, _ := flags["aborted"].(bool); ab {
				tx.Rollback()
				return Parked{}, ControlRefusal{Kind: "cancelled", Detail: "the loop is already being cancelled — it cannot be cancelled again"}, nil
			}
		}
	}
	now := s.Now()
	// Set the aborted flag in the runtime JSON (the engine's endStatus reads it);
	// the store never writes the terminal status itself.
	if err := s.setRuntimeFlag(tx, loopID, "aborted", now); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, err)
	}
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "cancel",
		ActorKind: actorKind, ActorID: actorID, Reason: reason,
	}); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, err)
	}
	if err := tx.Commit(); err != nil {
		return Parked{}, ControlRefusal{}, fmt.Errorf("store: cancel %s: %w", loopID, err)
	}
	// The state is returned for the engine to finish (endStatus/emitIfOver). The
	// store changed nothing terminal; it recorded the intent and the flag.
	return Parked{Changed: true, Status: status}, ControlRefusal{}, nil
}

// controlRefusalFor names the remedy for a state a control act cannot apply to.
// It is the one sentence the daemon renders verbatim (DB19: a refusal names the
// reason and the thing refused).
func controlRefusalFor(status, lastReason string) ControlRefusal {
	switch status {
	case "running":
		return ControlRefusal{Kind: "running", Detail: "the loop is running — it cannot be resumed or cancelled here; stop it first"}
	case "waiting":
		if lastReason == "stopped" {
			return ControlRefusal{Kind: "running", Detail: "the loop is waiting (stopped) — resume it"}
		}
		if lastReason == "cap-hit" {
			return ControlRefusal{Kind: "cap-hit", Detail: "the loop is waiting on a step that stopped on a budget cap — resume it (loop.resume) to continue the step"}
		}
		return ControlRefusal{Kind: "waiting-question", Detail: "the loop is waiting on a question — answer it, or cancel the loop"}
	case "blocked":
		if lastReason == "cap-hit" {
			return ControlRefusal{Kind: "cap-hit", Detail: "the loop is blocked on a budget cap — raise it (loop.raise), it cannot be resumed or cancelled"}
		}
		if lastReason == "stuck" {
			return ControlRefusal{Kind: "running", Detail: "the loop is blocked (stuck) — resume it"}
		}
		if ResumableBlock(lastReason) {
			return ControlRefusal{Kind: "running", Detail: "the loop is blocked on a step bound — resume it to re-run the step"}
		}
		return ControlRefusal{Kind: "blocked", Detail: "the loop is blocked — it has its own remedy, not a resume or cancel"}
	case "idle":
		return ControlRefusal{Kind: "idle", Detail: "the loop is idle — close it (loop.close), or cancel it"}
	default:
		return ControlRefusal{Kind: "terminal", Detail: "the loop is " + status + " — it has no control to apply"}
	}
}

// setRuntimeLastReason merges lastReason into the loop's pinned runtime JSON, in
// the transaction, so the pin and the row agree after a park (LC2). A JSON merge
// (the CommitGate pattern), never a hand-edit of the whole pin. A missing pin is
// tolerated (the row is the truth); a corrupt pin is an error.
func (s *Store) setRuntimeLastReason(tx *sql.Tx, loopID, lastReason, now string) error {
	var raw string
	if err := tx.QueryRow(`SELECT runtime_json FROM loop_blueprint WHERE loop_id=?`, loopID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // no pin: the row is the truth; the park stands
		}
		return fmt.Errorf("read the runtime of %s: %w", loopID, err)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(raw), &rt); err != nil {
		return fmt.Errorf("parse the runtime of %s: %w", loopID, err)
	}
	rt["lastReason"] = lastReason
	if lastReason == "" { // a resume: the outcome described the attempt being re-run
		delete(rt, "lastOutcome")
	}
	updated, err := json.Marshal(rt)
	if err != nil {
		return fmt.Errorf("encode the runtime of %s: %w", loopID, err)
	}
	if _, err := tx.Exec(`UPDATE loop_blueprint SET runtime_json=?, updated_at=? WHERE loop_id=?`, string(updated), now, loopID); err != nil {
		return fmt.Errorf("save the runtime of %s: %w", loopID, err)
	}
	return nil
}

// setRuntimeFlag sets a boolean flag in the loop's pinned runtime JSON (LC4's
// cancel: Flags["aborted"]). A JSON merge in the transaction; the engine's
// endStatus reads it to produce the terminal. A missing pin is tolerated.
func (s *Store) setRuntimeFlag(tx *sql.Tx, loopID, flag, now string) error {
	var raw string
	if err := tx.QueryRow(`SELECT runtime_json FROM loop_blueprint WHERE loop_id=?`, loopID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("read the runtime of %s: %w", loopID, err)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(raw), &rt); err != nil {
		return fmt.Errorf("parse the runtime of %s: %w", loopID, err)
	}
	flags, _ := rt["flags"].(map[string]any)
	if flags == nil {
		flags = map[string]any{}
	}
	flags[flag] = true
	rt["flags"] = flags
	updated, err := json.Marshal(rt)
	if err != nil {
		return fmt.Errorf("encode the runtime of %s: %w", loopID, err)
	}
	if _, err := tx.Exec(`UPDATE loop_blueprint SET runtime_json=?, updated_at=? WHERE loop_id=?`, string(updated), now, loopID); err != nil {
		return fmt.Errorf("save the runtime of %s: %w", loopID, err)
	}
	return nil
}

// readRuntimeJSON reads and unmarshals a loop's pinned runtime JSON, on the
// transaction. ok is false when there is no pin (the row is the truth, not an
// error); a present but corrupt pin is an error. The control CAS (resume,
// cancel) reads lastReason and Flags from here, because they are not loop
// columns.
func readRuntimeJSON(tx *sql.Tx, loopID string) (map[string]any, bool, error) {
	var raw string
	if err := tx.QueryRow(`SELECT runtime_json FROM loop_blueprint WHERE loop_id=?`, loopID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read the runtime of %s: %w", loopID, err)
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(raw), &rt); err != nil {
		return nil, false, fmt.Errorf("parse the runtime of %s: %w", loopID, err)
	}
	return rt, true, nil
}

// lastReasonOf reads the lastReason key from a parsed runtime JSON. An absent
// or non-string value reads as "" (no reason), so a gate-waiting loop (which
// never set it) is not eligible for a resume.
func lastReasonOf(rt map[string]any) string {
	if lr, ok := rt["lastReason"].(string); ok {
		return lr
	}
	return ""
}

// ControlEvent is one row of loop_control_event (LC10). Detail is JSON of
// enums/numbers only (closed keys); Reason is person-authored, its own column.
type ControlEvent struct {
	LoopID    string
	At        string
	Kind      string // stop|resume|cancel|raise|cap_hit|watchdog|request|dismiss
	ActorKind string // user|cli|system|agent
	ActorID   string
	StepID    *int64
	RuleID    string
	Reason    string
	Detail    string // JSON, '{}' when none
}

// appendControlEvent writes one loop_control_event row, on the shared
// transaction. Detail keys are validated against a closed set (LC10); unknown
// keys are refused, so a person's text can never ride in detail.
func appendControlEvent(tx *sql.Tx, e ControlEvent) error {
	if e.Detail == "" {
		e.Detail = "{}"
	}
	var rt map[string]any
	if err := json.Unmarshal([]byte(e.Detail), &rt); err != nil {
		return fmt.Errorf("store: control event detail is not JSON: %w", err)
	}
	for k := range rt {
		if !allowedControlDetailKey(k) {
			return fmt.Errorf("store: control event detail key %q is not in the closed set", k)
		}
	}
	_, err := tx.Exec(`INSERT INTO loop_control_event (loop_id,at,kind,actor_kind,actor_id,step_id,rule_id,reason,detail)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		e.LoopID, e.At, e.Kind, e.ActorKind, e.ActorID, nullableInt64(e.StepID),
		e.RuleID, e.Reason, e.Detail)
	if err != nil {
		return fmt.Errorf("store: append control event: %w", err)
	}
	return nil
}

// allowedControlDetailKey is the closed set of JSON keys a control event's
// detail may carry (LC10: enums/numbers only, no user text). A value that is a
// string (person text) is refused at the handler, not here.
func allowedControlDetailKey(k string) bool {
	switch k {
	case "budgetId", "scope", "subjectId", "dimension", "spendFloor", "cap", "window",
		"stepId", "ruleId", "signal", "action", "episode", "reasonKind", "parkedSteps", "how":
		return true
	}
	return false
}

// ListControlEvents reads a loop's control events, newest first (the audit).
func (s *Store) ListControlEvents(loopID string, limit int) ([]ControlEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.r.Query(`SELECT loop_id,at,kind,actor_kind,actor_id,step_id,rule_id,reason,detail
		FROM loop_control_event WHERE loop_id=? ORDER BY id DESC LIMIT ?`, loopID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list control events: %w", err)
	}
	defer rows.Close()
	var out []ControlEvent
	for rows.Next() {
		var e ControlEvent
		var step sql.NullInt64
		if err := rows.Scan(&e.LoopID, &e.At, &e.Kind, &e.ActorKind, &e.ActorID, &step, &e.RuleID, &e.Reason, &e.Detail); err != nil {
			return nil, fmt.Errorf("store: scan control event: %w", err)
		}
		if step.Valid {
			v := step.Int64
			e.StepID = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// The two counted limits of the request desk (LC15), named so the refusal and
// the tests point at the same numbers: open stop requests per loop and per
// agent. A refusal names the limit AND the door that shows the open ones.
const (
	ControlRequestOpenPerLoop  = 5
	ControlRequestOpenPerAgent = 20
)

// ControlRequest is the inert request desk (LC15): an agent may file a request
// that a person stop a loop; nothing halts until a person acts.
type ControlRequest struct {
	ID          int64
	LoopID      string
	RequestedAt string
	ByAgent     string
	ByStepID    *int64
	Reason      string
	Status      string // open|honored|dismissed
	DecidedAt   string
	DecidedBy   string
}

// AddControlRequest files one request, counted against the per-loop (5) and
// per-agent (20) limits; a refusal names the limit (LC15). The agent identity is
// stamped by the daemon (never from a request).
func (s *Store) AddControlRequest(loopID, agent, reason string, stepID *int64) (ControlRequest, error) {
	tx, err := s.w.Begin()
	if err != nil {
		return ControlRequest{}, fmt.Errorf("store: add control request: %w", err)
	}
	defer tx.Rollback()
	// The two limits, counted in the writing transaction so a concurrent file
	// cannot push past them.
	var openForLoop, openForAgent int
	if err := tx.QueryRow(`SELECT count(*) FROM loop_control_request WHERE loop_id=? AND status='open'`, loopID).Scan(&openForLoop); err != nil {
		return ControlRequest{}, fmt.Errorf("store: count open requests on %s: %w", loopID, err)
	}
	if err := tx.QueryRow(`SELECT count(*) FROM loop_control_request WHERE by_agent=? AND status='open'`, agent).Scan(&openForAgent); err != nil {
		return ControlRequest{}, fmt.Errorf("store: count open requests by %s: %w", agent, err)
	}
	if openForLoop >= ControlRequestOpenPerLoop {
		tx.Rollback()
		return ControlRequest{}, ControlRefusal{Kind: "request-limit", Detail: fmt.Sprintf("the loop already has %d open stop requests (the limit is %d) — loop.control's open requests list the open ones", openForLoop, ControlRequestOpenPerLoop)}
	}
	if openForAgent >= ControlRequestOpenPerAgent {
		tx.Rollback()
		return ControlRequest{}, ControlRefusal{Kind: "request-limit", Detail: fmt.Sprintf("your agent already has %d open stop requests (the limit is %d) — loop.control's open requests list the open ones", openForAgent, ControlRequestOpenPerAgent)}
	}
	now := s.Now()
	res, err := tx.Exec(`INSERT INTO loop_control_request (loop_id,requested_at,by_agent,by_step_id,reason,status)
		VALUES (?,?,?,?,?,'open')`, loopID, now, agent, nullableInt64(stepID), reason)
	if err != nil {
		return ControlRequest{}, fmt.Errorf("store: add control request: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ControlRequest{}, fmt.Errorf("store: add control request: %w", err)
	}
	// The file is itself a control event (kind 'request'), so the audit shows
	// it. One row, only because a new request is a changing act — and it commits
	// with the row in the same transaction.
	if err := appendControlEvent(tx, ControlEvent{
		LoopID: loopID, At: now, Kind: "request",
		ActorKind: "agent", ActorID: agent, Reason: reason,
	}); err != nil {
		return ControlRequest{}, fmt.Errorf("store: add control request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ControlRequest{}, fmt.Errorf("store: add control request: %w", err)
	}
	return ControlRequest{ID: id, LoopID: loopID, RequestedAt: now, ByAgent: agent, ByStepID: stepID, Reason: reason, Status: "open"}, nil
}

// DecideControlRequest moves a request open -> honored|dismissed exactly once
// (the trigger guards the rest). A decided or missing request is refused.
func (s *Store) DecideControlRequest(id int64, status, actor string) (ControlRequest, error) {
	if status != "honored" && status != "dismissed" {
		return ControlRequest{}, fmt.Errorf("store: a control request is decided honored or dismissed, not %q", status)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return ControlRequest{}, fmt.Errorf("store: decide control request: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	res, err := tx.Exec(`UPDATE loop_control_request SET status=?, decided_at=?, decided_by=? WHERE id=? AND status='open'`,
		status, now, actor, id)
	if err != nil {
		return ControlRequest{}, fmt.Errorf("store: decide control request: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ControlRequest{}, fmt.Errorf("store: decide control request: %w", err)
	}
	if n == 0 {
		tx.Rollback()
		return ControlRequest{}, fmt.Errorf("store: control request %d is not open: %w", id, ErrNotFound)
	}
	if err := tx.Commit(); err != nil {
		return ControlRequest{}, fmt.Errorf("store: decide control request: %w", err)
	}
	// Read the decided row back by id (the UPDATE matched exactly one open row).
	var r ControlRequest
	var step sql.NullInt64
	if err := s.r.QueryRow(`SELECT id,loop_id,requested_at,by_agent,by_step_id,reason,status,COALESCE(decided_at,''),COALESCE(decided_by,'')
		FROM loop_control_request WHERE id=?`, id).Scan(&r.ID, &r.LoopID, &r.RequestedAt, &r.ByAgent, &step, &r.Reason, &r.Status, &r.DecidedAt, &r.DecidedBy); err != nil {
		return ControlRequest{}, fmt.Errorf("store: read decided control request %d: %w", id, err)
	}
	if step.Valid {
		v := step.Int64
		r.ByStepID = &v
	}
	return r, nil
}

// ControlRequestFilter is the read filter for the request desk.
type ControlRequestFilter struct {
	LoopID string
	Status string
}

// ListControlRequests reads the request desk (open first, oldest first).
func (s *Store) ListControlRequests(f ControlRequestFilter) ([]ControlRequest, error) {
	q := `SELECT id,loop_id,requested_at,by_agent,by_step_id,reason,status,COALESCE(decided_at,''),COALESCE(decided_by,'')
		FROM loop_control_request WHERE 1=1`
	args := []any{}
	if f.LoopID != "" {
		q += ` AND loop_id=?`
		args = append(args, f.LoopID)
	}
	if f.Status != "" {
		q += ` AND status=?`
		args = append(args, f.Status)
	}
	q += ` ORDER BY status='open' DESC, id`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list control requests: %w", err)
	}
	defer rows.Close()
	var out []ControlRequest
	for rows.Next() {
		var r ControlRequest
		var step sql.NullInt64
		if err := rows.Scan(&r.ID, &r.LoopID, &r.RequestedAt, &r.ByAgent, &step, &r.Reason, &r.Status, &r.DecidedAt, &r.DecidedBy); err != nil {
			return nil, fmt.Errorf("store: scan control request: %w", err)
		}
		if step.Valid {
			v := step.Int64
			r.ByStepID = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// nullableInt64 renders a nil *int64 as SQL NULL (so a request with no step
// stays NULL, never zero — zero is a number, P31).
func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
