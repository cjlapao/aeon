package attention

import (
	"context"

	"github.com/cjlapao/aeon/internal/store"
)

// blocked.go — the `blocked` source (SV1): loops whose STATUS is blocked,
// carrying the daemon's own stored sentence VERBATIM (H4: `reason` is
// LoopView's lastReason, never a rewritten verdict).
//
// # The kind is READ, never guessed (SV-D12)
//
// T0's F5 measured that a blockTo reason kind does not survive verbatim into
// `lastReason` — the runtime JSON holds a kind there only for cap-hit, stuck
// and stopped; every other block stores the DETAIL. The kind therefore comes
// from the DURABLE `loop.blocked` event payload (the reader
// history.blockedReason pioneered), and `lastReason` is the fallback marker
// it actually is. An unknown or absent kind classifies as `blocked.other`,
// whose door is the generic one — the honest "no single command for this"
// rather than a command invented from a guess.

// knownBlockedKinds is the closed set the door table and the class seeds
// (SV4) carry sentences for. It is CLOSED and in code: a new block reason
// kind lands on blocked.other first, and grows a door in the same commit its
// emitter needs one (TestDoorTableGolden makes an untabled kind loud).
var knownBlockedKinds = map[string]bool{
	"action-denied": true,
	"step-blocked":  true,
	"model-error":   true,
	"runner-error":  true,
	"exhaustion":    true,
	"cap-hit":       true,
	"stuck":         true,
}

// BlockedSource reads blocked loops.
type BlockedSource struct {
	Loops  store.BlockedLoopsReader
	Owners *OwnerMaps
}

// Name is the source's stable name in `sources[]`.
func (BlockedSource) Name() string { return "blocked" }

// Items lists blocked loops with their stored reason and read kind.
func (b BlockedSource) Items(ctx context.Context, q Query) ([]Item, error) {
	loops, err := b.Loops.BlockedLoopsForScope(q.Scope.RootPaths)
	if err != nil {
		return nil, err
	}
	reasons, err := b.Loops.LatestBlockedReasons(q.Scope.RootPaths)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(loops))
	for _, l := range loops {
		kind, severity := classify(l.LastReason, reasons[l.LoopID])
		since := l.UpdatedAt
		if r, ok := reasons[l.LoopID]; ok && r.At != "" {
			since = r.At // the block happened when the event was appended
		}
		it := Item{
			Key:      KeyOf("blocked", l.LoopID, since),
			Kind:     kind,
			Severity: severity,
			Reason:   l.LastReason, // VERBATIM (the test's exact assertion)
			Since:    since,
			LoopID:   l.LoopID,
			LoopSlug: l.Slug,
		}
		if b.Owners != nil {
			it.ComponentID, it.RootID = b.Owners.Owner(l.ComponentRoot)
		}
		out = append(out, it)
	}
	return out, nil
}

// classify maps (lastReason, the durable event's reason) to (kind, severity)
// without inventing anything: the event's kind wins when it is one the
// tables know; else lastReason's own markers (cap-hit/stuck/stopped are what
// the runtime stores there — F5); else blocked.other. Severity follows SV2:
// a cap hit is a needs-you, a stuck loop is the `stuck` class, a plain block
// is `blocked` (a blocked.stopped kind — a stopped block — reads blocked
// like any other block, the STOPPED class belongs to the person-stop).
func classify(lastReason string, r store.BlockedReason) (string, Severity) {
	if r.Kind != "" {
		if knownBlockedKinds[r.Kind] {
			return blockedKind(r.Kind), severityForBlockedKind(r.Kind)
		}
		return "blocked.other", Blocked
	}
	switch lastReason {
	case "cap-hit", "stuck", "stopped":
		return blockedKind(lastReason), severityForBlockedKind(lastReason)
	}
	return "blocked.other", Blocked
}

func blockedKind(k string) string { return "blocked." + k }

func severityForBlockedKind(k string) Severity {
	switch k {
	case "cap-hit":
		return NeedsYou // SV2 names a cap hit a needs-you
	case "stuck":
		return Stuck // SV2's `stuck` class
	default:
		return Blocked
	}
}
