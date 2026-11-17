package daemon

// backup_service_test.go — S62 T6: the daemon half's behaviour, each a test
// (T6.md §Behaviour). The store is real (a throwaway temp dir — tests never
// point at the real store) but the CLOCK is the harness's: both the store's
// I9 seam and the service's engine clock read one shared instant, so a case
// can seed a row "25 hours ago" and step the machine forward day by day
// without sleeping. The settings seam is the env-shaped one over a fixed map,
// so a case can turn the schedule off or shrink the retention without
// touching the process environment.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// backupTestNow is the calendar point the machine starts at: a Monday noon in
// UTC, far from any day/week boundary the retention windows count from.
var backupTestNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// backupFinalNameRE is the ledger's file-name shape (BK4) — a name matching it
// is a PUBLISHED backup; one that does not is a build artifact and garbage the
// sweep owns.
var backupFinalNameRE = regexp.MustCompile(`^aeon-state-\d{8}T\d{6}Z-v\d+\.aeonbak$`)

// capturedEvent is one event the harness saw on the publish seam — kind plus
// the payload exactly as the engine or the service shaped it.
type capturedEvent struct {
	kind    string
	payload any
}

// backupHarness is a live store and a service over it, sharing one clock.
type backupHarness struct {
	t      *testing.T
	now    *time.Time
	st     *store.Store
	svc    *BackupService
	env    map[string]string
	mu     sync.Mutex
	events []capturedEvent
}

// newBackupHarness opens a throwaway store and builds the service over it.
// env is the AEON_BACKUP_* values the settings seam sees (nil = all defaults).
func newBackupHarness(t *testing.T, env map[string]string) *backupHarness {
	t.Helper()
	h := &backupHarness{t: t, now: &backupTestNow, env: env}
	st, err := store.Open(store.Options{
		Dir:   t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: func() time.Time { return *h.now },
	})
	if err != nil {
		t.Fatalf("open the throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	publish := func(kind string, payload any) {
		h.mu.Lock()
		h.events = append(h.events, capturedEvent{kind: kind, payload: payload})
		h.mu.Unlock()
	}
	// nil settings store: the test mount has no settings store, so the
	// service keeps the environment settings — which this harness then
	// replaces with its own seam two lines down.
	svc := NewBackupService(st, nil, "9.9.9-test", publish, discardLogger{})
	if svc == nil {
		t.Fatalf("NewBackupService over a live store answered nil — nil is the storeless daemon's answer (V13)")
	}
	svc.deps.Clock = func() time.Time { return *h.now }
	svc.deps.Settings = backup.NewEnvSettingsWith(func(key string) string { return h.env[key] })
	h.st, h.svc = st, svc
	return h
}

// setNow moves the shared clock: the store's finished_at stamps and the
// engine's due/age arithmetic both see the move.
func (h *backupHarness) setNow(at time.Time) { h.now = &at }

func (h *backupHarness) setNowAfter(d time.Duration) { at := h.now.Add(d); h.now = &at }

func (h *backupHarness) eventsOf(kind string) []capturedEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []capturedEvent
	for _, e := range h.events {
		if e.kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// countBackupRows counts the ledger's kind=backup rows (optionally one
// outcome), the harness's ledger read.
func (h *backupHarness) countBackupRows(t *testing.T, outcome string) int {
	t.Helper()
	rows, err := h.st.ListBackupRuns(store.BackupRunFilter{Kind: "backup", Outcome: outcome, Limit: 1000})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return len(rows)
}

// backupRows returns the kind=backup rows newest first.
func (h *backupHarness) backupRows(t *testing.T) []store.BackupRun {
	t.Helper()
	rows, err := h.st.ListBackupRuns(store.BackupRunFilter{Kind: "backup", Limit: 1000})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return rows
}

// seedBackupRow records one finished backup row at the shared clock's instant
// (the store stamps finished_at from it). The file need not exist: the
// schedule's due arithmetic reads the ledger, not the disk.
func (h *backupHarness) seedBackupRow(t *testing.T, file, outcome, errorKind string) {
	t.Helper()
	row, err := h.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "backup",
		Reason:         "scheduled",
		ActorKind:      "daemon",
		StartedAt:      (*h.now).UTC().Format(backupStampFormat),
		Outcome:        outcome,
		ErrorKind:      errorKind,
		Detail:         "the online snapshot failed",
		FileName:       file,
		FreeSpaceState: "unknown",
	})
	if err != nil {
		t.Fatalf("seed the ledger row: %v", err)
	}
	_ = row
}

