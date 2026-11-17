package main

// graph.go — `aeon graph build|reparse|delete|status|roots|explain|path|query`
// (S30 T5, P34).
//
// Every verb is a CLIENT of the socket, never a second door (the index.go
// shape, followed exactly): each Calls the identical method the app calls,
// so there is one resolver for each question in the process. The CLI opens
// no store here — the daemon owns the rows, the writes and the async pass;
// a store-opening CLI would be the second implementation the slice's whole
// argument forbids, and it could not honestly say "index building" anyway.
// The no-daemon case is stated, never emulated: dial refuses and says so.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdGraph(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(`graph needs a verb: build, reparse, delete, status, roots, explain, path, query — try "aeon graph help"`)
	}
	switch args[0] {
	case "help":
		fmt.Fprint(w, graphUsage)
		return nil
	case "build":
		return graphBuild(w, args[1:])
	case "reparse":
		return graphReparse(w, args[1:])
	case "delete":
		return graphDelete(w, args[1:])
	case "status":
		return graphStatus(w, args[1:])
	case "roots":
		return graphRoots(w, args[1:])
	case "explain":
		return graphExplain(w, args[1:])
	case "path":
		return graphPath(w, args[1:])
	case "query":
		return graphQuery(w, args[1:])
	default:
		return fmt.Errorf("graph verb %q unknown — the verbs are build, reparse, delete, status, roots, explain, path, query", args[0])
	}
}

const graphUsage = `aeon graph — the code graph, asked of the daemon (S30)

  aeon graph build    --root DIR               index the whole tree now (deliberate)
  aeon graph reparse  --root DIR --path FILE   re-index ONE file — needs --yes (S54)
  aeon graph delete   --root DIR               drop a root's graph rows — needs --yes (S54)
  aeon graph status   --root DIR               built? files? nodes? edges?
  aeon graph roots                             which roots have graphs
  aeon graph explain  --root DIR --symbol S    one symbol's edges, both directions
  aeon graph path     --root DIR --from A --to B
                                               the shortest chain (bounded)
  aeon graph query    --root DIR [--kind K] [--prefix P] [--file F] [--limit N]
                                               browse nodes

Structure, never contents: results name symbols and file:line. Four-state
answers: found / ambiguous (all candidates shown) / not-found / not-built.
The write signal keeps a built root current on its own; build is for the
first fill and the deliberate rest. reparse and delete replace or remove
DERIVED rows through the audited human doors (S54 SP3): each prints its
statement first and acts only on an explicit --yes — without the flag
nothing is written.`

// graphDial is the dial every graph verb shares; a var so the in-process
// parity tests (S54 T5) can swap in a handshake client over an in-memory
// pipe — the platform seatbelt tests drive these verbs through the REAL
// handlers (the mqDial shape, same package, same reason).
var graphDial = dial

func graphDialAndCall(w io.Writer, method protocol.MethodName, params, out any) error {
	c, err := graphDial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(method, params, out)
}

func graphBuild(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph build", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required — there is no ambient root)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("graph build needs --root — graph rows hang off exactly one root (T5 §5), and guessing the cwd is the ambient decision I5 refuses")
	}
	// S64: the verb's UX is unchanged (it blocks and prints the report)
	// but the pass is now a ledger worker — the app can watch the SAME
	// run through asyncworkers.list the moment this call returns, and a
	// Ctrl-C here stops the FOLLOWING, never the build (the daemon owns
	// it; `aeon graph build` again re-attaches to the same flight).
	var acc protocol.GraphBuildAccepted
	if err := graphDialAndCall(w, protocol.MethodGraphBuild, protocol.GraphBuildParams{ComponentRoot: *root}, &acc); err != nil {
		return err
	}
	if !acc.Started {
		fmt.Fprintf(w, "a build for %s was already flying — following worker %s\n", *root, acc.WorkerID)
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
		return fmt.Errorf("graph build %s ended %s: %s", view.WorkerID, view.State, reason)
	}
	var rep graph.BuildReport
	if len(view.Result) > 0 {
		if err := json.Unmarshal(view.Result, &rep); err != nil {
			return fmt.Errorf("graph build %s finished but its report does not decode: %w", view.WorkerID, err)
		}
	}
	fmt.Fprintf(w, "graph built at %s: %d files (%d skipped), %d nodes, %d edges, %d references unresolved\n",
		orRoot(rep.Root, *root), rep.Files, rep.FilesSkipped, rep.Nodes, rep.Edges, rep.UnresolvedEdges)
	return nil
}

