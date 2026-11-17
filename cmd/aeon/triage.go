package main

// The `aeon triage` verb — the bugs a project files BEFORE they become
// roadmap work (S29). This is a CLIENT of the daemon's `triage.*` methods
// and nothing else: it dials the same socket the app speaks (P34's ordering,
// CG9's one implementation underneath — the CLI is the secondary client),
// opens no store, and re-validates no law. The four-state machine (D1),
// D2's three-state priority, D5's suggest-only dedup and D8's hand-off are
// decided once, underneath, at the door and the CHECK; the sentences of
// refusal ride out of here verbatim.
//
// What this surface owns: argument shape and the honest print — validation
// and priority are ALWAYS their own words (unvalidated says "unvalidated",
// unknown says "unknown", never blank, never a combined badge — R4's guard,
// two fields the renderer renders apart), and an empty backlog says so.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/triage"
)

func cmdTriage(w io.Writer, args []string) error {
	if len(args) == 0 {
		printTriageUsage(w)
		return nil
	}
	switch args[0] {
	case "file":
		return cmdTriageFile(w, args[1:])
	case "list":
		return cmdTriageList(w, args[1:])
	case "show":
		return cmdTriageShow(w, args[1:])
	case "set-priority":
		return cmdTriageSetPriority(w, args[1:])
	case "confirm-duplicate":
		return cmdTriageConfirmDuplicate(w, args[1:])
	case "validate":
		return cmdTriageValidate(w, args[1:])
	case "handoff":
		return cmdTriageHandoff(w, args[1:])
	case "suggest-duplicates":
		return cmdTriageSuggest(w, args[1:])
	case "update":
		return cmdTriageUpdate(w, args[1:])
	case "history":
		return cmdTriageHistory(w, args[1:])
	case "board":
		return cmdTriageBoard(w, args[1:])
	case "health":
		return cmdTriageHealth(w, args[1:])
	case "vocabulary":
		return cmdTriageVocabulary(w, args[1:])
	default:
		printTriageUsage(w)
		return fmt.Errorf("unknown `triage` subcommand %q", args[0])
	}
}

func printTriageUsage(w io.Writer) {
	fmt.Fprint(w, `aeon triage — the bugs before they become roadmap work (S29)

  triage file --title TEXT --body TEXT
              [--priority known|assumed|unknown] [--priority-label L]
              [--component ROOT]
              the self-file door (D7): validation arrives unvalidated, the
              schema carries no state from the model; a priority a person
              decided may ride. There is no --filed-by (S51 TM3): the daemon
              derives the filer from the peer on the socket — you file as
              'person', an agent's twin files under its own slug.
  triage list [--component ROOT] [--validation S] [--priority P]
  triage show ID
  triage set-priority ID --state known|assumed|unknown [--label L]
  triage confirm-duplicate ID --of ID --confirmed-by PERSON
  triage validate ID            start the validation attempt (T3, the daemon
                                dispatches an ordinary loop; the bug stays
                                unvalidated until it finishes)
  triage handoff ID             hand off a validated, prioritized bug to the
                                roadmap (T6, D8)
  triage suggest-duplicates ID  nearest-neighbour duplicates within the bug's
                                OWN project (T4, a read — never a write)
  triage update ID [--title T] [--body B]
                                TM1's narrow edit: title and body, nothing
                                else; state, priority, links and hand-off
                                are verbs with their own doors
  triage history ID [--limit N] the bug_change audit, newest first — an
                                empty history is a bug nothing has been
                                done to, never a bug that is absent
  triage board [--component ROOT] [--validation S] [--priority P]
               [--handed-off] [--json]
                                attempts, SCORED candidates and hand-off
                                status per bug; the two state sets print
                                as SEPARATE sentences, never one badge
  triage health                 the validation blueprint's standing right
                                now — advice, never a gate (TM4)
  triage vocabulary             the closed sets from their owners, and the
                                two authority statements (TM9)

  validate, handoff and confirm-duplicate are a person's acts: they print
  the daemon's authority statement and act only on an explicit --yes
  (without it they print or write NOTHING, S51 TM9). update, history,
  board, health and vocabulary answer the socket, and — with no daemon
  running — the same answers over the store directly, saying so first.

  validation is a four-state machine (D1): unvalidated → reproduced |
  not-reproduced | could-not-tell. unvalidated never collapses into
  could-not-tell. priority is a SEPARATE three-state set (D2), unknown is
  the honest absence. the two are never one combined badge (R4).
`)
}

