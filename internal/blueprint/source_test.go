package blueprint

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// source/imports/catalog tests — S48 T2 rules 5 and the cross-package pins.

func TestEmbeddedSourceNamesMatchEmbedFS(t *testing.T) {
	src := EmbeddedSource()
	names, err := src.Names()
	if err != nil {
		t.Fatal(err)
	}
	// Equal the embedded files exactly, as a SET (T3 made the order the
	// shipped literal order — the order readers listed before rows).
	entries, err := standards.ReadDir("blueprints")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, e := range entries {
		want[strings.TrimSuffix(e.Name(), ".yaml")] = true
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names %v != embed files %v", names, want)
	}
	if !reflect.DeepEqual(src.Standard(), StandardNames()) {
		t.Fatal("Standard() disagrees with StandardNames()")
	}
	// Get: verbatim bytes, missing is (false, nil), bad grammar is (false,
	// nil) — never a path probe.
	for _, n := range StandardNames() {
		raw, ok, err := src.Get(n)
		if err != nil || !ok || len(raw) == 0 {
			t.Fatalf("Get(%q): %v %v len=%d", n, err, ok, len(raw))
		}
	}
	if raw, ok, err := src.Get("no-such-blueprint"); raw != nil || ok || err != nil {
		t.Fatalf("missing name gave (%v, %v, %v)", raw, ok, err)
	}
	for _, bad := range []string{"", "A", "../etc/passwd", "a/b", "."} {
		if _, ok, err := src.Get(bad); ok || err != nil {
			t.Fatalf("name %q reached the FS (ok=%v err=%v)", bad, ok, err)
		}
	}
}

// TestBlueprintPackageImportsUnchanged — rule 5. The package's production
// import list is PINNED: T2 adds pure logic and must bring no dependency —
// no policy, no tools, no handler, and nothing new. `time` joined with the
// injected clock (lint.go); the pre-existing internal/store, internal/errs
// and internal/paths (validate.go/load.go/standard.go, recorded by T0) are
// grandfathered HERE, in the pin, so the pin is checkable rather than
// remembered.
func TestBlueprintPackageImportsUnchanged(t *testing.T) {
	want := map[string]bool{
		"bytes": true, "crypto/sha256": true, "embed": true, "fmt": true,
		"io": true, "os": true, "path/filepath": true, "reflect": true,
		"regexp": true, "sort": true, "strings": true, "text/template": true,
		"time": true,
		// sync joined with source.go's installedMu fix (S27 T11: the
		// race-detector caught the global install map under -race; a
		// mutex is a language primitive, not a dependency — the pin's
		// purpose stands: no policy, no tools, no handler).
		"sync":                                   true,
		"gopkg.in/yaml.v3":                       true,
		"github.com/cjlapao/aeon/internal/errs":  true,
		"github.com/cjlapao/aeon/internal/paths": true,
		"github.com/cjlapao/aeon/internal/store": true,
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var extra []string
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			if strings.HasSuffix(name, "_test.go") {
				continue // tests may import the world (shipped_test.go reads handler)
			}
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if !want[path] {
					extra = append(extra, name+": "+path)
				}
			}
		}
	}
	if len(extra) > 0 {
		t.Fatalf("new imports appeared (T2 promised none beyond time): %v\nthe package stays pure — policy/tools/handler stay out", extra)
	}
}

// TestEmitsToolNamesMatchTheRecordsCatalog — the emitTool table (the ONE
// entity→tool map) must name tools the records catalog actually has. This
// test lives in internal/blueprint (the import blueprint→records is legal;
// records imports nothing of ours) rather than duplicating the map.
func TestEmitsToolNamesMatchTheRecordsCatalog(t *testing.T) {
	have := map[string]bool{}
	for _, spec := range records.Catalog() {
		have[spec.Name] = true
	}
	for _, entity := range []string{"goal", "epic", "brief", "task"} {
		tool, ok := EmitTool(entity)
		if !ok {
			t.Fatalf("EmitTool(%q) missing", entity)
		}
		if !have[tool] {
			t.Fatalf("emitTool %q → %q is not a records catalog tool", entity, tool)
		}
	}
	if !have["attach"] {
		t.Fatal("the catalog has no `attach` tool — Lint's attach question would always be silence")
	}
}
