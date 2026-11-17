package forge

// Tests for the exported credential-name accessor (S59 T4, approval A2).
//
// The accessor exists only so `execenv` can refuse a secret reference that
// points at the daemon's own forge token (EV7). Its whole value is that it is
// the SAME list `DefaultTokenSource` reads, so these tests pin the two together
// rather than pinning the names twice — a test that restates the names proves
// the list has been typed twice, which is the thing that can drift.

import (
	"strings"
	"testing"
)

func TestForgeTokenEnvNamesMatchDefaultTokenSource(t *testing.T) {
	names := TokenEnvNames()
	if len(names) == 0 {
		t.Fatal("TokenEnvNames() is empty; every forge credential would become referenceable")
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" {
			t.Fatal("TokenEnvNames contains an empty name")
		}
		if seen[name] {
			t.Errorf("%s appears twice; the gate would refuse it twice and the source would read it once", name)
		}
		seen[name] = true
	}

	// Same names: set exactly one and the source must find it. If the accessor
	// drops a name, the reference gate thinks that variable is somebody else's
	// and lets a loop read the daemon's identity.
	for _, name := range names {
		t.Setenv(name, "")
		for _, other := range names {
			t.Setenv(other, "")
		}
		t.Setenv(name, "token-for-"+name)
		got, err := DefaultTokenSource()()
		if err != nil {
			t.Fatalf("%s is set and DefaultTokenSource refused: %v", name, err)
		}
		if got != "token-for-"+name {
			t.Errorf("%s: DefaultTokenSource returned %q — it is reading a different list than the "+
				"accessor publishes, which is the drift A2 exists to prevent", name, got)
		}
	}

	// Same order: the first hit wins, and a caller using this list to decide
	// precedence has to get the same answer.
	for _, name := range names {
		t.Setenv(name, "")
	}
	t.Setenv(names[len(names)-1], "last")
	t.Setenv(names[0], "first")
	got, err := DefaultTokenSource()()
	if err != nil {
		t.Fatal(err)
	}
	if got != "first" {
		t.Errorf("with both %s and %s set the source returned %q; the accessor's order is not the "+
			"source's order", names[0], names[len(names)-1], got)
	}

	// Nothing set: the refusal still names every variable, so an operator
	// reading the message learns the same list the accessor publishes.
	for _, name := range names {
		t.Setenv(name, "")
	}
	_, err = DefaultTokenSource()()
	if err == nil {
		t.Fatal("no token in the environment and still an answer")
	}
	for _, name := range names {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name %s: %v", name, err)
		}
	}

	// The accessor returns a fresh slice: a caller that sorts or appends to it
	// must not be able to change what the next caller sees — the next caller is
	// the security gate.
	a := TokenEnvNames()
	a[0] = "MUTATED"
	if TokenEnvNames()[0] == "MUTATED" {
		t.Error("TokenEnvNames hands out a shared slice")
	}
}
