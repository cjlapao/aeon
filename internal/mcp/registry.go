package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/tools"
)

// Tier is where a tool belongs. A tool belongs to exactly one (MC1), and
// which one is settled by CONFIGURATION, never by the caller (MC12): core is
// built in, public sits in the public section, private nests under its one
// agent. This type is the load-bearing consequence of Q3 — the tier field on
// a Tool is a record of where the operator put the server, not an input.
type Tier string

const (
	TierCore    Tier = "core"
	TierPublic  Tier = "public"
	TierPrivate Tier = "private"
)

// Tool is one tool as one agent's view reports it. It is a value handed out
// by ToolsFor and nothing more — the registry owns the only storage.
//
// Effect is empty for core tools: the EffectClass policy type is still
// S16's declared stub, and core effect is enforced (when it arrives) by
// S16's evaluator, not by this registry inventing classes for tools whose
// prose and code it did not write. For EXTERNAL tools an empty class is not
// an absence — it is a refusal (T3).
type Tool struct {
	// Qualified is the name the model sees: "server.tool" for MCP tiers
	// (MC8 — two servers offering `search` are `alpha.search` and
	// `beta.search`, both reachable, neither shadowed), the bare name for
	// core.
	Qualified string

	// Server is the namespace owner; empty for core (built in, MC1).
	Server string

	// Name is the tool's own name within the server (or the core name).
	Name string

	Tier Tier

	// Rerun and Effect are the classes WE declared (Q4): read from Aeon's
	// configuration, never from the server's self-assessment. A server's
	// own claim is a hint that may RAISE and never LOWER (T3) — and it
	// arrives through the client, not this struct.
	Rerun  tools.RerunClass
	Effect EffectClass

	// Description and InputSchema are the server's self-report (T2),
	// attached only to tools the configuration already carries. They are
	// UNTRUSTED until labelled where they reach a model — that labelling is
	// T4's, and until it exists this field's doc comment is the security
	// note. A configured tool whose server never listed it keeps these
	// empty: the absence is real (T8 reports it).
	Description string
	InputSchema json.RawMessage
}

// EffectClass is the L22 vocabulary, carried as data until S16's evaluator
// owns it. The values are the strings config.go validates against.
type EffectClass string

const (
	EffectClassLocal        EffectClass = EffectLocal
	EffectClassOutward      EffectClass = EffectOutward
	EffectClassIrreversible EffectClass = EffectIrreversible
)

// Availability is the answer Availability() gives: three states because the
// three refusals are three different facts, and a transcript must tell them
// apart (T5).
type Availability int

const (
	// AvailabilityUnknown: not in this agent's registry at all. A peer
	// asking for another agent's private tool gets THIS, identical to the
	// answer for a tool nobody ever configured — `unknown tool`, never
	// `denied`, because `denied` confirms the tool exists (MC2, D2).
	AvailabilityUnknown Availability = iota

	// AvailabilityUnavailable: the tool exists for this agent but is
	// unclassified — refused, naming what is missing (MC6). Not a policy
	// denial; a different fact that reads differently in a transcript.
	AvailabilityUnavailable

	// AvailabilityAvailable: in this agent's registry and classified.
	// Whether the CALL proceeds is S10's answer (T7), not this one.
	AvailabilityAvailable
)

// Refusal is one row of the refused view: what is not offered, and why.
// The refused view exists so refusal is SPELLED OUT (`aeon mcp list
// --include-refused`, DEMO §5) rather than invisible — a tool silently
// missing is a model wondering why it cannot do what its prompt says it
// can.
type Refusal struct {
	Qualified string
	Reason    string

	// MissingClasses names what the classification lacks ("rerun",
	// "effect"); nil for refusal kinds that are not about classification.
	// A pointer so "no classes missing" and "this refusal is not about
	// classes" are distinguishable.
	MissingClasses *[]string
}

