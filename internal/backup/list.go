package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// RunReader is the ledger read the list and status views join through.
// *store.Store satisfies it; nil means "no ledger in this process" and
// every verify state is answered unverified — an honest third value, not a
// guess.
type RunReader interface {
	ListBackupRuns(f store.BackupRunFilter) ([]store.BackupRun, error)
}

// BackupView is one file as the list shows it: the manifest's facts, the
// size, and the latest verify verdict joined from the ledger. Paths stay
// out of the protocol view (T6 strips absolute paths from the socket);
// this internal view carries the dir because the pruner needs it.
type BackupView struct {
	FileName      string
	Dir           string
	CreatedAt     time.Time
	Reason        string
	AeonVersion   string
	Applied       int
	LastMigration string
	SizeBytes     int64
	SHA256        string
	// Verified is the joined verdict: "verified" when the ledger's latest
	// verify row for this file is a level-2-or-3 success, "unverified"
	// otherwise — including "no row at all", which is a state, not a
	// failure.
	Verified string
	// CanRestoreWithThisBinary is the newer-schema answer: false for a
	// backup a newer binary made.
	CanRestoreWithThisBinary bool
}

// List scans the given directories for final-named backups (partials,
// temp dirs, the lock and anything else are not backups and are not
// shown), reads each manifest header without extracting the database, and
// joins the latest verify row from the ledger. Newest first.
func List(dirs []string, runs RunReader) ([]BackupView, error) {
	latest := latestVerifyByFile(runs)
	var out []BackupView
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // a dir that was never made holds no backups
			}
			return nil, fmt.Errorf("backup: read the backup dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() || !fileRE.MatchString(e.Name()) {
				continue
			}
			m, merr := OpenHeader(filepath.Join(dir, e.Name()))
			if merr != nil {
				// A final-named file whose manifest will not parse is a
				// fact the list must show, not a reason the list cannot
				// show the rest: the view carries the refusal.
				out = append(out, BackupView{FileName: e.Name(), Dir: dir, Verified: "unverified"})
				continue
			}
			fi, _ := os.Stat(filepath.Join(dir, e.Name()))
			var size int64
			if fi != nil {
				size = fi.Size()
			}
			v := "unverified"
			if latest[e.Name()] {
				v = "verified"
			}
			newer, _ := newerSchema(m)
			out = append(out, BackupView{
				FileName:                 e.Name(),
				Dir:                      dir,
				CreatedAt:                m.Created(),
				Reason:                   m.Reason,
				AeonVersion:              m.AeonVersion,
				Applied:                  m.AppliedMigrations,
				LastMigration:            m.LastMigration,
				SizeBytes:                size,
				SHA256:                   m.DBSHA256,
				Verified:                 v,
				CanRestoreWithThisBinary: !newer,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].FileName > out[j].FileName
	})
	return out, nil
}

// latestVerifyByFile answers, per file name, whether the NEWEST verify row
// for it is a success at a restore level. A failed verify does not verify;
// an older success behind a newer failure is not the ledger's latest fact
// and loses — the list shows what the newest attempt says, and saying
// "verified" about a file whose newest attempt failed would be the lie the
// ledger exists to prevent.
func latestVerifyByFile(runs RunReader) map[string]bool {
	out := map[string]bool{}
	if runs == nil {
		return out
	}
	rows, err := runs.ListBackupRuns(store.BackupRunFilter{Kind: "verify", Limit: 500})
	if err != nil {
		return out // a ledger the process cannot read answers "no rows"
	}
	decided := map[string]bool{}
	for _, row := range rows { // newest first
		if row.FileName == nil {
			continue
		}
		name := *row.FileName
		if decided[name] {
			continue // this file's newest row already decided it
		}
		decided[name] = true
		if row.Outcome == "ok" && (strv(row.VerifyLevel) == "verified" || strv(row.VerifyLevel) == "drill") {
			out[name] = true
		}
	}
	return out
}

// StateStatus is the status machine's closed set (BK12):
//
//	never   — no successful backup row exists
//	fresh   — the last success is within backup.staleAfter
//	stale   — older than that, but nothing is failing
//	failing — the two most recent scheduled runs both failed
//
// "failing" outranks "stale": a backup that is both old and broken is
// told as broken, because the fix is different.
const (
	StateNever   = "never"
	StateFresh   = "fresh"
	StateStale   = "stale"
	StateFailing = "failing"
)

