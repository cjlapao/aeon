package main

// ════════════════════════════════════════════════════════════════════════════
// `aeon mcp` — the CLI seam for S12, and the ONLY importer of internal/mcp.
//
// # Why this file is the seam (T9 / MC11)
//
// The level-0 invariant says the whole layer is deletable: an installation
// with no MCP configuration behaves exactly as S11 closed — the core nine,
// no feature flag, no trace of the layer in the runtime. The greps in
// internal/mcp/absent_test.go pin that `cmd/aeon` is the only package that
// imports the layer at all: delete this file and the `case "mcp"` line in
// main.go and NOTHING ELSE in the product knows MCP exists. The runtime
// never consults the registry (step dispatch owns core tools; S11's gate
// owns their verdicts), so there is no runtime import to make conditional —
// the optionality is structural, not an `if enabled`.
//
// # What the CLI reads, and from where
//
//   - the CONFIG: the tables (S46 T3); the legacy file path is named only
//     by the seam's mcpLegacyPath, the importer (T4) and the doctor —
//     XDG user config. Absent is a legitimate state (MC11), rendered as the
//     DEMO's §1 words, not an empty line.
//   - the POLICIES: the same store the daemon decides with (the justified
//     second access path `aeon policy` already uses — WAL serialises behind
//     the daemon's writer). A listing that invented its own grant view would
//     be a second truth; GrantView renders the engine's answer.
//   - the SERVERS: dialed when --agent asks for its live view; a server that
//     does not answer is REPORTED ABSENT (T8/N8), never silently dropped.
//
// Credentials: never a flag (the standing rule — argv is the process list
// and the shell history). The config names variables; the client reads them
// from the environment at spawn (C17's neighbour, T2's least-privilege
// spawn). This file passes os.Getenv through and holds nothing itself.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

func cmdMCP(w io.Writer, args []string) error {
	if len(args) == 0 {
		return printMCPUsage(w)
	}
	switch args[0] {
	case "list":
		return cmdMCPList(w, args[1:])
	case "call":
		return cmdMCPCall(w, args[1:])
	case "serve":
		return cmdMCPServe(w, args[1:])
	// S46 T9: the management verbs, each a thin client of its socket
	// method over the two doors (mcp_client.go). The parity guard
	// (mcp_parity_test.go) pins verb ↔ method both ways.
	case "show":
		return cmdMCPShow(w, args[1:])
	case "grants":
		return cmdMCPGrantsVerb(w, args[1:])
	case "add":
		return cmdMCPAdd(w, args[1:])
	case "update":
		return cmdMCPUntag(w, args[1:])
	case "remove":
		return cmdMCPRemove(w, args[1:])
	case "enable":
		return cmdMCPToggle(w, args[1:], true)
	case "disable":
		return cmdMCPToggle(w, args[1:], false)
	case "share":
		return cmdMCPShare(w, args[1:])
	case "declare":
		return cmdMCPDeclare(w, args[1:])
	case "revoke":
		return cmdMCPRevoke(w, args[1:])
	case "core":
		return cmdMCPCore(w, args[1:])
	case "import-config":
		return cmdMCPImportConfig(w, args[1:])
	case "changes":
		return cmdMCPChanges(w, args[1:])
	case "status":
		return cmdMCPStatusWord(w, args[1:])
	case "check":
		return cmdMCPCheckWord(w, args[1:])
	case "inspect":
		return cmdMCPInspect(w, args[1:])
	case "processes":
		return cmdMCPProcesses(w, args[1:])
	case "logs":
		return cmdMCPLogs(w, args[1:])
	case "start":
		return cmdMCPStart(w, args[1:])
	case "stop":
		return cmdMCPStop(w, args[1:])
	case "restart":
		return cmdMCPRestart(w, args[1:])
	case "help", "-h", "--help":
		return printMCPUsage(w)
	default:
		return fmt.Errorf("unknown mcp verb %q — there is list, call, serve, and the S46 management set (aeon mcp help)", args[0])
	}
}

