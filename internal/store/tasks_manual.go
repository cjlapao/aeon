package store

// tasks_manual.go — OL14's second population. A manual task is made by a
// person (or an agent acting for one, T9's stamped tools), belongs to no loop
// at birth, carries a type, an assignee of either kind, an optional goal and
// parent, and — when RequireApproval says so — waits for a PERSON's approval
// (the carve-out: what approval then TRIGGERS is S40's business).
//
// The two populations share the table and the ladder but not the doors:
// loop tasks are the engine's (SetTaskStatus untouched; the update paths here
// refuse them with a sentence naming the owning loop), manual tasks are the
// person's. `manual` is stamped at creation and is not settable anywhere —
// a grep-pin in tasks_manual_test.go enforces that no SQL writes it.

import (
	"database/sql"
	"errors"
	"fmt"
)

// NewManualTask is the create input. ComponentID is required (the board
// filters by component; a task of nowhere cannot be filed), Type must be one
// of OL14's five, and Body — when given — becomes the task's OWN attached
// document (`tasks/{id}.md` in the task's document namespace, OL4: the prose
// stays the author's, the pointer is the row).
type NewManualTask struct {
	ComponentID  string
	Title        string
	Type         string
	Body         string
	AssigneeKind string // "agent" or "user"; empty means unassigned
	Assignee     string
	GoalID       string
	ParentTaskID string
	By           Actor
	// RequireApproval files the task waiting on a person's DecideTask; without
	// it the task is born actionable (approval_state 'none') — the choice is
	// the author's, not a default the store sneaks in.
	RequireApproval bool
	SourceStep      *int64
	// OriginLoopID (S40 AU5) names the loop whose AGENT filed this task —
	// the dispatcher's depth and cause are derived from that loop's row,
	// never passed in. "" means a person filed it. It is a loop id, never a
	// number: an unknown id fails closed in the helper rather than resetting
	// depth to 0.
	OriginLoopID string
}

// ManualTaskEdit changes a manual task. There is NO Manual field: the flag is
// stamped at creation and no edit can touch it (the same argument §4c made
// about a credential a response cannot carry — a thing that must never change
// has nowhere to be changed from). Empty string KEEPS (the §4c discipline);
// clearing the parent is its own intention with its own flag.
type ManualTaskEdit struct {
	ID            string
	Title         string
	Type          string
	AssigneeKind  string
	Assignee      string
	ClearAssignee bool
	GoalID        string
	ParentTaskID  string
	ClearParent   bool
	ComponentID   string
}

