package main

// tools_cmd.go — S47 T8 (TC10, standing rule 1, P34). `aeon tools` and
// `aeon surface`: the CLI as a CLIENT of the S47 reads.
//
// # The CLI re-derives nothing (the drift rule, TC10)
//
// Not one state, reason, capability, door, drift field or empty-table sentence is
// decided in this file. Every byte the printer shows as a judgement — "the gate
// refuses this", "this is a person's door", "the row disagrees with the code" —
// came off the wire or out of the same adapter, and is printed verbatim. What the
// CLI adds is layout and the words that make a boolean readable (`UNWIRED`,
// `UNCLASSIFIED`, `row stale`, `HUMAN-DOOR-ONLY`), which is presentation, not a
// second opinion. The tests that hold this: `TestToolsShowPrintsPerAgentReasonsVerbatim`
// (the reason bytes equal the wire's) and `TestDirectDoorUsesTheSamePackages`
// (the parser's view of what this file is allowed to compute).
//
// # What the printer must never hide
//
// `list` prints every tool the answer holds, its capability chips as words, and
// the three flags that change how a tool should be read — `UNWIRED` (a conditional
// tool this build never wired: the name exists and calling it is refused),
// `UNCLASSIFIED` (a declared MCP tool nobody classified, MC6), and `row stale`
// (the `tool` row disagrees with the code, TC2's drift — the row is never repaired
// and never allowed to be believed over the code).
// `show` prints the description, the CODE schema pretty-printed (never the row's
// copy), the capabilities, the policy types that judge it per call, the per-agent
// table with each engine sentence verbatim, and every touching policy row with its
// `describe` sentence and its id.
// `agent` groups by state, because "may call" against "refused" is the question —
// and a non-dispatchable agent prints that as the headline with no grant table
// underneath it, because a grant table for an agent nothing can dispatch is a
// table about a row nobody runs (SD7).
// `surface list` prints the doors as words and the `HUMAN-DOOR-ONLY` marker, so a
// person reading a terminal can see which doors exist without knowing the JSON
// field names.
//
// An MCP env value never appears: the wire carries no such field (T5's rule —
// names only, as everywhere), and the printer names a server and its tier, so
// there is no path from a secret to this output (the same §4c discipline the UI
// keeps by having no field to put one in).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

const toolsUsage = `aeon tools — what this build exposes as a tool, asked of the daemon (S47 T8, P34)

  aeon tools list   [--family F] [--source system|mcp] [--capability C] [--available true|false] [--q TEXT] [--json]
  aeon tools show   NAME [--json]      the tool, its code schema, capabilities, what judges it, every agent's state and reason
  aeon tools agent  AGENT [--json]     one agent's whole tool view, grouped by state, the engine's sentences verbatim
  aeon tools status [--json]           the tools.core seed state and every drifted row with its field names

A client of the daemon's four tools.* reads — the same door the app uses (P34).
Offline, with no daemon, it opens the SAME catalog directly over the store and
says so on stderr; any other dial fault (a version mismatch, a permission) it
REFUSES, never falling back (tools_door.go). It changes nothing: there is no verb
here that writes, and none will be added (TC1). --json prints the daemon's own
bytes. The agent's own-view reads (tools.mine.list, tools.mine.show) have NO
verb: they answer SELF, and a person at a terminal is not an agent — use
'aeon tools agent' for the same rows seen from outside.`

const surfaceUsage = `aeon surface — which methods this daemon registers, and through which doors (S47 T6/T8)

  aeon surface list [--kind read|write|control|diagnostic] [--door socket|public|narrow|agentService|toolTwin] [--human-door-only] [--q TEXT] [--json]
  aeon surface show METHOD [--json]

A client of surface.list / surface.get (P34). This verb REQUIRES the running
daemon: what it lists is which methods a fully wired build registers, and the
wiring that decides that lives in the daemon's entry point — an offline listing
would be a partial table wearing the listing's clothes, so it refuses instead
(tools_door.go says why). Nothing here is re-derived: the kind, the doors and the
human-door marker are the daemon's classification, printed as words.`

