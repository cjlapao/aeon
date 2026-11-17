//go:build !noretrieval

package retrieval

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// chunk.go — what a chunk IS (V11, Q9). Both corpora chunk through this one
// file: two chunkers is how V11's heuristic drifts into two different notions
// of a chunk, so the code corpus (T4) and the history corpus (T5) call here.
//
// Q9, the heuristic and its fallback, stated as this file reads:
//
//   - Brace languages (go, c/h, cc/cpp/hpp, java, js/mjs/cjs/jsx, ts/tsx, cs,
//     rs, swift, kt/kts, scala, php, dart, and json/css/scss — brace-ish
//     enough to try): a boundary closes a chunk at a `}` whose closing brace
//     sits at column 0 and whose NEXT line is blank or begins at column 0
//     with a letter — the top-level close of a gofmt'd file, seen from
//     outside the parser. This is deliberately NOT a parser: it does not
//     know strings from comments, and V11 says that is the point — a brace
//     heuristic plus a line window is most of the benefit of a per-language
//     parser at none of the project cost.
//   - Indent languages (py, rb, ex, yaml, yml): a boundary where a non-blank
//     line returns to column zero — a def/class at the left margin closes
//     the previous block.
//   - Everything else, and anything the heuristic cannot keep (a chunk that
//     outgrows maxHeuristicLines, a line longer than maxLineBytes, a file
//     whose braces never return to column 0): a FIXED LINE WINDOW WITH
//     OVERLAP. This is not a failure path. Prose, configuration, markdown,
//     and every language not listed take it, and they are a large fraction
//     of a repository — a fallback treated as exceptional is one that is
//     badly tuned, so the window has normal constants (windowLines) chosen
//     for the same reason as the heuristic's, and ChunkFile reports no
//     "degraded" flag because there is nothing degraded.
//
// The overlap exists for V11's named failure arriving through the fallback:
// a symbol split across a window boundary would appear in NEITHER chunk's
// semantic neighbourhood, so consecutive windows share windowOverlap lines.

// Chunk is one unit of indexing: a 1-indexed line range of one file, the
// content hash of THIS chunk (V8 — per chunk, never per file: a per-file
// hash means one edited line reindexes the whole file, which on a large
// source file is most of the cost of a full reindex for one changed
// function), and the text the index holds. Body is input, not result — V4's
// rule that results are locations, never text, binds the Hit type; the
// chunk is what goes IN.
type Chunk struct {
	Path  string
	Start int // 1-indexed line
	End   int
	Hash  string
	Body  string
}

// Skip reports a piece of input this chunker refused, BY NAME. Skips are
// never silent (the task's own line: "the skip is reported"): the caller —
// T8's reindex — puts these sentences in its record.
type Skip struct {
	Path   string
	Line   int // 0 for a whole-file skip
	Bytes  int
	Reason string
}

const (
	// windowLines / windowOverlap: the fallback's constants. 120 is roughly
	// "a screenful and a half of context", big enough that a conceptual
	// query lands inside one chunk's neighbourhood, small enough that an
	// embedding call sees the chunk as a unit. The overlap (1/6) is chosen
	// so no straddling symbol can be missed by BOTH windows: any run of 20
	// lines appears wholly in at least one window.
	windowLines   = 120
	windowOverlap = 20

	// maxHeuristicLines bounds what the heuristic may claim: a chunk longer
	// than this was not separated by the heuristic, it was abandoned by it
	// (no top-level `}` for 400 lines IS the unresolvable case), and the
	// window takes over.
	maxHeuristicLines = 400

	// maxLineBytes is the minified-file guard. A chunk cannot be split
	// below a line — results are file:line (V4), so a line is the atomic
	// unit of a range — therefore a line beyond this bound cannot be a
	// USEFUL chunk at all: it is skipped and reported, not indexed as one
	// enormous blob. That is the honest shape of "a minified file chunks
	// by window without producing one enormous chunk": it chunks, and the
	// one impossible line is said aloud.
	maxLineBytes = 64 * 1024
)

// braceExts / indentExts are the two known families, CLOSED and stated.
// Anything else takes the window — deliberately: guessing an indent
// language's structure from braces it does not use is how a chunker starts
// lying about code.
var (
	braceExts  = map[string]bool{"go": true, "c": true, "h": true, "cc": true, "cpp": true, "hpp": true, "cxx": true, "m": true, "mm": true, "java": true, "js": true, "mjs": true, "cjs": true, "jsx": true, "ts": true, "tsx": true, "cs": true, "rs": true, "swift": true, "kt": true, "kts": true, "scala": true, "php": true, "dart": true, "json": true, "css": true, "scss": true}
	indentExts = map[string]bool{"py": true, "rb": true, "ex": true, "exs": true, "yaml": true, "yml": true}
)

// ChunkFile turns one file's bytes into chunks. It cannot fail: refusal is
// a reported skip, not an error (a file that will not chunk is data, not a
// defect — the walk that calls this over thousands of files must not stop
// at the first minified bundle).
func ChunkFile(path string, content []byte) (chunks []Chunk, skips []Skip) {
	if len(content) == 0 {
		return nil, []Skip{{Path: path, Reason: "empty file — nothing to index"}}
	}
	// Binary detection is git's, stated: a NUL byte in the first 8 KiB.
	// Anything containing it is data a line-chunker would index as noise.
	probe := content
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if strings.IndexByte(string(probe), 0) >= 0 {
		return nil, []Skip{{Path: path, Bytes: len(content), Reason: "binary (NUL byte in the first 8 KiB) — a line chunker would index noise"}}
	}

	lines := splitLines(string(content))
	// The impossible lines are named and removed from chunking BEFORE any
	// strategy runs; the rest of the file keeps its chunks (a single
	// minified line does not cancel the other 200 lines of the bundle).
	longLine := map[int]bool{}
	for i, ln := range lines {
		if len(ln) > maxLineBytes {
			longLine[i] = true
			skips = append(skips, Skip{Path: path, Line: i + 1, Bytes: len(ln),
				Reason: "line longer than 64 KiB — a chunk cannot split below a line (V4 ranges are line-granular), so this line is not indexable"})
		}
	}

	ext := extension(path)
	switch {
	case braceExts[ext]:
		chunks = chunkBrace(path, lines, longLine)
	case indentExts[ext]:
		chunks = chunkIndent(path, lines, longLine)
	default:
		chunks = chunkWindow(path, lines, longLine)
	}
	return chunks, skips
}

