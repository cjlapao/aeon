package protocol

// surface_table_backup_test.go — S62 T6, the S47 ask: a test that asserts
// each of the five backup methods' CLASSIFICATION, method by method. The
// generic walks (TestSurfaceEveryEntryNamesARegisteredMethod,
// TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor) prove the table's two
// directions in bulk; this one names each of the five, so a table edit that
// reclassifies one of them fails with the method's own name in the message.
//
// The shape: three writes on the human door (each stamps the actor from the
// connection and moves something real — a file is published, a drill opens
// files, deletions go through the fence) and two plain reads (what exists,
// what the machine says — no flag, because a read about the person's own
// database is a read).

import (
	"testing"
)

func TestBackupSurfaceClassification(t *testing.T) {
	table := surfaceTable()

	// The three writes: write kind, the human door's mark, and a summary a
	// person can read.
	for _, m := range []MethodName{MethodBackupRun, MethodBackupVerify, MethodBackupPrune} {
		s, ok := table[m]
		if !ok {
			t.Fatalf("%s is not classified at all — the table's domain must cover every registered method (the completeness tests name the gap)", m)
		}
		if s.Kind != SurfaceWrite {
			t.Errorf("%s is classified %q, want write — the method moves something real (a file is published, a drill is run, files are deleted through the fence)", m, s.Kind)
		}
		if !s.HumanDoorOnly {
			t.Errorf("%s is not marked HumanDoorOnly — the actor is stamped from the connection (BK13) and a write without the person's door is a write without the person (BK12)", m)
		}
		if s.Summary == "" {
			t.Errorf("%s has no summary — the surface is a closed table of sentences, and an empty row is a missing row", m)
		}
	}

	// The two reads: read kind, and NOT the human door — a read about the
	// person's own database is a read, and the door mark is for writes.
	for _, m := range []MethodName{MethodBackupList, MethodBackupStatus} {
		s, ok := table[m]
		if !ok {
			t.Fatalf("%s is not classified at all — the table's domain must cover every registered method", m)
		}
		if s.Kind != SurfaceRead {
			t.Errorf("%s is classified %q, want read — the method answers what exists, it moves nothing", m, s.Kind)
		}
		if s.HumanDoorOnly {
			t.Errorf("%s is marked HumanDoorOnly — the mark is for the writes; a marked read would make the generic door test refuse a method the family settled as a plain read", m)
		}
	}
}
