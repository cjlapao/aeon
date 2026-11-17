package budget

import (
	"testing"
	"time"
)

// TestEvaluateTable is the pure engine's table test (LC9/LC11): every
// dimension × policy × known/partial/unknown × window. The one law it pins is
// P31: an unknown is a count, never a zero — a `stop` policy with unpriced work
// must hit cost-unproven, and a `proceed` one must note, never pretend the
// floor is a total.
func TestEvaluateTable(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	f := 50.0
	tokens := int64(1000)
	wall := int64(60_000)
	steps := int64(10)
	warn := 80

	cases := []struct {
		name       string
		budget     Budget
		spend      Spend
		warned     map[string]time.Time
		wantOK     bool
		wantHit    bool // a hit at all
		wantReason string
		wantNote   bool
		wantWarn   bool
	}{
		{
			name:   "usd under cap, exact",
			budget: Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f},
			spend:  Spend{USD: 10},
			wantOK: true,
		},
		{
			name:       "usd at cap is a hit (exhausted at the cap)",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f},
			spend:      Spend{USD: 50},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:       "usd over cap is a hit",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f},
			spend:      Spend{USD: 51},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:       "zero cap is a real cap, hit at floor 0",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &zeroF},
			spend:      Spend{USD: 0},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:   "no usd cap skips the dimension",
			budget: Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", Tokens: &tokens},
			spend:  Spend{USD: 9999},
			wantOK: true,
		},
		{
			name:       "usd stop policy with unknowns is cost-unproven",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, UnknownPolicy: "stop"},
			spend:      Spend{USD: 10, USDUnknown: 3},
			wantOK:     false,
			wantHit:    true,
			wantReason: "cost-unproven",
		},
		{
			name:     "usd proceed policy with unknowns notes, not hits",
			budget:   Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, UnknownPolicy: "proceed"},
			spend:    Spend{USD: 10, USDUnknown: 2},
			wantOK:   true,
			wantNote: true,
		},
		{
			name:       "usd proceed with unknowns still hits when over cap",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, UnknownPolicy: "proceed"},
			spend:      Spend{USD: 60, USDUnknown: 2},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:       "tokens at cap is a hit",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", Tokens: &tokens},
			spend:      Spend{Tokens: 1000},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:       "tokens stop policy with unknowns is cost-unproven",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", Tokens: &tokens, UnknownPolicy: "stop"},
			spend:      Spend{Tokens: 500, TokensUnknown: 1},
			wantOK:     false,
			wantHit:    true,
			wantReason: "cost-unproven",
		},
		{
			name:       "wall-time at cap is a hit",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", WallMs: &wall},
			spend:      Spend{WallMs: 60_000},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:       "steps at cap is a hit",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", Steps: &steps},
			spend:      Spend{Steps: 10},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:       "two dimensions both hit, usd reported first",
			budget:     Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, Tokens: &tokens},
			spend:      Spend{USD: 60, Tokens: 2000},
			wantOK:     false,
			wantHit:    true,
			wantReason: "over-cap",
		},
		{
			name:     "warn line crossed warns",
			budget:   Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, WarnPct: &warn},
			spend:    Spend{USD: 45}, // 90% of 50
			wantOK:   true,
			wantWarn: true,
		},
		{
			name:   "warn line not crossed does not warn",
			budget: Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, WarnPct: &warn},
			spend:  Spend{USD: 30}, // 60% of 50
			wantOK: true,
		},
		{
			name:   "lifetime warn deduped when already warned",
			budget: Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f, WarnPct: &warn},
			spend:  Spend{USD: 45},
			warned: map[string]time.Time{"bud-1|usd": now.Add(-time.Hour)},
			wantOK: true,
		},
		{
			name:   "day warn deduped within 24h",
			budget: Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "day", USD: &f, WarnPct: &warn},
			spend:  Spend{USD: 45},
			warned: map[string]time.Time{"bud-1|usd": now.Add(-time.Hour)},
			wantOK: true,
		},
		{
			name:     "day warn re-fires after 24h",
			budget:   Budget{ID: "bud-1", Scope: "loop", Subject: "lp-1", Window: "day", USD: &f, WarnPct: &warn},
			spend:    Spend{USD: 45},
			warned:   map[string]time.Time{"bud-1|usd": now.Add(-48 * time.Hour)},
			wantOK:   true,
			wantWarn: true,
		},
		{
			name:   "no budget is an ok verdict",
			budget: Budget{},
			spend:  Spend{},
			wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			budgets := []Budget{}
			if tc.name != "no budget is an ok verdict" {
				budgets = append(budgets, tc.budget)
			}
			sp := map[string]Spend{}
			if tc.budget.ID != "" {
				sp[tc.budget.ID] = tc.spend
			}
			v := Evaluate(budgets, sp, tc.warned, now)

			if v.OK != tc.wantOK {
				t.Errorf("OK = %v, want %v (hits=%v)", v.OK, tc.wantOK, v.Hits)
			}
			if tc.wantHit {
				if len(v.Hits) == 0 {
					t.Fatalf("wanted a hit, got none; notes=%v", v.Notes)
				}
				if v.Hits[0].Reason != tc.wantReason {
					t.Errorf("hit[0].Reason = %q, want %q", v.Hits[0].Reason, tc.wantReason)
				}
			} else if len(v.Hits) > 0 {
				t.Errorf("did not want a hit, got %v", v.Hits)
			}
			if tc.wantNote && len(v.Notes) == 0 {
				t.Errorf("wanted a note, got none")
			}
			if tc.wantWarn && len(v.Warns) == 0 {
				t.Errorf("wanted a warn, got none; pct check")
			}
			if !tc.wantWarn && len(v.Warns) > 0 {
				t.Errorf("did not want a warn, got %v", v.Warns)
			}
		})
	}
}

