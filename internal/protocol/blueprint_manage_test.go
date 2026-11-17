package protocol

// blueprint_manage_test.go — S48 T6 (BP4, BP12): the four write doors'
// contract at the socket. The actor is the CONNECTION's person — params
// that name one are refused by key, and an unresolvable peer is refused
// before the seam is even called. The doors register conditionally over
// the daemon's write service (V13): without it, the three new verbs are
// ABSENT, not registered-and-refusing.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeBlueprintWrites struct {
	saves    []BlueprintSaveParams
	actors   []Actor
	dup      func(BlueprintDuplicateParams) (BlueprintSaveResult, error)
	del      func(BlueprintDeleteParams) (BlueprintDeleteResult, error)
	rev      func(BlueprintRevertParams) (BlueprintSaveResult, error)
	lastSave BlueprintSaveResult
	lastDel  BlueprintDeleteResult
}

func (f *fakeBlueprintWrites) Save(_ context.Context, a Actor, p BlueprintSaveParams) (BlueprintSaveResult, error) {
	f.actors = append(f.actors, a)
	f.saves = append(f.saves, p)
	return f.lastSave, nil
}
func (f *fakeBlueprintWrites) Duplicate(_ context.Context, _ Actor, p BlueprintDuplicateParams) (BlueprintSaveResult, error) {
	if f.dup == nil {
		return BlueprintSaveResult{}, errors.New("unexpected")
	}
	return f.dup(p)
}
func (f *fakeBlueprintWrites) Delete(_ context.Context, _ Actor, p BlueprintDeleteParams) (BlueprintDeleteResult, error) {
	if f.del == nil {
		return BlueprintDeleteResult{}, errors.New("unexpected")
	}
	return f.del(p)
}
func (f *fakeBlueprintWrites) Revert(_ context.Context, _ Actor, p BlueprintRevertParams) (BlueprintSaveResult, error) {
	if f.rev == nil {
		return BlueprintSaveResult{}, errors.New("unexpected")
	}
	return f.rev(p)
}

// blueprintDoorTable: an initialized table with all four doors wired.
func blueprintDoorTable(t *testing.T) (*Table, string, *fakeBlueprintWrites) {
	t.Helper()
	tbl := newTestTable(t)
	fake := &fakeBlueprintWrites{}
	tbl.SaveBlueprint = func(_ context.Context, a Actor, p BlueprintSaveParams) (BlueprintSaveResult, error) {
		return fake.Save(context.Background(), a, p)
	}
	tbl.BlueprintWrites = fake
	if !tbl.RegisterBlueprintManage() {
		t.Fatal("RegisterBlueprintManage with a wired seam must register")
	}
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	conn := "c-bp-write"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	return tbl, conn, fake
}

func codeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	// The door's own shape errors are the two JSON-RPC typed values.
	switch err.(type) {
	case ErrBadParams:
		return -32602
	case ErrUnknownMethod:
		return -32601
	}
	t.Fatalf("want a typed error, got %T: %v", err, err)
	return 0
}

// BP12: params carry no actor, no origin, no version, no timestamps. The
// lenient decode (T3's additive rule) tolerates a NEW key; the identity KEY
// BAG is refused by name. Revert excepts rev — there, rev names WHICH
// revision to restore, not who wrote it.
func TestBlueprintWriteParamsCarryNoIdentity(t *testing.T) {
	for _, params := range []string{
		`{"name":"x","content":"c","actor":{"kind":"user","id":"9"}}`,
		`{"name":"x","content":"c","origin":"system"}`,
		`{"name":"x","content":"c","version":3}`,
		`{"name":"x","content":"c","createdAt":"2026-01-01T00:00:00Z"}`,
		`{"name":"x","content":"c","rev":1}`,
		`{"from":"a","name":"x","updatedBy":"user:9"}`,
		`{"name":"x","rev":2,"actorKind":"cli"}`,
		`{"name":"x","createdBy":"root"}`,
	} {
		method := MethodBlueprintSave
		if strings.Contains(params, `"from"`) {
			method = MethodBlueprintDuplicate
		}
		if strings.Contains(params, `"actorKind"`) {
			method = MethodBlueprintRevert
		}
		if strings.Contains(params, `"createdBy"`) {
			method = MethodBlueprintDelete
		}
		tbl, conn, _ := blueprintDoorTable(t)
		_, err := tbl.Dispatch(context.Background(), conn, method, mustRaw(params))
		if codeOf(t, err) != -32602 {
			t.Errorf("%s %s: identity key accepted (err %v)", method, params, err)
		}
	}
	// Revert's rev is the request, not a provenance claim.
	tbl, conn, _ := blueprintDoorTable(t)
	tbl.BlueprintWrites.(*fakeBlueprintWrites).rev = func(p BlueprintRevertParams) (BlueprintSaveResult, error) {
		if p.Rev != 2 {
			t.Errorf("rev lost: %+v", p)
		}
		return BlueprintSaveResult{Name: p.Name, Changed: true, Revision: 3}, nil
	}
	if _, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintRevert, mustRaw(`{"name":"x","rev":2}`)); err != nil {
		t.Errorf("revert rev refused: %v", err)
	}
}

