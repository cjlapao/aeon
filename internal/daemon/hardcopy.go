package daemon

// hardcopy.go — the file copy of a document, made by the daemon and never by an
// agent.
//
// A loop's deliverables live in the database. A pointer that other machinery
// hands to a different loop (the roadmap's brief and plan pointers, which a
// planner or an engineer later `read`s) needs a path, so the daemon exports the
// document to `{workspace}/.projects/{loop_slug}/{name}` — the one place a hard
// copy lives, one folder per loop — and the pointer names that file. The
// database stays the source of truth: the copy is overwritten on every export
// and nothing reads it back as the record.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjlapao/aeon/internal/store"
)

// exportHardCopy writes the head of a loop's document under the workspace and
// answers the absolute path. A document the loop never filed answers ("", nil):
// the caller decides whether that is a refusal or a fallback to a legacy file.
func (d *Daemon) exportHardCopy(workspaceRoot string, sc store.Scope, name string) (string, error) {
	if d.store == nil || workspaceRoot == "" {
		return "", nil
	}
	doc, err := d.store.ReadDocument(sc, name)
	if err != nil {
		return "", nil
	}
	dest := filepath.Join(workspaceRoot, ".projects", sc.LoopSlug, filepath.FromSlash(doc.File))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("export %s of loop %s: %w", doc.File, sc.LoopSlug, err)
	}
	if err := os.WriteFile(dest, []byte(doc.Content), 0o644); err != nil {
		return "", fmt.Errorf("export %s of loop %s: %w", doc.File, sc.LoopSlug, err)
	}
	return dest, nil
}
