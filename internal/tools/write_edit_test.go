package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// callInput builds the arguments for a tool under test. The table tests drive
// BOTH file tools through the same cases: write takes {path, content}, edit
// takes {path, old_string, new_string}.
func callInput(tool, path string) json.RawMessage {
	if tool == "write" {
		raw, _ := json.Marshal(map[string]string{"path": path, "content": "secret"})
		return raw
	}
	raw, _ := json.Marshal(map[string]string{"path": path, "old_string": "alpha", "new_string": "beta"})
	return raw
}

// TestWriteAndEditRefuseTheSameBadPaths is the behavioural half of the one
// enforcement point: the three escape classes — absolute path, `..` prefix,
// escaping symlink — are refused for BOTH tools, and the refusal text of each
// comes from the resolver itself (the same Reason substring appears in both
// tools' messages), not from a private check in either tool.
func TestWriteAndEditRefuseTheSameBadPaths(t *testing.T) {
	root := workspace(t, map[string]string{"notes/seed.txt": "seed"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "target.txt"), []byte("alpha"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"absolute", filepath.Join(outside, "target.txt")},
		{"traversal", "../outside.txt"},
		{"symlink-escape", "escape/target.txt"},
		{"symlink-escape-new", "escape/brand-new.txt"},
	}

	for _, tool := range []string{"write", "edit"} {
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
				got := set.Call(context.Background(), tool, callInput(tool, tc.path))
				if !got.IsError {
					t.Fatalf("%s accepted %q: %+v", tool, tc.path, got)
				}
				if !strings.Contains(got.Content, want.Reason) {
					t.Errorf("%s did not carry the resolver's reason for %q.\n got: %s\nwant substring: %s",
						tool, tc.path, got.Content, want.Reason)
				}
			})
		}
	}

	if b, err := os.ReadFile(filepath.Join(outside, "target.txt")); err != nil || string(b) != "alpha" {
		t.Fatalf("the outside file was touched: %q, %v", b, err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
		t.Fatalf("something was created outside the workspace: %v, %v", entries, err)
	}
}

// TestBothToolsShareTheOnePathCheck is the structural half: the resolver is
// referenced exactly once in write.go and once in edit.go (the same function,
// paths.go's Resolve); every file-touching call in those two files comes
// AFTER the resolver call and takes a resolved path, never a caller-
// constructed one; and neither file re-implements any part of the resolver.
// files.go keeps its own use (for `list`) and no longer holds a write body.
func TestBothToolsShareTheOnePathCheck(t *testing.T) {
	sources := packageSources(t)
	const resolverCall = "Resolve(tc.WorkspaceRoot,"

	// The resolver is DEFINED exactly once in the package, and where it has
	// always lived.
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

	for _, name := range []string{"write.go", "edit.go"} {
		src, ok := sources[name]
		if !ok {
			t.Fatalf("%s is missing from the package", name)
		}
		if n := strings.Count(src, resolverCall); n != 1 {
			t.Errorf("%s references the resolver %d times; exactly one reference is allowed", name, n)
		}
		for _, private := range []string{"func Resolve", "func resolve", "filepath.EvalSymlinks", "filepath.Join(", "filepath.Clean("} {
			if strings.Contains(src, private) {
				t.Errorf("%s carries private path handling (%s); the resolver stays in paths.go and is not re-implemented",
					name, private)
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
					t.Errorf("%s opens the filesystem (%s) BEFORE the resolver call; the file is touched after resolution, only",
						name, opener)
				}
				idx = abs + len(opener)
			}
		}
	}

	// files.go still USES the resolver (for list) but no longer holds the
	// write body; write.go owns the single copy of the result sentence.
	if !strings.Contains(sources["files.go"], resolverCall) {
		t.Error("files.go no longer goes through the resolver; `list` must share the enforcement point")
	}
	if strings.Contains(sources["files.go"], "wrote %s") {
		t.Error("files.go still duplicates the write body")
	}
	if n := strings.Count(sources["write.go"], "wrote %s"); n != 1 {
		t.Errorf(`the "wrote %%s" sentence appears %d times in write.go; one write body is allowed`, n)
	}
}

