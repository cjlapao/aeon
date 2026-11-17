package main

// scorecard.go — S63 T7 (RT10, RT12, standing rule 1): `aeon scorecard` —
// a PURE CLIENT of the person's socket doors. Every verb dials, Calls the
// very method the app calls, and PRINTS what came back: the CLI computes
// nothing, re-validates nothing, and re-renders no sentence (RT3's
// statement is printed from the answer's own field, never a copy — the
// sentence lives in one constant upstairs). No offline fallback (P5's
// recorded deviation, the mergequeue/platform shape): these reads read
// recorded facts the daemon owns, so a stopped daemon says so with dial's
// own words and this file adds no second answer.
//
// The output law (RT7 made visible): every metric line carries its n and
// its state; `too-few` and `cannot-say` print AS WORDS and never as a
// zero; a cost prints as the floor sentence the store already rendered
// (`detail.rendered`), never as a bare number. The coverage line comes
// first; the plane's statement comes last. There is NO sort-by-score and
// NO ranking flag — alphabetical metric order is presentation, not rank
// (T7's TestNoRankingFlag pins that no flag named sort/rank/best/worst
// exists, and no diff between cards is computed here — RT8).

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// scDial is the dial every verb shares; a var so tests can stand a table
// where the socket would be (the mqDial idiom). RAW dial: the "daemon not
// running" words are dial's own; none of these verbs has a no-daemon use.
var scDial = dial

func cmdScorecard(w io.Writer, args []string) error {
	if len(args) == 0 {
		printScorecardUsage(w)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "agent":
		return scorecardSubject(w, protocol.MethodScorecardAgent, "agent", rest)
	case "blueprint":
		return scorecardSubject(w, protocol.MethodScorecardBlueprint, "blueprint", rest)
	case "component":
		return scorecardComponent(w, rest)
	case "compare":
		return scorecardCompare(w, rest)
	case "vocabulary":
		return scorecardVocabulary(w, rest)
	case "help", "-h", "--help":
		printScorecardUsage(w)
		return nil
	default:
		printScorecardUsage(w)
		return fmt.Errorf("unknown `scorecard` subcommand %q", sub)
	}
}

func printScorecardUsage(w io.Writer) {
	fmt.Fprint(w, `usage: aeon scorecard <verb>

  agent SLUG        one agent's card (its own rows, wherever they ran)
  blueprint ID      one blueprint's card
  component ID      one component's card (--children=false to exclude)
  compare KIND SUBJECT --a SINCE..UNTIL --b SINCE..UNTIL
                    the same subject over two windows — two cards, no delta
  vocabulary        the metric names, states, minSample, milestones

flags (reads): --root ID --since RFC3339 --until RFC3339 --last N --json
A score is a measurement of what happened, printed as a measurement:
there is no sort-by-score, no ranking, and no flag that would add one.
`)
}

// scorecardFlags are the window flags plus --json; parsing refuses ONLY a
// malformed time (what cannot BE a value), never a semantic question
// (T7: the daemon's sentence, verbatim, is the only refusal caller).
type scorecardFlags struct {
	root, since, until string
	last               int
	asJSON             bool
}

func parseScorecardFlags(fs *flag.FlagSet, f *scorecardFlags, args []string) error {
	fs.StringVar(&f.root, "root", "", "restrict to one component ROOT (id or root path)")
	fs.StringVar(&f.since, "since", "", "window start (RFC3339)")
	fs.StringVar(&f.until, "until", "", "window end (RFC3339)")
	fs.IntVar(&f.last, "last", 0, "the last N loops instead of a date window")
	fs.BoolVar(&f.asJSON, "json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, s := range []struct {
		flag, val string
	}{{"--since", f.since}, {"--until", f.until}} {
		if s.val != "" {
			if _, err := time.Parse(time.RFC3339, s.val); err != nil {
				return fmt.Errorf("%s must be an RFC3339 time, got %q", s.flag, s.val)
			}
		}
	}
	return nil
}

