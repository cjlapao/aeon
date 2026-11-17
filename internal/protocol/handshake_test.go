package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// newTestTable builds a complete protocol server with no transport and no
// globals — which is what T11 buys: the method table is testable on its own.
func newTestTable(t *testing.T) *Table {
	t.Helper()
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.DaemonVersion = "9.9.9-test"
	tbl.BuildTime = "2026-10-15T00:00:00Z"
	tbl.GitCommit = "deadbee"
	return tbl
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// testLoopControlService is the non-nil seam the exact-set test needs to see
// the seven loop-control methods REGISTERED (the settingsStore precedent):
// the door (RegisterLoopControl) only inspects whether the seam is nil — it
// never calls these methods — so a zero-valued seam is enough.
type testLoopControlService struct{}

func (testLoopControlService) Stop(context.Context, Actor, string, string) (LoopStopResult, *ControlRefusal, error) {
	return LoopStopResult{}, nil, nil
}
func (testLoopControlService) Resume(context.Context, Actor, string, string) (LoopResumeResult, *ControlRefusal, error) {
	return LoopResumeResult{}, nil, nil
}
func (testLoopControlService) Cancel(context.Context, Actor, string, string) (LoopCancelResult, *ControlRefusal, error) {
	return LoopCancelResult{}, nil, nil
}
func (testLoopControlService) Control(context.Context, string) (LoopControlResult, error) {
	return LoopControlResult{}, nil
}
func (testLoopControlService) RequestStop(context.Context, string, string) (RequestStopResult, *ControlRefusal, error) {
	return RequestStopResult{}, nil, nil
}
func (testLoopControlService) Requests(context.Context, string, string) ([]ControlRequestView, error) {
	return nil, nil
}
func (testLoopControlService) Dismiss(context.Context, Actor, int64) (ControlRequestView, *ControlRefusal, error) {
	return ControlRequestView{}, nil, nil
}

// testBudgetService is the non-nil seam the exact-set test needs to see the
// eight budget methods REGISTERED (the same zero-valued-seam argument as
// testLoopControlService: RegisterBudget inspects only for nil).
type testBudgetService struct{}

func (testBudgetService) List(context.Context, string, string, string, *bool) ([]BudgetView, error) {
	return nil, nil
}
func (testBudgetService) Show(context.Context, string) (BudgetShowResult, error) {
	return BudgetShowResult{}, nil
}
func (testBudgetService) Effective(context.Context, string, string, string) (BudgetEffectiveResult, error) {
	return BudgetEffectiveResult{}, nil
}
func (testBudgetService) SpendReport(context.Context, SpendReportParams) (SpendReportResult, error) {
	return SpendReportResult{}, nil
}
func (testBudgetService) Set(context.Context, Actor, BudgetSetParams) (BudgetSetResult, *BudgetRefusal, error) {
	return BudgetSetResult{}, nil, nil
}
func (testBudgetService) RemoveBudget(context.Context, Actor, string, string) (BudgetRemoveResult, *BudgetRefusal, error) {
	return BudgetRemoveResult{}, nil, nil
}
func (testBudgetService) Raise(context.Context, Actor, LoopRaiseParams) (LoopRaiseResult, *BudgetRefusal, error) {
	return LoopRaiseResult{}, nil, nil
}
func (testBudgetService) Vocabulary() BudgetVocabularyResult {
	return BudgetVocabularyResult{}
}

// testWatchdogService is the non-nil seam the exact-set test needs to see the
// six watchdog methods REGISTERED (the same zero-valued-seam argument as
// testBudgetService: RegisterWatchdog inspects only for nil).
type testWatchdogService struct{}

func (testWatchdogService) List(context.Context, string, string, *bool) ([]WatchdogRuleView, error) {
	return nil, nil
}
func (testWatchdogService) Show(context.Context, string) (WatchdogShowResult, error) {
	return WatchdogShowResult{}, nil
}
func (testWatchdogService) Set(context.Context, Actor, WatchdogSetParams) (WatchdogSetResult, *WatchdogRefusal, error) {
	return WatchdogSetResult{}, nil, nil
}
func (testWatchdogService) SetEnabled(context.Context, Actor, string, bool, bool, string) (WatchdogSetEnabledResult, *WatchdogRefusal, error) {
	return WatchdogSetEnabledResult{}, nil, nil
}
func (testWatchdogService) RemoveRule(context.Context, Actor, string, string) (WatchdogRemoveResult, *WatchdogRefusal, error) {
	return WatchdogRemoveResult{}, nil, nil
}
func (testWatchdogService) Vocabulary(context.Context) (WatchdogVocabularyResult, error) {
	return WatchdogVocabularyResult{}, nil
}

// testBackupService is the non-nil seam the exact-set test needs to see the
// five backup methods REGISTERED (the same zero-valued-seam argument as
// testWatchdogService: RegisterBackup inspects only for nil).
type testBackupService struct{}

func (testBackupService) Run(context.Context, Actor, *bool) (BackupRunResult, *BackupRefusal, error) {
	return BackupRunResult{}, nil, nil
}
func (testBackupService) List(context.Context, int, bool) (BackupListResult, error) {
	return BackupListResult{}, nil
}
func (testBackupService) Verify(context.Context, Actor, string, bool) (BackupVerifyResult, *BackupRefusal, error) {
	return BackupVerifyResult{}, nil, nil
}
func (testBackupService) Status(context.Context) (BackupStatusView, error) {
	return BackupStatusView{}, nil
}
func (testBackupService) Prune(context.Context, Actor, bool) (BackupPruneView, *BackupRefusal, error) {
	return BackupPruneView{}, nil, nil
}

// testExportService is the non-nil seam the exact-set and parity tests need
// to see the two export methods REGISTERED (S62 T8; the same zero-valued-seam
// argument as testBackupService: RegisterExport inspects only for nil).
type testExportService struct{}

func (testExportService) Run(context.Context, Actor, ExportRunParams) (ExportRunResult, *BackupRefusal, error) {
	return ExportRunResult{}, nil, nil
}
func (testExportService) Sets(context.Context) (ExportSetsResult, error) {
	return ExportSetsResult{}, nil
}

// initialized runs a successful handshake and returns the connection id.
func initialized(t *testing.T, tbl *Table, connID string, want int) *Session {
	t.Helper()
	res, err := tbl.Dispatch(context.Background(), connID, MethodInitialize,
		mustJSON(t, InitializeParams{ClientName: "test", ClientVersion: "0", ProtocolVersion: want}))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if _, ok := res.(InitializeResult); !ok {
		t.Fatalf("initialize returned %T", res)
	}
	sess, ok := tbl.Sessions.Get(connID)
	if !ok {
		t.Fatal("initialize did not record a session")
	}
	return sess
}

// T7 · D1 — every method except `initialize` is refused before it.
func TestAnyMethodBeforeInitializeIsRefused(t *testing.T) {
	for _, m := range []MethodName{MethodPing, MethodVersion, MethodSubscribe, MethodUnsubscribe} {
		t.Run(string(m), func(t *testing.T) {
			tbl := newTestTable(t)
			_, err := tbl.Dispatch(context.Background(), "c1", m, nil)
			if err == nil {
				t.Fatalf("%s succeeded on an uninitialised connection", m)
			}
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("want *protocol.Error, got %T: %v", err, err)
			}
			if e.Code != -31001 {
				t.Fatalf("code = %d, want -31001", e.Code)
			}
			if !e.Data.Fatal {
				t.Error("must be fatal: without negotiation the connection is unusable")
			}
			// Naming the method is the difference between a client author
			// knowing which call was too early and guessing.
			if !strings.Contains(e.Data.What, string(m)) {
				t.Errorf("the refusal does not name the method: %q", e.Data.What)
			}
		})
	}
}

