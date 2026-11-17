package main

// goal.go — S38 T16: the terminal's goal door (P34: the socket is primary,
// the CLI is its client). Every verb here is one c.Call away from what the
// app calls — no store import, no second rules, no logic beyond flag
// parsing and honest printing. What the step half stamps (project, author,
// source) the person SAYS, and the daemon refuses a said-nothing exactly as
// it refuses a smuggled-anything.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// usageError marks a flag-level refusal — a missing --version is not a
// failed decision, it is an unasked one, and main renders those as exit 2
// (the usage exit, not the error exit).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func cmdGoal(w io.Writer, args []string) error {
	if len(args) == 0 {
		printGoalUsage(w)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdGoalList(w, args[1:])
	case "show":
		return cmdGoalShow(w, args[1:])
	case "create":
		return cmdGoalCreate(w, args[1:])
	case "submit":
		return cmdGoalSubmit(w, args[1:])
	case "decide":
		return cmdGoalDecide(w, args[1:])
	default:
		printGoalUsage(w)
		return fmt.Errorf("unknown `goal` subcommand %q", args[0])
	}
}

func printGoalUsage(w io.Writer) {
	fmt.Fprint(w, `aeon goal — the org's intentions, decided by a person (S38)

  goal list [--root ID] [--state draft|in_review|approved|rejected|parked]
  goal show ID
  goal create --root NAME_OR_ID --title T --type TYPE [--component C[,C…]]
              [--body-file F] [--epic ID]
  goal submit ID
  goal decide ID --version N --decision approved|rejected|parked [--reason R]

A goal arrives IN_REVIEW the moment it is authored, by whoever authored it:
writing one and judging one are different people (OL11), and this terminal
can be either but never both in one command. `+"`goal show`"+` answers the
composed picture — reviews, attachments, the loops it drives and their
DERIVED state, and the money as a floor when any step's cost is only
bounded (P32). There is no `+"`goal delete`"+`: a decided intention is
history, not litter.
`)
}

func cmdGoalList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("goal list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only this root (id)")
	state := fs.String("state", "", "only this review state: draft|in_review|approved|rejected|parked")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var goals []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Type        string `json:"type"`
		Version     int    `json:"version"`
		ReviewState string `json:"reviewState"`
		RootID      string `json:"rootId"`
	}
	params := map[string]any{}
	if *root != "" {
		params["root_id"] = *root
	}
	if *state != "" {
		params["state"] = *state
	}
	if err := c.Call(protocol.MethodName("goal.list"), params, &goals); err != nil {
		return err
	}
	if len(goals) == 0 {
		fmt.Fprintln(w, "no goals — `aeon goal create --root NAME --title T --type TYPE` files one for a person to judge")
		return nil
	}
	for _, g := range goals {
		fmt.Fprintf(w, "%-22s v%-2d %-10s %-12s %s\n", g.ID, g.Version, g.ReviewState, g.Type, g.Title)
	}
	return nil
}

func cmdGoalShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("goal show", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	positional := fs.Args()
	if len(positional) != 1 {
		return fmt.Errorf("`goal show` takes exactly one goal id")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.GoalShowResult
	if err := c.Call(protocol.MethodGoalShow, protocol.GoalShowParams{ID: positional[0]}, &res); err != nil {
		return err
	}
	g := res.Goal
	fmt.Fprintf(w, "%s  v%d  [%s]  %s\n", g.ID, g.Version, g.ReviewState, g.Type)
	fmt.Fprintf(w, "  %s\n", g.Title)
	if g.BodyNote != "" {
		fmt.Fprintf(w, "  %s\n", g.BodyNote)
	}
	fmt.Fprintf(w, "  execution: %s", res.ExecutionState)
	if res.Cost.USD > 0 || res.Cost.IsFloor {
		fmt.Fprintf(w, "  ·  cost $%.4f", res.Cost.USD)
		if res.Cost.IsFloor {
			fmt.Fprint(w, " (floor — some step is unpriced)")
		}
	}
	fmt.Fprintln(w)
	if len(res.Components) > 0 {
		fmt.Fprintln(w, "  components:")
		for _, comp := range res.Components {
			conf := ""
			if comp.Confidence != nil {
				conf = fmt.Sprintf(" confidence %d/5", *comp.Confidence)
			}
			fmt.Fprintf(w, "    %-22s%s   %s\n", comp.ComponentID, conf, comp.Note)
		}
	}
	if len(res.Loops) > 0 {
		fmt.Fprintln(w, "  loops:")
		for _, l := range res.Loops {
			bp := ""
			if l.Blueprint != "" {
				bp = " · " + l.Blueprint
			}
			fmt.Fprintf(w, "    %-16s %-10s %s%s\n", l.Slug, l.Status, l.ComponentRoot, bp)
		}
	}
	if len(res.Attachments) > 0 {
		fmt.Fprintln(w, "  attachments:")
		for _, a := range res.Attachments {
			fmt.Fprintf(w, "    %s  rev %d/%d  %s\n", a.Document, a.Rev, a.HeadRev, a.Currency)
		}
	}
	if len(res.Reviews) > 0 {
		fmt.Fprintln(w, "  ledger:")
		for _, r := range res.Reviews {
			fmt.Fprintf(w, "    v%d  %-9s %s/%s  %s\n", r.SubjectVersion, r.Decision, r.ByKind, r.ByID, r.Reason)
		}
	} else {
		fmt.Fprintln(w, "  nobody has judged it yet — `aeon goal decide` is the person's door")
	}
	return nil
}

func cmdGoalCreate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("goal create", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "the root — id or exact name (required)")
	title := fs.String("title", "", "what the goal is for (required)")
	goalType := fs.String("type", "", "one of the store's five (required)")
	components := fs.String("component", "", "comma list of components the goal stakes — root, name, slug or id")
	bodyFile := fs.String("body-file", "", "a file holding the body note — the prose stays the author's")
	epic := fs.String("epic", "", "an epic this goal belongs to")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" || *title == "" || *goalType == "" {
		return &usageError{"`goal create` needs --root, --title and --type — the daemon refuses to guess any of them"}
	}
	body := ""
	if *bodyFile != "" {
		raw, err := os.ReadFile(*bodyFile)
		if err != nil {
			return fmt.Errorf("read the body from %s: %w", *bodyFile, err)
		}
		body = strings.TrimSpace(string(raw))
	}
	params := map[string]any{
		"root": *root, "title": *title, "type": *goalType, "body_note": body,
	}
	if *epic != "" {
		params["epic_id"] = *epic
	}
	if *components != "" {
		var comps []map[string]any
		for _, name := range strings.Split(*components, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				comps = append(comps, map[string]any{"component": name})
			}
		}
		params["components"] = comps
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res struct {
		Goal struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Version     int    `json:"version"`
			ReviewState string `json:"reviewState"`
		} `json:"goal"`
	}
	if err := c.Call(protocol.MethodName("goal.create"), params, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%-22s v%d  %s  %s\n", res.Goal.ID, res.Goal.Version, res.Goal.ReviewState, res.Goal.Title)
	fmt.Fprint(w, "filed for a person's judgement — `aeon goal decide` is that door\n")
	return nil
}

func cmdGoalSubmit(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("goal submit", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	positional := fs.Args()
	if len(positional) != 1 {
		return fmt.Errorf("`goal submit` takes exactly one goal id")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var goal struct {
		ID          string `json:"id"`
		ReviewState string `json:"reviewState"`
	}
	if err := c.Call(protocol.MethodName("goal.submit_review"), map[string]any{"goal_id": positional[0]}, &goal); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s  %s\n", goal.ID, goal.ReviewState)
	return nil
}

func cmdGoalDecide(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("goal decide", flag.ContinueOnError)
	fs.SetOutput(w)
	version := fs.Int("version", 0, "the goal VERSION being judged (required — a verdict that does not name its version judges the wrong page)")
	decision := fs.String("decision", "", "approved|rejected|parked (required)")
	reason := fs.String("reason", "", "why — the sentence the ledger keeps beside the verdict")
	want := addProtocolVersionFlag(fs)
	// The id LEADS and the flags follow — `aeon goal decide ID --version 1`
	// is what a hand types; the flag package stops at the first positional,
	// so the leading id is taken before Parse rather than losing the flags
	// behind it.
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" || fs.NArg() != 0 {
		return &usageError{"`goal decide` takes exactly one goal id, then its flags"}
	}
	if *version == 0 {
		return &usageError{"`goal decide` needs --version — decisions address (goal, version), never a goal at large"}
	}
	if *decision != "approved" && *decision != "rejected" && *decision != "parked" {
		return &usageError{"`goal decide` needs --decision approved|rejected|parked"}
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var v protocol.DecideView
	if err := c.Call(protocol.MethodGoalDecide, protocol.GoalDecideParams{
		ID: id, Version: *version, Decision: *decision, Reason: *reason,
	}, &v); err != nil {
		return err
	}
	if !v.Changed {
		fmt.Fprintf(w, "already decided: %s by %s/%s at %s — this command changed nothing\n", v.State, "user", v.DecidedBy, v.DecidedAt)
		return nil
	}
	fmt.Fprintf(w, "decided: %s\n", v.State)
	return nil
}
