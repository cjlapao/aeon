package backup

// S62 T7 · the restore path's shape guards: source-level facts that a
// behaviour test cannot see.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestTheRestorePathNeverClaimsTheEndpoint: the restore path — the
// engine, the device seam, and the CLI door — talks to the daemon only
// through the probe. A claim on the restore path would make a restore
// that was refused under a live daemon the daemon's own next boot, which
// is a different act than the one the operator typed.
func TestTheRestorePathNeverClaimsTheEndpoint(t *testing.T) {
	files := map[string][]string{
		".":                                      {"restore.go", "restore_fs.go", "restore_device_linux.go", "restore_device_darwin.go", "restore_device_other.go"},
		filepath.Join("..", "..", "cmd", "aeon"): {"backup_restore.go", "backup_restore_log.go"},
	}
	for dir, names := range files {
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				t.Fatalf("read %s/%s: %v", dir, n, err)
			}
			if strings.Contains(string(b), "Claim(") {
				t.Errorf("%s: the restore path claims the endpoint — the probe is the only word it has with the daemon", n)
			}
		}
	}
}

// TestThePreRestoreNameMatchesNoRetentionFamily: the replaced database
// moves to state.db.pre-restore-<utc>Z, and no retention family in this
// package may match that shape — the replaced database is never pruned,
// by construction. The sample walks 8 days of clock instants, the whole
// range the name's clock can carry.
func TestThePreRestoreNameMatchesNoRetentionFamily(t *testing.T) {
	families := []*regexp.Regexp{fileRE, partialRE, tmpDirRE, restoreDirRE}
	base := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	checked := 0
	for i := 0; i < 8*24*3600; i += 3 {
		name := "state.db.pre-restore-" + base.Add(time.Duration(i)*time.Second).UTC().Format("20060102T150405") + "Z"
		for _, re := range families {
			if re.MatchString(name) {
				t.Fatalf("retention family %s matches the pre-restore dir name %s — the replaced database would be pruned", re, name)
			}
		}
		checked++
	}
	if checked < 20000 {
		t.Fatalf("the sample is too thin to be a proof: %d names", checked)
	}
}
