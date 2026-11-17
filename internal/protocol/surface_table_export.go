package protocol

// surface_table_export.go — S62 T8 (BK14): the surface table's two export
// entries, split from the growing core file by cohesion (the export family's
// own classification lives with its own names).
//
// run is the family's one WRITE and it is HumanDoorOnly: it stamps the actor
// from the connection (BK13) and moves something real — a tree of files is
// created outside the store, and the ledger takes its row when the request
// reaches the engine. It is named here by CONSTANT (the LW8 discipline, the
// backup precedent) and joins surfaceHumanDoorFamilies in surface.go, so the
// family — the "what is true must be marked" direction — catches a table edit
// that forgets the row.
//
// sets is a plain read: what the binary can export from the schema it holds,
// with no row counts and no actor — a read about the person's own database is
// a read, and nothing about one is a door only a person may pass through.
// It is OPEN (RequiresInitialize false) and says nothing that a not-yet-
// initialised daemon could not already say, so it stays out of the guard's
// `all` list beside the backup family's five.

func surfaceTableExport() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodExportRun: {
			Kind:          SurfaceWrite,
			Summary:       "Exports the declared record sets into an export dir as the human door; the actor is stamped from the connection, never a param.",
			HumanDoorOnly: true,
		},
		MethodExportSets: {
			Kind:    SurfaceRead,
			Summary: "Lists the declared export sets in registry order with each state from this schema — the opt-in named — and no row counts.",
		},
	}
}
