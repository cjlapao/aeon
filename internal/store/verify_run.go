// verify_run.go — the evidence (VG6): the draft a run lives in while
// unfinished (memory only, never a store row), the one-transaction finish
// that makes a run visible with all its results, and the reads. The
// command text is SNAPSHOTted on each result so a later edit or archive of
// the gate never rewrites this history. The types live in this file with
// the code that serves them; the shared probes are in verify.go.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// VerifyRun is one row of verify_run — one finished verification (VG6). A
// run row is visible only finished: it and its results land in one
// transaction, so a store can never be read with a run and no results, or
// with some. State is the aggregated outcome (T4 computes it: any required
// fail ⇒ fail, else any required unverified ⇒ unverified, else pass — an
// advisory never changes it); the per-gate detail is on the results.
type VerifyRun struct {
	ID            string
	LoopSlug      string
	ComponentRoot string
	ComponentID   string
	Task          string
	Position      int
	Attempt       int
	HeadSHA       string
	Trigger       string // "loop" | "manual"
	ActorKind     string
	ActorID       string
	StartedAt     string
	EndedAt       *string
	State         string // "pass" | "fail" | "unverified"
	Reason        string
	Summary       string
	Digest        string
}

// VerifyRunResult is one row of verify_run_result — one gate of a run.
// Command is the SNAPSHOT: the text as it read when the gate ran, so a later
// edit or archive of the gate never rewrites this row's story (VG6). Tails
// are at most 2 KiB each and exist only for non-pass results (the writer's
// rule, T4; the CHECK is the backstop).
type VerifyRunResult struct {
	Seq          int
	CommandID    *string
	Name         string
	Requirement  string
	Command      string
	ExitCode     *int
	Signal       string
	TimedOut     bool
	WallMS       int
	StdoutBytes  int
	StderrBytes  int
	Truncated    bool
	StdoutSHA256 string
	StderrSHA256 string
	StdoutTail   string
	StderrTail   string
	State        string // "pass" | "fail" | "unverified" | "skipped"
	Reason       string
}

// NewVerifyRun is the caller's description of a run BEFORE it is visible.
// The store writes nothing on this call: a run is visible only finished,
// and a crash between this call and FinishVerifyRun leaves NO run — the
// torn-evidence test's contract.
type NewVerifyRun struct {
	LoopSlug      string
	ComponentRoot string
	ComponentID   string
	Task          string
	Position      int
	Attempt       int
	HeadSHA       string
	Trigger       string // "loop" | "manual"
	Actor         Actor
	StartedAt     time.Time
}

// VerifyRunDraft is a run in the time between CreateVerifyRun and
// FinishVerifyRun — the one place an unfinished run may exist: in memory,
// never in the store. Results append in order; the seq is assigned by the
// append (0-based, the run's own numbering).
type VerifyRunDraft struct {
	store    *Store
	run      VerifyRun
	results  []VerifyRunResult
	finished bool
}

// AppendVerifyResult adds one gate's result to the draft (seq assigned in
// append order). The closed vocabularies are checked here, at the only
// place an unfinished value can exist.
func (d *VerifyRunDraft) AppendVerifyResult(r VerifyRunResult) error {
	if d == nil || d.finished {
		return errors.New("store: the run is finished; a finished run takes no more results")
	}
	if !requirementOK(r.Requirement) {
		return fmt.Errorf("store: result %d: the requirement is %q; the closed set is required|advisory", len(d.results), r.Requirement)
	}
	switch r.State {
	case "pass", "fail", "unverified", "skipped":
	default:
		return fmt.Errorf("store: result %d: the state is %q; the closed set is pass|fail|unverified|skipped", len(d.results), r.State)
	}
	if len(r.StdoutTail) > 2048 || len(r.StderrTail) > 2048 {
		return fmt.Errorf("store: result %d: a tail is over 2 KiB — the table bound", len(d.results))
	}
	r.Seq = len(d.results)
	d.results = append(d.results, r)
	return nil
}