// toolsDoorFactory is the selection the tools verbs speak through; a package var
// so a test can stand in a door (the S43-extra T7 / S45 T8 idiom) — a verb then
// speaks the fake without a store or a daemon.
var toolsDoorFactory = func() (toolsDoor, error) {
	return selectToolsDoor(context.Background(), os.Stderr)
}

// surfaceDoorFactory is the surface selection, same substitution seam.
var surfaceDoorFactory = func() (surfaceDoor, error) {
	return selectSurfaceDoor(context.Background(), os.Stderr)
}

// cmdTools is `aeon tools …`. No verb names the verbs; an unknown verb is
// refused naming the four that exist (`TestToolsNoVerbNamesTheVerbs`,
// `TestToolsUnknownVerbNamesTheVerbs`).
func cmdTools(w io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(w, toolsUsage)
		return fmt.Errorf("tools needs a verb: list, show, agent, status")
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(w, toolsUsage)
		return nil
	case "list":
		return toolsList(w, args[1:])
	case "show":
		return toolsShow(w, args[1:])
	case "agent":
		return toolsAgent(w, args[1:])
	case "status":
		return toolsStatusVerb(w, args[1:])
	default:
		return fmt.Errorf("tools verb %q unknown — the verbs are list, show, agent, status (all reads; TC1)", args[0])
	}
}

// cmdSurface is `aeon surface …`, the same shape over the two surface reads.
func cmdSurface(w io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(w, surfaceUsage)
		return fmt.Errorf("surface needs a verb: list, show")
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(w, surfaceUsage)
		return nil
	case "list":
		return surfaceListVerb(w, args[1:])
	case "show":
		return surfaceShowVerb(w, args[1:])
	default:
		return fmt.Errorf("surface verb %q unknown — the verbs are list and show (both reads; TC1)", args[0])
	}
}

// printRawJSON prints the daemon's own bytes for the result, verbatim (the one
// difference from printJSON, which re-marshals the decoded value: a re-encode can
// drop a null the wire carried, and a script reading --json would never know).
func printRawJSON(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("the door answered with no body at all — there is nothing to print as JSON")
	}
	fmt.Fprintln(w, string(raw))
	return nil
}

// --- tools list -------------------------------------------------------------

// toolsListFilters is the flag set's answer plus what was SET: a filter that was
// not given must stay nil, because a nil field does not filter and an empty
// string does (the difference between "show me everything" and "show me the
// family named ”", which is an answer of nothing).
func toolsList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("tools list", flag.ContinueOnError)
	var (
		family     = fs.String("family", "", "only this family")
		source     = fs.String("source", "", "system or mcp")
		capability = fs.String("capability", "", "only tools declaring this capability")
		available  = fs.Bool("available", true, "with --available=false, only the tools this build did NOT wire")
		q          = fs.String("q", "", "a substring of a name, description, family or capability")
		jsonFlag   = fs.Bool("json", false, "print the daemon's bytes verbatim")
	)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	f := protocol.ToolsListFilter{}
	if set["family"] && *family != "" {
		f.Family = family
	}
	if set["source"] {
		f.Source = source
	}
	if set["capability"] && *capability != "" {
		f.Capability = capability
	}
	if set["available"] {
		v := *available
		f.Available = &v
	}
	if set["q"] && *q != "" {
		f.Q = q
	}

	door, err := toolsDoorFactory()
	if err != nil {
		return err
	}
	res, raw, derr := door.list(context.Background(), f)
	if derr != nil {
		return derr
	}
	if *jsonFlag {
		return printRawJSON(w, raw)
	}
	renderToolsList(w, res)
	return nil
}

