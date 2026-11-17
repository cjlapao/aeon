package daemon

// records_history_test.go — S53 T3 (HV4, rule 4): the two history arms of
// the records service. What each test pins, in the order the spec lists:
//
//   - the two READ arms answer the SAME thing the person's socket doors
//     answer — both run the very protocol.AnswerHistory* the handlers call
//     (compared through canonical JSON, field for field): one
//     implementation, two doors;
//   - unwired, the arms say so BY NAME (V13) instead of answering an empty
//     history that would read as "nothing ever ran" (P3);
//   - the reads WRITE nothing — counts across the loop, step and archive
//     tables before and after, alike for list, filtered list and the
//     transcript drill-down (HV3 at the tool door);
//   - a person stepping in here is pointed at their own socket doors by
//     name (T15), never answered through a step;
//   - the catalog holds exactly the two, twinned, safe, non-mutating — and
//     no export twin exists to find (HV5).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
)

func newHistoryRecordsEnv(t *testing.T) recordsEnv {
	t.Helper()
	e := newRecordsEnv(t, "")
	// The same facade the Table seam holds at boot (daemon.go, S53 T3).
	e.svc.historyPlane = newHistoryFacadeForTest(t, e)
	// The harness loop's step needs a transcript before the drill-down has
	// something honest to read (the door refuses a transcript-less step by
	// name; that is the door's law, not this file's business).
	if err := e.s.AppendMessages(e.stepID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("kick the walk")}},
	}); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	return e
}

func historyCanon(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHistoryListAnswersTheDoorRenderingByteForByte(t *testing.T) {
	e := newHistoryRecordsEnv(t)
	for _, in := range []string{
		`{}`,
		`{"projectRoot":"/repo/alpha"}`,
		`{"groupBy":"phase","since":"2020-01-01T00:00:00Z"}`,
	} {
		toolOut, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "history_list", json.RawMessage(in))
		if err != nil || f != nil {
			t.Fatalf("history_list %s: f=%v err=%v", in, f, err)
		}
		var p protocol.HistoryListParams
		if err := json.Unmarshal([]byte(in), &p); err != nil {
			t.Fatal(err)
		}
		doorOut, err := protocol.AnswerHistoryList(e.svc.historyPlane, p) // the very call handleHistoryList makes
		if err != nil {
			t.Fatal(err)
		}
		if a, b := historyCanon(t, toolOut), historyCanon(t, doorOut); a != b {
			t.Errorf("the tool answer drifted from the socket answer for %s:\n tool: %s\n door: %s", in, a, b)
		}
	}
	// A filter word the lanes do not know is refused by name, verbatim (P3).
	_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "history_list", json.RawMessage(`{"groupBy":"vibe"}`))
	if err == nil && f == nil {
		t.Fatal("an unknown groupBy answered instead of refusing")
	}
	sentence := ""
	if f != nil {
		sentence = f.Sentence
	} else {
		sentence = err.Error()
	}
	if !strings.Contains(sentence, "vibe") {
		t.Errorf("the refusal must name the word it refused, got: %s", sentence)
	}
}

func TestHistoryShowAnswersTheDoorRenderingByteForByte(t *testing.T) {
	e := newHistoryRecordsEnv(t)
	toolOut, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "history_show", json.RawMessage(`{"id":"walk"}`))
	if err != nil || f != nil {
		t.Fatalf("history_show: f=%v err=%v", f, err)
	}
	doorOut, err := protocol.AnswerHistoryShow(e.svc.historyPlane, protocol.HistoryShowParams{ID: "walk"})
	if err != nil {
		t.Fatal(err)
	}
	if a, b := historyCanon(t, toolOut), historyCanon(t, doorOut); a != b {
		t.Errorf("the tool answer drifted from the socket answer:\n tool: %s\n door: %s", a, b)
	}
	// The unknown id rides the door's own refusal, naming the id (DB19) —
	// never an empty read.
	_, f2, err2 := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, "history_show", json.RawMessage(`{"id":"no-such-loop"}`))
	if err2 == nil && f2 == nil {
		t.Fatal("an unknown id answered instead of refusing")
	}
	var pe *protocol.Error
	if err2 != nil && !errors.As(err2, &pe) {
		t.Fatalf("the refusal lost its protocol identity: %v", err2)
	}
	if pe == nil || !strings.Contains(pe.Data.What, "no-such-loop") {
		t.Errorf("the refusal must name the id it could not find, got: %+v f=%v err=%v", pe, f2, err2)
	}
}

func TestHistoryArmsWithoutThePlaneRefuseByName(t *testing.T) {
	e := newRecordsEnv(t, "") // the harness default: no history plane wired
	for _, name := range []string{"history_list", "history_show"} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, name, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: err=%v", name, err)
		}
		if f == nil {
			t.Fatalf("%s answered %v on a storeless mount — absent must be stated, never answered empty (V13)", name, "no refusal")
		}
		if !strings.Contains(f.Sentence, name) || !strings.Contains(f.Sentence, "V13") {
			t.Errorf("%s refused without naming itself or the rule: %s", name, f.Sentence)
		}
	}
}

