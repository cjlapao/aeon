package main

// loop_messages.go — the thin client of `aeon loop messages` (S70 T5), the
// human's terminal window onto the agents' own exchange (MB7: the ONE door
// is a read). What is owned here is printing: the rows the store settled,
// the state they are in, and the SAID absence (P31) as its own sentence —
// a thread that does not exist is not an empty inbox, and the CLI is the
// surface where conflating the two costs an operator the most. The door,
// the rows, and the settlement rules are pinned by the daemon suite
// (agent_mailbox_test.go, the protocol handler tests, the conformance
// fixtures); the human path with a live daemon rides the DEMO §S70 walk.

import (
	"flag"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdLoopMessages(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop messages", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	thread := fs.Int64("thread", -1, "restrict to one thread — a thread IS its first ask's id (MB3); omit the flag (the default, -1) to read the whole loop")
	jsonOut := fs.Bool("json", false, "print the daemon's answer verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) == 0 {
		return fmt.Errorf("`loop messages` needs a loop id: aeon loop messages ID [--thread N] (S70: the agents' exchange — a read, never a write; the person's door to a loop is `loop message`)")
	}
	// -1 is the flag's OWN spelling of unset (the default); a SPOKEN
	// number must be a positive row id. Two mistakes the walk and the
	// post-merge smoke measured, both refused by name here: a spoken 0
	// silently reading as "unset" printed "no thread 0" beside a loop
	// that merely had no rows (silence wearing an absence's clothes,
	// P31's forbidden move) — and when 0 was FIRST refused outright,
	// the no-flag default died on the same refusal (the smoke ran into
	// it: the daemon never even got dialled). The daemon refuses spoken
	// non-positives too (ErrBadParams names MB3); refusing here saves
	// the round trip and names both the wrong value and the right way.
	if *thread < -1 || *thread == 0 {
		return fmt.Errorf("--thread must be a positive row id (a thread is its first ask's id); got %d — omit the flag to read the whole loop", *thread)
	}

	params := protocol.AgentMessagesParams{LoopID: positional[0]}
	if *thread > 0 {
		v := *thread
		params.Thread = &v
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.AgentMessagesResponse
	if err := c.Call(protocol.MethodAgentMessages, params, &res); err != nil {
		return err
	}

	if *jsonOut {
		return printJSON(w, res)
	}

	// An ask is answered when a reply settled through its link — the same
	// fold the UI does, from the same fields the store carries.
	answered := map[int64]bool{}
	for _, m := range res.Messages {
		if m.ResultOf > 0 {
			answered[m.ResultOf] = true
		}
	}

	if len(res.Messages) == 0 {
		// P31, in the surface with no pixels to hide behind: the absence
		// is SAID, in the daemon's own terms.
		if res.Absent {
			// %d on the *int64 would print a POINTER ADDRESS (measured at
			// the walk: "thread 34426036716312" — a machine fact wearing a
			// row id's clothes). Deref, and an absent read always knows
			// which thread it was asked about.
			fmt.Fprintf(w, "the loop holds no thread %d — the read said so, it did not come back empty\n", *res.Thread)
			return nil
		}
		fmt.Fprintf(w, "loop %s holds no exchange rows: its agents have not spoken to each other — or have not yet\n", res.LoopID)
		return nil
	}

	fmt.Fprintf(w, "exchange for %s — %d row(s); the rows are the state (S70 MB6)\n", res.LoopID, len(res.Messages))
	for _, m := range res.Messages {
		state := "posted"
		if answered[m.ID] {
			state = "answered"
		} else if m.DeliveredAt != "" {
			state = "delivered"
		}
		trailer := ""
		if state == "posted" && m.ExpectReply {
			trailer = " · awaiting reply"
		}
		fmt.Fprintf(w, "#%-4d %s → %s · %s   [%s%s, thread %d]\n",
			m.ID, m.FromAgent, m.ToAgent, m.Body, state, trailer, m.Thread)
	}
	return nil
}