// backupDir is the harness's resolved main backup dir (the service's own
// resolution, not a second one).
func (h *backupHarness) backupDir(t *testing.T) string {
	t.Helper()
	resolved, err := h.svc.resolved()
	if err != nil {
		t.Fatalf("resolve the settings: %v", err)
	}
	dir := h.svc.mainBackupDir(resolved)
	if dir == "" {
		t.Fatal("the harness store has no backup dir — the state dir resolution is broken")
	}
	return dir
}

// ─── behaviour 1: the schedule is restart-safe ───────────────────────────────

// TestSchedulerCatchUpIsOneRun is the schedule's whole argument: the due
// instant is re-derived from the ledger on every wake, so a daemon that was
// down for 3 days catches up with EXACTLY ONE run — not seventy-two, not
// zero. The same wake also answers "runs once at start" for a schedule that
// is 25 h behind its 24 h interval.
func TestSchedulerCatchUpIsOneRun(t *testing.T) {
	h := newBackupHarness(t, nil)

	// Last success 25 h ago (the interval is the 24 h default): due.
	h.setNow(backupTestNow.Add(-25 * time.Hour))
	h.seedBackupRow(t, "aeon-state-20261101T110000Z-v76.aeonbak", "ok", "")
	h.setNow(backupTestNow)

	h.svc.tick(context.Background(), true) // the wake at start

	rows := h.backupRows(t)
	if got := len(rows); got != 2 {
		t.Fatalf("after the start wake the ledger holds %d backup rows, want the seed plus exactly one run", got)
	}
	newest := rows[0]
	if newest.Reason != "scheduled" || newest.ActorKind != "daemon" || newest.Outcome != "ok" {
		t.Errorf("the catch-up run is reason %q actor %q outcome %q, want scheduled/daemon/ok — the ledger row is the schedule's own (BK10)", newest.Reason, newest.ActorKind, newest.Outcome)
	}

	// 3 days of downtime: exactly one catch-up, whatever the arithmetic.
	h.setNowAfter(72 * time.Hour)
	h.svc.tick(context.Background(), false)
	if got := h.countBackupRows(t, "ok"); got != 3 {
		t.Fatalf("after 72 h of downtime the ledger holds %d ok rows, want 3 — a catch-up is ONE run, the interval's due instant is re-derived from the ledger, never replayed", got)
	}

	// And a wake an hour later is NOT due: one hour is less than the
	// 24 h interval since the catch-up.
	h.setNowAfter(1 * time.Hour)
	h.svc.tick(context.Background(), false)
	if got := h.countBackupRows(t, "ok"); got != 3 {
		t.Fatalf("an hour after the catch-up the ledger holds %d ok rows, want still 3 — the interval has not passed", got)
	}
}

