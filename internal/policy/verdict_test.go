package policy_test

// Every claim here is about one STRING, printed exactly as a user will read it,
// because this file's subject is the only formatter in the product: the runtime
// refusal (T9), the CLI (T12), the socket (T13), the app (T14) and
// policy_event.detail (T8) all print what Verdict.Sentence() returns. Expected
// values are therefore whole literals — a test that re-derived the sentence from
// the fields would re-implement the formatter and agree with it no matter what
// it did.
//
// The Verdicts are built directly (no store, no evaluator, no clock): a
// rendering claim must depend on nothing but the fields, which is also what
// makes the degenerate ones reachable to test. The one test that does run the
// engine does so to prove Subject/Type travel from the Request into the verdict
// and then into the sentence — the wiring T4 depends on.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── fixtures ─────────────────────────────────────────────────────────────────

// dot is the segment separator verdict.go promises. It is spelled here rather
// than imported so a test failure shows the character the user would see.
const dot = " · "

// gone is a past expiry, and later is a future one; both fixed, never "now" —
// the renderer reads no clock and neither does this file.
var (
	gone  = time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)
	later = time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)
)

// rule builds one policy for a verdict literal. Targets come through
// ParseTarget, so a helper cannot invent an illegal one (T2's convention).
func rule(tb testing.TB, id string, typ policy.PolicyType, kind policy.TargetKind,
	targetID string, action policy.Action, source policy.Source, notAfter *time.Time) *policy.Policy {
	tb.Helper()
	target, rej := policy.ParseTarget(kind, targetID)
	if rej != nil {
		tb.Fatalf("helper built an illegal target %q/%q: %s", kind, targetID, rej.Reason)
	}
	return &policy.Policy{
		ID: id, Type: typ, Target: target, Action: action,
		Source: source, NotAfter: notAfter,
	}
}

// deniedBy is the shape almost every test starts from: a refusal decided by one
// named rule, with no expiry, against an agent-scoped request.
func deniedBy(tb testing.TB, by *policy.Policy, overridden *policy.Policy) policy.Verdict {
	tb.Helper()
	return policy.Verdict{Subject: "agent:main", Type: by.Type, By: by, OverriddenAllow: overridden}
}

// ── the five shapes, pinned to the character (D9/PL8/PL7/PL4/PL5/D10) ────────

func TestARefusalNamesItsPolicyTypeTargetSource(t *testing.T) {
	// D9/PL8, and the slice's own example from AGENTS.md §5: four things in the
	// sentence, each earning its place — the id so the rule can be found and
	// edited, the type so its spec can be read, the target so the scope is
	// obvious, the source so "who decided this" has an answer.
	v := policy.Verdict{
		Subject: "agent:main",
		Type:    policy.ToolAccess,
		By:      rule(t, "no-shell-in-review", policy.ToolAccess, policy.LaneTypeKind, "review", policy.Deny, policy.Operator, nil),
	}
	got := v.Sentence()
	want := `refused: agent:main · tool_access "no-shell-in-review" · target lane-type:review · source operator · no expiry`
	if got != want {
		t.Fatalf("the refusal sentence drifted\n got: %s\nwant: %s", got, want)
	}
	// Each of the four named parts, individually: the whole string can only be
	// right if all four are in it, and a failure that names which one went is
	// worth more than one that says the sentence changed.
	for _, part := range []string{
		`"no-shell-in-review"`, // the policy's id, quoted
		"tool_access",          // the type, so the spec can be read
		"target lane-type:review",
		"source operator",
		"no expiry",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("the refusal omits %s; a refusal must name its policy, type, target and source (D9/PL8): %s", part, got)
		}
	}
	if !strings.HasPrefix(got, "refused: ") {
		t.Errorf("a refusal does not announce itself as one: %s", got)
	}
}

