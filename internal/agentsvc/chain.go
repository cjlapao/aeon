package agentsvc

import (
	"fmt"
	"strings"
)

// chain.go reads the recorded asks (agent_chain, written by invoke.go BEFORE
// the task runs) and answers the two questions the graph asks — is this a
// cycle, and how deep does this go — with two DIFFERENT refusals, because
// conflating them gives the wrong answer confidently: a cycle that trips a
// cost report sends somebody to raise a cap while they hold an infinite loop
// (the cap was not wrong; it was the only thing that noticed — the refusal
// here refuses to be the thing that misdiagnoses first).
//
// Both answers are computed from ROWS, so they survive the restart that
// deletes every in-memory answer (and so S7 can resume a Task it did not
// create: the graph is where it was left).

// ChainDepthBound is Q1's answer, and the reasoning is the comment that
// belongs to a number: the shape this system actually builds is a planner
// asking a specialist — a path of ONE ask — or a planner asking a specialist
// who asks one more — a path of TWO. Four lets that shape breathe twice
// before refusing, and stays small enough to be what the spec calls it: a
// backstop, not a cycle detector that takes longer to fire. The cycle check
// runs FIRST and on its own terms; a bound that catches cycles by attrition
// is this task's failure mode wearing patience.
const ChainDepthBound = 4

// chainCheck is the invocation-time gate: it reads the graph as recorded and
// answers before the edge is written and long before any Task exists — a
// refusal after creation leaves a Task nobody will complete.
//
// A new edge caller → callee closes a cycle exactly when the callee can
// already reach the caller; the walk therefore starts at the callee and
// follows recorded asks. A refusal names the path in order, agents joined —
// `planner → reddit-agent → planner` is a sentence somebody can act on,
// where "cycle detected" is a shrug with a field name.
func (iv *Invoker) chainCheck(caller, callee string) *Reject {
	if caller == callee {
		return &Reject{Field: "cycle", Detail: fmt.Sprintf(
			"chain refused: %s cannot ask its own skill — %s → %s is a cycle of one, a structural mistake in the graph and not a cost failure: fix the graph, not a cap",
			caller, caller, callee)}
	}
	path, cyc, err := iv.reachablePath(callee, caller, []string{caller, callee})
	if err != nil {
		return &Reject{Field: "chain", Detail: fmt.Sprintf(
			`chain refused: the recorded graph could not be read (%v) — certifying "no cycle" from rows nobody could count is the confident wrong answer this file exists to refuse`, err)}
	}
	if cyc {
		return &Reject{Field: "cycle", Detail: fmt.Sprintf(
			"chain refused: asking %s from %s closes a cycle — %s — and a cycle is a structural mistake in the graph, not a cost failure: fix the graph, not a cap",
			callee, caller, strings.Join(path, " → ")),
		}
	}
	// The bound measures the WHOLE path the new edge would sit on: what is
	// already above the caller, the new ask itself, and what is already below
	// the callee. Measuring only one direction lets an infinite chain grow in
	// the unmeasured one (measured — the first version did exactly that).
	up, err := iv.longestTo(caller)
	if err != nil {
		return &Reject{Field: "chain", Detail: fmt.Sprintf(
			"chain refused: the recorded graph could not be read (%v) — a bound measured from missing rows is decoration", err)}
	}
	down, err := iv.longestFrom(callee)
	if err != nil {
		return &Reject{Field: "chain", Detail: fmt.Sprintf(
			"chain refused: the recorded graph could not be read (%v) — a bound measured from missing rows is decoration", err)}
	}
	depth := up + 1 + down
	if depth > ChainDepthBound {
		return &Reject{Field: "depth", Detail: fmt.Sprintf(
			"chain refused: asking %s from %s would reach depth %d and the bound is %d — no agent repeats on this path, so this is a runaway and NOT a cycle: fix the graph (nobody will audit a chain this deep); this bound is a backstop, not a cap to raise",
			callee, caller, depth, ChainDepthBound),
		}
	}
	return nil
}

// reachablePath walks the recorded asks outward from `from`, looking for
// `target`, and returns the agent path when found. seen() keeps the walk
// finite over already-recorded cycles: the graph on disk may already contain
// one (two loops that each asked the other before either met this gate —
// history predates it), and this function's job is to REPORT such a graph,
// never to spin on it.
func (iv *Invoker) reachablePath(from, target string, head []string) ([]string, bool, error) {
	type hop struct {
		agent string
		path  []string
	}
	seen := map[string]bool{from: true}
	queue := []hop{{agent: from, path: head}}
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		edges, err := iv.s.ChainEdgesByCallerAgent(h.agent)
		if err != nil {
			return nil, false, err
		}
		for _, e := range edges {
			next := append(append([]string(nil), h.path...), e.CalleeAgent)
			if e.CalleeAgent == target {
				return next, true, nil
			}
			if !seen[e.CalleeAgent] {
				seen[e.CalleeAgent] = true
				queue = append(queue, hop{agent: e.CalleeAgent, path: next})
			}
		}
	}
	return nil, false, nil
}

// longestTo is the longest recorded path ENDING at agent — asks counted,
// walking backwards through who-was-asked. Missing evidence fails closed:
// the error travels to chainCheck, which refuses rather than trusting a
// depth of zero that nobody could read off the rows.
func (iv *Invoker) longestTo(agent string) (int, error) {
	return iv.longestToFrom(agent, map[string]bool{})
}

func (iv *Invoker) longestToFrom(agent string, visiting map[string]bool) (int, error) {
	if visiting[agent] {
		return 0, nil
	}
	visiting[agent] = true
	best := 0
	edges, err := iv.s.ChainEdgesToCallee(agent)
	if err != nil {
		return 0, err
	}
	for _, e := range edges {
		d, err := iv.longestToFrom(e.CallerAgent, visiting)
		if err != nil {
			return 0, err
		}
		if 1+d > best {
			best = 1 + d
		}
	}
	delete(visiting, agent)
	return best, nil
}

// longestFrom is the longest recorded path STARTING at agent — the ask tree
// below. Same fail-closed rule.
func (iv *Invoker) longestFrom(agent string) (int, error) {
	return iv.longestFromFrom(agent, map[string]bool{})
}

func (iv *Invoker) longestFromFrom(agent string, visiting map[string]bool) (int, error) {
	if visiting[agent] {
		return 0, nil
	}
	visiting[agent] = true
	best := 0
	edges, err := iv.s.ChainEdgesByCallerAgent(agent)
	if err != nil {
		return 0, err
	}
	for _, e := range edges {
		d, err := iv.longestFromFrom(e.CalleeAgent, visiting)
		if err != nil {
			return 0, err
		}
		if 1+d > best {
			best = 1 + d
		}
	}
	delete(visiting, agent)
	return best, nil
}
