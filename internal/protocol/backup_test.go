package protocol

// The backup family's handler tests (S62 T6): the five doors as the socket
// sees them. The fake seam records every call so a refusal can be asserted to
// have happened BEFORE the engine was asked — the order is the safety (a
// reason forgery or an identity key that reached the engine would have been a
// write to the ledger with a caller-chosen audit field).

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// backupFake is the recording seam: it answers what the case sets and
// remembers every fact it was asked with, so a handler can be proven to have
// refused before forwarding, and to have forwarded the params verbatim.
type backupFake struct {
	runCalled    bool
	runActor     Actor
	runVerify    *bool
	listCalled   bool
	listLimit    int
	listInclude  bool
	verifyCalled bool
	verifyActor  Actor
	verifyFile   string
	verifyDrill  bool
	statusCalled bool
	pruneCalled  bool
	pruneActor   Actor
	pruneDryRun  bool

	runResult     BackupRunResult
	runRefusal    *BackupRefusal
	verifyResult  BackupVerifyResult
	verifyRefusal *BackupRefusal
	pruneResult   BackupPruneView
	pruneRefusal  *BackupRefusal
	listResult    BackupListResult
	statusResult  BackupStatusView
}

func (f *backupFake) Run(_ context.Context, actor Actor, verify *bool) (BackupRunResult, *BackupRefusal, error) {
	f.runCalled, f.runActor, f.runVerify = true, actor, verify
	return f.runResult, f.runRefusal, nil
}
func (f *backupFake) List(_ context.Context, limit int, includePreMigration bool) (BackupListResult, error) {
	f.listCalled, f.listLimit, f.listInclude = true, limit, includePreMigration
	return f.listResult, nil
}
func (f *backupFake) Verify(_ context.Context, actor Actor, file string, drill bool) (BackupVerifyResult, *BackupRefusal, error) {
	f.verifyCalled, f.verifyActor, f.verifyFile, f.verifyDrill = true, actor, file, drill
	return f.verifyResult, f.verifyRefusal, nil
}
func (f *backupFake) Status(context.Context) (BackupStatusView, error) {
	f.statusCalled = true
	return f.statusResult, nil
}
func (f *backupFake) Prune(_ context.Context, actor Actor, dryRun bool) (BackupPruneView, *BackupRefusal, error) {
	f.pruneCalled, f.pruneActor, f.pruneDryRun = true, actor, dryRun
	return f.pruneResult, f.pruneRefusal, nil
}

// backupDoorTable is a table with the five doors registered over the fake and
// a person stamped on the connection (the daemon's ActorFor seam, the shape
// mcp_admin_test.go carries).
func backupDoorTable(t *testing.T, fake *backupFake) (*Table, string) {
	t.Helper()
	tbl := newTestTable(t)
	tbl.Backup = fake
	if !tbl.RegisterBackup() {
		t.Fatal("RegisterBackup refused a wired seam — the five doors are wired out of existence")
	}
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	return tbl, "c-backup"
}

// backupBadParams extracts the shape-refusal sentinel from a dispatch:
// -32602 is JSON-RPC's own (a params shape fault, not an Aeon code), so the
// door returns ErrBadParams and the transport answers -32602; the door's
// sentence rides inside .Err (the mcp_admin_test.go shape, carried).
func backupBadParams(t *testing.T, tbl *Table, conn string, method MethodName, params string) error {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), conn, method, mustRaw(params))
	if err == nil {
		t.Fatalf("%s: wanted a shape refusal, got a success: %v", method, res)
	}
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("%s: want ErrBadParams (the -32602 shape), got %T: %v", method, err, err)
	}
	return bad.Err
}

func backupDispatchError(t *testing.T, tbl *Table, conn string, method MethodName, params string) *Error {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), conn, method, mustRaw(params))
	if err == nil {
		t.Fatalf("%s: wanted a refusal, got a success: %v", method, res)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("%s: want *protocol.Error, got %T: %v", method, err, err)
	}
	return e
}