func TestAnAllowRendersTheSameShapeWithItsOwnPrefix(t *testing.T) {
	// The allowed case shares the shape so a reader who understands a refusal
	// understands a grant, and differs in exactly one word — the prefix, which
	// the Allowed field picks (the engine sets it exactly when an allow
	// decided; see TestAVerdictThatNamesNoPolicyStillRefuses for the direction
	// a hand-built verdict that forgot it fails in).
	grant := func(action policy.Action) *policy.Policy {
		return rule(t, "read-only-tools", policy.ToolAccess, policy.LoopKind, "l1", action, policy.Operator, &later)
	}
	v := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess, Allowed: true, By: grant(policy.Allow)}
	got := v.Sentence()
	want := `allowed: loop:l1 · tool_access "read-only-tools" · target loop:l1 · source operator · expires 2999-01-01T00:00:00Z`
	if got != want {
		t.Fatalf("the allow sentence drifted\n got: %s\nwant: %s", got, want)
	}
	deny := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess, Allowed: false, By: grant(policy.Deny)}
	if strings.HasPrefix(deny.Sentence(), "allowed:") {
		t.Errorf("a denial rendered as an allow: %s", deny.Sentence())
	}
	if strings.Count(deny.Sentence(), dot) != strings.Count(got, dot) {
		t.Errorf("refusal and grant are supposed to differ in the prefix only\n grant: %s\nrefusal: %s", got, deny.Sentence())
	}
}

func TestNoExpiryIsStatedNotOmitted(t *testing.T) {
	// PL7. The dangerous pair is "no lifetime" and "a lifetime nobody checked":
	// if the segment went silent they would be the same sentence, and the
	// second is a policy about to stop working.
	base := func(notAfter *time.Time) policy.Verdict {
		return policy.Verdict{Subject: "agent:main", Type: policy.CommandAccess,
			By: rule(t, "rm-rf", policy.CommandAccess, policy.AgentKind, "main", policy.Deny, policy.Agent, notAfter)}
	}
	never := base(nil).Sentence()
	expiring := base(&gone).Sentence()

	if !strings.HasSuffix(never, dot+"no expiry") {
		t.Errorf(`a policy with no expiry must SAY "no expiry", not leave the segment out: %s`, never)
	}
	if strings.Count(never, dot) != strings.Count(expiring, dot) {
		t.Errorf("the two sentences differ by a whole segment, not a dropped one;\n never: %s\n expiry: %s", never, expiring)
	}
	if changed := strings.Replace(expiring, "expires 1999-12-31T23:59:59Z", "no expiry", 1); changed != never {
		t.Errorf("the expiry is the ONLY difference between the two sentences\n got: %s\nwant: %s", never, changed)
	}
	for _, s := range []string{never, expiring} {
		if strings.HasSuffix(s, dot) || strings.Contains(s, dot+dot) {
			t.Errorf("a segment was left blank instead of said: %q", s)
		}
	}
}

func TestAnExpiringPolicyRendersItsInstant(t *testing.T) {
	// PL7's other half: an expiring policy renders its RFC3339 instant, so the
	// reader can see WHEN it stops and go extend or delete it.
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"UTC renders Z", time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC),
			`expires 1999-12-31T23:59:59Z`},
		{"a non-UTC instant renders its offset, still RFC3339",
			time.Date(1999, 12, 31, 23, 59, 59, 0, time.FixedZone("CEST", 2*60*60)),
			`expires 1999-12-31T23:59:59+02:00`},
		{"sub-second precision is not silently kept (RFC3339 has no fractional seconds)",
			time.Date(1999, 12, 31, 23, 59, 59, 123456789, time.UTC),
			`expires 1999-12-31T23:59:59Z`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := c.at
			v := policy.Verdict{Subject: "agent:main", Type: policy.CommandAccess,
				By: rule(t, "rm-rf", policy.CommandAccess, policy.AgentKind, "main", policy.Deny, policy.Agent, &at)}
			got := v.Sentence()
			if !strings.HasSuffix(got, dot+c.want) {
				t.Errorf("expiry segment\n got: %s\nwant suffix: %s%s", got, dot, c.want)
			}
			if want := "expires " + at.Format(time.RFC3339); !strings.HasSuffix(got, want) {
				t.Errorf("the instant is not what time.RFC3339 says: got %q", got)
			}
		})
	}

	// The zero instant is a lie the design forbids (PL7), and the renderer's job
	// is to make it visible rather than to launder it into "no expiry" — a row
	// that expired in year 1 must never read as a rule that never ends.
	zero := time.Time{}
	v := policy.Verdict{Subject: "agent:main", Type: policy.CommandAccess,
		By: rule(t, "rm-rf", policy.CommandAccess, policy.AgentKind, "main", policy.Deny, policy.Agent, &zero)}
	got := v.Sentence()
	if !strings.HasSuffix(got, "expires 0001-01-01T00:00:00Z") {
		t.Errorf("a zero NotAfter should render its own absurd instant, got: %s", got)
	}
	if strings.HasSuffix(got, "no expiry") {
		t.Errorf("a zero NotAfter rendered as NO EXPIRY — the one conflation PL7 forbids: %s", got)
	}
}

