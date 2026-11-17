package protocol

// mergequeue_conformance_test.go — S60 T8: the `conformance/mergequeue`
// fixtures are the contract, walked the way every other plane's fixtures
// are (the environment runner is the template): replay each fixture's
// request through the real `Dispatch` and compare what comes back,
// semantically. A TypeScript extension and the Rust shell test against
// these files; they were written from the plane's rules (MQ13, MQ16, MQ17),
// not from the Go structs — the only way a fixture can catch a struct that
// grew a field a value should never sit in.
//
// Two kinds, and the difference is the point:
//
//   - fromSeam — the queue service (or the store under it) refused, and the
//     door carried ITS sentence in the `what`. The stub answers with that
//     error; the test checks it survives the door intact, code and all.
//   - fromDoor — the door itself refused, before any service was consulted
//     (identity keys in params, the vocabulary given params, a land without
//     its digest). The stub is healthy and must never be reached.

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

type mergeQueueFixture struct {
	file        string
	Description string          `json:"description"`
	Method      MethodName      `json:"method"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	FromDoor    bool            `json:"fromDoor"`
	FromSeam    bool            `json:"fromSeam"`
}

// mqConformanceStub answers with the fixture. Each method decodes the
// fixture's own `result` into its own return type, so the fixture — not a Go
// literal here — is the single source of truth for what the wire carries.
type mqConformanceStub struct {
	t       *testing.T
	raw     json.RawMessage
	err     error
	calls   map[MethodName]int
	reached bool
}

func (s *mqConformanceStub) into(m MethodName, into any) {
	s.t.Helper()
	s.calls[m]++
	s.reached = true
	if s.err != nil || len(s.raw) == 0 {
		return
	}
	if err := json.Unmarshal(s.raw, into); err != nil {
		s.t.Fatalf("the fixture's result does not decode into %s's return type: %v", m, err)
	}
}

func (s *mqConformanceStub) List(_ context.Context, _ MergeQueueListParams) ([]ItemView, error) {
	var out []ItemView
	s.into(MethodMergeQueueList, &out)
	return out, s.err
}
func (s *mqConformanceStub) Show(_ context.Context, _ MergeQueueShowParams) (ShowResult, error) {
	var out ShowResult
	s.into(MethodMergeQueueShow, &out)
	return out, s.err
}
func (s *mqConformanceStub) Status(_ context.Context, _ MergeQueueStatusParams) (StatusResult, error) {
	var out StatusResult
	s.into(MethodMergeQueueStatus, &out)
	return out, s.err
}
func (s *mqConformanceStub) Vocabulary(_ context.Context) (VocabularyResult, error) {
	var out VocabularyResult
	s.into(MethodMergeQueueVocabulary, &out)
	out.Methods = mergeQueueDoorViews() // the derived half is the door's
	return out, s.err
}
func (s *mqConformanceStub) Enqueue(_ context.Context, _ Actor, _ MergeQueueEnqueueParams) (ItemView, error) {
	var out ItemView
	s.into(MethodMergeQueueEnqueue, &out)
	return out, s.err
}
func (s *mqConformanceStub) Dequeue(_ context.Context, _ Actor, _ MergeQueueIDParams) (ItemView, error) {
	var out ItemView
	s.into(MethodMergeQueueDequeue, &out)
	return out, s.err
}
func (s *mqConformanceStub) Reorder(_ context.Context, _ Actor, _ MergeQueueReorderParams) (ItemView, error) {
	var out ItemView
	s.into(MethodMergeQueueReorder, &out)
	return out, s.err
}
func (s *mqConformanceStub) Retry(_ context.Context, _ Actor, _ MergeQueueIDParams) (ItemView, error) {
	var out ItemView
	s.into(MethodMergeQueueRetry, &out)
	return out, s.err
}
func (s *mqConformanceStub) Land(_ context.Context, _ Actor, _ MergeQueueLandParams) (ItemView, error) {
	var out ItemView
	s.into(MethodMergeQueueLand, &out)
	return out, s.err
}
func (s *mqConformanceStub) Settings(_ context.Context, _ Actor, _ MergeQueueSettingsParams) (ConfigView, string, error) {
	var wrapped SettingsResult
	s.into(MethodMergeQueueSettings, &wrapped)
	return wrapped.Config, wrapped.Statement, s.err
}

var _ MergeQueueService = (*mqConformanceStub)(nil)

func readMergeQueueFixtures(t *testing.T, want int) []mergeQueueFixture {
	t.Helper()
	dir := filepath.Join("conformance", "mergequeue")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v — the fixtures are the contract, and the contract is missing", dir, err)
	}
	var out []mergeQueueFixture
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err != nil {
			t.Fatalf("read %s/%s: %v", dir, en.Name(), err)
		}
		var f mergeQueueFixture
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

func TestMergeQueueConformanceFixturesMatchTheWire(t *testing.T) {
	for _, f := range readMergeQueueFixtures(t, 14) {
		f := f
		t.Run(f.file, func(t *testing.T) {
			runMergeQueueFixture(t, f)
		})
	}
}

func runMergeQueueFixture(t *testing.T, f mergeQueueFixture) {
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

	var seamErr error
	if f.FromSeam && want.Error != nil {
		var e *Error
		switch want.Error.Code {
		case ErrMergeQueueRefused.Code:
			e = ErrMergeQueueRefused.WithWhat("%s", want.Error.Data.What)
		case ErrMergeQueueNotFound.Code:
			e = ErrMergeQueueNotFound.WithWhat("%s", want.Error.Data.What)
		case ErrMergeQueueBaseUnknown.Code:
			e = ErrMergeQueueBaseUnknown.WithWhat("%s", want.Error.Data.What)
		case ErrMergeQueueConflictState.Code:
			e = ErrMergeQueueConflictState.WithWhat("%s", want.Error.Data.What)
		default:
			t.Fatalf("fixture %s declares seam code %d, which is not this plane's voice", f.file, want.Error.Code)
		}
		if want.Error.Data.Means != "" {
			e.Data.Means = want.Error.Data.Means
		}
		if want.Error.Data.Todo != "" {
			e.Data.Todo = want.Error.Data.Todo
		}
		seamErr = e
	}
	stub := &mqConformanceStub{t: t, raw: want.Result, err: seamErr, calls: map[MethodName]int{}}
	tbl := newTestTable(t)
	tbl.ActorFor = userActorFor("peer-1")
	tbl.MergeQueue = stub
	if !tbl.RegisterMergeQueue() {
		t.Fatal("the plane did not register over its seam — the fixture has nothing to speak to")
	}
	// S60 T9 (MQ14.3): the ask twin rides the records seam like every other
	// catalog door (S59's shape, its stub reused verbatim) — the queue seam
	// above is never consulted by it, which is exactly what the
	// door-refusal fixture asserts and the success fixture proves: an ASK
	// files a row through the PROPOSALS plane and touches no lane.
	propose := &envProposeConformanceRecords{recordNameSpy: &recordNameSpy{}}
	if want.Error == nil {
		propose.answer = append(json.RawMessage(nil), want.Result...)
	}
	tbl.Records = propose
	if !tbl.RegisterMergeQueueProposeEnqueue() {
		t.Fatal("mergequeue.proposeEnqueue did not register over the records seam — the ask fixture has no door to speak to")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	got, err := tbl.Dispatch(context.Background(), "c1", req.Method, req.Params)

	if want.Error != nil {
		var perr *Error
		if !errors.As(err, &perr) {
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("the fixture promises an error (code %d) and the wire gave %T (%v)", want.Error.Code, err, err)
			}
			if want.Error.Code != -32602 {
				t.Errorf("a door refusal's fixture declares code %d; ErrBadParams is the transport's -32602 (handshake.go:106)", want.Error.Code)
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
		if perr.Data.Means == "" || perr.Data.Todo == "" {
			t.Errorf("the refusal is missing a part — what/means/todo is the interface when something goes wrong: %+v", perr.Data)
		}
		if f.FromDoor && stub.reached {
			t.Errorf("a door refusal reached the service (calls %v) — the whole claim of a door refusal is that it happened before the data", stub.calls)
		}
		return
	}

	if err != nil {
		t.Fatalf("the fixture promises a result and the wire errored: %v", err)
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
	// SEC-033's honesty clause on every fixture, not only the ones written
	// to fail: the queue's wire carries ids, enums, counts and paths — a
	// diff, a hunk, or a secret in any answer is the plane broken.
	lower := strings.ToLower(string(gotRaw))
	for _, bad := range []string{"diff", "hunk", "patch", "sk-live", "sk-ant-", "password", "token\":"} {
		if strings.Contains(lower, bad) {
			t.Errorf("the answer carries what the queue's wire must never carry (%q) — SEC-033", bad)
		}
	}
}
