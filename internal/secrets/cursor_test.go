package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

// Point StoreDir at a temp dir for the duration of a test.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv(cursorSigningKeyEnv, "")
	return filepath.Join(dir, "aeon")
}

func TestGeneratesAndPersistsOnFirstRun(t *testing.T) {
	storeDir := isolate(t)

	if err := EnsureCursorSigningKey(); err != nil {
		t.Fatalf("EnsureCursorSigningKey: %v", err)
	}

	key := os.Getenv(cursorSigningKeyEnv)
	if len(key) != keyBytes*2 {
		t.Fatalf("key length = %d, want %d hex chars", len(key), keyBytes*2)
	}

	path := filepath.Join(storeDir, "cursor.key")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("key not persisted: %v", err)
	}
}

// The key signs pagination cursors. A key that changes per start invalidates
// every cursor already handed out, and that failure surfaces as a client bug.
func TestKeyIsStableAcrossRuns(t *testing.T) {
	isolate(t)

	if err := EnsureCursorSigningKey(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := os.Getenv(cursorSigningKeyEnv)

	t.Setenv(cursorSigningKeyEnv, "")
	if err := EnsureCursorSigningKey(); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if second := os.Getenv(cursorSigningKeyEnv); second != first {
		t.Fatalf("key changed across runs: %q then %q", first, second)
	}
}

// An operator injecting from a vault must always win. Aeon never overwrites a
// key it did not generate.
func TestAnExplicitKeyIsRespected(t *testing.T) {
	storeDir := isolate(t)
	t.Setenv(cursorSigningKeyEnv, "supplied-by-the-operator")

	if err := EnsureCursorSigningKey(); err != nil {
		t.Fatalf("EnsureCursorSigningKey: %v", err)
	}

	if got := os.Getenv(cursorSigningKeyEnv); got != "supplied-by-the-operator" {
		t.Fatalf("env key = %q, want it left alone", got)
	}
	if _, err := os.Stat(filepath.Join(storeDir, "cursor.key")); !os.IsNotExist(err) {
		t.Fatal("a key file was written although one was supplied; the supplied key must win and nothing should persist")
	}
}

// A secret must not be readable by anyone else, and an explicit mode is the
// only way to be sure: a permissive umask would otherwise make it world
// readable and nothing would report it.
func TestKeyFileIsOwnerOnly(t *testing.T) {
	storeDir := isolate(t)

	if err := EnsureCursorSigningKey(); err != nil {
		t.Fatalf("EnsureCursorSigningKey: %v", err)
	}

	info, err := os.Stat(filepath.Join(storeDir, "cursor.key"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file mode = %o, want 600", perm)
	}

	dirInfo, err := os.Stat(storeDir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("store dir mode = %o, want 700", perm)
	}
}

// An unreadable or empty key is corruption, not absence. Regenerating would
// silently invalidate every outstanding cursor; the cause is usually
// permissions, which is fixable once reported.
func TestAnEmptyKeyFileIsReportedNotReplaced(t *testing.T) {
	storeDir := isolate(t)
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(storeDir, "cursor.key")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := EnsureCursorSigningKey(); err == nil {
		t.Fatal("an empty key file was accepted or silently replaced; it must be reported")
	}
}
