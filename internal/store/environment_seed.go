package store

// environment_seed.go — S59 T6 (EV19): the ONE write path the
// `environments.standard` set has.
//
// The door writes `user` rows; only this file writes a `system` one, and only
// under a seed transaction. That separation is the whole of EV19's trust story:
// a socket method that could claim `origin='system'` would be a method that
// could forge the seed, and the seed's authority is precisely that nobody at a
// door authored it. The engine (S43-extra) decides WHEN to call this — the
// startup apply path calls it for a key with no row and skips a present one —
// so divergence is decided by the plan above and defended here by refusing to
// touch a row that already exists.
//
// The three rules EV19 states, in the order the code applies them:
//
//  1. **Idempotence.** Re-applying the same record writes nothing.
//  2. **Divergence is never overwritten.** A row that exists is left exactly as
//     it is, whether the person edited it or archived it. The seed's power ends
//     where a record begins.
//  3. **Restore is explicit.** Re-applying over a diverged row is the
//     `seed.restore` path's act, not the startup's, and this function does the
//     same thing either way: it does not write over a row.

import (
	"database/sql"
	"errors"
	"fmt"
)

// SeedActorKind and SeedActorID stamp the audit row a seeded record earns. The
// kind is `system` because the act is the system's, and the id is the engine's
// own actor id at startup — an audit row that said "user" would be a record of
// an act no person took (EV17: every changing write names an author that exists).
const (
	SeedActorKind = "system"
	SeedActorID   = "startup"
)

// SeedEnvironment is the record the seed applies: the same fields
// `NewEnvironment` carries, minus the provenance the door is forbidden to name
// (origin, legacy, revision, timestamps) — those are this function's to stamp,
// and a caller that could set them could set `origin='system'` from a door.
type SeedEnvironment struct {
	Slug        string
	Name        string
	Description string
	Backend     string
	Spec        string
}

