package store

// environments.go — the execution environment record and where it is bound
// (S59 EV2, EV17). One record, one binding table, one append-only audit.
//
// # What lives here and what does not
//
// This file owns the SQL half of `environment`, `environment_binding` and the
// `environment_change` audit (I2: SQL lives in internal/store and nowhere
// else). The audit helper is here because the audit is the environment plane's
// record; secretrefs.go calls it, nothing else writes an audit row.
//
// The SPEC's meaning is not decided here. `spec` is stored as canonical JSON
// that must parse, and the backend is a closed set — that is all the store
// asserts. `execenv`'s validator (EV4) is the door that says what a spec may
// hold, and the store cannot import it: store.go's import allow-list is
// model, database, paths, mergerule, and widening it for a schema question
// would be the wrong trade. A surface that wants "is this spec legal", asks
// execenv; a surface that wants "what does the row say", asks here.
//
// # The actor (EV17)
//
// Every *changing* write takes a store.Actor and appends one environment_change
// row IN THE SAME TRANSACTION. The actor comes from the door — the daemon's
// `table.ActorFor`, the peer credential behind the 0600 socket (P26) — and
// never from params: a write whose author the caller can type is a write
// nobody can trust. Reads, refusals, dry-runs and no-ops append nothing (P3),
// which is why the audit append sits after every guard and inside the same tx
// as the row it describes.
//
// # Discriminated results (DB19)
//
// A refusal is an *EnvironmentRefusal naming what it refused and why; `error`
// means the disk or the driver failed. A caller switches on Kind and renders
// Sentence verbatim.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The closed sets. The migration's CHECKs are the net (a raw statement cannot
// leak a vocabulary); these are the door in front of it, so a typo answers a
// sentence rather than a constraint violation naming a column.
var (
	environmentBackends = setOf("host-inherit", "host-scrubbed", "container", "vm")
	environmentOrigins  = setOf("system", "user")
	// environmentChangeActors is the audit's author vocabulary. It is wider
	// than actor.go's validActor (the records tier's user/agent/trigger) for
	// the same reason autonomy's is: this plane's writes also arrive from the
	// cli direct door and from the daemon itself (EV18's human door plus EV12's
	// reconcile). Who may WRITE an environment is the daemon's door guard
	// (EV18), not this file's; the store's job is that every write names an
	// author that exists in the vocabulary.
	environmentChangeActors = setOf("user", "cli", "system", "agent", "trigger", "daemon")
	environmentActions      = setOf("create", "update", "archive", "restore", "bind", "unbind")
	environmentSubjects     = setOf("environment", "secret_ref", "binding")
)

// environmentSlugPattern is the Go twin of what a slug may be: lowercase,
// digits, hyphen and underscore, starting with a letter or digit, ≤ 64. The
// migration gives `environment.slug` UNIQUE but no shape — the shape is a door
// decision, and a slug is an address users type (`aeon env show <slug>`), so
// it is checked before it becomes permanent.
var environmentSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// environmentIDPrefix is EV2's id shape: `env-<hex>`.
const environmentIDPrefix = "env-"

// Environment is one `environment` row (EV2). Spec is the closed JSON exactly
// as stored (EV4's shape is execenv's business); Legacy is 0/1 as the column
// has it, because `legacy = 1` is what `aeon doctor` and the panel warn on
// (EV6) and a bool would round-trip away the difference between "not legacy"
// and "not marked".
type Environment struct {
	ID          string
	Slug        string
	Name        string
	Description string
	Backend     string
	Spec        string
	Origin      string
	Legacy      int
	Revision    int64
	CreatedAt   string
	UpdatedAt   string
	ArchivedAt  *string
}

// Archived reports whether the row is archived. Resolution never picks one
// (EV2 refuses an archived binding's environment at bind time), and a listing
// shows it only under includeArchived.
func (e Environment) Archived() bool { return e.ArchivedAt != nil }

// NewEnvironment is a caller's request to create one (EV18's
// environment.create). Origin is not a caller field: a `system` row is the
// seed's to write (EV19), never something a door offers.
type NewEnvironment struct {
	Slug        string
	Name        string
	Description string
	Backend     string
	Spec        string
}

