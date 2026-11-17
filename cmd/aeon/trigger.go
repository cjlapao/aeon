package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
)

// trigger.go — the operator's hands on the clock (S20 T8). Four verbs, and
// every one of them is a call on the SAME door `aeon automation` uses
// (S49 T8): the socket's trigger.* methods over a running daemon, or — no
// daemon — the same daemon control services over the store directly, after
// one fixed line says so (S45 PM4). The CLI holds NO control logic, no
// store handle of its own, no second implementation of pause — the daemon's
// internal/trigger/control.go answers once, and this file only speaks
// (TestTriggerSocketAndCLICallSameControlFunctions is the call-site rule;
// P34's daemon-first is why `aeon trigger` over a live daemon and the
// app's buttons cannot drift). FLAGS AND OUTPUT ARE UNCHANGED since S20 —
// the goldens pin them (TestTriggerCLIGoldensUnchanged).
//
// The two off switches stay visibly apart in what these verbs PRINT:
//   - PAUSED here is operational — "not right now", yours to undo.
//   - A policy refusal is governance — S10's sentence, arriving in the
//     fire record (trigger history / the fire-now answer), never
//     invented here. This plane does not evaluate policy and would be
//     lying if it printed a verdict.
//
// fire-now is listed last because it needs the clearest sentence: it
// fires AS A TRIGGER. The loop it starts is unattended — no field of the
// call can make it attended, over CLI or socket alike (TR2: there is no
// attended exception for a watching human).

func cmdTrigger(w io.Writer, args []string) error {
	if len(args) == 0 {
		return usageTrigger(w)
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return cmdTriggerList(w, rest)
	case "pause":
		return cmdTriggerSet(w, rest, true)
	case "resume":
		return cmdTriggerSet(w, rest, false)
	case "fire-now":
		return cmdTriggerFireNow(w, rest)
	case "add":
		return cmdTriggerAdd(w, rest)
	case "history":
		return cmdTriggerHistory(w, rest)
	case "-h", "--help", "help":
		return usageTrigger(w)
	default:
		fmt.Fprintf(w, "unknown trigger verb %q\n\n", verb)
		return usageTrigger(w)
	}
}

func usageTrigger(w io.Writer) error {
	fmt.Fprint(w, `aeon trigger — the operator's hands on the clock (S20)

  aeon trigger add                  register a trigger (validated at write, in the
                                    validator's own sentences; an existing id is refused)
  aeon trigger list                 every trigger: kind, blueprint, expr, zone,
                                    PAUSED state, next fire (computed, same
                                    library the clock uses)
  aeon trigger pause  <id>          operational off — "not right now" (TR12).
                                    Undoable this afternoon; NOT policy.
  aeon trigger resume <id>          lift the operational bar; the clock and the
                                    policy rows own whatever happens next.
  aeon trigger fire-now <id>        fire AS A TRIGGER, right now: unattended,
                                    through the same gates a tick passes
                                    (window, overlap, governance). It cannot
                                    fire as you — TR2 admits no attended
                                    back door. Webhook rows refuse: they are
                                    delivered, not fired.
  aeon trigger history <id>         the fire answers: fired / did not fire /
                                    could not say / skipped / refused (TR4).

Governance ("no webhooks here", "no unattended schedules") is NOT here: it
lives in policy (aeon policy add --type trigger_access ...), authored,
recorded, with an author and a lifetime. A fresh install has no such allow
and fires NOTHING until you write one.
`)
	return nil
}

