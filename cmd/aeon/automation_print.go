package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/automationview"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
)

// automation_print.go — the ONE presentation per answer (both doors fill
// the same result structs, so one printer serves socket and offline; the
// parity test compares bytes through these functions). No printer decides
// a state, composes a health verdict, or invents a sentence: the state
// words read the view's booleans, the health words read the daemon's
// AutomationHealthView, refusals quote the door verbatim.

// --- the store-free trio ---------------------------------------------------------------

func automationStoreFreeTest(p protocol.AutomationTestParams) (protocol.AutomationTestResult, error) {
	res, err := typedAnswer[protocol.AutomationTestResult](protocol.AnswerAutomationTest(p))
	return res, err
}

func automationStoreFreePreview(p protocol.AutomationPreviewParams) (protocol.AutomationPreviewResult, error) {
	res, err := typedAnswer[protocol.AutomationPreviewResult](protocol.AnswerAutomationDraftPreview(p, daemon.PreviewDraftSpec))
	return res, err
}

func automationStoreFreeVocabulary() (automationview.VocabularyView, error) {
	return automationview.Vocabulary(), nil
}

// automationTestParams parses the two JSON flags into the wire params. The
// parse guards only SHAPE (valid JSON, a named event in the sample): the
// match grammar and the per-key questions are AnswerAutomationTest's and
// automationview's, verbatim, on both doors.
func automationTestParams(match, sample string) (protocol.AutomationTestParams, error) {
	p := protocol.AutomationTestParams{Match: json.RawMessage(match)}
	if sample == "" {
		return p, fmt.Errorf("--sample names the event to test against the rule, as JSON (keys: event, rootId, componentId, goalType, taskType, sourceKind)")
	}
	var s struct {
		Event       string `json:"event"`
		RootID      string `json:"rootId"`
		ComponentID string `json:"componentId"`
		GoalType    string `json:"goalType"`
		TaskType    string `json:"taskType"`
		SourceKind  string `json:"sourceKind"`
	}
	if err := json.Unmarshal([]byte(sample), &s); err != nil {
		return p, fmt.Errorf("--sample is not valid JSON: %v", err)
	}
	p.Sample = protocol.AutomationTestEvent{Event: s.Event, RootID: s.RootID, ComponentID: s.ComponentID, GoalType: s.GoalType, TaskType: s.TaskType, SourceKind: s.SourceKind}
	return p, nil
}

// --- the authority seatbelt -------------------------------------------------------------

// automationAuthorityLine is the plane's ONE statement composition: the
// daemon's fixed sentence with <description> filled from the SAME Describe
// formatter the desk uses — the CLI invents no authority text (the design's
// rule for every surface).
func automationAuthorityLine(desc string) (string, error) {
	v := automationview.Vocabulary()
	return strings.ReplaceAll(v.AuthorityStatement, "<description>", desc), nil
}

// automationSeatbeltShown serves enable/unarchive on a named row: reads
// only (show carries description AND health), prints statement + health,
// and refuses — the re-run with --yes is the only door.
func automationSeatbeltShown(w io.Writer, door automationDoor, ctx context.Context, source, id, verb string) error {
	res, err := door.show(ctx, protocol.AutomationShowParams{Source: source, ID: id})
	if err != nil {
		return err
	}
	return automationSeatbeltPrint(w, res.Description, automationHealthWords(res.Health), verb, source+":"+id)
}

// automationSeatbeltClock serves fire-now on a bare clock id: the row is
// found through the unified list (source is part of the NAME, and the
// fire-now argument carries only the id), health through the health read.
func automationSeatbeltClock(w io.Writer, door automationDoor, ctx context.Context, id, verb string) error {
	lr, err := door.list(ctx, protocol.AutomationListParams{Q: id, Archived: "include"})
	if err != nil {
		return err
	}
	var view *protocol.AutomationView
	for i := range lr.Automations {
		if lr.Automations[i].ID == id {
			view = &lr.Automations[i]
			break
		}
	}
	if view == nil {
		// No row: the seatbelt has no description to speak. The ghost id
		// answer belongs to fire-now itself (the daemon's sentence), not a
		// lookup the CLI half-did.
		return fmt.Errorf("no clock automation %q is registered — fire-now will say the same with --yes; nearest ids: see aeon automation list", id)
	}
	hr, err := door.health(ctx, protocol.AutomationHealthParams{Source: view.Source, ID: id})
	if err != nil {
		return err
	}
	words := "unverifiable"
	if len(hr.Health) == 1 {
		words = automationHealthWords(hr.Health[0])
	}
	return automationSeatbeltPrint(w, automationview.Describe(automationViewOf(*view)), words, verb, view.Source+":"+id)
}

