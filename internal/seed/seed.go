// Package seed is the one engine that applies code-declared, versioned seed
// sets to the database (S43-extra, SD1).
//
// The rule it makes true (the operator's, 2026-10-30): every record the
// product manages lives in the database — including the system ones defined
// in code. A system agent, a shipped policy, a standard blueprint, an MCP
// server definition, a tool's metadata: each is a row, put there by this
// engine, and at runtime the daemon reads only the row. Code is the source
// of the seed, never the source of truth. A *definition of shape* — the
// closed policy vocabulary, setting declarations, protocol methods, tool
// executors — is not a record and stays code; the test is "could a person
// reasonably want to list, read or copy it?"
//
// This package holds the engine's brain with no database, no clock and no
// I/O: what a set and a record are, how they are declared and validated, how
// content is canonicalised and hashed, and — as a pure function — what
// applying a set would do given the current ledger and live fingerprints
// (SD2–SD3, SD6). The time and the I/O arrive through the Engine (T3) over
// the Repo and Tx seams.
//
// Dependency rule (asserted by TestSeedImportsStdlibOnly): this package
// imports the standard library and nothing else — no internal/store,
// database/sql, internal/handler, internal/policy, internal/mcp,
// internal/daemon, internal/protocol. A domain imports seed, never the
// reverse (the internal/records precedent: pure data both sides can depend
// on, so a function cannot drift between the readings while a table cannot).
package seed

import "context"

// Class is the two-way safety split (SD5): an inert set changes nothing about
// what is permitted and applies unasked (at daemon start and by aeon init);
// a guard set widens or sets what a running loop may do and is never
// automatic — it is applied only by an explicit human-door action. Which
// class a set is is a product call (SD5); overruling it means guard sets
// also auto-apply, which contradicts D4 and PL1 and needs its own amendment.
type Class string

const (
	// ClassInert applies automatically at daemon start and by aeon init.
	ClassInert Class = "inert"
	// ClassGuard is applied only by an explicit seed.apply over the human door.
	ClassGuard Class = "guard"
	// ClassDefault is the product's own shipped defaults (2026-11-15 amendment to SD5/D4, by the
	// operator's ruling): it applies automatically at daemon start like inert, AND may write rows that
	// set what a running loop may do (the first-party agents' tool permissions). Its records are system
	// rows an operator keeps authority over: a removal is recorded and not re-applied, a hand-edited row
	// is diverged and left alone, and an operator's own rule always wins. It exists so the shipped
	// software is useful with no setup; it is NOT a way for any other path to widen permissions unasked.
	ClassDefault Class = "default"
)

// Set is one seed set, declared per area in its own file (SD2). A set is a
// release of a domain's records with a version that bumps when ANY record's
// content changes. It is registered by a domain's init() against this
// engine; a later management slice adds its own set the same way and
// invents no other way to ship a default (SD12).
type Set struct {
	// ID is the stable, grammar-checked set name ("agents.firstparty",
	// "mcp.servers" …). See the ID grammar in the registry.
	ID string
	// Class is inert or guard (SD5); a set whose Applier reports
	// GuardAffecting()==true may not be registered inert (rule 6).
	Class Class
	// Version is the set's release number; it bumps when any record's
	// content changes. It is the maximum of its records' versions.
	Version int
	// Records returns the set's records (never nil, rule 3). It is called
	// by the engine at apply time and by the seeder's source; it is not
	// cached.
	Records func() []Record
}

// Record is one stable, versioned row inside a set (SD2). Its Content is the
// canonical JSON the domain's Applier understands; the engine never parses a
// domain format, only its own records.
type Record struct {
	// Key is the stable identity inside the set — the domain's own key (an
	// agent slug, a policy id). Non-empty, <= 128 bytes, unique within the
	// set, no control characters (rule 4).
	Key string
	// Version is the record's own monotonic version (>= 1, <= Set.Version,
	// rule 3).
	Version int
	// Content is the canonical JSON the domain's Applier understands
	// (valid JSON, <= 1 MiB, rule 5). The hash is over its canonical form.
	Content []byte
}

