package scaffold

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PreviewFile is one file the write WILL create — path (relative), mode,
// size, and the sha256 of the exact bytes. `intake.plan` shows these and
// the plan digest covers them; TestPreviewEqualsWhatWasWritten is the
// promise that this preview is the truth, not a summary of intentions.
type PreviewFile struct {
	Path   string `json:"path"`
	Mode   int    `json:"mode"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

// Preview is the whole answer: files and the directories that must be
// created to hold them (targetDir itself first).
type Preview struct {
	Target string        `json:"target"`
	Files  []PreviewFile `json:"files"`
	Create []string      `json:"create"`
}

// Plan checks every path against the disk WITHOUT writing anything (GI9):
// the target guard has already run (CheckTarget); here each planned file
// must be a NEW path — Lstat sees it absent, and no path component along
// the way may be a symlink, so the check cannot be raced by a swap between
// plan and write (Write re-checks, and TestSymlinkRaceIsRefused proves the
// second look has teeth).
func Plan(targetDir string, files []File) (Preview, error) {
	if targetDir == "" || !filepath.IsAbs(targetDir) {
		return Preview{}, fmt.Errorf("scaffold: plan: the target must be absolute")
	}
	// GI9's plan-time reach: Lstat/EvalSymlinks of the PARENT. The target
	// itself may lawfully not exist yet (it is the thing being created);
	// CheckTarget has already refused it for being a non-empty dir, a
	// symlink or a stranger's ground.
	if _, err := resolveNoSymlink(filepath.Dir(targetDir)); err != nil {
		return Preview{}, fmt.Errorf("scaffold: plan: %w", err)
	}
	if st, err := os.Lstat(targetDir); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return Preview{}, fmt.Errorf("scaffold: plan: %s is a symlink", targetDir)
		}
		if !st.IsDir() {
			return Preview{}, fmt.Errorf("scaffold: plan: %s is not a directory", targetDir)
		}
	} else if !os.IsNotExist(err) {
		return Preview{}, fmt.Errorf("scaffold: plan: cannot look at %s: %w", targetDir, err)
	}
	pv := Preview{Target: targetDir}
	dirs := map[string]bool{targetDir: true}
	for _, f := range files {
		rel, err := validateRelPath(f.Path)
		if err != nil {
			return Preview{}, fmt.Errorf("scaffold: plan: %w", err)
		}
		full := filepath.Join(targetDir, filepath.FromSlash(rel))
		if err := refuseSymlinkChain(filepath.Dir(full), targetDir); err != nil {
			return Preview{}, err
		}
		if _, err := os.Lstat(full); err == nil {
			return Preview{}, fmt.Errorf("scaffold: plan: %s already exists — the scaffold writes new paths only and never overwrites", rel)
		} else if !os.IsNotExist(err) {
			return Preview{}, fmt.Errorf("scaffold: plan: cannot look at %s: %w", rel, err)
		}
		sum := sha256.Sum256(f.Content)
		pv.Files = append(pv.Files, PreviewFile{
			Path: rel, Mode: int(f.Mode), Size: len(f.Content), SHA256: hex.EncodeToString(sum[:]),
		})
		d := filepath.Dir(full)
		for d != targetDir && strings.HasPrefix(d, targetDir+string(os.PathSeparator)) {
			if dirs[d] {
				break
			}
			dirs[d] = true
			d = filepath.Dir(d)
		}
	}
	pv.Create = sortedForCreate(targetDir, dirs)
	return pv, nil
}

// sortedForCreate orders created dirs shallow-first for creation
// (deepest-first is the CLEANUP order; Cleanup walks this list backwards).
func sortedForCreate(targetDir string, dirs map[string]bool) []string {
	out := make([]string, 0, len(dirs))
	for d := range dirs {
		out = append(out, d)
	}
	sort.Strings(out) // shallower sorts before deeper under a shared prefix
	return out
}

// resolveNoSymlink resolves p fully (EvalSymlinks) and requires the result
// to exist; the pair (real path, error) is every guard's ground truth —
// comparisons against component roots, HOME and the state dir happen on
// REAL paths, never on strings a symlink can dress up.
func resolveNoSymlink(p string) (string, error) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved (%w) — the parent must exist as a real directory", p, err)
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("%s: %w", p, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", real)
	}
	return real, nil
}

// refuseSymlinkChain Lstats every component from stop (exclusive) down to
// leaf (inclusive) and refuses any symlink — the plan-time half of the
// TOCTOU story; Write's re-Lstat is the other half.
func refuseSymlinkChain(path, stop string) error {
	rel, err := filepath.Rel(stop, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("scaffold: plan: %s lies outside the target", path)
	}
	cur := stop
	for _, seg := range strings.Split(rel, string(os.PathSeparator)) {
		if seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		st, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil // below this, everything is being created
		}
		if err != nil {
			return fmt.Errorf("scaffold: plan: cannot look at %s: %w", cur, err)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("scaffold: plan: %s is a symlink — the scaffold refuses symlinked paths in either direction", cur)
		}
	}
	return nil
}