// automationAddSeatbelt serves add-* --enabled: no row exists yet, so the
// description is the DRAFT's (the same Describe formatter over the form
// fields) and the health line says so honestly — a draft has no health
// until the row does.
func automationAddSeatbelt(w io.Writer, verb string, view automationview.View) error {
	return automationSeatbeltPrint(w, automationview.Describe(view), "no health yet — the row does not exist until you re-run with --yes", verb, view.ID)
}

func automationSeatbeltPrint(w io.Writer, desc, healthWords, verb, name string) error {
	line, err := automationAuthorityLine(desc)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, line)
	fmt.Fprintf(w, "health now: %s\n", healthWords)
	fmt.Fprintf(w, "%s grants or restores authority — re-run with --yes (nothing was written)\n", name)
	return fmt.Errorf("%s refused without --yes: the statement above is the whole deal", verb)
}

// --- words the printers read ------------------------------------------------------------

// automationStateWord is the list's state column: enabled, disabled,
// paused, or ARCHIVED — the archive is LOUD because it is the state the
// engine cannot see (AN6: paused and archived are both invisible, and the
// page that blurs them loses the difference between "not now" and
// "withdrawn").
func automationStateWord(v protocol.AutomationView) string {
	switch {
	case v.Archived:
		return "ARCHIVED"
	case v.Source == "event":
		if v.Enabled {
			return "enabled"
		}
		return "disabled"
	case v.Paused:
		return "paused"
	}
	return "enabled"
}

// automationHealthWords is one health view as one line: ok, or the dot
// word (MISSING/INVALID/unverifiable) with the daemon's reasons verbatim.
// The webhook clause names the ENV NAME and its presence — never a value
// (C17: a value here would be a second copy of a secret in a terminal
// scrollback).
func automationHealthWords(h protocol.AutomationHealthView) string {
	word := strings.ToUpper(h.Blueprint.State)
	if h.Blueprint.State == "" || h.Blueprint.State == "ok" {
		word = "ok"
	}
	if h.Webhook != nil {
		set := "UNSET"
		if h.Webhook.Set {
			set = "set"
		}
		word += fmt.Sprintf(", secret env %s: %s", h.Webhook.EnvName, set)
	}
	if !h.CanFire && len(h.Reasons) > 0 {
		word += " — " + strings.Join(h.Reasons, "; ")
	}
	return word
}

// automationViewOf maps the wire view into the Describe formatter's view —
// data plumbing across the two shapes, no judgment added (the daemon's
// reader maps the same fields from rows).
func automationViewOf(v protocol.AutomationView) automationview.View {
	out := automationview.View{
		Source: v.Source, ID: v.ID, Expr: v.Expr, Zone: v.Zone,
		Blueprint: v.Blueprint, Workspace: v.Workspace, Goal: v.Goal,
		Enabled: v.Enabled, Paused: v.Paused, Archived: v.Archived,
		DisabledReason: v.DisabledReason,
		MaxRunsPerHour: v.MaxRunsPerHour,
	}
	if v.Window != nil {
		out.WindowStart, out.WindowEnd, out.WindowDays = v.Window.Start, v.Window.End, v.Window.Days
	}
	if v.SecretEnv != "" {
		out.SecretEnv = v.SecretEnv
	}
	if v.MaxCostPerDayUSD != nil {
		out.MaxCostPerDayUSD = *v.MaxCostPerDayUSD
	}
	if len(v.Match) > 0 {
		out.Match = automationMatchView(v.Match)
	}
	return out
}

// automationMatchView reads the stored rule into the Describe formatter's
// shape. Parse failures render as an event name no grammar owns rather
// than a lie — the stored form is the validator's business, printed
// verbatim elsewhere (show carries the raw JSON).
func automationMatchView(raw json.RawMessage) automationview.MatchView {
	var m struct {
		Event      string          `json:"event"`
		Root       []string        `json:"root"`
		Component  []string        `json:"component"`
		GoalType   json.RawMessage `json:"goal_type"`
		TaskType   json.RawMessage `json:"task_type"`
		SourceKind json.RawMessage `json:"source_kind"`
	}
	_ = json.Unmarshal(raw, &m)
	return automationview.MatchView{Event: m.Event, Root: m.Root, Component: m.Component,
		GoalType: automationStringList(m.GoalType), TaskType: automationStringList(m.TaskType), SourceKind: automationStringList(m.SourceKind)}
}

func automationStringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	return nil
}

// automationDraftView builds the Describe input for an add-schedule /
// add-webhook form (the seatbelt's description, before any row exists).
func automationDraftView(webhook bool, id, blueprint, expr, zone, secretEnv, workspace, goal, winStart, winEnd, winDays string) automationview.View {
	v := automationview.View{ID: id, Blueprint: blueprint, Workspace: workspace, Goal: goal, Enabled: true}
	if webhook {
		v.Source, v.SecretEnv = "webhook", secretEnv
		return v
	}
	v.Source, v.Expr, v.Zone = "schedule", expr, zone
	v.WindowStart, v.WindowEnd, v.WindowDays = winStart, winEnd, winDays
	return v
}

func automationDraftEventView(name, matchJSON, blueprint string, maxRuns int) automationview.View {
	return automationview.View{Source: "event", ID: name, Blueprint: blueprint, Enabled: true,
		MaxRunsPerHour: maxRuns, Match: automationMatchView(json.RawMessage(matchJSON))}
}

// --- printers -----------------------------------------------------------------------------

func printAutomationList(w io.Writer, res protocol.AutomationListResult, health map[string]protocol.AutomationHealthView) {
	for _, v := range res.Automations {
		dot := "unverifiable"
		if h, ok := health[v.Source+":"+v.ID]; ok {
			dot = automationHealthWords(h)
		}
		fmt.Fprintf(w, "%-8s %-9s %-24s %-22s %s\n", automationStateWord(v), v.Source, v.ID, v.Blueprint, dot)
		fmt.Fprintf(w, "        %s\n", automationview.Describe(automationViewOf(v)))
		if v.LastFire != nil {
			fmt.Fprintf(w, "        last: %s %s — %s\n", v.LastFire.At, v.LastFire.Outcome, v.LastFire.Reason)
		} else {
			fmt.Fprintf(w, "        last: never\n")
		}
	}
	if res.Statement != "" {
		// AN5's honest empty, exactly one of the two sentences the daemon
		// chose — the printer adds nothing.
		fmt.Fprintln(w, res.Statement)
	}
}

func printAutomationShow(w io.Writer, res protocol.AutomationShowResult) {
	v := res.Automation
	fmt.Fprintf(w, "%s:%s — %s [%s]\n", v.Source, v.ID, automationStateWord(v), v.Name)
	fmt.Fprintln(w, automationview.Describe(automationViewOf(v)))
	fmt.Fprintf(w, "health: %s\n", automationHealthWords(res.Health))
	fmt.Fprintf(w, "created by %s (%s) at %s\n", v.CreatedByID, v.CreatedByKind, v.CreatedAt)
	if v.Archived && v.ArchivedReason != "" {
		fmt.Fprintf(w, "archived: %s\n", v.ArchivedReason)
	}
	if canon := res.Schedule; canon != nil {
		fmt.Fprintf(w, "canonical: cron %s in %s\n", canon.Expr, canon.Zone)
		if canon.Window != nil {
			fmt.Fprintf(w, "           window %s–%s days %s\n", canon.Window.Start, canon.Window.End, canon.Window.Days)
		}
	}
	if canon := res.Webhook; canon != nil {
		fmt.Fprintf(w, "canonical: secret env %s (the value lives in the daemon's environment)\n", canon.SecretEnv)
	}
	if canon := res.Event; canon != nil {
		fmt.Fprintf(w, "canonical: match %s\n", string(canon.Match))
		fmt.Fprintf(w, "           scope root %s component %s; kickoff %q\n", v.RootID, v.ComponentID, canon.Kickoff)
	}
	if v.MaxRunsPerHour > 0 || v.MaxCostPerDayUSD != nil {
		cost := "none"
		if v.MaxCostPerDayUSD != nil {
			cost = fmt.Sprintf("%.2f USD/day", *v.MaxCostPerDayUSD)
		}
		fmt.Fprintf(w, "limits: %d runs/hour, cost %s\n", v.MaxRunsPerHour, cost)
	}
	fmt.Fprintln(w, "\nrecent fires:")
	if len(res.RecentFires) == 0 {
		fmt.Fprintln(w, "  none — nothing has answered for this automation yet")
	}
	for _, f := range res.RecentFires {
		fmt.Fprintf(w, "  %-26s %-14s %s\n", f.At, f.Outcome, f.Reason)
	}
	fmt.Fprintln(w, "\nrecent changes:")
	if len(res.Changes) == 0 {
		fmt.Fprintln(w, "  none recorded")
	}
	for _, c := range res.Changes {
		fmt.Fprintf(w, "  %-26s %s %s — %s\n", c.OccurredAt, c.ActorID+"@"+c.ActorKind, c.Op, c.Summary)
	}
}

