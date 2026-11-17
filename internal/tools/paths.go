// Package tools holds the step's tools: `read`, `bash`, and the allow-list
// that decides which of them a step has.
//
// # Two tools, and one of them has no policy
//
// `read` and `bash` (W11). **`bash` consults no policy at S5** (W12): there is
// no pattern corpus, no `ask`, no human prompt — S11 brings all three. What
// `bash` has instead is BOUNDS (W13): a mandatory timeout, stdin closed, output
// caps reported with true sizes, `oomKilled` reported, and whole-process-group
// termination.
//
// That is acceptable for a skateboard because the tool list is an ALLOW-LIST
// and `bash` is not in the default grant — a step gets it only when somebody
// passes `--allow-bash`, which warns every time. It is not shippable, and the
// demo says so in a section naming S11.
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The ONE path resolver (W2, I1).
//
// # Why one, and why that is an invariant rather than a preference
//
// Two resolvers diverge, only one gets tested, and **the divergence is an
// escape that works**. A test asserts no other function in this package joins
// or cleans a path.
//
// # The symlink check runs AFTER resolution, and that is the whole trick (O3)
//
// Measured, before this was written:
//
//	workspace/escape -> /somewhere/else          (a symlink inside the workspace)
//	filepath.Clean("workspace/escape/secret")  = workspace/escape/secret   ← LOOKS fine
//	filepath.EvalSymlinks(…)                   = /somewhere/else/secret    ← caught
//
// A pre-resolution check passes for a symlink, which is the escape this class
// of bug always uses. Cleaning is lexical; it does not touch the filesystem and
// cannot see a link.
//
// Two more measured facts shape the implementation:
//
//   - `EvalSymlinks` on a path that does NOT EXIST returns `""` and an
//     IsNotExist error. A `write` target need not exist, so the nearest
//     EXISTING ancestor is resolved instead and checked — which covers a write
//     to a new path through a symlinked directory, the case a naive check
//     misses.
//   - **the ROOT must be resolved too.** A symlinked workspace root resolves to
//     its real path, so comparing a resolved child against an unresolved root
//     makes every path look like an escape. `/tmp` is a symlink on macOS, so
//     this is not hypothetical.

// ResolveResult is a resolved path, or the named reason it was refused.
//
// A result rather than `(string, error)` because each refusal has its own
// REASON that reaches a model as a tool error, and the model can act on the
// difference: an absolute path is a mistake it can correct, and a symlink
// escape is one it should stop attempting.
type ResolveResult struct {
	OK bool

	// Abs is the absolute, symlink-resolved path, when OK.
	Abs string

	// Rel is the path relative to the root, for messages. A tool error quoting
	// an absolute path leaks the workspace's location into the transcript.
	Rel string

	// Refusal is which rule refused it.
	Refusal Refusal

	// Reason is what to tell the model.
	Reason string
}

// Refusal names which rule refused a path.
type Refusal string

const (
	RefusalNone Refusal = ""
	// RefusalEmpty — no path at all.
	RefusalEmpty Refusal = "empty"
	// RefusalAbsolute — an absolute path. The workspace is the only addressable
	// thing, so an absolute path is always outside it.
	RefusalAbsolute Refusal = "absolute"
	// RefusalTraversal — a `..` segment in the INPUT, refused before any
	// resolution.
	RefusalTraversal Refusal = "traversal"
	// RefusalEscape — resolved, and outside the root. The symlink case.
	RefusalEscape Refusal = "escape"
	// RefusalUnresolvable — the path's ancestors cannot be resolved at all.
	RefusalUnresolvable Refusal = "unresolvable"
)

