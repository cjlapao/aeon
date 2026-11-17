package main

// autonomy_parity_test.go — S56 T9 (AL13). The CLI is a client of the
// daemon's ten autonomy.* methods, and these guards keep that true:
//
//   - PARITY, both directions: a verb with no method (the CLI leads the
//     daemon) and a method with no verb (the CLI lags the daemon) are both
//     failures, walked over the autonomyVerbs table against a live table.
//   - THE OFFLINE ASYMMETRY: grant never falls back (a widening written
//     offline would leave an audit row whose identity no kernel attested);
//     every other dial fault (permission, version) is refused, never fallen
//     back from — a fallback from an error that is not "nobody is home"
//     routes around a daemon that exists.
//   - THE DIRECT DOOR IS THE SERVICE: its constructor opens the real store
//     and builds the daemon's own service; each of its ten methods speaks
//     the service through d.s — a go/parser walk forbids a second
//     implementation.
//   - ONE REFUSAL RENDERER: the same refusal over both doors is the same
//     *protocol.Error byte-for-byte.
//   - THE ACTOR: the direct door writes as the fixed {cli, cli}; the socket
//     door writes as the daemon's stamped peer — the CLI sends no actor.
//   - THE HELP IS BUILT, NOT TYPED: its subject/condition lists are
//     set-equal to the pure Vocabulary().
//   - THE SEATBELT: grant and revoke are refused without --yes, BEFORE any
//     door is asked.
//
// The socket-side tests run a REAL protocol.Client over an in-memory pipe
// against the REAL table (NewTable + seam + RegisterAutonomy + Dispatch) —
// the wire is asserted, not the code. No test here points at the real store.

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
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// --- the fixtures ------------------------------------------------------------

// autonomyVerbNames derives the closed verb set from the parity table — the
// table is the source the guards walk, so the guards do not re-list it.
func autonomyVerbNames() []string {
	out := make([]string, 0, len(autonomyVerbs))
	for _, e := range autonomyVerbs {
		out = append(out, e.verb)
	}
	return out
}

// fakeAutonomyService is the seam fake: with a refusal set, every method that
// can refuse returns it (the refusal tests); the three writes record the
// actor they were called with (the actor tests). Reads answer empty success.
type fakeAutonomyService struct {
	refusal       *protocol.AutonomyRefusal
	effectiveView protocol.EffectiveView

	mu     sync.Mutex
	actors []protocol.Actor
}

var _ protocol.AutonomyService = (*fakeAutonomyService)(nil)

func (f *fakeAutonomyService) recordActor(a protocol.Actor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actors = append(f.actors, a)
}

func (f *fakeAutonomyService) lastActor() (protocol.Actor, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.actors) == 0 {
		return protocol.Actor{}, false
	}
	return f.actors[len(f.actors)-1], true
}

func (f *fakeAutonomyService) List(context.Context, string, string, bool) ([]protocol.GrantView, error) {
	return nil, nil
}

func (f *fakeAutonomyService) Show(context.Context, string) (protocol.GrantView, *protocol.ChangeView, int, []string, *protocol.AutonomyRefusal, error) {
	if f.refusal != nil {
		return protocol.GrantView{}, nil, 0, nil, f.refusal, nil
	}
	return protocol.GrantView{}, nil, 0, nil, nil, nil
}

func (f *fakeAutonomyService) Vocabulary() (protocol.VocabularyView, error) {
	return protocol.VocabularyView{}, nil
}

func (f *fakeAutonomyService) Effective(context.Context, string, string, []string) (protocol.EffectiveView, *protocol.AutonomyRefusal, error) {
	if f.refusal != nil {
		return protocol.EffectiveView{}, f.refusal, nil
	}
	return f.effectiveView, nil, nil
}

func (f *fakeAutonomyService) Pinned(context.Context, string) (protocol.PinnedView, *protocol.AutonomyRefusal, error) {
	if f.refusal != nil {
		return protocol.PinnedView{}, f.refusal, nil
	}
	return protocol.PinnedView{}, nil, nil
}

func (f *fakeAutonomyService) Changes(context.Context, string, string, int) ([]protocol.ChangeView, error) {
	return nil, nil
}

func (f *fakeAutonomyService) Uses(context.Context, string, string, string, int) ([]protocol.UseView, error) {
	return nil, nil
}

func (f *fakeAutonomyService) Grant(_ context.Context, actor protocol.Actor, _ protocol.AutonomyGrantRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	f.recordActor(actor)
	if f.refusal != nil {
		return protocol.AutonomyWriteOutcome{}, f.refusal, nil
	}
	return protocol.AutonomyWriteOutcome{View: &protocol.GrantView{ID: "g-fake"}, Confirmation: "confirmed (fake)"}, nil, nil
}

