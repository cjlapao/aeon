package main

// inbox.go — S58 T6 (SV13): `aeon inbox` — the attention plane's CLI, a
// pure client of the socket's five doors (inbox.list · inbox.summary ·
// inbox.ack · inbox.snooze · inbox.unack). The CLI re-validates nothing:
// flag parsing refuses only what cannot be a value, every other refusal is
// the daemon's sentence printed verbatim, and `--json` is the wire result
// byte-verbatim (P5's parity, both directions tested).
//
// # No offline door (recorded P5 deviation)
//
// These verbs have no store-direct path and none is added: the inbox is
// LIVE evaluation (T2's whole argument — nothing stored, nothing cached),
// and a CLI that opened the store to answer it would be a second
// evaluator with a second truth. No daemon, no inbox — the guard sentence.

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdInbox(w io.Writer, args []string) error {
	if len(args) == 0 {
		printInboxUsage(w)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdInboxList(w, args[1:])
	case "summary":
		return cmdInboxSummary(w, args[1:])
	case "ack":
		return cmdInboxKeyed(w, args[1:], protocol.MethodInboxAck)
	case "unack":
		return cmdInboxKeyed(w, args[1:], protocol.MethodInboxUnack)
	case "snooze":
		return cmdInboxSnooze(w, args[1:])
	default:
		printInboxUsage(w)
		return fmt.Errorf("unknown `inbox` subcommand %q", args[0])
	}
}

// inboxNoDaemon is the guard for a plane that has no offline door: the
// inbox IS live evaluation (T2), and the usage says so too.
const inboxNoDaemon = "the inbox is live state evaluated by a running daemon — start it with `aeon serve`"

func printInboxUsage(w io.Writer) {
	fmt.Fprint(w, `aeon inbox — what needs a person (live evaluation; nothing stored)

  inbox list [--root ID] [--severity S] [--kind K]... [--include-acked]
             [--limit N] [--json]
  inbox summary [--root ID] [--json]
  inbox ack KEY [--json]
  inbox snooze KEY (--until RFC3339 | --for DUR) [--json]
  inbox unack KEY [--json]

Severities: needs-you | stuck | blocked | stopped | info. The answer always
names which sources reported, failed, or were never wired, so an empty
inbox can never read as "nothing was looking". Acking hides, never deletes.
Every verb needs a running daemon: the inbox is live state, never a file.
`)
}

func cmdInboxList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("inbox list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only this component's tree (default: every live root)")
	severity := fs.String("severity", "", "only this severity class")
	kinds := stringListFlag{}
	fs.Var(&kinds, "kind", "only these kinds (repeatable)")
	includeAcked := fs.Bool("include-acked", false, "also show items a person already acked or snoozed")
	limit := fs.Int("limit", 0, "at most N items (default 100, max 500)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialControl(*want, inboxNoDaemon)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.InboxListParams{
		RootID: *root, Severity: *severity, Kinds: kinds,
		IncludeAcked: *includeAcked, Limit: *limit,
	}
	if *asJSON {
		return callJSON(w, c, protocol.MethodInboxList, params)
	}
	var res protocol.InboxListResult
	if err := c.Call(protocol.MethodInboxList, params, &res); err != nil {
		return err
	}
	if len(res.Items) == 0 {
		fmt.Fprintln(w, "nothing needs you")
	} else {
		// Severity-grouped, the order the answer already gives within a
		// group (inbox order is the door's; grouping is a display of it).
		bySev := map[string][]protocol.AttentionItem{}
		var order []string
		for _, it := range res.Items {
			if _, ok := bySev[it.Severity]; !ok {
				order = append(order, it.Severity)
			}
			bySev[it.Severity] = append(bySev[it.Severity], it)
		}
		for _, sev := range order {
			fmt.Fprintf(w, "%s\n", sev)
			for _, it := range bySev[sev] {
				age := humanAge(it.AgeSeconds)
				extra := ""
				if it.LoopSlug != "" {
					extra = " " + it.LoopSlug
				}
				if it.Acked != nil {
					if it.Acked.Until != "" {
						extra += fmt.Sprintf(" (hidden until %s by %s)", it.Acked.Until, it.Acked.By)
					} else {
						extra += fmt.Sprintf(" (acked by %s)", it.Acked.By)
					}
				}
				fmt.Fprintf(w, "  [%s]%s\n", it.Reason, extra)
				fmt.Fprintf(w, "      key %s  %s  %s ago\n", it.Key, it.Kind, age)
				if it.Door.Sentence != "" {
					fmt.Fprintf(w, "      %s\n", it.Door.Sentence)
				}
			}
		}
	}
	printInboxSources(w, res.Sources)
	return nil
}

