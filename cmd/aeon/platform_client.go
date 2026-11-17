package main

// platform_client.go — the shared client arm for the derived-store reads
// (S54 T5, SP7).
//
// platformDial is the dial every platform verb shares; a var so the
// in-process parity tests can swap in a handshake client over an in-memory
// pipe and prove the verb speaks through the REAL handlers (the mergequeue
// family's mqDial shape, S60 T10 — same package, same reason). The no-
// daemon case is the plain dial error, stated, never emulated: there is no
// offline arm for a composed panel or a live-priced plan.

import (
	"flag"

	"github.com/cjlapao/aeon/internal/protocol"
)

var platformDial = dial

// jsonFlag registers the --json flag on a platform verb's flag set: the
// verb then prints the daemon's answer verbatim and nothing else — the
// parity test pins those bytes against the raw socket.
func jsonFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("json", false, "print the daemon's answer verbatim")
}

// platformYesRefusal is the fixed shape of every S54 seatbelt refusal: the
// gate names what the verb would have done and what confirms it. The
// statement the caller prints before reaching this line is the verb's own
// (the plan's figures, or the graph family's derived-row statement) — this
// sentence only closes the gate, so no verb can forget the clause.
func platformYesRefusal(what string) error {
	return &usageError{msg: what + " is a gated operation (S54 SP7) — the statement above is what it " +
		"would do and what it would cost, and NOTHING was written: re-run with --yes to confirm"}
}

// platformPlanCall asks the daemon for the plan a reindex of one named
// corpus would preview. It is the seatbelt's price tag: index rebuild
// prints this BEFORE it moves, and a refusal here stops the rebuild —
// an unpriced money pass never starts.
func platformPlanCall(c *protocol.Client, componentRoot, corpus string) (*protocol.RetrievalPlanAnswer, error) {
	var plan *protocol.RetrievalPlanAnswer
	if err := c.Call(protocol.MethodRetrievalReindexPlan,
		protocol.RetrievalPlanParams{ComponentRoot: componentRoot, Corpus: corpus}, &plan); err != nil {
		return nil, err
	}
	return plan, nil
}
