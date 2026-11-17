package store

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// agent_message_test.go pins S70 T1 (AT5, AT6 and the store half of AT3):
// a thread is its own header, an answer is a link and never an edit, the
// delivery stamps once, the caps refuse naming both sides, and there is no
// update or delete surface on the mailbox at all — rows are facts.

func mailboxScope(t *testing.T) (*Store, Scope) {
	t.Helper()
	s := openTemp(t)
	return s, seedLoop(t, s, "mailbox", "/repo")
}

func TestAT5ThreadIsItsOwnHeaderAndAnAnswerIsALink(t *testing.T) {
	s, sc := mailboxScope(t)
	st := seedStep(t, s, sc)
	callee := seedStep(t, s, sc)

	ask, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "reviewer", ToAgent: "author", FromStepID: st.ID,
		Body: "is the risk section current?", ExpectReply: true,
	})
	if err != nil {
		t.Fatalf("queue the ask: %v", err)
	}
	if ask.Thread != ask.ID {
		t.Errorf("a thread's first row is %d but carries thread %d — a conversation is its own header", ask.ID, ask.Thread)
	}

	// The answer inherits the thread from the message it answers (the one
	// honest inheritance) and lands linked, not replacing.
	answer, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "author", ToAgent: "reviewer", FromStepID: callee.ID,
		Body: "rewritten this morning", ResultOf: ask.ID,
	})
	if err != nil {
		t.Fatalf("queue the answer: %v", err)
	}
	if answer.Thread != ask.Thread || answer.ResultOf != ask.ID {
		t.Errorf("answer carries thread %d result_of %d — want %d and %d", answer.Thread, answer.ResultOf, ask.Thread, ask.ID)
	}
	if answer.ExpectReply {
		t.Error("an answer that also expects an answer is two messages, not one (the CHECK pair)")
	}

	// A thread from another loop answers nothing (the refusal names both
	// sides: the thread and the loop). The authoring step lives in THAT
	// loop, so what refuses is the thread, not the author.
	other := seedLoop(t, s, "elsewhere", "/other")
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: other.LoopSlug, ComponentRoot: other.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: seedStep(t, s, other).ID,
		Body: "borrowed thread", Thread: ask.Thread,
	}); err == nil || !strings.Contains(err.Error(), strconv.FormatInt(ask.Thread, 10)) || !strings.Contains(err.Error(), other.LoopSlug) {
		t.Errorf("cross-loop thread join: %v — want a refusal naming the thread and the loop", err)
	}

	// And a step cannot author FOR another loop either, even with a valid
	// thread of its own loop.
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: other.LoopSlug, ComponentRoot: other.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: st.ID,
		Body: "speaking for a loop I am not in", ExpectReply: true,
	}); err == nil || !strings.Contains(err.Error(), "belongs to") {
		t.Errorf("cross-loop authoring: %v — want the step's real loop named", err)
	}

	// Answering a message that expected nothing refuses naming it.
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "reviewer", ToAgent: "author", FromStepID: st.ID,
		Body: "answering an answer", ResultOf: answer.ID,
	}); err == nil || !strings.Contains(err.Error(), "did not expect an answer") {
		t.Errorf("answering a non-asking message: %v — want the refusal to name the expectation", err)
	}

	// A mismatched thread on an answer refuses naming both threads.
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "author", ToAgent: "reviewer", FromStepID: callee.ID,
		Body: "wrong thread", Thread: ask.ID + 1000, ResultOf: ask.ID,
	}); err == nil || !strings.Contains(err.Error(), "lives in thread") {
		t.Errorf("thread mismatch on an answer: %v — want both threads named", err)
	}

	// The conversation reads start to answer, and a missing thread is
	// ANSWERED, not silently empty.
	conv, err := s.ThreadAgentMessages(sc.ComponentRoot, sc.LoopSlug, ask.Thread)
	if err != nil || len(conv) != 2 || conv[0].ID != ask.ID || conv[1].ID != answer.ID {
		t.Fatalf("read the thread back: %d rows, err %v — want ask then answer, in order", len(conv), err)
	}
	if _, err := s.ThreadAgentMessages(sc.ComponentRoot, sc.LoopSlug, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing thread answered %v — want ErrNotFound naming it", err)
	}
}

