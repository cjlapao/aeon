package agentseed

import (
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// The agents set is the ONE inert, human-door set the S43-extra process
// ships: it is the 33 first-party bodies, each a row. It is the lossless
// round-trip SD8 promises: the source builds a record's Content from the
// handler's loaded Definition, the applier writes that Content to the agent
// row, and the fingerprint reads it back — so nothing a body declares is
// lost in the move from code to row.
const (
	// SetID is the agents set's id (the grammar is dotted lower-cased
	// segments; "agents.firstParty" is one inert set, SD2).
	SetID = "agents.firstParty"
	// setVersion is the set's version — a single number, the applied
	// version the ledger tracks against it (SD1).
	//
	// 2026-11-08 (documents): 1 -> 2. The deliverable authors' bodies moved
	// their output into the database (doc_write/doc_read/doc_edit and
	// task_create in place of the workspace write/edit), and a seeded row is
	// only rewritten by a HIGHER version over an unmodified row (seed/plan.go),
	// so the bump is what carries the new bodies to a live store. A row an
	// operator edited stays diverged and is never overwritten (SD6).
	setVersion = 3
)

// content is the canonical content of one agent record — everything first
// party dispatch reads, plus the recorded inert fields. The source builds
// it from the handler's Definition; the applier writes it to the agent row
// (the direct columns plus the Metadata blob); the fingerprint reads it
// back. One shape, three directions — the round-trip is lossless because
// the applier and the fingerprint agree on this exact shape.
type content struct {
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Temperature float64           `json:"temperature"`
	Tools       []string          `json:"tools"`
	Handoff     map[string]string `json:"handoff"`
	PolicyAgent string            `json:"policy_agent"`
	Tech        []string          `json:"tech"`
	Body        string            `json:"body"`
	Mode        string            `json:"mode"`
	Variant     string            `json:"variant"`
	Retired     []string          `json:"retired"`
	Declared    []string          `json:"declared"`
}

// contentFromDefinition builds the content from a loaded handler
// Definition. The slug is the name slugified (the same derivation the S43
// stub migration uses, so a record and its stub share a key). The inert
// fields (mode, variant, the retired and declared-false tool lists) are
// recorded so a future reading has the bytes; nothing branches on them.
func contentFromDefinition(name string, d handler.Definition) content {
	tools := append([]string(nil), d.Tools...)
	if tools == nil {
		tools = []string{}
	}
	// Handoff must round-trip as a non-nil map when empty: a nil map and an
	// empty map marshal differently, and the applier writes back what it is
	// given, so normalise to empty.
	handoff := map[string]string{}
	for k, v := range d.Handoff {
		handoff[k] = v
	}
	return content{
		Slug:        store.Slugify(d.Name),
		Name:        d.Name,
		Description: d.Description,
		Temperature: d.Temperature,
		Tools:       tools,
		Handoff:     handoff,
		PolicyAgent: d.PolicyAgent,
		Tech:        nilIfEmpty(d.Tech),
		Body:        d.Body,
		Mode:        d.Mode,
		Variant:     d.Variant,
		Retired:     nilIfEmpty(d.RetiredFound),
		Declared:    nilIfEmpty(d.DeclaredFalse),
	}
}

// Source is the agents set's declared records — one per registered body,
// the record's Content the canonical JSON of its content. The code is the
// seed source, not the truth (SD1): a body added to the tree but not yet
// applied shows up here, waiting for the ledger.
func Source() []seed.Record {
	var out []seed.Record
	// The seeder's record source is the embedded (code) bodies, always —
	// never the installed (row) source, which would seed from its own output
	// (SD1: the code is the seed source). RegisteredBodies() now delegates to
	// the installed source, so the seeder uses the named EmbeddedBodies().
	for _, name := range handler.EmbeddedBodies() {
		if name == intakeBody {
			// 2026-11-05 (S61 T9): the intake-architect seeds into its OWN
			// set (agents.intake, intake.go) — firstParty's 33 records stay
			// the settled census it shipped, and one row may not be claimed
			// by two sets (the second apply would read the first's row as an
			// unadoptable conflict). The exclusion is the filter; the body
			// stays a registered handler either way.
			continue
		}
		d, rej := handler.EmbeddedDefinition(name)
		if rej != nil {
			// A registered body that will not load is a source fault, not a
			// record to skip: the set would be silently incomplete.
			panic("agentseed: a registered body will not load — " + name + ": " + rej.Reason)
		}
		c := contentFromDefinition(name, d)
		out = append(out, seed.Record{
			Key:     c.Slug,
			Version: setVersion,
			Content: mustCanonical(c),
		})
	}
	return out
}

// mustCanonical is the source's canonical form of a content — the bytes the
// engine hashes and the applier writes. A content that will not canonicalise
// is a source fault, not a record to skip (the set would be incomplete).
func mustCanonical(c content) []byte {
	b, err := seed.Canonical(c)
	if err != nil {
		panic("agentseed: a content will not canonicalise — " + err.Error())
	}
	return b
}

func nilIfEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return in
}
