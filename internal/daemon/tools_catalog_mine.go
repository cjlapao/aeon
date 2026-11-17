// tools_catalog_mine.go — S47 T7 (TC9, MC2, OL3, 2026-11-02): the agent's OWN
// tool view, `tools_mine` and `tool_show`.
//
// These two computations sit beside `ToolsByAgent` on purpose. The person's
// `tools.byAgent` answers "what does this agent hold, and what does it not" over
// the WHOLE catalog; the agent's own view answers the first half of that and not
// the second, through the SAME `toolcatalog.ForAgent`, the SAME grant chain and
// the SAME policy set. Two implementations of "what does this agent hold" is how
// an agent is told one thing by the tool it calls and another by the panel that
// judges it — and T4's differential test exists precisely because the two must
// agree. So the only difference between the doors is what gets DROPPED on the way
// out, and every drop here is a rule with a test:
//
//   - `not_granted` rows go (rule 2). A tool the agent was not given is absent,
//     not announced: the list is its own inventory, and a row saying "you may not"
//     is a survey of what it was refused — which is recon, and the answer a
//     withheld grant should not be able to give back.
//   - The name universe is the code's names plus THIS agent's declared MCP names
//     (`w.view.ToolsFor(self)`), exactly as `ToolsByAgent` builds it for a subject
//     the person named. A peer's private server is never in the input, so MC2 is
//     structural rather than a filter: nothing has to be excluded from an answer
//     that was never computed from it.
//   - Policy rows are those that could change whether the CALLER may call —
//     `all-agents`, `lane-type`, or `agent` equal to self. Another agent's `agent`
//     row is dropped, because it reports that agent's restrictions and says
//     nothing about this one (rule 5).
//   - The nearest-name hints come from the caller's own held set only, and an
//     unheld name and a nonexistent one run through the SAME statement (rule 3),
//     so the door cannot tell "you were not given that" from "nobody ever had it".
//
// Nothing here writes, applies, repairs or caches (TC1, TC11, N8): `world(ctx)`
// reads its four authorities per call and discards them with the answer.
package daemon

