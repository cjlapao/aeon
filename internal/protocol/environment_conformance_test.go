package protocol

// environment_conformance_test.go — S59 T6: the nine `conformance/environment`
// and `conformance/secretref` fixtures are the contract, and this walks them
// the way every other plane's fixtures are walked: replay the fixture's request
// through the real `Dispatch` and compare what comes back, semantically.
//
// The fixtures exist because Aeon's client and daemon can agree with each other
// while both being wrong. These files are what a TypeScript extension and the
// Rust shell test against, and they were written from the plane's rules, not
// from the Go structs — which is the only way a fixture can catch a struct that
// grew a field a value should never sit in.
//
// Two kinds of fixture, and the difference is the point:
//
//   - `fromSeam` — the service (here, the store underneath it) refused, and the
//     door carried ITS sentence. The stub answers with that error; the test
//     checks it survives the door intact.
//   - `fromDoor` — the door itself refused, before any service was consulted.
//     The stub is healthy and must never be reached: a refusal that happens
//     after the service is a refusal that already touched the data.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type environmentFixture struct {
	// file is the fixture's name on disk, set while reading. The subtest is
	// named for it, because the name is what a person greps for when the
	// contract and the wire disagree.
	file        string
	Description string          `json:"description"`
	Method      MethodName      `json:"method"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	FromDoor    bool            `json:"fromDoor"`
	FromSeam    bool            `json:"fromSeam"`
}

// conformanceStub is the twenty-door seam that answers with the fixture. Each
// method decodes the fixture's own `result` into its own return type, so the
// fixture — not a Go literal in this test — is the single source of truth for
// what the wire carries.
type conformanceStub struct {
	t       *testing.T
	raw     json.RawMessage
	err     error
	calls   map[MethodName]int
	reached bool
}

func newConformanceStub(t *testing.T, raw json.RawMessage, err error) *conformanceStub {
	return &conformanceStub{t: t, raw: raw, err: err, calls: map[MethodName]int{}}
}

// into decodes the fixture's result into `into`, failing loudly when the
// fixture's shape is not the shape the method returns. A fixture that cannot
// be decoded into the type is a fixture that disagrees with the plane, which is
// exactly what this file exists to discover.
func (s *conformanceStub) into(m MethodName, into any) {
	s.t.Helper()
	s.calls[m]++
	s.reached = true
	if s.err != nil {
		return
	}
	if len(s.raw) == 0 {
		return
	}
	if err := json.Unmarshal(s.raw, into); err != nil {
		s.t.Fatalf("the fixture's result does not decode into %s's return type: %v", m, err)
	}
}

func (s *conformanceStub) List(_ context.Context, _ EnvironmentListParams) ([]EnvironmentView, error) {
	var out []EnvironmentView
	s.into(MethodEnvironmentList, &out)
	return out, s.err
}
func (s *conformanceStub) Show(_ context.Context, _ EnvironmentShowParams) (EnvironmentView, error) {
	var out EnvironmentView
	s.into(MethodEnvironmentShow, &out)
	return out, s.err
}
func (s *conformanceStub) Effective(_ context.Context, _ EnvironmentEffectiveParams) (EnvironmentEffectiveView, error) {
	var out EnvironmentEffectiveView
	s.into(MethodEnvironmentEffective, &out)
	return out, s.err
}
func (s *conformanceStub) Vocabulary(_ context.Context) (EnvironmentVocabularyView, error) {
	var out EnvironmentVocabularyView
	s.into(MethodEnvironmentVocabulary, &out)
	return out, s.err
}
func (s *conformanceStub) Changes(_ context.Context, _ EnvironmentChangesParams) ([]EnvironmentChangeView, error) {
	var out []EnvironmentChangeView
	s.into(MethodEnvironmentChanges, &out)
	return out, s.err
}
func (s *conformanceStub) Provisions(_ context.Context, _ EnvironmentProvisionsParams) ([]ProvisionView, error) {
	var out []ProvisionView
	s.into(MethodEnvironmentProvisions, &out)
	return out, s.err
}
func (s *conformanceStub) Create(_ context.Context, _ Actor, _ EnvironmentCreateParams) (EnvironmentView, error) {
	var out EnvironmentView
	s.into(MethodEnvironmentCreate, &out)
	return out, s.err
}
func (s *conformanceStub) Update(_ context.Context, _ Actor, _ EnvironmentUpdateParams) (EnvironmentView, error) {
	var out EnvironmentView
	s.into(MethodEnvironmentUpdate, &out)
	return out, s.err
}
func (s *conformanceStub) Archive(_ context.Context, _ Actor, _ EnvironmentIDParams) (EnvironmentView, error) {
	var out EnvironmentView
	s.into(MethodEnvironmentArchive, &out)
	return out, s.err
}
func (s *conformanceStub) Restore(_ context.Context, _ Actor, _ EnvironmentIDParams) (EnvironmentView, error) {
	var out EnvironmentView
	s.into(MethodEnvironmentRestore, &out)
	return out, s.err
}
func (s *conformanceStub) Bind(_ context.Context, _ Actor, _ EnvironmentBindParams) (BindResult, error) {
	var out BindResult
	s.into(MethodEnvironmentBind, &out)
	return out, s.err
}
func (s *conformanceStub) Unbind(_ context.Context, _ Actor, _ EnvironmentUnbindParams) (UnbindResult, error) {
	var out UnbindResult
	s.into(MethodEnvironmentUnbind, &out)
	return out, s.err
}
func (s *conformanceStub) Teardown(_ context.Context, _ Actor, _ ProvisionIDParams) (ProvisionView, error) {
	var out ProvisionView
	s.into(MethodEnvironmentTeardown, &out)
	return out, s.err
}

func (s *conformanceStub) ListRefs(_ context.Context, _ SecretRefListParams) ([]SecretRefView, error) {
	var out []SecretRefView
	s.into(MethodSecretRefList, &out)
	return out, s.err
}

func (s *conformanceStub) ShowRef(_ context.Context, _ SecretRefShowParams) (SecretRefView, error) {
	var out SecretRefView
	s.into(MethodSecretRefShow, &out)
	return out, s.err
}
func (s *conformanceStub) CheckRef(_ context.Context, _ SecretRefCheckParams) (SecretRefCheckResult, error) {
	var out SecretRefCheckResult
	s.into(MethodSecretRefCheck, &out)
	return out, s.err
}
func (s *conformanceStub) CreateRef(_ context.Context, _ Actor, _ SecretRefCreateParams) (SecretRefView, error) {
	var out SecretRefView
	s.into(MethodSecretRefCreate, &out)
	return out, s.err
}
func (s *conformanceStub) UpdateRef(_ context.Context, _ Actor, _ SecretRefUpdateParams) (SecretRefView, error) {
	var out SecretRefView
	s.into(MethodSecretRefUpdate, &out)
	return out, s.err
}
func (s *conformanceStub) ArchiveRef(_ context.Context, _ Actor, _ SecretRefNameParams) (SecretRefView, error) {
	var out SecretRefView
	s.into(MethodSecretRefArchive, &out)
	return out, s.err
}
func (s *conformanceStub) RestoreRef(_ context.Context, _ Actor, _ SecretRefNameParams) (SecretRefView, error) {
	var out SecretRefView
	s.into(MethodSecretRefRestore, &out)
	return out, s.err
}

// readFixtures loads one conformance directory and asserts it is not empty: a
// walker over an empty directory is a pass that checked nothing.
func readFixtures(t *testing.T, dir string, want int) []environmentFixture {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v — the fixtures are the contract, and the contract is missing", dir, err)
	}
	var out []environmentFixture
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err != nil {
			t.Fatalf("read %s/%s: %v", dir, en.Name(), err)
		}
		var f environmentFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("%s/%s is not a fixture: %v", dir, en.Name(), err)
		}
		if strings.TrimSpace(f.Description) == "" {
			t.Errorf("%s/%s carries no description — a fixture nobody can read is a fixture nobody can fix", dir, en.Name())
		}
		if f.Method == "" {
			t.Errorf("%s/%s names no method", dir, en.Name())
		}
		f.file = en.Name()
		out = append(out, f)
	}
	if len(out) < want {
		t.Fatalf("%s holds %d fixtures, want at least %d — this slice's own answer in the plane's own words", dir, len(out), want)
	}
	return out
}

// TestEnvironmentConformanceFixturesMatchTheWire replays every fixture through
// the real dispatcher and compares what comes back.
func TestEnvironmentConformanceFixturesMatchTheWire(t *testing.T) {
	envFixtures := readFixtures(t, filepath.Join("conformance", "environment"), 5)
	refFixtures := readFixtures(t, filepath.Join("conformance", "secretref"), 3)

	for _, f := range append(envFixtures, refFixtures...) {
		f := f
		t.Run(f.file, func(t *testing.T) {
			runEnvironmentFixture(t, f)
		})
	}
}

// expectedEnvelope is the fixture's `response` object.
type expectedEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Kind  string `json:"kind"`
			What  string `json:"what"`
			Means string `json:"means"`
			Todo  string `json:"todo"`
		} `json:"data"`
	} `json:"error"`
}

func runEnvironmentFixture(t *testing.T, f environmentFixture) {
	t.Helper()
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  MethodName      `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(f.Request, &req); err != nil {
		t.Fatalf("the fixture's request is not a JSON-RPC call: %v", err)
	}
	if req.Method != f.Method {
		t.Errorf("the fixture's top-level method %q and its request method %q disagree — a client reading one field would call the wrong door", f.Method, req.Method)
	}
	var want expectedEnvelope
	if err := json.Unmarshal(f.Response, &want); err != nil {
		t.Fatalf("the fixture's response is not a JSON-RPC answer: %v", err)
	}
	if want.Result == nil && want.Error == nil {
		t.Fatal("the fixture's response is neither a result nor an error")
	}

	// The seam: a fixture whose refusal comes from the SERVICE answers with
	// that error; a door refusal is replayed against a HEALTHY seam, because
	// the whole claim of a door refusal is that it happened before the service
	// was consulted.
	var seamErr error
	if f.FromSeam && want.Error != nil {
		e := ErrEnvironmentRefused.WithWhat("%s", want.Error.Data.What)
		e.Data.Kind = want.Error.Data.Kind
		e.Data.Means = want.Error.Data.Means
		e.Data.Todo = want.Error.Data.Todo
		seamErr = e
	}
	stub := newConformanceStub(t, want.Result, seamErr)
	tbl := newTestTable(t)
	tbl.ActorFor = userActorFor("peer-1")
	// A door refusal must not need a working service: the seams are wired so the
	// method EXISTS, and the test asserts the stub was never reached.
	tbl.Environment = envSeam{stub}
	tbl.SecretRefs = refSeam{stub}
	if !tbl.RegisterEnvironment() || !tbl.RegisterSecretRef() {
		t.Fatal("the plane did not register over its seams — the fixture has nothing to speak to")
	}
	// S59 T7: the plane's ONE agent door is registered over the records seam
	// like every other catalog twin. The stub counts its calls and answers the
	// fixture's own result bytes, so the door-refusal fixture can assert the
	// records plane was NEVER consulted (the claim of a door refusal), and the
	// success fixture can assert the answer came THROUGH it (rule 4: the tool
	// door and this method are one implementation).
	propose := &envProposeConformanceRecords{recordNameSpy: &recordNameSpy{}}
	if want.Error == nil {
		propose.answer = append(json.RawMessage(nil), want.Result...)
	}
	tbl.Records = propose
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("environment.proposeRestrict did not register over the records seam — the fixture has no door to speak to")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	got, err := tbl.Dispatch(context.Background(), "c1", req.Method, req.Params)

	if want.Error != nil {
		var perr *Error
		if !errors.As(err, &perr) {
			// The other shape of refusal on this plane's doors: a transport-level
			// condition (conformance/README). JSON-RPC's own -32602 is the type
			// ErrBadParams (handshake.go:103-107), and the transport maps THAT
			// type to the code — so the fixture's claim is verified by the type
			// plus the code it declares, and the sentence travels in the message
			// with no `data` at all.
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("the fixture promises an error (code %d) and the wire gave %T (%v)", want.Error.Code, err, err)
			}
			if want.Error.Code != -32602 {
				t.Errorf("a door refusal's fixture declares code %d; ErrBadParams is the transport's -32602 (handshake.go:103)", want.Error.Code)
			}
			if want.Error.Data.What != "" || want.Error.Data.Kind != "" {
				t.Errorf("a transport-level refusal carries no `data` (conformance/README) and this fixture declares some: %+v", want.Error.Data)
			}
			if got := err.Error(); got != want.Error.Message {
				t.Errorf("the refusal's sentence drifted:\n got  %q\n want %q", got, want.Error.Message)
			}
			if f.FromDoor && stub.reached {
				t.Errorf("a door refusal reached the service (calls %v) — a refusal that happens after the service is a refusal that already touched the data", stub.calls)
			}
			return
		}
		if perr.Code != want.Error.Code {
			t.Errorf("code = %d, want %d", perr.Code, want.Error.Code)
		}
		if want.Error.Data.What != "" && perr.Data.What != want.Error.Data.What {
			t.Errorf("the refusal's `what` drifted:\n got  %q\n want %q", perr.Data.What, want.Error.Data.What)
		}
		if want.Error.Data.Kind != "" && perr.Data.Kind != want.Error.Data.Kind {
			t.Errorf("kind = %q, want %q — the closed set is the client's branch point", perr.Data.Kind, want.Error.Data.Kind)
		}
		// The three parts are the plane's fixed voice: presence is the rule
		// (every Aeon error carries what/means/todo), and pinning their text
		// here would only freeze the wording the rules ask a person to read.
		if perr.Data.Means == "" || perr.Data.Todo == "" {
			t.Errorf("the refusal is missing a part — what/means/todo is the interface when something goes wrong: %+v", perr.Data)
		}
		if f.FromDoor && stub.reached {
			t.Errorf("a door refusal reached the service (calls %v) — a refusal that happens after the service is a refusal that already touched the data", stub.calls)
		}
		if f.FromDoor && req.Method == MethodEnvironmentProposeRestrict && propose.calls != 0 {
			t.Errorf("a door refusal consulted the records plane %d times — the whole claim of a door refusal is that it happened before the plane was reached (S59 T7)", propose.calls)
		}
		return
	}

	if err != nil {
		t.Fatalf("the fixture promises a result and the wire errored: %v", err)
	}
	if req.Method == MethodEnvironmentProposeRestrict && propose.calls != 1 {
		t.Errorf("environment.proposeRestrict answered with %d calls to the records plane, want exactly 1 — the tool door and this method are one implementation, so an answer that did not go through it is a second rendering (rule 4)", propose.calls)
	}
	gotRaw, merr := json.Marshal(got)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	var a, b any
	if err := json.Unmarshal(gotRaw, &a); err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if err := json.Unmarshal(want.Result, &b); err != nil {
		t.Fatalf("the fixture's result is not JSON: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("the wire and the fixture disagree:\n got  %s\n want %s", gotRaw, want.Result)
	}
	// No value ever crosses this plane's wire (EV7) — checked on every fixture
	// rather than only the ones written to fail, so the day a field is added the
	// whole set complains.
	lower := strings.ToLower(string(gotRaw))
	for _, bad := range []string{"sk-live", "sk-ant-", "\"value\":", "\"token\":", "\"apikey\":", "\"secretvalue\":"} {
		if strings.Contains(lower, bad) {
			t.Errorf("the answer carries what looks like a credential (%q) — a reference names where a value lives (EV7)", bad)
		}
	}
}

