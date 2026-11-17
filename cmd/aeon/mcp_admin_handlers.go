package main

// ════════════════════════════════════════════════════════════════════════════
// mcp_admin_handlers.go — S46 T3: the five PERSON'S management reads.
//
// mcp.servers · mcp.server · mcp.grants · mcp.vocabulary · mcp.changes —
// answers on Table.MCPAdmin, dispatched by method name (MG2's dispatch
// discipline: the handler NAME routes, params never carry identity). These
// are the human-door reads: unscoped by design (MG8 — the person holding
// the 0600 endpoint sees everything; the AGENT-scoped reads are T8's tools,
// a different code path through Registry.ToolsFor(self), and
// TestNoAgentToolCallsAManagementRead keeps the lanes apart).
//
// The rules these handlers obey:
//   - NO cache (MG12): every answer re-reads the tables through mcpAdmin;
//     two calls are two row reads. The seam holds handles and functions,
//     never values (TestMcpReadsHoldNoCache checks the shape).
//   - NO probes (MG5/MG11): the answers join the probe cache's ALREADY-KNOWN
//     rows (the same cache mcp.status/mcp.check use — one probe truth);
//     reading a list never spawns a process (TestMcpServersDoesNotProbe).
//     mcp.check remains the only door that dials.
//   - Env is NAMES only (C17/MG13): the transport view carries envNames —
//     the rows never held values and nothing here could invent them.
//   - The wire shapes are the PROTOCOL's types (internal/protocol/mcp_admin.go),
//     taken not restated — the conformance fixtures are the byte contract,
//     and a cmd-side lookalike struct is a second truth waiting to drift.
//   - The error convention (the protocol's forwarding contract): a thing
//     that does not exist answers the TYPED protocol.ErrMcpUnknown in err
//     (naming the nearest names — only this seam can see the rows); a
//     refusal is a plain `refused:` sentence (ships verbatim under
//     ErrMcpRefused); a plain error is infrastructure.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// --- the vocabulary seam ----------------------------------------------------------

// mcpVocabularySource is the seam the vocabulary read takes its values
// through. Production READS THE VOCABULARIES FROM THEIR OWNERS (transports
// from internal/mcp, rerun classes from internal/tools, effect classes from
// internal/mcp, grammars from the validator's own exported strings) — this
// file restates none of them. The test swaps the seam to prove the handler
// reads through it (TestMcpVocabularyComesFromTheValidator).
var mcpVocabularySource = func() protocol.MCPVocabularyResult {
	return protocol.MCPVocabularyResult{
		Transports: []string{string(mcp.TransportStdio), string(mcp.TransportHTTP)},
		Rerun:      []string{string(tools.RerunSafe), string(tools.RerunDetectable), string(tools.RerunUnsafe)},
		Effect:     []string{string(mcp.EffectClassLocal), string(mcp.EffectClassOutward), string(mcp.EffectClassIrreversible)},
		Grammars: protocol.MCPGrammar{
			Server:  mcp.ServerNameGrammar(),
			Tool:    mcp.ToolNameGrammar(),
			EnvName: mcp.EnvNameGrammar(),
		},
	}
}

// --- the seam ---------------------------------------------------------------------