// UpdateEnvironment is EV18's environment.update {id, spec, expectedRevision}:
// the spec is the only writable member (EV2's name and slug are immutable, and
// widening this struct is a design amendment, not a convenience).
// ExpectedRevision is optimistic concurrency (EV17's lost-update failure): the
// caller read revision N, and a write against anything else is a refusal that
// names the current one.
type UpdateEnvironment struct {
	ID               string
	Spec             string
	ExpectedRevision int64
	// Note is an OPTIONAL suffix on the audit summary (S59 T7). The human
	// door leaves it empty and its audit rows are byte-identical to what
	// they were before the field existed; the committed environment_restrict
	// proposal puts the proposal id and the axes that tightened there, so an
	// append-only row says WHY the spec moved, not only that it did. It is
	// summary text, never a credential, and it is bounded — an audit row a
	// person cannot read in one line is not a summary.
	Note string
}

// maxUpdateNoteBytes bounds the audit suffix. The proposal path trims its own
// axis list before it gets here; this is the outer wall.
const maxUpdateNoteBytes = 1024

// EnvironmentRefusal is the discriminated result of an environment or binding
// write (DB19). A nil *EnvironmentRefusal means the write was admitted.
type EnvironmentRefusal struct {
	Kind     string
	Sentence string
}

func (r EnvironmentRefusal) Error() string {
	return fmt.Sprintf("refused: %s · %s", r.Kind, r.Sentence)
}

// environmentRefusal builds the one shape every refusal in this plane takes.
func environmentRefusal(kind, format string, args ...any) *EnvironmentRefusal {
	return &EnvironmentRefusal{Kind: kind, Sentence: fmt.Sprintf("refused: "+format, args...)}
}

// EnvironmentFilter is the listing read (EV18's environment.list
// {scope?, backend?, includeArchived?}). `scope` is resolved by the daemon
// (it needs execenv's resolution and the attendedness rule), so the store
// filter carries what a row is selected by, not what a step would run in.
type EnvironmentFilter struct {
	Backend         string // "" = any
	Origin          string // "" = any
	IncludeArchived bool
	Limit           int // 0 = no limit
}

// Binding is one `environment_binding` row (EV2). Exactly one row has
// ScopeKind "workspace" (the partial unique index is what enforces it, since
// SQLite treats NULLs as distinct), and at most one row per component.
type Binding struct {
	ScopeKind     string
	ComponentID   *string
	EnvironmentID string
	BoundAt       string
}

// NewBinding is a bind: scope "workspace" with no component id, or scope
// "component" naming one.
type NewBinding struct {
	ScopeKind     string
	ComponentID   string
	EnvironmentID string
	// DryRun validates everything — the actor, the scopes, the environment,
	// its secret references — and writes nothing, not even an audit row (P3:
	// a dry-run changes nothing, so it is not a changing write). EV18's
	// environment.bind {… dryRun?} answers the daemon's own statement of what
	// would change; the statement itself is the door's, this is the check.
	DryRun bool
}

func environmentActorRefusal(a Actor) *EnvironmentRefusal {
	if a.ID == "" {
		return environmentRefusal("actor-anonymous",
			"an environment write needs a named actor — an anonymous author cannot be audited (EV17)")
	}
	if !environmentChangeActors[a.Kind] {
		return environmentRefusal("actor-unknown-kind",
			"actor kind %q is not one of %s — an author nobody can name is not an author (EV17)",
			a.Kind, strings.Join(sortedKeys(environmentChangeActors), ", "))
	}
	return nil
}

// sortedKeys renders a closed set in a stable order for a sentence.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// environmentSpecRefusal checks what the store may check about a spec: it is
// JSON, it is an object, and it is not absurdly large. It does NOT interpret
// the members — EV4's validator owns that, in the store-free package.
func environmentSpecRefusal(spec string) *EnvironmentRefusal {
	if len(spec) > 1<<20 {
		return environmentRefusal("spec-too-large",
			"a spec is over 1 MiB — an environment description that big is not a description")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(spec), &probe); err != nil {
		return environmentRefusal("spec-invalid",
			"the spec is not a JSON object (%v) — write it through environment.create's validator, not by hand", err)
	}
	return nil
}

// --- writes ------------------------------------------------------------------

