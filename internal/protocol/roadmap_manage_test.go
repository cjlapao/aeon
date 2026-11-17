package protocol

// S50 T2 — the live gates beside the roadmap.update/board/history/
// vocabulary fixtures: the strict param law refuses by KEY naming the
// fields that DO exist, the edit stamps the connection's person like
// every other roadmap write, the no-op is honest and unaudited, and the
// history of a missing row answers its own code rather than an empty
// list wearing not-found.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func roadmapWire(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoadmapUpdateRefusesEveryKeyOutsideItsSet(t *testing.T) {
	cases := []struct {
		name   string
		params string
	}{
		{"status", `{"id":12,"status":"done"}`},
		{"priority", `{"id":12,"priorityState":"known"}`},
		{"estimate", `{"id":12,"estimateMin":1}`},
		{"provenance", `{"id":12,"provenance":"authored"}`},
		{"loopSlug", `{"id":12,"loopSlug":"x"}`},
		{"actor (RM2's key rides the same law)", `{"id":12,"actor":"root"}`},
		{"typo'd field", `{"id":12,"titel":"x"}`},
	}
	refuseTable := newTestTable(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := refuseTable.handleRoadmapUpdate(context.Background(), nil, json.RawMessage(tc.params))
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("update with %s: err = %v, want ErrBadParams", tc.name, err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "refuses the param") || !strings.Contains(msg, "briefPath, planPath") ||
				!strings.Contains(msg, "(RM1)") {
				t.Fatalf("the refusal does not name the fields that exist: %s", msg)
			}
		})
	}
}

func TestRoadmapUpdateStampsTheConnectionPerson(t *testing.T) {
	tbl, s, root := roadmapActorMount(t, func(context.Context, *Session) Actor {
		return Actor{Kind: "user", ID: "4242"}
	})
	ctx := context.Background()
	added, err := tbl.handleRoadmapAdd(ctx, nil, roadmapWire(t, RoadmapAddParams{
		ComponentRoot: root, Title: "edit me", Provenance: "authored", PriorityState: "unknown",
	}))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var ar struct {
		Item struct {
			ID int64 `json:"id"`
		} `json:"item"`
	}
	b, _ := json.Marshal(added)
	if err := json.Unmarshal(b, &ar); err != nil || ar.Item.ID == 0 {
		t.Fatalf("add result: %s (%v)", b, err)
	}

	res, err := tbl.handleRoadmapUpdate(ctx, nil, roadmapWire(t, RoadmapUpdateParams{
		ID: ar.Item.ID, Title: strptr64("edited by 4242"),
	}))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	b, _ = json.Marshal(res)
	var ur RoadmapUpdateResult
	if err := json.Unmarshal(b, &ur); err != nil {
		t.Fatal(err)
	}
	if !ur.Changed || ur.Item.Title != "edited by 4242" {
		t.Fatalf("update answered %s", b)
	}
	rows, err := s.RoadmapChangeList(ctx, ar.Item.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Op != "update" || rows[0].ActorKind != "user" ||
		rows[0].ActorID != "4242" || rows[0].Summary != "edited item 1 (fields: title)" {
		t.Fatalf("audit: %+v — want the add and ONE update row stamped with the peer", rows)
	}

	// The no-op twin: changed:false, and the trail does NOT grow.
	res, err = tbl.handleRoadmapUpdate(ctx, nil, roadmapWire(t, RoadmapUpdateParams{
		ID: ar.Item.ID, Title: strptr64("edited by 4242"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(res)
	if err := json.Unmarshal(b, &ur); err != nil || ur.Changed {
		t.Fatalf("no-op update answered %s, want changed:false", b)
	}
	after, _ := s.RoadmapChangeList(ctx, ar.Item.ID, 0)
	if len(after) != 2 {
		t.Fatalf("the no-op audited: %+v", after)
	}
}

func TestRoadmapHistoryOfNoRowNamesTheMiss(t *testing.T) {
	tbl, _, _ := roadmapActorMount(t, nil)
	_, err := tbl.handleRoadmapHistory(context.Background(), nil, json.RawMessage(`{"id":9999}`))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrRoadmapUnknown.Code {
		t.Fatalf("history of no row: %v, want ErrRoadmapUnknown (-31932)", err)
	}
	if !strings.Contains(e.Data.What, "9999") {
		t.Fatalf("the miss does not name the id in `what`: %+v", e.Data)
	}
	if _, err := tbl.handleRoadmapHistory(context.Background(), nil, json.RawMessage(`{"id":1,"limit":-1}`)); err == nil {
		t.Fatal("a negative limit was accepted")
	}
}

func TestRoadmapBoardAndVocabularyAnswerThroughTheTable(t *testing.T) {
	tbl, _, root := roadmapActorMount(t, func(context.Context, *Session) Actor {
		return Actor{Kind: "user", ID: "7"}
	})
	ctx := context.Background()
	if _, err := tbl.handleRoadmapAdd(ctx, nil, roadmapWire(t, RoadmapAddParams{
		ComponentRoot: root, Title: "on the board", Provenance: "authored", PriorityState: "unknown",
	})); err != nil {
		t.Fatal(err)
	}

	// The board answers WITHOUT a componentRoot (RM6's whole-view).
	res, err := tbl.handleRoadmapBoard(ctx, nil, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("board with no filter: %v", err)
	}
	b, _ := json.Marshal(res)
	var board RoadmapBoardResult
	if err := json.Unmarshal(b, &board); err != nil {
		t.Fatal(err)
	}
	if len(board.Rows) != 1 || board.Rows[0].Ready || board.Rows[0].Blocker == "" ||
		!strings.HasPrefix(board.Rows[0].Describe, "idea · priority unknown") ||
		board.Groups[""] != 1 {
		t.Fatalf("board: %s", b)
	}
	// An illegal filter refuses naming the valid values.
	_, err = tbl.handleRoadmapBoard(ctx, nil, json.RawMessage(`{"status":"shipped"}`))
	var fe *Error
	if !errors.As(err, &fe) || !strings.Contains(fe.Data.What, "shipped") ||
		!strings.Contains(fe.Data.What, "cancelled") {
		t.Fatalf("illegal status filter: %v — want a `what` naming the value and the eight", err)
	}

	res, err = tbl.handleRoadmapVocabulary(ctx, nil, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	b, _ = json.Marshal(res)
	var vocab struct {
		Statuses      []any `json:"statuses"`
		TitleMaxBytes int   `json:"titleMaxBytes"`
	}
	if err := json.Unmarshal(b, &vocab); err != nil || len(vocab.Statuses) != 8 || vocab.TitleMaxBytes != 200 {
		t.Fatalf("vocabulary: %s", b)
	}
	if _, err := tbl.handleRoadmapVocabulary(ctx, nil, json.RawMessage(`{"componentRoot":"/x"}`)); err == nil {
		t.Fatal("vocabulary took a param")
	}
}

func strptr64(s string) *string { return &s }
