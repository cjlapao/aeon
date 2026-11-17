// mcp_services.go — the S34 trust bench's MCP seam: `mcp.status` (T3) and
// `mcp.check` (T4), dispatched through one entry-point func (daemon.Options.MCP,
// Table.MCP) because internal/protocol must never import internal/mcp
// (internal/mcp/absent_test.go's banned list names it explicitly — the same
// rule S36-mcp-dispatch/design.md MD1 states for step dispatch).
//
// # Reality finding (T3) — config load is all-or-nothing
//
// Verified against internal/mcp/config.go: LoadConfigFile -> parseConfig ->
// Config.Validate() returns ONE error for the whole file, or none. There is
// no partial load where some servers succeed and others carry a per-server
// `refused` string — design.md §2.3's sketch of that cannot happen with the
// real loader. The three real states mcp.status renders:
//
//  1. absent        — errors.Is(err, mcp.ErrNoConfig): {present:false, configPath}
//  2. present+valid — the ordinary case: {present:true, configPath, servers, probes}
//  3. present+broken — Validate()/decode refused it: {present:true, configPath,
//     loadError}, no servers key (there is no partial Config to report from)
//
// `tier` and `declaredBy` are DERIVED, never stored: a server in Config.Public
// is "public"; a server in an agent's AgentConfig.Private is "private" and
// belongs to exactly that one agent (MC2). declaredBy for a public server is
// every agent whose Declares names a qualified tool under that server's
// namespace; for a private server it is the single owning agent.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mcpCheckTool is one tool as the LIVE server reported it to one mcp.check
// call — the real RemoteTool fields (internal/mcp.ProbeTool), never an
// invented classification (see the probe.go reality finding this task also
// applies here: design.md's example `class` field does not exist).
type mcpCheckTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// mcpCheckResult is both `mcp.check`'s own response AND T4's cache row
// (mcp.status's `probes` field is a count-only PROJECTION of this, derived
// at render time rather than stored twice — see probeSummary below).
// CheckedAt empty means "not checked"; there is no other spelling of that
// state (TB2) — the cache starts EMPTY, never pre-seeded with one row per
// known server (brief §5: restarting the daemon returns not checked, never
// a stale lie).
type mcpCheckResult struct {
	Server    string         `json:"server"`
	OK        bool           `json:"ok"`
	Tools     []mcpCheckTool `json:"tools,omitempty"`
	Error     string         `json:"error,omitempty"`
	Ms        int64          `json:"ms"`
	CheckedAt string         `json:"checkedAt"` // RFC3339; empty means not checked
}

// probeSummary is mcp.status's `probes` entry — a count, not the full tool
// list (the full list is mcp.check's own response; mcp.status never dials
// anything, so it only ever reflects what the cache already knows).
type probeSummary struct {
	OK        bool   `json:"ok"`
	Tools     int    `json:"tools,omitempty"`
	Error     string `json:"error,omitempty"`
	Ms        int64  `json:"ms"`
	CheckedAt string `json:"checkedAt"`
}

func summarize(r mcpCheckResult) probeSummary {
	return probeSummary{OK: r.OK, Tools: len(r.Tools), Error: r.Error, Ms: r.Ms, CheckedAt: r.CheckedAt}
}

// mcpServerWire is one configured server as mcp.status renders it.
type mcpServerWire struct {
	Name       string   `json:"name"`
	Tier       string   `json:"tier"` // "public" | "private" — derived, never stored
	Transport  string   `json:"transport"`
	Command    string   `json:"command,omitempty"`
	URL        string   `json:"url,omitempty"`
	DeclaredBy []string `json:"declaredBy"`
}

// mcpStatusResult is `mcp.status`'s wire shape — the three states folded
// into one struct via which optional fields are present (TB2: a page reads
// `present` first, then `loadError` or `servers`, never guesses from shape).
type mcpStatusResult struct {
	ConfigPath string                  `json:"configPath"`
	Present    bool                    `json:"present"`
	LoadError  string                  `json:"loadError,omitempty"`
	Servers    []mcpServerWire         `json:"servers,omitempty"`
	Probes     map[string]probeSummary `json:"probes"`
}

