package trigger

// S49 T4 — the actor a control write names as its author (AU1 in this
// package's words). The control plane has always answered in sentences;
// this adds the one sentence it never spoke: WHO asked. The protocol door
// refuses a non-person first, the store refuses one before SQL, and this
// is the third lock — the door, the service, and the row all refuse a
// write nobody signed, each in the same words.
//
// The trigger package stays store-free and daemon-free (engine discipline):
// Actor is a bare pair of strings, and the ONLY production actors today are
// the socket's peer person (the daemon stamps it) and nothing else — the
// CLI dials that same socket (P34), so even argv arrives as a peer.

import (
	"context"
	"fmt"
)

// Actor is who asked. Kind is "user" for a person at the endpoint; an
// agent's actor never reaches this package — the doors refuse it by name —
// but the type carries Kind anyway so a regression arrives labelled, and
// the refusal can quote what it found.
type Actor struct {
	Kind string
	ID   string
}

// UserActor is the one shape a write may arrive in.
func UserActor(id string) Actor { return Actor{Kind: "user", ID: id} }

// Person is AU1 in one predicate: a user kind AND a non-blank id. An actor
// without an id has nothing for the audit to name, so it is refused exactly
// as a non-person is.
func (a Actor) Person() bool { return a.Kind == "user" && a.ID != "" }

// NotHumanSentence is THE sentence, one definition shared by the protocol
// door, the daemon service, and here (the store's requireUser wraps the
// same words around ErrActorNotHuman) — a paraphrase would be a second
// authority over who may write a trigger.
func NotHumanSentence(a Actor) string {
	return fmt.Sprintf("triggers are written by a person; this actor is %q", a.Kind)
}

// HumanWriter is the OPTIONAL store capability that makes a control write
// auditable: SaveTriggerScheduleAs carries the actor and the audit op into
// the write's own transaction (S49 AN3/AN4). It is optional — asserted at
// the call, never on the base Store interface — because clock.go is engine
// and byte-frozen: its Store interface keeps S20's shape, and a daemon
// whose rows adapter implements nothing here behaves exactly as it did
// before S49 (the fakes in this package's own tests prove the base path
// still works, untouched).
type HumanWriter interface {
	SaveTriggerScheduleAs(ctx context.Context, s Schedule, a Actor, op, summary string) error
}
