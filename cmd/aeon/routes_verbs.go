package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
)

// `aeon routes` — list, set, delete.
//
// # Why this verb arrives with S4 rather than with S3, which built routes
//
// S3 added route management as protocol methods and exercised them through the
// app, which was the right place: a settings form is a settings form. There was
// no CLI counterpart because there was nothing durable to inspect — a route set
// in one process was gone in the next.
//
// S4 made routes persist, and that changes the argument: "is my configuration
// actually saved" is now a real question, and answering it should not require
// opening a window. It is also the only way the integration tests can check the
// restart, and a property no test can reach is a property that regresses.
//
// # THE CREDENTIAL IS A PATH, NEVER A VALUE
//
// `--api-key-file`, and there is deliberately no `--api-key`. C17 and S19's O1:
// a value in `argv` is in the process list and in shell history, and that
// failure is silent — the person who typed it has no reason to think the
// secret leaked. A path is not a secret.
//
// The file is read and the value goes over the socket once (0600, in a 0700
// directory, peer credentials checked against the running user — P26). It is
// never printed back: `routes list` shows `hasKey` and the last four
// characters, because a prefix identifies a key's type and sometimes its
// project, and a suffix only answers *which key is this* (AGENTS §4c).

func cmdRoutes(w io.Writer, args []string) error {
	if len(args) == 0 {
		printRoutesUsage(w)
		return nil
	}
	switch args[0] {
	case "list":
		return cmdRoutesList(w, args[1:])
	case "set":
		return cmdRoutesSet(w, args[1:])
	case "delete":
		return cmdRoutesDelete(w, args[1:])
	case "probe":
		return cmdRoutesProbe(w, args[1:])
	default:
		printRoutesUsage(w)
		return fmt.Errorf("unknown `routes` subcommand %q", args[0])
	}
}

func printRoutesUsage(w io.Writer) {
	fmt.Fprint(w, `aeon routes — providers, models and their prices

  routes list
  routes set --id ID --provider NAME --model NAME [--base-url URL]
             [--max-tokens N] [--api-key-file PATH] [--clear-key]
             [--context-window N]
             [--input-price N] [--output-price N] [--no-price]
             [--capabilities tools,streaming-usage,thinking,system-role,parallel-tools]
  routes delete --id ID
  routes probe ID

Probe runs the ONE minimal request that checks a route's declared wire flags
(S18): the verdict per flag comes back with its state, its timestamp and the
model it was earned against, and the earned ones are cached in the store so
they survive a restart. Only YOU run it — nothing in the daemon ever probes on
its own (M32).

Prices are USD PER MILLION TOKENS, and the unit is in the flag name because a
price per token and a price per million differ by a factor of a million.

--no-price is how a route is declared UNMETERED on purpose. A NEW persisted
route with no price and no --no-price is refused: a silently missing price is
how a typo makes a metered model report "unknown" forever (O8).

--capabilities declares the wire-behaviour flags of the server (S18 Q8):
tools, streaming-usage, thinking, system-role, parallel-tools. Omitting the
flag KEEPS the saved declaration; these describe how the SERVER behaves and
are separate from the model catalogue's capabilities. "routes list" prints
each flag with its state, which reads "declared" until a probe says otherwise.

There is no --api-key flag, deliberately: a credential in argv is in the process
list and in shell history, and that failure is silent. Use --api-key-file.
`)
}