func TestAT5DeliveryStampsExactlyOnce(t *testing.T) {
	s, sc := mailboxScope(t)
	st := seedStep(t, s, sc)
	m, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: st.ID,
		Body: "drain me", ExpectReply: true,
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if m.DeliveredAt != "" {
		t.Fatal("a queued message arrived already delivered")
	}
	got, err := s.DeliverAgentMessage(m.ID, st.ID)
	if err != nil || got.DeliveredAt == "" {
		t.Fatalf("deliver: %v — want the stamp", err)
	}
	if got.DeliveredToStepID != st.ID {
		t.Errorf("the delivery is half-stamped: received by step %d, want %d — the pair lands together or not at all",
			got.DeliveredToStepID, st.ID)
	}
	if _, err := s.DeliverAgentMessage(m.ID, st.ID); err == nil || !strings.Contains(err.Error(), "again") || !strings.Contains(err.Error(), got.DeliveredAt) {
		t.Errorf("double delivery: %v — want a refusal naming the row and its stamp", err)
	}
	if _, err := s.DeliverAgentMessage(424242, st.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delivering a missing row answered %v — want ErrNotFound naming the id", err)
	}
}

// TestTheMailboxCodecHasNoUpdateOrDeleteSurface is AT5's compile-time half
// made runtime: the table's exported surface is queue, deliver, and reads —
// no method may UPDATE, DELETE, SET or CANCEL a mailbox row. Rows are facts;
// the bootstrap and the delivery stamp are transactions inside Queue and
// Deliver, not verbs a caller can reach.
func TestTheMailboxCodecHasNoUpdateOrDeleteSurface(t *testing.T) {
	typ := reflect.TypeOf(&Store{})
	found := map[string]bool{}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "agentmessage") && !strings.Contains(lower, "mailbox") && !strings.Contains(lower, "pendingforagent") {
			continue
		}
		for _, bad := range []string{"update", "delete", "set", "cancel", "remove", "edit"} {
			if strings.HasPrefix(lower, bad) {
				t.Errorf("the mailbox exposes %s — rows are facts; there is no %s verb", name, bad)
			}
		}
		found[lower] = true
	}
	for _, want := range []string{"queueagentmessage", "deliveragentmessage", "threadagentmessages", "loopagentmessages", "pendingforagent"} {
		if !found[want] {
			t.Errorf("the mailbox surface lost %s", want)
		}
	}
}

