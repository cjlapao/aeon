package desk

// roadmap_kinds_test.go — S50 T5: the desk's PURE half of roadmap_idea, the
// fifteenth kind. Shape, nothing else: a payload that is not
// {componentRoot, title, epic?, note} is refused (a smuggled "status",
// "priorityState", "estimate", "provenance" or "actor" key is a PARSE
// refusal — an idea that could name the rung it wants is a proposal wearing
// a write, and the actor is the commit door's stamp, never the proposer's
// payload), the root and title are non-blank, the note is REQUIRED (RM8's
// ask-a-person rule: the sentence a person reads before deciding is the ask
// itself), and the bounds — root 512, title 300, epic 200, note 1024 — hold
// (the columns carry no length CHECK; a payload the desk reads is bounded
// by the desk). The store halves (the root is a component, the identical
// OPEN item does not already stand) are the seam's and their tests live in
// cmd/aeon where the seam is built.

import (
	"context"
	"strings"
	"testing"
)

func ideaJSON(fields map[string]string) string {
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
		b.WriteString("\"" + k + "\":\"" + fields[k] + "\"")
	}
	b.WriteString("}")
	return b.String()
}

func goodIdea() map[string]string {
	return map[string]string{
		"componentRoot": "/srv/aeon",
		"title":         "carry the roadmap health line into the doctor report",
		"note":          "the health answer exists on the socket and doctor never reads it",
	}
}

func TestRoadmapIdeaShapeRefusals(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"blank root", ideaJSON(map[string]string{"componentRoot": "  ", "title": "t", "note": "n"}), "names the project root"},
		{"blank title", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "   ", "note": "n"}), "needs a title"},
		{"blank note", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "   "}), "needs a note"},
		{"missing note", `{"componentRoot":"/srv/x","title":"t"}`, "needs a note"},
		{"missing title", `{"componentRoot":"/srv/x","note":"n"}`, "needs a title"},
		{"missing root", `{"title":"t","note":"n"}`, "names the project root"},
		{"empty object", `{}`, "names the project root"},
		{"smuggled status", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "status": "ready"}), "unknown field"},
		{"smuggled priorityState", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "priorityState": "essential"}), "unknown field"},
		{"smuggled estimate", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "estimate": "small"}), "unknown field"},
		{"smuggled provenance", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "provenance": "imported"}), "unknown field"},
		{"smuggled actor", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "actor": "u1"}), "unknown field"},
		{"smuggled kind", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "kind": "roadmap_update"}), "unknown field"},
		{"smuggled decidedBy", ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "decidedBy": "u1"}), "unknown field"},
		{"not JSON", `componentRoot=/srv/x`, "not {\"componentRoot\""},
		{"wrong type", `{"componentRoot":7,"title":"t","note":"n"}`, "not {\"componentRoot\""},
	}
	for _, c := range cases {
		err := validateRoadmapIdeaPayload(context.Background(), []byte(c.payload))
		if err == nil {
			t.Errorf("%s: accepted — %s", c.name, c.payload)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: refusal %q does not name %q", c.name, err.Error(), c.want)
		}
		if !strings.HasPrefix(err.Error(), "refused: ") {
			t.Errorf("%s: refusal does not start with the house word: %q", c.name, err.Error())
		}
	}

	// The accepts: the whole shape, and the shape without the one optional
	// tag (RM8: only `epic` is optional — note is required and its own
	// ceiling).
	good := goodIdea()
	if err := validateRoadmapIdeaPayload(context.Background(), []byte(ideaJSON(good))); err != nil {
		t.Errorf("the plain ask refused: %v", err)
	}
	good["epic"] = "roadmap honesty"
	if err := validateRoadmapIdeaPayload(context.Background(), []byte(ideaJSON(good))); err != nil {
		t.Errorf("the ask with its epic tag refused: %v", err)
	}
}

