package model

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// # The probe (S18 T3)
//
// A declaration is a claim; a probe is the one cheap request that checks it.
// The rules this file encodes, and why each is what it is:
//
//   - **One minimal streaming request per route** (D7). Not per capability,
//     and never per step: a probe paid per step is a tax on every call for a
//     fact that does not change hour to hour. The single request asks for the
//     union of what the route DECLARED — a tool offered if `tools` or
//     `parallel-tools` is declared, usage in the stream (the OpenAI adapter
//     already asks the adapter's streaming-usage parameter), a thinking block if `thinking` is
//     declared — and the one answer is read five ways.
//   - **It never rewrites the declaration** (M28, I5). The probe records
//     alongside; `Route.WireCapabilities` is not a field this function is
//     allowed to touch, and a test asserts it stays byte-identical.
//   - **A probe failure is not a contradiction.** "Could not check" and
//     "checked and wrong" are different facts; recording a network failure as
//     `contradicted` would refuse every later step for a reason that was
//     never true. Only DEFINITIVE evidence — the provider's own 400 naming
//     the feature, or the measured arrival/absence of an asked-for stream
//     part — moves a capability out of `declared`.
//   - **A capability that cannot be cheaply probed stays `declared` and says
//     so** (Q9). `system-role` and `parallel-tools` get `declared` with a
//     reason rather than an invented `verified` badge nobody should trust.
//
// # Q9, answered per capability (what each probe is, and what it costs)
//
// | flag | evidence that VERIFIES | evidence that CONTRADICTS | otherwise |
// | --- | --- | --- | --- |
// | `tools` | the request carried a tool and the stream completed — the server ACCEPTED tool declarations | a 400 naming tools/functions | — |
// | `streaming-usage` | a usage object arrived in the stream (asked for with the adapter's standard include-usage streaming parameter) | the stream completed with no usage at all — the asked-for behaviour, measured absent | — |
// | `thinking` | a thinking block came back | a 400 naming the thinking/reasoning parameter | stays `declared`: a model that answered without thinking proved nothing |
// | `system-role` | (none cheap) | a 400 naming the system role | stays `declared`: a silently-ignored system message is indistinguishable from a honoured one in one request |
// | `parallel-tools` | two or more tool calls in the one turn | a 400 naming parallel tool calls | stays `declared`: one tool call is what a single request can show at most, never a refusal |
//
// Total cost when anything is declared: **one request**. Zero requests when
// the route declared nothing — the probe has nothing to ask.

// ProbeResult is one capability's verdict, with the two facts that let a
// person judge it (O2): WHEN it was probed and against WHICH model. A cached
// `verified` from three weeks ago against a model that has since been
// replaced is a stale claim wearing a verified badge — the timestamp and the
// model are what let someone notice.
type ProbeResult struct {
	Capability Capability      `json:"capability"`
	State      CapabilityState `json:"state"`
	// Detail is what the probe saw, in the probe's own words — the 400's
	// message, or "usage arrived in the final chunk". It renders verbatim;
	// a surface does not get to re-interpret it.
	Detail string `json:"detail,omitempty"`
	// ProbedAt and Model are REQUIRED: a result without them cannot be
	// judged staleness or substitution, which is the whole reason to keep it.
	ProbedAt time.Time `json:"probedAt"`
	Model    string    `json:"probedModel"`
}

// A definitive result is one that earns a cache row. `declared` is
// represented by the ABSENCE of a row (I5: the probe never writes the
// declaration), so a "declared, and here is why" verdict is returned to the
// caller for display but is not stored.
func (p ProbeResult) definitive() bool {
	return p.State == StateVerified || p.State == StateContradicted
}

// ProbeTransport is what a probe needs from the model layer: exactly the
// Stream method `*model.Client` (and the protocol's Streamer) already has.
// Tests hand in a fake — no test in this slice reaches a live provider (I7).
type ProbeTransport interface {
	Stream(ctx context.Context, req ModelRequest) (<-chan Delta, <-chan StreamResult, <-chan error)
}

// ProbeTimeout is the bound on one probe request. A hung server must not
// hang the operator who typed `aeon routes probe`; a timeout is a failure
// ("could not check"), which leaves everything `declared`.
const ProbeTimeout = 30 * time.Second

