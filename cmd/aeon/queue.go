package main

// The `aeon queue` verb — the backlog of intentions a project holds before
// any of them runs (S24). This is a CLIENT of the daemon's `queue.*`
// methods and nothing else: it dials the same socket the app speaks
// (P34's ordering, CG9's one implementation), opens no store, and
// re-validates no law — D8's range, the status machine, the settings
// vocabulary are decided once, underneath, and the sentences of refusal
// ride out of here verbatim.
//
// What this surface owns: argument shape, the settings read-merge-write
// (a socket write carries BOTH knobs; the CLI merges the untouched one
// from the daemon's own read — half a pair is never sent), and the
// honest empty state (an empty backlog says so, and says what does not
// happen: appending starts nothing — `advance` is the explicit door).
//
// The root flag is the house `--component` (the queue keys live on the
// component row, which owns roots — the slice docs' "project root"
// phrasing predates S38's OL1 rename).

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdQueue(w io.Writer, args []string) error {
	if len(args) == 0 {
		printQueueUsage(w)
		return nil
	}
	switch args[0] {
	case "append":
		return cmdQueueAppend(w, args[1:])
	case "list":
		return cmdQueueList(w, args[1:])
	case "reorder":
		return cmdQueueReorder(w, args[1:])
	case "remove":
		return cmdQueueRemove(w, args[1:])
	case "set-size":
		return cmdQueueSetSize(w, args[1:])
	case "settings":
		return cmdQueueSettings(w, args[1:])
	default:
		printQueueUsage(w)
		return fmt.Errorf("unknown `queue` subcommand %q", args[0])
	}
}

func printQueueUsage(w io.Writer) {
	fmt.Fprint(w, `aeon queue — the backlog of intentions, before any of them runs (S24)

  queue append --blueprint NAME --goal TEXT [--tech T] [--with k=v]…
               [--attended | --attended=false] [--component ROOT]
  queue list [--component ROOT]
  queue reorder ID POSITION
  queue remove ID
  queue set-size ID --min N --max N --basis "…"
  queue settings [--queue-advance auto|ask] [--sizing-mode manual|skill]
                 [--component ROOT]

An item IS a loop.start payload held in a queue (CG3): appending starts
nothing, and a blocked row halts the queue unconditionally until it is
resolved or cancelled (CG2). The explicit door is the socket's
queue.advance (CG4 — the call itself is the decision in ask mode);
under queue-advance auto a terminal event chains it for you.

Size is a fact or it is nothing (D8): --min, --max AND --basis together,
or the run that sized it said so with its own report_size call (skill
mode refuses a hand-written size BY NAME, never silently).
`)
}

// queueWithFlag collects repeatable --with k=v entries.
type queueWithFlag map[string]string

func (v queueWithFlag) String() string { return "" }

func (v queueWithFlag) Set(s string) error {
	k, val, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return fmt.Errorf("--with wants k=v, got %q", s)
	}
	v[k] = val
	return nil
}

// peelQueueID takes the leading positional id (goal decide's house
// shape: the id first, then flags). Returns 0 when absent or numeric-
// broken; callers refuse with their own named usage sentence.
func peelQueueID(args []string, verb string) (int64, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return 0, args
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, args
	}
	return id, args[1:]
}

