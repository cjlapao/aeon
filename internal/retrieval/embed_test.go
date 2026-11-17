//go:build !noretrieval

package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// embed_test.go — V16 made an embedding model a ROUTE, and this file is how
// that claim stays true: the seam speaks model types, the cost comes back as
// S18's three-state Cost, no test touches a network, and O5's "resume, not
// restart" is proven by counting what a retry sends — not by trusting that
// it remembers.

// --- the recorded fixture and the fake seam ----------------------------------

type embedFixture struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage *struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

func loadEmbedFixture(t *testing.T) embedFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "embed-openai-small.json"))
	must(t, err, "read the recorded embedding fixture")
	var f embedFixture
	must(t, json.Unmarshal(raw, &f), "parse the recorded embedding fixture")
	if len(f.Data) == 0 || f.Usage == nil {
		t.Fatal("the recorded fixture lost its shape (data and usage are the two facts the client parses)")
	}
	return f
}

// recordedEmbedder replays the fixture: each text gets one of the recorded
// vectors, chosen deterministically by hashing the text, so the same batch
// always produces the same vectors (a re-embed is comparable, which is what
// lets a geometry test state a number). It is a FAKE and says so — the
// vectors are recorded numbers, not provider math.
type recordedEmbedder struct {
	fixture embedFixture

	// Calls records every batch the pass sent: size and joined bytes. The
	// batching and resume tests read this — "bounded" and "resumes" are
	// claims about the CALLS, so they are measured at the calls.
	Calls []embedCall
	// FailAt fails the Nth call (1-based) with an error, once, like a
	// flaky connection; FailGeometry makes one vector of the Nth call come
	// back with the wrong length, like a corrupt answer.
	FailAt       int
	FailGeometry int

	calls int
}

type embedCall struct {
	Model string
	Texts int
	Bytes int
}

func (f *recordedEmbedder) EmbedBatch(ctx context.Context, route *model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	f.calls++
	n := f.calls
	total := 0
	for _, x := range texts {
		total += len(x)
	}
	f.Calls = append(f.Calls, embedCall{Model: route.Model, Texts: len(texts), Bytes: total})

	if f.FailAt == n {
		return nil, model.Usage{}, model.Cost{State: model.CostUnknown, Reason: model.ReasonStreamAbortedEarly},
			fmt.Errorf("recorded flake: the connection dropped on call %d", n)
	}
	vectors := make([][]float32, len(texts))
	for i, x := range texts {
		sum := sha256.Sum256([]byte(x))
		pick := int(binary.BigEndian.Uint32(sum[:4])) % len(f.fixture.Data)
		v := append([]float32(nil), f.fixture.Data[pick].Embedding...)
		if f.FailGeometry == n && i == 0 {
			v = append(v, 0.5) // one vector arrives with an extra dimension
		}
		vectors[i] = v
	}
	usage := model.Usage{InputTokens: model.IntPtr(f.fixture.Usage.PromptTokens)}
	cost := model.ComputeCost(usage, route.Prices)
	return vectors, usage, cost, nil
}

// embedChunkCount asks the store how many chunks the seed actually made —
// the tests measure the corpus instead of assuming files-times-functions,
// because a chunking change should move the number, not break a test.
func embedChunkCount(t *testing.T, x *Index, root string) int {
	t.Helper()
	row, ok, err := x.s.RetrievalCorpusRow(root, CorpusCode)
	must(t, err, "corpus row for count")
	if !ok {
		t.Fatal("the corpus is gone")
	}
	chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
	must(t, err, "chunk count")
	return len(chunks)
}

// embedSeed writes n distinct tiny Go files and indexes them, yielding a
// corpus with well over one batch of chunks. It returns the index; the
// caller embeds through the fake.
func embedSeed(t *testing.T, x *Index, root string, n int) {
	t.Helper()
	must(t, os.MkdirAll(root, 0o755), "seed root")
	for i := 0; i < n; i++ {
		var b strings.Builder
		b.WriteString("package main\n")
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&b, "func f%d_%d() int {\n\treturn %d\n}\n\n", i, j, i*3+j)
		}
		path := filepath.Join(root, fmt.Sprintf("f%03d.go", i))
		must(t, os.WriteFile(path, []byte(b.String()), 0o644), "seed file")
		plan, err := x.PlanFile(root, CorpusCode, path, []byte(b.String()))
		must(t, err, "plan seed")
		_, _, err = x.ApplyChunkPlan(root, CorpusCode, plan)
		must(t, err, "apply seed")
	}
}

