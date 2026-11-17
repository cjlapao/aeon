package daemon

// records_memory_test.go — S52 T4 (LM1, LM4, LM5, LM7, LM8): the four
// memory arms of the records service, at the arm. What each test pins, in
// the order:
//
//   - an unwired plane says so BY NAME (V13) — an empty readable set or a
//     zero count would read as "this agent remembers nothing";
//   - a PERSON meets each door by name with the pointer sentence (the
//     agent view is agent-scoped in BOTH directions; the person's own
//     doors already answer everything legitimately theirs);
//   - learning_add stamps, never trusts: agent scope under the caller's
//     own slug, project scope under the step's own root, a typed key that
//     names anything else REFUSED; provenance observed|assumed only —
//     told and relayed refused below the schema too (LM4); body and tags
//     bounded; and the write rides the plane with exactly ONE audit row,
//     actor kind agent, actor id the slug;
//   - at learnings.MaxSelfLogAddsPerAgent the refusal COUNTS — it names
//     the cap and what the agent filed in the window — and nothing files
//     past it (the cap is counted off the audit ledger, not beside it);
//   - the readable set (LM1/LM5): the person's word, the projects the
//     agent actually ran in, its own memory — and nothing else. A
//     sibling agent's memory and a root the agent never ran under answer
//     the SAME sentence a nonexistent id answers, in show and in the
//     propose alike: no oracle, in success or in error;
//   - the propose files the INERT learning_retract row through the shared
//     create path — nothing retracts, the stamp stays clean, the audit
//     ledger holds no retract line — until a person commits (LM7/LM8;
//     cmd/aeon's memory_proposal_test.go walks the commit).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
)

func newMemoryRecordsEnv(t *testing.T) (recordsEnv, *learnings.Learnings) {
	t.Helper()
	e := newRecordsEnv(t, "")
	l := learnings.New(e.s)
	e.svc.learningsPlane = l
	seedAgent(t, e, "Implementer", "implementer")
	seedAgent(t, e, "Sibling", "sibling")
	// The test mirror of cmd/aeon's production seam for the one kind:
	// desk's shape half verbatim, plus the two row facts the production
	// validator reads (it exists, it is not already retracted).
	e.svc.proposals = proposalStore(e.s)
	e.svc.validateProposal = func(ctx context.Context, kind, payload string) error {
		if err := desk.ValidateLearningRetract(ctx, []byte(payload)); err != nil {
			return err
		}
		var p desk.LearningRetractProposal
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		lr, found, err := l.Show(p.ID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("refused: no learning %d exists to take back (S52 T4)", p.ID)
		}
		if lr.Retracted {
			return fmt.Errorf("refused: learning %d is already retracted (S52 T4, TD3)", p.ID)
		}
		return nil
	}
	return e, l
}

func TestMemoryArmsWithoutThePlaneRefuseByName(t *testing.T) {
	e := newRecordsEnv(t, "") // plane deliberately unwired
	for _, tc := range []struct{ tool, input string }{
		{"learning_query", `{}`},
		{"learning_show", `{"id":1}`},
		{"learning_add", `{"scope":"agent","body":"b","provenance":"observed"}`},
		{"learning_propose_retract", `{"id":1,"reason":"stale"}`},
	} {
		_, f, err := e.do(t, tc.tool, tc.input)
		if err != nil || f == nil {
			t.Fatalf("%s unwired answered (%v %+v) — V13 says refuse by name, never answer zero", tc.tool, f, err)
		}
		if !strings.Contains(f.Sentence, tc.tool) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s refusal does not name itself: %s", tc.tool, f.Sentence)
		}
	}
}

func TestPersonMeetsTheMemoryDoorsByPointerSentence(t *testing.T) {
	e, _ := newMemoryRecordsEnv(t)
	needles := map[string]string{
		"learning_query":           "learnings.query",
		"learning_show":            "learnings.show",
		"learning_add":             "learnings.add",
		"learning_propose_retract": "learnings.retract",
	}
	for tool, door := range needles {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, tool, json.RawMessage(`{}`))
		if err != nil || f == nil {
			t.Fatalf("%s: a person was served the agent's door instead of refused (%v %+v)", tool, f, err)
		}
		if !strings.Contains(f.Sentence, "agent-scoped") || !strings.Contains(f.Sentence, door) {
			t.Errorf("%s: person refusal %q must be the pointer naming %s", tool, f.Sentence, door)
		}
	}
}

