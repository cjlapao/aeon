package daemon

// S58 T1 — characterization goldens of the notify plane AS T1 FOUND IT.
// Header, per tasks/T1.md: T4 replaces notifyHintFor with the class
// resolver; title/tone must stay byte-identical for the curated kinds,
// attentionKey/deepLink are ADDED to the wire hint, and hint-less events
// become class-driven. Every byte that changes under T4 changes in the
// file named here and nowhere else — these pins are the proof of what
// T4 did, so they are golden files, not assertions I could rewrite from
// memory while making the change.
//
// The finding these goldens record (S58 T0, F3): the daemon's loop emit
// path is loop.Deps.Event → d.publishLoop → d.publish (blueprint.go:29,
// :490-505), and the daemon's own publish NEVER consults notifyHintFor —
// budget_seam.go:138-141 says so in its own words. The curated switch
// bites at exactly two places: the table.OnEvent door (daemon.go:1229),
// and emitters that thread the hint themselves (publishBudgetEvent,
// watchdogService). So today the FOUR loop-lifecycle kinds reach live
// subscribers WITHOUT a hint through the path the loops actually take —
// the publish-* rows below pin that absence, and T4's resolver is what
// must make them carry hints. The onEvent-* rows pin the hint-bearing
// door's bytes for the same kinds: the titles T4 must reproduce
// byte-identically.
//
//   go test ./internal/daemon/ -run TestNotifyDeliveryGoldens -count=1 -race
//   go test ./internal/daemon/ -run TestNotifyDeliveryGoldens -count=1 -race -update-notify-goldens

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
)

var updateNotifyGoldens = flag.Bool("update-notify-goldens", false, "rewrite the S58 T1 notify delivery goldens instead of comparing")

// notifyGolden is one delivered event as the subscriber saw it: the
// fields the wire carries and nothing else. Seq is deliberately absent —
// it counts every event the test daemon appended, so it belongs to the
// harness, not to the behavior.
type notifyGolden struct {
	Kind          string            `json:"kind"`
	ComponentRoot string            `json:"componentRoot"`
	Payload       json.RawMessage   `json:"payload"`
	Notify        *notifyGoldenHint `json:"notify"`
}

type notifyGoldenHint struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tone  string `json:"tone"`
}

// captureGolden pulls the last delivery of `kind` out of the wire and
// freezes it; a kind that never arrived is a failed golden, not a nil.
func captureGolden(t *testing.T, w *wire, before int, kind string) notifyGolden {
	t.Helper()
	var found *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.ConnID == "c1" && s.Event.Kind == kind {
			ev := s.Event
			found = &ev
		}
	}
	if found == nil {
		t.Fatalf("event %s never reached the subscriber", kind)
	}
	g := notifyGolden{Kind: found.Kind, ComponentRoot: found.ComponentRoot, Payload: append(json.RawMessage(nil), found.Payload...)}
	if found.Notify != nil {
		g.Notify = &notifyGoldenHint{Title: found.Notify.Title, Body: found.Notify.Body, Tone: string(found.Notify.Tone)}
	}
	return g
}

func checkGolden(t *testing.T, name string, g notifyGolden) {
	t.Helper()
	got, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("encode golden %s: %v", name, err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "notify_golden", name+".json")
	if *updateNotifyGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-notify-goldens to create)", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("notify golden %s drifted:\n--- have ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// newNotifyWatcher builds the harness the notify tests need: a
// store-backed test daemon with the notify.events set applied (T4: the
// resolver needs the class table — there is no hint path without it any
// more, the switch is gone), one initialized connection subscribed to
// everything.
func newNotifyWatcher(t *testing.T) (*Daemon, *wire, context.Context) {
	t.Helper()
	d, _, w := newNotifyClassDaemon(t)
	return d, w, context.Background()
}

