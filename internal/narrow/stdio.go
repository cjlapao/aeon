package narrow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// stdio.go is the editor-facing mounting of the narrow surface: an MCP
// (Model Context Protocol) server over stdin/stdout, mounted by
// `aeon mcp serve` (cmd/aeon/mcp.go — the sibling of the client-side
// `list`/`call` verbs S12 shipped, and the reason the daemon imports
// neither internal/mcp nor this package: the server talks to the daemon as
// a socket CLIENT, like every other verb).
//
// The mapping is mechanical, and mechanical is the requirement: every tool
// is one table entry rendered through one function. A per-action special
// case here would mean the table has stopped being the single source, and
// the third surface N18 forbids has arrived by the back door.
//
// There is no auth on this transport — a pipe the parent process already
// owns is the access control (the editor's operator is the user; the socket
// behind this is 0600, P26). What auth exists (T2) belongs to HTTP.

// Caller is the socket client seen from this mounting: one method, the
// whole dependency. The daemon's client (*protocol.Client, after Connect)
// satisfies it; tests fake it to answer canned results on their own clock.
type Caller interface {
	Call(method protocol.MethodName, params, result any) error
}

// mcpProtocolVersion is the MCP revision this server speaks. It is a DATE
// string in MCP's own vocabulary — not to be confused with the daemon
// socket's integer protocolVersion, a different protocol on a different
// pipe (S18's walk learned that the hard way from the other side).
const mcpProtocolVersion = "2025-06-18"

// Server mounts the table as MCP tools over a byte stream. One Server per
// stdio connection; requests are handled concurrently so a slow read can
// never hold a trigger behind it (N8's discipline, asserted by the tests
// that interleave a slow read with a trigger).
type Server struct {
	caller Caller
	table  []Action

	writeMu sync.Mutex
	enc     *json.Encoder

	// inflight counts the per-line goroutines still owed a reply. EOF on
	// stdin is NOT permission to exit while replies are pending: the
	// client that piped a batch and closed its stdin (the jq-pipeline
	// shape, and how the T5 smoke found this) still intends to READ the
	// answers. Serving-then-vanishing is the worst failure a stdio server
	// has — the request was accepted and silently dropped.
	inflight sync.WaitGroup
}

func NewServer(caller Caller, table []Action) *Server {
	return &Server{caller: caller, table: table}
}

// jsonrpc shapes, minimal by design: this server answers the handful of
// methods an MCP client actually sends on stdio (initialize, ping,
// tools/list, tools/call) and JSON-RPC's own codes for the rest.
type rpcIn struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type rpcOut struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result,omitempty"`
	Error   *rpcErr          `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC's own codes — an unknown method answers with the transport's
// language, never with Aeon's vocabulary (the socket's dispatcher does the
// same, and for the same reason: a client that understands the transport
// can understand the refusal).
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Serve reads newline-delimited JSON-RPC from in and answers on out until
// in reaches EOF. Each line is dispatched on its own goroutine and each
// request gets exactly one response — malformed input included (an MCP
// error, never a crash and never a partial response).
func (s *Server) Serve(_ context.Context, in io.Reader, out io.Writer) error {
	s.enc = json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	// Generous line cap: tools/call arguments can legitimately be large
	// (a blueprint to validate), and the default 64KB would turn a big
	// legitimate call into a spurious "token too long".
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...) // the goroutine outlives the buffer
		if len(line) == 0 {
			continue
		}
		s.inflight.Add(1)
		go func() {
			defer s.inflight.Done()
			s.handleLine(line)
		}()
	}
	// Drain before returning: every request the scanner accepted gets its
	// reply written (or its error) before the process the caller is
	// waiting on exits.
	//
	// T7 answers the half T5 left open: the drain is BOUNDED. A caller
	// wedged in the daemon (a dispatch that never returns) must not make
	// EOF itself unreturnable — a stdio process that never exits behind a
	// CI job is its own small N8 failure. The bound is the same threshold
	// the HTTP mounting uses to call a dispatch wedged (callTimeout): a
	// call still in flight past its own deadline is wedged by definition,
	// and returning is what the caller's pipe is waiting for. Late
	// handlers find the writer gone and unwind; nothing writes after
	// Serve returns except those already-dead handlers.
	waited := make(chan struct{})
	go func() {
		s.inflight.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(callTimeout()):
	}
	return sc.Err()
}

func (s *Server) handleLine(line []byte) {
	var req rpcIn
	if err := json.Unmarshal(line, &req); err != nil {
		var missing *json.RawMessage
		s.reply(missing, nil, &rpcErr{Code: codeParseError, Message: "parse error: not valid JSON"})
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.reply(req.ID, nil, &rpcErr{Code: codeInvalidRequest, Message: "invalid request: jsonrpc \"2.0\" and method are required"})
		return
	}
	// A notification (no id) gets no response — JSON-RPC's rule, and the
	// rule whose violation shows up as a client hanging on a reply that
	// should never come.
	isNotification := req.ID == nil
	switch req.Method {
	case "initialize":
		if isNotification {
			return
		}
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		s.reply(req.ID, map[string]any{
			// Echo what the client asked for when it asked for
			// something; answer our revision otherwise.
			"protocolVersion": orDefault(p.ProtocolVersion, mcpProtocolVersion),
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "aeon", "title": "Aeon — the narrow surface", "version": "s19"},
			// N14, and the answer to "the natural place": MCP's
			// initialize result carries `instructions`, the field the
			// spec says clients feed to the model as context — the
			// place a client actually surfaces. The failure mode
			// named in the task ("a statement present in the
			// response that no client displays") is what demo §2
			// verifies against Claude Code itself (installed on
			// this box, plan F-6).
			"instructions": BoundaryStatement(),
		}, nil)
	case "notifications/initialized", "notifications/cancelled":
		return
	case "ping":
		if !isNotification {
			s.reply(req.ID, map[string]any{}, nil)
		}
	case "tools/list":
		if isNotification {
			return
		}
		s.reply(req.ID, s.toolsList(), nil)
	case "tools/call":
		if isNotification {
			return
		}
		s.handleToolCall(req)
	default:
		if !isNotification {
			s.reply(req.ID, nil, &rpcErr{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q is not registered on this surface", req.Method)})
		}
	}
}

// toolsList renders the table — mechanically, the ONE function through
// which every tool arrives.
func (s *Server) toolsList() map[string]any {
	tools := make([]map[string]any, 0, len(s.table))
	for _, a := range s.table {
		tools = append(tools, map[string]any{
			"name":        a.Name,
			"description": a.ToolDescription(),
			"inputSchema": inputSchemaFor(a),
		})
	}
	return map[string]any{"tools": tools}
}

// ToolNames is the emitted list seen as names — the surface `aeon narrow
// list --mounting=stdio` prints and the set-equality diff runs on. It goes
// through toolsList, the same rendering the wire sees: the list a test
// compares is the list a client receives.
func (s *Server) ToolNames() []string {
	tools := s.toolsList()["tools"].([]map[string]any)
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t["name"].(string))
	}
	return names
}

