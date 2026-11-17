package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// agent_tasks.go is S14 T3's row layer (migration 0022). The domain decisions
// live in internal/agentsvc; every statement over agent_task and agent_chain
// lives HERE, as I2 demands — and the transitions that must not be seen
// half-done are ONE transaction: a step suspended without its task row (or
// its question) is exactly the half-state a restart cannot reason about.

// AgentTaskStatuses is the CHECK vocabulary mirrored from migration 0022; the
// lock test compares the two.
var AgentTaskStatuses = []string{"running", "waiting", "completed", "failed", "refused"}

// AgentTask is one delegation — a row, nothing more (AS5: nothing spawns).
type AgentTask struct {
	ID           int64  `json:"id"`
	CallerLoopID string `json:"callerLoopId"`
	CallerLane   string `json:"callerLane"`
	CallerStepID int64  `json:"callerStepId"`
	QuestionID   *int64 `json:"questionId,omitempty"`
	CalleeAgent  string `json:"calleeAgent"`
	SkillID      string `json:"skillId"`
	Mode         string `json:"mode"`
	Status       string `json:"status"`
	ParamsJSON   string `json:"paramsJson"`
	ResultJSON   string `json:"resultJson,omitempty"`
	PoliciesJSON string `json:"policiesJson"`
	PoliciesHash string `json:"policiesHash"`
	CreatedAt    string `json:"createdAt"`
	CompletedAt  string `json:"completedAt,omitempty"`
}

// NewAgentTask is what a caller supplies; the store validates vocabulary and
// returns results, not exceptions, for domain refusals (DB19).
type NewAgentTask struct {
	CallerLoopID string
	CallerLane   string
	CallerStepID int64
	QuestionID   *int64
	CalleeAgent  string
	SkillID      string
	Mode         string
	Status       string
	ParamsJSON   string
	PoliciesJSON string
	PoliciesHash string
}

// SuspendStepForTask is the async creation in ONE transaction (D5): the step
// row's guard flip first (a step that is not running cannot suspend — the
// RowsAffected check is the race net, same shape as ContinueStepWithMessages),
// then the question and its thread entry (D16 keeps that pair atomic), then
// the task row. Any failure rolls all of it back: there is no world where the
// step says "suspended" while no task exists to complete.
func (s *Store) SuspendStepForTask(sc Scope, in NewAgentTask, questionBody string) (AgentTask, error) {
	if !sc.Valid() {
		return AgentTask{}, fmt.Errorf("store: SuspendStepForTask needs both a slug and a component root, got %s", sc)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: begin task suspension: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`UPDATE step SET status='suspended' WHERE id=? AND status='running'`, in.CallerStepID)
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: suspend step %d for task: %w", in.CallerStepID, err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return AgentTask{}, fmt.Errorf("store: step %d is not running (affected %d, err %v) — only a running step can delegate", in.CallerStepID, n, err)
	}

	now := s.Now()
	qres, err := tx.Exec(`
		INSERT INTO question (loop_slug, component_root, step_id, tool_use_id, kind, body, options, asked_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		sc.LoopSlug, sc.ComponentRoot, in.CallerStepID, nil, string(QuestionFree), questionBody, nil, now)
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: ask for task suspension in %s: %w", sc, err)
	}
	qid, err := qres.LastInsertId()
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: read the suspension question id: %w", err)
	}
	if _, err := appendThread(tx, now, sc, NewChatMessage{Author: "agent", Kind: "question", Body: questionBody}, &qid); err != nil {
		return AgentTask{}, fmt.Errorf("store: write the thread entry for the task question in %s: %w", sc, err)
	}

	tres, err := tx.Exec(`
		INSERT INTO agent_task (caller_loop_id, caller_lane, caller_step_id, question_id,
			callee_agent, skill_id, mode, status, params_json, policies_json, policies_hash, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.CallerLoopID, in.CallerLane, in.CallerStepID, qid,
		in.CalleeAgent, in.SkillID, in.Mode, in.Status, in.ParamsJSON, in.PoliciesJSON, in.PoliciesHash, now)
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: insert task for step %d: %w", in.CallerStepID, err)
	}
	id, err := tres.LastInsertId()
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: read the task id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AgentTask{}, fmt.Errorf("store: commit task suspension for step %d: %w", in.CallerStepID, err)
	}
	return s.GetAgentTask(id)
}

