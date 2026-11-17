package daemon

// records_platform_test.go — S54 T4 (SP4, T0's visibility ruling): the
// index_status arm of the records service. What each test pins, in order:
//
//   - the arm's answer EQUALS the person's panel for the step's root —
//     the two doors are built over one PlatformDeps value, so the figures,
//     the state words and the flying question can only agree, and the test
//     compares them field for field (rule 4 at the platform's door);
//   - the scope is the STEP: another component's corpora, figures and
//     roots are simply not in the answer, and no input field can widen it
//     (the schema is empty; identity is the whole scope, OL3);
//   - the operator-view fields the panel carries — root, embedRoute,
//     embedModel, embedDim — never appear in the model-facing answer
//     (T0's visibility ruling, stated at the tool family's line);
//   - an unwired subsystem reads `unwired` in words, never an omitted key
//     (the V13 absence spoken, not hidden);
//   - unwired SERVICE (no seam injected) refuses by name (V13), and a
//     person stepping in here is pointed at platform.status by name (T15);
//   - the read WRITES nothing — the platform_change ledger especially.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/search"
)

// platformTwoRoots seeds one status seam and one graph that answer BOTH
// fixture roots while the store's corpus rows — the panel's walk — exist
// only for the step's root. The org-group figures are deliberately
// distinctive (999 files, a 7-digit node count) so any leak into the
// step's answer is unmistakable.
func platformTwoRoots(t *testing.T, e recordsEnv) PlatformDeps {
	t.Helper()
	// ONLY the step's root carries corpus rows, so the panel's walk covers
	// exactly the scope the tool must answer for — block state and step
	// state are then the same question, and parity is checkable word for
	// word. The other root still lives on both seams (its figures answer
	// when asked, its graph rows ride graph.roots) so a leak is visible.
	for _, c := range [][2]string{
		{"/repo/alpha", "code"}, {"/repo/alpha", "notes"},
	} {
		if _, err := e.s.EnsureRetrievalCorpus(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	status := func(_ context.Context, root string) (search.StatusAnswer, error) {
		if root == "/repo/alpha" {
			return search.StatusAnswer{
				Root: "/repo/alpha", EmbedRoute: "openai-embed", EmbedModel: "emb-small", EmbedDim: 1536,
				Corpora: []search.CorpusStatus{
					{Corpus: "code", Files: 300, StaleFiles: 12, PendingEmbed: 40, Available: true},
					{Corpus: "notes", Files: 12, Available: true},
				},
			}, nil
		}
		return search.StatusAnswer{
			Root: "/repo/org-group", EmbedRoute: "openai-embed", EmbedModel: "emb-small", EmbedDim: 1536,
			Corpora: []search.CorpusStatus{{Corpus: "code", Files: 999, PendingEmbed: 7, Available: true}},
		}, nil
	}
	g := &fakeGraph{roots: []string{"/repo/alpha", "/repo/org-group"}, status: map[string]*graph.Status{
		"/repo/alpha":     {Root: "/repo/alpha", Built: true, Files: 31, Nodes: 900, Edges: 2400},
		"/repo/org-group": {Root: "/repo/org-group", Built: true, Files: 777, Nodes: 1234567, Edges: 8901},
	}}
	return PlatformDeps{Store: e.s, Status: status, Graph: g}
}

func platformFreshnessDo(t *testing.T, e recordsEnv, input string) (*IndexFreshnessView, string) {
	t.Helper()
	out, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, "index_status", json.RawMessage(input))
	if f != nil || err != nil {
		t.Fatalf("index_status refused: f=%v err=%v", f, err)
	}
	view, ok := out.(*IndexFreshnessView)
	if !ok {
		t.Fatalf("index_status answered %T, want *IndexFreshnessView", out)
	}
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	return view, string(b)
}

func TestIndexStatusEqualsThePanelForTheStepsRoot(t *testing.T) {
	e := newRecordsEnv(t, "")
	deps := platformTwoRoots(t, e)
	panel := NewPlatformStatus(deps)
	if panel == nil {
		t.Fatal("wired deps built no panel door")
	}
	e.svc.platformIndex = NewPlatformIndexFreshness(deps)
	view, raw := platformFreshnessDo(t, e, `{}`)

	v, err := panel(context.Background())
	if err != nil {
		t.Fatalf("the panel door refused: %v", err)
	}
	if view.EvaluatedAt == "" || v.EvaluatedAt == "" {
		t.Error("either door answered without the store's stamp")
	}
	// The figures, field for field, against the panel's own component for
	// /repo/alpha — and against the SAME composer's state words (one root
	// seeded in the corpus table, so block and step answer the same
	// question).
	if len(v.Retrieval.Components) != 1 || v.Retrieval.Components[0].Component != "/repo/alpha" {
		t.Fatalf("the panel walked %d components, want the one seeded root", len(v.Retrieval.Components))
	}
	comp := v.Retrieval.Components[0]
	if view.Retrieval.State != v.Retrieval.State {
		t.Errorf("state words disagree: tool %q, panel %q (the same composer must not split)", view.Retrieval.State, v.Retrieval.State)
	}
	if len(view.Retrieval.Corpora) != len(comp.Corpora) {
		t.Fatalf("the tool listed %d corpora, the panel's step-root component %d", len(view.Retrieval.Corpora), len(comp.Corpora))
	}
	for i, c := range view.Retrieval.Corpora {
		p := comp.Corpora[i]
		if c.Name != p.Name || c.Files != p.Files || c.StaleChunks != p.StaleChunks || c.Reindexing != p.Reindexing {
			t.Errorf("corpus %d drifted from the panel: tool %+v panel %+v", i, c, p)
		}
	}
	if view.Graph.State != v.Graph.State {
		t.Errorf("graph state words disagree: tool %q panel %q", view.Graph.State, v.Graph.State)
	}
	var pg graphFigures
	for _, r := range v.Graph.Roots {
		if r.Root == "/repo/alpha" {
			pg = graphFigures{!r.NeverBuilt, r.Files, r.Nodes, r.Edges}
		}
	}
	tg := graphFigures{view.Graph.Built, view.Graph.Files, view.Graph.Nodes, view.Graph.Edges}
	if tg != pg {
		t.Errorf("graph figures drifted: tool %+v panel %+v", tg, pg)
	}
	// The other root is not in the answer at all — its figures first (no
	// bare path leaks either).
	for _, leak := range []string{"999", "777", "1234567", "org-group", "/repo/org-group"} {
		if strings.Contains(raw, leak) {
			t.Errorf("the other component leaked into the step's answer (%q): %s", leak, raw)
		}
	}
	// The operator-view fields, absent by absence (T0's ruling).
	for _, leak := range []string{"embedRoute", "embedModel", "embedDim", "openai-embed", "emb-small", "\"root\"", "\"component\""} {
		if strings.Contains(raw, leak) {
			t.Errorf("an operator-view field reached the model-facing answer (%q): %s", leak, raw)
		}
	}
}

type graphFigures struct {
	built               bool
	files, nodes, edges int
}

func TestIndexStatusBuildingIsTheLedgersWord(t *testing.T) {
	e := newRecordsEnv(t, "")
	deps := platformTwoRoots(t, e)
	mgr := panelManager(t, e.s)
	if _, _, err := mgr.Start("retrieval.reindex", "/repo/alpha", "s54t4-flying", nil); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	deps.Workers = mgr
	e.svc.platformIndex = NewPlatformIndexFreshness(deps)
	view, raw := platformFreshnessDo(t, e, `{}`)
	if view.Retrieval.State != "building" {
		t.Errorf("with a flying reindex the step's block answered %q, want building: %s", view.Retrieval.State, raw)
	}
	// The flying figure is the LEDGER's fact on the step's root; the other
	// root's workers are not the step's news — and its corpora are absent
	// to ask about at all (checked above; the count says it again).
	if len(view.Retrieval.Corpora) != 2 {
		t.Errorf("want the step's two corpora, got %d", len(view.Retrieval.Corpora))
	}
}

func TestIndexStatusScopeIsNotAParameter(t *testing.T) {
	e := newRecordsEnv(t, "")
	deps := platformTwoRoots(t, e)
	e.svc.platformIndex = NewPlatformIndexFreshness(deps)
	// The empty schema is not a suggestion: the shared decoder refuses any
	// field at all, so there is no input shape that even pretends to ask
	// about another root. The refusal names the field; nothing is answered.
	_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, "index_status",
		json.RawMessage(`{"root":"/repo/org-group","componentRoot":"/repo/org-group","corpus":"code"}`))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "root") {
		t.Errorf("a field-named rescope attempt was answered instead of refused: %v", f)
	}
	// And the honest call stays scoped to the step whatever the attempts
	// above left behind.
	view, raw := platformFreshnessDo(t, e, `{}`)
	for _, leak := range []string{"999", "777", "org-group"} {
		if strings.Contains(raw, leak) {
			t.Errorf("an input field widened the scope to the other root (%q): %s", leak, raw)
		}
	}
	if len(view.Retrieval.Corpora) != 2 || view.Retrieval.Corpora[0].Files != 300 {
		t.Errorf("the step's own figures went missing: %s", raw)
	}
}