func cmdRoutesList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("routes list", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.RoutesListResult
	if err := c.Call(protocol.MethodRoutesList, nil, &res); err != nil {
		return err
	}

	// Whether a change would SURVIVE is printed first, because it is the
	// question somebody running this verb most often has.
	if res.Persisted {
		fmt.Fprint(w, "routes are saved; credentials live beside the store, not in it\n\n")
	} else {
		fmt.Fprint(w, "NOT SAVED — this daemon has no store, so these are in memory and gone on restart\n\n")
	}

	if len(res.Routes) == 0 {
		fmt.Fprintf(w, "no routes configured\n  adapters available: %s\n",
			strings.Join(res.Providers, ", "))
		return nil
	}

	for _, r := range res.Routes {
		key := "no key"
		if r.HasKey {
			// The last four, never the prefix.
			key = "key …" + r.KeyHint
		}
		priced := "unpriced"
		if r.Priced {
			priced = "priced"
		}
		// The third answer (O8): declared unmetered on purpose reads
		// differently from "nobody said", so a listing cannot mistake a
		// decision for a typo.
		if r.NoPrice {
			priced = "unmetered"
		}
		fmt.Fprintf(w, "%-16s %-10s %-24s %-12s %-10s %-9s cap %d%s%s\n",
			r.ID, r.Provider, r.Model, string(r.Source), key, priced, r.MaxOutputTokens,
			effortSuffix(r.ReasoningEffort), endpointSuffix(r.OpenAIEndpoint))
		if r.BaseURL != "" {
			fmt.Fprintf(w, "%-16s   %s\n", "", r.BaseURL)
		}
		// Wire flags print with their STATE (M28). With a probe cache the
		// states are the cached verdicts — verified and contradicted carry
		// their timestamp and the model they were earned against (O2), so a
		// stale claim is visible at a glance. Without one every flag is
		// `declared` and says so — the CLI does not promote a claim to
		// verified just because printing it is easier. Routes that declared
		// nothing print no line (the endpointSuffix reasoning again).
		if r.WireCapabilities != nil && len(*r.WireCapabilities) > 0 {
			states := make([]string, 0, len(*r.WireCapabilities))
			if len(r.CapabilityStates) > 0 {
				for _, st := range r.CapabilityStates {
					s := st.Capability + "(" + st.State
					if st.State != string(model.StateDeclared) && st.ProbedAt != "" {
						s += ", probed " + st.ProbedAt + " against " + st.ProbedModel
					}
					states = append(states, s+")")
				}
			} else {
				for _, c := range *r.WireCapabilities {
					states = append(states, c+"(declared)")
				}
			}
			fmt.Fprintf(w, "%-16s   wire flags: %s\n", "", strings.Join(states, " "))
		}
	}
	return nil
}

