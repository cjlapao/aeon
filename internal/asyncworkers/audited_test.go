package asyncworkers

// S54 T1 — the manager's audited starts. The audit and the ledger row are
// written by the SAME insert attempt, so the two halves of the rule are
// testable here without touching SQL: an "already running" answer — the
// flight collision — writes NEITHER (the transaction rolls back as one),
// and a malformed audit never reaches the ledger at all.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

func TestStartAuditedWritesOneAuditPerStartedWorker(t *testing.T) {
	release := make(chan struct{})
	m, st := openManager(t, Options{})
	if err := m.Register("graph.build", func(ctx context.Context, job Job, r Reporter) (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{}`), nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC) }

	w, started, err := m.StartAudited("graph.build", "/r", "/r", nil, Audit{
		Op: "graph_build", Subject: "/r", Summary: "graph.build started",
		Actor: AuditActor{Kind: "user", ID: "42"},
	})
	if err != nil || !started {
		t.Fatalf("the audited start refused: started=%v err=%v", started, err)
	}
	rows, err := st.PlatformChangeList(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d audit rows for one started worker, want exactly 1", len(rows))
	}
	row := rows[0]
	if row.Op != "graph_build" || row.Subject != "/r" || row.Summary != "graph.build started" {
		t.Errorf("the audit lost the door's composition: %+v", row)
	}
	if row.ActorKind != "user" || row.ActorID != "42" {
		t.Errorf("the audit lost the actor: %+v", row)
	}
	if row.OutcomeRef != w.WorkerID {
		t.Errorf("outcome_ref %q, want the worker the same attempt minted (%q)", row.OutcomeRef, w.WorkerID)
	}
	// The clock discipline: the queue time and the audit time are ONE
	// stamp — an audit that names a minute the worker never queued in
	// answers nothing.
	if w.QueuedAt != "2026-11-04T12:00:00.000Z" || row.OccurredAt != "2026-11-04T12:00:00.000Z" {
		t.Errorf("stamp split: worker %q, audit %q — want one injected clock, one instant", w.QueuedAt, row.OccurredAt)
	}
	close(release)
}

// TestAuditedCollisionWritesNothing is SP3's structural half at this
// layer, both directions: the plain holder vs the audited caller and the
// audited holder vs itself. "Already running" is a data answer, and the
// audit trail gains nothing for a start that did not happen.
func TestAuditedCollisionWritesNothing(t *testing.T) {
	release := make(chan struct{})
	m, st := openManager(t, Options{})
	if err := m.Register("retrieval.reindex", func(ctx context.Context, job Job, r Reporter) (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{}`), nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	audit := Audit{Op: "reindex", Subject: "/r\x00code", Summary: "retrieval.reindex started for corpus code",
		Actor: AuditActor{Kind: "user", ID: "42"}}

	if _, started, err := m.Start("retrieval.reindex", "/r", "/r\x00code", nil); err != nil || !started {
		t.Fatalf("plain start refused: %v", err)
	}
	w, started, err := m.StartAudited("retrieval.reindex", "/r", "/r\x00code", nil, audit)
	if err != nil || started {
		t.Fatalf("the collision answered started=%v err=%v — want the data answer", started, err)
	}
	if w == nil || w.WorkerID == "" {
		t.Fatal("the collision lost the live worker it must name")
	}
	rows, _ := st.PlatformChangeList(context.Background(), "", 0)
	if len(rows) != 0 {
		t.Fatalf("the collision against a PLAIN holder wrote %d audit rows — nothing was started", len(rows))
	}

	// Now the audited holder collides with itself.
	if _, started, err := m.StartAudited("retrieval.reindex", "/r", "/r\x00second", nil, audit); err != nil || !started {
		t.Fatalf("the second audited start (fresh key) refused: %v", err)
	}
	if _, started, err := m.StartAudited("retrieval.reindex", "/r", "/r\x00second", nil, audit); err != nil || started {
		t.Fatalf("same-key audited collision answered started=%v err=%v", started, err)
	}
	rows, _ = st.PlatformChangeList(context.Background(), "", 0)
	if len(rows) != 1 {
		t.Fatalf("%d audit rows for ONE audited start plus one collision, want 1", len(rows))
	}
	close(release)
}

// TestStartAuditedRefusesMalformedAuditAndUnknownType proves both guards
// run BEFORE the ledger is touched — and their refusals reach the caller
// intact (the door maps them to codes from the words).
func TestStartAuditedRefusesMalformedAuditAndUnknownType(t *testing.T) {
	m, st := openManager(t, Options{})
	if err := m.Register("graph.build", func(ctx context.Context, job Job, r Reporter) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}

	_, _, err := m.StartAudited("graph.build", "/r", "/r", nil, Audit{
		Op: "graph_build", Subject: "/r", Summary: "graph.build started",
		Actor: AuditActor{Kind: "system", ID: "hook"},
	})
	if err == nil || !strings.Contains(err.Error(), "lawful actor") {
		t.Fatalf("machine actor accepted or misrefused: %v", err)
	}

	_, _, err = m.StartAudited("graph.rebuild", "/r", "/r", nil, Audit{
		Op: "graph_build", Subject: "/r", Summary: "graph.build started",
		Actor: AuditActor{Kind: "user", ID: "42"},
	})
	if err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("unknown type refused with %v — want the Start refusal verbatim", err)
	}

	rows, _ := st.PlatformChangeList(context.Background(), "", 0)
	if len(rows) != 0 {
		t.Fatalf("refused audited starts wrote %d audit rows", len(rows))
	}
	live, err := st.ListAsyncWorkers(store.AsyncWorkerFilter{Limit: 10})
	if err != nil || len(live) != 0 {
		t.Fatalf("refused audited starts queued %d workers", len(live))
	}
}
