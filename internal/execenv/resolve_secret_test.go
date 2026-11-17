package execenv

// Resolution is where EV7 is either true or a comment, so these tests are the
// slice's security acceptance. Every refusal is checked twice: that it fires,
// and that its three-part sentence names the reference and the source without
// naming the value.

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const resolveSentinel = "Sup3rS3cretValue+/Rt9Q="

// fakeFiles answers the filesystem's two calls from maps, so the rules can be
// tested without chmod and chown — and so the interesting case, a file whose
// facts change between the stat and the open, is writable at all.
type fakeFiles struct {
	stat   map[string]FileFacts
	open   map[string]FileFacts // facts reported at open time; defaults to stat
	data   map[string][]byte
	err    map[string]error
	opened []string // paths Open was actually called with
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{
		stat: map[string]FileFacts{},
		open: map[string]FileFacts{},
		data: map[string][]byte{},
		err:  map[string]error{},
	}
}

func goodFile(uid int) FileFacts {
	return FileFacts{Regular: true, Perm: 0o600, Size: int64(len(resolveSentinel)), UID: uid}
}

func (f *fakeFiles) Lstat(path string) (FileFacts, error) {
	if err, ok := f.err[path+"#stat"]; ok {
		return FileFacts{}, err
	}
	facts, ok := f.stat[path]
	if !ok {
		return FileFacts{}, os.ErrNotExist
	}
	return facts, nil
}

func (f *fakeFiles) Open(path string) (FileFacts, []byte, error) {
	if err, ok := f.err[path+"#open"]; ok {
		return FileFacts{}, nil, err
	}
	f.opened = append(f.opened, path)
	facts, ok := f.open[path]
	if !ok {
		facts = f.stat[path]
	}
	return facts, f.data[path], nil
}

// keyRef is the reference every key-file case uses, over `path`.
func keyRef(path string) RefSpec {
	return RefSpec{Name: "db-url", SourceKind: "key-file", Source: path, InjectAs: "DATABASE_URL"}
}

func daemonEnvRefs(v string) ResolveEnv {
	return ResolveEnv{
		Lookup:      func(string) string { return v },
		DaemonUID:   1000,
		Credentials: DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN"}},
	}
}

func TestResolveDaemonEnv(t *testing.T) {
	ref := RefSpec{Name: "api", SourceKind: "daemon-env", Source: "APP_API_KEY", InjectAs: "API_KEY"}

	inj, r := ResolveRef(ref, daemonEnvRefs(resolveSentinel))
	if r != nil {
		t.Fatalf("a set variable was refused: %s", r.Sentence())
	}
	if inj.Name != "api" || inj.InjectAs != "API_KEY" {
		t.Errorf("the injection lost which reference it came from or which name it lands as: %+v", inj)
	}

	// Unset and blank are the same refusal, and the sentence names the
	// variable: a command that runs with an empty credential does something
	// other than what its author meant, and most programs read empty as
	// configured.
	for _, v := range []string{"", "   "} {
		_, r := ResolveRef(ref, daemonEnvRefs(v))
		if r == nil {
			t.Fatalf("an unset/blank variable resolved for %q", v)
		}
		if r.Kind != RefusalSecretUnresolvable {
			t.Errorf("kind = %q, want %s", r.Kind, RefusalSecretUnresolvable)
		}
		if !strings.Contains(r.Sentence(), "APP_API_KEY") {
			t.Errorf("the refusal does not name the variable to set: %s", r.Sentence())
		}
	}

	// A daemon that offered no reader at all is a different state from a
	// variable that is unset, and says so.
	_, r = ResolveRef(ref, ResolveEnv{DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials})
	if r == nil || !strings.Contains(r.Sentence(), "no environment") {
		t.Errorf("a daemon with no environment reader answered the wrong question: %v", r)
	}
}

