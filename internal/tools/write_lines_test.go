package tools

// # The write line counts (S31 T5, D8)
//
// "What did the agent just do to my file" answered by two numbers on the
// event, computed here, at the one place that holds both versions. The
// counts are LINE-DIFF COUNTS — multiset arithmetic, not a diff: they say
// how many lines the write cost, not which ones moved. And N5's freeze
// holds absolutely: `Content` stays the byte-identical sentence a pinned
// test already guards; the numbers ride NEW fields onto the event stream,
// never into what the model reads.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func callWrite(t *testing.T, root, path, content string) Result {
	t.Helper()
	set, err := BuildToolset([]string{"write"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return set.Call(context.Background(), "write", in)
}

func TestWriteCountsANewFileAsAllAdditions(t *testing.T) {
	root := workspace(t, map[string]string{"notes/keep.txt": "seed"})
	got := callWrite(t, root, "notes/new.md", "one\ntwo\nthree\n")
	if got.IsError {
		t.Fatalf("write failed: %s", got.Content)
	}
	if got.Path != "notes/new.md" || got.LinesAdded != 3 || got.LinesRemoved != 0 {
		t.Fatalf("a new file must count +3 −0 on its own path: %+v", got)
	}
	// N5: the model-facing sentence is the frozen one — no numbers joined it.
	if got.Content != "wrote notes/new.md (14 bytes)" {
		t.Fatalf("Content moved (N5): %q", got.Content)
	}
}

func TestWriteCountsAnOverwriteAgainstWhatItReplaced(t *testing.T) {
	root := workspace(t, map[string]string{"a.md": "keep\nchange\nkeep2\n"})
	got := callWrite(t, root, "a.md", "keep\nchanged\nkeep2\n")
	if got.IsError {
		t.Fatal(got.Content)
	}
	if got.LinesAdded != 1 || got.LinesRemoved != 1 {
		t.Fatalf("one changed line must count +1 −1, got +%d −%d", got.LinesAdded, got.LinesRemoved)
	}

	// Replacing everything: every line of the CURRENT content goes, every
	// new line comes. The current content is the 3 lines the write above
	// left behind — the baseline is what was there, not what was first seeded.
	got = callWrite(t, root, "a.md", "x\ny\n")
	if got.LinesAdded != 2 || got.LinesRemoved != 3 {
		t.Fatalf("total replacement must count +2 −3 (three lines were on disk), got +%d −%d",
			got.LinesAdded, got.LinesRemoved)
	}

	// Rewriting identical content costs nothing — the numbers tell the truth
	// about a no-op too.
	_ = os.WriteFile(filepath.Join(root, "same.md"), []byte("same\n"), 0o644)
	got = callWrite(t, root, "same.md", "same\n")
	if got.LinesAdded != 0 || got.LinesRemoved != 0 {
		t.Fatalf("an identical rewrite must count +0 −0, got +%d −%d", got.LinesAdded, got.LinesRemoved)
	}
}

func TestWriteCountsADeletedFileAsNew(t *testing.T) {
	// Delete-then-create has no previous content at write time — the honest
	// zero baseline, whatever the file used to hold.
	root := workspace(t, map[string]string{"gone.md": "was\nhere\nbefore\n"})
	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatal(err)
	}
	got := callWrite(t, root, "gone.md", "reborn\n")
	if got.IsError {
		t.Fatal(got.Content)
	}
	if got.LinesAdded != 1 || got.LinesRemoved != 0 {
		t.Fatalf("a deleted file must be +1 −0 (the −3 belongs to the delete, not this write), got +%d −%d",
			got.LinesAdded, got.LinesRemoved)
	}
}

func TestWriteLineCountingConventions(t *testing.T) {
	cases := []struct {
		name                   string
		previous, next         string
		wantAdded, wantRemoved int
	}{
		{"empty to one line", "", "a\n", 1, 0},
		{"trailing newline is not a line", "a\n", "a", 0, 0},
		{"a blank line is a line", "a\n", "a\n\n", 1, 0},
		{"identical multiset reordered is free", "a\nb\n", "b\na\n", 0, 0},
		{"duplicate lines counted as a multiset", "a\na\n", "a\n", 0, 1},
		{"emptying a file", "a\nb\n", "", 0, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := countLineDiff(tc.previous, tc.next)
			if added != tc.wantAdded || removed != tc.wantRemoved {
				t.Fatalf("countLineDiff(%q, %q) = +%d −%d, want +%d −%d",
					tc.previous, tc.next, added, removed, tc.wantAdded, tc.wantRemoved)
			}
		})
	}
}

func TestWriteCountsSurviveAFailedWriteByNotExisting(t *testing.T) {
	// A refused write announces nothing: no counts, no path — the file was
	// not touched and an announcement would be a claim about a change that
	// did not happen.
	root := workspace(t, map[string]string{"x.md": "one\n"})
	set, err := BuildToolset([]string{"write"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	got := set.Call(context.Background(), "write", json.RawMessage(`{"path":"../escape","content":"x"}`))
	if !got.IsError {
		t.Fatal("the escape write was not refused")
	}
	if got.Path != "" || got.LinesAdded != 0 || got.LinesRemoved != 0 {
		t.Fatalf("a refused write announced metadata: %+v", got)
	}
}
