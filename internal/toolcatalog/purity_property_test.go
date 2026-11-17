package toolcatalog_test

// purity_property_test.go — the two claims that are not about any particular
// answer: that this package computes only from what it is given (no store, no
// clock, no MCP import — read out of the import list, not out of a comment), and
// that the engine's own laws survive the trip through the view (PL4: a deny is
// never an allow, and an allow added under a deny cannot flip one), checked over
// 500 seeded random policy sets.

import (
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
)

// TestEffectiveIsPureAndClockInjected proves the two halves: same inputs, same
// answer — and a different `At` is the ONLY way the answer moves, because the
// package has no clock of its own. Then the import list is parsed and read, so a
// hidden reach at storage or at `internal/mcp` (MC11) is a compiler-visible fact
// and not a review.
func TestEffectiveIsPureAndClockInjected(t *testing.T) {
	in := FixtureInputs()
	a := toolcatalog.ForAgent("atlas", []string{"read", "bash", "grep", "todo", "attach", "gh.list_issues"}, in)
	b := toolcatalog.ForAgent("atlas", []string{"read", "bash", "grep", "todo", "attach", "gh.list_issues"}, in)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("the same inputs gave two answers:\n%+v\n%+v", a, b)
	}
	// The clock is a parameter: the same policy set, one instant apart across the
	// lapsed row, changes exactly the one answer that row could change — and
	// nothing else moves.
	inLater := FixtureInputs()
	inLater.At = At.Add(time.Hour)
	later := toolcatalog.ForAgent("atlas", []string{"read", "bash", "grep", "todo", "attach", "gh.list_issues"}, inLater)
	if !reflect.DeepEqual(a, later) {
		for i := range a {
			if !reflect.DeepEqual(a[i], later[i]) {
				t.Errorf("an hour later the answer changed without the caller moving the clock: %+v → %+v", a[i], later[i])
			}
		}
	}
	// Moving `At` PAST the lapse of the lapsed row must be able to change an
	// answer — otherwise the clock is decorative and the row is inert by
	// construction, which would make the expiry fixture test nothing.
	inAfterLapse := FixtureInputs()
	inAfterLapse.At = lapsed.Add(time.Minute)
	moved := false
	for _, e := range toolcatalog.ForAgent("atlas", []string{"read", "bash", "grep", "todo", "attach", "gh.list_issues"}, inAfterLapse) {
		for _, base := range a {
			if base.Tool == e.Tool && !reflect.DeepEqual(base, e) {
				moved = true
			}
		}
	}
	// The lapsed row is a deny on a tool already allowed, so D6 ("expired is
	// gone, not denied") means the honest expectation is that nothing changes.
	// If something did, the lapse was read as a denial — which is the bug D6
	// exists to prevent, and worth a named failure rather than a silent one.
	if moved {
		t.Error("a row that expired began to DENY at its expiry; D6 says an expired rule stops existing, it does not start refusing")
	}

	// The import list, parsed and not grepped.
	banned := map[string]string{
		"os":                                     "a catalog that reads the environment answers to the machine and not to the caller",
		"database/sql":                           "the store is not here (TC2's separation, and this package has no business in one)",
		"net/http":                               "a catalog that makes requests has started deciding by asking somebody else",
		"github.com/cjlapao/aeon/internal/store": "no row is read here; rows are T5's door's problem, and it will call the store and hand the result in",
		"github.com/cjlapao/aeon/internal/" + "mcp": "MC11: internal/mcp is importable only by cmd/aeon; the catalog takes the MCPView seam instead",
		"github.com/cjlapao/aeon/internal/daemon":   "the catalog is below the daemon, not beside it",
		"github.com/cjlapao/aeon/internal/protocol": "no wire types — T5 renders these entries into protocol results",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		checked++
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if why, bad := banned[path]; bad {
				t.Errorf("%s imports %s — %s", f, path, why)
			}
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// The clock specifically: `At` is the caller's, and a `time.Now()` here
		// would make every fixture expire on some night nobody tested.
		for _, needle := range []string{"time.Now(", "time.Since(", "time.Tick("} {
			if strings.Contains(string(src), needle) {
				t.Errorf("%s reads the clock with %s; the caller supplies Inputs.At", f, needle)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no sources were checked — the guard is testing nothing")
	}
}

// TestPolicyLawsHoldThroughTheCatalog is the property test: 500 seeded random
// policy sets over the fixture agents, and the engine's laws asserted through the
// view. A catalog that could disagree with the evaluator's own laws is a catalog
// that would one day show an operator the opposite of what the gate does.
//
// The laws, from PL4/PL5/D6:
//
//   - a `deny` row that matches never yields `granted_allowed`;
//   - adding an `allow` for an already-denied (tool, agent) cannot flip it — deny
//     outranks allow regardless of specificity, and a view that sorted by
//     specificity instead of folding actions would fail exactly here;
//   - no answer is ever outside the five states;
//   - an agent that holds nothing is `not_granted` whatever the policy set says,
//     including an all-agents allow (the catalog asks the engine about held tools
//     and nothing else).
func TestPolicyLawsHoldThroughTheCatalog(t *testing.T) {
	rng := rand.New(rand.NewSource(20261102))
	pool := []string{"read", "list", "grep", "bash", "todo", "write", "write_file", "attach"}
	agentNames := []string{"atlas", "sentry", "reviewer", "cursitor", "nightly"}
	targets := []struct {
		kind policy.TargetKind
		id   string
	}{
		{policy.AllAgents, ""},
		{policy.AgentKind, "atlas"},
		{policy.AgentKind, "nightly"},
		{policy.LaneTypeKind, "review"},
	}
	const rounds = 500
	deniesSeen, flipsSeen := 0, 0
	for round := 0; round < rounds; round++ {
		n := 1 + rng.Intn(5)
		ps := make([]policy.Policy, 0, n)
		for i := 0; i < n; i++ {
			tg := targets[rng.Intn(len(targets))]
			tool := pool[rng.Intn(len(pool))]
			action := []policy.Action{policy.Allow, policy.Deny, policy.Ask}[rng.Intn(3)]
			ps = append(ps, rowFor(t, "r", tool, action, tg.kind, tg.id))
		}
		in := FixtureInputs()
		in.Policies = ps

		for _, agent := range agentNames {
			for _, tool := range pool {
				e := toolcatalog.Effective(tool, agent, in)
				if !e.State.Valid() {
					t.Fatalf("round %d: %s/%s produced %q, outside the five", round, tool, agent, e.State)
				}
				held := holdsFor(in, agent, tool)
				if !held {
					if e.State != toolcatalog.StateNotGranted {
						t.Fatalf("round %d: %s/%s is held by nobody and reads %s (an all-agents allow leaked past the grant check)",
							round, tool, agent, e.State)
					}
					if e.Reason != "" {
						t.Fatalf("round %d: %s/%s invented %q for a question nobody asked", round, tool, agent, e.Reason)
					}
					continue
				}
				// The evaluator's own answer about this single tool with this set,
				// as the oracle for the deny laws.
				v := policy.Evaluate(ps, policy.Request{Type: policy.ToolAccess, Agent: agent, Tool: tool}, At)
				if v.Allowed && e.State == toolcatalog.StateGrantedDenied {
					t.Fatalf("round %d: the engine allows %s/%s and the catalog says denied", round, tool, agent)
				}
				if hasMatchingDeny(t, ps, agent, tool) {
					deniesSeen++
					if e.State == toolcatalog.StateGrantedAllowed {
						t.Fatalf("round %d: a deny matches %s/%s and the catalog says allowed", round, tool, agent)
					}
					// PL4: an allow added on top cannot flip it.
					plus := append(append([]policy.Policy(nil), ps...),
						rowFor(t, "extra-allow", tool, policy.Allow, policy.AgentKind, agent))
					inPlus := FixtureInputs()
					inPlus.Policies = plus
					if s := toolcatalog.Effective(tool, agent, inPlus).State; s == toolcatalog.StateGrantedAllowed {
						t.Fatalf("round %d: adding an allow flipped a deny for %s/%s (PL4)", round, tool, agent)
					}
					flipsSeen++
				}
				if hasMatchingAsk(t, ps, agent, tool) && e.State == toolcatalog.StateGrantedAllowed {
					t.Fatalf("round %d: an ask matches %s/%s and the catalog says allowed (an ask is not a permission)", round, tool, agent)
				}
			}
		}
	}
	if deniesSeen == 0 || flipsSeen == 0 {
		t.Fatalf("the 500 rounds saw %d matching denies and %d flip attempts — the property never fired", deniesSeen, flipsSeen)
	}
	t.Logf("%d rounds: %d matching denies checked, %d PL4 flip attempts refused", rounds, deniesSeen, flipsSeen)
}

// holdsFor recomputes "does this agent hold this tool" from the fixture cast,
// independently of the package (that is what makes the property test a check and
// not a tautology).
func holdsFor(in toolcatalog.Inputs, agent, tool string) bool {
	agents := FixtureAgents()
	a, ok := agents[agent]
	if !ok || !a.Dispatchable {
		return false
	}
	for _, n := range in.DefaultGrant {
		if n == tool {
			return true
		}
	}
	for _, n := range a.Granted {
		if n == tool {
			return true
		}
	}
	return false
}

// hasMatchingDeny asks the evaluator whether any deny in the set matches, by
// deciding with each row alone — the same trick `PoliciesTouching`'s test uses, so
// neither test reimplements the matcher.
func hasMatchingDeny(t *testing.T, ps []policy.Policy, agent, tool string) bool {
	t.Helper()
	for _, p := range ps {
		if p.Action != policy.Deny {
			continue
		}
		v := policy.Evaluate([]policy.Policy{p}, policy.Request{Type: policy.ToolAccess, Agent: agent, Tool: tool}, At)
		if !(v.DefaultDeny || v.Unimplemented) {
			return true
		}
	}
	return false
}

func hasMatchingAsk(t *testing.T, ps []policy.Policy, agent, tool string) bool {
	t.Helper()
	for _, p := range ps {
		if p.Action != policy.Ask {
			continue
		}
		v := policy.Evaluate([]policy.Policy{p}, policy.Request{Type: policy.ToolAccess, Agent: agent, Tool: tool}, At)
		if !(v.DefaultDeny || v.Unimplemented) {
			return true
		}
	}
	return false
}

// TestNoStateOutsideTheFive closes the vocabulary: the five states are the whole
// set, `States()` lists them, and a string from a fixture, a stale client or a
// future slice does not become a sixth by arriving as text.
func TestNoStateOutsideTheFive(t *testing.T) {
	want := []toolcatalog.State{
		toolcatalog.StateGrantedAllowed,
		toolcatalog.StateGrantedAsk,
		toolcatalog.StateGrantedDenied,
		toolcatalog.StateGrantedNoRule,
		toolcatalog.StateNotGranted,
	}
	if got := toolcatalog.States(); !reflect.DeepEqual(got, want) {
		t.Fatalf("States() = %v, want exactly %v", got, want)
	}
	for _, s := range want {
		if !s.Valid() {
			t.Errorf("%q says it is not a state", s)
		}
	}
	for _, fake := range []string{"", "allowed", "denied", "granted", "no_rule", "unknown", "unavailable", "available", "ask"} {
		if toolcatalog.State(fake).Valid() {
			t.Errorf("%q was accepted as a state — the vocabulary is closed (TC5)", fake)
		}
	}
	// And nothing the package produces is outside them, over the whole fixture
	// grid — including the MCP half.
	in := FixtureInputs()
	for agent := range FixtureAgents() {
		for _, tool := range append(tools.ToolNames(), "gh.list_issues", "gh.create_issue", "gh.search_code", "gh.silent_tool") {
			if s := toolcatalog.Effective(tool, agent, in).State; !s.Valid() {
				t.Errorf("%s/%s produced %q", tool, agent, s)
			}
		}
	}
}

// TestMCPToolNamesAreTheQualifiedOnes pins the two name spaces apart: a system
// tool's name never contains a dot, so the rule that routes a name to the MCP
// half cannot misroute a system tool — and a qualified name is never in the
// registry, so nothing is judged twice.
func TestMCPToolNamesAreTheQualifiedOnes(t *testing.T) {
	for _, n := range tools.ToolNames() {
		if toolcatalog.IsMCPToolName(n) {
			t.Errorf("%q is a registry tool and reads as an MCP name — the routing would send it to the wrong half", n)
		}
	}
	for _, q := range []string{"gh.list_issues", "srv.tool", "a.b"} {
		if !toolcatalog.IsMCPToolName(q) {
			t.Errorf("%q is a qualified name and the routing missed it", q)
		}
	}
	// No MCP fixture name collides with a registry tool either.
	for _, q := range MCPFixtureSet().Held["atlas"] {
		if tools.IsGrantedTool(q) {
			t.Errorf("%q is both an MCP name and a registry tool", q)
		}
	}
}
