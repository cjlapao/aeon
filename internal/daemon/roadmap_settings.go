package daemon

// roadmap_settings.go — S50 T3 (RM3/RM6): the two blueprint choices are
// SETTINGS, read at the moment of dispatch, and health is their honest
// mirror. The old constants (`brainstorm`, the plan host's record name) now
// live ONLY as the settings' declared defaults — so the daemon resolves a
// missing or unwired value to the registry's default (one truth), while a
// STORED or ENVIRONED value that is broken REFUSES the dispatch by name
// rather than silently running the default. A fallback at dispatch time
// would re-create exactly what the constants were: a run nobody chose.

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
)

// The two keys, spelled once. They are the SAME strings the panel shows and
// `settings.set` writes; nothing here can drift from them because the
// registry is the only other place a key is named.
const (
	roadmapIdeationSetting = "roadmap.ideationBlueprint"
	roadmapPlanSetting     = "roadmap.planBlueprint"
)

// roadmapSettingView is the seam through which a dispatch reads its setting:
// initRoadmap wires it to the daemon's ONE shared *config.Store (per-call —
// nothing is captured at construction; the layering, environment overrides
// included, resolves at read time). Unwired means a daemon with no settings
// mount at all: the registry's DEFAULT answers, which is the same value a
// fresh store would resolve, stated as a fact and not a second truth. Tests
// replace the seam to count calls (the per-dispatch promise).
type settingView func(ctx context.Context, key string) (config.View, error)

func (d *Daemon) roadmapSetting(ctx context.Context, key string) (config.View, error) {
	if d.roadmapSettingView != nil {
		return d.roadmapSettingView(ctx, key)
	}
	if d.settings != nil {
		return d.settings.Get(ctx, key)
	}
	decl, ok := config.DefaultRegistry().Lookup(key)
	if !ok {
		return config.View{}, fmt.Errorf("the registry carries no %q — the slice declaring it did not land", key)
	}
	v := config.View{Setting: decl, Value: decl.Default, Source: "default"}
	return v, nil
}

// roadmapBlueprintChoice resolves ONE dispatch's blueprint: the effective
// name (the setting's value — an explicit caller param short-circuits
// BEFORE this, at the call site), the row's bytes, and the parsed shape.
// Every refusal names its setting and says what to do; none falls back.
func (d *Daemon) roadmapBlueprintChoice(ctx context.Context, key string) (string, []byte, blueprint.Blueprint, error) {
	view, err := d.roadmapSetting(ctx, key)
	if err != nil {
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s could not be read — the dispatch refuses rather than guess a blueprint: %w", key, err)
	}
	if view.EnvInvalid != "" {
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s is set in the environment (%s) to a value that fails its declaration — the dispatch refuses rather than run some other value; fix or unset %s (RM3 refuses fallbacks)", key, view.EnvInvalid, view.EnvInvalid)
	}
	if view.StoredInvalid {
		stored := ""
		if view.Stored != nil {
			stored = *view.Stored
		}
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s holds %q, which fails its declaration — the dispatch refuses rather than silently use the default; fix it in Settings → Roadmap (RM3 refuses fallbacks)", key, stored)
	}
	name := view.Value
	if name == "" {
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s is empty — pick one in Settings → Roadmap", key)
	}
	raw, bp, state, _, _, _ := d.roadmapBlueprintRow(name)
	switch state {
	case stateMissing:
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s is %q, which does not exist — pick one in Settings → Roadmap", key, name)
	case stateInvalid:
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s is %q, which does not validate — pick one in Settings → Roadmap (`aeon blueprint validate %s` says why)", key, name, name)
	case stateUnverifiable:
		return "", nil, blueprint.Blueprint{}, fmt.Errorf("%s is %q, which cannot be verified right now — the dispatch refuses rather than start an unverifiable blueprint (RM6's health read names the reason)", key, name)
	}
	return name, raw, bp, nil
}

// The four states RM6's health read answers, spelled once here too.
const (
	stateOK           = "ok"
	stateMissing      = "missing"
	stateInvalid      = "invalid"
	stateUnverifiable = "unverifiable"
)

