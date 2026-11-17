// Package terminal owns daemon-owned PTY shell sessions (S31 — D9).
//
// # What this package is, and the four decisions that shape it
//
// A terminal here is the PERSON'S OWN SHELL — `$SHELL` (fallback `/bin/sh`)
// under their own uid, on a socket only they can open (P26), in a REGISTERED
// project root. Policy governs what AGENTS may do; gating the person's shell
// behind policies the person writes is theatre (LW8). The registry is owned
// by the daemon (it must survive connections); this package holds the
// session machinery it keeps.
//
//   - LW7 — output NEVER enters the EventLog. The log's `seq` is durable and
//     its ring is shared by every subscriber; keystroke-rate bytes would evict
//     real events and burn `seq` allocations. Output goes straight to attached
//     connections as the `localrpc` notifications `terminal.output` /
//     `terminal.exit`, through the injectable Notifier — which is exactly
//     `localrpc.Notify`'s shape, so the daemon wires `d.notify` in and tests
//     wire a recorder. Nothing here imports the EventLog because nothing here
//     can reach it.
//   - LW9 — a terminal outlives a CONNECTION, not the daemon. Detached
//     sessions are reaped after DetachTimeout (15 min default) with nobody
//     attached; CloseAll kills every process group on daemon shutdown. A
//     window reload must not kill a running `make test`; a forgotten shell
//     must not live forever.
//   - LW10 — bounded everything: a 1 MiB scrollback ring per terminal (the
//     TAIL kept), at most MaxPerRoot (8) terminals per project root, output
//     coalesced to one notification per FlushInterval (16 ms) or FlushBytes
//     (32 KiB), whichever comes first.
//   - LW12 — Windows is `unsupported`: the platform seam (session_windows.go)
//     refuses Open with a named UnsupportedError so the package compiles for
//     GOOS=windows. ConPTY is later work; a half-working Windows terminal
//     would be the confident wrong answer P31 warns about.
//
// # The process group, and why Setsid here equals Setpgid in spirit
//
// `tools/spawn.go` starts children with `Setpgid: true` and kills the whole
// group (`Kill(-pgid, …)`) — measured there: group-kill frees a grandchild's
// TCP port, parent-only kill does not. A PTY session needs the child to be a
// session LEADER (the pty must become its controlling terminal, which is what
// `Setctty` requires), so creack/pty starts it with `Setsid: true` — a new
// session whose pgid is the child's own pid. The OBSERVABLE is identical to
// spawn.go's: the child leads its own group and `Kill(-pgid)` reaches every
// descendant, including a shell that backgrounded a server. The group-kill
// test here re-runs spawn_test.go's port-holder measurement for exactly that
// reason.
package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

// Notification methods sent to attached connections (LW7 — direct, never
// through the EventLog). The strings are protocol surface (brief §5); the
// Rust shell forwards exactly these two (T8).
const (
	MethodOutput = "terminal.output"
	MethodExit   = "terminal.exit"
)

// Default bounds (LW10 / LW9). Each is a guess the task Notes record with
// measured reality; Options overrides any of them (tests shorten all).
const (
	DefaultScrollbackBytes = 1 << 20 // 1 MiB ring, tail kept
	DefaultMaxPerRoot      = 8       // LW10: ≤ 8 terminals per project root
	DefaultFlushBytes      = 32 << 10
	DefaultFlushInterval   = 16 * time.Millisecond
	DefaultKillGrace       = 2 * time.Second // same grace tools/spawn.go keeps
	DefaultReapInterval    = 30 * time.Second
	DefaultDetachTimeout   = 15 * time.Minute
)

// Logger is the same minimal surface the daemon's Logger exposes, so the
// daemon hands in its own and tests hand in a recorder.
type Logger interface {
	Infof(format string, args ...any)
}

// Options configures a Manager. Every zero field takes its default above.
type Options struct {
	// Notifier delivers one notification to one connection — the shape of
	// `localrpc.Notify(connID, method, params)`. Nil discards.
	Notifier func(connID, method string, params any) error

	ScrollbackBytes int
	MaxPerRoot      int
	FlushBytes      int
	FlushInterval   time.Duration
	KillGrace       time.Duration
	ReapInterval    time.Duration
	DetachTimeout   time.Duration

	// Now is injectable so the reaper is testable without wall-clock waits.
	Now func() time.Time
	// Log is optional; delivery errors are spoken, never fatal.
	Log Logger
}

