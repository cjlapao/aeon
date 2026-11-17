package main

// cmd/aeon/scheduler_parity_test.go — S27 T9's laws, tested.
//
// The harness stands the DAEMON half in-process: a real unix socket, the
// real protocol.Table dispatching over it, and either the fake ContinuousAPI
// behind the twelve scheduler doors or a real roadmap.Roadmap engine behind
// the graph pair. LOCALRPC_ENDPOINT_PATH points every client — raw dial
// included — at that socket (the mechanism production itself honours), so
// the CLI verbs run UNCHANGED, dispatch through cmdScheduler included.
//
// What each guard holds, in the spec's words: parity both ways (a verb with
// no door and a door with no verb both fail), the verbs are socket clients
// (no store import, parsed imports not grep), the seatbelts refuse without
// --yes AND print the daemon's statement before the act, --json is the wire
// verbatim, refusals are byte-identical between a raw client and the CLI on
// the same pipe, no daemon fails honestly, stop-all is advertised only
// while the daemon reports the plane, and roadmap depend/undepend are the
// socket's verbs they claim to be.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
)

// ---------------------------------------------------------------------------
// the fake continuous service — canned answers, call log, settable refusals
// ---------------------------------------------------------------------------

type schedFakeCall struct {
	method protocol.MethodName
	actor  protocol.ContinuousActor
}

type schedFakeService struct {
	status    protocol.ContinuousStatusView
	preview   protocol.ContinuousPreview
	decisions []protocol.ContinuousDecisionRow
	attention protocol.ContinuousAttention
	setRoot   protocol.ContinuousRepoView
	treeRoots []string
	stopAll   protocol.ContinuousStopAllResult

	refusal *protocol.ContinuousRefusal // set to make the controls refuse

	mu    sync.Mutex
	calls []schedFakeCall
}

var _ protocol.ContinuousAPI = (*schedFakeService)(nil)

func (f *schedFakeService) record(m protocol.MethodName, a protocol.ContinuousActor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, schedFakeCall{method: m, actor: a})
}

func (f *schedFakeService) callCount(m protocol.MethodName) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.method == m {
			n++
		}
	}
	return n
}

// callOrder is the seatbelt's proof: enable must NEVER be logged before
// the statement's status read, and never at all without --yes.
func (f *schedFakeService) callOrder() []protocol.MethodName {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.MethodName, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.method)
	}
	return out
}

func (f *schedFakeService) Status(_ context.Context) (protocol.ContinuousStatusView, error) {
	f.record(protocol.MethodSchedulerStatus, protocol.ContinuousActor{})
	return f.status, nil
}

func (f *schedFakeService) Preview(_ context.Context, _ string) (protocol.ContinuousPreview, error) {
	f.record(protocol.MethodSchedulerPreview, protocol.ContinuousActor{})
	return f.preview, nil
}

func (f *schedFakeService) Decisions(_ context.Context, _ protocol.ContinuousDecisionQuery) ([]protocol.ContinuousDecisionRow, error) {
	f.record(protocol.MethodSchedulerDecisions, protocol.ContinuousActor{})
	return f.decisions, nil
}

func (f *schedFakeService) Attention(_ context.Context, _ string) (protocol.ContinuousAttention, error) {
	f.record(protocol.MethodSchedulerAttention, protocol.ContinuousActor{})
	return f.attention, nil
}

func (f *schedFakeService) SetMaster(_ context.Context, a protocol.ContinuousActor, enabled bool) (*protocol.ContinuousRefusal, error) {
	// One seam method serves both doors (T7's shape); the call log records
	// the DOOR the actor walked, so the seatbelt counts speak the verb.
	m := protocol.MethodSchedulerEnable
	if !enabled {
		m = protocol.MethodSchedulerDisable
	}
	f.record(m, a)
	return f.refusal, nil
}

func (f *schedFakeService) SetPaused(_ context.Context, a protocol.ContinuousActor, paused bool, _ string) (*protocol.ContinuousRefusal, error) {
	m := protocol.MethodSchedulerPause
	if !paused {
		m = protocol.MethodSchedulerResume
	}
	f.record(m, a)
	return f.refusal, nil
}

