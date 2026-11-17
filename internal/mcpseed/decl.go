// Package mcpseed declares and applies S46's two MCP seed sets against the
// S43-extra engine (MG10): "ship a default MCP server" must be a record,
// never a new mechanism. The production sets are declared and EMPTY — zero
// records ship today (design fact 8), the machinery is proven only with
// fake records, and the first real default is a one-record change.
//
// The class split is the engine's rule 6 made concrete: mcp.servers is
// INERT (definitions nothing may use — enabled=false, no declarations, so
// applying them permits nothing and the startup path is lawful), and
// mcp.grants is GUARD (a declaration or an enabled flip widens what a
// running agent may do — never automatic, only the explicit human door's
// seed.apply/restore). The two sets are two sets PRECISELY because the
// engine's class gate forces it; a set that carried both halves could only
// be guard (starving definitions at start) or inert (smuggling widening).
//
// MC11: this package imports NO internal/mcp. Records carry plain local
// shapes; the one validator reaches the candidate through the RowValidator
// seam cmd/aeon supplies (validate.go), so deleting internal/mcp leaves
// this package intact.
package mcpseed

import (
	"github.com/cjlapao/aeon/internal/seed"
)

// SetIDs are the two set names — the strings the ledger, the events, the
// registry and every door name the sets by. The literals live HERE, once,
// in the domain's declaration (the policyseed PM15 stance).
const (
	SetServers = "mcp.servers"
	SetGrants  = "mcp.grants"
)

// Declarations returns the two set declarations. They are mounted by
// daemon.NewSeedEngine beside agentseed's init-registration and
// policyseed's function mount — and they are functions, not an init(),
// for policyseed's recorded reason: the package-global registry is also
// what test engines build against with their own appliers maps
// (seed.NewEngine refuses a registry set with no applier), and a domain
// may not widen the global registry from a distance and brick those
// builds. The one engine every door holds is built by NewSeedEngine, so
// mounting there IS the registration that matters.
//
// Version is 1 for both (SD2: the set's version is the maximum of its
// records'; with zero records it is the floor). Bumping a real record's
// version bumps the set's with it.
func Declarations() []seed.Set {
	return []seed.Set{
		{
			ID:      SetServers,
			Class:   seed.ClassInert,
			Version: 1,
			Records: ServersRecords,
		},
		{
			ID:      SetGrants,
			Class:   seed.ClassGuard,
			Version: 1,
			Records: GrantsRecords,
		},
	}
}

// ServersRecords is the mcp.servers production source — EMPTY, and
// honestly so (design fact 8: no shipped MCP defaults exist). It is never
// nil (engine rule 3). A shipped default arrives here as one Record:
// canonical JSON of serverContent, key = server name.
func ServersRecords() []seed.Record { return []seed.Record{} }

// GrantsRecords is the mcp.grants production source — EMPTY by the same
// fact and the same rule.
func GrantsRecords() []seed.Record { return []seed.Record{} }
