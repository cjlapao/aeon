package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// ack_test.go — S58 T3 end to end: a real store, the real table, the real
// handlers. The point is that ack/snooze/unack behave as SV3/SV12/P3 say
// THROUGH THE SOCKET — actor from the peer, ghosts refused, counts honest,
// recurrence unbothered by old marks, and NO other row moved.

func asPerson(t *testing.T, d *Daemon, kind, id string) {
	t.Helper()
	d.Table.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
		return protocol.Actor{Kind: kind, ID: id}
	}
}

func seedQuestion(t *testing.T, st *store.Store) store.Question {
	t.Helper()
	if _, err := st.CreateComponent(store.NewComponent{Name: "Alpha", Slug: "alpha", Root: "/w/alpha"}); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "demo", ComponentRoot: "/w/alpha", Goal: "a goal"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	q, err := st.Ask(store.Scope{LoopSlug: "demo", ComponentRoot: "/w/alpha"},
		store.NewQuestion{Kind: "clarify", Body: "which way?", Options: []string{"approve", "deny"}})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	return q
}

func inboxDispatch(t *testing.T, d *Daemon, method string, params string) (any, error) {
	t.Helper()
	return d.Table.Dispatch(context.Background(), "c1", protocol.MethodName(method), json.RawMessage(params))
}

func TestAckMethodsAbsentWithoutAStore(t *testing.T) {
	d, _ := newTestDaemon(t)
	have := map[string]bool{}
	for _, m := range d.Table.PublicMethods() {
		have[string(m)] = true
	}
	for _, m := range []string{"inbox.ack", "inbox.snooze", "inbox.unack"} {
		if have[m] {
			t.Fatalf("%s registered on a storeless daemon (V13: ABSENT, not refusing)", m)
		}
	}
}

