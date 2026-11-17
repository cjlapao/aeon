package store

import (
	"strings"
	"testing"
)

// T7's tests. The transactional pairing is the property that matters.

func TestAskWritesTheQuestionAndItsThreadEntryInOneTransaction(t *testing.T) {
	// D16. A question with no thread entry is a loop waiting on something the
	// user cannot see; a thread entry with no question is a prompt nobody can
	// answer. Both are silent, and neither errors.
	s := openTemp(t)
	sc := seedLoop(t, s, "pairing", "/repo")

	q, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "proceed to build?"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if q.ID == 0 {
		t.Fatal("no question id was allocated")
	}

	thread, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread) != 1 {
		t.Fatalf("the thread holds %d entries, want 1", len(thread))
	}
	if thread[0].QuestionID == nil || *thread[0].QuestionID != q.ID {
		t.Errorf("the thread entry does not point at the question: %+v", thread[0])
	}
	if thread[0].Kind != "question" || thread[0].Author != "agent" {
		t.Errorf("the thread entry is %s/%s, want agent/question", thread[0].Author, thread[0].Kind)
	}
}

func TestAForcedFailureMidAskLeavesNeitherRow(t *testing.T) {
	// D16's negative half, and the only honest way to check it: force the
	// SECOND write to fail and assert the first is gone.
	//
	// A trigger that aborts every question-kind thread insert is the forcing
	// mechanism. It is exactly what a broken second write looks like from
	// inside the transaction, and unlike a seq collision it cannot be absorbed
	// by the retry — which is what makes it a test of the TRANSACTION rather
	// than of the retry.
	s := openTemp(t)
	sc := seedLoop(t, s, "atomic-ask", "/repo")

	if _, err := s.w.Exec(`
		CREATE TRIGGER refuse_question_thread BEFORE INSERT ON chat_message
		WHEN NEW.kind = 'question'
		BEGIN SELECT RAISE(ABORT, 'forced failure in the thread insert'); END`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = s.w.Exec(`DROP TRIGGER refuse_question_thread`) }()

	_, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "this must not land"})
	if err == nil {
		t.Fatal("Ask succeeded although its thread entry could not be written")
	}
	if !strings.Contains(err.Error(), "thread entry") {
		t.Errorf("the error does not name what failed: %v", err)
	}

	if got := countRows(t, s, `SELECT count(*) FROM question`); got != 0 {
		t.Errorf("%d questions survived a failed Ask. A question with no thread entry is a loop waiting "+
			"on something the user cannot see, and it errors nowhere", got)
	}
	if got := countRows(t, s, `SELECT count(*) FROM chat_message`); got != 0 {
		t.Errorf("%d thread entries exist after the failure", got)
	}

	// The negative control: with the trigger gone, the same call must work —
	// otherwise this test would pass against an Ask that never writes anything.
	if _, err := s.w.Exec(`DROP TRIGGER refuse_question_thread`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "this one lands"}); err != nil {
		t.Fatalf("Ask fails even without the forced failure: %v", err)
	}
	if got := countRows(t, s, `SELECT count(*) FROM question`); got != 1 {
		t.Errorf("after the trigger was dropped, %d questions exist, want 1", got)
	}
}

func TestAnswerSetsTheAnswerAndAppendsAThreadEntry(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "answering", "/repo")
	q, err := s.Ask(sc, NewQuestion{Kind: QuestionClarify, Body: "which branch?"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.Answer(q.ID, "main")
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if !res.OK {
		t.Fatalf("the answer was refused: %s", res.Reason)
	}
	if res.Question.Answer != "main" {
		t.Errorf("the answer is %q", res.Question.Answer)
	}
	if !res.Question.Answered() {
		t.Error("answered_at was not set, so IsAwaiting would still report the loop as waiting")
	}

	thread, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread) != 2 {
		t.Fatalf("the thread holds %d entries, want the question and the answer", len(thread))
	}
	if thread[1].Kind != "answer" || thread[1].Author != "human" {
		t.Errorf("the answer entry is %s/%s", thread[1].Author, thread[1].Kind)
	}
}