// CreateManualTask files a hand-made task.
func (s *Store) CreateManualTask(in NewManualTask) (Task, *Refusal, error) {
	if !in.By.validActor() {
		return Task{}, &Refusal{Kind: "actor-unknown",
			Sentence: fmt.Sprintf("refused: author kind %q is not user, agent, or trigger (or the author has no id) — a manual task names who filed it", in.By.Kind)}, nil
	}
	if in.Title == "" {
		return Task{}, &Refusal{Kind: "title-empty",
			Sentence: "refused: a manual task needs a title"}, nil
	}
	if !allGoalTypes[in.Type] {
		return Task{}, &Refusal{Kind: "type-unknown",
			Sentence: fmt.Sprintf("refused: task type %q is not one of bug, feature, chore, refactor, investigation (OL14's five)", in.Type)}, nil
	}
	if in.AssigneeKind != "" && in.AssigneeKind != "agent" && in.AssigneeKind != "user" {
		return Task{}, &Refusal{Kind: "assignee-kind-unknown",
			Sentence: fmt.Sprintf("refused: assignee kind %q is not agent or user — and an unassigned task names nobody, which is its own kind: none", in.AssigneeKind)}, nil
	}
	approval := "none"
	if in.RequireApproval {
		approval = "pending"
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Task{}, nil, fmt.Errorf("store: create manual task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var one int
	if err := tx.QueryRow(`SELECT 1 FROM component WHERE id = ?`, in.ComponentID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, nil, fmt.Errorf("store: component %q does not exist — a manual task is filed in a component", in.ComponentID)
		}
		return Task{}, nil, fmt.Errorf("store: check component %s: %w", in.ComponentID, err)
	}
	if in.GoalID != "" {
		if err := tx.QueryRow(`SELECT 1 FROM goal WHERE id = ?`, in.GoalID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Task{}, nil, fmt.Errorf("store: goal %q does not exist", in.GoalID)
			}
			return Task{}, nil, fmt.Errorf("store: check goal %s: %w", in.GoalID, err)
		}
	}
	if in.ParentTaskID != "" {
		// The same-component rule, store-enforced (there is no FK, per 0036's
		// comment): the *what* decomposes into the *how*, and decomposition
		// does not cross components.
		err := tx.QueryRow(`SELECT 1 FROM task WHERE id = ? AND component_id = ?`, in.ParentTaskID, in.ComponentID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, nil, fmt.Errorf("store: parent task %s is not a task of component %s — decomposition stays within one component", in.ParentTaskID, in.ComponentID)
		} else if err != nil {
			return Task{}, nil, fmt.Errorf("store: check parent task %s: %w", in.ParentTaskID, err)
		}
	}

	now := s.Now()
	t := Task{ID: "tsk-" + newID(), Title: in.Title, Type: in.Type,
		ComponentID: in.ComponentID, GoalID: in.GoalID, ParentTaskID: in.ParentTaskID,
		AssigneeKind: in.AssigneeKind, Assignee: in.Assignee,
		Status: string(StatusPending), ApprovalState: approval, Manual: true,
		CreatedByKind: in.By.Kind, CreatedByID: in.By.ID, SourceStep: in.SourceStep,
		CreatedAt: now, UpdatedAt: now}
	if _, err := tx.Exec(`INSERT INTO task (id,title,status,updated_at,created_at,
		manual,type,assignee_kind,assignee,component_id,goal_id,parent_task_id,
		approval_state,created_by_kind,created_by_id,source_step)
		VALUES (?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Title, t.Status, now, now, t.Type,
		nullable(t.AssigneeKind), nullable(t.Assignee), t.ComponentID, nullable(t.GoalID), nullable(t.ParentTaskID),
		t.ApprovalState, t.CreatedByKind, t.CreatedByID, t.SourceStep); err != nil {
		return Task{}, nil, fmt.Errorf("store: insert manual task %q: %w", t.Title, err)
	}

	// The body is OL4's division in action: the prose is a DOCUMENT the author
	// owns, and the task keeps only the pointer. The namespace is the TASK's
	// (its surrogate id, under its component's primary root), because a manual
	// task has no loop to borrow — and the body belongs to the task, not to
	// whatever loop is later started on it. Attach/LatestBrief never look
	// there; only ListAttachments("task", id) and ReadManualTaskBody do.
	if in.Body != "" {
		att, r, err := s.attachTaskBody(tx, t.ID, t.ComponentID, in.Body, in.By.Kind+":"+t.CreatedByID)
		if err != nil || r != nil {
			return Task{}, r, err
		}
		_ = att
	}
	// S40 AU3/AU4: this is the manual branch by construction — the only
	// create path that reaches here stamps manual=1, and the loop-task
	// creation door (tasks.go) has no such call. Depth and cause ride the
	// helper's reading of OriginLoopID's row, never an input number.
	if err := insertAutomationEvent(tx, now, AutomationEventIn{
		Event: EventTaskCreated, SubjectKind: "task", SubjectID: t.ID,
		RootID: componentRoot(tx, t.ComponentID), ComponentID: t.ComponentID,
		TaskType: t.Type, Manual: true, OriginLoopID: in.OriginLoopID,
	}); err != nil {
		return Task{}, nil, err
	}
	// Created-as-approved is not a state this door can open — birth is
	// 'none' or 'pending', so this arm is defensive against a future door
	// that pre-approves (the second event's twin lives in DecideTask).
	if approval == "approved" {
		if err := insertAutomationEvent(tx, now, AutomationEventIn{
			Event: EventTaskApproved, SubjectKind: "task", SubjectID: t.ID,
			RootID: componentRoot(tx, t.ComponentID), ComponentID: t.ComponentID,
			TaskType: t.Type, Manual: true, OriginLoopID: in.OriginLoopID,
		}); err != nil {
			return Task{}, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Task{}, nil, fmt.Errorf("store: create manual task: %w", err)
	}
	return t, nil, nil
}

// attachTaskBody writes the body document + its attachment inside the
// creation transaction.
func (s *Store) attachTaskBody(tx *sql.Tx, taskID, componentID, body, writtenBy string) (Attachment, *Refusal, error) {
	var root sql.NullString
	if err := tx.QueryRow(`SELECT component_root FROM component_root WHERE component_id = ? AND is_primary = 1`, componentID).Scan(&root); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attachment{}, &Refusal{Kind: "component-has-no-root",
				Sentence: fmt.Sprintf("refused: component %s has no registered workspace — a body is a document, and a document lives in a workspace", componentID)}, nil
		}
		return Attachment{}, nil, fmt.Errorf("store: root of component %s: %w", componentID, err)
	}
	// Write the document row through the SAME machinery WriteDocument uses
	// (gzip + sha), under the task's own namespace.
	sc := Scope{LoopSlug: taskID, ComponentRoot: root.String}
	doc, err := s.writeDocumentInTx(tx, sc, "tasks/"+taskID+".md", body, writtenBy)
	if err != nil {
		return Attachment{}, nil, err
	}
	a := Attachment{SubjectKind: "task", SubjectID: taskID, ComponentRoot: root.String,
		LoopSlug: taskID, Document: doc.File, Rev: doc.Rev, CreatedAt: s.Now()}
	res, err := tx.Exec(`INSERT INTO attachment (subject_kind,subject_id,loop_slug,component_root,document,rev,created_at)
		VALUES (?,?,?,?,?,?,?)`, a.SubjectKind, a.SubjectID, a.LoopSlug, a.ComponentRoot, a.Document, a.Rev, a.CreatedAt)
	if err != nil {
		return Attachment{}, nil, fmt.Errorf("store: attach body to %s: %w", taskID, err)
	}
	if id, err := res.LastInsertId(); err == nil {
		a.ID = id
	}
	return a, nil, nil
}

// UpdateManualTask changes a manual task — and REFUSES a loop task with the
// sentence OL14 demands, naming the loop whose agent manages it. The engine's
// door is SetTaskStatus (unchanged); this door is the person's.
func (s *Store) UpdateManualTask(in ManualTaskEdit) (Task, *Refusal, error) {
	cur, err := s.getTaskByID(in.ID)
	if err != nil {
		return Task{}, nil, err
	}
	if !cur.Manual {
		return Task{}, &Refusal{Kind: "loop-task-read-only",
			Sentence: fmt.Sprintf("refused: task %s belongs to loop %s and is managed by its agent; it cannot be edited here", in.ID, cur.LoopSlug)}, nil
	}
	if in.Type != "" && !allGoalTypes[in.Type] {
		return Task{}, &Refusal{Kind: "type-unknown",
			Sentence: fmt.Sprintf("refused: task type %q is not one of bug, feature, chore, refactor, investigation", in.Type)}, nil
	}
	assigneeKind, assignee := cur.AssigneeKind, cur.Assignee
	if in.ClearAssignee {
		assigneeKind, assignee = "", ""
	} else if in.Assignee != "" {
		assignee = in.Assignee
		if in.AssigneeKind != "" {
			assigneeKind = in.AssigneeKind
		}
	}
	if (in.Assignee != "" || in.AssigneeKind != "") && !in.ClearAssignee {
		if assigneeKind != "agent" && assigneeKind != "user" {
			return Task{}, &Refusal{Kind: "assignee-kind-unknown",
				Sentence: fmt.Sprintf("refused: assignee kind %q is not agent or user", assigneeKind)}, nil
		}
	}
	parent := cur.ParentTaskID
	if in.ClearParent {
		parent = ""
	} else if in.ParentTaskID != "" {
		parent = in.ParentTaskID
	}
	title, etype, componentID, goal := cur.Title, cur.Type, cur.ComponentID, cur.GoalID
	if in.Title != "" {
		title = in.Title
	}
	if in.Type != "" {
		etype = in.Type
	}
	if in.ComponentID != "" {
		componentID = in.ComponentID
	}
	if in.GoalID != "" {
		goal = in.GoalID
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Task{}, nil, fmt.Errorf("store: update manual task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if parent != "" {
		var one int
		err := tx.QueryRow(`SELECT 1 FROM task WHERE id = ? AND component_id = ?`, parent, componentID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, nil, fmt.Errorf("store: parent task %s is not a task of component %s — decomposition stays within one component", parent, componentID)
		} else if err != nil {
			return Task{}, nil, fmt.Errorf("store: check parent task %s: %w", parent, err)
		}
	}
	if componentID != "" {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM component WHERE id = ?`, componentID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Task{}, nil, fmt.Errorf("store: component %q does not exist", componentID)
			}
			return Task{}, nil, fmt.Errorf("store: check component %s: %w", componentID, err)
		}
	}
	if _, err := tx.Exec(`UPDATE task SET title=?, type=?, assignee_kind=?, assignee=?, component_id=?, goal_id=?, parent_task_id=?, updated_at=?
		WHERE id=? AND manual=1`,
		title, nullable(etype), nullable(assigneeKind), nullable(assignee), nullable(componentID), nullable(goal), nullable(parent), s.Now(), in.ID); err != nil {
		return Task{}, nil, fmt.Errorf("store: update manual task %s: %w", in.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Task{}, nil, fmt.Errorf("store: update manual task: %w", err)
	}
	cur.Title, cur.Type, cur.AssigneeKind, cur.Assignee, cur.ComponentID, cur.GoalID, cur.ParentTaskID = title, etype, assigneeKind, assignee, componentID, goal, parent
	cur.UpdatedAt = s.Now()
	return cur, nil, nil
}