// State is the ledger's per-record state (SD3). It is what status shows and
// what apply decides. absent is the engine's word for "no ledger row at
// all"; the others are the ledger's stored states.
type State string

const (
	// StateAbsent means no ledger row exists for the record yet.
	StateAbsent State = "absent"
	// StateApplied means the record's applied version equals its live
	// fingerprint (up to date).
	StateApplied State = "applied"
	// StateRemoved means an operator removed the live row (kept in the
	// ledger; seed.restore re-applies).
	StateRemoved State = "removed"
	// StateDiverged means the live row's content hash no longer equals the
	// applied hash (an operator or a dev changed it).
	StateDiverged State = "diverged"
	// StateRetired means the record left the code (its row is kept, never
	// auto-deleted).
	StateRetired State = "retired"
	// StateFailed means an apply of the record failed; it is retried on the
	// next apply and visible in status.
	StateFailed State = "failed"
)

// Op is the one operation an apply performs for a record (SD3). It is the
// ledger and audit vocabulary; a refusal is an Op=skip with a reason,
// except a downgrade, which is a Refusal on the result that fails the whole
// apply.
type Op string

const (
	// OpApply creates a new record row.
	OpApply Op = "apply"
	// OpUpdate updates an unmodified record in place to a higher version.
	OpUpdate Op = "update"
	// OpSkip is a no-op (up to date, diverged, removed, or a conflict).
	OpSkip Op = "skip"
	// OpRestore re-applies a removed/diverged/retired record on request.
	OpRestore Op = "restore"
	// OpRetire flags a record that left the code (its row is kept).
	OpRetire Op = "retire"
	// OpAdopt fills an existing S43 stub row in place (SD9).
	OpAdopt Op = "adopt"
)

// OperatorRights is what a domain says an operator may do to a system row
// (SD4, SD6). The engine reads it only to EXPLAIN skips; enforcement of
// "may an operator edit/remove this row" stays in each domain.
type OperatorRights struct {
	// Edit is whether an operator may change a system row's content (which
	// makes the row diverged, never silently overwritten).
	Edit bool
	// Remove is whether an operator may delete a system row (which the
	// ledger records as removed, re-applied only by seed.restore).
	Remove bool
}

// Result is the engine's view of a transaction's write outcome (the seam
// over *sql.Result; see Tx). It carries no sql type — internal/seed imports
// the standard library only, so the daemon (T6) adapts *sql.Tx to it.
type Result interface {
	// LastInsertId is the rowid of the last inserted row (or an error).
	LastInsertId() (int64, error)
	// RowsAffected is how many rows the last statement changed.
	RowsAffected() (int64, error)
}

// Rows is the engine's view of a transaction's query results (the seam over
// *sql.Rows; see Tx). It is the minimal read a domain Applier needs for
// Fingerprint; the daemon (T6) adapts *sql.Rows to it.
type Rows interface {
	// Next advances to the next row; false when there are no more.
	Next() bool
	// Scan copies the current row's columns into dest.
	Scan(dest ...any) error
	// Close releases the rows.
	Close() error
}

// Tx is the minimal transaction seam a domain Applier and the Engine share
// (the seam over *sql.Tx, no sql import — the daemon adapts it in T6). It is
// the one way a seed apply touches the database, and the one thing that
// lets a domain row and its ledger row commit together in one transaction
// (SD3). A domain never imports database/sql through the engine.
type Tx interface {
	// ExecContext runs a write statement on the transaction (the domain's
	// row write; it commits with the ledger, SD3).
	ExecContext(ctx context.Context, query string, args ...any) (Result, error)
	// QueryContext runs a read on the transaction and returns its rows.
	QueryContext(ctx context.Context, query string, args ...any) (Rows, error)
}
