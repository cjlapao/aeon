package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T5's read tests. Every refusal reaches the model as a tool RESULT, because a
// model told what went wrong can try something else.

func callRead(t *testing.T, root string, args any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return Read().Run(context.Background(), ToolContext{WorkspaceRoot: root}, raw)
}

func TestReadReturnsTheFileWithItsPathFirst(t *testing.T) {
	root := workspace(t, map[string]string{"docs/a.md": "line one\nline two\n"})

	res := callRead(t, root, map[string]any{"path": "docs/a.md"})
	if res.IsError {
		t.Fatalf("error: %s", res.Content)
	}
	// The path is the first line, so a transcript with six reads in it can be
	// read without matching results back to calls.
	if !strings.HasPrefix(res.Content, "docs/a.md\n") {
		t.Errorf("the content does not start with the path: %q", res.Content[:min(40, len(res.Content))])
	}
	if !strings.Contains(res.Content, "line two") {
		t.Error("the file's content is missing")
	}
	if !strings.Contains(res.Summary, "docs/a.md") {
		t.Errorf("the summary does not name the file: %q", res.Summary)
	}
}

func TestEveryReadRefusalIsAToolResultAndNotAnError(t *testing.T) {
	// Q6/O6. A step that died on a bad path would be a step that failed for a
	// reason the model could have fixed in one more turn.
	root := workspace(t, map[string]string{"a.md": "x"})
	if err := os.MkdirAll(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		args  any
		wants string
	}{
		{"unparseable arguments", json.RawMessage(`{"path":`), "not valid JSON"},
		{"a missing path", map[string]any{}, "required"},
		{"an absolute path", map[string]any{"path": "/etc/passwd"}, "absolute"},
		{"a traversal", map[string]any{"path": "../outside"}, ".."},
		{"a file that does not exist", map[string]any{"path": "nope.md"}, "could not find"},
		{"a directory", map[string]any{"path": "adir"}, "is a directory"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var res Result
			if raw, isRaw := tc.args.(json.RawMessage); isRaw {
				res = Read().Run(context.Background(), ToolContext{WorkspaceRoot: root}, raw)
			} else {
				res = callRead(t, root, tc.args)
			}
			if !res.IsError {
				t.Fatalf("%s was not an error: %s", tc.name, res.Content)
			}
			if !strings.Contains(res.Content, tc.wants) {
				t.Errorf("the message does not contain %q: %s", tc.wants, res.Content)
			}
		})
	}
}

func TestReadingADirectoryNamesTheRightProblem(t *testing.T) {
	// A model that reads a directory expecting a listing should be told to use
	// a different tool, rather than handed an error about bytes.
	root := workspace(t, nil)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := callRead(t, root, map[string]any{"path": "docs"})
	if !res.IsError {
		t.Fatal("a directory read succeeded")
	}
	if !strings.Contains(res.Content, "Use `list`") {
		t.Errorf("the message does not say what is missing: %s", res.Content)
	}
}

func TestTruncationReportsTheTrueSize(t *testing.T) {
	// D9. "Truncated" on its own tells a model nothing about how much it is
	// missing: a file cut from 70 KiB and one cut from 70 MiB need different
	// responses — narrow the read, or give up and ask.
	big := strings.Repeat("x", ReadMaxBytes+5000) + "\n"
	root := workspace(t, map[string]string{"big.txt": big})

	res := callRead(t, root, map[string]any{"path": "big.txt"})
	if res.IsError {
		t.Fatalf("error: %s", res.Content)
	}
	if len(res.Content) > ReadMaxBytes+512 {
		t.Errorf("the content is %d bytes; the cap is %d", len(res.Content), ReadMaxBytes)
	}
	if !strings.Contains(res.Content, "cut at") {
		t.Errorf("no truncation note: %q", firstLines(res.Content, 3))
	}
	// The TRUE size, rendered for a person.
	if !strings.Contains(res.Content, humanBytes(len(big))) {
		t.Errorf("the note does not name the true size (%s): %q",
			humanBytes(len(big)), firstLines(res.Content, 3))
	}
}

