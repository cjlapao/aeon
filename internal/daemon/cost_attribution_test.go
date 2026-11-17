package daemon

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// TestLoopViewCarriesPerRouteAttribution — D16 and P34, the same fact the
// CLI table prints must REACH the app through `loop.show`: the per-route
// ledger rides the view, computed once by model.AttributeCosts in the daemon.
// A CLI-only ledger would leave S31's footprint panel with a single total and
// no way to show which route costs what — the app one step behind for the
// exact fact the task is about.
func TestLoopViewCarriesPerRouteAttribution(t *testing.T) {
	sstore, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer sstore.Close()
	sc := store.Scope{LoopSlug: "attr", ComponentRoot: "/p"}
	if _, err := sstore.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Root: "/p", Goal: "g", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	add := func(route, provider, m string, cost model.Cost) {
		st, err := sstore.CreateStep(sc, store.NewStep{Phase: "build", Agent: "a", Source: "live"})
		if err != nil {
			t.Fatal(err)
		}
		if err := sstore.AppendMessages(st.ID, []model.Message{
			{Role: model.RoleUser, Content: []model.Block{model.TextBlock("go")}},
			{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("done")},
				Provenance: &model.Provenance{Route: route, Provider: provider, Model: m}},
		}); err != nil {
			t.Fatal(err)
		}
		if err := sstore.SetStepCost(st.ID, cost); err != nil {
			t.Fatal(err)
		}
	}
	add("claude-route", "anthropic", "claude-opus", model.Cost{State: model.CostKnown, USD: 0.0412})
	add("gpt-route", "openai", "gpt-mini", model.Cost{State: model.CostKnown, USD: 0.0180})
	add("local", "ollama", "qwen3.5:latest", model.Cost{State: model.CostUnknown, Reason: model.ReasonNoPriceForRoute})

	d := &Daemon{store: sstore}
	st := loop.LoopState{ID: "l1", Slug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot, Status: "running",
		Blueprint: &blueprint.Blueprint{ID: "dev-loop"}}
	view := d.viewLoop(st, false)

	if view.CostAttribution == nil {
		t.Fatal("loop.show carries no ledger — the app cannot render what the surface never sent (D16, P34)")
	}
	at := view.CostAttribution
	if len(at.Groups) != 3 {
		t.Fatalf("%d route lines, want 3: %+v", len(at.Groups), at.Groups)
	}
	if at.Groups[2].RouteID != "local" {
		t.Errorf("route order is not first-seen: %+v", at.Groups)
	}
	total := at.TotalSentence()
	if !strings.Contains(total, "at least") || !strings.Contains(total, "0.0592") {
		t.Errorf("the view's total %q is not the floor the CLI will print — two renderers, one computation (D5, D16)", total)
	}
	if !strings.Contains(at.Groups[2].Sentence(), "route 'local' is unmetered") {
		t.Errorf("the unmetered line lost its reason on the way to the view (M30): %q", at.Groups[2].Sentence())
	}
	for _, line := range []string{total, at.Groups[0].Sentence(), at.Groups[1].Sentence(), at.Groups[2].Sentence()} {
		if strings.Contains(line, "$0.00") {
			t.Errorf("forbidden $0.00 reached the view rendering: %q (D9)", line)
		}
	}

	// And with no store at all the view is silent, not zero — the absence of
	// a ledger must never read as a free loop.
	bare := (&Daemon{}).viewLoop(st, false)
	if bare.CostAttribution != nil {
		t.Error("a daemon with no store invented a ledger")
	}
}
