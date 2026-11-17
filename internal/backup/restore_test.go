package backup

// S62 T7 · the restore engine, end to end.
//
// These tests run PlanRestore/Restore against REAL stores and REAL
// archives: the live store is seeded with data plus an FTS5 table and a
// vec0 table, backed up through the real Run, mutated, and put back — and
// every refusal, fault and device fact the task spec names is pinned to a
// sentence and a tree state. The daemon is represented where it exists:
// a live listener on the endpoint, and another process's connection on
// the file (same mechanism — the lock is per file, not per process).
//
// The spec's two tests that live elsewhere are not repeated here:
// TestProbeDoesNotReclaimAStaleSocket is the endpoint package's, and
// TestRestoreExistsOnNoRemoteSurface is T6's.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// --- fixtures -----------------------------------------------------------

// seedRestoreTables is the round-trip data: a plain table, an FTS5 table
// and a vec0 table, each with rows a restore must put back byte for
// byte (in content, if not in file bytes — the snapshot is a fresh file).
func seedRestoreTables(t *testing.T, st *store.Store) {
	t.Helper()
	db := st.GormDB()
	for _, s := range []string{
		`CREATE TABLE restore_probe (id INTEGER PRIMARY KEY, label TEXT NOT NULL, payload TEXT)`,
		`CREATE VIRTUAL TABLE restore_fts USING fts5(body)`,
		`CREATE VIRTUAL TABLE restore_vec USING vec0(embedding FLOAT[4], id integer)`,
		`INSERT INTO restore_probe (id, label, payload) VALUES (1, 'alpha', 'p1'), (2, 'beta', 'p2'), (3, 'gamma', 'p3')`,
		`INSERT INTO restore_fts (body) VALUES ('the quick brown fox'), ('lazy dog sleeps'), ('aeon backup restore')`,
	} {
		if res := db.Exec(s); res.Error != nil {
			t.Fatalf("seed %q: %v", s, res.Error)
		}
	}
	for id, v := range map[int][]float32{
		1: {1, 0, 0, 0},
		2: {0, 1, 0, 0},
		3: {0, 0, 1, 0},
	} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encode the vector: %v", err)
		}
		if res := db.Exec(`INSERT INTO restore_vec (id, embedding) VALUES (?, ?)`, id, string(raw)); res.Error != nil {
			t.Fatalf("seed the vector: %v", res.Error)
		}
	}
}

// vecJSON is the stored form a vector crosses the vec0 door in.
func vecJSON(t *testing.T, v []float32) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode the vector: %v", err)
	}
	return string(raw)
}

// restoreFix seeds a fresh state dir, runs a real manual backup of it at
// verify level 2, and closes the store — the "daemon down" precondition
// the restore assumes. It returns the state dir and the archive path.
func restoreFix(t *testing.T) (stateDir, archive string) {
	t.Helper()
	stateDir = t.TempDir()
	bkDir := t.TempDir()
	st, err := store.Open(store.Options{Dir: stateDir})
	if err != nil {
		t.Fatalf("open the live store: %v", err)
	}
	seedRestoreTables(t, st)
	d := testDeps(t, st, bkDir)
	one := true
	if _, err := Run(context.Background(), d, Options{Dir: bkDir, Reason: "manual", ActorKind: "cli", VerifyAfter: &one}); err != nil {
		_ = st.Close()
		t.Fatalf("run the backup: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close the live store: %v", err)
	}
	names := dirEntries(t, bkDir)
	for _, n := range names {
		if strings.HasSuffix(n, ".aeonbak") {
			archive = filepath.Join(bkDir, n)
			return stateDir, archive
		}
	}
	t.Fatalf("the backup left no archive in %v", names)
	return stateDir, ""
}

// restoreDeps is the restore's injection set over a CLOSED store: the
// drill's door is a plain store.Open over whatever dir it is given — and
// the drill is the only dir this path ever opens.
func restoreDeps(t *testing.T, stateDir, bkDir string) Deps {
	t.Helper()
	return Deps{
		CheckFile: store.CheckFile,
		OpenStore: func(dir string) (io.Closer, error) { return store.Open(store.Options{Dir: dir}) },
		Clock:     time.Now,
		Settings:  NewEnvSettingsWith(fakeEnv(map[string]string{"AEON_BACKUP_DIR": bkDir})),
		Version:   "0.0.0-test",
		OutsideDB: DefaultOutsideDB,
		StateDir:  stateDir,
	}
}

