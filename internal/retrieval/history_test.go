//go:build !noretrieval

package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// history_test.go — V15 as a PROPERTY with two enforcement points, N6's
// permanence, and the two-way wall of V2/D12. The gate that survives is
// TestStepMessageIsNeverIndexed's SECOND assertion: the index's contents
// catch the direct case; the grep over the indexer's inputs catches the
// version where somebody adds a source that reads a transcript on the way.

// historyRoot seeds a history corpus plus a code corpus in one root — the
// two-way wall needs both populated.
func historyRoot(t *testing.T) (*Index, string) {
	t.Helper()
	x := openStoreT4(t)
	root := t.TempDir()
	must(t, x.EnsureCorpus(context.Background(), root, CorpusHistory), "ensure history")
	must(t, x.EnsureCorpus(context.Background(), root, CorpusCode), "ensure code")
	return x, root
}

// the step whose transcript must never reach any chunk, anywhere:
const (
	transcriptSecret  = "grep-output-from-bash-QX9"
	transcriptSecret2 = "tool-payload-filecontents-ZK7"
)

// seedStep writes a real transcript pair through the house doors
// (CreateLoop → CreateStep → ContinueStepWithMessages) — raw tool output
// landing exactly the way a live step lands it.
func seedStep(t *testing.T, x *Index, root string) {
	t.Helper()
	_, err := x.s.CreateLoop(store.NewLoop{
		Slug: "hloop", Title: "hist", Goal: "g", Root: root, ComponentRoot: root,
		Mode: "attended", Phase: "build", Status: "running",
	})
	must(t, err, "loop")
	step, err := x.s.CreateStep(store.Scope{LoopSlug: "hloop", ComponentRoot: root}, store.NewStep{Phase: "build", Agent: "worker"})
	must(t, err, "step")
	db := x.s.GormDB()
	mustAny(t, db.Exec(`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at) VALUES (?, 1, 1, 'user', 'run the build', '2026-10-29T00:00:00Z')`, step.ID).Error, "user row")
	mustAny(t, db.Exec(`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at) VALUES (?, 2, 1, 'assistant', 'done', '2026-10-29T00:00:00Z')`, step.ID).Error, "assistant row")
	mustAny(t, db.Exec(`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at) VALUES (?, 10, 1, 'tool', ?, '2026-10-29T00:00:00Z')`,
		step.ID, "bash said: "+transcriptSecret).Error, "tool row")
	mustAny(t, db.Exec(`INSERT INTO step_message (step_id, seq, format_version, role, content, created_at) VALUES (?, 11, 1, 'tool', ?, '2026-10-29T00:00:00Z')`,
		step.ID, "file contents were: "+transcriptSecret2).Error, "tool row 2")
}
func TestStepMessageIsNeverIndexed(t *testing.T) {
	x, root := historyRoot(t)
	seedStep(t, x, root)
	// index everything a history walk can be asked to index, INCLUDING the
	// transcript's neighbours
	_, err := x.IndexTree(context.Background(), root, CorpusHistory)
	must(t, err, "index history tree")

	// ASSERTION ONE: the index's contents. Every body in both corpora must
	// hold no trace of what only the transcript holds.
	for _, corpus := range []string{CorpusCode, CorpusHistory} {
		row, ok, err := x.s.RetrievalCorpusRow(root, corpus)
		must(t, err, "corpus row")
		if !ok {
			continue
		}
		chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
		must(t, err, "chunks")
		for _, c := range chunks {
			for _, secret := range []string{transcriptSecret, transcriptSecret2} {
				if strings.Contains(c.Body, secret) {
					t.Fatalf("%s/%s:%d carries transcript content %q — V15 is broken at the contents level",
						corpus, c.Source, c.LineStart, secret)
				}
			}
		}
	}

	// ASSERTION TWO (the one that survives): the indexer's INPUTS must not
	// reference the transcript at all — no SQL, no store method, not even
	// in a comment. This catches the future source that reads transcripts
	// "just for context" on the way to a legitimate chunk. Ban tokens are
	// externalised to testdata (the hunter cannot contain the hunted —
	// fourth occurrence taught the lesson; this is the fifth, and the
	// ban-file discipline holds).
	bans := bannedTokens(t, "embed-banned-transcript.txt") // >= 5 floor
	sources := mustReadDirGo(t, ".")
	for file, body := range sources {
		for _, tok := range bans {
			if strings.Contains(body, tok) {
				t.Errorf("%s references %q — the indexer's inputs may not reach the transcript directly OR indirectly (V15/D7/I4); if this file legitimately needs the word, the ban list itself needs the argument that history_test cannot make for you", file, tok)
			}
		}
	}
}