func cmdTriggerAdd(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("trigger add", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("id", "", "the trigger's name — it is what pause, history and every fire record names")
	kind := fs.String("kind", "schedule", "schedule (a cron contract) or webhook (a signed door; it carries no cron and no zone)")
	blueprint := fs.String("blueprint", "", "the NAMED blueprint to fire (TR1 — a trigger fires a blueprint, never a query)")
	expr := fs.String("cron", "", "five-field expression or @daily-style descriptor, validated at write with robfig's own words")
	zone := fs.String("timezone", "", "explicit IANA zone (TR6) — never an offset, never the host's Local")
	workspace := fs.String("workspace", "", "the loop's working directory (workspace rules stay the loop's)")
	goal := fs.String("goal", "", "the loop's goal line; empty means the scheduler's stated default")
	winStart := fs.String("window-start", "", "working-hours window start (HH:MM); pair with --window-end (TR8, D7)")
	winEnd := fs.String("window-end", "", "working-hours window end (HH:MM)")
	winDays := fs.String("window-days", "", "ISO weekday list the window lives on, e.g. 1,2,3,4,5")
	secretEnv := fs.String("secret-env", "", "webhook only: the ENV VAR NAME holding the HMAC secret — the value is never typed here, stored anywhere, or logged (C17)")
	_ = addProtocolVersionFlag(fs) // the flag stays accepted; the door dials the current version
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := selectAutomationDoor(automationErrOut)
	if err != nil {
		return err
	}

	res, err := door.triggerAdd(context.Background(), protocol.TriggerAddParams{
		ID: *id, Kind: *kind, Blueprint: *blueprint, Expr: *expr, Zone: *zone,
		Workspace: *workspace, Goal: *goal,
		WindowStart: *winStart, WindowEnd: *winEnd, WindowDays: *winDays,
		SecretEnv: *secretEnv,
	})
	if err != nil {
		return err
	}
	if !res.Found {
		// T1's own sentence — the zone that does not load, the expression
		// robfig refused, the webhook row caught carrying a zone nobody
		// fires. The CLI quotes the validator; it does not paraphrase.
		fmt.Fprintf(w, "refused: %s\n", res.Reason)
		return nil
	}
	fmt.Fprintf(w, "%s: %s\n", res.TriggerID, res.Reason)
	return nil
}

func cmdTriggerList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("trigger list", flag.ContinueOnError)
	fs.SetOutput(w)
	_ = addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	door, err := selectAutomationDoor(automationErrOut)
	if err != nil {
		return err
	}

	res, err := door.triggerList(context.Background())
	if err != nil {
		return err
	}
	if len(res.Triggers) == 0 {
		fmt.Fprintln(w, "no triggers — the clock has nothing to answer (aeon trigger --help shows how to think about this)")
		return nil
	}
	for _, t := range res.Triggers {
		state := "ACTIVE"
		if t.Paused {
			// The operational word, printed nowhere else. A policy refusal
			// would NOT appear here — it arrives in history, in S10's own
			// sentence, because this plane cannot evaluate governance.
			state = "PAUSED"
		}
		fmt.Fprintf(w, "%-6s %-20s %-9s %s\n", state, t.ID, t.Kind, t.Blueprint)
		if t.Expr != "" {
			fmt.Fprintf(w, "        cron %s in %s — next: %s\n", t.Expr, t.Zone, t.NextFire)
		}
		if t.Window != "" {
			fmt.Fprintf(w, "        window %s\n", t.Window)
		}
		fmt.Fprintf(w, "        workspace %s\n", t.Workspace)
	}
	fmt.Fprint(w, "\nPAUSED = operational (undo it with resume) · governance is policy: aeon policy …\n")
	return nil
}

func cmdTriggerSet(w io.Writer, args []string, pause bool) error {
	fs := flag.NewFlagSet("trigger pause/resume", flag.ContinueOnError)
	fs.SetOutput(w)
	_ = addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(w, "usage: aeon trigger pause|resume <id>")
		return nil
	}
	door, err := selectAutomationDoor(automationErrOut)
	if err != nil {
		return err
	}

	var res protocol.TriggerControlResult
	if pause {
		res, err = door.triggerPause(context.Background(), id)
	} else {
		res, err = door.triggerResume(context.Background(), id)
	}
	if err != nil {
		return err
	}
	verb := "resumed"
	if pause {
		verb = "paused"
	}
	if !res.Found {
		fmt.Fprintf(w, "%s\n", res.Reason)
		return nil
	}
	fmt.Fprintf(w, "%s: %s — %s\n", verb, res.TriggerID, res.Reason)
	return nil
}

func cmdTriggerFireNow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("trigger fire-now", flag.ContinueOnError)
	fs.SetOutput(w)
	_ = addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(w, "usage: aeon trigger fire-now <id>   (it fires AS A TRIGGER — unattended, by construction)")
		return nil
	}
	door, err := selectAutomationDoor(automationErrOut)
	if err != nil {
		return err
	}

	res, err := door.triggerFireNow(context.Background(), id)
	if err != nil {
		return err
	}
	if !res.Found {
		fmt.Fprintf(w, "%s\n", res.Reason)
		return nil
	}
	dash := " — "
	if res.Reason == "" {
		// A fired answer needs no reason clause; printing "fired —  (loop…"
		// with a dangling dash read like a truncated sentence (T9 walk).
		dash = ""
	}
	if res.LoopID == "" {
		// Nothing started — refused, paused, or a webhook door. The
		// unattendedness parenthetical belongs ONLY to a line that names a
		// loop; printing "loop , started as a trigger: unattended" for a
		// refusal advertised a start that never happened (found on the T9
		// walk, second hand — the finding is in DEMO §Findings).
		fmt.Fprintf(w, "fire-now %s: %s%s%s\n", res.TriggerID, res.Outcome, dash, res.Reason)
		return nil
	}
	hand := "unattended" // not a promise read from the wire — a fact of TR2,
	// and the daemon test asserts the loop row agrees.
	fmt.Fprintf(w, "fire-now %s: %s%s%s (loop %s, started as a trigger: %s)\n", res.TriggerID, res.Outcome, dash, res.Reason, res.LoopID, hand)
	return nil
}

func cmdTriggerHistory(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("trigger history", flag.ContinueOnError)
	fs.SetOutput(w)
	_ = addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(w, "usage: aeon trigger history <id>")
		return nil
	}
	door, err := selectAutomationDoor(automationErrOut)
	if err != nil {
		return err
	}

	res, err := door.triggerHistory(context.Background(), id, 0)
	if err != nil {
		return err
	}
	if len(res.Fires) == 0 {
		fmt.Fprintf(w, "%s: no fire records — every instant so far is still owed an answer (reconcile speaks at startup)\n", id)
		return nil
	}
	for _, f := range res.Fires {
		fmt.Fprintf(w, "%-26s %-13s %s\n", f.Instant, f.Outcome, f.Reason)
	}
	fmt.Fprint(w, "\nfired / did not fire / could not say / skipped / refused — the five answers, recorded (TR4)\n")
	return nil
}
