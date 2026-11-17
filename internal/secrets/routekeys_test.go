package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Route credentials on disk. C17: the environment, or a dedicated file path —
// never a config file, never argv, never a log.

// withStateDir points the resolver at a temp directory.
//
// `t.Setenv` restores it, so this cannot leak into another test — and it is the
// one place in this package's tests that touches the environment, because
// `paths.StoreDir` reads `XDG_STATE_HOME` and there is no injectable form of it
// on this path.
func withStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("AEON_STATE_DIR", "")
	return filepath.Join(dir, "aeon")
}

func TestARouteKeyRoundTrips(t *testing.T) {
	withStateDir(t)
	const key = "sk-ant-api03-round-trip-7f3a"

	if err := SaveRouteKey("anthropic", key); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRouteKey("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if got != key {
		t.Errorf("the key came back as %q, want %q", got, key)
	}
}

func TestARouteKeyFileIs0600InA0700Directory(t *testing.T) {
	// P26 — access control is the filesystem. The mode is set EXPLICITLY
	// rather than left to the umask: a umask of 022 would make this
	// world-readable and nothing would say so.
	state := withStateDir(t)
	if err := SaveRouteKey("anthropic", "sk-ant-api03-perms"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(state, "credentials", "anthropic.key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the credential file is %04o, want 0600", mode)
	}

	dir, err := os.Stat(filepath.Join(state, "credentials"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dir.Mode().Perm(); mode != 0o700 {
		t.Errorf("the credentials directory is %04o, want 0700", mode)
	}
}

func TestAMissingRouteKeyIsNotAnError(t *testing.T) {
	// A route with no credential is a normal state: it is what `hasKey: false`
	// means in the app, and what a route configured before its key looks like.
	// An error here would make the daemon refuse to start over it.
	withStateDir(t)
	got, err := ReadRouteKey("never-configured")
	if err != nil {
		t.Fatalf("reading a missing key failed: %v", err)
	}
	if got != "" {
		t.Errorf("a missing key read back as %q", got)
	}
}

func TestAnEmptyKeyIsNotAWrite(t *testing.T) {
	// "Keep the existing credential" is what an empty key means everywhere
	// else in this product (S3's `apiKey` semantics, AGENTS §4c). Writing an
	// empty file would turn that into "unset it" — and the next call would use
	// a blank credential and get a 401 the person could not explain.
	state := withStateDir(t)
	if err := SaveRouteKey("anthropic", "sk-ant-api03-original"); err != nil {
		t.Fatal(err)
	}
	for _, empty := range []string{"", "   ", "\n"} {
		if err := SaveRouteKey("anthropic", empty); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadRouteKey("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-ant-api03-original" {
		t.Errorf("an empty key overwrote the existing one: %q", got)
	}
	// And an empty key for a route that has none writes no file at all, rather
	// than an empty one that would read back as a blank credential.
	if err := SaveRouteKey("never-had-one", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "credentials", "never-had-one.key")); !os.IsNotExist(err) {
		t.Error("an empty key created a file")
	}
}

func TestDeletingAKeyReportsWhetherOneExisted(t *testing.T) {
	withStateDir(t)
	if err := SaveRouteKey("openai", "sk-proj-delete-me"); err != nil {
		t.Fatal(err)
	}
	existed, err := DeleteRouteKey("openai")
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Error("deleting an existing key reported that nothing existed")
	}
	if existed, err = DeleteRouteKey("openai"); err != nil || existed {
		t.Errorf("deleting a missing key returned (%v, %v)", existed, err)
	}
	// And it is really gone, not emptied.
	got, err := ReadRouteKey("openai")
	if err != nil || got != "" {
		t.Errorf("after deletion the key reads %q (err %v)", got, err)
	}
}

func TestARouteIDThatWouldEscapeTheDirectoryIsRefused(t *testing.T) {
	// A route id is typed by a person in a form and travels to a path join.
	// **Refused rather than sanitised**: a cleaned id writes a credential
	// somewhere unexpected and looks like it worked, which is the same failure
	// the store's document paths exist to prevent.
	withStateDir(t)
	for _, id := range []string{
		"../../../etc/passwd",
		"..",
		".hidden",
		"/a",
		"a/",
		"a//b",
		"a/.hidden",
		"a/../b",
		`a\b`,
		"with space",
		"semi;colon",
		"nul\x00byte",
		"",
	} {
		t.Run(id, func(t *testing.T) {
			if _, err := RouteKeyPath(id); err == nil {
				t.Fatalf("the id %q was accepted as a file name", id)
			}
			// And the write path refuses it too, rather than only the resolver.
			if err := SaveRouteKey(id, "sk-should-not-be-written"); err == nil {
				t.Errorf("a key was written for the id %q", id)
			}
		})
	}

	// The negative control: a legitimate id must work, or this test would pass
	// against a function that refuses everything.
	for _, id := range []string{"anthropic", "openai", "my-route_2", "gpt.5", "hexgrad/Kokoro-82M"} {
		if _, err := RouteKeyPath(id); err != nil {
			t.Errorf("the legitimate id %q was refused: %v", id, err)
		}
	}
}

func TestAKeyIsNeverWrittenOutsideTheCredentialsDirectory(t *testing.T) {
	// The structural half: whatever the id, the resolved path is inside the
	// credentials directory. Belt and braces with the refusals above, because
	// the refusal list is a list and this is a property.
	state := withStateDir(t)
	want := filepath.Join(state, "credentials")
	for _, id := range []string{"anthropic", "openai", "a.b.c", "x_y-z", "hexgrad/Kokoro-82M"} {
		path, err := RouteKeyPath(id)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(path) != want {
			t.Errorf("the key for %q resolves to %s, outside %s", id, path, want)
		}
		if !strings.HasSuffix(path, ".key") {
			t.Errorf("the key for %q is not a .key file: %s", id, path)
		}
	}
}

func TestTheCursorKeyAndRouteKeysShareTheSameDirectoryAndMode(t *testing.T) {
	// One pattern, one set of permissions, one place to look. That sameness is
	// the argument for using a file at all rather than inventing a second
	// mechanism for route credentials.
	state := withStateDir(t)
	if err := SaveRouteKey("anthropic", "sk-ant-api03-together"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCursorSigningKey(); err != nil {
		t.Fatal(err)
	}

	cursor, err := os.Stat(filepath.Join(state, "cursor.key"))
	if err != nil {
		t.Fatal(err)
	}
	route, err := os.Stat(filepath.Join(state, "credentials", "anthropic.key"))
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Mode().Perm() != route.Mode().Perm() {
		t.Errorf("the cursor key is %04o and a route key is %04o; they should be the same",
			cursor.Mode().Perm(), route.Mode().Perm())
	}
}
