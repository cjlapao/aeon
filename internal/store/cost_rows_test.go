package store

import (
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// TestLoopCostRowsCarriesProvenanceAndState — the ledger's read side (S18
// T7, M26): cost from the step's columns, provenance from its LAST assistant
// turn (the turn that answered is the one whose usage was billed), and steps
// that never spoke land empty-tripled rather than guessed.
func TestLoopCostRowsCarriesProvenanceAndState(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "ledger", "/p")

	// Step 1: priced route, answered from claude after an openai attempt —
	// the last assistant turn is anthropic's, and that is where the money went.
	s1 := seedStep(t, s, sc)
	if err := s.AppendMessages(s1.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			model.TextBlock("let me look"),
			{Kind: model.BlockToolUse, ID: "t1", Name: "read", Input: []byte(`{"path":"README.md"}`)},
		}, Provenance: &model.Provenance{Route: "gpt", Provider: "openai", Model: "gpt-mini"}},
		{Role: model.RoleUser, Content: []model.Block{
			{Kind: model.BlockToolResult, ToolUseID: "t1", Content: "the readme"},
		}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("answered here")},
			Provenance: &model.Provenance{Route: "opus", Provider: "anthropic", Model: "claude-opus"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(s1.ID, model.Cost{State: model.CostKnown, USD: 0.0412}); err != nil {
		t.Fatal(err)
	}

	// Step 2: unmetered route, no prices — unknown carries its REASON through
	// the columns and back (D12: the state is the column).
	s2 := seedStep(t, s, sc)
	if err := s.AppendMessages(s2.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("local")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("ollama answered")},
			Provenance: &model.Provenance{Route: "local", Provider: "ollama", Model: "qwen3.5:latest"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepCost(s2.ID, model.Cost{State: model.CostUnknown, Reason: model.ReasonNoPriceForRoute}); err != nil {
		t.Fatal(err)
	}

	// Step 3: never reached a model.
	seedStep(t, s, sc)

	rows, err := s.LoopCostRows(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	r1, r2, r3 := rows[0], rows[1], rows[2]
	if r1.RouteID != "opus" || r1.Provider != "anthropic" || r1.Model != "claude-opus" {
		t.Errorf("step 1 attributed to %s/%s/%s — billing follows the ANSWER, not the first attempt (M26)", r1.RouteID, r1.Provider, r1.Model)
	}
	if r1.Cost.State != model.CostKnown || r1.Cost.USD != 0.0412 {
		t.Errorf("step 1 cost %+v", r1.Cost)
	}
	if r2.Cost.State != model.CostUnknown || r2.Cost.Reason != model.ReasonNoPriceForRoute {
		t.Errorf("the unmetered reason did not survive the columns: %+v", r2.Cost)
	}
	if r3.RouteID != "" || r3.Provider != "" || r3.Model != "" {
		t.Errorf("a step that never called a model was given a route: %+v", r3)
	}
	if r3.Cost.State != model.CostUnknown {
		t.Errorf("an uncalled step priced itself %v — nobody having spoken is not a zero (M9)", r3.Cost.State)
	}

	// One computation: the ledger over these rows reaches the spec's shape.
	at := model.AttributeCosts(rows)
	sent := at.TotalSentence()
	if at.Total.State != model.CostUnknown || at.Total.USDAtLeast != 0.0412 {
		t.Errorf("total %+v, want the 0.0412 floor over the unmetered step", at.Total)
	}
	for _, want := range []string{"at least", "0.0412"} {
		if !contains(sent, want) {
			t.Errorf("the ledger total %q misses %q (D5)", sent, want)
		}
	}
	if !contains(at.Groups[1].Sentence(), "route 'local' is unmetered") {
		t.Errorf("the unmetered line %q does not name its route (M30)", at.Groups[1].Sentence())
	}
	if at.Groups[len(at.Groups)-1].Provider != "" {
		t.Error("the never-spoke step left the unattributed line")
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (hay == needle || indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
