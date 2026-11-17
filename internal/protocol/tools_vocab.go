// The catalog's vocabulary door (S47 T10, TC3 + TC10).
//
// # The gap this door closes
//
// TC10 froze the S47 surface at eight methods, and none of the eight sends the
// WORDS the catalog is built from — only the values a tool happens to carry. The
// Settings panel T10 renders needs both halves: the capability names with their
// one-line meanings (TC3: the vocabulary is the daemon's, and a screen that
// writes its own sentence for `spawn_process` has created a second vocabulary
// that can disagree with the first in silence) and the spellings a filter select
// may offer (family, source, re-run class). Without a door the choices were to
// hardcode them in the UI — the one thing this slice's own guard test exists to
// catch — or to send them from the code that declares them. The operator ruled
// the second on 2026-11-02, and this file is that ruling.
//
// # What it is NOT
//
// It is not a ninth WRITE (TC1 still ships zero), not a filter (it answers what
// the words are, never which rows match), and not a cache of the catalog: the
// entries themselves still come from `tools.list`, freshly read (N8). The
// families it lists are the ones the catalog can render today, so a filter built
// from this answer cannot offer a value the list would then refuse — the pair is
// asserted in the daemon's own test, not assumed.

package protocol

import (
	"context"
	"encoding/json"
)

// --- the wire types -----------------------------------------------------------

// ToolCapabilityNote is one capability of the closed vocabulary with the
// daemon's own one-line meaning of it. `Name` is the capability exactly as
// `internal/tools` declares it — the panel renders this string and never
// translates, rewrites or re-spells it, because the value a policy judges is the
// value a person must be able to recognise.
type ToolCapabilityNote struct {
	Name    string `json:"name"`
	Meaning string `json:"meaning"`
}

// ToolsVocabularyResult is the whole vocabulary the catalog screens draw their
// words from (TC3). Every list is ordered and non-nil on the wire: an absent
// list would be a screen rendering nothing where the daemon answered something,
// and an unordered one would make a select jump between refreshes.
//
//   - `capabilities` is the closed capability vocabulary with its meanings —
//     exactly `tools.CapabilityNotes()`, so the panel's pills and the runtime's
//     gate read the same words.
//   - `families` is every family the catalog can render right now: the record
//     catalog's families for the system half, and the enabled servers' names for
//     the MCP half (an MCP tool's family IS its server, which is why a family
//     filter and an MCP server link are the same fact seen twice).
//   - `sources` is the two-word origin set (`system`, `mcp`) — sent rather than
//     typed so a third word cannot appear in a select that the daemon would
//     then filter to nothing.
//   - `rerunClasses` is the closed re-run vocabulary as the code declares it,
//     the same three spellings the committed `rerun_classes.json` pins.
type ToolsVocabularyResult struct {
	Capabilities []ToolCapabilityNote `json:"capabilities"`
	Families     []string             `json:"families"`
	Accesses     []string             `json:"accesses"` // S74: the access words a filter may offer
	Sources      []string             `json:"sources"`
	RerunClasses []string             `json:"rerunClasses"`
}

// --- the handler ---------------------------------------------------------------

// handleToolsVocabulary takes no params: the answer is a function of the
// compiled build and of which MCP servers are enabled, never of a filter
// (handleToolsStatus's argument — a door that accepted params would be
// inventing a fact the code already settles). The params bag is still decoded
// into an empty struct so a member that is not an object takes JSON-RPC's
// -32602 rather than being silently ignored, and an identity key is refused like
// every other read here: this door says what the words mean, and who is asking
// changes nothing about the answer.
func (t *Table) handleToolsVocabulary(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := rejectIdentityKeys(string(MethodToolsVocabulary), params); err != nil {
		return nil, err
	}
	if err := decodeParams(params, &struct{}{}); err != nil {
		return nil, err
	}
	raw, err := t.toolsCatalogCall(ctx, func(svc ToolsCatalog) (any, error) {
		return svc.ToolsVocabulary(ctx)
	})
	if err != nil {
		return nil, err
	}
	vocab, ok := raw.(ToolsVocabularyResult)
	if !ok {
		return nil, ErrStoreUnavailable.WithWhat("the tools catalog seam returned a value that is not a vocabulary result — the seam and this package disagree about the shape between them, which is a build fault, not a refusal")
	}
	return ShapeToolsVocabularyResult(vocab), nil
}

// ShapeToolsVocabularyResult is the ONE place a `tools.vocabulary` answer is
// shaped: the nil-slice normalisation that keeps every list a `[]` on the wire
// instead of `null`. Exported for the same reason ShapeToolsListResult is — the
// CLI's direct door and the socket door must produce the same bytes, and two
// copies of "when is a list empty rather than absent" is the drift this slice's
// parity guard exists to catch.
func ShapeToolsVocabularyResult(vocab ToolsVocabularyResult) ToolsVocabularyResult {
	if vocab.Capabilities == nil {
		vocab.Capabilities = []ToolCapabilityNote{}
	}
	if vocab.Families == nil {
		vocab.Families = []string{}
	}
	if vocab.Sources == nil {
		vocab.Sources = []string{}
	}
	if vocab.RerunClasses == nil {
		vocab.RerunClasses = []string{}
	}
	return vocab
}