func pricedEmbedRoute() *model.Route {
	in := 0.02
	return &model.Route{ID: "openai-small", Provider: "openai", Model: "text-embedding-3-small", Prices: &model.Prices{InputUSDPerMTok: &in}}
}

func unmeteredEmbedRoute() *model.Route {
	return &model.Route{ID: "local-embed", Provider: "openai", Model: "nomic-embed-text", NoPrice: true}
}

// embedIndex gives each test a REAL temp root: the embedding pass needs
// files on disk to chunk and seed (the stale tests' roots were labels;
// this pass reads bodies, so the root must be a directory that exists).
func embedIndex(t *testing.T) (*Index, func() string) {
	t.Helper()
	s := openStore(t)
	x := New(s)
	root := t.TempDir()
	must(t, x.EnsureCorpus(context.Background(), root, CorpusCode), "ensure corpus")
	return x, func() string { return root }
}

// bannedTokens reads a ban table from testdata — the tokens live OUTSIDE the
// scanned sources, so the hunter cannot contain the hunted even in
// assembled form (a scanner's bytes are its own file's bytes; measured).
func bannedTokens(t *testing.T, file string) []string {
	t.Helper()
	raw := mustRead(t, filepath.Join("testdata", file))
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) < 5 {
		t.Fatalf("ban table %s has %d tokens — a thin gate is no gate", file, len(out))
	}
	return out
}

// --- the task's gates ---------------------------------------------------------

func TestEmbeddingUsesS14Routes(t *testing.T) {
	// By import: the seam speaks model types and imports nothing else that
	// could hold a provider.
	src := mustRead(t, "embed.go")
	for _, want := range []string{`"github.com/cjlapao/aeon/internal/model"`, "model.Route", "model.Usage", "model.Cost", "ComputeCost"} {
		if !strings.Contains(src, want) {
			t.Errorf("embed.go does not speak S18 through %q — V16 says embedding is a route, and routes are model.Route, cost is model.Cost", want)
		}
	}
	if !strings.Contains(src, "*model.Client") {
		t.Error("embed.go has no production seam around *model.Client — the production Embedder must BE the S18 client, not an approximation of it")
	}
	// By shape: the aggregate cost is S18's rollup rule (Add), so one
	// usage-less batch makes the total unknown rather than too low (P32).
	if !strings.Contains(src, ".Add(") {
		t.Error("the pass does not roll costs up with model.Cost.Add — a hand-rolled sum would silently under-report the batch that never answered")
	}
	// And the cost is COMPUTED where S18 computes it: model's own file,
	// the same ComputeCost the chat path uses.
	client := mustRead(t, filepath.Join("..", "model", "embed.go"))
	if !strings.Contains(client, "ComputeCost(usage, route.Prices)") {
		t.Error("model's Embed does not price through ComputeCost at the single site — a second cost formula is exactly the parallel path N9 forbids")
	}
	var _ Embedder = &recordedEmbedder{} // the seam is implementable by a fake, which is what makes the route claim testable
}

func TestNoSecondProviderPath(t *testing.T) {
	// I8/N9 as a grep: this package sends no requests, reads no keys and
	// computes no prices. The convenient shortcut is one small HTTP call
	// here, and it would WORK — while quietly creating a second place that
	// holds a key and computes a cost.
	files, err := os.ReadDir(".")
	must(t, err, "list the package")
	checked, banned := 0, bannedTokens(t, "embed-banned-provider-path.txt")
	// The ban list itself must not be the hunted string in one piece; the
	// scanner below would otherwise trip on its own gate.
	for _, e := range files {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := mustRead(t, name)
		checked++
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s contains %q — the provider conversation belongs to internal/model (I8, N9)", name, b)
			}
		}
	}
	if checked < 6 {
		t.Fatalf("only %d files scanned — the gate needs the whole package, not a sample", checked)
	}
}

