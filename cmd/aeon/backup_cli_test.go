package main

// backup_cli_test.go — S62 T9: the backup CLI's behaviour at the binary
// level. The goldens pin the bytes of two fixed worlds; these tests pin
// the behaviours the goldens cannot reach:
//
//   - the door's choice, and the ONE fault that falls back (BK12, S45 T8
//     rule): nobody is home goes to the direct door with the fixed line;
//     a socket that is THERE and refuses — a leftover, a permission, a
//     different protocol — is refused as-is, never a fallback;
//   - the direct door's never-migrates contract, held against an
//     old-vintage store (BK12, T5's hook it must never trip);
//   - --json as the wire verbatim (decode back into the protocol's own
//     types);
//   - the two doors' bytes held to the same shape on the same verb
//     sequence (BK12's parity), door-specific facts tokenised.
//
// Nothing here points at the real store: every state dir is a throwaway.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// bkCleanEnv is the fixture's env strip (the S45 T8 finding): an ambient
// store address from another test would shadow the XDG pin and send the
// subprocess at the wrong store, so the known store-address keys are
// removed before the throwaway pins are appended.
func bkCleanEnv() []string {
	base := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "XDG_STATE_HOME=") ||
			strings.HasPrefix(kv, "LOCALRPC_ENDPOINT_PATH=") ||
			strings.HasPrefix(kv, "AEON_STATE_DIR=") ||
			strings.HasPrefix(kv, "DATABASE_SQLITE_STORAGE_PATH=") ||
			strings.HasPrefix(kv, "DATABASE_SQLITE_FILE_NAME=") ||
			strings.HasPrefix(kv, "DATABASE_SQLITE_DSN=") {
			continue
		}
		base = append(base, kv)
	}
	return base
}

// bkShortStateDir is a throwaway state dir at a SHORT path: t.TempDir
// names the directory after the test, and a long test name pushes the
// endpoint path past the unix socket's 108-byte cap — "invalid
// argument" from bind, which looks like a permissions problem (the
// fixture's freshDaemonDirs, the same finding).
func bkShortStateDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "bk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// bkStaleSocket leaves a socket file at path with nobody listening: the
// shape of a daemon that DIED — a crash or a kill. A graceful shutdown
// unlinks its socket (the net package does the unlink on Close), so the
// leftover stands only because the listener closed after its name had
// moved: bind under a side name, rename over the endpoint, close.
func bkStaleSocket(t *testing.T, path string) {
	t.Helper()
	side := path + ".bind"
	l, err := net.Listen("unix", side)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(side, path); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	// The close unlinks the name the listener was bound at (side, now
	// dangling); the inode stands at path, with no listener behind it.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the stale socket must stand at the path: %v", err)
	}
}