// result is the run's description with its id minted — what
// CreateVerifyRun validates and returns, still invisible.
func (s *Store) CreateVerifyRun(a Actor, in NewVerifyRun) (*VerifyRunDraft, error) {
	if a.ID == "" {
		return nil, fmt.Errorf("store: create verify run: an anonymous actor cannot be recorded (VG6)")
	}
	if in.Trigger != "loop" && in.Trigger != "manual" {
		return nil, fmt.Errorf("store: create verify run: the trigger is %q; the closed set is loop|manual", in.Trigger)
	}
	if in.ComponentID == "" {
		return nil, fmt.Errorf("store: create verify run: a run needs the node it happened on")
	}
	exists, _, err := s.componentState(in.ComponentID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("store: create verify run: %s is not a component: %w", in.ComponentID, ErrNotFound)
	}
	run := VerifyRun{
		ID:            "vr-" + newID(),
		LoopSlug:      in.LoopSlug,
		ComponentRoot: in.ComponentRoot,
		ComponentID:   in.ComponentID,
		Task:          in.Task,
		Position:      in.Position,
		Attempt:       in.Attempt,
		HeadSHA:       in.HeadSHA,
		Trigger:       in.Trigger,
		ActorKind:     a.Kind,
		ActorID:       a.ID,
		StartedAt:     isoUTC(in.StartedAt),
	}
	return &VerifyRunDraft{store: s, run: run}, nil
}

