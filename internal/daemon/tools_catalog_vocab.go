package daemon

// ════════════════════════════════════════════════════════════════════════════
// The catalog's vocabulary answer (S47 T10, TC3 + TC10): the door behind
// `tools.vocabulary`, the one place the words a catalog screen renders are sent
// to the screen.
//
// # Why the answers come from three different authorities
//
// The four lists look alike on the wire and are not alike in authority, and the
// difference is the whole point of writing them separately:
//
//   - `capabilities` is a CLOSED vocabulary declared in code
//     (`tools.Capabilities()`, checked closed by T1's own test), so the code is
//     the only thing that may say what it contains — hence
//     `tools.CapabilityNotes()`, the same names with the daemon's one-line
//     meanings. It lists every capability the vocabulary knows even if no tool
//     declares one today, because a vocabulary is not a census.
//   - `rerunClasses` is likewise closed (`tools.RerunClasses()`, the three
//     spellings the committed `rerun_classes.json` pins), so it is declared here
//     rather than counted out of the rows.
//   - `sources` and `families` are NOT closed: `system` and `mcp` are, but
//     whether an MCP tool exists at all is a fact about this build's seam and
//     the servers enabled in the store, and a family is whatever the records
//     catalog declares or a server is called. So these two are read out of the
//     entries the catalog can actually render, which buys the property a filter
//     needs: an option this door offers is a value `tools.list` can answer for.
//     A screen that offered `mcp` in a build with no MCP half would send the
//     person to the daemon's `no tools match this filter.` for a choice the
//     screen itself invented.
//
// # V11 still rules
//
// The entries are read through `world`, so a store that cannot be read is an
// ERROR out of this door, not a vocabulary missing its families: a screen built
// on a half answer is the fabricated answer this slice refuses everywhere else.
// And N8 still rules: nothing here is memoised across calls — enabling a server
// changes the next answer, no restart and no pause.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"sort"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/tools"
)

// ToolsVocabulary answers `tools.vocabulary`. See the block above for which
// authority each list is read from and why it is read there rather than written
// in the screen that consumes it.
func (sc *toolsCatalogService) ToolsVocabulary(ctx context.Context) (protocol.ToolsVocabularyResult, error) {
	notes := tools.CapabilityNotes()
	caps := make([]protocol.ToolCapabilityNote, 0, len(notes))
	for _, n := range notes {
		caps = append(caps, protocol.ToolCapabilityNote{Name: string(n.Capability), Meaning: n.Meaning})
	}

	classes := make([]string, 0, 3)
	for _, c := range tools.RerunClasses() {
		classes = append(classes, string(c))
	}

	w, err := sc.world(ctx)
	if err != nil {
		return protocol.ToolsVocabularyResult{}, err
	}
	families := uniqueSorted(w.entries(), func(e protocol.ToolEntry) string { return e.Family })
	sources := uniqueSorted(w.entries(), func(e protocol.ToolEntry) string { return e.Source })

	return protocol.ToolsVocabularyResult{
		Capabilities: caps,
		Families:     families,
		Accesses:     []string{tools.AccessRead, tools.AccessWrite},
		Sources:      sources,
		RerunClasses: classes,
	}, nil
}

// uniqueSorted maps the catalog's entries to one field and returns the distinct
// non-empty values in sorted order. Empty is dropped rather than listed: a
// family no entry carries is not a family the list can be filtered to, and an
// empty option in a select is how a screen offers "nothing" as a choice.
func uniqueSorted(entries []protocol.ToolEntry, field func(protocol.ToolEntry) string) []string {
	seen := make(map[string]bool, len(entries))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		v := field(e)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
