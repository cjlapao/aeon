//go:build !windows

package terminal

// env_golden_test.go — S59 T1 (characterization, P2 Phase A).
//
// `terminalEnv` is the ONE deliberate environment inheritance in the
// runtime: it replaces TERM and inherits everything else. S59 EV10 pins
// this behaviour and does not change it — a terminal that loses its
// ambient locale and path is a regression, and this test is the pin that
// says so. When the environment record takes over process spawning (T5),
// the terminal's child gets its environment from the same record, but
// the terminal's ONE documented exception (the TERM replacement) must
// survive the refactor intact.
//
// What the test holds:
//   - every TERM= entry the parent carries is gone,
//   - exactly one TERM=xterm-256color, at the end,
//   - everything else the parent carries survives, including a sentinel.

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestTerminalEnvGolden(t *testing.T) {
	const (
		termSentinel    = "AEON_S59_TERM_KEEP"
		termSentinelVal = "s59-terminal-keep"
	)
	t.Setenv("TERM", "dumb")
	t.Setenv(termSentinel, termSentinelVal)
	parent := os.Environ()

	env := terminalEnv(parent)

	// 1. No TERM entry except the one at the end.
	n := 0
	for i, kv := range env {
		if strings.HasPrefix(kv, "TERM=") {
			n++
			if i != len(env)-1 {
				t.Fatalf("TERM is not last: %q at %d of %d", kv, i, len(env))
			}
			if kv != "TERM=xterm-256color" {
				t.Fatalf("the TERM replacement drifted: %q", kv)
			}
		}
	}
	if n != 1 {
		t.Fatalf("the parent's %d TERM entry became %d", 1, n)
	}

	// 2. Everything that is not a TERM entry is inherited, untouched.
	kept := 0
	for _, kv := range parent {
		if strings.HasPrefix(kv, "TERM=") {
			continue
		}
		kept++
		if !slices.Contains(env, kv) {
			t.Errorf("the inherited variable was dropped: %q", kv)
		}
	}
	// 3. Arithmetic: out = (in minus the TERM entries) + 1.
	parentTerms := 0
	for _, kv := range parent {
		if strings.HasPrefix(kv, "TERM=") {
			parentTerms++
		}
	}
	if len(env) != kept+1 {
		t.Fatalf("the environment size drifted: got %d, want %d (inherited %d + one TERM)",
			len(env), kept+1, kept)
	}
	_ = parentTerms
}
