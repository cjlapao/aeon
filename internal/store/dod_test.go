package store

// dod_test.go — S57 T5's definition-of-done ledger (VG13): the
// materialisation's idempotence and its `met` carry-over (byte-equal text
// only), the mark's doors (the actor's, the state's, the evidence's, and
// the manual/verify_run pairing), and the ledger's append-only audit.

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// isDodRefusal is the test's check of a refused mark: the refusal is a
// discriminated result (DB19), never an error.
func isDodRefusal(w DodWrite, code DodRefusal) bool {
	return !w.OK() && w.Refusal == code
}

// TestMaterializeDoDIsIdempotentAndCarriesOnlyEqualText — the T5 name:
// re-running the same brief version writes nothing (the rows stand on
// (brief_id, idx)), and a new brief version keeps a `met` ONLY for the
// byte-equal text — a changed text is reset to unchecked (and told in a
// reset event), a dropped item is left in place (history), and the list
// read shows the loop's latest version.
func TestMaterializeDoDIsIdempotentAndCarriesOnlyEqualText(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "dod-root")
	sc := seedLoop(t, s, "dod-loop", "dod-root")
	user := s57User

	v1 := DoDBrief{BriefID: "brf-v1", BriefVersion: 1,
		Items: []string{"the daemon starts", "the loop completes its first step", "the socket answers"}}
	n, err := s.MaterializeDoD(sc, v1)
	if err != nil || n != 3 {
		t.Fatalf("materialise v1: wrote %d err %v, want 3", n, err)
	}
	// Re-run: idempotent on (brief_id, idx) — nothing new.
	if n, err := s.MaterializeDoD(sc, v1); err != nil || n != 0 {
		t.Fatalf("re-materialise v1: wrote %d err %v, want 0", n, err)
	}
	items, err := s.ListDoD(sc)
	if err != nil || len(items) != 3 {
		t.Fatalf("list after v1: %d items err %v, want 3", len(items), err)
	}
	// Mark idx 1 met, by a person's word (manual evidence).
	w, err := s.MarkDoD(user, items[1].ID, "met", DodEvidence{Kind: "manual", Ref: "it does — I watched it"})
	if err != nil || !w.OK() {
		t.Fatalf("mark idx 1 met: %v %v", w, err)
	}
	it1 := w.Item
	if it1.State != "met" || it1.EvidenceKind != "manual" {
		t.Fatalf("the mark did not land: %+v", it1)
	}

	// Version 2: idx 0 changed, idx 1 byte-equal (and met), idx 2
	// byte-equal (and unchecked — nothing to carry), idx 3 new.
	v2 := DoDBrief{BriefID: "brf-v2", BriefVersion: 2,
		Items: []string{"the daemon starts AND answers pings", "the loop completes its first step", "the socket answers", "a new criterion nobody met"}}
	if n, err := s.MaterializeDoD(sc, v2); err != nil || n != 4 {
		t.Fatalf("materialise v2: wrote %d err %v, want 4", n, err)
	}
	items, err = s.ListDoD(sc)
	if err != nil || len(items) != 4 {
		t.Fatalf("list after v2: %d items err %v, want 4 (the loop's latest version)", len(items), err)
	}
	// idx 0: the text changed — the old state (unchecked, but the point
	// stands) did not carry over; the reset is told.
	if items[0].State != "unchecked" || items[0].BriefVersion != 2 {
		t.Errorf("changed text is a new criterion: %+v", items[0])
	}
	// idx 1: byte-equal and met — the state AND the evidence AND the
	// marker carried over.
	if items[1].State != "met" || items[1].EvidenceKind != "manual" || items[1].EvidenceRef != "it does — I watched it" {
		t.Errorf("byte-equal met carried its evidence: %+v", items[1])
	}
	if items[1].ActorKind == nil || *items[1].ActorKind != "user" || items[1].ActorID == nil || *items[1].ActorID != "u1" {
		t.Errorf("the carry-over keeps the marker: %+v", items[1])
	}
	// idx 2: byte-equal but never met — a fresh unchecked row (no state
	// to carry, and no marker to keep).
	if items[2].State != "unchecked" || items[2].EvidenceKind != "" || items[2].ActorKind != nil {
		t.Errorf("byte-equal unchecked is a fresh row: %+v", items[2])
	}
	// idx 3: the new criterion.
	if items[3].State != "unchecked" || items[3].BriefVersion != 2 {
		t.Errorf("the new criterion is unchecked at v2: %+v", items[3])
	}
	// v1's rows still stand (history; the store does not delete).
	var old int
	if err := g.Raw(`SELECT count(*) FROM dod_item WHERE brief_id = 'brf-v1'`).Scan(&old).Error; err != nil {
		t.Fatalf("count v1 rows: %v", err)
	}
	if old != 3 {
		t.Errorf("v1's rows were deleted: %d stand, want 3 (history is not deleted)", old)
	}

	// Re-run v2: idempotent again.
	if n, err := s.MaterializeDoD(sc, v2); err != nil || n != 0 {
		t.Fatalf("re-materialise v2: wrote %d err %v, want 0", n, err)
	}

	// The audit: 3 materialize (v1), 1 mark, then v2's 1 reset + 3
	// materialize (the carried row's materialize names its arrival with the
	// carried state: from NULL, to met).
	var tells int
	if err := g.Raw(`SELECT count(*) FROM dod_event`).Scan(&tells).Error; err != nil {
		t.Fatalf("count dod events: %v", err)
	}
	if tells != 8 {
		t.Errorf("dod_event rows = %d, want 8 (3 materialise + 1 mark + 4 for v2)", tells)
	}
	var resets int
	if err := g.Raw(`SELECT count(*) FROM dod_event WHERE op = 'reset' AND from_state = 'unchecked'`).Scan(&resets).Error; err != nil {
		t.Fatalf("count resets: %v", err)
	}
	if resets != 1 {
		t.Errorf("resets = %d, want 1 (the changed text)", resets)
	}
	// The ledger is append-only.
	for _, stmt := range []string{
		`UPDATE dod_event SET op = 'mark' WHERE 1=1`,
		`DELETE FROM dod_event`,
	} {
		err := g.Exec(stmt).Error
		if err == nil {
			t.Errorf("%s was allowed — the ledger audit is editable", stmt)
			continue
		}
		if !strings.Contains(err.Error(), "dod_event is append-only") {
			t.Errorf("%s refused without naming its table: %v", stmt, err)
		}
	}
}

