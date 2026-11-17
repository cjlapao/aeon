package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func TestBlueprintStartRefusesWorkspaceMissingFromDaemonBeforeSaving(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := filepath.Join(t.TempDir(), "not-mounted-here")
	_, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: "two-step", Goal: "survey", WorkspaceRoot: root,
	})
	// T7 typed this refusal: the daemon understood and declined, so the
	// assertion is the TYPE (the narrow surface classifies on it — refused
	// 409, not transport 503-retry) plus the specific explanation, which
	// WithWhat carries in Data.
	var pe *protocol.Error
	if err == nil || !errors.As(err, &pe) || pe.Code != protocol.ErrWorkspaceNotVisible.Code {
		t.Fatalf("missing daemon-side path was not refused as the typed workspace refusal: %v", err)
	}
	if !strings.Contains(pe.Data.What, "not visible to the daemon") || !strings.Contains(pe.Data.What, root) {
		t.Fatalf("the typed refusal did not explain the specific fact: %+v", pe.Data)
	}
	loops, listErr := s.ListLoops(store.ComponentScope{ComponentRoot: root})
	if listErr != nil || len(loops) != 0 {
		t.Fatalf("unusable workspace created a loop: %v, %v", loops, listErr)
	}
}

func TestBlueprintWorkspaceCheckAcceptsDirectoryAndRejectsFileOrRelativePath(t *testing.T) {
	root := t.TempDir()
	if err := validateLoopWorkspace(root); err != nil {
		t.Fatalf("existing directory refused: %v", err)
	}
	file := filepath.Join(root, "go.mod")
	if err := os.WriteFile(file, []byte("module example.com/qa\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, want string }{
		{file, "not a directory"},
		{"relative/workspace", "must be absolute"},
	} {
		if err := validateLoopWorkspace(test.path); err == nil {
			t.Errorf("validateLoopWorkspace(%q) = nil, want %q", test.path, test.want)
			continue
		} else {
			// T7: typed (refused at the surface) AND specific — the code
			// pins the class, Data.What pins the explanation (O3).
			var pe *protocol.Error
			if !errors.As(err, &pe) || pe.Code != protocol.ErrWorkspaceNotVisible.Code || !strings.Contains(pe.Data.What, test.want) {
				t.Errorf("validateLoopWorkspace(%q) = %v, want %q in the typed refusal", test.path, err, test.want)
			}
		}
	}
}

func TestBlueprintStartTrimsWorkspaceBeforePinning(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	blueprintPath := filepath.Join(t.TempDir(), "workspace-check.yaml")
	content := "id: workspace-check\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: approve\n    step: {kind: gate, question: proceed}\nquestions:\n  proceed:\n    prompt: Continue?\n    answers: [yes, no]\n"
	if err := os.WriteFile(blueprintPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: blueprintPath, Goal: "check workspace", WorkspaceRoot: " " + root + " ",
	})
	if err != nil {
		t.Fatalf("trailing whitespace prevented a valid workspace: %v", err)
	}
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: root})
	if err != nil || len(loops) != 1 || loops[0].Root != root {
		t.Fatalf("workspace was not pinned without whitespace: %v, %v", loops, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		pin, err := s.ReadPinnedLoop(loops[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if pin.Loop.Status == "waiting" && !d.loopRunner.Active(loops[0].ID) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gate runner did not finish before store cleanup: %s", pin.Loop.Status)
		}
		time.Sleep(time.Millisecond)
	}
}
