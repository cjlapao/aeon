package policy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// S14 T7 — the SkillAccess evaluator that replaced the S10 placeholder.
// The placeholder's contract was: the shape ships first so rows can be
// authored today; this file proves the decision arrived without moving the
// shape.

var skillAccessCase = stubCase{
	typ: policy.SkillAccess, shape: `{"skill": string, "state": "allow"|"deny"}`,
	key: "skill", value: "reddit-agent/summarise-sentiment", hasState: true,
	valid: `{"skill":"reddit-agent/summarise-sentiment","state":"allow"}`,
	set:   func(r *policy.Request, v string) { r.Skill = v },
}

func skillPolicy(tb testing.TB, id, ref string, action policy.Action) policy.Policy {
	tb.Helper()
	return specPolicy(tb, policy.SkillAccess, id, policy.AllAgents, "", action,
		`{"skill":"`+ref+`","state":"`+string(action)+`"}`)
}

func skillReq(agent, ref string) policy.Request {
	return policy.Request{Type: policy.SkillAccess, Agent: agent, Skill: ref}
}

func TestSkillAccessRowsAuthoredUnderThePlaceholderNowDecide(t *testing.T) {
	// The row an operator could write in S10 — same shape, same authorship —
	// is the row S14 honours. Nothing about storage changed; only the
	// evaluator did.
	ref := "reddit-agent/summarise-sentiment"
	ps := []policy.Policy{skillPolicy(t, "grant-1", ref, policy.Allow)}
	v := policy.Evaluate(ps, skillReq("planner", ref), evalAt)
	if !v.Allowed {
		t.Fatalf("the stored allow must decide now: %s", v.Sentence())
	}
	if v.Unimplemented {
		t.Fatal("SkillAccess still answers could-not-determine — the placeholder was not replaced")
	}
}

func TestSkillAccessDenyOutranksAllowRegardlessOfSpecificity(t *testing.T) {
	// PL4, on the new type: a broad allow and a specific deny, deny wins,
	// and the sentence names the allow it looked past.
	ref := "reddit-agent/summarise-sentiment"
	allow := skillPolicy(t, "grant-wide", ref, policy.Allow)
	deny := specPolicy(t, policy.SkillAccess, "deny-narrow", policy.AgentKind, "planner", policy.Deny,
		`{"skill":"`+ref+`","state":"deny"}`)
	v := policy.Evaluate([]policy.Policy{allow, deny}, skillReq("planner", ref), evalAt)
	if v.Allowed {
		t.Fatalf("deny does not lose to specificity: %s", v.Sentence())
	}
	if v.By == nil || v.By.ID != "deny-narrow" {
		t.Fatalf("the deny decided and the sentence names it: %s", v.Sentence())
	}
	if !strings.Contains(v.Sentence(), "grant-wide") {
		t.Fatalf("PL4 made visible — the lost grant is named: %s", v.Sentence())
	}
}

func TestSkillAccessMatchesExactlyNoWildcards(t *testing.T) {
	// The pinned shape promises a skill name, and a wildcard here would be a
	// grant whose breadth nobody typed. A row for "*" matches a request that
	// literally names "*"; nothing else inherits it.
	ps := []policy.Policy{skillPolicy(t, "star", "*", policy.Allow)}
	v := policy.Evaluate(ps, skillReq("planner", "reddit-agent/summarise-sentiment"), evalAt)
	if v.Allowed || !v.DefaultDeny {
		t.Fatalf("a wildcard row must not open a named skill: %s", v.Sentence())
	}
	v2 := policy.Evaluate(ps, skillReq("planner", "*"), evalAt)
	if !v2.Allowed {
		t.Fatalf("exact comparison still matches the literal: %s", v2.Sentence())
	}
	// Partial prefixes do not match either.
	ps2 := []policy.Policy{skillPolicy(t, "prefix", "reddit-agent", policy.Allow)}
	if v3 := policy.Evaluate(ps2, skillReq("planner", "reddit-agent/summarise-sentiment"), evalAt); v3.Allowed {
		t.Fatal("the agent half is not a grant over its skills")
	}
}

func TestSkillAccessInertArmsPermitNothing(t *testing.T) {
	// Every unreadable or self-contradicting row decides NOTHING: no panic,
	// no permission — PL5's default deny is what the caller sees, and it
	// reads as the default deny (By == nil), not as a rule refusal.
	cases := []struct {
		name   string
		spec   string
		action policy.Action
		reqRef string
	}{
		{"not the pinned shape", `{"skill":123}`, policy.Allow, "a/b"},
		{"empty reference matches nothing", `{"skill":"","state":"allow"}`, policy.Allow, ""},
		{"state contradicts action", `{"skill":"a/b","state":"deny"}`, policy.Allow, "a/b"},
		{"action outside the closed set", `{"skill":"a/b","state":"maybe"}`, policy.Action("maybe"), "a/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := policy.Policy{ID: "inert", Type: policy.SkillAccess,
				Target: policy.Target{Kind: policy.AllAgents}, Action: c.action,
				Spec: json.RawMessage(c.spec), Source: policy.Operator}
			v := policy.Evaluate([]policy.Policy{p}, skillReq("planner", c.reqRef), evalAt)
			if v.Allowed {
				t.Fatalf("an inert row permitted: %+v", v)
			}
			if v.By != nil || !v.DefaultDeny {
				t.Fatalf("an inert row must read as the default deny, not a rule refusal: %s", v.Sentence())
			}
			if v.Unimplemented {
				t.Fatal("a decided type must not answer could-not-determine")
			}
		})
	}
}

func TestTheSkillAccessEvaluatorIsNotAStub(t *testing.T) {
	// The seam's own test (S11's pattern for CommandAccess): the registry
	// still holds the type — the seven-name guard covers that — but it no
	// longer carries the Stub() marker, because the could-not-determine
	// answer must have exactly one cause (inforce.go reads the marker and
	// nothing else).
	ev, ok := policy.EvaluatorFor(policy.SkillAccess)
	if !ok {
		t.Fatal("SkillAccess lost its evaluator")
	}
	if marker, isStub := ev.(policy.StubMarker); isStub && marker.Stub() {
		t.Fatal("SkillAccess still declares itself the placeholder while answering for real")
	}
	// And the in-force answer moves on its own: no more could-not-determine
	// for a stored skill_access row.
	ps := []policy.Policy{skillPolicy(t, "g", "a/b", policy.Allow)}
	if v := policy.Evaluate(ps, skillReq("x", "a/b"), evalAt); !v.Allowed {
		t.Fatalf("the type decides: %s", v.Sentence())
	}
}

func TestSkillAccessEmptySetIsTheDefaultDenyNotAThirdState(t *testing.T) {
	// PL5 through the new evaluator: empty set → default deny (By nil,
	// DefaultDeny true) — three answers stay three shapes even after a type
	// fills in.
	v := policy.Evaluate(nil, skillReq("planner", "reddit-agent/summarise-sentiment"), evalAt)
	if v.Allowed || v.Unimplemented || v.By != nil || !v.DefaultDeny {
		t.Fatalf("the empty set must read exactly as the default deny: %+v", v)
	}
}
