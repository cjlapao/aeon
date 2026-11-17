package backup

// The container's hygiene: a hand-built tar with a traversal name, a
// symlink member, an extra member, a truncated db or a mismatching digest
// is refused with a sentence that says which hostile shape it was.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

const testDBBytes = "state-bytes-for-the-container-tests"

// baseManifest is a valid manifest for testDBBytes at the given applied
// count — the control every hostile case mutates exactly one way.
func baseManifest(appliedN int, last string) Manifest {
	sum := sha256.Sum256([]byte(testDBBytes))
	fp := sha256.Sum256([]byte("agent\nloop"))
	return Manifest{
		Format:               ManifestFormat,
		Name:                 Name,
		CreatedAt:            testNow.Format(createdFormat),
		Reason:               "manual",
		AeonVersion:          "0.0.0-test",
		AppliedMigrations:    appliedN,
		LastMigration:        last,
		SchemaFingerprintSHA: hex.EncodeToString(fp[:]),
		Tables:               map[string]int64{"agent": 3, "loop": 1},
		DBBytes:              int64(len(testDBBytes)),
		DBSHA256:             hex.EncodeToString(sum[:]),
		JournalModeInFile:    "delete",
		CredentialsIncluded:  false,
		Confidential:         true,
		OutsideDB:            DefaultOutsideDB,
		Verified:             "unverified",
	}
}

// writeArchive writes a valid manifest member and the db member, then lets
// mutate add or change a third member's worth of damage.
func writeArchive(t *testing.T, dir, name string, m Manifest, mutate func(tw *tar.Writer)) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	man, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	mustHeader(t, tw, memberManifest, int64(len(man)))
	if _, err := tw.Write(man); err != nil {
		t.Fatal(err)
	}
	mustHeader(t, tw, memberDB, int64(len(testDBBytes)))
	if _, err := tw.Write([]byte(testDBBytes)); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(tw)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustHeader(t *testing.T, tw *tar.Writer, name string, size int64) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
}

