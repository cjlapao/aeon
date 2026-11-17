// agent.go — `aeon agent`: the five verbs that move plugin state, and the
// ONE reader the socket is allowed to have.
//
// This file is where AP10 and R6 are enforced, and both refusals are the
// loud kind:
//
//   - NOTHING FETCHES. `import` takes a path. There is no HTTP client here,
//     no marketplace, no auto-update, no dependency resolution — each is a
//     path by which code arrives without anybody deciding, which is what
//     MC3 refused for MCP servers for the same reason. A "URL" is refused
//     by the library before any disk is touched, and this package owns no
//     client to fetch with even by accident (a test greps it).
//   - IMPORT WIDENS AUTHORITY, AND ONLY IMPORT. `import` exists HERE and in
//     the app, never on a socket method: it hands a caller the means to
//     install the thing that bypasses the guards (R6 — S10 said it for
//     policy authoring, S12 for MCP servers, and the reasoning has not
//     aged a day for import specifically).
//
// R6 narrowed, not dropped (S35 T4, brief §3's ruling): enable/disable/
// revoke moved OFF this line. The imported agent already passed blocklist
// and consent AT IMPORT (S15); moving it between states starts something
// already permitted — like loop.create starting a loop from an approved
// blueprint — never widens what the daemon may do. They answer
// agent.enable/agent.disable/agent.revoke on the socket (S35 T4's seam,
// cmd/aeon/agent_transition_services.go), calling the SAME functions this
// file's own verbs call (plugin.Enable/Disable/Revoke via moveVerb/
// cmdAgentRevoke's bodies), never a thinner path. The single read,
// `agent.plugins`, is answered by this package via Table.Plugins.
//
// The CLI opens the store DIRECTLY, the same justified two-access path the
// policy verbs use (documented beside store_verbs.go). The daemon needs no
// notification: every gate reads the DURABLE record first (T5), so a row
// written here IS the daemon's next decision — N8 forbids caching anyway.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

func cmdAgent(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("refused: `aeon agent` needs a verb — import <path> · list [--all] [--provenance] · enable <name> · disable <name> · revoke <name> --reason <why>")
	}
	switch args[0] {
	case "import":
		return cmdAgentImport(w, args[1:])
	case "list":
		return cmdAgentList(w, args[1:])
	case "enable":
		return cmdAgentMove(w, args[1:], plugin.StateEnabled)
	case "disable":
		return cmdAgentMove(w, args[1:], plugin.StateDisabled)
	case "revoke":
		return cmdAgentRevoke(w, args[1:])
	default:
		return fmt.Errorf("refused: unknown verb %q — the verbs are import, list, enable, disable, revoke (there is no update, no fetch, no marketplace: nothing here downloads anything, ever)", args[0])
	}
}

// agentCtx bundles the three opens a verb needs: the store (rows), the
// policy boundary (the set the check judges against) and the plugin seams
// (ledger + card verifier) wired exactly as the daemon wires them — ONE
// implementation each, and a CLI that judged differently from the daemon
// would be a second truth.
type agentCtx struct {
	s        *store.Store
	boundary *policy.Store
	close    func()
	at       time.Time
}

func openAgentCtx() (*agentCtx, error) {
	s, err := openStoreDirectly(false)
	if err != nil {
		return nil, err
	}
	closeAll := func() { _ = s.Close() }
	if err := wirePluginSeams(s); err != nil {
		closeAll()
		return nil, err
	}
	return &agentCtx{s: s, boundary: policy.NewStore(policyRepo(s)), close: closeAll, at: time.Now().UTC()}, nil
}

// set reads the policy set in force NOW — the same read the daemon's door
// makes, from the same rows.
func (c *agentCtx) set() ([]policy.Policy, error) {
	rows, rj := c.boundary.List(context.Background(), c.at)
	if rj != nil {
		return nil, fmt.Errorf("the policy set could not be read (%s) — the check judges against the set in force, and inventing one would judge against nothing", rj.Reason)
	}
	return rows, nil
}

