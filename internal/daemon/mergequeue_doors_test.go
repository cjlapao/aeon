package daemon

// mergequeue_doors_test.go — S60 T8: the socket doors over the REAL store,
// the REAL repositories and the SAME driver T7 proved. The protocol-package
// tests proved the wire shape (V13, the identity-key refusals, the actor
// stamp, the four codes); what lives HERE is the door's BEHAVIOUR list, the
// sentences the spec demands and only a live daemon can answer:
//
//   - settings to auto needs the DAEMON's sentence, applied only on its
//     acknowledged digest (refusal first, action on the second round);
//   - land rechecks ready, binds to the head and position the caller SAW,
//     refuses while the queue is dark, and — when it acts — moves the lane
//     through the driver (the forge.land ask stays inside the loop);
//   - dequeue puts the item's live loops down FIRST;
//   - the actor on every event the doors stamp is the peer, kind user;
//   - the vocabulary is the driver's tables, derived.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var mqDoorActor = protocol.Actor{Kind: "user", ID: "cjlapao"}

func mqDoorsErr(t *testing.T, err error, code int) *protocol.Error {
	t.Helper()
	t.Fatalf("want code %d, got %v", code, err)
	return nil
}

func TestDoorSettingsNeedsTheDaemonStatement(t *testing.T) {
	e := newMQEnv(t)
	e.configure(t, "manual", "ask", 3)
	doors := newMergeQueueDoors(e.d)
	ctx := context.Background()

	// A plain, non-widening write applies at once: the land METHOD is a
	// preference, not an authority.
	cfg, _, err := doors.Settings(ctx, mqDoorActor, protocol.MergeQueueSettingsParams{
		ComponentRoot: e.ws, LandMethod: "merge"})
	if err != nil {
		t.Fatalf("a land-method change is nobody's authority to gate: %v", err)
	}
	if cfg.LandMethod != "merge" || cfg.Mode != "manual" {
		t.Fatalf("settings answered %+v", cfg)
	}

	// AUTO is authority-widening: the daemon refuses with its OWN sentence
	// and the digest to send back — and NOTHING was written.
	target := mergequeue.Settings{
		ComponentRoot: e.ws, Mode: mergequeue.ModeAuto,
		ConflictPolicy: mergequeue.PolicyAsk, LandMethod: mergequeue.LandMethod("merge"), MaxCatchUps: 3,
	}
	sentence := mergequeue.Statement(target)
	_, _, err = doors.Settings(ctx, mqDoorActor, protocol.MergeQueueSettingsParams{
		ComponentRoot: e.ws, Mode: "auto"})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueRefused.Code {
		mqDoorsErr(t, err, protocol.ErrMergeQueueRefused.Code)
	}
	if !strings.Contains(perr.Data.What, sentence) {
		t.Errorf("the refusal did not carry the daemon's own sentence:\n got  %q\n want it to contain %q", perr.Data.What, sentence)
	}
	digest := mqStatementDigest(sentence)
	if !strings.Contains(perr.Data.What, digest) {
		t.Errorf("the refusal did not name the digest to send back: %q", perr.Data.What)
	}
	if cur, _ := e.s.ReadMergeConfig(e.ws); cur.Mode == "auto" {
		t.Fatal("the un-acknowledged auto WIDENED the queue — the refusal wrote")
	}

	// The second round: the same request, now carrying the acknowledgement.
	// The row moves, and the answer quotes the same sentence.
	cfg, stmt, err := doors.Settings(ctx, mqDoorActor, protocol.MergeQueueSettingsParams{
		ComponentRoot: e.ws, Mode: "auto", Acknowledged: digest})
	if err != nil {
		t.Fatalf("the acknowledged auto did not apply: %v", err)
	}
	if cfg.Mode != "auto" {
		t.Fatalf("settings answered %+v after the acknowledgement", cfg)
	}
	if stmt != sentence {
		t.Errorf("the applied answer re-rendered the statement (one formatter):\n got  %q\n want %q", stmt, sentence)
	}
	// The audit trail: the config row names the person, kind user.
	cur, err := e.s.ReadMergeConfig(e.ws)
	if err != nil || cur.UpdatedByKind != "user" || cur.UpdatedByID != "cjlapao" {
		t.Fatalf("settings write was not stamped by the person: %+v %v", cur, err)
	}

	// A policy widening (ask → resolve) follows the same law.
	_, _, err = doors.Settings(ctx, mqDoorActor, protocol.MergeQueueSettingsParams{
		ComponentRoot: e.ws, ConflictPolicy: "resolve"})
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueRefused.Code {
		t.Fatalf("policy-to-resolve without an acknowledgement answered %v, want the plane's refusal", err)
	}
	// A bad enum is a refusal too, in the store's own words.
	_, _, err = doors.Settings(ctx, mqDoorActor, protocol.MergeQueueSettingsParams{
		ComponentRoot: e.ws, Mode: "turbo"})
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueRefused.Code {
		t.Fatalf("mode %q answered %v — bad-config is ErrMergeQueueRefused carrying the store's sentence", "turbo", err)
	}
}

