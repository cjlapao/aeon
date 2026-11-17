package main

// notify.go — S58 T6 (SV13): `aeon notify` — the notification plane's
// CLI, a pure client of the socket's doors. Classes (what notifies and how
// it reads), channels (where it goes), deliveries (what happened), and the
// vocabulary that generates all closed sets. The CLI re-validates nothing;
// the daemon's sentences print verbatim; `--json` is the wire verbatim.
//
// # C17: a secret goes in, never comes out, and never rides a flag
//
// There is no flag, stdin pipe or prompt that accepts a secret VALUE.
// `--secret-file PATH` is the only path in: the daemon reads the file and
// stores the value in its own secrets store; the CLI never reads it. A test
// greps this file for the forbidden flag names. What leaves the daemon is
// the view — name, host, enabled, outcome — never a URL path, a secret
// reference, or a subscription's detail beyond the person's own door.
//
// # --yes is a seatbelt, not a security control
//
// create, archive and test act OUTWARD (authority to send off the machine,
// a retirement, a real send right now); without --yes the CLI refuses
// before dialling — no request, no side effect. enable/disable flip a
// standing state the daemon itself refuses to honor on a born-disabled
// channel; they do not act outward at the moment of the call.

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

const notifyNoDaemon = "notification channels live in a running daemon's store and secrets — start it with `aeon serve`"

func cmdNotify(w io.Writer, args []string) error {
	if len(args) == 0 {
		printNotifyUsage(w)
		return nil
	}
	switch args[0] {
	case "classes":
		return cmdNotifyClasses(w, args[1:])
	case "class":
		return cmdNotifyClass(w, args[1:])
	case "channels":
		return cmdNotifyChannels(w, args[1:])
	case "channel":
		return cmdNotifyChannel(w, args[1:])
	case "subscriptions":
		if len(args) < 2 || args[1] != "set" {
			printNotifyUsage(w)
			return fmt.Errorf("`notify subscriptions` takes the `set` verb")
		}
		return cmdNotifySubscriptionsSet(w, args[2:])
	case "deliveries":
		return cmdNotifyDeliveries(w, args[1:])
	case "vocabulary":
		return cmdNotifyVocabulary(w, args[1:])
	default:
		printNotifyUsage(w)
		return fmt.Errorf("unknown `notify` subcommand %q", args[0])
	}
}

func printNotifyUsage(w io.Writer) {
	fmt.Fprint(w, `aeon notify — who gets told, where it goes, what happened

  notify classes [--json]
  notify class update KIND [--variant V] [--enable|--disable] [--tone T]
                   [--title TMPL] [--body TMPL] [--json]
  notify channels [--json]
  notify channel show ID [--json]
  notify channel create --name N --url U [--secret-file PATH] [--allow-private]
                        [--root ID [--min-severity S]]... --yes [--json]
  notify channel update ID [--name N] [--url U] [--secret-file PATH]
                     [--allow-private|--no-allow-private] [--json]
  notify channel enable ID [--json]
  notify channel disable ID [--json]
  notify channel archive ID --reason R --yes [--json]
  notify channel test ID --yes [--json]
  notify subscriptions set --channel ID [--root ID] [--min-severity S]
                     [--kinds K,K] [--quiet-start HH:MM --quiet-end HH:MM --quiet-zone Z]
                     [--max-per-hour N] [--json]
  notify deliveries [--channel ID] [--outcome O] [--include-archived]
                     [--limit N] [--json]
  notify vocabulary [--json]

A secret enters ONLY through --secret-file (a path whose contents the daemon
stores); no verb accepts a secret value, and no answer ever carries one.
create, archive and test send off this machine or retire the means to: they
require --yes. The vocabulary verb prints the closed sets the daemon keeps
(kinds, severities, tones, placeholders, error kinds, limits) — the flags
above spell nothing the daemon does not also answer.
`)
}

// dialNotify is the plane's one door-opener: no offline path exists (the
// channels and secrets are the daemon's store, not files to read).
func dialNotify(want int) (*protocol.Client, error) {
	return dialControl(want, notifyNoDaemon)
}

