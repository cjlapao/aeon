package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/errs"
)

// settings_store.go — the accessor. config.Store composes T2's closed registry
// (CS3) with a Repo seam (CS2: no SQL here), the process environment and a
// clock, and offers Get/List/Set/Unset/History plus the read-only Reader a
// consumer takes (CS11). It holds no SQL, no cache, no goroutine and no timer
// (CS7): a read is one indexed row read plus one env lookup, every time.
//
// The write boundary is the ONE place a value changes (CS8): Set/Unset run
// their refusals in order before the next step, a no-op writes nothing, and a
// changing write appends exactly one audit event in the same transaction.
// A read never turns a repo error into a default (P31): "we could not read it"
// and "it is unset" are different facts, and a Sensitive guard must not read
// as "unset → default" while the real stored value is the permissive one.

// Repo is everything config.Store needs from storage. It holds no domain rule
// (CS2): internal/store implements the SQL half (T1) and the daemon bridges
// it (T4). A repo read error is a fault the caller must see, not a default.
type Repo interface {
	SettingGet(ctx context.Context, key string) (RepoRow, bool, error)
	SettingList(ctx context.Context) ([]RepoRow, error)
	// SettingPut upserts the row and appends ev in ONE transaction; the repo
	// fills ev.OldValue from the row it replaced (a no-op writes neither).
	SettingPut(ctx context.Context, row RepoRow, ev RepoEvent) error
	SettingDelete(ctx context.Context, key string, ev RepoEvent) (existed bool, err error)
	SettingEventList(ctx context.Context, key string, limit int) ([]RepoEvent, error)
}

// RepoRow is one stored setting row as the Repo sees it: the canonical value
// the table holds, plus the audit stamp. Value is canonical text (CS3).
type RepoRow struct {
	Key       string
	Value     string
	UpdatedAt string
	UpdatedBy string
}

// RepoEvent is one audit entry as the Repo sees it (CS8's append-only audit).
// OldValue/NewValue are pointers: nil means "there was none" (a first write's
// old, an unset's new). The Repo fills OldValue from the row it replaced/deleted.
type RepoEvent struct {
	ID         int64
	Key        string
	Op         string
	OldValue   *string
	NewValue   *string
	Sensitive  bool
	ShadowedBy string
	Actor      string
	OccurredAt string
}

// Event is one audit entry as the Store exposes it (History). It is the same
// shape as the seam's RepoEvent; the alias keeps the Store's public surface
// distinct from the Repo's without a second copy of the fields.
type Event = RepoEvent

// SettingSource names WHICH layer an effective value came from (CS4). It is
// named SettingSource, not Source, because creds.go already exports a Source
// type (SourceEnv/SourceFile) for the credential rule; a second Source would
// collide, and a collision would edit or shadow the existing one.
type SettingSource string

const (
	// SettingDefault is the declared default: no env, no (valid) row.
	SettingDefault SettingSource = "default"
	// SettingDatabase is the stored row (when the environment is not in force).
	SettingDatabase SettingSource = "database"
	// SettingEnvironment is the environment override, which beats the database.
	SettingEnvironment SettingSource = "environment"
)

// nearestCount is how many declared keys an ErrSettingUnknown names in its Do.
const nearestCount = 5

// maxActorBytes is the write boundary's cap on the audit actor (CS8): an
// unattributed write is refused, and "unknown" is not an honest actor.
const maxActorBytes = 256

// Options wires a Store. Every one is nilable so a test injects the env map
// and a fixed clock and never touches the real environment or wall clock.
type Options struct {
	Registry  *Registry                    // nil → DefaultRegistry()
	LookupEnv func(string) (string, bool) // nil → os.LookupEnv
	Now       func() time.Time            // nil → time.Now().UTC
}

// Store is the accessor. Its fields are exactly repo, reg, lookupEnv, now
// (CS7's no-cache pin, asserted by reflection): no map, no mutex, no timestamp
// — a read is always one row read plus one env lookup, live every time.
type Store struct {
	repo      Repo
	reg       *Registry
	lookupEnv func(string) (string, bool)
	now       func() time.Time
}

// NewStore builds the accessor. It panics on a nil repo: a Store with nowhere
// to read is a wiring bug (DB19: a programmer error, not data), not a value to
// default. nil option fields fall back to the real environment / wall clock.
func NewStore(repo Repo, opt Options) *Store {
	if repo == nil {
		panic("config.NewStore: repo is nil — a Store with nowhere to read is a wiring bug, not a value")
	}
	reg := opt.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	lookupEnv := opt.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	now := opt.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{repo: repo, reg: reg, lookupEnv: lookupEnv, now: now}
}

