package main

// mcp_supervisor.go — the daemon owns its MCP servers' processes.
//
// When the daemon starts it starts every enabled stdio server and keeps it
// running. The supervisor tracks each one — state, PID, start time, restarts,
// how it last ended — keeps a bounded log of its stderr and of what the
// supervisor itself did to it, and lets a person terminate, start or restart it.
// A step that is offered a server's tools uses the running process through a
// lease (Close is a no-op): the process outlives the step, which is the point.
//
// What the supervisor does NOT do: restart a crashed server on its own. A crash
// is a fact the person is shown (state `crashed`, the exit status, the log), and
// restarting is their act. A terminate is deliberate and sticks until they start
// it again; a daemon restart starts everything enabled afresh.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

// --- the log ring ---------------------------------------------------------------

const (
	mcpLogMaxLines = 1000
	mcpLogMaxLine  = 8 << 10
	mcpLogMaxTotal = 256 << 10
)

// mcpLogRing is a bounded ring of log lines with a monotonically increasing
// sequence number, so a client can ask "after N" and be told what it missed.
type mcpLogRing struct {
	mu      sync.Mutex
	lines   []protocol.MCPProcessLogLine
	total   int
	nextSeq int64
	partial strings.Builder // an unterminated stderr line, waiting for its newline
	now     func() time.Time
}

func newMCPLogRing(now func() time.Time) *mcpLogRing { return &mcpLogRing{nextSeq: 1, now: now} }

func (r *mcpLogRing) add(stream, text string) {
	if len(text) > mcpLogMaxLine {
		text = text[:mcpLogMaxLine] + "…"
	}
	r.lines = append(r.lines, protocol.MCPProcessLogLine{Seq: r.nextSeq, At: r.now().UTC().Format(time.RFC3339Nano), Stream: stream, Text: text})
	r.nextSeq++
	r.total += len(text)
	for len(r.lines) > mcpLogMaxLines || r.total > mcpLogMaxTotal {
		r.total -= len(r.lines[0].Text)
		r.lines = r.lines[1:]
	}
}

// daemon records a line about what the supervisor did.
func (r *mcpLogRing) daemon(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.add("daemon", fmt.Sprintf(format, args...))
}

// Write implements io.Writer for the server's stderr: bytes in, whole lines out.
func (r *mcpLogRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			r.add("stderr", strings.TrimRight(r.partial.String(), "\r"))
			r.partial.Reset()
			continue
		}
		if r.partial.Len() < mcpLogMaxLine {
			r.partial.WriteByte(b)
		}
	}
	return len(p), nil
}

// since answers the lines after a sequence number, how many fell out of the ring
// in between, and the sequence to ask from next.
func (r *mcpLogRing) since(after int64, limit int) (lines []protocol.MCPProcessLogLine, next int64, dropped int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > mcpLogMaxLines {
		limit = 200
	}
	next = after
	if len(r.lines) > 0 && after > 0 && r.lines[0].Seq > after+1 {
		dropped = r.lines[0].Seq - after - 1
	}
	for _, l := range r.lines {
		if l.Seq > after {
			lines = append(lines, l)
		}
	}
	if len(lines) > limit {
		dropped += int64(len(lines) - limit)
		lines = lines[len(lines)-limit:]
	}
	if n := len(lines); n > 0 {
		next = lines[n-1].Seq
	} else if r.nextSeq-1 > next {
		next = r.nextSeq - 1
	}
	return lines, next, dropped
}

// --- one supervised server --------------------------------------------------------

// supervised is a server the supervisor is meant to know about: its configured
// definition and whether it is switched on.
type supervised struct {
	Name    string
	Enabled bool
	Config  mcp.ServerConfig
}

// procClient is the slice of a running server the supervisor needs. *mcp.Client
// satisfies it; a test fake stands in.
type procClient interface {
	mcpClient
	PID() int
	Exited() <-chan struct{}
	ExitError() error
}

type mcpProc struct {
	mu        sync.Mutex
	name      string
	cfg       mcp.ServerConfig
	enabled   bool
	state     string
	pid       int
	startedAt time.Time
	restarts  int
	tools     int
	lastExit  string
	lastError string
	client    procClient
	log       *mcpLogRing
	// wantRunning is what the person last asked for: false after a terminate, so a
	// stopped server is not started again by a reconcile they did not ask for.
	wantRunning bool
	gen         int // bumps on every start, so a late exit watcher cannot mislabel a newer run
}

