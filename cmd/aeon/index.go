package main

// index.go — `aeon index build|rebuild|status` (S22 T8, D15/I10/P34).
//
// These verbs are CLIENTS of the socket, not a second door: each Dispatches
// the identical method the app calls (`retrieval.status`,
// `retrieval.reindex`), so there is one reindex and one availability
// implementation in the process — the call-site lock is
// TestRetrievalStatusAndReindexSocketCallSameFunctions. A CLI that opened
// the store and embedded on its own would be the second implementation the
// slice's whole argument forbids (and the daemon owns the async pass, so a
// store-opening CLI could not report "reindex running" honestly anyway).
// The no-daemon case is stated, never emulated: dial refuses and says where
// to look.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
)

// indexDial is the dial the index verbs share; a var so the in-process
// gate tests (S54 T5) can drive `aeon index rebuild` through the REAL
// handlers over an in-memory pipe (the mqDial shape, same reason).
var indexDial = dial

func cmdIndex(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`index needs a verb: build, rebuild, status — try "aeon index help"`)
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, indexUsage)
		return nil
	case "status":
		return indexStatus(w, args[1:])
	case "build", "rebuild":
		return indexRebuild(w, args[0], args[1:])
	default:
		return fmt.Errorf("index verb %q unknown — the verbs are build, rebuild, status", args[0])
	}
}

const indexUsage = `aeon index — the retrieval index, asked explicitly (S22)

  aeon index status   [--root DIR]                 availability, per-corpus staleness, embedding route
  aeon index build    [--root DIR] [--corpus C]     deliberate first pass (asks the daemon)
  aeon index rebuild  [--root DIR] [--corpus C] [--embed-route ID] [--force]
                                                    [--yes]
                                                     deliberate re-pass; the daemon's COST PLAN
                                                     prints first and --yes is what acts (S54
                                                     SP7) — without it nothing is written;
                                                     --embed-route names a
                                                     different embedding model and wipes-then-builds
                                                     that corpus — money, said so before it moves

The corpora are "code" and "history"; a verb NAMES its corpus (no default).
Nothing here ever fires on its own: the daemon never reindexes on start, on
status, or on a stale query result (V21).`

func indexStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("index status", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required — there is no ambient root)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("index status needs --root — staleness is a property of ONE project root (V12), and guessing the caller's cwd is the ambient decision I5 refuses")
	}
	c, err := indexDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var ans search.StatusAnswer
	if err := c.Call(protocol.MethodRetrievalStatus, protocol.RetrievalStatusParams{ComponentRoot: *root}, &ans); err != nil {
		return err
	}
	fmt.Fprintf(w, "index status — root %s\n", ans.Root)
	if ans.EmbedRoute != "" {
		fmt.Fprintf(w, "embedding route %s (model %s, %d dims)\n", ans.EmbedRoute, ans.EmbedModel, ans.EmbedDim)
	} else {
		fmt.Fprint(w, "embedding route: none configured — vector search is absent; keyword answers carry the keyword-only note (D4)\n")
	}
	if len(ans.Corpora) == 0 {
		fmt.Fprint(w, "no corpora exist for this root yet — aeon index build asks the daemon to start one\n")
		return nil
	}
	for _, cs := range ans.Corpora {
		state := "current"
		switch {
		case !cs.Available && cs.Reason == "index-empty":
			state = "not built yet"
		case cs.Reindexing && cs.Percent > 0:
			state = fmt.Sprintf("reindex running, %d%%", cs.Percent)
		case cs.Reindexing:
			state = "reindex running"
		case cs.StaleFiles > 0 || cs.PendingEmbed > 0:
			state = fmt.Sprintf("stale: %d files", cs.StaleFiles)
			if cs.PendingEmbed > 0 {
				state += fmt.Sprintf(", %d chunks await embedding", cs.PendingEmbed)
			}
		}
		fmt.Fprintf(w, "  %-8s %5d files   %s", cs.Corpus, cs.Files, state)
		if !cs.Available && cs.Reason != "" {
			fmt.Fprintf(w, "   availability: %s", cs.Reason)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func indexRebuild(w io.Writer, verb string, args []string) error {
	fs := flag.NewFlagSet("index "+verb, flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	corpus := fs.String("corpus", "code", `which corpus: "code" or "history" (no default corpus exists)`)
	embedRoute := fs.String("embed-route", "", "embedding route id — naming another model rebuilds the corpus under it (wipes that corpus's index first; this spends money)")
	force := fs.Bool("force", false, "accept a rebuild onto a corpus embedded by a different model (the V17 restatement)")
	yes := fs.Bool("yes", false, "confirm the rebuild after reading the daemon's cost plan — without it this verb writes NOTHING (S54 SP7)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("index %s needs --root — the index is per project root (V12)", verb)
	}
	if *corpus != "code" && *corpus != "history" {
		return fmt.Errorf(`index %s refuses corpus %q — name "code" or "history"; there is no default and the money is per corpus`, verb, *corpus)
	}
	if verb == "build" && *yes {
		return fmt.Errorf("index build takes no --yes — the gated verb is rebuild; a first pass has no plan to confirm (S54 SP7)")
	}
	c, err := indexDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	if verb == "rebuild" {
		// SP7's seatbelt: the daemon's OWN plan prints before the money
		// moves — figures, the three-state cost, the replacement statement
		// — and without --yes that plan is all the verb ever writes. A
		// plan-door refusal stops the rebuild: an unpriced money pass never
		// starts. (V13 keeps this honest: the plan and the reindex ride the
		// same retrieval seam, so wherever a rebuild is possible a plan is
		// answerable.)
		plan, err := platformPlanCall(c, *root, *corpus)
		if err != nil {
			return fmt.Errorf("index rebuild prints the daemon's plan before any money moves, and the plan door refused: %w — nothing was started", err)
		}
		if err := printIndexPlan(w, plan); err != nil {
			return err
		}
		if *embedRoute != "" {
			fmt.Fprintf(w, "  the plan above prices the route the DAEMON has configured; --embed-route %s overrides it and is NOT priced by that figure — the daemon refuses to guess a price for a route it has not been asked about\n", *embedRoute)
		}
		if !*yes {
			return platformYesRefusal(fmt.Sprintf("index rebuild corpus %q at %s", *corpus, *root))
		}
	}
	if *embedRoute != "" {
		// The money warning before the move, stated as the operator's own
		// decision (O3's honesty belongs at the deliberate door too, not
		// only at the refusal that prices for someone else).
		fmt.Fprintf(w, "index %s: corpus %q at %s under route %q — this embeds and may cost real money (V10); a different model WIPES that corpus first.\n", verb, *corpus, *root, *embedRoute)
	}
	p := protocol.RetrievalReindexParams{ComponentRoot: *root, Corpus: *corpus, Force: *force}
	if *embedRoute != "" {
		r := *embedRoute
		p.EmbedRoute = &r
	}
	// S22 async (the graph's "same approach it was added"): the verb's
	// UX is unchanged — it blocks and prints the outcome — but the pass is
	// now a ledger worker. The app can watch the SAME run through
	// retrieval.status or asyncworkers.list the moment this call returns,
	// and a Ctrl-C here stops the FOLLOWING, never the pass (the daemon
	// owns it; `aeon index rebuild` again re-attaches to the same flight,
	// the same flight the app sees). The handle answers in the
	// milliseconds it takes to touch the ledger; we then follow the row
	// to terminal and print the finished ReindexOutcome, exactly as
	// `aeon graph build` does.
	var acc protocol.RetrievalReindexAccepted
	if err := c.Call(protocol.MethodRetrievalReindex, p, &acc); err != nil {
		return err
	}
	if !acc.Started {
		fmt.Fprintf(w, "a reindex for %s %s was already flying — following worker %s\n", *root, *corpus, acc.WorkerID)
	}
	view, err := followWorker(w, acc.WorkerID)
	if err != nil {
		return err
	}
	if view.State != "finished" {
		reason := view.Error
		if reason == "" {
			reason = "no reason recorded"
		}
		return fmt.Errorf("index %s %s ended %s: %s", verb, *corpus, view.State, reason)
	}
	var out search.ReindexOutcome
	if len(view.Result) > 0 {
		if err := json.Unmarshal(view.Result, &out); err != nil {
			return fmt.Errorf("index %s %s finished but its outcome does not decode: %w", verb, *corpus, err)
		}
	}
	cost := "unknown"
	if out.CostKnown && out.EmbeddedUSD != nil {
		cost = fmt.Sprintf("$%.4f", *out.EmbeddedUSD)
	}
	fmt.Fprintf(w, "%s corpus %q at %s: %d files, %d chunks written, %d deleted, %d skips, %d embedded (cost %s)\n",
		verb, out.Corpus, *root, out.Files, out.ChunksWritten, out.Deleted, out.Skips, out.Embedded, cost)
	return nil
}
