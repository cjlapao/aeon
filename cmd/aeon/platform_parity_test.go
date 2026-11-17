package main

// platform_parity_test.go — S54 T5's parity and gate tests.
//
// "Parity" here is NOT a two-door byte-comparison: the platform plane has
// ONE door (SP7's recorded shape — a composed panel or a live-priced plan
// rebuilt client-side would be a second implementation, so there is no
// offline arm to compare against; the mergequeue family's EV18 precedent).
// The two directions are therefore:
//
//	(1) METHOD ↔ VERB census both ways — the platform family reads exactly
//	    the three SP6 methods, no verb touches an operation door, and every
//	    verb dispatches a registered method;
//	(2) CLI ↔ WIRE bytes — `--json` prints the daemon's answer verbatim,
//	    byte-identical to the raw socket bytes over the same pipe, while the
//	    text renderer adds framing but invents no fact (states are printed
//	    as the daemon's words, including `unwired`).
//
// The seatbelts (SP7/RM9 shape): `index rebuild`, `graph delete`, `graph
// reparse` print the daemon's plan or the fixed derived-row statement and
// write NOTHING without --yes — proven on call counts at the seams, and
// the positive half proves the audited door stamps the PERSON (ActorFor →
// actor kind "user", the peer's id): the CLI itself sends no actor, which
// is what makes the ledger's word true. The fallback rule: the no-daemon
// case is the plain dial error, and the source scan proves there is no
// store-opening road to fall back onto at all.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/platformview"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
)

// --- the fakes -------------------------------------------------------------------

// platformFake answers the three SP6 reads, the gated reindex, and the two
// audited graph operations, recording every call. The call counts are the
// load-bearing assertion: a gate that fired without --yes, or a verb that
// sent an actor as a param, fails on a number, not on vibes.
type platformFake struct {
	status  *platformview.StatusView
	plan    *protocol.RetrievalPlanAnswer
	changes []protocol.PlatformChangeView

	statusCalls int
	planCalls   int
	planReqs    [][2]string // {root, corpus}
	changeReqs  [][2]string // {subject, limit}

	reindexCalls int
	graphCalls   []string      // method tails in order
	graphActors  []graph.Actor // what the DOOR stamped per call
	deleteOut    int
}

func (f *platformFake) statusView() *platformview.StatusView {
	f.statusCalls++
	if f.status != nil {
		return f.status
	}
	return platformFixtureView()
}

func (f *platformFake) planFor(root, corpus string) *protocol.RetrievalPlanAnswer {
	f.planCalls++
	f.planReqs = append(f.planReqs, [2]string{root, corpus})
	if f.plan != nil {
		return f.plan
	}
	return platformFixturePlan()
}

// graphFake implements protocol.Graph; the plain reads refuse (no test here
// needs them) and the audited twins record the ACTOR THE DOOR STAMPED from
// Table.ActorFor — never a param.
type graphFake struct {
	platform *platformFake
}

func (g *graphFake) Explain(context.Context, string, string) (*graph.ExplainResult, error) {
	return nil, errors.New("not wired for this test")
}
func (g *graphFake) Path(context.Context, string, string, string) (*graph.PathResult, error) {
	return nil, errors.New("not wired for this test")
}
func (g *graphFake) Query(context.Context, string, graph.QueryFilter) (*graph.QueryResult, error) {
	return nil, errors.New("not wired for this test")
}
func (g *graphFake) Build(context.Context, string) (*graph.BuildReport, error) {
	return nil, errors.New("not wired for this test")
}
func (g *graphFake) RebuildFileAt(context.Context, string, string) (*graph.BuildReport, error) {
	return nil, errors.New("the daemon's graph verbs reach the audited twin only (S54 T1)")
}
func (g *graphFake) Delete(context.Context, string) (int, error) {
	return 0, errors.New("the daemon's graph verbs reach the audited twin only (S54 T1)")
}
func (g *graphFake) Status(context.Context, string) (*graph.Status, error) {
	return nil, errors.New("not wired for this test")
}
func (g *graphFake) Roots(context.Context) ([]string, error) {
	return nil, errors.New("not wired for this test")
}

