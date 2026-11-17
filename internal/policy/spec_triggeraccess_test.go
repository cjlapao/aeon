package policy_test

import (
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// S20 T7 — the TriggerAccess evaluator that replaced the S10 placeholder.
// The placeholder's contract was: the shape ships first so rows can be
// authored today; this file proves the decision arrived without moving the
// shape — the row an operator wrote in S10 is the row S20 honours.

var triggerAccessCase = stubCase{
	typ: policy.TriggerAccess, shape: `{"trigger": string, "state": "allow"|"deny"}`,
	key: "trigger", value: "webhook", hasState: true,
	valid: `{"trigger":"webhook","state":"allow"}`,
	set:   func(r *policy.Request, v string) { r.Trigger = v },
}

func triggerPolicy(tb testing.TB, id, kind string, action policy.Action) policy.Policy {
	tb.Helper()
	return specPolicy(tb, policy.TriggerAccess, id, policy.AllAgents, "", action,
		`{"trigger":"`+kind+`","state":"`+string(action)+`"}`)
}

func triggerReq(agent, kind string) policy.Request {
	return policy.Request{Type: policy.TriggerAccess, Agent: agent, Trigger: kind}
}

func TestTriggerAccessRowsAuthoredUnderThePlaceholderNowDecide(t *testing.T) {
	ps := []policy.Policy{triggerPolicy(t, "grant-wh", "webhook", policy.Allow)}
	v := policy.Evaluate(ps, triggerReq("daemon", "webhook"), evalAt)
	if !v.Allowed {
		t.Fatalf("the stored allow must decide now: %s", v.Sentence())
	}
	if v.Unimplemented {
		t.Fatal("TriggerAccess still answers could-not-determine — the placeholder was not replaced")
	}
}

func TestDeniedTriggerKindCannotFire(t *testing.T) {
	// The design's sentence made executable: "no webhooks here" is a ROW,
	// and the row refuses — for BOTH the kinds a deployment worries about.
	for _, kind := range []string{"schedule", "webhook"} {
		ps := []policy.Policy{
			triggerPolicy(t, "allow-all-both", "schedule", policy.Allow),
			triggerPolicy(t, "allow-all-both-2", "webhook", policy.Allow),
			specPolicy(t, policy.TriggerAccess, "deny-here", policy.AgentKind, "prod-daemon", policy.Deny,
				`{"trigger":"`+kind+`","state":"deny"}`),
		}
		v := policy.Evaluate(ps, triggerReq("prod-daemon", kind), evalAt)
		if v.Allowed {
			t.Fatalf("no %s here is an operator sentence and the engine hears it: %s", kind, v.Sentence())
		}
		if v.By == nil || v.By.ID != "deny-here" {
			t.Fatalf("the deny decided and the sentence names it: %s", v.Sentence())
		}
	}
}

func TestTriggerAccessDenyOutranksAllowRegardlessOfSpecificity(t *testing.T) {
	// PL4 on the new type: broad allow, specific deny, deny wins — and the
	// sentence names the allow it looked past (PL4's visibility).
	allow := triggerPolicy(t, "grant-wide", "schedule", policy.Allow)
	deny := specPolicy(t, policy.TriggerAccess, "deny-daemon", policy.AgentKind, "prod-daemon", policy.Deny,
		`{"trigger":"schedule","state":"deny"}`)
	v := policy.Evaluate([]policy.Policy{allow, deny}, triggerReq("prod-daemon", "schedule"), evalAt)
	if v.Allowed {
		t.Fatalf("deny does not lose to specificity: %s", v.Sentence())
	}
	if v.OverriddenAllow == nil || v.OverriddenAllow.ID != "grant-wide" {
		t.Fatalf("the overridden allow is visible, not surprising: %s", v.Sentence())
	}
}

func TestTriggerAccessDefaultDenyIsTheEmptyPolicyState(t *testing.T) {
	// PL5 measured, not assumed: with NO trigger_access rows — the fresh
	// install — nothing fires, and the sentence says default deny, not a
	// bug and not "could not say" (the evaluator EXISTS; the operator has
	// simply not permitted anything yet). This is the fact T7's notes must
	// shout: a fresh install fires nothing until a row says otherwise.
	v := policy.Evaluate(nil, triggerReq("daemon", "schedule"), evalAt)
	if v.Allowed || !v.DefaultDeny || v.Unimplemented {
		t.Fatalf("empty policy is the default-deny state, not unimplemented, not allowed: %+v — %s", v, v.Sentence())
	}
}

func TestTriggerAccessKindWordsAreTheClosedThreeExactly(t *testing.T) {
	// The matching domain is the kind vocabulary trigger.Schedule speaks.
	// A row naming "cron" or "SCHEDULE" matches nothing (exact comparison,
	// no wildcards) — inert toward deny, PL5 does the refusing.
	for _, typed := range []string{"cron", "SCHEDULE", "*", "webhooks", " "} {
		ps := []policy.Policy{triggerPolicy(t, "typo-row", typed, policy.Allow)}
		v := policy.Evaluate(ps, triggerReq("daemon", "schedule"), evalAt)
		if v.Allowed {
			t.Fatalf("a typo typed as an allow decided: %q — %s", typed, v.Sentence())
		}
	}
	// The three that match their own kind.
	for _, kind := range []string{"manual", "schedule", "webhook"} {
		ps := []policy.Policy{triggerPolicy(t, "grant-"+kind, kind, policy.Allow)}
		v := policy.Evaluate(ps, triggerReq("daemon", kind), evalAt)
		if !v.Allowed {
			t.Fatalf("the closed three must speak their own word (%s): %s", kind, v.Sentence())
		}
	}
}

func TestTriggerAccessVacuousRowsMatchNothing(t *testing.T) {
	// The evaluator's own defence: a row whose trigger is whitespace and a
	// request naming no kind — neither may produce a permission.
	ps := []policy.Policy{specPolicy(t, policy.TriggerAccess, "empty-row", policy.AllAgents, "", policy.Allow,
		`{"trigger":"   ","state":"allow"}`)}
	if v := policy.Evaluate(ps, triggerReq("daemon", "webhook"), evalAt); v.Allowed {
		t.Fatalf("the whitespace row decided: %s", v.Sentence())
	}
	allow := triggerPolicy(t, "real-allow", "webhook", policy.Allow)
	if v := policy.Evaluate([]policy.Policy{allow}, policy.Request{Type: policy.TriggerAccess, Agent: "d"}, evalAt); v.Allowed {
		t.Fatalf("a request naming no kind inherited a grant: %s", v.Sentence())
	}
}