// cmdMCPServe mounts the narrow surface (S19) as an MCP server on stdio:
// `claude mcp add aeon -- aeon mcp serve`, and no port involved. The
// mounting reads nothing but the pipe and speaks nothing but JSON-RPC on
// stdout — diagnostics therefore go to stderr, because one stray banner
// byte on stdout is a parse error on the editor's side.
//
// The daemon is reached as a CLIENT, over the socket, exactly like every
// other verb (dial, initialize mandatory and first). That is the whole
// point of the stdio half existing before HTTP: everything about the
// surface is provable here without auth or a listener — the pipe's parent
// owns the transport, and the socket behind it is 0600 with peer
// credentials (P26).
func cmdMCPServe(w io.Writer, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("mcp serve takes no arguments")
	}
	client, err := dial(0)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	fmt.Fprintln(os.Stderr, "aeon mcp serve: serving the narrow surface on stdio (Ctrl-D to close)")
	return narrow.NewServer(client, narrow.Actions()).Serve(context.Background(), os.Stdin, os.Stdout)
}

func printMCPUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, `usage:
  aeon mcp list [--agent NAME] [--include-refused]
  aeon mcp call --agent NAME --tool SERVER.TOOL [--args JSON]
  aeon mcp serve
  aeon mcp show NAME | grants AGENT | changes [--server N] [--agent A] [--limit N]
  aeon mcp add --name N --tier public|private [--agent SLUG] --transport stdio|http
       [--command C] [--env NAME,NAME] [--url U] [--tool NAME:RERUN:EFFECT,...] [--disabled]
  aeon mcp update NAME [--transport … --command … --env … --url … --tool …]
  aeon mcp remove NAME [--cascade-declares] [--yes]
  aeon mcp enable|disable NAME
  aeon mcp share NAME [yes|no]   every agent declares the server's tools (a declaration; policy still decides use)
  aeon mcp declare --agent SLUG --tool SERVER.TOOL [--yes]
  aeon mcp revoke --agent SLUG --tool SERVER.TOOL
  aeon mcp core --agent SLUG [--set TOOL,TOOL | --all]
  aeon mcp import-config [--path ABSOLUTE] [--dry-run] [--yes]
  aeon mcp status | check [NAME]
  aeon mcp processes | logs NAME [-n N] | start|stop|restart NAME
  aeon mcp inspect --transport stdio --command C [--arg A ...] [--env NAME,NAME] | --transport http --url U

management verbs are CLIENTS of the daemon's socket methods; with no daemon
running they apply directly to the store and say so on stderr. --env takes
NAMES only — values come from the daemon's environment, never a flag (argv
is the process list and the shell history). --yes gates the widening
verbs (declare, import-config replace, remove of a server others declare):
a seatbelt for a typo, not a security control.

list reads the config at $AEON_MCP_CONFIG (or the XDG default), the policy
store, and — for --agent — dials that agent's configured servers to report
live classes and connection state. Absent configuration is a legitimate
answer, not an error. call spawns (stdio) or reaches (http) exactly the one
configured server named by the tool and makes exactly one tools/call.
serve mounts the NARROW SURFACE (S19: one enumerated table, five reads and
the one-way start) as an MCP server on this process's stdin/stdout, talking
to the daemon over the socket like any other verb — the editor-facing half,
no port, no auth, the boundary statement carried to the client.
`)
	return err
}

// mcpAbsentLines are the DEMO §1 words, verbatim. The absence must be
// STATED — "an empty line is a silent layer" (the demo's own failure
// criterion), and a script that greps for these words is testing the
// invariant, so they are pinned here and by the walk.
var mcpAbsentLines = []string{
	"no MCP servers configured.",
	"tools available: the core nine only (S11)",
}

