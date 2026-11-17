package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

const maxWriteBytes = 1 << 20
const maxListEntries = 200

// List finds files without granting a shell. It does not follow directory symlinks.
func List() Tool {
	return Tool{Class: RerunSafe, Spec: model.ToolSpec{
		Name: "list", Description: "List files and subdirectories directly inside a workspace-relative directory (up to 200 entries). Use this before guessing a filename. Directory symlinks are not followed.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative directory; use . for the root"}},"required":["path"],"additionalProperties":false}`),
	}, Run: runList}
}

func runList(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return Errorf("list arguments are not valid JSON: %v", err)
	}
	resolved := Resolve(tc.WorkspaceRoot, p.Path)
	if !resolved.OK {
		return Errorf("list refused %q: %s", p.Path, resolved.Reason)
	}
	info, err := os.Stat(resolved.Abs)
	if err != nil {
		return Errorf("list could not stat %s: %v", resolved.Rel, err)
	}
	if !info.IsDir() {
		return Errorf("%s is a file; list needs a directory", resolved.Rel)
	}
	entries, err := os.ReadDir(resolved.Abs)
	if err != nil {
		return Errorf("list failed for %s: %v", resolved.Rel, err)
	}
	var paths []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Errorf("list cancelled: %v", err)
		}
		if len(paths) >= maxListEntries {
			break
		}
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(resolved.Rel, entry.Name()))
		if entry.IsDir() {
			rel += "/"
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	content := strings.Join(paths, "\n")
	if len(paths) == maxListEntries && len(entries) > maxListEntries {
		content += "\n[listing capped at 200 entries]"
	}
	return Result{Content: content, Summary: fmt.Sprintf("list %s · %d entries", resolved.Rel, len(paths))}
}
