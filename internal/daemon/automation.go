package daemon

// automation.go — the dispatcher's door into the daemon (S40 T6). Like every
// unwired seam in daemon.go, a daemon without a store (or without the loop
// runner the starter needs) answers honestly that there is nothing to
// dispatch, rather than ticking on nothing.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/automation"
	"github.com/cjlapao/aeon/internal/store"
)

// RunAutomation blocks until ctx ends. It is serve.go's goroutine body:
// reconcile once, drain on the tick, and a stopped dispatcher is a log line,
// never a dead daemon (the errors that reach here already survived the
// Infof discipline inside Run; anything left is a caller bug worth naming).
func (d *Daemon) RunAutomation(ctx context.Context) error {
	if d.store == nil || d.loopRunner == nil {
		return nil // the honest absence — no rows, no starter, nothing to dispatch
	}
	if err := d.newDispatcher().Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// newDispatcher is the ONLY place the automation package meets the daemon:
// the store, T4's event starter, the S38 component read as root resolver,
// and time.Now as an INJECTED value (the package's grep pins its own single
// defaultClock; the daemon handing time in is not that read).
func (d *Daemon) newDispatcher() *automation.Dispatcher {
	return &automation.Dispatcher{
		Store:       d.store,
		Starter:     d.eventStarter(),
		ResolveRoot: d.componentRoot,
		Clock:       time.Now,
		Log:         d.log,
		PollEvery:   time.Second,
	}
}

// componentRoot resolves a component id to its repo root through S38's read.
// Measured: ReadComponent does NOT map no-rows to ErrNotFound (it wraps the
// bare sql.ErrNoRows), so both spellings become one sentence — a nil
// ResolveRoot in the dispatcher refuses component-scoped triggers as
// workspace-invalid, and this is where the reason gets its name.
func (d *Daemon) componentRoot(componentID string) (string, error) {
	c, err := d.store.ReadComponent(componentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("component %s does not exist", componentID)
		}
		return "", err
	}
	if c.Root == "" {
		return "", fmt.Errorf("component %s has no primary root", componentID)
	}
	return c.Root, nil
}
