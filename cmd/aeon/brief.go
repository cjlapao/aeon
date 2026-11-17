package main

// brief.go — S38 T16: the loop's contract at the terminal. Reading a brief
// and judging one are the person's doors; WRITING one stays the
// brief-author's lane (there is deliberately no `brief create` here — a
// contract authored by its judge would be a contract with no author).

import (
	"flag"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdBrief(w io.Writer, args []string) error {
	if len(args) == 0 {
		printBriefUsage(w)
		return nil
	}
	switch args[0] {
	case "show":
		return cmdBriefShow(w, args[1:])
	case "decide":
		return cmdBriefDecide(w, args[1:])
	default:
		printBriefUsage(w)
		return fmt.Errorf("unknown `brief` subcommand %q", args[0])
	}
}

func printBriefUsage(w io.Writer) {
	fmt.Fprint(w, `aeon brief — a loop's contract, read and judged (S38)

  brief show --loop SLUG_OR_ID [--component ROOT]
  brief decide --loop SLUG_OR_ID --version N --decision approved|rejected
               [--reason R]

The brief is what the loop promised to do and what it promised NOT to do.
Two components can run the same slug, so --component is how a person names
WHICH one when the slug alone would be a guess. A brief re-issue never
overwrites: --version is the page being judged, and a second decision
answers who decided first rather than deciding twice.

There is no `+"`brief create`"+`: the contract is authored by the lane's
brief-author and judged by a person — the same hands cannot hold both ends.
`)
}

func cmdBriefShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("brief show", flag.ContinueOnError)
	fs.SetOutput(w)
	loop := fs.String("loop", "", "the loop — slug or id (required)")
	component := fs.String("component", "", "the component root, when the slug alone would be a guess")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *loop == "" {
		return &usageError{"`brief show` needs --loop — a person carries no loop, so the loop is said"}
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	params := map[string]any{"loop": *loop}
	if *component != "" {
		params["component"] = *component
	}
	var b struct {
		ID            string   `json:"id"`
		Version       int      `json:"version"`
		ReviewState   string   `json:"reviewState"`
		Scope         string   `json:"scope"`
		OutOfScope    string   `json:"outOfScope"`
		Acceptance    []string `json:"acceptance"`
		Constraints   []string `json:"constraints"`
		TestPlan      string   `json:"testPlan"`
		GoalID        string   `json:"goalId,omitempty"`
		GoalVersion   int      `json:"goalVersion,omitempty"`
		ComponentRoot string   `json:"componentRoot"`
		LoopSlug      string   `json:"loopSlug"`
	}
	if err := c.Call(protocol.MethodName("brief.show"), params, &b); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s  v%d  %s  (%s @ %s)\n", b.ID, b.Version, b.ReviewState, b.LoopSlug, b.ComponentRoot)
	if b.GoalID != "" {
		fmt.Fprintf(w, "  under goal %s v%d\n", b.GoalID, b.GoalVersion)
	}
	fmt.Fprintf(w, "  scope:  %s\n", b.Scope)
	fmt.Fprintf(w, "  out:    %s\n", b.OutOfScope)
	for _, a := range b.Acceptance {
		fmt.Fprintf(w, "  - %s\n", a)
	}
	for _, k := range b.Constraints {
		fmt.Fprintf(w, "  ! %s\n", k)
	}
	if b.TestPlan != "" {
		fmt.Fprintf(w, "  test plan: %s\n", b.TestPlan)
	}
	return nil
}

func cmdBriefDecide(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("brief decide", flag.ContinueOnError)
	fs.SetOutput(w)
	loop := fs.String("loop", "", "the loop — slug or id (required)")
	component := fs.String("component", "", "the component root, when the slug alone would be a guess")
	version := fs.Int("version", 0, "the brief VERSION being judged (required)")
	decision := fs.String("decision", "", "approved|rejected (required)")
	reason := fs.String("reason", "", "why — the sentence the ledger keeps")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *loop == "" {
		return &usageError{"`brief decide` needs --loop — the brief lives in a loop, name it"}
	}
	if *version == 0 {
		return &usageError{"`brief decide` needs --version — briefs are versioned per loop; a verdict names its page"}
	}
	if *decision != "approved" && *decision != "rejected" {
		return &usageError{"`brief decide` needs --decision approved|rejected — a contract is not parked, it is held or sent back"}
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var v protocol.DecideView
	if err := c.Call(protocol.MethodBriefDecide, protocol.BriefDecideParams{
		LoopID: *loop, Component: *component, Version: *version, Decision: *decision, Reason: *reason,
	}, &v); err != nil {
		return err
	}
	if !v.Changed {
		fmt.Fprintf(w, "already decided: %s by %s at %s — this command changed nothing\n", v.State, v.DecidedBy, v.DecidedAt)
		return nil
	}
	fmt.Fprintf(w, "decided: %s\n", v.State)
	return nil
}
