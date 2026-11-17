package daemon

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// S41-notifications: a notify hint is delivery-time-only, reaches the
// subscribers that are actually there, and never leaks into what a plain
// `publish` or a cursor replay carries.

func TestPublishNotifyAttachesTheHintOnlyToTheLiveDelivery(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "watcher")
	subscribe(t, d, "c1", protocol.SubscribeParams{})

	before := len(w.delivered())
	d.PublishNotify("loop.finished", "", map[string]any{"loopId": "l1"},
		protocol.NotifyHint{Title: "Loop finished", Tone: protocol.NotifySuccess})

	var found *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == "loop.finished" {
			ev := s.Event
			found = &ev
		}
	}
	if found == nil {
		t.Fatal("loop.finished never reached the subscriber")
	}
	if found.Notify == nil {
		t.Fatal("PublishNotify did not attach a notify hint to the delivered event")
	}
	if found.Notify.Title != "Loop finished" || found.Notify.Tone != protocol.NotifySuccess {
		t.Errorf("unexpected hint: %+v", found.Notify)
	}
}

func TestPlainPublishCarriesNoNotifyHint(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "watcher")
	subscribe(t, d, "c1", protocol.SubscribeParams{})

	before := len(w.delivered())
	d.Publish("loop.advanced", "", map[string]any{"loopId": "l1"})

	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == "loop.advanced" && s.Event.Notify != nil {
			t.Fatalf("plain Publish must never carry a notify hint, got %+v", s.Event.Notify)
		}
	}
}

// The ring stores the event BEFORE the hint is attached (`publishWithNotify`
// mutates its own copy, after `Events.Append` already returned) — so a
// client that was not listening live and subscribes afterward with a cursor
// must see the same event with no hint. A reconnect's catch-up must not
// re-surface a toast for something already shown live.
func TestAReplayedEventCarriesNoNotifyHintEvenThoughTheLiveDeliveryDid(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "watcher")
	subscribe(t, d, "c1", protocol.SubscribeParams{})

	d.PublishNotify("loop.finished", "", map[string]any{"loopId": "l1"},
		protocol.NotifyHint{Title: "Loop finished", Tone: protocol.NotifySuccess})

	// A second client, arriving late, resumes from the very start.
	initConn(t, d, ctx, "c2", "latecomer")
	zero := uint64(0)
	before := len(w.delivered())
	subscribe(t, d, "c2", protocol.SubscribeParams{After: &zero})

	var replayed *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.ConnID == "c2" && s.Event.Kind == "loop.finished" {
			ev := s.Event
			replayed = &ev
		}
	}
	if replayed == nil {
		t.Fatal("the late subscriber never received the replayed loop.finished event")
	}
	if replayed.Notify != nil {
		t.Fatalf("a replayed event must never carry a notify hint, got %+v", replayed.Notify)
	}
}

// S58 T4: the closed curated set is now the class table — the resolver
// answers the same six examples (three curated kinds still raise, the
// uncurated and the unknown still do not). The sweep over the whole kind
// plane lives in lifecycle_hint_test.go.
func TestNotifyHintForIsAClosedCuratedSet(t *testing.T) {
	d, _, _ := newNotifyClassDaemon(t)
	cases := []struct {
		kind string
		want bool
	}{
		{protocol.KindLoopFinished, true},
		{protocol.KindLoopBlocked, true},
		{protocol.KindLoopWaiting, true},
		{protocol.KindQuestionAsked, true},
		{protocol.KindLoopAdvanced, false},
		{protocol.KindClientConnected, false},
		{"some.unknown.kind", false},
	}
	for _, c := range cases {
		hint := d.classes.HintFor(c.kind, map[string]any{})
		if (hint != nil) != c.want {
			t.Errorf("resolver(%q) = %+v, want present=%v", c.kind, hint, c.want)
		}
	}
}

// The real integration path: `table.OnEvent` is what `internal/runtime`
// calls for every domain event. This proves the class table actually
// reaches it — the resolver on the publish path, not a hint function in
// isolation.
func TestOnEventAttachesTheCuratedHintForALoopFinished(t *testing.T) {
	// newNotifyClassDaemon initializes c1 and subscribes it already.
	d, _, w := newNotifyClassDaemon(t)

	before := len(w.delivered())
	d.Table.OnEvent(protocol.KindLoopFinished, map[string]any{"loopId": "l1"})

	var found *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == protocol.KindLoopFinished {
			ev := s.Event
			found = &ev
		}
	}
	if found == nil {
		t.Fatal("loop.finished never reached the subscriber via table.OnEvent")
	}
	if found.Notify == nil {
		t.Fatal("table.OnEvent did not attach the curated notify hint for loop.finished")
	}
}

// And the common case — most kinds — is unaffected: table.OnEvent still
// delivers them, just without a hint.
func TestOnEventLeavesUncuratedKindsWithoutAHint(t *testing.T) {
	d, _, w := newNotifyClassDaemon(t)

	before := len(w.delivered())
	d.Table.OnEvent(protocol.KindLoopAdvanced, map[string]any{"loopId": "l1"})

	var found *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == protocol.KindLoopAdvanced {
			ev := s.Event
			found = &ev
		}
	}
	if found == nil {
		t.Fatal("loop.advanced never reached the subscriber via table.OnEvent")
	}
	if found.Notify != nil {
		t.Fatalf("loop.advanced is not in the curated set and must carry no hint, got %+v", found.Notify)
	}
}
