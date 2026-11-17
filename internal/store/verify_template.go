// verify_template.go — the inert templates (VG21): the rows a template is
// until a person applies it, the picker's read, and the all-or-nothing
// apply that copies them onto a component as origin='template' gates. The
// insert and the event row come from verify.go's shared machinery.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// --- the inert templates (VG21) ---------------------------------------------

// VerifyTemplate is one row of verify_template — a candidate gate. A
// template does NOTHING until a person applies it to a component (VG21):
// the row is a display + a copy source, and no loop reads it. T10's seed
// fills the system rows; the table is empty until then.
type VerifyTemplate struct {
	ID          string
	Stack       string
	Name        string
	Command     string
	Requirement string
	TimeoutS    int
	Origin      string
	Version     string
	CreatedAt   string
	UpdatedAt   string
	ArchivedAt  *string
}

// ListVerifyTemplates reads the live (non-archived) templates, grouped by
// stack and named within it — the "apply template" picker's read (VG22).
func (s *Store) ListVerifyTemplates() ([]VerifyTemplate, error) {
	rows, err := s.r.Query(`SELECT id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at, archived_at
		FROM verify_template WHERE archived_at IS NULL ORDER BY stack, name`)
	if err != nil {
		return nil, fmt.Errorf("store: list verify templates: %w", err)
	}
	defer rows.Close()
	var out []VerifyTemplate
	for rows.Next() {
		var (
			t        VerifyTemplate
			archived sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.Stack, &t.Name, &t.Command, &t.Requirement, &t.TimeoutS, &t.Origin, &t.Version, &t.CreatedAt, &t.UpdatedAt, &archived); err != nil {
			return nil, fmt.Errorf("store: list verify templates: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list verify templates: %w", err)
	}
	return out, nil
}

// VerifyApply is the outcome of an apply: the gates created (all of them,
// on success) or the refusal with every reason (on refusal, nothing was
// written — all or nothing).
type VerifyApply struct {
	Commands []VerifyCommand
	Refusal  VerifyRefusal
	Reason   string
}

// OK reports whether the apply was accepted.
func (a VerifyApply) OK() bool { return a.Refusal == VerifyRefusalNone }

// ApplyVerifyTemplates copies the named templates onto the component as
// origin='template' gates (VG21: a template does nothing until a person
// applies it; VG22's "apply template"). The whole set is ONE transaction:
// every name collision is checked BEFORE any write and reported in the
// sentence, so a refusal writes nothing — all or nothing. Each created gate
// takes the template's name, command, requirement and timeout; its position
// is one past the component's current last (each apply appends in the order
// asked). The event rows (op apply_template) land beside their gates.
//
// (The spec's one-line signature (componentID, templateID, names, actor) is
// realised as a list over template ids — one row per template — so the
// all-or-nothing and the per-collision report are properties of ONE store
// call, not of a loop at the daemon.)
func (s *Store) ApplyVerifyTemplates(a Actor, componentID string, templateIDs []string) (VerifyApply, error) {
	if r, why := a.verifyActorRefusal(); r != VerifyRefusalNone {
		return VerifyApply{Refusal: r, Reason: why}, nil
	}
	if len(templateIDs) == 0 {
		return VerifyApply{Refusal: VerifyRefusalTemplateUnknown, Reason: "refused: no templates to apply"}, nil
	}
	exists, archived, err := s.componentState(componentID)
	if err != nil {
		return VerifyApply{}, err
	}
	if !exists {
		return VerifyApply{Refusal: VerifyRefusalComponentUnknown,
			Reason: fmt.Sprintf("refused: %s is not a component — a gate needs a node it can sit on", componentID)}, nil
	}
	if archived {
		return VerifyApply{Refusal: VerifyRefusalComponentArchived,
			Reason: fmt.Sprintf("refused: %s is archived — its gates cannot be written", componentID)}, nil
	}
	// Resolve every template FIRST: an unknown one is a refusal, and the
	// names are gathered so the collision check below sees the whole set.
	type pending struct {
		t    VerifyTemplate
		name string
	}
	var pendings []pending
	for _, id := range templateIDs {
		var (
			t        VerifyTemplate
			archived sql.NullString
		)
		err := s.r.QueryRow(`SELECT id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at, archived_at
			FROM verify_template WHERE id = ?`, id).
			Scan(&t.ID, &t.Stack, &t.Name, &t.Command, &t.Requirement, &t.TimeoutS, &t.Origin, &t.Version, &t.CreatedAt, &t.UpdatedAt, &archived)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return VerifyApply{Refusal: VerifyRefusalTemplateUnknown,
					Reason: fmt.Sprintf("refused: %s is not a template (or it is archived)", id)}, nil
			}
			return VerifyApply{}, fmt.Errorf("store: apply template %s: %w", id, err)
		}
		pendings = append(pendings, pending{t: t, name: t.Name})
	}
	// The collision check over the WHOLE set, reporting each collision: the
	// names already live on the component, plus duplicates inside the set
	// itself (applying the same template twice is a collision with itself).
	var collisions []string
	seen := map[string]bool{}
	for _, p := range pendings {
		if seen[p.name] {
			collisions = append(collisions, p.name+" (twice in this apply)")
			continue
		}
		seen[p.name] = true
		if s.liveGateNameTaken(componentID, p.name) {
			collisions = append(collisions, p.name)
		}
	}
	if len(collisions) > 0 {
		return VerifyApply{Refusal: VerifyRefusalTemplateCollision,
			Reason: "refused: the gate name is already taken on " + componentID + " by: " + strings.Join(collisions, ", ") + " — nothing was written (all or nothing)"}, nil
	}

	now := s.Now()
	basePos, err := s.nextGatePosition(componentID)
	if err != nil {
		return VerifyApply{}, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return VerifyApply{}, fmt.Errorf("store: apply templates to %s: %w", componentID, err)
	}
	defer tx.Rollback()
	out := make([]VerifyCommand, 0, len(pendings))
	for i, p := range pendings {
		g := VerifyCommand{
			ID:          "vc-" + newID(),
			ComponentID: componentID,
			Name:        p.t.Name,
			Command:     p.t.Command,
			Requirement: p.t.Requirement,
			TimeoutS:    p.t.TimeoutS,
			Position:    basePos + i,
			Enabled:     true,
			Origin:      "template",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		tplID := p.t.ID
		g.TemplateID = &tplID
		if err := insertVerifyCommand(tx, g); err != nil {
			return VerifyApply{}, fmt.Errorf("store: apply template %s: %w", p.t.ID, err)
		}
		// The revision is per created gate (a new command's first row); the
		// summary names the template by id — the id, not its content (SEC-033).
		if err := appendGateEvent(tx, a, g, 1, "apply_template", now, nil, &g); err != nil {
			return VerifyApply{}, fmt.Errorf("store: apply template %s: %w", p.t.ID, err)
		}
		out = append(out, g)
	}
	if err := tx.Commit(); err != nil {
		return VerifyApply{}, fmt.Errorf("store: apply templates to %s: %w", componentID, err)
	}
	return VerifyApply{Commands: out}, nil
}

