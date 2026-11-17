package agentmgr

import (
	"context"
	"strings"
	"testing"
)

// Create stores the handoff clean; a map the runtime would refuse is refused here, by name.
func TestCreateStoresAndChecksTheHandoff(t *testing.T) {
	m, s := newCreateManager(t)
	a, err := m.Create(context.Background(), createActor, CreateInput{Name: "Reporter", Handoff: map[string]string{"summary": " what was done ", "risks?": "what could go wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.AgentBySlug(a.Slug)
	if got.Handoff != `{"risks?":"what could go wrong","summary":"what was done"}` {
		t.Fatalf("handoff must be stored trimmed and sorted: %s", got.Handoff)
	}
	for name, spec := range map[string]map[string]string{
		"no purpose": {"summary": "  "},
		"bad name":   {"two words": "x"},
		"twice":      {"a": "x", "a?": "y"},
		"empty name": {"?": "x"},
	} {
		if _, err := m.Create(context.Background(), createActor, CreateInput{Name: "Bad " + name, Handoff: spec}); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
}

// Edit: none sent keeps the handoff, a map sets it, "{}" clears it, a bad one is refused and changes nothing.
func TestEditHandoffKeepsSetsClearsAndRefuses(t *testing.T) {
	m, s := newCreateManager(t)
	a, err := m.Create(context.Background(), createActor, CreateInput{Name: "Reports", Handoff: map[string]string{"summary": "what was done"}})
	if err != nil {
		t.Fatal(err)
	}
	edit := func(h string) error {
		cur, _, _ := s.AgentBySlug(a.Slug)
		cur.Handoff = h
		return m.Edit(context.Background(), createActor, a.Slug, cur, "2026-11-11T10:00:00Z")
	}
	read := func() string { got, _, _ := s.AgentBySlug(a.Slug); return got.Handoff }

	if err := edit(""); err != nil || read() != `{"summary":"what was done"}` {
		t.Fatalf("none sent must keep: %v %s", err, read())
	}
	if err := edit(`{"files":"which files changed","notes?":"anything else"}`); err != nil || read() != `{"files":"which files changed","notes?":"anything else"}` {
		t.Fatalf("a map must be set: %v %s", err, read())
	}
	if err := edit(`{}`); err != nil || read() != `{}` {
		t.Fatalf("an empty map is a choice and must clear: %v %s", err, read())
	}
	if err := edit(`{"x y":"nope"}`); err == nil || !strings.Contains(err.Error(), "x y") || read() != `{}` {
		t.Fatalf("a bad name must be refused by name and change nothing: %v %s", err, read())
	}
	if err := edit(`[1]`); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("a non-object must be refused: %v", err)
	}
}

// A copy keeps the handoff its source declares.
func TestDuplicateCarriesTheHandoff(t *testing.T) {
	m, s := newCreateManager(t)
	src, err := m.Create(context.Background(), createActor, CreateInput{Name: "Src", Handoff: map[string]string{"summary": "what was done"}})
	if err != nil {
		t.Fatal(err)
	}
	cp, err := m.Duplicate(context.Background(), createActor, src.Slug, "Cp")
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.AgentBySlug(cp.Slug); got.Handoff != `{"summary":"what was done"}` {
		t.Fatalf("the copy lost the handoff: %s", got.Handoff)
	}
}
