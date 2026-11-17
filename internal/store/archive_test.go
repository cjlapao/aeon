package store

import (
	"strings"
	"testing"
)

// T9's archive tests. One transaction, because a half-archive has no owner.

func TestArchivingALoopMovesItsRowsAndDocumentsInOneTransaction(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "finished", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1", Title: "a task"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPhase(sc, "build", "done", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDocument(sc, "plan", "# Plan\n", "component-manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDocument(sc, "tasks/T1.md", "# T1\n", ""); err != nil {
		t.Fatal(err)
	}

	a, err := s.ArchiveLoop(sc, "done and dusted")
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if a.FileCount != 2 {
		t.Errorf("the archive holds %d files, want 2", a.FileCount)
	}
	if a.StateBytes == 0 {
		t.Error("the archive holds no state, so it could not be read back without the live tables")
	}

	// The loop is GONE from the live tables: the header plus its files are the
	// whole record.
	if _, err := s.GetLoop(sc); err == nil {
		t.Error("the loop is still live after being archived")
	}
	if n := countRows(t, s, `SELECT count(*) FROM task WHERE loop_slug = ?`, sc.LoopSlug); n != 0 {
		t.Errorf("%d tasks survived the archive", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM loop_document WHERE loop_slug = ?`, sc.LoopSlug); n != 0 {
		t.Errorf("%d documents survived the archive", n)
	}

	// And it reads back, content and all.
	header, files, err := s.ReadArchive(a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if header.Slug != "finished" || header.Note != "done and dusted" {
		t.Errorf("the header came back as %+v", header)
	}
	if len(files) != 2 {
		t.Fatalf("%d files came back", len(files))
	}
	for _, f := range files {
		if f.Content == "" {
			t.Errorf("%s came back with no content", f.Path)
		}
	}
}

func TestAForcedFailureMidArchiveLeavesTheLoopLiveAndUnarchived(t *testing.T) {
	// An archive that copies the documents but leaves the loop live, or the
	// reverse, is a state nothing reconciles.
	//
	// Forced with a trigger on the archive-file insert, which fails AFTER the
	// header is written and BEFORE the loop is deleted — the exact window where
	// a half state would be created.
	s := openTemp(t)
	sc := seedLoop(t, s, "half", "/repo")
	if _, err := s.WriteDocument(sc, "plan", "# Plan\n", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := s.w.Exec(`
		CREATE TRIGGER refuse_archive_file BEFORE INSERT ON loop_archive_file
		BEGIN SELECT RAISE(ABORT, 'forced failure mid-archive'); END`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = s.w.Exec(`DROP TRIGGER refuse_archive_file`) }()

	if _, err := s.ArchiveLoop(sc, ""); err == nil {
		t.Fatal("the archive succeeded although its files could not be written")
	}

	if _, err := s.GetLoop(sc); err != nil {
		t.Errorf("the loop was removed although the archive failed: %v", err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM loop_archive`); n != 0 {
		t.Errorf("%d archive headers exist after a failed archive", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM loop_document WHERE loop_slug = ?`, sc.LoopSlug); n != 1 {
		t.Errorf("the loop's documents were lost: %d remain", n)
	}
}

func TestAnArchiveManifestCanBeListedWithoutInflatingContent(t *testing.T) {
	// The split into two tables exists for this: a manifest that inflates
	// megabytes of content is a manifest nobody can list.
	s := openTemp(t)
	sc := seedLoop(t, s, "manifest", "/repo")
	if _, err := s.WriteDocument(sc, "plan", strings.Repeat("x", 4096), ""); err != nil {
		t.Fatal(err)
	}
	a, err := s.ArchiveLoop(sc, "")
	if err != nil {
		t.Fatal(err)
	}

	_, files, err := s.ReadArchive(a.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("%d files in the manifest", len(files))
	}
	if files[0].Content != "" {
		t.Error("the manifest inflated content although it was not asked to")
	}
	if files[0].Bytes != 4096 || files[0].SHA256 == "" {
		t.Errorf("the manifest lost the facts a listing needs: %+v", files[0])
	}
}

func TestAnArchivedFileIsVerifiedAgainstItsDigest(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "verify", "/repo")
	if _, err := s.WriteDocument(sc, "plan", "# Plan\n", ""); err != nil {
		t.Fatal(err)
	}
	a, err := s.ArchiveLoop(sc, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE loop_archive_file SET sha256 = 'wrong'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadArchive(a.ID, true); err == nil {
		t.Fatal("a corrupt archived file was returned")
	}
}

func TestArchivesAreListedNewestFirstAndScoped(t *testing.T) {
	s := openTemp(t)
	for _, root := range []string{"/repo-a", "/repo-b"} {
		sc := seedLoop(t, s, "refactor", root)
		if _, err := s.ArchiveLoop(sc, ""); err != nil {
			t.Fatal(err)
		}
	}

	inA, err := s.ListArchives(ComponentScope{ComponentRoot: "/repo-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inA) != 1 || inA[0].ComponentRoot != "/repo-a" {
		t.Errorf("listing /repo-a's archives returned %+v", inA)
	}
	all, err := s.ListArchives(ComponentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("the unscoped list returned %d archives", len(all))
	}
}

func TestDeletingAnArchiveCascadesToItsFiles(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "cascade-archive", "/repo")
	if _, err := s.WriteDocument(sc, "plan", "# Plan\n", ""); err != nil {
		t.Fatal(err)
	}
	a, err := s.ArchiveLoop(sc, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`DELETE FROM loop_archive WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM loop_archive_file`); n != 0 {
		t.Errorf("%d archived files survived their archive", n)
	}
}

func TestArchivingAMissingLoopIsNotFound(t *testing.T) {
	s := openTemp(t)
	if _, err := s.ArchiveLoop(Scope{LoopSlug: "never-existed", ComponentRoot: "/repo"}, ""); err == nil {
		t.Fatal("archiving a loop that does not exist succeeded")
	}
}
