package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
)

// `aeon ask` — the first verb that does something a person would pay for.
//
// It **subscribes before calling `ask`**, which is what makes a missed early
// delta impossible without buffering (M23). Doing it the other way round works
// almost always, and fails on a fast model.

func cmdAsk(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(w)
	route := fs.String("route", "", "route id to call (required)")
	maxTokens := fs.Int("max-tokens", 0, "output cap; 0 uses the route's")
	temperature := fs.Float64("temperature", -1, "sampling temperature; -1 leaves it unset")
	system := fs.String("system", "", "an optional system prompt")
	effort := fs.String("reasoning-effort", "",
		"reasoning for THIS call: none|minimal|low|medium|high|xhigh|max. Empty uses the route's. A chat can "+
			"reason freely — it offers no tools, so nothing lowers it")
	showTranscript := fs.Bool("show-transcript", false,
		"print the canonical transcript as JSON when the reply is complete")
	saveStep := fs.Bool("save-step", false,
		"persist what this call produced as a step (S4), and print its id")
	loopSlug := fs.String("loop", "ask", "the loop to save the step under, with --save-step")
	comp := componentFlag(fs)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	prompt := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if prompt == "" {
		return fmt.Errorf("aeon ask needs a prompt: `aeon ask --route <id> \"your question\"`")
	}
	if *route == "" {
		return fmt.Errorf("--route is required; `aeon debug roundtrip` and the daemon's log list " +
			"the configured ids")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	// **Subscribe first.** The subscription exists before the call that will
	// stream into it, so there is no window in which a delta has nowhere to go.
	sub, err := c.Subscribe(protocol.Filter{Kinds: []string{"ask.*"}}, nil)
	if err != nil {
		return err
	}

	params := protocol.AskParams{
		Route:           *route,
		Prompt:          prompt,
		SubscriptionID:  sub.SubscriptionID,
		MaxOutputTokens: *maxTokens,
		System:          *system,
		ReasoningEffort: strings.TrimSpace(*effort),
		// The bridge between S3 and S4, and the reason the demo can restart
		// the daemon and still find something.
		SaveStep:      *saveStep,
		LoopSlug:      *loopSlug,
		ComponentRoot: *comp,
	}
	if *temperature >= 0 {
		params.Temperature = temperature
	}

	var result protocol.AskResult
	if err := c.Call(protocol.MethodAsk, params, &result); err != nil {
		return err
	}

	// Ctrl-C cancels the call and still prints what arrived, with a cost line.
	// Those tokens were spent.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return streamAsk(ctx, w, c, result.AskID, prompt, *system, *showTranscript)
}

// streamAsk prints deltas as they arrive and the accounting at the end.
func streamAsk(ctx context.Context, w io.Writer, c *protocol.Client, askID, prompt, system string, showTranscript bool) error {
	var reply strings.Builder
	var thinking strings.Builder
	var toolCalls []model.Block
	printedThinkingHeader := false

	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "the connection closed before the reply finished")
				return nil
			}

			switch ev.Kind {
			case protocol.KindAskDelta:
				var p protocol.AskDeltaPayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil || p.AskID != askID {
					continue
				}
				switch p.Kind {
				case string(model.DeltaThinking):
					// Rendered separately and dimly: reasoning is not the
					// answer, and inlining it makes the answer hard to find.
					if !printedThinkingHeader {
						fmt.Fprintf(w, "\033[2mthinking: ")
						printedThinkingHeader = true
					}
					fmt.Fprint(w, p.Text)
					thinking.WriteString(p.Text)
				case string(model.DeltaToolUse):
					if p.Block != nil {
						toolCalls = append(toolCalls, *p.Block)
						fmt.Fprintf(w, "\n  → %s(%s)\n", p.Block.Name, p.Block.Input)
					}
				default:
					if printedThinkingHeader {
						fmt.Fprintf(w, "\033[0m\n\n")
						printedThinkingHeader = false
					}
					fmt.Fprint(w, p.Text)
					reply.WriteString(p.Text)
				}

			case protocol.KindAskDone:
				var p protocol.AskDonePayload
				if err := json.Unmarshal(ev.Payload, &p); err != nil || p.AskID != askID {
					continue
				}
				if printedThinkingHeader {
					fmt.Fprintf(w, "\033[0m")
				}
				fmt.Fprintln(w)
				fmt.Fprintln(w)
				printAccounting(w, p)

				if p.StepID > 0 {
					// The id, and what it is for. A bare number would leave a
					// person to guess which verb takes it.
					fmt.Fprintf(w, "step    %d  ·  `aeon step show %d` · `aeon step verify %d`\n",
						p.StepID, p.StepID, p.StepID)
					if thinking.Len() > 0 {
						// Said plainly, because it is the limitation a reader
						// most needs to know: this surface carries no
						// signature, so the saved thinking block is UNSIGNED
						// and S7 could not replay it to Anthropic. The store's
						// own tests write and read a signed block
						// byte-identically; it is the streaming bridge that
						// cannot supply one.
						fmt.Fprint(w, "        the saved thinking block is UNSIGNED — `ask`'s stream "+
							"carries no signature (S3)\n")
					}
				}

				if showTranscript {
					printTranscript(w, prompt, system, reply.String(), thinking.String(), toolCalls)
				}
				if p.Error != nil {
					// Returned as the structured error, so `renderError`
					// prints all three parts — the same shape a synchronous
					// failure gets.
					return p.Error
				}
				return nil
			}

		case <-ctx.Done():
			// **Still prints a cost line.** The daemon emits `ask.done` on
			// cancellation too (M15), but a client that has stopped listening
			// would never see it — so say what is known rather than nothing.
			fmt.Fprintln(w)
			fmt.Fprintln(w)
			fmt.Fprintln(w, "interrupted — the daemon is still accounting for what was spent;")
			fmt.Fprintln(w, "run `aeon watch --kinds ask.done` to see it.")
			return nil

		case <-c.Closed():
			fmt.Fprintln(w)
			fmt.Fprintln(w, "the daemon closed the connection")
			return nil
		}
	}
}