func renderToolsList(w io.Writer, res protocol.ToolsListResult) {
	if res.Statement != "" {
		// The daemon's sentence for a filtered-empty answer (the three states of
		// an empty list): the filter's answer, never "this build has no tools".
		fmt.Fprintln(w, res.Statement)
		return
	}
	if len(res.Tools) == 0 {
		fmt.Fprintln(w, "no tools in this build")
		return
	}
	for _, t := range res.Tools {
		fmt.Fprintf(w, "%s · %s · %s%s\n", t.Name, t.Family, t.Source, toolFlags(t))
		if t.Address != "" {
			fmt.Fprintf(w, "    address: %s\n", t.Address)
		}
		if len(t.Capabilities) > 0 {
			fmt.Fprintf(w, "    capabilities: %s\n", strings.Join(t.Capabilities, " "))
		}
	}
	fmt.Fprintf(w, "%d tools\n", len(res.Tools))
}

// toolFlags is the readable half of a row: the re-run class, and the three states
// that change how the name should be read. Each is the daemon's fact, named; none
// is decided here.
func toolFlags(t protocol.ToolEntry) string {
	var flags []string
	flags = append(flags, t.RerunClass)
	if !t.Available {
		flags = append(flags, "UNWIRED")
	}
	if t.Source == protocol.ToolsSourceMCP && !t.Classified {
		flags = append(flags, "UNCLASSIFIED")
	}
	if !t.RowCurrent {
		// One boolean on the wire covers BOTH drift states — `missing_row` and
		// `differs` — because the entry carries `rowCurrent` and not a drift
		// state (the adapter's own comment says which one it is lives in
		// `tools.status`). Printed as the spec names it: drift is drift, and the
		// verb that names the kind is `status`.
		flags = append(flags, "row stale")
	}
	if t.Server != "" {
		flags = append(flags, "server "+t.Server)
		if t.Tier != "" {
			flags = append(flags, "tier "+t.Tier)
		}
	}
	return " · " + strings.Join(flags, " · ")
}

// --- tools show -------------------------------------------------------------

func toolsShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("tools show", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the daemon's bytes verbatim")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := strings.TrimSpace(fs.Arg(0))
	if name == "" {
		return fmt.Errorf("tools show needs a tool name — 'aeon tools list' prints them")
	}
	door, err := toolsDoorFactory()
	if err != nil {
		return err
	}
	res, raw, derr := door.get(context.Background(), name)
	if derr != nil {
		return derr
	}
	if *jsonFlag {
		return printRawJSON(w, raw)
	}
	renderToolShow(w, res)
	return nil
}

func renderToolShow(w io.Writer, res protocol.ToolsGetResult) {
	t := res.Tool
	fmt.Fprintf(w, "%s · %s · %s%s\n", t.Name, t.Family, t.Source, toolFlags(t))
	if t.Description != "" {
		fmt.Fprintf(w, "  %s\n", t.Description)
	}
	if len(t.Aliases) > 0 {
		fmt.Fprintf(w, "  aliases: %s\n", strings.Join(t.Aliases, ", "))
	}
	if t.Twin != "" {
		fmt.Fprintf(w, "  socket twin: %s\n", t.Twin)
	}
	if len(t.Capabilities) > 0 {
		fmt.Fprintf(w, "  capabilities: %s\n", strings.Join(t.Capabilities, ", "))
	} else {
		fmt.Fprintln(w, "  capabilities: none declared")
	}
	if len(t.Governed) > 0 {
		// "governed by" is the policy types that judge a call per call — the
		// answer to "what decides whether this runs", from the registry's own
		// declaration and not from a remembered list.
		fmt.Fprintf(w, "  governed by: %s\n", strings.Join(t.Governed, ", "))
	}
	if len(t.Schema) > 0 {
		var pretty json.RawMessage
		if err := json.Unmarshal(t.Schema, &pretty); err == nil {
			if indented, merr := json.MarshalIndent(pretty, "  ", "  "); merr == nil {
				fmt.Fprintf(w, "  schema (the code's, %s):\n    %s\n", orDefaultCLI(t.SchemaHash, "no hash"), string(indented))
			}
		}
	}
	if t.Origin != "" {
		fmt.Fprintf(w, "  row: v%d · origin %s\n", t.Version, t.Origin)
	}
	if res.MCP != nil {
		// Names only — a server's name and its tier. No env value exists on the
		// wire to print (§4c's discipline, kept by the wire's shape).
		fmt.Fprintf(w, "  mcp: server %s · tier %s\n", res.MCP.Server, res.MCP.Tier)
	}
	renderAgentRows(w, "  ", res.Agents)
	renderPolicyRefs(w, "  ", res.Policies)
}

