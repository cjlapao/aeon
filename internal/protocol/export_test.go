package protocol

// The export family's handler tests (S62 T8): the two doors as the socket sees
// them. The fake seam records every call so a refusal can be asserted to have
// happened BEFORE the engine was asked — the order is the safety (an identity
// key or an undeclared set that reached the engine would have been a write to
// the ledger with a caller-chosen audit field or a run of a set the engine
// does not declare).

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// exportFake is the recording seam: it answers what the case sets and
// remembers every fact it was asked with, so a handler can be proven to have
// refused before forwarding, and to have forwarded the params verbatim.
type exportFake struct {
	runCalled  bool
	runActor   Actor
	runParams  ExportRunParams
	runResult  ExportRunResult
	runRefusal *BackupRefusal

	setsCalled bool
	setsResult ExportSetsResult
}

func (f *exportFake) Run(_ context.Context, actor Actor, params ExportRunParams) (ExportRunResult, *BackupRefusal, error) {
	f.runCalled, f.runActor, f.runParams = true, actor, params
	return f.runResult, f.runRefusal, nil
}
func (f *exportFake) Sets(context.Context) (ExportSetsResult, error) {
	f.setsCalled = true
	return f.setsResult, nil
}

// exportDoorTable is a table with the two doors registered over the fake and
// a person stamped on the connection (the daemon's ActorFor seam, the
// backupDoorTable shape).
func exportDoorTable(t *testing.T, fake *exportFake) (*Table, string) {
	t.Helper()
	tbl := newTestTable(t)
	tbl.Export = fake
	if !tbl.RegisterExport() {
		t.Fatal("RegisterExport refused a wired seam — the two doors are wired out of existence")
	}
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	return tbl, "c-export"
}

// TestExportMethodsAbsentWhenServiceNil is the V13 side of the exact-set
// handshake: with no seam the two names are not registered at all — a client
// gets JSON-RPC's own -32601, the fact that the build did not link the door,
// never a refusal (the TestBackupMethodsAbsentWhenServiceNil shape).
func TestExportMethodsAbsentWhenServiceNil(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c-absent", PROTOCOL_VERSION)
	for _, m := range []MethodName{MethodExportRun, MethodExportSets} {
		_, err := tbl.Dispatch(context.Background(), "c-absent", m, nil)
		if err == nil {
			t.Fatalf("%s succeeded on a table whose export seam is nil — V13 is a registration, not a refusal", m)
		}
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Fatalf("%s: absent wants ErrUnknownMethod (the -32601 the transport answers), got %T: %v", m, err, err)
		}
	}
}

// TestExportRunOverTheWire: the params forward verbatim (a nil Sets stays nil,
// the default the engine resolves to every non-optional set), the actor the
// engine sees is the door's stamp — the kind a caller never named — and the
// view comes back as the seam gave it.
func TestExportRunOverTheWire(t *testing.T) {
	rows := 3
	fake := &exportFake{runResult: ExportRunResult{
		Dir:      "/home/carlo/exports/aeon-2026-11-02",
		Sets:     []ExportSetView{{Name: "agents", Description: "the agents", State: "available", Rows: &rows}},
		Files:    4,
		Recorded: true,
	}}
	tbl, conn := exportDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), conn, MethodExportRun,
		mustRaw(`{"dir":"/home/carlo/exports/aeon-2026-11-02","sets":["agents"],"includeMemory":true}`))
	if err != nil {
		t.Fatalf("export.run: %v", err)
	}
	view, ok := res.(ExportRunResult)
	if !ok {
		t.Fatalf("export.run returned %T, want ExportRunResult", res)
	}
	if !fake.runCalled {
		t.Fatal("the handler did not forward to the seam")
	}
	if fake.runActor.Kind != "user" || fake.runActor.ID != "1000" {
		t.Errorf("actor = %+v, want the door's stamp {user 1000}", fake.runActor)
	}
	if fake.runParams.Dir != "/home/carlo/exports/aeon-2026-11-02" {
		t.Errorf("dir = %q, want the param verbatim", fake.runParams.Dir)
	}
	if len(fake.runParams.Sets) != 1 || fake.runParams.Sets[0] != "agents" {
		t.Errorf("sets = %v, want [agents] verbatim", fake.runParams.Sets)
	}
	if !fake.runParams.IncludeMemory {
		t.Error("includeMemory = false, want the flag forwarded")
	}
	if view.Files != 4 || !view.Recorded || len(view.Sets) != 1 || view.Sets[0].Rows == nil || *view.Sets[0].Rows != 3 {
		t.Errorf("view = %+v, want the seam's answer back", view)
	}

	// The absent keys: no sets, no flag — the default run, nil not empty.
	res, err = tbl.Dispatch(context.Background(), conn, MethodExportRun, mustRaw(`{"dir":"/home/carlo/exports/x"}`))
	if err != nil {
		t.Fatalf("export.run with a dir only: %v", err)
	}
	if fake.runParams.Sets != nil || fake.runParams.IncludeMemory {
		t.Errorf("default run forwarded sets=%v includeMemory=%v, want (nil false) — the default is the engine's to resolve", fake.runParams.Sets, fake.runParams.IncludeMemory)
	}
	if view2, _ := res.(ExportRunResult); view2.Sets == nil {
		t.Error("a nil Sets view came back nil — the door owes the caller an empty list, not the absence of one")
	}
}

