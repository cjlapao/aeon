package protocol

// history_s53_test.go — S53 T1's socket doors: history.coverage and
// history.vocabulary ride the same seam their siblings do (unwired answers
// ErrStoreUnavailable — V13's shape for a store the daemon cannot serve),
// coverage's two scope words are refused by name when both arrive (DB19), and
// vocabulary answers the package's own constants (HV2) — the socket never
// re-states the closed sets it serves.

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/store"
)

// TestS53HistoryReadsRideTheSeam: an unwired store answers both new doors with
// ErrStoreUnavailable, exactly like history.list — the method exists (it is
// classified, listed, fixture-covered) and honestly reports it cannot answer.
func TestS53HistoryReadsRideTheSeam(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	for _, m := range []MethodName{MethodHistoryCoverage, MethodHistoryVocabulary} {
		_, err := tbl.Dispatch(context.Background(), "c1", m, nil)
		if err != ErrStoreUnavailable {
			t.Errorf("%s with no seam: err = %v, want ErrStoreUnavailable", m, err)
		}
	}
}

// TestHistoryCoverageRefusesBothScopeWords: projectRoot and componentRoot name
// the same one scope; naming both is refused by name (DB19), never one
// silently preferred.
func TestHistoryCoverageRefusesBothScopeWords(t *testing.T) {
	tbl, s := historyTable(t)
	seedHistoryFixture(t, s)

	params, err := json.Marshal(HistoryCoverageParams{ProjectRoot: "/proj", ComponentRoot: "/other"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryCoverage, params); err == nil {
		t.Fatal("coverage accepted both scope words — one of them was silently preferred")
	}
}

// TestHistoryCoverageEqualsTheRealQuery is the door's differential: what the
// socket answers IS what internal/history.Coverage computes over the same
// store and scope — the socket is a projection, not a second counter.
func TestHistoryCoverageEqualsTheRealQuery(t *testing.T) {
	tbl, s := historyTable(t)
	seedHistoryFixture(t, s)

	before, err := countEvents(s)
	if err != nil {
		t.Fatalf("count events before: %v", err)
	}
	res, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryCoverage, nil)
	if err != nil {
		t.Fatalf("history.coverage: %v", err)
	}
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryVocabulary, nil); err != nil {
		t.Fatalf("history.vocabulary: %v", err)
	}
	after, err := countEvents(s)
	if err != nil {
		t.Fatalf("count events after: %v", err)
	}
	if after != before {
		t.Fatalf("the two new reads wrote %d event rows; a read is not an event", after-before)
	}
	got, ok := res.(history.CoverageView)
	if !ok {
		t.Fatalf("history.coverage returned %T, want history.CoverageView", res)
	}
	want, err := history.Coverage(s, history.Options{}, history.ByLoop)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the socket answered %+v, the store-side read %+v — two counters", got, want)
	}
	if got.Floor.Statement != history.FloorStatement {
		t.Errorf("the door did not carry the slice's own P32 sentence verbatim")
	}
}

// TestHistoryVocabularyEqualsTheConstantsOnTheWire: the closed sets cross the
// socket unchanged — the door serves internal/history's Vocabulary(), the one
// derivation (HV2). A client that types a lane or an outcome word instead of
// asking this door has stepped outside the vocabulary by choice, not by need.
func TestHistoryVocabularyEqualsTheConstantsOnTheWire(t *testing.T) {
	tbl, _ := historyTable(t)
	res, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryVocabulary, nil)
	if err != nil {
		t.Fatalf("history.vocabulary: %v", err)
	}
	got, ok := res.(history.VocabularyView)
	if !ok {
		t.Fatalf("history.vocabulary returned %T, want history.VocabularyView", res)
	}
	if !reflect.DeepEqual(got.Lanes, []string{"loop", "phase", "task", "agent"}) {
		t.Errorf("lanes on the wire = %v", got.Lanes)
	}
	if !reflect.DeepEqual(got.Outcomes, []string{history.OutcomeRunning, history.OutcomeWaiting, history.OutcomeBlocked,
		history.OutcomeDone, history.OutcomeIdle, history.OutcomeAborted, history.OutcomeFailed}) {
		t.Errorf("outcomes on the wire = %v, want the engine's own statuses", got.Outcomes)
	}
}

// TestHistoryRowsCarryDescribe: the rows history.list serves gained the
// additive describe block — the SAME history.Describe the CLI rows carry (the
// one formatter, HV2), and the eight S25 fields keep their exact meaning.
func TestHistoryRowsCarryDescribe(t *testing.T) {
	tbl, s := historyTable(t)
	seedHistoryFixture(t, s)
	res, err := tbl.Dispatch(context.Background(), "c1", MethodHistoryList, nil)
	if err != nil {
		t.Fatalf("history.list: %v", err)
	}
	got, ok := res.(HistoryListResult)
	if !ok {
		t.Fatalf("history.list returned %T", res)
	}
	if len(got.Rows) == 0 {
		t.Fatal("no rows — the fixture broke")
	}
	entries, err := history.Query(s, history.Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for i, row := range got.Rows {
		if row.Describe != history.Describe(entries[i]) {
			t.Errorf("row %d's describe block is not the one formatter's output — socket=%+v", i, row.Describe)
		}
		if row.Describe.Cost != row.Cost {
			t.Errorf("row %d: describe.cost %q disagrees with the row's own cost %q", i, row.Describe.Cost, row.Cost)
		}
	}
}
