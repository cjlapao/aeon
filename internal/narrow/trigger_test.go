package narrow

// trigger_test.go — S19 T6's surface-side gates: the trigger's one
// transform is real on BOTH mountings (asserted separately, the T4/T5
// discipline — a shared bug in one copy passes a merged test), the
// outward grant is nowhere a property of anything standing, and the
// tools/list leg tells a CI author about the one lever that exists
// (N14: the refusal without the alternative is the failure mode).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// triggerCaller records the exact params bytes each mounting dispatches —
// the assertion is on what the daemon would receive, not on what the
// surface claims it sent.
type triggerCaller struct {
	mu     sync.Mutex
	sent   map[string][]string // method → params JSON, in dispatch order
	called []string
}

func newTriggerCaller() *triggerCaller {
	return &triggerCaller{sent: map[string][]string{}}
}

func (c *triggerCaller) Call(method protocol.MethodName, params, result any) error {
	raw := mustJSON(params)
	c.mu.Lock()
	c.sent[string(method)] = append(c.sent[string(method)], raw)
	c.called = append(c.called, string(method))
	c.mu.Unlock()
	if p, ok := result.(*any); ok {
		*p = map[string]any{"id": "loop-t6", "echo": json.RawMessage(raw)}
	}
	return nil
}

func (c *triggerCaller) last(method protocol.MethodName) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.sent[string(method)]
	if len(list) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(list[len(list)-1]), &m); err != nil {
		return nil
	}
	return m
}

func mustReadFile(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func (c *triggerCaller) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.called)
}

// dispatchedAttended reads the attended value the daemon would ACTUALLY
// see. Absent counts as a failure wherever a PIN is required: the pin is
// a written false, not a silent nothing (a missing field is a default
// waiting to be invented).
func dispatchedAttended(t *testing.T, params map[string]any) any {
	t.Helper()
	v, ok := params["attended"]
	if !ok {
		t.Fatal("the trigger dispatched without an attended field at all — forced means WRITTEN, not omitted")
	}
	return v
}

func startBody(withAttended string) string {
	return `{"blueprint":"dev-loop","goal":"ship T6","workspaceRoot":"/home/ci/repo","allowOutward":true` + withAttended + `}`
}

// TestHTTPTriggerPinsUnattended — the HTTP mounting, end to end through
// ServeHTTP: a caller claiming `"attended":true` in the POST body is not
// believed, not asked for proof, not answered-with-an-error — the claim
// is overwritten and the daemon receives false. allowOutward rides
// through untouched (it is a decision the caller is ALLOWED to make, at
// this one moment, for this one loop).
func TestHTTPTriggerPinsUnattended(t *testing.T) {
	c := newTriggerCaller()
	mux := NewMux(c, Actions())
	req := httptest.NewRequest(http.MethodPost, "/v1/loops", strings.NewReader(startBody(`,"attended":true`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("trigger POST answered %d: %s", rec.Code, rec.Body.String())
	}
	params := c.last(protocol.MethodLoopStart)
	if params == nil {
		t.Fatal("loop.start was not dispatched")
	}
	if v := dispatchedAttended(t, params); v != false {
		t.Errorf("daemon received attended=%v; want false — the caller's claim must not survive the trigger", v)
	}
	if params["allowOutward"] != true {
		t.Errorf("allowOutward was lost or rewritten in transit: %v", params["allowOutward"])
	}
	if params["blueprint"] != "dev-loop" || params["goal"] != "ship T6" {
		t.Errorf("the transform disturbed fields that are the caller's to set: %v", params)
	}
}

// TestStdioTriggerPinsUnattended — the same claim on the other mounting,
// asserted separately (N18): a tools/call claiming attended true is
// overwritten, and a no-arguments call still arrives pinned (the empty
// default runs before the transform — order matters and this test is
// that order).
func TestStdioTriggerPinsUnattended(t *testing.T) {
	c := newTriggerCaller()
	srv := NewServer(c, Actions())
	inR, inW := io.Pipe()
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background(), inR, &out)
		close(done)
	}()
	fmt.Fprintf(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"aeon_loop_start","arguments":%s}}`+"\n", startBody(`,"attended":true`))
	fmt.Fprintf(inW, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aeon_loop_start","arguments":{}}}`+"\n")
	inW.Close()
	<-done

	params := c.last(protocol.MethodLoopStart)
	if params == nil {
		t.Fatal("the stdio trigger never reached the daemon")
	}
	// The LAST dispatch is the no-arguments call: even it is pinned.
	if v := dispatchedAttended(t, params); v != false {
		t.Errorf("stdio no-argument trigger dispatched attended=%v; want false", v)
	}
	// And the claimed-true call (first) was overwritten too.
	c.mu.Lock()
	first := c.sent[string(protocol.MethodLoopStart)][0]
	c.mu.Unlock()
	if !strings.Contains(first, `"attended":false`) {
		t.Errorf("the stdio trigger forwarded the caller's claim: %s", first)
	}
}

