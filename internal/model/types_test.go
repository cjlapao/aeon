package model

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// D12 / M20 — the trade the tagged struct makes, bought back.
//
// One case per foreign field, per kind. A table rather than a few samples
// because the enumeration IS the specification: a missing row is a field that
// can reach a stored transcript.
func TestValidateRejectsAFieldForeignToTheKind(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block Block
		field string
	}{
		{"text carrying a toolUseId", Block{Kind: BlockText, Text: "hi", ToolUseID: "t1"}, "toolUseId"},
		{"text carrying a signature", Block{Kind: BlockText, Text: "hi", Signature: "sig"}, "signature"},
		{"text carrying an id", Block{Kind: BlockText, Text: "hi", ID: "b1"}, "id"},
		{"text carrying a name", Block{Kind: BlockText, Text: "hi", Name: "bash"}, "name"},
		{"text carrying input", Block{Kind: BlockText, Text: "hi", Input: json.RawMessage(`{}`)}, "input"},
		{"text carrying content", Block{Kind: BlockText, Text: "hi", Content: "out"}, "content"},
		{"text carrying isError", Block{Kind: BlockText, Text: "hi", IsError: true}, "isError"},

		{"thinking carrying an id", Block{Kind: BlockThinking, Text: "hmm", ID: "b1"}, "id"},
		{"thinking carrying input", Block{Kind: BlockThinking, Text: "hmm", Input: json.RawMessage(`{}`)}, "input"},
		{"thinking carrying a toolUseId", Block{Kind: BlockThinking, Text: "hmm", ToolUseID: "t1"}, "toolUseId"},

		{"tool_use carrying text", Block{Kind: BlockToolUse, ID: "b1", Name: "bash", Text: "hi"}, "text"},
		{"tool_use carrying a signature", Block{Kind: BlockToolUse, ID: "b1", Name: "bash", Signature: "s"}, "signature"},
		{"tool_use carrying a toolUseId", Block{Kind: BlockToolUse, ID: "b1", Name: "bash", ToolUseID: "t"}, "toolUseId"},
		{"tool_use carrying content", Block{Kind: BlockToolUse, ID: "b1", Name: "bash", Content: "out"}, "content"},
		{"tool_use carrying isError", Block{Kind: BlockToolUse, ID: "b1", Name: "bash", IsError: true}, "isError"},

		{"tool_result carrying text", Block{Kind: BlockToolResult, ToolUseID: "t1", Text: "hi"}, "text"},
		{"tool_result carrying an id", Block{Kind: BlockToolResult, ToolUseID: "t1", ID: "b1"}, "id"},
		{"tool_result carrying a name", Block{Kind: BlockToolResult, ToolUseID: "t1", Name: "bash"}, "name"},
		{"tool_result carrying input", Block{Kind: BlockToolResult, ToolUseID: "t1", Input: json.RawMessage(`{}`)}, "input"},
		{"tool_result carrying a signature", Block{Kind: BlockToolResult, ToolUseID: "t1", Signature: "s"}, "signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.block.Validate()
			if err == nil {
				t.Fatalf("accepted a %s block carrying %q", tc.block.Kind, tc.field)
			}
			// The message names the field, or a caller has to guess which of
			// seven possibilities was wrong.
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("the error does not name %q: %v", tc.field, err)
			}
		})
	}
}

// A `default:` that permits an unknown kind is the hole through which a block
// from a future format version reaches the wire and is dropped silently.
func TestValidateRejectsAnUnknownKind(t *testing.T) {
	for _, kind := range []BlockKind{"image", "video", "TEXT", "tool-use", ""} {
		err := Block{Kind: kind, Text: "x"}.Validate()
		if err == nil {
			t.Errorf("accepted kind %q", kind)
		}
	}
}

