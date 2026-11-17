package store

// kickoff_tx_test.go — S40 T4's store half: the kickoff rides the pin
// transaction. (Placement deviation, recorded in T4's Notes: the task file
// sketched this test in daemon's start_loop_test.go, but a forced failure
// needs a RAISE(ABORT) trigger, and only this package holds the database
// handle — the assertions are exactly the plan's: no loop row, no
// loop_blueprint row, when the message cannot be written.)

import "testing"

func TestTheKickoffRidesThePinTransaction(t *testing.T) {
	s := openTemp(t)
	_, compID := seedAutomationWorld(t, s)
	comp, err := s.ReadComponent(compID)
	if err != nil {
		t.Fatal(err)
	}
	in := NewLoop{Slug: "k1", ComponentRoot: comp.Root, Title: "kickoff loop", Goal: "kickoff loop",
		StartedBy: "event", TriggerID: "tr1", AutomationDepth: 2,
		FirstMessage: &NewChatMessage{Author: "system", Kind: "note", Body: "kickoff words", Source: "automation", SourceRef: "tr1"}}

	// Forced failure at the message: the loop, its pin and its run 1 must
	// all roll back with it (the chat_test.go RAISE(ABORT) pattern).
	mustForce(t, s, `CREATE TRIGGER no_chat BEFORE INSERT ON chat_message
		BEGIN SELECT RAISE(ABORT, 'forced failure in the thread insert'); END`)
	if _, err := s.CreatePinnedLoop(in, "bp-k", 1, "raw bytes", `{"route":""}`); err == nil {
		t.Fatal("a kickoff that cannot be written still created the loop — the pin transaction is a lie")
	}
	if n := countRows(t, s, `SELECT count(*) FROM loop WHERE slug='k1'`); n != 0 {
		t.Errorf("%d loop rows after the forced kickoff failure, want 0", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM loop_blueprint WHERE loop_id IN (SELECT id FROM loop WHERE slug='k1')`); n != 0 {
		t.Errorf("%d loop_blueprint rows, want 0", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM chat_message WHERE loop_slug='k1'`); n != 0 {
		t.Errorf("%d thread rows, want 0", n)
	}
	mustForce(t, s, `DROP TRIGGER no_chat`)

	l, err := s.CreatePinnedLoop(in, "bp-k", 1, "raw bytes", `{"route":""}`)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Thread(Scope{LoopSlug: "k1", ComponentRoot: comp.Root}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("the thread has %d rows, want the single kickoff: %+v", len(msgs), msgs)
	}
	m := msgs[0]
	if m.Seq != 1 || m.Source != "automation" || m.SourceRef != "tr1" || m.Body != "kickoff words" || m.Author != "system" {
		t.Errorf("kickoff row = %+v, want seq 1, automation/tr1, the body, system", m)
	}
	if l.AutomationDepth != 2 {
		t.Errorf("Loop.AutomationDepth = %d, want 2 (read back through loopColumns)", l.AutomationDepth)
	}
	// The provenance reaches run 1 too (T0-Q2's admitted kind).
	var kind string
	if err := s.r.QueryRow(`SELECT started_by_kind FROM loop_run WHERE loop_id=? AND run_seq=1`, l.ID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "event" {
		t.Errorf("run 1 started_by_kind = %q, want event (the loop_run CHECK rebuilt by 0039 admits it)", kind)
	}
	// And a plain person's pin is unchanged: user kind, depth 0, no message.
	plain, err := s.CreatePinnedLoop(NewLoop{Slug: "k2", ComponentRoot: comp.Root}, "bp-k", 1, "raw bytes", `{"route":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.r.QueryRow(`SELECT started_by_kind FROM loop_run WHERE loop_id=? AND run_seq=1`, plain.ID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "user" {
		t.Errorf("a person's run 1 started_by_kind = %q, want user (S20/S39 parity kept)", kind)
	}
	if msgs, _ := s.Thread(Scope{LoopSlug: "k2", ComponentRoot: comp.Root}, 0); len(msgs) != 0 {
		t.Errorf("a plain start wrote %d thread rows, want 0 (nil FirstMessage is absence)", len(msgs))
	}
}
