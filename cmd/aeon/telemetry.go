package main

// telemetry.go — `aeon telemetry loop|component|project|workspace`
// (S33 T8, P34). Every verb is a pure CLIENT of the socket's four reads —
// the graph.go shape followed exactly: dial, Call the identical method the
// app calls, print. The CLI opens no store here (the daemon owns the rows;
// a store-opening reader would be the second door the slice forbids), and
// it prints ONLY what the wire said: null token counts print as "—",
// partial cost prints with its state as a word, and an unrecorded
// composition says "not recorded" rather than showing an empty list. The
// three-state honesty (TL11) survives to the terminal because this file
// refuses to render absence as zero.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdTelemetry(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`telemetry needs a verb: loop, component, workspace — try "aeon telemetry help"`)
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, telemetryUsage)
		return nil
	case "loop":
		return telemetryLoop(w, args[1:])
	case "component":
		return telemetryTier(w, args[1:], true)
	case "project":
		// S44-extra T8: a project is a root component; the telemetry project
		// door is gone, restated as one fixed redirect (CT13) and the usage
		// exit (2). The subtree rollup now reads through the component door.
		fmt.Fprintln(os.Stderr, componentRedirectLine())
		os.Exit(2)
	case "workspace":
		return telemetryWorkspace(w, args[1:])
	default:
		return fmt.Errorf("telemetry verb %q unknown — the verbs are loop, component, workspace", args[0])
	}
	// The project case os.Exit's (never returns); this is the one fall-through
	// the compiler must see to accept the function returns on every path.
	return nil
}

const telemetryUsage = `aeon telemetry — the counting tiers, asked of the daemon (S33)

  aeon telemetry loop       --step ID    per-turn rows, compactions, activity, D2's two numbers
  aeon telemetry component  --id ID      a component's subtree rollup (loops, tokens, cost, files, commands)
  aeon telemetry workspace               the singleton grand total (no arguments)

` + "`project`" + ` is gone — a project is a root component; its subtree rollup
reads through ` + "`aeon telemetry component`" + ` (S44-extra T8).

Reads only — nothing here writes, and nothing else may either: the detail
rows are written by the runtime's own capture seams and cannot be backfilled.
Cost prints with its STATE: (known), (partial — at least), (unknown).
`

func telemetryDialAndCall(method protocol.MethodName, params, out any) error {
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(method, params, out)
}

// renderSums is the ONE printer for tier numbers — component, project and
// workspace all speak it, so a terminal shows the same contract the app's
// tab renders. Cost never prints without its state word beside it.
func renderSums(w io.Writer, s protocol.TelemetrySums) {
	fmt.Fprintf(w, "loops run        %d\n", s.LoopsRun)
	fmt.Fprintf(w, "wall             %d ms\n", s.WallMsTotal)
	fmt.Fprintf(w, "tokens in/out    %d / %d\n", s.InputTokensTotal, s.OutputTokensTotal)
	fmt.Fprintf(w, "cache read/write %d / %d\n", s.CacheReadTokensTotal, s.CacheWriteTokensTotal)
	fmt.Fprintf(w, "reasoning        %d\n", s.ReasoningTokensTotal)
	switch s.CostState {
	case "known":
		fmt.Fprintf(w, "cost             $%.6f (known)\n", s.CostUSDTotal)
	case "partial":
		fmt.Fprintf(w, "cost             $%.6f (partial — AT LEAST this, never exactly)\n", s.CostUSDTotal)
	default:
		fmt.Fprintf(w, "cost             (unknown — no figure claimed)\n")
	}
	fmt.Fprintf(w, "files touched    %d (read %d, written %d)\n", s.FilesTouchedTotal, s.FilesReadTotal, s.FilesWrittenTotal)
	fmt.Fprintf(w, "lines            +%d / -%d\n", s.LinesAddedTotal, s.LinesRemovedTotal)
	fmt.Fprintf(w, "commands         %d run, %d failed, %d ms of wall\n", s.CommandsRunTotal, s.CommandsFailedTotal, s.CommandWallMsTotal)
}

