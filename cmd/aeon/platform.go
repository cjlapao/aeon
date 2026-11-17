package main

// platform.go — `aeon platform status|plan|changes` (S54 T5, SP7).
//
// These three verbs are CLIENTS of the socket and nothing else: each
// dispatches the identical method the app calls (`platform.status`,
// `retrieval.reindexPlan`, `platform.changes`), so the panel, the plan and
// the ledger have ONE implementation in the process — the same law
// index.go has lived by since S22. There is NO offline arm here: what these
// verbs print is a COMPOSED daemon read (the panel is the daemon's own
// adapter composing the subsystems it owns; the plan walks the daemon's
// live route registry), and a store-opening CLI that rebuilt either would
// be the second implementation the slice's whole argument forbids — the
// no-daemon case is stated by the dial, never emulated (S60's mergequeue
// family is the named precedent for the pure-socket shape).
//
// Nothing here recomputes anything (SP7): states are the daemon's words,
// costs ride the three states with their reasons, staleness is the daemon's
// count, and the `--json` flag prints the wire answer verbatim — the parity
// test pins that byte for byte against the raw socket.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/platformview"
	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdPlatform(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`platform needs a verb: status, plan, changes — try "aeon platform help"`)
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, platformUsage)
		return nil
	case "status":
		return platformStatusVerb(w, args[1:])
	case "plan":
		return platformPlanVerb(w, args[1:])
	case "changes":
		return platformChangesVerb(w, args[1:])
	default:
		return fmt.Errorf("platform verb %q unknown — the verbs are status, plan, changes", args[0])
	}
}

const platformUsage = `aeon platform — the daemon's own panel for the derived stores (S54)

  aeon platform status [--json]                     retrieval, graph and usage as the daemon
                                                    composes them: each block's state is the
                                                    daemon's word — available, stale, building,
                                                    unwired, error — never a CLI guess
  aeon platform plan CORPUS --component DIR [--json]
                                                    what a reindex pass over ONE named corpus
                                                    would do and would cost (CORPUS is "code"
                                                    or "history" — named, never defaulted)
  aeon platform changes [--subject S] [--limit N] [--json]
                                                    the platform_change ledger: who started which
                                                    derived-store operation, newest first

All three are pure clients of the daemon's socket; there is no offline arm
— a composed panel or a live-priced plan rebuilt client-side would be a
second implementation. Money never moves on these verbs: plan is a
preview, and the operations that spend ride the gated doors (index rebuild,
graph build/reparse/delete).`

// --- status ---------------------------------------------------------------------

func platformStatusVerb(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("platform status", flag.ContinueOnError)
	asJSON := jsonFlag(fs)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := platformDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var view *platformview.StatusView
	if err := c.Call(protocol.MethodPlatformStatus, struct{}{}, &view); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, view)
	}
	return printPlatformStatus(w, view)
}

// printPlatformStatus renders the composed panel. Every state word is the
// daemon's, printed as-is — including `unwired`, which is a STATE and never
// rendered as an empty block (the block is skipped ONLY when the answer
// carries no block at all, which the composed view never produces).
func printPlatformStatus(w io.Writer, view *platformview.StatusView) error {
	if view == nil {
		return fmt.Errorf("platform status answered nothing — the daemon composed no panel")
	}
	fmt.Fprintf(w, "platform status — evaluated %s\n", view.EvaluatedAt)
	printRetrievalBlock(w, view.Retrieval)
	printGraphBlock(w, view.Graph)
	printUsageBlock(w, view.Usage)
	return nil
}

func printRetrievalBlock(w io.Writer, b *platformview.RetrievalBlock) {
	if b == nil {
		fmt.Fprint(w, "retrieval  (the daemon answered no retrieval block)\n")
		return
	}
	fmt.Fprintf(w, "retrieval  %s\n", b.State)
	if b.Error != "" {
		fmt.Fprintf(w, "  the retrieval read refused: %s\n", b.Error)
		return
	}
	if b.Availability != "" {
		fmt.Fprintf(w, "  availability %s\n", b.Availability)
	}
	for _, comp := range b.Components {
		route := "no embedding route — keyword answers only"
		if comp.EmbedRoute != "" {
			route = fmt.Sprintf("embedding route %s (model %s, %d dims)", comp.EmbedRoute, comp.EmbedModel, comp.EmbedDim)
		}
		fmt.Fprintf(w, "  component %s — %s\n", comp.Component, route)
		if len(comp.Corpora) == 0 {
			fmt.Fprint(w, "    no corpora exist for this root yet — index build asks the daemon to start one\n")
			continue
		}
		for _, corpus := range comp.Corpora {
			touch := ""
			if corpus.Reindexing {
				touch = "  (a pass is flying)"
			}
			stale := "current"
			if corpus.StaleChunks > 0 {
				stale = fmt.Sprintf("%d stale chunks", corpus.StaleChunks)
			}
			fmt.Fprintf(w, "    %-8s %5d files   %s%s\n", corpus.Name, corpus.Files, stale, touch)
		}
	}
}

func printGraphBlock(w io.Writer, b *platformview.GraphBlock) {
	if b == nil {
		fmt.Fprint(w, "graph      (the daemon answered no graph block)\n")
		return
	}
	fmt.Fprintf(w, "graph      %s\n", b.State)
	if b.Error != "" {
		fmt.Fprintf(w, "  the graph read refused: %s\n", b.Error)
		return
	}
	if len(b.Roots) == 0 {
		fmt.Fprint(w, "  no roots carry graph rows\n")
		return
	}
	for _, root := range b.Roots {
		if root.NeverBuilt {
			fmt.Fprintf(w, "  %s  never built\n", root.Root)
			continue
		}
		fmt.Fprintf(w, "  %s  %d files, %d nodes, %d edges\n", root.Root, root.Files, root.Nodes, root.Edges)
	}
}