func TestHistoryReadsWriteNothing(t *testing.T) {
	e := newHistoryRecordsEnv(t)
	before := map[string]int64{}
	for _, tbl := range []string{"loop", "step", "step_message", "loop_archive", "loop_document", "policy_event"} {
		before[tbl] = historyRowCount(t, e, tbl)
	}
	for _, call := range []struct{ name, in string }{
		{"history_list", `{}`},
		{"history_list", `{"projectRoot":"/repo/alpha","groupBy":"task"}`},
		{"history_show", `{"id":"walk"}`},
	} {
		if _, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: "implementer"}, call.name, json.RawMessage(call.in)); err != nil || f != nil {
			t.Fatalf("%s %s: f=%v err=%v", call.name, call.in, f, err)
		}
	}
	for tbl, n := range before {
		if got := historyRowCount(t, e, tbl); got != n {
			t.Errorf("%s grew from %d to %d rows across two history reads and a drill-down — a read that writes is not a read (HV3)", tbl, n, got)
		}
	}
}

func TestHistoryToolsPointPersonsAtTheSocketDoors(t *testing.T) {
	e := newHistoryRecordsEnv(t)
	for name, want := range map[string]string{
		"history_list": "history.list",
		"history_show": "history.show",
	} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "user", ID: "person"}, name, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: err=%v", name, err)
		}
		if f == nil || !strings.Contains(f.Sentence, want) {
			t.Errorf("%s: a person should be pointed at the socket door %q, got f=%v err=%v", name, want, f, err)
		}
	}
}

func TestHistoryCatalogSpecsAreExactlyTheTwoReads(t *testing.T) {
	var found []records.Spec
	var exportTwins []string
	for _, sp := range records.Catalog() {
		if sp.Family == "history" {
			found = append(found, sp)
		}
		if strings.HasPrefix(sp.Name, "history_export") || sp.Method == "history.export" {
			exportTwins = append(exportTwins, sp.Name)
		}
	}
	if len(found) != 2 {
		t.Fatalf("the history family holds %d specs, want exactly 2: %+v", len(found), found)
	}
	if len(exportTwins) > 0 {
		t.Errorf("an export twin exists in the catalog — HV5 says the export is the person's door: %v", exportTwins)
	}
	byName := map[string]records.Spec{}
	for _, sp := range found {
		byName[sp.Name] = sp
	}
	for name, twin := range map[string]string{"history_list": "history.list", "history_show": "history.show"} {
		sp, ok := byName[name]
		if !ok {
			t.Fatalf("%s is not in the catalog", name)
		}
		if sp.Method != twin {
			t.Errorf("%s twins %s, want %s (rule 4's table)", name, sp.Method, twin)
		}
		if sp.Mutates || sp.Class != "safe" {
			t.Errorf("%s is class %q mutates=%v — HV4 grants two READS", name, sp.Class, sp.Mutates)
		}
		if len(sp.Description) < 40 {
			t.Errorf("%s carries a %d-character description; a step chooses on these words", name, len(sp.Description))
		}
		var shape map[string]any
		if err := json.Unmarshal(sp.Schema, &shape); err != nil {
			t.Fatal(err)
		}
		if shape["additionalProperties"] != false {
			t.Errorf("%s: a schema that admits unknown keys admits smuggled identity", name)
		}
		props, _ := shape["properties"].(map[string]any)
		for forbidden := range props {
			switch forbidden {
			case "actor", "actorKind", "origin", "actorId", "slug", "status":
				t.Errorf("%s: the schema carries the identity/where-field %q (OL3 — identity is stamped, never typed)", name, forbidden)
			}
		}
	}
}

// newHistoryFacadeForTest wires the ONE read-only facade the boot wires
// (daemon.go S53 T3) over the harness store.
func newHistoryFacadeForTest(t *testing.T, e recordsEnv) *history.Store {
	t.Helper()
	return history.NewStore(e.s)
}

func historyRowCount(t *testing.T, e recordsEnv, table string) int64 {
	t.Helper()
	var n int64
	if err := e.s.GormDB().Raw("SELECT COUNT(*) FROM " + table).Row().Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestTheDaemonWiresTheHistorySeam — the restoration, pinned. The October
// "wip" commit b5620631 deleted S25 T4's `table.HistoryStore =
// history.NewStore(opts.Store)` line and every shipped daemon has answered
// the five history doors with the storeless refusal ever since, tests
// included (they wired the seam themselves and never asked what BOOT
// does). A store-backed daemon now wires the facade once and the records
// arms hold the VERY SAME pointer the socket holds — one implementation,
// two doors, by identity and not by agreement; a storeless daemon keeps
// both nil (V13: absent, stated by the arms).
func TestTheDaemonWiresTheHistorySeam(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	d := New(Options{Store: st, Logger: mailboxTestLogger{t}, Routes: mailboxTestRoutes()})
	t.Cleanup(d.Shutdown) // store-backed boots shut down, the package pattern (the reactor/reaper goroutines are live from New)
	if d.Table.HistoryStore == nil {
		t.Fatal("a store-backed daemon did not wire Table.HistoryStore — the history doors are inert again")
	}
	if d.recordsSvc == nil {
		t.Fatal("no records service on a store-backed daemon")
	}
	if d.recordsSvc.historyPlane != d.Table.HistoryStore {
		t.Error("the records arms and the socket doors hold DIFFERENT facades — rule 4 wants one implementation, not two that agree by luck")
	}
	bare := New(Options{})
	if bare.Table.HistoryStore != nil || bare.recordsSvc != nil && bare.recordsSvc.historyPlane != nil {
		t.Error("a storeless daemon wired a history seam it has no store to answer through (V13 wants absence)")
	}
}
