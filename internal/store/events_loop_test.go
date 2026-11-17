package store

import (
	"fmt"
	"testing"
)

func appendLoopEvents(t *testing.T, s *Store, loopID, kind string, n int) []int64 {
	t.Helper()
	var seqs []int64
	for i := 0; i < n; i++ {
		ev, err := s.AppendEvent(NewEvent{LoopID: loopID, LoopSlug: "slug-" + loopID, ComponentRoot: "/r", Kind: kind, Payload: map[string]any{"i": i}})
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, ev.Seq)
	}
	return seqs
}

// One loop's journal is one indexed read: the latest window, older windows by cursor, newer windows by cursor, with
// another loop's events interleaved and never mixed in, and the streamed chunks left out when the caller says so.
func TestReadLoopEventsWindows(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	mine := appendLoopEvents(t, s, "L1", "step.done", 10)
	appendLoopEvents(t, s, "L2", "step.done", 5) // another loop, newer ids
	chunks := appendLoopEvents(t, s, "L1", "model.delta", 4)
	_ = chunks

	// Latest window of 3, chunks excluded: the three newest non-chunk events of L1, oldest first.
	win, err := s.ReadLoopEvents("L1", 0, 0, 3, nil, []string{"model.delta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(win.Events) != 3 || win.Events[0].Seq != mine[7] || win.Events[2].Seq != mine[9] || !win.HasOlder || win.HasNewer {
		t.Fatalf("latest window: %+v", win)
	}
	for _, ev := range win.Events {
		if ev.LoopID != "L1" || ev.LoopSlug != "slug-L1" {
			t.Errorf("event not attributed: %+v", ev)
		}
	}

	// Older: the three before the first of that window.
	older, err := s.ReadLoopEvents("L1", win.Events[0].Seq, 0, 3, nil, []string{"model.delta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Events) != 3 || older.Events[0].Seq != mine[4] || older.Events[2].Seq != mine[6] || !older.HasOlder || !older.HasNewer {
		t.Fatalf("older window: %+v", older)
	}

	// Newer from the middle: ascending, only L1.
	newer, err := s.ReadLoopEvents("L1", 0, mine[8], 10, nil, []string{"model.delta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(newer.Events) != 1 || newer.Events[0].Seq != mine[9] || newer.HasNewer {
		t.Fatalf("newer window: %+v", newer)
	}

	// Without the exclusion the chunks come back too.
	all, _ := s.ReadLoopEvents("L1", 0, 0, 100, nil, nil)
	if len(all.Events) != 14 || all.HasOlder {
		t.Fatalf("whole journal of L1: %d events, hasOlder %v", len(all.Events), all.HasOlder)
	}
	// A kind filter narrows.
	chunkOnly, _ := s.ReadLoopEvents("L1", 0, 0, 100, []string{"model.*"}, nil)
	if len(chunkOnly.Events) != 4 {
		t.Fatalf("kind filter: %d", len(chunkOnly.Events))
	}
	if _, err := s.ReadLoopEvents("L1", 5, 5, 1, nil, nil); err == nil {
		t.Error("before and after together must be refused")
	}
	if _, err := s.ReadLoopEvents("", 0, 0, 1, nil, nil); err == nil {
		t.Error("a missing loop id must be refused")
	}
	_ = fmt.Sprint
}

// The migration's backfill attributes events stored before the columns existed, from their payloads: a loop event by
// its loopId, a step event through the step's own row.
func TestEventJournalBackfillKnowsOldEvents(t *testing.T) {
	s, err := Open(Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// An event appended WITHOUT loop_id/step_id columns, as the old code wrote it.
	if _, err := s.w.Exec(`INSERT INTO event (loop_slug, component_root, kind, payload, created_at) VALUES (NULL,'/r','loop.advanced','{"loopId":"LX"}','2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// The migration's UPDATE, as written, applied to that row.
	if _, err := s.w.Exec(`UPDATE event SET loop_id = json_extract(payload, '$.loopId') WHERE json_valid(payload) AND json_type(payload, '$.loopId') = 'text' AND loop_id IS NULL`); err != nil {
		t.Fatal(err)
	}
	win, err := s.ReadLoopEvents("LX", 0, 0, 10, nil, nil)
	if err != nil || len(win.Events) != 1 || win.Events[0].Kind != "loop.advanced" {
		t.Fatalf("backfilled event not found: %+v %v", win, err)
	}
}