// roadmapBlueprintRow reads one name through the daemon's OWN source (S48
// T3: the row the last write left, never the embed) and validates it with
// the ONE analyzer. bpSrc is nil only before initBlueprints; a store is
// always there, so the store source is the daemon's source either way —
// "unverifiable" then means the read FAILED, which is its honest use.
// (valid, invalid, unverifiable) ride out with the state so health reports
// the SAME counts its state was decided from — one Analyze per entry, and
// the reportCounts can never disagree with the verdict beside them.
func (d *Daemon) roadmapBlueprintRow(name string) ([]byte, blueprint.Blueprint, string, int, int, int) {
	return roadmapBlueprintRowFrom(d.blueprintSource(), name)
}

// blueprintSource is the daemon's blueprint source: the seam when one is
// wired, the store rows otherwise (S48 T3 — the row the last write left,
// never the embed).
func (d *Daemon) blueprintSource() blueprint.Source {
	if d.bpSrc != nil {
		return d.bpSrc
	}
	return StoreBlueprintSource(d.store)
}

// roadmapBlueprintRowFrom is the ONE row read — the daemon's seam and the
// CLI's offline door (S50 T6) both run this, over whichever source is the
// truth for the moment asking. (valid, invalid, unverifiable) ride out
// with the state exactly as the comment above the daemon's wrapper says.
func roadmapBlueprintRowFrom(src blueprint.Source, name string) ([]byte, blueprint.Blueprint, string, int, int, int) {
	raw, ok, err := src.Get(name)
	if err != nil {
		return nil, blueprint.Blueprint{}, stateUnverifiable, 0, 0, 0
	}
	if !ok {
		return nil, blueprint.Blueprint{}, stateMissing, 0, 0, 0
	}
	bp, err := blueprint.Load(raw)
	if err != nil {
		return raw, blueprint.Blueprint{}, stateInvalid, 0, 0, 0
	}
	report := blueprint.Analyze(raw, blueprintValidationOptions(""))
	valid, invalid, unverifiable := report.Counts()
	switch {
	case invalid > 0:
		return raw, bp, stateInvalid, valid, invalid, unverifiable
	case unverifiable > 0:
		return raw, bp, stateUnverifiable, valid, invalid, unverifiable
	}
	return raw, bp, stateOK, valid, invalid, unverifiable
}

// roadmapHealth answers roadmap.health (RM6). Two entries, always both, in
// declaration order: the EFFECTIVE name and its state right now. A settings
// read that fails answers unverifiable with the declared default as the
// name — the honest answer is "I could not verify", never a borrowed ok.
func (d *Daemon) roadmapHealth(ctx context.Context) (any, error) {
	return roadmapHealthOver(ctx, d.roadmapSetting, d.blueprintSource()), nil
}

// roadmapHealthOver is the ONE health answer (S50 T6's rule-4 shape): the
// daemon's seam and the CLI's offline door run this, over whichever
// setting-view and blueprint-source are the truth for the moment asking.
// RM6's honesty belongs to the function: unverifiable is a real answer,
// never a borrowed ok, and a green line and a refused dispatch can
// honestly stand one second apart.
func roadmapHealthOver(ctx context.Context, read settingView, src blueprint.Source) protocol.RoadmapHealthResult {
	out := protocol.RoadmapHealthResult{EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, key := range []string{roadmapIdeationSetting, roadmapPlanSetting} {
		entry := protocol.RoadmapHealthSetting{Key: key, State: stateUnverifiable}
		view, err := read(ctx, key)
		if err != nil {
			if decl, ok := config.DefaultRegistry().Lookup(key); ok {
				entry.Name = decl.Default
			}
			out.Settings = append(out.Settings, entry)
			continue
		}
		entry.Name = view.Value
		if view.EnvInvalid != "" || view.StoredInvalid {
			// The setting itself is broken: the effective value is the
			// declaration's default and the row/env is shouting — the
			// NAME reported is what a dispatch would refuse over.
			if view.StoredInvalid && view.Stored != nil {
				entry.Name = *view.Stored
			} else if view.EnvInvalid != "" {
				entry.Name = "(shadowed by " + view.EnvInvalid + " — invalid)"
			}
			entry.State = stateInvalid
			out.Settings = append(out.Settings, entry)
			continue
		}
		_, _, state, valid, invalid, unverifiable := roadmapBlueprintRowFrom(src, view.Value)
		entry.State = state
		if state == stateOK || state == stateInvalid || state == stateUnverifiable {
			entry.ReportCounts = &protocol.RoadmapHealthCounts{Valid: valid, Invalid: invalid, Unverifiable: unverifiable}
		}
		out.Settings = append(out.Settings, entry)
	}
	return out
}
