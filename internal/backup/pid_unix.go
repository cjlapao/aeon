//go:build unix

package backup

import "syscall"

// pidAlive asks whether a process with this pid exists. Signal 0 is the
// probe: it changes nothing, and the answer distinguishes a live process
// (nil), one that exists but is not ours (EPERM — alive, from another
// user's point of view), and one that is gone (ESRCH).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
