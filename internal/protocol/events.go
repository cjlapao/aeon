package protocol

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/asyncworkers"
)

// S64 T3 — the async-worker lifecycle kinds. Aliased, not restated: the
// manager publishes through these exact strings, so "what the runner
// emits" and "what a subscriber filters on" are one definition read twice
// (the four-state types' rule for graph: restating a wire string is
// creating a second truth waiting to drift).
const (
	KindAsyncWorkerStarted     = asyncworkers.KindStarted
	KindAsyncWorkerProgress    = asyncworkers.KindProgress
	KindAsyncWorkerFinished    = asyncworkers.KindFinished
	KindAsyncWorkerFailed      = asyncworkers.KindFailed
	KindAsyncWorkerCancelled   = asyncworkers.KindCancelled
	KindAsyncWorkerInterrupted = asyncworkers.KindInterrupted
)

// # Events, seq, and the cursor (T6, T8, O2)
//
// The four kinds this slice emits. The daemon emits its OWN lifecycle (T8)
// because there are no loops, steps or stored events yet — so a subscriber
// would have nothing to receive, and subscriptions written blind now would
// first be exercised at S5, four slices later, with the loop engine as a
// confounding variable. A client connecting is a real event.
const (
	KindDaemonStarted       = "daemon.started"
	KindClientConnected     = "client.connected"
	KindClientDisconnected  = "client.disconnected"
	KindSubscriptionCreated = "subscription.created"
	// KindSubscriptionTick is a heartbeat delivered to a subscription that has been handed something, so a lost last event shows
	// as a jump in its delivery counter. Delivered live only, never written to the log.
	KindSubscriptionTick = "subscription.tick"
	KindRecoveryStarted     = "recovery.started"
	KindRecoveryStep        = "recovery.step"
	KindRecoveryResolved    = "recovery.resolved"
	KindRecoveryFinished    = "recovery.finished"
	KindLoopStarted         = "loop.started"
	KindLoopAccessChanged   = "loop.access.changed"
	KindLoopAdvanced        = "loop.advanced"
	KindLoopWaiting         = "loop.waiting"
	KindLoopSkipped         = "loop.skipped"
	KindLoopBlocked         = "loop.blocked"
	KindLoopFinished        = "loop.finished"
	// KindLoopRunFinished is S39 CR1: a run ended and the loop RESTES
	// (after: converse). The loop is not finished — the conversation is
	// open — so loop.finished must not fire; this is the event that carries
	// the run's outcome and its P32 cost floor to the timeline.
	KindLoopRunFinished = "loop.run.finished"
	// KindLoopRunStarted is S39 T9 (CR2): a NEW run claimed the loop — the
	// timeline needs the moment as loudly as the ending, and the payload
	// names who started it (user/agent/trigger) and the goal coordinates
	// the run pinned. The UI's unattended notice (T12) hangs on this.
	KindLoopRunStarted = "loop.run.started"
	// KindLoopStepDispatched is S31 D2: the orchestrator saying, before a loop
	// step's first event, "step X dispatched to agent Y, stepId N" — the link
	// that lets the loop view connect a ladder move to the step stream it
	// started. Step-family kinds (step.steer.*, step.tool.skipped,
	// step.route.changed, step.stopped) live beside the other step kinds in
	// `runtime.EventKinds()`, not here.
	KindLoopStepDispatched = "loop.step.dispatched"

	// KindSkillsChanged is S66 T3: the Agent Skills watcher saw the
	// catalog move — added/removed/changed names, plus the honest watch
	// state (watching false and watchError naming the blindness). It is
	// a NOTICE, not state: the rows are the catalog's re-scan, and a
	// subscriber that races it re-reads the same truth through
	// skills.list. Daemon-wide (no component root), like the lifecycle
	// kinds — the catalog belongs to the install, not to a project.
	KindSkillsChanged = "skills.changed"

	// KindLoopMessagePosted is S39 CR6: a message landed in a loop's
	// thread — who wrote it, what kind, and WHERE IT CAME FROM (user,
	// template, automation, blueprint). The loop screen draws the thread
	// from the row; this event is the paint trigger, payload included so a
	// subscriber needs no read-back for one message.
	KindLoopMessagePosted = "loop.message.posted"

	// S70 MB6 — the mailbox speaks on the stream. ASKED is emitted when a
	// wait:true ask suspends its caller (the question row and the ask event
	// are one moment); POSTED when a row lands in the mailbox (ask or
	// reply — the row itself carries which); DELIVERED when a boundary
	// drains a row into a turn (the pair stamp lands with it); ANSWERED
	// when a reply settles an ask through its result_of link. The payloads
	// name the row and its thread; the rows remain the source of truth
	// (a subscriber that races an event reads the same truth through
	// agent.messages).
	KindAgentMessageAsked     = "agent.message.asked"
	KindAgentMessagePosted    = "agent.message.posted"
	KindAgentMessageDelivered = "agent.message.delivered"
	KindAgentMessageAnswered  = "agent.message.answered"

	// S14 T10 — the task lifecycle (transport.go): the ask exists, and the
	// callee answered. These are fan-out notices, not state: the payload
	// carries the taskId and NOTHING else, because the rows are the source
	// of truth (a subscription that races an event reads the same truth
	// through agent.task.show that the event announced).
	KindTaskCreated = "task.created"
	// KindTaskChanged is S38 T15 (OL14): the org task's every transition —
	// created, moved, assigned, decided — as ONE kind with the previous
	// state inside the payload. Emitted AFTER the store commits; at-most-once
	// by design until S40's outbox (the daemon that crashes between commit
	// and publish loses the notification, never the row).
	KindTaskChanged   = "task.changed"
	KindTaskCompleted = "task.completed"
	KindTaskFailed    = "task.failed"

	// S56 T6 (AL11, R5) — the autonomy authority events. GRANTED/DENIED/
	// REVOKED are published by the socket handlers AFTER the store commits
	// (at-most-once by design, as task.changed); USED is published by the
	// loop's decision site after the use row is recorded. Payloads carry
	// ids and enums only — the rows are the source of truth (a subscriber
	// that races an event reads the same truth through autonomy.show /
	// autonomy.uses). No notify hint: the closed set in the class table is
	// S58's to widen (R5); AL publishes events only.
	KindAutonomyGranted = "autonomy.granted"
	KindAutonomyDenied  = "autonomy.denied"
	KindAutonomyRevoked = "autonomy.revoked"
	KindAutonomyUsed    = "autonomy.used"
	// S55 T5 (LC20) — the loop-control plane's two NEW kinds. A stop itself
	// rides the existing loop.waiting{reason:"stopped"} and a cancel the
	// existing loop.finished{status:"aborted"} (LC2/LC4: no new kind for the
	// end); these two cover the request desk, where a request HALTS NOTHING
	// (LC15: an inert row and an event — a person sees it and acts) and a
	// dismissal settles one. `loop.stop.requested` is the one with a notify
	// hint (warning): it is the agent asking a person for something.
	KindLoopStopRequested    = "loop.stop.requested"
	KindLoopControlDismissed = "loop.control.dismissed"

	// S55 T8 (LC) — the budget plane's two NEW kinds. A budget crossing its
	// warn line and a budget being exhausted are the budget's own news,
	// distinct from loop.blocked (the LOOP's consequence): one budget can
	// be watched by several loops, and the start-time hit (E3) has no loop
	// at all. Both carry the hit/warn's numbers (budget id, scope, subject,
	// dimension, window, floor, cap) so a subscriber needs no read-back;
	// the rows (budget_event warned/hit) remain the source of truth.
	KindBudgetWarn = "budget.warn"
	KindBudgetHit  = "budget.hit"

	// S57 T7 (VG5) — the post-merge watch plane's two NEW kinds. A merged PR
	// goes under watch and the watcher answers it from rows: `merge.watch.green`
	// is the job done (every reported check is success) and `merge.watch.red`
	// is the check that failed on the merged sha (the filer has fired, the
	// payload names the bug). A watch that never gets an answer EXPIRES and
	// emits nothing — absence is not a failure, so there is no "expired" kind.
	// The payload carries ids and enums only (watch = the merge event id, sha,
	// pr, the aggregate's one-line reason, bugId).
	KindMergeWatchGreen = "merge.watch.green"
	KindMergeWatchRed   = "merge.watch.red"

	// S57 T7 (VG3/VG16) — the gate-reactor plane's one NEW kind. A terminal
	// gate failure (a verify run the loop could not fix, an exhausted verify
	// rework, a merge refused by a red check) filed its deduped bug: the
	// event says a gate went red and where the bug stands. The payload
	// carries ids and enums only (kind, loop, run, pr, bugId) — never the
	// reason's text, which is the bug body's job.
	KindGateFailed = "gate.failed"

	// S75 (TD5) — the loop's todo list. `todo.updated` fires on every write and
	// every close, so a live client never parses a tool result to learn the
	// list. (The runtime's reminder is a STEP event, `step.todo.nudged`, in the
	// step stream beside `step.compacted`.)
	KindTodoUpdated = "todo.updated"

	// S55 T9 (LC20) — the watchdog plane's one NEW kind. A watchdog firing is
	// the notify arm's news: something about a running loop changed and a
	// person should look. The stop and block arms ride the existing
	// loop.waiting{reason:"stopped"} and loop.blocked (LC2/LC17: the moves
	// are the loop's own states, audited with actor {system,watchdog});
	// only the notify — which moves nothing — needs its own kind. The
	// payload carries the rule, signal, action and episode (enums only) so
	// a subscriber needs no read-back.
	KindWatchdogFired = "watchdog.fired"

	// S62 T6 (BK16) — the backup family's four kinds. The first two are the
	// engine's own, emitted by backup.Run (T4) on the way out of a run:
	// finished carries the file's basename (never a path), its size, digest,
	// applied migrations, the verify level reached, the free-space state and
	// the reason/actor enums; failed carries the reason/actor/error_kind
	// enums and the stage. The last two are the daemon service's, emitted
	// from the scheduled maintenance task (T6): stale fires ONCE per
	// transition into the stale state and re-arms when the state leaves it;
	// pruned fires after a prune applied a fenced deletion, naming the
	// removed files by basename. Payloads are ids/enums/numbers and
	// basenames only — never a path (BK16). The notify.events class table
	// decides (S58 T4 deleted the switch; a kind with no row notifies
	// nothing).
	KindBackupFinished = "backup.finished"
	KindBackupFailed   = "backup.failed"
	KindBackupStale    = "backup.stale"
	KindBackupPruned   = "backup.pruned"

	// S58 T4 (SV4) — `question.asked` has been a WIRE kind since S3
	// (step.go emitted the literal; T1's goldens pinned its payload) but
	// it was never NAMED, so it could not sit in the closed kinds list or
	// in the class table. Naming it is the additive act: the constant, a
	// Kinds() entry and the notify.events row. The emitters are unchanged
	// (T4.md: the question.asked emitters stay as they are) — they emit
	// the same literal, now spelled by the constant.
	KindQuestionAsked = "question.asked"
)

