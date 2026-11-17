package main

// mcp_process_admin.go — the seam's answers for the process doors. The supervisor
// is the daemon's: the CLI's direct door has none, and says so rather than
// answering an empty list as if nothing ran.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
)

// mcpSup is the running daemon's supervisor; nil in any process that is not the
// daemon (the CLI's direct door, tests that do not build one).
var mcpSup *mcpSupervisor

const mcpNoSupervisor = "refused: the MCP processes are managed by the running daemon — start it with `aeon serve`; this process has none"

func mcpProcessesRead(params json.RawMessage) (any, string, error) {
	if mcpSup == nil {
		return nil, mcpNoSupervisor, nil
	}
	return protocol.MCPProcessesResult{Processes: mcpSup.List()}, "", nil
}

func mcpProcessLogsRead(params json.RawMessage) (any, string, error) {
	if mcpSup == nil {
		return nil, mcpNoSupervisor, nil
	}
	var p protocol.MCPProcessLogsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: mcp.process.logs params are malformed (%v)", err), nil
	}
	res, err := mcpSup.Logs(p.Name, p.After, p.Limit)
	if errors.Is(err, errNoProcess) {
		return nil, fmt.Sprintf("refused: %q is not a configured server", p.Name), nil
	}
	return res, "", err
}

// mcpProcessControl runs start / stop / restart for the person at the endpoint.
func mcpProcessControl(ctx context.Context, method string, params json.RawMessage, kind, id string) (any, string, error) {
	if mcpSup == nil {
		return nil, mcpNoSupervisor, nil
	}
	var p protocol.MCPProcessControlParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Sprintf("refused: %s params are malformed (%v)", method, err), nil
	}
	by := kind + ":" + id
	var (
		view    protocol.MCPProcessView
		refusal string
	)
	switch method {
	case "mcp.process.start":
		view, refusal = mcpSup.Start(context.WithoutCancel(ctx), p.Name, by)
	case "mcp.process.stop":
		view, refusal = mcpSup.Stop(p.Name, by)
	default:
		view, refusal = mcpSup.Restart(context.WithoutCancel(ctx), p.Name, by)
	}
	if refusal != "" {
		return nil, refusal, nil
	}
	return protocol.MCPProcessControlResult{Process: view}, "", nil
}