// TestDoDDoors — the mark's doors: the actor's (a person or the engine,
// and nothing else), the state's closed set, an unknown item, a `met`
// without its evidence, and the manual/verify_run pairing — a manual
// mark by the engine and a run mark by a person are both refusals. A
// non-met mark clears the evidence, and a no-op mark writes nothing.
func TestDoDDoors(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "dod2-root")
	sc := seedLoop(t, s, "dod2-loop", "dod2-root")
	user := s57User
	engine := Actor{Kind: "trigger", ID: "engine"}
	agent := Actor{Kind: "agent", ID: "a1"}

	items, err := s.MaterializeDoD(sc, DoDBrief{BriefID: "brf-d1", BriefVersion: 1, Items: []string{"one", "two"}})
	if err != nil || items != 2 {
		t.Fatalf("seed the list: wrote %d err %v", items, err)
	}
	list, err := s.ListDoD(sc)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v", err)
	}
	id := list[0].ID

	// The actor's door (S57 T9): an agent MAY mark — the pairing below
	// decides what it may mark ON (a person's manual word is still not
	// its to claim); an anonymous author still cannot.
	if w, err := s.MarkDoD(agent, id, "met", DodEvidence{Kind: "manual", Ref: "sure"}); err != nil || !isDodRefusal(w, DodRefusalEvidenceActorMismatch) {
		t.Fatalf("agent manual met: err %v, want evidence-actor-mismatch", err)
	}
	if w, err := s.MarkDoD(Actor{Kind: "user"}, id, "met", DodEvidence{Kind: "manual", Ref: "sure"}); err != nil || !isDodRefusal(w, DodRefusalActorNotMarking) {
		t.Fatalf("anonymous mark: err %v, want actor-not-marking", err)
	}
	// An agent's non-met mark is its own to record; its note rides the
	// audit row (evidence is NULL on a non-met).
	if w, err := s.MarkDoD(agent, id, "unverifiable", DodEvidence{Note: "the gate hangs"}); err != nil || !w.OK() {
		t.Fatalf("agent unverifiable: %v %v", w, err)
	}
	var evNote string
	if err := g.Raw(`SELECT evidence FROM dod_event WHERE item_id = ? AND op = 'mark' ORDER BY rowid DESC LIMIT 1`, id).Scan(&evNote).Error; err != nil {
		t.Fatalf("read the mark's audit row: %v", err)
	}
	if evNote != "note: the gate hangs" {
		t.Errorf("the note's audit row = %q, want %q", evNote, "note: the gate hangs")
	}
	// The state's closed set.
	if w, err := s.MarkDoD(user, id, "met-ish", DodEvidence{}); err != nil || !isDodRefusal(w, DodRefusalStateInvalid) {
		t.Fatalf("bad state: err %v, want state-invalid", err)
	}
	// The unknown item.
	if w, err := s.MarkDoD(user, "dod-nope", "met", DodEvidence{Kind: "manual", Ref: "x"}); err != nil || !isDodRefusal(w, DodRefusalItemUnknown) {
		t.Fatalf("unknown item: err %v, want item-unknown", err)
	}
	// A met names its evidence — kind AND ref.
	if w, err := s.MarkDoD(user, id, "met", DodEvidence{}); err != nil || !isDodRefusal(w, DodRefusalMetWithoutEvidence) {
		t.Fatalf("met without kind: err %v, want met-without-evidence", err)
	}
	if w, err := s.MarkDoD(user, id, "met", DodEvidence{Kind: "green"}); err != nil || !isDodRefusal(w, DodRefusalMetWithoutEvidence) {
		t.Fatalf("met with a bad kind: err %v, want met-without-evidence", err)
	}
	if w, err := s.MarkDoD(user, id, "met", DodEvidence{Kind: "manual", Ref: ""}); err != nil || !isDodRefusal(w, DodRefusalMetWithoutEvidence) {
		t.Fatalf("met with an empty ref: err %v, want met-without-evidence", err)
	}
	// The pairing: manual is a person's word, verify_run the engine's.
	if w, err := s.MarkDoD(engine, id, "met", DodEvidence{Kind: "manual", Ref: "run 42"}); err != nil || !isDodRefusal(w, DodRefusalEvidenceActorMismatch) {
		t.Fatalf("manual by the engine: err %v, want evidence-actor-mismatch", err)
	}
	if w, err := s.MarkDoD(user, id, "met", DodEvidence{Kind: "verify_run", Ref: "vr-1"}); err != nil || !isDodRefusal(w, DodRefusalEvidenceActorMismatch) {
		t.Fatalf("run by a person: err %v, want evidence-actor-mismatch", err)
	}
	// The good marks, each the kind its actor may make.
	if w, err := s.MarkDoD(user, id, "met", DodEvidence{Kind: "manual", Ref: "I ran it"}); err != nil || !w.OK() {
		t.Fatalf("user manual met: %v %v", w, err)
	}
	// A non-met clears the evidence.
	wu, err := s.MarkDoD(user, id, "unmet", DodEvidence{})
	if err != nil || !wu.OK() {
		t.Fatalf("mark unmet: %v %v", wu, err)
	}
	got := wu.Item
	if got.State != "unmet" || got.EvidenceKind != "" || got.EvidenceRef != "" {
		t.Errorf("a non-met clears the evidence: %+v", got)
	}
	// The engine's own run mark.
	if w, err := s.MarkDoD(engine, id, "met", DodEvidence{Kind: "verify_run", Ref: "vr-9"}); err != nil || !w.OK() {
		t.Fatalf("engine run met: %v %v", w, err)
	}
	got, err = s.GetDoDItem(id)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.EvidenceKind != "verify_run" || got.EvidenceRef != "vr-9" || got.ActorKind == nil || *got.ActorKind != "trigger" {
		t.Errorf("the run mark stands with its marker: %+v", got)
	}
	// The no-op writes nothing.
	before := dodEventCount(t, g, id)
	if w, err := s.MarkDoD(engine, id, "met", DodEvidence{Kind: "verify_run", Ref: "vr-9"}); err != nil || !w.OK() {
		t.Fatalf("no-op mark: %v %v", w, err)
	}
	if after := dodEventCount(t, g, id); after != before {
		t.Errorf("the no-op mark wrote an event: %d -> %d", before, after)
	}
	// The agent's run marks (S57 T9): a PASSING run of the item's OWN
	// loop is its evidence (the store's half of the head-currency check;
	// the daemon's git half is the service's); a failing run, another
	// loop's run, or a run that does not exist is a refusal that names
	// the run and the reason.
	finishRun := func(loopSlug, state string, position int) string {
		t.Helper()
		d := s57NewDraft(t, s, NewVerifyRun{
			LoopSlug: loopSlug, ComponentRoot: "dod2-root", ComponentID: "dod2-root",
			Task: "VT1", Position: position, Attempt: 1, HeadSHA: "abc123", Trigger: "manual", StartedAt: time.Now(),
		})
		run, err := s.FinishVerifyRun(d, state, "", "", "", time.Now())
		if err != nil {
			t.Fatalf("finish the %s run: %v", state, err)
		}
		return run.ID
	}
	prun := finishRun("dod2-loop", "pass", 0)
	if w, err := s.MarkDoD(agent, id, "met", DodEvidence{Kind: "verify_run", Ref: prun}); err != nil || !w.OK() {
		t.Fatalf("agent run met: %v %v", w, err)
	}
	got, err = s.GetDoDItem(id)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if got.State != "met" || got.EvidenceKind != "verify_run" || got.EvidenceRef != prun ||
		got.ActorKind == nil || *got.ActorKind != "agent" || got.ActorID == nil || *got.ActorID != "a1" {
		t.Errorf("the agent's run mark stands with its marker: %+v", got)
	}
	frun := finishRun("dod2-loop", "fail", 1)
	if w, err := s.MarkDoD(agent, id, "met", DodEvidence{Kind: "verify_run", Ref: frun}); err != nil || !isDodRefusal(w, DodRefusalMetWithoutEvidence) {
		t.Errorf("a failing run is not met-evidence: %v", w.Reason)
	}
	orun := finishRun("elsewhere", "pass", 2)
	if w, err := s.MarkDoD(agent, id, "met", DodEvidence{Kind: "verify_run", Ref: orun}); err != nil || !isDodRefusal(w, DodRefusalMetWithoutEvidence) {
		t.Errorf("another loop's run is not this item's evidence: %v", w.Reason)
	}
	if w, err := s.MarkDoD(agent, id, "met", DodEvidence{Kind: "verify_run", Ref: "vr-none"}); err != nil || !isDodRefusal(w, DodRefusalMetWithoutEvidence) {
		t.Errorf("a missing run is not met-evidence: %v", w.Reason)
	}
	// Nothing the refusals touched moved: the item is the run's, and the
	// other item is still the loop's unchecked one.
	if got, _ := s.GetDoDItem(list[1].ID); got.State != "unchecked" {
		t.Errorf("the doors touched a neighbour: %+v", got)
	}
}

