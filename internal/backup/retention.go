package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Policy is the retention shape, resolved from the four keep settings.
// Every number is a floor of keeping: a policy that keeps nothing is a
// policy the settings layer refuses before it reaches here.
type Policy struct {
	Keep       int // the newest N backups, no matter when
	KeepDaily  int // per calendar day (UTC), the newest of each of the last N days
	KeepWeekly int // per ISO week (Mon–Sun UTC), the newest of each of the last N weeks
	KeepPreMig int // the newest N pre-migration backups, governed separately
}

// Decision is one file's place in the plan: kept or not, and why — a
// deletion that cannot say why is a deletion that cannot be reviewed.
type Decision struct {
	FileName string
	Dir      string
	Keep     bool
	Why      string
}

// Plan is the pure policy: the same listing and the same instant always
// answer the same plan, and nothing here touches the disk. The listing is
// newest first per BackupView's sort (List's output), or any order — the
// plan sorts what it is given.
func Plan(backups []BackupView, p Policy, now time.Time) []Decision {
	byName := map[string]BackupView{}
	for _, b := range backups {
		if _, seen := byName[b.FileName]; !seen {
			byName[b.FileName] = b
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := byName[names[i]], byName[names[j]]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.FileName > b.FileName
	})

	keep := map[string]string{} // file name -> the rule that keeps it

	// The pre-migration family is governed separately (BK6): its own keep
	// count, never pruned by the daily/weekly/count windows, and never
	// promoted by them.
	var preMig []string
	for _, n := range names {
		if byName[n].Reason == "pre-migration" {
			preMig = append(preMig, n)
		}
	}
	for i, n := range preMig {
		if i < p.KeepPreMig {
			keep[n] = fmt.Sprintf("pre-migration keep (%d of %d)", i+1, p.KeepPreMig)
		}
	}

	// The regular family, newest first.
	var regular []string
	for _, n := range names {
		if byName[n].Reason != "pre-migration" {
			regular = append(regular, n)
		}
	}
	mark := func(n, why string) {
		if _, already := keep[n]; !already {
			keep[n] = why
		}
	}
	if len(regular) > 0 {
		mark(regular[0], "the newest backup")
	}
	// The newest VERIFIED backup is never removed, even outside every
	// window: the retention policy's one promise is that a known-good
	// copy outlives the schedule. An unverified newest is not a
	// substitute — it has never been checked, so it carries no promise.
	for _, n := range regular {
		if byName[n].Verified == "verified" {
			mark(n, "the newest verified backup")
			break
		}
	}
	// The daily windows: the newest backup of each of the last
	// KeepDaily UTC days.
	nowDay := startOfDayUTC(now)
	for day := 0; day < p.KeepDaily; day++ {
		window := nowDay.AddDate(0, 0, -day)
		for _, n := range regular {
			c := byName[n].CreatedAt
			if !c.IsZero() && startOfDayUTC(c).Equal(window) {
				mark(n, "the newest of its day (daily window)")
				break
			}
		}
	}
	// The weekly windows: the newest backup of each of the last
	// KeepWeekly ISO weeks (Monday to Sunday, UTC).
	nowWeek := startOfISOWeekUTC(now)
	for wk := 0; wk < p.KeepWeekly; wk++ {
		window := nowWeek.AddDate(0, 0, -7*wk)
		for _, n := range regular {
			c := byName[n].CreatedAt
			if !c.IsZero() && startOfISOWeekUTC(c).Equal(window) {
				mark(n, "the newest of its week (weekly window)")
				break
			}
		}
	}
	// The count window: the newest Keep, no matter when.
	for i, n := range regular {
		if i < p.Keep {
			mark(n, fmt.Sprintf("within the keep count (%d of %d)", i+1, p.Keep))
		}
	}

	out := make([]Decision, 0, len(names))
	for _, n := range names {
		if why, kept := keep[n]; kept {
			out = append(out, Decision{FileName: n, Dir: byName[n].Dir, Keep: true, Why: why})
		} else {
			reason := "beyond retention"
			if byName[n].Reason == "pre-migration" {
				reason = "beyond the pre-migration keep"
			}
			out = append(out, Decision{FileName: n, Dir: byName[n].Dir, Keep: false, Why: reason})
		}
	}
	return out
}

// Apply executes a plan: every not-kept file goes through removeBackupFile
// — the one remover (BK8) — and the removed names are returned for the
// prune's report and the pruned event.
func Apply(plan []Decision) (removed []string, err error) {
	for _, d := range plan {
		if d.Keep {
			continue
		}
		if err := removeBackupFile(d.Dir, d.FileName); err != nil {
			return removed, fmt.Errorf("backup: retention plan: %w", err)
		}
		removed = append(removed, d.FileName)
	}
	return removed, nil
}

