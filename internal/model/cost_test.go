package model

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// A fully priced route, at figures close to a real frontier model so the
// worked examples below are recognisable rather than abstract.
func fullPrices() *Prices {
	return &Prices{
		InputUSDPerMTok:      Float64Ptr(3.00),
		OutputUSDPerMTok:     Float64Ptr(15.00),
		CacheReadUSDPerMTok:  Float64Ptr(0.30),
		CacheWriteUSDPerMTok: Float64Ptr(3.75),
	}
}

func TestEveryPricedFieldPresentYieldsKnown(t *testing.T) {
	cost := ComputeCost(Usage{
		InputTokens:      IntPtr(1_000),
		OutputTokens:     IntPtr(500),
		CacheReadTokens:  IntPtr(2_000),
		CacheWriteTokens: IntPtr(100),
	}, fullPrices())

	if cost.State != CostKnown {
		t.Fatalf("State = %q, want known (reason %q)", cost.State, cost.Reason)
	}
	if len(cost.Missing) != 0 {
		t.Errorf("Missing = %v on a known cost", cost.Missing)
	}
	// Hand-computed: 1000/1e6*3 + 500/1e6*15 + 2000/1e6*0.30 + 100/1e6*3.75
	//              = 0.003    + 0.0075      + 0.0006         + 0.000375
	const want = 0.003 + 0.0075 + 0.0006 + 0.000375
	if diff := cost.USD - want; diff > 1e-12 || diff < -1e-12 {
		t.Fatalf("USD = %v, want %v", cost.USD, want)
	}
}

// M25 — the unit, against a hand-computed figure. A factor-of-a-million error
// looks plausible in both directions, so the only guard is arithmetic somebody
// can check by eye.
func TestPricesAreUSDPerMillionAgainstAHandComputedValue(t *testing.T) {
	// One million input tokens at $3.00 per million is exactly $3.00.
	cost := ComputeCost(Usage{InputTokens: IntPtr(1_000_000)},
		&Prices{InputUSDPerMTok: Float64Ptr(3.00)})

	if cost.State != CostKnown {
		t.Fatalf("State = %q, want known", cost.State)
	}
	if cost.USD != 3.00 {
		t.Fatalf("one million tokens at $3/Mtok = %v, want exactly 3.00. If this reads 3000000 "+
			"the price is being treated as per-token; if it reads 0.000003 the tokens are.", cost.USD)
	}
}

func TestSomePricedFieldsAbsentYieldsPartialWithMissingNamingThem(t *testing.T) {
	cost := ComputeCost(Usage{InputTokens: IntPtr(1_000)}, fullPrices())

	if cost.State != CostPartial {
		t.Fatalf("State = %q, want partial", cost.State)
	}
	if cost.USDAtLeast != 0.003 {
		t.Errorf("USDAtLeast = %v, want 0.003 (what the reported counts cost)", cost.USDAtLeast)
	}
	if cost.USD != 0 {
		t.Errorf("USD = %v on a partial cost; the figure belongs in USDAtLeast, so a reader "+
			"cannot mistake a floor for a total", cost.USD)
	}
	want := []string{"cacheReadTokens", "cacheWriteTokens", "outputTokens"}
	if !reflect.DeepEqual(cost.Missing, want) {
		t.Fatalf("Missing = %v, want %v", cost.Missing, want)
	}
}

// **THE regression test for the failure this whole design exists to prevent.**
//
// One line, and it is the difference between a UI that says "unknown" and a UI
// that says a real call was free.
func TestAbsentUsageNeverYieldsKnownWithZeroUSD(t *testing.T) {
	cost := ComputeCost(Usage{}, fullPrices())

	if cost.State == CostKnown {
		t.Fatalf("a call with NO reported usage priced as known: %+v.\n"+
			"This is the single failure M9 and M10 exist to prevent — it reaches the UI at S9 "+
			"looking like a cheap step, and a rollup as a number that drags an average down", cost)
	}
	if cost.State != CostUnknown {
		t.Fatalf("State = %q, want unknown", cost.State)
	}
	if cost.Reason != ReasonProviderOmittedUsage {
		t.Errorf("Reason = %q, want %q", cost.Reason, ReasonProviderOmittedUsage)
	}
	if cost.USD != 0 || cost.USDAtLeast != 0 {
		t.Errorf("an unknown cost carries a figure: %+v", cost)
	}
}

