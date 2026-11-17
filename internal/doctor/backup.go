package doctor

// backup.go — S62 T9 (BK15): the doctor's database and backups lines.
//
// The doctor never opens the store (opening it migrates — and a health
// check that trips the pre-migration hook would back up the schema it
// is about to change). Its facts come from touches none of which can
// write:
//
//   - a stat of the store file and its WAL;
//   - the store's own read-only facts (store.ReadOnlyFacts — one
//     mode=ro connection the store owns, I2: no SQL outside
//     internal/store) for the journal mode and the _migrations
//     ledger count;
//   - a scan of the backup dirs, reading each manifest header without
//     extracting the database (backup.OpenHeader).
//
// Every "cannot tell" is a stated state, never a pass (BK17): a store
// file that is not there is a line, not an absence.
//
// The verify's verdict is the LEDGER's, and the ledger is inside the
// store. The daemon-up line therefore carries the daemon's joined
// verdict (its list's per-file token), and the daemon-down line carries
// the newest file's MANIFEST field — the file's own claim, which the
// engine writes "unverified" at publish and never updates (the file is
// immutable once published, BK10), so a daemon-down line reads
// (unverified) until a format that carries the verdict into the file
// arrives. A line that could not read the verdict prints no token at
// all, never a guess for it (P31).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/store"
)