func TestAckHidesFromDefaultListAndCountsUnderAcked(t *testing.T) {
	st, _ := openInboxStore(t)
	seedQuestion(t, st)
	d, _ := newTestDaemonWithStore(t, st)
	asPerson(t, d, "user", "1000")

	list, err := inboxDispatch(t, d, "inbox.list", `{}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	key := list.(protocol.InboxListResult).Items[0].Key

	res, err := inboxDispatch(t, d, "inbox.ack", `{"key":"`+key+`"}`)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	ack := res.(protocol.InboxAckResult)
	if ack.Item == nil || ack.Item.Acked == nil || ack.Item.Acked.By != "user:1000" {
		t.Fatalf("ack result = %+v, want the item with by=user:1000", ack)
	}

	// The default view hides it; includeAcked shows it; the count is honest.
	list, _ = inboxDispatch(t, d, "inbox.list", `{}`)
	if n := len(list.(protocol.InboxListResult).Items); n != 0 {
		t.Fatalf("acked item still in the default list (%d items)", n)
	}
	list, _ = inboxDispatch(t, d, "inbox.list", `{"includeAcked":true}`)
	items := list.(protocol.InboxListResult).Items
	if len(items) != 1 || items[0].Acked == nil {
		t.Fatalf("includeAcked = %+v, want the item with its mark", items)
	}
	sum, err := inboxDispatch(t, d, "inbox.summary", `{}`)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	s := sum.(protocol.InboxSummaryResult)
	if s.Total != 1 || s.Acked != 1 {
		t.Fatalf("summary = total %d, acked %d, want 1 and 1 (SV3: the number never lies)", s.Total, s.Acked)
	}

	// The row itself: one, op=ack, actor from the PEER stamp.
	marks, err := st.LatestAcks([]string{key})
	if err != nil {
		t.Fatalf("marks: %v", err)
	}
	if m := marks[key]; m.Op != "ack" || m.ActorKind != "user" || m.ActorID != "1000" {
		t.Fatalf("row = %+v", m)
	}

	// Idempotence through the wire: a second ack answers NOTHING new.
	res, _ = inboxDispatch(t, d, "inbox.ack", `{"key":"`+key+`"}`)
	if res.(protocol.InboxAckResult).Item != nil {
		t.Fatal("a no-op ack returned an item — the write did not change, so the answer must be empty (P3)")
	}

	// Unack returns it.
	res, err = inboxDispatch(t, d, "inbox.unack", `{"key":"`+key+`"}`)
	if err != nil || res.(protocol.InboxAckResult).Item == nil || res.(protocol.InboxAckResult).Item.Acked != nil {
		t.Fatalf("unack = %+v, %v", res, err)
	}
	list, _ = inboxDispatch(t, d, "inbox.list", `{}`)
	if n := len(list.(protocol.InboxListResult).Items); n != 1 {
		t.Fatalf("after unack the default list holds %d, want 1", n)
	}
}

func TestAckActorFromThePeerNotParams(t *testing.T) {
	st, _ := openInboxStore(t)
	seedQuestion(t, st)
	d, _ := newTestDaemonWithStore(t, st)
	asPerson(t, d, "user", "1000")
	list, err := inboxDispatch(t, d, "inbox.list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	key := list.(protocol.InboxListResult).Items[0].Key
	_, err = inboxDispatch(t, d, "inbox.ack", `{"key":"`+key+`","actor":{"kind":"user","id":"0"}}`)
	var bad protocol.ErrBadParams // the transport answers this shape -32602 (the backup door's twin)
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), `params carry "actor"`) {
		t.Fatalf("params-named actor = %v, want the identity refusal before anything else", err)
	}
}

func TestAckAgentActorRefused(t *testing.T) {
	st, _ := openInboxStore(t)
	q := seedQuestion(t, st)
	d, _ := newTestDaemonWithStore(t, st)
	key := questionKey(t, st, q)
	asPerson(t, d, "agent", "step-9")
	_, err := inboxDispatch(t, d, "inbox.ack", `{"key":"`+key+`"}`)
	var bad protocol.ErrBadParams
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "acknowledging is a person's act") {
		t.Fatalf("agent ack = %v, want the SV12 refusal", err)
	}
	// ...and no row landed under anybody.
	marks, _ := st.LatestAcks([]string{key})
	if _, has := marks[key]; has {
		t.Fatal("the refused agent wrote a marker row")
	}
}

func TestAckOfAGhostKeyRefused(t *testing.T) {
	st, _ := openInboxStore(t)
	seedQuestion(t, st)
	d, _ := newTestDaemonWithStore(t, st)
	asPerson(t, d, "user", "1000")
	_, err := inboxDispatch(t, d, "inbox.ack", `{"key":"questions:9999:2026-11-03T08:00:00Z"}`)
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrInboxScope.Code || !strings.Contains(perr.Data.What, "no longer needing attention") {
		t.Fatalf("ghost ack = %v, want ErrInboxScope with the ghost sentence", err)
	}
}

func TestSnoozeHorizonEndToEnd(t *testing.T) {
	st, _ := openInboxStore(t)
	seedQuestion(t, st)
	d, _ := newTestDaemonWithStore(t, st)
	asPerson(t, d, "user", "1000")
	list, _ := inboxDispatch(t, d, "inbox.list", `{}`)
	key := list.(protocol.InboxListResult).Items[0].Key

	far := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	_, err := inboxDispatch(t, d, "inbox.snooze", `{"key":"`+key+`","until":"`+far+`"}`)
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrSnoozeHorizon.Code {
		t.Fatalf("30-day snooze = %v, want ErrSnoozeHorizon (-31927)", err)
	}
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := inboxDispatch(t, d, "inbox.snooze", `{"key":"`+key+`","until":"`+past+`"}`); err == nil {
		t.Fatal("a snooze into the past was accepted — that is a deletion wearing a hide button")
	}

	// A real snooze hides, and expires AT READ with no job: two seconds
	// out, then a nap, then it is live again — no daemon clock trick.
	soon := time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339)
	res, err := inboxDispatch(t, d, "inbox.snooze", `{"key":"`+key+`","until":"`+soon+`"}`)
	if err != nil {
		t.Fatalf("snooze: %v", err)
	}
	if it := res.(protocol.InboxAckResult).Item; it == nil || it.Acked == nil || it.Acked.Until != soon {
		t.Fatalf("snooze result = %+v, want the mark with until", res)
	}
	time.Sleep(2200 * time.Millisecond)
	list, _ = inboxDispatch(t, d, "inbox.list", `{}`)
	if n := len(list.(protocol.InboxListResult).Items); n != 1 {
		t.Fatalf("an expired snooze still hides (%d items) — expiry must be a read-time comparison", n)
	}
}

func TestAckedRecurrenceStillShows(t *testing.T) {
	st, clock := openInboxStore(t)
	q := seedQuestion(t, st)
	d, _ := newTestDaemonWithStore(t, st)
	asPerson(t, d, "user", "1000")

	key := questionKey(t, st, q)
	if _, err := inboxDispatch(t, d, "inbox.ack", `{"key":"`+key+`"}`); err != nil {
		t.Fatalf("ack: %v", err)
	}
	// The person answers; the question is satisfied; later, a NEW question
	// (new since => new key) arrives. An old ack hiding a recurrence is
	// T2's key rule proven end to end.
	if _, err := st.Answer(q.ID, "approve"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	*clock = clock.Add(2 * time.Minute)
	q2, err := st.Ask(store.Scope{LoopSlug: "demo", ComponentRoot: "/w/alpha"},
		store.NewQuestion{Kind: "clarify", Body: "and the other one?"})
	if err != nil {
		t.Fatalf("re-ask: %v", err)
	}
	list, _ := inboxDispatch(t, d, "inbox.list", `{}`)
	items := list.(protocol.InboxListResult).Items
	if len(items) != 1 || items[0].Key != questionKey(t, st, q2) {
		t.Fatalf("recurrence hidden by the old ack: %+v", items)
	}
}

func TestAckTouchesNoOtherRow(t *testing.T) {
	st, _ := openInboxStore(t)
	q := seedQuestion(t, st)
	beforeAwaiting, err := st.AwaitingLoops(store.ComponentScope{})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := newTestDaemonWithStore(t, st)
	asPerson(t, d, "user", "1000")
	key := questionKey(t, st, q)
	if _, err := inboxDispatch(t, d, "inbox.ack", `{"key":"`+key+`"}`); err != nil {
		t.Fatalf("ack: %v", err)
	}
	afterAwaiting, err := st.AwaitingLoops(store.ComponentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeAwaiting) != len(afterAwaiting) {
		t.Fatalf("acking changed awaiting loops: %d -> %d (Behaviour 5: an ack hides, it never resolves)", len(beforeAwaiting), len(afterAwaiting))
	}
	rows, err := st.PendingQuestionsForScope([]string{"/w/alpha"})
	if err != nil || len(rows) != 1 || rows[0].QuestionID != q.ID {
		t.Fatalf("the question row itself after ack: %+v, %v", rows, err)
	}
}

// questionKey mints the item key for a seeded question the same way the
// questions source does — `<source>:<id>:<since>` with the row's asked_at.
func questionKey(t *testing.T, st *store.Store, q store.Question) string {
	t.Helper()
	rows, err := st.PendingQuestionsForScope([]string{q.ComponentRoot})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.QuestionID == q.ID {
			return "questions:" + itoa(r.QuestionID) + ":" + r.AskedAt
		}
	}
	t.Fatalf("question %d not pending", q.ID)
	return ""
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
