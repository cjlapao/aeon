package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type PinnedLoop struct {
	Loop             Loop   `json:"loop"`
	BlueprintID      string `json:"blueprintId"`
	BlueprintVersion int    `json:"blueprintVersion"`
	Content          string `json:"content"`
	RuntimeJSON      string `json:"runtimeJson"`
	UpdatedAt        string `json:"updatedAt"`
	// Attended is the D2 declaration pinned beside the blueprint bytes at
	// start: attended, unattended, or never recorded (nil). No omitempty —
	// dropping false and dropping nil are different answers, and a wire that
	// cannot tell them apart has already collapsed the third state (P31).
	Attended *bool `json:"attended"`
}

// CommitGate writes the question, thread entry and waiting position together.
// The question's tool_use_id and step_id are NULL by construction.
func (s *Store) CommitGate(loopID string, sc Scope, prompt string, options []string, runtimeJSON string) (Question, error) {
	if !sc.Valid() || prompt == "" {
		return Question{}, fmt.Errorf("store: a gate needs a scoped loop and prompt")
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return Question{}, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Question{}, err
	}
	defer tx.Rollback()
	now := s.Now()
	res, err := tx.Exec(`INSERT INTO question(loop_slug,component_root,step_id,tool_use_id,kind,body,options,asked_at) VALUES(?,?,NULL,NULL,'gate',?,?,?)`, sc.LoopSlug, sc.ComponentRoot, prompt, string(encodedOptions), now)
	if err != nil {
		return Question{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Question{}, err
	}
	if _, err := appendThread(tx, now, sc, NewChatMessage{Author: "agent", Kind: "question", Body: prompt}, &id); err != nil {
		return Question{}, err
	}
	var runtime map[string]any
	if err := json.Unmarshal([]byte(runtimeJSON), &runtime); err != nil {
		return Question{}, err
	}
	runtime["questionId"] = id
	updated, err := json.Marshal(runtime)
	if err != nil {
		return Question{}, err
	}
	res, err = tx.Exec(`UPDATE loop_blueprint SET runtime_json=?,updated_at=? WHERE loop_id=?`, string(updated), now, loopID)
	if err != nil {
		return Question{}, err
	}
	if err := affectPinnedOne(res, loopID, "save gate"); err != nil {
		return Question{}, err
	}
	res, err = tx.Exec(`UPDATE loop SET status='waiting',updated_at=? WHERE id=? AND slug=? AND component_root=?`, now, loopID, sc.LoopSlug, sc.ComponentRoot)
	if err != nil {
		return Question{}, err
	}
	if err := affectPinnedOne(res, loopID, "wait at gate"); err != nil {
		return Question{}, err
	}
	if err := tx.Commit(); err != nil {
		return Question{}, err
	}
	return s.ReadQuestion(id)
}

// AnswerGate atomically records a closed-set answer and the next position.
func (s *Store) AnswerGate(loopID string, questionID int64, answer, status, runtimeJSON string) (Question, error) {
	q, err := s.ReadQuestion(questionID)
	if err != nil {
		return Question{}, err
	}
	if q.Kind != string(QuestionGate) || q.StepID != nil || q.ToolUseID != "" {
		return Question{}, fmt.Errorf("question %d is not a gate", questionID)
	}
	if q.Answered() {
		return Question{}, fmt.Errorf("gate %d was already answered", questionID)
	}
	if len(q.Options) > 0 && !slices.Contains(q.Options, answer) {
		return Question{}, fmt.Errorf("answer %q is not one of %v", answer, q.Options)
	}
	pin, err := s.ReadPinnedLoop(loopID)
	if err != nil {
		return Question{}, err
	}
	if pin.Loop.Slug != q.LoopSlug || pin.Loop.ComponentRoot != q.ComponentRoot {
		return Question{}, fmt.Errorf("gate %d belongs to another loop", questionID)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Question{}, err
	}
	defer tx.Rollback()
	now := s.Now()
	res, err := tx.Exec(`UPDATE question SET answer=?,answered_at=? WHERE id=? AND answered_at IS NULL`, answer, now, questionID)
	if err != nil {
		return Question{}, err
	}
	if err := affectPinnedOne(res, loopID, "answer gate"); err != nil {
		return Question{}, err
	}
	if _, err := appendThread(tx, now, Scope{LoopSlug: q.LoopSlug, ComponentRoot: q.ComponentRoot}, NewChatMessage{Author: "human", Kind: "answer", Body: answer}, &questionID); err != nil {
		return Question{}, err
	}
	res, err = tx.Exec(`UPDATE loop_blueprint SET runtime_json=?,updated_at=? WHERE loop_id=?`, runtimeJSON, now, loopID)
	if err != nil {
		return Question{}, err
	}
	if err := affectPinnedOne(res, loopID, "advance gate"); err != nil {
		return Question{}, err
	}
	res, err = tx.Exec(`UPDATE loop SET status=?,updated_at=? WHERE id=?`, status, now, loopID)
	if err != nil {
		return Question{}, err
	}
	if err := affectPinnedOne(res, loopID, "resume loop"); err != nil {
		return Question{}, err
	}
	if err := tx.Commit(); err != nil {
		return Question{}, err
	}
	return s.ReadQuestion(questionID)
}

// CreatePinnedLoop commits loop identity, raw blueprint and its initial
// position together. A failed pin never leaves a loop row behind.
func (s *Store) CreatePinnedLoop(in NewLoop, blueprintID string, version int, content string, runtimeJSON string) (Loop, error) {
	if in.Slug == "" || in.ComponentRoot == "" || blueprintID == "" || content == "" || runtimeJSON == "" {
		return Loop{}, fmt.Errorf("store: pinned loop needs slug, component, blueprint and state")
	}
	now := s.Now()
	l := Loop{ID: newID(), GoalID: in.GoalID, Slug: in.Slug, Title: orDefault(in.Title, in.Slug), Goal: in.Goal, Root: orDefault(in.Root, in.ComponentRoot), ComponentRoot: in.ComponentRoot, ProjectKey: in.ProjectKey, Mode: orDefault(in.Mode, string(ModeStandard)), Phase: orDefault(in.Phase, string(PhasePreflight)), Status: orDefault(in.Status, "active"), StartedAt: now, UpdatedAt: now, StartedBy: in.StartedBy, TriggerID: in.TriggerID, AutomationDepth: in.AutomationDepth,
		DefaultBranch: in.DefaultBranch, BaseBranch: in.BaseBranch, BranchMode: in.BranchMode, FeatureBranch: in.FeatureBranch}
	tx, err := s.w.Begin()
	if err != nil {
		return Loop{}, err
	}
	defer tx.Rollback()
	// The goal coordinates ride the INSERT itself (S38 T13): the pin and the
	// goal it works arrive in the same transaction or not at all — a loop
	// row that outlives a failed coordinate write would be a loop claiming
	// a goal nobody checked.
	_, err = tx.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,project_key,mode,phase,status,attended,allow_outward,started_at,updated_at,started_by,trigger_id,goal_id,goal_version,automation_depth,default_branch,base_branch,branch_mode,feature_branch) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, l.ID, l.Slug, l.Title, l.Goal, l.Root, l.ComponentRoot, nullable(l.ProjectKey), l.Mode, l.Phase, l.Status, attendedArg(in.Attended), outwardArg(in.AllowOutward), l.StartedAt, l.UpdatedAt, in.StartedBy, in.TriggerID, nullable(in.GoalID), goalVersionArg(in.GoalVersion), in.AutomationDepth, nullable(l.DefaultBranch), nullable(l.BaseBranch), nullable(l.BranchMode), nullable(l.FeatureBranch))
	if err != nil {
		return Loop{}, fmt.Errorf("store: create pinned loop: %w", err)
	}
	_, err = tx.Exec(`INSERT INTO loop_blueprint(loop_id,blueprint_id,blueprint_version,content,runtime_json,updated_at) VALUES(?,?,?,?,?,?)`, l.ID, blueprintID, version, content, runtimeJSON, now)
	if err != nil {
		return Loop{}, fmt.Errorf("store: pin blueprint: %w", err)
	}
	// Run 1 (S39 CR2): every pinned loop has a run row from birth, so a
	// loop is never asked which run it is in. Provenance rides S20's 0033
	// columns — a trigger- or webhook-started loop's first run SAYS so
	// (started_by_kind=trigger, started_by_id=the trigger id); a plain
	// start is the user's. Same transaction as the pin: a loop with a pin
	// and no run 1 is the inconsistency this line exists to prevent
	// (runs_test.go pins it).
	kind := "user"
	if in.StartedBy == "event" {
		kind = "event" // S40 AU9: the event door says so, in run 1's own row (T1 rebuilt the CHECK; runs.go admits it)
	} else if in.StartedBy == "scheduler" {
		kind = "scheduler" // S27 T2 CN15: the scheduler's own word, no longer laundered into 'trigger' (T1's start_provenance golden is regenerated by this commit DELIBERATELY)
	} else if in.StartedBy != "" {
		kind = "trigger"
	}
	if _, err := tx.Exec(`INSERT INTO loop_run(loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_by_id,started_at) VALUES(?,1,?,?,?,'running',?,?,?)`,
		l.ID, blueprintID, version, content, kind, in.TriggerID, now); err != nil {
		return Loop{}, fmt.Errorf("store: record run 1: %w", err)
	}
	// The kickoff (S40 T4, AU9/AU10): an event start speaks its first words
	// in the SAME transaction the loop is pinned — never stamped after the
	// commit, because by then the runner may already have dispatched a step
	// whose first message does not exist. Any failure here rolls the loop
	// back with it: there is no loop without its kickoff.
	//
	// S77 links that kickoff onto run 1's own row. A chosen skill's reload
	// could lean on the thread-order heuristic ("the first human row");
	// ATTACHED BYTES must not: a launch's receipts have to be re-readable AS
	// this launch's after a restart, and a later turn's message is never the
	// launch. The link is a fact, not a hint — same transaction, or the
	// kickoff does not exist either.
	if in.FirstMessage != nil {
		kickoffID, err := appendThread(tx, now, Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}, *in.FirstMessage, nil)
		if err != nil {
			return Loop{}, fmt.Errorf("store: write the kickoff message: %w", err)
		}
		if _, err := tx.Exec(`UPDATE loop_run SET kickoff_message_id=? WHERE loop_id=? AND run_seq=1`, kickoffID, l.ID); err != nil {
			return Loop{}, fmt.Errorf("store: link the kickoff to run 1: %w", err)
		}
	}
	// The autonomy pin (S56 AL8): the immutable snapshot a policy-resolved loop
	// is pinned with, written in the SAME transaction the loop is born — a loop
	// with a pin that failed to write is the half-written birth this exists to
	// prevent, and the pin is never updated or deleted (AL6/AL8). A loop that is
	// not policy-resolved has no pin row (nil): today's behaviour exactly. No FK
	// to loop: archive moves loop rows out, so the pin outlives them like history.
	if in.AutonomyPin != nil {
		if _, err := tx.Exec(`INSERT INTO loop_autonomy_pin (loop_id, root_id, rev, origin, labels, allowed, pinned_at)
			VALUES (?,?,?,?,?,?,?)`, l.ID, in.AutonomyPin.RootID, in.AutonomyPin.Rev, in.AutonomyPin.Origin,
			in.AutonomyPin.Labels, in.AutonomyPin.Allowed, now); err != nil {
			return Loop{}, fmt.Errorf("store: write autonomy pin: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Loop{}, err
	}
	return l, nil
}

func (s *Store) ReadPinnedLoop(id string) (PinnedLoop, error) {
	reader := s.r
	tx, err := reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return PinnedLoop{}, err
	}
	defer tx.Rollback()
	var pin PinnedLoop
	row := tx.QueryRow(`SELECT `+loopColumns+` FROM loop WHERE id=?`, id)
	pin.Loop, err = scanLoop(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PinnedLoop{}, ErrNotFound
	}
	if err != nil {
		return PinnedLoop{}, err
	}
	err = tx.QueryRow(`SELECT blueprint_id,blueprint_version,content,runtime_json,updated_at FROM loop_blueprint WHERE loop_id=?`, id).Scan(&pin.BlueprintID, &pin.BlueprintVersion, &pin.Content, &pin.RuntimeJSON, &pin.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PinnedLoop{}, ErrNotFound
	}
	if err != nil {
		return PinnedLoop{}, err
	}
	// Attended is read by name, not through loopColumns: its NULL is a real
	// third state (never declared) and must never arrive as false.
	var rawAttended sql.NullInt64
	if err := tx.QueryRow(`SELECT attended FROM loop WHERE id=?`, id).Scan(&rawAttended); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PinnedLoop{}, ErrNotFound
		}
		return PinnedLoop{}, err
	}
	pin.Attended, err = attendedFromNull(rawAttended)
	if err != nil {
		return PinnedLoop{}, fmt.Errorf("store: pinned loop %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return PinnedLoop{}, err
	}
	return pin, nil
}

