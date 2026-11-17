package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	// The real adapters, so route validation is tested against the real
	// registry rather than a fake one. `adapters` imports `model`, and
	// `protocol` imports `model`, so there is no cycle — and the import is in
	// the test binary only.
	_ "github.com/cjlapao/aeon/internal/model/adapters"
)

func routeTable(t *testing.T, env ...model.Route) *Table {
	t.Helper()
	tbl := newTestTable(t)
	tbl.Routes = NewMutableRoutes(env...)
	tbl.Models = &fakeStreamer{}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

func listRoutes(t *testing.T, tbl *Table) RoutesListResult {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesList, nil)
	if err != nil {
		t.Fatalf("routes.list: %v", err)
	}
	return res.(RoutesListResult)
}

func setRoute(t *testing.T, tbl *Table, p SetRouteParams) (SetRouteResult, error) {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesSet, mustJSON(t, p))
	if err != nil {
		return SetRouteResult{}, err
	}
	return res.(SetRouteResult), nil
}

// # THE test in this file
//
// **A credential goes in and never comes out.** Asserted structurally rather
// than by inspecting a value: `RouteView` must have no field that could carry a
// key. A test that checked `view.APIKey == ""` would pass against a type that
// has the field and happens to leave it empty today.
func TestARouteViewHasNoFieldThatCouldCarryACredential(t *testing.T) {
	rt := reflect.TypeOf(RouteView{})

	// Each name here was added deliberately. The two that forced this comment
	// are `ReasoningEffort` and `OpenAIEndpoint`: both are configuration
	// (`none|minimal|low|medium|high` and `chat|responses`), neither carries a
	// secret, and a route that cannot run a step has to be able to say so where
	// somebody looks at routes.
	allowed := map[string]bool{
		"ID": true, "Provider": true, "Model": true, "BaseURL": true,
		"HasKey": true, "KeyHint": true, "Source": true,
		"MaxOutputTokens": true, "ContextWindow": true, "Prices": true, "Priced": true,
		"ReasoningEffort": true, "OpenAIEndpoint": true,
		"DisplayName": true, "DisplayDescription": true, "Icon": true, "Capabilities": true, "ReasoningEfforts": true,
		// S18 T2, added deliberately like the two above them: a price
		// DECISION and a server-behaviour declaration — configuration
		// someone must be able to see, and neither is anywhere a key could
		// hide. The forbidden-name loop below still refuses anything that
		// could carry one.
		"NoPrice": true, "WireCapabilities": true,
		// S18 T3, added deliberately: the cached probe verdicts per declared
		// flag — a fact a probe earned, with its timestamp and model. No
		// field a credential could hide, and the forbidden-name loop still
		// stands over it.
		"CapabilityStates": true,
		// The route's extra request fields. It COULD hold a credential a person pasted there, so the view carries
		// it only through redactExtraBody (secret-looking keys shown as a placeholder), pinned by
		// TestExtraBodyViewNeverShowsASecretValue.
		"ExtraBody": true,
		// How the route's speech endpoints are spoken: one of a closed set of words (openai, synthesize, synthesize-stream, chat).
		"AudioAPI": true,
	}
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if !allowed[name] {
			t.Errorf("RouteView has an unexpected field %q. If it can carry a credential, then "+
				"not returning one depends on every call site remembering — which is what a "+
				"separate type exists to avoid", name)
		}
	}
	for _, forbidden := range []string{"APIKey", "Key", "Secret", "Token", "Credential", "Password"} {
		if _, exists := rt.FieldByName(forbidden); exists {
			t.Errorf("RouteView has a %q field", forbidden)
		}
	}
}

// And the same property on the wire, which is what a client actually sees.
func TestRoutesListNeverSerialisesACredential(t *testing.T) {
	const secret = "sk-ant-THIS-MUST-NOT-APPEAR-ANYWHERE"
	tbl := routeTable(t, model.Route{
		ID: "anthropic", Provider: "anthropic", Model: "m",
		APIKey: secret, MaxOutputTokens: 1024,
	})

	raw, err := json.Marshal(listRoutes(t, tbl))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("routes.list serialised the credential:\n%s", raw)
	}
	if strings.Contains(string(raw), "THIS-MUST-NOT") {
		t.Fatalf("part of the credential survived:\n%s", raw)
	}
	// And the same for a single set result.
	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "anthropic", Provider: "anthropic", Model: "m2", APIKey: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(res)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("routes.set echoed the credential back:\n%s", raw)
	}
}

