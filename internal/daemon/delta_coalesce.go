package daemon

import (
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// delta_coalesce.go — a streamed answer is published a few times a second, not once per token.
//
// A model streams its reply token by token and each token was one published event. Every subscriber of the app's one
// connection gets each of them through a bounded queue (256 deep, dropped when full), so two agents talking at once overran
// it and the app silently lost events: lists that missed `loop.waiting`/`loop.resumed` stayed on the old status. The chat
// already folds consecutive chunks of one run into one block, so joining them here costs nothing it shows, and the queue
// stays free for the events that carry state.
//
// A chunk is held at most `deltaWindow`, and every other event flushes what is held first, so the order the subscriber sees is
// the order things happened. Chunks of different runs (another step, turn or kind of text) are never joined.

const deltaWindow = 40 * time.Millisecond

// deltaMaxBytes flushes early, so one very long run does not sit in memory or arrive as a single huge event.
const deltaMaxBytes = 2048

type deltaCoalescer struct {
	mu      sync.Mutex
	pending *protocol.StepEventPayload
	timer   *time.Timer
	publish func(kind string, payload any)
}

func newDeltaCoalescer(publish func(kind string, payload any)) *deltaCoalescer {
	return &deltaCoalescer{publish: publish}
}

func sameRun(a, b protocol.StepEventPayload) bool {
	return a.StepID == b.StepID && a.Iteration == b.Iteration && a.Tool == b.Tool && a.Agent == b.Agent
}

// Emit publishes an event, holding streamed text briefly so consecutive chunks of one run go out as one.
func (c *deltaCoalescer) Emit(kind string, payload any) {
	if kind != "model.delta" {
		c.flush()
		c.publish(kind, payload)
		return
	}
	p, ok := payload.(protocol.StepEventPayload)
	if !ok {
		c.flush()
		c.publish(kind, payload)
		return
	}
	c.mu.Lock()
	if c.pending != nil && !sameRun(*c.pending, p) {
		held := *c.pending
		c.pending = nil
		c.mu.Unlock()
		c.publish("model.delta", held)
		c.mu.Lock()
	}
	if c.pending == nil {
		held := p
		c.pending = &held
	} else {
		c.pending.Text += p.Text
	}
	full := len(c.pending.Text) >= deltaMaxBytes
	if !full && c.timer == nil {
		c.timer = time.AfterFunc(deltaWindow, c.flush)
	}
	c.mu.Unlock()
	if full {
		c.flush()
	}
}

// flush publishes whatever is held.
func (c *deltaCoalescer) flush() {
	c.mu.Lock()
	held := c.pending
	c.pending = nil
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	if held != nil {
		c.publish("model.delta", *held)
	}
}
