package store

// The steer store's own tests (S31 T3, D4).
//
// # What this file rules out
//
// D4 says a queued steer is a row that survives a daemon crash, and that its
// DELIVERY is recorded in the same transaction as the checkpoint containing it.
// The failure that earns every test here is not "the steer was lost" — it is
// the pair S7's replay cannot tell apart: a steer whose text is in the
// transcript while its row still says `pending` (so the replay drains it a
// SECOND time, and the model reads the same instruction twice and acts twice),
// and its mirror, a row marked delivered whose text never reached the
// transcript (so the person's instruction is silently gone). Both are one
// non-transactional write away, which is why the atomicity tests below are the
// centre of gravity of this file and not an appendix to it.
//
// The schema half is asserted at the schema, not in Go: a CHECK is the thing
// that holds against a future caller that forgot the vocabulary, so the tests
// insert rows BY HAND through the writer and read the live DDL back out of
// `sqlite_master`.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// ── the migration ────────────────────────────────────────────────────────────

func TestTheSteerTableArrivesWithMigration0021(t *testing.T) {
	// The table has to arrive at 0021 and not earlier, and it has to arrive at
	// all. An entry appended to a SHIPPED migration's CREATE TABLE fixes every
	// fresh database and leaves every upgraded one without the table, with a
	// ledger row swearing it is current — the undetectable failure AGENTS §3
	// exists to prevent. So both halves are checked: a fresh store has it, and a
	// database walked only through 0020 does NOT.
	all := Migrations()
	// 0021 must not be the LAST entry forever — it must be step_steer's OWN
	// entry, arriving at its own number (S14's 0022 rides above it; the check
	// is by identity, not by position-at-the-end).
	if all[20].ID != 21 || all[20].Name != "0021_step_steer" {
		t.Fatalf("entry 21 is %d/%s, want 21/0021_step_steer", all[20].ID, all[20].Name)
	}

	fresh := openTemp(t)
	tables, err := fresh.Tables()
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(tables, "step_steer") {
		t.Errorf("a fresh store has no step_steer; its tables are %v", tables)
	}
	if n := countCatalogueEntries(t, fresh.GormDB(), "index", "idx_step_steer_step_state"); n != 1 {
		t.Errorf("idx_step_steer_step_state exists %d times, want 1 — PendingSteers drains through it", n)
	}

	g, closeDB := rawGorm(t, filepath.Join(t.TempDir(), storeFileName))
	defer closeDB()
	applyMigrations(t, g, all[:20])
	if n := countCatalogueEntries(t, g, "table", "step_steer"); n != 0 {
		t.Fatalf("step_steer exists after 0020 (%d entries), so it did not arrive as its own migration", n)
	}
	applyMigrations(t, g, all[20:])
	if n := countCatalogueEntries(t, g, "table", "step_steer"); n != 1 {
		t.Fatalf("step_steer exists %d times after 0021, want 1", n)
	}
}

func TestTheLedgerRecords0021ExactlyOnceAcrossTwoOpens(t *testing.T) {
	// D2's property for the new entry specifically: opening the store twice is
	// the ordinary case (a restart, or the CLI beside a running daemon), and a
	// migration applied twice is a `CREATE TABLE` that failed the second time
	// and left the ledger reporting a database it never built.
	dir := t.TempDir()
	for open := 1; open <= 2; open++ {
		s := openAt(t, dir)
		applied, err := s.AppliedMigrations()
		if err != nil {
			t.Fatalf("open %d: %v", open, err)
		}
		var n int
		var status string
		for _, a := range applied {
			if a.Name == "0021_step_steer" {
				n++
				status = a.Status
			}
		}
		if n != 1 {
			t.Fatalf("open %d: the ledger records 0021_step_steer %d times, want exactly 1", open, n)
		}
		if status != "applied" {
			t.Errorf("open %d: 0021_step_steer is recorded %q, want applied", open, status)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("open %d: close: %v", open, err)
		}
	}
}

// ── the CHECKs ───────────────────────────────────────────────────────────────

func TestTheSteerStateCHECKAdmitsExactlyTheThreeDeclared(t *testing.T) {
	// The lock test, in the form §3 asks for: the Go vocabulary and the
	// database's are compared, not both trusted. Two lists edited in the same
	// commit agree with each other and still let a CHECK go wrong.
	s := openTemp(t)
	live := liveSteerDDL(t, s)

	// checkValues reads a `column IN (…)` list out of a CREATE. Its failure
	// message names 0016 because 0016 was its first user; the pattern is the
	// same one this CHECK is written in.
	got := checkValues(t, live, "state")
	want := []string{"pending", "delivered", "cancelled"}
	if !equalStrings(got, want) {
		t.Errorf("the live step_steer state CHECK is %v, want %v", got, want)
	}

	goNames := make([]string, 0, len(AllSteerStates()))
	for _, st := range AllSteerStates() {
		goNames = append(goNames, string(st))
	}
	if !equalStrings(goNames, want) {
		t.Errorf("AllSteerStates() is %v, want %v — the Go vocabulary has drifted from the CHECK", goNames, want)
	}
}