func TestEveryConstructorProducesAValidBlock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block Block
	}{
		{"TextBlock", TextBlock("hello")},
		{"ThinkingBlock", ThinkingBlock("reasoning", "sig-abc")},
		{"ThinkingBlock unsigned", ThinkingBlock("reasoning", "")},
		{"ToolUseBlock", ToolUseBlock("tu_1", "bash", json.RawMessage(`{"cmd":"ls"}`))},
		{"ToolResultBlock ok", ToolResultBlock("tu_1", "file.txt", false)},
		{"ToolResultBlock error", ToolResultBlock("tu_1", "no such file", true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.block.Validate(); err != nil {
				t.Fatalf("%s produced an invalid block: %v", tc.name, err)
			}
		})
	}
}

func TestAToolUseBlockWithoutAnIDOrNameIsRejected(t *testing.T) {
	if err := (Block{Kind: BlockToolUse, Name: "bash"}).Validate(); err == nil {
		t.Error("accepted a tool_use with no id; a tool result could never be matched to it")
	}
	if err := (Block{Kind: BlockToolUse, ID: "tu_1"}).Validate(); err == nil {
		t.Error("accepted a tool_use with no name")
	}
	if err := (Block{Kind: BlockToolResult, Content: "x"}).Validate(); err == nil {
		t.Error("accepted a tool_result with no toolUseId")
	}
}

// D2's foundation, and S7's dependency: BYTE equality, not deep equality.
//
// Byte comparison is what catches a RawMessage renormalised by the encoder —
// reordered keys, reformatted numbers — which would make a replayed tool call
// differ from the original one.
func TestEachBlockKindRoundTripsByteIdentically(t *testing.T) {
	for _, b := range []Block{
		TextBlock("hello world"),
		ThinkingBlock("let me think", "sig-9f8e7d"),
		ToolUseBlock("tu_1", "bash", json.RawMessage(`{"cmd":"ls -la","timeout":30}`)),
		ToolResultBlock("tu_1", "total 0", false),
		ToolResultBlock("tu_1", "command not found", true),
	} {
		t.Run(string(b.Kind)+"/"+b.ID+b.ToolUseID, func(t *testing.T) {
			first, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			var back Block
			if err := json.Unmarshal(first, &back); err != nil {
				t.Fatal(err)
			}
			second, err := json.Marshal(back)
			if err != nil {
				t.Fatal(err)
			}
			if string(first) != string(second) {
				t.Fatalf("not byte-identical:\n  first  %s\n  second %s", first, second)
			}
			if err := back.Validate(); err != nil {
				t.Errorf("a round-tripped block no longer validates: %v", err)
			}
		})
	}
}

// M17 — asserted on the EXACT string, not on presence. Anthropic rejects a
// tool-use turn whose prior thinking was altered, and S7 replays stored
// transcripts, so a mangled signature fails much later and somewhere else.
func TestASignedThinkingBlockSurvivesByteIdentically(t *testing.T) {
	// Deliberately awkward: base64-ish padding, slashes, plus signs — the
	// characters a careless encoder mangles.
	const signature = "ErUBCkYIBBgCKkBw+Zm3/Lx1Qm9kZXI=//abc+def==ghi"
	original := ThinkingBlock("I should check the tests first.", signature)

	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var back Block
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Signature != signature {
		t.Fatalf("the signature changed:\n  want %q\n  got  %q", signature, back.Signature)
	}
	// And through a whole transcript, which is the shape S4 stores.
	msgs := []Message{{Role: RoleAssistant, Content: []Block{original}}}
	rawMsgs, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	var backMsgs []Message
	if err := json.Unmarshal(rawMsgs, &backMsgs); err != nil {
		t.Fatal(err)
	}
	if backMsgs[0].Content[0].Signature != signature {
		t.Fatalf("the signature did not survive a transcript round trip: %q",
			backMsgs[0].Content[0].Signature)
	}
}

