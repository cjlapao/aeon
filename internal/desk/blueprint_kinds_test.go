package desk

import (
	"context"
	"strings"
	"testing"
)

// blueprint_kinds_test.go — S48 T9: the desk's PURE half of blueprint_save.
// What this file proves is shape, nothing else: a payload that is not
// {name, content, createOnly?, note?} is refused (a smuggled "mode",
// "origin" or "actor" key is a PARSE refusal — the validation mode is the
// daemon's choice and the actor is the commit door's stamp, never the
// proposer's payload), the name is non-blank and within the grammar's
// ceiling, the source is non-blank and within the 1 MiB the save door
// itself enforces (re-declared here: the envelope pin this package keeps
// from importing the writer), and trailing JSON is refused. The store
// halves (Analyze, cross-check, collision) are the seam's and their tests
// live in cmd/aeon where the seam is built.

func bpJSON(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	// Hand-built so a smuggled key can be placed in the payload exactly.
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\"" + k + "\":")
		b.WriteString(fields[k])
	}
	b.WriteString("}")
	return b.String()
}

func TestBlueprintSaveShapeRefusals(t *testing.T) {
	ok := bpJSON(map[string]string{
		"name":    `"two-step"`,
		"content": `"id: two-step\nversion: 1"`,
	})
	if err := ValidateBlueprintSave(context.Background(), []byte(ok)); err != nil {
		t.Fatalf("the well-formed payload was refused: %v", err)
	}
	for _, tc := range []struct {
		name    string
		payload string
		needle  string
	}{
		{"SmuggledMode", bpJSON(map[string]string{"name": `"a"`, "content": `"x"`, "mode": `"strict"`}), "mode"},
		{"SmuggledOrigin", bpJSON(map[string]string{"name": `"a"`, "content": `"x"`, "origin": `"system"`}), "origin"},
		{"SmuggledActor", bpJSON(map[string]string{"name": `"a"`, "content": `"x"`, "actor": `{"kind":"agent","id":"manny"}`}), "actor"},
		{"BlankName", bpJSON(map[string]string{"name": `"  "`, "content": `"x"`}), "non-blank name"},
		{"BlankContent", bpJSON(map[string]string{"name": `"a"`, "content": `""`}), "FULL proposed source"},
		{"MissingContent", bpJSON(map[string]string{"name": `"a"`}), "FULL proposed source"},
	} {
		err := ValidateBlueprintSave(context.Background(), []byte(tc.payload))
		if err == nil {
			t.Fatalf("%s: the shape validator accepted it", tc.name)
		}
		if !strings.Contains(err.Error(), tc.needle) {
			t.Fatalf("%s: the refusal does not name %q: %v", tc.name, tc.needle, err)
		}
	}
}

func TestBlueprintSaveShapeSizeBounds(t *testing.T) {
	long := bpJSON(map[string]string{"name": `"a"`, "content": `"` + strings.Repeat("x", MaxBlueprintSourceBytes+1) + `"`})
	if err := ValidateBlueprintSave(context.Background(), []byte(long)); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("a source over 1 MiB must be refused by the envelope: %v", err)
	}
	// The envelope counts the WHOLE payload, so a smuggled-key payload
	// padded past it is refused on size before it can even be parsed —
	// the bound is the payload's own, not a post-parse check.
	pad := `"` + strings.Repeat("y", MaxBlueprintSourceBytes+blueprintEnvelopeBytes+1) + `"`
	if err := ValidateBlueprintSave(context.Background(), []byte(bpJSON(map[string]string{"name": `"a"`, "content": pad}))); err == nil || !strings.Contains(err.Error(), "envelope") {
		t.Fatalf("a payload past the envelope must be refused: %v", err)
	}
	// The name ceiling is the grammar's (≤ 64), stated in the refusal.
	if err := ValidateBlueprintSave(context.Background(), []byte(bpJSON(map[string]string{"name": `"` + strings.Repeat("a", 65) + `"`, "content": `"x"`}))); err == nil || !strings.Contains(err.Error(), "grammar") {
		t.Fatalf("a name past the grammar ceiling must be refused: %v", err)
	}
}

func TestBlueprintSaveTrailingJSONRefused(t *testing.T) {
	good := bpJSON(map[string]string{"name": `"a"`, "content": `"x"`})
	if err := ValidateBlueprintSave(context.Background(), []byte(good+good)); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("two payloads glued together must be refused as trailing JSON: %v", err)
	}
}

// TestBlueprintSaveRegistryEntryExists pins the registry half the seams
// ride: the kind resolves to an entry that VALIDATES (the built-in shape
// half answers through the same door the seams complete), is INCOMPLETE
// without the cmd/aeon seam (Effect ok=false), and `Missing` names the two
// halves nobody supplied — so an unwired daemon refuses to commit the kind
// by name, never applies nothing and calls it committed.
func TestBlueprintSaveRegistryEntryExists(t *testing.T) {
	r := testRegistry(&spyBoundary{}, Seams{})
	if _, ok := r.Effect(string(KindBlueprintSave)); ok {
		t.Fatal("blueprint_save reads complete WITHOUT the seam — the store halves are the seam's")
	}
	miss := r.Missing(string(KindBlueprintSave))
	for _, half := range []string{"AlreadyApplied", "Apply"} {
		if !strings.Contains(miss, half) {
			t.Fatalf("Missing does not name the %s half: %q", half, miss)
		}
	}
	if err := r.ValidateProposal(context.Background(), string(KindBlueprintSave), `{"name":"a","content":"x"}`); err != nil {
		t.Fatalf("the shape half must answer through the registry: %v", err)
	}
	if err := r.ValidateProposal(context.Background(), string(KindBlueprintSave), `{"name":"a","content":"x","mode":"strict"}`); err == nil {
		t.Fatal("the registry door accepted a smuggled mode")
	}
}
