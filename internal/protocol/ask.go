package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cjlapao/aeon/internal/model"
)

// # `ask` — the first method that does something a person would pay for
//
// Two modes, and the difference is deliberate rather than incidental:
//
//   - **with a `subscriptionId`**: returns `{askId}` immediately, then streams
//     `ask.delta` events through S2's registry, ending with one `ask.done`
//     carrying usage and cost. One streaming path for the whole product (M23) —
//     a bespoke channel for model output would need its own filtering, cursor
//     and teardown, and would then have to be reconciled with subscriptions at
//     S9 when the app wants both.
//   - **without one**: buffers the whole reply and returns it. That path exists
//     for a script and for testing, and it is **explicitly a different mode**
//     rather than an accident, because it is the one that would hide a
//     streaming bug (O5, D1).

// Event kinds this method emits. Additive: a new event kind does not bump
// PROTOCOL_VERSION (see version.go's rule).
const (
	KindAskDelta = "ask.delta"
	KindAskDone  = "ask.done"
)

// AskBufferBound is how much a buffered (non-streaming) reply may hold.
//
// **A guess.** 256 KiB is roughly 60k tokens of English, far more than a
// buffered call should be producing — a reply that large wants streaming. The
// bound exists because an unbounded buffer is a memory hole with a
// prompt-shaped cause: one request asking for a novel, and the daemon is
// holding it all.
//
// Revisit when something real uses the buffered mode. Nothing does yet; S5's
// runtime streams.
const AskBufferBound = 256 << 10

type AskParams struct {
	// Route is the route id, required.
	Route string `json:"route"`
	// Prompt is the user's text, required. S5 replaces this with a full
	// transcript; at S3 one prompt is the whole conversation.
	Prompt string `json:"prompt"`
	// SubscriptionID switches on streaming mode. Optional (O5).
	SubscriptionID string `json:"subscriptionId,omitempty"`

	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	// System is an optional system prompt, so the demo can show the adapters'
	// different handling of it without a transcript.
	System string `json:"system,omitempty"`

	// ReasoningEffort overrides the route's for this call — the per-chat half
	// of the dial.
	//
	// A route's value is the default for both halves of the product, and the
	// halves want different things: a chat wants the model to think, a step
	// cannot have it thinking at all on the models that refuse tools while it
	// is on. So the call decides, and neither half has to keep its own route.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// SaveStep persists what this call produced as a step with a transcript
	// (S4). It is the bridge between S3 and the store, and the reason a demo
	// can restart the daemon and still find something.
	//
	// It needs a scope: a step belongs to a loop, and a loop belongs to a
	// project. With SaveStep set and either missing, the call is refused rather
	// than filed somewhere arbitrary.
	SaveStep      bool   `json:"saveStep,omitempty"`
	LoopSlug      string `json:"loopSlug,omitempty"`
	ComponentRoot string `json:"componentRoot,omitempty"`
}

// AskSaveParams is what the store is handed when SaveStep is set.
//
// # What it CANNOT carry, and why that matters
//
// There is no thinking SIGNATURE here, because S3's streaming surface does not
// produce one: `Delta` carries `Kind` and `Text` and nothing else, so a
// reasoning delta arrives as text. A transcript saved this way therefore holds
// an UNSIGNED thinking block — which S3's validator permits deliberately (not
// every provider signs) and which S7 cannot replay to Anthropic.
//
// That is a real limitation of this bridge, not of the store: the store's own
// tests write and read a signed block byte-identically. It is recorded here
// because the alternative is somebody discovering it at S7.
type AskSaveParams struct {
	LoopSlug      string
	ComponentRoot string
	Route         string
	// Provenance is the triple of the route the call actually went out on,
	// stamped by the handler that made it (M26). The assistant row it produces
	// is refused at the store gate without it.
	Provenance model.Provenance
	System     string
	Prompt     string
	Text       string
	Thinking   string
	Usage      model.Usage
	Cost       model.Cost
	StopReason string
	Failed     bool
	// ThinkingDropped is what the wire withheld from this call (S18 T5,
	// M4/O1). The step row this save produces carries it, so the omission
	// reaches the record and not only a log line.
	ThinkingDropped int
}

