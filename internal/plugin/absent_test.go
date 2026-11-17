// The level-0 invariant, third time, same discipline: S22 earned it for
// retrieval, S12 reused it for tools, S14 for agent services — here it is
// for plugins.
//
//	THE WHOLE SUITE PASSES WITH NO IMPORTED AGENTS (AP11).
//
// "Passes" here has a mechanical meaning, not a mood: a package's test
// binary can only execute plugin code if some file in that package imports
// it, so the set of packages whose suites touch the layer AT ALL is exactly
// the set carrying the import — and that set must be {cmd/aeon (the seam),
// internal/plugin (the layer)}. The other twenty-four packages' suites run
// with the layer provably OFF: the binary cannot link it. `make test-race`
// is therefore already the no-imported-agents run for the whole runtime,
// and this file is what keeps that claim from rotting (one import added to
// a loop test and the OFF claim was a lie).
//
// The specific form that matters here: **the 25 first-party bodies must not
// acquire a dependency on plugin machinery.** They were written at S13 and
// they are the product's floor. If importing became load-bearing for them,
// AP1's "same object, different source" would have quietly become "everything
// is a plugin" — and the simplest deployment would need the most complex
// path. The 25 are asserted UNCHANGED with the plugin tables empty; and no
// feature flag stands in for any of it — deletability means absent state
// works, not that a switch exists.
package plugin

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/runtime"
)

const pluginImportPath = "github.com/cjlapao/aeon/internal/plugin"

func absentWalk(t *testing.T) [][2]string {
	t.Helper()
	var out [][2]string
	err := filepath.WalkDir("../../", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, [2]string{path, string(src)})
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if len(out) < 100 {
		t.Fatalf("walked %d Go files — the module root is wrong or the walk is blind", len(out))
	}
	return out
}

// TestNoUnconditionalImportOfPlugin is the grep, and it counts TEST files:
// the OFF claim is about binaries, and a test binary links what its imports
// link. cmd/aeon may import the layer (the CLI verbs and the socket read
// are the seam — deletability means the seams are FEW, not zero); nothing
// else in the module may import it at all. A runtime-side import is AP11
// broken: the daemon would stop building with the directory deleted, which
// is how "optional" quietly stops being true.
func TestNoUnconditionalImportOfPlugin(t *testing.T) {
	var runtimeImporters, seamImporters []string
	for _, f := range absentWalk(t) {
		path, src := f[0], f[1]
		if !strings.Contains(src, pluginImportPath) {
			continue
		}
		clean := filepath.ToSlash(path)
		switch {
		case strings.Contains(clean, "/cmd/aeon/"):
			seamImporters = append(seamImporters, clean)
		case strings.Contains(clean, "/internal/plugin/"):
			// the layer knows itself
		default:
			runtimeImporters = append(runtimeImporters, clean)
		}
	}
	if len(runtimeImporters) > 0 {
		t.Fatalf("the runtime imports the layer — AP11 broken: %v", runtimeImporters)
	}
	if len(seamImporters) == 0 {
		t.Fatal("no seam importer at all — the layer is not wired anywhere, which is not optional but dead")
	}
}

