package tools

// gateenv_test.go — the allowlist (S57 VG5). The scrub is closed by
// omission: a name not on the list is gone, including every secret shape,
// and the canary proves the teeth by showing the SAME assertions finding
// the planted secrets in the environment the un-scrubbed child would
// inherit.

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// plantSecrets puts the shapes a daemon is likely to carry into this
// process's environment (what a child with the inherited environment would
// see) and returns their names.
func plantSecrets(t *testing.T) []string {
	t.Helper()
	plants := []struct{ name, value string }{
		{"AEON_FORGE_TOKEN", "aeon-forge-canary-01"},
		{"GITHUB_TOKEN", "ghp_canaryCanaryCanary0000000001"},
		{"GH_TOKEN", "ghp_canaryCanaryCanary0000000002"},
		{"ANTHROPIC_API_KEY", "sk-canarycanarycanary0000000001"},
		{"FOO_SECRET", "a-b-c-canary-secret-value"},
		{"AEON_HTTP_KEY", "hex-canary-key-value-0000000000000001"},
	}
	for _, p := range plants {
		t.Setenv(p.name, p.value)
	}
	var names []string
	for _, p := range plants {
		names = append(names, p.name)
	}
	return names
}

// envNames is the set of names present in an environment.
func envNames(env []string) map[string]bool {
	out := make(map[string]bool, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = true
				break
			}
		}
	}
	return out
}

// gateEnvAllowed reports whether the name is on the allowlist the test
// checks against — the fixed names plus the LC_ prefix, plus CI.
func gateEnvAllowed(name string) bool {
	if name == "CI" {
		return true
	}
	if strings.HasPrefix(name, "LC_") {
		return true
	}
	for _, n := range GateEnvNames() {
		if n == name {
			return true
		}
	}
	return false
}

// assertNoSecrets is the canary's assertion: none of the planted names may
// appear, and EVERY name that does appear must be allowed.
func assertNoSecrets(t *testing.T, got []string, planted []string) {
	t.Helper()
	names := envNames(got)
	for _, p := range planted {
		if names[p] {
			t.Errorf("the secret %s reached the gate's environment", p)
		}
	}
	for n := range names {
		if !gateEnvAllowed(n) {
			t.Errorf("the name %s reached the gate's environment and is not on the allowlist", n)
		}
	}
}

// TestGateEnvIsAnAllowlist — the keep set, the LC_ prefix, CI=1, and the
// full check that every name that survives is allowed.
func TestGateEnvIsAnAllowlist(t *testing.T) {
	planted := plantSecrets(t)
	// The keep set, deterministically: the ambient PATH survives (T1's
	// golden already depends on it being set), the rest are planted.
	t.Setenv("HOME", "/home/gateenv-test")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("TZ", "UTC")
	t.Setenv("CI", "0") // the constant must override a parent CI

	got := GateEnv(os.Environ())
	names := envNames(got)

	for _, want := range []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "CI"} {
		if !names[want] {
			t.Errorf("the allowlisted name %s did not survive the scrub", want)
		}
	}
	// CI is the constant 1, whatever the parent carried.
	for _, kv := range got {
		if strings.HasPrefix(kv, "CI=") && kv != "CI=1" {
			t.Errorf("CI is %q; the gate's constant is CI=1", kv)
		}
	}
	assertNoSecrets(t, got, planted)
}

// TestGateEnvScrubHasTeeth — the same canary assertions run against the
// parent environment (what a child with Env: nil, i.e. inheritance, would
// see) must FIND the planted secrets: the scrub is doing work, and a GateEnv
// that scrubbed nothing would fail TestGateEnvIsAnAllowlist above.
func TestGateEnvScrubHasTeeth(t *testing.T) {
	planted := plantSecrets(t)
	parent := os.Environ()
	if err := assertNoSecretsFind(t, parent, planted); err != nil {
		t.Fatalf("the canary has no teeth — the planted secrets are not in the parent environment: %v", err)
	}
}

// assertNoSecretsFind is the INVERTED canary: it FAILS when no planted
// secret is found, proving the plant is effective in the environment an
// un-scrubbed child would inherit.
func assertNoSecretsFind(t *testing.T, env []string, planted []string) error {
	t.Helper()
	names := envNames(env)
	found := 0
	for _, p := range planted {
		if names[p] {
			found++
		}
	}
	if found != len(planted) {
		return fmt.Errorf("only %d of %d planted secrets reached the parent environment", found, len(planted))
	}
	return nil
}
