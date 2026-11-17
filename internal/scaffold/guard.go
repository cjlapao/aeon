package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TargetFacts is what the guard knows about the ground the target must not
// touch — supplied by a seam at call time (T6/T7 read the component roots
// from the store, HOME and the state dir from the daemon's own paths, and
// intake.workspaceRoot from its S44 setting). The scaffold package holds
// none of it; a stale copy would be a second truth.
type TargetFacts struct {
	ComponentRoots []string // every registered component root (their REAL paths are compared)
	Home           string
	StateDir       string
	WorkspaceRoot  string // intake.workspaceRoot; empty = unset = no constraint
}

// CheckTarget is GI9's guard, run before anything plans or writes. The
// answer is one sentence naming what is wrong with THIS target. Comparisons
// happen on resolved paths: a symlinked root and its target are the same
// ground, and the guard refuses both.
func CheckTarget(target string, known TargetFacts) error {
	if target == "" || !filepath.IsAbs(target) {
		return fmt.Errorf("scaffold: the target %q is not an absolute path", target)
	}
	clean := filepath.Clean(target)
	if strings.Contains(clean, "..") {
		return fmt.Errorf("scaffold: the target %q still names a parent after cleaning — no .. paths", target)
	}
	realTarget, err := resolveGround(clean)
	if err != nil {
		return err
	}
	// Identity first: the HOME/state/component sentences must answer even for
	// grounds that are also non-empty directories — the empty sentence belongs to
	// a leftover scratch dir, not to someone’s home.
	if known.Home != "" {
		if realHome := realOr(known.Home); realHome != "" {
			if realTarget == realHome {
				return fmt.Errorf("scaffold: %s is the home directory itself — a project is a directory UNDER a place, never the place", realHome)
			}
		}
	}
	if known.StateDir != "" {
		if inside(realOr(known.StateDir), realTarget) {
			return fmt.Errorf("scaffold: %s is the daemon's own state ground — nothing is scaffolded inside where the database lives", realTarget)
		}
	}
	for _, root := range known.ComponentRoots {
		realRoot := realOr(root)
		if realRoot == "" {
			continue
		}
		switch {
		case realTarget == realRoot:
			return fmt.Errorf("scaffold: %s is already a registered component root — the scaffold creates a NEW project directory, it does not write into an existing component", realRoot)
		case inside(realRoot, realTarget):
			return fmt.Errorf("scaffold: %s lies inside the registered component root %s — a project directory cannot live under another project's working tree", realTarget, realRoot)
		case inside(realTarget, realRoot):
			return fmt.Errorf("scaffold: %s would CONTAIN the registered component root %s — scaffolding over a component's ancestor is scaffolding over the component", realTarget, realRoot)
		}
	}
	// The target must not exist, or be an empty directory (its REAL ground:
	// a symlink named target is refused, an empty dir is the one thing that
	// may be filled).
	if st, err := os.Lstat(clean); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("scaffold: %s is a symlink — the scaffold will not write through a path it did not make", clean)
		}
		if !st.IsDir() {
			return fmt.Errorf("scaffold: %s exists and is not a directory — the scaffold creates, it does not replace", clean)
		}
		entries, err := os.ReadDir(clean)
		if err != nil {
			return fmt.Errorf("scaffold: cannot read %s: %w", clean, err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("scaffold: %s is not empty — the scaffold writes into a new directory only and never overwrites what is there", clean)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("scaffold: cannot look at %s: %w", clean, err)
	}
	if known.WorkspaceRoot != "" {
		realWS := realOr(known.WorkspaceRoot)
		if realWS != "" && !inside(realWS, realTarget) {
			return fmt.Errorf("scaffold: %s is outside intake.workspaceRoot (%s) — the setting is the boundary; choose a target under it", realTarget, realWS)
		}
	}
	return nil
}

// resolveGround resolves the longest existing prefix of target (its REAL
// path) and puts the not-yet-created tail back: the guard compares real
// grounds even for a target that lawfully does not exist yet. The parent
// must exist and be a real directory (GI9).
func resolveGround(target string) (string, error) {
	// Every EDGE of the given path is walked from the root, because a
	// symlink in the MIDDLE of a path is invisible to a last-component
	// Lstat: path resolution swallows it before the check ever sees the
	// link (measured by the S61 T12 walk, whose symlinked-parent target
	// landed through the link with rc=0). No segment, middle or last, is
	// allowed to be a link.
	for p := filepath.Clean(target); ; {
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("scaffold: %s is a symlink — the scaffold refuses symlinked paths in either direction", p)
		}
		p = parent
	}
	tail := []string{}
	cur := target
	for {
		if st, err := os.Lstat(cur); err == nil {
			if st.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("scaffold: %s is a symlink — the scaffold refuses symlinked paths in either direction", cur)
			}
			if len(tail) == 0 && !st.IsDir() {
				return "", fmt.Errorf("scaffold: %s exists and is not a directory — the scaffold creates, it does not replace", cur)
			}
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", fmt.Errorf("scaffold: %s cannot be resolved: %w", cur, err)
			}
			if len(tail) > 0 && !st.IsDir() {
				return "", fmt.Errorf("scaffold: %s is not a directory — a path under it cannot be created", cur)
			}
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("scaffold: cannot look at %s: %w", cur, err)
		}
		tail = append(tail, filepath.Base(cur))
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("scaffold: %s has no existing parent directory", target)
		}
		cur = parent
	}
}

// realOr resolves p, returning "" when it cannot be resolved at all (a
// component root that no longer exists cannot be trespassed on; an
// unresolvable HOME/state dir simply does not constrain — the facts seam
// supplies real paths for what matters).
func realOr(p string) string {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return ""
	}
	return real
}

// inside reports whether probe lies strictly under parent.
func inside(parent, probe string) bool {
	if parent == "" {
		return false
	}
	rel, err := filepath.Rel(parent, probe)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
