package main

// cmd/aeon/scheduler.go — S27 T9 (CN18, standing rule 1, P5's client half).
// The CLI is a CLIENT of the scheduler's socket doors and has nothing the
// daemon lacks: every verb dials, calls, prints, and re-validates nothing.
// A bad blueprint, window, or zone is the daemon's sentence verbatim; the
// only client-side refusals are inputs that cannot be a value at all (a
// non-RFC3339 --since, a malformed HH:MM-HH:MM window, a --with pair with
// no `=`, a --days with no --window), and the one piece of input sugar is
// --window expanding to the wire's start/end pair — the grammar itself is
// the daemon's.
//
// The recorded P5 deviation this slice owns (S60's EV18 precedent): there
// is NO offline door. A scheduler without a daemon is not a thing — "daemon
// not running" is the answer, never a direct write to a store the CLI must
// not import (TestSchedulerVerbsAreSocketClients is the teeth; schedDial is
// the only road out of this file).
//
// The actor is never a flag (S27 CN2): the connection resolves the person,
// and every control carries what the CONNECTION said, not what argv
// whispered. No flag, stdin or prompt takes a secret (SEC-033 — the shapes
// printed here carry ids, enums, counts, and paths only).
//
// The seatbelts (P5 — seatbelts, not security): `--yes` is required for
// enable, disable, pause, stop-all, repo set, repo disable, enable-tree.
// `enable` and `enable-tree` first print the daemon's OWN statement — read
// from scheduler.status — so the person sees what they are granting before
// anything can stand. `stop-all` is advertised only while a live probe
// reports the scheduler plane: a daemon that does not answer without the
// door does not advertise the verb.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// ---------------------------------------------------------------------------
// the parity fixtures (S27 T9): one slice per family naming each verb's
// socket method. The parity tests walk BOTH directions against these rows
// and against what the files actually call (the AST census), so a verb that
// drifts from its door — either way — fails a test, not a demo.
// ---------------------------------------------------------------------------

// cliVerb names one CLI verb and the socket method it drives. seatbelt is
// the `--yes` law of the spec's verb list, recorded next to the method so
// the two cannot be read apart.
type cliVerb struct {
	verb     string
	method   protocol.MethodName
	seatbelt bool
}

// schedulerVerbs is the whole `aeon scheduler` surface: twelve verbs, the
// twelve `scheduler.*` doors T7 registered, one each. No verb leads; no
// door lags (TestSchedulerVerbParityBothWays).
var schedulerVerbs = []cliVerb{
	{"status", protocol.MethodSchedulerStatus, false},
	{"preview", protocol.MethodSchedulerPreview, false},
	{"decisions", protocol.MethodSchedulerDecisions, false},
	{"attention", protocol.MethodSchedulerAttention, false},
	{"enable", protocol.MethodSchedulerEnable, true},
	{"disable", protocol.MethodSchedulerDisable, true},
	{"pause", protocol.MethodSchedulerPause, true},
	{"resume", protocol.MethodSchedulerResume, false},
	{"stop-all", protocol.MethodSchedulerStopAll, true},
	{"repo set", protocol.MethodSchedulerRootSet, true},
	{"repo disable", protocol.MethodSchedulerRootDisable, true},
	{"enable-tree", protocol.MethodSchedulerEnableTree, true},
}

