package protocol

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestProtocolVersionIsAnIntStartingAtOne(t *testing.T) {
	// A typed assertion rather than a value one: the point is that it is an
	// integer and not a semantic version string (T2).
	var v int = PROTOCOL_VERSION
	if v < 1 {
		t.Fatalf("PROTOCOL_VERSION = %d, want >= 1", v)
	}
	if MinSupportedVersion > PROTOCOL_VERSION {
		t.Fatalf("MinSupportedVersion %d > PROTOCOL_VERSION %d", MinSupportedVersion, PROTOCOL_VERSION)
	}
}

// The rule is the mechanism. If it stops being documented beside the constant,
// the integer is just a number and the next person guesses.
func TestTheAdditiveRuleIsDocumentedBesideTheConstant(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "version.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var doc string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if name.Name == "PROTOCOL_VERSION" {
					if gd.Doc != nil {
						doc = gd.Doc.Text()
					}
				}
			}
		}
	}
	if doc == "" {
		t.Fatal("PROTOCOL_VERSION has no doc comment; the additive rule has to live where " +
			"somebody about to change the number is standing")
	}

	// Both columns of the rule, and the specific cases that get argued about.
	for _, want := range []string{
		"ADDITIVE", "BREAKING",
		"new method", "OPTIONAL field", "new event kind", "new capability", "new error code",
		"removing or renaming", "optional field required", "type or its meaning", "seq",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the rule beside PROTOCOL_VERSION does not mention %q", want)
		}
	}
}

func TestNegotiateRefusesUpwardNamingBothNumbers(t *testing.T) {
	_, err := Negotiate(PROTOCOL_VERSION + 1)
	if err == nil {
		t.Fatal("a client needing a higher version was not refused")
	}
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *protocol.Error, got %T", err)
	}
	if e.Code != ErrProtocolVersionUnsupported.Code {
		t.Fatalf("code = %d, want %d", e.Code, ErrProtocolVersionUnsupported.Code)
	}
	// Naming BOTH numbers is the requirement. "unsupported version" sends
	// somebody to read source to find out what IS supported.
	for _, want := range []string{fmt.Sprint(PROTOCOL_VERSION + 1), fmt.Sprint(PROTOCOL_VERSION)} {
		if !strings.Contains(e.Data.What, want) {
			t.Errorf("refusal does not name %q: %q", want, e.Data.What)
		}
	}
	if !e.Data.Fatal {
		t.Error("a version refusal must be fatal: nothing further on this connection will work")
	}
}

// THE test for this slice's versioning, and the one nothing else exercises.
//
// Refusing a client that asks for too much is the obvious half and gets
// written. Serving a client that asks for an OLDER version the daemon still
// supports is what keeps an editor extension working across a daemon upgrade —
// and a daemon that only knows how to refuse has version checking, not version
// negotiation.
//
// Driven through `negotiate` with injected bounds because min == max today, so
// through the constants this branch is unreachable and the test would skip.
func TestNegotiateServesAnOlderSupportedVersion(t *testing.T) {
	for _, tc := range []struct {
		name           string
		want, min, max int
		speak          int
		refused        bool
	}{
		{name: "older but supported is SERVED, not refused", want: 1, min: 1, max: 2, speak: 1},
		{name: "two versions back, still supported", want: 1, min: 1, max: 3, speak: 1},
		{name: "the current version", want: 2, min: 1, max: 2, speak: 2},
		{name: "newer than the daemon is refused", want: 3, min: 1, max: 2, refused: true},
		{name: "older than supported is refused", want: 1, min: 2, max: 3, refused: true},
		{name: "absent means the oldest supported", want: 0, min: 2, max: 4, speak: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			speak, err := negotiate(tc.want, tc.min, tc.max)
			if tc.refused {
				if err == nil {
					t.Fatalf("negotiate(%d, %d, %d) = %d, want a refusal", tc.want, tc.min, tc.max, speak)
				}
				e, ok := err.(*Error)
				if !ok || e.Code != ErrProtocolVersionUnsupported.Code {
					t.Fatalf("refusal was %v, want ErrProtocolVersionUnsupported", err)
				}
				// Both numbers, in every refusal — not only the upward one.
				if !strings.Contains(e.Data.What, itoa(tc.want)) || !strings.Contains(e.Data.What, itoa(tc.max)) {
					t.Errorf("refusal does not name both numbers (want %d, speaks %d): %q",
						tc.want, tc.max, e.Data.What)
				}
				return
			}
			if err != nil {
				t.Fatalf("negotiate(%d, %d, %d) was refused: %v", tc.want, tc.min, tc.max, err)
			}
			if speak != tc.speak {
				t.Fatalf("speak = %d, want %d — the daemon must answer with the version it will SPEAK",
					speak, tc.speak)
			}
		})
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// Against the real constants: the only reachable branches today.
func TestNegotiateAgainstTheRealConstants(t *testing.T) {
	speak, err := Negotiate(PROTOCOL_VERSION)
	if err != nil || speak != PROTOCOL_VERSION {
		t.Fatalf("Negotiate(%d) = %d, %v", PROTOCOL_VERSION, speak, err)
	}
	if _, err := Negotiate(PROTOCOL_VERSION + 1); err == nil {
		t.Fatal("a higher version was not refused through the real constants")
	}
}

// A client that omits the field is old or sloppy, not asking for version 0.
// Refusing it with a message about version 0 explains nothing.
func TestNegotiateTreatsZeroAsTheOldestSupported(t *testing.T) {
	speak, err := Negotiate(0)
	if err != nil {
		t.Fatalf("a missing protocolVersion was refused: %v", err)
	}
	if speak != MinSupportedVersion {
		t.Fatalf("speak = %d, want %d", speak, MinSupportedVersion)
	}
}

func TestCapabilitiesAreStrings(t *testing.T) {
	caps := Capabilities()
	if len(caps) == 0 {
		t.Fatal("no capabilities; `subscribe` is one")
	}
	found := false
	for _, c := range caps {
		if c == "subscribe" {
			found = true
		}
	}
	if !found {
		t.Errorf("`subscribe` is not advertised: %v", caps)
	}
}