// CreateEnvironment adds one environment row and its audit row in one
// transaction (EV2, EV17). A refusal writes neither. Origin is 'user' here by
// definition: a `system` row is the seed engine's to write (EV19), and a door
// that could claim 'system' would be a door that could forge the seed.
func (s *Store) CreateEnvironment(a Actor, in NewEnvironment) (Environment, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return Environment{}, ref, nil
	}
	if in.Slug == "" || !environmentSlugPattern.MatchString(in.Slug) {
		return Environment{}, environmentRefusal("slug-shape",
			"slug %q is not a slug — lowercase letters, digits, - and _, starting with a letter or digit, at most 64", in.Slug), nil
	}
	if in.Name == "" {
		return Environment{}, environmentRefusal("name-empty", "an environment needs a name to show in a panel"), nil
	}
	if !environmentBackends[in.Backend] {
		return Environment{}, environmentRefusal("backend-unknown",
			"backend %q is not one of %s (EV3)", in.Backend, strings.Join(sortedKeys(environmentBackends), ", ")), nil
	}
	if ref := environmentSpecRefusal(in.Spec); ref != nil {
		return Environment{}, ref, nil
	}
	if _, err := s.GetEnvironment(in.Slug); err == nil {
		return Environment{}, environmentRefusal("slug-taken",
			"an environment is already named %q — slugs are immutable, so give this one another name", in.Slug), nil
	} else if !errors.Is(err, ErrNotFound) {
		return Environment{}, nil, fmt.Errorf("store: probe environment %s: %w", in.Slug, err)
	}

	now := s.Now()
	e := Environment{
		ID:          environmentIDPrefix + newID(),
		Slug:        in.Slug,
		Name:        in.Name,
		Description: in.Description,
		Backend:     in.Backend,
		Spec:        in.Spec,
		Origin:      "user",
		Legacy:      0,
		Revision:    1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: create environment %s: %w", e.ID, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO environment (id, slug, name, description, backend, spec, origin, legacy, revision, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Slug, e.Name, e.Description, e.Backend, e.Spec, e.Origin, e.Legacy, e.Revision, e.CreatedAt, e.UpdatedAt); err != nil {
		return Environment{}, nil, fmt.Errorf("store: insert environment %s: %w", e.ID, err)
	}
	if err := appendEnvironmentChange(tx, a, now, "environment", e.ID, "create", &e.Revision, &e.Spec,
		fmt.Sprintf("environment create %s · backend %s · revision 1 · by %s", e.ID, e.Backend, a.ID)); err != nil {
		return Environment{}, nil, fmt.Errorf("store: audit create environment %s: %w", e.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Environment{}, nil, fmt.Errorf("store: create environment %s: %w", e.ID, err)
	}
	return e, nil, nil
}

