package store

// goals.go — the goal, its versions, and the two rules OL8 lives by: every
// goal passes in_review, and a decision is CLAIMED FROM the reviewed version
// in one transaction, so two deciders cannot both approve and a re-decision
// names the first rather than overwriting it.
//
// # The version model, stated once
//
// A goal is ONE row whose `version` number bumps. History does not live in
// goal rows — it lives in `review` rows (which versions were judged and how)
// and in `goal_component` (which keeps a component set per version). Callers
// address coordinates (goal_id, goal_version) — brief rows, loop columns,
// review subjects — and a coordinate whose version has been superseded keeps
// meaning: it names the goal AS IT WAS at that version, which is exactly
// what a review of it judged.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Goal is the current version of a goal. Components carries the CURRENT
// version's component set on GetGoal; a listing leaves it empty (the rollup
// is a per-goal read on purpose — a listing that computes one invites a
// stored-total drift OL11 refuses).
type Goal struct {
	ID            string `json:"id"`
	ComponentID   string `json:"componentId"`
	EpicID        string `json:"epicId,omitempty"`
	Version       int    `json:"version"`
	Title         string `json:"title"`
	Type          string `json:"type"`
	BodyNote      string `json:"bodyNote"`
	SourceKind    string `json:"sourceKind"`
	SourceRef     string `json:"sourceRef"`
	SourceLoop    string `json:"sourceLoop,omitempty"`
	SourceStep    *int64 `json:"sourceStep,omitempty"`
	ReviewState   string `json:"reviewState"`
	CreatedByKind string `json:"createdByKind"`
	CreatedByID   string `json:"createdById"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`

	Components []GoalComponent `json:"components,omitempty"`
}

// GoalComponent is one component's stake in a goal, per version. The three
// nullables are all P31 states, not defaults: an unknown estimate is UNKNOWN,
// not zero — a zero would silently lower the rollup's floor.
type GoalComponent struct {
	ComponentID string   `json:"componentId"`
	Rationale   string   `json:"rationale"`
	EstimateUSD *float64 `json:"estimateUsd,omitempty"`
	Confidence  *int     `json:"confidence,omitempty"` // 1 low, 2 medium, 3 high; nil = unknown
	Basis       []string `json:"basis"`
}

// NewGoal is the create input. By is required and must be well-formed; Submit
// files the draft straight into review — the brainstorm path (T10) uses it,
// a human drafting usually does not.
type NewGoal struct {
	ComponentID string
	EpicID      string
	Title       string
	Type        string
	BodyNote    string
	SourceKind  string
	SourceRef   string
	SourceLoop  string
	SourceStep  *int64
	Components  []GoalComponent
	By          Actor
	Submit      bool
}

// GoalEdit is the update input. EMPTY means keep (the §4c discipline). Any
// difference from the stored row is a MATERIAL edit: the version bumps, the
// goal re-enters review, and the component set moves with it (Components nil
// = copy the current set forward; non-nil = replace it).
type GoalEdit struct {
	ID         string
	Title      string
	Type       string
	BodyNote   string
	EpicID     string
	Components []GoalComponent
	By         Actor
}

// DecideResult is the claimed-FROM answer. Changed=false is never an error
// and never a silent no: State names where the subject stands now, and when
// a prior decision exists DecidedBy/DecidedAt name WHO and WHEN, so a second
// decider reads the ledger's first entry instead of overwriting it.
type DecideResult struct {
	Changed   bool   `json:"changed"`
	State     string `json:"state"`
	DecidedBy string `json:"decidedBy,omitempty"`
	DecidedAt string `json:"decidedAt,omitempty"`
}

// GoalFilter narrows a listing; every empty field is "any".
type GoalFilter struct {
	ComponentID string
	EpicID      string
	ReviewState string
	Type        string
}

// goalScan shapes one row — shared by Get and List so the column list lives
// in exactly one expression.
const goalColumns = `id, component_id, COALESCE(epic_id,''), version, title, type, body_note,
	source_kind, source_ref, COALESCE(source_loop,''), source_step, review_state,
	created_by_kind, created_by_id, created_at, updated_at`

func scanGoal(row interface{ Scan(...any) error }) (Goal, error) {
	var g Goal
	var step sql.NullInt64
	if err := row.Scan(&g.ID, &g.ComponentID, &g.EpicID, &g.Version, &g.Title, &g.Type, &g.BodyNote,
		&g.SourceKind, &g.SourceRef, &g.SourceLoop, &step, &g.ReviewState,
		&g.CreatedByKind, &g.CreatedByID, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return Goal{}, err
	}
	if step.Valid {
		v := step.Int64
		g.SourceStep = &v
	}
	return g, nil
}