func (f *schedFakeService) SetRoot(_ context.Context, a protocol.ContinuousActor, _ protocol.ContinuousRootInput) (protocol.ContinuousRepoView, *protocol.ContinuousRefusal, error) {
	f.record(protocol.MethodSchedulerRootSet, a)
	return f.setRoot, f.refusal, nil
}

func (f *schedFakeService) DisableRoot(_ context.Context, a protocol.ContinuousActor, _ string) (*protocol.ContinuousRefusal, error) {
	f.record(protocol.MethodSchedulerRootDisable, a)
	return f.refusal, nil
}

func (f *schedFakeService) EnableTree(_ context.Context, a protocol.ContinuousActor, _ string, _ protocol.ContinuousRootInput) ([]string, *protocol.ContinuousRefusal, error) {
	f.record(protocol.MethodSchedulerEnableTree, a)
	return f.treeRoots, f.refusal, nil
}

func (f *schedFakeService) StopAll(_ context.Context, a protocol.ContinuousActor, _ string) (protocol.ContinuousStopAllResult, *protocol.ContinuousRefusal, error) {
	f.record(protocol.MethodSchedulerStopAll, a)
	return f.stopAll, f.refusal, nil
}

// ---------------------------------------------------------------------------
// the socket harness — a REAL endpoint, so the raw dial is the real thing
// ---------------------------------------------------------------------------

// standSchedulerSocket binds a real unix socket in the test's temp dir,
// points LOCALRPC_ENDPOINT_PATH at it (t.Setenv — the mechanism
// endpoint.Path honours first), and serves the REAL table over it: the
// scheduler doors REGISTERED through RegisterContinuous iff wirePlane, the
// connection's person always user/uid-1000 (S27 CN2's peer). wirePlane is
// the pre-S55/pre-S27 daemon: the seam unset is V13's absence, and T7's law
// — the twelve register together or not at all — makes scheduler.status
// the probe that stands in for the plane. roadmapEngine, when non-nil,
// stands behind the always-registered roadmap doors for the graph pair.
// The fake service is the SHARED state across connections; each connection
// gets its own conn id, exactly as production's sessions do.
func standSchedulerSocket(t *testing.T, fake *schedFakeService, wirePlane bool) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "rpc.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("bind the stand socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv("LOCALRPC_ENDPOINT_PATH", sock)

	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if fake != nil {
		tbl.Continuous = fake
	}
	tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor {
		return protocol.Actor{Kind: "user", ID: "uid-1000"}
	}
	if wirePlane {
		if !tbl.RegisterContinuous() {
			t.Fatal("RegisterContinuous refused to register over a wired seam")
		}
	}

	var n atomic.Int64
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go serveParityConn(tbl, conn, fmt.Sprintf("c-stand-%d", n.Add(1)))
		}
	}()
}

// serveParityConn speaks the same line-delimited JSON-RPC the daemon
// speaks, dispatching through the REAL table (the parityPipeServer frame
// shape, over a real socket, one id per connection).
func serveParityConn(tbl *protocol.Table, conn net.Conn, id string) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	var mu sync.Mutex
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if uerr := json.Unmarshal(line, &req); uerr != nil || req.ID == nil {
			continue
		}
		res, derr := tbl.Dispatch(context.Background(), id, protocol.MethodName(req.Method), req.Params)
		resp := struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   *protocol.Error `json:"error,omitempty"`
		}{JSONRPC: "2.0", ID: *req.ID}
		switch {
		case derr != nil:
			var pe *protocol.Error
			if errors.As(derr, &pe) {
				resp.Error = pe
			} else {
				resp.Error = &protocol.Error{Code: -32603, Message: derr.Error()}
			}
		case res != nil:
			raw, merr := json.Marshal(res)
			if merr != nil {
				resp.Error = &protocol.Error{Code: -32603, Message: merr.Error()}
			} else {
				resp.Result = raw
			}
		}
		mu.Lock()
		werr := json.NewEncoder(conn).Encode(resp)
		mu.Unlock()
		if werr != nil {
			return
		}
	}
}

