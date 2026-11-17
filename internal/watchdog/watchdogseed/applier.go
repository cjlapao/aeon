package watchdogseed

import (
	"context"
	"database/sql"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// seedActorKind and seedActorID stamp the kind-'created' audit row a seeded
// rule earns (watchdog_event.actor_kind admits user|cli|system; the seed is
// the system's act, and 'startup' is the id the engine's actor carries when
// it applies inert sets at start).
const (
	seedActorKind = "system"
	seedActorID   = "startup"
)

// Applier is the watchdog set's applier. It holds the store (the one place
// the SQL lives, I2) and writes a rule row through the store's one write
// path on the shared transaction (SD3). It is inert (LC18): a rule never
// widens a guard, so the class gate is a non-event. It holds no cache and no
// goroutine.
//
// As with agentseed, the seed.Applier seam takes a seed.Tx but the store's
// methods take *sql.Tx, so this type is not itself a seed.Applier — the
// daemon adapts (internal/daemon/seed.go), and the set registers against
// that adapter's init-time stub (decl.go).
type Applier struct {
	store *store.Store
}

// NewApplier builds the applier over a store — the one door the rule row's
// SQL has, so no SQL lives outside internal/store (the store's guard).
func NewApplier(s *store.Store) *Applier { return &Applier{store: s} }

// Set names the set this applier serves — the one id the registry and the
// set agree on (Register refuses a mismatch).
func (a *Applier) Set() string { return SetID }

// GuardAffecting is false: the watchdog set is inert, so the applier never
// writes a guard (LC18) and the class gate is a non-event here.
func (a *Applier) GuardAffecting() bool { return false }

// Operator is the set's rights: it may edit and remove (retire) its own rows.
// A seed is never a permission to do more.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: true, Remove: true}
}

// Fingerprint reads the live rule row's content and hashes it — the same
// canonical form the source wrote, so the engine's divergence check
// (live.Hash != ledger.Hash) is a content comparison, not a column one
// (SD8). The live row's origin and enabled are part of the content: an
// operator who disables a system rule reads 'diverged', not 'applied', and a
// diverged or removed rule is never re-seeded (rule 6) — restore is
// explicit.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	c, err := contentForKey(key)
	if err != nil {
		return "", false, err
	}
	rule, present, err := a.store.FindWatchdogRuleTx(tx, c.Scope, c.SubjectID, c.Signal, c.Action, c.Threshold)
	if err != nil || !present {
		return "", present, err
	}
	live := content{
		Scope:     rule.Scope,
		SubjectID: rule.SubjectID,
		Signal:    rule.Signal,
		Threshold: rule.Threshold,
		Action:    rule.Action,
		Enabled:   rule.Enabled,
		Origin:    rule.Origin,
	}
	b, err := seed.Canonical(live)
	if err != nil {
		return "", false, err
	}
	return seed.Hash(b), true, nil
}

// Adoptable is always false: a watchdog rule has no stub form — the agents
// set's adoptable stub (an empty body the applier fills in place) has no
// twin here, because a rule is complete on one write. A present row is
// either the set's own (applied) or an operator's (diverged), and neither is
// adoptable (SD9).
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	return false, nil
}

// Upsert writes the record's rule, on the shared transaction. The engine's
// plan is what decides the write happens: the startup apply path only ever
// calls it for a key with NO row (a present row — the set's own or the
// operator's — is an OpSkip the plan already made, so an operator's choice
// outlives the set, SD6), while the explicit seed.restore path calls it for a
// record in a removable state, to re-apply the code version OVER the live
// row. Both ride WatchdogRestoreTx, the applier's one door: a create when
// the key has no row (the set's own birth, through WatchdogSetTx and its
// kind-'created' audit row), a no-op when the row matches the record, and a
// restore of enabled and origin when it does not (a removal at the store is
// a disabled row, never a deleted one — the row exists, diverged, and the
// restore is what brings it back).
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	c, err := decode(r.Content)
	if err != nil {
		return err
	}
	enabled := c.Enabled
	_, _, err = a.store.WatchdogRestoreTx(tx, store.WatchdogSetIn{
		Scope:     c.Scope,
		SubjectID: c.SubjectID,
		Signal:    c.Signal,
		Threshold: c.Threshold,
		Action:    c.Action,
		Enabled:   &enabled,
		Origin:    c.Origin,
		ActorKind: seedActorKind,
		ActorID:   seedActorID,
	})
	return err
}

// Retire is a no-op: a retired record's row is kept, never deleted (P3), and
// the row's kind-'removed' audit row — written when the operator removed it
// — is already the retirement's record (SD10).
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	return nil
}