func (f *fakeAutonomyService) Deny(_ context.Context, actor protocol.Actor, _ protocol.AutonomyDenyRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	f.recordActor(actor)
	if f.refusal != nil {
		return protocol.AutonomyWriteOutcome{}, f.refusal, nil
	}
	return protocol.AutonomyWriteOutcome{View: &protocol.GrantView{ID: "g-fake"}, Confirmation: "confirmed (fake)"}, nil, nil
}

func (f *fakeAutonomyService) Revoke(_ context.Context, actor protocol.Actor, _ protocol.AutonomyRevokeRequest) (protocol.AutonomyWriteOutcome, *protocol.AutonomyRefusal, error) {
	f.recordActor(actor)
	if f.refusal != nil {
		return protocol.AutonomyWriteOutcome{}, f.refusal, nil
	}
	return protocol.AutonomyWriteOutcome{View: &protocol.GrantView{ID: "g-fake"}, Confirmation: "confirmed (fake)"}, nil, nil
}

// newFakeTable wires a table the way the daemon does — seam, the actor seam,
// RegisterAutonomy (the same three lines daemon.go runs) — over the fake.
func newFakeTable(t *testing.T, svc protocol.AutonomyService, actor protocol.Actor) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Autonomy = svc
	if !tbl.RegisterAutonomy() {
		t.Fatal("RegisterAutonomy refused over a non-nil seam — the fake table would have no autonomy door at all")
	}
	tbl.ActorFor = func(context.Context, *protocol.Session) protocol.Actor { return actor }
	return tbl
}

// parityPipeServer stands up the daemon half of an in-memory pipe: one
// newline-JSON-RPC frame at a time, dispatched through the real table. The
// client's framing, handshake and id correlation are asserted ON THE WIRE —
// the same shape the real daemon answers with (result on success, the typed
// Aeon error on refusal, JSON-RPC's own -32603 for anything else).
func parityPipeServer(t *testing.T, tbl *protocol.Table) net.Conn {
	t.Helper()
	cr, cs := net.Pipe()
	t.Cleanup(func() { _ = cr.Close(); _ = cs.Close() })
	go func() {
		defer cs.Close()
		br := bufio.NewReader(cs)
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
				continue // a notification, or a frame the server should not answer
			}
			res, derr := tbl.Dispatch(context.Background(), "c-parity", protocol.MethodName(req.Method), req.Params)
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
			if werr := json.NewEncoder(cs).Encode(resp); werr != nil {
				return
			}
		}
	}()
	return cr
}

// paritySocketDoor is the REAL socket door over a REAL protocol.Client on an
// in-memory pipe — the handshake performed, so a test that skipped initialize
// would fail here rather than prove the protocol does.
func paritySocketDoor(t *testing.T, tbl *protocol.Table) autonomyDoor {
	t.Helper()
	c := protocol.NewClient(parityPipeServer(t, tbl), "parity-test", "0")
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake over the in-memory pipe: %v", err)
	}
	return autonomySocketDoor{c: c}
}

// recordingAutonomyDoor is the door fake for the seatbelt tests: it records
// which methods were called (a call without --yes is the offense) and the
// exact grant/revoke params that crossed it.
type recordingAutonomyDoor struct {
	mu         sync.Mutex
	calls      []string
	lastGrant  protocol.AutonomyGrantParams
	lastRevoke protocol.AutonomyRevokeParams
}

func (r *recordingAutonomyDoor) note(method string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, method)
}

func (r *recordingAutonomyDoor) called() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.calls...)
}

func (r *recordingAutonomyDoor) list(context.Context, protocol.AutonomyListParams) (protocol.AutonomyListResult, error) {
	r.note("list")
	return protocol.AutonomyListResult{}, nil
}

func (r *recordingAutonomyDoor) show(context.Context, protocol.AutonomyShowParams) (protocol.AutonomyShowResult, error) {
	r.note("show")
	return protocol.AutonomyShowResult{}, nil
}

func (r *recordingAutonomyDoor) vocabulary(context.Context) (protocol.VocabularyView, error) {
	r.note("vocabulary")
	return protocol.VocabularyView{}, nil
}

func (r *recordingAutonomyDoor) effective(context.Context, protocol.AutonomyEffectiveParams) (protocol.EffectiveView, error) {
	r.note("effective")
	return protocol.EffectiveView{}, nil
}