func TestADenyThatOverrodeAnAllowNamesTheAllow(t *testing.T) {
	// D1/PL4's visible half. Deny-beats-allow is counter-intuitive — the more
	// specific grant lost — so the sentence names what was lost, id AND target,
	// and "I allowed that!" is answered without opening the table.
	v := deniedBy(t,
		rule(t, "no-shell", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Operator, nil),
		rule(t, "shell-for-main", policy.ToolAccess, policy.AgentKind, "main", policy.Allow, policy.Operator, nil))
	got := v.Sentence()
	want := `refused: agent:main · tool_access "no-shell" · target all-agents · source operator · no expiry` +
		` (an allow exists: "shell-for-main" · agent:main — denied anyway, PL4)`
	if got != want {
		t.Fatalf("the override suffix drifted\n got: %s\nwant: %s", got, want)
	}
	for _, part := range []string{`"shell-for-main"`, "agent:main", "denied anyway, PL4"} {
		if !strings.Contains(got, part) {
			t.Errorf("the denial fails to name the allow it overrode (%s missing): %s", part, got)
		}
	}
	if !strings.HasSuffix(got, ")") {
		t.Errorf("the suffix must close the sentence, not appear mid-stream: %s", got)
	}

	// And it says nothing about an allow when nothing was overridden — an
	// invented "an allow exists" is worse than silence.
	plain := deniedBy(t,
		rule(t, "no-shell", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Operator, nil), nil)
	if strings.Contains(plain.Sentence(), "an allow exists") {
		t.Errorf("a denial with no overridden allow invented one: %s", plain.Sentence())
	}
}

func TestADefaultDenyRendersDifferentlyFromAPolicyDeny(t *testing.T) {
	// PL5 vs a rule. "Nobody allowed you" and "this rule refuses you" are
	// different answers needing different actions: delete or edit a policy,
	// versus author one. A reader must not have to parse the sentence to find
	// out which happened, so they share no marker.
	def := policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, DefaultDeny: true}.Sentence()
	want := `refused: agent:main · no allow matched · default deny (PL5)`
	if def != want {
		t.Fatalf("the default-deny sentence drifted\n got: %s\nwant: %s", def, want)
	}
	deny := deniedBy(t,
		rule(t, "no-shell", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Operator, nil), nil).Sentence()
	if def == deny {
		t.Fatalf("a default deny and a policy deny render identically: %s", def)
	}
	for _, marker := range []string{`"`, "target ", "source ", "expiry"} {
		if strings.Contains(def, marker) {
			t.Errorf("the default-deny sentence names a policy (%q appears) though none decided: %s", marker, def)
		}
	}
	for _, marker := range []string{"no allow matched", "default deny (PL5)"} {
		if !strings.Contains(def, marker) {
			t.Errorf("the default deny does not say so (%q missing): %s", marker, def)
		}
	}
}

func TestTheUnimplementedRefusalIsDistinctFromBothDenySentences(t *testing.T) {
	// D10. "This binary cannot decide that type yet" must never read as "a rule
	// refused you": T11's third state (could not determine) maps here, and the
	// three states are only honest if the strings behind them differ.
	unimpl := policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, Unimplemented: true}.Sentence()
	want := `refused: agent:main · tool_access has no evaluator yet · refusing rather than permitting (D10)`
	if unimpl != want {
		t.Fatalf("the D10 sentence drifted\n got: %s\nwant: %s", unimpl, want)
	}
	def := policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, DefaultDeny: true}.Sentence()
	deny := deniedBy(t,
		rule(t, "no-shell", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Operator, nil), nil).Sentence()
	allow := deniedBy(t,
		rule(t, "read-only", policy.ToolAccess, policy.AllAgents, "", policy.Allow, policy.Operator, nil), nil)
	allow.Allowed = true
	shapes := map[string]string{
		"unimplemented": unimpl, "default deny": def, "policy deny": deny, "policy allow": allow.Sentence(),
	}
	for a, sa := range shapes {
		for b, sb := range shapes {
			if a < b && sa == sb {
				t.Errorf("%s and %s render the same sentence; three states must stay three: %s", a, b, sa)
			}
		}
	}
	for _, marker := range []string{"has no evaluator yet", "(D10)"} {
		if !strings.Contains(unimpl, marker) {
			t.Errorf("the D10 sentence loses %q: %s", marker, unimpl)
		}
	}
	for _, marker := range []string{"default deny", "denied anyway", "target ", "source ", `"`} {
		if strings.Contains(unimpl, marker) {
			t.Errorf("the D10 sentence borrows denial vocabulary (%q): %s", marker, unimpl)
		}
	}
	if strings.Contains(unimpl, "allowed:") {
		t.Errorf("an unimplemented type rendered a permission: %s", unimpl)
	}
}

