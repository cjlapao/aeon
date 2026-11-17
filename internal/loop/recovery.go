package loop

import (
	"fmt"
	"strings"
)

// recoveryNote is appended to a step's prompt for its one recovery attempt: what is missing, how the first attempt
// ended and what it said, and what to do about it. The first attempt's own report is carried because it is often the
// whole of the work (an investigation's findings), so the second attempt can file it instead of redoing it.
func recoveryNote(missing, outcome, reported string) string {
	report := strings.TrimSpace(reported)
	if len(report) > 6000 {
		report = report[:6000] + "…"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n---\nRECOVERY. Your previous attempt at this step ended (%s) without filing `%s`, the output this step must produce. ", outcome, missing)
	b.WriteString("Do not start the work over. First call `doc_list` (or `loop_context`) to see what exists, then file `" + missing + "` NOW with `doc_write`, ")
	b.WriteString("from the work already done; re-read a source only for something you are actually missing. Keep your reasoning brief: the deliverable is what this attempt is for.")
	if report != "" {
		b.WriteString("\n\nWhat your previous attempt reported:\n" + report)
	}
	return b.String()
}