// BP12: the actor is the peer's. No identity seam, a blank id, or the uid 0
// a connection the transport never recorded — all refuse with
// ErrBlueprintNoActor BEFORE the write service sees a call.
func TestSaveActorFromPeerOnly(t *testing.T) {
	for _, tc := range []struct {
		why   string
		actor *Actor
	}{
		{"nil ActorFor", nil},
		{"blank id", &Actor{Kind: "user", ID: "   "}},
		{"uid 0 unrecorded", &Actor{Kind: "user", ID: "0"}},
	} {
		tbl := newTestTable(t)
		fake := &fakeBlueprintWrites{}
		tbl.SaveBlueprint = func(_ context.Context, a Actor, p BlueprintSaveParams) (BlueprintSaveResult, error) {
			return fake.Save(context.Background(), a, p)
		}
		tbl.BlueprintWrites = fake
		tbl.RegisterBlueprintManage()
		if tc.actor != nil {
			a := *tc.actor
			tbl.ActorFor = func(context.Context, *Session) Actor { return a }
		}
		conn := "c-" + strings.ReplaceAll(tc.why, " ", "-")
		initialized(t, tbl, conn, PROTOCOL_VERSION)
		_, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintSave, mustRaw(`{"name":"x","content":"c"}`))
		if codeOf(t, err) != ErrBlueprintNoActor.Code {
			t.Errorf("%s: got %v, want %d", tc.why, err, ErrBlueprintNoActor.Code)
		}
		if len(fake.actors) != 0 {
			t.Errorf("%s: the write service was reached without a person", tc.why)
		}
	}
	// The named peer arrives at the seam exactly as the connection said.
	tbl, conn, fake := blueprintDoorTable(t)
	if _, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintSave, mustRaw(`{"name":"x","content":"c"}`)); err != nil {
		t.Fatal(err)
	}
	if len(fake.actors) != 1 || fake.actors[0] != (Actor{Kind: "user", ID: "1000"}) {
		t.Fatalf("actor stamped wrong: %+v", fake.actors)
	}
}

// The door's own bounds: the same 1 MiB sentence the file writer refused
// with, and the ≤ 256-byte note (SEC-033 — stored, never printed).
func TestBlueprintWriteDoorBounds(t *testing.T) {
	tbl, conn, _ := blueprintDoorTable(t)
	_, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintSave, mustRaw(`{"name":"x","content":"`+strings.Repeat("a", (1<<20)+1)+`"}`))
	if codeOf(t, err) != -32602 || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Errorf("oversize content: %v", err)
	}
	_, err = tbl.Dispatch(context.Background(), conn, MethodBlueprintSave, mustRaw(`{"name":"x","content":"c","note":"`+strings.Repeat("n", 257)+`"}`))
	if codeOf(t, err) != -32602 || !strings.Contains(err.Error(), "256") {
		t.Errorf("over-long note: %v", err)
	}
	_, err = tbl.Dispatch(context.Background(), conn, MethodBlueprintDelete, mustRaw(`{"name":""}`))
	if codeOf(t, err) != -32602 {
		t.Errorf("empty delete name: %v", err)
	}
	_, err = tbl.Dispatch(context.Background(), conn, MethodBlueprintDuplicate, mustRaw(`{"from":"same","name":"same"}`))
	if codeOf(t, err) != -32602 {
		t.Errorf("self-copy: %v", err)
	}
	_, err = tbl.Dispatch(context.Background(), conn, MethodBlueprintRevert, mustRaw(`{"name":"x","rev":0}`))
	if codeOf(t, err) != -32602 {
		t.Errorf("rev 0: %v", err)
	}
}