// mcpProbeCache is the daemon-lifetime cache T4's mcp.check writes to and
// mcp.status reads from. A plain mutex-guarded map, not the store: brief §5
// is explicit that a restart returns "not checked" — this is memory on
// purpose, never a row. inFlight is the single-flight guard (TB8): a
// channel per server currently being probed, closed (and removed) the
// moment that probe's result lands in rows — a map[string]*sync.Once would
// be wrong here because a Once cannot be reused for a LATER check once the
// first has finished (brief §6.2, TB8).
type mcpProbeCache struct {
	mu       sync.Mutex
	rows     map[string]mcpCheckResult
	inFlight map[string]chan struct{}
}

func newMCPProbeCache() *mcpProbeCache {
	return &mcpProbeCache{rows: map[string]mcpCheckResult{}, inFlight: map[string]chan struct{}{}}
}

func (c *mcpProbeCache) snapshot() map[string]probeSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]probeSummary, len(c.rows))
	for k, v := range c.rows {
		out[k] = summarize(v)
	}
	return out
}

// checkOrJoin runs do() for server, or — if a probe for the same server is
// already in flight — waits for that one and returns its result instead of
// starting a second. Exactly one dial happens per overlapping burst of
// clicks (TB8's single-flight), proven by TestMCPCheckSingleFlight counting
// calls to a spy do().
func (c *mcpProbeCache) checkOrJoin(server string, do func() mcpCheckResult) mcpCheckResult {
	c.mu.Lock()
	if ch, ok := c.inFlight[server]; ok {
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		r := c.rows[server]
		c.mu.Unlock()
		return r
	}
	done := make(chan struct{})
	c.inFlight[server] = done
	c.mu.Unlock()

	r := do()

	c.mu.Lock()
	c.rows[server] = r
	delete(c.inFlight, server)
	c.mu.Unlock()
	close(done)
	return r
}

// wireMCPServices wires Table.MCP at the entry point — called once, from
// cmd/aeon serve, beside wireAgentServices.
func wireMCPServices(st *store.Store, opts *daemon.Options) error {
	cache := newMCPProbeCache()
	opts.MCP = mcpHandleFor(st, cache)
	// S46 T3: the management reads ride the SAME probe cache — mcp.servers
	// joins the very answers this door's probes produced (one probe truth,
	// MG11), and the seam itself holds no values (MG12: every answer
	// re-reads the tables).
	opts.MCPAdmin = mcpAdminHandleFor(st, cache)
	return nil
}

// mcpHandleFor answers Table.MCP, dispatched by method name. It checks
// nothing beyond that dispatch — the real work is mcpStatus below (and T4's
// mcpCheck, added to this same switch rather than a second seam).
func mcpHandleFor(st *store.Store, cache *mcpProbeCache) func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	return func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
		switch protocol.MethodName(method) {
		case protocol.MethodMCPStatus:
			return mcpStatus(cache)
		case protocol.MethodMCPCheck:
			return mcpCheck(ctx, params, cache)
		default:
			// Defensive: the protocol-side dispatcher should never send a
			// method this func doesn't handle — a seam that trusts its
			// caller silently is how a typo ships.
			return nil, fmt.Sprintf("refused: %q is not a method this seam answers", method), nil
		}
	}
}

// mcpStatus implements T3. See the file header for the three-state finding.
// S46 T3: the three states are now the TABLES' — no rows is the legitimate
// absence (present:false, the state, not a refusal), rows that assemble is
// the normal state, and rows the one validator refuses are the broken
// state (which the store's CHECK net makes a store-bug, never a user
// state). configPath stays the path a LEGACY file would live at — the
// importer's and the doctor's fact (T4), named here because the operator
// reading status is the same operator deciding whether to import.
func mcpStatus(cache *mcpProbeCache) (any, string, error) {
	path := mcpLegacyPath()
	out := mcpStatusResult{ConfigPath: path, Probes: cache.snapshot()}

	cfg, err := loadMCPConfig()
	if isNoConfig(err) {
		out.Present = false
		return out, "", nil // MC11: absence is a state, not a refusal
	}
	if err != nil {
		// Present and broken — the rows exist but the one validator
		// refused them. There is no partial Config to report servers from.
		out.Present = true
		out.LoadError = err.Error()
		return out, "", nil
	}

	out.Present = true
	out.Servers = []mcpServerWire{}

	declaresByAgent := map[string][]string{}
	for agent, ac := range cfg.Agents {
		declaresByAgent[agent] = ac.Declares
	}
	for _, s := range cfg.Public {
		var declaredBy []string
		prefix := s.Name + "."
		for agent, declares := range declaresByAgent {
			for _, d := range declares {
				if strings.HasPrefix(d, prefix) {
					declaredBy = append(declaredBy, agent)
					break
				}
			}
		}
		out.Servers = append(out.Servers, serverWire(s, "public", declaredBy))
	}
	agentKeys := make([]string, 0, len(cfg.Agents))
	for agent := range cfg.Agents {
		agentKeys = append(agentKeys, agent)
	}
	sort.Strings(agentKeys) // the map coin, refused (see serverWire)
	for _, agent := range agentKeys {
		ac := cfg.Agents[agent]
		for _, s := range ac.Private {
			out.Servers = append(out.Servers, serverWire(s, "private", []string{agent}))
		}
	}
	return out, "", nil
}

