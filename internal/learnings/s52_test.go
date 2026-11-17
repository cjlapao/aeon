package learnings

// S52 T2 — the management half of the memory law, tested where the law
// lives: the retract stamp excludes the body from EVERY default read while
// Show flags it; the audit line rides the act in fixed daemon words; the
// similarity index loses what the reads lose (LM5, proven at the vector
// table); forAgent reads the resolved scopes through the SAME Query and
// costs an embed only when a query is asked; Describe keeps the four
// provenances four; the board filters daemon-side.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

func userActor() Actor { return Actor{Kind: "user", ID: "1000"} }
func cliActor() Actor  { return Actor{Kind: "cli", ID: "cli"} }

// TestRetractRefusesWithoutAHumanOrAReason — LM2/LM8 walked from the door:
// an agent-kind actor is refused BY NAME with the proposal alternative
// (T4), an empty actor is refused, a missing or over-long reason is
// refused — and each refusal leaves the row, the reads and the audit
// census exactly as they were. A refusal that wrote is a refusal that lied.
func TestRetractRefusesWithoutAHumanOrAReason(t *testing.T) {
	l, s := openLearnings(t)
	row := mustAppend(t, l, AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "the body stays", Provenance: ProvenanceTold})
	before, err := s.CountAllLearningChanges()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		why    string
		actor  Actor
		reason string
		want   string
	}{
		{"agent kind", Actor{Kind: "agent", ID: "loop-1"}, "because", "learning_retract"},
		{"system kind", Actor{Kind: "system", ID: "triage-feed"}, "because", "human door"},
		{"empty actor id", Actor{Kind: "user", ID: ""}, "because", "names itself"},
		{"no reason", userActor(), "", "reason"},
		{"long reason", userActor(), strings.Repeat("r", 257), "256"},
	}
	for _, c := range cases {
		_, changed, ref := l.Retract(row.ID, c.reason, c.actor)
		if ref == nil || !strings.Contains(strings.ToLower(ref.Reason), strings.ToLower(c.want)) {
			t.Fatalf("%s: ref = %+v, want a refusal naming %q", c.why, ref, c.want)
		}
		if changed {
			t.Fatalf("%s: a refusal changed the row", c.why)
		}
	}
	after, err := s.CountAllLearningChanges()
	if err != nil || after != before {
		t.Fatalf("audit census moved from %d to %d across five refusals — a refusal writes nothing, not even a line", before, after)
	}
	got, ok, _ := l.Show(row.ID)
	if !ok || got.Retracted {
		t.Fatalf("row after refusals = %+v — five refused acts must leave it untouched", got)
	}
}

