package blueprintseed

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoStore is the adapter's refusal until the daemon installs the store:
// a set with no store is a named refusal, not a silent gap (the agentseed
// precedent).
var errNoStore = errors.New("blueprintseed: the store is not installed — the daemon installs it at serve (S48 T5)")

// Applier is the domain's half (SD2, SD4, SD6): it reads and writes the
// blueprint rows for this set, always through store.BlueprintPutTx — the
// one write path (T1). It takes *sql.Tx (the store's row methods need it
// and a seed.Tx cannot reach them — the same finding agentseed records),
// and the daemon adapts it to the seed.Applier the registry holds.
type Applier struct {
	store  *store.Store
	setID  string
	source func() []seed.Record
}

// NewApplier binds the standard set to a store.
func NewApplier(s *store.Store) *Applier { return NewApplierFor(s, SetID, Source) }

// NewApplierFor binds a DIFFERENT shipped set to the SAME blueprint row
// machinery (S60 T7): blueprints.landing records carry the identical
// {name, version, content} envelope — same row table, same validation,
// same revision shape — so the second set gets a second pair of coordinates,
// not a second applier to drift. The source may be any function returning
// records in this envelope; an empty pair falls back to the standard set
// (zero-value use stays the standard applier).
func NewApplierFor(s *store.Store, setID string, source func() []seed.Record) *Applier {
	if setID == "" {
		setID = SetID
	}
	if source == nil {
		source = Source
	}
	return &Applier{store: s, setID: setID, source: source}
}

// Set is the set ID this applier serves.
func (a *Applier) Set() string {
	if a.setID == "" {
		return SetID
	}
	return a.setID
}

// GuardAffecting is false (SD5): a blueprint is a definition, not a
// permission — applying it widens nothing a loop may do. It is what lets
// the set be inert (registry rule 6).
func (a *Applier) GuardAffecting() bool { return false }

// Operator: an operator may EDIT a system blueprint (it diverges — BP6
// keeps their text); they may not REMOVE one from the shipped set (the row
// is a shipped thing; the delete door refuses it — BP10's shape).
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: true, Remove: false}
}

// records is the set's source with the fallback already resolved by the
// constructors — the Adoptable walk and nothing else reads the set's own
// content from inside the applier.
func (a *Applier) records() []seed.Record {
	if a.source == nil {
		return Source()
	}
	return a.source()
}

// Fingerprint is the canonical content hash of the live row — the same
// function the engine's Hash applies to a record's Content. The row stores
// the YAML bytes verbatim (the database holds the shipped text), while the
// record wraps them in the {name, version, content} envelope, so the
// fingerprint re-envelopes the live fields and hashes THAT: applied==live
// exactly when the row holds the record, and a hand edit of the content
// column reads as a diverge. The row's own content_hash column stays the
// hash of the bytes as stored (T1's shape, the adoption ledger's key).
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	if err := a.need(); err != nil {
		return "", false, err
	}
	row, present, err := a.store.BlueprintRowTx(ctx, tx, key)
	if err != nil || !present {
		return "", false, err
	}
	body, err := json.Marshal(recordContent{Name: key, Version: row.Version, Content: row.Content})
	if err != nil {
		return "", false, fmt.Errorf("blueprintseed: fingerprinting %q: %w", key, err)
	}
	return seed.Hash(body), true, nil
}

