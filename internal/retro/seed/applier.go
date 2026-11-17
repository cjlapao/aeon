package retroseed

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoStore is the adapter's refusal until the daemon installs the
// store-backed applier (the verifyseed/toolseed precedent: the global
// registration carries the refusing Adapter; the store-backed applier is
// NewSeedEngine's function-mount, and the daemon's wrapper unwraps
// seed.Tx to the *sql.Tx every method here takes).
var errNoStore = errors.New("retroseed: the store is not installed — the daemon mounts it at NewSeedEngine")

// Applier writes the set through the store's retro_template doors. T1
// shipped UpsertRetroTemplate; T3 added the Tx twins so the row and its
// ledger row commit together (SD3).
type Applier struct {
	s *store.Store
}

// NewApplier binds the applier to the daemon's store.
func NewApplier(s *store.Store) *Applier { return &Applier{s: s} }

func (a *Applier) Set() string { return SetID }

// GuardAffecting is false: a template row changes nothing a loop may do
// — applying the set lays bodies for a renderer, and the renderer writes
// learnings only when a terminal fact row lands on a milestone. Registry
// rule 6 is what lets the set be inert at all.
func (a *Applier) GuardAffecting() bool { return false }

// Operator: neither EDIT nor REMOVE (T3's ruling, unlike verify.templates
// whose person-facing picker offers copies): a retro template has no
// human edit door — no socket method copies or edits one, the body is
// this slice's shipped text — and removal has no door either: the
// renderer READS the row at write time, and a missing row is a silently
// missing learning, which is exactly what Remove:false keeps from
// happening by hand.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: false, Remove: false}
}

// Fingerprint rebuilds the canonical object from the live row's body
// column, so applied==live exactly when the row holds the shipped text
// and any hand edit of the body reads as a diverge (the version column is
// metadata, outside the hash — the verifyseed convention).
func (a *Applier) Fingerprint(tx *sql.Tx, key string) (string, bool, error) {
	if a.s == nil {
		return "", false, errNoStore
	}
	row, present, err := a.s.GetRetroTemplateTx(tx, key)
	if err != nil || !present {
		return "", false, err
	}
	b, err := seed.Canonical(Content{Template: row.Template})
	if err != nil {
		return "", false, fmt.Errorf("retroseed: fingerprinting %q: %w", key, err)
	}
	return seed.Hash(b), true, nil
}

// Adoptable is false, always: no earlier slice wrote retro_template rows
// (T1 laid the table), so a present row without a ledger row is a hand
// write — a conflict the engine reports, never a stub to fill.
func (a *Applier) Adoptable(_ *sql.Tx, _ string) (bool, error) { return false, nil }

// Upsert lays one system row inside the engine's transaction.
func (a *Applier) Upsert(tx *sql.Tx, r seed.Record, _ bool) error {
	if a.s == nil {
		return errNoStore
	}
	c, err := ContentOf(r)
	if err != nil {
		return err
	}
	return a.s.UpsertRetroTemplateTx(tx, r.Key, r.Version, c.Template, "system")
}

// Retire refuses: the set ships five bodies and retires none, the table
// carries no retired state, and with Remove:false the engine never has a
// door to call this. The refusal is the proof — a call is a bug, and it
// names itself rather than silently half-doing a removal this domain
// refused to have.
func (a *Applier) Retire(_ *sql.Tx, key string) error {
	return fmt.Errorf("retroseed: %q has no retire door (Remove:false — no operator removal, no shipped retirement)", key)
}

// Adapter is the seed.Applier the package-global registry holds from
// init: it refuses until the daemon mounts the store-backed applier.
type Adapter struct{}

func (Adapter) Set() string                   { return SetID }
func (Adapter) GuardAffecting() bool          { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: false, Remove: false} }

func (Adapter) Fingerprint(seed.Tx, string) (string, bool, error) { return "", false, errNoStore }
func (Adapter) Adoptable(seed.Tx, string) (bool, error)           { return false, errNoStore }
func (Adapter) Upsert(seed.Tx, seed.Record, bool) error           { return errNoStore }
func (Adapter) Retire(seed.Tx, string) error                      { return errNoStore }
