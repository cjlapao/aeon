package model

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// # The client: route → adapter, retry, cancel, cost ONCE
//
// This is the only place a request is sent, and **the only place a cost is
// constructed** (O1, I4). An adapter computing its own cost would mean a third
// adapter at S18 could get it wrong in a way nothing here would catch.

// StreamResult is one call's accounting, sent exactly once.
type StreamResult struct {
	Usage      Usage  `json:"usage"`
	Cost       Cost   `json:"cost"`
	StopReason string `json:"stopReason,omitempty"`
	// Dropped counts provider content that mapped to no canonical kind (M4).
	// A non-zero value is not an error — it is a number somebody can look at
	// when a reply seems to be missing something.
	Dropped int `json:"dropped,omitempty"`
	// ThinkingDropped counts REQUEST-direction thinking blocks withheld from
	// the wire because their producer is not this provider (S18 T5, M4/M27).
	// Counted, never silent: a transcript that keeps every block (M1) while
	// the wire omits some needs its omission visible where the step's record
	// is read, which is where "why did this turn behave differently" gets
	// asked (O1).
	ThinkingDropped int `json:"thinkingDropped,omitempty"`
	// OwedToolErrors names tool calls whose arguments would not parse (D6).
	//
	// **This layer says so rather than inventing a tool outcome.** S5's runtime
	// writes a `tool_result` with IsError into the transcript, because only it
	// knows what a tool result looks like in a transcript.
	OwedToolErrors []string `json:"owedToolErrors,omitempty"`
	// Attempts is how many requests were sent, including the successful one.
	Attempts int `json:"attempts"`
}

// StatusError is a provider's non-2xx answer, quoted verbatim. It exists as
// a TYPE because a caller sometimes needs to tell "the server refused THIS
// feature" from "the request went wrong somewhere" — the probe (S18 T3)
// treats a 400 naming tools as evidence and every other failure as no
// evidence at all, and string-matching a formatted message for that would
// couple the verdict to punctuation.
type StatusError struct {
	Provider string
	Status   int
	// Body is the provider's own words, trimmed, capped at read time.
	Body string
	// Hint is Aeon's added sentence (the reasoning-effort hint), not the
	// provider's — kept separate so the probe reads Body, never Hint.
	Hint string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("model: %s answered %d: %s%s", e.Provider, e.Status, e.Body, e.Hint)
}

// Client sends model requests.
//
// `HTTP` is injectable, which is what makes the whole suite pass with no
// network and no credentials in the environment (D15, I7).
type Client struct {
	HTTP *http.Client

	// MaxAttempts bounds retries. Zero means the default.
	MaxAttempts int
	// BaseBackoff is the first delay; each attempt doubles it, with jitter.
	BaseBackoff time.Duration
	// Sleep is indirected so a retry test does not spend real seconds waiting.
	// It must honour the context, or a cancelled call sleeps out its backoff.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is indirected for the HTTP-date form of Retry-After.
	Now func() time.Time
}

const (
	defaultMaxAttempts = 4
	defaultBaseBackoff = 500 * time.Millisecond

	// maxBackoff bounds OUR OWN exponential guess. Beyond half a minute an
	// exponential delay is no longer an estimate of anything — the provider has
	// not told us to wait, so we are guessing, and a long guess is just a hang.
	maxBackoff = 30 * time.Second

	// maxRetryAfter bounds what a PROVIDER asks for, and is deliberately
	// larger.
	//
	// # Why these are two constants
	//
	// They were one, at 30s, and a test caught it: a `Retry-After` of 90
	// seconds is an ordinary rate-limit instruction, and clamping it to 30 meant
	// retrying into the same limit twice more before giving up — the opposite of
	// what honouring the header is for.
	//
	// The provider's instruction is information; our exponent is a guess. A
	// long wait is safe here because `sleep` honours the context, so cancelling
	// a loop is still immediate — which is what made the original clamp
	// unnecessary. What remains is a guard against an absurd value, not against
	// a long one.
	maxRetryAfter = 2 * time.Minute
)

