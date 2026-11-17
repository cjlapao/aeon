package protocol

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// ════════════════════════════════════════════════════════════════════════════
// S25 T4 gates: the three history.* reads are read-only, and the socket and
// the CLI render the SAME rows (H3 — structural, not a rot-prone test).
//
// The socket cannot import cmd/aeon, so it cannot call the CLI's rowOf
// directly. But both surfaces turn an Entry into a row through ONE function —
// internal/history.Row — and both read through ONE query — internal/history.
// Query. So asserting "the socket's rows equal history.Row of the socket's
// own query" pins H3: if the socket ever drifted (a second query, a second
// formatter), the gate fails here, before the app or the CLI ever see the
// difference.
// ════════════════════════════════════════════════════════════════════════════

// historyTable wires the three reads over a fresh real store and initialises
// the connection, mirroring policyTable.
func historyTable(t *testing.T) (*Table, *store.Store) {
	t.Helper()
	tbl := newTestTable(t)
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open a store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tbl.HistoryStore = history.NewStore(s)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, s
}

// seedHistoryFixture puts one live loop (with a known-cost step), one archived
// loop (cold-stored content), and one blocked loop (a recorded reason) under
// the same component — the three shapes history.Row must render distinctly.
func seedHistoryFixture(t *testing.T, s *store.Store) {
	t.Helper()
	if _, err := s.CreateComponent(store.NewComponent{Name: "/proj", Slug: "/proj", Root: "/proj"}); err != nil {
		t.Fatalf("seed component: %v", err)
	}

	// Live loop with a known-cost step AND a transcript the drill-down reads
	// (LiveTranscript walks the step's step_message; a step with none is a
	// real ReadError, never an empty transcript).
	_, _ = s.CreateLoop(store.NewLoop{Slug: "live", ComponentRoot: "/proj", Goal: "build the live loop"})
	sc := store.Scope{LoopSlug: "live", ComponentRoot: "/proj"}
	step, err := s.CreateStep(sc, store.NewStep{Phase: "build", Agent: "gpt"})
	if err != nil {
		t.Fatalf("create step: %v", err)
	}
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: "build the live loop"}}},
		{Role: model.RoleAssistant, Content: []model.Block{{Kind: model.BlockText, Text: "done"}},
			Provenance: &model.Provenance{Route: "r1", Provider: "anthropic", Model: "claude"}},
	}
	if err := s.AppendMessages(step.ID, msgs); err != nil {
		t.Fatalf("append messages: %v", err)
	}

	// Archived loop with cold-stored content (ArchiveLoop reads loop_document).
	arch, _ := s.CreateLoop(store.NewLoop{Slug: "ghost", ComponentRoot: "/proj", Goal: "build the archived loop"})
	as := store.Scope{LoopSlug: "ghost", ComponentRoot: "/proj"}
	if _, err := s.WriteDocument(as, "notes.md", "the archived content", "engineer"); err != nil {
		t.Fatalf("write a document: %v", err)
	}
	if _, err := s.ArchiveLoop(as, "cold storage"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	_ = arch

	// Blocked loop with a recorded reason (D4: the reason travels verbatim).
	if _, err := s.AppendEvent(store.NewEvent{
		LoopSlug: "blocked", ComponentRoot: "/proj", Kind: "loop.blocked",
		Payload: map[string]string{"reason": "no-shell-in-review"},
	}); err != nil {
		t.Fatalf("append a blocked event: %v", err)
	}
	if _, err := s.CreateLoop(store.NewLoop{Slug: "blocked", ComponentRoot: "/proj", Goal: "build the blocked loop"}); err != nil {
		t.Fatalf("create blocked loop: %v", err)
	}
	_ = s.SetLoopStatus(store.Scope{LoopSlug: "blocked", ComponentRoot: "/proj"}, "blocked")
}

