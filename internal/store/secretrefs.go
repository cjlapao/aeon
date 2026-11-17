package store

// secretrefs.go — where a secret comes from, never the secret (S59 EV7).
//
// A `secret_ref` row is a WHERE, not a WHAT. Read the column list and the
// claim is structural rather than promised: there is no column that could hold
// a value, so a backup, an export, a transcript or a bug in this file cannot
// carry one either (BK, C17). The three sources are:
//
//   - `daemon-env` — `source` is the NAME of a variable the daemon holds;
//   - `key-file`   — `source` is an absolute path the daemon reads at spawn;
//   - `store`      — `source` is an id in Aeon's own credential file, the
//     `internal/secrets` 0600 store (approved by the operator at T0's A1).
//
// # What this file refuses and why it is the one that refuses it
//
// A reference naming one of the daemon's own credential variables is refused
// at the write. That is not a duplicate of `execenv`'s deny list: `execenv`
// refuses a NAME reaching a child, and this refuses a NAME being *registered*
// as a source. The two must both exist because the second one is what makes a
// `secretref list` safe to show a person — a list of rows that are each a
// pointer at the forge token would be a UI for pointing at the forge token.
//
// The name list is PASSED IN by the caller. The store does not import the
// daemon, the forge, or `internal/config`: what counts as one of the daemon's
// credentials is those packages' knowledge, and the store's job is to hold a
// row that names a source, not to decide whose secrets exist.
//
// # Values
//
// None, anywhere, including in a refusal. A refusal that echoes `source` is
// safe only because `source` is a name or a path — which is exactly why the
// two are the only things a row may hold.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The closed sets, mirrored in migration 20261103063823's CHECKs. The CHECK is
// the net; these are the door in front of it, so a typo answers with a
// sentence rather than with a constraint violation naming a column.
var (
	secretRefSourceKinds = setOf("daemon-env", "key-file", "store")
	secretRefActions     = setOf("create", "update", "archive", "restore")
)

// The id prefix and the name shape. A reference name is an address a person
// types (`aeon secretref show db-url`) and a spec member (`"secrets":
// ["db-url"]`), so it is a slug and not free text.
const secretRefIDPrefix = "sref-"

