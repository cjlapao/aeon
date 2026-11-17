// Package landingseed ships S60's two queue blueprints — land-feature and
// resolve-conflict — as the inert seed set blueprints.landing.
//
// A second set, deliberately, not a second list inside blueprints.standard
// (S60 T7's design note): the standard set's release bytes are pinned by
// S48's goldens and its own version line, and the queue's landing shape is
// a different thing being shipped — a set whose bytes change when the
// landing shape changes, without re-releasing the eight dev-loop
// blueprints. The records carry the SAME {name, version, content} envelope
// blueprintseed's applier decodes, so the daemon mounts the standard
// blueprint applier over this set too (blueprintseed.NewApplierFor) — one
// row machinery, two shipped releases, neither able to drift from the
// other's update rules.
//
// Class inert (SD5): a blueprint is a definition, not a permission. These
// two rows change NOTHING a loop may do until the merge-queue reactor — the
// only driver that starts them, and only from a queue item row — starts a
// landing run under the repository's own settings and grants.
package landingseed

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's stable name (SD2).
const SetID = "blueprints.landing"

//go:embed blueprints/*.yaml
var shipped embed.FS

// names is the shipped list, in stable order. The reactor's Decide names
// these two blueprints verbatim (mergequeue.Decision.StartLoop); a name
// added here without a driver is a row that no code starts, and a name the
// driver names without a row here is a start that fails at the pin — the
// TestLandingBlueprintsSeeded pair pins both directions.
var names = []string{"land-feature", "resolve-conflict"}

// recordContent is blueprintseed's canonical envelope, restated here on
// purpose: the two packages may not import each other's internals, and the
// JSON is the contract between them (pinned by the same applier that
// decodes it back — a shape drift is a refused apply, not a silent gap).
type recordContent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Content string `json:"content"`
}

// Source returns one record per shipped blueprint. Like blueprintseed's,
// it panics on a broken embed: a set that would ship short is a build-time
// fact, and rule 3 says Records is never nil and complete.
func Source() []seed.Record {
	recs := make([]seed.Record, 0, len(names))
	for _, name := range names {
		raw, err := shipped.ReadFile("blueprints/" + name + ".yaml")
		if err != nil {
			panic(fmt.Sprintf("landingseed: embedded %q did not resolve: %v", name, err))
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			panic(fmt.Sprintf("landingseed: embedded %q does not load: %v", name, err))
		}
		body, err := json.Marshal(recordContent{Name: name, Version: bp.Version, Content: string(raw)})
		if err != nil {
			panic(fmt.Sprintf("landingseed: encoding %q: %v", name, err))
		}
		recs = append(recs, seed.Record{Key: name, Version: bp.Version, Content: body})
	}
	return recs
}

// SetVersion is the maximum of the records' versions (rule 3), computed
// from Source so the two can never disagree.
func SetVersion() int {
	max := 1
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}

// Names is the shipped list (the seeding test's walk, and the reactor's
// own blueprint names pinned against it).
func Names() []string { return append([]string(nil), names...) }

// errNoStore is the refusing adapter's answer until the daemon mounts the
// store-backed applier (the blueprintseed precedent): a set with no store
// is a named refusal, not a silent gap.
var errNoStore = errors.New("landingseed: the store is not installed — the daemon installs it at serve (S60 T7)")

// init declares the set (SD2, SD12): blueprints.landing, class inert, the
// refusing Adapter until the daemon's NewSeedEngine function-mount swaps
// in the store-backed blueprintseed applier for this SetID. A broken
// declaration panics — the init-time stance.
func init() {
	if err := seed.Register(
		seed.Set{
			ID:      SetID,
			Class:   seed.ClassInert,
			Version: SetVersion(),
			Records: Source,
		},
		Adapter{},
	); err != nil {
		panic("landingseed: refusing to register the set: " + err.Error())
	}
}

// Adapter is the seed.Applier the package-global registry holds from init.
// It refuses every row call until the daemon mounts the store-backed
// applier — the shape every seeded set uses (a domain may not widen the
// global registry from a distance).
type Adapter struct{}

func (Adapter) Set() string                   { return SetID }
func (Adapter) GuardAffecting() bool          { return false }
func (Adapter) Operator() seed.OperatorRights { return seed.OperatorRights{Edit: true, Remove: false} }

func (Adapter) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, errNoStore
}
func (Adapter) Adoptable(seed.Tx, string) (bool, error) { return false, errNoStore }
func (Adapter) Upsert(seed.Tx, seed.Record, bool) error { return errNoStore }
func (Adapter) Retire(seed.Tx, string) error            { return errNoStore }