func TestWriteReportsTheSameResultAndHonorsTheCap(t *testing.T) {
	root := workspace(t, map[string]string{"notes/keep.txt": "seed"})
	set, err := BuildToolset([]string{"write"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}

	got := set.Call(context.Background(), "write", json.RawMessage(`{"path":"notes/a.txt","content":"hello"}`))
	if got.IsError || !strings.Contains(got.Content, "wrote notes/a.txt (5 bytes)") {
		t.Fatalf("write result: %+v", got)
	}
	if b, err := os.ReadFile(filepath.Join(root, "notes/a.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("file on disk is %q, %v", b, err)
	}

	// Parent directories must exist — write does not create them.
	got = set.Call(context.Background(), "write", json.RawMessage(`{"path":"missing/a.txt","content":"x"}`))
	if !got.IsError || !strings.Contains(got.Content, "could not write missing/a.txt") {
		t.Errorf("a missing parent was not refused honestly: %+v", got)
	}

	// The 1 MiB cap.
	big := json.RawMessage(`{"path":"big.txt","content":"` + strings.Repeat("x", maxWriteBytes+1) + `"}`)
	got = set.Call(context.Background(), "write", big)
	if !got.IsError || !strings.Contains(got.Content, "exceeds 1 MiB") {
		t.Errorf("a 1 MiB+1 write was not refused: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "big.txt")); !os.IsNotExist(err) {
		t.Error("the over-cap write reached the disk")
	}

	got = set.Call(context.Background(), "write", json.RawMessage(`{"path":`))
	if !got.IsError || !strings.Contains(got.Content, "not valid JSON") {
		t.Errorf("non-JSON arguments were not refused: %+v", got)
	}
}

func TestEditHappyPathReplacesTheUniqueMatch(t *testing.T) {
	root := workspace(t, map[string]string{"src/main.go": "package main\nfunc main() {}\n"})
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"src/main.go","old_string":"func main() {}","new_string":"func main() { println(\"hi\") }"}`))
	if got.IsError {
		t.Fatalf("happy-path edit refused: %+v", got)
	}
	if !strings.Contains(got.Content, "edited src/main.go") {
		t.Errorf("result sentence: %+v", got)
	}
	b, err := os.ReadFile(filepath.Join(root, "src/main.go"))
	if err != nil || !strings.Contains(string(b), `println("hi")`) {
		t.Fatalf("file on disk is %q, %v", b, err)
	}
	// The atomic path leaves no temp file behind.
	entries, err := os.ReadDir(filepath.Join(root, "src"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "main.go" {
		t.Errorf("the atomic write left residue in the directory: %v", entries)
	}
}

func TestEditStaleOldStringFailsAsNotFound(t *testing.T) {
	// A stale replay — the file moved on since the caller last read it — is
	// the 0-match case and fails LOUDLY. This is what keeps class "detectable"
	// honest: a stale replay never becomes a second, invisible edit.
	root := workspace(t, map[string]string{"notes.txt": "alpha"})
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"notes.txt","old_string":"alpha","new_string":"beta"}`)); got.IsError {
		t.Fatalf("first edit refused: %+v", got)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"notes.txt","old_string":"alpha","new_string":"gamma"}`))
	if !got.IsError || !strings.Contains(got.Content, "old_string not found (exact match; no regex, no fuzzy)") {
		t.Fatalf("the stale edit did not fail as not-found: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "notes.txt")); string(b) != "beta" {
		t.Errorf("the stale replay modified the file anyway: %q", b)
	}
}

func TestEditRepeatedOldStringNamesTheCount(t *testing.T) {
	// A repeated match is a loud refusal naming the count, never a first-hit
	// guess.
	root := workspace(t, map[string]string{"dup.txt": "aa and aa and bb"})
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"dup.txt","old_string":"aa","new_string":"cc"}`))
	if !got.IsError {
		t.Fatalf("a 2-match edit was accepted: %+v", got)
	}
	if !strings.Contains(got.Content, "matched 2 times") || !strings.Contains(got.Content, "unique exact match") {
		t.Errorf("refusal does not name the count: %s", got.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "dup.txt")); b != nil && string(b) != "aa and aa and bb" {
		t.Errorf("the refused edit still touched the file: %q", b)
	}
}