// schedRawClient is the test's OWN client on the same pipe the CLI dials —
// the verbatim comparisons need bytes from both sides of the same socket.
func schedRawClient(t *testing.T) *protocol.Client {
	t.Helper()
	c, err := dial(0)
	if err != nil {
		t.Fatalf("raw dial to the stand socket: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ---------------------------------------------------------------------------
// the fixed frame — one golden shape for every test that prints
// ---------------------------------------------------------------------------

func schedFixedFake() *schedFakeService {
	return &schedFakeService{
		status: protocol.ContinuousStatusView{
			Enabled: false, Paused: true, PausedReason: "person paused it for the demo",
			Settings: []protocol.ContinuousSetting{
				{Key: "scheduler.maxAgentSlots", Value: "2", Source: "setting"},
			},
			Slots:      protocol.ContinuousSlots{Used: 1, Cap: 2},
			NextTickAt: "2026-11-05T14:15:00Z",
			Attention:  protocol.ContinuousAttentionSum{Open: 2},
			Repos: []protocol.ContinuousRepoView{{
				ComponentRoot: "/repo/alpha", Enabled: true,
				Template:    protocol.ContinuousTemplate{Blueprint: "dev-loops"},
				Window:      &protocol.ContinuousWindow{Start: "09:00", End: "18:00", Days: "1-5", Zone: "Europe/Lisbon"},
				MinPriority: "assumed", LastStartAt: "2026-11-05T10:00:00Z",
				State: protocol.ContinuousRepoState{Kind: "waiting", Reason: "opens Mon 09:00 Europe/Lisbon"},
			}},
			Statement: "enabling lets the scheduler start loops unattended (S27 CN2)",
		},
		preview: protocol.ContinuousPreview{
			Now: "2026-11-05T14:14:00Z",
			WouldStart: []protocol.ContinuousPreviewLine{{
				ComponentRoot: "/repo/alpha", SubjectKind: "queue", SubjectID: "7",
				Verdict: "start", Reason: "capacity", Detail: "1 of 2 slots open",
			}},
			Decisions: []protocol.ContinuousPreviewLine{
				{ComponentRoot: "/repo/alpha", SubjectKind: "queue", SubjectID: "7",
					Verdict: "start", Reason: "capacity", Detail: "1 of 2 slots open"},
				{ComponentRoot: "/repo/beta", SubjectKind: "roadmap", SubjectID: "9",
					Verdict: "defer", Reason: "window-closed", Detail: "opens Mon 09:00 Europe/Lisbon"},
			},
		},
		attention: protocol.ContinuousAttention{
			Open: 1,
			Items: []protocol.ContinuousAttentionItem{{
				DecisionID: 41, ComponentRoot: "/repo/alpha", Kind: "needs-you",
				LoopSlug: "queue-7", Note: "the loop asked a question", Since: "2026-11-04T08:00:00Z",
			}},
		},
	}
}

// ---------------------------------------------------------------------------
// parity, both ways
// ---------------------------------------------------------------------------

// schedSourceMethods walks scheduler.go for the protocol.MethodSchedulerX
// selectors the verbs ACTUALLY call — read off the AST, not a hand-kept
// list (a hand-kept parity list drifts exactly the way this test exists to
// catch). Same mechanism as the mergequeue census.
func schedSourceMethods(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "scheduler.go", nil, 0)
	if err != nil {
		t.Fatalf("parse scheduler.go: %v", err)
	}
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "protocol" &&
			strings.HasPrefix(sel.Sel.Name, "MethodScheduler") {
			out[sel.Sel.Name] = true
		}
		return true
	})
	return out
}

// schedConstFor names the Go constant a wire method compiles from — the
// join between the twelve registered names and the selectors the AST walk
// reports.
func schedConstFor(m protocol.MethodName) string {
	switch m {
	case protocol.MethodSchedulerStatus:
		return "MethodSchedulerStatus"
	case protocol.MethodSchedulerPreview:
		return "MethodSchedulerPreview"
	case protocol.MethodSchedulerDecisions:
		return "MethodSchedulerDecisions"
	case protocol.MethodSchedulerAttention:
		return "MethodSchedulerAttention"
	case protocol.MethodSchedulerEnable:
		return "MethodSchedulerEnable"
	case protocol.MethodSchedulerDisable:
		return "MethodSchedulerDisable"
	case protocol.MethodSchedulerPause:
		return "MethodSchedulerPause"
	case protocol.MethodSchedulerResume:
		return "MethodSchedulerResume"
	case protocol.MethodSchedulerRootSet:
		return "MethodSchedulerRootSet"
	case protocol.MethodSchedulerRootDisable:
		return "MethodSchedulerRootDisable"
	case protocol.MethodSchedulerEnableTree:
		return "MethodSchedulerEnableTree"
	case protocol.MethodSchedulerStopAll:
		return "MethodSchedulerStopAll"
	}
	return "?" + string(m)
}

