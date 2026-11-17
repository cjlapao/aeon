package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// D9 asserts over the WHOLE table, not a sample. A code added at S9 without its
// three parts fails here — which is the reason this test enumerates.
func TestEveryErrorCodeHasWhatMeansTodoAndFatal(t *testing.T) {
	for _, e := range ErrorRegistry() {
		if e.Message == "" {
			t.Errorf("code %d: empty message", e.Code)
		}
		if strings.TrimSpace(e.Data.What) == "" {
			t.Errorf("code %d (%s): empty `what`", e.Code, e.Message)
		}
		if strings.TrimSpace(e.Data.Means) == "" {
			t.Errorf("code %d (%s): empty `means`", e.Code, e.Message)
		}
		if strings.TrimSpace(e.Data.Todo) == "" {
			t.Errorf("code %d (%s): empty `todo`", e.Code, e.Message)
		}
		// Fatal is a bool, so there is nothing to assert about its presence —
		// what is assertable is that it is marshalled, since a client cannot
		// guess it and an omitted field would read as false.
		raw, err := e.MarshalData()
		if err != nil {
			t.Fatalf("code %d: marshal data: %v", e.Code, err)
		}
		if !strings.Contains(string(raw), `"fatal"`) {
			t.Errorf("code %d: `fatal` is not in the marshalled data: %s", e.Code, raw)
		}
	}
}

func TestErrorCodesAreUnique(t *testing.T) {
	seen := map[int]string{}
	for _, e := range ErrorRegistry() {
		if prev, dup := seen[e.Code]; dup {
			t.Errorf("code %d allocated twice: %q and %q", e.Code, prev, e.Message)
		}
		seen[e.Code] = e.Message
	}
}

// JSON-RPC's own codes are reserved by the specification. An Aeon code in that
// range would be indistinguishable from a transport condition.
func TestNoErrorCodeFallsInJSONRPCReservedRange(t *testing.T) {
	for _, e := range ErrorRegistry() {
		if e.Code >= ReservedJSONRPCMin && e.Code <= ReservedJSONRPCMax {
			t.Errorf("code %d (%s) is inside JSON-RPC's reserved range %d..%d",
				e.Code, e.Message, ReservedJSONRPCMin, ReservedJSONRPCMax)
		}
		if e.Code < AeonCodeMin || e.Code > AeonCodeMax {
			t.Errorf("code %d (%s) is outside Aeon's range %d..%d",
				e.Code, e.Message, AeonCodeMin, AeonCodeMax)
		}
	}
}

// The design's §Errors illustrates the shape with -31004. That is an
// illustration, not an allocation — questions arrive at S6 and take a domain
// code. This test keeps -31004 free so the two can never be confused.
func TestIllustrativeCodeIsLeftUnallocated(t *testing.T) {
	for _, e := range ErrorRegistry() {
		if e.Code == -31004 {
			t.Errorf("-31004 is allocated to %q; the design uses it illustratively for a question "+
				"error that belongs to S6's domain band", e.Message)
		}
	}
}

// WithWhat must not mutate the registry entry. A handler filling in specifics
// on a package-level value would corrupt every later use of that code — a bug
// that only appears under concurrency, and then as the wrong error text.
func TestWithWhatDoesNotMutateTheRegistryEntry(t *testing.T) {
	before := ErrProtocolVersionUnsupported.Data.What
	_ = ErrProtocolVersionUnsupported.WithWhat("client needs %d", 99)
	if ErrProtocolVersionUnsupported.Data.What != before {
		t.Fatalf("registry entry mutated:\n  before %q\n   after %q",
			before, ErrProtocolVersionUnsupported.Data.What)
	}
}

func TestWithWhatFillsInTheSpecifics(t *testing.T) {
	e := ErrNotInitialized.WithWhat("method %q before initialize", "subscribe")
	if !strings.Contains(e.Data.What, `"subscribe"`) {
		t.Fatalf("what did not take the argument: %q", e.Data.What)
	}
	if e.Code != ErrNotInitialized.Code || e.Data.Means != ErrNotInitialized.Data.Means {
		t.Fatal("WithWhat changed something other than `what`")
	}
	if !e.Data.Fatal {
		t.Fatal("WithWhat lost the fatal marker")
	}
}

// The wire shape, asserted rather than assumed: a client reads `data.what`,
// `data.means`, `data.todo` and `data.fatal` by those names.
func TestErrorMarshalsToTheThreePartShape(t *testing.T) {
	raw, err := json.Marshal(ErrInvalidFilter)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			What  string `json:"what"`
			Means string `json:"means"`
			Todo  string `json:"todo"`
			Fatal bool   `json:"fatal"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Code != -31010 || back.Data.What == "" || back.Data.Means == "" || back.Data.Todo == "" {
		t.Fatalf("three-part shape did not survive marshalling: %s", raw)
	}
}