func TestIndexStatusShowsUnwiredNeverOmits(t *testing.T) {
	e := newRecordsEnv(t, "")
	deps := platformTwoRoots(t, e)
	deps.Status = nil // a no-retrieval build: the graph is there, the index is not
	e.svc.platformIndex = NewPlatformIndexFreshness(deps)
	view, raw := platformFreshnessDo(t, e, `{}`)
	if view.Retrieval == nil || view.Retrieval.State != "unwired" {
		t.Errorf("the absent subsystem did not speak its absence: %s", raw)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["retrieval"]; !ok {
		t.Errorf("the unwired block was OMITTED, which reads as nothing-to-see: %s", raw)
	}
	if view.Graph.State != "available" {
		t.Errorf("the wired block answered %q beside the unwired one, want available", view.Graph.State)
	}
}

func TestIndexStatusUnwiredServiceRefusesByName(t *testing.T) {
	e := newRecordsEnv(t, "")
	// Freshness OF NOTHING is no door: the constructor itself goes nil
	// when neither a status seam nor a graph service exists…
	if NewPlatformIndexFreshness(PlatformDeps{Store: e.s}) != nil {
		t.Error("a storeless of both subsystems still built a freshness door")
	}
	// …and a service mounted without the seam refuses BY NAME (V13),
	// never answering a wall of unwired blocks forever.
	_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, "index_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "index_status") || !strings.Contains(f.Sentence, "not wired") {
		t.Errorf("the bare mount answered %v instead of refusing by name", f)
	}
}

