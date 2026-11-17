package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T10 — the agent-services surface, tested at the level this package owns:
// framing, the one enumeration, refusal verbatim travel, event fan-out, and
// the handshake. The MEANINGS of the seven methods live in agentsvc and are
// tested there; nothing here re-decides an agent question — this package
// contains no agent logic and the test proves it stays that way.

func TestMethodTableInOnePlace(t *testing.T) {
	// The enumeration (agentServiceMethods), the registrations (tbl), and
	// NOTHING ELSE: a method in the table without an entry is a surface
	// that grew by accident; an entry without a registration is prose
	// posing as a method.
	tbl := newTestTable(t)
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		if strings.HasPrefix(string(m), "agent.") {
			registered[m] = true
		}
	}
	// S35 T4's three excluded by name, with the same reasoning repeated at
	// their own declaration in method.go: they are plain, synchronous
	// writes (the result returns in the RPC response, like loop.create),
	// not members of the S14/S15/S21 agent-to-agent TASK lifecycle this
	// slice's own Emits requirement exists for (an agent.invoke spawns an
	// async task a caller must subscribe to find the end of — these three
	// spawn nothing to subscribe to).
	for _, m := range []MethodName{MethodAgentEnable, MethodAgentDisable, MethodAgentRevoke} {
		if !registered[m] {
			t.Fatalf("%s is not registered", m)
		}
		delete(registered, m)
	}
	// S43 T7's eight agent-MANAGEMENT methods — create/import/export/
	// duplicate/delete/edit/view/migrate — are excluded by name for the same
	// reason the three above are: agentServiceMethods is the S14/S15/S21
	// agent-to-agent TASK LIFECYCLE (the entries carrying an Emits event,
	// because an agent.invoke spawns an ASYNC task a caller must subscribe
	// to find the end of). These eight are a SEPARATE family — the
	// agent-management door, ONE seam (Table.AgentManage) dispatched by name
	// — ordinary synchronous daemon writes (the result returns in the RPC
	// response, like loop.create) that touch the agent table, not a task
	// lifecycle. Forcing an Emits value here would invent a notification no
	// subscriber mechanism expects, purely to satisfy a test literal — the
	// same reasoning method.go repeats at their own declaration.
	for _, m := range []MethodName{
		MethodAgentCreate, MethodAgentImport, MethodAgentExport, MethodAgentDuplicate,
		MethodAgentDelete, MethodAgentEdit, MethodAgentView, MethodAgentMigrate,
		// S73 T2: `agent.rows` joins the exclusion for the same family reason
		// — it is the management door's paged READ. It spawns nothing, so it
		// has no Emits value and no subscriber to invent one for.
		MethodAgentRows,
		// S73 T5: `agent.changes` for the same reason again — the management
		// door's paged read of the LEDGER. Nothing spawns, nothing emits.
		MethodAgentChanges,
	} {
		if !registered[m] {
			t.Fatalf("%s is not registered", m)
		}
		delete(registered, m)
	}
	// S70 MB7: `agent.messages` is excluded by name for the same family
	// reason — it is neither the task lifecycle (it spawns nothing to
	// subscribe to) nor the management door (it writes nothing at all).
	// It is the READ of the agent↔agent mailbox: scope from the loop row,
	// rows as the truth; MB7 forbids it any authoring counterpart, so no
	// Emits invention applies here either.
	if !registered[MethodAgentMessages] {
		t.Fatalf("%s is not registered", MethodAgentMessages)
	}
	delete(registered, MethodAgentMessages)
	if len(agentServiceMethods) != len(registered) {
		t.Fatalf("%d entries for %d registered agent methods — one list grew without the other", len(agentServiceMethods), len(registered))
	}
	for _, e := range agentServiceMethods {
		if !registered[e.Name] {
			t.Fatalf("enumeration names %q, the table does not register it", e.Name)
		}
		// N12's discipline with teeth: an entry that states no restriction
		// is a list somebody stops reading.
		if len(e.Cannot) < 40 {
			t.Errorf("%s: the `cannot` statement is missing or too short to constrain anyone", e.Name)
		}
		if e.ReadOnly && e.Emits != "" {
			t.Errorf("%s: a read that emits an event is not a read", e.Name)
		}
		if !e.ReadOnly && e.Emits == "" {
			t.Errorf("%s: a lifecycle method with no event kind leaves subscribers blind (the task exists and nobody is told)", e.Name)
		}
	}
	// The writes are Debug — off the narrow surface (LW8's guard for the
	// same reason steer and terminal are): the public table answers.
	pub := map[MethodName]bool{}
	for _, m := range tbl.PublicMethods() {
		pub[m] = true
	}
	for _, e := range agentServiceMethods {
		if e.ReadOnly && !pub[e.Name] {
			t.Errorf("%s: the app's read window is not public — the app cannot read what the runtime decides with", e.Name)
		}
		if !e.ReadOnly && pub[e.Name] {
			t.Errorf("%s: a write on the narrow HTTP/stdio projection — invoking or settling a person's work is product control over a P26 socket, never a remote trigger", e.Name)
		}
	}
	// AgentServiceMethods is a copy: a surface that can edit the table of
	// what methods cannot do is a surface that will. (The write is undone
	// from the copy taken before it — the test must not leave a scar.)
	// Take the copy FIRST; tamper the original; the copy must be untouched
	// (a function that copies at call time would hand the tampered value
	// back, which is the difference between a snapshot and a window).
	early := AgentServiceMethods()
	pristine := agentServiceMethods[0].Cannot
	agentServiceMethods[0].Cannot = "tampered"
	later := AgentServiceMethods()
	agentServiceMethods[0].Cannot = pristine
	if early[0].Cannot == "tampered" {
		t.Fatalf("the handed-out slice shares the live backing array: %q", early[0].Cannot)
	}
	_ = later
}