func (r *recordingAutonomyDoor) pinned(context.Context, protocol.AutonomyPinnedParams) (protocol.PinnedView, error) {
	r.note("pinned")
	return protocol.PinnedView{}, nil
}

func (r *recordingAutonomyDoor) changes(context.Context, protocol.AutonomyChangesParams) (protocol.AutonomyChangesResult, error) {
	r.note("changes")
	return protocol.AutonomyChangesResult{}, nil
}

func (r *recordingAutonomyDoor) uses(context.Context, protocol.AutonomyUsesParams) (protocol.AutonomyUsesResult, error) {
	r.note("uses")
	return protocol.AutonomyUsesResult{}, nil
}

func (r *recordingAutonomyDoor) grant(_ context.Context, p protocol.AutonomyGrantParams) (protocol.AutonomyGrantResult, error) {
	r.note("grant")
	r.mu.Lock()
	r.lastGrant = p
	r.mu.Unlock()
	return protocol.AutonomyGrantResult{Confirmation: "granted (recording)"}, nil
}

func (r *recordingAutonomyDoor) deny(context.Context, protocol.AutonomyDenyParams) (protocol.AutonomyDenyResult, error) {
	r.note("deny")
	return protocol.AutonomyDenyResult{Confirmation: "denied (recording)"}, nil
}

func (r *recordingAutonomyDoor) revoke(_ context.Context, p protocol.AutonomyRevokeParams) (protocol.AutonomyRevokeResult, error) {
	r.note("revoke")
	r.mu.Lock()
	r.lastRevoke = p
	r.mu.Unlock()
	return protocol.AutonomyRevokeResult{Confirmation: "revoked (recording)"}, nil
}

// noDaemonErr is the dial's own no-daemon wrap shape (dial.go): the "no
// daemon at" prefix around a wrapped connection error. isNoDaemon must read
// it as "nobody is home".
func noDaemonErr() error {
	return fmt.Errorf("no daemon at /fake/rpc.sock: %w\n  start one with `aeon serve`", os.ErrNotExist)
}

// stubAutonomyDial stands in the selection's dial; returns the restore.
func stubAutonomyDial(t *testing.T, err error) func() {
	t.Helper()
	orig := autonomyDial
	autonomyDial = func(int) (*protocol.Client, error) { return nil, err }
	return func() { autonomyDial = orig }
}

// stubDirectDoorBuilder stands in the direct-door construction and spies on
// it: a build is RECORDED and answered with the real (unwired, never
// exercised) door type — so a verb that falls back gets a door, and a test
// that asserts the spy was NOT called proves the build never happened.
func stubDirectDoorBuilder(t *testing.T, spy func()) func() {
	t.Helper()
	orig := newAutonomyDirectDoorFunc
	newAutonomyDirectDoorFunc = func(context.Context) (autonomyDoor, error) {
		spy()
		return autonomyDirectDoor{}, nil
	}
	return func() { newAutonomyDirectDoorFunc = orig }
}

// firstField is the fixed-width first column of a help line ("  %-28s …").
func firstField(s string, width int) string {
	if len(s) < width {
		return s
	}
	return s[:width]
}

func setEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// --- parity, both directions (T0 §3's table) --------------------------------

// TestEveryAutonomyVerbHasASocketMethod: the CLI must not LEAD the daemon —
// every verb names a method that is actually registered on the socket.
func TestEveryAutonomyVerbHasASocketMethod(t *testing.T) {
	if len(autonomyVerbs) != 10 {
		t.Fatalf("the parity table carries %d entries — the closed set is ten (T0 §3)", len(autonomyVerbs))
	}
	tbl := newAutonomyTable(t, true)
	for _, e := range autonomyVerbs {
		if _, ok := tbl.Lookup(e.method); !ok {
			t.Errorf("verb %q names %s, which is not registered on the socket — the CLI leads the daemon", e.verb, e.method)
		}
	}
}

// TestEveryAutonomyMethodHasAVerb: the CLI must not LAG the daemon — every
// autonomy.* method on the socket has a verb, no exceptions.
func TestEveryAutonomyMethodHasAVerb(t *testing.T) {
	tbl := newAutonomyTable(t, true)
	verbMethods := map[protocol.MethodName]bool{}
	for _, e := range autonomyVerbs {
		verbMethods[e.method] = true
	}
	for _, m := range tbl.Methods() {
		if strings.HasPrefix(string(m), "autonomy.") && !verbMethods[m] {
			t.Errorf("%s is registered on the socket but has no CLI verb — the CLI lags the daemon (T0 §3's table is the contract)", m)
		}
	}
}

