package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The two tokens normalizeGolden may introduce. Nothing else in this file
// substitutes for what the CLI printed.
const (
	goldenTimeToken = "<TIME>"
	goldenIDToken   = "<ID>"
)

// goldenNormalizations is the single normalisation pass, applied to both the
// command transcript and the store dump. Each rule targets one shape: an
// RFC3339 instant; a proposal id in prose (`proposal 7 committed`); an event
// row's leading id; a proposal id in the list's own ID column. They are
// ordered so the id-column rules see the text the time rule already produced.
var goldenNormalizations = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z`), goldenTimeToken},
	{regexp.MustCompile(`\bproposal \d+\b`), "proposal " + goldenIDToken},
	{regexp.MustCompile(`(?m)^(\s*)event \d+ `), "${1}event " + goldenIDToken + " "},
	// An events row line: the id column is followed by the instant rule 1
	// already turned into <TIME>, so the instant is what marks the leading
	// number as a row id rather than a count. (RE2 has no lookahead, hence the
	// capture of the token rather than a look-around.)
	{regexp.MustCompile(`(?m)^(\s*)\d+ +(` + goldenTimeToken + `)`), "${1}" + goldenIDToken + " ${2}"},
	// The proposal list's own ID column, marked by the kind and status that
	// follow it in the row.
	{regexp.MustCompile(`(?m)^(\s*)\d+ +([a-z][a-z0-9_]* +(?:proposed|committed|rejected|discarded)\b)`), "${1}" + goldenIDToken + " ${2}"},
	// S46 T6 (recorded): retiring the MCP file mirror made the store dumper's
	// "(unreadable: …)" branch reachable for the first time — and it embeds the
	// fixture's t.TempDir() path, which carries a random suffix. The
	// volatility token normalizes the volatile part; the golden still pins
	// THAT the file is absent there, which is the fact the case asserts.
	{regexp.MustCompile(regexp.QuoteMeta(filepath.Clean(os.TempDir())+string(os.PathSeparator)) + `Test[^\s]*?\d+[/\\]\d+[/\\]`), "<TMPDIR>/"},
}

func normalizeGolden(s string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		// The `$ aeon …` line is this file's own echo of the command it ran,
		// not a byte the CLI printed: it stays verbatim, because which instant
		// a case asked for (--at, --not-after) is part of the scenario.
		if strings.HasPrefix(line, "$ ") {
			b.WriteString(line)
			continue
		}
		for _, n := range goldenNormalizations {
			line = n.re.ReplaceAllString(line, n.repl)
		}
		b.WriteString(line)
	}
	return b.String()
}
