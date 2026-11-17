package doctor

// backup_test.go — S62 T9 (BK15/BK17): the doctor's database and
// backups facts, unit-held. The structural guard
// (TestDoctorBackupLegsAreStoreFreeAtTheTypeLevel, in cmd/aeon) holds
// the imports; these hold the sentences and the two properties a
// health check most needs: a state it cannot read is a stated state,
// and the facts themselves never create the store they report on.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/store"
)

func TestAgeHumanSaysTheCoarsestUnitThatCarriesTheFact(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-5 * time.Second, "0s"}, // a clock skew is zero, not negative
		{0, "0s"},
		{45 * time.Second, "45s"},
		{time.Minute + 30*time.Second, "1m30s"},
		{time.Hour, "1h0m"},
		{2*time.Hour + 5*time.Minute, "2h5m"},
		{24 * time.Hour, "1d0h"},
		{3*24*time.Hour + 4*time.Hour, "3d4h"},
	}
	for _, c := range cases {
		if got := AgeHuman(c.in); got != c.want {
			t.Errorf("AgeHuman(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBytesHumanKeepsItsThreeStates(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 bytes"},
		{1023, "1023 bytes"},
		{1024, "1 KiB"},
		{512 << 10, "512 KiB"},
		{1 << 20, "1.0 MiB"},
		{3 << 20, "3.0 MiB"},
	}
	for _, c := range cases {
		if got := BytesHuman(c.in); got != c.want {
			t.Errorf("BytesHuman(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDatabaseFactsOfANeverCreatedStoreIsAStateNotAnError(t *testing.T) {
	dir := t.TempDir()
	f, err := DatabaseFacts(dir)
	if err != nil {
		t.Fatalf("a missing store is a state the doctor says, not an error: %v", err)
	}
	if f.Present {
		t.Fatal("a dir with no state.db reports the store present")
	}
	want := filepath.Join(dir, "state.db")
	if f.Path != want {
		t.Errorf("Path = %q, want %q", f.Path, want)
	}
	if got := f.Line(); got != "database   no state.db at "+want+" — the store has not been created" {
		t.Errorf("Line() = %q", got)
	}
	// The facts must not have created the store: a stat of a missing
	// file does not make it, and the one conn the facts may open is
	// mode=ro — it would be refused the moment it tried to.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the database facts left %d entr(y/ies) in the state dir: %v", len(entries), entryNames(entries))
	}
}

func TestDatabaseLineTemplate(t *testing.T) {
	d := Database{
		Path:        "/s/aeon/state.db",
		Present:     true,
		Bytes:       2 << 20,
		WALBytes:    4 << 10,
		JournalMode: "wal",
		Applied:     80,
		Pending:     0,
	}
	want := "database   /s/aeon/state.db · 2.0 MiB + 4 KiB WAL · journal wal · 80 applied / 0 pending"
	if got := d.Line(); got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}

func TestBackupsLinesTemplates(t *testing.T) {
	// The never state: one line, the state and the verb that changes it.
	never := Backups{Dir: "/d"}
	if !never.Never() {
		t.Fatal("zero files is the never state")
	}
	lines := never.Lines()
	if len(lines) != 1 || lines[0] != "backups    never backed up — run `aeon backup run`" {
		t.Fatalf("the never line = %v", lines)
	}

	// The main line, verified and unverified, with the stale line when
	// the retention says so and no pre-migration line when the dirs
	// agree.
	main := Backups{Dir: "/d", Count: 3, LastAge: 5 * time.Minute, LastVerified: true, StaleAfter: time.Hour}
	lines = main.Lines()
	want := []string{"backups    last 5m0s ago (verified) · 3 kept · /d"}
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("the main line = %v, want %v", lines, want)
	}

	main.LastVerified = false
	if lines = main.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "(unverified)") {
		t.Fatalf("the unverified token = %v", lines)
	}

	main.LastVerified = true
	main.Stale = true
	lines = main.Lines()
	want = append(want, "backups    stale: last success 5m0s ago (limit 1h0m)")
	if fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("the stale line = %v, want %v", lines, want)
	}

	// The pre-migration divergence: only when the dir the hook would
	// have written before a settings change still holds files.
	main.PreMigrationDir = "/old/aeon/backups/pre-migration"
	lines = main.Lines()
	if len(lines) != 3 || lines[2] != "backups    pre-migration dir /old/aeon/backups/pre-migration differs from backup.dir /d" {
		t.Fatalf("the divergence line = %v", lines)
	}
}

// bkStripBackupEnv keeps the test's resolution the defaults': an
// ambient AEON_BACKUP_* in the runner's environment would resolve a
// dir the test does not own.
func bkStripBackupEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"AEON_BACKUP_ENABLED", "AEON_BACKUP_DIR", "AEON_BACKUP_INTERVAL",
		"AEON_BACKUP_TIMEOUT", "AEON_BACKUP_KEEP", "AEON_BACKUP_KEEP_DAILY",
		"AEON_BACKUP_KEEP_WEEKLY", "AEON_BACKUP_KEEP_PREMIGRATION",
		"AEON_BACKUP_STALE_AFTER",
	} {
		t.Setenv(k, "")
	}
}

func TestBackupFactsIsStoreFree(t *testing.T) {
	bkStripBackupEnv(t)
	dir := t.TempDir()
	b, err := BackupFacts(dir, time.Now())
	if err != nil {
		t.Fatalf("an empty state dir is the never state, not an error: %v", err)
	}
	if b.Count != 0 || !b.Never() {
		t.Fatalf("the never state = %+v", b)
	}
	if lines := b.Lines(); len(lines) != 1 || !strings.HasPrefix(lines[0], "backups    never backed up") {
		t.Fatalf("the never line = %v", lines)
	}
	if b.Dir != backup.DefaultDir(dir) {
		t.Errorf("the default dir = %q, want %q", b.Dir, backup.DefaultDir(dir))
	}
	// The point of the store-free source: a state dir with no store in
	// it stays that way after the facts are read. A facts function
	// that creates its subject would report on a store it built.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the backups facts left %d entr(y/ies) in the state dir: %v", len(entries), entryNames(entries))
	}
}

func TestDatabaseFactsOfARealStoreCountsTheLedger(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	f, err := DatabaseFacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := len(store.Migrations())
	if f.Applied != total || f.Pending != 0 {
		t.Fatalf("applied=%d pending=%d, want %d/0 — the facts must count the ledger the same way the runner does", f.Applied, f.Pending, total)
	}
	if !f.Present || f.Bytes == 0 {
		t.Fatalf("the store's facts = %+v", f)
	}
	if f.JournalMode != "wal" {
		t.Errorf("journal mode = %q, want wal", f.JournalMode)
	}
	if want := fmt.Sprintf("%d applied / 0 pending", total); !strings.HasSuffix(f.Line(), want) {
		t.Errorf("Line() = %q, want it to end %q", f.Line(), want)
	}
}

func entryNames(entries []os.DirEntry) []string {
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
