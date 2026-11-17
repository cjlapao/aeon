package daemon

// ════════════════════════════════════════════════════════════════════════════
// The person's tools catalog adapter (S47 T5): the `protocol.ToolsCatalog` seam
// behind `tools.list`, `tools.get`, `tools.byAgent` and `tools.status`.
//
// # What lives here and what does not
//
// This file ASSEMBLES; it decides nothing. The four inputs the catalog is
// computed from — the code, the rows, the grant chain, the policy set — are read
// here because the daemon holds the store, the seed engine and the entry point's
// MCP seam. The join itself is `internal/toolcatalog`'s (T4): pure, clock
// injected, and asserted pair-by-pair against `policy.Evaluate`. Writing a second
// "may this agent call this tool" here would be exactly the bug TC5 refuses — a
// panel that says allowed while the step refuses.
//
// The four reads, and the authorities they read from:
//
//   - code: `toolseed.Source()` — one pass over `tools.Registry()`,
//     `tools.AllMeta()` and `records.Catalog()` — for every system tool's
//     identity, family, description, class, capabilities, governed set, twin,
//     aliases and conditional flag; plus `tools.Registry()` again for the SERVED
//     schema and its hash and for the availability fact (TC2: the row is a
//     projection, the spec the model is handed is code);
//   - rows: `Store.ToolList` for `version` and `origin`, and — through
//     `toolseed.Drift`, a pure function of rows and code that writes nothing —
//     `rowCurrent` (TC11: reported, never repaired);
//   - grants: `Store.AgentsAll` for each row's `tools` allow-list and its `origin`
//     (the SD7 rule: `system` dispatches, custom does not), `tools.DefaultGrant()`
//     for the floor, and the MCP declare rows through S46's `mcp.grants`;
//   - policy: `policy.NewStore(newPolicyRepo(store)).ListDetailed(ctx, at)` — the
//     same store the step's gate decides with (`table.PolicyStore`'s line above),
//     so the panel and the gate cannot disagree about which rows exist.
//
// # N8 — no cache, and what that costs
//
// Every call re-reads all four. The cost is a handful of cheap SELECTs per panel
// refresh; the alternative is the failure this slice exists to kill — a screen
// still showing a grant the gate stopped honouring, or an operator's new deny not
// appearing because the catalog memoised the old world. `TestToolsReadsAreLive`
// pins it. Memoisation is allowed only WITHIN a call (`mcp.grants` is read at
// most once per agent per request), which is not a cache because nothing survives
// the request.
//
// # MC11 — the MCP half arrives through the entry point's seam
//
// The daemon never imports `internal/mcp` (its own ban plus S46's `absent_test`).
// The MCP entries and the MCP authority view are read through `Options.MCPAdmin`,
// the same func value `mcp.servers` and `mcp.grants` ride. A nil seam means this
// build has no MCP half: the entries are absent and every qualified name reads
// not-held (V13 — an absent answer, never a refusal).
//
// # V11 — a failed read is an error, never an empty list
//
// If the policy set, the rows or the agents cannot be read, the door ERRORS.
// Rendering the catalog with no policies would show every held tool as
// `granted_no_rule` — complete, plausible, and wrong, which is the shape of a
// fabricated answer. An empty list is only ever what an actual empty world
// contains, and the filtered-empty sentence (the door's, in `internal/protocol`)
// keeps "filtered to nothing" distinct from "nothing exists".
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/toolseed"
)

// toolsCatalogService is the daemon-side seam, the `budgetService` shape: a small
// struct over the daemon's store, so the nil guard is a constructor and a
// storeless daemon hands the table nil — `RegisterToolsCatalog` then leaves the
// four doors ABSENT rather than registered-and-refusing (V13).
//
// `now` is the clock as a field rather than a call inline: `toolcatalog.Inputs.At`
// is what policy expiry is judged against, and a test that wants a rule to be
// lapsed says so with a date instead of waiting for one. Production passes
// `time.Now().UTC()`; these reads are live, not replayable, so the daemon holds no
// other clock for them.
type toolsCatalogService struct {
	s        *store.Store
	engine   *seed.Engine
	mcpAdmin func(ctx context.Context, method string, params json.RawMessage) (any, string, error)
	now      func() time.Time
}

// newToolsCatalogService is the nil guard the daemon's wiring calls (V13, the
// `NewAutonomyService(opts.Store)` precedent — a free function over Options
// rather than a method, because the mount happens while the table is being
// built, before the Daemon value exists). Everything it reads comes from
// Options the entry point already fills: `Store` for the rows, agents and policy
// set; `Seed` for the ONE engine `seed.status` reads too, so the catalog cannot
// report a second, differently stamped ledger for `tools.core` (P34); `MCPAdmin`
// for the MCP half — a nil seam is a build without an MCP half, not a refusal.
//
// There is deliberately no new `Options` field: the seam's implementation is
// daemon-owned (it touches no import-banned package), so the entry point has
// nothing to inject and a hand-built `daemon.Options{}` leaves the four doors
// absent instead of demanding a wiring nobody wrote.
// NewToolsCatalogService is the CLI's door onto the ONE adapter (S47 T8, P34):
// `aeon tools` offline builds the SAME four reads over a directly opened store
// rather than a second implementation of them, and passes the same three things
// the daemon's wiring passes (the store, the one seed engine, the one MCP admin
// func). The nil guard is the same one — a storeless Options hands back nil, and
// the CLI says so rather than printing an empty catalog (V13).
func NewToolsCatalogService(opts Options) protocol.ToolsCatalog {
	return newToolsCatalogService(opts)
}

