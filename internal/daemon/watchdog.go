// watchdog.go — the daemon's watchdog service (S55 T9, LC17/LC18/LC20).
//
// It is the one place a firing becomes an act: a ticker over the RUNNING
// loops only (a waiting/blocked/idle/terminal loop is never "stuck"), a fresh
// read of the rules on every tick (no cache — a disable is live at the next
// tick), the pure Evaluate from internal/watchdog, and one move per firing:
//
//   - notify: a kind-'watchdog' control event (moves nothing) plus the
//     watchdog.fired screen event (LC20);
//   - stop or block, whichever LC17's attendedness picks: an ATTENDED loop
//     parks waiting/stopped (ParkLoopStopped), an UNATTENDED or
//     never-declared one blocks stuck (BlockLoopStuck) — both audited with
//     actor {system, watchdog}, never a user actor (the service stamps it).
//
// A firing is once per (loop, rule, episode): the episode memory lives HERE
// (the pure package holds no state), and a loop that makes progress gets a
// new episode from Evaluate and may fire again. And by construction there is
// no retry: this file never calls an advance, a start or a resume (the grep
// test in cmd/aeon's watchdog_amend_test pins that), so a blocked loop stays
// blocked until a person moves it.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/watchdog"
)

// watchdogTickInterval is the service's tick — a NAMED CONSTANT, not a
// setting (the tick is the daemon's own rhythm, like the automation
// dispatcher's poll, not an operator's knob). Each tick adds a jitter of up
// to 20% so several daemons do not evaluate on the same wall-clock instant.
const watchdogTickInterval = 30 * time.Second

// watchdogActorKind and watchdogActorID are the actor every watchdog act
// stamps (LC17: the daemon's {system, watchdog}, NEVER a user — a watchdog
// stop that reads as a person's stop is an audit a person would trust).
const (
	watchdogActorKind = "system"
	watchdogActorID   = "watchdog"
)

// watchdogService is the running service: the daemon it acts through, the
// clock (injectable so the episode tests do not sleep), and the once-per-
// episode memory lastFired[loopID][ruleID] = the episode last acted on. The
// memory is shared by the ticker (which writes it, on its goroutine) and the
// watchdog.list door (which reads it, on a request goroutine), so mu guards
// both sides — a race on the map would be a torn episode the list renders.
type watchdogService struct {
	d         *Daemon
	now       func() time.Time
	mu        sync.RWMutex // guards lastFired
	lastFired map[string]map[string]string
}

// newWatchdogService builds the service over the daemon. It holds no store
// reference of its own: every read and every act goes through the daemon's
// store, so the V13 absence rule holds (a storeless daemon never ticks).
func (d *Daemon) newWatchdogService() *watchdogService {
	return &watchdogService{d: d, now: time.Now, lastFired: map[string]map[string]string{}}
}

