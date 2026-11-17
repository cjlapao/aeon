//go:build aeondebug

package daemon

import (
	"os"
	"strconv"
)

func debugCrashDuringRecovery(completed int) {
	n, err := strconv.Atoi(os.Getenv("AEON_DEBUG_CRASH_DURING_RECOVERY"))
	if err == nil && n > 0 && completed == n {
		process, findErr := os.FindProcess(os.Getpid())
		if findErr == nil {
			_ = process.Kill()
		}
	}
}
