package model

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// embed.go — embedding is a ROUTE (V16, M12), so the embedding call is
// method on the ONE client, not a second provider path (I8, N9): same
// Client, same HTTP config, same adapter registry, same key handling
// (route.APIKey is filled by the same door, read the same way, and is
// unserialisable so it cannot leak through a route print), and cost computed
// HERE at the same single site everything else computes it (O1/I4 —
// "Cost is computed HERE and nowhere else" holds across both files).
//
// Q5 (S22 T3), answered in the shape of this file: the Route type needed no
// generalising. Kind/provider/model/base-URL/prices/noPrice all apply to an
// embedding model unchanged; the model-purpose vocabulary gained the word
// "embedding" (metadata.go — a purpose, like text and image), and that is
// the whole generalisation. What was chat-shaped was the CALLER — Client
// only ever spoke chat — and the fix is this method, not a second provider
// integration. A parallel path would have meant two places computing cost,
// two places holding a key, and two notions of what a provider is; there is
// still exactly one of each.

// Embedder is the optional adapter half of embedding: an adapter whose
// provider speaks an embeddings endpoint implements it, and one that does
// not is refused at the door naming what is available. It is OPTIONAL on
// Adapter (a separate interface, not a new method on the closed one) for
// exactly the reason Anthropic is in the registry without it: an embedding
// refusal is a provider FACT, and forcing every adapter to answer the
// question would push "this provider has no embeddings" into six switch
// statements instead of one type assertion.
type Embedder interface {
	// EmbedToWire builds the HTTP request. NO I/O (the Adapter contract,
	// kept to the letter): it returns the request and sends nothing, which
	// is what keeps client.go the only place a request is sent.
	EmbedToWire(route Route, texts []string) (*http.Request, error)

	// EmbedFromWire translates a response body into vectors and usage. The
	// vectors must come back in the order the texts went out — the caller
	// pairs them with chunks by position, so a silent reorder is a wrong
	// index that no error would ever name.
	EmbedFromWire(status int, body []byte) ([][]float32, Usage, error)
}

// Embed sends texts to the route's embeddings endpoint and returns the
// vectors, the usage the provider reported, and the cost through the SAME
// ComputeCost every other call pays (three states, V10; unmetered routes are
// `unknown` with a reason — a local embedding model has no price Aeon knows,
// and $0.00 would be a claim (M30, D11)).
//
// What it refuses: an empty batch (a call that can return nothing is a
// caller bug or a batching bug, and the wire would answer 400 with less
// context than this sentence), a provider whose adapter does not speak
// embeddings (named, with what IS registered), and a response whose vector
// count does not match the texts sent — the reorder/corruption guard above.
func (c *Client) Embed(ctx context.Context, route Route, texts []string) ([][]float32, Usage, Cost, error) {
	if len(texts) == 0 {
		return nil, Usage{}, Cost{State: CostUnknown, Reason: ReasonNotAttempted},
			fmt.Errorf("model: refused an embedding call with no texts (route %q) — an empty batch is a batching bug upstream of the wire, and the provider's 400 would say less", route.ID)
	}
	adapter, err := AdapterFor(route.Provider)
	if err != nil {
		return nil, Usage{}, Cost{State: CostUnknown, Reason: ReasonNotAttempted}, err
	}
	emb, ok := adapter.(Embedder)
	if !ok {
		return nil, Usage{}, Cost{State: CostUnknown, Reason: ReasonNotAttempted},
			fmt.Errorf("model: refused an embedding call through provider %q (route %q) — that provider does not speak an embeddings endpoint; embedding routes run on the provider that does (see the registry's providers), and configuring a model as what it cannot do is a route fix, not a code path",
				route.Provider, route.ID)
	}
	req, err := emb.EmbedToWire(route, texts)
	if err != nil {
		return nil, Usage{}, Cost{State: CostUnknown, Reason: ReasonNotAttempted}, err
	}
	req = req.WithContext(ctx)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, Usage{}, Cost{State: CostUnknown, Reason: ReasonStreamAbortedEarly},
			fmt.Errorf("model: embedding call to %s did not answer (route %q): %w", req.URL.String(), route.ID, err)
	}
	defer resp.Body.Close()
	// The body is bounded before it is read: an embedding response is
	// numbers, and numbers that grow past 64 MiB are a malfunction worth
	// failing on rather than an allocation worth dying on.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, Usage{}, Cost{State: CostUnknown, Reason: ReasonStreamAbortedEarly},
			fmt.Errorf("model: reading the embedding response (route %q): %w", route.ID, err)
	}
	vectors, usage, err := emb.EmbedFromWire(resp.StatusCode, raw)
	if err != nil {
		return nil, usage, Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage}, withCall(err, req, route)
	}
	if len(vectors) != len(texts) {
		return nil, usage, Cost{State: CostUnknown, Reason: ReasonProviderOmittedUsage},
			fmt.Errorf("model: embedding route %q returned %d vectors for %d texts — pairing vectors to chunks by position is the whole index, so a count mismatch is refused, not truncated",
				route.ID, len(vectors), len(texts))
	}
	// Cost, at the one site. Embeddings bill INPUT tokens; a route that also
	// prices output and gets no output usage back lands in `partial` honestly
	// (a floor with Missing naming the gap), never in a fake `known`.
	cost := ComputeCost(usage, route.Prices)
	return vectors, usage, cost, nil
}

// EmbedRefuseUnknownModel is not a thing this method does: a wrong model
// name is the provider's 400 to report, and EmbedFromWire carries the
// provider's sentence out — a local check of model names against a catalogue
// Aeon does not own would be a guess with an outage attached.

// embedEndpointSuffix is appended to the base URL, which keeps its /v1
// (ollama serves /v1/embeddings; OpenAI serves /v1/embeddings — both take
// the base as configured, nothing is inferred).
const embedEndpointSuffix = "/embeddings"

// joinEmbedBase trims exactly one trailing slash, matching ToWire's house
// style for base URLs — a double slash is a 404 that reads like a routing
// bug in the provider.
func joinEmbedBase(base string) string {
	return strings.TrimSuffix(base, "/") + embedEndpointSuffix
}
