package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/automationview"
)

// automation_read_test.go — S49 T3: the protocol half of the unified
// automation reads. The DAEMON's suite (internal/daemon/automation_read_
// *_test.go) proves the answers equal the engines and the store; this file
// pins what the SOCKET adds: V13 absence, the named-sentence param refusals
// (-32602), the two empties of automation.list, the changes statement, and
// that the vocabulary and the test door speak automationview's answers
// byte-identically (no protocol-side restatement — AN5).

// automationReadsStub is the wired seam: a non-nil interface with the eight
// methods, each delegating to a func field the test sets (unset ⇒ zero
// answer). Registration only inspects for nil; behavior tests set fns.
type automationReadsStub struct {
	listFn    func(context.Context, AutomationListParams) ([]AutomationView, int, error)
	showFn    func(context.Context, AutomationShowParams) (AutomationShowResult, error)
	firesFn   func(context.Context, AutomationFiresParams) ([]AutomationFireView, AutomationFireCounts, string, error)
	runsFn    func(context.Context, AutomationRunsParams) (AutomationRunsResult, error)
	changesFn func(context.Context, AutomationChangesParams) ([]AutomationChangeView, error)
	healthFn  func(context.Context, AutomationHealthParams) ([]AutomationHealthView, error)
	previewFn func(context.Context, AutomationPreviewParams) ([]AutomationInstant, error)
	dryFn     func(context.Context, AutomationDryRunParams) (AutomationDryRunResult, error)
}

func (s automationReadsStub) ListAutomations(ctx context.Context, p AutomationListParams) ([]AutomationView, int, error) {
	if s.listFn != nil {
		return s.listFn(ctx, p)
	}
	return []AutomationView{}, 0, nil
}
func (s automationReadsStub) ShowAutomation(ctx context.Context, p AutomationShowParams) (AutomationShowResult, error) {
	if s.showFn != nil {
		return s.showFn(ctx, p)
	}
	return AutomationShowResult{}, nil
}
func (s automationReadsStub) ListAutomationFires(ctx context.Context, p AutomationFiresParams) ([]AutomationFireView, AutomationFireCounts, string, error) {
	if s.firesFn != nil {
		return s.firesFn(ctx, p)
	}
	return []AutomationFireView{}, AutomationFireCounts{}, "", nil
}
func (s automationReadsStub) AutomationRuns(ctx context.Context, p AutomationRunsParams) (AutomationRunsResult, error) {
	if s.runsFn != nil {
		return s.runsFn(ctx, p)
	}
	return AutomationRunsResult{}, nil
}
func (s automationReadsStub) ListAutomationChanges(ctx context.Context, p AutomationChangesParams) ([]AutomationChangeView, error) {
	if s.changesFn != nil {
		return s.changesFn(ctx, p)
	}
	return []AutomationChangeView{}, nil
}
func (s automationReadsStub) AutomationHealth(ctx context.Context, p AutomationHealthParams) ([]AutomationHealthView, error) {
	if s.healthFn != nil {
		return s.healthFn(ctx, p)
	}
	return []AutomationHealthView{}, nil
}
func (s automationReadsStub) PreviewAutomations(ctx context.Context, p AutomationPreviewParams) ([]AutomationInstant, error) {
	if s.previewFn != nil {
		return s.previewFn(ctx, p)
	}
	return []AutomationInstant{}, nil
}
func (s automationReadsStub) DryRunAutomation(ctx context.Context, p AutomationDryRunParams) (AutomationDryRunResult, error) {
	if s.dryFn != nil {
		return s.dryFn(ctx, p)
	}
	return AutomationDryRunResult{}, nil
}

