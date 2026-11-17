package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// runs.go is S39's store layer for blueprint runs (CR2, CR9). A loop is a
// conversation that can run blueprints one after another without ending;
// loop_blueprint stays ONE row per loop meaning *the current run's pin and
// live position* (T0-Q2: seven statements, recovery reads steps not pins),
// and loop_run holds what a re-key would have: one immutable copy of each
// run's pinned YAML and, when the run ends, a snapshot of its runtime.
// BeginRun is the one transaction that claims the loop and re-pins it;
// EndRun is the one that closes it. The run's `state` never stores the
// loop's waiting/blocked condition (DB6 — no stored flag that can disagree
// with loop.status), and costs are never stored: they are a P32 floor query
// over steps, grouped by run_seq (T1's ALTER column), per step or for the
// whole loop.

// Run is one blueprint run inside a loop (CR2).
type Run struct {
	LoopID           string `json:"loopId"`
	RunSeq           int    `json:"runSeq"`
	Blueprint        string `json:"blueprint"`
	BlueprintVersion int    `json:"blueprintVersion"`
	Content          string `json:"-"` // the pinned YAML; never on the wire from here
	RuntimeJSON      string `json:"-"` // snapshot taken when the run ended
	State            string `json:"state"`
	StartedByKind    string `json:"startedByKind"` // user | agent | trigger
	StartedByID      string `json:"startedById"`
	KickoffMessageID *int64 `json:"kickoffMessageId,omitempty"`
	GoalID           string `json:"goalId,omitempty"`
	GoalVersion      int    `json:"goalVersion,omitempty"`
	TaskID           string `json:"taskId,omitempty"`
	StartedAt        string `json:"startedAt"`
	EndedAt          string `json:"endedAt,omitempty"`
}

// NewRun is the caller's request to start a run.
type NewRun struct {
	Blueprint     string
	Version       int
	Content       string
	StartedByKind string
	StartedByID   string
	// KickoffMessageID points at the chat message that IS the run's first
	// prompt (CR6/CR7); nil for a run whose kickoff has no message yet.
	KickoffMessageID *int64
	GoalID           string
	GoalVersion      int
	TaskID           string
}

// RunCost is the P32 floor: USDAtLeast sums every measured number; Exact is
// true only when every step is `known`. A `partial` or `unknown` step may
// carry a floor in cost_usd (encodeCost) — summing it is honest precisely
// because it is a floor; the Partial/Unknown counts are how the reader sees
// what the number does not know. Zero is never silently substituted for a
// missing number.
type RunCost struct {
	USDAtLeast float64 `json:"usdAtLeast"`
	Exact      bool    `json:"exact"`
	Partial    int     `json:"partial"`
	Unknown    int     `json:"unknown"`
	Steps      int     `json:"steps"`
}

var ErrLoopNotIdle = errors.New("store: the loop is not idle")
var ErrRunNotRunning = errors.New("store: the run is not running")

