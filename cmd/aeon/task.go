package main

// task.go — S38 T16: the board at the terminal. `task list` is the person's
// board read (the SAME composition the socket answers — filters, the
// floor-flagged cost, both kinds side by side); `task create` is the
// hand-made task (OL14): a person's task is manual by definition, and the
// engine's rows answer with T7's sentence from the daemon, not a second
// opinion here.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdTask(w io.Writer, args []string) error {
	if len(args) == 0 {
		printTaskUsage(w)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdTaskList(w, args[1:])
	case "show":
		return cmdTaskShow(w, args[1:])
	case "create":
		return cmdTaskCreate(w, args[1:])
	default:
		printTaskUsage(w)
		return fmt.Errorf("unknown `task` subcommand %q", args[0])
	}
}

func printTaskUsage(w io.Writer) {
	fmt.Fprint(w, `aeon task — the board: hand-made and engineered rows, side by side (S38)

  task list [--root P] [--component C] [--assignee NAME] [--kind agent|user]
            [--status pending|in-progress|complete|blocked|…] [--type T] [--manual true|false]
            [--approval none|pending|approved|rejected] [--goal ID] [--limit N]
  task show ID
  task create --component C --title T --type TYPE [--assignee user:NAME|agent:NAME]
              [--body-file F] [--parent ID] [--require-approval]

A hand-made task is manual by definition (OL14) — `+"`--assignee`"+` names
its kind with the colon the ledger keeps separately, so user:carlos and
agent:golang-engineer cannot blur into a bare string. Cost prints with its
honesty: a figure whose every step is priced is a figure, and one with an
unpriced step is a FLOOR, flagged. There is no `+"`task set-status`"+` here:
a person moves a manual rung through the board's doors, and an engine row's
status is the engine's — the daemon answers that refusal itself.
`)
}

func cmdTaskList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := fs.String("root", "", "only this root — id or exact name")
	component := fs.String("component", "", "only this component — root, name, slug or id")
	assignee := fs.String("assignee", "", "only this assignee's name")
	kind := fs.String("kind", "", "agent or user")
	status := fs.String("status", "", "pending|in-progress|done|skipped|blocked|…")
	taskType := fs.String("type", "", "one of the five work-item types")
	manual := fs.String("manual", "", "true = hand-made only, false = engineered only")
	approval := fs.String("approval", "", "none|pending|approved|rejected")
	goal := fs.String("goal", "", "only tasks filed under this goal")
	limit := fs.Int("limit", 0, "stop after N rows")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	p := protocol.TaskListParams{
		Root: *root, Component: *component, Assignee: *assignee,
		AssigneeKind: *kind, Status: *status, Type: *taskType,
		ApprovalState: *approval, Goal: *goal, Limit: *limit,
	}
	switch *manual {
	case "":
	case "true":
		v := true
		p.Manual = &v
	case "false":
		v := false
		p.Manual = &v
	default:
		return &usageError{"--manual is true or false — a third answer is not a filter"}
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.TaskListResult
	if err := c.Call(protocol.MethodTaskList, p, &res); err != nil {
		return err
	}
	if len(res.Tasks) == 0 {
		fmt.Fprintln(w, "no tasks answer this filter")
		return nil
	}
	for _, t := range res.Tasks {
		who := "—"
		if t.Assignee != "" {
			who = t.AssigneeKind + ":" + t.Assignee
		}
		origin := "engine"
		if t.Manual {
			origin = "manual"
		}
		cost := ""
		if t.CostUSD > 0 || t.CostIsFloor {
			cost = fmt.Sprintf("  $%.4f", t.CostUSD)
			if t.CostIsFloor {
				cost += "≥"
			}
		}
		approval := ""
		if t.ApprovalState != "" && t.ApprovalState != "none" {
			approval = " [" + t.ApprovalState + "]"
		}
		fmt.Fprintf(w, "%-22s %-10s %-12s %-8s %-24s %s%s%s\n",
			t.ID, origin, t.Status, t.Type, who, t.Title, cost, approval)
	}
	return nil
}

func cmdTaskShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("task show", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	positional := fs.Args()
	if len(positional) != 1 {
		return fmt.Errorf("`task show` takes exactly one task id (the tsk-… surrogate)")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var t struct {
		ID            string   `json:"id"`
		TaskID        string   `json:"taskid"`
		LoopSlug      string   `json:"loopSlug"`
		ComponentRoot string   `json:"componentRoot"`
		Title         string   `json:"title"`
		Type          string   `json:"type"`
		Tech          string   `json:"tech"`
		Status        string   `json:"status"`
		Manual        bool     `json:"manual"`
		AssigneeKind  string   `json:"assigneeKind"`
		Assignee      string   `json:"assignee"`
		ApprovalState string   `json:"approvalState"`
		DependsOn     []string `json:"dependsOn"`
	}
	if err := c.Call(protocol.MethodName("task.show"), map[string]any{"id": positional[0]}, &t); err != nil {
		return err
	}
	origin := "engineered by loop " + t.LoopSlug
	if t.Manual {
		origin = "hand-made"
	}
	fmt.Fprintf(w, "%s  %s  [%s]\n", t.ID, t.Status, origin)
	fmt.Fprintf(w, "  %s\n", t.Title)
	if t.Type != "" {
		fmt.Fprintf(w, "  type: %s\n", t.Type)
	}
	if t.Assignee != "" {
		fmt.Fprintf(w, "  assignee: %s:%s\n", t.AssigneeKind, t.Assignee)
	}
	if t.ApprovalState != "" && t.ApprovalState != "none" {
		fmt.Fprintf(w, "  approval: %s — the person's door is the socket's task.decide (the app's board answers it)\n", t.ApprovalState)
	}
	if len(t.DependsOn) > 0 {
		fmt.Fprintf(w, "  depends on: %s\n", strings.Join(t.DependsOn, ", "))
	}
	return nil
}

func cmdTaskCreate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	fs.SetOutput(w)
	component := fs.String("component", "", "the component — root, name, slug or id (required)")
	title := fs.String("title", "", "what the task does (required)")
	taskType := fs.String("type", "", "one of the five work-item types (required)")
	assignee := fs.String("assignee", "", "user:NAME or agent:NAME — the kind is part of the name")
	bodyFile := fs.String("body-file", "", "a file holding the task's own document")
	parent := fs.String("parent", "", "a parent task's surrogate id")
	requireApproval := fs.Bool("require-approval", false, "file it waiting on a person's decision")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *component == "" || *title == "" || *taskType == "" {
		return &usageError{"`task create` needs --component, --title and --type — a task of nowhere cannot be filed (OL14)"}
	}
	params := map[string]any{"component": *component, "title": *title, "type": *taskType}
	if *assignee != "" {
		kind, name, ok := strings.Cut(*assignee, ":")
		if !ok || (kind != "user" && kind != "agent") || name == "" {
			return &usageError{"--assignee is user:NAME or agent:NAME — the ledger keeps kind and name apart, so the flag says both"}
		}
		params["assignee_kind"] = kind
		params["assignee"] = name
	}
	if *bodyFile != "" {
		raw, err := os.ReadFile(*bodyFile)
		if err != nil {
			return fmt.Errorf("read the body from %s: %w", *bodyFile, err)
		}
		params["body_note"] = strings.TrimSpace(string(raw))
	}
	if *parent != "" {
		params["parent"] = *parent
	}
	if *requireApproval {
		params["require_approval"] = true
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var t struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		Type          string `json:"type"`
		Status        string `json:"status"`
		Manual        bool   `json:"manual"`
		AssigneeKind  string `json:"assigneeKind"`
		Assignee      string `json:"assignee"`
		ApprovalState string `json:"approvalState"`
	}
	if err := c.Call(protocol.MethodName("task.create"), params, &t); err != nil {
		return err
	}
	who := "unassigned"
	if t.Assignee != "" {
		who = t.AssigneeKind + ":" + t.Assignee
	}
	fmt.Fprintf(w, "%-22s %-10s %-8s %-24s %s\n", t.ID, t.Status, t.Type, who, t.Title)
	if !t.Manual {
		// Unreachable through the person's door — printed only if a future
		// daemon ever lets one through, which is exactly when it should be
		// loud.
		fmt.Fprintln(w, "  (not manual — a person's hand-made task is manual by definition)")
	}
	if t.ApprovalState == "pending" {
		fmt.Fprint(w, "  waiting on a decision — the socket's task.decide is the person's door (S38 T15)\n")
	}
	return nil
}