// TestExportRunRefusalRidesTheFamilyVoice: the engine's refusal is one code
// (ErrBackupRefused, -31863 — the reuse S62 T8 records in errors.go), the
// closed-set kind in data.kind, the person's sentence in data.what naming the
// verb and the engine's own sentence.
func TestExportRunRefusalRidesTheFamilyVoice(t *testing.T) {
	fake := &exportFake{runRefusal: &BackupRefusal{Kind: "dir-nonempty", Detail: "the dir /x is not empty — an export refuses to merge into files that were there first (no accidental worktree)"}}
	tbl, conn := exportDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)

	e := backupDispatchError(t, tbl, conn, MethodExportRun, `{"dir":"/x"}`)
	if e.Code != -31863 {
		t.Errorf("code = %d, want -31863 (the family's refusal voice)", e.Code)
	}
	if e.Code != ErrBackupRefused.Code {
		t.Errorf("code = %d, want the ErrBackupRefused definition's code", e.Code)
	}
	if e.Data.Kind != "dir-nonempty" {
		t.Errorf("data.kind = %q, want the closed-set kind for a client to branch on", e.Data.Kind)
	}
	if !strings.HasPrefix(e.Data.What, "export run refused: ") {
		t.Errorf("data.what = %q, want the verb named in front of the engine's sentence", e.Data.What)
	}
	if !strings.Contains(e.Data.What, "is not empty") {
		t.Errorf("data.what = %q, want the engine's own sentence carried", e.Data.What)
	}
	if e.Data.Fatal {
		t.Error("data.fatal = true, want false — a refusal is a state found, not a death")
	}
}

// TestExportRunRefusesIdentityKeysOnEveryDoor: the actor is stamped by the
// daemon from the connection and never decoded from params (BK13) — the
// backup family's rule, reused. All three key names, on both doors, and the
// refusal happens before the seam is asked.
func TestExportRunRefusesIdentityKeysOnEveryDoor(t *testing.T) {
	doors := []MethodName{MethodExportRun, MethodExportSets}
	keys := []string{"actor", "actorKind", "who"}
	for _, m := range doors {
		for _, k := range keys {
			fake := &exportFake{}
			tbl, conn := exportDoorTable(t, fake)
			initialized(t, tbl, conn, PROTOCOL_VERSION)
			inner := backupBadParams(t, tbl, conn, m, `{"`+k+`":{"kind":"user","id":"0"}}`)
			if !strings.Contains(inner.Error(), k) {
				t.Errorf("%s %q: the sentence does not name the key it refused: %s", m, k, inner)
			}
			if fake.runCalled || fake.setsCalled {
				t.Errorf("%s %q reached the engine — the identity key was not refused at the door", m, k)
			}
		}
	}
}