// dodEventCount is the one item's audit depth, straight from the table
// (the drill counts the tellings; the store API's audit read is the
// T8/T11 concern).
func dodEventCount(t *testing.T, g *gorm.DB, itemID string) int {
	t.Helper()
	var n int
	if err := g.Raw(`SELECT count(*) FROM dod_event WHERE item_id = ?`, itemID).Scan(&n).Error; err != nil {
		t.Fatalf("count dod events for %s: %v", itemID, err)
	}
	return n
}

// TestDoDMaterialisationDoors — the materialisation's own refusals: a
// half-named loop, a loop that is not there, and a brief without its
// identity.
func TestDoDMaterialisationDoors(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "dod3-root")
	seedLoop(t, s, "dod3-loop", "dod3-root")

	if _, err := s.MaterializeDoD(Scope{LoopSlug: "dod3-loop"}, DoDBrief{BriefID: "b", BriefVersion: 1, Items: []string{"x"}}); err == nil {
		t.Errorf("a slug alone materialised — a half-named loop is not a loop")
	}
	if _, err := s.MaterializeDoD(Scope{LoopSlug: "nowhere", ComponentRoot: "dod3-root"}, DoDBrief{BriefID: "b", BriefVersion: 1, Items: []string{"x"}}); err == nil {
		t.Errorf("a loop that is not there materialised")
	}
	if _, err := s.MaterializeDoD(Scope{LoopSlug: "dod3-loop", ComponentRoot: "dod3-root"}, DoDBrief{BriefID: "", BriefVersion: 1, Items: []string{"x"}}); err == nil {
		t.Errorf("a brief without an id materialised")
	}
	if _, err := s.MaterializeDoD(Scope{LoopSlug: "dod3-loop", ComponentRoot: "dod3-root"}, DoDBrief{BriefID: "b", BriefVersion: 0, Items: []string{"x"}}); err == nil {
		t.Errorf("a brief at version 0 materialised")
	}
	// And the list read's door: the half-named loop.
	if _, err := s.ListDoD(Scope{ComponentRoot: "dod3-root"}); err == nil {
		t.Errorf("a slug alone listed the ledger")
	}
}