func TestARouteWithNoPricesYieldsUnknownWithNoPriceForRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prices *Prices
	}{
		{"nil prices", nil},
		{"empty prices", &Prices{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost := ComputeCost(Usage{InputTokens: IntPtr(1_000)}, tc.prices)
			if cost.State != CostUnknown {
				t.Fatalf("State = %q, want unknown", cost.State)
			}
			if cost.Reason != ReasonNoPriceForRoute {
				t.Errorf("Reason = %q, want %q — the two unknown reasons send somebody to "+
					"different fixes", cost.Reason, ReasonNoPriceForRoute)
			}
		})
	}
}

// The order matters: with no prices, what the provider reported is irrelevant,
// so the reason must be about the route rather than the provider.
func TestNoPricesIsCheckedBeforeNoUsage(t *testing.T) {
	cost := ComputeCost(Usage{}, nil)
	if cost.Reason != ReasonNoPriceForRoute {
		t.Fatalf("Reason = %q, want %q: with no prices the provider's silence is not the "+
			"problem to report", cost.Reason, ReasonNoPriceForRoute)
	}
}

// An absent field nobody priced is not missing information.
func TestAnUnpricedAbsentFieldDoesNotAppearInMissing(t *testing.T) {
	// Only input and output are priced; the cache fields are not.
	cost := ComputeCost(Usage{InputTokens: IntPtr(1_000)}, &Prices{
		InputUSDPerMTok:  Float64Ptr(3.00),
		OutputUSDPerMTok: Float64Ptr(15.00),
	})
	if cost.State != CostPartial {
		t.Fatalf("State = %q, want partial", cost.State)
	}
	for _, unpriced := range []string{"cacheReadTokens", "cacheWriteTokens"} {
		for _, got := range cost.Missing {
			if got == unpriced {
				t.Errorf("Missing names %q, which nobody priced — a reader would go looking "+
					"for a provider problem that does not exist", unpriced)
			}
		}
	}
	if !reflect.DeepEqual(cost.Missing, []string{"outputTokens"}) {
		t.Fatalf("Missing = %v, want only outputTokens", cost.Missing)
	}
}

// Usage arrived, but nothing it reported is priced. The counts buy nothing, so
// this is not a floor of zero — it is unknown.
func TestUsageWithNothingPricedYieldsUnknownRatherThanAZeroFloor(t *testing.T) {
	cost := ComputeCost(Usage{CacheReadTokens: IntPtr(5_000)},
		&Prices{InputUSDPerMTok: Float64Ptr(3.00)})
	if cost.State == CostPartial && cost.USDAtLeast == 0 {
		t.Fatal("reported a partial cost with a zero floor, which reads as 'at least free'")
	}
	if cost.State != CostUnknown {
		t.Fatalf("State = %q, want unknown", cost.State)
	}
}

// Rounding here makes rollups drift as they accumulate: a hundred steps each
// rounded to the cent is a figure wrong by design.
func TestTheReturnedUSDIsNotRounded(t *testing.T) {
	cost := ComputeCost(Usage{InputTokens: IntPtr(1)}, &Prices{InputUSDPerMTok: Float64Ptr(3.00)})
	if cost.State != CostKnown {
		t.Fatalf("State = %q", cost.State)
	}
	const want = 3.0 / 1_000_000
	if cost.USD != want {
		t.Fatalf("USD = %v, want the unrounded %v — one token must not price as 0.00", cost.USD, want)
	}
	if cost.USD == 0 {
		t.Fatal("one token rounded to zero; a thousand such calls would report free")
	}
}

