package verifyseed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoStore is the adapter's refusal until the daemon installs the store
// (the blueprintseed/toolseed precedent: the package-global registration
// carries the refusing Adapter; the store-backed applier is the daemon's
// function-mount in NewSeedEngine).
var errNoStore = errors.New("verifyseed: the store is not installed — the daemon installs it at serve (S57 T10)")

// Applier writes the set through the store's ONE verify_template write
// path (T10 added VerifyTemplateUpsertTx/RowTx/RetireTx beside T3's
// apply — VG21: the seed writes system rows and nothing else).
type Applier struct {
	s *store.Store
}

// NewApplier binds the applier to the daemon's store.
func NewApplier(s *store.Store) *Applier { return &Applier{s: s} }

func (a *Applier) Set() string { return SetID }

// GuardAffecting is false: a verify_template row is a candidate a person
// copies — applying the set changes nothing about what any loop may run
// (VG21's inertness argument, registry rule 6).
func (a *Applier) GuardAffecting() bool { return false }

// Operator: a person may REMOVE a shipped template (its seed retirement
// is the honest removal door); they may not EDIT a system row in place —
// the copy-and-edit-the-copy door is `verify.applyTemplate` +
// `verify.update` on the copy, so an operator edit never diverges a
// system row (the inversion of the blueprint's rights, VG21's ruling).
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: false, Remove: true}
}

// Fingerprint is the canonical content hash of the live row — Source
// marshals Content, so the fingerprint rebuilds the same object from the
// row's columns and hashes it: applied==live exactly when the row holds
// the record, and a hand edit of a content column reads as a diverge.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	if a.s == nil {
		return "", false, errNoStore
	}
	row, present, err := a.s.VerifyTemplateRowTx(ctx, tx, key)
	if err != nil || !present {
		return "", false, err
	}
	b, err := seed.Canonical(contentFromRow(row.Stack, row.Name, row.Command, row.Requirement, row.TimeoutS))
	if err != nil {
		return "", false, fmt.Errorf("verifyseed: fingerprinting %q: %w", key, err)
	}
	return seed.Hash(b), true, nil
}

// Adoptable is false, always (the toolseed argument): there is no stub
// shape for a template row — no earlier slice wrote verify_template rows
// (T3 shipped only the read and the apply copy), so a live row with
// different bytes is either an unrecorded apply or a hand edit, and both
// are conflicts the engine reports, never rows to fill in place.
func (a *Applier) Adoptable(_ context.Context, _ *sql.Tx, _ string) (bool, error) {
	return false, nil
}

// Upsert writes one system row. Version is published into the TEXT column
// as "vN" (the same publication the golden records as an integer; the
// column's CHECK-free TEXT shape is T3's, the wire view is a label).
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, _ bool) error {
	if a.s == nil {
		return errNoStore
	}
	c, err := ContentOf(r)
	if err != nil {
		return err
	}
	return store.VerifyTemplateUpsertTx(tx, store.VerifyTemplate{
		ID:          r.Key,
		Stack:       c.Stack,
		Name:        c.Name,
		Command:     c.Command,
		Requirement: c.Requirement,
		TimeoutS:    c.TimeoutS,
		Version:     fmt.Sprintf("v%d", r.Version),
	})
}

// Retire archives the row (VG21: removal never resurrects — the row
// stands so gates applied from it keep naming their template, and the
// picker stops offering it). It never deletes.
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	if a.s == nil {
		return errNoStore
	}
	_, err := a.s.VerifyTemplateRetireTx(ctx, tx, key)
	return err
}

// Adapter is the seed.Applier the package-global registry holds from
// init: it refuses until the daemon mounts the store-backed applier
// (NewSeedEngine's function-mount — a domain may not widen the global
// from a distance).
type Adapter struct{}

func (Adapter) Set() string                   { return SetID }
func (Adapter) GuardAffecting() bool          { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: false, Remove: true} }

func (Adapter) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, errNoStore
}
func (Adapter) Adoptable(seed.Tx, string) (bool, error) { return false, errNoStore }
func (Adapter) Upsert(seed.Tx, seed.Record, bool) error { return errNoStore }
func (Adapter) Retire(seed.Tx, string) error            { return errNoStore }