// --- import ---------------------------------------------------------------------

func cmdAgentImport(w io.Writer, args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("refused: import takes exactly ONE argument — the local path of the package directory (nothing is fetched; a URL is refused before any disk is touched, AP10)")
	}
	return importAgentPackage(w, args[0])
}

// importAgentPackage is the ONE import body: open the ledger/policy seams, read
// the policy set, plugin.ImportChecked, print the consent VERBATIM, store each
// carried Deny through the policy boundary, report a revoked past life.
// `aeon agent import` and `aeon proposal commit` (kind agent_import) both end
// here — a second implementation of import is exactly what S37's AA5 forbids
// (AD4: cmdAgentImport's body, called from commit).
func importAgentPackage(w io.Writer, pkgPath string) error {
	c, err := openAgentCtx()
	if err != nil {
		return err
	}
	defer c.close()
	existing, err := c.set()
	if err != nil {
		return err
	}
	a, rj := plugin.ImportChecked(pkgPath, existing, c.at)
	if rj != nil {
		return fmt.Errorf("%s", rj.Error())
	}
	// The consent, printed VERBATIM — this is the text an operator decides
	// against, and a CLI that prettified it would be editing the thing the
	// decision rests on (T4's own words, not a summary of them).
	fmt.Fprint(w, plugin.RenderConsent(a, existing).String()+"\n")
	// The carried denials become REAL here — this surface is the only door
	// policy rows have (S10's authoring rule), so persisting them is the
	// CLI's job, through the ONE boundary that enforces PL1. A refusal to
	// store one is printed, not swallowed: an unstored denial is a hole
	// somebody must see.
	for _, p := range a.Carried {
		StoreCarriedPluginDenial(w, c.boundary, p, c.at)
	}
	// AP4's visibility half: a REVOKED life that comes back must be VISIBLE
	// in the consent, not inferred from a timestamp. "Somebody put this
	// back" is a fact the operator deserves at the moment of decision —
	// found by the T9 walk, which caught the DEMO promising this line while
	// the code never printed it.
	if ts, err := c.s.PluginAgentTransitions(a.Name); err == nil {
		for i := len(ts) - 1; i >= 0; i-- {
			if ts[i].ToState == "revoked" {
				reason := ts[i].Reason
				if reason == "" {
					reason = "none was given — which is itself the finding (a revocation should always say why)"
				}
				fmt.Fprintf(w, "NOTE  a previous import of %s was REVOKED at %s — reason: %s\n", a.Name, ts[i].At, reason)
				fmt.Fprintf(w, "      this is a NEW record, not a resurrection: somebody put this back, so the consent says it out loud (AP4)\n")
				break
			}
		}
	}
	fmt.Fprintf(w, "imported %s as DISABLED (AP2: import runs the check and stops — arrival is not permission)\n", a.Name)
	fmt.Fprintf(w, "read the consent above; when decided: aeon agent enable %s\n", a.Name)
	return nil
}

// --- list -----------------------------------------------------------------------

