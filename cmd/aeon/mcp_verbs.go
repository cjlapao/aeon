package main

// mcp_verbs.go — S46 T9 (MG5, MG6). The management verbs of `aeon mcp`,
// each one a thin CLIENT of its socket method over mcpDoor (T9's parity
// rule: no verb without a socket twin, and the twin is the only thing the
// verb speaks). `list`, `call` and `serve` keep their own files and their
// own meaning; `list`'s presentation stays byte-pinned by T3's goldens —
// its rows already come from the same functions the daemon's reads answer
// from (see T9.md's recorded deviation for why the goldens own that verb's
// presentation and the door owns every verb added here).
//
// # The CLI re-validates NOTHING
//
// Flag parsing refuses only what cannot be a value: a --env NAME=value
// (C17's rule applied before a value exists), a malformed --tool triple
// (input parsing splits on `:`; classifying is the validator's), a
// relative --path. Every other refusal is the validator's or the seam's
// sentence, VERBATIM, from whichever door answered — and both doors run
// the same functions (mcp_client.go), so a byte differs by no door.
//
// # --yes: a seatbelt, not a lock
//
// The verbs that WIDEN (declare, import-config's replace) and the remove of
// a server others declare print their plan and refuse without --yes,
// writing nothing (S44/S45's stance, the seed apply and autonomy grant
// sentences' siblings). The seam holds the real line; this gate stops the
// typo.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// mcpWithDoor selects a door, runs one verb through it, and closes the
// door. The ctx travels to the direct door's handler functions (the socket
// door stamps the peer instead); timeouts are the daemon's problem, not
// the CLI's — dialTimeout already bounds the connection half.
// mcpWithDoor selects a door and runs one verb through it. The fixed
// offline line goes to os.Stderr — policy.go's precedent (the operator's
// terminal sees it; a test of the line drives selectMCPDoor directly, as
// S45's TestSelectDoorFallsBackOnlyWhenNoDaemon does).
func mcpWithDoor(w io.Writer, run func(ctx context.Context, d mcpDoor) error) error {
	d, err := selectMCPDoor(os.Stderr)
	if err != nil {
		return err
	}
	defer func() { _ = d.close() }()
	return run(context.Background(), d)
}

// --- reads ----------------------------------------------------------------------

func cmdMCPShow(w io.Writer, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("mcp show takes exactly one server name")
	}
	name := args[0]
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var res protocol.MCPServerResult
		if err := d.relay(ctx, protocol.MethodMCPServer, protocol.MCPServerParams{Name: name}, &res); err != nil {
			return err
		}
		s := res.Server
		fmt.Fprintf(w, "%s (%s", s.Name, s.Tier)
		if s.Agent != "" {
			fmt.Fprintf(w, ", owned by %s", s.Agent)
		}
		fmt.Fprintf(w, ")\n  transport: %s %s%s\n", s.Transport.Type, s.Transport.Command, transportTail(s.Transport))
		if !s.Enabled {
			fmt.Fprintln(w, "  DISABLED (MC3: not offered, not dialled; declarations stand)")
		}
		fmt.Fprintf(w, "  origin: %s   updated: %s\n", s.Origin, s.UpdatedAt)
		if len(s.Tools) == 0 {
			fmt.Fprintln(w, "  tools: none declared")
		}
		for _, t := range s.Tools {
			mark := ""
			if !t.Classified {
				mark = "  UNCLASSIFIED (refused at call time until classified — MC6)"
			}
			fmt.Fprintf(w, "    %-24s %s/%s%s\n", t.Name, mcpOrNone(t.Rerun), mcpOrNone(t.Effect), mark)
		}
		if len(res.DeclaredBy) > 0 {
			fmt.Fprintf(w, "  declared by: %s\n", strings.Join(res.DeclaredBy, ", "))
		}
		for _, c := range res.Changes {
			fmt.Fprintf(w, "  #%d %s %s/%s — %s (%s%s)\n", c.ID, c.Op, c.Server, c.Qualified, c.Summary, c.ActorKind, actorTail(c))
		}
		return nil
	})
}

