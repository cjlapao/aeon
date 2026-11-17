package store

// The steer store: `step_steer`, the durable queue a person writes into a
// running step (S31 T3, D4, LW5).
//
// # What this file owns
//
// Two things, and they are the two that matter.
//
// **The row.** A steer typed into a running step is queued before it is ever
// injected, so the window between "the person pressed enter" and "the model read
// it" is a window the daemon can die in. An in-memory queue loses the instruction
// silently — the transcript the person is watching simply never contains what
// they typed. A row makes that visible: after a crash the drain finds `pending`
// and delivers, or finds `delivered` and finds the text already in the
// checkpointed transcript.
//
// **The transaction.** CheckpointWithSteers writes the new transcript and flips
// the steers it contains to `delivered` in ONE transaction, which is the only way
// D4 holds under `kill -9` at any instruction (LW5). The crash matrix, as S7's
// cold replay will meet it:
//
//	kill before the drain        → pending,   text absent   → drain it
//	kill after the drain         → pending,   text absent   → drain it
//	kill inside the transaction  → pending,   text absent   → drain it  (SQLite
//	                                                            rolls it all back)
//	kill after the commit        → delivered, text present   → leave it alone
//
// The two states that would break a replay — pending WITH the text in the
// transcript, and delivered WITHOUT it — are the two this file makes
// unrepresentable, because there is no instant at which only one half has
// happened. The first is a steer injected twice: the model reads the same
// instruction again and does the thing a second time, which is the kind of wrong
// nobody notices until an agent has committed something twice. The second is a
// person's instruction vanished with nothing to show for it.
//
// # What it deliberately does not do
//
// **It does not decide who may steer.** LW4's refusal of a steer on a step that
// is not `running` belongs to `step.steer`'s handler (brief §6 T4), because what
// is worth saying there is the method to use instead — `answer` for a suspended
// step, `step.continue {message}` for a stopped one. A store that refused on
// `status` would take the naming of the remedy away from the surface that knows
// it. QueueSteer therefore checks only that the step EXISTS: a steer for a step
// that never was is a queue with nobody to drain it.
//
// **It does not enforce LW10's bounds** (≤ 32 KiB of text, ≤ 16 pending per
// step). Those are refusals a person has to be told about, in the method that can
// also say what to do instead; the database's version would arrive as a constraint
// failure with no advice attached (AGENTS §5).
//
// **It does not deliver lazily.** The drain (PendingSteers) and the delivery
// (CheckpointWithSteers) are separate calls because the runtime has to see the
// text in order to append it — but delivery is not a separate write from the
// checkpoint, and the result type says so: what the caller named and what this
// transaction actually flipped are reported apart, never silently reconciled.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// SteerState is `step_steer.state`'s closed vocabulary.
//
// Three values and no fourth: `pending` (queued, not yet read by the model),
// `delivered` (its text is in a checkpointed transcript, and DeliveredAt says
// when), `cancelled` (the person withdrew it before it reached the model). Both
// non-pending states are terminal — which is why a cancel of either is refused
// rather than accepted as a no-op, and why 0021 closes the set with a CHECK
// instead of trusting this enum.
type SteerState string

// The three states, spelled as 0021's CHECK spells them.
const (
	SteerPending   SteerState = "pending"
	SteerDelivered SteerState = "delivered"
	SteerCancelled SteerState = "cancelled"
)

// AllSteerStates is the Go mirror of 0021's `ck_step_steer_state`, in the CHECK's
// own order — the same lock the policy vocabulary keeps (§3): a test reads the
// live DDL out of the catalogue and compares, so the enum and the constraint
// cannot drift by one value in one commit.
func AllSteerStates() []SteerState {
	return []SteerState{SteerPending, SteerDelivered, SteerCancelled}
}

