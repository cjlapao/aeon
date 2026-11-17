package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// automation_cmd.go — `aeon automation` (S49 T8, AN10/AN12, P34, S45 PM4):
// one verb set over BOTH families, event triggers' first CLI, schedules'
// first update/archive — every verb a CLIENT of the socket methods through
// the door in automation_client.go. The offline road is the same daemon
// services; `aeon trigger`'s verbs (trigger.go) now share that door too.
//
// The CLI re-validates only what cannot be a value: an unknown SOURCE
// prefix, a non-JSON --match/--sample, a non-numeric -n. Cron, zone,
// window, match grammar, blueprint validity, scope and the actor's right to
// write are the daemon's sentences, printed verbatim through either door.
// There is NO path for a secret VALUE: --secret-env takes a variable NAME
// and the daemon reads that name from its own environment (C17).

// automationErrOut is the side-channel: the fixed offline notice and the
// seatbelt's "re-run with --yes" line go where a person watching the
// terminal sees them before/above the answer (blueprintErrOut's shape).
var automationErrOut io.Writer = os.Stderr

func cmdAutomation(w io.Writer, args []string) error {
	if len(args) == 0 {
		printAutomationUsage(w)
		return nil
	}
	verb, rest := args[0], args[1:]
	// One selection per invocation, whichever arm answers: the offline door
	// prints its fixed line once, to stderr, before any output.
	needDoor := func() (automationDoor, error) { return selectAutomationDoor(automationErrOut) }
	if verb == "test" {
		// `test` is store-free AND dial-free on both roads (the grammar is
		// pure code): the fixed fallback line would misdescribe an answer
		// that opens neither daemon nor store, so it is served WITHOUT
		// choosing a door at all. `vocabulary` and `preview --cron` answer
		// through the door wherever one can be chosen — vocabulary is a
		// socket method like the rest (AN10); its offline door runs the
		// same automationview.Vocabulary().
		return runAutomationStoreFree(w, verb, rest)
	}
	if verb == "preview" && automationWantsDraft(rest) {
		// `preview --cron` walks a DRAFT: the store-free road again (the
		// daemon's own draft walker), before any door is chosen — same
		// reason lint --file has in the blueprint plane: naming no store
		// must not print "applied directly to the store".
		return runAutomationPreview(w, nil, context.Background(), rest)
	}
	if !isAutomationVerb(verb) {
		printAutomationUsage(w)
		return fmt.Errorf("unknown automation verb %q", verb)
	}
	door, err := needDoor()
	if err != nil {
		return err
	}
	return runAutomationVerb(w, door, verb, rest)
}

// automationVerbList is the closed verb set — the parity fixture's left
// column is automationVerbMethods() in automation_parity_test.go, pinned
// against the live table in BOTH directions.
func automationVerbList() []string {
	return []string{
		"list", "show", "fires", "runs", "health", "changes", "vocabulary",
		"preview", "test", "dryrun",
		"add-schedule", "add-webhook", "add-event", "update",
		"enable", "disable", "pause", "resume", "fire-now", "archive", "unarchive",
	}
}

func isAutomationVerb(verb string) bool {
	if verb == "help" || verb == "-h" || verb == "--help" {
		return true
	}
	for _, v := range automationVerbList() {
		if v == verb {
			return true
		}
	}
	return false
}