// roadmapVerbs is `aeon roadmap`'s census: every verb and the door it
// speaks. S50's fifteen doors stand in S50's record; S27 adds exactly two
// rows (T3's socket doors, T9's client half) — the graph's edges are plain
// verbs with plain doors and no seatbelt, because an edge starts nothing
// (readiness is re-read at the attempt, D4).
var roadmapVerbs = []cliVerb{
	{"add", protocol.MethodRoadmapAdd, false},
	{"list", protocol.MethodRoadmapList, false},
	{"show", protocol.MethodRoadmapShow, false},
	{"cancel", protocol.MethodRoadmapCancel, false},
	{"estimate", protocol.MethodRoadmapEstimate, false},
	{"priority", protocol.MethodRoadmapPriority, false},
	{"update", protocol.MethodRoadmapUpdate, false},
	{"ideate", protocol.MethodRoadmapIdeate, false},
	{"plan", protocol.MethodRoadmapPlan, false},
	{"materialize", protocol.MethodRoadmapMaterialize, false},
	{"history", protocol.MethodRoadmapHistory, false},
	{"board", protocol.MethodRoadmapBoard, false},
	{"health", protocol.MethodRoadmapHealth, false},
	{"vocabulary", protocol.MethodRoadmapVocabulary, false},
	{"depend", protocol.MethodRoadmapDepend, false},
	{"undepend", protocol.MethodRoadmapUndepend, false},
}

// schedDial is the dial every verb shares; a var so the in-process tests
// can stand a daemon behind it (the mqDial / historyDial idiom). RAW dial:
// the standard "daemon not running" words are dial's own; this file adds no
// notice and no offline arm (the recorded P5 deviation).
var schedDial = dial

// schedSeatbelt is the refusal the seven gated verbs answer with when the
// flag is missing — it NAMES the flag it wants, because the spec's test
// refuses any refusal that withholds it.
func schedSeatbelt(verb string) error {
	return &usageError{msg: fmt.Sprintf("`aeon scheduler %s` needs --yes — read the daemon's statement and confirm; the seatbelt is a habit, not a security control (S27 P5)", verb)}
}

func cmdScheduler(w io.Writer, args []string) error {
	if len(args) == 0 {
		printSchedulerUsage(w)
		return nil
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "status":
		return schedVerbStatus(w, rest)
	case "preview":
		return schedVerbPreview(w, rest)
	case "decisions":
		return schedVerbDecisions(w, rest)
	case "attention":
		return schedVerbAttention(w, rest)
	case "enable":
		return schedVerbEnable(w, rest)
	case "disable":
		return schedVerbDisable(w, rest)
	case "pause":
		return schedVerbPause(w, rest)
	case "resume":
		return schedVerbResume(w, rest)
	case "stop-all":
		return schedVerbStopAll(w, rest)
	case "repo":
		return cmdSchedulerRepo(w, rest)
	case "enable-tree":
		return schedVerbEnableTree(w, rest)
	case "help":
		printSchedulerUsage(w)
		return nil
	default:
		printSchedulerUsage(w)
		return &usageError{msg: fmt.Sprintf("unknown `scheduler` subcommand %q — `aeon scheduler help` lists the twelve", verb)}
	}
}

// ---------------------------------------------------------------------------
// usage — rendered from a live probe (the mergequeue vocabulary-in-help
// shape): stop-all is advertised only while the daemon reports the
// scheduler plane; a daemon that lacks the door does not advertise it.
// ---------------------------------------------------------------------------

const schedStopAllLine = "  stop-all    --reason TEXT --yes — pause AND stop every live loop\n"

const schedStopAllHiddenLine = "  stop-all    — listed only when the daemon reports the scheduler plane; this one does not\n"

func printSchedulerUsage(w io.Writer) {
	stopAllLine := schedStopAllHiddenLine
	if _, err := schedStatusProbe(); err == nil {
		stopAllLine = schedStopAllLine
	}
	fmt.Fprint(w, `usage: aeon scheduler <verb> — the person's doors onto the continuous scheduler (S27)

  status      [--json] — master, pause, settings, slots, repositories, attention
  preview     [--component PATH | --root ID] [--json] — what the next tick would do
  decisions   [--component PATH] [--subject kind/id] [--decision D] [--reason R]
              [--since RFC3339] [--limit N] [--json] — the ledger, newest first
  attention   [--component PATH | --root ID] [--json] — what waits on a person
  enable      --yes — the master switch; prints the daemon's authority statement first
  disable     --yes — the master off: nothing starts until enable
  pause       --reason TEXT --yes — stop picking; running loops keep running
  resume      — pick again
`+stopAllLine+`  repo set     --component PATH --blueprint NAME [--tech T] [--with k=v,...]
               [--window HH:MM-HH:MM --days 1-5 --zone Z]
               [--min-priority known|assumed] [--enable] --yes
  repo disable --component PATH --yes
  enable-tree  --root ID --blueprint NAME [--tech T] [--with k=v,...]
               [--window HH:MM-HH:MM --days 1-5 --zone Z]
               [--min-priority known|assumed] --yes

  Every verb is a client of the daemon's socket — there is no offline door:
  a scheduler without a daemon is not a thing (the recorded P5 deviation).
  The daemon's own sentences answer every refusal; the CLI re-validates
  nothing except inputs that cannot be a value. --yes is a seatbelt, not a
  security control (S27 P5).
`)
}

