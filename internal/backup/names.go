package backup

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// Name is the setting-free constant every artefact is named after (BK4). No
// user text ever reaches a file name, so a name is safe to parse, quote and
// delete.
const Name = "aeon-state"

// fileGlob is the GLOB the store's backup_run ledger admits on file_name
// (T3's migration). T3's lockstep test keeps the SQL and the Go twin agreed;
// this string is the third voice in the same argument, exported so that test
// and any later SQL change read one definition.
const fileGlob = "aeon-state-????????T??????Z-v*.aeonbak"

// fileRE is the strict twin: a real name's timestamp is digits and its
// schema count is digits, so the lister, the pruner and the fence all match
// exactly the names Run can produce — and nothing else. Every name fileRE
// accepts is a name the GLOB admits (a test holds the lockstep one way the
// SQL cares about: a real artefact must never be refused by the ledger).
var fileRE = regexp.MustCompile(`^aeon-state-\d{8}T\d{6}Z-v\d+\.aeonbak$`)

// IsFinalName is fileRE as a predicate: a file name is a final backup
// (not a partial, not a temp dir, not anything else) iff it matches.
// Exported so that a store-free reader — the doctor's four lines (S62
// T9) — counts exactly the names the lister shows, from one definition.
func IsFinalName(name string) bool { return fileRE.MatchString(name) }

// partialRE matches a tar in flight: the final name with the .partial
// suffix. A partial is never a backup — it is a name the crash sweep looks
// for and the lister ignores.
var partialRE = regexp.MustCompile(`^aeon-state-.*\.aeonbak\.partial$`)

// tmpDirRE matches the build directory's name: .tmp- plus sixteen hex
// characters (randomID). It is a directory name the fence will remove, and
// it is never a final name, so a .tmp-* that outlives its run is garbage by
// definition.
var tmpDirRE = regexp.MustCompile(`^\.tmp-[a-f0-9]{16}$`)

// restoreDirRE matches the restore's stage dir: .restore- plus sixteen hex
// characters, staged INSIDE the state dir (T7, BK9 step 5). It is the fence
// second — and last — directory family (removeRestoreStageDir), and the
// pre-restore dir (state.db.pre-restore-<ts>) matches nothing in this file,
// so no retention pass can ever touch the replaced database.
var restoreDirRE = regexp.MustCompile(`^\.restore-[a-f0-9]{16}$`)

// FileName builds the final name from the creation instant and the applied
// migration count: aeon-state-<UTC yyyymmddThhmmssZ>-v<applied>.aeonbak.
func FileName(t time.Time, applied int) string {
	return fmt.Sprintf("%s-%sZ-v%d.aeonbak", Name, t.UTC().Format("20060102T150405"), applied)
}

// ParseName answers what a final name says: its creation instant (second
// precision, UTC) and its applied-migration count. ok is false for anything
// that is not a well-formed final name — a partial, a look-alike, a path.
func ParseName(name string) (t time.Time, applied int, ok bool) {
	m := fileRE.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, 0, false
	}
	ts, err := time.ParseInLocation("20060102T150405", m[1], time.UTC)
	if err != nil {
		return time.Time{}, 0, false
	}
	n, err := parseInt(m[2])
	if err != nil {
		return time.Time{}, 0, false
	}
	return ts, n, true
}

// randomID is the temp directory's identity: sixteen hex characters from
// crypto/rand, which a crashed run's directory carries and the sweep matches.
// It refuses rather than falls back — a predictable id in a name the fence
// deletes by pattern is a hole in the fence.
func randomID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("backup: cannot mint the temp id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func parseInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a digit: %q", r)
		}
		n = n*10 + int(r-'0')
		if n > 1_000_000 {
			return 0, fmt.Errorf("count out of range: %s", s)
		}
	}
	return n, nil
}