func TestDoorEnqueueStampsThePeerAndBindsToTheRepository(t *testing.T) {
	e := newMQEnv(t)
	doors := newMergeQueueDoors(e.d)
	ctx := context.Background()
	f := e.feature(t, "feat-door-enqueue", "")

	view, err := doors.Enqueue(ctx, mqDoorActor, protocol.MergeQueueEnqueueParams{
		ComponentRoot: e.ws, BaseBranch: "main", FeatureBranch: f, Attended: false})
	if err != nil {
		t.Fatalf("door enqueue: %v", err)
	}
	if view.Status != "queued" || view.EnqueuedBy.Kind != "user" || view.EnqueuedBy.ID != "cjlapao" {
		t.Fatalf("the row answered %+v — the person at the socket is the author", view)
	}
	if view.NextStep == "" || view.Digest == "" {
		t.Error("the row answered without its driver sentence or its land digest")
	}

	// Idempotent on the open branch (MQ14): the person's repeat reuses the row.
	again, err := doors.Enqueue(ctx, mqDoorActor, protocol.MergeQueueEnqueueParams{
		ComponentRoot: e.ws, BaseBranch: "main", FeatureBranch: f, Attended: false})
	if err != nil || again.ID != view.ID {
		t.Fatalf("the repeat enqueue answered %+v (err %v), want the same open row %s", again, err, view.ID)
	}

	// A base the repository does not carry is ErrMergeQueueBaseUnknown, in
	// the validator's words, and nothing enqueued. (A FRESH branch: on the
	// open one the idempotent reuse answers before validation, as it must.)
	f2 := e.feature(t, "feat-door-badbase", "")
	_, err = doors.Enqueue(ctx, mqDoorActor, protocol.MergeQueueEnqueueParams{
		ComponentRoot: e.ws, BaseBranch: "release", FeatureBranch: f2, Attended: false})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueBaseUnknown.Code {
		t.Fatalf("unknown base answered %v, want -31954", err)
	}
	// A branch that exists nowhere is refused by name.
	_, err = doors.Enqueue(ctx, mqDoorActor, protocol.MergeQueueEnqueueParams{
		ComponentRoot: e.ws, BaseBranch: "main", FeatureBranch: "feature/never-was", Attended: false})
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueRefused.Code ||
		!strings.Contains(perr.Data.What, "feature/never-was") {
		t.Fatalf("missing branch answered %v, want -31952 naming the branch", err)
	}

	// The show door answers the ledger, the digest, and no forbidden bytes.
	show, err := doors.Show(ctx, protocol.MergeQueueShowParams{ID: view.ID})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if len(show.Transitions) == 0 || show.Transitions[0].Kind != "transition" ||
		show.Transitions[0].ToStatus != "queued" ||
		show.Transitions[0].ActorKind != "user" || show.Transitions[0].ActorID != "cjlapao" {
		t.Fatalf("the ledger lost the person's stamp: %+v", show.Transitions)
	}
	if show.Item.Digest != mqLandDigest(e.item(t, view.ID)) {
		t.Error("the show digest and the row's recompute disagree — the land binding is a lie")
	}
	raw, _ := json.Marshal(show)
	for _, bad := range []string{"diff", "hunk", "patch"} {
		if strings.Contains(strings.ToLower(string(raw)), bad) {
			t.Errorf("SEC-033: the show answer carries %q", bad)
		}
	}

	// The status door answers settings, counts and lane heads for the root.
	st, err := doors.Status(ctx, protocol.MergeQueueStatusParams{ComponentRoot: e.ws})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Counts["queued"] != 1 || len(st.Lanes) != 1 || st.Lanes[0].Base != "main" || st.Lanes[0].HeadItem != view.ID {
		t.Fatalf("status answered %+v", st)
	}

	// The vocabulary door is the DRIVER's tables, derived: nine states, and
	// queued's legal next is exactly what mergequeue.Legal carries.
	vocab, err := doors.Vocabulary(ctx)
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if len(vocab.States) != len(mergequeue.StatusSet) {
		t.Fatalf("the vocabulary shows %d states, the driver carries %d", len(vocab.States), len(mergequeue.StatusSet))
	}
	for _, s := range vocab.States {
		if s.Name == "queued" {
			if strings.Join(s.LegalNext, ",") != "catching_up,removed" {
				t.Errorf("queued's legal next is %v — probe mergequeue.Legal, do not hand-copy", s.LegalNext)
			}
		}
		if s.Meaning == "" || len(s.LegalNext) == 0 && s.Name != "merged" && s.Name != "removed" {
			t.Errorf("state %s answered meaning %q with edges %v", s.Name, s.Meaning, s.LegalNext)
		}
	}
	if len(vocab.Methods) != 0 {
		t.Errorf("the service answers the CLOSED sets; the door half of the methods table is derived in the protocol handler — a second copy here is a second truth")
	}
}

