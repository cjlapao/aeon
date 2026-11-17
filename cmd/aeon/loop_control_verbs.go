package main

// loop_control_verbs.go — S55 T11: the loop-control plane as CLI verbs. Thin
// clients of the socket doors (loop.stop / loop.resume / loop.cancel /
// loop.control / loop.control.requests / loop.control.dismiss, plus
// loop.raise from the budget plane): the CLI re-validates nothing, prints
// the daemon's refusals and results verbatim, and opens no store
// (TestCLIControlVerbsNeverImportStore).
//
// # The no-daemon answer (the P5 deviation, recorded)
//
// A stop of work nobody is running is not an error to diagnose: the fixed
// sentence is the answer. dialControl maps the "nobody is home" dial fault
// to it; every other dial fault is refused as-is and never falls back (the
// S45 T8 rule).
//
// # The --yes seatbelt
//
// cancel and raise carry no confirm field on the wire, so the seatbelt is
// the CLI's own: it reads the loop's state first (loop.control — the daemon's
// status and verdicts, the statement a person reads before the act) and only
// with --yes sends the state-changing call. Without --yes nothing moves and
// the exit is non-zero (the seed.go shape).

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// The no-daemon sentences (T11): the fixed answer the control verbs give when
// nobody is home, beside the non-zero exit. The loop/step verbs stop work, so
// the answer is "there is nothing to stop"; the budget/watchdog verbs guard
// work, so the answer names the daemon that would apply them.
const (
	noDaemonStopSentence  = "no daemon is running — there is nothing to stop"
	noDaemonGuardSentence = "budgets and watchdog rules are applied by a running daemon — start it with `aeon serve`"
)

// callJSON prints the wire result verbatim (T11): the bytes the daemon
// answered — no re-encode, no indent, no second formatter.
func callJSON(w io.Writer, c *protocol.Client, method protocol.MethodName, params any) error {
	var raw json.RawMessage
	if err := c.Call(method, params, &raw); err != nil {
		return err
	}
	fmt.Fprintln(w, string(raw))
	return nil
}

// loosenGate is the --yes seatbelt the budget and watchdog planes share
// (T11): the act comes back confirm-required carrying the daemon's canonical
// statement and its digest; the CLI prints the statement verbatim and only
// with --yes commits against the digest. Any other error is returned as-is,
// for the one renderer to speak (no re-wording here).
func loosenGate(w io.Writer, err error, yes bool, retry func(confirm string) error) error {
	var e *protocol.Error
	if !errors.As(err, &e) ||
		(e.Code != protocol.ErrBudgetRefused.Code && e.Code != protocol.ErrWatchdogRefused.Code) ||
		e.Data.Kind != "confirm-required" {
		return err
	}
	if e.Data.Statement != "" {
		fmt.Fprintln(w, e.Data.Statement)
	}
	if !yes {
		return fmt.Errorf("re-run with --yes to apply — nothing was written")
	}
	return retry(e.Data.Confirm)
}

// --- loop stop -----------------------------------------------------------------

func cmdLoopStop(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop stop", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why the loop stops (person text, the audit's own words)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop stop ID [--reason T] [--json] — `aeon loop list` names the loops that exist")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.LoopStopParams{ID: positional[0], Reason: *reason}
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopStop, params)
	}
	var res protocol.LoopStopResult
	if err := c.Call(protocol.MethodLoopStop, params, &res); err != nil {
		return err
	}
	if res.Stopped {
		fmt.Fprintf(w, "stopped loop %s — status %s\n", res.LoopID, res.Status)
	} else {
		// A lost race, not a refusal: the daemon's note names the status the
		// loop already has, and it is printed as the daemon wrote it.
		fmt.Fprintf(w, "loop %s not stopped — status %s: %s\n", res.LoopID, res.Status, res.Note)
	}
	if len(res.ParkedSteps) > 0 {
		ids := make([]string, len(res.ParkedSteps))
		for i, id := range res.ParkedSteps {
			ids[i] = strconv.FormatInt(id, 10)
		}
		fmt.Fprintf(w, "parked steps: %s\n", strings.Join(ids, ", "))
	}
	return nil
}

// --- loop resume -----------------------------------------------------------------

func cmdLoopResume(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop resume", flag.ContinueOnError)
	fs.SetOutput(w)
	message := fs.String("message", "", "stop-and-inject text: reaches a parked step's transcript exactly once")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop resume ID [--message T] [--json] — `aeon loop list` names the loops that exist")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.LoopResumeParams{ID: positional[0], Message: *message}
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopResume, params)
	}
	var res protocol.LoopResumeResult
	if err := c.Call(protocol.MethodLoopResume, params, &res); err != nil {
		return err
	}
	if res.Resumed {
		how := ""
		if res.How != "" {
			how = " (via " + res.How + ")"
		}
		fmt.Fprintf(w, "resumed loop %s — status %s%s\n", res.LoopID, res.Status, how)
		return nil
	}
	// A resume that did not move carries the daemon's refusal IN THE RESULT
	// (the loop.resume precedent) — printed as the daemon wrote it.
	if res.Refusal != nil {
		fmt.Fprintf(w, "loop %s not resumed — %s\n", res.LoopID, res.Refusal.Detail)
		return nil
	}
	fmt.Fprintf(w, "loop %s not resumed — status %s\n", res.LoopID, res.Status)
	return nil
}