func TestEveryIndexedSourceHasARedactionStep(t *testing.T) {
	// Against CORPUS.md's committed list: every source in the CODE's list
	// names a redactor (never empty), appears in CORPUS.md, and vice versa.
	corpus := mustRead(t, "CORPUS.md")
	for _, src := range IndexedHistorySources {
		if strings.TrimSpace(src.Redactor) == "" {
			t.Fatalf("source %q has no redaction step — V15: a source without what-stood-between is not indexable", src.Source)
		}
		if !strings.Contains(corpus, src.Source) {
			t.Fatalf("source %q is in the code's list but not in CORPUS.md — the two must move together (the decision belongs with the reasoning)", src.Source)
		}
		// a NON-EMPTY redactor must appear in the file — "the source name
		// appears" alone would pass on a row that forgot its reasoning
		if !strings.Contains(corpus, src.Redactor) {
			t.Fatalf("CORPUS.md does not state what stood between %q and the index (looked for %q)", src.Source, src.Redactor)
		}
	}
	for _, src := range ForbiddenHistorySources {
		if !strings.Contains(corpus, src.Source) || !strings.Contains(corpus, "Never indexed") {
			t.Fatalf("forbidden source %q not recorded in CORPUS.md with its refusal", src.Source)
		}
	}
	// And the file names Q8, so a reader finds the list where they look.
	if !strings.Contains(corpus, "Q8") {
		t.Error("CORPUS.md does not record that it answers Q8")
	}
}

func TestNoFlagEnablesTranscriptIndexing(t *testing.T) {
	// N6: no configuration path, no build tag, no environment variable.
	// The grep runs over the package's own sources — the only place such a
	// flag could arrive that this package controls. Assembled needles
	// (hunter/hunted).
	sources := mustReadDirGo(t, ".")
	needles := []string{
		"AEON_" + "INDEX_TRANSCRIPT",
		"index_transcripts",
		"//go:build" + " " + "indextt", // a tag-shaped name
		"enableTranscript",
		"AllowTranscript",
	}
	for file, body := range sources {
		for _, n := range needles {
			if strings.Contains(body, n) {
				t.Errorf("%s contains %q — N6: transcript indexing is permanent-never, not flag-off (and permanence is recorded, see TestPermanenceIsRecorded)", file, n)
			}
		}
	}
}

func TestPermanenceIsRecorded(t *testing.T) {
	// The comment the next person reads BEFORE they look for the flag.
	h := mustRead(t, "history.go")
	joined := strings.ToLower(h)
	for _, phrase := range []string{"never", "flag", "permanent"} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("history.go does not say the word %q — N6's permanence must be stated where the temptation arrives, in the code (the block comment), not only in CORPUS.md", phrase)
		}
	}
	// and the doc file says it too, in its own words
	c := strings.ToLower(mustRead(t, "CORPUS.md"))
	if !strings.Contains(c, "permanent") || !strings.Contains(c, "flag") {
		t.Error("CORPUS.md does not record permanence (N6)")
	}
}

func TestHistoryQueryReturnsNoCode(t *testing.T) {
	// V2/D12 both ways: a history query returns no code, and a code query
	// returns no history. Two corpora, one root — the wall is a property
	// of the query, not of luck in seeding.
	x, root := historyRoot(t)
	must(t, os.WriteFile(filepath.Join(root, "svc.go"),
		[]byte("package main\n\nfunc computeWidgetAllocation(n int) int {\n\treturn n * 3\n}\n"), 0o644), "seed code")
	must(t, os.MkdirAll(filepath.Join(root, "decisions"), 0o755), "mkdir decisions")
	must(t, os.WriteFile(filepath.Join(root, "decisions", "brief-queue.md"),
		[]byte("# brief\n\n## decision\n\nWe drain the queue FIFO. computeWidgetAllocation stays a name only here.\n"), 0o644), "seed brief dir")
	// history artefacts come through IndexHistoryFile (the review door)
	_, err := x.IndexHistoryFile(context.Background(), root, "decisions/brief-queue.md")
	must(t, err, "index history file")
	_, err = x.IndexTree(context.Background(), root, CorpusCode)
	must(t, err, "index code")

	// keyword halves answer without a route (no embedding configured:
	// degraded-but-honest, D4) — the wall is asserted on SOURCES.
	hits, av, err := x.KeywordSearch(context.Background(), root, CorpusHistory, "computeWidgetAllocation", nil, 10)
	must(t, err, "history search")
	if !av.Available {
		t.Fatalf("history unavailable: %+v", av)
	}
	for _, h := range hits {
		if strings.HasSuffix(h.Source, ".go") {
			t.Fatalf("a HISTORY query returned %q — the corpora merged (V2/D12)", h.Source)
		}
	}
	// the code corpus holds NO markdown at all (the corpora are different
	// populations; the .md document reached the code query only because
	// this root had a stale shared corpus row earlier in the session —
	// here the assertion is the population itself):
	row, ok, err := x.s.RetrievalCorpusRow(root, CorpusCode)
	must(t, err, "code row")
	if ok {
		chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
		must(t, err, "code chunks")
		for _, c := range chunks {
			if strings.HasSuffix(c.Source, ".md") {
				t.Fatalf("the CODE corpus holds %q — the corpora merged at the write door, not the query (V2/D12)", c.Source)
			}
		}
	}
	kh, _, err := x.KeywordSearch(context.Background(), root, CorpusCode, "drain", nil, 10)
	must(t, err, "code search")
	for _, h := range kh {
		if strings.HasSuffix(h.Source, ".md") {
			t.Fatalf("a CODE query returned %q — the corpora merged (V2/D12)", h.Source)
		}
	}
	// and the demo's first question lands in the right corpus:
	before, _, err := x.KeywordSearch(context.Background(), root, CorpusHistory, "FIFO", nil, 10)
	must(t, err, "have we decided this before")
	found := false
	for _, h := range before {
		if strings.Contains(h.Source, "brief-queue") {
			found = true
		}
	}
	if !found {
		t.Fatal("the demo's first question (\"have we decided this before?\") found nothing in the history corpus")
	}
}