func cmdMCPGrantsVerb(w io.Writer, args []string) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("mcp grants takes exactly one agent slug")
	}
	agent := args[0]
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var res protocol.MCPGrantsResult
		if err := d.relay(ctx, protocol.MethodMCPGrants, protocol.MCPGrantsParams{Agent: agent}, &res); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\n", res.Agent)
		if res.Core == nil {
			fmt.Fprintln(w, "  core: the whole core set (no narrowing)")
		} else if len(res.Core) == 0 {
			fmt.Fprintln(w, "  core: NONE (narrowed to nothing)")
		} else {
			fmt.Fprintf(w, "  core: %s\n", strings.Join(res.Core, ", "))
		}
		if len(res.Declares) == 0 {
			fmt.Fprintln(w, "  declares: none")
		} else {
			fmt.Fprintf(w, "  declares: %s\n", strings.Join(res.Declares, ", "))
		}
		for _, g := range res.Grants {
			line := fmt.Sprintf("    %-28s %s/%s  %s", g.Qualified, g.Tier, stateWord(g.State), mcpOrNone(g.Reason))
			if g.MissingClasses != nil && len(*g.MissingClasses) > 0 {
				line += fmt.Sprintf("  (missing: %s)", strings.Join(*g.MissingClasses, ","))
			}
			fmt.Fprintln(w, line)
		}
		return nil
	})
}

func cmdMCPChanges(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp changes", flag.ContinueOnError)
	var (
		server = fs.String("server", "", "only this server's rows")
		agent  = fs.String("agent", "", "only rows touching this agent")
		limit  = fs.Int("limit", 0, "how many rows (default the seam's)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp changes takes no positional arguments")
	}
	p := protocol.MCPChangesParams{}
	if *server != "" {
		p.Server = server
	}
	if *agent != "" {
		p.Agent = agent
	}
	if *limit > 0 {
		p.Limit = limit
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var res protocol.MCPChangesResult
		if err := d.relay(ctx, protocol.MethodMCPChanges, p, &res); err != nil {
			return err
		}
		for _, c := range res.Changes {
			fmt.Fprintf(w, "#%d %s %s/%s — %s (%s%s)\n", c.ID, c.Op, c.Server, c.Qualified, c.Summary, c.ActorKind, actorTail(c))
		}
		if res.Statement != "" {
			fmt.Fprintln(w, res.Statement)
		}
		return nil
	})
}

// cmdMCPStatusWord / cmdMCPCheckWord are thin clients of the two S34 reads
// (MG5/MG11): the bytes are the answer's bytes, relayed as compact JSON.
// check DIALS (it is the probe door, single-flight behind the cache);
// status never does — it joins the cache and says "unprobed" honestly.
func cmdMCPStatusWord(w io.Writer, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("mcp status takes no arguments (the cached answer for every configured server)")
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var raw json.RawMessage
		if err := d.relay(ctx, protocol.MethodMCPStatus, struct{}{}, &raw); err != nil {
			return err
		}
		return writePrettyJSON(w, raw)
	})
}

func cmdMCPCheckWord(w io.Writer, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("mcp check takes at most one server name")
	}
	var p struct {
		Server string `json:"server,omitempty"`
	}
	if len(args) == 1 {
		p.Server = args[0]
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		var raw json.RawMessage
		if err := d.relay(ctx, protocol.MethodMCPCheck, p, &raw); err != nil {
			return err
		}
		return writePrettyJSON(w, raw)
	})
}

// --- writes ----------------------------------------------------------------------

