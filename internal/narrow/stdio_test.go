package narrow

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// fakeCaller is the socket seen from the mounting: canned results on the
// test's clock, recorded calls, and per-method delays — the delays are what
// let the concurrency gates (a slow read must never hold anything behind
// it) be asserted on timing rather than hoped for.
type fakeCaller struct {
	mu       sync.Mutex
	calls    []string
	inflight int
	peak     int
	delays   map[protocol.MethodName]time.Duration
	errs     map[protocol.MethodName]error
}

func newFakeCaller() *fakeCaller {
	return &fakeCaller{
		delays: map[protocol.MethodName]time.Duration{},
		errs:   map[protocol.MethodName]error{},
	}
}

func (f *fakeCaller) Call(method protocol.MethodName, params, result any) error {
	f.mu.Lock()
	f.calls = append(f.calls, string(method))
	f.inflight++
	if f.inflight > f.peak {
		f.peak = f.inflight
	}
	delay := f.delays[method]
	failed := f.errs[method]
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	f.mu.Lock()
	f.inflight--
	f.mu.Unlock()

	if failed != nil {
		return failed
	}
	if p, ok := result.(*any); ok {
		*p = map[string]any{"id": "loop-77", "method": string(method), "echo": json.RawMessage(mustJSON(params))}
	}
	return nil
}

func (f *fakeCaller) peakConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peak
}

func (f *fakeCaller) calledMethods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// harness drives the server over an in-memory pipe: requests in one end,
// responses collected from the other, with ids as the correlation key.
// Replies that are not the one being waited for are stashed, so
// concurrent requests interleave cleanly instead of deadlocking the test.
type harness struct {
	t       *testing.T
	in      io.WriteCloser
	replies chan map[string]any
	pending map[string]map[string]any
}

func start(t *testing.T, caller Caller, table []Action) *harness {
	t.Helper()
	cReq, sReq := io.Pipe() // test writes → server reads
	cOut, sOut := io.Pipe() // server writes → test reads
	srv := NewServer(caller, table)
	go func() { _ = srv.Serve(nil, cReq, sOut) }()
	h := &harness{t: t, in: sReq, replies: make(chan map[string]any, 64), pending: map[string]map[string]any{}}
	go func() {
		sc := bufio.NewScanner(cOut)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
				h.replies <- map[string]any{"_malformed": sc.Text()}
				continue
			}
			h.replies <- msg
		}
	}()
	t.Cleanup(func() { _ = h.in.Close() })
	return h
}

func (h *harness) send(raw string) {
	h.t.Helper()
	if _, err := fmt.Fprintln(h.in, raw); err != nil {
		h.t.Fatalf("send: %v", err)
	}
}

func (h *harness) next(wantID any) map[string]any {
	h.t.Helper()
	key := mustJSON(wantID)
	if msg, ok := h.pending[key]; ok {
		delete(h.pending, key)
		return msg
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-h.replies:
			if _, bad := msg["_malformed"]; bad {
				h.t.Fatalf("server emitted a malformed response line: %v", msg)
			}
			if mustJSON(msg["id"]) == key {
				return msg
			}
			h.pending[mustJSON(msg["id"])] = msg
		case <-deadline:
			h.t.Fatalf("no reply to id %v within 5s", wantID)
		}
	}
}

func req(id any, method string, params any) string {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	return mustJSON(m)
}

func initializeAndList(t *testing.T, h *harness) []map[string]any {
	t.Helper()
	h.send(req(1, "initialize", map[string]any{"protocolVersion": mcpProtocolVersion, "capabilities": map[string]any{}}))
	initRes := h.next(1)
	if initRes["error"] != nil {
		t.Fatalf("initialize failed: %v", initRes["error"])
	}
	h.send(req(2, "tools/list", nil))
	list := h.next(2)
	if list["error"] != nil {
		t.Fatalf("tools/list failed: %v", list["error"])
	}
	result, ok := list["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list result shape: %v", list)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list has no tools array: %v", result)
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tv := range tools {
		out = append(out, tv.(map[string]any))
	}
	return out
}