func (p *mcpProc) view() protocol.MCPProcessView {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := protocol.MCPProcessView{Name: p.name, Transport: p.cfg.Transport.Type, State: p.state, PID: p.pid,
		Restarts: p.restarts, Tools: p.tools, LastExit: p.lastExit, LastError: p.lastError}
	if !p.startedAt.IsZero() && (p.state == "running" || p.state == "starting") {
		v.StartedAt = p.startedAt.UTC().Format(time.RFC3339)
	}
	return v
}

// --- the supervisor ----------------------------------------------------------------

// mcpDialTimeout bounds one start: a first `npx -y` downloads before it can answer.
var mcpDialTimeout = 90 * time.Second

type mcpSupervisor struct {
	mu     sync.Mutex
	procs  map[string]*mcpProc
	source func(ctx context.Context) ([]supervised, error)
	getenv func(cfg mcp.ServerConfig) func(string) string
	dial   func(ctx context.Context, srv mcp.ServerConfig, getenv func(string) string, stderr *mcpLogRing) (procClient, error)
	// preflight refuses a start before any process exists; nil allows everything.
	preflight func(mcp.ServerConfig) error
	now       func() time.Time
	wg        sync.WaitGroup
}

func newMCPSupervisor(source func(context.Context) ([]supervised, error), getenv func(mcp.ServerConfig) func(string) string) *mcpSupervisor {
	return &mcpSupervisor{
		procs: map[string]*mcpProc{}, source: source, getenv: getenv, now: time.Now,
		dial: func(ctx context.Context, srv mcp.ServerConfig, getenv func(string) string, log *mcpLogRing) (procClient, error) {
			c, err := mcp.DialWith(ctx, srv, getenv, mcp.DialOptions{Stderr: log})
			if err != nil {
				return nil, err
			}
			return c, nil
		},
	}
}

func (s *mcpSupervisor) proc(name string) *mcpProc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.procs[name]
}