func TestUnmeteredEmbeddingIsUnknownWithAReason(t *testing.T) {
	x, rootOf := embedIndex(t)
	root := rootOf()
	embedSeed(t, x, root, 2)

	fake := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	pass, err := x.EmbedPending(context.Background(), root, CorpusCode, unmeteredEmbedRoute(), fake)
	must(t, err, "embed an unmetered route")

	if pass.Cost.State != model.CostUnknown {
		t.Fatalf("an unmetered route cost %v — a local model has no price Aeon knows, and any number would be a claim (M30)", pass.Cost.State)
	}
	if pass.Cost.Reason != model.ReasonNoPriceForRoute {
		t.Fatalf("unknown without its reason: %q — M30 says unknown must NAME why", pass.Cost.Reason)
	}
	// And no $0.00 anywhere: the pass carries no USD to render as free.
	if pass.Cost.USD != 0 || pass.Cost.State == model.CostKnown {
		t.Fatalf("a declared-unmetered pass still reports a priceable total (%+v) — that is the $0.00 claim in disguise", pass.Cost)
	}
	if len(fake.Calls) == 0 || pass.Embedded == 0 {
		t.Fatal("the unmetered path embedded nothing — unknown-cost must not mean no-embedding (a local model indexes for free and reports unknown, it does not refuse)")
	}
}

func TestModelAndDimensionRecordedInTheIndex(t *testing.T) {
	x, rootOf := embedIndex(t)
	root := rootOf()
	embedSeed(t, x, root, 1)

	fake := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	pass, err := x.EmbedPending(context.Background(), root, CorpusCode, pricedEmbedRoute(), fake)
	must(t, err, "embed")

	row, ok, err := x.s.RetrievalCorpusRow(root, CorpusCode)
	must(t, err, "read corpus row")
	if !ok {
		t.Fatal("the corpus vanished")
	}
	if row.EmbedModel != "text-embedding-3-small" || row.EmbedDim != 4 {
		t.Fatalf("the row records model=%q dim=%d — V6 makes both DATA on the row so T8 can compare them (the dimension came from the wire, not from the model name)", row.EmbedModel, row.EmbedDim)
	}
	if pass.Model != row.EmbedModel || pass.Dim != row.EmbedDim {
		t.Errorf("the pass report disagrees with the row (%s/%d vs %s/%d) — two answers to one question", pass.Model, pass.Dim, row.EmbedModel, row.EmbedDim)
	}
	ids, err := x.s.VecChunkIDs(root, CorpusCode, 4)
	must(t, err, "vec ids")
	if len(ids) != pass.Embedded || len(ids) == 0 {
		t.Fatalf("vec rows %d vs embedded %d — the record means nothing if the vectors it certifies are not there", len(ids), pass.Embedded)
	}
}

func TestFailureMidIndexLeavesThePortionStanding(t *testing.T) {
	x, rootOf := embedIndex(t)
	root := rootOf()
	embedSeed(t, x, root, 300) // hundreds of chunks, well past one batch
	total := embedChunkCount(t, x, root)
	if total < 2*maxEmbedBatchChunks {
		t.Fatalf("the seed produced %d chunks — the flake test needs multiple batches to fail one of", total)
	}

	flake := &recordedEmbedder{fixture: loadEmbedFixture(t), FailAt: 2}
	pass, err := x.EmbedPending(context.Background(), root, CorpusCode, pricedEmbedRoute(), flake)
	must(t, err, "a flake must not fail the PASS — it fails chunks (O5)")

	if len(flake.Calls) < 3 {
		t.Fatalf("expected the seed to arrive in at least 3 bounded calls, got %d", len(flake.Calls))
	}
	embedded := total - flake.Calls[1].Texts
	if pass.Embedded != embedded {
		t.Fatalf("%d chunks stand, want %d — the portion BEFORE and AFTER the flake must all be there (the indexed portion stands)", pass.Embedded, embedded)
	}
	if len(pass.Failures) != flake.Calls[1].Texts {
		t.Fatalf("%d failures recorded, want %d — per CHUNK, naming the failure of the whole batch as N separate losses", len(pass.Failures), flake.Calls[1].Texts)
	}
	for _, f := range pass.Failures {
		if f.Source == "" || f.Err == "" || f.LineEnd < f.LineStart {
			t.Fatalf("a failure without its coordinates: %+v — \"the batch failed\" is not a per-chunk record", f)
		}
	}

	// THE RESUME: a retry must embed the missing 96 and NOT the standing
	// 204. This is the whole O5 claim, counted at the calls.
	resume := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	again, err := x.EmbedPending(context.Background(), root, CorpusCode, pricedEmbedRoute(), resume)
	must(t, err, "retry")

	if len(resume.Calls) != 1 || resume.Calls[0].Texts != flake.Calls[1].Texts {
		t.Fatalf("the retry sent %d calls / %v — resuming means re-sending ONLY the chunks that never got vectors, counted here, not remembered there (the rows are the resume state)", len(resume.Calls), resume.Calls)
	}
	if again.Embedded != flake.Calls[1].Texts {
		t.Fatalf("the retry embedded %d, want %d", again.Embedded, flake.Calls[1].Texts)
	}
	if again.Skipped != embedded {
		t.Fatalf("the retry reports %d skipped, want %d — \"resumed\" must be a number, not a mood", again.Skipped, embedded)
	}
	ids, err := x.s.VecChunkIDs(root, CorpusCode, 4)
	must(t, err, "vec ids after resume")
	if len(ids) != total {
		t.Fatalf("the corpus ends with %d vectors for %d chunks", len(ids), total)
	}
}