func TestEveryTargetKindRendersItsCanonicalForm(t *testing.T) {
	// <target> renders `all-agents` bare, and `kind:id` otherwise — through
	// Target.String(), the one target renderer in the product.
	cases := []struct {
		kind     policy.TargetKind
		id       string
		wantTarg string
	}{
		{policy.AllAgents, "", "all-agents"},
		{policy.AgentKind, "main", "agent:main"},
		{policy.LoopKind, "l1", "loop:l1"},
		{policy.LaneTypeKind, "review", "lane-type:review"},
		{policy.MCPServerKind, "github", "mcp:github"},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			by := rule(t, "r1", policy.ToolAccess, c.kind, c.id, policy.Deny, policy.Operator, nil)
			v := deniedBy(t, by, nil)
			got := v.Sentence()
			want := `refused: agent:main · tool_access "r1" · target ` + c.wantTarg +
				` · source operator · no expiry`
			if got != want {
				t.Fatalf("\n got: %s\nwant: %s", got, want)
			}
			// The delegation, pinned behaviourally: the segment IS what the
			// shared renderer says, so a second target rendering cannot drift
			// in here unnoticed.
			if !strings.Contains(got, "target "+by.Target.String()) {
				t.Errorf("target segment is not Target.String(): got %q, want it to contain %q", got, "target "+by.Target.String())
			}
		})
	}
}

func TestTargetRendersComeFromTargetString(t *testing.T) {
	// The override suffix renders the allow's target with the SAME renderer as
	// the deny's — one target rendering, used twice, not two renderings.
	by := rule(t, "broad-deny", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Import, nil)
	lost := rule(t, "narrow-allow", policy.ToolAccess, policy.MCPServerKind, "github", policy.Allow, policy.Operator, nil)
	got := deniedBy(t, by, lost).Sentence()
	if !strings.Contains(got, "target "+by.Target.String()) {
		t.Errorf("the deny's target did not come from Target.String(): %s", got)
	}
	if !strings.Contains(got, dot+lost.Target.String()) {
		t.Errorf("the overridden allow's target did not come from Target.String(): %s", got)
	}
	if want := ` (an allow exists: "narrow-allow" · mcp:github — denied anyway, PL4)`; !strings.HasSuffix(got, want) {
		t.Errorf("suffix\n got: %s\nwant suffix: %s", got, want)
	}
}

// ── the renderer never leaves a hole ─────────────────────────────────────────

func TestASubjectThatIsAbsentStillRendersTheCanonicalForm(t *testing.T) {
	// Evaluate always fills Subject off the Request; a hand-built verdict that
	// does not must render what an anonymous request renders — the same answer
	// from the same renderer (Request.Subject) — rather than a blank where a
	// person's name goes.
	blank := policy.Verdict{Type: policy.ToolAccess, DefaultDeny: true}.Sentence()
	canonical := policy.Verdict{Subject: policy.Request{}.Subject(), Type: policy.ToolAccess, DefaultDeny: true}.Sentence()
	if blank != canonical {
		t.Errorf("a verdict with no subject renders differently from an anonymous request's\n no subject: %s\n canonical: %s", blank, canonical)
	}
	if !strings.HasPrefix(blank, "refused: agent:") {
		t.Errorf("a missing subject went silent instead of naming nobody: %s", blank)
	}
}

func TestTheD10LineNamesTheTypeEvenWhenNoPolicyDecided(t *testing.T) {
	// The whole reason Verdict carries Type: on this path By is nil, so the type
	// — the one thing a reader can go and register — has to travel on the
	// verdict itself.
	named := policy.Verdict{Subject: "loop:l1", Type: policy.SkillAccess, Unimplemented: true}.Sentence()
	if want := `refused: loop:l1 · skill_access has no evaluator yet`; !strings.Contains(named, want) {
		t.Errorf("the D10 line lost the type it refuses to evaluate\n got: %s\nwant it to contain: %s", named, want)
	}
	unnamed := policy.Verdict{Subject: "agent:main", Unimplemented: true}.Sentence()
	if want := "refused: agent:main · unnamed type has no evaluator yet"; !strings.HasPrefix(unnamed, want) {
		t.Errorf("a verdict with no type went blank instead of saying so\n got: %s\nwant prefix: %s", unnamed, want)
	}
	if strings.Contains(unnamed, dot+dot) {
		t.Errorf("an absent type collapsed a segment: %q", unnamed)
	}
}

