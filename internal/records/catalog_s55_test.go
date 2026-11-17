package records

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// S55 T10 — the seven loop-control / budget / watchdog specs, and the
// boundary the catalog keeps around them: reads and the ONE inert ask, no
// decision or cap/rule-write tool, and a package that stays pure data.

// s55Seven is the T10 surface pinned field for field: tool name, socket
// twin, family, class and the one write flag.
var s55Seven = map[string]struct {
	method, family, class string
	mutates               bool
}{
	"loop_control_state": {"loop.control", "loopcontrol", "safe", false},
	"loop_request_stop":  {"loop.requestStop", "loopcontrol", "detectable", true},
	"budget_list":        {"budget.list", "budget", "safe", false},
	"budget_show":        {"budget.show", "budget", "safe", false},
	"budget_effective":   {"budget.effective", "budget", "safe", false},
	"budget_spend":       {"spend.report", "budget", "safe", false},
	"watchdog_list":      {"watchdog.list", "watchdog", "safe", false},
}

// TestCatalogHasTheSevenLoopControlSpecs walks the catalog against the seven
// and fails on a spec that is missing, duplicated or mis-flagged.
func TestCatalogHasTheSevenLoopControlSpecs(t *testing.T) {
	seen := map[string]int{}
	for _, sp := range Catalog() {
		seen[sp.Name]++
		want, ok := s55Seven[sp.Name]
		if !ok {
			continue
		}
		if sp.Method != want.method {
			t.Errorf("%q twines %q, want %q — the tool and its door must name the same action", sp.Name, sp.Method, want.method)
		}
		if sp.Family != want.family {
			t.Errorf("%q is in family %q, want %q", sp.Name, sp.Family, want.family)
		}
		if sp.Class != want.class {
			t.Errorf("%q is class %q, want %q", sp.Name, sp.Class, want.class)
		}
		if sp.Mutates != want.mutates {
			t.Errorf("%q Mutates = %v, want %v — the request is the ONE write an agent may take (LC15)", sp.Name, sp.Mutates, want.mutates)
		}
	}
	for name := range s55Seven {
		switch seen[name] {
		case 0:
			t.Errorf("the catalog has no %q — the seven specs are the S55 T10 surface", name)
		case 1:
		default:
			t.Errorf("%q appears %d times in the catalog — a spec is one action, named once", name, seen[name])
		}
	}
}

// propNames lists a schema's property keys, sorted for a stable failure line.
func propNames(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestRequestStopSchemaCarriesNoIdentity pins the ONE write's schema:
// additionalProperties false, exactly {loop_id, reason}, only reason
// required, and no identity anywhere — an actor a model typed is a parse
// error, not an ignored field (OL3's records half).
func TestRequestStopSchemaCarriesNoIdentity(t *testing.T) {
	var sp *Spec
	for i := range Catalog() {
		if Catalog()[i].Name == "loop_request_stop" {
			sp = &Catalog()[i]
			break
		}
	}
	if sp == nil {
		t.Fatal("the catalog has no loop_request_stop")
	}
	var schema struct {
		Type            string                     `json:"type"`
		AdditionalProps bool                       `json:"additionalProperties"`
		Properties      map[string]json.RawMessage `json:"properties"`
		Required        []string                   `json:"required"`
	}
	if err := json.Unmarshal(sp.Schema, &schema); err != nil {
		t.Fatalf("the loop_request_stop schema does not parse: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("schema type = %q, want object", schema.Type)
	}
	if schema.AdditionalProps {
		t.Error("additionalProperties is true — an identity smuggled into input must be a parse error, not a shrug")
	}
	want := []string{"loop_id", "reason"}
	for _, p := range want {
		if _, ok := schema.Properties[p]; !ok {
			t.Errorf("schema has no property %q — the request takes a loop and a reason", p)
		}
	}
	if got := propNames(schema.Properties); len(got) != len(want) ||
		(len(got) == len(want) && (got[0] != want[0] || got[1] != want[1])) {
		t.Errorf("schema properties = %v, want exactly %v — nothing else has a place to land", got, want)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "reason" {
		t.Errorf("required = %v, want [\"reason\"] — a stop request without a reason is a flag nobody can act on", schema.Required)
	}
	for name := range schema.Properties {
		for _, bad := range []string{"actor", "by", "status", "kind"} {
			if strings.Contains(name, bad) {
				t.Errorf("schema property %q carries identity (%q) — identity is stamped by the step, never decoded", name, bad)
			}
		}
	}
}

// forbiddenPlaneMethods are the doors an agent must NOT have a tool for: the
// loop decisions, the budget writes and the rule writes. A spec twining to
// one of them is a lever the catalog must never hand out (OD10, LC15).
var forbiddenPlaneMethods = map[string]bool{
	"loop.stop": true, "loop.resume": true, "loop.cancel": true, "loop.raise": true,
	"loop.control.dismiss": true,
	"budget.set":           true, "budget.remove": true,
	"watchdog.set": true, "watchdog.setEnabled": true, "watchdog.remove": true,
}

// TestNoControlDecisionToolExists is the positive "no decision tool exists"
// walk: nothing twines to a forbidden door, and nothing is spelled like a
// halt except the one ask that is the whole point.
func TestNoControlDecisionToolExists(t *testing.T) {
	for _, sp := range Catalog() {
		if forbiddenPlaneMethods[sp.Method] {
			t.Errorf("catalog action %q twines to %q — a decision or a cap/rule write is a human door, never a tool", sp.Name, sp.Method)
		}
		if sp.Name == "loop_request_stop" {
			continue
		}
		for _, bad := range []string{"stop", "cancel", "resume", "raise"} {
			if strings.Contains(sp.Name, bad) {
				t.Errorf("catalog action %q is %q-shaped — only loop_request_stop may spell a halt, and it is an ask", sp.Name, bad)
			}
		}
	}
}

// TestRecordsCatalogStillImportsNothing is the purity half of "pure data":
// the package may import the standard library and nothing else. A module
// path always carries a dot in its first element (a domain name); a stdlib
// root never does — that is the test. A dependency in here is a second
// vocabulary the catalog could drift toward, which is exactly what the
// pure-table argument is against.
func TestRecordsCatalogStillImportsNothing(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		checked++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			root := path
			if i := strings.IndexByte(path, '/'); i >= 0 {
				root = path[:i]
			}
			if strings.Contains(root, ".") {
				t.Errorf("%s imports %q — the catalog is pure data and stays stdlib-only (a dependency is a second vocabulary)", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no package files found — the walk is asserting against nothing")
	}
}

// keep the ast import honest: the parser takes a FileSet and returns *ast.File.
var _ = (*ast.File)(nil)