// UpdateEnvironment replaces the spec under optimistic concurrency (EV17's
// lost-update failure, EV18's environment.update). A stale ExpectedRevision is
// a refusal that NAMES the current revision, so the caller can re-read and
// decide; it is never an error and never a silent last-writer-wins.
//
// The revision bumps on every changing write to the row — update, archive,
// restore — and never resets: EV17's audit rows are diffable by revision, and
// a counter that resets would make two different rows claim the same revision.
func (s *Store) UpdateEnvironment(a Actor, in UpdateEnvironment) (Environment, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return Environment{}, ref, nil
	}
	e, err := s.GetEnvironment(in.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Environment{}, environmentRefusal("environment-unknown",
				"%s is not an environment (id or slug) — environment.list shows what exists", in.ID), nil
		}
		return Environment{}, nil, err
	}
	if e.Archived() {
		return Environment{}, environmentRefusal("environment-archived",
			"%s is archived — restore it before changing it (EV18's environment.restore)", e.ID), nil
	}
	if ref := environmentSpecRefusal(in.Spec); ref != nil {
		return Environment{}, ref, nil
	}
	if len(in.Note) > maxUpdateNoteBytes {
		return Environment{}, environmentRefusal("note-too-large",
			"the update's note is %d bytes and the cap is %d — an audit summary a person cannot read in one line is not a summary (S59 T7)", len(in.Note), maxUpdateNoteBytes), nil
	}
	if in.ExpectedRevision != e.Revision {
		return Environment{}, environmentRefusal("revision-stale",
			"%s is at revision %d, not the revision %d this write expected — re-read it and apply again (EV17)",
			e.Slug, e.Revision, in.ExpectedRevision), nil
	}
	if in.Spec == e.Spec {
		// A no-op writes nothing (P3) — same spec, same revision, no audit row.
		return e, nil, nil
	}

	now := s.Now()
	next := e.Revision + 1
	tx, err := s.w.Begin()
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: update environment %s: %w", e.ID, err)
	}
	defer tx.Rollback()
	// The revision is in the WHERE as well as the comparison above: two
	// concurrent updates cannot both land, whichever read first.
	res, err := tx.Exec(`UPDATE environment SET spec=?, revision=?, updated_at=? WHERE id=? AND revision=?`,
		in.Spec, next, now, e.ID, in.ExpectedRevision)
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: update environment %s: %w", e.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: count update of environment %s: %w", e.ID, err)
	}
	if n == 0 {
		// Lost the race between the read and the write: the same refusal, with
		// the revision the winner wrote.
		current, err := s.GetEnvironment(e.ID)
		if err != nil {
			return Environment{}, nil, err
		}
		return Environment{}, environmentRefusal("revision-stale",
			"%s moved to revision %d while this write was in flight — re-read it and apply again (EV17)",
			current.Slug, current.Revision), nil
	}
	spec := in.Spec
	summary := fmt.Sprintf("environment update %s · revision %d · by %s", e.ID, next, a.ID)
	if in.Note != "" {
		summary += " · " + in.Note
	}
	if err := appendEnvironmentChange(tx, a, now, "environment", e.ID, "update", &next, &spec,
		summary); err != nil {
		return Environment{}, nil, fmt.Errorf("store: audit update environment %s: %w", e.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Environment{}, nil, fmt.Errorf("store: update environment %s: %w", e.ID, err)
	}
	e.Spec, e.Revision, e.UpdatedAt = in.Spec, next, now
	return e, nil, nil
}

// ArchiveEnvironment sets archived_at (EV18's environment.archive) and appends
// its audit row. Archiving an environment that a live binding still names is
// refused, naming the scopes: EV2 has no code fallback, so a command whose
// binding points at an archived environment would be refused at the step — a
// person should not be able to arrange that failure from the settings panel.
// (EV18's own words: the workspace binding cannot be unbound; the same logic
// says it cannot be pointed at an archived row either.) An already-archived
// environment is a no-op: nothing changes, so no audit row (P3).
func (s *Store) ArchiveEnvironment(a Actor, idOrSlug string) (Environment, *EnvironmentRefusal, error) {
	return s.setArchivedEnvironment(a, idOrSlug, true)
}

// RestoreEnvironment clears archived_at (EV18's environment.restore). The
// revision bumps — the row changed, and EV17's history says when.
func (s *Store) RestoreEnvironment(a Actor, idOrSlug string) (Environment, *EnvironmentRefusal, error) {
	return s.setArchivedEnvironment(a, idOrSlug, false)
}

func (s *Store) setArchivedEnvironment(a Actor, idOrSlug string, archive bool) (Environment, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return Environment{}, ref, nil
	}
	e, err := s.GetEnvironment(idOrSlug)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Environment{}, environmentRefusal("environment-unknown",
				"%s is not an environment (id or slug) — environment.list shows what exists", idOrSlug), nil
		}
		return Environment{}, nil, err
	}
	if e.Origin == "system" && archive {
		// EV19: the seed's rows are the seed's. Archiving one is a person's act
		// on their own install, and the door is environment.archive — so it is
		// allowed; what is NOT allowed is a `system`-origin row appearing by
		// any other door, which CreateEnvironment already refuses.
		_ = e
	}
	if e.Archived() == archive {
		return e, nil, nil // no-op: nothing changed, so no audit row (P3)
	}
	if archive {
		scopes, err := s.bindingScopesFor(e.ID)
		if err != nil {
			return Environment{}, nil, err
		}
		if len(scopes) > 0 {
			return Environment{}, environmentRefusal("environment-bound",
				"%s is still bound at %s — bind those scopes to another environment first (EV2: a bound-but-archived environment leaves its commands with nothing to run in)",
				e.Slug, strings.Join(scopes, ", ")), nil
		}
	}

	now := s.Now()
	next := e.Revision + 1
	tx, err := s.w.Begin()
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: archive=%v environment %s: %w", archive, e.ID, err)
	}
	defer tx.Rollback()
	var res sql.Result
	if archive {
		res, err = tx.Exec(`UPDATE environment SET archived_at=?, revision=?, updated_at=? WHERE id=? AND archived_at IS NULL`,
			now, next, now, e.ID)
	} else {
		res, err = tx.Exec(`UPDATE environment SET archived_at=NULL, revision=?, updated_at=? WHERE id=? AND archived_at IS NOT NULL`,
			next, now, e.ID)
	}
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: set archived_at on %s: %w", e.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Environment{}, nil, fmt.Errorf("store: count archived_at on %s: %w", e.ID, err)
	}
	if n == 0 {
		return e, nil, nil // a concurrent identical write already settled it
	}
	action := "restore"
	if archive {
		action = "archive"
	}
	if err := appendEnvironmentChange(tx, a, now, "environment", e.ID, action, &next, &e.Spec,
		fmt.Sprintf("environment %s of %s · revision %d · by %s", action, e.ID, next, a.ID)); err != nil {
		return Environment{}, nil, fmt.Errorf("store: audit %s environment %s: %w", action, e.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return Environment{}, nil, fmt.Errorf("store: %s environment %s: %w", action, e.ID, err)
	}
	e.Revision, e.UpdatedAt = next, now
	if archive {
		at := now
		e.ArchivedAt = &at
	} else {
		e.ArchivedAt = nil
	}
	return e, nil, nil
}