// Kinds is every event kind the daemon can emit, for the conformance fixtures
// and for a test that asserts each is reachable.
//
// It grows with each slice — **additively**, which does not bump
// PROTOCOL_VERSION (see version.go's rule). S2 contributed the four lifecycle
// kinds; S3 added `ask.*`.
func Kinds() []string {
	return []string{
		// S2 — the daemon's own lifecycle (T8)
		KindDaemonStarted,
		KindClientConnected,
		KindClientDisconnected,
		KindSubscriptionCreated,
		// S3 — the model layer
		KindAskDelta,
		KindAskDone,
		KindRecoveryStarted,
		KindRecoveryStep,
		KindRecoveryResolved,
		KindRecoveryFinished,
		// S8 — a durable runner and its visible ladder transitions.
		KindLoopStarted,
		KindLoopAccessChanged,
		KindLoopAdvanced,
		KindLoopWaiting,
		KindLoopSkipped,
		KindLoopBlocked,
		KindLoopFinished,
		// S39 — a run ended inside a resting loop (CR1).
		KindLoopRunFinished,
		KindLoopRunStarted,
		// S31 — the orchestrator naming the step it dispatched (D2).
		KindLoopStepDispatched,
		// S39 — a thread message posted, with its provenance (CR6).
		KindLoopMessagePosted,
		KindAgentMessageAsked,
		KindAgentMessagePosted,
		KindAgentMessageDelivered,
		KindAgentMessageAnswered,
		// S14 — the task lifecycle, emitted by the agent-services seam
		// (agent.go) after invoke, complete and fail settle rows.
		KindTaskCreated,
		KindTaskChanged,
		KindTaskCompleted,
		KindTaskFailed,
		// S56 — the autonomy authority events (granted/denied/revoked from
		// the socket handlers after commit; used from the loop's decision
		// site after the use row lands).
		KindAutonomyGranted,
		KindAutonomyDenied,
		KindAutonomyRevoked,
		KindAutonomyUsed,
		// S55 T5 (LC20) — the request desk's two kinds (LC15: an inert row
		// and an event; nothing halts until a person acts).
		KindLoopStopRequested,
		KindLoopControlDismissed,
		// S55 T8 (LC) — the budget plane's two kinds (the warn line crossed,
		// the cap exhausted — the budget's own news, not the loop's).
		KindBudgetWarn,
		KindBudgetHit,
		// S55 T9 (LC20) — the watchdog plane's kind (the notify arm's event;
		// the stop/block arms ride the loop's own kinds).
		KindWatchdogFired,
		// S62 T6 (BK16) — the backup family's four kinds (the run's own
		// finished/failed, the state machine's stale, retention's pruned).
		KindBackupFinished,
		KindBackupFailed,
		KindBackupStale,
		KindBackupPruned,
		// S57 T7 (VG5) — the post-merge watch plane's two kinds (the green
		// that ends a watch, the red that files; expiry emits nothing).
		KindMergeWatchGreen,
		KindMergeWatchRed,
		// S57 T7 (VG16) — the gate-reactor plane's kind (a terminal gate
		// failure filed its bug).
		KindGateFailed,
		KindTodoUpdated,
		// S58 T4 (SV4) — the wire kind S3 has always emitted, named and
		// listed so the class table and the hint sweep can speak it.
		KindQuestionAsked,
	}
}

