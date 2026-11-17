package token_forge_test

// This is the unforgeability proof from decision 11 / QAP-15: it lives in an
// external package — the position of an author who must produce their own
// authority — and it shows that no such authority can be produced there.
//
// The two compile-time facts the rule rests on are asserted here, and
// TestForgedCodeDoesNotCompile checks them with the compiler itself, using
// the tag-gated files forge_literal.go and forge_spawn.go (tags nothing ever
// sets), compiled deliberately to fail:
//
//  1. policy.AllowToken keeps every field unexported, so a composite literal
//     outside the policy package cannot carry a minted sentinel, a bound
//     command, or a mint nonce;
//  2. tools.Spawn takes the token as a positional parameter, so no call that
//     omits the verdict compiles — the authority cannot be skipped, only
//     supplied.
//
// What the forge package does NOT do is trust the compiler alone: it also
// runs the forged zero token against the real Spawn and proves the runtime
// refuses it and touches nothing on disk, so even a hypothetical type-system
// escape would not buy execution.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// spawnEnv builds the child environment an authorized spawn needs (S59, EV5):
// a real `execenv.Build`, because a spawn that is supposed to SUCCEED must not
// be handed the inert value — that would let the environment refusal stand in
// for the token refusal and the test would pass while proving nothing.
func spawnEnv(tb testing.TB) execenv.ChildEnv {
	tb.Helper()
	env, r := execenv.Build(
		execenv.Spec{Env: execenv.EnvSection{Passthrough: []string{"PATH", "HOME"}}},
		execenv.DaemonEnv{Lookup: os.Getenv, Names: func() []string { return []string{"PATH", "HOME"} }},
		nil)
	if r != nil {
		tb.Fatalf("the harness's own environment was refused: %s", r.Sentence())
	}
	return env
}

// mint runs the REAL evaluator over a one-rule operator allow and hands back
// the token its allow verdict mints. Nothing in this package invents a
// Verdict or an AllowToken — the only token that exists here is one the
// engine itself produced, which is exactly the point.
func mint(tb testing.TB, command string) policy.AllowToken {
	tb.Helper()
	spec, err := json.Marshal(map[string]string{"command": "*", "state": "allow"})
	if err != nil {
		tb.Fatalf("helper built an unreadable spec: %v", err)
	}
	target, rej := policy.ParseTarget(policy.AllAgents, "")
	if rej != nil {
		tb.Fatalf("helper built an illegal target: %s", rej.Reason)
	}
	act, rej := policy.ParseAction("allow")
	if rej != nil {
		tb.Fatalf("helper built an illegal action: %s", rej.Reason)
	}
	v := policy.Evaluate(
		[]policy.Policy{{
			ID:     "forge-allow-star",
			Type:   policy.CommandAccess,
			Target: target,
			Action: act,
			Spec:   spec,
			Source: policy.Operator,
		}},
		policy.Request{Type: policy.CommandAccess, Agent: "forge", Tool: "bash", Command: command},
		time.Now(),
	)
	token, ok := v.Token(command)
	if !ok {
		tb.Fatalf("the engine refused its own allow for %q: %s", command, v.Sentence())
	}
	return token
}

// TestACompositeLiteralCarriesNoAuthority is the load-bearing test: the one
// AllowToken value a package outside policy can construct is the zero value,
// and the zero value is inert.
func TestACompositeLiteralCarriesNoAuthority(t *testing.T) {
	forged := policy.AllowToken{}
	if forged.Allows("rm -rf /") {
		t.Fatal("a composite literal minted spawn authority")
	}

	root := t.TempDir()
	marker := filepath.Join(root, "spawned.marker")
	command := "touch " + marker

	_, err := tools.Spawn(context.Background(), command, tools.SpawnOptions{Dir: root}, forged,
		execenv.ChildEnv{})
	if err == nil {
		t.Fatal("Spawn accepted a composite-literal token")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("Spawn executed under a composite-literal token")
	}
}

// TestNoValueEqualTokenExistsOutsidePolicy shows the binding cannot be
// imitated by value: the minted token is never equal to anything constructible
// here, because the mint nonce makes every mint distinct and no field of it
// is reachable.
func TestNoValueEqualTokenExistsOutsidePolicy(t *testing.T) {
	minted := mint(t, "true")
	if minted == (policy.AllowToken{}) {
		t.Fatal("a minted token must never equal the composite-literal value")
	}
	if !minted.Allows("true") {
		t.Fatal("the minted token should allow its exact command")
	}
	if minted.Allows("true ") || minted.Allows("TRUE") || minted.Allows(" true") {
		t.Fatal("the minted token accepts a variant of its bound command")
	}
}

// TestSpawningWithoutTheVerdictDoesNotCompileAcrossPackages pins the call
// shape from the consumer side: the token is a positional parameter of
// Spawn, so the only legal four-argument call needs an AllowToken — and the
// only expression producing one is Verdict.Token.
func TestSpawningWithoutTheVerdictDoesNotCompileAcrossPackages(t *testing.T) {
	// This compiles (five arguments: options, minted token, resolved
	// environment) — and it is the ONLY way to write the call;
	// forge_literal.go and forge_spawn.go prove the field-filled literal and
	// the short call do not type-check. S59 added the environment as the fifth
	// positional parameter for the same reason the token is the fourth: a
	// setting with a zero value is a door, a positional demand is not.
	token := mint(t, "true")
	var _ policy.AllowToken = token
	if _, err := tools.Spawn(context.Background(), "true", tools.SpawnOptions{Dir: t.TempDir()}, token,
		spawnEnv(t)); err != nil {
		t.Fatalf("an authorised spawn from outside policy failed: %v", err)
	}
}

// TestForgedCodeDoesNotCompile asks the compiler the question directly. The
// forge-tagged files are excluded from every normal build; here each is
// compiled deliberately, and each build must FAIL with exactly the diagnostic
// the unforgeability rule promises. A passing control (go vet with no forge
// tag) guards against crediting a failure to the wrong cause.
func TestForgedCodeDoesNotCompile(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go toolchain is not on PATH; the compile-time proof cannot run")
	}

	// Control: without any forge tag the package vets clean.
	out, err := run(goBin, "vet", ".")
	if err != nil {
		t.Fatalf("the control build (no forge tag) must vet clean: %v\n%s", err, out)
	}

	for _, c := range []struct {
		name string
		tag  string
		want string
	}{
		// The forged literal: minted / command / nonce are unexported.
		{"a hand-written token", "forge_literal", "cannot refer to unexported field"},
		// The bypass: Spawn's fourth parameter is the token.
		{"a spawn without a verdict", "forge_spawn", "not enough arguments in call to"},
	} {
		out, err := run(goBin, "vet", "-tags", c.tag, ".")
		if err == nil {
			t.Fatalf("%s compiled; the authority is forgeable\n%s", c.name, out)
		}
		if !strings.Contains(string(out), c.want) {
			t.Fatalf("%s was rejected for the wrong reason; want %q in:\n%s", c.name, c.want, out)
		}
	}
}

func run(goBin string, args ...string) (string, error) {
	tt := exec.Command(goBin, args...)
	out, err := tt.CombinedOutput()
	return string(out), err
}