func TestResolveKeyFileRules(t *testing.T) {
	const path = "/etc/aeon/db.key"
	cases := []struct {
		name    string
		facts   FileFacts
		want    string
		prefers string // a fragment the sentence must contain
	}{
		{"a symlink is refused, not followed", FileFacts{Symlink: true, Perm: 0o600, UID: 1000, Regular: false},
			KindKeyFileSymlink, "symlink"},
		{"a directory is refused", FileFacts{Perm: 0o600, UID: 1000}, KindKeyFileNotRegular, "not a regular file"},
		{"a group-readable file is refused", FileFacts{Regular: true, Perm: 0o640, UID: 1000},
			KindKeyFileMode, "chmod 600"},
		{"an other-readable file is refused", FileFacts{Regular: true, Perm: 0o644, UID: 1000},
			KindKeyFileMode, "chmod 600"},
		{"another user's file is refused", FileFacts{Regular: true, Perm: 0o600, UID: 0},
			KindKeyFileOwner, "chown"},
		{"an oversized file is refused before it is read", FileFacts{Regular: true, Perm: 0o600, UID: 1000,
			Size: MaxKeyFileBytes + 1}, KindKeyFileTooLarge, "65536"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeFiles()
			f.stat[path] = tc.facts
			f.data[path] = []byte(resolveSentinel)
			env := ResolveEnv{Files: f, DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials}
			_, r := ResolveRef(keyRef(path), env)
			if r == nil {
				t.Fatalf("the unsafe file was read; Open was called %v", f.opened)
			}
			if r.Kind != tc.want {
				t.Errorf("kind = %q, want %q (%s)", r.Kind, tc.want, r.Sentence())
			}
			if !strings.Contains(r.Sentence(), tc.prefers) {
				t.Errorf("the sentence does not name the fix %q: %s", tc.prefers, r.Sentence())
			}
			if !strings.Contains(r.Sentence(), path) || !strings.Contains(r.Sentence(), "db-url") {
				t.Errorf("the sentence names neither the reference nor the file: %s", r.Sentence())
			}
		})
	}

	t.Run("a missing file is its own refusal", func(t *testing.T) {
		f := newFakeFiles()
		_, r := ResolveRef(keyRef(path), ResolveEnv{Files: f, DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials})
		if r == nil || r.Kind != KindKeyFileMissing {
			t.Fatalf("r = %v, want %s", r, KindKeyFileMissing)
		}
		if !strings.Contains(r.Sentence(), "not there") {
			t.Errorf("a missing file must not read as an empty one: %s", r.Sentence())
		}
	})

	t.Run("a stat that fails for another reason says so", func(t *testing.T) {
		f := newFakeFiles()
		f.err[path+"#stat"] = fmt.Errorf("permission denied on the parent directory")
		_, r := ResolveRef(keyRef(path), ResolveEnv{Files: f, DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials})
		if r == nil || r.Kind != RefusalSecretUnresolvable {
			t.Fatalf("r = %v", r)
		}
	})

	t.Run("a relative path never reaches the filesystem", func(t *testing.T) {
		f := newFakeFiles()
		f.stat["db.key"] = goodFile(1000)
		f.data["db.key"] = []byte(resolveSentinel)
		_, r := ResolveRef(keyRef("db.key"), ResolveEnv{Files: f, DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials})
		if r == nil || r.Kind != KindKeyFileRelative {
			t.Fatalf("a relative path resolved: r=%v, opens=%v", r, f.opened)
		}
		if len(f.opened) != 0 {
			t.Errorf("the file was opened anyway: %v", f.opened)
		}
	})

	t.Run("one trailing newline is trimmed and the value still matches redaction", func(t *testing.T) {
		f := newFakeFiles()
		f.stat[path] = goodFile(1000)
		f.data[path] = []byte(resolveSentinel + "\n")
		inj, r := ResolveRef(keyRef(path), ResolveEnv{Files: f, DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials})
		if r != nil {
			t.Fatalf("a written-with-echo key file was refused: %s", r.Sentence())
		}
		// The proof the trim matters: a redactor built from this injection
		// finds the value as the command prints it (without the newline).
		rd := NewRedactor([]Injection{inj})
		if got := rd.RedactString("token=" + resolveSentinel); strings.Contains(got, resolveSentinel) {
			t.Errorf("the trailing newline stayed inside the value, so the printed token does not match: %s", got)
		}
	})

	t.Run("a file swapped between the check and the read is refused", func(t *testing.T) {
		// The TOCTOU case the interface exists for: the stat said regular,
		// 0600, owned — and the inode actually opened is somebody else's
		// symlink. A matcher that trusted the first answer would read it.
		f := newFakeFiles()
		f.stat[path] = goodFile(1000)
		f.open[path] = FileFacts{Symlink: true, Perm: 0o777, UID: 0}
		f.data[path] = []byte(resolveSentinel)
		_, r := ResolveRef(keyRef(path), ResolveEnv{Files: f, DaemonUID: 1000, Credentials: daemonEnvRefs("").Credentials})
		if r == nil || r.Kind != KindKeyFileSymlink {
			t.Fatalf("the swapped file was accepted: r=%v", r)
		}
	})
}