func telemetryTier(w io.Writer, args []string, component bool) error {
	kind := "project"
	if component {
		kind = "component"
	}
	fs := flag.NewFlagSet("telemetry "+kind, flag.ContinueOnError)
	id := fs.String("id", "", kind+" id (required)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" {
		return fmt.Errorf("telemetry %s needs --id — there is no ambient subject to guess", kind)
	}
	if component {
		var row protocol.ComponentTelemetry
		if err := telemetryDialAndCall(protocol.MethodTelemetryComponent, protocol.TelemetryComponentParams{ComponentID: *id}, &row); err != nil {
			return err
		}
		fmt.Fprintf(w, "component %s (updated %s)\n", row.ComponentID, row.UpdatedAt)
		renderSums(w, row.TelemetrySums)
		return nil
	}
	var row protocol.ProjectTelemetry
	if err := telemetryDialAndCall(protocol.MethodTelemetryProject, protocol.TelemetryProjectParams{ProjectID: *id}, &row); err != nil {
		return err
	}
	fmt.Fprintf(w, "project %s (updated %s)\n", row.ProjectID, row.UpdatedAt)
	renderSums(w, row.TelemetrySums)
	return nil
}

func telemetryWorkspace(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("telemetry workspace", flag.ContinueOnError)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var row protocol.WorkspaceTelemetry
	if err := telemetryDialAndCall(protocol.MethodTelemetryWorkspace, struct{}{}, &row); err != nil {
		return err
	}
	if !row.Exists {
		fmt.Fprintln(w, "no telemetry row yet — nothing has been counted since telemetry began (this is a state, not a failure)")
		return nil
	}
	fmt.Fprintf(w, "workspace (updated %s)\n", row.UpdatedAt)
	renderSums(w, row.TelemetrySums)
	return nil
}

func telemetryLoop(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("telemetry loop", flag.ContinueOnError)
	stepID := fs.Int64("step", 0, "step id (required)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stepID <= 0 {
		return fmt.Errorf("telemetry loop needs --step ID — `aeon step list` names the steps that exist")
	}
	var data protocol.TelemetryLoopData
	if err := telemetryDialAndCall(protocol.MethodTelemetryLoop, protocol.TelemetryLoopParams{StepID: *stepID}, &data); err != nil {
		return err
	}
	fmt.Fprintf(w, "step %d · loop %s · root %s (%s)\n", data.Step.ID, data.Step.LoopSlug, data.Step.ComponentRoot, data.Step.Status)
	// D2's two numbers, side by side, each labelled with what it MEANS.
	fmt.Fprintf(w, "context now      %d tokens estimated (what the next call would carry)\n", data.CurrentContextTokens)
	fmt.Fprintf(w, "cumulative       %d tokens (the step_turn rows' sum — what the loop has spent)\n", data.CumulativeTokensTotal)
	fmt.Fprintf(w, "turns %d · compactions %d · files %d · commands %d\n",
		len(data.Turns), len(data.Compactions), len(data.FileActivities), len(data.CommandActivities))
	for _, t := range data.Turns {
		// Null tokens print as dash, never 0; cost prints as its state.
		fmt.Fprintf(w, "  turn %d %-16s wall %6dms  in %-6s out %-5s cost %s\n",
			t.Iteration, t.Category, t.WallMs, tokenOr(t.InputTokens), tokenOr(t.OutputTokens), costCell(t))
	}
	for _, c := range data.Compactions {
		fmt.Fprintf(w, "  compaction rows %d→%d  est %d→%d  in %-6s out %-5s cost %s\n",
			c.StartIdx, c.EndIdx, c.EstimateBefore, c.EstimateAfter, tokenOr(c.InputTokens), tokenOr(c.OutputTokens), costCellComp(c))
	}
	for _, f := range data.FileActivities {
		fmt.Fprintf(w, "  file   %-8s %s  +%s/-%s\n", f.Action, f.Path, intOr(f.LinesAdded), intOr(f.LinesRemoved))
	}
	for _, c := range data.CommandActivities {
		fmt.Fprintf(w, "  command %q  exit %s  %d ms\n", c.Command, exitOr(c.ExitCode), c.WallMs)
	}
	if len(data.Composition) == 0 {
		fmt.Fprintln(w, "composition    not recorded (a step from before the composition was captured — not empty, UNKNOWN)")
	}
	return nil
}

func tokenOr(v *int) string { return intOr(v) }

func intOr(v *int) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *v)
}

func exitOr(v *int) string {
	if v == nil {
		return "— (ended on a signal)"
	}
	return fmt.Sprintf("%d", *v)
}

func costCell(t protocol.TurnRow) string {
	switch t.CostState {
	case "known":
		if t.CostUSD == nil {
			return "known but unpriced"
		}
		return fmt.Sprintf("$%.6f (known)", *t.CostUSD)
	case "partial":
		if t.CostUSD == nil {
			return "(partial)"
		}
		return fmt.Sprintf("$%.6f+ (partial)", *t.CostUSD)
	default:
		return "(unknown)"
	}
}

func costCellComp(c protocol.CompactionRow) string {
	switch c.CostState {
	case "known":
		if c.CostUSD == nil {
			return "known but unpriced"
		}
		return fmt.Sprintf("$%.6f (known)", *c.CostUSD)
	case "partial":
		if c.CostUSD == nil {
			return "(partial)"
		}
		return fmt.Sprintf("$%.6f+ (partial)", *c.CostUSD)
	default:
		return "(unknown)"
	}
}
