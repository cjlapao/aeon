package workspace

// git_env_golden_test.go — S59 T1 (characterization, P2 Phase A).
//
// `gitEnv()` is the one environment builder in the codebase that already
// does what S59's environment records generalise: it refuses to pass the
// parent's GIT_* state to a child and it names the hardening it adds.
// This test pins, byte for byte:
//
//   - every GIT_* variable the parent carries is stripped (the sentinel
//     proves a real value, not an empty one, goes away),
//   - the exact hardening block that is added, all of it, in name and
//     value,
//   - everything that is NOT a GIT_* variable is inherited untouched
//     (the sentinel that survives is a different sentinel),
//   - the arithmetic: out = (in minus the GIT_* variables) plus the
//     hardening block — no variable is invented, none is dropped twice.
//
// T2 adds the environment record store; T5 turns the host-scrubbed
// default into an allowlist over this same idea. Neither may change what
// this function does today; if either does, this test says so.

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
)

// The hardening block gitEnv appends, in order, with exact values.
var goldenHardening = []string{
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_ATTR_NOSYSTEM=1",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_PAGER=cat",
	"GIT_LITERAL_PATHSPECS=1",
	"LC_ALL=C",
}

// gitAllowlist is execenv's default passthrough, restated here ON PURPOSE:
// this golden is the app's own pin on what its git child may contain, and
// reading the list from execenv would let a change to the list silently change
// the golden. TestGitAllowlistMatchesExecenv says which one moved.
var gitAllowlist = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_*", "TZ", "TMPDIR"}

func TestGitAllowlistMatchesExecenv(t *testing.T) {
	got := execenv.DefaultPassthrough()
	if len(got) != len(gitAllowlist) {
		t.Fatalf("the app's pin and execenv's list differ in size: %d vs %d — one of the two moved",
			len(got), len(gitAllowlist))
	}
	for i := range got {
		if got[i] != gitAllowlist[i] {
			t.Errorf("position %d: execenv says %q, this golden pins %q", i, got[i], gitAllowlist[i])
		}
	}
}

func TestWorkspaceGitEnvGolden(t *testing.T) {
	// The variables this test owns. Names chosen so they cannot collide
	// with anything a real daemon environment carries.
	const (
		gitSentinel = "GIT_S59_GOLD_DIR"
		gitVal      = "/tmp/s59-gold-ambient"
		keepName    = "AEON_S59_GOLD_KEEP"
		keepVal     = "s59-keep-me"
		otherName   = "PATH" // present on any sane machine; inherited, not invented
	)
	t.Setenv(gitSentinel, gitVal)
	t.Setenv(keepName, keepVal)
	parent := os.Environ()

	env := gitEnv()

	// 1. The GIT_* state is stripped — the sentinel, with its real value,
	// is gone from the child's view.
	for _, kv := range env {
		name := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(name, "GIT_") && name != "GIT_S59_GOLD_DIR" {
			// only the hardening block may reintroduce GIT_ names; the
			// test's own sentinel must not be among them
		}
		if kv == gitSentinel+"="+gitVal {
			t.Fatalf("the ambient GIT_* variable survived into the child environment: %q", kv)
		}
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_S59_GOLD_DIR=") {
			t.Fatalf("the ambient GIT_* sentinel is in the child environment: %q", kv)
		}
	}

	// 2. The hardening block, all of it, exactly, in order.
	for _, want := range goldenHardening {
		if !slices.Contains(env, want) {
			t.Errorf("the hardening block is missing %q", want)
		}
	}
	pos := -1
	for _, want := range goldenHardening {
		i := slices.Index(env, want)
		if i < 0 {
			continue
		}
		if pos >= 0 && i < pos {
			t.Errorf("the hardening block is out of order: %q at %d after %q at %d", want, i, goldenHardening[len(goldenHardening)-1], pos)
		}
		pos = i
	}
	// No GIT_* name outside the hardening block may appear.
	for _, kv := range env {
		name := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(name, "GIT_") && !slices.Contains(goldenHardening, kv) {
			t.Errorf("an unexpected GIT_* variable reached the child: %q", kv)
		}
	}

	// 3. # Amended by S59 T5 (A3) — the flip, and only the flip.
	//
	// This section used to read "the non-GIT_* parent is inherited untouched"
	// and asserted that every parent variable except GIT_* and LC_ALL crossed
	// into the child. That property is what EV10 exists to remove: a read of
	// `git log` for the app has no business seeing the daemon's provider keys,
	// and it used to. The assertions below are the same arithmetic with the
	// allowlist in place of the parent — the strip, the hardening block and
	// its ordering above are UNCHANGED, and the sentinel that used to be
	// required to survive is now required to vanish.
	if slices.Contains(env, keepName+"="+keepVal) {
		t.Fatalf("the child inherited a variable nobody named: %s — EV10 is open", keepName)
	}
	for _, kv := range env {
		name := strings.SplitN(kv, "=", 2)[0]
		if !slices.Contains(gitAllowlist, name) && !slices.Contains(goldenHardening, kv) {
			t.Errorf("a variable outside the allowlist reached the UI's git child: %q", kv)
		}
	}
	for _, name := range []string{"PATH", "HOME"} {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			if !slices.Contains(env, name+"="+v) {
				t.Errorf("the allowlisted %s was dropped or altered: the app cannot run git at all without it", name)
			}
		}
	}
	// SSH_AUTH_SOCK is the daemon's git only (D9): an app read of a log must
	// not be able to reach the person's ssh agent.
	if v, ok := os.LookupEnv("SSH_AUTH_SOCK"); ok && v != "" {
		for _, kv := range env {
			if strings.HasPrefix(kv, "SSH_AUTH_SOCK=") {
				t.Errorf("SSH_AUTH_SOCK crossed into a UI git child: %q", kv)
			}
		}
	}

	// 4. The arithmetic: exactly (allowlisted names the parent actually has)
	// plus the hardening block. No invented variables, no duplicates.
	kept := 0
	for _, kv := range parent {
		name := strings.SplitN(kv, "=", 2)[0]
		if !slices.Contains(gitAllowlist, name) || strings.HasPrefix(name, "GIT_") || name == "LC_ALL" {
			continue
		}
		if strings.HasSuffix(name, "*") {
			continue
		}
		if !slices.Contains(env, kv) {
			t.Errorf("the allowlisted parent entry %q did not reach the child", kv)
			continue
		}
		kept++
	}
	if len(env) != kept+len(goldenHardening) {
		t.Fatalf("the environment size drifted: got %d, want %d (allowlisted %d + hardening %d)",
			len(env), kept+len(goldenHardening), kept, len(goldenHardening))
	}
	seen := map[string]int{}
	for _, kv := range env {
		seen[strings.SplitN(kv, "=", 2)[0]]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("the variable %s appears %d times — the strip left a duplicate", name, n)
		}
	}
	_ = otherName
}
