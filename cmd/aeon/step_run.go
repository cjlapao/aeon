package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// `aeon step "<prompt>"` — **the slice's demo, in one command**.
//
// # Why the bare form runs rather than printing usage
//
// S4's `step` verb took subcommands (`show`, `verify`, `list`) because every
// one of them read something. S5 adds the one that DOES something, and the
// thing a person types first is the prompt — so `aeon step "count the files in
// docs/"` runs a step, and the reading subcommands keep their names.
//
// `run` is accepted as an explicit spelling, for a script that would rather not
// depend on a prompt never colliding with a subcommand name.
//
// # Streaming
//
// The call BLOCKS for the whole step (the method returns when the step is
// done), so the events are drained in this goroutine while the call runs in
// another. The client drops events rather than blocking its read loop, and a
// step emitting a delta per token would overrun a 256-event buffer in a few
// seconds — so draining concurrently is not an optimisation, it is the
// difference between seeing the step and seeing a summary.

// stepRunFlags are the run verb's flags, declared once so the verb and the
// test that reads their descriptions cannot drift.
type stepRunFlags struct {
	workspace     *string
	toolList      *string
	allowBash     *bool
	allowQuestion *bool
	contextFiles  *string
	maxIterations *int
	route         *string
	loopSlug      *string
	quiet         *bool
	asJSON        *bool
	effort        *string
	want          *int
}

func cmdStepRunFlags(fs *flag.FlagSet) stepRunFlags {
	return stepRunFlags{
		workspace: fs.String("workspace", "",
			"the component the step reads; defaults to the current directory"),
		toolList: fs.String("tools", "", "comma-separated grant list; defaults to `read,list,write`"),
		// The consequence is IN the flag's description, because `--help` is
		// where somebody reads about a flag before using it, and "grant the
		// shell tool" on its own sounds like a capability rather than a
		// decision. The description is S11's truth (T19): the command policy
		// the earlier apology apologized for missing now exists, and it
		// speaks at the moment it acts — an unruled command denies, a ruled
		// ask suspends for an answer.
		allowBash: fs.Bool("allow-bash", false,
			"also grant the shell. Commands are governed by command_access policy (S11): "+
				"an unruled command denies, a ruled ask suspends for an answer. Runs stay "+
				"bounded by a timeout and the iteration limit. "+tools.BashHonesty),
		allowQuestion: fs.Bool("allow-question", false, "allow the step to ask a question and suspend until aeon answer is run"),
		contextFiles:  fs.String("context-files", "", "comma-separated workspace files to freeze in the opening context and check on resume"),
		maxIterations: fs.Int("max-iterations", 0, "override the loop bound; 0 uses the default"),
		effort: fs.String("reasoning-effort", "",
			"reasoning for THIS step: none|minimal|low|medium|high|xhigh|max. Omitted is the right answer "+
				"almost always — the step lowers the route's effort to `none` by itself, because "+
				"models with this dial refuse tools while it is on, and it says so when it does"),
		route:    fs.String("route", "", "route id; with one configured route it is optional"),
		loopSlug: fs.String("loop", "", "the loop to file the step under; the default is `step`"),
		quiet:    fs.Bool("quiet", false, "print only the outcome, not the events as they happen"),
		asJSON:   fs.Bool("json", false, "print the result as JSON instead of prose"),
		want:     addProtocolVersionFlag(fs),
	}
}