// printAccounting prints usage and the cost line.
func printAccounting(w io.Writer, p protocol.AskDonePayload) {
	fmt.Fprintf(w, "%s\n", costLine(p.Cost))

	var parts []string
	if n := p.Usage.InputTokens; n != nil {
		parts = append(parts, fmt.Sprintf("in %d", *n))
	}
	if n := p.Usage.OutputTokens; n != nil {
		parts = append(parts, fmt.Sprintf("out %d", *n))
	}
	if n := p.Usage.CacheReadTokens; n != nil {
		parts = append(parts, fmt.Sprintf("cache-read %d", *n))
	}
	if n := p.Usage.CacheWriteTokens; n != nil {
		parts = append(parts, fmt.Sprintf("cache-write %d", *n))
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "tokens  %s\n", strings.Join(parts, " · "))
	} else {
		// Said explicitly. Printing nothing would read as zero tokens.
		fmt.Fprintf(w, "tokens  not reported\n")
	}
	if p.StopReason != "" {
		fmt.Fprintf(w, "stop    %s\n", p.StopReason)
	}
	if p.Dropped > 0 {
		fmt.Fprintf(w, "dropped %d provider block(s) that map to no canonical kind\n", p.Dropped)
	}
	for _, id := range p.OwedToolErrors {
		fmt.Fprintf(w, "note    tool call %s had unparseable arguments; a tool_result with "+
			"isError is owed\n", id)
	}
}

// costLine renders a cost in **three distinct forms**.
//
// # Why this function exists rather than a format string
//
// The three states are the whole point of the cost design, and the way they get
// lost is a client that formats `usd` and hopes. An `unknown` cost rendered as
// `$0.00` is the exact failure M9 and M10 exist to prevent, and a bare `—`
// is barely better: it says nothing about why.
func costLine(c model.Cost) string {
	switch c.State {
	case model.CostKnown:
		return fmt.Sprintf("cost    %s", model.MoneyString(c.USD))

	case model.CostPartial:
		// "at least", and what is missing. A floor presented as a total is a
		// number somebody will add up.
		missing := "some token counts were not reported"
		if len(c.Missing) > 0 {
			missing = "missing: " + strings.Join(c.Missing, ", ")
		}
		return fmt.Sprintf("cost    at least %s  (%s)", model.MoneyString(c.USDAtLeast), missing)

	case model.CostUnknown:
		line := "cost    not reported"
		if c.Reason != "" {
			line += "  (" + explainCostReason(c.Reason) + ")"
		}
		if c.USDAtLeast > 0 {
			line += fmt.Sprintf("  — at least %s was measured", model.MoneyString(c.USDAtLeast))
		}
		return line

	default:
		// A state this binary does not know. Said plainly rather than rendered
		// as one of the three, because guessing which would be a lie.
		return fmt.Sprintf("cost    unrecognised state %q — this client is older than the daemon",
			c.State)
	}
}

// explainCostReason turns a reason code into a sentence a person can act on.
func explainCostReason(reason string) string {
	switch reason {
	case model.ReasonNoPriceForRoute:
		return "the route has no prices configured"
	case model.ReasonProviderOmittedUsage:
		return "the provider reported no token counts"
	case model.ReasonStreamAbortedEarly:
		return "the stream ended before any usage arrived"
	default:
		return reason
	}
}

// printTranscript prints the canonical messages as JSON.
//
// So a person can see a signed `thinking` block and feed it to `debug
// roundtrip` — which is the human-visible form of the fidelity test.
func printTranscript(w io.Writer, prompt, system, reply, thinking string, toolCalls []model.Block) {
	var msgs []model.Message
	if system != "" {
		msgs = append(msgs, model.Message{
			Role: model.RoleSystem, Content: []model.Block{model.TextBlock(system)},
		})
	}
	msgs = append(msgs, model.Message{
		Role: model.RoleUser, Content: []model.Block{model.TextBlock(prompt)},
	})

	var assistant []model.Block
	if thinking != "" {
		// The signature is NOT available here: it never reaches a client,
		// because it is replay metadata rather than display content. Said so,
		// rather than emitting a thinking block that looks complete and would
		// be rejected on replay.
		assistant = append(assistant, model.ThinkingBlock(thinking, ""))
	}
	if reply != "" {
		assistant = append(assistant, model.TextBlock(reply))
	}
	assistant = append(assistant, toolCalls...)
	if len(assistant) > 0 {
		msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: assistant})
	}

	raw, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "\n(the transcript could not be rendered: %v)\n", err)
		return
	}
	fmt.Fprintf(w, "\ntranscript\n%s\n", raw)
	if thinking != "" {
		fmt.Fprintln(w, "\nnote: the thinking block above carries NO signature. A signature is "+
			"replay metadata and never reaches a client, so this transcript is for reading "+
			"rather than for replaying — S5 persists the signed version daemon-side.")
	}
}
