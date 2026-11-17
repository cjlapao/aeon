// Package secrets provisions the local secrets the daemon needs to start.
//
// Aeon is a single-user local daemon, so there is no operator standing by to
// mint a key before the first run. Anything the framework requires and the
// product cannot ask a human for, this package generates once and keeps.
package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/paths"
)

// cursorSigningKeyEnv is the variable the framework's config service reads.
const cursorSigningKeyEnv = "CURSOR_SIGNING_KEY"

// keyBytes is 32 bytes — 256 bits, the length the framework's own guidance
// suggests (`openssl rand -hex 32`).
const keyBytes = 32

// EnsureCursorSigningKey guarantees CURSOR_SIGNING_KEY is set in this process's
// environment, generating and persisting one on first run.
//
// MUST be called BEFORE system.Initialize. The config service reads the
// environment as it initialises, and a key set afterwards is a key it never
// saw — which fails as "cursor.signing_key is not set", pointing at
// configuration rather than at ordering.
//
// Three cases, in order:
//
//  1. Already in the environment — respected and left alone, so an operator
//     injecting from a vault or a secret store always wins. Aeon never
//     overwrites a key it did not generate.
//  2. On disk from a previous run — loaded.
//  3. Neither — generated, written 0600, and exported.
//
// The file is the reason this is safe to generate rather than prompt for: the
// key must be STABLE across restarts, and a key regenerated per start would
// invalidate every cursor already handed out, which looks like a client bug.
func EnsureCursorSigningKey() error {
	if strings.TrimSpace(os.Getenv(cursorSigningKeyEnv)) != "" {
		return nil
	}

	path, err := paths.CursorSigningKeyFile()
	if err != nil {
		return fmt.Errorf("resolve cursor key path: %w", err)
	}

	if key, err := readKey(path); err == nil {
		return os.Setenv(cursorSigningKeyEnv, key)
	} else if !os.IsNotExist(err) {
		// A key that exists but cannot be read is NOT a reason to mint a new
		// one: overwriting it would silently invalidate every outstanding
		// cursor, and the cause is usually permissions, which is fixable.
		return fmt.Errorf("read cursor key %s: %w", path, err)
	}

	key, err := generateKey()
	if err != nil {
		return err
	}
	if err := writeKey(path, key); err != nil {
		return err
	}
	return os.Setenv(cursorSigningKeyEnv, key)
}

func readKey(path string) (string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the path is ours, not caller input
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		// An empty file is corruption, not absence. Reporting it beats both
		// silently regenerating and starting with an empty key.
		return "", fmt.Errorf("cursor key file %s is empty", path)
	}
	return key, nil
}

func generateKey() (string, error) {
	buf := make([]byte, keyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate cursor key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// writeKey persists the key readable only by its owner.
//
// 0700 on the directory and 0600 on the file, set explicitly rather than left
// to the umask — a permissive umask would otherwise make a secret world
// readable, and nothing would report it.
func writeKey(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return fmt.Errorf("write cursor key %s: %w", path, err)
	}
	return nil
}
