//go:build darwin

package backup

import "syscall"

// sameDeviceFS answers BK17's restore pre-check on macOS: two dirs are on
// the same filesystem when Statfs names them the same F_type. ok=false is
// the stated "cannot say" (a dir that will not stat) — a stated state,
// never a pass (P31).
func sameDeviceFS(a, b string) (same bool, ok bool) {
	var sa, sb syscall.Statfs_t
	if err := syscall.Statfs(a, &sa); err != nil {
		return false, false
	}
	if err := syscall.Statfs(b, &sb); err != nil {
		return false, false
	}
	return sa.F_type == sb.F_type, true
}
