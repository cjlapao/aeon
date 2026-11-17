package protocol

// platform_door_test.go — S54 T1: the four derived-store OPERATIONS are
// human doors. The registration half is V13 with the S54 split (seam AND
// a stamping source); the refusal half is the peer, never the params
// (SP3), with the audit-composition templates pinned at the door the
// store will commit.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/search"
)

func doorHas(tab *Table, name MethodName) bool {
	for _, m := range append(tab.Methods(), tab.PublicMethods()...) {
		if m == name {
			return true
		}
	}
	return false
}

// fullDoorTable wires every seam the four doors need plus a person; the
// actor knob stays with the caller (set tbl.ActorFor or clear it).
func fullDoorTable(t *testing.T) (*Table, *fakeGraph, *fakeWorkers) {
	t.Helper()
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	g := &fakeGraph{}
	f := &fakeWorkers{}
	tab.Graph = g
	tab.AsyncWorkers = f
	tab.RetrievalReindex = func(context.Context, string, string, string, bool, func(int, int)) (search.ReindexOutcome, error) {
		t.Fatal("the reindex seam was called directly — the door starts a worker (V21)")
		return search.ReindexOutcome{}, nil
	}
	tab.RegisterGraph()
	tab.RegisterRetrievalReindex()
	return tab, g, f
}

// Test 1 — the V13 split: a wired seam WITHOUT a stamping source keeps
// the reads and drops the four operations; the same table with ActorFor
// has the whole family. Absent means the name is out of the table, not
// in it and refusing.
func TestPlatformDoorsRegisterOverSeamAndActorSource(t *testing.T) {
	ops := []MethodName{MethodGraphBuild, MethodGraphReparse, MethodGraphDelete, MethodRetrievalReindex}
	reads := []MethodName{MethodGraphExplain, MethodGraphPath, MethodGraphQuery, MethodGraphStatus, MethodGraphRoots}

	tab, _, _ := fullDoorTable(t) // no ActorFor
	for _, name := range ops {
		if doorHas(tab, name) {
			t.Errorf("%s registered without a stamping source — the audit could never name who started it (V13)", name)
		}
	}
	for _, name := range reads {
		if !doorHas(tab, name) {
			t.Errorf("%s absent with the seam wired — reads are not operations; their gate is unchanged (SP6)", name)
		}
	}

	// The knob is read AT registration, so the wired half is a fresh
	// table that sets ActorFor before registering.
	tab3 := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab3.Graph = &fakeGraph{}
	tab3.AsyncWorkers = &fakeWorkers{}
	tab3.RetrievalReindex = func(context.Context, string, string, string, bool, func(int, int)) (search.ReindexOutcome, error) {
		return search.ReindexOutcome{}, nil
	}
	tab3.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	tab3.RegisterGraph()
	tab3.RegisterRetrievalReindex()
	for _, name := range append(append([]MethodName{}, ops...), reads...) {
		if !doorHas(tab3, name) {
			t.Errorf("%s missing with seam AND actor source wired", name)
		}
	}
}

// Test 2 — the refusals: no stamping source, no session, an empty id,
// uid 0, and the agent kind all answer the same code, and NOTHING ran —
// no worker queued, no service call. The kind refusal names the kind it
// refused; the rest ride the registry's own sentence.
func TestPlatformDoorsRefuseUnattributableEndpoints(t *testing.T) {
	cases := []struct {
		name     string
		actorFor func(context.Context, *Session) Actor
		sess     *Session
		wantKind string // only for the kind refusal
	}{
		{"no stamping source", nil, &Session{}, ""},
		{"no session at all", func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }, nil, ""},
		{"empty peer id", func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: ""} }, &Session{}, ""},
		{"uid 0", func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "0"} }, &Session{}, ""},
		{"blank peer id", func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "   "} }, &Session{}, ""},
		{"agent kind", func(context.Context, *Session) Actor { return Actor{Kind: "agent", ID: "step_7"} }, &Session{}, "agent"},
	}
	for _, c := range cases {
		for _, door := range []string{"reindex", "build", "reparse", "delete"} {
			tab, g, f := fullDoorTable(t)
			tab.ActorFor = c.actorFor
			var err error
			raw, _ := json.Marshal(map[string]any{"componentRoot": "/r", "path": "a.go", "corpus": "code"})
			ctx := context.Background()
			switch door {
			case "reindex":
				_, err = tab.handleRetrievalReindex(ctx, c.sess, raw)
			case "build":
				_, err = tab.handleGraphBuild(ctx, c.sess, raw)
			case "reparse":
				_, err = tab.handleGraphReparse(ctx, c.sess, raw)
			case "delete":
				_, err = tab.handleGraphDelete(ctx, c.sess, raw)
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != -31956 {
				t.Errorf("%s / %s: refused with %v, want -31956", c.name, door, err)
				continue
			}
			if c.wantKind != "" && !strings.Contains(e.Data.What, `kind "agent"`) {
				t.Errorf("%s / %s: the kind refusal does not name the kind it refused: %q", c.name, door, e.Data.What)
			}
			if len(g.calls) != 0 || len(f.startCalls) != 0 {
				t.Errorf("%s / %s: the refusal still ran the seams — graph %v, workers %v", c.name, door, g.calls, f.startCalls)
			}
		}
	}
}

