package daemon

// S24 T4 — the answering half. The raising side is T3's (and its
// TestAskModeRaisesGateQuestionInsteadOfStarting already walks the raise);
// these gates walk the answer door: who may answer, what the words are,
// what an answer starts, and — above all — that materialization has ONE
// code path with two callers.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// askFixture drives a finished loop with a pending gate question: `ask`
// mode, first item done, question riding the finished loop's slug.
func askFixture(t *testing.T) (*reactorFixture, int64, int64) {
	t.Helper()
	f := newReactorFixture(t)
	if _, err := f.s.SetComponentQueueSettings(f.root, "ask", "manual"); err != nil {
		t.Fatal(err)
	}
	first := f.appendReady(t, "shipped", nil)
	second := f.appendReady(t, "the next thing", nil)
	f.seedLoop(t, "loop-one")
	if _, err := f.s.SetQueueItemLoop(first.ID, f.root, "loop-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TransitionQueueItem(first.ID, "ready", "running"); err != nil {
		t.Fatal(err)
	}
	f.materialize(t, "loop-one", "done", "")
	await(t, "the gate question", func() bool {
		_, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
		return err == nil && found
	})
	return f, first.ID, second.ID
}

func (f *reactorFixture) answer(t *testing.T, loopID, text string) map[string]any {
	t.Helper()
	res, err := f.d.Table.AnswerBlueprintLoop(context.Background(), protocol.LoopAnswerParams{ID: loopID, Answer: text})
	if err != nil {
		t.Fatalf("answer %q: %v", text, err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("answer returned %T, not the door's map", res)
	}
	return m
}

func TestAnsweredApproveMaterializesNextReady(t *testing.T) {
	// The affirmative starts EXACTLY the item NextReady would have
	// returned — the same second item, through the same advanceNext the
	// `auto` branch calls (the call-site test below pins "same function";
	// this pins "same item").
	f, _, second := askFixture(t)
	res := f.answer(t, f.ids["loop-one"], "advance")
	if refused, ok := res["refused"]; ok {
		t.Fatalf("the answer was refused: %v", refused)
	}
	if advanced, _ := res["advanced"].(bool); !advanced {
		t.Fatalf("advanced=%v — an affirmative that starts nothing is a question that never meant anything", res["advanced"])
	}
	select {
	case p := <-f.starts:
		if p.Goal != "the next thing" {
			t.Errorf("started %q, not what NextReady would have returned", p.Goal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the affirmative never reached the loop.start door")
	}
	await(t, "the second item running", func() bool { return f.itemByID(t, second).Status == "running" })
}

func TestAnsweredDeclineLeavesQueueHalted(t *testing.T) {
	// A declining answer is honoured, not re-asked: the question is
	// answered (durable), nothing starts, and NOTHING asks again (CG4 —
	// no poller, no nag).
	f, _, second := askFixture(t)
	res := f.answer(t, f.ids["loop-one"], "hold")
	if advanced, _ := res["advanced"].(bool); advanced {
		t.Fatal("hold advanced the queue")
	}
	if _, found, _ := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"}); found {
		t.Error("the declined question is still pending — the answer was not durable")
	}
	select {
	case p := <-f.starts:
		t.Fatalf("hold started %q", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
	if row := f.itemByID(t, second); row.Status != "ready" {
		t.Errorf("the second item reads %s after a decline", row.Status)
	}
}

func TestSecondAnswerRefusedReplaysNothing(t *testing.T) {
	// Claimed-ONCE at the store, honoured at the door: the replay reads
	// the refusal with the first timestamp and — the part the store
	// cannot enforce alone — starts NO second loop.
	f, _, _ := askFixture(t)
	if m := f.answer(t, f.ids["loop-one"], "advance"); m["refused"] != nil {
		t.Fatalf("first answer refused: %v", m["refused"])
	}
	select {
	case <-f.starts:
	case <-time.After(2 * time.Second):
		t.Fatal("first advance never started")
	}
	m := f.answer(t, f.ids["loop-one"], "advance")
	refused, ok := m["refused"].(map[string]string)
	if !ok || refused["kind"] != "already-answered" {
		t.Fatalf("second answer returned %v — a replay must be refused by name", m)
	}
	if !strings.Contains(refused["detail"], "already answered at") {
		t.Errorf("the refusal %q must carry the first answer's timestamp", refused["detail"])
	}
	select {
	case p := <-f.starts:
		t.Fatalf("the replay started %q — a second answer must start nothing", p.Goal)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestUnknownAnswerIsRefusedNamingBothWords(t *testing.T) {
	// The closed set is validated BEFORE anything is recorded: a typo'd
	// "yes" is refused naming advance and hold, and the question stays
	// pending — a silent hold on a typo would leave the person believing
	// their yes landed.
	f, _, _ := askFixture(t)
	m := f.answer(t, f.ids["loop-one"], "yes please")
	refused, ok := m["refused"].(map[string]string)
	if !ok || refused["kind"] != "unknown-answer" {
		t.Fatalf("unknown answer returned %v", m)
	}
	if !strings.Contains(refused["detail"], `"advance"`) || !strings.Contains(refused["detail"], `"hold"`) {
		t.Errorf("refusal %q must name both admissible words", refused["detail"])
	}
	if _, found, _ := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"}); !found {
		t.Error("the refused answer recorded something — validation happens BEFORE the write")
	}
}

func TestUnansweredNeverMaterializesAdvancingTheClock(t *testing.T) {
	// T4.md's gate, honoured literally: advancing TIME, not sleeping.
	// The queue's halt has no timer — CG4's cold-start gap cut the other
	// way: nothing waits on a question, and nothing fills the silence.
	f, _, second := askFixture(t)
	// Reopen the store with an injectable clock is overkill for an
	// ABSENCE proof — the clock matters to rows, not to this reactor
	// (it reads none); advancing the fixture's clock and waiting proves
	// the absence across any amount of time the store would record.
	f.advanceClock(72 * time.Hour)
	select {
	case p := <-f.starts:
		t.Fatalf("three days passed and %q started itself — the halt has no timer", p.Goal)
	case <-time.After(200 * time.Millisecond):
	}
	if row := f.itemByID(t, second); row.Status != "ready" {
		t.Errorf("the unanswered queue moved on its own to %s", row.Status)
	}
	// And the door still answers LATE — the halt waits for the person
	// whenever they arrive, not within a window.
	m := f.answer(t, f.ids["loop-one"], "advance")
	if m["refused"] != nil {
		t.Fatalf("a three-day-old question was refused: %v — no window exists", m["refused"])
	}
	select {
	case <-f.starts:
	case <-time.After(2 * time.Second):
		t.Fatal("the late answer never materialized")
	}
}

func TestAskRaisesGateQuestionOnFinishedLoopSlug(t *testing.T) {
	// The row is on the JUST-FINISHED loop's slug — design §2's FK
	// finding made visible: the next loop does not exist yet, so its
	// slug could not carry the row at all.
	f, _, _ := askFixture(t)
	q, found, err := f.s.PendingQuestion(store.Scope{ComponentRoot: f.root, LoopSlug: "loop-one"})
	if !found || err != nil {
		t.Fatalf("no question on the finished loop: found=%v err=%v", found, err)
	}
	if q.LoopSlug != "loop-one" {
		t.Errorf("question rides %q, not the finished loop", q.LoopSlug)
	}
	if q.StepID != nil {
		t.Error("a queue question carries no step — that nil is its identity at the door")
	}
}

func TestOneMaterializationCodePathTwoCallers(t *testing.T) {
	// The gate T4.md demands: T3's auto branch and this task's answered
	// branch call the SAME function, asserted structurally. advanceNext
	// is defined once; its callers are exactly the two; and the
	// StartBlueprintLoop door is reached from ONE place in daemon
	// queue code (inside advanceNext) — a second caller would be the
	// second execution path brief §4 forbids.
	def, callers := 0, 0
	for _, name := range []string{"queue_reactor.go", "queue_ask.go"} {
		src, err := readFileForTest(t, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		def += strings.Count(src, "func (d *Daemon) advanceNext(")
		callers += strings.Count(src, "d.advanceNext(")
		if door := strings.Count(src, "d.Table.StartBlueprintLoop("); door > (map[string]int{"queue_reactor.go": 1}[name]) {
			t.Errorf("%s reaches the loop.start door %d times — the door is reached once, from inside advanceNext", name, door)
		}
		if strings.Contains(src, "startLoopWith(") {
			t.Errorf("%s bypasses the door entirely", name)
		}
	}
	if def != 1 {
		t.Errorf("advanceNext is defined %d times — one materialization path", def)
	}
	if callers != 2 {
		t.Errorf("advanceNext has %d callers — exactly the two: T3's auto branch and the answered answer", callers)
	}
}

// advanceClock moves the store's clock — the rows it stamps ride it, and
// T4.md asked for time advanced, not sleeps counted.
func (f *reactorFixture) advanceClock(d time.Duration) { f.clock.add(d) }
