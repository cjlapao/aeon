package daemon

// platform_reads_test.go — S54 T2: the composed panel answers with the
// figures its sources answered (differential, real store, fake seams), an
// unwired block is exactly a nil seam (including the typed-nil check
// newTelemetryReadStore is famous for), a refusing subsystem rides its own
// sentence verbatim while the door itself answers, and "building" is the
// async ledger's flying row — never a second tracker and never a guess.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/store"
)

// --- fakes ---------------------------------------------------------------------

// fakeGraph is a protocol.Graph whose roots/status answers are fixed and
// whose audited twins must never be reached by a READ.
type fakeGraph struct {
	roots   []string
	status  map[string]*graph.Status
	statusE error
	rootsE  error
	audited int
}

func (f *fakeGraph) Explain(context.Context, string, string) (*graph.ExplainResult, error) {
	return nil, nil
}
func (f *fakeGraph) Path(context.Context, string, string, string) (*graph.PathResult, error) {
	return nil, nil
}
func (f *fakeGraph) Query(context.Context, string, graph.QueryFilter) (*graph.QueryResult, error) {
	return nil, nil
}
func (f *fakeGraph) Build(context.Context, string) (*graph.BuildReport, error) { return nil, nil }
func (f *fakeGraph) RebuildFileAt(context.Context, string, string) (*graph.BuildReport, error) {
	return nil, nil
}
func (f *fakeGraph) Delete(context.Context, string) (int, error) { return 0, nil }
func (f *fakeGraph) Status(_ context.Context, root string) (*graph.Status, error) {
	if f.statusE != nil {
		return nil, f.statusE
	}
	st, ok := f.status[root]
	if !ok {
		return nil, errors.New("no such root")
	}
	return st, nil
}
func (f *fakeGraph) Roots(context.Context) ([]string, error) { return f.roots, f.rootsE }
func (f *fakeGraph) RebuildFileAtAudited(context.Context, string, string, graph.Actor) (*graph.BuildReport, error) {
	f.audited++
	return nil, errors.New("a status panel never auditions an operation")
}
func (f *fakeGraph) DeleteAudited(context.Context, string, graph.Actor) (int, error) {
	f.audited++
	return 0, errors.New("a status panel never auditions an operation")
}

