package agentsvc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// T10 — transport.go, the agentsvc half of the socket surface. The protocol
// tests prove the framing; these prove the MEANINGS: the same doors T4–T8
// built judge a socket ask exactly as they judge a lane ask, and the
// transport adds no door of its own and bypasses none.

// transportEnv wires the transport the entry point would: real store, real
// signed cards, an injected policy reader (the seam cmd/aeon bridges).
type transportEnv struct {
	e    *chainEnv
	tr   *Transport
	list *int // counts policy reads — the freeze is one reading per invoke
}

func newTransportEnv(t *testing.T) *transportEnv {
	t.Helper()
	e := newChainEnv(t)
	reads := new(int)
	tr := NewTransport(e.s, e.iv, func(ctx context.Context, at time.Time) ([]policy.Policy, error) {
		*reads = *reads + 1
		return e.frozen(t), nil
	})
	return &transportEnv{e: e, tr: tr, list: reads}
}

// frozen: allow every ref this suite asks for (the door still JUDGES; these
// tests are about the socket path, not the verdict).
func (e *chainEnv) frozen(t *testing.T) []policy.Policy {
	t.Helper()
	ps := oneDeny()
	for _, ref := range []string{"reddit-agent/do-thing", "planner/do-thing"} {
		ps = append(ps, frozenAllow(ref)...)
	}
	return ps
}

func (te *transportEnv) dispatch(t *testing.T, method string, params string) (any, string, error) {
	t.Helper()
	return te.tr.Handle(context.Background(), method, json.RawMessage(params))
}

func TestSkillInvokableOverTheSocket(t *testing.T) {
	// The gate: a skill is invokable over the EXISTING socket — built here
	// exactly as the daemon builds it (protocol.NewTable + the injected
	// seam), dispatched through Table.Dispatch, the same entry the localrpc
	// transport feeds. The ask becomes a row-bearing waiting task through
	// the real doors, and the subscriber is told.
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, te.e.s, te.e.loopSt, "planner")

	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	var events []string
	tbl.OnEvent = func(kind string, payload any) { events = append(events, kind) }
	tbl.Agents = te.tr.Handle
	if _, err := tbl.Dispatch(context.Background(), "c1", protocol.MethodInitialize,
		json.RawMessage(`{"clientName":"t","clientVersion":"0","protocolVersion":2}`)); err != nil {
		t.Fatal(err)
	}
	res, err := tbl.Dispatch(context.Background(), "c1", protocol.MethodAgentInvoke,
		json.RawMessage(`{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"what do people say about aeon?"}`))
	if err != nil {
		t.Fatalf("a legal delegation must suspend over the socket: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("the receipt must be the JSON-ready answer: %T", res)
	}
	if m["status"] != "waiting" || m["skillRef"] != "reddit-agent/do-thing" {
		t.Fatalf("the receipt: %v", m)
	}
	tid, _ := m["taskId"].(int64)
	if tid == 0 {
		t.Fatalf("taskId must be the int64 receipt: %#v", m["taskId"])
	}
	// Rows are the truth (P25): task + chain edge written, step untouched.
	row, err := te.e.s.GetAgentTask(tid)
	if err != nil || row.Status != string(TaskWaiting) {
		t.Fatalf("the ask must be a waiting row: %v %v", row, err)
	}
	if len(events) != 1 || events[0] != protocol.KindTaskCreated {
		t.Fatalf("the subscriber hears exactly one task.created: %v", events)
	}
	// The frozen set was read ONCE (one clock reading for set and verdict).
	if *te.list != 1 {
		t.Fatalf("the policy reader ran %d times; freeze and verdict must share one instant", *te.list)
	}
	// And the same ask WITHOUT the allow is refused by the same door the
	// lane ask meets — second hand, not second mechanism (MC4).
	te.tr.listPolicies = func(ctx context.Context, at time.Time) ([]policy.Policy, error) {
		return oneDeny(), nil // denies the OTHER ref; this ref now has no allow
	}
	_, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"again?"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rj, "default deny") {
		t.Fatalf("the socket ask meets the SkillAccess door with S10's sentence: %q", rj)
	}
}

