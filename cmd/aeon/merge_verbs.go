package main

// merge_verbs.go — S57 T10 (VG20): `aeon merge` — the merge record's
// two reads at the terminal. Events are what the forge reported;
// watches are the daemon's promises to keep checking. Reads only — the
// merge itself is the daemon's and the forge's, never a CLI verb.

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdMerge(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "aeon merge: a verb is required — events · watches"}
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "events":
		return runMergeEvents(w, rest)
	case "watches":
		return runMergeWatches(w, rest)
	case "help", "-h", "--help":
		printMergeUsage(w)
		return nil
	default:
		return &usageError{msg: fmt.Sprintf("aeon merge: unknown verb %q — events · watches (the merge itself is the daemon's and the forge's)", verb)}
	}
}

func printMergeUsage(w io.Writer) {
	fmt.Fprint(w, `aeon merge — the merge record (S57)

Verbs: events · watches

  events [--component C] [--loop L] [--limit N] [--json]
  watches [--state S] [--json]
`)
}

func runMergeEvents(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge events", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "filter to one component")
	loop := fs.String("loop", "", "filter to one loop")
	limit := fs.Int("limit", 0, "most recent N (daemon default applies)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeEvents(context.Background(), protocol.MergeEventsQuery{
		ComponentID: *component, LoopSlug: *loop, Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	if len(res.Events) == 0 {
		fmt.Fprintln(w, "(no merge events match)")
		return nil
	}
	for _, e := range res.Events {
		fmt.Fprintf(w, "  PR #%d  %s  %s  %s\n", e.PR, e.Evidence, orDash(e.Method), e.MergedAt)
		fmt.Fprintf(w, "    loop %s   component %s   root %s   sha %s\n",
			first(e.LoopSlug, "-"), orDash(e.ComponentID), e.ComponentRoot, short(e.MergeSHA))
	}
	return nil
}

func runMergeWatches(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("merge watches", flag.ContinueOnError)
	fs.SetOutput(w)
	state := fs.String("state", "", "filter to one watch state")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.MergeWatches(context.Background(), protocol.MergeWatchesQuery{State: *state})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	if len(res.Watches) == 0 {
		fmt.Fprintln(w, "(no watches — nothing is waiting on a commit to land)")
		return nil
	}
	for _, wt := range res.Watches {
		fmt.Fprintf(w, "  %s  %s  %s @ %s  (%d checks)\n", pad(wt.State, 10), wt.ComponentID, wt.SHA[:min(12, len(wt.SHA))], wt.BaseRef, wt.Attempts)
		if wt.Detail != "" {
			fmt.Fprintf(w, "    %s\n", wt.Detail)
		}
	}
	return nil
}

func orDash(s *string, emptyAs ...string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
