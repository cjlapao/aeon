package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"
)

// inbox_test.go — S58 T2 at the wiring layer: the real store under the real
// table, so "every door names a REGISTERED method" is checked against the
// method set the same build actually serves (the attention package proves
// the table is coherent; only the daemon can prove the table and the socket
// agree).

// openInboxStore opens a real store whose clock the TEST can advance (the
// pointer is the clock: the recurrence test steps it a minute to mint a new
// `since`). Callers who never touch it get the fixed 09:00 they had before.
func openInboxStore(t *testing.T) (*store.Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" },
		Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, &now
}

func TestInboxAbsentWithoutAStore(t *testing.T) {
	d, _ := newTestDaemon(t)
	for _, m := range d.Table.PublicMethods() {
		if m == protocol.MethodInboxList || m == protocol.MethodInboxSummary {
			t.Fatalf("%s is registered on a storeless daemon (V13: ABSENT, not refusing)", m)
		}
	}
}

func TestInboxListAnswersWithDoorsFromTheLiveTable(t *testing.T) {
	st, _ := openInboxStore(t)
	if _, err := st.CreateComponent(store.NewComponent{Name: "Alpha", Slug: "alpha", Root: "/w/alpha"}); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "demo", ComponentRoot: "/w/alpha", Goal: "a goal"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	sc := store.Scope{LoopSlug: "demo", ComponentRoot: "/w/alpha"}
	if _, err := st.Ask(sc, store.NewQuestion{Kind: "clarify", Body: "which way?", Options: []string{"approve", "deny"}}); err != nil {
		t.Fatalf("ask: %v", err)
	}

	d2, _ := newTestDaemonWithStore(t, st)

	res, err := d2.Table.Dispatch(context.Background(), "c1", protocol.MethodInboxList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("inbox.list: %v", err)
	}
	list, ok := res.(protocol.InboxListResult)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if len(list.Items) != 1 {
		t.Fatalf("items = %+v, want the one question", list.Items)
	}
	it := list.Items[0]
	if it.Kind != "question" || it.Severity != "needs-you" || it.Reason != "waiting for your answer" {
		t.Fatalf("item = %+v", it)
	}
	if it.Door.Method != "answer" {
		t.Fatalf("door = %+v, want answer", it.Door)
	}
	// THE wiring claim: the door names a method THIS table registers.
	reg := map[string]bool{}
	for _, m := range d2.Table.PublicMethods() {
		reg[string(m)] = true
	}
	for _, it := range list.Items {
		if it.Door.Method != "" && !reg[it.Door.Method] {
			t.Fatalf("door names %q, which this build does not register (S19's F-1 phantom)", it.Door.Method)
		}
	}
	// The census (SV1).
	census := map[string]string{}
	for _, s := range list.Sources {
		census[s.Name] = s.State
	}
	if census["questions"] != "reporting" {
		t.Fatalf("census = %v", census)
	}
	for _, name := range []string{"budget", "stuck", "scheduler"} {
		if census[name] != "not-wired" {
			t.Fatalf("census = %v, want %s not-wired", census, name)
		}
	}
	// `gates` was reserved for the verification plane; with its source registered, plus the proposals and
	// triage sources, all three report.
	for _, name := range []string{"gates", "proposals", "triage"} {
		if census[name] != "reporting" {
			t.Fatalf("census = %v, want %s reporting", census, name)
		}
	}
	if it.Prompt != "which way?" {
		t.Fatalf("prompt field = %q (the human door renders it; inbox.list IS the human door)", it.Prompt)
	}
	if it.AgeSeconds < 0 {
		t.Fatalf("age = %d", it.AgeSeconds)
	}
}

func TestInboxUnknownScopeIsATypedRefusal(t *testing.T) {
	st, _ := openInboxStore(t)
	d2, _ := newTestDaemonWithStore(t, st)
	_, err := d2.Table.Dispatch(context.Background(), "c1", protocol.MethodInboxList,
		json.RawMessage(`{"rootId":"cmp-nope"}`))
	var perr *protocol.Error
	if !errors.As(err, &perr) || perr.Code != protocol.ErrInboxScope.Code {
		t.Fatalf("unknown scope = %v, want ErrInboxScope (-31926) — never an empty list", err)
	}
	// And with no rootId at all, the every-tree read answers empty-list
	// HONESTLY (there really is nothing) beside the full census.
	res, err := d2.Table.Dispatch(context.Background(), "c1", protocol.MethodInboxList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("every-tree inbox.list: %v", err)
	}
	if list := res.(protocol.InboxListResult); len(list.Items) != 0 || len(list.Sources) < 7 {
		t.Fatalf("empty store answered %+v", list)
	}
}

func TestInboxSummaryCounts(t *testing.T) {
	st, _ := openInboxStore(t)
	if _, err := st.CreateComponent(store.NewComponent{Name: "Alpha", Slug: "alpha", Root: "/w/alpha"}); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := st.CreateLoop(store.NewLoop{Slug: "demo", ComponentRoot: "/w/alpha", Goal: "a goal"}); err != nil {
		t.Fatalf("loop: %v", err)
	}
	sc := store.Scope{LoopSlug: "demo", ComponentRoot: "/w/alpha"}
	if _, err := st.Ask(sc, store.NewQuestion{Kind: "clarify", Body: "which way?"}); err != nil {
		t.Fatalf("ask: %v", err)
	}
	d2, _ := newTestDaemonWithStore(t, st)
	res, err := d2.Table.Dispatch(context.Background(), "c1", protocol.MethodInboxSummary, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("inbox.summary: %v", err)
	}
	sum := res.(protocol.InboxSummaryResult)
	if sum.Total != 1 || sum.BySeverity["needs-you"] != 1 || sum.OldestSince == "" {
		t.Fatalf("summary = %+v", sum)
	}
}

// newTestDaemonWithStore is newTestDaemon with a real store wired (S4's
// Options seam): the inbox needs rows, and the door check needs the table
// this daemon actually built over them.
func newTestDaemonWithStore(t *testing.T, st *store.Store) (*Daemon, *wire) {
	t.Helper()
	w := &wire{}
	d := New(Options{
		DaemonVersion: "9.9.9-test",
		BuildTime:     "2026-10-15T00:00:00Z",
		GitCommit:     "deadbee",
		Logger:        w,
		Notify:        w.notify,
		Store:         st,
		Connections:   func() []localrpc.ConnInfo { return w.conns },
	})
	initConn(t, d, context.Background(), "c1", "test")
	return d, w
}