func TestNoSegmentIsEverBlank(t *testing.T) {
	// The "never silently omitted" rule generalized past the expiry: whatever a
	// hand-built verdict lacks, the sentence says so instead of printing a hole
	// the reader has to notice. Only Evaluate's own verdicts can be complete.
	v := policy.Verdict{By: &policy.Policy{ID: "mystery"}}
	got := v.Sentence()
	if strings.Contains(got, dot+dot) || strings.HasSuffix(got, dot) || strings.Contains(got, "target  ") ||
		strings.Contains(got, "source  ") {
		t.Errorf("a blank segment shipped: %q", got)
	}
	for _, placeholder := range []string{"unnamed type", "unnamed target", "unrecorded source"} {
		if !strings.Contains(got, placeholder) {
			t.Errorf("the absent %s is not named: %s", placeholder, got)
		}
	}
	if !strings.Contains(got, `"mystery"`) {
		t.Errorf("the policy that decided is not named: %s", got)
	}
	// An empty id is quoted-empty rather than dropped: `""` is visibly wrong,
	// a missing segment is not.
	idless := policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess,
		By: &policy.Policy{Type: policy.ToolAccess, Source: policy.Operator}}.Sentence()
	if !strings.Contains(idless, `""`) {
		t.Errorf(`a policy with no id must render the empty quotes, not nothing: %s`, idless)
	}
}

func TestAVerdictThatNamesNoPolicyStillRefuses(t *testing.T) {
	// Fail closed. None of these can come out of Evaluate (every return path
	// sets a decision, and a By wherever one decided), so if one is ever built
	// the renderer must not hand out a permission it cannot attribute.
	const pl5 = "refused: agent:main · no allow matched · default deny (PL5)"
	overridden := rule(t, "orphan-allow", policy.ToolAccess, policy.AgentKind, "main", policy.Allow, policy.Operator, nil)
	cases := []struct {
		name string
		v    policy.Verdict
		want string
	}{
		{"the zero verdict", policy.Verdict{},
			"refused: agent: · no allow matched · default deny (PL5)"},
		{"allowed with no policy to show for it",
			policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, Allowed: true}, pl5},
		{"allowed, naming only an overridden allow",
			policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, Allowed: true, OverriddenAllow: overridden}, pl5},
		{"a deny flag with no deny",
			policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, Allowed: false}, pl5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.v.Sentence()
			if strings.HasPrefix(got, "allowed:") {
				t.Errorf("a verdict naming no policy was rendered as a permission: %s", got)
			}
			if got != c.want {
				t.Errorf("\n got: %s\nwant: %s", got, c.want)
			}
		})
	}
}

func TestThePrefixFollowsTheAllowedFieldSoNoUnattributablePermissionExists(t *testing.T) {
	// "Allowed picks the prefix" is the contract, and the direction it fails in
	// is the one that matters: a By holding an Allow that the verdict does not
	// call an allow renders as a REFUSAL. Evaluate never produces the pair (it
	// sets Allowed exactly when an allow decided), so if a surface ever builds
	// it, someone loses a permission rather than silently gaining one.
	by := rule(t, "unearned", policy.ToolAccess, policy.AgentKind, "main", policy.Allow, policy.Operator, nil)
	got := policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, By: by}.Sentence()
	want := `refused: agent:main · tool_access "unearned" · target agent:main · source operator · no expiry`
	if got != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
	if strings.HasPrefix(got, "allowed:") {
		t.Errorf("a verdict whose Allowed is false printed a permission: %s", got)
	}
}

func TestTheSameVerdictAlwaysRendersTheSameSentence(t *testing.T) {
	// A formatter is a function of the value. A recorded detail and a live `why`
	// must print byte-identical text for one verdict, and a second render must
	// not depend on anything that could have moved in between.
	v := deniedBy(t,
		rule(t, "no-shell", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Operator, &gone), nil)
	first := v.Sentence()
	for i := 0; i < 3; i++ {
		if again := v.Sentence(); again != first {
			t.Fatalf("render %d differs from render 0\n 0: %s\n %d: %s", i+1, first, i+1, again)
		}
	}
	twin := deniedBy(t,
		rule(t, "no-shell", policy.ToolAccess, policy.AllAgents, "", policy.Deny, policy.Operator, &gone), nil)
	if twin.Sentence() != first {
		t.Errorf("two equal verdicts rendered differently: %q vs %q", first, twin.Sentence())
	}
}