// ComponentRooted is implemented by payloads that know which project they
// belong to (S31 D1). `OnEvent` stays one hook — `func(kind string, payload
// any)` — and the root travels INSIDE the payload, because the alternative
// (a second argument) would have rippled a signature change through every
// emitter in this package, including the ones that genuinely have no project
// (`daemon.started`, `client.connected`, `ask.*`). A payload that does not
// implement it publishes with an EMPTY root, which the filter treats as
// daemon-wide — matching every subscriber. That is correct for the daemon's
// own lifecycle and wrong for a step stream, which is precisely why step
// payloads carry the real root (CODEMAP (g) names the leak).
type ComponentRooted interface {
	// EventComponentRoot returns the workspace root the event is filed under,
	// or "" when there genuinely is no project.
	EventComponentRoot() string
}

// Backing is the durable allocator and replay source for events.
//
// # S4 made `seq` durable, and this is the seam that let it
//
// S2 allocated `seq` from an in-memory counter that reset on every daemon
// restart. It said so here, in a doc comment, and in its demo — deliberately,
// so nobody would build a client that trusted a cursor across a restart.
//
// **S4 closed that.** The store's `event` table is the allocator: `seq` is an
// AUTOINCREMENT rowid, so it survives a restart, and a reconnecting client's
// cursor answers "did I miss anything" across a daemon upgrade rather than only
// across a dropped connection.
//
// The seam is an interface rather than an import because `internal/protocol`
// imports nothing transport-shaped and nothing storage-shaped (T11/I1) — that
// is what lets S19 mount the same method table on HTTP and MCP-stdio. The
// adapter over `internal/store` lives in `internal/daemon`, which already knows
// both sides.
//
// The methods take and return protocol types; the adapter converts. It is
// deliberately narrow: allocate, read a range, report the head.
type Backing interface {
	// Append persists the event and returns it with its allocated seq and
	// timestamp. The ONLY allocator (S4's DB20): two allocators produce
	// duplicates or regressions, and a cursor handed a duplicate silently
	// delivers the same event twice.
	Append(kind, loopSlug, componentRoot string, payload json.RawMessage) (Event, error)

	// Range returns up to limit events after `after` matching the filter, in
	// seq order. Gaps are expected — a filtered subscription receives ids with
	// holes in them, and the holes carry no information a client needs.
	Range(after uint64, filter Filter, limit int) ([]Event, error)

	// Head is the highest allocated seq, or 0 before anything is appended.
	Head() (uint64, error)
}

