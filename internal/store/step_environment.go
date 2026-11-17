package store

// step_environment.go — what a step ACTUALLY ran in (S59 EV9).
//
// One row per distinct outcome per step, names only. The distinction from
// `environment` is the whole point: the environment record says what a loop is
// bound to, and this says what the process was given — which can differ, by a
// downgrade (EV6), by a service's injected variables (EV14), or by a refusal
// that stopped the command before it started.
//
// # Names, never values (EV7, EV9)
//
// `var_names` and `secret_refs` are JSON arrays of NAMES. The type has no
// field that could hold a value, and `execenv`'s `ChildEnv.Names()` is the only
// producer of the list — the type that can print a value is never handed to
// this writer. A step record that leaked a value would be the leak this slice
// exists to close, recorded on purpose.
//
// # Append-only
//
// UPDATE and DELETE are refused by trigger in the migration, so the record a
// step wrote at spawn is the record there is: an environment widened later
// cannot rewrite what a command could see then. No foreign key on `step_id`
// either (P3) — `store/archive.go` moves step rows out and the record outlives
// them, like history does.
//
// # Dedupe
//
// The dedupe is here rather than in a UNIQUE index because the distinctness is
// over PARSED JSON: two writes whose arrays differ only in order are the same
// outcome, and an index cannot know that. The runtime calls this once per
// spawn attempt; a step that retried the same command in the same environment
// gets one row, and a step that ran in two environments gets two.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// StepEnvironment is one `step_environment` row.
type StepEnvironment struct {
	ID              int64
	StepID          int64
	EnvironmentID   *string
	Backend         *string
	Downgraded      bool
	DowngradeReason *string
	RefusalKind     *string
	Refusal         *string
	VarNames        []string
	SecretRefs      []string
	ResolvedAt      string
}

// NewStepEnvironment is the writer's input. The refusal pair is what EV11
// decided, recorded rather than dropped: a step that never started still has to
// answer "why" a day later, from the row.
type NewStepEnvironment struct {
	StepID          int64
	EnvironmentID   *string
	Backend         *string
	Downgraded      bool
	DowngradeReason *string
	RefusalKind     *string
	Refusal         *string
	VarNames        []string
	SecretRefs      []string
}

