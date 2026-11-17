package daemon

// continuous_settings.go — S27 T8 (CN21, CN9, CN11, CN12, CN18): the four
// numbers the scheduler spends, read the S44 way. Declared in
// internal/config/settings_decl_scheduler.go, reachable only through
// config (Reader.Int for the number the plan spends, Get/View for the
// layer the panel shows), written only through `settings.set` on the human
// door (CS9). There is no second mechanism and no cache: the worker's Load
// runs once per tick and once per answer (CS7), so a change lands on the
// NEXT tick — and never disturbs a loop already running, because these
// numbers are spent at a start, not held by one.
//
// The source words carry the truth S59 had to leave two-valued: S44's
// Get/View DOES report its layer, so the four-key answer ships three real
// states — `setting` (a row decides), `environment` (an env var decides,
// and a shadowed row says so through ShadowedBy, untouched), and
// `declared-default` (nothing set; the declaration's default runs). The
// fourth honest word, `unreadable-default`, is the daemon with no config
// store on the path or a row whose value cannot mean an int: the declared
// default still runs and the view SAYS that is what is happening, rather
// than letting a number read as a decision somebody made.
//
// None of the four turns the scheduler on. Authority is scheduler_state and
// scheduler_root, written by the eight human-door controls and nothing else
// (CN2); TestNoAuthorityKeyInTheRegistry in internal/config refuses a
// key-shaped authority, and this file would refuse to read one.

import (
	"context"
	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/protocol"
	"strconv"
)

// The four declared keys (settings_decl_scheduler.go). Named once each —
// a key typed twice is a key that drifts.
const (
	schedulerMaxSlotsSetting  = "scheduler.maxAgentSlots"
	schedulerTickSetting      = "scheduler.tickSeconds"
	schedulerMaxStartsSetting = "scheduler.maxStartsPerRootPerHour"
	schedulerAgingSetting     = "scheduler.agingHours"
)

// The declared defaults, mirrored from the declarations. A fallback that
// itself reads a setting is a circle, so these are constants;
// TestSchedulerDeclaredDefaultsMatchTheDeclarations pins the two against
// each other so the mirror cannot drift. (The pre-T8 stopgap ran MaxSlots
// at 4 before the key was declared; the DECLARED default is 2 and from T8
// the declaration is what runs.)
const (
	declaredMaxSlotsDefault  = 2
	declaredTickDefault      = 15
	declaredMaxStartsDefault = 6
	declaredAgingDefault     = 24
)

// The source words the status view answers — S44's View layers, rendered
// in the plane's words (the view has always spoken `declared-default`, and
// CN21 says the panel speaks the daemon's own words, not the framework's).
const (
	settingFromRow      = "setting"
	settingFromEnv      = "environment"
	settingFromDeclared = "declared-default"
	settingUnreadable   = "unreadable-default"
)

// schedulerSettingKeys lists the four in panel order — the order T7's
// status view already answers them in.
var schedulerSettingKeys = []string{
	schedulerMaxSlotsSetting, schedulerTickSetting,
	schedulerMaxStartsSetting, schedulerAgingSetting,
}

// liveSettings reads the four numbers FRESH — every call hits the store's
// resolution (environment → row → default). No value is kept across a
// tick (CS7). A daemon with no config store runs the declared defaults; so
// does one whose read or parse fails, because a scheduler that refuses to
// plan when it cannot read a knob would punish the loops that only wanted
// to be planned.
func (a *continuousAdapter) liveSettings(ctx context.Context) continuous.Settings {
	st := continuous.Settings{
		MaxSlots:                declaredMaxSlotsDefault,
		MaxStartsPerRootPerHour: declaredMaxStartsDefault,
		AgingHours:              declaredAgingDefault,
		TickSeconds:             declaredTickDefault,
	}
	r := a.d.settings
	if r == nil {
		return st
	}
	if v, err := r.Int(ctx, schedulerMaxSlotsSetting); err == nil && v > 0 {
		st.MaxSlots = v
	}
	if v, err := r.Int(ctx, schedulerTickSetting); err == nil && v > 0 {
		st.TickSeconds = v
	}
	if v, err := r.Int(ctx, schedulerMaxStartsSetting); err == nil && v > 0 {
		st.MaxStartsPerRootPerHour = v
	}
	if v, err := r.Int(ctx, schedulerAgingSetting); err == nil && v > 0 {
		st.AgingHours = v
	}
	return st
}

// schedulerSettingsAnswer lists the four settings with the layer each one
// actually came from — the Get/View walk that makes the third state real.
// The VALUES ride the same resolution liveSettings spends (one read of one
// store per key), so the panel can never show a number the plan is not
// using; the WORDS ride the layer the View reports.
func (d *Daemon) schedulerSettingsAnswer(ctx context.Context) []protocol.ContinuousSetting {
	out := make([]protocol.ContinuousSetting, 0, len(schedulerSettingKeys))
	for _, key := range schedulerSettingKeys {
		cs := protocol.ContinuousSetting{Key: key}
		if d.settings == nil {
			cs.Value = declaredValue(key)
			cs.Source = settingUnreadable
			out = append(out, cs)
			continue
		}
		v, err := d.settings.Get(ctx, key)
		if err != nil {
			// The key is not declared or cannot resolve: the declared
			// default runs and the answer says it is unreadable, never
			// silently "default".
			cs.Value = declaredValue(key)
			cs.Source = settingUnreadable
			out = append(out, cs)
			continue
		}
		cs.Value = v.Value
		if v.StoredInvalid || v.EnvInvalid != "" {
			cs.Source = settingUnreadable
			cs.Value = declaredValue(key)
		} else {
			switch v.Source {
			case "database":
				cs.Source = settingFromRow
			case "environment":
				cs.Source = settingFromEnv
			default:
				cs.Source = settingFromDeclared
			}
		}
		out = append(out, cs)
	}
	return out
}

// declaredValue mirrors the four declarations' Default, in the same
// canonical text form the View answers.
func declaredValue(key string) string {
	switch key {
	case schedulerMaxSlotsSetting:
		return strconv.Itoa(declaredMaxSlotsDefault)
	case schedulerTickSetting:
		return strconv.Itoa(declaredTickDefault)
	case schedulerMaxStartsSetting:
		return strconv.Itoa(declaredMaxStartsDefault)
	case schedulerAgingSetting:
		return strconv.Itoa(declaredAgingDefault)
	}
	return ""
}

// settingsTickSeconds renders the cadence for the status view's next-tick
// arithmetic, kept as a helper so T7's NextTickAt line reads unchanged.
func settingsTickSeconds(st continuous.Settings) float64 {
	return st.TickEvery().Seconds()
}
