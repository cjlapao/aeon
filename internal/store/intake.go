// intake.go — S61 T1 (GI6): the intake session row and its resume ledger
// (GI13). There is NO state column — state is derived (interviewing → planned
// → committing → committed / abandoned) from the loop, the open plan proposal
// and the ledger, so no column can disagree with the truth it would claim.
//
// The session row is created by a person and only a person (created_by_kind
// is CHECKed 'user'); each of its three late-arriving fields — the interview
// loop's slug, the commit's target path, the registered root — may be written
// exactly once, and the intake_root_once trigger is the schema's net under
// that law. The ledger is the resumability contract: a step is recorded once,
// and a re-run SKIPS a done step after re-verifying its evidence.
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// allStepOutcomes is the closed ledger outcome set (GI6).
var allStepOutcomes = map[string]bool{
	"done": true, "skipped": true, "failed": true,
}

// Intake is one intake session. RootID and TargetPath read empty until the
// commit fills them — empty is the not-yet state, never a guess (P31).
type Intake struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TargetPath    string `json:"targetPath,omitempty"`
	ScratchPath   string `json:"scratchPath"`
	LoopSlug      string `json:"loopSlug,omitempty"`
	RootID        string `json:"rootId,omitempty"`
	CreatedByKind string `json:"createdByKind"`
	CreatedByID   string `json:"createdById"`
	CreatedAt     string `json:"createdAt"`
}

// IntakeStep is one row of the resume ledger. Detail is the fixed template
// with ids and names filled in (SEC-033) — never a user sentence.
type IntakeStep struct {
	IntakeID string `json:"intakeId"`
	Ordinal  int    `json:"ordinal"`
	Step     string `json:"step"`
	Outcome  string `json:"outcome"`
	Detail   string `json:"detail"`
	At       string `json:"at"`
}

const intakeColumns = `id, name, COALESCE(target_path,''), scratch_path,
 	COALESCE(loop_slug,''), COALESCE(root_id,''), created_by_kind, created_by_id, created_at`

func scanIntake(row interface{ Scan(...any) error }) (Intake, error) {
	var in Intake
	err := row.Scan(&in.ID, &in.Name, &in.TargetPath, &in.ScratchPath, &in.LoopSlug,
		&in.RootID, &in.CreatedByKind, &in.CreatedByID, &in.CreatedAt)
	return in, err
}

// CreateIntake opens a session. The scratch workspace path is the daemon's
// own (it created the 0700 directory before calling — GI6); the store checks
// the person, not the path: file layout is decided above this layer.
func (s *Store) CreateIntake(name, scratchPath string, by Actor) (Intake, *Refusal, error) {
	if by.Kind != "user" || by.ID == "" {
		return Intake{}, &Refusal{Kind: "intake-not-a-person",
			Sentence: fmt.Sprintf("refused: kind %q opened an intake — only a person opens one (GI6); an agent joins a session through its loop", by.Kind)}, nil
	}
	if name == "" || scratchPath == "" {
		return Intake{}, &Refusal{Kind: "intake-incomplete",
			Sentence: "refused: an intake names itself and its scratch workspace — both are required"}, nil
	}
	in := Intake{ID: "int-" + newID(), Name: name, ScratchPath: scratchPath,
		CreatedByKind: by.Kind, CreatedByID: by.ID, CreatedAt: s.Now()}
	if _, err := s.w.Exec(`INSERT INTO intake (id,name,target_path,scratch_path,loop_slug,root_id,created_by_kind,created_by_id,created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, in.ID, in.Name, nil, in.ScratchPath, nil, nil,
		in.CreatedByKind, in.CreatedByID, in.CreatedAt); err != nil {
		return Intake{}, nil, fmt.Errorf("store: create intake: %w", err)
	}
	return in, nil, nil
}

// SetIntakeLoop records the interview loop after it starts (the slug is not
// knowable before then). Set once — the trigger refuses a second word.
func (s *Store) SetIntakeLoop(id, slug string) (*Refusal, error) {
	cur, err := s.GetIntake(id)
	if err != nil {
		return nil, err
	}
	if cur.LoopSlug != "" && cur.LoopSlug != slug {
		return &Refusal{Kind: "intake-loop-set",
			Sentence: fmt.Sprintf("refused: intake %s already interviews on loop %s — the interview loop is recorded once", id, cur.LoopSlug)}, nil
	}
	if cur.LoopSlug == slug {
		return nil, nil
	}
	if _, err := s.w.Exec(`UPDATE intake SET loop_slug = ? WHERE id = ?`, slug, id); err != nil {
		return nil, fmt.Errorf("store: set intake loop %s: %w", id, err)
	}
	return nil, nil
}

// SetIntakeRoot links the finished intake to the component root the commit
// registered — the one link the row may never re-make. Pass targetPath (may
// be empty) to record the person's chosen path in the SAME write: two facts,
// one history entry.
func (s *Store) SetIntakeRoot(id, rootID, targetPath string) (*Refusal, error) {
	cur, err := s.GetIntake(id)
	if err != nil {
		return nil, err
	}
	if rootID == "" {
		return &Refusal{Kind: "intake-root-empty",
			Sentence: fmt.Sprintf("refused: intake %s would link an empty root — the link names a component", id)}, nil
	}
	if cur.RootID != "" {
		return &Refusal{Kind: "intake-root-set",
			Sentence: fmt.Sprintf("refused: intake %s is already linked to %s — the link is history", id, cur.RootID)}, nil
	}
	var one int
	if err := s.r.QueryRow(`SELECT 1 FROM component WHERE id = ?`, rootID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &Refusal{Kind: "component-unknown",
				Sentence: fmt.Sprintf("refused: component %q does not exist — the link names a component that exists", rootID)}, nil
		}
		return nil, fmt.Errorf("store: check intake root %s: %w", rootID, err)
	}
	var tp any
	if targetPath != "" {
		tp = targetPath
	} else if cur.TargetPath != "" {
		return &Refusal{Kind: "intake-target-set",
			Sentence: fmt.Sprintf("refused: intake %s already records a target path — it and the root land together", id)}, nil
	}
	if _, err := s.w.Exec(`UPDATE intake SET root_id = ?, target_path = ? WHERE id = ?`, rootID, tp, id); err != nil {
		return nil, fmt.Errorf("store: link intake %s: %w", id, err)
	}
	return nil, nil
}

// GetIntake reads one session row; absence is the house-style error.
func (s *Store) GetIntake(id string) (Intake, error) {
	in, err := scanIntake(s.r.QueryRow(`SELECT `+intakeColumns+` FROM intake WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Intake{}, fmt.Errorf("store: intake %q does not exist", id)
	}
	if err != nil {
		return Intake{}, fmt.Errorf("store: read intake %s: %w", id, err)
	}
	return in, nil
}