func toolNames(tools []map[string]any) []string {
	var names []string
	for _, tl := range tools {
		names = append(names, tl["name"].(string))
	}
	return names
}

// TestStdioExposesTheWholeTable — both directions: every table entry
// appears as a tool, and NOTHING ELSE does. A tool the table lacks is the
// third surface arriving by a different route (N18).
func TestStdioExposesTheWholeTable(t *testing.T) {
	h := start(t, newFakeCaller(), Actions())
	tools := initializeAndList(t, h)
	table := Actions()
	if len(tools) != len(table) {
		t.Fatalf("emitted %d tools, the table has %d entries", len(tools), len(table))
	}
	emitted := map[string]bool{}
	for _, n := range toolNames(tools) {
		emitted[n] = true
	}
	for _, a := range table {
		if !emitted[a.Name] {
			t.Errorf("table entry %s is not a tool", a.Name)
		}
		delete(emitted, a.Name)
	}
	for extra := range emitted {
		t.Errorf("tool %q is not in the table — a mounting-side tool is a third surface (N18)", extra)
	}
}

// TestBoundaryReachesTheClient — the statement is in the field the MCP
// specification says clients surface (initialize.result.instructions, fed
// to the model as context) and echoed on every tool's description. The
// walk against real Claude Code (demo §2, and claude IS installed on this
// box — plan F-6) is the human-half verification; this gate is the
// machine-half. The cmd wiring is pinned too: the verb exists and mounts
// this server, not a lookalike.
func TestBoundaryReachesTheClient(t *testing.T) {
	h := start(t, newFakeCaller(), Actions())
	h.send(req(1, "initialize", map[string]any{"protocolVersion": mcpProtocolVersion}))
	initRes := h.next(1)
	result, ok := initRes["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result shape: %v", initRes)
	}
	instructions, _ := result["instructions"].(string)
	if instructions != BoundaryStatement() {
		t.Errorf("initialize.instructions must carry the boundary statement verbatim:\n want:\n%s\n got:\n%s", BoundaryStatement(), instructions)
	}
	for _, want := range []string{"no operational control", "not restricted, absent", "aeon_loop_start (trigger)"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("instructions must contain %q", want)
		}
	}
	tools := initializeAndList(t, h)
	for _, tl := range tools {
		if !strings.Contains(tl["description"].(string), BoundaryAbsentHint) {
			t.Errorf("tool %s: the one-line boundary must ride every description — a client that never reads instructions still learns it from the list", tl["name"])
		}
	}
	// The cmd wiring, pinned at the source (same shape as T2's serve.go
	// gate): `aeon mcp serve` mounts THIS server with the real table and
	// the socket client.
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", "mcp.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{`case "serve":`, "narrow.NewServer", "narrow.Actions()"} {
		if !strings.Contains(src, want) {
			t.Errorf("cmd/aeon/mcp.go must contain %s — the verb and this mounting are one mechanism", want)
		}
	}
}

// TestNoOperationalToolIsListed — the T1 enumeration, re-run ON THE
// EMITTED LIST rather than on the table the mounting read. Names carry the
// check (descriptions legitimately speak of what tools cannot do).
func TestNoOperationalToolIsListed(t *testing.T) {
	h := start(t, newFakeCaller(), Actions())
	tools := initializeAndList(t, h)
	forbidden := map[string]bool{}
	for _, w := range ForbiddenOperationalControl {
		forbidden[w] = true
	}
	for _, tl := range tools {
		for _, tok := range tokenize(tl["name"].(string)) {
			if forbidden[tok] {
				t.Errorf("emitted tool %q carries operational-control token %q", tl["name"], tok)
			}
		}
	}
}

// TestCannotIsInTheDescription — N12's legibility half for the model that
// reads tool descriptions: a tool whose limits live only in this
// repository is a tool a model will reach for wrongly.
func TestCannotIsInTheDescription(t *testing.T) {
	h := start(t, newFakeCaller(), Actions())
	tools := initializeAndList(t, h)
	byName := map[string]map[string]any{}
	for _, tl := range tools {
		byName[tl["name"].(string)] = tl
	}
	for _, a := range Actions() {
		tl, ok := byName[a.Name]
		if !ok {
			t.Fatalf("%s not listed", a.Name)
		}
		desc := tl["description"].(string)
		for _, cannot := range a.Cannot {
			if !strings.Contains(desc, cannot) {
				t.Errorf("%s: description must contain %q — Cannot reaches the model, not just the test", a.Name, cannot)
			}
		}
		if !strings.Contains(desc, string(a.SocketMethod)) {
			t.Errorf("%s: description names the projected method", a.Name)
		}
	}
}

