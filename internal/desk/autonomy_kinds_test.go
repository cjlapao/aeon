package desk

import (
	"context"
	"strings"
	"testing"
)

// S56 T8 — the two restrictive autonomy kinds' desk half, unit-tested here;
// the store halves (the seam) are tested where the store is reachable
// (cmd/aeon's commit tests) and the "no grant kind" property is asserted on
// every surface that can see it (the store's CHECK test, T7's line-offense
// guard, and the Kinds pin below).

// TestAutonomyKindsAreTheOnlyAutonomyKinds pins the closed list: exactly the
// two restrictive kinds appear, and no grant kind — the kind an agent would
// need to WIDEN authority — is not one the desk knows, which is the AL11
// lock at the registry level (the store's CHECK is the lock at the table
// level, T7's guard at the source level).
func TestAutonomyKindsAreTheOnlyAutonomyKinds(t *testing.T) {
	got := map[Kind]bool{}
	for _, k := range Kinds() {
		got[k] = true
		if strings.HasPrefix(string(k), "autonomy_") && k != KindAutonomyRevoke && k != KindAutonomyDeny {
			t.Fatalf("kind %q is an autonomy kind that is not one of the two restrictive kinds", k)
		}
	}
	// AL11 at the registry level: the kind an agent would need to WIDEN
	// authority (a grant) is not in the closed list at all.
	if got[Kind("autonomy_grant")] {
		t.Fatal("autonomy_grant is in the desk's closed list — no proposal shape may create an allow")
	}
	for _, want := range []Kind{KindAutonomyRevoke, KindAutonomyDeny} {
		if !got[want] {
			t.Fatalf("kind %s is missing from the closed list", want)
		}
	}
}

// TestValidateAutonomyRevokeIsAShapePin covers behaviour 4's pure half: the
// strict decode refuses a smuggled state (with the sentence the create door
// gives, byte for byte), the id and the reason rule refuse by name, and a
// well-shaped payload passes.
func TestValidateAutonomyRevokeIsAShapePin(t *testing.T) {
	ctx := context.Background()
	if err := ValidateAutonomyRevoke(ctx, []byte(`{"id":"ag_1","reason":"the review lane tightened"}`)); err != nil {
		t.Fatalf("a well-shaped revoke payload passes: %v", err)
	}
	// The smuggle: a payload with state:"allow" is a parse refusal — the
	// shape has no such field, and a decode that accepted it would be a
	// grant in disguise.
	err := ValidateAutonomyRevoke(ctx, []byte(`{"id":"ag_1","reason":"x","state":"allow"}`))
	if err == nil || !strings.Contains(err.Error(), `autonomy_revoke payload is {"id": "<grant id>", "reason": "<why>"} and nothing else`) {
		t.Fatalf("state:allow smuggle: %v — the strict decode must refuse with the shape's sentence", err)
	}
	if err := ValidateAutonomyRevoke(ctx, []byte(`{"id":"  ","reason":"x"}`)); err == nil {
		t.Fatal("a blank grant id must be refused")
	}
	if err := ValidateAutonomyRevoke(ctx, []byte(`{"id":"ag_1","reason":"   "}`)); err == nil {
		t.Fatal("a blank reason must be refused (the 1-to-512 rule)")
	}
	long := make([]byte, 0, 560)
	long = append(long, `{"id":"ag_1","reason":"`...)
	long = append(long, strings.Repeat("a", 550)...)
	long = append(long, `"}`...)
	err = ValidateAutonomyRevoke(ctx, long)
	if err == nil || !strings.Contains(err.Error(), "the cap is 512") {
		t.Fatalf("an over-cap reason: %v — the refusal must name the cap", err)
	}
}

// TestValidateAutonomyDenyIsAShapePin covers the deny's pure half: the
// two-shape subject vocabulary (action by non-empty name, the one class
// named exactly "outward"), the node rule, the reason rule, and the same
// smuggle refusal as the revoke.
func TestValidateAutonomyDenyIsAShapePin(t *testing.T) {
	ctx := context.Background()
	if err := ValidateAutonomyDeny(ctx, []byte(`{"nodeId":"root-1","subject":{"kind":"action","name":"forge.merge"},"reason":"it bit us once"}`)); err != nil {
		t.Fatalf("a well-shaped action-veto payload passes: %v", err)
	}
	if err := ValidateAutonomyDeny(ctx, []byte(`{"nodeId":"root-1","subject":{"kind":"class","name":"outward"},"reason":"outward is a person's call"}`)); err != nil {
		t.Fatalf("the class veto named outward passes: %v", err)
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"smuggled state", `{"nodeId":"r","subject":{"kind":"action","name":"forge.merge"},"reason":"x","state":"allow"}`, `autonomy_deny payload is`},
		{"blank node", `{"nodeId":" ","subject":{"kind":"action","name":"forge.merge"},"reason":"x"}`, "needs the node's id"},
		{"empty action name", `{"nodeId":"r","subject":{"kind":"action","name":"  "},"reason":"x"}`, "needs the action's name"},
		{"wrong class name", `{"nodeId":"r","subject":{"kind":"class","name":"irreversible"},"reason":"x"}`, `the one class is named "outward"`},
		{"unknown subject kind", `{"nodeId":"r","subject":{"kind":"grant","name":"x"},"reason":"x"}`, `a subject's kind is "action" or "class"`},
		{"blank reason", `{"nodeId":"r","subject":{"kind":"action","name":"forge.merge"},"reason":""}`, "needs a reason"},
	}
	for _, tc := range cases {
		err := ValidateAutonomyDeny(ctx, []byte(tc.in))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v — want a refusal naming %q", tc.name, err, tc.want)
		}
	}
}

// TestProposeReasonSentenceIsOneDefinition pins the shared bytes: both the
// blank and the over-cap refusals, and the pass (empty string) case — the
// daemon's policy arms delegate to these exact bytes (2026-11-01, S56 T8).
func TestProposeReasonSentenceIsOneDefinition(t *testing.T) {
	if s := ProposeReasonSentence("  ", "policy_propose_add"); s == "" {
		t.Fatal("a blank reason must produce the sentence")
	}
	if s := ProposeReasonSentence("fine", "policy_propose_add"); s != "" {
		t.Fatalf("a valid reason passes, got %q", s)
	}
	s := ProposeReasonSentence(strings.Repeat("b", 513), "policy_propose_add")
	if !strings.Contains(s, "the cap is 512") || !strings.Contains(s, "policy_propose_add") {
		t.Fatalf("over-cap sentence: %q — cap and tool must be named", s)
	}
}

// TestDeciderRoundTripsAndFailsClosed pins the commit identity: set, read
// back equal; absent, ok is false (an Apply half that needed it refuses
// rather than guessing).
func TestDeciderRoundTripsAndFailsClosed(t *testing.T) {
	d := Decider{Kind: "user", ID: "cjlapao", AuthorKind: "agent", AuthorID: "implementer"}
	ctx := WithDecider(context.Background(), d)
	got, ok := DeciderFrom(ctx)
	if !ok || got != d {
		t.Fatalf("round trip: %+v ok=%v, want %+v", got, ok, d)
	}
	if _, ok := DeciderFrom(context.Background()); ok {
		t.Fatal("a context without a decider must answer ok=false (fail closed)")
	}
}
