package store

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T2 at the store: the O8 write gate, the grandfathering of pre-rule
// rows, and the persistence of the Q8 wire flags. The rule lives at the gate
// new rows enter — the read side stays permissive, because a daemon that
// refuses its own history is rejecting the past instead of protecting the
// future.

// unmetered marks a fixture route as DECLARED unmetered, so a test about
// something other than prices does not trip O8 — the same declaration the
// app's editor will carry as a checkbox. A fixture that silently predates
// the rule (a raw INSERT) is used where the point IS the pre-rule row.
func unmetered(r model.Route) model.Route {
	r.NoPrice = true
	return r
}

func bareRoute(id string) model.Route {
	return model.Route{ID: id, Provider: "anthropic", Model: "claude-test", MaxOutputTokens: 1024}
}

func priced(id string) model.Route {
	r := bareRoute(id)
	r.Prices = &model.Prices{InputUSDPerMTok: &[]float64{3}[0]}
	return r
}

// TestMissingPriceIsAConfigurationError — O8. A NEW route that is neither
// priced nor declared unmetered is refused at the gate, naming the route,
// and nothing is written.
func TestMissingPriceIsAConfigurationError(t *testing.T) {
	s := openTemp(t)

	err := s.UpsertRoute(bareRoute("typo-route"))
	if err == nil {
		t.Fatal("a silently unpriced new route was stored (O8)")
	}
	for _, want := range []string{"typo-route", "noPrice", "unknown"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if _, err := s.ReadRoute("typo-route"); err == nil {
		t.Error("the refused route exists — the gate must precede the write")
	}
}

// TestNoPriceIsADeliberateDeclaration — the same route loads when the
// absence is a DECISION, and reads back as one.
func TestNoPriceIsADeliberateDeclaration(t *testing.T) {
	s := openTemp(t)

	r := bareRoute("local-llama")
	r.NoPrice = true
	if err := s.UpsertRoute(r); err != nil {
		t.Fatalf("the declared-unmetered route was refused: %v", err)
	}
	got, err := s.ReadRoute("local-llama")
	if err != nil {
		t.Fatal(err)
	}
	if !got.NoPrice {
		t.Error("the declaration did not survive the round trip")
	}
	if got.Prices.Any() {
		t.Error("an unmetered route came back priced")
	}
}

// TestPricedRoutesAreUntouchedByTheGate — O8 refuses an ABSENCE of decision,
// not a price.
func TestPricedRoutesAreUntouchedByTheGate(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertRoute(priced("metered")); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadRoute("metered")
	if err != nil {
		t.Fatal(err)
	}
	if got.NoPrice {
		t.Error("a priced route came back declared unmetered")
	}
}

// TestGrandfatheredUnpricedRowStillLoadsAndSaves — the containment half. A
// row written before the rule (the raw INSERT below, the statement as it
// stood pre-0028) loads, and editing it does not trip a rule it predates.
// The daemon must not die — or refuse an edit — over a row it accepted under
// yesterday's contract.
func TestGrandfatheredUnpricedRowStillLoadsAndSaves(t *testing.T) {
	s := openTemp(t)

	if _, err := s.w.ExecContext(context.Background(),
		`INSERT INTO route (id, provider, model, max_output_tokens, context_window, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		"old-free", "openai", "gpt-4o", 1024, 0, s.Now(), s.Now()); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadRoute("old-free")
	if err != nil {
		t.Fatalf("a pre-O8 route must still load: %v", err)
	}
	if got.NoPrice {
		t.Error("absence of the declaration was read as a declaration — 'nobody decided' is its own answer")
	}

	r := bareRoute("old-free")
	r.Provider = "openai"
	r.Model = "gpt-4o-mini"
	r.MaxOutputTokens = 2048
	if err := s.UpsertRoute(r); err != nil {
		t.Fatalf("editing a grandfathered route tripped the rule it predates: %v", err)
	}
	got, err = s.ReadRoute("old-free")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-4o-mini" {
		t.Errorf("the edit did not land: %q", got.Model)
	}
}

// TestWireCapabilitiesRoundTrip — canonical order survives the database,
// nil stays nil (UNDECLARED) and [] stays [] (DECLARED-NONE), and an
// unknown flag is refused at the store gate too (the socket is not the only
// door: `UpsertRoute` is the one place that cannot be bypassed).
func TestWireCapabilitiesRoundTrip(t *testing.T) {
	s := openTemp(t)

	r := priced("wirey")
	r.WireCapabilities = []string{"thinking", "tools"}
	if err := s.UpsertRoute(r); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadRoute("wirey")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.WireCapabilities, ",") != "tools,thinking" {
		t.Errorf("wire flags read back %v, want canonical order", got.WireCapabilities)
	}

	n := priced("undeclared")
	if err := s.UpsertRoute(n); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ReadRoute("undeclared")
	if got.WireCapabilities != nil {
		t.Errorf("an undeclared route read back %v — nil and [] are different answers", got.WireCapabilities)
	}

	e := priced("declared-none")
	e.WireCapabilities = []string{}
	if err := s.UpsertRoute(e); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ReadRoute("declared-none")
	if got.WireCapabilities == nil || len(got.WireCapabilities) != 0 {
		t.Errorf("DECLARED-NONE read back as %#v", got.WireCapabilities)
	}

	bad := priced("bad-flag")
	bad.WireCapabilities = []string{"vision"}
	if err := s.UpsertRoute(bad); err == nil {
		t.Error("the store accepted a flag outside the closed set")
	} else if !strings.Contains(err.Error(), "vision") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
}

// TestTheNewColumnsAreWhatTheAlterSaysTheyAre — asserted against the live
// schema, never against the migration text.
func TestTheNewColumnsAreWhatTheAlterSaysTheyAre(t *testing.T) {
	s := openTemp(t)
	rows, err := s.r.Query(
		`SELECT name, type, "notnull", dflt_value FROM pragma_table_info('route')
		WHERE name IN ('wire_capabilities','no_price') ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := map[string][2]string{}
	for rows.Next() {
		var name, typ string
		var notnull int
		var dflt interface{}
		if err := rows.Scan(&name, &typ, &notnull, &dflt); err != nil {
			t.Fatal(err)
		}
		d := ""
		if dflt != nil {
			d = dflt.(string)
		}
		found[name] = [2]string{typ + "/" + map[bool]string{true: "notnull", false: "null"}[notnull == 1], d}
	}
	if got := found["wire_capabilities"]; got[0] != "TEXT/null" {
		t.Errorf(`wire_capabilities is %v, want TEXT NULL — NULL means UNDECLARED and DEFAULT '[]' would erase that`, got)
	}
	if got := found["no_price"]; got[0] != "INTEGER/notnull" {
		t.Errorf("no_price is %v, want INTEGER NOT NULL", got)
	}
}