// View is what a read returns (CS4, CS10). It carries the declaration
// (embedded), the EFFECTIVE value in canonical form, and the three facts a
// client needs to know which layer won (D3's "name the layer"): Source
// (which layer), Stored (what the table holds, or nil) and ShadowedBy (the env
// var name when the environment is in force and a row exists — "your saved
// value is being overridden").
type View struct {
	Setting // the declaration: Key, Area, Type, Default, Description, Sensitive, bounds, EnumValues

	Value         string        // EFFECTIVE value, canonical form
	Source        SettingSource // "default" | "database" | "environment"
	Stored        *string       // what the table holds, nil when no row
	ShadowedBy    string        // the env var name when the environment is in force AND a row exists
	EnvInvalid    string        // env var name whose value failed validation and was NOT used; "" otherwise
	StoredInvalid bool          // the row's value fails the CURRENT declaration; the effective value is then Default
	UpdatedAt     string        // from the row; "" when none
	UpdatedBy     string        // from the row; "" when none
}

// resolve is the one resolution algorithm every read and write result runs
// (T3.md, CS4): environment → database → default. It is the single place the
// precedence, the invalid-env and stored-invalid flags, and ShadowedBy are
// computed, so no read can disagree with a write's result. A repo read error
// is RETURNED, never defaulted (P31).
func (s *Store) resolve(ctx context.Context, key string) (View, error) {
	// 1. Look the key up; absent → ErrSettingUnknown (with Nearest). Absence is
	//    a refusal, not a default — a typo must not create a silent no-op.
	decl, ok := s.reg.Lookup(key)
	if !ok {
		return View{}, NewSettingUnknown(key, s.reg.Nearest(key, nearestCount))
	}
	v := View{Setting: decl}
	envName := EnvName(key)

	// Read the row first (P31: a fault is returned, never a default). It is
	// needed for the database layer AND for ShadowedBy when the env is in force.
	row, rowOK, err := s.repo.SettingGet(ctx, key)
	if err != nil {
		return View{}, err
	}
	if rowOK {
		stored := row.Value
		v.Stored = &stored
		v.UpdatedAt = row.UpdatedAt
		v.UpdatedBy = row.UpdatedBy
	}

	// 2. Environment, first (CS4). An env var that is SET is parsed, untrimmed;
	//    valid → in force, invalid → named and not used (fall through). An env
	//    set to "" is still "set": for bool/int/enum it fails validation and is
	//    reported (an exported-but-empty variable is a common mistake, loud not
	//    ignored); for a string "" is a valid value and is in force.
	if envVal, set := s.lookupEnv(envName); set {
		if canonical, perr := decl.ParseValue(envVal); perr == nil {
			v.Value = canonical
			v.Source = SettingEnvironment
			// 5. ShadowedBy: the env is in force AND a row exists.
			if v.Stored != nil {
				v.ShadowedBy = envName
			}
			return v, nil
		}
		v.EnvInvalid = envName // not used; fall through to the row / default
	}

	// 3/4. The row, then the default.
	if rowOK {
		if canonical, perr := decl.ParseValue(row.Value); perr == nil {
			v.Value = canonical
			v.Source = SettingDatabase
			return v, nil
		}
		// 4. The stored value fails the CURRENT declaration (a later tightening,
		//    CS10): effective value is the restrictive default, but the read is
		//    loud (StoredInvalid) and carries the raw text — never fatal.
		v.StoredInvalid = true
		v.Value = decl.FormatDefault()
		v.Source = SettingDefault
		return v, nil
	}

	// 3. No row: the declared default, unset is a normal answer (DB19).
	v.Value = decl.FormatDefault()
	v.Source = SettingDefault
	return v, nil
}

// Get is the read boundary: it returns the resolved View for a declared key.
// An undeclared key is ErrSettingUnknown; a repo fault is returned (P31).
func (s *Store) Get(ctx context.Context, key string) (View, error) {
	return s.resolve(ctx, key)
}