func cmdMCPAdd(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	var cmdArgs repeatedFlag
	fs.Var(&cmdArgs, "arg", "one command argument; repeat for each (e.g. --arg -y --arg @modelcontextprotocol/server-github)")
	var (
		name     = fs.String("name", "", "server name (required)")
		title    = fs.String("title", "", "the friendly name the app shows (the name stays the identifier)")
		descr    = fs.String("description", "", "what the server is for")
		tier     = fs.String("tier", "", "public or private (required)")
		agent    = fs.String("agent", "", "owning slug (required for private, forbidden for public)")
		tr       = fs.String("transport", "", "stdio or http (required)")
		command  = fs.String("command", "", "the stdio command")
		env      = fs.String("env", "", "comma-separated environment variable NAMES the process sees (names only — never a value on a flag)")
		envFrom  = fs.String("env-from", "", "comma-separated NAME=VARIABLE: bind each env name to a stored variable (aeon vault); every env name needs a binding — an environment variable cannot be empty")
		url      = fs.String("url", "", "the http URL")
		tools    = fs.String("tool", "", "comma-separated NAME:RERUN:EFFECT triples")
		disabled = fs.Bool("disabled", false, "add disabled (MC3: presence means enabled)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp add takes no positional arguments — every fact is a flag")
	}
	triple, err := mcpParseTransport(*tr, *command, *env, *url, cmdArgs, *envFrom)
	if err != nil {
		return err
	}
	toolRows, err := mcpParseTools(*tools)
	if err != nil {
		return err
	}
	p := protocol.MCPServerAddParams{Name: *name, Title: *title, Description: *descr, Tier: *tier, Agent: *agent, Transport: triple, Tools: toolRows}
	if *disabled {
		off := false
		p.Enabled = &off
	}
	return mcpVerbWrite(w, protocol.MethodMCPServerAdd, "added server", p)
}

func cmdMCPUntag(w io.Writer, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("mcp update takes the server name first, then whole-field replacements")
	}
	fs := flag.NewFlagSet("mcp update", flag.ContinueOnError)
	var cmdArgs repeatedFlag
	fs.Var(&cmdArgs, "arg", "one command argument; repeat for each (replaces the argument list with --command)")
	var (
		tr      = fs.String("transport", "", "stdio or http (with --command/--env/--url: replaces the transport whole)")
		command = fs.String("command", "", "the stdio command")
		env     = fs.String("env", "", "comma-separated environment variable NAMES (names only)")
		envFrom = fs.String("env-from", "", "comma-separated NAME=VARIABLE: bind each env name to a stored variable")
		url     = fs.String("url", "", "the http URL")
		tools   = fs.String("tool", "", "comma-separated NAME:RERUN:EFFECT triples (replaces the tool set whole)")
		title   = fs.String("title", "", "the friendly name the app shows (an empty value clears it)")
		descr   = fs.String("description", "", "what the server is for (an empty value clears it)")
	)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp update takes no positional arguments after the name")
	}
	p := protocol.MCPServerUpdateParams{Name: args[0]}
	// A flag that was typed, even empty, is a change: only then does the field ride.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			p.Title = title
		case "description":
			p.Description = descr
		}
	})
	if *tr != "" || *command != "" || *env != "" || *url != "" {
		triple, err := mcpParseTransport(*tr, *command, *env, *url, cmdArgs, *envFrom)
		if err != nil {
			return err
		}
		p.Transport = &triple
	}
	if *tools != "" {
		toolRows, err := mcpParseTools(*tools)
		if err != nil {
			return err
		}
		p.Tools = &toolRows
	}
	return mcpVerbWrite(w, protocol.MethodMCPServerUpdate, "updated server", p)
}

func cmdMCPRemove(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp remove", flag.ContinueOnError)
	cascade := fs.Bool("cascade-declares", false, "also drop the declarations that name this server")
	yes := fs.Bool("yes", false, "confirm removing a server others declare")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		return fmt.Errorf("mcp remove takes exactly one server name")
	}
	name := fs.Arg(0)
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		// The seatbelt reads first: a removal that would drop other
		// agents' declarations is a widening SURPRISE, and the plan of it
		// (the holders, named) is what --yes should have seen.
		if !*cascade && !*yes {
			var one protocol.MCPServerResult
			if err := d.relay(ctx, protocol.MethodMCPServer, protocol.MCPServerParams{Name: name}, &one); err != nil {
				return err
			}
			if one.Server.DeclaredBy > 0 || len(one.DeclaredBy) > 0 {
				holders := one.DeclaredBy
				if len(holders) == 0 {
					holders = []string{fmt.Sprintf("(%d agents)", one.Server.DeclaredBy)}
				}
				fmt.Fprintf(w, "plan: remove %s — %d declaration(s) name it: %s; they would be refused unless --cascade-declares drops them\n",
					name, one.Server.DeclaredBy, strings.Join(holders, ", "))
				return fmt.Errorf("remove %s: re-run with --yes — removing a server others declare takes their tools away mid-loop, and the widening-surprise should be deliberate (a seatbelt, not a lock)", name)
			}
		}
		return mcpRunWrite(ctx, d, w, protocol.MethodMCPServerRemove, "removed server",
			protocol.MCPServerRemoveParams{Name: name, CascadeDeclares: *cascade})
	})
}

