// Package mergequeue holds the merge queue's DECISIONS: the state machine,
// the enqueue validator, and the one-sentence describer. The daemon, the CLI
// and the agent tools never re-derive any of it (MQ5) — they call these
// functions and obey. The core files (state.go, transitions.go, decide.go)
// import nothing but fmt: no clock, no git, no forge, no database (a test
// enforces it).
//
// The closed vocabularies here MIRROR the store migration's CHECK lists;
// TestStatusSetMatchesMigrationCheck parses the embedded SQL and fails on any
// drift, so "Go and DDL" stay one truth without either importing the other.
package mergequeue

// Status is the queue's nine statuses — the same words as
// merge_queue_item.status's CHECK, in the same order.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusCatchingUp Status = "catching_up"
	StatusVerifying  Status = "verifying"
	StatusReady      Status = "ready"
	StatusMerging    Status = "merging"
	StatusMerged     Status = "merged"
	StatusConflicted Status = "conflicted"
	StatusFailed     Status = "failed"
	StatusRemoved    Status = "removed"
)

// StatusSet is the migration's status list, restated — the lock test compares.
var StatusSet = []string{"queued", "catching_up", "verifying", "ready", "merging", "merged", "conflicted", "failed", "removed"}

// Valid reports whether s is one of the nine. An exhaustive switch, so a new
// constant here without a case is a compile-visible gap.
func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusCatchingUp, StatusVerifying, StatusReady, StatusMerging,
		StatusMerged, StatusConflicted, StatusFailed, StatusRemoved:
		return true
	}
	return false
}

// IsOpen reports the six statuses that still hold a branch and (maybe) a
// slot — the same set the partial index merge_queue_one_open encodes.
func (s Status) IsOpen() bool {
	switch s {
	case StatusQueued, StatusCatchingUp, StatusVerifying, StatusReady, StatusMerging, StatusConflicted:
		return true
	}
	return false
}

// IsTerminal reports the three that are over.
func (s Status) IsTerminal() bool {
	return s == StatusMerged || s == StatusFailed || s == StatusRemoved
}

// ReasonKind is why an item ended (or was removed) — the eleven words of
// reason_kind's CHECK. The pure core only ever repeats a kind a FACT carried
// (a denial, a refusal, a person's dequeue); it never invents one.
type ReasonKind string

const (
	ReasonDenied          ReasonKind = "denied"
	ReasonConflict        ReasonKind = "conflict"
	ReasonGateFailed      ReasonKind = "gate-failed"
	ReasonHeadMoved       ReasonKind = "head-moved"
	ReasonProtected       ReasonKind = "protected"
	ReasonForge           ReasonKind = "forge"
	ReasonGit             ReasonKind = "git"
	ReasonStaleExhausted  ReasonKind = "stale-exhausted"
	ReasonRemovedByPerson ReasonKind = "removed-by-person"
	ReasonLoopStopped     ReasonKind = "loop-stopped"
	ReasonUnknown         ReasonKind = "unknown"
)

// ReasonSet mirrors the migration's reason_kind CHECK (with 'unknown').
var ReasonSet = []string{"denied", "conflict", "gate-failed", "head-moved", "protected",
	"forge", "git", "stale-exhausted", "removed-by-person", "loop-stopped", "unknown"}

// Valid reports whether k is one of the eleven.
func (k ReasonKind) Valid() bool {
	switch k {
	case ReasonDenied, ReasonConflict, ReasonGateFailed, ReasonHeadMoved, ReasonProtected,
		ReasonForge, ReasonGit, ReasonStaleExhausted, ReasonRemovedByPerson,
		ReasonLoopStopped, ReasonUnknown:
		return true
	}
	return false
}

// Mode is merge_queue_config.mode. off is the default and the whole reason
// the queue can be shipped dark (OD3): nothing starts, nothing enqueues
// itself; a person's enqueue still lands and sits `queued` (MQ13).
type Mode string

const (
	ModeOff    Mode = "off"
	ModeManual Mode = "manual"
	ModeAuto   Mode = "auto"
)

// ModeSet mirrors the config table's mode CHECK.
var ModeSet = []string{"off", "manual", "auto"}

// Valid reports whether m is one of the three.
func (m Mode) Valid() bool {
	switch m {
	case ModeOff, ModeManual, ModeAuto:
		return true
	}
	return false
}

// ConflictPolicy is merge_queue_config.conflict_policy (MQ9's three exits).
type ConflictPolicy string

const (
	PolicyAsk     ConflictPolicy = "ask"
	PolicyResolve ConflictPolicy = "resolve"
	PolicyBlock   ConflictPolicy = "block"
)

// PolicySet mirrors the config table's conflict_policy CHECK.
var PolicySet = []string{"ask", "resolve", "block"}

// Valid reports whether p is one of the three.
func (p ConflictPolicy) Valid() bool {
	switch p {
	case PolicyAsk, PolicyResolve, PolicyBlock:
		return true
	}
	return false
}

// LandMethod is merge_queue_config.land_method (OD4's default squash).
type LandMethod string

const (
	MethodSquash LandMethod = "squash"
	MethodMerge  LandMethod = "merge"
)

// MethodSet mirrors the config table's land_method CHECK.
var MethodSet = []string{"squash", "merge"}

// Valid reports whether m is one of the two.
func (m LandMethod) Valid() bool {
	switch m {
	case MethodSquash, MethodMerge:
		return true
	}
	return false
}

// Config is the repository's queue settings as gathered by the caller — the
// same defaults the config table carries for an absent row (mode off,
// ask, squash, 3).
type Config struct {
	Mode           Mode
	ConflictPolicy ConflictPolicy
	LandMethod     LandMethod
	MaxCatchUps    int
}

// DefaultConfig is what an absent merge_queue_config row means: dark.
func DefaultConfig() Config {
	return Config{Mode: ModeOff, ConflictPolicy: PolicyAsk, LandMethod: MethodSquash, MaxCatchUps: 3}
}

// Valid reports whether every field is in its set and the bound is 1–10.
func (c Config) Valid() bool {
	if !c.Mode.Valid() || !c.ConflictPolicy.Valid() || !c.LandMethod.Valid() {
		return false
	}
	return c.MaxCatchUps >= 1 && c.MaxCatchUps <= 10
}

// LoopState is where the item's landing or resolution loop stands — the
// five states the reactor reads, as words. "" means no such loop exists.
type LoopState string

const (
	LoopNone    LoopState = ""
	LoopPending LoopState = "pending"
	LoopRunning LoopState = "running"
	LoopDone    LoopState = "done"
	LoopBlocked LoopState = "blocked"
	LoopFailed  LoopState = "failed"
	LoopStopped LoopState = "stopped"
)

// Valid reports whether ls is one of the six known states (or none).
func (ls LoopState) Valid() bool {
	switch ls {
	case LoopNone, LoopPending, LoopRunning, LoopDone, LoopBlocked, LoopFailed, LoopStopped:
		return true
	}
	return false
}
