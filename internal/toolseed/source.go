// Package toolseed declares and applies S47's `tools.core` seed set against the
// S43-extra engine: the **catalog entries** of the system tools become rows,
// produced by the seed process and current at every start, while the executors
// and the schemas the runtime parses a call against stay exactly where they are
// — in code, in `internal/tools` (design TC2).
//
// # Why a seed set and not a migration
//
// A tool's catalog entry is configuration-order data (a person lists and reads
// it), but it is authored in code, so the only lawful way it reaches a row is
// the seed process: one record per tool, applied at start, versioned, with the
// ledger saying what is applied and the golden file refusing a silent content
// change (SD12's promise exercised — a sibling slice adding a tool adds one
// record and touches no migration and no mechanism).
//
// # The class is inert, and that is a security claim
//
// `tools.core` is **inert** (SD5): applying it widens nothing. The rows are a
// display copy; `BuildToolset` still reads `Registry()`, and the gate still
// judges the frozen set — `TestRuntimeStillBuildsSpecsFromTheRegistry` and T2's
// `TestToolTableIsNotReadByTheRuntimeSpecPath` are what make that a fact of the
// build rather than an intention. The engine's own rule 6 enforces the pairing
// from the other side: an inert set with a guard-affecting applier is refused at
// registration, so `GuardAffecting() == false` here is checked, not trusted.
//
// # What is NOT in here
//
// No MCP tool. S46's tables are their one truth (TC4) and `internal/mcp` is
// importable only by `cmd/aeon` (MC11) — a catalog that joined the two would be
// a second answer to "what tools are there", and `TestToolseedImportsNoMcp`
// keeps this package deletable along with the rest of the layer.
package toolseed

import (
	"encoding/json"
	"sort"

	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/tools"
)

// conditionalTools are the two tools that exist in code in **every** build and
// are *registered* only when a seam is wired (V13: nil seam ⇒ absent, never
// present-and-refusing). The catalog cannot depend on a build flag or a wiring
// choice — a person reading the catalog in a no-retrieval daemon must still see
// that `search` and `graph` exist and know they are conditional — so `Source`
// names them always, flags them `conditional`, and builds their spec from the
// exported constructors (`tools.Search()`, `tools.Graph()`), which are callable
// with no resolver wired: their `Run` checks the seam at call time.
var conditionalTools = []string{"search", "graph"}

// Content is one tool's catalog entry, the domain format this package owns. The
// field set is the task's, and the JSON names are the row's column names so
// `Drift` compares a record and a row on the same vocabulary.
//
// `Version` is deliberately **not** a field: it is the record's metadata
// (`seed.Record.Version`), and the engine's divergence check is a content
// comparison. A content change that did not bump its version is what
// `TestToolVersionsBumpWithContent` refuses, so the version rides with the
// content without being part of what is hashed.
type Content struct {
	Name         string          `json:"name"`
	Family       string          `json:"family"`
	Description  string          `json:"description"`
	Schema       json.RawMessage `json:"schema"`
	SchemaHash   string          `json:"schema_hash"`
	RerunClass   string          `json:"rerun_class"`
	Capabilities []string        `json:"capabilities"`
	Governed     []string        `json:"governed"`
	Twin         string          `json:"twin"`
	Aliases      []string        `json:"aliases"`
	Conditional  bool            `json:"conditional"`
	Notes        string          `json:"notes"`
}