// Registry answers, for one agent, what tools exist — and nothing else.
//
// The type is deliberately small: there is exactly one exported method that
// returns tools (ToolsFor), and the two views that need to know about tools
// they do NOT have (Availability, Refused) answer in verdicts, never in
// Tool values. TestNoAllToolsFunctionExists greps the surface to keep it so
// (D3); widening it must be a deliberate edit to that test.
type Registry struct {
	cfg Config

	// lmu guards listings: server → tool name → the server's own report.
	// It is the ONLY place server-supplied data lives, and it is keyed
	// only under names ApplyListings already admitted — configuration
	// remains the sole source of a tool's existence (MC3).
	lmu      sync.Mutex
	listings map[string]map[string]RemoteTool
	clients  map[string]*Client // attached by the daemon (T8 owns lifecycle)
}

// New validates the configuration again and builds the registry.
// Re-validation is not redundancy: New is callable with a hand-built Config (tests,
// the daemon's embedded default), and a registry over an unvalidated config
// would be a security object whose invariants were checked only on the
// loader's lucky path.
func New(cfg Config) (*Registry, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Registry{cfg: cfg, listings: map[string]map[string]RemoteTool{}}, nil
}

// ApplyListings attaches a server's self-report (tools/list, T2) to the
// tools the configuration ALREADY carries for that server.
//
// Two refusals make MC3 executable at this boundary:
//
//   - an unconfigured server is refused outright — a server announcing
//     itself is not a server configured, and its listings change nothing;
//   - a reported tool the config never declared is dropped, not added —
//     the server's offer is not the operator's edit, and "offered" is not
//     "classified" (T3), let alone "granted" (T7).
//
// What is kept: descriptions and schemas for configured tools (untrusted
// data awaiting T4's label), and the listing set itself, which T8's
// degradation reads to report "declared but the server never listed it".
func (r *Registry) ApplyListings(server string, reported []RemoteTool) error {
	if !r.configured(server) {
		return fmt.Errorf("mcp: server %q is not configured — listings from an unconfigured server are refused (MC3)", server)
	}
	byName := map[string]RemoteTool{}
	for _, rt := range reported {
		byName[rt.Name] = rt
	}
	r.lmu.Lock()
	defer r.lmu.Unlock()
	r.listings[server] = byName
	return nil
}

// configured answers whether the operator's file names this server at all
// (public section or any agent's private tier).
func (r *Registry) configured(server string) bool {
	for _, s := range r.cfg.Public {
		if s.Name == server {
			return true
		}
	}
	for _, ac := range r.cfg.Agents {
		for _, s := range ac.Private {
			if s.Name == server {
				return true
			}
		}
	}
	return false
}

// listed returns the server's own report for a tool, if any.
func (r *Registry) listed(server, tool string) (RemoteTool, bool) {
	r.lmu.Lock()
	defer r.lmu.Unlock()
	byName, ok := r.listings[server]
	if !ok {
		return RemoteTool{}, false
	}
	rt, ok := byName[tool]
	return rt, ok
}

// ToolsFor is THE accessor (T1, MC2, D3): core the agent was granted,
// public it declared, private configured for it — and nothing else exists
// from its point of view.
//
// The empty agent is not a wildcard: it answers nothing. There is no
// caller-supplied "all" because "all" is the list the whole design refuses
// to have.
func (r *Registry) ToolsFor(agent string) []Tool {
	if agent == "" {
		return nil
	}
	ac := r.cfg.Agents[agent] // absent agent: core only, no MCP tools (MC11)

	var out []Tool
	out = append(out, r.coreTools(ac)...)
	out = append(out, r.publicTools(agent, ac)...)
	out = append(out, r.privateTools(ac)...)

	// One attach point for the server's self-report: the capability list
	// stays config-shaped, and only the prose/schema ride in. Deep-copy the
	// schema so a caller mutating what ToolsFor handed out cannot edit the
	// registry's stored bytes (the immutability tripwire).
	//
	// The same attach point is the ONLY raise site: the hint merges into the
	// classes WE declared, upward only (T3/Q4). Availability is deliberately
	// un-raised — existence and classifiability are config facts; the
	// effective classes are what the model is shown and what T6's use-time
	// ask decides on.
	for i := range out {
		if out[i].Tier == TierCore {
			continue
		}
		if rt, ok := r.listed(out[i].Server, out[i].Name); ok {
			out[i].Description = rt.Description
			if rt.InputSchema != nil {
				out[i].InputSchema = append(json.RawMessage(nil), rt.InputSchema...)
			}
			out[i] = out[i].withHint(rt.Annotations)
		}
	}
	return out
}

