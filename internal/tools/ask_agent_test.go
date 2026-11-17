package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// ask_agent_test.go pins S70 AT1 (the parser refuses before anything
// persists — and the parser IS the before) and AT2's vocabulary half.

func TestAT1ParseAskAgentRefusesUnknownFieldsByName(t *testing.T) {
	_, err := ParseAskAgent(json.RawMessage(`{"to":"author","text":"x","prioritz":1,"prority":2}`))
	if err == nil {
		t.Fatal("unknown fields sailed through the parser")
	}
	for _, want := range []string{"prioritz", "prority"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name the unknown field %q: %v", want, err)
		}
	}
	if !strings.Contains(err.Error(), "to, text, thread, wait") {
		t.Errorf("the refusal does not state the vocabulary: %v", err)
	}
}

func TestAT1ParseAskAgentRefusesShapeLies(t *testing.T) {
	for _, in := range []struct {
		name, json, want string
	}{
		{"missing to", `{"text":"x"}`, "to is required"},
		{"blank to", `{"to":"  ","text":"x"}`, "to is required"},
		{"missing text", `{"to":"author"}`, "text is required"},
		{"blank text", `{"to":"author","text":"   "}`, "text is required"},
		{"thread zero", `{"to":"author","text":"x","thread":0}`, "positive thread id"},
		{"thread negative", `{"to":"author","text":"x","thread":-3}`, "positive thread id"},
		{"wait not a bool", `{"to":"author","text":"x","wait":"yes"}`, "not valid"},
		{"not an object", `[1,2]`, "not valid"},
	} {
		_, err := ParseAskAgent(json.RawMessage(in.json))
		if err == nil || !strings.Contains(err.Error(), in.want) {
			t.Errorf("%s: got %v — want it to name %q", in.name, err, in.want)
		}
	}
}

func TestAT1ParseAskAgentAcceptsTheHonestShapes(t *testing.T) {
	p, err := ParseAskAgent(json.RawMessage(`{"to":"author","text":"is the risk section current?"}`))
	if err != nil {
		t.Fatalf("the plain honest ask refused: %v", err)
	}
	if p.To != "author" || p.Text == "" || p.Thread != nil || p.Wait != nil {
		t.Errorf("parsed %+v — omitted optionals must stay nil, not zero-shape", p)
	}
	th := int64(7)
	w := false
	p, err = ParseAskAgent(json.RawMessage(`{"to":"author","text":"follow-up","thread":7,"wait":false}`))
	if err != nil {
		t.Fatalf("the full ask refused: %v", err)
	}
	if p.Thread == nil || *p.Thread != th || p.Wait == nil || *p.Wait != w {
		t.Errorf("parsed %+v — want thread 7, wait false, both present", p)
	}
}

func TestAT2AskAgentIsRegisteredClosedAndNeverExecutable(t *testing.T) {
	tool, ok := Registry()["ask_agent"]
	if !ok {
		t.Fatal("ask_agent left the registry")
	}
	if tool.Spec.Name != "ask_agent" {
		t.Errorf("spec name %q — the registry key and the spec must agree", tool.Spec.Name)
	}
	if tool.Class != RerunDetectable {
		t.Errorf("class %q — an ask files a row; a re-run that duplicates it is detectable, not silent", tool.Class)
	}
	// Closed vocabulary at the spec level too (AT1's schema twin).
	var spec map[string]any
	if err := json.Unmarshal(tool.Spec.Parameters, &spec); err != nil {
		t.Fatalf("the tool's parameter schema is not JSON: %v", err)
	}
	if spec["additionalProperties"] != false {
		t.Error("the parameter schema does not refuse additional properties")
	}
	props, _ := spec["properties"].(map[string]any)
	for _, name := range []string{"to", "text", "thread", "wait"} {
		if _, ok := props[name]; !ok {
			t.Errorf("the schema lost %q", name)
		}
	}
	// Run is unreachable: calling it is the bug the question tool's K18
	// declares (the runtime recognises ask_agent by name and files itself).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("ask_agent Run executed instead of panicking — the runtime must own every effect")
			} else if !strings.Contains(safeString(r), "ask_agent") {
				t.Errorf("the panic does not name the tool: %v", r)
			}
		}()
		tool.Run(nil, ToolContext{}, json.RawMessage(`{}`))
	}()
}

func safeString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
