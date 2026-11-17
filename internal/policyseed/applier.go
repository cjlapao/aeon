package policyseed

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// ErrConflict is the applier's refusal to overwrite what an operator authored:
// a policy row present with a provenance that is not 'system' is the
// operator's rule, and a seed re-writing it would be the machine editing the
// human's guards — the exact direction PL15/PM15 forbids. The engine skips a
// conflict before reaching Upsert (the plan already refused it); the refusal
// lives here so the misuse is impossible rather than merely unplanned.
var ErrConflict = fmt.Errorf("refused: a policy row is present and its origin is not system — an operator's rule is never overwritten by the seed")

// Applier is the policy.corpus set's applier. It holds two doors and no
// third: the store (the one place SQL lives, SD1) for reading a row's
// content columns, and the write BOUNDARY (policy.Store) for writing rows —
// Upsert calls AddInTx, the same ValidateForAdd and the same row builder the
// plain Add runs through, so this package contains no policy write of its
// own. It holds no cache and no goroutine.
//
// It is not a seed.Applier directly, for the reason agentseed records: the
// seed.Applier's methods take a seed.Tx, the store's methods take *sql.Tx,
// and the SQL must live in internal/store. The daemon adapts this applier to
// the seed.Applier seam by handing each call the transaction it opened; the
// set registers against that adapter.
type Applier struct {
	store  *store.Store
	bound  *policy.Store
	now    func() time.Time
	id     string                // the set this applier serves
	source func() []seed.Record // that set's declared records (Adoptable reads them)
}

// NewApplier builds the applier over the store and the write boundary, with
// the clock as the parameter it is everywhere in the policy package (D5).
func NewApplier(s *store.Store, b *policy.Store, now func() time.Time) *Applier {
	return &Applier{store: s, bound: b, now: now, id: SetID, source: Source}
}

// NewFirstPartyApplier builds the applier for the permissions.firstParty set: the same write boundary, the same
// rights, the same ledger discipline, a different declared source.
func NewFirstPartyApplier(s *store.Store, b *policy.Store, now func() time.Time) *Applier {
	return &Applier{store: s, bound: b, now: now, id: FirstPartySetID, source: FirstPartySource}
}

// Set names the set this applier serves (rules 6, 7).
func (a *Applier) Set() string { return a.id }

// GuardAffecting is TRUE, and is the set's whole story (SD5): applying this
// set writes `allow` rows — it widens what a running loop may do — so the
// registry (rule 6) refuses it inert, and the engine's class gate refuses it
// on the startup path. D4 stays true by construction.
func (a *Applier) GuardAffecting() bool { return true }

// Operator is the set's rights over its own system rows (SD4, SD6): no
// EDIT — an operator cannot change a seeded rule's content in place (the
// engine's divergence answer covers the hand-edited row: it is diverged and
// skipped, never silently rewritten, and never re-"corrected" by an edit
// back into the seed); REMOVE yes — an operator may retire a seeded rule,
// the ledger records it removed, and only seed.restore brings it back.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: false, Remove: true}
}

// Fingerprint reads the live row's content columns and hashes them — the
// same canonical form the source wrote, so the engine's divergence check is
// a content comparison, not a column one. origin is NOT among the hashed
// columns: provenance is not content (PM15) — a row whose origin flipped
// between user and system (the old verb's row adopted) hashes the same,
// which is exactly what lets adopt succeed.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	r, present, err := a.store.PolicySeedRow(ctx, tx, key)
	if err != nil {
		return "", false, err
	}
	if !present {
		return "", false, nil
	}
	h, err := hashContent(contentFromRow(r))
	if err != nil {
		return "", false, err
	}
	return h, true, nil
}

// Adoptable reports whether a PRESENT live row with no ledger row is an
// adoptable rule (SD9's policy shape): the old verb's row — seeded before
// the origin column existed (or by the CLI verb, whose rows carry 'user'
// and no ledger entry) whose CONTENT already equals the declared record.
// Content equality is the whole test: an adopt writes the same bytes the
// row already carries and merely records the ledger, so id and created_at
// are kept and nothing an operator authored differently is touched. A
// present row whose content differs is not adoptable — it is the
// operator's choice and the engine plans it as a conflict.
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	r, present, err := a.store.PolicySeedRow(ctx, tx, key)
	if err != nil {
		return false, err
	}
	if !present {
		return false, nil
	}
	h, err := hashContent(contentFromRow(r))
	if err != nil {
		return false, err
	}
	for _, rec := range a.source() {
		if rec.Key != key {
			continue
		}
		return h == seed.Hash(rec.Content), nil
	}
	return false, nil
}

