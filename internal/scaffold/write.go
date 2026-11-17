package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Ledger is what one Write run CREATED — nothing else. Cleanup is only
// ever allowed to remove entries named here (GI9: "removes only what this
// run created, never a pre-existing directory"), and the intake ledger
// step can print it (an operator sees which paths the daemon is about to
// take back).
type Ledger struct {
	Dirs  []string `json:"dirs"`  // absolute, creation order (shallow first)
	Files []string `json:"files"` // absolute, creation order
}

// Write lays the previewed files: directories 0755, every file created
// with O_CREATE|O_EXCL (a file that appeared since the plan is refused,
// not replaced), and every path re-Lstat'd through its whole chain first —
// a symlink planted between plan and write dies here, not on the other
// side of the fence (TestSymlinkRaceIsRefused). On any refusal mid-run the
// caller calls Cleanup(ledger) and only this run's paths are taken back.
func Write(targetDir string, files []File, ledger *Ledger) error {
	if targetDir == "" || !filepath.IsAbs(targetDir) {
		return fmt.Errorf("scaffold: write: the target must be absolute")
	}
	if ledger == nil {
		return fmt.Errorf("scaffold: write: a ledger is required — an untracked write is an uncleanable one")
	}
	pv, err := Plan(targetDir, files)
	if err != nil {
		return err
	}
	for _, d := range pv.Create {
		if d == targetDir {
			if _, err := os.Lstat(targetDir); os.IsNotExist(err) {
				if err := os.Mkdir(targetDir, 0o755); err != nil {
					return fmt.Errorf("scaffold: write: creating the target: %w", err)
				}
				ledger.Dirs = append(ledger.Dirs, targetDir)
			} else if err != nil {
				return fmt.Errorf("scaffold: write: the target: %w", err)
			}
			continue
		}
		if _, err := os.Lstat(d); err == nil {
			continue // someone else's directory, pre-existing — never ours to record
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("scaffold: write: cannot look at %s: %w", d, err)
		}
		if err := os.Mkdir(d, 0o755); err != nil {
			return fmt.Errorf("scaffold: write: creating %s: %w", d, err)
		}
		ledger.Dirs = append(ledger.Dirs, d)
	}
	for _, f := range files {
		full := filepath.Join(targetDir, filepath.FromSlash(f.Path))
		if err := refuseSymlinkChain(filepath.Dir(full), targetDir); err != nil {
			return err
		}
		if _, err := os.Lstat(full); err == nil {
			return fmt.Errorf("scaffold: write: %s appeared after the plan — refusing to overwrite what this run did not create", f.Path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("scaffold: write: cannot look at %s: %w", full, err)
		}
		fh, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.Mode)
		if err != nil {
			return fmt.Errorf("scaffold: write: creating %s: %w", f.Path, err)
		}
		_, werr := fh.Write(f.Content)
		cerr := fh.Close()
		if werr != nil || cerr != nil {
			return fmt.Errorf("scaffold: write: writing %s: %w%v", f.Path, werr, cerr)
		}
		// OpenFile's mode rides through the umask; the preview promised an
		// exact mode, so the file is CHMOD'd to it — the promise holds
		// whatever the daemon's umask is.
		if err := os.Chmod(full, f.Mode); err != nil {
			return fmt.Errorf("scaffold: write: setting mode on %s: %w", f.Path, err)
		}
		ledger.Files = append(ledger.Files, full)
	}
	return nil
}

// Cleanup takes back exactly the ledger: files first, then directories
// deepest-first — each one removed only because THIS run wrote it into the
// ledger. An entry that is no longer empty (something else moved in) is
// reported, never forced: a forced delete of a stranger's content is the
// data loss this function exists to prevent.
func Cleanup(ledger *Ledger) error {
	if ledger == nil {
		return nil
	}
	var errs []string
	for _, f := range ledger.Files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", f, err))
		}
	}
	for i := len(ledger.Dirs) - 1; i >= 0; i-- {
		d := ledger.Dirs[i]
		if err := os.Remove(d); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", d, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("scaffold: cleanup left something behind (nothing was forced): %s", strings.Join(errs, "; "))
	}
	*ledger = Ledger{}
	return nil
}
