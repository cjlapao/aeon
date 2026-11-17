package store

// epics.go — the level above a goal (OL7). An epic is deliberately plain:
// title, objective, owner, target date, budget — and NO status column. A
// future status arrives by a new migration, never by an edit to 0036 (§3:
// shipped shape is immutable), and an epic's life is short enough that
// inventing a vocabulary for it now would be inventing it wrong.
//
// CT10: an epic belongs to a ROOT component, not a child — it is the funded
// stretch the whole tree pursues, so it hangs on the top of the tree. The
// column is component_id (renamed from project_id at T3); the value is a
// root's id.

import (
	"database/sql"
	"fmt"
)

// Epic is one funded stretch of work a root component pursues.
//
// S55 T8 (LC13): BudgetUSD is DERIVED, not stored. The epic's budget is the
// budget row (scope='epic', window='lifetime') the T6 migration backfilled —
// the single truth. The old epic budget column (the DDL still names it) is
// DEAD from here: it stays in the schema (dropping it is a rebuild S55 does
// not need) but is
// read by no production code and written by none — the DDL that keeps it
// alive is migration 0036, and a grep test (TestNoProductionReadOfEpicBudgetColumn)
// holds that promise.
type Epic struct {
	ID          string   `json:"id"`
	ComponentID string   `json:"componentId"`
	Title       string   `json:"title"`
	Objective   string   `json:"objective"`
	Owner       string   `json:"owner"`
	TargetDate  string   `json:"targetDate,omitempty"` // empty = none
	BudgetUSD   *float64 `json:"budgetUsd,omitempty"`  // derived from the budget row (LC13); nil = no USD cap (P31)
	SourceStep  *int64   `json:"sourceStep,omitempty"` // the step that made it, when a loop did
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// NewEpic is the create input. ComponentID is the ROOT a epic hangs on and is
// required; the root must exist and be a root (CT10, the FK + the door).
// S55 T8 (LC13): no budget here — a new epic has no budget row until a
// budget.set files one, and the view reads as nil (no USD cap) until then.
type NewEpic struct {
	ComponentID string
	Title       string
	Objective   string
	Owner       string
	TargetDate  string
	SourceStep  *int64
}

// EpicEdit is the update input. Keep-semantics per §4c's discipline: an empty
// string KEEPS the existing value; clearing the target date is its own
// intention and carries its own flag, because "" cannot tell "unset" from
// "do not change". S55 T8 (LC13): an update here never touches the budget —
// the cap's door is budget.set; the field that used to carry it is gone.
type EpicEdit struct {
	ID              string
	Title           string
	Objective       string
	Owner           string
	ClearTargetDate bool
	TargetDate      string
}

// CreateEpic files an epic under a root component.
func (s *Store) CreateEpic(in NewEpic) (Epic, error) {
	if in.ComponentID == "" || in.Title == "" {
		return Epic{}, fmt.Errorf("store: an epic needs a component and a title")
	}
	// CT10: an epic hangs on a root, never a child — the component must exist
	// and have no parent.
	if !s.isRoot(in.ComponentID) {
		return Epic{}, fmt.Errorf("store: component %q is not a root — an epic hangs on the top of the tree", in.ComponentID)
	}
	now := s.Now()
	out := Epic{ID: "epc-" + newID(), ComponentID: in.ComponentID, Title: in.Title,
		Objective: in.Objective, Owner: in.Owner, TargetDate: in.TargetDate,
		SourceStep: in.SourceStep, CreatedAt: now, UpdatedAt: now}
	// S55 T8 (LC13): the INSERT no longer names the dead epic budget column
	// — it defaults to NULL, and the epic's budget, when one is set, is the
	// budget row, not a value stored beside the epic.
	_, err := s.w.Exec(`INSERT INTO epic (id,component_id,title,objective,owner,target_date,source_step,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		out.ID, out.ComponentID, out.Title, out.Objective, out.Owner,
		nullable(out.TargetDate), out.SourceStep, now, now)
	if err != nil {
		return Epic{}, fmt.Errorf("store: create epic %q: %w", in.Title, err)
	}
	return out, nil
}

// UpdateEpic changes an epic's plan. There is no status to set — there is no
// status column, by design. S55 T8 (LC13): the UPDATE no longer names the
// dead budget column — an update here changes the plan, never the cap
// (budget.set is the cap's door), and the derived BudgetUSD the read carried
// is left alone.
func (s *Store) UpdateEpic(in EpicEdit) (Epic, error) {
	cur, err := s.GetEpic(in.ID)
	if err != nil {
		return Epic{}, err
	}
	target := cur.TargetDate
	if in.ClearTargetDate {
		target = ""
	} else if in.TargetDate != "" {
		target = in.TargetDate
	}
	title, objective, owner := cur.Title, cur.Objective, cur.Owner
	if in.Title != "" {
		title = in.Title
	}
	if in.Objective != "" {
		objective = in.Objective
	}
	if in.Owner != "" {
		owner = in.Owner
	}
	now := s.Now()
	if _, err := s.w.Exec(`UPDATE epic SET title=?, objective=?, owner=?, target_date=?, updated_at=? WHERE id=?`,
		title, objective, owner, nullable(target), now, in.ID); err != nil {
		return Epic{}, fmt.Errorf("store: update epic %s: %w", in.ID, err)
	}
	cur.Title, cur.Objective, cur.Owner, cur.TargetDate, cur.UpdatedAt = title, objective, owner, target, now
	return cur, nil
}

// epicBudgetUSD is the view's derived budgetUsd (LC13): the USD cap of the
// epic's lifetime budget row — the single truth the T6 migration backfilled —
// or nil when the epic has no such row, or the row carries no USD cap (P31:
// nil is "no USD cap", never a hidden zero). The dead epic budget column is
// no longer read; this is the one reader of the budget row for the view.
func (s *Store) epicBudgetUSD(epicID string) (*float64, error) {
	b, ok, err := s.FindBudget("epic", epicID, "lifetime")
	if err != nil {
		return nil, fmt.Errorf("store: the budget of epic %s: %w", epicID, err)
	}
	if !ok {
		return nil, nil
	}
	return b.USD, nil
}

// GetEpic reads one epic by id. A missing epic is an error, not a zero — the
// caller asked for a named thing and the answer "no" is infrastructure-shaped
// here the way GetProject made it refusal-shaped for the CLI: the records
// readers are called by handlers that already validated existence.
func (s *Store) GetEpic(id string) (Epic, error) {
	// S55 T8 (LC13): the SELECT no longer names the dead budget column —
	// the view's budgetUsd is derived from the epic's lifetime budget row.
	row := s.r.QueryRow(`SELECT id, component_id, title, objective, owner, COALESCE(target_date,''), source_step, created_at, updated_at
		FROM epic WHERE id = ?`, id)
	var e Epic
	var target sql.NullString
	var step sql.NullInt64
	if err := row.Scan(&e.ID, &e.ComponentID, &e.Title, &e.Objective, &e.Owner, &target, &step, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return Epic{}, fmt.Errorf("store: epic %q does not exist", id)
		}
		return Epic{}, fmt.Errorf("store: read epic %s: %w", id, err)
	}
	e.TargetDate = target.String
	if step.Valid {
		v := step.Int64
		e.SourceStep = &v
	}
	budget, err := s.epicBudgetUSD(id)
	if err != nil {
		return Epic{}, err
	}
	e.BudgetUSD = budget
	return e, nil
}

// ListEpics lists a root component's epics, most recently touched first.
// S55 T8 (LC13): the SELECT no longer names the dead budget column — each
// row's budgetUsd is derived from its lifetime budget row, one extra read per
// epic (a root's epics are few; the list stays as fast as the epics table
// and as true as the budget table).
func (s *Store) ListEpics(componentID string) ([]Epic, error) {
	rows, err := s.r.Query(`SELECT id, component_id, title, objective, owner, COALESCE(target_date,''), source_step, created_at, updated_at
		FROM epic WHERE component_id = ? ORDER BY updated_at DESC, id`, componentID)
	if err != nil {
		return nil, fmt.Errorf("store: list epics: %w", err)
	}
	defer rows.Close()
	var out []Epic
	for rows.Next() {
		var e Epic
		var target sql.NullString
		var step sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ComponentID, &e.Title, &e.Objective, &e.Owner, &target, &step, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan epic: %w", err)
		}
		e.TargetDate = target.String
		if step.Valid {
			v := step.Int64
			e.SourceStep = &v
		}
		budget, err := s.epicBudgetUSD(e.ID)
		if err != nil {
			return nil, err
		}
		e.BudgetUSD = budget
		out = append(out, e)
	}
	return out, rows.Err()
}
