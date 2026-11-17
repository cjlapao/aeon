package loop

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// turn_test.go pins the pure half of a conversation turn — rewritten by
// S68 (B3/B4): the thread rides as REAL alternating messages, the person's
// new words ride the user message ALONE and verbatim, the frame rides the
// system message, and provenance never reaches the model (S39 T6 CR6 stands).

func threadOf(pairs ...[2]string) []store.ChatMessage {
	var out []store.ChatMessage
	for i, p := range pairs {
		out = append(out, store.ChatMessage{ID: int64(i + 1), Seq: int64(i + 1), Author: p[0], Body: p[1]})
	}
	return out
}

func TestTurnWordsIsThePersonsMessageVerbatim(t *testing.T) {
	// B3: no prefix, no trim, no decoration — what the person typed is what
	// rides, and what the chat window shows as "you".
	got := TurnWords("  fix the login page  please ")
	if got != "  fix the login page  please " {
		t.Fatalf("words were edited on the way to the wire: %q", got)
	}
	// B9: nothing human to answer speaks the ONE fallback sentence — never
	// an empty turn.
	if TurnWords("") != ContinueFallbackText || TurnWords("   \n ") != ContinueFallbackText {
		t.Fatal("empty words must speak the fallback sentence verbatim")
	}
}

func TestTurnWordsAndFrameSeparateTheVoices(t *testing.T) {
	// B2/B3: the conversation instruction prose is INSTRUCTION — it rides
	// the frame (system message), and the person's words contain none of it.
	st := LoopState{Goal: "ship the thing"}
	frame, _ := TurnParts(st, nil, 0, TurnThreadWindow)
	if !strings.Contains(frame, "You are in a conversation with a person") ||
		!strings.Contains(frame, "The loop's goal: ship the thing") {
		t.Fatalf("the frame lost its conversation promise or its goal:\n%s", frame)
	}
	// The goal is NOT re-speaked inside the words (pre-S68 the user message
	// carried `Goal:` lines; now the words are the words, the frame carries
	// the goal).
	if strings.Contains(TurnWords("hello"), "ship the thing") {
		t.Fatal("the goal leaked into the person's message")
	}
}

func TestTurnPartsSpreadsTheThreadRealTurns(t *testing.T) {
	thread := threadOf(
		[2]string{"human", "hello"},
		[2]string{"agent", "hi — what shall we build?"},
		[2]string{"human", "the thing"},
		[2]string{"system", "the turn could not run: boom"}, // never rides (B4)
		[2]string{"automation", "trigger fired"},            // never rides
		[2]string{"agent", ""},                              // empty rides nowhere
		[2]string{"agent", "here it is"},
	)
	// The row being answered (id 3, the person's just-filed message) rides
	// as the user message — never twice, never in the history.
	_, history := TurnParts(LoopState{Goal: "g"}, thread, 3, TurnThreadWindow)
	want := []ThreadTurn{
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "hi — what shall we build?"},
		{Role: "assistant", Text: "here it is"},
	}
	if len(history) != len(want) {
		t.Fatalf("history shape drifted: %+v", history)
	}
	for i, w := range want {
		if history[i].Role != w.Role || history[i].Text != w.Text || len(history[i].Attachments) != 0 {
			t.Fatalf("turn %d: got %+v want %+v", i, history[i], w)
		}
	}
	// The system's voice and the automation's note are absent BY ROLE, not
	// by chance; and only the two conversation roles exist on it at all.
	for _, h := range history {
		if strings.Contains(h.Text, "boom") || strings.Contains(h.Text, "trigger") || h.Text == "the thing" {
			t.Fatalf("a row that must not ride the wire is on it: %+v", h)
		}
		if h.Role != "user" && h.Role != "assistant" {
			t.Fatalf("role %q never rides (B4)", h.Role)
		}
	}
}

func TestTurnPartsTrimsToTheWindowAndSaysSo(t *testing.T) {
	// The window counts SURVIVORS ("last 40 of those authors"), and the
	// frame — not the words — carries the honest omission note.
	var pairs [][2]string
	for i := 0; i < 45; i++ {
		pairs = append(pairs, [2]string{"human", "m"})
	}
	frame, history := TurnParts(LoopState{Goal: "g"}, threadOf(pairs...), 0, 40)
	if len(history) != 40 {
		t.Fatalf("%d turns kept, want exactly 40 (the LAST forty, oldest first)", len(history))
	}
	if !strings.Contains(frame, "Earlier messages omitted: 5.") {
		t.Fatalf("the frame must name the cut honestly:\n%s", frame)
	}
	if strings.Contains(frame, "[human]") || strings.Contains(frame, "[agent]") {
		t.Fatal("the flattened blob format is back")
	}
}

func TestTurnPartsIsIdenticalForUserAndTemplateSources(t *testing.T) {
	// CR6 survives the split: typed and templated read the SAME to the
	// model — neither frame nor history carries a source.
	plain := threadOf([2]string{"human", "start the goal"}, [2]string{"agent", "sure"})
	templated := threadOf([2]string{"human", "start the goal"}, [2]string{"agent", "sure"})
	plain[0].Source = "user"
	templated[0].Source = "template"
	fa, ha := TurnParts(LoopState{Goal: "g"}, plain, 0, TurnThreadWindow)
	fb, hb := TurnParts(LoopState{Goal: "g"}, templated, 0, TurnThreadWindow)
	if fa != fb {
		t.Fatalf("the source leaked into the frame — CR6:\n%q\n%q", fa, fb)
	}
	if len(ha) != len(hb) {
		t.Fatal("the source leaked into the history length")
	}
	for i := range ha {
		if !reflect.DeepEqual(ha[i], hb[i]) {
			t.Fatalf("the source leaked into history turn %d — CR6", i)
		}
	}
}

func TestConverseAgent(t *testing.T) {
	if got := ConverseAgent(&blueprint.Blueprint{Converse: "brainstormer"}, DefaultConverseAgentForTest); got != "brainstormer" {
		t.Fatalf("the blueprint's converse must win, got %q", got)
	}
	if got := ConverseAgent(&blueprint.Blueprint{}, "default-agent"); got != "default-agent" {
		t.Fatalf("an unnamed converse falls back, got %q", got)
	}
	if got := ConverseAgent(nil, "default-agent"); got != "default-agent" {
		t.Fatalf("a nil blueprint has no opinion, got %q", got)
	}
}

// DefaultConverseAgentForTest keeps this package out of the daemon's chosen
// default — precedence is the rule under test here, not the name.
const DefaultConverseAgentForTest = "some-default"