func TestAnsweringAnAlreadyAnsweredQuestionIsRefusedNamingTheExistingTimestamp(t *testing.T) {
	// Answering twice is a RACE, not an edit. The first answer may already have
	// resumed the step, so a second is refused — and the refusal names when the
	// first landed, because "already answered" without a time leaves the caller
	// unable to tell a double submit from a stale window.
	s := openTemp(t)
	sc := seedLoop(t, s, "twice", "/repo")
	q, err := s.Ask(sc, NewQuestion{Kind: QuestionFree, Body: "anything?"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Answer(q.ID, "yes")
	if err != nil || !first.OK {
		t.Fatalf("the first answer did not land: %v %+v", err, first)
	}

	second, err := s.Answer(q.ID, "actually no")
	if err != nil {
		t.Fatalf("a refused answer returned an error: %v", err)
	}
	if second.OK {
		t.Fatal("the second answer was applied, so the resumed step would act on a value the person " +
			"believes they replaced")
	}
	if !strings.Contains(second.Reason, first.Question.AnsweredAt) {
		t.Errorf("the refusal does not name the existing timestamp (%s): %s",
			first.Question.AnsweredAt, second.Reason)
	}
	if second.Question.Answer != "yes" {
		t.Errorf("the stored answer changed to %q", second.Question.Answer)
	}
}

func TestPendingQuestionReturnsTheOldestUnansweredQuestion(t *testing.T) {
	// OLDEST, not newest: questions are answered in the order asked, and a
	// newest-first reader strands the first one indefinitely — which looks like
	// a loop that is stuck for no reason.
	s := openTemp(t)
	sc := seedLoop(t, s, "oldest", "/repo")

	var ids []int64
	for _, body := range []string{"first", "second", "third"} {
		q, err := s.Ask(sc, NewQuestion{Kind: QuestionFree, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, q.ID)
	}

	pending, ok, err := s.PendingQuestion(sc)
	if err != nil || !ok {
		t.Fatalf("no pending question with three outstanding: %v", err)
	}
	if pending.ID != ids[0] || pending.Body != "first" {
		t.Errorf("the pending question is %q (id %d), want the first", pending.Body, pending.ID)
	}

	// Answering it hands over to the next, in order.
	if _, err := s.Answer(ids[0], "ok"); err != nil {
		t.Fatal(err)
	}
	pending, ok, err = s.PendingQuestion(sc)
	if err != nil || !ok {
		t.Fatalf("no pending question after answering the first: %v", err)
	}
	if pending.Body != "second" {
		t.Errorf("the next pending question is %q", pending.Body)
	}
}

func TestIsAwaitingAgreesWithPendingQuestionAcrossEveryState(t *testing.T) {
	// All FIVE states (D15). A two-case test passes against an implementation
	// returning `count > 0`, which is wrong in exactly the case that matters:
	// several questions with one unanswered.
	cases := []struct {
		name     string
		ask      int
		answer   int
		awaiting bool
	}{
		{"no questions", 0, 0, false},
		{"one unanswered", 1, 0, true},
		{"one answered", 1, 1, false},
		{"several, one unanswered", 3, 2, true},
		{"several, all answered", 3, 3, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			sc := seedLoop(t, s, "awaiting", "/repo")

			var ids []int64
			for i := 0; i < tc.ask; i++ {
				q, err := s.Ask(sc, NewQuestion{Kind: QuestionFree, Body: "q"})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, q.ID)
			}
			for i := 0; i < tc.answer; i++ {
				if _, err := s.Answer(ids[i], "a"); err != nil {
					t.Fatal(err)
				}
			}

			awaiting, err := s.IsAwaiting(sc)
			if err != nil {
				t.Fatal(err)
			}
			_, hasPending, err := s.PendingQuestion(sc)
			if err != nil {
				t.Fatal(err)
			}

			if awaiting != tc.awaiting {
				t.Errorf("IsAwaiting is %v, want %v", awaiting, tc.awaiting)
			}
			if awaiting != hasPending {
				t.Errorf("IsAwaiting says %v and PendingQuestion says %v; the two must never disagree, "+
					"because nothing would say which is right", awaiting, hasPending)
			}
		})
	}
}

func TestAwaitingIsScopedToItsLoop(t *testing.T) {
	// A question in one component must not make another component's loop of the
	// same name look like it is waiting.
	s := openTemp(t)
	a := seedLoop(t, s, "refactor", "/repo-a")
	b := seedLoop(t, s, "refactor", "/repo-b")
	if _, err := s.Ask(a, NewQuestion{Kind: QuestionGate, Body: "proceed?"}); err != nil {
		t.Fatal(err)
	}

	if awaiting, _ := s.IsAwaiting(a); !awaiting {
		t.Error("/repo-a is not awaiting, although it has an unanswered question")
	}
	if awaiting, _ := s.IsAwaiting(b); awaiting {
		t.Error("/repo-b is awaiting because of a question asked in /repo-a")
	}

	loops, err := s.AwaitingLoops(ComponentScope{ComponentRoot: "/repo-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 0 {
		t.Errorf("AwaitingLoops in /repo-b returned %+v", loops)
	}
	loops, err = s.AwaitingLoops(ComponentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 {
		t.Errorf("AwaitingLoops across every component returned %d, want 1", len(loops))
	}
}

func TestToolUseIDIsNullForAGateAndSetForAPermissionPrompt(t *testing.T) {
	// Nullable on purpose. A permission prompt comes from a tool call and has
	// one; a phase gate is asked by the loop driver and does not. A NOT NULL
	// column would force the driver to fabricate an id — and a fabricated id is
	// one some resume path will eventually try to match.
	s := openTemp(t)
	sc := seedLoop(t, s, "toolids", "/repo")

	gate, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "proceed?"})
	if err != nil {
		t.Fatal(err)
	}
	if gate.ToolUseID != "" {
		t.Errorf("a gate carries tool_use_id %q", gate.ToolUseID)
	}
	var raw *string
	if err := s.r.QueryRow(`SELECT tool_use_id FROM question WHERE id = ?`, gate.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Errorf("a gate stored tool_use_id as %q rather than NULL; '' and absent are different facts", *raw)
	}

	perm, err := s.Ask(sc, NewQuestion{Kind: QuestionPermission, ToolUseID: "toolu_01",
		Body: "run `rm -rf build`?", Options: []string{"allow once", "always", "deny"}})
	if err != nil {
		t.Fatal(err)
	}
	if perm.ToolUseID != "toolu_01" {
		t.Errorf("a permission prompt lost its tool_use_id: %q", perm.ToolUseID)
	}
	if len(perm.Options) != 3 {
		t.Errorf("the options came back as %v", perm.Options)
	}
}

func TestAFreeFormQuestionHasNoOptions(t *testing.T) {
	// NULL rather than an empty array: "no options were offered" and "an empty
	// list of options" render differently, and only the first is a real state.
	s := openTemp(t)
	sc := seedLoop(t, s, "freeform", "/repo")
	q, err := s.Ask(sc, NewQuestion{Kind: QuestionFree, Body: "what next?"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Options != nil {
		t.Errorf("a free-form question came back with options %v", q.Options)
	}
	var raw *string
	if err := s.r.QueryRow(`SELECT options FROM question WHERE id = ?`, q.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Errorf("options stored as %q rather than NULL", *raw)
	}
}

func TestTheStoreAssignsChatMessageSeqAndTheCallerCannot(t *testing.T) {
	// O3. NewChatMessage has no Seq field — the caller has nowhere to put one.
	s := openTemp(t)
	sc := seedLoop(t, s, "threadseq", "/repo")

	for i, body := range []string{"one", "two", "three"} {
		m, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: body})
		if err != nil {
			t.Fatal(err)
		}
		if m.Seq != int64(i+1) {
			t.Errorf("entry %d got seq %d, want %d", i, m.Seq, i+1)
		}
	}
}

func TestASeqCollisionRetriesAndSucceeds(t *testing.T) {
	// Two goroutines append here — a runner and the server — so a collision is
	// expected occasionally. The UNIQUE constraint catches the loser and the
	// write retries with a recomputed seq, bounded.
	//
	// The collision is FORCED: a row is planted at the seq the store is about
	// to compute, so the first attempt must lose.
	s := openTemp(t)
	sc := seedLoop(t, s, "collision", "/repo")

	if _, err := s.w.Exec(`INSERT INTO chat_message (loop_slug, component_root, seq, author, kind, body, created_at)
		VALUES (?,?,1,'system','note','planted at seq 1','t')`, sc.LoopSlug, sc.ComponentRoot); err != nil {
		t.Fatal(err)
	}

	m, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: "after the collision"})
	if err != nil {
		t.Fatalf("the append did not recover from a collision: %v", err)
	}
	if m.Seq != 2 {
		t.Errorf("the recovered append took seq %d, want 2", m.Seq)
	}

	thread, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread) != 2 {
		t.Errorf("the thread holds %d entries, want 2 — a collision must not lose a message", len(thread))
	}
}

