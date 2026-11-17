package mcp

// disassemble.go — Config to rows, the write side's mirror (S46 T2). Pure
// and total: every valid Config disassembles, and the round-trip law
// (T2.md law 1) says the trip back loses nothing:
//
//	Assemble(Disassemble(c), {IncludeDisabled: true}) == c
//
// including the two empties that must stay distinct — Core nil ("the whole
// core set") versus Core *[]string to an empty list ("none"). Disassembly
// sets Enabled:true for every server: a Config, from the file or from any
// candidate, describes servers that are being offered; the enable/disable
// fact is a row's (MG4) and no Config in existence has ever carried it.

import "sort"

// Disassemble flattens a Config into Rows. Public servers become
// TierPublic rows with no agent key; each agent's Private block becomes
// TierPrivate rows owned by that agent key; the agent's own narrowing
// becomes an AgentRow (present iff the agent appears in the Config).
// Deterministic: public by name, each agent's private by name, declares
// sorted — the same order Assemble produces, so rows written today and read
// back tomorrow are byte-stable.
func Disassemble(c Config) Rows {
	var r Rows
	for _, s := range c.Public {
		r.Servers = append(r.Servers, serverRowOf(s, TierPublic, ""))
	}
	if len(c.Agents) > 0 {
		keys := make([]string, 0, len(c.Agents))
		for k := range c.Agents {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ac := c.Agents[k]
			for _, s := range ac.Private {
				r.Servers = append(r.Servers, serverRowOf(s, TierPrivate, k))
			}
			a := AgentRow{AgentKey: k, Declares: copySorted(ac.Declares)}
			if ac.Core != nil {
				core := append([]string(nil), ac.Core...)
				if core == nil {
					core = []string{} // a non-nil Core means a narrowing; "none" stays "none"
				}
				a.Core = &core
			}
			r.Agents = append(r.Agents, a)
		}
	}
	sort.SliceStable(r.Servers, func(i, j int) bool {
		a, b := r.Servers[i], r.Servers[j]
		if a.Tier != b.Tier {
			return a.Tier == TierPublic // public block first, deterministic
		}
		if a.AgentKey != b.AgentKey {
			return a.AgentKey < b.AgentKey
		}
		return a.Name < b.Name
	})
	return r
}

func serverRowOf(s ServerConfig, tier Tier, agentKey string) ServerRow {
	return ServerRow{
		Name:      s.Name,
		Tier:      tier,
		AgentKey:  agentKey,
		Transport: copyTransport(s.Transport),
		Tools:     copyTools(s.Tools),
		Enabled:   true, // a Config describes what is offered; the row owns the flag (MG4)
	}
}
