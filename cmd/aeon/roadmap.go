package main

// The `aeon roadmap` verb — the intentions a project holds BEFORE they are
// even queued (S26). This is a CLIENT of the daemon's `roadmap.*` methods
// and nothing else: it dials the same socket the app speaks (P34's
// ordering, CG9's one implementation underneath — brief §5's D5 grants
// this surface the same departure PL15 once was), opens no store, and
// re-validates no law. The eight-status machine (R1), D2's three-state
// priority, D3's estimate shape and D4's readiness re-check are decided
// once, underneath, at the door and the CHECK; the sentences of refusal
// ride out of here verbatim.
//
// What this surface owns: argument shape and the honest print — the
// priority is ALWAYS its own word (unknown says "unknown", never blank),
// the estimate is the range with its basis or a sentence saying it is not
// sized (D3/P32), and the empty roadmap says so while saying what did NOT
// change: loop.start was never asked about any of this (D8).

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdRoadmap(w io.Writer, args []string) error {
	if len(args) == 0 {
		printRoadmapUsage(w)
		return nil
	}
	switch args[0] {
	case "add":
		return cmdRoadmapAdd(w, args[1:])
	case "list":
		return cmdRoadmapList(w, args[1:])
	case "show":
		return cmdRoadmapShow(w, args[1:])
	case "cancel":
		return cmdRoadmapCancel(w, args[1:])
	case "depend":
		return cmdRoadmapDepend(w, args[1:])
	case "undepend":
		return cmdRoadmapUndepend(w, args[1:])
	case "estimate":
		return cmdRoadmapEstimate(w, args[1:])
	case "priority":
		return cmdRoadmapPriority(w, args[1:])
	case "update":
		return cmdRoadmapUpdate(w, args[1:])
	case "history":
		return cmdRoadmapHistory(w, args[1:])
	case "board":
		return cmdRoadmapBoard(w, args[1:])
	case "health":
		return cmdRoadmapHealth(w, args[1:])
	case "vocabulary":
		return cmdRoadmapVocabulary(w, args[1:])
	case "ideate":
		return cmdRoadmapIdeate(w, args[1:])
	case "plan":
		return cmdRoadmapPlan(w, args[1:])
	case "materialize":
		return cmdRoadmapMaterialize(w, args[1:])
	default:
		printRoadmapUsage(w)
		return fmt.Errorf("unknown `roadmap` subcommand %q", args[0])
	}
}

func printRoadmapUsage(w io.Writer) {
	fmt.Fprint(w, `aeon roadmap — the intentions before they are even queued (S26)

  roadmap add --title TEXT --provenance authored|triggered|triaged
              [--epic E] [--priority known|assumed|unknown] [--priority-label L]
              [--estimate-min N --estimate-max N --estimate-basis "…"]
              [--component ROOT]
  roadmap list [--component ROOT] [--epic E] [--status S] [--priority P]
  roadmap show ID
  roadmap cancel ID
  roadmap depend ID --on ID2      (S27 T3: ID waits for ID2 — an edge of intent; readiness is decided at the attempt, never stored)
  roadmap undepend ID --on ID2    (S27 T3: remove exactly that one edge; no cascade, no loop touched)
  roadmap estimate ID --min N --max N --basis "…"
  roadmap priority ID --state known|assumed|unknown [--label L]
  roadmap update ID [--title T] [--epic E] [--clear-epic]
              [--brief-path P] [--plan-path P]
  roadmap history ID [--limit N]
  roadmap board [--component ROOT] [--status S] [--epic E] [--json]
  roadmap health
  roadmap vocabulary
  roadmap ideate ID --workspace DIR [--blueprint NAME]
              [--attended | --attended=false] [--yes]
  roadmap plan ID --workspace DIR [--yes]
  roadmap materialize ID --blueprint NAME --goal TEXT --workspace DIR
              [--tech T] [--with k=v]… [--attended | --attended=false] [--yes]

An item walks R1's eight statuses and nothing else decides them: idea →
ideating → brief-agreed → planned → ready → running → done, cancelled off
every pre-loop rung. ready means brief AND plan AND a priority that is not
unknown — re-checked at the attempt, never cached (D4). Provenance has no
default (D5): the writer answers where the item came from. Materialize is
queue-first: a project with queue rows hands the loop.start payload to
queue.append and S24's ordering decides WHEN (appending starts nothing); a
project without one starts the loop directly — one door either way (R4).
loop.start itself never asks about any of this (D8).
ideate, plan and materialize are DISPATCHES (S50 RM9): they print the
daemon's authority statement beside the item's own describe and act only
on --yes — without it they write nothing. update, history, board, health
and vocabulary answer through the direct door when no daemon is running,
saying so first; the dispatches never fall back — no daemon, no loops.
`)
}