// mcpAdminHandleFor answers the five reads, dispatched by method NAME
// (the protocol binds the name at registration; params never carry
// routing). The probe cache is the SAME one wireMCPServices gave
// mcp.status/mcp.check, so the status join reads the very answers the probe
// door produced and never a second truth — and spawns nothing (MG5).
func mcpAdminHandleFor(st *store.Store, cache *mcpProbeCache) func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	admin := newMCPAdmin(st)
	return func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
		switch method {
		case "mcp.servers":
			return mcpServersRead(ctx, admin, cache, params)
		case "mcp.server":
			return mcpServerRead(ctx, admin, cache, params)
		case "mcp.grants":
			return mcpGrantsRead(ctx, admin, params)
		case "mcp.vocabulary":
			return mcpVocabularyRead(params)
		case "mcp.changes":
			return mcpChangesRead(ctx, admin, params)
		case "mcp.server.add":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.server.update":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.server.remove":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.server.setEnabled":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.server.share":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.server.inspect":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.processes":
			return mcpProcessesRead(params)
		case "mcp.process.logs":
			return mcpProcessLogsRead(params)
		case "mcp.process.start", "mcp.process.stop", "mcp.process.restart":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.grant.declare":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.grant.revoke":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.core.set":
			return mcpWrite(ctx, admin, method, params)
		case "mcp.importFile":
			return mcpImportFileRead(ctx, admin, params)
		// S46 T8: the five AGENT-SCOPED entries (mcp.mine.*). The daemon's
		// records arms enter them with self stamped through
		// protocol.WithMCPActor; they are not registered as socket methods
		// here — the mcp.mine.* twins ride the catalog (protocol/records.go)
		// through the records door, and a person never reaches this half
		// (mineSelf refuses even a stamped-person call). MG5: the status
		// entry reads this SAME cache and spawns nothing.
		case "mcp.mine.servers":
			return mineServersRead(ctx, admin, params)
		case "mcp.mine.grants":
			return mineGrantsRead(ctx, admin, params)
		case "mcp.mine.status":
			return mineStatusRead(ctx, admin, cache, params)
		case "mcp.mine.proposeRemove":
			return mineProposeRemove(ctx, admin, params)
		case "mcp.mine.proposeRevoke":
			return mineProposeRevoke(ctx, admin, params)
		default:
			return nil, fmt.Sprintf("refused: %q is not a method this seam answers", method), nil
		}
	}
}

// --- mcp.servers -------------------------------------------------------------------

// normalizeParams treats nil/null as the empty object (every filter here
// is optional — the protocol's decodeParams guarantees an object or nil;
// the seam defends its own decode anyway).
func normalizeParams(params json.RawMessage) json.RawMessage {
	if len(params) == 0 || string(params) == "null" {
		return json.RawMessage("{}")
	}
	return params
}

func mcpServersRead(ctx context.Context, admin *mcpAdmin, cache *mcpProbeCache, params json.RawMessage) (any, string, error) {
	params = normalizeParams(params)
	var req protocol.MCPServersParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Sprintf("refused: mcp.servers needs an object of optional filters (%v)", err), nil
	}
	if req.Tier != nil && *req.Tier != "public" && *req.Tier != "private" {
		return nil, fmt.Sprintf("refused: unknown tier %q — mcp.vocabulary names the set", *req.Tier), nil
	}
	if req.Origin != nil && *req.Origin != "system" && *req.Origin != "user" && *req.Origin != "import" {
		return nil, fmt.Sprintf("refused: unknown origin %q — the provenance set is system, user and import", *req.Origin), nil
	}
	servers, _, err := admin.serverRows(ctx)
	if err != nil {
		return nil, "", err
	}
	all, err := admin.st.McpRows(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("read the MCP tables: %w", err)
	}
	declaredCount := declaredCounts(all.Declares, admin)
	probes := cache.snapshot()

	out := []protocol.MCPServerView{}
	for _, r := range servers {
		if req.Tier != nil && r.Tier != *req.Tier {
			continue
		}
		if req.Enabled != nil && r.Enabled != *req.Enabled {
			continue
		}
		if req.Origin != nil && r.Origin != *req.Origin {
			continue
		}
		v, err := serverView(admin, r, probes, declaredCount[r.Name])
		if err != nil {
			return nil, "", err
		}
		if req.Agent != nil && v.Agent != *req.Agent {
			continue
		}
		out = append(out, v)
	}
	return protocol.MCPServersResult{Servers: out}, "", nil
}

// declaredCounts counts, per server name, how many DISTINCT agents declare
// at least one of its tools (the COUNT is this read's; the NAMES are
// mcp.server's).
func declaredCounts(declares []store.McpDeclareRow, admin *mcpAdmin) map[string]int {
	count := map[string]int{}
	seen := map[string]bool{} // (agent,server) dedup: two tools declared = one declarer
	for _, d := range declares {
		i := strings.Index(d.Qualified, ".")
		if i <= 0 {
			continue
		}
		name := d.Qualified[:i]
		if k, ok := admin.mapKey(d.AgentSlug); ok {
			if seen[k+"\x00"+name] {
				continue
			}
			seen[k+"\x00"+name] = true
		}
		count[name]++
	}
	return count
}

