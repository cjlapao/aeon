package main

// retro.go — S63 T7 (RT10, RT12, RT13): `aeon retro` — a PURE CLIENT of
// the retro plane's doors: list, show, status, digest, and the optional
// backfill, whose dry-run preview is the door's own answer (RT13). The
// CLI computes nothing and re-renders no sentence; the one sentence it
// adds — "nothing written; pass --yes to write" — is the seatbelt's words
// for a dry-run answer (P5), printed beside the daemon's preview, never
// instead of it. No offline fallback (T7's recorded deviation from P5):
// a stopped daemon says so with dial's own words.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// retroDial is the dial every verb shares; a var for in-process tests.
var retroDial = dial

func cmdRetro(w io.Writer, args []string) error {
	if len(args) == 0 {
		printRetroUsage(w)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return retroList(w, rest)
	case "show":
		return retroShow(w, rest)
	case "status":
		return retroStatus(w, rest)
	case "backfill":
		return retroBackfill(w, rest)
	case "digest":
		return retroDigest(w, rest)
	case "help", "-h", "--help":
		printRetroUsage(w)
		return nil
	default:
		printRetroUsage(w)
		return fmt.Errorf("unknown `retro` subcommand %q", sub)
	}
}

func printRetroUsage(w io.Writer) {
	fmt.Fprint(w, `usage: aeon retro <verb>

  list      recorded loop outcomes (--component ID --agent SLUG --blueprint ID
          --outcome KIND --since RFC3339 --until RFC3339 --limit N --json)
  show ID   one outcome, its per-agent rows, its learning link (--json)
  status    the recorder's own counts (--json)
  backfill  fill missing outcomes — a dry-run preview unless --yes (--json)
  digest ID the one-paragraph readout of one component (--since --until --json)
`)
}

// retroDoor dials, Calls, and either prints wire bytes (--json) or hands
// them to the renderer — the same single code path as the scorecard verbs.
func retroDoor(w io.Writer, asJSON bool, method protocol.MethodName, params any, render func(json.RawMessage) error) error {
	c, err := retroDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var raw json.RawMessage
	if err := c.Call(method, params, &raw); err != nil {
		return err
	}
	if asJSON {
		fmt.Fprintln(w, string(raw))
		return nil
	}
	return render(raw)
}

func retroList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("retro list", flag.ContinueOnError)
	fs.SetOutput(w)
	var p protocol.RetroListParams
	var since, until string
	var asJSON bool
	fs.StringVar(&p.ID, "component", "", "component id or root path")
	fs.StringVar(&p.Agent, "agent", "", "one agent's rows")
	fs.StringVar(&p.Blueprint, "blueprint", "", "one blueprint's rows")
	fs.StringVar(&p.Outcome, "outcome", "", "one outcome kind")
	fs.StringVar(&since, "since", "", "recorded at or after (RFC3339)")
	fs.StringVar(&until, "until", "", "recorded at or before (RFC3339)")
	fs.IntVar(&p.Limit, "limit", 0, "row cap")
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validRFC3339("--since", since); err != nil {
		return err
	}
	if err := validRFC3339("--until", until); err != nil {
		return err
	}
	p.Since, p.Until = since, until
	return retroDoor(w, asJSON, protocol.MethodRetroList, p, func(raw json.RawMessage) error {
		var r protocol.RetroListResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		for _, row := range r.Rows {
			fmt.Fprintf(w, "%d  %-24s %s\n", row.ID, row.LoopSlug, row.Describe)
		}
		if len(r.Rows) == 0 {
			fmt.Fprintln(w, "no recorded outcomes answer this filter")
		}
		if r.NextCursor != 0 {
			fmt.Fprintf(w, "next cursor: %d\n", r.NextCursor)
		}
		fmt.Fprintln(w, r.Statement)
		return nil
	})
}

func retroShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("retro show", flag.ContinueOnError)
	fs.SetOutput(w)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	rest, err := splitID(fs, "retro show", args)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return fmt.Errorf("retro show wants the numeric outcome id printed by `retro list`, got %q", rest[0])
	}
	if err := fs.Parse(rest[1:]); err != nil {
		return err
	}
	return retroDoor(w, asJSON, protocol.MethodRetroShow, protocol.RetroShowParams{OutcomeID: id}, func(raw json.RawMessage) error {
		var r protocol.RetroShowResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		fmt.Fprintf(w, "%d  %s\n%s\n", r.Row.ID, r.Row.LoopSlug, r.Row.Describe)
		fmt.Fprintf(w, "source: %s   complete: %t   recorded: %s\n", r.Row.Source, r.Row.Complete, dash(r.Row.RecordedAt))
		if len(r.Agents) > 0 {
			fmt.Fprintln(w, "agents:")
			for _, a := range r.Agents {
				slug := "slug unknown"
				if a.AgentSlugKnown {
					slug = "slug known"
				}
				fmt.Fprintf(w, "  %-20s (%s)  steps %s done %s rejected %s blocked %s\n", a.Agent, slug,
					retroPtrInt(a.Steps), retroPtrInt(a.StepsDone), retroPtrInt(a.StepsRejected), retroPtrInt(a.StepsBlocked))
			}
		}
		if r.Learning != nil {
			fmt.Fprintf(w, "learning: #%d [%s] %s\n", r.Learning.ID, r.Learning.Provenance, r.Learning.Body)
			if r.Learning.RetractedAt != nil {
				fmt.Fprintf(w, "retracted: %s\n", *r.Learning.RetractedAt)
			}
		}
		fmt.Fprintln(w, r.Statement)
		return nil
	})
}

func retroStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("retro status", flag.ContinueOnError)
	fs.SetOutput(w)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return retroDoor(w, asJSON, protocol.MethodRetroStatus, struct{}{}, func(raw json.RawMessage) error {
		var r protocol.RetroStatusResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		fmt.Fprintf(w, "recorded since: %s\n", dash(r.RecordedSince))
		fmt.Fprintf(w, "loops recorded: %d\n", r.LoopsRecorded)
		fmt.Fprintf(w, "outcomes: %d   last write: %s   dropped: %d\n", r.Outcomes, dash(r.LastWriteAt), r.Dropped)
		fmt.Fprintf(w, "learnings written: %d\n", r.LearningsWritten)
		if r.Reconcile.NeverRun {
			fmt.Fprintln(w, "reconcile: never run")
		} else {
			fmt.Fprintf(w, "reconcile: last %s, %d written\n", dash(r.Reconcile.LastRunAt), r.Reconcile.LastWritten)
		}
		fmt.Fprintf(w, "model retrospective: %s\n", modelRetroWord(r.ModelRetro))
		fmt.Fprintln(w, r.Statement)
		return nil
	})
}

// retroBackfill — RT13: the preview is the DEFAULT and the --yes is the
// seatbelt (P5). The door already answers DryRun:true when it previewed;
// this verb's only added words are the seatbelt sentence beside it.
func retroBackfill(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("retro backfill", flag.ContinueOnError)
	fs.SetOutput(w)
	var p protocol.RetroBackfillParams
	var asJSON bool
	fs.StringVar(&p.Root, "root", "", "one component root")
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	fs.BoolVar(&p.Yes, "yes", false, "write what the preview lists (the seatbelt)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if p.Yes {
		// RT13's resend: the door's default is the preview, so a write
		// is asked for in BOTH words — dryRun:false AND yes:true. The
		// CLI does not paraphrase that law, it obeys it.
		no := false
		p.DryRun = &no
	}
	return retroDoor(w, asJSON, protocol.MethodRetroBackfill, p, func(raw json.RawMessage) error {
		var r protocol.RetroBackfillWire
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		fmt.Fprintf(w, "scanned: %d   written: %d\n", r.Scanned, r.Written)
		fmt.Fprintf(w, "skipped: %d already recorded, %d by rule, %d open, %d without scope\n",
			r.SkippedRecorded, r.SkippedRule, r.SkippedOpen, r.SkippedNoScope)
		if r.DryRun {
			var kinds []string
			for k, n := range r.ByKind {
				kinds = append(kinds, fmt.Sprintf("%s=%d", k, n))
			}
			fmt.Fprintf(w, "would write: %s\n", strings.Join(kinds, " "))
			fmt.Fprintln(w, "nothing written; pass --yes to write")
		}
		fmt.Fprintln(w, r.Statement)
		return nil
	})
}

func retroDigest(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("retro digest", flag.ContinueOnError)
	fs.SetOutput(w)
	var p protocol.DigestParams
	var asJSON bool
	fs.StringVar(&p.Since, "since", "", "window start (RFC3339)")
	fs.StringVar(&p.Until, "until", "", "window end (RFC3339)")
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	rest, err := splitID(fs, "retro digest", args)
	if err != nil {
		return err
	}
	p.ID = rest[0]
	if err := fs.Parse(rest[1:]); err != nil {
		return err
	}
	if err := validRFC3339("--since", p.Since); err != nil {
		return err
	}
	if err := validRFC3339("--until", p.Until); err != nil {
		return err
	}
	return retroDoor(w, asJSON, protocol.MethodDigestLearned, p, func(raw json.RawMessage) error {
		var r protocol.DigestResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		return printDigest(w, r)
	})
}