func runAutomationVerb(w io.Writer, door automationDoor, verb string, args []string) error {
	if verb == "help" || verb == "-h" || verb == "--help" {
		printAutomationUsage(w)
		return nil
	}
	ctx := context.Background()
	switch verb {
	case "list":
		return runAutomationList(w, door, ctx, args)
	case "show":
		return runAutomationShow(w, door, ctx, args)
	case "fires":
		return runAutomationFires(w, door, ctx, args)
	case "runs":
		return runAutomationRuns(w, door, ctx, args)
	case "health":
		return runAutomationHealth(w, door, ctx, args)
	case "changes":
		return runAutomationChanges(w, door, ctx, args)
	case "vocabulary":
		res, err := door.vocabulary(ctx)
		if err != nil {
			return err
		}
		return automationOutput(w, false, res, func(w io.Writer, v any) error { printAutomationVocabulary(w, v); return nil })
	case "preview":
		return runAutomationPreview(w, door, ctx, args)
	case "dryrun":
		return runAutomationDryRun(w, door, ctx, args)
	case "add-schedule":
		return runAutomationAddSchedule(w, door, ctx, args, false)
	case "add-webhook":
		return runAutomationAddSchedule(w, door, ctx, args, true)
	case "add-event":
		return runAutomationAddEvent(w, door, ctx, args)
	case "update":
		return runAutomationUpdate(w, door, ctx, args)
	case "enable":
		return runAutomationEnable(w, door, ctx, args)
	case "disable":
		return runAutomationDisable(w, door, ctx, args)
	case "pause", "resume":
		return runAutomationPauseResume(w, door, ctx, args, verb == "pause")
	case "fire-now":
		return runAutomationFireNow(w, door, ctx, args)
	case "archive", "unarchive":
		return runAutomationArchive(w, door, ctx, args, verb == "archive")
	}
	return fmt.Errorf("unknown automation verb %q", verb)
}

// runAutomationStoreFree serves `test` and `vocabulary` WITHOUT dialling:
// both are pure code on both roads, so a daemonless person gets the same
// answer with no fallback line and no store (T8's
// TestPreviewAndTestWorkOfflineWithoutAStore; the socket door, when a
// daemon IS running, answers from the very same functions — that equality
// is the point of routing them through the door elsewhere).
func runAutomationStoreFree(w io.Writer, verb string, args []string) error {
	switch verb {
	case "test":
		fs := flag.NewFlagSet("automation test", flag.ContinueOnError)
		fs.SetOutput(w)
		match := fs.String("match", "", "the rule to test, in the stored JSON form")
		sample := fs.String("sample", "", "the event to test against it, as JSON")
		asJSON := fs.Bool("json", false, "print the verdict as JSON")
		rest, err := automationParseFlags(fs, args)
		if err != nil {
			return err
		}
		if len(rest) != 0 || *match == "" {
			return fmt.Errorf("usage: aeon automation test --match JSON --sample JSON [--json]")
		}
		p, err := automationTestParams(*match, *sample)
		if err != nil {
			return err
		}
		res, err := automationStoreFreeTest(p)
		if err != nil {
			return err
		}
		return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error { printAutomationTest(w, v.(protocol.AutomationTestResult)); return nil })
	}
	return fmt.Errorf("unknown store-free automation verb %q", verb)
}

// --- reads ---------------------------------------------------------------------------

func runAutomationList(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation list", flag.ContinueOnError)
	fs.SetOutput(w)
	source := fs.String("source", "", "schedule, webhook or event")
	enabled := fs.String("enabled", "", "pass no value for either; --enabled arms only those, --disabled the others")
	archived := fs.String("archived", "", "include or only: widen the default listing to archived rows")
	blueprint := fs.String("blueprint", "", "only automations firing this blueprint name")
	component := fs.String("component", "", "only event triggers scoped to this component id")
	q := fs.String("q", "", "substring over id/name and the description")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("automation list takes no arguments (filters are flags)")
	}
	p := protocol.AutomationListParams{Source: *source, Blueprint: *blueprint, ComponentID: *component, Q: *q}
	switch *enabled {
	case "":
	case "true":
		yes := true
		p.Enabled = &yes
	case "false":
		no := false
		p.Enabled = &no
	default:
		return fmt.Errorf("the --enabled/--disabled flags carry no value; got %q", *enabled)
	}
	if *archived != "" {
		// The CLI's own closed set — the filter WORDS cannot be a name, so
		// refusing here is refusing what cannot be a value (the socket
		// refuses the same words, identically).
		if *archived != "include" && *archived != "only" {
			return fmt.Errorf("--archived takes include or only (the default excludes archived rows)")
		}
		p.Archived = *archived
	}
	res, err := door.list(ctx, p)
	if err != nil {
		return err
	}
	// The list's health dots are the daemon's health read (AN8 keeps them
	// advice); one second read, matched by source:id.
	health := map[string]protocol.AutomationHealthView{}
	if !*asJSON && len(res.Automations) > 0 {
		hr, err := door.health(ctx, protocol.AutomationHealthParams{})
		if err != nil {
			return err
		}
		for _, h := range hr.Health {
			health[h.Source+":"+h.ID] = h
		}
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationList(w, v.(protocol.AutomationListResult), health)
		return nil
	})
}

