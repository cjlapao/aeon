// Building the entries (S47 T5): the system half from code (TC2 — the served
// schema and hash are the registry's, the row supplies version/origin/rowCurrent
// only) and the MCP half from the seam (TC4 — no tool row, so no version).

package daemon

import (
	"encoding/json"
	"sort"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/toolseed"
)

// --- the entries -----------------------------------------------------------------

// systemEntries renders every system tool the CODE offers: the honest registry
// names plus the conditionals, whether or not their seams are wired, each with the
// row's version, origin and drift verdict.
//
// The iteration is over `toolseed.Source()` and never over `Registry()`, for two
// reasons that are both load-bearing. The registry's keys include the hidden
// `write_file` alias, and one executor rendered as two entries is precisely the
// second truth TC11 exists to make visible (the alias rides on `write`'s
// `aliases`). And the registry OMITS an unwired conditional, which would make
// `search` simply not exist in a build without its seam — when the honest fact is
// that it exists and cannot run today (P31's three-state availability).
func (w *catalogWorld) systemEntries() []protocol.ToolEntry {
	out := make([]protocol.ToolEntry, 0, len(w.code))
	for _, rec := range w.code {
		c := w.contents[rec.Key]
		e := protocol.ToolEntry{
			Name:         c.Name,
			Aliases:      c.Aliases,
			Family:       c.Family,
			Source:       protocol.ToolsSourceSystem,
			Description:  c.Description,
			Schema:       c.Schema,
			SchemaHash:   c.SchemaHash,
			RerunClass:   c.RerunClass,
			Capabilities: c.Capabilities,
			Governed:     c.Governed,
			Twin:         c.Twin,
			Conditional:  c.Conditional,
			Version:      rec.Version,
			Origin:       "system",
		}
		if e.Capabilities == nil {
			e.Capabilities = []string{} // [] and never null (the settings-list contract)
		}
		caps := make([]tools.Capability, 0, len(e.Capabilities))
		for _, c := range e.Capabilities {
			caps = append(caps, tools.Capability(c))
		}
		e.Category, e.Access = e.Family, tools.AccessOf(caps)
		e.Address = tools.SystemAddress(e.Family, e.Name, caps)
		if e.Governed == nil {
			e.Governed = []string{}
		}
		if t, wired := w.reg[c.Name]; wired {
			e.Available = true
			// TC2: the served schema is the one the executor parses a call
			// against, canonicalised the way the hash is taken. Serving the row's
			// copy would put the catalog out of step with the toolset the model is
			// handed, and the disagreement would be invisible — which is what
			// `tools.status`' drift is for instead.
			e.Schema = json.RawMessage(tools.CanonicalSchema(t.Spec))
			e.SchemaHash = tools.SchemaHash(t.Spec)
		}
		if row, hasRow := w.rows[c.Name]; hasRow {
			e.Version = row.Version
			e.Origin = row.Origin
		}
		// Three facts, not two: compared-and-equal is `rowCurrent`, and never
		// compared (no drift entry at all) is not. `missing_row` and `differs`
		// both read false; which one it is lives in `tools.status`' report, and
		// neither is repaired here (TC11).
		d, seen := w.drift[c.Name]
		e.RowCurrent = seen && d.State == toolseed.DriftCurrent
		out = append(out, e)
	}
	return out
}

// mcpEntries renders the declared tools of every ENABLED server. Three absences
// are recorded here rather than papered over:
//
//   - `description` is empty because no authority holds one: the `tool` table is
//     not consulted for an MCP tool (TC4 — S46's tables are its truth) and S46's
//     `mcp_server_tool` carries name, rerun and effect only, so `MCPToolView` has
//     no description field to carry. Inventing a sentence for a third-party tool
//     would be the catalog claiming knowledge it does not have. Recorded as a
//     named gap in T5's Notes: the honest fix is a description column in S46's
//     table, which is not this task's file to move (TC13).
//   - `capabilities` is empty because a declared tool declares none, and a policy
//     cannot allow what declares nothing (S14's rule) — the same rule the runtime
//     applies, so the catalog agrees with the gate instead of flattering the tool.
//   - `version` is 0 and `rowCurrent` is true because there is no seeded row to be
//     out of date about: TC11's drift check is the `tools.core` set's, and saying
//     `false` here would report a disagreement that cannot exist.
func (w *catalogWorld) mcpEntries() []protocol.ToolEntry {
	out := make([]protocol.ToolEntry, 0, len(w.servers))
	for _, s := range w.servers {
		for _, t := range s.Tools {
			// S74 TA3: an MCP tool is a read only when it is classified, local
			// and safe to re-run — the server's readOnlyHint feeds that class
			// (internal/mcp/classify.go). Anything undeclared is a write: absent
			// is not safe (P31).
			access := tools.AccessWrite
			if t.Classified && string(t.Effect) == "local" && t.Rerun == string(tools.RerunSafe) {
				access = tools.AccessRead
			}
			out = append(out, protocol.ToolEntry{
				Name:         s.Name + "." + t.Name,
				Family:       s.Name,
				Category:     "mcp",
				Access:       access,
				Address:      tools.MCPAddress(s.Name, t.Name, access),
				Source:       protocol.ToolsSourceMCP,
				Schema:       json.RawMessage(`{}`),
				RerunClass:   t.Rerun,
				Capabilities: []string{},
				Governed:     []string{},
				Available:    true,
				Server:       s.Name,
				Tier:         s.Tier,
				Effect:       t.Effect,
				Classified:   t.Classified,
				Origin:       s.Origin,
				RowCurrent:   true,
			})
		}
	}
	return out
}

// entries is the whole catalog as one slice: every system tool the code offers
// plus the enabled MCP tools, ordered (source, family, name) — the rows' own
// reading order, stable between two refreshes so a list does not jump under the
// cursor. System first, because "mcp" sorts before "system" and a catalog that
// opened with third-party tools would read as though they were the primary ones.
func (w *catalogWorld) entries() []protocol.ToolEntry {
	all := append(w.systemEntries(), w.mcpEntries()...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Source != all[j].Source {
			return all[i].Source > all[j].Source // "system" before "mcp"
		}
		if all[i].Family != all[j].Family {
			return all[i].Family < all[j].Family
		}
		return all[i].Name < all[j].Name
	})
	return all
}