func TestLearningAddStampsIdentityAndRefusesForeignOnes(t *testing.T) {
	e, l := newMemoryRecordsEnv(t)

	// Own slug stamped when scopeKey is omitted.
	got, f, err := e.do(t, "learning_add", `{"scope":"agent","body":"the flaky test is the cache race, not the retry","tags":["ci","flaky"],"provenance":"observed"}`)
	if err != nil || f != nil {
		t.Fatalf("learning_add: %v %+v", err, f)
	}
	item, ok := got.(protocol.LearningsItemResult)
	if !ok {
		t.Fatalf("add answered %T, want protocol.LearningsItemResult", got)
	}
	if item.Learning.Scope != "agent" || item.Learning.ScopeKey != "implementer" {
		t.Errorf("stamped %s/%s, want agent/implementer — the scope is the identity's, never a typed one", item.Learning.Scope, item.Learning.ScopeKey)
	}
	if item.Learning.Provenance != "observed" || item.Learning.Retracted {
		t.Errorf("row came back %s/%v — observed and live are the only shapes this door writes", item.Learning.Provenance, item.Learning.Retracted)
	}
	// The confirming scopeKey is honoured; a FOREIGN one is refused.
	if _, f, err := e.do(t, "learning_add", `{"scope":"agent","scopeKey":"implementer","body":"confirming my own key is the stamp, not a claim","provenance":"assumed"}`); err != nil || f != nil {
		t.Fatalf("confirming scopeKey refused: %v %+v", err, f)
	}
	if _, f, err := e.do(t, "learning_add", `{"scope":"agent","scopeKey":"boss","body":"I would like the boss's memory please","provenance":"observed"}`); err != nil || f == nil || !strings.Contains(f.Sentence, "implementer") {
		t.Fatalf("foreign agent scopeKey answered %+v — an agent files under its OWN slug (LM1)", f)
	}

	// Project scope: the step's root, stamped; a named root refused; no
	// root, no filing; person scope is the person's door.
	if _, f, err := e.do(t, "learning_add", `{"scope":"project","body":"the build graph caches per root","tags":["build"],"provenance":"observed"}`); err != nil || f != nil {
		t.Fatalf("project add: %v %+v", err, f)
	}
	rows, err := l.Query(context.Background(), learnings.QueryFilter{Scope: learnings.ScopeProject, ScopeKey: "/repo/alpha"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("project read = %d rows (%v), want the one filed under the step's root", len(rows), err)
	}
	if _, f, _ := e.do(t, "learning_add", `{"scope":"project","scopeKey":"/repo/org-group","body":"filing into a root I do not run in","provenance":"observed"}`); f == nil {
		t.Error("a foreign project root was honoured instead of refused (LM1)")
	}
	if _, f, _ := e.do(t, "learning_add", `{"scope":"person","scopeKey":"operator","body":"telling the person's store what to remember","provenance":"observed"}`); f == nil || !strings.Contains(f.Sentence, "learnings.add") {
		t.Errorf("person scope answered %+v — the person's scope is the person's door (LM7)", f)
	}

	// LM4 below the schema: told and relayed are nameable by no agent door.
	for _, prov := range []string{"told", "relayed", "invented", ""} {
		_, f, _ := e.do(t, "learning_add", `{"scope":"agent","body":"x","provenance":"`+prov+`"}`)
		if f == nil || !strings.Contains(f.Sentence, "observed or assumed") {
			t.Errorf("provenance %q answered instead of the LM4 refusal (%+v)", prov, f)
		}
	}

	// Bounds: the body is a sentence, the tags are a handful.
	long := strings.Repeat("b", maxMineBodyBytes+1)
	if _, f, _ := e.do(t, "learning_add", `{"scope":"agent","body":"`+long+`","provenance":"observed"}`); f == nil || !strings.Contains(f.Sentence, "2048") {
		t.Errorf("a %d-byte body answered instead of naming the 2048 cap (%+v)", len(long), f)
	}
	if _, f, _ := e.do(t, "learning_add", `{"scope":"agent","body":"nine tags","tags":["a","b","c","d","e","f","g","h","i"],"provenance":"observed"}`); f == nil || !strings.Contains(f.Sentence, "8") {
		t.Errorf("nine tags answered instead of naming the cap (%+v)", f)
	}

	// And every file that DID land carries exactly one audit line, agent
	// kind, slug id — LM4's provenance observed at the ledger.
	hist, err := l.History(item.Learning.ID)
	if err != nil || len(hist) != 1 {
		t.Fatalf("audit lines = %v (%v), want exactly one", hist, err)
	}
	if hist[0].Op != "add" || hist[0].ActorKind != "agent" || hist[0].ActorID != "implementer" {
		t.Errorf("audit line = %+v, want add by agent/implementer", hist[0])
	}
}

func TestSelfLogCapIsCountedAndNamesItsNumber(t *testing.T) {
	e, _ := newMemoryRecordsEnv(t)
	for i := 0; i < learnings.MaxSelfLogAddsPerAgent; i++ {
		in := fmt.Sprintf(`{"scope":"agent","body":"filed entry %d for the ledger to count","provenance":"observed"}`, i)
		if _, f, err := e.do(t, "learning_add", in); err != nil || f != nil {
			t.Fatalf("entry %d refused early: %v %+v", i, err, f)
		}
	}
	_, f, err := e.do(t, "learning_add", `{"scope":"agent","body":"one past the cap","provenance":"observed"}`)
	if err != nil || f == nil {
		t.Fatal("the cap let a filing through — it is counted before the write, not after")
	}
	if !strings.Contains(f.Sentence, fmt.Sprint(learnings.MaxSelfLogAddsPerAgent)) || !strings.Contains(f.Sentence, "24h") {
		t.Errorf("the counted refusal does not name its number and window: %s", f.Sentence)
	}
}

func TestReadableSetAnswersInsideAndLikeNonexistentOutside(t *testing.T) {
	e, l := newMemoryRecordsEnv(t)
	file := func(scope, key, body, kind, id string) int64 {
		t.Helper()
		got, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.Scope(scope), ScopeKey: key, Body: body, Provenance: learnings.ProvenanceObserved}, learnings.Actor{Kind: kind, ID: id})
		if ref != nil {
			t.Fatalf("seed %s: %s", body, ref.Reason)
		}
		return got.ID
	}
	own := file("agent", "implementer", "my own retry policy note", "agent", "implementer")
	person := file("person", "operator", "the person's deploy rule", "user", "person")
	sibling := file("agent", "sibling", "the sibling's private memory about the payroll system", "agent", "sibling")
	unrunRoot := file("project", "/repo/org-group", "a project memory under a root implementer never ran in", "user", "person")

	hits, ref := l.MineQuery(e.ctx, "implementer", learnings.MineFilter{})
	if ref != nil {
		t.Fatalf("MineQuery: %s", ref.Reason)
	}
	bodies := map[string]bool{}
	for _, h := range hits {
		bodies[h.Learning.Body] = true
	}
	for _, want := range []string{"my own retry policy note", "the person's deploy rule"} {
		if !bodies[want] {
			t.Errorf("readable set is missing %q", want)
		}
	}
	for _, gone := range []string{"the sibling's private memory about the payroll system", "a project memory under a root implementer never ran in"} {
		if bodies[gone] {
			t.Errorf("readable set carries %q — out of scope must be invisible, not filtered at the edge", gone)
		}
	}

	// show: in-scope answers the ONE renderer's bytes; out-of-scope,
	// retracted and nonexistent answer the SAME sentence.
	_, f, err := e.do(t, "learning_show", `{"id":`+itoa(own)+`}`)
	if err != nil || f != nil {
		t.Fatalf("show of my own id: %v %+v", err, f)
	}
	door, derr := json.Marshal(protocol.AnswerLearningsItem(mustShow(t, l, own)))
	tool, terr := json.Marshal(mustAnswerOf(t, e, "learning_show", itoa(own)))
	if derr != nil || terr != nil {
		t.Fatal(derr, terr)
	}
	if string(door) != string(tool) {
		t.Errorf("show drifted from the shared renderer:\n tool: %s\n door: %s", tool, door)
	}
	_, fOut, _ := e.do(t, "learning_show", `{"id":`+itoa(sibling)+`}`)
	_, fMiss, _ := e.do(t, "learning_show", `{"id":424242}`)
	if !sameNoSuchSentence(fOut, fMiss) {
		t.Errorf("out-of-scope and nonexistent answer differently — that difference is the oracle LM5 refuses:\n %v\n %v", fOut, fMiss)
	}
	if _, fRoot, _ := e.do(t, "learning_show", `{"id":`+itoa(unrunRoot)+`}`); !sameNoSuchSentence(fRoot, fMiss) {
		t.Errorf("a root never run in answers %v, want the no-such sentence", fRoot)
	}
	// A person's retract makes it history, not memory: same sentence.
	if _, _, ref := l.Retract(person, "superseded", learnings.Actor{Kind: "user", ID: "person"}); ref != nil {
		t.Fatalf("seed retract: %+v", ref)
	}
	if _, fRet, _ := e.do(t, "learning_show", `{"id":`+itoa(person)+`}`); !sameNoSuchSentence(fRet, fMiss) {
		t.Errorf("a retracted row answers %v, want the no-such sentence (LM2's live-view law)", fRet)
	}
}