func (f *scorecardFlags) window() protocol.ScorecardWindow {
	return protocol.ScorecardWindow{Since: f.since, Until: f.until, Last: f.last}
}

// scCall dials once per invocation, Calls the method, and either prints
// the wire bytes verbatim (--json) or hands the decoded result to render.
func scCall(w io.Writer, asJSON bool, method protocol.MethodName, params any, result func(json.RawMessage) error) error {
	c, err := scDial(0)
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
	return result(raw)
}

// scorecardSubject serves `agent` and `blueprint`: same window, same
// render, one param field apart — one client code path for the pair.
func scorecardSubject(w io.Writer, method protocol.MethodName, kind string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("scorecard %s needs its %s: aeon scorecard %s <%s>", kind, kind, kind, strings.ToUpper(kind))
	}
	fs := flag.NewFlagSet("scorecard "+kind, flag.ContinueOnError)
	fs.SetOutput(w)
	var f scorecardFlags
	if err := parseScorecardFlags(fs, &f, args[1:]); err != nil {
		return err
	}
	var params any
	if method == protocol.MethodScorecardAgent {
		params = protocol.ScorecardAgentParams{Agent: args[0], RootID: f.root, ScorecardWindow: f.window()}
	} else {
		params = protocol.ScorecardBlueprintParams{Blueprint: args[0], RootID: f.root, ScorecardWindow: f.window()}
	}
	return scCall(w, f.asJSON, method, params, func(raw json.RawMessage) error {
		var r protocol.ScorecardCardResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		printCard(w, r.Card)
		fmt.Fprintln(w, r.Statement)
		return nil
	})
}

func scorecardComponent(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("scorecard component needs the component (id or root path)")
	}
	fs := flag.NewFlagSet("scorecard component", flag.ContinueOnError)
	fs.SetOutput(w)
	var f scorecardFlags
	var children bool
	fs.BoolVar(&children, "children", true, "roll up through the component's children")
	if err := parseScorecardFlags(fs, &f, args[1:]); err != nil {
		return err
	}
	return scCall(w, f.asJSON, protocol.MethodScorecardComponent,
		protocol.ScorecardComponentParams{ID: args[0], IncludeChildren: children, ScorecardWindow: f.window()},
		func(raw json.RawMessage) error {
			var r protocol.ScorecardComponentResult
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			printCard(w, r.Card)
			if len(r.Breakdown) > 0 {
				fmt.Fprintln(w, "children:")
				for _, b := range r.Breakdown {
					fmt.Fprintf(w, "  %-24s loops=%-4d done=%-4d blocked=%d\n", b.Component, b.Loops, b.Done, b.Blocked)
				}
			}
			fmt.Fprintln(w, r.Statement)
			return nil
		})
}

// scorecardCompare prints the TWO cards the daemon answers — the subject
// over two windows, never a third computed thing (RT8: no delta field
// exists to print, and none is invented here).
func scorecardCompare(w io.Writer, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("scorecard compare needs KIND and SUBJECT (component, agent or blueprint)")
	}
	fs := flag.NewFlagSet("scorecard compare", flag.ContinueOnError)
	fs.SetOutput(w)
	var a, b string
	var asJSON bool
	fs.StringVar(&a, "a", "", "window A as SINCE..UNTIL (RFC3339, either end may stand alone)")
	fs.StringVar(&b, "b", "", "window B as SINCE..UNTIL")
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	wa, err := parseCompareWindow("--a", a)
	if err != nil {
		return err
	}
	wb, err := parseCompareWindow("--b", b)
	if err != nil {
		return err
	}
	return scCall(w, asJSON, protocol.MethodScorecardCompare,
		protocol.ScorecardCompareParams{Kind: args[0], Subject: args[1], WindowA: wa, WindowB: wb},
		func(raw json.RawMessage) error {
			var r protocol.ScorecardCompareResult
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			fmt.Fprintln(w, "A:")
			printCard(w, r.A)
			fmt.Fprintln(w, "B:")
			printCard(w, r.B)
			fmt.Fprintln(w, r.Statement)
			return nil
		})
}

