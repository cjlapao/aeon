package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// Every rule the agent doors author must be one the policy boundary accepts — the same validator
// `policy.add` runs — so a rule that would be refused is caught here, not when someone creates an agent.
func TestAgentGrantRulesPassThePolicyBoundary(t *testing.T) {
	for name, st := range map[string]agentGrantState{
		"plain":    {Slug: "release-notes-writer", Tools: []string{"read", "grep", "bash", "doc_write"}},
		"imported": {Slug: "groot", Tools: []string{"read", "bash"}, Origin: "import", Signed: false},
		"long":     {Slug: strings.Repeat("a-long-slug-", 5), Tools: []string{"learning_propose_retract", "bash"}},
	} {
		for _, r := range st.rules() {
			p := policy.Policy{
				ID: r.ID, Type: policy.PolicyType(r.Type), Target: policy.Target{Kind: policy.AgentKind, ID: st.Slug},
				Action: policy.Action(r.State), Spec: json.RawMessage(r.Spec), Source: policy.Operator,
			}
			if _, rej := policy.ValidateForAdd(p); rej != nil {
				t.Errorf("%s: rule %s refused by the boundary: %s", name, r.ID, rej.Reason)
			}
			if len([]rune(r.ID)) > maxPolicyIDRunes {
				t.Errorf("%s: id %q is over the cap", name, r.ID)
			}
		}
	}
}

func TestAgentGrantRulesAskForBashAndAllowTheRest(t *testing.T) {
	got := map[string]string{}
	for _, r := range agentGrantRules("worker", []string{"read", "bash", "glob", "read"}, false) {
		got[r.ID] = r.Type + ":" + r.State
	}
	want := map[string]string{
		"grant.worker.read":     "tool_access:allow",
		"grant.worker.glob":     "tool_access:allow",
		"grant.worker.bash":     "tool_access:ask",
		"grant.worker.bash.cmd": "command_access:ask",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %q, want %q", id, got[id], w)
		}
	}
}

func TestAnImportedUnsignedAgentAsksForEverything(t *testing.T) {
	for _, r := range (agentGrantState{Slug: "groot", Tools: []string{"read", "grep"}, Origin: "import"}).rules() {
		if r.State != "ask" {
			t.Errorf("%s = %s, an unsigned import asks for everything", r.ID, r.State)
		}
	}
	// A signed import, and the person's own agent, are not asked more than any other.
	for _, st := range []agentGrantState{{Slug: "a", Tools: []string{"read"}, Origin: "import", Signed: true}, {Slug: "a", Tools: []string{"read"}, Origin: "user"}} {
		if st.rules()[0].State != "allow" {
			t.Errorf("%+v should allow", st)
		}
	}
}

func TestAgentGrantIDIsPredictableAndShortLongOnesStayUnique(t *testing.T) {
	if agentGrantID("a", "read") != "grant.a.read" {
		t.Fatal("a short id is plain")
	}
	long := strings.Repeat("x", 80)
	a, b := agentGrantID(long+"1", "read"), agentGrantID(long+"2", "read")
	if a == b || len([]rune(a)) > maxPolicyIDRunes || a != agentGrantID(long+"1", "read") {
		t.Fatalf("a long id must be capped, stable and distinct: %s %s", a, b)
	}
}

// --- the sync: only what changed is written -------------------------------------------------

type fakeAuthoring struct{ added, removed []string }

func (f *fakeAuthoring) Add(_ context.Context, _ Actor, p PolicyAddParams) (PolicyAddResult, error) {
	f.added = append(f.added, p.ID+":"+p.Action)
	return PolicyAddResult{}, nil
}
func (f *fakeAuthoring) Remove(_ context.Context, _ Actor, id string) (PolicyRemoveResult, error) {
	f.removed = append(f.removed, id)
	return PolicyRemoveResult{}, nil
}
func (f *fakeAuthoring) SeedCorpus(context.Context, Actor, SeedCorpusParams) (SeedCorpusResult, error) {
	return SeedCorpusResult{}, nil
}

