package mcp

// grants.go — MC4 made viewable.
//
// **A grant is an operator-authored ToolAccess policy. There is no second
// grant mechanism.** This file holds no state (grep-pinned by
// TestRegistryHoldsNoGrantState — `policy.Policy` appears only as a
// parameter here); it derives an AUTHORITY view from the caller's frozen
// policy set at the caller's instant.
//
// Two views exist and they are not interchangeable:
//
//   - `ToolsFor` is the CAPABILITY view: config facts — what the agent
//     declared and the operator classified. It is what the prompt is built
//     from, frozen with the step, because a policy event mid-step must not
//     flicker the model's tool list (D12's discipline).
//   - `GrantView` is the AUTHORITY view: what the policy set permits at
//     `at`, per tool, with the exact refusal. It is what `aeon mcp list`
//     renders (T9) and what revocation visibly changes (T7).
//
// Core tools appear in both and are exempt here — their verdict is S11's
// step gate's (the frozen set, per step), and a second evaluation in this
// package would be a second place for the verdict to disagree with the gate
// that owns the step.

import (
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// GrantState is the honest fourth answer beside available: *why not*, as a
// kind rather than a prose guess. Three refusals (T5's table) plus owed
// consent — never collapsed, because the CLI and the transcript render them
// differently.
type GrantState string

const (
	GrantAvailable   GrantState = "available"
	GrantUnavailable GrantState = "unavailable" // unclassified (MC6) or no allow (MC10) — the Reason says which
	GrantRefused     GrantState = "refused"     // a rule spoke: S10's sentence is the Reason
	GrantAsk         GrantState = "ask"         // permitted, but consent is owed at use
)

// GrantView is one tool as the authority sees it, for listing and for
// revocation-visibility. Tags added for S34 T2 (`tools.grants`'s wire
// shape, shipped verbatim rather than hand-copied into a second struct).
type GrantView struct {
	Qualified string     `json:"qualified"`
	Tier      Tier       `json:"tier"`
	State     GrantState `json:"state"`
	Reason    string     `json:"reason,omitempty"`
}

// GrantView derives the authority view. Like everything policy-shaped, the
// set and the clock arrive as parameters and are held by nobody.
func (r *Registry) GrantView(agent string, ps []policy.Policy, at time.Time) []GrantView {
	list := r.ToolsFor(agent)
	out := make([]GrantView, 0, len(list))
	for _, tl := range list {
		if tl.Tier == TierCore {
			out = append(out, GrantView{
				Qualified: tl.Qualified,
				Tier:      tl.Tier,
				State:     GrantAvailable,
				Reason:    "core: judged by the step's S11 policy gate (S12 does not re-decide core)",
			})
			continue
		}
		server, _, _ := splitQualified(tl.Qualified)
		v := policy.Evaluate(ps, policy.Request{
			Type:      policy.ToolAccess,
			Agent:     agent,
			MCPServer: server,
			Tool:      tl.Qualified,
			// S74: see Call — the address lets a pattern row decide.
			ToolAddress: ToolAddress(tl),
		}, at)
		switch {
		case v.Unimplemented:
			out = append(out, GrantView{tl.Qualified, tl.Tier, GrantRefused, v.Sentence()})
		case v.DefaultDeny:
			out = append(out, GrantView{tl.Qualified, tl.Tier, GrantUnavailable, mc10Sentence(tl.Qualified)})
		case v.Asked:
			out = append(out, GrantView{tl.Qualified, tl.Tier, GrantAsk, v.Sentence()})
		case !v.Allowed:
			out = append(out, GrantView{tl.Qualified, tl.Tier, GrantRefused, v.Sentence()})
		default:
			if tl.Effect == EffectClassOutward || tl.Effect == EffectClassIrreversible {
				out = append(out, GrantView{tl.Qualified, tl.Tier, GrantAsk, effectAskSentence(tl.Qualified, tl.Effect)})
				continue
			}
			out = append(out, GrantView{tl.Qualified, tl.Tier, GrantAvailable, ""})
		}
	}
	return out
}

// mc10Sentence and effectAskSentence have exactly one definition each,
// shared by the call path (untrusted.go) and this view — a refusal whose
// text depends on which door you asked through is two truths.
func mc10Sentence(qualified string) string {
	return "unavailable: no ToolAccess allow rule grants " + qualified + " (MC10)"
}

func effectAskSentence(qualified string, eff EffectClass) string {
	return "ask: " + qualified + " has effect " + string(eff) +
		" — an outward or irreversible effect asks at use even when allowed (L22)"
}