var (
	secretRefNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	secretRefInjectPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

// SecretRef is one `secret_ref` row. There is no field for a value because
// there is no column for one — a type that could hold a secret would be a
// type that eventually does (EV7).
type SecretRef struct {
	ID          string
	Name        string
	Description string
	SourceKind  string
	Source      string
	InjectAs    string
	Origin      string
	CreatedAt   string
	UpdatedAt   string
	ArchivedAt  *string
}

// Archived reports whether the row is archived. An archived reference is not
// resolvable and cannot be named by a live environment (EV7).
func (r SecretRef) Archived() bool { return r.ArchivedAt != nil }

// NewSecretRef is EV18's secretref.create. `Origin` is not a caller field: a
// `system` row is the seed's to write (EV19).
type NewSecretRef struct {
	Name        string
	Description string
	SourceKind  string
	Source      string
	InjectAs    string
}

// UpdateSecretRef is EV18's secretref.update. Name is deliberately absent: a
// reference's name is what its environments' specs say, so renaming one would
// silently unbind every environment that named it — the far worse alternative
// to a person archiving the old one and creating the new one (EV17).
type UpdateSecretRef struct {
	Name        string
	Description *string
	SourceKind  *string
	Source      *string
	InjectAs    *string
}

// SecretRefFilter is the listing read.
type SecretRefFilter struct {
	SourceKind      string
	IncludeArchived bool
	Limit           int
}

// CreateSecretRef writes one reference (EV18's secretref.create). Every guard
// runs BEFORE the first statement, and the audit row lands in the same tx as
// the row it describes (EV17).
func (s *Store) CreateSecretRef(a Actor, in NewSecretRef, reserved []string) (SecretRef, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return SecretRef{}, ref, nil
	}
	if in.Name == "" || !secretRefNamePattern.MatchString(in.Name) {
		return SecretRef{}, environmentRefusal("name-shape",
			"reference name %q is not a name — lowercase letters, digits, - and _, starting with a letter or digit, at most 64 (it is what an environment's spec will say)", in.Name), nil
	}
	if !secretRefSourceKinds[in.SourceKind] {
		return SecretRef{}, environmentRefusal("source-kind-unknown",
			"source kind %q is not one of %s (EV7)", in.SourceKind,
			strings.Join(sortedKeys(secretRefSourceKinds), ", ")), nil
	}
	if ref := secretRefSourceRefusal(in.Name, in.SourceKind, in.Source, reserved); ref != nil {
		return SecretRef{}, ref, nil
	}
	if in.InjectAs == "" || !secretRefInjectPattern.MatchString(in.InjectAs) {
		return SecretRef{}, environmentRefusal("inject-as-shape",
			"inject_as %q is not a variable name — a letter or underscore, then letters, digits or underscores (it is the name the child will see)", in.InjectAs), nil
	}
	if _, err := s.GetSecretRef(in.Name); err == nil {
		return SecretRef{}, environmentRefusal("name-taken",
			"a secret reference is already named %q — names are immutable and a second one would be shadowed, not preferred", in.Name), nil
	} else if !errors.Is(err, ErrNotFound) {
		return SecretRef{}, nil, fmt.Errorf("store: probe secret ref %s: %w", in.Name, err)
	}

	now := s.Now()
	r := SecretRef{
		ID:          secretRefIDPrefix + newID(),
		Name:        in.Name,
		Description: in.Description,
		SourceKind:  in.SourceKind,
		Source:      in.Source,
		InjectAs:    in.InjectAs,
		Origin:      "user",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	tx, err := s.w.Begin()
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: create secret ref %s: %w", r.Name, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO secret_ref (id, name, description, source_kind, source, inject_as, origin, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Description, r.SourceKind, r.Source, r.InjectAs, r.Origin, r.CreatedAt, r.UpdatedAt); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: insert secret ref %s: %w", r.Name, err)
	}
	if err := appendEnvironmentChange(tx, a, now, "secret_ref", r.ID, "create", nil, nil,
		fmt.Sprintf("secret_ref create %s · source kind %s · inject as %s · by %s",
			r.Name, r.SourceKind, r.InjectAs, a.ID)); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: audit create secret ref %s: %w", r.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: create secret ref %s: %w", r.Name, err)
	}
	return r, nil, nil
}

// secretRefSourceRefusal is EV7's write-time door, and the one place a
// reference can be stopped from pointing at the daemon's own credentials.
func secretRefSourceRefusal(name, kind, source string, reserved []string) *EnvironmentRefusal {
	if strings.TrimSpace(source) == "" {
		return environmentRefusal("source-empty",
			"a %s reference needs a source — an empty one resolves to nothing at spawn and reads as a reference that exists (EV7)", kind)
	}
	switch kind {
	case "key-file":
		if !strings.HasPrefix(source, "/") {
			return environmentRefusal("key-file-relative",
				"the key-file source %q is not an absolute path — a relative path resolves against whatever the daemon's working directory happens to be, which turns a reference into a read of an unexpected file (EV7)", source)
		}
		if strings.Contains(source, "..") {
			return environmentRefusal("key-file-traversal",
				"the key-file source %q contains .. — a stored path is opened long after it was written, and a path that escapes is a path nobody reviewed", source)
		}
	case "store":
		// The `internal/secrets` credential file names are validated by that
		// package when the value is read; here the store refuses only what it
		// can see: a source that is not a single path-free component would be
		// a path in disguise, and a credential file path belongs to the
		// secrets package, not to a row.
		if strings.ContainsAny(source, "/\\\x00") || source == "." || strings.HasPrefix(source, ".") {
			return environmentRefusal("store-source-shape",
				"the store source %q is not a credential id — it is a name in Aeon's own credential file, not a path (EV7)", source)
		}
	case "daemon-env":
		upper := strings.ToUpper(source)
		if !varNameShape(upper) {
			return environmentRefusal("daemon-env-shape",
				"the daemon-env source %q is not a variable name — it is the NAME of a variable the daemon holds, and the name is what gets looked up (EV7)", source)
		}
		for _, r0 := range reserved {
			if r0 == "" {
				continue
			}
			if strings.EqualFold(source, r0) {
				return environmentRefusal("daemon-credential",
					"the reference %s would read the daemon's own variable %s — that value is the daemon's credential, not this loop's, and a reference to it is how a step ends up printing the key that runs it (EV7)",
					name, upper)
			}
		}
	}
	return nil
}

