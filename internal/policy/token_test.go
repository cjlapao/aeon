package policy

import (
	"encoding/json"
	"testing"
	"time"
)

// The token tests (S11, G16/QAP-15). These run INSIDE the package and so can
// only assert the MINTING side honestly — what an allow yields, what a deny
// and an ask refuse, and what the zero value is. The FORGING side (what a
// package outside policy cannot do) is proven from token_forge_test, where
// "internal" privileges are structurally impossible; asserting it from here
// would be the wolf checking the fence.

var tokenTestAt = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// tokenCommandRow is one stored CommandAccess row over an exact pattern: the
// same shape evaluate_commandaccess_test builds, sized to one command. The
// spec's state repeats the action (stateAgreesWithAction), and the source is
// operator because PL1 says only an operator may author an allow.
func tokenCommandRow(tb testing.TB, id, pattern, action string) Policy {
	tb.Helper()
	spec, err := json.Marshal(map[string]string{"command": pattern, "state": action})
	if err != nil {
		tb.Fatalf("helper built an unreadable spec: %v", err)
	}
	target, rej := ParseTarget(AllAgents, "")
	if rej != nil {
		tb.Fatalf("helper built an illegal target: %s", rej.Reason)
	}
	act, rej := ParseAction(action)
	if rej != nil {
		tb.Fatalf("helper built an illegal action: %s", rej.Reason)
	}
	return Policy{
		ID:     id,
		Type:   CommandAccess,
		Target: target,
		Action: act,
		Spec:   spec,
		Source: Operator,
	}
}

// tokenAllow evaluates one command over a one-rule allow set through the
// REAL evaluator. Hand-building a Verdict to mint a token is forbidden here
// and everywhere: the token's whole claim is "the engine decided", so every
// token in this suite comes out of Evaluate.
func tokenAllow(tb testing.TB, command string) Verdict {
	tb.Helper()
	ps := []Policy{tokenCommandRow(tb, "allow-exact", command, "allow")}
	return Evaluate(ps, Request{Type: CommandAccess, Agent: "t13-agent", Tool: "bash", Command: command}, tokenTestAt)
}

func TestVerdictTokenMintsOnlyFromAnAllowVerdict(t *testing.T) {
	// The three verdict shapes the engine returns for a command question,
	// and what each may produce.
	askPs := []Policy{tokenCommandRow(t, "ask-exact", "git push*", "ask")}
	asked := Evaluate(askPs, Request{Type: CommandAccess, Agent: "t13-agent", Tool: "bash", Command: "git push origin main"}, tokenTestAt)

	denyPs := []Policy{tokenCommandRow(t, "deny-exact", "git push*", "deny")}
	denied := Evaluate(denyPs, Request{Type: CommandAccess, Agent: "t13-agent", Tool: "bash", Command: "git push origin main"}, tokenTestAt)

	empty := Evaluate(nil, Request{Type: CommandAccess, Agent: "t13-agent", Tool: "bash", Command: "git push origin main"}, tokenTestAt)

	tests := []struct {
		name    string
		v       Verdict
		command string
		wantOK  bool
	}{
		{"allow mints", tokenAllow(t, "echo hi"), "echo hi", true},
		{"deny mints nothing", denied, "git push origin main", false},
		{"ask mints nothing (nobody spawns on a question nobody answered)", asked, "git push origin main", false},
		{"default deny mints nothing", empty, "git push origin main", false},
		{"allow for a command cannot mint for the empty command", tokenAllow(t, "echo hi"), "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tok, ok := tc.v.Token(tc.command)
			if ok != tc.wantOK {
				t.Fatalf("Token ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				if tok.minted || tok.command != "" || tok.nonce != 0 {
					t.Errorf("a non-allow returned a non-zero token: %+v", tok)
				}
				if tok.Allows(tc.command) {
					t.Error("the token it refused to mint still Allows()")
				}
			}
		})
	}
}

func TestAllowTokenIsBoundToItsExactCommand(t *testing.T) {
	// The binding half: minted for one string, inert against every other.
	v := tokenAllow(t, "echo hi")
	tok, ok := v.Token("echo hi")
	if !ok {
		t.Fatal("the allow refused to mint")
	}
	if !tok.Allows("echo hi") {
		t.Error("the token does not authorize the command it was minted for")
	}
	for _, other := range []string{
		"echo hi ",            // trailing space is a different command
		"echo  hi",            // and so is a doubled space
		"echo hi && rm -rf /", // an appended second segment
		"ECHO hi",             // case is part of the string
		"",                    // nothing authorizes the empty command
		"echo",                // the prefix is not the string
	} {
		if tok.Allows(other) {
			t.Errorf("a token for %q authorized %q", "echo hi", other)
		}
	}
}

func TestZeroAllowTokenIsInert(t *testing.T) {
	// The sentinel, not the shape: a hand-laid AllowToken carries the
	// minted bit's ABSENCE, and Allows checks that bit first. This is also
	// the shape a forger OUTSIDE the package is limited to (token_forge_test
	// proves they can produce nothing else); from in here the fields are
	// visible, and even the zero value must answer no to everything.
	var zero AllowToken
	for _, cmd := range []string{"echo hi", "", "git push origin main", "touch marker.txt"} {
		if zero.Allows(cmd) {
			t.Errorf("the zero token authorized %q — the minted sentinel is not what Allows checks", cmd)
		}
	}
	if _, ok := (Verdict{}).Token("echo hi"); ok {
		t.Error("a zero Verdict minted a token; Allowed is not the only door")
	}
}

func TestTokensAreNeverValueEqual(t *testing.T) {
	// The nonce half: two allow verdicts over the SAME command produce
	// tokens that are not `==`. The counter is package-monotonic, so the
	// only way two mints agree on every field is a wraparound nobody will
	// reach; what this pins is that the property exists at all.
	a, ok1 := tokenAllow(t, "echo hi").Token("echo hi")
	b, ok2 := tokenAllow(t, "echo hi").Token("echo hi")
	if !ok1 || !ok2 {
		t.Fatal("the allows refused to mint")
	}
	if a == b {
		t.Error("two mints produced value-equal tokens; the nonce did not discriminate")
	}
	if !a.Allows("echo hi") || !b.Allows("echo hi") {
		t.Error("distinct tokens must still both authorize their command")
	}
}

func TestAskAndDenyVerdictsMintEvenWhenTheyBeatAnAllow(t *testing.T) {
	// PL4 made a token question: an allow EXISTS and matches, and the
	// deciding tier still refuses the key. The verdict's Allowed is the
	// only door, and an overridden allow is not a verdict.
	ps := []Policy{
		tokenCommandRow(t, "allow-echo", "echo*", "allow"),
		tokenCommandRow(t, "deny-echo", "echo hi*", "deny"),
	}
	v := Evaluate(ps, Request{Type: CommandAccess, Agent: "t13-agent", Tool: "bash", Command: "echo hi there"}, tokenTestAt)
	if v.Allowed {
		t.Fatal("the harness: the deny did not beat the allow")
	}
	if tok, ok := v.Token("echo hi there"); ok || tok.Allows("echo hi there") {
		t.Error("a deny that beat an allow minted (or nearly minted) a token")
	}
}
