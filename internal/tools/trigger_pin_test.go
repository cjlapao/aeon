package tools

// trigger_pin_test.go — AU1, made executable: no agent tool, no tool-shaped
// string, no handler body, no narrow-surface action and no MCP exposure can
// reach a trigger WRITE. Reads reach (T7); writes are a person's doors
// (AU14), and this test is the pin that keeps them so. A scan that reads
// nothing passes forever, so the scan size is asserted too.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNoTriggerAuthoringIsReachableFromAnAgentTool(t *testing.T) {
	// (a) The tool vocabulary has no trigger in it — neither the registry's
	// keys nor the names it hands a model.
	for name := range Registry() {
		if strings.Contains(strings.ToLower(name), "trigger") {
			t.Errorf("tool registry carries %q — AU1: no trigger tool, ever", name)
		}
	}
	for _, name := range ToolNames() {
		if strings.Contains(strings.ToLower(name), "trigger") {
			t.Errorf("ToolNames() carries %q — the model's vocabulary has no trigger verb", name)
		}
	}

	// (b) The write verbs appear in none of the agent-facing packages.
	reach := regexp.MustCompile(`(?i)(CreateEventTrigger|UpdateEventTrigger|SetEventTriggerEnabled|EventTriggerWriter|trigger\.(create|update|enable|disable))`)
	seen := 0
	var offenders []string
	for _, dir := range []string{"tools", "records", "narrow", "agentsvc", "mcp"} {
		root := filepath.Join("..", dir)
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			seen++
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if reach.Find(b) != nil {
				offenders = append(offenders, path)
			}
			return nil
		}); err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
	}
	if seen < 10 {
		t.Fatalf("the scan read %d files — a grep that reads nothing passes forever", seen)
	}
	if len(offenders) != 0 {
		t.Fatalf("trigger-write reach from agent-facing packages: %v — AU1 says these doors are human-only", offenders)
	}

	// (c) No handler BODY teaches a model a trigger_* verb either: bodies
	// are what a model reads as its own instructions.
	bodies := 0
	bodyRe := regexp.MustCompile(`(?i)\btrigger_(create|update|enable|disable)\b`)
	ms, err := filepath.Glob(filepath.Join("..", "handler", "bodies", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		bodies++
		if bodyRe.Find(b) != nil {
			t.Errorf("handler body %s offers a trigger write verb — the model cannot file a standing instruction", m)
		}
	}
	if bodies == 0 {
		t.Fatal("no handler bodies scanned — the (c) arm read nothing and proved nothing")
	}
}
