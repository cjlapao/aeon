package backup

// The retention policy's table: what is kept, why, and what the fence
// refuses. The plan is pure; the fence is the only deletion in the package.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The week under test: 2026-11-02 is a Monday, so the ISO weeks are
// Nov 2–8 (wk0), Oct 26–Nov 1 (wk1), Oct 19–25 (wk2).
var retNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func retView(name string, at time.Time, reason, verified string) BackupView {
	return BackupView{FileName: name, Dir: "the-dir", CreatedAt: at, Reason: reason, Verified: verified}
}

func retName(day string) string {
	// A legal final name per the closed set; the date part is cosmetic
	// for the pure plan, which sorts on CreatedAt.
	return "aeon-state-" + strings.ReplaceAll(day, "-", "") + "T000000Z-v3.aeonbak"
}

func TestRetentionPlanTable(t *testing.T) {
	p := Policy{Keep: 1, KeepDaily: 2, KeepWeekly: 3, KeepPreMig: 2}

	// A: Mon Nov 2 11:00 — the newest regular.
	// B: Sun Nov 1 12:00 — within the daily (day1) and weekly (wk1) windows.
	// C: Sat Oct 31 12:00 — outside every window (day2 not kept, wk1's newest is B).
	// D: Sun Oct 25 12:00 — outside the daily windows, kept by weekly wk2.
	// E: Mon Oct 19 12:00 — same week as D, older in it: not kept.
	views := []BackupView{
		retView(retName("2026-10-19"), time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC), "scheduled", "unverified"),
		retView(retName("2026-10-25"), time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC), "scheduled", "unverified"),
		retView(retName("2026-10-31"), time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), "manual", "unverified"),
		retView(retName("2026-11-01"), time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), "scheduled", "unverified"),
		retView(retName("2026-11-02"), time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC), "scheduled", "unverified"),
		// The pre-migration family: governed by its own keep only.
		retView("aeon-state-20261102T010000Z-v3.aeonbak", time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC), "pre-migration", "unverified"),
		retView("aeon-state-20261101T010000Z-v3.aeonbak", time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC), "pre-migration", "unverified"),
		retView("aeon-state-20261031T010000Z-v3.aeonbak", time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC), "pre-migration", "unverified"),
	}
	// Reorder the input: the plan must not depend on listing order.
	views = append(views[len(views)-2:], views[:len(views)-2]...)

	plan := Plan(views, p, retNow)
	byName := map[string]Decision{}
	for _, d := range plan {
		byName[d.FileName] = d
	}

	want := map[string]struct {
		keep bool
		why  string // substring
	}{
		retName("2026-11-02"):                    {true, "the newest backup"},
		retName("2026-11-01"):                    {true, "daily window"},
		retName("2026-10-31"):                    {false, "beyond retention"},
		retName("2026-10-25"):                    {true, "weekly window"},
		retName("2026-10-19"):                    {false, "beyond retention"},
		"aeon-state-20261102T010000Z-v3.aeonbak": {true, "pre-migration keep"},
		"aeon-state-20261101T010000Z-v3.aeonbak": {true, "pre-migration keep"},
		"aeon-state-20261031T010000Z-v3.aeonbak": {false, "beyond the pre-migration keep"},
	}
	if len(plan) != len(want) {
		t.Fatalf("the plan has %d decisions, the table has %d files", len(plan), len(want))
	}
	for name, w := range want {
		d, ok := byName[name]
		if !ok {
			t.Errorf("the plan has no decision for %s", name)
			continue
		}
		if d.Keep != w.keep {
			t.Errorf("%s: kept=%v, the table says %v (why %q)", name, d.Keep, w.keep, d.Why)
		}
		if !strings.Contains(d.Why, w.why) {
			t.Errorf("%s: the reason %q does not say %q — a deletion that cannot say why is a deletion that cannot be reviewed", name, d.Why, w.why)
		}
	}
	// The pre-migration family never borrows the regular windows: the
	// third pre-migration backup is NOT kept by the count or the daily
	// window even though it is within the last two UTC days.
	if d := byName["aeon-state-20261031T010000Z-v3.aeonbak"]; d.Keep {
		t.Errorf("a pre-migration backup rode the regular windows into the keep: %q", d.Why)
	}
}