// schedStatusProbe is the usage probe AND enable/enable-tree's statement
// read: one dial, one scheduler.status. An error is an answer, not a bug —
// pre-S27 daemons and cold endpoints both mean "do not advertise, do not
// grant".
func schedStatusProbe() (protocol.ContinuousStatusView, error) {
	c, err := schedDial(0)
	if err != nil {
		return protocol.ContinuousStatusView{}, err
	}
	defer c.Close()
	var view protocol.ContinuousStatusView
	if err := c.Call(protocol.MethodSchedulerStatus, struct{}{}, &view); err != nil {
		return protocol.ContinuousStatusView{}, err
	}
	return view, nil
}

// ---------------------------------------------------------------------------
// the four reads
// ---------------------------------------------------------------------------

func schedVerbStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler status", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler status takes no positional arguments — `aeon scheduler status [--json]`"}
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodSchedulerStatus, struct{}{})
	}
	var view protocol.ContinuousStatusView
	if err := c.Call(protocol.MethodSchedulerStatus, struct{}{}, &view); err != nil {
		return err
	}
	renderSchedulerStatus(w, view)
	return nil
}

// renderSchedulerStatus is the fixed human template over the wire fields.
// Every word that is not a label is the daemon's: the state kind, the
// reason, the settings' source words, the pause reason. Nothing here
// computes, re-words, or re-sorts (CN18; rule 4).
func renderSchedulerStatus(w io.Writer, v protocol.ContinuousStatusView) {
	master := "disabled"
	if v.Enabled {
		master = "enabled"
	}
	fmt.Fprintf(w, "master: %s\n", master)
	if v.Paused {
		fmt.Fprintf(w, "paused: yes — %s\n", v.PausedReason)
	} else {
		fmt.Fprintf(w, "paused: no\n")
	}
	fmt.Fprintf(w, "slots: %d/%d · next tick: %s · attention: %d open\n",
		v.Slots.Used, v.Slots.Cap, v.NextTickAt, v.Attention.Open)
	for _, s := range v.Settings {
		fmt.Fprintf(w, "setting %s = %s (%s)\n", s.Key, s.Value, s.Source)
	}
	if len(v.Repos) == 0 {
		fmt.Fprintf(w, "repositories: none — `aeon scheduler repo set` files the first row\n")
		return
	}
	fmt.Fprintf(w, "repositories (%d):\n", len(v.Repos))
	for _, r := range v.Repos {
		renderSchedulerRepo(w, r, "  ")
	}
}

// renderSchedulerRepo prints one repository row: state and reason in the
// daemon's own sentence, template and window as the row carries them.
func renderSchedulerRepo(w io.Writer, r protocol.ContinuousRepoView, indent string) {
	enabled := "disabled"
	if r.Enabled {
		enabled = "enabled"
	}
	line := fmt.Sprintf("%s%s · %s · blueprint %s", indent, r.ComponentRoot, enabled, r.Template.Blueprint)
	if r.Template.Tech != "" {
		line += "/" + r.Template.Tech
	}
	if r.Window != nil {
		line += fmt.Sprintf(" · window %s-%s", r.Window.Start, r.Window.End)
		if r.Window.Days != "" {
			line += " " + r.Window.Days
		}
		if r.Window.Zone != "" {
			line += " " + r.Window.Zone
		}
	}
	if r.MinPriority != "" {
		line += " · min-priority " + r.MinPriority
	}
	if r.LastStartAt != "" {
		line += " · last start " + r.LastStartAt
	}
	fmt.Fprintln(w, line)
	state := r.State.Reason
	if state == "" {
		state = r.State.Detail
	}
	if state != "" {
		fmt.Fprintf(w, "%s  state: %s — %s\n", indent, r.State.Kind, state)
	} else {
		fmt.Fprintf(w, "%s  state: %s\n", indent, r.State.Kind)
	}
}