// renderAgentRows prints the per-agent table: one line per (tool, agent) cell,
// the state and the ENGINE'S SENTENCE verbatim. A reason is never reworded,
// shortened or re-punctuated here — TestToolsShowPrintsPerAgentReasonsVerbatim
// compares these bytes with the wire's.
func renderAgentRows(w io.Writer, indent string, rows []protocol.AgentGrantRow) {
	if len(rows) == 0 {
		fmt.Fprintf(w, "%sno agents in the answer\n", indent)
		return
	}
	fmt.Fprintf(w, "%sper agent:\n", indent)
	for _, r := range rows {
		line := fmt.Sprintf("%s  %s · %s", indent, r.Agent, r.State)
		if !r.Dispatchable {
			line += " · not dispatchable"
		}
		if r.Reason != "" {
			line += " · " + r.Reason
		}
		if len(r.GrantedBy) > 0 {
			line += fmt.Sprintf(" [granted by %s]", strings.Join(r.GrantedBy, ", "))
		}
		fmt.Fprintln(w, line)
	}
}

func renderPolicyRefs(w io.Writer, indent string, rows []protocol.PolicyRef) {
	if len(rows) == 0 {
		fmt.Fprintf(w, "%sno policy rows touch this tool\n", indent)
		return
	}
	fmt.Fprintf(w, "%spolicy rows that touch it:\n", indent)
	for _, p := range rows {
		line := fmt.Sprintf("%s  %s · %s · %s", indent, p.ID, p.Action, p.Target)
		if p.Expired {
			line += " · expired"
		}
		fmt.Fprintln(w, line)
		if p.Describe != "" {
			// The one formatter's sentence, carried on the row from
			// policy.Describe — printed as it arrived.
			fmt.Fprintf(w, "%s    %s\n", indent, p.Describe)
		}
	}
}

// --- tools agent ------------------------------------------------------------