func cmdAgentList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(w)
	all := fs.Bool("all", false, "include revoked agents (their lives stay readable — AP4)")
	prov := fs.Bool("provenance", false, "show where each came from, including a package's refused first-party claim")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	c, err := openAgentCtx()
	if err != nil {
		return err
	}
	defer c.close()
	existing, err := c.set()
	if err != nil {
		return err
	}
	rows, err := c.s.PluginAgentsAll(*all)
	if err != nil {
		return fmt.Errorf("read the plugin ledger: %w", err)
	}
	carriedCount := func(name string) int {
		n := 0
		for _, p := range existing {
			if string(p.Source) == string(policy.Import) && string(p.Target.Kind) == "agent" && p.Target.ID == name {
				n++
			}
		}
		return n
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	if len(rows) == 0 {
		// AP11: an empty registry is the NORMAL state, and the listing says
		// so without alarm — nobody must ever feel pressure to import.
		fmt.Fprintf(w, "no plugin agents imported — this is the normal state: the first-party agents run, nothing was fetched, nothing is missing\n")
	} else {
		fmt.Fprintf(w, "%-24s %-10s %-9s %s\n", "NAME", "STATE", "CARRIED", "SOURCE")
		for _, r := range rows {
			line := fmt.Sprintf("%-24s %-10s %-9d %s", r.Name, r.State, carriedCount(r.Name), r.Source)
			// AP4 in the listing: `--all` answers "what did we once run, and
			// why did we stop" from the rows alone — the reason rides with
			// the revoked row (the T9 walk found it promised and missing).
			if r.State == "revoked" {
				if ts, err := c.s.PluginAgentTransitions(r.Name); err == nil {
					for i := len(ts) - 1; i >= 0; i-- {
						if ts[i].ToState == "revoked" {
							line += " · reason: " + ts[i].Reason
							break
						}
					}
				}
			}
			fmt.Fprintln(w, line)
			if *prov {
				line := r.Name + " · imported from " + r.Source + " at " + r.ImportedAt + " · first-party: no (registry-set)"
				if claimed, rj := plugin.PackageClaimsFirstParty(r.Source); rj == nil && claimed {
					line += " · PACKAGE CLAIMED FIRST-PARTY — IGNORED (AP9)"
				}
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}
	fp := 0
	for _, a := range plugin.Agents() {
		if a.FirstParty {
			fp++
		}
	}
	fmt.Fprintf(w, "first-party: %d compiled bodies (they have no plugin state — the four verbs do not apply to them)\n", fp)
	return nil
}

// --- enable / disable / revoke ----------------------------------------------------

func cmdAgentMove(w io.Writer, args []string, to plugin.State) error {
	fs := flag.NewFlagSet("agent "+string(to), flag.ContinueOnError)
	fs.SetOutput(w)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("refused: %s takes exactly ONE name", to)
	}
	name := fs.Arg(0)
	c, err := openAgentCtx()
	if err != nil {
		return err
	}
	defer c.close()
	if to == plugin.StateEnabled {
		// Enable runs on a package THIS process has read (an unread prompt
		// must never run); the row names the directory, Rehydrate reads it,
		// and the check runs AGAIN on the re-read — a package that grew a
		// request after install is caught before a verb trusts it.
		rec, found, err := c.s.PluginAgentState(name)
		if err != nil {
			return fmt.Errorf("read the record for %s: %w", name, err)
		}
		if !found {
			return fmt.Errorf("refused: %s has no record — is it one of the first-party 25 (they have no plugin state), or simply never imported? (aeon agent list)", name)
		}
		existing, err := c.set()
		if err != nil {
			return err
		}
		if _, rj := plugin.Rehydrate(rec.Source, existing, c.at); rj != nil {
			return fmt.Errorf("%s", rj.Error())
		}
	}
	if _, rj := moveVerb(c, name, to); rj != nil {
		return fmt.Errorf("%s", rj.Error())
	}
	rec, found, err := c.s.PluginAgentState(name)
	if err != nil {
		return fmt.Errorf("read %s's state back: %w", name, err)
	}
	after := "unknown"
	if found {
		after = rec.State
	}
	fmt.Fprintf(w, "%s is now %s\n", name, after)
	return nil
}

func cmdAgentRevoke(w io.Writer, args []string) error {
	// --reason is read BEFORE positional parsing: the name arrives first
	// (`aeon agent revoke groot --reason …`, the shape people type), and
	// Go's flag package stops at the first non-flag word — a parse that
	// silently lost --reason would silently lose the reason column.
	var reason string
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--reason" || a == "-reason":
			if i+1 >= len(args) {
				return fmt.Errorf("refused: --reason takes a value — the why, not the flag alone")
			}
			i++
			reason = args[i]
		case strings.HasPrefix(a, "--reason="):
			reason = strings.TrimPrefix(a, "--reason=")
		case strings.HasPrefix(a, "-reason="):
			reason = strings.TrimPrefix(a, "-reason=")
		case a == "--help" || a == "-h":
			fmt.Fprintln(w, "usage: aeon agent revoke <name> --reason <why>   (the reason is REQUIRED — it survives, AP4)")
			return nil
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) != 1 {
		return fmt.Errorf("refused: revoke takes exactly ONE name (got %d)", len(positional))
	}
	if strings.TrimSpace(reason) == "" {
		// The CLI fails fast with the same demand the store enforces — and
		// prints it with the point: "never again" without a why is a
		// deletion with extra steps.
		return fmt.Errorf("refused: revoke without --reason — the reason is REQUIRED and survives (AP4): who will read it? the operator who wonders next month why groot-translator stopped. Name the vendor, the audit, the incident")
	}
	name := positional[0]
	c, err := openAgentCtx()
	if err != nil {
		return err
	}
	defer c.close()
	// The daemon needs no notification: every gate reads the DURABLE row
	// first, so this write IS the daemon's next decision (N8 forbids
	// caching — there is nothing to invalidate).
	if _, rj := plugin.Revoke(name, strings.TrimSpace(reason)); rj != nil {
		return fmt.Errorf("%s", rj.Error())
	}
	// Stopping needs no package: the durable row alone carries the whole
	// truth a stop needs (the vendor's directory may be exactly what is
	// gone). The transitions table is the life; read it back.
	rows, err := c.s.PluginAgentTransitions(name)
	if err != nil {
		return fmt.Errorf("read %s's transitions: %w", name, err)
	}
	if n := len(rows); n > 0 {
		fmt.Fprintf(w, "%s is now revoked — terminal (a re-import creates a NEW record). Last transition: %s → %s · %s\n",
			name, orNone(rows[n-1].FromState), rows[n-1].ToState, rows[n-1].Reason)
		return nil
	}
	fmt.Fprintf(w, "%s is now revoked — terminal (a re-import creates a NEW record)\n", name)
	return nil
}