func mustRaw(s string) (raw []byte) { return []byte(s) }

// TestBackupMethodsAbsentWhenServiceNil is the V13 side of the exact-set
// handshake: with no seam the five names are not registered at all — a client
// gets JSON-RPC's own -32601, the fact that the build did not link the door,
// never a refusal (an absent door is a fact about the build while a refusal is
// a fact about a decision; the two must never be confused).
func TestBackupMethodsAbsentWhenServiceNil(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c-absent", PROTOCOL_VERSION)
	for _, m := range []MethodName{MethodBackupRun, MethodBackupList, MethodBackupVerify, MethodBackupStatus, MethodBackupPrune} {
		_, err := tbl.Dispatch(context.Background(), "c-absent", m, nil)
		if err == nil {
			t.Fatalf("%s succeeded on a table whose backup seam is nil — V13 is a registration, not a refusal", m)
		}
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Fatalf("%s: absent wants ErrUnknownMethod (the -32601 the transport answers), got %T: %v", m, err, err)
		}
	}
}

// TestBackupRunForwardsTheTriStateAndTheStampedActor: the verify param is a
// tri-state (nil = the setting decides), forwarded pointer intact, and the
// actor the engine sees is the door's stamp — the kind a caller never named.
func TestBackupRunForwardsTheTriStateAndTheStampedActor(t *testing.T) {
	fake := &backupFake{}
	tbl, conn := backupDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), conn, MethodBackupRun, mustRaw(`{"reason":"manual","verify":true}`))
	if err != nil {
		t.Fatalf("backup.run: %v", err)
	}
	view, ok := res.(BackupRunResult)
	if !ok {
		t.Fatalf("backup.run returned %T, want BackupRunResult", res)
	}
	if !fake.runCalled {
		t.Fatal("the handler did not forward to the seam")
	}
	if fake.runActor.Kind != "user" || fake.runActor.ID != "1000" {
		t.Errorf("actor = %+v, want the door's stamp {user 1000}", fake.runActor)
	}
	if fake.runVerify == nil || *fake.runVerify != true {
		t.Errorf("verify = %v, want the true override forwarded as a pointer", fake.runVerify)
	}
	_ = view

	// The absent key: nil, not false — the setting decides.
	fake.runVerify = nil
	if _, err := tbl.Dispatch(context.Background(), conn, MethodBackupRun, mustRaw(`{}`)); err != nil {
		t.Fatalf("backup.run with no params: %v", err)
	}
	if fake.runVerify != nil {
		t.Errorf("verify = %v with no key, want nil (the setting decides)", *fake.runVerify)
	}
}

// TestBackupRunReasonForgeryIsRefusedBeforeTheSeam: from this door the reason
// is manual by definition; a caller naming the daemon's own (scheduled) or the
// store's own (pre-migration) would be forging the ledger's audit field, and
// the refusal is a SHAPE refusal (-32602) that happens before the engine is
// even asked — the fake must show it was never called.
func TestBackupRunReasonForgeryIsRefusedBeforeTheSeam(t *testing.T) {
	for _, forged := range []string{"scheduled", "pre-migration", "post-restore-check", "bogus"} {
		fake := &backupFake{}
		tbl, conn := backupDoorTable(t, fake)
		initialized(t, tbl, conn, PROTOCOL_VERSION)
		inner := backupBadParams(t, tbl, conn, MethodBackupRun, `{"reason":"`+forged+`"}`)
		if !strings.Contains(inner.Error(), "manual") {
			t.Errorf("reason %q: the sentence does not say what the door is (manual): %s", forged, inner)
		}
		if fake.runCalled {
			t.Errorf("reason %q reached the engine — the forgery was not caught at the door", forged)
		}
	}
}

