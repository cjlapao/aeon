package tools

// The `grep` tool: search file contents for a Go regular expression, from
// behind the ONE path resolver (S11, T15).
//
// # The one caller-supplied path, and why there is no second
//
// `path` is the only caller string this tool resolves — it names the
// DIRECTORY to search, and it goes through Resolve exactly the way read.go
// and write.go do (identical refusals, same three escape classes). The
// `glob` filter is NOT a path: it is matched against walker-produced
// relative paths and never resolved, never opened. Everything the tool
// reads is a path the WALKER produced from inside the resolved directory;
// each rendered hit is `rel/path:line: text` with `rel/path` from the walk.
// A path appearing in a RESULT is information the model may then choose to
// open — through `read`, which resolves it — never an implicit read this
// tool performed on the model's behalf.
//
// # Caps are reported with the TRUE totals (D9 — spawn.go's house style)
//
// GrepMaxMatches caps the hits returned, GrepMaxLineBytes caps one rendered
// line, GrepMaxBytes caps the rendered block; every cap reports the TRUE
// totals the completed scan discovered. The scan keeps counting after the
// match cap rather than stopping early, so "showing N of M matches" names an
// exact M, never a lower bound.
//
// # Binary skip and unreadable skip — counted, never silent
//
// A NUL byte in the first 512 bytes (GrepBinarySniffBytes) marks a file
// binary: grepping a compiled artifact for `foo` and getting a wall of
// mojibake is a worse answer than a counted skip. A file that cannot be
// opened or read mid-scan is SKIPPED WITH A COUNTED NOTE, never a crash —
// workspaces gain and lose files under a running step, and the rest of the
// results are worth having. A scan that skipped anything says so in the
// output: "no matches (scanned 3 files)" and "no matches (scanned 3 files,
// [skipped 1 unreadable file])" are different claims, and only the first
// one is a proof of absence.
//
// A file past GrepMaxFileBytes is skipped WITH A COUNT before it is ever
// opened (SEC-T15-01, stat-before-open): the directory-wide walk reaches
// files the model never named, and the one artifact shape that turns a scan
// into an OOM — huge, printable, no newline — must be refused at the
// boundary that has to survive it (the daemon owns every loop, P29). The
// scan also checks ctx inside one file (grepCtxCheckEveryLines), so the
// bound is cancellable, not just sized.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// GrepMaxMatches caps the hits `grep` returns.
//
// # Provenance: the S11 T15 spec's pinned number ("GrepMaxMatches (e.g.
// 200)"). Chosen with the same arithmetic as GlobMaxResults: above what a
// step acts on, under what a transcript carries.
const GrepMaxMatches = 200

// GrepMaxLineBytes caps ONE rendered hit line.
//
// # Provenance: chosen — 512 bytes. A minified line can be megabytes; the
// matched text a model needs is at the match, and the cut says so with the
// line's true length (D9 applies per line, not just per result).
const GrepMaxLineBytes = 512

// GrepMaxBytes caps the rendered hits block.
//
// # Provenance: chosen — 64 KiB, agreeing with BashStdoutCap and
// GlobMaxBytes so the layers agree at the boundary. The count and line caps
// bound 200 × ~550 bytes ≈ 110 KiB, so this cap is reachable behind them.
const GrepMaxBytes = 65536

// GrepBinarySniffBytes is the NUL-sniff window.
//
// # Provenance: the S11 T15 spec's pinned window ("NUL sniff in the first
// 512 bytes"). It is also one bufio-sized glance: everything past it has to
// be read to be examined anyway, and every format with a text prefix puts
// its magic where a reader will find it early.
const GrepBinarySniffBytes = 512