// --- the selection: the offline asymmetry ------------------------------------

// TestSelectDoorGrantNeverFallsBack: with no daemon, grant is refused with
// the fixed sentence and the direct door is NEVER built; the other nine
// verbs take the direct door and the fixed stderr line.
func TestSelectDoorGrantNeverFallsBack(t *testing.T) {
	restoreDial := stubAutonomyDial(t, noDaemonErr())
	defer restoreDial()
	directBuilt := false
	restoreNew := stubDirectDoorBuilder(t, func() { directBuilt = true })
	defer restoreNew()

	// grant: the fixed sentence, verbatim, and no door at all.
	_, err := selectAutonomyDoor("grant", io.Discard)
	if err == nil || err.Error() != autonomyGrantOfflineRefusal {
		t.Fatalf("grant offline: err = %v, want the fixed sentence %q verbatim", err, autonomyGrantOfflineRefusal)
	}
	if directBuilt {
		t.Fatal("grant fell back to the direct door — a widening was written offline (AL13)")
	}

	// The other nine: the direct door, and the fixed stderr line.
	for _, verb := range autonomyVerbNames() {
		if verb == "grant" {
			continue
		}
		directBuilt = false
		var stderr bytes.Buffer
		_, err := selectAutonomyDoor(verb, &stderr)
		if err != nil {
			t.Fatalf("verb %q offline: %v — a read or a restrictive verb must fall back", verb, err)
		}
		if !directBuilt {
			t.Errorf("verb %q: the direct door was not built — the fallback lost its door", verb)
		}
		if !strings.Contains(stderr.String(), autonomyDirectLine) {
			t.Errorf("verb %q: stderr = %q, want the fixed line %q", verb, stderr.String(), autonomyDirectLine)
		}
	}
}

// TestSelectDoorRefusesOtherDialFailures: a dial fault that is NOT "nobody
// is home" — the socket exists and the operator may not connect (permission),
// or the daemon is there and disagreeing (version skew) — is refused for
// every verb, with no fallback and no fallback line.
func TestSelectDoorRefusesOtherDialFailures(t *testing.T) {
	directBuilt := false
	restoreNew := stubDirectDoorBuilder(t, func() { directBuilt = true })
	defer restoreNew()

	faults := map[string]error{
		"permission-denied": fmt.Errorf("no daemon at /fake/rpc.sock: %w\n  start one with `aeon serve`",
			&net.OpError{Op: "dial", Net: "unix", Err: os.ErrPermission}),
		"version-mismatch": errors.New("protocol version mismatch: the daemon speaks 4, this client needs 5"),
	}
	for name, fault := range faults {
		t.Run(name, func(t *testing.T) {
			restoreDial := stubAutonomyDial(t, fault)
			defer restoreDial()
			for _, verb := range autonomyVerbNames() {
				directBuilt = false
				var stderr bytes.Buffer
				_, err := selectAutonomyDoor(verb, &stderr)
				if err == nil {
					t.Errorf("verb %q: the %s dial fault succeeded — a fault that is not \"nobody is home\" must be refused", verb, name)
				}
				if directBuilt {
					t.Errorf("verb %q: fell back to the direct door from a %s fault — routing around a daemon that exists (AL13)", verb, name)
				}
				if strings.Contains(stderr.String(), autonomyDirectLine) {
					t.Errorf("verb %q: printed the fallback line for a %s fault — that line is for \"no daemon\" only", verb, name)
				}
			}
		})
	}
}

// --- the direct door is the service, not a copy -------------------------------

