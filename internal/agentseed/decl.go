package agentseed

import (
	"errors"

	"github.com/cjlapao/aeon/internal/seed"
)

// errNoStore is the adapter's refusal until the daemon (T6) installs the
// store: a set with no store is a named refusal, not a silent gap.
var errNoStore = errors.New("agentseed: the store is not installed — the daemon (T6) installs it at serve")

// init registers the agents set. The applier holds the store (the one place
// the SQL lives, SD1) and takes *sql.Tx, so it is not a seed.Applier
// directly: the daemon (T6) supplies the store and adapts it to the
// seed.Applier the registry needs. Here the set is registered against an
// adapter that the daemon fills in at serve; the set's class and version are
// fixed, and a broken declaration panics (the init-time stance, SD2).
//
// Note (S43-extra T4): the task's "Applier over *sql.Tx" and the store's
// TestNoSQLLivesOutsideTheStore guard cannot both hold — a seed.Applier
// method takes a seed.Tx, but the store's row methods take *sql.Tx and a
// seed.Tx cannot reach them. The SQL therefore lives in internal/store, the
// applier holds the *Store and takes *sql.Tx, and the daemon adapts. The set
// registers against that adapter.
func init() {
	if err := seed.Register(
		seed.Set{
			ID:      SetID,
			Class:   seed.ClassInert,
			Version: setVersion,
			Records: Source,
		},
		Adapter{}, // a seed.Applier over a *Store the daemon installs; see T6
	); err != nil {
		panic("agentseed: refusing to register a set: " + err.Error())
	}
}

// Adapter is the seed.Applier the registry holds: it adapts the store-
// backed Applier (which takes *sql.Tx) to the seed.Applier seam (which
// takes seed.Tx). The daemon (T6) installs the store at serve; until then
// the adapter refuses, so a set with no store is a named refusal, not a
// silent gap.
type Adapter struct{}

func (Adapter) Set() string        { return SetID }
func (Adapter) GuardAffecting() bool { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: true, Remove: true} }
func (Adapter) Fingerprint(_ seed.Tx, _ string) (string, bool, error) {
	return "", false, errNoStore
}
func (Adapter) Adoptable(_ seed.Tx, _ string) (bool, error) { return false, errNoStore }
func (Adapter) Upsert(_ seed.Tx, _ seed.Record, _ bool) error { return errNoStore }
func (Adapter) Retire(_ seed.Tx, _ string) error              { return errNoStore }
