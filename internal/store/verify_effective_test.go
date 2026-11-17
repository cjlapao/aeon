package store

// verify_effective_test.go — S57 T3's VG3 tests: the effective set is ONE
// level, replace-not-merge. A child with its own live gates runs exactly
// those; a child with none runs its root's; an archived node — child or
// root — supplies nothing and takes nothing.

import (
	"errors"
	"testing"
)

// TestEffectiveGatesOwnReplacesRoot — the core of VG3: a child's own rows
// REPLACE the root's, never merge with them; archived and disabled rows of
// the child do not count as "own" and fall through to the root.
func TestEffectiveGatesOwnReplacesRoot(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "ef-root")
	s57SeedChild(t, g, "ef-child-own", "ef-root")
	s57SeedChild(t, g, "ef-child-none", "ef-root")
	s57SeedChild(t, g, "ef-child-dead", "ef-root")

	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "ef-root", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 300})
	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "ef-root", Name: "test", Command: "go test ./...", Requirement: "advisory", TimeoutS: 300})
	// The child's own single gate, and a disabled + an archived one that
	// must not count as "own" on ef-child-dead.
	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "ef-child-own", Name: "lint", Command: "golangci-lint run", Requirement: "required", TimeoutS: 120})
	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "ef-child-dead", Name: "lint", Command: "golangci-lint run", Requirement: "required", TimeoutS: 120, Enabled: s57ptr(false)})
	dead, err := s.AddVerifyCommand(s57User, NewVerifyCommand{ComponentID: "ef-child-dead", Name: "arch", Command: "make check", Requirement: "advisory", TimeoutS: 60})
	if err != nil || !dead.OK() {
		t.Fatalf("seed the archivable gate: %v %v", dead, err)
	}
	if w, err := s.ArchiveVerifyCommand(s57User, dead.Command.ID); err != nil || !w.OK() {
		t.Fatalf("archive the seed gate: %v %v", w, err)
	}

	// Own replaces root: the child sees exactly its own gate, not the
	// root's two.
	eg, err := s.EffectiveGates("ef-child-own")
	if err != nil {
		t.Fatalf("effective of child: %v", err)
	}
	if eg.Source != "own" || eg.SourceID != "ef-child-own" || len(eg.Gates) != 1 || eg.Gates[0].Command.Name != "lint" {
		t.Fatalf("child with own rows: source %s/%s, %d gates — want only its own lint", eg.Source, eg.SourceID, len(eg.Gates))
	}
	for _, gate := range eg.Gates {
		if gate.Source != "own" {
			t.Errorf("own gate carries source %q", gate.Source)
		}
	}

	// No own rows: the child inherits the root's two, source root.
	eg, err = s.EffectiveGates("ef-child-none")
	if err != nil {
		t.Fatalf("effective of bare child: %v", err)
	}
	if eg.Source != "root" || eg.SourceID != "ef-root" || len(eg.Gates) != 2 {
		t.Fatalf("bare child: source %s/%s, %d gates — want the root's two", eg.Source, eg.SourceID, len(eg.Gates))
	}
	names := map[string]bool{}
	for _, gate := range eg.Gates {
		if gate.Source != "root" {
			t.Errorf("inherited gate carries source %q", gate.Source)
		}
		names[gate.Command.Name] = true
	}
	if !names["build"] || !names["test"] {
		t.Errorf("inherited gates %v, want build and test", names)
	}

	// Archived + disabled own rows do not count as own: the fall-through
	// lands on the root.
	eg, err = s.EffectiveGates("ef-child-dead")
	if err != nil {
		t.Fatalf("effective of dead child: %v", err)
	}
	if eg.Source != "root" || eg.SourceID != "ef-root" || len(eg.Gates) != 2 {
		t.Fatalf("child with only archived/disabled rows: source %s/%s, %d gates — want the root's two", eg.Source, eg.SourceID, len(eg.Gates))
	}

	// An archived child takes nothing (its own lint would otherwise show).
	s57ArchiveComponent(t, g, "ef-child-own")
	eg, err = s.EffectiveGates("ef-child-own")
	if err != nil {
		t.Fatalf("effective of archived child: %v", err)
	}
	if len(eg.Gates) != 0 || eg.Source != "" {
		t.Fatalf("archived child: %d gates source %q — want the empty set", len(eg.Gates), eg.Source)
	}

	// An archived root supplies nothing to its children.
	seedComponent(t, g, "ef-root2")
	s57SeedChild(t, g, "ef-child2", "ef-root2")
	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "ef-root2", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 300})
	s57ArchiveComponent(t, g, "ef-root2")
	eg, err = s.EffectiveGates("ef-child2")
	if err != nil {
		t.Fatalf("effective under archived root: %v", err)
	}
	if len(eg.Gates) != 0 || eg.Source != "" {
		t.Fatalf("child under archived root: %d gates source %q — want the empty set", len(eg.Gates), eg.Source)
	}
}

// TestEffectiveGatesOnARootAndAStandalone — the tree edge cases: a root that
// has children still resolves to its own rows (children are not its
// parents); a standalone node with no parent resolves own, or the empty set
// when it declares nothing (an empty answer, not an error); only an unknown
// component is an error.
func TestEffectiveGatesOnARootAndAStandalone(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "st-root")
	s57SeedChild(t, g, "st-child", "st-root")
	seedComponent(t, g, "st-alone-gates")
	seedComponent(t, g, "st-alone-bare")

	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "st-root", Name: "build", Command: "go build ./...", Requirement: "required", TimeoutS: 300})
	s57Gate(t, s, s57User, NewVerifyCommand{ComponentID: "st-alone-gates", Name: "lint", Command: "golangci-lint run", Requirement: "required", TimeoutS: 120})

	// A root with children resolves its own rows.
	eg, err := s.EffectiveGates("st-root")
	if err != nil {
		t.Fatalf("effective of root: %v", err)
	}
	if eg.Source != "own" || eg.SourceID != "st-root" || len(eg.Gates) != 1 || eg.Gates[0].Command.Name != "build" {
		t.Fatalf("root: source %s/%s, %d gates — want its own build", eg.Source, eg.SourceID, len(eg.Gates))
	}

	// A standalone with its own rows resolves own.
	eg, err = s.EffectiveGates("st-alone-gates")
	if err != nil {
		t.Fatalf("effective of standalone: %v", err)
	}
	if eg.Source != "own" || eg.SourceID != "st-alone-gates" || len(eg.Gates) != 1 {
		t.Fatalf("standalone: source %s/%s, %d gates — want its own lint", eg.Source, eg.SourceID, len(eg.Gates))
	}

	// A standalone declaring nothing is an honest empty set, not an error.
	eg, err = s.EffectiveGates("st-alone-bare")
	if err != nil {
		t.Fatalf("effective of bare standalone: %v — empty is not an error", err)
	}
	if len(eg.Gates) != 0 || eg.Source != "" {
		t.Fatalf("bare standalone: %d gates source %q — want the empty set", len(eg.Gates), eg.Source)
	}

	// Only an unknown component is an error.
	if _, err := s.EffectiveGates("st-ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown component: %v — want ErrNotFound", err)
	}
}