func (c *Client) maxAttempts() int {
	if c.MaxAttempts > 0 {
		return c.MaxAttempts
	}
	return defaultMaxAttempts
}

func (c *Client) baseBackoff() time.Duration {
	if c.BaseBackoff > 0 {
		return c.BaseBackoff
	}
	return defaultBaseBackoff
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// sleep waits, or returns early when the context ends.
//
// **A cancelled context during a backoff delay ends immediately** rather than
// sleeping it out — otherwise cancelling a loop would take as long as the
// provider asked us to wait, which on a 429 can be a minute.
func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stream sends a request and streams the reply.
//
// # Three channels, and why
//
//   - **deltas** for display, closed when the stream ends
//   - **one result** for accounting, sent exactly once — on success, on
//     failure, and on cancellation (M15). A caller that sees no result has a
//     bug to find; a caller that sees two cannot add up a loop's cost
//   - **errors** distinguishable from a clean end, because a truncated stream
//     reported as a completed call is the failure the whole error path exists
//     to prevent
//
// Every channel is closed before Stream's goroutine returns, so a caller can
// range over all three.
func (c *Client) Stream(ctx context.Context, req ModelRequest) (<-chan Delta, <-chan StreamResult, <-chan error) {
	deltas := make(chan Delta)
	results := make(chan StreamResult, 1)
	errs := make(chan error, 1)

	go func() {
		defer close(deltas)
		defer close(results)
		defer close(errs)
		c.stream(ctx, req, deltas, results, errs)
	}()

	return deltas, results, errs
}

func (c *Client) stream(ctx context.Context, req ModelRequest, deltas chan<- Delta, results chan<- StreamResult, errs chan<- error) {
	adapter, err := AdapterFor(req.Route.Provider)
	if err != nil {
		errs <- err
		return
	}
	if err := req.Validate(); err != nil {
		errs <- err
		return
	}

	// Tool names go out in the form the provider accepts and come back canonical.
	names := newWireNames(req)
	if names.changes() {
		req = names.encode(req)
	}

	// deltaYielded is the retry boundary. **Once a delta has reached the
	// caller, a retry would duplicate it** — the caller has already rendered
	// text, and a second attempt would render it again. So after the first
	// delta the call fails rather than retrying, whatever the status.
	deltaYielded := false
	attempts := 0

	finish := func(streamErr error) {
		usage, stopReason, dropped := adapter.Finalise()

		// **Cost is computed HERE and nowhere else** (O1, I4).
		cost := ComputeCost(usage, req.Route.Prices)

		// The one reason `cost.go` deliberately does not produce (T4): only the
		// caller of the stream knows the stream was cut.
		if streamErr != nil && usage.Empty() {
			cost = Cost{State: CostUnknown, Reason: ReasonStreamAbortedEarly}
		}

		result := StreamResult{
			Usage:      usage,
			Cost:       cost,
			StopReason: stopReason,
			Dropped:    dropped,
			Attempts:   attempts,
		}
		if owed, ok := adapter.(interface{ OwedToolErrors() []string }); ok {
			result.OwedToolErrors = owed.OwedToolErrors()
		}
		// The same optional-extra idiom as OwedToolErrors above: an adapter
		// that withholds thinking blocks reports their number here, and the
		// number rides the accounting to the step's record (S18 T5, O1). An
		// adapter without the method is not a failure — it dropped nothing.
		if td, ok := adapter.(interface{ ThinkingDropped() int }); ok {
			result.ThinkingDropped = td.ThinkingDropped()
		}
		results <- result

		if streamErr != nil {
			errs <- streamErr
		}
	}

	for {
		attempts++

		httpReq, err := adapter.ToWire(req)
		if err != nil {
			finish(err)
			return
		}
		httpReq = httpReq.WithContext(ctx)

		resp, err := c.httpClient().Do(httpReq)
		if err != nil {
			// A transport error is retryable — unless a delta already went out,
			// or the context ended, or we are out of attempts.
			if retryErr := c.waitToRetry(ctx, attempts, deltaYielded, nil, err); retryErr != nil {
				finish(retryErr)
				return
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			// The body is read and closed before deciding, so a retry does not
			// leak a connection — and so the message can quote what the
			// provider said.
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
			resp.Body.Close()

			body := strings.TrimSpace(string(raw))
			statusErr := &StatusError{
				Provider: req.Route.Provider,
				Status:   resp.StatusCode,
				Body:     body,
				Hint:     reasoningEffortHint(req, body),
			}

			if retryErr := c.waitToRetry(ctx, attempts, deltaYielded, resp, statusErr); retryErr != nil {
				finish(retryErr)
				return
			}
			continue
		}

		// A 200. From here the call is committed: anything that goes wrong is
		// reported, never retried, because the stream has begun.
		frames, frameErrs := ReadSSE(ctx, resp.Body)
		for delta := range adapter.FromWire(ctx, frames) {
			delta = names.decode(delta)
			select {
			case deltas <- delta:
				deltaYielded = true
			case <-ctx.Done():
				// M15: cancellation reports the usage it saw.
				finish(ctx.Err())
				return
			}
		}

		// The frame reader's error, if any. Drained after the deltas so a
		// truncation is reported rather than looking like a clean end.
		var streamErr error
		for err := range frameErrs {
			if streamErr == nil {
				streamErr = err
			}
		}
		if streamErr == nil && ctx.Err() != nil {
			streamErr = ctx.Err()
		}
		finish(streamErr)
		return
	}
}

// waitToRetry decides whether to retry, and waits if so.
//
// Returns nil to retry, or the error to report.
func (c *Client) waitToRetry(ctx context.Context, attempts int, deltaYielded bool, resp *http.Response, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// **Never retry once a delta has been yielded**, whatever the status. The
	// caller has rendered text; a second attempt would render it twice.
	if deltaYielded {
		return fmt.Errorf("%w (not retried: the reply had already started)", cause)
	}
	if !retryable(resp) {
		// Any other 4xx is a schema or content error. Retrying wastes time and
		// money and produces the same answer.
		return fmt.Errorf("%w (not retried: the provider will answer the same way)", cause)
	}
	if attempts >= c.maxAttempts() {
		return fmt.Errorf("%w (gave up after %d attempts)", cause, attempts)
	}

	delay := c.backoff(attempts, resp)
	if err := c.sleep(ctx, delay); err != nil {
		return err
	}
	return nil
}

// retryable reports whether a response is worth another attempt.
//
// A nil response means a transport error, which is retryable: the request may
// never have arrived.
func retryable(resp *http.Response) bool {
	if resp == nil {
		return true
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode >= 500 && resp.StatusCode <= 599
}

// backoff is the delay before the next attempt.
//
// `Retry-After` wins where the provider sent one — it knows its own rate limit
// better than an exponential guess does — and is honoured in BOTH forms the
// specification allows.
func (c *Client) backoff(attempt int, resp *http.Response) time.Duration {
	if d, ok := c.retryAfter(resp); ok {
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}

	// Exponential, with jitter. Jitter matters because without it every client
	// that hit the same rate limit retries in the same millisecond and hits it
	// again — a thundering herd of one process's own concurrent calls.
	delay := c.baseBackoff() * time.Duration(1<<(attempt-1))
	if delay > maxBackoff {
		delay = maxBackoff
	}
	jitter := time.Duration(rand.Int63n(int64(delay)/2 + 1))
	return delay/2 + jitter
}

// retryAfter parses the header in both of its forms.
//
// Seconds — `Retry-After: 30` — and an HTTP-date — `Retry-After: Wed, 21 Oct
// 2026 07:28:00 GMT`. A client that handles only the former waits zero against
// a provider that sends the latter, and then retries immediately into the same
// rate limit.
func (c *Client) retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(raw); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}

	if when, err := http.ParseTime(raw); err == nil {
		d := when.Sub(c.now())
		if d < 0 {
			// A date in the past means "retry now", not "wait negative".
			return 0, true
		}
		return d, true
	}
	return 0, false
}

// reasoningEffortHint turns one specific provider refusal into something a
// person can act on.
//
// # The refusal, measured
//
// OpenAI's current models reject function tools and reasoning together on
// `/v1/chat/completions`:
//
//	Function tools with reasoning_effort are not supported for gpt-6-luna in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// Every Aeon step offers tools, so on those models every step fails — and the
// provider's message, while accurate, names a parameter the person never set
// and an endpoint Aeon does not use. Without this they are left to discover
// `Route.ReasoningEffort` from the source.
//
// # Why a hint and not a retry
//
// A retry with the parameter changed would spend a second call on a guess about
// what somebody wanted. The route is configuration: the right move is to change
// it once, deliberately, and this says exactly how.
//
// # Why it is matched on the body rather than on the model's name
//
// A name list is folklore with an expiry date — the model that breaks it is
// always one nobody has seen yet. The provider, on the other hand, tells us
// precisely what it refused and why, in a message it is unlikely to stop
// sending while the restriction exists.
func reasoningEffortHint(req ModelRequest, body string) string {
	if len(req.Tools) == 0 || !strings.Contains(body, "reasoning_effort") {
		return ""
	}

	effective := req.EffectiveReasoningEffort()

	// Already on the API that accepts both: this restriction should not be
	// reachable there at all, so the remedy below (switching to it) is not a
	// remedy — something else refused the request.
	if req.Route.EffectiveOpenAIEndpoint() == OpenAIEndpointResponses {
		return fmt.Sprintf("\n\nThis route already talks to /v1/responses, which accepts function "+
			"tools and reasoning together — so this restriction is not the one Aeon expected here. "+
			"Model %q may have a different rule on this endpoint than the one this message assumes.",
			req.Route.Model)
	}

	// `none` was already sent on the chat endpoint and the provider still
	// refused the tools: repeating that advice is worse than saying it did
	// not work, and naming the real next step — the other endpoint.
	if effective == "none" {
		return fmt.Sprintf("\n\nThis route already sets reasoningEffort=none and the provider still "+
			"refused the tools, so lowering the effort further is not the remedy. The other OpenAI "+
			"API accepts tools and reasoning together:\n"+
			"    aeon routes set --id %s --provider %s --model %s --openai-endpoint responses\n"+
			"Try that before concluding this model needs something Aeon does not support.",
			req.Route.ID, req.Route.Provider, req.Route.Model)
	}

	current := "unset, so the model's own default applies"
	switch {
	case req.ReasoningEffort != "":
		// Named on the CALL, so the route is not where to fix it.
		return fmt.Sprintf("\n\nThis step offered %d tool(s) and asked for reasoningEffort %q "+
			"explicitly on the chat API, which refuses that combination. Pass `--reasoning-effort "+
			"none` on this call, or run it against a route whose --openai-endpoint is `responses`, "+
			"which accepts both.",
			len(req.Tools), req.ReasoningEffort)
	case req.Route.ReasoningEffort != "":
		current = fmt.Sprintf("%q", req.Route.ReasoningEffort)
	}
	// The WHOLE command, including --provider and --model: `routes set` requires
	// them, so a shorter line would be advice that fails when pasted. Found by
	// reading this message back against the verb's own validation.
	//
	// TWO remedies, because they trade off against each other: `none` is one
	// flag and gives up reasoning for this step; `responses` keeps reasoning
	// and is a second wire format this build has to speak correctly.
	return fmt.Sprintf("\n\nThis step offered %d tool(s), and this route's reasoningEffort is %s. "+
		"The chat API refuses function tools while reasoning is on. Either turn reasoning off for "+
		"this route:\n"+
		"    aeon routes set --id %s --provider %s --model %s --reasoning-effort none\n"+
		"or move it to the API that accepts both, to keep reasoning in a step:\n"+
		"    aeon routes set --id %s --provider %s --model %s --openai-endpoint responses\n"+
		"Every Aeon step offers tools, so without one of those this route can answer `aeon ask` but "+
		"no step.",
		len(req.Tools), current, req.Route.ID, req.Route.Provider, req.Route.Model,
		req.Route.ID, req.Route.Provider, req.Route.Model)
}