// RecordStepEnvironment writes the row, or returns the identical row that is
// already there (the dedupe). It takes no Actor and appends no audit row: this
// is the runtime's own record of a spawn, not a person changing a record
// (EV17's audit covers environments, references and bindings).
func (s *Store) RecordStepEnvironment(in NewStepEnvironment) (StepEnvironment, error) {
	vars := canonicalNames(in.VarNames)
	refs := canonicalNames(in.SecretRefs)

	if existing, ok, err := s.findStepEnvironment(in.StepID, in.EnvironmentID, in.Backend,
		in.Downgraded, in.DowngradeReason, in.RefusalKind, in.Refusal, vars, refs); err != nil {
		return StepEnvironment{}, err
	} else if ok {
		return existing, nil
	}

	varNames, err := marshalNames(vars)
	if err != nil {
		return StepEnvironment{}, err
	}
	secretRefs, err := marshalNames(refs)
	if err != nil {
		return StepEnvironment{}, err
	}
	now := s.Now()
	res, err := s.w.Exec(`INSERT INTO step_environment (step_id, environment_id, backend, downgraded, downgrade_reason,
			refusal_kind, refusal, var_names, secret_refs, resolved_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		in.StepID, in.EnvironmentID, in.Backend, boolToInt(in.Downgraded), in.DowngradeReason,
		in.RefusalKind, in.Refusal, varNames, secretRefs, now)
	if err != nil {
		return StepEnvironment{}, fmt.Errorf("store: record step environment for step %d: %w", in.StepID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return StepEnvironment{}, fmt.Errorf("store: id of the step environment row for step %d: %w", in.StepID, err)
	}
	return StepEnvironment{
		ID: id, StepID: in.StepID, EnvironmentID: in.EnvironmentID, Backend: in.Backend,
		Downgraded: in.Downgraded, DowngradeReason: in.DowngradeReason,
		RefusalKind: in.RefusalKind, Refusal: in.Refusal,
		VarNames: vars, SecretRefs: refs, ResolvedAt: now,
	}, nil
}

// StepEnvironments reads a step's rows in the order they were written (EV18's
// step.environments). More than one row is not a bug: a step that ran a command
// in one environment and then in another has one row each, and the sequence IS
// the story.
func (s *Store) StepEnvironments(stepID int64) ([]StepEnvironment, error) {
	rows, err := s.r.Query(`SELECT id, step_id, environment_id, backend, downgraded, downgrade_reason,
			refusal_kind, refusal, var_names, secret_refs, resolved_at
		FROM step_environment WHERE step_id = ? ORDER BY id ASC`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: list step environments for %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []StepEnvironment
	for rows.Next() {
		e, err := scanStepEnvironment(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan step environment for %d: %w", stepID, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list step environments for %d: %w", stepID, err)
	}
	return out, nil
}

func (s *Store) findStepEnvironment(stepID int64, environmentID, backend *string, downgraded bool,
	downgradeReason, refusalKind, refusal *string, vars, refs []string) (StepEnvironment, bool, error) {
	rows, err := s.r.Query(`SELECT id, step_id, environment_id, backend, downgraded, downgrade_reason,
			refusal_kind, refusal, var_names, secret_refs, resolved_at
		FROM step_environment WHERE step_id = ? ORDER BY id ASC`, stepID)
	if err != nil {
		return StepEnvironment{}, false, fmt.Errorf("store: read step environments for %d: %w", stepID, err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanStepEnvironment(rows)
		if err != nil {
			return StepEnvironment{}, false, fmt.Errorf("store: scan step environment for %d: %w", stepID, err)
		}
		if samePtr(e.EnvironmentID, environmentID) && samePtr(e.Backend, backend) &&
			e.Downgraded == downgraded && samePtr(e.DowngradeReason, downgradeReason) &&
			samePtr(e.RefusalKind, refusalKind) && samePtr(e.Refusal, refusal) &&
			equalNames(e.VarNames, vars) && equalNames(e.SecretRefs, refs) {
			return e, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return StepEnvironment{}, false, fmt.Errorf("store: read step environments for %d: %w", stepID, err)
	}
	return StepEnvironment{}, false, nil
}

func scanStepEnvironment(row interface{ Scan(dest ...any) error }) (StepEnvironment, error) {
	var (
		e                                   StepEnvironment
		environmentID, backend, reason      sql.NullString
		kind, refusal, varNames, secretRefs sql.NullString
		downgraded                          int
	)
	if err := row.Scan(&e.ID, &e.StepID, &environmentID, &backend, &downgraded, &reason,
		&kind, &refusal, &varNames, &secretRefs, &e.ResolvedAt); err != nil {
		return StepEnvironment{}, err
	}
	e.EnvironmentID = nullStringPtr(environmentID)
	e.Backend = nullStringPtr(backend)
	e.DowngradeReason = nullStringPtr(reason)
	e.RefusalKind = nullStringPtr(kind)
	e.Refusal = nullStringPtr(refusal)
	e.Downgraded = downgraded != 0
	var err error
	if e.VarNames, err = unmarshalNames(varNames, "var_names"); err != nil {
		return StepEnvironment{}, err
	}
	if e.SecretRefs, err = unmarshalNames(secretRefs, "secret_refs"); err != nil {
		return StepEnvironment{}, err
	}
	return e, nil
}

// canonicalNames sorts, dedupes and drops empties: the shape a row is compared
// in, and the shape a reader should see. It is deliberately the same function
// the dedupe compares with, so "identical outcome" means the same thing in both
// halves of this file.
func canonicalNames(in []string) []string {
	seen := map[string]bool{}
	for _, n := range in {
		if n = strings.TrimSpace(n); n != "" {
			seen[n] = true
		}
	}
	if len(seen) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalNames writes one of the two name arrays. An empty list is `[]`, never
// NULL: "the step saw no variables" and "nobody recorded what the step saw" are
// different claims, and NULL is how the second one would hide in the first
// one's clothing (P3).
func marshalNames(names []string) (string, error) {
	if names == nil {
		names = []string{}
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return "", fmt.Errorf("store: encode step names: %w", err)
	}
	return string(raw), nil
}

func unmarshalNames(n sql.NullString, column string) ([]string, error) {
	if !n.Valid || n.String == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(n.String), &out); err != nil {
		return nil, fmt.Errorf("store: %s of a step environment row is not a JSON array: %w", column, err)
	}
	return canonicalNames(out), nil
}
