package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// messages_test.go is S39 T5's daemon half (CR6 + CR10): the thread's
// publish door files loop.message.posted under the loop's own project root
// — a component-scoped subscriber must see its loop's conversation, not
// every loop's — and the steer views keep the byte-identical promise: a
// person's steer serialises exactly as it did before S39, the label rides
// only when there is something to say.

func TestSteerViewHidesTheDefaultSource(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         bool
	}{
		{"the default is the absent case", "user", false},
		{"history before S39", "", false},
		{"the engine's own push-back speaks for itself", "blueprint", true},
		{"the clock's steer says so", "automation", true},
	} {
		raw, err := json.Marshal(steerViewOf(store.Steer{ID: 1, StepID: 2, Text: "x", State: store.SteerPending, CreatedAt: "2026-10-27T00:00:00Z", Source: tc.source}))
		if err != nil {
			t.Fatal(err)
		}
		got := string(raw)
		if has := json.Valid(raw) && containsSourceLabel(got); has != tc.want {
			t.Fatalf("%s: source %q on the wire = %v, want %v — CR10: person's steers serialise byte-for-byte as before, only a labelled one carries \"source\": %s", tc.name, tc.source, has, tc.want, got)
		}
	}
	// The unlabelled view is not merely label-free — it is the OLD bytes:
	// every key the S38 steer frame had, and no other.
	raw, err := json.Marshal(steerViewOf(store.Steer{ID: 1, StepID: 2, Text: "x", State: store.SteerPending, CreatedAt: "2026-10-27T00:00:00Z", Source: "user"}))
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		if k == "source" {
			t.Fatalf("a user's steer grew a wire key: %s", raw)
		}
	}
}

// containsSourceLabel is deliberate: "source" as a VALUE must not count,
// only the key.
func containsSourceLabel(raw string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	_, ok := m["source"]
	return ok
}

func TestPublishMessageFilesTheEventUnderTheLoopsRoot(t *testing.T) {
	f := newStreamFixture(t, nil)
	ctx := context.Background()
	l, err := f.s.CreatePinnedLoop(store.NewLoop{Slug: "thread", ComponentRoot: f.root, Root: f.root, Goal: "g"}, "rest", 1,
		"id: rest\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: only\n    step: {kind: model-step, agent: default}\n", "{}")
	if err != nil {
		t.Fatal(err)
	}
	initConn(t, f.d, ctx, "c-msg", "ui")
	sub := subscribe(t, f.d, "c-msg", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: f.root}})

	f.d.publishMessage(l.ID, store.ChatMessage{
		ID: 41, LoopSlug: "thread", ComponentRoot: f.root, Seq: 7,
		Author: "human", Kind: "note", Body: "ok, create the goal", Source: "user",
	})

	// A DIFFERENT project's subscriber must NOT receive it — the root
	// filing is the whole point of the test.
	initConn(t, f.d, ctx, "c-other", "other-ui")
	subOther := subscribe(t, f.d, "c-other", protocol.SubscribeParams{Filter: protocol.Filter{ComponentRoot: "/somewhere/else"}})

	var mine []protocol.Event
	for _, ev := range streamEvents(f.w, "c-msg", sub.SubscriptionID) {
		if ev.Kind == protocol.KindLoopMessagePosted {
			mine = append(mine, ev)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("the loop's own subscriber received %d loop.message.posted, want 1", len(mine))
	}
	ev := mine[0]
	if ev.ComponentRoot != f.root {
		t.Fatalf("the event is filed under root %q, want the loop's own %q", ev.ComponentRoot, f.root)
	}
	var p map[string]any
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"loopId": l.ID, "messageId": float64(41), "seq": float64(7),
		"author": "human", "kind": "note", "source": "user", "body": "ok, create the goal",
	} {
		if p[key] != want {
			t.Fatalf("payload[%q] = %v, want %v", key, p[key], want)
		}
	}
	for _, ev := range streamEvents(f.w, "c-other", subOther.SubscriptionID) {
		if ev.Kind == protocol.KindLoopMessagePosted {
			t.Fatal("another project's subscriber received the loop's message — the event is not filed under its loop's root")
		}
	}
}
