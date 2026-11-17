//go:build !noretrieval

package retrieval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chunk_test.go — T2's gates. The one that decides whether V8's incremental
// reindex is actually incremental is TestHashPerChunkNotPerFile; the per-file
// version passes every other test here, so that test gets the loudest assert.

// chunkText chunks a named file's text — the shape callers have (read bytes,
// chunk them), not a convenience that hides the contract.
func chunkText(t *testing.T, path, text string) ([]Chunk, []Skip) {
	t.Helper()
	return ChunkFile(path, []byte(text))
}

const goTwoFuncs = `package main

func alpha() int {
	return 1
}

func beta() int {
	return 2
}
`

// TestChunkDoesNotSpanTwoSymbols is V11's stated failure, asserted directly:
// "a chunk spanning two functions retrieves for neither" — so for EACH
// supported language shape, two adjacent top-level definitions must land in
// two chunks, and no chunk may name both.
func TestChunkDoesNotSpanTwoSymbols(t *testing.T) {
	shapes := []struct {
		name, path, text string
		first, second    string
	}{
		{"brace/go", "a.go", goTwoFuncs, "func alpha", "func beta"},
		{"brace/js", "a.js", "function alpha() {\n  return 1;\n}\n\nfunction beta() {\n  return 2;\n}\n", "function alpha", "function beta"},
		{"brace/c", "a.c", "int alpha(void) {\n  return 1;\n}\n\nint beta(void) {\n  return 2;\n}\n", "int alpha", "int beta"},
		{"indent/python", "a.py", "def alpha():\n    return 1\n\n\ndef beta():\n    return 2\n", "def alpha", "def beta"},
		{"indent/ruby", "a.rb", "def alpha\n  1\nend\n\ndef beta\n  2\nend\n", "def alpha", "def beta"},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			chunks, skips := chunkText(t, sh.path, sh.text)
			if len(skips) != 0 {
				t.Fatalf("the two-function file was skipped: %+v", skips)
			}
			if len(chunks) < 2 {
				t.Fatalf("%d chunk for two adjacent definitions — V11 named this failure: a chunk spanning two functions retrieves for NEITHER (chunks: %+v)", len(chunks), chunks)
			}
			for _, ch := range chunks {
				if strings.Contains(ch.Body, sh.first) && strings.Contains(ch.Body, sh.second) {
					t.Fatalf("chunk %s:%d-%d spans both %q and %q — the exact failure V11 exists to prevent", sh.path, ch.Start, ch.End, sh.first, sh.second)
				}
			}
			// And each symbol is somewhere — separation that loses a symbol
			// is worse than a spanning chunk.
			for _, sym := range []string{sh.first, sh.second} {
				found := false
				for _, ch := range chunks {
					if strings.Contains(ch.Body, sym) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("%q appeared in no chunk — the chunker separated and lost", sym)
				}
			}
		})
	}
}

// TestFallbackIsALineWindow: an unknown extension, prose, and a file the
// heuristic cannot resolve ALL chunk by window — and this test treats that
// as normal, not as a degraded path (the task's own words). "Normal" is
// asserted structurally: the same ChunkFile answers, no skip is reported,
// and the window's shape (bounded line spans) is what comes back.
func TestFallbackIsALineWindow(t *testing.T) {
	cases := []struct{ name, path, text string }{
		{"unknown extension", "data.foo", strings.Repeat("some configuration value line\n", 300)},
		{"prose", "notes.md", "The design says a chunk spanning two functions retrieves for neither.\n\n" + strings.Repeat("Another paragraph line about retrieval and indexing.\n", 200)},
		{"brace file that never resolves", "weird.go", "package main\n\nvar table = map[string]string{\n" + strings.Repeat("\t\"k\": \"v\",\n", 500) + "}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, skips := chunkText(t, tc.path, tc.text)
			if len(skips) != 0 {
				t.Fatalf("the window path reported skips %v — prose and unknown files are NOT exceptional (V11)", skips)
			}
			if len(chunks) == 0 {
				t.Fatal("the fallback produced no chunks at all")
			}
			for _, ch := range chunks {
				if span := ch.End - ch.Start + 1; span > windowLines {
					t.Fatalf("fallback chunk %s:%d-%d spans %d lines, more than the window (%d) — the heuristic claiming unbounded lines is the drift V11 warned about", tc.path, ch.Start, ch.End, span, windowLines)
				}
			}
		})
	}
}