func cmdMCPToggle(w io.Writer, args []string, enabled bool) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("mcp enable/disable take exactly one server name")
	}
	word := "enabled"
	if !enabled {
		word = "disabled"
	}
	return mcpVerbWrite(w, protocol.MethodMCPServerSetEnabled, word+" server",
		protocol.MCPServerSetEnabledParams{Name: args[0], Enabled: enabled})
}

func cmdMCPShare(w io.Writer, args []string) error {
	if len(args) < 1 || len(args) > 2 || args[0] == "" {
		return fmt.Errorf("mcp share takes a server name and optionally yes or no")
	}
	shared := true
	if len(args) == 2 {
		switch args[1] {
		case "yes":
		case "no":
			shared = false
		default:
			return fmt.Errorf("mcp share: the second word is yes or no, not %q", args[1])
		}
	}
	word := "shared with every agent"
	if !shared {
		word = "no longer shared"
	}
	return mcpVerbWrite(w, protocol.MethodMCPServerShare, word,
		protocol.MCPServerShareParams{Name: args[0], Shared: shared})
}

func cmdMCPDeclare(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp declare", flag.ContinueOnError)
	agent := fs.String("agent", "", "the agent that declares (required)")
	tool := fs.String("tool", "", "SERVER.TOOL to declare (required)")
	yes := fs.Bool("yes", false, "confirm the widening")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp declare takes no positional arguments — every fact is a flag")
	}
	p := protocol.MCPGrantDeclareParams{Agent: *agent, Qualified: *tool}
	if !*yes {
		fmt.Fprintf(w, "plan: declare %s for %s — MC5: declaring grants ACCESS; usability still needs S10's allow (two facts, never merged)\n", *tool, *agent)
		return fmt.Errorf("declare %s for %s: re-run with --yes — a declare widens what an agent may reach, and the widening should be deliberate (a seatbelt, not a lock)", *tool, *agent)
	}
	return mcpVerbWrite(w, protocol.MethodMCPGrantDeclare, "declared", p)
}

func cmdMCPRevoke(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp revoke", flag.ContinueOnError)
	agent := fs.String("agent", "", "the agent whose declaration (required)")
	tool := fs.String("tool", "", "SERVER.TOOL to revoke (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp revoke takes no positional arguments — every fact is a flag")
	}
	return mcpVerbWrite(w, protocol.MethodMCPGrantRevoke, "revoked",
		protocol.MCPGrantRevokeParams{Agent: *agent, Qualified: *tool})
}

func cmdMCPCore(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp core", flag.ContinueOnError)
	agent := fs.String("agent", "", "the agent whose core set (required)")
	set := fs.String("set", "", "comma-separated core tool names ([] = none — a fact, not an omission)")
	all := fs.Bool("all", false, "the WHOLE core set (null — the other fact)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp core takes no positional arguments — every fact is a flag")
	}
	p := protocol.MCPCoreSetParams{Agent: *agent}
	// PRESENCE of --set is the fact, not its content: `--set=` with no
	// names is core narrowed to NOTHING ([]), the distinct neighbour of
	// --all (null = the whole set). flag.Visit is the only honest way to
	// see a set-but-empty flag.
	setGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "set" {
			setGiven = true
		}
	})
	switch {
	case *all && setGiven:
		return fmt.Errorf("mcp core: --all and --set name two different facts (the whole set vs this list) — choose one; a flag that merges them would guess")
	case *all:
		// Core stays nil → JSON null → "the whole core set".
	case setGiven:
		names := splitCSV(*set)
		p.Core = &names // [] serialises as [] — "core narrowed to nothing", the OTHER fact
	default:
		return fmt.Errorf("mcp core needs --set NAMES or --all — absence is not a third fact")
	}
	return mcpVerbWrite(w, protocol.MethodMCPCoreSet, "set core for", p)
}