// SavePinnedRuntime atomically commits the next position and loop status.
func (s *Store) SavePinnedRuntime(id, status, runtimeJSON string) error {
	if runtimeJSON == "" {
		return fmt.Errorf("store: empty loop runtime state")
	}
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.Now()
	res, err := tx.Exec(`UPDATE loop_blueprint SET runtime_json=?,updated_at=? WHERE loop_id=?`, runtimeJSON, now, id)
	if err != nil {
		return err
	}
	if err := affectPinnedOne(res, id, "save loop state"); err != nil {
		return err
	}
	res, err = tx.Exec(`UPDATE loop SET status=?,updated_at=? WHERE id=?`, status, now, id)
	if err != nil {
		return err
	}
	if err := affectPinnedOne(res, id, "set loop status"); err != nil {
		return err
	}
	return tx.Commit()
}

func affectPinnedOne(res sql.Result, id, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: %s %s: %w", what, id, ErrNotFound)
	}
	return nil
}

func (s *Store) PinnedLoopsByStatus(statuses ...string) ([]PinnedLoop, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	query := `SELECT loop_id FROM loop_blueprint JOIN loop ON loop.id=loop_blueprint.loop_id WHERE loop.status IN (`
	args := make([]any, len(statuses))
	for i, status := range statuses {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = status
	}
	query += `) ORDER BY loop.started_at,loop.id`
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]PinnedLoop, 0, len(ids))
	for _, id := range ids {
		pin, err := s.ReadPinnedLoop(id)
		if err != nil {
			return nil, err
		}
		out = append(out, pin)
	}
	return out, nil
}

// LoopBlueprintID reads which blueprint a loop runs — from the PIN table,
// where the blueprint lives (loop.blueprint_id would be a column on a table
// that never had one; the pin is the single source). S38 T9: goal_create
// stamps source_kind=brainstorm when the step's loop is the brainstorm loop
// — an identity read from the store, never from tool input. A loop with no
// pin reads as ErrNotFound, which the service folds into "manual".
func (s *Store) LoopBlueprintID(sc Scope) (string, error) {
	var bp string
	err := s.r.QueryRow(`SELECT lb.blueprint_id FROM loop_blueprint lb
		JOIN loop l ON l.id = lb.loop_id
		WHERE l.component_root = ? AND l.slug = ?`, sc.ComponentRoot, sc.LoopSlug).Scan(&bp)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("store: loop %s has no pin (or no loop): %w", sc, ErrNotFound)
		}
		return "", fmt.Errorf("store: blueprint of %s: %w", sc, err)
	}
	return bp, nil
}
