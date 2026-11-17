package tools

// canon.go is S47 T1's schema-hash helper: the SHA-256 of a tool's INPUT
// SCHEMA, used by the `tools.core` seed set (T3) to store a read-only copy of
// the shape and by `tools.status` (T5) to detect drift between the code and the
// catalog row (design TC2/TC11).
//
// # Why code owns the schema and the row only copies it
//
// The executor and the input schema are one shape: a stored schema that could
// disagree with the function it describes would be a second truth for the one
// thing that must not have two. So the row carries a display copy plus this
// hash, and a disagreement is reported rather than repaired (TC11).
//
// # One canonicalisation, not a copy of one
//
// The bytes hashed are `seed.Canonical`'s — sorted object keys, no insignificant
// whitespace, Go's JSON escapes — so two schemas that differ only in key order
// or formatting hash identically (SD3). A hash that changed on a cosmetic edit
// would make `tools.status` report drift that is not there, which is worse than
// no hash at all because people stop reading the report.

import (
	"encoding/json"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/seed"
)

// SchemaHash returns the hex SHA-256 of the canonical JSON of a tool spec's
// input schema. An absent schema hashes as JSON null, so "no parameters" is a
// value and not a blank that could collide with a malformed one.
func SchemaHash(spec model.ToolSpec) string {
	raw := spec.Parameters
	if len(raw) == 0 {
		raw = []byte("null")
	}
	return seed.Hash(raw)
}

// CanonicalSchema returns the canonical JSON bytes of a tool spec's input
// schema — the exact bytes whose hash goes into the catalog row, so the row and
// the hash cannot be produced by two different encoders.
func CanonicalSchema(spec model.ToolSpec) []byte {
	raw := spec.Parameters
	if len(raw) == 0 {
		raw = []byte("null")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Unparseable schema bytes hash their raw form (seed.Hash's own rule for
		// a malformed value: it names itself instead of colliding), and the row
		// stores them verbatim so the drift report can show what code holds.
		return append([]byte(nil), raw...)
	}
	b, err := seed.Canonical(v)
	if err != nil {
		return append([]byte(nil), raw...)
	}
	return b
}
