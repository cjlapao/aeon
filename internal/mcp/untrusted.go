package mcp

// untrusted.go — MC7 in full, both halves in one file because they are one
// rule: **third-party text is labelled where it reaches a model.** The
// halves are descriptions (the new one, arriving BEFORE any call) and
// results (W23 as already written). Split them and payloads get labelled
// while prose does not — the exact failure this slice's design calls its
// largest cost.
//
// Labelling is not filtering. The content is byte-for-byte verbatim: a
// rewritten description is one the model acts on while nobody can see what
// the server actually said, and the rewriting rule itself becomes the thing
// to attack.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/label"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// labelRegion is the anti-spoofing mechanism — ONE definition, in
// internal/label, shared with S14's skill results (AS12). It was extracted
// here into that package at S14 T8 rather than duplicated, because the rule
// this file was written against is that a third labelling implementation is
// two too many: descriptions (MC7), tool results (W23) and skill results
// (AS12) are one threat and get one grammar. The boundary is a per-label
// random token the content cannot contain without having predicted it; the
// content is byte-for-byte verbatim. See internal/label/label.go for the
// full argument — it moved, it did not change.
func labelRegion(what, content string) string {
	return label.Region(what, content)
}

// LabelDescription is D7: a server's prose about its own tool, labelled at
// the one place it reaches a model — the assembled spec list. The label
// names the server, because "untrusted *from where*" is the question a
// reader (human or model) asks next.
func LabelDescription(server, description string) string {
	return labelRegion(fmt.Sprintf("tool description from MCP server %q", server), description)
}

// LabelResult is W23's requirement, spoken for MCP results: the text a
// server returns is attacker-influenced by default — that is what tool
// servers carry.
func LabelResult(server, tool, text string) string {
	return labelRegion(fmt.Sprintf("tool result from MCP server %q tool %q", server, tool), text)
}

// ToolSpecsFor is the PROMPT surface: what the provider is told exists for
// this agent. It is derived inside the registry from the one accessor
// (ToolsFor) and applies the labels there and then — a caller assembling
// specs by hand would be assembling them without the label, which is the
// failure mode MC7 was written against.
//
// Core descriptions are NOT labelled: they are reviewed Go source we wrote
// (that is what the compile-time design bought). Labelling our own prose
// third-party is not caution — it is noise that teaches the model to ignore
// the labels that matter.
func (r *Registry) ToolSpecsFor(agent string) []model.ToolSpec {
	list := r.ToolsFor(agent)
	out := make([]model.ToolSpec, 0, len(list))
	for _, tl := range list {
		desc := tl.Description
		params := tl.InputSchema
		if tl.Tier == TierCore {
			if reg, ok := tools.Registry()[tl.Name]; ok {
				desc = reg.Spec.Description
				params = reg.Spec.Parameters
			}
		} else {
			desc = LabelDescription(tl.Server, desc)
		}
		out = append(out, model.ToolSpec{Name: tl.Qualified, Description: desc, Parameters: params})
	}
	return out
}

// CallOutcome is what one registry call produces: model-facing content and
// the error flag (IsError is the tools.Result convention — a failure the
// model can read and recover from, not a panic).
//
// AskRequired is the use-time consent the loop must obtain from a human
// BEFORE this call may run (L22/MC5: outward and irreversible effects ask at
// use; S10's matched `ask` rule asks too). When it is true the server was
// NOT called — asking and then calling anyway is not consent, it is an
// apology. Nothing in this package may answer the ask: consent is a human
// act, and the answer arrives as policy (or as S11's gate machinery), never
// as a flag the client sets itself.
type CallOutcome struct {
	Content     string
	IsError     bool
	AskRequired bool

	// TimedOut marks the third outcome (T8): the server did not answer
	// within the bound. It is neither an empty result nor a generic error,
	// and a transcript that cannot tell the three apart will render a hang
	// as "the tool found nothing" — the failure MC9's subtler half names.
	TimedOut bool
}

// effectFor reads the EFFECT the agent is actually shown for this tool —
// the raised one. A server's destructive admission must be able to summon
// the consent screen; classes are consulted where they are rendered, never
// re-derived from a second copy nobody updated.
func (r *Registry) effectFor(agent, qualified string) EffectClass {
	for _, tl := range r.ToolsFor(agent) {
		if tl.Qualified == qualified {
			return tl.Effect
		}
	}
	// Unreachable in practice (Call only gets here for available tools);
	// irreversible is the only answer that fails safe.
	return EffectClassIrreversible
}

// AttachClient wires a live client (T2) for a configured server. The
// registry owns NO process lifecycle — the daemon dials, T8's degradation
// detaches on death — and a server with no attached client is answered
// exactly as T8 will answer it: absence, reported.
func (r *Registry) AttachClient(server string, c *Client) error {
	if !r.configured(server) {
		return fmt.Errorf("mcp: server %q is not configured — attaching a client to a name the operator never wrote would be discovery wearing a handshake (MC3)", server)
	}
	r.lmu.Lock()
	defer r.lmu.Unlock()
	if r.clients == nil {
		r.clients = map[string]*Client{}
	}
	r.clients[server] = c
	return nil
}

