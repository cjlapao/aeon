package store

// tool.go — the store's half of S47: the **catalog entry** of a system tool as
// a row (design TC2).
//
// # What a row is, and what it is not
//
// A tool has two halves with different natures. Its catalog entry — identity,
// family, description, class, capabilities, socket twin, origin, version — is
// what a person lists and reads, so CONFIGURATION-ORDER's rule 6 puts it in the
// database. Its **executor and its input schema are code**: the shape a call is
// parsed against and the function that runs it cannot be edited apart from each
// other, so they stay in `internal/tools`, and the model-facing spec the runtime
// offers still comes from `tools.Registry()`. The row carries a read-only copy
// of the schema **plus its SHA-256** for exactly one reason: so `tools.status`
// can say the copy and the code have drifted (TC11). Nothing here is consulted
// by tool execution or by the specs a model is shown — `TestToolTableIsNotReadByTheRuntimeSpecPath`
// makes that a fact of the build, not a promise.
//
// # One writer
//
// The INSERT/UPDATE path is `ToolUpsertTx`, and it is called by **the
// `tools.core` seed applier and nobody else** (T3). There is deliberately **no
// delete accessor**: a tool whose code went away leaves its row and is reported
// `retired` (S43-extra SD6's rule for a system record — the ledger and the row
// are the history, and a row that can be deleted is a history that can be
// edited). MCP tools are not rows here at all: S46's tables are their one truth
// (TC4), and a second table holding them would be a second answer to "what tools
// are there".

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ToolRow is one tool's catalog entry as the database holds it. The JSON-shaped
// columns are stored as text and validated by CHECKs (TC2); they are decoded by
// the readers above them, never here, so this layer stays SQL and the catalog
// package owns the meaning.
type ToolRow struct {
	Name         string
	Family       string
	Description  string
	Schema       string // canonical JSON, a display copy — the executor's schema is code
	SchemaHash   string // hex SHA-256 of that canonical JSON, 64 chars
	RerunClass   string // safe | detectable | unsafe — never defaulted
	Capabilities string // JSON array, at least one entry
	Governed     string // JSON array of policy type names
	Twin         string // the records socket twin; "" for a core tool
	Aliases      string // JSON array (the hidden write_file alias rides here, on write)
	Conditional  bool   // seeded always, availability computed live (TC7)
	Notes        string // ≤ 200 chars, rendered in the app
	Version      int    // bumped by the seed set's golden test, never by hand
	Origin       string // 'system' — the CHECK admits nothing else in this slice
	CreatedAt    string
	UpdatedAt    string
}