func TestBatchesAreBounded(t *testing.T) {
	x, rootOf := embedIndex(t)
	root := rootOf()
	embedSeed(t, x, root, 300)
	chunks := embedChunkCount(t, x, root)

	fake := &recordedEmbedder{fixture: loadEmbedFixture(t)}
	_, err := x.EmbedPending(context.Background(), root, CorpusCode, pricedEmbedRoute(), fake)
	must(t, err, "embed in batches")

	if len(fake.Calls) < 2 {
		t.Fatalf("300 chunks in %d call(s) — batching exists precisely so this is never one call", len(fake.Calls))
	}
	total := 0
	for _, c := range fake.Calls {
		if c.Texts > maxEmbedBatchChunks {
			t.Errorf("a call carried %d chunks, past the bound of %d", c.Texts, maxEmbedBatchChunks)
		}
		if c.Bytes > maxEmbedBatchBytes {
			t.Errorf("a call carried %d bytes, past the bound of %d", c.Bytes, maxEmbedBatchBytes)
		}
		total += c.Texts
	}
	if total != chunks {
		t.Fatalf("the batches cover %d of %d chunks", total, chunks)
	}

	// A partial batch failure is PER CHUNK: one corrupt vector among good
	// ones costs that chunk its vector, not the batch its whole call.
	x2, rootOf2 := embedIndex(t)
	root2 := rootOf2()
	embedSeed(t, x2, root2, 4)
	geomChunks := embedChunkCount(t, x2, root2)
	geom := &recordedEmbedder{fixture: loadEmbedFixture(t), FailGeometry: 1}
	pass, err := x2.EmbedPending(context.Background(), root2, CorpusCode, pricedEmbedRoute(), geom)
	must(t, err, "geometry pass")
	// One wrong-shaped vector per CALL (call 1's first vector), and every
	// batch-mate lands: failures == calls, not failures == chunks.
	if len(pass.Failures) != len(geom.Calls) {
		t.Fatalf("%d failures for %d wrong-shaped vectors (one per call) — per chunk means one per bad vector, not one per whole batch", len(pass.Failures), len(geom.Calls))
	}
	if pass.Embedded != geomChunks-len(geom.Calls) {
		t.Fatalf("%d embedded of %d chunks with %d bad vectors — the batch-mates had nothing wrong with them", pass.Embedded, geomChunks, len(geom.Calls))
	}
	if pass.Failures[0].Err == "" || !strings.Contains(pass.Failures[0].Err, "dimension") {
		t.Errorf("the geometry failure does not name itself: %q", pass.Failures[0].Err)
	}
	if pass.Embedded == 0 {
		t.Fatal("a corrupt vector cancelled everything — per-chunk means per-chunk")
	}
}

