package policy_test

// Every test here is named as a sentence, because the sentence is the claim
// the slice rests on and a failing run should read as an English complaint.
//
// The ordering invariant, first: deny-wins is proven through a TRIVIAL
// test-only type registered in this file, never through the first production
// spec type. Deny-wins debugged through a real spec is deny-wins attributed
// to that spec (the seed's argument for the evaluator before any real type).
// The task's own grep — the production type's name appearing only in the
// seven-type empty-set loop — passes trivially here, because even that loop
// iterates policy.AllPolicyTypes() and names no type literally.
//
// The groups: deny beats allow (both authorings, specificity never rescues);
// the empty set denies; expiry is exact at the nanosecond boundary and has no
// clock; an expired deny becomes nothing, not an allow and not a denial; a
// deny carries the allow it overrode; unimplemented refuses; and purity,
// asserted as syntax rather than prose.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── The test-only types the whole proof runs on ──────────────────────────────

const (
	testOnlyType   policy.PolicyType = "test_only"   // the {"key": string} spec
	testStubType   policy.PolicyType = "test_stub"   // registered; refuses to decide (the T5 shape)
	testNilType    policy.PolicyType = "test_nil"    // registered with nil evaluator and validator
	testAbsentType policy.PolicyType = "test_absent" // never registered
)

// testKeySpec is the test type's spec — {"key": string} — matched against
// req.Tool, one of Request's per-type value fields. That is Q4 exercised: a
// per-type value rides in the one struct, and only the registered evaluator
// knows which field is meaningful.
type testKeySpec struct {
	Key string `json:"key"`
}

type testKeyEvaluator struct{}

func (testKeyEvaluator) Match(p policy.Policy, r policy.Request) policy.MatchRes {
	var s testKeySpec
	if err := json.Unmarshal(p.Spec, &s); err != nil {
		// A spec that cannot be read matches nothing: inert, toward deny.
		// Totality is the evaluator's share to keep — a malformed row must
		// not become a panic in the runtime or, worse, a permission.
		return policy.MatchRes{Note: `spec is not {"key": string}`}
	}
	if s.Key == "" {
		// An empty key names nothing. Unguarded, `{"key":""}` would match
		// any request whose Tool is unset — a spec that permits everything
		// wearing the costume of a spec.
		return policy.MatchRes{Note: "an empty key names nothing"}
	}
	if s.Key == r.Tool {
		return policy.MatchRes{Matched: true}
	}
	return policy.MatchRes{Note: "the key does not name the request's tool"}
}

type testKeyValidator struct{}

func (testKeyValidator) Validate(p policy.Policy) *policy.Reject {
	var s testKeySpec
	if err := json.Unmarshal(p.Spec, &s); err != nil {
		return &policy.Reject{Reason: `test_only spec must be {"key": string}`}
	}
	if s.Key == "" {
		return &policy.Reject{Reason: "test_only spec needs a non-empty key"}
	}
	return nil
}

// testStubEvaluator is the D10 shape T5 will register for the six later-slice
// types: registered, consulted, and refusing — distinguishable from a policy
// deny (nothing decided) and from the default deny (the engine could not
// decide, rather than decided to permit nothing).
type testStubEvaluator struct{}

func (testStubEvaluator) Match(policy.Policy, policy.Request) policy.MatchRes {
	return policy.MatchRes{Unimplemented: true, Note: "the test stub never evaluates"}
}

