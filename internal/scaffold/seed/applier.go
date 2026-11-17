package scaffoldseed

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// errNoStore is the adapter's refusal until the daemon installs the
// store-backed applier (the retroseed precedent: the global registration
// carries the refusing Adapter; the store-backed applier is NewSeedEngine's
// function-mount, and the daemon's wrapper unwraps seed.Tx to the *sql.Tx
// every method here takes).
var errNoStore = errors.New("scaffoldseed: the store is not installed — the daemon mounts it at NewSeedEngine")

// Applier writes the set through the store's ONE template save path:
// SaveTemplateTx lays row+revision+ledger-row in the engine's transaction
// (SD3 — the Tx twin exists for exactly this call), with origin 'system'
// on a new slug and the actor system:seed naming the machine honestly
// (migration 20261105093000 widened the revision CHECK to admit it).
type Applier struct {
	s *store.Store
}

// NewApplier binds the applier to the daemon's store.
func NewApplier(s *store.Store) *Applier { return &Applier{s: s} }

func (a *Applier) Set() string { return SetID }

// GuardAffecting is false: a template row changes nothing a loop may do —
// it is a menu entry until a person scaffolds with it, and the scaffold
// itself writes only into a fresh target through a person door. Registry
// rule 6 is what lets the set be inert at all.
func (a *Applier) GuardAffecting() bool { return false }

// Operator: Edit true, Remove false (GI14). Editing a shipped template is
// the design — the person's template.save door is specced for it — and an
// edited template then reads diverged, never overwritten by a re-seed,
// restorable only by an explicit restore. Removal has no door because the
// table has no delete: the schema itself refuses it, and resurrecting the
// question with Remove:true would be a door to a refusal.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: true, Remove: false}
}

// Fingerprint rebuilds the canonical content from the live row's words and
// files, so applied==live exactly when the row holds what we shipped; the
// person's edit door, a title fix, anything that moves the bytes reads as
// diverged, and the engine reports without overwriting (version is
// metadata, outside the hash — the verifyseed convention).
func (a *Applier) Fingerprint(tx *sql.Tx, key string) (string, bool, error) {
	if a.s == nil {
		return "", false, errNoStore
	}
	row, present, err := a.s.GetTemplateTx(tx, key)
	if err != nil || !present {
		return "", false, err
	}
	c := Content{Title: row.Title, Description: row.Description, StackTags: row.StackTags}
	if len(c.StackTags) == 0 {
		// The store round-trips "[]" to an EMPTY slice while the record
		// marshals an absent list as null; both spell "no tags". Without
		// this normalization a freshly laid row would fingerprint diverged
		// from the moment it was laid — the report would lie about bytes
		// that never moved.
		c.StackTags = nil
	}
	for _, f := range row.Files {
		c.Files = append(c.Files, FileSpec{Path: f.Path, Mode: f.Mode, Content: f.Content})
	}
	b, err := seed.Canonical(c)
	if err != nil {
		return "", false, fmt.Errorf("scaffoldseed: fingerprinting %q: %w", key, err)
	}
	return seed.Hash(b), true, nil
}

// Adoptable is false, always: no earlier slice wrote scaffold_template
// rows (T1 laid the table), so a present row without a ledger row is a
// hand write — a conflict the engine reports, never a stub to fill.
func (a *Applier) Adoptable(_ *sql.Tx, _ string) (bool, error) { return false, nil }

// Upsert lays one template row inside the engine's transaction. A shipped
// template the STORE refuses is a build bug, and it says so — the seed
// must not half-lay a set.
func (a *Applier) Upsert(tx *sql.Tx, r seed.Record, _ bool) error {
	if a.s == nil {
		return errNoStore
	}
	c, err := ContentOf(r)
	if err != nil {
		return err
	}
	files := make([]store.TemplateFile, 0, len(c.Files))
	for _, f := range c.Files {
		files = append(files, store.TemplateFile{Path: f.Path, Mode: f.Mode, Content: f.Content})
	}
	_, ref, err := a.s.SaveTemplateTx(tx, store.NewTemplate{
		Slug:        r.Key,
		Title:       c.Title,
		Description: c.Description,
		StackTags:   c.StackTags,
		Files:       files,
		SeedOrigin:  "system",
	}, store.Actor{Kind: "system", ID: "seed"})
	if err != nil {
		return fmt.Errorf("scaffoldseed: laying %q: %w", r.Key, err)
	}
	if ref != nil {
		return fmt.Errorf("scaffoldseed: the store refused shipped template %q (a build bug, not an operator's problem): %s", r.Key, ref.Sentence)
	}
	return nil
}

// Retire refuses: the domain has no removal door — the schema itself
// refuses the delete (the append-only pair on the revision and the
// no-delete trigger on the row), so the applier reports the absence of the
// door rather than silently half-doing a removal this slice refused to
// have.
func (a *Applier) Retire(_ *sql.Tx, key string) error {
	return fmt.Errorf("scaffoldseed: %q has no retire door (Remove:false — a shipped template is edited or left, never deleted)", key)
}

// Adapter is the seed.Applier the package-global registry holds from init:
// it refuses until the daemon mounts the store-backed applier.
type Adapter struct{}

func (Adapter) Set() string                   { return SetID }
func (Adapter) GuardAffecting() bool          { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: true, Remove: false} }

func (Adapter) Fingerprint(seed.Tx, string) (string, bool, error) { return "", false, errNoStore }
func (Adapter) Adoptable(seed.Tx, string) (bool, error)           { return false, errNoStore }
func (Adapter) Upsert(seed.Tx, seed.Record, bool) error           { return errNoStore }
func (Adapter) Retire(seed.Tx, string) error                      { return errNoStore }