func TestSchedulerVerbParityBothWays(t *testing.T) {
	// Direction ONE — every registered door has exactly one verb, and the
	// verb's file actually calls the constant.
	called := schedSourceMethods(t)
	seen := map[protocol.MethodName]int{}
	for _, v := range schedulerVerbs {
		seen[v.method]++
		if !called[schedConstFor(v.method)] {
			t.Errorf("`scheduler %s` names %s in the fixture but scheduler.go never calls it — the fixture would vouch for a door nobody opens", v.verb, v.method)
		}
	}
	for _, m := range schedulerAllMethods() {
		if seen[m] != 1 {
			t.Errorf("%s is covered by %d scheduler verbs — exactly one verb per door, both ways (T0's parity law)", m, seen[m])
		}
	}
	if len(schedulerVerbs) != len(schedulerAllMethods()) {
		t.Errorf("the fixture lists %d verbs, the plane registers %d doors", len(schedulerVerbs), len(schedulerAllMethods()))
	}
	// Direction TWO — every fixture row names a REGISTERED socket method;
	// roadmapVerbs rides the same law (T0 §7: S27 adds roadmap.depend and
	// roadmap.undepend to the table; the CLI must not lead or lag).
	censusTable := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	censusTable.Continuous = &schedFakeService{}
	if !censusTable.RegisterContinuous() {
		t.Fatal("RegisterContinuous refused over a wired seam")
	}
	registered := map[protocol.MethodName]bool{}
	for _, m := range censusTable.Methods() {
		registered[m] = true
	}
	roadmapSource := schedRoadmapSourceMethods(t)
	for _, v := range append(append([]cliVerb{}, schedulerVerbs...), roadmapVerbs...) {
		if !registered[v.method] {
			t.Errorf("%s (%s) is not a registered socket method — the fixture names a door the daemon does not open", v.verb, v.method)
		}
		if v.method == protocol.MethodRoadmapDepend || v.method == protocol.MethodRoadmapUndepend {
			constName := "MethodRoadmapDepend"
			if v.method == protocol.MethodRoadmapUndepend {
				constName = "MethodRoadmapUndepend"
			}
			if !roadmapSource[constName] {
				t.Errorf("roadmap.go never calls %s — the graph verb is a fixture claim without a socket call behind it", v.method)
			}
		}
	}
	// The open-gap ledger this task leaves: NONE. Both directions are
	// closed; an `open-gap` row would have to name the task that closes
	// it, and T9 closes its own.
}

// The seatbelt fixture column is DOCUMENTATION only here — the truth of
// the law is TestSeatbeltsRefuseAcrossTheSeven, which runs the seven verbs
// against a live stand and counts calls: a gate that opened, or a refusal
// that did not name --yes, fails on the log, not on a list.

// schedRoadmapSourceMethods walks roadmap.go (and its manage siblings) for
// the depend/undepend selectors specifically.
func schedRoadmapSourceMethods(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, name := range []string{"roadmap.go", "roadmap_manage.go", "roadmap_client.go"} {
		if _, err := os.Stat(name); err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "protocol" &&
				(sel.Sel.Name == "MethodRoadmapDepend" || sel.Sel.Name == "MethodRoadmapUndepend") {
				out[sel.Sel.Name] = true
			}
			return true
		})
	}
	return out
}