// TestTriggerReturnsImmediately — a trigger that blocked until completion
// would turn every CI job into a held connection. Asserted on timing: a
// SLOW read is in flight when the trigger arrives, and the trigger still
// answers first, with the loop id.
func TestTriggerReturnsImmediately(t *testing.T) {
	f := newFakeCaller()
	f.delays[protocol.MethodLoopShow] = 2 * time.Second // a read that would hold the line
	h := start(t, f, Actions())
	initializeAndList(t, h)

	h.send(req(10, "tools/call", map[string]any{"name": "aeon_loop_show", "arguments": map[string]any{"id": "loop-1"}}))
	start := time.Now()
	h.send(req(11, "tools/call", map[string]any{"name": "aeon_loop_start", "arguments": map[string]any{"blueprint": "dev-loop", "prompt": "go"}}))
	reply := h.next(11)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("the trigger waited %s behind a slow read — it must answer the start, not the loop", elapsed)
	}
	if reply["error"] != nil {
		t.Fatalf("trigger error: %v", reply["error"])
	}
	result, _ := reply["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("trigger returned no content")
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "loop-77") {
		t.Errorf("the trigger must return the started loop's id, got %q", text)
	}
	// The slow read still completes — nothing was dropped, only not blocking.
	slow := h.next(10)
	if slow["error"] != nil {
		t.Fatalf("slow read failed: %v", slow["error"])
	}
}

// TestReadsDoNotBlockALoop — concurrency, asserted at the caller: while
// two slow reads are in flight, a fast read AND a trigger still complete,
// and the daemon saw overlapping calls (peak concurrency ≥ 3).
func TestReadsDoNotBlockALoop(t *testing.T) {
	f := newFakeCaller()
	f.delays[protocol.MethodLoopShow] = 1500 * time.Millisecond
	f.delays[protocol.MethodStepShow] = 1500 * time.Millisecond
	h := start(t, f, Actions())
	initializeAndList(t, h)

	h.send(req(20, "tools/call", map[string]any{"name": "aeon_loop_show", "arguments": map[string]any{}}))
	h.send(req(21, "tools/call", map[string]any{"name": "aeon_step_show", "arguments": map[string]any{}}))
	time.Sleep(100 * time.Millisecond) // let both slow reads occupy their goroutines
	start := time.Now()
	h.send(req(22, "tools/call", map[string]any{"name": "aeon_loop_list", "arguments": map[string]any{}}))
	h.send(req(23, "tools/call", map[string]any{"name": "aeon_loop_start", "arguments": map[string]any{}}))
	h.next(22)
	h.next(23)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("fast calls took %s while slow reads ran — the mounting serialized them", elapsed)
	}
	if p := f.peakConcurrency(); p < 3 {
		t.Errorf("peak concurrency at the daemon was %d; slow reads are holding the line (expected ≥3 overlapping)", p)
	}
	for _, want := range []string{"loop.list", "loop.start", "loop.show", "step.show"} {
		seen := false
		for _, m := range f.calledMethods() {
			if m == want {
				seen = true
			}
		}
		if !seen {
			t.Errorf("the daemon never saw %s", want)
		}
	}
}