func cmdInboxSummary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("inbox summary", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only this component's tree (default: every live root)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialControl(*want, inboxNoDaemon)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.InboxSummaryParams{RootID: *root}
	if *asJSON {
		return callJSON(w, c, protocol.MethodInboxSummary, params)
	}
	var res protocol.InboxSummaryResult
	if err := c.Call(protocol.MethodInboxSummary, params, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%d need attention", res.Total)
	if res.Acked > 0 {
		fmt.Fprintf(w, " (%d already acked or snoozed — counted, hidden)", res.Acked)
	}
	if res.Total > 0 {
		sevs := make([]string, 0, len(res.BySeverity))
		for s := range res.BySeverity {
			sevs = append(sevs, s)
		}
		sort.Strings(sevs)
		parts := make([]string, 0, len(sevs))
		for _, s := range sevs {
			parts = append(parts, fmt.Sprintf("%s %d", s, res.BySeverity[s]))
		}
		fmt.Fprintf(w, ": %s", strings.Join(parts, ", "))
		if res.OldestSince != "" {
			fmt.Fprintf(w, "; oldest since %s", res.OldestSince)
		}
	}
	fmt.Fprintln(w)
	printInboxSources(w, res.Sources)
	return nil
}

// cmdInboxKeyed is ack and unack: the door takes the key and nothing else —
// the ACTOR is the peer the socket authenticated, never a param (SV12).
func cmdInboxKeyed(w io.Writer, args []string, method protocol.MethodName) error {
	fs := flag.NewFlagSet("inbox "+string(method[:strings.IndexByte(string(method), '.')]), flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	// The id/key leads and the flags follow (`aeon inbox ack KEY --json`),
	// so the positional is peeled before parsing — the way every aeon verb
	// with a subject parses (`splitPositional`, the store-verbs helper).
	// S58 T6: the dead-endpoint dispatch test caught this shape — flags
	// after the key were being silently dropped.
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	key := ""
	if len(positional) > 0 {
		key = positional[0]
	}
	if key == "" {
		return fmt.Errorf("`%s` needs the item key (inbox list shows them)", method)
	}
	c, err := dialControl(*want, inboxNoDaemon)
	if err != nil {
		return err
	}
	defer c.Close()
	var params any = protocol.InboxAckParams{Key: key}
	if method == protocol.MethodInboxUnack {
		params = protocol.InboxUnackParams{Key: key}
	}
	if *asJSON {
		return callJSON(w, c, method, params)
	}
	var res protocol.InboxAckResult
	if err := c.Call(method, params, &res); err != nil {
		return err
	}
	if res.Item == nil {
		fmt.Fprintln(w, "nothing changed (already in that state — no row written)")
		return nil
	}
	fmt.Fprintf(w, "%s  [%s]\n", res.Item.Key, res.Item.Reason)
	return nil
}

func cmdInboxSnooze(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("inbox snooze", flag.ContinueOnError)
	fs.SetOutput(w)
	until := fs.String("until", "", "hide until this RFC 3339 instant (at most 7 days out)")
	forDur := fs.String("for", "", "hide for this duration from now (e.g. 4h, 90m)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	// The id/key leads and the flags follow (`aeon inbox ack KEY --json`),
	// so the positional is peeled before parsing — the way every aeon verb
	// with a subject parses (`splitPositional`, the store-verbs helper).
	// S58 T6: the dead-endpoint dispatch test caught this shape — flags
	// after the key were being silently dropped.
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	key := ""
	if len(positional) > 0 {
		key = positional[0]
	}
	if key == "" {
		return fmt.Errorf("`inbox snooze` needs the item key (inbox list shows them)")
	}
	// Flag parsing refuses only what cannot be a value (P5): both spellings
	// of the horizon, or neither, is no intent. The horizon itself — the
	// 7-day cap, the clock — is the daemon's to refuse, verbatim.
	if (*until == "") == (*forDur == "") {
		return fmt.Errorf("`inbox snooze` takes exactly one of --until RFC3339 or --for DUR")
	}
	if *until != "" {
		if _, err := time.Parse(time.RFC3339, *until); err != nil {
			return fmt.Errorf("--until is not an RFC 3339 instant: %v", err)
		}
	} else {
		d, err := time.ParseDuration(*forDur)
		if err != nil {
			return fmt.Errorf("--for is not a duration: %v", err)
		}
		*until = time.Now().UTC().Add(d).Format(time.RFC3339)
	}
	c, err := dialControl(*want, inboxNoDaemon)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.InboxSnoozeParams{Key: key, Until: *until}
	if *asJSON {
		return callJSON(w, c, protocol.MethodInboxSnooze, params)
	}
	var res protocol.InboxAckResult
	if err := c.Call(protocol.MethodInboxSnooze, params, &res); err != nil {
		return err
	}
	if res.Item == nil {
		fmt.Fprintln(w, "nothing changed (already hidden until then — no row written)")
		return nil
	}
	fmt.Fprintf(w, "%s hidden until %s\n", res.Item.Key, *until)
	return nil
}

// printInboxSources is the trailing census line every inbox answer ends
// with (SV1): the states, so "nothing needs you" names who was looking.
func printInboxSources(w io.Writer, sources []protocol.InboxSourceState) {
	if len(sources) == 0 {
		return
	}
	parts := make([]string, 0, len(sources))
	for _, s := range sources {
		parts = append(parts, s.Name+":"+s.State)
	}
	fmt.Fprintf(w, "sources %s\n", strings.Join(parts, " "))
}

// humanAge renders the daemon's ageSeconds — display only; the wire carries
// the seconds and every surface does its own math at read (SV1).
func humanAge(secs int64) string {
	switch {
	case secs < 90:
		return strconv.FormatInt(secs, 10) + "s"
	case secs < 5400:
		return strconv.FormatInt(secs/60, 10) + "m"
	case secs < 86400:
		return strconv.FormatInt(secs/3600, 10) + "h"
	default:
		return strconv.FormatInt(secs/86400, 10) + "d"
	}
}

// stringListFlag collects a repeatable string flag.
type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(v string) error {
	if v == "" {
		return fmt.Errorf("empty kind")
	}
	*f = append(*f, v)
	return nil
}