func cmdMCPImportConfig(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp import-config", flag.ContinueOnError)
	path := fs.String("path", "", "the legacy file to import (default the XDG mcp.json; absolute)")
	dryRun := fs.Bool("dry-run", false, "answer the plan and write nothing")
	yes := fs.Bool("yes", false, "confirm the replace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("mcp import-config takes no positional arguments — every fact is a flag")
	}
	if *path != "" && !filepath.IsAbs(*path) {
		return fmt.Errorf("--path %q is relative — the door imports one NAMED file, never a world-relative guess (give an absolute path)", *path)
	}
	mode := "replace"
	if *dryRun {
		mode = "dry-run"
	}
	call := func(ctx context.Context, d mcpDoor, m string) (protocol.McpImportFileResult, error) {
		var res protocol.McpImportFileResult
		p := protocol.McpImportFileParams{Mode: m}
		if *path != "" {
			p.Path = path
		}
		err := d.relay(ctx, protocol.MethodMCPImportFile, p, &res)
		return res, err
	}
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		if !*yes {
			// The seatbelt plans first: the daemon's OWN dry-run answers
			// the diff (zero writes on this path — the plan comes from
			// the same function the replace runs), and the operator sees
			// exactly what --yes would do.
			res, err := call(ctx, d, "dry-run")
			if err != nil {
				return err
			}
			printMCPImport(w, res)
			return fmt.Errorf("import-config: re-run with --yes to replace the configuration from this file — adoption is once, all-or-nothing, and audited (a seatbelt, not a lock)")
		}
		res, err := call(ctx, d, mode)
		if err != nil {
			return err
		}
		printMCPImport(w, res)
		return nil
	})
}

// --- shared relay + input parsing -------------------------------------------------

// mcpWrite is the one shape every write verb takes: relay, print the
// before/after facts, print warnings the seam raised — and nothing else.
// The refusal (if any) already travelled verbatim from the validator.
func mcpVerbWrite(w io.Writer, method protocol.MethodName, verb string, params any) error {
	return mcpWithDoor(w, func(ctx context.Context, d mcpDoor) error {
		return mcpRunWrite(ctx, d, w, method, verb, params)
	})
}

func mcpRunWrite(ctx context.Context, d mcpDoor, w io.Writer, method protocol.MethodName, verb string, params any) error {
	var res protocol.MCPWriteResult
	if err := d.relay(ctx, method, params, &res); err != nil {
		return err
	}
	if !res.Changed {
		fmt.Fprintln(w, "nothing changed (the rows already said this — audited as no-ops are nothing)")
	} else {
		fmt.Fprintf(w, "%s\n", verb)
	}
	for _, g := range res.Grants {
		fmt.Fprintf(w, "  %s: %d → %d grant view rows\n", g.Agent, len(g.Before), len(g.After))
	}
	for _, r := range res.Revoked {
		fmt.Fprintf(w, "  revoked: %s\n", r)
	}
	for _, warn := range res.Warnings {
		fmt.Fprintf(w, "  warning: %s\n", warn)
	}
	return nil
}

// mcpParseTransport refuses what cannot be a value (an env NAME=value —
// C17's rule before a value exists) and passes the rest to the one
// validator verbatim.
func mcpParseTransport(tr, command, env, url string, args []string, envFrom ...string) (protocol.MCPTransportInput, error) {
	if tr != "" && tr != "stdio" && tr != "http" {
		// This one is not a validator check duplicated: a transport that
		// is neither word cannot BE a transport — it is a typo in the
		// one word the flag's meaning depends on.
		return protocol.MCPTransportInput{}, fmt.Errorf("--transport %q is neither stdio nor http — the two mounts that exist", tr)
	}
	out := protocol.MCPTransportInput{Type: tr, Command: command, URL: url, Args: args}
	if env != "" {
		for _, name := range splitCSV(env) {
			if strings.Contains(name, "=") {
				return protocol.MCPTransportInput{}, fmt.Errorf("--env %q carries a VALUE — env carries NAMES only; the values come from the daemon's environment (never a flag: argv is the process list and the shell history — C17)", name)
			}
			out.Env = append(out.Env, name)
		}
	}
	// --env-from NAME=VARIABLE binds a declared name to a stored variable. The
	// VARIABLE is a name in the variable manager (`aeon vault set` puts one there);
	// a VALUE never rides a flag — argv is the process list and the shell history.
	for _, spec := range envFrom {
		for _, pair := range splitCSV(spec) {
			name, variable, ok := strings.Cut(pair, "=")
			if !ok || name == "" || variable == "" || strings.Contains(variable, "=") {
				return protocol.MCPTransportInput{}, fmt.Errorf("--env-from %q is not NAME=VARIABLE — the env name the process sees, and the stored variable that supplies it", pair)
			}
			if out.EnvFrom == nil {
				out.EnvFrom = map[string]string{}
			}
			out.EnvFrom[name] = variable
		}
	}
	return out, nil
}