func cmdMCPList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp list", flag.ContinueOnError)
	agent := fs.String("agent", "", "render this agent's view")
	includeRefused := fs.Bool("include-refused", false, "also show refused tools and why")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadMCPConfig()
	if errors.Is(err, mcp.ErrNoConfig) {
		for _, line := range mcpAbsentLines {
			fmt.Fprintln(w, line)
		}
		return nil
	}
	if err != nil {
		// Rows that the one validator refuses are NOT absent configuration:
		// a store fault is reported as one, never smoothed into the empty
		// state a script might mistake for "nothing configured" (P31).
		return err
	}

	reg, err := mcp.New(cfg)
	if err != nil {
		return err
	}

	// MG4 at this door: a disabled server is not listed and never dialled —
	// the declarations stand (they are in the candidate above, which is
	// what made mcp.New legal), the server simply stays off.
	off, err := loadMCPDisabled()
	if err != nil {
		return err
	}
	on := func(name string) bool { return !off[name] }

	ps, closeStore, err := loadToolAccessPolicies()
	if err != nil {
		return err
	}
	defer closeStore()
	at := time.Now() // once, like a step's frozen instant

	// Which servers this view needs.
	type target struct {
		srv  mcp.ServerConfig
		tier string
	}
	var targets []target
	if *agent != "" {
		ac := cfg.Agents[*agent]
		for _, s := range ac.Private {
			if on(s.Name) {
				targets = append(targets, target{s, "private"})
			}
		}
		// Public servers matter only if the agent declared from them.
		declared := map[string]bool{}
		for _, d := range ac.Declares {
			if i := strings.Index(d, "."); i > 0 {
				declared[d[:i]] = true
			}
		}
		for _, s := range cfg.Public {
			if declared[s.Name] && on(s.Name) {
				targets = append(targets, target{s, "public"})
			}
		}
	} else {
		for _, s := range cfg.Public {
			if on(s.Name) {
				targets = append(targets, target{s, "public"})
			}
		}
		for _, ac := range cfg.Agents {
			for _, s := range ac.Private {
				if on(s.Name) {
					targets = append(targets, target{s, "private"})
				}
			}
		}
	}

	// Rows have no file order, and cfg.Agents is a MAP — listing private
	// servers straight out of it would flip order between runs the day
	// two agents own servers (the coin mcpStatus's declaredBy spun until
	// S46 T3). Sort WITHIN each append-group: the groups are the view's
	// shape (plain lists public then private; --agent lists the agent's
	// private first, then what it declared from), names ascend inside
	// each group, and the pinned bytes never saw another order.
	sortStart := 0
	for i := 1; i <= len(targets); i++ {
		if i == len(targets) || targets[i].tier != targets[i-1].tier {
			group := targets[sortStart:i]
			sort.SliceStable(group, func(a, b int) bool {
				return group[a].srv.Name < group[b].srv.Name
			})
			sortStart = i
		}
	}

	// Dial once per server, report absence honestly for the rest (T8: the
	// degradation is per-server and the absence is reported).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	down := map[string]string{}
	for _, tg := range targets {
		c, err := mcp.Dial(ctx, tg.srv, os.Getenv)
		if err != nil {
			down[tg.srv.Name] = err.Error()
			continue
		}
		remote, err := c.ListTools(ctx)
		if err != nil {
			down[tg.srv.Name] = err.Error()
			_ = c.Close()
			continue
		}
		if err := reg.ApplyListings(tg.srv.Name, remote); err != nil {
			_ = c.Close()
			return err
		}
		if err := reg.AttachClient(tg.srv.Name, c); err != nil {
			_ = c.Close()
			return err
		}
		defer func() { _ = c.Close() }()
	}

	if *agent != "" {
		fmt.Fprintf(w, "%s\n", *agent)
		fmt.Fprintln(w, "  core:")
		for _, tl := range reg.ToolsFor(*agent) {
			if tl.Tier != mcp.TierCore {
				continue
			}
			fmt.Fprintf(w, "    %-24s\n", tl.Name)
		}
		fmt.Fprintln(w, "  (the core set — S11 owns their verdicts; MCP adds nothing here until configured)")
	}

	// Classes come from the capability view (the raised, effective ones —
	// what the model is shown); states from the authority view. One map
	// each, never a third derivation.
	capTools := map[string]mcp.Tool{}
	for _, tl := range reg.ToolsFor(*agent) {
		capTools[tl.Qualified] = tl
	}
	views := reg.GrantView(*agent, ps, at)
	byServer := map[string][]mcp.GrantView{}
	for _, v := range views {
		if strings.HasPrefix(v.Qualified, "core:") || !strings.Contains(v.Qualified, ".") {
			continue // core rendered above
		}
		server := v.Qualified[:strings.Index(v.Qualified, ".")]
		byServer[server] = append(byServer[server], v)
	}
	servers := make([]string, 0, len(byServer))
	for s := range byServer {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	for _, s := range servers {
		tier := "public"
		if *agent != "" {
			for _, p := range cfg.Agents[*agent].Private {
				if p.Name == s {
					tier = "private"
				}
			}
		}
		lines := make([]string, 0, len(byServer[s]))
		for _, v := range byServer[s] {
			if v.State == mcp.GrantRefused {
				if !*includeRefused {
					continue // §2: refused tools are not listed unless asked
				}
				lines = append(lines, fmt.Sprintf("    %-24s — %s", v.Qualified, v.Reason))
				continue
			}
			tl := capTools[v.Qualified]
			tail := ""
			if _, bad := down[s]; bad {
				// A class over a dead wire is a fact about the config, not
				// an offer: say so on the line the operator is reading, not
				// only in the section footer somebody will scroll past.
				tail = "  · server did not answer (reported absent)"
			}
			class := fmt.Sprintf("    %-24s %-12s · %s%s", v.Qualified, effectWord(tl.Effect), rerunWord(tl.Rerun), tail)
			if v.State == mcp.GrantAsk {
				class += "  · asks at use (L22)"
			}
			if v.State == mcp.GrantUnavailable && *includeRefused {
				class = fmt.Sprintf("    %-24s — %s", v.Qualified, v.Reason)
			}
			lines = append(lines, class)
		}
		if len(lines) == 0 {
			// A header over an empty section is the lie of a suppressed
			// refusal: say what happened instead (the tools exist — every
			// one is refused, and the flag that shows why is named).
			fmt.Fprintf(w, "  %s  (%s) — no tools shown; every one refused or filtered (try --include-refused)\n", s, tier)
			continue
		}
		fmt.Fprintf(w, "  %s  (%s)\n", s, tier)
		sort.Strings(lines)
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}

	// The unclassified view refused tools: configured, reported, no classes
	// (MC6) — refused is a fact with a reason, shown on request.
	if *includeRefused {
		for _, tg := range targets {
			for _, tc := range tg.srv.Tools {
				q := tg.srv.Name + "." + tc.Name
				st, missing, _ := reg.Availability(*agent, q)
				if st == mcp.AvailabilityUnavailable && len(missing) > 0 {
					fmt.Fprintf(w, "    %-24s — %s\n", q, mcp.UnavailableReason(q, missing))
				}
			}
		}
	}

	for _, tg := range targets {
		if why, bad := down[tg.srv.Name]; bad {
			fmt.Fprintf(w, "  MCP server %q did not answer — its tools are reported absent, not silently dropped: %s\n", tg.srv.Name, why)
		}
	}
	return nil
}

