package seed

// Applier is the domain's half of the engine: it knows how to read, write
// and flag the domain's own rows for one set (SD2, SD4, SD6). It is
// declared here in the seed package and implemented per domain (T4's
// agentseed applier is the first; a later slice adds its own). The engine
// calls it through the Repo's transaction seam, so a domain row and its
// ledger row commit together (SD3).
//
// A domain never imports database/sql through the engine: it reads and
// writes through Tx (the minimal seam the daemon adapts from *sql.Tx in
// T6). Fingerprint reads the live row's content hash; Upsert writes the
// row (adopt=true fills an existing stub, SD9); Retire flags a row whose
// record left the code (its row is kept, never deleted).
type Applier interface {
	// Set is the set ID this applier serves; it is the lookup key the
	// registry and the engine use (rules 6, 7).
	Set() string

	// GuardAffecting reports whether applying this set widens or sets what
	// a running loop may do (SD5). A set that is guard-affecting may not be
	// registered inert (rule 6): an inert set changes nothing about what is
	// permitted, and a guard-affecting applier that were allowed to be
	// inert would apply unasked at daemon start — the D4/PL1 boundary.
	GuardAffecting() bool

	// Operator is what an operator may do to a system row of this domain
	// (SD4, SD6): edit and/or remove. The engine reads it only to explain
	// skips; the domain enforces the rule (a system row the operator
	// changed is diverged, one the operator removed is removed — both
	// skipped by apply, re-applied only by restore).
	Operator() OperatorRights

	// Fingerprint returns the canonical content hash of the LIVE row for
	// key and whether the row is present, inside the transaction. present
	// is false when the row is absent (the engine then knows a create is
	// due, not a diverge). It reads the row's content columns only, so a
	// hand edit of a content column is seen as a diverge.
	Fingerprint(tx Tx, key string) (hash string, present bool, err error)

	// Adoptable reports whether a PRESENT live row with no ledger row is an
	// adoptable stub (SD9): a S43 stub the applier will fill in place. It is
	// the applier's domain call — for the agents applier it is a row with
	// the slug, internal=1 and empty content. The engine uses it to decide
	// adopt vs conflict so the plan and apply agree on the same state.
	Adoptable(tx Tx, key string) (bool, error)

	// Upsert writes the row for record: a new row when the key is absent
	// (op=apply), an update of the content columns when the row is present
	// (op=update), or — when adopt is true — a fill of an existing stub
	// row, keeping its id, slug and created_at (op=adopt, SD9). It never
	// writes id, slug, created_at or the signature columns.
	Upsert(tx Tx, r Record, adopt bool) error

	// Retire flags the row for key as retired — its record left the code —
	// keeping the row (never deleted, SD6). It is a write, not a delete.
	Retire(tx Tx, key string) error
}