// TestMappingIsMechanical — the gate that keeps the mounting honest. The
// test hands the server a table with one EXTRA entry that production
// never had; it must appear as a tool with no mounting-side change
// (stdio.go is untouched to make it appear — that is the proof). A
// mounting needing a per-action case has stopped rendering the table.
func TestMappingIsMechanical(t *testing.T) {
	table := append(Actions(), Action{
		Name:         "aeon_extra_read",
		SocketMethod: protocol.MethodLoopRead,
		Kind:         KindRead,
		Cannot:       []string{"cannot do anything else"},
	})
	h := start(t, newFakeCaller(), table)
	tools := initializeAndList(t, h)
	found := false
	for _, tl := range tools {
		if tl["name"] == "aeon_extra_read" {
			found = true
			if !strings.Contains(tl["description"].(string), "cannot do anything else") {
				t.Error("the fixture entry's Cannot must ride its description like any other")
			}
		}
	}
	if !found {
		t.Fatal("a table entry added in the test did not appear as a tool — the mapping is not mechanical")
	}
	// And it is callable through the same one path.
	h.send(req(30, "tools/call", map[string]any{"name": "aeon_extra_read", "arguments": map[string]any{}}))
	if r := h.next(30); r["error"] != nil {
		t.Errorf("fixture tool failed to call: %v", r["error"])
	}
}

// TestMalformedRequestIsAnMCPError — one error response with JSON-RPC's
// own code and a null id, the server alive and correct afterwards. Not a
// crash, and not a partial response.
func TestMalformedRequestIsAnMCPError(t *testing.T) {
	h := start(t, newFakeCaller(), Actions())
	h.send(`{"jsonrpc":"2.0","id":40,"method":"initialize","params":{}}`)
	h.next(40)

	h.send(`this is {{{ not json`)
	bad := h.next(nil) // parse errors answer with id: null
	errObj, ok := bad["error"].(map[string]any)
	if !ok {
		t.Fatalf("malformed input must answer with an error object, got %v", bad)
	}
	if code, _ := errObj["code"].(float64); int(code) != codeParseError {
		t.Errorf("expected parse-error code %d, got %v", codeParseError, errObj["code"])
	}
	if bad["id"] != nil {
		t.Errorf("a parse error carries id null, got %v", bad["id"])
	}
	if _, has := bad["result"]; has {
		t.Error("an error response must not also carry a result (no partial responses)")
	}
	// Alive: a good request after the bad one is answered normally.
	h.send(req(41, "ping", nil))
	if r := h.next(41); r["error"] != nil {
		t.Errorf("the server died on malformed input: %v", r["error"])
	}
	// Unknown method answers with the transport's own language.
	h.send(req(42, "tools/shoot", nil))
	r := h.next(42)
	if errObj, ok := r["error"].(map[string]any); !ok || int(errObj["code"].(float64)) != codeMethodNotFound {
		t.Errorf("unknown method must be a JSON-RPC %d, got %v", codeMethodNotFound, r)
	}
}

// TestEOFDoesNotDropPendingReplies — the drain rule (found by the T5
// smoke, not by a test): a client that pipes a batch and closes stdin
// (the jq-pipeline shape) still expects every answer. Before the fix,
// Serve returned on EOF while the slow call's goroutine was still
// running, and its reply vanished — the request was accepted and
// silently dropped, the worst failure a stdio server has.
func TestEOFDoesNotDropPendingReplies(t *testing.T) {
	c := newFakeCaller()
	c.delays[protocol.MethodLoopList] = 150 * time.Millisecond
	cReq, sReq := io.Pipe()
	// A BUFFER, not a pipe, for the out half: io.Pipe is synchronous, and
	// a pipe here would deadlock the very thing under test (Serve drains
	// its replies while the test waits for Serve — each waiting on the
	// other). The shape under test is "the client is gone; the replies
	// still got written", which a buffer shows exactly.
	var out bytes.Buffer
	srv := NewServer(c, Actions())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(nil, cReq, &out) }()
	if _, err := fmt.Fprintln(sReq, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"aeon_loop_list","arguments":{}}}`); err != nil {
		t.Fatal(err)
	}
	// EOF immediately — mid-handler, which is exactly when the old Serve
	// walked out.
	_ = sReq.Close()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	// Serve returned — AFTER writing the in-flight reply, not before.
	dec := json.NewDecoder(&out)
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("the reply was lost at EOF: %v", err)
	}
	if id, _ := m["id"].(float64); id != 7 {
		t.Fatalf("wrong reply: %v", m)
	}
	if _, ok := m["result"]; !ok {
		t.Fatalf("the drained reply lost its result: %v", m)
	}
}
