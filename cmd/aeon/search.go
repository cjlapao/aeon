package main

// search.go — `aeon search` (S22 T9): the human's client of the SAME
// socket door the model's `search` tool shares (D15/P34 — a client verb,
// not a second implementation; the resolver answers both). It prints
// locations, never text (V4/D8): `file:line–line` plus the score, and the
// keyword-only note when the answer is that half of the system.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
)

func cmdSearch(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required — the index is per root, V12)")
	corpus := fs.String("corpus", "", `which corpus: "code" or "history" (required — no default, O8)`)
	limit := fs.Int("limit", 0, "maximum hits (0 = the resolver's default)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("search needs --root — staleness and the index are properties of ONE project root (I5 refuses the ambient guess)")
	}
	if *corpus != "code" && *corpus != "history" {
		return fmt.Errorf(`search refuses corpus %q — name "code" or "history"; there is no default (O8)`, *corpus)
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return fmt.Errorf("search needs a query — an empty query is not a question")
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var ans search.SearchAnswer
	p := protocol.SearchQueryParams{ComponentRoot: *root, Corpus: *corpus, Query: query, Limit: *limit}
	if err := c.Call(protocol.MethodSearchQuery, p, &ans); err != nil {
		return err
	}
	if len(ans.Hits) == 0 {
		fmt.Fprintln(w, "no locations — the index holds nothing for this query (the refusal, if any, would have said why)")
		return nil
	}
	for _, h := range ans.Hits {
		fmt.Fprintf(w, "  %s:%d-%d\t%.4f\n", h.Source, h.LineStart, h.LineEnd, h.Score)
	}
	if ans.KeywordOnly {
		fmt.Fprint(w, "\nkeyword-only: no embedding route answered — this is the lexical half speaking (V9/D4), read the files for truth\n")
	}
	if ans.Staleness != "" {
		fmt.Fprintf(w, "\nstaleness: %s\n", ans.Staleness)
	}
	return nil
}
