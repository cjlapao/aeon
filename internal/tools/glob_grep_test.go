package tools

// T15's tests: the two search tools behind the one resolver, with caps that
// never truncate silently and results that never become implicit reads.
//
// The behavioural half (escapes, caps, skip notes) is table-driven through
// BuildToolset — the same seam a step uses. The structural half
// (TestGlobAndGrepShareTheOnePathCheck) reads the package sources, because
// "path-enforced through the one resolver" is a claim about the code, not
// just about the outputs: a second resolver could pass every behavioural
// test until the day its weaker arm is the one reached.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// globInput/grepInput build the arguments, path included only when set.
func globInput(pattern, path string) json.RawMessage {
	m := map[string]any{"pattern": pattern}
	if path != "" {
		m["path"] = path
	}
	raw, _ := json.Marshal(m)
	return raw
}

func grepInput(pattern, path string, extra map[string]any) json.RawMessage {
	m := map[string]any{"pattern": pattern}
	if path != "" {
		m["path"] = path
	}
	for k, v := range extra {
		m[k] = v
	}
	raw, _ := json.Marshal(m)
	return raw
}

// splitResultContent separates header (plus any note line) from the body.
// The renderer joins header + optional "\n[skipped …]" + "\n\n" + body.
func splitResultContent(t *testing.T, content string) (string, []string) {
	t.Helper()
	head, body, found := strings.Cut(content, "\n\n")
	if !found {
		t.Fatalf("the result has no body separator:\n%s", content)
	}
	if body == "" {
		return head, nil
	}
	return head, strings.Split(body, "\n")
}