// TestBackupIdentityKeysAreRefusedOnEveryDoor: the actor is stamped by the
// daemon from the connection and never decoded from params (BK13) — a param
// that names it would let a caller forge the audit identity of a write to
// their own data. All three key names, on every door that takes a bag, and
// the refusal happens before the seam is asked.
func TestBackupIdentityKeysAreRefusedOnEveryDoor(t *testing.T) {
	doors := []MethodName{MethodBackupRun, MethodBackupList, MethodBackupVerify, MethodBackupStatus, MethodBackupPrune}
	keys := []string{"actor", "actorKind", "who"}
	for _, m := range doors {
		for _, k := range keys {
			fake := &backupFake{}
			tbl, conn := backupDoorTable(t, fake)
			initialized(t, tbl, conn, PROTOCOL_VERSION)
			inner := backupBadParams(t, tbl, conn, m, `{"`+k+`":{"kind":"user","id":"0"}}`)
			if !strings.Contains(inner.Error(), k) {
				t.Errorf("%s %q: the sentence does not name the key it refused: %s", m, k, inner)
			}
			if fake.runCalled || fake.listCalled || fake.verifyCalled || fake.statusCalled || fake.pruneCalled {
				t.Errorf("%s %q reached the engine — the identity key was not refused at the door", m, k)
			}
		}
	}
}

// TestBackupVerifyFileParamIsABasenameOnly: the file param is a BASENAME the
// engine resolves inside the configured dirs — a separator in it would let a
// caller ask about a file the daemon never meant to name (BK12's wire-side
// twin), and an empty name names nothing. Both are shape refusals before the
// engine; the full path belongs to the CLI's direct door.
func TestBackupVerifyFileParamIsABasenameOnly(t *testing.T) {
	// The backslash entry is written already JSON-escaped (the literal \\
	// decodes to one backslash, which is the separator the check must meet —
	// a bare one in the JSON source would fail at the decoder, a different
	// refusal this test does not want to conflate).
	bad := []string{``, `../state.db`, `a/b.aeonbak`, `pre\\migration\\x.aeonbak`}
	for _, file := range bad {
		fake := &backupFake{}
		tbl, conn := backupDoorTable(t, fake)
		initialized(t, tbl, conn, PROTOCOL_VERSION)
		inner := backupBadParams(t, tbl, conn, MethodBackupVerify, `{"file":"`+file+`"}`)
		if file != "" && !strings.Contains(inner.Error(), "basename") {
			t.Errorf("file %q: the sentence does not name the shape it refused: %s", file, inner)
		}
		if fake.verifyCalled {
			t.Errorf("file %q reached the engine", file)
		}
	}
	// The well-shaped name forwards, with drill forwarded too.
	fake := &backupFake{}
	tbl, conn := backupDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	if _, err := tbl.Dispatch(context.Background(), conn, MethodBackupVerify, mustRaw(`{"file":"aeon-state-20261102T115930Z-v76.aeonbak","drill":true}`)); err != nil {
		t.Fatalf("well-shaped verify: %v", err)
	}
	if !fake.verifyCalled || fake.verifyFile != "aeon-state-20261102T115930Z-v76.aeonbak" || !fake.verifyDrill {
		t.Errorf("verify forwarded (called=%v file=%q drill=%v), want (true the-name true)",
			fake.verifyCalled, fake.verifyFile, fake.verifyDrill)
	}
	if fake.verifyActor.ID != "1000" {
		t.Errorf("verify actor = %+v, want the door's stamp", fake.verifyActor)
	}
}

