package mcp

// probe.go — S34 T4's one genuinely new behavior: an operator-triggered,
// dial-and-release check of one configured server. `initialize` + `tools/list`
// only; it builds no `tools/call` request anywhere in its body
// (TestProbeNeverCallsATool asserts this against a fake transport, not in
// prose).
//
// # Reality finding — no per-server timeout field exists to read
//
// design.md §5 said "5s default, from the config's own timeout field when
// present." Verified: ServerConfig/TransportConfig (config.go) carry
// Type/Command/Args/Env/URL — no timeout field anywhere in the schema.
// Adding one would be an internal/mcp config-schema change, out of this
// bench slice's scope (brief §7: this slice exposes, it adds no domain
// field). One fixed constant instead.
//
// # Reality finding — RemoteTool carries no "class"
//
// design.md §2.4's example shows `{"name": "list_issues", "class": "read"}`.
// RemoteTool (client.go) is name/description/inputSchema/annotations — a
// server's self-report, and its own doc comment says annotations are a hint
// that "never classifies anything" (Q4: only OUR configuration classifies a
// tool, and Probe has no config in scope — it dials the live server, it does
// not consult the registry). ProbeTool carries Name and Description, the
// real fields, never an invented class.
//
// # Reality finding — Probe does not refactor cmdMCPList's loop in place
//
// Q1 (plan.md) asked whether the CLI's dial path refactors cleanly into
// Probe. It does not: cmdMCPList's loop calls reg.ApplyListings and
// reg.AttachClient and keeps the connection OPEN (deferred Close, for the
// registry to use afterward) — ToolsFor/GrantView read through that live
// registry. mcp.check's probe is the opposite shape on purpose (brief §3/§5:
// "one probe, one answer," dial-and-release, no lingering session from a UI
// click). Forcing one function to serve both would mean Probe either leaks a
// connection per Check click or cmdMCPList loses the registry it needs.
// Plan.md's own escape hatch applies: "the probe ships as a thin wrapper
// calling the same internal functions" — Probe and cmdMCPList's loop both
// call Dial/ListTools (the one real dial path), independently orchestrated.
// cmdMCPList is UNCHANGED by this task; its existing tests needed no edits
// because nothing in its body moved.

import (
	"context"
	"time"
)

// DefaultProbeTimeout bounds one mcp.check — see the reality finding above.
const DefaultProbeTimeout = 5 * time.Second

// ProbeTool is one tool as the live server reported it — the real
// RemoteTool fields a human can read, never an invented classification.
type ProbeTool struct {
	Name        string
	Description string
}

// ProbeResult is one Probe's answer — always one of these two honest
// shapes: OK with its tools, or not OK with Err naming why (a dial failure,
// a ListTools failure, or ctx's own DeadlineExceeded).
type ProbeResult struct {
	OK    bool
	Tools []ProbeTool
	Err   error
	Ms    int64
}

// Probe dials srv, completes initialize (inside Dial) and tools/list, and
// releases the connection before returning — an ephemeral check, never a
// kept-open session. It builds no tools/call request anywhere in its body.
func Probe(ctx context.Context, srv ServerConfig, getenv func(string) string) ProbeResult {
	start := time.Now()
	c, err := Dial(ctx, srv, getenv)
	if err != nil {
		return ProbeResult{OK: false, Err: err, Ms: time.Since(start).Milliseconds()}
	}
	defer func() { _ = c.Close() }()

	remote, err := c.ListTools(ctx)
	if err != nil {
		return ProbeResult{OK: false, Err: err, Ms: time.Since(start).Milliseconds()}
	}
	tools := make([]ProbeTool, 0, len(remote))
	for _, r := range remote {
		tools = append(tools, ProbeTool{Name: r.Name, Description: r.Description})
	}
	return ProbeResult{OK: true, Tools: tools, Ms: time.Since(start).Milliseconds()}
}
