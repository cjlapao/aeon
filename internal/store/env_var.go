package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// env_var.go — the rows behind the variable manager. The store holds CIPHERTEXT
// and flags; it never sees a plaintext value and owns no key. Encryption,
// resolution order and the agent-visibility rule live in internal/vault, which
// is the only caller of the value-bearing reads.

// EnvVarRow is one variable as stored. Cipher is opaque to the store.
type EnvVarRow struct {
	ID           string
	Name         string
	ProjectID    string // "" = global; otherwise a ROOT component's id
	Description  string
	Secret       bool
	AgentVisible bool
	Cipher       []byte
	CreatedAt    string
	UpdatedAt    string
}

// EnvVarChange is one ledger row: names and flags, never a value.
type EnvVarChange struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ProjectID  string `json:"projectId,omitempty"`
	Op         string `json:"op"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

const envVarColumns = `id, name, COALESCE(project_id,''), description, is_secret, agent_visible, value, created_at, updated_at`

func scanEnvVar(row interface{ Scan(...any) error }) (EnvVarRow, error) {
	var r EnvVarRow
	var secret, visible int
	if err := row.Scan(&r.ID, &r.Name, &r.ProjectID, &r.Description, &secret, &visible, &r.Cipher, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return EnvVarRow{}, err
	}
	r.Secret, r.AgentVisible = secret == 1, visible == 1
	return r, nil
}

// EnvVarGet reads one variable by name in one scope ("" = global).
func (s *Store) EnvVarGet(name, projectID string) (EnvVarRow, bool, error) {
	r, err := scanEnvVar(s.r.QueryRow(`SELECT `+envVarColumns+` FROM env_var WHERE name = ? AND COALESCE(project_id,'') = ?`, name, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return EnvVarRow{}, false, nil
	}
	if err != nil {
		return EnvVarRow{}, false, fmt.Errorf("store: read variable %s: %w", name, err)
	}
	return r, true, nil
}

// EnvVarList lists every variable, global first then by project, ordered by name.
func (s *Store) EnvVarList() ([]EnvVarRow, error) {
	rows, err := s.r.Query(`SELECT ` + envVarColumns + ` FROM env_var ORDER BY COALESCE(project_id,''), name`)
	if err != nil {
		return nil, fmt.Errorf("store: list variables: %w", err)
	}
	defer rows.Close()
	var out []EnvVarRow
	for rows.Next() {
		r, err := scanEnvVar(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list variables: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EnvVarUpsert writes a variable and its ledger row in one transaction. An empty
// Cipher on an EXISTING row keeps the stored value (a flags-only edit); on a new
// row it is refused — a variable without a value is nothing.
func (s *Store) EnvVarUpsert(ctx context.Context, in EnvVarRow, actor Actor, summary string) (EnvVarRow, error) {
	if !actor.validActor() {
		return EnvVarRow{}, fmt.Errorf("refused: a variable change names who made it")
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return EnvVarRow{}, fmt.Errorf("store: set variable %s: %w", in.Name, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := s.Now()
	existing, err := scanEnvVar(tx.QueryRowContext(ctx, `SELECT `+envVarColumns+` FROM env_var WHERE name = ? AND COALESCE(project_id,'') = ?`, in.Name, in.ProjectID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if len(in.Cipher) == 0 {
			return EnvVarRow{}, fmt.Errorf("refused: variable %s has no value yet — a new variable needs one", in.Name)
		}
		in.ID, in.CreatedAt, in.UpdatedAt = "var-"+newID(), now, now
		if _, err := tx.ExecContext(ctx, `INSERT INTO env_var (id,name,project_id,description,is_secret,agent_visible,value,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			in.ID, in.Name, nullable(in.ProjectID), in.Description, b2i(in.Secret), b2i(in.AgentVisible), in.Cipher, now, now); err != nil {
			return EnvVarRow{}, fmt.Errorf("store: insert variable %s: %w", in.Name, err)
		}
	case err != nil:
		return EnvVarRow{}, fmt.Errorf("store: read variable %s: %w", in.Name, err)
	default:
		in.ID, in.CreatedAt, in.UpdatedAt = existing.ID, existing.CreatedAt, now
		if len(in.Cipher) == 0 {
			in.Cipher = existing.Cipher
		}
		if _, err := tx.ExecContext(ctx, `UPDATE env_var SET description=?, is_secret=?, agent_visible=?, value=?, updated_at=? WHERE id=?`,
			in.Description, b2i(in.Secret), b2i(in.AgentVisible), in.Cipher, now, in.ID); err != nil {
			return EnvVarRow{}, fmt.Errorf("store: update variable %s: %w", in.Name, err)
		}
	}
	if err := envVarChangeTx(ctx, tx, in.Name, in.ProjectID, "set", summary, actor, now); err != nil {
		return EnvVarRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnvVarRow{}, fmt.Errorf("store: set variable %s: %w", in.Name, err)
	}
	return in, nil
}

// EnvVarDelete removes a variable and records it; false when there was none.
func (s *Store) EnvVarDelete(ctx context.Context, name, projectID string, actor Actor) (bool, error) {
	if !actor.validActor() {
		return false, fmt.Errorf("refused: a variable change names who made it")
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: delete variable %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM env_var WHERE name = ? AND COALESCE(project_id,'') = ?`, name, projectID)
	if err != nil {
		return false, fmt.Errorf("store: delete variable %s: %w", name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if err := envVarChangeTx(ctx, tx, name, projectID, "delete", "deleted variable "+name, actor, s.Now()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// EnvVarRecordRead writes the ledger row an agent's read of a variable owes.
func (s *Store) EnvVarRecordRead(ctx context.Context, name, projectID string, actor Actor) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: record variable read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := envVarChangeTx(ctx, tx, name, projectID, "agent_read", "read variable "+name, actor, s.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// EnvVarChanges lists the ledger newest first, for one name or all (limit <= 0 = 100).
func (s *Store) EnvVarChanges(name string, limit int) ([]EnvVarChange, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, name, COALESCE(project_id,''), op, summary, actor_kind, actor_id, occurred_at FROM env_var_change`
	args := []any{}
	if name != "" {
		q += ` WHERE name = ?`
		args = append(args, name)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list variable changes: %w", err)
	}
	defer rows.Close()
	var out []EnvVarChange
	for rows.Next() {
		var c EnvVarChange
		if err := rows.Scan(&c.ID, &c.Name, &c.ProjectID, &c.Op, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: list variable changes: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func envVarChangeTx(ctx context.Context, tx *sql.Tx, name, projectID, op, summary string, actor Actor, at string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO env_var_change (name, project_id, op, summary, actor_kind, actor_id, occurred_at) VALUES (?,?,?,?,?,?,?)`,
		name, nullable(projectID), op, summary, actor.Kind, actor.ID, at); err != nil {
		return fmt.Errorf("store: variable ledger (%s %s): %w", op, name, err)
	}
	return nil
}
