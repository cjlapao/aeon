package protocol

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// refusalText digs the specifics out of an *Error the way routes_test.go's
// -31103 assertions do: the registry entry carries the shape of the answer,
// and it is WithWhat that carries the route's name.
func refusalText(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want an *Error, got %v", err)
	}
	return e.Data.What + " " + e.Data.Means
}

// S18 T2 at the socket: the O8 refusal (new persisted routes only), the
// wire-flag patch semantics, and the view's honest rendering.

// fakePersist is a RoutePersistence you can pre-load, which is what makes
// "this row predates the rule" (stored) testable against "this would create
// a new row" (absent / environment) without a database.
type fakePersist struct {
	saved map[string]model.Route
}

func (f *fakePersist) Save(r model.Route) error {
	if f.saved == nil {
		f.saved = map[string]model.Route{}
	}
	f.saved[r.ID] = r
	return nil
}
func (f *fakePersist) Remove(id string) error {
	delete(f.saved, id)
	return nil
}
func (f *fakePersist) Load() ([]model.Route, error) {
	var out []model.Route
	for _, r := range f.saved {
		out = append(out, r)
	}
	return out, nil
}

func persistedRouteTable(t *testing.T, preloaded ...model.Route) (*Table, *fakePersist) {
	t.Helper()
	tbl := newTestTable(t)
	m := NewMutableRoutes()
	p := &fakePersist{saved: map[string]model.Route{}}
	for _, r := range preloaded {
		p.saved[r.ID] = r
	}
	if err := m.WithPersistence(p); err != nil {
		t.Fatal(err)
	}
	tbl.Routes = m
	tbl.Models = &fakeStreamer{}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, p
}

func pricedParams(id string) SetRouteParams {
	return SetRouteParams{
		ID: id, Provider: "anthropic", Model: "claude-test",
		Prices: &PricesView{InputUSDPerMTok: &[]float64{3}[0]},
	}
}

// TestMissingPriceIsAConfigurationError — the socket refuses a NEW persisted
// route with neither prices nor the noPrice declaration, and it refuses
// BEFORE the in-memory Set: "usable now but not saved" would be the wrong
// story for a configuration error (that sentence is for disk failures).
func TestMissingPriceIsAConfigurationError(t *testing.T) {
	tbl, _ := persistedRouteTable(t)

	_, err := setRoute(t, tbl, SetRouteParams{ID: "typo", Provider: "anthropic", Model: "claude-test"})
	if err == nil {
		t.Fatal("a new unpriced route was accepted (O8)")
	}
	msg := refusalText(t, err)
	for _, want := range []string{"typo", "noPrice", "unknown"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q: %s", want, msg)
		}
	}
	if _, ok := tbl.Routes.(*MutableRoutes).Route("typo"); ok {
		t.Error("the refused route is live in memory — the refusal preceded nothing")
	}
}

// TestEnvironmentRoutesAreUntouchedByTheGate — an environment route is
// ephemeral configuration, not the ledger: it lists unpriced and the socket
// works with it. It is only the attempt to PERSIST it that asks for the
// decision, and that path is a NEW persisted row — tested above.
func TestEnvironmentRoutesAreUntouchedByTheGate(t *testing.T) {
	env := model.Route{ID: "env-anthropic", Provider: "anthropic", Model: "claude-sonnet-4-5",
		APIKey: "sk-test", MaxOutputTokens: 1024}
	tbl := newTestTable(t)
	m := NewMutableRoutes(env)
	if err := m.WithPersistence(&fakePersist{saved: map[string]model.Route{}}); err != nil {
		t.Fatal(err)
	}
	tbl.Routes = m
	tbl.Models = &fakeStreamer{}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res := listRoutes(t, tbl)
	if len(res.Routes) != 1 || res.Routes[0].ID != "env-anthropic" {
		t.Fatalf("the environment route vanished: %+v", res.Routes)
	}
	if res.Routes[0].Priced || res.Routes[0].NoPrice {
		t.Error("an environment route claims a price decision it never made")
	}
}