// ListIntakes is every session, newest first. Filters belong to the derived
// state, which lives in the loop and the proposal, not in a column here.
func (s *Store) ListIntakes() ([]Intake, error) {
	rows, err := s.r.Query(`SELECT ` + intakeColumns + ` FROM intake ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list intakes: %w", err)
	}
	defer rows.Close()
	var out []Intake
	for rows.Next() {
		in, err := scanIntake(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan intake: %w", err)
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// AppendIntakeStep writes the next ledger row (ordinal = max+1 inside the
// transaction, so the UNIQUE key makes a lost update impossible). Detail is
// the caller's fixed template — the store carries it and never composes it.
func (s *Store) AppendIntakeStep(intakeID, step, outcome, detail string) (IntakeStep, *Refusal, error) {
	if !allStepOutcomes[outcome] {
		return IntakeStep{}, &Refusal{Kind: "outcome-unknown",
			Sentence: fmt.Sprintf("refused: step outcome %q is not one of done, skipped, failed", outcome)}, nil
	}
	if step == "" || detail == "" {
		return IntakeStep{}, &Refusal{Kind: "step-incomplete",
			Sentence: "refused: a ledger row names its step and says, in the fixed template, what happened"}, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return IntakeStep{}, nil, fmt.Errorf("store: append intake step: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM intake WHERE id = ?`, intakeID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IntakeStep{}, &Refusal{Kind: "intake-unknown",
				Sentence: fmt.Sprintf("refused: intake %q does not exist — a ledger needs its session", intakeID)}, nil
		}
		return IntakeStep{}, nil, fmt.Errorf("store: check intake %s: %w", intakeID, err)
	}
	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(ordinal),0)+1 FROM intake_step WHERE intake_id = ?`, intakeID).
		Scan(&next); err != nil {
		return IntakeStep{}, nil, fmt.Errorf("store: next ordinal for %s: %w", intakeID, err)
	}
	st := IntakeStep{IntakeID: intakeID, Ordinal: next, Step: step, Outcome: outcome, Detail: detail, At: s.Now()}
	if _, err := tx.Exec(`INSERT INTO intake_step (intake_id,ordinal,step,outcome,detail,at)
		VALUES (?,?,?,?,?,?)`, st.IntakeID, st.Ordinal, st.Step, st.Outcome, st.Detail, st.At); err != nil {
		return IntakeStep{}, nil, fmt.Errorf("store: append step %s of %s: %w", step, intakeID, err)
	}
	if err := tx.Commit(); err != nil {
		return IntakeStep{}, nil, fmt.Errorf("store: append intake step: %w", err)
	}
	return st, nil, nil
}

// IntakeSteps is the ledger in order — the resume map a re-run reads to know
// what is already done.
func (s *Store) IntakeSteps(intakeID string) ([]IntakeStep, error) {
	rows, err := s.r.Query(`SELECT intake_id, ordinal, step, outcome, detail, at
		FROM intake_step WHERE intake_id = ? ORDER BY ordinal`, intakeID)
	if err != nil {
		return nil, fmt.Errorf("store: read steps of %s: %w", intakeID, err)
	}
	defer rows.Close()
	var out []IntakeStep
	for rows.Next() {
		var st IntakeStep
		if err := rows.Scan(&st.IntakeID, &st.Ordinal, &st.Step, &st.Outcome, &st.Detail, &st.At); err != nil {
			return nil, fmt.Errorf("store: scan step: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
