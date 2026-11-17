// Package continuous is the scheduler's pure core (CN1): given a snapshot,
// it returns the decisions a tick would make — and for every unit it did
// not start, the first reason it failed, from a closed vocabulary (CN15).
//
// The purity is the design, not a virtue attached to it: selection holds
// no SQL, no clock read, no goroutine and no daemon import, so every rule
// is a table-driven test and the preview IS the tick by construction —
// the same pure function over the same shape of data. The seams the
// daemon fills (budgets, grants, occupancy, readiness) enter here as
// SNAPSHOT FACTS computed by the adapter before Plan runs; that keeps the
// rules in one readable function instead of spread across callers.
//
// What crosses the boundary is ids and enums only — the decision sentence
// never carries user text (SEC-033); a title belongs to roadmap.show, not
// to a scheduling log line.
package continuous

import (
	"time"

	"github.com/cjlapao/aeon/internal/roadmap"
)

// Master is the daemon's continuous scheduler master switch as the
// snapshot carries it. Master off or paused stops EVERYTHING else from
// being evaluated: not-enabled, occupied and window states are not even
// consulted (plan step 1) — a paused switch is a paused switch.
type Master struct {
	Enabled bool
	Paused  bool
}

// Settings is the effective S44 settings the plan enforces. The adapter
// resolves them once per snapshot (spec, per-root override, default) and
// records their sources; the core just applies the numbers.
type Settings struct {
	// MaxSlots is CN9's capacity: the sum of agent slots a tick may have
	// running or starting (effective max_parallel_agents weights).
	MaxSlots int
	// MaxStartsPerRootPerHour is CN12's rate backstop (1..60, default 6).
	MaxStartsPerRootPerHour int
	// AgingHours is CN11's fairness: a candidate deferred for capacity at
	// least this long goes to the front of the queue.
	AgingHours int
	// TickSeconds is the worker's cadence (T5's Run loop); 0 reads as the
	// default. It never reaches Plan — Plan is a function, not a clock
	// owner — so the field lives in the same declared bag the adapter
	// resolves and the Worker reads for its ticker.
	TickSeconds int
}

// TickEvery resolves the ticker's period, default included: the settings
// seam may answer 0 (not declared yet) and the worker still ticks.
func (s Settings) TickEvery() time.Duration {
	n := s.TickSeconds
	if n <= 0 {
		n = 15 // the default cadence: responsive, not busy
	}
	return time.Duration(n) * time.Second
}

// LoopRef names a loop occupying a repository: its slug and its CURRENT
// state, the two facts CN8's sentence is allowed to name (state feeds
// trigger.StillGoing; no run title crosses this line).
type LoopRef struct {
	Slug  string
	State string
}

// QueueUnit is the repository's queue head as the snapshot carries it.
// Attended keeps loop/queue's three states (S24 D5): declared true means a
// person is steering it and the scheduler must not start it; false and nil
// both mean unattended — nil is "never declared", which belongs to the
// person-driven path, and pinning it to the scheduler's own default is
// exactly what attending is for (T0's F-findings, OD6). AutonomyCap rides
// the CN14 cap ('grant' | 'none' | 'outward') to the START; it never gates
// selection here.
type QueueUnit struct {
	ID          int64
	Status      string // snapshot fact: the queue's vocabulary, head is "ready"
	Attended    *bool
	AutonomyCap string
}

// Dep is one OPEN or CANCELLED upstream edge as the snapshot carries it
// (S27 T3's graph, read by the adapter). Done dependencies are not
// carried at all — readiness consumes them silently.
type Dep struct {
	ID     int64
	Status roadmap.Status
}

// RoadmapUnit is one ready roadmap item. The fields are DERIVED FACTS the
// adapter read from the row and the graph at snapshot time — the core
// re-derives nothing from paths or text, and Plan cannot start a unit
// whose facts do not add up to ready (the Do-NOT law).
type RoadmapUnit struct {
	ID       int64
	Priority roadmap.PriorityState
	// BriefOK and PlanOK are the two artifact facts IsReady checks, as
	// booleans — the paths themselves are user text and never travel.
	BriefOK bool
	PlanOK  bool
	// OpenDeps carries every dependency that is not done: a cancelled
	// dependency is its OWN verdict (dependency-cancelled), not "waiting".
	OpenDeps    []Dep
	Claimed     bool
	AutonomyCap string
}

