package main

// watchdog_verbs.go — S55 T11: the watchdog plane as CLI verbs. Thin clients
// of the socket's six doors (watchdog.list/show/vocabulary/set/setEnabled/
// remove) — the CLI re-validates nothing. A create and an enable TIGHTEN a
// safeguard and commit directly; a disable and a remove LOOSEN one, and take
// the shared --yes seatbelt (loosenGate): the daemon's statement is printed,
// and only with --yes is its digest committed. The usage's closed sets —
// scopes, signals WITH their unit and availability, actions — are generated
// from watchdog.vocabulary (rule 4: typed once, on the daemon, never a
// client copy).

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdWatchdog(w io.Writer, args []string) error {
	if len(args) == 0 {
		// The usage's closed sets are the daemon's vocabulary, fetched live.
		return watchdogUsage(w)
	}
	switch args[0] {
	case "list":
		return cmdWatchdogList(w, args[1:])
	case "show":
		return cmdWatchdogShow(w, args[1:])
	case "vocabulary":
		return cmdWatchdogVocabulary(w, args[1:])
	case "set":
		return cmdWatchdogSet(w, args[1:])
	case "enable":
		return cmdWatchdogSetEnabled(w, args[1:], true)
	case "disable":
		return cmdWatchdogSetEnabled(w, args[1:], false)
	case "remove":
		return cmdWatchdogRemove(w, args[1:])
	default:
		printWatchdogVerbLines(w)
		return fmt.Errorf("unknown `watchdog` subcommand %q", args[0])
	}
}

// printWatchdogVerbLines is the static half of the usage: the verbs and their
// flags. The closed sets (scopes, signals with unit and availability,
// actions) are not here — they are watchdog.vocabulary's.
func printWatchdogVerbLines(w io.Writer) {
	fmt.Fprint(w, `aeon watchdog — the rules that fire on a signal and move a loop

  watchdog list [--scope S] [--subject ID] [--json]
  watchdog show ID [--json]
  watchdog vocabulary [--json]
  watchdog set --scope S --subject ID --signal SIG --threshold N --action A
             [--disabled] [--yes] [--json]
  watchdog enable ID [--json]
  watchdog disable ID --yes [--json]
  watchdog remove ID --yes [--json]

A disable and a remove loosen a safeguard: without --yes the daemon's
statement is printed and nothing is written; with --yes the digest of that
statement commits the act. A create and an enable tighten, and commit
directly. The values for --scope, --signal and --action are the vocabulary's
closed sets (printed when a daemon is running); --threshold is in the
signal's own unit, which the vocabulary names.
`)
}

func watchdogUsage(w io.Writer) error {
	v, err := watchdogVocabulary()
	if err != nil {
		return err
	}
	printWatchdogVerbLines(w)
	printWatchdogVocabulary(w, v)
	return nil
}

// watchdogVocabulary fetches the closed sets over the socket (want=0: the
// help is not a version probe). With no daemon the guard sentence is the
// answer.
func watchdogVocabulary() (protocol.WatchdogVocabularyResult, error) {
	c, err := dialControl(0, noDaemonGuardSentence)
	if err != nil {
		return protocol.WatchdogVocabularyResult{}, err
	}
	defer c.Close()
	var v protocol.WatchdogVocabularyResult
	if err := c.Call(protocol.MethodWatchdogVocabulary, struct{}{}, &v); err != nil {
		return protocol.WatchdogVocabularyResult{}, err
	}
	return v, nil
}