type AskResult struct {
	AskID string `json:"askId"`

	// The three below are populated ONLY in buffered mode. In streaming mode
	// they arrive on `ask.done`, and leaving them empty here is what keeps a
	// client from reading a half-finished answer as the whole one.
	Text  string       `json:"text,omitempty"`
	Usage *model.Usage `json:"usage,omitempty"`
	Cost  *model.Cost  `json:"cost,omitempty"`

	// Truncated reports that the buffer bound was reached. **Reported, not
	// silent**: a reply that stops mid-sentence with no explanation reads as a
	// model failure.
	Truncated bool `json:"truncated,omitempty"`
	// StopReason and Dropped come from the provider, as on ask.done.
	StopReason string `json:"stopReason,omitempty"`
	Dropped    int    `json:"dropped,omitempty"`

	// StepID is the persisted step, when SaveStep was set. Zero means nothing
	// was saved, which is the default.
	StepID int64 `json:"stepId,omitempty"`
}

// AskDeltaPayload is one `ask.delta` event.
type AskDeltaPayload struct {
	AskID string `json:"askId"`
	// Kind distinguishes assistant text from reasoning, so a client can render
	// thinking differently rather than inline.
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	// Block carries a complete tool_use where the model called one. Nothing
	// executes it at S3 (N3); it is here so a client can show what was asked.
	Block *model.Block `json:"block,omitempty"`
}

// AskDonePayload is the single `ask.done` event.
type AskDonePayload struct {
	AskID          string      `json:"askId"`
	Usage          model.Usage `json:"usage"`
	Cost           model.Cost  `json:"cost"`
	StopReason     string      `json:"stopReason,omitempty"`
	Dropped        int         `json:"dropped,omitempty"`
	OwedToolErrors []string    `json:"owedToolErrors,omitempty"`
	// Error is set when the call failed. `ask.done` is emitted either way,
	// because **a cancelled or failed ask that reports nothing loses the
	// spend** (M15) — and those tokens were still bought.
	//
	// # Structured, not a string
	//
	// It was a string first, and walking the demo showed what that costs: in
	// streaming mode the failure arrives HERE rather than as a JSON-RPC error,
	// so a string meant the three-part shape was lost in exactly the mode a
	// person uses — `aeon ask` printed the provider's sentence and no `todo`.
	//
	// The same error object the synchronous path returns, so a client renders
	// one shape wherever the failure came from.
	Error *Error `json:"error,omitempty"`

	// StepID is the persisted step, when SaveStep was set (S4). It arrives HERE
	// as well as on the result because in streaming mode the result returns
	// before the reply exists — so the id of what was saved can only be known
	// at the end.
	StepID int64 `json:"stepId,omitempty"`
}

// Routes is where the table gets its routes.
//
// An interface rather than a map so S23's configuration can supply them later
// without this file changing, and so a test can provide one route without
// building a config.
type Routes interface {
	Route(id string) (model.Route, bool)
	RouteIDs() []string
}

// StaticRoutes is a Routes backed by a map. Used by tests and, until S23, by
// the daemon.
type StaticRoutes struct {
	mu sync.RWMutex
	by map[string]model.Route
}

func NewStaticRoutes(routes ...model.Route) *StaticRoutes {
	s := &StaticRoutes{by: map[string]model.Route{}}
	for _, r := range routes {
		s.by[r.ID] = r
	}
	return s
}

func (s *StaticRoutes) Route(id string) (model.Route, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.by[id]
	return r, ok
}

