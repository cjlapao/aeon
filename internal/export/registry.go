// Package export is the S62 T8 export engine (design BK14): a deterministic,
// read-only copy of the managed records into a plain JSON tree a person can
// review and git-diff. It holds no SQL and opens no network: every read goes
// through the Reader seam, implemented by the store (internal/store/export.go),
// so the engine and its rules are proven against fake reads, and the SQL stays
// in the one place SQL lives. There is no importer: the export is a review
// copy, and the restore path is the whole-database backup's (design BK14).
package export

import (
	"context"

	"github.com/cjlapao/aeon/internal/store"
)

// Reader is the seam to the database: an allow-listed read of one declared
// table's columns, and an existence probe. *store.Store implements it; the
// engine is tested against a fake, so the rules that live here never depend
// on a database being reachable. The spec's shape (TableSpec) is the store's,
// at the SQL boundary — the engine builds reads from store.DeclaredColumns,
// so it can never read what the store does not declare.
type Reader interface {
	// ExportRows reads the declared columns of one declared table, in the
	// declared order. A missing column is store.ErrColumnsMissing (the set is
	// reported unavailable, not an error).
	ExportRows(ctx context.Context, spec store.TableSpec) ([]map[string]any, error)
	// TableExists reports whether the named export table is in the schema.
	// A name outside the closed export table set is never probed: false.
	TableExists(name string) bool
}

// Exporter is one declared record set. Read is the set's exporter: it asks the
// Reader ONLY for the set's declared tables and columns, shapes the rows into
// the set's files, and returns them (the engine writes, orders and guards).
type Exporter struct {
	// Name is the set's closed-set name, its directory under the export
	// dir, and the --records word.
	Name string
	// Description is the person's one-liner for export.sets and the README.
	Description string
	// Tables is the closed list of tables the set reads; every one of them
	// must exist in the schema or the set is "unavailable (slice not built)".
	Tables []string
	// Optional marks the opt-in sets: memory is absent from a default export
	// and joins only on --include-memory (the loop's scratch memory is the
	// set a person most often forgets to want in a diff).
	Optional bool
	// Read produces the set's files. It must never name an identifier that
	// is not in the set's declared columns (CheckSpec refuses such a read at
	// the seam, and TestExportersReadOnlyDeclaredColumns pins it on the fake).
	Read func(ctx context.Context, r Reader) ([]File, error)
}

// registryOrder is the closed set of declared sets, in the order the
// manifests, the README and the --records help name them. A set that is not in
// this slice does not exist: a new set is a design decision (BK14), not an
// append.
var registryOrder = []string{
	"agents", "components", "goals", "memory", "policies", "routes", "settings", "triggers",
}

// All returns the declared sets in registry order.
func All() []Exporter {
	out := make([]Exporter, 0, len(registryOrder))
	for _, name := range registryOrder {
		out = append(out, registry[name])
	}
	return out
}

// Get returns one declared set by name.
func Get(name string) (Exporter, bool) {
	e, ok := registry[name]
	return e, ok
}

// Names returns the closed set's names in registry order.
func Names() []string {
	return append([]string(nil), registryOrder...)
}
