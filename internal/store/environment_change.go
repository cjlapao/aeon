package store

// environment_change.go — the environment plane's append-only audit (S59
// EV17), and the one way a `environment_change` row gets written.
//
// It is a separate file from environments.go by cohesion, not by necessity:
// opening environments.go is for "what does the record say", and opening this
// is for "who changed it, when, and how do I know". The two share one
// transaction and nothing else.
//
// The properties that matter, each pinned by a test in
// execenv_schema_test.go / execenv_store_test.go:
//
//   - one row per CHANGING write, in the SAME transaction as the write — an
//     audit that can commit apart from its write describes a write that may
//     never have happened;
//   - none for a read, a refusal, a dry-run or a no-op (P3);
//   - UPDATE and DELETE refused by trigger at the schema level, so no code
//     path, including a future one, can rewrite history;
//   - the actor arrives from the door and the summary is a template built at
//     the call site from fields, never a sentence a caller types (SEC-033).

import (
	"database/sql"
	"fmt"
	"strings"
)

// EnvironmentChange is one `environment_change` row (EV17): the append-only
// audit, one row per changing write, in the same transaction as that write.
// The three nullables are what makes the table honest about its three
// subjects: only an environment row carries a revision, only an environment
// or secret-reference row has a spec to record, a binding row has neither.
type EnvironmentChange struct {
	ID          int64
	At          string
	ActorKind   string
	ActorID     string
	SubjectKind string
	SubjectID   string
	Action      string
	Revision    *int64
	SpecAfter   *string
	Summary     string
}

// EnvironmentChangeFilter is the audit read (EV18's environment.changes
// {subject?, limit?}).
type EnvironmentChangeFilter struct {
	SubjectKind string
	SubjectID   string
	Action      string
	Limit       int // 0 = no limit
}

// --- the door pieces ---------------------------------------------------------

// environmentActorRefusal is the actor door every audit-writing write shares:
// a named author from the closed vocabulary, before any SQL. An anonymous or
// unvocabularied actor is a caller bug answered as a refusal, never as an
// error and never as a row nobody can attribute.

// --- the audit ---------------------------------------------------------------

// appendEnvironmentChange writes one `environment_change` row. It takes the
// *sql.Tx rather than opening one: the audit row and the row it describes are
// one unit, and an audit that can be committed apart from its write is an audit
// that will one day describe a write that never happened.
//
// The actor arrives from the door (EV17), never from params, and the summary is
// a fixed template built here rather than a string a caller types (SEC-033) —
// which is why this function takes fields and not a sentence.
func appendEnvironmentChange(tx *sql.Tx, a Actor, at string, subjectKind, subjectID, action string,
	revision *int64, specAfter *string, summary string) error {
	if !environmentSubjects[subjectKind] {
		return fmt.Errorf("store: audit subject %q is not environment, secret_ref or binding", subjectKind)
	}
	if !environmentActions[action] {
		return fmt.Errorf("store: audit action %q is not in the closed set", action)
	}
	if !environmentChangeActors[a.Kind] {
		return fmt.Errorf("store: audit actor kind %q is not in the closed set", a.Kind)
	}
	_, err := tx.Exec(`INSERT INTO environment_change
		(at, actor_kind, actor_id, subject_kind, subject_id, action, revision, spec_after, summary)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		at, a.Kind, a.ID, subjectKind, subjectID, action, revision, specAfter, summary)
	return err
}

// ListChanges reads the audit (EV18's environment.changes). Three states, per
// P3: an empty listing under a filter is the FILTER's answer, and the caller
// renders it as such — this function returns no rows and no error and does not
// claim that nothing was ever written.
func (s *Store) ListChanges(f EnvironmentChangeFilter) ([]EnvironmentChange, error) {
	if f.SubjectKind != "" && !environmentSubjects[f.SubjectKind] {
		return nil, fmt.Errorf("store: list changes: subject %q is not in the closed set", f.SubjectKind)
	}
	if f.Action != "" && !environmentActions[f.Action] {
		return nil, fmt.Errorf("store: list changes: action %q is not in the closed set", f.Action)
	}
	var (
		conds []string
		args  []any
	)
	if f.SubjectKind != "" {
		conds = append(conds, "subject_kind = ?")
		args = append(args, f.SubjectKind)
	}
	if f.SubjectID != "" {
		conds = append(conds, "subject_id = ?")
		args = append(args, f.SubjectID)
	}
	if f.Action != "" {
		conds = append(conds, "action = ?")
		args = append(args, f.Action)
	}
	query := `SELECT id, at, actor_kind, actor_id, subject_kind, subject_id, action, revision, spec_after, summary
		FROM environment_change`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY id DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list environment changes: %w", err)
	}
	defer rows.Close()
	var out []EnvironmentChange
	for rows.Next() {
		var (
			c        EnvironmentChange
			revision sql.NullInt64
			spec     sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.At, &c.ActorKind, &c.ActorID, &c.SubjectKind, &c.SubjectID,
			&c.Action, &revision, &spec, &c.Summary); err != nil {
			return nil, fmt.Errorf("store: scan environment change: %w", err)
		}
		if revision.Valid {
			v := revision.Int64
			c.Revision = &v
		}
		if spec.Valid {
			v := spec.String
			c.SpecAfter = &v
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list environment changes: %w", err)
	}
	return out, nil
}