func TestNoSecondListenerIsOpened(t *testing.T) {
	// Asserted by grep (P25/N7): this package binds nothing. No listen, no
	// bind, no dial, no TLS, no HTTP server — A2A's wire is S21's, with its
	// trust store and egress policy, and a port opened here would be a
	// fourth surface nobody asked for.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		name := en.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		// The banned shapes are the ACTS of listening, not the English word
		// (card.go's comment says "opens no listener" — a grep that bans
		// prose forbids truth-telling; T9 learned this the same way).
		for _, banned := range []string{"net.Listen", "ListenAndServe", "net.Dial", "http.Server",
			"tls.Config", "x509", "Listen(", ".Bind("} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("%s mentions %q — no new transport (N7); the socket is the daemon's, the wire is S21's", name, banned)
			}
		}
	}
	// And no remote address can be CONFIGURED: the invoke parameters are a
	// closed shape — reflect over the real struct, field by field.
	for _, s := range []any{invokeParams{}, struct {
		CallerLoopSlug string `json:"callerLoopSlug"`
		ComponentRoot  string `json:"projectRoot"`
		CallerStepID   int64  `json:"callerStepId"`
	}{}} {
		st := reflect.TypeOf(s)
		for i := 0; i < st.NumField(); i++ {
			n := strings.ToLower(st.Field(i).Name)
			for _, banned := range []string{"host", "port", "addr", "url", "endpoint", "token", "key", "cred"} {
				if strings.Contains(n, banned) {
					t.Fatalf("invoke params carry %q — a remote address configured here pre-empts S21's trust store without its egress policy", st.Field(i).Name)
				}
			}
		}
	}
}

func TestAgentsvcHandlesMatchTheProtocolEnumerationExactly(t *testing.T) {
	// The one-table claim across the two packages: the protocol registrations,
	// the protocol enumeration, and the transport's dispatch are the SAME
	// seven names — a drift either way is the surface growing by accident.
	te := newTransportEnv(t)
	_ = te
	// S15 amends the parity, honestly: `agent.plugins` joins the protocol
	// enumeration but NOT this dispatch — the plugin registry is the entry
	// point's seam (Table.Plugins), and this package is swept FORBIDDEN
	// from importing internal/plugin, so it could not answer the read even
	// if it wanted to. The parity is exact for everything else, and the
	// exclusion is asserted rather than subtracted in silence.
	want := map[string]bool{}
	for _, e := range protocol.AgentServiceMethods() {
		want[string(e.Name)] = true
	}
	if !want["agent.plugins"] {
		t.Fatal("S15's one read must be in the protocol enumeration (N12: it states its own Cannot)")
	}
	delete(want, "agent.plugins")
	// S34 T1 amends the parity the same way: `agent.list` joins the protocol
	// enumeration (it is modeled on agent.plugins' own exclusion, per its
	// Cannot comment in internal/protocol/agent.go) but NOT this dispatch —
	// the roster is Table.AgentRoster, a different seam than the task/invoke
	// methods this transport answers.
	if !want["agent.list"] {
		t.Fatal("S34 T1's roster read must be in the protocol enumeration (modeled on agent.plugins' own exception)")
	}
	delete(want, "agent.list")
	// S35 T4's agent.enable/disable/revoke are a THIRD shape, not a third
	// instance of this exception: unlike agent.plugins/agent.list, they
	// are not in protocol.AgentServiceMethods() at all (that slice is the
	// S14/S15/S21 task lifecycle; these three are plain synchronous
	// writes with no async task to subscribe to — see their own doc in
	// internal/protocol/method.go). So `want` never contains them and
	// there is nothing to delete here; the assertion below that `got`
	// doesn't either is still worth stating explicitly, since they forward
	// to Table.AgentTransition, a cmd/aeon seam, never this transport.
	got := map[string]bool{}
	for _, h := range Handles() {
		got[h] = true
	}
	if got["agent.plugins"] {
		t.Fatal("the transport must NOT handle agent.plugins — answering it here would need an import the layer sweep refuses (the read belongs to the entry point)")
	}
	if got["agent.list"] {
		t.Fatal("the transport must NOT handle agent.list — the roster is Table.AgentRoster's seam, not this transport's")
	}
	for _, m := range []string{"agent.enable", "agent.disable", "agent.revoke"} {
		if got[m] {
			t.Fatalf("the transport must NOT handle %q — it is Table.AgentTransition's seam, not this transport's", m)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("Handles has %d names, protocol enumerates %d (minus the entry-point read)", len(got), len(want))
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("protocol registers %q and the transport does not handle it", n)
		}
	}
	// And the unknown-method arm is honest about drift rather than permissive.
	if _, rj, err := te.tr.Handle(context.Background(), "agent.nope", nil); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("an unknown method must name the drift loudly: %v %v", rj, err)
	}
}