func (r *Registry) client(server string) (*Client, bool) {
	r.lmu.Lock()
	defer r.lmu.Unlock()
	c, ok := r.clients[server]
	return c, ok
}

// Call is THE call path (T5 pins its refusals; T6/T7 make it consult the
// policy evaluator). One path exists so there is no second place that
// might answer `denied` where `unknown` belongs, and result labelling
// lives IN the path — a caller cannot forget it because there is no
// calling without it.
//
// The policy set arrives per call and is held by nobody (MC4 — grants are
// policies, so this package holds no grant state; T7's grep pins it). `at`
// is the step's single instant, supplied by the caller (S10 purity: the
// clock is a parameter).
//
// The evaluation order IS the semantics:
//
//	visibility first (unknown answers before any policy is consulted —
//	consulting policy for a private tool of another agent would let the
//	policy set itself become a disclosure channel),
//	classification second (unclassified refuses whatever the rules say),
//	policy third: deny → S10's sentence; DefaultDeny → MC10 ("no allow rule
//	grants it" is exactly "no allow ⇒ unavailable"); matched ask → S10's
//	ask sentence + consent owed;
//	effect last: an ALLOWED outward/irreversible tool still asks at use
//	(L22/MC5). ToolAccess answers "may this agent use this server's tools
//	at all"; the effect class answers "does this use need a human". They
//	are different questions — W22's two classes were always two, and a
//	grant named after one does not silence the other. (There is no
//	"explicit allow" escape hatch here: the ToolAccess evaluator MATCHES
//	only a spec that names the tool, so every possible allow is already
//	"explicit" — the distinction cannot vary, and inventing it would be
//	inventing a wildcard the engine refuses to have.)
func (r *Registry) Call(ctx context.Context, agent, qualified string, args json.RawMessage, ps []policy.Policy, at time.Time) (CallOutcome, error) {
	server, tool, ok := splitQualified(qualified)
	st, missing, _ := r.Availability(agent, qualified)
	switch {
	case !ok:
		// A core name called through the registry: core calls belong to the
		// step's toolset (S11 owns them); the registry's call path is for
		// MCP tiers. Answer like any other unknown rather than pretending.
		return CallOutcome{Content: "unknown tool: " + qualified, IsError: true}, nil
	case st == AvailabilityUnknown:
		return CallOutcome{Content: "unknown tool: " + qualified, IsError: true}, nil
	case st == AvailabilityUnavailable:
		return CallOutcome{Content: UnavailableReason(qualified, missing), IsError: true}, nil
	}

	v := policy.Evaluate(ps, policy.Request{
		Type:      policy.ToolAccess,
		Agent:     agent,
		MCPServer: server,
		Tool:      qualified,
		// S74: the address lets a `mcp.<server>.*.read` pattern decide.
		ToolAddress: r.addressFor(agent, qualified),
	}, at)
	switch {
	case v.Unimplemented:
		// D10: "nobody can answer yet" never renders as "a rule refused
		// you"; S10's formatter already says it in its own shape.
		return CallOutcome{Content: v.Sentence(), IsError: true}, nil
	case v.DefaultDeny:
		return CallOutcome{Content: mc10Sentence(qualified), IsError: true}, nil
	case v.Asked:
		return CallOutcome{Content: v.Sentence(), AskRequired: true}, nil
	case !v.Allowed:
		// A matched deny: S10's sentence VERBATIM — the one formatter,
		// unprefixed (T7 pins it byte-exact).
		return CallOutcome{Content: v.Sentence(), IsError: true}, nil
	}

	// Allowed. The effect class gets the last word on *use*: outward or
	// irreversible asks at use time even under a grant (L22/MC5).
	eff := r.effectFor(agent, qualified)
	if eff == EffectClassOutward || eff == EffectClassIrreversible {
		return CallOutcome{Content: effectAskSentence(qualified, eff), AskRequired: true}, nil
	}

	c, connected := r.client(server)
	if !connected {
		// Reported absence (T8): "the server did not answer" is a different
		// fact from "the tool found nothing", and is not this agent's fault.
		return CallOutcome{Content: "unavailable: MCP server " + strconvQuote(server) +
			" is not connected — its tools are reported absent, not silently dropped (N8)", IsError: true}, nil
	}
	// The ceiling (T8/MC9): the caller's bound may shorten, never lengthen —
	// a call that could raise its own has no ceiling (bash's rule, W13, in
	// this package's shape).
	cctx, cancel, bound := boundCall(ctx)
	defer cancel()
	res, err := c.CallTool(cctx, tool, args)
	if err != nil {
		if isTimeout(err) {
			// The third outcome, rendered as itself: never empty, never a
			// generic error, never silently retried.
			return CallOutcome{Content: timeoutSentence(server, qualified, bound), IsError: true, TimedOut: true}, nil
		}
		return CallOutcome{}, err // transport-level failure: the caller reports it as infrastructure
	}
	content := res.Text
	if res.IsError {
		return CallOutcome{Content: LabelResult(server, tool, content), IsError: true}, nil
	}
	return CallOutcome{Content: LabelResult(server, tool, content)}, nil
}
