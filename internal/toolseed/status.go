package toolseed

import (
	"context"
	"sort"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// status.go — the **drift report** (TC11). Code is the truth about a tool; the
// row is a projection of it, and the only honest thing to do when they disagree
// is say so, in the open, naming the fields — never to quietly rewrite the row or
// to serve the code's answer while the row claims otherwise.
//
// It is a pure function over the two sides it is given, and it holds no store:
// there is no reachable path from a report to a write. `TestDriftNeverRepairs`
// pins that, and it is why the signature takes rows and records instead of a
// database — a function that could not reach a transaction cannot repair what it
// reports.

// DriftState is one tool's verdict: code and row agree, code has no row, the row
// has no code, or they differ in the named fields.
type DriftState string

const (
	// DriftCurrent is agreement — the row says what the code says.
	DriftCurrent DriftState = "current"
	// DriftMissingRow is a tool in code with no row: it was added and the set
	// has not been applied since (a start applies it; the report says so until
	// then, rather than hiding a tool that a person cannot find in the catalog).
	DriftMissingRow DriftState = "missing_row"
	// DriftRetired is a row whose tool left the code. The row is kept (SD6) and
	// the ledger flags it; a catalog that silently dropped retired tools would
	// make an audit trail point at nothing.
	DriftRetired DriftState = "retired"
	// DriftDiffers is both sides present and disagreeing, in `Fields`.
	DriftDiffers DriftState = "differs"
)

// DriftFieldNames are the compared fields, in the order they are reported. It is
// exported because T5's `tools.status` prints the vocabulary and T6's surface
// classification names it: one list, not two spellings of the same ten words.
var DriftFieldNames = []string{
	"description", "family", "schema_hash", "rerun_class", "capabilities",
	"governed", "twin", "aliases", "conditional", "notes",
}

// DriftEntry is one tool's report.
type DriftEntry struct {
	Name   string
	State  DriftState
	Fields []string // only for DriftDiffers, in DriftFieldNames' order
}

// Drift compares the code's records against the database's rows.
//
// The `ctx` is the caller's (the store reads above this function take one); the
// comparison itself touches nothing. A row absent from the code and a record
// absent from the rows are both reported — the two directions of drift are
// different problems (a code change that has not been applied, and a tool that
// left the code) and collapsing them into "out of sync" would lose which action
// fixes it.
func Drift(_ context.Context, rows []store.ToolRow, code []seed.Record) []DriftEntry {
	byName := make(map[string]store.ToolRow, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}
	byKey := make(map[string]Content, len(code))
	for _, rec := range code {
		c, err := ContentOf(rec)
		if err != nil {
			// A record this package did not produce is a bug in the caller, and
			// the honest report of a content that cannot be parsed is a differs
			// naming itself — not a skip, which would be a silent one.
			byKey[rec.Key] = Content{Name: rec.Key, Notes: "\x00 unparseable record content"}
			continue
		}
		byKey[rec.Key] = c
	}

	names := make([]string, 0, len(byName)+len(byKey))
	for n := range byName {
		names = append(names, n)
	}
	for n := range byKey {
		if _, ok := byName[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	out := make([]DriftEntry, 0, len(names))
	for _, name := range names {
		row, haveRow := byName[name]
		c, haveCode := byKey[name]
		switch {
		case haveRow && !haveCode:
			out = append(out, DriftEntry{Name: name, State: DriftRetired})
		case !haveRow && haveCode:
			out = append(out, DriftEntry{Name: name, State: DriftMissingRow})
		default:
			fields := differ(row, c)
			if len(fields) == 0 {
				out = append(out, DriftEntry{Name: name, State: DriftCurrent})
				continue
			}
			out = append(out, DriftEntry{Name: name, State: DriftDiffers, Fields: fields})
		}
	}
	return out
}

// differ names the fields a row and a record disagree about, in the declared
// order. The comparison is on the row's stored form against the record's decoded
// one — including `schema_hash` and NOT the schema text, which is the point of
// storing the hash: two spellings of one schema are the same contract, and a
// field-by-field text diff would report a cosmetic re-encode as drift.
func differ(row store.ToolRow, c Content) []string {
	var f []string
	if row.Description != c.Description {
		f = append(f, "description")
	}
	if row.Family != c.Family {
		f = append(f, "family")
	}
	if row.SchemaHash != c.SchemaHash {
		f = append(f, "schema_hash")
	}
	if row.RerunClass != c.RerunClass {
		f = append(f, "rerun_class")
	}
	if !sameJSONList(row.Capabilities, c.Capabilities) {
		f = append(f, "capabilities")
	}
	if !sameJSONList(row.Governed, c.Governed) {
		f = append(f, "governed")
	}
	if row.Twin != c.Twin {
		f = append(f, "twin")
	}
	if !sameJSONList(row.Aliases, c.Aliases) {
		f = append(f, "aliases")
	}
	if row.Conditional != c.Conditional {
		f = append(f, "conditional")
	}
	if row.Notes != c.Notes {
		f = append(f, "notes")
	}
	return f
}

// sameJSONList compares a stored JSON array against a decoded one. Order is
// significant on purpose: a capability set is a set in Go's type but the row is a
// person's reading order, and a reordering is a change worth naming rather than
// one to wave through.
func sameJSONList(stored string, want []string) bool {
	got := unmarshal(stored)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
