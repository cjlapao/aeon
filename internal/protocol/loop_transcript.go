package protocol

import (
	"context"
	"encoding/json"
)

// `loop.transcript` — a FIFO-windowed read across a loop's whole history,
// live or long finished.
//
// # Why this exists beside `step.show`
//
// `step.show` answers one step; the loop workspace's live view already
// stitches many steps together as events arrive, but only for whatever the
// daemon's shared, bounded event ring (`EventLog`, `DefaultRetention`) still
// holds — which forgets by design (T4's N6 finding) and is the SAME ring
// every loop on the daemon shares. Reopening an old loop, or scrolling back
// past what the ring kept, needs the durable source instead: this method
// reads `step_message` directly, across every step a loop ran, windowed so a
// long-running or long-finished loop never has to load all at once.
//
// # The cursor is `step_message`'s own id, never an offset
//
// It is a single, global, monotonic AUTOINCREMENT across every step this
// store has ever written. `before`/`after` compare against it directly — no
// signing, no derived position. An OFFSET cursor goes stale the instant a
// concurrent write lands ahead of the window; a sibling project hit exactly
// that in production as a live chat's page stuck replaying itself. This
// cursor cannot go stale that way: `id > n` always means the same rows.

// LoopTranscriptParams scopes the loop the same way `step.list` does
// (`slug` + `componentRoot`, never a bare slug — DB4) and names the window.
//
// Exactly one of Before/After is honoured; both absent means "the latest
// window" — what opening a loop, running or finished, bootstraps the view
// with, scrolled to the bottom exactly as the live stream already does.
type LoopTranscriptParams struct {
	Slug          string `json:"slug"`
	ComponentRoot string `json:"componentRoot"`
	// Before: the window immediately preceding this message id — scrolling up.
	Before *int64 `json:"before,omitempty"`
	// After: the window immediately following this message id — scrolling down.
	After *int64 `json:"after,omitempty"`
	// Limit caps the page; the server clamps a bad value rather than refusing
	// it, since a window this app asks for carries no security weight.
	Limit int `json:"limit,omitempty"`
}

// LoopTranscriptMessageView is one row of the window, oldest first — the same
// order the live event stream renders in, so a host can concatenate a
// history page with the live tail with no reordering step.
type LoopTranscriptMessageView struct {
	// ID is `step_message`'s own id — the cursor a next `before`/`after` call
	// passes back.
	ID        int64  `json:"id"`
	StepID    int64  `json:"stepId"`
	Agent     string `json:"agent"`
	Phase     string `json:"phase"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
	// Content is the stored bytes, unchanged — the same reasoning
	// `StepView.Messages` already uses: a re-encoding here would silently
	// drop whatever field this package does not itself declare.
	Content json.RawMessage `json:"content,omitempty"`
	// Unreadable names why THIS row could not be shown — a future
	// format_version, typically — without refusing the rest of the page
	// around it (P31, applied to a reader that loses nothing by naming one
	// bad turn instead of discarding the other forty-nine with it).
	Unreadable string `json:"unreadable,omitempty"`
}

// LoopTranscriptResult is one window.
type LoopTranscriptResult struct {
	Messages []LoopTranscriptMessageView `json:"messages"`
	// HasOlder/HasNewer say whether scrolling further in either direction
	// would find more — the honest edge, never inferred from `len(Messages)
	// == limit` (a page that happens to land exactly on the last row would
	// lie that way).
	HasOlder bool `json:"hasOlder"`
	HasNewer bool `json:"hasNewer"`
}

func (t *Table) handleLoopTranscript(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p LoopTranscriptParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Slug == "" || p.ComponentRoot == "" {
		return nil, ErrInvalidScope.WithWhat("reading a loop's transcript needs both a slug and a componentRoot")
	}
	if p.Before != nil && p.After != nil {
		return nil, ErrInvalidScope.WithWhat("loop.transcript takes before OR after, never both")
	}
	return mem.ReadLoopTranscript(p)
}