// Upsert writes the record's policy through the ONE boundary, on the
// engine's shared transaction (SD3): a create when the row is absent
// (op=apply), a delete-then-insert when it is present (op=update — the
// boundary has no in-place rewrite, by design: a rule that "changed" is removed and
// re-added, so this is the boundary's own vocabulary, on one transaction,
// never two commits), or the same move filling the old verb's row in place
// (op=adopt, SD9 — the row keeps its id by key and its created_at by
// handing AddInTx the old row's instant as the clock parameter).
//
// Every stored row is paired with its authored audit row, exactly as the
// CLI seed verb pairs them — same subject (the target), same kind, the
// policy_id naming the rule — written on the ENGINE'S transaction through
// RecordAuthoredInTx, so a seeded rule and its witness commit together or
// not at all (PM8: half an authoring on one transaction is no authoring).
// This is the second sanctioned caller of that record (PM15): the seed
// door the PL15 grep knows by name.
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	p, rej := policyFromContent(r.Content)
	if rej != nil {
		return fmt.Errorf("policyseed: record %q carries content the boundary will not decode: %s", r.Key, rej.Reason)
	}
	if p.ID != r.Key {
		return fmt.Errorf("policyseed: record %q carries content for id %q — the key and the content must name one rule", r.Key, p.ID)
	}
	old, present, err := a.store.PolicySeedRow(ctx, tx, r.Key)
	if err != nil {
		return err
	}
	at := a.now()
	if present {
		if !adopt && old.Origin != policy.OriginSystem {
			return ErrConflict
		}
		if _, err := a.bound.RemoveInTx(ctx, tx, r.Key); err != nil {
			return err
		}
		if ts, terr := time.Parse(time.RFC3339Nano, old.CreatedAt); terr == nil {
			at = ts
		}
	}
	if wrej := a.bound.AddInTx(ctx, tx, p, policy.OriginSystem, at); wrej != nil {
		return fmt.Errorf("policyseed: the write boundary refused record %q: %s", r.Key, wrej.Reason)
	}
	arej := a.bound.RecordAuthoredInTx(ctx, tx, p.Target.String(), p.ID, at)
	if arej != nil {
		return fmt.Errorf("policyseed: the authored audit for %q failed: %s", p.ID, arej.Reason)
	}
	return nil
}

// Retire is the no-write write (SD6's policy shape): a record that left the
// code keeps its row — policy has no metadata column to flag and inventing
// one would be a schema change the ruling does not ask for — and the LEDGER
// row the engine writes in the same transaction is where "retired" lives.
// The row stays enforceable exactly as it stands: retirement removes the
// declaration, never the permission an operator may still rely on.
func (a *Applier) Retire(_ context.Context, _ *sql.Tx, _ string) error { return nil }

// contentFromRow maps a row's content columns back onto the canonical
// content — the inverse of what the source wrote, and the move that makes
// Fingerprint hash the same bytes. The empty-string spellings of NULL
// (target id, not_after) are the store's COALESCE answer; a stored
// not_after rides verbatim, because the row's rendering is the instant the
// boundary wrote.
func contentFromRow(r store.PolicyRow) content {
	c := content{
		ID:     r.ID,
		Type:   r.Type,
		Target: contentTarget{Kind: r.TargetKind, ID: r.TargetID},
		Action: r.Action,
		Spec:   json.RawMessage(r.SpecJSON),
		Source: r.Source,
	}
	if c.Spec == nil || len(c.Spec) == 0 {
		c.Spec = json.RawMessage("{}")
	}
	if r.NotAfter != "" {
		s := r.NotAfter
		c.NotAfter = &s
	}
	return c
}

// hashContent is the applier's one definition of a content's canonical
// hash, so Fingerprint, Adoptable and the engine's own Hash of the record's
// Content agree by going through one function, not by agreeing twice.
func hashContent(c content) (string, error) {
	b, err := seed.Canonical(c)
	if err != nil {
		return "", err
	}
	return seed.Hash(b), nil
}
