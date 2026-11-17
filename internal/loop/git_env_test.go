package loop

// One real-git client for the loop tests (S59 T5, EV10).
//
// A `git` client built without an environment now REFUSES rather than inheriting
// the daemon's (or, here, the test process's) — `git.NewClient` names the
// missing thing and nothing runs. The loop tests drive real repositories, so
// they hand their client the environment the daemon's own git calls get: the
// identity is named rather than read from an operator's ~/.gitconfig, and
// `GIT_TERMINAL_PROMPT=0` means a credential question answers itself as an error
// instead of hanging a unit.

import (
	"os"

	"github.com/cjlapao/aeon/internal/git"
)

func realGitClient() *git.Client { return git.NewClient(realGitEnv()) }

func realGitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_AUTHOR_NAME=s59-fixture", "GIT_AUTHOR_EMAIL=s59@aeon.test",
		"GIT_COMMITTER_NAME=s59-fixture", "GIT_COMMITTER_EMAIL=s59@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C",
	}
}