// parseCompareWindow splits SINCE..UNTIL. Only a malformed TIME is refused
// here — a bad kind or subject is the daemon's sentence, verbatim.
func parseCompareWindow(flagName, v string) (protocol.ScorecardWindow, error) {
	var win protocol.ScorecardWindow
	if v == "" {
		return win, nil
	}
	since, until, _ := strings.Cut(v, "..")
	for _, s := range []string{since, until} {
		if s != "" {
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				return win, fmt.Errorf("%s: not an RFC3339 time: %q", flagName, s)
			}
		}
	}
	win.Since, win.Until = since, until
	return win, nil
}

func scorecardVocabulary(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("scorecard vocabulary", flag.ContinueOnError)
	fs.SetOutput(w)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return scCall(w, asJSON, protocol.MethodScorecardVocabulary, struct{}{}, func(raw json.RawMessage) error {
		var r protocol.ScorecardVocabularyResult
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		fmt.Fprintln(w, "metrics:")
		for _, m := range r.Metrics {
			fmt.Fprintf(w, "  %-24s [%s]  %s\n", m.Name, m.Scope, m.Description)
		}
		fmt.Fprintf(w, "states: %s\n", strings.Join(r.States, " · "))
		fmt.Fprintf(w, "minSample: %d\n", r.MinSample)
		if len(r.Windows) > 0 {
			var names []string
			for _, wp := range r.Windows {
				names = append(names, wp.Name)
			}
			fmt.Fprintf(w, "windows: %s\n", strings.Join(names, " · "))
		}
		if len(r.Milestones) > 0 {
			var ms []string
			for _, m := range r.Milestones {
				ms = append(ms, strconv.Itoa(m))
			}
			fmt.Fprintf(w, "milestones: %s\n", strings.Join(ms, " · "))
		}
		fmt.Fprintln(w, r.Statement)
		return nil
	})
}

// printCard is the human rendering, in the spec's order: coverage first,
// window, subject, then every metric as `name  value  (n=…)  [state]` —
// alphabetical, because presentation order is not a rank. The card's own
// rendered cost sentence prints as the value when the metric carries one;
// a NULL value prints its state word, never a zero (RT7).
func printCard(w io.Writer, c protocol.CardWire) {
	fmt.Fprintf(w, "coverage: %d of %d loops recorded (%d not recorded, since %s)\n",
		c.Coverage.LoopsRecorded, c.Coverage.LoopsInHistory, c.Coverage.LoopsNotRecorded, dash(c.Coverage.RecordedSince))
	fmt.Fprintf(w, "window: %s .. %s\n", dash(c.Window.Since), dash(c.Window.Until))
	fmt.Fprintf(w, "subject: %s %s\n", c.Subject.Kind, c.Subject.ID)
	names := make([]string, 0, len(c.Metrics))
	for name := range c.Metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "metrics:")
	for _, name := range names {
		fmt.Fprintln(w, formatMetricLine(name, c.Metrics[name]))
	}
}

// formatMetricLine is the ONE metric line shape. Exported to tests by
// being the only thing between a wire metric and its printed line.
func formatMetricLine(name string, m protocol.MetricWire) string {
	value := "—"
	if rendered, ok := m.Detail["rendered"].(string); ok && rendered != "" {
		value = rendered
	} else if m.Value != nil {
		value = strconv.FormatFloat(*m.Value, 'f', -1, 64)
	}
	line := fmt.Sprintf("  %-24s %s  (n=%d)  [%s]", name, value, m.N, m.State)
	if m.Note != "" {
		line += "  " + m.Note
	}
	return line
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