// TestSuitePassesWithNoImportedAgents states the suite claim reproducibly.
// No build tag, no flag, no skip: the set of packages whose test binaries
// CAN link the layer is exactly {cmd/aeon, internal/plugin}, and the
// layer's own suite runs level-0 — every test here starts from Reset(), so
// "no imported agents" is the default state under which everything else is
// asserted. Everything else (the other twenty-four) is the run that
// `make test-race` already performs with the layer unlinkable.
func TestSuitePassesWithNoImportedAgents(t *testing.T) {
	linkers := map[string]bool{}
	for _, f := range absentWalk(t) {
		path, src := f[0], f[1]
		if !strings.Contains(src, pluginImportPath) {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		dir = strings.TrimPrefix(dir, "../../")
		linkers[dir] = true
	}
	for dir := range linkers {
		if dir != "cmd/aeon" && dir != "internal/plugin" {
			t.Fatalf("package %q links the layer into its test binary — the suite no longer runs with no imported agents", dir)
		}
	}
	if !linkers["cmd/aeon"] {
		t.Fatal("the seam does not link the layer — the daemon and the CLI are unwired")
	}
	if !linkers["internal/plugin"] {
		t.Fatal("the layer's own suite is missing — the level-0 proof has nothing to prove")
	}
}

// TestThe25BodiesUnaffected runs one of the compiled bodies against S13's
// expectation WITH THE PLUGIN TABLES EMPTY — and then again with an
// imported agent present, demanding the same object back (AP1's "same
// registry" is only worth anything if the first-party half does not move
// when the imported half arrives).
func TestThe25BodiesUnaffected(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	first := handler.RegisteredBodies()
	if len(first) < 20 {
		t.Fatalf("S13's floor is the compiled bodies — %d is not the set this slice was told to leave alone", len(first))
	}
	ag := Agents()
	for _, a := range ag {
		if !a.FirstParty || a.State != "running" {
			t.Fatalf("with the tables empty, the registry must be exactly the first-party set: %+v", a)
		}
	}
	if len(ag) != len(first) {
		t.Fatalf("registry ≠ compiled bodies at level 0: %d vs %d", len(ag), len(first))
	}

	// Run ONE through the machinery S13 says it runs through: the registry's
	// handler, assembled by the same primitive plugin.Dispatch uses for
	// imported agents (AP1: one object, different source — and the source
	// here never touched a package).
	const body = "golang-engineer"
	h, ok := handler.Lookup(body)
	if !ok {
		t.Fatalf("%s is not a registered body — S13 moved without telling this test", body)
	}
	if strings.TrimSpace(h.SystemPrompt) == "" {
		t.Fatalf("S13's body arrives with its prompt — %s has none", body)
	}
	st := runtime.DefaultHandler(h.SystemPrompt, runtime.DefaultGrantNames())
	if st.Name == "" || len(st.Tools) != 3 || st.Tools[0] != "read" || st.Tools[1] != "list" || st.Tools[2] != "write" {
		t.Fatalf("S13's default dispatch shape (read/list/write, named handler): %+v", st)
	}
	// The first-party body does NOT go through the plugin door — Dispatch
	// answers imported names; the loop dispatches the compiled bodies the
	// way it always has (S13), which is the same OBJECT (AP1), not a
	// plugin-shaped copy of it.
	if _, routed := Dispatch(body); routed {
		t.Fatalf("%s routed through the plugin door — the compiled bodies are not managed by it", body)
	}

	// The same object with an imported agent present: the imported half
	// arriving must not move the first-party half — not the entry, not the
	// prompt, not the provenance.
	dir := writePackage(t, "uninvited", "", nil, nil)
	mustImport(t, dir)
	again := handler.RegisteredBodies()
	if fmt.Sprint(again) != fmt.Sprint(first) {
		t.Fatal("the compiled set changed when an import arrived")
	}
	a2, ok := Lookup(body)
	if !ok || !a2.FirstParty || a2.State != "running" {
		t.Fatalf("the first-party entry moved: %+v", a2)
	}
	// And the gate never trips over the abstraction: the 25 are refused
	// nothing for lacking a plugin row.
	if _, refused := InvocationGate()(body); refused {
		t.Fatal("refusing a compiled body for lacking a plugin row is AP1 betrayed")
	}
	// The imported agent did not shadow or join the compiled set: two
	// sources, one registry, no collision and no confusion of sources.
	if a, ok := Lookup("uninvited"); !ok || a.FirstParty {
		t.Fatalf("the import arrived as itself: %+v", a)
	}
}

// TestRegistryEmptyIsNormalState is AP11 as behaviour: zero imports is not
// an error, not a warning, not an alarm — and an unwired daemon (no ledger,
// no loader) behaves exactly as it did before this slice existed: gates
// inert, refusals about UNAVAILABLE rather than false certainties, and no
// invented permission anywhere.
func TestRegistryEmptyIsNormalState(t *testing.T) {
	t.Cleanup(func() { BindStateStore(nil); Reset() })
	BindStateStore(nil)
	Reset()
	if ag := Agents(); len(ag) == 0 || !ag[0].FirstParty {
		t.Fatalf("empty imports must still be the compiled registry: %d", len(ag))
	}
	if _, ok := Lookup("nobody-imported"); ok {
		t.Fatal("Lookup must answer absence, not manufacture it")
	}
	if _, refused := InvocationGate()("anyone"); refused {
		t.Fatal("an unwired daemon is inert (the S14 discipline): no ledger, no refusals invented")
	}
	if _, refused := ArrivalGate()("anyone", 1, nil); refused {
		t.Fatal("arrival is inert too")
	}
	if _, rj := Refusals("anyone"); rj == nil || !strings.Contains(rj.Error(), "UNAVAILABLE") {
		t.Fatalf("no ledger is UNAVAILABLE, never \"nothing was refused\" (P31's third state): %v", rj)
	}
}

// TestNoFeatureFlagWasAdded closes the door the lazy future opens: a
// PLUGIN_ENABLED env var or build tag would replace "absent state works"
// with "a switch exists" — and switches drift into defaults, and defaults
// into load-bearing. Deletability here means the tests above pass with the
// directory's code present-but-unused; nothing reads an environment to
// decide whether the invariant holds.
func TestNoFeatureFlagWasAdded(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		if strings.Contains(src, "os.Getenv") || strings.Contains(src, "LookupEnv") {
			t.Fatalf("%s reads the environment to decide behaviour — the invariant is code, not a switch (T8: no feature flag)", path)
		}
		if strings.Contains(src, "//go:build") {
			t.Fatalf("%s carries a build tag — absent state must work through absence, not compilation", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