// TestEvaluateOrderingIsDeterministic (LC9) pins that the tightest breach is
// reported first: scope narrow→wide, then budget id. A flaky "which budget"
// answer is exactly the instability this exists to prevent.
func TestEvaluateOrderingIsDeterministic(t *testing.T) {
	f := 100.0
	budgets := []Budget{
		{ID: "bud-ws", Scope: "workspace", Subject: "workspace", Window: "lifetime", USD: &f},
		{ID: "bud-epic", Scope: "epic", Subject: "ep-1", Window: "lifetime", USD: &f},
		{ID: "bud-comp", Scope: "component", Subject: "cp-1", Window: "lifetime", USD: &f},
		{ID: "bud-loop", Scope: "loop", Subject: "lp-1", Window: "lifetime", USD: &f},
	}
	sp := map[string]Spend{
		"bud-ws":   {USD: 200},
		"bud-epic": {USD: 200},
		"bud-comp": {USD: 200},
		"bud-loop": {USD: 200},
	}
	v := Evaluate(budgets, sp, nil, time.Now())
	if v.OK {
		t.Fatal("expected not ok")
	}
	if len(v.Hits) != 4 {
		t.Fatalf("expected 4 hits, got %d", len(v.Hits))
	}
	// Tightest first: loop, then epic, then component, then workspace.
	want := []string{"bud-loop", "bud-epic", "bud-comp", "bud-ws"}
	for i, id := range want {
		if v.Hits[i].BudgetID != id {
			t.Errorf("hits[%d] = %s, want %s", i, v.Hits[i].BudgetID, id)
		}
	}
}

// TestEvaluateSentenceIsStable pins the one renderer's shape: the hit names the
// budget, scope, subject, dimension, floor and cap, byte for byte.
func TestEvaluateSentenceIsStable(t *testing.T) {
	f := 50.0
	budgets := []Budget{{ID: "bud-1", Scope: "epic", Subject: "ep-1", Window: "lifetime", USD: &f}}
	v := Evaluate(budgets, map[string]Spend{"bud-1": {USD: 50}}, nil, time.Now())
	if len(v.Hits) != 1 {
		t.Fatalf("wanted one hit, got %v", v.Hits)
	}
	got := v.Hits[0].Sentence()
	want := "budget bud-1 on epic ep-1 is exhausted (usd 50 of 50)"
	if got != want {
		t.Errorf("sentence = %q, want %q", got, want)
	}
}

// zeroF is a real zero cap.
var zeroF = 0.0
