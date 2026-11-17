//go:build !linux && !darwin

package backup

// sameDeviceFS on the platforms without a POSIX Statfs (Windows and the
// rest) cannot run the cross-filesystem check, so it answers the stated
// "cannot say" — ok=false — and the restore logs the check as unavailable
// rather than passing it silently (P31, BK17: every "I cannot tell" is a
// stated state). The swap itself is one rename inside the state dir, so
// its atomicity does not depend on the archive's location.
func sameDeviceFS(a, b string) (same bool, ok bool) {
	return false, false
}
