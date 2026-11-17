package main

// step_verbs_control.go — S55 T11: closes the rule-1 gap. step.stop and
// step.continue have had socket methods and a UI door since S9/S31 but no CLI
// verb; a person watching a step from the terminal could not pause it. These
// two are thin clients of the same two doors the app dials — the refusal a
// cap-parked step answers (loop.raise is the door for that shape) is printed
// as the daemon wrote it, never re-worded here.

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdStepStop(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("step stop", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon step stop ID [--json] — `aeon step list SLUG` names the loop's steps")
	}
	id, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a step id: %w", positional[0], err)
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodStepStop, protocol.StepStopParams{StepID: id})
	}
	var res protocol.StepStopResult
	if err := c.Call(protocol.MethodStepStop, protocol.StepStopParams{StepID: id}, &res); err != nil {
		return err
	}
	if res.Stopped {
		fmt.Fprintf(w, "stopped step %d — it parks at the next turn boundary\n", res.StepID)
		return nil
	}
	// Not an error: "already stopped" and "never running" both land here, and
	// a double-click is ordinary (the unsubscribe precedent) — the daemon's
	// honest answer, as the daemon gave it.
	fmt.Fprintf(w, "step %d not stopped — it was not running (already stopped, or never started)\n", res.StepID)
	return nil
}

func cmdStepContinue(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("step continue", flag.ContinueOnError)
	fs.SetOutput(w)
	message := fs.String("message", "", "stop-and-inject text: joins the transcript exactly once")
	route := fs.String("route", "", "the route this continuation runs on (empty keeps the step's own)")
	effort := fs.String("reasoning-effort", "", "the reasoning effort this continuation runs at (empty keeps the step's own)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon step continue ID [--message T] [--route R] [--reasoning-effort E] [--json]")
	}
	id, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a step id: %w", positional[0], err)
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.StepContinueParams{StepID: id, Route: *route, ReasoningEffort: *effort, Message: *message}
	if *asJSON {
		return callJSON(w, c, protocol.MethodStepContinue, params)
	}
	var res protocol.AnswerResponse
	if err := c.Call(protocol.MethodStepContinue, params, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "resumed step %d — status %s\n", res.StepID, res.Status)
	return nil
}
