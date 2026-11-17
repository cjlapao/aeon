// Package exitcode is the one table a CI job branches on (C10, D12). A loop in
// CI must distinguish "failed" from "blocked" from "waiting for a human": those
// are three different things and only one is a problem with the run. A CI job
// that treats all non-zero exits as failures will fail a build because a loop
// politely asked a question — so there is one table, one mapping, and a code
// means the same thing everywhere, which is the only way a CI job can branch.
//
// It composes with S19: a CI-triggered loop is unattended, so a gate does not
// produce Waiting — it produces Blocked with the ask denied (G7, L22). Both
// codes exist because both situations do, and which one a CI job sees tells it
// whether running attended would have helped.
//
// The code is what a process returns; the three-part message (S23 T1) is what
// a message contains. Both, for the same event: a code with no message is a CI
// job with nothing to read (T1), so every non-zero exit accompanies a
// three-part error.
package exitcode

import (
	"github.com/cjlapao/aeon/internal/errs"
)

// Code is the table. Six codes, one table, one mapping (C10, D12).
type Code int

const (
	OK     Code = 0 // did what was asked — including an aborted loop (S17)
	Failed Code = 1 // the work failed — the one that is a problem with the run
	Blocked Code = 2 // the loop cannot proceed: a code action unavailable, a bad plan
	Waiting Code = 3 // a human is needed: a gate, a permission ask, an escalation
	Usage  Code = 4 // the caller's mistake: a bad flag, a bad argument
	Config Code = 5 // configuration or credentials (S23 T2)
)

// String names the code so --help and EXITCODES.md can document it (C10, D12):
// a code nobody documented is a code nobody branches on.
func (c Code) String() string {
	switch c {
	case OK:
		return "ok"
	case Failed:
		return "failed"
	case Blocked:
		return "blocked"
	case Waiting:
		return "waiting"
	case Usage:
		return "usage"
	case Config:
		return "config"
	}
	return "?"
}

// Event is what happened. A mapping is from an event (and whether a human was
// expected) to a code, never a literal — so a code means the same thing
// everywhere a CI job can branch on it.
type Event string

const (
	EventFailed  Event = "failed"  // a step failed
	EventGate    Event = "gate"    // a gate, permission ask, or escalation
	EventBlocked Event = "blocked" // a code action is unavailable, a bad plan
	EventAborted Event = "aborted" // a plan read and declined — a working system (S17)
	EventUsage   Event = "usage"   // a bad flag, a bad argument
	EventConfig  Event = "config"  // a bad config or a credential
)

// Attended says whether a human was expected. This is the whole of the S19
// composition: the same gate produces Waiting when attended (a person is
// needed) and Blocked when unattended (a CI job, and the ask is denied).
type Attendance bool

const (
	// Attended: a human is present; a gate produces Waiting.
	Attended Attendance = true
	// Unattended: a CI job; a gate produces Blocked, the ask denied (G7, L22).
	Unattended Attendance = false
)

// CodeFor is the one mapping (D12): an event, and whether a human was expected,
// to a code. A gate earns the table — Waiting when attended, Blocked when
// unattended — and an aborted loop is OK, because a declined plan is a working
// system (S17's T7), not a failure.
func CodeFor(event Event, attended Attendance) Code {
	switch event {
	case EventFailed:
		return Failed
	case EventBlocked:
		return Blocked
	case EventUsage:
		return Usage
	case EventConfig:
		return Config
	case EventAborted:
		return OK // a plan someone read and declined is not a failure
	case EventGate:
		// The gate that earns the table: attended is Waiting, unattended is
		// Blocked (the ask is denied, G7, L22).
		if attended == Attended {
			return Waiting
		}
		return Blocked
	}
	// An unknown event is a failure with the work, not a silent zero: a CI
	// job must not treat "I do not know" as success.
	return Failed
}

// Result is a code and its message, both produced for the same event. A code
// with no message is a CI job with nothing to read (T1), so a non-zero result
// carries the three-part error; a zero result may stand alone (did what was
// asked needs no explanation).
type Result struct {
	Code    Code
	Err     errs.Error // the three-part message; empty only when Code is OK
	HasMsg  bool
}

// NonZero reports whether the code is a failure of some kind — the only
// situation a message is required to accompany.
func (r Result) NonZero() bool { return r.Code != OK }

// Exit renders the code for a process to return. It is the one place a code
// becomes a process exit, so the table is the only mapping (D12).
func (r Result) Exit() int { return int(r.Code) }
