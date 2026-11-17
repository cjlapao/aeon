package toolseed

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// applier.go — the domain's half of the engine for `tools.core` (SD2, SD4): how
// one catalog record becomes one row, how a live row is fingerprinted, and what
// retirement means for a row that is never deleted.
//
// The engine calls these through its transaction seam, so the row and its ledger
// row commit together (SD3). `Fingerprint` is the subtle one: it must rebuild the
// **same canonical content** `Source` hashed, from the row's columns — if the two
// derived their hashes differently, every start would report the whole catalog as
// diverged, and the honest-looking way to "fix" that (hash the row's primary key)
// would make the check meaningless.

// Applier writes the tool catalog rows. It holds the store and nothing else: no
// MCP registry (MC11), no policy set, no clock — retirement stamps its own
// timestamp through the store, which owns the one clock.
type Applier struct {
	s *store.Store
}

// NewApplier builds the applier over the store. The daemon mounts it in
// `NewSeedEngine`, which is where every door's engine is built.
func NewApplier(s *store.Store) *Applier { return &Applier{s: s} }

// Set is the set this applier serves — the lookup key (rules 6, 7).
func (a *Applier) Set() string { return SetID }

// GuardAffecting is false, and the engine checks the pairing: registering this
// set inert with an applier that said otherwise is refused at registration
// (registry.go's rule 6). Applying the catalog changes no permission because
// nothing on an execution path reads a row (TC2), which is what makes the claim
// true rather than merely declared.
func (a *Applier) GuardAffecting() bool { return false }

// Operator is `{Edit: false, Remove: false}`: an operator may neither edit a
// catalog row nor remove it. Both halves follow from the row's nature — its
// content is a projection of code, so a hand edit is drift to report (T5), not a
// variation to honour, and removal would delete history the ledger still points
// at. `seed.apply` re-publishes the truth when the code changes; nothing else
// needs to write here.
func (a *Applier) Operator() seed.OperatorRights {
	return seed.OperatorRights{Edit: false, Remove: false}
}

// Fingerprint hashes the LIVE row's content in the same canonical form `Source`
// published, inside the transaction. `present` false is the engine's word for "a
// create is due", not an error.
func (a *Applier) Fingerprint(ctx context.Context, tx *sql.Tx, key string) (string, bool, error) {
	row, present, err := a.s.SeedToolRow(ctx, tx, key)
	if err != nil {
		return "", false, err
	}
	if !present {
		return "", false, nil
	}
	b, err := seed.Canonical(contentFromRow(row))
	if err != nil {
		return "", false, err
	}
	return seed.Hash(b), true, nil
}

// Adoptable is false, always. There is no stub shape for a catalog row — no
// earlier slice created `tool` rows, and a row that exists with different content
// is either an apply the ledger never recorded or a hand edit, both of which the
// engine settles as a conflict to report rather than a row to fill in place.
// Returning true here would let a start silently overwrite an operator's row.
func (a *Applier) Adoptable(_ context.Context, _ *sql.Tx, _ string) (bool, error) {
	return false, nil
}

// Upsert writes the row on the caller's transaction. `adopt` is accepted and
// ignored: with no adoptable shape, an adopt is the same write as an update, and
// the engine still books the distinction in the audit (which is the engine's
// fact, not this one).
//
// The `version` written is the RECORD's, not the content's — a content change
// that did not bump its version is refused by the golden test rather than by
// trusting whatever the record says.
func (a *Applier) Upsert(ctx context.Context, tx *sql.Tx, r seed.Record, _ bool) error {
	c, err := ContentOf(r)
	if err != nil {
		return err
	}
	return a.s.ToolUpsertTx(tx, store.ToolRow{
		Name:         c.Name,
		Family:       c.Family,
		Description:  c.Description,
		Schema:       string(c.Schema),
		SchemaHash:   c.SchemaHash,
		RerunClass:   c.RerunClass,
		Capabilities: marshal(c.Capabilities),
		Governed:     marshal(c.Governed),
		Twin:         c.Twin,
		Aliases:      marshal(c.Aliases),
		Conditional:  c.Conditional,
		Notes:        c.Notes,
		Version:      r.Version,
	})
}

// Retire flags the row — it never deletes it (SD6). The flag lives in the ledger's
// state; the row keeps every content column so a person can still read what the
// tool was, and `Drift` reports it as `retired` from the two sides it is given.
func (a *Applier) Retire(ctx context.Context, tx *sql.Tx, key string) error {
	_, err := a.s.SeedToolRetire(ctx, tx, key)
	return err
}

// contentFromRow rebuilds the record content from a row's columns. Every column
// is decoded to the same Go shape `Source` encoded, including the two empties:
// a nil slice marshals to `null` and an empty one to `[]`, and a hash that flips
// between those two would report divergence for a row nobody touched.
func contentFromRow(r store.ToolRow) Content {
	return Content{
		Name:         r.Name,
		Family:       r.Family,
		Description:  r.Description,
		Schema:       json.RawMessage(orNull(r.Schema)),
		SchemaHash:   r.SchemaHash,
		RerunClass:   r.RerunClass,
		Capabilities: unmarshal(r.Capabilities),
		Governed:     unmarshal(r.Governed),
		Twin:         r.Twin,
		Aliases:      unmarshal(r.Aliases),
		Conditional:  r.Conditional,
		Notes:        r.Notes,
	}
}

func marshal(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	b, err := json.Marshal(ss)
	if err != nil {
		return "[]" // Unreachable for []string; never a silent "null" if it ever were.
	}
	return string(b)
}

func unmarshal(s string) []string {
	var out []string
	if s == "" || !json.Valid([]byte(s)) {
		return []string{}
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func orNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}
