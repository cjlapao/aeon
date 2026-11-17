package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegeneratingProducesNoDiff(t *testing.T) {
	got, err := generate("../../internal/protocol", "../../internal/protocol/conformance")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../ui/src/proto/contract.ts")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("generated contract differs; run make protogen")
	}
}

// TestNewMethodWithoutFixtureFailsGeneration is the generator's teeth: a method
// declared in Go with no committed fixture is an error, not a silently absent
// client type. It copies the WHOLE package into a temp dir before adding the
// method, because that is what production reads — a test that mutated one file
// would be testing a path the daemon no longer takes (S59 T6: the method names
// live beside each plane's handlers, and the generator reads the package).
func TestNewMethodWithoutFixtureFailsGeneration(t *testing.T) {
	dir := t.TempDir()
	entries, err := os.ReadDir("../../internal/protocol")
	if err != nil {
		t.Fatal(err)
	}
	copied := 0
	for _, en := range entries {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".go") || strings.HasSuffix(en.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../internal/protocol", en.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, en.Name()), raw, 0600); err != nil {
			t.Fatal(err)
		}
		copied++
	}
	if copied < 20 {
		t.Fatalf("copied %d package files into the temp dir — a thin copy would pass this test by scanning almost nothing", copied)
	}
	// The new constant goes somewhere in the package, and it must still be
	// found: which file a plane declares its doors in is the plane's business.
	methodsFile := filepath.Join(dir, "method.go")
	source, err := os.ReadFile(methodsFile)
	if err != nil {
		t.Fatal(err)
	}
	changed := string(source) + "\nconst MethodMissing MethodName = \"future.missing\"\n"
	if err := os.WriteFile(methodsFile, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = generate(dir, "../../internal/protocol/conformance")
	if err == nil || !strings.Contains(err.Error(), "future.missing") {
		t.Fatalf("missing fixture should be named, got %v", err)
	}

	// And the same method declared in a PLANE file (not method.go) must be
	// found too — otherwise a plane that keeps its constants beside its
	// handlers is invisible to the app, which is the drift this change removed.
	if err := os.WriteFile(methodsFile, source, 0600); err != nil {
		t.Fatal(err)
	}
	plane := filepath.Join(dir, "environment.go")
	planeSource, err := os.ReadFile(plane)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plane, append(planeSource, []byte("\nconst MethodAlsoMissing MethodName = \"future.also-missing\"\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = generate(dir, "../../internal/protocol/conformance")
	if err == nil || !strings.Contains(err.Error(), "future.also-missing") {
		t.Fatalf("a method declared in a plane file was not seen by the generator: %v", err)
	}
}

// TestPackageScanSeesTheExecutionEnvironmentPlane pins what S59 T6 changed: the
// twenty environment/secretref doors are in the app's contract even though
// their constants live in internal/protocol/environment.go and secretref.go
// rather than in the 1,954-line method.go. Before this change the generated
// file simply had no such lines, and nothing failed.
func TestPackageScanSeesTheExecutionEnvironmentPlane(t *testing.T) {
	generated, err := generate("../../internal/protocol", "../../internal/protocol/conformance")
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, want := range []string{
		`"environment.list"`, `"environment.effective"`, `"environment.vocabulary"`, `"environment.teardown"`,
		`"secretref.list"`, `"secretref.check"`, `"secretref.create"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the contract has no %s — the plane's doors are invisible to the app", want)
		}
	}
	// A floor, measured 2026-11-03 at 293 methods (`grep -c '^  "[a-z.-]+": { params'`
	// on the generated file), re-measured the same day after the S59 merge at 323
	// (`grep -c ': { params: ' ui/src/proto/contract.ts` → 323, contract.ts 6,706
	// lines): the floor stays at the lower number on purpose, because its job is to
	// catch a disappearance, not to track every method another slice adds.
	// Re-measured 2026-11-03 again, after S59 T7's `environment.proposeRestrict` and
	// the merge with S49 T3's unified reads and S58 T5's channels plane: **344**
	// methods (`grep -c ': { params: ' ui/src/proto/contract.ts`) across **7,170**
	// lines (`wc -l`). Still a floor, still 293 — the number that would fail is the
	// one below the smallest set ever measured, not the newest count.
	// Reading the package rather than one file ADDED 68
	// methods that had fixtures and handlers and were absent from the app's
	// contract — dod, learnings, merge, mergerule, queue, roadmap, triage and
	// verify had their constants outside method.go, and the single-file reader
	// never saw them. Nothing was lost: 225 → 293, verified by diffing the
	// sorted method-name sets of the two generated files. The floor's job is to
	// make the NEXT disappearance of a plane a failure instead of a smaller
	// client type nobody noticed.
	const measuredMethods = 293
	if got := strings.Count(text, `: { params: `); got < measuredMethods {
		t.Errorf("the contract declares %d methods, below the measured floor %d — a plane has gone missing from the scan", got, measuredMethods)
	}
}

func TestGeneratorCoverageIsStated(t *testing.T) {
	generated, err := generate("../../internal/protocol", "../../internal/protocol/conformance")
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "Fields never present") {
		t.Fatal("fixture coverage limitation is not recorded")
	}
	if strings.Contains(text, "somethingFromTheFuture") || strings.Contains(text, "featureWeHaveNotWrittenYet") {
		t.Fatal("forward-compat test fields became production client types")
	}
}