// --- loop control (the read) ---------------------------------------------------------

func cmdLoopControl(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop control", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop control ID [--json] — the loop's status, its four eligibility verdicts, and the recent audit")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopControl, protocol.LoopControlParams{ID: positional[0]})
	}
	var res protocol.LoopControlResult
	if err := c.Call(protocol.MethodLoopControl, protocol.LoopControlParams{ID: positional[0]}, &res); err != nil {
		return err
	}
	renderLoopControl(w, res)
	return nil
}

// renderLoopControl prints the daemon's own verdicts — the SAME functions the
// operations gate on, so the screen and the engine cannot disagree. A
// non-eligible action carries the refusal the operation would answer,
// printed verbatim (never a guess the CLI makes).
func renderLoopControl(w io.Writer, res protocol.LoopControlResult) {
	fmt.Fprintf(w, "loop %s — %s\n", res.LoopID, res.Status)
	eligibilityLine(w, "stop", res.Eligibility.Stop)
	eligibilityLine(w, "resume", res.Eligibility.Resume)
	eligibilityLine(w, "cancel", res.Eligibility.Cancel)
	eligibilityLine(w, "raise", res.Eligibility.Raise)
	if res.CapHit != nil {
		fmt.Fprintf(w, "cap hit: budget %s (%s, %s) — spend floor %d, cap %d\n",
			res.CapHit.BudgetID, res.CapHit.Scope, res.CapHit.Dimension, res.CapHit.SpendFloor, res.CapHit.Cap)
	}
	if len(res.OpenRequests) > 0 {
		fmt.Fprintf(w, "open stop requests: %d — `aeon loop requests %s`\n", len(res.OpenRequests), res.LoopID)
	}
	for _, e := range res.Events {
		line := fmt.Sprintf("  %s %s by %s", e.At, e.Kind, e.ActorID)
		if e.Reason != "" {
			line += " — " + e.Reason
		}
		fmt.Fprintln(w, line)
	}
}

func eligibilityLine(w io.Writer, act string, v *protocol.ControlActionView) {
	if v == nil {
		return
	}
	if v.Eligible {
		fmt.Fprintf(w, "  %-7s eligible\n", act)
		return
	}
	if v.Refusal != nil {
		fmt.Fprintf(w, "  %-7s refused: %s\n", act, v.Refusal.Detail)
		return
	}
	fmt.Fprintf(w, "  %-7s not eligible\n", act)
}

// --- loop cancel -----------------------------------------------------------------

// cmdLoopCancel is loop.cancel's client. The --yes seatbelt is the CLI's own
// (T11): the state read is the statement, and the state-changing call goes
// out only with --yes — without it, nothing moves and the exit is non-zero.
func cmdLoopCancel(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop cancel", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why the loop is cancelled (required — the audit must say why)")
	yes := fs.Bool("yes", false, "confirm the cancel (a seatbelt against a typo, not a lock)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop cancel ID --reason T --yes [--json] — `aeon loop list` names the loops that exist")
	}
	if strings.TrimSpace(*reason) == "" {
		return fmt.Errorf("`loop cancel` needs --reason — a cancel destroys progress irreversibly, and the audit must say why")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	id := positional[0]
	// The seatbelt: the daemon's state is the statement a person reads
	// before the act. The act itself goes out only below, with --yes.
	var ctl protocol.LoopControlResult
	if err := c.Call(protocol.MethodLoopControl, protocol.LoopControlParams{ID: id}, &ctl); err != nil {
		return err
	}
	renderLoopControl(w, ctl)
	if !*yes {
		return fmt.Errorf("loop cancel %s: re-run with --yes to apply", id)
	}
	var res protocol.LoopCancelResult
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopCancel, protocol.LoopCancelParams{ID: id, Reason: *reason})
	}
	if err := c.Call(protocol.MethodLoopCancel, protocol.LoopCancelParams{ID: id, Reason: *reason}, &res); err != nil {
		return err
	}
	line := fmt.Sprintf("cancelled loop %s — status %s", res.LoopID, res.Status)
	if res.RunState != "" {
		line += ", run " + res.RunState
	}
	if res.SkippedAbortLane {
		line += " — the blueprint's on_abort lane was not run (a cancel is certain and free)"
	}
	fmt.Fprintln(w, line)
	return nil
}

// --- loop raise -------------------------------------------------------------------

