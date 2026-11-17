package config

// settings_decl_backup_test.go — S62 T10: the ten backup keys are settings,
// not secrets. None of them is credential-shaped, so none is Sensitive
// (CS5) — and a future declaration that flips one is a diff a reviewer has
// to see, not a drift.

import "testing"

// TestNoSecretKeysDeclared (T10): every declared backup.* key is
// Sensitive: false. Sensitive is a guard-widening flag (CS8: a long
// description saying what the default protects, the panel's type-to-confirm
// dance) that a settings value like a retention count or a duration must
// not need — a backup setting that read as a secret would be a credential
// that wandered into the wrong store.
func TestNoSecretKeysDeclared(t *testing.T) {
	seen := 0
	for _, decl := range DefaultRegistry().All() {
		if decl.Area != "backup" {
			continue
		}
		seen++
		if decl.Sensitive {
			t.Errorf("%s is declared Sensitive; none of the ten backup keys is credential-shaped (CS5)", decl.Key)
		}
	}
	if seen != 10 {
		t.Errorf("the registry carries %d backup.* keys, want exactly 10 (BK11's closed set)", seen)
	}
}