// TestSchedulerDisabledNeverRuns: the setting is the schedule's off switch,
// and a disabled schedule that was down a long time still runs nothing on
// return — the setting is read on every wake, so it outranks the due instant.
func TestSchedulerDisabledNeverRuns(t *testing.T) {
	h := newBackupHarness(t, map[string]string{"AEON_BACKUP_ENABLED": "false"})

	h.setNow(backupTestNow.Add(-72 * time.Hour))
	h.seedBackupRow(t, "aeon-state-20261030T120000Z-v76.aeonbak", "ok", "")
	h.setNow(backupTestNow)

	h.svc.tick(context.Background(), true)

	rows := h.backupRows(t)
	if len(rows) != 1 {
		t.Fatalf("a disabled schedule left %d backup rows on its wake, want only the seed — disabled means never (BK7)", len(rows))
	}
	if h.countBackupRows(t, "skipped") != 0 {
		t.Error("a disabled wake recorded a skip — a skip is a FACT of the schedule (busy, shutting down), and a disabled schedule is not busy, it is off")
	}
	if len(h.eventsOf("backup.finished"))+len(h.eventsOf("backup.failed")) != 0 {
		t.Error("a disabled schedule emitted a run event — it ran nothing")
	}
}

// TestSchedulerSkipsWhileBusy: a tick that lands while a run is going does
// not start a second — it records the skip on the ledger, the schedule's own
// voice ("skipped: busy"), and stops.
func TestSchedulerSkipsWhileBusy(t *testing.T) {
	h := newBackupHarness(t, nil)

	h.svc.mu.Lock()
	h.svc.runActive = true // a run in progress
	h.svc.mu.Unlock()

	h.svc.tick(context.Background(), true)

	rows := h.backupRows(t)
	if len(rows) != 1 {
		t.Fatalf("a busy tick left %d backup rows, want exactly one (the skip)", len(rows))
	}
	row := rows[0]
	if row.Outcome != "skipped" || row.Detail != "skipped: busy" {
		t.Errorf("the skip row is outcome %q detail %q, want skipped / %q — the fixed template, never the reason's text (SEC-033)", row.Outcome, row.Detail, "skipped: busy")
	}
	if row.Reason != "scheduled" || row.ActorKind != "daemon" {
		t.Errorf("the skip row is reason %q actor %q, want scheduled/daemon — the skip is a fact of the schedule, not of a caller", row.Reason, row.ActorKind)
	}
	if h.countBackupRows(t, "ok") != 0 {
		t.Error("a busy tick started a run — single-flight is the schedule's one promise")
	}
}

// ─── behaviour 2: the daemon methods ─────────────────────────────────────────

// TestBackupMethodsRoundTrip runs the five doors over the live store: run
// creates a VERIFIED backup and returns the list's own shape for it, list
// answers newest-first with the can-restore fact, status answers fresh, and
// prune plans before it applies.
func TestBackupMethodsRoundTrip(t *testing.T) {
	h := newBackupHarness(t, nil)
	actor := protocol.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()

	res, refusal, err := h.svc.Run(ctx, actor, nil) // nil verify = the setting (default: on)
	if err != nil {
		t.Fatalf("backup.run: %v", err)
	}
	if refusal != nil {
		t.Fatalf("backup.run was refused (%s: %s) over a live empty store — nothing may hold the lock", refusal.Kind, refusal.Detail)
	}
	if res.Run.VerifyLevel != "verified" {
		t.Errorf("the run's verify level is %q, want verified — the setting is on by default and the run must keep its promise", res.Run.VerifyLevel)
	}
	if res.Backup == nil {
		t.Fatal("the run's answer carries no backup view — the fresh file must be shown the way every other backup is shown (the list's shape, not a second one)")
	}
	list, err := h.svc.List(ctx, 0, false)
	if err != nil {
		t.Fatalf("backup.list: %v", err)
	}
	if len(list.Backups) != 1 || list.Backups[0].FileName != res.Run.FileName {
		t.Fatalf("backup.list answered %d entries (first %v), want the fresh file named by the run", len(list.Backups), list.Backups)
	}
	if !list.Backups[0].CanRestoreWithThisBinary {
		t.Error("the list says this binary cannot restore a backup this binary just made at its own applied count — the fact is the manifest's applied set vs the binary's, and they are the same")
	}
	if len(list.Dirs) != 1 {
		t.Errorf("backup.list names %d dirs without the pre-migration ask, want 1 (the main dir)", len(list.Dirs))
	}

	view, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("backup.status: %v", err)
	}
	if view.State != "fresh" {
		t.Errorf("the status is %q right after a successful run, want fresh", view.State)
	}
	if view.DB.Applied == 0 {
		t.Error("the status's database block reports zero applied migrations on a freshly migrated store — Info() is lying or unasked")
	}

	plan, refusal, err := h.svc.Prune(ctx, actor, true) // dry run: plan only
	if err != nil {
		t.Fatalf("backup.prune dry run: %v", err)
	}
	if refusal != nil {
		t.Fatalf("the dry run was refused (%s: %s) — planning deletes nothing and may never be refused by the fence", refusal.Kind, refusal.Detail)
	}
	if len(plan.Plan) != 1 || !plan.Plan[0].Keep {
		t.Errorf("the dry run's plan is %v, want the single fresh backup kept — a retention that prunes its only copy is a retention that destroys recovery", plan.Plan)
	}
	if plan.Removed != nil {
		t.Errorf("a dry run reported removed %v — a plan is a plan", plan.Removed)
	}
}

