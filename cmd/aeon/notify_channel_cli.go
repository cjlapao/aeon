package main

// notify_channel_cli.go — S58 T6 (SV7, SV13): the `aeon notify channel`
// and `notify subscriptions` verbs, pure clients of the channel plane's
// doors. The rules that live HERE (and nowhere else) are flag shape and
// the --yes seatbelt; every judgement — is this URL a public host, is this
// kind known, does a secret resolve — is the daemon's and prints verbatim.
//
// The seatbelt (C17's companion): create makes standing authority to send
// off this machine, archive retires it for real (its log outlives it), test
// sends one NOW. Those three refuse before dialling without --yes. No flag
// anywhere reads a secret value: --secret-file names a path the DAEMON
// reads.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// seatbelt is the one sentence shape for a verb that acts outward (the
// refusal happens BEFORE dialControl — no request leaves the process).
func seatbelt(w io.Writer, yes bool, verb, act string) error {
	if yes {
		return nil
	}
	return fmt.Errorf("`notify channel %s` %s: pass --yes to mean it", verb, act)
}

func cmdNotifyChannel(w io.Writer, args []string) error {
	if len(args) == 0 {
		printNotifyUsage(w)
		return fmt.Errorf("`notify channel` takes show|create|update|enable|disable|archive|test")
	}
	switch args[0] {
	case "show":
		return cmdNotifyChannelShow(w, args[1:])
	case "create":
		return cmdNotifyChannelCreate(w, args[1:])
	case "update":
		return cmdNotifyChannelUpdate(w, args[1:])
	case "enable":
		return cmdNotifyChannelSetEnabled(w, args[1:], protocol.MethodNotifyChannelEnable)
	case "disable":
		return cmdNotifyChannelSetEnabled(w, args[1:], protocol.MethodNotifyChannelDisable)
	case "archive":
		return cmdNotifyChannelArchive(w, args[1:])
	case "test":
		return cmdNotifyChannelTest(w, args[1:])
	default:
		printNotifyUsage(w)
		return fmt.Errorf("unknown `notify channel` subcommand %q", args[0])
	}
}

func cmdNotifyChannelShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify channel show", flag.ContinueOnError)
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
	id := ""
	if len(positional) > 0 {
		id = positional[0]
	}
	if id == "" {
		return fmt.Errorf("`notify channel show` needs the channel id (notify channels lists them)")
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.NotifyChannelShowParams{ID: id}
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyChannelShow, params)
	}
	var v protocol.NotifyChannelView
	if err := c.Call(protocol.MethodNotifyChannelShow, params, &v); err != nil {
		return err
	}
	printChannelView(w, v)
	return nil
}

// printChannelView is the person's channel reading: host, never the URL's
// path; secretPresent, never a reference (SV7 — the view is the leakproof
// type; this renderer cannot print what is not in it).
func printChannelView(w io.Writer, v protocol.NotifyChannelView) {
	state := "enabled"
	if !v.Enabled {
		state = "disabled"
	}
	if v.ArchivedAt != "" {
		state = "archived (" + v.ArchiveReason + ")"
	}
	secret := "no"
	if v.SecretPresent {
		secret = "yes"
	}
	fmt.Fprintf(w, "%s  %s (%s)\n", v.ID, v.Name, v.Kind)
	fmt.Fprintf(w, "  host %s  %s  secret %s  since %s\n", v.Host, state, secret, v.CreatedAt)
	if v.LastOutcome != "" {
		fmt.Fprintf(w, "  last delivery %s", v.LastOutcome)
		if v.ConsecutiveFailures > 0 {
			fmt.Fprintf(w, "  (%d consecutive failures)", v.ConsecutiveFailures)
		}
		fmt.Fprintln(w)
	}
	for _, s := range v.Subscriptions {
		root := "every root"
		if s.RootID != "" {
			root = "root " + s.RootID
		}
		sev := "any"
		if s.MinSeverity != "" {
			sev = "at least " + s.MinSeverity
		}
		kinds := "all kinds (every kind the class table raises)"
		if s.Kinds != nil {
			if len(s.Kinds) == 0 {
				kinds = "none listed"
			} else {
				kinds = strings.Join(s.Kinds, ",")
			}
		}
		fmt.Fprintf(w, "  %s  %s  %s  max %d/hour\n", root, sev, kinds, s.MaxPerHour)
	}
}

