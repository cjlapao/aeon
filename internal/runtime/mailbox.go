package runtime

// # The agent↔agent mailbox at the step loop (S70 — MB3, MB4, MB6)
//
// Two seams, each the size of the question it answers, each nil-means-off
// exactly as Steers, Policy and Telemetry arrive (I2/I3): a step built
// before S70 runs the code it ran before, byte for byte.
//
//   - `MailboxDrain` is the READING arm: at the first boundary (the same
//     pre-call boundary LW1 puts steers at) the step asks its adapter for
//     messages addressed to it. The adapter closes over the step's scope —
//     which loop, which agent — exactly as the steer adapter closes over the
//     step's queue; the runtime never learns what a scope is. The words are
//     folded as an ordinary user turn (AppendUserTurn, the one fold) and the
//     rows flip DELIVERED inside the same checkpoint transaction that
//     persists the fold (D4's law, quoted from steers: a stamp after the
//     commit is the window where a double delivery hides).
//
//   - `MailboxAskDoor` is the WRITING arm: `ask_agent` is recognised by name
//     like `question` (K18 — the tool's Run panics; the runtime owns the
//     effect), and the runtime hands the RAW arguments to the door. The door
//     — the daemon's adapter — parses with the ONE parser (AT1: the parser
//     lives in `internal/tools`, the dependency runs that way, and
//     validation before persist is a property of the persist path), checks
//     the addressee against the loop's pinned lanes (MB2), lets the frozen
//     policy set judge the call (D12), and files the row. The runtime gets
//     back an outcome, not a row: an answer to give the tool_use, or a
//     `Suspends` with the question the pause is visible as (D16) — and then
//     suspension rides `suspendWith`, the ONE constructor (S6's shape; a
//     second way to suspend is how sibling results start going missing).
//
// The reading order at a boundary: the person's steers first, the peers'
// messages after — a message from the person outranks a message from a
// colleague, and both arrive before the model speaks.

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/cjlapao/aeon/internal/model"
)

// MailboxMessage is one addressed message as the RUNTIME sees it — the
// adapter reads the store row and hands over what the transcript needs.
// DeliveredAt is deliberately absent: the stamp is the checkpoint's doing,
// and a runtime that carried timestamps would be a runtime that invents
// them (the clock belongs to the writer).
type MailboxMessage struct {
	// ID names the durable row; it rides the delivered event and the
	// checkpoint's flip list, which is what makes "pending with its text
	// already in the transcript" unrepresentable.
	ID int64

	// FromAgent is the speaking agent's name — the frame line's subject.
	FromAgent string

	// Body is the ask or answer, word for word as filed. It lands in the
	// transcript byte-identical (AT7): the frame above it is composition,
	// never editing.
	Body string
}

// MailboxDrain is the reading arm: called at the pre-call boundary only
// (LW1's rule, inherited), ONCE per boundary, and an adapter that could not
// read hands back nil — nothing delivered THIS boundary, nothing lost: the
// rows are pending, the next boundary asks again (the steer source's honest
// reading, quoted).
type MailboxDrain interface {
	Pending(ctx context.Context) []MailboxMessage
}

// AskOutcome is the door's answer to one ask. The runtime never sees a row;
// it sees what the turn must do next.
type AskOutcome struct {
	// MessageID and Thread name what was filed, for the event.
	MessageID int64
	Thread    int64

	// Result answers the tool_use when the step keeps running (wait:false,
	// or any outcome the door decides does not pause the step).
	Result string

	// Suspends is true when the door filed an ask whose answer must arrive
	// before this step speaks again. Question is then the wait made visible
	// (D16: a suspend the UI cannot see is a hang) and the blocked tool_use
	// stays UNANSWERED — the answer resumes it, the permission ask's shape,
	// quoted.
	Suspends bool
	Question *PendingQuestion
}

// MailboxAskDoor is the writing arm. `input` is the tool_use's raw
// arguments: the ONE parser lives above the door in internal/tools (the
// dependency direction is tools→runtime never, runtime→tools never — the
// adapter imports both), and every refusal comes back as an error naming
// the reason and the thing refused (AGENTS §5), which the loop answers the
// tool_use with honestly.
type MailboxAskDoor interface {
	Ask(ctx context.Context, input json.RawMessage) (AskOutcome, error)
}

// mailboxAskResult closes an ask's tool_use with the door's answer (or its
// refusal) — the model reads the ask as answered, honestly, either way.
func mailboxAskResult(use model.Block, text string, isErr bool) model.Block {
	if text == "" {
		text = "the ask was filed"
	}
	return model.ToolResultBlock(use.ID, text, isErr)
}

// mailboxWireText is the drain's ONE composition point (MB4): the frame
// line, a blank line, the words untouched below it — steerWireText's shape
// for a different sender. The frame says WHO and WHICH row; the body is
// byte-identical from the row to the wire, which is what AT7 pins.
func mailboxWireText(m MailboxMessage) string {
	return "message from " + m.FromAgent + " (mailbox " +
		strconv.FormatInt(m.ID, 10) + "):\n\n" + m.Body
}