// Resolve turns a workspace-relative path into an absolute one, or refuses it.
//
// `root` must be absolute. A relative root is a programmer error and panics
// rather than being refused: it comes from `StepRequest.WorkspaceRoot`, which
// the protocol handler validated, so reaching here with a relative one means a
// caller skipped construction.
func Resolve(root, rel string) ResolveResult {
	if !filepath.IsAbs(root) {
		panic(fmt.Sprintf("tools: the workspace root %q is not absolute; it comes from a validated "+
			"StepRequest, so a relative one means construction was skipped", root))
	}

	if strings.TrimSpace(rel) == "" {
		return ResolveResult{Refusal: RefusalEmpty,
			Reason: "a path is required"}
	}
	if strings.ContainsRune(rel, 0) {
		return ResolveResult{Refusal: RefusalTraversal,
			Reason: "a path cannot contain a NUL byte"}
	}

	// Backslashes count as separators too, so a Windows-shaped path cannot
	// smuggle a `..` past a check that only splits on `/`.
	unified := strings.ReplaceAll(rel, `\`, "/")

	if filepath.IsAbs(rel) || strings.HasPrefix(unified, "/") || windowsDriveAbsolute(unified) {
		return ResolveResult{Refusal: RefusalAbsolute,
			Reason: fmt.Sprintf("%q is an absolute path. Paths are relative to the workspace root, "+
				"which is the only thing a step can address", rel)}
	}

	// `..` is refused IN THE INPUT, before anything is cleaned. A cleaned path
	// has had its escape resolved away: `a/../../b` becomes `../b` and then
	// the check is about a different string than the model sent.
	for _, segment := range strings.Split(unified, "/") {
		if segment == ".." {
			return ResolveResult{Refusal: RefusalTraversal,
				Reason: fmt.Sprintf("%q contains a `..` segment. It is refused rather than resolved, "+
					"because resolving it would read somewhere outside the workspace", rel)}
		}
	}

	// The root is resolved too — see the note above about a symlinked root.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ResolveResult{Refusal: RefusalUnresolvable,
			Reason: fmt.Sprintf("the workspace root could not be resolved: %v", err)}
	}

	candidate := filepath.Join(realRoot, filepath.FromSlash(unified))

	// Resolve the candidate, or its nearest existing ancestor when it does not
	// exist yet.
	resolved, err := resolveExistingPrefix(candidate)
	if err != nil {
		return ResolveResult{Refusal: RefusalUnresolvable,
			Reason: fmt.Sprintf("%q could not be resolved: %v", rel, err)}
	}

	if !within(realRoot, resolved) {
		return ResolveResult{Refusal: RefusalEscape,
			Reason: fmt.Sprintf("%q resolves outside the workspace. A symlink pointing out of the "+
				"workspace is refused — the check runs after resolution, because before it the "+
				"path looks like an ordinary one inside", rel)}
	}

	relative, err := filepath.Rel(realRoot, candidate)
	if err != nil {
		relative = unified
	}
	return ResolveResult{OK: true, Abs: candidate, Rel: filepath.ToSlash(relative)}
}

// resolveExistingPrefix resolves symlinks as far down the path as exists.
//
// For a path that exists, that is the path. For one that does not — a `write`
// target, which S5 has no tool for but whose resolver this is — it is the
// nearest existing ancestor, which is what catches a write to a NEW file
// through a symlinked directory.
//
// It walks up rather than down because `EvalSymlinks` is all-or-nothing: given
// a missing leaf it reports IsNotExist and resolves nothing, so there is no
// partial answer to take.
func resolveExistingPrefix(path string) (string, error) {
	current := path
	var missing []string

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			// Re-attach what did not exist. The result is "where this path
			// WOULD be", which is exactly what the containment check needs.
			return filepath.Join(append([]string{resolved}, reverse(missing)...)...), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding anything that
			// exists. Unreachable in practice — `/` exists — and reported
			// rather than looped on.
			return "", fmt.Errorf("no ancestor of %q exists", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// within reports whether `path` is inside `root`.
//
// `filepath.Rel` rather than a string prefix, because a prefix comparison says
// `/workspace-evil` is inside `/workspace`. That is the oldest bug in this
// family and it is one character of difference.
func within(root, path string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

func windowsDriveAbsolute(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return isLetter && s[1] == ':' && s[2] == '/'
}

func reverse(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