func runAutomationShow(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation show", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation show SOURCE:ID [--json]")
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	res, err := door.show(ctx, protocol.AutomationShowParams{Source: source, ID: id})
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationShow(w, v.(protocol.AutomationShowResult))
		return nil
	})
}

func runAutomationFires(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation fires", flag.ContinueOnError)
	fs.SetOutput(w)
	outcome := fs.String("outcome", "", "one of fired, did-not-fire, refused, skipped, could-not-say (the five are the daemon's; this plane keeps its own five words)")
	limit := fs.Int("limit", 0, "newest-first page size (the daemon's bound is 200)")
	before := fs.String("before", "", "the cursor the last page handed out (nextBeforeId)")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: aeon automation fires [SOURCE:ID] [--outcome O] [--limit N] [--json]")
	}
	p := protocol.AutomationFiresParams{Outcome: *outcome, Limit: *limit, BeforeId: *before}
	if len(rest) == 1 {
		source, id, err := parseAutomationSourceID(rest[0])
		if err != nil {
			return err
		}
		p.Source, p.ID = source, id
	}
	res, err := door.fires(ctx, p)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationFires(w, v.(protocol.AutomationFiresResult))
		return nil
	})
}

func runAutomationRuns(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation runs", flag.ContinueOnError)
	fs.SetOutput(w)
	limit := fs.Int("limit", 0, "newest-first page size")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation runs SOURCE:ID [--limit N] [--json]")
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	res, err := door.runs(ctx, protocol.AutomationRunsParams{Source: source, ID: id, Limit: *limit})
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationRuns(w, v.(protocol.AutomationRunsResult))
		return nil
	})
}

func runAutomationHealth(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation health", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	p := protocol.AutomationHealthParams{}
	if len(rest) == 1 {
		source, id, err := parseAutomationSourceID(rest[0])
		if err != nil {
			return err
		}
		p.Source, p.ID = source, id
	} else if len(rest) != 0 {
		return fmt.Errorf("usage: aeon automation health [SOURCE:ID] [--json]")
	}
	res, err := door.health(ctx, p)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationHealth(w, v.(protocol.AutomationHealthResult))
		return nil
	})
}

func runAutomationChanges(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation changes", flag.ContinueOnError)
	fs.SetOutput(w)
	limit := fs.Int("limit", 0, "newest-first page size (the daemon's bound is the vocabulary's auditPageMax)")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	p := protocol.AutomationChangesParams{Limit: *limit}
	if len(rest) == 1 {
		source, id, err := parseAutomationSourceID(rest[0])
		if err != nil {
			return err
		}
		p.Source, p.ID = source, id
	} else if len(rest) != 0 {
		return fmt.Errorf("usage: aeon automation changes [SOURCE:ID] [--limit N] [--json]")
	}
	res, err := door.changes(ctx, p)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationChanges(w, v.(protocol.AutomationChangesResult))
		return nil
	})
}