// The hint identifies a key without reconstructing it — which is the actual
// question somebody has when two environments behave differently.
func TestTheKeyHintIsTheLastFourCharactersAndNotThePrefix(t *testing.T) {
	tbl := routeTable(t)
	_, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "m", APIKey: "sk-ant-api03-SECRETMIDDLE-wxyz",
	})
	if err != nil {
		t.Fatal(err)
	}
	view := listRoutes(t, tbl).Routes[0]

	if view.KeyHint != "…wxyz" {
		t.Fatalf("KeyHint = %q, want the last four", view.KeyHint)
	}
	// **Not the prefix.** A prefix identifies the key's type and, for some
	// providers, its project — which is more than "which key is this".
	if strings.Contains(view.KeyHint, "sk-") || strings.Contains(view.KeyHint, "api03") {
		t.Errorf("KeyHint leaks the prefix: %q", view.KeyHint)
	}
	if !view.HasKey {
		t.Error("HasKey is false for a route with a key")
	}
}

// A short key is a mistyped key, and the hint is how somebody notices they
// pasted half of one.
func TestAShortKeyIsMaskedRatherThanShown(t *testing.T) {
	tbl := routeTable(t)
	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "m", APIKey: "abc",
	}); err != nil {
		t.Fatal(err)
	}
	hint := listRoutes(t, tbl).Routes[0].KeyHint
	if strings.Contains(hint, "abc") {
		t.Fatalf("KeyHint shows a short key verbatim: %q", hint)
	}
	if hint != "•••" {
		t.Fatalf("KeyHint = %q, want three bullets so the length is still visible", hint)
	}
}

// **The reason the UI never has to hold a credential.**
func TestAnEmptyAPIKeyKeepsTheExistingCredential(t *testing.T) {
	const secret = "sk-ant-ORIGINAL-KEY-abcd"
	tbl := routeTable(t)

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "old-model", APIKey: secret,
	}); err != nil {
		t.Fatal(err)
	}

	// Edit the model only, sending no key — which is all a UI can do, since it
	// was never told the key.
	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "new-model",
	}); err != nil {
		t.Fatal(err)
	}

	store := tbl.Routes.(*MutableRoutes)
	route, ok := store.Route("r")
	if !ok {
		t.Fatal("the route vanished")
	}
	if route.Model != "new-model" {
		t.Errorf("Model = %q, want the edit to apply", route.Model)
	}
	if route.APIKey != secret {
		t.Fatalf("the credential was lost by an edit that did not mention it (%q). A UI would "+
			"then have to hold one in order to change a model name", route.APIKey)
	}
}

// "Unset the key" and "do not change the key" are different intentions, and one
// empty string cannot express both.
func TestClearKeyRemovesTheCredentialExplicitly(t *testing.T) {
	tbl := routeTable(t)
	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "m", APIKey: "sk-ant-abcd",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "m", ClearKey: true,
	}); err != nil {
		t.Fatal(err)
	}

	view := listRoutes(t, tbl).Routes[0]
	if view.HasKey {
		t.Fatal("clearKey did not remove the credential")
	}
	if view.KeyHint != "" {
		t.Errorf("a hint survived the clear: %q", view.KeyHint)
	}
	route, _ := tbl.Routes.(*MutableRoutes).Route("r")
	if route.APIKey != "" {
		t.Fatalf("the credential is still in memory: %q", route.APIKey)
	}
}