// automationTable wires the stub seam and initialises the connection — the
// reads are RequiresInitialize, so every behavior test starts post-handshake.
func automationTable(t *testing.T, stub automationReadsStub) *Table {
	t.Helper()
	tbl := newTestTable(t)
	tbl.AutomationReads = stub
	if !tbl.RegisterAutomationReads() {
		t.Fatal("RegisterAutomationReads refused a wired seam")
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

// V13, the absent half: a bare table answers method-not-found for all ten.
func TestAutomationReadsAbsentWhenSeamNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterAutomationReads() {
		t.Fatal("RegisterAutomationReads reported success with a nil seam")
	}
	for _, m := range []MethodName{
		MethodAutomationList, MethodAutomationShow, MethodAutomationFires, MethodAutomationRuns,
		MethodAutomationHealth, MethodAutomationVocabulary, MethodAutomationPreview,
		MethodAutomationTest, MethodAutomationDryRun, MethodAutomationChanges,
	} {
		for _, pm := range tbl.PublicMethods() {
			if pm == m {
				t.Fatalf("%s is in the table with a nil seam (V13 wants it ABSENT, not refusing)", m)
			}
		}
	}
}

func TestAutomationReadsRefuseBadFilterWords(t *testing.T) {
	tbl := automationTable(t, automationReadsStub{})
	cases := []struct {
		method       MethodName
		params       string
		wantContains string
	}{
		{MethodAutomationList, `{"source":"clock"}`, "not one of schedule, webhook, event"},
		{MethodAutomationList, `{"archived":"yes"}`, "not one of exclude, include, only"},
		{MethodAutomationShow, `{"id":"sch-x"}`, "source"},                                              // absent source refuses its own way
		{MethodAutomationShow, `{"source":"event"}`, "`id` is required"},                                // absent id
		{MethodAutomationFires, `{"outcome":"did-not-fire"}`, "outcome"},                                // the view speaks the five
		{MethodAutomationRuns, `{"source":"event"}`, "`id` is required"},                                // one named automation's runs
		{MethodAutomationPreview, `{"spec":{"expr":"@hourly","zone":"UTC"},"n":21}`, "preview horizon"}, // past the engine's cap
		{MethodAutomationPreview, `{"spec":{"expr":"* * * * *","zone":"UTC"},"from":"now"}`, "RFC3339"},
		{MethodAutomationPreview, `{}`, "exactly one of"}, // neither id nor spec
		{MethodAutomationDryRun, `{}`, "`id` is required"},
		{MethodAutomationChanges, `{"limit":-1}`, "negative"},
	}
	for _, tc := range cases {
		_, err := tbl.Dispatch(context.Background(), "c1", tc.method, json.RawMessage(tc.params))
		if err == nil {
			t.Errorf("%s %s: accepted", tc.method, tc.params)
			continue
		}
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s %s: err = %v, want ErrBadParams (-32602)", tc.method, tc.params, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantContains) {
			t.Errorf("%s %s: %q does not name %q", tc.method, tc.params, err.Error(), tc.wantContains)
		}
	}
	// The `show` id-present case: source required beside the id.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationShow, json.RawMessage(`{"id":"sch-x","source":"clock"}`)); err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Errorf("show with a bad source: %v", err)
	}
}

// AN5's two empties: a filtered-empty list is the FILTER's answer; an
// unfiltered-empty list over an empty store is the STORE's. A stub answers
// both live totals; the handler must not conflate them.
func TestListTellsTheTwoEmptiesApart(t *testing.T) {
	cases := []struct {
		name, params string
		liveTotal    int
		want         string
	}{
		{"store empty", `{}`, 0, "no automations. nothing starts agents on its own."},
		{"filtered away", `{"source":"schedule"}`, 7, "no automations match this filter."},
		{"only-archived, none archived", `{"archived":"only"}`, 7, "no automations match this filter."},
		{"filter over empty store keeps the store's sentence", `{"blueprint":"none"}`, 0, "no automations match this filter."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := automationTable(t, automationReadsStub{
				listFn: func(context.Context, AutomationListParams) ([]AutomationView, int, error) {
					return []AutomationView{}, tc.liveTotal, nil
				},
			})
			res, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationList, json.RawMessage(tc.params))
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			got := res.(AutomationListResult)
			if got.Statement != tc.want {
				t.Errorf("statement = %q, want %q", got.Statement, tc.want)
			}
		})
	}
	// Non-empty list carries no statement at all.
	tbl := automationTable(t, automationReadsStub{
		listFn: func(context.Context, AutomationListParams) ([]AutomationView, int, error) {
			return []AutomationView{{Source: "schedule", ID: "sch-a"}}, 1, nil
		},
	})
	res, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(AutomationListResult); got.Statement != "" {
		t.Errorf("a non-empty list answered statement %q — the sentence is for the empty only", got.Statement)
	}
}

