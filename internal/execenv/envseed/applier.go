package envseed

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// Applier is the standard environment set's applier. It holds the store — the
// one place SQL lives (I2) — and writes through the store's seed path on the
// engine's shared transaction. It is inert (EV19): it creates a row that does
// not exist and binds the workspace only when nothing is bound there, and it
// holds no cache, no goroutine and no opinion about a row that already exists.
//
// As with watchdogseed and agentseed, the `seed.Applier` seam takes a
// `seed.Tx` while the store's methods take a `*sql.Tx`, so this type is not
// itself a `seed.Applier` — the daemon adapts (internal/daemon/seed.go) and the
// set registers against the adapter in decl.go.
type Applier struct {
	store *store.Store
}

// NewApplier builds the applier over a store: the one door the seeded row's SQL
// has, so no SQL lives outside internal/store (that package's own guard).
func NewApplier(s *store.Store) *Applier { return &Applier{store: s} }

// Set names the set this applier serves — the one id the registry and the set
// agree on (Register refuses a mismatch).
func (a *Applier) Set() string { return SetID }

// GuardAffecting is false, and it is the assertion EV19 rests on: this set
// applies at daemon start with no human in the loop, which is only legitimate
// because what it writes cannot widen anything. A row that did not exist gains
// a name; a workspace with no binding gains the shipped default. Neither
// grants a command anything a binding did not already decide — and once a
// person has bound the workspace, this set writes nothing there at all.
func (a *Applier) GuardAffecting() bool { return false }

// Operator is the set's rights over its own rows: it may edit and retire. A
// seed is never a permission to do more, and "edit" here means the `seed
// .restore` path re-applying the code's version — never a start-up repair of a
// row a person changed.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: true, Remove: true}
}

// Fingerprint reads the live row and hashes it in the same canonical form the
// source wrote, so the engine's divergence check (live hash ≠ ledger hash) is a
// content comparison. An absent row is `(false)`, which is what makes the
// engine's plan write it; an ARCHIVED row is returned as it stands, so a
// person who archived the system environment reads as diverged — a gap in the
// table would hide the one fact the ledger exists to report.
//
// `BindWorkspace` is the set's intent rather than a column, so it is taken from
// the declared record and overlaid: the comparison stays "what the set says
// against what the row is", and a column the row does not have cannot drift
// into disagreement with itself.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	declared, err := declaredContent(key)
	if err != nil {
		return "", false, err
	}
	row, present, err := a.store.EnvironmentRowTx(tx, declared.Slug)
	if err != nil || !present {
		return "", present, err
	}
	live := content{
		Slug: row.Slug, Name: row.Name, Description: row.Description,
		Backend: row.Backend, Spec: row.Spec, Origin: row.Origin,
		BindWorkspace: declared.BindWorkspace,
	}
	b, err := seed.Canonical(live)
	if err != nil {
		return "", false, fmt.Errorf("envseed: the live row %s cannot be canonicalised: %w", key, err)
	}
	return seed.Hash(b), true, nil
}

// Adoptable is always false: this set's record is complete in one write, so
// there is no stub form to adopt in place. A present row is either the set's
// own (applied) or a person's (diverged), and neither is adoptable.
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	return false, nil
}

// Upsert writes the record's row, then applies the first-binding rule, both on
// the engine's transaction — one transaction is what makes them one fact: a
// workspace bound to an environment whose row did not commit is a binding to
// nothing, and EV2 would answer every command in the workspace with a refusal
// that names a missing environment.
//
// The engine decides WHETHER this is called (the startup path calls it only for
// a key with no row; the explicit `seed.restore` path calls it to re-apply over
// a removable one). Both cases land in the store's seed path, which is
// idempotent and never touches an existing row, so neither call can overwrite a
// person's edit — and the binding half refuses to move a binding that exists.
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	c, err := decode(r.Content)
	if err != nil {
		return err
	}
	row, _, err := a.store.SeedEnvironmentTx(tx, store.SeedEnvironment{
		Slug: c.Slug, Name: c.Name, Description: c.Description, Backend: c.Backend, Spec: c.Spec,
	})
	if err != nil {
		return err
	}
	if !c.BindWorkspace {
		return nil
	}
	if _, err := a.store.SeedWorkspaceBindingTx(tx, row.ID); err != nil {
		return err
	}
	return nil
}

// Retire is a no-op: a retired record's row is kept, never deleted (P3). An
// environment row is a thing a binding may still name, and the audit trail
// (`environment_change`) is the record of the retirement — deleting the row
// would leave an audit entry describing something that never existed.
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	return nil
}

// declaredContent finds the set's own record for a key, so the fingerprint and
// the applier read the same declared intent the registry has. A key the set
// does not declare is an error and not an empty content: hashing "nothing"
// against "nothing" reports a divergence-free row that was never compared to
// anything (P3's third state, missed).
func declaredContent(key string) (content, error) {
	for _, rec := range Source() {
		if rec.Key == key {
			return decode(rec.Content)
		}
	}
	c, _ := contentForKey(key)
	return content{}, fmt.Errorf("envseed: the standard set declares no record keyed %q (its slugs are the keys; this one is %q)", key, c.Slug)
}