// Sync reads the configured servers and brings the processes in line: it starts
// every enabled stdio server that is not running and not deliberately stopped,
// and stops the ones that are gone or switched off. It is what daemon start runs.
func (s *mcpSupervisor) Sync(ctx context.Context) error {
	servers, err := s.source(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var starts sync.WaitGroup
	for _, srv := range servers {
		seen[srv.Name] = true
		// In parallel: one slow first npx run must not hold every other server back.
		starts.Add(1)
		go func(srv supervised) {
			defer starts.Done()
			s.reconcileOne(ctx, srv)
		}(srv)
	}
	starts.Wait()
	s.mu.Lock()
	var gone []string
	for name := range s.procs {
		if !seen[name] {
			gone = append(gone, name)
		}
	}
	s.mu.Unlock()
	for _, name := range gone {
		s.terminate(name, "removed from the configuration", false)
		s.mu.Lock()
		delete(s.procs, name)
		s.mu.Unlock()
	}
	return nil
}

// Reconcile applies ONE server's configuration change after a write: a removed or
// disabled server is stopped, a changed one restarted, a new enabled one started.
func (s *mcpSupervisor) Reconcile(ctx context.Context, name string) {
	servers, err := s.source(ctx)
	if err != nil {
		return
	}
	for _, srv := range servers {
		if srv.Name == name {
			// Background: the write that caused this must not wait for a first npx
			// download; the page polls the process list for the state.
			go s.reconcileOne(ctx, srv)
			return
		}
	}
	if s.proc(name) != nil {
		s.terminate(name, "removed from the configuration", false)
		s.mu.Lock()
		delete(s.procs, name)
		s.mu.Unlock()
	}
}

func (s *mcpSupervisor) reconcileOne(ctx context.Context, srv supervised) {
	p := s.proc(srv.Name)
	if p == nil {
		p = &mcpProc{name: srv.Name, log: newMCPLogRing(s.now), wantRunning: true}
		s.mu.Lock()
		s.procs[srv.Name] = p
		s.mu.Unlock()
	}
	p.mu.Lock()
	changed := !sameServer(p.cfg, srv.Config)
	p.cfg, p.enabled = srv.Config, srv.Enabled
	state, want := p.state, p.wantRunning
	p.mu.Unlock()

	switch {
	case srv.Config.Transport.Type != mcp.TransportStdio:
		p.mu.Lock()
		p.state = "remote"
		p.mu.Unlock()
	case !srv.Enabled:
		if state == "running" || state == "starting" {
			s.terminate(srv.Name, "disabled", false)
		}
		p.mu.Lock()
		p.state = "disabled"
		p.mu.Unlock()
	case state == "running" && changed:
		p.log.daemon("configuration changed — restarting")
		s.stopProc(p, "configuration changed")
		s.startProc(ctx, p)
	case state == "" || state == "disabled" || state == "remote":
		p.mu.Lock()
		p.wantRunning = true
		p.mu.Unlock()
		s.startProc(ctx, p)
	case (state == "crashed" || state == "failed") && changed && want:
		s.startProc(ctx, p)
	}
}

func sameServer(a, b mcp.ServerConfig) bool {
	return a.Transport.Type == b.Transport.Type && a.Transport.Command == b.Transport.Command &&
		a.Transport.URL == b.Transport.URL && strings.Join(a.Transport.Args, "\x00") == strings.Join(b.Transport.Args, "\x00") &&
		strings.Join(a.Transport.Env, "\x00") == strings.Join(b.Transport.Env, "\x00") &&
		sameBinding(a.Transport.EnvFrom, b.Transport.EnvFrom)
}

// startProc starts the process and waits for the handshake; the state is
// `starting` meanwhile, so a list shows a slow first npx run for what it is.
func (s *mcpSupervisor) startProc(ctx context.Context, p *mcpProc) {
	p.mu.Lock()
	if p.state == "running" || p.state == "starting" {
		p.mu.Unlock()
		return
	}
	p.state, p.lastError = "starting", ""
	p.gen++
	gen := p.gen
	cfg := p.cfg
	p.mu.Unlock()
	if s.preflight != nil {
		if err := s.preflight(cfg); err != nil {
			p.mu.Lock()
			p.state, p.pid, p.lastError = "failed", 0, err.Error()
			p.mu.Unlock()
			p.log.daemon("not started: %v", err)
			return
		}
	}
	p.log.daemon("starting %s", describeCommand(cfg))

	dctx, cancel := context.WithTimeout(ctx, mcpDialTimeout)
	defer cancel()
	client, err := s.dial(dctx, cfg, s.getenv(cfg), p.log)
	p.mu.Lock()
	if err != nil {
		p.state, p.pid = "failed", 0
		p.lastError = err.Error()
		p.mu.Unlock()
		p.log.daemon("start failed: %v", err)
		return
	}
	p.client, p.pid, p.startedAt, p.state = client, client.PID(), s.now(), "running"
	pid := p.pid
	p.mu.Unlock()
	p.log.daemon("running, pid %d", pid)

	if listed, lerr := client.ListTools(dctx); lerr == nil {
		p.mu.Lock()
		p.tools = len(listed)
		p.mu.Unlock()
	}
	s.wg.Add(1)
	go s.watch(p, client, gen)
}

func describeCommand(cfg mcp.ServerConfig) string {
	return strings.TrimSpace(cfg.Transport.Command + " " + strings.Join(cfg.Transport.Args, " "))
}

// watch records how a run ended — and only if it is still the current run and
// nobody asked for the end (a terminate sets its own state first).
func (s *mcpSupervisor) watch(p *mcpProc, c procClient, gen int) {
	defer s.wg.Done()
	<-c.Exited()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gen != gen || p.client != c {
		return
	}
	exit := exitDescription(c.ExitError())
	p.lastExit, p.pid = exit, 0
	if p.state == "running" {
		p.state = "crashed"
		p.log.daemon("exited on its own: %s", exit)
	}
	p.client = nil
}

func exitDescription(err error) string {
	if err == nil {
		return "exited cleanly"
	}
	return err.Error()
}

// stopProc terminates the running process (if any) and waits for it to end.
func (s *mcpSupervisor) stopProc(p *mcpProc, why string) {
	p.mu.Lock()
	c, pid := p.client, p.pid
	if c != nil {
		p.state = "stopped" // set BEFORE the kill: the watcher must not call a deliberate end a crash
	}
	p.mu.Unlock()
	if c == nil {
		return
	}
	p.log.daemon("terminating pid %d (%s)", pid, why)
	_ = c.Close()
	p.mu.Lock()
	p.client, p.pid = nil, 0
	p.lastExit = "terminated by the daemon (" + why + ")"
	p.mu.Unlock()
}

// terminate is the person's stop (or a removal): the process ends and stays ended.
func (s *mcpSupervisor) terminate(name, why string, deliberate bool) {
	p := s.proc(name)
	if p == nil {
		return
	}
	p.mu.Lock()
	if deliberate {
		p.wantRunning = false
	}
	p.mu.Unlock()
	s.stopProc(p, why)
	p.mu.Lock()
	if p.state != "remote" && p.state != "disabled" {
		p.state = "stopped"
	}
	p.mu.Unlock()
}

// --- the doors ---------------------------------------------------------------------

var errNoProcess = errors.New("no such server")

func (s *mcpSupervisor) List() []protocol.MCPProcessView {
	s.mu.Lock()
	procs := make([]*mcpProc, 0, len(s.procs))
	for _, p := range s.procs {
		procs = append(procs, p)
	}
	s.mu.Unlock()
	out := make([]protocol.MCPProcessView, 0, len(procs))
	for _, p := range procs {
		out = append(out, p.view())
	}
	sortProcViews(out)
	return out
}

func sortProcViews(v []protocol.MCPProcessView) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].Name < v[j-1].Name; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

