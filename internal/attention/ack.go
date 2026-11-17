package attention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ack.go — S58 T3 (SV3, P3, SV-D11): the write half of visibility. A
// person hides an item they have seen (`ack`), hides it until a moment
// (`snooze`), or brings it back (`unack`); each row is attributed from the
// peer identity the door already checked and lands in the append-only
// `attention_ack` table — the row IS the audit (P3: a second audit table
// for the same act would double-count; deviation from T3.md's open question
// recorded there and in the task Notes).
//
// Nothing here ever touches the loop, the question, or any other row: an
// ack changes who is looking, never what is true (Behaviour 5).

// MaxSnooze is SV-D11: a snooze may not hide an item past one week. A
// longer silence needs a channel schedule (T5), not an invisible problem.
const MaxSnooze = 7 * 24 * time.Hour

// ErrGhostKey says the key names nothing currently needing attention. The
// protocol door renders it as ErrInboxScope(-31926) with its own sentence;
// the refusal exists so `attention_ack` cannot be filled with arbitrary
// keys by anyone who can spell the grammar.
var ErrGhostKey = errors.New("attention: that item is no longer needing attention")

// ErrSnoozeHorizon says `until` is not a moment inside the allowed window
// (future, no further than now + MaxSnooze). The protocol renders it as
// ErrSnoozeHorizon(-31927).
var ErrSnoozeHorizon = errors.New("attention: snooze horizon")

// AckWriter is the store seam: T1's append-only marker write, named rather
// than taken as *store.Store (the same seam discipline as the readers).
type AckWriter interface {
	AppendAttentionAck(subjectKey, op, until, actorKind, actorID string) (int64, error)
}

// AckService performs the three visibility marks over a live registry and
// the append-only table.
type AckService struct {
	// Write is the marker table.
	Write AckWriter
	// LiveItems evaluates the registry NOW and returns every item
	// currently needing attention, ACKED INCLUDED — the ghost test must
	// see the pre-hide set (acking an already-acked item is a no-op, not
	// a ghost), and the no-op test reads each item's Acked state.
	LiveItems func(ctx context.Context) ([]Item, error)
	// Now is the clock; the horizon comparison is computed at write,
	// expiry is computed at READ (no job, ever — SV3).
	Now func() time.Time
}

func (a *AckService) now() time.Time {
	if a.Now == nil {
		return time.Now().UTC()
	}
	return a.Now().UTC()
}

// Ack hides the item. Already-acked ⇒ no row and wrote=false (P3: one row
// per CHANGING write; idempotence is a no-op, not an error).
func (a *AckService) Ack(ctx context.Context, actorKind, actorID, key string) (bool, error) {
	return a.mark(ctx, "ack", "", actorKind, actorID, key)
}

// Snooze hides the item until a moment inside the horizon. A re-snooze IS a
// change (a different moment), so it writes even over an existing mark.
func (a *AckService) Snooze(ctx context.Context, actorKind, actorID, key string, until time.Time) (bool, error) {
	now := a.now()
	if !until.After(now) {
		return false, fmt.Errorf("%w: until %s is not in the future (now %s) — a past snooze would hide an item forever", ErrSnoozeHorizon, until.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	}
	if until.After(now.Add(MaxSnooze)) {
		return false, fmt.Errorf("%w: until %s is past the one-week horizon (max %s) — a longer silence is a channel schedule (T5), not an invisible problem", ErrSnoozeHorizon, until.UTC().Format(time.RFC3339Nano), now.Add(MaxSnooze).Format(time.RFC3339Nano))
	}
	return a.mark(ctx, "snooze", until.UTC().Format(time.RFC3339), actorKind, actorID, key)
}

// Unack brings the item back. Not currently hidden ⇒ no row.
func (a *AckService) Unack(ctx context.Context, actorKind, actorID, key string) (bool, error) {
	return a.mark(ctx, "unack", "", actorKind, actorID, key)
}

func (a *AckService) mark(ctx context.Context, op, until, actorKind, actorID, key string) (bool, error) {
	if err := ValidKey(key); err != nil {
		return false, err
	}
	if actorKind == "" || actorID == "" {
		return false, fmt.Errorf("attention: %s needs an attributed actor, got kind %q id %q", op, actorKind, actorID)
	}
	if a.LiveItems == nil {
		return false, errors.New("attention: AckService has no LiveItems to check the key against")
	}
	items, err := a.LiveItems(ctx)
	if err != nil {
		return false, fmt.Errorf("attention: the live evaluation refused the %s check: %w", op, err)
	}
	var found *Item
	for i := range items {
		if items[i].Key == key {
			found = &items[i]
			break
		}
	}
	if found == nil {
		return false, fmt.Errorf("%w: %s", ErrGhostKey, key)
	}
	switch op {
	case "ack":
		if found.Acked != nil {
			return false, nil // already hidden — idempotent, NO row
		}
	case "unack":
		if found.Acked == nil {
			return false, nil // already visible — idempotent, NO row
		}
	}
	if _, err := a.Write.AppendAttentionAck(key, op, until, actorKind, actorID); err != nil {
		return false, fmt.Errorf("attention: append %s: %w", op, err)
	}
	return true, nil
}

// ValidKey checks the key grammar `<source>:<id>:<since>`: three segments,
// a lowercase-letter source, a non-empty id, and a `since` that parses as
// the RFC 3339 stamp the sources mint (split is SplitN — an RFC 3339 `time`
// carries colons of its own).
func ValidKey(key string) error {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return fmt.Errorf("attention: %q is not an item key (want <source>:<id>:<since>)", key)
	}
	for i, r := range parts[0] {
		if r < 'a' || r > 'z' {
			_ = i
			return fmt.Errorf("attention: %q is not an item key (source %q has characters outside a-z)", key, parts[0])
		}
	}
	if _, ok := parseSince(parts[2]); !ok {
		return fmt.Errorf("attention: %q is not an item key (since %q is not the RFC 3339 stamp the mint uses)", key, parts[2])
	}
	return nil
}