// varNameShape is the store's own copy of the shape rule. It is not imported
// from execenv: the store's import set is model, database, paths, mergerule,
// and a schema-level shape check does not earn widening it. The two are pinned
// equal by a test in execenv_schema_test.go.
func varNameShape(upper string) bool {
	if upper == "" || len(upper) > 128 {
		return false
	}
	for i := 0; i < len(upper); i++ {
		c := upper[i]
		switch {
		case c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// UpdateSecretRef changes the members a reference is made of, never its name
// (see UpdateSecretRef's comment). A nil pointer means "unchanged", which is
// the three-state that an empty string cannot express.
func (s *Store) UpdateSecretRef(a Actor, in UpdateSecretRef) (SecretRef, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return SecretRef{}, ref, nil
	}
	cur, err := s.GetSecretRef(in.Name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SecretRef{}, environmentRefusal("secret-ref-unknown",
				"there is no secret reference named %q to update (EV7)", in.Name), nil
		}
		return SecretRef{}, nil, err
	}
	next := cur
	if in.Description != nil {
		next.Description = *in.Description
	}
	if in.SourceKind != nil {
		next.SourceKind = *in.SourceKind
	}
	if in.Source != nil {
		next.Source = *in.Source
	}
	if in.InjectAs != nil {
		next.InjectAs = *in.InjectAs
	}
	if !secretRefSourceKinds[next.SourceKind] {
		return SecretRef{}, environmentRefusal("source-kind-unknown",
			"source kind %q is not one of %s (EV7)", next.SourceKind,
			strings.Join(sortedKeys(secretRefSourceKinds), ", ")), nil
	}
	if ref := secretRefSourceRefusal(cur.Name, next.SourceKind, next.Source, nil); ref != nil {
		return SecretRef{}, ref, nil
	}
	if !secretRefInjectPattern.MatchString(next.InjectAs) {
		return SecretRef{}, environmentRefusal("inject-as-shape",
			"inject_as %q is not a variable name (EV7)", next.InjectAs), nil
	}
	if next == cur {
		return cur, nil, nil // no-op: nothing changed, so no audit row (P3)
	}

	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: update secret ref %s: %w", cur.Name, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE secret_ref SET description = ?, source_kind = ?, source = ?, inject_as = ?, updated_at = ?
		WHERE id = ?`,
		next.Description, next.SourceKind, next.Source, next.InjectAs, now, cur.ID); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: update secret ref %s: %w", cur.Name, err)
	}
	if err := appendEnvironmentChange(tx, a, now, "secret_ref", cur.ID, "update", nil, nil,
		fmt.Sprintf("secret_ref update %s · source kind %s → %s · inject as %s → %s · by %s",
			cur.Name, cur.SourceKind, next.SourceKind, cur.InjectAs, next.InjectAs, a.ID)); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: audit update secret ref %s: %w", cur.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: update secret ref %s: %w", cur.Name, err)
	}
	next.UpdatedAt = now
	return next, nil, nil
}

// ArchiveSecretRef retires a reference — unless a live environment still names
// it (EV7). The refusal names every environment that does, because the caller
// cannot narrow a spec they cannot find, and "still referenced" without the
// list is a dead end with a period at the end.
func (s *Store) ArchiveSecretRef(a Actor, name string) (SecretRef, *EnvironmentRefusal, error) {
	return s.setArchivedSecretRef(a, name, true)
}

// RestoreSecretRef brings one back. It does NOT re-check the environments that
// name it: a reference that was resolvable before it was archived is
// resolvable again, and re-validating on restore would make a transient
// resolver failure permanent.
func (s *Store) RestoreSecretRef(a Actor, name string) (SecretRef, *EnvironmentRefusal, error) {
	return s.setArchivedSecretRef(a, name, false)
}

func (s *Store) setArchivedSecretRef(a Actor, name string, archive bool) (SecretRef, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return SecretRef{}, ref, nil
	}
	cur, err := s.GetSecretRef(name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return SecretRef{}, environmentRefusal("secret-ref-unknown",
				"there is no secret reference named %q (EV7)", name), nil
		}
		return SecretRef{}, nil, err
	}
	if cur.Archived() == archive {
		return cur, nil, nil // no-op: no row change, no audit row (P3)
	}
	if archive {
		users, err := s.environmentsReferencingSecretRef(cur.Name)
		if err != nil {
			return SecretRef{}, nil, err
		}
		if len(users) > 0 {
			return SecretRef{}, environmentRefusal("secret-ref-referenced",
				"%s is named by the live environment(s) %s — narrow those environments first, or leave the reference in place (EV7: a spec that names a missing reference cannot be bound, and cannot run either)",
				cur.Name, strings.Join(users, ", ")), nil
		}
	}

	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: archive=%v secret ref %s: %w", archive, cur.ID, err)
	}
	defer tx.Rollback()
	var res sql.Result
	if archive {
		res, err = tx.Exec(`UPDATE secret_ref SET archived_at = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`,
			now, now, cur.ID)
	} else {
		res, err = tx.Exec(`UPDATE secret_ref SET archived_at = NULL, updated_at = ? WHERE id = ? AND archived_at IS NOT NULL`,
			now, cur.ID)
	}
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: archive=%v secret ref %s: %w", archive, cur.ID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: archive=%v secret ref %s: %w", archive, cur.ID, err)
	} else if n == 0 {
		return cur, nil, nil // a concurrent write settled it; still a no-op (P3)
	}
	action := "archive"
	if !archive {
		action = "restore"
	}
	if err := appendEnvironmentChange(tx, a, now, "secret_ref", cur.ID, action, nil, nil,
		fmt.Sprintf("secret_ref %s %s · by %s", action, cur.Name, a.ID)); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: audit %s secret ref %s: %w", action, cur.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return SecretRef{}, nil, fmt.Errorf("store: archive=%v secret ref %s: %w", archive, cur.ID, err)
	}
	out := cur
	out.ArchivedAt = nil
	if archive {
		at := now
		out.ArchivedAt = &at
	}
	out.UpdatedAt = now
	return out, nil, nil
}

// GetSecretRef reads one by name. Names are the address (EV7's specs hold
// names, not ids), so the id is an internal detail a caller never types.
func (s *Store) GetSecretRef(name string) (SecretRef, error) {
	var (
		r        SecretRef
		archived sql.NullString
	)
	err := s.r.QueryRow(`SELECT id, name, description, source_kind, source, inject_as, origin, created_at, updated_at, archived_at
		FROM secret_ref WHERE name = ?`, name).
		Scan(&r.ID, &r.Name, &r.Description, &r.SourceKind, &r.Source, &r.InjectAs,
			&r.Origin, &r.CreatedAt, &r.UpdatedAt, &archived)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SecretRef{}, fmt.Errorf("store: secret ref %s: %w", name, ErrNotFound)
		}
		return SecretRef{}, fmt.Errorf("store: read secret ref %s: %w", name, err)
	}
	r.ArchivedAt = nullStringPtr(archived)
	return r, nil
}

// ListSecretRefs is EV18's secretref.list. Safe to show a person by
// construction: every field is a name or a path.
func (s *Store) ListSecretRefs(f SecretRefFilter) ([]SecretRef, error) {
	if f.SourceKind != "" && !secretRefSourceKinds[f.SourceKind] {
		return nil, fmt.Errorf("store: list secret refs: source kind %q is not in the closed set (EV7)", f.SourceKind)
	}
	var (
		conds []string
		args  []any
	)
	if f.SourceKind != "" {
		conds = append(conds, "source_kind = ?")
		args = append(args, f.SourceKind)
	}
	if !f.IncludeArchived {
		conds = append(conds, "archived_at IS NULL")
	}
	query := `SELECT id, name, description, source_kind, source, inject_as, origin, created_at, updated_at, archived_at
		FROM secret_ref`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY name ASC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list secret refs: %w", err)
	}
	defer rows.Close()
	var out []SecretRef
	for rows.Next() {
		var (
			r        SecretRef
			archived sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.SourceKind, &r.Source, &r.InjectAs,
			&r.Origin, &r.CreatedAt, &r.UpdatedAt, &archived); err != nil {
			return nil, fmt.Errorf("store: scan secret ref: %w", err)
		}
		r.ArchivedAt = nullStringPtr(archived)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list secret refs: %w", err)
	}
	return out, nil
}

// EnvironmentsReferencingSecretRef is the door-facing export of the archive
// check below (S59 T6, EV18): `secretref.list`/`.show` answer which LIVE
// environments name a reference, so archiving one says what it would strand
// before it is archived rather than after. No new SQL — the archive guard and
// the read answer from the same query, which is the only way the two can never
// disagree about what "referenced" means.
func (s *Store) EnvironmentsReferencingSecretRef(name string) ([]string, error) {
	return s.environmentsReferencingSecretRef(name)
}

// environmentsReferencingSecretRef answers the archive check: which LIVE
// environments name this reference, by slug. It walks the live rows and parses
// each spec rather than pattern-matching the JSON in SQL, because the question
// is "does this name appear in the two arrays that hold names" and a LIKE on
// JSON answers a different question — `"db-url"` also appears inside
// `"my-db-url"`, and an archive refused by a substring is a refusal nobody can
// reproduce by reading the spec.
//
// The table is a few dozen rows on any real install, so the walk costs nothing
// and says exactly what it checked.
func (s *Store) environmentsReferencingSecretRef(name string) ([]string, error) {
	rows, err := s.r.Query(`SELECT slug, spec FROM environment WHERE archived_at IS NULL ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("store: scan environments for %s: %w", name, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug, spec string
		if err := rows.Scan(&slug, &spec); err != nil {
			return nil, fmt.Errorf("store: scan environment for %s: %w", name, err)
		}
		names, err := secretRefNamesInSpec(spec)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if n == name {
				out = append(out, slug)
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: scan environments for %s: %w", name, err)
	}
	return out, nil
}

// secretRefNamesInSpec is the one parse of "which names does this spec
// mention" — the environment's own list plus every service's. Sorted and
// deduplicated, so two callers comparing specs compare sets.
func secretRefNamesInSpec(spec string) ([]string, error) {
	var doc struct {
		Env struct {
			Secrets []string `json:"secrets"`
		} `json:"env"`
		Services []struct {
			Secrets []string `json:"secrets"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		return nil, fmt.Errorf("store: parse environment spec for secret refs: %w", err)
	}
	seen := map[string]bool{}
	for _, n := range doc.Env.Secrets {
		if n = strings.TrimSpace(n); n != "" {
			seen[n] = true
		}
	}
	for _, svc := range doc.Services {
		for _, n := range svc.Secrets {
			if n = strings.TrimSpace(n); n != "" {
				seen[n] = true
			}
		}
	}
	return sortedKeys(seen), nil
}