// BindEnvironment applies one binding (EV18's environment.bind). Re-binding a
// scope to the environment it already names is a no-op — no row change, no
// audit row (P3). Everything is checked before any row is touched: the
// environment must exist, must not be archived, and every secret reference its
// spec names must exist and be unarchived (EV7's "a check, not a read", at
// bind time exactly as the design demands).
func (s *Store) BindEnvironment(a Actor, in NewBinding) (Binding, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return Binding{}, ref, nil
	}
	if in.ScopeKind != "workspace" && in.ScopeKind != "component" {
		return Binding{}, environmentRefusal("scope-kind-unknown",
			"scope kind %q is not one of workspace, component (EV2)", in.ScopeKind), nil
	}
	if (in.ScopeKind == "workspace") != (in.ComponentID == "") {
		return Binding{}, environmentRefusal("scope-shape",
			"scope %q %s a component id — the pair is the binding's identity (EV2)",
			in.ScopeKind, map[bool]string{true: "carries no", false: "needs"}[in.ScopeKind == "workspace"]), nil
	}

	targetID := in.ComponentID
	if in.ScopeKind == "component" {
		if err := s.componentBindable(in.ComponentID); err != nil {
			return Binding{}, err, nil
		}
	}

	e, err := s.GetEnvironment(in.EnvironmentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Binding{}, environmentRefusal("environment-unknown",
				"%s is not an environment (id or slug) — bind nothing that does not exist (EV2)", in.EnvironmentID), nil
		}
		return Binding{}, nil, err
	}
	if e.Archived() {
		return Binding{}, environmentRefusal("environment-archived",
			"%s is archived — bind an environment that is live, or restore it first", e.Slug), nil
	}
	// EV7: every name the spec references must exist and be unarchived. This
	// is a CHECK, not a read: the store asks for names and existence only, and
	// no value is anywhere in reach.
	refs, err := s.EnvironmentSecretRefs(e.ID)
	if err != nil {
		return Binding{}, nil, err
	}
	for _, name := range refs {
		sr, err := s.GetSecretRef(name)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return Binding{}, environmentRefusal("secret-ref-unknown",
					"%s names the secret reference %q, which does not exist — create it, or narrow the environment (EV7)",
					e.Slug, name), nil
			}
			return Binding{}, nil, err
		}
		if sr.Archived() {
			return Binding{}, environmentRefusal("secret-ref-archived",
				"%s names the secret reference %q, which is archived — restore it, or narrow the environment (EV7)",
				e.Slug, name), nil
		}
	}

	if existing, ok, err := s.bindingRow(in.ScopeKind, targetID); err != nil {
		return Binding{}, nil, err
	} else if ok && existing.EnvironmentID == e.ID {
		return existing, nil, nil // no-op: same binding, no row change, no audit (P3)
	}

	now := s.Now()
	b := Binding{ScopeKind: in.ScopeKind, EnvironmentID: e.ID, BoundAt: now}
	if in.ScopeKind == "component" {
		id := targetID
		b.ComponentID = &id
	}
	if in.DryRun {
		// Nothing was written, and nothing will be: a dry-run is not a
		// changing write, so it takes no audit row (P3).
		return b, nil, nil
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Binding{}, nil, fmt.Errorf("store: bind %s: %w", in.ScopeKind, err)
	}
	defer tx.Rollback()
	var res sql.Result
	if in.ScopeKind == "workspace" {
		// The partial unique index would refuse a second workspace row; the
		// UPSERT makes "one workspace binding" a single statement rather than
		// a delete-then-insert that could briefly leave the workspace with none.
		res, err = tx.Exec(`INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
			VALUES ('workspace', NULL, ?, ?)
			ON CONFLICT (scope_kind) WHERE scope_kind = 'workspace' DO UPDATE SET environment_id=excluded.environment_id, bound_at=excluded.bound_at`,
			e.ID, now)
	} else {
		res, err = tx.Exec(`INSERT INTO environment_binding (scope_kind, component_id, environment_id, bound_at)
			VALUES ('component', ?, ?, ?)
			ON CONFLICT (component_id) WHERE component_id IS NOT NULL DO UPDATE SET environment_id=excluded.environment_id, bound_at=excluded.bound_at`,
			targetID, e.ID, now)
	}
	if err != nil {
		return Binding{}, nil, fmt.Errorf("store: bind %s %s: %w", in.ScopeKind, targetID, err)
	}
	if _, err := res.RowsAffected(); err != nil {
		return Binding{}, nil, fmt.Errorf("store: bind %s %s: %w", in.ScopeKind, targetID, err)
	}
	subjectID := "workspace"
	if in.ScopeKind == "component" {
		subjectID = targetID
	}
	if err := appendEnvironmentChange(tx, a, now, "binding", subjectID, "bind", nil, nil,
		fmt.Sprintf("binding %s %s to %s (backend %s) · by %s", in.ScopeKind, subjectID, e.ID, e.Backend, a.ID)); err != nil {
		return Binding{}, nil, fmt.Errorf("store: audit bind %s %s: %w", in.ScopeKind, targetID, err)
	}
	if err := tx.Commit(); err != nil {
		return Binding{}, nil, fmt.Errorf("store: bind %s %s: %w", in.ScopeKind, targetID, err)
	}
	return b, nil, nil
}