// TestExportRunRefusesAnUndeclaredSet: a Sets entry the engine does not
// declare is a SHAPE refusal (-32602) at the door, before the actor is even
// asked and before the seam — naming the set and the closed set it is not in.
// "memory" is a declared set, so it passes the wire and is the ENGINE's
// refusal (the flag's door); this test must not conflate the two.
func TestExportRunRefusesAnUndeclaredSet(t *testing.T) {
	fake := &exportFake{}
	tbl, conn := exportDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	inner := backupBadParams(t, tbl, conn, MethodExportRun, `{"dir":"/x","sets":["nope"]}`)
	if !strings.Contains(inner.Error(), `"nope"`) {
		t.Errorf("the sentence does not name the set it refused: %s", inner)
	}
	if !strings.Contains(inner.Error(), "declared sets") {
		t.Errorf("the sentence does not say what the sets must name: %s", inner)
	}
	if fake.runCalled {
		t.Error("an undeclared set reached the engine — the wire must refuse the spelling first")
	}
}

// TestExportRunRefusesWithoutIdentity: the actor is the door's, and a
// connection the daemon cannot stamp (no ActorFor seam, or an empty id) gets a
// shape refusal (-32602, the backupActor shape) that says what the door is —
// before the engine is asked.
func TestExportRunRefusesWithoutIdentity(t *testing.T) {
	// No identity seam at all.
	fake := &exportFake{}
	tbl := newTestTable(t)
	tbl.Export = fake
	tbl.RegisterExport()
	initialized(t, tbl, "c-no-stamp", PROTOCOL_VERSION)
	inner := backupBadParams(t, tbl, "c-no-stamp", MethodExportRun, `{"dir":"/x"}`)
	if !strings.Contains(inner.Error(), "person's door") {
		t.Errorf("the sentence does not say what the door is: %s", inner)
	}
	if fake.runCalled {
		t.Error("an unstamped run reached the engine — the audit identity is the door's to guarantee")
	}

	// A stamp with an empty id: refused just the same.
	fake2 := &exportFake{}
	tbl2 := newTestTable(t)
	tbl2.Export = fake2
	tbl2.RegisterExport()
	tbl2.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "  "} }
	initialized(t, tbl2, "c-empty-id", PROTOCOL_VERSION)
	inner2 := backupBadParams(t, tbl2, "c-empty-id", MethodExportRun, `{"dir":"/x"}`)
	if !strings.Contains(inner2.Error(), "person's door") {
		t.Errorf("the sentence does not say what the door is: %s", inner2)
	}
	if fake2.runCalled {
		t.Error("an empty-id run reached the engine")
	}
}

// TestExportSetsIsOpen: the sets door answers a connection that never
// handshook — RequiresInitialize is false, and the contrast (run on the same
// uninitialised connection is refused by the session gate) says the
// difference is the method's declaration, not the table's mood. It takes no
// params and forwards to the seam.
func TestExportSetsIsOpen(t *testing.T) {
	fake := &exportFake{setsResult: ExportSetsResult{Sets: []ExportSetView{
		{Name: "memory", Description: "the learning rows", Optional: true, State: "available (opt-in)"},
	}}}
	tbl := newTestTable(t)
	tbl.Export = fake
	if !tbl.RegisterExport() {
		t.Fatal("RegisterExport refused a wired seam")
	}

	// No initialize on this connection at all: sets still answers.
	res, err := tbl.Dispatch(context.Background(), "c-open", MethodExportSets, nil)
	if err != nil {
		t.Fatalf("export.sets on an uninitialised connection: %v — the door is open, that is the point", err)
	}
	view, ok := res.(ExportSetsResult)
	if !ok {
		t.Fatalf("export.sets returned %T, want ExportSetsResult", res)
	}
	if !fake.setsCalled {
		t.Fatal("the handler did not forward to the seam")
	}
	if len(view.Sets) != 1 || view.Sets[0].State != "available (opt-in)" || view.Sets[0].Rows != nil {
		t.Errorf("view = %+v, want the seam's answer, rows null", view)
	}

	// The contrast: run on the same connection is the session gate's refusal.
	if _, err := tbl.Dispatch(context.Background(), "c-open", MethodExportRun, mustRaw(`{"dir":"/x"}`)); err == nil {
		t.Error("export.run on an uninitialised connection succeeded — the write door is not open")
	}
}
