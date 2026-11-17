package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// # The fixtures are tested against the Go types, and the Go types against them
//
// Comparison is SEMANTIC, never by bytes: key order in JSON is not part of the
// contract, and asserting on it would make every reformatting a failure.

type fixture struct {
	Path        string
	Description string          `json:"description"`
	Method      *string         `json:"method"`
	RawRequest  *string         `json:"rawRequest"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	// Notification is set instead of request/response for a server→client event.
	Notification json.RawMessage `json:"notification"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	var out []fixture
	err := filepath.WalkDir("conformance", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var f fixture
		if err := json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		f.Path = path
		out = append(out, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures found; every test below would pass vacuously")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Every fixture explains itself. A fixture whose purpose is not written down is
// a fixture somebody deletes.
func TestEveryFixtureHasADescription(t *testing.T) {
	for _, f := range loadFixtures(t) {
		if len(strings.TrimSpace(f.Description)) < 40 {
			t.Errorf("%s: the description is missing or too short to say why the fixture exists", f.Path)
		}
	}
}

// Every method has at least one SUCCESS fixture.
//
// # Why "success" rather than "a fixture naming the method"
//
// That was the first version, and a negative control caught it: deleting
// `ping/ok.json` failed nothing, because `errors/not-initialized.json` names
// `ping` too — it documents calling ping before the handshake. A method whose
// only fixture is an error fixture has no stated happy path, which is the thing
// a third-party client needs most.
func TestEveryMethodHasASuccessFixture(t *testing.T) {
	covered := map[string]bool{}
	for _, f := range loadFixtures(t) {
		if f.Method != nil && len(f.Response) > 0 && !expectsError(f) {
			covered[*f.Method] = true
		}
	}
	tbl := newTestTable(t)
	for _, name := range tbl.Methods() {
		if !covered[string(name)] {
			t.Errorf("method %q has no SUCCESS fixture — an error fixture naming it does not "+
				"state its happy path, which is what a third-party client needs most", name)
		}
	}
}

// Every error code has a fixture. A code without one is a code nobody else can
// implement against, and D9's promise is only as good as its coverage.
func TestEveryErrorCodeHasAFixture(t *testing.T) {
	covered := map[int]bool{}
	for _, f := range loadFixtures(t) {
		if len(f.Response) == 0 {
			continue
		}
		var resp struct {
			Error *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(f.Response, &resp); err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		if resp.Error != nil {
			covered[resp.Error.Code] = true
		}
	}
	for _, e := range ErrorRegistry() {
		if !covered[e.Code] {
			t.Errorf("error %d (%s) has no fixture", e.Code, e.Message)
		}
	}
	// And the transport-level codes a client must also handle.
	for _, code := range []int{-32601, -32602, -32700} {
		if !covered[code] {
			t.Errorf("JSON-RPC code %d has no fixture; a client has to handle it too", code)
		}
	}
}

// Every event kind has a fixture.
func TestEveryEventKindHasAFixture(t *testing.T) {
	covered := map[string]bool{}
	for _, f := range loadFixtures(t) {
		if len(f.Notification) == 0 {
			continue
		}
		var n struct {
			Params struct {
				Kind string `json:"kind"`
			} `json:"params"`
		}
		if err := json.Unmarshal(f.Notification, &n); err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		covered[n.Params.Kind] = true
	}
	for _, kind := range Kinds() {
		if !covered[kind] {
			t.Errorf("event kind %q has no fixture", kind)
		}
	}
}

// Every fixture round-trips: the request parses into the Go type, re-marshals,
// and still means the same thing.
func TestEveryFixtureRoundTripsThroughTheGoTypes(t *testing.T) {
	for _, f := range loadFixtures(t) {
		t.Run(f.Path, func(t *testing.T) {
			if len(f.Request) > 0 {
				roundTripRequest(t, f)
			}
			if len(f.Response) > 0 {
				roundTripResponse(t, f)
			}
			if len(f.Notification) > 0 {
				roundTripNotification(t, f)
			}
		})
	}
}

func roundTripRequest(t *testing.T, f fixture) {
	t.Helper()

	// A fixture documenting -32602 carries params that deliberately do NOT fit
	// the Go type — that is the condition it exists to describe. Requiring them
	// to parse would make the invalid-params case unexpressible.
	if expectsError(f) {
		return
	}
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(f.Request, &env); err != nil {
		t.Fatalf("request envelope: %v", err)
	}
	if env.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", env.JSONRPC)
	}
	if len(env.Params) == 0 {
		return
	}
	target := paramsTypeFor(MethodName(env.Method))
	if target == nil {
		return // an unknown method has no params type, by definition
	}
	if err := json.Unmarshal(env.Params, target); err != nil {
		t.Fatalf("params do not fit %T: %v", target, err)
	}
}

func roundTripResponse(t *testing.T, f fixture) {
	t.Helper()
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(f.Response, &env); err != nil {
		t.Fatalf("response envelope: %v", err)
	}
	if env.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", env.JSONRPC)
	}

	if len(env.Error) > 0 {
		assertErrorShape(t, f, env.Error)
		return
	}
	if f.Method == nil {
		return
	}
	target := resultTypeFor(MethodName(*f.Method))
	if target == nil {
		return
	}
	if err := json.Unmarshal(env.Result, target); err != nil {
		t.Fatalf("result does not fit %T: %v", target, err)
	}
	// Semantic round trip: marshal the Go value back and compare as data.
	back, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	assertKnownFieldsMatch(t, "result", env.Result, back, target)
}

// assertErrorShape: an Aeon code carries all three parts; a JSON-RPC code does
// not have to, because the three-part shape is Aeon's.
func assertErrorShape(t *testing.T, f fixture, raw json.RawMessage) {
	t.Helper()
	var e struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if e.Code >= AeonCodeMin && e.Code <= AeonCodeMax {
		if len(e.Data) == 0 {
			t.Fatalf("Aeon error %d carries no `data`; every Aeon error has three parts", e.Code)
		}
		var d Data
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatalf("data does not fit protocol.Data: %v", err)
		}
		if d.What == "" || d.Means == "" || d.Todo == "" {
			t.Errorf("error %d is missing a part: %+v", e.Code, d)
		}
		// The code must be in the registry, or a client implements against a
		// code that does not exist.
		found := false
		for _, known := range ErrorRegistry() {
			if known.Code == e.Code {
				found = true
			}
		}
		if !found {
			t.Errorf("error %d is in a fixture but not in the registry", e.Code)
		}
		return
	}
	if e.Code < ReservedJSONRPCMin || e.Code > ReservedJSONRPCMax {
		t.Errorf("code %d is in neither JSON-RPC's range nor Aeon's", e.Code)
	}
}

func roundTripNotification(t *testing.T, f fixture) {
	t.Helper()
	var n struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(f.Notification, &n); err != nil {
		t.Fatal(err)
	}
	if n.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", n.JSONRPC)
	}
	// A notification has no `id` — that is how a client tells it from a
	// response, and it is JSON-RPC's rule rather than Aeon's.
	var withID map[string]any
	if err := json.Unmarshal(f.Notification, &withID); err != nil {
		t.Fatal(err)
	}
	if _, has := withID["id"]; has {
		t.Error("a notification carries an `id`; a client would treat it as a response")
	}
	// S31 T9 / LW7: TWO notifications are deliberately NOT Events —
	// terminal.output and terminal.exit reach attached connections without
	// the durable log's seq, so they have no subscriptionId/kind/at to
	// round-trip through Event. The harness knows exactly these two by name;
	// anything else still has to be an event, which keeps the guard it was.
	if n.Method == "terminal.output" || n.Method == "terminal.exit" {
		roundTripTerminalNotification(t, n.Method, n.Params)
		return
	}
	if n.Method != "event" {
		t.Errorf("method = %q, want event", n.Method)
	}

	var ev Event
	if err := json.Unmarshal(n.Params, &ev); err != nil {
		t.Fatalf("params do not fit protocol.Event: %v", err)
	}
	if ev.Seq == 0 || ev.Kind == "" || ev.At == "" {
		t.Errorf("event is missing seq, kind or at: %+v", ev)
	}
	back, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	assertKnownFieldsMatch(t, "event", n.Params, back, &Event{})
}