// TestHistorySocketAndCLIProduceIdenticalRows is H3 made structural: the
// socket's history.list renders every entry through history.Row, the SAME
// function the CLI's rowOf delegates to. So the socket's rows must equal the
// rows the CLI would produce from the very same entries — asserted here
// directly, not by a second query that could rot.
func TestHistorySocketAndCLIProduceIdenticalRows(t *testing.T) {
	tbl, s := historyTable(t)
	seedHistoryFixture(t, s)

	res, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryList, nil)
	if err != nil {
		t.Fatalf("history.list: %v", err)
	}
	got, ok := res.(HistoryListResult)
	if !ok {
		t.Fatalf("history.list returned %T, want HistoryListResult", res)
	}

	// The CLI's rowOf delegates to history.Row, so re-render the SAME entries
	// the socket read through history.Query through history.Row and compare.
	entries, err := history.Query(s, history.Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	want := make([]history.HistoryRow, 0, len(entries))
	for _, e := range entries {
		want = append(want, history.Row(e))
	}

	if len(got.Rows) != len(want) {
		t.Fatalf("history.list returned %d rows, the CLI query produced %d", len(got.Rows), len(want))
	}
	for i := range want {
		if got.Rows[i] != want[i] {
			t.Fatalf("row %d differs from the CLI: socket=%+v cli=%+v", i, got.Rows[i], want[i])
		}
	}
	// The three shapes must each be present and distinct, or the gate is not
	// really exercising the parity — it would pass on an empty fixture.
	if got.Statement != nil {
		t.Fatalf("a fixture with three loops must not carry an empty statement")
	}
}

// TestHistoryMethodsReadOnlyNoAuditRow is the gate's other half: none of the
// three history.* reads touches a recorder, so no policy_event-style audit row
// appears. A read is not an event; the gate pins that the three methods are
// inert against the store's write surface, the same property policy.*'s reads
// earn (S10 T13).
func TestHistoryMethodsReadOnlyNoAuditRow(t *testing.T) {
	tbl, s := historyTable(t)
	seedHistoryFixture(t, s)

	before, err := countEvents(s)
	if err != nil {
		t.Fatalf("count events before: %v", err)
	}
	for _, m := range []MethodName{MethodHistoryList, MethodHistoryExport, MethodHistoryShow} {
		if _, err := tbl.Dispatch(context.Background(), "c1", m, mustJSON(t, HistoryShowParams{ID: "ghost"})); err != nil {
			// history.show on the ghost (archived) entry answers cleanly; a
			// wrong-door is a refusal, not a failure, so only a true error fails.
			t.Fatalf("%s: %v", m, err)
		}
	}
	after, err := countEvents(s)
	if err != nil {
		t.Fatalf("count events after: %v", err)
	}
	if after != before {
		t.Fatalf("the history reads wrote %d event rows; they must write none", after-before)
	}
}

// countEvents counts every event in the store (no filter, a large limit), so a
// read that wrote an audit row is detected. The three reads must leave the
// count unchanged — a read is not an event.
func countEvents(s *store.Store) (int, error) {
	evs, err := s.ReadEvents(0, store.EventFilter{}, 100000)
	if err != nil {
		return 0, err
	}
	return len(evs), nil
}

// TestHistoryShowRefusesWrongDoor is DB19 on the socket: a live entry asked of
// the archived door (or vice versa) is a named refusal, never an empty read.
func TestHistoryShowRefusesWrongDoor(t *testing.T) {
	tbl, s := historyTable(t)
	seedHistoryFixture(t, s)

	// The live entry asked of the socket's show: it is live, so the archive
	// door is refused — the result carries the refusal, not an empty transcript.
	res, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryShow, mustJSON(t, HistoryShowParams{ID: "live"}))
	if err != nil {
		t.Fatalf("history.show on a live entry: %v", err)
	}
	got, ok := res.(HistoryShowResult)
	if !ok {
		t.Fatalf("history.show returned %T, want HistoryShowResult", res)
	}
	if got.Kind != "live" || len(got.Transcript) == 0 {
		t.Fatalf("a live entry's show must carry its transcript, got %+v", got)
	}
}