// TestWindowsOverlap: a symbol straddling a window boundary must appear,
// WHOLE, inside at least one chunk's range — the same failure V11 named,
// arriving through the fallback instead of the heuristic.
func TestWindowsOverlap(t *testing.T) {
	var b strings.Builder
	symStart := windowLines - 5 // straddles the first boundary at windowLines
	symEnd := symStart + 10
	for i := 1; i <= 4*windowLines; i++ {
		if i == symStart {
			b.WriteString("STRADDLING-SYMBOL-BEGIN\n")
		} else if i >= symStart && i <= symEnd {
			b.WriteString(fmt.Sprintf("  part of the symbol line %d\n", i))
		} else if i == symEnd+1 {
			b.WriteString("STRADDLING-SYMBOL-END\n")
		} else {
			b.WriteString(fmt.Sprintf("filler line %d for neighbourhood\n", i))
		}
	}
	chunks, _ := chunkText(t, "filler.txt", b.String())
	coverStart, coverEnd := symStart, symEnd+1
	for _, ch := range chunks {
		if ch.Start <= coverStart && ch.End >= coverEnd {
			return // whole symbol inside one chunk's neighbourhood
		}
	}
	t.Fatalf("the symbol at lines %d–%d straddled every window that could hold it — overlap (%d of %d) is not doing its job: %+v", coverStart, coverEnd, windowOverlap, windowLines, ranges(chunks))
}

func ranges(chunks []Chunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, fmt.Sprintf("%d-%d", c.Start, c.End))
	}
	return out
}

// TestHashPerChunkNotPerFile is THE test (V8): editing one function changes
// ONE chunk's hash; the rest are unchanged. A per-file hash passes every
// other test in this file and fails ONLY this one — which is why it exists.
func TestHashPerChunkNotPerFile(t *testing.T) {
	src := "package main\n\nfunc alpha() int {\n\treturn 1\n}\n\nfunc beta() int {\n\treturn 2\n}\n\nfunc gamma() int {\n\treturn 3\n}\n"
	before, _ := chunkText(t, "three.go", src)
	if len(before) < 3 {
		t.Fatalf("%d chunks for three separated functions — the comparison below needs one chunk per function", len(before))
	}
	edited := strings.Replace(src, "\treturn 2\n", "\treturn 22 // edited\n", 1)
	after, _ := chunkText(t, "three.go", edited)

	byRange := func(cs []Chunk) map[string]string {
		m := map[string]string{}
		for _, c := range cs {
			m[fmt.Sprintf("%d-%d", c.Start, c.End)] = c.Hash
		}
		return m
	}
	b, a := byRange(before), byRange(after)
	if len(a) != len(b) {
		t.Fatalf("the one-line edit changed the CHUNK COUNT (%d → %d) — ranges shifted, so 'which chunks are stale' would rewrite the file", len(b), len(a))
	}
	changed := []string{}
	for r, h := range b {
		if a[r] != h {
			changed = append(changed, r)
		}
	}
	if len(changed) != 1 {
		t.Fatalf("%d chunks changed hash for a one-function edit (%v) — that is a PER-FILE hash wearing a per-chunk costume; V8's incrementality dies here", len(changed), changed)
	}
	// And the one that changed IS beta's chunk.
	if !strings.Contains(chunkAt(after, changed[0]).Body, "beta") {
		t.Fatalf("the changed chunk (%s) is not beta's — the hash moved for a chunk nobody edited", changed[0])
	}
}

func chunkAt(cs []Chunk, r string) Chunk {
	for _, c := range cs {
		if fmt.Sprintf("%d-%d", c.Start, c.End) == r {
			return c
		}
	}
	return Chunk{}
}

