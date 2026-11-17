package notify

import (
	"encoding/json"
	"strconv"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// Resolver answers the one question the delivery path asks per event:
// does this event kind carry a hint, and what are its exact bytes
// (SV4/SV5). It replaces the old curated switch with a lookup over the
// seeded class table; there is NO cache (N8 — the table is a handful of
// rows and one indexed read per event is the price the design names).
//
// It reads, never writes: the rows arrive through the seed engine (T4's
// notify.events set) and the human door (notify.class.update). A nil
// store — a storeless daemon — resolves to nil for every kind: no class
// table, no hints, the V13 stance.
type Resolver struct {
	Store *store.Store
}

// NewResolver binds the resolver to a store. A nil store is legal and
// resolves nothing.
func NewResolver(s *store.Store) *Resolver { return &Resolver{Store: s} }

// HintFor resolves (kind, payload) to the delivery-time hint, or nil for
// "this event is not worth a toast". Resolution is the row for
// (kind, variant), else the row for (kind, ""), else nil; a disabled row
// is nil (an operator turned the class off — nothing may revive it here).
//
// The payload is consulted for three facts only, all ids or enums read
// out of its JSON shape: the blocked reason's KIND (loop.blocked's
// variant — T0's decision, the payload's nested reason.kind, the shape
// the runtime's blockTo emits and T1's goldens pinned), the waiting
// reason (loop.waiting carries "reason":"stopped" as a plain string at
// the stop-to-wait emissions), the loop id and the question id (for the
// attentionKey and the deepLink). Nothing else about the payload is read,
// and NOTHING from it is ever rendered: prompts and messages reach this
// function's input and cannot reach its output (SV6 — Values has no field
// for them).
func (r *Resolver) HintFor(kind string, payload any) *protocol.NotifyHint {
	h, _ := r.Resolve(kind, payload)
	return h
}

// Resolve is HintFor's full answer: the toast hint AND the closed
// ResolvedEvent the dispatcher builds its envelope from — one class
// read, one pin read, two consumers. The event half carries ids, slugs,
// enums and the rendered title: the same SV6 guarantee, because the
// struct has no field a payload value could fill except through these
// closed reads (T5). A nil hint means "raise nothing, dispatch nothing";
// the facts travel beside it only for the sender to re-check (never
// instead of) the hint.
func (r *Resolver) Resolve(kind string, payload any) (*protocol.NotifyHint, ResolvedEvent) {
	if r == nil || r.Store == nil {
		return nil, ResolvedEvent{}
	}
	classes, err := r.Store.ListNotifyClasses()
	if err != nil {
		// a store that cannot read the class table raises no hint and no
		// error: the event path must not fail because notifying failed
		// (SV8's isolation, one floor up)
		return nil, ResolvedEvent{}
	}
	ev := decodePayload(payload)
	c, ok := resolve(classes, kind, ev.variant(kind))
	if !ok || !c.Enabled {
		return nil, ResolvedEvent{}
	}
	re := ResolvedEvent{Kind: kind, Variant: c.Variant,
		Severity: severityFor(kind, ev.ReasonKind), LoopID: ev.LoopID}
	// Values for the render: the enum-derived fields come free, the slug
	// fields cost a store read whenever the event names a loop at all —
	// T5 deviation from T4: the envelope carries the slugs even when no
	// template asks for them, so one pin read serves both consumers.
	v := Values{ReasonKind: ev.ReasonKind, Severity: re.Severity, Count: "1"}
	if ev.LoopID != "" {
		if pin, err := r.Store.ReadPinnedLoop(ev.LoopID); err == nil {
			v.LoopSlug = pin.Loop.Slug
			re.LoopSlug = pin.Loop.Slug
			re.ComponentRoot = pin.Loop.ComponentRoot
			if comp, err := r.Store.ComponentForRoot(pin.Loop.ComponentRoot); err == nil {
				v.RootSlug = comp.Slug
				re.RootSlug = comp.Slug
			}
		}
	}
	title := Render(c.TitleTemplate, v)
	body := Render(c.BodyTemplate, v)
	if title == "" {
		return nil, ResolvedEvent{} // a row that renders no title raises no toast — the honest absence
	}
	re.Title = title
	re.AttentionKey = r.attentionKey(kind, ev)
	re.DeepLink = deepLinkFor(ev)
	h := &protocol.NotifyHint{Title: title, Body: body, Tone: protocol.NotifyTone(c.Tone)}
	h.AttentionKey = re.AttentionKey
	h.DeepLink = re.DeepLink
	return h, re
}

// severityFor maps a class to its attention severity enum — the {severity}
// placeholder's whole vocabulary. It agrees with the inbox ranking (SV2):
// the class the SEVERITY names is the class the item would carry; kinds
// the inbox does not rank (a finished loop) read info, the honest floor.
func severityFor(kind, variant string) string {
	switch kind {
	case protocol.KindLoopBlocked:
		return "blocked"
	case protocol.KindLoopWaiting:
		if variant == "stopped" {
			return "stopped"
		}
		return "needs-you"
	case protocol.KindQuestionAsked, protocol.KindLoopStopRequested, protocol.KindBudgetWarn, protocol.KindBudgetHit:
		return "needs-you"
	case protocol.KindWatchdogFired:
		return "stuck"
	default:
		return "info"
	}
}

// resolve is the (kind, variant) → (kind, ”) fallback, in order.
func resolve(classes []store.NotifyClass, kind, variant string) (store.NotifyClass, bool) {
	var fallback store.NotifyClass
	haveFallback := false
	for _, c := range classes {
		if c.Kind != kind {
			continue
		}
		if c.Variant == "" {
			fallback, haveFallback = c, true
			continue
		}
		if c.Variant == variant {
			return c, true
		}
	}
	return fallback, haveFallback
}

// eventFacts are the ONLY fields read out of an event's payload. The
// payload arrives as whatever the emitter built (a map, a struct); the
// re-marshal through JSON is the closed read — the same bytes the durable
// log will hold, decoded against these three names and nothing else.
type eventFacts struct {
	LoopID     string `json:"loopId"`
	ReasonKind string `json:"-"` // derived from `reason`, never read as a flat field
	QuestionID int64  `json:"questionId"`
	reasonKind string // the derived variant, kept unexported
	ReasonRaw  json.RawMessage
}

func decodePayload(payload any) eventFacts {
	var ev eventFacts
	if payload == nil {
		return ev
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ev
	}
	probe := struct {
		LoopID     string          `json:"loopId"`
		QuestionID json.Number     `json:"questionId"`
		ID         json.Number     `json:"id"`
		Reason     json.RawMessage `json:"reason"`
	}{}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ev
	}
	ev.LoopID = probe.LoopID
	// The question id travels under TWO names, both real: `questionId`
	// (the loop.waiting payload the runtime builds) and `id` — the actual
	// question.asked payload IS runtime.PendingQuestion (step.go:837
	// emits result.Question with .ID filled by AskStep), whose json field
	// is `id`. The attention key needs the row either way; the kinds that
	// consult it (question.asked, loop.waiting) are exactly the ones whose
	// `id` means the question and nothing else.
	qid := probe.QuestionID
	if qid == "" {
		qid = probe.ID
	}
	if n, err := qid.Int64(); err == nil {
		ev.QuestionID = n
	}
	ev.ReasonRaw = probe.Reason
	// loop.blocked: `reason` is the policy-shaped OBJECT {"kind":…};
	// loop.waiting: `reason` is the plain string "stopped". The variant is
	// whichever shape the emitter actually used (T0's decision; T1's
	// goldens pinned the object shape for blocked).
	if len(probe.Reason) > 0 {
		var obj struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(probe.Reason, &obj) == nil && obj.Kind != "" {
			ev.ReasonKind = obj.Kind
		} else {
			var s string
			if json.Unmarshal(probe.Reason, &s) == nil {
				ev.ReasonKind = s
			}
		}
	}
	return ev
}

