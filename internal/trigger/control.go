package trigger

import (
	"context"
	"time"
)

// control.go — S20 T8: the OPERATIONAL plane, in one implementation.
//
// # The two off switches, kept apart on purpose (TR12)
//
//	TriggerAccess        governance — "no webhooks in this deployment"
//	                     lives in policy: operator-authored, recorded,
//	                     versioned, a deployment-wide rule with a lifetime.
//	pause / resume       operational control — "not right now"
//	                     lives HERE: CLI and app only, a decision somebody
//	                     may undo this afternoon.
//
// Both stop a loop from starting. They are DIFFERENT FACTS, and the answers
// say so in different words: a paused fire answers did_not_fire with the
// TR12 sentence (the off is honoured), a governed fire answers refused with
// S10's sentence (the rule spoke). Anyone reading only one word cannot
// confuse them, and `trigger list` prints PAUSED — never a policy verdict,
// because this plane does not evaluate policy and would be lying if it
// claimed to.
//
// # Why pause is NOT on the narrow surface (R6)
//
// Operational control on the narrow surface would hand a caller — an MCP
// server, a model with tool access — the means to bypass the very guards
// written to constrain it. The scheduler needs an off switch that is not
// "uninstall", and that switch is LOCAL: these functions are reachable from
// the CLI and the daemon's socket table, and the narrow action table
// forbids their words (ForbiddenOperationalControl; the pin test scans the
// whole table, synonyms included).
//
// # fire-now is a convenience, and it is an honest one (TR2)
//
// It fires AS A TRIGGER — through the same fireAt walk a clock tick takes,
// with the same window, overlap and governance gates, and the starter's own
// hard-coded unattendedness. There is no parameter that could make the loop
// attended: Fire carries no field that speaks attendedness (T2's
// signature-level rule), and a person watching the fire changes nothing
// about who the loop runs for. A fire-now that fired "as the invoking user"
// would be an attendedness override through the back door, which is exactly
// what TR2 forbids — so it does not exist here.
//
// # fire-now never answers for the untrusted door
//
// A webhook row is DELIVERED, not fired: fire-now refuses it outright, or
// the convenience would become a way to start a webhook's loop without the
// signature that is its entire authentication story.

// TriggerView is one row as the control plane reports it: the operational
// facts only. Policy does not appear here because this plane cannot
// evaluate it, and an absent verdict printed as an implicit "allowed" is
// how governance dies — a refusal arrives in its own fire record, in S10's
// own sentence, not through this listing.
type TriggerView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Blueprint string `json:"blueprint"`
	Expr      string `json:"expr,omitempty"`
	Zone      string `json:"zone,omitempty"`
	Window    string `json:"window,omitempty"`
	SecretEnv string `json:"secretEnv,omitempty"`
	Workspace string `json:"workspace"`
	Goal      string `json:"goal,omitempty"`
	Paused    bool   `json:"paused"`
	NextFire  string `json:"nextFire,omitempty"`
	Covered   string `json:"coveredUntil,omitempty"`
}

// ControlAnswer is what pause/resume/fire-now say back. Found/HTTPStatus
// are outcomes, not errors — "no trigger ship" is an answer, the way 404
// was for the delivery door (T6), and an absent row leaks nothing about
// what other rows exist.
type ControlAnswer struct {
	TriggerID string `json:"triggerId"`
	Outcome   string `json:"outcome,omitempty"`
	Reason    string `json:"reason,omitempty"`
	LoopID    string `json:"loopId,omitempty"`
	Paused    bool   `json:"paused"`
	Found     bool   `json:"found"`
}

// row loads one trigger row by id. The read is the scheduler's own
// unfiltered row read (R-19: filters belong to the consumer — and control
// must SEE its paused rows to report and honour them).
func (sc *Scheduler) row(ctx context.Context, id string) (Schedule, bool, error) {
	rows, err := sc.Store.ActiveSchedules(ctx)
	if err != nil {
		return Schedule{}, false, err
	}
	for _, s := range rows {
		if s.ID == id {
			return s, true, nil
		}
	}
	return Schedule{}, false, nil
}