func printAutomationFires(w io.Writer, res protocol.AutomationFiresResult) {
	for _, f := range res.Fires {
		loop := ""
		if f.LoopID != "" {
			loop = " (loop " + f.LoopID + ")"
		}
		fmt.Fprintf(w, "%-26s %-9s %-14s %-13s %s%s\n", f.At, f.Source, f.TriggerID, f.Outcome, f.Reason, loop)
	}
	c := res.Counts
	fmt.Fprintf(w, "\nstarted %d · refused %d · limited %d · skipped %d · could-not-say %d\n", c.Started, c.Refused, c.Limited, c.Skipped, c.CouldNotSay)
	if res.NextBeforeId != "" {
		fmt.Fprintf(w, "more: aeon automation fires --before %q\n", res.NextBeforeId)
	}
}

func printAutomationRuns(w io.Writer, res protocol.AutomationRunsResult) {
	for _, l := range res.Loops {
		fmt.Fprintf(w, "%-28s %-22s %-14s %s\n", l.ID, l.Slug, l.Status, l.StartedAt)
	}
	fmt.Fprintf(w, "\n%d loop(s) started by this automation (total %d)\n", len(res.Loops), res.Total)
}

func printAutomationHealth(w io.Writer, res protocol.AutomationHealthResult) {
	if len(res.Health) == 0 {
		fmt.Fprintln(w, "no automation to check.")
		return
	}
	for _, h := range res.Health {
		word := automationHealthWords(h)
		mark := "MISSING"
		if h.CanFire {
			mark = "ok"
		}
		fmt.Fprintf(w, "%-9s %-24s %-11s %s\n", h.Source, h.ID, mark, word)
	}
	fmt.Fprintf(w, "\nadvice only — the fire log owns every outcome (AN8); evaluated %s\n", res.EvaluatedAt)
}

func printAutomationChanges(w io.Writer, res protocol.AutomationChangesResult) {
	for _, c := range res.Changes {
		fmt.Fprintf(w, "%-26s %-9s %-22s %-10s %s@%s: %s\n", c.OccurredAt, c.Source, c.TriggerID, c.Op, c.ActorID, c.ActorKind, c.Summary)
	}
	if res.Statement != "" {
		fmt.Fprintln(w, res.Statement)
	}
}

func printAutomationVocabulary(w io.Writer, v any) {
	voc := v.(automationview.VocabularyView)
	fmt.Fprintln(w, "events (with the keys that may carry values):")
	for _, e := range voc.Events {
		fmt.Fprintf(w, "  %-28s %s\n", e.Event, e.Meaning)
		fmt.Fprintf(w, "  %-28s keys: %s\n", "", strings.Join(e.ApplicableKeys, ", "))
	}
	fmt.Fprintf(w, "\nmatch keys: %s\n", strings.Join(voc.MatchKeys, ", "))
	fmt.Fprintf(w, "task types: %s\n", strings.Join(voc.TaskTypes, ", "))
	fmt.Fprintf(w, "goal types: %s\n", strings.Join(voc.GoalTypes, ", "))
	fmt.Fprintln(w, "outcomes:")
	for _, o := range voc.Outcomes {
		fmt.Fprintf(w, "  %-16s %s (%s)\n", o.Word, o.Meaning, o.Family)
	}
	fmt.Fprintf(w, "\nlimits: %d values/key · %d chars · %d preview instants · %d audit page · %d id length\n",
		voc.Limits.MaxValuesPerKey, voc.Limits.MaxValueLen, voc.Limits.PreviewMax, voc.Limits.AuditPageMax, voc.Limits.TriggerIDMaxLen)
	fmt.Fprintf(w, "window grammar: %s\n", voc.WindowGrammar)
	fmt.Fprintf(w, "zones: %d named (full list: --json)\n", len(voc.Zones))
	fmt.Fprintf(w, "\nauthority: %s\n", voc.AuthorityStatement)
}