// ToolCount is how many catalog rows exist. The seed's status read and the
// "every system tool has a row" acceptance criterion both start here.
func (s *Store) ToolCount(ctx context.Context) (int, error) {
	var n int
	if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM tool`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: counting the tool catalog: %w", err)
	}
	return n, nil
}

// ToolList returns every catalog row, ordered by family then name — the order a
// person reads a catalog in, and a stable one, so a UI list does not shuffle
// between two refreshes (which would make a filter feel broken when it is not).
func (s *Store) ToolList(ctx context.Context) ([]ToolRow, error) {
	rows, err := s.r.QueryContext(ctx, toolSelect+` ORDER BY family, name`)
	if err != nil {
		return nil, fmt.Errorf("store: listing the tool catalog: %w", err)
	}
	defer rows.Close()
	var out []ToolRow
	for rows.Next() {
		r, err := scanToolRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: listing the tool catalog: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing the tool catalog: %w", err)
	}
	return out, nil
}

// ToolGet reads one catalog row. A name with no row is sql.ErrNoRows — the
// catalog's `missing_row` state (TC11) is a fact about code having no row, and
// it is reported by the drift check, never defaulted to a row that was not
// written.
func (s *Store) ToolGet(ctx context.Context, name string) (ToolRow, error) {
	row := s.r.QueryRowContext(ctx, toolSelect+` WHERE name = ?`, name)
	r, err := scanToolRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ToolRow{}, sql.ErrNoRows
		}
		return ToolRow{}, fmt.Errorf("store: reading the tool catalog row %q: %w", name, err)
	}
	return r, nil
}

// ToolUpsertTx writes one catalog row inside the caller's transaction. It is the
// **only** write path, and its only caller is the `tools.core` seed applier (T3)
// — which reaches it through the seed engine's transaction seam so the row and
// its ledger row commit together.
//
// An update writes the content columns and `updated_at`; it **never** touches
// `created_at` (the row's identity moment) and never touches `origin` (a row
// this slice ships is a system row forever — the CHECK admits nothing else, and
// a user-authored tool could not exist anyway: executors are code). `version`
// travels with the content, because a content change that did not bump its
// version is precisely the drift TC11 exists to make visible, and the golden
// test catches it at `go test` time.
func (s *Store) ToolUpsertTx(tx *sql.Tx, row ToolRow) error {
	if tx == nil {
		return errors.New("store: ToolUpsertTx needs the caller's transaction — a catalog row never commits on its own")
	}
	if row.Name == "" {
		return errors.New("store: ToolUpsertTx needs a name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.Exec(`
		INSERT INTO tool (
			name, family, description, schema, schema_hash, rerun_class,
			capabilities, governed, twin, aliases, conditional, notes,
			version, origin, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'system', ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			family        = excluded.family,
			description   = excluded.description,
			schema        = excluded.schema,
			schema_hash   = excluded.schema_hash,
			rerun_class   = excluded.rerun_class,
			capabilities  = excluded.capabilities,
			governed      = excluded.governed,
			twin          = excluded.twin,
			aliases       = excluded.aliases,
			conditional   = excluded.conditional,
			notes         = excluded.notes,
			version       = excluded.version,
			updated_at    = excluded.updated_at`,
		row.Name, row.Family, row.Description, row.Schema, row.SchemaHash, row.RerunClass,
		row.Capabilities, defaultJSON(row.Governed), row.Twin, defaultJSON(row.Aliases), boolInt(row.Conditional),
		row.Notes, row.Version, now, now)
	if err != nil {
		return fmt.Errorf("store: upserting the tool catalog row %q: %w", row.Name, err)
	}
	return nil
}

// SeedToolRow reads one catalog row **inside the caller's transaction** — the
// seed engine's read (SD3). The fingerprint and the plan must see the state the
// write lands in: a fingerprint from the read pool could straddle the
// transaction and make the plan and the apply disagree about whether a row
// exists. `present` false is the engine's word for "a create is due"; an error
// is an infrastructure failure and never a missing row.
func (s *Store) SeedToolRow(ctx context.Context, tx *sql.Tx, name string) (ToolRow, bool, error) {
	if tx == nil {
		return ToolRow{}, false, errors.New("store: SeedToolRow needs the caller's transaction")
	}
	row, err := scanToolRow(tx.QueryRowContext(ctx, toolSelect+` WHERE name = ?`, name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ToolRow{}, false, nil
		}
		return ToolRow{}, false, fmt.Errorf("store: reading the tool catalog row %q in the seed transaction: %w", name, err)
	}
	return row, true, nil
}

// SeedToolRetire is the seed engine's `Retire` write (SD6): the record left the
// code and the row is **kept**. There is no `retired` column, deliberately — the
// flag is the ledger's state, and `toolseed.Drift` reports a row with no code
// record as `retired` from the two sides it is handed. Touching a content column
// here would be the worst kind of lie: it would read as *divergence* against the
// applied hash for a tool nobody edited. So the write records the one true fact
// a retirement adds — when the row stopped being current.
func (s *Store) SeedToolRetire(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	if tx == nil {
		return false, errors.New("store: SeedToolRetire needs the caller's transaction")
	}
	res, err := tx.ExecContext(ctx, `UPDATE tool SET updated_at = ? WHERE name = ?`,
		time.Now().UTC().Format(time.RFC3339), name)
	if err != nil {
		return false, fmt.Errorf("store: retiring the tool catalog row %q: %w", name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: retiring the tool catalog row %q: %w", name, err)
	}
	return n > 0, nil
}

// toolSelect is the one SELECT list of the catalog, shared by the two reads so a
// column can never be added to one and forgotten in the other.
const toolSelect = `SELECT
	name, family, description, schema, schema_hash, rerun_class,
	capabilities, governed, twin, aliases, conditional, notes,
	version, origin, created_at, updated_at
	FROM tool`

// scanToolRow is the one row reader (the package's shared `rowScanner`, satisfied
// by both *sql.Row and *sql.Rows), so the single read and the list cannot drift
// apart on a column.
func scanToolRow(r rowScanner) (ToolRow, error) {
	var (
		row     ToolRow
		cond    int
		schema  string
		caps    string
		gov     string
		aliases string
	)
	err := r.Scan(&row.Name, &row.Family, &row.Description, &schema, &row.SchemaHash, &row.RerunClass,
		&caps, &gov, &row.Twin, &aliases, &cond, &row.Notes,
		&row.Version, &row.Origin, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return ToolRow{}, err
	}
	row.Schema, row.Capabilities, row.Governed, row.Aliases = schema, caps, gov, aliases
	row.Conditional = cond != 0
	return row, nil
}

func defaultJSON(s string) string {
	if s == "" {
		return "[]"
	}
	return s
}