// A price of zero is a legitimate configuration — a free local model — and must
// not be confused with an absent price.
func TestAPriceOfZeroIsKnownAndFreeRatherThanUnknown(t *testing.T) {
	cost := ComputeCost(Usage{InputTokens: IntPtr(1_000), OutputTokens: IntPtr(500)},
		&Prices{InputUSDPerMTok: Float64Ptr(0), OutputUSDPerMTok: Float64Ptr(0)})
	if cost.State != CostKnown {
		t.Fatalf("State = %q, want known: a configured zero price is a known cost of zero, "+
			"which is different from an unknown one", cost.State)
	}
	if cost.USD != 0 {
		t.Fatalf("USD = %v, want 0", cost.USD)
	}
}

// P32 — an aggregate over uncertain values is itself uncertain.
func TestAddPropagatesUncertainty(t *testing.T) {
	known := Cost{State: CostKnown, USD: 1.50}
	partial := Cost{State: CostPartial, USDAtLeast: 0.25, Missing: []string{"outputTokens"}}
	unknown := Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage}

	for _, tc := range []struct {
		name      string
		a, b      Cost
		wantState CostState
		wantFloor float64
	}{
		{"known + known = known", known, known, CostKnown, 3.00},
		{"known + partial = partial", known, partial, CostPartial, 1.75},
		{"partial + partial = partial", partial, partial, CostPartial, 0.50},
		{"known + unknown = UNKNOWN", known, unknown, CostUnknown, 1.50},
		{"unknown + known = UNKNOWN", unknown, known, CostUnknown, 1.50},
		{"partial + unknown = UNKNOWN", partial, unknown, CostUnknown, 0.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.a.Add(tc.b)
			if got.State != tc.wantState {
				t.Fatalf("State = %q, want %q.\nA total that silently omits one call's cost is "+
					"a wrong total presented as a right one (P32)", got.State, tc.wantState)
			}
			floor := got.USD
			if tc.wantState != CostKnown {
				floor = got.USDAtLeast
			}
			if diff := floor - tc.wantFloor; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("floor = %v, want %v", floor, tc.wantFloor)
			}
		})
	}
}

// What was measured is kept even when the total is unknown, so "unknown, at
// least $1.40" can be rendered rather than discarding the measurement.
func TestAddKeepsTheKnownFloorUnderAnUnknownTotal(t *testing.T) {
	got := Cost{State: CostKnown, USD: 1.40}.Add(Cost{State: CostUnknown, Reason: "x"})
	if got.USDAtLeast != 1.40 {
		t.Fatalf("USDAtLeast = %v, want 1.40 — the measured part should survive", got.USDAtLeast)
	}
	if got.Reason == "" {
		t.Error("the unknown total carries no reason")
	}
}

func TestAddDoesNotDuplicateMissingFields(t *testing.T) {
	a := Cost{State: CostPartial, USDAtLeast: 1, Missing: []string{"outputTokens"}}
	got := a.Add(a)
	if len(got.Missing) != 1 || got.Missing[0] != "outputTokens" {
		t.Fatalf("Missing = %v, want one entry", got.Missing)
	}
}

// The reason set is documented as complete, including the one this file does
// not produce — so it does not look like an oversight.
func TestStreamAbortedEarlyIsDocumentedAsTheClientsToSet(t *testing.T) {
	src, err := readSource("cost.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "NOT produced here") {
		t.Error("cost.go does not say that stream-aborted-early is the client's to set, so the " +
			"reason set looks incomplete")
	}
	if ReasonStreamAbortedEarly == "" {
		t.Error("the reason constant is empty")
	}
}

