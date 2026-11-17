package model

import (
	"encoding/json"
	"testing"
)

func TestToolNamesLeaveInAFormTheProviderAcceptsAndComeBackCanonical(t *testing.T) {
	req := ModelRequest{
		Tools: []ToolSpec{{Name: "read"}, {Name: "github.create_issue"}, {Name: "github.create-issue"}},
		Messages: []Message{{Role: RoleAssistant, Content: []Block{
			{Kind: BlockToolUse, ID: "c1", Name: "github.create_issue", Input: json.RawMessage(`{}`)},
		}}},
	}
	w := newWireNames(req)
	if !w.changes() {
		t.Fatal("a dotted name changes on the wire")
	}
	out := w.encode(req)
	seen := map[string]bool{}
	for _, tl := range out.Tools {
		if !wireNameOK.MatchString(tl.Name) {
			t.Errorf("wire name %q is not accepted by the providers", tl.Name)
		}
		if seen[tl.Name] {
			t.Errorf("wire name %q is used twice", tl.Name)
		}
		seen[tl.Name] = true
	}
	if out.Tools[0].Name != "read" {
		t.Errorf("a first-party name changed: %q", out.Tools[0].Name)
	}
	if got := out.Messages[0].Content[0].Name; got != out.Tools[1].Name {
		t.Errorf("history names %q, tool list names %q — one tool, one wire name", got, out.Tools[1].Name)
	}
	if req.Tools[1].Name != "github.create_issue" {
		t.Error("the caller's request was edited")
	}
	back := w.decode(Delta{Kind: DeltaToolUse, Block: &Block{Kind: BlockToolUse, Name: out.Tools[1].Name}})
	if back.Block.Name != "github.create_issue" {
		t.Errorf("decoded to %q", back.Block.Name)
	}
}

func TestARequestWithOnlyAcceptedNamesIsUntouched(t *testing.T) {
	w := newWireNames(ModelRequest{Tools: []ToolSpec{{Name: "read"}, {Name: "doc_write"}}})
	if w.changes() {
		t.Fatal("nothing to translate")
	}
}