// TestBackupRefusalsRideTheFamilyCodeWithTheKind: the engine's refusal is
// data on the wire (DB19) — one code for every refusal the family meets
// (ErrBackupRefused), the closed-set kind in Data.Kind so a client can branch
// on the state found, the person's sentence in Data.What, and the three-part
// data kept whole (what/means/todo, the error contract).
func TestBackupRefusalsRideTheFamilyCodeWithTheKind(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		fake := &backupFake{runRefusal: &BackupRefusal{Kind: "lock-held", Detail: "the backup lock is held — pid 48213"}}
		tbl, conn := backupDoorTable(t, fake)
		initialized(t, tbl, conn, PROTOCOL_VERSION)
		e := backupDispatchError(t, tbl, conn, MethodBackupRun, `{}`)
		if e.Code != ErrBackupRefused.Code {
			t.Fatalf("run refusal code = %d, want %d", e.Code, ErrBackupRefused.Code)
		}
		if e.Data.Kind != "lock-held" {
			t.Errorf("data.kind = %q, want lock-held", e.Data.Kind)
		}
		if !strings.Contains(e.Data.What, "the backup lock is held") {
			t.Errorf("data.what does not carry the engine's sentence: %s", e.Data.What)
		}
		if e.Data.Means == "" || e.Data.Todo == "" {
			t.Errorf("the refusal lost a part of the error contract: %+v", e.Data)
		}
	})
	t.Run("verify", func(t *testing.T) {
		fake := &backupFake{verifyRefusal: &BackupRefusal{Kind: "unknown", Detail: "no backup file named \"x\" — the configured backup dirs hold no such file"}}
		tbl, conn := backupDoorTable(t, fake)
		initialized(t, tbl, conn, PROTOCOL_VERSION)
		e := backupDispatchError(t, tbl, conn, MethodBackupVerify, `{"file":"x"}`)
		if e.Code != ErrBackupRefused.Code || e.Data.Kind != "unknown" {
			t.Fatalf("verify refusal = code %d kind %q, want %d / unknown", e.Code, e.Data.Kind, ErrBackupRefused.Code)
		}
	})
	t.Run("prune", func(t *testing.T) {
		fake := &backupFake{pruneRefusal: &BackupRefusal{Kind: "io", Detail: "the fence will not delete"}}
		tbl, conn := backupDoorTable(t, fake)
		initialized(t, tbl, conn, PROTOCOL_VERSION)
		e := backupDispatchError(t, tbl, conn, MethodBackupPrune, `{}`)
		if e.Code != ErrBackupRefused.Code || e.Data.Kind != "io" {
			t.Fatalf("prune refusal = code %d kind %q, want %d / io", e.Code, e.Data.Kind, ErrBackupRefused.Code)
		}
	})
}

// TestBackupWriteDoorsRefuseWithoutAnIdentity: the three write doors (run,
// verify, prune) are a person's doors — with no identity seam on the
// connection there is no person, and the write is refused (the seedActor
// shape: an empty id is refused as well). The two READS (list, status) are
// not identity doors and answer without one.
func TestBackupWriteDoorsRefuseWithoutAnIdentity(t *testing.T) {
	for _, tc := range []struct {
		method MethodName
		params string
		write  bool
	}{
		{MethodBackupRun, `{"reason":"manual"}`, true},
		{MethodBackupVerify, `{"file":"aeon-state-20261102T115930Z-v76.aeonbak"}`, true},
		{MethodBackupPrune, `{}`, true},
		{MethodBackupList, `{}`, false},
		{MethodBackupStatus, `{}`, false},
	} {
		fake := &backupFake{}
		tbl := newTestTable(t)
		tbl.Backup = fake
		if !tbl.RegisterBackup() {
			t.Fatal("RegisterBackup refused a wired seam")
		}
		// ActorFor left nil: the honest absence, a connection with no persons.
		initialized(t, tbl, "c-anon", PROTOCOL_VERSION)
		if tc.write {
			inner := backupBadParams(t, tbl, "c-anon", tc.method, tc.params)
			if !strings.Contains(inner.Error(), "no identity is available") {
				t.Errorf("%s: the sentence does not name the absence: %s", tc.method, inner)
			}
			if fake.runCalled || fake.verifyCalled || fake.pruneCalled {
				t.Errorf("%s reached the engine with no identity stamped", tc.method)
			}
		} else {
			if _, err := tbl.Dispatch(context.Background(), "c-anon", tc.method, mustRaw(tc.params)); err != nil {
				t.Errorf("%s (a read) refused a connection with no identity: %v", tc.method, err)
			}
		}
	}

	// The empty id is refused as well (the door stamped none).
	fake := &backupFake{}
	tbl, _ := backupDoorTable(t, fake)
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "  "} }
	initialized(t, tbl, "c-empty", PROTOCOL_VERSION)
	inner := backupBadParams(t, tbl, "c-empty", MethodBackupRun, `{}`)
	if !strings.Contains(inner.Error(), "no identity is available") {
		t.Errorf("empty-id run: the sentence does not name the absence: %s", inner)
	}
	if fake.runCalled {
		t.Error("an empty-id actor reached the engine")
	}
}