func printTriageBug(w io.Writer, b protocol.TriageView) {
	fmt.Fprintf(w, "#%d · %s · %s · %s", b.ID, b.Validation, b.PriorityState, b.Title)
	if b.PriorityLabel != "" {
		fmt.Fprintf(w, " (%s)", b.PriorityLabel)
	}
	fmt.Fprintf(w, " · filed by %s", b.FiledBy)
	if b.DuplicateOf != nil {
		fmt.Fprintf(w, " · dup of #%d", *b.DuplicateOf)
	}
	if b.RoadmapItemID != nil {
		fmt.Fprintf(w, " · → roadmap #%d", *b.RoadmapItemID)
	}
	fmt.Fprintln(w)
}

func cmdTriageFile(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage file", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	title := fs.String("title", "", "what the bug is — required (D7: the self-file)")
	body := fs.String("body", "", "how to reproduce it — required")
	priority := fs.String("priority", "", "known | assumed | unknown — a person's decision; the model path may not (D2)")
	label := fs.String("priority-label", "", "the label that MEANS something; carried only when the state is not unknown")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" || strings.TrimSpace(*body) == "" {
		return &usageError{"triage file needs --title and --body — the door carries no validation state (the store's default is the backstop, D7)"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.TriageView
	if err := c.Call(protocol.MethodTriageFile, protocol.TriageFileParams{
		ComponentRoot: *root, Title: *title, Body: *body,
		PriorityState: *priority, PriorityLabel: *label,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · filed, validation UNVALIDATED (the attempt has not run; the bug arrives unvalidated, D1/D7) · ", res.ID)
	printTriageBug(w, res)
	return nil
}

func cmdTriageList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	validation := fs.String("validation", "", "filter: only this validation state (unvalidated|reproduced|not-reproduced|could-not-tell)")
	priority := fs.String("priority", "", "filter: only this priority state (known|assumed|unknown)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res []protocol.TriageView
	if err := c.Call(protocol.MethodTriageList, protocol.TriageListParams{
		ComponentRoot: *root, ValidationState: *validation, PriorityState: *priority,
	}, &res); err != nil {
		return err
	}
	if len(res) == 0 {
		fmt.Fprintf(w, "no bugs — the backlog of %s is EMPTY (a filter that matches nothing is the filter's answer, not the store's)\n", *root)
		return nil
	}
	for _, b := range res {
		printTriageBug(w, b)
	}
	return nil
}

func cmdTriageShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage show", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage show ID needs a positive id"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.TriageView
	if err := c.Call(protocol.MethodTriageShow, protocol.TriageShowParams{ID: id}, &res); err != nil {
		return err
	}
	printTriageBug(w, res)
	if res.Body != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, res.Body)
	}
	return nil
}

