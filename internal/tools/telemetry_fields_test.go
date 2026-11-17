package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// S33 T6 (D6) — the two new Result fields are ADDITIVE and nothing else
// (N5): the ten tools' model-facing behaviour is frozen, so this test pins
// the bytes the model reads for the three file tools AND pins the new
// fields' three-state (nil = never a command, set = ran, always set for
// bash including exit 0).

func TestResultFieldsAreAdditiveOnly(t *testing.T) {
	root := t.TempDir()
	set, err := BuildToolset([]string{"read", "write", "edit", "bash"}, ToolContext{
		Exec:          testExecSeam(t),
		WorkspaceRoot: root, CommandVerdict: allowAllSeam(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// write: the model-facing bytes are EXACTLY what they were before this
	// field existed (S31's pin re-stated here for the amendment's diff),
	// and a write is not a command — the new fields stay at absence.
	const content = "one\ntwo\nthree\n" // 14 bytes
	w := set.Call(ctx, "write", json.RawMessage(`{"path":"a.md","content":"one\ntwo\nthree\n"}`))
	if w.IsError {
		t.Fatalf("write failed: %s", w.Content)
	}
	if w.Content != "wrote a.md (14 bytes)" || w.Summary != "write a.md · 14 bytes" {
		t.Errorf("write's model-facing/event-facing bytes drifted under the amendment: %q / %q", w.Content, w.Summary)
	}
	if w.ExitCode != nil || w.WallMs != 0 {
		t.Errorf("a write is not a command: ExitCode=%v WallMs=%d", w.ExitCode, w.WallMs)
	}
	if w.Path != "a.md" || w.LinesAdded != 3 || w.LinesRemoved != 0 {
		t.Errorf("the S31 fields rode as before? got %q +/-%d-%d", w.Path, w.LinesAdded, w.LinesRemoved)
	}

	e := set.Call(ctx, "edit", json.RawMessage(`{"path":"a.md","old_string":"two","new_string":"2"}`))
	if e.IsError {
		t.Fatalf("edit failed: %s", e.Content)
	}
	if e.Content != "edited a.md (12 bytes)" || e.Summary != "edit a.md · 12 bytes" {
		t.Errorf("edit's frozen bytes drifted: %q / %q", e.Content, e.Summary)
	}
	if e.ExitCode != nil || e.WallMs != 0 {
		t.Errorf("an edit is not a command: ExitCode=%v WallMs=%d", e.ExitCode, e.WallMs)
	}

	r := set.Call(ctx, "read", json.RawMessage(`{"path":"a.md"}`))
	if r.IsError || r.ExitCode != nil || r.WallMs != 0 || r.Path != "" {
		t.Errorf("read must be untouched by the amendment: %+v", r)
	}

	// bash: the exit code rides on EVERY result whose command started —
	// success included (D6 counts runs, and the exit 0 row is the
	// single most load-bearing number on the page).
	b := set.Call(ctx, "bash", json.RawMessage(`{"command":"true"}`))
	if b.IsError {
		t.Fatalf("bash true failed: %s", b.Content)
	}
	if b.ExitCode == nil || *b.ExitCode != 0 {
		t.Fatalf("an exit 0 bash result must carry a NON-NIL pointer to zero, got %v", b.ExitCode)
	}
	if b.WallMs < 0 {
		t.Errorf("the spawn wall is negative: %d", b.WallMs)
	}
	f := set.Call(ctx, "bash", json.RawMessage(`{"command":"exit 3"}`))
	if f.IsError {
		t.Errorf("a failing command is not a tool error: %+v", f)
	}
	if f.ExitCode == nil || *f.ExitCode != 3 {
		t.Fatalf("exit 3 did not ride: %v", f.ExitCode)
	}

	// And the constructor of refusals keeps absence: an Errorf result has
	// never seen a command at all — nil is the honest state there too.
	x := Errorf("nope")
	if x.ExitCode != nil || x.WallMs != 0 {
		t.Errorf("Errorf smuggled command facts into a refusal: %+v", x)
	}
}