func (g *graphFake) RebuildFileAtAudited(_ context.Context, root, absPath string, a graph.Actor) (*graph.BuildReport, error) {
	g.platform.graphCalls = append(g.platform.graphCalls, "graph.reparse")
	g.platform.graphActors = append(g.platform.graphActors, a)
	return &graph.BuildReport{Root: root, Nodes: 4, Edges: 9}, nil
}

func (g *graphFake) DeleteAudited(_ context.Context, root string, a graph.Actor) (int, error) {
	g.platform.graphCalls = append(g.platform.graphCalls, "graph.delete")
	g.platform.graphActors = append(g.platform.graphActors, a)
	return g.platform.deleteOut, nil
}

// --- the fixtures ------------------------------------------------------------------

func platformFixtureView() *platformview.StatusView {
	cost := 1.2523
	return &platformview.StatusView{
		EvaluatedAt: "2026-11-04T12:00:00Z",
		Retrieval:   &platformview.RetrievalBlock{State: platformview.StateUnwired},
		Graph: &platformview.GraphBlock{
			State: platformview.StateAvailable,
			Roots: []platformview.GraphRootView{
				{Root: "/repo/alpha", Files: 31, Nodes: 900, Edges: 2400},
				{Root: "/repo/beta", NeverBuilt: true},
			},
		},
		Usage: &platformview.UsageBlock{
			State: platformview.StateAvailable,
			Workspace: &platformview.UsageFigures{
				LoopsRun: 7, InputTokensTotal: 123450, OutputTokensTotal: 6789,
				CostUSDTotal: cost, CostState: "partial", Recorded: true,
			},
		},
	}
}

func platformFixturePlan() *protocol.RetrievalPlanAnswer {
	usd := 0.42
	return &protocol.RetrievalPlanAnswer{
		ComponentRoot: "/repo/alpha", Corpus: "code",
		Files: 31, StaleFiles: 3, StaleChunks: 40,
		EstimatedInputTokens: 120000, EstimateBasis: "chars/4 over the stale files",
		Cost:       &protocol.PlanCost{USD: &usd, State: "known"},
		EmbedModel: "emb-small", EmbedDim: 1536,
		WillReplaceEmbeddings: true,
	}
}

// standPlatformSocket stands the socket half: a fresh table per dial (the
// mq shape — production's own per-connection shape) with the fakes wired
// and EVERY S54-family door registered the daemon would register, ActorFor
// standing in for the connection's person. The dial vars the verbs share
// (platformDial, graphDial) are swapped and restored at cleanup; the index
// verb dials through the shared dial, so its tests drive the gate up to —
// but not into — the reindex door, whose refusal from a workerless table
// still proves the verb dispatched it.
func standPlatformSocket(t *testing.T, fake *platformFake) {
	t.Helper()
	mkTable := func() *protocol.Table {
		tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
		tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
			return protocol.Actor{Kind: "user", ID: "1000"}
		}
		tbl.PlatformStatus = func(context.Context) (*platformview.StatusView, error) {
			return fake.statusView(), nil
		}
		tbl.RetrievalPlan = func(_ context.Context, root, corpus string) (*protocol.RetrievalPlanAnswer, error) {
			return fake.planFor(root, corpus), nil
		}
		tbl.PlatformChanges = func(_ context.Context, subject string, limit int) ([]protocol.PlatformChangeView, error) {
			fake.changeReqs = append(fake.changeReqs, [2]string{subject, fmt.Sprint(limit)})
			return fake.changes, nil
		}
		tbl.Graph = &graphFake{platform: fake}
		tbl.RetrievalReindex = func(context.Context, string, string, string, bool, func(int, int)) (search.ReindexOutcome, error) {
			fake.reindexCalls++
			return search.ReindexOutcome{}, nil
		}
		if !tbl.RegisterPlatformStatus() || !tbl.RegisterRetrievalPlan() || !tbl.RegisterPlatformChanges() {
			t.Fatal("an S54 read refused to register over a wired seam")
		}
		if !tbl.RegisterGraph() {
			t.Fatal("RegisterGraph refused to register over a wired seam")
		}
		if !tbl.RegisterRetrievalReindex() {
			t.Fatal("RegisterRetrievalReindex refused to register over wired seams")
		}
		return tbl
	}
	connect := func(int) (*protocol.Client, error) {
		conn := parityPipeServer(t, mkTable())
		c := protocol.NewClient(conn, ClientName, Version)
		if _, err := c.Connect(0); err != nil {
			_ = c.Close()
			return nil, err
		}
		return c, nil
	}
	restorePlatform, restoreGraph, restoreIndex := platformDial, graphDial, indexDial
	t.Cleanup(func() { platformDial, graphDial, indexDial = restorePlatform, restoreGraph, restoreIndex })
	platformDial = connect
	graphDial = connect
	indexDial = connect
}