// Steer is one row of `step_steer`.
//
// The three optional payloads are empty strings here and NULL in the column:
// `step.steer` takes at least one of text / route / reasoning effort (brief §5),
// so "the person said nothing" and "the person did not address the text" are
// different facts and one string cannot carry both (the ruling 0003's
// `question.tool_use_id` follows). DeliveredAt is empty until delivered and
// DeliveredIteration is 0 — 0021's delivery CHECK ties both to `delivered`, so no
// row can claim one without the other.
type Steer struct {
	ID     int64 `json:"id"`
	StepID int64 `json:"stepId"`

	Text            string `json:"text,omitempty"`
	Route           string `json:"route,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	State     SteerState `json:"state"`
	CreatedAt string     `json:"createdAt"`

	// DeliveredAt and DeliveredIteration record the ONE delivery: when the
	// checkpoint containing this steer committed, and which iteration of the
	// step's loop wrote it. They are what `step.steer.delivered` reports and what
	// a replay compares against, and they are write-once — a checkpoint naming an
	// already-delivered steer reports it rather than re-stamping it, because a
	// re-stamped timestamp is how a double delivery hides.
	DeliveredAt        string `json:"deliveredAt,omitempty"`
	DeliveredIteration int    `json:"deliveredIteration,omitempty"`

	// Source is where the steer came from (S39 CR6): user is the default
	// and the whole history; blueprint is the engine's own push-back (the
	// missingRecord nudge must not read as a person typing); automation is
	// S20's clock. The row always carries a value — a steer predating S39
	// could only have been a person's, so the default states history.
	Source string `json:"source,omitempty"`

	// Skills are the skill NAMES the operator chose for this steer (S68,
	// B7). nil is the third state (P31): nothing was chosen. `Text` stays
	// the operator's PURE words — the UI bubble reads it alone; the chosen
	// skills are rendered to canonical lines by the daemon at the drain
	// (B7), never stored mixed into `text` (B8 keeps the row honest).
	Skills []string `json:"skills,omitempty"`

	// Attachments is the S69 receipt — references into the frozen
	// @attachments/ document revisions, never bytes (AT2).
	Attachments []AttachmentReceipt `json:"attachments,omitempty"`
}

// NewSteer is what a caller supplies to QueueSteer: at least one of the three,
// with the empty string meaning "not supplied" (see Steer).
type NewSteer struct {
	Text            string
	Route           string
	ReasoningEffort string
	// Source labels provenance; "" defaults to user (CR6).
	Source string
	// Attachments are the S69 receipts filed by the steer door.
	Attachments []AttachmentReceipt

	// Skills are the skill NAMES chosen for this steer (S68, B7). The daemon
	// door resolves them against the live catalog before queuing, refusing
	// any name it cannot find; empty stores NULL.
	Skills []string
}

// steerColumns is the one column list every read of this table uses, so no two
// readers disagree about what a steer row is.
const steerColumns = `id, step_id, COALESCE(text,''), COALESCE(route,''), COALESCE(reasoning_effort,''),
	state, created_at, COALESCE(delivered_at,''), COALESCE(delivered_iteration,0), source, skills, attachments`

func scanSteer(row interface{ Scan(...any) error }) (Steer, error) {
	var st Steer
	var state string
	var skills, attachments sql.NullString
	if err := row.Scan(&st.ID, &st.StepID, &st.Text, &st.Route, &st.ReasoningEffort,
		&state, &st.CreatedAt, &st.DeliveredAt, &st.DeliveredIteration, &st.Source, &skills, &attachments); err != nil {
		return Steer{}, err
	}
	st.State = SteerState(state)
	var err error
	if st.Skills, err = decodeSkills(skills); err != nil {
		return Steer{}, err
	}
	if st.Attachments, err = decodeAttachments(attachments); err != nil {
		return Steer{}, err
	}
	return st, nil
}

// QueueSteer queues one steer for a step and returns the row it wrote.
//
// # The contract
//
//   - The row lands `pending`, with created_at from the store's clock. Nothing
//     else in the store writes a steer at all, and nothing else sets its state:
//     delivery and cancellation go through the two methods below, which is what
//     makes a steer's history readable afterwards.
//   - The step must exist, and that is the only precondition (see this file's
//     header for why LW4 lives at the handler).
//   - At least one payload must be present, or the steer would inject nothing —
//     the row a caller with a broken parameter check writes, and the reason the
//     same rule is a CHECK in 0021 rather than only a check here.
//
// # The crash argument
//
// A steer is durable the instant this returns. Whatever happens to the daemon
// afterwards — including between the drain and the checkpoint, the window that
// makes LW5 what it is — the row is there for the next process to find, either
// still pending or already accounted for in the transcript.
func (s *Store) QueueSteer(stepID int64, in NewSteer) (Steer, error) {
	text := strings.TrimSpace(in.Text)
	route := strings.TrimSpace(in.Route)
	effort := strings.TrimSpace(in.ReasoningEffort)
	if text == "" && route == "" && effort == "" {
		return Steer{}, fmt.Errorf("store: refusing to queue a steer for step %d: it carries no text, no route "+
			"and no reasoning effort, so it would inject nothing", stepID)
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Steer{}, fmt.Errorf("store: queue a steer for step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Named here rather than left to the FOREIGN KEY constraint: an FK failure
	// says "FOREIGN KEY constraint failed" and not which step it refused, and the
	// caller is the one holding the id.
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM step WHERE id = ?`, stepID).Scan(&exists); err == sql.ErrNoRows {
		return Steer{}, fmt.Errorf("store: cannot queue a steer for step %d: no such step: %w", stepID, ErrNotFound)
	} else if err != nil {
		return Steer{}, fmt.Errorf("store: look up step %d to steer it: %w", stepID, err)
	}

	// NULL for the payloads the caller did not supply, through the one `nullable`
	// every other optional column in this package uses.
	// Source (S39 CR6): "" means a person — the historical and still common
	// case — and the closed set is checked HERE as well as by the column's
	// CHECK, so the caller hears the sentence rather than a constraint.
	src := orDefault(in.Source, "user")
	if !ValidSource(src) || src == "" {
		return Steer{}, fmt.Errorf("store: refusing to queue a steer for step %d: source %q is not one of user, template, automation, blueprint", stepID, in.Source)
	}
	res, err := tx.Exec(`
		INSERT INTO step_steer (step_id, text, route, reasoning_effort, state, created_at, source, skills, attachments)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		stepID, nullable(text), nullable(route), nullable(effort), string(SteerPending), s.Now(), src, encodeSkills(in.Skills), encodeAttachments(in.Attachments))
	if err != nil {
		return Steer{}, fmt.Errorf("store: queue a steer for step %d: %w", stepID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Steer{}, fmt.Errorf("store: read the id of the steer queued for step %d: %w", stepID, err)
	}
	if err := tx.Commit(); err != nil {
		return Steer{}, fmt.Errorf("store: commit the steer queued for step %d: %w", stepID, err)
	}

	// Read back through the reader pool, the way Ask does: the row a caller gets
	// is the row that is on disk, defaults and all, not what this function
	// believes it wrote.
	return s.steerByID(id, fmt.Sprintf("store: read back the steer queued for step %d", stepID))
}

// PendingSteers is the drain: the steers of ONE step that the model has not read
// yet, in the order the person queued them.
//
// # The contract
//
//   - `pending` only. A delivered steer is already in the transcript, and handing
//     it back would inject the same instruction twice; a cancelled one is the
//     person's own withdrawal, and delivering it would override them.
//   - This step only. P33: everything steer-related is keyed by stepId, never by
//     loop, so nothing here assumes one running step per loop.
//   - Arrival order (ORDER BY id). Two steers queued in one turn are delivered in
//     the order they were typed (brief §6 T4), and `id` is the honest clock for
//     that — a wall clock shared to the millisecond is not.
//   - The reader pool, never the writer (O6, I6): this runs before every model
//     call, and a drain that queued behind the writer would stall the daemon's
//     own step.
//
// An empty queue is an empty answer, not an error: it is the common case.
func (s *Store) PendingSteers(stepID int64) ([]Steer, error) {
	rows, err := s.r.Query(`SELECT `+steerColumns+`
		FROM step_steer WHERE step_id = ? AND state = ? ORDER BY id`,
		stepID, string(SteerPending))
	if err != nil {
		return nil, fmt.Errorf("store: read the pending steers of step %d: %w", stepID, err)
	}
	defer rows.Close()

	var out []Steer
	for rows.Next() {
		st, err := scanSteer(rows)
		if err != nil {
			return nil, fmt.Errorf("store: read the pending steers of step %d: %w", stepID, err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the pending steers of step %d: %w", stepID, err)
	}
	return out, nil
}

// ListSteers is `step.steers`: EVERY steer of one step — pending, delivered and
// cancelled — in the order they were queued.
//
// PendingSteers is the drain and must stay blind to delivered rows (handing one
// back would inject the same instruction twice); this is the READ a person makes
// when they want to see what they have asked and what became of it, which is the
// whole three-state row including `delivered_at` and `delivered_iteration`.
// Cancelled rows appear because the person withdrew them, and a history that
// silently loses a withdrawal is not a history.
func (s *Store) ListSteers(stepID int64) ([]Steer, error) {
	rows, err := s.r.Query(`SELECT `+steerColumns+`
		FROM step_steer WHERE step_id = ? ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: list the steers of step %d: %w", stepID, err)
	}
	defer rows.Close()

	var out []Steer
	for rows.Next() {
		st, err := scanSteer(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list the steers of step %d: %w", stepID, err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list the steers of step %d: %w", stepID, err)
	}
	return out, nil
}