func TestProposeRetractFilesTheInertAskAndRefusesWhatItCannotSee(t *testing.T) {
	e, l := newMemoryRecordsEnv(t)
	own, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "implementer", Body: "the stale flake note I now want taken back", Provenance: learnings.ProvenanceAssumed}, learnings.Actor{Kind: "agent", ID: "implementer"})
	if ref != nil {
		t.Fatalf("seed: %s", ref.Reason)
	}
	sib, ref := l.AppendAs(learnings.AppendInput{Scope: learnings.ScopeAgent, ScopeKey: "sibling", Body: "not mine to ask about", Provenance: learnings.ProvenanceObserved}, learnings.Actor{Kind: "agent", ID: "sibling"})
	if ref != nil {
		t.Fatalf("seed: %s", ref.Reason)
	}

	out, f, err := e.do(t, "learning_propose_retract", `{"id":`+itoa(own.ID)+`,"reason":"superseded by the cache fix"}`)
	if err != nil || f != nil {
		t.Fatalf("propose: %v %+v", err, f)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("propose answered %T, want protocol.PolicyProposeResult", out)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("the inert note did not travel: %q", res.Note)
	}
	if res.Proposal.Kind != string(desk.KindLearningRetract) {
		t.Errorf("kind %q, want %s", res.Proposal.Kind, desk.KindLearningRetract)
	}
	var p desk.LearningRetractProposal
	if err := json.Unmarshal(res.Proposal.Payload, &p); err != nil || p.ID != own.ID || p.Reason != "superseded by the cache fix" {
		t.Errorf("payload %s did not carry the ask verbatim (%v)", res.Proposal.Payload, err)
	}

	// INERT, measured: nothing stamped, nothing audited as a retraction.
	lr, _, err := l.Show(own.ID)
	if err != nil || lr.Retracted {
		t.Fatalf("the row retracted at PROPOSE time (retracted=%v, %v) — an ask is not an act (LM7)", lr.Retracted, err)
	}
	hist, err := l.History(own.ID)
	if err != nil || len(hist) != 1 || hist[0].Op != "add" {
		t.Fatalf("audit career = %v (%v), want only the add", hist, err)
	}

	// Out of sight, out of ask: the sibling's row answers the SAME
	// no-such sentence show answers — an ask about what you cannot see is
	// an ask about nothing, and the two are indistinguishable by design.
	_, fSib, _ := e.do(t, "learning_propose_retract", `{"id":`+itoa(sib.ID)+`,"reason":"not mine"}`)
	_, fMiss, _ := e.do(t, "learning_propose_retract", `{"id":424242,"reason":"nothing"}`)
	if !sameNoSuchSentence(fSib, fMiss) {
		t.Errorf("out-of-scope and nonexistent asks answer differently (%v vs %v) — LM5's law covers the propose too", fSib, fMiss)
	}
	// The desk shape half still gates the payload: a blank reason and a
	// reason longer than the stamp column's own bound never become a row.
	for _, bad := range []string{`{"id":` + itoa(own.ID) + `,"reason":""}`, `{"id":` + itoa(own.ID) + `,"reason":"` + strings.Repeat("r", 257) + `"}`} {
		if _, f, _ := e.do(t, "learning_propose_retract", bad); f == nil {
			t.Errorf("malformed ask %q became a row instead of a refusal", bad)
		}
	}
}