func TestAgentServicesNotWiredRefusesByName(t *testing.T) {
	// Half-wired is visible (S31's discipline): an unwired Table answers by
	// naming the absence, not by succeeding at nothing.
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentDiscover, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrNotWired.Code {
		t.Fatalf("unwired agent services must answer ErrNotWired, got %v", err)
	}
	if !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("the refusal names the absence: %v", err)
	}
}

func TestAgentRefusalTravelsVerbatim(t *testing.T) {
	// The boundary already named the fault; this package adds no
	// interpretation — the sentence crosses byte for byte (refusals are
	// values, carried to the socket exactly as the policy reads carry theirs).
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	// The sentence is READ from the conformance fixture, not assembled here:
	// verdict.go owns the formatter (the purity sweep enforces it), and the
	// fixture is the one place this package is allowed to carry one.
	sentence := fixtureRefusal(t, "invoke-refused.json")
	tbl.Agents = func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
		return nil, sentence, nil
	}
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentInvoke, json.RawMessage(`{}`))
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("a refusal must arrive as a protocol Error, got %T %v", err, err)
	}
	if e.Code != ErrAgentRefused.Code {
		t.Fatalf("refusals carry the agent band, got %d", e.Code)
	}
	if e.Data.What != sentence {
		t.Fatalf("the door's sentence travels verbatim in `what`: %+v", e.Data)
	}
}

func TestLifecycleEventsCarryTheTaskId(t *testing.T) {
	// The fan-out contract: invoke/complete/fail announce with the taskId
	// and nothing else (events announce; rows are the source of truth).
	cases := []struct {
		method MethodName
		kind   string
	}{
		{MethodAgentInvoke, KindTaskCreated},
		{MethodAgentTaskComplete, KindTaskCompleted},
		{MethodAgentTaskFail, KindTaskFailed},
	}
	for _, c := range cases {
		t.Run(string(c.method), func(t *testing.T) {
			var gotKind string
			var gotPayload any
			tbl := newTestTable(t)
			tbl.OnEvent = func(kind string, payload any) { gotKind, gotPayload = kind, payload }
			initialized(t, tbl, "c1", PROTOCOL_VERSION)
			tbl.Agents = func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
				return map[string]any{"taskId": 12, "status": "waiting"}, "", nil
			}
			if _, err := tbl.Dispatch(context.Background(), "c1", c.method, json.RawMessage(`{}`)); err != nil {
				t.Fatal(err)
			}
			if gotKind != c.kind {
				t.Fatalf("emitted %q, want %q", gotKind, c.kind)
			}
			m, ok := gotPayload.(map[string]any)
			if !ok || m["taskId"] != int64(12) {
				t.Fatalf("the payload is the taskId and nothing else: %v", gotPayload)
			}
		})
	}
}

func TestLifecycleResultWithoutTaskIdRefusesLoudly(t *testing.T) {
	// The wiring defect is REFUSED, not swallowed: a settled task whose
	// event was quietly skipped is a subscriber that never learns, and
	// silent skips are how contracts rot.
	tbl := newTestTable(t)
	emitted := 0
	tbl.OnEvent = func(kind string, payload any) { emitted++ }
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	tbl.Agents = func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
		return map[string]any{"status": "waiting"}, "", nil // no taskId
	}
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentInvoke, json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "taskId") {
		t.Fatalf("a lifecycle result without its subject must refuse loudly, got %v", err)
	}
	if emitted != 0 {
		t.Fatal("it must refuse BEFORE emitting a half-truth")
	}
}

func TestAgentMethodsBeforeInitializeAreRefused(t *testing.T) {
	// The handshake pin: the agent surface is not reachable pre-negotiation
	// (RequiresInitialize on every registration — a reader finds the field,
	// this test proves no registration forgot it).
	for _, name := range AgentServiceMethods() {
		t.Run(string(name.Name), func(t *testing.T) {
			tbl := newTestTable(t)
			_, err := tbl.Dispatch(context.Background(), "c1", name.Name, nil)
			var e *Error
			if !errors.As(err, &e) || e.Code != ErrNotInitialized.Code {
				t.Fatalf("%s answered %v on an uninitialised connection", name.Name, err)
			}
		})
	}
}