func serverWire(s mcp.ServerConfig, tier string, declaredBy []string) mcpServerWire {
	// The declarers arrive from a MAP range above, and Go randomizes map
	// order per iteration — the S34 golden survived Phase A on a coin
	// that kept landing the same way. Sort where the view is built, so
	// `declaredBy` is the SET it claims to be and every surface (and the
	// golden) reads it in one deterministic order. This changes bytes
	// only relative to a flip of the coin that never should have been
	// spun; the pinned golden is the sorted one.
	sort.Strings(declaredBy)
	return mcpServerWire{
		Name: s.Name, Tier: tier, Transport: s.Transport.Type,
		Command: s.Transport.Command, URL: s.Transport.URL,
		DeclaredBy: declaredBy,
	}
}

// findServer loads the config fresh (the SAME rows-backed path mcpStatus
// reads) and looks for name across the public tier and every agent's
// private tier. The second return is every known server name, sorted, for
// an unknown-name refusal to list (same convention as T2's unknown-agent
// refusal). S46 T3: the read is the tables', not the file's.
func findServer(name string) (mcp.ServerConfig, []string, bool, error) {
	cfg, err := loadMCPConfig()
	if err != nil {
		return mcp.ServerConfig{}, nil, false, err
	}
	var names []string
	for _, s := range cfg.Public {
		names = append(names, s.Name)
		if s.Name == name {
			return s, names, true, nil
		}
	}
	for _, ac := range cfg.Agents {
		for _, s := range ac.Private {
			names = append(names, s.Name)
			if s.Name == name {
				return s, names, true, nil
			}
		}
	}
	sort.Strings(names)
	return mcp.ServerConfig{}, names, false, nil
}

// mcpCheck implements T4: one operator-triggered probe of one configured
// server, single-flight per name, bounded by mcp.DefaultProbeTimeout, cached
// with its timestamp. Dial/ListTools are internal/mcp's own (mcp.Probe) —
// this function adds no dial logic of its own, only the single-flight join,
// the timeout, and the cache write.
func mcpCheck(ctx context.Context, params json.RawMessage, cache *mcpProbeCache) (any, string, error) {
	var req struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Sprintf("refused: mcp.check needs an object naming `server` (%v)", err), nil
	}
	if strings.TrimSpace(req.Server) == "" {
		return nil, "refused: mcp.check needs a non-empty `server` field", nil
	}

	srv, known, found, err := findServer(req.Server)
	if errors.Is(err, mcp.ErrNoConfig) {
		return nil, fmt.Sprintf("refused: there is no MCP config to check %q against", req.Server), nil
	}
	if err != nil {
		return nil, fmt.Sprintf("refused: the MCP config could not be read (%v) — there is nothing to dial", err), nil
	}
	if !found {
		return nil, fmt.Sprintf("refused: unknown server %q — known servers: %s", req.Server, strings.Join(known, ", ")), nil
	}

	result := cache.checkOrJoin(req.Server, func() mcpCheckResult {
		probeCtx, cancel := context.WithTimeout(ctx, mcp.DefaultProbeTimeout)
		defer cancel()
		r := mcp.Probe(probeCtx, srv, mcpGetenvFor(srv, ""))
		out := mcpCheckResult{
			Server: req.Server, OK: r.OK, Ms: r.Ms,
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if r.OK {
			out.Tools = make([]mcpCheckTool, 0, len(r.Tools))
			for _, t := range r.Tools {
				out.Tools = append(out.Tools, mcpCheckTool{Name: t.Name, Description: t.Description})
			}
		} else if r.Err != nil {
			out.Error = r.Err.Error()
		}
		return out
	})
	return result, "", nil
}