// schedRepoFilterParams is the preview/attention wire shape: the two
// spellings of "which repository" as pointers — an absent flag sends
// nothing, and BOTH flags sends both and lets the daemon refuse verbatim.
type schedRepoFilterParams struct {
	ComponentRoot *string `json:"componentRoot,omitempty"`
	RootID        *string `json:"rootId,omitempty"`
}

func schedRepoFlags(fs *flag.FlagSet) (comp, root *string) {
	return fs.String("component", "", "the component root to answer for (default: the whole machine)"),
		fs.String("root", "", "the tree root id to answer for (root:ID on the wire) — name one of --component/--root")
}

func schedRepoParams(comp, root *string) schedRepoFilterParams {
	p := schedRepoFilterParams{}
	if comp != nil && *comp != "" {
		v := *comp
		p.ComponentRoot = &v
	}
	if root != nil && *root != "" {
		v := *root
		p.RootID = &v
	}
	return p
}

func schedVerbPreview(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler preview", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	comp, root := schedRepoFlags(fs)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler preview takes no positional arguments — `aeon scheduler preview [--component PATH | --root ID] [--json]`"}
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	p := schedRepoParams(comp, root)
	if *asJSON {
		return callJSON(w, c, protocol.MethodSchedulerPreview, p)
	}
	var view protocol.ContinuousPreview
	if err := c.Call(protocol.MethodSchedulerPreview, p, &view); err != nil {
		return err
	}
	fmt.Fprintf(w, "now: %s\n", view.Now)
	fmt.Fprintf(w, "would start (%d):\n", len(view.WouldStart))
	for _, l := range view.WouldStart {
		renderSchedulerPreviewLine(w, "  ", l)
	}
	var notStarting []protocol.ContinuousPreviewLine
	for _, l := range view.Decisions {
		if l.Verdict != "start" {
			notStarting = append(notStarting, l)
		}
	}
	fmt.Fprintf(w, "not starting (%d):\n", len(notStarting))
	for _, l := range notStarting {
		renderSchedulerPreviewLine(w, "  ", l)
	}
	return nil
}

// renderSchedulerPreviewLine prints "reason — detail" in the daemon's own
// words (the spec's example shape: `window-closed — opens Mon 09:00
// Europe/Lisbon`). The verdict word rides the row for the eye; it is the
// daemon's raw word, never a re-formatted one.
func renderSchedulerPreviewLine(w io.Writer, indent string, l protocol.ContinuousPreviewLine) {
	line := fmt.Sprintf("%s%s %s/%s · %s", indent, l.Verdict, l.SubjectKind, l.SubjectID, l.Reason)
	if l.Detail != "" {
		line += " — " + l.Detail
	}
	if l.ComponentRoot != "" {
		line += " [" + l.ComponentRoot + "]"
	}
	fmt.Fprintln(w, line)
}

