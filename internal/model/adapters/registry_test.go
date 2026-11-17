package adapters_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/model/adapters"
)

// # This file is in `adapters_test`, not `adapters`
//
// Deliberately external: it reaches the adapters only the way the rest of the
// product does — through `model.AdapterFor` after a blank import. A test in the
// same package could call `newOpenAI()` directly and would prove nothing about
// whether registration happens.

func TestTheRegistryResolvesAnAdapterForEachProviderID(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			a, err := model.AdapterFor(provider)
			if err != nil {
				t.Fatalf("no adapter for %q: %v", provider, err)
			}
			if a.Provider() != provider {
				t.Errorf("adapter reports %q, registered under %q", a.Provider(), provider)
			}
		})
	}
}

// O6 — the failure names what IS available. "provider not found" on its own is
// a support question, and the list is short enough to print.
func TestAnUnknownProviderFailsWithANamedErrorListingTheConfiguredIDs(t *testing.T) {
	_, err := model.AdapterFor("gemini")
	if err == nil {
		t.Fatal("resolved an adapter for an unregistered provider")
	}
	if !strings.Contains(err.Error(), "gemini") {
		t.Errorf("the error does not name what was asked for: %v", err)
	}
	for _, want := range []string{"openai", "anthropic"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not list %q as available: %v", want, err)
		}
	}
}

// Each call gets a FRESH adapter.
//
// An adapter accumulates fragment state across a stream, so a shared instance
// would interleave two concurrent calls' tool arguments — which appears as a
// corrupted tool call under load and nowhere else.
//
// # Why this counts factory invocations instead of comparing pointers
//
// The first version asserted `a != b`, and it **failed on correct code**: an
// adapter with no fields yet is a zero-sized struct, and Go is free to return
// the same address for every zero-size allocation. The test was asserting a
// property of the allocator.
//
// Counting invocations asserts the actual contract — the registry calls the
// factory per resolution and never caches — and keeps doing so once the real
// adapters have fields.
func TestEachResolutionCallsTheFactoryAgain(t *testing.T) {
	calls := 0
	model.RegisterAdapter("counting-provider", func() model.Adapter {
		calls++
		return &stubAdapter{}
	})

	for i := 1; i <= 3; i++ {
		if _, err := model.AdapterFor("counting-provider"); err != nil {
			t.Fatal(err)
		}
		if calls != i {
			t.Fatalf("after %d resolutions the factory ran %d times; the registry is caching "+
				"an instance, and two concurrent calls would share its fragment state", i, calls)
		}
	}
}

// M12's proof, and the test S18's cost estimate rests on.
//
// A fictional provider, registered and driven end to end. **If this passes
// while touching only `adapters/`, adding the OpenAI-compatible path at S18 is
// one file.**
func TestAStubAdapterForAFictionalProviderWorksEndToEnd(t *testing.T) {
	model.RegisterAdapter("acme-llm", func() model.Adapter { return &stubAdapter{} })

	a, err := model.AdapterFor("acme-llm")
	if err != nil {
		t.Fatalf("a newly registered provider did not resolve: %v", err)
	}

	req := model.ModelRequest{
		Route:    model.Route{ID: "acme", Provider: "acme-llm", Model: "acme-1", MaxOutputTokens: 100},
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}}},
	}

	httpReq, err := a.ToWire(req)
	if err != nil {
		t.Fatalf("ToWire: %v", err)
	}
	if httpReq == nil {
		t.Fatal("ToWire returned no request")
	}

	frames := make(chan model.Frame, 2)
	frames <- model.Frame{Data: `{"text":"hi"}`}
	close(frames)

	var got []model.Delta
	for d := range a.FromWire(context.Background(), frames) {
		got = append(got, d)
	}
	if len(got) != 2 || got[0].Kind != model.DeltaText || got[1].Kind != model.DeltaStop {
		t.Fatalf("deltas = %+v, want a text then a stop", got)
	}

	usage, stop, dropped := a.Finalise()
	if usage.InputTokens == nil || *usage.InputTokens != 7 {
		t.Errorf("usage did not survive Finalise: %+v", usage)
	}
	if stop != "end_turn" {
		t.Errorf("stopReason = %q", stop)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 — unmapped content is dropped and COUNTED (M4)", dropped)
	}
}

// A stub provider, written only against `model.Adapter`. Its existence inside a
// test file is the point: nothing outside this directory had to change.
type stubAdapter struct{ sawFrames int }

func (*stubAdapter) Provider() string { return "acme-llm" }

func (*stubAdapter) ToWire(req model.ModelRequest) (*http.Request, error) {
	return http.NewRequest(http.MethodPost, "https://acme.example/v1/stream", strings.NewReader("{}"))
}

func (s *stubAdapter) FromWire(ctx context.Context, frames <-chan model.Frame) <-chan model.Delta {
	out := make(chan model.Delta)
	go func() {
		defer close(out)
		for range frames {
			s.sawFrames++
			out <- model.Delta{Kind: model.DeltaText, Text: "hi"}
		}
		out <- model.Delta{Kind: model.DeltaStop, Reason: "end_turn"}
	}()
	return out
}

