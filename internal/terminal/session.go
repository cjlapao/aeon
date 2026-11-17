package terminal

import (
	"encoding/base64"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Signal numbers used for the group kill, spelled as constants so the
// shared file stays free of `syscall` (which does not compile for GOOS=windows —
// LW12's compile requirement). The platform file maps them.
const (
	sigTERM = 15
	sigKILL = 9
)

// Session is one PTY shell. Fields are guarded by mu with two exceptions
// fixed at Open (ID, ComponentRoot, Shell, CreatedAt, Cwd — Cwd set before
// registration) and the flushMu ordering rule below.
//
// # The one reader goroutine, the ring, and the coalescer (LW10)
//
// readLoop is the session's single reader of the pty master. Every byte it
// reads goes to the ring (tail kept) and into `pending`; pending flushes as
// ONE `terminal.output` notification per FlushInterval (16 ms) or FlushBytes
// (32 KiB), whichever comes first — one seq allocation per coalesced chunk,
// per byte is what LW7 says the EventLog must never see, and here it is not
// even one notification per byte.
//
// flushMu serialises the two things that flush (the reader on size, the
// timer on time) so notifications leave in `seq` order even when both fire
// close together; mu is never held across a notify, same discipline as
// daemon.deliver.
type Session struct {
	mgr *Manager

	ID            string
	ComponentRoot string
	Shell         string
	CreatedAt     time.Time
	Cwd           string

	mu          sync.Mutex
	cmd         *exec.Cmd // set by startChild (unix); nil until then, never on windows
	ptmx        *os.File  // the pty master
	pgid        int       // == the child's pid: Setsid made it its own group leader
	ring        *Ring
	pending     []byte
	flushTimer  *time.Timer
	seq         uint64
	attached    map[string]struct{}
	idleSince   time.Time
	exited      bool
	exitCode    *int
	signal      string
	reaped      bool          // claimed by the reaper, so a racing Close cannot
	terminating bool          // claimed by Close/CloseAll/ReapOnce — one owner
	waitDone    chan struct{} // closed when waitLoop's cmd.Wait returned

	flushMu sync.Mutex
}

// ── the reader and the coalescer ────────────────────────────────────────────

func (s *Session) readLoop() {
	buf := make([]byte, s.mgr.opts.FlushBytes)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.consume(buf[:n])
		}
		if err != nil {
			// EIO is the normal ending: the last slave fd closed when the
			// group died. A read error while the shell still lives (a Close
			// racing it) lands the exit facts through waitLoop instead.
			return
		}
	}
}

// consume takes one read chunk: ring (tail kept) then the coalescer.
func (s *Session) consume(b []byte) {
	s.mu.Lock()
	if s.ring != nil {
		s.ring.Write(b)
	}
	s.pending = append(s.pending, b...)
	big := len(s.pending) >= s.mgr.opts.FlushBytes
	if !big && s.flushTimer == nil {
		s.flushTimer = time.AfterFunc(s.mgr.opts.FlushInterval, s.flushNow)
	}
	s.mu.Unlock()
	if big {
		s.flushNow()
	}
}

// flushNow delivers `pending` as one notification (seq++), to a SNAPSHOT of
// the attached connections taken under mu and notified outside it.
func (s *Session) flushNow() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	if len(s.pending) == 0 {
		s.mu.Unlock()
		return
	}
	// A chunk never exceeds FlushBytes: the reader's read plus the
	// coalescer's arming window can leave threshold-1 bytes and a whole read
	// in pending (measured: 32 776 bytes in one notification before this
	// cap), so over-threshold pending SPLITS — the first FlushBytes go now,
	// the remainder re-arms the timer and goes on the next tick. LW10's
	// "32 KiB whichever first" is then a bound a client can rely on.
	chunk := s.pending
	var rest []byte
	if len(chunk) > s.mgr.opts.FlushBytes {
		rest = chunk[s.mgr.opts.FlushBytes:]
		chunk = chunk[:s.mgr.opts.FlushBytes]
	}
	s.pending = rest
	if len(rest) > 0 {
		// The split-off remainder goes on the next interval, not silently
		// parked until new bytes arrive.
		s.flushTimer = time.AfterFunc(s.mgr.opts.FlushInterval, s.flushNow)
	}
	s.seq++
	p := OutputParams{
		TerminalID: s.ID,
		Seq:        s.seq,
		Data:       base64.StdEncoding.EncodeToString(chunk),
	}
	conns := s.snapshotConnsLocked()
	s.mu.Unlock()

	s.notifyAll(conns, MethodOutput, p)
}

// markExited records the exit, flushes what the reader already read (so
// output the shell produced reaches a client before its exit), and sends
// `terminal.exit` — always carrying exitCode OR signal, never neither (T8's
// Rust bridge drops a frame with neither rather than guessing).
func (s *Session) markExited(code int, signal string) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return
	}
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	var out *OutputParams
	if len(s.pending) > 0 {
		s.seq++
		out = &OutputParams{
			TerminalID: s.ID,
			Seq:        s.seq,
			Data:       base64.StdEncoding.EncodeToString(s.pending),
		}
		s.pending = nil
	}
	if signal != "" {
		s.signal = signal
	} else if code >= 0 {
		c := code
		s.exitCode = &c
	} else {
		// No code AND no signal is not a state a client can read. cmd.Wait
		// without a usable ProcessState is the only way here; say so rather
		// than emit the neither-frame T8 refuses.
		s.signal = "unknown"
	}
	ep := ExitParams{TerminalID: s.ID, ExitCode: s.exitCode, Signal: s.signal}
	conns := s.snapshotConnsLocked()
	s.exited = true
	s.mu.Unlock()

	if out != nil {
		s.notifyAll(conns, MethodOutput, *out)
	}
	s.notifyAll(conns, MethodExit, ep)
}