// inputSchemaFor is the mechanical half of the mapping's honesty: a schema
// DERIVED from the entry (its kind and projected method), never a
// per-action hand-written block. The daemon's handlers validate their own
// params — the dispatcher says so at the source — so the schema is open on
// purpose; what it closes is the shape of the answer (an object) and what
// it says (where truth about params lives).
func inputSchemaFor(a Action) map[string]any {
	desc := fmt.Sprintf("Parameters for the daemon method %q. The daemon validates them; aeon's docs (docs/NARROW.md) describe each method.", a.SocketMethod)
	if a.Kind == KindTrigger {
		desc += " Returns immediately with the started loop's id — it does not wait for the loop to finish."
		desc += " The loop is unattended, always: attendedness is forced false here (a CI job is not present in the sense a gate needs, and polling does not make it so). " +
			"An unattended loop that must take OUTWARD actions declares \"allowOutward\":true in this body — per loop, at start, the only outward lever this surface has; " +
			"there is no key-level grant, and allowOutward never grants irreversible."
	}
	if a.Kind == KindWebhook {
		desc += " The delivery must carry a signature that verifies (HMAC over the exact payload bytes) — the signature proves WHO sent it and never WHAT they may cause (governance decides that, after authentication). The payload enters the started loop labelled third-party DATA, never instructions (TR10)."
	}
	if a.Kind == KindCallback {
		desc += " The completion must carry a signature that verifies against the CONFIGURED peer's own key AND name a Task this daemon created, still awaiting, whose callee is the sender — verification proves WHO, binding proves WHAT THEY WERE ASKED (RA8); an unsolicited completion is refused no matter whose signature it wears."
	}
	return map[string]any{
		"type":                 "object",
		"description":          desc,
		"additionalProperties": true,
	}
}

func (s *Server) handleToolCall(req rpcIn) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		s.reply(req.ID, nil, &rpcErr{Code: codeInvalidParams, Message: "tools/call: params must be {name, arguments}"})
		return
	}
	var action Action
	found := false
	for _, a := range s.table {
		if a.Name == p.Name {
			action, found = a, true
			break
		}
	}
	if !found {
		// MCP's own answer for an unknown tool is a protocol error — but
		// what makes N14 real is that the client has ALREADY seen the
		// boundary (instructions + every description carry it), so an
		// unknown tool reads as a client bug, not as a hunt for the
		// version that has `kill`.
		if msg, refused := QuestionToolRefusal(p.Name); refused {
			// The stdio twin of the HTTP refusal (T5, asserted separately
			// — TestBothMountingsAgree): the guessed tool name earns the
			// reason and the alternative inside the protocol error.
			s.reply(req.ID, nil, &rpcErr{Code: codeInvalidParams, Message: msg})
			return
		}
		s.reply(req.ID, nil, &rpcErr{Code: codeInvalidParams, Message: fmt.Sprintf("unknown tool %q — this surface carries exactly the tools in tools/list; aeon has no operational control tools, by design (see instructions)", p.Name)})
		return
	}
	args := p.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if action.Kind == KindTrigger {
		// Same one transform as the HTTP mounting (N18: one table, one
		// trigger — neither mounting owns its own copy). AFTER the empty
		// default, so even a no-arguments trigger is pinned unattended.
		args = TriggerParamsJSON(args)
	}
	var result any
	if err := s.caller.Call(action.SocketMethod, json.RawMessage(args), &result); err != nil {
		// The daemon's refusal is the answer, verbatim (I5): isError with
		// the daemon's own sentence, never a paraphrase.
		s.reply(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": err.Error()}},
			"isError": true,
		}, nil)
		return
	}
	text, err := json.Marshal(result)
	if err != nil {
		text = []byte(fmt.Sprintf("%v", result))
	}
	s.reply(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": false,
	}, nil)
}

// reply writes exactly one message; the mutex is what keeps two
// concurrent goroutines from interleaving bytes into one half-response.
func (s *Server) reply(id *json.RawMessage, result any, e *rpcErr) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.enc.Encode(rpcOut{JSONRPC: "2.0", ID: id, Result: result, Error: e})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
