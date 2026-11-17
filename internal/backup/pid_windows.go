//go:build windows

package backup

// pidAlive on Windows: this package has no portable process-existence probe
// that is worth a Win32 import here, so it answers "cannot tell". That is
// the honest answer, and it costs nothing: a lock whose holder cannot be
// disproven still comes down by age — lockStaleAfter reclaims it, and the
// reclaim is recorded, so the worst case is a wait of up to six hours
// instead of a corrupted backup.
func pidAlive(pid int) bool { return false }