func syncTable(f *fakeAuthoring) *Table {
	return &Table{PolicyAuthoring: f, ActorFor: func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }}
}

func TestSyncWritesOnlyWhatChanged(t *testing.T) {
	f := &fakeAuthoring{}
	tbl := syncTable(f)
	before := agentGrantState{Slug: "w", Tools: []string{"read", "bash"}}
	after := agentGrantState{Slug: "w", Tools: []string{"read", "grep"}}
	if err := tbl.syncAgentGrants(context.Background(), nil, &before, &after); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.added, ",") != "grant.w.grep:allow" {
		t.Errorf("added %v, want only the new tool", f.added)
	}
	if strings.Join(f.removed, ",") != "grant.w.bash,grant.w.bash.cmd" {
		t.Errorf("removed %v, want the dropped tool's two rules", f.removed)
	}
}

func TestSyncOnCreateAddsAllAndOnDeleteRemovesAll(t *testing.T) {
	f := &fakeAuthoring{}
	tbl := syncTable(f)
	st := agentGrantState{Slug: "w", Tools: []string{"read", "bash"}}
	if err := tbl.syncAgentGrants(context.Background(), nil, nil, &st); err != nil {
		t.Fatal(err)
	}
	if len(f.added) != 3 || len(f.removed) != 0 {
		t.Fatalf("create: added %v removed %v", f.added, f.removed)
	}
	f2 := &fakeAuthoring{}
	if err := syncTable(f2).syncAgentGrants(context.Background(), nil, &st, nil); err != nil {
		t.Fatal(err)
	}
	if len(f2.removed) != 3 || len(f2.added) != 0 {
		t.Fatalf("delete: added %v removed %v", f2.added, f2.removed)
	}
}

func TestSyncWithNoAuthoringWiredLeavesTheAgentAlone(t *testing.T) {
	st := agentGrantState{Slug: "w", Tools: []string{"read"}}
	if err := (&Table{}).syncAgentGrants(context.Background(), nil, nil, &st); err != nil {
		t.Fatalf("an unwired policy door is not an error: %v", err)
	}
}

// The rules do what the design says when the real policy engine reads them: a granted tool is
// allowed, `bash` asks (the tool, and every command), a tool nobody granted is still denied, and the
// rules govern only their own agent.
func TestAgentGrantRulesDecideAsDesignedInTheRealEngine(t *testing.T) {
	st := agentGrantState{Slug: "w", Tools: []string{"read", "bash"}}
	var ps []policy.Policy
	for _, r := range st.rules() {
		ps = append(ps, policy.Policy{
			ID: r.ID, Type: policy.PolicyType(r.Type), Target: policy.Target{Kind: policy.AgentKind, ID: "w"},
			Action: policy.Action(r.State), Spec: json.RawMessage(r.Spec), Source: policy.Operator,
		})
	}
	at := policyTestNow()
	tool := func(agent, name string) policy.Verdict {
		return policy.Evaluate(ps, policy.Request{Type: policy.ToolAccess, Agent: agent, Tool: name}, at)
	}
	cmd := func(agent, c string) policy.Verdict {
		return policy.Evaluate(ps, policy.Request{Type: policy.CommandAccess, Agent: agent, Command: c}, at)
	}
	if v := tool("w", "read"); !v.Allowed {
		t.Errorf("a granted tool is allowed: %s", v.Sentence())
	}
	if v := tool("w", "bash"); !v.Asked || v.Allowed {
		t.Errorf("bash asks: %s", v.Sentence())
	}
	for _, c := range []string{"ls", "go test ./...", "git status && git push"} {
		if v := cmd("w", c); !v.Asked || v.Allowed {
			t.Errorf("every command asks, %q did not: %s", c, v.Sentence())
		}
	}
	if v := tool("w", "grep"); v.Allowed || v.Asked {
		t.Errorf("a tool nobody granted stays denied: %s", v.Sentence())
	}
	if v := tool("someone-else", "read"); v.Allowed {
		t.Errorf("the rules govern their own agent only: %s", v.Sentence())
	}
}

func policyTestNow() time.Time { return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) }
