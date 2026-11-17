package protocol

// scorecard_conformance_test.go — S63 T5: the `conformance/scorecard`,
// `conformance/retro` and `conformance/digest` fixtures are the wire
// contract for the whole outcome-learning plane, replayed through the
// real Dispatch the way every other plane's fixtures are (the
// merge-queue runner is the template). Two kinds, same law:
//
//   - fromSeam — the read service refused and the door carried ITS
//     sentence intact, code and all;
//   - fromDoor — the door itself refused before any service was
//     consulted (a typed actor, an unconfirmed backfill write, a digest
//     without its component). A door refusal that reached the service
//     is a refusal that already touched the data.
//
// The plane's forbidden payload, checked on every successful fixture:
// hunk text, diffs, and credential shapes — a scorecard that leaked a
// diff or a secret in a metric note is the plane broken (SEC-033's
// law, kept).

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

type planeFixture struct {
	dir, file   string
	Description string          `json:"description"`
	Method      MethodName      `json:"method"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	FromDoor    bool            `json:"fromDoor"`
	FromSeam    bool            `json:"fromSeam"`
}

func (f planeFixture) id() string { return f.dir + "/" + f.file }

// planeConfStub answers every door of the plane with the fixture's own
// result, decoded into the method's own return type — the fixture, not
// a Go literal, is the single source of truth for what the wire carries.
type planeConfStub struct {
	t       *testing.T
	raw     json.RawMessage
	err     error
	calls   map[MethodName]int
	reached bool
}

func (s *planeConfStub) into(m MethodName, into any) {
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

// ScorecardService.
func (s *planeConfStub) Agent(_ context.Context, _ ScorecardAgentParams) (ScorecardCardResult, error) {
	var out ScorecardCardResult
	s.into(MethodScorecardAgent, &out)
	return out, s.err
}
func (s *planeConfStub) Blueprint(_ context.Context, _ ScorecardBlueprintParams) (ScorecardCardResult, error) {
	var out ScorecardCardResult
	s.into(MethodScorecardBlueprint, &out)
	return out, s.err
}
func (s *planeConfStub) Component(_ context.Context, _ ScorecardComponentParams) (ScorecardComponentResult, error) {
	var out ScorecardComponentResult
	s.into(MethodScorecardComponent, &out)
	return out, s.err
}
func (s *planeConfStub) Compare(_ context.Context, _ ScorecardCompareParams) (ScorecardCompareResult, error) {
	var out ScorecardCompareResult
	s.into(MethodScorecardCompare, &out)
	return out, s.err
}
func (s *planeConfStub) Vocabulary(_ context.Context) (ScorecardVocabularyResult, error) {
	var out ScorecardVocabularyResult
	s.into(MethodScorecardVocabulary, &out)
	return out, s.err
}

// RetroService.
func (s *planeConfStub) List(_ context.Context, _ RetroListParams) (RetroListResult, error) {
	var out RetroListResult
	s.into(MethodRetroList, &out)
	return out, s.err
}
func (s *planeConfStub) Show(_ context.Context, _ RetroShowParams) (RetroShowResult, error) {
	var out RetroShowResult
	s.into(MethodRetroShow, &out)
	return out, s.err
}
func (s *planeConfStub) Status(_ context.Context) (RetroStatusResult, error) {
	var out RetroStatusResult
	s.into(MethodRetroStatus, &out)
	return out, s.err
}
func (s *planeConfStub) Backfill(_ context.Context, _ Actor, _ RetroBackfillParams) (RetroBackfillWire, error) {
	var out RetroBackfillWire
	s.into(MethodRetroBackfill, &out)
	return out, s.err
}

// DigestService.
func (s *planeConfStub) Learned(_ context.Context, _ DigestParams) (DigestResult, error) {
	var out DigestResult
	s.into(MethodDigestLearned, &out)
	return out, s.err
}

func TestOutcomeLearningConformanceFixturesMatchTheWire(t *testing.T) {
	var all []planeFixture
	for dir, want := range map[string]int{"scorecard": 6, "retro": 6, "digest": 2} {
		all = append(all, readPlaneFixtures(t, dir, want)...)
	}
	for _, f := range all {
		f := f
		t.Run(f.id(), func(t *testing.T) { runPlaneFixture(t, f) })
	}
}

func readPlaneFixtures(t *testing.T, dir string, want int) []planeFixture {
	t.Helper()
	full := filepath.Join("conformance", dir)
	entries, err := os.ReadDir(full)
	if err != nil {
		t.Fatalf("read %s: %v — the fixtures are the contract, and the contract is missing", full, err)
	}
	var out []planeFixture
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(full, en.Name()))
		if err != nil {
			t.Fatalf("read %s/%s: %v", full, en.Name(), err)
		}
		var f planeFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("%s/%s is not a fixture: %v", full, en.Name(), err)
		}
		if strings.TrimSpace(f.Description) == "" {
			t.Errorf("%s/%s carries no description", full, en.Name())
		}
		if f.Method == "" {
			t.Errorf("%s/%s names no method", full, en.Name())
		}
		f.dir, f.file = dir, en.Name()
		out = append(out, f)
	}
	if len(out) < want {
		t.Fatalf("conformance/%s holds %d fixtures, want at least %d", dir, len(out), want)
	}
	return out
}

func runPlaneFixture(t *testing.T, f planeFixture) {
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
		t.Errorf("fixture method %q and request method %q disagree", f.Method, req.Method)
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
		case ErrScorecardRefused.Code:
			e = ErrScorecardRefused.WithWhat("%s", want.Error.Data.What)
		case ErrRetroUnavailable.Code:
			e = ErrRetroUnavailable.WithWhat("%s", want.Error.Data.What)
		default:
			t.Fatalf("fixture %s declares seam code %d, which is not this plane's voice", f.id(), want.Error.Code)
		}
		if want.Error.Data.Means != "" {
			e.Data.Means = want.Error.Data.Means
		}
		if want.Error.Data.Todo != "" {
			e.Data.Todo = want.Error.Data.Todo
		}
		seamErr = e
	}

	stub := &planeConfStub{t: t, raw: want.Result, err: seamErr, calls: map[MethodName]int{}}
	tbl := newTestTable(t)
	tbl.ActorFor = userActorFor("peer-1")
	tbl.Scorecard = stub
	tbl.Retro = stub
	tbl.Digest = stub
	if !tbl.RegisterScorecard() || !tbl.RegisterRetro() || !tbl.RegisterDigest() {
		t.Fatal("the plane did not register over its seam — the fixture has nothing to speak to")
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
				t.Errorf("a door refusal's fixture declares code %d; ErrBadParams is the transport's -32602", want.Error.Code)
			}
			if want.Error.Data.What != "" || want.Error.Data.Kind != "" {
				t.Errorf("a transport-level refusal carries no `data` and this fixture declares some: %+v", want.Error.Data)
			}
			if g := err.Error(); g != want.Error.Message {
				t.Errorf("the refusal's sentence drifted:\n got  %q\n want %q", g, want.Error.Message)
			}
			if f.FromDoor && stub.reached {
				t.Errorf("a door refusal reached the service (calls %v)", stub.calls)
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
			t.Errorf("the refusal is missing a part — what/means/todo: %+v", perr.Data)
		}
		if f.FromDoor && stub.reached {
			t.Errorf("a door refusal reached the service (calls %v)", stub.calls)
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
	lower := strings.ToLower(string(gotRaw))
	for _, bad := range []string{"hunk", "diff --", "patch", "sk-live", "sk-ant-", "password", "token\":"} {
		if strings.Contains(lower, bad) {
			t.Errorf("the answer carries what the plane's wire must never carry (%q) — SEC-033", bad)
		}
	}
}
