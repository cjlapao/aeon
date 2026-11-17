package retro

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// ErrNoPin is Compose's answer when the loop's pin cannot be read at all:
// with no root, slug or updated_at there is no scope and no terminal key,
// so no honest row exists to write. The daemon logs and drops — a skipped
// fact, never a fabricated one.
var ErrNoPin = errors.New("retro: the loop's pin is not readable")

// Reader is the whole world the recorder sees — eight store methods, no
// more. It is an interface so Compose tests fake it without a daemon or a
// database; the *store.Store behind the daemon satisfies it directly.
// Every method is a READ (RT3.2: the write side reads and records, and
// nothing here can start a loop, change a grant or edit a setting).
type Reader interface {
	ReadPinnedLoop(id string) (store.PinnedLoop, error)
	ListSteps(sc store.Scope) ([]store.Step, error)
	ListTasks(sc store.Scope) ([]store.Task, error)
	ListRuns(loopID string) ([]store.Run, error)
	LoopCost(loopID string) (store.RunCost, error)
	CountEscalationQuestions(sc store.Scope) (int, error)
	AgentBySlug(slug string) (store.Agent, bool, error)
	AgentByName(name string) (store.Agent, bool, error)
}

// Event is the terminal news already reduced to what the recorder may
// store: which loop, and the normalized outcome kind. The daemon does the
// normalization (it is the only party that sees the engine's
// *loop.BlockedReason shape; retro must not import loop), and the
// outcome_kind string is one of: done | aborted | cancelled |
// blocked/<kind> | blocked/other:<kind> — the closed vocabulary, because
// Compose must never be handed free text (the Do-NOT: no reason detail in
// a fact).
type Event struct {
	LoopID      string
	OutcomeKind string
}

// Compose reads the loop's facts through Reader and assembles the outcome
// row plus its per-agent rows. It writes nothing and errors only when the
// pin is unreadable — that is the one absence that makes an honest row
// impossible.
//
// Every OTHER read failure degrades the row, never aborts it (Behaviour
// §2): the field stays NULL (a pointer that stays nil — never a zero),
// the field's name lands in `missing`, and the caller records
// complete=0 and logs one line per name. The daemon's reactor is the
// caller; the names are the log payloads.
//
// Two readings are named here because they are decisions, not accidents:
//
//   - cost is store.LoopCost — THE floor (F11), never re-summed here;
//     its own counts ride along, so the row states its own honesty.
//   - verified_pass is ALWAYS NULL: T0 §1.9 measured that the step row
//     cannot tell a handoff that DECLARED verification=pass from one that
//     said nothing (the engine only refuses the declared `unverified`,
//     advance.go's storedOutcome and handler/handoff.go:175), and a
//     `done` count would silently claim a pass the handoff never said.
//     The NULL is permanent by design, so a recorder row always arrives
//     complete=0 — the scorecard's `partial` state exists for exactly
//     this, and T4 reads it without inventing a number T0 refused.
func Compose(ctx context.Context, r Reader, ev Event) (store.RetroOutcome, []store.RetroAgent, []string, error) {
	pin, err := r.ReadPinnedLoop(ev.LoopID)
	if errors.Is(err, store.ErrNotFound) {
		return store.RetroOutcome{}, nil, nil, ErrNoPin
	}
	if err != nil {
		return store.RetroOutcome{}, nil, nil, fmt.Errorf("retro: read the pin of %s: %w", ev.LoopID, err)
	}
	sc := store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot}
	var missing []string
	note := func(field string) { missing = append(missing, field) }

	out := store.RetroOutcome{
		ComponentRoot:    pin.Loop.ComponentRoot,
		LoopSlug:         pin.Loop.Slug,
		OutcomeKind:      ev.OutcomeKind,
		TerminalKey:      TerminalKey(ev.OutcomeKind, pin.UpdatedAt),
		BlueprintID:      pin.BlueprintID,
		BlueprintVersion: ptr(pin.BlueprintVersion),
		StartedByKind:    pin.Loop.StartedBy,
		Attended:         pin.Attended, // the third state is the pin's, untouched
		QARounds:         ptr(pin.Loop.QARounds),
		TotalFailedGates: ptr(pin.Loop.TotalFailedGates),
		SplitsDone:       ptr(pin.Loop.SplitsDone),
		Source:           "recorder",
	}
	// The goal coordinates: a loop started without a goal has none — both
	// NULL, not an empty string beside a zero that reads like a version.
	if pin.Loop.GoalID != "" {
		out.GoalID = pin.Loop.GoalID
		out.GoalVersion = ptr(pin.Loop.GoalVersion)
	}

	// Tasks: the sums RT2 names (F8). `complete` is the ladder's done word.
	if tasks, err := r.ListTasks(sc); err != nil {
		note("tasks")
	} else {
		var done, failed, retries, review, sec int
		for _, t := range tasks {
			switch t.Status {
			case "complete":
				done++
			case "failed":
				failed++
			}
			retries += t.Retries
			review += t.ReviewRounds
			sec += t.SecurityRounds
		}
		out.TasksDone, out.TasksFailed = ptr(done), ptr(failed)
		out.Retries, out.ReviewRounds, out.SecurityRounds = ptr(retries), ptr(review), ptr(sec)
	}

	// Steps: the loop-level verdicts plus the per-agent groups. A step
	// with no agent belongs to nobody — it counts in nothing, because
	// inventing an agent row for "" would misattribute work to a name
	// that does not exist.
	var unverified, vFail int
	groups := map[string]*agentAgg{}
	if steps, err := r.ListSteps(sc); err != nil {
		note("steps")
	} else {
		for _, st := range steps {
			switch st.Outcome {
			case "reject":
				vFail++
			case "unverified":
				unverified++
			}
			if st.Agent == "" {
				continue
			}
			g := groups[st.Agent]
			if g == nil {
				g = &agentAgg{}
				groups[st.Agent] = g
			}
			g.add(st)
		}
		out.UnverifiedSteps, out.VerifiedFail = ptr(unverified), ptr(vFail)
	}
	// The permanent honest NULL (doc above), named once in `missing` so
	// the reactor logs it like any other unreadable field.
	out.VerifiedPass = nil
	note("verified_pass (no persisted handoff DECLARATION — T0 §1.9; NULL by design)")

	// Wall: the run RT2 names — the loop's last ENDED loop_run, else NULL.
	// ListRuns is run_seq-ascending, so "last ended" is simply the final
	// row carrying an ended_at; a running (never-ended) tail row is not a
	// span and does not clear an earlier one.
	if runs, err := r.ListRuns(ev.LoopID); err != nil {
		note("wall")
	} else {
		for i := len(runs) - 1; i >= 0; i-- {
			rn := runs[i]
			if rn.EndedAt == "" {
				continue
			}
			if ms, ok := wallMS(rn.StartedAt, rn.EndedAt); ok {
				out.WallMS = ptr(ms)
			} else {
				note("wall")
			}
			break // the last ended run decides, exactly once
		}
	}

	// Cost: THE floor, never a re-summation (F11).
	if c, err := r.LoopCost(ev.LoopID); err != nil {
		note("cost")
	} else {
		out.CostUSDFloor = ptr(c.USDAtLeast)
		out.CostSteps = ptr(c.Steps)
		out.CostPartialSteps = ptr(c.Partial)
		out.CostUnknownSteps = ptr(c.Unknown)
	}

	if n, err := r.CountEscalationQuestions(sc); err != nil {
		note("escalations")
	} else {
		out.Escalations = ptr(n)
	}

	// Agent rows, name-sorted for a deterministic write (map order would
	// make the same event's replay bytes differ run to run).
	var agents []store.RetroAgent
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		g := groups[name]
		slug, known := resolveAgent(r, name)
		ra := store.RetroAgent{
			Agent:            slug,
			AgentSlugKnown:   known,
			Steps:            ptr(g.steps),
			StepsDone:        ptr(g.done),
			StepsRejected:    ptr(g.rejected),
			StepsBlocked:     ptr(g.blocked),
			VerifyFail:       ptr(g.rejected),
			VerifyUnverified: ptr(g.unverified),
			// VerifyPass: NULL, the loop-level reason — the same
			// persisted-state gap, one level down.
			CostPartialSteps: ptr(g.partial),
			CostUnknownSteps: ptr(g.unknown),
		}
		if g.sawCost {
			ra.CostUSDFloor = ptr(g.floor)
		}
		if g.sawMS {
			ra.AgentMS = ptr(g.msSum)
		}
		agents = append(agents, ra)
	}
	return out, agents, missing, nil
}

