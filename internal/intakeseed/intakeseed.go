// Package intakeseed ships the goal-intake blueprint as its own inert seed
// set (S61 T9, GI15): `blueprints.intake`, one record, applied through the
// SAME blueprintseed applier machinery landingseed proved ("an applier may
// serve a second set" — R4). It is the row that lets intake.start open a
// goal-intake loop; the row permits nothing — the blueprint has no code
// action, and the plane's writes stay person doors the loop cannot reach.
//
// Deviation from landingseed, recorded on purpose (2026-11-05): the set is
// NOT registered from an init(). The package-global registry is also what
// test engines build against with their own appliers maps, and a set
// declared into the global registry from a distance forces every one of
// those maps to grow (policyseed/decl.go's recorded reason; the toolseed
// pattern is the modern form). Declaration() is mounted by daemon's
// NewSeedEngine rebuild block — the one engine every door holds.
package intakeseed

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's stable name (SD2).
const SetID = "blueprints.intake"

//go:embed blueprints/*.yaml
var shipped embed.FS

// names is the shipped list, in stable order. intake.start names this
// blueprint verbatim; a name added here without a start path is a row no
// code starts, and a name the start path names without a row here is a
// start that fails at the pin.
var names = []string{"goal-intake"}

// recordContent is blueprintseed's canonical envelope, restated here on
// purpose (the landingseed precedent): the two packages may not import
// each other's internals, and the JSON is the contract between them — a
// shape drift is a refused apply, not a silent gap.
type recordContent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Content string `json:"content"`
}

// Source returns one record per shipped blueprint; it panics on a broken
// embed or an unloadable yaml: a set that would ship short is a build-time
// fact, and Records is never nil and incomplete.
func Source() []seed.Record {
	recs := make([]seed.Record, 0, len(names))
	for _, name := range names {
		raw, err := shipped.ReadFile("blueprints/" + name + ".yaml")
		if err != nil {
			panic(fmt.Sprintf("intakeseed: embedded %q did not resolve: %v", name, err))
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			panic(fmt.Sprintf("intakeseed: embedded %q does not load: %v", name, err))
		}
		body, err := json.Marshal(recordContent{Name: name, Version: bp.Version, Content: string(raw)})
		if err != nil {
			panic(fmt.Sprintf("intakeseed: encoding %q: %v", name, err))
		}
		recs = append(recs, seed.Record{Key: name, Version: bp.Version, Content: body})
	}
	return recs
}

// SetVersion is the maximum of the records' versions, computed from Source
// so the two can never disagree.
func SetVersion() int {
	max := 1
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}

// Names is the shipped list (the seeding test's walk).
func Names() []string { return append([]string(nil), names...) }

// Declaration is the set the daemon mounts through NewSeedEngine's rebuild
// block: inert, one record source, the class that ApplyInertAtStart reaches
// and nothing else.
func Declaration() seed.Set {
	return seed.Set{
		ID:      SetID,
		Class:   seed.ClassInert,
		Version: SetVersion(),
		Records: Source,
	}
}