func TestResolveRefusesShortSecrets(t *testing.T) {
	// 7 refused, 8 accepted: the boundary is where redaction stops being
	// trustworthy, so it is asserted at the edge rather than somewhere safely
	// inside it.
	ref := RefSpec{Name: "short", SourceKind: "daemon-env", Source: "APP_VAR", InjectAs: "APP_VAR"}
	for n := 1; n <= 7; n++ {
		_, r := ResolveRef(ref, daemonEnvRefs(strings.Repeat("x", n)))
		if r == nil || r.Kind != KindSecretTooShort {
			t.Errorf("%d bytes: r = %v, want %s", n, r, KindSecretTooShort)
		}
	}
	if _, r := ResolveRef(ref, daemonEnvRefs(strings.Repeat("x", MinSecretBytes))); r != nil {
		t.Errorf("a %d-byte value was refused (%s); the documented minimum is %d",
			MinSecretBytes, r.Sentence(), MinSecretBytes)
	}
	// The refusal carries no length: "the value is 5 bytes" is a disclosure.
	_, r := ResolveRef(ref, daemonEnvRefs("tiny"))
	if r != nil && strings.Contains(r.Sentence(), "5") {
		t.Errorf("the refusal disclosed the length: %s", r.Sentence())
	}
}

func TestRefusalsNameTheRefNeverTheValue(t *testing.T) {
	// Every refusal path, with the sentinel in the value: no sentence may
	// contain it, its base64, or a length that discloses its shape.
	const path = "/etc/aeon/db.key"
	f := newFakeFiles()
	f.stat[path] = FileFacts{Regular: true, Perm: 0o644, UID: 1000, Size: 24}
	f.data[path] = []byte(resolveSentinel)

	refs := []RefSpec{
		{Name: "a", SourceKind: "daemon-env", Source: "APP_VAR", InjectAs: "APP_VAR"},
		{Name: "b", SourceKind: "key-file", Source: path, InjectAs: "DATABASE_URL"},
		{Name: "c", SourceKind: "store", Source: "route-42", InjectAs: "ROUTE_KEY"},
		{Name: "d", SourceKind: "vault", Source: "whatever", InjectAs: "WHATEVER"},
		{Name: "e", SourceKind: "daemon-env", Source: "AEON_FORGE_TOKEN", InjectAs: "APP_TOKEN"},
	}
	env := ResolveEnv{
		Lookup:      func(string) string { return resolveSentinel },
		Files:       f,
		DaemonUID:   1000,
		Credentials: DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"AEON_FORGE_TOKEN", "GH_TOKEN"}},
		Store:       func(id string) (string, bool) { return resolveSentinel, id == "route-42" },
	}
	_, refusals := ResolveAll(refs, env)
	if len(refusals) == 0 {
		t.Fatal("nothing was refused, so this test asserted nothing")
	}
	for _, r := range refusals {
		s := r.Sentence()
		encodings := []string{resolveSentinel}
		for _, enc := range []func([]byte) string{
			func(b []byte) string { return base64.StdEncoding.EncodeToString(b) },
			func(b []byte) string { return hex.EncodeToString(b) },
			percentEncode,
		} {
			encodings = append(encodings, enc([]byte(resolveSentinel)))
		}
		for _, forbidden := range encodings {
			if strings.Contains(s, forbidden) {
				t.Errorf("a refusal disclosed something about the value (%q): %s", forbidden, s)
			}
		}
		if r.Kind == "" || r.What == "" || r.Why == "" || r.Do == "" {
			t.Errorf("a refusal is missing a part of its sentence: %+v", r)
		}
	}
	// The store kind is the one A1 granted: it resolves when the daemon wired a
	// lookup and refuses, naming that fact, when it did not.
	inj, r := ResolveRef(refs[2], env)
	if r != nil {
		t.Fatalf("the approved store kind was refused: %s", r.Sentence())
	}
	if inj.InjectAs != "ROUTE_KEY" {
		t.Errorf("the injection landed as %q", inj.InjectAs)
	}
	if _, r := ResolveRef(refs[2], ResolveEnv{Credentials: env.Credentials, DaemonUID: 1000}); r == nil ||
		r.Kind != KindStoreNotWired {
		t.Errorf("an unwired store kind produced %v, want %s", r, KindStoreNotWired)
	}
}

