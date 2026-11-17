package mcp

// rows_test.go — the row structs themselves (S46 T2): the zero Rows is a
// valid level-0 configuration (MC11's "nothing configured" is a state, not
// an error), and the empty round trip stays empty rather than inventing
// servers, agents or the dangerous default of a whole core set where none
// was written.

import (
	"reflect"
	"testing"
)

func TestZeroRowsAssembleToTheLevelZeroConfig(t *testing.T) {
	c, err := Assemble(Rows{}, AssembleOptions{IncludeDisabled: true})
	if err != nil {
		t.Fatalf("zero rows refused: %v", err)
	}
	if len(c.Public) != 0 || len(c.Agents) != 0 {
		t.Errorf("zero rows assembled into %d public / %d agents — level 0 is emptiness, not a default", len(c.Public), len(c.Agents))
	}
	// And back: an empty Config disassembles to no rows at all.
	if r := Disassemble(Config{}); len(r.Servers) != 0 || len(r.Agents) != 0 {
		t.Errorf("empty Config disassembled to %+v", r)
	}
}

func TestAgentRowCoreTwoEmptiesSurviveStructCopies(t *testing.T) {
	// The structural precondition for the round-trip law's Core clause:
	// nil and a pointer-to-empty are different values in Rows (the store
	// keeps them distinct through NULL vs '[]', T1 — the caller maps one
	// onto the other and this is where the difference is allowed to live).
	none := []string{}
	r := Rows{Agents: []AgentRow{
		{AgentKey: "whole"},             // Core nil — the whole set
		{AgentKey: "none", Core: &none}, // Core &[] — none
	}}
	c, err := Assemble(r, AssembleOptions{IncludeDisabled: true})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if c.Agents["whole"].Core != nil {
		t.Errorf("whole-set core came back %v — nil must survive", c.Agents["whole"].Core)
	}
	if c.Agents["none"].Core == nil || len(c.Agents["none"].Core) != 0 {
		t.Errorf("none-core collapsed into the whole set: %v", c.Agents["none"].Core)
	}
	// Deep-equal round trip back to rows.
	back := Disassemble(c)
	if len(back.Agents) != 2 {
		t.Fatalf("back: %+v", back.Agents)
	}
	byKey := map[string]AgentRow{}
	for _, a := range back.Agents {
		byKey[a.AgentKey] = a
	}
	if a := byKey["whole"]; a.Core != nil {
		t.Errorf("whole core drifted: %+v", a)
	}
	if a := byKey["none"]; a.Core == nil || len(*a.Core) != 0 {
		t.Errorf("none core drifted: %+v", a)
	}
	if reflect.DeepEqual(byKey["whole"], byKey["none"]) {
		t.Error("the two empties deep-equal each other — they must not")
	}
}
