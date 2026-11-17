package runtime

// S22 T7 — resolution feeds the manifest, once, and says where it came from.
//
// The integrity of this whole slice sits on TestResumeDoesNotReResolve: a
// resumed or recovered step that re-asked the index could get a DIFFERENT
// context from the one that suspended, which unmakes K14's changed-file
// report and S7's recovery. The spy at zero on BOTH paths is the assertion;
// a test that covers only resume passes while recovery quietly re-resolves.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/search"
)

// retrievalSpy counts resolution calls. Every one of them is an event that
// may happen exactly once per declaration — the counts ARE the assertions.
type retrievalSpy struct {
	calls   []retrievalCall
	hits    []search.SearchHit
	err     error
	keyword bool
}

type retrievalCall struct {
	corpus, query string
	limit         int
}

func (s *retrievalSpy) resolve(_ context.Context, corpus, query string, limit int) (search.SearchAnswer, error) {
	s.calls = append(s.calls, retrievalCall{corpus, query, limit})
	if s.err != nil {
		return search.SearchAnswer{}, s.err
	}
	return search.SearchAnswer{Hits: s.hits, KeywordOnly: s.keyword}, nil
}

func retrievalHits() []search.SearchHit {
	return []search.SearchHit{
		{Source: "internal/policy/match.go", LineStart: 41, LineEnd: 66, Score: 0.42},
	}
}

func retrievalDeps(files map[string]string, spy *retrievalSpy) ContextDeps {
	d := newReadSpy(files).deps()
	d.Search = spy.resolve
	return d
}

func retrievalSpec(corpus, query string) SectionSpec {
	return SectionSpec{Title: "Retrieved for query \"" + query + "\"", Retrieval: &RetrievalSpec{Corpus: corpus, Query: query}, Category: CategoryInjectedContext, Destination: SectionTranscript}
}

func TestManifestRecordsQueryAndScore(t *testing.T) {
	spy := &retrievalSpy{hits: retrievalHits()}
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Plan", Text: "the declared plan", Category: CategoryInjectedContext, Destination: SectionTranscript},
		retrievalSpec("code", "where is the retry policy"),
	}, retrievalDeps(map[string]string{"internal/policy/match.go": "package policy\n"}, spy))
	if !res.OK {
		t.Fatalf("assembly refused: %s", res.Reason)
	}
	if len(res.Manifest.Files) != 1 {
		t.Fatalf("%d entries; a text section adds none, the retrieved file adds one", len(res.Manifest.Files))
	}
	got := res.Manifest.Files[0]
	if got.Resolution != "retrieved" || got.Query != "where is the retry policy" || got.Score != 0.42 {
		t.Errorf("provenance not recorded: %+v", got)
	}
	// A declared entry says so and carries NEITHER query nor score (C4):
	// `omitempty` makes absence structural, and a declared entry wearing a
	// query would be a provenance lie.
	declared := AssembleContext(context.Background(), []SectionSpec{
		{Title: "F", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
	}, newReadSpy(map[string]string{"a.md": "alpha"}).deps())
	if !declared.OK {
		t.Fatalf("declared assembly refused: %s", declared.Reason)
	}
	d := declared.Manifest.Files[0]
	if d.Resolution != "declared" || d.Query != "" || d.Score != 0 {
		t.Errorf("declared entry carries retrieval fields: %+v", d)
	}
}

