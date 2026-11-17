package mcpseed

import (
	"context"

	"github.com/cjlapao/aeon/internal/store"
)

// RowValidator is the seam through which the ONE validator reaches the
// candidate (MG4's law, S43-extra's SD rule that no applier re-implements
// a domain's grammar): mcpseed must not import internal/mcp (MC11), so the
// daemon's caller — cmd/aeon, the only named importer of the mcp grammar
// (MC11's list) — supplies the move that assembles the candidate rows into
// a Config and runs Config.Validate over it. The candidate arrives as the
// FULL post-write world: every live row plus this record's effect applied.
//
// The refusal sentence it returns is the validator's own and is returned
// VERBATIM by Upsert — the CLI/daemon/desk parity argument (T5) applies
// here too: a seed record that would produce an invalid configuration is
// refused with the same words the write door would use.
//
// A nil validator fails CLOSED: Upsert refuses ("no validator supplied")
// rather than writing an unvalidated row. An empty production set never
// reaches it; a domain that ships records without wiring the seam is
// refused, not trusted.
type RowValidator func(ctx context.Context, candidate store.McpAll) error

// candidateServers converts a record into the store row Upsert writes —
// origin 'system', enabled 0, the timestamps the applier's clock says. The
// candidate read for the validator uses it too, so what is validated is
// byte-for-byte what is written.
func serverRowFromContent(c serverContent, at string) store.McpServerRow {
	args, _ := jsonMarshalStrings(c.Transport.Args)
	env, _ := jsonMarshalStrings(c.Transport.Env)
	row := store.McpServerRow{
		Name:          c.Name,
		Tier:          c.Tier,
		TransportType: c.Transport.Type,
		Command:       c.Transport.Command,
		Args:          args,
		Env:           env,
		URL:           c.Transport.URL,
		Enabled:       false, // the inert set ships definitions nothing may use
		Origin:        "system",
		CreatedAt:     at,
		UpdatedAt:     at,
	}
	for _, t := range c.Tools {
		row.Tools = append(row.Tools, store.McpToolRow{Name: t.Name, Rerun: t.Rerun, Effect: t.Effect})
	}
	return row
}

// overlay applies one record's effect to a copy of the live rows — the
// candidate the validator sees. It mutates a COPY (the live slice rides
// untouched from the read); a server key is replaced, a declare key adds,
// an enable key flips the named server's Enabled.
func (a *Applier) overlay(ctx context.Context, all store.McpAll, rec serverContent, kind grantKind, g grantContent) (store.McpAll, error) {
	out := all
	switch a.set {
	case SetServers:
		row := serverRowFromContent(rec, a.stamp())
		out.Servers = append([]store.McpServerRow{}, all.Servers...)
		replaced := false
		for i := range out.Servers {
			if out.Servers[i].Name == row.Name {
				out.Servers[i] = row
				replaced = true
			}
		}
		if !replaced {
			out.Servers = append(out.Servers, row)
		}
	case SetGrants:
		switch kind {
		case grantDeclare:
			out.Declares = append(append([]store.McpDeclareRow{}, all.Declares...),
				store.McpDeclareRow{AgentSlug: g.Agent, Qualified: g.Qualified})
		case grantEnable:
			out.Servers = append([]store.McpServerRow{}, all.Servers...)
			for i := range out.Servers {
				if out.Servers[i].Name == g.Server {
					out.Servers[i].Enabled = true
				}
			}
		}
	}
	return out, nil
}
