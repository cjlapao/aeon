package main

// automation_add.go — the three ADD roads of `aeon automation` (S49 T8,
// split from automation_cmd.go under SIZE1: one concern per file, and the
// adds are the one concern with a seatbelt BEFORE the write and a second
// audited write after it). The disarmed road is the whole story here:
// without --enabled a schedule/webhook row is created and immediately
// PAUSED, an event row created and immediately DISABLED — each half
// through its own door method, each audited in its own frozen sentence.
// --enabled ARMS, and arming is an authority grant: it needs --yes, and
// the seatbelt shows the DRAFT's description with the honest "no health
// yet" note (the row does not exist until the write happens).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
)

func runAutomationAddSchedule(w io.Writer, door automationDoor, ctx context.Context, args []string, webhook bool) error {
	verb := "add-schedule"
	if webhook {
		verb = "add-webhook"
	}
	fs := flag.NewFlagSet("automation "+verb, flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("id", "", "the automation's name — SOURCE is derived, the id is what every record names")
	blueprintName := fs.String("blueprint", "", "the NAMED blueprint to run (TR1 — never a query)")
	expr := fs.String("cron", "", "five-field expression or @daily-style descriptor (schedule only)")
	zone := fs.String("timezone", "", "explicit IANA zone (TR6, schedule only)")
	workspace := fs.String("workspace", "", "the loop's working directory")
	goal := fs.String("goal", "", "the loop's goal line")
	winStart := fs.String("window-start", "", "working-hours window start (HH:MM)")
	winEnd := fs.String("window-end", "", "working-hours window end (HH:MM)")
	winDays := fs.String("window-days", "", "ISO weekday list, e.g. 1,2,3,4,5")
	secretEnv := fs.String("secret-env", "", "webhook only: the ENV VAR NAME holding the HMAC secret — the value lives in the DAEMON's environment, never in a flag (C17)")
	enabled := fs.Bool("enabled", false, "ARM it at creation — an authority grant, needs --yes; without it the row is created and immediately paused")
	fs.Bool("yes", false, "confirm the authority grant") // read from argv by the seatbelt gate
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 || *id == "" || *blueprintName == "" {
		return fmt.Errorf("usage: aeon automation %s --id ID --blueprint B [--cron E --timezone Z …] [--enabled --yes] [--json]", verb)
	}
	if *enabled && !automationHasYes(args) {
		return automationAddSeatbelt(w, verb, automationDraftView(webhook, *id, *blueprintName, *expr, *zone, *secretEnv, *workspace, *goal, *winStart, *winEnd, *winDays))
	}
	kind := "schedule"
	if webhook {
		kind = "webhook"
	}
	res, err := door.triggerAdd(ctx, protocol.TriggerAddParams{
		ID: *id, Kind: kind, Blueprint: *blueprintName, Expr: *expr, Zone: *zone,
		Workspace: *workspace, Goal: *goal,
		WindowStart: *winStart, WindowEnd: *winEnd, WindowDays: *winDays,
		SecretEnv: *secretEnv,
	})
	if err != nil {
		return err
	}
	if !res.Found {
		return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
			fmt.Fprintf(w, "refused: %s\n", v.(protocol.TriggerControlResult).Reason)
			return nil
		})
	}
	if *asJSON {
		return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
			fmt.Fprintf(w, "%s: %s\n", v.(protocol.TriggerControlResult).TriggerID, v.(protocol.TriggerControlResult).Reason)
			return nil
		})
	}
	if !*enabled {
		// The disarmed road: created, then immediately PAUSED through the
		// same operational door — two audited lines, both the daemon's
		// sentences. The row exists; the authority to run does not yet.
		pr, perr := door.triggerPause(ctx, *id)
		if perr != nil {
			return perr
		}
		fmt.Fprintf(w, "%s: %s\n", res.TriggerID, res.Reason)
		fmt.Fprintf(w, "%s\n", pr.Reason)
		return nil
	}
	fmt.Fprintf(w, "%s: %s\n", res.TriggerID, res.Reason)
	return nil
}

func runAutomationAddEvent(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation add-event", flag.ContinueOnError)
	fs.SetOutput(w)
	name := fs.String("name", "", "the trigger's name (event ids are minted; the NAME is what records speak)")
	match := fs.String("match", "", "the rule as JSON — the same form `automation test` takes")
	blueprintName := fs.String("blueprint", "", "the NAMED blueprint to run")
	maxRuns := fs.Int("max-runs-per-hour", 0, "runs per hour the trigger may start (the validator owns the numbers)")
	maxCost := fs.Float64("max-cost-per-day-usd", 0, "daily cost cap in USD")
	root := fs.String("root", "", "scope: the root component id")
	component := fs.String("component", "", "scope: the component id")
	kickoff := fs.String("kickoff", "", "the loop's kickoff line")
	enabled := fs.Bool("enabled", false, "ARM it at creation — an authority grant, needs --yes; without it the row is created and immediately disabled")
	fs.Bool("yes", false, "confirm the authority grant") // read from argv by the seatbelt gate
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 || *name == "" || *match == "" || *blueprintName == "" {
		return fmt.Errorf("usage: aeon automation add-event --name N --match JSON --blueprint B --max-runs-per-hour N [--enabled --yes] [--json]")
	}
	if !json.Valid([]byte(*match)) {
		// What cannot be a value: broken JSON never reaches the grammar
		// (which owns every real refusal, verbatim, for anything parseable).
		return fmt.Errorf("--match is not valid JSON: %s", automationMatchGrammarHint())
	}
	if *enabled && !automationHasYes(args) {
		return automationAddSeatbelt(w, "add-event", automationDraftEventView(*name, *match, *blueprintName, *maxRuns))
	}
	p := protocol.EventTriggerCreateParams{
		Name: *name, RootID: *root, ComponentID: *component, Match: json.RawMessage(*match),
		Blueprint: *blueprintName, Kickoff: *kickoff, MaxRunsPerHour: *maxRuns,
	}
	if *maxCost > 0 {
		v := *maxCost
		p.MaxCostPerDayUSD = &v
	}
	view, err := door.eventCreate(ctx, p)
	if err != nil {
		return err
	}
	if !*enabled {
		// The disarmed road beside the clock family's: created, then
		// DISABLED through the same door, the reason naming the road.
		view, err = door.eventDisable(ctx, view.ID, "created disarmed by automation add-event without --enabled — enabling is a separate deliberate act (AN3)")
		if err != nil {
			return err
		}
	}
	return automationOutput(w, *asJSON, view, func(w io.Writer, v any) error {
		printAutomationEventView(w, v.(protocol.EventTriggerView))
		return nil
	})
}
