package seed

import (
	"strings"
	"testing"
)

// validSet is the one baseline a subtest mutates to break a single rule.
func validSet() Set {
	return Set{
		ID:      "agents.firstparty",
		Class:   ClassInert,
		Version: 1,
		Records: func() []Record {
			return []Record{{Key: "golang-engineer", Version: 1, Content: []byte(`{"name":"golang-engineer"}`)}}
		},
	}
}

// TestSetIDGrammarTable asserts the ID grammar on a table of valid and
// invalid IDs (rule 1).
func TestSetIDGrammarTable(t *testing.T) {
	cases := []struct {
		id   string
		ok   bool
	}{
		{"agents.firstparty", true},
		{"policy.corpus", true},
		{"mcp.servers", true},
		{"blueprints.standard", true},
		{"ab", false}, // shorter than 3 bytes
		{"a", false},
		{"-leading", false}, // must start with a letter
		{"1leading", false}, // must start with a letter
		{"Upper.Case", false}, // first segment must be lowercase
		{"a.b.c", true}, // three segments
		{"a.1b", false}, // a later segment must start with a letter
		{"a_b", false}, // underscore is not allowed
		{"agents.firstParty", true}, // a later segment may contain uppercase
		{"a.B", false}, // a later segment must START lowercase
		{strings.Repeat("a", 65) + ".b", false}, // longer than 64 bytes
		{"agents.firstparty.tool", true},
	}
	for _, c := range cases {
		s := validSet()
		s.ID = c.id
		_, err := NewRegistry([]Set{s}, []Applier{newFakeApplier(s.ID, false)})
		if c.ok {
			if err != nil {
				t.Errorf("set id %q: expected valid, got %v", c.id, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("set id %q: expected a refusal, got nil", c.id)
		}
	}
}

// TestRegistryRefusesEachDeclarationRule is one subtest per rule 1-7; each
// breaks a single rule and asserts the refusal names it.
func TestRegistryRefusesEachDeclarationRule(t *testing.T) {
	cases := map[string]func(Set) Set{
		"rule1 bad id":         func(s Set) Set { s.ID = "Bad Id"; return s },
		"rule2 bad class":      func(s Set) Set { s.Class = "maybe"; return s },
		"rule3 set version 0":  func(s Set) Set { s.Version = 0; return s },
		"rule3 record v 0":     func(s Set) Set { s.Records = func() []Record { return []Record{{Key: "k", Version: 0, Content: []byte(`{}`)}} }; return s },
		"rule3 record > set":   func(s Set) Set { s.Records = func() []Record { return []Record{{Key: "k", Version: 2, Content: []byte(`{}`)}} }; return s },
		"rule4 nil records":    func(s Set) Set { s.Records = nil; return s },
		"rule4 empty key":      func(s Set) Set { s.Records = func() []Record { return []Record{{Key: "", Version: 1, Content: []byte(`{}`)}} }; return s },
		"rule4 dup key":        func(s Set) Set { s.Records = func() []Record { return []Record{{Key: "k", Version: 1, Content: []byte(`{}`)}, {Key: "k", Version: 1, Content: []byte(`{}`)}} }; return s },
		"rule5 bad json":       func(s Set) Set { s.Records = func() []Record { return []Record{{Key: "k", Version: 1, Content: []byte(`not json`)}} }; return s },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := mutate(validSet())
			_, err := NewRegistry([]Set{s}, []Applier{newFakeApplier(s.ID, false)})
			if err == nil {
				t.Fatalf("%s: expected a refusal, got nil", name)
			}
			if !strings.Contains(err.Error(), s.ID) {
				t.Fatalf("%s: the refusal did not name the set: %v", name, err)
			}
		})
	}
	t.Run("rule6 inert but guard-affecting", func(t *testing.T) {
		s := validSet() // inert
		_, err := NewRegistry([]Set{s}, []Applier{newFakeApplier(s.ID, true)})
		if err == nil {
			t.Fatalf("rule6: an inert set with a guard-affecting applier was accepted")
		}
		if !strings.Contains(err.Error(), "guard-affecting") {
			t.Fatalf("rule6: the refusal did not name the guard-affecting applier: %v", err)
		}
	})
	t.Run("rule7 set with no applier", func(t *testing.T) {
		s := validSet()
		_, err := NewRegistry([]Set{s}, nil)
		if err == nil {
			t.Fatalf("rule7: a set with no applier was accepted")
		}
	})
	t.Run("duplicate set id", func(t *testing.T) {
		_, err := NewRegistry([]Set{validSet(), validSet()}, []Applier{newFakeApplier("agents.firstparty", false)})
		if err == nil {
			t.Fatalf("duplicate set id was accepted")
		}
	})
}

// TestClassApplierCrossCheck is the SD5 headline: an inert set whose applier
// is guard-affecting is refused, naming both.
func TestClassApplierCrossCheck(t *testing.T) {
	inert := validSet()
	guard := Set{
		ID:      "policy.corpus",
		Class:   ClassGuard,
		Version: 1,
		Records: func() []Record { return []Record{{Key: "k", Version: 1, Content: []byte(`{}`)}} },
	}
	// A guard set with a guard-affecting applier is fine.
	if _, err := NewRegistry([]Set{guard}, []Applier{newFakeApplier("policy.corpus", true)}); err != nil {
		t.Fatalf("a guard set with a guard-affecting applier was refused: %v", err)
	}
	// An inert set with a guard-affecting applier is refused, naming both.
	_, err := NewRegistry([]Set{inert}, []Applier{newFakeApplier("agents.firstparty", true)})
	if err == nil {
		t.Fatalf("an inert set with a guard-affecting applier was accepted (SD5)")
	}
}

// TestDefaultRegistryHasOnlyDeclaredSets asserts the default registry holds
// exactly the sets a domain has registered. At T1 no domain is registered,
// so it is empty; T4's agentseed init() flips it to exactly
// agents.firstparty.
func TestDefaultRegistryHasOnlyDeclaredSets(t *testing.T) {
	reg := DefaultRegistry()
	if reg.Len() != 0 {
		t.Fatalf("default registry at T1 must be empty, has %d sets", reg.Len())
	}
}