// variant is the class-table second column: the reason kind for the two
// kinds that carry one, empty for every other kind. The variant is taken
// ONLY for those kinds — a stray "reason" field on some other payload
// cannot re-route that event's class (the closed read, again).
func (ev eventFacts) variant(kind string) string {
	switch kind {
	case protocol.KindLoopBlocked, protocol.KindLoopWaiting:
		return ev.ReasonKind
	default:
		return ""
	}
}

// attentionKey is the SV1 key the inbox would mint for the SAME subject —
// the dedup handle SV5/SV7 hand to the consumers (the app and T5's
// dispatcher dedup by it; the resolver itself never dedups). It is minted
// with attention.KeyOf, the one function the inbox sources use, so the
// two agree BY CONSTRUCTION and not by hoping the formats match.
//
// Subjects the inbox does not carry (finished loops, budget warnings,
// watchdog firings, stop requests) get no key — an empty omitempty field,
// which is the honest answer "there is no inbox item this names". A store
// read that fails gives the same empty answer: the hint still rides, its
// key simply is unknown (delivery-path isolation, and never a guess).
func (r *Resolver) attentionKey(kind string, ev eventFacts) string {
	switch {
	case ev.QuestionID > 0 && (kind == protocol.KindQuestionAsked || kind == protocol.KindLoopWaiting):
		q, err := r.Store.ReadQuestion(ev.QuestionID)
		if err != nil || q.AskedAt == "" {
			return ""
		}
		return attention.KeyOf("questions", strconv.FormatInt(ev.QuestionID, 10), q.AskedAt)
	case kind == protocol.KindLoopBlocked && ev.LoopID != "":
		pin, err := r.Store.ReadPinnedLoop(ev.LoopID)
		if err != nil {
			return ""
		}
		since := pin.Loop.UpdatedAt // the same inputs the blocked source reads (blocked.go: UpdatedAt, overridden by the durable reason's own At)
		if reasons, err := r.Store.LatestBlockedReasons([]string{pin.Loop.ComponentRoot}); err == nil {
			if rec, ok := reasons[ev.LoopID]; ok && rec.At != "" {
				since = rec.At
			}
		}
		return attention.KeyOf("blocked", ev.LoopID, since)
	case kind == protocol.KindLoopWaiting && ev.ReasonKind == "stopped" && ev.LoopID != "":
		pin, err := r.Store.ReadPinnedLoop(ev.LoopID)
		if err != nil || pin.Loop.UpdatedAt == "" {
			return ""
		}
		return attention.KeyOf("stopped", ev.LoopID, pin.Loop.UpdatedAt)
	default:
		return ""
	}
}

// deepLinkFor is the SV9 navigation target — a LOCATION and nothing else:
// no token, no answer, no capability (S19 T5's permanence). A loop event
// links to its loop; anything else links to the inbox screen, which is
// where "the daemon needs a person" always reads correctly.
func deepLinkFor(ev eventFacts) string {
	if ev.LoopID != "" {
		return "aeon://loop/" + ev.LoopID
	}
	return "aeon://inbox"
}