// serverView renders one row as the protocol's ServerView: the row's facts
// (enabled, origin, updatedAt included) plus the probe join's honest three
// states — reachable, unreachable, and the DISTINCT never-asked word.
func serverView(admin *mcpAdmin, r store.McpServerRow, probes map[string]probeSummary, declaredBy int) (protocol.MCPServerView, error) {
	v := protocol.MCPServerView{
		Name: r.Name, Title: r.Title, Description: r.Description, Tier: r.Tier,
		Enabled: r.Enabled, Origin: r.Origin,
		DeclaredBy: declaredBy, UpdatedAt: r.UpdatedAt,
		Status: protocol.MCPServerStatusUnprobed,
	}
	v.Shared = r.Tier == "public" && admin.st.McpIsShared(context.Background(), r.Name)
	if r.AgentSlug != nil {
		if key, ok := admin.mapKey(*r.AgentSlug); ok {
			v.Agent = key
		}
	}
	if p, ok := probes[r.Name]; ok {
		if p.OK {
			v.Status = "reachable"
		} else {
			v.Status = "unreachable"
		}
	}
	t, err := transportView(r)
	if err != nil {
		return protocol.MCPServerView{}, err
	}
	v.Transport = t
	v.Tools = []protocol.MCPToolView{}
	for _, tl := range r.Tools {
		v.Tools = append(v.Tools, protocol.MCPToolView{
			Name: tl.Name, Rerun: tl.Rerun, Effect: tl.Effect,
			Classified: tl.Rerun != "" && tl.Effect != "",
		})
	}
	return v, nil
}

// transportView decodes the stored JSON arrays for the response — names
// only, never values (the rows never held values; MG13).
func transportView(r store.McpServerRow) (protocol.MCPTransportSummary, error) {
	args, err := mcpJSONArray(r.Name, "args", r.Args)
	if err != nil {
		return protocol.MCPTransportSummary{}, err
	}
	env, err := mcpJSONArray(r.Name, "env", r.Env)
	if err != nil {
		return protocol.MCPTransportSummary{}, err
	}
	envFrom := map[string]string{}
	_ = json.Unmarshal([]byte(r.EnvFrom), &envFrom)
	return protocol.MCPTransportSummary{
		Type: r.TransportType, Command: r.Command, Args: args, URL: r.URL, EnvNames: env, EnvFrom: nilIfEmpty(envFrom),
	}, nil
}

// --- mcp.server ---------------------------------------------------------------------

func mcpServerRead(ctx context.Context, admin *mcpAdmin, cache *mcpProbeCache, params json.RawMessage) (any, string, error) {
	params = normalizeParams(params)
	var req protocol.MCPServerParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Sprintf("refused: mcp.server needs an object naming `name` (%v)", err), nil
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, "refused: mcp.server needs a non-empty `name` field", nil
	}
	r, found, err := admin.st.McpServerGet(ctx, req.Name)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "", unknownServerError(admin, ctx, req.Name)
	}
	declaredBy, declarerList := declarersOf(admin, ctx, r.Name)
	v, err := serverView(admin, r, cache.snapshot(), declaredBy)
	if err != nil {
		return nil, "", err
	}
	changes, err := changesView(ctx, admin.st, r.Name, "", 20)
	if err != nil {
		return nil, "", err
	}
	return protocol.MCPServerResult{Server: v, DeclaredBy: declarerList, Changes: changes}, "", nil
}