// ── the wiring the renderer depends on (T3's Evaluate → T4's sentence) ───────

func TestTheSubjectAndTypeTravelFromTheRequestIntoTheSentence(t *testing.T) {
	// Sentence() takes no arguments, so the verdict has to carry who asked
	// (Subject) and what about (Type) itself — Evaluate fills both off the
	// Request on EVERY return path. A path that forgot one would print a
	// refusal about nobody, which is the exact failure PL8 exists to prevent.
	// The two refusals that name no policy are the interesting ones: with By
	// nil, the verdict is the only place those words can come from.
	inLoop := func(typ policy.PolicyType) policy.Request {
		return policy.Request{Type: typ, Agent: "main", Loop: "l1", Tool: "bash"}
	}
	cases := []struct {
		name   string
		ps     []policy.Policy
		req    policy.Request
		prefix string // the whole sentence starts this way
	}{
		{"a policy denial", []policy.Policy{
			testPolicy(t, "deny-bash", policy.AgentKind, "main", policy.Deny, "bash")},
			inLoop(testOnlyType), "refused: loop:l1 · "},
		{"an allow", []policy.Policy{
			testPolicy(t, "allow-bash", policy.AgentKind, "main", policy.Allow, "bash")},
			inLoop(testOnlyType), "allowed: loop:l1 · "},
		{"the default deny (no policy to name)", nil,
			inLoop(testOnlyType), "refused: loop:l1 · no allow matched"},
		{"an unimplemented type (no policy to name)", nil,
			inLoop(testAbsentType), "refused: loop:l1 · test_absent has no evaluator yet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := policy.Evaluate(c.ps, c.req, evalAt)
			if v.Subject != c.req.Subject() {
				t.Errorf("Verdict.Subject = %q, want the request's %q — the verdict outlives the request, so it carries the answer",
					v.Subject, c.req.Subject())
			}
			if v.Type != c.req.Type {
				t.Errorf("Verdict.Type = %q, want %q", v.Type, c.req.Type)
			}
			got := v.Sentence()
			if !strings.HasPrefix(got, c.prefix) {
				t.Errorf("the sentence does not start by naming who was decided about and what about\n got: %s\nwant prefix: %s", got, c.prefix)
			}
			// The subject in the sentence came from the request, not from a
			// caller who happened to still have it.
			if !strings.Contains(got, c.req.Subject()) {
				t.Errorf("the subject %q is missing from the refusal the user reads: %s", c.req.Subject(), got)
			}
		})
	}

	// A request with no loop falls back to the agent, and the sentence follows.
	v := policy.Evaluate(nil, policy.Request{Type: testOnlyType, Agent: "boss"}, evalAt)
	if got, want := v.Sentence(), "refused: agent:boss · no allow matched · default deny (PL5)"; got != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

// ── DB19: a verdict is not an error type ─────────────────────────────────────