func TestAT6TheCapsRefuseNamingBothSides(t *testing.T) {
	// Body: 32 KiB + 1, refused naming the size, the cap, and where large
	// payloads belong.
	s, sc := mailboxScope(t)
	st := seedStep(t, s, sc)
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: st.ID,
		Body: strings.Repeat("x", MaxAgentMessageBodyBytes+1), ExpectReply: true,
	}); err == nil || !strings.Contains(err.Error(), "32769") || !strings.Contains(err.Error(), "32768") {
		t.Errorf("oversized body: %v — want the size and the cap both named", err)
	}

	// Per step: eight messages ride, the ninth from the SAME step refuses
	// naming the step.
	s, sc = mailboxScope(t)
	st = seedStep(t, s, sc)
	for i := 0; i < MaxMessagesPerStep; i++ {
		if _, err := s.QueueAgentMessage(NewAgentMessage{
			LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
			FromAgent: "a", ToAgent: "b", FromStepID: st.ID,
			Body: "ask one turn", ExpectReply: true,
		}); err != nil {
			t.Fatalf("author message %d of the per-step budget: %v", i+1, err)
		}
	}
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: st.ID,
		Body: "the ninth", ExpectReply: true,
	}); err == nil || !strings.Contains(err.Error(), "step") || !strings.Contains(err.Error(), "spam") {
		t.Errorf("ninth message from one step: %v — want the step named", err)
	}

	// Per thread: 16 messages fill it — 8 asks from one step and 8 answers
	// from another — and the 17th from a FRESH step refuses naming the thread
	// and the number.
	s, sc = mailboxScope(t)
	caller, responder, outsider := seedStep(t, s, sc), seedStep(t, s, sc), seedStep(t, s, sc)
	first, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: caller.ID,
		Body: "open", ExpectReply: true,
	})
	if err != nil {
		t.Fatalf("open the thread: %v", err)
	}
	lastAsked := first.ID
	for i := 0; i < 7; i++ {
		if _, err := s.QueueAgentMessage(NewAgentMessage{
			LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
			FromAgent: "b", ToAgent: "a", FromStepID: responder.ID,
			Body: "answer", ResultOf: lastAsked,
		}); err != nil {
			t.Fatalf("answer %d in the thread: %v", i+1, err)
		}
		follow, err := s.QueueAgentMessage(NewAgentMessage{
			LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
			FromAgent: "a", ToAgent: "b", FromStepID: caller.ID,
			Body: "follow-up", Thread: first.Thread, ExpectReply: true,
		})
		if err != nil {
			t.Fatalf("follow-up %d in the thread: %v", i+1, err)
		}
		lastAsked = follow.ID
	}
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "b", ToAgent: "a", FromStepID: responder.ID,
		Body: "final answer", ResultOf: lastAsked,
	}); err != nil {
		t.Fatalf("the sixteenth message in the thread: %v", err)
	}
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "c", ToAgent: "d", FromStepID: outsider.ID,
		Body: "the seventeenth", Thread: first.Thread, ExpectReply: true,
	}); err == nil || !strings.Contains(err.Error(), "thread") || !strings.Contains(err.Error(), "16") {
		t.Errorf("seventeenth message in a thread: %v — want the thread and 16 named", err)
	}

	// Per loop (lifetime bound): eight steps author 8 new threads each = 64;
	// a NINTH fresh step asking for the 65th refuses naming the loop and the
	// cap — the per-step bound cannot have been the one that fired, because
	// this step has authored nothing.
	s, sc = mailboxScope(t)
	authors := []Step{}
	for i := 0; i < 9; i++ {
		authors = append(authors, seedStep(t, s, sc))
	}
	for i := 0; i < 8; i++ {
		for j := 0; j < MaxMessagesPerStep; j++ {
			if _, err := s.QueueAgentMessage(NewAgentMessage{
				LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
				FromAgent: "a", ToAgent: "b", FromStepID: authors[i].ID,
				Body: "a thread of its own", ExpectReply: true,
			}); err != nil {
				t.Fatalf("thread %d of 64 (author %d, message %d): %v", i*MaxMessagesPerStep+j+1, i, j+1, err)
			}
		}
	}
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: authors[8].ID,
		Body: "the 65th thread", ExpectReply: true,
	}); err == nil || !strings.Contains(err.Error(), "loop") || !strings.Contains(err.Error(), "64") {
		t.Errorf("the 65th thread: %v — want the loop and the cap named", err)
	}
}

func TestAT3StoreHalfAddressedNeverSelfNeverEmpty(t *testing.T) {
	s, sc := mailboxScope(t)
	st := seedStep(t, s, sc)
	base := NewAgentMessage{LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: st.ID, Body: "x", ExpectReply: true}

	in := base
	in.FromAgent, in.ToAgent = "same", "same"
	if _, err := s.QueueAgentMessage(in); err == nil || !strings.Contains(err.Error(), "asking itself") {
		t.Errorf("self-ask: %v — want the D8 refusal", err)
	}
	in = base
	in.ToAgent = "  "
	if _, err := s.QueueAgentMessage(in); err == nil || !strings.Contains(err.Error(), "broadcast") {
		t.Errorf("unaddressed: %v — want 'never broadcast'", err)
	}
	in = base
	in.Body = "   "
	if _, err := s.QueueAgentMessage(in); err == nil || !strings.Contains(err.Error(), "empty body") {
		t.Errorf("empty body: %v — want it named", err)
	}
	in = base
	in.FromAgent, in.ToAgent = "a", "b"
	in.FromStepID = 999999
	if _, err := s.QueueAgentMessage(in); !errors.Is(err, ErrNotFound) {
		t.Errorf("message authored by a missing step answered %v — want ErrNotFound naming the step", err)
	}
}