// resolveAgent is RT2's slug rule: an agent row answers the slug
// (name-first-then-slug reads, both S43's); no row keeps the literal with
// known=false, and a failed READ also keeps the literal — claiming a slug
// from a store error would be the fabrication this slice exists to stop.
func resolveAgent(r Reader, name string) (string, bool) {
	if a, found, err := r.AgentByName(name); err == nil && found {
		return a.Slug, true
	}
	if a, found, err := r.AgentBySlug(name); err == nil && found {
		return a.Slug, true
	}
	return name, false
}

// agentAgg is one agent's running totals over the loop's steps.
type agentAgg struct {
	steps, done, rejected, blocked, unverified int
	sawCost                                    bool
	floor                                      float64
	partial, unknown                           int
	sawMS                                      bool
	msSum                                      int64
}

func (g *agentAgg) add(st store.Step) {
	g.steps++
	switch st.Outcome {
	case "done":
		g.done++
	case "reject":
		g.rejected++
	case "blocked":
		g.blocked++
	case "unverified":
		g.unverified++
	}
	g.sawCost = true
	switch st.Cost.State {
	case model.CostKnown:
		g.floor += st.Cost.USD
	case model.CostPartial:
		g.floor += st.Cost.USDAtLeast
		g.partial++
	default: // unknown, and any word this measure does not know: counted, never added
		g.unknown++
	}
	if st.AgentMS != nil {
		g.sawMS = true
		g.msSum += int64(*st.AgentMS)
	}
}

// wallMS is the loop_run span RT2 names. Times are the store's isoUTC
// strings (ms precision); an unparseable pair is an honest miss, not a
// zero.
func wallMS(startedAt, endedAt string) (int64, bool) {
	start, err1 := time.Parse(time.RFC3339Nano, startedAt)
	end, err2 := time.Parse(time.RFC3339Nano, endedAt)
	if err1 != nil || err2 != nil || end.Before(start) {
		return 0, false
	}
	return end.Sub(start).Milliseconds(), true
}

func ptr[T any](v T) *T { return &v }