func TestDoorLandBindsToTheDisplayedItem(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	doors := newMergeQueueDoors(e.d)
	ctx := context.Background()
	f := e.feature(t, "feat-door-land", "")
	it := e.enqueueAttended(t, f, false)
	got := e.waitFor(t, it.ID, "ready")

	// The digest from THIS read is the only key that fits.
	row := e.item(t, it.ID)
	show, err := doors.Show(ctx, protocol.MergeQueueShowParams{ID: it.ID})
	if err != nil || show.Item.Digest != mqLandDigest(row) {
		t.Fatalf("show after ready: %+v %v", show, err)
	}
	_, err = doors.Land(ctx, mqDoorActor, protocol.MergeQueueLandParams{ID: it.ID, Digest: strings.Repeat("0", 64)})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueRefused.Code ||
		!strings.Contains(perr.Data.What, "moved since") {
		t.Fatalf("a stale digest answered %v, want -31952 naming the move", err)
	}
	if e.item(t, it.ID).Status != "ready" {
		t.Fatal("the wrong-digest land moved the lane")
	}

	// The queue dark (mode off): not even the right key opens it.
	e.configure(t, "off", "ask", 3)
	_, err = doors.Land(ctx, mqDoorActor, protocol.MergeQueueLandParams{ID: it.ID, Digest: show.Item.Digest})
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueRefused.Code ||
		!strings.Contains(perr.Data.What, "dark") {
		t.Fatalf("land while the queue is dark answered %v, want -31952 (mode off)", err)
	}
	e.configure(t, "manual", "ask", 3)

	// The right key on the ready row: the door records the demand, answers
	// the loop's standing land-check gate with its own merge-word, and the
	// DRIVER moves the lane. The gate question still asks inside the loop
	// (S11/S56): the test answers it through the loop plane, the way the
	// person's other door would.
	if _, err := doors.Land(ctx, mqDoorActor, protocol.MergeQueueLandParams{ID: it.ID, Digest: show.Item.Digest}); err != nil {
		t.Fatalf("land with the bound digest: %v", err)
	}
	// The lane may already be through by the time we look — merging and
	// merged are both "the demand was consumed"; the loop's slug stands in
	// either case.
	got = e.waitFor(t, it.ID, "catching_up", "verifying", "merging", "merged")
	slug := got.LandingLoopSlug
	if slug == "" {
		t.Fatalf("the demand did not start a landing loop: %+v", got)
	}
	// The door answers the loop's standing land-check gate with its own
	// merge-word. If the question had not stood yet at that instant (the
	// loop parks a moment later), the person's OTHER door — loop.answer —
	// answers here; the test only speaks when a question still stands.
	if q, found, err := e.s.PendingQuestion(store.Scope{ComponentRoot: e.ws, LoopSlug: slug}); err == nil && found && !q.Answered() {
		loopID := e.waitWaiting(t, slug)
		e.answerGate(t, loopID, "merge-now")
	}
	e.settle(t)
	e.waitFor(t, it.ID, "merged")
	fresh := e.item(t, it.ID)
	if fresh.LandedSHA == "" {
		t.Fatal("landed with no landed_sha — the queue forgot where it landed")
	}

	// A merged item answers land with ConflictState once the fresh digest
	// is presented (the row's own current state is the check that speaks).
	after, err := doors.Show(ctx, protocol.MergeQueueShowParams{ID: it.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = doors.Land(ctx, mqDoorActor, protocol.MergeQueueLandParams{ID: it.ID, Digest: after.Item.Digest})
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueConflictState.Code {
		t.Fatalf("land on a merged item answered %v, want -31955", err)
	}
	_ = row
}

func TestDoorDequeuePutsTheLiveLoopsDownFirst(t *testing.T) {
	e := newMQEnv(t)
	e.grant(t)
	e.configure(t, "manual", "ask", 3)
	doors := newMergeQueueDoors(e.d)
	ctx := context.Background()
	f := e.feature(t, "feat-door-dequeue", "")
	it := e.enqueueAttended(t, f, false)
	got := e.waitFor(t, it.ID, "catching_up", "verifying", "ready")
	slug := got.LandingLoopSlug
	if slug == "" {
		t.Fatalf("no live loop to put down: %+v", got)
	}
	view, err := doors.Dequeue(ctx, mqDoorActor, protocol.MergeQueueIDParams{ID: it.ID})
	if err != nil {
		t.Fatalf("door dequeue: %v", err)
	}
	if view.Status != "removed" || view.Reason != "removed-by-person" {
		t.Fatalf("the dequeued row answered %+v", view)
	}
	// The loop is down NOW, not on the next sweep: the door put it down
	// before the row moved.
	e.settle(t)
	if st, ok := e.loopStateFor(t, slug); ok && st.Status != "aborted" && st.Status != "cancelled" && st.Status != "done" && st.Status != "idle" {
		t.Fatalf("the dequeued item's loop %s lives on at %s — the door must put it down before the row moves", slug, st.Status)
	}
	if e.mergeCalls() != 0 {
		t.Fatalf("a dequeued item still reached the forge: %d merge calls", e.mergeCalls())
	}
	// The event carries the PERSON, kind user — the socket's peer.
	events, err := e.s.ListMergeQueueEvents(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.ActorKind != "user" || last.ActorID != "cjlapao" {
		t.Errorf("the removal event is stamped by %s/%s — the dequeue is the person's act", last.ActorKind, last.ActorID)
	}
}

func TestDoorRetryMovesAFailedItemBack(t *testing.T) {
	e := newMQEnv(t)
	doors := newMergeQueueDoors(e.d)
	ctx := context.Background()
	f := e.feature(t, "feat-door-retry", "")
	it := e.enqueueAttended(t, f, false)
	// Fail it by the store's own door: a removed item is the failed row's
	// shape for this test — seed failed through the service's transitions.
	svc := mergequeue.NewService(e.s)
	queued := e.item(t, it.ID)
	got, err := svc.Transition(it.ID, mergequeue.StatusCatchingUp, store.TransitionFacts{}, mqMergeActor(), queued)
	if err != nil {
		t.Fatalf("seed catching_up: %v", err)
	}
	got, err = svc.Transition(it.ID, mergequeue.StatusVerifying, store.TransitionFacts{}, mqMergeActor(), got)
	if err != nil {
		t.Fatalf("seed verifying: %v", err)
	}
	if _, err := svc.Transition(it.ID, mergequeue.StatusFailed,
		store.TransitionFacts{Reason: "gate-failed"}, mqMergeActor(), got); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	view, err := doors.Retry(ctx, mqDoorActor, protocol.MergeQueueIDParams{ID: it.ID})
	if err != nil {
		t.Fatalf("door retry: %v", err)
	}
	if view.Status != "queued" {
		t.Fatalf("retry answered %+v — the promise is the state", view)
	}
	// From any other state it refuses with ConflictState, naming the state.
	_, err = doors.Retry(ctx, mqDoorActor, protocol.MergeQueueIDParams{ID: it.ID})
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrMergeQueueConflictState.Code {
		t.Fatalf("retry off a queued item answered %v, want -31955", err)
	}
	if !strings.Contains(perr.Data.What, "queued") {
		t.Errorf("the conflict refusal did not name the state the row is in: %q", perr.Data.What)
	}
}

// TestDoorUnknownIdSpeaksNotFound is T10's measured fix-forward: every door
// that reads a row BY ID must answer the plane's own -31953 when no row
// answers — the live walk of 2026-11-04 caught the store's raw
// ErrNotFound slipping through mqDoorError as a bare JSON-RPC -32603 with
// three empty parts, the one error shape this plane's Do-Not list rules
// out ("refusals verbatim" cannot mean bytes nobody can read).
func TestDoorUnknownIdSpeaksNotFound(t *testing.T) {
	e := newMQEnv(t)
	e.configure(t, "manual", "ask", 3)
	doors := newMergeQueueDoors(e.d)
	ctx := context.Background()

	_, err := doors.Show(ctx, protocol.MergeQueueShowParams{ID: "itm_nobody"})
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.ErrMergeQueueNotFound.Code {
		t.Fatalf("show on a missing id answered %v, want %d", err, protocol.ErrMergeQueueNotFound.Code)
	}
	if !strings.Contains(pe.Data.What, "itm_nobody") {
		t.Errorf("the sentence must name the id the caller rang: %+v", pe.Data)
	}
	_, err = doors.Dequeue(ctx, mqDoorActor, protocol.MergeQueueIDParams{ID: "itm_nobody"})
	if !errors.As(err, &pe) || pe.Code != protocol.ErrMergeQueueNotFound.Code {
		t.Fatalf("dequeue on a missing id answered %v, want %d", err, protocol.ErrMergeQueueNotFound.Code)
	}
}