func printAutomationPreview(w io.Writer, res protocol.AutomationPreviewResult) {
	if len(res.Instants) == 0 {
		// The honest empty the walk can answer within a legal spec (a
		// windowed schedule with no hit in the horizon) — never a silent
		// nothing.
		fmt.Fprintln(w, "no instants within the horizon — the walk ran the engine's own preview")
	}
	for _, in := range res.Instants {
		note := ""
		if in.Note != "" {
			note = " — " + in.Note
		}
		fmt.Fprintf(w, "%-34s %-8s %s%s\n", in.At, in.Kind, in.Wall, note)
	}
}

func printAutomationTest(w io.Writer, res protocol.AutomationTestResult) {
	yes := "does NOT match"
	if res.Matches {
		yes = "MATCHES"
	}
	fmt.Fprintf(w, "the sample %s\n", yes)
	for _, k := range res.PerKey {
		status := "ok"
		if !k.OK {
			status = "NO"
		}
		have := k.Have
		if have == "" {
			have = "—"
		}
		fmt.Fprintf(w, "  %-14s want %-24s have %-24s %s\n", k.Key, strings.Join(k.Want, "|"), have, status)
	}
	fmt.Fprintf(w, "stored form: %s\n", res.Canonical)
}

func printAutomationDryRun(w io.Writer, res protocol.AutomationDryRunResult) {
	for _, m := range res.WouldMatch {
		fmt.Fprintf(w, "  %-10d %-28s %-10s %s  %s\n", m.OutboxID, m.Event, m.SubjectKind, m.SubjectID, m.At)
	}
	fmt.Fprintf(w, "\nwould match %d of %d events scanned (outbox cursor %d — NOTHING was inserted; the dry run inserts nothing)\n",
		len(res.WouldMatch), res.Scanned, res.From)
}

func printAutomationEventView(w io.Writer, v protocol.EventTriggerView) {
	state := "enabled"
	if !v.Enabled {
		state = "disabled"
		if v.DisabledReason != "" {
			state += " — " + v.DisabledReason
		}
	}
	if v.LastFire == nil && state == "enabled" {
		// A just-created armed row reads honestly: it has been ARMED, not
		// proven — the first fire is the proof.
		state += " (armed; never fired yet)"
	}
	fmt.Fprintf(w, "event:%s %s — %s\n", v.ID, v.Name, state)
	fmt.Fprintln(w, automationview.Describe(automationview.View{Source: "event", ID: v.ID, Blueprint: v.Blueprint,
		Enabled: v.Enabled, MaxRunsPerHour: v.MaxRunsPerHour, Match: automationMatchView(v.Match)}))
}

func printAutomationUpdateResult(w io.Writer, res protocol.TriggerUpdateResult) {
	if !res.Changed {
		fmt.Fprintf(w, "%s: unchanged — the stored row already says that, no write happened\n", res.After.ID)
	} else {
		fmt.Fprintf(w, "%s: updated\n", res.After.ID)
	}
	if len(res.NextFires) > 0 {
		fmt.Fprint(w, "next: ")
		for i, in := range res.NextFires {
			if i > 0 {
				fmt.Fprint(w, ", ")
			}
			fmt.Fprint(w, in.At)
		}
		fmt.Fprintln(w)
	}
}

// printAutomationControlResult keeps trigger.go's exact bytes (the S20 goldens
// rule): the answer's own reason, the verb word only where trigger.go puts it.
func printAutomationControlResult(w io.Writer, res protocol.TriggerControlResult, verb string) {
	if verb != "" {
		if !res.Found {
			fmt.Fprintf(w, "%s\n", res.Reason)
			return
		}
		fmt.Fprintf(w, "%s: %s — %s\n", verb, res.TriggerID, res.Reason)
		return
	}
	// fire-now's printer (trigger.go's shape, moved here so trigger.go and
	// automation share ONE control renderer).
	if !res.Found {
		fmt.Fprintf(w, "%s\n", res.Reason)
		return
	}
	dash := " — "
	if res.Reason == "" {
		dash = ""
	}
	if res.LoopID == "" {
		fmt.Fprintf(w, "fire-now %s: %s%s%s\n", res.TriggerID, res.Outcome, dash, res.Reason)
		return
	}
	hand := "unattended" // TR2's fact, not a wire read — trigger.go's rule, kept.
	fmt.Fprintf(w, "fire-now %s: %s%s%s (loop %s, started as a trigger: %s)\n", res.TriggerID, res.Outcome, dash, res.Reason, res.LoopID, hand)
}

