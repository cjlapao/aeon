package narrow

import (
	"fmt"
	"strings"
)

// BoundaryStatement is the surface's own account of its absence (N14, O3):
// what exists, what is ABSENT and stays absent, why, and what to use
// instead. It is mounted where clients DISPLAY it — the MCP initialize
// result and every tool description point back at it (T3), and the HTTP
// surface answers the unknown-action path with it (T4) — because a silence
// that reads as a version mismatch sends someone hunting for a newer Aeon
// that has `kill`. It does not exist, and the surface itself is the place to
// learn that.
//
// The Available and ABSENT lines are RENDERED from the table and
// ForbiddenOperationalControl, never hand-copied: a boundary that
// contradicts its own data is a second truth (D16), and the enumeration
// tests would pass while the sentence drifted.
//
// The "instead" clause names what a human can actually reach (plan.md F-1):
// the app (StepScreen/ChatView stop through `step.stop` on the socket) and
// the socket itself — `step.stop` over the 0600 path, which is precisely the
// surface a human holds and a sandboxed agent does not (P26). It does NOT
// name `aeon loop stop`: no such verb exists, and promising a phantom verb
// here would be S18's F-6 recidivism.
func BoundaryStatement() string {
	var reads, triggers []string
	for _, a := range Actions() {
		if a.Kind == KindTrigger {
			triggers = append(triggers, a.Name+" (trigger)")
		} else if a.Kind == KindWebhook {
			// Named as what it is: a delivery starts a loop like a trigger
			// and is not a read — the statement must not file a signed
			// POST under "Available: reads" (S19's honesty standard).
			triggers = append(triggers, a.Name+" (webhook)")
		} else if a.Kind == KindCallback {
			// S21 T6: also not a read — a signed inbound write that lands
			// a task result. Filing it under reads would be the same
			// dishonesty the webhook comment refuses, one verb sharper.
			triggers = append(triggers, a.Name+" (completion)")
		} else {
			reads = append(reads, a.Name)
		}
	}
	var b strings.Builder
	b.WriteString("aeon has no operational control actions, by design.\n\n")
	b.WriteString("  Available:  " + strings.Join(triggers, ", ") + "\n              " + strings.Join(reads, ", ") + "\n")
	b.WriteString("  ABSENT:     " + strings.Join(ForbiddenOperationalControl, ", ") +
		" — not restricted, absent.\n\n")
	b.WriteString("  Reason: handing an agent the means to start and stop loops is handing it a way\n")
	b.WriteString("  around every bound placed on one.\n\n")
	b.WriteString("  Instead: stop or answer a loop from the app, or over the daemon's socket\n")
	b.WriteString("  (`step.stop`, `loop.answer`) — the socket is the human surface: filesystem\n")
	b.WriteString("  permissions on a 0600 path, peer credentials (P26). This surface is not.\n")
	return b.String()
}

// ToolDescription renders one entry's description for the stdio tool list
// (and the HTTP route table): the projection it is, and its Cannot — N12's
// requirement is only legible if it reaches the reader, not just the test.
func (a Action) ToolDescription() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s of %s over the daemon socket. ", a.Name, string(a.Kind), a.SocketMethod)
	b.WriteString("It cannot: " + strings.Join(a.Cannot, "; "))
	b.WriteString(". " + BoundaryAbsentHint)
	return b.String()
}

// BoundaryAbsentHint is the one-line form every tool description carries, so
// a client that lists tools but never reads the long statement still learns
// the boundary from inside the list (N14's legibility half).
const BoundaryAbsentHint = "No operational control (stop/kill/pause/resume/retry/answer) exists " +
	"on this surface, by design — control loops from the app or the socket (P26)."