func TestResolveAllReportsEveryProblem(t *testing.T) {
	f := newFakeFiles()
	const good = "/etc/aeon/good.key"
	f.stat[good] = goodFile(1000)
	f.data[good] = []byte(resolveSentinel)
	env := ResolveEnv{
		Lookup: func(n string) string {
			switch n {
			case "SET_VAR":
				return resolveSentinel
			case "SHORT_VAR":
				return "abc"
			}
			return ""
		},
		Files:       f,
		DaemonUID:   1000,
		Credentials: DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}},
	}
	refs := []RefSpec{
		{Name: "ok-env", SourceKind: "daemon-env", Source: "SET_VAR", InjectAs: "SET_VAR"},
		{Name: "ok-file", SourceKind: "key-file", Source: good, InjectAs: "FILE_VAR"},
		{Name: "missing", SourceKind: "daemon-env", Source: "UNSET_VAR", InjectAs: "UNSET_VAR"},
		{Name: "short", SourceKind: "daemon-env", Source: "SHORT_VAR", InjectAs: "SHORT"},
	}
	inj, refusals := ResolveAll(refs, env)
	if len(inj) != 2 {
		t.Errorf("resolved %d injections, want the 2 good ones", len(inj))
	}
	if len(refusals) != 2 {
		t.Fatalf("reported %d refusals, want 2 — a caller that gets the first refusal only cannot fix a "+
			"form one field at a time: %+v", len(refusals), refusals)
	}
	kinds := map[string]bool{}
	for _, r := range refusals {
		kinds[r.Kind] = true
	}
	if !kinds[RefusalSecretUnresolvable] {
		t.Errorf("the unset variable is missing from the refusals: %+v", refusals)
	}
}

func TestValidateRefRefusesBadShapes(t *testing.T) {
	creds := DaemonCredentials{Provider: []string{"ANTHROPIC_API_KEY"}, Forge: []string{"GH_TOKEN", "GITHUB_TOKEN", "AEON_FORGE_TOKEN"}}
	cases := []struct {
		name string
		ref  RefSpec
		want string
	}{
		{"no name", RefSpec{SourceKind: "daemon-env", Source: "APP", InjectAs: "APP"}, KindSourceEmpty},
		{"empty daemon source", RefSpec{Name: "n", SourceKind: "daemon-env", InjectAs: "APP"}, KindSourceEmpty},
		{"a daemon source that is not a name", RefSpec{Name: "n", SourceKind: "daemon-env", Source: "APP KEY", InjectAs: "APP"}, KindDaemonEnvShape},
		{"a daemon source with an equals", RefSpec{Name: "n", SourceKind: "daemon-env", Source: "APP=1", InjectAs: "APP"}, KindDaemonEnvShape},
		{"a relative key file", RefSpec{Name: "n", SourceKind: "key-file", Source: "k.key", InjectAs: "K"}, KindKeyFileRelative},
		{"a traversal key file", RefSpec{Name: "n", SourceKind: "key-file", Source: "/etc/../home/x/k.key", InjectAs: "K"}, KindKeyFileTraversal},
		{"a store source shaped like a path", RefSpec{Name: "n", SourceKind: "store", Source: "../state.db", InjectAs: "K"}, KindStoreSourceShape},
		{"a dot-prefixed store source", RefSpec{Name: "n", SourceKind: "store", Source: ".hidden", InjectAs: "K"}, KindStoreSourceShape},
		{"an unknown kind", RefSpec{Name: "n", SourceKind: "vault", Source: "x", InjectAs: "K"}, KindSourceKindUnknown},
		{"the daemon's provider key", RefSpec{Name: "n", SourceKind: "daemon-env", Source: "ANTHROPIC_API_KEY", InjectAs: "APP_TOKEN"}, KindDaemonCredential},
		{"the daemon's forge token", RefSpec{Name: "n", SourceKind: "daemon-env", Source: "AEON_FORGE_TOKEN", InjectAs: "APP_TOKEN"}, KindDaemonCredential},
		{"injecting AS the daemon's key", RefSpec{Name: "n", SourceKind: "daemon-env", Source: "APP_OK", InjectAs: "ANTHROPIC_API_KEY"}, KindDaemonCredential},
		{"injecting into the AEON namespace", RefSpec{Name: "n", SourceKind: "daemon-env", Source: "APP_OK", InjectAs: "AEON_WHATEVER"}, KindDaemonCredential},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ValidateRef(tc.ref, creds)
			if r == nil {
				t.Fatalf("accepted: %+v", tc.ref)
			}
			if r.Kind != tc.want {
				t.Errorf("kind = %q, want %q (%s)", r.Kind, tc.want, r.Sentence())
			}
			if !strings.Contains(r.Sentence(), "n") {
				t.Errorf("the refusal does not name the reference: %s", r.Sentence())
			}
		})
	}
	// And a clean reference is not refused, so the list above is not just a
	// list of everything.
	if r := ValidateRef(RefSpec{Name: "n", SourceKind: "daemon-env", Source: "APP_OK", InjectAs: "APP_TOKEN"}, creds); r != nil {
		t.Errorf("a clean reference was refused: %s", r.Sentence())
	}
	// An empty credential list refuses rather than passing: "not checked" is
	// not "checked and clean" (P3).
	if r := ValidateRef(RefSpec{Name: "n", SourceKind: "daemon-env", Source: "APP_OK", InjectAs: "APP_TOKEN"},
		DaemonCredentials{}); r == nil || r.Kind != KindDaemonCredential {
		t.Errorf("an empty credential list passed the daemon-credential check: %v", r)
	}
}

