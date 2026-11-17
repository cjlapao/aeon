package protocol

// S51 T2 — the management doors at the wire: the edit refuses what it may
// not edit BEFORE anything else reads the params, answers changed:false
// without an audit row, and refuses to be anonymous; the history read
// keeps "no rows" and "no bug" as the two different errors they are; the
// vocabulary answers the closed sets and nothing invented. The static
// contracts live in conformance/triage/{update,update-body,board,
// board-empty,board-filtered,history,vocabulary,history-unknown}.json.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/triage"
)

func TestUpdateRefusesKeysItMayNotEdit(t *testing.T) {
	tbl, _, _ := triageActorMount(t, nil)
	for _, tc := range []struct{ raw, want string }{
		{`{"id":1,"title":"t","actor":"root"}`, `"actor"`},
		{`{"id":1,"title":"t","filedBy":"golang-engineer"}`, `"filedBy"`},
		{`{"id":1,"title":"t","actorKind":"system"}`, `"actorKind"`},
		{`{"id":1,"title":"t","validationState":"reproduced"}`, `"validationState"`},
		{`{"id":1,"title":"t","priorityState":"known"}`, `"priorityState"`},
		{`{"id":1,"title":"t","duplicateOf":2}`, `"duplicateOf"`},
		{`{"id":1,"title":"t","roadmapItemId":3}`, `"roadmapItemId"`},
		{`{"id":1,"title":"t","componentRoot":"/r"}`, `"componentRoot"`}, // a bug's home is not a title
	} {
		_, err := tbl.handleTriageUpdate(context.Background(), nil, json.RawMessage(tc.raw))
		if err == nil {
			t.Errorf("%s: accepted", tc.raw)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q lacks %q", tc.raw, err.Error(), tc.want)
		}
	}
	// An edit of NOTHING is refused too — the pointers make "absent" say
	// so, and a call that edits neither field is a bug in the caller.
	if _, err := tbl.handleTriageUpdate(context.Background(), nil, json.RawMessage(`{"id":1}`)); err == nil {
		t.Error("an update of no fields was accepted")
	}
}

func TestUpdateIsThePersonsDoorAndTheNoOpEarnsNoRow(t *testing.T) {
	stamp := func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "42"} }
	tbl, s, root := triageActorMount(t, stamp)
	ctx := context.Background()
	fileRaw, _ := json.Marshal(map[string]any{"componentRoot": root, "title": "the original", "body": "report"})
	view, err := tbl.handleTriageFile(ctx, nil, fileRaw)
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	id := view.(*TriageView).ID

	// Anonymous: refused before anything reads or writes (T1's law, the
	// new verb rides it).
	noStamp, _, _ := triageActorMount(t, nil)
	if _, err := noStamp.handleTriageUpdate(ctx, nil, json.RawMessage(`{"id":1,"title":"x"}`)); err == nil {
		t.Fatal("anonymous update accepted")
	} else if ae, ok := err.(*Error); !ok || ae.Code != ErrTriageNoActor.Code {
		t.Errorf("no-actor update: got %#v want code %d", err, ErrTriageNoActor.Code)
	}

	// A real edit lands, and its audit row names the field word only.
	up, err := tbl.handleTriageUpdate(ctx, nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`,"title":"fixed title"}`))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	res := up.(*TriageUpdateResult)
	if !res.Changed || res.Bug.Title != "fixed title" {
		t.Fatalf("update result: %+v", res)
	}
	rows, herr := s.BugChangeList(ctx, id, 0)
	if herr != nil {
		t.Fatalf("history read: %v", herr)
	}
	var updates int
	for _, r := range rows {
		if r.Op == "update" {
			updates++
			if r.ActorKind != "user" || r.ActorID != "42" {
				t.Errorf("update audit actor: %s/%s", r.ActorKind, r.ActorID)
			}
		}
	}
	if updates != 1 {
		t.Fatalf("update rows: %d want 1", updates)
	}

	// The no-op: same title ⇒ changed:false AND no new row.
	before := len(rows)
	up2, err := tbl.handleTriageUpdate(ctx, nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`,"title":"fixed title"}`))
	if err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	if up2.(*TriageUpdateResult).Changed {
		t.Error("an identical edit answered changed:true")
	}
	after, _ := s.BugChangeList(ctx, id, 0)
	if len(after) != before {
		t.Errorf("the no-op earned an audit row — %d rows were %d", len(after), before)
	}
}

