package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
)

// S36-mcp-dispatch T2 — the offer, unit-tested with fixtures (no dial, no
// subprocess). The tests pin that the offer is GrantView's partition, not a
// re-derivation: offered is exactly the available rows, withheld carries
// grants.go's sentences verbatim, and core tools never appear.

// t2Config is a config that classifies five public tools for one agent,
// covering each authority state: one local-allowed, one deny, one ask, one
// outward-allowed (L22), and one with no rule. A core tool (read) rides the
// registry automatically, to prove it is excluded from the partition.
func t2Config() mcp.Config {
	return mcp.Config{
		Public: []mcp.ServerConfig{{
			Name:      "alpha",
			Transport: mcp.TransportConfig{Type: "stdio", Command: "unused"},
			Tools: []mcp.ToolConfig{
				{Name: "search", Rerun: "safe", Effect: "local"},
				{Name: "delete", Rerun: "safe", Effect: "outward"},
				{Name: "refuse", Rerun: "safe", Effect: "local"},
				{Name: "asks", Rerun: "safe", Effect: "local"},
				{Name: "norule", Rerun: "safe", Effect: "local"},
			},
		}},
		Agents: map[string]mcp.AgentConfig{
			"agent-a": {Declares: []string{"alpha.search", "alpha.delete", "alpha.refuse", "alpha.asks", "alpha.norule"}},
		},
	}
}

func t2Policy(action policy.Action, agent, qualified, state string) policy.Policy {
	spec, _ := json.Marshal(policy.ToolAccessSpec{ToolName: qualified, State: state})
	return policy.Policy{
		ID:     "t2-" + string(action) + "-" + qualified,
		Type:   policy.ToolAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: action,
		Spec:   spec,
	}
}

// t2Policies is the set that drives each tool to a distinct state.
func t2Policies() []policy.Policy {
	return []policy.Policy{
		t2Policy(policy.Allow, "agent-a", "alpha.search", "allow"),
		t2Policy(policy.Deny, "agent-a", "alpha.refuse", "deny"),
		t2Policy(policy.Ask, "agent-a", "alpha.asks", "ask"),
		t2Policy(policy.Allow, "agent-a", "alpha.delete", "allow"), // effect outward ⇒ ask at use (L22)
	}
}

func t2Registry(t *testing.T) *mcp.Registry {
	t.Helper()
	r, err := mcp.New(t2Config())
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	return r
}

func t2Granted(t *testing.T) (offered []mcp.GrantView, withheld []daemon.MCPWithheld) {
	t.Helper()
	return partitionOffer(t2Registry(t), "agent-a", t2Policies(), time.Now())
}

// TestOfferedIsExactlyGrantAvailable pins that the offered set is GrantView's
// available rows exactly — no second filter criterion.
func TestOfferedIsExactlyGrantAvailable(t *testing.T) {
	offered, _ := t2Granted(t)
	got := map[string]bool{}
	for _, v := range offered {
		got[v.Qualified] = true
	}
	// Only the available row is offered; every other tool is withheld.
	if !got["alpha.search"] {
		t.Errorf("alpha.search (available) not offered: %v", got)
	}
	for _, mustNot := range []string{"alpha.refuse", "alpha.asks", "alpha.delete", "alpha.norule"} {
		if got[mustNot] {
			t.Errorf("%s is not available but was offered", mustNot)
		}
	}
	// Cross-check against GrantView directly: offered == the available rows
	// (minus core, handled in the core test).
	views := t2Registry(t).GrantView("agent-a", t2Policies(), time.Now())
	want := map[string]bool{}
	for _, v := range views {
		if v.Tier == mcp.TierCore {
			continue
		}
		if v.State == mcp.GrantAvailable {
			want[v.Qualified] = true
		}
	}
	if len(got) != len(want) {
		t.Errorf("offered count %d != available count %d (got=%v want=%v)", len(got), len(want), got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("available tool %s missing from offered", k)
		}
	}
}