func TestCostReportsThroughTheThreeStates(t *testing.T) {
	// Known: a priced route whose usage arrived.
	x, rootOf := embedIndex(t)
	root := rootOf()
	embedSeed(t, x, root, 1)
	known, err := x.EmbedPending(context.Background(), root, CorpusCode, pricedEmbedRoute(), &recordedEmbedder{fixture: loadEmbedFixture(t)})
	must(t, err, "known pass")
	if known.Cost.State != model.CostKnown || known.Cost.USD <= 0 {
		t.Fatalf("a metered pass costs %v — indexing a large repository costs real MONEY and must show it (V10)", known.Cost)
	}

	// Unknown: the unmetered route (tested at length above; the STATE's
	// reachability is what belongs to this gate).
	x2, rootOf2 := embedIndex(t)
	root2 := rootOf2()
	embedSeed(t, x2, root2, 1)
	unknown, err := x2.EmbedPending(context.Background(), root2, CorpusCode, unmeteredEmbedRoute(), &recordedEmbedder{fixture: loadEmbedFixture(t)})
	must(t, err, "unknown pass")
	if unknown.Cost.State != model.CostUnknown || unknown.Cost.Reason == "" {
		t.Fatalf("unmetered is %v — unknown WITH a reason (M30)", unknown.Cost)
	}

	// Partial: a route priced on both sides, a server that reports only
	// input usage — S18's floor with Missing naming the gap. The fake
	// reports input-only usage, so pricing output too is what reaches the
	// third state.
	x3, rootOf3 := embedIndex(t)
	root3 := rootOf3()
	embedSeed(t, x3, root3, 1)
	in, out := 0.02, 0.10
	partial := &model.Route{ID: "both-prices", Provider: "openai", Model: "text-embedding-3-small",
		Prices: &model.Prices{InputUSDPerMTok: &in, OutputUSDPerMTok: &out}}
	pass, err := x3.EmbedPending(context.Background(), root3, CorpusCode, partial, &recordedEmbedder{fixture: loadEmbedFixture(t)})
	must(t, err, "partial pass")
	if pass.Cost.State != model.CostPartial || len(pass.Cost.Missing) == 0 {
		t.Fatalf("a floor with no Missing list is not a floor, it is a guess: %+v", pass.Cost)
	}
}

func TestAgainstRecordedFixturesOnly(t *testing.T) {
	// The floor that makes the sentence true: the fake USED the fixture,
	// and nothing in this package (tests included) can reach a network.
	f := loadEmbedFixture(t)
	if len(f.Data) < 3 || f.Usage.PromptTokens <= 0 {
		t.Fatal("the recorded fixture is too thin to run the suite on")
	}
	files, err := os.ReadDir(".")
	must(t, err, "list the package")
	hits := 0
	for _, e := range files {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		src := mustRead(t, e.Name())
		for _, b := range bannedTokens(t, "embed-banned-network.txt") {
			if strings.Contains(src, b) {
				t.Errorf("%s contains %q — no live embedding provider in any test (and no localhost either: a local server's base URL belongs in a ROUTE the user configures, never in a test)", e.Name(), b)
			}
		}
		hits++
	}
	if hits < 9 {
		t.Fatalf("only %d files scanned", hits)
	}
}

func TestQ5Recorded(t *testing.T) {
	// Q5: does S18's route type accommodate an embedding model, or does it
	// assume chat? ANSWER (asserted, not noted): it accommodates one —
	// "embedding" joined the model-purpose vocabulary, and a route may
	// declare it.
	found := false
	for _, c := range model.Capabilities() {
		if c == "embedding" {
			found = true
		}
	}
	if !found {
		t.Fatal("the route vocabulary has no \"embedding\" — Q5's generalisation was never made")
	}
	r := model.Route{ID: "r", Provider: "openai", Model: "text-embedding-3-small", Capabilities: []string{"embedding"}}
	if err := r.NormalizeMetadata(); err != nil {
		t.Fatalf("an embedding-capable route is refused by the route validator: %v", err)
	}

	// And NO parallel path: the provider that speaks no embeddings is
	// refused at the client door, naming the provider, BEFORE any wire —
	// which also proves retrieval could not have grown its own HTTP: the
	// one client owns this answer.
	anth := &model.Route{ID: "claude", Provider: "anthropic", Model: "claude-x"}
	e := ModelClientEmbedder{C: nil} // nil client: refusal comes from the seam's own gate
	_, _, _, err := e.EmbedBatch(context.Background(), anth, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("the seam did not guard its own client: %v", err)
	}
	// (The anthropic refusal itself lives in internal/model's embed wire
	// test, where the adapters are registered — the grep here would be a
	// test reaching across packages to assert a comment.)
}