// TestBackupMethodsRefuseSeparators is the service-side twin of the wire's
// BK12 refusal: the shared door (the one T7's direct door and any future
// caller reach) resolves a file name against the configured dirs, and a name
// carrying a separator would make that join a PATH. The socket refuses the
// same shape with -32602 (the protocol layer's test owns that half); here the
// service itself must answer a refusal that names the shape, and nothing may
// leave the configured dirs.
func TestBackupMethodsRefuseSeparators(t *testing.T) {
	h := newBackupHarness(t, nil)
	actor := protocol.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()

	for _, file := range []string{"../outside.aeonbak", "a/b.aeonbak", `pre\migration\x.aeonbak`} {
		_, refusal, err := h.svc.Verify(ctx, actor, file, false)
		if err != nil {
			t.Fatalf("backup.verify %q: infrastructure error %v — a refusal is a RESULT, and this is a refusal", file, err)
		}
		if refusal == nil {
			t.Fatalf("backup.verify %q was NOT refused — a separator in the name is a path, and the join would have left the configured dirs (BK12)", file)
		}
		if refusal.Kind != "unknown" {
			t.Errorf("the refusal's kind is %q, want unknown — the name is not a file the dirs hold", refusal.Kind)
		}
		if !strings.Contains(refusal.Detail, fmt.Sprintf("%q", file)) {
			t.Errorf("the refusal %q does not name the refused thing %q", refusal.Detail, file)
		}
	}

	// And the ledger got no verdict for a file that was never checked.
	if got := h.countBackupRows(t, ""); got != 0 {
		t.Fatalf("refused verifies recorded %d rows, want 0 — a refusal is not a verdict", got)
	}
}

// TestBackupStatusStateMachine walks the four states in the machine's own
// order (never → fresh → stale → failing), each from the numbers the status
// itself reports.
func TestBackupStatusStateMachine(t *testing.T) {
	h := newBackupHarness(t, nil)
	actor := protocol.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()

	state := func() string {
		t.Helper()
		view, err := h.svc.Status(ctx)
		if err != nil {
			t.Fatalf("backup.status: %v", err)
		}
		return view.State
	}

	if got := state(); got != "never" {
		t.Fatalf("a fresh store reports %q, want never — no success, no state", got)
	}

	if _, refusal, err := h.svc.Run(ctx, actor, nil); err != nil || refusal != nil {
		t.Fatalf("the run that establishes fresh: %v / %v", err, refusal)
	}
	if got := state(); got != "fresh" {
		t.Fatalf("right after a successful run the state is %q, want fresh (last success at the clock's instant, the 48 h stale window open)", got)
	}

	h.setNowAfter(49 * time.Hour) // past the 48 h default
	if got := state(); got != "stale" {
		t.Fatalf("49 h after the last success the state is %q, want stale — past staleAfter is the definition", got)
	}

	// Two failed scheduled runs outrank staleness: failing beats stale,
	// because the fix is different (list.go's stated priority).
	h.seedBackupRow(t, "", "failed", "io")
	h.seedBackupRow(t, "", "failed", "io")
	if got := state(); got != "failing" {
		t.Fatalf("after two failed scheduled runs the state is %q, want failing — a backup that is both old and broken is told as broken", got)
	}
}