// The complement: `initialize` itself is reachable before initialising, which
// is the one method for which that is true.
func TestInitializeIsReachableBeforeInitializing(t *testing.T) {
	tbl := newTestTable(t)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize,
		mustJSON(t, InitializeParams{ProtocolVersion: PROTOCOL_VERSION})); err != nil {
		t.Fatalf("initialize was refused on a fresh connection: %v", err)
	}
}

func TestInitializeAnswersWithTheVersionItWillSpeak(t *testing.T) {
	tbl := newTestTable(t)
	res, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize,
		mustJSON(t, InitializeParams{ClientName: "aeon-cli", ClientVersion: "0.1.0", ProtocolVersion: PROTOCOL_VERSION}))
	if err != nil {
		t.Fatal(err)
	}
	r := res.(InitializeResult)
	if r.ProtocolVersion != PROTOCOL_VERSION {
		t.Errorf("protocolVersion = %d, want %d", r.ProtocolVersion, PROTOCOL_VERSION)
	}
	if r.DaemonVersion != "9.9.9-test" {
		t.Errorf("daemonVersion = %q — it must come from the daemon, not a constant", r.DaemonVersion)
	}
	if len(r.Capabilities) == 0 {
		t.Error("no capabilities advertised")
	}
}

func TestInitializeRefusesAHigherVersionNamingBoth(t *testing.T) {
	tbl := newTestTable(t)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize,
		mustJSON(t, InitializeParams{ProtocolVersion: 99}))
	if err == nil {
		t.Fatal("a client needing version 99 was not refused")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31002 {
		t.Fatalf("want -31002, got %v", err)
	}
	for _, want := range []string{"99", fmt.Sprint(PROTOCOL_VERSION)} {
		if !strings.Contains(e.Data.What, want) {
			t.Errorf("refusal does not name %q: %q", want, e.Data.What)
		}
	}
	// And the connection must NOT be recorded as initialised.
	if _, ok := tbl.Sessions.Get("c1"); ok {
		t.Error("a refused handshake left a session behind")
	}
}

// T2's I3 in practice: the negotiated version is recorded on the connection and
// reported by `version` — not re-derived from the constant.
func TestTheNegotiatedVersionIsRecordedAndReportedByVersion(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := res.(VersionResult)
	if v.ProtocolVersion != PROTOCOL_VERSION {
		t.Errorf("version reported protocol %d, want the negotiated %d", v.ProtocolVersion, PROTOCOL_VERSION)
	}
	if v.DaemonVersion != "9.9.9-test" {
		t.Errorf("daemonVersion = %q, want the injected one", v.DaemonVersion)
	}
}