// What keeps ComputeCost's `priced == 0` branch unreachable, so removing the
// dead branch is safe.
//
// **Add a field to `Prices` without adding a line to ComputeCost and this
// fails** — which is the failure it protects against: a priced route silently
// reporting `unknown` because the new field was never priced.
func TestEveryPriceFieldIsPricedByComputeCost(t *testing.T) {
	priced := map[string]bool{}
	for _, name := range pricedFieldNames() {
		priced[name] = true
	}

	rt := reflect.TypeOf(Prices{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if !priced[name] {
			t.Errorf("Prices.%s is never priced by ComputeCost. A route configuring it would "+
				"report an unknown cost while looking fully priced — add it to ComputeCost's "+
				"`lines` and to pricedFieldNames", name)
		}
	}
	if rt.NumField() != len(priced) {
		t.Errorf("Prices has %d fields and ComputeCost prices %d", rt.NumField(), len(priced))
	}

	// And each named field really does affect the result, which a name list
	// alone would not prove.
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		p := &Prices{}
		reflect.ValueOf(p).Elem().FieldByName(name).Set(reflect.ValueOf(Float64Ptr(1_000_000)))

		u := Usage{}
		// The usage field that corresponds, by position.
		usageField := []string{"InputTokens", "OutputTokens", "CacheReadTokens", "CacheWriteTokens"}[i]
		reflect.ValueOf(&u).Elem().FieldByName(usageField).Set(reflect.ValueOf(IntPtr(1_000_000)))

		cost := ComputeCost(u, p)
		if cost.State != CostKnown || cost.USD != 1_000_000 {
			t.Errorf("pricing only %s gave %+v; one million tokens at $1M/Mtok should be a "+
				"known $1,000,000", name, cost)
		}
	}
}

func TestDedupeHandlesTheEmptyCase(t *testing.T) {
	if got := dedupe(nil); got != nil {
		t.Errorf("dedupe(nil) = %v, want nil", got)
	}
	if got := dedupe([]string{}); got != nil {
		t.Errorf("dedupe(empty) = %v, want nil", got)
	}
	if got := dedupe([]string{"a", "a", "b", "b", "b", "c"}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("dedupe = %v", got)
	}
}

// --- S18 T7: the ledger -------------------------------------------------------

// stateAssign finds every `State: Cost…` assignment in the ledger code, so
// the fourth-state check is a grep over source and not a hope.
var stateAssign = regexp.MustCompile(`State:\s*(Cost[A-Za-z]+)`)

func T(i, o int) Usage {
	return Usage{InputTokens: &i, OutputTokens: &o}
}

func TestUnmeteredIsUnknownWithAReason(t *testing.T) {
	// Q5 answered from the code, not the spec: a priced route with no usage
	// is `unknown` — S3 got this right, and T7 pins that it STAYS right.
	if c := ComputeCost(Usage{}, &Prices{InputUSDPerMTok: f(1), OutputUSDPerMTok: f(2)}); c.State != CostUnknown || c.Reason != ReasonProviderOmittedUsage {
		t.Fatalf("absent usage produced %+v — M9 forbids the zero, and the state is the fix", c)
	}
	// An unmetered route: unknown, and the ledger sentence NAMES the route.
	rows := []CostRow{{StepID: 1, RouteID: "local", Provider: "ollama", Model: "qwen3.5:latest",
		Cost: ComputeCost(T(9, 12), nil)}}
	at := AttributeCosts(rows)
	if at.Total.State != CostUnknown {
		t.Fatalf("an unmetered route priced to %v — M30: not a claim of free", at.Total.State)
	}
	s := at.Groups[0].Sentence()
	for _, want := range []string{"unknown", "route 'local' is unmetered", "no price declared"} {
		if !strings.Contains(s, want) {
			t.Errorf("the unmetered sentence %q misses %q — a reason that does not name the route is a shrug with a prefix (M30, D9)", s, want)
		}
	}
}

func f(v float64) *float64 { return &v }

