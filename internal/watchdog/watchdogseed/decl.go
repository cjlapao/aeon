package watchdogseed

import (
	"errors"

	"github.com/cjlapao/aeon/internal/seed"
)

// errNoStore is the adapter's refusal until the daemon installs the store: a
// set with no store is a named refusal, not a silent gap (SD2).
var errNoStore = errors.New("watchdogseed: the store is not installed — the daemon installs it at serve")

const (
	// SetID is the watchdog set's id (SD2's dotted grammar).
	SetID = "watchdog.default"
	// setVersion is the set's release number (SD1): it bumps when ANY
	// record's content changes — a rule's threshold is content, so a
	// content change without a version bump is refused by the golden test.
	setVersion = 1
)

// init registers the watchdog set. It is INERT (LC18): a rule only notices —
// a notification, a stop of an attended loop, a block of an unattended one —
// and it never widens what a loop may do, so it applies at daemon start
// without a human in the loop (SD5). The applier holds the store (the one
// place the SQL lives, I2) and takes *sql.Tx, so the set registers against
// an adapter the daemon (T9) fills in at serve, as agentseed's does; a
// broken declaration panics (the init-time stance, SD2).
func init() {
	if err := seed.Register(
		seed.Set{
			ID:      SetID,
			Class:   seed.ClassInert,
			Version: setVersion,
			Records: Source,
		},
		Adapter{}, // a seed.Applier over a *Store the daemon installs; see applier.go
	); err != nil {
		panic("watchdogseed: refusing to register a set: " + err.Error())
	}
}

// Adapter is the seed.Applier the registry holds until the daemon installs
// the store-backed Applier over it; until then every call refuses, so a set
// with no store is a named refusal, not a silent gap.
type Adapter struct{}

func (Adapter) Set() string                                           { return SetID }
func (Adapter) GuardAffecting() bool                                  { return false }
func (Adapter) Operator() seed.OperatorRights                         { return seed.OperatorRights{Edit: true, Remove: true} }
func (Adapter) Fingerprint(_ seed.Tx, _ string) (string, bool, error) { return "", false, errNoStore }
func (Adapter) Adoptable(_ seed.Tx, _ string) (bool, error)           { return false, errNoStore }
func (Adapter) Upsert(_ seed.Tx, _ seed.Record, _ bool) error         { return errNoStore }
func (Adapter) Retire(_ seed.Tx, _ string) error                      { return errNoStore }
