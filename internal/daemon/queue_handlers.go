package daemon

// S24 T7 — the queue's daemon-side seams. CG9's "one implementation
// underneath two transports" lives here: the socket handlers in
// internal/protocol/queue.go reach the queue through THESE fields, and
// the CLI (T6) dials those same methods — there is no queue code path
// that does not end in internal/queue. initQueue is called from
// initBlueprints (the wiring point the daemon already has); the store
// instance is the daemon's own — the CLI, when it needs the rows, goes
// through the socket, never a second store handle (P34).

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/queue"
)

// initQueue fills the Table's queue seams. Nil-safe by construction:
// every handler answers ErrStoreUnavailable when the field is missing,
// so a daemon built without this call degrades loudly, not silently.
func (d *Daemon) initQueue() {
	if d.Table == nil || d.store == nil {
		return
	}
	d.Table.Queue = queue.New(d.store)
	// The design's explicit materialization door (CG4's cold start and
	// every `ask`-mode advance): the SAME advanceNext the reactor chains
	// and the answered-gate calls — one materialization, and this is its
	// explicit third caller. It also carries the skill-mode sizing
	// dispatch (T5): advancing an unsized backlog sizes it.
	d.Table.QueueAdvance = func(ctx context.Context, componentRoot string) (bool, error) {
		if componentRoot == "" {
			return false, fmt.Errorf("an advance names the project whose queue it advances")
		}
		return d.advanceNext(ctx, componentRoot), nil
	}
	// loop.start's OWN loader — not `blueprint.Resolve` alone: measured,
	// Resolve only says where a name WOULD live (it answers nil for a
	// name with no file), and it is `loadStartBlueprint` (Resolve + the
	// read + the parse) that is the validation loop.start actually
	// performs. queue.append calls that same function, so the two doors
	// answer an unknown name with the identical sentence by construction.
	d.Table.QueueValidateBlueprint = func(name string) error {
		_, err := d.loadStartBlueprint(name)
		return err
	}
}