// followWorker watches one ledger row to a terminal state by polling the
// socket's own asyncworkers.get once a second. Polling rather than a
// subscription keeps the CLI on the plain Call path every other verb
// uses; the app, which can subscribe, gets the events. The bound is the
// worker timeout plus a margin: past it the honest answer is "still
// running, daemon says", never a silent hang.
func followWorker(w io.Writer, workerID string) (*asyncworkers.View, error) {
	deadline := time.Now().Add(35 * time.Minute)
	var lastNote string
	for {
		var v asyncworkers.View
		if err := graphDialAndCall(io.Discard, protocol.MethodAsyncWorkersGet, protocol.AsyncWorkersGetParams{WorkerID: workerID}, &v); err != nil {
			return nil, err
		}
		if v.State != "queued" && v.State != "running" {
			return &v, nil
		}
		if v.ProgressNote != "" && v.ProgressNote != lastNote {
			lastNote = v.ProgressNote
			fmt.Fprintf(w, "  %s\n", v.ProgressNote)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("graph build worker %s is still %s after 35 minutes — it outlived the follow budget; check `aeon graph status` or asyncworkers.get", workerID, v.State)
		}
		time.Sleep(time.Second)
	}
}

func orRoot(a, fallback string) string {
	if a != "" {
		return a
	}
	return fallback
}

func graphReparse(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph reparse", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	path := fs.String("path", "", "one file, project-relative (required)")
	yes := fs.Bool("yes", false, "confirm after reading the statement — the reparse replaces derived rows and is audited; without it this verb writes NOTHING (S54 SP7)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" || strings.TrimSpace(*path) == "" {
		return fmt.Errorf("graph reparse needs --root and --path — it reparses ONE named file, never a guess")
	}
	// The SP3 statement before the move, then the gate: reparse REPLACES
	// this file's derived graph rows through the audited human door — one
	// platform_change row will name the person who confirmed it. Without
	// --yes the statement is all the verb writes: nothing.
	fmt.Fprintf(w, "graph reparse %s at %s: the file's graph rows are REPLACED by a fresh parse — derived rows only, source files untouched; the run is audited to you under a platform_change row.\n", *path, *root)
	if !*yes {
		return platformYesRefusal(fmt.Sprintf("graph reparse %s at %s", *path, *root))
	}
	var rep graph.BuildReport
	if err := graphDialAndCall(w, protocol.MethodGraphReparse, protocol.GraphReparseParams{ComponentRoot: *root, Path: *path}, &rep); err != nil {
		return err
	}
	fmt.Fprintf(w, "reparse %s at %s: %d rows touched, %d unresolved\n", *path, rep.Root, rep.Nodes+rep.Edges, rep.UnresolvedEdges)
	return nil
}

func graphDelete(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph delete", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	yes := fs.Bool("yes", false, "confirm after reading the statement — the delete removes every derived row of this root's graph and is audited; without it this verb writes NOTHING (S54 SP7)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("graph delete needs --root — a delete that guessed its scope would delete the wrong project's index")
	}
	// The same gate, the louder statement: this one removes EVERY node and
	// edge of the root (the deletion law's shape — derived rows, fully
	// regenerable by a later build, sources untouched).
	fmt.Fprintf(w, "graph delete %s: removes this root's ENTIRE graph — every node and every edge, derived rows only; a later `aeon graph build` regenerates them, and no source file is touched. The run is audited to you under a platform_change row.\n", *root)
	if !*yes {
		return platformYesRefusal(fmt.Sprintf("graph delete %s", *root))
	}
	var out struct {
		Root         string `json:"root"`
		NodesDeleted int    `json:"nodesDeleted"`
	}
	if err := graphDialAndCall(w, protocol.MethodGraphDelete, protocol.GraphDeleteParams{ComponentRoot: *root}, &out); err != nil {
		return err
	}
	fmt.Fprintf(w, "graph deleted at %s: %d nodes removed (edges ride the cascade)\n", out.Root, out.NodesDeleted)
	return nil
}

func graphStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph status", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("graph status needs --root — the fourth state (not-built) is per root, so the question is per root")
	}
	var st graph.Status
	if err := graphDialAndCall(w, protocol.MethodGraphStatus, protocol.GraphStatusParams{ComponentRoot: *root}, &st); err != nil {
		return err
	}
	if !st.Built {
		fmt.Fprintf(w, "graph at %s: NOT BUILT — no rows for this root (aeon graph build starts one)\n", st.Root)
		return nil
	}
	fmt.Fprintf(w, "graph at %s: built — %d files, %d nodes, %d edges\n", st.Root, st.Files, st.Nodes, st.Edges)
	return nil
}