// SetManualTaskStatus walks a MANUAL task along the same ladder — Do-NOT
// #4: there is no second vocabulary. ModeStandard's table applies because a
// manual task has no loop to carry a mode; the machine is the machine.
func (s *Store) SetManualTaskStatus(id string, to TaskStatus) (SetTaskStatusResult, *Refusal, error) {
	cur, err := s.getTaskByID(id)
	if err != nil {
		return SetTaskStatusResult{}, nil, err
	}
	if !cur.Manual {
		return SetTaskStatusResult{}, &Refusal{Kind: "loop-task-read-only",
			Sentence: fmt.Sprintf("refused: task %s belongs to loop %s — its status is the engine's door (SetTaskStatus), not this one", id, cur.LoopSlug)}, nil
	}
	res := ValidateTransition(TaskStatus(cur.Status), to, ModeStandard)
	if !res.OK {
		return SetTaskStatusResult{OK: false, Transition: res}, nil, nil
	}
	// S40 T2: this door wrote with a bare s.w.Exec until today (T0-Q1
	// measured it), which left no transaction for an outbox row to share.
	// It is wrapped now — the status UPDATE and the task.completed event
	// commit together or not at all (AU4). The re-read stays OUTSIDE, after
	// the commit: it runs on the reader pool, which cannot see an
	// uncommitted row.
	tx, err := s.w.Begin()
	if err != nil {
		return SetTaskStatusResult{}, nil, fmt.Errorf("store: set manual task status: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	// AND status<>to is the claimed-FROM twin of the other three sites, and
	// it matters HERE specifically: ValidateTransition treats from==to as OK
	// (the machine's idempotent replay path, machine.go:244), so a
	// re-delivered complete reaches this UPDATE — matching its own target,
	// RowsAffected would say one, and the same completion would emit twice.
	// "Only write rows that are not already there" makes the replay match
	// nothing: the caller's answer is unchanged (OK, from the machine) and
	// the outbox says each completion once.
	upd, err := tx.Exec(`UPDATE task SET status=?, updated_at=? WHERE id=? AND manual=1 AND status<>?`, string(to), now, id, string(to))
	if err != nil {
		return SetTaskStatusResult{}, nil, fmt.Errorf("store: set manual task %s status: %w", id, err)
	}
	n, _ := upd.RowsAffected()
	// S40 AU3: completion emits, and ONLY completion (isCompletedTaskStatus
	// — the ladder's other rungs are not done-ness). n==1 is the same
	// claimed-FROM honesty as the other three sites: a write that matched
	// nothing emits nothing. The origin is "": this function takes no scope,
	// and inventing a loop for a write nobody named would be the depth-lie
	// AU5 forbids from the other side.
	if n == 1 && isCompletedTaskStatus(to) {
		if err := insertAutomationEvent(tx, now, AutomationEventIn{
			Event: EventTaskCompleted, SubjectKind: "task", SubjectID: id,
			RootID: componentRoot(tx, cur.ComponentID), ComponentID: cur.ComponentID,
			TaskType: cur.Type, Manual: true,
		}); err != nil {
			return SetTaskStatusResult{}, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SetTaskStatusResult{}, nil, fmt.Errorf("store: set manual task status: %w", err)
	}
	updated, err := s.getTaskByID(id)
	if err != nil {
		return SetTaskStatusResult{}, nil, err
	}
	return SetTaskStatusResult{OK: true, Transition: res, Task: updated}, nil, nil
}

// DecideTask is the human door OL14 carves out: only a manual task, only a
// user, claimed-FROM `approval_state='pending'` — the first person decides,
// the second is told who did. `parked` is NOT a task decision: a parked
// approval is a shelved goal, not a task verdict (approved|rejected only).
func (s *Store) DecideTask(id string, decision string, by Actor, reason string) (DecideResult, *Refusal, error) {
	if !by.human() {
		return DecideResult{}, &Refusal{Kind: "decider-not-human",
			Sentence: fmt.Sprintf("refused: only a user can approve a task · caller kind %q is not a person (OL8)", by.Kind)}, nil
	}
	if decision != "approved" && decision != "rejected" {
		return DecideResult{}, &Refusal{Kind: "decision-unknown",
			Sentence: fmt.Sprintf("refused: task approval is approved or rejected — %q is neither (a task is parked nowhere)", decision)}, nil
	}
	cur, err := s.getTaskByID(id)
	if err != nil {
		return DecideResult{}, nil, err
	}
	if !cur.Manual {
		return DecideResult{}, &Refusal{Kind: "loop-task-not-approvable",
			Sentence: fmt.Sprintf("refused: task %s belongs to loop %s — loop tasks carry no approval (the CHECK of 0036 makes that a schema fact)", id, cur.LoopSlug)}, nil
	}
	if cur.ApprovalState != "pending" {
		out := DecideResult{Changed: false, State: cur.ApprovalState}
		if prior, ok := s.latestReview("task", id, 0); ok {
			out.DecidedBy, out.DecidedAt = prior.ByID, prior.DecidedAt
		}
		return out, nil, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	res, err := tx.Exec(`UPDATE task SET approval_state=?, updated_at=? WHERE id=? AND manual=1 AND approval_state='pending'`,
		decision, now, id)
	if err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide task %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Somebody claimed it between the read and the write — the ledger
		// names them.
		_ = tx.Rollback()
		fresh, err := s.getTaskByID(id)
		if err != nil {
			return DecideResult{}, nil, err
		}
		out := DecideResult{Changed: false, State: fresh.ApprovalState}
		if prior, ok := s.latestReview("task", id, 0); ok {
			out.DecidedBy, out.DecidedAt = prior.ByID, prior.DecidedAt
		}
		return out, nil, nil
	}
	// A task has no version — subject_version 0 is its coordinate.
	if err := reviewInsert(tx, "task", id, 0, decision, by.ID, reason, now); err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: record decision on task %s: %w", id, err)
	}
	// S40 AU3/AU4: only an APPROVAL emits (a rejection is silence, AU3), on
	// the claimed-FROM branch that changed one row — and the origin is "":
	// approval is a human door (AU1), no loop caused a person to decide.
	if decision == "approved" {
		if err := insertAutomationEvent(tx, now, AutomationEventIn{
			Event: EventTaskApproved, SubjectKind: "task", SubjectID: id,
			RootID: componentRoot(tx, cur.ComponentID), ComponentID: cur.ComponentID,
			TaskType: cur.Type, Manual: true,
		}); err != nil {
			return DecideResult{}, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide task: %w", err)
	}
	return DecideResult{Changed: true, State: decision, DecidedBy: by.ID, DecidedAt: now}, nil, nil
}

// LinkTaskToLoop connects a task and a loop (OL7's task_loop). A manual task
// gains a WORKER (the loop was started on it — S39's loop.start {TaskID});
// the OWNER row belongs to a loop task's own loop and CreateTask wrote it.
// Re-linking the same pair with the same role is the no-op a retried tool
// call needs; changing a role is a different fact and is refused.
func (s *Store) LinkTaskToLoop(taskID string, sc Scope, role string) error {
	if !sc.Valid() {
		return fmt.Errorf("store: LinkTaskToLoop needs both a slug and a component root, got %s", sc)
	}
	if role != "worker" && role != "owner" {
		return fmt.Errorf("store: task_loop role %q is not worker or owner", role)
	}
	t, err := s.getTaskByID(taskID)
	if err != nil {
		return err
	}
	if role == "owner" && (t.Manual || t.LoopSlug != sc.LoopSlug || t.ComponentRoot != sc.ComponentRoot) {
		return fmt.Errorf("store: the owner row belongs to the loop that planned the task — %s is %s", taskID, "not that loop")
	}
	if role == "worker" && !t.Manual {
		return fmt.Errorf("store: a loop task is owned by exactly one loop (%s) — worker rows are for manual tasks a loop was started on", t.LoopSlug)
	}
	var one int
	if err := s.r.QueryRow(`SELECT 1 FROM loop WHERE component_root = ? AND slug = ?`, sc.ComponentRoot, sc.LoopSlug).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: loop %s does not exist — a link names a loop that is", sc)
		}
		return fmt.Errorf("store: check loop %s: %w", sc, err)
	}
	if _, err := s.w.Exec(`INSERT INTO task_loop (task_id, loop_slug, component_root, role) VALUES (?,?,?,?)`,
		taskID, sc.LoopSlug, sc.ComponentRoot, role); err != nil {
		if isUniqueViolation(err) {
			var have string
			_ = s.r.QueryRow(`SELECT role FROM task_loop WHERE task_id=? AND loop_slug=? AND component_root=?`,
				taskID, sc.LoopSlug, sc.ComponentRoot).Scan(&have)
			if have == role {
				return nil // idempotent: the retried call states the same fact
			}
			return fmt.Errorf("store: %s is already %s of %s — a role change is a different fact, not a re-link", taskID, have, sc)
		}
		return fmt.Errorf("store: link %s to %s: %w", taskID, sc, err)
	}
	return nil
}

// ReadManualTaskBody returns the body document's content — the pointer's
// other half. A task with no body reads as empty, not an error: absent is a
// fact, missing is a state (the attachment's currency says which).
func (s *Store) ReadManualTaskBody(taskID string) (string, error) {
	var root, slug, file string
	var rev int
	err := s.r.QueryRow(`SELECT component_root, loop_slug, document, rev FROM attachment
		WHERE subject_kind='task' AND subject_id=? ORDER BY created_at DESC, id DESC LIMIT 1`, taskID).
		Scan(&root, &slug, &file, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: body pointer of %s: %w", taskID, err)
	}
	doc, err := s.ReadDocument(Scope{LoopSlug: slug, ComponentRoot: root}, file)
	if err != nil {
		return "", fmt.Errorf("store: body of %s: %w", taskID, err)
	}
	_ = rev
	return doc.Content, nil
}

// GetTaskByID exports the surrogate-id read for the records service (T9's
// attach guard reads the task whose pointer is being pinned).
func (s *Store) GetTaskByID(id string) (Task, error) { return s.getTaskByID(id) }
