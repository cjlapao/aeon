package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// The policy seam for tests (T13). Everything here mints verdicts and tokens
// through the REAL evaluator — the spec forbids an exported-for-tests mint
// seam, and the ban holds in the test suite too: a helper that fabricated an
// AllowToken would test the helper, not G16. The rules are hand-arranged
// `policy.Policy` values (what Evaluate reads), authored as operator with
// state repeating action exactly as a stored row must.

// commandRowT is one stored CommandAccess row over a pattern.
func commandRowT(tb testing.TB, id, pattern, action string) policy.Policy {
	tb.Helper()
	spec, err := json.Marshal(map[string]string{"command": pattern, "state": action})
	if err != nil {
		tb.Fatalf("helper built an unreadable spec: %v", err)
	}
	target, rej := policy.ParseTarget(policy.AllAgents, "")
	if rej != nil {
		tb.Fatalf("helper built an illegal target: %s", rej.Reason)
	}
	act, rej := policy.ParseAction(action)
	if rej != nil {
		tb.Fatalf("helper built an illegal action: %s", rej.Reason)
	}
	return policy.Policy{
		ID:     id,
		Type:   policy.CommandAccess,
		Target: target,
		Action: act,
		Spec:   spec,
		Source: policy.Operator,
	}
}

// seamOver returns the ToolContext closure shape over a fixed rule set: the
// same call policy.Evaluate makes for a bash block, from the tools' side of
// the seam.
func seamOver(tb testing.TB, ps []policy.Policy) func(string) (policy.Verdict, bool) {
	tb.Helper()
	return func(command string) (policy.Verdict, bool) {
		v := policy.Evaluate(ps, policy.Request{
			Type: policy.CommandAccess, Agent: "tools-test", Tool: "bash", Command: command,
		}, time.Now())
		return v, true
	}
}

// allowAllSeam is the permissive source: a single operator allow over `*`.
// This is the same shape the corpus's own broadest rule takes, and the
// validator's header is explicit that it is legal. It is the seam a
// "control: bash CAN run things" test wants — the binding property (a token
// for one command authorizes no other) is pinned by token_test and the
// forge tests, not diluted here.
func allowAllSeam(tb testing.TB) func(string) (policy.Verdict, bool) {
	tb.Helper()
	return seamOver(tb, []policy.Policy{commandRowT(tb, "test-allow-star", "*", "allow")})
}

// denySeam denies one exact pattern; everything else answers through the
// default deny (which also mints no token — the distinction is the gate's,
// not Spawn's).
func denySeam(tb testing.TB, pattern string) func(string) (policy.Verdict, bool) {
	tb.Helper()
	return seamOver(tb, []policy.Policy{
		commandRowT(tb, "test-allow-star", "*", "allow"),
		commandRowT(tb, "test-deny-exact", pattern, "deny"),
	})
}

// spawnAuthorized runs Spawn with a token minted from the REAL evaluator over
// a one-rule allow set for the test command (the spec's minting rule for this
// suite). Every Spawn call site in the package's tests goes through it — the
// token is as mandatory in test as in production, and a call site that
// forgot it fails to compile.
func spawnAuthorized(tb testing.TB, ctx context.Context, command string, opts SpawnOptions) (RunResult, error) {
	tb.Helper()
	v := policy.Evaluate(
		[]policy.Policy{commandRowT(tb, "spawn-test-allow", "*", "allow")},
		policy.Request{Type: policy.CommandAccess, Agent: "spawn-test", Tool: "bash", Command: command},
		time.Now(),
	)
	token, ok := v.Token(command)
	if !ok {
		tb.Fatalf("the engine refused the harness's own allow for %q: %s", command, v.Sentence())
	}
	// The environment is as mandatory in test as in production: every call site
	// in this package goes through here, so no test can spawn with an inert
	// ChildEnv by forgetting an argument.
	return Spawn(ctx, command, opts, token, testScrubbedEnv(tb))
}