// TestRetractStampsExcludesAndAudits — the act itself: the stamp lands,
// every default read (List, Query, Board) loses the body, Show and the
// explicit includeRetracted reads FLAGGED keep it (LM2's "flagged, never
// silently"), and the audit line says the act in the daemon's fixed
// template — naming neither the body nor the human's reason text (the
// reason lives in the stamp, where the row itself answers it).
func TestRetractStampsExcludesAndAudits(t *testing.T) {
	l, s := openLearnings(t)
	ctx := context.Background()
	body := "always run gofmt before push"
	row := mustAppendAs(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/repo", Body: body, Provenance: ProvenanceTold, Tags: []string{"go"}})
	got, changed, ref := l.Retract(row.ID, "superseded by CI lint", userActor())
	if ref != nil || !changed || !got.Retracted {
		t.Fatalf("retract = changed %v ref %+v, want the act done", changed, ref)
	}
	if got.RetractedAt == "" || got.RetractedReason != "superseded by CI lint" || got.RetractedBy != "user/1000" {
		t.Fatalf("stamp = %q/%q/%q, want instant + reason + kind/id author", got.RetractedAt, got.RetractedReason, got.RetractedBy)
	}
	if rows, err := l.List(ScopeProject, "/repo"); err != nil || len(rows) != 0 {
		t.Fatalf("List after retract = %d rows — the default read must lose it", len(rows))
	}
	if rows, err := l.ListOpts(ScopeProject, "/repo", true); err != nil || len(rows) != 1 || !rows[0].Retracted {
		t.Fatalf("ListOpts(includeRetracted) = %+v — the explicit exception must see it flagged", rows)
	}
	if rows, err := l.Query(ctx, QueryFilter{Scope: ScopeProject, ScopeKey: "/repo"}); err != nil || len(rows) != 0 {
		t.Fatalf("Query after retract = %d rows", len(rows))
	}
	if rows, err := l.Board(BoardFilter{}); err != nil || len(rows) != 0 {
		t.Fatalf("board after retract = %d rows — retracted rows are not memory", len(rows))
	}
	shown, ok, _ := l.Show(row.ID)
	if !ok || !shown.Retracted {
		t.Fatalf("Show = %v %+v, want the flagged row (retracted is shown, never silent)", ok, shown)
	}
	lines, err := s.ListLearningChanges(row.ID)
	if err != nil || len(lines) != 2 {
		t.Fatalf("audit lines = %d (err %v), want add + retract", len(lines), err)
	}
	want := fmt.Sprintf("retracted learning %d (scope project//repo)", row.ID)
	if lines[1].Op != "retract" || lines[1].Summary != want || lines[1].ActorKind != "user" || lines[1].ActorID != "1000" {
		t.Fatalf("retract line = %+v, want the fixed template %q by user/1000", lines[1], want)
	}
	if strings.Contains(lines[1].Summary, body) || strings.Contains(lines[1].Summary, "superseded") {
		t.Fatalf("audit summary carries the body or the reason text: %q (LM3: fixed templates only)", lines[1].Summary)
	}
}

// TestRetractAndRestoreAreIdempotent — the second act of each pair answers
// changed:false and writes NOTHING (LM2): the census stands at the first
// act's lines. Restore reverses the stamp and keeps the words: the reason
// and the by survive as recent history beside the now-live row, and the
// restore is what the audit says.
func TestRetractAndRestoreAreIdempotent(t *testing.T) {
	l, s := openLearnings(t)
	row := mustAppendAs(t, l, AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "b", Provenance: ProvenanceAssumed})
	if _, changed, ref := l.Retract(row.ID, "wrong", userActor()); ref != nil || !changed {
		t.Fatalf("first retract: changed %v ref %+v", changed, ref)
	}
	_, changed, ref := l.Retract(row.ID, "again", cliActor())
	if ref != nil || changed {
		t.Fatalf("second retract: changed %v ref %+v, want the honest no-op", changed, ref)
	}
	n, _ := s.CountAllLearningChanges()
	if n != 2 {
		t.Fatalf("census %d after the double retract, want 2 — a no-op writes no audit line", n)
	}
	got, changed, ref := l.Restore(row.ID, cliActor())
	if ref != nil || !changed || got.Retracted {
		t.Fatalf("restore: %+v changed %v ref %+v", got, changed, ref)
	}
	if got.RetractedAt != "" || got.RetractedReason != "wrong" || got.RetractedBy != "user/1000" {
		t.Fatalf("restore cleared the wrong half: %q/%q/%q — the instant clears, the words stay", got.RetractedAt, got.RetractedReason, got.RetractedBy)
	}
	_, changed, ref = l.Restore(row.ID, cliActor())
	if ref != nil || changed {
		t.Fatalf("second restore: changed %v ref %+v", changed, ref)
	}
	lines, _ := s.ListLearningChanges(row.ID)
	if len(lines) != 3 || lines[2].Op != "restore" || lines[2].ActorID != "cli" {
		t.Fatalf("lines = %+v, want add/retract/restore with the third by cli", lines)
	}
	if rows, err := l.List(ScopePerson, "me"); err != nil || len(rows) != 1 {
		t.Fatalf("restored row absent from the default List")
	}
}

