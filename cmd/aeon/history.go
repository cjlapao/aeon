package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// The `aeon history` verb — the first concrete surface over internal/history
// (S25). A pure read: it opens the store (the justified second access path, as
// `policy` does) and reads through the read-only internal/history package.
//
// H3 (parity, proven here for the first of its two surfaces): `list` and
// `export` call ONE query — `historyQuery` — parameterized only by output
// format (a human table, NDJSON). Never a second query that happens to produce
// similar-looking output, because that is the second truth H3 exists to forbid.
// H6/P32: every cost renders through internal/history's `Rollup.Render`, a
// floor, never a bare number, and this verb adds no second cost formatter.
// D4/H4: a blocked loop's reason is `Entry.Reason`, read from the event stream
// verbatim, never a synthesized verdict.
//
// P34: this is the CLI, a secondary client. The app reads the same facts
// through the socket's `history.*` methods (S25 T4); the CLI and the socket
// read the same `internal/history`, so the two cannot drift.
//
// The verbs take an injectable *store.Store (the `policy` test idiom); the
// command opens it via openStoreDirectly. A test reaches the same query with a
// temp store, never the real one.

func cmdHistory(w io.Writer, args []string) error {
	if len(args) == 0 {
		printHistoryUsage(w)
		return nil
	}
	// S53 T4 (HV6, P5): the two verbs this slice adds are clients of the
	// socket's history.* methods, with the offline fallback — see
	// history_client.go. They are dispatched BEFORE the store opens because
	// the socket arm opens no store at all.
	switch args[0] {
	case "coverage":
		return historyCoverage(w, args[1:])
	case "vocabulary":
		return historyVocabulary(w, args[1:])
	}
	s, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer s.Close()
	switch args[0] {
	case "list":
		return historyList(s, w, args[1:])
	case "show":
		return historyShow(s, w, args[1:])
	case "export":
		return historyExport(s, w, args[1:])
	default:
		printHistoryUsage(w)
		return fmt.Errorf("unknown `history` subcommand %q", args[0])
	}
}

// historyCoverage — `aeon history coverage [--root R] [--component C]
// [--json]` (T4): the three honest counts and the floor statement, answered
// through the door (socket first, store direct only when nobody is home).
// The client computes NOTHING: no coverage, no lane, no word is
// re-derived here (HV1's law on this surface).
func historyCoverage(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("history coverage", flag.ContinueOnError)
	fs.SetOutput(w)
	var root, component string
	var asJSON bool
	fs.StringVar(&root, "root", "", "the component (project) root; empty spans every component")
	fs.StringVar(&component, "component", "", "a second name for the same scope key (componentRoot); refused together with --root")
	fs.BoolVar(&asJSON, "json", false, "print the view's wire shape instead of the table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := selectHistoryDoor(historyErrOut)
	if err != nil {
		return err
	}
	view, err := door.coverage(context.Background(), protocol.HistoryCoverageParams{
		ProjectRoot: root, ComponentRoot: component,
	})
	if err != nil {
		return err
	}
	if asJSON {
		return printHistoryJSON(w, view)
	}
	renderHistoryCoverage(w, view)
	return nil
}

// historyVocabulary — `aeon history vocabulary [--json]` (T4): the closed
// sets, verbatim from the plane's own function whichever door answered.
func historyVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("history vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "print the view's wire shape instead of the words")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := selectHistoryDoor(historyErrOut)
	if err != nil {
		return err
	}
	view, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	if asJSON {
		return printHistoryJSON(w, view)
	}
	renderHistoryVocabulary(w, view)
	return nil
}

// historyQuery is the ONE query both `list` and `export` call (H3): the same
// internal/history read, parameterized only by the flags. The only difference
// between the two verbs is the output format, not the query — so a cost or a
// reason cannot come out differently under `list` than under `export`. A
// second query here (one verb calling one read, the other another) would be a
// second truth that could drift from the view.
// historyQuery is the ONE query both `list` and `export` call (H3) — and the
// very same function the socket's history.* handlers call (S25 T4). It is a
// thin pass-through to internal/history.Query, so the CLI and the socket read
// through one read, parameterized only by the flags; the only difference
// between the two surfaces is the output, never the query.
func historyQuery(s *store.Store, opts history.Options, by history.GroupBy) ([]history.Entry, error) {
	return history.Query(s, opts, by)
}

// historyFlags is what both `list` and `export` parse — the identical flag set,
// so the two verbs can only differ by output format, not by query (H3).
type historyFlags struct {
	ComponentRoot string
	GroupBy       string
	Since         string
}

func parseHistoryFlags(fs *flag.FlagSet, args []string) (historyFlags, error) {
	var f historyFlags
	fs.StringVar(&f.ComponentRoot, "project-root", "", "the component (project) root; empty spans every component")
	fs.StringVar(&f.GroupBy, "group-by", "", "group by loop|phase|task|agent (default: loop)")
	fs.StringVar(&f.Since, "since", "", "an RFC3339 lower bound on the timeline")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	return f, nil
}

// queryOpts folds the parsed flags into the one query's options — the same
// fold for `list` and `export`, so the two verbs cannot query differently.
func (f historyFlags) options() history.Options {
	return history.Options{
		Scope: store.ComponentScope{ComponentRoot: f.ComponentRoot},
		Since: f.Since,
	}
}

// historyList — the human table. It calls the shared query and renders each
// row; the cost is the floored figure (H6) and the reason is verbatim (D4).
func historyList(s *store.Store, w io.Writer, args []string) error {
	fs := flag.NewFlagSet("history list", flag.ContinueOnError)
	fs.SetOutput(w)
	f, err := parseHistoryFlags(fs, args)
	if err != nil {
		return err
	}
	entries, err := historyQuery(s, f.options(), history.GroupBy(f.GroupBy))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		// The honest empty (P31): "nothing" is not "the store is broken."
		fmt.Fprintln(w, "no history — no live or archived loops in scope")
		return nil
	}
	fmt.Fprintln(w, "kind      id                    slug              status     cost")
	for _, e := range entries {
		row := rowOf(e)
		fmt.Fprintf(w, "%-9s %s  %-18s %-9s %s\n", row.Kind, row.ID, row.Slug, row.Status, row.Cost)
		if row.Reason != "" {
			// D4/H4: the stored reason, verbatim — never a synthesized verdict.
			fmt.Fprintf(w, "          blocked: %s\n", row.Reason)
		}
	}
	return nil
}

