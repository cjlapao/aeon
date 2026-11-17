package main

// mcp_verb_inspect.go — `aeon mcp inspect`: run a candidate server's command and
// list the tools it offers, over the daemon's `mcp.server.inspect` door. The
// command runs on the daemon, nothing is installed or stored, and the output is
// the server's own description of each tool — what a person reads before
// `aeon mcp add --tool NAME:RERUN:EFFECT,...` classifies them.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// repeatedFlag collects a flag given more than once, in order — an argument
// list where one argument may hold spaces cannot ride a comma-separated flag.
type repeatedFlag []string

func (r *repeatedFlag) String() string     { return strings.Join(*r, " ") }
func (r *repeatedFlag) Set(v string) error { *r = append(*r, v); return nil }

func cmdMCPInspect(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp inspect", flag.ContinueOnError)
	var cmdArgs repeatedFlag
	var (
		tr      = fs.String("transport", "", "stdio or http (required)")
		command = fs.String("command", "", "the stdio command, e.g. npx")
		env     = fs.String("env", "", "comma-separated environment variable NAMES (names only)")
		envFrom = fs.String("env-from", "", "comma-separated NAME=VARIABLE: bind a name to a stored variable (used with --use-variables)")
		url     = fs.String("url", "", "the http URL")
		useVars = fs.Bool("use-variables", false, "give the command the stored VALUES of the --env names (from `aeon vault`); without it the command sees the daemon's own environment only")
	)
	fs.Var(&cmdArgs, "arg", "one command argument; repeat for each (e.g. --arg -y --arg @modelcontextprotocol/server-github)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp inspect takes no positional arguments — every fact is a flag")
	}
	transport, err := mcpParseTransport(*tr, *command, *env, *url, cmdArgs, *envFrom)
	if err != nil {
		return err
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var res protocol.MCPServerInspectResult
		if err := d.relay(ctx, protocol.MethodMCPServerInspect, protocol.MCPServerInspectParams{Transport: transport, UseVariables: *useVars}, &res); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s %s (MCP %s) — %d tool(s) in %dms\n", res.ServerName, res.ServerVersion, res.ProtocolVersion, len(res.Tools), res.Ms)
		for _, t := range res.Tools {
			fmt.Fprintf(w, "  %s%s\n", t.Name, inspectHints(t.Annotations))
			if first := inspectFirstLine(t.Description); first != "" {
				fmt.Fprintf(w, "      %s\n", first)
			}
		}
		return nil
	})
}

func inspectHints(a *protocol.MCPInspectToolAnnotations) string {
	if a == nil {
		return ""
	}
	var hints []string
	set := func(name string, v *bool) {
		if v != nil && *v {
			hints = append(hints, name)
		}
	}
	set("readOnly", a.ReadOnlyHint)
	set("destructive", a.DestructiveHint)
	set("idempotent", a.IdempotentHint)
	set("openWorld", a.OpenWorldHint)
	if len(hints) == 0 {
		return ""
	}
	return "  [" + strings.Join(hints, ", ") + "]"
}

func inspectFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
