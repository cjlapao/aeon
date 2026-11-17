// intake_records.go — S61 T6 (GI13): the intake commit's records land in
// ONE transaction. The engine owns order and the ledger; this file owns the
// SQL, because SQL stays in internal/store. Epics, goals (straight to
// `in_review` — GI14's no auto-approval), their component stakes and
// criteria, and roadmap items (status `idea`, provenance `intake`) all
// commit together or not at all: a refusal on the seventh goal leaves no
// epic, and the ledger says the records step failed. `Fault`, nil in
// production, is the crash matrix's mid-transaction kill: it runs inside
// the transaction, and everything it aborts rolls back — the same rows a
// lost process would have left uncommitted.
package store

import (
	"database/sql"
	"fmt"
)

// IntakeEpicRow / IntakeGoalRow / IntakeItemRow are the commit-time shapes:
// every reference is already resolved to an id (the engine resolved slugs and
// the root), and EpicKey is the plan's own key — the transaction mints the
// epic's id and files goals under it by key, so one write cannot cross the
// plan's keys and the store's ids.
type IntakeEpicRow struct {
	Key       string
	Title     string
	Objective string
}

type IntakeGoalRow struct {
	EpicKey    string // "" = no epic
	Title      string
	Type       string
	BodyNote   string
	Components []GoalComponent
	Criteria   []Criterion
}

type IntakeItemRow struct {
	ComponentRoot string // the component's root PATH (the FK rides component_root, as the person's roadmap.add carries)
	Title         string
	EpicKey       string // the plan's key; stored verbatim in the epic column
	PriorityState string
	PriorityLabel *string
	EstimateMin   *int
	EstimateMax   *int
	EstimateBasis *string
}

// IntakeRecordsInput is one intake's whole records step. ComponentID is the
// ROOT the epics and goals hang on (CT10 — the engine resolved it after the
// tree step); Author is the committer, who is the records' honest author
// (GI11's attribution law, carried through records too).
type IntakeRecordsInput struct {
	IntakeID    string
	ComponentID string
	Author      Actor
	Epics       []IntakeEpicRow
	Goals       []IntakeGoalRow
	Items       []IntakeItemRow
	// Fault is the crash matrix's scalpel, nil in production: it runs in
	// the transaction after goalsDone goals are inserted, and any error
	// aborts — and rolls back — the whole step.
	Fault func(goalsDone int) error
}

// IntakeRecordsResult counts what the one transaction filed.
type IntakeRecordsResult struct {
	Epics   int
	Goals   int
	Items   int
	GoalIDs []string
}