func cmdNotifyChannelCreate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify channel create", flag.ContinueOnError)
	fs.SetOutput(w)
	name := fs.String("name", "", "the channel's name")
	url := fs.String("url", "", "the webhook URL (https, public host — the daemon checks; --allow-private is the deliberate exception)")
	secretFile := fs.String("secret-file", "", "PATH to the signing secret's file (the daemon reads the file; no flag carries a value)")
	allowPrivate := fs.Bool("allow-private", false, "permit a private or loopback host (a deliberate, checked exception)")
	var roots, severities stringListFlag
	fs.Var(&roots, "root", "subscribe this root (repeatable; omit --root entirely for every root)")
	fs.Var(&severities, "min-severity", "at least this severity, paired by position with --root")
	yes := fs.Bool("yes", false, "I know this creates standing authority to send off this machine")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *url == "" {
		return fmt.Errorf("`notify channel create` needs --name and --url")
	}
	// The seatbelt BEFORE dialling: without --yes nothing is even asked.
	if err := seatbelt(w, *yes, "create", "makes standing authority to send off this machine"); err != nil {
		return err
	}
	p := protocol.NotifyChannelCreateParams{
		Name: *name, URL: *url, SecretFile: *secretFile, AllowPrivate: *allowPrivate,
	}
	if len(roots) > 0 || len(severities) > 0 {
		if len(severities) > len(roots) {
			return fmt.Errorf("every --min-severity pairs with an earlier --root (position by position)")
		}
		for i, r := range roots {
			sub := protocol.NotifySubscriptionSetParams{RootID: r}
			if i < len(severities) {
				sub.MinSeverity = severities[i]
			}
			p.Subscriptions = append(p.Subscriptions, sub)
		}
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyChannelCreate, p)
	}
	var v protocol.NotifyChannelView
	if err := c.Call(protocol.MethodNotifyChannelCreate, p, &v); err != nil {
		return err
	}
	fmt.Fprintln(w, "channel created — born DISABLED (enable is the second, separate act):")
	printChannelView(w, v)
	return nil
}

func cmdNotifyChannelUpdate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify channel update", flag.ContinueOnError)
	fs.SetOutput(w)
	name := fs.String("name", "", "rename")
	url := fs.String("url", "", "new webhook URL (checked like create)")
	secretFile := fs.String("secret-file", "", "PATH to a replacement secret's file")
	allowPrivate := fs.Bool("allow-private", false, "permit a private or loopback host")
	noAllowPrivate := fs.Bool("no-allow-private", false, "withdraw the private-host exception")
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
	id := ""
	if len(positional) > 0 {
		id = positional[0]
	}
	if id == "" {
		return fmt.Errorf("`notify channel update` needs the channel id")
	}
	if *allowPrivate && *noAllowPrivate {
		return fmt.Errorf("--allow-private and --no-allow-private cannot both stand")
	}
	p := protocol.NotifyChannelUpdateParams{ID: id, SecretFile: *secretFile}
	if *name != "" {
		v := *name
		p.Name = &v
	}
	if *url != "" {
		v := *url
		p.URL = &v
	}
	if *allowPrivate {
		t := true
		p.AllowPrivate = &t
	}
	if *noAllowPrivate {
		f := false
		p.AllowPrivate = &f
	}
	if p.Name == nil && p.URL == nil && p.AllowPrivate == nil && p.SecretFile == "" {
		return fmt.Errorf("nothing to change: give --name, --url, --secret-file or the allow-private pair")
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyChannelUpdate, p)
	}
	var v protocol.NotifyChannelView
	if err := c.Call(protocol.MethodNotifyChannelUpdate, p, &v); err != nil {
		return err
	}
	printChannelView(w, v)
	return nil
}

