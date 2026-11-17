package protocol

// S54 T2 — the three platform doors. Registration rides the seams (V13:
// absent, never registered-and-refusing); platform.status is a pure pass-
// through of the composed view (the handler owns no figures and no clock —
// the adapter stamped evaluatedAt before this file ever saw the answer);
// the plan door refuses to price a nothing (I5: no ambient root, no
// default corpus), and the changes door trims its filter and forwards the
// limit, which the STORE clamps — a second clamp here would be a second
// opinion about the same number.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/platformview"
)

func newPlatformTable(status PlatformStatusFunc, plan RetrievalPlanFunc, changes PlatformChangesFunc) *Table {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.PlatformStatus = status
	tbl.RetrievalPlan = plan
	tbl.PlatformChanges = changes
	tbl.RegisterPlatformStatus()
	tbl.RegisterRetrievalPlan()
	tbl.RegisterPlatformChanges()
	return tbl
}

func TestPlatformDoorsAreAbsentWithoutTheirSeams(t *testing.T) {
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tbl.RegisterPlatformStatus() || tbl.RegisterRetrievalPlan() || tbl.RegisterPlatformChanges() {
		t.Fatal("a T2 door registered over a nil seam — V13 answers ABSENT, not refusing")
	}
	seen := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		seen[m] = true
	}
	for _, m := range []MethodName{MethodPlatformStatus, MethodRetrievalReindexPlan, MethodPlatformChanges} {
		if seen[m] {
			t.Errorf("%s is in the method list without its seam — absence means the name does not answer at all", m)
		}
		if _, err := tbl.Dispatch(context.Background(), "c1", m, json.RawMessage(`{}`)); !errors.As(err, new(ErrUnknownMethod)) {
			t.Errorf("%s without a seam answered %v, want the unknown-method sentence", m, err)
		}
	}
}

func TestPlatformDoorsRegisterOverTheirSeams(t *testing.T) {
	tbl := newPlatformTable(
		func(context.Context) (*platformview.StatusView, error) { return &platformview.StatusView{}, nil },
		func(context.Context, string, string) (*RetrievalPlanAnswer, error) {
			return &RetrievalPlanAnswer{}, nil
		},
		func(context.Context, string, int) ([]PlatformChangeView, error) { return nil, nil })
	seen := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		seen[m] = true
	}
	for _, m := range []MethodName{MethodPlatformStatus, MethodRetrievalReindexPlan, MethodPlatformChanges} {
		if !seen[m] {
			t.Errorf("wired seam did not register %s", m)
		}
	}
}

// initConn walks the handshake so RequiresInitialize methods answer; the
// table's initialize gate is transport, not the subject of these tests.
func initConn(t *testing.T, tbl *Table, conn string) {
	t.Helper()
	init := mustJSON(t, InitializeParams{ClientName: "t", ClientVersion: "0", ProtocolVersion: PROTOCOL_VERSION})
	if _, err := tbl.Dispatch(context.Background(), conn, MethodInitialize, init); err != nil {
		t.Fatalf("initialize: %v", err)
	}
}