func runAutomationPreview(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation preview", flag.ContinueOnError)
	fs.SetOutput(w)
	expr := fs.String("cron", "", "the draft expression to walk (with --timezone); with it, no id and no store")
	zone := fs.String("timezone", "", "explicit IANA zone for the draft (TR6)")
	from := fs.String("from", "", "RFC3339 start instant (default: now)")
	n := fs.Int("n", 0, "how many instants (the daemon's cap is the vocabulary's previewMax)")
	winStart := fs.String("window-start", "", "draft window start (HH:MM)")
	winEnd := fs.String("window-end", "", "draft window end (HH:MM)")
	winDays := fs.String("window-days", "", "ISO weekday list for the draft window")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: aeon automation preview (SOURCE:ID | --cron EXPR --timezone ZONE …) [-n N] [--json]")
	}
	p := protocol.AutomationPreviewParams{N: *n, From: *from}
	if *expr != "" || *winStart != "" || *winEnd != "" || *winDays != "" {
		if len(rest) == 1 {
			return fmt.Errorf("preview walks ONE thing: an id or a --cron draft, not both")
		}
		spec := &protocol.AutomationPreviewSpec{Expr: *expr, Zone: *zone}
		if *winStart != "" || *winEnd != "" || *winDays != "" {
			spec.Window = &protocol.AutomationWindowView{Start: *winStart, End: *winEnd, Days: *winDays}
		}
		p.Spec = spec
	} else if len(rest) == 1 {
		id := rest[0]
		if i := strings.Index(id, ":"); i > 0 {
			// An id may arrive SOURCE-qualified; the door reads ids across
			// families anyway, and the source word here must still be one
			// of the three (an unknown SOURCE cannot be a value).
			source, bare := id[:i], id[i+1:]
			if err := automationCheckSource(source); err != nil {
				return err
			}
			id = bare
		}
		p.ID = id
	} else {
		return fmt.Errorf("usage: aeon automation preview (SOURCE:ID | --cron EXPR --timezone ZONE …) [-n N] [--json]")
	}
	if door == nil {
		res, err := automationStoreFreePreview(p)
		if err != nil {
			return err
		}
		return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
			printAutomationPreview(w, v.(protocol.AutomationPreviewResult))
			return nil
		})
	}
	res, err := door.preview(ctx, p)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationPreview(w, v.(protocol.AutomationPreviewResult))
		return nil
	})
}

func runAutomationDryRun(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation dryrun", flag.ContinueOnError)
	fs.SetOutput(w)
	since := fs.Int64("since-outbox-id", 0, "start the scan here (default: the trigger's own cursor — where the dispatcher left off)")
	limit := fs.Int("limit", 0, "cap the scan")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation dryrun SOURCE:ID [--since-outbox-id N] [--limit N] [--json]")
	}
	_, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	p := protocol.AutomationDryRunParams{ID: id, Limit: *limit}
	if *since > 0 {
		v := *since
		p.SinceOutboxId = &v
	}
	res, err := door.dryRun(ctx, p)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationDryRun(w, v.(protocol.AutomationDryRunResult))
		return nil
	})
}

// --- writes --------------------------------------------------------------------------