// TestNewestVerifiedAlwaysKept: a verified backup outlives the schedule;
// an unverified one does not — it has never been checked, so it carries no
// promise.
func TestNewestVerifiedAlwaysKept(t *testing.T) {
	p := Policy{Keep: 1, KeepDaily: 1, KeepWeekly: 1, KeepPreMig: 1}
	views := []BackupView{
		// A: the newest, unverified — the count/daily/weekly windows all keep it.
		retView(retName("2026-11-02"), time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC), "scheduled", "unverified"),
		// U: unverified, inside the same day window but older in it — not kept.
		retView(retName("20261102T100000Z"), time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), "scheduled", "unverified"),
		// V: verified, outside every window (day1, wk1) — kept by the promise.
		retView(retName("2026-11-01"), time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC), "scheduled", "verified"),
	}
	plan := Plan(views, p, retNow)
	got := map[string]bool{}
	for _, d := range plan {
		got[d.FileName] = d.Keep
	}
	if !got[retName("2026-11-02")] {
		t.Errorf("the newest backup is not kept")
	}
	if got[retName("20261102T100000Z")] {
		t.Errorf("an unverified backup was kept outside its windows — unverified is not a promise")
	}
	if !got[retName("2026-11-01")] {
		t.Errorf("the newest VERIFIED backup was removed — the retention policy's one promise broke")
	}
}

