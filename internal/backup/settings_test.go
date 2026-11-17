package backup

// The settings seam: the env implementation, the invalid third state, and
// the refusal set — each refusal names the key and the rule.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSettingsEnvSeam: the env layer over the defaults, read through the
// injected lookup so the process environment is never touched.
func TestSettingsEnvSeam(t *testing.T) {
	env := map[string]string{
		"AEON_BACKUP_ENABLED":    "false",
		"AEON_BACKUP_INTERVAL":   "45m",
		"AEON_BACKUP_KEEP":       "9",
		"AEON_BACKUP_KEEP_DAILY": "14",
	}
	s := NewEnvSettingsWith(func(k string) string { return env[k] })

	if v, src := s.Bool(KeyEnabled); v || src != "env" {
		t.Errorf("enabled: got %v/%s, want false/env", v, src)
	}
	if v, src := s.Duration(KeyInterval); v != 45*time.Minute || src != "env" {
		t.Errorf("interval: got %v/%s, want 45m/env", v, src)
	}
	if v, src := s.Int(KeyKeep); v != 9 || src != "env" {
		t.Errorf("keep: got %v/%s, want 9/env", v, src)
	}
	if v, src := s.Int(KeyKeepDaily); v != 14 || src != "env" {
		t.Errorf("keepDaily: got %v/%s, want 14/env (the camelCase key maps to the SNAKE env)", v, src)
	}
	// Unset keys answer their defaults, named as defaults.
	if v, src := s.Duration(KeyTimeout); v != DefaultTimeout || src != "default" {
		t.Errorf("timeout default: got %v/%s", v, src)
	}
	if v, src := s.Bool(KeyVerifyAfter); !v || src != "default" {
		t.Errorf("verifyAfter default: got %v/%s", v, src)
	}
	if v, src := s.String(KeyDir); v != "" || src != "default" {
		t.Errorf("dir default: got %q/%s", v, src)
	}
	if len(s.Keys()) != len(AllKeys) {
		t.Errorf("Keys() has %d entries, the closed set has %d", len(s.Keys()), len(AllKeys))
	}
}

// TestResolveSettingsRefusals: each refusal names the key and the rule, and
// the invalid third state is refused before any value is used.
func TestResolveSettingsRefusals(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"invalid bool", map[string]string{"AEON_BACKUP_ENABLED": "yes-ish"}, "backup.enabled"},
		{"invalid duration", map[string]string{"AEON_BACKUP_INTERVAL": "sometimes"}, "backup.interval"},
		{"negative duration", map[string]string{"AEON_BACKUP_TIMEOUT": "-5m"}, "backup.timeout"},
		{"invalid int", map[string]string{"AEON_BACKUP_KEEP": "five"}, "backup.keep"},
		{"negative int", map[string]string{"AEON_BACKUP_KEEP_DAILY": "-1"}, "backup.keepDaily"},
		{"keep zero", map[string]string{"AEON_BACKUP_KEEP": "0"}, "backup.keep"},
		{"keepDaily zero", map[string]string{"AEON_BACKUP_KEEP_DAILY": "0"}, "backup.keepDaily"},
		{"keepWeekly zero", map[string]string{"AEON_BACKUP_KEEP_WEEKLY": "0"}, "backup.keepWeekly"},
		{"keepPreMigration zero", map[string]string{"AEON_BACKUP_KEEP_PREMIGRATION": "0"}, "backup.keepPreMigration"},
		{"relative dir", map[string]string{"AEON_BACKUP_DIR": "backups"}, "backup.dir"},
		{"interval below floor", map[string]string{"AEON_BACKUP_INTERVAL": "10m"}, "15m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewEnvSettingsWith(func(k string) string { return c.env[k] })
			_, err := ResolveSettings(s)
			if err == nil {
				t.Fatalf("the %s setting was not refused", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the refusal %q does not name %q — a refusal says the key and the rule", err.Error(), c.want)
			}
		})
	}

	// The valid shape answers the resolved values and their sources.
	env := map[string]string{
		"AEON_BACKUP_DIR":      t.TempDir(),
		"AEON_BACKUP_INTERVAL": "1h",
	}
	s := NewEnvSettingsWith(func(k string) string { return env[k] })
	r, err := ResolveSettings(s)
	if err != nil {
		t.Fatalf("the valid settings: %v", err)
	}
	if r.Dir != env["AEON_BACKUP_DIR"] || r.Sources[KeyDir] != "env" {
		t.Errorf("dir: got %q from %s", r.Dir, r.Sources[KeyDir])
	}
	if r.Interval != time.Hour || r.Sources[KeyInterval] != "env" {
		t.Errorf("interval: got %v from %s", r.Interval, r.Sources[KeyInterval])
	}
	if r.Sources[KeyKeep] != "default" || r.Keep != DefaultKeep {
		t.Errorf("keep default: got %d from %s", r.Keep, r.Sources[KeyKeep])
	}
	p := r.Policy()
	if p.Keep != r.Keep || p.KeepDaily != r.KeepDaily || p.KeepWeekly != r.KeepWeekly || p.KeepPreMig != r.KeepPreMig {
		t.Errorf("Policy() does not carry the resolved keeps: %+v", p)
	}
}

// TestRunRefusesUnknownReasonsAndActors: the run's closed sets are the
// ledger's — a reason or actor that is not one of them is refused before
// any lock is touched and nothing is written. (An empty reason is manual by
// the door's contract, not a refusal; the empty actor is user, the same way.)
func TestRunRefusesUnknownReasonsAndActors(t *testing.T) {
	applied := firstApplied(3)
	for _, reason := range []string{"restore", "post-restore-check", "backup", "manual-typo"} {
		t.Run("reason "+reason, func(t *testing.T) {
			bdir := t.TempDir()
			fs := newFakeStore(applied)
			d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
			_, err := Run(context.Background(), d, Options{Dir: bdir, Reason: reason, ActorKind: "user"})
			if err == nil {
				t.Fatalf("the reason %q was accepted — the closed set is the ledger's", reason)
			}
			if !strings.Contains(err.Error(), "is not a run reason") || !strings.Contains(err.Error(), reason) {
				t.Errorf("the refusal %q does not name the reason it refused", err.Error())
			}
			if len(fs.rowsSnapshot()) != 0 {
				t.Errorf("a run refused before it starts records no row — the ledger counts runs, not caller bugs")
			}
		})
	}
	for _, actor := range []string{"daemon-admin", "api", "agent"} {
		t.Run("actor "+actor, func(t *testing.T) {
			bdir := t.TempDir()
			fs := newFakeStore(applied)
			d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
			_, err := Run(context.Background(), d, Options{Dir: bdir, Reason: "manual", ActorKind: actor})
			if err == nil {
				t.Fatalf("the actor %q was accepted — the closed set is the ledger's", actor)
			}
			if !strings.Contains(err.Error(), "not one of user, daemon, cli") {
				t.Errorf("the refusal %q does not name the closed set", err.Error())
			}
		})
	}
	// The empty forms are the doors' defaults, not refusals.
	bdir := t.TempDir()
	fs := newFakeStore(applied)
	d := fakeDeps(fs, defaultFakeReport(applied), t.TempDir())
	if _, err := Run(context.Background(), d, Options{Dir: bdir}); err != nil {
		t.Errorf("an empty reason and actor default to manual/user: %v", err)
	}
}