func TestProseTakesTheWindowPath(t *testing.T) {
	// V11's fallback is the NORMAL path for prose; sections are boundaries
	// where present. A document LONGER than one window, with section
	// headers, must chunk into multiple chunks (no blob), and a section
	// must be able to live in a chunk of its own.
	x, root := historyRoot(t)
	var b strings.Builder
	b.WriteString("# title\n\nintro text here\n\n## alpha section\n\n")
	for i := 0; i < 90; i++ {
		b.WriteString("alpha prose line about the widget budget\n")
	}
	b.WriteString("\n## beta section\n\n")
	for i := 0; i < 90; i++ {
		b.WriteString("beta prose line about the socket budget\n")
	}
	must(t, os.WriteFile(filepath.Join(root, "notes.md"), []byte(b.String()), 0o644), "seed prose")
	res, err := x.IndexHistoryFile(context.Background(), root, "notes.md")
	must(t, err, "index prose")
	if res.Written < 2 {
		t.Fatalf("a %d-line document produced %d chunk — the window path must walk (T2/V11: the window is the NORMAL prose path, not a degraded one)", len(strings.Split(b.String(), "\n")), res.Written)
	}
	row, ok, err := x.s.RetrievalCorpusRow(root, CorpusHistory)
	must(t, err, "row")
	_ = ok
	chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
	must(t, err, "chunks")
	sawSeparatedBeta := false
	for _, c := range chunks {
		if strings.Contains(c.Body, "beta prose") && !strings.Contains(c.Body, "alpha prose") {
			sawSeparatedBeta = true // beta lives in a chunk without alpha's prose
		}
	}
	if !sawSeparatedBeta {
		t.Fatal("no chunk held beta without alpha — the 180-line document collapsed into overlapping blobs? (the window steps, it does not smear)")
	}
	// and the 1-indexed bounds hold for prose too (T2's bounds, V4's
	// locations — every chunk's range sits inside the file)
	lines := strings.Count(b.String(), "\n") + 1
	for _, c := range chunks {
		if c.LineStart < 1 || c.LineEnd > lines || c.LineEnd < c.LineStart {
			t.Fatalf("prose chunk %+v outside 1..%d", c, lines)
		}
	}
}

func TestHandoffsAreIndexedByField(t *testing.T) {
	x, root := historyRoot(t)
	body, err := x.IndexHandoff(context.Background(), root, "hloop", 7, map[string]string{
		"status":   "ok",
		"summary":  "the retry budget is now bounded by the tick",
		"evidence": "internal/automation/tick.go",
		"sneaky":   "undeclared content that must not cross the schema boundary",
	})
	must(t, err, "index handoff")
	if strings.Contains(body, "sneaky") {
		t.Fatal("an undeclared key was rendered — the schema's boundary is the index's boundary (S13's O6)")
	}
	for _, f := range HandoffFieldNames {
		if !strings.Contains(body, f+":") {
			t.Fatalf("declared field %q is not rendered BY NAME — field name + purpose is what makes a handoff searchable", f)
		}
	}
	// searchable through the history corpus on the FIELD word:
	_, err = x.IndexHistoryFile(context.Background(), root, "handoff-hloop-7.md")
	// (the handoff text was returned for the caller to store; store it via
	// the same prose door to make it queryable):
	must(t, os.WriteFile(filepath.Join(root, "handoff-hloop-7.md"), []byte(body), 0o644), "persist handoff")
	_, err = x.IndexHistoryFile(context.Background(), root, "handoff-hloop-7.md")
	must(t, err, "index handoff file")
	hits, _, err := x.KeywordSearch(context.Background(), root, CorpusHistory, "summary", nil, 10)
	must(t, err, "search by field")
	found := false
	for _, h := range hits {
		if strings.Contains(h.Source, "handoff") {
			found = true
		}
	}
	if !found {
		t.Fatal("searching by the declared FIELD name did not find the handoff — Purpose strings are in the index and useful (T5's gate)")
	}
}

