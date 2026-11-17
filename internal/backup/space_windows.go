//go:build windows

package backup

// queryFreeSpace on Windows: the platform's free-space query needs the
// volume's serial number and a Win32 call this package has not ported.
// Rather than guess — a guess here is a silent pass on the one check whose
// whole point is honesty (BK17) — the answer is unknown, and the run
// records that it took the space it was given without a measurement.
func queryFreeSpace(dir string) (uint64, FreeState) {
	return 0, FreeUnknown
}

// fsyncDir on Windows: directory-entry durability is not exposed the same
// way; the file's own Sync (already done before the rename) is the durable
// boundary this package holds.
func fsyncDir(dir string) error { return nil }