// steerByID reads one row by id through the reader pool, with a caller-supplied
// prefix so the failure names what the caller was trying to do.
func (s *Store) steerByID(id int64, what string) (Steer, error) {
	st, err := scanSteer(s.r.QueryRow(`SELECT `+steerColumns+` FROM step_steer WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Steer{}, fmt.Errorf("%s: no such steer: %w", what, ErrNotFound)
	}
	if err != nil {
		return Steer{}, fmt.Errorf("%s: steer %d: %w", what, id, err)
	}
	return st, nil
}

// ReadSteer reads one steer row by id. The cancel door uses it to learn which
// STEP a row belonged to: the cancel params name only a steerId, and the
// `step.steer.cancelled` event is a step event — an event without a stepId
// belongs to no thread (D1).
func (s *Store) ReadSteer(id int64) (Steer, error) {
	return s.steerByID(id, "read steer")
}

// CancelSteerResult is the answer to "may this steer be withdrawn".
//
// A result, never an error (DB19, and I4 for this slice): "already delivered" and
// "the disk is full" need different handling, and one `error` return makes the
// caller guess which it got. This is also the case the UI actually meets — the
// person presses cancel a moment after the turn boundary passed.
type CancelSteerResult struct {
	OK bool

	// SteerID is echoed so a caller that cancelled several can match the answer to
	// the request without depending on the order.
	SteerID int64

	// Found separates the third state (P31): refused-because-delivered,
	// refused-because-cancelled and there-was-no-such-row are THREE answers, and
	// collapsing the last into the others is how a typo'd id reads as "someone got
	// there first".
	Found bool

	// State is the state found: `cancelled` on success (brief §5 answers
	// `step.steer.cancel` with `{state}`), and whatever the row actually said when
	// refused. Empty when there was no row — no state is invented for a row that
	// does not exist.
	State SteerState

	// Reason is the whole refusal in a sentence, naming the row and the state
	// found (AGENTS §5). Empty on success.
	Reason string
}

// CancelSteer withdraws a queued steer — a `pending` one, and only ever a
// `pending` one.
//
// # The contract
//
//   - pending → cancelled, in one statement guarded by `state = 'pending'`, so
//     the flip cannot happen twice even if two calls race: the second meets its
//     own precondition gone and is refused with the state it found.
//   - delivered and cancelled are terminal, and refusing them is a result naming
//     the state found. A delivered steer's text is already in a checkpointed
//     transcript — cancelling it would be un-sending a message the model has
//     already read — and a row that says cancelled was withdrawn once, which a
//     second cancel changes nothing about and a silent success would misreport.
//   - The delivery columns stay NULL. 0021's delivery CHECK is what makes a
//     cancelled steer unable to read as having been delivered.
//
// # The crash argument
//
// A cancel commits on its own, so a crash cannot leave a steer half-cancelled.
// The interesting instant is the one this method cannot win: the runtime has
// drained the row and the person cancels before the checkpoint commits. The
// cancel lands first — one writer, so the checkpoint's transaction queues behind
// it — and CheckpointWithSteers then finds the row cancelled, commits the
// transcript anyway, and reports the row as not delivered. The person's
// withdrawal is what stands.
func (s *Store) CancelSteer(id int64) (CancelSteerResult, error) {
	res := CancelSteerResult{SteerID: id}

	tx, err := s.w.Begin()
	if err != nil {
		return res, fmt.Errorf("store: cancel steer %d: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	var state string
	err = tx.QueryRow(`SELECT state FROM step_steer WHERE id = ?`, id).Scan(&state)
	if err == sql.ErrNoRows {
		res.Reason = fmt.Sprintf("store: cannot cancel steer %d: no such steer", id)
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("store: look up steer %d to cancel it: %w", id, err)
	}
	res.Found = true
	if SteerState(state) != SteerPending {
		res.State = SteerState(state)
		res.Reason = fmt.Sprintf("store: refusing to cancel steer %d: it is %s, and only a pending steer can be "+
			"cancelled", id, state)
		return res, nil
	}

	// The guard is in the statement, not in the read above. This is the one
	// writer, so nothing can interleave — but a flip whose safety depends on a
	// prior read is a flip that reads as safe and is not, and the next reader of
	// this contract is the person debugging why a steer moved twice.
	upd, err := tx.Exec(`UPDATE step_steer SET state = ? WHERE id = ? AND state = ?`,
		string(SteerCancelled), id, string(SteerPending))
	if err != nil {
		return res, fmt.Errorf("store: cancel steer %d: %w", id, err)
	}
	n, err := upd.RowsAffected()
	if err != nil {
		return res, fmt.Errorf("store: cancel steer %d: %w", id, err)
	}
	if n != 1 {
		// Unreachable while this is the only writer; said out loud rather than
		// assumed, because the alternative is reporting success on a row that did
		// not move.
		var now string
		if err := tx.QueryRow(`SELECT state FROM step_steer WHERE id = ?`, id).Scan(&now); err != nil {
			return res, fmt.Errorf("store: re-read steer %d after the cancel wrote nothing: %w", id, err)
		}
		res.State = SteerState(now)
		res.Reason = fmt.Sprintf("store: refusing to cancel steer %d: it is %s, and only a pending steer can be "+
			"cancelled", id, now)
		return res, nil
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("store: commit the cancellation of steer %d: %w", id, err)
	}
	res.OK = true
	res.State = SteerCancelled
	return res, nil
}

// CheckpointSteersResult is what CheckpointWithSteers actually achieved.
//
// A result beside the `error`, not instead of one (DB19): the transaction either
// committed or it did not — that is infrastructure, and an `error` carries it —
// but WHICH steers the commit delivered is a domain answer the caller has to see,
// because the runtime turns it into events.
type CheckpointSteersResult struct {
	// OK is true when the transaction committed AND every steer the caller named
	// was among the rows it delivered. A committed checkpoint that delivered less
	// than it was asked is NOT OK: the caller has to know, because the alternative
	// is a steer that reads as delivered and was never read.
	OK bool

	// Delivered names the rows this transaction flipped from pending to delivered,
	// in the order the caller named them. These are the steers
	// `step.steer.delivered` is emitted for.
	Delivered []int64

	// NotDelivered carries the named rows this transaction did NOT deliver, each
	// with the state actually found — the honest answer to the race where the
	// person cancelled after the drain, and to a replay that names a steer an
	// earlier process already delivered.
	NotDelivered []Steer

	// Reason names the shortfall when there is one, and every row in it.
	Reason string
}

// CheckpointWithSteers writes a step's transcript and marks the steers it
// contains delivered, in ONE transaction.
//
// It is SaveRunningCheckpoint's shape (checkpoint.go:13) with the delivery folded
// into the same transaction, and the deliberate difference is the whole of LW5:
// SaveRunningCheckpoint persists a transcript that contains no steer text, so its
// writes can stand apart; a checkpoint that DOES contain steers cannot have its
// transcript committed by one statement and its steer state by another, because a
// crash between them is the double-delivery case.
//
// # The contract
//
//   - msgs is the WHOLE transcript, as in SaveRunningCheckpoint: the rows are
//     replaced (DELETE then re-INSERT at seq 1..n), not appended, so seq stays
//     contiguous, which is what every reader after this one assumes.
//   - deliveredIDs are the steers whose text msgs now carries — normally the ids
//     PendingSteers just returned. Each must be a steer THIS step queued: naming
//     another step's row, or one that does not exist, is an `error` and NOTHING is
//     written, because a caller that cannot say what it drained cannot say what it
//     persisted either.
//   - A named steer that has LEFT pending (cancelled after the drain, or delivered
//     by a process that already ran this instant) does not fail the transaction —
//     its text is already in msgs, and refusing the checkpoint would lose the turn
//     it belongs to. It is reported in NotDelivered with the state found.
//   - iteration is recorded on each delivered row. It is what
//     `step.steer.delivered` announces and what a replay compares, so it is
//     written once and never rewritten: a second checkpoint naming the same steer
//     leaves the original timestamp and iteration standing.
//   - The step must be `running`, and the refusal names the status found — the
//     same guard SaveRunningCheckpoint has (checkpoint.go:46), and for the same
//     reason: this is the write a stale goroutine still holding a step id would
//     otherwise land on a finished step.
//   - The caller's ctx begins the transaction, so a step whose deadline passed
//     mid-turn does not have its checkpoint written behind its own back.
//   - With no ids this is a plain checkpoint, deliberately: the runtime's steer
//     path has ONE persistence call, not one for "steers" and one for "none", and
//     two calls that must agree is where a crash gets its gap.
//
// # The crash argument, at the statement level
//
// Everything below runs inside one transaction on the single writer: the
// transcript DELETE, the re-INSERT, and each steer's flip. Returning an error
// after any of them rolls all of it back — the test that injects a bad statement
// after the transcript write asserts NEITHER half happened, which is the property,
// not a description of it. There is therefore no instant at which the transcript
// contains a steer while the row says pending, and none at which the row says
// delivered while the transcript lacks it: the two states S7's replay could not
// tell apart are not reachable from this file.
func (s *Store) CheckpointWithSteers(ctx context.Context, stepID int64, msgs []model.Message,
	snapshot json.RawMessage, deliveredIDs []int64, iteration int) (CheckpointSteersResult, error) {
	if verdict := model.ValidateTranscript(msgs); !verdict.OK {
		return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint with steers of step %d: %s",
			stepID, verdict.Reason)
	}
	if iteration < 0 {
		return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint with steers of step %d: iteration %d is "+
			"not an iteration the step reached", stepID, iteration)
	}
	// The saved-request half is required, not optional (S31 T4, LW6): the
	// envelope carries the step's Request — including a route/effort a steer
	// just overrode — and LoadResume replays from it. A checkpoint that wrote
	// the transcript but left the envelope stale would make recovery answer
	// with the PRE-steer route: the override announced, persisted nowhere.
	if !json.Valid(snapshot) {
		return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint with steers of step %d: the saved request "+
			"envelope is not valid JSON, and recovery replays from it", stepID)
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return CheckpointSteersResult{}, fmt.Errorf("store: begin the checkpoint with steers of step %d: %w",
			stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// The step must still be running, and the refusal names the status found
	// (AGENTS §5) rather than saying only "not running".
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM step WHERE id = ?`, stepID).Scan(&status)
	if err == sql.ErrNoRows {
		return CheckpointSteersResult{}, fmt.Errorf("store: cannot checkpoint step %d with steers: no such step: %w",
			stepID, ErrNotFound)
	}
	if err != nil {
		return CheckpointSteersResult{}, fmt.Errorf("store: look up step %d to checkpoint it with steers: %w",
			stepID, err)
	}
	if status != string(StepRunning) {
		return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint step %d with steers refused: it is %s, "+
			"and a checkpoint replaces the transcript of a running step", stepID, status)
	}

	// The transcript half, in SaveRunningCheckpoint's exact shape: replace, never
	// append, so a checkpoint is a whole state rather than a delta someone has to
	// replay to read.
	if _, err := tx.ExecContext(ctx, `DELETE FROM step_message WHERE step_id=?`, stepID); err != nil {
		return CheckpointSteersResult{}, fmt.Errorf("store: replace the transcript of step %d: %w", stepID, err)
	}
	now := s.Now()
	for i, msg := range msgs {
		content, err := marshalBlocks(msg)
		if err != nil {
			return CheckpointSteersResult{}, fmt.Errorf("store: encode message %d of step %d: %w", i, stepID, err)
		}
		prov, err := marshalProvenance(msg)
		if err != nil {
			return CheckpointSteersResult{}, fmt.Errorf("store: encode message %d of step %d: %w", i, stepID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO step_message (step_id, seq, format_version, role, content, provenance, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			stepID, i+1, model.FormatVersion, string(msg.Role), content, prov, now); err != nil {
			return CheckpointSteersResult{}, fmt.Errorf("store: write message %d of step %d: %w", i, stepID, err)
		}
	}

	// The saved-request half, in the same transaction and under the same
	// running guard the transcript writes under (SaveRunningCheckpoint's own
	// shape): the envelope and the transcript describe the same instant, or
	// neither does.
	upd, err := tx.ExecContext(ctx,
		`UPDATE step SET context_manifest=? WHERE id=? AND status='running'`, string(snapshot), stepID)
	if err != nil {
		return CheckpointSteersResult{}, fmt.Errorf("store: save the request envelope of step %d with its steers: %w",
			stepID, err)
	}
	if n, err := upd.RowsAffected(); err != nil {
		return CheckpointSteersResult{}, fmt.Errorf("store: count the envelope write of step %d: %w", stepID, err)
	} else if n != 1 {
		return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint step %d with steers refused: the step "+
			"stopped being a running step while this checkpoint was being written", stepID)
	}

	// The delivery half, in the same transaction. One instant for the whole
	// checkpoint (`now`, taken above): every steer this commit delivered reached
	// the model at the same moment, and two timestamps in one transaction would be
	// two claims about when it happened.
	//
	// Ids are deduplicated first. A caller that named one row twice cannot mean
	// "deliver it twice" — there is no such state — and reporting the second
	// naming as a refusal would be reporting the caller's typo as a domain event.
	res := CheckpointSteersResult{OK: true}
	seen := make(map[int64]bool, len(deliveredIDs))
	for _, id := range deliveredIDs {
		if seen[id] {
			continue
		}
		seen[id] = true

		st, err := scanSteer(tx.QueryRowContext(ctx, `SELECT `+steerColumns+`
			FROM step_steer WHERE id = ?`, id))
		if err == sql.ErrNoRows {
			// The caller named a row it does not own. Half a checkpoint on that is
			// exactly what this function exists to make impossible, so the whole
			// thing rolls back and says which name was wrong.
			return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint of step %d names steer %d, which does "+
				"not exist: a steer is delivered only by the step that queued it", stepID, id)
		}
		if err != nil {
			return CheckpointSteersResult{}, fmt.Errorf("store: look up steer %d to deliver it in the checkpoint "+
				"of step %d: %w", id, stepID, err)
		}
		if st.StepID != stepID {
			return CheckpointSteersResult{}, fmt.Errorf("store: checkpoint of step %d names steer %d, which belongs "+
				"to step %d: steers are delivered by the step that queued them (P33)", stepID, id, st.StepID)
		}
		if st.State != SteerPending {
			// Not this transaction's business to refuse the checkpoint over: the
			// text is already in msgs. Its business to SAY so, in the result.
			res.OK = false
			res.NotDelivered = append(res.NotDelivered, st)
			continue
		}

		upd, err := tx.ExecContext(ctx, `
			UPDATE step_steer SET state = ?, delivered_at = ?, delivered_iteration = ?
			WHERE id = ? AND state = ?`,
			string(SteerDelivered), now, iteration, id, string(SteerPending))
		if err != nil {
			return CheckpointSteersResult{}, fmt.Errorf("store: deliver steer %d in the checkpoint of step %d: %w",
				id, stepID, err)
		}
		n, err := upd.RowsAffected()
		if err != nil {
			return CheckpointSteersResult{}, fmt.Errorf("store: deliver steer %d in the checkpoint of step %d: %w",
				id, stepID, err)
		}
		if n != 1 {
			// As in CancelSteer: unreachable on the single writer, and reported
			// rather than assumed, because reporting a delivery that did not happen
			// is the failure this whole file is written against.
			found, err := scanSteer(tx.QueryRowContext(ctx, `SELECT `+steerColumns+`
				FROM step_steer WHERE id = ?`, id))
			if err != nil {
				return CheckpointSteersResult{}, fmt.Errorf("store: re-read steer %d after the delivery wrote "+
					"nothing: %w", id, err)
			}
			res.OK = false
			res.NotDelivered = append(res.NotDelivered, found)
			continue
		}
		res.Delivered = append(res.Delivered, id)
	}

	if len(res.NotDelivered) > 0 {
		parts := make([]string, 0, len(res.NotDelivered))
		for _, st := range res.NotDelivered {
			parts = append(parts, fmt.Sprintf("steer %d is %s", st.ID, st.State))
		}
		res.Reason = fmt.Sprintf("store: the checkpoint of step %d committed without delivering %d of the %d "+
			"steers it named: %s", stepID, len(res.NotDelivered), len(seen), strings.Join(parts, "; "))
	}

	if err := tx.Commit(); err != nil {
		return CheckpointSteersResult{}, fmt.Errorf("store: commit the checkpoint with steers of step %d: %w",
			stepID, err)
	}
	return res, nil
}
