package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// S26 T2 — the roadmap's SQL. The MEANING (the eight statuses, their
// transition table, D4's readiness) lives in internal/roadmap; this file is
// the handle's side: rows in, rows out, and the claimed-FROM updates that
// make two dispatchers racing for one item a decided race rather than a
// corrupt one. Vocabulary closes at the database (T1's CHECKs), so no Go
// constant is load-bearing here — queue.go's deliberate rule, carried:
// one table, one CHECK, one truth.

// RoadmapItemRow mirrors roadmap_item's columns exactly, nullables as
// pointers. internal/roadmap converts it into the package's Item; the row
// type carries no behaviour on purpose — behaviour here would be a second
// place that knows what `ready` means.
type RoadmapItemRow struct {
	ID            int64
	ComponentRoot string
	Title         string
	Epic          *string
	Status        string
	PriorityState string
	PriorityLabel *string
	EstimateMin   *int
	EstimateMax   *int
	EstimateBasis *string
	Provenance    string
	BriefPath     *string
	PlanPath      *string
	LoopSlug      *string
	CreatedAt     string
	UpdatedAt     string
}

// NewRoadmapItemRow is the insert shape. Identity and timestamps are the
// store's; status is not part of it — a created item is `idea` (R1's start:
// created, ideation not started), and letting the caller pick the entry
// status would smuggle a half-run state in through the front door.
// PriorityState defaults to 'unknown' at the database when empty here —
// D2's honest absence is the DEFAULT, not a caller's habit.
type NewRoadmapItemRow struct {
	ComponentRoot string
	Title         string
	Epic          *string
	PriorityState string
	PriorityLabel *string
	EstimateMin   *int
	EstimateMax   *int
	EstimateBasis *string
	Provenance    string
	BriefPath     *string
	PlanPath      *string
}

const roadmapItemColumns = `id, component_root, title, epic, status,
	priority_state, priority_label, estimate_min, estimate_max, estimate_basis,
	provenance, brief_path, plan_path, loop_slug, created_at, updated_at`