func cmdTriageSetPriority(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage set-priority", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	state := fs.String("state", "", "known | assumed | unknown — the three-state word (D2), never a number")
	label := fs.String("label", "", "the label that MEANS something; carried only when the state is not unknown")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage set-priority ID needs a positive id"}
	}
	if *state == "" {
		return &usageError{"triage set-priority needs --state known|assumed|unknown (D2)"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.TriageView
	if err := c.Call(protocol.MethodTriageSetPriority, protocol.TriageSetPriorityParams{
		ID: id, ComponentRoot: *root, State: *state, Label: *label,
	}, &res); err != nil {
		return err
	}
	printTriageBug(w, res)
	return nil
}

func cmdTriageConfirmDuplicate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage confirm-duplicate", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	of := fs.Int64("of", 0, "the id of the bug this one is a duplicate of — required (D5)")
	confirmedBy := fs.String("confirmed-by", "", "a person — required; a machine confirmer is refused (D5/N3)")
	yes := fs.Bool("yes", false, "confirm the link — a duplicate is a human's irreversible act; without --yes this verb writes NOTHING (S51 TM9)")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage confirm-duplicate ID needs a positive id"}
	}
	if *of <= 0 {
		return &usageError{"triage confirm-duplicate needs --of ID — the bug it is a duplicate of"}
	}
	if strings.TrimSpace(*confirmedBy) == "" {
		return &usageError{"triage confirm-duplicate needs --confirmed-by PERSON — a duplicate becomes real only from a person (D5/N3)"}
	}
	if !*yes {
		return &usageError{fmt.Sprintf("triage confirm-duplicate %d → %d is a person's irreversible link — re-run with --yes to confirm it (S51 TM9)", id, *of)}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.TriageView
	if err := c.Call(protocol.MethodTriageConfirmDuplicate, protocol.TriageConfirmDuplicateParams{
		ID: id, ComponentRoot: *root, Of: *of, ConfirmedBy: *confirmedBy,
	}, &res); err != nil {
		return err
	}
	printTriageBug(w, res)
	return nil
}

func cmdTriageValidate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage validate", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the attempt after reading its authority statement — without --yes this verb prints and writes NOTHING (S51 TM9)")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage validate ID needs a positive id"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	if err := triageAuthorityGate(w, c, "validate", id, *yes); err != nil {
		return err
	}
	var res map[string]any
	if err := c.Call(protocol.MethodTriageValidate, protocol.TriageValidateParams{ID: id}, &res); err != nil {
		return err
	}
	if note, ok := res["note"].(string); ok {
		fmt.Fprintln(w, note)
	}
	if loopID, ok := res["loopId"].(string); ok {
		fmt.Fprintf(w, "validation attempt loop: %s (the bug stays unvalidated until it finishes; the reactor moves it)\n", loopID)
	}
	return nil
}

func cmdTriageHandoff(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage handoff", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the hand-off after reading its authority statement — without --yes this verb prints and writes NOTHING (S51 TM9)")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage handoff ID needs a positive id"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	if err := triageAuthorityGate(w, c, "handoff", id, *yes); err != nil {
		return err
	}
	var res map[string]any
	if err := c.Call(protocol.MethodTriageHandoff, protocol.TriageHandoffParams{ID: id}, &res); err != nil {
		return err
	}
	if itemID, ok := res["roadmapItem"].(int64); ok {
		fmt.Fprintf(w, "handed off: bug #%d → roadmap item #%d (provenance triaged, D8; S26's own door, zero changes to it)\n", id, itemID)
		return nil
	}
	fmt.Fprintln(w, "handed off")
	return nil
}