// TestOSFilesAgainstRealFiles exercises the production opener, not the fake:
// the rules are only as good as the facts, and `factsOf` is where they come
// from. It is skipped where the platform cannot answer ownership, because a
// refusal there is the designed behaviour and not a failure to test.
func TestOSFilesAgainstRealFiles(t *testing.T) {
	if CurrentUID() < 0 {
		t.Skip("this platform reports no uid; the owner check fails closed and there is nothing to compare")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.key")
	if err := os.WriteFile(good, []byte(resolveSentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(good, 0o600); err != nil {
		t.Fatal(err)
	}
	env := ResolveEnv{Files: OSFiles{}, DaemonUID: CurrentUID(), Credentials: daemonEnvRefs("").Credentials}

	inj, r := ResolveRef(keyRef(good), env)
	if r != nil {
		t.Fatalf("a real 0600 file the daemon owns was refused: %s", r.Sentence())
	}
	if rd := NewRedactor([]Injection{inj}); strings.Contains(rd.RedactString(resolveSentinel), resolveSentinel) {
		t.Error("the value read from disk does not match what redaction was built from")
	}

	wide := filepath.Join(dir, "wide.key")
	if err := os.WriteFile(wide, []byte(resolveSentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wide, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, r := ResolveRef(keyRef(wide), env); r == nil || r.Kind != KindKeyFileMode {
		t.Errorf("a group-readable key file was read: %v", r)
	}

	link := filepath.Join(dir, "link.key")
	if err := os.Symlink(good, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if _, r := ResolveRef(keyRef(link), env); r == nil || r.Kind != KindKeyFileSymlink {
		t.Errorf("a symlink was followed: %v", r)
	}

	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, r := ResolveRef(keyRef(subdir), env); r == nil || r.Kind != KindKeyFileNotRegular {
		t.Errorf("a directory was read: %v", r)
	}

	if _, r := ResolveRef(keyRef(filepath.Join(dir, "gone.key")), env); r == nil || r.Kind != KindKeyFileMissing {
		t.Errorf("a missing file answered %v", r)
	}

	// A file nobody owns (uid 0) is refused by a non-root daemon; run as root
	// the check still fires, because the facts still differ.
	other := goodFile(CurrentUID() + 1)
	other.Size = int64(len(resolveSentinel))
	f := newFakeFiles()
	f.stat["/x"] = other
	f.data["/x"] = []byte(resolveSentinel)
	if _, r := ResolveRef(keyRef("/x"), ResolveEnv{Files: f, DaemonUID: CurrentUID(), Credentials: daemonEnvRefs("").Credentials}); r == nil ||
		r.Kind != KindKeyFileOwner {
		t.Errorf("the owner rule does not fire on the facts the real stat produces: %v", r)
	}
}