// Adoptable is the domain's answer to the engine's question (SD9): a live
// row with no ledger entry is adoptable — updated in place — exactly when
// it is a system row whose bytes are the shipped bytes or a recognised
// pristine revision. Any other live row is a conflict the engine reports
// and skips: an operator's edit is never overwritten (BP6).
func (a *Applier) Adoptable(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	if err := a.need(); err != nil {
		return false, err
	}
	row, present, err := a.store.BlueprintRowTx(ctx, tx, key)
	if err != nil || !present {
		return false, err
	}
	if row.Origin != "system" {
		return false, nil // a user row is not this set's to fill
	}
	for _, r := range a.records() {
		if r.Key != key {
			continue
		}
		c, err := DecodeRecord(r)
		if err != nil {
			return false, err
		}
		sum := sha256.Sum256([]byte(c.Content))
		if row.ContentHash == hex.EncodeToString(sum[:]) {
			return true, nil
		}
		for _, h := range PristineHashes(key) {
			if h == row.ContentHash {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil // not a record of this set — not adoptable by it
}

// Upsert writes the row on the caller's transaction. The engine decides
// WHEN to write (apply/adopt/update/skip); this decides the revision's op
// from the state it is being asked to write INTO — the engine's call does
// not carry the op, and the states are exclusive:
//
//	no live row                            ⇒ 'seed'    (a new shipped row)
//	present, the engine says adopt         ⇒ 'update'  (a pristine copy updated in place)
//	present, bytes already the shipped ones ⇒ 'update' (a re-claim at the same content)
//	present, other bytes                   ⇒ 'restore' (the engine reaches a diverged row
//	                                                      ONLY through seed.restore)
//
// Content is validated exec-grade (blueprint.Load) before any write — the
// set never plants a row the loader would refuse — and refs are derived
// from the parsed blueprint, all of it on the engine's transaction so row,
// revision, refs and the ledger commit together (SD3).
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, adopt bool) error {
	if err := a.need(); err != nil {
		return err
	}
	c, err := DecodeRecord(r)
	if err != nil {
		return err
	}
	bp, err := blueprint.Load([]byte(c.Content))
	if err != nil {
		return fmt.Errorf("blueprintseed: shipped content for %q does not load: %w", r.Key, err)
	}
	sum := sha256.Sum256([]byte(c.Content))
	hash := hex.EncodeToString(sum[:])
	existing, present, err := a.store.BlueprintRowTx(ctx, tx, c.Name)
	if err != nil {
		return err
	}
	// The revision's op, read from the ledger the engine is about to
	// update (the ledger is the memory of what was applied; the engine's
	// call carries no op, and the plan guarantees these states are
	// exclusive — apply never writes a diverged row, only Restore does):
	ledState, ledHash := seedLedger(ctx, a.store, a.Set(), c.Name)
	op := "seed"
	switch {
	case adopt:
		op = "update" // a pristine copy filled in place
	case !present && (ledState == "removed" || ledState == "retired"):
		op = "restore" // re-applied over an operator's removal / a retire
	case !present && ledState != "":
		op = "update" // a retry of a failed apply whose row half-existed
	case present && ledState == "":
		op = "update" // a never-ledgered row re-claimed by apply
	case present && existing.ContentHash == ledHash:
		op = "update" // unmodified under the ledger — an update in place
	case present:
		op = "restore" // only seed.restore reaches a row the ledger calls diverged
	}
	row := store.BlueprintRow{
		Name: c.Name, Version: r.Version, Content: c.Content, ContentHash: hash,
		Origin: "system", UpdatedBy: "system:seed",
	}
	rev := store.RevisionRow{
		Version: r.Version, Content: c.Content, ContentHash: hash,
		Op: op, ActorKind: "system", ActorID: "seed",
	}
	var refs []store.RefRow
	for _, ref := range blueprint.References(&bp) {
		refs = append(refs, store.RefRow{Name: c.Name, Kind: ref.Kind, Target: ref.Target})
	}
	return store.BlueprintPutTx(tx, row, rev, refs)
}

// Retire keeps the row (never delete, SD6) and writes nothing: the
// blueprint table has no retired column, and its revision op vocabulary is
// closed (T1's CHECK) — the memory that the record left the code is the
// engine's ledger row (state retired), which seed.status shows. The row
// stays exactly as it was.
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	if err := a.need(); err != nil {
		return err
	}
	_, _, err := a.store.BlueprintRowTx(ctx, tx, key)
	return err
}

// seedLedger reads this set's ledger entry for one key — its state and
// applied hash, both "" when there is no ledger row. It is the applier's
// READ of the engine's memory, never a write: the engine still writes the
// ledger row itself (SD3); the applier only consults it to name its own
// revision op correctly.
func seedLedger(ctx context.Context, s *store.Store, setID, key string) (state, appliedHash string) {
	rows, err := s.SeedLedgerList(ctx, setID)
	if err != nil {
		return "", ""
	}
	for _, r := range rows {
		if r.RecordKey == key {
			return r.State, r.AppliedHash
		}
	}
	return "", ""
}

func (a *Applier) need() error {
	if a.store == nil {
		return errNoStore
	}
	return nil
}

// Adapter is the seed.Applier the package-global registry holds from init:
// it refuses until the daemon mounts the store-backed applier
// (NewSeedEngine's function-mount, the policy/watchdog precedent — a
// domain may not widen the global from a distance).
type Adapter struct{}

func (Adapter) Set() string                   { return SetID }
func (Adapter) GuardAffecting() bool          { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: true, Remove: false} }

func (Adapter) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, errNoStore
}
func (Adapter) Adoptable(seed.Tx, string) (bool, error) { return false, errNoStore }
func (Adapter) Upsert(seed.Tx, seed.Record, bool) error { return errNoStore }
func (Adapter) Retire(seed.Tx, string) error            { return errNoStore }
