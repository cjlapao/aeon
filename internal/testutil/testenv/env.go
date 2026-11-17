// Package testenv isolates process-wide test defaults from a contributor's state.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Isolate removes store overrides before setting private defaults. Per-test
// t.Setenv calls still override these defaults and restore them on cleanup.
func Isolate(dir string) error {
	for _, key := range []string{"AEON_STATE_DIR", "DATABASE_SQLITE_DSN", "DATABASE_SQLITE_STORAGE_PATH", "DATABASE_SQLITE_FILE_NAME"} {
		if err := os.Unsetenv(key); err != nil {
			return err
		}
	}
	if err := isolateGitHome(dir); err != nil {
		return err
	}
	for key, subdir := range map[string]string{"XDG_STATE_HOME": "state", "XDG_CONFIG_HOME": "config", "LocalAppData": "local"} {
		path := filepath.Join(dir, subdir)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		if err := os.Setenv(key, path); err != nil {
			return err
		}
	}
	return nil
}

// Run owns the temporary environment until all tests and their cleanups finish.
func Run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ae-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	if err := Isolate(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// isolateGitHome points HOME at a private directory holding a minimal
// .gitconfig. The daemon's git children keep the user's git config by design
// (code actions push to real remotes) and never copy GIT_* variables, so the
// only way to keep a contributor's `commit.gpgsign=true` with an SSH signing
// key from prompting for a passphrase on every test commit is a different HOME.
//
// Go's own directories are pinned from the real HOME first, so a test that
// shells out to `go build` still finds the module and build caches instead of
// downloading the private framework again.
func isolateGitHome(dir string) error {
	real := os.Getenv("HOME")
	if real == "" {
		return nil
	}
	if os.Getenv("GOPATH") == "" {
		if err := os.Setenv("GOPATH", filepath.Join(real, "go")); err != nil {
			return err
		}
	}
	if os.Getenv("GOMODCACHE") == "" {
		if err := os.Setenv("GOMODCACHE", filepath.Join(os.Getenv("GOPATH"), "pkg", "mod")); err != nil {
			return err
		}
	}
	if cache, err := os.UserCacheDir(); err == nil && os.Getenv("GOCACHE") == "" {
		if err := os.Setenv("GOCACHE", filepath.Join(cache, "go-build")); err != nil {
			return err
		}
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	cfg := "[user]\n\tname = aeon-test\n\temail = aeon-test@aeon.test\n[commit]\n\tgpgsign = false\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(cfg), 0o600); err != nil {
		return err
	}
	// An empty .zshrc keeps zsh from opening its first-run setup menu in the
	// terminal tests, which would swallow the shell's output.
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), nil, 0o600); err != nil {
		return err
	}
	return os.Setenv("HOME", home)
}