// ── small test aids (kept local: the harness helpers above are the shared
// ones; these three know only about the learnings plane) ─────────────────

// sameNoSuchSentence compares the no-such sentences modulo the id each
// one quotes: LM5's law is ONE sentence, and the id is its only variable
// word. Any other difference — a different shape, a different explanation
// — is the oracle this test exists to catch.
func sameNoSuchSentence(a, b *Failure) bool {
	const head = "refused: no such learning with id "
	if a == nil || b == nil {
		return false
	}
	if !strings.HasPrefix(a.Sentence, head) || !strings.HasPrefix(b.Sentence, head) {
		return false
	}
	ta := a.Sentence[strings.Index(a.Sentence, " — "):]
	tb := b.Sentence[strings.Index(b.Sentence, " — "):]
	return ta == tb
}

func mustShow(t *testing.T, l *learnings.Learnings, id int64) learnings.Learning {
	t.Helper()
	lr, found, err := l.Show(id)
	if err != nil || !found {
		t.Fatalf("show %d: %v %v", id, found, err)
	}
	return lr
}

func mustAnswerOf(t *testing.T, e recordsEnv, tool, id string) any {
	t.Helper()
	out, f, err := e.do(t, tool, `{"id":`+id+`}`)
	if err != nil || f != nil {
		t.Fatalf("%s %s: %v %+v", tool, id, err, f)
	}
	return out
}