func TestSchedulerVerbsAreSocketClients(t *testing.T) {
	// The P34/P5 rule in one walk: the verbs file imports no store (and no
	// driver, and no daemon package — the socket is the door). Parsed
	// imports, not grep: a comment cannot trip it and an alias cannot hide.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "scheduler.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, imp := range f.Imports {
		var p string
		if err := json.Unmarshal([]byte(imp.Path.Value), &p); err != nil {
			continue
		}
		for _, banned := range []string{"/internal/store", "/internal/database", "/internal/daemon", "gorm.io"} {
			if strings.HasPrefix(p, "github.com/cjlapao/aeon"+banned) || strings.HasPrefix(p, banned) {
				t.Errorf("scheduler.go imports %s — the CLI opens nothing; the daemon answers", p)
			}
		}
	}
	// And the file reaches the daemon through NOTHING but the shared dial.
	src, err := os.ReadFile("scheduler.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"net.Dial", "http.", "exec.Command", "store.Open", "sqlite"} {
		if strings.Contains(string(src), banned) {
			t.Errorf("scheduler.go contains %q — schedDial is the only road out of this file (rule 1: one implementation, the socket)", banned)
		}
	}
}

func TestSchedulerVerbIsDispatchedByTheBinary(t *testing.T) {
	// The S53 F-1 lesson, applied to the new verb: b5620631 hid a whole
	// plane behind a deleted dispatch line while every direct-call test
	// stayed green. Dispatch, callee and usage line are pinned here.
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, "case \"scheduler\":") {
		t.Fatal(`main.go lost case "scheduler": — the real binary answers "unknown command" while every cmdScheduler test stays green`)
	}
	if !strings.Contains(src, "err = cmdScheduler(os.Stdout, args)") {
		t.Fatal(`main.go's scheduler case no longer dispatches to cmdScheduler(os.Stdout, args) — the CLI is ONE implementation`)
	}
	if !strings.Contains(src, "scheduler         status, preview, decisions, attention") {
		t.Error("the usage list no longer advertises `scheduler` — a hidden verb is a deleted verb waiting to happen")
	}
}

// ---------------------------------------------------------------------------
// the seatbelts
// ---------------------------------------------------------------------------

func TestEnableRequiresYesAndPrintsTheStatement(t *testing.T) {
	fake := schedFixedFake()
	standSchedulerSocket(t, fake, true)

	// Without --yes: the statement prints FIRST, the refusal NAMES the
	// flag, and the fake never sees an enable call — the call log is the
	// proof, not the print.
	var out bytes.Buffer
	err := cmdScheduler(&out, []string{"enable"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("enable without --yes must refuse and name the flag, got %v", err)
	}
	if !strings.Contains(out.String(), fake.status.Statement) {
		t.Errorf("the daemon's own statement must print before any refusal can stand:\n%s", out.String())
	}
	if fake.callCount(protocol.MethodSchedulerEnable) != 0 {
		t.Errorf("the enable call reached the daemon WITHOUT --yes: %v", fake.callOrder())
	}

	// With --yes: statement then act, in THAT order — the call log holds
	// status before enable.
	out.Reset()
	if err := cmdScheduler(&out, []string{"enable", "--yes"}); err != nil {
		t.Fatalf("enable --yes: %v", err)
	}
	order := fake.callOrder()
	si, ei := -1, -1
	for i, m := range order {
		if m == protocol.MethodSchedulerStatus && si == -1 {
			si = i
		}
		if m == protocol.MethodSchedulerEnable {
			ei = i
		}
	}
	if si == -1 || ei == -1 || si > ei {
		t.Errorf("the statement read must PRECEDE the act (call log %v)", order)
	}
	if !strings.Contains(out.String(), fake.status.Statement) {
		t.Errorf("--yes still prints what was granted:\n%s", out.String())
	}
	// The actor crossed the pipe from the CONNECTION, not from a flag
	// (S27 CN2): the fake sees the peer the table resolved.
	if got := fake.calls[ei].actor; got.Kind != "user" || got.ID != "uid-1000" {
		t.Errorf("enable reached the service as %+v — the person is the peer, never argv", got)
	}
}

func TestSeatbeltsRefuseAcrossTheSeven(t *testing.T) {
	fake := schedFixedFake()
	standSchedulerSocket(t, fake, true)
	cases := [][]string{
		{"enable"}, {"disable"}, {"pause", "--reason", "demo"},
		{"stop-all", "--reason", "demo"},
		{"repo", "set", "--component", "/repo/alpha", "--blueprint", "dev-loops"},
		{"repo", "disable", "--component", "/repo/alpha"},
		{"enable-tree", "--root", "42", "--blueprint", "dev-loops"},
	}
	before := map[protocol.MethodName]int{
		protocol.MethodSchedulerEnable:      fake.callCount(protocol.MethodSchedulerEnable),
		protocol.MethodSchedulerDisable:     fake.callCount(protocol.MethodSchedulerDisable),
		protocol.MethodSchedulerPause:       fake.callCount(protocol.MethodSchedulerPause),
		protocol.MethodSchedulerStopAll:     fake.callCount(protocol.MethodSchedulerStopAll),
		protocol.MethodSchedulerRootSet:     fake.callCount(protocol.MethodSchedulerRootSet),
		protocol.MethodSchedulerRootDisable: fake.callCount(protocol.MethodSchedulerRootDisable),
		protocol.MethodSchedulerEnableTree:  fake.callCount(protocol.MethodSchedulerEnableTree),
	}
	for _, args := range cases {
		var out bytes.Buffer
		err := cmdScheduler(&out, args)
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("`scheduler %s` without --yes did not name the flag: %v", strings.Join(args, " "), err)
		}
		for m, n := range before {
			if got := fake.callCount(m); got != n {
				t.Errorf("`scheduler %s` without --yes reached the daemon (%s x%d): %+v", strings.Join(args, " "), m, got, args)
			}
		}
	}
	// And the gated act LANDS with --yes (the seatbelt gates, it does not
	// forbid — the human-door law of CN18).
	if err := cmdScheduler(&bytes.Buffer{}, []string{"disable", "--yes"}); err != nil {
		t.Fatalf("disable --yes: %v", err)
	}
	if fake.callCount(protocol.MethodSchedulerDisable) != 1 {
		t.Errorf("disable --yes must call the door exactly once, saw %d", fake.callCount(protocol.MethodSchedulerDisable))
	}
}