// schedDecisionsParams is the ledger read's wire shape (T7's door shape;
// the filter grammar is the daemon's — the CLI passes strings through).
type schedDecisionsParams struct {
	ComponentRoot string `json:"componentRoot"`
	Subject       string `json:"subject"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	Since         string `json:"since"`
	Limit         *int   `json:"limit"`
}

func schedVerbDecisions(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler decisions", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	comp := fs.String("component", "", "narrow to one component root (default: every repository the daemon answers for)")
	subject := fs.String("subject", "", `narrow to one subject, spelled "kind/id" — kind is one of queue, roadmap, root, daemon (the daemon's vocabulary; it names the refusal)`)
	decision := fs.String("decision", "", "narrow to one ledger word (started, deferred, skipped, refused, needs-you, …)")
	reason := fs.String("reason", "", "narrow to one reason word (capacity, window-closed, …)")
	since := fs.String("since", "", "only rows at or after this RFC3339 instant (a value that cannot parse here can never parse there)")
	limit := fs.Int("limit", 0, "at most N rows, newest first (the daemon bounds 1..500 and names the bound)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler decisions takes no positional arguments — see `aeon scheduler help`"}
	}
	p := schedDecisionsParams{ComponentRoot: *comp, Subject: *subject, Decision: *decision, Reason: *reason}
	if *since != "" {
		// The one documented client-side refusal: a non-RFC3339 --since
		// cannot be a value anywhere, and the flag doc names the format.
		if _, err := time.Parse(time.RFC3339, *since); err != nil {
			return &usageError{msg: fmt.Sprintf("`--since %s` is not RFC3339 (like 2026-11-05T14:00:00Z) — the daemon would refuse the same words", *since)}
		}
		p.Since = *since
	}
	if fs.Lookup("limit").Value.String() != "0" {
		v := *limit
		p.Limit = &v
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if *asJSON {
		return callJSON(w, c, protocol.MethodSchedulerDecisions, p)
	}
	var rows []protocol.ContinuousDecisionRow
	if err := c.Call(protocol.MethodSchedulerDecisions, p, &rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintf(w, "no decisions match — the ledger answers honestly empty, and the filters were %s\n", strings.TrimSpace(
			fmt.Sprintf("component=%q subject=%q decision=%q reason=%q since=%q", p.ComponentRoot, p.Subject, p.Decision, p.Reason, p.Since)))
		return nil
	}
	for _, r := range rows {
		line := fmt.Sprintf("%s · %s · %s/%d · %s — %s", r.At, r.Decision, r.SubjectKind, r.SubjectID, r.Reason, r.Summary)
		if r.LoopSlug != "" {
			line += " · loop " + r.LoopSlug
		}
		if r.GrantRevision != "" {
			line += " · grant " + r.GrantRevision
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

func schedVerbAttention(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler attention", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim instead of the table")
	comp, root := schedRepoFlags(fs)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler attention takes no positional arguments — `aeon scheduler attention [--component PATH | --root ID] [--json]`"}
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	p := schedRepoParams(comp, root)
	if *asJSON {
		return callJSON(w, c, protocol.MethodSchedulerAttention, p)
	}
	var att protocol.ContinuousAttention
	if err := c.Call(protocol.MethodSchedulerAttention, p, &att); err != nil {
		return err
	}
	fmt.Fprintf(w, "open: %d\n", att.Open)
	for _, it := range att.Items {
		line := fmt.Sprintf("#%d · %s · %s", it.DecisionID, it.ComponentRoot, it.Kind)
		if it.LoopSlug != "" {
			line += " · loop " + it.LoopSlug
		}
		if it.Note != "" {
			line += " — " + it.Note
		}
		if it.Since != "" {
			line += " · since " + it.Since
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// ---------------------------------------------------------------------------
// the master switch
// ---------------------------------------------------------------------------

// schedVerbEnable is the one verb that MUST make the person read before
// they grant: the daemon's statement rides scheduler.status and prints
// first — with --yes or without it. Without, the refusal names --yes and
// nothing is sent; with it, the enable call follows the statement, in that
// order (the parity test watches the fake service's call log).
func schedVerbEnable(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler enable", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: you have read the daemon's authority statement printed above")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler enable takes no positional arguments — `aeon scheduler enable --yes`"}
	}
	view, err := schedStatusProbe()
	if err != nil {
		return err
	}
	schedPrintStatement(w, view.Statement)
	if !*yes {
		return schedSeatbelt("enable")
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res map[string]any
	if err := c.Call(protocol.MethodSchedulerEnable, struct{}{}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "master: enabled\n")
	return nil
}

// schedVerbDisable needs no statement — turning authority OFF is the act
// the seatbelt alone covers (S27 T9's verb law).
func schedVerbDisable(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler disable", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: the scheduler stops starting work the moment this lands")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler disable takes no positional arguments — `aeon scheduler disable --yes`"}
	}
	if !*yes {
		return schedSeatbelt("disable")
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Call(protocol.MethodSchedulerDisable, struct{}{}, new(map[string]any)); err != nil {
		return err
	}
	fmt.Fprintf(w, "master: disabled\n")
	return nil
}

func schedVerbPause(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler pause", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: pause stops PICKING; running loops keep running")
	reason := fs.String("reason", "", "why the scheduler pauses — the audit's own words (required by the daemon; its refusal is verbatim)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler pause takes no positional arguments — `aeon scheduler pause --reason TEXT --yes`"}
	}
	if !*yes {
		return schedSeatbelt("pause")
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res map[string]any
	if err := c.Call(protocol.MethodSchedulerPause, struct {
		Reason string `json:"reason"`
	}{*reason}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "paused: true\n")
	if s, ok := res["runningLoops"].(string); ok && s != "" {
		fmt.Fprintf(w, "running loops: %s\n", s) // the daemon's sentence, verbatim
	}
	return nil
}

func schedVerbResume(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler resume", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler resume takes no positional arguments — `aeon scheduler resume`"}
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Call(protocol.MethodSchedulerResume, struct{}{}, new(map[string]any)); err != nil {
		return err
	}
	fmt.Fprintf(w, "paused: false — the scheduler picks again\n")
	return nil
}

// ---------------------------------------------------------------------------
// the off switch
// ---------------------------------------------------------------------------

func schedVerbStopAll(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler stop-all", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: this pauses the scheduler AND stops every live loop")
	reason := fs.String("reason", "", "why the switch is thrown — the audit's own words (required by the daemon; its refusal is verbatim)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler stop-all takes no positional arguments — `aeon scheduler stop-all --reason TEXT --yes`"}
	}
	if !*yes {
		return schedSeatbelt("stop-all")
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res protocol.ContinuousStopAllResult
	if err := c.Call(protocol.MethodSchedulerStopAll, struct {
		Reason string `json:"reason"`
	}{*reason}, &res); err != nil {
		return err
	}
	paused := "false"
	if res.Paused {
		paused = "true"
	}
	fmt.Fprintf(w, "paused: %s · stopped %d loop(s)\n", paused, len(res.Stopped))
	for _, s := range res.Stopped {
		fmt.Fprintf(w, "  stopped: %s\n", s)
	}
	for _, f := range res.Failures {
		fmt.Fprintf(w, "  failure: %s\n", f) // the daemon's sentence, verbatim
	}
	if len(res.Failures) > 0 {
		fmt.Fprintf(w, "the pause stands even on partial failure — a half-off switch is still off (CN18)\n")
	}
	return nil
}

// ---------------------------------------------------------------------------
// the repository doors
// ---------------------------------------------------------------------------

func cmdSchedulerRepo(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{"`aeon scheduler repo` takes set or disable — see `aeon scheduler help`"}
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "set":
		return schedVerbRepoSet(w, rest)
	case "disable":
		return schedVerbRepoDisable(w, rest)
	default:
		return &usageError{msg: fmt.Sprintf("unknown `scheduler repo` subcommand %q — set or disable", verb)}
	}
}

