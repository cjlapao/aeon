package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// lockFileName is the single-flight marker: one per backup directory,
// O_EXCL so two processes cannot both claim it, 0600 because it names a
// live process and its business.
const lockFileName = ".backup.lock"

// lockStaleAfter is OD6's half of the stale rule: a lock older than this is
// reclaimable even if its pid still looks alive — a daemon that stopped
// releasing without dying (a hang, a killed -STOP) must not hold a backup
// forever, and six hours is far longer than any run the interval allows.
const lockStaleAfter = 6 * time.Hour

// lockHolder is the file's content: who holds the lock, since when, and why
// — the second process's refusal sentence reads all three back.
type lockHolder struct {
	PID    int    `json:"pid"`
	Start  string `json:"start"`
	Reason string `json:"reason"`
}

// LockHeldError is a refused claim: the lock is held, by whom, since when,
// for what. It is a result, not a fault — the other run is doing its job.
type LockHeldError struct {
	Holder lockHolder
	Self   bool // true when the holder is this process (a re-entrant call)
}

func (e *LockHeldError) Error() string {
	if e.Self {
		return "refused: an earlier call in this process still holds the backup lock"
	}
	return fmt.Sprintf("refused: another process holds the backup lock (pid %d, since %s, reason %s); it releases when its run finishes", e.Holder.PID, e.Holder.Start, e.Holder.Reason)
}

// Lock is a held claim. Release is idempotent: it deletes the file only if
// it still names this holder, so a reclaimed-then-reused file belonging to
// the next holder is never deleted by the old one.
type Lock struct {
	dir       string
	pid       int
	start     time.Time
	reclaimed bool
}

// Dir is where the lock lives, for the run's reporting.
func (l *Lock) Dir() string { return l.dir }

// Reclaimed reports whether this acquisition took the lock off a stale
// holder — the reclaim is a fact the run's sentence and event carry.
func (l *Lock) Reclaimed() bool { return l.reclaimed }

// holderStart is the instant a lock was taken, stamped by the taker.
func (l *Lock) Start() time.Time { return l.start }

// heldByProcess tracks the in-process claim: the file's O_EXCL protects
// across processes, and this map protects within one — a re-entrant Run in
// the same process (a pre-migration hook inside a run, a test) must see its
// own lock as held, not as stale, and reclaim it.
var (
	holdMu  sync.Mutex
	heldDir = map[string]int{} // backup dir -> active claims in this process
)

func holdEnter(dir string) bool {
	holdMu.Lock()
	defer holdMu.Unlock()
	if heldDir[dir] > 0 {
		return false
	}
	heldDir[dir] = 1
	return true
}

func holdExit(dir string) {
	holdMu.Lock()
	defer holdMu.Unlock()
	heldDir[dir]--
	if heldDir[dir] <= 0 {
		delete(heldDir, dir)
	}
}

func holdingHere(dir string) bool {
	holdMu.Lock()
	defer holdMu.Unlock()
	return heldDir[dir] > 0
}

// Acquire claims the directory's lock. It refuses with *LockHeldError when
// a live, fresh holder is present (the sentence names the holder); it
// reclaims — and says so on the returned Lock — when the holder is dead or
// the lock is older than lockStaleAfter (OD6). A corrupt or unreadable
// lock file is treated as stale: a file that cannot name its holder cannot
// prove its claim.
func Acquire(dir, reason string, now time.Time) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: make the backup dir: %w", err)
	}
	if !holdEnter(dir) {
		return nil, &LockHeldError{Self: true}
	}
	path := filepath.Join(dir, lockFileName)
	holder := lockHolder{PID: os.Getpid(), Start: now.UTC().Format(createdFormat), Reason: reason}
	reclaimed := false
	for attempt := 0; attempt < 4; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			b, _ := json.Marshal(holder)
			if _, werr := f.Write(append(b, '\n')); werr == nil {
				_ = f.Sync()
			}
			_ = f.Close()
			return &Lock{dir: dir, pid: holder.PID, start: now, reclaimed: reclaimed}, nil
		}
		if !os.IsExist(err) {
			holdExit(dir)
			return nil, fmt.Errorf("backup: claim the backup lock: %w", err)
		}
		// Someone holds it. Read them and judge.
		other, rerr := readLockHolder(path)
		if rerr != nil || !pidAlive(other.PID) || now.Sub(parseStamp(other.Start)) >= lockStaleAfter {
			// Stale — dead, unidentifiable, or old. Reclaim through the
			// fence (the lock file is one of this package's names in the
			// dir) and retry the claim.
			if remErr := removeBackupFile(dir, lockFileName); remErr != nil {
				holdExit(dir)
				return nil, fmt.Errorf("backup: the stale lock will not be reclaimed: %w", remErr)
			}
			reclaimed = true
			continue
		}
		holdExit(dir)
		return nil, &LockHeldError{Holder: other}
	}
	holdExit(dir)
	return nil, fmt.Errorf("backup: the backup lock could not be claimed after four attempts — a live holder and a stale one are taking turns, which should not happen; check the directory")
}

// Release gives the claim back. Idempotent: a second Release, or a Release
// whose file has already been reclaimed by a later holder, is a no-op —
// deletion is conditional on the file still naming this holder.
func (l *Lock) Release() error {
	holdExit(l.dir)
	path := filepath.Join(l.dir, lockFileName)
	other, err := readLockHolder(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already gone — nothing to release
		}
		return nil // unreadable is not ours to argue with; the stale rule owns it
	}
	if other.PID == l.pid && other.Start == l.start.UTC().Format(createdFormat) {
		return removeBackupFile(l.dir, lockFileName)
	}
	return nil
}

func readLockHolder(path string) (lockHolder, error) {
	var h lockHolder
	b, err := os.ReadFile(path)
	if err != nil {
		return h, err
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return h, fmt.Errorf("the lock file is not a holder record: %w", err)
	}
	if h.PID <= 0 || h.Start == "" {
		return h, fmt.Errorf("the lock file names no holder")
	}
	return h, nil
}

func parseStamp(s string) time.Time {
	t, err := time.Parse(createdFormat, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