// TestRetractedBodyIsNotRerankable — LM5's proof from the INDEX, not only
// from the reads: the same body that a similarity query ranks while live
// is unreachable after a retract with the EXACT BODY as the query, proven
// at the vector table itself (no row for the id, in any learning_vec
// table), and it returns to the index when a restore hands it back to the
// next embedding pass — EmbedPending reads through the live predicate, so
// the retracted body was never even embeddable while retracted.
func TestRetractedBodyIsNotRerankable(t *testing.T) {
	l, s := openLearnings(t)
	ctx := context.Background()
	route := &model.Route{ID: "embed", Provider: "openai", Model: "fake-embed"}
	body := "the vector body ranks first for itself"
	l.SetRoute(route, fakeEmbedder{table: map[string][]float32{body: {1, 0}, "the vector body ranks first for itself again": {0, 1}}})
	row := mustAppend(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: body, Provenance: ProvenanceTold})
	if _, err := l.EmbedPending(ctx, ScopeAgent, "golang-engineer"); err != nil {
		t.Fatal(err)
	}
	ids, err := s.LearningVecIDs("fake-embed", 2)
	if err != nil || len(ids) != 1 || ids[0] != row.ID {
		t.Fatalf("vec ids = %v (err %v), want the live row indexed", ids, err)
	}
	if hits, err := l.Query(ctx, QueryFilter{Scope: ScopeAgent, ScopeKey: "golang-engineer", QueryText: body}); err != nil || len(hits) != 1 {
		t.Fatalf("live similarity query = %d hits (err %v)", len(hits), err)
	}
	if _, _, ref := l.Retract(row.ID, "index hygiene", userActor()); ref != nil {
		t.Fatalf("retract: %+v", ref)
	}
	ids, err = s.LearningVecIDs("fake-embed", 2)
	if err != nil || len(ids) != 0 {
		t.Fatalf("vec ids after retract = %v (err %v) — the retract must drop the vector from every table (LM5)", ids, err)
	}
	if hits, err := l.Query(ctx, QueryFilter{Scope: ScopeAgent, ScopeKey: "golang-engineer", QueryText: body}); err != nil || len(hits) != 0 {
		t.Fatalf("exact-body query returned %d hits after the retract — the LM2 proof", len(hits))
	}
	// A re-retract while retracted cannot make it embeddable either.
	if _, err := l.EmbedPending(ctx, ScopeAgent, "golang-engineer"); err != nil {
		t.Fatal(err)
	}
	if ids, err = s.LearningVecIDs("fake-embed", 2); err != nil || len(ids) != 0 {
		t.Fatalf("EmbedPending embedded a RETRACTED body: %v", ids)
	}
	if _, _, ref := l.Restore(row.ID, userActor()); ref != nil {
		t.Fatalf("restore: %+v", ref)
	}
	if _, err := l.EmbedPending(ctx, ScopeAgent, "golang-engineer"); err != nil {
		t.Fatal(err)
	}
	if ids, err = s.LearningVecIDs("fake-embed", 2); err != nil || len(ids) != 1 || ids[0] != row.ID {
		t.Fatalf("vec ids after restore + embed pass = %v — the restored body returns to the index", ids)
	}
}

// countingEmbedder is the fakeEmbedder with a tally: the LM5 cost law is
// "no embedding call without a query", and a law about calls is proven by
// counting them, not by reading prose about them.
type countingEmbedder struct {
	table map[string][]float32
	calls *int
}

func (f countingEmbedder) Embed(ctx context.Context, route *model.Route, texts []string) ([][]float32, model.Usage, model.Cost, error) {
	*f.calls++
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := f.table[t]
		if !ok {
			v = []float32{0.1, 0.1}
		}
		out[i] = v
	}
	return out, model.Usage{}, model.Cost{State: model.CostKnown}, nil
}