// coreTools returns the built-in tools the agent's grant admits. The
// classes are the ones internal/tools declares — the registry reads them, it
// never re-labels (a registry that re-declares classes is a second source
// that will disagree with the first).
func (r *Registry) coreTools(ac AgentConfig) []Tool {
	reg := tools.Registry()
	names := make([]string, 0, len(reg))
	if ac.Core == nil {
		for name := range reg {
			if name == aliasWriteFile {
				continue // the hidden alias is filtered from every listing (S11)
			}
			names = append(names, name)
		}
	} else {
		names = append(names, ac.Core...)
	}
	sort.Strings(names)

	out := make([]Tool, 0, len(names))
	for _, name := range names {
		tool, ok := reg[name]
		if !ok {
			continue // Validate refused unknown names; belt for hand-built configs.
		}
		out = append(out, Tool{
			Qualified: name,
			Name:      name,
			Tier:      TierCore,
			Rerun:     tool.RerunClass(),
		})
	}
	return out
}

// publicTools returns the public tools this agent DECLARED. Public means
// available-to-declare, not granted-to-all: an agent that does not name a
// public tool does not have it (T6), which is what keeps MC5's freedom from
// becoming "every agent holds every public tool".
func (r *Registry) publicTools(agent string, ac AgentConfig) []Tool {
	var out []Tool
	for _, q := range ac.Declares {
		server, toolName, ok := splitQualified(q)
		if !ok {
			continue // Validate refused it; belt.
		}
		s, isPublic := publicServer(r.cfg, server)
		if !isPublic {
			continue
		}
		tc, found := findTool(s, toolName)
		if !found || !classified(tc) {
			continue // unclassified tools never reach a capability list (T3)
		}
		out = append(out, externalTool(TierPublic, s.Name, tc))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}

// privateTools returns the servers nested under this agent, and ONLY this
// agent's: the caller cannot ask for another agent's private tier because
// the answer is computed from this agent's own configuration entry — there
// is no other entry it could be computed from.
func (r *Registry) privateTools(ac AgentConfig) []Tool {
	var out []Tool
	for _, s := range ac.Private {
		for _, tc := range s.Tools {
			if !classified(tc) {
				continue
			}
			out = append(out, externalTool(TierPrivate, s.Name, tc))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out
}

// Availability answers which of the three refusals — or none — applies to
// (agent, qualified). It answers in verdicts, never in tools: this is how
// the call path distinguishes the refusals without a side door for
// extracting Tool values.
//
// The peer case and the nonexistent case return the SAME answer by
// construction (one code path, not two that must be kept equal): `unknown
// tool` for both, so no timing, wording, or shape difference confirms a
// private tool exists (D2).
func (r *Registry) Availability(agent, qualified string) (Availability, []string, Tier) {
	if agent == "" || qualified == "" {
		return AvailabilityUnknown, nil, ""
	}

	if server, toolName, ok := splitQualified(qualified); ok {
		// The agent's own private server: visible to it, invisible to all.
		ac := r.cfg.Agents[agent]
		for _, s := range ac.Private {
			if s.Name != server {
				continue
			}
			if tc, found := findTool(s, toolName); found {
				if missing := missingClasses(tc); len(missing) > 0 {
					return AvailabilityUnavailable, missing, TierPrivate
				}
				return AvailabilityAvailable, nil, TierPrivate
			}
			return AvailabilityUnknown, nil, TierPrivate
		}

		// A public server: available only through declaration. A peer that
		// never declared it is answered exactly as a stranger is.
		if s, isPublic := publicServer(r.cfg, server); isPublic {
			if !declares(ac, qualified) {
				return AvailabilityUnknown, nil, TierPublic
			}
			if tc, found := findTool(s, toolName); found {
				if missing := missingClasses(tc); len(missing) > 0 {
					return AvailabilityUnavailable, missing, TierPublic
				}
				return AvailabilityAvailable, nil, TierPublic
			}
			return AvailabilityUnknown, nil, TierPublic
		}

		return AvailabilityUnknown, nil, ""
	}

	// No dot: a core name, or nothing.
	ac := r.cfg.Agents[agent]
	if ac.Core != nil && !containsStr(ac.Core, qualified) {
		return AvailabilityUnknown, nil, TierCore
	}
	if _, ok := tools.Registry()[qualified]; !ok || qualified == aliasWriteFile {
		return AvailabilityUnknown, nil, ""
	}
	return AvailabilityAvailable, nil, TierCore
}

// Refused returns the tools this agent does NOT get, with the reason each
// one is refused — the `--include-refused` view (DEMO §5). It covers the
// servers this agent can see (its private tier and its declarations): a
// refusal for a tool the agent has no relationship with would itself be a
// disclosure.
func (r *Registry) Refused(agent string) ([]Refusal, error) {
	if agent == "" {
		return nil, nil
	}
	ac := r.cfg.Agents[agent]

	var out []Refusal
	add := func(server string, tc ToolConfig) {
		missing := missingClasses(tc)
		if len(missing) == 0 {
			return
		}
		parts := make([]string, 0, 2)
		for _, m := range missing {
			parts = append(parts, "no "+m+" class")
		}
		out = append(out, Refusal{
			Qualified:      server + "." + tc.Name,
			Reason:         fmt.Sprintf("unclassified — %s (MC6)", strings.Join(parts, ", ")),
			MissingClasses: &missing,
		})
	}

	for _, s := range ac.Private {
		for _, tc := range s.Tools {
			add(s.Name, tc)
		}
	}
	for _, q := range ac.Declares {
		server, toolName, ok := splitQualified(q)
		if !ok {
			continue
		}
		s, isPublic := publicServer(r.cfg, server)
		if !isPublic {
			continue
		}
		if tc, found := findTool(s, toolName); found {
			add(s.Name, tc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified < out[j].Qualified })
	return out, nil
}

// --- shared lookups ---------------------------------------------------------

func publicServer(cfg Config, name string) (ServerConfig, bool) {
	for _, s := range cfg.Public {
		if s.Name == name {
			return s, true
		}
	}
	return ServerConfig{}, false
}

func findTool(s ServerConfig, name string) (ToolConfig, bool) {
	for _, t := range s.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolConfig{}, false
}

func declares(ac AgentConfig, qualified string) bool {
	return containsStr(ac.Declares, qualified)
}

func classified(tc ToolConfig) bool { return len(missingClasses(tc)) == 0 }

// missingClasses answers what the classification lacks, in the order the
// refusal sentence names them. Empty string means absent; an unrecognised
// value was refused at load (config.go), so anything non-empty here is
// genuinely missing, not misspelled.
func missingClasses(tc ToolConfig) []string {
	var missing []string
	if tc.Rerun == "" {
		missing = append(missing, "rerun")
	}
	if tc.Effect == "" {
		missing = append(missing, "effect")
	}
	return missing
}

func externalTool(tier Tier, server string, tc ToolConfig) Tool {
	return Tool{
		Qualified: server + "." + tc.Name,
		Server:    server,
		Name:      tc.Name,
		Tier:      tier,
		Rerun:     tools.RerunClass(tc.Rerun),
		Effect:    EffectClass(tc.Effect),
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