// Subject is what a decision is about: the queue item, the roadmap item,
// the repository, or the occupying loop. It is a KEY (kind + id), never a
// sentence — decisions, preview rows and the deferral log all speak by it.
type Subject struct {
	Kind string // "queue" | "roadmap" | "repository" | "loop"
	ID   string
}

// SubjectKey is Subject flattened for map use (FirstDeferral).
type SubjectKey string

func (s Subject) Key() SubjectKey {
	return SubjectKey(s.Kind + ":" + s.ID)
}

// Repo is one repository's whole world as the adapter saw it. The fields
// below the settings facts are exactly the CN20 row's readable shape plus
// the two live reads (occupancy, queue head).
type Repo struct {
	Root   string
	RootID int64
	// Enabled is CN20's per-repository switch; false is repo-off.
	Enabled bool
	// Template is CN5's work template (the blueprint a roadmap item runs).
	// TemplateOK is the pick-time revalidation fact (false ⇒ refuse
	// template-invalid — a blueprint removed or broken since enable leaves
	// the item untouched and says so). Queue items carry their own
	// blueprint and never consult it.
	Template   string
	TemplateOK bool
	Window     Window
	// MinPriority gates ROADMAP candidates only (OD4: default assumed, a
	// person may raise it to known). The queue's order is a human
	// instruction and is never second-guessed (CN6.1).
	MinPriority roadmap.PriorityState
	// Occupying is every loop row on this root that is not done/cancelled;
	// trigger.StillGoing decides which of them OCCUPY (CN8).
	Occupying []LoopRef
	// QueueLive counts the repository's live queue rows
	// (pending|sizing|ready|running|blocked) — the drain gate that admits
	// roadmap work (CN6.2), stricter than R4's "rows exist" probe.
	QueueLive int
	// QueueHead is NextReadyQueueItem's answer (nil when none).
	QueueHead *QueueUnit
	// Ready is the repository's ready roadmap rows with their dependency
	// facts — every row, whether or not it can be a candidate.
	Ready []RoadmapUnit
	// LastStart is this repository's last SCHEDULER start (zero = never);
	// CN11's round-robin key.
	LastStart time.Time
	// StartsInHour are the starts inside the sliding CN12 window, oldest
	// first — its length is the count, its head is the leave-instant.
	StartsInHour []time.Time
	// Weight is the repository's effective max_parallel_agents — its
	// candidate's slot cost (CN9). Minimum 1, enforced by the store.
	Weight int
	// FirstDeferral maps a subject to when its CURRENT run of capacity
	// deferrals began (CN11, derived by the adapter from the decision log;
	// absent = never deferred or since started).
	FirstDeferral map[SubjectKey]time.Time
	// The seams the daemon fills, arriving as facts so Plan stays pure
	// (CN24): budgets answers S55's check for THIS repository and reason,
	// and GrantAbsent is S56's "the repository's agent has no grant" (V13:
	// absent is a fact the core states, never a guess it hides).
	BudgetOK     bool
	BudgetReason string
	GrantAbsent  bool
}

// Slots is the global admission state: the weights currently in use
// (running loops' effective weights, summed by the adapter).
type Slots struct {
	Used int
}

// Snapshot is everything the pure core knows: NOW arrives as data (the
// clock is the adapter's, not the core's — the package reads it nowhere),
// and every other field is the adapter's read at that same instant.
type Snapshot struct {
	Now      time.Time
	Master   Master
	Settings Settings
	Slots    Slots
	Repos    []Repo
}

// Verdict is the closed set of things a tick can decide about a unit.
type Verdict string

const (
	VerdictStart  Verdict = "start"  // admitted: the adapter must claim-then-start this
	VerdictDefer  Verdict = "defer"  // could run, something time-or-capacity bounded says not now
	VerdictSkip   Verdict = "skip"   // this unit, as it stands, is not the scheduler's to start
	VerdictRefuse Verdict = "refuse" // a configuration that can never succeed, said once
)

// Decision is one line of the tick: what verdict, about which subject of
// which repository, from which reason, at what weight. Detail is a
// bounded, ids-and-enums string the SENDER of the reason controls; the
// renderer (the adapter, the preview wire) wraps it in its own locale.
type Decision struct {
	Root    string
	Subject Subject
	Verdict Verdict
	Reason  Reason
	Detail  string
	Weight  int
}