func TestNotifyDeliveryGoldens(t *testing.T) {
	// loopBlocked and the rest use the payload shapes the emitters
	// actually build (loop/advance.go:246 waiting, :126 emit, daemon
	// rest.go finished, loop_control_service.go:222 stop request).
	blocked := map[string]any{"loopId": "loop-golden", "reason": loop.BlockedReason{
		Kind:   "action-denied",
		Detail: `action "deploy production" denied by policy`,
		Action: "deploy production",
	}}
	waiting := map[string]any{"loopId": "loop-golden", "questionId": 7,
		"prompt": "Deploy to production?", "answers": []string{"yes", "no"},
		"lane": "ship", "step": "step-7"}
	finished := map[string]any{"loopId": "loop-golden", "status": "done"}
	stopRequest := map[string]any{"loopId": "loop-golden", "reason": "the agent asks: the work is done"}

	// The same payload bytes ride both doors in the paired rows, so a
	// diff between publish-* and onEvent-* after T4 is ONLY the hint.
	t.Run("publish-loop-finished", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.publishLoop(protocol.KindLoopFinished, finished)
		checkGolden(t, "publish-loop-finished", captureGolden(t, w, before, protocol.KindLoopFinished))
	})
	t.Run("publish-loop-blocked", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.publishLoop(protocol.KindLoopBlocked, blocked)
		checkGolden(t, "publish-loop-blocked", captureGolden(t, w, before, protocol.KindLoopBlocked))
	})
	t.Run("publish-loop-waiting", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.publishLoop(protocol.KindLoopWaiting, waiting)
		checkGolden(t, "publish-loop-waiting", captureGolden(t, w, before, protocol.KindLoopWaiting))
	})
	t.Run("publish-loop-stop-requested", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.publishLoop(protocol.KindLoopStopRequested, stopRequest)
		checkGolden(t, "publish-loop-stop-requested", captureGolden(t, w, before, protocol.KindLoopStopRequested))
	})
	t.Run("onEvent-loop-finished", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Table.OnEvent(protocol.KindLoopFinished, finished)
		checkGolden(t, "onEvent-loop-finished", captureGolden(t, w, before, protocol.KindLoopFinished))
	})
	t.Run("onEvent-loop-blocked", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Table.OnEvent(protocol.KindLoopBlocked, blocked)
		checkGolden(t, "onEvent-loop-blocked", captureGolden(t, w, before, protocol.KindLoopBlocked))
	})
	t.Run("onEvent-loop-waiting", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Table.OnEvent(protocol.KindLoopWaiting, waiting)
		checkGolden(t, "onEvent-loop-waiting", captureGolden(t, w, before, protocol.KindLoopWaiting))
	})
	t.Run("onEvent-loop-stop-requested", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Table.OnEvent(protocol.KindLoopStopRequested, stopRequest)
		checkGolden(t, "onEvent-loop-stop-requested", captureGolden(t, w, before, protocol.KindLoopStopRequested))
	})

	// Budget emits through its one door (publishBudgetEvent) — since T4
	// that door just publishes and the resolver inside publish decides.
	t.Run("emit-budget-warn", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.publishBudgetEvent(protocol.KindBudgetWarn, "comp-golden",
			map[string]any{"loopId": "loop-golden", "dimension": "tokens", "cap": 1000000, "pct": 92})
		checkGolden(t, "emit-budget-warn", captureGolden(t, w, before, protocol.KindBudgetWarn))
	})
	t.Run("emit-budget-hit", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.publishBudgetEvent(protocol.KindBudgetHit, "comp-golden",
			map[string]any{"loopId": "loop-golden", "dimension": "tokens", "cap": 1000000, "pct": 100})
		checkGolden(t, "emit-budget-hit", captureGolden(t, w, before, protocol.KindBudgetHit))
	})
	// Watchdog's service needs a store and a firing rule to reach its
	// emit line; this mirrors what watchdog.go does since T4 — the plain
	// kind-rooted publish, the resolver adding the hint — and pins the
	// bytes that line delivers.
	t.Run("emit-watchdog-fired", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Publish(protocol.KindWatchdogFired, "comp-golden",
			map[string]any{"loopId": "loop-golden", "ruleId": "wd-stalled", "signal": "stall",
				"action": "notify", "metric": "minutes-since-step", "episode": "loop-golden:wd-stalled:1"})
		checkGolden(t, "emit-watchdog-fired", captureGolden(t, w, before, protocol.KindWatchdogFired))
	})

	// question.asked: T1 pinned the ABSENCE of a hint on this event;
	// T4's class table GIVES it one ("A question is waiting for you",
	// info) — the regenerated golden is the written proof of exactly
	// that change, and the payload is the real runtime.PendingQuestion
	// shape (the row id travels as `id`).
	t.Run("onEvent-question-asked", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Table.OnEvent(protocol.KindQuestionAsked, map[string]any{"id": 7, "stepId": 41,
			"kind": "clarify", "body": "Deploy to production?"})
		checkGolden(t, "onEvent-question-asked", captureGolden(t, w, before, protocol.KindQuestionAsked))
	})
	// The control that must NEVER gain a hint: loop.advanced is routine.
	t.Run("onEvent-loop-advanced", func(t *testing.T) {
		d, w, _ := newNotifyWatcher(t)
		before := len(w.delivered())
		d.Table.OnEvent(protocol.KindLoopAdvanced, map[string]any{"loopId": "loop-golden", "step": 3})
		checkGolden(t, "onEvent-loop-advanced", captureGolden(t, w, before, protocol.KindLoopAdvanced))
	})
}
