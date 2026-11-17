package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func user(text string) Message {
	return Message{Role: RoleUser, Content: []Block{TextBlock(text)}}
}
func assistant(blocks ...Block) Message {
	return Message{Role: RoleAssistant, Content: blocks}
}

func TestValidateTranscriptRejectsTwoConsecutiveAssistantMessagesAndNamesTheIndex(t *testing.T) {
	res := ValidateTranscript([]Message{
		user("hello"),
		assistant(TextBlock("hi")),
		assistant(TextBlock("also hi")),
	})
	if res.OK {
		t.Fatal("accepted two consecutive assistant messages; no provider does")
	}
	// The index must be the SECOND one — the one to remove or merge.
	if res.Index != 2 {
		t.Errorf("Index = %d, want 2 (the second of the pair, which is the one to fix)", res.Index)
	}
	if !strings.Contains(res.Reason, "assistant") {
		t.Errorf("the reason does not say what was wrong: %q", res.Reason)
	}
}

func TestValidateTranscriptRejectsATranscriptBeginningWithAssistant(t *testing.T) {
	res := ValidateTranscript([]Message{assistant(TextBlock("hi")), user("hello")})
	if res.OK {
		t.Fatal("accepted a transcript opening with an assistant turn")
	}
	if res.Index != 0 {
		t.Errorf("Index = %d, want 0", res.Index)
	}
}

func TestValidateTranscriptRejectsAToolResultWithNoEarlierToolUse(t *testing.T) {
	res := ValidateTranscript([]Message{
		user("run ls"),
		Message{Role: RoleUser, Content: []Block{ToolResultBlock("tu_never", "out", false)}},
	})
	if res.OK {
		t.Fatal("accepted a tool_result matching no tool_use")
	}
	if !strings.Contains(res.Reason, "tu_never") {
		t.Errorf("the reason does not name the id: %q", res.Reason)
	}
}

// A tool_result answering a tool_use that appears LATER. The ordering matters:
// a provider matches by id and would pair them, then fail confusingly.
func TestValidateTranscriptRejectsAToolResultBeforeItsToolUse(t *testing.T) {
	res := ValidateTranscript([]Message{
		user("run ls"),
		Message{Role: RoleUser, Content: []Block{ToolResultBlock("tu_1", "out", false)}},
		assistant(ToolUseBlock("tu_1", "bash", json.RawMessage(`{}`))),
	})
	if res.OK {
		t.Fatal("accepted a tool_result preceding its tool_use")
	}
}

// THE case a future contributor will read as a bug.
//
// A suspended transcript ends on an assistant turn holding an unanswered
// `tool_use` — which is exactly what a pending question looks like (S6) and the
// normal resting state of a waiting loop. A validator requiring every tool_use
// to be answered would reject every suspended step.
func TestValidateTranscriptAcceptsASuspendedTranscript(t *testing.T) {
	res := ValidateTranscript([]Message{
		user("refactor the auth module"),
		assistant(
			TextBlock("I need to ask you something first."),
			ToolUseBlock("tu_q1", "question", json.RawMessage(`{"prompt":"which module?"}`)),
		),
	})
	if !res.OK {
		t.Fatalf("rejected a SUSPENDED transcript: %s (index %d).\n"+
			"An unanswered tool_use in the FINAL assistant turn is a waiting loop, not a lost "+
			"tool result — this is the normal resting state of a suspended step (S6)", res.Reason, res.Index)
	}
}

// The complement, which is what the exception must not swallow: an unanswered
// tool_use in a MIDDLE turn is a genuinely lost tool result.
func TestValidateTranscriptRejectsAnUnansweredToolUseThatIsNotTheFinalTurn(t *testing.T) {
	res := ValidateTranscript([]Message{
		user("run ls"),
		assistant(ToolUseBlock("tu_lost", "bash", json.RawMessage(`{}`))),
		user("never mind"),
		assistant(TextBlock("ok")),
	})
	if res.OK {
		t.Fatal("accepted a transcript with a lost tool result in a middle turn")
	}
	if !strings.Contains(res.Reason, "tu_lost") {
		t.Errorf("the reason does not name the tool_use: %q", res.Reason)
	}
	if res.Index != 1 {
		t.Errorf("Index = %d, want 1 (where the unanswered tool_use is)", res.Index)
	}
}

func TestValidateTranscriptRejectsEmptyContent(t *testing.T) {
	res := ValidateTranscript([]Message{user("hi"), {Role: RoleAssistant, Content: nil}})
	if res.OK {
		t.Fatal("accepted a message with no content")
	}
	if res.Index != 1 {
		t.Errorf("Index = %d, want 1", res.Index)
	}
}

func TestValidateTranscriptRejectsADuplicateToolUseID(t *testing.T) {
	res := ValidateTranscript([]Message{
		user("run it twice"),
		assistant(
			ToolUseBlock("tu_same", "bash", json.RawMessage(`{"cmd":"ls"}`)),
			ToolUseBlock("tu_same", "bash", json.RawMessage(`{"cmd":"pwd"}`)),
		),
	})
	if res.OK {
		t.Fatal("accepted two tool_use blocks with one id; a tool result could match either")
	}
}

func TestValidateTranscriptAcceptsACompleteToolRoundTrip(t *testing.T) {
	res := ValidateTranscript([]Message{
		{Role: RoleSystem, Content: []Block{TextBlock("be brief")}},
		user("list the files"),
		assistant(
			ThinkingBlock("I should use bash", "sig-1"),
			ToolUseBlock("tu_1", "bash", json.RawMessage(`{"cmd":"ls"}`)),
		),
		{Role: RoleUser, Content: []Block{ToolResultBlock("tu_1", "a.go b.go", false)}},
		assistant(TextBlock("Two files: a.go and b.go.")),
	})
	if !res.OK {
		t.Fatalf("rejected a valid transcript: %s (index %d)", res.Reason, res.Index)
	}
}

