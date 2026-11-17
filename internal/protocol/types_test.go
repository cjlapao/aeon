package protocol

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T3 · D3 — unknown fields are ignored, IN BOTH DIRECTIONS.
//
// This looks trivial and it is the single property that makes the next fifteen
// slices additive. A client that rejects an unknown result field is as broken
// as a daemon that rejects an unknown param, and only one of those is obvious —
// so both are tested.
func TestUnknownFieldInParamsIsIgnored(t *testing.T) {
	raw := []byte(`{
	  "clientName":"aeon-cli","clientVersion":"0.1.0","protocolVersion":1,
	  "somethingFromTheFuture":{"nested":[1,2,3]}
	}`)
	var p InitializeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("a request carrying an unknown field was rejected: %v", err)
	}
	if p.ClientName != "aeon-cli" || p.ProtocolVersion != 1 {
		t.Fatalf("the known fields did not survive: %+v", p)
	}
}

func TestClientAcceptsAResultCarryingAnUnknownField(t *testing.T) {
	raw := []byte(`{
	  "protocolVersion":1,"daemonVersion":"0.1.0","capabilities":["subscribe"],
	  "featureWeHaveNotWrittenYet":true
	}`)
	var r InitializeResult
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("a result carrying an unknown field was rejected: %v", err)
	}
	if r.ProtocolVersion != 1 || len(r.Capabilities) != 1 {
		t.Fatalf("the known fields did not survive: %+v", r)
	}
}

// Every type on the wire, not just the handshake: one DisallowUnknownFields
// anywhere turns additive evolution into breaking evolution, and it would be
// added in a decoder somebody wrote in a hurry.
func TestEveryWireTypeToleratesAnUnknownField(t *testing.T) {
	const extra = `"somethingFromTheFuture":"x"`
	for _, tc := range []struct {
		name string
		raw  string
		into any
	}{
		{"InitializeParams", `{"clientName":"c",` + extra + `}`, &InitializeParams{}},
		{"InitializeResult", `{"protocolVersion":1,` + extra + `}`, &InitializeResult{}},
		{"PingParams", `{` + extra + `}`, &PingParams{}},
		{"PingResult", `{` + extra + `}`, &PingResult{}},
		{"VersionParams", `{` + extra + `}`, &VersionParams{}},
		{"VersionResult", `{"daemonVersion":"d",` + extra + `}`, &VersionResult{}},
		{"SubscribeParams", `{"filter":{"kinds":["a"],` + extra + `},` + extra + `}`, &SubscribeParams{}},
		{"SubscribeResult", `{"subscriptionId":"s","from":1,` + extra + `}`, &SubscribeResult{}},
		{"UnsubscribeParams", `{"subscriptionId":"s",` + extra + `}`, &UnsubscribeParams{}},
		{"UnsubscribeResult", `{` + extra + `}`, &UnsubscribeResult{}},
		{"Event", `{"seq":1,"kind":"k","at":"t",` + extra + `}`, &Event{}},
		{"Filter", `{"componentRoot":"/r",` + extra + `}`, &Filter{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.raw), tc.into); err != nil {
				t.Fatalf("%s rejected an unknown field: %v", tc.name, err)
			}
		})
	}
}

// The mechanism, asserted directly: nothing in this package may call
// DisallowUnknownFields. Without this, the tests above pass today and somebody
// adds it tomorrow in one decoder.
func TestNothingCallsDisallowUnknownFields(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		if usesIdentifier(identifiersIn(t, file), "DisallowUnknownFields") {
			t.Errorf("%s calls DisallowUnknownFields — that turns every additive change "+
				"into a breaking one (T3)", file)
		}
	}
}

// I4 — every method's request and result type is in types.go, none inline in a
// handler, because the conformance fixtures are tested against these types and
// a type declared inside a function cannot be.
func TestEveryWireTypeIsDeclaredInTypesGo(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok {
				declared[ts.Name.Name] = true
			}
		}
	}
	for _, want := range []string{
		"InitializeParams", "InitializeResult",
		"PingParams", "PingResult",
		"VersionParams", "VersionResult",
		"SubscribeParams", "SubscribeResult",
		"UnsubscribeParams", "UnsubscribeResult",
		"Filter", "Event",
	} {
		if !declared[want] {
			t.Errorf("%s is not declared in types.go (I4)", want)
		}
	}
}

// readProtocolSource reads a file in this package, for the tests that assert a
// limitation or a guess is documented in the source itself.
func readProtocolSource(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