func TestEditEmptyOldStringIsRefused(t *testing.T) {
	root := workspace(t, map[string]string{"e.txt": "alpha"})
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"e.txt","old_string":"","new_string":"x"}`))
	if !got.IsError || !strings.Contains(got.Content, "old_string must not be empty") {
		t.Fatalf("an empty old_string was not refused: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "e.txt")); string(b) != "alpha" {
		t.Errorf("the refused edit touched the file: %q", b)
	}
}

func TestEditResultCapIsOnTheResult(t *testing.T) {
	// The file fits; the edit is refused because the RESULT would exceed
	// 1 MiB — and the original is untouched.
	root := workspace(t, map[string]string{"big.txt": strings.Repeat("a", 700<<10) + "UNIQUE"})
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"big.txt","old_string":"UNIQUE","new_string":"`+strings.Repeat("b", maxWriteBytes)+`"}`))
	if !got.IsError || !strings.Contains(got.Content, "result exceeds 1 MiB") {
		t.Fatalf("an over-cap result was not refused: %+v", got)
	}
	b, err := os.ReadFile(filepath.Join(root, "big.txt"))
	if err != nil || !strings.HasSuffix(string(b), "UNIQUE") {
		t.Fatalf("the refused edit replaced the file: %v", err)
	}
}

func TestEditRefusesMissingFileAndNonJSON(t *testing.T) {
	root := workspace(t, nil)
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"nope.txt","old_string":"a","new_string":"b"}`))
	if !got.IsError || !strings.Contains(got.Content, "could not find nope.txt") {
		t.Errorf("missing file: %+v", got)
	}
	got = set.Call(context.Background(), "edit", json.RawMessage(`{"path":"a","old_string":`))
	if !got.IsError || !strings.Contains(got.Content, "not valid JSON") {
		t.Errorf("non-JSON arguments: %+v", got)
	}
}

func TestWriteAndEditHonourCancellation(t *testing.T) {
	root := workspace(t, map[string]string{"c.txt": "alpha"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wset, err := BuildToolset([]string{"write"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := wset.Call(ctx, "write", json.RawMessage(`{"path":"c.txt","content":"x"}`)); !got.IsError ||
		!strings.Contains(got.Content, "write cancelled") {
		t.Errorf("cancelled write: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "c.txt")); string(b) != "alpha" {
		t.Errorf("the cancelled write touched the file: %q", b)
	}
	eset, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := eset.Call(ctx, "edit", json.RawMessage(`{"path":"c.txt","old_string":"alpha","new_string":"x"}`)); !got.IsError ||
		!strings.Contains(got.Content, "edit cancelled") {
		t.Errorf("cancelled edit: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "c.txt")); string(b) != "alpha" {
		t.Errorf("the cancelled edit touched the file: %q", b)
	}
}

func TestEditLeavesOriginalWhenTheTempFileCannotBeCreated(t *testing.T) {
	// The atomic write's failure path: when the same-directory temp file
	// cannot be created, the edit fails as a refusal naming the write, and
	// the original content survives intact.
	root := workspace(t, map[string]string{"ro/keep.txt": "alpha"})
	dir := filepath.Join(root, "ro")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("restore the directory mode: %v", err)
		}
	})
	set, err := BuildToolset([]string{"edit"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "edit", json.RawMessage(
		`{"path":"ro/keep.txt","old_string":"alpha","new_string":"beta"}`))
	if !got.IsError || !strings.Contains(got.Content, "could not write ro/keep.txt") {
		t.Fatalf("the failed atomic write was not refused honestly: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); string(b) != "alpha" {
		t.Errorf("the failed edit replaced the original: %q", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the failed edit left residue: %v", entries)
	}
}

func TestWriteAndEditClassesReadDetectableFromTheCommittedFixture(t *testing.T) {
	// Both tools report `detectable` through the class seam, and the fixture
	// ALREADY committed that class (they were among the 11 names) — no class
	// value changed in this task.
	entries := loadClasses(t)
	registry := Registry()
	for _, name := range []string{"write", "edit"} {
		entry, ok := entries[name]
		if !ok {
			t.Fatalf("%s is missing from the committed class list", name)
		}
		if entry.Class != RerunDetectable {
			t.Errorf("%s is committed as %q, want detectable", name, entry.Class)
		}
		if entry.Pending {
			t.Errorf("%s is still marked pending although it now exists", name)
		}
		tool, exists := registry[name]
		if !exists {
			t.Fatalf("%s is not in the registry", name)
		}
		if tool.RerunClass() != RerunDetectable {
			t.Errorf("%s reports class %q through the seam, want detectable", name, tool.RerunClass())
		}
	}
}
