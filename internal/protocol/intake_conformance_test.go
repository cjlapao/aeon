package protocol

// intake_conformance_test.go — S61 T7: the `conformance/charter`,
// `conformance/template` and `conformance/intake` fixtures are the wire
// contract for the goal-intake plane, replayed through the real Dispatch
// the way every other plane's fixtures are (the outcome-learning runner
// is the template). Two kinds, same law:
//
//   - fromSeam — the service refused and the door carried ITS sentence
//     intact, code and all (ErrCharterRefused -31964 / ErrIntakeRefused
//     -31965);
//   - fromDoor — the door itself refused before any service was consulted
//     (a typed actor key, an unresolvable peer on a person's door, an
//     intake.plan without its ids). A door refusal that reached the
//     service is a refusal that already touched the data.
//
// The plane's forbidden payload, checked on every successful fixture:
// credential shapes and a charter answering its body through anything
// but its canonical form.

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

type s61Fixture struct {
	dir, file   string
	Description string          `json:"description"`
	Method      MethodName      `json:"method"`
	Request     json.RawMessage `json:"request"`
	Response    json.RawMessage `json:"response"`
	FromDoor    bool            `json:"fromDoor"`
	FromSeam    bool            `json:"fromSeam"`
	PeerUID     string          `json:"peerUid"` // "" ⇒ the ordinary person peer
}

func (f s61Fixture) id() string { return f.dir + "/" + f.file }

// s61ConfStub answers every door of the plane with the fixture's own
// result, decoded into the method's own return type — the fixture, not a
// Go literal, is the single source of truth for what the wire carries —
// and counts every call, because TestIntakePlanIsReadOnly reads the
// census: plan-ok must show exactly one seam call, intake.plan's.
type s61ConfStub struct {
	t       *testing.T
	raw     json.RawMessage
	err     error
	calls   map[MethodName]int
	reached bool
	saved   Actor // the actor the door handed a write
}