func (s *mcpSupervisor) Logs(name string, after int64, limit int) (protocol.MCPProcessLogsResult, error) {
	p := s.proc(name)
	if p == nil {
		return protocol.MCPProcessLogsResult{}, errNoProcess
	}
	lines, next, dropped := p.log.since(after, limit)
	if lines == nil {
		lines = []protocol.MCPProcessLogLine{}
	}
	return protocol.MCPProcessLogsResult{Name: name, Lines: lines, Next: next, Dropped: dropped}, nil
}

// Start starts a server that is not running, as the person's act.
func (s *mcpSupervisor) Start(ctx context.Context, name, by string) (protocol.MCPProcessView, string) {
	p := s.proc(name)
	if p == nil {
		return protocol.MCPProcessView{}, fmt.Sprintf("refused: %q is not a configured server", name)
	}
	if refusal := controlRefusal(p, "start"); refusal != "" {
		return p.view(), refusal
	}
	p.mu.Lock()
	state := p.state
	p.wantRunning = true
	p.mu.Unlock()
	if state == "running" || state == "starting" {
		return p.view(), fmt.Sprintf("refused: %s is already %s", name, state)
	}
	p.log.daemon("start requested by %s", by)
	s.startProc(ctx, p)
	return p.view(), ""
}

// Stop terminates the process and keeps it stopped.
func (s *mcpSupervisor) Stop(name, by string) (protocol.MCPProcessView, string) {
	p := s.proc(name)
	if p == nil {
		return protocol.MCPProcessView{}, fmt.Sprintf("refused: %q is not a configured server", name)
	}
	if refusal := controlRefusal(p, "stop"); refusal != "" {
		return p.view(), refusal
	}
	p.log.daemon("terminate requested by %s", by)
	s.terminate(name, "requested by "+by, true)
	return p.view(), ""
}

// Restart terminates (if running) and starts again.
func (s *mcpSupervisor) Restart(ctx context.Context, name, by string) (protocol.MCPProcessView, string) {
	p := s.proc(name)
	if p == nil {
		return protocol.MCPProcessView{}, fmt.Sprintf("refused: %q is not a configured server", name)
	}
	if refusal := controlRefusal(p, "restart"); refusal != "" {
		return p.view(), refusal
	}
	p.log.daemon("restart requested by %s", by)
	s.stopProc(p, "restart requested by "+by)
	p.mu.Lock()
	p.restarts++
	p.state, p.wantRunning = "stopped", true
	p.mu.Unlock()
	s.startProc(ctx, p)
	return p.view(), ""
}

// controlRefusal is the plain answer for a server that has no process of ours.
func controlRefusal(p *mcpProc, verb string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.cfg.Transport.Type != mcp.TransportStdio:
		return fmt.Sprintf("refused: %s is an http server — there is no process of ours to %s", p.name, verb)
	case !p.enabled:
		return fmt.Sprintf("refused: %s is disabled — enable it first; a disabled server is never started", p.name)
	}
	return ""
}

// StopAll ends every supervised process; daemon shutdown calls it.
func (s *mcpSupervisor) StopAll() {
	s.mu.Lock()
	names := make([]string, 0, len(s.procs))
	for name := range s.procs {
		names = append(names, name)
	}
	s.mu.Unlock()
	for _, name := range names {
		s.terminate(name, "daemon shutting down", false)
	}
}

// --- the lease step dispatch uses ----------------------------------------------------

