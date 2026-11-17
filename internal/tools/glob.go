package tools

// The `glob` tool: which files match a name pattern, answered from behind
// the ONE path resolver (S11, T15).
//
// # What this tool is, and what it is not
//
// glob answers "which files match this pattern" and nothing else. The walker
// (filepath.WalkDir) is the ONLY thing that produces a path in the output, so
// every returned line is a workspace-relative path that existed at walk time.
// A path in a RESULT is information for the model, never an implicit read:
// the caller must still resolve anything it wants opened, through the same
// Resolve this tool consults for its `path` argument. There is no argument
// here that resolves-and-returns content — asserted structurally, once per
// file, in glob_grep_test.go.
//
// # The syntax is the TOOL's, not the policy matcher's
//
// internal/policy/pattern.go owns the command-policy grammar: a single '*'
// anywhere, everything else literal bytes. This is a different language for a
// different job — file paths — and the two must never be conflated by a
// model that just authored a CommandAccess rule:
//
//   - `**` as a whole segment matches any number of directories,
//   - `*` matches within ONE path segment and never crosses a `/`,
//   - `?` matches one character and `[abc]` a character class (the
//     path.Match vocabulary, which this tool owns and documents),
//   - a pattern with no `/` is a file-NAME pattern, matched anywhere under
//     the start directory; a pattern with a `/` is anchored at it.
//
// The Description states in words that this syntax is the tool's own and
// that policy command patterns use the different, single-asterisk syntax.
//
// # Caps are reported with the TRUE size (D9 — spawn.go's house style)
//
// GlobMaxResults caps the returned paths and GlobMaxBytes caps the rendered
// listing; BOTH report the true count the completed walk discovered, because
// "500 paths" and "500 of 31,402 paths" are different answers — the first
// invites the model to conclude it has seen everything. The walk keeps
// counting after the cap rather than stopping early, so the total is exact
// and no "at least" hedge is ever needed here.
//
// # What the walker skips, and why nothing else is special
//
// `.git` trees are skipped (the answer a step wants is about its own files,
// not the object store), and non-regular entries are neither followed nor
// returned — a symlinked file pointing outside the workspace would otherwise
// put a path the caller could NOT have resolved into the results, and a
// symlinked directory is never descended. Nothing else gets special
// treatment: dotfiles are ordinary files and match patterns like anything
// else.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// GlobMaxResults caps the number of paths glob returns.
//
// # Provenance: the S11 T15 spec's pinned number ("define a package const,
// e.g. 500"). Chosen to sit far above what a step lists while staying far
// under what a transcript can carry.
const GlobMaxResults = 500

// GlobMaxBytes caps the rendered listing.
//
// # Provenance: chosen — 64 KiB, the same ceiling as BashStdoutCap
// (limits.go), so the two layers agree at the boundary rather than fighting.
// The count cap alone is not a bound on the STRING: 500 paths with
// 250-byte names reach ~125 KiB, which is why a byte cap sits behind it.
const GlobMaxBytes = 65536

// globParams is `glob`'s input.
type globParams struct {
	Pattern string `json:"pattern"`
	// Path is optional; empty means the workspace root, named in the
	// resolver call as "." rather than special-cased around it.
	Path string `json:"path,omitempty"`
}

