package backup

// configsettings.go — S62 T10 (BK11): the settings seam over the config
// store. The keys, the defaults and the pre-migration exception do not move
// with the adoption; what moves is the layer in the middle — the value a
// run reads now comes from environment → database → default (S44's CS4
// precedence, read live per call, CS7's no-cache) instead of environment →
// default.
//
// The precedence this implementation answers, top to bottom:
//
//  1. the ESTABLISHED AEON_BACKUP_* variable for the key — it existed
//     before the adoption and operators have exported it, so it keeps its
//     precedence and its voice (set-but-unparseable is "invalid", which
//     refuseInvalid turns into a refused run);
//  2. the S44 layer: AEON_SETTING_BACKUP_* (S44's own env convention for
//     the same key), then the setting-table row, then the declared default.
//     A S44 env var set to a value its declaration rejects is NOT used and
//     falls through to the row / default with S44's resulting source —
//     S44's CS4 semantics, not this seam's refusal voice;
//  3. "unreadable" — the setting table could not be read (a repo fault):
//     the accessor answers its declared default with that source. A run
//     on a known-good default is safer than a run refused over a layer
//     fault, and the status view's settings:[{key,value,source}] line says
//     what happened to a person.
//
// What this seam keeps from EnvSettings' voice: a value that IS set and
// does not parse as its type is "invalid" — the P31 third state, neither
// its value nor its default — and the run is refused over it. That is why
// the duration parse lives HERE and not only in the S44 declaration: the
// durations are declared TypeString (the gates' precedent, so the
// established AEON_BACKUP_* duration strings keep parsing), and a string
// that is not a duration is "set but unparseable", exactly as a bad
// environment value always was.

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/config"
)

type ConfigSettings struct {
	store *config.Store
	env   func(string) string // the established AEON_BACKUP_* layer
}

// NewConfigSettings answers the ten keys through a shared *config.Store.
// A nil store is a wiring bug and is refused: a ConfigSettings with nowhere
// to read would answer only the env and the default and silently forget the
// database layer operators have written.
func NewConfigSettings(store *config.Store) Settings {
	if store == nil {
		panic("backup.NewConfigSettings: store is nil — the database layer would be silently forgotten")
	}
	return &ConfigSettings{store: store, env: os.Getenv}
}

// NewConfigSettingsWith is the test seam: the same store, a supplied lookup
// for the established AEON_BACKUP_* layer.
func NewConfigSettingsWith(store *config.Store, env func(string) string) Settings {
	if store == nil {
		panic("backup.NewConfigSettingsWith: store is nil — the database layer would be silently forgotten")
	}
	return &ConfigSettings{store: store, env: env}
}

var _ Settings = (*ConfigSettings)(nil)

// layer answers the raw value and the layer that supplied it: "env" (either
// env layer), "db" (the row), "default", "invalid" (set but unparseable at
// this seam's type level) or "unreadable" (the table could not be read).
func (c *ConfigSettings) layer(key string) (string, string) {
	// 1. The established env var keeps the top slot and its voice.
	if name, ok := envKey[key]; ok {
		if r := c.env(name); r != "" {
			return r, "env"
		}
	}
	// 2. The S44 layer.
	v, err := c.store.Get(context.Background(), key)
	if err != nil {
		return "", "unreadable"
	}
	switch v.Source {
	case config.SettingEnvironment:
		return v.Value, "env"
	case config.SettingDatabase:
		return v.Value, "db"
	default: // SettingDefault (also the stored-invalid fall-through)
		return v.Value, "default"
	}
}

func (c *ConfigSettings) String(key string) (string, string) {
	if key != KeyDir {
		return "", "invalid"
	}
	raw, src := c.layer(key)
	if src == "unreadable" {
		return "", src
	}
	return raw, src
}

func (c *ConfigSettings) Bool(key string) (bool, string) {
	raw, src := c.layer(key)
	switch src {
	case "unreadable":
		// Both bool keys' defaults are true (the safety net and the
		// verification are on by default); a layer fault answers the
		// default it could not read, named as such.
		return true, src
	case "default":
		switch key {
		case KeyEnabled, KeyVerifyAfter:
			return raw == "true", src
		}
		return false, src
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, "invalid"
	}
	return b, src
}

func (c *ConfigSettings) Duration(key string) (time.Duration, string) {
	raw, src := c.layer(key)
	if src == "unreadable" {
		switch key {
		case KeyInterval:
			return DefaultInterval, src
		case KeyTimeout:
			return DefaultTimeout, src
		case KeyStaleAfter:
			return DefaultStaleAfter, src
		}
		return 0, src
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, "invalid"
	}
	return d, src
}

func (c *ConfigSettings) Int(key string) (int, string) {
	raw, src := c.layer(key)
	if src == "unreadable" {
		switch key {
		case KeyKeep:
			return DefaultKeep, src
		case KeyKeepDaily:
			return DefaultKeepDaily, src
		case KeyKeepWeekly:
			return DefaultKeepWeekly, src
		case KeyKeepPreMig:
			return DefaultKeepPreMig, src
		}
		return 0, src
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, "invalid"
	}
	return n, src
}

func (c *ConfigSettings) Keys() []string { return AllKeys }
