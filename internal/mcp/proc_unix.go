//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the server in its own process group.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the server and everything it started. A group that is
// already gone is not an error; a process that cannot be signalled as a group
// is killed directly.
func killProcessGroup(p *os.Process) {
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		_ = p.Kill()
	}
}
