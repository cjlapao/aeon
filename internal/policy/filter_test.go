package policy_test

// The ONE filter (S45 T2, PM5): `policy.list`'s params, the CLI's own
// composition, and the daemon's audit view all mean the same question, so
// the question is asked in one place — here. The tests pin the composition
// (dimensions AND), whole-target equality (no prefix matching, no
// kind-blind id matching), the ONE expiry rule's boundary (the same
// `expired` the evaluator folds with — the filter may never carry its own
// opinion of "gone"), order preservation, and which value counts as
// "filtered nothing" (Empty), since that decides which empty sentence the
// surface owes the caller.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

var filterAt = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func filterRow(id string, typ policy.PolicyType, target policy.Target, action policy.Action, source policy.Source, notAfter *time.Time) policy.Policy {
	return policy.Policy{
		ID: id, Type: typ, Target: target, Action: action, Source: source,
		Spec: json.RawMessage(`{"toolName":"bash","state":"` + string(action) + `"}`), NotAfter: notAfter,
	}
}

// filterNotAfter returns a NotAfter one week on either side of the filter
// instant: future true = still in force at filterAt, false = gone by it.
func filterNotAfter(future bool) *time.Time {
	t := filterAt.AddDate(0, 0, 1)
	if !future {
		t = filterAt.AddDate(0, 0, -1)
	}
	return &t
}

var (
	expiredT   = filterNotAfter(false) // a week ago: expired at filterAt
	liveT      = filterNotAfter(true)  // next week: not expired
	agentBoss  = policy.Target{Kind: policy.AgentKind, ID: "boss"}
	agentBobby = policy.Target{Kind: policy.AgentKind, ID: "bobby"}
	loopBoss   = policy.Target{Kind: policy.LoopKind, ID: "boss"}
	allAgents  = policy.Target{Kind: policy.AllAgents}
)

func corpus() []policy.Policy {
	return []policy.Policy{
		filterRow("t-agent-allow", policy.ToolAccess, agentBoss, policy.Allow, policy.Operator, nil),
		filterRow("t-agent-deny", policy.ToolAccess, agentBoss, policy.Deny, policy.Agent, nil),
		filterRow("t-loop-ask", policy.ToolAccess, loopBoss, policy.Ask, policy.Operator, nil),
		filterRow("c-agent-allow", policy.CommandAccess, agentBoss, policy.Allow, policy.Operator, liveT),
		filterRow("e-expired", policy.ToolAccess, agentBoss, policy.Allow, policy.Operator, expiredT),
		filterRow("r-lane-deny", policy.RouteAccess, policy.Target{Kind: policy.LaneTypeKind, ID: "review"}, policy.Deny, policy.MCPServer, nil),
		filterRow("x-never", policy.SkillAccess, allAgents, policy.Deny, policy.Import, nil),
	}
}

func filterIds(ps []policy.Policy) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func TestPolicyListFiltersCompose(t *testing.T) {
	all := corpus()
	cases := []struct {
		name string
		f    policy.ListFilter
		want []string
	}{
		{"no filter keeps everything, in order", policy.ListFilter{IncludeExpired: true},
			[]string{"t-agent-allow", "t-agent-deny", "t-loop-ask", "c-agent-allow", "e-expired", "r-lane-deny", "x-never"}},
		{"type", policy.ListFilter{Type: policy.RouteAccess, IncludeExpired: true}, []string{"r-lane-deny"}},
		{"target equality (id AND kind — boss-as-loop does not match boss-as-agent)",
			policy.ListFilter{Target: &agentBoss, IncludeExpired: true},
			[]string{"t-agent-allow", "t-agent-deny", "c-agent-allow", "e-expired"}},
		{"no prefix matching — agent:bobby is not agent:bob",
			policy.ListFilter{Target: &agentBobby, IncludeExpired: true}, []string{}},
		{"source", policy.ListFilter{Source: policy.Import, IncludeExpired: true}, []string{"x-never"}},
		{"action", policy.ListFilter{Action: policy.Deny, IncludeExpired: true},
			[]string{"t-agent-deny", "r-lane-deny", "x-never"}},
		{"two dimensions AND", policy.ListFilter{Type: policy.ToolAccess, Action: policy.Allow, IncludeExpired: true},
			[]string{"t-agent-allow", "e-expired"}},
		{"all four dimensions", policy.ListFilter{Type: policy.ToolAccess, Target: &agentBoss, Source: policy.Operator, Action: policy.Allow, IncludeExpired: true},
			[]string{"t-agent-allow", "e-expired"}},
		{"all four dimensions, expiry hidden — the same question minus the gone row",
			policy.ListFilter{Type: policy.ToolAccess, Target: &agentBoss, Source: policy.Operator, Action: policy.Allow},
			[]string{"t-agent-allow"}},
		{"all-agents matches only the whole all-agents target",
			policy.ListFilter{Target: &allAgents, IncludeExpired: true}, []string{"x-never"}},
		{"expiry hidden by itself removes exactly the gone row",
			policy.ListFilter{IncludeExpired: false},
			[]string{"t-agent-allow", "t-agent-deny", "t-loop-ask", "c-agent-allow", "r-lane-deny", "x-never"}},
		{"a combination matching nothing answers empty, not nil and not an error",
			policy.ListFilter{Type: policy.SkillAccess, Target: &agentBoss, IncludeExpired: true}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filterIds(policy.Filter(all, c.f, filterAt))
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v (order is the store's, preserved)", got, c.want)
				}
			}
		})
	}
}