// TestDirectDoorUsesTheSameFunctions is the spec-named go/parser guard: the
// direct door's constructor opens the real store and builds the daemon's own
// service, each of its ten methods speaks that service through d.s (one
// engine, two doors), and the five grant-table needles never appear in the
// file (comments stripped — the header's mention of them is prose, not a
// door).
func TestDirectDoorUsesTheSameFunctions(t *testing.T) {
	src, err := os.ReadFile("autonomy_client.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "autonomy_client.go", src, 0)
	if perr != nil {
		t.Fatalf("parse autonomy_client.go: %v", perr)
	}

	// (1) the constructor: store.Open + daemon.NewAutonomyService.
	var ctor *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "newAutonomyDirectDoor" {
			ctor = fd
		}
	}
	if ctor == nil {
		t.Fatal("newAutonomyDirectDoor is gone — the direct door's constructor moved or was renamed")
	}
	ctorCalls := map[string]bool{}
	ast.Inspect(ctor.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		se, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := se.X.(*ast.Ident); ok {
			ctorCalls[x.Name+"."+se.Sel.Name] = true
		}
		return true
	})
	for _, want := range []string{"store.Open", "daemon.NewAutonomyService"} {
		if !ctorCalls[want] {
			t.Errorf("the constructor does not call %s — the direct door no longer builds the real service (it is the service, not a copy)", want)
		}
	}

	// (2) every method: exactly one d.s.<seam-method> call, and the right one.
	wantSeam := map[string]string{
		"list": "List", "show": "Show", "vocabulary": "Vocabulary",
		"effective": "Effective", "pinned": "Pinned", "changes": "Changes", "uses": "Uses",
		"grant": "Grant", "deny": "Deny", "revoke": "Revoke",
	}
	seenMethods := 0
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		rt := fd.Recv.List[0].Type
		if st, ok := rt.(*ast.StarExpr); ok {
			rt = st.X
		}
		id, ok := rt.(*ast.Ident)
		if !ok || id.Name != "autonomyDirectDoor" {
			continue
		}
		seenMethods++
		want, known := wantSeam[fd.Name.Name]
		if !known {
			t.Errorf("the direct door carries a method %q that is not one of the ten seam methods", fd.Name.Name)
			continue
		}
		var seamCalls []string
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			se, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if inner, ok := se.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "s" {
				if x, ok := inner.X.(*ast.Ident); ok && x.Name == "d" {
					seamCalls = append(seamCalls, se.Sel.Name)
				}
			}
			return true
		})
		if len(seamCalls) != 1 || seamCalls[0] != want {
			t.Errorf("method %s speaks the service as %v — want exactly d.s.%s (one engine, two doors)", fd.Name.Name, seamCalls, want)
		}
	}
	if seenMethods != 10 {
		t.Errorf("the direct door carries %d methods — the closed set is ten", seenMethods)
	}

	// (3) the grant-table needles: zero crossings, comments stripped.
	if n := grantTableCrossings(string(src)); n != 0 {
		t.Errorf("autonomy_client.go crosses the grant-table boundary %d time(s) — the direct door must not name the store's write doors itself", n)
	}
}

// --- one refusal renderer, both doors ----------------------------------------

// TestCLIRefusalsVerbatimBothDoors: the same refusal, rendered through both
// doors, is the same *protocol.Error byte-for-byte — the socket handler's
// autonomyRefused and the CLI's autonomyDoorRefusal are one renderer (the
// ErrAutonomyRefused family, the sentence verbatim in `what`). A second
// phrasing would be a second truth.
func TestCLIRefusalsVerbatimBothDoors(t *testing.T) {
	ref := &protocol.AutonomyRefusal{
		Code:     "subject-unknown",
		Sentence: "refused: subject \"forge.nope\" is not on the registry's closed set",
	}
	peer := protocol.Actor{Kind: "user", ID: "uid-7"}

	t.Run("show", func(t *testing.T) {
		var direct, socket *protocol.Error
		_, err := (autonomyDirectDoor{s: &fakeAutonomyService{refusal: ref}}).show(context.Background(), protocol.AutonomyShowParams{ID: "g-1"})
		if !errors.As(err, &direct) {
			t.Fatalf("direct door's refusal is %T: %v, want *protocol.Error", err, err)
		}
		so := paritySocketDoor(t, newFakeTable(t, &fakeAutonomyService{refusal: ref}, peer))
		_, err = so.show(context.Background(), protocol.AutonomyShowParams{ID: "g-1"})
		if !errors.As(err, &socket) {
			t.Fatalf("socket door's refusal is %T: %v, want *protocol.Error", err, err)
		}
		assertSameRefusalError(t, direct, socket, ref)
	})

	t.Run("grant", func(t *testing.T) {
		// Over the socket the actor door (lock 2) passes first — the fake's
		// refusal arrives after the stamped user actor, so the identity
		// difference between the doors cannot touch the wording.
		params := protocol.AutonomyGrantParams{RootID: "r1", Subject: "forge.openPR"}
		var direct, socket *protocol.Error
		_, err := (autonomyDirectDoor{s: &fakeAutonomyService{refusal: ref}}).grant(context.Background(), params)
		if !errors.As(err, &direct) {
			t.Fatalf("direct door's refusal is %T: %v, want *protocol.Error", err, err)
		}
		so := paritySocketDoor(t, newFakeTable(t, &fakeAutonomyService{refusal: ref}, peer))
		_, err = so.grant(context.Background(), params)
		if !errors.As(err, &socket) {
			t.Fatalf("socket door's refusal is %T: %v, want *protocol.Error", err, err)
		}
		assertSameRefusalError(t, direct, socket, ref)
	})
}