// printWatchdogVocabulary is the generated half of the usage, and the body of
// the explicit `watchdog vocabulary` verb: one renderer, so the help and the
// verb cannot drift apart.
func printWatchdogVocabulary(w io.Writer, v protocol.WatchdogVocabularyResult) {
	fmt.Fprintln(w, "Closed sets, from the daemon's vocabulary:")
	fmt.Fprintf(w, "  scopes   %s\n", strings.Join(v.Scopes, " | "))
	signals := make([]string, 0, len(v.Signals))
	for _, s := range v.Signals {
		avail := "available"
		if !s.Available {
			avail = "unavailable"
			if s.Reason != "" {
				avail += ": " + s.Reason
			}
		}
		signals = append(signals, fmt.Sprintf("%s (%s, %s)", s.Signal, s.Unit, avail))
	}
	fmt.Fprintf(w, "  signals  %s\n", strings.Join(signals, " | "))
	fmt.Fprintf(w, "  actions  %s\n", strings.Join(v.Actions, " | "))
}

func cmdWatchdogVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("watchdog vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
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
	if *asJSON {
		return callJSON(w, c, protocol.MethodWatchdogVocabulary, struct{}{})
	}
	var v protocol.WatchdogVocabularyResult
	if err := c.Call(protocol.MethodWatchdogVocabulary, struct{}{}, &v); err != nil {
		return err
	}
	printWatchdogVocabulary(w, v)
	return nil
}

func cmdWatchdogList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("watchdog list", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "only this scope (the vocabulary names the set)")
	subject := fs.String("subject", "", "only this subject id")
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
	params := protocol.WatchdogListParams{Scope: *scope, SubjectID: *subject}
	if *asJSON {
		return callJSON(w, c, protocol.MethodWatchdogList, params)
	}
	var res protocol.WatchdogListResult
	if err := c.Call(protocol.MethodWatchdogList, params, &res); err != nil {
		return err
	}
	if len(res.Rules) == 0 {
		fmt.Fprintln(w, "no watchdog rules match")
		return nil
	}
	for _, r := range res.Rules {
		fmt.Fprintf(w, "%s  %s/%s  %-22s %-14s %-8s %-4s  fired %d\n",
			r.ID, r.Scope, r.SubjectID, r.Signal, r.ThresholdHuman, r.Action, onOff(r.Enabled), r.FireCount)
	}
	return nil
}

func cmdWatchdogShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("watchdog show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon watchdog show ID [--json] — `aeon watchdog list` names the rules that exist")
	}
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.WatchdogShowParams{ID: positional[0]}
	if *asJSON {
		return callJSON(w, c, protocol.MethodWatchdogShow, params)
	}
	var res protocol.WatchdogShowResult
	if err := c.Call(protocol.MethodWatchdogShow, params, &res); err != nil {
		return err
	}
	r := res.Rule
	fmt.Fprintf(w, "rule %s — %s/%s, %s, threshold %s, action %s, %s · origin %s\n",
		r.ID, r.Scope, r.SubjectID, r.Signal, r.ThresholdHuman, r.Action, onOff(r.Enabled), r.Origin)
	if r.LastFiredAt != "" {
		fmt.Fprintf(w, "  last fired %s (%d fires)\n", r.LastFiredAt, r.FireCount)
	}
	if len(r.LoopsInEpisode) > 0 {
		fmt.Fprintf(w, "  in episode under it: %s — it will not re-fire for them until they make progress\n",
			strings.Join(r.LoopsInEpisode, ", "))
	}
	for _, e := range res.Events {
		oldVal, newVal := "—", "—"
		if e.OldValue != nil {
			oldVal = strconv.FormatInt(*e.OldValue, 10)
		}
		if e.NewValue != nil {
			newVal = strconv.FormatInt(*e.NewValue, 10)
		}
		fmt.Fprintf(w, "  %s %s by %s:%s (%s→%s)\n", e.At, e.Kind, e.ActorKind, e.ActorID, oldVal, newVal)
	}
	for _, f := range res.Fires {
		fmt.Fprintf(w, "  fired %s on %s — %s (episode %s)\n", f.At, f.LoopID, f.Action, f.Episode)
	}
	return nil
}

func cmdWatchdogSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("watchdog set", flag.ContinueOnError)
	fs.SetOutput(w)
	scope := fs.String("scope", "", "required — the vocabulary names the set")
	subject := fs.String("subject", "", "required — the scope's subject id")
	signal := fs.String("signal", "", "required — the vocabulary names the set, with its unit")
	threshold := fs.Int64("threshold", 0, "the threshold in the signal's own unit")
	action := fs.String("action", "", "required — the vocabulary names the set")
	disabled := fs.Bool("disabled", false, "create the rule disabled")
	yes := fs.Bool("yes", false, "confirm a loosening (a seatbelt against a typo, not a lock)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{
		"--scope": *scope, "--subject": *subject, "--signal": *signal, "--action": *action,
	} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("`watchdog set` needs %s — (scope, subject, signal, action, threshold) is a rule's identity", name)
		}
	}
	p := protocol.WatchdogSetParams{
		Scope: *scope, SubjectID: *subject, Signal: *signal, Threshold: *threshold, Action: *action,
	}
	if *disabled {
		f := false
		p.Enabled = &f
	}
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	attempt := func(confirm string) error {
		p.Confirm = confirm
		if *asJSON {
			return callJSON(w, c, protocol.MethodWatchdogSet, p)
		}
		var res protocol.WatchdogSetResult
		if err := c.Call(protocol.MethodWatchdogSet, p, &res); err != nil {
			return err
		}
		if res.View != nil {
			fmt.Fprintf(w, "watchdog rule %s: %s — %s/%s %s %s, threshold %s\n",
				res.View.ID, res.Effect, res.View.Scope, res.View.SubjectID,
				res.View.Signal, res.View.Action, res.View.ThresholdHuman)
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

func cmdWatchdogSetEnabled(w io.Writer, args []string, enabled bool) error {
	verb := "enable"
	if !enabled {
		verb = "disable"
	}
	fs := flag.NewFlagSet("watchdog "+verb, flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm the disable (a seatbelt against a typo, not a lock)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon watchdog %s ID%s [--json] — `aeon watchdog list` names the rules that exist",
			verb, usageYes(verb))
	}
	id := positional[0]
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	attempt := func(confirm string) error {
		params := protocol.WatchdogSetEnabledParams{ID: id, Enabled: enabled, Confirm: confirm}
		if *asJSON {
			return callJSON(w, c, protocol.MethodWatchdogSetEnabled, params)
		}
		var res protocol.WatchdogSetEnabledResult
		if err := c.Call(protocol.MethodWatchdogSetEnabled, params, &res); err != nil {
			return err
		}
		idOut := id
		if res.View != nil {
			idOut = res.View.ID
		}
		fmt.Fprintf(w, "watchdog rule %s: %s\n", idOut, res.Effect)
		return nil
	}
	// An enable TIGHTENS (the daemon commits it directly, no digest); a
	// disable LOOSENS (confirm-required rides back with the statement, and
	// the gate decides the rest). One attempt, the gate's branching.
	if err := attempt(""); err != nil {
		return loosenGate(w, err, *yes, attempt)
	}
	return nil
}

func usageYes(verb string) string {
	if verb == "disable" {
		return " --yes"
	}
	return ""
}

func cmdWatchdogRemove(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("watchdog remove", flag.ContinueOnError)
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
		return fmt.Errorf("usage: aeon watchdog remove ID --yes [--json] — `aeon watchdog list` names the rules that exist")
	}
	id := positional[0]
	c, err := dialControl(*want, noDaemonGuardSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	attempt := func(confirm string) error {
		if *asJSON {
			return callJSON(w, c, protocol.MethodWatchdogRemove, protocol.WatchdogRemoveParams{ID: id, Confirm: confirm})
		}
		var res protocol.WatchdogRemoveResult
		if err := c.Call(protocol.MethodWatchdogRemove, protocol.WatchdogRemoveParams{ID: id, Confirm: confirm}, &res); err != nil {
			return err
		}
		fmt.Fprintf(w, "removed watchdog rule %s — after this it will never fire; the row stays disabled and the audit stays\n", res.ID)
		return nil
	}
	if err := attempt(""); err != nil {
		return loosenGate(w, err, *yes, attempt)
	}
	return nil
}
