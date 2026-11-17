package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonical returns the canonical JSON bytes of v: sorted object keys, no
// insignificant whitespace, UTF-8, with the JSON escapes Go's encoder
// produces (the only escapes that survive a round-trip, so a re-encode of a
// canonical value is identical to it). The engine hashes canonical bytes,
// so two contents that differ only in key order, whitespace or escapes hash
// the same — that is what lets status say a row still equals what was
// applied (SD3). json.Marshal already sorts map keys and is compact, so
// this is json.Marshal; it is named for the contract it guarantees.
func Canonical(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Hash returns the hex SHA-256 of the canonical form of content (SD3).
// content is canonicalised first (parsed, then re-encoded through Canonical)
// so that two contents that differ only in key order, whitespace or escapes
// hash identically — a hash that could differ on a cosmetic edit would let
// a status read "diverged" for a row that is, in fact, unchanged. content
// is always valid JSON by rule 5; an unparseable input hashes its raw bytes
// so a malformed value still names itself rather than colliding.
func Hash(content []byte) string {
	v, err := jsonUnmarshal(content)
	if err != nil {
		return hexsha256(content)
	}
	c, err := Canonical(v)
	if err != nil {
		return hexsha256(content)
	}
	return hexsha256(c)
}

// validJSON reports whether content is syntactically valid JSON (rule 5).
func validJSON(content []byte) bool {
	return json.Valid(content)
}

func hexsha256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// jsonUnmarshal parses JSON into an interface{}. Numbers are kept as
// float64 (encoding/json's interface{} behaviour); for a canonical-content
// hash that is immaterial because the domain's content is already canonical
// and a re-marshal of a canonical value is identical to it.
func jsonUnmarshal(b []byte) (any, error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}