// Registration happens in init, before any test runs and before any Evaluate
// reads the map: RegisterType is a code-time call, and a registration racing
// a read is exactly the data race the doc on the registry forbids.
func init() {
	policy.RegisterType(testOnlyType, testKeyValidator{}, testKeyEvaluator{})
	policy.RegisterType(testStubType, testKeyValidator{}, testStubEvaluator{})
	// Registered with nothing: an unfinished registration evaluates as
	// unimplemented, not as permitted (D10).
	policy.RegisterType(testNilType, nil, nil)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// evalAt is THE instant every non-expiry decision is made at. Fixed, not
// "now": the clock is an input in this package, so the tests hold one and the
// boundary tests hold three more. Nothing here sleeps or reads a clock —
// TestTheEvaluatorIsPure fails the file that does, test files included.
var evalAt = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func keySpec(key string) json.RawMessage { return json.RawMessage(`{"key":"` + key + `"}`) }

// policyOf builds one policy for the test types. Targets come through
// ParseTarget, so a helper cannot invent an illegal target — T2's conventions
// are reused, not reinvented, and a helper bug surfaces here rather than
// masquerading as an evaluator bug.
func policyOf(tb testing.TB, typ policy.PolicyType, id string, kind policy.TargetKind, targetID string,
	action policy.Action, key string) policy.Policy {
	tb.Helper()
	target, rej := policy.ParseTarget(kind, targetID)
	if rej != nil {
		tb.Fatalf("helper built an illegal target %q/%q: %s", kind, targetID, rej.Reason)
	}
	return policy.Policy{
		ID:     id,
		Type:   typ,
		Target: target,
		Action: action,
		Spec:   keySpec(key),
		Source: policy.Operator,
	}
}

// testPolicy builds a test_only policy — the type deny-wins is proven through.
func testPolicy(tb testing.TB, id string, kind policy.TargetKind, targetID string,
	action policy.Action, key string) policy.Policy {
	tb.Helper()
	return policyOf(tb, testOnlyType, id, kind, targetID, action, key)
}

// expiring sets the expiry. It takes the instant by value so each caller
// gets its own NotAfter pointer, and the pointer is the policy's own.
func expiring(p policy.Policy, at time.Time) policy.Policy {
	instant := at
	p.NotAfter = &instant
	return p
}

func testReq(tool string) policy.Request {
	return policy.Request{Type: testOnlyType, Agent: "main", Tool: tool}
}

// assertVerdict is the one comparison, so no test quietly checks only the
// half it cares about. It pins every Verdict decision field against the
// claim: the three decision flags (Allowed, DefaultDeny, Unimplemented — and
// Asked, pinned FALSE here, because this is the baseline every non-ask path
// uses; the asked path goes through assertAskVerdict, which pins it true),
// which policy (by ID) decided, which allow (by ID) was overridden — nil-ness
// included — and the Reason contract: non-empty exactly on the unimplemented
// paths, where it must quote the type.
func assertVerdict(tb testing.TB, v policy.Verdict, allowed, defaultDeny, unimplemented bool,
	byID, overriddenID string) {
	tb.Helper()
	where := func() string { return fmt.Sprintf("%+v", shapeOf(v)) }
	if v.Allowed != allowed {
		tb.Errorf("Allowed = %v, want %v; verdict %s", v.Allowed, allowed, where())
	}
	if v.Asked {
		// The baseline: a verdict this helper is called on routes nobody to
		// a human. An ask arriving here is either the fold firing where the
		// test claims it does not, or the wrong helper being called.
		tb.Errorf("Asked = true on a non-ask assertion; an asked verdict goes through "+
			"assertAskVerdict: %s", where())
	}
	if v.DefaultDeny != defaultDeny {
		tb.Errorf("DefaultDeny = %v, want %v; verdict %s", v.DefaultDeny, defaultDeny, where())
	}
	if v.Unimplemented != unimplemented {
		tb.Errorf("Unimplemented = %v, want %v; verdict %s", v.Unimplemented, unimplemented, where())
	}
	if v.DefaultDeny && v.Unimplemented {
		// "Nobody could decide" and "the engine decided to permit nothing"
		// are different answers (T11's three states); never both.
		tb.Errorf("verdict claims both default-deny and unimplemented: %s", where())
	}
	checkNamed(tb, "By", v.By, byID)
	checkNamed(tb, "OverriddenAllow", v.OverriddenAllow, overriddenID)
	switch {
	case unimplemented && strings.TrimSpace(v.Reason) == "":
		tb.Errorf("an unimplemented verdict carries no Reason; a refusal names what it refused: %s", where())
	case !unimplemented && v.Reason != "":
		// Fixed strings only, and only where the design says prose: T4
		// renders sentences from the fields, so anything else here is a
		// second formatter's draft.
		tb.Errorf("verdict carries Reason %q on a %s path; prose belongs to verdict.go (T4)", v.Reason, where())
	}
}

// assertAskVerdict is the asked verdict's comparison — the fourth shape
// S11's D1 adds. It pins Asked TRUE and the other three flags FALSE (an ask
// is a decision the engine made: not a permission, not a default deny, not
// could-not-determine — T11's three-state answer depends on the four being
// distinguishable), the deciding ask policy by ID, and the allow it
// outranked, when there was one.
func assertAskVerdict(tb testing.TB, v policy.Verdict, byID, overriddenID string) {
	tb.Helper()
	where := func() string { return fmt.Sprintf("%+v", shapeOf(v)) }
	if !v.Asked {
		tb.Errorf("Asked = false, want true; the fold ranks ask above allow: %s", where())
	}
	if v.Allowed {
		tb.Errorf("Allowed = true on an asked verdict; an ask is NEVER a permission — nobody may act on it: %s", where())
	}
	if v.DefaultDeny {
		tb.Errorf("DefaultDeny = true alongside Asked; a rule routed the question, so this is not PL5: %s", where())
	}
	if v.Unimplemented {
		tb.Errorf("Unimplemented = true alongside Asked; the engine decided, it did not refuse to decide: %s", where())
	}
	if v.Reason != "" {
		tb.Errorf("an asked verdict carries Reason %q; prose belongs to verdict.go (T4): %s", v.Reason, where())
	}
	checkNamed(tb, "By", v.By, byID)
	checkNamed(tb, "OverriddenAllow", v.OverriddenAllow, overriddenID)
	if v.Asked && v.By == nil {
		// Distinct from a default deny and from could-not-determine (the
		// three-state honesty T11 renders): an ask that cannot name its
		// rule is neither of those, and the engine never produces it.
		tb.Errorf("an asked verdict names no deciding policy; By != nil is what makes it the fourth shape: %s", where())
	}
}

func checkNamed(tb testing.TB, field string, got *policy.Policy, wantID string) {
	tb.Helper()
	if wantID == "" {
		if got != nil {
			tb.Errorf("%s = %q, want nil; the verdict must name exactly the rules that acted", field, got.ID)
		}
		return
	}
	if got == nil {
		tb.Errorf("%s = nil, want the policy %q", field, wantID)
		return
	}
	if got.ID != wantID {
		tb.Errorf("%s = %q, want %q", field, got.ID, wantID)
	}
}

// verdictShape strips the pointers to their IDs so a failure prints the whole
// verdict without dumping a Policy (and its spec bytes) into the log.
type verdictShape struct {
	Allowed         bool
	Asked           bool
	By              string
	OverriddenAllow string
	DefaultDeny     bool
	Unimplemented   bool
	Reason          string
}

func shapeOf(v policy.Verdict) verdictShape {
	s := verdictShape{
		Allowed:       v.Allowed,
		Asked:         v.Asked,
		DefaultDeny:   v.DefaultDeny,
		Unimplemented: v.Unimplemented,
		Reason:        v.Reason,
	}
	if v.By != nil {
		s.By = v.By.ID
	}
	if v.OverriddenAllow != nil {
		s.OverriddenAllow = v.OverriddenAllow.ID
	}
	return s
}

// ── deny beats allow (D1/PL4) ────────────────────────────────────────────────

func TestDenyBeatsAllowRegardlessOfTargetSpecificity(t *testing.T) {
	// effective = (union of allows) − (union of denies). "Most specific
	// wins" is the intuitive rule and the wrong one: it lets an agent-level
	// allow defeat a system deny, which is the escalation this design exists
	// to close. Both authorings — the pair in either slice order — must
	// give the same denial: slice order is nobody's decision, and a verdict
	// that moved when a row was appended is a verdict nobody can trust.
	cases := []struct {
		name  string
		allow policy.Policy
		deny  policy.Policy
	}{
		{"specific allow against specific deny",
			testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash"),
			testPolicy(t, "no-bash", policy.AgentKind, "main", policy.Deny, "bash")},
		{"specific allow against broad deny",
			testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash"),
			testPolicy(t, "no-bash-anyone", policy.AllAgents, "", policy.Deny, "bash")},
		{"broad allow against specific deny",
			testPolicy(t, "allow-bash-anyone", policy.AllAgents, "", policy.Allow, "bash"),
			testPolicy(t, "no-bash", policy.AgentKind, "main", policy.Deny, "bash")},
	}
	for _, c := range cases {
		for _, order := range []string{"allow authored first", "deny authored first"} {
			t.Run(c.name+" — "+order, func(t *testing.T) {
				ps := []policy.Policy{c.allow, c.deny}
				if order == "deny authored first" {
					ps = []policy.Policy{c.deny, c.allow}
				}
				v := policy.Evaluate(ps, testReq("bash"), evalAt)
				assertVerdict(t, v, false, false, false, c.deny.ID, c.allow.ID)
			})
		}
	}
}

func TestTheMostSpecificAllowLosesToTheBroadestDeny(t *testing.T) {
	// The demo's §3 through the test type (and T5 repeats it through the
	// real one): a tool granted to one agent, denied to everyone. The most
	// specific allow in the set loses to the broadest deny in it —
	// specificity is a fact about the allow, and the rule that says "not
	// even that" outranks it absolutely.
	granted := testPolicy(t, "grant-bash-to-main", policy.AgentKind, "main", policy.Allow, "bash")
	banned := testPolicy(t, "ban-bash-for-all", policy.AllAgents, "", policy.Deny, "bash")

	for _, order := range [][]policy.Policy{{granted, banned}, {banned, granted}} {
		v := policy.Evaluate(order, testReq("bash"), evalAt)
		assertVerdict(t, v, false, false, false, banned.ID, granted.ID)
		// The verdict names the caller's own element, not a copy of it: the
		// store will later hand Evaluate rows it holds, and the verdict must
		// point into that set, not beside it.
		for i := range order {
			if v.By == &order[i] && order[i].ID == banned.ID {
				break
			}
			if i == len(order)-1 {
				t.Error("By does not alias the deciding element of the caller's slice")
			}
		}
	}
}

// ── the empty set denies (D4/PL5) ────────────────────────────────────────────

func TestAnEmptyPolicySetDeniesForEveryType(t *testing.T) {
	// D4/PL5, looped over exactly seven. For a type the engine CAN evaluate,
	// "nothing matched" is the default deny — a decision. For a type nobody
	// has registered an evaluator for yet (in T3: all seven real ones; ToolAccess
	// got its in S10's T5 and CommandAccess got its in S11, leaving the five
	// refusing stubs), the refusal lands one step earlier as Unimplemented.
	// Either way Allowed == false: an empty set is never a permission, and the
	// two refusal shapes are never both true.
	for _, typ := range policy.AllPolicyTypes() {
		t.Run(string(typ), func(t *testing.T) {
			req := policy.Request{Type: typ, Agent: "main", Loop: "l1", LaneType: "review", MCPServer: "github"}
			for name, ps := range map[string][]policy.Policy{"nil": nil, "empty slice": {}} {
				v := policy.Evaluate(ps, req, evalAt)
				if v.Allowed {
					t.Fatalf("an empty %s policy set permitted %q (%+v); PL5 denies by default", name, typ, shapeOf(v))
				}
				if v.By != nil || v.OverriddenAllow != nil {
					t.Errorf("an empty set named deciding policies (By=%v OverriddenAllow=%v); nothing was there to decide",
						idOf(v.By), idOf(v.OverriddenAllow))
				}
				if v.DefaultDeny == v.Unimplemented {
					t.Errorf("empty-set verdict for %q is neither a default deny nor a refusal — or both: %+v",
						string(typ), shapeOf(v))
				}
			}
		})
	}
	// The registered test type reaches the true PL5 path.
	t.Run("a registered type with nothing to match lands on the default", func(t *testing.T) {
		v := policy.Evaluate(nil, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})
}

func idOf(p *policy.Policy) string {
	if p == nil {
		return "nil"
	}
	return p.ID
}

// ── expiry is exact (D6/D8) ──────────────────────────────────────────────────

func TestExpiryIsExactAtTheBoundary(t *testing.T) {
	// D6. The instant IS the boundary: NotAfter-1ns the policy is alive,
	// NotAfter it is gone, NotAfter+1ns it is gone. No sleeping and no
	// clock reads anywhere —
	// `at` is a parameter (D5), which is the whole reason the nanosecond
	// boundary is testable instead of being an argument about scheduling.
	// Both actions are checked: the boundary must not lean on whether the
	// expiring policy was a permission or a refusal.
	base := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	boundaries := []struct {
		name  string
		at    time.Time
		alive bool
	}{
		{"NotAfter-1ns is still live", base.Add(-time.Nanosecond), true},
		{"NotAfter is gone", base, false},
		{"NotAfter+1ns is gone", base.Add(time.Nanosecond), false},
	}

	for _, c := range boundaries {
		t.Run("allow "+c.name, func(t *testing.T) {
			allow := expiring(testPolicy(t, "time-limited-allow", policy.AgentKind, "main", policy.Allow, "bash"), base)
			ps := []policy.Policy{allow}
			v := policy.Evaluate(ps, testReq("bash"), c.at)
			if c.alive {
				assertVerdict(t, v, true, false, false, allow.ID, "")
				if v.By != &ps[0] {
					t.Error("the live allow is not the slice's own element")
				}
			} else {
				assertVerdict(t, v, false, true, false, "", "")
			}
		})
		t.Run("deny "+c.name, func(t *testing.T) {
			deny := expiring(testPolicy(t, "time-limited-deny", policy.AgentKind, "main", policy.Deny, "bash"), base)
			v := policy.Evaluate([]policy.Policy{deny}, testReq("bash"), c.at)
			if c.alive {
				assertVerdict(t, v, false, false, false, deny.ID, "")
			} else {
				// Gone, not denied: the fall-through is the default, and the
				// boundary instant belongs to "gone".
				assertVerdict(t, v, false, true, false, "", "")
			}
		})
	}
}

func TestANilNotAfterNeverExpires(t *testing.T) {
	// D8/PL7: no expiry is nil, so no instant turns it into one. The far
	// future is the claim; the zero instant proves the comparison never
	// happens at all rather than happening favourably.
	allow := testPolicy(t, "allow-forever", policy.AgentKind, "main", policy.Allow, "bash")
	if allow.NotAfter != nil {
		t.Fatal("the helper gave the policy an expiry")
	}
	ps := []policy.Policy{allow}
	for name, at := range map[string]time.Time{
		"the year 2999":       time.Date(2999, 12, 31, 23, 59, 59, 999999999, time.UTC),
		"the zero instant":    {},
		"beyond any sentinel": time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
	} {
		t.Run(name, func(t *testing.T) {
			v := policy.Evaluate(ps, testReq("bash"), at)
			assertVerdict(t, v, true, false, false, allow.ID, "")
		})
	}
}

// ── an expired deny becomes nothing (D7) ─────────────────────────────────────

func TestAnExpiredDenyFallsThroughToTheDefaultNotToAllow(t *testing.T) {
	// D7, and the easy thing to get subtly wrong. An expired deny stops
	// existing: it must not linger as a phantom denial, and its disappearance
	// must not hand the field to any allow in the set. "Gone" is not
	// "denied", and "the deny is gone" is not "the allow wins" — the default
	// takes over unless the allow itself matches. The expired deny is
	// authored FIRST in every case: if any path let slice position decide,
	// these cases are where it would show.
	deadAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // strictly before evalAt
	expiredDeny := expiring(testPolicy(t, "old-ban", policy.AgentKind, "main", policy.Deny, "bash"), deadAt)
	// The guard on the fixture: a "dead" deny whose expiry is not actually
	// past at evalAt would silently test the live-deny path and pass for the
	// wrong reason (measured: it did). Assert the setup before the verdict.
	if !evalAt.After(deadAt) {
		t.Fatal("the expired deny does not expire before evalAt; these cases would test the wrong instant")
	}

	t.Run("expired deny with no allow at all reaches the default", func(t *testing.T) {
		v := policy.Evaluate([]policy.Policy{expiredDeny}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("expired deny with a live allow that does not match still defaults", func(t *testing.T) {
		// The allow must NOT rescue the request just because the deny
		// vanished: this allow was written for another tool, and "the deny
		// is gone" is not "everything is allowed".
		otherAllow := testPolicy(t, "allow-other", policy.AgentKind, "main", policy.Allow, "read")
		v := policy.Evaluate([]policy.Policy{expiredDeny, otherAllow}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("only the deny expired and the allow live means allowed", func(t *testing.T) {
		// The other direction of "gone ≠ denied": the expired deny leaves no
		// residue behind. This is the same set one instant later, and the
		// difference between this and the case above is only whether the
		// allow itself matches.
		matchingAllow := testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash")
		v := policy.Evaluate([]policy.Policy{expiredDeny, matchingAllow}, testReq("bash"), evalAt)
		assertVerdict(t, v, true, false, false, matchingAllow.ID, "")
	})
}

// ── the deny carries the allow it overrode (D1 made visible) ─────────────────

func TestADenyCarriesTheAllowItOverrode(t *testing.T) {
	// OverriddenAllow is what makes PL4 auditable instead of surprising —
	// "I allowed that!" answered by naming the allow that lost. T4 renders
	// it; the evaluator's job is to carry it, and to carry it deterministically.

	t.Run("the most specific allow is the one carried", func(t *testing.T) {
		broad := testPolicy(t, "all-may-use-bash", policy.AllAgents, "", policy.Allow, "bash")
		narrow := testPolicy(t, "main-may-use-bash", policy.AgentKind, "main", policy.Allow, "bash")
		ban := testPolicy(t, "ban-bash", policy.AllAgents, "", policy.Deny, "bash")
		for _, ps := range [][]policy.Policy{{ban, broad, narrow}, {narrow, broad, ban}, {broad, narrow, ban}} {
			v := policy.Evaluate(ps, testReq("bash"), evalAt)
			assertVerdict(t, v, false, false, false, ban.ID, narrow.ID)
		}
	})

	t.Run("a tie breaks by ID not slice order", func(t *testing.T) {
		// Two equally-specific allows and no other order to break the tie
		// with: the rule is lexicographically smallest ID, so the verdict
		// reads the same in every checkout and after every row append.
		first := testPolicy(t, "allow-a", policy.AgentKind, "main", policy.Allow, "bash")
		last := testPolicy(t, "allow-z", policy.AgentKind, "main", policy.Allow, "bash")
		ban := testPolicy(t, "ban-bash", policy.AgentKind, "main", policy.Deny, "bash")
		for _, ps := range [][]policy.Policy{{ban, first, last}, {ban, last, first}} {
			v := policy.Evaluate(ps, testReq("bash"), evalAt)
			assertVerdict(t, v, false, false, false, ban.ID, first.ID)
		}
	})

	t.Run("the deny named is the most specific deny ties by ID", func(t *testing.T) {
		// By obeys the same determinism rule: broad loses to named, ties go
		// to the smallest ID, and slice order is not a tiebreaker.
		broadBan := testPolicy(t, "ban-all", policy.AllAgents, "", policy.Deny, "bash")
		narrowBan := testPolicy(t, "ban-main", policy.AgentKind, "main", policy.Deny, "bash")
		v := policy.Evaluate([]policy.Policy{narrowBan, broadBan}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, false, false, narrowBan.ID, "")

		siblingA := testPolicy(t, "ban-a", policy.AgentKind, "main", policy.Deny, "bash")
		siblingZ := testPolicy(t, "ban-z", policy.AgentKind, "main", policy.Deny, "bash")
		for _, ps := range [][]policy.Policy{{siblingA, siblingZ}, {siblingZ, siblingA}} {
			v := policy.Evaluate(ps, testReq("bash"), evalAt)
			assertVerdict(t, v, false, false, false, siblingA.ID, "")
		}
	})

	t.Run("a deny with nothing to override carries nothing", func(t *testing.T) {
		ban := testPolicy(t, "ban-bash", policy.AgentKind, "main", policy.Deny, "bash")
		other := testPolicy(t, "allow-read", policy.AgentKind, "main", policy.Allow, "read")
		// The allow is present but does not match this request — there was
		// no allow TO override, and inventing one would put words in a
		// human's mouth that T4 would then render.
		v := policy.Evaluate([]policy.Policy{ban, other}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
	})
}

// ── ask is the middle tier (S11's D1: deny > ask > allow) ───────────────────

func TestTheAskFoldRanksDenyAskAllow(t *testing.T) {
	// The middle tier, table-driven. Every row states which flag lights up
	// and which rules the verdict names; "an ask is never Allowed" is
	// asserted by assertAskVerdict on every asked row, and "a deny still
	// beats everything" is asserted by assertVerdict on every denied one.
	// The specificity twist is deliberate: the narrow ALLOW loses to the
	// broad ASK for the same reason it loses to the broad deny — a rule that
	// routes to a human outranks a rule that does not, and the narrower
	// grant may not silently close the door a broader rule opened onto a
	// person (the Ask constant's own argument).
	cases := []struct {
		name           string
		rows           []policy.Policy
		wantBy         string
		wantOverridden string
		wantAsk        bool
		wantDeny       bool // denied: assertVerdict with all-false flags and By
	}{
		{"ask alone asks",
			[]policy.Policy{testPolicy(t, "ask-bash", policy.AgentKind, "main", policy.Ask, "bash")},
			"ask-bash", "", true, false},
		{"ask beats an allow whatever the specificity",
			[]policy.Policy{
				testPolicy(t, "ask-bash-anyone", policy.AllAgents, "", policy.Ask, "bash"),
				testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash")},
			"ask-bash-anyone", "allow-bash", true, false},
		{"deny beats ask and allow together",
			[]policy.Policy{
				testPolicy(t, "ask-bash", policy.AgentKind, "main", policy.Ask, "bash"),
				testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash"),
				testPolicy(t, "ban-bash-anyone", policy.AllAgents, "", policy.Deny, "bash")},
			"ban-bash-anyone", "allow-bash", false, true},
		{"deny beats ask alone",
			[]policy.Policy{
				testPolicy(t, "ask-bash", policy.AgentKind, "main", policy.Ask, "bash"),
				testPolicy(t, "ban-bash", policy.AgentKind, "main", policy.Deny, "bash")},
			"ban-bash", "", false, true},
		{"an expired ask is gone, not asked — the allow it stood over wins",
			[]policy.Policy{
				expiring(testPolicy(t, "ask-bash", policy.AgentKind, "main", policy.Ask, "bash"), evalAt),
				testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash")},
			"allow-bash", "", false, false}, // the Allowed path is asserted separately below
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := policy.Evaluate(c.rows, testReq("bash"), evalAt)
			switch {
			case c.wantAsk:
				assertAskVerdict(t, v, c.wantBy, c.wantOverridden)
			case c.wantDeny:
				assertVerdict(t, v, false, false, false, c.wantBy, c.wantOverridden)
			default:
				// The expired-ask row: the ask stopped existing (D6/D7
				// mirror), so the remaining allow is allowed — and the
				// verdict is NOT asked: expiry removed the rule, it did not
				// route anybody anywhere.
				assertVerdict(t, v, true, false, false, c.wantBy, "")
			}
		})
	}
}

func TestTheAskFoldNamesTheMostSpecificAsk(t *testing.T) {
	// Same determinism rule as By everywhere else: the verdict names the most
	// specific ask, ties break by ID, slice order decides nothing. Both
	// authoring orders must produce the same verdict, and the allow shown as
	// overridden is the most specific allow lost.
	askBroad := testPolicy(t, "ask-anyone", policy.AllAgents, "", policy.Ask, "bash")
	askNamed := testPolicy(t, "ask-main", policy.AgentKind, "main", policy.Ask, "bash")
	grant := testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash")
	for _, order := range [][]policy.Policy{
		{askBroad, askNamed, grant},
		{grant, askNamed, askBroad},
	} {
		v := policy.Evaluate(order, testReq("bash"), evalAt)
		assertAskVerdict(t, v, "ask-main", "allow-bash")
	}
}

func TestAnExpiredAskFallsThroughToTheDefaultNotToAnAllowItNeverSaw(t *testing.T) {
	// The D7 mirror for the third action: an expired ask is GONE (it does not
	// linger as a phantom question), and with no allow to fall through to,
	// PL5's default answers — the ask does not half-decide from the grave.
	expired := expiring(testPolicy(t, "ask-bash", policy.AgentKind, "main", policy.Ask, "bash"), evalAt)
	v := policy.Evaluate([]policy.Policy{expired}, testReq("bash"), evalAt)
	assertVerdict(t, v, false, true, false, "", "")
}

// ── unimplemented refuses (D10) ──────────────────────────────────────────────

func TestAnUnimplementedTypeRefuses(t *testing.T) {
	// Five of seven have no evaluator until later slices (ToolAccess got its
	// real one in S10, CommandAccess in S11). A default branch that permitted
	// would silently grant everything they govern, so the engine refuses — and
	// says "unimplemented" rather than "denied by policy", because T11's
	// could-not-determine state depends on that distinction existing in the
	// verdict, not in a log line.

	t.Run("a type with no registered evaluator", func(t *testing.T) {
		// Even a set whose every policy would match on target and spec
		// cannot be evaluated: the refusal runs before ps is read.
		set := []policy.Policy{
			policyOf(t, testAbsentType, "would-deny", policy.AgentKind, "main", policy.Deny, "bash"),
			policyOf(t, testAbsentType, "would-allow", policy.AllAgents, "", policy.Allow, "bash"),
		}
		v := policy.Evaluate(set, policy.Request{Type: testAbsentType, Agent: "main", Tool: "bash"}, evalAt)
		assertVerdict(t, v, false, false, true, "", "")
		if !strings.Contains(v.Reason, strconv.Quote(string(testAbsentType))) {
			t.Errorf("the refusal %q does not quote the type it refused", v.Reason)
		}
	})

	t.Run("a type registered with a nil evaluator", func(t *testing.T) {
		// An unfinished registration is not a permission either.
		v := policy.Evaluate(nil, policy.Request{Type: testNilType, Agent: "main", Tool: "bash"}, evalAt)
		assertVerdict(t, v, false, false, true, "", "")
	})

	t.Run("a registered evaluator that refuses to decide", func(t *testing.T) {
		// The T5 shape, proven now: the stub type's evaluator is consulted
		// and answers Unimplemented. The verdict must be distinguishable
		// from BOTH a policy deny (By == nil, not merely Allowed == false)
		// and the default deny (DefaultDeny == false).
		matcher := policyOf(t, testStubType, "stub-rule", policy.AgentKind, "main", policy.Deny, "bash")
		v := policy.Evaluate([]policy.Policy{matcher}, policy.Request{Type: testStubType, Agent: "main", Tool: "bash"}, evalAt)
		assertVerdict(t, v, false, false, true, "", "")
		if !strings.Contains(v.Reason, strconv.Quote(string(testStubType))) {
			t.Errorf("the refusal %q does not quote the type it refused", v.Reason)
		}
	})

	t.Run("another type's unregistered-ness does not contaminate a registered one", func(t *testing.T) {
		// Policies whose Type is not req.Type are inert — other questions'
		// answers. The registered type still decides for itself: here, the
		// default deny, not a refusal.
		inert := policyOf(t, testAbsentType, "someone-elses-rule", policy.AgentKind, "main", policy.Deny, "bash")
		v := policy.Evaluate([]policy.Policy{inert}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("the zero request refuses", func(t *testing.T) {
		// A Type nobody typed is unregistered, and unregistered refuses.
		// Totality with a conservative centre.
		v := policy.Evaluate(nil, policy.Request{}, evalAt)
		assertVerdict(t, v, false, false, true, "", "")
	})
}

// ── shapes the engine is contractually made of ───────────────────────────────

// The compile-time half of the signature contract: these assignments stop
// the build if Evaluate's shape drifts, which is the earliest any of this can
// be caught (the same idiom T2 used for the parsers).
var (
	_ func([]policy.Policy, policy.Request, time.Time) policy.Verdict = policy.Evaluate
	_ func(policy.PolicyType, policy.Validator, policy.Evaluator)     = policy.RegisterType
	_ func(policy.PolicyType) (policy.Validator, bool)                = policy.ValidatorFor
	_ func(policy.PolicyType) (policy.Evaluator, bool)                = policy.EvaluatorFor
	_ func(policy.Request) string                                     = policy.Request.Subject
)

func TestTheEvaluatorHasExactlyTheContractedSignature(t *testing.T) {
	// The runtime half of the same claim: a reading of the types rather than
	// of the file, so editing a signature and this test in one commit cannot
	// quietly redefine what "total" means. Evaluate takes (ps, req, at) and
	// NOTHING else, and returns one Verdict — no Reject, no error, no second
	// chance (PL6).
	ft := reflect.TypeOf(policy.Evaluate)
	if ft.NumIn() != 3 {
		t.Fatalf("Evaluate takes %d parameters, want exactly 3 (ps, req, at)", ft.NumIn())
	}
	wantIn := []reflect.Type{
		reflect.TypeOf([]policy.Policy(nil)),
		reflect.TypeOf(policy.Request{}),
		reflect.TypeOf(time.Time{}),
	}
	for i, want := range wantIn {
		if got := ft.In(i); got != want {
			t.Errorf("Evaluate parameter %d is %s, want %s", i, got, want)
		}
	}
	if ft.NumOut() != 1 {
		t.Fatalf("Evaluate returns %d values, want exactly 1; a total function has no failure channel (PL6)", ft.NumOut())
	}
	if ft.Out(0) != reflect.TypeOf(policy.Verdict{}) {
		t.Errorf("Evaluate returns %s, want policy.Verdict", ft.Out(0))
	}
	errType := reflect.TypeOf((*error)(nil)).Elem()
	for i := 0; i < ft.NumOut(); i++ {
		if out := ft.Out(i); out == errType || out.Implements(errType) {
			t.Errorf("Evaluate returns %s; the evaluator is total and never fails open through an error (PL6)", out)
		}
	}

	// Request is ONE struct (Q4, answered yes): Type + four identity fields +
	// seven per-type values + S74's optional ToolAddress. The count is locked because the drift this
	// prevents is per-type parameters creeping back in — seven functions'
	// disease wearing a struct's clothes.
	rft := reflect.TypeOf(policy.Request{})
	wantFields := []string{"Type", "Agent", "Loop", "LaneType", "MCPServer",
		"Tool", "Command", "Route", "Effect", "Cost", "Skill", "Trigger", "ToolAddress"}
	if rft.NumField() != len(wantFields) {
		t.Fatalf("Request has %d fields (%s), want exactly %d — one struct, per-type fields optional, never per-type parameters (Q4)",
			rft.NumField(), fieldNames(rft), len(wantFields))
	}
	for i, name := range wantFields {
		if rft.Field(i).Name != name {
			t.Errorf("Request field %d is %q, want %q", i, rft.Field(i).Name, name)
		}
	}

	// Verdict is the nine-field shape every later surface renders from; T4,
	// the CLI, the socket and the app all read these names, so a rename here
	// must be a decision, not a typo. The two leading fields arrived with T4:
	// Sentence() is a method on the Verdict, so the verdict has to carry who
	// asked (Subject) and what about (Type) — the two refusals that name no
	// policy have no By to read either from, and a formatter that took them as
	// parameters would be a formatter a caller can hand a different request to.
	// Asked arrived with S11's D1: the third decision sits beside Allowed, not
	// inside it — four distinguishable shapes (permitted, refused, asked,
	// undecided) is what T11's three-state answer is built from.
	// DefaultAsk (2026-11-15) names the fifth cause of an ask: the fall-through, no rule at all
	// (EvaluateAskingWhenUnmatched), which By cannot name because there is no rule.
	vft := reflect.TypeOf(policy.Verdict{})
	wantVerdict := []string{"Subject", "Type", "Allowed", "Asked", "DefaultAsk", "By", "OverriddenAllow",
		"DefaultDeny", "Unimplemented", "Reason"}
	if vft.NumField() != len(wantVerdict) {
		t.Fatalf("Verdict has %d fields (%s), want exactly %d", vft.NumField(), fieldNames(vft), len(wantVerdict))
	}
	policyPtr := reflect.PointerTo(reflect.TypeOf(policy.Policy{}))
	for i, name := range wantVerdict {
		f := vft.Field(i)
		if f.Name != name {
			t.Errorf("Verdict field %d is %q, want %q", i, f.Name, name)
			continue
		}
		if name == "By" || name == "OverriddenAllow" {
			if f.Type != policyPtr {
				t.Errorf("Verdict.%s is %s, want *policy.Policy — the verdict names the policy that acted", name, f.Type)
			}
		}
		if name == "Subject" && f.Type != reflect.TypeOf("") {
			t.Errorf("Verdict.Subject is %s, want string — the sentence names who was refused", f.Type)
		}
		if name == "Type" && f.Type != reflect.TypeOf(policy.ToolAccess) {
			t.Errorf("Verdict.Type is %s, want policy.PolicyType — the D10 sentence names what was asked about", f.Type)
		}
	}
}

func TestTargetKindsMatchOnlyTheirOwnIdentityField(t *testing.T) {
	// The target half of the filter, kind by kind: each named kind compares
	// its ID against the ONE request field that kind governs. A field
	// mix-up — the agent target reading req.Loop, say — is invisible if you
	// only ever exercise one kind, so every kind gets both directions: it
	// matches when its own field carries its ID, and it misses when every
	// OTHER field carries it.
	const governed = "governed"
	kinds := []struct {
		kind policy.TargetKind
		set  func(r *policy.Request, value string)
	}{
		{policy.AgentKind, func(r *policy.Request, s string) { r.Agent = s }},
		{policy.LoopKind, func(r *policy.Request, s string) { r.Loop = s }},
		{policy.LaneTypeKind, func(r *policy.Request, s string) { r.LaneType = s }},
		{policy.MCPServerKind, func(r *policy.Request, s string) { r.MCPServer = s }},
	}
	for _, c := range kinds {
		name := string(c.kind)
		t.Run(name+" matches its own field", func(t *testing.T) {
			req := policy.Request{Type: testOnlyType, Tool: "bash"}
			c.set(&req, governed)
			allow := testPolicy(t, "allow-"+name, c.kind, governed, policy.Allow, "bash")
			v := policy.Evaluate([]policy.Policy{allow}, req, evalAt)
			assertVerdict(t, v, true, false, false, allow.ID, "")
		})
		t.Run(name+" is not answered by the other fields", func(t *testing.T) {
			req := policy.Request{Type: testOnlyType, Tool: "bash",
				Agent: governed, Loop: governed, LaneType: governed, MCPServer: governed}
			c.set(&req, "") // its own field emptied: only decoys remain
			allow := testPolicy(t, "allow-"+name, c.kind, governed, policy.Allow, "bash")
			v := policy.Evaluate([]policy.Policy{allow}, req, evalAt)
			assertVerdict(t, v, false, true, false, "", "")
		})
	}

	t.Run("all-agents matches everything, a zero request included", func(t *testing.T) {
		ban := testPolicy(t, "ban-all", policy.AllAgents, "", policy.Deny, "bash")
		v := policy.Evaluate([]policy.Policy{ban}, policy.Request{Type: testOnlyType, Tool: "bash"}, evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
	})
}

func TestRequestSubjectNamesTheLoopThenTheAgent(t *testing.T) {
	// The subject a refusal is about: the loop when there is one (a rule
	// about this loop outranks a rule about the agent generally), the agent
	// otherwise. Fixed renderings, because T4 puts them in a sentence.
	for _, c := range []struct {
		req  policy.Request
		want string
	}{
		{policy.Request{Agent: "main"}, "agent:main"},
		{policy.Request{Agent: "main", Loop: "aeon-s10-policy"}, "loop:aeon-s10-policy"},
		{policy.Request{Loop: "l1"}, "loop:l1"},
		{policy.Request{}, "agent:"},
	} {
		if got := c.req.Subject(); got != c.want {
			t.Errorf("Request%+v .Subject() = %q, want %q", c.req, got, c.want)
		}
	}
}

func fieldNames(ft reflect.Type) string {
	names := make([]string, ft.NumField())
	for i := range names {
		names[i] = ft.Field(i).Name
	}
	return strings.Join(names, ",")
}

func TestRegistryAccessorsAndCodeTimeRegistration(t *testing.T) {
	// The upward seam (PL3): a type arrives by registering, and the accessors
	// are the one question — "can this type be decided" — asked the same way
	// by Evaluate and by every caller.

	if _, ok := policy.EvaluatorFor(testOnlyType); !ok {
		t.Error("test_only has an evaluator but EvaluatorFor says no")
	}
	if _, ok := policy.ValidatorFor(testOnlyType); !ok {
		t.Error("test_only has a validator but ValidatorFor says no")
	}
	// Registered with nil halves: not-ok, so "registered" never means
	// "decidable" unless BOTH halves are real.
	if _, ok := policy.EvaluatorFor(testNilType); ok {
		t.Error("EvaluatorFor reports a nil evaluator as available")
	}
	if _, ok := policy.ValidatorFor(testNilType); ok {
		t.Error("ValidatorFor reports a nil validator as available")
	}
	if _, ok := policy.EvaluatorFor(testAbsentType); ok {
		t.Error("EvaluatorFor reports an unregistered type as available")
	}

	// A type arriving mid-test the way T5's will arrive mid-package: register
	// it, and Evaluate decides through it without any switch having been
	// edited. (No unregister: this registration persists for the binary, and
	// nothing else names test_dyn.)
	dynType := policy.PolicyType("test_dyn")
	policy.RegisterType(dynType, testKeyValidator{}, testKeyEvaluator{})
	allow := policyOf(t, dynType, "dyn-allow", policy.AgentKind, "main", policy.Allow, "bash")
	v := policy.Evaluate([]policy.Policy{allow}, policy.Request{Type: dynType, Agent: "main", Tool: "bash"}, evalAt)
	assertVerdict(t, v, true, false, false, allow.ID, "")

	// The validator half is a domain refusal, not an error (DB19): same
	// carrier T2 settled on.
	val, ok := policy.ValidatorFor(dynType)
	if !ok {
		t.Fatal("freshly registered type reports no validator")
	}
	if rej := val.Validate(policy.Policy{ID: "x", Type: dynType, Spec: json.RawMessage(`{}`)}); rej == nil ||
		!strings.Contains(strings.ToLower(rej.Reason), "key") {
		t.Errorf("the validator accepted or vaguely refused an empty spec: %v", rej)
	}
}

// ── purity (D5/PL6) and no-default-permits (D10) ─────────────────────────────

// clockCalls are the ways a "pure" decision reads the world through the time
// package. The TYPES of time (Time, Duration) and its pure constructors
// (Date, Parse) are fine — reading a clock is not.
var clockCalls = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Sleep": true,
	"After": true, "Tick": true, "NewTimer": true, "NewTicker": true,
}

func TestTheEvaluatorIsPure(t *testing.T) {
	// D5/PL6. The import list and the clock reads are asserted as SYNTAX, via
	// go/parser — never a text grep: a grep over source hits the guard's own
	// forbidden words in this very file's comments and cries wolf, which is
	// how purity guards get weakened until they match nothing. T2 established
	// the idiom (TestNoFileInThePolicyPackageImportsDatabaseSQL, itself
	// following internal/protocol's import_test.go); this test is that guard
	// for the evaluator's own era.
	//
	// Scope: every *.go in the package. Non-test files are held to the full
	// list; test files to the clock ban as well (a test that sleeps or reads
	// time.Now is the same impurity D5 forbids the engine, and D6's
	// no-sleeps-at-the-boundary claim enforced rather than promised). The
	// `database/sql` ban covers every file, tests included — the same rule
	// types_test.go set: a later task that needs an exception changes BOTH
	// guards in the open, with the reason.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob the package: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("found %d source files in the package; this test would pass almost vacuously", len(files))
	}
	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		isTest := strings.HasSuffix(file, "_test.go")
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == "database/sql" || strings.HasPrefix(path, "database/sql/") {
				t.Errorf("%s imports %q; nothing under internal/policy touches a database (D5) — "+
					"the statements live in internal/store and the adapter holds the boundary", file, path)
			}
			if isTest {
				continue
			}
			for _, bad := range []string{"os", "net", "math/rand"} {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q; the engine is a function of its inputs (D5) — "+
						"the clock arrives only as the `at` parameter", file, path)
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "time" {
				return true
			}
			if clockCalls[sel.Sel.Name] {
				t.Errorf("%s reads or waits on the clock via time.%s; the instant is a parameter, "+
					"constructed from the calendar and compared at the nanosecond — never slept through (D5/D6)",
					file, sel.Sel.Name)
			}
			return true
		})
	}

	// D10, asserted as behaviour rather than as a reading of the source: "no
	// registry default: branch may permit" is exactly the claim that for a
	// type nobody can evaluate, no authored set — however permissive it
	// looks, whichever way you ask — ever answers Allowed == true. Types that
	// later slices have registered are skipped: their permits are earned
	// through matching allows, which the tests above already pin.
	unregistrable := append(policy.AllPolicyTypes(), "", "made_up", "tool", "tool-access")
	for _, typ := range unregistrable {
		if _, ok := policy.EvaluatorFor(typ); ok {
			continue
		}
		req := policy.Request{Type: typ, Agent: "main", Loop: "l1", LaneType: "review", MCPServer: "github", Tool: "bash"}
		permisive := []policy.Policy{
			policyOf(t, typ, "would-allow-all", policy.AllAgents, "", policy.Allow, "bash"),
			policyOf(t, typ, "would-allow-agent", policy.AgentKind, "main", policy.Allow, "bash"),
		}
		for name, ps := range map[string][]policy.Policy{"empty": nil, "a set of matching allows": permisive} {
			v := policy.Evaluate(ps, req, evalAt)
			if v.Allowed {
				t.Errorf("type %q with %s was PERMITTED by an engine that has no evaluator for it (D10)", typ, name)
			}
			if !v.Unimplemented {
				t.Errorf("type %q with %s returned %+v; an unregistered type must REFUSE, not default — the refusal "+
					"shape is what T11 renders as could-not-determine", typ, name, shapeOf(v))
			}
		}
	}
}

func TestDegenerateInputsDecideTowardDeny(t *testing.T) {
	// The totality the interface promises: everything the store can hand
	// back — including the rows a hand-rolled path could build that
	// ParseTarget forbids — produces a Verdict, and every hole denies or
	// refuses rather than permits. This is the half of D10 that lives in the
	// filters rather than the registry.

	t.Run("a named target that names nobody matches nothing", func(t *testing.T) {
		// An agent-kind target with an empty ID against an agent-less
		// request: bare equality would match them and an Allow would
		// permit. It must default-deny instead.
		ghost := policy.Policy{
			ID: "ghost", Type: testOnlyType, Target: policy.Target{Kind: policy.AgentKind},
			Action: policy.Allow, Spec: keySpec("bash"), Source: policy.Operator,
		}
		v := policy.Evaluate([]policy.Policy{ghost}, policy.Request{Type: testOnlyType, Tool: "bash"}, evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("an unknown target kind matches nothing", func(t *testing.T) {
		// 0005's vocabulary reaching the evaluator (a row that skipped every
		// guard) must evaluate as nothing, not as everything.
		stale := policy.Policy{
			ID: "stale", Type: testOnlyType, Target: policy.Target{Kind: policy.TargetKind("global"), ID: "x"},
			Action: policy.Allow, Spec: keySpec("bash"), Source: policy.Operator,
		}
		v := policy.Evaluate([]policy.Policy{stale}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("an action outside the closed set is inert", func(t *testing.T) {
		// A row whose action escaped the closed set (all three real values
		// are in it since migration 0019): it can rescue nothing and permit
		// nothing — it sits out and the default answers.
		neither := policy.Policy{
			ID: "neither", Type: testOnlyType, Target: policy.Target{Kind: policy.AgentKind, ID: "main"},
			Action: policy.Action("permit"), Spec: keySpec("bash"), Source: policy.Operator,
		}
		ban := testPolicy(t, "ban-bash", policy.AgentKind, "main", policy.Deny, "bash")
		v := policy.Evaluate([]policy.Policy{ban, neither}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, false, false, ban.ID, "")
		v = policy.Evaluate([]policy.Policy{neither}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("a spec that cannot be read is inert not fatal", func(t *testing.T) {
		// The totality the evaluator must keep on behalf of every evaluator:
		// a malformed spec produces a verdict (the call itself returning
		// proves it did not panic), and the verdict is the default deny.
		broken := policy.Policy{
			ID: "broken", Type: testOnlyType, Target: policy.Target{Kind: policy.AgentKind, ID: "main"},
			Action: policy.Allow, Spec: json.RawMessage(`{"key":`), Source: policy.Operator,
		}
		v := policy.Evaluate([]policy.Policy{broken}, testReq("bash"), evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})

	t.Run("an empty key in a spec matches nothing", func(t *testing.T) {
		// A spec naming nothing must not match a request naming nothing:
		// `{"key":""}` would otherwise be a permission for every unset tool.
		emptyKey := testPolicy(t, "empty-key", policy.AgentKind, "main", policy.Allow, "")
		v := policy.Evaluate([]policy.Policy{emptyKey}, policy.Request{Type: testOnlyType, Agent: "main"}, evalAt)
		assertVerdict(t, v, false, true, false, "", "")
	})
}

// The amended PL5: with nothing matching, a tool or a command ASKS (when the caller opts in by
// calling EvaluateAskingWhenUnmatched), every other type still refuses, and a matched rule is never
// softened — a deny still denies and an allow still allows.
func TestUnmatchedAsksOnlyForToolsAndCommandsAndNeverSoftensARule(t *testing.T) {
	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC) // the instant is a parameter, never the clock (D5/D6)
	tool := policy.Request{Type: policy.ToolAccess, Agent: "a", Tool: "read"}
	if v := policy.Evaluate(nil, tool, now); !v.DefaultDeny || v.Asked {
		t.Fatalf("plain Evaluate must keep PL5: %+v", v)
	}
	v := policy.EvaluateAskingWhenUnmatched(nil, tool, now)
	if !v.Asked || !v.DefaultAsk || v.Allowed || v.DefaultDeny || v.By != nil {
		t.Fatalf("an unmatched tool call asks: %+v", v)
	}
	if got := v.Sentence(); !strings.HasPrefix(got, "asked: ") || !strings.Contains(got, "no rule covers this") {
		t.Fatalf("sentence = %q", got)
	}
	cmd := policy.Request{Type: policy.CommandAccess, Agent: "a", Tool: "bash", Command: "ls"}
	if v := policy.EvaluateAskingWhenUnmatched(nil, cmd, now); !v.DefaultAsk {
		t.Fatalf("an unmatched command asks: %+v", v)
	}
	route := policy.Request{Type: policy.RouteAccess, Agent: "a", Route: "r"}
	if v := policy.EvaluateAskingWhenUnmatched(nil, route, now); !v.DefaultDeny || v.Asked {
		t.Fatalf("a route with no rule still defaults to deny: %+v", v)
	}
}
