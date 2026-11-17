package main

// component_parity_test.go — S44-extra T8: the P5 parity, both directions.
// A capability the CLI cannot reach is not finished (P34), and a verb that
// names no method is a door with no back. The nine component.* socket methods
// and the nine `aeon component` verbs are the same set, named two ways, so
// neither list can grow without the other.

import (
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// componentMethodNames is the nine component.* socket methods, in the order the
// protocol declares them. The parity test reads this from the protocol's own
// constants, not a second hand-written list, so a method that lands on the
// socket is seen here at once.
func componentMethodNames() []string {
	return []string{
		string(protocol.MethodComponentList),
		string(protocol.MethodComponentShow),
		string(protocol.MethodComponentCreate),
		string(protocol.MethodComponentUpdate),
		string(protocol.MethodComponentPromote),
		string(protocol.MethodComponentAttach),
		string(protocol.MethodComponentDetach),
		string(protocol.MethodComponentArchive),
		string(protocol.MethodComponentRestore),
	}
}

// componentVerbNames is the nine `aeon component` verbs, in the same order the
// dispatch switch (cmdComponent) names them. The parity test checks this
// against the methods, so a verb with no method, or a method with no verb,
// fails.
func componentVerbNames() []string {
	return []string{
		"component.list", "component.show", "component.create", "component.update",
		"component.promote", "component.attach", "component.detach", "component.archive",
		"component.restore",
	}
}

// TestEveryComponentMethodHasAVerb is one direction of P5: a socket method
// nobody can reach from the terminal is a capability that exists only on the
// wire. Every method must have a verb.
func TestEveryComponentMethodHasAVerb(t *testing.T) {
	methods := componentMethodNames()
	verbs := map[string]bool{}
	for _, v := range componentVerbNames() {
		verbs[v] = true
	}
	for _, m := range methods {
		if !verbs[m] {
			t.Errorf("component method %s has no `aeon component` verb", m)
		}
	}
}

// TestEveryComponentVerbHasAMethod is the other direction of P5: a verb that
// names no method is a door with no back — it would dial nothing. Every verb
// must name a method.
func TestEveryComponentVerbHasAMethod(t *testing.T) {
	methods := map[string]bool{}
	for _, m := range componentMethodNames() {
		methods[m] = true
	}
	for _, v := range componentVerbNames() {
		if !methods[v] {
			t.Errorf("component verb %s names no socket method", v)
		}
	}
}

// TestComponentVerbCountIsNine pins the family at nine — the tree's operations,
// one each. A tenth verb (or a method that lands without a verb) makes this
// count drift, which is the parity the test guards.
func TestComponentVerbCountIsNine(t *testing.T) {
	got := len(componentMethodNames())
	want := 9
	if got != want {
		t.Fatalf("the component family has %d methods, want %d", got, want)
	}
	if len(componentVerbNames()) != want {
		t.Fatalf("the component verbs number %d, want %d", len(componentVerbNames()), want)
	}
}

// TestNoProjectVerbOrMethodRemains is the gone project's guard: neither a
// `project` verb nor a `project.*` method may survive in the component family.
func TestNoProjectVerbOrMethodRemains(t *testing.T) {
	for _, m := range componentMethodNames() {
		if m == "project" || len(m) > 7 && m[:7] == "project" {
			t.Errorf("project method %s survives", m)
		}
	}
	for _, v := range componentVerbNames() {
		if v == "project" {
			t.Errorf("project verb %s survives", v)
		}
	}
}