func (s *StaticRoutes) RouteIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.by))
	for id := range s.by {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// Streamer is the model client, as this package needs it.
//
// An interface so `ask_test.go` can drive the whole method without a provider
// or a transport, and so the daemon injects the real one.
type Streamer interface {
	Stream(ctx context.Context, req model.ModelRequest) (<-chan model.Delta, <-chan model.StreamResult, <-chan error)
}

var askCounter atomic.Uint64

// newAskID is short, sortable and unique within a daemon's life. Not a UUID:
// it appears in every delta and a person reads it in `aeon watch`.
func newAskID() string { return fmt.Sprintf("a-%d", askCounter.Add(1)) }

// handleAsk runs one `ask`.
func (t *Table) handleAsk(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p AskParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("`prompt` is required")}
	}
	if t.Routes == nil || t.Models == nil {
		return nil, ErrProviderFailed.WithWhat("this daemon has no model layer wired; `ask` " +
			"cannot work until it does")
	}

	route, ok := t.Routes.Route(p.Route)
	if !ok {
		ids := t.Routes.RouteIDs()
		if len(ids) == 0 {
			return nil, ErrUnknownRoute.WithWhat("route %q is not configured, and no routes are "+
				"configured at all", p.Route)
		}
		return nil, ErrUnknownRoute.WithWhat("route %q is not configured; the configured routes "+
			"are %s", p.Route, strings.Join(ids, ", "))
	}

	// S18 T3 (O3, D6): a cached contradiction refuses BEFORE the request is
	// paid for — the use-time half of refusing twice, so whoever probed on
	// Monday does not have to remember on Friday. The refusal sentence is
	// model.ContradictionSentence's, rendered verbatim in `what`.
	if err := t.contradictionRefusal(route); err != nil {
		return nil, err
	}

	// The subscription is validated BEFORE the call. Discovering it afterwards
	// would mean the call was paid for and the result had nowhere to go.
	var sub *Subscription
	if p.SubscriptionID != "" {
		sub = t.Subs.Find(sess.ConnID, p.SubscriptionID)
		if sub == nil {
			return nil, ErrUnknownSubscription.WithWhat(
				"subscription %q does not exist on connection %s", p.SubscriptionID, sess.ConnID)
		}
	}

	if !model.ValidReasoningEffort(strings.TrimSpace(p.ReasoningEffort)) {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"reasoningEffort %q is not one of %s", p.ReasoningEffort,
			strings.Join(model.ReasoningEfforts(), ", "))}
	}

	req := model.ModelRequest{
		Route:           route,
		MaxOutputTokens: p.MaxOutputTokens,
		Temperature:     p.Temperature,
		// Ask offers NO tools, so nothing lowers this: a chat is the half of
		// the product that can reason freely.
		ReasoningEffort: strings.TrimSpace(p.ReasoningEffort),
	}
	if p.System != "" {
		req.Messages = append(req.Messages, model.Message{
			Role: model.RoleSystem, Content: []model.Block{model.TextBlock(p.System)},
		})
	}
	req.Messages = append(req.Messages, model.Message{
		Role: model.RoleUser, Content: []model.Block{model.TextBlock(p.Prompt)},
	})

	// A step belongs to a loop and a loop belongs to a component, so a save with
	// half a scope is refused rather than filed somewhere arbitrary (S4).
	if p.SaveStep {
		if t.Memory == nil {
			return nil, ErrStoreUnavailable
		}
		if p.LoopSlug == "" || p.ComponentRoot == "" {
			return nil, ErrInvalidScope.WithWhat("saveStep needs both a loopSlug and a componentRoot, " +
				"because a step belongs to a loop and a loop belongs to a component")
		}
	}

	askID := newAskID()

	if sub == nil {
		return t.askBuffered(ctx, askID, p, req)
	}
	return t.askStreaming(ctx, askID, p, req, sub)
}

// saveAsk persists what an ask produced, when the caller asked for it.
//
// Failures to SAVE are reported in the log rather than turned into a failed
// ask: the tokens were already bought, and losing the reply because the store
// could not hold it would be the worse of the two outcomes. The step id comes
// back as zero, which is what "nothing was saved" looks like everywhere else.
func (t *Table) saveAsk(p AskParams, req model.ModelRequest, text, thinking string,
	result model.StreamResult, failed bool) int64 {
	if !p.SaveStep || t.Memory == nil {
		return 0
	}
	id, err := t.Memory.SaveAsk(AskSaveParams{
		LoopSlug:        p.LoopSlug,
		ComponentRoot:   p.ComponentRoot,
		Route:           req.Route.ID,
		Provenance:      model.NewProvenance(req.Route),
		System:          p.System,
		Prompt:          p.Prompt,
		Text:            text,
		Thinking:        thinking,
		Usage:           result.Usage,
		Cost:            result.Cost,
		StopReason:      result.StopReason,
		Failed:          failed,
		ThinkingDropped: result.ThinkingDropped,
	})
	if err != nil {
		return 0
	}
	return id
}