// peelRoadmapID takes the leading positional id (the queue verb's house
// shape: the id first, then flags).
func peelRoadmapID(args []string) (int64, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, args
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, args
	}
	return id, args[1:]
}

func printRoadmapItem(w io.Writer, it protocol.RoadmapItemView) {
	fmt.Fprintf(w, "#%d · %-12s · %s · %s", it.ID, it.Status, it.PriorityState, it.Title)
	if it.PriorityLabel != nil {
		fmt.Fprintf(w, " (%s)", *it.PriorityLabel)
	}
	if it.Epic != "" {
		fmt.Fprintf(w, " · epic=%s", it.Epic)
	}
	if it.EstimateMin != nil && it.EstimateMax != nil && it.EstimateBasis != nil {
		fmt.Fprintf(w, " · %d–%d (%s)", *it.EstimateMin, *it.EstimateMax, *it.EstimateBasis)
	}
	if it.Provenance != "" {
		fmt.Fprintf(w, " · from %s", it.Provenance)
	}
	if it.BriefPath != "" {
		fmt.Fprintf(w, " · brief=%s", it.BriefPath)
	}
	if it.PlanPath != "" {
		fmt.Fprintf(w, " · plan=%s", it.PlanPath)
	}
	if it.LoopSlug != "" {
		fmt.Fprintf(w, " · loop=%s", it.LoopSlug)
	}
	// S27 T3: the graph, printed only when there is one — an edge-less item
	// renders byte-identically to every pre-S27 golden, and the daemon is
	// what read the graph (the client computes nothing, CN16's honesty).
	if len(it.Dependencies) > 0 {
		waits := make([]string, 0, len(it.Dependencies))
		for _, d := range it.Dependencies {
			waits = append(waits, fmt.Sprintf("#%d(%s)", d.ID, d.Status))
		}
		fmt.Fprintf(w, " · waits for %s", strings.Join(waits, " "))
	}
	if it.Dependents > 0 {
		fmt.Fprintf(w, " · %d wait on it", it.Dependents)
	}
	fmt.Fprintln(w)
}

func cmdRoadmapAdd(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap add", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	title := fs.String("title", "", "what the person wants — required, and what the board renders (required)")
	epic := fs.String("epic", "", "optional grouping label (a string, not a record — S38's epic is a different record)")
	priority := fs.String("priority", "", "known | assumed | unknown — omitted is the honest unknown (D2), never a silent normal")
	label := fs.String("priority-label", "", "the label that MEANS something to the team (P1, now, backlog…); carried only when the state is not unknown")
	prov := fs.String("provenance", "", "authored | triggered | triaged — required, no default (D5)")
	estMin := fs.Int("estimate-min", 0, "smallest honest estimate (with --estimate-max and --estimate-basis, or no estimate)")
	estMax := fs.Int("estimate-max", 0, "largest honest estimate")
	estBasis := fs.String("estimate-basis", "", "why that range is the range")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return &usageError{"roadmap add needs --title — the board renders named rows"}
	}
	if *prov == "" {
		return &usageError{"roadmap add needs --provenance authored|triggered|triaged — the column has no default on purpose (D5): the writer answers where the item came from"}
	}
	p := protocol.RoadmapAddParams{
		ComponentRoot: *root, Title: *title, Epic: *epic,
		PriorityState: *priority, PriorityLabel: *label, Provenance: *prov,
	}
	if *estMin != 0 || *estMax != 0 || *estBasis != "" {
		if *estMin <= 0 || *estMax <= 0 || strings.TrimSpace(*estBasis) == "" {
			return &usageError{"an estimate is a range with its basis or no estimate at all (D3): --estimate-min, --estimate-max AND --estimate-basis together"}
		}
		mn, mx := *estMin, *estMax
		p.EstimateMin, p.EstimateMax, p.EstimateBasis = &mn, &mx, *estBasis
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapAdd, p, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · the item enters at idea (its only door) · ", res.Item.ID)
	printRoadmapItem(w, res.Item)
	return nil
}