func (o *Options) withDefaults() {
	if o.ScrollbackBytes <= 0 {
		o.ScrollbackBytes = DefaultScrollbackBytes
	}
	if o.MaxPerRoot <= 0 {
		o.MaxPerRoot = DefaultMaxPerRoot
	}
	if o.FlushBytes <= 0 {
		o.FlushBytes = DefaultFlushBytes
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = DefaultFlushInterval
	}
	if o.KillGrace <= 0 {
		o.KillGrace = DefaultKillGrace
	}
	if o.ReapInterval <= 0 {
		o.ReapInterval = DefaultReapInterval
	}
	if o.DetachTimeout <= 0 {
		o.DetachTimeout = DefaultDetachTimeout
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = discardLogger{}
	}
}

type discardLogger struct{}

func (discardLogger) Infof(string, ...any) {}

// ── refusals (I4: every refusal names the thing refused) ────────────────────

// NotFoundError is an unknown terminal id — never opened, or already closed
// (a closed session is gone; LW9 keeps an EXITED one, which stays findable).
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("terminal: no terminal %q — it was never opened or it has been closed", e.ID)
}

// LimitError names the root that is full and the count — LW10's bound
// refused out loud rather than a silent failure to open.
type LimitError struct {
	Root  string
	Count int
	Limit int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("terminal: project root %q already has %d terminals; the bound is %d per root",
		e.Root, e.Count, e.Limit)
}

// ExitedError is input/resize against a session kept after its shell exited.
type ExitedError struct{ ID string }

func (e *ExitedError) Error() string {
	return fmt.Sprintf("terminal: terminal %q has already exited; its output is scrollback-only", e.ID)
}

// UnsupportedError is LW12's named refusal: no PTY on this OS.
type UnsupportedError struct{ OS string }

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("terminal: PTY terminals are unsupported on %s", e.OS)
}

// BadInputError is input that is not a valid UTF-8 string (Q3/LW10): input
// crosses the socket as a string; the PTY takes bytes, and handing the shell
// invalid UTF-8 is the caller's bug, refused rather than passed on.
type BadInputError struct {
	ID     string
	Reason string
}

func (e *BadInputError) Error() string {
	return fmt.Sprintf("terminal: input for terminal %q was refused: %s", e.ID, e.Reason)
}

// ── params and views (the shapes protocol/terminal.go maps onto) ────────────

type OpenParams struct {
	ComponentRoot string
	Cols          int
	Rows          int
}

// AttachResult is what terminal.attach returns; Scrollback is the raw ring
// bytes so the protocol layer base64s them exactly once.
type AttachResult struct {
	Scrollback []byte
	Seq        uint64
	Exited     bool
	ExitCode   *int
}

// CloseResult reports the exit code only when the shell had ALREADY exited —
// a killed shell died by signal and has no exit code to report.
type CloseResult struct {
	ExitCode *int
}

// View is one row of terminal.list.
type View struct {
	TerminalID    string `json:"terminalId"`
	ComponentRoot string `json:"componentRoot"`
	Cwd           string `json:"cwd"`
	Shell         string `json:"shell"`
	CreatedAt     string `json:"createdAt"`
	Attached      bool   `json:"attached"`
	Exited        bool   `json:"exited"`
	// ExitCode is set only after the shell exited by itself with a code.
	ExitCode *int `json:"exitCode,omitempty"`
}

// OutputParams is the `terminal.output` notification payload: seq is
// per-terminal and monotonic (one allocation per COALESCED chunk, not per
// byte), data is base64 because a shell produces non-UTF-8 bytes (Q3).
type OutputParams struct {
	TerminalID string `json:"terminalId"`
	Seq        uint64 `json:"seq"`
	Data       string `json:"data"`
}