func moveVerb(c *agentCtx, name string, to plugin.State) (*plugin.Agent, *plugin.Reject) {
	switch to {
	case plugin.StateEnabled:
		return plugin.Enable(name)
	case plugin.StateDisabled:
		return plugin.Disable(name)
	}
	return nil, &plugin.Reject{Field: "state", Detail: "revoke goes through its own verb — the reason is mandatory there, and revoke does not route through a flag that could silently lose it"}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// --- the ONE socket read ----------------------------------------------------------

// pluginsWire is the wire shape of `agent.plugins` — the listing's fields,
// and NOTHING that could be mistaken for a verb: there is no way to ask
// this reader to change a state, because the reader cannot change anything
// (R6's line, drawn in the JSON as well as the table).
type pluginsWire struct {
	Name        string   `json:"name"`
	State       string   `json:"state"`
	Source      string   `json:"source"`
	ImportedAt  string   `json:"importedAt"`
	Carried     int      `json:"carriedDenials"`
	FirstParty  bool     `json:"firstParty"`
	Transitions []string `json:"transitions,omitempty"`
}

type pluginsResult struct {
	Plugins         []pluginsWire `json:"plugins"`
	FirstPartyCount int           `json:"firstPartyCount"`
	Counts          struct {
		Disabled int `json:"disabled"`
		Enabled  int `json:"enabled"`
		Revoked  int `json:"revoked"`
	} `json:"counts"`
	Note string `json:"note"`
}

// pluginsHandleFor answers `agent.plugins` from the rows. Params must be
// empty (every handler validates its own — SEC-T6-005): there is nothing
// for a reader to ask for beyond the listing, and no filter is an excuse
// for a caller to probe for agents it was not shown.
func pluginsHandleFor(st *store.Store) func(ctx context.Context, params json.RawMessage) (any, string, error) {
	return func(ctx context.Context, params json.RawMessage) (any, string, error) {
		if len(params) > 0 {
			var probe map[string]any
			if err := json.Unmarshal(params, &probe); err != nil {
				return nil, fmt.Sprintf("refused: agent.plugins takes no params — it answers the whole listing or nothing (%v)", err), nil
			}
			if len(probe) > 0 {
				return nil, "refused: agent.plugins takes no params — a filter would let a reader probe for agents it was not shown; the listing is the surface (R6: this read can neither install nor move anything)", nil
			}
		}
		if err := wirePluginSeams(st); err != nil {
			return nil, "", err
		}
		existing, rj := policy.NewStore(policyRepo(st)).List(ctx, time.Now().UTC())
		if rj != nil {
			return nil, fmt.Sprintf("the policy set could not be read (%s) — carried counts would be invented", rj.Reason), nil
		}
		rows, err := st.PluginAgentsAll(true)
		if err != nil {
			return nil, "", fmt.Errorf("read the plugin ledger: %w", err)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		out := pluginsResult{Plugins: []pluginsWire{}}
		for _, r := range rows {
			w := pluginsWire{Name: r.Name, State: r.State, Source: r.Source, ImportedAt: r.ImportedAt}
			for _, p := range existing {
				if string(p.Source) == string(policy.Import) && string(p.Target.Kind) == "agent" && p.Target.ID == r.Name {
					w.Carried++
				}
			}
			switch r.State {
			case "disabled":
				out.Counts.Disabled++
			case "enabled":
				out.Counts.Enabled++
			case "revoked":
				out.Counts.Revoked++
			}
			out.Plugins = append(out.Plugins, w)
		}
		for _, a := range plugin.Agents() {
			if a.FirstParty {
				out.FirstPartyCount++
			}
		}
		out.Note = "zero imports is the normal state (nothing was fetched — nothing here can fetch); revoked records stay readable because a life that ended is a fact, not an embarrassment (AP4)"
		return out, "", nil
	}
}

// agentRosterEntry/agentListResult are `agent.list`'s wire shape (S34 T1).
// State is the REAL vocabulary — verified this session, not design.md's
// pre-code guess:
//
//   - first-party bodies: "running" — plugin/import.go:296's Agent{State:
//     "running"}, a literal that exists ONLY there, outside
//     store.PluginAgentStates entirely (first-party bodies run when their
//     loop dispatches them; the plugin state machine governs imported
//     agents only).
//   - imported agents: "disabled" | "enabled" | "revoked" —
//     store/plugin_state.go:22's PluginAgentStates, the migration-0023 CHECK
//     vocabulary (and plugin/state.go's own State type, which additionally
//     declares "not-installed" as the transition graph's virtual starting
//     point — never a value a stored row or plugin.Agents() actually
//     returns, so it does not appear here).
//
// There is no "blocked" and no "never_arrived" anywhere in the real code.
type agentRosterEntry struct {
	Name       string `json:"name"`
	Provenance string `json:"provenance"` // "first_party" | "imported"
	State      string `json:"state"`      // "running" | "disabled" | "enabled" | "revoked"
	Source     string `json:"source,omitempty"`
	ImportedAt string `json:"importedAt,omitempty"`
}

type agentListResult struct {
	Agents []agentRosterEntry `json:"agents"`
}

// agentRoster reads the whole roster — first-party bodies plus imported
// plugins, sorted by name. The one place both agent.list (T1) and
// tools.grants' unknown-agent check (T2) read it, so the two doors cannot
// disagree about who is a known agent.
func agentRoster(st *store.Store) ([]agentRosterEntry, error) {
	out := make([]agentRosterEntry, 0)
	for _, a := range plugin.Agents() {
		if !a.FirstParty {
			continue
		}
		out = append(out, agentRosterEntry{Name: a.Name, Provenance: "first_party", State: string(a.State)})
	}
	rows, err := st.PluginAgentsAll(true)
	if err != nil {
		return nil, fmt.Errorf("read the plugin ledger: %w", err)
	}
	for _, r := range rows {
		out = append(out, agentRosterEntry{
			Name: r.Name, Provenance: "imported", State: r.State, Source: r.Source, ImportedAt: r.ImportedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// agentRosterHandleFor answers `agent.list` — the whole roster, first-party
// bodies and imported plugins alike, as identity only (TB1: what an agent
// may DO is tools.grants, T2's separate method). Params validation copied
// verbatim from pluginsHandleFor above, the one place that convention lives
// (R6: this read can neither install nor move anything, same as plugins).
func agentRosterHandleFor(st *store.Store) func(ctx context.Context, params json.RawMessage) (any, string, error) {
	return func(ctx context.Context, params json.RawMessage) (any, string, error) {
		if len(params) > 0 {
			var probe map[string]any
			if err := json.Unmarshal(params, &probe); err != nil {
				return nil, fmt.Sprintf("refused: agent.list takes no params — it answers the whole roster or nothing (%v)", err), nil
			}
			if len(probe) > 0 {
				return nil, "refused: agent.list takes no params — a filter would let a reader probe for agents it was not shown; the roster is the surface (R6: this read can neither install nor move anything)", nil
			}
		}
		agents, err := agentRoster(st)
		if err != nil {
			return nil, "", err
		}
		return agentListResult{Agents: agents}, "", nil
	}
}

// toolsGrantsResult is `tools.grants`' wire shape (S34 T2) — the S12
// authority view (mcp.GrantView) for one named agent, verbatim.
type toolsGrantsResult struct {
	Agent  string          `json:"agent"`
	Grants []mcp.GrantView `json:"grants"`
}

// toolGrantsHandleFor answers `tools.grants {agent}`. It ships
// Registry.GrantView's own answer unchanged — no second visibility×authority
// join (S36 MD2's rule, applied here too; see docs/aeon/S34-trust-bench/
// tasks/T2.md's own reality finding, which replaced the brief's imagined
// four-bucket shape with this one). Absent MCP config is MC11's legitimate
// empty, not a refusal: an agent with no config file still has its core
// tools, so the registry builds over an empty Config{} in that case.
func toolGrantsHandleFor(st *store.Store) func(ctx context.Context, params json.RawMessage) (any, string, error) {
	return func(ctx context.Context, params json.RawMessage) (any, string, error) {
		var req struct {
			Agent string `json:"agent"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Sprintf("refused: tools.grants needs an object naming `agent` (%v)", err), nil
		}
		if strings.TrimSpace(req.Agent) == "" {
			return nil, "refused: tools.grants needs a non-empty `agent` field — it answers one agent's view, never the whole roster", nil
		}

		roster, err := agentRoster(st)
		if err != nil {
			return nil, "", err
		}
		known := false
		names := make([]string, 0, len(roster))
		for _, a := range roster {
			names = append(names, a.Name)
			if a.Name == req.Agent {
				known = true
			}
		}
		if !known {
			sort.Strings(names)
			return nil, fmt.Sprintf("refused: unknown agent %q — known agents: %s", req.Agent, strings.Join(names, ", ")), nil
		}

		// S46 T3: the rows are the source (mcp_admin.go); no rows is MC11's
		// legitimate absence, and the core-only answer still applies.
		cfg, err := loadMCPConfig()
		if errors.Is(err, mcp.ErrNoConfig) {
			cfg = mcp.Config{} // MC11: absent config is a state, not a refusal — core tools still apply
		} else if err != nil {
			return nil, fmt.Sprintf("refused: the MCP configuration could not be read (%v) — refused rows are a store fault, not the legitimate absence MC11 describes", err), nil
		}
		reg, err := mcp.New(cfg)
		if err != nil {
			return nil, "", err
		}

		existing, rj := policy.NewStore(policyRepo(st)).List(ctx, time.Now().UTC())
		if rj != nil {
			return nil, fmt.Sprintf("the policy set could not be read (%s) — grants would be invented", rj.Reason), nil
		}

		grants := reg.GrantView(req.Agent, existing, time.Now().UTC())
		return toolsGrantsResult{Agent: req.Agent, Grants: grants}, "", nil
	}
}
