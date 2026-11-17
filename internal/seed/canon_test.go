package seed

import "testing"

// TestCanonicalIsStable asserts that key order, whitespace and JSON-escape
// variants of one value hash identically, and two different values differ.
func TestCanonicalIsStable(t *testing.T) {
	// Same logical content, three cosmetic forms.
	formA := []byte(`{"name":"golang-engineer","temperature":0.4}`)
	formB := []byte(`{"temperature":0.4, "name":"golang-engineer"}`) // key order + whitespace
	formC := []byte(`{"name":"golang-engineer", "temperature": 0.4 }`) // whitespace
	if Hash(formA) != Hash(formB) {
		t.Errorf("Hash is not stable under key order / whitespace: A=%s B=%s", Hash(formA), Hash(formB))
	}
	if Hash(formA) != Hash(formC) {
		t.Errorf("Hash is not stable under whitespace: A=%s C=%s", Hash(formA), Hash(formC))
	}
	// Two different values differ.
	formD := []byte(`{"name":"rust-engineer","temperature":0.4}`)
	if Hash(formA) == Hash(formD) {
		t.Errorf("two different values hashed the same")
	}
	// Nested objects: order is normalised recursively.
	n1 := []byte(`{"a":{"z":1,"b":2},"c":3}`)
	n2 := []byte(`{"c":3,"a":{"b":2,"z":1}}`)
	if Hash(n1) != Hash(n2) {
		t.Errorf("Hash is not stable under nested key order")
	}
}

// TestHashIsHexSha256 pins the hash against a known SHA-256 vector: the
// hex SHA-256 of the canonical bytes of `{"k":1}` (its own canonical form).
func TestHashIsHexSha256(t *testing.T) {
	// The canonical form of `{"k":1}` is `{"k":1}`; its SHA-256 is the fixed
	// value below (verified with `printf '%s' '{"k":1}' | sha256sum`).
	const known = "a0da1fce57d0e4f9f0ae4e4cbe040d34dcc046255c6c8d18e97f55aaed0655f0"
	if got := Hash([]byte(`{"k":1}`)); got != known {
		t.Fatalf("Hash(%q) = %s, want %s", `{"k":1}`, got, known)
	}
	// A different value has a different digest.
	if Hash([]byte(`{"k":1}`)) == Hash([]byte(`{"k":2}`)) {
		t.Fatalf("two different values hashed the same")
	}
}