func assertSameRefusalError(t *testing.T, a, b *protocol.Error, ref *protocol.AutonomyRefusal) {
	t.Helper()
	if a.Code != protocol.ErrAutonomyRefused.Code {
		t.Errorf("refusal code = %d, want %d (the autonomy refusal family)", a.Code, protocol.ErrAutonomyRefused.Code)
	}
	if a.Data.What != ref.Sentence {
		t.Errorf("the refusal's what = %q, want the sentence verbatim %q", a.Data.What, ref.Sentence)
	}
	ab, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	if aerr != nil || berr != nil {
		t.Fatalf("marshal the refusals: %v / %v", aerr, berr)
	}
	if !bytes.Equal(ab, bb) {
		t.Errorf("the doors render the same refusal differently (a door-dependent wording):\n direct: %s\n socket: %s", ab, bb)
	}
}

// --- the actor: cli on the direct door only -----------------------------------

// TestCLIActorIsCliOnTheDirectDoorOnly: the direct door writes as the fixed
// {cli, cli} — audited actor_kind='cli', which the store admits for the
// restrictive writes and refuses for allow (the backstop). The socket door
// writes as the daemon's stamped peer; the CLI sends no actor, and the
// protocol's identity-key check refuses one smuggled in params (lock 2).
func TestCLIActorIsCliOnTheDirectDoorOnly(t *testing.T) {
	direct := &fakeAutonomyService{}
	if _, err := (autonomyDirectDoor{s: direct}).deny(context.Background(), protocol.AutonomyDenyParams{NodeID: "n1", Subject: "forge.openPR"}); err != nil {
		t.Fatalf("direct deny: %v", err)
	}
	if got, ok := direct.lastActor(); !ok || got != (protocol.Actor{Kind: "cli", ID: "cli"}) {
		t.Errorf("the direct door's deny actor = %+v, want the fixed {cli, cli} (audited actor_kind='cli')", got)
	}
	directGrant := &fakeAutonomyService{}
	if _, err := (autonomyDirectDoor{s: directGrant}).grant(context.Background(), protocol.AutonomyGrantParams{RootID: "r1", Subject: "forge.openPR"}); err != nil {
		t.Fatalf("direct grant (the backstop path): %v", err)
	}
	if got, ok := directGrant.lastActor(); !ok || got != (protocol.Actor{Kind: "cli", ID: "cli"}) {
		t.Errorf("the direct door's grant actor = %+v, want {cli, cli} — the backstop speaks the service with the same identity", got)
	}

	peer := protocol.Actor{Kind: "user", ID: "uid-7"}
	fake := &fakeAutonomyService{}
	so := paritySocketDoor(t, newFakeTable(t, fake, peer))
	if _, err := so.deny(context.Background(), protocol.AutonomyDenyParams{NodeID: "n1", Subject: "forge.openPR"}); err != nil {
		t.Fatalf("socket deny: %v", err)
	}
	if got, ok := fake.lastActor(); !ok || got != peer {
		t.Errorf("the socket door's actor = %+v, want the daemon's {user, uid-7} — the CLI must not send one", got)
	}
}

// --- the help is generated from the vocabulary --------------------------------

