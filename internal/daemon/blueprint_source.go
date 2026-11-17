package daemon

import (
	"context"
	"sort"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprint_source.go — S48 T3 (BP2): THE production source. Blueprint
// content comes from the `blueprint` table; nothing here touches a file.
// NO CACHE (N8): a write is visible to the next `loop.start` and the next
// trigger fire — every read goes to the store, exactly as S47 ruled for the
// tool catalog's live reads.
//
// The installed source is ONE per process, installed by the daemon's own
// wiring (serve → New → initBlueprints). `installBlueprintSource` re-arms
// the seam first (UseEmbeddedForOfflineTools un-counts) so a test process
// that builds many daemons always reads through the LAST one built — the
// last daemon is the one that exists. Production gets exactly one install:
// one binary, one store, one daemon (B4 + the endpoint claim). A SECOND
// direct SetSource without re-arm still panics (source.go).

// storeBlueprintSource is the store-backed blueprint.Source.
type storeBlueprintSource struct{ s *store.Store }

// StoreBlueprintSource exposes the row-backed source to a CLI verb that
// opens the store directly (the `aeon blueprint list|validate` path — the
// justified second access path, WAL-serialised behind the daemon's writer,
// the same shape `aeon policy` uses).
func StoreBlueprintSource(s *store.Store) blueprint.Source { return storeBlueprintSource{s} }

// InstallBlueprintSource wires a store as THE source (the daemon's wiring
// and the CLI verbs that open the store directly — the same justified
// second access path `aeon policy` uses). It re-arms first: last installer
// wins, so a test process that builds many daemons reads through the last
// store built; production installs once (one binary, one store, B4).
func InstallBlueprintSource(s *store.Store) {
	blueprint.UseEmbeddedForOfflineTools()
	blueprint.SetSource(StoreBlueprintSource(s))
}

func (x storeBlueprintSource) Get(name string) ([]byte, bool, error) {
	row, ok, err := x.s.BlueprintGet(context.Background(), name)
	if err != nil || !ok {
		return nil, ok, err
	}
	return []byte(row.Content), true, nil
}

// Names keeps the order readers saw before rows existed: the shipped
// standard set in its literal order (present rows only), then the custom
// rows alphabetically — the same shape LegacyNames produced from files.
func (x storeBlueprintSource) Names() ([]string, error) {
	rows, err := x.s.BlueprintList(context.Background(), "", "")
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(rows))
	for _, r := range rows {
		have[r.Name] = true
	}
	var out []string
	for _, n := range blueprint.StandardNames() {
		if have[n] {
			out = append(out, n)
			have[n] = false
		}
	}
	var extra []string
	for n, alive := range have {
		if alive && n != "" {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	return append(out, extra...), nil
}

// Standard answers from the compiled shipped set — the seed set's names
// (T5), which rows cannot tell us (origin is provenance, not shipment).
func (storeBlueprintSource) Standard() []string { return blueprint.StandardNames() }

// SeedStandardBlueprints puts the shipped standards into the table, ONLY
// where the name has no row — never overwriting an operator's row (BP10:
// a custom that happens to wear a shipped name keeps its bytes; the write
// door is human-first, the seed is not). Op `seed`, actor `system`: this
// is the daemon arriving, not a person editing.
//
// T5's seed set formalises this hook (the `blueprints.standard` set and
// its inertness assertions); T3 pulls the hook forward because BP2 with
// no fallback means an unseeded store knows no shipped name, and the
// existing S8–S47 suites must stay green UNEDITED.
func SeedStandardBlueprints(s *store.Store) error {
	ctx := context.Background()
	for _, name := range blueprint.StandardNames() {
		if _, ok, err := s.BlueprintGet(ctx, name); err != nil {
			return err
		} else if ok {
			continue
		}
		raw, found, err := blueprint.EmbeddedSource().Get(name)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		bp, err := blueprint.Load(raw)
		if err != nil {
			return err // a shipped standard that does not load is a build bug, said plain
		}
		content := string(raw)
		_ = bp // the load above is the build-bug gate; SeedSystem re-derives refs
		// S48 T6: the write itself moved to the daemon's one writer
		// (blueprint_write.go) — this file keeps the arrival NET (which
		// names are shipped, which rows already exist), not the door.
		if err := newBlueprintWrites(s).SeedSystem(ctx, name, content); err != nil {
			return err
		}
	}
	return nil
}