func cmdRoutesSet(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("routes set", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("id", "", "the route id — what `--route` names (required)")
	provider := fs.String("provider", "", "the provider's adapter name (required)")
	modelName := fs.String("model", "", "the provider's model name (required)")
	baseURL := fs.String("base-url", "", "override the provider's base URL")
	maxTokens := fs.Int("max-tokens", 0, "output cap; 0 takes the default")
	contextWindow := fs.Int("context-window", 0, "the model input budget in tokens; 0 keeps the saved value or leaves it unknown")
	keyFile := fs.String("api-key-file", "",
		"a FILE holding the credential. There is no --api-key: a value in argv is in the "+
			"process list and in shell history")
	clearKey := fs.Bool("clear-key", false,
		"remove the stored credential. Different from omitting --api-key-file, which KEEPS it")
	inputPrice := fs.Float64("input-price", -1, "USD per MILLION input tokens")
	outputPrice := fs.Float64("output-price", -1, "USD per MILLION output tokens")
	effort := fs.String("reasoning-effort", "",
		"how hard a reasoning model thinks: none|minimal|low|medium|high|xhigh|max. **`none` is what lets a "+
			"step use tools** — OpenAI's reasoning models refuse function tools otherwise, and every "+
			"Aeon step offers tools. Omitting this KEEPS the current value")
	endpoint := fs.String("openai-endpoint", "",
		"which OpenAI API this route talks to: chat|responses. **`responses` is the one that lets a "+
			"step reason WHILE using tools**; the `chat` API refuses that combination. Omitting this "+
			"KEEPS the current value; the default for a new route is chat")
	clearEffort := fs.Bool("clear-reasoning-effort", false,
		"unset it, leaving the provider's own default. Different from omitting the flag, which KEEPS it")
	capabilities := fs.String("capabilities", "",
		"comma list of wire-behaviour flags: tools,streaming-usage,thinking,system-role,parallel-tools. "+
			"These describe how the SERVER behaves on the wire — separate from the model catalogue's "+
			"capabilities. Omitting this KEEPS the saved declaration")
	noPrice := fs.Bool("no-price", false,
		"declare the route UNMETERED on purpose (O8). A new stored route with no price and no --no-price "+
			"is refused, because a silently missing price is how a typo makes a metered model report "+
			"`unknown` forever")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// `--help` is a question, not a failure. The flag package has
			// already printed the list to `w`; returning the error as well
			// would exit 1 with "flag: help requested", which reads as a broken
			// verb to somebody looking up a flag for the first time.
			//
			// The other verbs still do that. This one is fixed because it is
			// where somebody goes to find `--reasoning-effort`.
			return nil
		}
		return err
	}

	if *id == "" || *provider == "" || *modelName == "" {
		return fmt.Errorf("`routes set` needs --id, --provider and --model")
	}

	params := protocol.SetRouteParams{
		ID:                   *id,
		Provider:             *provider,
		Model:                *modelName,
		BaseURL:              *baseURL,
		MaxOutputTokens:      *maxTokens,
		ContextWindow:        *contextWindow,
		ClearKey:             *clearKey,
		ReasoningEffort:      strings.TrimSpace(*effort),
		ClearReasoningEffort: *clearEffort,
		OpenAIEndpoint:       strings.TrimSpace(*endpoint),
	}
	if *clearEffort && params.ReasoningEffort != "" {
		return fmt.Errorf("--reasoning-effort and --clear-reasoning-effort say opposite things; " +
			"pass one")
	}

	if *keyFile != "" {
		raw, err := os.ReadFile(*keyFile)
		if err != nil {
			return fmt.Errorf("read the credential from %s: %w", *keyFile, err)
		}
		params.APIKey = strings.TrimSpace(string(raw))
		if params.APIKey == "" {
			// Refused rather than treated as "keep the existing key": the
			// person pointed at a file, so they meant to set something.
			return fmt.Errorf("%s is empty; omit --api-key-file to keep the existing credential, "+
				"or pass --clear-key to remove it", *keyFile)
		}
	}

	if *inputPrice >= 0 || *outputPrice >= 0 {
		params.Prices = &protocol.PricesView{}
		if *inputPrice >= 0 {
			params.Prices.InputUSDPerMTok = &[]float64{*inputPrice}[0]
		}
		if *outputPrice >= 0 {
			params.Prices.OutputUSDPerMTok = &[]float64{*outputPrice}[0]
		}
	}
	// --no-price beside a price is two decisions in one breath. Refused here,
	// at the flag, rather than resolved by precedence: "unset" and "do not
	// change" already taught this CLI that opposite intentions cannot share
	// one value, and --no-price --input-price 3 is the same mistake wearing
	// different clothes.
	if *noPrice && params.Prices != nil {
		return fmt.Errorf("--no-price and --input-price/--output-price say opposite things " +
			"(unmetered vs metered); pass one")
	}
	// Only pass noPrice when the flag was EXPLICITLY given: the parameter is
	// *bool so that "omitted" keeps the saved declaration instead of quietly
	// flipping a declared-unmetered route back to metered.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "no-price" {
			v := *noPrice
			params.NoPrice = &v
		}
	})
	if strings.TrimSpace(*capabilities) != "" {
		var caps []string
		for _, c := range strings.Split(*capabilities, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				caps = append(caps, c)
			}
		}
		params.WireCapabilities = &caps
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.SetRouteResult
	if err := c.Call(protocol.MethodRoutesSet, params, &res); err != nil {
		// A `-31108` says the route works and was not saved, which is a
		// different thing from a refused route — and `renderError` prints all
		// three parts either way.
		return err
	}

	key := "no key"
	if res.Route.HasKey {
		key = "key …" + res.Route.KeyHint
	}
	fmt.Fprintf(w, "%s  %s/%s  %s  %s  cap %d%s%s\n",
		res.Route.ID, res.Route.Provider, res.Route.Model, string(res.Route.Source), key,
		res.Route.MaxOutputTokens, effortSuffix(res.Route.ReasoningEffort),
		endpointSuffix(res.Route.OpenAIEndpoint))
	if res.Route.Source == protocol.RouteSourceStored {
		fmt.Fprint(w, "saved — it will be there after a restart\n")
	} else {
		fmt.Fprint(w, "NOT saved — this daemon has no store, so this is gone on restart\n")
	}
	return nil
}