// List reports the operational state of every trigger. nextFire is computed
// from the row's own expression and zone — printed, never guessed; a row
// whose zone or expression cannot load says so in NextFire itself rather
// than vanishing from the listing (the listing's job is to show what is
// configured, including what is broken).
func (sc *Scheduler) List(ctx context.Context) ([]TriggerView, error) {
	rows, err := sc.Store.ActiveSchedules(ctx)
	if err != nil {
		return nil, err
	}
	now := sc.Clock.Now()
	out := make([]TriggerView, 0, len(rows))
	for _, s := range rows {
		v := TriggerView{
			ID: s.ID, Kind: string(s.Kind), Blueprint: s.Blueprint,
			Expr: s.Expr, Zone: s.Zone, SecretEnv: s.SecretEnv,
			Workspace: s.Workspace, Goal: s.Goal, Paused: s.Paused,
			Covered: s.CoveredUntil,
		}
		if s.WindowStart != "" || s.WindowEnd != "" {
			v.Window = s.WindowStart + "–" + s.WindowEnd + " (" + s.WindowDays + ")"
		}
		if s.Kind == KindSchedule {
			loc, err := LoadZone(s.Zone)
			if err != nil {
				v.NextFire = "zone does not load: " + s.Zone
			} else if expr, err := s.ScheduleExpr(); err != nil {
				v.NextFire = "expression does not parse"
			} else if next, ok := nextAfter(expr, loc, now); ok {
				v.NextFire = next.Format(time.RFC3339) + " (" + s.Zone + ")"
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// nextAfter asks the parsed expression for the first instant strictly
// after `after` — the same cron library the tick walks, so the listing's
// "next" and the clock's due are the same computation, never a second
// opinion about what the expression means.
func nextAfter(expr cronLike, loc *time.Location, after time.Time) (time.Time, bool) {
	t := expr.Next(after.In(loc))
	if t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}

// Add is the control plane's entry door (T9's finding made whole: the
// slice shipped every door except the one that adds — no production code
// wrote a schedule row, so the demo could not start). One rule makes it
// safe: it is the validator's OWN sentences that answer a bad row (T1 —
// zone must be IANA, expressions validated at write with robfig's words
// quoted, the webhook/zone and webhook/expr refusals), and a name in use
// is REFUSED, never silently rewritten: an upsert on add is how a typo
// quietly retires somebody's nightly job.
func (sc *Scheduler) Add(ctx context.Context, a Actor, s Schedule) (ControlAnswer, error) {
	// S49 AU1, third lock: the door refuses a non-person, the store refuses
	// one before SQL, and the control plane refuses one before EITHER —
	// before validation, before the row read: an unsigned write is refused
	// as an actor problem, never dressed as a shape problem.
	if !a.Person() {
		return ControlAnswer{TriggerID: s.ID, Reason: NotHumanSentence(a)}, nil
	}
	if err := ValidateSchedule(s); err != nil {
		return ControlAnswer{TriggerID: s.ID, Reason: err.Error()}, nil
	}
	if _, found, err := sc.row(ctx, s.ID); err != nil {
		return ControlAnswer{}, err
	} else if found {
		return ControlAnswer{TriggerID: s.ID, Reason: "trigger " + s.ID + " already exists — add does not rewrite a row (a typo must not silently retire somebody's schedule)"}, nil
	}
	if s.CreatedAt == "" {
		s.CreatedAt = sc.Clock.Now().UTC().Format(time.RFC3339Nano)
	}
	// A fresh trigger starts from NOW, never replays the past (T4's rule)
	// — and Add is the field's ONLY producer, so it must actually write it
	// (T9 walk finding #5: with CoveredUntil empty the frontier parses as
	// zero time, the walk caps at "unrealistically wide — left untouched",
	// and a brand-new schedule answers NOTHING, tick included; T4's tests
	// seeded the frontier themselves and hid this until a real daemon
	// added a real schedule).
	if s.CoveredUntil == "" {
		s.CoveredUntil = s.CreatedAt
	}
	if err := sc.saveAs(ctx, s, a, "create", "created "+string(s.Kind)+" "+s.ID); err != nil {
		return ControlAnswer{}, err
	}
	return ControlAnswer{TriggerID: s.ID, Found: true,
		Reason: "registered — the clock owns its next answer now (governance still asks TriggerAccess before any fire)"}, nil
}

// Pause flips the operational off. Idempotent by answer, not by silence:
// a second pause says it was already off, and the record exists once.
func (sc *Scheduler) Pause(ctx context.Context, a Actor, id string) (ControlAnswer, error) {
	return sc.setPaused(ctx, a, id, true, "paused — the clock will skip this trigger until it is resumed (TR12's operational off; governance said nothing)", "pause")
}

// Resume turns it back on. What happens next belongs to the clock and the
// policy rows — this function promises only that the operational bar is
// lifted, and says nothing about whether any fire will pass the gates.
func (sc *Scheduler) Resume(ctx context.Context, a Actor, id string) (ControlAnswer, error) {
	return sc.setPaused(ctx, a, id, false, "resumed — the clock owns the next answer now", "resume")
}

// saveAs is the S49 audit fork in one place: a rows adapter that can write
// AS a person (HumanWriter — the daemon's) gets the actor and the audit op
// and takes them into the write's own transaction; a base adapter (every
// test fake this package has ever had) keeps S20's plain save, byte for
// byte. clock.go never reaches here — Add and setPaused are the only
// callers, control-plane doors both (T0's measurement, pinned there).
func (sc *Scheduler) saveAs(ctx context.Context, s Schedule, a Actor, op, summary string) error {
	if hw, ok := sc.Store.(HumanWriter); ok {
		return hw.SaveTriggerScheduleAs(ctx, s, a, op, summary)
	}
	return sc.Store.SaveTriggerSchedule(ctx, s)
}

func (sc *Scheduler) setPaused(ctx context.Context, a Actor, id string, want bool, done, op string) (ControlAnswer, error) {
	if !a.Person() {
		return ControlAnswer{TriggerID: id, Reason: NotHumanSentence(a)}, nil
	}
	s, found, err := sc.row(ctx, id)
	if err != nil {
		return ControlAnswer{}, err
	}
	if !found {
		return ControlAnswer{TriggerID: id, Found: false,
			Reason: "no trigger " + id + " is registered (nothing was paused — a name nobody saved has no switch to throw)"}, nil
	}
	if s.Paused == want {
		// Idempotent: the second "pause" changes nothing and says so. It
		// is not an error — the operator's intent (it is off) is already
		// true — but silence would hide a typo'd second id from nobody.
		return ControlAnswer{TriggerID: id, Paused: want, Found: true,
			Reason: map[bool]string{true: "already paused — the off is recorded once", false: "already running — nothing to resume"}[want]}, nil
	}
	s.Paused = want
	if err := sc.saveAs(ctx, s, a, op, op+" schedule "+id); err != nil {
		return ControlAnswer{}, err
	}
	return ControlAnswer{TriggerID: id, Paused: want, Found: true, Reason: done}, nil
}

// FireNow fires the trigger at the arriving instant — AS A TRIGGER. The
// honesty of this function is its route through fireAt: the same window
// check, the same overlap hook, the same governance hook, the same
// starter with its hard-coded unattendedness. Nothing here can produce an
// attended loop because nothing here can name the invoker at all.
func (sc *Scheduler) FireNow(ctx context.Context, a Actor, id string) (ControlAnswer, error) {
	if !a.Person() {
		// AU1 covers fire-now too: a fire writes a trigger_fire answer and
		// may start a loop — the loudest write a trigger has. The refusal
		// records NOTHING: an agent asking is not a change (AN4), and the
		// fire row that would carry it is not the refusal's to write.
		return ControlAnswer{TriggerID: id, Reason: NotHumanSentence(a)}, nil
	}
	s, found, err := sc.row(ctx, id)
	if err != nil {
		return ControlAnswer{}, err
	}
	if !found {
		return ControlAnswer{TriggerID: id, Found: false,
			Reason: "no trigger " + id + " is registered (a fire-now needs a row to fire)"}, nil
	}
	if s.Kind == KindWebhook {
		// The ruling, stated so it cannot be re-litigated by accident:
		// fire-now on a webhook row would start its loop without the
		// signature that is the delivery's ENTIRE authentication — the
		// convenience becoming the bypass. Refused, and recorded: a human
		// asked a question in the history's ear.
		now := sc.Clock.Now().UTC()
		f := Fire{TriggerID: s.ID, Blueprint: s.Blueprint, Workspace: s.Workspace,
			Goal: s.Goal, Provenance: ProvenanceWebhook, Instant: now, At: now}
		reason := "webhook triggers are DELIVERED, not fired — fire-now does not stand in for the signature (refusing to make the convenience the bypass)"
		sc.answer(ctx, s, f, OutcomeRefused, reason)
		return ControlAnswer{TriggerID: id, Found: true, Outcome: string(OutcomeRefused), Reason: reason}, nil
	}
	if s.Paused {
		// TR12 honoured: "not right now" means fire-now too. The human
		// asked; the answer is did_not_fire with the operational sentence
		// — never refused, because governance opened its mouth nowhere
		// here, and never silence, because a human's explicit request
		// deserves an answer in the record (Q3's silence governs the
		// AUTOMATIC paths, not a person pressing a button).
		now := sc.Clock.Now().UTC()
		f := Fire{TriggerID: s.ID, Blueprint: s.Blueprint, Workspace: s.Workspace,
			Goal: s.Goal, Provenance: ProvenanceTrigger, Instant: now, At: now}
		reason := "paused — fire-now honours the operational off (TR12: not right now means not when asked nicely either)"
		sc.answer(ctx, s, f, OutcomeDidNotFire, reason)
		return ControlAnswer{TriggerID: id, Found: true, Paused: true, Outcome: string(OutcomeDidNotFire), Reason: reason}, nil
	}
	now := sc.Clock.Now().UTC()
	// The EXACT now, not the minute grid (T9 walk finding #4): trigger_fire
	// is UNIQUE(schedule_id, instant) — that key is the tick's idempotency
	// — and a fire-now that truncated to the minute both collided with the
	// tick's row (the fired answer vanished into a logged record-failure)
	// and blind-sided the overlap gate (which asks "what fired BEFORE this
	// instant?" — a row at the same instant is invisible to its own query).
	// fire-now has no scheduled instant; the instant it fired IS the moment
	// the human asked.
	due := now
	loc, expr := time.UTC, cronLike(nil)
	if s.Kind == KindSchedule {
		l, err := LoadZone(s.Zone)
		if err != nil {
			return ControlAnswer{TriggerID: id, Found: true,
				Reason: "the schedule's zone does not load: " + s.Zone + " — fix the row before firing it"}, nil
		}
		e, err := s.ScheduleExpr()
		if err != nil {
			return ControlAnswer{TriggerID: id, Found: true,
				Reason: "the schedule's expression does not parse: " + err.Error()}, nil
		}
		loc, expr = l, e
	}
	// THE same walk a tick takes. Window, overlap, governance, start —
	// fire-now has no gate it skips and no answer it invents.
	f, reason := sc.fireAt(ctx, s, loc, expr, due, now)
	return ControlAnswer{TriggerID: id, Found: true, Paused: s.Paused,
		Outcome: string(f.Outcome), LoopID: f.LoopID, Reason: reason}, nil
}