// --- (1) the census, both directions -------------------------------------------------

// verbFiles are the T5 surface; the census walks their ASTs.
var verbFiles = []string{"platform.go", "platform_client.go", "index.go", "graph.go", "main.go"}

// methodRefs returns the protocol.Method* consts each named file references.
func methodRefs(t *testing.T, file string) map[string]bool {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	refs := map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "protocol" && strings.HasPrefix(sel.Sel.Name, "Method") {
			refs["protocol."+sel.Sel.Name] = true
		}
		return true
	})
	return refs
}

func TestPlatformReadsAreExactlyTheThreeMethods(t *testing.T) {
	// Direction one: the platform family's own two files reference EXACTLY
	// the three SP6 reads — no operation door, no twin by accident. A verb
	// that reached graph.delete from platform.go would be SP4's
	// "no control tool" rule violated on the human's own surface.
	refs := map[string]bool{}
	for k := range methodRefs(t, "platform.go") {
		refs[k] = true
	}
	for k := range methodRefs(t, "platform_client.go") {
		refs[k] = true
	}
	want := map[string]bool{
		"protocol.MethodPlatformStatus":       true,
		"protocol.MethodRetrievalReindexPlan": true,
		"protocol.MethodPlatformChanges":      true,
	}
	if len(refs) != len(want) {
		t.Fatalf("platform*.go reference %d protocol.Method consts, want %d: %v", len(refs), len(want), keysOf(refs))
	}
	for k := range want {
		if !refs[k] {
			t.Errorf("%s is not referenced by the platform verbs", k)
		}
	}
	for k := range refs {
		if strings.HasPrefix(k, "protocol.MethodGraph") || k == "protocol.MethodRetrievalReindex" || k == "protocol.MethodRetrievalStatus" {
			t.Errorf("platform*.go reference an operation door (%s) — the platform family is reads only (SP4/SP6)", k)
		}
	}
}

