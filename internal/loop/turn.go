package loop

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// turn.go is S39 T6's conversation half (CR3): a turn is an ordinary step
// spoken by the loop's conversation agent, and everything it needs from the
// engine is here, pure — the daemon owns the running, this file owns the
// saying.

// TurnThreadWindow bounds the thread the model sees, oldest trimmed. Forty
// is a conversation, not a transcript: the person's last few exchanges plus
// enough history to not ask "which file?" twice. Trimming SAYS SO — a
// silent cut is a model that hallucinates it saw everything.
const TurnThreadWindow = 40

// ConverseAgent is the precedence: the blueprint's converse: agent (the
// author named it at pin time), else the daemon's default (T0-Q3). A nil
// blueprint has no opinion.
func ConverseAgent(bp *blueprint.Blueprint, def string) string {
	if bp != nil && bp.Converse != "" {
		return bp.Converse
	}
	return def
}

// ThreadTurn is one row of the conversation as it rides the wire (S68, B4):
// a real alternating message, not a line in a flattened blob. Role is "user"
// or "assistant" — the two conversation roles.
type ThreadTurn struct {
	Role string // "user" | "assistant"
	Text string
	// Attachments is the row's receipt as filed (S69 AT2) — references,
	// never bytes, and never rendered into Text: the bubble and the wire
	// each read them from here, and the expansion beside the adapters is
	// the only door that turns them into content.
	Attachments []store.AttachmentReceipt
}

// TurnWords is the USER message for one conversation turn (S68, B3): the
// person's new message verbatim — the row just filed. No goal prefix, no
// `[author]` decoration, no thread: the thread rides as history messages
// under it, and the frame above it. B9: empty words (a defensive door —
// the caller validates first) speak the fallback rather than ship silence.
func TurnWords(text string) string {
	if strings.TrimSpace(text) == "" {
		return ContinueFallbackText
	}
	return text
}

// TurnParts renders the two SYSTEM-side halves of one conversation turn
// (S68, B2/B4): the frame the model is spoken in, and the thread as REAL
// alternating messages — no flattened `[author] body` blob anywhere.
// Deterministic — same rows, same counts, same bytes (P29's replay
// promise) — and it NEVER shows a message's source (S39 T6 CR6: a typed
// message and a templated kickoff read the same to the model; provenance is
// for the human's thread and the audit, not a prompt hint to play to).
//
// The row with newWordsID is the person's message being answered: it rides
// the USER message (TurnWords), never twice. System-side authors ("system",
// "automation", "blueprint") never ride the wire (B4) — they exist for the
// human's thread and the audit. The window trims SURVIVORS to forty
// ("last 40 of those authors"), and the frame says so honestly when it did.
func TurnParts(st LoopState, thread []store.ChatMessage, newWordsID int64, window int) (string, []ThreadTurn) {
	if window < 1 {
		window = TurnThreadWindow
	}
	speaking := make([]store.ChatMessage, 0, len(thread))
	for _, m := range thread {
		if m.ID == newWordsID {
			continue // rides as the user message itself
		}
		switch m.Author {
		case "system", "automation", "blueprint":
			continue // the system's voice never rides the wire
		}
		if strings.TrimSpace(m.Body) == "" {
			continue // an empty message is accepted by nobody
		}
		speaking = append(speaking, m)
	}
	omitted := 0
	if len(speaking) > window {
		omitted = len(speaking) - window
		speaking = speaking[omitted:]
	}
	out := make([]ThreadTurn, 0, len(speaking))
	for _, m := range speaking {
		role := "user"
		if m.Author == "agent" {
			role = "assistant"
		}
		out = append(out, ThreadTurn{Role: role, Text: m.Body, Attachments: m.Attachments})
	}

	var b strings.Builder
	b.WriteString("You are in a conversation with a person. Reply to their last message. ")
	b.WriteString("You may call your tools when they ask for something.\n")
	b.WriteString("The loop's goal: ")
	b.WriteString(goalText(st))
	if omitted > 0 {
		fmt.Fprintf(&b, "\nEarlier messages omitted: %d.", omitted)
	}
	return b.String(), out
}