func TestDeletingAQuestionNullsItsThreadEntry(t *testing.T) {
	// ON DELETE SET NULL, not CASCADE: the thread is what a person READ, and
	// deleting a question should not delete the record of having been asked.
	s := openTemp(t)
	sc := seedLoop(t, s, "setnull", "/repo")
	q, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "proceed?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`DELETE FROM question WHERE id = ?`, q.ID); err != nil {
		t.Fatal(err)
	}

	thread, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread) != 1 {
		t.Fatalf("the thread entry disappeared with its question: %d entries", len(thread))
	}
	if thread[0].QuestionID != nil {
		t.Errorf("question_id is still %d after the question was deleted", *thread[0].QuestionID)
	}
	if thread[0].Body != "proceed?" {
		t.Errorf("the thread entry lost its body: %q", thread[0].Body)
	}
}

func TestThreadReturnsSeqOrderAndHonoursTheAfterCursor(t *testing.T) {
	// The same cursor shape as the event stream, so a reader that knows one
	// knows the other.
	s := openTemp(t)
	sc := seedLoop(t, s, "cursor", "/repo")
	for _, body := range []string{"one", "two", "three", "four"} {
		if _, err := s.Note(sc, NewChatMessage{Author: "human", Kind: "note", Body: body}); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("got %d entries", len(all))
	}
	for i, m := range all {
		if m.Seq != int64(i+1) {
			t.Errorf("entry %d has seq %d", i, m.Seq)
		}
	}

	tail, err := s.Thread(sc, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || tail[0].Body != "three" {
		t.Errorf("after=2 returned %d entries starting at %q", len(tail), tail[0].Body)
	}
}

func TestAQuestionNeedsABody(t *testing.T) {
	// An empty question is a prompt with nothing to read, and it would suspend
	// a loop on a blank screen.
	s := openTemp(t)
	sc := seedLoop(t, s, "empty-q", "/repo")
	if _, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "   "}); err == nil {
		t.Fatal("a question with a blank body was accepted")
	}
}

func TestTheAwaitingSQLIsTheQueryTheStoreActuallyRuns(t *testing.T) {
	// `aeon db query awaiting` PRINTS this string, so anyone arriving from v1
	// can see why there is no column. A second copy in the CLI would
	// eventually print something the store does not run — so the constant is
	// the query, and this asserts it still runs.
	s := openTemp(t)
	sc := seedLoop(t, s, "published-sql", "/repo")
	if _, err := s.Ask(sc, NewQuestion{Kind: QuestionGate, Body: "proceed?"}); err != nil {
		t.Fatal(err)
	}
	var one int
	if err := s.r.QueryRow(AwaitingSQL, sc.ComponentRoot, sc.LoopSlug).Scan(&one); err != nil {
		t.Fatalf("the published SQL does not run: %v", err)
	}
	if !strings.Contains(AwaitingSQL, "answered_at IS NULL") {
		t.Error("the published SQL does not show the derivation, which is the reason it is published")
	}
}

func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
