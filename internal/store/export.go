package store

// The store's half of the S62 T8 export seam.
//
// The read side of the seam is a deliberately thin SQL surface, and this file
// is its single definition: the closed table set, the per-table column
// allow-lists, the spec shape (TableSpec) and the refusal of any identifier
// the lists do not declare (CheckSpec). The export engine (internal/export)
// builds its exporter reads from these declarations — the engine imports the
// store for their shape, exactly as internal/backup does, and this package
// never imports the engine (the one-way direction the import guard in
// store_test.go pins). A spec whose declared column the schema does not have
// answers ErrColumnsMissing — the set is unavailable (slice not built), not
// an error (BK14's three states stay three states).
//
// No network, no database/sql import of its own: the queries run on the
// store's read pool (O6/I6 — a read never touches the writer), and the DSN,
// the pragmas and the pooling are the store's own business.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// TableSpec is a declared read: one table, the exact columns to select, and
// the order key. Every identifier in a spec is checked against the closed
// allow-lists declared in this file (CheckSpec), and then against the schema
// — a missing column is ErrColumnsMissing, which the engine turns into
// "unavailable (slice not built)", never an error (BK14: the three states
// stay three states).
type TableSpec struct {
	Table   string
	Columns []string
	// OrderBy is a declared column of the table, optionally suffixed " DESC".
	OrderBy string
	// Where is declared for the shape's future; this slice supports no row
	// predicate, and a non-empty value is refused (a predicate is a second
	// selection rule, and selection rules live here, in the allow-list).
	Where string
}

// colsAgent: the agent's record content, verbatim. signature and
// signer_certificate are deliberately NOT declared — signing artifacts are
// credentials in all but name, and an export copies declared content, not
// everything the row happens to hold.
var colsAgent = []string{
	"id", "slug", "name", "description", "functionality", "metadata", "prompt",
	"skills", "version", "internal", "avatar_type", "avatar_color", "avatar_mood",
	"signed", "created_at", "updated_at", "tools", "temperature", "handoff",
	"policy_agent", "body_hash", "origin",
}

var colsPolicy = []string{
	"id", "type", "target_kind", "target_id", "action", "spec", "source",
	"not_after", "created_at", "updated_at", "origin",
}

// colsRoute: the route table holds no key column at all — the keys live in
// the credentials/ files on disk (design BK14's "credentials never" half is
// structural here, not a filter).
var colsRoute = []string{
	"id", "provider", "model", "base_url", "max_output_tokens", "prices",
	"extra_body", "created_at", "updated_at", "reasoning_effort", "openai_endpoint", "audio_api",
	"context_window", "display_name", "display_description", "icon",
	"capabilities", "reasoning_efforts", "wire_capabilities", "no_price",
}

// colsComponent: remote_key and repo_key are credential-shaped (a remote URL
// can carry a token) and are NOT declared. parent_id and archived_at ARE
// declared — they shape into the tree's parentId/archivedAt.
var colsComponent = []string{
	"id", "name", "slug", "remote", "owner", "tags", "created_at", "updated_at",
	"icon", "short_description", "description", "parent_id", "archived_at",
	"default_branch", "branch_prefix", "queue_advance", "sizing_mode",
	"max_parallel_agents",
}

// colsComponentRoot: remote_key/repo_key excluded as in colsComponent.
var colsComponentRoot = []string{
	"component_root", "component_id", "is_primary", "added_at",
}

var colsEventTrigger = []string{
	"id", "name", "source", "root_id", "component_id", "match_json", "blueprint",
	"kickoff", "max_runs_per_hour", "max_cost_per_day_usd", "enabled",
	"disabled_reason", "from_outbox_id", "created_by_kind", "created_by_id",
	"created_at", "updated_at",
}

// colsTriggerSchedule: secret_env is credential-shaped and NOT declared.
var colsTriggerSchedule = []string{
	"id", "kind", "blueprint", "expr", "zone", "window_start", "window_end",
	"window_days", "paused", "covered_until", "created_at", "workspace_root",
	"goal", "event_filter",
}

// colsEpic: the epic's budget column is deliberately NOT declared — S55 T8
// (LC13): after the T6 backfill it is dead and the budget row (scope='epic',
// window='lifetime') is the single truth; a second reader would be a second
// truth, and a copy of the dead value in an export is exactly that.
var colsEpic = []string{
	"id", "component_id", "title", "objective", "owner", "target_date",
	"source_step", "created_at", "updated_at",
}

