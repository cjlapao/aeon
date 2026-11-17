package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// idle_loop_test.go is S39 T4's second half: what the DAEMON promises about
// a resting loop. D9: idle is neither started nor recovered — the two
// startup sweeps select status='running', and an idle loop must be invisible
// to both (a conversation is not an interrupted walk). And loop.close is the
// human door out of rest: claims from idle, refuses everything else as data.

func restPinnedLoop(t *testing.T, s *store.Store, slug, root string) (string, store.Scope) {
	t.Helper()
	l, err := s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Root: root, Goal: "g"}, "rest", 1,
		"id: rest\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default}\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(l.ID, 1, "done", "{}"); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: slug, ComponentRoot: root}
	if err := s.SetLoopStatus(sc, "idle"); err != nil {
		t.Fatal(err)
	}
	return l.ID, sc
}

func TestAnIdleLoopIsNeitherStartedNorRecovered(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	id, _ := restPinnedLoop(t, s, "rest", root)
	d := New(Options{Store: s})
	// StartLoops starts the clock (S20 T2) — and a clock outliving the
	// t.Cleanup store close is a goroutine querying a closed database on
	// someone else's test. Cancel with the test.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := d.StartLoops(ctx); err != nil {
		t.Fatalf("StartLoops: %v", err)
	}
	if d.loopRunner != nil && d.loopRunner.Active(id) {
		t.Fatal("an idle loop was STARTED — D9: the sweep selects running; a resting conversation is not work to resume")
	}
	got, err := d.Recover(ctx, false)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got.Total != 0 {
		t.Fatalf("Recover summary Total=%d, want 0 — recovery reads running steps, and an idle loop has none", got.Total)
	}
	pin, err := s.ReadPinnedLoop(id)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Loop.Status != "idle" {
		t.Fatalf("the sweeps moved the resting loop to %q, want idle untouched", pin.Loop.Status)
	}
}

func TestLoopCloseMethod(t *testing.T) {
	f, _ := gatedFixture(t, nil)
	ctx := context.Background()
	id, _ := restPinnedLoop(t, f.s, "close-me", f.root)
	initConn(t, f.d, ctx, "c-close", "ui")
	sub := subscribe(t, f.d, "c-close", protocol.SubscribeParams{})

	res := dispatchJSON(t, f, "c-close", protocol.MethodLoopClose, map[string]any{"id": id})
	if res["status"] != "done" || res["id"] != id {
		t.Fatalf("loop.close answered %v, want {id, status:done}", res)
	}
	// The event side of the promise: closing a resting loop ENDS it, so
	// loop.finished rides — exactly the {loopId, status:done} payload.
	var sawFinish map[string]any
	for _, ev := range streamEvents(f.w, "c-close", sub.SubscriptionID) {
		if ev.Kind != "loop.finished" {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("loop.finished payload does not decode: %v", err)
		}
		sawFinish = p
	}
	if sawFinish == nil {
		t.Fatal("loop.close ended the loop but no loop.finished was delivered")
	}
	// publishLoop ADDS the durable row+snapshot view (documented additive
	// wire data); the engine's own payload is pinned byte-exact at
	// rest_test.go — here the promise fields must ride.
	if sawFinish["loopId"] != id || sawFinish["status"] != "done" {
		t.Fatalf("loop.finished lost its promise fields: %v", sawFinish)
	}

	// A running loop is refused as DATA with the daemon's sentence — never
	// a socket error the app has to interpret.
	running, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "busy", ComponentRoot: f.root, Root: f.root, Goal: "g"}, "rest", 1,
		"id: rest\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default}\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetLoopStatus(store.Scope{LoopSlug: "busy", ComponentRoot: f.root}, "running"); err != nil {
		t.Fatal(err)
	}
	res = dispatchJSON(t, f, "c-close", protocol.MethodLoopClose, map[string]any{"id": running.ID})
	refused, ok := res["refused"].(map[string]any)
	if !ok || refused["kind"] != "not-idle" {
		t.Fatalf("closing a running loop answered %v, want refused.kind=not-idle", res)
	}
	if detail, _ := refused["detail"].(string); detail == "" {
		t.Fatal("the refusal must carry the daemon's sentence (which status it found)")
	}
	// And the refusal changed nothing.
	pin, err := f.s.ReadPinnedLoop(running.ID)
	if err != nil || pin.Loop.Status != "running" {
		t.Fatalf("the refused close moved the loop to %q (err %v)", pin.Loop.Status, err)
	}
}
