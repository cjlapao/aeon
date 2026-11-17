package daemon

import (
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// messages.go is S39 T5's publish door for the thread (CR6).
//
// # Why a door and not a call from the store
//
// The store appends the row and knows nothing about streams (the layer rule
// S5 drew); the daemon that served the append is the one with subscribers.
// Nothing calls this yet — T6 (a turn posting its message) and T8 (the
// kickoff posting its first message) are the callers this exists for; the
// test drives it directly so the wire shape is pinned before a caller could
// drift it.

// publishMessage announces one appended thread message. loopID is the
// loop's id (not slug) — publishLoop resolves it to the loop's project
// root (the event is filed where the loop lives, so a component-scoped
// subscription receives it) and adds the durable row/snapshot view beside
// the message fields.
func (d *Daemon) publishMessage(loopID string, m store.ChatMessage) {
	payload := map[string]any{
		"loopId":    loopID,
		"messageId": m.ID,
		"seq":       m.Seq,
		"author":    m.Author,
		"kind":      m.Kind,
		"source":    m.Source,
		"body":      m.Body,
	}
	// S68 B8: the row's chosen-skills receipt is announced with the row, so
	// the chip UNDER the bubble (which skills rode this message — their
	// LINES went into the system half, hidden from the window) renders from
	// live events and from replay identically. Absent when nothing was
	// chosen: the pre-S68 wire shape is byte-identical (D7).
	if len(m.Skills) > 0 {
		payload["skills"] = m.Skills
	}
	// S69 AT2: the attachment receipt rides with the row too — chips under
	// the bubble (and, for images, previews through attachment.read) render
	// from live events and from replay identically. Absent when nothing was
	// attached: the pre-S69 wire shape is byte-identical (D7).
	if len(m.Attachments) > 0 {
		payload["attachments"] = m.Attachments
	}
	d.publishLoop(protocol.KindLoopMessagePosted, payload)
}