// historyExport — NDJSON, one object per line, the IDENTICAL query `list`
// uses (H3): same `historyQuery`, same options, the only difference is the
// row is a JSON object, not a table line. The cost is the floored figure
// rendered through the ONE formatter (H6); the reason is verbatim (D4).
func historyExport(s *store.Store, w io.Writer, args []string) error {
	fs := flag.NewFlagSet("history export", flag.ContinueOnError)
	fs.SetOutput(w)
	f, err := parseHistoryFlags(fs, args)
	if err != nil {
		return err
	}
	entries, err := historyQuery(s, f.options(), history.GroupBy(f.GroupBy))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for _, e := range entries {
		// The same row `list` builds — so export cannot carry a cost or a
		// reason `list` does not (H3's parity, asserted byte-for-byte).
		if err := enc.Encode(rowOf(e)); err != nil {
			return err
		}
	}
	return nil
}

// historyShow — the drill-down (T2), by an entry's id. It finds the entry in
// the shared query (so `show` reads the same facts `list` does, never a second
// query), then reads it through its own door: a live loop's transcript, an
// archived loop's cold-stored content on demand (Q3). A wrong entry is refused
// by name (DB19), not read as empty.
func historyShow(s *store.Store, w io.Writer, args []string) error {
	fs := flag.NewFlagSet("history show", flag.ContinueOnError)
	fs.SetOutput(w)
	var f historyFlags
	fs.StringVar(&f.ComponentRoot, "project-root", "", "the component (project) root; empty spans every component")
	fs.StringVar(&f.Since, "since", "", "an RFC3339 lower bound on the timeline")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("history show takes exactly one id, got %d", fs.NArg())
	}
	id := fs.Arg(0)

	entries, err := historyQuery(s, f.options(), history.ByLoop)
	if err != nil {
		return err
	}
	var entry *history.Entry
	for i := range entries {
		if entries[i].ID == id {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		// DB19: name the id that was not found, rather than reading it as empty.
		return fmt.Errorf("history: no loop or archive with id %q in scope", id)
	}

	if entry.Kind == history.KindLive {
		msgs, err := history.LiveTranscript(s, *entry)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			fmt.Fprintf(w, "%s  %s\n", string(m.Role), blockText(m))
		}
		return nil
	}

	archive, files, err := history.ArchivedTranscript(s, *entry)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "archive %s — %s (archived, cold-stored)\n", archive.ID, archive.Title)
	for _, af := range files {
		fmt.Fprintf(w, "  %s (%d bytes)\n", af.Path, af.Bytes)
	}
	return nil
}

// rowOf is a thin wrapper over the ONE shared row shape (H3): the CLI renders
// through internal/history.Row exactly as the socket's history.* handlers do
// (S25 T4). A second row shape here would be a second truth; the two surfaces
// share the conversion so a cost or a reason cannot come out differently.
func rowOf(e history.Entry) history.HistoryRow {
	return history.Row(e)
}

// blockText renders a message's text — the transcript's content, verbatim (the
// drill-down shows what was said, nothing synthesized). Only text blocks carry
// the words; thinking and tool blocks are not shown here (S25 does not decode
// the cold-stored state, Q3, and a transcript row is what a person reads).
func blockText(m model.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

func printHistoryUsage(w io.Writer) {
	fmt.Fprint(w, `aeon history — the time-ordered projection of everything a project has done (S25)

  history list   [--project-root ROOT] [--group-by loop|phase|task|agent] [--since RFC3339]
  history show   [--project-root ROOT] <loop-or-archive-id>   the drill-down: a live loop's
                                                              transcript, an archived loop's cold-stored content on demand
  history export [--project-root ROOT] [--group-by …] [--since …]
               NDJSON, one object per line — the identical query `+"`list`"+` uses, not a second one
  history coverage [--root R | --component C] [--json]
               what the view can and cannot speak of: shown, not shown (why), cannot say — and the cost floor
  history vocabulary [--json]
               the closed words: lanes, outcomes, filter keys — served by the plane, typed by nobody

  A cost is a floor, never a bare number ("at least $X (K of N steps unknown)");
  a blocked loop's reason is the stored reason, verbatim, never a synthesized verdict.
`)
}