func cmdNotifyChannelSetEnabled(w io.Writer, args []string, method protocol.MethodName) error {
	fs := flag.NewFlagSet("notify channel "+strings.TrimPrefix(string(method), "notify.channel."), flag.ContinueOnError)
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
	id := ""
	if len(positional) > 0 {
		id = positional[0]
	}
	if id == "" {
		return fmt.Errorf("`notify channel %s` needs the channel id", strings.TrimPrefix(string(method), "notify.channel."))
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var params any = protocol.NotifyChannelEnableParams{ID: id}
	if method == protocol.MethodNotifyChannelDisable {
		params = protocol.NotifyChannelDisableParams{ID: id}
	}
	if *asJSON {
		return callJSON(w, c, method, params)
	}
	var v protocol.NotifyChannelView
	if err := c.Call(method, params, &v); err != nil {
		return err
	}
	// The daemon's own born-disabled / unverified refusals ride `err`
	// verbatim; reaching here means the flip happened.
	printChannelView(w, v)
	return nil
}

func cmdNotifyChannelArchive(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify channel archive", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why this channel retires (required — the row outlives the memory)")
	yes := fs.Bool("yes", false, "I know this retires standing authority for real")
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
	id := ""
	if len(positional) > 0 {
		id = positional[0]
	}
	if id == "" {
		return fmt.Errorf("`notify channel archive` needs the channel id")
	}
	if *reason == "" {
		return fmt.Errorf("`notify channel archive` needs --reason (the log outlives the channel and the reason outlives the argument)")
	}
	if err := seatbelt(w, *yes, "archive", "retires that authority for real (the delivery log stays)"); err != nil {
		return err
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.NotifyChannelArchiveParams{ID: id, Reason: *reason}
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyChannelArchive, params)
	}
	var v protocol.NotifyChannelView
	if err := c.Call(protocol.MethodNotifyChannelArchive, params, &v); err != nil {
		return err
	}
	fmt.Fprintln(w, "archived (never deleted — notify deliveries --include-archived reads its history):")
	printChannelView(w, v)
	return nil
}

func cmdNotifyChannelTest(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify channel test", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "I know this sends a real notification NOW")
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
	id := ""
	if len(positional) > 0 {
		id = positional[0]
	}
	if id == "" {
		return fmt.Errorf("`notify channel test` needs the channel id")
	}
	if err := seatbelt(w, *yes, "test", "sends a real notification off this machine NOW"); err != nil {
		return err
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.NotifyChannelTestParams{ID: id}
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifyChannelTest, params)
	}
	var res protocol.NotifyTestResult
	if err := c.Call(protocol.MethodNotifyChannelTest, params, &res); err != nil {
		return err
	}
	line := fmt.Sprintf("%s  %s  attempts %d", res.Outcome, res.At, res.Attempts)
	if res.ErrorKind != "" {
		line += "  " + res.ErrorKind
		if res.HTTPStatus != 0 {
			line += fmt.Sprintf(" (HTTP %d)", res.HTTPStatus)
		}
	}
	fmt.Fprintln(w, line)
	return nil
}

// cmdNotifySubscriptionsSet is the client of notify.subscription.set — the
// parity demands every door has a verb.
func cmdNotifySubscriptionsSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("notify subscriptions set", flag.ContinueOnError)
	fs.SetOutput(w)
	channel := fs.String("channel", "", "the channel (required)")
	root := fs.String("root", "", "the root (omit for every root)")
	minSeverity := fs.String("min-severity", "", "at least this severity (the vocabulary names the set)")
	kinds := fs.String("kinds", "", "comma list of exact kinds (unset = all the class table raises)")
	quietStart := fs.String("quiet-start", "", "quiet window start HH:MM (all three or none)")
	quietEnd := fs.String("quiet-end", "", "quiet window end HH:MM")
	quietZone := fs.String("quiet-zone", "", "quiet window IANA zone (e.g. America/New_York)")
	maxPerHour := fs.Int("max-per-hour", 0, "the rate ceiling for this subscription (0 = daemon default)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *channel == "" {
		return fmt.Errorf("`notify subscriptions set` needs --channel ID")
	}
	// The quiet window is all-or-none as a SHAPE question (a half window
	// cannot be a window); whether the zone exists is the daemon's check.
	n := 0
	for _, s := range []string{*quietStart, *quietEnd, *quietZone} {
		if s != "" {
			n++
		}
	}
	if n != 0 && n != 3 {
		return fmt.Errorf("the quiet window is all of --quiet-start --quiet-end --quiet-zone or none of them")
	}
	p := protocol.NotifySubscriptionSetParams{
		ChannelID: *channel, RootID: *root, MinSeverity: *minSeverity,
		QuietStart: *quietStart, QuietEnd: *quietEnd, QuietZone: *quietZone,
		MaxPerHour: *maxPerHour,
	}
	if *kinds != "" {
		list := strings.Split(*kinds, ",")
		p.Kinds = &list
	}
	c, err := dialNotify(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodNotifySubscriptionSet, p)
	}
	var v protocol.NotifySubscriptionView
	if err := c.Call(protocol.MethodNotifySubscriptionSet, p, &v); err != nil {
		return err
	}
	fmt.Fprintf(w, "subscription set: %s  %s  %v  max %d/hour\n",
		v.ChannelID, v.RootID, v.Kinds, v.MaxPerHour)
	return nil
}
