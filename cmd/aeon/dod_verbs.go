package main

// dod_verbs.go — S57 T10 (VG20): `aeon dod` — the human's door onto the
// loop's Definition of Done. Two verbs: list (the items, their states,
// and the evidence each met item names) and mark (the human's own mark:
// evidence-bound, and the daemon — not the CLI — is what refuses an
// agent-flavoured mark, an unmet-bound met, or a state off the closed
// set, verbatim through the door it came from).

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdDoD(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "aeon dod: a verb is required — list · mark"}
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return runDoDList(w, rest)
	case "mark":
		return runDoDMark(w, rest)
	case "help", "-h", "--help":
		printDoDUsage(w)
		return nil
	default:
		return &usageError{msg: fmt.Sprintf("aeon dod: unknown verb %q — list · mark", verb)}
	}
}

func printDoDUsage(w io.Writer) {
	fmt.Fprintf(w, `aeon dod — the loop's Definition of Done (S57)

Verbs: list · mark

  list --loop L [--json]
  mark ITEM --state %s [--evidence-run ID | --manual [--note TEXT]] [--json]

The mark's closed set above is the daemon's (dod.mark checks it at the
door); this usage names it offline from the same Go constant.
`, strings.Join(protocol.DoDMarkStateValues, " | "))
}

func runDoDList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("dod list", flag.ContinueOnError)
	fs.SetOutput(w)
	loop := fs.String("loop", "", "the loop's slug (required)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *loop == "" {
		return &usageError{msg: "aeon dod list: --loop is required — the DoD belongs to a loop"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.DoDList(context.Background(), *loop)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "definition of done — loop %s\n", res.Loop)
	if len(res.Items) == 0 {
		fmt.Fprintln(w, "  (no items — the DoD materialises when a loop runs a brief with criteria)")
		return nil
	}
	for _, it := range res.Items {
		fmt.Fprintf(w, "  %2d  %s  %s\n", it.Idx, pad(it.State, 13), it.Text)
		if it.EvidenceKind != "" {
			fmt.Fprintf(w, "      evidence: %s %s  (%s/%s)\n", it.EvidenceKind, it.EvidenceRef,
				orEmpty(it.ActorKind), orEmpty(it.ActorID))
		}
	}
	return nil
}

func runDoDMark(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("dod mark", flag.ContinueOnError)
	fs.SetOutput(w)
	state := fs.String("state", "", "met | unmet | unverifiable (required)")
	evRun := fs.String("evidence-run", "", "a verify run's id is the evidence")
	manual := fs.Bool("manual", false, "the human is the evidence (a judgement, recorded as one)")
	note := fs.String("note", "", "note on the manual evidence (with --manual)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return &usageError{msg: "aeon dod mark: one argument is required — the item's id (aeon dod list --loop L lists them)"}
	}
	if *state == "" {
		return &usageError{msg: "aeon dod mark: --state is required (" + strings.Join(protocol.DoDMarkStateValues, " | ") + ")"}
	}
	// Flag-SHAPE refusals (the wire carries one evidence, so naming two
	// is an ambiguous request, not a daemon verdict). Whether the state
	// needs evidence at all, and whether the state exists, is the
	// daemon's refusal to print verbatim — the CLI checks nothing.
	var ev *protocol.DoDEvidence
	if *evRun != "" && *manual {
		return &usageError{msg: "aeon dod mark: --evidence-run and --manual pick two evidences — name one"}
	}
	switch {
	case *evRun != "":
		ev = &protocol.DoDEvidence{Kind: "run", Ref: *evRun}
	case *manual:
		ev = &protocol.DoDEvidence{Kind: "manual", Note: *note}
	case *note != "":
		return &usageError{msg: "aeon dod mark: --note belongs to --manual — a note without an evidence kind marks nothing"}
	}
	door, err := gateSelect()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.DoDMark(context.Background(), protocol.DoDMarkInput{
		ID: positional[0], State: *state, Evidence: ev,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, res)
	}
	it := res.Item
	fmt.Fprintf(w, "item %d marked %s: %s\n", it.Idx, it.State, it.Text)
	if it.EvidenceKind != "" {
		fmt.Fprintf(w, "  evidence: %s %s\n", it.EvidenceKind, it.EvidenceRef)
	}
	return nil
}

func orEmpty(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}
