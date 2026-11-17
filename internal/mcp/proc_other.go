//go:build windows

package mcp

import (
	"os"
	"os/exec"
)

func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(p *os.Process) { _ = p.Kill() }
