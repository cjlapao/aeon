package store

// briefs.go — the brief a loop works under (OL7/OL8). A brief is VERSIONED
// per loop the way a goal is versioned per goal: (component_root, loop_slug,
// version) is unique, so a rewrite is a NEW row and the review of the old
// version keeps its meaning — briefs, like reviews, are history, not state.
//
// A brief is born in_review, not drafted: it arrives as the answer a plan
// step produced, and something must judge it. The prose columns (scope,
// out_of_scope) hold the ONE-LINE summaries; the long-form stays in the
// attached loop_document (Q3's division — structured facts are columns, the
// author's prose is a document).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Brief is one version of a loop's brief. Acceptance and Constraints are
// JSON arrays of the enumerable items only — Q3 walked five real briefs to
// settle which headings a column can honestly hold, and a prose heading that
// does not fit is the document's, not the schema's.
type Brief struct {
	ID            string   `json:"id"`
	ComponentRoot string   `json:"componentRoot"`
	LoopSlug      string   `json:"loopSlug"`
	GoalID        string   `json:"goalId,omitempty"`
	GoalVersion   int      `json:"goalVersion,omitempty"` // 0 = not filed under a goal
	Version       int      `json:"version"`
	Scope         string   `json:"scope"`
	OutOfScope    string   `json:"outOfScope"`
	Acceptance    []string `json:"acceptance"`
	Constraints   []string `json:"constraints"`
	TestPlan      string   `json:"testPlan"`
	ReviewState   string   `json:"reviewState"`
	SourceStep    *int64   `json:"sourceStep,omitempty"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

// NewBrief is the create input; version is never caller-chosen — the store
// computes next = max+1 for the loop, so two writers cannot pick the same
// version and one of them quietly overwrite the other.
type NewBrief struct {
	GoalID      string
	GoalVersion int
	Scope       string
	OutOfScope  string
	Acceptance  []string
	Constraints []string
	TestPlan    string
	SourceStep  *int64
}

const briefColumns = `id, component_root, loop_slug, COALESCE(goal_id,''), COALESCE(goal_version,0), version,
	scope, out_of_scope, acceptance, constraints, test_plan, review_state, source_step, created_at, updated_at`

func scanBrief(row interface{ Scan(...any) error }) (Brief, error) {
	var b Brief
	var acceptance, constraints string
	var step sql.NullInt64
	if err := row.Scan(&b.ID, &b.ComponentRoot, &b.LoopSlug, &b.GoalID, &b.GoalVersion, &b.Version,
		&b.Scope, &b.OutOfScope, &acceptance, &constraints, &b.TestPlan, &b.ReviewState,
		&step, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return Brief{}, err
	}
	if step.Valid {
		v := step.Int64
		b.SourceStep = &v
	}
	if err := json.Unmarshal([]byte(acceptance), &b.Acceptance); err != nil {
		return Brief{}, fmt.Errorf("store: acceptance of brief %s: %w", b.ID, err)
	}
	if err := json.Unmarshal([]byte(constraints), &b.Constraints); err != nil {
		return Brief{}, fmt.Errorf("store: constraints of brief %s: %w", b.ID, err)
	}
	return b, nil
}

// CreateBrief files the next version of this loop's brief.
func (s *Store) CreateBrief(sc Scope, in NewBrief) (Brief, error) {
	if !sc.Valid() {
		return Brief{}, fmt.Errorf("store: CreateBrief needs both a slug and a component root, got %s", sc)
	}
	// The loop must exist — the FK says so, and the sentence says so sooner
	// (a brief for nobody is a filing error, not an infrastructure failure).
	var one int
	if err := s.r.QueryRow(`SELECT 1 FROM loop WHERE component_root = ? AND slug = ?`, sc.ComponentRoot, sc.LoopSlug).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Brief{}, fmt.Errorf("store: loop %s/%s does not exist — a brief belongs to a loop", sc.ComponentRoot, sc.LoopSlug)
		}
		return Brief{}, fmt.Errorf("store: check loop %s: %w", sc, err)
	}
	if in.GoalID != "" && in.GoalVersion > 0 {
		if err := s.r.QueryRow(`SELECT 1 FROM goal WHERE id = ? AND version = ?`, in.GoalID, in.GoalVersion).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Brief{}, fmt.Errorf("store: goal %s does not stand at version %d — a brief pins the goal version it answers", in.GoalID, in.GoalVersion)
			}
			return Brief{}, fmt.Errorf("store: check goal %s: %w", in.GoalID, err)
		}
	}
	var maxVersion int
	if err := s.r.QueryRow(`SELECT COALESCE(MAX(version),0) FROM brief WHERE component_root = ? AND loop_slug = ?`,
		sc.ComponentRoot, sc.LoopSlug).Scan(&maxVersion); err != nil {
		return Brief{}, fmt.Errorf("store: next brief version for %s: %w", sc, err)
	}
	acceptance := in.Acceptance
	if acceptance == nil {
		acceptance = []string{}
	}
	constraints := in.Constraints
	if constraints == nil {
		constraints = []string{}
	}
	aj, err := json.Marshal(acceptance)
	if err != nil {
		return Brief{}, fmt.Errorf("store: encode acceptance: %w", err)
	}
	cj, err := json.Marshal(constraints)
	if err != nil {
		return Brief{}, fmt.Errorf("store: encode constraints: %w", err)
	}
	now := s.Now()
	b := Brief{ID: "brf-" + newID(), ComponentRoot: sc.ComponentRoot, LoopSlug: sc.LoopSlug,
		GoalID: in.GoalID, GoalVersion: in.GoalVersion, Version: maxVersion + 1,
		Scope: in.Scope, OutOfScope: in.OutOfScope, Acceptance: acceptance, Constraints: constraints,
		TestPlan: in.TestPlan, ReviewState: "in_review", SourceStep: in.SourceStep,
		CreatedAt: now, UpdatedAt: now}
	if _, err := s.w.Exec(`INSERT INTO brief (id,loop_slug,component_root,goal_id,goal_version,version,scope,out_of_scope,acceptance,constraints,test_plan,review_state,source_step,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, sc.LoopSlug, sc.ComponentRoot, nullable(b.GoalID), nilIfZero(b.GoalVersion), b.Version,
		b.Scope, b.OutOfScope, string(aj), string(cj), b.TestPlan, b.ReviewState, b.SourceStep, now, now); err != nil {
		return Brief{}, fmt.Errorf("store: create brief for %s: %w", sc, err)
	}
	return b, nil
}

