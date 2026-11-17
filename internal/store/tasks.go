package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// The task ladder's rows.
//
// # The machine is enforced HERE
//
// Every status change goes through ValidateTransition (machine.go), and a
// refusal comes back as a RESULT rather than an error (DB19). The store is
// where the machine is enforced because a caller that could bypass it makes
// the machine advisory — and an advisory state machine is a comment.

// Task is a task's row.
type Task struct {
	TaskID          string   `json:"taskid"`
	LoopSlug        string   `json:"loopSlug"`
	ComponentRoot   string   `json:"componentRoot"`
	Ord             int      `json:"ord"`
	Title           string   `json:"title"`
	Tech            string   `json:"tech"`
	Engineer        string   `json:"engineer"`
	DependsOn       []string `json:"dependsOn"`
	Status          string   `json:"status"`
	Issue           string   `json:"issue,omitempty"`
	PR              string   `json:"pr,omitempty"`
	Branch          string   `json:"branch,omitempty"`
	Retries         int      `json:"retries"`
	SecurityRounds  int      `json:"securityRounds"`
	ReviewRounds    int      `json:"reviewRounds"`
	EstChangedLines *int     `json:"estChangedLines,omitempty"`
	SuccessSummary  string   `json:"successSummary,omitempty"`
	LastEvidence    string   `json:"lastEvidence,omitempty"`
	StartedAt       string   `json:"startedAt,omitempty"`
	CreatedAt       string   `json:"createdAt,omitempty"`
	UpdatedAt       string   `json:"updatedAt"`

	// The S38 T5/OL14 columns. ID is the surrogate that addresses a task
	// whoever it belongs to — loop tasks keep their (component_root, loop_slug,
	// taskid) key for the engine, and the surrogate is what the board, the
	// review ledger and task_loop speak.
	ID            string `json:"id"`
	Manual        bool   `json:"manual"`
	AssigneeKind  string `json:"assigneeKind,omitempty"`
	Assignee      string `json:"assignee,omitempty"`
	ComponentID   string `json:"componentId,omitempty"`
	GoalID        string `json:"goalId,omitempty"`
	ParentTaskID  string `json:"parentTaskId,omitempty"`
	Type          string `json:"type,omitempty"`
	ApprovalState string `json:"approvalState"`
	CreatedByKind string `json:"createdByKind"`
	CreatedByID   string `json:"createdById,omitempty"`
	SourceStep    *int64 `json:"sourceStep,omitempty"`
}

// NewTask is what a caller supplies to create one.
type NewTask struct {
	TaskID    string
	Ord       int
	Title     string
	Tech      string
	Engineer  string
	DependsOn []string
	Status    TaskStatus
	// ParentTaskID names the surrogate id of a task IN THE SAME COMPONENT
	// (store-enforced — there is no FK, per 0036's comment). The engine never
	// sets it; it is what S38's decomposition (a feature loop's tasks hanging
	// under a manual task) rides on.
	ParentTaskID string
}