// TestHashIsContentNotMtime: touching a file without changing it changes no
// hash. The chunker never receives a timestamp — this asserts the
// consequence through the door callers actually use (read, touch, re-read).
func TestHashIsContentNotMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stamped.go")
	if err := os.WriteFile(path, []byte(goTwoFuncs), 0o600); err != nil {
		t.Fatal(err)
	}
	first, _ := chunkText(t, path, mustRead(t, path))
	old := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	second, _ := chunkText(t, path, mustRead(t, path))
	if len(first) != len(second) {
		t.Fatalf("touching the file changed the chunk count (%d → %d)", len(first), len(second))
	}
	for i := range first {
		if first[i].Hash != second[i].Hash {
			t.Fatalf("touching the file changed chunk %d's hash — V8's hash is content, and mtime in the seam is how a reindex burns itself re-embedding unchanged chunks every scan", i)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestChunkBoundsAreLineRanges: Start and end are 1-indexed lines, since
// V4's results are file:line — line 1 exists, Start <= End, and the ranges
// meet the file's real first line.
func TestChunkBoundsAreLineRanges(t *testing.T) {
	chunks, _ := chunkText(t, "bounds.go", goTwoFuncs)
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	for _, ch := range chunks {
		if ch.Start < 1 || ch.End < ch.Start {
			t.Fatalf("chunk %s:%d-%d is not a 1-indexed line range (V4 results are file:line)", ch.Path, ch.Start, ch.End)
		}
		if n := strings.Count(ch.Body, "\n"); n == 0 && ch.End > ch.Start {
			t.Fatalf("chunk %s:%d-%d holds %d lines of text — Body and range disagree", ch.Path, ch.Start, ch.End, n)
		}
	}
	if chunks[0].Start != 1 {
		t.Fatalf("the first chunk starts at %d — line 1 exists (1-indexed)", chunks[0].Start)
	}
}

// TestVeryLongLineDoesNotBreakChunking: a minified file chunks by window
// without producing one enormous chunk. The honest shape: a chunk cannot
// split below a line (ranges are line-granular, V4), so the ONE impossible
// line is skipped and REPORTED while the file's other lines still chunk.
func TestVeryLongLineDoesNotBreakChunking(t *testing.T) {
	// (a) many long-ish lines, unknown language: windowed normally.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString(strings.Repeat("x", 400) + "\n")
	}
	chunks, skips := chunkText(t, "bundle.foo", b.String())
	if len(chunks) == 0 || len(skips) != 0 {
		t.Fatalf("400-byte lines (%d chunks, %d skips) should be ordinary window material", len(chunks), len(skips))
	}
	for _, ch := range chunks {
		if len(ch.Body) > maxLineBytes {
			t.Fatalf("chunk %s:%d-%d is %d bytes — an enormous chunk escaped the bound", ch.Path, ch.Start, ch.End, len(ch.Body))
		}
	}

	// (b) the genuinely impossible line: skipped BY NAME, not blobbed.
	minified := "var a = " + strings.Repeat("1+1", 40000) + ";\n"
	chunks, skips = chunkText(t, "min.js", minified)
	if len(chunks) != 0 {
		t.Fatalf("a %d-byte single line became %d chunks — one enormous chunk is exactly what this test forbids", len(minified), len(chunks))
	}
	if len(skips) != 1 || !strings.Contains(skips[0].Reason, "64 KiB") || skips[0].Line != 1 {
		t.Fatalf("the impossible line was not skipped by name (%+v) — a skip that is not reported is a silent one", skips)
	}
}

// TestEmptyAndBinaryFilesAreSkipped — and the skip is reported, not silent:
// each skip names its path and says why.
func TestEmptyAndBinaryFilesAreSkipped(t *testing.T) {
	chunks, skips := chunkText(t, "empty.go", "")
	if len(chunks) != 0 || len(skips) != 1 || !strings.Contains(skips[0].Reason, "empty") {
		t.Fatalf("empty file: %d chunks, skips %+v — expected one reported empty skip", len(chunks), skips)
	}
	if skips[0].Path != "empty.go" {
		t.Fatalf("skip does not name its path: %+v", skips[0])
	}

	binary := []byte("package main\n\x00\x01\x02binary noise\n")
	chunks, skips = ChunkFile("blob.go", binary)
	if len(chunks) != 0 || len(skips) != 1 || !strings.Contains(skips[0].Reason, "binary") {
		t.Fatalf("binary file: %d chunks, skips %+v — expected one reported binary skip", len(chunks), skips)
	}
	if !strings.Contains(skips[0].Reason, "NUL") {
		t.Fatalf("the binary skip does not state the mechanism (NUL in the first 8 KiB — git's rule, stated): %+v", skips[0])
	}
}
