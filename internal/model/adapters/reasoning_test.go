package adapters

import (
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// `Route.ReasoningEffort` — the field a real provider 400 asked for.
//
// # The refusal, verbatim
//
//	Function tools with reasoning_effort are not supported for gpt-6-luna in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// Aeon sent no `reasoning_effort` at all, so the model's own default applied and
// the tools were refused — and every Aeon step offers tools, so no step could
// run on that route. The field exists so the answer is one flag rather than a
// hand-written `extraBody`.

func withTools(route model.Route) model.ModelRequest {
	return model.ModelRequest{
		Route:    route,
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hi")}}},
		Tools: []model.ToolSpec{{
			Name: "read", Description: "read a file",
			Parameters: json.RawMessage(`{"type":"object"}`),
		}},
		MaxOutputTokens: 256,
	}
}

func TestOpenAISendsTheRoutesReasoningEffort(t *testing.T) {
	for _, effort := range model.ReasoningEfforts() {
		t.Run(effort, func(t *testing.T) {
			route := testRoute(ProviderOpenAI)
			route.ReasoningEffort = effort

			adapter, err := model.AdapterFor(ProviderOpenAI)
			if err != nil {
				t.Fatal(err)
			}
			req, err := adapter.ToWire(withTools(route))
			if err != nil {
				t.Fatal(err)
			}
			body := wireBody(t, req)

			if got := body["reasoning_effort"]; got != effort {
				t.Errorf("reasoning_effort is %v, want %q", got, effort)
			}
			// And the tools are still there: `none` is what makes the two
			// coexist, so a version that dropped the tools instead would
			// "work" and do nothing.
			if _, ok := body["tools"]; !ok {
				t.Error("the tools were dropped")
			}
		})
	}
}

func TestOpenAISendsNoReasoningEffortWhenTheRouteDoesNotSetOne(t *testing.T) {
	// **Not defaulted**, deliberately. A model with no reasoning dial answers
	// 400 `Unrecognized request argument supplied: reasoning_effort`, so
	// injecting it for every route would break the routes that work today —
	// and choosing which models need it by sniffing their names is folklore
	// with an expiry date.
	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(withTools(testRoute(ProviderOpenAI)))
	if err != nil {
		t.Fatal(err)
	}
	if _, present := wireBody(t, req)["reasoning_effort"]; present {
		t.Error("an unset reasoning effort was sent anyway")
	}
}

func TestAnthropicNeverSendsReasoningEffort(t *testing.T) {
	// It is OpenAI's parameter. A route carrying it — because the same route id
	// was repointed, or because somebody set it everywhere — must not produce a
	// 400 from the provider that has never heard of it.
	route := testRoute(ProviderAnthropic)
	route.ReasoningEffort = "none"

	adapter, err := model.AdapterFor(ProviderAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(withTools(route))
	if err != nil {
		t.Fatal(err)
	}
	body := wireBody(t, req)
	if _, present := body["reasoning_effort"]; present {
		t.Error("the Anthropic body carries reasoning_effort")
	}
	// And nothing was invented in its place: "medium" is not a number of
	// tokens, and translating one into the other would make Aeon the author of
	// a cost/quality tradeoff nobody chose.
	if _, present := body["thinking"]; present {
		t.Error("a reasoning effort was translated into a thinking budget")
	}
}

func TestExtraBodyStillOverridesTheReasoningEffort(t *testing.T) {
	// M11: the escape hatch merges LAST. An escape hatch that cannot override
	// is not one — and this is the field most likely to need overriding next,
	// when a provider renames it or adds a tier.
	route := testRoute(ProviderOpenAI)
	route.ReasoningEffort = "high"
	route.ExtraBody = json.RawMessage(`{"reasoning_effort":"none"}`)

	adapter, err := model.AdapterFor(ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	req, err := adapter.ToWire(withTools(route))
	if err != nil {
		t.Fatal(err)
	}
	if got := wireBody(t, req)["reasoning_effort"]; got != "none" {
		t.Errorf("reasoning_effort is %v, want the extraBody's value", got)
	}
}

func TestTheAcceptedSetIsSmallAndIncludesNone(t *testing.T) {
	// `none` is the one that matters: it is what the provider's own error
	// message names, and what makes function tools possible at all on a
	// reasoning model over chat/completions.
	if !model.ValidReasoningEffort("none") {
		t.Fatal("`none` is not accepted")
	}
	if !model.ValidReasoningEffort("") {
		t.Error("empty is not accepted; it must mean `leave the provider's default alone`")
	}
	for _, bad := range []string{"NONE", "off", "zero", "none ", "disabled", "low-ish"} {
		if model.ValidReasoningEffort(bad) {
			t.Errorf("%q is accepted; a typo must not reach the provider as a 400 about a field "+
				"the person believed they had set", bad)
		}
	}
}