func cmdStepRun(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("step", flag.ContinueOnError)
	fs.SetOutput(w)
	f := cmdStepRunFlags(fs)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// `--help` is a question, not a failure. Printing the flag list and
			// then exiting 1 with "flag: help requested" makes a verb look
			// broken to anyone reading it for the first time — and to a script
			// that checks exit codes.
			printStepUsage(w)
			return nil
		}
		return err
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		printStepUsage(w)
		return fmt.Errorf("aeon step needs a prompt: `aeon step \"count the files in docs/\"`")
	}

	root := *f.workspace
	if root == "" {
		// The CLIENT's working directory, sent explicitly. The daemon has no
		// default for it (Q10) because the daemon multiplexes components and its
		// own directory is nobody's workspace.
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("the current directory could not be read, so there is no workspace to "+
				"send: %w", err)
		}
		root = cwd
	}

	granted, err := grantList(*f.toolList, *f.allowBash)
	if err != nil {
		return err
	}
	if *f.allowQuestion && !contains(granted, "question") {
		granted = append(granted, "question")
	}

	c, err := dial(*f.want)
	if err != nil {
		return err
	}
	defer c.Close()

	// Subscribe FIRST, for the reason `ask` does: the subscription exists
	// before the call that streams into it, so no early event has nowhere to
	// go.
	var subID string
	if !*f.quiet {
		// BOTH prefixes. `model.delta` is the model's own text arriving and is
		// NOT a `step.*` kind — a filter of `step.*` alone subscribes to the
		// bookkeeping and misses the answer, which is what the first walk of
		// this slice's demo showed: a step that ran correctly and printed
		// nothing between "started" and "ok".
		sub, err := c.Subscribe(protocol.Filter{Kinds: []string{"step.*", "model.*", "question.*"}}, nil)
		if err != nil {
			return err
		}
		subID = sub.SubscriptionID
	}

	params := protocol.StepParams{
		Prompt:          prompt,
		Tools:           granted,
		SubscriptionID:  subID,
		MaxIterations:   *f.maxIterations,
		ReasoningEffort: strings.TrimSpace(*f.effort),
		WorkspaceRoot:   root,
		Route:           *f.route,
		LoopSlug:        *f.loopSlug,
		ContextFiles:    splitContextFiles(*f.contextFiles),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	type outcome struct {
		res protocol.StepResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		var res protocol.StepResult
		err := c.Call(protocol.MethodStep, params, &res)
		done <- outcome{res, err}
	}()

	if !*f.quiet {
		fmt.Fprintf(w, "step · %s\n", prompt)
		fmt.Fprintf(w, "workspace %s · tools %s\n\n", root, strings.Join(granted, ", "))
	}

	seenSeq := map[uint64]bool{}
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				// The connection went away mid-step. The step may well be
				// running still, which is worth saying — a person who sees
				// nothing will run it again.
				fmt.Fprintln(w, "\nthe connection closed before the step finished; "+
					"the daemon may still be running it — `aeon step list step` lists what was recorded")
				return fmt.Errorf("the connection closed during the step")
			}
			if !seenSeq[ev.Seq] && !*f.quiet {
				renderStepEvent(w, ev)
			}
			seenSeq[ev.Seq] = true

		case got := <-done:
			if got.err != nil {
				return got.err
			}
			// Drain what is already queued, so the last tool's result is not
			// lost to the race between the result and the final events.
			for {
				select {
				case ev := <-c.Events():
					if !seenSeq[ev.Seq] && !*f.quiet {
						renderStepEvent(w, ev)
					}
					seenSeq[ev.Seq] = true
					continue
				default:
				}
				break
			}
			return renderStepResult(w, got.res, *f.asJSON)

		case <-ctx.Done():
			// The step is the DAEMON's work, and this process interrupting
			// does not stop it. Saying so is the honest thing: the alternative
			// is a person believing they cancelled something that is still
			// spending money.
			fmt.Fprintln(w, "\ninterrupted. The step is running in the daemon and this only stopped "+
				"watching it — `aeon step list step` shows what was recorded.")
			return nil
		}
	}
}