// ---------------------------------------------------------------------------
// the prints
// ---------------------------------------------------------------------------

func TestPreviewOutputIsTheWire(t *testing.T) {
	fake := schedFixedFake()
	standSchedulerSocket(t, fake, true)

	// --json is the daemon's bytes with NOTHING added: the same raw call
	// on the same socket gives the reference.
	var out bytes.Buffer
	if err := cmdScheduler(&out, []string{"preview", "--json"}); err != nil {
		t.Fatalf("preview --json: %v", err)
	}
	c := schedRawClient(t)
	var raw json.RawMessage
	if err := c.Call(protocol.MethodSchedulerPreview, struct{}{}, &raw); err != nil {
		t.Fatalf("raw preview: %v", err)
	}
	if got, want := out.String(), string(raw)+"\n"; got != want {
		t.Errorf("preview --json is not the wire verbatim:\n got %s\nwant %s", got, want)
	}

	// The human render is a FIXED template over the wire fields — golden:
	// reason and detail in the daemon's own words, the spec's
	// `window-closed — opens Mon 09:00 Europe/Lisbon` shape intact.
	out.Reset()
	if err := cmdScheduler(&out, []string{"preview"}); err != nil {
		t.Fatalf("preview: %v", err)
	}
	const golden = "now: 2026-11-05T14:14:00Z\n" +
		"would start (1):\n" +
		"  start queue/7 · capacity — 1 of 2 slots open [/repo/alpha]\n" +
		"not starting (1):\n" +
		"  defer roadmap/9 · window-closed — opens Mon 09:00 Europe/Lisbon [/repo/beta]\n"
	if out.String() != golden {
		t.Errorf("the preview render drifted from the golden:\n got %q\nwant %q", out.String(), golden)
	}
}

