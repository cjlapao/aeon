package main

import (
	"os"
	"testing"
)

// trackExit reaps exactly one process and lets readiness checks fail immediately
// when startup fails. Capture the process and channel for cleanup: restart replaces
// f.cmd, and a cleanup for the old process must never kill or wait on its replacement.
func (f *daemonFixture) trackExit(t *testing.T, logFile *os.File) <-chan struct{} {
	t.Helper()
	cmd := f.cmd
	done := make(chan struct{})
	f.exited = done
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
		_ = logFile.Close()
	})
	return done
}