func newToolsCatalogService(opts Options) protocol.ToolsCatalog {
	if opts.Store == nil {
		return nil
	}
	return &toolsCatalogService{
		s:        opts.Store,
		engine:   opts.Seed,
		mcpAdmin: opts.MCPAdmin,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// --- one read's world ----------------------------------------------------------

// catalogWorld is everything the four reads compute from, read once per call and
// discarded with it (N8). The field order is the order of the reasoning — code,
// rows, grants, policy, MCP — and every field is a fact some authority supplied:
// nothing here is derived from anything else except through `toolcatalog`.
type catalogWorld struct {
	at       time.Time
	code     []seed.Record
	contents map[string]toolseed.Content
	reg      map[string]tools.Tool
	rows     map[string]store.ToolRow
	drift    map[string]toolseed.DriftEntry
	agents   []store.Agent
	bySlug   map[string]store.Agent
	policies []policy.Policy
	origin   map[string]string // policy id → the row's 'system'/'user'/'import'
	servers  []protocol.MCPServerView
	view     *mcpGrantsView
	inputs   toolcatalog.Inputs
}

// world reads the four authorities. Any failure is returned as an error and the
// door fails rather than answering from a half-world (V11).
func (sc *toolsCatalogService) world(ctx context.Context) (*catalogWorld, error) {
	at := sc.now()

	code := toolseed.Source()
	contents := make(map[string]toolseed.Content, len(code))
	for _, rec := range code {
		c, err := toolseed.ContentOf(rec)
		if err != nil {
			// A record `Source()` did not produce is a bug in this build, and the
			// honest report names the record that cannot be read — not a catalog
			// that quietly lost a tool.
			return nil, fmt.Errorf("tools catalog: the seeded record %q cannot be read: %w", rec.Key, err)
		}
		contents[c.Name] = c
	}

	rows, err := sc.s.ToolList(ctx)
	if err != nil {
		return nil, fmt.Errorf("tools catalog: the tool rows could not be read: %w", err)
	}
	byName := make(map[string]store.ToolRow, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}
	// Drift is a pure function of rows and code (T3): it writes nothing and
	// repairs nothing, and it is the only place a row is consulted at all — as
	// data, never as the spec (TC2).
	drift := make(map[string]toolseed.DriftEntry, len(rows)+len(code))
	for _, e := range toolseed.Drift(ctx, rows, code) {
		drift[e.Name] = e
	}

	agents, err := sc.s.AgentsAll()
	if err != nil {
		return nil, fmt.Errorf("tools catalog: the agent rows could not be read: %w", err)
	}
	bySlug := make(map[string]store.Agent, len(agents))
	for _, a := range agents {
		bySlug[a.Slug] = a
	}

	// The gate's own store, built per call: `policy.Store` keeps no cache, so two
	// calls are two reads, which is what N8 asks for. `ListDetailed` rather than
	// `List` because the panel shows where a rule came from. The failure is a
	// `*Reject` and NOT an empty set — a catalog without policies would read as
	// "nothing refuses anything", the most dangerous plausible answer this file
	// could produce.
	ps := policy.NewStore(newPolicyRepo(sc.s))
	listed, rej := ps.ListDetailed(ctx, at)
	if rej != nil {
		return nil, fmt.Errorf("tools catalog: the policy set could not be read: %s", rej.Reason)
	}
	policies := make([]policy.Policy, 0, len(listed))
	origin := make(map[string]string, len(listed))
	for _, l := range listed {
		policies = append(policies, l.Policy)
		origin[l.ID] = l.Origin
	}

	w := &catalogWorld{
		at:       at,
		code:     code,
		contents: contents,
		reg:      tools.Registry(),
		rows:     byName,
		drift:    drift,
		agents:   agents,
		bySlug:   bySlug,
		policies: policies,
		origin:   origin,
	}

	if sc.mcpAdmin != nil {
		servers, err := sc.enabledServers(ctx)
		if err != nil {
			return nil, err
		}
		w.servers = servers
		w.view = &mcpGrantsView{sc: sc, ctx: ctx, memo: map[string]*protocol.MCPGrantsResult{}}
	}

	w.inputs = toolcatalog.Inputs{
		DefaultGrant: tools.DefaultGrant(),
		AgentGrants:  w.agentGrants,
		Policies:     policies,
		At:           at,
	}
	if w.view != nil {
		w.inputs.MCP = w.view // a nil view would be a typed nil: absent means the FIELD is nil
	}
	return w, nil
}

// agentGrants is `toolcatalog.Inputs.AgentGrants`: the row's allow-list, the SD7
// dispatch fact, and whether the agent exists at all. A row whose `tools` column
// cannot be parsed keeps the empty half rather than inventing a grant — the same
// rule `toolcatalog.grantsFor` applies to a malformed list (T4's deviation 4).
func (w *catalogWorld) agentGrants(agent string) (granted []string, dispatchable bool, ok bool) {
	a, found := w.bySlug[agent]
	if !found {
		return nil, false, false
	}
	var list []string
	if s := strings.TrimSpace(a.Tools); s != "" && s != "null" {
		if err := json.Unmarshal([]byte(s), &list); err != nil {
			list = nil
		}
	}
	return list, true, true
}