func TestNoDollarZeroAnywhere(t *testing.T) {
	// The literal string, searched across EVERY cost rendering this task
	// touches — the three states of a ledger line, the total, and the
	// deliberately hostile figures: a zero-usage free-tier model (usage
	// REPORTED, priced to nothing) and real sub-cent charges whose decimals
	// begin "$0.00" and read as free to a skimming eye (D9, I6, C7).
	rows := []CostRow{
		{StepID: 1, RouteID: "free", Provider: "openai", Model: "nano",
			Cost: ComputeCost(T(0, 0), &Prices{InputUSDPerMTok: f(0), OutputUSDPerMTok: f(0)})},
		{StepID: 2, RouteID: "cheap", Provider: "openai", Model: "mini",
			Cost: ComputeCost(T(100, 40), &Prices{InputUSDPerMTok: f(0.015), OutputUSDPerMTok: f(0.06)})},
		{StepID: 3, RouteID: "local", Provider: "ollama", Model: "qwen",
			Cost: ComputeCost(T(9, 12), nil)},
	}
	at := AttributeCosts(rows)
	rendered := []string{at.TotalSentence(), at.Groups[0].Sentence(), at.Groups[1].Sentence(), at.Groups[2].Sentence(),
		MoneyString(0), MoneyString(0.0043), MoneyString(0.00062), MoneyString(0.00999)}
	for _, r := range rendered {
		if strings.Contains(r, "$0.00") {
			t.Errorf("a cost rendering contains the forbidden literal $0.00: %q — free is a claim Aeon never makes for a route it cannot price (D9)", r)
		}
	}
	// And the zero must still say WHAT it is, not just avoid the digits.
	if !strings.Contains(at.Groups[0].Sentence(), "zero cost") {
		t.Errorf("the reported-and-priced-to-nothing case lost its own words: %q", at.Groups[0].Sentence())
	}
}

func TestAbsentUsageIsUnknownNotZero(t *testing.T) {
	// Q5's fix-site, asserted through the ledger too: a known $0.0412 next
	// to a usage-omitted step keeps the floor and names the omission — it
	// never collapses toward a smaller confident total (M9, D12).
	c := ComputeCost(Usage{}, &Prices{InputUSDPerMTok: f(3), OutputUSDPerMTok: f(15)})
	if c.State != CostUnknown || c.USD != 0 || c.USDAtLeast != 0 {
		t.Fatalf("absent usage gave %+v; absent is not zero (M9)", c)
	}
	at := AttributeCosts([]CostRow{
		{StepID: 1, RouteID: "a", Provider: "anthropic", Model: "claude", Cost: Cost{State: CostKnown, USD: 0.0412}},
		{StepID: 2, RouteID: "a", Provider: "anthropic", Model: "claude", Cost: c},
	})
	if at.Total.State != CostUnknown || at.Total.USDAtLeast != 0.0412 {
		t.Fatalf("total %+v, want unknown carrying the measured 0.0412 floor (P32)", at.Total)
	}
	s := at.TotalSentence()
	if !strings.Contains(s, "at least") || !strings.Contains(s, "0.0412") || !strings.Contains(s, "no usage") {
		t.Errorf("the total sentence %q drops the floor or the reason", s)
	}
}

func TestTotalOverUnknownIsAFloor(t *testing.T) {
	// The spec's own table, measured.
	at := AttributeCosts([]CostRow{
		{StepID: 1, RouteID: "claude-route", Provider: "anthropic", Model: "claude-opus", Cost: Cost{State: CostKnown, USD: 0.0412}},
		{StepID: 2, RouteID: "gpt-route", Provider: "openai", Model: "gpt-mini", Cost: Cost{State: CostKnown, USD: 0.0180}},
		{StepID: 3, RouteID: "local", Provider: "ollama", Model: "qwen2.5", Cost: Cost{State: CostUnknown, Reason: ReasonNoPriceForRoute}},
	})
	s := at.TotalSentence()
	if !strings.Contains(s, "at least") {
		t.Errorf("a total over an unknown read without 'at least' (D5): %q", s)
	}
	if !strings.Contains(s, "0.0592") {
		t.Errorf("the floor does not carry the measured part: %q", s)
	}
	if at.Total.State != CostUnknown {
		t.Errorf("the total state is %v — Add's rule stands: uncertain over anything is uncertain", at.Total.State)
	}
}