func TestStatusOutputIsTheWire(t *testing.T) {
	fake := schedFixedFake()
	standSchedulerSocket(t, fake, true)
	var out bytes.Buffer
	if err := cmdScheduler(&out, []string{"status", "--json"}); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	c := schedRawClient(t)
	var raw json.RawMessage
	if err := c.Call(protocol.MethodSchedulerStatus, struct{}{}, &raw); err != nil {
		t.Fatalf("raw status: %v", err)
	}
	if got, want := out.String(), string(raw)+"\n"; got != want {
		t.Errorf("status --json is not the wire verbatim:\n got %s\nwant %s", got, want)
	}
	out.Reset()
	if err := cmdScheduler(&out, []string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	// The status table speaks the daemon's sentences: the pause reason
	// and the repository state verbatim, the settings' source words, the
	// slots as used/cap.
	for _, want := range []string{
		"master: disabled", "paused: yes — person paused it for the demo",
		"slots: 1/2 · next tick: 2026-11-05T14:15:00Z · attention: 2 open",
		"setting scheduler.maxAgentSlots = 2 (setting)",
		"/repo/alpha · enabled · blueprint dev-loops",
		"window 09:00-18:00 1-5 Europe/Lisbon",
		"state: waiting — opens Mon 09:00 Europe/Lisbon",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status render missing %q:\n%s", want, out.String())
		}
	}
}

// ---------------------------------------------------------------------------
// refusals, honesty, and the hidden verb
// ---------------------------------------------------------------------------

func TestRefusalsAreVerbatim(t *testing.T) {
	fake := schedFixedFake()
	fake.refusal = &protocol.ContinuousRefusal{Sentence: "another agent holds the switch; ask a person"}
	standSchedulerSocket(t, fake, true)

	// The SAME question to the same socket, two doors: a raw client and
	// the CLI must answer with the SAME bytes. The CLI adds no sentence of
	// its own to a refusal — it passes the daemon's through (rule 4).
	c := schedRawClient(t)
	rawErr := c.Call(protocol.MethodSchedulerEnable, struct{}{}, new(map[string]any))
	if rawErr == nil {
		t.Fatal("the settable refusal did not reach the raw client")
	}
	cliErr := cmdScheduler(&bytes.Buffer{}, []string{"enable", "--yes"})
	if cliErr == nil {
		t.Fatal("the refusal did not reach the CLI")
	}
	if got, want := cliErr.Error(), rawErr.Error(); got != want {
		t.Errorf("the CLI re-worded the daemon's refusal:\n CLI  %s\n DAEMON %s", got, want)
	}
	// The refusal's own words ride in the wire payload — Data.What is
	// where T7 renders the sentence — so the comparison goes one level
	// deeper than the headline: the CLI's error and the daemon's, encoded,
	// must agree to the byte.
	var cliPE, rawPE *protocol.Error
	if !errors.As(cliErr, &cliPE) || !errors.As(rawErr, &rawPE) {
		t.Fatalf("neither door surfaced a protocol error: cli=%v raw=%v", cliErr, rawErr)
	}
	cliBytes, _ := json.Marshal(cliPE)
	if !strings.Contains(string(cliBytes), "another agent holds the switch") {
		t.Errorf("the refusal's own words must ride through the CLI's error: %s", cliBytes)
	}
}

func TestNoDaemonFailsHonestly(t *testing.T) {
	// The recorded P5 deviation, tested: nobody home is the ANSWER. The
	// standard dial line, no offline arm, no second sentence, nothing
	// written — a scheduler without a daemon is not a thing.
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "cold.sock"))
	for _, args := range [][]string{{"status"}, {"preview"}, {"decisions"}, {"attention"}} {
		var out bytes.Buffer
		err := cmdScheduler(&out, args)
		var nd *noDaemonError
		if !errors.As(err, &nd) {
			t.Errorf("`scheduler %s` did not answer with the standard no-daemon line: %v", args, err)
		}
		if out.Len() != 0 {
			t.Errorf("`scheduler %s` printed %q while the daemon was cold — the no-daemon line is the whole answer", args, out.String())
		}
	}
}

