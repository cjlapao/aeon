package history

// ════════════════════════════════════════════════════════════════════════════
// The history row (S25 T4): the ONE shape every surface renders a history
// entry as — the CLI's `list`/`export` and the socket's `history.list`/
// `history.export`.
//
// H3's parity is structural, not a test that could rot: the CLI (cmd/aeon)
// and the socket (internal/protocol) both turn an Entry into a Row through
// THIS one function. Neither surface has its own row shape, so a cost or a
// reason cannot come out differently under the socket than under the CLI —
// that is the second truth H3 exists to forbid, made impossible by having one
// definition, not by a test that compares two.
//
// H6/P32: the cost is Rollup.Render — a floor, never a bare number. D4/H4:
// the reason is the stored reason, verbatim, never a synthesized verdict.
// Both are the package's own reads, so the row carries them without a second
// formatter anywhere in either surface.
// ════════════════════════════════════════════════════════════════════════════

// HistoryRow is one history entry as every surface renders it. The JSON tags
// are the wire shape the app reads (T5) and the NDJSON the CLI emits (T3);
// the same tags in both are what makes the two byte-identical by construction.
type HistoryRow struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Timeline string `json:"timeline"`
	Cost     string `json:"cost"`
	Reason   string `json:"reason,omitempty"`
	// Describe is the row's details through the ONE formatter (S53 T1, HV2),
	// additive on the S25 wire: the same Entry this row flattened from, rendered
	// with lane/outcome/duration/cost-state/links as data. The eight fields
	// above keep their exact S25 meaning — history.list/show/export are
	// otherwise unchanged (HV5).
	Describe Described `json:"describe"`
}

// Row is the ONE conversion from an Entry to the shape every surface renders
// (H3): the cost through the single floored formatter (H6), the reason
// verbatim (D4). A second conversion in any surface is a second truth.
func Row(e Entry) HistoryRow {
	return HistoryRow{
		Kind:     e.Kind.String(),
		ID:       e.ID,
		Slug:     e.Slug,
		Title:    e.Title,
		Status:   e.Status,
		Timeline: e.Timeline,
		Cost:     e.Cost.Render(),
		Reason:   e.Reason,
		Describe: Describe(e),
	}
}