// assertKnownFieldsMatch compares the fields the Go type KNOWS ABOUT.
//
// # Why not "every field the fixture states"
//
// That was the first version, and `forward-compat.json` failed it: the fixture
// carries `featureWeHaveNotWrittenYet`, the Go type does not know it, and the
// re-marshal drops it. Demanding the Go side reproduce it would have required
// the type to keep a field it has never heard of — which is the opposite of
// what T3 asks for, and would have made the one fixture that proves additive
// evolution impossible to pass.
//
// So the comparison is over the type's own JSON field names, taken by
// reflection:
//
//   - a field the type knows and the fixture states → must be equal
//   - a field the fixture states and the type does not know → ignored, because
//     tolerating it IS the contract
//   - a field the type has and the fixture omits → ignored, because `omitempty`
//     and later optional fields are the additive direction T2 permits
func assertKnownFieldsMatch(t *testing.T, what string, fixtureRaw, goRaw json.RawMessage, target any) {
	t.Helper()
	var want, got map[string]any
	if err := json.Unmarshal(fixtureRaw, &want); err != nil {
		return // not an object; nothing to compare field-wise
	}
	if err := json.Unmarshal(goRaw, &got); err != nil {
		t.Fatal(err)
	}

	known := jsonFieldNames(target)
	checked := 0
	for key, wantVal := range want {
		if !known[key] {
			continue // a field from the future; tolerating it is the point
		}
		gotVal, present := got[key]
		if !present {
			t.Errorf("%s: the Go type dropped %q, which it declares and the fixture states", what, key)
			continue
		}
		checked++
		if !reflect.DeepEqual(normalise(wantVal), normalise(gotVal)) {
			t.Errorf("%s.%s = %#v, fixture says %#v", what, key, gotVal, wantVal)
		}
	}

	// A comparison that checked nothing would pass silently, which is how a
	// renamed json tag slips through: every fixture field would become
	// "unknown" and the test would go green.
	if len(want) > 0 && checked == 0 && len(known) > 0 {
		t.Errorf("%s: not one of the fixture's %d fields matched a field the Go type declares "+
			"(%v) — a json tag was probably renamed", what, len(want), sortedFieldNames(known))
	}
}