// --- T10's seed write path (VG21) -------------------------------------
//
// The helpers below are the ONLY way a `verify_template` row is
// written besides the person's apply copy above. They exist because T3
// left the comment "T10's seed fills the system rows" without building
// the tx helpers the seed engine's Applier needs (the engine hands one
// transaction and the applier must live inside it — the read, the
// plan and the write all see one state). The shape is the toolseed
// twin (`ToolUpsertTx` / `SeedToolRow` / `SeedToolRetire`), not an
// invention.

// VerifyWithTx opens one transaction on the single writer and runs fn
// over it — the domain's one write boundary, the SeedWithTx/McpWithTx/
// BlueprintWithTx shape (DB15). The seed applier receives the engine's
// tx and never opens one; this is for callers outside the engine that
// hold a whole verify-domain decision (and for the tests that stand in
// for the database's own hand).
func (s *Store) VerifyWithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: verify with tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: verify commit: %w", err)
	}
	return nil
}

// VerifyTemplateRowTx reads one template row **inside the caller's
// transaction** — the seed engine's fingerprint read. present=false is
// the absent answer (nil row). It reads by id, the seed key, which is
// the "stack/name" pair the record is stable under.
func (s *Store) VerifyTemplateRowTx(ctx context.Context, tx *sql.Tx, id string) (VerifyTemplate, bool, error) {
	if tx == nil {
		return VerifyTemplate{}, false, errors.New("store: VerifyTemplateRowTx needs the caller's transaction")
	}
	var (
		t        VerifyTemplate
		archived sql.NullString
	)
	err := tx.QueryRowContext(ctx, `SELECT id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at, archived_at
		FROM verify_template WHERE id = ?`, id).
		Scan(&t.ID, &t.Stack, &t.Name, &t.Command, &t.Requirement, &t.TimeoutS, &t.Origin, &t.Version, &t.CreatedAt, &t.UpdatedAt, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return VerifyTemplate{}, false, nil
	}
	if err != nil {
		return VerifyTemplate{}, false, fmt.Errorf("store: reading the verify template %q: %w", id, err)
	}
	if archived.Valid {
		t.ArchivedAt = &archived.String
	}
	return t, true, nil
}

// VerifyTemplateUpsertTx inserts or updates one system template row
// (VG21: the seed ships origin='system' rows that do nothing until a
// person applies them). Origin is fixed to 'system' in the INSERT and is
// NOT touched by the update — a row's origin is set at birth and no
// re-apply flips it (the blueprint BP6 rule, kept by the literal).
// created_at and archived_at are likewise never touched on update:
// a retired template is never resurrected by a re-seed; the person's
// restore door (`verify.restore`) is the only door that un-retires.
func VerifyTemplateUpsertTx(tx *sql.Tx, row VerifyTemplate) error {
	if tx == nil {
		return errors.New("store: VerifyTemplateUpsertTx needs the caller's transaction — a template row never commits on its own")
	}
	if row.ID == "" || row.Stack == "" || row.Name == "" {
		return errors.New("store: VerifyTemplateUpsertTx needs an id, a stack and a name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.Exec(`
		INSERT INTO verify_template (id, stack, name, command, requirement, timeout_s, origin, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'system', ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			stack       = excluded.stack,
			name        = excluded.name,
			command     = excluded.command,
			requirement = excluded.requirement,
			timeout_s   = excluded.timeout_s,
			version     = excluded.version,
			updated_at  = excluded.updated_at`,
		row.ID, row.Stack, row.Name, row.Command, row.Requirement, row.TimeoutS, row.Version, now, now)
	if err != nil {
		return fmt.Errorf("store: upserting the verify template %q: %w", row.ID, err)
	}
	return nil
}

// VerifyTemplateRetireTx sets archived_at on the row — the seed's Retire
// (SD6 + VG21's "removal never resurrects"): the row is kept so applied
// gates keep naming their template, and ListVerifyTemplates stops
// offering it. It never deletes, and it says whether a row answered.
func (s *Store) VerifyTemplateRetireTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	if tx == nil {
		return false, errors.New("store: VerifyTemplateRetireTx needs the caller's transaction")
	}
	res, err := tx.ExecContext(ctx, `UPDATE verify_template SET archived_at = ? WHERE id = ? AND archived_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, fmt.Errorf("store: retiring the verify template %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: retiring the verify template %q: %w", id, err)
	}
	return n > 0, nil
}