// waitLoop reaps the child and turns its ProcessState into the exit facts.
// It is the ONLY cmd.Wait caller; terminate() waits on waitDone rather than
// calling Wait itself (two Waits race over ProcessState).
func (s *Session) waitLoop() {
	defer close(s.waitDone)
	_ = s.cmd.Wait() // the exit status is a fact for the client, not an error here
	code, signal := exitInfo(s.cmd.ProcessState)
	s.markExited(code, signal)
}

// snapshotConnsLocked copies the attached set; notifyAll must run unlocked.
func (s *Session) snapshotConnsLocked() []string {
	if len(s.attached) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.attached))
	for c := range s.attached {
		out = append(out, c)
	}
	return out
}

func (s *Session) notifyAll(conns []string, method string, params any) {
	if s.mgr.opts.Notifier == nil || len(conns) == 0 {
		return
	}
	for _, connID := range conns {
		if err := s.mgr.opts.Notifier(connID, method, params); err != nil {
			// Spoken, never fatal: a connection can die between the snapshot
			// and the send, and that is the connection's lifecycle (it
			// DETACHES via onConnectionClosed), not the session's fault.
			s.mgr.opts.Log.Infof("terminal: %s to %s could not be delivered: %v", method, connID, err)
		}
	}
}

// ── termination (the group kill, LW9) ───────────────────────────────────────

// terminate owns one death: SIGTERM the group, wait the grace, SIGKILL the
// group unconditionally (checking first is a race, and SIGKILL to a dead
// group is a harmless ESRCH — the spawn.go rule). It returns the exit code
// only when the shell had ALREADY exited before this call (the close.json
// contract: "reports the exit code when the shell had already exited").
func (s *Session) terminate() CloseResult {
	s.mu.Lock()
	if s.terminating {
		s.mu.Unlock()
		<-s.waitDone
		return CloseResult{} // the FIRST terminator owns the report
	}
	s.terminating = true
	alreadyExited := s.exited
	var already *int
	if alreadyExited && s.exitCode != nil {
		c := *s.exitCode
		already = &c
	}
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		s.killGroup(sigTERM)
		select {
		case <-s.waitDone:
		case <-time.After(s.mgr.opts.KillGrace):
			s.killGroup(sigKILL)
			<-s.waitDone // SIGKILL cannot be caught; this returns
		}
	}
	s.closeMaster() // unblocks a reader still parked in Read

	if alreadyExited {
		return CloseResult{ExitCode: already}
	}
	return CloseResult{}
}

// abortUnstarted cleans up after a startChild that failed part way (a pty
// opened but the child never exec'd). No group exists in that case; the
// master must still not leak.
func (s *Session) abortUnstarted() {
	if s.cmd != nil && s.cmd.Process != nil {
		s.killGroup(sigKILL)
	}
	s.closeMaster()
}

// ── the scrollback ring (LW10: 1 MiB, TAIL kept) ────────────────────────────

// Ring is a fixed-capacity circular byte buffer that keeps the most recent
// capacity bytes. A window reload re-attaches and gets the TAIL — the part
// a person is looking at — rather than the head of a build log that already
// scrolled away.
type Ring struct {
	buf  []byte
	pos  int
	full bool
}

func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = DefaultScrollbackBytes
	}
	return &Ring{buf: make([]byte, 0, capacity)}
}

func (r *Ring) Write(b []byte) {
	if len(b) == 0 {
		return
	}
	// Past capacity only the tail matters; start from the last `capacity`.
	if len(b) >= cap(r.buf) {
		b = b[len(b)-cap(r.buf):]
		copy(r.buf, b)
		r.pos = 0
		r.full = true
		return
	}
	if len(r.buf) < cap(r.buf) {
		// Filling for the first time.
		room := cap(r.buf) - len(r.buf)
		if len(b) <= room {
			r.buf = append(r.buf, b...)
			return
		}
		r.buf = append(r.buf, b[:room]...)
		b = b[room:]
		r.pos = 0
		r.full = true
		// fall through to wrap the remainder
	}
	n := copy(r.buf[r.pos:], b)
	if n < len(b) {
		copy(r.buf, b[n:])
	}
	r.pos = (r.pos + len(b)) % cap(r.buf)
	r.full = true
}

// Bytes returns the retained bytes, oldest first.
func (r *Ring) Bytes() []byte {
	if !r.full {
		out := make([]byte, len(r.buf))
		copy(out, r.buf)
		return out
	}
	out := make([]byte, 0, cap(r.buf))
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return out
}

// Len reports the retained byte count (a test of the bound, not of memory).
func (r *Ring) Len() int {
	if !r.full {
		return len(r.buf)
	}
	return cap(r.buf)
}

// childCwd is what the shell reports as PWD: the cleaned registered root
// handed to exec as Dir. (symlink reality recorded in the task Notes.)
func childCwd(s *Session) string { return s.ComponentRoot }