// BeginRun claims an idle loop, inserts its next run, and re-pins the loop
// to the new blueprint — all in ONE transaction, so there is no moment in
// which the loop runs one blueprint while its run row says another (the
// claim is the guard: zero rows updated on a non-idle loop means somebody
// else already took it). runtimeJSON is the fresh walk position for the new
// pin; it must not be empty.
func (s *Store) BeginRun(loopID string, in NewRun, runtimeJSON string) (Run, error) {
	if in.Blueprint == "" || in.Content == "" || runtimeJSON == "" {
		return Run{}, fmt.Errorf("store: begin run needs blueprint, content and a position")
	}
	switch in.StartedByKind {
	case "user", "agent", "trigger", "event": // S40 T0-Q2: T1 rebuilt the CHECK for 'event'; this is the Go half of that admission
	case "scheduler": // S27 T2 CN15: the Go half of the widened CHECK (20261105025314 rebuilt loop_run)
	default:
		return Run{}, fmt.Errorf("store: begin run %s: unknown started_by_kind %q", loopID, in.StartedByKind)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	now := s.Now()

	// 1. The claim. idle → running, atomically; anything else refuses.
	res, err := tx.Exec(`UPDATE loop SET status='running', updated_at=? WHERE id=? AND status='idle'`, now, loopID)
	if err != nil {
		return Run{}, fmt.Errorf("store: claim idle loop %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Run{}, fmt.Errorf("store: claim idle loop %s: %w", loopID, err)
	}
	if n == 0 {
		var status string
		if err := tx.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Run{}, fmt.Errorf("store: begin run in %s: %w", loopID, ErrNotFound)
			}
			return Run{}, fmt.Errorf("store: read the status of %s: %w", loopID, err)
		}
		return Run{}, fmt.Errorf("store: begin run in %s: loop is %s: %w", loopID, status, ErrLoopNotIdle)
	}

	// 2. The invariant the claim cannot enforce alone: an idle loop must not
	// carry a still-running run — that is data corruption, said plainly.
	var live int
	var liveSeq int
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(MAX(run_seq),0) FROM loop_run WHERE loop_id=? AND state='running'`, loopID).Scan(&live, &liveSeq); err != nil {
		return Run{}, fmt.Errorf("store: look for a live run in %s: %w", loopID, err)
	}
	if live > 0 {
		return Run{}, fmt.Errorf("store: loop %s is idle but run %d is still running", loopID, liveSeq)
	}

	// 3. The next run_seq.
	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(run_seq),0)+1 FROM loop_run WHERE loop_id=?`, loopID).Scan(&next); err != nil {
		return Run{}, fmt.Errorf("store: number the next run of %s: %w", loopID, err)
	}

	// 4. The run row — its own immutable pinned copy.
	var gvArg any
	if in.GoalVersion > 0 {
		gvArg = in.GoalVersion // 0 means NONE, and NULL is how it stays (never version 0)
	}
	if _, err := tx.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_by_id,kickoff_message_id,goal_id,goal_version,task_id,started_at) VALUES (?,?,?,?,?,'running',?,?,?,?,?,?,?)`,
		loopID, next, in.Blueprint, in.Version, in.Content, in.StartedByKind, in.StartedByID,
		in.KickoffMessageID, nullable(in.GoalID), gvArg, nullable(in.TaskID), now); err != nil {
		return Run{}, fmt.Errorf("store: record run %d of %s: %w", next, loopID, err)
	}

	// 5. The re-pin — the same transaction, so pin and run can never disagree.
	pin, err := tx.Exec(`UPDATE loop_blueprint SET blueprint_id=?, blueprint_version=?, content=?, runtime_json=?, updated_at=? WHERE loop_id=?`,
		in.Blueprint, in.Version, in.Content, runtimeJSON, now, loopID)
	if err != nil {
		return Run{}, fmt.Errorf("store: pin run %d of %s: %w", next, loopID, err)
	}
	if err := affectPinnedOne(pin, loopID, "pin run"); err != nil {
		return Run{}, err
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("store: begin run %d of %s: %w", next, loopID, err)
	}
	return s.ReadRun(loopID, next)
}

// EndRun closes a running run with its terminal state and its runtime
// snapshot. The AND state='running' guard makes a double end harmless: the
// second call answers ErrRunNotRunning and changes nothing (callers may
// ignore it — a run closed twice was closed once).
func (s *Store) EndRun(loopID string, runSeq int, state, runtimeJSON string) error {
	switch state {
	case "done", "failed", "cancelled":
	default:
		return fmt.Errorf("store: end run %d of %s: state %q is not one of done, failed, cancelled", runSeq, loopID, state)
	}
	res, err := s.w.Exec(`UPDATE loop_run SET state=?, ended_at=?, runtime_json=? WHERE loop_id=? AND run_seq=? AND state='running'`,
		state, s.Now(), runtimeJSON, loopID, runSeq)
	if err != nil {
		return fmt.Errorf("store: end run %d of %s: %w", runSeq, loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: end run %d of %s: %w", runSeq, loopID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: run %d of %s: %w", runSeq, loopID, ErrRunNotRunning)
	}
	return nil
}

// CloseIdleLoop moves idle -> done (S39 T4, loop.close's door). Claimed
// from: any other status is ErrLoopNotIdle — closing a running loop would
// end a walk mid-stride, and closing a finished one is a no-op dressed as
// a command. The UPDATE's WHERE status='idle' is the claim (same shape as
// BeginRun's): the status cannot move between the check and the write.
func (s *Store) CloseIdleLoop(loopID string) error {
	res, err := s.w.Exec(`UPDATE loop SET status='done', updated_at=? WHERE id=? AND status='idle'`, s.Now(), loopID)
	if err != nil {
		return fmt.Errorf("store: close idle loop %s: %w", loopID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: close idle loop %s: %w", loopID, err)
	}
	if n == 0 {
		var status string
		if err := s.r.QueryRow(`SELECT status FROM loop WHERE id=?`, loopID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("store: close %s: %w", loopID, ErrNotFound)
			}
			return fmt.Errorf("store: read the status of %s: %w", loopID, err)
		}
		return fmt.Errorf("store: close %s: loop is %s: %w", loopID, status, ErrLoopNotIdle)
	}
	return nil
}

// ReadRun reads one run by (loop, seq).
func (s *Store) ReadRun(loopID string, runSeq int) (Run, error) {
	row := s.r.QueryRow(`SELECT loop_id,run_seq,blueprint,blueprint_version,content,runtime_json,state,started_by_kind,started_by_id,kickoff_message_id,goal_id,goal_version,task_id,started_at,ended_at FROM loop_run WHERE loop_id=? AND run_seq=?`, loopID, runSeq)
	return scanRun(row)
}

// CurrentRun reads the run with the highest run_seq — the loop's live run,
// the owner of the loop_blueprint row's meaning.
func (s *Store) CurrentRun(loopID string) (Run, error) {
	row := s.r.QueryRow(`SELECT loop_id,run_seq,blueprint,blueprint_version,content,runtime_json,state,started_by_kind,started_by_id,kickoff_message_id,goal_id,goal_version,task_id,started_at,ended_at FROM loop_run WHERE loop_id=? ORDER BY run_seq DESC LIMIT 1`, loopID)
	return scanRun(row)
}

// ListRuns reads every run of a loop, oldest first. Empty is an empty
// slice, never nil — a reader rendering a timeline should not branch.
func (s *Store) ListRuns(loopID string) ([]Run, error) {
	rows, err := s.r.Query(`SELECT loop_id,run_seq,blueprint,blueprint_version,content,runtime_json,state,started_by_kind,started_by_id,kickoff_message_id,goal_id,goal_version,task_id,started_at,ended_at FROM loop_run WHERE loop_id=? ORDER BY run_seq`, loopID)
	if err != nil {
		return nil, fmt.Errorf("store: list runs of %s: %w", loopID, err)
	}
	defer rows.Close()
	runs := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list runs of %s: %w", loopID, err)
	}
	return runs, nil
}

// SetStepRun tags a step with the run whose cost it belongs to. Steps
// written before a run existed (and T1's backfill) carry their own value;
// the engine tags every step it starts from T8 on.
// SetRunKickoff links the run's first message (CR6): the chat_message row
// that IS the run's opening words. Zero rows is ErrNotFound — a kickoff
// cannot be filed against a run that does not exist.
func (s *Store) SetRunKickoff(loopID string, runSeq int, messageID int64) error {
	res, err := s.w.Exec(`UPDATE loop_run SET kickoff_message_id=? WHERE loop_id=? AND run_seq=?`, messageID, loopID, runSeq)
	if err != nil {
		return fmt.Errorf("store: link kickoff of run %d in %s: %w", runSeq, loopID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: link kickoff of run %d in %s: %w", runSeq, loopID, err)
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetStepRun(stepID int64, runSeq int) error {
	res, err := s.w.Exec(`UPDATE step SET run_seq=? WHERE id=?`, runSeq, stepID)
	if err != nil {
		return fmt.Errorf("store: tag step %d with run %d: %w", stepID, runSeq, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: tag step %d with run %d: %w", stepID, runSeq, err)
	}
	if n == 0 {
		return fmt.Errorf("store: step %d: %w", stepID, ErrNotFound)
	}
	return nil
}

// RunCost is the P32 floor for one run's steps (the ones tagged run_seq).
func (s *Store) RunCost(loopID string, runSeq int) (RunCost, error) {
	return s.costQuery(loopID, " AND s.run_seq = ?", runSeq)
}

// LoopCost is the floor across every step of the loop — every run, and the
// steps that carry no run tag (old data keeps costing honestly).
func (s *Store) LoopCost(loopID string) (RunCost, error) {
	return s.costQuery(loopID, "")
}

// costQuery is the one cost statement. The JOIN speaks component_root —
// the live column name since 0035 (T0-Q4: the immutable CREATE TABLE still
// says project_root; the live shape does not).
func (s *Store) costQuery(loopID string, extra string, args ...any) (RunCost, error) {
	var c RunCost
	q := `SELECT COALESCE(SUM(s.cost_usd),0),
	       COALESCE(SUM(CASE WHEN s.cost_state IN ('partial') THEN 1 ELSE 0 END),0),
	       COALESCE(SUM(CASE WHEN s.cost_state IN ('unknown') THEN 1 ELSE 0 END),0),
	       COUNT(*)
	FROM step s JOIN loop l ON l.slug = s.loop_slug AND l.component_root = s.component_root
	WHERE l.id = ?` + extra
	all := append([]any{loopID}, args...)
	if err := s.r.QueryRow(q, all...).Scan(&c.USDAtLeast, &c.Partial, &c.Unknown, &c.Steps); err != nil {
		return RunCost{}, fmt.Errorf("store: cost of loop %s: %w", loopID, err)
	}
	c.Exact = c.Partial == 0 && c.Unknown == 0
	return c, nil
}

// scanRun reads one run row; nullable columns ride sql.Null* so a NULL
// never becomes the empty string by accident.
func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var (
		r         Run
		runtime   sql.NullString
		kickoff   sql.NullInt64
		goalID    sql.NullString
		goalVer   sql.NullInt64
		taskID    sql.NullString
		startedBy sql.NullString
		endedAt   sql.NullString
	)
	err := row.Scan(&r.LoopID, &r.RunSeq, &r.Blueprint, &r.BlueprintVersion, &r.Content, &runtime,
		&r.State, &r.StartedByKind, &startedBy, &kickoff, &goalID, &goalVer, &taskID, &r.StartedAt, &endedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("store: run: %w", ErrNotFound)
	}
	if err != nil {
		return Run{}, fmt.Errorf("store: read run: %w", err)
	}
	r.RuntimeJSON = runtime.String
	r.StartedByID = startedBy.String
	if kickoff.Valid {
		v := kickoff.Int64
		r.KickoffMessageID = &v
	}
	r.GoalID = goalID.String
	if goalVer.Valid {
		r.GoalVersion = int(goalVer.Int64)
	}
	r.TaskID = taskID.String
	r.EndedAt = endedAt.String
	return r, nil
}