func cmdNotifyClasses(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify classes", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyClassList, struct{}{})
	}
	var res protocol.NotifyClassListResult
	if err := c.Call(protocol.MethodNotifyClassList, struct{}{}, &res); err != nil {
		return err
	}
	if len(res.Classes) == 0 {
		// The honest empty (F-T6-walk, found on a fresh daemon): the
		// shipped class table rides INERT until the seed is applied, so
		// zero rows is a state to name, not silence to misread.
		fmt.Fprintln(w, "no event classes — the shipped `notify.events` seed set rides inert until `aeon seed apply notify.events` lays it")
		return nil
	}
	for _, cl := range res.Classes {
		name := cl.Kind
		if cl.Variant != "" {
			name += "/" + cl.Variant
		}
		off := ""
		if !cl.Enabled {
			off = "  (disabled)"
		}
		fmt.Fprintf(w, "%-34s %-10s v%-3d %s%s\n", name, cl.Tone, cl.Version, cl.TitleTemplate, off)
	}
	return nil
}

func cmdNotifyClass(w io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "update" {
		printNotifyUsage(w)
		return fmt.Errorf("`notify class` takes the `update` verb")
	}
	return cmdNotifyClassUpdate(w, args[1:])
}

func cmdNotifyClassUpdate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify class update", flag.ContinueOnError)
	fs.SetOutput(w)
	variant := fs.String("variant", "", "the row's variant (kinds with variants name both)")
	enable := fs.Bool("enable", false, "this kind notifies")
	disable := fs.Bool("disable", false, "this kind stays silent")
	tone := fs.String("tone", "", "info | success | warning | failure")
	title := fs.String("title", "", "the title template (the vocabulary names the placeholders)")
	body := fs.String("body", "", "the body template")
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
	kind := ""
	if len(positional) > 0 {
		kind = positional[0]
	}
	if kind == "" {
		return fmt.Errorf("`notify class update` needs the kind (notify classes lists them)")
	}
	// Parsing refuses only what cannot be a value: both flags, or no field
	// at all, is not an edit. Everything else — unknown kinds, bad
	// placeholders, bad tones — is the daemon's sentence, verbatim.
	if *enable && *disable {
		return fmt.Errorf("--enable and --disable cannot both name the same class")
	}
	p := protocol.NotifyClassUpdateParams{Kind: kind, Variant: *variant}
	if *enable {
		t := true
		p.Enabled = &t
	}
	if *disable {
		f := false
		p.Enabled = &f
	}
	if *tone != "" {
		v := *tone
		p.Tone = &v
	}
	if *title != "" {
		v := *title
		p.Title = &v
	}
	if *body != "" {
		v := *body
		p.Body = &v
	}
	if p.Enabled == nil && p.Tone == nil && p.Title == nil && p.Body == nil {
		return fmt.Errorf("nothing to change: give --enable/--disable, --tone, --title or --body")
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyClassUpdate, p)
	}
	var res protocol.NotifyClassItem
	if err := c.Call(protocol.MethodNotifyClassUpdate, p, &res); err != nil {
		return err
	}
	name := res.Kind
	if res.Variant != "" {
		name += "/" + res.Variant
	}
	fmt.Fprintf(w, "%s  %s  v%d  %s\n", name, res.Tone, res.Version, res.TitleTemplate)
	return nil
}