// bkRunCLI runs the real binary with the throwaway state dir and the
// endpoint path (absent unless the test planted it), and returns stdout
// and stderr SEPARATELY: the door's line is on stderr, the answer on
// stdout, and a test that mixes the two cannot tell which door spoke.
func bkRunCLI(t *testing.T, stateDir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = append(bkCleanEnv(),
		"LOCALRPC_ENDPOINT_PATH="+filepath.Join(stateDir, "nothing.sock"),
		"XDG_STATE_HOME="+stateDir,
		"GOPRIVATE=github.com/cjlapao/*",
	)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// TestBackupDirectDoorLine pins the fallback's one observable: with no
// daemon (no file at the endpoint), the verb succeeds on the direct
// door and stderr carries exactly the fixed line — no more, no less, so
// a script can depend on it.
func TestBackupDirectDoorLine(t *testing.T) {
	// Short for the same 108-byte endpoint-cap reason as bkShortStateDir —
	// a long t.TempDir() turns the fallback dial into "invalid argument".
	stateDir := bkShortStateDir(t)
	out, errOut, err := bkRunCLI(t, stateDir, "backup", "status")
	if err != nil {
		t.Fatalf("backup status with no daemon failed: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	if errOut != backupDirectLine+"\n" {
		t.Fatalf("the direct door's stderr line is not the fixed one:\n%q", errOut)
	}
	if !strings.HasPrefix(out, "backup  never — dir ") || !strings.Contains(out, "/aeon/backups") {
		t.Fatalf("a fresh machine answers never, with the resolved dir:\n%s", out)
	}
}

// TestDirectDoorStatusOfAStoreItJustCreatedSaysTheTruth pins the one
// state the daemon seam never meets: a fresh machine's first verb on the
// direct door CREATES the store's file (a zero-table store), and the
// status block over it cannot say the zeros of a failed call — Info()
// needs the ledger table that does not exist yet. The journal the file
// runs in, the zero the missing ledger states, and the registry's
// pending total are the truth, stated (P31).
func TestDirectDoorStatusOfAStoreItJustCreatedSaysTheTruth(t *testing.T) {
	// A short state dir: the endpoint path must stay under the unix
	// socket's 108-byte cap (bkShortStateDir's reason), or the dial dies
	// with "invalid argument" — a dial fault that is not nobody-is-home,
	// so the door refuses instead of falling back.
	stateDir := bkShortStateDir(t)
	out, errOut, err := bkRunCLI(t, stateDir, "backup", "status")
	if err != nil {
		t.Fatalf("a fresh machine's status is a state, not a refusal: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	var dbLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "db: ") {
			dbLine = line
			break
		}
	}
	if dbLine == "" {
		t.Fatalf("the status block has no db line:\n%s", out)
	}
	total := len(store.Migrations())
	size := `(?:\d+ bytes|\d+ KiB|\d+\.\d MiB)`
	want := regexp.MustCompile(`db: ` + size + ` \+ ` + size + ` WAL · journal wal · 0 applied / ` + strconv.Itoa(total) + ` pending`)
	if !want.MatchString(dbLine) {
		t.Fatalf("the db block of a store the door just created must state the truth, not the zeros of a failed call:\n%q", dbLine)
	}
}

// TestSelectDoorFallsBackOnlyWhenNobodyIsHome holds the S45 T8 rule at
// the wire: the ONE fault that may fall back is "no file at the
// endpoint"; every other dial fault is refused as-is — and a refusal
// must leave the store untouched, because the tell of a fallback that
// should not have happened is an empty state.db the door created on its
// way through.
func TestSelectDoorFallsBackOnlyWhenNobodyIsHome(t *testing.T) {
	sockPath := func(stateDir string) string { return filepath.Join(stateDir, "nothing.sock") }
	storePath := func(stateDir string) string { return filepath.Join(stateDir, "aeon", "state.db") }

	// "nobody is home": no file at the endpoint — the one fallback.
	t.Run("nobody-is-home", func(t *testing.T) {
		stateDir := bkShortStateDir(t) // same 108-byte cap as its siblings below
		out, errOut, err := bkRunCLI(t, stateDir, "backup", "list")
		if err != nil {
			t.Fatalf("the fallback fault refused: %v\n%s%s", err, out, errOut)
		}
		if errOut != backupDirectLine+"\n" {
			t.Fatalf("the fallback must speak the fixed line: %q", errOut)
		}
		if !strings.HasPrefix(out, "backups — ") {
			t.Fatalf("the list answered through the direct door:\n%s", out)
		}
	})

	// Every other dial fault: refused, no line, and — the tell — the
	// store was never opened.
	refused := func(t *testing.T, stateDir string, why string) {
		t.Helper()
		out, errOut, err := bkRunCLI(t, stateDir, "backup", "status")
		if err == nil {
			t.Fatalf("%s: a dial fault that is not nobody-is-home must refuse, not succeed:\n%s%s", why, out, errOut)
		}
		if strings.Contains(errOut, backupDirectLine) {
			t.Fatalf("%s: the fallback line is the tell of a fallback that must not happen:\n%s", why, errOut)
		}
		if _, serr := os.Stat(storePath(stateDir)); !os.IsNotExist(serr) {
			t.Fatalf("%s: the direct door opened the store on a fault that refuses — a second writer the rule exists to prevent", why)
		}
	}

	// A dead daemon's leftover: the socket file stands, nobody listens.
	// A short state dir: the endpoint path must stay under the unix
	// socket's 108-byte cap, or bind dies with "invalid argument".
	t.Run("leftover-socket-refused", func(t *testing.T) {
		stateDir := bkShortStateDir(t)
		bkStaleSocket(t, sockPath(stateDir))
		refused(t, stateDir, "a leftover socket (ECONNREFUSED)")
	})

	// A socket that will not admit this peer: present, mode 0000.
	t.Run("endpoint-refuses-the-peer", func(t *testing.T) {
		stateDir := bkShortStateDir(t)
		bkStaleSocket(t, sockPath(stateDir))
		os.Chmod(sockPath(stateDir), 0)
		refused(t, stateDir, "a socket that refuses the peer (EACCES)")
	})

	// A socket that answers a different protocol: the handshake fails,
	// which is a connection fault, not an absence.
	t.Run("speaks-a-different-protocol", func(t *testing.T) {
		stateDir := bkShortStateDir(t) // bind needs the same 108-byte cap
		l, err := net.Listen("unix", sockPath(stateDir))
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		go func() {
			for {
				c, aerr := l.Accept()
				if aerr != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
					buf := make([]byte, 512)
					_, _ = c.Read(buf) // eat the initialize
					_, _ = c.Write([]byte("not jsonrpc at all"))
				}(c)
			}
		}()
		time.Sleep(100 * time.Millisecond) // the accept loop is live before the dial
		refused(t, stateDir, "a handshake that answers a different protocol")
	})
}

// bkMigrationCount reads the store's migration ledger straight off the
// file: the count the direct door must leave behind. The driver comes
// through internal/database — I2 keeps the mattn import in the two
// packages that own it; this file registers no driver of its own.
func bkMigrationCount(t *testing.T, dbPath string) int {
	t.Helper()
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestDirectDoorNeverMigrates holds the door's one hard contract against
// an old-vintage store: a store at 60 of the 79 migrations stays at 60
// after the direct door reads it (a door that migrates would trip the
// pre-migration hook — the hook that backs up the schema it is about to
// change — and would hand a backup a half-migrated database). The verbs
// that can answer without the newer tables answer honestly; the
// migration the store is missing is the store's business, not the
// door's.
func TestDirectDoorNeverMigrates(t *testing.T) {
	stateDir := t.TempDir()
	storeDir := filepath.Join(stateDir, "aeon")
	dbPath := filepath.Join(storeDir, "state.db")

	// A fully-migrated store, then the ledger knocked back to 60 of 79 —
	// an old binary's machine, mid-life, whose backup tables exist but
	// whose later migrations have not run.
	st, err := store.Open(store.Options{Dir: storeDir})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM _migrations WHERE name NOT IN
		(SELECT name FROM _migrations ORDER BY name LIMIT 60)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if n := bkMigrationCount(t, dbPath); n != 60 {
		t.Fatalf("the vintage must stand at 60 before the door sees it, not %d", n)
	}

	// The direct door over it, with nobody home: the verbs that can
	// answer do, and the ledger they leave is the one they found.
	out, errOut, err := bkRunCLI(t, stateDir, "backup", "list")
	if err != nil {
		t.Fatalf("list over an old-vintage store refused: %v\n%s%s", err, out, errOut)
	}
	if !strings.HasPrefix(out, "backups — 0 file(s) in ") {
		t.Fatalf("an old-vintage store with no backups lists none:\n%s", out)
	}
	out, _, err = bkRunCLI(t, stateDir, "backup", "status")
	if err != nil {
		t.Fatalf("status over an old-vintage store refused: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "backup  never — dir ") {
		t.Fatalf("a store with no backup row answers never:\n%s", out)
	}

	// The contract: the door read the store and left it exactly as it
	// found it — no migration, no pre-migration backup, no hook.
	if n := bkMigrationCount(t, dbPath); n != 60 {
		t.Fatalf("the direct door MIGRATED the store: %d -> %d (it must never migrate; the pre-migration hook would have backed up a schema it is about to change)", 60, n)
	}
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".aeonbak") {
			t.Fatalf("the pre-migration hook ran on the direct door's open: %s", e.Name())
		}
	}

	// The control: the runner's own Pending() agrees the store is old
	// — the 20 the vintage left out are exactly the ones the door
	// declined to run. (2026-11-03, S48 T9: 19→20 — the vintage cut
	// stands where S62 left it and the new proposal_blueprint_save
	// migration lands above it, so one more waits pending.)
	// (2026-11-03, S57 T10 sweep: 20→21 — same rule, one more above the
	// cut: S59 T2's 20261103063823_execution_environment, merged during
	// S57 T9; the goldens it drifted are regenerated in the same commit —
	// S62's header says a new migration forces that, any slice.) (A full open is not the control: it would apply
	// those 19 over a schema the earlier open already built, and this
	// vintage is a ledger view, store_test.go's own pattern — delete
	// the rows, and let Pending() say the rest.)
	st2, err := store.Open(store.Options{Dir: storeDir, SkipMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	pending, err := st2.Pending()
	if err != nil {
		t.Fatal(err)
	}
	// 2026-11-03 fix-forward: 21→23 — S58 T1's supervision migration and S49's
	// automation migration are pending for this old vintage like every later
	// file; the literal rides the count drift, the claim does not move.
	// 2026-11-03 (S59 T7): 23→24 — the environment_restrict kind's migration
	// (20261103181500) is pending for this vintage like every later file.
	// 2026-11-03 (S49 T6): 24→25 — the automation_disable kind's migration
	// (20261103193000) is pending for this vintage like every later file;
	// the S59 T10 sweep's rule again, same hour: 25→26 — S59 T10's
	// provision_owner_network (20261103203000) rides above it too.
	// 2026-11-04 (S49 T7 merge fix-forward): 26→27 — S50 T1's
	// roadmap_change (20261104012856) rides above it all; their commit
	// (b678e469) paid the bk goldens and the store counts and left these
	// two literals — main's red behind, this payment is the house law.
	// 2026-11-04 (S50 T5): 27→28 — S50's proposal_roadmap_idea
	// (20261104101500) rides above them all; the same law pays here again.
	// 2026-11-04 (S60 T2 merge): the merge_queue and
	// loop_worktree_integration files joined the later-than-vintage set,
	// 28 became 30.
	// 2026-11-04 (S60 T6 merge fix-forward): 30→31 — S51 T1's bug_change
	// (20261104115500, fd16d00c) paid its own bk goldens and store
	// counts and left this window literal — main's red behind (proven
	// red on the pristine pre-T6 tree), this payment is the house law,
	// the same one b678e469's drift was paid under above.
	// 2026-11-04 (S52 T1): 31→32 — S52's learning_management file
	// (20261104132302) lands behind this frozen window too, and the
	// genuinely-old store owes one more pending migration.
	// 2026-11-04 (S54 T1): 32→33 — the platform_change file
	// (20261104135327) lands behind this frozen window too; same law.
	// 2026-11-04 (S52 T4, paid in-merge with S54 T1): 33→34 —
	// proposal_learning_retract (20261104171000) rides the same window;
	// both same-day files land behind it, both count here.
	// 2026-11-04 (S60 T9 landing merge, paid IN THE MERGE): 34→35 —
	// proposal_kind_mergequeue (20261104193000), the seventeenth kind,
	// rides the same window; each side paid its own 33→34 on its own
	// branch, the merged truth is one payment.
	// 2026-11-04 (S63 T1, paid at S63 T5 when the full cmd suite ran):
	// 35→36 — the retro_facts file (20261104215735), the three fact
	// tables, rides the same window.
	// 2026-11-05 (S61 T1+T3, paid IN S63 T7's full cmd run): 36→38 —
	// goal_intake (20261105024130, f22afd72) and template_revision_system_actor
	// (20261105093000, dd799a33) both ride above this frozen vintage; S61 T3
	// paid its own applied-count literal (98) in this file and left this
	// window literal — main's red behind (proven red solo on this tree; the
	// S63 T7 diff is cmd-CLI-only and touches no migration), this payment is
	// the house law, the same one b678e469's drift and S63 T1's file were
	// paid under.
	// 2026-11-05 (S27 T2, paid IN THE MERGE of S63 T7): 38→40 —
	// scheduler (20261105025313) and loop_run_scheduler (20261105025314),
	// both 3c12e6fa, landed on main after T7's tree was cut; T7's 36→38
	// rode its own tree, the merged truth is 40 — pristine origin/main
	// (601f12c6) proven red solo: sees 40, want 36. Same law again.
	// 2026-11-06 (S73 T2, FOUND RED AT BASE and paid here): 40→41 —
	// proposal_kinds_intake (20261105110000, f5a7780c, S61 T5) rides above
	// this frozen window and the merge that carried it (0c6681b6) paid its
	// applied-count goldens without paying this window literal. Proven red
	// solo on a pristine HEAD worktree — `sees 41 pending, want 40` with
	// nothing of S73 on disk, and `git status` shows the migrations tree
	// untouched by S73 T2, which adds no migration at all. Same law.
	// 2026-11-06 (S73 T3): 41→42 — S73's own agent_change file
	// (20261106181435) lands behind this frozen window, so the genuinely-old
	// control store owes one more pending migration. S73 pays its own
	// house payment here rather than leaving it for the next merge.
	// 2026-11-08 (documents): 43→44 — document_owner (20261108120000) lands
	// behind this frozen window, so the genuinely-old control store owes one
	// more pending migration.
	// 2026-11-08 (variables): 44→45 — env_var (20261108130000) lands behind the window too.
	// 2026-11-08 (MCP env binding): 45→46 — mcp_env_from (20261108140000) lands behind the window too.
	// 2026-11-10 (blueprint override): 48→49 — blueprint_override (20261110120000) lands behind the window too.
	// 2026-11-09 (MCP title): 47→48 — mcp_title (20261109120000) lands behind the window too.
	// 2026-11-08 (MCP share): 46→47 — mcp_share (20261108150000) lands behind the window too.
	// 2026-11-15 (loop_access): 49→50 — loop_access (20261115120000) lands behind the window too.
	// 2026-11-16: 50→58 — the eight files after loop_access (event_journal, step_summary, loop_model_option,
	// loop_archive_delete, step_context, work_closure, route_audio_api, trigger_event_filter) land behind the window too.
	if len(pending) != 58 {
		t.Fatalf("the control sees %d pending, want 58 — the store must be genuinely old in the ledger's view, not a lie: %+v", len(pending), pending)
	}
}

// TestBackupJSONIsTheWireVerbatim holds --json to its promise: the bytes
// are the wire result, decoded back into the protocol's own types with
// the fields a person would want. A renderer that reshapes the wire on
// its way out is a second truth.
func TestBackupJSONIsTheWireVerbatim(t *testing.T) {
	stateDir := t.TempDir()
	backupDir := filepath.Join(stateDir, "aeon", "backups")

	if _, _, err := bkRunCLI(t, stateDir, "db", "info"); err != nil {
		t.Fatalf("the store must exist before the battery: %v", err)
	}
	if _, _, err := bkRunCLI(t, stateDir, "backup", "run"); err != nil {
		t.Fatalf("the pre-run failed: %v", err)
	}
	// The file name has second precision: a run in the same second
	// takes the same name and overwrites its predecessor, so the
	// battery's two runs stand one second apart by construction.
	time.Sleep(1100 * time.Millisecond)

	// run: the run's facts and the file as the list will show it.
	out, _, err := bkRunCLI(t, stateDir, "backup", "run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rr protocol.BackupRunResult
	if err := json.Unmarshal([]byte(out), &rr); err != nil {
		t.Fatalf("run --json does not decode as the wire's own type: %v\n%s", err, out)
	}
	if !rr.Run.Recorded || rr.Backup == nil || rr.Backup.Reason != "manual" {
		t.Fatalf("run --json lost the run's facts: %+v", rr)
	}

	// list: the files and the dirs, in the wire's names.
	out, _, err = bkRunCLI(t, stateDir, "backup", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var lr protocol.BackupListResult
	if err := json.Unmarshal([]byte(out), &lr); err != nil {
		t.Fatalf("list --json does not decode as the wire's own type: %v\n%s", err, out)
	}
	if len(lr.Backups) != 2 || len(lr.Dirs) != 1 || lr.Dirs[0] != backupDir {
		t.Fatalf("list --json lost the files or the dir: %+v", lr)
	}
	latest := ""
	for _, b := range lr.Backups { // newest first
		latest = b.FileName
		break
	}

	// verify: the verdict and the ledger's row, both the wire's.
	out, _, err = bkRunCLI(t, stateDir, "backup", "verify", latest, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var vr protocol.BackupVerifyResult
	if err := json.Unmarshal([]byte(out), &vr); err != nil {
		t.Fatalf("verify --json does not decode as the wire's own type: %v\n%s", err, out)
	}
	if !vr.Report.OK || vr.Report.Level != 2 || vr.Run.Outcome != "ok" {
		t.Fatalf("verify --json lost the verdict: %+v", vr)
	}

	// status: the machine's state and the database's own facts.
	out, _, err = bkRunCLI(t, stateDir, "backup", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var sv protocol.BackupStatusView
	if err := json.Unmarshal([]byte(out), &sv); err != nil {
		t.Fatalf("status --json does not decode as the wire's own type: %v\n%s", err, out)
	}
	// The applied count is the fresh store's own truth: every migration
	// (2026-11-03, carried by S57 T10's sweep: 80→81 with S59 T2's
	// execution_environment, same drift as the bk goldens; fix-forward
	// 2026-11-03: 81→83 — S58 T1's supervision file and S49's automation
	// file shipped after that sweep, same literal rides it; S59 T7 the same
	// day: 83→84 — the environment_restrict kind's migration; S49 T6 the
	// same drift again: 84→85 — the automation_disable kind's migration;
	// and the merge of the two same-hour files lands the union: 85→86 —
	// S59 T10's provision_owner_network rides above mine (…203000 above
	// …193000), and main still said 84 there, a red its own fix-forward
	// was still due; this merge carries the honest 86 for both).
	// 2026-11-04 (S49 T7 merge fix-forward): 86→87 — S50 T1's
	// roadmap_change (20261104012856, b678e469) paid its own bk goldens
	// and store counts; this wire literal rides the same drift and was
	// main's red behind until this merge.
	// 2026-11-04 (S51 T1): 90→91 — S51's bug_change file (20261104115500)
	// pays its own bk golden and store counts the same way.
	// 2026-11-04 (S52 T1): 91→92 — S52's learning_management file
	// (20261104132302) pays its own bk goldens and store counts; this
	// wire count rides with them.
	// 2026-11-04 (S54 T1): 92→93 — the platform_change file
	// (20261104135327) pays its own bk goldens and store counts; this
	// wire count rides with them. S52 T4's proposal_learning_retract
	// (20261104171000) made the same move from the other side of the same
	// day; that merge truth counted both, 94.
	// 2026-11-04 (S60 T9 landing merge, paid IN THE MERGE): 94→95 —
	// proposal_kind_mergequeue (20261104193000), the seventeenth kind,
	// pays the same set; the wire count rides with it.
	// 2026-11-04 (paid at S63 T5): 95→96 — retro_facts (20261104215735)
	// is one more applied file in the fresh store; the same bump was
	// carried independently in the S61 T0 record. 2026-11-05 (S61 T1):
	// goal_intake (20261105024130) is 97; the wire count rides with it.
	// 2026-11-05 (S27 T2, paid IN THE MERGE): the scheduler pair
	// (20261105025313, 20261105025314 — timestamps between goal_intake's
	// …024130 and S61 T3's …093000) is 98 and 99. 2026-11-05 (S61 T3):
	// template_revision_system_actor (20261105093000) is 100; the wire
	// count rides with it — this is the merged walk that counts all four
	// same-day files. 2026-11-05 (S61 T5): proposal_kinds_intake
	// (20261105110000) is 101 — the eighteenth and nineteenth kinds on the
	// proposal rebuild; the wire count rides with it, as every migration's.
	// 2026-11-06 (S73 T3): 101→102 — agent_change (20261106181435) is one more
	// applied file in a fresh store, and S73 pays its own wire count in the
	// change that adds it, beside the two bk goldens, the two store frozen
	// counts and this file's own pending-window literal above.
	// 2026-11-15 (event_journal): 110→111 — event_journal (20261115150000).
	// 2026-11-15 (loop_access): 109→110 — loop_access (20261115120000).
	// 2026-11-10 (blueprint override): 108→109 — blueprint_override (20261110120000).
	// 2026-11-09 (MCP title): 107→108 — mcp_title (20261109120000).
	// 2026-11-08 (MCP share): 106→107 — mcp_share (20261108150000).
	// 2026-11-08 (MCP env binding): 105→106 — mcp_env_from (20261108140000).
	// 2026-11-08 (variables): 104→105 — env_var (20261108130000).
	// 2026-11-08 (documents): 102→104 — loop_todo (20261107120000, 103, whose
	// landing left this literal unpaid) and document_owner (20261108120000).
	if sv.State != "fresh" || sv.Dir != backupDir || sv.DB.Applied != 118 {
		t.Fatalf("status --json lost the state or the database's facts: %+v", sv)
	}

	// prune: the plan, and the seatbelt — a bare call with --json keeps
	// the wire on stdout and speaks the seatbelt line on stderr.
	out, errOut, err := bkRunCLI(t, stateDir, "backup", "prune", "--json")
	if err != nil {
		t.Fatal(err)
	}
	// The direct door is the one serving here (no daemon), so the stderr
	// is the door line and the seatbelt — the JSON itself stays the plan.
	if errOut != backupDirectLine+"\n"+backupPruneSeatbeltLine+"\n" {
		t.Fatalf("the direct door's line and the bare-call seatbelt must be on stderr with --json: %q", errOut)
	}
	var pv protocol.BackupPruneView
	if err := json.Unmarshal([]byte(out), &pv); err != nil {
		t.Fatalf("prune --json does not decode as the wire's own type: %v\n%s", err, out)
	}
	if pv.Removed != nil || len(pv.Plan) == 0 {
		t.Fatalf("the bare call must plan without removing: %+v", pv)
	}

	// The human form is a rendering, not the wire's bytes.
	human, _, err := bkRunCLI(t, stateDir, "backup", "list")
	if err != nil {
		t.Fatal(err)
	}
	wire, _, err := bkRunCLI(t, stateDir, "backup", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if human == wire {
		t.Fatalf("the human form and the wire's bytes are identical — the renderer added nothing")
	}
}

// --- BK12's parity: the same verb sequence, the two doors, one shape ---

// The parity normaliser: the facts that are the DOOR's (the path, the
// file's identity, sizes, ages, digests, stamps) are tokenised; every
// other byte must be the same. A divergence anywhere else is a second
// rendering.
var (
	bkParityFile  = regexp.MustCompile(`aeon-state-\d{8}T\d{6}Z-v\d+\.aeonbak`)
	bkParityStamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`)
	bkParitySHA   = regexp.MustCompile(`[0-9a-f]{64}`)
	bkParityAge   = regexp.MustCompile(`\d+ s`)
	bkParitySize  = regexp.MustCompile(`\d+(\.\d+)? (?:bytes|KiB|MiB|GiB)`)
)

func bkParityNorm(out, stateDir string) string {
	out = strings.ReplaceAll(out, stateDir, "$STATE")
	out = bkParityFile.ReplaceAllString(out, "$FILE")
	out = bkParityStamp.ReplaceAllString(out, "$STAMP")
	out = bkParitySHA.ReplaceAllString(out, "$SHA")
	out = bkParityAge.ReplaceAllString(out, "$AGE s")
	out = bkParitySize.ReplaceAllString(out, "$SIZE")
	return out
}

// TestBackupVerbsIdenticalThroughBothDoors is BK12's parity at the
// binary level: the same pre-state (a migrated store, one manual
// backup), the same verb sequence — run, list, verify, list, status,
// prune, prune --dry-run — once with the daemon down (the direct door)
// and once with it up (the socket door). The bytes must hold the same
// shape, the door-specific facts tokenised, and each door must say
// which one it is (the direct line on stderr; the socket's silence).
func TestBackupVerbsIdenticalThroughBothDoors(t *testing.T) {
	dirA := t.TempDir() // the direct door's world: nobody is ever home
	dirB := t.TempDir() // the socket door's world: the daemon comes home after the pre-state

	// The pre-state, identical in both worlds, and the DIRECT door's
	// doing in both (no daemon in either yet): the store created and
	// migrated, one manual backup published and in the ledger. From
	// here the ledgers hold the same shape — one ok manual row, one
	// file — so the daemon's scheduler sees a fresh machine and runs
	// nothing at boot, and the two worlds' stores carry the same
	// schema (different rows: the daemon seeds its own).
	for _, dir := range []string{dirA, dirB} {
		if out, _, err := bkRunCLI(t, dir, "db", "info"); err != nil {
			t.Fatalf("db info (pre-state): %v\n%s", err, out)
		}
		if out, _, err := bkRunCLI(t, dir, "backup", "run"); err != nil {
			t.Fatalf("backup run (pre-state): %v\n%s", err, out)
		}
	}

	f := startDaemonInStateDir(t, dirB)
	bkWaitForBootSettles(t, f)

	runA := func(args ...string) (string, string, error) { return bkRunCLI(t, dirA, args...) }
	runB := func(args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.Command(aeonBinary, args...)
		cmd.Env = f.env()
		var out, errb bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errb
		err := cmd.Run()
		return out.String(), errb.String(), err
	}

	// The step's contract: both doors succeed, the direct door speaks
	// the fixed line, the socket door says nothing, and the normalised
	// bytes are one shape.
	step := func(name string, args ...string) (string, string) {
		t.Helper()
		outA, errA, eA := runA(args...)
		outB, errB, eB := runB(args...)
		if eA != nil || eB != nil {
			t.Fatalf("%s: a door failed: direct %v\n%s | socket %v\n%s", name, eA, outA, eB, outB)
		}
		if errA != backupDirectLine+"\n" {
			t.Fatalf("%s: the direct door's stderr is not the fixed line: %q", name, errA)
		}
		if errB != "" {
			t.Fatalf("%s: the socket door spoke on stderr where it should be silent: %q", name, errB)
		}
		na, nb := bkParityNorm(outA, dirA), bkParityNorm(outB, dirB)
		if na != nb {
			t.Fatalf("the %s step's bytes diverge between doors:\n--- direct ---\n%s\n--- socket ---\n%s", name, na, nb)
		}
		return outA, outB
	}

	// The newest file in each world's list: the first final-named
	// field, newest first.
	latestFile := func(out string) string {
		t.Helper()
		for _, line := range strings.Split(out, "\n") {
			fld := strings.Fields(line)
			if len(fld) > 0 && backup.IsFinalName(fld[0]) {
				return fld[0]
			}
		}
		t.Fatalf("no final-named file in the list:\n%s", out)
		return ""
	}

	// 1 — a second manual run (A over the direct door, B over the socket).
	step("run", "backup", "run")

	// 2 — the list: two files, both unverified.
	listA, listB := step("list", "backup", "list")
	fileA, fileB := latestFile(listA), latestFile(listB)
	if backup.IsFinalName(fileA) != backup.IsFinalName(fileB) {
		t.Fatalf("the worlds' newest files are not both final names: %q vs %q", fileA, fileB)
	}

	// 3 — verify the newest (level 2), each door over its own file.
	outA, errA, eA := runA("backup", "verify", fileA)
	outB, errB, eB := runB("backup", "verify", fileB)
	if eA != nil || eB != nil {
		t.Fatalf("verify: a door failed: direct %v | socket %v", eA, eB)
	}
	if errA != backupDirectLine+"\n" || errB != "" {
		t.Fatalf("verify: the doors' stderr diverged: %q vs %q", errA, errB)
	}
	if na, nb := bkParityNorm(outA, dirA), bkParityNorm(outB, dirB); na != nb {
		t.Fatalf("verify's bytes diverge:\n--- direct ---\n%s\n--- socket ---\n%s", na, nb)
	}

	// 4 — the list again: the newest now verified, the other still not.
	step("list-after-verify", "backup", "list")

	// 5 — the status: fresh, the last-verified stamp, the next due.
	step("status", "backup", "status")

	// 6 — the bare prune: the plan, the seatbelt, nothing moved.
	step("prune", "backup", "prune")

	// 7 — the prune --dry-run: the plan, nothing deleted.
	step("prune-dry-run", "backup", "prune", "--dry-run")

	// The tell of a parity that ran: each world's backups dir holds
	// exactly the two files its runs made — the bare prune moved
	// nothing, on either door.
	for _, dir := range []string{dirA, dirB} {
		entries, err := os.ReadDir(filepath.Join(dir, "aeon", "backups"))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if backup.IsFinalName(e.Name()) {
				n++
			}
		}
		if n != 2 {
			t.Fatalf("the bare prune must move nothing: %d final files in %s, want 2", n, dir)
		}
	}
}