func cmdRoadmapList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	epic := fs.String("epic", "", "filter: only this epic")
	status := fs.String("status", "", "filter: only this status (idea|ideating|brief-agreed|planned|ready|running|done|cancelled)")
	priority := fs.String("priority", "", "filter: only this priority state (known|assumed|unknown)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapListResult
	if err := c.Call(protocol.MethodRoadmapList, protocol.RoadmapListParams{
		ComponentRoot: *root, Epic: *epic, Status: *status, PriorityState: *priority,
	}, &res); err != nil {
		return err
	}
	if len(res.Items) == 0 {
		fmt.Fprintf(w, "no items — the roadmap of %s is EMPTY (and loop.start never asked about any of this: D8)\n", res.ComponentRoot)
		return nil
	}
	for _, it := range res.Items {
		printRoadmapItem(w, it)
	}
	return nil
}

func cmdRoadmapShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap show", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap show takes exactly one item id: roadmap show ID"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapShow, protocol.RoadmapShowParams{ID: id}, &res); err != nil {
		return err
	}
	printRoadmapItem(w, res.Item)
	if res.Item.EstimateMin == nil {
		fmt.Fprintln(w, "not sized yet — a range with its basis arrives at any time before done, and nothing asks for it earlier (D3)")
	}
	if res.Item.PriorityState == "unknown" {
		fmt.Fprintln(w, "priority unknown — ready needs a priority that is NOT unknown (D4): roadmap priority "+strconv.FormatInt(id, 10)+" --state known --label \"…\"")
	}
	return nil
}

func cmdRoadmapCancel(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap cancel", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap cancel takes exactly one item id: roadmap cancel ID"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapCancel, protocol.RoadmapCancelParams{ID: id}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · %s — cancelled never deletes: the row stays and `list` still shows it; a running item is the loop's own record and its loop closes through loop verbs (D7)\n", res.Item.ID, res.Item.Status)
	return nil
}

func cmdRoadmapEstimate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap estimate", flag.ContinueOnError)
	fs.SetOutput(w)
	minN := fs.Int("min", 0, "smallest honest estimate (required)")
	maxN := fs.Int("max", 0, "largest honest estimate (required, ≥ --min)")
	basis := fs.String("basis", "", "why that range is the range (required — a bare number is D3's shapeless figure)")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap estimate takes exactly one item id, then its flags: roadmap estimate ID --min N --max N --basis S"}
	}
	if *minN <= 0 || *maxN <= 0 || strings.TrimSpace(*basis) == "" {
		return &usageError{fmt.Sprintf("roadmap estimate wants --min N --max N --basis \"…\" together — the estimate is a fact or it is nothing (D3): roadmap estimate %d --min 1 --max 3 --basis \"one handler, one door\"", id)}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapEstimate, protocol.RoadmapEstimateParams{
		ID: id, EstimateMin: *minN, EstimateMax: *maxN, EstimateBasis: *basis,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · %s · estimate %d–%d (%s) — D8's rule carried: the range and its basis, never a bare figure\n", res.Item.ID, res.Item.Status, *minN, *maxN, *basis)
	return nil
}

func cmdRoadmapPriority(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap priority", flag.ContinueOnError)
	fs.SetOutput(w)
	state := fs.String("state", "", "known | assumed | unknown — required (D2: there is no default priority)")
	label := fs.String("label", "", "the label the state earns; refused with --state unknown, never precedence-resolved")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap priority takes exactly one item id, then its flags: roadmap priority ID --state known --label \"…\""}
	}
	if *state == "" {
		return &usageError{"roadmap priority needs --state known|assumed|unknown — an omitted priority is the honest unknown SAID as unknown, never a silent normal (D2)"}
	}
	if *state == "unknown" && *label != "" {
		return &usageError{"roadmap priority refuses --state unknown with a --label — that is D2's contradiction, and the door says it, not a precedence rule here"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapPriority, protocol.RoadmapPriorityParams{
		ID: id, PriorityState: *state, PriorityLabel: *label,
	}, &res); err != nil {
		return err
	}
	printRoadmapItem(w, res.Item)
	return nil
}