func cmdNotifyChannels(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify channels", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyChannelList, struct{}{})
	}
	var res protocol.NotifyChannelListResult
	if err := c.Call(protocol.MethodNotifyChannelList, struct{}{}, &res); err != nil {
		return err
	}
	if len(res.Channels) == 0 {
		fmt.Fprintln(w, "no channels (notify channel create makes one; it is born disabled)")
		return nil
	}
	for _, ch := range res.Channels {
		state := "enabled"
		if !ch.Enabled {
			state = "disabled"
		}
		if ch.ArchivedAt != "" {
			state = "archived"
		}
		line := fmt.Sprintf("%s  %s  %s  host %s  %s", ch.ID, ch.Name, ch.Kind, ch.Host, state)
		if ch.ConsecutiveFailures >= 3 {
			line += fmt.Sprintf("  FAILING (%d in a row)", ch.ConsecutiveFailures)
		} else if ch.LastOutcome != "" {
			line += "  last: " + ch.LastOutcome
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func cmdNotifyDeliveries(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify deliveries", flag.ContinueOnError)
	fs.SetOutput(w)
	channel := fs.String("channel", "", "only this channel (required — deliveries belong to a channel)")
	outcome := fs.String("outcome", "", "only this outcome (shown as a filter, the log's truth unchanged)")
	includeArchived := fs.Bool("include-archived", false, "cover an archived channel's log too")
	limit := fs.Int("limit", 0, "at most N rows (daemon's default when unset)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *channel == "" {
		return fmt.Errorf("`notify deliveries` needs --channel ID (notify channels lists them)")
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.NotifyDeliveryListParams{
		ChannelID: *channel, Limit: *limit, IncludeArchived: *includeArchived,
	}
	if *asJSON && *outcome == "" {
		return callJSON(w, c, protocol.MethodNotifyDeliveryList, params)
	}
	var res protocol.NotifyDeliveryListResult
	if err := c.Call(protocol.MethodNotifyDeliveryList, params, &res); err != nil {
		return err
	}
	shown := 0
	for _, d := range res.Deliveries {
		if *outcome != "" && d.Outcome != *outcome {
			continue
		}
		line := fmt.Sprintf("%d  %s  %s  attempts %d  %s", d.ID, d.Outcome, d.At, d.Attempts, d.ChannelID)
		if d.ErrorKind != "" {
			line += "  " + d.ErrorKind
			if d.HTTPStatus != 0 {
				line += " " + strconv.Itoa(d.HTTPStatus)
			}
		}
		fmt.Fprintln(w, line)
		shown++
	}
	if *outcome != "" && shown == 0 {
		fmt.Fprintf(w, "no deliveries with outcome %q among the rows read (filter shown; not the log's claim)\n", *outcome)
	}
	return nil
}

// printNotifyVocabulary is one renderer for the generated usage half AND the
// `notify vocabulary` verb (the watchdog pattern: help and verb cannot drift).
func printNotifyVocabulary(w io.Writer, v protocol.NotifyVocabularyResult) {
	fmt.Fprintln(w, "Closed sets, from the daemon's vocabulary:")
	fmt.Fprintf(w, "  kinds        %s\n", strings.Join(v.Kinds, " | "))
	fmt.Fprintf(w, "  severities   %s\n", strings.Join(v.Severities, " | "))
	fmt.Fprintf(w, "  tones        %s\n", strings.Join(v.Tones, " | "))
	fmt.Fprintf(w, "  placeholders %s\n", strings.Join(v.Placeholders, " | "))
	fmt.Fprintf(w, "  errorKinds   %s\n", strings.Join(v.ErrorKinds, " | "))
	fmt.Fprintf(w, "  channelKinds %s\n", strings.Join(v.ChannelKinds, " | "))
	fmt.Fprintf(w, "  limits       title %dB  body %dB  dedup %dm  maxPerHour %d (default, floor %d, ceiling %d)  timeout %ds  attempts %d\n",
		v.Limits.TitleMaxBytes, v.Limits.BodyMaxBytes, v.Limits.DedupMinutes,
		v.Limits.DefaultMaxPerHour, v.Limits.MaxPerHourFloor, v.Limits.MaxPerHourCeiling,
		v.Limits.SendTimeoutSecs, v.Limits.Attempts)
}

func cmdNotifyVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyVocabulary, struct{}{})
	}
	var v protocol.NotifyVocabularyResult
	if err := c.Call(protocol.MethodNotifyVocabulary, struct{}{}, &v); err != nil {
		return err
	}
	printNotifyVocabulary(w, v)
	return nil
}
