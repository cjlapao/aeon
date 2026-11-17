package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// Write is the `write` tool — one of D3's honest ten.
//
// # The one path-enforcement point (S11 T14)
//
// The write body moved here from files.go verbatim-adapted, and it carries
// the single argument this tool family owns: every path a write can reach
// goes through the SAME resolver (`Resolve`, paths.go) that `read`, `list`
// and `edit` use. Absolute paths, `..` traversal and symlink escapes are
// refused there, before this file touches the disk — `os.WriteFile` appears
// below and only ever holds `resolved.Abs`.
//
// There is no second, private path check here. A second resolver is the
// escape that always works (two resolvers diverge, only one gets tested),
// and `TestBothToolsShareTheOnePathCheck` fails the package if one appears
// in this file.
func Write() Tool {
	return Tool{Class: RerunDetectable, Spec: model.ToolSpec{
		Name: "write", Description: "Create or replace a UTF-8 file inside the workspace (up to 1 MiB). Parent directories must exist. Paths are workspace-relative; absolute paths, `..` segments and symlink escapes are refused before anything is written.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative file path"},"content":{"type":"string","description":"complete new file content"}},"required":["path","content"],"additionalProperties":false}`),
	}, Run: runWrite}
}

func runWrite(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return Errorf("write arguments are not valid JSON: %v", err)
	}
	// The cap is checked BEFORE the resolver because it is a fact about the
	// argument, not about the path — the same order the body had in files.go.
	if len(p.Content) > maxWriteBytes {
		return Errorf("write refused %q: content exceeds 1 MiB", p.Path)
	}
	// The ONE path-enforcement point for writes.
	resolved := Resolve(tc.WorkspaceRoot, p.Path)
	if !resolved.OK {
		return Errorf("write refused %q: %s", p.Path, resolved.Reason)
	}
	if err := ctx.Err(); err != nil {
		return Errorf("write cancelled: %v", err)
	}
	if docs, name, ok := claimedDocument(ctx, tc, resolved); ok {
		return routedWrite(ctx, docs, name, p.Content)
	}
	// The previous content is read for the LINE COUNTS only (D8). A file that
	// was not there yet has no previous content — the honest zero baseline;
	// any other read failure counts the write as new too, because best-effort
	// metadata must never fail a write that would otherwise succeed, and the
	// counts are an announcement, not the record. The file itself is the
	// record, and the counts are computed against what this write replaced.
	var previous []byte
	if old, err := os.ReadFile(resolved.Abs); err == nil {
		previous = old
	}
	if err := os.WriteFile(resolved.Abs, []byte(p.Content), 0o644); err != nil {
		return Errorf("write could not write %s: %v", resolved.Rel, err)
	}
	// N5 holds: Content and Summary are the bytes they were before this
	// feature existed (a pinned test asserts them verbatim). The counts ride
	// NEW fields onto the sink, never into what the model reads.
	added, removed := countLineDiff(string(previous), p.Content)
	return Result{Content: fmt.Sprintf("wrote %s (%d bytes)", resolved.Rel, len(p.Content)), Summary: fmt.Sprintf("write %s · %s", resolved.Rel, humanBytes(len(p.Content))),
		Path: resolved.Rel, LinesAdded: added, LinesRemoved: removed}
}

// countLineDiff answers "how many lines did this write add and how many did
// it remove" — a LINE-DIFF COUNT, not a diff (T5): multiset arithmetic, no
// LCS, no order claims. `a,b,c` → `a,x,c` costs +1 −1 whichever way the
// change sits; a new file is +N −0; a replacement of everything is
// +len(new) −len(old). It is the number that answers "what did the agent
// just do to my file" at a glance, and it deliberately does NOT answer
// "which lines moved".
func countLineDiff(previous, next string) (added, removed int) {
	counts := map[string]int{}
	for _, line := range splitFileLines(previous) {
		counts[line]++
	}
	for _, line := range splitFileLines(next) {
		if counts[line] > 0 {
			counts[line]--
			continue
		}
		added++
	}
	for _, outstanding := range counts {
		removed += outstanding
	}
	return added, removed
}

// splitFileLines counts a file's lines the way an editor does: a trailing
// newline ends the last line and is not itself a line. Empty content has no
// lines; a blank line is a line.
func splitFileLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