// It is called on data read from a database, so a panic there takes down a
// recovery. The table is every malformed shape that could be stored.
func TestValidateTranscriptNeverPanics(t *testing.T) {
	cases := [][]Message{
		nil,
		{},
		{{}},
		{{Role: "", Content: nil}},
		{{Role: "nonsense", Content: []Block{TextBlock("x")}}},
		{{Role: RoleUser, Content: []Block{{}}}},
		{{Role: RoleUser, Content: []Block{{Kind: "unknown-kind"}}}},
		{{Role: RoleUser, Content: []Block{{Kind: BlockToolResult}}}},
		{{Role: RoleUser, Content: []Block{{Kind: BlockToolUse}}}},
		{user("hi"), {Role: RoleAssistant, Content: []Block{{Kind: BlockToolUse, ID: "x"}}}},
		{{Role: RoleAssistant, Content: []Block{TextBlock("first")}}},
	}
	for i, msgs := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("case %d panicked: %v", i, r)
				}
			}()
			res := ValidateTranscript(msgs)
			// Every case must come back with a result — and a rejection must
			// carry a reason, or the caller has nothing to log.
			if !res.OK && res.Reason == "" {
				t.Errorf("case %d was rejected with no reason", i)
			}
		}()
	}
}

func TestMigrateTranscriptRefusesAFutureVersionNamingBoth(t *testing.T) {
	stored, err := json.Marshal(StoredTranscript{
		FormatVersion: FormatVersion + 1,
		Messages:      []Message{user("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := MigrateTranscript(stored)
	if res.OK() {
		t.Fatal("parsed a transcript from the future; fields this build does not know would be " +
			"dropped silently — plausibly a thinking signature")
	}
	if res.Outcome != MigrationFromTheFuture {
		t.Errorf("Outcome = %q, want %q", res.Outcome, MigrationFromTheFuture)
	}
	for _, want := range []string{"2", "1"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("the refusal does not name %q: %q", want, res.Reason)
		}
	}
}

// Distinguishable from the future case: the two need different fixes, and
// conflating them sends somebody to the wrong one.
func TestMigrateTranscriptRefusesALowerVersionWithADistinguishableReason(t *testing.T) {
	stored, _ := json.Marshal(StoredTranscript{FormatVersion: 0, Messages: []Message{user("hi")}})
	res := MigrateTranscript(stored)
	if res.OK() {
		t.Fatal("accepted a version with no migration")
	}
	if res.Outcome != MigrationTooOld {
		t.Fatalf("Outcome = %q, want %q — the two refusals must be distinguishable",
			res.Outcome, MigrationTooOld)
	}
	if res.Outcome == MigrationFromTheFuture {
		t.Error("a too-old transcript reported as from-the-future")
	}
}

func TestMigrateTranscriptHandlesGarbageWithoutPanicking(t *testing.T) {
	for _, in := range [][]byte{
		nil,
		[]byte(""),
		[]byte("not json"),
		[]byte("{"),
		[]byte(`{"formatVersion":"one"}`),
		[]byte(`{"formatVersion":1,"messages":"not an array"}`),
		[]byte(`[]`),
		[]byte(`null`),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("MigrateTranscript(%q) panicked: %v", in, r)
				}
			}()
			res := MigrateTranscript(in)
			if res.OK() {
				t.Errorf("MigrateTranscript(%q) reported OK", in)
			}
			if res.Reason == "" {
				t.Errorf("MigrateTranscript(%q) gave no reason", in)
			}
		}()
	}
}

func TestMigrateTranscriptAcceptsTheCurrentVersion(t *testing.T) {
	msgs := []Message{
		user("hi"),
		assistant(ThinkingBlock("thinking", "sig-xyz"), TextBlock("hello")),
	}
	stored, err := StoreTranscript(msgs)
	if err != nil {
		t.Fatal(err)
	}
	res := MigrateTranscript(stored)
	if !res.OK() {
		t.Fatalf("round trip failed: %s", res.Reason)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(res.Messages))
	}
	// The signature survives the store/load cycle — this is S7's dependency.
	if got := res.Messages[1].Content[0].Signature; got != "sig-xyz" {
		t.Fatalf("the signature did not survive storage: %q", got)
	}
}

// An envelope that parses can still hold an invalid transcript — a corrupt row,
// or one written by a build with a bug. Handing that to a recovery is worse
// than refusing it.
func TestMigrateTranscriptRefusesAParseableButInvalidTranscript(t *testing.T) {
	stored, _ := json.Marshal(StoredTranscript{
		FormatVersion: FormatVersion,
		Messages:      []Message{assistant(TextBlock("starts with assistant"))},
	})
	res := MigrateTranscript(stored)
	if res.OK() {
		t.Fatal("accepted a parseable but invalid transcript")
	}
	if !strings.Contains(res.Reason, "not valid") {
		t.Errorf("the reason does not say it parsed but failed validation: %q", res.Reason)
	}
}

func TestStoreTranscriptRefusesToStoreSomethingInvalid(t *testing.T) {
	_, err := StoreTranscript([]Message{assistant(TextBlock("hi"))})
	if err == nil {
		t.Fatal("stored an invalid transcript")
	}
	if !strings.Contains(err.Error(), "message 0") {
		t.Errorf("the refusal does not name the index: %v", err)
	}
}