// askBuffered runs the call synchronously and returns the whole reply.
func (t *Table) askBuffered(ctx context.Context, askID string, p AskParams, req model.ModelRequest) (any, error) {
	deltas, results, errs := t.Models.Stream(ctx, req)

	var text strings.Builder
	var thinking strings.Builder
	truncated := false
	for d := range deltas {
		if d.Kind == model.DeltaThinking {
			// Captured for the transcript (S4's saveStep). It becomes an
			// UNSIGNED thinking block, because this surface carries no
			// signature — see AskSaveParams.
			thinking.WriteString(d.Text)
			continue
		}
		if d.Kind != model.DeltaText {
			continue
		}
		if text.Len()+len(d.Text) > AskBufferBound {
			// Truncate and keep draining: abandoning the stream here would
			// leave the usage unread, and the call would report an unknown cost
			// for a call that completed.
			truncated = true
			continue
		}
		text.WriteString(d.Text)
	}

	var result model.StreamResult
	for r := range results {
		result = r
	}
	var streamErr error
	for e := range errs {
		if streamErr == nil {
			streamErr = e
		}
	}

	if streamErr != nil {
		// The spend is persisted even on a failure, when a save was asked for:
		// a failed ask that reports nothing LOSES THE SPEND (M15), and those
		// tokens were still bought.
		t.saveAsk(p, req, text.String(), thinking.String(), result, true)
		// The provider's own words travel in `what`, because they are usually
		// the actionable part — a wrong model name says so precisely.
		return nil, ErrProviderFailed.WithWhat("%v", streamErr)
	}

	out := AskResult{
		AskID:      askID,
		Text:       text.String(),
		Usage:      &result.Usage,
		Cost:       &result.Cost,
		StopReason: result.StopReason,
		Dropped:    result.Dropped,
		Truncated:  truncated,
		StepID:     t.saveAsk(p, req, text.String(), thinking.String(), result, false),
	}
	return out, nil
}

// askStreaming returns immediately and emits events as the reply arrives.
func (t *Table) askStreaming(ctx context.Context, askID string, p AskParams, req model.ModelRequest, sub *Subscription) (any, error) {
	// The connection's context, not the request's: the stream outlives this
	// handler's return, and must end when the client disconnects rather than
	// when the handler finishes.
	streamCtx := ctx

	go func() {
		deltas, results, errs := t.Models.Stream(streamCtx, req)

		var text strings.Builder
		var thinking strings.Builder

		for d := range deltas {
			payload := AskDeltaPayload{AskID: askID, Kind: string(d.Kind)}
			switch d.Kind {
			case model.DeltaText, model.DeltaThinking:
				payload.Text = d.Text
				// Accumulated for the transcript when a save was asked for
				// (S4). The deltas are still forwarded unchanged — the store
				// is a second reader of the stream, not a stage in it.
				if d.Kind == model.DeltaText {
					text.WriteString(d.Text)
				} else {
					thinking.WriteString(d.Text)
				}
			case model.DeltaToolUse:
				payload.Block = d.Block
			case model.DeltaStop:
				// The stop reason arrives on ask.done, where the accounting is.
				continue
			}
			t.publish(KindAskDelta, payload)
		}

		var result model.StreamResult
		for r := range results {
			result = r
		}
		var streamErr error
		for e := range errs {
			if streamErr == nil {
				streamErr = e
			}
		}

		done := AskDonePayload{
			AskID:          askID,
			Usage:          result.Usage,
			Cost:           result.Cost,
			StopReason:     result.StopReason,
			Dropped:        result.Dropped,
			OwedToolErrors: result.OwedToolErrors,
		}
		if streamErr != nil {
			// Mapped through the same error the synchronous path returns, so
			// `what` carries the provider's own words and `todo` says what to
			// do about them.
			done.Error = ErrProviderFailed.WithWhat("%v", streamErr)
		}
		// Saved before `ask.done` is published, so the id on the event is the
		// id of something that exists. A client that received the event first
		// could ask for a step the store had not finished writing.
		done.StepID = t.saveAsk(p, req, text.String(), thinking.String(), result, streamErr != nil)

		// **Emitted whatever happened** (M15). A cancelled or failed ask that
		// reports nothing loses the spend, and those tokens were still bought.
		t.publish(KindAskDone, done)
	}()

	return AskResult{AskID: askID}, nil
}

// publish hands an event to the daemon, which owns the wire.
func (t *Table) publish(kind string, payload any) {
	if t.OnEvent == nil {
		return
	}
	t.OnEvent(kind, payload)
}