// declarersOf answers WHO declares from a server (names, sorted) and how
// many (the count ServerView carries).
func declarersOf(admin *mcpAdmin, ctx context.Context, name string) (int, []string) {
	all, err := admin.st.McpRows(ctx)
	if err != nil {
		return 0, []string{}
	}
	prefix := name + "."
	seen := map[string]bool{}
	var out []string
	for _, d := range all.Declares {
		if !strings.HasPrefix(d.Qualified, prefix) {
			continue
		}
		if k, ok := admin.mapKey(d.AgentSlug); ok && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return len(out), out
}

// unknownServerError is the typed not-exists answer (protocol.ErrMcpUnknown,
// forwarded untouched): it names the NEAREST known names (bounded edit
// distance) and falls back to the full sorted roster — a bare not-found is
// not a refusal that helps (AGENTS.md §5).
func unknownServerError(admin *mcpAdmin, ctx context.Context, name string) error {
	servers, _, err := admin.serverRows(ctx)
	if err != nil {
		return err
	}
	var names []string
	for _, s := range servers {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return protocol.ErrMcpUnknown.WithWhat("unknown MCP server %q — no MCP servers are configured (the tables are empty)", name)
	}
	var near []string
	for _, n := range names {
		if editDistanceAtMostTwo(name, n) {
			near = append(near, n)
		}
	}
	if len(near) == 0 {
		return protocol.ErrMcpUnknown.WithWhat("unknown MCP server %q — known servers: %s", name, strings.Join(names, ", "))
	}
	return protocol.ErrMcpUnknown.WithWhat("unknown MCP server %q — did you mean: %s", name, strings.Join(near, ", "))
}

// --- mcp.grants -----------------------------------------------------------------------

func mcpGrantsRead(ctx context.Context, admin *mcpAdmin, params json.RawMessage) (any, string, error) {
	params = normalizeParams(params)
	var req protocol.MCPGrantsParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Sprintf("refused: mcp.grants needs an object naming `agent` (%v)", err), nil
	}
	if strings.TrimSpace(req.Agent) == "" {
		return nil, "refused: mcp.grants needs a non-empty `agent` field — it answers one agent's view, never the whole roster", nil
	}
	// The person's view of ANY existing agent (MG8: unscoped; unknown ⇒ the
	// typed not-exists answer naming the roster — the same convention
	// tools.grants keeps).
	agents, err := admin.st.AgentsAll()
	if err != nil {
		return nil, "", err
	}
	known := false
	names := make([]string, 0, len(agents))
	// The roster names agents by slug: a row's name is its friendly title now.
	for _, a := range agents {
		names = append(names, a.Slug)
		if a.Slug == req.Agent || a.Name == req.Agent {
			known = true
		}
	}
	if !known {
		sort.Strings(names)
		return nil, "", protocol.ErrMcpUnknown.WithWhat("unknown agent %q — known agents: %s", req.Agent, strings.Join(names, ", "))
	}
	cfg, err := admin.config(ctx)
	if err != nil {
		if isNoConfig(err) {
			cfg = mcp.Config{} // MC11 at this door too: no rows is a state — core-only answers
		} else {
			return nil, fmt.Sprintf("refused: the MCP rows could not be assembled (%v) — refused rows are a store fault, not the legitimate absence", err), nil
		}
	}
	reg, err := mcp.New(cfg)
	if err != nil {
		return nil, fmt.Sprintf("refused: the MCP configuration refused the validator (%v)", err), nil
	}
	// The SAME policy store the daemon decides with (the tools.grants door's
	// own source — one policy truth for both doors).
	ps, frej := policy.NewStore(policyRepo(admin.st)).List(ctx, admin.now())
	if frej != nil {
		return nil, "", fmt.Errorf("the policy set could not be read (%s) — grants would be invented", frej.Reason)
	}
	at := admin.now()
	core := cfg.Agents[req.Agent].Core
	if _, ok := cfg.Agents[req.Agent]; !ok {
		core = nil // an agent with no MCP row has the WHOLE core set (the two empties, on the wire: null)
	}
	views := reg.GrantView(req.Agent, ps, at)
	// MC6's missingClasses join: the unclassified refusal view (registry.go
	// owns that fact too — this door states it, never re-derives it).
	refused, err := reg.Refused(req.Agent)
	if err != nil {
		return nil, "", err
	}
	missing := map[string]*[]string{}
	for _, rf := range refused {
		if rf.MissingClasses != nil {
			missing[rf.Qualified] = rf.MissingClasses
		}
	}
	grants := make([]protocol.MCPGrantView, 0, len(views))
	for _, g := range views {
		grants = append(grants, protocol.MCPGrantView{
			Qualified: g.Qualified, Tier: string(g.Tier), State: string(g.State),
			Reason: g.Reason, MissingClasses: missing[g.Qualified],
		})
	}
	return protocol.MCPGrantsResult{
		Agent: req.Agent, Core: core,
		Declares: cfg.Agents[req.Agent].Declares,
		Grants:   grants,
	}, "", nil
}