// panelManager registers the two operation types the panel asks the
// ledger about — Start refuses an unregistered type, and refusing here
// would mean the test's own ledger seed was a fiction.
func panelManager(t *testing.T, st *store.Store) *asyncworkers.Manager {
	t.Helper()
	mgr := asyncworkers.New(st, asyncworkers.Options{})
	// The runner BLOCKS: a worker that finishes the instant it starts has
	// left the flying window before the panel can ask, and the test would
	// measure its own scheduling instead of the ledger.
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	for _, wt := range []string{"retrieval.reindex", "graph.build"} {
		if err := mgr.Register(wt, func(ctx context.Context, _ asyncworkers.Job, _ asyncworkers.Reporter) (json.RawMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, asyncworkers.WorkerOpts{}); err != nil {
			t.Fatalf("register %s: %v", wt, err)
		}
	}
	return mgr
}

type fakeTelemetry struct {
	row protocol.WorkspaceTelemetry
	err error
}

func (f *fakeTelemetry) ComponentTelemetry(string) (protocol.ComponentTelemetry, error) {
	return protocol.ComponentTelemetry{}, nil
}
func (f *fakeTelemetry) ProjectTelemetry(string) (protocol.ProjectTelemetry, error) {
	return protocol.ProjectTelemetry{}, nil
}
func (f *fakeTelemetry) WorkspaceTelemetry() (protocol.WorkspaceTelemetry, error) {
	return f.row, f.err
}
func (f *fakeTelemetry) LoopTelemetry(int64) (protocol.TelemetryLoopData, error) {
	return protocol.TelemetryLoopData{}, nil
}

// --- changes -------------------------------------------------------------------

func TestPlatformChangesSeamEqualsTheLedger(t *testing.T) {
	st := openStoreForGraph(t)
	// The lawful append is the store's own: audit inside a transaction,
	// occurred_at the caller's stamp — two rows, deliberately ordered so
	// "newest first" cannot pass by insertion accident.
	appendRow := func(a store.PlatformAudit, at string) {
		if err := st.PlatformWithTx(func(tx *sql.Tx) error {
			return store.PlatformChangeInsertTx(tx, a, at)
		}); err != nil {
			t.Fatalf("seed %s: %v", a.Op, err)
		}
	}
	appendRow(store.PlatformAudit{Op: "reindex", Subject: "/home/dev/aeon\x00code",
		OutcomeRef: "aw_7f3a9c2e14b8d051", Summary: "retrieval.reindex started for corpus code",
		Actor: store.PlatformActor{Kind: "user", ID: "4242"}}, "2026-11-04T10:00:00Z")
	appendRow(store.PlatformAudit{Op: "graph_build", Subject: "/home/dev/aeon",
		Summary: "graph.build started", Actor: store.PlatformActor{Kind: "cli", ID: "cli"}},
		"2026-11-04T11:00:00Z")

	fn := NewPlatformChanges(st)
	if fn == nil {
		t.Fatal("a daemon WITH a store built no changes seam")
	}
	rows, err := fn(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(rows) != 2 || rows[0].Op != "graph_build" || rows[1].Op != "reindex" {
		t.Fatalf("newest-first broken: %+v", rows)
	}
	r := rows[1]
	if r.Subject != "/home/dev/aeon\x00code" || r.OutcomeRef != "aw_7f3a9c2e14b8d051" ||
		r.Summary != "retrieval.reindex started for corpus code" || r.ActorKind != "user" ||
		r.ActorID != "4242" || r.OccurredAt != "2026-11-04T10:00:00Z" {
		t.Errorf("the mapping drifted from the row: %+v", r)
	}
	if rows[0].ID <= rows[1].ID {
		t.Errorf("ids %d/%d are not the ledger's own order", rows[0].ID, rows[1].ID)
	}
	// The subject filter is exact — the composed NUL key, not a prefix.
	only, err := fn(context.Background(), "/home/dev/aeon", 0)
	if err != nil || len(only) != 1 || only[0].Op != "graph_build" {
		t.Errorf("subject filter answered %d row(s) %v — want the one exact-subject row", len(only), err)
	}
	if NewPlatformChanges(nil) != nil {
		t.Error("a storeless daemon built a changes door — V13 says the ledger door is absent")
	}
}

// --- status ----------------------------------------------------------------------

func TestPlatformStatusEqualsItsSources(t *testing.T) {
	st := openStoreForGraph(t)
	for _, c := range [][2]string{{"/a", "code"}, {"/a", "history"}, {"/b", "code"}} {
		if _, err := st.EnsureRetrievalCorpus(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	mgr := panelManager(t, st)
	if _, _, err := mgr.Start("retrieval.reindex", "/a", "dedupe-a", nil); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	status := func(_ context.Context, root string) (search.StatusAnswer, error) {
		if root == "/a" {
			return search.StatusAnswer{
				Root: "/a", EmbedRoute: "openai-embed", EmbedModel: "emb-small", EmbedDim: 1536,
				Corpora: []search.CorpusStatus{
					{Corpus: "code", Files: 1204, StaleFiles: 38, PendingEmbed: 96, Available: true, Reindexing: true},
					{Corpus: "history", Files: 71, Available: true},
				},
			}, nil
		}
		return search.StatusAnswer{Root: "/b", Corpora: []search.CorpusStatus{{Corpus: "code", Files: 5, Available: true}}}, nil
	}
	g := &fakeGraph{roots: []string{"/a", "/b"}, status: map[string]*graph.Status{
		"/a": {Root: "/a", Built: true, Files: 431, Nodes: 18342, Edges: 60719},
		"/b": {Root: "/b"},
	}}
	tel := &fakeTelemetry{row: protocol.WorkspaceTelemetry{
		TelemetrySums: protocol.TelemetrySums{LoopsRun: 12, InputTokensTotal: 481234,
			OutputTokensTotal: 90211, CostUSDTotal: 4.17, CostState: "known"},
		Exists: true}}
	fn := NewPlatformStatus(PlatformDeps{Store: st, Status: status, Graph: g, Telemetry: tel, Workers: mgr})
	if fn == nil {
		t.Fatal("wired deps built no panel door")
	}
	v, err := fn(context.Background())
	if err != nil {
		t.Fatalf("the panel door refused: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, v.EvaluatedAt); err != nil {
		t.Errorf("evaluatedAt %q is not the store's isoUTC stamp: %v", v.EvaluatedAt, err)
	}
	// Retrieval: two components, figures byte-equal to the seam's answers,
	// and "building" because the LEDGER has a flying reindex for /a (the
	// seam's own Reindexing flag agrees — one source, two voices of it).
	if len(v.Retrieval.Components) != 2 {
		t.Fatalf("components = %d, want the store's two roots", len(v.Retrieval.Components))
	}
	a := v.Retrieval.Components[0]
	if a.Component != "/a" || a.EmbedModel != "emb-small" || a.EmbedDim != 1536 ||
		len(a.Corpora) != 2 || a.Corpora[0].Files != 1204 || a.Corpora[0].StaleChunks != 96 {
		t.Errorf("the panel re-worded the status seam: %+v", a)
	}
	if v.Retrieval.State != "building" {
		t.Errorf("flying ledger row + reindexing flag answered %q", v.Retrieval.State)
	}
	// Graph: /a built with figures, /b listed-never-built → stale… but no
	// worker flies for graph.build, so the neverBuilt rule alone decides:
	// stale, and /b carries the flag.
	if v.Graph.State != "stale" || !v.Graph.Roots[1].NeverBuilt || v.Graph.Roots[0].Files != 431 {
		t.Errorf("graph block = %+v", v.Graph)
	}
	if g.audited != 0 {
		t.Error("a status read touched an AUDITED graph twin")
	}
	// Usage: the singleton row's figures and state word, unmoved.
	u := v.Usage.Workspace
	if v.Usage.State != "available" || u.LoopsRun != 12 || u.InputTokensTotal != 481234 ||
		u.CostUSDTotal != 4.17 || u.CostState != "known" || !u.Recorded {
		t.Errorf("usage block = %+v", v.Usage)
	}
}

func TestUnwiredBlocksAndAbsentDoor(t *testing.T) {
	st := openStoreForGraph(t)
	if NewPlatformStatus(PlatformDeps{}) != nil {
		t.Error("no store and no seam built a door — V13: absent")
	}
	fn := NewPlatformStatus(PlatformDeps{Store: st})
	v, err := fn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Retrieval.State != "unwired" || v.Graph.State != "unwired" || v.Usage.State != "unwired" {
		t.Errorf("a storeless-of-subsystems daemon answered %q/%q/%q", v.Retrieval.State, v.Graph.State, v.Usage.State)
	}
	// The typed-nil check: newTelemetryReadStore(nil) must not reach the
	// panel as a non-nil interface over a nil store, or usage would answer
	// error where the truth is unwired.
	if tel := newTelemetryReadStore(nil); tel != nil {
		t.Errorf("newTelemetryReadStore(nil) is not a nil interface: %v", tel)
	}
	v2, err := NewPlatformStatus(PlatformDeps{Store: st, Telemetry: newTelemetryReadStore(nil)})(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v2.Usage.State != "unwired" {
		t.Errorf("the typed nil store answered usage %q — unwired is the honest word", v2.Usage.State)
	}
}

func TestErrorBlocksQuoteTheirSourcesAndTheDoorStillAnswers(t *testing.T) {
	st := openStoreForGraph(t)
	if _, err := st.EnsureRetrievalCorpus("/a", "code"); err != nil {
		t.Fatal(err)
	}
	sentence := "read status for /a: walk refused: open /x: permission denied"
	fn := NewPlatformStatus(PlatformDeps{
		Store: st,
		Status: func(context.Context, string) (search.StatusAnswer, error) {
			return search.StatusAnswer{}, errors.New(sentence)
		},
		Graph:     &fakeGraph{rootsE: errors.New("graph roots read refused: database is locked")},
		Telemetry: &fakeTelemetry{err: errors.New("telemetry row unreadable")},
	})
	v, err := fn(context.Background())
	if err != nil {
		t.Fatalf("a half-broken platform is DATA, not a door failure: %v", err)
	}
	if v.Retrieval.State != "error" || v.Retrieval.Error != sentence {
		t.Errorf("retrieval block = %q / %q, want error + verbatim", v.Retrieval.State, v.Retrieval.Error)
	}
	if v.Graph.State != "error" || !strings.Contains(v.Graph.Error, "database is locked") {
		t.Errorf("graph block = %+v", v.Graph)
	}
	if v.Usage.State != "error" || v.Usage.Error != "telemetry row unreadable" {
		t.Errorf("usage block = %+v", v.Usage)
	}
}

func TestFlyingIsNilSafeAndLedgerTruth(t *testing.T) {
	if flying(nil, "retrieval.reindex", "/a") {
		t.Error("a nil manager says flying")
	}
	st := openStoreForGraph(t)
	mgr := panelManager(t, st)
	if flying(mgr, "retrieval.reindex", "") {
		t.Error("an empty root says flying")
	}
	if flying(mgr, "retrieval.reindex", "/never") {
		t.Error("a root with no ledger row says flying")
	}
	if _, _, err := mgr.Start("graph.build", "/b", "dedupe-b", nil); err != nil {
		t.Fatal(err)
	}
	if !flying(mgr, "graph.build", "/b") {
		t.Error("a queued worker did not read as flying")
	}
	if flying(mgr, "retrieval.reindex", "/b") {
		t.Error("a graph.build worker made the reindex plane claim flying")
	}
}