// TestForAgentEqualsTheRealRetrievalRanking — the differential that makes
// the door honest rather than decorative: forAgent's answer must be the
// SAME set the four scope reads answer (own scope, person, the projects
// the agent has RUN IN — from step joined to loop, not a guess beside it),
// and with a query its order must be what the one ranker over that merged
// set answers, then partitioned by M1 (relayed last). Another agent's
// scope and another project's rows never appear.
func TestForAgentEqualsTheRealRetrievalRanking(t *testing.T) {
	l, s := openLearnings(t)
	ctx := context.Background()
	calls := 0
	route := &model.Route{ID: "embed", Provider: "openai", Model: "fake-embed"}
	l.SetRoute(route, countingEmbedder{calls: &calls, table: map[string][]float32{
		"own lesson":     {1, 0},
		"person lesson":  {0.5, 0.5},
		"project lesson": {0.9, 0.1},
		"relayed lesson": {0.99, 0.01},
		"auth":           {1, 0},
	}})
	// The running itself: a loop in /srv/a with a step by the agent's
	// NAME (what step.agent carries), and a second agent running in /srv/b.
	seedRun(t, s, "loop-a", "/srv/a", "golang-engineer")
	seedRun(t, s, "loop-b", "/srv/b", "imported-agent")

	own1 := mustAppendAs(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "own lesson", Provenance: ProvenanceObserved})
	ownRelay, ref := l.AppendRelayedAs(AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "relayed lesson", Provenance: ProvenanceRelayed}, Actor{Kind: "system", ID: "board-watch"})
	if ref != nil {
		t.Fatalf("relayed append: %+v", ref)
	}
	person := mustAppendAs(t, l, AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "person lesson", Provenance: ProvenanceTold})
	projA := mustAppendAs(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/srv/a", Body: "project lesson", Provenance: ProvenanceAssumed})
	mustAppendAs(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "imported-agent", Body: "other agent lesson", Provenance: ProvenanceTold})
	mustAppendAs(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "/srv/b", Body: "other project lesson", Provenance: ProvenanceTold})

	hits, r := l.ForAgent(ctx, "golang-engineer", "", 20)
	if r != nil {
		t.Fatalf("forAgent: %+v", r)
	}
	var got []int64
	for _, h := range hits {
		got = append(got, h.Learning.ID)
	}
	// The SET half: exactly own ∪ person ∪ projects-run-in.
	if calls != 0 {
		t.Fatalf("embedding calls without a query = %d — the glance must not bill (LM5)", calls)
	}
	set := map[int64]bool{own1.ID: true, ownRelay.ID: true, person.ID: true, projA.ID: true}
	if len(got) != len(set) {
		t.Fatalf("forAgent set %v, want exactly %v", got, set)
	}
	for _, id := range got {
		if !set[id] {
			t.Fatalf("forAgent answered id %d outside the resolved scopes", id)
		}
	}
	// The ORDER half without a query: scope order, then M1's partition —
	// the relayed lesson sits below every non-relayed one, note quoting.
	if got[len(got)-1] != ownRelay.ID {
		t.Fatalf("no-query order %v, want the relayed lesson last (M1)", got)
	}
	if !strings.Contains(hits[len(hits)-1].Why.M1Note, "relayed ranks below") {
		t.Fatalf("why note = %q, want M1's law quoted", hits[len(hits)-1].Why.M1Note)
	}
	if !strings.Contains(hits[0].Why.Basis, "no embedding call") {
		t.Fatalf("basis = %q, want the honest no-query basis", hits[0].Why.Basis)
	}

	// The differential WITH a query: the same ranker the real retrieval
	// uses, over the same merged set, computed independently here. The
	// expected order is computed FIRST, so the call tally below counts
	// only what forAgent itself embeds.
	merged := []Learning{own1, person, projA, ownRelay}
	ranked := l.rankWithinSet(ctx, "auth", merged)
	want := []int64{}
	for _, lr := range ranked {
		if !isRelayed(lr) {
			want = append(want, lr.ID)
		}
	}
	want = append(want, ownRelay.ID)
	before := calls
	hits, r = l.ForAgent(ctx, "golang-engineer", "auth", 20)
	if r != nil {
		t.Fatalf("forAgent with query: %+v", r)
	}
	got = nil
	for _, h := range hits {
		got = append(got, h.Learning.ID)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("forAgent query order %v, want the real ranking %v", got, want)
	}
	if hits[0].Why.Rank != 1 {
		t.Fatalf("first hit rank = %d, want 1", hits[0].Why.Rank)
	}
	// One embed for the query — the bodies' vectors were already in the
	// index; a door that re-embedded every row would bill per glance.
	if calls-before != 1 {
		t.Fatalf("embedding calls with a query = %d, want the one query call", calls-before)
	}
	// Reads wrote nothing: the census is still exactly the six audited
	// seed writes (five appends + one relayed append).
	if n, err := s.CountAllLearningChanges(); err != nil || n != 6 {
		t.Fatalf("audit census after the reads = %d (err %v), want the 6 seed writes and nothing else", n, err)
	}
}

// TestForAgentRefusals — an unknown slug is refused naming it (LM1's
// authority: the slug is the identity); the limit caps honestly at the
// vocabulary's own maximum.
func TestForAgentRefusals(t *testing.T) {
	l, _ := openLearnings(t)
	if _, r := l.ForAgent(context.Background(), "nobody", "", 0); r == nil || !strings.Contains(r.Reason, "nobody") {
		t.Fatalf("unknown agent: %+v, want a refusal naming the slug", r)
	}
	if _, r := l.ForAgent(context.Background(), "", "", 0); r == nil {
		t.Fatal("empty agent refused nothing")
	}
}

// seedRun puts a loop with a project root and one step by an agent NAME
// into the store — the running that forAgent's project resolution reads.
func seedRun(t *testing.T, s *store.Store, slug, root, agentName string) {
	t.Helper()
	if _, err := s.CreateLoop(store.NewLoop{Slug: slug, Title: slug, Root: root, ComponentRoot: root, Mode: "plan", Status: "running"}); err != nil {
		t.Fatalf("seed loop %s: %v", slug, err)
	}
	if _, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root}, store.NewStep{TaskID: "t1", Agent: agentName, Attempt: 1}); err != nil {
		t.Fatalf("seed step in %s: %v", slug, err)
	}
}

