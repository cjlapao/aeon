package daemon

// triage_settings.go — S51 T3 (TM4): the validation blueprint is a
// SETTING, read at the moment of dispatch, and `triage.health` is its
// honest mirror. The old constant (`brainstorm`) now lives ONLY as the
// setting's declared default — so the daemon resolves a missing or unwired
// value to the registry's default (one truth), while a STORED or ENVIRONED
// value that is broken REFUSES the attempt by name rather than silently
// running the default. A fallback at dispatch time would re-create exactly
// what the constant was: an attempt nobody chose. The shape is S50's RM3/
// RM6 twin on purpose — roadmap_settings.go owns the shared row read
// (roadmapBlueprintRowFrom), so the analyzer's verdict is one function and
// the two planes can never disagree about what "invalid" means.

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
)

// The key, spelled once. It is the SAME string the panel shows and
// `settings.set` writes; nothing here can drift from it because the
// registry is the only other place the key is named.
const triageValidationSetting = "triage.validationBlueprint"

// triageSetting reads the validation choice at CALL time — nothing is
// captured at construction; the layering, environment overrides included,
// resolves per call. Unwired means a daemon with no settings mount at all:
// the registry's DEFAULT answers, the same value a fresh store would
// resolve, stated as a fact and not a second truth. Tests replace the seam
// to count calls (the per-validate promise).
func (d *Daemon) triageSetting(ctx context.Context, key string) (config.View, error) {
	if d.triageSettingView != nil {
		return d.triageSettingView(ctx, key)
	}
	if d.settings != nil {
		return d.settings.Get(ctx, key)
	}
	decl, ok := config.DefaultRegistry().Lookup(key)
	if !ok {
		return config.View{}, fmt.Errorf("the registry carries no %q — the slice declaring it did not land", key)
	}
	return config.View{Setting: decl, Value: decl.Default, Source: "default"}, nil
}

// triageBlueprintSource is the source the validation choice is checked
// against: the seam when one is wired, the store rows otherwise (S48 T3 —
// the row the last write left, never the embed). false means the daemon
// cannot see ANY blueprint source (no seam, no store yet): the honest
// answer is then unverifiable, never a borrowed ok (TM4/health).
func (d *Daemon) triageBlueprintSource() (blueprint.Source, bool) {
	if d.bpSrc != nil {
		return d.bpSrc, true
	}
	if d.store != nil {
		return StoreBlueprintSource(d.store), true
	}
	return nil, false
}

// triageBlueprintChoice resolves ONE validation attempt's blueprint: the
// effective name (the setting's value) and its parsed shape. Every refusal
// names the setting and says what to do; none falls back (TM4).
func (d *Daemon) triageBlueprintChoice(ctx context.Context) (string, blueprint.Blueprint, error) {
	view, err := d.triageSetting(ctx, triageValidationSetting)
	if err != nil {
		return "", blueprint.Blueprint{}, fmt.Errorf("%s could not be read — the validation attempt refuses rather than guess a blueprint: %w", triageValidationSetting, err)
	}
	if view.EnvInvalid != "" {
		return "", blueprint.Blueprint{}, fmt.Errorf("%s is set in the environment (%s) to a value that fails its declaration — the attempt refuses rather than run some other value; fix or unset %s (TM4 refuses fallbacks)", triageValidationSetting, view.EnvInvalid, view.EnvInvalid)
	}
	if view.StoredInvalid {
		stored := ""
		if view.Stored != nil {
			stored = *view.Stored
		}
		return "", blueprint.Blueprint{}, fmt.Errorf("%s holds %q, which fails its declaration — the attempt refuses rather than silently use the default; fix it in Settings → Triage (TM4 refuses fallbacks)", triageValidationSetting, stored)
	}
	name := view.Value
	if name == "" {
		return "", blueprint.Blueprint{}, fmt.Errorf("%s is empty — pick one in Settings → Triage", triageValidationSetting)
	}
	src, ok := d.triageBlueprintSource()
	if !ok {
		return "", blueprint.Blueprint{}, fmt.Errorf("%s is %q, which cannot be verified right now — the daemon sees no blueprint source, and the attempt refuses rather than start an unverifiable blueprint (triage.health names the state)", triageValidationSetting, name)
	}
	_, bp, state, _, _, _ := roadmapBlueprintRowFrom(src, name)
	switch state {
	case stateMissing:
		return "", blueprint.Blueprint{}, fmt.Errorf("%s is %q, which does not exist — pick one in Settings → Triage", triageValidationSetting, name)
	case stateInvalid:
		return "", blueprint.Blueprint{}, fmt.Errorf("%s is %q, which does not validate — pick one in Settings → Triage (`aeon blueprint validate %s` says why)", triageValidationSetting, name, name)
	case stateUnverifiable:
		return "", blueprint.Blueprint{}, fmt.Errorf("%s is %q, which cannot be verified right now — the attempt refuses rather than start an unverifiable blueprint (triage.health names the reason)", triageValidationSetting, name)
	}
	return name, bp, nil
}