func TestAT5PendingDrainOrderAndScope(t *testing.T) {
	s, sc := mailboxScope(t)
	st := seedStep(t, s, sc)
	other := seedLoop(t, s, "elsewhere", "/other")

	for i, to := range []string{"author", "author", "someone-else"} {
		if _, err := s.QueueAgentMessage(NewAgentMessage{
			LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
			FromAgent: "reviewer", ToAgent: to, FromStepID: st.ID,
			Body: "note", ExpectReply: i == 0,
		}); err != nil {
			t.Fatalf("queue %d: %v", i, err)
		}
	}
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: other.LoopSlug, ComponentRoot: other.ComponentRoot,
		FromAgent: "reviewer", ToAgent: "author", FromStepID: seedStep(t, s, other).ID,
		Body: "other loop", ExpectReply: true,
	}); err != nil {
		t.Fatalf("queue into the other loop: %v", err)
	}

	pending, err := s.PendingForAgent(sc.ComponentRoot, sc.LoopSlug, "author")
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending for author: %d, err %v — want this loop's two, in order", len(pending), err)
	}
	if pending[0].ID > pending[1].ID {
		t.Error("the drain came back out of filing order")
	}
	kept := pending[1].ID
	if _, err := s.DeliverAgentMessage(pending[0].ID, st.ID); err != nil {
		t.Fatalf("deliver the first: %v", err)
	}
	pending, err = s.PendingForAgent(sc.ComponentRoot, sc.LoopSlug, "author")
	if err != nil || len(pending) != 1 || pending[0].ID != kept {
		t.Fatalf("after delivery: %d pending, err %v — want only the undelivered row left", len(pending), err)
	}
	// The whole-loop read sees everything including the other loop's rows are
	// NOT there.
	all, err := s.LoopAgentMessages(sc.ComponentRoot, sc.LoopSlug)
	if err != nil || len(all) != 3 {
		t.Fatalf("the loop mailbox: %d rows, err %v — want three, and never the other loop's", len(all), err)
	}
}

// TestDeliveryPairAndOwedReplies pins T2d's honest to-do lists: a completed
// step asks the ROWS which replies it owes (delivered to THIS step, expected
// an answer, nothing links back to them yet), and a dispatcher sees the
// wait:true asks no boundary has drained. Rows are the state; the answers to
// both questions must be readable from the table alone.
func TestDeliveryPairAndOwedReplies(t *testing.T) {
	s, sc := mailboxScope(t)
	author := seedStep(t, s, sc)
	receiver := seedStep(t, s, sc)
	ask, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "a", ToAgent: "b", FromStepID: author.ID,
		Body: "answer me", ExpectReply: true,
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	// Before delivery: the ask is pending for the dispatcher and owes nothing.
	owed, err := s.RepliesOwedForStep(receiver.ID)
	if err != nil || len(owed) != 0 {
		t.Fatalf("owed before delivery: %d, err %v — nothing is owed until received", len(owed), err)
	}
	asks, err := s.PendingAsksFromStep(author.ID)
	if err != nil || len(asks) != 1 || asks[0].ID != ask.ID {
		t.Fatalf("pending asks: %+v, err %v — want the undelivered wait:true ask", asks, err)
	}
	if _, err := s.DeliverAgentMessage(ask.ID, receiver.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if a, _ := s.PendingAsksFromStep(author.ID); len(a) != 0 {
		t.Error("a delivered ask still reads as pending for the dispatcher")
	}
	if owed, err := s.RepliesOwedForStep(receiver.ID); err != nil || len(owed) != 1 || owed[0].ID != ask.ID {
		t.Fatalf("owed after delivery: %+v, err %v — want the ask", owed, err)
	}
	// The reply settles the list — through the link, not an edit.
	if _, err := s.QueueAgentMessage(NewAgentMessage{
		LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		FromAgent: "b", ToAgent: "a", FromStepID: receiver.ID,
		Body: "answered", ResultOf: ask.ID,
	}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	if owed, _ := s.RepliesOwedForStep(receiver.ID); len(owed) != 0 {
		t.Errorf("the reply did not settle the owed list: %+v — the link is the settlement", owed)
	}
	// The dispatcher's running question answers from rows (P33's shape): the
	// seeded steps are running, and they speak for their agent.
	if running, err := s.RunningStepForAgent(sc.ComponentRoot, sc.LoopSlug, "golang-engineer"); err != nil || !running {
		t.Errorf("running step for the seeded agent: %v, err %v — the seeded steps speak for it", running, err)
	}
	if running, err := s.RunningStepForAgent(sc.ComponentRoot, sc.LoopSlug, "nobody"); err != nil || running {
		t.Errorf("running step for nobody: %v, err %v — want false", running, err)
	}
}