// EventLog allocates seq and serves a cursor.
//
// # Two modes, and only one of them ships
//
// With a Backing attached — which is every `aeon serve`, because S4's store is
// opened before the daemon is built — `seq` comes from the `event` table and is
// DURABLE. The in-memory ring is then a cache in front of it, and a cursor
// older than the ring is served from the table.
//
// With no Backing, `seq` is an in-memory counter that resets. That mode exists
// for tests that construct a log without a database, and it is NOT a shipped
// configuration: a storeless daemon has no way to be started.
//
// # Why there is a retained log at all, against N6
//
// The brief's N6 says "no server-side replay buffer", and T4's validation
// requires that `after: n` replay the tail from n+1 across twenty events at
// three cursor positions. **Both cannot be literally true**: serving a cursor
// into the past means having the past. The resolution taken here, and the
// reason:
//
//   - N6's target is a DURABLE or per-subscription buffer — state held for a
//     client that may never return, which is the leak T5 and N6 are about.
//   - What this is instead: one bounded ring, shared by every subscription,
//     that forgets. It holds no client-specific state and cannot grow with the
//     number of clients.
//   - When a cursor is older than the ring, the subscription resumes at the
//     oldest retained seq and `from` says so — **the gap is reported, not
//     hidden**. That is what makes a bounded ring honest rather than a buffer
//     pretending to be a log.
//
// Recorded as a deviation in T4's notes rather than silently resolved, because
// the next person to read N6 deserves to find out why it is not literal.
type EventLog struct {
	mu   sync.RWMutex
	seq  uint64
	ring []Event
	// cap is the retained count. A guess, like the subscription bounds: enough
	// that a client reconnecting within a few seconds of a disconnect is served
	// from memory rather than from the table.
	cap int

	// backing is the durable allocator (S4). Nil only in tests.
	backing Backing
}

