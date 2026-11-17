package tools

// Test helpers for the execution seam (S59 T5).
//
// Two environments are used across this package's tests, and which one a test
// gets is a fact about what that test asserts:
//
//   - testScrubbedEnv is what a step gets: a BUILT, host-scrubbed environment
//     with the handful of names a command needs to find its binaries. A test
//     that sets a daemon variable and expects the child to see it FAILS against
//     this, which is the property T5 exists to install.
//   - execenv.LegacyInherit() is the named legacy backend, and it is used only
//     by the tests that are about inheritance itself — the S57 golden, which
//     pins what the legacy backend still does, and the inverted leak test, which
//     pins that the default no longer does it.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
)

// testPassthroughNames is the allowlist the tests' scrubbed environment
// carries. It is the same handful a `host-scrubbed` environment gives a shell
// that has to locate `touch` and report a `whoami`: enough to run, not enough
// to see the daemon.
// The two `AEON_TEST_PORT_*` names are the harness's own: the group-kill
// measurement re-executes THIS test binary as a grandchild that binds a port,
// and it finds out which mode to run in from its environment. They are listed
// because `AEON_` is a denied namespace (EV4), so the harness renamed them out
// of it rather than exempting a test — the deny list is not loosened for
// anybody's convenience, including this file's.
var testPassthroughNames = []string{"PATH", "HOME", "USER", "LANG", "SHELL", "TMPDIR",
	"S59_TEST_PORT_HOLDER", "S59_TEST_PORT_READY"}

// testScrubbedEnv builds the environment a tools test spawns with when the test
// is about something other than the environment.
func testScrubbedEnv(tb testing.TB) execenv.ChildEnv {
	tb.Helper()
	env, r := execenv.Build(
		execenv.Spec{Env: execenv.EnvSection{Passthrough: testPassthroughNames}},
		execenv.DaemonEnv{Lookup: os.Getenv, Names: environNames},
		nil,
	)
	if r != nil {
		tb.Fatalf("the test's own scrubbed environment was refused (the deny list grew?): %s", r.Sentence())
	}
	return env
}

// environNames is the daemon-side view of the process environment: names only,
// which is all `Build` may ask for. A passthrough pattern with no name list
// refuses, and that refusal is a test worth having; here the list is real.
func environNames() []string {
	raw := os.Environ()
	out := make([]string, 0, len(raw))
	for _, kv := range raw {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out = append(out, kv[:i])
		}
	}
	return out
}

// envWithSecrets builds a scrubbed environment with one injected secret, for the
// tests that assert a value reaches the child AND leaves the transcript
// filtered — the pair that only makes sense together.
func envWithSecrets(tb testing.TB, name, injectAs, value string) execenv.ChildEnv {
	tb.Helper()
	inj, r := execenv.ResolveRef(
		execenv.RefSpec{Name: name, SourceKind: "daemon-env", Source: injectAs, InjectAs: injectAs},
		execenv.ResolveEnv{
			Lookup:      func(string) string { return value },
			DaemonUID:   os.Getuid(),
			Credentials: execenv.DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN"}},
		})
	if r != nil {
		tb.Fatalf("the test's own secret reference was refused: %s", r.Sentence())
	}
	env, rr := execenv.Build(
		execenv.Spec{Env: execenv.EnvSection{Passthrough: testPassthroughNames}},
		execenv.DaemonEnv{Lookup: os.Getenv, Names: environNames},
		[]execenv.Injection{inj},
	)
	if rr != nil {
		tb.Fatalf("the test's own environment with a secret was refused: %s", rr.Sentence())
	}
	return env
}

// testExecSeam is the `ToolContext.Exec` a tools test installs when the test is
// about the tool and not about the environment: an executor over the scrubbed
// environment, so `bash` gets past its fail-closed refusal without the test
// having to think about environments.
//
// Tests that ARE about the environment build their own `HostExecutor` and pass
// it directly, so the executor under test is the one the test named.
func testExecSeam(tb testing.TB) func(context.Context) (CommandExecutor, error) {
	tb.Helper()
	return func(context.Context) (CommandExecutor, error) {
		return HostExecutor{Env: testScrubbedEnv(tb)}, nil
	}
}

// refusedExecSeam is the seam that answers with a refusal instead of an
// executor — the shape of an unbound environment or a missing container driver
// — for a test that asserts the refusal travels to the model verbatim.
func refusedExecSeam(r *execenv.Refusal) func(context.Context) (CommandExecutor, error) {
	return func(context.Context) (CommandExecutor, error) { return nil, r }
}