func TestTheSteerStateCHECKRefusesAFourthValue(t *testing.T) {
	// A fourth state is the bug a CHECK is for: `watching` would be a steer
	// nobody's drain query reads and nobody's cancel refuses, and it would
	// arrive from a caller that typed a string.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-check", "/repo")
	st := seedStep(t, s, sc)

	err := rawSteer(t, s, st.ID, "watching", "text", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("a fourth steer state was accepted, so the CHECK does not close the set")
	}
	if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the fourth state failed, but not on a CHECK: %v", err)
	}
	if !strings.Contains(err.Error(), "ck_step_steer_state") {
		t.Errorf("the refusal does not name the constraint it tripped (a fourth state must read as the "+
			"state CHECK, not as some other gate): %v", err)
	}

	// And the three legal ones are admitted by the schema itself, not merely by
	// the Go API — a future caller that writes the row by hand is the case the
	// CHECK has to serve.
	for _, state := range []string{"pending", "delivered", "cancelled"} {
		at, iter := any(nil), any(nil)
		if state == "delivered" {
			at, iter = s.Now(), 1
		}
		if err := rawSteer(t, s, st.ID, state, "text", nil, nil, at, iter); err != nil {
			t.Errorf("the CHECK rejected the legal state %q: %v", state, err)
		}
	}
}

