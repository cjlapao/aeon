package protocol

// mcp_process.go — the MCP servers' PROCESSES, on the socket.
//
// The daemon starts every enabled stdio server when it starts and keeps it
// running; these doors are the control over that. Two reads — `mcp.processes`
// (state, PID, uptime, restarts, how it last ended) and `mcp.process.logs` (the
// server's stderr and the daemon's own lines about it, from a bounded ring) — and
// three human-door writes: start, stop (terminate) and restart. The actor is the
// connection's peer, never a param; an agent has no road to any of them.
//
// A server with an http transport has no process of ours: it is listed as
// `remote`, with no PID, and start/stop/restart answer that plainly.
//
// Like the rest of the MCP management plane this package holds no MCP logic and
// imports no internal/mcp: the types are plain, the seam (`Table.MCPAdmin`) is
// the cmd/aeon supervisor.

import (
	"context"
	"encoding/json"
)

// MCPProcessView is one server's process as the supervisor sees it.
type MCPProcessView struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	// State is one of: starting, running, stopped (terminated on purpose),
	// crashed (ended on its own), failed (never completed its handshake),
	// disabled (configured off), remote (http: nothing of ours to run).
	State     string `json:"state"`
	PID       int    `json:"pid,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	Restarts  int    `json:"restarts"`
	Tools     int    `json:"tools"`
	// LastExit says how the previous run ended ("exit status 3", "killed"); LastError
	// is why the last start failed. Both are the process's own words.
	LastExit  string `json:"lastExit,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type MCPProcessesResult struct {
	Processes []MCPProcessView `json:"processes"`
}

type MCPProcessLogsParams struct {
	Name string `json:"name"`
	// After is the last sequence number the caller already has; 0 asks for the
	// whole ring. Limit caps the lines returned (default 200, at most 1000).
	After int64 `json:"after,omitempty"`
	Limit int   `json:"limit,omitempty"`
}

// MCPProcessLogLine is one line of a server's log. Stream is "stderr" (the
// server's own output) or "daemon" (what the supervisor did to it).
type MCPProcessLogLine struct {
	Seq    int64  `json:"seq"`
	At     string `json:"at"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

type MCPProcessLogsResult struct {
	Name  string              `json:"name"`
	Lines []MCPProcessLogLine `json:"lines"`
	// Next is the sequence number to send as `after` next time.
	Next int64 `json:"next"`
	// Dropped counts lines that fell out of the ring since `after`: a gap is said, not hidden.
	Dropped int64 `json:"dropped,omitempty"`
}

type MCPProcessControlParams struct {
	Name string `json:"name"`
}

type MCPProcessControlResult struct {
	Process MCPProcessView `json:"process"`
}

func (t *Table) handleMCPProcesses(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPProcesses), params); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPProcesses, params)
}

func (t *Table) handleMCPProcessLogs(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodMCPProcessLogs), params); err != nil {
		return nil, err
	}
	var p MCPProcessLogsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return t.mcpAdminCall(ctx, MethodMCPProcessLogs, params)
}

func (t *Table) mcpProcessControl(name MethodName) func(context.Context, *Session, json.RawMessage) (any, error) {
	return func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
		var p MCPProcessControlParams
		c, err := t.mcpWriteEdge(ctx, sess, name, params, &p)
		if err != nil {
			return nil, err
		}
		return t.mcpAdminCall(c, name, params)
	}
}