func cmdTriageSuggest(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage suggest-duplicates", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Int("limit", 0, "max candidates (the daemon's default if unset)")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage suggest-duplicates ID needs a positive id"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res []protocol.TriageView
	if err := c.Call(protocol.MethodTriageSuggest, protocol.TriageSuggestParams{ID: id}, &res); err != nil {
		return err
	}
	if len(res) == 0 {
		fmt.Fprintf(w, "no candidates within the bug's own project — the detector suggests, it never sets duplicate_of (D5/N3); a human confirms\n")
		return nil
	}
	fmt.Fprintln(w, "candidates (a suggestion only — duplicate_of is set only by a human confirmation, D5/N3):")
	for _, b := range res {
		printTriageBug(w, b)
	}
	return nil
}

// triageAuthorityGate is the TM9 gate for the two enabling verbs: it
// prints the daemon's OWN authority statement (the vocabulary carries
// it, one string, so CLI and UI quote the same words — S50 RM9's law)
// and the bug's own daemon-rendered line, then refuses without --yes.
// The bug's Describe rides as the printTriageBug line — validation and
// priority as their own words, the duplicate and hand-off links included
// — never as a second renderer's paraphrase; the full board-row sentences
// stay on the board, whose shape the daemon owns (deviation recorded in
// the task Notes: the SHOW fixture S29 owns stays byte-intact).
func triageAuthorityGate(w io.Writer, c *protocol.Client, verb string, id int64, yes bool) error {
	var view protocol.TriageView
	if err := c.Call(protocol.MethodTriageShow, protocol.TriageShowParams{ID: id}, &view); err != nil {
		return triageVerbatim(err)
	}
	var voc triage.Vocabulary
	if err := c.Call(protocol.MethodTriageVocabulary, struct{}{}, &voc); err != nil {
		return triageVerbatim(err)
	}
	switch verb {
	case "validate":
		fmt.Fprintln(w, voc.ValidateStatement)
	default:
		fmt.Fprintln(w, voc.HandoffStatement)
	}
	printTriageBug(w, view)
	if !yes {
		return &usageError{fmt.Sprintf("triage %s %d is a person's act — re-run with --yes once you have read what it starts above", verb, id)}
	}
	return nil
}

func cmdTriageUpdate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage update", flag.ContinueOnError)
	fs.SetOutput(w)
	title := fs.String("title", "", "the new title — an empty one is refused verbatim (D6 law)")
	body := fs.String("body", "", "the new body")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage update ID needs a positive id"}
	}
	// Absent and empty are different intentions (TM1's pointers): only a
	// flag that was SET travels. The door, not this client, decides what
	// an edit may touch and what its words refuse.
	var setT, setB bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			setT = true
		case "body":
			setB = true
		}
	})
	p := protocol.TriageUpdateParams{ID: id}
	if setT {
		v := *title
		p.Title = &v
	}
	if setB {
		v := *body
		p.Body = &v
	}
	door, err := selectTriageDoor(triageErrOut)
	if err != nil {
		return err
	}
	res, err := door.update(context.Background(), triageCLIActor, p)
	if err != nil {
		return err
	}
	if !res.Changed {
		fmt.Fprintln(w, "unchanged — the edit named nothing that differs (TM1; no audit row for a bug that did not move)")
	}
	if res.Bug != nil {
		printTriageBug(w, *res.Bug)
	} else {
		fmt.Fprintf(w, "#%d · the edit answered without a bug view — say so and file it\n", id)
	}
	return nil
}

func cmdTriageHistory(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage history", flag.ContinueOnError)
	fs.SetOutput(w)
	limit := fs.Int("limit", 0, "max rows (the door's default and cap if unset)")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id <= 0 {
		return &usageError{"triage history ID needs a positive id"}
	}
	door, err := selectTriageDoor(triageErrOut)
	if err != nil {
		return err
	}
	rows, err := door.history(context.Background(), protocol.TriageHistoryParams{ID: id, Limit: *limit})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintf(w, "#%d · no audit rows — an empty history is a bug nothing has been done to, not a bug that is absent (a bug that never was answers its own refusal)\n", id)
		return nil
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%s · %s · %s:%s · %s\n", r.OccurredAt, r.Op, r.ActorKind, r.ActorID, r.Summary)
	}
	return nil
}