func graphRoots(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph roots", flag.ContinueOnError)
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var roots []string
	if err := graphDialAndCall(w, protocol.MethodGraphRoots, struct{}{}, &roots); err != nil {
		return err
	}
	if len(roots) == 0 {
		fmt.Fprintln(w, "no project has a graph yet — aeon graph build --root DIR starts one")
		return nil
	}
	for _, r := range roots {
		fmt.Fprintln(w, r)
	}
	return nil
}

func graphExplain(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph explain", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	symbol := fs.String("symbol", "", "the symbol to explain (required)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" || strings.TrimSpace(*symbol) == "" {
		return fmt.Errorf("graph explain needs --root and --symbol")
	}
	var res graph.ExplainResult
	if err := graphDialAndCall(w, protocol.MethodGraphExplain, protocol.GraphExplainParams{ComponentRoot: *root, Symbol: *symbol}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s\n", res.State)
	switch res.State {
	case graph.StateFound:
		n := res.Node
		fmt.Fprintf(w, "  %s  (%s · %s:%d-%d)\n", n.Symbol, n.Kind, n.FilePath, n.StartLine, n.EndLine)
		for _, e := range res.Outgoing {
			fmt.Fprintf(w, "  → %-9s %s  [%s]  (%s)\n", e.Kind, e.To.Symbol, e.Resolution, e.To.FilePath)
		}
		for _, e := range res.Incoming {
			fmt.Fprintf(w, "  ← %-9s %s  [%s]  (%s)\n", e.Kind, e.From.Symbol, e.Resolution, e.From.FilePath)
		}
	case graph.StateAmbiguous:
		for _, c := range res.Candidates {
			fmt.Fprintf(w, "  candidate: %s (%s · %s:%d)\n", c.Symbol, c.Kind, c.FilePath, c.StartLine)
		}
		fmt.Fprintln(w, "  ask again with the qualified name — nothing is picked for you")
	case graph.StateNotBuilt:
		fmt.Fprintln(w, "  this root was never indexed — aeon graph build --root DIR")
	}
	return nil
}

func graphPath(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph path", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	from := fs.String("from", "", "start symbol (required)")
	to := fs.String("to", "", "end symbol (required)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		return fmt.Errorf("graph path needs --root, --from and --to — a chain has two named ends")
	}
	var res graph.PathResult
	if err := graphDialAndCall(w, protocol.MethodGraphPath, protocol.GraphPathParams{ComponentRoot: *root, From: *from, To: *to}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s\n", res.State)
	switch res.State {
	case graph.StateFound:
		fmt.Fprintf(w, "  %s\n", res.Start.Symbol)
		for _, h := range res.Hops {
			fmt.Fprintf(w, "    -%-9s [%s]→ %s (%s:%d)\n", h.Kind, h.Resolution, h.To.Symbol, h.To.FilePath, h.To.StartLine)
		}
	case graph.StateAmbiguous:
		for _, c := range res.Candidates {
			fmt.Fprintf(w, "  candidate: %s (%s · %s:%d)\n", c.Symbol, c.Kind, c.FilePath, c.StartLine)
		}
	case graph.StateNotBuilt:
		fmt.Fprintln(w, "  this root was never indexed — aeon graph build --root DIR")
	}
	return nil
}

func graphQuery(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("graph query", flag.ContinueOnError)
	root := fs.String("root", "", "project root (required)")
	kind := fs.String("kind", "", "filter by node kind")
	prefix := fs.String("prefix", "", "filter by symbol prefix")
	file := fs.String("file", "", "filter by project-relative file")
	limit := fs.Int("limit", 0, "maximum rows (default 100)")
	addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("graph query needs --root")
	}
	var res graph.QueryResult
	if err := graphDialAndCall(w, protocol.MethodGraphQuery, protocol.GraphQueryParams{
		ComponentRoot: *root, Kind: *kind, Prefix: *prefix, File: *file, Limit: *limit,
	}, &res); err != nil {
		return err
	}
	fmt.Fprintf(w, "%s\n", res.State)
	for _, n := range res.Nodes {
		fmt.Fprintf(w, "  %-10s %-40s %s:%d-%d\n", n.Kind, n.Symbol, n.FilePath, n.StartLine, n.EndLine)
	}
	return nil
}
