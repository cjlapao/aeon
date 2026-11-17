// Package dbfixture clones a closed database fixture into independent test directories.
// It does not open databases or choose their schema; the calling package supplies that work.
package dbfixture

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Template migrates one empty database lazily. The initializer must close every
// connection before returning, so committed WAL pages have reached the main file.
// No test shares a connection, writable file, or directory with another test.
type Template struct {
	once sync.Once
	data []byte
	err  error
}

// Dir returns a fresh directory containing a private copy of the initialized file.
// Even the first caller gets a copy, so its writes cannot alter later fixtures.
func (f *Template) Dir(t *testing.T, file string, initialize func(string) error) string {
	t.Helper()
	f.once.Do(func() {
		dir := t.TempDir()
		if f.err = initialize(dir); f.err == nil {
			f.data, f.err = os.ReadFile(filepath.Join(dir, file))
		}
	})
	if f.err != nil {
		t.Fatalf("initialize database fixture: %v", f.err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, file), f.data, 0o600); err != nil {
		t.Fatalf("clone database fixture: %v", err)
	}
	return dir
}
