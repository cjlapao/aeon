//go:build unix

package backup

import (
	"os"
	"syscall"
)

// queryFreeSpace asks the filesystem how many bytes can be written under
// dir. On the unix platforms Aeon builds for the answer is Bavail × Bsize
// from statfs — the space an unprivileged process may actually allocate,
// which is the number the threshold has to compare against. Any failure to
// measure answers unknown, never a guess (BK17).
func queryFreeSpace(dir string) (uint64, FreeState) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, FreeUnknown
	}
	return st.Bavail * uint64(st.Bsize), FreeOK
}

// fsyncDir makes the directory entry itself durable: a rename is a fact
// about the directory, and an unflushed directory is a rename a power cut
// can undo. A directory opened read-only is the handle its Sync flushes.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