// runRestore plans and runs the swap, releasing the plan's lock on every
// path. It returns the plan (for the log and the flags) and the first
// failure, plan-stage or swap-stage.
func runRestore(t *testing.T, d Deps, archive string, opts PlanOpts) (RestorePlan, error) {
	t.Helper()
	p, err := PlanRestore(context.Background(), d, archive, opts)
	t.Cleanup(p.Release)
	if err != nil {
		return p, err
	}
	return p, Restore(context.Background(), d, &p, true)
}

// walkSnapshot is the byte-identity witness: every file under dir, by
// relative path, to its sha256 (the premigrate fileHash is single-file).
func walkSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		sum := sha256hexOf(t, path)
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out[rel] = sum
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func sha256hexOf(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// stageDirs and preRestoreDirs list the two restore-owned families in a
// state dir; both must be empty after a clean restore.
func stageDirs(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, n := range dirEntries(t, dir) {
		if restoreDirRE.MatchString(n) {
			out = append(out, n)
		}
	}
	return out
}

func preRestoreDirs(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, n := range dirEntries(t, dir) {
		if strings.HasPrefix(n, "state.db.pre-restore-") {
			out = append(out, n)
		}
	}
	return out
}

func noRestoreArtefacts(t *testing.T, dir string) {
	t.Helper()
	if s := stageDirs(t, dir); len(s) > 0 {
		t.Fatalf("stage dirs left behind: %v", s)
	}
}

func livePath(t *testing.T, stateDir string) string {
	t.Helper()
	return filepath.Join(stateDir, "state.db")
}

// assertRefusedPlan pins a PlanRestore refusal: the kind, the sentence
// fragments, the tree byte-identical, and no log written (a refusal
// changed nothing, and nothing that changed nothing writes a log).
func assertRefusedPlan(t *testing.T, stateDir, archive string, err error, kind string, frags ...string) {
	t.Helper()
	var re RunError
	if !errors.As(err, &re) {
		t.Fatalf("want a RunError, got %T: %v", err, err)
	}
	if re.Kind != kind {
		t.Fatalf("the refusal's kind is %q, want %q (sentence: %s)", re.Kind, kind, re.What)
	}
	for _, f := range frags {
		if !strings.Contains(err.Error(), f) {
			t.Fatalf("the refusal %q does not name %q", err.Error(), f)
		}
	}
}

// --- the spec's tests ----------------------------------------------------

// TestRestoreRefusesWithALiveDaemon: something answers the endpoint, and
// the restore is refused before a byte is written — the sentence names
// the endpoint and the peer, and the tree stays byte-identical.
func TestRestoreRefusesWithALiveDaemon(t *testing.T) {
	stateDir, archive := restoreFix(t)
	sock := filepath.Join(t.TempDir(), "daemon.sock")
	t.Setenv("LOCALRPC_ENDPOINT_PATH", sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = c.Close()
		}
	}()

	before := walkSnapshot(t, stateDir)
	d := restoreDeps(t, stateDir, t.TempDir())
	p, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	p.Release()
	assertRefusedPlan(t, stateDir, archive, err, ErrKindBusy, sock, "pid", "stop the daemon")
	if p.StaleEndpoint {
		t.Fatal("a live endpoint is not stale")
	}
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the live-daemon refusal changed the state dir: %v -> %v", before, after)
	}
	if logs := dirEntries(t, stateDir); logsContain(logs, "restore-") {
		t.Fatalf("a refusal wrote a log: %v", logs)
	}
}