func TestAttributionGroupsByRoute(t *testing.T) {
	// O4: two routes, SAME provider and model, different prices. A ledger
	// grouped by provider would merge a 3/Mtok row with a 15/Mtok row and
	// the bill would be wrong in a way nothing could see.
	at := AttributeCosts([]CostRow{
		{StepID: 1, RouteID: "claude-cheap", Provider: "anthropic", Model: "claude", Cost: Cost{State: CostKnown, USD: 0.01}},
		{StepID: 2, RouteID: "claude-priced", Provider: "anthropic", Model: "claude", Cost: Cost{State: CostKnown, USD: 0.02}},
	})
	if len(at.Groups) != 2 {
		t.Fatalf("%d groups, want 2 — grouping collapsed onto provider+model (O4): %+v", len(at.Groups), at.Groups)
	}
	if at.Groups[0].RouteID != "claude-cheap" || at.Groups[1].RouteID != "claude-priced" {
		t.Errorf("group order is not first-seen: %+v", at.Groups)
	}
}

func TestAttributionSumsToTheTotal(t *testing.T) {
	at := AttributeCosts([]CostRow{
		{StepID: 1, RouteID: "a", Provider: "p", Model: "m", Cost: Cost{State: CostKnown, USD: 0.01}},
		{StepID: 2, RouteID: "a", Provider: "p", Model: "m", Cost: Cost{State: CostKnown, USD: 0.02}},
		{StepID: 3, RouteID: "b", Provider: "q", Model: "n", Cost: Cost{State: CostPartial, USDAtLeast: 0.005, Missing: []string{"CacheReadUSDPerMTok"}}},
	})
	var floor float64
	for _, g := range at.Groups {
		switch g.Cost.State {
		case CostKnown:
			floor += g.Cost.USD
		default:
			floor += g.Cost.USDAtLeast
		}
	}
	if at.Total.State != CostPartial || at.Total.USDAtLeast != floor {
		t.Fatalf("the groups sum to %v but the total says %+v — one computation must agree with its own parts (D16)", floor, at.Total)
	}
	if len(at.Total.Missing) != 1 || at.Total.Missing[0] != "CacheReadUSDPerMTok" {
		t.Errorf("the total lost what was unpriced: %v", at.Total.Missing)
	}
}

func TestNoFourthCostState(t *testing.T) {
	// M30 reuses M10's three states; the task that adds a fourth is the one
	// this grep catches. The states are the set, the set is asserted, and
	// every assignment in the ledger's own renderer goes through a switch
	// over exactly those three.
	if string(CostKnown) != "known" || string(CostPartial) != "partial" || string(CostUnknown) != "unknown" {
		t.Fatalf("M10's three states moved: %q %q %q", CostKnown, CostPartial, CostUnknown)
	}
	src, err := os.ReadFile("cost.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range stateAssign.FindAllStringSubmatch(string(src), -1) {
		switch m[1] {
		case "CostKnown", "CostPartial", "CostUnknown":
		default:
			t.Errorf("cost.go assigns State: %s — a fourth cost state (M30 says the three are complete; M30's reason is a field, not a state)", m[1])
		}
	}
}

func TestUnattributedStepsLandOnTheirOwnLine(t *testing.T) {
	// The pre-provenance row and the never-called-a-model step: absent,
	// spoken aloud at the bottom of the bill — never merged into a priced
	// line and never priced (P31, I5).
	at := AttributeCosts([]CostRow{
		{StepID: 1, RouteID: "a", Provider: "p", Model: "m", Cost: Cost{State: CostKnown, USD: 0.02}},
		{StepID: 2, Cost: Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage}},
	})
	last := at.Groups[len(at.Groups)-1]
	if last.RouteID != "" || last.Provider != "" || last.Model != "" {
		t.Fatalf("the unattributed line is not empty-tripled: %+v", last)
	}
	if at.Groups[0].RouteID != "a" {
		t.Errorf("the unknown line jumped above the known one: %+v", at.Groups)
	}
	// And an empty ledger is nobody-having-spoken, not a free loop.
	empty := AttributeCosts(nil)
	if empty.Total.State != CostUnknown || len(empty.Groups) != 0 {
		t.Errorf("no steps rendered as %+v", empty.Total)
	}
}