// Probe runs the ONE request for a route and returns a verdict per declared
// capability. A nil error with results means the evidence came in; an error
// means the probe itself failed — and NOTHING has been contradicted, which
// is the caller's cue to leave every capability as it was and say why.
func Probe(ctx context.Context, r Route, transport ProbeTransport, now func() time.Time) ([]ProbeResult, error) {
	if transport == nil {
		return nil, fmt.Errorf("probe: no transport to ask route %q with", r.ID)
	}
	if len(r.WireCapabilities) == 0 {
		return nil, nil // nothing declared: nothing to ask, zero requests
	}
	if now == nil {
		now = time.Now
	}

	probed := map[Capability]bool{}
	for _, c := range r.WireCapabilities {
		probed[Capability(c)] = true
	}

	// The minimal request: as short as a prompt can be, asking exactly for
	// what the route claims. MaxOutputTokens stays small — this is a probe,
	// not a conversation.
	req := ModelRequest{
		Route: r,
		Messages: []Message{
			{Role: RoleSystem, Content: []Block{{Kind: BlockText, Text: "You are a capability probe. Answer exactly as asked."}}},
			{Role: RoleUser, Content: []Block{{Kind: BlockText, Text: "Reply with exactly: OK"}}},
		},
		MaxOutputTokens: 64,
	}
	if probed[CapTools] || probed[CapParallelTools] {
		req.Tools = []ToolSpec{{
			Name:        "probe_say_ok",
			Description: "Say OK through the tool. You may answer in text instead; either is correct.",
			Parameters:  []byte(`{"type":"object","properties":{},"additionalProperties":false}`),
		}}
	}
	if probed[CapThinking] {
		req.ThinkingBudgetTokens = 1024
	}

	deltas, results, errs := transport.Stream(ctx, req)
	toolCalls, sawThink, sawUsage, streamErr := drainProbe(deltas, results, errs)

	// A 400 that NAMES a feature is evidence — the provider's own refusal.
	// Any other failure ("could not check") aborts the probe with nothing
	// contradicted: a network failure is not a fact about the server's
	// capabilities, and recording it as `contradicted` would refuse every
	// later step for a reason that was never true.
	se := statusErrorOf(streamErr)
	refusal := se != nil && se.Status == 400
	if streamErr != nil && !refusal {
		return nil, fmt.Errorf("probe of route %q could not complete: %w", r.ID, streamErr)
	}
	if refusal && !(refusalNames(se, "tool", "function") && (probed[CapTools] || probed[CapParallelTools]) ||
		refusalNames(se, "thinking", "reasoning") && probed[CapThinking] ||
		refusalNames(se, "system role", "system message", "\"system\"") && probed[CapSystemRole] ||
		refusalNames(se, "parallel") && probed[CapParallelTools]) {
		// The 400 is about something else — the prompt, the temperature, a
		// typo in the model name. That is a failed probe, not a verdict.
		return nil, fmt.Errorf("probe of route %q could not complete (the server answered 400 about something other than the declared capabilities): %w", r.ID, streamErr)
	}

	out := make([]ProbeResult, 0, len(probed))
	add := func(c Capability, state CapabilityState, detail string) {
		out = append(out, ProbeResult{Capability: c, State: state, Detail: detail, ProbedAt: now(), Model: r.Model})
	}
	// When the request died at a 400, no stream completed: nothing was
	// measured, so the capabilities the refusal did not name get `declared`
	// with the reason — never a contradiction by association.
	neverRan := "the request was refused before the stream began, so this was never observable"

	for _, c := range WireCapabilityFlags() {
		if !probed[Capability(c)] {
			continue
		}
		switch Capability(c) {
		case CapTools:
			switch {
			case refusal && refusalNames(se, "tool", "function"):
				add(Capability(c), StateContradicted, "the server refused the tool declaration: "+se.Body)
			case refusal:
				add(Capability(c), StateDeclared, neverRan)
			default:
				add(Capability(c), StateVerified, "the request carried a tool declaration and the stream completed — the server accepted tools")
			}
		case CapStreamingUsage:
			switch {
			case refusal:
				add(Capability(c), StateDeclared, neverRan)
			case sawUsage:
				add(Capability(c), StateVerified, "a usage object arrived in the stream, as asked for")
			default:
				add(Capability(c), StateContradicted, "the stream completed with no usage at all, although the adapter asked for usage in the stream by the provider's standard means")
			}
		case CapThinking:
			switch {
			case refusal && refusalNames(se, "thinking", "reasoning"):
				add(Capability(c), StateContradicted, "the server refused the thinking request: "+se.Body)
			case sawThink:
				add(Capability(c), StateVerified, "a thinking block came back")
			case refusal:
				add(Capability(c), StateDeclared, neverRan)
			default:
				add(Capability(c), StateDeclared, "the model answered without producing a thinking block, which proves nothing either way — left declared")
			}
		case CapSystemRole:
			if refusal && refusalNames(se, "system role", "system message", "\"system\"") {
				add(Capability(c), StateContradicted, "the server refused the system position: "+se.Body)
			} else {
				add(Capability(c), StateDeclared, "not cheaply probable: a silently-ignored system message looks exactly like a honoured one in one request")
			}
		case CapParallelTools:
			switch {
			case refusal && refusalNames(se, "parallel"):
				add(Capability(c), StateContradicted, "the server refused parallel tool calls: "+se.Body)
			case toolCalls >= 2:
				add(Capability(c), StateVerified, fmt.Sprintf("one turn emitted %d tool calls", toolCalls))
			case refusal:
				add(Capability(c), StateDeclared, neverRan)
			default:
				add(Capability(c), StateDeclared, "one request can show at most what came back, not what the server refuses — left declared")
			}
		}
	}
	return out, nil
}