// jsonFieldNames returns the wire names a type declares.
func jsonFieldNames(target any) map[string]bool {
	out := map[string]bool{}
	rt := reflect.TypeOf(target)
	for rt != nil && rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt == nil || rt.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		out[strings.Split(tag, ",")[0]] = true
	}
	return out
}

func sortedFieldNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func expectsError(f fixture) bool {
	if len(f.Response) == 0 {
		return false
	}
	var resp struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(f.Response, &resp); err != nil {
		return false
	}
	return len(resp.Error) > 0
}

// normalise makes JSON numbers comparable regardless of how they decoded.
func normalise(v any) any {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			out[k] = normalise(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalise(val)
		}
		return out
	}
	return v
}

// T3's standing proof. If this file is deleted the test fails, which is the
// only thing that stops it being tidied away as a mistake.
func TestTheForwardCompatibilityFixtureExistsAndIsAccepted(t *testing.T) {
	raw, err := os.ReadFile("conformance/forward-compat.json")
	if err != nil {
		t.Fatalf("conformance/forward-compat.json is missing. It is the only standing test of " +
			"T3 — a client that rejects an unknown field turns every additive change into a " +
			"breaking one. See conformance/README.md before restoring it.")
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}

	// The request, with its unknown field, must dispatch successfully.
	var env struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(f.Request, &env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env.Params), "somethingFromTheFuture") {
		t.Fatal("the forward-compat fixture no longer carries an unknown request field")
	}
	tbl := newTestTable(t)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize, env.Params); err != nil {
		t.Fatalf("a request with an unknown field was rejected: %v", err)
	}

	// And the result, with its unknown field, must be accepted by the client's
	// type.
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(f.Response, &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(resp.Result), "featureWeHaveNotWrittenYet") {
		t.Fatal("the forward-compat fixture no longer carries an unknown result field")
	}
	var out InitializeResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("a result with an unknown field was rejected: %v", err)
	}
	if out.ProtocolVersion != PROTOCOL_VERSION {
		t.Fatalf("the known fields did not survive: %+v", out)
	}
}

