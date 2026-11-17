package git

// S61 T4 (GI9/GI12): the scaffold plumbing. `init -b`, the one remote
// door, the remote read that lets a caller refuse to retarget, and the
// identity resolved through `git var` — each with its refusal, and the
// standing prohibitions walking over the new code too (the package-wide
// TestNoForcePushAnywhere covers these methods exactly as it covers the
// old ones).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesTheBranchItWasNamed(t *testing.T) {
	c := NewClient(gitTestEnv())
	root := t.TempDir()
	dir := filepath.Join(root, "sub", "fresh") // parents absent — `git init <dir>` creates the leaf
	if err := c.Init(context.Background(), dir, "trunk"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
		t.Fatalf("no repository at %s: %v", dir, err)
	}
	out, err := c.Run(context.Background(), dir, "symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(out) != "trunk" {
		t.Fatalf("branch: %q %v — `init -b` is the whole of GI12's default-branch duty", out, err)
	}
}

// TestInitRefusesUnsafeBranchNames — the floor before git: leading dashes
// (a name that turns into a flag), the range marker, the lock suffix,
// whitespace. The refusal happens before the directory sees a process.
func TestInitRefusesUnsafeBranchNames(t *testing.T) {
	c := NewClient(gitTestEnv())
	for _, bad := range []string{"", "-b", "main feature", "a..b", "refs/heads/x.lock", "/x", "x/", "a\nb"} {
		dir := filepath.Join(t.TempDir(), "never")
		err := c.Init(context.Background(), dir, bad)
		ge, ok := err.(*GitError)
		if err == nil || !ok || ge.Kind != KindRefused {
			t.Fatalf("branch %q: %v — want a refusal", bad, err)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Fatalf("branch %q was refused and the directory was made anyway", bad)
		}
	}
}

func TestAddRemoteAddRemoteURLIsPresenceAsData(t *testing.T) {
	c := NewClient(gitTestEnv())
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := c.Init(ctx, dir, "main"); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "origin.git")
	if _, err := c.Run(ctx, t.TempDir(), "init", "--bare", "-b", "main", bare); err != nil {
		t.Fatal(err)
	}
	if err := c.AddRemote(ctx, dir, "origin", bare); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.RemoteURL(ctx, dir, "origin")
	if err != nil || !ok || got != bare {
		t.Fatalf("get-url: %q %v %v", got, ok, err)
	}
	// Absence is a result: "", false, nil — the answer GI10's
	// never-retarget rule and createRepo's add-or-keep branch both read.
	got, ok, err = c.RemoteURL(ctx, dir, "upstream")
	if err != nil || ok || got != "" {
		t.Fatalf("absent remote: %q %v %v — want (\"\", false, nil)", got, ok, err)
	}
	// git's own duplicate refusal travels; and an url that starts with a
	// dash is refused before git is asked.
	if err := c.AddRemote(ctx, dir, "origin", bare); err == nil {
		t.Fatal("duplicate remote accepted")
	}
	if err := c.AddRemote(ctx, dir, "x", "--upload-pack=evil"); err == nil {
		t.Fatal("option-shaped url accepted")
	}
}

func TestIdentityResolvedReadsWhatGitWouldSay(t *testing.T) {
	c := NewClient(gitTestEnv())
	name, email, ok, err := c.IdentityResolved(context.Background(), filepath.Join(t.TempDir(), "not-created-yet", "deeper"))
	if err != nil || !ok {
		t.Fatalf("env identity: %q %q %v %v", name, email, ok, err)
	}
	if name != "s59-fixture" || email != "s59@aeon.test" {
		t.Fatalf("identity: %q %q — `git var` answers the injection, which is how the real daemon's identity will arrive", name, email)
	}
}

// TestIdentityResolvedUnresolvedIsNotAnError — GI12's hinge: git's
// "unable to auto-detect" (exit 128) is a RESULT (ok=false, err=nil), so
// the caller owes the sentence and git never hears an invented author.
// The exit is SCRIPTED, not host-dependent: whether a machine's fallback
// ident (user@hostname) resolves is the host's business, and a test that
// asserts the host's gitconfig would be a test that moves when someone
// sets their name.
func TestIdentityResolvedUnresolvedIsNotAnError(t *testing.T) {
	c := &Client{Exec: func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "var" {
			return "fatal: unable to auto-detect email address (got 'nobody@nowhere.(none)')",
				&GitError{Kind: KindExit, Detail: "unable to auto-detect email address", Code: 128}
		}
		return "", nil
	}}
	_, _, ok, err := c.IdentityResolved(context.Background(), filepath.Join(t.TempDir(), "x"))
	if err != nil {
		t.Fatalf("unresolved must not be an error: %v", err)
	}
	if ok {
		t.Fatal("git said it could not name anyone and the client resolved an identity anyway")
	}
}

// TestIdentityResolvedRejectsAnUnshapedAnswer — exit 0 and output that is
// not "Name <email> …" is unresolved too: an identity half-read is worse
// than none, because it commits with the wrong author in silence.
func TestIdentityResolvedRejectsAnUnshapedAnswer(t *testing.T) {
	c := &Client{Exec: func(_ context.Context, _ string, _ ...string) (string, error) {
		return "author identity unavailable", nil
	}}
	if _, _, ok, _ := c.IdentityResolved(context.Background(), t.TempDir()); ok {
		t.Fatal("prose parsed as an identity")
	}
}

// TestIdentityResolvedPropagatesARefusal — the no-environment refusal is
// a wiring bug wearing a result's clothes if it comes back as ok=false:
// the caller would then tell a person to configure git when the daemon
// forgot to hand it an environment. It travels as the error it is.
func TestIdentityResolvedPropagatesARefusal(t *testing.T) {
	c := &Client{} // no Exec, no Env — realExec refuses
	_, _, _, err := c.IdentityResolved(context.Background(), t.TempDir())
	ge, ok := err.(*GitError)
	if err == nil || !ok || ge.Kind != KindRefused {
		t.Fatalf("refusal: %v — want the KindRefused GitError, not an unresolved", err)
	}
}

func TestParseIdent(t *testing.T) {
	n, e, ok := parseIdent("Ada Lovelace <ada@example.com> 1730000000 +0000")
	if !ok || n != "Ada Lovelace" || e != "ada@example.com" {
		t.Fatalf("%q %q %v", n, e, ok)
	}
	if _, _, ok := parseIdent("no angle brackets here"); ok {
		t.Fatal("unparseable parsed")
	}
	if _, _, ok := parseIdent("<only@email> 0 +0000"); ok {
		t.Fatal("an identity with no name is not an identity")
	}
}