// Test 3 — the reindex door composes the audit the store will commit:
// the flight key as subject (root NUL corpus), the closed-word template
// as summary, the peer as actor — and never the seam directly.
func TestReindexDoorComposesTheAudit(t *testing.T) {
	tab, _, f := fullDoorTable(t)
	tab.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "cli", ID: "4242"} }
	raw, _ := json.Marshal(RetrievalReindexParams{ComponentRoot: "rel/proj", Corpus: "history"})
	res, err := tab.handleRetrievalReindex(context.Background(), &Session{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if acc, ok := res.(*RetrievalReindexAccepted); !ok || !acc.Started {
		t.Fatalf("handle answered %+v, want a started worker", res)
	}
	if f.audit == nil {
		t.Fatal("the door started through the plain twin — no audit rode the start")
	}
	abs, _ := filepath.Abs("rel/proj")
	want := asyncworkers.Audit{
		Op: "reindex", Subject: abs + "\x00history",
		Summary: "retrieval.reindex started for corpus history",
		Actor:   asyncworkers.AuditActor{Kind: "cli", ID: "4242"},
	}
	if *f.audit != want {
		t.Errorf("audit = %+v,\n want %+v — the subject must be the flight key itself and the summary the closed template", *f.audit, want)
	}
	if f.startCalls[0] != "retrieval.reindex "+abs+" "+abs+"\x00history" {
		t.Errorf("start saw %q — the door's scoping changed under S54", f.startCalls[0])
	}
}

// Test 4 — the build door's audit: subject the absolute root (its dedupe
// key and the rows' component_root), summary the fixed sentence.
func TestGraphBuildDoorComposesTheAudit(t *testing.T) {
	tab, g, f := fullDoorTable(t)
	tab.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	raw, _ := json.Marshal(GraphBuildParams{ComponentRoot: "rel/proj"})
	if _, err := tab.handleGraphBuild(context.Background(), &Session{}, raw); err != nil {
		t.Fatal(err)
	}
	if f.audit == nil {
		t.Fatal("graph.build started without an audit")
	}
	abs, _ := filepath.Abs("rel/proj")
	want := asyncworkers.Audit{
		Op: "graph_build", Subject: abs, Summary: "graph.build started",
		Actor: asyncworkers.AuditActor{Kind: "user", ID: "1000"},
	}
	if *f.audit != want {
		t.Errorf("audit = %+v, want %+v", *f.audit, want)
	}
	for _, c := range g.calls {
		if strings.HasPrefix(c, "build ") {
			t.Errorf("the build ran inline again: %v", g.calls)
		}
	}
}

// Test 5 — the "already running" answer names its starter (SP3): the
// flight holder's audit row rides the answer as startedBy/startedAt; a
// pass with no audit behind it (write-hook, SP4) answers started:false
// with NO name — data, never a guess.
func TestAlreadyRunningNamesItsStarter(t *testing.T) {
	tab, _, f := fullDoorTable(t)
	tab.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	f.noStart = true
	raw, _ := json.Marshal(RetrievalReindexParams{ComponentRoot: "/r", Corpus: "code"})
	res, err := tab.handleRetrievalReindex(context.Background(), &Session{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	acc := res.(*RetrievalReindexAccepted)
	if acc.Started || acc.StartedBy != "" || acc.StartedAt != "" {
		t.Fatalf("collision with no audit behind it answered %+v — the no-name is the honest answer (SP4)", acc)
	}

	f.starter = &asyncworkers.StarterView{Kind: "cli", ID: "42", At: "2026-11-04T12:00:00.000Z"}
	res, err = tab.handleRetrievalReindex(context.Background(), &Session{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	acc = res.(*RetrievalReindexAccepted)
	if acc.Started || acc.StartedBy != "cli:42" || acc.StartedAt != "2026-11-04T12:00:00.000Z" {
		t.Errorf("the flight holder was not named: %+v, want startedBy cli:42 at the stamped instant", acc)
	}

	tab2, _, f2 := fullDoorTable(t)
	tab2.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	f2.noStart = true
	f2.starter = &asyncworkers.StarterView{Kind: "user", ID: "1000", At: "2026-11-04T12:00:00.000Z"}
	rawB, _ := json.Marshal(GraphBuildParams{ComponentRoot: "/r"})
	res, err = tab2.handleGraphBuild(context.Background(), &Session{}, rawB)
	if err != nil {
		t.Fatal(err)
	}
	bacc := res.(*GraphBuildAccepted)
	if bacc.Started || bacc.StartedBy != "user:1000" || bacc.StartedAt != "2026-11-04T12:00:00.000Z" {
		t.Errorf("graph.build collision did not name the holder: %+v", bacc)
	}
}

// Test 6 — reparse and delete are the INLINE doors: they hand the actor
// across the seam to the service's audited twins (the fake pins the
// actor; the store test pins what the service stamps).
func TestInlineDoorsHandTheActorAcross(t *testing.T) {
	tab, g, _ := fullDoorTable(t)
	tab.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	raw, _ := json.Marshal(GraphReparseParams{ComponentRoot: "/r", Path: "a.go"})
	if _, err := tab.handleGraphReparse(context.Background(), &Session{}, raw); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(GraphDeleteParams{ComponentRoot: "/r"})
	if _, err := tab.handleGraphDelete(context.Background(), &Session{}, raw); err != nil {
		t.Fatal(err)
	}
	want := "reparse-audited /r a.go user:1000|delete-audited /r user:1000"
	if got := strings.Join(g.calls, "|"); got != want {
		t.Errorf("calls = %s, want %s — the inline doors must walk the AUDITED twins with the peer", got, want)
	}
}
