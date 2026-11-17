package protocol

import (
	"go/scanner"
	"go/token"
	"os"
	"strings"
	"testing"
)

// identifiersIn returns the identifiers and selectors appearing in a Go file's
// CODE, with comments and string literals excluded.
//
// # Why a tokenizer rather than grep
//
// Both of this package's invariant tests started as text searches and both
// cried wolf immediately: `import_test.go` names every forbidden import by
// definition, and `types.go`'s own doc comment explains that nothing calls
// `DisallowUnknownFields` — which a text search reads as a call to it. The
// usual repair is to weaken the pattern until it matches nothing, at which
// point the invariant is unguarded and nobody knows.
//
// Tokenizing answers the question that was actually being asked: does the CODE
// do this. Comments may discuss sockets, because prose explaining why a thing
// is forbidden is the opposite of a violation.
func identifiersIn(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))

	var s scanner.Scanner
	// Mode 0: comments are not even emitted.
	s.Init(file, src, func(_ token.Position, msg string) {
		t.Fatalf("%s: scan error: %s", path, msg)
	}, 0)

	var out []string
	var pending string // builds `pkg.Sel` across the three tokens
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.IDENT:
			if pending != "" {
				out = append(out, pending+"."+lit)
				pending = ""
			}
			out = append(out, lit)
			pending = lit
		case token.PERIOD:
			// keep `pending` so the next IDENT forms a selector
		default:
			pending = ""
		}
	}
	return out
}

func usesIdentifier(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// Proof the scanner does what this file claims — that a mention in a comment is
// not a use, and a real call is. Without this, a tokenizer that silently
// returned nothing would make every invariant test above pass vacuously.
func TestTheScannerDistinguishesCodeFromProse(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sample.go"
	const src = `package p

// This comment mentions net.Listen and DisallowUnknownFields, which is prose.
const s = "net.Dial in a string literal is not a call either"

func f() { _ = realCall() }
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ids := identifiersIn(t, path)

	for _, prose := range []string{"Listen", "DisallowUnknownFields", "Dial"} {
		if usesIdentifier(ids, prose) {
			t.Errorf("%q was read out of a comment or a string literal as code: %v", prose, ids)
		}
	}
	if !usesIdentifier(ids, "realCall") {
		t.Errorf("an actual call was not seen: %v", ids)
	}
	if !strings.Contains(strings.Join(ids, " "), "realCall") {
		t.Error("sanity: the scanner returned nothing useful")
	}
}