// effectWord/rerunWord render classes the way the DEMO prints them: the
// DEMO's `read-only · reversible` for a local+safe tool. The stored enums
// stay the enums; only the presentation maps — and an unknown enum reaches
// the terminal as its own value, never a guessed word (three-state honesty).
func effectWord(e mcp.EffectClass) string {
	switch e {
	case mcp.EffectClassLocal:
		return "read-only"
	case mcp.EffectClassOutward:
		return "outward"
	case mcp.EffectClassIrreversible:
		return "irreversible"
	}
	return string(e)
}

func rerunWord(r tools.RerunClass) string {
	switch r {
	case tools.RerunSafe:
		return "reversible"
	case tools.RerunDetectable:
		return "detectable"
	case tools.RerunUnsafe:
		return "unsafe"
	}
	return string(r)
}

// loadToolAccessPolicies reads the SAME store the daemon decides with —
// the justified second access path `aeon policy` uses (WAL serialises
// behind the daemon's writer). Filtering to ToolAccess keeps the view's
// work honest: other types answer other questions.
func loadToolAccessPolicies() ([]policy.Policy, func(), error) {
	boundary, closeFn, err := openPolicyBoundaryDirectly()
	if err != nil {
		return nil, nil, err
	}
	ps, rej := boundary.List(context.Background(), time.Now())
	if rej != nil {
		closeFn()
		return nil, nil, fmt.Errorf("%s", rej.Reason)
	}
	out := make([]policy.Policy, 0, len(ps))
	for _, p := range ps {
		if p.Type == policy.ToolAccess {
			out = append(out, p)
		}
	}
	return out, closeFn, nil
}