func TestVerdictIsNotAnErrorType(t *testing.T) {
	// S4's DB19. A refusal is a domain outcome the caller branches on; `error`
	// is for the disk being full. Sentence() returning an error would make every
	// surface guess which it got — and would invite a surface to drop the
	// sentence on the floor when the error is non-nil, which is a refusal with
	// no explanation.
	var _ func(policy.Verdict) string = policy.Verdict.Sentence

	errType := reflect.TypeOf((*error)(nil)).Elem()
	stringer := reflect.TypeOf((*fmt.Stringer)(nil)).Elem()
	vt := reflect.TypeOf(policy.Verdict{})

	if vt.Implements(errType) || reflect.PointerTo(vt).Implements(errType) {
		t.Error("policy.Verdict satisfies error; a refusal must not be passable as one (DB19)")
	}
	// And it is deliberately not a fmt.Stringer either: %v of a verdict must
	// stay a field dump. A String() method would let the sentence appear in a
	// log line nobody chose, and T8 binds detail at one call site on purpose.
	if vt.Implements(stringer) || reflect.PointerTo(vt).Implements(stringer) {
		t.Error("policy.Verdict implements fmt.Stringer; the sentence is printed by calling Sentence(), never by accident")
	}

	// Every method on the type, by reflection rather than by reading the file:
	// nothing in the rendering chain may take or return an error.
	if vt.NumMethod() == 0 {
		t.Fatal("Verdict has no methods at all; Sentence() is the whole point of T4")
	}
	found := false
	for i := 0; i < vt.NumMethod(); i++ {
		m := vt.Method(i)
		if m.Name == "Sentence" {
			found = true
		}
		ft := m.Func.Type()
		for j := 1; j < ft.NumIn(); j++ { // In(0) is the receiver
			if in := ft.In(j); in == errType || in.Implements(errType) {
				t.Errorf("Verdict.%s takes %s; a total renderer takes no error", m.Name, in)
			}
		}
		for j := 0; j < ft.NumOut(); j++ {
			if out := ft.Out(j); out == errType || out.Implements(errType) {
				t.Errorf("Verdict.%s returns %s; a verdict is a discriminated result, not an error (DB19)", m.Name, out)
			}
		}
	}
	if !found {
		t.Fatal("Verdict has no Sentence method; verdict.go is the only formatter in the product")
	}

	// Sentence specifically: no arguments to get wrong, one string out.
	sf := reflect.TypeOf(policy.Verdict.Sentence)
	if sf.NumIn() != 1 || sf.NumOut() != 1 || sf.Out(0).Kind() != reflect.String {
		t.Errorf("Sentence is %s; want func(Verdict) string — no options, no error, one sentence", sf)
	}

	// The renderers it leans on are equally error-free.
	for _, c := range []struct {
		name string
		fn   any
	}{
		{"Target.String", policy.Target{}.String},
		{"Request.Subject", policy.Request{}.Subject},
	} {
		ft := reflect.TypeOf(c.fn)
		if ft.NumOut() != 1 || ft.Out(0).Kind() != reflect.String {
			t.Errorf("%s is %s; the renderers return a string and nothing else", c.name, ft)
		}
	}
}

// ── purity (D5) and the sole-formatter rule ──────────────────────────────────

func TestTheRenderingHasNoIO(t *testing.T) {
	// The criterion names six things: os., "net, "database/sql, time.Now,
	// fmt.Print, io. They are asserted as SYNTAX (the house idiom T2/T3 set — a
	// text grep would hit these very words in this file's comments), so each
	// maps to a check over verdict.go's own AST:
	//
	//	os.            → import "os" banned + any os.X selector banned
	//	"net…           → import "net"… banned + any net.X selector banned
	//	"database/sql" → import banned + any sql.X/db.X selector banned
	//	time.Now       → any selector in clockCalls banned
	//	fmt.Print      → import "fmt" banned outright + any fmt.X selector banned
	//	io.            → import "io"… banned + any io.X selector banned
	//
	// The import check is an ALLOW-list, which is stronger than a ban-list: the
	// renderer may import strings and time and nothing else, so a new way to
	// reach the world has to be added here, in the open, with a reason.
	const file = "verdict.go"
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	allowed := map[string]bool{"strings": true, "time": true}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !allowed[path] {
			t.Errorf("%s imports %q; rendering is string building over the fields it was handed — no I/O, no clock, no environment (D5)", file, path)
		}
	}
	banned := map[string]bool{"os": true, "io": true, "net": true, "fmt": true, "sql": true, "db": true,
		"syscall": true, "math": true, "runtime": true}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true // a field access on a value, not a package call
		}
		if banned[pkg.Name] {
			t.Errorf("%s reaches the world via %s.%s; the renderer is pure (D5)", file, pkg.Name, sel.Sel.Name)
		}
		if pkg.Name == "time" && clockCalls[sel.Sel.Name] {
			t.Errorf("%s reads or waits on the clock via time.%s; an expiry prints the instant the policy carries, never now (D5/D6)",
				file, sel.Sel.Name)
		}
		return true
	})

	// The claim itself has to exist for the checks above to mean anything: the
	// file must actually declare Sentence.
	declared := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if fd, ok := n.(*ast.FuncDecl); ok && fd.Name != nil {
			declared[fd.Name.Name] = true
		}
		return true
	})
	if !declared["Sentence"] {
		t.Fatalf("%s declares no Sentence; every surface is calling something else", file)
	}
}