// RunWatchdog is serve.go's goroutine body: it ticks until the context ends
// (the daemon's recovery context — T4's drain cancels it, and a stopped
// watchdog is a log line, never a dead daemon). A daemon without a store is
// an honest absence: nothing to observe, nothing to run.
func (d *Daemon) RunWatchdog(ctx context.Context) error {
	if d.store == nil {
		return nil // the honest absence — no rows, nothing to watch
	}
	if d.watchdog == nil {
		d.watchdog = d.newWatchdogService()
	}
	if err := d.watchdog.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// run is the tick loop.
func (s *watchdogService) run(ctx context.Context) error {
	for {
		delay := s.nextDelay()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		s.tick(ctx) // a tick that fails in the middle is a log line; the loop continues
	}
}

// nextDelay is the tick interval with its jitter (up to +20%: the jitter
// only spreads daemons over time, it never shortens the rhythm).
func (s *watchdogService) nextDelay() time.Duration {
	jitter := time.Duration(rand.Float64() * 0.2 * float64(watchdogTickInterval))
	return watchdogTickInterval + jitter
}

// tick is one evaluation pass: the rules re-read from the store (no cache —
// behaviour 7: a disable or a removal is live at the next tick), every
// RUNNING loop observed, and one move per fresh firing.
func (s *watchdogService) tick(ctx context.Context) {
	rules, err := s.d.store.ListWatchdogRules()
	if err != nil {
		s.d.log.Infof("watchdog: could not read the rules: %v", err)
		return
	}
	pins, err := s.d.store.PinnedLoopsByStatus("running")
	if err != nil {
		s.d.log.Infof("watchdog: could not read the running loops: %v", err)
		return
	}
	now := s.now()
	wsRules := make([]watchdog.Rule, 0, len(rules))
	for _, r := range rules {
		wsRules = append(wsRules, storeToWatchdogRule(r))
	}
	for _, pin := range pins {
		s.tickLoop(wsRules, pin, now)
	}
}

// tickLoop observes ONE loop and acts on its fresh firings. An observation
// read that fails skips the loop for this tick (an unreadable loop is
// touched not at all — the safe side of an honest error), and a firing whose
// episode was already acted on is dropped (once per (loop, rule, episode)).
func (s *watchdogService) tickLoop(rules []watchdog.Rule, pin store.PinnedLoop, now time.Time) {
	obs, ok := s.observe(pin)
	if !ok {
		return
	}
	for _, firing := range watchdog.Evaluate(rules, obs, now) {
		loopID := pin.Loop.ID
		if seen := s.lastFired[loopID]; seen != nil && seen[firing.Rule.ID] == firing.Episode {
			continue // the same episode was already acted on: one event per episode
		}
		s.act(obs, firing)
	}
}

// observe gathers the observation reads for one pinned running loop. It
// reports ok=false (and logs) when a source read fails — the loop is then
// left untouched this tick, which is the only safe reading of a fault.
func (s *watchdogService) observe(pin store.PinnedLoop) (watchdog.Observation, bool) {
	loop := pin.Loop
	sc := store.Scope{LoopSlug: loop.Slug, ComponentRoot: loop.ComponentRoot}

	obs := watchdog.Observation{
		LoopID:        loop.ID,
		Status:        "running", // PinnedLoopsByStatus("running") is the whole filter
		Attended:      pin.Attended,
		ComponentRoot: loop.ComponentRoot,
		Root:          loop.Root,
		Position:      watchdog.Position{Lane: loop.Phase, Item: loop.NowTask},
	}

	lastActivity, err := s.d.store.WatchdogLastActivity(loop.ID)
	if err != nil {
		s.d.log.Infof("watchdog: last activity of %s unreadable: %v", loop.ID, err)
		return obs, false
	}
	obs.LastActivityAt = lastActivity

	dispatches, err := s.d.store.WatchdogPositionDispatches(sc, obs.Position.Lane, obs.Position.Item)
	if err != nil {
		s.d.log.Infof("watchdog: position dispatches of %s unreadable: %v", loop.ID, err)
		return obs, false
	}
	obs.PositionDispatches = dispatches

	// A position with no item is not a position the same-position signal can
	// count: the step rows it would count do not exist, and counting nothing
	// six times is not progress that failed — it is no position at all.
	if obs.Position.Item == "" {
		obs.PositionDispatches = 0
	}

	hash, repeats, runSince, err := s.d.store.WatchdogReasonRun(loop.ID)
	if err != nil {
		s.d.log.Infof("watchdog: reason run of %s unreadable: %v", loop.ID, err)
		return obs, false
	}
	obs.LastReasonHash, obs.ReasonRepeats, obs.ReasonRunSince = hash, repeats, runSince

	runningSince, deadline, err := s.d.store.WatchdogRunningStep(sc)
	if err != nil {
		s.d.log.Infof("watchdog: running step of %s unreadable: %v", loop.ID, err)
		return obs, false
	}
	obs.StepRunningSince, obs.StepDeadline = runningSince, deadline
	return obs, true
}

// act is one firing turned into its move (LC17): notify appends the audit
// row and publishes the screen event (the loop keeps running); stop and
// block both park the loop, and which of the two the loop takes is decided
// by attendedness at fire time — an attended loop stops (waiting/stopped, a
// person can pick it up), an unattended or never-declared one blocks
// (blocked/stuck, only a person's resume or raise moves it again). The
// detail's action key is the rule's action on the notify arm and the move
// actually taken on the stop/block arm (a stop rule on an unattended loop
// reads block: the attendedness, not the rule, picked the move), so the
// fire row always says what happened to the loop.
func (s *watchdogService) act(obs watchdog.Observation, firing watchdog.Firing) {
	rule := firing.Rule
	detail := map[string]any{
		"ruleId":  rule.ID,
		"signal":  string(rule.Signal),
		"action":  string(rule.Action),
		"episode": firing.Episode,
	}
	switch rule.Action {
	case watchdog.ActionNotify:
		if err := s.d.store.AppendWatchdogFire(obs.LoopID, watchdogActorKind, watchdogActorID,
			fmt.Sprintf("the watchdog is watching this loop: %s crossed its threshold", rule.Signal),
			rule.ID, detail); err != nil {
			s.d.log.Infof("watchdog: the notify event of %s could not be written: %v", obs.LoopID, err)
			return
		}
		s.publishWatchdogFired(obs, firing)
		s.remember(obs.LoopID, rule.ID, firing.Episode)
	default: // stop and block: the move is the attendedness (LC17)
		attended := obs.Attended != nil && *obs.Attended
		taken := "block"
		if attended {
			taken = "stop"
		}
		detail["action"] = taken
		if attended {
			parked, err := s.d.store.ParkLoopStopped(obs.LoopID, watchdogActorKind, watchdogActorID,
				fmt.Sprintf("the watchdog stopped this loop: %s crossed its threshold", rule.Signal))
			if err != nil {
				s.d.log.Infof("watchdog: the stop of %s failed: %v", obs.LoopID, err)
				return
			}
			if !parked.Changed {
				return // a lost race wrote no event: the episode is not consumed
			}
		} else {
			parked, err := s.d.store.BlockLoopStuck(obs.LoopID, watchdogActorKind, watchdogActorID,
				fmt.Sprintf("the watchdog blocked this loop: %s crossed its threshold", rule.Signal),
				rule.ID, detail)
			if err != nil {
				s.d.log.Infof("watchdog: the block of %s failed: %v", obs.LoopID, err)
				return
			}
			if !parked.Changed {
				return // a lost race wrote no event: the episode is not consumed
			}
		}
		s.remember(obs.LoopID, rule.ID, firing.Episode)
	}
}

// publishWatchdogFired is the notify arm's screen event (LC20): the kind-
// rooted publish — the toast decision lives in the class table now (S58
// T4 replaced the switch with the resolver, consulted inside publish),
// so this emitter names the event and nothing else.
func (s *watchdogService) publishWatchdogFired(obs watchdog.Observation, firing watchdog.Firing) {
	payload := WatchdogFiredPayload{
		LoopID:        obs.LoopID,
		ComponentRoot: obs.ComponentRoot,
		RuleID:        firing.Rule.ID,
		Signal:        string(firing.Rule.Signal),
		Action:        string(firing.Rule.Action),
		Metric:        firing.Metric,
		Episode:       firing.Episode,
	}
	s.d.Publish(protocol.KindWatchdogFired, obs.ComponentRoot, payload)
}

// remember records the episode last acted on for (loop, rule) — the once-
// per-episode memory. The write is under the lock: the list door reads the
// map from a request goroutine while the ticker writes from its own.
func (s *watchdogService) remember(loopID, ruleID, episode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastFired[loopID] == nil {
		s.lastFired[loopID] = map[string]string{}
	}
	s.lastFired[loopID][ruleID] = episode
}

// storeToWatchdogRule maps the store's row onto the pure package's rule —
// the one translation between the two shapes (the store's strings are the
// CHECKed vocabulary; a row that could not map is a schema fault the tick
// logs, not a rule it invents).
func storeToWatchdogRule(r store.WatchdogRule) watchdog.Rule {
	return watchdog.Rule{
		ID:        r.ID,
		Scope:     watchdog.Scope(r.Scope),
		SubjectID: r.SubjectID,
		Signal:    watchdog.Signal(r.Signal),
		Threshold: r.Threshold,
		Action:    watchdog.Action(r.Action),
		Enabled:   r.Enabled,
		Origin:    watchdog.Origin(r.Origin),
	}
}

// WatchdogFiredPayload is the watchdog.fired event's payload (LC20): the
// rule, the signal, the action and the episode — enums only, so a
// subscriber needs no read-back. It is rooted at the loop's component so a
// component-scoped subscription sees it.
type WatchdogFiredPayload struct {
	LoopID string `json:"loopId"`
	// ComponentRoot is the S31 D1 field the ComponentRooted interface reads;
	// it does not ride the payload itself — the envelope carries it (the
	// budget events' shape), so a subscriber gets the root once.
	ComponentRoot string `json:"-"`
	RuleID        string `json:"ruleId"`
	Signal        string `json:"signal"`
	Action        string `json:"action"`
	Metric        int64  `json:"metric"`
	Episode       string `json:"episode"`
}

// EventComponentRoot makes the payload a ComponentRooted event (the root
// travels inside the payload, the S31 D1 convention).
func (p WatchdogFiredPayload) EventComponentRoot() string { return p.ComponentRoot }