// TestDescribeKeepsTheFourProvenancesDistinct — LM6's formatter: four
// claims about the world, four different sentences; relayed says channel
// and NEVER verified (M1), observed carries the count as a word not a
// merged score.
func TestDescribeKeepsTheFourProvenancesDistinct(t *testing.T) {
	three := 3
	four := []Learning{
		{Provenance: ProvenanceTold},
		{Provenance: ProvenanceObserved, ObservedCount: &three},
		{Provenance: ProvenanceAssumed},
		{Provenance: ProvenanceRelayed},
	}
	seen := map[string]string{}
	for _, lr := range four {
		d := Describe(lr)
		if other, dup := seen[d]; dup {
			t.Fatalf("Describe says %q for both %s and %s — four provenances, four words (LM6)", d, other, lr.Provenance)
		}
		seen[d] = string(lr.Provenance)
	}
	relay := Describe(four[3])
	if !strings.Contains(relay, "channel") || strings.Contains(strings.ToLower(relay), "verified") && !strings.Contains(relay, "never a claim that it was verified") {
		t.Fatalf("relayed = %q — a channel fact, never dressed as verified (M1)", relay)
	}
	if !strings.Contains(Describe(four[1]), "(×3)") {
		t.Fatalf("observed ×3 = %q — the count rides the word (M3)", Describe(four[1]))
	}
}

// TestBoardFiltersRunDaemonSide — LM6: every filter ANDs and runs beside
// the rows here, not in a client; scope+key pairs from another scope never
// collide; the includeRetracted exception is explicit and the flagged
// state comes with it.
func TestBoardFiltersRunDaemonSide(t *testing.T) {
	l, _ := openLearnings(t)
	loop := "loop-x"
	tags := []string{"cgo"}
	mustAppend(t, l, AppendInput{Scope: ScopeAgent, ScopeKey: "golang-engineer", Body: "agent row", Provenance: ProvenanceObserved, Tags: tags, SourceLoopSlug: &loop})
	mustAppend(t, l, AppendInput{Scope: ScopeProject, ScopeKey: "golang-engineer", Body: "project row wearing the same key", Provenance: ProvenanceTold})
	mustAppend(t, l, AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "person row", Provenance: ProvenanceTold})
	rows, err := l.Board(BoardFilter{Agent: "golang-engineer"})
	if err != nil || len(rows) != 1 || rows[0].Body != "agent row" {
		t.Fatalf("board agent filter = %+v — a project row sharing the key must not answer (scope+key)", rows)
	}
	if rows, err := l.Board(BoardFilter{Tag: "cgo", Provenance: "observed"}); err != nil || len(rows) != 1 {
		t.Fatalf("board tag+provenance = %+v", rows)
	}
	if rows, err := l.Board(BoardFilter{Tag: "cgo", Provenance: "told"}); err != nil || len(rows) != 0 {
		t.Fatalf("AND filters do not AND: %+v", rows)
	}
	if rows, err := l.Board(BoardFilter{Loop: "loop-x"}); err != nil || len(rows) != 1 {
		t.Fatalf("board loop filter = %+v", rows)
	}
	row := mustAppend(t, l, AppendInput{Scope: ScopePerson, ScopeKey: "me", Body: "retracted row", Provenance: ProvenanceTold})
	if _, _, ref := l.Retract(row.ID, "gone", userActor()); ref != nil {
		t.Fatalf("retract: %+v", ref)
	}
	if rows, err := l.Board(BoardFilter{}); err != nil || len(rows) != 3 {
		t.Fatalf("default board = %d rows, want the retracted one absent", len(rows))
	}
	rows, err = l.Board(BoardFilter{IncludeRetracted: true})
	if err != nil || len(rows) != 4 {
		t.Fatalf("includeRetracted board = %d, want it back", len(rows))
	}
	for _, r := range rows {
		if r.Body == "retracted row" && !r.Retracted {
			t.Fatal("the includeRetracted row lost its flag")
		}
	}
}

// mustAppendAs is mustAppend's audited sibling — every fixture row in the
// management tests carries its own add line, because these tests count
// audit lines and the ledger must start truthful.
func mustAppendAs(t *testing.T, l *Learnings, in AppendInput) Learning {
	t.Helper()
	got, ref := l.AppendAs(in, userActor())
	if ref != nil {
		t.Fatalf("append as person (%s/%s/%s): %s", in.Scope, in.ScopeKey, in.Provenance, ref.Reason)
	}
	return got
}