// TestHelpIsGeneratedFromVocabulary: the help's subject and condition lists
// are set-equal to the pure autonomy.Vocabulary() — BUILT, not typed (a
// registry action added later appears with no edit in the CLI). And no
// forge.* token in the help names a subject the vocabulary does not carry:
// a hard-coded subject is a second source.
func TestHelpIsGeneratedFromVocabulary(t *testing.T) {
	var out bytes.Buffer
	printAutonomyUsage(&out)
	text := out.String()
	v := autonomy.Vocabulary()
	if len(v.Subjects) == 0 || len(v.Conditions) == 0 {
		t.Fatal("autonomy.Vocabulary() is empty — the help check would pass on nothing")
	}

	const subjHdr = "Subjects --action may name"
	const condHdr = "Conditions a grant may carry"
	subjStart := strings.Index(text, subjHdr)
	condStart := strings.Index(text, condHdr)
	if subjStart < 0 || condStart < 0 || condStart < subjStart {
		t.Fatalf("the help lost its subject/condition headers (positions %d / %d)", subjStart, condStart)
	}

	// Subject lines are "  %-28s …": the name is the first 28-column field.
	helpSubjects := map[string]bool{}
	for _, line := range strings.Split(text[subjStart:condStart], "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue // the header line and blanks
		}
		if name := strings.TrimSpace(firstField(line[2:], 28)); name != "" {
			helpSubjects[name] = true
		}
	}
	wantSubjects := map[string]bool{}
	for _, s := range v.Subjects {
		wantSubjects[s.Name] = true
	}
	if !setEqual(helpSubjects, wantSubjects) {
		t.Errorf("the help's subject list drifted from the vocabulary:\n help: %v\n vocabulary: %v", keys(helpSubjects), keys(wantSubjects))
	}

	// Condition lines are "  %-16s …": the name is the first 16-column field.
	helpConditions := map[string]bool{}
	for _, line := range strings.Split(text[condStart:], "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		if name := strings.TrimSpace(firstField(line[2:], 16)); name != "" {
			helpConditions[name] = true
		}
	}
	wantConditions := map[string]bool{}
	for _, c := range v.Conditions {
		wantConditions[c.Name] = true
	}
	if !setEqual(helpConditions, wantConditions) {
		t.Errorf("the help's condition list drifted from the vocabulary:\n help: %v\n vocabulary: %v", keys(helpConditions), keys(wantConditions))
	}

	// No subject anywhere in the help that the vocabulary does not carry.
	for _, m := range regexp.MustCompile(`forge\.\w+`).FindAllString(text, -1) {
		if !wantSubjects[m] {
			t.Errorf("the help names %q, which the vocabulary does not carry — a hard-coded subject is a second source (AL15)", m)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- the seatbelt --------------------------------------------------------------

// TestGrantRequiresYes: a widening that was not confirmed is refused BEFORE
// any door is asked (the daemon, and its audit, are never told about it), and
// with --yes the door is asked with DryRun false — the CLI has no --dry-run
// flag and must not invent one. The conditions pointer semantics ride along:
// no condition flag ⇒ nil (the daemon's whole-object default applies); any
// one flag ⇒ the object with the flags' values verbatim.
func TestGrantRequiresYes(t *testing.T) {
	orig := autonomyDoorFactory
	defer func() { autonomyDoorFactory = orig }()

	rec := &recordingAutonomyDoor{}
	autonomyDoorFactory = func(string) (autonomyDoor, error) { return rec, nil }
	var out bytes.Buffer
	err := autonomyGrant(&out, []string{"--root", "r1", "--action", "forge.openPR"})
	if err == nil {
		t.Fatal("grant without --yes succeeded — the seatbelt is gone")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("the refusal does not name the flag: %q", err)
	}
	if got := rec.called(); len(got) != 0 {
		t.Errorf("the door was called %v — an unconfirmed widening must not be asked at all", got)
	}

	recYes := &recordingAutonomyDoor{}
	autonomyDoorFactory = func(string) (autonomyDoor, error) { return recYes, nil }
	if err := autonomyGrant(&out, []string{"--root", "r1", "--action", "forge.openPR", "--yes"}); err != nil {
		t.Fatalf("grant with --yes: %v", err)
	}
	if got := recYes.called(); len(got) != 1 || got[0] != "grant" {
		t.Fatalf("the door calls = %v, want [grant]", got)
	}
	if gp := recYes.lastGrant; gp.DryRun {
		t.Error("DryRun is true on the wire — the CLI has no --dry-run flag and must not invent one")
	}
	if gp := recYes.lastGrant; gp.RootID != "r1" || gp.Subject != "forge.openPR" {
		t.Errorf("the grant params crossed the door as %+v — root and action were not carried", gp)
	}
	if gp := recYes.lastGrant; gp.Conditions != nil {
		t.Errorf("Conditions = %+v, want nil — no condition flag was named, so the daemon's whole-object default must apply", *gp.Conditions)
	}

	recConds := &recordingAutonomyDoor{}
	autonomyDoorFactory = func(string) (autonomyDoor, error) { return recConds, nil }
	if err := autonomyGrant(&out, []string{"--root", "r1", "--action", "forge.openPR", "--yes", "--require-gates"}); err != nil {
		t.Fatalf("grant with --require-gates: %v", err)
	}
	if gp := recConds.lastGrant; gp.Conditions == nil || !gp.Conditions.RequireGates || gp.Conditions.RequireBudget {
		t.Errorf("Conditions = %+v — a named flag sends the object with its VALUES verbatim (requireBudget off when only gates is named)", gp.Conditions)
	}
}

// TestRevokeRequiresYes: a revoked grant is never re-activated, so the
// revocation is refused without --yes, before any door is asked; with it, the
// door is asked with the id and the reason, DryRun false.
func TestRevokeRequiresYes(t *testing.T) {
	orig := autonomyDoorFactory
	defer func() { autonomyDoorFactory = orig }()

	rec := &recordingAutonomyDoor{}
	autonomyDoorFactory = func(string) (autonomyDoor, error) { return rec, nil }
	var out bytes.Buffer
	err := autonomyRevoke(&out, []string{"g-1", "--reason", "scope creep"})
	if err == nil {
		t.Fatal("revoke without --yes succeeded — the seatbelt is gone")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("the refusal does not name the flag: %q", err)
	}
	if got := rec.called(); len(got) != 0 {
		t.Errorf("the door was called %v — an unconfirmed revocation must not be asked at all", got)
	}

	recYes := &recordingAutonomyDoor{}
	autonomyDoorFactory = func(string) (autonomyDoor, error) { return recYes, nil }
	if err := autonomyRevoke(&out, []string{"g-1", "--reason", "scope creep", "--yes"}); err != nil {
		t.Fatalf("revoke with --yes: %v", err)
	}
	if got := recYes.called(); len(got) != 1 || got[0] != "revoke" {
		t.Fatalf("the door calls = %v, want [revoke]", got)
	}
	if rp := recYes.lastRevoke; rp.ID != "g-1" || rp.Reason != "scope creep" || rp.DryRun {
		t.Errorf("the revoke params crossed the door as %+v — id and reason carried, DryRun false", rp)
	}
}

// --- the import boundary --------------------------------------------------------

// TestCLINeverImportsTheStoreOutsideTheFallback: the verbs (autonomy.go)
// cannot reopen the store — internal/store and internal/daemon are imported
// by the CLI's autonomy files ONLY in autonomy_client.go, where the documented
// read/restrictive fallback lives. A verb that re-imports the store is a
// second door.
func TestCLINeverImportsTheStoreOutsideTheFallback(t *testing.T) {
	files, err := filepath.Glob("autonomy*.go")
	if err != nil {
		t.Fatal(err)
	}
	seenFallback := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		pf, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", f, perr)
		}
		imports := map[string]bool{}
		for _, imp := range pf.Imports {
			imports[strings.Trim(imp.Path.Value, `"`)] = true
		}
		if f == "autonomy_client.go" {
			if !imports["github.com/cjlapao/aeon/internal/store"] {
				t.Errorf("%s no longer imports the store — the documented direct fallback is gone (AL13)", f)
			} else {
				seenFallback = true
			}
			continue
		}
		for _, forbidden := range []string{
			"github.com/cjlapao/aeon/internal/store",
			"github.com/cjlapao/aeon/internal/daemon",
		} {
			if imports[forbidden] {
				t.Errorf("%s imports %s — the verbs reach the store only through the documented fallback in autonomy_client.go", f, forbidden)
			}
		}
	}
	if !seenFallback {
		t.Error("no autonomy file imports the store — the documented direct fallback is gone (AL13)")
	}
}