// UnbindEnvironment removes a component's binding (EV18's environment.unbind
// {componentId}). The WORKSPACE binding cannot be removed, at any door, by any
// actor: EV2 has no code fallback, so a workspace with no binding means every
// command in it is refused (`environment-unbound`). The replacement is a bind
// to another environment, which is what the refusal says.
func (s *Store) UnbindEnvironment(a Actor, componentID string) (bool, *EnvironmentRefusal, error) {
	if ref := environmentActorRefusal(a); ref != nil {
		return false, ref, nil
	}
	if componentID == "" {
		return false, environmentRefusal("workspace-binding-permanent",
			"the workspace binding cannot be unbound — bind the workspace to another environment instead (EV2: with no binding there is no fallback, and every command is refused)"), nil
	}
	existing, ok, err := s.bindingRow("component", componentID)
	if err != nil {
		return false, nil, err
	}
	if !ok {
		return false, nil, nil // no-op: nothing to remove, so no audit row (P3)
	}

	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return false, nil, fmt.Errorf("store: unbind component %s: %w", componentID, err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM environment_binding WHERE scope_kind = 'component' AND component_id = ?`, componentID)
	if err != nil {
		return false, nil, fmt.Errorf("store: unbind component %s: %w", componentID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, nil, fmt.Errorf("store: count unbind of %s: %w", componentID, err)
	}
	if n == 0 {
		return false, nil, nil // a concurrent unbind settled it
	}
	if err := appendEnvironmentChange(tx, a, now, "binding", componentID, "unbind", nil, nil,
		fmt.Sprintf("binding component %s unbound (was %s) · by %s", componentID, existing.EnvironmentID, a.ID)); err != nil {
		return false, nil, fmt.Errorf("store: audit unbind component %s: %w", componentID, err)
	}
	if err := tx.Commit(); err != nil {
		return false, nil, fmt.Errorf("store: unbind component %s: %w", componentID, err)
	}
	return true, nil, nil
}

// --- reads -------------------------------------------------------------------

// GetEnvironment reads one environment by id or by slug (EV18's
// environment.show {id|slug}). The two never collide: ids carry the "env-"
// prefix and a slug may not start with a digit-only hex that matches one.
func (s *Store) GetEnvironment(idOrSlug string) (Environment, error) {
	var e Environment
	var archived sql.NullString
	err := s.r.QueryRow(`SELECT id, slug, name, description, backend, spec, origin, legacy, revision, created_at, updated_at, archived_at
		FROM environment WHERE id = ? OR slug = ?`, idOrSlug, idOrSlug).
		Scan(&e.ID, &e.Slug, &e.Name, &e.Description, &e.Backend, &e.Spec, &e.Origin,
			&e.Legacy, &e.Revision, &e.CreatedAt, &e.UpdatedAt, &archived)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Environment{}, fmt.Errorf("store: environment %s: %w", idOrSlug, ErrNotFound)
		}
		return Environment{}, fmt.Errorf("store: read environment %s: %w", idOrSlug, err)
	}
	e.ArchivedAt = nullStringPtr(archived)
	return e, nil
}

// ListEnvironments is the listing read (EV18's environment.list). A nil slice
// and an empty slice are the same answer here — there is no third state to
// lose, the table either has matching rows or it does not.
func (s *Store) ListEnvironments(f EnvironmentFilter) ([]Environment, error) {
	if f.Backend != "" && !environmentBackends[f.Backend] {
		return nil, fmt.Errorf("store: list environments: backend %q is not a backend (EV3)", f.Backend)
	}
	if f.Origin != "" && !environmentOrigins[f.Origin] {
		return nil, fmt.Errorf("store: list environments: origin %q is not system or user", f.Origin)
	}
	var conds []string
	var args []any
	if f.Backend != "" {
		conds = append(conds, "backend = ?")
		args = append(args, f.Backend)
	}
	if f.Origin != "" {
		conds = append(conds, "origin = ?")
		args = append(args, f.Origin)
	}
	if !f.IncludeArchived {
		conds = append(conds, "archived_at IS NULL")
	}
	query := `SELECT id, slug, name, description, backend, spec, origin, legacy, revision, created_at, updated_at, archived_at
		FROM environment`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY legacy DESC, slug ASC" // the row a person should notice first sorts first
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list environments: %w", err)
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var (
			e        Environment
			archived sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Description, &e.Backend, &e.Spec, &e.Origin,
			&e.Legacy, &e.Revision, &e.CreatedAt, &e.UpdatedAt, &archived); err != nil {
			return nil, fmt.Errorf("store: scan environment: %w", err)
		}
		e.ArchivedAt = nullStringPtr(archived)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list environments: %w", err)
	}
	return out, nil
}

// bindingRow reads one binding. The two scopes are one table with a shape
// CHECK, so the read is one statement with the component id absent for the
// workspace row — and `ok` says whether there was one, because "no binding" is
// a state EV2 needs distinct from a read that failed.
func (s *Store) bindingRow(scopeKind, componentID string) (Binding, bool, error) {
	var (
		b     Binding
		comp  sql.NullString
		query = `SELECT scope_kind, component_id, environment_id, bound_at FROM environment_binding WHERE scope_kind = 'workspace' AND component_id IS NULL`
		args  []any
	)
	if scopeKind == "component" {
		query = `SELECT scope_kind, component_id, environment_id, bound_at FROM environment_binding WHERE scope_kind = 'component' AND component_id = ?`
		args = append(args, componentID)
	}
	err := s.r.QueryRow(query, args...).Scan(&b.ScopeKind, &comp, &b.EnvironmentID, &b.BoundAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, false, nil
	}
	if err != nil {
		return Binding{}, false, fmt.Errorf("store: read %s binding: %w", scopeKind, err)
	}
	if comp.Valid {
		id := comp.String
		b.ComponentID = &id
	}
	return b, true, nil
}

// bindingScopesFor names the scopes an environment is bound at, for the
// archive refusal's sentence (EV2: a bound-but-archived environment leaves its
// commands with nothing to run in). It answers with what a person would type,
// not with row internals — "workspace" and "component <id>" are the two shapes
// a binding has, and a refusal that names them is a refusal they can act on.
func (s *Store) bindingScopesFor(environmentID string) ([]string, error) {
	rows, err := s.r.Query(`SELECT scope_kind, component_id FROM environment_binding
		WHERE environment_id = ? ORDER BY CASE scope_kind WHEN 'workspace' THEN 0 ELSE 1 END, component_id`, environmentID)
	if err != nil {
		return nil, fmt.Errorf("store: read bindings of %s: %w", environmentID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			kind string
			comp sql.NullString
		)
		if err := rows.Scan(&kind, &comp); err != nil {
			return nil, fmt.Errorf("store: scan binding of %s: %w", environmentID, err)
		}
		if kind == "workspace" {
			out = append(out, "workspace")
			continue
		}
		out = append(out, "component "+comp.String)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read bindings of %s: %w", environmentID, err)
	}
	return out, nil
}

// ListBindings is the panel's map of what is bound where (EV18's
// environment.bindings). Ordered so the workspace row — the one that decides
// everything else — comes first.
func (s *Store) ListBindings() ([]Binding, error) {
	rows, err := s.r.Query(`SELECT scope_kind, component_id, environment_id, bound_at FROM environment_binding
		ORDER BY CASE scope_kind WHEN 'workspace' THEN 0 ELSE 1 END, component_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list bindings: %w", err)
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		var (
			b    Binding
			comp sql.NullString
		)
		if err := rows.Scan(&b.ScopeKind, &comp, &b.EnvironmentID, &b.BoundAt); err != nil {
			return nil, fmt.Errorf("store: scan binding: %w", err)
		}
		if comp.Valid {
			id := comp.String
			b.ComponentID = &id
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list bindings: %w", err)
	}
	return out, nil
}

// BindingFor resolves EV2's order in SQL-shaped Go: the component's own
// binding, else its root component's, else the workspace's. `source` names
// which of the three answered, because a step record that says "host-scrubbed"
// without saying WHERE it came from cannot be debugged when the wrong one wins.
//
// The order lives here and nowhere else: `execenv.Resolve` applies the rule
// over three values it is handed, and this is the code that hands them over.
// A second copy of the order is a second answer.
func (s *Store) BindingFor(componentID string) (Binding, string, error) {
	if componentID != "" {
		if b, ok, err := s.bindingRow("component", componentID); err != nil {
			return Binding{}, "", err
		} else if ok {
			return b, "component", nil
		}
		root, err := s.RootOf(componentID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Binding{}, "", err
		}
		if root != "" && root != componentID {
			if b, ok, err := s.bindingRow("component", root); err != nil {
				return Binding{}, "", err
			} else if ok {
				return b, "root", nil
			}
		}
	}
	if b, ok, err := s.bindingRow("workspace", ""); err != nil {
		return Binding{}, "", err
	} else if ok {
		return b, "workspace", nil
	}
	// No binding anywhere is EV2's `environment-unbound`, and it is NOT an
	// error: the refusal is a decision, and the door that turns it into a
	// three-part sentence is execenv's, not this file's.
	return Binding{}, "", nil
}

// componentBindable is the existence check a component binding needs. It reads
// the table directly rather than through ComponentWriteFor because that read
// carries write-shaped fields this door has no business loading.
func (s *Store) componentBindable(componentID string) *EnvironmentRefusal {
	var one int
	err := s.r.QueryRow(`SELECT 1 FROM component WHERE id = ?`, componentID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return environmentRefusal("component-unknown",
			"component %s does not exist — bind a component that exists (EV2)", componentID)
	}
	if err != nil {
		return nil // falls through to the caller's error path below
	}
	return nil
}

// EnvironmentSecretRefs answers "which secret reference NAMES does this
// environment's spec mention?" — and only that (EV7). The spec is a WHERE,
// never a WHAT: these are names a caller then resolves through
// GetSecretRef's own door, and nothing in this function could return a value
// even if asked to.
//
// It is here rather than in execenv because the spec is a stored column and
// reading a column is the store's job (I2). It returns sorted, deduplicated
// names, so a caller comparing two environments compares sets.
func (s *Store) EnvironmentSecretRefs(idOrSlug string) ([]string, error) {
	e, err := s.GetEnvironment(idOrSlug)
	if err != nil {
		return nil, err
	}
	return secretRefNamesInSpec(e.Spec)
}