func splitContextFiles(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

// grantList turns the flags into the grant the step is given.
//
// **`--allow-bash` grants the shell and prints nothing on stderr** (T19
// retired the every-use warning together with the apology it carried). The
// warning existed because S10 had no command policy at all and the grantor
// had to be told the door was unguarded; S11 shipped the policy the warning
// apologized for — every command is judged by command_access over the rules
// frozen in the step's manifest, an unruled command denies before it starts,
// a ruled ask suspends for an answer, and a command that runs is token-bound
// to its exact string. Enforcement now says all of that AT THE MOMENT IT
// ACTS (the refusal sentence, the ask body), and the limits worth reading
// BEFORE the decision live where they are read: the flag description above
// and the tool spec the model sees (G20's honesty string, verbatim in both).
// The grant behavior is untouched: `--allow-bash` still appends "bash", and
// the grant list stays the only thing that decides what a step can call.
func grantList(list string, allowBash bool) ([]string, error) {
	var granted []string
	if strings.TrimSpace(list) != "" {
		for _, name := range strings.Split(list, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			granted = append(granted, name)
		}
	}
	if len(granted) == 0 {
		granted = runtime.DefaultGrantNames()
	}

	if allowBash {
		if !contains(granted, "bash") {
			granted = append(granted, "bash")
		}
	}

	return granted, nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// renderStepEvent prints one event as it happens.
func renderStepEvent(w io.Writer, ev protocol.Event) {
	var p protocol.StepEventPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		return
	}

	switch ev.Kind {
	case runtime.EventStepStarted:
		fmt.Fprintf(w, "\033[2m· %s\033[0m\n", p.Summary)
	case runtime.EventModelDelta:
		// Printed raw, with no newline: it is the model's own text arriving.
		fmt.Fprint(w, p.Text)
	case runtime.EventToolStart:
		fmt.Fprintf(w, "\n\033[2m→ %s %s\033[0m\n", p.Tool, p.Summary)
	case runtime.EventToolEnd:
		marker := "←"
		if p.IsError {
			marker = "✗"
		}
		// The write line counts, when the call was a write (S31 D8): the
		// terminal says what the file cost, not just that something ran.
		counts := ""
		if p.LinesAdded > 0 || p.LinesRemoved > 0 {
			counts = fmt.Sprintf(" · +%d −%d", p.LinesAdded, p.LinesRemoved)
		}
		fmt.Fprintf(w, "\033[2m%s %s%s\033[0m\n", marker, firstLine(p.Result), counts)
	case runtime.EventTurnCompleted:
		// One model call, finished: its tokens and wall time. The streamed text above already said what it wrote.
		in, out := 0, 0
		if p.Usage != nil {
			if p.Usage.InputTokens != nil {
				in = *p.Usage.InputTokens
			}
			if p.Usage.OutputTokens != nil {
				out = *p.Usage.OutputTokens
			}
		}
		fmt.Fprintf(w, "\n\033[2m· turn %d done · %d in / %d out · %.1fs\033[0m\n", p.Iteration, in, out, float64(p.WallMs)/1000)
	case runtime.EventStepDone:
		fmt.Fprintf(w, "\n\033[2m· %s\033[0m\n", p.Outcome)
	case runtime.EventSteerDelivered:
		skipped := ""
		if n := len(p.SkippedTools); n > 0 {
			skipped = fmt.Sprintf(" · %d tool call(s) not run", n)
		}
		fmt.Fprintf(w, "\n\033[2m· steer #%d delivered at iteration %d%s\033[0m\n",
			p.SteerID, p.Iteration, skipped)
	case runtime.EventPolicyDecided:
		// The verdict sentence verbatim (§4d's one formatter), allowed calls
		// included — silence would read as "policy was not consulted".
		fmt.Fprintf(w, "\033[2m⛨ policy %s %s · %s\033[0m\n", p.Verdict, p.Tool, firstLine(p.Reason))
	case runtime.EventToolSkipped:
		fmt.Fprintf(w, "\033[2m⊘ %s %s not run — the person steered\033[0m\n", p.Tool, p.ToolUseID)
	case runtime.EventRouteChanged:
		effort := p.ReasoningEffort
		if effort == "" {
			effort = "default"
		}
		fmt.Fprintf(w, "\n\033[2m· route → %s (reasoning %s) from iteration %d\033[0m\n",
			p.Route, effort, p.FromIteration)
	case runtime.EventRouteDeferred:
		// The refusal in the daemon's own words (S18 T8, O5): the person
		// must SEE that their switch was not silently dropped — it waits
		// for the next step, and the sentence says exactly that.
		fmt.Fprintf(w, "\n\033[33m· %s\033[0m\n", firstLine(p.Text))
	case "step.steer.queued":
		what := "provider"
		if p.HasText {
			what = "instruction"
		}
		fmt.Fprintf(w, "\n\033[2m· steer #%d queued (%s)\033[0m\n", p.SteerID, what)
	case "step.steer.cancelled":
		fmt.Fprintf(w, "\n\033[2m· steer #%d withdrawn\033[0m\n", p.SteerID)
	case runtime.EventAgentMessageAsked:
		// The two agent-message kinds arrived from T0, which found them
		// rendering NOTHING and handed the red forward rather than absorbing it
		// silently: a kind nobody prints is a moment of the run nobody watching
		// the terminal ever sees, and an agent asking a peer — or waiting on one
		// — is exactly the moment a person watching wants to see. The summary is
		// the daemon's own text (the question body when the ask suspended the
		// step, the peer's answer when it did not); nothing is added here.
		tool := p.Tool
		if tool == "" {
			tool = "ask_agent"
		}
		fmt.Fprintf(w, "\n\033[2m· %s asks a peer · #%d · iteration %d\033[0m\n%s\n",
			tool, p.MessageID, p.Iteration, firstLine(p.Summary))
	case runtime.EventAgentMessageDelivered:
		// A peer's message arriving in the mailbox. The summary is the sender
		// slug, which is the fact worth printing: the step is now carrying words
		// that did not come from its own model.
		from := p.Summary
		if from == "" {
			// Three-state honesty (P31): a missing sender is printed as unknown,
			// never as a blank that reads like the step spoke to itself.
			from = "unknown"
		}
		fmt.Fprintf(w, "\n\033[2m· peer message #%d from %s delivered · iteration %d\033[0m\n",
			p.MessageID, from, p.Iteration)
	case runtime.EventStepCompacted:
		fmt.Fprintf(w, "\n\033[2m· compacted messages %d..%d · estimate %d → %d tokens\033[0m\n",
			p.Start, p.End, p.EstimateBefore, p.EstimateAfter)
	case runtime.EventTodoNudged:
		// S75: the runtime reminded the agent about open todo items. The
		// reminder text is a runtime turn in the transcript; this line says
		// that one was sent and why.
		fmt.Fprintf(w, "\n\033[2m· todo reminder (%s) · open: %s\033[0m\n", p.Reason, p.Summary)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 100 {
		s = s[:97] + "…"
	}
	return s
}

// renderStepResult prints the outcome, the accounting and what to read next.
func renderStepResult(w io.Writer, res protocol.StepResult, asJSON bool) error {
	if asJSON {
		raw, err := json.Marshal(res)
		if err != nil {
			return err
		}
		var pretty strings.Builder
		if err := indentJSON(&pretty, raw); err != nil {
			return err
		}
		fmt.Fprintln(w, pretty.String())
		return failedStepError(res)
	}
	if res.Kind == string(runtime.ResultSuspended) && res.Question != nil {
		fmt.Fprintf(w, "\n  ▸ question  %s\n", res.Question.Body)
		if len(res.Question.Options) > 0 {
			fmt.Fprintf(w, "    options   %s\n", strings.Join(res.Question.Options, " | "))
		}
		fmt.Fprintf(w, "\nsuspended · waiting for you\n  answer with:  aeon answer %d \"<answer>\"\n", res.StepID)
		return nil
	}

	fmt.Fprintf(w, "\nstep %d · %s\n", res.StepID, res.Outcome)
	if res.Reason != "" {
		fmt.Fprintf(w, "  %s\n", res.Reason)
	}
	if len(res.Handoff) > 0 {
		var pretty strings.Builder
		if err := indentJSON(&pretty, res.Handoff); err == nil {
			fmt.Fprintf(w, "\n%s\n", pretty.String())
		}
	}
	fmt.Fprintf(w, "\n  iterations %d\n", res.Iterations)
	fmt.Fprintf(w, "  cost       %s\n", renderCost(costView(res.Cost)))
	if res.SinkFailures > 0 {
		// Said rather than hidden: a client that saw no events and a
		// successful result would otherwise have no way to tell which half
		// broke.
		fmt.Fprintf(w, "  events     %d could not be delivered to this connection\n", res.SinkFailures)
	}
	fmt.Fprintf(w, "\nThe transcript is stored. `aeon step show %d` reads it, and "+
		"`aeon step verify %d` checks it round-trips.\n", res.StepID, res.StepID)

	return failedStepError(res)
}

// failedStepError makes a failed step a non-zero exit.
//
// `reject` and `blocked` are NOT errors: the step worked and the answer was no,
// or the step ran out of room. Only `failed` is — and a script that cares about
// the difference reads the outcome rather than the exit code.
func failedStepError(res protocol.StepResult) error {
	if res.Outcome == string(runtime.OutcomeFailed) {
		return fmt.Errorf("step %d failed", res.StepID)
	}
	return nil
}

func renderStepCompactions(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		fmt.Fprintln(w, "no compactions recorded")
		return nil
	}
	var snapshot struct {
		Compactions []runtime.CompactionRecord `json:"compactions"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("read compactions: %w", err)
	}
	if len(snapshot.Compactions) == 0 {
		fmt.Fprintln(w, "no compactions recorded")
		return nil
	}
	for i, c := range snapshot.Compactions {
		fmt.Fprintf(w, "compaction %d · messages %d..%d · estimate %d → %d tokens · cost %s\n%s\n",
			i+1, c.Start, c.End, c.EstimateBefore, c.EstimateAfter, renderCost(costView(c.Cost)), c.Summary)
	}
	return nil
}

// --- step show --manifest / --requests ---------------------------------------

// renderStepManifest prints the frozen context manifest.
//
// The manifest is what the step was ASSEMBLED from, frozen at assembly (K13).
// Printing it is how a person checks whether a step read the file they thought
// it did — and the hashes are how they check it was the version they thought.
func renderStepManifest(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		fmt.Fprint(w, "this step has no stored manifest\n")
		return nil
	}

	var manifest runtime.ContextManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		// Printed raw rather than refused: a manifest this build cannot parse
		// is still the one thing a person has to look at.
		fmt.Fprintf(w, "the stored manifest does not match this build's shape (%v). Raw:\n%s\n",
			err, string(raw))
		return nil
	}

	fmt.Fprintf(w, "iterations %d of %d\n", manifest.IterationsUsed, manifest.MaxIterations)
	if !manifest.DeadlineAt.IsZero() {
		fmt.Fprintf(w, "deadline   %s\n", manifest.DeadlineAt.UTC().Format(time.RFC3339))
	}
	if len(manifest.Files) == 0 {
		fmt.Fprint(w, "\nNo declared files. This step read what it needed with tools rather than being\n"+
			"handed files at assembly, which is what the ad-hoc handler does.\n")
		return nil
	}
	fmt.Fprintf(w, "\n%d declared file(s):\n", len(manifest.Files))
	for _, f := range manifest.Files {
		fmt.Fprintf(w, "  %-40s %8d bytes  %s\n", f.Path, f.Bytes, f.Hash)
	}
	fmt.Fprint(w, "\nThe hashes are of what was read AT ASSEMBLY. A file that changed since is a\n"+
		"file this step did not see the current version of.\n")
	return nil
}

// renderStepRequests reconstructs the model-call boundaries from the transcript.
//
// # What this can and cannot show
//
// It CAN show how many calls the step made and what each one carried, because
// each assistant turn is one call's answer and everything before it is what was
// sent.
//
// It CANNOT show the tools each call offered, because the transcript stores the
// conversation rather than the request envelope — so the tool-free retry (K4)
// is visible here as a final assistant turn with no tool call, not as a request
// with no tools. The test that asserts the retry carries no tools asserts it on
// the request object in `internal/runtime`, which is the only place the object
// exists.
func renderStepRequests(w io.Writer, raw json.RawMessage) error {
	var messages []json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return fmt.Errorf("the stored transcript is not a JSON array of messages: %w", err)
		}
	}
	if len(messages) == 0 {
		fmt.Fprint(w, "this step has no stored transcript\n")
		return nil
	}

	type turn struct {
		Role    string `json:"role"`
		Content []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"content"`
	}

	call := 0
	sent := 0
	for _, raw := range messages {
		var t turn
		if err := json.Unmarshal(raw, &t); err != nil {
			continue
		}
		sent++
		if t.Role != "assistant" {
			continue
		}
		call++
		var tools []string
		for _, blk := range t.Content {
			if blk.Kind == "tool_use" {
				tools = append(tools, blk.Name)
			}
		}
		switch len(tools) {
		case 0:
			fmt.Fprintf(w, "call %d · %d message(s) sent · the model answered with text\n", call, sent-1)
		default:
			fmt.Fprintf(w, "call %d · %d message(s) sent · the model asked for %s\n",
				call, sent-1, strings.Join(tools, ", "))
		}
	}

	fmt.Fprintf(w, "\n%d model call(s), reconstructed from the transcript.\n", call)
	fmt.Fprint(w, "What this cannot show: which tools each call OFFERED. The transcript stores the\n"+
		"conversation, not the request envelope — so a tool-free retry appears here as a\n"+
		"final text answer rather than as a request with no tools.\n")
	return nil
}

// costView converts the model layer's cost to the protocol's view, so the CLI
// has ONE cost renderer.
//
// Two renderers is how `partial` ends up printed as a plain number in one place
// — and the whole point of the three states is that a surface which cannot show
// `unknown` shows a confident wrong answer instead.
func costView(c model.Cost) protocol.CostView {
	return protocol.CostView{
		State:      string(c.State),
		USD:        c.USD,
		USDAtLeast: c.USDAtLeast,
		Missing:    c.Missing,
		Reason:     c.Reason,
	}
}

// stepFlagSetForTest exposes the run flags for the test that asserts a flag's
// description says what the flag gives away.
//
// A test-only export rather than a duplicated literal: a copy of the
// description in a test is a copy that stays right while the real one changes.
func stepFlagSetForTest() *flag.FlagSet {
	var discard strings.Builder
	fs := flag.NewFlagSet("step", flag.ContinueOnError)
	fs.SetOutput(&discard)
	cmdStepRunFlags(fs)
	return fs
}