// triageHealth answers triage.health (TM4's read): the setting's standing,
// not a gate. One entry, always: the EFFECTIVE name and its state right
// now. A settings read that fails answers unverifiable with the declared
// default as the name — the honest answer is "I could not verify", never a
// borrowed ok. A green health line and a refused dispatch can honestly
// stand one second apart, because the dispatch re-checks at its own moment.
func (d *Daemon) triageHealth(ctx context.Context) (any, error) {
	src, ok := d.triageBlueprintSource()
	read := func(ctx context.Context, key string) (config.View, error) { return d.triageSetting(ctx, key) }
	return triageHealthOver(ctx, read, src, ok)
}

// triageHealthOver is the health answer as ONE function over an explicit
// settings read and an explicit blueprint source (S51 T6, the S50
// roadmapHealthOver shape): the daemon's seam and the CLI's direct door
// run the same code — offline the store source is the only source there
// is, and the honest states are the same states.
func triageHealthOver(ctx context.Context, read func(context.Context, string) (config.View, error), src blueprint.Source, srcOK bool) (protocol.TriageHealthResult, error) {
	out := protocol.TriageHealthResult{EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	entry := protocol.TriageHealthSetting{Key: triageValidationSetting, State: stateUnverifiable}
	view, err := read(ctx, triageValidationSetting)
	if err != nil {
		if decl, ok := config.DefaultRegistry().Lookup(triageValidationSetting); ok {
			entry.Name = decl.Default
		}
		out.Settings = append(out.Settings, entry)
		return out, nil
	}
	entry.Name = view.Value
	if view.EnvInvalid != "" || view.StoredInvalid {
		// The setting itself is broken: the NAME reported is what a
		// dispatch would refuse over, not the value the layering falls
		// back to answering.
		if view.StoredInvalid && view.Stored != nil {
			entry.Name = *view.Stored
		} else if view.EnvInvalid != "" {
			entry.Name = "(shadowed by " + view.EnvInvalid + " — invalid)"
		}
		entry.State = stateInvalid
		out.Settings = append(out.Settings, entry)
		return out, nil
	}
	if !srcOK {
		// No settings row and no visible source: the name's own standing
		// is what cannot be verified (S48 unwired is exactly this state).
		out.Settings = append(out.Settings, entry)
		return out, nil
	}
	_, _, state, valid, invalid, unverifiable := roadmapBlueprintRowFrom(src, view.Value)
	entry.State = state
	if state == stateOK || state == stateInvalid || state == stateUnverifiable {
		entry.ReportCounts = &protocol.TriageHealthCounts{Valid: valid, Invalid: invalid, Unverifiable: unverifiable}
	}
	out.Settings = append(out.Settings, entry)
	return out, nil
}