func printAutomationArchiveResult(w io.Writer, res protocol.AutomationArchiveResult, archive bool) {
	word := "unarchived"
	if archive {
		word = "archived"
	}
	if !res.Changed {
		fmt.Fprintf(w, "%s:%s: already %s — no write happened\n", res.After.Source, res.After.ID, word+"d")
		return
	}
	fmt.Fprintf(w, "%s:%s %s\n", res.After.Source, res.After.ID, word)
	if res.After.ArchivedReason != "" {
		fmt.Fprintf(w, "reason: %s\n", res.After.ArchivedReason)
	}
	fmt.Fprintf(w, "health now: %s\n", automationHealthWords(res.Health))
	if !archive {
		fmt.Fprint(w, "the row is back but NOT armed — enabling/resuming is a separate deliberate act (AN3)\n")
	}
}

// --- usage -------------------------------------------------------------------------------

func printAutomationUsage(w io.Writer) {
	fmt.Fprint(w, `aeon automation — one verb set over both families (S49): schedules, webhooks, events

  automation list      [--source schedule|webhook|event] [--enabled|--disabled]
                       [--archived include|only] [--blueprint N] [--component C]
                       [--q TEXT] [--json]
  automation show      SOURCE:ID [--json]     description, canonical form, health,
                                             recent fires, recent changes
  automation fires     [SOURCE:ID] [--outcome O] [--limit N] [--json]
  automation runs      SOURCE:ID [--limit N] [--json]
  automation health    [SOURCE:ID] [--json]
  automation changes   [SOURCE:ID] [--limit N] [--json]
  automation vocabulary [--json]
  automation preview   (SOURCE:ID | --cron EXPR --timezone ZONE [--window-*]) [-n N] [--json]
  automation test      --match JSON --sample JSON [--json]
  automation dryrun    SOURCE:ID [--since-outbox-id N] [--limit N] [--json]

  automation add-schedule --id ID --blueprint B --cron EXPR --timezone ZONE
                       [--workspace P --goal G --window-start/-end/-days …]
                       [--enabled --yes] [--json]
  automation add-webhook  --id ID --blueprint B --secret-env NAME
                       [--workspace P --goal G] [--enabled --yes] [--json]
  automation add-event    --name N --match JSON --blueprint B --max-runs-per-hour N
                       [--max-cost-per-day-usd X] [--component C --root R --kickoff T]
                       [--enabled --yes] [--json]
  automation update    SOURCE:ID [--blueprint|--cron|--timezone|--workspace|--goal|
                       --window-*|--clear-window|--secret-env|--name|--match|
                       --kickoff|--max-runs-per-hour|--max-cost-per-day-usd|
                       --clear-max-cost] [--json]
  automation enable    SOURCE:ID --yes        disable SOURCE:ID --reason R
  automation pause     SOURCE:ID             resume SOURCE:ID
  automation fire-now  SCHEDULE_ID --yes
  automation archive   SOURCE:ID --reason R   unarchive SOURCE:ID --yes

SOURCE:ID is schedule:nightly, webhook:ci, event:approved-features — the source
is part of the name (AN1). add-* WITHOUT --enabled creates the row disarmed
(paused/disabled through the same doors, audited); WITH --enabled it arms at
creation — an authority grant, so --yes. enable/fire-now/unarchive need --yes
likewise: without it the daemon's authority statement, the description and the
health print, nothing writes.

SECRETS: --secret-env takes a variable NAME. There is no flag, prompt or stdin
path for a secret VALUE — the daemon reads that name from its OWN environment,
and no answer in this plane can print what it holds (C17).

Without a running daemon the read verbs and the writes answer through the same
daemon services over the store directly (a fixed line says so first); fire-now
REFUSES offline — a loop needs the daemon's starter, and the refusal says so.
The clock's operational plane stays visible beside this one: aeon trigger …
is unchanged and speaks these same methods.
`)
}