func runAutomationUpdate(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation update", flag.ContinueOnError)
	fs.SetOutput(w)
	blueprintName := fs.String("blueprint", "", "the new blueprint name")
	expr := fs.String("cron", "", "the new expression (clock families)")
	zone := fs.String("timezone", "", "the new IANA zone (clock families)")
	workspace := fs.String("workspace", "", "the new workspace")
	goal := fs.String("goal", "", "the new goal line")
	winStart := fs.String("window-start", "", "new window start (HH:MM)")
	winEnd := fs.String("window-end", "", "new window end (HH:MM)")
	winDays := fs.String("window-days", "", "new ISO weekday list")
	clearWindow := fs.Bool("clear-window", false, "empty the whole window at once (the pair itself is never half-cleared)")
	secretEnv := fs.String("secret-env", "", "new ENV VAR NAME for a webhook secret — a name, never a value (C17)")
	name := fs.String("name", "", "the new name (events)")
	match := fs.String("match", "", "the new rule, as JSON (events)")
	kickoff := fs.String("kickoff", "", "the new kickoff line (events)")
	maxRuns := fs.Int("max-runs-per-hour", -1, "new hourly cap (events); -1 leaves it alone")
	maxCost := fs.Float64("max-cost-per-day-usd", -1, "new daily cap in USD (events); -1 leaves it alone")
	clearCost := fs.Bool("clear-max-cost", false, "remove the cost cap (events; its own intention, §4c)")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation update SOURCE:ID [--blueprint B --cron E …] [--json]")
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	seen0 := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen0[f.Name] = true })
	if !seen0["blueprint"] && !seen0["cron"] && !seen0["timezone"] && !seen0["workspace"] &&
		!seen0["goal"] && !seen0["window-start"] && !seen0["window-end"] && !seen0["window-days"] &&
		!seen0["secret-env"] && !seen0["name"] && !seen0["match"] && !seen0["kickoff"] &&
		!seen0["max-runs-per-hour"] && !seen0["max-cost-per-day-usd"] && !*clearWindow && !*clearCost {
		// "Update with no fields" is not a no-op worth a door call and an
		// audit line — it is a person mid-sentence.
		return fmt.Errorf("update with no fields changes nothing; name at least one flag")
	}
	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	changed := func(k string) bool { return seen[k] }
	if source == "event" {
		p := protocol.EventTriggerUpdateParams{ID: id, ClearMaxCost: *clearCost}
		if changed("name") {
			p.Name = name
		}
		if changed("match") {
			if !json.Valid([]byte(*match)) {
				return fmt.Errorf("--match is not valid JSON: %s", automationMatchGrammarHint())
			}
			p.Match = json.RawMessage(*match)
		}
		if changed("blueprint") {
			p.Blueprint = blueprintName
		}
		if changed("kickoff") {
			p.Kickoff = kickoff
		}
		if changed("max-runs-per-hour") {
			v := *maxRuns
			p.MaxRunsPerHour = &v
		}
		if changed("max-cost-per-day-usd") {
			v := *maxCost
			p.MaxCostPerDayUSD = &v
		}
		view, err := door.eventUpdate(ctx, p)
		if err != nil {
			return err
		}
		return automationOutput(w, *asJSON, view, func(w io.Writer, v any) error {
			printAutomationEventView(w, v.(protocol.EventTriggerView))
			return nil
		})
	}
	p := protocol.TriggerUpdateParams{ID: id, ClearWindow: *clearWindow}
	if changed("blueprint") {
		p.Blueprint = blueprintName
	}
	if changed("cron") {
		p.Cron = expr
	}
	if changed("timezone") {
		p.Timezone = zone
	}
	if changed("workspace") {
		p.Workspace = workspace
	}
	if changed("goal") {
		p.Goal = goal
	}
	if changed("window-start") {
		p.WindowStart = winStart
	}
	if changed("window-end") {
		p.WindowEnd = winEnd
	}
	if changed("window-days") {
		p.WindowDays = winDays
	}
	if changed("secret-env") {
		p.SecretEnv = secretEnv
	}
	res, err := door.triggerUpdate(ctx, p)
	if err != nil {
		return err
	}
	if res.Reason != "" && !res.Changed {
		fmt.Fprintf(w, "refused: %s\n", res.Reason)
		return nil
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationUpdateResult(w, v.(protocol.TriggerUpdateResult))
		return nil
	})
}

func runAutomationEnable(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation enable", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Bool("yes", false, "confirm the authority grant — read from argv by the seatbelt gate")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation enable SOURCE:ID --yes   (arming is deliberate — see the statement above)")
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	if source != "event" {
		// The honest mapping, spoken: clock rows carry no enabled column —
		// the engine's word for them is paused (AN6).
		return fmt.Errorf("schedules and webhooks are PAUSED and RESUMED, not enabled and disabled — the engine sees paused: aeon automation resume %s", rest[0])
	}
	if !automationHasYes(args) {
		return automationSeatbeltShown(w, door, ctx, source, id, "enable")
	}
	view, err := door.eventEnable(ctx, id)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, view, func(w io.Writer, v any) error {
		printAutomationEventView(w, v.(protocol.EventTriggerView))
		return nil
	})
}

