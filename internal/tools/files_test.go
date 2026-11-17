package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceFilesNeedNoShell(t *testing.T) {
	root := workspace(t, map[string]string{"ui/README.md": "hello"})
	set, err := BuildToolset(DefaultGrant(), ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if set.Has("bash") {
		t.Fatal("the default file tools also granted bash")
	}
	listing := set.Call(context.Background(), "list", json.RawMessage(`{"path":"."}`))
	if listing.IsError || !strings.Contains(listing.Content, "ui/") {
		t.Fatalf("list: %+v", listing)
	}
	missing := set.Call(context.Background(), "read", json.RawMessage(`{"path":"README.md"}`))
	if !missing.IsError || !strings.Contains(missing.Content, "use `list`") || strings.Contains(missing.Content, root) {
		t.Fatalf("missing README guidance: %+v", missing)
	}
	listing = set.Call(context.Background(), "list", json.RawMessage(`{"path":"ui"}`))
	if listing.IsError || !strings.Contains(listing.Content, "ui/README.md") {
		t.Fatalf("list ui: %+v", listing)
	}
	read := set.Call(context.Background(), "read", json.RawMessage(`{"path":"ui/README.md"}`))
	if read.IsError || !strings.Contains(read.Content, "hello") {
		t.Fatalf("read: %+v", read)
	}
	write := set.Call(context.Background(), "write", json.RawMessage(`{"path":"ui/README.md","content":"updated"}`))
	if write.IsError {
		t.Fatalf("write: %+v", write)
	}
	got, err := os.ReadFile(filepath.Join(root, "ui/README.md"))
	if err != nil || string(got) != "updated" {
		t.Fatalf("file is %q, %v", got, err)
	}
}

func TestWriteFileCannotEscapeWorkspace(t *testing.T) {
	root := workspace(t, nil)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	set, err := BuildToolset(DefaultGrant(), ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../outside.txt", "escape/outside.txt", filepath.Join(outside, "outside.txt")} {
		input, _ := json.Marshal(map[string]string{"path": path, "content": "secret"})
		if got := set.Call(context.Background(), "write", input); !got.IsError {
			t.Errorf("accepted %q", path)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists: %v", err)
	}
}
