package daemon

// W2-2 (S59 T12, found by walking a real daemon). The terminal teardown hook is
// the plane's promise that a loop which finished done or aborted does not keep
// a container running (EV12). It read the ledger with `ProvisionsFor("", id)`
// — an empty component root and the loop's HEX ID against a query keyed on
// (component_root, loop_slug) — so it always found zero rows and returned
// silently, and a cancelled loop's container stayed Up with no log line.
//
// This test drives the read half of that hook through the door the event
// actually carries: a hex loop id. It fails against the old two-argument call.

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

func TestTerminalTeardownFindsTheFinishedLoopsProvisions(t *testing.T) {
	f := newEnvFixture(t)
	env := f.containerEnv(t)

	loop, err := f.store.CreateLoop(store.NewLoop{
		Slug: "s59-teardown", ComponentRoot: "/tmp/builder", Goal: "hold a container",
	})
	if err != nil {
		t.Fatalf("create the loop the ledger is keyed to: %v", err)
	}
	ready := f.provision(t, env.ID, "container", "c267d7a7ccfaa745658beade32abfc8a54373c2d9a25dfbf0426f827850f9d72")
	if ready.State != "ready" || ready.Handle == "" {
		t.Fatalf("the fixture's row is not a teardown's shape: state=%q handle=%q", ready.State, ready.Handle)
	}

	d := &Daemon{store: f.store}
	rows, err := d.terminalProvisions(loop.ID)
	if err != nil {
		t.Fatalf("the ledger read refused: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the finished loop's teardown found %d provisions, want 1 — %d rows means it is looking up the ledger by "+
			"something other than the loop slug (W2-2: the old read passed an empty root and the hex id, matched nothing, and the hook went silent)",
			len(rows), len(rows))
	}
	if rows[0].ID != ready.ID || rows[0].Backend != "container" {
		t.Errorf("it found the wrong row: %s/%s, want %s/container", rows[0].ID, rows[0].Backend, ready.ID)
	}

	// A loop with nothing to remove is answered, not swallowed: the row list is
	// empty and the read says so (see the comment in terminalProvisions). The
	// distinction between "never provisioned" and "cannot find them" is the whole
	// reason this defect survived two slices.
	other, err := f.store.CreateLoop(store.NewLoop{Slug: "s59-terminal-empty", ComponentRoot: "/tmp/other", Goal: "no container"})
	if err != nil {
		t.Fatalf("create the second loop: %v", err)
	}
	empty, err := d.terminalProvisions(other.ID)
	if err != nil || len(empty) != 0 {
		t.Errorf("a loop with no provisions answered %d rows, err %v — want none and no error", len(empty), err)
	}

	// An id that is not a loop must NOT read as "nothing to remove": the hook's
	// job includes refusing to look confident about a loop it cannot name.
	if _, err := d.terminalProvisions("00000000000000000000000000000000"); err == nil {
		t.Errorf("an unnameable loop id answered no error — silence is what hid W2-2")
	}
}