func TestTheTruncationNoteComesBeforeTheContent(t *testing.T) {
	// A truncation notice after 64 KiB of text is a notice nobody reads, and
	// the model needs to know what it is holding before it reasons about it.
	lines := make([]string, ReadMaxLines+100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	root := workspace(t, map[string]string{"long.txt": strings.Join(lines, "\n")})

	res := callRead(t, root, map[string]any{"path": "long.txt"})
	noteAt := strings.Index(res.Content, "capped at")
	bodyAt := strings.Index(res.Content, "line 0")
	if noteAt < 0 {
		t.Fatalf("no cap note: %q", firstLines(res.Content, 3))
	}
	if noteAt > bodyAt {
		t.Error("the note comes after the content")
	}
	if !strings.Contains(res.Content, fmt.Sprintf("of %d", len(lines))) {
		t.Errorf("the note does not say how many lines there are: %q", firstLines(res.Content, 3))
	}
}

func TestOffsetAndLimitWindowTheFile(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five"}
	root := workspace(t, map[string]string{"a.txt": strings.Join(lines, "\n")})

	res := callRead(t, root, map[string]any{"path": "a.txt", "offset": 2, "limit": 2})
	if res.IsError {
		t.Fatalf("error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "two") || !strings.Contains(res.Content, "three") {
		t.Errorf("the window is wrong: %q", res.Content)
	}
	if strings.Contains(res.Content, "five") {
		t.Errorf("the limit was ignored: %q", res.Content)
	}
	// And the window is DESCRIBED, because a model holding lines 2-3 of 5
	// needs to know there are five.
	if !strings.Contains(res.Content, "showing lines 2-3 of 5") {
		t.Errorf("the window is not described: %q", firstLines(res.Content, 3))
	}
}

func TestAnOffsetPastTheEndSaysSoRatherThanReturningNothing(t *testing.T) {
	// Empty content with no explanation reads to a model as an empty file,
	// and it will conclude the file is empty and move on.
	root := workspace(t, map[string]string{"a.txt": "one\ntwo"})

	res := callRead(t, root, map[string]any{"path": "a.txt", "offset": 99})
	if res.IsError {
		t.Fatalf("error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "past the end") {
		t.Errorf("the result does not explain: %q", res.Content)
	}
	if !strings.Contains(res.Content, "2 lines") {
		t.Errorf("the result does not say how many lines there are: %q", res.Content)
	}
}

func TestAnEmptyFileIsNotAnError(t *testing.T) {
	root := workspace(t, map[string]string{"empty.md": ""})
	res := callRead(t, root, map[string]any{"path": "empty.md"})
	if res.IsError {
		t.Fatalf("an empty file was an error: %s", res.Content)
	}
}

func TestReadStopsOnACancelledContextBeforeTouchingTheDisk(t *testing.T) {
	// The read is the expensive part, and a cancelled step should not start
	// one.
	root := workspace(t, map[string]string{"a.md": "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := Read().Run(ctx, ToolContext{WorkspaceRoot: root}, json.RawMessage(`{"path":"a.md"}`))
	if !res.IsError {
		t.Fatal("a cancelled read succeeded")
	}
	if !strings.Contains(res.Content, "cancelled") {
		t.Errorf("the message does not say why: %s", res.Content)
	}
}

func TestReadNeverReadsOutsideTheWorkspaceEvenThroughASymlink(t *testing.T) {
	// The end-to-end version of the resolver test: the refusal has to survive
	// the trip through the tool, because a tool that resolved the path itself
	// would be the second resolver.
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("NOT-YOURS-SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := workspace(t, nil)
	if err := os.Symlink(secret, filepath.Join(root, "innocent.txt")); err != nil {
		t.Fatal(err)
	}

	res := callRead(t, root, map[string]any{"path": "innocent.txt"})
	if !res.IsError {
		t.Fatalf("the read succeeded: %s", res.Content)
	}
	if strings.Contains(res.Content, "NOT-YOURS-SENTINEL") {
		t.Fatal("the file's contents reached the model")
	}
}

func TestTheReadSpecDescribesItsOwnBounds(t *testing.T) {
	// The model is told the caps exist. One that is not told will read a 9 MiB
	// file, get 256 KiB, and reason about the whole file.
	spec := Read().Spec
	if spec.Name != "read" {
		t.Errorf("name is %q", spec.Name)
	}
	for _, want := range []string{"relative", "refused", "capped", "true size"} {
		if !strings.Contains(spec.Description, want) {
			t.Errorf("the description does not mention %q: %s", want, spec.Description)
		}
	}
	// The parameters are valid JSON Schema-shaped JSON, because a provider
	// rejects the whole request if they are not.
	var parsed map[string]any
	if err := json.Unmarshal(spec.Parameters, &parsed); err != nil {
		t.Fatalf("the parameters are not valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("the parameters are not an object schema: %v", parsed["type"])
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
