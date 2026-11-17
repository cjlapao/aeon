package main

// S62 T7 · the CLI door for the restore: `aeon backup restore FILE`.
//
// The state dir is resolved the way the CLI resolves it — XDG_STATE_HOME
// at a throwaway absolute dir, so the tests never point at the real store
// (§5, made executable). The daemon is represented by a live listener on
// the endpoint; the archive is a real backup of a real seeded store.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/store"
)

// cliRestoreFixture points XDG_STATE_HOME at a throwaway dir, opens the
// store there, seeds data, runs a real backup, and closes the store.
func cliRestoreFixture(t *testing.T) (stateDir, archive string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	stateDir = filepath.Join(base, "aeon")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("make the state dir: %v", err)
	}
	st, err := store.Open(store.Options{Dir: stateDir})
	if err != nil {
		t.Fatalf("open the fixture store: %v", err)
	}
	for _, s := range []string{
		`CREATE TABLE restore_probe (id INTEGER PRIMARY KEY, label TEXT NOT NULL, payload TEXT)`,
		`INSERT INTO restore_probe (id, label, payload) VALUES (1, 'alpha', 'p1'), (2, 'beta', 'p2')`,
	} {
		if res := st.GormDB().Exec(s); res.Error != nil {
			_ = st.Close()
			t.Fatalf("seed the fixture: %v", res.Error)
		}
	}
	bkDir := t.TempDir()
	env := func(k string) string {
		if k == "AEON_BACKUP_DIR" {
			return bkDir
		}
		return os.Getenv(k)
	}
	d := backup.Deps{
		Store:     st,
		CheckFile: store.CheckFile,
		Clock:     time.Now,
		StateDir:  stateDir,
		Version:   Version,
		OutsideDB: backup.DefaultOutsideDB,
		Settings:  backup.NewEnvSettingsWith(env),
	}
	one := true
	if _, err := backup.Run(context.Background(), d, backup.Options{Dir: bkDir, Reason: "manual", ActorKind: "cli", VerifyAfter: &one}); err != nil {
		_ = st.Close()
		t.Fatalf("run the fixture backup: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close the fixture store: %v", err)
	}
	entries, err := os.ReadDir(bkDir)
	if err != nil {
		t.Fatalf("list the backup dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".aeonbak") {
			return stateDir, filepath.Join(bkDir, e.Name())
		}
	}
	t.Fatalf("the fixture backup left no archive")
	return stateDir, ""
}

// cliSnapshot is the byte-identity witness: every file under dir, by
// relative path, to its sha256.
func cliSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(b)
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// restoreLogs lists the restore-*.log files a run left in the state dir.
func restoreLogs(t *testing.T, stateDir string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("list the state dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "restore-") && strings.HasSuffix(e.Name(), ".log") {
			out = append(out, e.Name())
		}
	}
	return out
}

func noStageDirs(t *testing.T, stateDir string) {
	t.Helper()
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("list the state dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".restore-") {
			t.Fatalf("a stage dir survived: %s", e.Name())
		}
	}
}

// The plan is printed and nothing moves: the stop without --yes is a
// usage error (exit 2 in the real door), the tree stays byte-identical,
// and a run that changed nothing writes no log.
func TestCmdBackupRestoreWithoutYesPrintsPlanAndChangesNothing(t *testing.T) {
	stateDir, archive := cliRestoreFixture(t)
	before := cliSnapshot(t, stateDir)

	var out bytes.Buffer
	err := cmdBackup(&out, []string{"restore", archive})
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("want the usageError that stops the swap, got %T: %v", err, err)
	}
	if after := cliSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the plan-only run changed the state dir: %v -> %v", before, after)
	}
	if logs := restoreLogs(t, stateDir); len(logs) != 0 {
		t.Fatalf("a run that changed nothing wrote a log: %v", logs)
	}
	s := out.String()
	for _, frag := range []string{"restore plan", "the plan is printed and the swap is not run", "--yes"} {
		if !strings.Contains(s, frag) {
			t.Fatalf("the plan output lacks %q:\n%s", frag, s)
		}
	}
}