// SeedEnvironmentTx writes the system environment row on the engine's shared
// transaction (I2: the store holds the SQL, the seed package holds the record).
//
// It answers `created` — true only when this call wrote the row — and never
// errors on "already there": the row's existence is the ordinary case on the
// second daemon start, not a failure. An existing row is left untouched even
// when its content differs (rule 2): the engine's plan has already decided
// whether this key should be applied at all, and a store that quietly repaired
// a diverged record would be the thing EV19 forbids — the code's opinion
// riding over a person's choice.
func (s *Store) SeedEnvironmentTx(tx *sql.Tx, in SeedEnvironment) (Environment, bool, error) {
	if in.Slug == "" || !environmentSlugPattern.MatchString(in.Slug) {
		return Environment{}, false, fmt.Errorf("store: seed environment slug %q is not a slug", in.Slug)
	}
	if in.Name == "" {
		return Environment{}, false, fmt.Errorf("store: seed environment %s needs a name", in.Slug)
	}
	if !environmentBackends[in.Backend] {
		return Environment{}, false, fmt.Errorf("store: seed environment %s names backend %q, outside the closed set", in.Slug, in.Backend)
	}
	if ref := environmentSpecRefusal(in.Spec); ref != nil {
		return Environment{}, false, fmt.Errorf("store: seed environment %s: %s", in.Slug, ref.Sentence)
	}

	existing, found, err := environmentBySlugTx(tx, in.Slug)
	if err != nil {
		return Environment{}, false, err
	}
	if found {
		// The row exists. Idempotence for the matching case, non-interference
		// for every other — including a row a person archived, which is their
		// act and stays their act.
		return existing, false, nil
	}

	now := s.Now()
	e := Environment{
		ID: environmentIDPrefix + newID(), Slug: in.Slug, Name: in.Name, Description: in.Description,
		Backend: in.Backend, Spec: in.Spec,
		// `system` is the whole point of this function and the reason it is not
		// the door's CreateEnvironment: a system row is the set's, its drift is
		// reported by the ledger, and no socket method can author one.
		Origin: "system", Legacy: 0, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.Exec(`INSERT INTO environment (id, slug, name, description, backend, spec, origin, legacy, revision, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Slug, e.Name, e.Description, e.Backend, e.Spec, e.Origin, e.Legacy, e.Revision, e.CreatedAt, e.UpdatedAt); err != nil {
		return Environment{}, false, fmt.Errorf("store: seed insert environment %s: %w", e.ID, err)
	}
	if err := appendEnvironmentChange(tx, Actor{Kind: SeedActorKind, ID: SeedActorID}, now, "environment", e.ID, "create",
		&e.Revision, &e.Spec,
		fmt.Sprintf("environment seed %s · backend %s · revision 1 · origin system", e.ID, e.Backend)); err != nil {
		return Environment{}, false, fmt.Errorf("store: seed audit environment %s: %w", e.ID, err)
	}
	return e, true, nil
}

// EnvironmentRowTx reads one environment on the engine's transaction, for the
// ledger's fingerprint: the applier hashes what this returns, in the same
// canonical form its `Source()` wrote, so "did the row change?" is a content
// comparison and not a column-by-column argument (the watchdog set's shape,
// carried onto this one). A row the seed does not know about is `false`, and an
// ARCHIVED row is returned as it is — an archived system row is a divergence
// the ledger reports, never a gap that hides one.
func (s *Store) EnvironmentRowTx(tx *sql.Tx, slug string) (Environment, bool, error) {
	return environmentBySlugTx(tx, slug)
}

// environmentBySlugTx is the one keyed read this file needs, shared by the
// probe the seed insert makes and the fingerprint the ledger asks for. Written
// once so the two can never disagree about what "the row is there" means — a
// probe that used different columns than the read would report divergence
// where there is none (P3's third state, missed).
func environmentBySlugTx(tx *sql.Tx, slug string) (Environment, bool, error) {
	var e Environment
	var archived sql.NullString
	err := tx.QueryRow(`SELECT id, slug, name, description, backend, spec, origin, legacy, revision, created_at, updated_at, archived_at
		FROM environment WHERE slug = ?`, slug).
		Scan(&e.ID, &e.Slug, &e.Name, &e.Description, &e.Backend, &e.Spec,
			&e.Origin, &e.Legacy, &e.Revision, &e.CreatedAt, &e.UpdatedAt, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return Environment{}, false, nil
	}
	if err != nil {
		return Environment{}, false, fmt.Errorf("store: seed read environment %s: %w", slug, err)
	}
	e.ArchivedAt = nullStringPtr(archived)
	return e, true, nil
}

// SeedWorkspaceBindingTx is EV19's FIRST-BINDING rule, and the only place the
// workspace scope can be written by something that is not a person's door:
//
//	bind the workspace default ONLY IF no workspace binding exists
//
// A fresh install is never left with no environment — that is the state in
// which every command is refused (EV2 has no fallback), and a daemon that
// starts up into it is a daemon that does not work. But the rule is
// first-binding and not last-write: once a person has chosen, the choice is
// the record, and a daemon restart must not silently move it back to the
// shipped default. So the row's existence is checked on the SAME transaction
// as the insert, and the answer says whether this call bound anything.
func (s *Store) SeedWorkspaceBindingTx(tx *sql.Tx, environmentID string) (bool, error) {
	if environmentID == "" {
		return false, fmt.Errorf("store: the first binding needs the environment to bind")
	}
	var existingID string
	err := tx.QueryRow(`SELECT environment_id FROM environment_binding WHERE scope_kind = 'workspace'`).Scan(&existingID)
	if err == nil {
		return false, nil // a person (or an earlier start) already chose; it is theirs now
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("store: seed probe workspace binding: %w", err)
	}
	var slug, backend string
	err = tx.QueryRow(`SELECT slug, backend FROM environment WHERE id = ?`, environmentID).Scan(&slug, &backend)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("store: the system environment %s is not there to bind (EV19: the set writes its row before its binding, in one transaction)", environmentID)
	}
	if err != nil {
		return false, fmt.Errorf("store: seed read system environment %s: %w", environmentID, err)
	}
	now := s.Now()
	if _, err := tx.Exec(`INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
		VALUES ('workspace', NULL, ?, ?)`, environmentID, now); err != nil {
		return false, fmt.Errorf("store: seed insert workspace binding: %w", err)
	}
	if err := appendEnvironmentChange(tx, Actor{Kind: SeedActorKind, ID: SeedActorID}, now, "binding", "workspace", "bind",
		nil, nil,
		fmt.Sprintf("binding workspace to %s (backend %s) · first binding · origin system", environmentID, backend)); err != nil {
		return false, fmt.Errorf("store: seed audit workspace binding: %w", err)
	}
	return true, nil
}