var colsGoal = []string{
	"id", "component_id", "epic_id", "version", "title", "type", "body_note",
	"source_kind", "source_ref", "source_loop", "source_step", "review_state",
	"created_by_kind", "created_by_id", "created_at", "updated_at",
}

var colsBrief = []string{
	"id", "loop_slug", "component_root", "goal_id", "goal_version", "version",
	"scope", "out_of_scope", "acceptance", "constraints", "test_plan",
	"review_state", "source_step", "created_at", "updated_at",
}

var colsSetting = []string{"key", "value", "updated_at", "updated_by"}

var colsLearning = []string{
	"id", "scope", "scope_key", "body", "provenance", "observed_count",
	"source_loop_slug", "source_step_id", "created_at",
}

// DeclaredColumns is the closed allow-list of exportable columns, per table,
// declared next to the queries that enforce it. A read may name nothing else:
// CheckSpec refuses the rest, and the engine (internal/export) builds its
// exporter reads from this map, so a set can never read what the store does
// not declare.
var DeclaredColumns = map[string][]string{
	"agent":            colsAgent,
	"policy":           colsPolicy,
	"route":            colsRoute,
	"component":        colsComponent,
	"component_root":   colsComponentRoot,
	"event_trigger":    colsEventTrigger,
	"trigger_schedule": colsTriggerSchedule,
	"epic":             colsEpic,
	"goal":             colsGoal,
	"brief":            colsBrief,
	"setting":          colsSetting,
	"learning":         colsLearning,
}

// IsExportTable reports whether name is one of the closed export tables. A
// reader that ever answered for any other table — sqlite_master, loop, step,
// a future table — would be a door this slice never built.
func IsExportTable(name string) bool {
	_, ok := DeclaredColumns[name]
	return ok
}

// ErrColumnsMissing is the seam's sentinel for "the schema does not have this
// declared column (yet)": the set is unavailable (slice not built), not an
// error. The store raises it; the engine maps it (internal/export).
var ErrColumnsMissing = errors.New("export: the schema is missing a declared column — the set is unavailable, not an error")

// CheckSpec refuses a spec that names an identifier the allow-lists do not
// declare. This is the "no caller-derived identifiers" rule (T8): a spec built
// from user input — a table, a column, an order key — fails here, before any
// SQL is assembled, and the refusal names the identifier.
func CheckSpec(spec TableSpec) error {
	if !IsExportTable(spec.Table) {
		return fmt.Errorf("export: table %q is not one of the closed export tables %v — the read would name an identifier no exporter declared", spec.Table, sortedTableNames())
	}
	if len(spec.Columns) == 0 {
		return fmt.Errorf("export: the read of %s declares no columns", spec.Table)
	}
	declared := map[string]bool{}
	for _, c := range DeclaredColumns[spec.Table] {
		declared[c] = true
	}
	seen := map[string]bool{}
	for _, c := range spec.Columns {
		if seen[c] {
			return fmt.Errorf("export: the read of %s declares column %q twice", spec.Table, c)
		}
		seen[c] = true
		if !declared[c] {
			return fmt.Errorf("export: column %q of %s is not a declared export column — the allow-list is %v, and an undeclared column is never selected", c, spec.Table, DeclaredColumns[spec.Table])
		}
	}
	if spec.Where != "" {
		return fmt.Errorf("export: the read of %s carries a row predicate %q — this slice supports no predicate, because a predicate is a selection rule and selection rules live in the allow-list", spec.Table, spec.Where)
	}
	if spec.OrderBy != "" {
		key := spec.OrderBy
		if strings.HasSuffix(key, " DESC") {
			key = strings.TrimSuffix(key, " DESC")
		}
		if !declared[key] {
			return fmt.Errorf("export: the read of %s orders by %q, which is not one of its declared columns", spec.Table, spec.OrderBy)
		}
		if spec.OrderBy != key && spec.OrderBy != key+" DESC" {
			return fmt.Errorf("export: the read of %s orders by %q — the shape is a declared column, optionally suffixed \" DESC\"", spec.Table, spec.OrderBy)
		}
	}
	return nil
}