// O2 — the struct tag that could undo M9 silently.
//
// # This asserts the TAG, not only the behaviour, and the reason matters
//
// The behavioural test came first and it was not a guard. `omitempty` omits
// only the ZERO value, and `"known"` is not empty — so adding `omitempty` to
// `State` changes nothing observable today and the test stayed green. A
// negative control caught that.
//
// The tag is still wrong, because it is only safe **in combination with
// CostState's current constants**. The day `CostState` becomes an int, or a
// constant is reordered so that `known` is the zero value, a known cost
// serialises with no state at all and a reader cannot tell it from an absent
// one. That is a two-step failure, and the second step would be in a different
// file months later — so the guard belongs on the tag.
func TestCostStateHasNoOmitemptyAndSerialisesForAKnownZeroCost(t *testing.T) {
	field, ok := reflect.TypeOf(Cost{}).FieldByName("State")
	if !ok {
		t.Fatal("Cost has no State field")
	}
	if strings.Contains(field.Tag.Get("json"), "omitempty") {
		t.Errorf(`Cost.State is tagged %q. It must have no omitempty (O2): the day CostState's `+
			`zero value becomes a meaningful state, a known cost would serialise without one `+
			`and be indistinguishable from an absent cost`, field.Tag.Get("json"))
	}

	raw, err := json.Marshal(Cost{State: CostKnown, USD: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"state":"known"`) {
		t.Fatalf("a known zero cost did not serialise its state: %s", raw)
	}
	unknown, _ := json.Marshal(Cost{State: CostUnknown, Reason: "no usage reported"})
	if string(raw) == string(unknown) {
		t.Fatal("a known zero cost and an unknown cost serialise identically")
	}
}

// The same guard for the other tag O3 names, for the same reason: `IsError`'s
// behavioural test works today only because `false` happens to be the zero
// value. Keeping both guards structural means neither depends on which
// constant is zero.
func TestBlockIsErrorHasNoOmitempty(t *testing.T) {
	field, ok := reflect.TypeOf(Block{}).FieldByName("IsError")
	if !ok {
		t.Fatal("Block has no IsError field")
	}
	if strings.Contains(field.Tag.Get("json"), "omitempty") {
		t.Errorf("Block.IsError is tagged %q; with omitempty a failed and a successful tool "+
			"result serialise identically (O3)", field.Tag.Get("json"))
	}
}

// O3 — a failed tool result and a successful one must differ on the wire.
func TestBlockIsErrorSerialisesWhenFalse(t *testing.T) {
	ok, err := json.Marshal(ToolResultBlock("tu_1", "fine", false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ok), `"isError":false`) {
		t.Fatalf("isError was omitted when false, so a failed and a successful tool result "+
			"serialise identically: %s", ok)
	}
	bad, _ := json.Marshal(ToolResultBlock("tu_1", "fine", true))
	if string(ok) == string(bad) {
		t.Fatal("a failed and a successful tool result serialise identically")
	}
}

// M9 — the distinction the pointer fields exist for.
func TestAnAbsentUsageFieldIsDistinguishableFromZero(t *testing.T) {
	absent := Usage{}
	zero := Usage{InputTokens: IntPtr(0)}

	if !absent.Empty() {
		t.Error("an empty Usage does not report itself empty")
	}
	if zero.Empty() {
		t.Error("a Usage with a reported zero reports itself empty — that is the confusion M9 exists to prevent")
	}

	rawAbsent, _ := json.Marshal(absent)
	rawZero, _ := json.Marshal(zero)
	if string(rawAbsent) == string(rawZero) {
		t.Fatalf("absent and zero serialise identically: %s", rawAbsent)
	}
	if !strings.Contains(string(rawZero), `"inputTokens":0`) {
		t.Fatalf("a reported zero did not survive: %s", rawZero)
	}

	// And back again: the distinction must survive a round trip, since this is
	// what S4 stores and S9 renders.
	var back Usage
	if err := json.Unmarshal(rawZero, &back); err != nil {
		t.Fatal(err)
	}
	if back.InputTokens == nil || *back.InputTokens != 0 {
		t.Fatal("a reported zero became absent in a round trip")
	}
	var backAbsent Usage
	if err := json.Unmarshal(rawAbsent, &backAbsent); err != nil {
		t.Fatal(err)
	}
	if backAbsent.InputTokens != nil {
		t.Fatal("an absent field became present in a round trip")
	}
}

// An API key inside an event payload or a log line is the accident this slice
// is most likely to have. `json:"-"` means the accident cannot leak it.
func TestARouteNeverSerialisesItsAPIKey(t *testing.T) {
	raw, err := json.Marshal(Route{
		ID: "anthropic-sonnet", Provider: "anthropic", Model: "claude-sonnet-5",
		APIKey: "sk-ant-SECRET-VALUE", MaxOutputTokens: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "apiKey") {
		t.Fatalf("a Route serialised its API key: %s", raw)
	}
}

// The prices say their unit in the field name, because a factor-of-a-million
// error looks plausible in both directions.
func TestPriceFieldNamesStateTheirUnit(t *testing.T) {
	raw, err := json.Marshal(Route{Prices: &Prices{InputUSDPerMTok: Float64Ptr(3)}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "UsdPerMTok") {
		t.Fatalf("the price field does not name its unit: %s", raw)
	}
}

func TestModelRequestValidationNamesWhereItFailed(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ModelRequest
		want string
	}{
		{"no messages", ModelRequest{}, "no messages"},
		{"empty content", ModelRequest{Messages: []Message{{Role: RoleUser}}}, "empty content"},
		{
			"an invalid block",
			ModelRequest{Messages: []Message{{Role: RoleUser, Content: []Block{
				{Kind: BlockText, Text: "hi", ToolUseID: "t1"},
			}}}},
			"block 0",
		},
		{
			"a nameless tool",
			ModelRequest{
				Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock("hi")}}},
				Tools:    []ToolSpec{{Description: "does things"}},
			},
			"tool 0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if err == nil {
				t.Fatal("accepted an invalid request")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the error does not say where: %v", err)
			}
		})
	}
}

func TestAValidRequestPasses(t *testing.T) {
	req := ModelRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: []Block{TextBlock("be brief")}},
			{Role: RoleUser, Content: []Block{TextBlock("hello")}},
		},
		Tools: []ToolSpec{{Name: "bash", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
}

// I1 — this package is a STRICT leaf: it imports nothing under `internal/`.
//
// # Stricter than the brief, and it has to be
//
// I1 permits importing `protocol`'s error helpers. **That permission cannot be
// taken**, because T10 puts the `ask` method in `internal/protocol`, so
// `protocol` imports `model` — and `model` importing `protocol` back would be
// an import cycle. Demonstrated, not assumed: the same cycle T5's layout
// implied.
//
// So this package returns plain Go errors, and `protocol/ask.go` maps them onto
// Aeon codes — which is exactly how `internal/daemon` already maps protocol
// errors onto wire errors. The mapping lives with the layer that owns the
// vocabulary.
//
// Recorded as a deviation in T10's notes.
func TestModelIsAStrictLeafPackage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no source files found; this test would pass vacuously")
	}

	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		// A TEST file in the external test package (`model_test`) is not a
		// dependency edge of the package at all — the toolchain compiles it
		// separately, so adapters can be imported there to register the
		// real wire for embed_wire_test.go. Production files keep the
		// strict rule, which is the half the cycle argument is about.
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if !strings.Contains(path, "github.com/cjlapao/aeon/internal/") {
				continue
			}
			t.Errorf("%s imports %q. internal/model is a strict leaf: `protocol` imports it "+
				"for the `ask` method, so importing anything back is a cycle (I1, amended)",
				file, path)
		}
	}
}

// readSource reads a file in this package, for the tests that assert a
// limitation or a guess is documented in the source itself — a limitation only
// in a design document is a limitation nobody reads.
func readSource(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}