// Reordering a fixture's keys must not fail anything. Proven by reordering one.
func TestComparisonIsSemanticNotByteEquality(t *testing.T) {
	a := json.RawMessage(`{"protocolVersion":1,"daemonVersion":"0.1.0","capabilities":["subscribe"]}`)
	b := json.RawMessage(`{"capabilities":["subscribe"],"daemonVersion":"0.1.0","protocolVersion":1}`)
	if string(a) == string(b) {
		t.Fatal("the two fixtures are byte-identical; this test proves nothing")
	}
	var fake testing.T
	assertKnownFieldsMatch(&fake, "result", a, b, &InitializeResult{})
	if fake.Failed() {
		t.Fatal("reordering keys failed the comparison; key order is not part of the contract")
	}
}

// The staleness check, run in CI: the fixture set cannot quietly fall behind.
func TestTheFixtureSetCoversTheWholeSurface(t *testing.T) {
	// Deliberately a summary rather than a new assertion: the three tests above
	// are the check. This one fails with a count, so a CI log says how far
	// behind the set is rather than only which item is missing.
	fixtures := loadFixtures(t)
	tbl := newTestTable(t)
	want := len(tbl.Methods()) + len(ErrorRegistry()) + len(Kinds())
	if len(fixtures) < want {
		t.Errorf("%d fixtures for %d methods + %d error codes + %d event kinds; "+
			"every one needs at least a fixture",
			len(fixtures), len(tbl.Methods()), len(ErrorRegistry()), len(Kinds()))
	}
}

// roundTripTerminalNotification is S31 T9's extension: the two direct
// notifications are checked against THEIR shapes — output carries a
// per-terminal monotonic seq and base64 bytes (Q3); exit carries EXACTLY
// one of exitCode/signal, the way cmd.Wait reports an exit. A fixture that
// faked both would be a fixture that lies about a killed shell.
func roundTripTerminalNotification(t *testing.T, method string, params json.RawMessage) {
	t.Helper()
	switch method {
	case "terminal.output":
		var p struct {
			TerminalID string `json:"terminalId"`
			Seq        uint64 `json:"seq"`
			Data       string `json:"data"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("terminal.output params: %v", err)
		}
		if p.TerminalID == "" || p.Seq == 0 || p.Data == "" {
			t.Errorf("terminal.output is missing a field it must carry: %s", params)
		}
		if _, err := base64.StdEncoding.DecodeString(p.Data); err != nil {
			t.Errorf("terminal.output data is not base64 (Q3): %v", err)
		}
		back, _ := json.Marshal(p)
		assertKnownFieldsMatch(t, "terminal.output params", params, back, &p)
	case "terminal.exit":
		var p struct {
			TerminalID string `json:"terminalId"`
			ExitCode   *int   `json:"exitCode"`
			Signal     string `json:"signal"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatalf("terminal.exit params: %v", err)
		}
		if p.TerminalID == "" {
			t.Errorf("terminal.exit lost its terminalId: %s", params)
		}
		if (p.ExitCode != nil) == (p.Signal != "") {
			t.Errorf("terminal.exit must carry EXACTLY one of exitCode/signal — cmd.Wait says so: %s", params)
		}
		back, _ := json.Marshal(p)
		assertKnownFieldsMatch(t, "terminal.exit params", params, back, &p)
	}
}