func TestIndexStatusPersonIsPointedToThePanel(t *testing.T) {
	e := newRecordsEnv(t, "")
	deps := platformTwoRoots(t, e)
	e.svc.platformIndex = NewPlatformIndexFreshness(deps)
	_, f, err := e.svc.Do(context.Background(), Actor{Kind: "user", ID: "cj"}, "index_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "platform.status") {
		t.Errorf("a person at the step's door was not pointed at platform.status: %v", f)
	}
}

func TestIndexStatusWritesNothing(t *testing.T) {
	e := newRecordsEnv(t, "")
	deps := platformTwoRoots(t, e)
	e.svc.platformIndex = NewPlatformIndexFreshness(deps)
	before := map[string]int64{}
	for _, tbl := range []string{"platform_change", "retrieval_corpus", "loop", "step"} {
		before[tbl] = platformRowCount(t, e, tbl)
	}
	platformFreshnessDo(t, e, `{}`)
	platformFreshnessDo(t, e, `{}`)
	for _, tbl := range []string{"platform_change", "retrieval_corpus", "loop", "step"} {
		if got := platformRowCount(t, e, tbl); got != before[tbl] {
			t.Errorf("%s grew from %d to %d across two freshness reads — a read does not write", tbl, before[tbl], got)
		}
	}
}

func platformRowCount(t *testing.T, e recordsEnv, table string) int64 {
	t.Helper()
	var n int64
	if err := e.s.GormDB().Raw("SELECT COUNT(*) FROM " + table).Row().Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
