package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
)

// S36-mcp-dispatch T3 — the dial half of the seam (MD2, MD4). It takes T2's
// partition (offered / withheld) and, for every server holding ≥1 offered
// tool, dials it under mcpDialBudget in parallel, lists its LIVE tools, and
// builds the final specs from the live schema. The dial seam is a small
// interface (mcpClient) so the tests can inject a fake without a real
// subprocess — *mcp.Client satisfies it in production.

// mcpClient is the sliver of *mcp.Client the seam needs — enough to enumerate
// and to dispatch, and enough for a fake to stand in. *mcp.Client satisfies
// it (it has these three methods); a fake implements it in tests.
type mcpClient interface {
	ListTools(ctx context.Context) ([]mcp.RemoteTool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (mcp.CallResult, error)
	Close() error
}

// dialServer is the one dial path the seam uses: mcp.Dial in production, a
// fake in tests. It is a variable (not a parameter) because Options.StepMCP
// (T1) carries no dial; the seam owns it. A test overrides it and restores it
// — no production path reads a dial other than this one.
var dialServer = func(ctx context.Context, srv mcp.ServerConfig, getenv func(string) string) (mcpClient, error) {
	// A server the daemon supervises is used where it runs — its process outlives
	// the step, and a stopped one is withheld with the reason, never re-spawned
	// behind the person's back. Anything unmanaged dials as it always did.
	if mcpSup != nil {
		if c, managed, err := mcpSup.Lease(srv.Name); managed {
			return c, err
		}
	}
	c, err := mcp.Dial(ctx, srv, getenv)
	return c, err
}

// buildMCPBundle is the StepMCP implementation (T3 dials; T4 refines the
// Call's deadline and error-text and wires Close at the four BuildTools
// callers). It reads the operator's config through the shared path (Q1),
// partitions the offer (T2), then — only for servers that hold an offered
// tool — dials, lists, and intersects (MD2). A server that offers nothing to
// this agent is never dialled (MD8 / MC11's spirit, carried here).
func buildMCPBundle(ctx context.Context, agent string, ps []policy.Policy, at time.Time) (*daemon.MCPBundle, error) {
	cfg, err := loadMCPConfig()
	if errors.Is(err, mcp.ErrNoConfig) {
		// Absent config is an empty registry, not an error (MD8).
		cfg = mcp.Config{}
	} else if err != nil {
		return nil, err
	}
	reg, err := mcp.New(cfg)
	if err != nil {
		return nil, err
	}

	offered, withheld := partitionOffer(reg, agent, ps, at)
	// MG4 at the offer seam (T2's recorded landmine, honored here): the
	// registry was built from the include-disabled CANDIDATE (law 2 — the
	// validator saw everything, a declaration could never dangle past it),
	// so a disabled server's tools can reach `offered`; they are dropped
	// HERE, after partition, before any dial. A disable is not a revoke —
	// the declarations stand in the rows (that's why they're in the
	// candidate) and the server simply is not offered while off.
	if len(offered) > 0 {
		off, err := loadMCPDisabled()
		if err != nil {
			return nil, err
		}
		if len(off) > 0 {
			kept := offered[:0:0]
			for _, v := range offered {
				if srv, _, ok := splitQualifiedName(v.Qualified); ok && off[srv] {
					continue // not offered while the server is off (MG4) — no dial, no withheld drama
				}
				kept = append(kept, v)
			}
			offered = kept
		}
	}
	if len(offered) == 0 {
		// The whole point of the seam being optional: nothing offered means
		// no dials at all — the step is unaffected (MD8 / MC11).
		return &daemon.MCPBundle{
			Withheld: withheld,
			Close:    func() error { return nil },
		}, nil
	}

	// Group the offered tools by server; a qualified name with no server part
	// is a construction error (MD10 pin #2, also checked in BuildTools).
	byServer := map[string][]mcp.GrantView{}
	for _, v := range offered {
		srv, _, ok := splitQualifiedName(v.Qualified)
		if !ok {
			withheld = append(withheld, daemon.MCPWithheld{
				Qualified: v.Qualified,
				Sentence:  "unavailable: " + v.Qualified + " is not a qualified server.tool name",
			})
			continue
		}
		byServer[srv] = append(byServer[srv], v)
	}

	// Dial each server in parallel, bounded by mcpDialBudget. A slow or dead
	// server degrades its offered tools to withheld and never blocks another
	// server or the step start (criterion 4's build-time half).
	outcomes := make(map[string]serverOutcome, len(byServer))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for srv, offeredOnSrv := range byServer {
		wg.Add(1)
		go func(srv string, offeredOnSrv []mcp.GrantView) {
			defer wg.Done()
			out := enumerateServer(ctx, cfg, agent, srv, offeredOnSrv, mcpGetenv(""))
			mu.Lock()
			outcomes[srv] = out
			mu.Unlock()
		}(srv, offeredOnSrv)
	}
	wg.Wait()

	// Assemble: specs from the live intersection, withheld from T2 plus the
	// live cross-check failures, and the dialled clients cached for the Call.
	var specs []model.ToolSpec
	clients := map[string]mcpClient{}
	for srv, out := range outcomes {
		specs = append(specs, out.specs...)
		withheld = append(withheld, out.withheld...)
		if out.client != nil {
			clients[srv] = out.client
		}
	}

	return &daemon.MCPBundle{
		Specs:    specs,
		Withheld: withheld,
		Call:     makeCall(clients),
		Close:    closeClients(clients),
	}, nil
}

// enumerateServer is one server's dial-and-intersect: dial under the budget,
// list the live tools, intersect with the offered set. Every failure mode
// degrades to withheld — never a synthesized schema (MD2), never a hang.
func enumerateServer(ctx context.Context, cfg mcp.Config, agent, srv string, offeredOnSrv []mcp.GrantView, getenv func(string) string) serverOutcome {
	srvCfg, ok := serverConfigFor(cfg, agent, srv)
	if !ok {
		// The offered tool names a server the config does not carry: the
		// disagreement between config and the (absent) server costs every
		// offered tool the call (brief §R3, quoting step.go's own words).
		return serverOutcome{withheld: withheldAll(offeredOnSrv, "unavailable: server "+srv+" is not configured")}
	}

	// The budget is per server, at build — the parent ctx may be long-lived,
	// but this server's dial is bounded by its own budget (TestDialTimeout…).
	dctx, cancel := context.WithTimeout(ctx, mcpDialBudget)
	defer cancel()

	client, err := dialServer(dctx, srvCfg, mcpGetenvFor(srvCfg, ""))
	if err != nil {
		// Dial failed: every offered tool for this server is withheld with
		// the error text — the same shape cmdMCPList uses (down[srv] =
		// err.Error()), so the same fact is not worded two ways (MD9).
		return serverOutcome{withheld: withheldAll(offeredOnSrv, err.Error())}
	}

	tools, err := client.ListTools(dctx)
	if err != nil {
		// List failed (after a good dial): same degradation, and the client
		// is reaped so a dead half-open transport is not left open.
		_ = client.Close()
		return serverOutcome{withheld: withheldAll(offeredOnSrv, err.Error())}
	}

	liveByName := map[string]mcp.RemoteTool{}
	for _, t := range tools {
		liveByName[t.Name] = t
	}

	var specs []model.ToolSpec
	var wh []daemon.MCPWithheld
	for _, v := range offeredOnSrv {
		_, tool, _ := splitQualifiedName(v.Qualified)
		rt, ok := liveByName[tool]
		if !ok {
			// Offered by the config but absent from the server's live list:
			// withheld with a named sentence — never silently dropped (the
			// step would not know why a tool is missing) or silently kept
			// (a call the server no longer offers would fail at use).
			wh = append(wh, daemon.MCPWithheld{Qualified: v.Qualified, Sentence: noLongerListedSentence(v.Qualified, srv)})
			continue
		}
		// Live schema wins: the spec carries the server's live schema, never
		// a synthesized one from config's declared schema (MD2).
		specs = append(specs, model.ToolSpec{
			Name:        v.Qualified,
			Description: rt.Description,
			Parameters:  rt.InputSchema,
		})
	}
	return serverOutcome{specs: specs, withheld: wh, client: client}
}

// serverOutcome is one server's result from the parallel dial phase: the
// specs that survived the live cross-check, the withheld that did not, and
// the dialled client (nil on any failure) cached for the Call.
type serverOutcome struct {
	specs    []model.ToolSpec
	withheld []daemon.MCPWithheld
	client   mcpClient
}

// serverConfigFor finds a server's config by name for an agent — public or
// the agent's private — the only way the offered rows (which carry a
// qualified name, not a config pointer) get back to a dialable ServerConfig.
func serverConfigFor(cfg mcp.Config, agent, name string) (mcp.ServerConfig, bool) {
	for _, s := range cfg.Public {
		if s.Name == name {
			return s, true
		}
	}
	if ac, ok := cfg.Agents[agent]; ok {
		for _, s := range ac.Private {
			if s.Name == name {
				return s, true
			}
		}
	}
	return mcp.ServerConfig{}, false
}

// splitQualifiedName splits "server.tool" at the first dot — the same rule as
// mcp's (unexported) splitQualified: the server is the part before the first
// dot, a tool name may itself contain dots.
func splitQualifiedName(q string) (server, tool string, ok bool) {
	i := strings.Index(q, ".")
	if i <= 0 || i == len(q)-1 {
		return "", "", false
	}
	return q[:i], q[i+1:], true
}

// noLongerListedSentence is the ONE sentence for the "offered but the server
// no longer lists it" refusal (MD9). Picked once, tested for exactness in
// TestOfferedButNotLiveIsWithheld — a second wording would be two truths.
func noLongerListedSentence(qualified, server string) string {
	return "unavailable: " + qualified + " is no longer listed by " + server
}

func withheldAll(offered []mcp.GrantView, sentence string) []daemon.MCPWithheld {
	out := make([]daemon.MCPWithheld, 0, len(offered))
	for _, v := range offered {
		out = append(out, daemon.MCPWithheld{Qualified: v.Qualified, Sentence: sentence})
	}
	return out
}

// makeCall is the dispatch half the bundle carries: a qualified "server.tool"
// routes to the CACHED client's CallTool (one dial per server per step — T3
// cached the clients, this reuses them), bounded by mcpCallDeadline. Every
// failure (dial, timeout, call error, server-reported error, server crash)
// becomes an error-text result — never a thrown error that kills the step
// (criterion 4's dispatch half): the composite (T1) turns the (string, error)
// into a ToolOutcome with IsError = err != nil, so the step sees an
// error-text tool_result and continues.
func makeCall(clients map[string]mcpClient) daemon.MCPCall {
	return func(ctx context.Context, qualified string, args json.RawMessage) (string, error) {
		srv, tool, ok := splitQualifiedName(qualified)
		if !ok {
			return "error: " + qualified + " is not a qualified server.tool name",
				fmt.Errorf("mcp: %q is not a qualified server.tool name", qualified)
		}
		client, ok := clients[srv]
		if !ok {
			return "error: server " + srv + " was not dialled at build (its offered tools were withheld)",
				fmt.Errorf("mcp: server %q was not dialled at build", srv)
		}
		// The dispatch budget bounds this one call (MD4): the parent ctx may be
		// long-lived, but the call is bounded by mcpCallDeadline, and a timeout
		// degrades to an error-text result, never a hang (TestCallDeadlineBounds).
		dctx, cancel := context.WithTimeout(ctx, mcpCallDeadline)
		defer cancel()
		res, err := client.CallTool(dctx, tool, args)
		if err != nil {
			// Dial/timeout/call error: the error text is the result; the Go
			// error marks it IsError in the composite. Never a thrown error.
			return "error: " + err.Error(), err
		}
		if res.IsError {
			// The server spoke and said no: its text is the result, marked as
			// an error by the Go error the composite reads.
			return res.Text, fmt.Errorf("mcp: server %q reported an error for %s", srv, qualified)
		}
		return res.Text, nil
	}
}

// closeClients reaps every dialled client; one error is reported, the rest
// are still closed (MD5: a Close that stops at the first failure leaks the
// others).
func closeClients(clients map[string]mcpClient) func() error {
	return func() error {
		var first error
		for _, c := range clients {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
}
