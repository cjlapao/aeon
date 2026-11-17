package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// The ten settings, by their stable keys. T10 (S44) reads the same keys
// from the settings table through config.Reader; the keys, the defaults
// and the pre-migration exception do not move with the adoption — only the
// implementation behind this interface changes.
const (
	KeyEnabled     = "backup.enabled"
	KeyDir         = "backup.dir"
	KeyInterval    = "backup.interval"
	KeyTimeout     = "backup.timeout"
	KeyKeep        = "backup.keep"
	KeyKeepDaily   = "backup.keepDaily"
	KeyKeepWeekly  = "backup.keepWeekly"
	KeyKeepPreMig  = "backup.keepPreMigration"
	KeyVerifyAfter = "backup.verifyAfter"
	KeyStaleAfter  = "backup.staleAfter"
)

// The defaults (OD2/OD3, BK6): a backup is on by default, hourly-or-better
// is the floor for the interval, verification after every run is on by
// default, and the retention windows are the design's numbers.
const (
	DefaultInterval    = 24 * time.Hour
	DefaultTimeout     = 10 * time.Minute
	DefaultKeep        = 5
	DefaultKeepDaily   = 30
	DefaultKeepWeekly  = 12
	DefaultKeepPreMig  = 5
	DefaultVerifyAfter = true
	DefaultStaleAfter  = 48 * time.Hour
	// MinInterval is BK17's sibling floor: an interval below fifteen
	// minutes is a setting that would make the backup run more often than
	// the store can safely be copied, so it is refused, not clamped — a
	// clamped setting is a setting the operator never chose.
	MinInterval = 15 * time.Minute
)

// AllKeys is the closed set, in display order — the status view's
// settings:[{key,value,source}] walks it.
var AllKeys = []string{
	KeyEnabled, KeyDir, KeyInterval, KeyTimeout,
	KeyKeep, KeyKeepDaily, KeyKeepWeekly, KeyKeepPreMig,
	KeyVerifyAfter, KeyStaleAfter,
}

// Settings is the read seam over the ten keys. Each accessor answers the
// value and where it came from — source is one of "env", "db", "default",
// "invalid" (set, but unparseable — the P31 third state: an invalid value
// is neither its value nor its default) or "unreadable" (the database
// layer could not be read; the accessor answers its declared default).
// EnvSettings (pre-S44) answers "env" / "default" / "invalid";
// ConfigSettings (T10) answers the S44 layers — "env" for either env
// convention, "db" for the row, "default" for the declared default —
// through config.Reader's CS4 precedence.
type Settings interface {
	String(key string) (value string, source string)
	Bool(key string) (value bool, source string)
	Duration(key string) (value time.Duration, source string)
	Int(key string) (value int, source string)
	Keys() []string
}

// envKey maps a settings key to its environment variable: AEON_BACKUP_
// plus the key's part after the dot, upper-cased with the dot as an
// underscore (backup.keepDaily is AEON_BACKUP_KEEP_DAILY).
var envKey = map[string]string{
	KeyEnabled:     "AEON_BACKUP_ENABLED",
	KeyDir:         "AEON_BACKUP_DIR",
	KeyInterval:    "AEON_BACKUP_INTERVAL",
	KeyTimeout:     "AEON_BACKUP_TIMEOUT",
	KeyKeep:        "AEON_BACKUP_KEEP",
	KeyKeepDaily:   "AEON_BACKUP_KEEP_DAILY",
	KeyKeepWeekly:  "AEON_BACKUP_KEEP_WEEKLY",
	KeyKeepPreMig:  "AEON_BACKUP_KEEP_PREMIGRATION",
	KeyVerifyAfter: "AEON_BACKUP_VERIFY_AFTER",
	KeyStaleAfter:  "AEON_BACKUP_STALE_AFTER",
}

// EnvSettings is the pre-S44 implementation: the environment layer over
// the defaults. It is the only implementation until T10 lands, and it is
// what the pre-migration hook uses, because the settings table is inside
// the very database being migrated (BK6's bootstrap constraint).
type EnvSettings struct {
	env func(string) string
}

// NewEnvSettings reads the process environment.
func NewEnvSettings() Settings { return EnvSettings{env: os.Getenv} }

// NewEnvSettingsWith reads through the supplied lookup — the test seam, so
// a case can set an AEON_BACKUP_* value without touching the process
// environment.
func NewEnvSettingsWith(env func(string) string) Settings { return EnvSettings{env: env} }

func (e EnvSettings) raw(key string) (string, bool) {
	v, ok := envKey[key]
	if !ok {
		return "", false
	}
	r := e.env(v)
	return r, r != ""
}

func (e EnvSettings) String(key string) (string, string) {
	if r, ok := e.raw(key); ok {
		return r, "env"
	}
	return "", "default"
}

func (e EnvSettings) Bool(key string) (bool, string) {
	if r, ok := e.raw(key); ok {
		b, err := strconv.ParseBool(r)
		if err != nil {
			return false, "invalid"
		}
		return b, "env"
	}
	switch key {
	case KeyEnabled, KeyVerifyAfter:
		return true, "default"
	}
	return false, "default"
}

func (e EnvSettings) Duration(key string) (time.Duration, string) {
	if r, ok := e.raw(key); ok {
		d, err := time.ParseDuration(r)
		if err != nil || d <= 0 {
			return 0, "invalid"
		}
		return d, "env"
	}
	switch key {
	case KeyInterval:
		return DefaultInterval, "default"
	case KeyTimeout:
		return DefaultTimeout, "default"
	case KeyStaleAfter:
		return DefaultStaleAfter, "default"
	}
	return 0, "default"
}