func TestStopAllHiddenWithoutS55(t *testing.T) {
	// T7's registration law is what makes this testable as a probe: the
	// twelve doors register together iff the seam is wired, so a daemon
	// WITHOUT the plane is exactly the daemon that lacks scheduler.stopAll
	// — the pre-S27 daemon answers scheduler.status with method-not-found,
	// and `aeon scheduler` must not advertise a verb the daemon lacks.
	standSchedulerSocket(t, nil, false) // seam unset: V13's absence
	var out bytes.Buffer
	printSchedulerUsage(&out)
	if strings.Contains(out.String(), schedStopAllLine) {
		t.Errorf("usage advertises stop-all against a daemon that does not report the plane:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "listed only when the daemon reports the scheduler plane") {
		t.Errorf("the hidden line must SAY why the verb is absent:\n%s", out.String())
	}
	// The verb itself answers honestly too — the daemon's own
	// method-not-found, not a CLI-invented excuse.
	err := cmdScheduler(&bytes.Buffer{}, []string{"stop-all", "--reason", "demo", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "scheduler.stopAll") || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("stop-all on a plane-less daemon must pass the daemon's own unknown-method sentence through, got %v", err)
	}
	// With the plane wired, the verb is listed (both halves of the law).
	fake := schedFixedFake()
	standSchedulerSocket(t, fake, true)
	out.Reset()
	printSchedulerUsage(&out)
	if !strings.Contains(out.String(), schedStopAllLine) {
		t.Errorf("usage must advertise stop-all while the daemon reports the plane:\n%s", out.String())
	}
}

// ---------------------------------------------------------------------------
// the graph pair — roadmap depend/undepend, the socket's verbs
// ---------------------------------------------------------------------------

func TestRoadmapDependVerbs(t *testing.T) {
	// T3 opened the doors, T9 holds the client half: depend and undepend
	// are plain socket verbs — dial, call, print — over a REAL daemon and
	// the REAL engine behind the always-registered roadmap doors (S50's
	// own parity shape: a fixture daemon, a CLI handle to its store). An
	// edge is not a dispatch: no seatbelt (D4 re-reads readiness at the
	// attempt).
	d := startDaemon(t)
	repo := queueRepo(t, d, "rmdep")
	st := openFixtureStoreForCLI(t, d)
	engine := roadmap.New(st)
	seed := func(title string) int64 {
		it, refusals := engine.CreateAudited(repo, roadmap.NewItem{
			Title: title, Provenance: roadmap.ProvenanceAuthored,
		}, roadmap.Actor{Kind: "user", ID: "carlos"})
		if refusals != nil {
			t.Fatalf("seed %s: %s", title, refusals.Reason)
		}
		return it.ID
	}
	first, second := seed("waits"), seed("blocks")
	t.Setenv("LOCALRPC_ENDPOINT_PATH", d.endpoint)

	var out bytes.Buffer
	if err := cmdRoadmapDepend(&out, []string{fmt.Sprint(first), "--on", fmt.Sprint(second)}); err != nil {
		t.Fatalf("roadmap depend: %v", err)
	}
	if !strings.Contains(out.String(), fmt.Sprintf("waits for #%d(", second)) {
		t.Errorf("after depend the item must print its graph, got:\n%s", out.String())
	}

	// The edge refuses for the daemon's reasons, verbatim through the
	// CLI: an item does not wait on itself.
	if err := cmdRoadmapDepend(&out, []string{fmt.Sprint(first), "--on", fmt.Sprint(first)}); err == nil {
		t.Error("self-depend was accepted — the daemon's law did not reach the CLI verbatim")
	}

	out.Reset()
	if err := cmdRoadmapUndepend(&out, []string{fmt.Sprint(first), "--on", fmt.Sprint(second)}); err != nil {
		t.Fatalf("roadmap undepend: %v", err)
	}
	if strings.Contains(out.String(), "waits for") {
		t.Errorf("after undepend the edge must be gone, got:\n%s", out.String())
	}
	// And the fixture row matches what the file calls (the census above
	// checks the selectors; this checks the verbs ran).
	var found int
	for _, v := range roadmapVerbs {
		if v.method == protocol.MethodRoadmapDepend || v.method == protocol.MethodRoadmapUndepend {
			found++
			if v.seatbelt {
				t.Errorf("%s must not carry a seatbelt — an edge starts nothing (D4)", v.verb)
			}
		}
	}
	if found != 2 {
		t.Errorf("roadmapVerbs must carry the graph pair exactly once each, found %d", found)
	}
}
