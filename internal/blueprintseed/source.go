package blueprintseed

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's stable name. It is declared here, once (SD2).
const SetID = "blueprints.standard"

// Record is the canonical JSON shape this set's Content carries: the name,
// the YAML's own version, and the embedded YAML bytes VERBATIM under
// "content". The engine hashes this JSON; the applier stores only the
// inner content, so the row holds exactly the bytes the release shipped.
type recordContent struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Content string `json:"content"`
}

// Source returns one record per embedded standard blueprint, in
// StandardNames order (the shipped list T0 settled, two-step included).
// The engine calls it at apply time and the seeder's source reads it; the
// golden test pins that content changes ride a version bump.
func Source() []seed.Record {
	recs := make([]seed.Record, 0, len(blueprint.StandardNames()))
	for _, name := range blueprint.StandardNames() {
		raw, ok, err := blueprint.EmbeddedSource().Get(name)
		if err != nil || !ok {
			// A standard that does not resolve is a build-time fact, not
			// a runtime one: the set must never silently ship short (rule
			// 3: Records is never nil and complete). Panic at the source
			// — the same init-time stance SD2 takes for a broken
			// declaration — so a broken embedded file fails loudly.
			panic(fmt.Sprintf("blueprintseed: embedded %q did not resolve (ok=%v, err=%v)", name, ok, err))
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			panic(fmt.Sprintf("blueprintseed: embedded %q does not load: %v", name, err))
		}
		body, err := json.Marshal(recordContent{Name: name, Version: bp.Version, Content: string(raw)})
		if err != nil {
			panic(fmt.Sprintf("blueprintseed: encoding %q: %v", name, err))
		}
		recs = append(recs, seed.Record{Key: name, Version: bp.Version, Content: body})
	}
	return recs
}

// SetVersion is the set's release number: the maximum of its records'
// versions (rule 3). It is computed from Source() so the two can never
// disagree — the golden test is what forces a human to bump a record's
// YAML version when its content changes.
func SetVersion() int {
	max := 1
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}

// DecodeRecord reads a record's canonical body back. The applier's one
// parse; a foreign shape is a refusal naming the key.
func DecodeRecord(r seed.Record) (recordContent, error) {
	var c recordContent
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return c, fmt.Errorf("blueprintseed: record %q is not canonical JSON: %w", r.Key, err)
	}
	if c.Name != r.Key {
		return c, fmt.Errorf("blueprintseed: record %q names %q inside — the key and the name must agree", r.Key, c.Name)
	}
	if c.Content == "" {
		return c, fmt.Errorf("blueprintseed: record %q carries empty content", r.Key)
	}
	return c, nil
}
