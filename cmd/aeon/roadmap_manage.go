package main

// roadmap_manage.go — the S50 T6 verbs: `aeon roadmap update|history|board|
// health|vocabulary` and the dispatch seatbelt the three dispatch verbs
// (ideate/plan/materialize) now speak. They reach the plane through the
// two-door selector in roadmap_client.go — the socket door when the daemon
// is there, the direct door when it is not, the fixed line printed first
// either way — and they re-validate NOTHING: RM1's field rules, D4's
// readiness, the unknown-row sentence and the board's blocker all ride out
// of here verbatim, because they are decided underneath (roadmap.go owns
// that law and its nine verbs stay exactly as S26 left them).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
)

func cmdRoadmapUpdate(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap update", flag.ContinueOnError)
	fs.SetOutput(w)
	title := fs.String("title", "", "the new title (a typo fix is legal at every rung; the door decides what is a rewrite)")
	epic := fs.String("epic", "", "move the item to this epic grouping")
	clearEpic := fs.Bool("clear-epic", false, "take the epic grouping off (the one explicit removal — clearing is never a guess from an empty --epic)")
	briefPath := fs.String("brief-path", "", "the pointer to brief.md — absolute under the item's component, and only while the item has not started (RM1; the door says the window, not this verb)")
	planPath := fs.String("plan-path", "", "the pointer to plan.md — same law as --brief-path")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap update takes exactly one item id, then its flags: roadmap update ID [--title T] [--epic E] [--clear-epic] [--brief-path P] [--plan-path P]"}
	}
	p := protocol.RoadmapUpdateParams{ID: id}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			v := *title
			p.Title = &v
		case "epic":
			v := *epic
			p.Epic = &v
		case "clear-epic":
			p.ClearEpic = *clearEpic
		case "brief-path":
			v := *briefPath
			p.BriefPath = &v
		case "plan-path":
			v := *planPath
			p.PlanPath = &v
		}
	})
	if p.Title == nil && p.Epic == nil && !p.ClearEpic && p.BriefPath == nil && p.PlanPath == nil {
		return &usageError{"roadmap update needs at least one change — --title, --epic, --clear-epic, --brief-path or --plan-path (an update naming nothing edits nothing)"}
	}
	door, err := selectRoadmapDoor(roadmapErrOut)
	if err != nil {
		return err
	}
	res, err := door.update(context.Background(), roadmapCLIActor, p)
	if err != nil {
		return err
	}
	printRoadmapItem(w, res.Item)
	if !res.Changed {
		fmt.Fprintln(w, "· unchanged — the edit named nothing that differs; no audit line (the audit records changes, not touches)")
	}
	return nil
}

func cmdRoadmapHistory(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap history", flag.ContinueOnError)
	fs.SetOutput(w)
	limit := fs.Int("limit", 0, "show at most N changes, newest first (omitted is the daemon's own default page)")
	addProtocolVersionFlag(fs)
	id, rest := peelRoadmapID(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"roadmap history takes exactly one item id, then its flags: roadmap history ID [--limit N]"}
	}
	door, err := selectRoadmapDoor(roadmapErrOut)
	if err != nil {
		return err
	}
	res, err := door.history(context.Background(), protocol.RoadmapHistoryParams{ID: id, Limit: *limit})
	if err != nil {
		return err
	}
	if len(res.Changes) == 0 {
		fmt.Fprintf(w, "#%d · no changes — the trail is empty (creating was the first change and even that rides here once it is audited)\n", id)
		return nil
	}
	for _, ch := range res.Changes {
		fmt.Fprintf(w, "%s · %s · %s:%s · %s\n", ch.OccurredAt, ch.Op, ch.ActorKind, ch.ActorID, ch.Summary)
	}
	return nil
}

func cmdRoadmapBoard(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap board", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	status := fs.String("status", "", "narrow to one of the eight statuses (RM6's filter; the board is cross-component by default — this is not `roadmap list` and replaces no one)")
	epic := fs.String("epic", "", "narrow to one epic grouping")
	jsonOut := fs.Bool("json", false, "print the daemon's board answer as JSON — the door's bytes and nothing else")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"roadmap board takes no positional id — it is the whole board: roadmap board [--component ROOT] [--status S] [--epic E] [--json]"}
	}
	door, err := selectRoadmapDoor(roadmapErrOut)
	if err != nil {
		return err
	}
	res, err := door.board(context.Background(), protocol.RoadmapBoardParams{
		ComponentRoot: *root, Status: *status, Epic: *epic,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		// The house --json law: the door's bytes and nothing else — no
		// commentary rides out beside them.
		raw, err := json.Marshal(res)
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(raw))
		return nil
	}
	if len(res.Rows) == 0 {
		fmt.Fprintln(w, "the board is empty — no item matches (adding is `roadmap add`; a filter matching nothing is the FILTER's empty, and says so as this line)")
		return nil
	}
	groups := make([]string, 0, len(res.Groups))
	for g := range res.Groups {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	shown := make([]string, 0, len(groups))
	for _, g := range groups {
		shown = append(shown, fmt.Sprintf("%s %d", g, res.Groups[g]))
	}
	fmt.Fprintf(w, "board · %d rows · %s\n", len(res.Rows), strings.Join(shown, " · "))
	for _, row := range res.Rows {
		fmt.Fprintf(w, "#%d · %s", row.Item.ID, row.Describe)
		if row.Loop != nil {
			fmt.Fprintf(w, " · loop %s (%s)", row.Loop.Slug, row.Loop.Status)
		}
		fmt.Fprintln(w)
		if !row.Ready && row.Blocker != "" {
			// RM6: the SAME sentence IsReady produced — never a
			// re-derivation this verb could get wrong.
			fmt.Fprintf(w, "    not ready: %s\n", row.Blocker)
		}
	}
	return nil
}