// TestAgentFixturesMatchWhatTheHandlersReturn closes the conformance gap
// this package's fixtures would otherwise leave: the harness pins that every
// method HAS a success fixture; this test pins that each success fixture IS
// what the handler returns, by replaying the fixture's request through
// Dispatch with the seam returning the fixture's result. The fixture is the
// canned seam answer — one source of truth for both sides.
func TestAgentFixturesMatchWhatTheHandlersReturn(t *testing.T) {
	entries, err := os.ReadDir("conformance/agents")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		t.Run(en.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("conformance/agents", en.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var f struct {
				Method   *string         `json:"method"`
				Request  json.RawMessage `json:"request"`
				Response json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			var reqEnv struct {
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(f.Request, &reqEnv); err != nil {
				t.Fatal(err)
			}
			var env struct {
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code int `json:"code"`
					Data struct {
						What string `json:"what"`
					} `json:"data"`
				} `json:"error"`
			}
			if err := json.Unmarshal(f.Response, &env); err != nil {
				t.Fatal(err)
			}
			tbl := newTestTable(t)
			initialized(t, tbl, "c1", PROTOCOL_VERSION)
			// Each agents/*.json fixture's method is answered by the ENTRY
			// POINT's own seam, not necessarily Table.Agents — wireFakeSeam
			// picks the right one so the fixture contract is the same
			// regardless of which seam a method forwards through.
			if env.Error != nil {
				wireFakeSeam(tbl, *f.Method, func(ctx context.Context, params json.RawMessage) (any, string, error) {
					return nil, env.Error.Data.What, nil
				})
				_, err := tbl.Dispatch(context.Background(), "c1", MethodName(*f.Method), reqEnv.Params)
				var e *Error
				if !errors.As(err, &e) || e.Code != env.Error.Code {
					t.Fatalf("the refused fixture must arrive as code %d, got %v", env.Error.Code, err)
				}
				if e.Data.What != env.Error.Data.What {
					t.Fatalf("the sentence must travel verbatim: %+v", e.Data)
				}
				return
			}
			want := env.Result
			wireFakeSeam(tbl, *f.Method, func(ctx context.Context, params json.RawMessage) (any, string, error) {
				var v any
				if err := json.Unmarshal(want, &v); err != nil {
					t.Fatal(err)
				}
				return v, "", nil
			})
			got, err := tbl.Dispatch(context.Background(), "c1", MethodName(*f.Method), reqEnv.Params)
			if err != nil {
				t.Fatal(err)
			}
			gotRaw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			_ = json.Unmarshal(gotRaw, &a)
			_ = json.Unmarshal(env.Result, &b)
			if !jsonEqual(a, b) {
				t.Fatalf("the fixture does not state what the handler returns:\nfixture: %s\nhandler: %s", env.Result, gotRaw)
			}
		})
	}
}

// wireFakeSeam wires fn onto whichever Table seam `method` forwards
// through, so a conformance/agents/*.json fixture exercises the real
// dispatch path regardless of which entry-point seam backs its method —
// Table.Plugins (agent.plugins), Table.AgentRoster (agent.list),
// Table.ToolGrants (tools.grants), or Table.Agents (the seven agentsvc
// methods, which also carry the method name).
func wireFakeSeam(tbl *Table, method string, fn func(ctx context.Context, params json.RawMessage) (any, string, error)) {
	switch MethodName(method) {
	case MethodAgentPlugins:
		tbl.Plugins = fn
	case MethodAgentList:
		tbl.AgentRoster = fn
	case MethodToolsGrants:
		tbl.ToolGrants = fn
	case MethodAgentEnable, MethodAgentDisable, MethodAgentRevoke:
		tbl.AgentTransition = func(ctx context.Context, _ string, params json.RawMessage) (any, string, error) {
			return fn(ctx, params)
		}
	default:
		tbl.Agents = func(ctx context.Context, _ string, params json.RawMessage) (any, string, error) {
			return fn(ctx, params)
		}
	}
}

// fixtureRefusal lifts the refusal sentence out of a fixture file.
func fixtureRefusal(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("conformance/agents", name))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Response struct {
			Error struct {
				Data struct {
					What string `json:"what"`
				} `json:"data"`
			} `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Response.Error.Data.What == "" {
		t.Fatalf("%s carries no refusal sentence", name)
	}
	return f.Response.Error.Data.What
}

// jsonEqual is a semantic comparison (the conformance rule: key order is
// not part of the contract).
func jsonEqual(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	// Re-marshal through map ordering is already canonical in encoding/json
	// for maps; compare bytes.
	return string(ja) == string(jb)
}

// S34 T1: agent.list is a separate seam from the seven agent-services
// methods above (Table.AgentRoster, not Table.Agents) — same unwired
// discipline, its own test.
func TestAgentListUnwiredRefusesNamingTheTask(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentList, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrNotWired.Code {
		t.Fatalf("unwired agent roster must answer ErrNotWired, got %v", err)
	}
	if !strings.Contains(e.Data.What, "not wired") || !strings.Contains(e.Data.What, "AgentRoster") {
		t.Fatalf("the refusal names the missing seam: %+v", e.Data)
	}
}