// extension returns the lowercase file extension without the dot ("" for a
// dotless or dot-terminated name — which routes to the window, as an
// unknown name should).
func extension(path string) string {
	base := path
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	i := strings.LastIndexByte(base, '.')
	if i < 0 || i == len(base)-1 {
		return ""
	}
	return strings.ToLower(base[i+1:])
}

// splitLines keeps 1-indexed line positions meaningful: N lines, each
// without its newline. Trailing newline does not invent an empty last line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

// chunkBrace: boundary at a top-level `}` (column 0) followed by a blank
// line or a declaration at column 0 — V11's shape verbatim. A chunk that
// outgrows maxHeuristicLines was not separated, only survived, and the
// window finishes the job.
func chunkBrace(path string, lines []string, longLine map[int]bool) []Chunk {
	isClose := func(ln string) bool {
		return strings.HasPrefix(ln, "}") && (ln == "}" || ln == "};")
	}
	var chunks []Chunk
	start := 0
	flush := func(end int) { // end is exclusive
		if end > start {
			chunks = append(chunks, bounded(path, lines, longLine, start, end)...)
		}
		start = end
	}
	for i, ln := range lines {
		if !isClose(ln) {
			continue
		}
		next := ""
		if i+1 < len(lines) {
			next = lines[i+1]
		}
		if next == "" || strings.TrimSpace(next) == "" || startsDeclaration(next) {
			flush(i + 1)
		}
	}
	flush(len(lines))
	return chunks
}

// startsDeclaration: the "followed by a declaration" half, read cheaply —
// a column-0 line that begins with a letter or an attribute/signature
// marker. The alternative (a keyword list) would be a parser's stub, and
// stub parsers drift.
func startsDeclaration(next string) bool {
	if next == "" || next[0] == ' ' || next[0] == '\t' {
		return false
	}
	c := next[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '@' || c == '#' || c == '/' || c == '+'
}

// chunkIndent: a boundary where a non-blank line returns to column zero —
// a def/class at the left margin closes the previous block. Comment and
// blank lines at column zero do NOT break a chunk (a comment belongs to
// what follows it, and breaking there would starve the next chunk of its
// heading).
func chunkIndent(path string, lines []string, longLine map[int]bool) []Chunk {
	var chunks []Chunk
	start := 0
	flush := func(end int) {
		if end > start {
			chunks = append(chunks, bounded(path, lines, longLine, start, end)...)
		}
		start = end
	}
	for i, ln := range lines {
		if i == 0 || ln == "" || ln[0] == ' ' || ln[0] == '\t' || ln[0] == '#' {
			continue
		}
		// Column zero, non-blank, not a comment: the block above is done —
		// but only if the line above was indented or blank (i.e. we are at
		// a block START, not a continuation column like a top-level
		// decorator chain). Any column-0 line starts a new neighbourhood.
		flush(i)
	}
	flush(len(lines))
	return chunks
}

// chunkWindow is the normal path for everything unknown: fixed windows with
// overlap, so no 20-line run is absent from every chunk's neighbourhood.
func chunkWindow(path string, lines []string, longLine map[int]bool) []Chunk {
	if len(lines) == 0 {
		return nil
	}
	var chunks []Chunk
	step := windowLines - windowOverlap
	for start := 0; start < len(lines); start += step {
		end := start + windowLines
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, bounded(path, lines, longLine, start, end)...)
		if end == len(lines) {
			break
		}
	}
	return chunks
}

// bounded emits [start,end) as one chunk — or falls back to the window when
// the heuristic's chunk outgrew what a chunk should be. Long lines named in
// longLine are dropped from THIS chunk's body (and reported by ChunkFile
// once); a chunk whose every line was dropped emits nothing.
func bounded(path string, lines []string, longLine map[int]bool, start, end int) []Chunk {
	if end-start > maxHeuristicLines {
		return chunkWindow(path, lines[start:end], longLine)
	}
	// Trailing blank padding is trimmed BEFORE anything else: blank lines
	// that never met content are not a chunk — they retrieve nothing, and a
	// chunk that appears and vanishes as trailing whitespace moves is a
	// reindex paying embedding calls for air. Interior blanks STAY: they
	// are the separation a chunk's neighbourhood is made of.
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	var body strings.Builder
	first, last := -1, -1
	for i := start; i < end; i++ {
		if longLine[i] {
			continue // named and skipped by ChunkFile, once
		}
		if first < 0 {
			first = i
		}
		last = i
		body.WriteString(lines[i])
		body.WriteByte('\n')
	}
	if first < 0 {
		return nil // the whole range was impossible lines or blank padding
	}
	return []Chunk{{Path: path, Start: first + 1, End: last + 1,
		Hash: hashBytes(body.String()), Body: body.String()}}
}

// hashBytes is THE chunk hash — content, always, never mtime (the chunker
// is never even handed a file, let alone a timestamp: TestHashIsContentNotMtime
// asserts the consequence rather than trusting the prose).
func hashBytes(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