func cmdTriageBoard(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage board", flag.ContinueOnError)
	root := componentFlag(fs)
	validation := fs.String("validation", "", "filter: one of the four (triage.vocabulary answers the set; any other word is refused by name)")
	priority := fs.String("priority", "", "filter: one of the three (the closed sets are refused by word, never coerced)")
	handedOff := fs.Bool("handed-off", false, "filter: only handed-off (set the flag) or only not (set it to false); unset is any")
	asJSON := fs.Bool("json", false, "print the door's JSON answer and nothing else")
	addProtocolVersionFlag(fs)
	id, rest := peelTriageID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id != 0 {
		return &usageError{"triage board takes no id — it answers the backlog, not a bug"}
	}
	p := protocol.TriageBoardParams{ComponentRoot: *root, Validation: *validation, Priority: *priority}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "handed-off" {
			v := *handedOff
			p.HandedOff = &v
		}
	})
	door, err := selectTriageDoor(triageErrOut)
	if err != nil {
		return err
	}
	rows, err := door.board(context.Background(), p)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintf(w, "the board of %s is EMPTY (a filter that matches nothing is the filter's answer, not the store's)\n", *root)
		return nil
	}
	for _, row := range rows {
		if row.Bug != nil {
			printTriageBug(w, *row.Bug)
		}
		// The two state sets, as SEPARATE sentences, from the daemon's
		// own Describe — never one combined badge (R4, TM5).
		fmt.Fprintf(w, "    validation: %s\n", row.Describe.ValidationSentence)
		fmt.Fprintf(w, "    priority:   %s\n", row.Describe.PrioritySentence)
		if row.Attempts > 0 {
			fmt.Fprintf(w, "    attempts: %d (last: %s)\n", row.Attempts, row.LastAttemptAt)
		} else {
			fmt.Fprintln(w, "    attempts: 0 — no validation attempt has been AUDITED for this bug (the digest counts audit rows, never a column's age)")
		}
		for _, c := range row.Candidates {
			fmt.Fprintf(w, "    candidate: #%d · %s · distance %.4f (a suggestion only — D5/N3)\n", c.Bug.ID, c.Bug.Title, c.Distance)
		}
		if row.RoadmapStatus != "" {
			fmt.Fprintf(w, "    hand-off: roadmap item status %s (where the work is NOW, not where it was left, D8)\n", row.RoadmapStatus)
		}
	}
	return nil
}

func cmdTriageHealth(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage health", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"triage health takes no args — it answers over the setting's standing (TM4)"}
	}
	door, err := selectTriageDoor(triageErrOut)
	if err != nil {
		return err
	}
	res, err := door.health(context.Background())
	if err != nil {
		return err
	}
	for _, st := range res.Settings {
		fmt.Fprintf(w, "%s · %s · %s\n", st.Key, st.Name, st.State)
		if st.ReportCounts != nil {
			fmt.Fprintf(w, "    blueprints: %d valid · %d invalid · %d unverifiable\n",
				st.ReportCounts.Valid, st.ReportCounts.Invalid, st.ReportCounts.Unverifiable)
		}
	}
	fmt.Fprintf(w, "evaluated %s · health is ADVICE, never a gate — a green line and a refused attempt can honestly stand one second apart (TM4)\n", res.EvaluatedAt)
	return nil
}

func cmdTriageVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("triage vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"triage vocabulary takes no args — the closed sets have no filters"}
	}
	door, err := selectTriageDoor(triageErrOut)
	if err != nil {
		return err
	}
	voc, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "validation states (four, D1): %s\n", strings.Join(voc.ValidationStates, " · "))
	for _, from := range voc.ValidationStates {
		next := voc.LegalNext[from]
		if len(next) == 0 {
			fmt.Fprintf(w, "    from %s: terminal — a landed outcome does not move again (D1)\n", from)
			continue
		}
		fmt.Fprintf(w, "    from %s: %s\n", from, strings.Join(next, " | "))
	}
	fmt.Fprintf(w, "priority states (three, D2 — a SEPARATE set; the two are never one combined badge, R4): %s\n", strings.Join(voc.PriorityStates, " · "))
	fmt.Fprintf(w, "validate: %s\n", voc.ValidateStatement)
	fmt.Fprintf(w, "handoff:  %s\n", voc.HandoffStatement)
	return nil
}

// peelTriageID takes the leading positional id (the queue/roadmap verb's
// house shape: the id first, then flags).
func peelTriageID(args []string) (int64, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, args
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, args
	}
	return id, args[1:]
}