func TestHistoryAnswersTwoDifferentNothings(t *testing.T) {
	tbl, _, root := triageActorMount(t, nil)
	ctx := context.Background()

	// No such bug: ErrTriageUnknown, the named refusal — NOT an empty list.
	_, err := tbl.handleTriageHistory(ctx, nil, json.RawMessage(`{"id":9999}`))
	ae, ok := err.(*Error)
	if !ok || ae.Code != ErrTriageUnknown.Code {
		t.Fatalf("unknown id: got %#v want code %d", err, ErrTriageUnknown.Code)
	}

	// A real bug with no rows: EMPTY, not an error. tr.Create (no audit)
	// makes exactly that bug through the door the socket does not use.
	b, r := tbl.Triage.Create(root, triage.NewBug{Title: "quiet", Body: "no history", FiledBy: "person"})
	if r != nil {
		t.Fatalf("seed: %v", r)
	}
	out, err := tbl.handleTriageHistory(ctx, nil, json.RawMessage(`{"id":`+strconv.FormatInt(b.ID, 10)+`}`))
	if err != nil {
		t.Fatalf("empty history: %v", err)
	}
	if rows := out.([]TriageHistoryRow); len(rows) != 0 {
		t.Fatalf("a bug with no audit answered %d rows", len(rows))
	}
}

func TestVocabularyAnswersTheOwners(t *testing.T) {
	tbl, _, _ := triageActorMount(t, nil)
	out, err := tbl.handleTriageVocabulary(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	got, ok := out.(triage.Vocabulary)
	if !ok {
		t.Fatalf("type %T", out)
	}
	if want := tbl.Triage.Vocabulary(); !reflect.DeepEqual(got, want) {
		t.Errorf("the wire drifted from the door: %v vs %v", got, want)
	}
	// And the door itself equals the plane's owners (the property test in
	// internal/triage pins the deep equality; here the wire carries it).
	if len(got.ValidationStates) != 4 || len(got.PriorityStates) != 3 {
		t.Errorf("set sizes: %v / %v", got.ValidationStates, got.PriorityStates)
	}
}

func TestBoardWithoutItsSeamIsLoud(t *testing.T) {
	tbl, _, _ := triageActorMount(t, nil)
	// Triage is mounted but the BOARD seam is not: the registration answer
	// is ErrStoreUnavailable, loud — never an empty board dressed as truth.
	if _, err := tbl.handleTriageBoard(context.Background(), nil, json.RawMessage(`{}`)); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil board seam: got %v want ErrStoreUnavailable", err)
	}
}

func TestHealthWithoutItsSeamIsLoudAndTakesNoParams(t *testing.T) {
	tbl, _, _ := triageActorMount(t, nil)
	// Registration exists but the seam does not: ErrStoreUnavailable, loud.
	if _, err := tbl.handleTriageHealth(context.Background(), nil, json.RawMessage(`{}`)); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil health seam: got %v want ErrStoreUnavailable", err)
	}
	// And a filter is refused before anything else: a filtered health
	// would imply the read gates something (TM4).
	_, err := tbl.handleTriageHealth(context.Background(), nil, json.RawMessage(`{"key":"triage.validationBlueprint"}`))
	var bad ErrBadParams
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "takes no params") {
		t.Fatalf("filtered health params: got %v, want the no-params refusal by name", err)
	}
}
