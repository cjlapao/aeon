package policy_test

// The asked sentence — S11's D1's rendering, and the honesty the audit depends
// on. There are now THREE decided sentences (refused / allowed / asked) plus
// the two refusals that name no policy, and the three decided ones must differ
// from each other in the prefix and nothing else: an operator reading
// `aeon policy why`, a policy_event.detail row, or the app must be able to
// tell, from the first word, whether a rule refused them, permitted them, or
// routed the question to a human. A renderer that let "asked" print as
// "refused" would tell a person to go change a rule that already says what
// they want; one that let it print as "allowed" would report a permission
// nobody holds. These literals pin which failure is impossible.
//
// Verdicts are built directly (no store, no evaluator, no clock), the way
// verdict_test.go builds them: a rendering claim depends on the fields alone.
// The rule/dot/later helpers live beside these tests in verdict_test.go.

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// askRule is the corpus's own kind of rule: an import-sourced CommandAccess
// row that routes a hard reset to a human. Import is deliberate — an ask is
// deny-like at the boundary (any source may author one), and the committed
// corpus's seven ask rules ARE import-sourced.
func askRule(tb testing.TB) *policy.Policy {
	tb.Helper()
	return rule(tb, "confirm-hard-reset", policy.CommandAccess, policy.AgentKind, "main",
		policy.Ask, policy.Import, nil)
}

func TestAnAskRendersItsOwnPrefixNamingEverything(t *testing.T) {
	// The whole literal, character for character. The asked sentence shares
	// the decided shape — subject, type, quoted id, target, source, expiry —
	// and differs from the refusal and the permission in the prefix alone.
	v := policy.Verdict{Subject: "agent:main", Type: policy.CommandAccess, Asked: true, By: askRule(t)}
	got := v.Sentence()
	want := `asked: agent:main · command_access "confirm-hard-reset" · target agent:main · source import · no expiry`
	if got != want {
		t.Fatalf("the ask sentence drifted\n got: %s\nwant: %s", got, want)
	}
	if !strings.HasPrefix(got, "asked: ") {
		t.Errorf("an ask does not announce itself as one: %s", got)
	}
	for _, part := range []string{
		`"confirm-hard-reset"`, // the rule that asked, so the audit can find it
		"command_access",       // the type, so the spec can be read
		"target agent:main",
		"source import",
		"no expiry",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("the ask sentence omits %s; an ask must name its rule, type, target, source and expiry (D9/PL8): %s", part, got)
		}
	}
}

func TestTheThreeDecidedSentencesDifferFromEachOther(t *testing.T) {
	// The audit's honesty, asserted as pairwise difference over one shared
	// rule shape: the same policy fields, three verdict flags, three
	// sentences, no two alike. If two of these ever render the same string
	// the surfaces above stop being able to tell a permission from a
	// question, and no reader can act on a decision they cannot name.
	base := rule(t, "shared-shape", policy.ToolAccess, policy.LoopKind, "l1", policy.Ask, policy.Operator, &later)
	asked := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess, Asked: true, By: base}.Sentence()
	refused := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess, Allowed: false, By: base}.Sentence()
	allowed := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess, Allowed: true, By: base}.Sentence()

	if asked == refused || asked == allowed || refused == allowed {
		t.Fatalf("two of the three decided sentences render alike\n asked:   %s\n refused: %s\n allowed: %s",
			asked, refused, allowed)
	}
	// And they differ in the PREFIX only — same segment count, same body —
	// so a reader who understands one understands all three.
	for _, s := range []string{asked, refused, allowed} {
		if strings.Count(s, dot) != strings.Count(refused, dot) {
			t.Errorf("the decided sentences differ in more than the prefix:\n asked:   %s\n refused: %s\n allowed: %s",
				asked, refused, allowed)
		}
	}
	if !strings.HasPrefix(asked, "asked: ") || !strings.HasPrefix(refused, "refused: ") ||
		!strings.HasPrefix(allowed, "allowed: ") {
		t.Errorf("a decided sentence has the wrong prefix:\n asked:   %s\n refused: %s\n allowed: %s",
			asked, refused, allowed)
	}
}

func TestAnAskedVerdictCarriesTheOverriddenAllowParenthetical(t *testing.T) {
	// The engine sets OverriddenAllow on an ask that beat an allow (visible
	// for the same reason it does so on a deny: "but I had an allow!" gets
	// answered by the sentence). The verb follows the tier: an ask asks, it
	// does not deny — "asked anyway, PL4".
	grant := rule(t, "wide-grant", policy.ToolAccess, policy.LoopKind, "l1", policy.Allow, policy.Operator, nil)
	v := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess, Asked: true,
		By: askRule(t), OverriddenAllow: grant}
	got := v.Sentence()
	if !strings.Contains(got, `(an allow exists: "wide-grant" · loop:l1 — asked anyway, PL4)`) {
		t.Errorf("the ask does not name the allow it outranked, in the asked verb:\n got: %s", got)
	}
	if strings.Contains(got, "denied anyway") {
		t.Errorf("an asked verdict rendered the deny verb; the tier must be legible from the sentence: %s", got)
	}
	if !strings.HasPrefix(got, "asked: ") {
		t.Errorf("the parenthetical stole the prefix: %s", got)
	}
	// A deny over the same allow still says the deny verb — the two tiers
	// cannot drift into one wording.
	deny := policy.Verdict{Subject: "loop:l1", Type: policy.ToolAccess,
		By:              rule(t, "ban-tools", policy.ToolAccess, policy.LoopKind, "l1", policy.Deny, policy.Operator, nil),
		OverriddenAllow: grant}
	if s := deny.Sentence(); !strings.Contains(s, "denied anyway, PL4)") || strings.Contains(s, "asked anyway") {
		t.Errorf("the denial's override verb drifted toward the ask's: %s", s)
	}
}

func TestAnAskedVerdictThatNamesNoPolicyFailsClosed(t *testing.T) {
	// The By == nil fail-closed arm covers the ask too: "asked:" for a
	// question that names no rule would be a routing to a human nobody can
	// answer — and the honest sentence is PL5's, the one the renderer already
	// means. The engine never builds this shape (assertAskVerdict pins
	// By != nil); this pins the renderer's direction if a hand-built one ever
	// claims it.
	v := policy.Verdict{Subject: "agent:main", Type: policy.ToolAccess, Asked: true}
	got := v.Sentence()
	want := "refused: agent:main" + dot + "no allow matched" + dot + "default deny (PL5)"
	if got != want {
		t.Errorf("an ask naming no rule did not fail closed to the PL5 line\n got: %s\nwant: %s", got, want)
	}
	if strings.HasPrefix(got, "asked:") || strings.HasPrefix(got, "allowed:") {
		t.Errorf("a ruleless ask presented as a decision: %s", got)
	}
}

func TestAskedOutranksAllowedWhenAHandBuiltVerdictClaimsBoth(t *testing.T) {
	// A hand-built contradiction (the engine never emits it: the fold returns
	// one shape at a time). The renderer's dispatch order asks before
	// allowing, which is the fail-safe direction — rendering a question as a
	// permission is the dangerous mislabel, and it must be the impossible
	// one.
	by := askRule(t)
	v := policy.Verdict{Subject: "agent:main", Type: policy.CommandAccess, Asked: true, Allowed: true, By: by}
	if !strings.HasPrefix(v.Sentence(), "asked: ") {
		t.Errorf("a verdict claiming both rendered as neither-ask nor fail-safe: %s", v.Sentence())
	}
}
