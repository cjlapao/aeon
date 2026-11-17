package main

// mcp_admin_inspect.go — `mcp.server.inspect`: run a CANDIDATE server and read
// what it offers, the way an MCP inspector does.
//
// The person types a command (`npx -y @modelcontextprotocol/server-github`) and
// the daemon starts it WITHOUT anything being installed or stored — npx fetches
// into its own cache, the process speaks initialize and tools/list, and it is
// stopped before the answer returns. Nothing here builds a tools/call request
// (probe.go's rule, held for this door too), and nothing is written: the answer
// is the server's own description of itself — names, descriptions, input
// schemas and annotation hints — for the person to turn into a classified tool
// list. The server's report is untrusted data on its way to being classified by
// OUR configuration (the registry's Q4): it travels to the form as hints.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
)

// mcpInspectTimeout bounds one inspect. Generous on purpose: a first `npx -y`
// downloads the package before the server can answer, which is the one case a
// 5-second probe would call a failure.
var mcpInspectTimeout = 2 * time.Minute

// Caps keep a hostile or chatty server from putting megabytes on the wire.
const (
	mcpInspectMaxTools       = 500
	mcpInspectMaxDescription = 4 << 10
	mcpInspectMaxSchema      = 32 << 10
)

// mcpInspectDial is the seam a test replaces: the real one is mcp.Dial.
var mcpInspectDial = func(ctx context.Context, srv mcp.ServerConfig, getenv func(string) string) (mcpInspectClient, error) {
	return mcp.Dial(ctx, srv, getenv)
}

// mcpInspectClient is the slice of mcp.Client an inspect uses — initialize has
// already happened inside Dial, so only the listing and the release remain.
type mcpInspectClient interface {
	ServerInfo() mcp.ServerInfo
	ProtocolVersion() string
	ListTools(ctx context.Context) ([]mcp.RemoteTool, error)
	Close() error
}

func (a *mcpAdmin) inspectServer(ctx context.Context, p protocol.MCPServerInspectParams) (any, string, error) {
	srv := mcp.ServerConfig{Name: "inspect", Transport: mcp.TransportConfig{
		Type: p.Transport.Type, Command: p.Transport.Command, Args: p.Transport.Args,
		Env: p.Transport.Env, EnvFrom: p.Transport.EnvFrom, URL: p.Transport.URL,
	}}
	if err := mcp.ValidateTransport(srv); err != nil {
		return nil, "refused: " + err.Error(), nil
	}

	runCtx, cancel := context.WithTimeout(ctx, mcpInspectTimeout)
	defer cancel()
	start := time.Now()
	// The daemon's own environment by default. Stored variables reach a candidate
	// command only when the person asked for them in this request: the command is
	// whatever they typed, and a secret that can be put in front of any process is
	// a secret that can be read.
	getenv := os.Getenv
	if p.UseVariables {
		getenv = mcpGetenvFor(srv, "")
	}
	// A value typed in THIS request is the person's own input, not a stored secret:
	// it goes to the command they are trying, and is kept nowhere.
	if typed := p.Transport.EnvValues; len(typed) > 0 {
		inner := getenv
		getenv = func(name string) string {
			if v, ok := typed[name]; ok {
				return v
			}
			return inner(name)
		}
	}
	client, err := mcpInspectDial(runCtx, srv, getenv)
	if err != nil {
		return nil, inspectRefusal("start the server and complete its handshake", err, runCtx), nil
	}
	defer func() { _ = client.Close() }()

	remote, err := client.ListTools(runCtx)
	if err != nil {
		return nil, inspectRefusal("list the server's tools", err, runCtx), nil
	}
	info := client.ServerInfo()
	res := protocol.MCPServerInspectResult{
		ServerName: info.Name, ServerVersion: info.Version, ProtocolVersion: client.ProtocolVersion(),
		Ms: time.Since(start).Milliseconds(), Tools: []protocol.MCPInspectTool{},
	}
	for i, r := range remote {
		if i == mcpInspectMaxTools {
			break
		}
		t := protocol.MCPInspectTool{Name: r.Name, Description: clip(r.Description, mcpInspectMaxDescription)}
		if len(r.InputSchema) > 0 && len(r.InputSchema) <= mcpInspectMaxSchema {
			t.InputSchema = r.InputSchema
		}
		if r.Annotations != nil {
			t.Annotations = &protocol.MCPInspectToolAnnotations{
				ReadOnlyHint: r.Annotations.ReadOnlyHint, DestructiveHint: r.Annotations.DestructiveHint,
				IdempotentHint: r.Annotations.IdempotentHint, OpenWorldHint: r.Annotations.OpenWorldHint,
			}
		}
		res.Tools = append(res.Tools, t)
	}
	return res, "", nil
}

// inspectRefusal words a failed run as the person's own fact — the command they
// typed did not produce a server — rather than as a daemon failure.
func inspectRefusal(doing string, err error, ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("refused: the server did not %s within %s — the first run of an npx server downloads it, so run the command once in a terminal to warm npm's cache and inspect again", doing, mcpInspectTimeout)
	}
	return fmt.Sprintf("refused: could not %s: %v", doing, err)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