func TestContainerRefusesHostileTar(t *testing.T) {
	applied := firstApplied(3)
	last := applied[len(applied)-1]

	t.Run("traversal name", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		// The first member wears a path: the reader must refuse it before
		// it reads a byte of the database.
		path := filepath.Join(dir, nameFor(t, m))
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		tw := tar.NewWriter(f)
		if err := tw.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o600, Size: 4, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte("evil"))
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "a bare name, not a path") {
			t.Fatalf("the traversal member: got %v, want the bare-name refusal", err)
		}
	})

	t.Run("symlink member", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := writeArchive(t, dir, nameFor(t, m), m, func(tw *tar.Writer) {
			// A second state.db, a link to somewhere else: the archive
			// must refuse the type, and the first valid one is what the
			// header check sees first, so this member is added under the
			// db name after the real one — which the reader meets when it
			// asks for the next member.
			_ = tw.WriteHeader(&tar.Header{Name: memberDB, Mode: 0o600, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
		})
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "third member") {
			t.Fatalf("the extra symlink member: got %v, want the third-member refusal", err)
		}
	})

	t.Run("symlink as the database member", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := filepath.Join(dir, nameFor(t, m))
		f, _ := os.Create(path)
		defer f.Close()
		tw := tar.NewWriter(f)
		man, _ := m.Marshal()
		mustHeader(t, tw, memberManifest, int64(len(man)))
		_, _ = tw.Write(man)
		// The database member is a symlink wearing the right name.
		_ = tw.WriteHeader(&tar.Header{Name: memberDB, Mode: 0o600, Size: 0, Typeflag: tar.TypeSymlink, Linkname: "../../state.db"})
		_ = tw.Close()
		f.Close()
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("the symlink database member: got %v, want the type refusal", err)
		}
	})

	t.Run("directory member", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := filepath.Join(dir, nameFor(t, m))
		f, _ := os.Create(path)
		defer f.Close()
		tw := tar.NewWriter(f)
		man, _ := m.Marshal()
		mustHeader(t, tw, memberManifest, int64(len(man)))
		_, _ = tw.Write(man)
		_ = tw.WriteHeader(&tar.Header{Name: memberDB, Mode: 0o755, Typeflag: tar.TypeDir})
		_ = tw.Close()
		f.Close()
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("the directory member: got %v, want the type refusal", err)
		}
	})

	t.Run("extra member", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := writeArchive(t, dir, nameFor(t, m), m, func(tw *tar.Writer) {
			_ = tw.WriteHeader(&tar.Header{Name: "notes.txt", Mode: 0o600, Size: 5, Typeflag: tar.TypeReg})
			_, _ = tw.Write([]byte("hello"))
		})
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "third member") {
			t.Fatalf("the extra member: got %v, want the third-member refusal", err)
		}
	})

	t.Run("truncated database", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := writeArchive(t, dir, nameFor(t, m), m, nil)
		// Cut the file off inside the database member's data, measured:
		// the manifest's blocks, the db's header block, half the data.
		man, merr := m.Marshal()
		if merr != nil {
			t.Fatal(merr)
		}
		// manifest = its header block plus its data blocks, then the db's
		// header block, then the db's data.
		dataStart := int64(2+(len(man)+511)/512) * 512
		if err := os.Truncate(path, dataStart+int64(len(testDBBytes))/2); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "will not stream") {
			t.Fatalf("the truncated member: got %v, want the streaming refusal", err)
		}
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		m.DBSHA256 = strings.Repeat("0", 64) // a digest the bytes do not carry
		path := writeArchive(t, dir, nameFor(t, m), m, nil)
		_, err := Open(path)
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("the mismatched digest: got %v, want the checksum refusal", err)
		}
		// Verify level 1 maps it to the ledger's checksum kind.
		rep, verr := Verify(context.Background(), fakeDeps(newFakeStore(applied), store.FileReport{}, t.TempDir()), path, 1)
		if verr == nil || rep.OK {
			t.Fatalf("verify over the mismatched digest: want a refused level 1, got %+v / %v", rep, verr)
		}
	})

	t.Run("manifest size disagrees with the member", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		m.DBBytes = int64(len(testDBBytes)) + 1
		path := writeArchive(t, dir, nameFor(t, m), m, nil)
		_, err := Open(path)
		if err == nil || !strings.Contains(err.Error(), "disagree") {
			t.Fatalf("the size disagreement: got %v, want the manifest-vs-member refusal", err)
		}
	})

	t.Run("wrong member order", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := filepath.Join(dir, nameFor(t, m))
		f, _ := os.Create(path)
		defer f.Close()
		tw := tar.NewWriter(f)
		// The database first: the first member is not the manifest.
		mustHeader(t, tw, memberDB, int64(len(testDBBytes)))
		_, _ = tw.Write([]byte(testDBBytes))
		man, _ := m.Marshal()
		mustHeader(t, tw, memberManifest, int64(len(man)))
		_, _ = tw.Write(man)
		_ = tw.Close()
		f.Close()
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "manifest.json") {
			t.Fatalf("the out-of-order members: got %v, want the expected-name refusal", err)
		}
	})

	t.Run("valid archive passes", func(t *testing.T) {
		dir := t.TempDir()
		m := baseManifest(3, last)
		path := writeArchive(t, dir, nameFor(t, m), m, nil)
		r, err := Open(path)
		if err != nil {
			t.Fatalf("the valid archive: %v", err)
		}
		got := r.Manifest()
		if got.DBSHA256 != m.DBSHA256 || got.DBBytes != m.DBBytes {
			t.Fatalf("the manifest did not round-trip: %+v", got)
		}
		db, err := os.ReadFile(r.DBPath())
		if err != nil {
			t.Fatalf("read the extracted db: %v", err)
		}
		if !bytes.Equal(db, []byte(testDBBytes)) {
			t.Fatalf("the extracted bytes differ from the member")
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close the reader: %v", err)
		}
	})
}

// nameFor is a legal final name for a test archive: the closed set's shape,
// the fixed clock, a known last migration.
func nameFor(t *testing.T, m Manifest) string {
	t.Helper()
	return FileName(testNow, m.AppliedMigrations)
}