func TestPlatformStatusIsAByteEqualPassThrough(t *testing.T) {
	// The composed view rides out exactly as the adapter composed it —
	// evaluatedAt included. A handler that stamped or edited anything
	// would fail the re-marshal comparison.
	view := &platformview.StatusView{
		EvaluatedAt: "2026-11-04T14:31:07Z",
		Retrieval:   &platformview.RetrievalBlock{State: platformview.StateStale},
		Graph:       &platformview.GraphBlock{State: platformview.StateUnwired},
		Usage:       &platformview.UsageBlock{State: platformview.StateAvailable},
	}
	tbl := newPlatformTable(func(context.Context) (*platformview.StatusView, error) { return view, nil }, nil, nil)
	initConn(t, tbl, "c1")
	res, err := tbl.Dispatch(context.Background(), "c1", MethodPlatformStatus, mustJSON(t, struct{}{}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("the door re-worded the panel:\n got %s\nwant %s", got, want)
	}
}

func TestPlatformStatusCarriesTheAdaptersRefusal(t *testing.T) {
	// The seam's own error rides out — the handler invents no second
	// refusal voice.
	tbl := newPlatformTable(func(context.Context) (*platformview.StatusView, error) {
		return nil, ErrStoreUnavailable.WithWhat("the store refused the platform reads: database is locked")
	}, nil, nil)
	initConn(t, tbl, "c1")
	_, err := tbl.Dispatch(context.Background(), "c1", MethodPlatformStatus, mustJSON(t, struct{}{}))
	var e *Error
	if !errors.As(err, &e) || e.Data.What != "the store refused the platform reads: database is locked" {
		t.Errorf("the seam's refusal arrived as %v — the sentence must ride verbatim", err)
	}
}

func TestPlanRefusesWithoutRootAndCorpus(t *testing.T) {
	// I5: there is no ambient root on the socket and no default corpus to
	// price; the seam is never asked when the params are missing, because
	// asking would price a nothing.
	for _, tc := range []struct{ name, root, corpus, want string }{
		{"no root", "", "code", "componentRoot"},
		{"blank root", "   ", "code", "componentRoot"},
		{"no corpus", "/proj", "", "corpus"},
		{"blank corpus", "/proj", " \t ", "corpus"},
		{"neither", "", "", "componentRoot"},
	} {
		var asked int
		tbl := newPlatformTable(nil, func(context.Context, string, string) (*RetrievalPlanAnswer, error) {
			asked++
			return &RetrievalPlanAnswer{}, nil
		}, nil)
		initConn(t, tbl, "c1")
		_, err := tbl.Dispatch(context.Background(), "c1", MethodRetrievalReindexPlan,
			mustJSON(t, RetrievalPlanParams{ComponentRoot: tc.root, Corpus: tc.corpus}))
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrInvalidScope.Code || !strings.Contains(e.Data.What, tc.want) {
			t.Errorf("%s: dispatch answered %v (+What %q), want the invalid-scope code naming %q",
				tc.name, err, e.Data.What, tc.want)
		}
		if asked != 0 {
			t.Errorf("%s: the plan seam ran despite the missing param (%d calls)", tc.name, asked)
		}
	}
}

func TestPlanPassesParamsThroughAndCarriesSeamErrors(t *testing.T) {
	// The valid door hands the seam the EXACT strings (no normalisation —
	// retrieval.ValidCorpus and the store own those judgements downstream),
	// and ErrPlatformUnknown arrives with its code, not a paraphrase.
	var gotRoot, gotCorpus string
	tbl := newPlatformTable(nil, func(_ context.Context, root, corpus string) (*RetrievalPlanAnswer, error) {
		gotRoot, gotCorpus = root, corpus
		if corpus == "memes" {
			return nil, ErrPlatformUnknown.WithWhat(`no corpus "memes" at /home/dev/aeon — the corpora are "code" and "history"`)
		}
		return &RetrievalPlanAnswer{ComponentRoot: root, Corpus: corpus}, nil
	}, nil)
	initConn(t, tbl, "c1")
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodRetrievalReindexPlan,
		mustJSON(t, RetrievalPlanParams{ComponentRoot: "/home/dev/aeon", Corpus: "code"})); err != nil {
		t.Fatalf("the valid plan refused: %v", err)
	}
	if gotRoot != "/home/dev/aeon" || gotCorpus != "code" {
		t.Errorf("the seam was asked (%q, %q) — the door edited the params", gotRoot, gotCorpus)
	}
	_, err := tbl.Dispatch(context.Background(), "c1", MethodRetrievalReindexPlan,
		mustJSON(t, RetrievalPlanParams{ComponentRoot: "/home/dev/aeon", Corpus: "memes"}))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrPlatformUnknown.Code {
		t.Errorf("the unknown corpus arrived as %v, want %d verbatim", err, ErrPlatformUnknown.Code)
	}
}

func TestChangesTrimsTheSubjectAndForwardsTheLimit(t *testing.T) {
	var gotSubject string
	var gotLimit, gotCalls int
	tbl := newPlatformTable(nil, nil, func(_ context.Context, subject string, limit int) ([]PlatformChangeView, error) {
		gotSubject, gotLimit, gotCalls = subject, limit, gotCalls+1
		return []PlatformChangeView{{ID: 7, Op: "reindex", Summary: "retrieval.reindex started for corpus code",
			ActorKind: "user", ActorID: "4242", OccurredAt: "2026-11-04T14:31:07Z"}}, nil
	})
	initConn(t, tbl, "c1")
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodPlatformChanges,
		mustJSON(t, PlatformChangesParams{Subject: "  /home/dev/aeon\x00code  ", Limit: 50})); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if gotSubject != "/home/dev/aeon\x00code" || gotLimit != 50 {
		t.Errorf("the seam saw (%q, %d) — want the trimmed subject and the limit as sent", gotSubject, gotLimit)
	}
	// An empty filter is no filter; an omitted limit is the store's
	// default (0 forward — the clamp is the store's one opinion).
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodPlatformChanges, mustJSON(t, struct{}{})); err != nil {
		t.Fatalf("empty dispatch: %v", err)
	}
	if gotCalls != 2 || gotSubject != "" || gotLimit != 0 {
		t.Errorf("empty params reached the seam as (%q, %d) after %d calls", gotSubject, gotLimit, gotCalls)
	}
}