// schedWindowRegexp is the input sugar the spec names: HH:MM-HH:MM,
// expanded client-side to the wire's start/end pair. A value that cannot
// split is refused HERE — it could not be a value anywhere (CN5's pair
// rule is still the daemon's to enforce on what remains).
var schedWindowRegexp = regexp.MustCompile(`^([0-2][0-9]:[0-5][0-9])-([0-2][0-9]:[0-5][0-9])$`)

func schedWindowFlag(fs *flag.FlagSet, days, zone *string) (*string, func() (*protocol.ContinuousWindow, error)) {
	win := fs.String("window", "", "the quiet hours as HH:MM-HH:MM — expanded client-side to the wire's start/end (the grammar itself is the daemon's)")
	build := func() (*protocol.ContinuousWindow, error) {
		if *win == "" {
			if *days != "" || *zone != "" {
				return nil, &usageError{"--days and --zone qualify a --window; without one they cannot be a value"}
			}
			return nil, nil
		}
		m := schedWindowRegexp.FindStringSubmatch(*win)
		if m == nil {
			return nil, &usageError{msg: fmt.Sprintf("`--window %s` is not HH:MM-HH:MM (like 09:00-18:00) — the daemon refuses what does not split either", *win)}
		}
		return &protocol.ContinuousWindow{Start: m[1], End: m[2], Days: *days, Zone: *zone}, nil
	}
	return win, build
}