// GrepMaxFileBytes bounds how much ONE file can cost the scan.
//
// # Provenance: chosen (SEC-T15-01, the T19 hardening item) — 64 MiB.
//
// grepOne accumulates a file with no newline (or no early NUL) whole into
// memory while ReadString hunts for the delimiter: measured, a crafted no-
// newline artifact grows the heap ~2× its size, and at ≥8 GiB the daemon
// dies — and the daemon owns every loop (P29). The directory-wide walk
// reaches such a file WITHOUT the model naming it, so the bound belongs
// here, at the boundary: stat before open, and anything past it is SKIPPED
// WITH A COUNT, never scanned and never silent.
//
// 64 MiB sits above every honest source-shaped file (minified bundles are
// megabytes, the recorded accident scale) and two orders below the measured
// kill threshold, so the accepted cost of a false skip is a search the
// operator re-runs after a counted note, and the accepted cost of no bound
// was a dead daemon. The size is the lstat at walk time: a file that grows
// after being counted can outscan its number by whatever it grew — the kill
// class is a PRE-EXISTING multi-gigabyte artifact, and that is the class
// this closes. The mid-file ctx check in grepOne is the responsiveness half
// (a long scan is cancellable); neither re-reads what the other bounds.
const GrepMaxFileBytes = 64 << 20

// grepParams is `grep`'s input.
type grepParams struct {
	Pattern string `json:"pattern"`
	// Path is optional; empty means the workspace root — resolved, like any
	// other caller path, and the only caller path this tool resolves.
	Path string `json:"path,omitempty"`
	// Glob is a file FILTER in the tool's own pattern syntax (glob.go). It
	// is matched against walker paths; it is never resolved and never opened.
	Glob string `json:"glob,omitempty"`
	// IgnoreCase compiles the regexp with (?i) rather than lowercasing by
	// hand: one semantics (Go's), no second matching rule to keep in sync.
	IgnoreCase bool `json:"ignore_case,omitempty"`
}