// StatusView is the backup.status facts (the protocol view T6 reshapes):
// the machine's state and the numbers behind it.
type StatusView struct {
	State         string
	Enabled       bool
	Dir           string
	LastSuccessAt string            // ISO-8601 UTC, "" when never
	AgeSeconds    int64             // since the last success; 0 when never
	LastVerifyAt  string            // ISO-8601 UTC of the newest verify row, any file
	NextDueAt     string            // last success + interval, when enabled; "" otherwise
	Running       bool              // a live lock is held on the resolved dir
	Settings      map[string]string // key -> value,source (the ten keys)
}

// Status answers the machine: it reads the ledger, the settings and the
// lock, and says which of the four states the backup is in and why.
func Status(d Deps, runs RunReader, now time.Time) (StatusView, error) {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	resolved, err := ResolveSettings(d.Settings)
	if err != nil {
		return StatusView{}, err
	}
	dir := ""
	if resolved.Dir != "" {
		dir = resolved.Dir
	} else if d.StateDir != "" {
		dir = DefaultDir(d.StateDir)
	}
	view := StatusView{Enabled: resolved.Enabled, Dir: dir, Settings: map[string]string{}}
	for _, k := range AllKeys {
		view.Settings[k] = valueSource(d.Settings, k)
	}

	backups, err := listRuns(runs, store.BackupRunFilter{Kind: "backup", Limit: 200})
	if err != nil {
		return view, err
	}
	var lastSuccess *store.BackupRun
	for i := range backups {
		if backups[i].Outcome == "ok" {
			lastSuccess = &backups[i]
			break // newest first
		}
	}
	if lastSuccess == nil {
		view.State = StateNever
		return view, nil
	}
	lastAt := parseRunStamp(lastSuccess.FinishedAt)
	view.LastSuccessAt = lastSuccess.FinishedAt
	if !lastAt.IsZero() {
		view.AgeSeconds = int64(now.Sub(lastAt).Seconds())
	}
	// The failing test: the two newest scheduled runs, both failed.
	var scheduled []store.BackupRun
	for i := range backups {
		if backups[i].Reason == "scheduled" {
			scheduled = append(scheduled, backups[i])
			if len(scheduled) == 2 {
				break
			}
		}
	}
	if len(scheduled) == 2 && scheduled[0].Outcome != "ok" && scheduled[1].Outcome != "ok" {
		view.State = StateFailing
		return view, nil
	}
	if !lastAt.IsZero() && now.Sub(lastAt) > resolved.StaleAfter {
		view.State = StateStale
	} else {
		view.State = StateFresh
	}
	if resolved.Enabled && !lastAt.IsZero() {
		view.NextDueAt = lastAt.Add(resolved.Interval).UTC().Format(createdFormat)
	}

	verifies, verr := listRuns(runs, store.BackupRunFilter{Kind: "verify", Limit: 1})
	if verr == nil && len(verifies) > 0 {
		view.LastVerifyAt = verifies[0].FinishedAt
	}

	view.Running = lockHeldNow(dir, now)
	return view, nil
}

// strv reads a ledger pointer column: "" for NULL.
func strv(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func listRuns(runs RunReader, f store.BackupRunFilter) ([]store.BackupRun, error) {
	if runs == nil {
		return nil, nil
	}
	rows, err := runs.ListBackupRuns(f)
	if err != nil {
		// "no such table" is the normal state of a store that predates the
		// ledger — or a door that opened one SkipMigrations on a fresh
		// machine (the direct backup door on its first run). Such a ledger
		// holds no rows: the state is "never", not an error. The same
		// reading the store package applies to its own optional tables.
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: read the ledger: %w", err)
	}
	return rows, nil
}

// valueSource is the settings map's one voice: value, comma, source.
func valueSource(s Settings, k string) string {
	var v any
	switch k {
	case KeyEnabled, KeyVerifyAfter:
		v, _ = s.Bool(k)
	case KeyDir:
		v, _ = s.String(k)
	case KeyInterval, KeyTimeout, KeyStaleAfter:
		v, _ = s.Duration(k)
	default:
		v, _ = s.Int(k)
	}
	return fmt.Sprintf("%v,%s", v, sourceOf(s, k))
}

// lockHeldNow answers whether the dir's lock exists and is not stale — the
// status view's "running" fact, read the way the run's refusal would read
// it: a fresh live holder is running; anything older is a corpse the next
// run will reclaim, and saying "running" about a corpse is a lie.
func lockHeldNow(dir string, now time.Time) bool {
	if dir == "" {
		return false
	}
	h, err := readLockHolder(filepath.Join(dir, lockFileName))
	if err != nil {
		return false
	}
	if !pidAlive(h.PID) {
		return false
	}
	return now.Sub(parseStamp(h.Start)) < lockStaleAfter
}

func parseRunStamp(s string) time.Time {
	t, err := time.Parse(createdFormat, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
