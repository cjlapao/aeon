package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
)

type loopDetail struct {
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	Blueprint     string   `json:"blueprint"`
	Version       int      `json:"version"`
	Goal          string   `json:"goal"`
	Root          string   `json:"root"`
	Status        string   `json:"status"`
	QuestionID    int64    `json:"questionId"`
	GateAnswers   []string `json:"gateAnswers,omitempty"`
	GatePrompt    string   `json:"gatePrompt,omitempty"`
	PinnedContent string   `json:"pinnedContent"`
	LastReason    string   `json:"lastReason"`
	// S17 T2: the two-level budgets, rendered by `--counters` and read
	// verbatim by the app panel — the CLI never tallies its own.
	LoopCounters map[string]int `json:"loopCounters,omitempty"`
	TaskCounters []struct {
		Task        string `json:"task"`
		Counter     string `json:"counter"`
		Level       string `json:"level"`
		Value       int    `json:"value"`
		Cap         int    `json:"cap"`
		CapExceeded bool   `json:"capExceeded"`
	} `json:"taskCounters,omitempty"`
	Position struct {
		Lane string `json:"lane"`
		Item string `json:"item"`
		Step string `json:"step"`
	} `json:"position"`
	// CostAttribution is the daemon-computed ledger (S18 T7, D16): this
	// client RENDERS it and recomputes nothing — a CLI-side sum would be
	// the second counter all over again.
	CostAttribution *model.Attribution `json:"costAttribution,omitempty"`
	Lanes           []struct {
		ID        string   `json:"id"`
		Status    string   `json:"status"`
		Condition string   `json:"condition"`
		Iterate   string   `json:"iterate"`
		ItemCount int      `json:"itemCount"`
		Produces  []string `json:"produces"`
		Steps     []struct {
			ID      string          `json:"id"`
			Kind    string          `json:"kind"`
			Status  string          `json:"status"`
			Outcome string          `json:"outcome"`
			Reason  string          `json:"reason"`
			StepID  int64           `json:"stepId"`
			Cost    json.RawMessage `json:"cost"`
		} `json:"steps"`
	} `json:"lanes"`
}

func cmdLoopStart(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop start", flag.ContinueOnError)
	fs.SetOutput(w)
	name := fs.String("blueprint", "two-step", "blueprint name or YAML path")
	route := fs.String("route", "", "model route; empty uses the only configured route")
	mode := fs.String("mode", "standard", "simple, standard or poc")
	slug := fs.String("slug", "", "optional loop slug")
	// T10: the operator's tech tag for a `by-tech-tag` lane. It enters the
	// loop's durable `with` map at pin time — an unclaimed tag is refused at
	// dispatch, never defaulted (A13).
	tech := fs.String("tech", "", "tech tag for resolver lanes (go, python, javascript, rust, sql, react-ts, vue-ts, tauri-react, tauri-vue, websocket); empty means unclaimed")
	// D2: attendedness is DECLARED at start, and a human typing this verb at
	// a terminal IS attended — so the flag defaults true rather than asking.
	// --attended=false is the deliberate statement that nobody is there to
	// answer questions (a scheduled or scripted start).
	// T12's walk: the operator's loop-level with entries — a single-task
	// loop declares task (and titles) at start; absent is absent and the
	// registry refuses naming the missing argument, never guesses.
	with := withFlag{}
	fs.Var(&with, "with", "loop-level with entry key=value (repeatable), e.g. --with task=4a17")
	attended := fs.Bool("attended", true, "declare whether a human is present to answer questions, pinned at start; --attended=false declares nobody is (a question then cannot be answered — it denies rather than waits)")
	root := componentFlag(fs)
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop start --blueprint NAME \"GOAL\"")
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var result struct {
		ID        string `json:"id"`
		Slug      string `json:"slug"`
		Blueprint string `json:"blueprint"`
		Status    string `json:"status"`
	}
	if err := c.Call(protocol.MethodLoopStart, protocol.LoopStartParams{Blueprint: *name, Goal: positional[0], WorkspaceRoot: *root, Route: *route, Mode: *mode, Slug: *slug, Attended: attended, Tech: *tech, With: with}, &result); err != nil {
		return err
	}
	fmt.Fprintf(w, "loop %s  %s  %s\n", result.ID, result.Blueprint, result.Status)
	fmt.Fprintf(w, "Use `aeon loop show %s` to watch the ladder.\n", result.ID)
	return nil
}

func cmdLoopShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop show", flag.ContinueOnError)
	fs.SetOutput(w)
	pinned := fs.Bool("blueprint", false, "print the pinned YAML, not the current source file")
	asJSON := fs.Bool("json", false, "print the loop view as JSON")
	_ = fs.Bool("wide", false, "show the full ladder (the default while S8 is a test bench)")
	counters := fs.Bool("counters", false, "print the two-level budgets — task counters per task, loop counters once (S17)")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		return fmt.Errorf("usage: aeon loop show ID [--blueprint] [--json]")
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var v loopDetail
	if err := c.Call(protocol.MethodLoopShow, protocol.LoopShowParams{ID: positional[0], IncludeBlueprint: *pinned}, &v); err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	fmt.Fprintf(w, "loop %s  %s@%d  %s\n", v.ID, v.Blueprint, v.Version, v.Status)
	if v.Goal != "" {
		fmt.Fprintf(w, "  goal: %s\n", v.Goal)
	}
	for _, lane := range v.Lanes {
		fmt.Fprintf(w, "  %-18s %-12s", lane.ID, lane.Status)
		if len(lane.Produces) > 0 {
			fmt.Fprintf(w, " %s", strings.Join(lane.Produces, ", "))
		}
		if lane.Iterate != "" {
			fmt.Fprintf(w, " iterate: %s (%d)", lane.Iterate, lane.ItemCount)
		}
		if lane.Condition != "" {
			fmt.Fprintf(w, " (when: %s)", lane.Condition)
		}
		fmt.Fprintln(w)
		for _, step := range lane.Steps {
			if step.Reason != "" {
				fmt.Fprintf(w, "      %s: %s\n", step.ID, step.Reason)
			}
		}
	}
	if *counters {
		for _, row := range v.TaskCounters {
			if row.Cap > 0 {
				mark := ""
				if row.CapExceeded {
					mark = "  EXHAUSTED"
				}
				fmt.Fprintf(w, "  %-10s %-16s %d/%d%s\n", row.Task, row.Counter, row.Value, row.Cap, mark)
			} else {
				fmt.Fprintf(w, "  %-10s %-16s %d\n", row.Task, row.Counter, row.Value)
			}
		}
		for _, name := range []string{"total_failed_gates", "splits_done", "qa_rounds"} {
			value, ok := v.LoopCounters[name]
			if !ok {
				continue
			}
			if name == "total_failed_gates" {
				fmt.Fprintf(w, "  %-10s %-16s %d/10\n", "loop", name, value)
			} else if name == "qa_rounds" {
				fmt.Fprintf(w, "  %-10s %-16s %d/2\n", "loop", name, value)
			} else {
				fmt.Fprintf(w, "  %-10s %-16s %d\n", "loop", name, value)
			}
		}
	}
	// The cost ledger (S18 T7): per-route lines and the total, printed when
	// the daemon computed one. `aeon loop show` shows what `loop.show`
	// answered — the same figures the app's footprint panel renders.
	if v.CostAttribution != nil {
		writeCostLedger(w, v.CostAttribution)
	}
	if v.QuestionID > 0 {
		fmt.Fprintf(w, "  question %d is waiting; answer with `aeon answer %s CHOICE`\n", v.QuestionID, v.ID)
	}
	if v.LastReason != "" {
		fmt.Fprintf(w, "  reason: %s\n", v.LastReason)
	}
	if *pinned {
		fmt.Fprintln(w, "\n--- pinned blueprint ---")
		fmt.Fprint(w, v.PinnedContent)
	}
	return nil
}

// withFlag collects repeatable --with key=value entries into the loop's
// durable with map. It validates the SHAPE at the flag (key=value); it
// validates nothing about the VALUES — those are the loop's data, judged
// where they are consumed.
type withFlag map[string]string

func (w withFlag) String() string { return "" }

func (w withFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("--with wants key=value, got %q", v)
	}
	w[k] = val
	return nil
}

// writeCostLedger prints the daemon-computed ledger (S18 T7): per-route
// lines and the total, grouped by route rather than provider (O4), with the
// total over an unknown read as the floor it is — "at least", in words (D5).
//
// It FORMATS and never sums: every figure, every state word and the
// "at least" itself arrive from model.AttributeCosts through `loop.show`.
// A CLI that kept its own tally would be the second counter all over again
// (D16, P34), and one that printed `$0.00` for a route nobody priced would
// be the claim this task exists to forbid (D9, M30).
func writeCostLedger(w io.Writer, at *model.Attribution) {
	fmt.Fprintln(w, "  cost by route:")
	for _, g := range at.Groups {
		provider := g.Provider
		if provider == "" {
			// The unattributed line: steps with no recorded provenance.
			// Absence spoken aloud, never a guessed row (P31).
			provider = "(unattributed)"
		}
		steps := "step"
		if g.Steps != 1 {
			steps = "steps"
		}
		fmt.Fprintf(w, "    %-8s %-22s %s  (%d %s)\n", provider, g.Model, g.Sentence(), g.Steps, steps)
	}
	fmt.Fprintf(w, "    %20s  %s\n", "total", at.TotalSentence())
}
