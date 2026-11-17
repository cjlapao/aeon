package main

// budget_verbs.go — S55 T11: the budget plane as CLI verbs. Thin clients of
// the socket's eight doors (budget.list/show/effective/set/remove/vocabulary,
// spend.report; loop.raise rides the loop verb) — the CLI re-validates
// nothing, and the help's closed sets are GENERATED from budget.vocabulary
// (rule 4: typed once, on the daemon, never a client copy that drifts when
// the set widens). A set or remove that loosens authority takes the shared
// --yes seatbelt: the daemon's statement is printed, and only with --yes is
// its digest committed (loosenGate).

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdBudget(w io.Writer, args []string) error {
	if len(args) == 0 || args[0] == "help" {
		// The usage's closed sets are the daemon's vocabulary, fetched live:
		// a set the CLI cannot name is a set it does not own (T8's guard).
		return budgetUsage(w)
	}
	switch args[0] {
	case "list":
		return cmdBudgetList(w, args[1:])
	case "show":
		return cmdBudgetShow(w, args[1:])
	case "effective":
		return cmdBudgetEffective(w, args[1:])
	case "set":
		return cmdBudgetSet(w, args[1:])
	case "remove":
		return cmdBudgetRemove(w, args[1:])
	case "spend":
		return cmdBudgetSpend(w, args[1:])
	default:
		printBudgetVerbLines(w)
		return fmt.Errorf("unknown `budget` subcommand %q", args[0])
	}
}

// printBudgetVerbLines is the static half of the usage: the verbs and their
// flags. The closed sets (scopes, windows, dimensions, unknown policies,
// statuses) are not here — they are budget.vocabulary's, printed only when a
// daemon answers them.
func printBudgetVerbLines(w io.Writer) {
	fmt.Fprint(w, `aeon budget — the budgets, and the spend against them

  budget list [--scope S] [--status S] [--json]
  budget show ID [--json]
  budget effective (--loop ID | --root ID | --component ID) [--json]
  budget set --scope S --subject ID --window W [--usd N] [--tokens N]
             [--wall DUR] [--steps N] [--unknown P] [--warn PCT]
             [--disabled] [--yes] [--json]
  budget remove ID --yes [--json]
  budget spend [--by G] [--since T] [--until T] [--root ID] [--json]

A set that RAISES or DISABLES an enabled cap, and every remove, loosen
authority: without --yes the daemon's statement is printed and nothing is
written; with --yes the digest of that statement commits the act. The values
for --scope, --window, --unknown and --status are the vocabulary's closed
sets (printed when a daemon is running); the daemon's own refusal names the
set --by takes.
`)
}

func budgetUsage(w io.Writer) error {
	v, err := budgetVocabulary()
	if err != nil {
		return err
	}
	printBudgetVerbLines(w)
	fmt.Fprintln(w, "Closed sets, from the daemon's vocabulary:")
	fmt.Fprintf(w, "  scopes           %s\n", strings.Join(v.Scopes, " | "))
	fmt.Fprintf(w, "  windows          %s\n", strings.Join(v.Windows, " | "))
	fmt.Fprintf(w, "  dimensions       %s\n", strings.Join(v.Dimensions, " | "))
	fmt.Fprintf(w, "  unknown policies %s\n", strings.Join(v.UnknownPolicies, " | "))
	fmt.Fprintf(w, "  statuses         %s\n", strings.Join(v.Statuses, " | "))
	return nil
}

// budgetVocabulary fetches the closed sets over the socket (want=0: the help
// is not a version probe). With no daemon the guard sentence is the answer.
func budgetVocabulary() (protocol.BudgetVocabularyResult, error) {
	c, err := dialControl(0, noDaemonGuardSentence)
	if err != nil {
		return protocol.BudgetVocabularyResult{}, err
	}
	defer c.Close()
	var v protocol.BudgetVocabularyResult
	if err := c.Call(protocol.MethodBudgetVocabulary, struct{}{}, &v); err != nil {
		return protocol.BudgetVocabularyResult{}, err
	}
	return v, nil
}

// budgetCaps renders a view's caps, or the honest absence: nil means NO CAP
// on the dimension (never zero, which is a real cap).
func budgetCaps(b protocol.BudgetView) string {
	caps := make([]string, 0, len(b.Dimensions))
	for _, d := range b.Dimensions {
		if d.Cap == nil {
			continue
		}
		caps = append(caps, fmt.Sprintf("%s %.6g", d.Dimension, *d.Cap))
	}
	if len(caps) == 0 {
		return "no caps"
	}
	return "caps " + strings.Join(caps, ", ")
}

func cmdBudgetList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("budget list", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "only this scope (the vocabulary names the set)")
	status := fs.String("status", "", "only this computed status (the vocabulary names the set)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.BudgetListParams{Scope: *scope, Status: *status}
	if *asJSON {
		return callJSON(w, c, protocol.MethodBudgetList, params)
	}
	var res protocol.BudgetListResult
	if err := c.Call(protocol.MethodBudgetList, params, &res); err != nil {
		return err
	}
	if len(res.Budgets) == 0 {
		fmt.Fprintln(w, "no budgets match")
		return nil
	}
	for _, b := range res.Budgets {
		fmt.Fprintf(w, "%s  %s/%s  %-8s  %-4s  %s\n",
			b.ID, b.Scope, b.SubjectID, b.Window, b.Status, budgetCaps(b))
	}
	return nil
}

func cmdBudgetShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("budget show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon budget show ID [--json] — `aeon budget list` names the budgets that exist")
	}
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.BudgetShowParams{ID: positional[0]}
	if *asJSON {
		return callJSON(w, c, protocol.MethodBudgetShow, params)
	}
	var res protocol.BudgetShowResult
	if err := c.Call(protocol.MethodBudgetShow, params, &res); err != nil {
		return err
	}
	b := res.View
	warn := "no warn line"
	if b.WarnPct != nil {
		warn = fmt.Sprintf("warn %d%%", *b.WarnPct)
	}
	fmt.Fprintf(w, "budget %s — %s/%s, %s, %s · enabled %s · unknown %s · %s · origin %s\n",
		b.ID, b.Scope, b.SubjectID, b.Window, b.Status, onOff(b.Enabled), b.UnknownPolicy, warn, b.Origin)
	for _, d := range b.Dimensions {
		capStr := "no cap"
		if d.Cap != nil {
			capStr = fmt.Sprintf("%.6g", *d.Cap)
		}
		fmt.Fprintf(w, "  %-9s cap %-12s spend floor ", d.Dimension, capStr)
		if d.SpendFloor == nil {
			fmt.Fprintf(w, "unknown (%d unknown)%s\n", d.UnknownCount, exactMark(d.Exact))
			continue
		}
		fmt.Fprintf(w, "%.6g (%d partial-unknown)%s\n", *d.SpendFloor, d.UnknownCount, exactMark(d.Exact))
	}
	s := res.Spend
	fmt.Fprintf(w, "spend: usd %.6g%s · wall %s · steps %d\n",
		s.USDFloor, exactMarkBool(s.Exact), (humanDuration(s.WallMs)), s.Steps)
	if s.Tokens != nil {
		fmt.Fprintf(w, "      tokens %d (%d unknown)\n", *s.Tokens, s.TokensUnknown)
	}
	if len(res.OpenLoops) > 0 {
		fmt.Fprintf(w, "open loops under it: %s\n", strings.Join(res.OpenLoops, ", "))
	}
	if res.OvershootNote != "" {
		fmt.Fprintln(w, res.OvershootNote)
	}
	for _, e := range res.History {
		fmt.Fprintf(w, "  %s %s by %s:%s\n", e.At, e.Kind, e.ActorKind, e.ActorID)
	}
	return nil
}

// exactMark renders P31's honesty mark: an empty string when the floor is a
// total, a marker when it is only a floor (partial or unknown spend beside it).
func exactMark(exact *bool) string {
	if exact == nil {
		return ""
	}
	if *exact {
		return " · exact"
	}
	return " · floor, not a total"
}

func exactMarkBool(exact bool) string {
	if exact {
		return " · exact"
	}
	return " · floor, not a total"
}

func humanDuration(ms int64) string {
	switch {
	case ms == 0:
		return "0s"
	default:
		return strconv.FormatInt(ms, 10) + "ms"
	}
}

func cmdBudgetEffective(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("budget effective", flag.ContinueOnError)
	fs.SetOutput(w)
	loopID := fs.String("loop", "", "the loop's id")
	rootID := fs.String("root", "", "the root's id")
	componentID := fs.String("component", "", "the component's id")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	named := 0
	for _, s := range []string{*loopID, *rootID, *componentID} {
		if strings.TrimSpace(s) != "" {
			named++
		}
	}
	if named == 0 {
		return fmt.Errorf("usage: aeon budget effective --loop ID | --root ID | --component ID [--json] — name the subject whose in-force budgets you want")
	}
	// More than one named is the daemon's refusal (it names the count); the
	// CLI's only job is to not guess a subject for the person.
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.BudgetEffectiveParams{LoopID: *loopID, RootID: *rootID, ComponentID: *componentID}
	if *asJSON {
		return callJSON(w, c, protocol.MethodBudgetEffective, params)
	}
	var res protocol.BudgetEffectiveResult
	if err := c.Call(protocol.MethodBudgetEffective, params, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "subject %s — %d budget(s) in force, tightest first:\n", res.Subject, len(res.Budgets))
	for _, b := range res.Budgets {
		fmt.Fprintf(w, "  %s  %s/%s  %-8s  %-4s  %s\n",
			b.ID, b.Scope, b.SubjectID, b.Window, b.Status, budgetCaps(b))
	}
	return nil
}

func cmdBudgetSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("budget set", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "required — the vocabulary names the set")
	subject := fs.String("subject", "", "required — the scope's subject id")
	window := fs.String("window", "", "required — the vocabulary names the set")
	usd := fs.Float64("usd", 0, "the usd cap")
	tokens := fs.Int64("tokens", 0, "the token cap")
	wall := fs.Duration("wall", 0, "the wall-time cap")
	steps := fs.Int64("steps", 0, "the step cap")
	unknown := fs.String("unknown", "", "how spend the daemon cannot measure is treated (the vocabulary names the set)")
	warn := fs.Int("warn", 0, "the warn line, 1–99")
	disabled := fs.Bool("disabled", false, "store the budget disabled (a loosening on an enabled row)")
	yes := fs.Bool("yes", false, "confirm a loosening (a seatbelt against a typo, not a lock)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{"--scope": *scope, "--subject": *subject, "--window": *window} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("`budget set` needs %s — the key (scope, subject, window) is the budget's identity", name)
		}
	}
	p := protocol.BudgetSetParams{Scope: *scope, SubjectID: *subject, Window: *window}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "usd":
			v := *usd
			p.USDCap = &v
		case "tokens":
			v := *tokens
			p.TokensCap = &v
		case "wall":
			v := wall.Milliseconds()
			p.WallMsCap = &v
		case "steps":
			v := *steps
			p.StepsCap = &v
		case "unknown":
			p.UnknownPolicy = *unknown
		case "warn":
			v := *warn
			p.WarnPct = &v
		case "disabled":
			v := *disabled
			p.Enabled = &v
		}
	})
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	attempt := func(confirm string) error {
		p.Confirm = confirm
		if *asJSON {
			return callJSON(w, c, protocol.MethodBudgetSet, p)
		}
		var res protocol.BudgetSetResult
		if err := c.Call(protocol.MethodBudgetSet, p, &res); err != nil {
			return err
		}
		if res.View != nil {
			fmt.Fprintf(w, "budget %s — %s/%s, %s: %s, %s\n",
				res.View.ID, res.View.Scope, res.View.SubjectID, res.View.Window, res.Effect, budgetCaps(*res.View))
		} else {
			fmt.Fprintf(w, "%s\n", res.Effect)
		}
		if res.Confirmed {
			fmt.Fprintln(w, "confirmed — the digest of the statement as read committed it")
		}
		return nil
	}
	if err := attempt(""); err != nil {
		return loosenGate(w, err, *yes, attempt)
	}
	return nil
}

func cmdBudgetRemove(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("budget remove", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the removal (a seatbelt against a typo, not a lock)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon budget remove ID --yes [--json] — `aeon budget list` names the budgets that exist")
	}
	id := positional[0]
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	attempt := func(confirm string) error {
		if *asJSON {
			return callJSON(w, c, protocol.MethodBudgetRemove, protocol.BudgetRemoveParams{ID: id, Confirm: confirm})
		}
		var res protocol.BudgetRemoveResult
		if err := c.Call(protocol.MethodBudgetRemove, protocol.BudgetRemoveParams{ID: id, Confirm: confirm}, &res); err != nil {
			return err
		}
		fmt.Fprintf(w, "removed budget %s — after this, no cap applies at its subject; the audit stays\n", res.ID)
		return nil
	}
	if err := attempt(""); err != nil {
		return loosenGate(w, err, *yes, attempt)
	}
	return nil
}

func cmdBudgetSpend(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("budget spend", flag.ContinueOnError)
	fs.SetOutput(w)
	by := fs.String("by", "day", "the report's group key (the daemon's refusal names the set)")
	since := fs.String("since", "", "RFC 3339 — the window's floor (default: the floor)")
	until := fs.String("until", "", "RFC 3339 — the window's ceiling (default: now)")
	root := fs.String("root", "", "scope the report to one project")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.SpendReportParams{GroupBy: *by, Since: *since, Until: *until, RootID: *root}
	if *asJSON {
		return callJSON(w, c, protocol.MethodSpendReport, params)
	}
	var res protocol.SpendReportResult
	if err := c.Call(protocol.MethodSpendReport, params, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "spend by %s", res.GroupBy)
	if res.Since != "" {
		fmt.Fprintf(w, " · since %s", res.Since)
	}
	if res.Until != "" {
		fmt.Fprintf(w, " · until %s", res.Until)
	}
	if res.RootID != "" {
		fmt.Fprintf(w, " · root %s", res.RootID)
	}
	fmt.Fprintln(w)
	if len(res.Buckets) == 0 {
		fmt.Fprintln(w, "no spend in the window")
		return nil
	}
	fmt.Fprintf(w, "%-24s %14s %14s %8s\n", "key", "usd (floor)", "tokens (floor)", "steps")
	for _, b := range res.Buckets {
		tokens := "—"
		if b.Tokens != nil {
			tokens = strconv.FormatInt(*b.Tokens, 10)
		}
		mark := " "
		if !b.Exact {
			mark = "+"
		}
		fmt.Fprintf(w, "%-24s %13.6g%s %14s %8d\n", b.Key, b.USDFloor, mark, tokens, b.Steps)
	}
	fmt.Fprintln(w, "a + marks a floor the daemon cannot call a total (partial or unknown spend beside it)")
	return nil
}