func cmdQueueAppend(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("queue append", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	blueprintName := fs.String("blueprint", "", "the blueprint the item will materialize as (required)")
	goal := fs.String("goal", "", "the goal text — the loop's own, verbatim (required)")
	tech := fs.String("tech", "", "tech tag for resolver lanes (optional)")
	with := queueWithFlag{}
	fs.Var(&with, "with", "loop-level with entry key=value (repeatable)")
	attended := fs.Bool("attended", false, "declare per-item whether a human is present to answer questions; --attended=false declares nobody is. Omitted, the project's declared default decides — never whoever happens to be present (D5/G14)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *blueprintName == "" || *goal == "" {
		return &usageError{"queue append needs --blueprint and --goal — an item materializes as loop.start with exactly these (CG3)"}
	}
	// `--attended` written at all is a DECLARATION (its value the fact);
	// omitted is absent-for-the-project-default. fs.Visit distinguishes
	// the two honestly — the zero of a bool cannot.
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
	var res protocol.QueueItemResult
	if err := c.Call(protocol.MethodQueueAppend, protocol.QueueAppendParams{
		ComponentRoot: *root, Blueprint: *blueprintName, Goal: *goal,
		Tech: *tech, With: with, Attended: attendedSet,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%d queued · position %d · %s · %s\n", res.Item.ID, res.Item.Position, res.Item.Status, res.Item.Blueprint)
	fmt.Fprintln(w, "appending started nothing — queue.advance (or the auto chain on a terminal) is the explicit door (CG4)")
	return nil
}

func cmdQueueList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("queue list", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.QueueListResult
	if err := c.Call(protocol.MethodQueueList, protocol.QueueListParams{ComponentRoot: *root}, &res); err != nil {
		return err
	}
	if len(res.Items) == 0 {
		fmt.Fprintf(w, "no items — the backlog of %s is EMPTY (appending starts nothing; nothing runs until queue advance says so)\n", res.ComponentRoot)
		return nil
	}
	for _, it := range res.Items {
		fmt.Fprintf(w, "#%d · pos %d · %-10s · %s · %s", it.ID, it.Position, it.Status, it.Blueprint, it.Goal)
		if it.SizeMin != nil && it.SizeMax != nil && it.SizeBasis != nil {
			fmt.Fprintf(w, " · size %d–%d (%s)", *it.SizeMin, *it.SizeMax, *it.SizeBasis)
		}
		if it.Attended != nil {
			fmt.Fprintf(w, " · attended=%t", *it.Attended)
		}
		if it.Tech != "" {
			fmt.Fprintf(w, " · tech=%s", it.Tech)
		}
		if it.LoopSlug != "" {
			fmt.Fprintf(w, " · loop=%s", it.LoopSlug)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func cmdQueueReorder(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("queue reorder", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	// House parse (goal decide's shape): the id is peeled from the front,
	// flags follow — flags-after-positional is not the flag package's
	// grammar, and pretending otherwise silently drops them.
	id, rest := peelQueueID(args, "reorder")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 {
		return &usageError{"queue reorder takes the item id first, then the position: queue reorder ID N"}
	}
	if fs.NArg() != 1 {
		return &usageError{fmt.Sprintf("queue reorder wants a position too: queue reorder %d N", id)}
	}
	pos, err := strconv.Atoi(fs.Arg(0))
	if err != nil || pos < 1 {
		return &usageError{fmt.Sprintf("queue reorder wants a position of 1 or more, not %q", fs.Arg(0))}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	var res protocol.QueueItemResult
	if err := c.Call(protocol.MethodQueueReorder, protocol.QueueReorderParams{ID: id, Position: pos}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · pos %d · %s — the row moved; nothing else was disturbed (D1)\n", res.Item.ID, res.Item.Position, res.Item.Status)
	return nil
}

func cmdQueueRemove(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("queue remove", flag.ContinueOnError)
	fs.SetOutput(w)
	addProtocolVersionFlag(fs)
	id, rest := peelQueueID(args, "remove")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"queue remove takes exactly one item id: queue remove ID"}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	// D12: remove is CANCEL, never delete — and this verb prints what
	// that means, because `list` must keep showing the row.
	var res protocol.QueueItemResult
	if err := c.Call(protocol.MethodQueueRemove, protocol.QueueRemoveParams{ID: id}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · %s — removed means cancelled, never deleted (D12): the row stays and `list` still shows it\n", res.Item.ID, res.Item.Status)
	return nil
}

func cmdQueueSetSize(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("queue set-size", flag.ContinueOnError)
	fs.SetOutput(w)
	minN := fs.Int("min", 0, "smallest honest estimate (required)")
	maxN := fs.Int("max", 0, "largest honest estimate (required, ≥ --min)")
	basis := fs.String("basis", "", "why that range is the range (required — a bare number is D8's shapeless size)")
	addProtocolVersionFlag(fs)
	id, rest := peelQueueID(args, "set-size")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if id == 0 || fs.NArg() != 0 {
		return &usageError{"queue set-size takes exactly one item id, then its flags: queue set-size ID --min N --max N --basis S"}
	}
	if *minN <= 0 || *maxN <= 0 || strings.TrimSpace(*basis) == "" {
		return &usageError{fmt.Sprintf("queue set-size wants --min N --max N --basis \"…\" together — the size is a fact or it is nothing (D8): queue set-size %d --min 1 --max 3 --basis \"one handler, one door\"", id)}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	// The skill-mode refusal is the DAEMON's sentence (named, never a
	// silent no-op): report_size is the only writer of size there (D9).
	var res protocol.QueueItemResult
	if err := c.Call(protocol.MethodQueueSetSize, protocol.QueueSetSizeParams{
		ID: id, SizeMin: *minN, SizeMax: *maxN, SizeBasis: *basis,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "#%d · %s · size %d–%d (%s)\n", res.Item.ID, res.Item.Status, *minN, *maxN, *basis)
	return nil
}

func cmdQueueSettings(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("queue settings", flag.ContinueOnError)
	fs.SetOutput(w)
	root := componentFlag(fs)
	advance := fs.String("queue-advance", "", "auto | ask — write form (the bare form reads)")
	sizing := fs.String("sizing-mode", "", "manual | skill — write form (the bare form reads)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *advance != "" && *advance != "auto" && *advance != "ask" {
		return &usageError{fmt.Sprintf("queue settings refuses --queue-advance %q — auto or ask (D3)", *advance)}
	}
	if *sizing != "" && *sizing != "manual" && *sizing != "skill" {
		return &usageError{fmt.Sprintf("queue settings refuses --sizing-mode %q — manual or skill (D7)", *sizing)}
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	// A write carries BOTH knobs (the socket refuses half a pair before
	// any seam); the merge is done HERE from the daemon's own read, so
	// the untouched flag keeps its current value (T6's round-trip gate).
	if *advance != "" || *sizing != "" {
		var cur protocol.QueueSettingsResult
		if err := c.Call(protocol.MethodQueueSettings, protocol.QueueSettingsParams{ComponentRoot: *root}, &cur); err != nil {
			return err
		}
		a, s := cur.QueueAdvance, cur.SizingMode
		if *advance != "" {
			a = *advance
		}
		if *sizing != "" {
			s = *sizing
		}
		var w2 protocol.QueueSettingsResult
		if err := c.Call(protocol.MethodQueueSettings, protocol.QueueSettingsParams{
			ComponentRoot: *root, QueueAdvance: &a, SizingMode: &s,
		}, &w2); err != nil {
			return err
		}
		printQueueSettings(w, w2)
		return nil
	}
	var res protocol.QueueSettingsResult
	if err := c.Call(protocol.MethodQueueSettings, protocol.QueueSettingsParams{ComponentRoot: *root}, &res); err != nil {
		return err
	}
	printQueueSettings(w, res)
	return nil
}

func printQueueSettings(w io.Writer, r protocol.QueueSettingsResult) {
	fmt.Fprintf(w, "queue_advance=%s · sizing_mode=%s · component=%s", r.QueueAdvance, r.SizingMode, r.ComponentRoot)
	if !r.Known {
		// Three-state honesty (P31): unregistered means these are the
		// slice's DECLARED defaults, not stored facts.
		fmt.Fprint(w, " · UNREGISTERED — no component owns this root; these are the slice's declared defaults, never stored facts")
	}
	fmt.Fprintln(w)
}