func runAutomationDisable(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation disable", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why — required, stored with the row and the audit line (AN3's deliberate narrowing)")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 || strings.TrimSpace(*reason) == "" {
		return fmt.Errorf("usage: aeon automation disable SOURCE:ID --reason R   (an unreasoned disable is what the reason flag exists to stop)")
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	if source != "event" {
		return fmt.Errorf("schedules and webhooks are PAUSED and RESUMED, not enabled and disabled — the operational off is: aeon automation pause %s", rest[0])
	}
	view, err := door.eventDisable(ctx, id, *reason)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, view, func(w io.Writer, v any) error {
		printAutomationEventView(w, v.(protocol.EventTriggerView))
		return nil
	})
}

func runAutomationPauseResume(w io.Writer, door automationDoor, ctx context.Context, args []string, pause bool) error {
	fs := flag.NewFlagSet("automation pause/resume", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	verb := "resume"
	if pause {
		verb = "pause"
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation %s SOURCE:ID [--json]", verb)
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	if source == "event" {
		return fmt.Errorf("events are not on a clock — narrow an event the deliberate way: aeon automation disable event:%s --reason R", id)
	}
	var res protocol.TriggerControlResult
	if pause {
		res, err = door.triggerPause(ctx, id)
	} else {
		res, err = door.triggerResume(ctx, id)
	}
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationControlResult(w, v.(protocol.TriggerControlResult), verb+"d")
		return nil
	})
}

func runAutomationFireNow(w io.Writer, door automationDoor, ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("automation fire-now", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.Bool("yes", false, "confirm the authority grant — read from argv by the seatbelt gate")
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation fire-now SCHEDULE_ID --yes   (it fires AS A TRIGGER — unattended, by construction, TR2)")
	}
	id := rest[0]
	if strings.HasPrefix(id, "event:") || strings.HasPrefix(id, "trg-") {
		// An event id typed here is a person mid-thought; the clock verb
		// cannot fire one and says so instead of the daemon's ghost-id.
		return fmt.Errorf("events fire when their event happens — fire-now is a clock verb; the outbox question is: aeon automation dryrun %s", id)
	}
	if !automationHasYes(args) {
		return automationSeatbeltClock(w, door, ctx, id, "fire-now")
	}
	res, err := door.triggerFireNow(ctx, id)
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationControlResult(w, v.(protocol.TriggerControlResult), "")
		return nil
	})
}

func runAutomationArchive(w io.Writer, door automationDoor, ctx context.Context, args []string, archive bool) error {
	fs := flag.NewFlagSet("automation archive", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "why — required to archive, stored with the row and the audit")
	fs.Bool("yes", false, "confirm: unarchive RESTORES nothing — it returns the row to its off state, still disarmed (AN3)") // read from argv by the gate
	asJSON := automationJSONFlagOnly(fs)
	rest, err := automationParseFlags(fs, args)
	if err != nil {
		return err
	}
	verb := "archive"
	if !archive {
		verb = "unarchive"
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: aeon automation %s SOURCE:ID [--reason R] [--yes] [--json]", verb)
	}
	if archive && strings.TrimSpace(*reason) == "" {
		return fmt.Errorf("archive without --reason is exactly the unreasoned state change the flag exists to stop")
	}
	source, id, err := parseAutomationSourceID(rest[0])
	if err != nil {
		return err
	}
	if !archive && !automationHasYes(args) {
		// Unarchive restores nothing by itself (AN3) — the seatbelt still
		// shows the statement and the row's health, because it is the
		// automation plane's gate, not a decoration.
		return automationSeatbeltShown(w, door, ctx, source, id, "unarchive")
	}
	p := protocol.AutomationArchiveParams{Source: source, ID: id, Reason: *reason}
	var res protocol.AutomationArchiveResult
	if archive {
		res, err = door.archive(ctx, p)
	} else {
		res, err = door.unarchive(ctx, p)
	}
	if err != nil {
		return err
	}
	return automationOutput(w, *asJSON, res, func(w io.Writer, v any) error {
		printAutomationArchiveResult(w, v.(protocol.AutomationArchiveResult), archive)
		return nil
	})
}