// CreateGoal files a goal — a draft, or straight into review when Submit is
// set. `approved` is unreachable from here by construction: review_state is
// written as 'draft' or 'in_review' and nothing else (records_guard_test.go
// greps the package to keep the promise louder than the code).
func (s *Store) CreateGoal(in NewGoal) (Goal, *Refusal, error) {
	if !in.By.validActor() {
		return Goal{}, &Refusal{Kind: "actor-unknown",
			Sentence: fmt.Sprintf("refused: author kind %q is not user, agent, or trigger (or the author has no id) — every goal names who made it", in.By.Kind)}, nil
	}
	if in.Title == "" {
		return Goal{}, &Refusal{Kind: "title-empty",
			Sentence: "refused: a goal needs a title — the one line a person can point at"}, nil
	}
	if !allGoalTypes[in.Type] {
		return Goal{}, &Refusal{Kind: "type-unknown",
			Sentence: fmt.Sprintf("refused: goal type %q is not one of bug, feature, chore, refactor, investigation", in.Type)}, nil
	}
	if !allSourceKinds[in.SourceKind] {
		return Goal{}, &Refusal{Kind: "source-kind-unknown",
			Sentence: fmt.Sprintf("refused: source kind %q is not one of manual, brainstorm, webhook, cron, event, intake", in.SourceKind)}, nil
	}
	state := "draft"
	if in.Submit {
		state = "in_review"
	}
	now := s.Now()
	g := Goal{ID: "goal-" + newID(), ComponentID: in.ComponentID, EpicID: in.EpicID, Version: 1,
		Title: in.Title, Type: in.Type, BodyNote: in.BodyNote,
		SourceKind: in.SourceKind, SourceRef: in.SourceRef, SourceLoop: in.SourceLoop, SourceStep: in.SourceStep,
		ReviewState: state, CreatedByKind: in.By.Kind, CreatedByID: in.By.ID,
		CreatedAt: now, UpdatedAt: now}

	tx, err := s.w.Begin()
	if err != nil {
		return Goal{}, nil, fmt.Errorf("store: create goal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// CT10: a goal hangs on a root component, never a child.
	if !s.isRoot(in.ComponentID) {
		return Goal{}, nil, fmt.Errorf("store: component %q is not a root — a goal hangs on the top of the tree", in.ComponentID)
	}
	var one int
	if in.EpicID != "" {
		if err := tx.QueryRow(`SELECT 1 FROM epic WHERE id = ?`, in.EpicID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Goal{}, nil, fmt.Errorf("store: epic %q does not exist", in.EpicID)
			}
			return Goal{}, nil, fmt.Errorf("store: check epic %s: %w", in.EpicID, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO goal (id,component_id,epic_id,version,title,type,body_note,source_kind,source_ref,source_loop,source_step,review_state,created_by_kind,created_by_id,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.ComponentID, nullable(g.EpicID), g.Version, g.Title, g.Type, g.BodyNote,
		g.SourceKind, g.SourceRef, nullable(g.SourceLoop), g.SourceStep, g.ReviewState,
		g.CreatedByKind, g.CreatedByID, now, now); err != nil {
		return Goal{}, nil, fmt.Errorf("store: insert goal %q: %w", g.Title, err)
	}
	g.Components, err = insertGoalComponents(tx, g.ID, 1, in.Components)
	if err != nil {
		return Goal{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Goal{}, nil, fmt.Errorf("store: create goal: %w", err)
	}
	return g, nil, nil
}

// insertGoalComponents writes a version's component set, validating each
// member exists (the FK would refuse too — the sentence is the point:
// "component c9 does not exist" beats "FOREIGN KEY constraint failed").
// Returns the set as stored (Basis normalised), for the caller's copy.
func insertGoalComponents(tx *sql.Tx, goalID string, version int, set []GoalComponent) ([]GoalComponent, error) {
	var stored []GoalComponent
	for _, gc := range set {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM component WHERE id = ?`, gc.ComponentID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("store: component %q does not exist — a goal stakes work in components that are real", gc.ComponentID)
			}
			return nil, fmt.Errorf("store: check component %s: %w", gc.ComponentID, err)
		}
		basis := gc.Basis
		if basis == nil {
			basis = []string{}
		}
		b, err := json.Marshal(basis)
		if err != nil {
			return nil, fmt.Errorf("store: encode basis: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO goal_component (goal_id,goal_version,component_id,rationale,estimate_usd,confidence,basis)
			VALUES (?,?,?,?,?,?,?)`, goalID, version, gc.ComponentID, gc.Rationale, gc.EstimateUSD, gc.Confidence, string(b)); err != nil {
			return nil, fmt.Errorf("store: file component %s under goal %s v%d: %w", gc.ComponentID, goalID, version, err)
		}
		stored = append(stored, GoalComponent{ComponentID: gc.ComponentID, Rationale: gc.Rationale,
			EstimateUSD: gc.EstimateUSD, Confidence: gc.Confidence, Basis: basis})
	}
	return stored, nil
}

// UpdateGoal applies a material edit: the version bumps, the goal re-enters
// review (an approved goal edited is an approved goal nobody has seen —
// TestMaterialEditBumpsVersionAndReopensReview), and the component set rides
// along: nil copies the previous version's set forward, non-nil replaces it.
// An edit that changes nothing is reported as Noop via an unchanged version
// — it is not an error, and it does not reopen anything.
func (s *Store) UpdateGoal(in GoalEdit) (Goal, *Refusal, error) {
	if !in.By.validActor() {
		return Goal{}, &Refusal{Kind: "actor-unknown",
			Sentence: fmt.Sprintf("refused: author kind %q is not user, agent, or trigger (or the author has no id)", in.By.Kind)}, nil
	}
	cur, err := s.GetGoal(in.ID)
	if err != nil {
		return Goal{}, nil, err
	}
	if in.Type != "" && !allGoalTypes[in.Type] {
		return Goal{}, &Refusal{Kind: "type-unknown",
			Sentence: fmt.Sprintf("refused: goal type %q is not one of bug, feature, chore, refactor, investigation", in.Type)}, nil
	}
	title, body, etype, epic := cur.Title, cur.BodyNote, cur.Type, cur.EpicID
	if in.Title != "" {
		title = in.Title
	}
	if in.BodyNote != "" {
		body = in.BodyNote
	}
	if in.Type != "" {
		etype = in.Type
	}
	if in.EpicID != "" {
		epic = in.EpicID
	}
	material := title != cur.Title || body != cur.BodyNote || etype != cur.Type || epic != cur.EpicID || in.Components != nil
	if !material {
		return cur, nil, nil // Noop: same version, same state — nothing reopened
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Goal{}, nil, fmt.Errorf("store: update goal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	newVersion := cur.Version + 1
	if epic != "" {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM epic WHERE id = ?`, epic).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Goal{}, nil, fmt.Errorf("store: epic %q does not exist", epic)
			}
			return Goal{}, nil, fmt.Errorf("store: check epic %s: %w", epic, err)
		}
	}
	// Claim the row at its CURRENT version, so a concurrent edit that already
	// bumped it cannot be clobbered silently: this UPDATE either moves v→v+1
	// or moves nothing and says so.
	res, err := tx.Exec(`UPDATE goal SET title=?, type=?, body_note=?, epic_id=?, version=?, review_state='in_review', updated_at=?
		WHERE id=? AND version=?`, title, etype, body, nullable(epic), newVersion, now, in.ID, cur.Version)
	if err != nil {
		return Goal{}, nil, fmt.Errorf("store: bump goal %s: %w", in.ID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Goal{}, &Refusal{Kind: "goal-changed-underneath",
			Sentence: fmt.Sprintf("refused: goal %s moved while this edit was being prepared — re-read it and try again", in.ID)}, nil
	}
	if in.Components == nil {
		// Copy the previous version's set forward — the version bump is about
		// the TEXT changing, and the component set is part of what was
		// approved.
		if _, err := tx.Exec(`INSERT INTO goal_component (goal_id,goal_version,component_id,rationale,estimate_usd,confidence,basis)
			SELECT goal_id, ?, component_id, rationale, estimate_usd, confidence, basis
			FROM goal_component WHERE goal_id = ? AND goal_version = ?`, newVersion, in.ID, cur.Version); err != nil {
			return Goal{}, nil, fmt.Errorf("store: copy component set of goal %s: %w", in.ID, err)
		}
	} else if _, err := insertGoalComponents(tx, in.ID, newVersion, in.Components); err != nil {
		return Goal{}, nil, err
	}
	// S61 GI5: the criteria copy forward exactly as the component set does —
	// an edit changes the TEXT, and the definition of done is part of what
	// was agreed. There is no UpdateGoal input for criteria on purpose: if a
	// later task needs to change them, that is an explicit replace, not a
	// side effect of editing the body.
	if _, err := tx.Exec(`INSERT INTO goal_criterion (goal_id,goal_version,ordinal,criterion_id,text,check_kind,check_ref)
		SELECT goal_id, ?, ordinal, criterion_id, text, check_kind, check_ref
		FROM goal_criterion WHERE goal_id = ? AND goal_version = ?`, newVersion, in.ID, cur.Version); err != nil {
		return Goal{}, nil, fmt.Errorf("store: copy criteria set of goal %s: %w", in.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Goal{}, nil, fmt.Errorf("store: update goal: %w", err)
	}
	cur.Title, cur.Type, cur.BodyNote, cur.EpicID, cur.Version, cur.ReviewState, cur.UpdatedAt = title, etype, body, epic, newVersion, "in_review", now
	if in.Components != nil {
		cur.Components = in.Components
	}
	return cur, nil, nil
}

// SubmitGoalForReview moves a draft into review. Anything already judged or
// already waiting is not resubmitted — the answer names where it stands.
func (s *Store) SubmitGoalForReview(id string) (Goal, *Refusal, error) {
	cur, err := s.GetGoal(id)
	if err != nil {
		return Goal{}, nil, err
	}
	if cur.ReviewState != "draft" {
		return cur, &Refusal{Kind: "goal-not-draft",
			Sentence: fmt.Sprintf("refused: goal %s is %s, not a draft — only drafts submit", id, cur.ReviewState)}, nil
	}
	now := s.Now()
	if _, err := s.w.Exec(`UPDATE goal SET review_state='in_review', updated_at=? WHERE id=? AND review_state='draft'`, now, id); err != nil {
		return Goal{}, nil, fmt.Errorf("store: submit goal %s: %w", id, err)
	}
	cur.ReviewState, cur.UpdatedAt = "in_review", now
	return cur, nil, nil
}

// GetGoal reads one goal with its CURRENT version's component set.
func (s *Store) GetGoal(id string) (Goal, error) {
	g, err := scanGoal(s.r.QueryRow(`SELECT `+goalColumns+` FROM goal WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("store: goal %q does not exist", id)
		}
		return Goal{}, fmt.Errorf("store: read goal %s: %w", id, err)
	}
	g.Components, err = s.readGoalComponents(id, g.Version)
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// ListGoals returns goals newest-touched first. The component set is not
// loaded — a listing reads rows; the rollup is a GetGoal decision (see
// GoalRollup).
func (s *Store) ListGoals(f GoalFilter) ([]Goal, error) {
	var where []string
	var args []any
	if f.ComponentID != "" {
		where = append(where, `component_id = ?`)
		args = append(args, f.ComponentID)
	}
	if f.EpicID != "" {
		where = append(where, `epic_id = ?`)
		args = append(args, f.EpicID)
	}
	if f.ReviewState != "" {
		where = append(where, `review_state = ?`)
		args = append(args, f.ReviewState)
	}
	if f.Type != "" {
		where = append(where, `type = ?`)
		args = append(args, f.Type)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + join(where, " AND ")
	}
	rows, err := s.r.Query(`SELECT `+goalColumns+` FROM goal`+clause+` ORDER BY updated_at DESC, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list goals: %w", err)
	}
	defer rows.Close()
	var out []Goal
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan goal: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// join avoids importing strings into a file that otherwise does not need it.
func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// DecideGoal claims a decision FROM the reviewed version in ONE transaction:
// the UPDATE fires only while that exact version still sits in_review, so the
// winner writes the review row and every loser reads it instead of overwriting
// it. A non-human actor is refused BEFORE SQL (OL8 — a queued answer from the
// model is not a person), and the decision vocabulary is checked in the same
// breath.
func (s *Store) DecideGoal(id string, version int, decision string, by Actor, reason string) (DecideResult, *Refusal, error) {
	if r := decideRefusal(by, decision); r != nil {
		return DecideResult{}, r, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide goal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	res, err := tx.Exec(`UPDATE goal SET review_state=?, updated_at=? WHERE id=? AND version=? AND review_state='in_review'`,
		decision, now, id, version)
	if err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide goal %s v%d: %w", id, version, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if err := tx.Rollback(); err != nil {
			return DecideResult{}, nil, fmt.Errorf("store: decide goal rollback: %w", err)
		}
		// The claim was somebody else's (or the version is gone). Name the
		// winner rather than asserting nothing happened.
		var state string
		err := s.r.QueryRow(`SELECT review_state FROM goal WHERE id = ? AND version = ?`, id, version).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			state = "superseded" // that version is no longer the row's — the coordinates still name what was reviewed
		} else if err != nil {
			return DecideResult{}, nil, fmt.Errorf("store: read goal %s v%d state: %w", id, version, err)
		}
		out := DecideResult{Changed: false, State: state}
		if prior, ok := s.latestReview("goal", id, version); ok {
			out.DecidedBy, out.DecidedAt = prior.ByID, prior.DecidedAt
		}
		return out, nil, nil
	}
	if err := reviewInsert(tx, "goal", id, version, decision, by.ID, reason, now); err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: record decision on goal %s v%d: %w", id, version, err)
	}
	// S40 AU3/AU4: an APPROVED goal emits, inside this same transaction —
	// the claimed-FROM branch above is what makes "emitted twice" impossible,
	// because a second decider changes zero rows and never reaches here. A
	// rejected or parked goal is silent (AU3's closed intention list). The
	// facts are read from THIS tx, after this tx's own write: a reader
	// outside could see a goal this decision has not yet made.
	if decision == "approved" {
		var rootID, gType, sourceKind string
		if err := tx.QueryRow(`SELECT component_id, type, source_kind FROM goal WHERE id = ? AND version = ?`, id, version).
			Scan(&rootID, &gType, &sourceKind); err != nil {
			return DecideResult{}, nil, fmt.Errorf("store: read goal %s v%d for its automation event: %w", id, version, err)
		}
		if err := insertAutomationEvent(tx, now, AutomationEventIn{
			Event: EventGoalApproved, SubjectKind: "goal", SubjectID: id, SubjectVersion: version,
			RootID: rootID, ComponentID: singleGoalComponent(tx, id, version),
			GoalType: gType, SourceKind: sourceKind,
		}); err != nil {
			return DecideResult{}, nil, err // the deferred Rollback undoes the decision with the event
		}
	}
	if err := tx.Commit(); err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide goal: %w", err)
	}
	return DecideResult{Changed: true, State: decision, DecidedBy: by.ID, DecidedAt: now}, nil, nil
}

// GoalRollup answers, for a goal already read with its component set:
// the WORST confidence anyone claimed (three "high"s do not outweigh one
// "low" — the rollup is the weakest stake, not the average), the sum of what
// is KNOWN, and whether the sum is a floor rather than an estimate.
// costIsFloor is the P31 honesty: an unknown estimate is absent from the sum,
// never zero in it — the caller renders "at least $X", and a UI that drops
// the floor flag is lying by omission.
func GoalRollup(g Goal) (minConfidence *int, sumKnownUSD float64, costIsFloor bool) {
	for _, gc := range g.Components {
		if gc.Confidence != nil {
			if minConfidence == nil || *gc.Confidence < *minConfidence {
				v := *gc.Confidence
				minConfidence = &v
			}
		}
		if gc.EstimateUSD == nil {
			costIsFloor = true
			continue
		}
		sumKnownUSD += *gc.EstimateUSD
	}
	return minConfidence, sumKnownUSD, costIsFloor
}

// GetGoalVersion reads one goal at a specific version — (goal_id,
// version) is the coordinate superseded history keeps meaning at (S38 T9:
// goal_show answers for any version a record once carried).
func (s *Store) GetGoalVersion(id string, version int) (Goal, error) {
	row := s.r.QueryRow(`SELECT `+goalColumns+` FROM goal WHERE id = ? AND version = ?`, id, version)
	g, err := scanGoal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Goal{}, fmt.Errorf("store: goal %s has no version %d: %w", id, version, ErrNotFound)
	}
	if err != nil {
		return Goal{}, fmt.Errorf("store: read goal %s v%d: %w", id, version, err)
	}
	g.Components, err = s.readGoalComponents(g.ID, g.Version)
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// readGoalComponents is the ONE reader of a version's stake set.
func (s *Store) readGoalComponents(id string, version int) ([]GoalComponent, error) {
	rows, err := s.r.Query(`SELECT component_id, rationale, estimate_usd, confidence, basis
		FROM goal_component WHERE goal_id = ? AND goal_version = ? ORDER BY component_id`, id, version)
	if err != nil {
		return nil, fmt.Errorf("store: read components of goal %s: %w", id, err)
	}
	defer rows.Close()
	var out []GoalComponent
	for rows.Next() {
		var gc GoalComponent
		var est sql.NullFloat64
		var conf sql.NullInt64
		var basis string
		if err := rows.Scan(&gc.ComponentID, &gc.Rationale, &est, &conf, &basis); err != nil {
			return nil, fmt.Errorf("store: scan component of goal %s: %w", id, err)
		}
		if est.Valid {
			v := est.Float64
			gc.EstimateUSD = &v
		}
		if conf.Valid {
			v := int(conf.Int64)
			gc.Confidence = &v
		}
		if err := json.Unmarshal([]byte(basis), &gc.Basis); err != nil {
			return nil, fmt.Errorf("store: basis of goal %s/%s: %w", id, gc.ComponentID, err)
		}
		out = append(out, gc)
	}
	return out, rows.Err()
}