// TestGlobAndGrepRefuseTheSameBadPaths is the behavioural half of the one
// enforcement point for the search tools: the three escape classes —
// absolute path, `..`, escaping symlink — are refused for BOTH tools, and
// each refusal text comes from the resolver itself (the same Reason
// substring that read/list/write/edit emit for the same input), not from a
// private check in either tool.
func TestGlobAndGrepRefuseTheSameBadPaths(t *testing.T) {
	root := workspace(t, map[string]string{"notes/seed.txt": "needle"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "needle.txt"), []byte("needle OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"absolute", filepath.Join(outside, "needle.txt")},
		{"traversal", "../outside.txt"},
		{"symlink-escape", "escape/needle.txt"},
		{"symlink-escape-root", "escape"},
	}

	for _, tool := range []string{"glob", "grep"} {
		set, err := BuildToolset([]string{tool}, ToolContext{WorkspaceRoot: root})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		for _, tc := range cases {
			t.Run(tool+"/"+tc.name, func(t *testing.T) {
				want := Resolve(root, tc.path)
				if want.OK {
					t.Fatalf("the resolver itself accepted %q — the table tests nothing", tc.path)
				}
				var in json.RawMessage
				if tool == "glob" {
					in = globInput("**", tc.path)
				} else {
					in = grepInput("needle", tc.path, nil)
				}
				got := set.Call(context.Background(), tool, in)
				if !got.IsError {
					t.Fatalf("%s accepted %q: %+v", tool, tc.path, got)
				}
				if !strings.Contains(got.Content, want.Reason) {
					t.Errorf("%s did not carry the resolver's reason for %q.\n got: %s\nwant substring: %s",
						tool, tc.path, got.Content, want.Reason)
				}
				// "Identical refusal" is checked against the tool that has
				// always had it: the same resolver sentence read.go emits.
				rset, err := BuildToolset([]string{"read"}, ToolContext{WorkspaceRoot: root})
				if err != nil {
					t.Fatal(err)
				}
				r := rset.Call(context.Background(), "read", json.RawMessage(fmt.Sprintf(`{"path":%q}`, tc.path)))
				if !r.IsError || !strings.Contains(r.Content, want.Reason) {
					t.Errorf("read does not share the refusal for %q: %+v", tc.path, r)
				}
			})
		}
	}
}

// TestGlobMatchingRules drives the tool's OWN syntax — `**` across
// directories, `*` within one segment, `?`, classes, the basename rule — and
// the walker's skip rules (.git, symlinks, directories-are-not-results).
func TestGlobMatchingRules(t *testing.T) {
	root := workspace(t, map[string]string{
		"a.go":             "x",
		"b.go":             "x",
		"docs/b.go":        "x",
		"docs/sub/c.go":    "x",
		"readme.md":        "x",
		".hidden.go":       "x",
		"deep/a/b/main.go": "x",
		"deep/a/b/b.go":    "x",
		".git/hook.go":     "x",
	})
	// A symlink inside the workspace to an ordinary file: never followed,
	// never returned (the regular-file filter is doing real work).
	if err := os.Symlink(filepath.Join(root, "a.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		pattern string
		want    []string
	}{
		{"*.go", []string{".hidden.go", "a.go", "b.go", "deep/a/b/b.go", "deep/a/b/main.go", "docs/b.go", "docs/sub/c.go"}},
		{"docs/*.go", []string{"docs/b.go"}},
		{"**/*.go", []string{".hidden.go", "a.go", "b.go", "deep/a/b/b.go", "deep/a/b/main.go", "docs/b.go", "docs/sub/c.go"}},
		{"deep/a/b/*.go", []string{"deep/a/b/b.go", "deep/a/b/main.go"}},
		{"deep/**/b.go", []string{"deep/a/b/b.go"}},
		{"?ain.go", []string{"deep/a/b/main.go"}},
		{"[ab].go", []string{"a.go", "b.go", "deep/a/b/b.go", "docs/b.go"}}, // basename rule: every file NAMED a.go/b.go
		{"readme.md", []string{"readme.md"}},
		{"**", []string{".hidden.go", "a.go", "b.go", "deep/a/b/b.go", "deep/a/b/main.go", "docs/b.go", "docs/sub/c.go", "readme.md"}},
	}
	set, err := BuildToolset([]string{"glob"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			got := set.Call(context.Background(), "glob", globInput(tc.pattern, ""))
			if got.IsError {
				t.Fatalf("refused: %s", got.Content)
			}
			header, lines := splitResultContent(t, got.Content)
			if strings.Contains(header, "truncated") || strings.Contains(header, "cut at") {
				t.Fatalf("unexpected truncation: %s", header)
			}
			sort.Strings(lines)
			if strings.Join(lines, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("paths =\n%v\nwant\n%v", lines, tc.want)
			}
			for _, l := range lines {
				if strings.HasSuffix(l, "/") {
					t.Errorf("directories are not results, got %q", l)
				}
			}
		})
	}

	// `.git` is the skipped tree: the workspace above has .git/hook.go, and
	// the "*.go" case did NOT list it. Assert it directly too, from root.
	got := set.Call(context.Background(), "glob", globInput("**hook.go", ""))
	if got.IsError || strings.Contains(got.Content, ".git") {
		t.Errorf(".git tree leaked into results: %+v", got)
	}

	// The default path is the workspace root (no `path` argument at all) —
	// every case above exercised it; the explicit "." must answer the same:
	withExplicit := set.Call(context.Background(), "glob", globInput("readme.md", "."))
	if withExplicit.IsError {
		t.Errorf("explicit \".\" refused: %s", withExplicit.Content)
	}
}

// TestGlobCapReportsTrueCount is the count-cap test: N+50 matches, and the
// output states the TRUE N+50 — truncation is never silent (D9).
func TestGlobCapReportsTrueCount(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < GlobMaxResults+50; i++ {
		files[fmt.Sprintf("f%04d.txt", i)] = "x"
	}
	root := workspace(t, files)
	set, err := BuildToolset([]string{"glob"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "glob", globInput("f*.txt", ""))
	if got.IsError {
		t.Fatalf("refused: %s", got.Content)
	}
	header, lines := splitResultContent(t, got.Content)
	wantHeader := fmt.Sprintf("showing %d of %d paths (truncated at %d; narrow the pattern)",
		GlobMaxResults, GlobMaxResults+50, GlobMaxResults)
	if !strings.Contains(header, wantHeader) {
		t.Errorf("header = %q\nwant substring: %s", header, wantHeader)
	}
	if len(lines) != GlobMaxResults {
		t.Errorf("returned %d lines, want exactly the cap %d", len(lines), GlobMaxResults)
	}
	if !strings.Contains(got.Summary, fmt.Sprintf("showing %d of %d", GlobMaxResults, GlobMaxResults+50)) {
		t.Errorf("summary hides the true size: %s", got.Summary)
	}
}

// TestGlobByteCapReportsTrueSize proves the SECOND cap is real: names long
// enough that the count cap never fires but the rendered string exceeds the
// byte ceiling — and the true count is still reported.
func TestGlobByteCapReportsTrueSize(t *testing.T) {
	const n = 300
	files := map[string]string{}
	for i := 0; i < n; i++ {
		name := strings.Repeat("n", 240) + fmt.Sprintf("%03d", i) // 243 bytes, under the 255 name limit
		files[name] = "x"
	}
	root := workspace(t, files)
	set, err := BuildToolset([]string{"glob"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "glob", globInput("n*", ""))
	if got.IsError {
		t.Fatalf("refused: %s", got.Content)
	}
	header, lines := splitResultContent(t, got.Content)
	if !strings.Contains(header, fmt.Sprintf("of %d paths (rendered output cut at %d bytes", n, GlobMaxBytes)) {
		t.Errorf("header does not report the byte cut with the true size: %q", header)
	}
	if len(lines) >= n {
		t.Errorf("the byte cap did not cut: %d lines", len(lines))
	}
	if len(strings.Join(lines, "")) > GlobMaxBytes {
		t.Errorf("the rendered body exceeds the byte cap")
	}
}

// TestGlobRefusalsAndEdges covers the refusals that name themselves.
func TestGlobRefusalsAndEdges(t *testing.T) {
	root := workspace(t, map[string]string{"a.go": "x", "ro/deep/f.go": "x"})
	ro := filepath.Join(root, "locked")
	if err := os.MkdirAll(filepath.Join(ro, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ro, "sub", "g.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(ro, 0o755); err != nil {
			t.Errorf("restore: %v", err)
		}
	})

	set, err := BuildToolset([]string{"glob"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		input   json.RawMessage
		wantSub string
	}{
		{"bad-json", json.RawMessage(`{"pattern":`), "not valid JSON"},
		{"empty-pattern", globInput("", ""), "a pattern is required"},
		{"bad-syntax", globInput("[", ""), "not valid tool-glob syntax"},
		{"bad-syntax-names-pattern", globInput("[", ""), `"["`},
		{"empty-segment", globInput("a//b", ""), "not valid tool-glob syntax"},
		{"missing-dir", globInput("**", "nope"), "could not find nope"},
		{"file-not-dir", globInput("**", "a.go"), "glob walks directories"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := set.Call(context.Background(), "glob", tc.input)
			if !got.IsError || !strings.Contains(got.Content, tc.wantSub) {
				t.Fatalf("got %+v, want refusal containing %q", got, tc.wantSub)
			}
		})
	}

	// An unreadable directory is skipped and COUNTED, never a crash.
	got := set.Call(context.Background(), "glob", globInput("**", ""))
	if got.IsError {
		t.Fatalf("the unreadable directory crashed the walk: %s", got.Content)
	}
	if !strings.Contains(got.Content, "unreadable directories") {
		t.Errorf("the skip is not counted in the output: %s", got.Content)
	}
	if strings.Contains(got.Content, "locked/sub/g.go") {
		t.Errorf("walked into a directory the process cannot read")
	}

	// Cancellation is honoured like every other tool's.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = set.Call(ctx, "glob", globInput("**", ""))
	if !got.IsError || !strings.Contains(got.Content, "cancelled") {
		t.Errorf("cancelled glob: %+v", got)
	}

	// Empty tree answers with its own state, not a fake "no matches".
	empty := workspace(t, nil)
	eset, _ := BuildToolset([]string{"glob"}, ToolContext{WorkspaceRoot: empty})
	got = eset.Call(context.Background(), "glob", globInput("**", ""))
	if got.IsError || !strings.Contains(got.Content, "no files to search") {
		t.Errorf("empty workspace: %+v", got)
	}
}

// TestGrepHappyRendering pins the hit format (`rel/path:line: text`, the
// walker's rel), the honest header, the glob filter, ignore_case, and the
// refusals that name their inputs.
func TestGrepHappyRendering(t *testing.T) {
	root := workspace(t, map[string]string{
		"src/main.go":       "package main\nfunc needle() {}\n",
		"src/sub/other.txt": "needle here\n",
		"notes.md":          "NEEDLE shout\n",
	})
	set, err := BuildToolset([]string{"grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}

	got := set.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if got.IsError {
		t.Fatalf("refused: %s", got.Content)
	}
	header, lines := splitResultContent(t, got.Content)
	// Case-SENSITIVE by default: 2 matches (NEEDLE in notes.md stays put).
	if !strings.Contains(header, "— 2 matches in 3 files scanned") {
		t.Errorf("header = %q, want the honest count \"— 2 matches in 3 files scanned\"", header)
	}
	wantLines := []string{"src/main.go:2: func needle() {}", "src/sub/other.txt:1: needle here"}
	if strings.Join(lines, "\n") != strings.Join(wantLines, "\n") {
		t.Errorf("hits =\n%v\nwant\n%v", lines, wantLines)
	}

	// The glob filter restricts which files are searched (tool syntax).
	got = set.Call(context.Background(), "grep", grepInput("needle", "", map[string]any{"glob": "src/*.go"}))
	if got.IsError {
		t.Fatalf("filtered grep refused: %s", got.Content)
	}
	_, lines = splitResultContent(t, got.Content)
	if len(lines) != 1 || lines[0] != "src/main.go:2: func needle() {}" {
		t.Errorf("glob filter hits = %v", lines)
	}

	// ignore_case is (?i) semantics, not a hand-lowercase — the TEXT is
	// rendered as it stands in the file.
	got = set.Call(context.Background(), "grep", grepInput("needle", "", map[string]any{"ignore_case": true}))
	header, lines = splitResultContent(t, got.Content)
	if !strings.Contains(header, "3 matches") || len(lines) != 3 {
		t.Errorf("ignore_case: header=%q lines=%v", header, lines)
	}
	if !strings.Contains(got.Content, "notes.md:1: NEEDLE shout") {
		t.Errorf("ignore_case rewrote the matched text: %s", got.Content)
	}

	cases := []struct {
		name    string
		input   json.RawMessage
		wantSub string
	}{
		{"bad-json", json.RawMessage(`{"pattern":`), "not valid JSON"},
		{"empty-pattern", grepInput("", "", nil), "a pattern is required"},
		{"bad-regexp-names-pattern", grepInput("func(", "", nil), "not a valid Go regular expression"},
		{"bad-regexp-quotes-pattern", grepInput("func(", "", nil), `"func("`},
		{"bad-glob-names-filter", grepInput("x", "", map[string]any{"glob": "["}), "glob filter"},
		{"missing-dir", grepInput("x", "nope", nil), "could not find nope"},
		{"file-not-dir", grepInput("x", "src/main.go", nil), "grep searches directories"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := set.Call(context.Background(), "grep", tc.input)
			if !got.IsError || !strings.Contains(got.Content, tc.wantSub) {
				t.Fatalf("got %+v, want refusal containing %q", got, tc.wantSub)
			}
		})
	}

	// The zero-match answer says what it scanned (three-state honesty).
	got = set.Call(context.Background(), "grep", grepInput("zzzznomatch", "", nil))
	if got.IsError {
		t.Fatalf("zero matches is not an error: %s", got.Content)
	}
	if !strings.Contains(got.Content, "no matches (3 files scanned)") {
		t.Errorf("zero-match honesty: %s", got.Content)
	}

	// Cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = set.Call(ctx, "grep", grepInput("needle", "", nil))
	if !got.IsError || !strings.Contains(got.Content, "cancelled") {
		t.Errorf("cancelled grep: %+v", got)
	}

	// An unreadable directory is counted, never fatal.
	ro := filepath.Join(root, "locked")
	if err := os.MkdirAll(filepath.Join(ro, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ro, "sub", "n.go"), []byte("needle locked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(ro, 0o755); err != nil {
			t.Errorf("restore: %v", err)
		}
	})
	got = set.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if got.IsError || !strings.Contains(got.Content, "unreadable directories") {
		t.Errorf("locked directory not counted as skipped: %+v", got)
	}
	if strings.Contains(got.Content, "locked/sub/n.go") {
		t.Errorf("read into a locked directory")
	}
}

// TestGrepCapReportsTrueCount: N+50 matches; the header reports the TRUE
// N+50 and the truncation names the cap.
func TestGrepCapReportsTrueCount(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < GrepMaxMatches+50; i++ {
		fmt.Fprintf(&sb, "line %d needle\n", i)
	}
	root := workspace(t, map[string]string{"many.txt": sb.String()})
	set, err := BuildToolset([]string{"grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if got.IsError {
		t.Fatalf("refused: %s", got.Content)
	}
	header, lines := splitResultContent(t, got.Content)
	wantHeader := fmt.Sprintf("showing %d of %d matches (truncated at %d; narrow the pattern)",
		GrepMaxMatches, GrepMaxMatches+50, GrepMaxMatches)
	if !strings.Contains(header, wantHeader) {
		t.Errorf("header = %q\nwant substring: %s", header, wantHeader)
	}
	if len(lines) != GrepMaxMatches {
		t.Errorf("returned %d hits, want exactly the cap %d", len(lines), GrepMaxMatches)
	}
	if !strings.Contains(got.Summary, fmt.Sprintf("showing %d of %d", GrepMaxMatches, GrepMaxMatches+50)) {
		t.Errorf("summary hides the true size: %s", got.Summary)
	}
}

// TestGrepLineAndByteCaps proves the per-line cap and the block cap with
// their true sizes: lines too long to render whole (line cap, true length
// named) in a quantity that pushes the block past the byte ceiling (block
// cap) WITHOUT reaching the match cap — so both messages are reachable.
func TestGrepLineAndByteCaps(t *testing.T) {
	const n = 150 // under GrepMaxMatches: only the byte cap can fire
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString("HIT")
		sb.WriteString(strings.Repeat("y", 700))
		sb.WriteString("\n")
	}
	root := workspace(t, map[string]string{"long.txt": sb.String()})
	set, err := BuildToolset([]string{"grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "grep", grepInput("HIT", "", nil))
	if got.IsError {
		t.Fatalf("refused: %s", got.Content)
	}
	header, lines := splitResultContent(t, got.Content)
	if !strings.Contains(header, fmt.Sprintf("of %d matches (rendered output cut at %d bytes", n, GrepMaxBytes)) {
		t.Errorf("header does not report the byte cut with the true size: %q", header)
	}
	if len(lines) >= n {
		t.Errorf("the byte cap did not cut: %d lines", len(lines))
	}
	joined := strings.Join(lines, "")
	if len(joined) > GrepMaxBytes {
		t.Errorf("the rendered body exceeds the byte cap")
	}
	// The per-line cap names the line's TRUE length (D9, per line).
	if !strings.Contains(joined, fmt.Sprintf("line cut at %d bytes; the line is %d bytes", GrepMaxLineBytes, 3+700)) {
		t.Errorf("the line cap does not report the true line length: %.200s", joined)
	}
}

// TestGrepBinaryAndUnreadableSkipsAreCounted pins the skip rules: NUL in the
// first 512 bytes is binary (skipped and counted, content never grepped);
// an unreadable file is skipped with a counted note, never a crash — and a
// zero-match answer over skipped material says BOTH, so "no matches" can
// never be read as proof of absence.
func TestGrepBinaryAndUnreadableSkipsAreCounted(t *testing.T) {
	root := workspace(t, map[string]string{
		"bin.dat":      "needle\x00more binary",
		"unread.txt":   "needle in the unreadable",
		"ok.txt":       "nothing here",
		"lock/hid.txt": "needle in the locked dir",
	})
	if err := os.Chmod(filepath.Join(root, "unread.txt"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(filepath.Join(root, "unread.txt"), 0o644); err != nil {
			t.Errorf("restore: %v", err)
		}
	})
	if err := os.Chmod(filepath.Join(root, "lock"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(filepath.Join(root, "lock"), 0o755); err != nil {
			t.Errorf("restore: %v", err)
		}
	})

	set, err := BuildToolset([]string{"grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if got.IsError {
		t.Fatalf("a skip must never be a crash: %s", got.Content)
	}
	if strings.Contains(got.Content, "in the unreadable") || strings.Contains(got.Content, "in the locked") ||
		strings.Contains(got.Content, "more binary") {
		t.Errorf("skipped content leaked into results: %s", got.Content)
	}
	if !strings.Contains(got.Content, "no matches (3 files scanned)") {
		t.Errorf("scanned count wrong (bin.dat+unread.txt+ok.txt = 3 opened): %s", got.Content)
	}
	for _, want := range []string{"skipped 1 binary files", "1 unreadable files", "1 unreadable directories"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the skip note omits %q: %s", want, got.Content)
		}
	}
}

// TestResultsNeverBecomeUnresolvedReads — the spec-named invariant. A path
// appearing in a RESULT is information, never an implicit read. Asserted
// three ways: (a) by the tools' ARGUMENT SCHEMAS — neither tool takes a
// content-returning path argument that bypasses the resolver; (b)
// structurally — the resolver is called exactly once per tool, on the
// caller-supplied `path` alone, and grep's file reads are walker-produced
// paths only; (c) behaviourally — content behind a symlink (which the
// caller could NOT resolve) is never read, never listed, and passing the
// escaped path as `path` gets the resolver's own refusal.
func TestResultsNeverBecomeUnresolvedReads(t *testing.T) {
	// (a) The argument schemas, exhaustively.
	schemas := map[string]struct {
		props    []string
		required []string
	}{
		"glob": {props: []string{"path", "pattern"}, required: []string{"pattern"}},
		"grep": {props: []string{"glob", "ignore_case", "path", "pattern"}, required: []string{"pattern"}},
	}
	for name, want := range schemas {
		tool, ok := Registry()[name]
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		var schema struct {
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             []string                   `json:"required"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
		}
		if err := json.Unmarshal(tool.Spec.Parameters, &schema); err != nil {
			t.Fatalf("%s parameters: %v", name, err)
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("%s lets a model invent an argument", name)
		}
		var props []string
		for p := range schema.Properties {
			props = append(props, p)
		}
		sort.Strings(props)
		sort.Strings(want.required)
		if strings.Join(props, ",") != strings.Join(want.props, ",") {
			t.Errorf("%s properties = %v, want exactly %v — an extra argument is a candidate bypass", name, props, want.props)
		}
		if strings.Join(schema.Required, ",") != strings.Join(want.required, ",") {
			t.Errorf("%s required = %v, want %v", name, schema.Required, want.required)
		}
	}

	// (b) Structurally: one resolver call per tool, on p.Path; glob reads
	// no file content at all; grep reads ONLY via grepOne after resolution.
	sources := packageSources(t)
	for _, name := range []string{"glob.go", "grep.go"} {
		src, ok := sources[name]
		if !ok {
			t.Fatalf("%s is missing from the package", name)
		}
		const resolverCall = "Resolve(tc.WorkspaceRoot,"
		if n := strings.Count(src, resolverCall); n != 1 {
			t.Errorf("%s references the resolver %d times; exactly one reference is allowed", name, n)
		}
		if !strings.Contains(src, "Resolve(tc.WorkspaceRoot, p.Path)") {
			t.Errorf("%s does not resolve the caller-supplied `path` argument itself", name)
		}
		if n := strings.Count(src, "Resolve("); n != 1 {
			t.Errorf("%s mentions the resolver %d times; there is no second path-enforcement point here", name, n)
		}
		for _, private := range []string{"func Resolve", "func resolve", "filepath.EvalSymlinks",
			"filepath.Join(", "filepath.Clean(", "filepath.Rel(", "os.Getenv", "os.Getwd"} {
			if strings.Contains(src, private) {
				t.Errorf("%s carries private path handling (%s); the resolver stays in paths.go", name, private)
			}
		}
		resAt := strings.Index(src, resolverCall)
		for _, opener := range []string{"os.Open", "os.ReadFile"} {
			idx := 0
			for {
				at := strings.Index(src[idx:], opener)
				if at < 0 {
					break
				}
				abs := idx + at
				if abs < resAt {
					t.Errorf("%s opens the filesystem (%s) BEFORE the resolver call", name, opener)
				}
				idx = abs + len(opener)
			}
		}
	}
	if src := sources["glob.go"]; strings.Contains(src, "os.Open") || strings.Contains(src, "os.ReadFile") {
		t.Error("glob.go reads file content; glob returns PATHS ONLY")
	}

	// (c) Behaviourally: content reachable only through a symlink the
	// caller could not resolve is invisible to both tools.
	root := workspace(t, map[string]string{"notes/seed.txt": "needle"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "leak.txt"), []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "leak.txt"), filepath.Join(root, "leak-link.txt")); err != nil {
		t.Fatal(err)
	}

	gset, err := BuildToolset([]string{"glob", "grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	// Non-vacuity control: the hit pipeline DOES render `rel:line: text`
	// for files the walker can read — so the absence assertions below are
	// evidence, not a format that could never appear.
	control := gset.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if !strings.Contains(control.Content, "notes/seed.txt:1: needle") {
		t.Fatalf("control grep does not render hits; the absence checks would pass forever: %s", control.Content)
	}
	// A symlink to EXTERNAL content is not searched (no implicit read) and
	// not listed (the walker produces results from real in-workspace files).
	// NOTE: the header quotes the query itself, so the assertion is about
	// HIT STRUCTURE (`rel:line: text`) and the no-matches state — never
	// about the sentinel string appearing anywhere (it appears by design in
	// the pattern echo; asserting absence there is a passing-forever trap).
	g := gset.Call(context.Background(), "grep", grepInput("TOPSECRET", "", nil))
	if g.IsError {
		t.Fatalf("grep over the workspace failed: %s", g.Content)
	}
	if !strings.Contains(g.Content, "no matches") {
		t.Errorf("grep found hits for content reachable only through an unresolved symlink: %s", g.Content)
	}
	for _, forbidden := range []string{"leak-link.txt:1", "escape/leak.txt:"} {
		if strings.Contains(g.Content, forbidden) {
			t.Errorf("grep rendered a hit at %q — implicit read through a symlink the caller could never resolve", forbidden)
		}
	}
	l := gset.Call(context.Background(), "glob", globInput("**", ""))
	if l.IsError {
		t.Fatalf("glob over the workspace failed: %s", l.Content)
	}
	for _, forbidden := range []string{"escape/leak.txt", "leak-link.txt"} {
		if strings.Contains(l.Content, forbidden) {
			t.Errorf("glob listed %q — a path its caller could never resolve, and the walk must not put there", forbidden)
		}
	}
	// And the escaped path, passed explicitly, meets the resolver's refusal.
	for _, toolName := range []string{"glob", "grep"} {
		in := globInput("**", "escape/leak.txt")
		if toolName == "grep" {
			in = grepInput("TOPSECRET", "escape/leak.txt", nil)
		}
		r := gset.Call(context.Background(), toolName, in)
		want := Resolve(root, "escape/leak.txt")
		if want.OK {
			t.Fatalf("the resolver accepted the escape — the case tests nothing")
		}
		if !r.IsError || !strings.Contains(r.Content, want.Reason) {
			t.Errorf("%s did not refuse the escaped path with the resolver's reason: %+v", toolName, r)
		}
	}
}

// TestGlobAndGrepShareTheOnePathCheck is the structural twin of
// TestBothToolsShareTheOnePathCheck, for the search tools: each references
// paths.go's Resolve exactly once, carries no private path handling, and the
// package still defines the resolver in exactly one place.
func TestGlobAndGrepShareTheOnePathCheck(t *testing.T) {
	sources := packageSources(t)
	defs := 0
	for name, src := range sources {
		if strings.Contains(src, "func Resolve(") {
			defs++
			if name != "paths.go" {
				t.Errorf("the resolver is defined in %s; paths.go is the one enforcement point", name)
			}
		}
	}
	if defs != 1 {
		t.Errorf("the package defines the resolver %d times; exactly one definition is allowed", defs)
	}
}

// TestGlobAndGrepDeclareRerunSafeThroughTheClassSeam asserts what recovery
// may do with these tools — pure reads, class "safe", declared (not
// defaulted: the zero value is invalid), read through the Toolset seam, and
// matching the committed fixture (whose `pending` flags this task flips,
// because a registered tool may not remain pending).
func TestGlobAndGrepDeclareRerunSafeThroughTheClassSeam(t *testing.T) {
	root := workspace(t, nil)
	set, err := BuildToolset([]string{"glob", "grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"glob", "grep"} {
		if got := set.RerunClass(name); got != RerunSafe {
			t.Errorf("%s reports class %q through the seam, want safe", name, got)
		}
		entries := loadClasses(t)
		entry, ok := entries[name]
		if !ok {
			t.Fatalf("%s is missing from the committed class list", name)
		}
		if entry.Class != RerunSafe {
			t.Errorf("%s is committed as %q, want safe", name, entry.Class)
		}
		if entry.Pending {
			t.Errorf("%s is still marked pending although it now exists", name)
		}
	}
}

// TestGlobAndGrepDescriptionsStateTheSyntaxDistinction pins the disclaimer
// VERBATIM, in both descriptions — the confusion it prevents is a model
// filtering files with the policy matcher's single-asterisk grammar.
func TestGlobAndGrepDescriptionsStateTheSyntaxDistinction(t *testing.T) {
	const disclaimer = "This tool's glob syntax is its own; policy command patterns use a different, single-asterisk syntax."
	for _, name := range []string{"glob", "grep"} {
		tool, ok := Registry()[name]
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if !strings.Contains(tool.Spec.Description, disclaimer) {
			t.Errorf("%s omits the syntax disclaimer verbatim: %s", name, tool.Spec.Description)
		}
		if !strings.Contains(tool.Spec.Description, "truncation is reported with the true") {
			t.Errorf("%s does not promise true-size truncation: %s", name, tool.Spec.Description)
		}
	}
	// grep's own addition: a result path is information, not a read.
	if d := Registry()["grep"].Spec.Description; !strings.Contains(d, "information, never an implicit read") {
		t.Errorf("grep's description omits the results-are-not-reads claim: %s", d)
	}
}

// --- T19: SEC-T15-01 — stat-before-open bound + mid-file cancellation --------

func TestGrepSkipsFilesPastThePerFileBound(t *testing.T) {
	// The walk reaches files the model never named, and the one shape that
	// turns a scan into a dead daemon is a multi-gigabyte printable no-newline
	// file (SEC-T15-01 measured ≥8 GiB as the kill). The bound is a stat
	// BEFORE the open: the giant is counted and never even asked for its
	// first byte. Sparse truncation stands in for the giant honestly — the
	// cap reads the size, and a real 64 MiB+1 file would read the same.
	root := workspace(t, map[string]string{"ok.txt": "needle here\n"})
	huge := filepath.Join(root, "huge.log")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(GrepMaxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	set, err := BuildToolset([]string{"grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if got.IsError {
		t.Fatalf("a size skip must never be a crash: %s", got.Content)
	}
	if !strings.Contains(got.Content, fmt.Sprintf("skipped 1 files above the %d-byte per-file limit", GrepMaxFileBytes)) {
		t.Errorf("the giant was not skipped with a count: %s", got.Content)
	}
	if !strings.Contains(got.Content, "ok.txt:1: needle here") || !strings.Contains(got.Content, "1 files scanned") {
		t.Errorf("the bound swallowed the scanable file or counted the skipped giant as scanned: %s", got.Content)
	}
	// The control: exactly at the bound, the file is searched, not skipped.
	if err := os.Truncate(huge, GrepMaxFileBytes); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(huge, []byte("needle at the bound\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = set.Call(context.Background(), "grep", grepInput("needle", "", nil))
	if got.IsError || !strings.Contains(got.Content, "needle at the bound") {
		t.Errorf("the at-bound file was skipped — the bound must be >, not >=: %s", got.Content)
	}
}

// countingCancelCtx reports Err only once it has been asked failAt times —
// deterministic mid-file cancellation without a race: the walk checks ctx
// once on the directory and once on the file, grepOne checks on entry, and
// the fourth ask is therefore INSIDE the line loop (at the
// grepCtxCheckEveryLines multiple), proving the walk-entry check was not the
// one that answered.
type countingCancelCtx struct {
	context.Context
	calls  int
	failAt int
}

func (c *countingCancelCtx) Err() error {
	c.calls++
	if c.calls >= c.failAt {
		return context.Canceled
	}
	return nil
}

func TestGrepIsCancellableInsideOneFile(t *testing.T) {
	// SEC-T15-01's second half: the walk checked ctx per ENTRY, leaving one
	// long file the one uncancellable unit. A 3000-line file scanned under a
	// context that turns off mid-loop must come back as the cancellation it
	// is — named, bounded, and never counted as an unreadable file.
	var sb strings.Builder
	for i := 0; i < 3000; i++ {
		sb.WriteString("line number needle\n")
	}
	root := workspace(t, map[string]string{"many.txt": sb.String()})

	set, err := BuildToolset([]string{"grep"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &countingCancelCtx{Context: context.Background(), failAt: 4}
	got := set.Call(ctx, "grep", grepInput("needle", "", nil))
	if !got.IsError {
		t.Fatalf("the mid-file cancellation was not reported: %s", got.Content)
	}
	if !strings.Contains(got.Content, "cancelled") {
		t.Errorf("the refusal does not name the cancellation: %s", got.Content)
	}

	// The direct seam too: grepOne answers a dead context with ctx.Err(),
	// before opening and inside the loop alike.
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, gerr := grepOne(dead, regexp.MustCompile("needle"), filepath.Join(root, "many.txt"), "many.txt", 10)
	if !errors.Is(gerr, context.Canceled) {
		t.Errorf("grepOne with a cancelled context returned %v, want context.Canceled", gerr)
	}
}
