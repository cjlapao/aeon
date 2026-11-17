package main

// status.go — S58 T6 (SV11, SV13): `aeon status` — the mission overview,
// a pure client of mission.status. Per root: what is running, waiting,
// blocked and idle; what needs you (the inbox's own census, same words);
// what it costs against its cap with the floor LABELLED (P32); what comes
// next. A group whose seam is unwired prints `not wired` with its owner —
// never a zero, which would read as "checked, nothing found". There is no
// composite health number to print, because there is no composite health
// number to compute (SV11). No offline door: the overview is live
// evaluation, like the inbox.

import (
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/cjlapao/aeon/internal/protocol"
)

const statusNoDaemon = "the mission overview reads live state from a running daemon — start it with `aeon serve`"

func cmdStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "one component's tree (default: every live root)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dialControl(*want, statusNoDaemon)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.MissionStatusParams{RootID: *root}
	if *asJSON {
		return callJSON(w, c, protocol.MethodMissionStatus, params)
	}
	var res protocol.MissionStatusResult
	if err := c.Call(protocol.MethodMissionStatus, params, &res); err != nil {
		return err
	}
	if len(res.Components) == 0 {
		fmt.Fprintln(w, "no live root components")
		return nil
	}
	for _, comp := range res.Components {
		name := comp.Name
		if name == "" {
			name = comp.RootID
		}
		fmt.Fprintf(w, "%s (%s)\n", name, comp.RootID)
		fmt.Fprintf(w, "  loops    running %d  waiting %d  blocked %d  idle %d\n",
			comp.Loops.Running, comp.Loops.Waiting, comp.Loops.Blocked, comp.Loops.Idle)
		fmt.Fprintf(w, "  needsYou %d waiting", comp.NeedsYou.Total)
		if comp.NeedsYou.Acked > 0 {
			fmt.Fprintf(w, " (%d acked/snoozed, counted)", comp.NeedsYou.Acked)
		}
		for _, s := range comp.NeedsYou.Sources {
			if s.State != "reporting" {
				fmt.Fprintf(w, "  source %s: %s", s.Name, s.State)
			}
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  spend    %s\n", spendLine(comp.Spend))
		fmt.Fprintf(w, "  cap      %s\n", capLine(comp.Cap))
		fmt.Fprintf(w, "  next     %s\n", nextLine(comp.NextReady))
		if comp.LastActivityAt != "" {
			fmt.Fprintf(w, "  last     %s\n", comp.LastActivityAt)
		}
	}
	return nil
}

// spendLine names the floor and its cost state — `partial` or `unknown`
// never prints without the word (P32).
func spendLine(s protocol.MissionSpend) string {
	if s.State != "reporting" {
		return "not wired"
	}
	floor := strconv.FormatFloat(s.UsdAtLeast, 'f', 2, 64)
	switch s.CostState {
	case "known":
		return "$" + floor + " (exact)"
	case "partial":
		return "at least $" + floor + " (partial rollup — not exact)"
	default:
		return "at least $" + floor + " (cost state unknown)"
	}
}

func capLine(c protocol.MissionCap) string {
	switch c.State {
	case "in-force":
		s := "IN FORCE: " + c.Summary
		if c.Period != "" {
			s += " (" + c.Period + ")"
		}
		return s
	case "none":
		return "no cap in force over this tree"
	default:
		return "not wired (S55)"
	}
}

func nextLine(n protocol.MissionNextReady) string {
	switch n.State {
	case "ready":
		s := n.LoopSlug
		if n.Phase != "" {
			s += " at " + n.Phase
		}
		if n.Reason != "" {
			s += " (" + n.Reason + ")"
		}
		return s
	case "none":
		return "nothing queued"
	default:
		return "not wired (S27)"
	}
}