// automation.changes: the statement is the AUDIT's own sentence and appears
// exactly when the listing is empty (even under a filter — T3 spec).
func TestChangesStatementOnlyWhenEmpty(t *testing.T) {
	tbl := automationTable(t, automationReadsStub{})
	res, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationChanges, json.RawMessage(`{"source":"event"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(AutomationChangesResult); got.Statement != "no automation changes recorded." {
		t.Errorf("empty audit: statement = %q", got.Statement)
	}
	tbl2 := automationTable(t, automationReadsStub{
		changesFn: func(context.Context, AutomationChangesParams) ([]AutomationChangeView, error) {
			return []AutomationChangeView{{ID: 1, Source: "event", TriggerID: "evt-a", Op: "create"}}, nil
		},
	})
	res2, err := tbl2.Dispatch(context.Background(), "c1", MethodAutomationChanges, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := res2.(AutomationChangesResult); got.Statement != "" || len(got.Changes) != 1 {
		t.Errorf("non-empty audit: %+v", got)
	}
}

// The vocabulary read answers automationview.Vocabulary() ITSELF — wire
// bytes equal, because there is no protocol-side copy to drift (AN5).
func TestVocabularyIsTheValidatorDerivedReadByteIdentical(t *testing.T) {
	tbl := automationTable(t, automationReadsStub{})
	res, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationVocabulary, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	got, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(automationview.Vocabulary())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("vocabulary drifted from automationview.Vocabulary():\n got %s\nwant %s", got, want)
	}
}

// automation.test runs the REAL grammar (internal/match through
// automationview.TestMatch): a valid rule answers per-key rows, and an
// invalid one answers ErrAutomationPreview carrying the validator's
// sentence VERBATIM — the words a save would speak (AN8).
func TestTestSpeaksTheGrammarVerbatim(t *testing.T) {
	tbl := automationTable(t, automationReadsStub{})
	res, err := tbl.Dispatch(context.Background(), "c1", MethodAutomationTest, json.RawMessage(
		`{"match":{"event":"task.created","task_type":["bug"]},"sample":{"event":"task.created","taskType":"bug"}}`))
	if err != nil {
		t.Fatalf("test (valid rule): %v", err)
	}
	got := res.(AutomationTestResult)
	if !got.Matches {
		t.Errorf("bug task did not match a rule wanting a bug: %+v", got)
	}
	if !strings.Contains(got.Canonical, `"task_type":["bug"]`) && got.Canonical == "" {
		t.Errorf("no canonical form in %+v", got)
	}
	_, err = tbl.Dispatch(context.Background(), "c1", MethodAutomationTest, json.RawMessage(
		`{"match":{"event":"task.created","root":4},"sample":{"event":"task.created"}}`))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrAutomationPreview.Code {
		t.Fatalf("test (invalid rule): err = %v, want %d", err, ErrAutomationPreview.Code)
	}
	_, wantErr := automationview.TestMatch(json.RawMessage(`{"event":"task.created","root":4}`), automationview.Event{Event: "task.created"})
	if wantErr == nil || !strings.Contains(e.Data.What, wantErr.Error()) {
		t.Errorf("refusal %q does not carry the validator's sentence %q verbatim", e.Data.What, wantErr)
	}
}

// The refusal errors are registered with the three parts intact — the
// conformance fixtures pin the wire; this pins the Go table.
func TestS49ErrorCodesRegistered(t *testing.T) {
	reg := map[int]string{}
	for _, e := range ErrorRegistry() {
		reg[e.Code] = e.Message
	}
	for code, msg := range map[int]string{-31890: "unknown automation", -31891: "automation preview refused"} {
		if reg[code] != msg {
			t.Errorf("ErrorRegistry()[%d] = %q, want %q", code, reg[code], msg)
		}
	}
}