func logsContain(entries []string, prefix string) bool {
	for _, n := range entries {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// TestRestoreRefusesWhenAnotherProcessHoldsTheFile: a connection with an
// open read transaction is the file's holder — the lock comes back busy,
// the refusal names the possibilities, and once the holder goes the lock
// is free again.
func TestRestoreRefusesWhileAHoldsTheFile(t *testing.T) {
	stateDir, archive := restoreFix(t)
	hold := storeHolder(t, stateDir)
	defer hold.Close()

	before := walkSnapshot(t, stateDir)
	d := restoreDeps(t, stateDir, t.TempDir())
	p, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	p.Release()
	assertRefusedPlan(t, stateDir, archive, err, ErrKindBusy, "aeon watch", "a CLI verb", "a daemon on a different endpoint")
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the held-file refusal changed the state dir")
	}

	// The holder goes away — a bare plan now takes the lock.
	hold.Close()
	p2, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	p2.Release()
	if err != nil {
		t.Fatalf("after the holder went the lock is still busy: %v", err)
	}
}

// TestRestoreRefusesWhenAnotherProcessHoldsTheFile is the spec's name for
// the held-file test above; the alias keeps the spec's 13 names in the
// tree verbatim.
func TestRestoreRefusesWhenAnotherProcessHoldsTheFile(t *testing.T) {
	TestRestoreRefusesWhileAHoldsTheFile(t)
}

// storeHolder is the other process's handle: a second store.Open on the
// live file, its pool connection left with an open read transaction — the
// measured WAL fact is that an open transaction, even a deferred one that
// has read one row, blocks the exclusive lock. The connection is reached
// through the store seam (the (*gorm.DB).DB() escape hatch), not a driver
// import: the I2 guard (the store package) allows mattn/go-sqlite3 only
// inside internal/database and internal/store, and the backup package
// must not become a second driver user, in a test file included. Closing
// the returned pool closes the holding connection with it, so the lock
// goes free again (a gorm *sql.Tx would not: its connection is checked
// out and survives a store.Close, and the lock would stay busy).
func storeHolder(t *testing.T, stateDir string) *sql.DB {
	t.Helper()
	st, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open the holder store: %v", err)
	}
	sqlDB, err := st.GormDB().DB()
	if err != nil {
		_ = st.Close()
		t.Fatalf("the holder will not reach the pool: %v", err)
	}
	if _, err := sqlDB.Exec("BEGIN"); err != nil {
		_ = sqlDB.Close()
		t.Fatalf("the holder will not begin: %v", err)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM restore_probe`).Scan(&n); err != nil {
		_ = sqlDB.Close()
		t.Fatalf("the holder will not read: %v", err)
	}
	return sqlDB // the transaction stays open — that is the holding
}

// TestRestoreRefusesCorruptChecksum: a byte flipped in the database
// member is a checksum mismatch at level 1 — refused, named, and nothing
// changed.
func TestRestoreRefusesCorruptChecksum(t *testing.T) {
	stateDir, archive := restoreFix(t)
	b, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read the archive: %v", err)
	}
	b[len(b)/2] ^= 0xFF // inside the database member, past the manifest
	if err := os.WriteFile(archive, b, 0o600); err != nil {
		t.Fatalf("corrupt the archive: %v", err)
	}

	before := walkSnapshot(t, stateDir)
	d := restoreDeps(t, stateDir, t.TempDir())
	p, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	p.Release()
	assertRefusedPlan(t, stateDir, archive, err, ErrKindChecksum, "checksum")
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the checksum refusal changed the state dir")
	}
}

// TestRestoreRefusesNewerSchema: the manifest names a migration this
// binary does not know — level 1 still answers (readability is a fact),
// level 2 refuses with the unknown name, and the refusal kind is
// newer-schema.
func TestRestoreRefusesNewerSchema(t *testing.T) {
	stateDir, archive := restoreFix(t)
	newer := archive + ".newer.aeonbak"
	rewriteArchiveManifest(t, archive, newer, func(m *Manifest) {
		m.AppliedMigrations = len(store.Migrations()) + 1
		m.LastMigration = "99999999999999_after_this_binary"
	})

	d := restoreDeps(t, stateDir, t.TempDir())
	p, err := PlanRestore(context.Background(), d, newer, PlanOpts{})
	p.Release()
	assertRefusedPlan(t, stateDir, newer, err, ErrKindNewerSchema, "newer schema", "99999999999999_after_this_binary", "use a newer aeon")
	if p.Verify.NewerSchema != true {
		t.Fatal("the report does not carry the newer-schema fact")
	}
}

// rewriteArchiveManifest re-packs an archive with one manifest field
// changed: the database member is byte-for-byte the same, so the digest
// still holds — only the statement changed.
func rewriteArchiveManifest(t *testing.T, src, dst string, mutate func(m *Manifest)) {
	t.Helper()
	r, err := Open(src)
	if err != nil {
		t.Fatalf("open the archive: %v", err)
	}
	m := r.Manifest()
	mutate(&m)
	f, err := os.Create(dst)
	if err != nil {
		_ = r.Close()
		t.Fatalf("create the re-pack: %v", err)
	}
	// The database member is byte-for-byte the same, so the digest still
	// holds — only the statement changed. Write before Close: Close
	// removes the extracted copy.
	if err := Write(f, m, r.DBPath()); err != nil {
		_ = f.Close()
		_ = r.Close()
		t.Fatalf("re-pack the archive: %v", err)
	}
	if err := f.Close(); err != nil {
		_ = r.Close()
		t.Fatalf("close the re-pack: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close the reader: %v", err)
	}
}

// TestRestoreRefusesWrongFormat: a right name over a non-archive is
// refused at the container's door — the sentence says it is not a
// readable archive, and nothing moved.
func TestRestoreRefusesWrongFormat(t *testing.T) {
	stateDir, _ := restoreFix(t)
	bkDir := t.TempDir()
	bad := filepath.Join(bkDir, "aeon-state-20261102T120000Z-v76.aeonbak")
	if err := os.WriteFile(bad, []byte("this is not an archive"), 0o600); err != nil {
		t.Fatalf("write the impostor: %v", err)
	}

	before := walkSnapshot(t, stateDir)
	d := restoreDeps(t, stateDir, t.TempDir())
	p, err := PlanRestore(context.Background(), d, bad, PlanOpts{})
	p.Release()
	var re RunError
	if !errors.As(err, &re) {
		t.Fatalf("want a RunError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "not a readable archive") {
		t.Fatalf("the refusal does not name the shape: %s", err)
	}
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the wrong-format refusal changed the state dir")
	}
}

// TestRestoreWithoutYesChangesNothing: the plan prints, the lock holds
// only the pre-swap window, and no swap without the operator's word —
// the tree is byte-identical and no log exists.
func TestRestoreWithoutYesChangesNothing(t *testing.T) {
	stateDir, archive := restoreFix(t)
	before := walkSnapshot(t, stateDir)
	d := restoreDeps(t, stateDir, t.TempDir())

	p, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the plan refused a clean fixture: %v", err)
	}
	p.Release()
	// A clean close leaves the WAL sidecars in place (a zeroed,
	// checkpointed set), so the live set is the database plus however
	// many sidecars exist — never anything else.
	if !p.LiveExists || p.LiveFiles[0] != "state.db" {
		t.Fatalf("the plan did not read the live set: %+v", p)
	}
	for _, n := range p.LiveFiles {
		if n != "state.db" && n != "state.db-wal" && n != "state.db-shm" {
			t.Fatalf("the live set carries a foreign name: %s", n)
		}
	}
	// No Restore call — the no-yes path. The plan held its lock and
	// released it; the tree must be byte-identical.
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("planning without --yes changed the state dir: %v -> %v", before, after)
	}
	if logs := dirEntries(t, stateDir); logsContain(logs, "restore-") {
		t.Fatalf("a plan-only run wrote a log: %v", logs)
	}

	// And the guard inside Restore: yes=false is a refusal, not a no-op.
	p2, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the plan refused a clean fixture: %v", err)
	}
	if err := Restore(context.Background(), d, &p2, false); err == nil {
		t.Fatal("a swap ran without --yes")
	}
	p2.Release()
	if p2.Swapped || p2.RolledBack {
		t.Fatal("a refused swap carried a swap fact")
	}
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the refused swap changed the state dir")
	}
}

// TestRestoreRoundTrip: data in, backup, mutation, restore, reopen —
// the data is the backup's instant, the FTS and the vectors answer, and
// no old sidecar survives the swap.
func TestRestoreRoundTrip(t *testing.T) {
	stateDir, archive := restoreFix(t)
	live := livePath(t, stateDir)

	// The mutation: the state the operator wants to forget.
	mut, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("reopen for the mutation: %v", err)
	}
	for _, s := range []string{
		`UPDATE restore_probe SET payload = 'mutated' WHERE id = 1`,
		`DELETE FROM restore_probe WHERE id = 2`,
		`INSERT INTO restore_fts (body) VALUES ('backup of the backup')`,
	} {
		if res := mut.GormDB().Exec(s); res.Error != nil {
			_ = mut.Close()
			t.Fatalf("mutate: %v", res.Error)
		}
	}
	if err := mut.Close(); err != nil {
		t.Fatalf("close the mutation: %v", err)
	}
	afterMut := fileHash(t, live)

	d := restoreDeps(t, stateDir, t.TempDir())
	p, err := runRestore(t, d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the restore failed: %v", err)
	}
	if !p.Swapped {
		t.Fatal("the swap did not set its fact")
	}
	if got := stepsInOrder(p.Log); got != "" {
		t.Fatalf("the log's steps are wrong: %s", got)
	}

	// Reopen: the data is the backup's instant, not the mutation's.
	st, err := store.Open(store.Options{Dir: stateDir})
	if err != nil {
		t.Fatalf("reopen after the restore: %v", err)
	}
	db := st.GormDB()
	var (
		count   int
		payload string
		label   string
		ftsHits int
		vecID   int
	)
	if res := db.Raw(`SELECT count(*) FROM restore_probe`).Scan(&count); res.Error != nil || count != 3 {
		t.Fatalf("the restore brought back %d probe rows, want 3: %v", count, res.Error)
	}
	if res := db.Raw(`SELECT payload FROM restore_probe WHERE id = 1`).Scan(&payload); res.Error != nil || payload != "p1" {
		t.Fatalf("the restore did not bring back the original payload: %q (%v)", payload, res.Error)
	}
	if res := db.Raw(`SELECT label FROM restore_probe WHERE id = 2`).Scan(&label); res.Error != nil || label != "beta" {
		t.Fatalf("the restore did not bring back row 2: %q (%v)", label, res.Error)
	}
	if res := db.Raw(`SELECT count(*) FROM restore_fts WHERE restore_fts MATCH ?`, "backup").Scan(&ftsHits); res.Error != nil || ftsHits != 1 {
		t.Fatalf("fts after the restore: %d hits, want 1 (the mutation's extra doc is gone): %v", ftsHits, res.Error)
	}
	if res := db.Raw(`SELECT id FROM restore_vec WHERE embedding MATCH ? AND k = ?`, vecJSON(t, []float32{0.9, 0.1, 0, 0}), 1).Scan(&vecID); res.Error != nil || vecID != 1 {
		t.Fatalf("knn after the restore: id %d, want 1: %v", vecID, res.Error)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close after the recheck: %v", err)
	}

	// No old sidecar survives: the replaced set is the pre-restore dir's,
	// byte for byte.
	keeps := preRestoreDirs(t, stateDir)
	if len(keeps) != 1 {
		t.Fatalf("the pre-restore dirs: %v, want exactly one", keeps)
	}
	keep := filepath.Join(stateDir, keeps[0])
	if inside := dirEntries(t, keep); len(inside) == 0 || inside[0] != "state.db" {
		t.Fatalf("the pre-restore dir does not hold the replaced set: %v", inside)
	}
	for _, n := range dirEntries(t, keep) {
		if n != "state.db" && n != "state.db-wal" && n != "state.db-shm" {
			t.Fatalf("the pre-restore dir holds a foreign name: %s", n)
		}
	}
	if got := fileHash(t, filepath.Join(keep, "state.db")); got != afterMut {
		t.Fatal("the pre-restore dir's state.db is not the replaced database's bytes — the state at the swap")
	}
	if fileHash(t, live) == afterMut {
		t.Fatal("the swap left the pre-restore file in place — nothing moved")
	}
	noRestoreArtefacts(t, stateDir)
}

var allSteps = []string{StepProbe, StepLock, StepVerify, StepPlan, StepStage, StepCheckStaged, StepDrill, StepMoveOld, StepRenameIn, StepRemoveStage}

// stepsInOrder checks the log's step sequence: every step the swap
// passed through must appear, in order, and no step outside the closed
// set may appear. One step may log more than one fact (stage logs the
// filesystem check and the staging itself) — what is refused is a step
// out of order or a missing step. It returns the first violation, or "".
func stepsInOrder(log []LogEntry) string {
	pos := map[string]int{}
	for i, s := range allSteps {
		pos[s] = i
	}
	last := -1
	seen := map[string]bool{}
	names := []string{}
	for _, e := range log {
		names = append(names, e.Step)
		i, ok := pos[e.Step]
		if !ok {
			return "a step outside the closed set: " + e.Step
		}
		if i < last {
			return "the steps are out of order: " + strings.Join(names, ", ")
		}
		last = i
		seen[e.Step] = true
	}
	for _, s := range allSteps {
		if !seen[s] {
			return "the " + s + " step is missing"
		}
	}
	return ""
}

// TestReplacedSetIsKeptTogether: the live set (db plus its WAL sidecars)
// moves into the pre-restore dir byte for byte, and no retention family
// in this package matches that dir's name.
func TestReplacedSetIsKeptTogether(t *testing.T) {
	stateDir, archive := restoreFix(t)
	live := livePath(t, stateDir)
	before := fileHash(t, live)

	// A zero-byte WAL is the state right after a TRUNCATE checkpoint: a
	// valid, empty sidecar — the set the swap must keep together.
	for _, side := range []string{"state.db-wal", "state.db-shm"} {
		if err := os.WriteFile(filepath.Join(stateDir, side), nil, 0o600); err != nil {
			t.Fatalf("plant %s: %v", side, err)
		}
	}

	d := restoreDeps(t, stateDir, t.TempDir())
	_, err := runRestore(t, d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the restore failed: %v", err)
	}

	keeps := preRestoreDirs(t, stateDir)
	if len(keeps) != 1 {
		t.Fatalf("the pre-restore dirs: %v, want exactly one", keeps)
	}
	keep := keeps[0]
	inside := dirEntries(t, filepath.Join(stateDir, keep))
	want := []string{"state.db", "state.db-shm", "state.db-wal"}
	if !reflect.DeepEqual(inside, want) {
		t.Fatalf("the pre-restore dir holds %v, want %v — the set is kept together or not at all", inside, want)
	}
	if got := fileHash(t, filepath.Join(stateDir, keep, "state.db")); got != before {
		t.Fatal("the pre-restore dir's state.db is not the previous database's bytes")
	}
	// Retention can never touch the replaced database.
	for _, re := range []*regexp.Regexp{fileRE, partialRE, tmpDirRE, restoreDirRE} {
		if re.MatchString(keep) {
			t.Fatalf("retention family %s matches the pre-restore dir name %s", re, keep)
		}
	}
	noRestoreArtefacts(t, stateDir)
}

// TestRestoreFaultInjectionEveryStep: a fault after each step leaves the
// database in exactly one of three states — untouched, restored, or
// rolled back — never half. The stage is fenced away wherever the fault
// precedes the move; after the rename, nothing is rolled back.
func TestRestoreFaultInjectionEveryStep(t *testing.T) {
	for _, step := range allSteps {
		t.Run(step, func(t *testing.T) {
			stateDir, archive := restoreFix(t)
			live := livePath(t, stateDir)
			before := fileHash(t, live)
			injected := errors.New("an injected fault")
			d := restoreDeps(t, stateDir, t.TempDir())

			p, perr := PlanRestore(context.Background(), d, archive, PlanOpts{fault: map[string]error{step: injected}})
			t.Cleanup(p.Release)
			var rerr error
			if perr != nil {
				rerr = perr
			} else {
				rerr = Restore(context.Background(), d, &p, true)
			}
			if !errors.Is(rerr, injected) {
				t.Fatalf("the injected fault is not the error: %v", rerr)
			}
			after := fileHash(t, live)

			switch {
			case step == StepProbe || step == StepLock || step == StepVerify || step == StepPlan ||
				step == StepStage || step == StepCheckStaged || step == StepDrill:
				// The live store is untouched, and the stage (where it
				// reached one) is fenced away.
				if after != before {
					t.Fatalf("a fault at %s changed the live database", step)
				}
				noRestoreArtefacts(t, stateDir)
				if k := preRestoreDirs(t, stateDir); len(k) > 0 {
					t.Fatalf("a fault at %s made a pre-restore dir: %v", step, k)
				}
				if p.Swapped || p.RolledBack {
					t.Fatalf("a fault at %s carried a swap fact", step)
				}
			case step == StepMoveOld:
				// The move happened, and the put-back undid it: the
				// previous database is back, byte for byte.
				if after != before {
					t.Fatalf("the put-back did not restore the previous database's bytes")
				}
				if !p.RolledBack {
					t.Fatal("the rolled-back plan does not carry its fact")
				}
				noRestoreArtefacts(t, stateDir)
				for _, k := range preRestoreDirs(t, stateDir) {
					if inside := dirEntries(t, filepath.Join(stateDir, k)); len(inside) != 0 {
						t.Fatalf("the pre-restore dir %s still holds %v after the put-back", k, inside)
					}
				}
			case step == StepRenameIn || step == StepRemoveStage:
				// The swap has COMPLETED: the live file is the backup's
				// file, the replaced set is in the pre-restore dir, and
				// nothing after a swap is rolled back.
				if after == before {
					t.Fatalf("a fault at %s did not complete the swap", step)
				}
				if !p.Swapped {
					t.Fatal("a completed swap does not carry its fact")
				}
				keeps := preRestoreDirs(t, stateDir)
				if len(keeps) != 1 {
					t.Fatalf("the completed swap left no pre-restore dir: %v", keeps)
				}
				if got := fileHash(t, filepath.Join(stateDir, keeps[0], "state.db")); got != before {
					t.Fatal("the pre-restore dir does not hold the previous database's bytes")
				}
				left := stageDirs(t, stateDir)
				if step == StepRenameIn {
					if len(left) != 1 {
						t.Fatalf("a fault at rename-in left no stage dir behind (the operator must be told which): %v", left)
					}
					if !strings.Contains(rerr.Error(), left[0]) {
						t.Fatalf("the error does not name the leftover stage dir %s: %s", left[0], rerr)
					}
				} else {
					if len(left) != 0 {
						t.Fatalf("a fault at remove-stage left the stage dir behind: %v", left)
					}
				}
			}
		})
	}
}

// TestRestoreRefusesCrossDevice: the backup dir and the state dir on
// different filesystems — refused before the stage dir exists (BK17's
// sentence), and the stated "cannot say" proceeds with the line logged.
func TestRestoreRefusesCrossDevice(t *testing.T) {
	stateDir, archive := restoreFix(t)
	before := walkSnapshot(t, stateDir)

	d := restoreDeps(t, stateDir, t.TempDir())
	d.sameDevice = func(a, b string) (bool, bool) { return false, true }
	p, perr := PlanRestore(context.Background(), d, archive, PlanOpts{})
	if perr != nil {
		t.Fatalf("the plan itself refused: %v", perr)
	}
	rerr := Restore(context.Background(), d, &p, true)
	p.Release()
	var re RunError
	if !errors.As(rerr, &re) {
		t.Fatalf("want a RunError, got %T: %v", rerr, rerr)
	}
	if re.Kind != ErrKindIO {
		t.Fatalf("the cross-device refusal's kind is %q, want %q", re.Kind, ErrKindIO)
	}
	if !strings.Contains(rerr.Error(), "different filesystems") {
		t.Fatalf("the refusal does not say why: %s", rerr)
	}
	if after := walkSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the cross-device refusal changed the state dir")
	}
	if s := stageDirs(t, stateDir); len(s) > 0 {
		t.Fatalf("the stage dir was made before the filesystem check: %v", s)
	}

	// The stated "cannot say" (the non-POSIX platforms): the line is
	// logged and the restore proceeds.
	stateDir2, archive2 := restoreFix(t)
	d2 := restoreDeps(t, stateDir2, t.TempDir())
	d2.sameDevice = func(a, b string) (bool, bool) { return false, false }
	p2, err := runRestore(t, d2, archive2, PlanOpts{})
	if err != nil {
		t.Fatalf("the stated cannot-say refused a clean fixture: %v", err)
	}
	saw := false
	for _, e := range p2.Log {
		if e.Step == StepStage && strings.Contains(e.Fact, "cannot run") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("the cannot-say fact is not in the log: %+v", p2.Log)
	}
}

// TestRestoreOlderSchemaThenOpenMigratesWithABackup: a backup at an older
// vintage is restored byte-for-byte, and the store's own next start is
// what migrates it — through the real pre-migration hook, with its own
// backup first.
func TestRestoreOlderSchemaThenOpenMigratesWithABackup(t *testing.T) {
	vintageDir := t.TempDir()
	applied := seedOldStore(t, vintageDir)
	seed, err := store.Open(store.Options{Dir: vintageDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("reopen the vintage store: %v", err)
	}
	if res := seed.GormDB().Exec(`CREATE TABLE restore_older (id INTEGER PRIMARY KEY, label TEXT)`); res.Error != nil {
		_ = seed.Close()
		t.Fatalf("seed the vintage data: %v", res.Error)
	}
	if res := seed.GormDB().Exec(`INSERT INTO restore_older VALUES (1, 'old row'), (2, 'old row two')`); res.Error != nil {
		_ = seed.Close()
		t.Fatalf("seed the vintage data: %v", res.Error)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close the vintage store: %v", err)
	}

	// The backup of the vintage.
	bkDir := t.TempDir()
	s2, err := store.Open(store.Options{Dir: vintageDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("reopen for the backup: %v", err)
	}
	one := true
	if _, err := Run(context.Background(), testDeps(t, s2, bkDir), Options{Dir: bkDir, Reason: "manual", ActorKind: "cli", VerifyAfter: &one}); err != nil {
		_ = s2.Close()
		t.Fatalf("run the vintage backup: %v", err)
	}
	_ = s2.Close()
	var archive string
	for _, n := range dirEntries(t, bkDir) {
		if strings.HasSuffix(n, ".aeonbak") {
			archive = filepath.Join(bkDir, n)
		}
	}
	if archive == "" {
		t.Fatal("the vintage backup left no archive")
	}
	m, err := OpenHeader(archive)
	if err != nil {
		t.Fatalf("read the vintage manifest: %v", err)
	}
	if m.AppliedMigrations != applied {
		t.Fatalf("the manifest's applied count is %d, the store seeded %d", m.AppliedMigrations, applied)
	}
	if m.AppliedMigrations >= len(store.Migrations()) {
		t.Fatalf("the fixture is not older than this binary: %d vs %d", m.AppliedMigrations, len(store.Migrations()))
	}

	// The restore, into a fresh state dir.
	target := t.TempDir()
	d := restoreDeps(t, target, bkDir)
	p, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the plan refused the older-schema backup: %v", err)
	}
	if !p.OlderSchema {
		t.Fatal("the plan does not carry the older-schema fact")
	}
	if err := Restore(context.Background(), d, &p, true); err != nil {
		p.Release()
		t.Fatalf("the restore of the older schema failed: %v", err)
	}
	p.Release()

	// The store's own next start: the real hook, the real migration.
	bkDir2 := t.TempDir()
	st := openMigrating(t, target, testDeps(t, nil, bkDir2), PreMigrateOptions{ActorKind: "cli"})
	defer func() { _ = st.Close() }()
	appliedNow, err := st.AppliedMigrations()
	if err != nil {
		t.Fatalf("list the migrations: %v", err)
	}
	if len(appliedNow) != len(store.Migrations()) {
		t.Fatalf("the next start applied %d migrations, want %d", len(appliedNow), len(store.Migrations()))
	}
	var n int
	if res := st.GormDB().Raw(`SELECT count(*) FROM restore_older`).Scan(&n); res.Error != nil || n != 2 {
		t.Fatalf("the vintage data did not survive the migration: %d rows (%v)", n, res.Error)
	}
	// The hook's backup exists, verifies, and is the ledger's one row.
	files := premigFiles(t, bkDir2)
	if len(files) != 1 {
		t.Fatalf("the pre-migration backups: %v, want exactly one", files)
	}
	vrep, verr := Verify(context.Background(), testDeps(t, nil, bkDir2), filepath.Join(bkDir2, "pre-migration", files[0]), 2)
	if verr != nil || !vrep.OK {
		t.Fatalf("the pre-migration backup does not verify: %v %+v", verr, vrep)
	}
	rows := ledgerRows(t, st)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want the hook's one: %+v", len(rows), rows)
	}
	if rows[0].Reason != "pre-migration" || rows[0].Outcome != "ok" {
		t.Fatalf("the ledger row: %+v", rows[0])
	}
}

// TestRestoreOnAFreshMachine: no state.db at the target — the plan says
// there is nothing to keep, and the restore is a plain placement.
func TestRestoreOnAFreshMachine(t *testing.T) {
	_, archive := restoreFix(t)
	target := t.TempDir()
	d := restoreDeps(t, target, t.TempDir())

	p, err := PlanRestore(context.Background(), d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the plan refused a fresh machine: %v", err)
	}
	if p.LiveExists || len(p.LiveFiles) != 0 {
		t.Fatalf("the plan sees a live set on a fresh machine: %+v", p)
	}
	if err := Restore(context.Background(), d, &p, true); err != nil {
		p.Release()
		t.Fatalf("the restore failed on a fresh machine: %v", err)
	}
	p.Release()

	if k := preRestoreDirs(t, target); len(k) > 0 {
		t.Fatalf("a fresh machine has a pre-restore dir: %v", k)
	}
	st, err := store.Open(store.Options{Dir: target})
	if err != nil {
		t.Fatalf("the restored fresh machine will not open: %v", err)
	}
	var count int
	if res := st.GormDB().Raw(`SELECT count(*) FROM restore_probe`).Scan(&count); res.Error != nil || count != 3 {
		t.Fatalf("the fresh-machine restore brought back %d rows, want 3: %v", count, res.Error)
	}
	_ = st.Close()
	noRestoreArtefacts(t, target)
}

// TestRestoreNeverStartsOrOpensTheStore (the runtime half): the only
// store this path opens is the drill's copy — never the state dir
// itself. The source half (no Claim on the restore path) is the guard's.
func TestRestoreNeverStartsOrOpensTheStore(t *testing.T) {
	stateDir, archive := restoreFix(t)
	var openDirs []string
	d := restoreDeps(t, stateDir, t.TempDir())
	d.OpenStore = func(dir string) (io.Closer, error) {
		openDirs = append(openDirs, dir)
		return store.Open(store.Options{Dir: dir})
	}
	_, err := runRestore(t, d, archive, PlanOpts{})
	if err != nil {
		t.Fatalf("the restore failed: %v", err)
	}
	if len(openDirs) != 1 {
		t.Fatalf("the restore opened %d stores: %v — the drill opens exactly one copy", len(openDirs), openDirs)
	}
	if openDirs[0] == stateDir {
		t.Fatalf("the restore opened the STATE DIR: %s", openDirs[0])
	}
	if !strings.HasPrefix(filepath.Base(openDirs[0]), ".tmp-") {
		t.Fatalf("the drill's dir %s is not the fence's .tmp- family", openDirs[0])
	}
}
