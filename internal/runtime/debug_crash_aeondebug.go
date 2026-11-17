//go:build aeondebug

package runtime

import (
	"os"
	"sync/atomic"
)

var crashAfterTool atomic.Bool

func ConfigureDebugCrashAfterTool(enabled bool) { crashAfterTool.Store(enabled) }

// Only a deliberately tagged development binary can simulate the narrow
// receipt-completed/transcript-not-yet-written crash window.
func debugCrashAfterTool() {
	if crashAfterTool.Load() {
		process, err := os.FindProcess(os.Getpid())
		if err == nil {
			_ = process.Kill()
		}
	}
}