// ApplyIntakeRecords files the whole records step atomically. The rules it
// keeps are the plain doors' rules, not new ones: goals enter `in_review`
// (Submit's word, never `draft` and never `approved`), items enter `idea`
// with D3/D2's estimate and priority shapes re-checked by the same helpers
// the plain creators use, and provenance is `intake` because that is who
// filed it (GI13, OD9).
func (s *Store) ApplyIntakeRecords(in IntakeRecordsInput) (IntakeRecordsResult, error) {
	var out IntakeRecordsResult
	if in.IntakeID == "" || in.ComponentID == "" {
		return out, fmt.Errorf("store: intake records need the intake and the root they file under")
	}
	if !in.Author.validActor() {
		return out, fmt.Errorf("store: intake records need a well-formed author — these records name who filed them")
	}
	if !s.isRoot(in.ComponentID) {
		return out, fmt.Errorf("store: component %q is not a root — an intake's epics and goals hang on the top of the tree", in.ComponentID)
	}
	seenEpic := map[string]bool{}
	for _, e := range in.Epics {
		if e.Key == "" || e.Title == "" {
			return out, fmt.Errorf("store: an intake epic needs its plan key and a title")
		}
		if seenEpic[e.Key] {
			return out, fmt.Errorf("store: epic key %q appears twice — the plan's keys are its identity", e.Key)
		}
		seenEpic[e.Key] = true
	}

	tx, err := s.w.Begin()
	if err != nil {
		return out, fmt.Errorf("store: intake records: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()

	epicID := map[string]string{}
	for _, e := range in.Epics {
		id := "epc-" + newID()
		if _, err := tx.Exec(`INSERT INTO epic (id,component_id,title,objective,owner,target_date,source_step,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			id, in.ComponentID, e.Title, e.Objective, "", nil, nil, now, now); err != nil {
			return out, fmt.Errorf("store: intake records: file epic %q: %w", e.Key, err)
		}
		epicID[e.Key] = id
		out.Epics++
	}

	for i, g := range in.Goals {
		if g.Title == "" {
			return out, fmt.Errorf("store: intake records: goal %d has no title", i+1)
		}
		if !allGoalTypes[g.Type] {
			return out, fmt.Errorf("store: intake records: goal %q type %q is not one of bug, feature, chore, refactor, investigation", g.Title, g.Type)
		}
		goalID := "goal-" + newID()
		var eid any
		if g.EpicKey != "" {
			id, ok := epicID[g.EpicKey]
			if !ok {
				return out, fmt.Errorf("store: intake records: goal %q names epic key %q and the plan files no such epic", g.Title, g.EpicKey)
			}
			eid = id
		}
		if _, err := tx.Exec(`INSERT INTO goal (id,component_id,epic_id,version,title,type,body_note,source_kind,source_ref,source_loop,source_step,review_state,created_by_kind,created_by_id,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			goalID, in.ComponentID, eid, 1, g.Title, g.Type, g.BodyNote,
			"intake", in.IntakeID, nil, nil, "in_review", in.Author.Kind, in.Author.ID, now, now); err != nil {
			return out, fmt.Errorf("store: intake records: file goal %q: %w", g.Title, err)
		}
		if _, err := insertGoalComponents(tx, goalID, 1, g.Components); err != nil {
			return out, err
		}
		if err := fileIntakeCriteria(tx, goalID, 1, g.Criteria); err != nil {
			return out, err
		}
		out.GoalIDs = append(out.GoalIDs, goalID)
		out.Goals++
		if in.Fault != nil {
			if err := in.Fault(i + 1); err != nil {
				return out, fmt.Errorf("store: intake records aborted at goal %d: %w", i+1, err)
			}
		}
	}

	for _, it := range in.Items {
		row := NewRoadmapItemRow{
			ComponentRoot: it.ComponentRoot, Title: it.Title,
			PriorityState: it.PriorityState, PriorityLabel: it.PriorityLabel,
			EstimateMin: it.EstimateMin, EstimateMax: it.EstimateMax, EstimateBasis: it.EstimateBasis,
			Provenance: "intake",
		}
		if it.EpicKey != "" {
			k := it.EpicKey
			row.Epic = &k // the plan's own key rides the epic column — GI13's "epic by key"
		}
		if err := checkNewRoadmapItemShape(row); err != nil {
			return out, err
		}
		if row.PriorityState == "" {
			row.PriorityState = "unknown"
		}
		if _, err := tx.Exec(createRoadmapItemSQL,
			row.ComponentRoot, row.Title, row.Epic, "idea", row.PriorityState, row.PriorityLabel,
			row.EstimateMin, row.EstimateMax, row.EstimateBasis, row.Provenance, nil, nil, now, now); err != nil {
			return out, fmt.Errorf("store: intake records: file item %q: %w", it.Title, err)
		}
		out.Items++
	}

	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("store: intake records: %w", err)
	}
	return out, nil
}

// fileIntakeCriteria writes one goal version's criteria inside the shared
// transaction. The laws are ReplaceGoalCriteria's — same caps, same
// closed check set, same ordinal and id rules — restated here because the
// records step files a FRESH goal (nothing to replace) and must join one
// transaction the standalone writer's own Begin cannot join.
func fileIntakeCriteria(tx *sql.Tx, goalID string, version int, criteria []Criterion) error {
	if len(criteria) > maxCriteriaPerGoal {
		return fmt.Errorf("store: intake records: %d criteria — a goal carries at most %d (GI2)", len(criteria), maxCriteriaPerGoal)
	}
	seen := map[string]bool{}
	for i, c := range criteria {
		if c.Text == "" {
			return fmt.Errorf("store: intake records: criterion %d is empty", i+1)
		}
		if len(c.Text) > maxCriterionText {
			return fmt.Errorf("store: intake records: criterion %d is over %d characters", i+1, maxCriterionText)
		}
		if c.CheckKind != "" && !allCriterionChecks[c.CheckKind] {
			return fmt.Errorf("store: intake records: check kind %q is not one of manual, command, test", c.CheckKind)
		}
		id := c.CriterionID
		if id == "" {
			id = fmt.Sprintf("ac-%d", i+1)
		}
		if seen[id] {
			return fmt.Errorf("store: intake records: criterion id %q appears twice", id)
		}
		seen[id] = true
		if _, err := tx.Exec(`INSERT INTO goal_criterion (goal_id,goal_version,ordinal,criterion_id,text,check_kind,check_ref)
			VALUES (?,?,?,?,?,?,?)`, goalID, version, i+1, id, c.Text, nullable(c.CheckKind), nullable(c.CheckRef)); err != nil {
			return fmt.Errorf("store: intake records: criterion %s of %s: %w", id, goalID, err)
		}
	}
	return nil
}