// Glob is the `glob` tool.
func Glob() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name: "glob",
			Description: "Find files by name with a glob pattern under a workspace-relative directory " +
				"(default: the workspace root). Results are workspace-relative paths produced by the walk — " +
				"a result path is information, not a read; pass it to `read` to see content. " +
				"`**` as a segment crosses directories, `*` matches within one path segment, `?` one character, " +
				"`[abc]` a character class; a pattern without `/` matches file names anywhere. " +
				".git trees and symlinks are skipped. Results are capped, and truncation is reported with the " +
				"true count discovered. This tool's glob syntax is its own; policy command patterns use a " +
				"different, single-asterisk syntax.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "tool glob pattern: ** crosses directories, * stays inside one path segment"},
    "path": {"type": "string", "description": "workspace-relative directory to walk; defaults to the workspace root"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`),
		},
		Run: runGlob,
	}
}

func runGlob(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p globParams
	if err := json.Unmarshal(input, &p); err != nil {
		// A tool error, not a step failure (Q6): a model told its arguments
		// were malformed can fix them.
		return Errorf("the arguments for `glob` are not valid JSON: %v", err)
	}
	if p.Pattern == "" {
		return Errorf("glob refused: a pattern is required; `**` matches every file under the start directory")
	}
	pat, err := compileGlob(p.Pattern)
	if err != nil {
		// The refusal NAMES the pattern — a model that cannot see which
		// pattern it sent cannot correct it.
		return Errorf("glob refused: the pattern %q is not valid tool-glob syntax: %v. "+
			"This tool's glob syntax is its own; policy command patterns use a different, "+
			"single-asterisk syntax", p.Pattern, err)
	}

	if strings.TrimSpace(p.Path) == "" {
		p.Path = "." // the default IS the workspace root, and the resolver checks it like any other path
	}
	resolved := Resolve(tc.WorkspaceRoot, p.Path)
	if !resolved.OK {
		// The named refusal reaches the model verbatim, exactly as read.go and
		// write.go render it — the one enforcement point speaks with one voice.
		return Errorf("glob refused %q: %s", p.Path, resolved.Reason)
	}

	info, err := os.Stat(resolved.Abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Errorf("glob could not find %s in the workspace; use `list` to find the directory", resolved.Rel)
		}
		return Errorf("glob could not stat %s: %v", resolved.Rel, err)
	}
	if !info.IsDir() {
		return Errorf("%s is a file; glob walks directories — pass the directory to search", resolved.Rel)
	}

	var (
		paths        []string
		total        int // the TRUE count discovered, cap or no cap
		scannedFiles int
		skippedDirs  int
		cancelErr    error
	)

	walkErr := filepath.WalkDir(resolved.Abs, func(walked string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			cancelErr = cerr
			return fs.SkipAll
		}
		if err != nil {
			// A directory that cannot be read (permissions, a vanished tree)
			// is SKIPPED and COUNTED, never a crash: one unreadable corner
			// must not destroy the answers for the rest of the workspace,
			// and a silent skip would let a model conclude what it cannot
			// see does not exist.
			skippedDirs++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// `.git` is the one skipped tree (never the walk root itself —
			// a caller that explicitly resolves INTO .git through the
			// resolver made that choice; incidental descent is what is skipped).
			if walked != resolved.Abs && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// Symlinks are never followed and never returned (see the header).
			return nil
		}
		scannedFiles++
		rel := walkerRelative(resolved.Rel, resolved.Abs, walked)
		if !pat.match(rel) {
			return nil
		}
		total++
		if len(paths) < GlobMaxResults {
			paths = append(paths, rel)
		}
		return nil
	})
	if walkErr != nil {
		return Errorf("glob failed walking %s: %v", resolved.Rel, walkErr)
	}
	if cancelErr != nil {
		return Errorf("glob was cancelled after %d of %d paths: %v", len(paths), total, cancelErr)
	}

	// The byte cap sits behind the count cap (see GlobMaxBytes provenance).
	shown := len(paths)
	body := strings.Join(paths, "\n")
	countTruncated := shown < total
	byteTruncated := false
	if len(body) > GlobMaxBytes {
		cut := strings.LastIndexByte(body[:GlobMaxBytes], '\n')
		if cut < 0 {
			cut = GlobMaxBytes
		}
		body = body[:cut]
		shown = strings.Count(body, "\n") + 1
		byteTruncated = true
	}

	var header string
	switch {
	case scannedFiles == 0:
		// Three-state honesty: "no files to search" is not "nothing matched".
		if skippedDirs > 0 {
			// And a scan that saw nothing because it could not read is a
			// THIRD state — the note says which one the model is holding.
			header = fmt.Sprintf("pattern %q in %s — no files to search under this directory (%d unreadable directories skipped)",
				p.Pattern, resolved.Rel, skippedDirs)
		} else {
			header = fmt.Sprintf("pattern %q in %s — no files to search under this directory", p.Pattern, resolved.Rel)
		}
	case total == 0:
		header = fmt.Sprintf("pattern %q in %s — no files matched (%d files scanned)", p.Pattern, resolved.Rel, scannedFiles)
	case countTruncated:
		header = fmt.Sprintf("pattern %q in %s — showing %d of %d paths (truncated at %d; narrow the pattern)",
			p.Pattern, resolved.Rel, shown, total, GlobMaxResults)
	case byteTruncated:
		header = fmt.Sprintf("pattern %q in %s — showing %d of %d paths (rendered output cut at %d bytes; narrow the pattern)",
			p.Pattern, resolved.Rel, shown, total, GlobMaxBytes)
	default:
		header = fmt.Sprintf("pattern %q in %s — %d paths", p.Pattern, resolved.Rel, total)
	}
	if skippedDirs > 0 && scannedFiles > 0 {
		header += fmt.Sprintf(" [skipped %d unreadable directories]", skippedDirs)
	}

	summary := fmt.Sprintf("glob %s in %s · showing %d of %d paths", p.Pattern, resolved.Rel, shown, total)
	return Result{Content: header + "\n\n" + body, Summary: summary}
}

// walkerRelative renders a walk-visited path as a workspace-relative path.
//
// It exists so the walker is the ONLY producer of result paths: the string
// comes from the walk itself (`walked`, a descendant of `rootAbs`) joined to
// the resolver's own relative form of the start directory — never from a
// caller-supplied string. No path joining on the workspace root happens
// outside the resolver, which is what TestGlobAndGrepShareTheOnePathCheck
// and paths.go's own structural test both police.
func walkerRelative(startRel, rootAbs, walked string) string {
	sub := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(walked, rootAbs), string(filepath.Separator)))
	return path.Join(startRel, sub)
}

// globPattern is a compiled tool-glob pattern (see the file header for the
// grammar). It is NOT the policy matcher; internal/policy/pattern.go owns
// that single-asterisk grammar and this type shares nothing with it.
type globPattern struct {
	segs         []string
	basenameOnly bool
}

// compileGlob splits the pattern into segments and refuses malformed syntax
// UP FRONT, at the argument boundary — a model gets one refusal naming the
// pattern, not a silent no-match. The probe against "x" is what surfaces
// path.Match's ErrBadPattern (an unterminated class) before matching, so
// match below never has to answer an error.
func compileGlob(pattern string) (globPattern, error) {
	g := globPattern{
		segs:         strings.Split(pattern, "/"),
		basenameOnly: !strings.Contains(pattern, "/"),
	}
	for _, seg := range g.segs {
		if seg == "" {
			return globPattern{}, errors.New("it contains an empty segment")
		}
		if _, err := path.Match(seg, "x"); err != nil {
			return globPattern{}, err
		}
	}
	return g, nil
}

// match reports whether a walker-produced workspace-relative path satisfies
// the pattern. Pure, total, panic-free: path.Match only ever sees the
// compile-time-validated segments and a real path segment.
func (g globPattern) match(rel string) bool {
	target := rel
	if g.basenameOnly {
		target = path.Base(rel)
	}
	return matchGlobSegments(g.segs, strings.Split(target, "/"))
}

// matchGlobSegments is the recursive segment walk. `**` consumes any number
// of segments (including none); every other pattern segment must match its
// aligned path segment through path.Match — which cannot cross `/`, which is
// the whole point of segment-wise matching. Exponential blowup is bounded by
// pattern length, not tree size: the patterns here are file-name patterns,
// and a `**` per segment is pathological in a way a 255-byte name is not.
func matchGlobSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchGlobSegments(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], segs[0]) // validated at compile; the error arm is unreachable
	return ok && matchGlobSegments(pat[1:], segs[1:])
}
