package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

type seen struct {
	kind string
	p    any
}

func collect() (*deltaCoalescer, func() []seen) {
	var got []seen
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	c := newDeltaCoalescer(func(kind string, payload any) {
		<-mu
		got = append(got, seen{kind, payload})
		mu <- struct{}{}
	})
	return c, func() []seen { <-mu; defer func() { mu <- struct{}{} }(); return append([]seen(nil), got...) }
}

func delta(step int64, iter int, tool, text string) protocol.StepEventPayload {
	return protocol.StepEventPayload{StepID: step, Agent: "a", Iteration: iter, Tool: tool, Text: text}
}

// Consecutive chunks of one run go out as one event; another run, or any other event, ends the run so the order holds.
func TestStreamedChunksOfOneRunArePublishedTogether(t *testing.T) {
	c, got := collect()
	for _, w := range []string{"Hel", "lo ", "wor", "ld"} {
		c.Emit("model.delta", delta(1, 0, "", w))
	}
	c.Emit("step.tool.start", protocol.StepEventPayload{StepID: 1}) // flushes the held text first
	c.Emit("model.delta", delta(1, 0, "thinking", "hmm"))           // a different kind of text: a different run
	c.Emit("model.delta", delta(1, 0, "", "again"))                 // and back: the held thinking is published first
	c.flush()
	ev := got()
	if len(ev) != 4 {
		t.Fatalf("want 4 events (text, tool start, thinking, text), got %d: %+v", len(ev), ev)
	}
	if ev[0].p.(protocol.StepEventPayload).Text != "Hello world" || ev[1].kind != "step.tool.start" || ev[2].p.(protocol.StepEventPayload).Text != "hmm" || ev[3].p.(protocol.StepEventPayload).Text != "again" {
		t.Fatalf("order or joining wrong: %+v", ev)
	}
}

func TestHeldTextIsNotHeldForever(t *testing.T) {
	c, got := collect()
	c.Emit("model.delta", delta(1, 0, "", "tail"))
	deadline := time.Now().Add(time.Second)
	for len(got()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(got()) != 1 || got()[0].p.(protocol.StepEventPayload).Text != "tail" {
		t.Fatalf("the held chunk was never published: %+v", got())
	}
}

func TestALongRunFlushesEarly(t *testing.T) {
	c, got := collect()
	c.Emit("model.delta", delta(1, 0, "", strings.Repeat("x", deltaMaxBytes+1)))
	if len(got()) != 1 {
		t.Fatalf("a run past the size limit must be published at once, got %d", len(got()))
	}
}