// WithBacking attaches the durable allocator, and is how the daemon makes `seq`
// survive a restart.
//
// # The sequencing (Q8), and why no live subscription breaks
//
// It is attached at CONSTRUCTION, before the daemon registers its methods and
// therefore before any client can connect — so there is no moment at which a
// subscription exists against the in-memory counter and then sees the table's
// numbering. A live swap is not supported and is not needed: the store opens
// first in `serve`, so the only ordering that can happen is the correct one.
//
// A daemon already running cannot be upgraded in place; it is restarted, which
// is the same thing the client's cursor is being made robust against.
func (l *EventLog) WithBacking(b Backing) *EventLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.backing = b
	if b != nil {
		// Start from the table's head, so the first event after a restart
		// CONTINUES rather than colliding with one a client already saw.
		if head, err := b.Head(); err == nil {
			l.seq = head
		}
	}
	return l
}

// DefaultRetention is how many events the ring holds. A guess (see EventLog).
const DefaultRetention = 1024

func NewEventLog() *EventLog { return &EventLog{cap: DefaultRetention} }

// NewEventLogWithRetention is for tests that need to drive the ring past its
// limit without emitting a thousand events.
func NewEventLogWithRetention(n int) *EventLog { return &EventLog{cap: n} }

// Append allocates the next seq and records the event.
//
// seq is allocated exactly once per event and never reused: monotonic, no gaps,
// no duplicates. The allocation and the append happen under one lock, because
// allocating outside it would let two concurrent emitters write the ring out of
// seq order — and "monotonic" would then be true of the counter and false of
// what a subscriber receives.
func (l *EventLog) Append(kind, componentRoot string, payload any) (Event, error) {
	var raw json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Event{}, err
		}
		raw = encoded
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	var ev Event
	if l.backing != nil {
		// The STORE allocates (DB20). Doing it here as well would be a second
		// allocator, and the two would disagree the first time anything else
		// wrote to the table.
		stored, err := l.backing.Append(kind, "", componentRoot, raw)
		if err != nil {
			return Event{}, err
		}
		ev = stored
		if ev.Seq > l.seq {
			l.seq = ev.Seq
		}
	} else {
		l.seq++
		ev = Event{
			Seq:           l.seq,
			Kind:          kind,
			ComponentRoot: componentRoot,
			At:            now().UTC().Format(time.RFC3339Nano),
			Payload:       raw,
		}
	}

	l.ring = append(l.ring, ev)
	if l.cap > 0 && len(l.ring) > l.cap {
		l.ring = l.ring[len(l.ring)-l.cap:]
	}
	return ev, nil
}