// SweepPartials removes the crash garbage at start-up (BK4): the .partial
// tars and the .tmp-* build directories. It touches nothing else — a
// final-named backup, the lock, a restore's stage dir (T7's names) and any
// foreign file are all left exactly where they are, and a test proves the
// sweep on a directory holding one of each.
func SweepPartials(dir string) (removed []string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: sweep the backup dir: %w", err)
	}
	for _, e := range entries {
		if partialRE.MatchString(e.Name()) || tmpDirRE.MatchString(e.Name()) {
			if rerr := removeBackupFile(dir, e.Name()); rerr != nil {
				return removed, rerr
			}
			removed = append(removed, e.Name())
		}
	}
	return removed, nil
}

// removeBackupFile is the ONLY code in this package that deletes anything
// in a backup directory — the fence BK8 exists to make that sentence true
// and testable. It refuses, before touching the filesystem:
//
//   - a name that is not one of this package's own: a final .aeonbak name,
//     a .partial, a .tmp-* build dir, or the lock file (look-alikes such as
//     aeon-state-…aeonbak.bak are not in the set);
//   - a name carrying a separator, or a path that resolves outside dir;
//   - a symlink, whatever it points at (a link named .tmp-* pointing at
//     state.db is a deletion of state.db wearing a safe name);
//   - a directory that is not a (possibly non-empty) .tmp-* build dir;
//   - anything that is not a regular file or such a directory.
//
// The test TestRemoveFenceRefusesEverythingElse walks the refusal list,
// and its canary — a deliberately porous copy of this function — shows the
// assertions are not vacuous: the canary deletes everything the fence
// refuses.
func removeBackupFile(dir, name string) error {
	if dir == "" || name == "" {
		return fmt.Errorf("backup: the fence needs a dir and a name")
	}
	if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return fmt.Errorf("refused: %q is not a bare file name", name)
	}
	isFamily := fileRE.MatchString(name) || partialRE.MatchString(name) || tmpDirRE.MatchString(name) || name == lockFileName
	if !isFamily {
		return fmt.Errorf("refused: %q is not a name this package makes — the fence deletes its own names only", name)
	}
	full := filepath.Join(dir, name)
	rel, rerr := filepath.Rel(dir, full)
	if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refused: %q resolves outside the backup dir", name)
	}
	fi, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already gone — removal is a goal, not a fact
		}
		return fmt.Errorf("backup: stat %s: %w", name, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refused: %s is a symlink, and the fence does not follow links", name)
	}
	if fi.IsDir() {
		if !tmpDirRE.MatchString(name) {
			return fmt.Errorf("refused: %s is a directory, and only a .tmp-* build dir may be removed as one", name)
		}
		return os.RemoveAll(full)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("refused: %s is %s, not a regular file", name, fi.Mode().String())
	}
	return os.Remove(full)
}

func startOfDayUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// startOfISOWeekUTC is the Monday 00:00 UTC of the week t falls in (ISO
// 8601: weeks run Monday to Sunday; Sunday is the seventh day, not the
// first — Go's Weekday() starts Sunday at zero, so the shift is
// (weekday+6)%7 days back).
func startOfISOWeekUTC(t time.Time) time.Time {
	day := startOfDayUTC(t)
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}

// removeRestoreStageDir is the fence's restore variant (T7, BK9 step 8):
// the stage dir the swap staged inside the state dir is this path's second
// — and last — deletion site, and it goes through the fence like the
// backup dir's own names do: a bare name from a family this package makes
// (restoreDirRE), resolving inside the dir given, no symlink, and a
// directory before it is removed. The pre-restore dir is never a family
// name, so it can never reach this remover.
func removeRestoreStageDir(dir, name string) error {
	if dir == "" || name == "" {
		return fmt.Errorf("backup: the fence needs a dir and a name")
	}
	if !restoreDirRE.MatchString(name) {
		return fmt.Errorf("refused: %q is not a restore stage dir — the fence deletes its own names only", name)
	}
	full := filepath.Join(dir, name)
	rel, rerr := filepath.Rel(dir, full)
	if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refused: %q resolves outside the state dir", name)
	}
	fi, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already gone — removal is a goal, not a fact
		}
		return fmt.Errorf("backup: stat %s: %w", name, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refused: %s is a symlink, and the fence does not follow links", name)
	}
	if !fi.IsDir() {
		return fmt.Errorf("refused: %s is not a directory", name)
	}
	return os.RemoveAll(full)
}