func printUsageBlock(w io.Writer, b *platformview.UsageBlock) {
	if b == nil {
		fmt.Fprint(w, "usage      (the daemon answered no usage block)\n")
		return
	}
	fmt.Fprintf(w, "usage      %s\n", b.State)
	if b.Error != "" {
		fmt.Fprintf(w, "  the usage read refused: %s\n", b.Error)
		return
	}
	u := b.Workspace
	if u == nil {
		return
	}
	if !u.Recorded {
		fmt.Fprint(w, "  nothing recorded yet — this workspace has run no counted loops\n")
		return
	}
	fmt.Fprintf(w, "  %d loops run · %d input / %d output tokens · cost $%.4f (%s)\n",
		u.LoopsRun, u.InputTokensTotal, u.OutputTokensTotal, u.CostUSDTotal, u.CostState)
}

// --- plan -----------------------------------------------------------------------

func platformPlanVerb(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`platform plan names its corpus first: aeon platform plan code --component DIR (there is no default corpus — the money is per corpus)`)
	}
	corpus := args[0]
	fs := flag.NewFlagSet("platform plan", flag.ContinueOnError)
	component := fs.String("component", "", "component root (required — there is no ambient root)")
	asJSON := jsonFlag(fs)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*component) == "" {
		return fmt.Errorf("platform plan needs --component — a plan for a guessed root would price the wrong project (I5)")
	}
	c, err := platformDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var plan *protocol.RetrievalPlanAnswer
	if err := c.Call(protocol.MethodRetrievalReindexPlan,
		protocol.RetrievalPlanParams{ComponentRoot: *component, Corpus: corpus}, &plan); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, plan)
	}
	return printIndexPlan(w, plan)
}

// printIndexPlan renders SP2's preview — the figures plus the cost in its
// THREE states with the daemon's own reason, and the replacement statement.
// The plan is priced by the daemon only for its configured route: a caller
// overriding --embed-route is told the figure does not price that route
// (the CLI never re-prices — SP7).
func printIndexPlan(w io.Writer, plan *protocol.RetrievalPlanAnswer) error {
	if plan == nil {
		return fmt.Errorf("the plan door answered nothing")
	}
	fmt.Fprintf(w, "reindex plan — %s corpus %q\n", plan.ComponentRoot, plan.Corpus)
	fmt.Fprintf(w, "  %d files (%d stale), %d stale chunks\n", plan.Files, plan.StaleFiles, plan.StaleChunks)
	basis := ""
	if plan.EstimateBasis != "" {
		basis = fmt.Sprintf(" (%s)", plan.EstimateBasis)
	}
	fmt.Fprintf(w, "  estimated %d input tokens%s\n", plan.EstimatedInputTokens, basis)
	cost := "unknown — the daemon priced no figure"
	if plan.Cost != nil {
		switch plan.Cost.State {
		case "known":
			usd := 0.0
			if plan.Cost.USD != nil {
				usd = *plan.Cost.USD
			}
			cost = fmt.Sprintf("known: about $%.4f — an estimate, never an invoice", usd)
		case "partial":
			cost = fmt.Sprintf("partial: about $%.4f — %s", derefUSD(plan.Cost.USD), plan.Cost.Reason)
		default:
			cost = fmt.Sprintf("unknown: %s", plan.Cost.Reason)
		}
	}
	fmt.Fprintf(w, "  embedding cost %s\n", cost)
	if plan.EmbedModel != "" {
		fmt.Fprintf(w, "  embedding model %s (%d dims)\n", plan.EmbedModel, plan.EmbedDim)
	}
	if plan.WillReplaceEmbeddings {
		fmt.Fprint(w, "  this pass REPLACES the corpus's derived vectors; source files are never touched\n")
	}
	return nil
}

func derefUSD(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// --- changes ----------------------------------------------------------------------

func platformChangesVerb(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("platform changes", flag.ContinueOnError)
	subject := fs.String("subject", "", "prefix filter on the operation subject (a root, or root\\x00corpus)")
	limit := fs.Int("limit", 0, "newest N rows (the daemon clamps [1,500]; 0 is the daemon's default)")
	asJSON := jsonFlag(fs)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := platformDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var rows []protocol.PlatformChangeView
	if err := c.Call(protocol.MethodPlatformChanges,
		protocol.PlatformChangesParams{Subject: strings.TrimSpace(*subject), Limit: *limit}, &rows); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(w, rows)
	}
	if len(rows) == 0 {
		fmt.Fprint(w, "no platform changes — the ledger has never seen a derived-store operation\n")
		return nil
	}
	for _, r := range rows {
		outcome := ""
		if r.OutcomeRef != "" {
			outcome = fmt.Sprintf(" → %s", r.OutcomeRef)
		}
		fmt.Fprintf(w, "#%d  %s  %s:%s  %s%s  %s\n", r.ID, r.Op, r.ActorKind, r.ActorID, r.Subject, outcome, r.Summary)
		fmt.Fprintf(w, "      at %s\n", r.OccurredAt)
	}
	return nil
}