// The test that catches a handler returning PROTOCOL_VERSION instead of the
// session's: with two connections at different negotiated versions, a constant
// would make them agree.
func TestVersionReportsPerConnectionNotAConstant(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)

	// Forge a second session at a version the constant is not, the way a future
	// daemon speaking v2 would have one.
	sess := &Session{ConnID: "c2", Version: 7, ClientName: "old-editor"}
	if err := tbl.Sessions.Open(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	res, err := tbl.Dispatch(context.Background(), "c2", MethodVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(VersionResult).ProtocolVersion; got != 7 {
		t.Fatalf("version reported %d for a connection negotiated at 7 — the handler is "+
			"returning a constant rather than the session's version", got)
	}
}

func TestASecondInitializeIsRefused(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize,
		mustJSON(t, InitializeParams{ProtocolVersion: PROTOCOL_VERSION}))
	if err == nil {
		t.Fatal("a second initialize succeeded; it would change the version under a live subscription")
	}
	var e *Error
	if !errors.As(err, &e) || e.Code != -31003 {
		t.Fatalf("want -31003, got %v", err)
	}
	if e.Data.Fatal {
		t.Error("a second initialize is not fatal: the first connection is still usable")
	}
}

// T3 inbound, through the real dispatcher rather than the type alone.
func TestAnUnknownFieldInParamsIsIgnoredByTheDispatcher(t *testing.T) {
	tbl := newTestTable(t)
	raw := json.RawMessage(`{"clientName":"c","protocolVersion":2,"somethingFromTheFuture":{"x":1}}`)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize, raw); err != nil {
		t.Fatalf("an unknown param field was rejected: %v", err)
	}
}

// O5 — an unknown capability name is ignored, not an error. Otherwise
// `capabilities` becomes a version by another name, which is what T2 avoids.
func TestAnUnknownCapabilityNameIsIgnored(t *testing.T) {
	tbl := newTestTable(t)
	raw := json.RawMessage(`{"protocolVersion":2,"capabilities":["subscribe","timeTravel"]}`)
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize, raw); err != nil {
		t.Fatalf("an unknown capability name was rejected: %v", err)
	}
}

// An unknown method is JSON-RPC's own condition, not an Aeon one.
func TestAnUnknownMethodIsNotAnAeonError(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodName("nope"), nil)
	if err == nil {
		t.Fatal("an unknown method succeeded")
	}
	var aeon *Error
	if errors.As(err, &aeon) {
		t.Fatalf("an unknown method returned an Aeon code (%d); it is a fact about "+
			"JSON-RPC, not about Aeon", aeon.Code)
	}
	var unknown ErrUnknownMethod
	if !errors.As(err, &unknown) {
		t.Fatalf("want ErrUnknownMethod, got %T", err)
	}
}

func TestBadParamsAreNotAnAeonError(t *testing.T) {
	tbl := newTestTable(t)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodInitialize,
		json.RawMessage(`{"protocolVersion":"not-a-number"}`))
	if err == nil {
		t.Fatal("params of the wrong type were accepted")
	}
	var aeon *Error
	if errors.As(err, &aeon) {
		t.Fatalf("a params decode failure returned an Aeon code (%d); -32602 is "+
			"JSON-RPC's own", aeon.Code)
	}
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("want ErrBadParams, got %T", err)
	}
}

// Absent params are legitimate for a method whose params are all optional.
func TestAbsentParamsAreAccepted(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("{}")} {
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodPing, raw); err != nil {
			t.Errorf("ping with params %q was refused: %v", raw, err)
		}
	}
}

func TestTheDebugMethodIsExcludedFromThePublicTable(t *testing.T) {
	tbl := newTestTable(t)
	for _, name := range tbl.PublicMethods() {
		if name == MethodDebugSubscriptions || name == MethodDebugGoroutines {
			t.Fatalf("%s is in the public table; S19 would mount it on HTTP", name)
		}
		m, _ := tbl.Lookup(name)
		if m.Debug {
			t.Errorf("%s is marked Debug but appears in the public table", name)
		}
	}
	// The excluded set is the two debug methods AND S14's three socket-only
	// writes (invoke, task.complete, task.fail — Debug-marked for LW8's
	// reason: opening and settling a person's work is product control over
	// the P26 socket, never the narrow projection). The count is the
	// difference, and the Debug field is what excludes them; this asserts
	// the count moved for a REASON somebody wrote down.
	if len(tbl.PublicMethods()) != len(tbl.Methods())-5 {
		t.Errorf("PublicMethods dropped %d methods, want the two debug methods plus the three S14 socket-only writes",
			len(tbl.Methods())-len(tbl.PublicMethods()))
	}
}

// Registering a method twice would silently shadow the first — the one bug a
// method table can have that no test of either method would catch.
func TestRegisteringAMethodTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate method registration did not panic")
		}
	}()
	tbl := newTestTable(t)
	tbl.register(&Method{Name: MethodPing})
}