// --- mcp.vocabulary ---------------------------------------------------------------------

func mcpVocabularyRead(params json.RawMessage) (any, string, error) {
	if len(params) > 0 && string(params) != "{}" && string(params) != "null" {
		return nil, "refused: mcp.vocabulary takes no parameters — the vocabulary is the validator's, not a filtered view of it", nil
	}
	return mcpVocabularySource(), "", nil
}

// --- mcp.changes --------------------------------------------------------------------------

func mcpChangesRead(ctx context.Context, admin *mcpAdmin, params json.RawMessage) (any, string, error) {
	params = normalizeParams(params)
	var req protocol.MCPChangesParams
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Sprintf("refused: mcp.changes needs an object of optional filters (%v)", err), nil
	}
	limit := 0
	if req.Limit != nil {
		limit = *req.Limit
	}
	server, agentSlug := "", ""
	filtered := false
	if req.Server != nil {
		server, filtered = *req.Server, true
	}
	if req.Agent != nil {
		// History is keyed by slug (it outlives renames); the filter names
		// the dispatch key like every other door, so translate here — and
		// an unknown agent is the typed not-exists answer.
		a, found, err := admin.st.AgentByName(*req.Agent)
		if err != nil {
			return nil, "", err
		}
		if !found {
			return nil, "", protocol.ErrMcpUnknown.WithWhat("unknown agent %q — no agent by that name exists", *req.Agent)
		}
		agentSlug, filtered = a.Slug, true
	}
	rows, err := changesView(ctx, admin.st, server, agentSlug, limit)
	if err != nil {
		return nil, "", err
	}
	out := protocol.MCPChangesResult{Changes: rows}
	// The statement belongs to the Count-proved EMPTY TABLE, never to a
	// filter that matched nothing — "my filter found nothing" is the
	// filter's answer and must not read as "the audit is empty" (P31).
	if !filtered && len(rows) == 0 {
		out.Statement = protocol.MCPChangesNoChangesStatement
	}
	return out, "", nil
}

func changesView(ctx context.Context, st *store.Store, server, agentSlug string, limit int) ([]protocol.MCPChangeView, error) {
	rows, err := st.McpChangeList(ctx, server, agentSlug, limit)
	if err != nil {
		return nil, fmt.Errorf("read the MCP audit: %w", err)
	}
	out := make([]protocol.MCPChangeView, 0, len(rows))
	for _, c := range rows {
		v := protocol.MCPChangeView{
			ID: c.ID, Op: c.Op, Summary: c.Summary,
			ActorKind: c.ActorKind, ActorID: c.ActorID, OccurredAt: c.OccurredAt,
		}
		if c.Server != nil {
			v.Server = *c.Server
		}
		if c.AgentSlug != nil {
			v.Agent = *c.AgentSlug // history names what it named then (it outlives renames)
		}
		if c.Qualified != nil {
			v.Qualified = *c.Qualified
		}
		out = append(out, v)
	}
	return out, nil
}

// --- small helpers --------------------------------------------------------------------------

// isNoConfig is the level-0 sentinel test (errors.Is on mcp.ErrNoConfig).
func isNoConfig(err error) bool {
	return errors.Is(err, mcp.ErrNoConfig)
}

// editDistanceAtMostTwo is the "did you mean" net: cheap, bounded, and
// deliberately not a full Levenshtein table — a refusal helper must not
// become a second algorithm with its own bugs.
func editDistanceAtMostTwo(a, b string) bool {
	la, lb := len(a), len(b)
	if la > lb {
		a, b, la, lb = b, a, lb, la
	}
	if lb-la > 2 {
		return false
	}
	prev, cur := make([]int, lb+1), make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if cur[j] < best {
				best = cur[j]
			}
		}
		if best > 2 {
			return false
		}
		prev, cur = cur, prev
	}
	return prev[lb] <= 2
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
