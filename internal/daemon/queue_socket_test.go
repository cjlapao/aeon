package daemon

// S24 T7 — the daemon side of the queue's socket surface: the seams
// initQueue fills, and the two doors whose identity the client-side gates
// depend on (advance = the same advanceNext; blueprint validation =
// loop.start's own resolver, so the two doors cannot drift).

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"

	"github.com/cjlapao/aeon/internal/queue"
)

func TestQueueSeamsAreWired(t *testing.T) {
	f := newReactorFixture(t)
	if f.d.Table.Queue == nil {
		t.Fatal("Table.Queue is nil — the socket handlers would answer ErrStoreUnavailable forever (CG9's surface is dead)")
	}
	if f.d.Table.QueueAdvance == nil {
		t.Fatal("Table.QueueAdvance is nil — queue.advance has no door")
	}
	if f.d.Table.QueueValidateBlueprint == nil {
		t.Fatal("Table.QueueValidateBlueprint is nil — queue.append would answer any name")
	}
}

func TestQueueAdvanceDoorIsTheMaterializationDoor(t *testing.T) {
	f := newReactorFixture(t)
	f.appendReady(t, "advance me", nil)
	started, err := f.d.Table.QueueAdvance(context.Background(), f.root)
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !started {
		t.Fatal("a ready item with a free slot did not start — the door is not the door")
	}
	// D2: the second call finds the slot taken and says so — `started=false`
	// is an answer, not a failure.
	again, err := f.d.Table.QueueAdvance(context.Background(), f.root)
	if err != nil {
		t.Fatalf("second advance: %v", err)
	}
	if again {
		t.Fatal("two materializations from one root — D2's slot is not one")
	}
	if _, err := f.d.Table.QueueAdvance(context.Background(), ""); err == nil {
		t.Fatal("an advance naming no project was accepted — every queue fact is per-project (CG8)")
	}
}

// TestQueueAppendValidatesBlueprintTheWayLoopStartDoes is the server half
// of T6's identical-sentence gate: the queue's validator IS loop.start's
// resolver (blueprint.Resolve behind the same seam), so an unknown name
// answers with the SAME sentence from both doors by construction — the
// client-side equality test (cmd/aeon, T6) rides this identity.
func TestQueueAppendValidatesBlueprintTheWayLoopStartDoes(t *testing.T) {
	f := newReactorFixture(t)
	if err := f.d.Table.QueueValidateBlueprint("dev-loop"); err != nil {
		t.Fatalf("a shipped name refused: %v", err)
	}
	_, wantErr := f.d.loadStartBlueprint("nope-not-a-blueprint")
	if wantErr == nil {
		t.Fatal("the loader accepted a name with no file — the test's premise is the bug")
	}
	gotErr := f.d.Table.QueueValidateBlueprint("nope-not-a-blueprint")
	if gotErr == nil || !strings.Contains(gotErr.Error(), wantErr.Error()) {
		t.Fatalf("queue validation drifted from the resolver's own sentence:\n got %v\nwant it to contain %v", gotErr, wantErr)
	}
	// And the socket handler refuses it before a row is ever written.
	initConn(t, f.d, context.Background(), "c-t7", "t7")
	_, err := f.d.Table.Dispatch(context.Background(), "c-t7", protocol.MethodQueueAppend, mustJSON(t, protocol.QueueAppendParams{
		ComponentRoot: f.root, Blueprint: "nope-not-a-blueprint", Goal: "g",
	}))
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("queue.append answered an unknown blueprint differently than the resolver says: %v", err)
	}
	items, lerr := f.q.List(f.root)
	if lerr != nil {
		t.Fatalf("read: %v", lerr)
	}
	if len(items) != 0 {
		t.Fatalf("the refused append left %d row(s) — validation must precede the door, not follow it", len(items))
	}
}

// TestQueueAdvanceDoorHaltsOnAnyBlocked pins CG2 AT THE DOOR — the T9 walk
// (`DEMO.md` §S24, F-1) found `queue.advance` returning `started: true` and
// starting the second item while the first sat `blocked`: the D2
// one-running clause answered "what may start" and said nothing about a
// block. The store door now refuses both selections; this gate pins the
// consequence where the person (and the app) actually knock — the
// `queue.advance` seam — with the positive control that the SAME call
// starts an item once the block is cancelled (D12's door resolves it).
func TestQueueAdvanceDoorHaltsOnAnyBlocked(t *testing.T) {
	f := newReactorFixture(t)
	a := f.appendReady(t, "first intent", nil)
	f.appendReady(t, "second intent", nil)

	// The walk's real order: a starts, then its loop reports blocked.
	run, r := f.q.Transition(a, queue.StatusRunning)
	if r != nil {
		t.Fatalf("running the first: %s", r.Reason)
	}
	if _, r := f.q.Transition(run, queue.StatusBlocked); r != nil {
		t.Fatalf("blocking the first: %s", r.Reason)
	}
	// No item runs — D2's clause cannot be what refuses here.
	started, err := f.d.Table.QueueAdvance(context.Background(), f.root)
	if err != nil {
		t.Fatalf("advance over a blocked board: %v", err)
	}
	if started {
		t.Fatal("the explicit door started an item OVER a blocked row — CG2 halts unconditionally")
	}
	// The sizing dispatch halts with the queue (the walk's second half).
	if _, err := f.q.SetSettings(f.root, "auto", "skill"); err != nil {
		t.Fatal(err)
	}
	if _, aerr := f.q.Append(f.root, "dev-loop", "third intent", f.root, queue.NewItem{}); aerr != nil {
		t.Fatal(aerr)
	}
	if started, err = f.d.Table.QueueAdvance(context.Background(), f.root); err != nil || started {
		t.Fatalf("a sizing run dispatched over a blocked row: started=%v err=%v", started, err)
	}
	// Cancel the block (D12) — the same call now starts the second item.
	if _, r := f.q.Remove(a.ID); r != nil {
		t.Fatalf("cancelling the blocked row: %s", r.Reason)
	}
	if started, err = f.d.Table.QueueAdvance(context.Background(), f.root); err != nil || !started {
		t.Fatalf("the door stayed shut after the halt was resolved: started=%v err=%v", started, err)
	}
}
