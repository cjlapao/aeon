// Provenance — one field, and it is the one an imported package must never
// control: FirstParty decides whether AS12 labelling applies, and a package
// that could claim first-party would skip the single thing standing between
// a stranger's prose and an unlabelled prompt. So the field is SET BY THE
// REGISTRY (the import path sets false and only false; the 25 arrive true
// from handler.RegisteredBodies — code, not metadata), and the guarantee is
// a TEST that a claiming package is ignored, not a hope that we remembered
// not to read it.
//
// This answers S14's Q3 (does a first-party result need the label, once
// first-party is a registry property rather than a compile-time fact):
// the boundary is read HERE, and only here — the record says who set it.
package plugin

import "time"

// Provenance is the auditable answer to "where did this agent come from":
// the package path, the import instant, the provenance the REGISTRY holds,
// and whether the package TRIED to claim otherwise.
type Provenance struct {
	Source       string
	ImportedAt   time.Time
	FirstParty   bool // set by the registry; NEVER read from package input
	ClaimedFirst bool // the package asked — recorded, honoured by NOTHING
}

// ProvenanceFor reads the provenance of a known agent.
func ProvenanceFor(name string) (Provenance, bool) {
	a, ok := Lookup(name)
	if !ok {
		return Provenance{}, false
	}
	return Provenance{Source: a.Source, ImportedAt: a.ImportedAt, FirstParty: a.FirstParty, ClaimedFirst: a.ClaimedFirstParty}, true
}

// ProvenanceLine renders one agent's provenance for `aeon agent list
// --provenance`. The claim is reported, NOT silent: a package that tried to
// pass itself off as ours is worth knowing about long after the import, and
// an ignored claim nobody can see is an ignored claim nobody investigates.
func ProvenanceLine(name string) string {
	p, ok := ProvenanceFor(name)
	if !ok {
		return name + " · unknown (not in this process's registry)"
	}
	if p.FirstParty {
		return name + " · first-party (registry: the compiled bodies — AS12 labels no further)"
	}
	line := name + " · imported from " + p.Source + " at " + p.ImportedAt.Format(time.RFC3339) + " · first-party: no (registry-set)"
	if p.ClaimedFirst {
		line += " · PACKAGE CLAIMED FIRST-PARTY — IGNORED (claims are recorded and honoured by nothing; AP9)"
	}
	return line
}
