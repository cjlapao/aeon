package mcp

// degrade.go — MC9/N8/N10: a third-party server that hangs must not hang a
// loop, and a timeout must never read as "the tool found nothing".
//
// **Q2, answered: the default per-call timeout is 30 seconds.** Chosen and
// visible, the way S10's T7 picked its age cap:
//
//   - healthy MCP calls are network round-trips behind a small process —
//     docs search, ticket fetch — sub-second to a few seconds. Thirty
//     seconds is an order of magnitude past "healthy" and still short
//     enough that a step's human sees progress.
//   - the step loop is dominated by model latency; a server occupying a
//     step for bash's 600s (W13's bound, `limits.go`) would read as a dead
//     loop. The two bounds differ **because the failure modes differ**:
//     a local build genuinely runs for minutes; a third-party `tools/call`
//     that does not answer in half a minute is not about to.
//   - `30s` is the number the S12 DEMO prints; the DEMO saying the real
//     number is part of what keeps this from drifting into a magic constant.
//
// The bound is **caller-shortenable, never caller-lengthenable**, in bash's
// shape verbatim: `TimeoutSeconds… cannot be lengthened past BashTimeout:
// a tool that could raise its own ceiling has no ceiling` — same rule, same
// reason, same direction of failure.

import (
	"context"
	"errors"
	"sort"
	"time"
)

const DefaultCallTimeout = 30 * time.Second

// callTimeout is the seam the tests lower so "bounded" is testable without
// thirty-second waits. Nothing but DefaultCallTimeout reads it upward.
var callTimeout = DefaultCallTimeout

// timeoutSentence is THE rendering of the third outcome — distinct from an
// empty result and from an error, and the content itself says why they must
// not be conflated: a model that reads a timeout as "no results" reports
// an absence it never observed.
func timeoutSentence(server, qualified string, bound time.Duration) string {
	return "timeout: MCP server " + strconvQuote(server) + " did not answer tools/call for " +
		qualified + " within " + bound.String() +
		" — the server not answering is a different fact from the tool finding nothing (N8/N10); " +
		"no retry was made, and another attempt is the model's call to make, visibly"
}

func strconvQuote(s string) string {
	// Small indirection so the sentence has one formatting definition even
	// though json/strconv both could quote it — whichever the transcript
	// standard is, it is decided HERE once.
	return "\"" + s + "\""
}

// boundCall applies the ceiling: no deadline gets in (the caller's own
// shorter bound always wins; this package cannot be talked into an
// unbounded call by a careless context.Background()).
func boundCall(ctx context.Context) (context.Context, context.CancelFunc, time.Duration) {
	if d, ok := ctx.Deadline(); ok && time.Until(d) <= callTimeout {
		return ctx, func() {}, time.Until(d)
	}
	bound := callTimeout
	if d, ok := ctx.Deadline(); ok && time.Until(d) < bound {
		bound = time.Until(d)
	}
	ctx2, cancel := context.WithTimeout(ctx, bound)
	return ctx2, cancel, bound
}

// isTimeout recognises the bounded wait giving up — from either transport
// (both surface the ctx's DeadlineExceeded; neither retries, so one is
// really one attempt).
func isTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

// DownServers names the configured servers with no attached live client —
// the fact `aeon mcp list` (T9) renders as REPORTED ABSENCE beside each
// tool the server would have carried. A tool silently missing is a model
// wondering why it cannot do what its prompt says it can; the degradation
// is per-server, so one failing name never removes another's tools.
//
// This package does not dial, restart, or declare a client dead — the
// daemon owns lifecycle. What lives here is the honest *answer*, not a
// retry loop pretending the question was never asked.
func (r *Registry) DownServers() []string {
	need := map[string]bool{}
	for _, s := range r.cfg.Public {
		need[s.Name] = true
	}
	for _, ac := range r.cfg.Agents {
		for _, s := range ac.Private {
			need[s.Name] = true
		}
	}
	var down []string
	for name := range need {
		if _, ok := r.client(name); !ok {
			down = append(down, name)
		}
	}
	sort.Strings(down)
	return down
}