// sentenceMarkers are the pieces of the sentence that only the formatter has
// any business assembling. A file outside verdict.go that writes one is a
// second formatter being born.
var sentenceMarkers = []string{
	"no allow matched",
	"has no evaluator yet",
	"denied anyway, PL4",
	"default deny (PL5)",
}

// offender reports the first sentence fragment a source assembles itself
// without ever calling the formatter. Exported to no one, testable on purpose:
// a guard nobody can make fail is not a guard (T2's TestTheLockCanFail).
func offender(text string) (string, bool) {
	if strings.Contains(text, "Sentence(") {
		return "", false
	}
	for _, marker := range sentenceMarkers {
		if strings.Contains(text, marker) {
			return marker, true
		}
	}
	return "", false
}

func TestSentenceIsTheOnlyFormatterInTheModule(t *testing.T) {
	// The rule this task exists for: four surfaces print one sentence, so the
	// sentence is assembled in one place. Asserted over every .go file in the
	// module rather than trusted:
	//
	//   - a NON-test file may contain a marker only if it also mentions
	//     Sentence( — i.e. it is forwarding the formatter's output, which is
	//     what T8/T9/T10/T12/T13 must do;
	//   - a _test.go file may contain markers only if it too mentions Sentence(,
	//     so a surface's test cannot pin a hand-copied sentence its production
	//     code then renders differently (verdict_test.go itself is exempt: it is
	//     the pin file).
	//
	// The app (T14) is out of reach here — it is TypeScript, and its own vitest
	// holds the same rule ("verdict printed verbatim").
	root := moduleRoot(t)
	seen := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == "verdict.go" {
			return nil
		}
		seen++
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		isTest := strings.HasSuffix(path, "_test.go")
		if isTest && filepath.Base(path) == "verdict_test.go" {
			return nil
		}
		marker, bad := offender(text)
		if !bad {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		role := "code"
		if isTest {
			role = "a test"
		}
		t.Errorf("%s (%s) assembles %q itself and never calls Sentence(); the sentence is built in verdict.go alone — "+
			"print what Verdict.Sentence() returns (T8/T9/T10/T12/T13 depend on there being one)", rel, role, marker)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if seen < 20 {
		t.Fatalf("only %d .go files were scanned under %s; this guard would pass almost vacuously", seen, root)
	}
}

func TestTheSoleFormatterGuardCanFail(t *testing.T) {
	// A guard that cannot fail guards nothing. Each half of the rule is exercised
	// on synthetic source, and the marker list is checked against the real thing
	// so it cannot quietly rot into four strings nothing contains.
	synthetic := []struct {
		name string
		src  string
		want string // the fragment it should be caught on ("" = must not be caught)
	}{
		{
			"a CLI that builds the refusal itself",
			"package cli\n\nfunc why(v policy.Verdict) string {\n\treturn \"refused: \" + v.Subject + \" · no allow matched\"\n}\n",
			"no allow matched",
		},
		{
			"a socket handler that spells the D10 shape",
			"package protocol\n\nvar refusal = \"tool_access has no evaluator yet\"\n",
			"has no evaluator yet",
		},
		{
			"a CLI that forwards the formatter and quotes the shape in a comment",
			"package cli\n\n// Renders the PL5 shape: refused: <subject> · no allow matched · default deny (PL5)\nfunc why(v policy.Verdict) string { return v.Sentence() }\n",
			"",
		},
	}
	for _, c := range synthetic {
		t.Run(c.name, func(t *testing.T) {
			marker, bad := offender(c.src)
			if c.want == "" {
				if bad {
					t.Errorf("the guard cried wolf on source that forwards Sentence(): it flagged %q", marker)
				}
				return
			}
			if !bad {
				t.Fatalf("the guard missed a hand-built sentence that never calls Sentence(): %s", c.src)
			}
			if marker != c.want {
				t.Errorf("the guard found %q, want the fragment the file actually assembled (%q)", marker, c.want)
			}
		})
	}

	real, err := os.ReadFile("verdict.go")
	if err != nil {
		t.Fatalf("read verdict.go: %v", err)
	}
	for _, marker := range sentenceMarkers {
		if !strings.Contains(string(real), marker) {
			t.Errorf("the marker %q no longer appears in verdict.go; the guard is checking a sentence nobody renders", marker)
		}
	}
}

// moduleRoot walks up from the package directory to the module's go.mod, and
// fails loudly rather than scanning nothing if it cannot find it.
func moduleRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatalf("where am I: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("no go.mod above the package directory; the sole-formatter guard has nothing to scan")
		}
		dir = parent
	}
}
