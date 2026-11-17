package handler

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/tools"
)

// A kit never names a tool of its own: every name is held against the real registry, so a tool
// that is renamed or removed fails here instead of reaching a person's picker.
func TestToolKitsOnlyNameRegisteredTools(t *testing.T) {
	seenID := map[string]bool{}
	for _, k := range ToolKits() {
		if k.ID == "" || k.Label == "" || k.Description == "" || len(k.Tools) == 0 {
			t.Errorf("kit %+v is missing a part a person needs to choose it", k)
		}
		if seenID[k.ID] {
			t.Errorf("kit id %q is used twice", k.ID)
		}
		seenID[k.ID] = true
		switch k.Effect {
		case "reads", "changes", "runs", "reaches", "asks":
		default:
			t.Errorf("kit %s has effect %q, outside the five the window knows", k.ID, k.Effect)
		}
		for _, name := range k.Tools {
			if !tools.IsToolName(name) {
				t.Errorf("kit %s names %q, which is not a registered tool", k.ID, name)
			}
		}
	}
}

func TestToolKitsReturnsACopy(t *testing.T) {
	a := ToolKits()
	a[0].Tools[0] = "mutated"
	if ToolKits()[0].Tools[0] == "mutated" {
		t.Fatal("ToolKits handed out its own slice")
	}
}

func TestNormalizeTools(t *testing.T) {
	got, err := NormalizeTools([]string{" read ", "bash", "read", "", "glob"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "bash,glob,read" {
		t.Fatalf("got %v, want the clean sorted set", got)
	}
	if empty, err := NormalizeTools(nil); err != nil || len(empty) != 0 {
		t.Fatalf("no tools is a legal choice: %v %v", empty, err)
	}
	if _, err := NormalizeTools([]string{"read", "wrtie", "nope"}); err == nil || !strings.Contains(err.Error(), "nope, wrtie") {
		t.Fatalf("an unknown tool must be refused by name, got %v", err)
	}
}