// envSeam and refSeam adapt the recording stub to the two seams. They are
// separate types because the planes are two contracts, and one struct cannot
// carry two `List` methods — which is also why a fixture for one plane never
// accidentally answers for the other.
type envSeam struct{ s *conformanceStub }
type refSeam struct{ s *conformanceStub }

var (
	_ EnvironmentService = envSeam{}
	_ SecretRefService   = refSeam{}
)

func (e envSeam) List(ctx context.Context, p EnvironmentListParams) ([]EnvironmentView, error) {
	return e.s.List(ctx, p)
}
func (e envSeam) Show(ctx context.Context, p EnvironmentShowParams) (EnvironmentView, error) {
	return e.s.Show(ctx, p)
}
func (e envSeam) Effective(ctx context.Context, p EnvironmentEffectiveParams) (EnvironmentEffectiveView, error) {
	return e.s.Effective(ctx, p)
}
func (e envSeam) Vocabulary(ctx context.Context) (EnvironmentVocabularyView, error) {
	return e.s.Vocabulary(ctx)
}
func (e envSeam) Changes(ctx context.Context, p EnvironmentChangesParams) ([]EnvironmentChangeView, error) {
	return e.s.Changes(ctx, p)
}
func (e envSeam) Provisions(ctx context.Context, p EnvironmentProvisionsParams) ([]ProvisionView, error) {
	return e.s.Provisions(ctx, p)
}
func (e envSeam) Create(ctx context.Context, a Actor, p EnvironmentCreateParams) (EnvironmentView, error) {
	return e.s.Create(ctx, a, p)
}
func (e envSeam) Update(ctx context.Context, a Actor, p EnvironmentUpdateParams) (EnvironmentView, error) {
	return e.s.Update(ctx, a, p)
}
func (e envSeam) Archive(ctx context.Context, a Actor, p EnvironmentIDParams) (EnvironmentView, error) {
	return e.s.Archive(ctx, a, p)
}
func (e envSeam) Restore(ctx context.Context, a Actor, p EnvironmentIDParams) (EnvironmentView, error) {
	return e.s.Restore(ctx, a, p)
}
func (e envSeam) Bind(ctx context.Context, a Actor, p EnvironmentBindParams) (BindResult, error) {
	return e.s.Bind(ctx, a, p)
}
func (e envSeam) Unbind(ctx context.Context, a Actor, p EnvironmentUnbindParams) (UnbindResult, error) {
	return e.s.Unbind(ctx, a, p)
}
func (e envSeam) Teardown(ctx context.Context, a Actor, p ProvisionIDParams) (ProvisionView, error) {
	return e.s.Teardown(ctx, a, p)
}

func (r refSeam) List(ctx context.Context, p SecretRefListParams) ([]SecretRefView, error) {
	return r.s.ListRefs(ctx, p)
}
func (r refSeam) Show(ctx context.Context, p SecretRefShowParams) (SecretRefView, error) {
	return r.s.ShowRef(ctx, p)
}
func (r refSeam) Check(ctx context.Context, p SecretRefCheckParams) (SecretRefCheckResult, error) {
	return r.s.CheckRef(ctx, p)
}
func (r refSeam) Create(ctx context.Context, a Actor, p SecretRefCreateParams) (SecretRefView, error) {
	return r.s.CreateRef(ctx, a, p)
}
func (r refSeam) Update(ctx context.Context, a Actor, p SecretRefUpdateParams) (SecretRefView, error) {
	return r.s.UpdateRef(ctx, a, p)
}
func (r refSeam) Archive(ctx context.Context, a Actor, p SecretRefNameParams) (SecretRefView, error) {
	return r.s.ArchiveRef(ctx, a, p)
}
func (r refSeam) Restore(ctx context.Context, a Actor, p SecretRefNameParams) (SecretRefView, error) {
	return r.s.RestoreRef(ctx, a, p)
}
