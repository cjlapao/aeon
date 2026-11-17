package daemon

// # The git doors end to end (S31 T6, D7)
//
// The package-level tests in `internal/workspace` prove the parse; the
// protocol tests prove the door. This file proves the WIRE: the daemon the
// operator actually boots answers `workspace.git.status` and
// `workspace.git.diff` about a TEMPORARY repository it built itself — never
// a real one (§5) — and every refusal in between keeps its name and its
// path all the way to the socket.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// gitOrSkip: the three-state machinery cannot be tested without git; an
// absent binary is a missing test dependency here, and saying so is the
// honest answer (the FEATURE's answer for a missing git is its own package's
// test's subject).
func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed on this box — the doors' answers cannot be exercised")
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=T6", "GIT_AUTHOR_EMAIL=t6@aeon.test",
		"GIT_COMMITTER_NAME=T6", "GIT_COMMITTER_EMAIL=t6@aeon.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// makeRepo builds a COMMITTED repository in a fresh temporary directory and
// returns the directory. The daemon reads it; nobody writes to it from the
// product side.
func makeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	gitOrSkip(t)
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(files) > 0 {
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", "seed")
	}
	return dir
}

func registerComponent(t *testing.T, s *store.Store, slug, root string) {
	t.Helper()
	if _, err := s.CreateComponent(store.NewComponent{Name: slug, Slug: slug, Root: root}); err != nil {
		t.Fatal(err)
	}
}

func gitFixture(t *testing.T) *streamFixture {
	t.Helper()
	f := newStreamFixture(t, nil)
	initConn(t, f.d, context.Background(), "c1", "reader")
	return f
}

// dispatchGitErr is dispatchJSON's sibling for the cases that MUST fail —
// dispatchJSON fatals on any error, which is right for happy paths and
// wrong for refusals under test.
func dispatchGitErr(t *testing.T, f *streamFixture, method protocol.MethodName, params any) error {
	t.Helper()
	raw, _ := json.Marshal(params)
	_, err := f.d.Table.Dispatch(context.Background(), "c1", method, raw)
	return err
}

func TestGitStatusAnswersTheWire(t *testing.T) {
	f := gitFixture(t)
	repo := makeRepo(t, map[string]string{"tracked.txt": "one\ntwo\n"})
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\nTWO\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "brand-new.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registerComponent(t, f.s, "gitread", repo)

	got := dispatchJSON(t, f, "c1", protocol.MethodWorkspaceGitStatus,
		protocol.WorkspaceGitStatusParams{ComponentRoot: repo})
	if got["state"] != "ok" {
		t.Fatalf("state: %v", got)
	}
	if got["branch"] != "main" {
		t.Fatalf("branch: %v", got["branch"])
	}
	if got["detached"] != false {
		t.Fatalf("detached should be present and false: %v", got)
	}
	// P31 as data: no upstream was configured, so ahead/behind are ABSENT —
	// not zero. A zero would claim a measured relationship that does not
	// exist.
	if _, has := got["ahead"]; has {
		t.Fatalf("ahead invented for an upstream-less branch: %v", got)
	}
	if _, has := got["upstream"]; has {
		t.Fatalf("upstream invented: %v", got)
	}
	changed, _ := got["changed"].([]any)
	if len(changed) != 2 {
		t.Fatalf("changed paths: %v", got["changed"])
	}
	byPath := map[string]map[string]any{}
	for _, c := range changed {
		m := c.(map[string]any)
		byPath[m["path"].(string)] = m
	}
	// An UNSTAGED modification: the index is unchanged (`.`), the worktree
	// carries the M — the XY codes verbatim, which is the point of riding
	// porcelain v2 rather than inventing a vocabulary.
	if byPath["tracked.txt"]["index"] != "." || byPath["tracked.txt"]["worktree"] != "M" {
		t.Errorf("the modified file's XY codes are wrong: %v", byPath["tracked.txt"])
	}
	untracked := byPath["brand-new.txt"]
	if untracked == nil || untracked["index"] != "?" || untracked["worktree"] != "U" {
		t.Errorf("the untracked file must read ?/U (the fixture pins it): %v", got["changed"])
	}
}

func TestGitStatusNotARepoIsAnAnswerOnTheWire(t *testing.T) {
	f := gitFixture(t)
	plain := t.TempDir()
	registerComponent(t, f.s, "plain", plain)
	got := dispatchJSON(t, f, "c1", protocol.MethodWorkspaceGitStatus,
		protocol.WorkspaceGitStatusParams{ComponentRoot: plain})
	if got["state"] != "not-a-repo" {
		t.Fatalf("a plain directory answered %v — never a reassuring empty list", got)
	}
	if _, has := got["changed"]; has {
		t.Fatalf("not-a-repo carried a changed list anyway: %v", got)
	}
}

func TestGitStatusRefusesAnUnregisteredRootNamingThePath(t *testing.T) {
	f := gitFixture(t)
	secret := makeRepo(t, map[string]string{"secrets.txt": "a\n"}) // a REAL repo — and still refused
	err := dispatchGitErr(t, f, protocol.MethodWorkspaceGitStatus,
		protocol.WorkspaceGitStatusParams{ComponentRoot: secret})
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != protocol.ErrComponentRootUnknown.Code {
		t.Fatalf("got %v, want ErrComponentRootUnknown", err)
	}
	if !strings.Contains(e.Data.What, secret) {
		t.Fatalf("the refusal did not name the path (I4): %s", e.Data.What)
	}
}

func TestGitDiffAnswersAndRefusesOnTheWire(t *testing.T) {
	f := gitFixture(t)
	repo := makeRepo(t, map[string]string{"a.md": "one\ntwo\n", "b dir/c.md": "x\n"})
	if err := os.WriteFile(filepath.Join(repo, "a.md"), []byte("one\nTWO CHANGED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registerComponent(t, f.s, "diffread", repo)

	got := dispatchJSON(t, f, "c1", protocol.MethodWorkspaceGitDiff,
		protocol.WorkspaceGitDiffParams{ComponentRoot: repo, Path: "a.md"})
	if got["state"] != "ok" {
		t.Fatalf("state: %v", got)
	}
	body, _ := got["diff"].(string)
	if !strings.Contains(body, "-two") || !strings.Contains(body, "+TWO CHANGED") {
		t.Fatalf("the unified diff is not the change: %q", body)
	}
	// The path WITH SPACES resolves through the same door.
	got = dispatchJSON(t, f, "c1", protocol.MethodWorkspaceGitDiff,
		protocol.WorkspaceGitDiffParams{ComponentRoot: repo, Path: "b dir/c.md"})
	if got["state"] != "ok" {
		t.Fatalf("a spaced path was not resolved: %v", got)
	}

	err := dispatchGitErr(t, f, protocol.MethodWorkspaceGitDiff,
		protocol.WorkspaceGitDiffParams{ComponentRoot: repo, Path: "../outside"})
	var bp protocol.ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("escaping the root: got %v, want bad params", err)
	}
	if !strings.Contains(err.Error(), "outside the root") && !strings.Contains(err.Error(), "refused") {
		t.Fatalf("the escape refusal did not name the reason (I4): %v", err)
	}
}
