package daemon

// The step tests' environment fixture (S59 T5).
//
// # Why a step test has to ask for this now
//
// Before S59 a step's `bash` inherited whatever the test process held, so a
// test that ran a command got an environment for free and nobody had to say
// where it came from. That free ride is what EV5 and EV2 removed: a step with
// no binding is REFUSED, and there is no built-in default to fall back to. So
// a test that means to run a command must bind an environment the way a person
// does — through the store's own doors — and say so at its own top line.
//
// `recoveryStore` stays the plain, unbound store on purpose. The unbound state
// is a real state with real behaviour (a refusal naming the command to run), and
// a fixture that quietly bound an environment for everyone would make it
// untestable and would tell a reader that a fresh daemon runs commands. This is
// the sibling a test calls when it is about to spawn something.

import (
	"os"
	"testing"

	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/store"
)

// stepEnvSpec is the minimum a real command needs: the two names every
// non-interactive command looks for, no secrets, nothing set that the test did
// not name.
const stepEnvSpec = `{"env":{"passthrough":["PATH","HOME"],"set":{},"secrets":[]},` +
	`"home":"real","limits":{},"egress":{"mode":"unenforced"},"image":null,"services":[],"preview":null}`

// bindStepEnvironment binds a scrubbed environment at workspace scope, so a
// step whose root is this test's directory has an environment to run in.
func bindStepEnvironment(t *testing.T, s *store.Store) {
	t.Helper()
	actor := store.Actor{Kind: "user", ID: "carlos"}
	if _, _, err := s.CreateEnvironment(actor, store.NewEnvironment{
		Slug: "test-scrubbed", Name: "Test (scrubbed)", Description: "the step tests' environment",
		Backend: "host-scrubbed", Spec: stepEnvSpec,
	}); err != nil {
		t.Fatalf("create the step environment: %v", err)
	}
	if _, r, err := s.BindEnvironment(actor, store.NewBinding{
		ScopeKind: "workspace", EnvironmentID: "test-scrubbed",
	}); r != nil || err != nil {
		t.Fatalf("bind the step environment: r=%v err=%v", r, err)
	}
}

// recoveryStoreBound is `recoveryStore` plus the binding: the fixture for a
// test that is about to run a command.
func recoveryStoreBound(t *testing.T) *store.Store {
	t.Helper()
	s := recoveryStore(t)
	bindStepEnvironment(t, s)
	return s
}

// testGitEnv is the environment a daemon test hands a real-git client: the two
// names every command needs, the commit identity named rather than read from an
// operator's ~/.gitconfig, and a credential question that answers itself as an
// error instead of hanging the unit.
func testGitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_AUTHOR_NAME=s59-fixture", "GIT_AUTHOR_EMAIL=s59@aeon.test",
		"GIT_COMMITTER_NAME=s59-fixture", "GIT_COMMITTER_EMAIL=s59@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C",
	}
}

// testGitClient is a working git client for a daemon test. `&git.Client{}` now
// refuses rather than inheriting the test process (S59, EV10), and a test that
// is asserting somebody else's sentence would read that refusal as the answer.
func testGitClient() *git.Client { return git.NewClient(testGitEnv()) }