func (*stubAdapter) Finalise() (model.Usage, string, int) {
	return model.Usage{InputTokens: model.IntPtr(7)}, "end_turn", 1
}

// A duplicate registration panics rather than letting initialisation order
// decide which adapter a provider gets.
func TestRegisteringAProviderTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate provider registration did not panic")
		}
	}()
	model.RegisterAdapter("openai", func() model.Adapter { return &stubAdapter{} })
}

// M8, enforced by the TYPE rather than by a rule in a document.
//
// If `Delta` has no way to express a fragment, an adapter cannot yield one —
// which is stronger than any amount of care, because the careless case does not
// compile.
func TestDeltaHasNoRepresentationForAPartialToolUse(t *testing.T) {
	rt := reflect.TypeOf(model.Delta{})

	// Every field, enumerated. A new field on Delta must be considered against
	// this test rather than added past it.
	allowed := map[string]bool{"Kind": true, "Text": true, "Block": true, "Reason": true}
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if !allowed[name] {
			t.Errorf("Delta has an unexpected field %q. If it can carry a fragment of a tool "+
				"call — an index, a partial arguments string, a more-to-come flag — then M8 is "+
				"a rule in a document again rather than a property of the type", name)
		}
	}

	// And the specific shapes a fragment would take, named so the intent
	// survives a refactor.
	for _, forbidden := range []string{
		"Index", "Partial", "Fragment", "More", "Done", "ArgumentsSoFar", "Incomplete", "Delta",
	} {
		if _, exists := rt.FieldByName(forbidden); exists {
			t.Errorf("Delta has a %q field, which is how a partial tool call would be expressed", forbidden)
		}
	}

	// Block is a pointer, so "no tool call in this delta" is representable
	// without a sentinel — and its doc comment says it is always complete.
	field, _ := rt.FieldByName("Block")
	if field.Type.Kind() != reflect.Pointer {
		t.Error("Delta.Block is not a pointer; absence would need a sentinel value")
	}
}

// The doc comment is part of the contract here: `Block` being complete is not
// checkable at compile time, so the statement is what a reader relies on.
func TestDeltaBlockIsDocumentedAsAlwaysComplete(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	idx := strings.Index(text, "// Block is set only for DeltaToolUse")
	if idx < 0 {
		t.Fatal("Delta.Block has no doc comment")
	}
	if !strings.Contains(text[idx:idx+400], "always complete") {
		t.Error("Delta.Block's doc comment does not state that it is always complete")
	}
}

// `ToWire` builds a request and sends nothing.
//
// Asserted with a transport that PANICS: anything that reached the network
// would take the test down with a stack trace pointing at the offender, rather
// than passing quietly on a machine that happens to have no credentials.
func TestToWirePerformsNoIO(t *testing.T) {
	// Installed as the default transport for the duration, so even a stray
	// `http.Get` inside an adapter is caught rather than only a call through an
	// injected client.
	original := http.DefaultTransport
	http.DefaultTransport = panicTransport{t: t}
	originalClient := http.DefaultClient.Transport
	http.DefaultClient.Transport = panicTransport{t: t}
	t.Cleanup(func() {
		http.DefaultTransport = original
		http.DefaultClient.Transport = originalClient
	})

	for _, provider := range model.RegisteredProviders() {
		a, err := model.AdapterFor(provider)
		if err != nil {
			t.Fatal(err)
		}
		req := model.ModelRequest{
			Route: model.Route{
				ID: "t", Provider: provider, Model: "m", MaxOutputTokens: 64,
				BaseURL: "https://example.invalid", APIKey: "<redacted>",
			},
			Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("hello")}}},
		}
		// A panic here is the failure, and it names the provider.
		if _, err := a.ToWire(req); err != nil {
			t.Logf("%s ToWire returned an error (acceptable; the point is that it sent nothing): %v",
				provider, err)
		}
	}
}

type panicTransport struct{ t *testing.T }

func (p panicTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	panic("ToWire performed I/O: " + r.Method + " " + r.URL.String() +
		" — it must build a request and return it, so client.go stays the only place a request is sent")
}

// `model` names one provider id, and this asserts the two spellings agree.
//
// `model.ProviderOpenAI` exists because a Route has to answer "does this API
// refuse tools while reasoning is on" before any adapter is chosen, and `model`
// cannot import this package (the adapter contract is made of `model`'s types,
// so it would be a cycle). The duplication is bounded by this test: rename
// either constant and it fails here rather than silently disabling the rule.
func TestTheProviderIdThatModelKnowsMatchesThisPackages(t *testing.T) {
	if model.ProviderOpenAI != adapters.ProviderOpenAI {
		t.Fatalf("model says %q and adapters says %q — the route's ToolsRefuseReasoning rule "+
			"would never fire", model.ProviderOpenAI, adapters.ProviderOpenAI)
	}
}