// A live daemon answers the endpoint, and the restore refuses before a
// byte is written: the sentence names the endpoint and the peer, and the
// tree stays byte-identical.
func TestCmdBackupRestoreRefusesWithALiveDaemon(t *testing.T) {
	stateDir, archive := cliRestoreFixture(t)
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

	before := cliSnapshot(t, stateDir)
	var out bytes.Buffer
	err = cmdBackup(&out, []string{"restore", archive, "--yes"})
	if err == nil {
		t.Fatal("the restore ran under a live daemon")
	}
	if !strings.Contains(err.Error(), sock) {
		t.Fatalf("the refusal does not name the endpoint: %s", err)
	}
	if !strings.Contains(err.Error(), "pid") {
		t.Fatalf("the refusal does not name the peer: %s", err)
	}
	if after := cliSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the live-daemon refusal changed the state dir")
	}
	if logs := restoreLogs(t, stateDir); len(logs) != 0 {
		t.Fatalf("a refusal wrote a log: %v", logs)
	}
	noStageDirs(t, stateDir)
}

// --yes runs the swap: the data is the backup's instant, exactly one log
// exists beside the pre-restore dir, the log carries no / at all, and it
// names every step the swap passed through.
func TestCmdBackupRestoreYesRunsTheSwapAndWritesTheLog(t *testing.T) {
	stateDir, archive := cliRestoreFixture(t)

	// The mutation the operator wants to forget.
	mut, err := store.Open(store.Options{Dir: stateDir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("reopen for the mutation: %v", err)
	}
	if res := mut.GormDB().Exec(`DELETE FROM restore_probe WHERE id = 2`); res.Error != nil {
		_ = mut.Close()
		t.Fatalf("mutate: %v", res.Error)
	}
	if err := mut.Close(); err != nil {
		t.Fatalf("close the mutation: %v", err)
	}

	var out bytes.Buffer
	if err := cmdBackup(&out, []string{"restore", archive, "--yes"}); err != nil {
		t.Fatalf("the restore failed: %v\n%s", err, out.String())
	}

	logs := restoreLogs(t, stateDir)
	if len(logs) != 1 {
		t.Fatalf("the restore left %d logs, want exactly one: %v", len(logs), logs)
	}
	b, err := os.ReadFile(filepath.Join(stateDir, logs[0]))
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	if strings.Contains(string(b), "/") {
		t.Fatalf("the log carries a slash — the guard refused a path: %s", b)
	}
	for _, frag := range []string{
		filepath.Base(archive),
		"probe", "lock", "verify", "plan", "stage",
		"check-staged", "drill", "move-old", "rename-in", "remove-stage",
	} {
		if !strings.Contains(string(b), frag) {
			t.Fatalf("the log lacks %q:\n%s", frag, b)
		}
	}

	// The data is the backup's instant.
	st, err := store.Open(store.Options{Dir: stateDir})
	if err != nil {
		t.Fatalf("reopen after the restore: %v", err)
	}
	var n int
	if res := st.GormDB().Raw(`SELECT count(*) FROM restore_probe`).Scan(&n); res.Error != nil || n != 2 {
		t.Fatalf("the restore brought back %d rows, want 2: %v", n, res.Error)
	}
	_ = st.Close()

	if !strings.Contains(out.String(), "the restore is done") {
		t.Fatalf("the success line is missing:\n%s", out.String())
	}
}

// The door reads no environment itself: the endpoint's env var belongs
// to the endpoint package, and the state dir to paths — a second reader
// here is a second truth.
func TestTheCLIRestoreDoorReadsNoEnvironmentItself(t *testing.T) {
	for _, f := range []string{"backup_restore.go", "backup_restore_log.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(b)
		if strings.Contains(s, "os.Getenv") {
			t.Errorf("%s reads the environment — the endpoint and the paths packages own their variables", f)
		}
		if strings.Contains(s, "LOCALRPC_ENDPOINT_PATH") {
			t.Errorf("%s names the endpoint's variable — the endpoint package is the only reader", f)
		}
	}
}
