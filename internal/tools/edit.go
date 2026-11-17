package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// Edit is the `edit` tool — one of D3's honest ten.
//
// # The one path-enforcement point (S11 T14)
//
// Same argument as write.go, one subject class over: the path is resolved by
// the ONE resolver (`Resolve`, paths.go) before anything is read, and the
// read below holds only `resolved.Abs`. The file is read AFTER resolution,
// only — there is no content probe that runs on an unresolved path.
//
// Exact-match is load-bearing, not taste: S7's rerun classification
// (rerun_classes.json, class "detectable") promises that re-running a
// step can DETECT a stale replay. A silent first-match replace would
// turn a stale replay into a second, invisible edit. If you are here
// wanting regex or replace-all: that is a different tool with a
// different class, and detectable stops being true.
func Edit() Tool {
	return Tool{Class: RerunDetectable, Spec: model.ToolSpec{
		Name: "edit", Description: "Replace exactly one occurrence of old_string with new_string in a UTF-8 workspace file (result up to 1 MiB). Exact match only — no regex, no fuzzy: old_string must appear exactly once, or the edit is refused. Paths are workspace-relative.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"old_string":{"type":"string","description":"exact text to replace; must appear exactly once"},"new_string":{"type":"string","description":"replacement text"}},"required":["path","old_string","new_string"],"additionalProperties":false}`),
	}, Run: runEdit}
}

func runEdit(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		// An unparseable argument is a tool error, not a step failure (Q6),
		// refused the same way write refuses non-JSON.
		return Errorf("edit arguments are not valid JSON: %v", err)
	}
	if p.OldString == "" {
		// Refused as an argument fact, before the resolver: strings.Count
		// counts the empty string everywhere (len+1 times), so an empty
		// pattern would drown in the repeated-match branch instead of
		// naming what is actually wrong with the call.
		return Errorf("edit refused %q: old_string must not be empty", p.Path)
	}
	// The ONE path-enforcement point for edits.
	resolved := Resolve(tc.WorkspaceRoot, p.Path)
	if !resolved.OK {
		return Errorf("edit refused %q: %s", p.Path, resolved.Reason)
	}
	if err := ctx.Err(); err != nil {
		return Errorf("edit cancelled: %v", err)
	}
	if docs, name, ok := claimedDocument(ctx, tc, resolved); ok {
		return routedEdit(ctx, docs, name, p.OldString, p.NewString, false)
	}
	content, err := os.ReadFile(resolved.Abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Errorf("edit could not find %s in the workspace; use `list` to find the file", resolved.Rel)
		}
		return Errorf("edit could not read %s: %v", resolved.Rel, err)
	}

	matches := strings.Count(string(content), p.OldString)
	switch {
	case matches == 0:
		// The stale-old_string case IS this case: the file moved on since
		// the caller last read it, which from here is indistinguishable from
		// a wrong string — and both fail loudly rather than guessing.
		return Errorf("edit refused %s: old_string not found (exact match; no regex, no fuzzy)", resolved.Rel)
	case matches > 1:
		// A repeated match is a loud refusal naming the count, never a
		// first-hit guess.
		return Errorf("edit refused %s: old_string matched %d times; edit requires a unique exact match", resolved.Rel, matches)
	}

	result := strings.Replace(string(content), p.OldString, p.NewString, 1)
	if len(result) > maxWriteBytes {
		// The cap is on the RESULT, not the old_string: an edit is refused
		// when the file it would produce is over 1 MiB.
		return Errorf("edit refused %q: result exceeds 1 MiB", p.Path)
	}
	if err := writeAtomic(resolved.Abs, result); err != nil {
		return Errorf("edit could not write %s: %v", resolved.Rel, err)
	}
	return Result{Content: fmt.Sprintf("edited %s (%d bytes)", resolved.Rel, len(result)), Summary: fmt.Sprintf("edit %s · %s", resolved.Rel, humanBytes(len(result)))}
}

// writeAtomic replaces target's content through a same-directory temp file
// and one rename, so a reader never sees half an edit and a failed edit
// leaves the original intact.
//
// Both halves of the path come from the resolver's answer, never from the
// caller: the temp file is created in filepath.Dir(target) — which is inside
// the workspace because target is `resolved.Abs` — and the rename replaces
// exactly the resolved path. There is no caller-constructed path in here to
// escape with.
func writeAtomic(target, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".aeon-edit-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Covers every failure path after creation; after a successful rename the
	// temp name no longer exists and the removal is a harmless no-op.
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil { // CreateTemp makes 0600; workspace files are 0644, matching write
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}