// BytesHuman is the size's voice (the CLI's humanBytes, mirrored here
// because the doctor package may not import cmd — one definition per
// side, the same three states: MiB with one fraction, whole KiB, whole
// bytes).
func BytesHuman(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KiB", n/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// AgeHuman is the age's voice for a person: the coarsest unit that
// still carries the fact, so "40 minutes ago" does not read "2400 s".
// One definition shared by the doctor's four lines and the CLI's
// backup printers (cmd/aeon calls this, never a twin).
func AgeHuman(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}

// Database is the store's own size facts as the doctor sees it — no
// store handle ever opened.
type Database struct {
	Path        string
	Present     bool
	Bytes       int64
	WALBytes    int64
	JournalMode string
	Applied     int
	Pending     int
}

// DatabaseFacts gathers the database line's facts: the stat, and one
// read-only connection (mode=ro) for the journal mode and the
// migrations ledger's count. A missing file is a fact (Present false),
// not an error — the doctor's job is to say the state, and "no store
// yet" is a state.
func DatabaseFacts(stateDir string) (Database, error) {
	out := Database{Path: filepath.Join(stateDir, "state.db")}
	fi, err := os.Stat(out.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	out.Present = true
	out.Bytes = fi.Size()
	if wal, err := os.Stat(out.Path + "-wal"); err == nil {
		out.WALBytes = wal.Size()
	}

	// The doctor's one touch of the store's file is the store's own
	// read-only answer (I2: no SQL outside internal/store — the
	// questions live in store.ReadOnlyFacts, over a mode=ro
	// connection that cannot write and cannot migrate). A file that
	// is there but unreadable (a torn write, a permissions state) is
	// a state: the line says what is known and that the rest could
	// not be read — the honest zeros, not a guess (P31). A file
	// without a _migrations table is a zero count, said by the
	// answer, not an error.
	facts, err := store.ReadOnlyFacts(out.Path)
	if err != nil {
		return out, err
	}
	if !facts.Readable {
		out.JournalMode = "unreadable"
		return out, nil
	}
	out.JournalMode = facts.JournalMode
	out.Applied = facts.Applied
	// The pending count is the registry's total minus what the ledger
	// says applied — the same arithmetic store.Pending() performs, over
	// the registry the doctor is allowed to read (store.Migrations, a
	// pure list).
	total := len(store.Migrations())
	out.Pending = total - out.Applied
	if out.Pending < 0 {
		out.Pending = 0
	}
	return out, nil
}

// Line renders the database line in its fixed template:
//
//	database   <path> · <bytes> + <wal bytes> WAL · journal <mode> · <N> applied / <M> pending
//
// and its fixed sentence for the store-not-created state.
func (d Database) Line() string {
	if !d.Present {
		return fmt.Sprintf("database   no state.db at %s — the store has not been created", d.Path)
	}
	return fmt.Sprintf("database   %s · %s + %s WAL · journal %s · %d applied / %d pending",
		d.Path, BytesHuman(d.Bytes), BytesHuman(d.WALBytes), d.JournalMode, d.Applied, d.Pending)
}

// backupsNeverLine is the one no-success line: the state and the verb
// that would change it. Both sources (the daemon's never state, the
// store-free empty dir) share it — the same state is the same line.
const backupsNeverLine = "backups    never backed up — run `aeon backup run`"

// staleLine is the retention's own answer, in the retention's own
// units: the newest success is older than backup.staleAfter.
func staleLine(age, limit time.Duration) string {
	return fmt.Sprintf("backups    stale: last success %s ago (limit %s)", AgeHuman(age), AgeHuman(limit))
}

// Backups is the backup family's facts the line may claim: the
// configured dir, what it holds, when the newest file is from, the
// newest file's verify token, the retention limit the stale line
// measures against, and the retention's own answer. One type for both
// sources (the daemon's joined verdicts when it answers, the store-free
// scan when it does not); each source fills only what it can read, and
// the token's source is the header comment's argument — the verdict
// lives in the ledger (BK10), and the store-free source reads the
// manifest's own field instead (which the engine writes "unverified" at
// publish and never updates, the file being immutable: the daemon-down
// token is the file's own claim, never a ledger fact the doctor cannot
// read).
type Backups struct {
	Dir          string
	Count        int
	LastAge      time.Duration
	LastVerified bool
	StaleAfter   time.Duration
	// Stale is the retention's answer from the source that may claim
	// it: the machine's stale state (daemon-up) or the measured age
	// against the limit (daemon-down).
	Stale bool
	// PreMigrationDir is set when pre-migration files live in a dir
	// that is not the current backup.dir's pre-migration subdir (the
	// settings moved since the hook wrote them) — the divergence the
	// divergence line names.
	PreMigrationDir string
}

// Never is the no-backups state: the line that says so, and the one
// that names the verb that would change it.
func (b Backups) Never() bool { return b.Count == 0 }

// Lines renders the backups lines in their fixed templates — the main
// line, and the two conditional ones (stale, pre-migration divergence)
// only when their states hold:
//
//	backups    never backed up — run `aeon backup run`
//	backups    last <age> ago (verified|unverified) · <count> kept · <dir>
//	backups    stale: last success <age> ago (limit <staleAfter>)
//	backups    pre-migration dir <path> differs from backup.dir <path>
func (b Backups) Lines() []string {
	if b.Never() {
		return []string{backupsNeverLine}
	}
	token := "unverified"
	if b.LastVerified {
		token = "verified"
	}
	out := []string{
		fmt.Sprintf("backups    last %s ago (%s) · %d kept · %s",
			AgeHuman(b.LastAge), token, b.Count, b.Dir),
	}
	if b.Stale {
		out = append(out, staleLine(b.LastAge, b.StaleAfter))
	}
	expectedPre := filepath.Join(b.Dir, "pre-migration")
	if b.PreMigrationDir != "" && b.PreMigrationDir != expectedPre {
		out = append(out, fmt.Sprintf("backups    pre-migration dir %s differs from backup.dir %s",
			b.PreMigrationDir, b.Dir))
	}
	return out
}

// BackupFacts gathers the store-free backups facts: the settings' dir
// resolution (backup.dir, else the default under the state dir), a
// scan of the dir and its pre-migration subdir, and the newest file's
// manifest header. A dir that was never made is the never state, not
// an error.
func BackupFacts(stateDir string, now time.Time) (Backups, error) {
	resolved, err := backup.ResolveSettings(backup.NewEnvSettings())
	if err != nil {
		return Backups{}, err
	}
	dir := resolved.Dir
	if dir == "" {
		dir = backup.DefaultDir(stateDir)
	}
	out := Backups{Dir: dir, StaleAfter: resolved.StaleAfter}

	count, newest, verified, err := scanBackupDir(dir, now)
	if err != nil {
		return Backups{}, err
	}
	out.Count = count
	out.LastAge = newest
	// The store-free token: the newest file's manifest's own claim
	// (the engine writes "unverified" at publish — the ledger's joined
	// verdict is the daemon's, and this source may not read it).
	out.LastVerified = verified
	out.Stale = out.Count > 0 && out.LastAge > out.StaleAfter

	// The pre-migration divergence: the hook writes to the CURRENT
	// resolution's pre-migration subdir, but files written before a
	// backup.dir change sit in the OLD resolution's. The default
	// resolution's subdir is the only other place the hook could
	// have written — if it holds final names and it is not the
	// current subdir, the line names both paths.
	defaultPre := filepath.Join(backup.DefaultDir(stateDir), "pre-migration")
	if defaultPre != filepath.Join(dir, "pre-migration") {
		if n, _, _, err := scanBackupDir(defaultPre, now); err == nil && n > 0 {
			out.PreMigrationDir = defaultPre
		}
	}
	return out, nil
}

// scanBackupDir counts the final-named backups in one dir and answers
// the newest one's age and its manifest's own verify field. The age is
// against the manifest's created_at (the engine's stamp, not the
// filesystem's mtime — an mtime a copy moves); a header that will not
// parse falls back to the mtime (a file the list shows as a refusal is
// a file the doctor still counts — the count is about what is on disk,
// not what is healthy) and the unverified state (the field could not
// be read, so the line claims nothing).
func scanBackupDir(dir string, now time.Time) (count int, newest time.Duration, lastVerified bool, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, false, nil
		}
		return 0, 0, false, err
	}
	type candidate struct {
		name    string
		created time.Time
	}
	var cs []candidate
	for _, e := range entries {
		if e.IsDir() || !backup.IsFinalName(e.Name()) {
			continue
		}
		count++
		full := filepath.Join(dir, e.Name())
		m, merr := backup.OpenHeader(full)
		if merr != nil {
			fi, serr := os.Stat(full)
			if serr != nil {
				continue
			}
			cs = append(cs, candidate{name: e.Name(), created: fi.ModTime()})
			continue
		}
		cs = append(cs, candidate{name: e.Name(), created: m.Created()})
	}
	if len(cs) == 0 {
		return count, 0, false, nil
	}
	sort.Slice(cs, func(i, j int) bool {
		if !cs[i].created.Equal(cs[j].created) {
			return cs[i].created.After(cs[j].created)
		}
		return cs[i].name > cs[j].name
	})
	newest = now.Sub(cs[0].created)
	if newest < 0 {
		newest = 0
	}
	// The newest file's manifest's own field: "verified" iff the file
	// claims it (the engine writes "unverified" at publish; a file that
	// claims otherwise is newer than this engine, and the line shows
	// what the file says).
	m, merr := backup.OpenHeader(filepath.Join(dir, cs[0].name))
	if merr == nil {
		lastVerified = m.Verified == "verified"
	}
	return count, newest, lastVerified, nil
}
