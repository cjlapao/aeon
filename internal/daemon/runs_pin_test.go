package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runs_pin_test.go pins D5 and CR4 structurally: the run door has exactly
// the two callers the slice named, and the conversation code contains no
// path to a run at all — a message can answer, it cannot start work (CR4).

func TestOnlyTheRunDoorsCallStartRun(t *testing.T) {
	var offenders []string
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err // skip unreadable dirs the same way the tree allows
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Only internal/ production files; the loop package owns its own
		// definition, the daemon's run.go owns the human door (and T10's
		// catalog case will call THIS function, not loop.StartRun).
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		if !strings.Contains(text, "StartRun(") {
			return nil
		}
		switch path {
		case filepath.Join("..", "loop", "run.go"), filepath.Join("..", "daemon", "runs.go"):
			return nil
		}
		offenders = append(offenders, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Fatalf("loop.StartRun reached from %v — D5: a third door started runs (the human doors are loop.run and the run_start tool, and both land in daemon/runs.go)", offenders)
	}
}

func TestNoRunIsStartedFromMessageText(t *testing.T) {
	for _, name := range []string{"conversation.go", "turn.go"} {
		raw, err := os.ReadFile(filepath.Join("..", "daemon", name))
		if name == "turn.go" {
			raw, err = os.ReadFile(filepath.Join("..", "loop", name))
		}
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "StartRun") || strings.Contains(text, "startRun") {
			t.Fatalf("CR4 broken: %s names a run starter — what a person writes is answered, and never executed", name)
		}
	}
}