// Grep is the `grep` tool.
func Grep() Tool {
	return Tool{
		Class: RerunSafe,
		Spec: model.ToolSpec{
			Name: "grep",
			Description: "Search file contents for a Go regular expression under a workspace-relative directory " +
				"(default: the workspace root). Hits render as `rel/path:line: text` with workspace-relative paths " +
				"produced by the walk — a path in a result is information, never an implicit read; pass it to `read` " +
				"to see content. Binary files are sniffed and skipped, unreadable files are skipped and counted, and " +
				"files above the per-file size limit are skipped before being opened and counted; every skip is stated. " +
				"Match counts and lines are capped; truncation is reported with the true totals. " +
				"The optional `glob` filters which files are searched using `**` (crosses directories) and `*` " +
				"(within one path segment). This tool's glob syntax is its own; policy command patterns use a " +
				"different, single-asterisk syntax.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Go regular expression; a pattern that does not compile is refused by name"},
    "path": {"type": "string", "description": "workspace-relative directory to search; defaults to the workspace root"},
    "glob": {"type": "string", "description": "optional file filter in the tool's own glob syntax: ** crosses directories, * stays inside one path segment"},
    "ignore_case": {"type": "boolean", "description": "case-insensitive matching"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`),
		},
		Run: runGrep,
	}
}

func runGrep(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p grepParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Errorf("the arguments for `grep` are not valid JSON: %v", err)
	}
	if p.Pattern == "" {
		return Errorf("grep refused: a pattern is required; an empty regular expression matches every " +
			"line of every file, which is never what was meant")
	}

	// The pattern is a Go regexp, and a compile failure is a REFUSAL naming
	// the pattern — not a scan that quietly finds nothing.
	source := p.Pattern
	if p.IgnoreCase {
		source = "(?i)" + source
	}
	re, err := regexp.Compile(source)
	if err != nil {
		return Errorf("grep refused: the pattern %q is not a valid Go regular expression: %v", p.Pattern, err)
	}

	// The glob filter compiles under the TOOL's syntax (glob.go owns it) and
	// the refusal repeats the disclaimer — the confusion it prevents is a
	// model filtering files with policy matcher syntax it just authored.
	var filter *globPattern
	if strings.TrimSpace(p.Glob) != "" {
		f, ferr := compileGlob(p.Glob)
		if ferr != nil {
			return Errorf("grep refused: the glob filter %q is not valid tool-glob syntax: %v. "+
				"This tool's glob syntax is its own; policy command patterns use a different, "+
				"single-asterisk syntax", p.Glob, ferr)
		}
		filter = &f
	}

	if strings.TrimSpace(p.Path) == "" {
		p.Path = "." // the default IS the workspace root, and the resolver checks it like any other path
	}
	resolved := Resolve(tc.WorkspaceRoot, p.Path)
	if !resolved.OK {
		// The named refusal, verbatim from the one enforcement point.
		return Errorf("grep refused %q: %s", p.Path, resolved.Reason)
	}

	info, err := os.Stat(resolved.Abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Errorf("grep could not find %s in the workspace; use `list` to find the directory", resolved.Rel)
		}
		return Errorf("grep could not stat %s: %v", resolved.Rel, err)
	}
	if !info.IsDir() {
		return Errorf("%s is a file; grep searches directories — pass the directory to search, or `read` the file", resolved.Rel)
	}

	var (
		hits              []string
		total             int // the TRUE match count across the whole scan
		scannedFiles      int
		skippedBinary     int
		skippedUnreadable int
		skippedDirs       int
		skippedTooLarge   int
		cancelErr         error
	)

	walkErr := filepath.WalkDir(resolved.Abs, func(walked string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			cancelErr = cerr
			return fs.SkipAll
		}
		if err != nil {
			skippedDirs++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if walked != resolved.Abs && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// Never followed, never opened: a symlink to a file OUTSIDE the
			// workspace would put external CONTENT into results the caller
			// never resolved — the exact escape this task's title forbids.
			return nil
		}
		rel := walkerRelative(resolved.Rel, resolved.Abs, walked)
		if filter != nil && !filter.match(rel) {
			return nil
		}
		// Stat BEFORE opening (SEC-T15-01, the T19 hardening): a file past
		// the per-file bound is skipped with a count and never opened, so
		// the giant is never even asked for its first byte. The walk-time
		// size is the bound; see GrepMaxFileBytes for the arithmetic.
		info, serr := d.Info()
		if serr != nil {
			skippedUnreadable++
			return nil
		}
		if info.Size() > GrepMaxFileBytes {
			skippedTooLarge++
			return nil
		}
		scannedFiles++
		room := GrepMaxMatches - len(hits)
		lines, matched, binary, rerr := grepOne(ctx, re, walked, rel, room)
		if rerr != nil {
			if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
				// The scan was cancelled mid-file — the walk-entry check
				// alone left one huge file as the one uncancellable unit
				// (SEC-T15-01's second half). Report it like the entry
				// check does, not as an unreadable file.
				cancelErr = rerr
				return fs.SkipAll
			}
			skippedUnreadable++ // counted, not crashed (see the header)
			return nil
		}
		if binary {
			skippedBinary++
			return nil
		}
		hits = append(hits, lines...)
		total += matched
		return nil
	})
	if walkErr != nil {
		return Errorf("grep failed walking %s: %v", resolved.Rel, walkErr)
	}
	if cancelErr != nil {
		return Errorf("grep was cancelled after %d of %d matches: %v", len(hits), total, cancelErr)
	}

	shown := len(hits)
	body := strings.Join(hits, "\n")
	countTruncated := shown < total
	byteTruncated := false
	if len(body) > GrepMaxBytes {
		cut := strings.LastIndexByte(body[:GrepMaxBytes], '\n')
		if cut < 0 {
			cut = GrepMaxBytes
		}
		body = body[:cut]
		shown = strings.Count(body, "\n") + 1
		byteTruncated = true
	}

	// The skip note comes with the header, BEFORE the hits (read.go's rule:
	// a notice after the content is a notice nobody reads).
	var notes []string
	if skippedBinary > 0 {
		notes = append(notes, fmt.Sprintf("%d binary files", skippedBinary))
	}
	if skippedUnreadable > 0 {
		notes = append(notes, fmt.Sprintf("%d unreadable files", skippedUnreadable))
	}
	if skippedDirs > 0 {
		notes = append(notes, fmt.Sprintf("%d unreadable directories", skippedDirs))
	}
	if skippedTooLarge > 0 {
		notes = append(notes, fmt.Sprintf("%d files above the %d-byte per-file limit", skippedTooLarge, GrepMaxFileBytes))
	}
	note := ""
	if len(notes) > 0 {
		note = "\n[skipped " + strings.Join(notes, ", ") + "]"
	}

	var header string
	switch {
	case scannedFiles == 0 && total == 0:
		header = fmt.Sprintf("pattern /%s/ in %s — no files to search under this directory", p.Pattern, resolved.Rel)
	case total == 0:
		header = fmt.Sprintf("pattern /%s/ in %s — no matches (%d files scanned)", p.Pattern, resolved.Rel, scannedFiles)
	case countTruncated:
		header = fmt.Sprintf("pattern /%s/ in %s — showing %d of %d matches (truncated at %d; narrow the pattern)",
			p.Pattern, resolved.Rel, shown, total, GrepMaxMatches)
	case byteTruncated:
		header = fmt.Sprintf("pattern /%s/ in %s — showing %d of %d matches (rendered output cut at %d bytes; narrow the pattern)",
			p.Pattern, resolved.Rel, shown, total, GrepMaxBytes)
	default:
		header = fmt.Sprintf("pattern /%s/ in %s — %d matches in %d files scanned", p.Pattern, resolved.Rel, total, scannedFiles)
	}

	summary := fmt.Sprintf("grep /%s/ in %s · showing %d of %d matches", p.Pattern, resolved.Rel, shown, total)
	return Result{Content: header + note + "\n\n" + body, Summary: summary}
}

// grepCtxCheckEveryLines bounds how often grepOne consults the context
// inside one file.
//
// # Provenance: chosen (SEC-T15-01's responsiveness half) — 1024 lines.
//
// The walk checks ctx per ENTRY, which left one long file the one
// uncancellable unit; consulting ctx on every line pays a call per line for
// a cancellation nobody observes at line granularity. 1024 lines is the
// balance: a scan notices a cancel within milliseconds on real files, and
// the check costs nothing measurable against the match itself.
const grepCtxCheckEveryLines = 1024

// grepOne scans ONE file for the pattern.
//
// It renders at most `room` hits (negative and zero mean "count only") but
// it COUNTS every match in the file, which is what keeps the header's "of M"
// exact after the cap (D9 — cappedBuffer's keep-counting-after-the-cap
// discipline applied to a scan instead of a stream).
//
// A file this cannot read comes back as an error the CALLER counts; a file
// whose opening sniff finds a NUL comes back flagged binary. Neither is a
// crash. The ctx arrives so a long scan is cancellable INSIDE the file
// (SEC-T15-01): cancellation returns ctx.Err(), which the caller reports as
// the cancellation it is — never as an unreadable file.
func grepOne(ctx context.Context, re *regexp.Regexp, abs, rel string, room int) (rendered []string, matched int, binary bool, err error) {
	if cerr := ctx.Err(); cerr != nil {
		return nil, 0, false, cerr
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	peek, perr := br.Peek(GrepBinarySniffBytes)
	if perr != nil && !errors.Is(perr, io.EOF) {
		return nil, 0, false, perr
	}
	if bytes.IndexByte(peek, 0) >= 0 {
		return nil, 0, true, nil
	}

	lineNo := 0
	for {
		line, rerr := br.ReadString('\n')
		if line != "" {
			lineNo++
			text := strings.TrimRight(line, "\r\n")
			if re.MatchString(text) {
				matched++
				if len(rendered) < room {
					rendered = append(rendered, fmt.Sprintf("%s:%d: %s", rel, lineNo, capGrepLine(text)))
				}
			}
			if lineNo%grepCtxCheckEveryLines == 0 {
				if cerr := ctx.Err(); cerr != nil {
					return rendered, matched, false, cerr
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return rendered, matched, false, nil
			}
			return rendered, matched, false, rerr
		}
	}
}

// capGrepLine renders one matched line within GrepMaxLineBytes, naming the
// line's TRUE length when it cuts — the per-line form of "truncation is
// reported with the true size", so a model can tell a long log line from a
// short one it failed to match.
func capGrepLine(text string) string {
	if len(text) <= GrepMaxLineBytes {
		return text
	}
	// Cut on bytes and repair: a valid UTF-8 result matters to every
	// consumer downstream, and one broken rune at the cut is worth dropping,
	// not debugging.
	cut := strings.ToValidUTF8(text[:GrepMaxLineBytes], "")
	return fmt.Sprintf("%s …[line cut at %d bytes; the line is %d bytes]", cut, GrepMaxLineBytes, len(text))
}
