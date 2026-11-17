package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestLoopFileReaderUsesPinnedWorkspaceAndConfinesPaths(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tasks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tasks", "discovery.md"), []byte("# Discovery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("secret=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "security.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.md")); err != nil {
		t.Fatal(err)
	}
	blueprintPath := filepath.Join(t.TempDir(), "files.yaml")
	content := "id: files\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: approve\n    step: {kind: gate, question: proceed}\nquestions:\n  proceed:\n    prompt: Continue?\n    answers: [yes, no]\n"
	if err := os.WriteFile(blueprintPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	started, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{Blueprint: blueprintPath, Goal: "inspect files", WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	id := started.(map[string]any)["id"].(string)
	defer func() {
		deadline := time.Now().Add(2 * time.Second)
		for d.loopRunner.Active(id) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}()
	result, err := d.listLoopFiles(context.Background(), protocol.LoopFileParams{ID: id, Path: "."})
	if err != nil {
		t.Fatal(err)
	}
	files := result.(map[string]any)["files"].([]loopFileEntry)
	if len(files) != 1 || files[0].Path != "tasks" || files[0].Kind != "directory" {
		t.Fatalf("root files = %+v", files)
	}
	result, err = d.readLoopFile(context.Background(), protocol.LoopFileParams{ID: id, Path: "tasks/discovery.md"})
	if err != nil || result.(map[string]any)["content"] != "# Discovery\n" {
		t.Fatalf("read = %v, %v", result, err)
	}
	for _, path := range []string{"../security.md", ".env", "escape.md", "tasks/../../security.md", "tasks\\..\\.env"} {
		if _, err := d.readLoopFile(context.Background(), protocol.LoopFileParams{ID: id, Path: path}); err == nil {
			t.Errorf("reader accepted %q", path)
		}
	}
	if _, err := d.listLoopFiles(context.Background(), protocol.LoopFileParams{ID: id, Path: "../"}); err == nil {
		t.Fatalf("directory traversal = %v", err)
	}
}
