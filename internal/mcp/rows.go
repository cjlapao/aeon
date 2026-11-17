package mcp

// rows.go — S46 T2: the row shapes the configuration lives as in the
// database (design MG1), seen from THIS side. These are plain structs on
// purpose: `internal/mcp` never imports `internal/store` or `database/sql`
// (MC11 — the deletability that made S12 safe to build now extends to the
// storage layer), so the caller — `cmd/aeon`'s admin seam — maps the store's
// rows to these and back, and maps S43's agent SLUG to the dispatch KEY the
// registry uses and back (MG2: the mapping is the caller's, the meaning is
// here). An `AgentKey` is the name the registry keys agents by; this package
// never sees a slug and never invents one.
//
// Enabled is the row's, not the Config's (MG4): a disabled server is a
// server that exists and is not offered — the registry path assembles with
// IncludeDisabled:false, every candidate path with true, so the one
// validator always judges the complete truth (T2.md law 2).

// Tiers are S12's own (registry.go: TierCore/TierPublic/TierPrivate — the
// values "public"/"private" are what the store's tier column holds, and the
// caller maps the column text onto these). The tier is stored data, not
// position — POSITION is the tier only in the JSON file (Q3); rows have no
// nesting to carry the fact, so the column states it and the store's CHECK
// nets enforce the coupling (public ⇔ no agent key, private ⇔ exactly one).

// ServerRow is one mcp_server row: a server, its tier, its owner (only when
// private), its transport, its classified tools, and whether it is offered.
// Transport.Env carries variable NAMES only — the same C17 rule as the file;
// a value-shaped entry is refused by Validate on the way through Assemble.
type ServerRow struct {
	Name      string
	Tier      Tier   // TierPublic | TierPrivate (S12's own constants)
	AgentKey  string // "" iff TierPublic; the owning agent's dispatch key iff private
	Transport TransportConfig
	Tools     []ToolConfig
	Enabled   bool
}

// AgentRow is one agent's narrowing: its core grant and its declarations,
// keyed by the dispatch key. Core nil is the WHOLE registered core set
// (S11's default); a non-nil pointer is a narrowing, and *Core == an empty
// list is "none" — two different facts that never collapse (MC's two
// empties, carried into the rows).
type AgentRow struct {
	AgentKey string
	Core     *[]string
	Declares []string
}

// Rows is the whole configuration as data — the store's McpAll mapped
// through the caller's slug↔key table, and the input to Assemble.
type Rows struct {
	Servers []ServerRow
	Agents  []AgentRow
}