func toolsAgent(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("tools agent", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the daemon's bytes verbatim")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	agent := strings.TrimSpace(fs.Arg(0))
	if agent == "" {
		return fmt.Errorf("tools agent needs an agent slug — 'aeon agent list' prints them")
	}
	door, err := toolsDoorFactory()
	if err != nil {
		return err
	}
	res, raw, derr := door.agent(context.Background(), agent)
	if derr != nil {
		// An unknown slug refuses naming the known agents — the daemon's
		// sentence, printed untouched, which is what TestToolsAgentUnknownPrintsKnownAgents
		// reads (a CLI that invented its own "no such agent" would be a second
		// answer to a question only the store can answer).
		return derr
	}
	if *jsonFlag {
		return printRawJSON(w, raw)
	}
	renderToolsAgent(w, res)
	return nil
}

// stateOrder is the order the agent view prints its groups in. It is presentation
// only — a fixed sequence so the two doors and two runs print the same bytes —
// and it holds EVERY state of the closed five so a state never arrives ungrouped.
var stateOrder = []string{
	"granted_allowed", "granted_ask", "granted_denied", "granted_no_rule", "not_granted",
}

func renderToolsAgent(w io.Writer, res protocol.ToolsByAgentResult) {
	fmt.Fprintf(w, "agent %s\n", res.Agent)
	if !res.Dispatchable {
		// SD7 as the headline, and NO grant table under it: an agent nothing can
		// dispatch does not have a set of tools it may call, whatever its row
		// names. The rows are still on --json, because the row is a fact about a
		// row and the app screen shows them.
		fmt.Fprintln(w, "  dispatchable: no — nothing dispatches this agent, so it holds no callable set")
		return
	}
	fmt.Fprintln(w, "  dispatchable: yes")
	if len(res.Tools) == 0 {
		fmt.Fprintln(w, "  no tools in the answer")
		return
	}
	byState := map[string][]protocol.AgentGrantRow{}
	for _, r := range res.Tools {
		byState[string(r.State)] = append(byState[string(r.State)], r)
	}
	states := append([]string{}, stateOrder...)
	for s := range byState {
		// A state outside the closed five is still printed rather than dropped —
		// a hidden row is the thing this printer must never do.
		found := false
		for _, known := range states {
			if known == s {
				found = true
				break
			}
		}
		if !found {
			states = append(states, s)
		}
	}
	sort.Strings(states[len(stateOrder):])
	for _, s := range states {
		rows := byState[s]
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s (%d):\n", s, len(rows))
		for _, r := range rows {
			line := fmt.Sprintf("    %s", r.Tool)
			if r.Reason != "" {
				line += " · " + r.Reason
			}
			if len(r.GrantedBy) > 0 {
				line += fmt.Sprintf(" [granted by %s]", strings.Join(r.GrantedBy, ", "))
			}
			fmt.Fprintln(w, line)
		}
	}
}

// --- tools status -----------------------------------------------------------

func toolsStatusVerb(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("tools status", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the daemon's bytes verbatim")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := toolsDoorFactory()
	if err != nil {
		return err
	}
	res, raw, derr := door.status(context.Background())
	if derr != nil {
		return derr
	}
	if *jsonFlag {
		return printRawJSON(w, raw)
	}
	renderToolsStatus(w, res)
	return nil
}

func renderToolsStatus(w io.Writer, res protocol.ToolsStatusResult) {
	fmt.Fprintf(w, "seed set %s · %s · available v%d · applied v%d\n",
		res.Seed.Set, res.Seed.State, res.Seed.AvailableVersion, res.Seed.AppliedVersion)
	fmt.Fprintf(w, "tools: %d system · %d mcp · %d conditional unwired\n",
		res.Counts.System, res.Counts.MCP, res.Counts.ConditionalUnwired)
	if len(res.Drift) == 0 {
		fmt.Fprintln(w, "no drift: every tool row matches the code")
		return
	}
	// Drift is reported and NEVER repaired (TC11): the field names are the
	// disagreement, printed so a person can decide what to do about it.
	fmt.Fprintf(w, "drift (%d):\n", len(res.Drift))
	for _, d := range res.Drift {
		line := fmt.Sprintf("  %s · %s", d.Name, d.State)
		if len(d.Fields) > 0 {
			line += " · fields: " + strings.Join(d.Fields, ", ")
		}
		fmt.Fprintln(w, line)
	}
}

// --- surface ----------------------------------------------------------------

func surfaceListVerb(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("surface list", flag.ContinueOnError)
	var (
		kind      = fs.String("kind", "", "read, write, control or diagnostic")
		door      = fs.String("door", "", "socket, public, narrow, agentService or toolTwin")
		humanOnly = fs.Bool("human-door-only", false, "only the methods a person at the endpoint may call")
		q         = fs.String("q", "", "a substring of a method name or its summary")
		jsonFlag  = fs.Bool("json", false, "print the daemon's bytes verbatim")
	)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	f := protocol.SurfaceListFilter{}
	if set["kind"] && *kind != "" {
		k := protocol.SurfaceKind(*kind)
		f.Kind = &k
	}
	if set["door"] {
		f.Door = door
	}
	if set["human-door-only"] {
		v := *humanOnly
		f.HumanDoorOnly = &v
	}
	if set["q"] && *q != "" {
		f.Q = q
	}
	d, err := surfaceDoorFactory()
	if err != nil {
		return err
	}
	res, raw, derr := d.list(context.Background(), f)
	if derr != nil {
		return derr
	}
	if *jsonFlag {
		return printRawJSON(w, raw)
	}
	renderSurfaceList(w, res)
	return nil
}

func renderSurfaceList(w io.Writer, res protocol.SurfaceListResult) {
	if res.Statement != "" {
		fmt.Fprintln(w, res.Statement)
		return
	}
	for _, e := range res.Methods {
		line := fmt.Sprintf("%s · %s", e.Method, e.Kind)
		if doors := surfaceDoorWords(e.Doors); doors != "" {
			line += " · " + doors
		}
		if e.HumanDoorOnly {
			// The marker that says the method is a person's door and is absent
			// from every surface a non-person reaches — the classification's most
			// consequential fact, printed rather than left in a boolean.
			line += " · HUMAN-DOOR-ONLY"
		}
		if e.Debug {
			line += " · debug"
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "%d of %d methods", res.Counts.Total, res.Counts.Unfiltered)
	if len(res.Counts.ByKind) > 0 {
		kinds := make([]string, 0, len(res.Counts.ByKind))
		for k, n := range res.Counts.ByKind {
			kinds = append(kinds, fmt.Sprintf("%s %d", k, n))
		}
		sort.Strings(kinds)
		fmt.Fprintf(w, " (%s)", strings.Join(kinds, " "))
	}
	fmt.Fprintln(w)
}

// surfaceDoorWords turns the door booleans into the words a person reads:
// `person` is the socket, `public` the HTTP/MCP-stdio projection, `narrow:<action>`
// the narrow projection's named action (or `narrow:unknown` when the daemon never
// wired that seam — the third state, printed as unknown rather than as "none"),
// `agent-service`, and `tool:<name>` for a method an agent-facing tool twins to.
func surfaceDoorWords(d protocol.SurfaceDoors) string {
	var words []string
	if d.Socket {
		words = append(words, "person")
	}
	if d.Public {
		words = append(words, "public")
	}
	switch d.Narrow {
	case "", "none":
		// no narrow door — say nothing rather than say "narrow:none"
	case "unknown":
		words = append(words, "narrow:unknown")
	default:
		words = append(words, "narrow:"+d.Narrow)
	}
	if d.AgentService {
		words = append(words, "agent-service")
	}
	if d.ToolTwin != "" {
		words = append(words, "tool:"+d.ToolTwin)
	}
	return strings.Join(words, " ")
}

func surfaceShowVerb(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("surface show", flag.ContinueOnError)
	jsonFlag := fs.Bool("json", false, "print the daemon's bytes verbatim")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	method := strings.TrimSpace(fs.Arg(0))
	if method == "" {
		return fmt.Errorf("surface show needs a method name — 'aeon surface list' prints them")
	}
	d, err := surfaceDoorFactory()
	if err != nil {
		return err
	}
	res, raw, serr := d.get(context.Background(), method)
	if serr != nil {
		return serr
	}
	if *jsonFlag {
		return printRawJSON(w, raw)
	}
	e := res.Entry
	line := fmt.Sprintf("%s · %s", e.Method, e.Kind)
	if doors := surfaceDoorWords(e.Doors); doors != "" {
		line += " · " + doors
	}
	if e.HumanDoorOnly {
		line += " · HUMAN-DOOR-ONLY"
	}
	fmt.Fprintln(w, line)
	if e.Summary != "" {
		fmt.Fprintf(w, "  %s\n", e.Summary)
	}
	fmt.Fprintf(w, "  requires initialize: %v · debug: %v\n", e.RequiresInitialize, e.Debug)
	return nil
}

// orDefaultCLI keeps a blank field from printing as a bare space.
func orDefaultCLI(v, dflt string) string {
	if strings.TrimSpace(v) == "" {
		return dflt
	}
	return v
}