func cmdRoutesDelete(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("routes delete", flag.ContinueOnError)
	fs.SetOutput(w)
	id := fs.String("id", "", "the route id (required)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("`routes delete` needs --id")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.DeleteRouteResult
	if err := c.Call(protocol.MethodRoutesDelete, protocol.DeleteRouteParams{ID: *id}, &res); err != nil {
		return err
	}
	if !res.Deleted {
		fmt.Fprintf(w, "no route called %s\n", *id)
		return nil
	}
	// Said explicitly: the credential goes with the route, because a key left
	// behind for a route nobody can call is a secret on disk with no owner.
	fmt.Fprintf(w, "%s deleted, credential included\n", *id)
	return nil
}

// effortSuffix renders a route's reasoning effort, and says nothing when it is
// unset.
//
// Nothing, rather than "default": the field's absence means the provider
// decides, and printing a word for it would suggest Aeon had chosen one.
func effortSuffix(effort string) string {
	if effort == "" {
		return ""
	}
	return "  reasoning " + effort
}

// endpointSuffix renders a route's OpenAI endpoint, and says nothing for the
// default.
//
// Nothing for `chat`, for the same reason `effortSuffix` says nothing when
// unset: a listing that labels every route with its default teaches nobody
// anything, and the one that is NOT the default is the fact worth seeing.
func endpointSuffix(endpoint string) string {
	if endpoint == "" || endpoint == model.OpenAIEndpointChat {
		return ""
	}
	return "  via the " + endpoint + " API"
}

// cmdRoutesProbe is a THIN client of the socket's routes.probe (P34): it
// dials, it calls, it prints. There is no probe logic here — the one
// implementation lives in internal/model/probe.go, which the handler calls,
// so the operator with a terminal and the app's Probe button see the same
// function's answer.
func cmdRoutesProbe(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("routes probe", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("`routes probe` takes exactly one route id: aeon routes probe ID")
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.ProbeRouteResult
	if err := c.Call(protocol.MethodRoutesProbe, protocol.ProbeRouteParams{Route: fs.Arg(0)}, &res); err != nil {
		return err
	}

	if res.Failure != "" {
		// The third answer (P31): "could not check" — spoken plainly, and
		// with the one sentence that keeps it from being misread as a
		// verdict. Nothing was contradicted; nothing was verified.
		fmt.Fprintf(w, "route %q: the probe could not check: %s\n", res.Route, res.Failure)
		fmt.Fprint(w, "nothing was contradicted — every capability keeps its declaration and its `declared` state\n")
		return nil
	}
	if len(res.Probes) == 0 {
		fmt.Fprintf(w, "route %q: nothing to probe\n", res.Route)
		if res.Note != "" {
			fmt.Fprintf(w, "  %s\n", res.Note)
		}
		return nil
	}
	for _, p := range res.Probes {
		fmt.Fprintf(w, "  %-16s %-14s %s\n", p.Capability, p.State, p.Detail)
		if p.State == model.StateVerified || p.State == model.StateContradicted {
			// O2: WHEN and AGAINST WHICH MODEL, always printed for an
			// earned verdict. A three-week-old `verified` against a model
			// that has since been replaced has to be NOTICEABLE.
			fmt.Fprintf(w, "  %16s   probed %s against %s\n", "", p.ProbedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), p.Model)
		}
	}
	if res.Saved {
		fmt.Fprint(w, "verdicts saved — they survive a restart (routes list shows them)\n")
	} else {
		fmt.Fprint(w, "NOT SAVED — these verdicts die with this answer\n")
	}
	if res.Note != "" {
		fmt.Fprintf(w, "  %s\n", res.Note)
	}
	return nil
}
