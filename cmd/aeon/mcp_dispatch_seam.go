package main

import (
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
)

// mcpDialBudget is MD4's build-time budget: per server, at build, in parallel
// across servers. It bounds how long a slow or dead server may hold before its
// offered tools degrade to withheld and the step starts anyway (criterion 4).
// 3 s is a number to CONFIRM against reality (the dial tests measure the
// observed latency against the fake), not folklore copied in. mcpCallDeadline
// is NOT added here — it lands beside its first use in T4, so a diff reviewer
// sees each budget next to the code that enforces it.
const mcpDialBudget = 3 * time.Second

// mcpCallDeadline is MD4's dispatch-time budget (T4's, landed here beside its
// first use — makeCall). It bounds one CallTool: a server that never answers
// cannot hold a step past this, and a failure becomes an error-text result
// (criterion 4's dispatch half), never a hang or a thrown error. A var (not a
// const) so a test can shrink it to prove the bound without waiting 60s,
// restored via t.Cleanup — the same seam shape as dialServer.
var mcpCallDeadline = 60 * time.Second

// S36-mcp-dispatch T2 — the seam's implementation, in cmd/aeon.
//
// This is the ONLY file in the tree that imports internal/mcp for the step
// path — MC11's one sacred line (brief §0): internal/daemon must not, and
// this package is the sanctioned importer. A function value (Options.StepMCP,
// T1) carries no import; this file is where the word "mcp" lives.
//
// The task's boundary (recorded for T3): T2 owns the OFFER — which tools are
// offered and which are withheld, from GrantView. T3 takes the offered list
// and dials the servers for their LIVE schema (MD2: no synthesized specs),
// then assembles the *daemon.MCPBundle. Keeping T2 free of any dial is what
// makes it unit-testable with fixtures (the task's DoD: "no dialing
// anywhere in this task's code").

// loadMCPConfig (Q1's pinned path) lives in mcp_admin.go since S46 T3: the
// CLI's mcp.* verbs and the step's offer read the operator's configuration
// through the SAME code — one implementation, several consumers — and that
// implementation is now the tables (store rows → mcp.Assemble), not the
// JSON file. The sentinel discipline survives exactly: a configuration with
// no rows is ErrNoConfig (the CLI prints the absent lines, the offer treats
// it as an empty registry, MD8); a broken read is an error, never silently
// absent.

// partitionOffer is the offer half of the S36 seam (MD2/MD9). It CALLS
// GrantView — S12's double gate, the one the CLI renders — and partitions its
// rows by State, never re-implementing the verdict (MD2: the offer filter
// State==GrantAvailable IS ruling §3; a second intersection would be a second
// truth):
//
//	GrantAvailable → offered (T3 dials these for their live schema);
//	everything else → withheld, with grants.go's sentence copied VERBATIM
//	(MD9 — one sentence per refusal, across the CLI, the view and the
//	runtime; re-typing it here would be two truths).
//
// Core tools (TierCore) are excluded entirely, in both lists: their verdict
// is S11's step gate's alone (the frozen set, per step), and a second
// evaluation here would be a second place for it to disagree.
//
// No dialing (T3 owns that), and no clock read — the caller supplies `at`
// (Q2: the same instant the step's own gate decides with; the CLI at
// mcp.go and the gate's commandVerdict both use time.Now at build), so a
// step cannot evaluate GrantView's rule expiry against a different clock
// than the gate that will re-decide the same tool at dispatch.
func partitionOffer(reg *mcp.Registry, agent string, ps []policy.Policy, at time.Time) (offered []mcp.GrantView, withheld []daemon.MCPWithheld) {
	for _, v := range reg.GrantView(agent, ps, at) {
		if v.Tier == mcp.TierCore {
			continue
		}
		if v.State == mcp.GrantAvailable {
			offered = append(offered, v)
			continue
		}
		withheld = append(withheld, daemon.MCPWithheld{Qualified: v.Qualified, Sentence: v.Reason})
	}
	return
}