func cmdMCPCall(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("mcp call", flag.ContinueOnError)
	agent := fs.String("agent", "", "which agent is calling (required)")
	tool := fs.String("tool", "", "qualified SERVER.TOOL (required)")
	argsJSON := fs.String("args", "{}", "the tool's arguments, as one JSON object")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *agent == "" || *tool == "" {
		return fmt.Errorf("--agent and --tool are both required — a call without a caller or a name is not a call")
	}
	var probe map[string]any
	if strings.TrimSpace(*argsJSON) != "" && strings.TrimSpace(*argsJSON) != "{}" {
		if err := json.Unmarshal([]byte(*argsJSON), &probe); err != nil {
			return fmt.Errorf("--args must be one JSON object: %v", err)
		}
		if probe == nil {
			return fmt.Errorf("--args must be one JSON object, not null")
		}
	}

	cfg, err := loadMCPConfig()
	if errors.Is(err, mcp.ErrNoConfig) {
		// Absence is told, not silent — and the call answer follows from it:
		// with no configuration, no tool exists for anyone.
		for _, line := range mcpAbsentLines {
			fmt.Fprintln(w, line)
		}
		fmt.Fprintf(w, "unknown tool: %s\n", *tool)
		return nil
	}
	if err != nil {
		return err
	}
	reg, err := mcp.New(cfg)
	if err != nil {
		return err
	}

	ps, closeStore, err := loadToolAccessPolicies()
	if err != nil {
		return err
	}
	defer closeStore()
	at := time.Now() // the call's one instant (S10's clock discipline)

	srvName := *tool
	if i := strings.Index(*tool, "."); i > 0 {
		srvName = (*tool)[:i]
	}
	srv, ok := configuredServer(cfg, *agent, srvName)
	if !ok {
		// The refusal an unconfigured server deserves: the same unknown a
		// stranger gets (T5's discipline reaches the CLI).
		fmt.Fprintf(w, "unknown tool: %s\n", *tool)
		return nil
	}
	// MG4 at the call door too: an off server answers as off — told, not
	// disguised as unknown (the server IS configured; it is switched off).
	off, err := loadMCPDisabled()
	if err != nil {
		return err
	}
	if off[srvName] {
		fmt.Fprintf(w, "disabled: server %q is off (MG4) — its tools are not offered, and a call would invent an answer\n", srvName)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mcp.Dial(ctx, srv, os.Getenv)
	if err != nil {
		// The call's answer to a server that is not there is the SAME fact
		// the registry answers and the list prints — reported absence on
		// stdout. This CLI is a mouthpiece for the layer's three states,
		// not a process that dies when someone else's server does (MC9).
		fmt.Fprintf(w, "unavailable: MCP server %q is not connected — its tools are reported absent, not silently dropped: %v\n", srvName, err)
		return nil
	}
	defer func() { _ = c.Close() }()
	remote, err := c.ListTools(ctx)
	if err != nil {
		fmt.Fprintf(w, "unavailable: MCP server %q did not answer tools/list — its tools are reported absent, not silently dropped: %v\n", srvName, err)
		return nil
	}
	if err := reg.ApplyListings(srvName, remote); err != nil {
		return err
	}
	if err := reg.AttachClient(srvName, c); err != nil {
		return err
	}

	res, err := reg.Call(ctx, *agent, *tool, json.RawMessage(*argsJSON), ps, at)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, res.Content)
	if res.AskRequired {
		fmt.Fprintln(w, "(consent is owed before this call may run — the server was not contacted)")
	}
	return nil
}

func configuredServer(cfg mcp.Config, agent, server string) (mcp.ServerConfig, bool) {
	for _, s := range cfg.Agents[agent].Private {
		if s.Name == server {
			return s, true
		}
	}
	for _, s := range cfg.Public {
		if s.Name == server {
			return s, true
		}
	}
	return mcp.ServerConfig{}, false
}