// drainProbe consumes the three channels of one Stream exactly once and
// reports what the single request revealed. The producer closes deltas and
// results before errs, so reading them in sequence is the documented order.
func drainProbe(deltas <-chan Delta, results <-chan StreamResult, errs <-chan error) (toolCalls int, sawThink, sawUsage bool, streamErr error) {
	for d := range deltas {
		switch d.Kind {
		case DeltaToolUse:
			toolCalls++
		case DeltaThinking:
			sawThink = true
		}
	}
	for res := range results {
		if res.Usage.Any() {
			sawUsage = true
		}
	}
	for e := range errs {
		if streamErr == nil {
			streamErr = e
		}
	}
	return
}

// statusErrorOf digs a provider 400 out of a stream error, for the
// definitive-refusal evidence the verdict table above names.
func statusErrorOf(err error) *StatusError {
	for err != nil {
		if se, ok := err.(*StatusError); ok {
			return se
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = u.Unwrap()
	}
	return nil
}

// refusalNames reports whether a provider 400 names one of the given words —
// the only text the verdict table treats as definitive. Matching the
// provider's own words (lower-cased, whole-phrase) rather than any text: a
// body that happens to contain "tools" is not a refusal of tools.
func refusalNames(se *StatusError, words ...string) bool {
	if se == nil || se.Status != 400 {
		return false
	}
	body := strings.ToLower(se.Body)
	for _, w := range words {
		if strings.Contains(body, w) {
			return true
		}
	}
	return false
}

// FirstContradiction reports the first cached verdict that contradicts a
// capability the route DECLARES — the use-time refusal's evidence (O3, D6).
//
// The declaration leads: a `contradicted` row for a capability the route no
// longer declares contradicts nothing (an old probe of a since-undeclared
// capability must not refuse a step for a claim the operator has withdrawn).
// And only declared capabilities are checked, because the row records the
// probe's fact about the CLAIM — no claim, no contradiction.
func (r Route) FirstContradiction(rows []ProbeResult) (ProbeResult, bool) {
	if len(r.WireCapabilities) == 0 {
		return ProbeResult{}, false
	}
	declared := make(map[string]bool, len(r.WireCapabilities))
	for _, c := range r.WireCapabilities {
		declared[c] = true
	}
	for _, row := range rows {
		if row.State == StateContradicted && declared[string(row.Capability)] {
			return row, true
		}
	}
	return ProbeResult{}, false
}

// ContradictionSentence renders the use-time refusal (O3/D6) verbatim for
// every surface that enforces it — the one sentence, not a per-surface
// paraphrase:
//
//	refused: route 'local-wrong' declares 'tools' but the probe contradicted it
//	         (probed 2026-09-06T14:02Z against llama3.2:1b)
func ContradictionSentence(routeID, capability string, at time.Time, againstModel, detail string) string {
	s := fmt.Sprintf("refused: route '%s' declares '%s' but the probe contradicted it (probed %s against %s",
		routeID, capability, at.UTC().Format(time.RFC3339), againstModel)
	if strings.TrimSpace(detail) != "" {
		s += ": " + detail
	}
	return s + ")"
}