// TestTriggerTransformIsOneDefinition — N18 made structural: both
// mountings call the SAME two functions, neither owns its own copy, and
// no third place in the package forces attendedness. Grep-counted over
// non-test sources: the literal `"attended"` appears exactly once
// (trigger.go's forcing), and each Trigger* entry point has exactly one
// call site outside trigger.go.
func TestTriggerTransformIsOneDefinition(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	hits := map[string]int{}
	callsites := map[string]int{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		hits[`"attended"`] += strings.Count(string(src), `"attended"`)
		if f != "trigger.go" {
			callsites["TriggerParamsFor"] += strings.Count(string(src), "TriggerParamsFor(")
			callsites["TriggerParamsJSON"] += strings.Count(string(src), "TriggerParamsJSON(")
		}
	}
	if hits[`"attended"`] != 1 {
		t.Errorf(`"attended" appears %d times in non-test sources; want exactly 1 (trigger.go's force) — a second forcing site is a second trigger`, hits[`"attended"`])
	}
	if callsites["TriggerParamsFor"] != 1 || callsites["TriggerParamsJSON"] != 1 {
		t.Errorf("transform call sites: %v; want one each (http.go and stdio.go) — two copies is how mountings drift", callsites)
	}
}

// TestNoKeyLevelGrantExists — O2's grep, made structural: reflect over
// every exported type in the package and grep every non-test source for
// standing-permission vocabulary. A key is a key. If a standing outward
// grant ever appears — on the key, the validator, the server, or a
// package-level map — this gate fails at compile of its intent.
func TestNoKeyLevelGrantExists(t *testing.T) {
	// (a) No exported type in the package carries a grant-shaped field.
	granty := regexp.MustCompile(`(?i)^(grants?$|permissions?$|roles?$|privileges?$|allowed)|outward`)
	for _, name := range []any{Key{}, Validator{}, Error{}, Action{}, MuxEntry{}} {
		tv := reflect.TypeOf(name)
		for i := 0; i < tv.NumField(); i++ {
			if f := tv.Field(i); granty.MatchString(f.Name) {
				t.Errorf("%s.%s is grant-shaped — outward authority belongs on the loop's start body and nowhere else (O2)", tv.Name(), f.Name)
			}
		}
	}
	// (b) The one appearance of the outward vocabulary in the package is
	// explanatory (comments, the schema description) — never a field,
	// map, or config read.
	files, _ := filepath.Glob("*.go")
	nonTest := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		nonTest++
		for i, line := range strings.Split(string(mustReadFile(t, f)), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			if strings.Contains(trimmed, "allowOutward") && strings.Contains(trimmed, "=") && !strings.Contains(trimmed, "==") {
				t.Errorf("%s:%d assigns from allowOutward outside comments — the surface must pass it through mechanically, not consume it: %q", f, i+1, trimmed)
			}
		}
	}
	if nonTest == 0 {
		t.Fatal("grep walked no files — the absence proved nothing")
	}
}

// TestToolsListCarriesTheLever — N14's legibility half: the CI author
// reading tools/list learns, without asking, that the loop is unattended
// always, that allowOutward is the one outward lever and is per-loop,
// that no key-level grant exists, and that irreversible is not on offer.
// A refusal nobody can see the shape of invites the wrong workaround.
func TestToolsListCarriesTheLever(t *testing.T) {
	srv := NewServer(newTriggerCaller(), Actions())
	inR, inW := io.Pipe()
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background(), inR, &out)
		close(done)
	}()
	fmt.Fprintln(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	inW.Close()
	<-done
	body := out.String()
	if !strings.Contains(body, "allowOutward") {
		t.Error("tools/list never names the one outward lever")
	}
	for _, want := range []string{"unattended", "no key-level grant", "irreversible"} {
		if !strings.Contains(body, want) {
			t.Errorf("tools/list is missing %q — the CI author should not have to read Go source to learn the rules", want)
		}
	}
	if strings.Contains(body, "allowOutward\":true") && strings.Contains(body, "always") {
		// Not a refusal of the refusal: just making sure the description
		// does not promise the flag is automatic — it must read as a
		// declaration the CALLER makes.
		if strings.Contains(body, "automatically") {
			t.Error("the schema description suggests the opt-in is automatic")
		}
	}
}