// Source returns one record per **system** tool: the honest registry names, plus
// `search` and `graph` whether or not their seams are wired. The hidden
// `write_file` alias is not a record of its own — it is a key in `Registry()`
// that `ToolNames()` filters out, and recording it separately would put a second
// row behind a tool that has one executor, which is the drift TC11 exists to
// make visible. It rides on `write`'s `aliases`.
//
// Every field is read from the code that *is* the tool — the spec and class from
// `Registry()`, the capabilities/family/governed/notes from `tools.MetaFor`
// (T1), the twin from `records.Catalog()` — never restated here. A second copy
// of a description in this file would be a second truth to drift.
func Source() []seed.Record {
	reg := tools.Registry()
	honest := map[string]bool{}
	for _, n := range tools.ToolNames() {
		honest[n] = true
	}
	names := make([]string, 0, len(honest)+len(conditionalTools))
	for n := range honest {
		names = append(names, n)
	}
	for _, n := range conditionalTools {
		if !honest[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	// The hidden aliases, derived rather than named: every registry key the
	// honest listing filters out, mapped to the tool it canonicalises to. A new
	// compatibility key then appears in its owner's `aliases` with no edit here,
	// and a removed one disappears — the same rule `ToolNames` already enforces
	// about what a person may be shown.
	hidden := map[string][]string{}
	for key := range reg {
		if honest[key] {
			continue
		}
		owner := tools.CanonicalToolName(key)
		hidden[owner] = append(hidden[owner], key)
	}

	// The socket twins, from the one catalog that pairs a tool with its method.
	twin := map[string]string{}
	for _, s := range records.Catalog() {
		twin[s.Name] = s.Method
	}

	// The whole metadata table, **once**. `MetaFor` is function-built (T1's rule
	// against a package-level map), which is right for a door asked about one
	// tool and wrong here: called per name it rebuilds all 68 entries 68 times,
	// ~1.3 ms each — and the engine asks for the records once per record
	// (`engine.recordFor` calls `Set.Records()`), so a 100 ms `Source` is a
	// several-second startup apply. Measured on this box, and it broke
	// `cmd/aeon`'s `controlLoopFixture`, which waits 20 s for the daemon's
	// startup seed to become visible. One `AllMeta()` brings the whole pass to
	// the low milliseconds; `MetaFor` stays the per-name door for T5 and T6.
	metas := tools.AllMeta()

	recs := make([]seed.Record, 0, len(names))
	for _, name := range names {
		tool, ok := reg[name]
		if !ok {
			// Only the conditionals can land here, and only unwired.
			switch name {
			case "search":
				tool = tools.Search()
			case "graph":
				tool = tools.Graph()
			default:
				continue // A name with neither a registry entry nor a constructor is not a tool.
			}
		}
		meta, ok := metas[name]
		if !ok {
			continue // T1's completeness test makes this unreachable; a tool with no Meta must not be seeded.
		}
		c := Content{
			Name:         name,
			Family:       meta.Family,
			Description:  tool.Spec.Description,
			Schema:       json.RawMessage(tools.CanonicalSchema(tool.Spec)),
			SchemaHash:   tools.SchemaHash(tool.Spec),
			RerunClass:   string(tool.Class),
			Capabilities: strings(meta.Capabilities),
			Governed:     append([]string(nil), meta.Governed...),
			Twin:         twin[name],
			Aliases:      append([]string(nil), hidden[name]...),
			Conditional:  isConditional(name),
			Notes:        meta.Notes,
		}
		if c.Capabilities == nil {
			c.Capabilities = []string{}
		}
		if c.Governed == nil {
			c.Governed = []string{}
		}
		if c.Aliases == nil {
			c.Aliases = []string{}
		}
		b, err := seed.Canonical(c)
		if err != nil {
			// Unreachable for this struct (plain fields and a valid JSON schema),
			// and unreachable-not-silent is the point: a content that cannot be
			// canonicalised cannot be hashed, and a record whose hash is a lie is
			// worse than no record.
			panic("toolseed: the catalog content of " + name + " cannot be canonicalised: " + err.Error())
		}
		recs = append(recs, seed.Record{Key: name, Version: recordVersion(name), Content: b})
	}
	return recs
}

// ContentOf decodes a record's content. It is exported because `Drift` and the
// daemon's status read both need the typed view of a record, and a third parse
// of the same JSON somewhere else would be a third place to get a field's
// meaning wrong.
func ContentOf(r seed.Record) (Content, error) {
	var c Content
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return Content{}, err
	}
	return c, nil
}

// strings spells a capability set as the plain strings the row and the JSON
// carry. The type exists in Go to stop a free-string capability (TC3); the row
// stores the vocabulary's words, and this is the one place the two meet.
func strings(cs []tools.Capability) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, string(c))
	}
	return out
}

func isConditional(name string) bool {
	for _, c := range conditionalTools {
		if c == name {
			return true
		}
	}
	return false
}
