package main

// mcp_verb_process.go — `aeon mcp processes|logs|start|stop|restart`: the MCP
// servers' processes at the terminal, clients of the daemon's `mcp.process*`
// doors. The processes belong to the running daemon, so these have no direct
// door: with no daemon they say so.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdMCPProcesses(w io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("mcp processes takes no arguments")
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var res protocol.MCPProcessesResult
		if err := d.relay(ctx, protocol.MethodMCPProcesses, map[string]any{}, &res); err != nil {
			return err
		}
		if len(res.Processes) == 0 {
			fmt.Fprintln(w, "no MCP servers configured")
			return nil
		}
		for _, p := range res.Processes {
			pid := "-"
			if p.PID > 0 {
				pid = fmt.Sprint(p.PID)
			}
			fmt.Fprintf(w, "%-24s %-9s pid %-7s restarts %-3d tools %-3d", p.Name, p.State, pid, p.Restarts, p.Tools)
			if p.LastExit != "" {
				fmt.Fprintf(w, "  last exit: %s", p.LastExit)
			}
			if p.LastError != "" {
				fmt.Fprintf(w, "  error: %s", p.LastError)
			}
			fmt.Fprintln(w)
		}
		return nil
	})
}

func cmdMCPLogs(w io.Writer, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("mcp logs takes the server name first")
	}
	name := args[0]
	fs := flag.NewFlagSet("mcp logs", flag.ContinueOnError)
	fs.SetOutput(w)
	limit := fs.Int("n", 200, "at most this many lines (the newest)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var res protocol.MCPProcessLogsResult
		if err := d.relay(ctx, protocol.MethodMCPProcessLogs, protocol.MCPProcessLogsParams{Name: name, Limit: *limit}, &res); err != nil {
			return err
		}
		for _, l := range res.Lines {
			fmt.Fprintf(w, "%s %-6s %s\n", l.At, l.Stream, l.Text)
		}
		if res.Dropped > 0 {
			fmt.Fprintf(w, "(%d earlier line(s) are no longer in the ring)\n", res.Dropped)
		}
		return nil
	})
}

// cmdMCPProcessControl is start / stop / restart: one name, one door.
func cmdMCPProcessControl(verb string, method protocol.MethodName) func(io.Writer, []string) error {
	return func(w io.Writer, args []string) error {
		if len(args) != 1 || args[0] == "" || strings.HasPrefix(args[0], "-") {
			return fmt.Errorf("mcp %s takes exactly one server name", verb)
		}
		name := args[0]
		return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
			var res protocol.MCPProcessControlResult
			if err := d.relay(ctx, method, protocol.MCPProcessControlParams{Name: name}, &res); err != nil {
				return err
			}
			p := res.Process
			fmt.Fprintf(w, "%s: %s", p.Name, p.State)
			if p.PID > 0 {
				fmt.Fprintf(w, " (pid %d)", p.PID)
			}
			fmt.Fprintln(w)
			return nil
		})
	}
}

func cmdMCPStart(w io.Writer, args []string) error {
	return cmdMCPProcessControl("start", protocol.MethodMCPProcessStart)(w, args)
}
func cmdMCPStop(w io.Writer, args []string) error {
	return cmdMCPProcessControl("stop", protocol.MethodMCPProcessStop)(w, args)
}
func cmdMCPRestart(w io.Writer, args []string) error {
	return cmdMCPProcessControl("restart", protocol.MethodMCPProcessRestart)(w, args)
}