func TestFilterExpiryBoundaryIsTheEngineRule(t *testing.T) {
	// The one expiry rule (evaluate.go's expired: NotAfter != nil AND NOT
	// at.Before(NotAfter)) decides "gone"; the filter reuses it, so the
	// boundary rows must agree with the evaluator's fold, not with a
	// second opinion of the filter's own.
	onTheSecond := policy.Policy{ID: "exactly", Type: policy.ToolAccess, Target: agentBoss, Action: policy.Allow,
		Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`), NotAfter: &filterAt}
	oneNSAfter := filterAt.Add(time.Nanosecond) // one nanosecond of force still left
	justLive := onTheSecond
	justLive.ID = "ns"
	justLive.NotAfter = &oneNSAfter

	fHide := policy.ListFilter{} // includeExpired false — the audit-off view
	fShow := policy.ListFilter{IncludeExpired: true}
	if got := filterIds(policy.Filter([]policy.Policy{onTheSecond}, fHide, filterAt)); len(got) != 0 {
		t.Errorf("at == NotAfter must read as expired (the engine's own rule; at is NOT before NotAfter), got %v", got)
	}
	if got := filterIds(policy.Filter([]policy.Policy{justLive}, fHide, filterAt)); len(got) != 1 {
		t.Errorf("one nanosecond before NotAfter is still in force (PL7's boundary), got %v", got)
	}
	if got := filterIds(policy.Filter([]policy.Policy{onTheSecond}, fShow, filterAt)); len(got) != 1 {
		t.Errorf("the audit view (includeExpired) must show the expired row — D6/D8: an audit view does not hide expiry")
	}
	// No expiry at all is never expired (PL7: nil means no expiry, never a
	// sentinel), under either mode.
	never := policy.Policy{ID: "never", Type: policy.ToolAccess, Target: agentBoss, Action: policy.Allow,
		Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`)}
	if got := filterIds(policy.Filter([]policy.Policy{never}, fHide, filterAt)); len(got) != 1 {
		t.Errorf("NotAfter == nil is no expiry, not an immediate one, got %v", got)
	}
}

func TestFilterEmptyIsTheNoFilterValue(t *testing.T) {
	// Empty() decides which empty sentence the surface owes: the unfiltered
	// corpus-empty gets D4's statement, anything else gets the filter's.
	// The no-filter value is every criterion unset AND IncludeExpired true
	// (the wire's default) — hiding expiry is itself a criterion, because
	// it removes rows.
	if !(policy.ListFilter{IncludeExpired: true}).Empty() {
		t.Error("the wire's zero request must read as unfiltered")
	}
	if (policy.ListFilter{}).Empty() {
		t.Error("includeExpired:false hides rows and is a criterion — not empty")
	}
	if (policy.ListFilter{Type: policy.ToolAccess, IncludeExpired: true}).Empty() {
		t.Error("a stated type is a criterion — not empty")
	}
	tgt := agentBoss
	if (policy.ListFilter{Target: &tgt, IncludeExpired: true}).Empty() {
		t.Error("a stated target is a criterion — not empty")
	}
	if (policy.ListFilter{Source: policy.Operator, IncludeExpired: true}).Empty() {
		t.Error("a stated source is a criterion — not empty")
	}
	if (policy.ListFilter{Action: policy.Ask, IncludeExpired: true}).Empty() {
		t.Error("a stated action is a criterion — not empty")
	}
}