// CreateTask inserts a task into a loop.
func (s *Store) CreateTask(sc Scope, in NewTask) (Task, error) {
	if !sc.Valid() {
		return Task{}, fmt.Errorf("store: CreateTask needs both a slug and a component root, got %s", sc)
	}
	if in.TaskID == "" {
		return Task{}, fmt.Errorf("store: a task needs an id")
	}

	status := in.Status
	if status == "" {
		status = StatusPending
	}
	// An unknown status at creation is refused rather than stored: the ladder
	// is enumerated (machine.go), and a row holding a status no transition
	// mentions can never move again.
	if _, known := AllowedTransitions()[status]; !known {
		return Task{}, fmt.Errorf("store: %q is not a task status; the ladder is %s",
			status, joinStatuses(TaskStatuses(), ", "))
	}

	deps := in.DependsOn
	if deps == nil {
		deps = []string{}
	}
	encoded, err := json.Marshal(deps)
	if err != nil {
		return Task{}, fmt.Errorf("store: encode depends_on for %s: %w", in.TaskID, err)
	}

	now := s.Now()
	t := Task{
		ID:            "tsk-" + newID(), // surrogate, the way 0036's rebuild generated them
		TaskID:        in.TaskID,
		LoopSlug:      sc.LoopSlug,
		ComponentRoot: sc.ComponentRoot,
		Ord:           in.Ord,
		Title:         orDefault(in.Title, in.TaskID),
		Tech:          in.Tech,
		Engineer:      in.Engineer,
		DependsOn:     deps,
		Status:        string(status),
		CreatedAt:     now,
		UpdatedAt:     now,
		// Assignee was ENGINEER (OL14: "kept as the first assignee") — and a
		// blank engineer is no assignee at all, never an anonymous agent.
		AssigneeKind:  assigneeKindFor(in.Engineer),
		Assignee:      in.Engineer,
		ApprovalState: "none", // the CHECK's only legal value for manual=0
		CreatedByKind: "agent",
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Task{}, fmt.Errorf("store: create task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The component this loop's root belongs to, when the root is registered;
	// loops may run in unregistered workspaces, so NULL is honest here — but
	// a PARENT is then impossible to place, and the sentence says so.
	var componentID sql.NullString
	_ = tx.QueryRow(`SELECT component_id FROM component_root WHERE component_root = ? AND is_primary = 1`,
		sc.ComponentRoot).Scan(&componentID)
	if componentID.Valid {
		t.ComponentID = componentID.String
	}
	// A task planned in a loop that works a goal is that goal's task: the goal
	// view and the roadmap list a goal's tasks by this column, so a planner that
	// files tasks leaves them findable without naming the goal itself.
	var loopGoal sql.NullString
	_ = tx.QueryRow(`SELECT goal_id FROM loop WHERE component_root = ? AND slug = ?`,
		sc.ComponentRoot, sc.LoopSlug).Scan(&loopGoal)
	if loopGoal.Valid {
		t.GoalID = loopGoal.String
	}
	if in.ParentTaskID != "" {
		var one int
		err := tx.QueryRow(`SELECT 1 FROM task WHERE id = ? AND component_id = ?`, in.ParentTaskID, t.ComponentID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, fmt.Errorf("store: parent task %s is not a task of this component — decomposition stays within one component (the store enforces what the schema cannot: no FK across a rebuild)", in.ParentTaskID)
		} else if err != nil {
			return Task{}, fmt.Errorf("store: check parent task %s: %w", in.ParentTaskID, err)
		}
		t.ParentTaskID = in.ParentTaskID
	}

	if _, err = tx.Exec(`
		INSERT INTO task (
			id, loop_slug, component_root, taskid, ord, title, tech, engineer,
			depends_on, status, created_at, updated_at,
			manual, assignee_kind, assignee, component_id, parent_task_id,
			approval_state, created_by_kind, created_by_id, goal_id
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, sc.LoopSlug, sc.ComponentRoot, t.TaskID, t.Ord, t.Title, t.Tech, t.Engineer,
		string(encoded), t.Status, now, now,
		0, nullable(t.AssigneeKind), nullable(t.Assignee), nullable(t.ComponentID), nullable(t.ParentTaskID),
		"none", "agent", "", nullable(t.GoalID)); err != nil {
		return Task{}, fmt.Errorf("store: create task %s in %s: %w", in.TaskID, sc, err)
	}
	// The owner row (OL7): this loop OWNS the task it planned.
	if _, err := tx.Exec(`INSERT INTO task_loop (task_id, loop_slug, component_root, role) VALUES (?,?,?, 'owner')`,
		t.ID, sc.LoopSlug, sc.ComponentRoot); err != nil {
		return Task{}, fmt.Errorf("store: record ownership of %s in %s: %w", in.TaskID, sc, err)
	}
	if err := tx.Commit(); err != nil {
		return Task{}, fmt.Errorf("store: create task: %w", err)
	}
	return t, nil
}

// assigneeKindFor derives the kind from the old engineer text: named engineer
// means an agent was routed, blank means NO assignee (NULL, never an
// anonymous agent — 0036's rebuild derived the same way).
func assigneeKindFor(engineer string) string {
	if engineer == "" {
		return ""
	}
	return "agent"
}

const taskColumns = `id, manual, COALESCE(assignee_kind,''), COALESCE(assignee,''),
	COALESCE(component_id,''), COALESCE(goal_id,''), COALESCE(parent_task_id,''),
	COALESCE(type,''), approval_state, created_by_kind, COALESCE(created_by_id,''), source_step,
	COALESCE(loop_slug,''), COALESCE(component_root,''), COALESCE(taskid,''), ord, title, tech, engineer,
	depends_on, status, COALESCE(issue,''), COALESCE(pr,''), COALESCE(branch,''),
	retries, security_rounds, review_rounds, est_changed_lines,
	COALESCE(success_summary,''), COALESCE(last_evidence,''),
	COALESCE(started_at,''), COALESCE(created_at,''), updated_at`

// scanTask is the ONE reader of taskColumns; every SELECT of a task uses the
// const so a missed column cannot exist.
func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var deps string
	var step sql.NullInt64
	if err := row.Scan(
		&t.ID, &t.Manual, &t.AssigneeKind, &t.Assignee,
		&t.ComponentID, &t.GoalID, &t.ParentTaskID,
		&t.Type, &t.ApprovalState, &t.CreatedByKind, &t.CreatedByID, &step,
		&t.LoopSlug, &t.ComponentRoot, &t.TaskID, &t.Ord, &t.Title, &t.Tech, &t.Engineer,
		&deps, &t.Status, &t.Issue, &t.PR, &t.Branch,
		&t.Retries, &t.SecurityRounds, &t.ReviewRounds, &t.EstChangedLines,
		&t.SuccessSummary, &t.LastEvidence,
		&t.StartedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return Task{}, err
	}
	if step.Valid {
		v := step.Int64
		t.SourceStep = &v
	}
	if err := json.Unmarshal([]byte(deps), &t.DependsOn); err != nil {
		// A corrupt depends_on is reported rather than silently treated as
		// empty: empty means "no dependencies", which would let a task complete
		// that should have been gated.
		return Task{}, fmt.Errorf("the depends_on of %s is not a JSON array: %w", t.TaskID, err)
	}
	if t.DependsOn == nil {
		t.DependsOn = []string{}
	}
	return t, nil
}

// getTaskByID reads a task by its surrogate id — the address manual tasks and
// the board live at.
func (s *Store) getTaskByID(id string) (Task, error) {
	row := s.r.QueryRow(`SELECT `+taskColumns+` FROM task WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("store: task %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Task{}, fmt.Errorf("store: read task %s: %w", id, err)
	}
	return t, nil
}

// GetTask reads one task.
func (s *Store) GetTask(sc Scope, taskID string) (Task, error) {
	where, args := sc.Where("")
	args = append(args, taskID)
	row := s.r.QueryRow(`SELECT `+taskColumns+` FROM task WHERE `+where+` AND taskid = ?`, args...)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("store: task %s in %s: %w", taskID, sc, ErrNotFound)
	}
	if err != nil {
		return Task{}, fmt.Errorf("store: read task %s in %s: %w", taskID, sc, err)
	}
	return t, nil
}

// ListTasks reads a loop's tasks in plan order.
func (s *Store) ListTasks(sc Scope) ([]Task, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`SELECT `+taskColumns+` FROM task WHERE `+where+` ORDER BY ord, taskid`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the tasks of %s: %w", sc, err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list the tasks of %s: %w", sc, err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTaskStatusResult is the outcome of a status change.
//
// Three states, not two: it happened, the machine refused it, or the
// infrastructure failed (DB19). A caller that cannot tell a refusal from a
// failure retries the wrong one.
type SetTaskStatusResult struct {
	OK bool

	// Transition carries the machine's answer, including the shortest legal
	// path, when the refusal came from the machine.
	Transition TransitionResult

	// Task is the row as it now stands, when OK.
	Task Task
}

// SetTaskStatus moves a task along the ladder, through the machine.
//
// The loop's MODE decides which table applies (`poc` removes the PR rungs), so
// it is read here rather than taken from the caller — a caller passing the mode
// could pass a different one from the loop's and get a transition the ladder
// does not actually allow.
func (s *Store) SetTaskStatus(sc Scope, taskID string, to TaskStatus) (SetTaskStatusResult, error) {
	current, err := s.GetTask(sc, taskID)
	if err != nil {
		return SetTaskStatusResult{}, err
	}
	l, err := s.GetLoop(sc)
	if err != nil {
		return SetTaskStatusResult{}, err
	}

	res := ValidateTransition(TaskStatus(current.Status), to, LoopMode(l.Mode))
	if !res.OK {
		// Refused, not failed. No row changes, and the error return is nil —
		// the caller reads OK, not err, to find out what happened.
		return SetTaskStatusResult{OK: false, Transition: res}, nil
	}

	where, args := sc.Where("")
	args = append([]any{string(to), s.Now()}, args...)
	args = append(args, taskID)
	if _, err := s.w.Exec(`UPDATE task SET status = ?, updated_at = ? WHERE `+where+` AND taskid = ?`, args...); err != nil {
		return SetTaskStatusResult{}, fmt.Errorf("store: set the status of %s in %s: %w", taskID, sc, err)
	}

	// Touch the loop too, so "every loop by recency" (2a) reflects work on its
	// tasks. A loop whose tasks are moving and whose updated_at is stale sorts
	// to the bottom of the one list a person looks at.
	loopWhere, loopArgs := sc.WhereLoop("")
	loopArgs = append([]any{s.Now()}, loopArgs...)
	if _, err := s.w.Exec(`UPDATE loop SET updated_at = ? WHERE `+loopWhere, loopArgs...); err != nil {
		return SetTaskStatusResult{}, fmt.Errorf("store: touch %s: %w", sc, err)
	}

	updated, err := s.GetTask(sc, taskID)
	if err != nil {
		return SetTaskStatusResult{}, err
	}
	return SetTaskStatusResult{OK: true, Transition: res, Task: updated}, nil
}

// TaskCounterColumns is the closed set of task-row counters — the columns
// BumpTaskCounter may touch, and the whitelist is what keeps a caller string
// out of SQL. S17's engine (internal/loop/counters.go) owns which counters
// exist and at what level; this is the row-mechanical half.
var TaskCounterColumns = []string{"retries", "security_rounds", "review_rounds"}

// BumpTaskCounter adds one to a task's counter and reports where it landed.
//
// capAt <= 0 means uncapped. Reaching the cap is INCLUSIVE (Caps(): reaching
// it is fine, exceeding is the caller's escalation signal), so a counter at
// its cap does not move further — a bump at the cap reports the cap and true
// without a write. This is the S5 discipline for the absolute step deadline,
// applied to budgets: a counter a caller can push past its cap is a cap that
// only exists in the comment above it.
//
// An unknown column is an error naming it: a typo'd column at this boundary
// is a programming fault, not a domain outcome (DB19 reserves discriminated
// results for "this transition is not allowed"-class answers).
func (s *Store) BumpTaskCounter(sc Scope, taskID, column string, capAt int) (int, bool, error) {
	legal := false
	for _, c := range TaskCounterColumns {
		if c == column {
			legal = true
			break
		}
	}
	if !legal {
		return 0, false, fmt.Errorf("store: unknown task counter column %q — the set is %v", column, TaskCounterColumns)
	}

	where, args := sc.Where("")
	// The placeholder must be in the SQL, not just the argument: measured
	// here, the CGO driver IGNORES an extra bind rather than erroring, so an
	// unscoped SELECT silently returns the loop's FIRST task and a bump
	// computes another task's budget. Both statements carry taskid.
	args = append(args, taskID)
	var current int
	if err := s.r.QueryRow(`SELECT `+column+` FROM task WHERE `+where+` AND taskid = ?`, args...).Scan(&current); err != nil {
		return 0, false, fmt.Errorf("store: read counter %s of task %s in %s: %w", column, taskID, sc, err)
	}
	if capAt > 0 && current >= capAt {
		return capAt, true, nil // clamped at the cap; no write, no drift
	}

	// column is whitelist-checked above; it can only ever be one of three
	// literals here, which is what makes splicing it into SQL safe.
	// The taskid belongs in the WHERE: every other task in the loop has its
	// own budget (D2), and a bump that updates every row in the loop is
	// exactly the per-loop-only failure S17 exists to refuse — the
	// independence tests catch it, on exactly this argument. (Measured here:
	// passing the argument WITHOUT the placeholder does not error on this
	// driver — the extra bind is ignored and every row moves. The
	// placeholder is the whole scoping; the argument alone is decoration.)
	update, uargs := sc.Where("")
	uargs = append(uargs, taskID)
	if _, err := s.w.Exec(`UPDATE task SET `+column+` = `+column+` + 1, updated_at = ? WHERE `+update+` AND taskid = ?`, append([]any{s.Now()}, uargs...)...); err != nil {
		return 0, false, fmt.Errorf("store: bump %s of task %s in %s: %w", column, taskID, sc, err)
	}
	// Touch the loop for recency sorting, the discipline SetTaskStatus keeps.
	loopWhere, loopArgs := sc.WhereLoop("")
	if _, err := s.w.Exec(`UPDATE loop SET updated_at = ? WHERE `+loopWhere, append([]any{s.Now()}, loopArgs...)...); err != nil {
		return 0, false, fmt.Errorf("store: touch %s: %w", sc, err)
	}
	value := current + 1
	return value, capAt > 0 && value >= capAt, nil
}

// LoopTaskEdit is the OWNING AGENT's edit of its loop's task (S38 T10): the
// read-only rule of OL14 is for PEOPLE on the socket, not for the agent the
// loop runs on. Title/tech empty keep; DependsOn nil keeps, non-nil
// REPLACES (and every name must already exist in the loop — the caller
// validates through plan, and this door refuses anyway if a name is gone
// underneath the argument).
type LoopTaskEdit struct {
	Title     string
	Tech      string
	DependsOn []string
}

// UpdateLoopTask writes as the loop's agent. The key stays the engine's
// (component_root, loop_slug, taskid); the surrogate id rides along,
// untouched — nothing here can change who owns the row.
func (s *Store) UpdateLoopTask(sc Scope, taskID string, in LoopTaskEdit) (Task, error) {
	if !sc.Valid() {
		return Task{}, fmt.Errorf("store: UpdateLoopTask needs both a slug and a component root, got %s", sc)
	}
	cur, err := s.GetTask(sc, taskID)
	if err != nil {
		return Task{}, err
	}
	if cur.Manual {
		return Task{}, fmt.Errorf("store: task %s in %s is a manual task — its doors are the person's (UpdateManualTask)", taskID, sc)
	}
	title, tech := cur.Title, cur.Tech
	if in.Title != "" {
		title = in.Title
	}
	if in.Tech != "" {
		tech = in.Tech
	}
	deps := cur.DependsOn
	if in.DependsOn != nil {
		deps = in.DependsOn
	}
	encoded, err := json.Marshal(deps)
	if err != nil {
		return Task{}, fmt.Errorf("store: encode depends_on for %s: %w", taskID, err)
	}
	if _, err := s.w.Exec(`UPDATE task SET title=?, tech=?, depends_on=?, updated_at=?
		WHERE component_root=? AND loop_slug=? AND taskid=?`,
		title, tech, string(encoded), s.Now(), sc.ComponentRoot, sc.LoopSlug, taskID); err != nil {
		return Task{}, fmt.Errorf("store: update loop task %s in %s: %w", taskID, sc, err)
	}
	return s.GetTask(sc, taskID)
}

// SetTaskEvidence records what the agent showed at a status rung (S38 T10:
// task_set_status {evidence?}). It writes last_evidence only — the ladder
// itself is SetTaskStatus's and the machine's, never this door's. The id is
// the surrogate: the rung being climbed is addressed whoever owns the task.
func (s *Store) SetTaskEvidence(id, evidence string) error {
	if _, err := s.w.Exec(`UPDATE task SET last_evidence=?, updated_at=? WHERE id=?`, evidence, s.Now(), id); err != nil {
		return fmt.Errorf("store: evidence of task %s: %w", id, err)
	}
	return nil
}

// IsLoopWorkerOfTask reports whether a loop is linked to a manual task as a
// WORKER (S38 T10's agent scope: a loop may work a manual task's ladder only
// when one was started on it).
func (s *Store) IsLoopWorkerOfTask(taskID string, sc Scope) (bool, error) {
	if !sc.Valid() {
		return false, fmt.Errorf("store: IsLoopWorkerOfTask needs both a slug and a component root, got %s", sc)
	}
	var one int
	err := s.r.QueryRow(`SELECT 1 FROM task_loop WHERE task_id = ? AND loop_slug = ? AND component_root = ? AND role = 'worker'`,
		taskID, sc.LoopSlug, sc.ComponentRoot).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: worker link of %s to %s: %w", taskID, sc, err)
	}
	return true, nil
}