// cmdLoopRaise is loop.raise's client (the budget plane's human door). The
// same seatbelt as cancel: the state read is the statement, and the act goes
// out only with --yes. Exactly one new cap is the WIRE's shape (the daemon
// refuses any other count by name) — the CLI forwards the flags as given.
func cmdLoopRaise(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop raise", flag.ContinueOnError)
	fs.SetOutput(w)
	budgetID := fs.String("budget", "", "the budget the loop is parked on (must name it, when it is known)")
	usd := fs.Float64("usd", 0, "the new usd cap")
	tokens := fs.Int64("tokens", 0, "the new token cap")
	wall := fs.Duration("wall", 0, "the new wall-time cap")
	steps := fs.Int64("steps", 0, "the new step cap")
	reason := fs.String("reason", "", "why the cap is raised (required — the audit's own words)")
	yes := fs.Bool("yes", false, "confirm the raise (a seatbelt against a typo, not a lock)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop raise ID [--budget B] [--usd N | --tokens N | --wall DUR | --steps N] --reason T --yes [--json]")
	}
	if strings.TrimSpace(*reason) == "" {
		return fmt.Errorf("`loop raise` needs --reason — the audit must say why the cap moved")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	id := positional[0]
	var ctl protocol.LoopControlResult
	if err := c.Call(protocol.MethodLoopControl, protocol.LoopControlParams{ID: id}, &ctl); err != nil {
		return err
	}
	renderLoopControl(w, ctl)
	if !*yes {
		return fmt.Errorf("loop raise %s: re-run with --yes to apply", id)
	}
	p := protocol.LoopRaiseParams{ID: id, BudgetID: *budgetID, Reason: *reason}
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
		}
	})
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopRaise, p)
	}
	var res protocol.LoopRaiseResult
	if err := c.Call(protocol.MethodLoopRaise, p, &res); err != nil {
		return err
	}
	if res.Raised != nil {
		from := "no recorded cap"
		if res.Raised.From != nil {
			from = strconv.FormatFloat(*res.Raised.From, 'g', -1, 64)
		}
		fmt.Fprintf(w, "raised loop %s: %s from %s to %s\n",
			res.LoopID, res.Raised.Dimension, from, strconv.FormatFloat(res.Raised.To, 'g', -1, 64))
	}
	switch {
	case res.Resumed:
		how := ""
		if res.How != "" {
			how = " (via " + res.How + ")"
		}
		fmt.Fprintf(w, "loop %s resumed — status %s%s\n", res.LoopID, res.Status, how)
	case res.Refusal != nil:
		// A lost race: the cap was lifted when the raise committed and the
		// loop moved first (the loop.resume precedent) — the daemon's
		// sentence, as the daemon wrote it.
		fmt.Fprintf(w, "loop %s: %s\n", res.LoopID, res.Refusal.Detail)
	case res.Note != "":
		fmt.Fprintf(w, "loop %s: %s\n", res.LoopID, res.Note)
	default:
		fmt.Fprintf(w, "loop %s — status %s\n", res.LoopID, res.Status)
	}
	return nil
}

// --- loop requests / dismiss ---------------------------------------------------------

func cmdLoopRequests(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop requests", flag.ContinueOnError)
	fs.SetOutput(w)
	status := fs.String("status", "", "only this status: open, honored or dismissed")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) > 1 {
		return fmt.Errorf("usage: aeon loop requests [ID] [--status open|honored|dismissed] [--json] — the stop-request desk")
	}
	loopID := ""
	if len(positional) == 1 {
		loopID = positional[0]
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.LoopControlRequestsParams{LoopID: loopID, Status: *status}
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopControlRequests, params)
	}
	var res protocol.LoopControlRequestsResult
	if err := c.Call(protocol.MethodLoopControlRequests, params, &res); err != nil {
		return err
	}
	if len(res.Requests) == 0 {
		fmt.Fprintln(w, "no stop requests match")
		return nil
	}
	for _, r := range res.Requests {
		fmt.Fprintf(w, "#%d %-32s %-10s %s\n", r.ID, r.LoopID, r.Status, r.Reason)
		if r.ByAgent != "" {
			line := "     requested by " + r.ByAgent
			if r.ByStepID != nil {
				line += fmt.Sprintf(" (step %d)", *r.ByStepID)
			}
			fmt.Fprintln(w, line)
		}
	}
	return nil
}

func cmdLoopDismiss(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop dismiss", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop dismiss REQ [--json] — `aeon loop requests` names the desk")
	}
	reqID, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a stop-request id: %w", positional[0], err)
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.LoopControlDismissParams{RequestID: reqID}
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopControlDismiss, params)
	}
	var res protocol.LoopControlDismissResult
	if err := c.Call(protocol.MethodLoopControlDismiss, params, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "dismissed stop request %d — status %s\n", res.RequestID, res.Status)
	return nil
}