func TestSocketAskIsJudgedByIdentity(t *testing.T) {
	// The transport READS the step row: a step that belongs to another loop
	// refuses by naming both loops, and no door downstream is trusted to
	// catch a misfired ask.
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	// A step from a SECOND loop, asked in the name of the first: the loop
	// resolves (it exists), the step resolves (it exists), and the CROSS
	// refuses — an ask asked from the wrong loop is a misfired ask.
	other, _ := startTaskLoop(t, te.e.s, "elsewhere")
	foreign := runningStepAs(t, te.e.s, other, "planner")
	_, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(foreign)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"misfired"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rj, "belongs to loop") || !strings.Contains(rj, "not the loop named") {
		t.Fatalf("the mismatch names both loops: %q", rj)
	}
	// And a step that does not exist refuses by naming the read.
	_, rj, err = te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":99999,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"ghost"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rj, "could not be read") {
		t.Fatalf("the missing step is named: %q", rj)
	}
	// The caller agent is NOT a parameter — reflect proves there is no
	// callerAgent field to lie through.
	st := reflect.TypeOf(invokeParams{})
	for i := 0; i < st.NumField(); i++ {
		if strings.EqualFold(st.Field(i).Name, "CallerAgent") {
			t.Fatal("the app may not claim who is asking; the row says")
		}
	}
}

func TestSocketAskIsAsyncOnly(t *testing.T) {
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, te.e.s, te.e.loopSt, "planner")
	_, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"sync?","mode":"sync"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rj, "sync") || !strings.Contains(rj, "agent.task.complete") {
		t.Fatalf("the sync refusal must state the alternative: %q", rj)
	}
}

func TestParamsAreStrictAndBounded(t *testing.T) {
	// Unknown field: refused by name (a typo must not silently drop an
	// identity); free text bounded (socket text reaches renderings).
	te := newTransportEnv(t)
	// discover takes NO params: an unknown field must refuse by naming the
	// shape, not be silently ignored.
	_, rj, _ := te.dispatch(t, "agent.discover", `{"agent":"reddit-agent"}`)
	if !strings.Contains(rj, "params") {
		t.Fatalf("unknown params must refuse by naming the shape: %q", rj)
	}
	_, rj, _ = te.dispatch(t, "agent.card", `{"agent":"../etc"}`)
	if !strings.Contains(rj, "agent") {
		t.Fatalf("a name outside the boundary's alphabet refuses by field (SEC-046 delegation): %q", rj)
	}
	long := strings.Repeat("x", maxAskText+1)
	_, rj, _ = te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"l","componentRoot":"/r","callerStepId":1,"calleeAgent":"a","skillId":"s","text":"`+long+`"}`)
	if !strings.Contains(rj, "ceiling") {
		t.Fatalf("the ask text ceiling refuses with its number: %q", rj)
	}
}

func TestInputModeTravelsAndUnsupportedModeNamesTheSet(t *testing.T) {
	// T12 walk finding: the socket must be able to SAY the media type, and
	// the typed contract must answer an unsupported one by naming what IS
	// supported (the pre-money check the lane meets, met over the socket).
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, te.e.s, te.e.loopSt, "planner")
	_, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"an image?","inputMode":"image/png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rj, "image/png") || !strings.Contains(rj, "text/plain") {
		t.Fatalf(`the refusal names the refused mode AND the supported set: %q`, rj)
	}
}

func TestReportsSettleOnceAndNeverInventCost(t *testing.T) {
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, te.e.s, te.e.loopSt, "planner")
	res, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"report me"}`)
	if rj != "" || err != nil {
		t.Fatal(rj, err)
	}
	tid := res.(map[string]any)["taskId"].(int64)

	// No cost in the report → unknown WITH a reason, never zero (O2).
	if _, rj, err := te.dispatch(t, "agent.task.complete", `{"taskId":`+itoa(tid)+`,"result":"all is well"}`); rj != "" || err != nil {
		t.Fatal(rj, err)
	}
	row, err := te.e.s.GetAgentTask(tid)
	if err != nil {
		t.Fatal(err)
	}
	task := toTask(row)
	if task.Cost.State != model.CostUnknown || task.Cost.Reason != "cost-not-reported" {
		t.Fatalf("a silent report records silence, not zero: %+v", task.Cost)
	}
	// Settled is settled (T3's idempotence, unchanged by the socket).
	if _, rj, err := te.dispatch(t, "agent.task.complete", `{"taskId":`+itoa(tid)+`,"result":"again"}`); rj == "" || err != nil {
		t.Fatalf("a second report must be refused by the task layer: %q", rj)
	}
	// fail requires its reason.
	// The first ask's resume closed its step (rows are the truth); the
	// second ask needs its own running step — one step, one ask.
	step2 := runningStepAs(t, te.e.s, te.e.loopSt, "planner")
	res2, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(step2)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"fail me"}`)
	if rj != "" || err != nil {
		t.Fatal(rj, err)
	}
	tid2 := res2.(map[string]any)["taskId"].(int64)
	if _, rj, _ := te.dispatch(t, "agent.task.fail", `{"taskId":`+itoa(tid2)+`}`); !strings.Contains(rj, "reason") {
		t.Fatalf("a silent failure refuses: %q", rj)
	}
	if _, rj, err := te.dispatch(t, "agent.task.fail", `{"taskId":`+itoa(tid2)+`,"reason":"rate limited"}`); rj != "" || err != nil {
		t.Fatal(rj, err)
	}
}

func TestTaskReadsCarryHonestCostAndNoCalleeToolset(t *testing.T) {
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, te.e.s, te.e.loopSt, "planner")
	res, rj, err := te.dispatch(t, "agent.invoke", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"list me in"}`)
	if rj != "" || err != nil {
		t.Fatal(rj, err)
	}
	tid := res.(map[string]any)["taskId"].(int64)

	list, rj, err := te.dispatch(t, "agent.task.list", `{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`}`)
	if rj != "" || err != nil {
		t.Fatal(rj, err)
	}
	raw, _ := json.Marshal(list)
	body := string(raw)
	if !strings.Contains(body, `"taskId":1`) || !strings.Contains(body, "do-thing") {
		t.Fatalf("the listing names the ask: %s", body)
	}
	// MC2 through the projection: no field of the callee's toolset can
	// appear — the task view is a CLOSED set of keys.
	for _, banned := range []string{"tools", "capabilities", "security", "publicKey"} {
		if strings.Contains(body, `"`+banned+`"`) {
			t.Fatalf("the listing leaks %q — the caller never learns the callee's toolset (MC2)", banned)
		}
	}
	// task.show: the rollup arrives with its state, and a child that never
	// reported makes the total partial or unknown — never known-by-default.
	show, rj, err := te.dispatch(t, "agent.task.show", `{"taskId":`+itoa(tid)+`}`)
	if rj != "" || err != nil {
		t.Fatal(rj, err)
	}
	m := show.(map[string]any)
	roll, ok := m["rollup"].(map[string]any)
	if !ok {
		t.Fatalf("the show answer carries the rollup: %v", m)
	}
	cost, ok := roll["cost"].(model.Cost)
	if !ok {
		t.Fatalf("the rollup cost travels as the honest struct, not a flattened guess: %T", roll["cost"])
	}
	if cost.State != model.CostPartial && cost.State != model.CostUnknown {
		t.Fatalf("nothing reported yet, so the total cannot be known: %+v", cost)
	}
}

func TestCardReadHoldsTheThreeStates(t *testing.T) {
	te := newTransportEnv(t)
	te.e.offer(t, "reddit-agent")
	res, rj, _ := te.dispatch(t, "agent.card", `{"agent":"reddit-agent"}`)
	if rj != "" {
		t.Fatal(rj)
	}
	if m := res.(map[string]any); m["loaded"] != true || m["state"] != "loaded" {
		t.Fatalf("a good card: %v", m)
	}
	res, _, _ = te.dispatch(t, "agent.card", `{"agent":"nobody"}`)
	if m := res.(map[string]any); m["loaded"] != false || m["state"] != "absent" {
		t.Fatalf("no file is `absent`, stated: %v", m)
	}
	// A file that fails the load path is `refused` WITH the why — not
	// absent, not loaded: three states, three answers.
	if err := os.WriteFile(filepath.Join(te.e.cards, "tampered.json"), []byte(`{"card":{},"publicKey":"x","signature":"y"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _, _ = te.dispatch(t, "agent.card", `{"agent":"tampered"}`)
	if m := res.(map[string]any); m["loaded"] != false || m["state"] != "refused" || m["why"] == "" || m["why"] == nil {
		t.Fatalf("a failed load is `refused` with its why: %v", m)
	}
}