func TestDeletingARouteForgetsItsCredential(t *testing.T) {
	tbl := routeTable(t)
	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "m", APIKey: "sk-ant-abcd",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesDelete,
		mustJSON(t, DeleteRouteParams{ID: "r"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.(DeleteRouteResult).Deleted {
		t.Error("Deleted = false for a route that existed")
	}
	if _, ok := tbl.Routes.(*MutableRoutes).Route("r"); ok {
		t.Fatal("the route survived deletion, and so did its credential")
	}

	// Deleting again is not an error: a client racing its own refresh is
	// ordinary.
	res, err = tbl.Dispatch(context.Background(), "c1", MethodRoutesDelete,
		mustJSON(t, DeleteRouteParams{ID: "r"}))
	if err != nil {
		t.Fatalf("deleting a missing route errored: %v", err)
	}
	if res.(DeleteRouteResult).Deleted {
		t.Error("Deleted = true for a route that did not exist")
	}
}

// Refused at SAVE time, not at call time.
func TestARouteNamingAnUnimplementedProviderIsRefusedWhenSaved(t *testing.T) {
	tbl := routeTable(t)
	_, err := setRoute(t, tbl, SetRouteParams{ID: "r", Provider: "gemini", Model: "m"})
	if err == nil {
		t.Fatal("saved a route for a provider with no adapter. It would fail later, on a " +
			"different screen, after a prompt had been typed")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31103 {
		t.Fatalf("want -31103, got %v", err)
	}
	// And it lists what IS available, like every other not-found in this
	// protocol.
	for _, want := range []string{"gemini", "anthropic", "openai"} {
		if !strings.Contains(e.Data.What, want) {
			t.Errorf("the refusal does not name %q: %q", want, e.Data.What)
		}
	}
}

func TestARouteNeedsAnIDAndAModel(t *testing.T) {
	tbl := routeTable(t)
	for _, tc := range []struct {
		name string
		p    SetRouteParams
		want string
	}{
		{"no id", SetRouteParams{Provider: "anthropic", Model: "m"}, "id"},
		{"blank id", SetRouteParams{ID: "   ", Provider: "anthropic", Model: "m"}, "id"},
		{"no model", SetRouteParams{ID: "r", Provider: "anthropic"}, "model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setRoute(t, tbl, tc.p)
			if err == nil {
				t.Fatal("accepted an incomplete route")
			}
			if !strings.Contains(err.(*Error).Data.What, tc.want) {
				t.Errorf("the refusal does not name the missing part: %q", err.(*Error).Data.What)
			}
		})
	}
}

// A route the runtime touched is a runtime route, even if the environment
// seeded it — because its key may no longer be the environment's.
func TestEditingAnEnvironmentRouteMarksItAsRuntime(t *testing.T) {
	tbl := routeTable(t, model.Route{
		ID: "anthropic", Provider: "anthropic", Model: "m", APIKey: "from-env", MaxOutputTokens: 1024,
	})
	if got := listRoutes(t, tbl).Routes[0].Source; got != RouteSourceEnvironment {
		t.Fatalf("Source = %q, want environment", got)
	}

	if _, err := setRoute(t, tbl, SetRouteParams{ID: "anthropic", Provider: "anthropic", Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	if got := listRoutes(t, tbl).Routes[0].Source; got != RouteSourceRuntime {
		t.Fatalf("Source = %q after an edit, want runtime — saying `environment` would be wrong "+
			"once a value may no longer be the environment's", got)
	}
}

// **The UI must be able to say that nothing was saved.**
func TestRoutesListReportsThatNothingIsPersisted(t *testing.T) {
	tbl := routeTable(t)
	if listRoutes(t, tbl).Persisted {
		t.Fatal("routes.list claims persistence. The store is S4; a key set now lives in memory " +
			"and is gone on restart, and a settings page that looks like it saved is worse than " +
			"one that admits it did not")
	}
}

// A client needs the provider list to offer a choice rather than a free-text
// field that fails after a call.
func TestRoutesListReportsTheRegisteredProviders(t *testing.T) {
	tbl := routeTable(t)
	providers := listRoutes(t, tbl).Providers
	if len(providers) < 2 {
		t.Fatalf("providers = %v, want the registered adapters", providers)
	}
}

// Prices round-trip with their unit in every field name, so a UI cannot label a
// field ambiguously.
func TestPricesRoundTripWithTheirUnitInTheName(t *testing.T) {
	tbl := routeTable(t)
	_, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "anthropic", Model: "m",
		Prices: &PricesView{
			InputUSDPerMTok:  model.Float64Ptr(3),
			OutputUSDPerMTok: model.Float64Ptr(15),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	view := listRoutes(t, tbl).Routes[0]
	if !view.Priced {
		t.Error("Priced is false for a route with prices")
	}
	if view.Prices == nil || view.Prices.InputUSDPerMTok == nil || *view.Prices.InputUSDPerMTok != 3 {
		t.Fatalf("prices did not round-trip: %+v", view.Prices)
	}
	raw, _ := json.Marshal(view.Prices)
	if !strings.Contains(string(raw), "UsdPerMTok") {
		t.Errorf("a price field does not name its unit: %s", raw)
	}
	// An unpriced route reports so, which is what lets a UI warn BEFORE a call
	// rather than explain an unknown cost after one.
	if _, err := setRoute(t, tbl, SetRouteParams{ID: "r2", Provider: "openai", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range listRoutes(t, tbl).Routes {
		if v.ID == "r2" && v.Priced {
			t.Error("an unpriced route reports Priced = true")
		}
	}
}

// A route set through the protocol is immediately callable — the whole point of
// setting it from a UI rather than restarting the daemon with new environment.
func TestARouteSetThroughTheProtocolIsImmediatelyCallable(t *testing.T) {
	tbl := routeTable(t)
	tbl.Models = &fakeStreamer{
		texts: []string{"hi"},
		usage: model.Usage{InputTokens: model.IntPtr(5), OutputTokens: model.IntPtr(2)},
		cost:  model.Cost{State: model.CostKnown, USD: 0.00005},
		stop:  "end_turn",
	}

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "fresh", Provider: "anthropic", Model: "m", APIKey: "sk-ant-abcd",
		Prices: &PricesView{InputUSDPerMTok: model.Float64Ptr(3), OutputUSDPerMTok: model.Float64Ptr(15)},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := tbl.Dispatch(context.Background(), "c1", MethodAsk,
		mustJSON(t, AskParams{Route: "fresh", Prompt: "hello"}))
	if err != nil {
		t.Fatalf("a route set a moment ago is not callable: %v", err)
	}
	if res.(AskResult).Text != "hi" {
		t.Errorf("result = %+v", res)
	}
}

// --- reasoning effort --------------------------------------------------------

// `routes.set` validates the value rather than forwarding it.
//
// A typo would otherwise become a provider 400 about a field the person
// believed they had set, three layers from where they typed it.
func TestAnUnknownReasoningEffortIsRefusedAndListsTheRealOnes(t *testing.T) {
	tbl := routeTable(t)

	for _, bad := range []string{"NONE", "off", "disabled", "lowish", "no-reasoning"} {
		_, err := setRoute(t, tbl, SetRouteParams{
			ID: "r", Provider: "openai", Model: "m", ReasoningEffort: bad,
		})
		if err == nil {
			t.Fatalf("reasoningEffort %q was accepted", bad)
		}
		what := errorWhat(err)
		if !strings.Contains(what, "none, minimal, low, medium, high") {
			t.Errorf("the refusal does not list the accepted values: %s", what)
		}
		// And it says which value is the one that matters, because the person
		// hitting this is usually trying to make a step work.
		if !strings.Contains(what, "step use tools") {
			t.Errorf("the refusal does not say what `none` is for: %s", what)
		}
	}
}

// Surrounding whitespace is TRIMMED here, and that is the opposite of the rule
// for a handoff status (S5's T4, where `"done "` must fail).
//
// The difference is what happens next. A handoff status is compared later
// against an exact string, so trimming it there hides a mismatch that surfaces
// in another slice. This value is normalised once and then both STORED and SENT
// in its trimmed form — there is nothing left to disagree with it, and refusing
// a pasted trailing space would be a refusal with no benefit.
func TestSurroundingWhitespaceIsTrimmedFromTheReasoningEffort(t *testing.T) {
	tbl := routeTable(t)

	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", ReasoningEffort: "  none  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.ReasoningEffort != "none" {
		t.Errorf("the stored value is %q, want it trimmed", res.Route.ReasoningEffort)
	}
}

func TestEveryAcceptedReasoningEffortRoundTripsThroughTheView(t *testing.T) {
	tbl := routeTable(t)

	for _, effort := range model.ReasoningEfforts() {
		res, err := setRoute(t, tbl, SetRouteParams{
			ID: "r", Provider: "openai", Model: "m", ReasoningEffort: effort,
		})
		if err != nil {
			t.Fatalf("%s: %v", effort, err)
		}
		if res.Route.ReasoningEffort != effort {
			t.Errorf("the view reports %q, want %q", res.Route.ReasoningEffort, effort)
		}
	}
}

func TestAnOmittedReasoningEffortKeepsTheStoredOne(t *testing.T) {
	// The same rule as the key: empty means KEEP. A client editing a route's
	// prices must not silently drop a setting it never showed — and the UI's
	// form does exactly that round trip.
	tbl := routeTable(t)

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", ReasoningEffort: "none",
	}); err != nil {
		t.Fatal(err)
	}

	// An edit that says nothing about the effort.
	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", MaxOutputTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.ReasoningEffort != "none" {
		t.Errorf("an omitted reasoning effort dropped the stored %q", "none")
	}
	if res.Route.MaxOutputTokens != 8192 {
		t.Errorf("the edit did not apply: cap is %d", res.Route.MaxOutputTokens)
	}
}

func TestClearingTheReasoningEffortIsExplicit(t *testing.T) {
	// "Unset it" and "do not change it" are different intentions, and one
	// empty string cannot express both.
	tbl := routeTable(t)

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", ReasoningEffort: "high",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", ClearReasoningEffort: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.ReasoningEffort != "" {
		t.Errorf("the effort is still %q", res.Route.ReasoningEffort)
	}
}

func TestTheReasoningEffortIsNotACredentialAndIsReported(t *testing.T) {
	// It is configuration. The §4c rule is about credentials, and a route that
	// cannot run a step has to be able to say why in the one place somebody
	// looks at routes — so this field goes OUT as well as in, unlike the key.
	tbl := routeTable(t)

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", APIKey: "sk-test-secret-value",
		ReasoningEffort: "none",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := tbl.Dispatch(context.Background(), "c1", MethodRoutesList, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, ok := res.(RoutesListResult)
	if !ok {
		t.Fatalf("routes.list returned %T", res)
	}
	if len(listed.Routes) != 1 {
		t.Fatalf("%d routes", len(listed.Routes))
	}
	if listed.Routes[0].ReasoningEffort != "none" {
		t.Errorf("the listing does not report the effort: %+v", listed.Routes[0])
	}

	// And the credential still never comes out.
	raw := mustJSON(t, listed)
	if strings.Contains(string(raw), "sk-test-secret-value") {
		t.Fatal("the credential reached the wire")
	}
}

// --- set what you name, keep what you do not ---------------------------------

// `routes.set` must not clear a field the caller said nothing about.
//
// # Found by reading an error message back
//
// S5's reasoning-effort hint suggested `aeon routes set --id openai --provider
// openai --model gpt-6-luna --reasoning-effort none`, which was correct about
// the effort and silently cleared the route's BASE URL — and the next call went
// to the real provider instead of the local stand-in it had been pointed at.
//
// A verb that needs every field re-typed is a verb that loses one. The key
// already worked this way; now everything does.
func TestSettingOneFieldKeepsTheOthers(t *testing.T) {
	tbl := routeTable(t)

	// A fully-specified route.
	first, err := setRoute(t, tbl, SetRouteParams{
		ID: "openai", Provider: "openai", Model: "gpt-6-luna",
		BaseURL: "http://127.0.0.1:8124/v1", APIKey: "sk-test-key-value",
		MaxOutputTokens: 8192,
		Prices: &PricesView{
			InputUSDPerMTok:  model.Float64Ptr(1.25),
			OutputUSDPerMTok: model.Float64Ptr(10),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Route.BaseURL == "" || !first.Route.Priced || !first.Route.HasKey {
		t.Fatalf("the route was not stored as given: %+v", first.Route)
	}

	// The command the hint suggests: the id, the provider, the model and the
	// one field being changed.
	second, err := setRoute(t, tbl, SetRouteParams{
		ID: "openai", Provider: "openai", Model: "gpt-6-luna", ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatal(err)
	}

	if second.Route.ReasoningEffort != "none" {
		t.Errorf("the change did not apply: %q", second.Route.ReasoningEffort)
	}
	if second.Route.BaseURL != "http://127.0.0.1:8124/v1" {
		t.Errorf("THE BASE URL WAS CLEARED (%q) — the next call would go to the public endpoint",
			second.Route.BaseURL)
	}
	if second.Route.MaxOutputTokens != 8192 {
		t.Errorf("the output cap was reset to %d", second.Route.MaxOutputTokens)
	}
	if !second.Route.Priced {
		t.Error("the prices were dropped; every later cost would read `unknown`")
	}
	if !second.Route.HasKey {
		t.Error("the credential was dropped")
	}
}

func TestANewRouteStillGetsTheDefaultOutputCap(t *testing.T) {
	// "Keep what you do not name" must not become "a new route has no cap" —
	// one provider refuses a call without one (M16).
	tbl := routeTable(t)

	res, err := setRoute(t, tbl, SetRouteParams{ID: "fresh", Provider: "openai", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.MaxOutputTokens != defaultRouteOutputCap {
		t.Errorf("a new route's cap is %d, want the default %d",
			res.Route.MaxOutputTokens, defaultRouteOutputCap)
	}
}

func TestAnExplicitValueStillWins(t *testing.T) {
	// The complement: "keep" must not become "ignore".
	tbl := routeTable(t)

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m",
		BaseURL: "http://first.invalid", MaxOutputTokens: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m",
		BaseURL: "http://second.invalid", MaxOutputTokens: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.BaseURL != "http://second.invalid" {
		t.Errorf("the base URL is %q", res.Route.BaseURL)
	}
	if res.Route.MaxOutputTokens != 2000 {
		t.Errorf("the cap is %d", res.Route.MaxOutputTokens)
	}
}

// --- openai endpoint ----------------------------------------------------------

func TestAnUnknownOpenAIEndpointIsRefusedAndListsTheRealOnes(t *testing.T) {
	tbl := routeTable(t)

	for _, bad := range []string{"CHAT", "v1/responses", "completions", "Responses"} {
		_, err := setRoute(t, tbl, SetRouteParams{
			ID: "r", Provider: "openai", Model: "m", OpenAIEndpoint: bad,
		})
		if err == nil {
			t.Fatalf("openaiEndpoint %q was accepted", bad)
		}
		what := errorWhat(err)
		if !strings.Contains(what, "chat, responses") {
			t.Errorf("the refusal does not list the accepted values: %s", what)
		}
		if !strings.Contains(what, "reason while using tools") {
			t.Errorf("the refusal does not say what `responses` is for: %s", what)
		}
	}
}

// Surrounding whitespace is trimmed, like the reasoning effort — it is
// normalised once and then both stored and sent in its trimmed form, so there
// is nothing left to disagree with it.
func TestSurroundingWhitespaceIsTrimmedFromTheOpenAIEndpoint(t *testing.T) {
	tbl := routeTable(t)

	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", OpenAIEndpoint: "  responses  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.OpenAIEndpoint != "responses" {
		t.Errorf("the stored value is %q, want it trimmed", res.Route.OpenAIEndpoint)
	}
}

func TestEveryAcceptedOpenAIEndpointRoundTripsThroughTheView(t *testing.T) {
	tbl := routeTable(t)

	for _, endpoint := range model.OpenAIEndpoints() {
		res, err := setRoute(t, tbl, SetRouteParams{
			ID: "r", Provider: "openai", Model: "m", OpenAIEndpoint: endpoint,
		})
		if err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
		if res.Route.OpenAIEndpoint != endpoint {
			t.Errorf("the view reports %q, want %q", res.Route.OpenAIEndpoint, endpoint)
		}
	}
}

func TestAnOmittedOpenAIEndpointKeepsTheStoredOne(t *testing.T) {
	// The same "set what you name, keep what you do not" rule as the base URL
	// and the reasoning effort.
	tbl := routeTable(t)

	if _, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", OpenAIEndpoint: "responses",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := setRoute(t, tbl, SetRouteParams{
		ID: "r", Provider: "openai", Model: "m", MaxOutputTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.OpenAIEndpoint != "responses" {
		t.Errorf("an omitted endpoint dropped the stored %q", "responses")
	}
}

func TestANewRouteDefaultsToTheChatEndpoint(t *testing.T) {
	// Empty means chat, so every route created before this field existed
	// — and every route created without naming it — behaves identically.
	tbl := routeTable(t)

	res, err := setRoute(t, tbl, SetRouteParams{ID: "r", Provider: "openai", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Route.OpenAIEndpoint != "" {
		t.Errorf("a new route's endpoint is %q, want unset (defaults to chat)", res.Route.OpenAIEndpoint)
	}
}