// errNotRunning is the sentence a step's tool list carries for a stopped server.
func notRunningError(p *mcpProc) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return fmt.Errorf("the MCP server %s is %s — start it from the MCP servers page", p.name, p.state)
}

type leasedClient struct{ procClient }

// Close on a lease releases nothing: the process is the supervisor's.
func (leasedClient) Close() error { return nil }

// Lease returns the running process for a step, or ok=false when the supervisor
// does not manage this server (the caller then dials per step as before). A
// managed server that is not running answers an error, so its tools are withheld
// with the reason rather than silently re-spawned behind the person's back.
func (s *mcpSupervisor) Lease(name string) (mcpClient, bool, error) {
	p := s.proc(name)
	if p == nil {
		return nil, false, nil
	}
	p.mu.Lock()
	c, state := p.client, p.state
	typ := p.cfg.Transport.Type
	p.mu.Unlock()
	if typ != mcp.TransportStdio {
		return nil, false, nil
	}
	if state == "running" && c != nil {
		return leasedClient{c}, true, nil
	}
	return nil, true, notRunningError(p)
}

// --- from the store ------------------------------------------------------------------

// superviseFromStore reads the configured servers (all tiers, enabled or not) as
// the supervisor's input.
func superviseFromStore(st *store.Store) func(context.Context) ([]supervised, error) {
	return func(ctx context.Context) ([]supervised, error) {
		all, err := st.McpRows(ctx)
		if err != nil {
			return nil, fmt.Errorf("read the MCP tables: %w", err)
		}
		out := make([]supervised, 0, len(all.Servers))
		for _, r := range all.Servers {
			out = append(out, supervised{Name: r.Name, Enabled: r.Enabled, Config: serverConfigFromRow(r)})
		}
		return out, nil
	}
}

func serverConfigFromRow(r store.McpServerRow) mcp.ServerConfig {
	var args, env []string
	envFrom := map[string]string{}
	_ = json.Unmarshal([]byte(r.Args), &args)
	_ = json.Unmarshal([]byte(r.Env), &env)
	_ = json.Unmarshal([]byte(r.EnvFrom), &envFrom)
	return mcp.ServerConfig{Name: r.Name, Transport: mcp.TransportConfig{
		Type: r.TransportType, Command: r.Command, Args: args, Env: env, EnvFrom: nilIfEmpty(envFrom), URL: r.URL}}
}

// newStoreSupervisor is the daemon's: servers from the store, environment names
// resolved through the variable manager and then the daemon's own environment.
func newStoreSupervisor(st *store.Store) *mcpSupervisor {
	s := newMCPSupervisor(superviseFromStore(st), func(cfg mcp.ServerConfig) func(string) string { return mcpGetenvFor(cfg, "") })
	s.preflight = mcpEnvPreflight
	return s
}

// --- binding resolution ------------------------------------------------------------------

// mcpGetenvFor is the lookup a server's process gets: an env NAME bound to a
// variable resolves to that variable's value (the project's override first, then
// the global one); a name with no binding resolves as it always did.
func mcpGetenvFor(cfg mcp.ServerConfig, workspaceRoot string) func(string) string {
	base := mcpGetenv(workspaceRoot)
	if len(cfg.Transport.EnvFrom) == 0 || mcpVault == nil {
		return base
	}
	root := vault.RootFor(mcpVaultStore, workspaceRoot)
	return func(name string) string {
		if variable, bound := cfg.Transport.EnvFrom[name]; bound {
			if v, ok := mcpVault.Resolve(variable, root); ok {
				return v
			}
			return ""
		}
		return base(name)
	}
}

// mcpEnvPreflight refuses a start that would hand a process an EMPTY variable: a
// name bound to a variable that has no value, or an unbound one nothing supplies.
func mcpEnvPreflight(cfg mcp.ServerConfig) error {
	if cfg.Transport.Type != mcp.TransportStdio {
		return nil
	}
	getenv := mcpGetenvFor(cfg, "")
	var missing []string
	for _, name := range cfg.Transport.Env {
		if getenv(name) != "" {
			continue
		}
		if variable, bound := cfg.Transport.EnvFrom[name]; bound {
			missing = append(missing, fmt.Sprintf("%s (bound to variable %s, which has no value)", name, variable))
		} else {
			missing = append(missing, name+" (not bound to a variable, and not in the daemon's environment)")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("an environment variable cannot be empty — no value for %s; bind it in the server's settings or set the variable in Variables & secrets", strings.Join(missing, ", "))
	}
	return nil
}

func sameBinding(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