func TestUnwiredPolicyReaderIsAnInfrastructureFailureNotADeny(t *testing.T) {
	// The distinction that keeps the third state clean: an unwired reader
	// must not present as "default deny" (which would be a confident wrong
	// answer about the OPERATOR's rules when the truth is about this
	// daemon's wiring).
	te := newTransportEnv(t)
	te.e.offer(t, "planner")
	te.e.offer(t, "reddit-agent")
	stepID := runningStepAs(t, te.e.s, te.e.loopSt, "planner")
	te.tr.listPolicies = nil
	_, rj, err := te.tr.Handle(context.Background(), "agent.invoke",
		json.RawMessage(`{"callerLoopSlug":"`+te.e.loopSt.Slug+`","componentRoot":"`+te.e.loopSt.Root+`","callerStepId":`+itoa(stepID)+`,"calleeAgent":"reddit-agent","skillId":"do-thing","text":"x"}`))
	// The step read comes first (nil store → infrastructure error), so the
	// reader check is reached only with a store; assert at least: refusal
	// is NOT the deny sentence, and an error or honest refusal results.
	if strings.Contains(rj, "default deny") {
		t.Fatalf("an unwired reader may not answer as the policy's default deny: %q", rj)
	}
	if rj == "" && err == nil {
		t.Fatal("the unwired state must fail LOUDLY")
	}
	if !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("the infrastructure failure names itself: %v", err)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
