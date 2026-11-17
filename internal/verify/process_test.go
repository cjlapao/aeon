package verify

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// A killed child may briefly remain visible to signal 0 until its parent reaps
// it. Linux containers without a reaping PID 1 can retain zombies indefinitely;
// a zombie has exited and cannot execute work or hold the gate's resources.
func processRunningAfterGrace(pid int, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for {
		if syscall.Kill(pid, 0) != nil {
			return false
		}
		if runtime.GOOS == "linux" {
			raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if os.IsNotExist(err) {
				return false
			}
			if i := strings.LastIndex(string(raw), ") "); i >= 0 && len(raw) > i+2 {
				if state := raw[i+2]; state == 'Z' || state == 'X' {
					return false
				}
			}
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
}