// printDigest walks RT12's seven sections IN ORDER — the field order IS
// the contract; every word the digest quotes arrives already rendered by
// its one formatter, and this function only lays lines down.
func printDigest(w io.Writer, r protocol.DigestResult) error {
	fmt.Fprintf(w, "window: %s .. %s\n", dash(r.Window.Since), dash(r.Window.Until))
	fmt.Fprintf(w, "coverage: %d of %d loops recorded (%d not recorded, since %s)\n",
		r.Coverage.LoopsRecorded, r.Coverage.LoopsInHistory, r.Coverage.LoopsNotRecorded, dash(r.Coverage.RecordedSince))
	fmt.Fprint(w, "outcomes:\n")
	for _, o := range r.Outcomes.ByKind {
		rate := "—"
		if o.Rate != nil {
			rate = strconv.FormatFloat(*o.Rate, 'f', -1, 64)
		}
		fmt.Fprintf(w, "  %-16s loops=%-4d rate=%s\n", o.Kind, o.Loops, rate)
	}
	if r.Outcomes.Previous != nil {
		fmt.Fprintf(w, "previous window %s .. %s:\n", dash(r.Outcomes.Previous.CardWindow.Since), dash(r.Outcomes.Previous.CardWindow.Until))
		for _, o := range r.Outcomes.Previous.ByKind {
			rate := "—"
			if o.Rate != nil {
				rate = strconv.FormatFloat(*o.Rate, 'f', -1, 64)
			}
			fmt.Fprintf(w, "  %-16s loops=%-4d rate=%s\n", o.Kind, o.Loops, rate)
		}
	}
	fmt.Fprint(w, "rework:\n")
	if r.Rework.ReviewRounds != nil {
		fmt.Fprintln(w, formatMetricLine("review rounds", *r.Rework.ReviewRounds))
	}
	if r.Rework.SecurityRounds != nil {
		fmt.Fprintln(w, formatMetricLine("security rounds", *r.Rework.SecurityRounds))
	}
	fmt.Fprintf(w, "  %-24s %d\n", "retry rounds", r.Rework.RetryRounds)
	fmt.Fprintf(w, "  %-24s %d\n", "qa rounds", r.Rework.QARounds)
	for _, b := range r.Rework.TopBlueprints {
		fmt.Fprintf(w, "  blueprint %s: %d rounds\n", b.BlueprintID, b.Rounds)
	}
	fmt.Fprint(w, "blocked:\n")
	if len(r.Blocked.Top) == 0 {
		fmt.Fprintln(w, "  nothing blocked")
	}
	for _, b := range r.Blocked.Top {
		fmt.Fprintf(w, "  %-24s %d loops\n", b.Kind, b.Loops)
	}
	fmt.Fprint(w, "cost:\n")
	fmt.Fprintf(w, "  floor: %s (%d unknown steps)\n", dash(r.Cost.Floor), r.Cost.UnknownSteps)
	fmt.Fprintln(w, formatMetricLine("per completed item", r.Cost.PerCompletedItem))
	if r.Cost.LargestLoop != nil {
		fmt.Fprintf(w, "  largest loop: %s / %s\n", r.Cost.LargestLoop.Root, r.Cost.LargestLoop.Loop)
	}
	fmt.Fprint(w, "learned:\n")
	for _, g := range r.Learned.Groups {
		fmt.Fprintf(w, "  %s:\n", g.Provenance)
		for _, it := range g.Items {
			fmt.Fprintf(w, "    #%d [%s] %s\n", it.ID, it.Scope, it.Body)
		}
	}
	fmt.Fprint(w, "attention:\n")
	for _, b := range r.Attention.BlockedNow {
		fmt.Fprintf(w, "  %s / %s — %s (%d min)\n", b.Root, b.Loop, b.Kind, b.AgeMinutes)
	}
	fmt.Fprintf(w, "  pending questions: %d   proposals pending: %d\n", r.Attention.PendingQuestions, r.Attention.ProposalsPending)
	fmt.Fprintf(w, "  %s\n", r.Attention.Statement)
	fmt.Fprintln(w, r.Statement)
	return nil
}

// --- small shared prints -------------------------------------------------------

func splitID(fs *flag.FlagSet, name string, args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s needs its argument", name)
	}
	return args, nil
}

func validRFC3339(flagName, v string) error {
	if v == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, v); err != nil {
		return fmt.Errorf("%s must be an RFC3339 time, got %q", flagName, v)
	}
	return nil
}

func retroPtrInt(v *int) string {
	if v == nil {
		return "—"
	}
	return strconv.Itoa(*v)
}

func modelRetroWord(m protocol.RetroModelRetroWire) string {
	if m.Enabled {
		return "enabled"
	}
	if m.BlockedBy != "" {
		return "not enabled — " + m.BlockedBy
	}
	return "not enabled"
}