// FinishVerifyRun makes the run visible: the run row and every result land
// in ONE transaction (VG6) — a store read sees the run finished with all
// its results, or sees nothing at all. State must be one of pass|fail|
// unverified; ended_at is written (a finished run is ended). A loop run
// that claims an already-taken (loop, root, position, attempt) slot is a
// duplicate dispatch and errors (the partial unique index is the backstop).
func (s *Store) FinishVerifyRun(d *VerifyRunDraft, state, reason, summary, digest string, endedAt time.Time) (VerifyRun, error) {
	if d == nil {
		return VerifyRun{}, fmt.Errorf("store: finish verify run: no run to finish")
	}
	if d.finished {
		return VerifyRun{}, fmt.Errorf("store: finish verify run: %s is already finished", d.run.ID)
	}
	switch state {
	case "pass", "fail", "unverified":
	default:
		return VerifyRun{}, fmt.Errorf("store: finish verify run %s: the state is %q; the closed set is pass|fail|unverified", d.run.ID, state)
	}
	d.finished = true
	ended := isoUTC(endedAt)
	d.run.EndedAt = &ended
	d.run.State = state
	d.run.Reason = reason
	d.run.Summary = summary
	d.run.Digest = digest

	tx, err := d.store.w.Begin()
	if err != nil {
		return VerifyRun{}, fmt.Errorf("store: finish verify run %s: %w", d.run.ID, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO verify_run (id, loop_slug, component_root, component_id, task, position, attempt, head_sha, trigger, actor_kind, actor_id, started_at, ended_at, state, reason, summary, digest)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.run.ID, d.run.LoopSlug, d.run.ComponentRoot, d.run.ComponentID, d.run.Task, d.run.Position, d.run.Attempt,
		d.run.HeadSHA, d.run.Trigger, d.run.ActorKind, d.run.ActorID, d.run.StartedAt, d.run.EndedAt,
		d.run.State, d.run.Reason, d.run.Summary, d.run.Digest); err != nil {
		return VerifyRun{}, fmt.Errorf("store: finish verify run %s: %w", d.run.ID, err)
	}
	for _, r := range d.results {
		if _, err := tx.Exec(`INSERT INTO verify_run_result (run_id, seq, command_id, name, requirement, command, exit_code, signal, timed_out, wall_ms, stdout_bytes, stderr_bytes, truncated, stdout_sha256, stderr_sha256, stdout_tail, stderr_tail, state, reason)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.run.ID, r.Seq, nullableStr(r.CommandID), r.Name, r.Requirement, r.Command, nullableInt(r.ExitCode),
			r.Signal, boolInt(r.TimedOut), r.WallMS, r.StdoutBytes, r.StderrBytes, boolInt(r.Truncated),
			r.StdoutSHA256, r.StderrSHA256, r.StdoutTail, r.StderrTail, r.State, r.Reason); err != nil {
			return VerifyRun{}, fmt.Errorf("store: finish verify run %s: %w", d.run.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return VerifyRun{}, fmt.Errorf("store: finish verify run %s: %w", d.run.ID, err)
	}
	return d.run, nil
}

// VerifyRunFilter is the runs list read (verify.runs).
type VerifyRunFilter struct {
	ComponentRoot string
	LoopSlug      string
	HeadSHA       string
	State         string
	Limit         int
}

// ListVerifyRuns reads finished runs, newest first (ended_at, then id for
// the same instant). Every filter is an exact match; empty means all. The
// loop slug is never a scope on its own (I4): with it, the component root is
// required, and the pair is composed through Scope.
func (s *Store) ListVerifyRuns(f VerifyRunFilter) ([]VerifyRun, error) {
	var where []string
	var args []any
	switch {
	case f.LoopSlug != "":
		if f.ComponentRoot == "" {
			return nil, fmt.Errorf("store: list verify runs: a loop slug without its component root is not a scope (I4)")
		}
		pred, predArgs := Scope{LoopSlug: f.LoopSlug, ComponentRoot: f.ComponentRoot}.Where("")
		where = append(where, pred)
		args = append(args, predArgs...)
	case f.ComponentRoot != "":
		pred, predArgs := ComponentScope{ComponentRoot: f.ComponentRoot}.Where("")
		where = append(where, pred)
		args = append(args, predArgs...)
	}
	if f.HeadSHA != "" {
		where = append(where, `head_sha = ?`)
		args = append(args, f.HeadSHA)
	}
	if f.State != "" {
		where = append(where, `state = ?`)
		args = append(args, f.State)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	query := `SELECT id, loop_slug, component_root, component_id, task, position, attempt, head_sha, trigger, actor_kind, actor_id, started_at, ended_at, state, reason, summary, digest
		FROM verify_run` + clause + ` ORDER BY ended_at DESC, id DESC`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list verify runs: %w", err)
	}
	defer rows.Close()
	var out []VerifyRun
	for rows.Next() {
		var (
			vr      VerifyRun
			endedAt sql.NullString
		)
		if err := rows.Scan(&vr.ID, &vr.LoopSlug, &vr.ComponentRoot, &vr.ComponentID, &vr.Task, &vr.Position, &vr.Attempt,
			&vr.HeadSHA, &vr.Trigger, &vr.ActorKind, &vr.ActorID, &vr.StartedAt, &endedAt, &vr.State, &vr.Reason, &vr.Summary, &vr.Digest); err != nil {
			return nil, fmt.Errorf("store: list verify runs: %w", err)
		}
		vr.EndedAt = nullStringPtr(endedAt)
		out = append(out, vr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list verify runs: %w", err)
	}
	return out, nil
}

// GetVerifyRun reads one finished run with its results in seq order. Absent
// is ErrNotFound (a run in its draft time is in no store to be read).
func (s *Store) GetVerifyRun(id string) (VerifyRun, []VerifyRunResult, error) {
	vr, err := scanVerifyRun(s.r.QueryRow(`SELECT id, loop_slug, component_root, component_id, task, position, attempt, head_sha, trigger, actor_kind, actor_id, started_at, ended_at, state, reason, summary, digest
		FROM verify_run WHERE id = ?`, id))
	if err != nil {
		return VerifyRun{}, nil, err
	}
	rows, err := s.r.Query(`SELECT run_id, seq, command_id, name, requirement, command, exit_code, signal, timed_out, wall_ms, stdout_bytes, stderr_bytes, truncated, stdout_sha256, stderr_sha256, stdout_tail, stderr_tail, state, reason
		FROM verify_run_result WHERE run_id = ? ORDER BY seq`, id)
	if err != nil {
		return VerifyRun{}, nil, fmt.Errorf("store: verify run %s results: %w", id, err)
	}
	defer rows.Close()
	var results []VerifyRunResult
	for rows.Next() {
		var (
			runID     string
			r         VerifyRunResult
			commandID sql.NullString
			exitCode  sql.NullInt64
			timedOut  int
			trunc     int
		)
		if err := rows.Scan(&runID, &r.Seq, &commandID, &r.Name, &r.Requirement, &r.Command, &exitCode, &r.Signal, &timedOut,
			&r.WallMS, &r.StdoutBytes, &r.StderrBytes, &trunc, &r.StdoutSHA256, &r.StderrSHA256, &r.StdoutTail, &r.StderrTail, &r.State, &r.Reason); err != nil {
			return VerifyRun{}, nil, fmt.Errorf("store: verify run %s results: %w", id, err)
		}
		r.CommandID = nullStringPtr(commandID)
		if exitCode.Valid {
			v := int(exitCode.Int64)
			r.ExitCode = &v
		}
		r.TimedOut = timedOut == 1
		r.Truncated = trunc == 1
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return VerifyRun{}, nil, fmt.Errorf("store: verify run %s results: %w", id, err)
	}
	return vr, results, nil
}

func scanVerifyRun(row interface{ Scan(...any) error }) (VerifyRun, error) {
	var (
		vr      VerifyRun
		endedAt sql.NullString
	)
	err := row.Scan(&vr.ID, &vr.LoopSlug, &vr.ComponentRoot, &vr.ComponentID, &vr.Task, &vr.Position, &vr.Attempt,
		&vr.HeadSHA, &vr.Trigger, &vr.ActorKind, &vr.ActorID, &vr.StartedAt, &endedAt, &vr.State, &vr.Reason, &vr.Summary, &vr.Digest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VerifyRun{}, fmt.Errorf("store: verify run: %w", ErrNotFound)
		}
		return VerifyRun{}, fmt.Errorf("store: read verify run: %w", err)
	}
	vr.EndedAt = nullStringPtr(endedAt)
	return vr, nil
}

// LatestRunForCommand answers "when was this gate last run": the newest
// finished run that ran a result row for the command (a gate is a row,
// and its runs are the runs whose results name it). The (run, false, nil)
// answer is the honest never-run — the list's last-run line is simply
// absent. Ordered the way runs are everywhere: newest finished first.
func (s *Store) LatestRunForCommand(commandID string) (VerifyRun, bool, error) {
	row := s.r.QueryRow(`SELECT r.id, r.loop_slug, r.component_root, r.component_id, r.task, r.position, r.attempt, r.head_sha, r.trigger, r.actor_kind, r.actor_id, r.started_at, r.ended_at, r.state, r.reason, r.summary, r.digest
		FROM verify_run r
		JOIN verify_run_result rr ON rr.run_id = r.id
		WHERE rr.command_id = ?
		ORDER BY r.ended_at DESC, r.id DESC
		LIMIT 1`, commandID)
	vr, err := scanVerifyRun(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return VerifyRun{}, false, nil
		}
		return VerifyRun{}, false, err
	}
	return vr, true, nil
}

// LatestPassingRun answers "is this head of this loop of this root already
// verified green?": the newest finished pass over the exact
// (component_root, loop_slug, head_sha) triple, or (zero, false, nil) when
// there is none. An empty head_sha matches nothing (a run that never knew
// its head is not evidence for a head) — the caller asking with an empty
// SHA gets the honest no.
func (s *Store) LatestPassingRun(componentRoot, loopSlug, headSHA string) (VerifyRun, bool, error) {
	if headSHA == "" {
		return VerifyRun{}, false, nil
	}
	var vr VerifyRun
	err := s.r.QueryRow(`SELECT id, loop_slug, component_root, component_id, task, position, attempt, head_sha, trigger, actor_kind, actor_id, started_at, ended_at, state, reason, summary, digest
		FROM verify_run
		WHERE component_root = ? AND loop_slug = ? AND head_sha = ? AND state = 'pass'
		ORDER BY ended_at DESC, id DESC LIMIT 1`, componentRoot, loopSlug, headSHA).
		Scan(&vr.ID, &vr.LoopSlug, &vr.ComponentRoot, &vr.ComponentID, &vr.Task, &vr.Position, &vr.Attempt,
			&vr.HeadSHA, &vr.Trigger, &vr.ActorKind, &vr.ActorID, &vr.StartedAt, &vr.EndedAt, &vr.State, &vr.Reason, &vr.Summary, &vr.Digest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VerifyRun{}, false, nil
		}
		return VerifyRun{}, false, fmt.Errorf("store: latest passing run of %s@%s: %w", componentRoot, headSHA, err)
	}
	return vr, true, nil
}