func TestRoadmapIdeaShapeSizeBounds(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		over    int
		under   int
		message string
	}{
		{"root", "componentRoot", maxRoadmapIdeaRootBytes + 1, maxRoadmapIdeaRootBytes, "root is a path"},
		{"title", "title", maxRoadmapIdeaTitleBytes + 1, maxRoadmapIdeaTitleBytes, "board renders titles"},
		{"epic", "epic", maxRoadmapIdeaEpicBytes + 1, maxRoadmapIdeaEpicBytes, "epic is a tag"},
		{"note", "note", maxRoadmapIdeaNoteBytes + 1, maxRoadmapIdeaNoteBytes, "note for the desk"},
	}
	for _, c := range cases {
		over := goodIdea()
		over[c.field] = strings.Repeat("x", c.over)
		err := validateRoadmapIdeaPayload(context.Background(), []byte(ideaJSON(over)))
		if err == nil {
			t.Errorf("%s at %d bytes accepted — the cap is %d", c.name, c.over, c.over-1)
			continue
		}
		if !strings.Contains(err.Error(), c.message) {
			t.Errorf("%s over the cap: refusal %q does not say why", c.name, err.Error())
		}
		under := goodIdea()
		under[c.field] = strings.Repeat("x", c.under)
		if err := validateRoadmapIdeaPayload(context.Background(), []byte(ideaJSON(under))); err != nil {
			t.Errorf("%s at exactly the cap (%d) refused: %v", c.name, c.under, err)
		}
	}
}

func TestRoadmapIdeaTrailingJSONRefused(t *testing.T) {
	payload := ideaJSON(goodIdea()) + ideaJSON(goodIdea())
	err := validateRoadmapIdeaPayload(context.Background(), []byte(payload))
	if err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("two payloads read as one ask (err = %v) — the strict door reads one document", err)
	}
}

// TestRoadmapIdeaRegistryEntryExists pins the registry half the seam rides:
// the kind resolves to an entry that VALIDATES (the built-in shape half
// answers through the same door the seam completes), is INCOMPLETE without
// the cmd/aeon seam (Effect ok=false), and `Missing` names the two halves
// nobody supplied — so an unwired daemon refuses to commit the kind by
// name, never applies nothing and calls it committed. The nine-name kinds
// list desk_test.go pins is UNTOUCHED: this kind reaches the registry
// through the effects map, and only through it (the census that list drives
// is not this door).
func TestRoadmapIdeaRegistryEntryExists(t *testing.T) {
	r := testRegistry(&spyBoundary{}, Seams{})
	if _, ok := r.Effect(string(KindRoadmapIdea)); ok {
		t.Fatal("roadmap_idea reads complete WITHOUT the seam — the store halves are the seam's")
	}
	miss := r.Missing(string(KindRoadmapIdea))
	for _, half := range []string{"AlreadyApplied", "Apply"} {
		if !strings.Contains(miss, half) {
			t.Fatalf("Missing does not name the %s half: %q", half, miss)
		}
	}
	if err := r.ValidateProposal(context.Background(), string(KindRoadmapIdea), ideaJSON(goodIdea())); err != nil {
		t.Fatalf("the shape half must answer through the registry: %v", err)
	}
	if err := r.ValidateProposal(context.Background(), string(KindRoadmapIdea), ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "status": "ready"})); err == nil {
		t.Fatal("the registry door accepted a smuggled status — an idea that names its own rung is the write RM8 keeps out")
	}
}

// TestValidateRoadmapIdeaExportsTheShapeHalf pins the door the seam runs:
// the exported wrapper answers with the SAME words as the built-in half
// (one function per door, TD3 — the seam adds the store halves around it,
// never a second spelling of the shape).
func TestValidateRoadmapIdeaExportsTheShapeHalf(t *testing.T) {
	payload := ideaJSON(map[string]string{"componentRoot": "/srv/x", "title": "t", "note": "n", "priorityState": "essential"})
	in := validateRoadmapIdeaPayload(context.Background(), []byte(payload))
	out := ValidateRoadmapIdea(context.Background(), []byte(payload))
	if in == nil || out == nil || in.Error() != out.Error() {
		t.Fatalf("the exported half differs from the built-in one:\n built-in: %v\n exported: %v", in, out)
	}
}