func TestTheSteerDeliveryCHECKTiesTheTimestampToTheState(t *testing.T) {
	// The second CHECK is what makes "delivered" mean something rather than
	// merely read like something. A row that says delivered with no timestamp
	// cannot answer the question S7's replay asks ("when did this reach the
	// model, and in which iteration?"), and one that says pending while
	// carrying a delivered_at is the double-delivery case wearing a disguise.
	// Asserting it at the schema is what stops a future writer from making one.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-delivery-check", "/repo")
	st := seedStep(t, s, sc)

	cases := []struct {
		name        string
		state       string
		deliveredAt any
		iteration   any
	}{
		{"pending with a delivered_at", "pending", "2026-10-15T09:41:12.345Z", 1},
		{"pending with an iteration", "pending", nil, 1},
		{"cancelled with a delivered_at", "cancelled", "2026-10-15T09:41:12.345Z", 1},
		{"delivered with no delivered_at", "delivered", nil, 1},
		{"delivered with no iteration", "delivered", "2026-10-15T09:41:12.345Z", nil},
	}
	for _, tc := range cases {
		err := rawSteer(t, s, st.ID, tc.state, "text", nil, nil, tc.deliveredAt, tc.iteration)
		if err == nil {
			t.Errorf("%s was accepted; the delivery half of the state is not tied to the state", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("%s failed, but not on a CHECK: %v", tc.name, err)
		}
	}

	// And the honest shapes go in: pending and cancelled with nothing, delivered
	// with both.
	for _, tc := range []struct {
		name        string
		state       string
		deliveredAt any
		iteration   any
	}{
		{"pending", "pending", nil, nil},
		{"cancelled", "cancelled", nil, nil},
		{"delivered", "delivered", "2026-10-15T09:41:12.345Z", 4},
	} {
		if err := rawSteer(t, s, st.ID, tc.state, "text", nil, nil, tc.deliveredAt, tc.iteration); err != nil {
			t.Errorf("the honest %s shape was refused: %v", tc.name, err)
		}
	}
}

func TestTheSteerContentCHECKRefusesARowThatCarriesNothing(t *testing.T) {
	// `step.steer` requires at least one of text / route / reasoningEffort
	// (brief §5). A row carrying none is a steer that injects nothing and
	// changes nothing, and it is the row a caller with a bug in its parameter
	// validation writes — so the database refuses it too, the way 0019's CHECKs
	// refuse a policy nobody wrote.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-empty-check", "/repo")
	st := seedStep(t, s, sc)

	if err := rawSteer(t, s, st.ID, "pending", nil, nil, nil, nil, nil); err == nil {
		t.Fatal("an empty steer was accepted; the CHECK does not require the row to carry something")
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("the empty steer failed, but not on a CHECK: %v", err)
	}

	// Each one alone is enough: a route-only steer is D5's mid-run change.
	for i, carry := range []struct {
		text, route, effort any
	}{
		{"change course", nil, nil},
		{nil, "fast-route", nil},
		{nil, nil, "high"},
	} {
		if err := rawSteer(t, s, st.ID, "pending", carry.text, carry.route, carry.effort, nil, nil); err != nil {
			t.Errorf("the CHECK refused a steer carrying only field %d: %v", i, err)
		}
	}
}

// ── QueueSteer ───────────────────────────────────────────────────────────────

func TestQueueSteerWritesTheRowAsPending(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-queue", "/repo")
	st := seedStep(t, s, sc)

	got, err := s.QueueSteer(st.ID, NewSteer{Text: "use the table-driven tests", Route: "fast", ReasoningEffort: "low"})
	if err != nil {
		t.Fatalf("queue a steer: %v", err)
	}
	if got.ID == 0 {
		t.Error("the queued steer came back with no id, so it cannot be cancelled or delivered by name")
	}
	if got.StepID != st.ID {
		t.Errorf("the steer belongs to step %d, want %d", got.StepID, st.ID)
	}
	if got.State != SteerPending {
		t.Errorf("a freshly queued steer is %q, want pending", got.State)
	}
	if got.Text != "use the table-driven tests" || got.Route != "fast" || got.ReasoningEffort != "low" {
		t.Errorf("the steer came back as %+v, which is not what was queued", got)
	}
	// The fixed clock, asserted as an exact string rather than a shape (I9).
	if got.CreatedAt != "2026-10-15T09:41:12.345Z" {
		t.Errorf("created_at is %q, want the injected clock's instant", got.CreatedAt)
	}
	if got.DeliveredAt != "" || got.DeliveredIteration != 0 {
		t.Errorf("an undelivered steer carries %q / iteration %d; absent must read as absent, not as a value",
			got.DeliveredAt, got.DeliveredIteration)
	}

	// And it is a ROW: the durable half of D4 is read back from the reader pool,
	// not from what the call happened to return.
	list, err := s.PendingSteers(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != got.ID {
		t.Fatalf("PendingSteers returned %+v, want the one row just queued", list)
	}
}

func TestQueueSteerRefusesAStepThatDoesNotExist(t *testing.T) {
	// The FK says a steer cannot outlive its step; the store's own check says it
	// in words a caller can act on. A steer for a step that is not there would
	// be the inverse of the DB6 failure: a queue with nobody to drain it.
	s := openTemp(t)

	_, err := s.QueueSteer(999999, NewSteer{Text: "anyone there?"})
	if err == nil {
		t.Fatal("a steer was queued for a step that does not exist")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("the refusal does not wrap ErrNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), "999999") {
		t.Errorf("the refusal does not name the step it refused: %v", err)
	}
}

func TestQueueSteerRefusesASubjectThatCarriesNothing(t *testing.T) {
	// The protocol-layer version of the content CHECK, worded for the person
	// who typed it (AGENTS §5: a refusal names the thing refused).
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-nothing", "/repo")
	st := seedStep(t, s, sc)

	_, err := s.QueueSteer(st.ID, NewSteer{Text: "   "})
	if err == nil {
		t.Fatal("an empty steer was queued")
	}
	for _, want := range []string{"text", "route", "reasoning effort", itoa(st.ID)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}

	// Whitespace is nothing, and an all-blank steer is the shape a form field
	// submits when the person pressed enter by accident.
	if _, err := s.QueueSteer(st.ID, NewSteer{Text: "\n\t "}); err == nil {
		t.Error("a whitespace-only steer was queued")
	}
}

// ── PendingSteers ────────────────────────────────────────────────────────────

func TestPendingSteersIsThePendingQueueOfOneStepOnly(t *testing.T) {
	// Three properties the runtime's drain depends on: ONLY pending (a delivered
	// steer re-injected is D4's double delivery; a cancelled one is the person's
	// refusal overridden), ONLY this step's (P33: steers are keyed by stepId,
	// never by loop), and in QUEUE ORDER (two steers in one turn are delivered
	// in the order the person typed them — brief §6 T4's test says "both
	// delivered in order, in one message").
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-pending", "/repo")
	mine := seedStep(t, s, sc)
	theirs := seedStep(t, s, sc)

	first, err := s.QueueSteer(mine.ID, NewSteer{Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.QueueSteer(mine.ID, NewSteer{Text: "second"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.QueueSteer(mine.ID, NewSteer{Text: "third"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueSteer(theirs.ID, NewSteer{Text: "not yours"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelSteer(second.ID); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := s.CheckpointWithSteers(ctx, mine.ID, []model.Message{userMsg("delivered one")}, json.RawMessage(`{"envelope":"delivered-one"}`), []int64{third.ID}, 2); err != nil {
		t.Fatalf("deliver the third steer: %v", err)
	}

	got, err := s.PendingSteers(mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != first.ID {
		var ids []int64
		for _, st := range got {
			ids = append(ids, st.ID)
		}
		t.Fatalf("PendingSteers(%d) returned %v, want exactly the one still-pending steer %d",
			mine.ID, ids, first.ID)
	}

	// Order, with everything pending: the second step has two rows and must hand
	// them back in the order the person typed them.
	if _, err := s.QueueSteer(theirs.ID, NewSteer{Text: "one more"}); err != nil {
		t.Fatal(err)
	}
	got, err = s.PendingSteers(theirs.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("PendingSteers(%d) is %+v, want the two rows still pending", theirs.ID, got)
	}
	if got[0].Text != "not yours" || got[1].Text != "one more" {
		t.Errorf("PendingSteers(%d) is [%q %q], want them in arrival order [not yours, one more]",
			theirs.ID, got[0].Text, got[1].Text)
	}
	if got[0].ID > got[1].ID {
		t.Errorf("PendingSteers(%d) returned ids %d then %d; the queue order is the arrival order",
			theirs.ID, got[0].ID, got[1].ID)
	}
}

// ── CancelSteer ──────────────────────────────────────────────────────────────

func TestCancelSteerFlipsAPendingSteerAndLeavesTheDeliveryColumnsEmpty(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-cancel", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "never mind"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.CancelSteer(queued.ID)
	if err != nil {
		t.Fatalf("cancel a pending steer: %v", err)
	}
	if !res.OK {
		t.Fatalf("the cancel was refused: %s", res.Reason)
	}
	if res.State != SteerCancelled {
		t.Errorf("the result reports %q, want cancelled (brief §5: step.steer.cancel answers {state})", res.State)
	}

	row := mustSteer(t, s, queued.ID)
	if row.State != SteerCancelled {
		t.Errorf("the row is %q, want cancelled", row.State)
	}
	if row.DeliveredAt != "" || row.DeliveredIteration != 0 {
		t.Errorf("a cancelled steer carries delivered_at %q / iteration %d; it was never delivered",
			row.DeliveredAt, row.DeliveredIteration)
	}

	// And it is gone from the queue, which is the whole reason to cancel.
	pending, err := s.PendingSteers(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("a cancelled steer is still pending: %+v", pending)
	}
}

func TestCancelSteerRefusesADeliveredSteerNamingTheStateFound(t *testing.T) {
	// I4 and DB19 in one assertion. "Already delivered" is a domain refusal
	// with something useful to say — the state found — and NOT an error, which
	// a caller would have to guess at. It is also the case the UI will actually
	// meet: the person presses cancel just as the turn boundary passes.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-cancel-delivered", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "too late"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.CheckpointWithSteers(ctx, st.ID, []model.Message{userMsg("it went in")}, json.RawMessage(`{"envelope":"it-went-in"}`), []int64{queued.ID}, 3); err != nil {
		t.Fatalf("deliver the steer: %v", err)
	}
	before := mustSteer(t, s, queued.ID)

	res, err := s.CancelSteer(queued.ID)
	if err != nil {
		t.Fatalf("a refused transition must be a result, not an error: %v", err)
	}
	if res.OK {
		t.Fatal("a delivered steer was cancelled; delivery is terminal")
	}
	if res.State != SteerDelivered {
		t.Errorf("the refusal reports %q, want delivered — a refusal names the state found (I4)", res.State)
	}
	if !res.Found {
		t.Error("the refusal says there was no such row, and there was")
	}
	if !strings.Contains(res.Reason, "delivered") || !strings.Contains(res.Reason, itoa(queued.ID)) {
		t.Errorf("the refusal does not name the state and the row: %q", res.Reason)
	}

	// Nothing moved: not the state, not the timestamp, not the iteration.
	after := mustSteer(t, s, queued.ID)
	if after.State != before.State || after.DeliveredAt != before.DeliveredAt ||
		after.DeliveredIteration != before.DeliveredIteration {
		t.Errorf("the refused cancel still changed the row: %+v was %+v", after, before)
	}
}

func TestCancelSteerRefusesACancelledSteerNamingTheStateFound(t *testing.T) {
	// The double-press. A second cancel is refused rather than silently
	// idempotent, because the UI is allowed to know the first one worked and
	// this one changed nothing.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-cancel-twice", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "not at all"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelSteer(queued.ID); err != nil {
		t.Fatal(err)
	}

	res, err := s.CancelSteer(queued.ID)
	if err != nil {
		t.Fatalf("a refused transition must be a result, not an error: %v", err)
	}
	if res.OK {
		t.Fatal("a cancelled steer was cancelled again")
	}
	if res.State != SteerCancelled {
		t.Errorf("the refusal reports %q, want cancelled", res.State)
	}
	if !strings.Contains(res.Reason, "cancelled") || !strings.Contains(res.Reason, itoa(queued.ID)) {
		t.Errorf("the refusal does not name the state and the row: %q", res.Reason)
	}
}

func TestCancelSteerOfAnUnknownIdSaysThereWasNoSuchRow(t *testing.T) {
	// Three-state honesty (P31): "refused because it is already delivered",
	// "refused because it is already cancelled" and "there is no such steer" are
	// THREE answers. Collapsing the third into the other two is how a typo'd id
	// reads as "someone got there first".
	s := openTemp(t)

	res, err := s.CancelSteer(4242)
	if err != nil {
		t.Fatalf("a refusal is a result, not an error: %v", err)
	}
	if res.OK || res.Found {
		t.Fatalf("cancelling a steer that does not exist reported %+v", res)
	}
	if res.State != "" {
		t.Errorf("the refusal invents a state %q for a row that is not there", res.State)
	}
	if !strings.Contains(res.Reason, "4242") {
		t.Errorf("the refusal does not name the id it could not find: %q", res.Reason)
	}
}

// ── CheckpointWithSteers ─────────────────────────────────────────────────────

func TestCheckpointWithSteersWritesTheTranscriptAndMarksDeliveredInOneTransaction(t *testing.T) {
	// The happy path, asserted on BOTH halves and on the columns D4 is made of:
	// delivered_at and delivered_iteration are what `step.steer.delivered`
	// reports and what a replay compares against.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-checkpoint", "/repo")
	st := seedStep(t, s, sc)

	first, err := s.QueueSteer(st.ID, NewSteer{Text: "prefer sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.QueueSteer(st.ID, NewSteer{Route: "cheap"})
	if err != nil {
		t.Fatal(err)
	}

	msgs := []model.Message{
		userMsg("the original ask"),
		assistantMsg("I will open the store"),
		userMsg("prefer sqlite", "the person steered before this tool ran"),
	}
	res, err := s.CheckpointWithSteers(context.Background(), st.ID, msgs, json.RawMessage(`{"envelope":"pair-4"}`), []int64{first.ID, second.ID}, 4)
	if err != nil {
		t.Fatalf("checkpoint with steers: %v", err)
	}
	if !res.OK {
		t.Errorf("a full delivery reported %q", res.Reason)
	}
	if len(res.Delivered) != 2 || len(res.NotDelivered) != 0 {
		t.Errorf("the result is %+v, want both ids delivered and nothing refused", res)
	}

	got := mustReadTranscript(t, s, st.ID)
	if len(got) != len(msgs) {
		t.Fatalf("the transcript holds %d messages, want %d", len(got), len(msgs))
	}
	for i, m := range got {
		if m.Role != msgs[i].Role || len(m.Content) != len(msgs[i].Content) {
			t.Fatalf("message %d is %+v, want %+v", i, m, msgs[i])
		}
	}
	if n, err := s.TranscriptRows(st.ID); err != nil || n != 3 {
		t.Errorf("the transcript row count is %d (%v), want 3 — the checkpoint replaced it, it did not append", n, err)
	}

	for _, id := range []int64{first.ID, second.ID} {
		row := mustSteer(t, s, id)
		if row.State != SteerDelivered {
			t.Errorf("steer %d is %q, want delivered", id, row.State)
		}
		if row.DeliveredAt != "2026-10-15T09:41:12.345Z" {
			t.Errorf("steer %d was delivered at %q, want the injected clock's instant", id, row.DeliveredAt)
		}
		if row.DeliveredIteration != 4 {
			t.Errorf("steer %d records iteration %d, want 4 — the iteration is what step.steer.delivered "+
				"says, and what a replay compares", id, row.DeliveredIteration)
		}
	}

	// The untouched steer — still pending, and nothing about it moved.
	left, err := s.PendingSteers(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("PendingSteers is %+v after both were delivered", left)
	}
}

func TestCheckpointWithSteersRollsBackBothHalvesWhenTheSteerHalfFails(t *testing.T) {
	// THE test. The failure being injected is the one `kill -9` produces at the
	// worst instruction — after the transcript rows are written and before the
	// commit — reached here by a BAD STATEMENT rather than a real signal, which
	// is how S7 proves its own cases (K30: staging a crash at a precise instant
	// is unreliable; constructing the state is not).
	//
	// The assertion is the pair that D4 forbids: if the transaction is real then
	// NEITHER half happened. A checkpoint that is not one leaves the transcript
	// rewritten with the steers still pending — S7's replay drains them again
	// and the model reads the same instruction twice.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-atomic", "/repo")
	st := seedStep(t, s, sc)
	other := seedStep(t, s, sc)

	saved := []model.Message{userMsg("the checkpoint that was there before")}
	if _, err := s.CheckpointWithSteers(context.Background(), st.ID, saved, json.RawMessage(`{"envelope":"pre-injection"}`), nil, 1); err != nil {
		t.Fatalf("seed the prior checkpoint: %v", err)
	}
	before, err := s.TranscriptDigest(st.ID)
	if err != nil {
		t.Fatal(err)
	}

	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "the steer that must stay pending"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.QueueSteer(other.ID, NewSteer{Text: "belongs to another step"})
	if err != nil {
		t.Fatal(err)
	}

	injections := []struct {
		name   string
		ids    []int64
		reason string
	}{
		{"an id that does not exist", []int64{queued.ID, 999999}, "999999"},
		{"an id belonging to another step", []int64{queued.ID, theirs.ID}, itoa(theirs.ID)},
	}
	for _, inj := range injections {
		_, err := s.CheckpointWithSteers(context.Background(), st.ID,
			[]model.Message{userMsg("the transcript that must NOT be there")}, json.RawMessage(`{"envelope":"rolled-back"}`), inj.ids, 7)
		if err == nil {
			t.Errorf("%s: the checkpoint claimed success on a statement that cannot work", inj.name)
			continue
		}
		if !strings.Contains(err.Error(), inj.reason) {
			t.Errorf("%s: the failure does not name the thing refused (%s): %v", inj.name, inj.reason, err)
		}

		// Neither half. The transcript is still the one from before the call…
		after, err := s.TranscriptDigest(st.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Errorf("%s: the transcript changed while the checkpoint failed — half a checkpoint is the "+
				"failure D4 exists to prevent (replay drains the steer a second time)", inj.name)
		}
		// …and the steer is still pending, undelivered.
		row := mustSteer(t, s, queued.ID)
		if row.State != SteerPending || row.DeliveredAt != "" || row.DeliveredIteration != 0 {
			t.Errorf("%s: the steer moved to %+v while the checkpoint failed", inj.name, row)
		}
		// …and the other step's steer is untouched by a call that named it.
		foreign := mustSteer(t, s, theirs.ID)
		if foreign.State != SteerPending {
			t.Errorf("%s: another step's steer was delivered by this call: %+v", inj.name, foreign)
		}
	}
}

func TestCheckpointWithSteersReportsASteerThatLeftPendingWithoutRefusingTheTranscript(t *testing.T) {
	// The race the design has to answer honestly: the runtime drains the queue,
	// appends the text, and the person cancels one of those steers before the
	// checkpoint lands. Refusing the whole checkpoint at that instant would be
	// the worse of the two wrongs — the transcript already carries the text, and
	// a caller that cannot persist it either loses the turn or loops. So the
	// transaction COMMITS, and the result says plainly which rows it did not
	// deliver and what state it found them in. A lie of omission here is a
	// steer that reads as delivered and never was.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-race", "/repo")
	st := seedStep(t, s, sc)

	deliver, err := s.QueueSteer(st.ID, NewSteer{Text: "still wanted"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.QueueSteer(st.ID, NewSteer{Text: "withdrawn"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelSteer(cancelled.ID); err != nil {
		t.Fatal(err)
	}

	res, err := s.CheckpointWithSteers(context.Background(), st.ID,
		[]model.Message{userMsg("the drained text")}, json.RawMessage(`{}`), []int64{deliver.ID, cancelled.ID}, 2)
	if err != nil {
		t.Fatalf("a steer leaving pending mid-checkpoint is a domain outcome, not an error: %v", err)
	}
	if res.OK {
		t.Error("the result claims everything was delivered; one row was not")
	}
	if len(res.Delivered) != 1 || res.Delivered[0] != deliver.ID {
		t.Errorf("Delivered is %v, want just %d", res.Delivered, deliver.ID)
	}
	if len(res.NotDelivered) != 1 || res.NotDelivered[0].ID != cancelled.ID ||
		res.NotDelivered[0].State != SteerCancelled {
		t.Errorf("NotDelivered is %+v, want the cancelled row named with the state found", res.NotDelivered)
	}
	if !strings.Contains(res.Reason, "cancelled") || !strings.Contains(res.Reason, itoa(cancelled.ID)) {
		t.Errorf("the Reason does not name the row and the state found: %q", res.Reason)
	}

	// The transcript half DID land — that is the point of committing.
	if n, err := s.TranscriptRows(st.ID); err != nil || n != 1 {
		t.Errorf("the committed transcript holds %d rows (%v), want 1", n, err)
	}
	row := mustSteer(t, s, cancelled.ID)
	if row.State != SteerCancelled || row.DeliveredAt != "" || row.DeliveredIteration != 0 {
		t.Errorf("the refused row was rewritten after all: %+v", row)
	}
	if got := mustSteer(t, s, deliver.ID); got.State != SteerDelivered {
		t.Errorf("the pending steer is %q, want delivered", got.State)
	}
}

func TestASteerIsDeliveredOnceEvenAcrossAReopen(t *testing.T) {
	// D4's replay case, stated the way S7 will meet it: the process dies after a
	// checkpoint, a new one opens the same file and replays the same instant.
	// The steer is already `delivered` and already in the transcript — so the
	// drain must not offer it, and a checkpoint that names it again must not
	// stamp it a second time. A re-stamped delivered_at is how a double delivery
	// hides: the row still reads delivered, and nobody can tell the two apart.
	//
	// The two halves of the store are two OPENS over one directory, with
	// different clocks, so "not re-stamped" is a fact about the row rather than
	// an artefact of one injected instant.
	dir := t.TempDir()
	s := openAt(t, dir)
	sc := seedLoop(t, s, "steer-replay", "/repo")
	st := seedStep(t, s, sc)

	first, err := s.QueueSteer(st.ID, NewSteer{Text: "do it once"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.QueueSteer(st.ID, NewSteer{Text: "and again"})
	if err != nil {
		t.Fatal(err)
	}

	msgs := []model.Message{userMsg("the ask"), userMsg("do it once", "and again")}
	res, err := s.CheckpointWithSteers(context.Background(), st.ID, msgs, json.RawMessage(`{"envelope":"reopen"}`), []int64{first.ID, second.ID}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("the first delivery was refused: %s", res.Reason)
	}
	digest, err := s.TranscriptDigest(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantAt := mustSteer(t, s, first.ID).DeliveredAt
	if wantAt == "" {
		t.Fatal("the first delivery recorded no timestamp")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// The next process, a later clock, the same file.
	later := time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC)
	s2, err := Open(Options{Dir: dir, Clock: func() time.Time { return later }, Env: emptyEnv})
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	defer s2.Close()

	// The drain sees nothing: the steers are delivered, not pending.
	if pending, err := s2.PendingSteers(st.ID); err != nil || len(pending) != 0 {
		t.Fatalf("a reopened store offers %d pending steers (%v); a delivered steer is never drained twice",
			len(pending), err)
	}

	// A checkpoint naming the same ids reports them instead of re-stamping them.
	res, err = s2.CheckpointWithSteers(context.Background(), st.ID, msgs, json.RawMessage(`{"envelope":"reopen"}`), []int64{first.ID, second.ID}, 2)
	if err != nil {
		t.Fatalf("a replayed checkpoint is a domain outcome, not an error: %v", err)
	}
	if res.OK {
		t.Error("the replayed checkpoint reported a clean delivery of steers it had already delivered")
	}
	if len(res.NotDelivered) != 2 {
		t.Errorf("NotDelivered is %+v, want both already-delivered rows named", res.NotDelivered)
	}
	for _, row := range res.NotDelivered {
		if row.State != SteerDelivered {
			t.Errorf("steer %d reported as %q, want delivered — the state found", row.ID, row.State)
		}
	}

	for _, id := range []int64{first.ID, second.ID} {
		row := mustSteer(t, s2, id)
		if row.DeliveredAt != wantAt {
			t.Errorf("steer %d was re-stamped to %q, want the original %q — delivery is recorded once",
				id, row.DeliveredAt, wantAt)
		}
		if row.DeliveredIteration != 2 {
			t.Errorf("steer %d records iteration %d, want the iteration of its ONE delivery", id, row.DeliveredIteration)
		}
	}
	if got, err := s2.TranscriptDigest(st.ID); err != nil || got != digest {
		t.Errorf("the replayed checkpoint changed the transcript (%v vs %v, err %v); it rewrites the same "+
			"rows, it does not append them", got, digest, err)
	}
}

func TestCheckpointWithSteersRefusesAStepThatIsNotRunning(t *testing.T) {
	// The same guard SaveRunningCheckpoint has (checkpoint.go:46), carried over
	// because it is the one that stops a finished step being rewritten by a
	// late checkpoint from a goroutine still holding its step id. The refusal
	// names the status FOUND — "not running" alone sends the reader back to the
	// call site to work out which of three states it met.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-not-running", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "too late"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepStatus(st.ID, StepDone, "ok"); err != nil {
		t.Fatal(err)
	}

	_, err = s.CheckpointWithSteers(context.Background(), st.ID, []model.Message{userMsg("late")}, json.RawMessage(`{}`), []int64{queued.ID}, 3)
	if err == nil {
		t.Fatal("a finished step was checkpointed")
	}
	if !strings.Contains(err.Error(), "running") || !strings.Contains(err.Error(), "done") {
		t.Errorf("the refusal does not name the status found: %v", err)
	}
	if !strings.Contains(err.Error(), itoa(st.ID)) {
		t.Errorf("the refusal does not name the step it refused: %v", err)
	}
	if row := mustSteer(t, s, queued.ID); row.State != SteerPending {
		t.Errorf("the refusal still delivered the steer: %+v", row)
	}
}

func TestCheckpointWithSteersHonoursTheCallerContext(t *testing.T) {
	// The transaction is begun with the caller's context, exactly as
	// SaveRunningCheckpoint does (checkpoint.go:21). A step whose deadline
	// passed mid-turn must not have its checkpoint written behind its own back;
	// a `Begin()` without the context would be invisible here and visible only
	// as a step that resumed after it was supposed to have stopped.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-ctx", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "too late"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CheckpointWithSteers(ctx, st.ID, []model.Message{userMsg("never written")}, json.RawMessage(`{}`), []int64{queued.ID}, 1); err == nil {
		t.Fatal("a checkpoint on a cancelled context succeeded")
	}
	if n, err := s.TranscriptRows(st.ID); err != nil || n != 0 {
		t.Errorf("the cancelled checkpoint left %d transcript rows behind (%v)", n, err)
	}
	if row := mustSteer(t, s, queued.ID); row.State != SteerPending {
		t.Errorf("the cancelled checkpoint delivered a steer anyway: %+v", row)
	}
}

func TestCheckpointWithSteersWithNoIDsIsAPlainCheckpoint(t *testing.T) {
	// The turn with no steers is the common case, and the method has to serve it
	// without ceremony: the runtime has ONE persistence call on the steer path,
	// not one for "steers" and one for "none".
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-none", "/repo")
	st := seedStep(t, s, sc)

	res, err := s.CheckpointWithSteers(context.Background(), st.ID,
		[]model.Message{userMsg("no steers this turn")}, json.RawMessage(`{"envelope":"plain"}`), nil, 1)
	if err != nil {
		t.Fatalf("checkpoint with no steers: %v", err)
	}
	if !res.OK || len(res.Delivered) != 0 || len(res.NotDelivered) != 0 {
		t.Errorf("the result is %+v, want a clean empty delivery", res)
	}
	if n, err := s.TranscriptRows(st.ID); err != nil || n != 1 {
		t.Errorf("the transcript holds %d rows (%v), want 1", n, err)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// userMsg builds the transcripts these tests checkpoint. Several text blocks in
// ONE user message is the shape LW2 produces — the synthetic tool results and
// the steer text in the same message — so the tests write it directly rather
// than through a fixture.
func userMsg(texts ...string) model.Message {
	blocks := make([]model.Block, 0, len(texts))
	for _, x := range texts {
		blocks = append(blocks, model.TextBlock(x))
	}
	return model.Message{Role: model.RoleUser, Content: blocks}
}

func assistantMsg(text string) model.Message {
	return model.Message{Role: model.RoleAssistant, Provenance: testProvenance(), Content: []model.Block{model.TextBlock(text)}}
}

// rawSteer inserts a step_steer row BY HAND, through the store's own writer,
// with the state and the delivery columns supplied rather than produced. A
// CHECK is only worth having if something can try to violate it, and the API
// under test will not let this file name a fourth state — so the writes go
// under the API, the way policy_test.go plants rows to test 0016 and 0019.
func rawSteer(t *testing.T, s *Store, stepID int64, state string, text, route, effort, deliveredAt any, iteration any) error {
	t.Helper()
	_, err := s.w.Exec(`
		INSERT INTO step_steer (step_id, text, route, reasoning_effort, state, created_at, delivered_at, delivered_iteration)
		VALUES (?,?,?,?,?,?,?,?)`,
		stepID, text, route, effort, state, s.Now(), deliveredAt, iteration)
	return err
}

// mustSteer reads one row by id, failing the test rather than returning a
// zero value a comparison would silently accept.
func mustSteer(t *testing.T, s *Store, id int64) Steer {
	t.Helper()
	var row Steer
	var state string
	err := s.r.QueryRow(`
		SELECT id, step_id, COALESCE(text,''), COALESCE(route,''), COALESCE(reasoning_effort,''),
		       state, created_at, COALESCE(delivered_at,''), COALESCE(delivered_iteration,0)
		FROM step_steer WHERE id = ?`, id).
		Scan(&row.ID, &row.StepID, &row.Text, &row.Route, &row.ReasoningEffort,
			&state, &row.CreatedAt, &row.DeliveredAt, &row.DeliveredIteration)
	if err != nil {
		t.Fatalf("read steer %d: %v", id, err)
	}
	row.State = SteerState(state)
	return row
}

// mustReadTranscript reads a step's transcript and fails rather than handing
// back a nil slice that every comparison below would accept.
func mustReadTranscript(t *testing.T, s *Store, stepID int64) []model.Message {
	t.Helper()
	res := s.ReadTranscript(stepID)
	if !res.OK {
		t.Fatalf("read the transcript of step %d: %s", stepID, res.Reason())
	}
	return res.Messages
}

// liveSteerDDL reads the table's CREATE out of the catalogue, the same way
// livePolicyDDL does for 0016's CHECKs: the tests compare against the shape
// that is ACTUALLY installed, not against a second copy of the DDL in a test
// file.
func liveSteerDDL(t *testing.T, s *Store) string {
	t.Helper()
	var create string
	if err := s.r.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name='step_steer'`).
		Scan(&create); err != nil {
		t.Fatalf("read the stored CREATE for step_steer from the catalogue: %v", err)
	}
	if !strings.Contains(create, "CHECK") {
		t.Fatalf("the stored CREATE for step_steer carries no CHECK at all: %q", create)
	}
	return create
}

// ── the S31 T4 additions ─────────────────────────────────────────────────────

func TestListSteersShowsEveryStateOfOneStepInQueueOrder(t *testing.T) {
	// `step.steers` answers "what did I ask and what became of it", which is
	// the THREE-state row. The drain (PendingSteers) must stay blind to
	// delivered rows; this reader must not be.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-list", "/repo")
	mine := seedStep(t, s, sc)
	theirs := seedStep(t, s, sc)

	first, err := s.QueueSteer(mine.ID, NewSteer{Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.QueueSteer(mine.ID, NewSteer{Text: "second", Route: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.QueueSteer(mine.ID, NewSteer{Text: "third"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueSteer(theirs.ID, NewSteer{Text: "not yours"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckpointWithSteers(context.Background(), mine.ID,
		[]model.Message{userMsg("first read")}, json.RawMessage(`{}`), []int64{first.ID}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelSteer(third.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListSteers(mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("listed %d steers, want all three (a history that loses states is not a history)", len(got))
	}
	wantStates := []SteerState{SteerDelivered, SteerPending, SteerCancelled}
	wantIDs := []int64{first.ID, second.ID, third.ID}
	for i, st := range got {
		if st.ID != wantIDs[i] {
			t.Fatalf("steer %d id = %d, want %d: queue order is by id, the honest clock", i, st.ID, wantIDs[i])
		}
		if st.State != wantStates[i] {
			t.Fatalf("steer %d state = %q, want %q (queue order by id)", i, st.State, wantStates[i])
		}
	}
	if got[0].DeliveredAt == "" {
		t.Fatal("the delivered steer lost its delivered_at")
	}
	if got[0].DeliveredIteration != 1 {
		t.Fatalf("delivered iteration = %d, want 1", got[0].DeliveredIteration)
	}
	if got[1].Route != "fast" {
		t.Fatalf("the route-only field of a listed steer = %q, want fast", got[1].Route)
	}
	if theirsList, err := s.ListSteers(theirs.ID); err != nil || len(theirsList) != 1 {
		t.Fatalf("the other step's listing = %d rows (%v); ListSteers is keyed by stepId (P33)", len(theirsList), err)
	}
}

func TestCheckpointWithSteersPersistsTheEnvelopeWithTheTranscript(t *testing.T) {
	// LW6: a steer's route/effort override must survive recovery, and recovery
	// replays from the SAVED REQUEST — so the envelope rides the same
	// transaction as the transcript and the delivery, or none of them do.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-envelope", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "use the fast route", Route: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	env := json.RawMessage(`{"request":{"route":{"name":"fast"}},"usage":{"inputTokens":7}}`)
	if _, err := s.CheckpointWithSteers(context.Background(), st.ID,
		[]model.Message{userMsg("queued", "use the fast route")}, env, []int64{queued.ID}, 2); err != nil {
		t.Fatal(err)
	}
	row, err := s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(row.ContextManifestRaw) != string(env) {
		t.Fatalf("the envelope on disk = %s, want the checkpoint's %s (byte for byte)",
			row.ContextManifestRaw, env)
	}

	// The rollback half: a checkpoint refused by the DELIVERY (a steer owned by
	// another step) must not have written the new envelope either.
	other, err := s.QueueSteer(st.ID, NewSteer{Text: "fine"})
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	steal, err := s.QueueSteer(seedStep(t, s, sc).ID, NewSteer{Text: "not yours"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckpointWithSteers(context.Background(), st.ID,
		[]model.Message{userMsg("must not appear")}, json.RawMessage(`{"request":{"route":{"name":"slow"}}}`),
		[]int64{steal.ID}, 3); err == nil {
		t.Fatal("a checkpoint naming another step's steer should be refused")
	} else if !strings.Contains(err.Error(), "steers are delivered by the step that queued them") {
		t.Fatalf("refusal = %v", err)
	}
	row, err = s.ReadStep(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(row.ContextManifestRaw) != string(env) {
		t.Fatalf("a refused checkpoint still wrote the envelope: %s", row.ContextManifestRaw)
	}
}

func TestCheckpointWithSteersRefusesAnEnvelopeThatIsNotJSON(t *testing.T) {
	// The envelope is what LoadResume replays. Half a JSON document is worse
	// than none: recovery would fail on the thing that was MEANT to survive.
	s := openTemp(t)
	sc := seedLoop(t, s, "steer-envelope-bad", "/repo")
	st := seedStep(t, s, sc)
	queued, err := s.QueueSteer(st.ID, NewSteer{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckpointWithSteers(context.Background(), st.ID,
		[]model.Message{userMsg("queued", "hello")}, json.RawMessage(`{"request":`),
		[]int64{queued.ID}, 1); err == nil {
		t.Fatal("a half-written envelope should be refused before the transaction begins")
	} else if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("refusal = %v", err)
	}
	// Still pending: the refusal happened BEFORE any write.
	got, err := s.PendingSteers(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("the steer was consumed by a refused checkpoint: %d pending", len(got))
	}
}
