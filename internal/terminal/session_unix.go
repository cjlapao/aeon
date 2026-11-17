//go:build !windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

// startChild starts the shell on a PTY in its OWN session, which is also its
// own process group: creack/pty sets `Setsid: true` (and `Setctty: true`, so
// the pty is the child's controlling terminal — the kernel requires the two
// together). The group id therefore equals the child's pid, and killGroup's
// `Kill(-pgid, …)` reaches every descendant — the same observable
// tools/spawn.go proves with a freed port, where the mechanism is `Setpgid`.
// The brief's "Setsid" names what actually happens here; see the T7 Notes.
//
// # EV10's one deliberate inherit, named (S59 T5)
//
// This child inherits the daemon's environment (through `terminalEnv`, which
// strips only the `GIT_*` retargeters), and it is the ONE place in the daemon
// where that is the designed behaviour rather than the leak S59 closes. The
// reason is that this child is not model-driven: it is the operator's own
// interactive shell, in the operator's own terminal, opened by the operator
// from the app — the same shell they would get from a dock icon. Scrubbing it
// would break the thing it exists to be (`ssh`, `gh`, `brew`, their dotfiles,
// their agent socket) without removing any risk a model controls, because no
// model is on the other end of this pty. Every spawn a MODEL can reach goes
// through `tools.Spawn`, which refuses to run without a resolved environment.
// T1's `TestTerminalEnvironmentGolden` pins this environment byte for byte;
// T5 changed no behaviour here, only this paragraph.
func (s *Session) startChild(cols, rows int) error {
	cmd := exec.Command(s.Shell)
	cmd.Dir = s.ComponentRoot
	cmd.Env = terminalEnv(os.Environ())

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return fmt.Errorf("terminal: the shell %q could not start in %q: %w", s.Shell, s.ComponentRoot, err)
	}
	s.cmd = cmd
	s.ptmx = ptmx
	s.pgid = cmd.Process.Pid
	return nil
}

// killGroup signals the group AND every member of the child's session.
//
// The group kill alone is NOT enough on a PTY — measured here, and the
// difference from spawn.go is the reason: spawn.go's shell runs `bash -c`
// NON-interactively, so backgrounded jobs stay in the shell's group and
// `Kill(-pgid)` reaches them. A terminal's shell is INTERACTIVE (its stdin
// is a tty), interactive shells enable job control, and job control puts
// EACH BACKGROUNDED JOB in its own process group — inside the child's
// SESSION, outside its group. The port-holder test caught exactly this: the
// group was killed, the port stayed held.
//
// So: Kill(-pgid) first (the cheap, correct path for foreground work), then
// a /proc walk of this session's members (session id == pgid, since Setsid
// made the child a session leader) and signal each pid. SIGKILL after the
// grace cannot be caught, so a survivor of the SIGTERM pass dies on the
// second pass anyway. /proc exists on Linux; on other unixes the walk
// returns nothing and the behaviour equals spawn.go's group kill (recorded
// as such in the task Notes — darwin/bsd job-control survivors are later
// work, LW12-honest rather than silently partial).
func (s *Session) killGroup(sig int) {
	if s.pgid <= 0 {
		return
	}
	_ = syscall.Kill(-s.pgid, syscall.Signal(sig))
	for _, pid := range sessionMembers(s.pgid) {
		if pid == s.pgid {
			continue // already reached by the group kill
		}
		_ = syscall.Kill(pid, syscall.Signal(sig))
	}
}

// sessionMembers lists the pids whose terminal session id is sid. Any read
// error skips a pid — a process vanishing mid-walk is a RACE with death,
// not a failure worth reporting: the point is that none of them survives
// the SIGKILL pass.
func sessionMembers(sid int) []int {
	dir, err := os.Open("/proc")
	if err != nil {
		return nil
	}
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil
	}
	var out []int
	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil || pid <= 0 {
			continue
		}
		stat, err := os.ReadFile("/proc/" + name + "/stat")
		if err != nil {
			continue
		}
		// "pid (comm) state ppid pgrp session …" — comm may contain spaces
		// and parens; count from the LAST ')'.
		rest := string(stat)
		if i := strings.LastIndexByte(rest, ')'); i >= 0 {
			rest = rest[i+2:] // skip ") "
			f := strings.Fields(rest)
			if len(f) >= 4 {
				if sess, err := strconv.Atoi(f[3]); err == nil && sess == sid {
					out = append(out, pid)
				}
			}
		}
	}
	return out
}

func (s *Session) writeInput(b []byte) (int, error) {
	if s.ptmx == nil {
		return 0, fmt.Errorf("terminal: terminal %s has no pty", s.ID)
	}
	return s.ptmx.Write(b)
}

func (s *Session) setSize(cols, rows int) error {
	if s.ptmx == nil {
		return fmt.Errorf("terminal: terminal %s has no pty", s.ID)
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (s *Session) closeMaster() {
	if s.ptmx != nil {
		_ = s.ptmx.Close() // concurrent with a parked Read: os.File says so, and readLoop treats any error as the end
	}
}

// exitInfo reads the wait status the way spawn.go reads it: a signalled
// death has a signal and NO exit code (ExitCode() reports -1 there, and the
// two must not be conflated in what the client is told).
func exitInfo(ps *os.ProcessState) (code int, signal string) {
	if ps == nil {
		return -1, ""
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return -1, ws.Signal().String()
	}
	return ps.ExitCode(), ""
}

// terminalEnv inherits the daemon's environment (the stated spawn.go
// limitation — scrubbing PATH would break every shell) with TERM set to
// xterm-256color, the declared capability: an existing TERM is REPLACED,
// not appended, or the shell would honour the first and ignore the second.
func terminalEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if len(kv) >= 5 && kv[:5] == "TERM=" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color")
}