// TestApplyRemovesThroughTheFence: Apply deletes exactly the not-kept files
// and nothing else, and it answers the removed names.
func TestApplyRemovesThroughTheFence(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep, drop := retName("2026-11-02"), retName("2026-10-31")
	mk(keep)
	mk(drop)
	mk("not-a-backup.txt") // foreign to the fence: never touched
	plan := []Decision{
		{FileName: keep, Dir: dir, Keep: true, Why: "the newest backup"},
		{FileName: drop, Dir: dir, Keep: false, Why: "beyond retention"},
	}
	removed, err := Apply(plan)
	if err != nil {
		t.Fatalf("apply the plan: %v", err)
	}
	if len(removed) != 1 || removed[0] != drop {
		t.Fatalf("apply removed %v, the plan keeps one and drops %s", removed, drop)
	}
	if _, err := os.Stat(filepath.Join(dir, drop)); !os.IsNotExist(err) {
		t.Errorf("the dropped file is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
		t.Errorf("the kept file is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "not-a-backup.txt")); err != nil {
		t.Errorf("apply touched a foreign file: %v", err)
	}
}

// TestRemoveFenceRefusesEverythingElse walks the refusal list. The canary
// at the end is a deliberately porous copy of the remover: it deletes
// everything the fence refuses, which proves the assertions are not
// vacuous.
func TestRemoveFenceRefusesEverythingElse(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(outside, []byte("the live store"), 0o600); err != nil {
		t.Fatal(err)
	}
	final := retName("2026-11-02")
	put := func(name string, contents string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// In the family: the final name (deleted), a partial, a tmp dir, the
	// lock — the fence deletes its own names.
	put(final, "data")
	put(final+".partial", "data")
	if err := os.MkdirAll(filepath.Join(dir, ".tmp-canary1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-canary1", "state.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	put(lockFileName, `{"pid":1,"start":"2026-11-02T00:00:00.000Z","reason":"manual"}`)

	// Outside the family: every one of these must survive.
	put(final+".bak", "a look-alike")
	put("aeon-state-20261102T000000Z-v3.aeonbak.old", "another look-alike")
	put("notes.txt", "foreign")
	put(".backup.lock.bak", "the lock, with a suffix")
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "inner"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A symlink wearing a safe name, pointing at the live store: the
	// deletion it hides is of the live store.
	if err := os.Symlink(outside, filepath.Join(dir, ".tmp-evil")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, retName("2026-11-01"))); err != nil {
		t.Fatal(err)
	}

	// The names the fence must delete.
	for _, name := range []string{final, final + ".partial", lockFileName} {
		if err := removeBackupFile(dir, name); err != nil {
			t.Errorf("the fence refused its own name %s: %v", name, err)
		}
	}
	canaryTmp := ".tmp-0a1b2c3d4e5f6789" // sixteen hex: the fence's own build name
	if err := os.MkdirAll(filepath.Join(dir, canaryTmp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeBackupFile(dir, canaryTmp); err != nil {
		t.Errorf("the fence refused its own tmp build dir: %v", err)
	}
	// The names it must refuse, each with the live store still present.
	for _, name := range []string{
		final + ".bak",
		"aeon-state-20261102T000000Z-v3.aeonbak.old",
		"notes.txt",
		".backup.lock.bak",
		"subdir",
		"sub/inner",
		"../escape",
		".",
		"..",
		".tmp-evil",
		retName("2026-11-01"),
	} {
		if err := removeBackupFile(dir, name); err == nil {
			t.Errorf("the fence deleted %q — it deletes its own names only", name)
		}
	}
	// The already-gone case: removal is a goal, not a fact.
	if err := removeBackupFile(dir, final); err != nil {
		t.Errorf("removing a name that is already gone: %v", err)
	}

	// Survivors.
	for _, name := range []string{final + ".bak", "notes.txt", ".backup.lock.bak", "subdir", ".tmp-evil", retName("2026-11-01")} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("the fence removed %q: %v", name, err)
		}
	}
	// The live store, twice over: both symlinks stood, and so does the target.
	fi, err := os.Stat(outside)
	if err != nil || fi.Size() != int64(len("the live store")) {
		t.Errorf("the live store is missing or altered: %v", err)
	}

	// The canary: a deliberately porous remover — no family check, no link
	// check, no dir check. It deletes everything the fence just refused,
	// which is the proof those refusals are doing work.
	cdir := t.TempDir()
	for _, name := range []string{final + ".bak", "notes.txt"} {
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cdir, name), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(cdir, ".tmp-evil")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{final + ".bak", "notes.txt", ".tmp-evil"} {
		if err := porousRemover(cdir, name); err != nil {
			t.Errorf("the canary should delete %q (that is its weakness): %v", name, err)
		}
	}
	// The canary's symlink deletion reached the live store: the fence's
	// refusal of the same name is what kept the store alive.
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("the canary's symlink deletion did not reach the store — the canary is not porous, the test is not a proof")
	}
}

// porousRemover is the canary: the remover without the fence. It resolves
// the path first — the one habit that makes a naive remover delete what a
// link POINTS AT — and then removes it. It is the deletion the fence
// exists to prevent, kept here so the refusal test has a copy that deletes
// what the fence refuses: a test of a fence with no porous twin is a test
// that cannot be wrong.
func porousRemover(dir, name string) error {
	full := filepath.Join(dir, name)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return err
	}
	if fi, err := os.Lstat(real); err == nil && fi.IsDir() {
		return os.RemoveAll(real)
	}
	return os.Remove(real)
}

// TestSweepPartialsTouchesOnlyGarbage: the start-up sweep removes the
// crash garbage and leaves every other name exactly where it is.
func TestSweepPartialsTouchesOnlyGarbage(t *testing.T) {
	dir := t.TempDir()
	final := retName("2026-11-02")
	for _, name := range []string{final, final + ".partial", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	crashTmp := ".tmp-0a1b2c3d4e5f6789" // sixteen hex: the fence's own build name
	if err := os.MkdirAll(filepath.Join(dir, crashTmp), 0o700); err != nil {
		t.Fatalf("mk the crash dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".backup.lock"), []byte(`{"pid":1,"start":"2026-11-02T00:00:00.000Z","reason":"manual"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := SweepPartials(dir)
	if err != nil {
		t.Fatalf("the sweep: %v", err)
	}
	got := map[string]bool{}
	for _, r := range removed {
		got[r] = true
	}
	if !got[final+".partial"] || !got[crashTmp] {
		t.Fatalf("the sweep removed %v, the crash garbage is the partial and the tmp dir", removed)
	}
	for _, keep := range []string{final, "notes.txt", ".backup.lock"} {
		if _, err := os.Lstat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("the sweep touched %q: %v", keep, err)
		}
	}
}