// mcpParseTools splits NAME:RERUN:EFFECT triples. Splitting is input
// parsing; classifying is THE validator's — a nonsense class travels to
// the refusing sentence verbatim (the CLI never checks a classification).
func mcpParseTools(s string) ([]protocol.MCPToolInput, error) {
	if s == "" {
		return nil, nil
	}
	var out []protocol.MCPToolInput
	for _, t := range splitCSV(s) {
		parts := strings.Split(t, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("--tool %q is not a NAME:RERUN:EFFECT triple — three fields, colon-separated (the classes' WORDS are the validator's to refuse, the SHAPE is the parser's)", t)
		}
		for _, p := range parts {
			if p == "" {
				return nil, fmt.Errorf("--tool %q has an empty field — a triple with a hole is not a declaration", t)
			}
		}
		out = append(out, protocol.MCPToolInput{Name: parts[0], Rerun: parts[1], Effect: parts[2]})
	}
	return out, nil
}

func splitCSV(s string) []string {
	// NON-nil even when empty: a *[]string built from a nil slice marshals
	// JSON null — which is the OTHER fact (the whole core set). Presence
	// of --set must survive to the door as [], never smuggled as null.
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- small printers ---------------------------------------------------------------

func printMCPImport(w io.Writer, r protocol.McpImportFileResult) {
	fmt.Fprintf(w, "import-config [%s] %s → %s\n", r.Mode, r.Path, r.Outcome)
	fmt.Fprintf(w, "  servers: %d   tools: %d   declares: %d\n", r.Servers, r.Tools, r.Declares)
	if r.Detail != "" {
		fmt.Fprintf(w, "  %s\n", r.Detail)
	}
	if r.Diff != nil {
		fmt.Fprintf(w, "  diff: servers +%d/-%d, tools +%d/-%d, declares +%d/-%d\n",
			len(r.Diff.ServersAdded), len(r.Diff.ServersRemoved),
			len(r.Diff.ToolsAdded), len(r.Diff.ToolsRemoved),
			len(r.Diff.DeclaresAdded), len(r.Diff.DeclaresRemoved))
	}
	for _, a := range r.Affected {
		fmt.Fprintf(w, "  affected: %s (%d → %d tools)\n", a.Agent, len(a.Before), len(a.After))
	}
}

func writePrettyJSON(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("the daemon answered with no body")
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Bytes that are not JSON are the daemon's fault stated as bytes —
		// relay them, do not reword them.
		fmt.Fprintln(w, string(raw))
		return nil
	}
	if err := enc.Encode(v); err != nil {
		return err
	}
	fmt.Fprint(w, buf.String())
	return nil
}

func transportTail(t protocol.MCPTransportSummary) string {
	var b strings.Builder
	if t.URL != "" {
		b.WriteString(" " + t.URL)
	}
	for _, a := range t.Args {
		b.WriteString(" " + a)
	}
	if len(t.EnvNames) > 0 {
		b.WriteString("   env: " + strings.Join(t.EnvNames, ","))
	}
	return b.String()
}

func mcpOrNone(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func stateWord(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func actorTail(c protocol.MCPChangeView) string {
	if c.ActorID == "" || c.ActorID == c.ActorKind {
		return ""
	}
	return "/" + c.ActorID
}