// TestWithheldSentencesAreGrantViewVerbatim pins MD9: a withheld entry's
// sentence is GrantView's Reason, byte-for-byte — never re-typed.
func TestWithheldSentencesAreGrantViewVerbatim(t *testing.T) {
	at := time.Now()
	_, withheld := partitionOffer(t2Registry(t), "agent-a", t2Policies(), at)
	sent := map[string]string{}
	for _, w := range withheld {
		sent[w.Qualified] = w.Sentence
	}
	// The withheld rows are exactly the non-available, non-core rows.
	wantQuals := map[string]bool{"alpha.refuse": true, "alpha.asks": true, "alpha.delete": true, "alpha.norule": true}
	if len(sent) != len(wantQuals) {
		t.Fatalf("withheld count %d != %d (got=%v)", len(sent), len(wantQuals), sent)
	}
	// Compare each sentence against GrantView's own Reason, computed
	// independently — a re-typed or paraphrased sentence diverges here.
	views := t2Registry(t).GrantView("agent-a", t2Policies(), at)
	byQualified := map[string]mcp.GrantView{}
	for _, v := range views {
		byQualified[v.Qualified] = v
	}
	for q, s := range sent {
		v := byQualified[q]
		if s != v.Reason {
			t.Errorf("%s: withheld sentence %q != GrantView.Reason %q (MD9: one sentence per refusal)", q, s, v.Reason)
		}
	}
	// A spot-check that the sentences are the real ones, not empty.
	if !strings.Contains(sent["alpha.norule"], "MC10") {
		t.Errorf("norule (no allow) should carry the MC10 sentence, got %q", sent["alpha.norule"])
	}
	if !strings.Contains(sent["alpha.delete"], "L22") {
		t.Errorf("alpha.delete (outward, allowed) should carry the L22 ask sentence, got %q", sent["alpha.delete"])
	}
}

// TestCoreToolsExemptFromRePartition pins that core tools never appear in
// either list — their verdict is S11's step gate's alone (MD2), untouched by
// this seam.
func TestCoreToolsExemptFromRePartition(t *testing.T) {
	offered, withheld := t2Granted(t)
	// Every offered row is a qualified (dotted) MCP name — core tools are
	// dot-free and must not be here.
	for _, v := range offered {
		if !strings.Contains(v.Qualified, ".") {
			t.Errorf("a dot-free (core) tool reached the offered set: %q", v.Qualified)
		}
	}
	// A known core tool must be in neither list.
	for _, name := range []string{"read", "write", "bash"} {
		for _, v := range offered {
			if v.Qualified == name {
				t.Errorf("core tool %q in offered set", name)
			}
		}
		for _, w := range withheld {
			if w.Qualified == name {
				t.Errorf("core tool %q in withheld set", name)
			}
		}
	}
}

// TestClockMatchesStepGatesInstant pins Q2: the offer evaluates GrantView's
// rule expiry against the instant the caller supplies (the same clock the
// step's gate decides with), never its own time.Now(). An expiring allow
// flips the partition as `at` crosses its NotAfter — which the offer cannot
// do unless it honors `at`.
func TestClockMatchesStepGatesInstant(t *testing.T) {
	ps := append([]policy.Policy{}, t2Policies()...)
	// Expire alpha.search's allow.
	expiry := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ps[0].NotAfter = &expiry

	inside := expiry.Add(-time.Second)
	after := expiry.Add(time.Second)

	offeredIn, _ := partitionOffer(t2Registry(t), "agent-a", ps, inside)
	if !hasQualified(offeredIn, "alpha.search") {
		t.Errorf("before expiry, the in-force allow should offer alpha.search: %v", offeredIn)
	}
	offeredAfter, withheldAfter := partitionOffer(t2Registry(t), "agent-a", ps, after)
	if hasQualified(offeredAfter, "alpha.search") {
		t.Errorf("after expiry, alpha.search must not be offered")
	}
	if !hasQualifiedW(withheldAfter, "alpha.search") {
		t.Errorf("after expiry, alpha.search must be withheld (its allow lapsed): %v", withheldAfter)
	}
}

func hasQualified(views []mcp.GrantView, q string) bool {
	for _, v := range views {
		if v.Qualified == q {
			return true
		}
	}
	return false
}

func hasQualifiedW(wh []daemon.MCPWithheld, q string) bool {
	for _, w := range wh {
		if w.Qualified == q {
			return true
		}
	}
	return false
}