// nilIfZero files "no goal version" as NULL, not 0 — a zero would read as
// "version zero of a goal", which is a thing that never exists.
func nilIfZero(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// GetBrief reads one exact version.
func (s *Store) GetBrief(sc Scope, version int) (Brief, error) {
	b, err := scanBrief(s.r.QueryRow(`SELECT `+briefColumns+` FROM brief WHERE component_root = ? AND loop_slug = ? AND version = ?`,
		sc.ComponentRoot, sc.LoopSlug, version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Brief{}, fmt.Errorf("store: brief %s/%s v%d does not exist", sc.ComponentRoot, sc.LoopSlug, version)
		}
		return Brief{}, fmt.Errorf("store: read brief %s v%d: %w", sc, version, err)
	}
	return b, nil
}

// LatestBrief reads the newest version — what a loop runs under until judged.
func (s *Store) LatestBrief(sc Scope) (Brief, error) {
	b, err := scanBrief(s.r.QueryRow(`SELECT `+briefColumns+` FROM brief WHERE component_root = ? AND loop_slug = ?
		ORDER BY version DESC LIMIT 1`, sc.ComponentRoot, sc.LoopSlug))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Brief{}, fmt.Errorf("store: loop %s has no brief yet", sc)
		}
		return Brief{}, fmt.Errorf("store: latest brief %s: %w", sc, err)
	}
	return b, nil
}

// DecideBrief claims a decision FROM the judged version in one transaction —
// the same claimed-FROM shape as DecideGoal (T0-Q8 settled that DecideBrief
// joins AnswerGate's transaction from the runtime side; here it is the store
// half, self-contained and atomic).
func (s *Store) DecideBrief(sc Scope, version int, decision string, by Actor, reason string) (DecideResult, *Refusal, error) {
	if !sc.Valid() {
		return DecideResult{}, nil, fmt.Errorf("store: DecideBrief needs both a slug and a component root, got %s", sc)
	}
	if r := decideRefusal(by, decision); r != nil {
		return DecideResult{}, r, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide brief: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	res, err := tx.Exec(`UPDATE brief SET review_state=?, updated_at=?
		WHERE component_root=? AND loop_slug=? AND version=? AND review_state='in_review'`,
		decision, now, sc.ComponentRoot, sc.LoopSlug, version)
	if err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide brief %s v%d: %w", sc, version, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if err := tx.Rollback(); err != nil {
			return DecideResult{}, nil, fmt.Errorf("store: decide brief rollback: %w", err)
		}
		var state string
		err := s.r.QueryRow(`SELECT review_state FROM brief WHERE component_root=? AND loop_slug=? AND version=?`,
			sc.ComponentRoot, sc.LoopSlug, version).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return DecideResult{}, nil, fmt.Errorf("store: brief %s v%d does not exist", sc, version)
		} else if err != nil {
			return DecideResult{}, nil, fmt.Errorf("store: read brief %s v%d: %w", sc, version, err)
		}
		out := DecideResult{Changed: false, State: state}
		if prior, ok := s.latestReview("brief", sc.LoopSlug, version); ok {
			out.DecidedBy, out.DecidedAt = prior.ByID, prior.DecidedAt
		}
		return out, nil, nil
	}
	// A brief's subject id IS its loop's slug — the (root, slug) pair is the
	// key briefs live under, and a review row naming the slug joins the same
	// audit reading as a goal's.
	if err := reviewInsert(tx, "brief", sc.LoopSlug, version, decision, by.ID, reason, now); err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: record decision on brief %s v%d: %w", sc, version, err)
	}
	if err := tx.Commit(); err != nil {
		return DecideResult{}, nil, fmt.Errorf("store: decide brief: %w", err)
	}
	return DecideResult{Changed: true, State: decision, DecidedBy: by.ID, DecidedAt: now}, nil, nil
}