import (
	"context"
	"strings"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// --- the agent's own inventory -----------------------------------------------------

// heldNames is the name universe for an OWN view: the tools the code offers plus
// the qualified tools THIS agent declares through MCP. It is deliberately the same
// construction `ToolsByAgent` uses, and it is the reason a peer's private server
// cannot leak: the MCP half comes from `MCPView.ToolsFor(self)`, the scoped entry
// keyed by the caller's own stamp, never from the server list.
func (w *catalogWorld) heldNames(agent string) []string {
	// The universe is a SET. The two halves overlap by nature — a tool the agent
	// was granted is both offered by the code and named by the agent's own row —
	// and `toolcatalog.ForAgent` answers one Entry per name it is handed, keeping
	// duplicates because they are the caller's count (its own docblock says so).
	// Handing it the raw union therefore printed every granted tool twice: 146
	// rows for 73 tools, measured against a real daemon by the S47 T11 walk.
	seen := make(map[string]bool, len(w.code)+8)
	names := make([]string, 0, len(w.code)+8)
	for _, rec := range w.code {
		if seen[rec.Key] {
			continue
		}
		seen[rec.Key] = true
		names = append(names, rec.Key)
	}
	if w.view != nil {
		for _, n := range w.view.ToolsFor(agent) {
			if seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
	}
	return names
}

// entryByName indexes the catalog by name for the own-view doors, so a held row
// can carry the catalog's own words about its tool instead of a summary invented
// here. The hidden `write_file` alias is indexed too, so an alias spelled by the
// grant chain resolves to the one entry rather than vanishing.
func (w *catalogWorld) entryByName() map[string]protocol.ToolEntry {
	all := w.entries()
	out := make(map[string]protocol.ToolEntry, len(all))
	for _, e := range all {
		out[e.Name] = e
		for _, a := range e.Aliases {
			if _, taken := out[a]; !taken {
				out[a] = e
			}
		}
	}
	return out
}

// rowsThatCouldReach is rule 5, and it is one line of consequence: `PoliciesTouching`
// already restricted to `tool_access` rows whose target kind can govern an agent
// (`all-agents`, `agent`, `lane-type`), and the one kind that names a SPECIFIC
// somebody else is `agent` — so that is the only thing dropped. An `all-agents` or
// `lane-type` row is a class rule that speaks about the caller too; hiding it would
// tell an agent nothing refuses it while a rule it cannot see outranks the allow it
// is reading about.
func rowsThatCouldReach(rows []policy.Policy, self string) []policy.Policy {
	out := make([]policy.Policy, 0, len(rows))
	for _, p := range rows {
		if p.Target.Kind == policy.AgentKind && p.Target.ID != self {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ToolsMine answers the `tools_mine` tool and its `tools.mine.list` twin.
//
// `agent` is never a param: it is the identity stamped on the step, resolved by
// the records door (OL3) and passed here by the arm. An agent that does not exist
// is the typed refusal naming the known agents — the same answer `tools.byAgent`
// gives a person who mistypes a slug, because "you hold nothing" and "that agent
// does not exist" are different facts and collapsing them is how a person debugs a
// grant that was never missing.
//
// An EMPTY list is a real answer here and carries no statement, unlike
// `tools.list`: there is no filter to be the reason, and an agent whose row names
// nothing and who declares nothing genuinely holds nothing. The refusal above is
// what distinguishes that from a nonsense agent.
func (sc *toolsCatalogService) ToolsMine(ctx context.Context, agent string) (protocol.ToolsMineResult, error) {
	w, err := sc.world(ctx)
	if err != nil {
		return protocol.ToolsMineResult{}, err
	}
	agent = strings.TrimSpace(agent)
	row, found := w.lookupAgent(agent)
	if !found {
		return protocol.ToolsMineResult{}, protocol.ErrToolAgentUnknown.WithWhat(
			"the own-view read named %q, which is not an agent this store carries; the known agents are: %s",
			agent, quotedNames(w.agentSlugs()))
	}

	entries := toolcatalog.ForAgent(row.Slug, w.heldNames(row.Slug), w.inputs)
	byName := w.entryByName()
	seenPolicy := map[string]bool{}
	res := protocol.ToolsMineResult{Dispatchable: true}
	for _, e := range toolcatalog.SortedForRendering(entries) {
		if e.State == toolcatalog.StateNotGranted {
			continue // absent, never announced (rule 2)
		}
		tool, known := byName[e.Tool]
		if !known {
			// The grant chain says held and the catalog has no entry: an MCP
			// declaration on a server that is no longer enabled. The row stays —
			// dropping it would make the answer disagree with the gate that would
			// let the call through — but it is answered `available:false` with the
			// server named, which is the truth, rather than a description invented
			// for a tool no authority describes.
			server, _, _ := strings.Cut(e.Tool, ".")
			tool = protocol.ToolEntry{
				Name:         e.Tool,
				Family:       server,
				Source:       protocol.ToolsSourceMCP,
				Capabilities: []string{},
				Governed:     []string{},
				Server:       server,
			}
		}
		res.Tools = append(res.Tools, protocol.ToolsMineEntry{
			Tool:    tool,
			State:   e.State,
			Reason:  e.Reason,
			Granted: e.GrantedBy,
		})
		for _, p := range rowsThatCouldReach(toolcatalog.PoliciesTouching(e.Tool, w.policies), row.Slug) {
			seenPolicy[p.ID] = true
		}
	}
	if res.Tools == nil {
		res.Tools = []protocol.ToolsMineEntry{}
	}
	res.Held = len(res.Tools)
	res.PolicySeen = len(seenPolicy)
	if err := checkView(w); err != nil {
		return protocol.ToolsMineResult{}, err
	}
	return res, nil
}

// --- `tool_show` ------------------------------------------------------------------

// ToolShow answers the `tool_show` tool and its `tools.mine.show` twin: one tool
// the caller holds, in full, from the caller's own point of view.
//
// The refusal is the load-bearing part. A name outside the agent's held set and a
// name nobody ever had run through the SAME two statements below, so the bytes are
// identical and the door is not an existence oracle for a peer's private MCP tool
// (MC2) or for a grant an operator withheld. The hints come from the caller's own
// held names, never from the catalog — a hint list built from everything would
// answer the question the refusal refuses to answer.
func (sc *toolsCatalogService) ToolShow(ctx context.Context, agent, name string) (protocol.ToolShowResult, error) {
	w, err := sc.world(ctx)
	if err != nil {
		return protocol.ToolShowResult{}, err
	}
	agent = strings.TrimSpace(agent)
	row, found := w.lookupAgent(agent)
	if !found {
		return protocol.ToolShowResult{}, protocol.ErrToolAgentUnknown.WithWhat(
			"the own-view read named %q, which is not an agent this store carries; the known agents are: %s",
			agent, quotedNames(w.agentSlugs()))
	}

	entries := toolcatalog.SortedForRendering(
		toolcatalog.ForAgent(row.Slug, w.heldNames(row.Slug), w.inputs))
	held := make([]string, 0, len(entries))
	var mine *toolcatalog.Entry
	for i := range entries {
		if entries[i].State == toolcatalog.StateNotGranted {
			continue
		}
		held = append(held, entries[i].Tool)
		if entries[i].Tool == name || toolEntryMatches(byNameEntry(w, entries[i].Tool), name) {
			cp := entries[i]
			mine = &cp
		}
	}
	if mine == nil {
		return protocol.ToolShowResult{}, protocol.ErrToolUnknown.WithWhat(
			"the own-view read named %q, which is not a tool in your set; the nearest names are: %s",
			name, nearestToolNames(name, held))
	}

	tool, known := w.entryByName()[mine.Tool]
	if !known {
		server, _, _ := strings.Cut(mine.Tool, ".")
		tool = protocol.ToolEntry{
			Name:         mine.Tool,
			Family:       server,
			Source:       protocol.ToolsSourceMCP,
			Capabilities: []string{},
			Governed:     []string{},
			Server:       server,
		}
	}
	res := protocol.ToolShowResult{
		Tool:     tool,
		State:    mine.State,
		Reason:   mine.Reason,
		Granted:  mine.GrantedBy,
		Policies: w.policyRefsForRow(mine.Tool, row.Slug),
	}
	if toolcatalog.IsMCPToolName(tool.Name) {
		res.MCP = &protocol.ToolMCPLink{
			Server: tool.Server,
			Tier:   tool.Tier,
			Link:   protocol.Link{Kind: "mcp_server", ID: tool.Server},
		}
	}
	if err := checkView(w); err != nil {
		return protocol.ToolShowResult{}, err
	}
	return res, nil
}

// policyRefsForRow is `policyRefs` with rule 5 applied BEFORE rendering, not after:
// once a row is a `PolicyRef` its target is a rendered string, and re-parsing a
// sentence to decide who may read it is exactly the kind of second authority over
// what a rule means that this slice keeps refusing to build.
func (w *catalogWorld) policyRefsForRow(tool, self string) []protocol.PolicyRef {
	rows := rowsThatCouldReach(toolcatalog.PoliciesTouching(tool, w.policies), self)
	if len(rows) == 0 {
		return []protocol.PolicyRef{}
	}
	// policyRefs is the one renderer (S45's single describer, verbatim sentences);
	// it reads the whole set, so the restriction is applied by name rather than by
	// giving it a second parameter it would have to agree with.
	all := w.policyRefs(tool)
	want := make(map[string]bool, len(rows))
	for _, p := range rows {
		want[p.ID] = true
	}
	out := make([]protocol.PolicyRef, 0, len(rows))
	for _, ref := range all {
		if want[ref.ID] {
			out = append(out, ref)
		}
	}
	return out
}

// byNameEntry resolves the name a grant chain produced to the catalog entry it
// names, for the alias-aware match `tools.get` uses. An MCP declaration has no
// alias and no row, so an empty entry simply fails to match by name — which is the
// honest outcome, since its qualified spelling IS its identity.
func byNameEntry(w *catalogWorld, name string) protocol.ToolEntry {
	if e, ok := w.entryByName()[name]; ok {
		return e
	}
	return protocol.ToolEntry{Name: name}
}
