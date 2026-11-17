//go:build windows

package terminal

import "os"

// # LW12 — Windows is `unsupported`, named rather than half-working
//
// This file is the build-tagged stub that lets `GOOS=windows` compile the
// package (verified: `GOOS=windows CGO_ENABLED=0 go vet ./internal/terminal/`)
// while `terminal.open` refuses with the NAMED UnsupportedError the protocol
// layer maps to its registered code. ConPTY is later work; shipping the
// creack/pty Windows path (which itself answers ErrUnsupported at the start
// call) dressed up as a terminal would be the confident wrong answer P31
// refuses. Everything a terminal could do is answered honestly: none of it
// is there.

func (s *Session) startChild(cols, rows int) error {
	return &UnsupportedError{OS: "windows"}
}

// killGroup never runs (no child ever starts), and says nothing it cannot:
// with no group there is no signal to send.
func (s *Session) killGroup(sig int) {}

func (s *Session) writeInput(b []byte) (int, error) {
	return 0, &UnsupportedError{OS: "windows"}
}

func (s *Session) setSize(cols, rows int) error {
	return &UnsupportedError{OS: "windows"}
}

func (s *Session) closeMaster() {
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
}

// exitInfo has no wait status to read on this path; the "unknown" signal
// fallback in markExited keeps terminal.exit carrying one of its two fields
// even here, so T8's bridge never sees the neither-frame it drops.
func exitInfo(ps *os.ProcessState) (code int, signal string) {
	if ps == nil {
		return -1, ""
	}
	return ps.ExitCode(), ""
}