// TestBackupEmptyAnswersRenderEmpty: a nil slice from the seam renders as an
// empty array, not null — a list of nothing is a fact (the dirs hold no
// backups), and a plan of nothing is a fact too (nothing is retained-
// removable). The handler owns the rendering; a client must not have to.
func TestBackupEmptyAnswersRenderEmpty(t *testing.T) {
	fake := &backupFake{listResult: BackupListResult{Backups: nil, Dirs: []string{"/x/backups"}}}
	tbl, conn := backupDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	res, err := tbl.Dispatch(context.Background(), conn, MethodBackupList, mustRaw(`{}`))
	if err != nil {
		t.Fatalf("backup.list: %v", err)
	}
	list := res.(BackupListResult)
	if list.Backups == nil || len(list.Backups) != 0 {
		t.Errorf("backups = %v, want a present empty slice (renders as [], not null)", list.Backups)
	}

	fake.pruneResult = BackupPruneView{Plan: nil}
	res, err = tbl.Dispatch(context.Background(), conn, MethodBackupPrune, mustRaw(`{"dryRun":true}`))
	if err != nil {
		t.Fatalf("backup.prune: %v", err)
	}
	prune := res.(BackupPruneView)
	if prune.Plan == nil || len(prune.Plan) != 0 {
		t.Errorf("plan = %v, want a present empty slice (a dry run plans something — or nothing — and says so)", prune.Plan)
	}
	if prune.Removed != nil {
		t.Errorf("removed = %v on a dry run, want absent (the plan is not an act)", prune.Removed)
	}
}

// TestBackupListLimitIsACapNotAnOffset: a negative limit is a shape refusal —
// a cap on how many to show cannot be a count to skip, and the sentence says
// so rather than echoing a guess.
func TestBackupListLimitIsACapNotAnOffset(t *testing.T) {
	fake := &backupFake{}
	tbl, conn := backupDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	inner := backupBadParams(t, tbl, conn, MethodBackupList, `{"limit":-3}`)
	if !strings.Contains(inner.Error(), "cap") {
		t.Errorf("negative limit: the sentence does not name what a limit is (a cap): %s", inner)
	}
	if fake.listCalled {
		t.Error("a negative limit reached the engine")
	}
	// A positive cap forwards verbatim.
	if _, err := tbl.Dispatch(context.Background(), conn, MethodBackupList, mustRaw(`{"limit":5,"includePreMigration":true}`)); err != nil {
		t.Fatalf("limit 5: %v", err)
	}
	if !fake.listCalled || fake.listLimit != 5 || !fake.listInclude {
		t.Errorf("list forwarded (called=%v limit=%d include=%v), want (true 5 true)",
			fake.listCalled, fake.listLimit, fake.listInclude)
	}
}

// TestBackupStatusTakesNoParams: the machine is a fact of the daemon, not of
// the caller — the door answers the machine's state and takes no knob.
func TestBackupStatusTakesNoParams(t *testing.T) {
	fake := &backupFake{statusResult: BackupStatusView{State: "fresh", Enabled: true}}
	tbl, conn := backupDoorTable(t, fake)
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	res, err := tbl.Dispatch(context.Background(), conn, MethodBackupStatus, mustRaw(`{}`))
	if err != nil {
		t.Fatalf("backup.status: %v", err)
	}
	view := res.(BackupStatusView)
	if view.State != "fresh" || !view.Enabled {
		t.Errorf("status = %+v, want the seam's answer", view)
	}
	if !fake.statusCalled {
		t.Error("the handler did not forward to the seam")
	}
}
