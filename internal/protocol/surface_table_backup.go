package protocol

// surface_table_backup.go — S62 T6 (BK7): the surface table's five backup
// entries, split from the growing core file by cohesion (the backup family's
// own classification lives with its own names).
//
// The two READS (list, status) are plain reads — what exists, what the
// machine says — with no flag: a read about the person's own database is a
// read, and nothing about one is a door only a person may pass through.
//
// The three WRITES (run, verify, prune) are HumanDoorOnly: each stamps the
// actor from the connection (BK13) and moves something real — a file is
// published, a drill is run on a copy, files are deleted through the fence.
// They are named here by CONSTANT (the LW8 discipline, the seed.apply
// precedent), and they join surfaceHumanDoorFamilies in surface.go, so the
// family — the "what is true must be marked" direction — catches a table
// edit that forgets a row.

func surfaceTableBackup() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodBackupRun: {
			Kind:          SurfaceWrite,
			Summary:       "Starts a backup now, as the human door — the actor is stamped from the connection, never a param, and the file's ledger row says manual.",
			HumanDoorOnly: true,
		},
		MethodBackupVerify: {
			Kind:          SurfaceWrite,
			Summary:       "Checks one named backup at the restore level, or drills the copy as a store — the drill opens files, so the door is the person's.",
			HumanDoorOnly: true,
		},
		MethodBackupPrune: {
			Kind:          SurfaceWrite,
			Summary:       "Plans or applies the retention policy through the fence — a dry run first, deletions named by the ledger's own rows.",
			HumanDoorOnly: true,
		},
		MethodBackupList: {
			Kind:    SurfaceRead,
			Summary: "Lists the backup files newest first, each joined with its latest verify verdict and whether this binary can restore it.",
		},
		MethodBackupStatus: {
			Kind:    SurfaceRead,
			Summary: "Reports the backup's four-state machine — never, fresh, stale or failing — with the numbers behind it and the database's own size.",
		},
	}
}