// Head is the highest allocated seq. Zero before anything is emitted.
//
// With a Backing, the TABLE is the truth: another process may have appended
// since (two `aeon serve` instances are refused, but a CLI verb writing an
// event is not), and a head taken from memory would hand a client a cursor that
// skips whatever it missed.
func (l *EventLog) Head() uint64 {
	l.mu.RLock()
	backing := l.backing
	cached := l.seq
	l.mu.RUnlock()

	if backing != nil {
		if head, err := backing.Head(); err == nil {
			return head
		}
		// A failed read falls back to the cache rather than to zero: zero would
		// read as "nothing has happened", which is the one answer that is
		// certainly wrong.
	}
	return cached
}

// Oldest is the lowest retained seq, or head+1 when nothing is retained.
func (l *EventLog) Oldest() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.ring) == 0 {
		return l.seq + 1
	}
	return l.ring[0].Seq
}

// Resume decides where a subscription starts, and returns the events to replay.
//
// Three cases, and each is reported honestly through `from`:
//
//   - after == nil          → start at the head; nothing replayed.
//   - after inside the ring → from = after+1, replaying exactly that tail.
//   - after beyond the head → NOT an error. It means "nothing yet", and from is
//     head+1 — a client that saw more than the daemon has is a client talking to
//     a restarted daemon, which O2 says will happen.
//   - after older than the ring → from = the oldest retained seq, which is
//     GREATER than after+1. The client compares the two and knows it missed the
//     difference, instead of assuming continuity it did not get.
func (l *EventLog) Resume(after *uint64, filter Filter) (from uint64, replay []Event) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if after == nil {
		return l.seq + 1, nil
	}
	want := *after + 1
	if want > l.seq {
		return l.seq + 1, nil
	}

	from = want
	if len(l.ring) > 0 && l.ring[0].Seq > want {
		// The cursor is older than what is RETAINED IN MEMORY.
		//
		// Before S4 this was where the gap was reported and accepted. Now the
		// table holds it, so the honest answer is to serve it — and `from`
		// reports no gap because there is none.
		if l.backing != nil {
			stored, err := l.backing.Range(*after, filter, 0)
			if err == nil {
				return want, stored
			}
			// A failed read falls back to the ring and reports the gap, which
			// is what S2 did and is still better than silence.
		}
		from = l.ring[0].Seq
	}
	for _, ev := range l.ring {
		if ev.Seq >= from && filter.Matches(ev.Kind, ev.ComponentRoot) {
			replay = append(replay, ev)
		}
	}
	return from, replay
}

// Since returns retained events at or after seq matching the filter, without
// the resume semantics. For `debug` and tests.
func (l *EventLog) Since(seq uint64, filter Filter) []Event {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var out []Event
	for _, ev := range l.ring {
		if ev.Seq >= seq && filter.Matches(ev.Kind, ev.ComponentRoot) {
			out = append(out, ev)
		}
	}
	return out
}