func cmdRoadmapHealth(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap health", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"roadmap health takes no arguments — it answers over the whole settings plane; a filter would imply it gates something (RM6)"}
	}
	door, err := selectRoadmapDoor(roadmapErrOut)
	if err != nil {
		return err
	}
	res, err := door.health(context.Background())
	if err != nil {
		return err
	}
	for _, s := range res.Settings {
		fmt.Fprintf(w, "%s · %s · %s", s.Key, s.Name, s.State)
		if s.ReportCounts != nil {
			fmt.Fprintf(w, " · source: %d valid, %d invalid, %d unverifiable",
				s.ReportCounts.Valid, s.ReportCounts.Invalid, s.ReportCounts.Unverifiable)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "evaluated %s\n", res.EvaluatedAt)
	fmt.Fprintln(w, "advice only — a green line never promises a dispatch and a refused dispatch never contradicts it: the dispatch checks the same facts at its own moment (RM6/RM3)")
	return nil
}

func cmdRoadmapVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("roadmap vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return &usageError{"roadmap vocabulary takes no arguments — the vocabulary is the daemon's, not a view of yours"}
	}
	door, err := selectRoadmapDoor(roadmapErrOut)
	if err != nil {
		return err
	}
	v, err := door.vocabulary(context.Background())
	if err != nil {
		return err
	}
	for _, st := range v.Statuses {
		if st.Terminal {
			fmt.Fprintf(w, "%s · terminal\n", st.Status)
			continue
		}
		next := make([]string, 0, len(st.Next))
		for _, n := range st.Next {
			next = append(next, string(n))
		}
		fmt.Fprintf(w, "%s → %s\n", st.Status, strings.Join(next, " | "))
	}
	fmt.Fprintf(w, "priorities · %s\n", strings.Join(v.Priorities, " | "))
	fmt.Fprintf(w, "provenances · %s\n", strings.Join(v.Provenances, " | "))
	fmt.Fprintf(w, "estimates · %s\n", v.EstimateRule)
	fmt.Fprintf(w, "paths · %s (editable while: %s)\n", v.PathRule, statusesWords(v.PathEditable))
	fmt.Fprintf(w, "limits · title <= %d bytes · epic <= %d bytes\n", v.TitleMaxBytes, v.EpicMaxBytes)
	// The statement rides out with its <item> slot UNfilled — this read
	// names no item; the dispatch verbs print it filled from the daemon's
	// own Describe of theirs.
	fmt.Fprintf(w, "dispatch · %s\n", v.DispatchStatement)
	return nil
}

func statusesWords(ss []roadmap.Status) string {
	ws := make([]string, 0, len(ss))
	for _, s := range ss {
		ws = append(ws, string(s))
	}
	return strings.Join(ws, ", ")
}

// roadmapDispatchGate is S49 T8's seatbelt for the three dispatch verbs
// (RM9): BEFORE anything is asked to start, the verb reads the daemon's
// vocabulary (the authority statement is the roadmap package's const —
// one sentence every surface prints, the CLI included, so the CLI cannot
// paraphrase what starting agents means) and the item's own Describe, and
// prints both. Without --yes the verb STOPS with a usage error naming the
// re-run and NOTHING is written — no status moved, no loop started, no
// audit line, because the daemon was never asked. The reads ride the same
// dial the dispatch will use; there is no offline dispatch and the gate
// does not pretend otherwise.
func roadmapDispatchGate(w io.Writer, c *protocol.Client, verb string, id int64, yes bool) error {
	var show protocol.RoadmapShowResult
	if err := c.Call(protocol.MethodRoadmapShow, protocol.RoadmapShowParams{ID: id}, &show); err != nil {
		return roadmapVerbatim(err)
	}
	var voc roadmap.VocabularyAnswer
	if err := c.Call(protocol.MethodRoadmapVocabulary, struct{}{}, &voc); err != nil {
		return roadmapVerbatim(err)
	}
	fmt.Fprintln(w, strings.ReplaceAll(voc.DispatchStatement, "<item>", show.Describe))
	printRoadmapItem(w, show.Item)
	if !yes {
		return &usageError{fmt.Sprintf("roadmap %s %d is a dispatch — re-run with --yes once you have read what it starts above", verb, id)}
	}
	return nil
}