// ─── behaviour 3: the events ─────────────────────────────────────────────────

// assertEventCarriesNoPaths walks an event payload and refuses any string
// that carries a path separator: the contract is ids, enums, numbers and the
// file's BASENAME (BK16's payload rule, the SEC-033 twin on the wire).
func assertEventCarriesNoPaths(t *testing.T, payload any) {
	t.Helper()
	switch v := payload.(type) {
	case map[string]any:
		for _, e := range v {
			assertEventCarriesNoPaths(t, e)
		}
	case []any:
		for _, e := range v {
			assertEventCarriesNoPaths(t, e)
		}
	case []string:
		for _, e := range v {
			assertEventCarriesNoPaths(t, e)
		}
	case string:
		if strings.ContainsAny(v, `/\`) {
			t.Errorf("an event string carries a path separator: %q — events carry ids, enums, numbers and the basename, never a path", v)
		}
	default:
		// numbers, booleans, nil — the allowed rest
	}
}

// TestEventsCarryNoPaths runs a backup, walks the stale transition (once,
// re-armed) and a real prune, and checks every event the machine emitted:
// no path in any of them, and the enum fields inside the closed sets.
func TestEventsCarryNoPaths(t *testing.T) {
	h := newBackupHarness(t, nil)
	actor := protocol.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()

	if _, refusal, err := h.svc.Run(ctx, actor, nil); err != nil || refusal != nil {
		t.Fatalf("the run: %v / %v", err, refusal)
	}

	fins := h.eventsOf("backup.finished")
	if len(fins) != 1 {
		t.Fatalf("the run emitted %d finished events, want 1 — the daemon is where the engine's events live (BK16)", len(fins))
	}
	assertEventCarriesNoPaths(t, fins[0].payload)
	payload, ok := fins[0].payload.(map[string]any)
	if !ok {
		t.Fatalf("the finished payload is %T, want a map", fins[0].payload)
	}
	if got, _ := payload["file_name"].(string); !backupFinalNameRE.MatchString(got) {
		t.Errorf("the finished payload's file_name %q is not the ledger's basename shape", got)
	}
	if lvl, _ := payload["verify_level"].(string); lvl != "verified" && lvl != "written" && lvl != "drill" {
		t.Errorf("the finished payload's verify_level %q is outside the closed set", lvl)
	}
	if kind, _ := payload["actor_kind"].(string); kind != "user" && kind != "daemon" && kind != "cli" {
		t.Errorf("the finished payload's actor_kind %q is outside the closed set", kind)
	}

	// The stale transition: fires ONCE when the machine enters stale…
	h.setNowAfter(49 * time.Hour)
	view, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "stale" {
		t.Fatalf("the machine is %q, want stale — the transition cannot be tested without it", view.State)
	}
	stales := h.eventsOf("backup.stale")
	if len(stales) != 1 {
		t.Fatalf("entering stale emitted %d stale events, want exactly 1 — once per transition (BK16)", len(stales))
	}
	assertEventCarriesNoPaths(t, stales[0].payload)
	sp, _ := stales[0].payload.(map[string]any)
	if state, _ := sp["state"].(string); state != "stale" {
		t.Errorf("the stale payload's state is %q, want stale", state)
	}
	if age, _ := sp["age_seconds"].(int64); age <= 48*3600 {
		t.Errorf("the stale payload's age is %d s, want past the 48 h threshold the machine just crossed", age)
	}
	// …and a second observation of the same stale state emits nothing.
	if _, err := h.svc.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(h.eventsOf("backup.stale")); got != 1 {
		t.Fatalf("a second observation of the same stale state emitted %d events, want still 1 — re-firing would make the notice a shout", got)
	}

	// …and leaving stale re-arms: a new success, then the machine re-crosses.
	if _, refusal, err := h.svc.Run(ctx, actor, nil); err != nil || refusal != nil {
		t.Fatalf("the run that leaves stale: %v / %v", err, refusal)
	}
	if _, err := h.svc.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(h.eventsOf("backup.stale")); got != 1 {
		t.Fatalf("leaving stale emitted an event — the arm fires on the transition INTO stale, never out of it")
	}
	h.setNowAfter(49 * time.Hour)
	if _, err := h.svc.Status(ctx); err != nil {
		t.Fatal(err)
	}
	stales = h.eventsOf("backup.stale")
	if len(stales) != 2 {
		t.Fatalf("the second transition into stale emitted %d total events, want 2 — the re-arm is the contract", len(stales))
	}
	assertEventCarriesNoPaths(t, stales[1].payload)
}

// TestEventsCarryNoPathsPruned covers the prune half of the same contract:
// backup.pruned fires when (and only when) files went, and names them by
// basename.
func TestEventsCarryNoPathsPruned(t *testing.T) {
	// keep=1 with the daily/weekly windows at their floors (a 0 is refused
	// by the settings — "at least one is kept"), so only the newest
	// survives the count window and neither day nor week window reaches the
	// older run: it is the week's SECOND-newest, two days old.
	h := newBackupHarness(t, map[string]string{
		"AEON_BACKUP_KEEP":        "1",
		"AEON_BACKUP_KEEP_DAILY":  "1",
		"AEON_BACKUP_KEEP_WEEKLY": "1",
	})
	actor := protocol.Actor{Kind: "user", ID: "1000"}
	ctx := context.Background()
	noVerify := false

	// Two unverified runs two days apart (unverified: the newest-verified
	// promise cannot keep the older one, and the day windows are off).
	if _, refusal, err := h.svc.Run(ctx, actor, &noVerify); err != nil || refusal != nil {
		t.Fatalf("the first run: %v / %v", err, refusal)
	}
	h.setNowAfter(48 * time.Hour)
	if _, refusal, err := h.svc.Run(ctx, actor, &noVerify); err != nil || refusal != nil {
		t.Fatalf("the second run: %v / %v", err, refusal)
	}
	if got := len(h.eventsOf("backup.pruned")); got != 0 {
		t.Fatalf("runs emitted %d pruned events — the runs do not prune", got)
	}

	view, refusal, err := h.svc.Prune(ctx, actor, false)
	if err != nil {
		t.Fatalf("backup.prune: %v", err)
	}
	if refusal != nil {
		t.Fatalf("the apply was refused (%s: %s) — nothing holds the fence in a test", refusal.Kind, refusal.Detail)
	}
	if len(view.Removed) != 1 {
		t.Fatalf("the prune removed %v, want exactly the older of the two backups (keep=1, windows off)", view.Removed)
	}
	if !backupFinalNameRE.MatchString(view.Removed[0]) {
		t.Errorf("the prune removed %q — not a ledger basename", view.Removed[0])
	}

	pruned := h.eventsOf("backup.pruned")
	if len(pruned) != 1 {
		t.Fatalf("a prune that removed one file emitted %d pruned events, want 1", len(pruned))
	}
	assertEventCarriesNoPaths(t, pruned[0].payload)
	p, _ := pruned[0].payload.(map[string]any)
	if removed, _ := p["removed"].(int); removed != 1 {
		t.Errorf("the pruned payload's removed is %v, want 1", p["removed"])
	}
	// The capture seam receives the payload in-memory: file_names is the
	// service's own []string (the wire carries the same JSON array).
	names, ok := p["file_names"].([]string)
	if !ok || len(names) != 1 {
		t.Fatalf("the pruned payload's file_names is %v, want the one removed name", p["file_names"])
	}

	// And the act is on the ledger: one prune row, ok, the count in a fixed
	// template.
	rows, err := h.st.ListBackupRuns(store.BackupRunFilter{Kind: "prune", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Outcome != "ok" || rows[0].Detail != "prune: 1 removed" {
		t.Errorf("the prune's ledger row is %+v, want one ok row detailing the count", rows)
	}
}

// ─── behaviour 4: SIGTERM mid-run ────────────────────────────────────────────

// blockingSnapshotStore is the real store with one stage slowed: the
// snapshot blocks until its context goes, which is what a SIGTERM mid-run is
// to the engine — a context that cancels while the run is inside a stage.
type blockingSnapshotStore struct {
	*store.Store
}

func (b *blockingSnapshotStore) SnapshotTo(ctx context.Context, dest string) (store.SnapshotInfo, error) {
	<-ctx.Done()
	return store.SnapshotInfo{}, ctx.Err()
}

// TestCancelledRunLeavesNoFinalNameAndRecordsCancelled is the SIGTERM
// contract at the service level: the ticker's context goes (the run's context
// is derived from it, so the run's goes too), and the run ends as a CANCELLED
// fact — no file with a final name, no build directory left, one ledger row
// with error_kind cancelled, and the failed event that says the stage.
func TestCancelledRunLeavesNoFinalNameAndRecordsCancelled(t *testing.T) {
	h := newBackupHarness(t, nil)
	h.svc.deps.Store = &blockingSnapshotStore{Store: h.st}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond) // the run is inside the snapshot
		cancel()
	}()

	actor := protocol.Actor{Kind: "user", ID: "1000"}
	res, refusal, err := h.svc.Run(ctx, actor, nil)
	if err != nil {
		t.Fatalf("a cancelled run surfaced as an infrastructure error %v — the engine's RunErrors are RESULTS, the refusals the handler shapes", err)
	}
	if refusal == nil {
		t.Fatalf("a cancelled run answered success (res %+v) — it ended cancelled, and the answer must say so", res)
	}
	if refusal.Kind != "cancelled" {
		t.Errorf("the refusal's kind is %q, want cancelled — the context went, and the engine names it", refusal.Kind)
	}

	// Nothing with a final name, no build directory: the publish never
	// happened and the cleanup took the garbage.
	dir := h.backupDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the backup dir: %v", err)
	}
	for _, e := range entries {
		if backupFinalNameRE.MatchString(e.Name()) {
			t.Errorf("a cancelled run published %s — the atomic publish's whole point is that a dead run leaves nothing final-named (BK4)", e.Name())
		}
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("a cancelled run left the build directory %s — the cleanup owns it", e.Name())
		}
	}

	// The row: failed, cancelled, the fixed template.
	rows := h.backupRows(t)
	if len(rows) != 1 {
		t.Fatalf("a cancelled run left %d backup rows, want exactly one (the cancellation is a fact)", len(rows))
	}
	row := rows[0]
	if row.Outcome != "failed" || strvRow(row.ErrorKind) != "cancelled" {
		t.Errorf("the row is outcome %q error_kind %q, want failed / cancelled", row.Outcome, strvRow(row.ErrorKind))
	}
	if row.Detail != "the run was cancelled" {
		t.Errorf("the row's detail is %q, want the fixed template — never the raw error", row.Detail)
	}

	// And the failed event names the stage the context caught it in.
	faileds := h.eventsOf("backup.failed")
	if len(faileds) != 1 {
		t.Fatalf("the cancellation emitted %d failed events, want 1", len(faileds))
	}
	assertEventCarriesNoPaths(t, faileds[0].payload)
	p, _ := faileds[0].payload.(map[string]any)
	if kind, _ := p["error_kind"].(string); kind != "cancelled" {
		t.Errorf("the failed payload's error_kind is %q, want cancelled", kind)
	}
	if stage, _ := p["stage"].(string); stage != "cancelled" {
		t.Errorf("the failed payload's stage is %q, want cancelled", stage)
	}
}