// schedWithFlag decodes `k=v,k=v` into the wire's JSON object. A pair with
// no `=` cannot be a value; anything beyond SHAPE is the daemon's to
// refuse (the template validator owns the content, CN5).
func schedWithFlag(fs *flag.FlagSet) (*string, func() (json.RawMessage, error)) {
	with := fs.String("with", "", "fixed template pairs, k=v,k=v (an object of strings on the wire; the daemon validates content)")
	build := func() (json.RawMessage, error) {
		if *with == "" {
			return nil, nil
		}
		pairs := map[string]string{}
		for _, kv := range strings.Split(*with, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return nil, &usageError{msg: fmt.Sprintf("`--with %s` carries a pair with no `=` — k=v,k=v is the shape that can reach the daemon at all", *with)}
			}
			pairs[k] = v
		}
		raw, err := json.Marshal(pairs)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(raw), nil
	}
	return with, build
}

// schedRootSetParams is the scheduler.root.set wire shape (T7's door
// params; enabled is a pointer for the daemon's silent-enable law — the
// CLI always names it: --enable sends true, its absence sends false and
// the usage says so).
type schedRootSetParams struct {
	ComponentRoot string                     `json:"componentRoot"`
	Enabled       *bool                      `json:"enabled"`
	Blueprint     string                     `json:"blueprint"`
	Tech          string                     `json:"tech"`
	With          json.RawMessage            `json:"with,omitempty"`
	Window        *protocol.ContinuousWindow `json:"window,omitempty"`
	MinPriority   string                     `json:"minPriority"`
}

func schedVerbRepoSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler repo set", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: you have read the daemon's authority statement printed above")
	comp := fs.String("component", "", "the repository's component root (required; the daemon names the refusal)")
	blueprint := fs.String("blueprint", "", "the template blueprint every start in this repository runs (CN5: no template, no enable)")
	tech := fs.String("tech", "", "the tech the template takes (optional)")
	withRaw, buildWith := schedWithFlag(fs)
	_ = withRaw
	days := fs.String("days", "", "window days, e.g. 1-5 (Mon-Fri; the daemon's grammar)")
	zone := fs.String("zone", "", "window zone, e.g. Europe/Lisbon (the daemon's grammar)")
	win, buildWin := schedWindowFlag(fs, days, zone)
	_ = win
	minPriority := fs.String("min-priority", "", "known or assumed — how sure the priority must be before a start (the daemon's closed set)")
	enable := fs.Bool("enable", false, "file the row ENABLED; omitting this flags writes an enabled=off row (the daemon refuses a silent default)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler repo set takes no positional arguments — see `aeon scheduler help`"}
	}
	if !*yes {
		// The statement comes before the seatbelt check so the refusal is
		// always read against what enabling WOULD grant (enable-tree too).
		view, err := schedStatusProbe()
		if err != nil {
			return err
		}
		schedPrintStatement(w, view.Statement)
		return schedSeatbelt("repo set")
	}
	view, err := schedStatusProbe()
	if err != nil {
		return err
	}
	schedPrintStatement(w, view.Statement)
	window, err := buildWin()
	if err != nil {
		return err
	}
	with, err := buildWith()
	if err != nil {
		return err
	}
	p := schedRootSetParams{
		ComponentRoot: *comp, Enabled: enable, Blueprint: *blueprint,
		Tech: *tech, With: with, Window: window, MinPriority: *minPriority,
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var out protocol.ContinuousRepoView
	if err := c.Call(protocol.MethodSchedulerRootSet, p, &out); err != nil {
		return err
	}
	renderSchedulerRepo(w, out, "")
	return nil
}