// cmdRoadmapDepend is the CLI twin of roadmap.depend (S27 T3, T0 §7's law:
// every door speaks a verb). A CLIENT and nothing else: it dials, calls,
// prints; same repository, cycle, cancelled and terminal laws answer from
// the daemon verbatim. No --yes seatbelt: an edge is not a dispatch — it
// starts nothing (readiness is re-read at the attempt, D4).
func cmdRoadmapDepend(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap depend", flag.ContinueOnError)
	fs.SetOutput(w)
	on := fs.Int64("on", 0, "the item ID waits for — required; same repository, live, and not already waiting for ID (the daemon names the closure when it would cycle)")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || *on <= 0 || fs.NArg() != 0 {
		return &usageError{"roadmap depend takes exactly one item id and an --on ITEM: roadmap depend ID --on ID2 — an edge names two real items"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapDepend, protocol.RoadmapDependParams{
		ID: id, DependsOn: *on,
	}, &res); err != nil {
		return err
	}
	printRoadmapItem(w, res.Item)
	return nil
}

// cmdRoadmapUndepend is cmdRoadmapDepend's twin (S27 T3): one edge out of
// the graph, nothing else — no cascade, no loop touched, no status moved.
func cmdRoadmapUndepend(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap undepend", flag.ContinueOnError)
	fs.SetOutput(w)
	on := fs.Int64("on", 0, "the item ID no longer waits for — required; the edge must exist, or the daemon names the no-such-edge")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || *on <= 0 || fs.NArg() != 0 {
		return &usageError{"roadmap undepend takes exactly one item id and an --on ITEM: roadmap undepend ID --on ID2 — the edge to remove is named by its pair"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.RoadmapItemResult
	if err := c.Call(protocol.MethodRoadmapUndepend, protocol.RoadmapUndependParams{
		ID: id, DependsOn: *on,
	}, &res); err != nil {
		return err
	}
	printRoadmapItem(w, res.Item)
	return nil
}

func cmdRoadmapIdeate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap ideate", flag.ContinueOnError)
	fs.SetOutput(w)
	workspace := fs.String("workspace", "", "the workspace root the ideation loop runs in and where brief.md lands (required)")
	bpName := fs.String("blueprint", "", "an EXISTING blueprint that declares produces: brief.md (default: brainstorm — the daemon refuses one that does not declare it, R3)")
	attended := fs.Bool("attended", false, "declare whether a human is present for the conversation's questions; omitted is never-declared")
	yes := fs.Bool("yes", false, "confirm the dispatch after reading its authority statement — without it this verb prints and writes NOTHING (S50 RM9)")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap ideate takes exactly one item id, then its flags: roadmap ideate ID --workspace DIR"}
	}
	if strings.TrimSpace(*workspace) == "" {
		return &usageError{"roadmap ideate needs --workspace — ideation is an ordinary loop, and an ordinary loop needs a workspace (D1)"}
	}
	var attendedSet *bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "attended" {
			v := *attended
			attendedSet = &v
		}
	})
	c, err := dial(0)
	if err != nil {
		return err
	}
	if err := roadmapDispatchGate(w, c, "ideate", id, *yes); err != nil {
		return err
	}
	var res map[string]any
	if err := c.Call(protocol.MethodRoadmapIdeate, protocol.RoadmapIdeateParams{
		ID: id, WorkspaceRoot: *workspace, Blueprint: *bpName, Attended: attendedSet,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · ideating — the ideation loop is %v; when it finishes done (brief.md produced), the item reads brief-agreed by itself\n", id, res["loopSlug"])
	return nil
}

func cmdRoadmapPlan(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap plan", flag.ContinueOnError)
	fs.SetOutput(w)
	workspace := fs.String("workspace", "", "the workspace root the planner reads the brief in and writes plan.md to (required)")
	yes := fs.Bool("yes", false, "confirm the dispatch after reading its authority statement — without it this verb prints and writes NOTHING (S50 RM9)")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap plan takes exactly one item id, then its flags: roadmap plan ID --workspace DIR"}
	}
	if strings.TrimSpace(*workspace) == "" {
		return &usageError{"roadmap plan needs --workspace — the planner writes plan.md where the item lives"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	if err := roadmapDispatchGate(w, c, "plan", id, *yes); err != nil {
		return err
	}
	var res map[string]any
	if err := c.Call(protocol.MethodRoadmapPlan, protocol.RoadmapPlanParams{
		ID: id, WorkspaceRoot: *workspace,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · planning dispatched on host %v — when the planner's handoff counts tasks and plan.md is on disk, the item reads planned by itself; a failed run leaves it brief-agreed, never silently advanced\n", id, res["hostLoop"])
	return nil
}

func cmdRoadmapMaterialize(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap materialize", flag.ContinueOnError)
	fs.SetOutput(w)
	blueprintName := fs.String("blueprint", "", "the blueprint the loop runs (required)")
	goal := fs.String("goal", "", "the goal the materialized loop carries (required)")
	workspace := fs.String("workspace", "", "the workspace root (required)")
	tech := fs.String("tech", "", "tech tag for resolver lanes (optional)")
	with := queueWithFlag{}
	fs.Var(&with, "with", "loop-level with entry key=value (repeatable)")
	attended := fs.Bool("attended", false, "declare per-loop whether a human is present; omitted is never-declared")
	yes := fs.Bool("yes", false, "confirm the dispatch after reading its authority statement — without it this verb prints and writes NOTHING (S50 RM9)")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap materialize takes exactly one item id, then its flags: roadmap materialize ID --blueprint NAME --goal TEXT --workspace DIR"}
	}
	if *blueprintName == "" || *goal == "" || strings.TrimSpace(*workspace) == "" {
		return &usageError{"roadmap materialize needs --blueprint, --goal and --workspace — materialization ends at one loop.start call carrying exactly these (R4)"}
	}
	var attendedSet *bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "attended" {
			v := *attended
			attendedSet = &v
		}
	})
	c, err := dial(0)
	if err != nil {
		return err
	}
	if err := roadmapDispatchGate(w, c, "materialize", id, *yes); err != nil {
		return err
	}
	var res map[string]any
	if err := c.Call(protocol.MethodRoadmapMaterialize, protocol.RoadmapMaterializeParams{
		ID: id, Blueprint: *blueprintName, Goal: *goal, WorkspaceRoot: *workspace,
		Tech: *tech, With: with, Attended: attendedSet,
	}, &res); err != nil {
		return err
	}
	if queued, _ := res["queued"].(bool); queued {
		fmt.Fprintf(w, "#%d · running-queued — the payload went to queue.append; S24's ordering decides WHEN its loop runs (appending starts nothing, CG4)\n", id)
		return nil
	}
	fmt.Fprintf(w, "#%d · running — the loop is %v (its own record from here; it reads done when the loop finishes done)\n", id, res["loopSlug"])
	return nil
}
