package notifyseed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoStore is the adapter's refusal until the daemon installs the store:
// a set with no store is a named refusal, not a silent gap (the
// blueprintseed precedent).
var errNoStore = errors.New("notifyseed: the store is not installed — the daemon installs it at serve (S58 T4)")

// Applier is the domain's half (SD2, SD4, SD6): it reads and writes the
// notify_event_class rows for this set, always through the store's own
// doors — GetNotifyClassTx to read, UpsertNotifyClassTx to write (the
// same upsert T1 wrote for exactly this caller). The daemon adapts it to
// the seed.Applier the registry holds (the *sql.Tx finding blueprintseed
// records: the store's row methods need the real tx, and the engine's
// seam is unwrapped at the mount).
type Applier struct {
	store *store.Store
}

// NewApplier binds the set to a store.
func NewApplier(s *store.Store) *Applier { return &Applier{store: s} }

// Set is the set ID this applier serves.
func (a *Applier) Set() string { return SetID }

// GuardAffecting is false: a hint notifies, it permits nothing. That is
// what lets the set be inert at all (registry rule 6, SD5).
func (a *Applier) GuardAffecting() bool { return false }

// Operator (SV4): an operator may EDIT a system class (tone, templates
// within the closed set, the enabled flag — the edit diverges the row and
// a re-seed never overwrites it) and may REMOVE it by DISABLING it (the
// row stays; the class goes silent).
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: true, Remove: true}
}

// Fingerprint re-envelopes the live row's content columns into the
// record's canonical bytes and hashes them (the blueprintseed shape):
// applied == live exactly when the row still holds what the set shipped,
// and an operator edit — a different tone, a different title, a silenced
// enabled, an bumped version — re-envelops to different bytes and reads
// as a divergence the engine will not write over.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	if err := a.need(); err != nil {
		return "", false, err
	}
	kind, variant, ok := SplitKey(key)
	if !ok {
		return "", false, fmt.Errorf("notifyseed: record key %q is not a kind|variant pair", key)
	}
	row, present, err := a.store.GetNotifyClassTx(ctx, tx, kind, variant)
	if err != nil || !present {
		return "", false, err
	}
	body, err := json.Marshal(recordContent{
		Kind: row.Kind, Variant: row.Variant, Tone: row.Tone,
		TitleTemplate: row.TitleTemplate, BodyTemplate: row.BodyTemplate,
		Enabled: row.Enabled, Version: row.Version,
	})
	if err != nil {
		return "", false, fmt.Errorf("notifyseed: fingerprinting %q: %w", key, err)
	}
	return seed.Hash(body), true, nil
}

// Adoptable is false for every key (SD9's domain call): the class table
// has no stub story — rows arrive only from this set or from an operator
// authoring one by hand, and a hand-authored row with the same key is a
// CONFLICT the engine reports and skips, never silently fills.
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	if err := a.need(); err != nil {
		return false, err
	}
	_, _, err := a.Fingerprint(ctx, tx, key)
	return false, err
}

// Upsert writes the row on the caller's transaction (the engine decides
// WHEN: apply, update or restore — an untouched system row re-applies as
// an update in place; a diverged row is reached only through
// seed.restore). Origin is 'system' and the row's version is the
// record's: provenance and the engine's counter, both owned here, never
// by the human door's update path (which keeps them).
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	if err := a.need(); err != nil {
		return err
	}
	c, err := DecodeRecord(r)
	if err != nil {
		return err
	}
	return a.store.UpsertNotifyClassTx(tx, store.NotifyClass{
		Kind: c.Kind, Variant: c.Variant, Tone: c.Tone,
		TitleTemplate: c.TitleTemplate, BodyTemplate: c.BodyTemplate,
		Enabled: c.Enabled, Origin: "system", Version: r.Version,
	})
}

// Retire keeps the row and writes nothing (SD6's never-delete, the
// blueprint precedent): the class table has no retired column, the
// memory that the record left the code is the engine's ledger row
// (state retired) which seed.status shows — and a class for a kind no
// emitter produces can never fire anyway, so silence needs no flag.
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	if err := a.need(); err != nil {
		return err
	}
	_, _, err := a.Fingerprint(ctx, tx, key)
	return err
}

func (a *Applier) need() error {
	if a.store == nil {
		return errNoStore
	}
	return nil
}

// Adapter is the seed.Applier the package-global registry holds from
// init: it refuses until the daemon mounts the store-backed applier
// (NewSeedEngine's function-mount, the blueprint/policy precedent).
type Adapter struct{}

func (Adapter) Set() string                   { return SetID }
func (Adapter) GuardAffecting() bool          { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: true, Remove: true} }

func (Adapter) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, errNoStore
}
func (Adapter) Adoptable(seed.Tx, string) (bool, error) { return false, errNoStore }
func (Adapter) Upsert(seed.Tx, seed.Record, bool) error { return errNoStore }
func (Adapter) Retire(seed.Tx, string) error            { return errNoStore }