func TestDeclaredAndRetrievedAreDistinct(t *testing.T) {
	// Asserted on the STORED manifest — the bytes the daemon persists and a
	// replay reads back — not on a rendering.
	spy := &retrievalSpy{hits: retrievalHits()}
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "F", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		retrievalSpec("code", "retry"),
	}, retrievalDeps(map[string]string{"a.md": "alpha", "internal/policy/match.go": "package policy\n"}, spy))
	if !res.OK {
		t.Fatalf("assembly refused: %s", res.Reason)
	}
	b, err := json.Marshal(res.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, `"resolution":"declared"`) || !strings.Contains(text, `"resolution":"retrieved"`) {
		t.Fatalf("the two kinds are not both stated in the stored manifest: %s", text)
	}
	var perEntry struct {
		Files []struct {
			Path  string  `json:"path"`
			Query *string `json:"query"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &perEntry); err != nil {
		t.Fatal(err)
	}
	if perEntry.Files[0].Query != nil {
		t.Errorf("the declared entry carries a query key: %s", text)
	}
	if perEntry.Files[1].Query == nil || *perEntry.Files[1].Query != "retry" {
		t.Errorf("the retrieved entry does not carry its query: %s", text)
	}
	var back ContextManifest
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Files[0].Resolution != "declared" || back.Files[1].Resolution != "retrieved" {
		t.Errorf("round-trip merged the kinds: %+v", back.Files)
	}
}

func TestNothingFoundIsRecorded(t *testing.T) {
	// O6: not an error, not silence. The step PROCEEDS and the manifest says
	// the index was asked and had no opinion.
	spy := &retrievalSpy{}
	res := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Plan", Text: "the declared plan", Category: CategoryInjectedContext, Destination: SectionTranscript},
		retrievalSpec("code", "the nothing query"),
	}, retrievalDeps(nil, spy))
	if !res.OK {
		t.Fatalf("nothing-found failed the step: %s", res.Reason)
	}
	if len(res.Manifest.Retrievals) != 1 {
		t.Fatalf("the nothing was not recorded: %+v", res.Manifest.Retrievals)
	}
	rec := res.Manifest.Retrievals[0]
	if rec.Found || rec.Hits != 0 || rec.Files != 0 || rec.Query != "the nothing query" || rec.Corpus != "code" {
		t.Errorf("the record is wrong: %+v", rec)
	}
	if !strings.Contains(res.Text, "found nothing") || !strings.Contains(res.Text, "the nothing query") {
		t.Errorf("the model cannot see that retrieval asked and found nothing: %q", res.Text)
	}
	if !strings.Contains(res.Text, "the declared plan") {
		t.Error("the step did not proceed on its declared context")
	}

	// A resolver REFUSAL (the empty index naming itself, D2) is recorded the
	// same way — with the resolver's own sentence — never a construction error.
	refusing := &retrievalSpy{err: errors.New(
		"retrieval: refused the query at /work (code) — index-empty: nothing is indexed for this corpus yet")}
	res2 := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Plan", Text: "plan", Category: CategoryInjectedContext, Destination: SectionTranscript}, retrievalSpec("code", "q"),
	}, retrievalDeps(nil, refusing))
	if !res2.OK {
		t.Fatalf("a refusing index failed the step: %s", res2.Reason)
	}
	if len(res2.Manifest.Retrievals) != 1 || !strings.Contains(res2.Manifest.Retrievals[0].Note, "index-empty") {
		t.Errorf("the resolver's sentence did not reach the record: %+v", res2.Manifest.Retrievals)
	}
}

func TestProvenanceReachesTheModel(t *testing.T) {
	// V14's mechanism: the annotation must be in the text the model is
	// SHOWN (the opening user message), not only in the stored manifest.
	spy := &retrievalSpy{hits: retrievalHits(), keyword: true}
	res := AssembleContext(context.Background(), []SectionSpec{
		retrievalSpec("code", "where is the retry policy"),
	}, retrievalDeps(map[string]string{"internal/policy/match.go": "package policy\n"}, spy))
	if !res.OK {
		t.Fatalf("assembly refused: %s", res.Reason)
	}
	messages := OpeningTranscript(testHandler("read"), res.SystemText, res.TranscriptText, nil)
	var user string
	for _, m := range messages {
		if m.Role == model.RoleUser {
			for _, b := range m.Content {
				user += b.Text
			}
		}
	}
	for _, want := range []string{
		`retrieved for query "where is the retry policy"`,
		"score 0.4200",
		"keyword-only",
		"similarity-matched, not declared",
		"verify",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("the opening user message lacks %q:\n%s", want, user)
		}
	}
}

func TestResolutionRespectsTheCorpus(t *testing.T) {
	// V2: code resolution searches code, not history — asserted on the
	// arguments the seam actually received, not on what it might do.
	spy := &retrievalSpy{hits: retrievalHits()}
	d := newReadSpy(map[string]string{"internal/policy/match.go": "package policy\n"}).deps()
	d.Search = spy.resolve
	res := AssembleContext(context.Background(), []SectionSpec{
		retrievalSpec("code", "c query"),
		retrievalSpec("history", "h query"),
	}, d)
	if !res.OK {
		t.Fatalf("assembly refused: %s", res.Reason)
	}
	if len(spy.calls) != 2 {
		t.Fatalf("%d resolutions; each declaration is asked once", len(spy.calls))
	}
	if spy.calls[0].corpus != "code" || spy.calls[0].query != "c query" {
		t.Errorf("first resolution: %+v", spy.calls[0])
	}
	if spy.calls[1].corpus != "history" || spy.calls[1].query != "h query" {
		t.Errorf("second resolution: %+v", spy.calls[1])
	}
	if spy.calls[0].limit != retrievedFileLimit {
		t.Errorf("limit %d, want the declared cap %d", spy.calls[0].limit, retrievedFileLimit)
	}
}

func TestAssemblyRefusesAnUnbuiltRetrieval(t *testing.T) {
	// V13 at the assembly door: no seam + a retrieval declaration is a
	// CONSTRUCTION refusal naming the absence — never a skipped section,
	// which would make "the index had no opinion" and "this binary cannot
	// answer" one unreadable state.
	res := AssembleContext(context.Background(), []SectionSpec{
		retrievalSpec("code", "q"),
	}, newReadSpy(nil).deps())
	if res.OK || !strings.Contains(res.Reason, "no Search seam was injected") {
		t.Errorf("assembly did not refuse the unbuilt retrieval: %+v", res)
	}
	// And a corpus is NAMED (O8).
	spy := &retrievalSpy{}
	res2 := AssembleContext(context.Background(), []SectionSpec{
		retrievalSpec("wiki", "q"),
	}, retrievalDeps(nil, spy))
	if res2.OK || !strings.Contains(res2.Reason, "code") || !strings.Contains(res2.Reason, "history") {
		t.Errorf("the corpus refusal does not name both options: %+v", res2)
	}
	if len(spy.calls) != 0 {
		t.Error("a refused declaration still asked the index")
	}
}

func TestResumeDoesNotReResolve(t *testing.T) {
	// THE assertion (D6, C5, I3): the retrieval spy is at ZERO on resume
	// AND on recovery, and the manifest is byte-identical across both.
	suspended, _ := suspendedFixture(t)

	spy := &retrievalSpy{hits: retrievalHits()}
	declared := AssembleContext(context.Background(), []SectionSpec{
		{Title: "F", DeclaredFile: "a.md", Category: CategoryInjectedContext, Destination: SectionTranscript},
		retrievalSpec("code", "where is the retry policy"),
	}, retrievalDeps(map[string]string{"a.md": "alpha", "internal/policy/match.go": "package policy\n"}, spy))
	if !declared.OK {
		t.Fatalf("declaration refused: %s", declared.Reason)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("%d resolutions at declaration; exactly one is the rule", len(spy.calls))
	}
	// The fixed-clock spy stamped DeadlineAt at its epoch; a real loop is
	// bounded by a real clock. The deadline is a bound, not provenance —
	// overriding it changes nothing the byte-identity below compares.
	declared.Manifest.DeadlineAt = time.Now().Add(time.Hour)
	// Byte-identity is asserted on the PROVENANCE the manifest froze — the
	// files and the retrieval records. IterationsUsed is a usage counter the
	// loop legitimately bumps on every iteration; it is not context, and
	// pretending otherwise would freeze a number that must move.
	beforeFiles, err := json.Marshal(declared.Manifest.Files)
	if err != nil {
		t.Fatal(err)
	}
	beforeRet, err := json.Marshal(declared.Manifest.Retrievals)
	if err != nil {
		t.Fatal(err)
	}
	sameProvenance := func(label string, got ContextManifest) {
		t.Helper()
		gf, err := json.Marshal(got.Files)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := json.Marshal(got.Retrievals)
		if err != nil {
			t.Fatal(err)
		}
		if string(gf) != string(beforeFiles) || string(gr) != string(beforeRet) {
			t.Errorf("the frozen provenance is not byte-identical across %s:\n%s %s\nvs\n%s %s",
				label, beforeFiles, beforeRet, gf, gr)
		}
	}

	asked := func() (*retrievalSpy, ContextDeps) {
		s := &retrievalSpy{hits: retrievalHits()}
		contents := map[string]string{"a.md": "alpha", "internal/policy/match.go": "package policy\n"}
		d := ContextDeps{ReadFile: func(path string) ([]byte, error) {
			if raw, ok := contents[path]; ok {
				return []byte(raw), nil
			}
			return nil, errors.New("read outside the declared set: " + path)
		}, Search: s.resolve}
		return s, d
	}

	// Resume path.
	req := suspended
	req.Manifest = declared.Manifest
	resumeSpy, deps := asked()
	res := ResumeStep(context.Background(), req, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools("read", "question"),
		Context: deps, HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("resume failed: %s", res.Reason)
	}
	if len(resumeSpy.calls) != 0 {
		t.Errorf("resume re-resolved %d time(s) — the frozen context is unmade (D6)", len(resumeSpy.calls))
	}
	sameProvenance("resume", res.Manifest)

	// Recovery path — the one nobody watches (C5).
	recSpy, recDeps := asked()
	recReq := suspended
	recReq.Manifest = declared.Manifest
	recReq.Recovery = true
	res2 := RecoverStep(context.Background(), recReq, RecoveryPlan{}, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools("read", "question"),
		Context: recDeps, HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if res2.Outcome != OutcomeOK {
		t.Fatalf("recovery failed: %s", res2.Reason)
	}
	if len(recSpy.calls) != 0 {
		t.Errorf("recovery re-resolved %d time(s) — recovery is exactly the path nobody watches (C5)", len(recSpy.calls))
	}
	sameProvenance("recovery", res2.Manifest)
}

func TestFrozenManifestSurvivesAChangedFile(t *testing.T) {
	// K14's report still works when the entry was a guess: Rehash compares
	// by path and hash only; the retrieved provenance rides along unchanged.
	spy := &retrievalSpy{hits: retrievalHits()}
	res := AssembleContext(context.Background(), []SectionSpec{
		retrievalSpec("code", "retry"),
	}, retrievalDeps(map[string]string{"internal/policy/match.go": "package policy\n"}, spy))
	if !res.OK {
		t.Fatalf("assembly refused: %s", res.Reason)
	}
	mutated := newReadSpy(map[string]string{"internal/policy/match.go": "package policy // edited while suspended"}).deps()
	changes := Rehash(res.Manifest, mutated)
	if len(changes.Changed) != 1 || changes.Changed[0] != "internal/policy/match.go" {
		t.Fatalf("K14's report lost the retrieved entry: %+v", changes)
	}
	if res.Manifest.Files[0].Hash != HashContent([]byte("package policy\n")) || res.Manifest.Files[0].Resolution != "retrieved" {
		t.Errorf("Rehash mutated the frozen entry: %+v", res.Manifest.Files[0])
	}
}

func TestNoFlagEnablesReResolution(t *testing.T) {
	// N7: permanence is structural. The seam lives in ONE file (assembly),
	// the resume/recovery sources must not even name it, and no toggle
	// string exists anywhere in the package.
	sources := packageSources(t)
	for name, src := range sources {
		if name == "context.go" {
			if !strings.Contains(src, "EXACTLY one instant") {
				t.Error("the permanence of declaration-only resolution is not recorded where the seam lives (N7)")
			}
			continue
		}
		if strings.Contains(src, ".Search(") || strings.Contains(src, "d.Search") {
			t.Errorf("%s calls the retrieval seam; resolution belongs to assembly alone (N7)", name)
		}
		// Toggle SHAPES only — prose about re-resolution is what the
		// permanence comments are allowed to say; an identifier or env word
		// is what a toggle would be.
		for _, toggle := range []string{"ReResolve", "Reresolve", "RE_RESOLVE", "RERESOLVE"} {
			if strings.Contains(src, toggle) {
				t.Errorf("%s names a re-resolution toggle (%s); there is no flag and there never will be (N7)", name, toggle)
			}
		}
	}
}

func TestResolvedContextReducesToolCalls(t *testing.T) {
	// V20 — the criterion for keeping the optional slice: the SAME step
	// (same handler, route, prompt, workspace) run WITHOUT and WITH resolved
	// context makes measurably fewer `grep`/`read` calls, COUNTED. The two
	// model turns are scripted — this measures the economy of context that
	// already answers, which is exactly what the live model will do, and the
	// numbers recorded in Notes are measured here, not asserted from the
	// design doc.
	without := newSpyTools("grep", "read")
	stWithout := testState(testHandler("grep", "read"), []model.ToolSpec{toolSpec("grep"), toolSpec("read")})
	stWithout.Messages = []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock("answer the question")}}}
	resWithout := RunLoop(context.Background(), stWithout, LoopDeps{
		Client: newFakeClient(
			scriptedTurn{ToolUses: []model.Block{
				toolUse("g1", "grep", `{"pattern":"retry"}`), toolUse("g2", "grep", `{"pattern":"backoff"}`),
				toolUse("r1", "read", `{"path":"escalate.go"}`), toolUse("r2", "read", `{"path":"retry.go"}`),
			}},
			scriptedTurn{Text: okHandoff},
		),
		Tools:       without,
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if resWithout.Outcome != OutcomeOK {
		t.Fatalf("the without-context run failed: %s", resWithout.Reason)
	}
	unCounted := countLookups(without.recorded())

	withSpy := &retrievalSpy{hits: retrievalHits()}
	resolved := AssembleContext(context.Background(), []SectionSpec{
		{Title: "Task", Text: "answer the question", Category: CategorySystemPrompt, Destination: SectionTranscript},
		retrievalSpec("code", "where is the retry policy"),
	}, retrievalDeps(map[string]string{"internal/policy/match.go": "package policy\nconst retry = true\n"}, withSpy))
	if !resolved.OK {
		t.Fatalf("declaration refused: %s", resolved.Reason)
	}
	if !strings.Contains(resolved.Text, "const retry = true") {
		t.Fatal("the resolved run's context does not actually contain the answer — the comparison would be rigged")
	}
	with := newSpyTools("grep", "read")
	stWith := testState(testHandler("grep", "read"), []model.ToolSpec{toolSpec("grep"), toolSpec("read")})
	stWith.Messages = []model.Message{{Role: model.RoleUser, Content: []model.Block{model.TextBlock(resolved.Text)}}}
	resWith := RunLoop(context.Background(), stWith, LoopDeps{
		Client:      newFakeClient(scriptedTurn{Text: okHandoff}),
		Tools:       with,
		HandoffSpec: HandoffSpec{Required: []string{"status", "summary"}},
	})
	if resWith.Outcome != OutcomeOK {
		t.Fatalf("the with-context run failed: %s", resWith.Reason)
	}
	withCounted := countLookups(with.recorded())
	t.Logf("V20 measured: grep/read calls WITHOUT resolved context = %d, WITH = %d", unCounted, withCounted)
	if withCounted >= unCounted {
		t.Errorf("resolved context did not earn its cost: %d lookups with, %d without (V20)", withCounted, unCounted)
	}
	if withCounted != 0 || unCounted != 4 {
		t.Errorf("the recorded numbers moved: with=%d want 0, without=%d want 4", withCounted, unCounted)
	}
}

func countLookups(calls []toolCall) int {
	n := 0
	for _, c := range calls {
		if c.Name == "grep" || c.Name == "read" {
			n++
		}
	}
	return n
}