func TestEveryPlatformVerbDispatchesARegisteredMethod(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	cases := [][]string{
		{"status"},
		{"plan", "code", "--component", "/repo/alpha"},
		{"changes"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := cmdPlatform(&buf, tc); err != nil {
			t.Fatalf("platform %v: %v", tc, err)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- the dispatch and the usage line (S53's F-1 lesson, carried) ----------------------

func TestPlatformVerbIsDispatchedByTheBinary(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		`case "platform":`,
		"err = cmdPlatform(os.Stdout, args)",
		"platform          status",
		"only on --yes (S54 SP7)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not contain %q — the verb must be reachable in the real binary and named in the top usage (S53's F-1)", want)
		}
	}
}

// --- (2) CLI ↔ wire bytes, both directions ------------------------------------------------

func TestPlatformStatusJSONIsTheWireVerbatim(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	if err := cmdPlatform(&buf, []string{"status", "--json"}); err != nil {
		t.Fatalf("platform status --json: %v", err)
	}
	// The same pipe, the raw socket bytes: what --json printed must be
	// what the daemon answered, byte for byte, with framing added
	// nowhere.
	c, err := platformDial(0)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	var raw json.RawMessage
	if err := c.Call(protocol.MethodPlatformStatus, struct{}{}, &raw); err != nil {
		t.Fatalf("raw call: %v", err)
	}
	if got := strings.TrimRight(buf.String(), "\n"); got != string(raw) {
		t.Errorf("--json is not the wire verbatim:\n cli: %s\n raw: %s", got, string(raw))
	}
}

func TestPlatformStatusStatesAreTheDaemonsWords(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	if err := cmdPlatform(&buf, []string{"status"}); err != nil {
		t.Fatalf("platform status: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"evaluated 2026-11-04T12:00:00Z",
		"retrieval  unwired", // unwired AS a state, never an empty block
		"graph      available",
		"/repo/alpha  31 files, 900 nodes, 2400 edges",
		"/repo/beta  never built",
		"usage      available",
		"7 loops run",
		"(partial)", // the cost state rides as the store's word
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status render missing %q in:\n%s", want, out)
		}
	}
	// The negative half of "never recompute": the renderer's numbers are
	// exactly the fixture's — an invented second figure would show as a
	// number the fixture never carried.
	if strings.Count(out, "900") != 1 || strings.Count(out, "2400") != 1 {
		t.Errorf("a figure is printed twice — the renderer should present, not re-derive:\n%s", out)
	}
}

func TestPlatformPlanRendersTheThreeCostStates(t *testing.T) {
	usd := 0.10
	states := []struct {
		cost protocol.PlanCost
		want string
	}{
		{protocol.PlanCost{USD: &usd, State: "known"}, "known: about $0.1000"},
		{protocol.PlanCost{USD: &usd, State: "partial", Reason: "no price for half the route"}, "partial: about $0.1000 — no price for half the route"},
		{protocol.PlanCost{State: "unknown", Reason: "no route configured"}, "unknown: no route configured"},
	}
	for _, tc := range states {
		cost := tc.cost
		fake := &platformFake{plan: &protocol.RetrievalPlanAnswer{
			ComponentRoot: "/repo/alpha", Corpus: "code", Files: 2, EstimateBasis: "b",
			Cost: &cost, WillReplaceEmbeddings: true,
		}}
		standPlatformSocket(t, fake)
		var buf bytes.Buffer
		if err := cmdPlatform(&buf, []string{"plan", "code", "--component", "/repo/alpha"}); err != nil {
			t.Fatalf("plan --%s: %v", tc.cost.State, err)
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("cost state %q did not render as %q:\n%s", tc.cost.State, tc.want, buf.String())
		}
		if !strings.Contains(buf.String(), "REPLACES the corpus's derived vectors") {
			t.Errorf("the replacement statement is missing for state %q:\n%s", tc.cost.State, buf.String())
		}
	}
}

func TestPlatformChangesRendersTheLedger(t *testing.T) {
	fake := &platformFake{changes: []protocol.PlatformChangeView{{
		ID: 12, Op: "graph.delete", Subject: "/repo/alpha", OutcomeRef: "worker-9",
		Summary: "deleted the graph at /repo/alpha", ActorKind: "user", ActorID: "1000",
		OccurredAt: "2026-11-04T12:00:00Z",
	}}}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	if err := cmdPlatform(&buf, []string{"changes", "--subject", "/repo", "--limit", "5"}); err != nil {
		t.Fatalf("platform changes: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"#12", "graph.delete", "user:1000", "/repo/alpha → worker-9", "deleted the graph"} {
		if !strings.Contains(out, want) {
			t.Errorf("changes render missing %q in:\n%s", want, out)
		}
	}
	if fake.changeReqs[0] != [2]string{"/repo", "5"} {
		t.Errorf("the verb sent %+v, want the daemon's own params {/repo,5} — filters ride, never recompute", fake.changeReqs[0])
	}
}

// --- the seatbelts ---------------------------------------------------------------------

func TestIndexRebuildPrintsThePlanFirstAndWritesNothingWithoutYes(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	err := cmdIndex(&buf, []string{"rebuild", "--root", "/repo/alpha", "--corpus", "code"})
	if err == nil {
		t.Fatal("rebuild without --yes returned nil — the gate must refuse")
	}
	var ue *usageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("the refusal is not the gate's usageError naming --yes: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "reindex plan — /repo/alpha corpus \"code\"") {
		t.Fatalf("the plan did not print before the refusal (SP7: plan first):\n%s", out)
	}
	if !strings.Contains(out, "known: about $0.4200") {
		t.Errorf("the daemon's price did not ride into the refusal:\n%s", out)
	}
	if !strings.Contains(err.Error(), "NOTHING was written") {
		t.Errorf("the refusal must say the gate wrote nothing: %v", err)
	}
	// The counts: the plan was asked exactly once (it is a read); the
	// reindex door was NEVER dispatched.
	if fake.planCalls != 1 || fake.planReqs[0] != [2]string{"/repo/alpha", "code"} {
		t.Errorf("plan calls %d reqs %v — want exactly one for the named root and corpus", fake.planCalls, fake.planReqs)
	}
	if fake.reindexCalls != 0 {
		t.Errorf("the reindex seam ran %d times without --yes — the gate wrote something", fake.reindexCalls)
	}
}

func TestIndexRebuildYesDispatchesTheDoor(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	err := cmdIndex(&buf, []string{"rebuild", "--root", "/repo/alpha", "--corpus", "code", "--yes"})
	// The pipe table has no async-worker manager, so the DOOR refuses for
	// a reason PAST the gate — -31104 ErrStoreUnavailable is the handler
	// itself speaking ("needs the async-worker manager…"), which is the
	// proof the verb crossed the seatbelt and dispatched the operation.
	// (The seam never runs without a manager; the refusal rides the
	// client rendering of the code, not the handler's What.)
	if err == nil || !strings.Contains(err.Error(), "-31104") {
		t.Fatalf("with --yes the verb must reach the reindex door and hear the HANDLER refuse; got: %v", err)
	}
	if fake.planCalls != 1 {
		t.Errorf("the plan must print on the confirmed path too, got %d calls", fake.planCalls)
	}
	if !strings.Contains(buf.String(), "reindex plan") {
		t.Errorf("confirmed rebuild skipped the plan print:\n%s", buf.String())
	}
}

func TestIndexRebuildEmbedRouteNoteRefusesToPriceTheOverride(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	err := cmdIndex(&buf, []string{"rebuild", "--root", "/repo/alpha", "--corpus", "code", "--embed-route", "other-route"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected the gate refusal, got %v", err)
	}
	if !strings.Contains(buf.String(), "--embed-route other-route overrides it and is NOT priced") {
		t.Errorf("the override note is missing — the daemon prices only its configured route:\n%s", buf.String())
	}
	if fake.reindexCalls != 0 {
		t.Errorf("the money door ran without --yes")
	}
}

func TestIndexBuildRefusesAYesItDoesNotNeed(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	err := cmdIndex(&buf, []string{"build", "--root", "/repo/alpha", "--corpus", "code", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "no --yes") {
		t.Fatalf("index build must refuse a --yes it has no plan to gate: %v", err)
	}
}

func TestGraphDeleteGatesOnYesAndStampsThePerson(t *testing.T) {
	fake := &platformFake{deleteOut: 42}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	err := cmdGraph(&buf, []string{"delete", "--root", "/repo/alpha"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("delete without --yes must refuse: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"removes this root's ENTIRE graph", "derived rows only", "audited"} {
		if !strings.Contains(out, want) {
			t.Errorf("the delete statement missing %q in:\n%s", want, out)
		}
	}
	if len(fake.graphCalls) != 0 {
		t.Fatalf("graph.delete ran without --yes — the statement must be all the verb writes")
	}

	buf.Reset()
	if err := cmdGraph(&buf, []string{"delete", "--root", "/repo/alpha", "--yes"}); err != nil {
		t.Fatalf("delete --yes: %v", err)
	}
	if len(fake.graphCalls) != 1 || fake.graphCalls[0] != "graph.delete" {
		t.Fatalf("with --yes the audited twin must run exactly once, got %v", fake.graphCalls)
	}
	// The deviation the ledger tells true: the actor is NOT {cli,cli} —
	// a socket CLI call stamps the PERSON behind the endpoint, and the
	// verb itself sent no actor field (the door derived it).
	a := fake.graphActors[0]
	if a.Kind != "user" || a.ID != "1000" {
		t.Errorf("the audited actor is %+v, want the peer person {user 1000} — not a CLI claim", a)
	}
	if !strings.Contains(buf.String(), "42 nodes removed") {
		t.Errorf("the delete report did not ride through:\n%s", buf.String())
	}
}

func TestGraphReparseGatesOnYesToo(t *testing.T) {
	fake := &platformFake{}
	standPlatformSocket(t, fake)
	var buf bytes.Buffer
	err := cmdGraph(&buf, []string{"reparse", "--root", "/repo/alpha", "--path", "a.go"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("reparse without --yes must refuse: %v", err)
	}
	if !strings.Contains(buf.String(), "the file's graph rows are REPLACED") {
		t.Errorf("the reparse statement did not print first:\n%s", buf.String())
	}
	if len(fake.graphCalls) != 0 {
		t.Fatalf("graph.reparse ran without --yes")
	}
	if err := cmdGraph(&buf, []string{"reparse", "--root", "/repo/alpha", "--path", "a.go", "--yes"}); err != nil {
		t.Fatalf("reparse --yes: %v", err)
	}
	if len(fake.graphCalls) != 1 || fake.graphActors[0].Kind != "user" {
		t.Errorf("with --yes the audited twin must run once stamped to the person, got %v %v", fake.graphCalls, fake.graphActors)
	}
}

// --- the fallback rule -------------------------------------------------------------------

func TestPlatformVerbsCarryNoOfflineRoad(t *testing.T) {
	// The P5 fallback exists only where a daemon-less answer function
	// exists. Here none does — and the scan proves the CLI cannot open a
	// store, reach a direct-door Answer* helper, or import the daemon,
	// so there is no road to fall back ONTO. (S60's mergequeue carried
	// the same recorded deviation from the spec's fallback clause.)
	banned := []string{"internal/store", "internal/daemon", "protocol.Answer", "store.Open", "GormDB"}
	for _, file := range []string{"platform.go", "platform_client.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s references %q — the platform verbs have ONE door", file, b)
			}
		}
	}
	// The dial error is the stated no-daemon case, verbatim.
	wanted := errors.New("dial refused: no daemon home")
	restore := platformDial
	t.Cleanup(func() { platformDial = restore })
	platformDial = func(int) (*protocol.Client, error) { return nil, wanted }
	for _, args := range [][]string{{"status"}, {"plan", "code", "--component", "/r"}, {"changes"}} {
		var buf bytes.Buffer
		err := cmdPlatform(&buf, args)
		if !errors.Is(err, wanted) {
			t.Errorf("platform %v with no daemon: %v — want the plain dial error stated, never emulated", args, err)
		}
		if buf.Len() != 0 {
			t.Errorf("platform %v printed something with no daemon:\n%s", args, buf.String())
		}
	}
}
