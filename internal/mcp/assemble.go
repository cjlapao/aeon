package mcp

// assemble.go — rows to Config, the one direction the runtime reads (S46
// T2). Pure: no store, no disk, no clock. The order of this file matters
// and is tested: build the candidate from EVERY row (disabled included),
// run the ONE validator on it, and only then optionally filter the disabled
// servers out (T2.md law 2) — so a declaration can never dangle past
// validation, and the registry's view stays resolvable through the
// registry's existing "not configured" (Refused) path rather than a new
// validation error invented here.

import (
	"fmt"
	"sort"
)

// AssembleOptions is the single dial Assemble has.
type AssembleOptions struct {
	// IncludeDisabled keeps servers whose row says Enabled:false. The
	// candidate path (every write, and the round-trip law) passes true —
	// the truth includes what exists but is not offered. The registry path
	// passes false — a disabled server is not offered (MG4), though the
	// declarations that name its tools STAND.
	IncludeDisabled bool
}

// serverEntry carries a shaped server alongside the row's enabled-ness.
// ServerConfig has no Enabled field (config.go is S12's, unedited — the
// file format never carried one), so the flag rides BESIDE it, which is
// exactly the fact MG4 stores in a column.
type serverEntry struct {
	sc      ServerConfig
	enabled bool
}

// Assemble turns Rows into the Config the unchanged registry consumes, and
// returns Config.Validate()'s OWN error verbatim — never a rewording, never
// a second opinion (T2.md law 3: rows and file refuse the same mistake with
// the same words). Output is deterministic: public servers by name, each
// agent's private servers by name, declares sorted — so the same rows
// always produce byte-identical JSON, and the same truth never produces
// flaky grants.
func Assemble(r Rows, opt AssembleOptions) (Config, error) {
	pub, priv, meta, err := shapeRows(r)
	if err != nil {
		return Config{}, err
	}

	// The complete truth — validation sees disabled servers too, so a
	// declaration cannot dangle past the one validator (law 2).
	c := buildConfig(pub, priv, meta)
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	if opt.IncludeDisabled {
		return c, nil
	}
	// The registry's view is the same truth MINUS the not-offered servers,
	// filtered AFTER validation; their declarations stand (the grant is the
	// agent's, the outage is the server's), and the registry's existing
	// Refused path is the honest answer.
	keep := func(es []serverEntry) []serverEntry {
		var out []serverEntry
		for _, e := range es {
			if e.enabled {
				out = append(out, e)
			}
		}
		return out
	}
	return buildConfig(keep(pub), privView(priv, keep), meta), nil
}

func privView(priv map[string][]serverEntry, keep func([]serverEntry) []serverEntry) map[string][]serverEntry {
	out := make(map[string][]serverEntry, len(priv))
	for k, es := range priv {
		out[k] = keep(es)
	}
	return out
}

// agentMeta is one agent's narrowing, shaped during row placement and
// applied at build time — kept out of Config itself so the filter rebuild
// never has to re-derive it.
type agentMeta struct {
	core     []string
	declares []string
}

// shapeRows places the rows into tiered lists (Q3: position is the tier)
// without judging content. Its only refusals are STRUCTURAL — a row whose
// tier and agent key disagree is not a config mistake an operator makes
// (the store's CHECK net refuses it, T1) but a caller bug, and it would
// otherwise assemble into a tier nobody wrote.
func shapeRows(r Rows) ([]serverEntry, map[string][]serverEntry, map[string]agentMeta, error) {
	var pub []serverEntry
	priv := map[string][]serverEntry{}

	for _, s := range r.Servers {
		switch {
		case s.Tier == TierPublic && s.AgentKey != "":
			return nil, nil, nil, fmt.Errorf("mcp: server row %q is public but names agent %q — a public server belongs to nobody", s.Name, s.AgentKey)
		case s.Tier == TierPrivate && s.AgentKey == "":
			return nil, nil, nil, fmt.Errorf("mcp: server row %q is private but names no agent — a private server belongs to exactly one", s.Name)
		case s.Tier != TierPublic && s.Tier != TierPrivate:
			return nil, nil, nil, fmt.Errorf("mcp: server row %q has tier %q — the row's tier is public or private (MG2)", s.Name, s.Tier)
		}
		e := serverEntry{
			sc:      ServerConfig{Name: s.Name, Transport: copyTransport(s.Transport), Tools: copyTools(s.Tools)},
			enabled: s.Enabled,
		}
		if s.Tier == TierPublic {
			pub = append(pub, e)
			continue
		}
		priv[s.AgentKey] = append(priv[s.AgentKey], e)
	}
	sort.Slice(pub, func(i, j int) bool { return pub[i].sc.Name < pub[j].sc.Name })
	for k := range priv {
		es := priv[k]
		sort.Slice(es, func(i, j int) bool { return es[i].sc.Name < es[j].sc.Name })
		priv[k] = es
	}

	// The agent entries: every agent with an AgentRow, plus every key that
	// owns a private server — so an AgentRow never silently re-parents a
	// server it does not describe, and a server-owning agent is in the
	// Config even with no narrowing of its own.
	meta := map[string]agentMeta{}
	for _, a := range r.Agents {
		if _, dup := meta[a.AgentKey]; dup {
			// A repeated key is the caller's bug (the table's PRIMARY KEY
			// refuses it, T1); naming it here beats silently last-wins.
			return nil, nil, nil, fmt.Errorf("mcp: agent row %q appears twice — one narrowing per agent", a.AgentKey)
		}
		m := agentMeta{declares: copySorted(a.Declares)}
		if a.Core != nil {
			core := copyCore(a.Core)
			m.core = core
		} else {
			// nil means "the whole core set" — the distinction rides on the
			// presence of the key's narrowing below.
			m.core = nil
		}
		meta[a.AgentKey] = m
	}
	for k := range priv {
		if _, ok := meta[k]; !ok {
			meta[k] = agentMeta{}
		}
	}
	return pub, priv, meta, nil
}

func buildConfig(pub []serverEntry, priv map[string][]serverEntry, meta map[string]agentMeta) Config {
	var c Config
	for _, e := range pub {
		c.Public = append(c.Public, e.sc)
	}
	if len(meta) > 0 {
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		c.Agents = map[string]AgentConfig{}
		for _, k := range keys {
			m := meta[k]
			ac := AgentConfig{Core: m.core, Declares: m.declares}
			for _, e := range priv[k] {
				ac.Private = append(ac.Private, e.sc)
			}
			c.Agents[k] = ac
		}
	}
	return c
}

func copyTransport(t TransportConfig) TransportConfig {
	out := t
	if len(t.Args) > 0 {
		out.Args = append([]string(nil), t.Args...)
	} else {
		out.Args = nil
	}
	if len(t.Env) > 0 {
		out.Env = append([]string(nil), t.Env...)
	} else {
		out.Env = nil
	}
	return out
}

func copyTools(ts []ToolConfig) []ToolConfig {
	if len(ts) == 0 {
		return nil
	}
	return append([]ToolConfig(nil), ts...)
}

func copyCore(core *[]string) []string {
	if core == nil {
		return nil
	}
	if len(*core) == 0 {
		return []string{}
	}
	return append([]string(nil), *core...)
}

func copySorted(xs []string) []string {
	if len(xs) == 0 {
		return nil
	}
	out := append([]string(nil), xs...)
	sort.Strings(out)
	return out
}