// List returns the declared keys (only — orphan repo rows are not listed),
// each resolved to its View, sorted by area then key, filtered by area ("" =
// all). An unknown area is an empty slice, not a refusal.
func (s *Store) List(ctx context.Context, area string) ([]View, error) {
	out := make([]View, 0)
	for _, decl := range s.reg.All() {
		if area != "" && decl.Area != area {
			continue
		}
		v, err := s.resolve(ctx, decl.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// checkActor is CS8's first refusal: a write needs an honest, attributed
// actor (the audit column is NOT NULL), non-empty and ≤ 256 bytes.
func checkActor(actor string) error {
	if actor == "" {
		return errors.New("actor must be non-empty: an unattributed write is refused — the audit actor column is NOT NULL and \"unknown\" is not an honest actor")
	}
	if len(actor) > maxActorBytes {
		return fmt.Errorf("actor must be at most %d bytes", maxActorBytes)
	}
	return nil
}

// envShadows reports whether the environment is in force for key per resolve
// step 2 (set and valid) — the audit's shadowed_by="env" signal.
func (s *Store) envShadows(key string, decl Setting) bool {
	envVal, set := s.lookupEnv(EnvName(key))
	if !set {
		return false
	}
	_, err := decl.ParseValue(envVal)
	return err == nil
}

// Set is the one write boundary (CS8). It runs its refusals in order, each
// before the next step, then writes: a no-op (value already stored) writes
// nothing and records no event; a changing write appends exactly one event in
// the row's transaction and returns the FRESH View — what is now in force, not
// what was just sent.
func (s *Store) Set(ctx context.Context, key, raw, actor string) (View, bool, error) {
	// 1. actor (before any repo call).
	if err := checkActor(actor); err != nil {
		return View{}, false, err
	}
	// 2. unknown key.
	decl, ok := s.reg.Lookup(key)
	if !ok {
		return View{}, false, NewSettingUnknown(key, s.reg.Nearest(key, nearestCount))
	}
	// 3. defensive credential-name re-check (CS5): the registry is a package
	//    global a test could mutate, so the write path does not trust it for
	//    the one rule C4 calls non-negotiable — even a declared credential-
	//    shaped name is refused here, before any repo call.
	if isCredentialName(key) {
		return View{}, false, NewSettingNotWritable(key, "a credential-shaped name never lives in the setting table (C4); route it through config.Credential")
	}
	// 4. value.
	canonical, err := decl.ParseValue(raw)
	if err != nil {
		return View{}, false, err
	}
	// 5. read the current row live (CS7). A no-op writes nothing, no event.
	row, rowOK, err := s.repo.SettingGet(ctx, key)
	if err != nil {
		return View{}, false, err
	}
	if rowOK && row.Value == canonical {
		v, err := s.resolve(ctx, key)
		if err != nil {
			return View{}, false, err
		}
		return v, false, nil
	}
	// 6. write, then re-resolve and return the fresh View (changed=true).
	shadowedBy := ""
	if s.envShadows(key, decl) {
		shadowedBy = "env"
	}
	occurred := s.now().Format(time.RFC3339)
	ev := RepoEvent{
		Key:        key,
		Op:         "set",
		NewValue:   &canonical,
		Sensitive:  decl.Sensitive,
		ShadowedBy: shadowedBy,
		Actor:      actor,
		OccurredAt: occurred,
	}
	if err := s.repo.SettingPut(ctx, RepoRow{Key: key, Value: canonical, UpdatedAt: occurred, UpdatedBy: actor}, ev); err != nil {
		return View{}, false, err
	}
	v, err := s.resolve(ctx, key)
	if err != nil {
		return View{}, false, err
	}
	return v, true, nil
}

// Unset mirrors Set: the same actor / key / credential refusals, then a delete
// in one transaction. A delete of a key with no row is changed=false, not an
// error; the event carries the old value the repo read.
func (s *Store) Unset(ctx context.Context, key, actor string) (View, bool, error) {
	if err := checkActor(actor); err != nil {
		return View{}, false, err
	}
	decl, ok := s.reg.Lookup(key)
	if !ok {
		return View{}, false, NewSettingUnknown(key, s.reg.Nearest(key, nearestCount))
	}
	if isCredentialName(key) {
		return View{}, false, NewSettingNotWritable(key, "a credential-shaped name never lives in the setting table (C4); route it through config.Credential")
	}
	shadowedBy := ""
	if s.envShadows(key, decl) {
		shadowedBy = "env"
	}
	occurred := s.now().Format(time.RFC3339)
	ev := RepoEvent{
		Key:        key,
		Op:         "unset",
		NewValue:   nil,
		Sensitive:  decl.Sensitive,
		ShadowedBy: shadowedBy,
		Actor:      actor,
		OccurredAt: occurred,
	}
	existed, err := s.repo.SettingDelete(ctx, key, ev)
	if err != nil {
		return View{}, false, err
	}
	if !existed {
		v, err := s.resolve(ctx, key)
		if err != nil {
			return View{}, false, err
		}
		return v, false, nil
	}
	v, err := s.resolve(ctx, key)
	if err != nil {
		return View{}, false, err
	}
	return v, true, nil
}

// History is the audit read (CS8): it does NOT look the key up in the registry
// (a key removed from the registry keeps its trail) and returns events newest
// first. limit defaults to 50 and clamps to 500; key "" is all events.
func (s *Store) History(ctx context.Context, key string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	evt, err := s.repo.SettingEventList(ctx, key, limit)
	if err != nil {
		return nil, err
	}
	return evt, nil
}

// Reader is the read-only surface a consumer takes (CS11): one *Store is
// handed to both doors, so a consumer reads through the same live accessor the
// daemon's protocol seam uses. It is read-only on purpose — a consumer never
// writes a setting, and a write that an agent could reach would hand it the
// means to change the rules written to constrain it.
type Reader interface {
	Bool(ctx context.Context, key string) (bool, error)
	Int(ctx context.Context, key string) (int, error)
	String(ctx context.Context, key string) (string, error)
	Enum(ctx context.Context, key string) (string, error)
}

// *Store implements Reader. Each helper is Get → type check → convert. A key
// declared with another type is a programmer error (not user data), so it
// returns the distinct ErrSettingWrongType; a StoredInvalid / EnvInvalid read
// still returns the effective (restrictive default) value and a nil error —
// the loud signal is in Get's View, not a consumer's hot path.
var _ Reader = (*Store)(nil)

// Bool reads a bool setting, refusing a key declared with another type.
func (s *Store) Bool(ctx context.Context, key string) (bool, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if v.Type != TypeBool {
		return false, NewErrSettingWrongType(key, v.Type, TypeBool)
	}
	return v.Value == "true", nil
}

// Int reads an int setting, refusing a key declared with another type.
func (s *Store) Int(ctx context.Context, key string) (int, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	if v.Type != TypeInt {
		return 0, NewErrSettingWrongType(key, v.Type, TypeInt)
	}
	n, err := strconv.ParseInt(v.Value, 10, 64)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// String reads a string setting, refusing a key declared with another type.
func (s *Store) String(ctx context.Context, key string) (string, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if v.Type != TypeString {
		return "", NewErrSettingWrongType(key, v.Type, TypeString)
	}
	return v.Value, nil
}

// Enum reads an enum setting, refusing a key declared with another type.
func (s *Store) Enum(ctx context.Context, key string) (string, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if v.Type != TypeEnum {
		return "", NewErrSettingWrongType(key, v.Type, TypeEnum)
	}
	return v.Value, nil
}

// --- the one error this file owns (T3.md: defined here, not in T2's
// settings_errors.go). It is a three-part domain refusal like the other two
// but for a programmer mistake, not user data. ---

// ErrSettingWrongType is the programmer-error refusal a Reader helper returns
// when a key is declared with a different type (CS11): Bool("test.count") is
// an error, not a coerced guess. It names the key and both types.
type ErrSettingWrongType struct {
	Key      string
	Declared SettingType
	Asked    SettingType
	Err      errs.Error
}

// NewErrSettingWrongType is the only constructor; it derives the three parts
// from the declared and asked types.
func NewErrSettingWrongType(key string, declared, asked SettingType) *ErrSettingWrongType {
	return &ErrSettingWrongType{
		Key:      key,
		Declared: declared,
		Asked:    asked,
		Err: errs.New(
			"setting "+quote(key)+" is declared as "+string(declared)+" but was read as "+string(asked),
			"reading a setting as a type it is not declared as is a programmer error, not a value error — a coerced guess is a silent wrong answer",
			"read "+quote(key)+" with the "+string(declared)+" helper",
		),
	}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrSettingWrongType) Error() string { return e.Err.Error() }
func (e *ErrSettingWrongType) What() string  { return e.Err.What() }
func (e *ErrSettingWrongType) Why() string   { return e.Err.Why() }
func (e *ErrSettingWrongType) Do() string    { return e.Err.Do() }