// V13: without the daemon's write service the three new doors are ABSENT —
// registered-and-refusing is the shape V13 forbids.
func TestBlueprintWriteDoorsAbsentWithoutService(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterBlueprintManage() {
		t.Fatal("registered without a seam")
	}
	initialized(t, tbl, "c-v13", PROTOCOL_VERSION)
	for _, m := range []MethodName{MethodBlueprintDuplicate, MethodBlueprintDelete, MethodBlueprintRevert} {
		_, err := tbl.Dispatch(context.Background(), "c-v13", m, mustRaw(`{"name":"x"}`))
		if codeOf(t, err) != -32601 {
			t.Errorf("%s without the service: got %v, want method-not-found", m, err)
		}
	}
}

// The result the daemon answers is the result the socket speaks — the door
// must not reshape it.
func TestBlueprintWriteDoorPassesResultThrough(t *testing.T) {
	tbl, conn, fake := blueprintDoorTable(t)
	fake.lastSave = BlueprintSaveResult{
		Name: "x", Changed: true, Revision: 4, Origin: "user", Version: 1,
		Report:     json.RawMessage(`{"blueprint":"x","schemaVersion":1,"version":1,"validCount":6,"diagnostics":[]}`),
		References: []BlueprintRefView{{Kind: "agent", Target: "default"}},
	}
	res, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintSave, mustRaw(`{"name":"x","content":"c","note":"first"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := res.(BlueprintSaveResult)
	if got.Revision != 4 || got.References[0].Target != "default" {
		t.Fatalf("result reshaped at the door: %+v", got)
	}
	if fake.saves[0].Note != "first" {
		t.Fatalf("note lost: %+v", fake.saves[0])
	}
	fake.del = func(p BlueprintDeleteParams) (BlueprintDeleteResult, error) {
		return BlueprintDeleteResult{Deleted: true, LoopsStartedFromIt: 2, Note: "started loops keep their pinned content"}, nil
	}
	res, err = tbl.Dispatch(context.Background(), conn, MethodBlueprintDelete, mustRaw(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	d := res.(BlueprintDeleteResult)
	if !d.Deleted || d.LoopsStartedFromIt != 2 {
		t.Fatalf("delete answer: %+v", d)
	}
}

// --- S48 T7: the READ doors ---------------------------------------------------------------

type fakeBlueprintReads struct {
	usage  func(BlueprintUsageParams) (BlueprintUsageResult, error)
	hist   func(BlueprintHistoryParams) (BlueprintHistoryResult, error)
	rev    func(BlueprintRevisionParams) (BlueprintRevisionResult, error)
	diff   func(BlueprintDiffParams) (BlueprintDiffResult, error)
	lint   func(BlueprintLintParams) (BlueprintLintResult, error)
	seen   []string
	lastUs BlueprintUsageResult
}

func (f *fakeBlueprintReads) Usage(_ context.Context, p BlueprintUsageParams) (BlueprintUsageResult, error) {
	f.seen = append(f.seen, "usage")
	if f.usage != nil {
		return f.usage(p)
	}
	return f.lastUs, nil
}
func (f *fakeBlueprintReads) History(_ context.Context, p BlueprintHistoryParams) (BlueprintHistoryResult, error) {
	f.seen = append(f.seen, "history")
	if f.hist != nil {
		return f.hist(p)
	}
	return BlueprintHistoryResult{}, nil
}
func (f *fakeBlueprintReads) Revision(_ context.Context, p BlueprintRevisionParams) (BlueprintRevisionResult, error) {
	f.seen = append(f.seen, "revision")
	if f.rev != nil {
		return f.rev(p)
	}
	return BlueprintRevisionResult{}, nil
}
func (f *fakeBlueprintReads) Diff(_ context.Context, p BlueprintDiffParams) (BlueprintDiffResult, error) {
	f.seen = append(f.seen, "diff")
	if f.diff != nil {
		return f.diff(p)
	}
	return BlueprintDiffResult{}, nil
}
func (f *fakeBlueprintReads) Lint(_ context.Context, p BlueprintLintParams) (BlueprintLintResult, error) {
	f.seen = append(f.seen, "lint")
	if f.lint != nil {
		return f.lint(p)
	}
	return BlueprintLintResult{}, nil
}

// V13 at the read doors: without the service the five verbs are ABSENT —
// registered-and-refusing would promise a door that cannot answer.
func TestBlueprintReadDoorsAbsentWithoutService(t *testing.T) {
	tbl := newTestTable(t)
	conn := "c-bp-read-absent"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	for _, m := range []MethodName{MethodBlueprintUsage, MethodBlueprintHistory, MethodBlueprintRevision, MethodBlueprintDiff, MethodBlueprintLint} {
		_, err := tbl.Dispatch(context.Background(), conn, m, mustRaw(`{"name":"x","a":"current","b":"1","rev":1}`))
		if code := codeOf(t, err); code != -32601 {
			t.Fatalf("%s without a service: want -32601 (absent), got %d (%v)", m, code, err)
		}
	}
}

// The doors' own bounds, answered BEFORE any service is reached.
func TestBlueprintReadDoorBounds(t *testing.T) {
	tbl, conn, _ := blueprintDoorTable(t)
	fake := &fakeBlueprintReads{}
	tbl.BlueprintReads = fake
	tbl.RegisterBlueprintReads()
	cases := []struct {
		method MethodName
		params string
	}{
		{MethodBlueprintUsage, `{}`},
		{MethodBlueprintHistory, `{"name":"x","limit":201}`},
		{MethodBlueprintHistory, `{}`},
		{MethodBlueprintRevision, `{"name":"x","rev":0}`},
		{MethodBlueprintDiff, `{"name":"x","a":"current"}`},
		{MethodBlueprintLint, `{}`},
		{MethodBlueprintList, `{"origin":"everybody"}`},
	}
	for i, c := range cases {
		_, err := tbl.Dispatch(context.Background(), conn, c.method, mustRaw(c.params))
		if code := codeOf(t, err); code != -32602 {
			t.Fatalf("case %d (%s): want -32602, got %d (%v)", i, c.method, code, err)
		}
	}
	if len(fake.seen) != 0 {
		t.Fatalf("a bounds refusal reached the service: %v", fake.seen)
	}
}

// The door passes the service's answer through untouched, and list takes
// its filters to the seam verbatim.
func TestBlueprintReadDoorPassesResultThrough(t *testing.T) {
	tbl, conn, _ := blueprintDoorTable(t)
	fake := &fakeBlueprintReads{lastUs: BlueprintUsageResult{
		Schedules: []BlueprintUsageRef{{Kind: "schedule", ID: "sched-a", Name: "sched-a"}},
		Triggers:  []BlueprintUsageRef{}, QueueItems: []BlueprintUsageRef{},
		Agents: []string{"default"}, Actions: []string{}, Skills: []string{},
		LoopsStarted: 2,
	}}
	tbl.BlueprintReads = fake
	tbl.RegisterBlueprintReads()
	var listParams *BlueprintListParams
	tbl.ListBlueprints = func(p BlueprintListParams) (BlueprintListResult, error) {
		listParams = &p
		return BlueprintListResult{Names: []string{"one"}, Blueprints: []BlueprintSummary{{Name: "one", Origin: "user"}}}, nil
	}
	res, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintUsage, mustRaw(`{"name":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), `"loopsStarted":2`) || !strings.Contains(string(raw), `"sched-a"`) {
		t.Fatalf("the usage answer was rewritten at the door: %s", raw)
	}
	if _, err := tbl.Dispatch(context.Background(), conn, MethodBlueprintList, mustRaw(`{"origin":"user","q":"on"}`)); err != nil {
		t.Fatal(err)
	}
	if listParams == nil || listParams.Origin != "user" || listParams.Q != "on" {
		t.Fatalf("the filters did not reach the seam: %+v", listParams)
	}
}