func sortedTableNames() []string {
	names := make([]string, 0, len(DeclaredColumns))
	for name := range DeclaredColumns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TableExists answers "is this table in the schema" for the closed export set
// and for nothing else. Anything that is not one of the twelve declared
// export tables — loop, step, sqlite_master, a table a future slice adds — is
// answered false without a probe: a TableExists that ever answered for an
// undeclared table would be a door this slice never built, and the probe is
// the only thing that would make it.
func (s *Store) TableExists(name string) bool {
	if !IsExportTable(name) {
		return false
	}
	var n int
	// The placeholder binds the name; the gate above already refused every
	// name outside the closed set, so the probe can only name a table the
	// export may read.
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// ExportRows reads one declared spec: the table's declared columns, in spec
// order, in the spec's order. Every identifier in the query string is one the
// allow-lists already named — CheckSpec on the spec, and quoteIdent on a
// closed-set name — so no caller input reaches the SQL as an identifier.
//
// The result rows are maps, column name to value, with the driver's scalars
// normalized to the JSON-safe set the export encodes: int64, float64, string,
// bool, nil — and []byte read as string (the database's text, in bytes).
func (s *Store) ExportRows(ctx context.Context, spec TableSpec) ([]map[string]any, error) {
	if err := CheckSpec(spec); err != nil {
		return nil, err
	}
	cols, exists, err := s.exportTableColumns(spec.Table)
	if err != nil {
		return nil, err
	}
	if !exists {
		// A table that is not in the schema is the same state as a missing
		// column: the slice that builds it has not landed, and the set is
		// unavailable (slice not built) — not an error (BK14).
		return nil, fmt.Errorf("%w: the table %s is not in the schema", ErrColumnsMissing, spec.Table)
	}
	for _, c := range spec.Columns {
		if !cols[c] {
			return nil, fmt.Errorf("%w: the table %s is missing the declared column %s", ErrColumnsMissing, spec.Table, c)
		}
	}

	q := "SELECT " + quotedColumnList(spec.Columns) + " FROM " + quoteIdent(spec.Table)
	if spec.OrderBy != "" {
		key := spec.OrderBy
		desc := false
		if strings.HasSuffix(key, " DESC") {
			key = strings.TrimSuffix(key, " DESC")
			desc = true
		}
		q += " ORDER BY " + quoteIdent(key)
		if desc {
			q += " DESC"
		}
	}
	rows, err := s.r.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store: export read of %s: %w", spec.Table, err)
	}
	defer func() { _ = rows.Close() }()

	var out []map[string]any
	for rows.Next() {
		// One any per column, scanned through its address: the driver writes
		// each cell into the any, and normalizeExportCell names the shapes it
		// will not encode.
		vals := make([]any, len(spec.Columns))
		dests := make([]any, len(spec.Columns))
		for i := range vals {
			dests[i] = &vals[i]
		}
		if err := rows.Scan(dests...); err != nil {
			return nil, fmt.Errorf("store: export read of %s: %w", spec.Table, err)
		}
		row := make(map[string]any, len(spec.Columns))
		for i, c := range spec.Columns {
			v, err := normalizeExportCell(c, spec.Table, vals[i])
			if err != nil {
				return nil, err
			}
			row[c] = v
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// exportTableColumns lists the table's real columns through pragma_table_info.
// The table name is already closed-set by CheckSpec, so the pragma argument
// can name only a table the export may read.
func (s *Store) exportTableColumns(table string) (map[string]bool, bool, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
		return nil, false, fmt.Errorf("store: export probe of %s: %w", table, err)
	}
	if n == 0 {
		return nil, false, nil
	}
	cols := map[string]bool{}
	// pragma_table_info takes the name as its argument; the closed set makes
	// the quoted literal safe, and it is the shape fingerprint.go already uses.
	rows, err := s.r.Query(fmt.Sprintf("SELECT name FROM pragma_table_info(%q)", table))
	if err != nil {
		return nil, false, fmt.Errorf("store: export columns of %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, false, fmt.Errorf("store: export columns of %s: %w", table, err)
		}
		cols[name] = true
	}
	return cols, true, rows.Err()
}

// quotedColumnList renders the spec's columns as a quoted, comma-joined list.
func quotedColumnList(cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = quoteIdent(c)
	}
	return strings.Join(parts, ", ")
}

// normalizeExportCell maps a driver cell to the JSON-safe set the export
// encodes. A cell in a shape this list does not name is an error that says
// what it found — not a guess, because a guess is a byte the determinism claim
// does not cover.
func normalizeExportCell(col, table string, v any) (any, error) {
	switch n := v.(type) {
	case nil:
		return nil, nil
	case int64:
		return n, nil
	case float64:
		return n, nil
	case string:
		return n, nil
	case bool:
		return n, nil
	case []byte:
		return string(n), nil
	default:
		// %T is the type's own name; it is diagnostic text, not SQL.
		return nil, fmt.Errorf("store: export cell %s.%s holds a %s, which the export does not encode", table, col, fmt.Sprintf("%T", n))
	}
}