// ExitParams is the `terminal.exit` notification payload; exactly one of
// ExitCode and Signal is set, the same way cmd.Wait reports it.
type ExitParams struct {
	TerminalID string `json:"terminalId"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	Signal     string `json:"signal,omitempty"`
}

// ── the Manager: the registry (the stepCancels shape: mutex + map) ──────────

// Manager owns every live session. One instance per Daemon, built by
// daemon.initTerminals; the mutex-and-map shape mirrors
// internal/daemon/stepcancel.go deliberately — same argument (cross-call
// shared state the method table must not own), same discipline (one lock,
// and no lock held across a notify or a syscall wait).
type Manager struct {
	opts Options

	mu   sync.Mutex
	byID map[string]*Session
}

func New(opts Options) *Manager {
	opts.withDefaults()
	return &Manager{opts: opts, byID: map[string]*Session{}}
}

// Open starts one shell in a registered project root. The CALLER (the
// protocol layer) has already checked the root is registered; Open checks
// what only it can see — the bound per root, and that the directory exists
// for the child's cwd.
func (m *Manager) Open(p OpenParams) (*Session, error) {
	root := filepath.Clean(p.ComponentRoot)
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("terminal: project root %q must be absolute; a relative cwd would follow "+
			"the daemon's directory, not yours", p.ComponentRoot)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("terminal: project root %q is not usable as a cwd: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("terminal: project root %q is not a directory", root)
	}

	cols, rows := p.Cols, p.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	m.mu.Lock()
	count := 0
	for _, s := range m.byID {
		if s.ComponentRoot == root {
			count++
		}
	}
	if count >= m.opts.MaxPerRoot {
		m.mu.Unlock()
		return nil, &LimitError{Root: root, Count: count, Limit: m.opts.MaxPerRoot}
	}
	s := &Session{
		mgr:           m,
		ID:            newID(m.byID),
		ComponentRoot: root,
		Shell:         resolveShell(),
		CreatedAt:     m.opts.Now(),
	}
	// Registered BEFORE startChild fails or succeeds is wrong the other way
	// round (a failed child must not linger in the map), so start first under
	// the lock's protection of the count, register after.
	m.mu.Unlock()

	if err := s.startChild(cols, rows); err != nil {
		return nil, err
	}
	s.Cwd = childCwd(s)

	m.mu.Lock()
	// Re-check the bound: Opens racing this one could each have passed the
	// first count. The cheap fix is counting twice, not a second lock.
	count = 0
	for _, other := range m.byID {
		if other.ComponentRoot == root {
			count++
		}
	}
	if count >= m.opts.MaxPerRoot {
		m.mu.Unlock()
		s.abortUnstarted()
		return nil, &LimitError{Root: root, Count: count, Limit: m.opts.MaxPerRoot}
	}
	s.ring = NewRing(m.opts.ScrollbackBytes)
	s.idleSince = s.CreatedAt
	s.attached = map[string]struct{}{}
	s.waitDone = make(chan struct{})
	m.byID[s.ID] = s
	m.mu.Unlock()

	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

// find is the one place that turns an id into a session or a refusal.
func (m *Manager) find(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return nil, &NotFoundError{ID: id}
	}
	return s, nil
}

// Input writes a UTF-8 string to the PTY (Q3: string IN, base64 OUT).
func (m *Manager) Input(id, data string) error {
	s, err := m.find(id)
	if err != nil {
		return err
	}
	if !utf8.ValidString(data) {
		return &BadInputError{ID: id, Reason: "input must be a valid UTF-8 string"}
	}
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return &ExitedError{ID: id}
	}
	if _, err := s.writeInput([]byte(data)); err != nil {
		return fmt.Errorf("terminal: writing to terminal %q failed: %w", id, err)
	}
	return nil
}

// Resize sets the PTY window (a shell laid out at 80 columns wraps at 80
// forever unless the app resizes it with its panel).
func (m *Manager) Resize(id string, cols, rows int) error {
	s, err := m.find(id)
	if err != nil {
		return err
	}
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("terminal: resize of %q needs positive cols and rows, got %dx%d", id, cols, rows)
	}
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited {
		return &ExitedError{ID: id}
	}
	if err := s.setSize(cols, rows); err != nil {
		return fmt.Errorf("terminal: resizing terminal %q failed: %w", id, err)
	}
	return nil
}

// Attach binds a connection and hands back the bounded scrollback (tail kept)
// plus the seq it reached, so the client continues from there.
func (m *Manager) Attach(connID, id string) (AttachResult, error) {
	s, err := m.find(id)
	if err != nil {
		return AttachResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached[connID] = struct{}{}
	out := AttachResult{Seq: s.seq}
	if s.ring != nil {
		out.Scrollback = s.ring.Bytes()
	}
	if s.exited {
		out.Exited = true
		if s.exitCode != nil {
			c := *s.exitCode
			out.ExitCode = &c
		}
	}
	return out, nil
}

// Detach unbinds one connection; the shell keeps running (LW9).
func (m *Manager) Detach(connID, id string) error {
	s, err := m.find(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attached, connID)
	if len(s.attached) == 0 {
		s.idleSince = m.opts.Now()
	}
	return nil
}

// DetachConn drops the connection from EVERY session — the connection-close
// hook. Nil-safe and never an error: a connection that was attached to
// nothing detaches from nothing. This is the line the daemon calls from
// onConnectionClosed; unlike subscriptions it is not a refusal to find the
// connID anywhere, because a connection may close having opened no terminal.
func (m *Manager) DetachConn(connID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	all := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.mu.Lock()
		if _, ok := s.attached[connID]; ok {
			delete(s.attached, connID)
			if len(s.attached) == 0 {
				s.idleSince = m.opts.Now()
			}
		}
		s.mu.Unlock()
	}
}

// Close kills the session — the whole process group — and removes it. It
// reports the exit code when the shell had already exited before the close.
func (m *Manager) Close(id string) (CloseResult, error) {
	m.mu.Lock()
	s, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return CloseResult{}, &NotFoundError{ID: id}
	}
	delete(m.byID, id)
	m.mu.Unlock()
	return s.terminate(), nil
}

// CloseAll kills every session's process group — daemon shutdown (LW9).
// Concurrent: each terminate waits its own SIGTERM grace, and a daemon with
// eight shells must not take eight graces to die. Nil-safe (the shutdown
// hook line must never be the thing that panics on the way out).
func (m *Manager) CloseAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	all := make([]*Session, 0, len(m.byID))
	for id, s := range m.byID {
		all = append(all, s)
		delete(m.byID, id)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.terminate()
		}(s)
	}
	wg.Wait()
}

// List enumerates sessions, optionally filtered by (cleaned) project root.
func (m *Manager) List(componentRoot string) []View {
	var want string
	if componentRoot != "" {
		want = filepath.Clean(componentRoot)
	}
	m.mu.Lock()
	all := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		all = append(all, s)
	}
	m.mu.Unlock()

	out := make([]View, 0, len(all))
	for _, s := range all {
		s.mu.Lock()
		if want != "" && s.ComponentRoot != want {
			s.mu.Unlock()
			continue
		}
		v := View{
			TerminalID:    s.ID,
			ComponentRoot: s.ComponentRoot,
			Cwd:           s.Cwd,
			Shell:         s.Shell,
			CreatedAt:     s.CreatedAt.Format(time.RFC3339),
			Attached:      len(s.attached) > 0,
			Exited:        s.exited,
		}
		if s.exited && s.exitCode != nil {
			c := *s.exitCode
			v.ExitCode = &c
		}
		s.mu.Unlock()
		out = append(out, v)
	}
	// Stable order for the UI: newest first reads as the tabs a person just
	// opened, and map order would otherwise be random per call.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt > out[j-1].CreatedAt; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ReapOnce runs one LW9 sweep and returns how many sessions it reaped:
// nobody attached, idle past DetachTimeout. It is separate from RunReaper so
// a test drives the clock through Options.Now instead of waiting on intervals.
func (m *Manager) ReapOnce() int {
	if m == nil {
		return 0
	}
	now := m.opts.Now()
	m.mu.Lock()
	var doomed []*Session
	for id, s := range m.byID {
		s.mu.Lock()
		idle := len(s.attached) == 0 && !s.reaped && now.Sub(s.idleSince) > m.opts.DetachTimeout
		if idle {
			s.reaped = true
			doomed = append(doomed, s)
			delete(m.byID, id)
		}
		s.mu.Unlock()
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range doomed {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.terminate()
		}(s)
	}
	wg.Wait()
	return len(doomed)
}

// RunReaper sweeps every ReapInterval until ctx ends. The daemon starts it
// with a background context; tests call ReapOnce with a clock they own.
func (m *Manager) RunReaper(ctx context.Context) {
	t := time.NewTicker(m.opts.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := m.ReapOnce(); n > 0 {
				m.opts.Log.Infof("terminal: reaped %d detached session(s)", n)
			}
		}
	}
}

// newID is a collision-checked id. crypto/rand rather than a counter: ids
// cross a socket, and `t-1`, `t-2` next to someone else's `t-2` is an
// id-theft waiting to happen (the lookup would close someone else's shell).
func newID(existing map[string]*Session) string {
	var b [6]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			panic("terminal: no randomness for session ids: " + err.Error())
		}
		id := "t-" + hex.EncodeToString(b[:])
		if _, clash := existing[id]; !clash {
			return id
		}
	}
}

// resolveShell is $SHELL with the POSIX fallback — the person's own shell,
// resolved at open so an operator who changes $SHELL and restarts nothing
// gets the shell their next terminal asks for.
func resolveShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}
