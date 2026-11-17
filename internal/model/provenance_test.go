package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// Provenance is M26's record of WHO PRODUCED an assistant turn. These tests
// pin the rule in its one home — ValidateProvenance — plus the derivation
// (NewProvenance reads the Route the call went out on) and the envelope
// behaviour that makes the store's job honest: omitempty means old JSON
// without the field decodes to nil, NOT to a hollow triple.

func routeOf(id, provider, model_ string) Route {
	r := Route{ID: id, Provider: provider, Model: model_}
	return r
}

// TestNewProvenanceReadsTheRouteTheCallWentOutOn — the producer stamps from
// the Route in hand (O4). One derivation, so there is no second place where
// the triple could be assembled wrongly.
func TestNewProvenanceReadsTheRouteTheCallWentOutOn(t *testing.T) {
	r := routeOf("local-llama", "openai-compatible", "llama3.3")
	p := NewProvenance(r)
	if p.Route != "local-llama" || p.Provider != "openai-compatible" || p.Model != "llama3.3" {
		t.Errorf("the triple is %+v, want the route's own ID, provider and model", p)
	}
	if err := p.Complete(); err != nil {
		t.Errorf("a triple read from a real route must be complete: %v", err)
	}
}

// TestHollowProvenanceIsRefused — present-but-empty is a different failure
// from absent, and each of the three empties gets its own named reason
// (DB19's discipline: a refusal names the thing it refused).
func TestHollowProvenanceIsRefused(t *testing.T) {
	cases := []struct {
		name string
		p    Provenance
		want string
	}{
		{"no route", Provenance{Provider: "anthropic", Model: "claude"}, "route"},
		{"no provider", Provenance{Route: "r1", Model: "claude"}, "provider"},
		{"no model", Provenance{Route: "r1", Provider: "anthropic"}, "model"},
	}
	for _, c := range cases {
		err := c.p.Complete()
		if err == nil {
			t.Errorf("%s: a hollow triple was accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the refusal does not name %q: %v", c.name, c.want, err)
		}
	}
}

// TestEveryAssistantTurnCarriesProvenance — the M26 rule in its one home.
// An assistant turn without it fails validation; with a complete triple it
// passes; a hollow one fails through Complete.
func TestEveryAssistantTurnCarriesProvenance(t *testing.T) {
	blocks := []Block{TextBlock("the answer")}

	bare := Message{Role: RoleAssistant, Content: blocks}
	if err := bare.ValidateProvenance(); err == nil {
		t.Error("an assistant turn without provenance passed validation (M26)")
	} else if !strings.Contains(err.Error(), "provenance") {
		t.Errorf("the refusal does not name provenance: %v", err)
	}

	p := Provenance{Route: "r1", Provider: "anthropic", Model: "claude"}
	full := Message{Role: RoleAssistant, Content: blocks, Provenance: &p}
	if err := full.ValidateProvenance(); err != nil {
		t.Errorf("a complete triple was refused: %v", err)
	}

	hollow := Message{Role: RoleAssistant, Content: blocks,
		Provenance: &Provenance{Route: "r1", Provider: ""}}
	if err := hollow.ValidateProvenance(); err == nil {
		t.Error("an assistant turn with a hollow triple passed validation")
	}
}

// TestUserAndToolTurnsHaveNone — a user turn has no producing model, and a
// tool_result was produced by a TOOL (M26). Provenance on either would make
// the field mean two things, so its presence there is as much a refusal as
// its absence is on an assistant turn.
func TestUserAndToolTurnsHaveNone(t *testing.T) {
	for _, role := range []Role{RoleUser, RoleSystem} {
		plain := Message{Role: role, Content: []Block{TextBlock("hi")}}
		if err := plain.ValidateProvenance(); err != nil {
			t.Errorf("a bare %s turn must be valid: %v", role, err)
		}
		p := Provenance{Route: "r1", Provider: "anthropic", Model: "claude"}
		laden := Message{Role: role, Content: []Block{TextBlock("hi")}, Provenance: &p}
		if err := laden.ValidateProvenance(); err == nil {
			t.Errorf("a %s turn with provenance was accepted — the field would mean two things", role)
		}
	}
	// A tool_result rides inside a user turn; it has no producer either.
	tr := Message{Role: RoleUser, Content: []Block{ToolResultBlock("t1", "42", false)}}
	if err := tr.ValidateProvenance(); err != nil {
		t.Errorf("a tool_result turn must be valid without provenance: %v", err)
	}
}

// TestRouteIsRecordedNotJustModel — O4. Two routes at the SAME provider and
// model with different prices or base URLs are different servers as far as
// the ledger is concerned, and the triple keeps them apart.
func TestRouteIsRecordedNotJustModel(t *testing.T) {
	a := Message{Role: RoleAssistant, Content: []Block{TextBlock("x")},
		Provenance: &Provenance{Route: "expensive", Provider: "openai", Model: "gpt-4o"}}
	b := Message{Role: RoleAssistant, Content: []Block{TextBlock("x")},
		Provenance: &Provenance{Route: "cheap-mirror", Provider: "openai", Model: "gpt-4o"}}
	if err := a.ValidateProvenance(); err != nil {
		t.Fatal(err)
	}
	if err := b.ValidateProvenance(); err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a.Provenance)
	jb, _ := json.Marshal(b.Provenance)
	if string(ja) == string(jb) {
		t.Error("two routes behind one provider+model encode identically — the route was lost (O4)")
	}
	if !strings.Contains(string(ja), "expensive") {
		t.Errorf("the route is not in the encoding: %s", ja)
	}
}

// TestProvenanceSurvivesJSONRoundTrip — S3's envelope is JSON, so the
// omitempty tag carries two promises: a stamped turn round-trips byte-shape
// identical, and an unstamped one (every row written before 0027) has NO
// "provenance" key at all rather than a hollow object. The second is what
// lets old StoredTranscript envelopes decode exactly as they were written.
func TestProvenanceSurvivesJSONRoundTrip(t *testing.T) {
	p := Provenance{Route: "r1", Provider: "anthropic", Model: "claude-sonnet-4-5"}
	in := Message{Role: RoleAssistant, Content: []Block{TextBlock("hello")}, Provenance: &p}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"provenance"`) {
		t.Fatalf("a stamped turn must carry the field: %s", raw)
	}
	var out Message
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Provenance == nil || *out.Provenance != p {
		t.Errorf("round trip lost or changed the triple: %+v", out.Provenance)
	}

	bare := Message{Role: RoleUser, Content: []Block{TextBlock("hello")}}
	raw, err = json.Marshal(bare)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "provenance") {
		t.Errorf("an unstamped turn must omit the field entirely (old envelopes): %s", raw)
	}

	var legacy Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"kind":"text","text":"x"}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Provenance != nil {
		t.Error("a pre-M26 envelope must decode to nil provenance, not a hollow triple")
	}
}