func schedVerbRepoDisable(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler repo disable", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: this repository's rows stop feeding the scheduler")
	comp := fs.String("component", "", "the repository's component root (required; the daemon names the refusal)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler repo disable takes no positional arguments — `aeon scheduler repo disable --component PATH --yes`"}
	}
	if !*yes {
		return schedSeatbelt("repo disable")
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res map[string]any
	if err := c.Call(protocol.MethodSchedulerRootDisable, struct {
		ComponentRoot string `json:"componentRoot"`
	}{*comp}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s: disabled\n", *comp)
	return nil
}

// schedEnableTreeParams is the CN2 bulk-expansion wire shape.
type schedEnableTreeParams struct {
	RootID      string                     `json:"rootId"`
	Blueprint   string                     `json:"blueprint"`
	Tech        string                     `json:"tech"`
	With        json.RawMessage            `json:"with,omitempty"`
	Window      *protocol.ContinuousWindow `json:"window,omitempty"`
	MinPriority string                     `json:"minPriority"`
}

func schedVerbEnableTree(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scheduler enable-tree", flag.ContinueOnError)
	fs.SetOutput(w)
	yes := fs.Bool("yes", false, "confirm: you have read the daemon's authority statement printed above")
	root := fs.String("root", "", "the tree root id whose repositories are expanded, one row each (required; the daemon names the refusal)")
	blueprint := fs.String("blueprint", "", "the template filed into every row (CN5: no template, no enable; required)")
	tech := fs.String("tech", "", "the tech the template takes (optional)")
	withRaw, buildWith := schedWithFlag(fs)
	_ = withRaw
	days := fs.String("days", "", "window days, e.g. 1-5 (the daemon's grammar)")
	zone := fs.String("zone", "", "window zone, e.g. Europe/Lisbon (the daemon's grammar)")
	win, buildWin := schedWindowFlag(fs, days, zone)
	_ = win
	minPriority := fs.String("min-priority", "", "known or assumed (the daemon's closed set)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"scheduler enable-tree takes no positional arguments — see `aeon scheduler help`"}
	}
	view, err := schedStatusProbe()
	if err != nil {
		return err
	}
	schedPrintStatement(w, view.Statement)
	if !*yes {
		return schedSeatbelt("enable-tree")
	}
	window, err := buildWin()
	if err != nil {
		return err
	}
	with, err := buildWith()
	if err != nil {
		return err
	}
	c, err := schedDial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var res struct {
		Roots []string `json:"roots"`
		Count int      `json:"count"`
		Note  string   `json:"note"`
	}
	if err := c.Call(protocol.MethodSchedulerEnableTree, schedEnableTreeParams{
		RootID: *root, Blueprint: *blueprint, Tech: *tech,
		With: with, Window: window, MinPriority: *minPriority,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "enabled %d row(s):\n", res.Count)
	for _, r := range res.Roots {
		fmt.Fprintf(w, "  %s\n", r)
	}
	if res.Note != "" {
		fmt.Fprintf(w, "%s\n", res.Note) // the daemon's CN6 sentence, verbatim
	}
	return nil
}

// schedPrintStatement prints the daemon's authority statement — the ONE
// string the UI quotes too (RM9's law, kept by S27): the CLI never
// paraphrases it.
func schedPrintStatement(w io.Writer, statement string) {
	if statement == "" {
		return
	}
	fmt.Fprintf(w, "the daemon's own statement of what this grants:\n  %s\n", statement)
}