func (s *s61ConfStub) into(m MethodName, into any) {
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

// CharterService.
func (s *s61ConfStub) CharterGet(_ context.Context, _ CharterGetParams) (CharterView, error) {
	var out CharterView
	s.into(MethodCharterGet, &out)
	return out, s.err
}
func (s *s61ConfStub) CharterList(_ context.Context) (CharterListResult, error) {
	var out CharterListResult
	s.into(MethodCharterList, &out)
	return out, s.err
}
func (s *s61ConfStub) CharterRevisions(_ context.Context, _ CharterRevisionsParams) (CharterRevisionsResult, error) {
	var out CharterRevisionsResult
	s.into(MethodCharterRevisions, &out)
	return out, s.err
}
func (s *s61ConfStub) CharterVocabulary(_ context.Context) (CharterVocabularyResult, error) {
	var out CharterVocabularyResult
	s.into(MethodCharterVocabulary, &out)
	return out, s.err
}
func (s *s61ConfStub) CharterSave(_ context.Context, a Actor, _ CharterSaveParams) (CharterView, error) {
	s.saved = a
	var out CharterView
	s.into(MethodCharterSave, &out)
	return out, s.err
}

// TemplateService (the fixture names the method; the stub shares one
// result per call).
func (s *s61ConfStub) TemplateList(_ context.Context, _ TemplateListParams) (TemplateListResult, error) {
	var out TemplateListResult
	s.into(MethodTemplateList, &out)
	return out, s.err
}
func (s *s61ConfStub) TemplateGet(_ context.Context, _ TemplateGetParams) (TemplateView, error) {
	var out TemplateView
	s.into(MethodTemplateGet, &out)
	return out, s.err
}
func (s *s61ConfStub) TemplateSave(_ context.Context, a Actor, _ TemplateSaveParams) (TemplateView, error) {
	s.saved = a
	var out TemplateView
	s.into(MethodTemplateSave, &out)
	return out, s.err
}

// IntakeService.
func (s *s61ConfStub) IntakeStart(_ context.Context, a Actor, _ IntakeStartParams) (IntakeView, error) {
	s.saved = a
	var out IntakeView
	s.into(MethodIntakeStart, &out)
	return out, s.err
}
func (s *s61ConfStub) IntakeList(_ context.Context) (IntakeListResult, error) {
	var out IntakeListResult
	s.into(MethodIntakeList, &out)
	return out, s.err
}
func (s *s61ConfStub) IntakeStatus(_ context.Context, _ IntakeStatusParams) (IntakeStatusView, error) {
	var out IntakeStatusView
	s.into(MethodIntakeStatus, &out)
	return out, s.err
}
func (s *s61ConfStub) IntakePlan(_ context.Context, _ IntakePlanParams) (IntakePreview, error) {
	var out IntakePreview
	s.into(MethodIntakePlan, &out)
	return out, s.err
}
func (s *s61ConfStub) IntakeCommit(_ context.Context, a Actor, _ IntakeCommitParams) (IntakeCommitResult, error) {
	s.saved = a
	var out IntakeCommitResult
	s.into(MethodIntakeCommit, &out)
	return out, s.err
}
func (s *s61ConfStub) IntakeAbandon(_ context.Context, a Actor, _ IntakeAbandonParams) (IntakeAbandonResult, error) {
	s.saved = a
	var out IntakeAbandonResult
	s.into(MethodIntakeAbandon, &out)
	return out, s.err
}
func (s *s61ConfStub) IntakeVocabulary(_ context.Context) (IntakeVocabularyResult, error) {
	var out IntakeVocabularyResult
	s.into(MethodIntakeVocabulary, &out)
	return out, s.err
}

// The three seams are separate interfaces; the stub answers each under
// its own name, so the table takes three views of the same object.
type charterConfView struct{ s *s61ConfStub }

func (v charterConfView) Get(ctx context.Context, p CharterGetParams) (CharterView, error) {
	return v.s.CharterGet(ctx, p)
}
func (v charterConfView) List(ctx context.Context) (CharterListResult, error) {
	return v.s.CharterList(ctx)
}
func (v charterConfView) Revisions(ctx context.Context, p CharterRevisionsParams) (CharterRevisionsResult, error) {
	return v.s.CharterRevisions(ctx, p)
}
func (v charterConfView) Vocabulary(ctx context.Context) (CharterVocabularyResult, error) {
	return v.s.CharterVocabulary(ctx)
}
func (v charterConfView) Save(ctx context.Context, a Actor, p CharterSaveParams) (CharterView, error) {
	return v.s.CharterSave(ctx, a, p)
}

type templateConfView struct{ s *s61ConfStub }

func (v templateConfView) List(ctx context.Context, p TemplateListParams) (TemplateListResult, error) {
	return v.s.TemplateList(ctx, p)
}
func (v templateConfView) Get(ctx context.Context, p TemplateGetParams) (TemplateView, error) {
	return v.s.TemplateGet(ctx, p)
}
func (v templateConfView) Save(ctx context.Context, a Actor, p TemplateSaveParams) (TemplateView, error) {
	return v.s.TemplateSave(ctx, a, p)
}

type intakeConfView struct{ s *s61ConfStub }

func (v intakeConfView) Start(ctx context.Context, a Actor, p IntakeStartParams) (IntakeView, error) {
	return v.s.IntakeStart(ctx, a, p)
}
func (v intakeConfView) List(ctx context.Context) (IntakeListResult, error) {
	return v.s.IntakeList(ctx)
}
func (v intakeConfView) Status(ctx context.Context, p IntakeStatusParams) (IntakeStatusView, error) {
	return v.s.IntakeStatus(ctx, p)
}
func (v intakeConfView) Plan(ctx context.Context, p IntakePlanParams) (IntakePreview, error) {
	return v.s.IntakePlan(ctx, p)
}
func (v intakeConfView) Commit(ctx context.Context, a Actor, p IntakeCommitParams) (IntakeCommitResult, error) {
	return v.s.IntakeCommit(ctx, a, p)
}
func (v intakeConfView) Abandon(ctx context.Context, a Actor, p IntakeAbandonParams) (IntakeAbandonResult, error) {
	return v.s.IntakeAbandon(ctx, a, p)
}
func (v intakeConfView) Vocabulary(ctx context.Context) (IntakeVocabularyResult, error) {
	return v.s.IntakeVocabulary(ctx)
}

func TestIntakePlaneConformanceFixturesMatchTheWire(t *testing.T) {
	var all []s61Fixture
	// S61 T8 (2026-11-05, GI17): the census moved 7/4/8 → 8/4/9 — the two
	// propose twins ride their plane's directories.
	// S61 T9 (2026-11-05, GI13): intake 9 → 10 — intake.abandon, the fifth
	// human door, rides the intake directory.
	for dir, want := range map[string]int{"charter": 8, "template": 4, "intake": 10} {
		all = append(all, readS61Fixtures(t, dir, want)...)
	}
	for _, f := range all {
		f := f
		t.Run(f.id(), func(t *testing.T) { runS61Fixture(t, f) })
	}
}

// TestIntakePlanIsReadOnly is GI10 at the door: replaying the plan-ok
// fixture must touch EXACTLY one seam method — intake.plan's — and the
// classification table must call it a read. A preview that wrote anything
// would show in the call census or the surface kind.
func TestIntakePlanIsReadOnly(t *testing.T) {
	var plan *s61Fixture
	all := readS61Fixtures(t, "intake", 8)
	for i, f := range all {
		if f.file == "plan-ok.json" {
			plan = &all[i]
		}
	}
	if plan == nil {
		t.Fatal("conformance/intake has no plan-ok.json — GI10's read-only promise needs its fixture")
	}
	var req struct {
		Method MethodName      `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(plan.Request, &req); err != nil || req.Method != MethodIntakePlan {
		t.Fatalf("plan-ok.json is not an intake.plan call: %v", err)
	}
	stub, tbl := newS61Table(t, plan, nil)
	if _, err := tbl.Dispatch(context.Background(), "c1", req.Method, req.Params); err != nil {
		t.Fatalf("intake.plan errored: %v", err)
	}
	if len(stub.calls) != 1 || stub.calls[MethodIntakePlan] != 1 {
		t.Errorf("intake.plan touched the seam %v — the preview is read-only (GI10) and may call nothing else", stub.calls)
	}
	if s, ok := surfaceTable()[MethodIntakePlan]; !ok || s.Kind != SurfaceRead {
		t.Errorf("intake.plan classifies as %+v — a preview that files rows would be a write wearing a read's face", s)
	}
}

func readS61Fixtures(t *testing.T, dir string, want int) []s61Fixture {
	t.Helper()
	full := filepath.Join("conformance", dir)
	entries, err := os.ReadDir(full)
	if err != nil {
		t.Fatalf("read %s: %v — the fixtures are the contract, and the contract is missing", full, err)
	}
	var out []s61Fixture
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(full, en.Name()))
		if err != nil {
			t.Fatalf("read %s/%s: %v", full, en.Name(), err)
		}
		var f s61Fixture
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

// newS61Table wires the plane's three seams over one counting stub.
func newS61Table(t *testing.T, f *s61Fixture, seamErr error) (*s61ConfStub, *Table) {
	t.Helper()
	var want expectedEnvelope
	if f != nil {
		if err := json.Unmarshal(f.Response, &want); err != nil {
			t.Fatalf("the fixture's response is not a JSON-RPC answer: %v", err)
		}
	}
	stub := &s61ConfStub{t: t, raw: want.Result, err: seamErr, calls: map[MethodName]int{}}
	tbl := newTestTable(t)
	peer := "1"
	if f != nil && f.PeerUID != "" {
		peer = f.PeerUID
	}
	tbl.ActorFor = userActorFor(peer)
	tbl.Charter = charterConfView{stub}
	tbl.Templates = templateConfView{stub}
	tbl.Intake = intakeConfView{stub}
	if !tbl.RegisterCharter() || !tbl.RegisterTemplates() || !tbl.RegisterIntake() {
		t.Fatal("the plane did not register over its seam — the fixture has nothing to speak to")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return stub, tbl
}

func runS61Fixture(t *testing.T, f s61Fixture) {
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
	// 2026-11-05 (S61 T8, GI17): the two propose twins answer over the
	// RECORDS seam (RecordsAPI.Do), not the three plane seams this runner
	// wires; their live behaviour is pinned by internal/daemon's
	// records_charter_test.go and their wire shape by the Go-type round
	// trip. The replay skips; the fixtures still count in the family census.
	if req.Method == MethodCharterPropose || req.Method == MethodIntakePropose {
		return
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
		case ErrCharterRefused.Code:
			e = ErrCharterRefused.WithWhat("%s", want.Error.Data.What)
		case ErrIntakeRefused.Code:
			e = ErrIntakeRefused.WithWhat("%s", want.Error.Data.What)
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

	stub, tbl := newS61Table(t, &f, seamErr)
	if f.PeerUID == "0" {
		// The door's own missing-identity arm: the daemon stamped a peer
		// that resolves to nobody.
		tbl.ActorFor = userActorFor("0")
	}

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
	for _, bad := range []string{"sk-live", "sk-ant-", "password", "token\":", "secretvalue", "privatekey"} {
		if strings.Contains(lower, bad) {
			t.Errorf("the answer carries what the plane's wire must never carry (%q) — a credential goes in and never comes out", bad)
		}
	}
}

// TestCharterSaveStampsPeerActor (P4): the saving person is the
// CONNECTION's, and an `actor` key in params is refused by name before
// the body is even read. The actor that does reach the seam is the
// peer's — kind user, id the connection's.
func TestCharterSaveStampsPeerActor(t *testing.T) {
	f := &s61Fixture{
		Description: "actor stamped",
		Method:      MethodCharterSave,
		Request:     json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"charter.save","params":{"rootId":"cmp-1","body":{"goal":"g"}}}`),
		Response:    json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"rootId":"cmp-1","revision":2}}`),
	}
	stub, tbl := newS61Table(t, f, nil)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodCharterSave, json.RawMessage(`{"rootId":"cmp-1","body":{"goal":"g"}}`)); err != nil {
		t.Fatalf("charter.save: %v", err)
	}
	if stub.saved.Kind != "user" || stub.saved.ID == "" || stub.saved.ID == "0" {
		t.Errorf("the seam saw actor %+v — the save must carry the connection's person (P4/GI8)", stub.saved)
	}

	// And the forgery arm: an actor key in params is a parse-time refusal.
	_, err := tbl.Dispatch(context.Background(), "c1", MethodCharterSave,
		json.RawMessage(`{"rootId":"cmp-1","body":{"goal":"g"},"actor":"someone-else"}`))
	if err == nil {
		t.Fatal("charter.save accepted an `actor` param — P4 is not a suggestion")
	}
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("an actor param answered %T, want the transport's parse refusal", err)
	}
	if !strings.Contains(err.Error(), `"actor"`) {
		t.Errorf("the refusal must name the key: %v", err)
	}
}

// TestIntakeAbandonStampsPeerRefusesForgeryAndSilence is the fifth human
// door's own gate (T9, 2026-11-05, GI13): the person comes from the peer,
// an identity key in params is a parse-time refusal, and the two required
// fields are required — the reason especially, because the rejects this
// door drives are decisions and a decision without its sentence is the
// audit gap S35 PM7.5 closed everywhere else.
func TestIntakeAbandonStampsPeerRefusesForgeryAndSilence(t *testing.T) {
	f := &s61Fixture{
		Description: "abandon stamped",
		Method:      MethodIntakeAbandon,
		Request:     json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"intake.abandon","params":{"intakeId":"int-1","reason":"stalled"}}`),
		Response:    json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"intakeId":"int-1","loopClosed":true,"intake":{"id":"int-1"}}}`),
	}
	stub, tbl := newS61Table(t, f, nil)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodIntakeAbandon,
		json.RawMessage(`{"intakeId":"int-1","reason":"stalled"}`)); err != nil {
		t.Fatalf("intake.abandon: %v", err)
	}
	if stub.saved.Kind != "user" || stub.saved.ID == "" || stub.saved.ID == "0" {
		t.Errorf("the seam saw actor %+v — the abandon must carry the connection's person (P4/GI13)", stub.saved)
	}
	for _, c := range []struct{ name, params, want string }{
		{"forgery", `{"intakeId":"int-1","reason":"stalled","actor":"someone-else"}`, `"actor"`},
		{"no id", `{"reason":"stalled"}`, "intakeId"},
		{"no reason", `{"intakeId":"int-1"}`, "reason"},
	} {
		_, err := tbl.Dispatch(context.Background(), "c1", MethodIntakeAbandon, json.RawMessage(c.params))
		if err == nil {
			t.Fatalf("intake.abandon accepted %s — %s is not optional", c.name, c.want)
		}
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("%s answered %T, want the parse/refusal half", c.name, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s refusal = %v — must name %s", c.name, err, c.want)
		}
	}
}

// TestCharterTemplateIntakeDoorsAbsentWithoutSeam is V13 for the whole
// plane: an unwired seam registers NOTHING — absent, not refusing.
func TestCharterTemplateIntakeDoorsAbsentWithoutSeam(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterCharter() || tbl.RegisterTemplates() || tbl.RegisterIntake() {
		t.Fatal("a nil seam registered doors — V13")
	}
	for _, m := range []MethodName{
		MethodCharterGet, MethodCharterList, MethodCharterRevisions, MethodCharterVocabulary, MethodCharterSave,
		MethodTemplateList, MethodTemplateGet, MethodTemplateSave,
		MethodIntakeStart, MethodIntakeList, MethodIntakeStatus, MethodIntakePlan, MethodIntakeCommit, MethodIntakeAbandon, MethodIntakeVocabulary,
	} {
		if _, err := tbl.Dispatch(context.Background(), "c1", m, nil); err == nil {
			t.Errorf("%s answered without a seam", m)
		} else if !strings.Contains(err.Error(), "method not found") && !strings.Contains(err.Error(), "not found") {
			if _, ok := tbl.methods[m]; !ok {
				continue // ErrUnknownMethod by another spelling is the absent answer
			}
			t.Errorf("%s registered without a seam", m)
		}
	}
}