func TestArtefactsFollowTheProject(t *testing.T) {
	// V8's incrementality on the history corpus: a brief written AFTER the
	// first walk is invisible until the reindex that follows it.
	x, root := historyRoot(t)
	first, err := x.IndexHistoryFile(context.Background(), root, "decisions/late-brief.md")
	if err == nil {
		t.Fatalf("a brief that does not exist yet was indexed: %+v", first)
	}
	must(t, os.MkdirAll(filepath.Join(root, "decisions"), 0o755), "mkdir")
	must(t, os.WriteFile(filepath.Join(root, "decisions", "late-brief.md"),
		[]byte("# late brief\n\ndecided: the queue drains per tick.\n"), 0o644), "write brief")
	second, err := x.IndexHistoryFile(context.Background(), root, "decisions/late-brief.md")
	must(t, err, "index late brief")
	if second.Written == 0 {
		t.Fatal("the reindex after the artefact landed wrote nothing — V8's follow-through is broken")
	}
	hits, _, err := x.KeywordSearch(context.Background(), root, CorpusHistory, "tick", nil, 10)
	must(t, err, "search")
	found := false
	for _, h := range hits {
		if strings.Contains(h.Source, "late-brief") {
			found = true
		}
	}
	if !found {
		t.Fatal("a brief written by a loop is not searchable after its reindex (T5's gate)")
	}
}

func TestSecretsAreRedactedBeforeAnythingIsStored(t *testing.T) {
	// The narrow net under the review step (history.go's rule): a pasted
	// credential shape must not reach a chunk body or an embed call, even
	// in a "reviewed" artefact.
	x, root := historyRoot(t)
	must(t, os.WriteFile(filepath.Join(root, "oops-brief.md"),
		[]byte("# brief\n\nthe provider key is sk-liveTokenLeak123 and it worked.\n"), 0o644), "seed")
	_, err := x.IndexHistoryFile(context.Background(), root, "oops-brief.md")
	must(t, err, "index")
	row, ok, err := x.s.RetrievalCorpusRow(root, CorpusHistory)
	must(t, err, "row")
	_ = ok
	chunks, err := x.s.RetrievalChunksForEmbedding(row.ID)
	must(t, err, "chunks")
	for _, c := range chunks {
		if strings.Contains(c.Body, "sk-liveTokenLeak123") {
			t.Fatal("a credential shape was stored — redaction must happen BEFORE any store door (V15's \"un-redacted\" half)")
		}
		if !strings.Contains(c.Body, "[redacted]") {
			t.Fatal("the redaction left no trace — [redacted] is the honest mark of removal, not silence")
		}
	}
}

func TestChatModelTurnsRefuse(t *testing.T) {
	x, root := historyRoot(t)
	if err := x.IndexChatMessage(context.Background(), root, "hloop", "assistant", transcriptSecret); err == nil {
		t.Fatal("an assistant turn was accepted into history — a model turn can quote tool output verbatim (step_message's side door)")
	}
	if err := x.IndexChatMessage(context.Background(), root, "hloop", "tool", "whatever"); err == nil {
		t.Fatal("a tool turn was accepted")
	}
	if err := x.IndexChatMessage(context.Background(), root, "hloop", "user", "have we decided the queue order before?"); err != nil {
		t.Fatalf("the human turn refused: %v — the human IS the redaction step for chat", err)
	}
	_ = root
}

// --- helpers local to this file ---------------------------------------------

func mustAny(t *testing.T, err error, what string) {
	t.Helper()
	must(t, err, what)
}

func mustReadDirGo(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err, "read dir")
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// the scanner's own bytes must not carry the ban words it greps;
		// TestStepMessageIsNeverIndexed greps the OTHER files.
		if e.Name() == "history_test.go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		must(t, err, "read "+e.Name())
		out[e.Name()] = string(b)
	}
	return out
}