// TestGrandfatheredStoredRouteSavesUnpriced — a row that predates the rule
// keeps saving as it stands. The daemon must not refuse to edit history
// because the operator did not re-decide yesterday.
func TestGrandfatheredStoredRouteSavesUnpriced(t *testing.T) {
	old := model.Route{ID: "old-free", Provider: "openai", Model: "gpt-4o", MaxOutputTokens: 1024}
	tbl, p := persistedRouteTable(t, old)

	res, err := setRoute(t, tbl, SetRouteParams{ID: "old-free", Provider: "openai", Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("editing a grandfathered route tripped O8: %v", err)
	}
	if res.Route.Priced || res.Route.NoPrice {
		t.Errorf("the edit invented a price decision: priced=%v noPrice=%v", res.Route.Priced, res.Route.NoPrice)
	}
	if _, ok := p.saved["old-free"]; !ok {
		t.Error("the write-through did not happen")
	}
}

// TestNoPriceIsADeliberateDeclaration — through the socket, the declaration
// lands on the view and the row.
func TestNoPriceIsADeliberateDeclaration(t *testing.T) {
	tbl, p := persistedRouteTable(t)
	fresh := true

	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "local-llama", Provider: "openai", Model: "llama3.3",
		BaseURL: "http://127.0.0.1:11434/v1",
		NoPrice: &fresh,
	})
	if err != nil {
		t.Fatalf("the declared-unmetered route was refused: %v", err)
	}
	if !res.Route.NoPrice || res.Route.Priced {
		t.Errorf("view says priced=%v noPrice=%v, want false/true", res.Route.Priced, res.Route.NoPrice)
	}
	if saved := p.saved["local-llama"]; !saved.NoPrice {
		t.Error("the write-through lost the declaration")
	}
}

// TestWireFlagsPatchKeepAndReject — absent keeps, [] declares none, unknown
// fails listing the accepted set; and the fields never touch the metadata
// `capabilities` on the same route.
func TestWireFlagsPatchKeepAndReject(t *testing.T) {
	tbl, _ := persistedRouteTable(t)

	caps := []string{"thinking", "tools"}
	res, err := setRoute(t, tbl, func() SetRouteParams {
		p := pricedParams("wirey")
		p.WireCapabilities = &caps
		p.Capabilities = &[]string{"thinking"} // the metadata field, same word, other meaning
		return p
	}())
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.WireCapabilities == nil ||
		strings.Join(*res.Route.WireCapabilities, ",") != "tools,thinking" {
		t.Fatalf("wire flags on the view are %v", res.Route.WireCapabilities)
	}
	if res.Route.Capabilities == nil || strings.Join(*res.Route.Capabilities, ",") != "thinking" {
		t.Errorf("the metadata field interfered: %v", res.Route.Capabilities)
	}

	// Absent keeps.
	kept, err := setRoute(t, tbl, pricedParams("wirey"))
	if err != nil {
		t.Fatal(err)
	}
	if kept.Route.WireCapabilities == nil || strings.Join(*kept.Route.WireCapabilities, ",") != "tools,thinking" {
		t.Errorf("omitting the field dropped the declaration: %v", kept.Route.WireCapabilities)
	}

	// [] declares none.
	none, err := setRoute(t, tbl, func() SetRouteParams {
		p := pricedParams("wirey")
		p.WireCapabilities = &[]string{}
		return p
	}())
	if err != nil {
		t.Fatal(err)
	}
	if none.Route.WireCapabilities == nil || len(*none.Route.WireCapabilities) != 0 {
		t.Errorf("DECLARED-NONE came back as %v", none.Route.WireCapabilities)
	}

	// Unknown fails, listing the accepted set.
	bad, err := setRoute(t, tbl, func() SetRouteParams {
		p := pricedParams("wirey")
		p.WireCapabilities = &[]string{"vision"}
		return p
	}())
	if err == nil {
		t.Fatalf("the socket accepted a flag outside the closed set: %+v", bad)
	}
	msg := refusalText(t, err)
	for _, want := range []string{"vision", "parallel-tools", "streaming-usage"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not list %q: %s", want, msg)
		}
	}
}

// TestViewsCarryWireFlagsWithoutStates — the view carries the CLAIMS; the
// three states are the probe's record (T3). A view that could say `verified`
// before probes exist is the confident wrong answer P31 forbids.
func TestViewsCarryWireFlagsWithoutStates(t *testing.T) {
	// RouteView's field set, asserted structurally rather than trusted:
	// no field on the view may carry a state or a verification today.
	typ := reflect.TypeOf(RouteView{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		lower := strings.ToLower(name)
		if strings.Contains(lower, "verified") || strings.Contains(lower, "contradicted") ||
			lower == "state" || lower == "states" {
			t.Errorf("RouteView carries %q — states arrive with the probe (T3), not before it", name)
		}
	}
}