// CreateAgentTask is the sync insert (and the plain row write); the async
// path goes through SuspendStepForTask so the step flip is never separable.
func (s *Store) CreateAgentTask(in NewAgentTask) (AgentTask, error) {
	res, err := s.w.Exec(`
		INSERT INTO agent_task (caller_loop_id, caller_lane, caller_step_id, question_id,
			callee_agent, skill_id, mode, status, params_json, policies_json, policies_hash, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.CallerLoopID, in.CallerLane, in.CallerStepID, in.QuestionID,
		in.CalleeAgent, in.SkillID, in.Mode, in.Status, in.ParamsJSON, in.PoliciesJSON, in.PoliciesHash, s.Now())
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: insert agent task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: read the agent task id: %w", err)
	}
	return s.GetAgentTask(id)
}

const agentTaskColumns = `id, caller_loop_id, caller_lane, caller_step_id, COALESCE(question_id,0),
	callee_agent, skill_id, mode, status, params_json, COALESCE(result_json,''),
	policies_json, policies_hash, created_at, COALESCE(completed_at,'')`

func scanAgentTask(row interface{ Scan(...any) error }) (AgentTask, error) {
	var t AgentTask
	var qid int64
	if err := row.Scan(&t.ID, &t.CallerLoopID, &t.CallerLane, &t.CallerStepID, &qid,
		&t.CalleeAgent, &t.SkillID, &t.Mode, &t.Status, &t.ParamsJSON, &t.ResultJSON,
		&t.PoliciesJSON, &t.PoliciesHash, &t.CreatedAt, &t.CompletedAt); err != nil {
		return AgentTask{}, err
	}
	if qid != 0 {
		t.QuestionID = &qid
	}
	return t, nil
}

// GetAgentTask reads one task. The row is sufficient for recovery — loop id,
// lane, step, question: S7 resumes a Task it did not create from this alone.
func (s *Store) GetAgentTask(id int64) (AgentTask, error) {
	row := s.r.QueryRow(`SELECT `+agentTaskColumns+` FROM agent_task WHERE id = ?`, id)
	t, err := scanAgentTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentTask{}, fmt.Errorf("store: agent task %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return AgentTask{}, fmt.Errorf("store: read agent task %d: %w", id, err)
	}
	return t, nil
}

// MarkAgentTaskTerminal moves a task to a terminal status IF it still stands
// where expected — RowsAffected is the answer, so a double completion is
// visible rather than silent (the same race discipline as a double answer).
func (s *Store) MarkAgentTaskTerminal(id int64, from, to, resultJSON string) (bool, error) {
	var res sql.Result
	var err error
	if to == "completed" || to == "failed" || to == "refused" {
		res, err = s.w.Exec(`UPDATE agent_task SET status=?, result_json=?, completed_at=? WHERE id=? AND status=?`,
			to, nullable(resultJSON), s.Now(), id, from)
	} else {
		res, err = s.w.Exec(`UPDATE agent_task SET status=? WHERE id=? AND status=?`, to, id, from)
	}
	if err != nil {
		return false, fmt.Errorf("store: move agent task %d %s→%s: %w", id, from, to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: count the move of agent task %d: %w", id, err)
	}
	return n == 1, nil
}

// --- the chain edge (0022's second table) -----------------------------------

// NewChainEdge is what Invoke supplies before the callee runs (AS6): the edge
// exists so a crash mid-invocation leaves a recorded attempt rather than an
// untraceable cost. T5 owns the chain's PROPERTIES (cycles, depth); this owns
// putting the row there, in the right order.
type NewChainEdge struct {
	CallerLoopID string
	CallerStepID int64
	CalleeAgent  string
	SkillID      string
	Depth        int
}

// CreateAgentChainEdge refuses an empty identity rather than writing a row
// that could never be rolled up — the vocabulary the CHECK fixes, the
// emptiness the Go side refuses with words.
func (s *Store) CreateAgentChainEdge(in NewChainEdge) error {
	if in.CallerLoopID == "" {
		return fmt.Errorf("store: a chain edge without a caller loop is untraceable by construction")
	}
	if in.CalleeAgent == "" || in.SkillID == "" {
		return fmt.Errorf("store: a chain edge must name the callee and the skill asked of it")
	}
	if in.Depth < 1 {
		return fmt.Errorf("store: chain depth %d is below the root call's 1", in.Depth)
	}
	_, err := s.w.Exec(
		`INSERT INTO agent_chain (caller_loop_id, caller_step_id, callee_agent, skill_id, depth, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		in.CallerLoopID, in.CallerStepID, in.CalleeAgent, in.SkillID, in.Depth, s.Now(),
	)
	if err != nil {
		return fmt.Errorf("store: record the chain edge: %w", err)
	}
	return nil
}

// CountAgentChainEdges is the read T4's ordering proof needs — was the
// recorded attempt written even though its Task never was.
func (s *Store) CountAgentChainEdges(callerLoopID string, callerStepID int64) (int, error) {
	var n int
	if err := s.r.QueryRow(
		`SELECT COUNT(*) FROM agent_chain WHERE caller_loop_id = ? AND caller_step_id = ?`,
		callerLoopID, callerStepID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count chain edges: %w", err)
	}
	return n, nil
}

// CountAgentTasksForCaller answers "did this step's attempt become a task"
// without letting a consumer reach SQL (I2): the pairing with
// CountAgentChainEdges is what proves the edge-before-task order.
func (s *Store) CountAgentTasksForCaller(callerLoopID string, callerStepID int64) (int, error) {
	var n int
	if err := s.r.QueryRow(
		`SELECT COUNT(*) FROM agent_task WHERE caller_loop_id = ? AND caller_step_id = ?`,
		callerLoopID, callerStepID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count tasks: %w", err)
	}
	return n, nil
}

// ChainEdge is a recorded ask (0022's agent_chain). The caller AGENT is not
// stored on the row — it is the caller step's agent, which is what the join
// below resolves and what keeps the graph honest: an ask recorded under a
// step belongs to the agent that step runs, not to whoever claimed it.
type ChainEdge struct {
	ID           int64  `json:"id"`
	CallerLoopID string `json:"callerLoopId"`
	CallerStepID int64  `json:"callerStepId"`
	CallerAgent  string `json:"callerAgent"`
	CalleeAgent  string `json:"calleeAgent"`
	SkillID      string `json:"skillId"`
	Depth        int    `json:"depth"`
	CreatedAt    string `json:"createdAt"`
}

// ChainEdgesByCallerAgent answers the chain-walker's question — what has THIS
// agent asked of others — through the step join (see ChainEdge). Ordered by
// id: a chain is history, and history has an order.
func (s *Store) ChainEdgesByCallerAgent(agent string) ([]ChainEdge, error) {
	rows, err := s.r.Query(
		`SELECT ac.id, ac.caller_loop_id, ac.caller_step_id, COALESCE(st.agent, ''), ac.callee_agent, ac.skill_id, ac.depth, ac.created_at
		 FROM agent_chain ac LEFT JOIN step st ON st.id = ac.caller_step_id
		 WHERE st.agent = ? ORDER BY ac.id`, agent)
	if err != nil {
		return nil, fmt.Errorf("store: read the chain for %q: %w", agent, err)
	}
	defer rows.Close()
	var out []ChainEdge
	for rows.Next() {
		var e ChainEdge
		if err := rows.Scan(&e.ID, &e.CallerLoopID, &e.CallerStepID, &e.CallerAgent, &e.CalleeAgent, &e.SkillID, &e.Depth, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan a chain edge: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: walk the chain for %q: %w", agent, err)
	}
	return out, nil
}

// ChainEdgesToCallee answers the backward question the depth bound needs —
// who has THIS agent been asked to do things for — through the same join,
// ordered by id.
func (s *Store) ChainEdgesToCallee(callee string) ([]ChainEdge, error) {
	rows, err := s.r.Query(
		`SELECT ac.id, ac.caller_loop_id, ac.caller_step_id, COALESCE(st.agent, ''), ac.callee_agent, ac.skill_id, ac.depth, ac.created_at
		 FROM agent_chain ac LEFT JOIN step st ON st.id = ac.caller_step_id
		 WHERE ac.callee_agent = ? ORDER BY ac.id`, callee)
	if err != nil {
		return nil, fmt.Errorf("store: read who asked %q: %w", callee, err)
	}
	defer rows.Close()
	var out []ChainEdge
	for rows.Next() {
		var e ChainEdge
		if err := rows.Scan(&e.ID, &e.CallerLoopID, &e.CallerStepID, &e.CallerAgent, &e.CalleeAgent, &e.SkillID, &e.Depth, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan a chain edge: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: walk who-asked for %q: %w", callee, err)
	}
	return out, nil
}

// ListAgentTasksForCaller reads every task a caller step filed — the rows a
// rollup walks. Ordered by id: a chain is history.
func (s *Store) ListAgentTasksForCaller(callerLoopID string, callerStepID int64) ([]AgentTask, error) {
	rows, err := s.r.Query(`SELECT `+agentTaskColumns+` FROM agent_task
		 WHERE caller_loop_id = ? AND caller_step_id = ? ORDER BY id`, callerLoopID, callerStepID)
	if err != nil {
		return nil, fmt.Errorf("store: read tasks for step %d: %w", callerStepID, err)
	}
	defer rows.Close()
	var out []AgentTask
	for rows.Next() {
		t, err := scanAgentTask(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan a task for step %d: %w", callerStepID, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: walk tasks for step %d: %w", callerStepID, err)
	}
	return out, nil
}

// ListAgentChainEdges reads the asks recorded under one caller step — the
// walk's frontier. Ordered by id.
func (s *Store) ListAgentChainEdges(callerLoopID string, callerStepID int64) ([]ChainEdge, error) {
	rows, err := s.r.Query(
		`SELECT ac.id, ac.caller_loop_id, ac.caller_step_id, COALESCE(st.agent, ''), ac.callee_agent, ac.skill_id, ac.depth, ac.created_at
		 FROM agent_chain ac LEFT JOIN step st ON st.id = ac.caller_step_id
		 WHERE ac.caller_loop_id = ? AND ac.caller_step_id = ? ORDER BY ac.id`, callerLoopID, callerStepID)
	if err != nil {
		return nil, fmt.Errorf("store: read edges for step %d: %w", callerStepID, err)
	}
	defer rows.Close()
	var out []ChainEdge
	for rows.Next() {
		var e ChainEdge
		if err := rows.Scan(&e.ID, &e.CallerLoopID, &e.CallerStepID, &e.CallerAgent, &e.CalleeAgent, &e.SkillID, &e.Depth, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan an edge for step %d: %w", callerStepID, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: walk edges for step %d: %w", callerStepID, err)
	}
	return out, nil
}