func scanRoadmapItemRow(row interface{ Scan(dest ...any) error }) (RoadmapItemRow, error) {
	var r RoadmapItemRow
	err := row.Scan(&r.ID, &r.ComponentRoot, &r.Title, &r.Epic, &r.Status,
		&r.PriorityState, &r.PriorityLabel, &r.EstimateMin, &r.EstimateMax, &r.EstimateBasis,
		&r.Provenance, &r.BriefPath, &r.PlanPath, &r.LoopSlug, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// CreateRoadmapItem adds one item, entering at `idea`. The estimate trio
// and the priority pair each travel together or not at all — T1's CHECKs
// refuse the partial shapes, and this door refuses the partial WRITES so
// the refusal arrives as a sentence, not a driver error.
// The shared write SQL — one const per statement, read by the plain ctx
// variant here AND the audited tx variant in roadmap_audited.go, so the two
// execution paths of the same write cannot drift (S50 T1).
const (
	createRoadmapItemSQL = `INSERT INTO roadmap_item
		(component_root, title, epic, status, priority_state, priority_label,
		 estimate_min, estimate_max, estimate_basis, provenance, brief_path, plan_path, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	transitionRoadmapItemSQL = `UPDATE roadmap_item SET status = ?, updated_at = ? WHERE id = ? AND status = ?`
	setRoadmapEstimateSQL    = `UPDATE roadmap_item SET estimate_min = ?, estimate_max = ?, estimate_basis = ?, updated_at = ? WHERE id = ?`
	setRoadmapPrioritySQL    = `UPDATE roadmap_item SET priority_state = ?, priority_label = ?, updated_at = ? WHERE id = ?`
	setRoadmapLoopSQL        = `UPDATE roadmap_item SET loop_slug = ?, updated_at = ? WHERE id = ? AND component_root = ? AND status = ?`
)

// checkNewRoadmapItemShape is the shared shape law of CreateRoadmapItem and
// its audited twin — partial estimates and label-on-unknown arrive as
// sentences, not driver errors, on BOTH paths.
func checkNewRoadmapItemShape(in NewRoadmapItemRow) error {
	if in.ComponentRoot == "" || in.Title == "" {
		return fmt.Errorf("store: a roadmap item needs a component root and a title")
	}
	if in.Provenance == "" {
		return fmt.Errorf("store: a roadmap item names its provenance — authored, triggered or triaged (D5); the column has no default on purpose")
	}
	if (in.EstimateMin == nil) != (in.EstimateMax == nil) || (in.EstimateMin == nil) != (in.EstimateBasis == nil) {
		return fmt.Errorf("store: an estimate is a range with its basis or no estimate at all (D3) — min, max and basis arrive together or not at all")
	}
	state := in.PriorityState
	if state == "" {
		state = "unknown"
	}
	if state == "unknown" && in.PriorityLabel != nil {
		return fmt.Errorf("store: a priority label on an unknown priority contradicts itself (D2) — set the state or drop the label")
	}
	return nil
}

// checkRoadmapEstimateShape is D3's never-a-bare-figure law, shared by the
// plain and audited estimate writers.
func checkRoadmapEstimateShape(id int64, min, max int, basis string) error {
	if strings.TrimSpace(basis) == "" {
		return fmt.Errorf("store: roadmap item %d: an estimate without its basis is not an estimate (D3)", id)
	}
	if min > max {
		return fmt.Errorf("store: roadmap item %d: estimate min %d above max %d", id, min, max)
	}
	return nil
}

// checkRoadmapArtifactPointers refuses the "" sentinel on either pointer
// (absence is NULL, presence is a path) — shared by both artifact writers.
func checkRoadmapArtifactPointers(id int64, briefPath, planPath *string) error {
	if briefPath != nil && *briefPath == "" {
		return fmt.Errorf("store: roadmap item %d: a brief_path of \"\" is a sentinel — absence is NULL", id)
	}
	if planPath != nil && *planPath == "" {
		return fmt.Errorf("store: roadmap item %d: a plan_path of \"\" is a sentinel — absence is NULL", id)
	}
	return nil
}

// roadmapArtifactSets builds the SET columns and their args (updated_at
// excluded — joinRoadmapSets appends it last, the one order both writers
// share).
func roadmapArtifactSets(briefPath, planPath *string) ([]string, []any) {
	var sets []string
	var args []any
	if briefPath != nil {
		sets = append(sets, "brief_path = ?")
		args = append(args, *briefPath)
	}
	if planPath != nil {
		sets = append(sets, "plan_path = ?")
		args = append(args, *planPath)
	}
	return sets, args
}

func joinRoadmapSets(sets []string) string {
	return strings.Join(sets, ", ") + ", updated_at = ?"
}

func (s *Store) CreateRoadmapItem(in NewRoadmapItemRow) (RoadmapItemRow, error) {
	if err := checkNewRoadmapItemShape(in); err != nil {
		return RoadmapItemRow{}, err
	}
	if in.PriorityState == "" {
		in.PriorityState = "unknown"
	}
	now := s.Now()
	res, err := s.w.Exec(createRoadmapItemSQL,
		in.ComponentRoot, in.Title, in.Epic, "idea", in.PriorityState, in.PriorityLabel,
		in.EstimateMin, in.EstimateMax, in.EstimateBasis, in.Provenance, in.BriefPath, in.PlanPath,
		now, now)
	if err != nil {
		return RoadmapItemRow{}, fmt.Errorf("store: create roadmap item in %s: %w", in.ComponentRoot, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RoadmapItemRow{}, fmt.Errorf("store: create roadmap item id: %w", err)
	}
	row, ok, err := s.RoadmapItemByID(id)
	if err != nil {
		return RoadmapItemRow{}, err
	}
	if !ok {
		return RoadmapItemRow{}, fmt.Errorf("store: roadmap item %d vanished between commit and read", id)
	}
	return row, nil
}

// RoadmapItemByID reads one row; false, not error, for an id that is not
// there — the caller decides whether absence is a refusal.
func (s *Store) RoadmapItemByID(id int64) (RoadmapItemRow, bool, error) {
	r, err := scanRoadmapItemRow(s.r.QueryRow(`SELECT `+roadmapItemColumns+` FROM roadmap_item WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return RoadmapItemRow{}, false, nil
	}
	if err != nil {
		return RoadmapItemRow{}, false, fmt.Errorf("store: read roadmap item %d: %w", id, err)
	}
	return r, true, nil
}

// RoadmapItemByLoopSlug is the reactor's lookup: the item whose recorded
// loop IS this slug (queue's QueueItemByLoopSlug, same shape). Absence is
// false, not error — a loop that belongs to no roadmap item is the common
// case, not the exceptional one, and the reactor must do nothing quietly.
func (s *Store) RoadmapItemByLoopSlug(componentRoot, loopSlug string) (RoadmapItemRow, bool, error) {
	r, err := scanRoadmapItemRow(s.r.QueryRow(`SELECT `+roadmapItemColumns+` FROM roadmap_item
		WHERE component_root = ? AND loop_slug = ?`, componentRoot, loopSlug))
	if err == sql.ErrNoRows {
		return RoadmapItemRow{}, false, nil
	}
	if err != nil {
		return RoadmapItemRow{}, false, fmt.Errorf("store: roadmap item by loop %s/%s: %w", componentRoot, loopSlug, err)
	}
	return r, true, nil
}

// RoadmapItemFilters is the list's optional narrowing — T2's door names
// them epic/priority/status; empty means "no filter on that column".
type RoadmapItemFilters struct {
	Epic          string
	PriorityState string
	Status        string
}

// ListRoadmapItems reads a root's items in the roadmap's own order: the
// forward Gantt's shape. Statuses ride R1's machine order — idea first,
// cancelled last — so the renderer walks the future the way the machine
// runs it; within a status, oldest first (an item's own sequence inside
// its rung), then id as the tie-breaker two same-second rows need.
var roadmapStatusOrder = []string{"idea", "ideating", "brief-agreed", "planned", "ready", "running", "done", "cancelled"}

// An empty componentRoot is S50's ruling (RM6): the board may answer
// across every component when the filter names none — roadmap.list stays
// scoped (S26 N2 untouched); this is the ONE whole-view read, and the
// board is why.
func (s *Store) ListRoadmapItems(componentRoot string, f RoadmapItemFilters) ([]RoadmapItemRow, error) {
	q := `SELECT ` + roadmapItemColumns + ` FROM roadmap_item WHERE 1=1`
	var args []any
	if componentRoot != "" {
		q += ` AND component_root = ?`
		args = append(args, componentRoot)
	}
	if f.Epic != "" {
		q += ` AND epic = ?`
		args = append(args, f.Epic)
	}
	if f.PriorityState != "" {
		q += ` AND priority_state = ?`
		args = append(args, f.PriorityState)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	q += ` ORDER BY CASE status`
	for i, st := range roadmapStatusOrder {
		q += fmt.Sprintf(" WHEN '%s' THEN %d", st, i)
	}
	q += ` ELSE 9 END ASC, created_at ASC, id ASC`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list roadmap items in %s: %w", componentRoot, err)
	}
	defer rows.Close()
	var out []RoadmapItemRow
	for rows.Next() {
		r, err := scanRoadmapItemRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan roadmap item in %s: %w", componentRoot, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountRoadmapItems is D8's evidence door: a project with ZERO roadmap
// items must behave exactly like one that never heard of roadmaps, and the
// loop-start path (and its tests) asks that question honestly — a count,
// not a judgement.
func (s *Store) CountRoadmapItems(componentRoot string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM roadmap_item WHERE component_root = ?`, componentRoot).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count roadmap items in %s: %w", componentRoot, err)
	}
	return n, nil
}

// TransitionRoadmapItem is the claimed-FROM update — queue.go's discipline
// carried: the row moves from → to only if it is STILL `from` when the
// write lands. Two dispatchers racing for one item produce one winner
// (affected 1) and one honest loser (affected 0), never two statuses.
// Vocabulary is not re-checked here — T1's CHECK closes it at the database.
func (s *Store) TransitionRoadmapItem(id int64, from, to string) (bool, error) {
	res, err := s.w.Exec(transitionRoadmapItemSQL, to, s.Now(), id, from)
	if err != nil {
		return false, fmt.Errorf("store: transition roadmap item %d %s→%s: %w", id, from, to, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: transition roadmap item %d: %w", id, err)
	}
	return n == 1, nil
}

// SetRoadmapItemArtifacts records the pointers D9 keeps — brief_path and
// plan_path are POINTERS, never content (the plan lives in plan.md and
// S13's project-planner owns it). Either may stay NULL: an item is on its
// way. The empty-string is refused as a value because a path column
// holding "" is the sentinel the whole house style avoids — absence is
// NULL, presence is a path.
func (s *Store) SetRoadmapItemArtifacts(id int64, briefPath, planPath *string) (bool, error) {
	if err := checkRoadmapArtifactPointers(id, briefPath, planPath); err != nil {
		return false, err
	}
	sets, args := roadmapArtifactSets(briefPath, planPath)
	args = append(args, s.Now(), id)
	res, err := s.w.Exec(`UPDATE roadmap_item SET `+joinRoadmapSets(sets)+` WHERE id = ?`, args...)
	if err != nil {
		return false, fmt.Errorf("store: set roadmap item %d artifacts: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set roadmap item %d artifacts: %w", id, err)
	}
	return n == 1, nil
}

// SetRoadmapItemEstimate writes the whole range with its basis in ONE
// update — D3's never-a-bare-figure made structural at the door level,
// mirroring SizeAndReadyQueueItem's all-three-or-none shape: partial
// estimate writes are not offered at all, so the database CHECK is the
// backstop and not the plan.
func (s *Store) SetRoadmapItemEstimate(id int64, min, max int, basis string) (bool, error) {
	if err := checkRoadmapEstimateShape(id, min, max, basis); err != nil {
		return false, err
	}
	res, err := s.w.Exec(setRoadmapEstimateSQL, min, max, basis, s.Now(), id)
	if err != nil {
		return false, fmt.Errorf("store: set roadmap item %d estimate: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set roadmap item %d estimate: %w", id, err)
	}
	return n == 1, nil
}

// SetRoadmapItemPriority writes state and label TOGETHER — the pair is one
// decision (D2): moving to 'unknown' carries a nil label because a label
// on an unknown priority is the contradiction T1 made unwritable, and a
// known/assumed priority may carry its label or none yet.
func (s *Store) SetRoadmapItemPriority(id int64, state string, label *string) (bool, error) {
	res, err := s.w.Exec(setRoadmapPrioritySQL, state, label, s.Now(), id)
	if err != nil {
		return false, fmt.Errorf("store: set roadmap item %d priority: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: set roadmap item %d priority: %w", id, err)
	}
	return n == 1, nil
}

// SetRoadmapItemLoop records the loop an item materialized into. The
// claimed guard is the status: the record lands only while the item still
// wears `running`, so a retry after a crash cannot re-point a finished
// item's loop, and the composite FK (T1) refuses a slug naming no loop.
func (s *Store) SetRoadmapItemLoop(id int64, componentRoot, loopSlug, requireStatus string) (bool, error) {
	// The stamp is SCOPED (the queue's SetQueueItemLoop is the precedent):
	// an id plus the caller's root — stamping another project's item is a
	// refusal by the WHERE, not a hope.
	res, err := s.w.Exec(setRoadmapLoopSQL, loopSlug, s.Now(), id, componentRoot, requireStatus)
	if err != nil {
		return false, fmt.Errorf("store: record roadmap item %d loop %s: %w", id, loopSlug, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: record roadmap item %d loop: %w", id, err)
	}
	return n == 1, nil
}
