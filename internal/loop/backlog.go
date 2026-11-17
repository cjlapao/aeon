package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The deferred-findings backlog — S17 T8 (O7, I6, E4).
//
// A review that reports ok WITH findings worth recording and NOT worth
// blocking on must put them somewhere durable; without somewhere to
// live, every finding becomes a blocker by default and the review lane
// becomes the thing people route around. Appended, never rewritten — a
// backlog that gets regenerated loses the history that makes it a
// backlog.
//
// Q10, answered: the file lives in the PROJECT the loop works on at
// `docs/unresolved-pr-comments.md` — v1's name, kept on purpose (the
// person triaging it six months from now greps for the path they
// already know) — and the entry shape is documented in
// `aeon/docs/BACKLOG.md`. The spec names that document `aeon/docs/
// BACKLOG.md`; the runtime path naming diverges deliberately and the
// divergence is recorded in both places.

// BacklogEntry is one deferred finding. PR and Date are REQUIRED in
// every entry (T8's TestBacklogEntryNamesPRAndDate): an entry that does
// not say which PR and which day cannot be triaged.
type BacklogEntry struct {
	PR      int    // the PR from the EVIDENCE, never from prose (Q7)
	Date    string // YYYY-MM-DD
	Finding string // the reviewer's own words
	Lane    string
	Step    string
}

func BacklogPath(root string) string {
	return filepath.Join(root, "docs", "unresolved-pr-comments.md")
}

const backlogHeader = "# Deferred findings\n" +
	"\n" +
	"Appended by the loop's review lane, never rewritten. Each entry names\n" +
	"its PR, its date, its lane/step, and the finding. Shape documented in\n" +
	"aeon/docs/BACKLOG.md (S17 T8).\n"

// AppendBacklog opens O_APPEND|O_CREATE and writes one line — the
// append-only guarantee is a flag, not a discipline. There is no
// rewrite path in this file: the only os.OpenFile call opens for
// appending, and a test (TestBacklogIsAppendOnly) asserts prior bytes
// survive as a prefix.
func AppendBacklog(root string, e BacklogEntry) error {
	if strings.TrimSpace(e.Finding) == "" {
		return fmt.Errorf("backlog: an entry must name a finding — an empty entry is noise that trains nobody to read it")
	}
	dir := filepath.Dir(BacklogPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := BacklogPath(root)
	prefix := ""
	if _, err := os.Stat(path); os.IsNotExist(err) {
		prefix = backlogHeader
	} else if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	pr := fmt.Sprintf("PR #%d", e.PR)
	if e.PR <= 0 {
		pr = "PR (no number recorded)"
	}
	date := e.Date
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	line := fmt.Sprintf("%s- %s · %s · %s/%s · %s\n", prefix, pr, date, e.Lane, e.Step, e.Finding)
	_, err = f.WriteString(line)
	return err
}

// evidencePR is the backlog entry's PR number, read from the recorded
// evidence of the actions that created it (Q7: numbers come from
// evidence rows, never from prose).
func evidencePR(st *LoopState) int {
	for _, ev := range st.Evidence {
		if n := atoiSafe(ev["pr_number"]); n > 0 {
			return n
		}
	}
	return 0
}