// --- --json is the wire ---------------------------------------------------------

// TestAutonomyEffectiveJSONIsTheWire: `effective --json` over the socket door
// prints the wire result verbatim — the bytes on stdout equal the JSON of the
// EffectiveView the seam returned. A re-formatted result is a second truth.
func TestAutonomyEffectiveJSONIsTheWire(t *testing.T) {
	view := protocol.EffectiveView{
		RootID:      "r1",
		Origin:      protocol.OriginSchedule,
		EvaluatedAt: "2026-11-01T12:00:00Z",
		Actions: []protocol.EffectiveAction{
			{
				Action:  "forge.merge",
				Class:   "outward",
				Verdict: "deny",
				Because: "no grant admits it",
				Conditions: []protocol.EffectiveCond{
					{Name: "requireBudget", State: "unknown", Sentence: "the budget could not be asked"},
				},
			},
		},
		Statement: "an unattended loop on r1 started now may do nothing outward",
	}
	fake := &fakeAutonomyService{effectiveView: view}
	so := paritySocketDoor(t, newFakeTable(t, fake, protocol.Actor{Kind: "user", ID: "uid-7"}))

	orig := autonomyDoorFactory
	defer func() { autonomyDoorFactory = orig }()
	autonomyDoorFactory = func(string) (autonomyDoor, error) { return so, nil }

	var out bytes.Buffer
	if err := autonomyEffective(&out, []string{"--root", "r1", "--json"}); err != nil {
		t.Fatalf("effective --json: %v", err)
	}
	want, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want)+"\n" {
		t.Errorf("effective --json is not the wire:\n got:  %s\n want: %s", out.String(), want)
	}
}