func (e EnvSettings) Int(key string) (int, string) {
	if r, ok := e.raw(key); ok {
		n, err := strconv.Atoi(r)
		if err != nil || n < 0 {
			return 0, "invalid"
		}
		return n, "env"
	}
	switch key {
	case KeyKeep:
		return DefaultKeep, "default"
	case KeyKeepDaily:
		return DefaultKeepDaily, "default"
	case KeyKeepWeekly:
		return DefaultKeepWeekly, "default"
	case KeyKeepPreMig:
		return DefaultKeepPreMig, "default"
	}
	return 0, "default"
}

func (e EnvSettings) Keys() []string { return AllKeys }

// Resolved is a settings set that passed validation: every value the run
// and the status view use, with the source of each.
type Resolved struct {
	Enabled     bool
	Dir         string
	Interval    time.Duration
	Timeout     time.Duration
	Keep        int
	KeepDaily   int
	KeepWeekly  int
	KeepPreMig  int
	VerifyAfter bool
	StaleAfter  time.Duration
	Sources     map[string]string
}

// Policy is the retention shape (retention.go), resolved from the four keep
// settings.
func (r Resolved) Policy() Policy {
	return Policy{Keep: r.Keep, KeepDaily: r.KeepDaily, KeepWeekly: r.KeepWeekly, KeepPreMig: r.KeepPreMig}
}

// ResolveSettings validates a Settings implementation and answers the
// resolved values. It refuses, with a sentence naming the key: an invalid
// source on any key, a dir that is not absolute, a keep below one, and an
// interval below MinInterval. A refusal here is a settings problem, not a
// backup problem — the run records it as refused with error_kind unknown,
// and the sentence is the whole diagnosis.
func ResolveSettings(s Settings) (Resolved, error) {
	if s == nil {
		s = NewEnvSettings()
	}
	out := Resolved{Sources: map[string]string{}}
	for _, k := range s.Keys() {
		out.Sources[k] = sourceOf(s, k)
	}
	if err := refuseInvalid(s); err != nil {
		return Resolved{}, err
	}
	out.Enabled, _ = s.Bool(KeyEnabled)
	out.VerifyAfter, _ = s.Bool(KeyVerifyAfter)
	out.Interval, _ = s.Duration(KeyInterval)
	out.Timeout, _ = s.Duration(KeyTimeout)
	out.StaleAfter, _ = s.Duration(KeyStaleAfter)
	out.Keep, _ = s.Int(KeyKeep)
	out.KeepDaily, _ = s.Int(KeyKeepDaily)
	out.KeepWeekly, _ = s.Int(KeyKeepWeekly)
	out.KeepPreMig, _ = s.Int(KeyKeepPreMig)
	// T10: the absoluteness check no longer keys on the source — it keys on
	// the value. A dir that is SET (from either env layer or the setting
	// table) must be absolute; the unset default is "" and resolves to
	// DefaultDir(stateDir), which is absolute by construction.
	dir, _ := s.String(KeyDir)
	if dir != "" && !filepath.IsAbs(dir) {
		return Resolved{}, fmt.Errorf("refused: backup.dir is %q — the backup dir must be an absolute path, because a relative one would follow whoever's working directory", dir)
	}
	if out.Keep < 1 {
		return Resolved{}, fmt.Errorf("refused: backup.keep is %d — at least one backup must be kept", out.Keep)
	}
	for key, n := range map[string]int{KeyKeepDaily: out.KeepDaily, KeyKeepWeekly: out.KeepWeekly, KeyKeepPreMig: out.KeepPreMig} {
		if n < 1 {
			return Resolved{}, fmt.Errorf("refused: %s is %d — at least one is kept", key, n)
		}
	}
	if out.Interval < MinInterval {
		return Resolved{}, fmt.Errorf("refused: backup.interval is %s — the floor is %s, below which a run could start before the last one's files have settled", out.Interval, MinInterval)
	}
	if out.Timeout <= 0 || out.StaleAfter <= 0 {
		return Resolved{}, fmt.Errorf("refused: backup.timeout and backup.staleAfter must both be positive durations")
	}
	out.Dir = dir
	return out, nil
}

// sourceOf reads the source for every key kind in one voice, so the status
// view and the invalid-refusal share the definition of "where did this
// come from".
func sourceOf(s Settings, k string) string {
	switch k {
	case KeyEnabled, KeyVerifyAfter:
		_, src := s.Bool(k)
		return src
	case KeyDir:
		_, src := s.String(k)
		return src
	case KeyInterval, KeyTimeout, KeyStaleAfter:
		_, src := s.Duration(k)
		return src
	default:
		_, src := s.Int(k)
		return src
	}
}

// refuseInvalid names the first key whose value was set but could not be
// parsed. The sentence says the key and the rule, not the raw value — a
// value the operator typed wrong may be a paste of something private, and
// echoing it is not kindness.
func refuseInvalid(s Settings) error {
	for _, k := range s.Keys() {
		if sourceOf(s, k) == "invalid" {
			return fmt.Errorf("refused: %s is set but cannot be parsed as its type — unset it or fix its value", k)
		}
	}
	return nil
}

// DefaultDir is the setting-free home of the backups: <state dir>/backups.
// It is the answer when backup.dir is unset — the only answer the
// pre-migration hook can use, since at its moment the setting is not yet
// readable (BK6).
func DefaultDir(stateDir string) string { return filepath.Join(stateDir, "backups") }
