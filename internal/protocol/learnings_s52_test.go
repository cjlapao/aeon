package protocol

// S52 T2 — the six management doors on the wire: retract/restore are the
// human door (the peer stamps, an agent is refused BY NAME, an unknown id
// answers ErrLearningUnknown — the named absence beside the refusal);
// board/history/vocabulary/forAgent are open reads that write nothing,
// each answered here through the real store behind the table.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/store"
)

func s52Mount(t *testing.T, stamp func(context.Context, *Session) Actor) (*Table, *store.Store) {
	t.Helper()
	tbl, s := learningsActorMount(t, stamp)
	// LM1's authority for the agent scope and forAgent's slug key.
	if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "golang-engineer", Name: "golang-engineer"}, "2026-11-04T00:00:00Z"); err != nil {
		t.Fatalf("seed the agent row: %v", err)
	}
	return tbl, s
}

func s52User(ctx context.Context, _ *Session) Actor { return Actor{Kind: "user", ID: "1000"} }

func s52Append(t *testing.T, tbl *Table, scope, key, body, prov string) int64 {
	t.Helper()
	got, ref := tbl.Learnings.AppendAs(learnings.AppendInput{
		Scope: learnings.Scope(scope), ScopeKey: key, Body: body, Provenance: learnings.Provenance(prov),
	}, learnings.Actor{Kind: "user", ID: "1000"})
	if ref != nil {
		t.Fatalf("seed append: %+v", ref)
	}
	return got.ID
}

func TestWireRetractIsTheHumanDoorAndIdempotent(t *testing.T) {
	tbl, s := s52Mount(t, s52User)
	id := s52Append(t, tbl, "person", "me", "always gofmt", "told")
	res, err := tbl.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`,"reason":"superseded"}`))
	if err != nil {
		t.Fatalf("retract: %v", err)
	}
	first, ok := res.(LearningsActResult)
	if !ok || !first.Changed || !first.Learning.Retracted || first.Learning.RetractedBy != "user/1000" {
		t.Fatalf("result = %+v, want changed + flagged + peer stamp", first)
	}
	if first.Learning.RetractedReason != "superseded" || first.Learning.RetractedAt == "" {
		t.Fatalf("stamp on the wire = %q @ %q", first.Learning.RetractedReason, first.Learning.RetractedAt)
	}
	res, err = tbl.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`,"reason":"again"}`))
	if err != nil {
		t.Fatalf("second retract: %v", err)
	}
	if second := res.(LearningsActResult); second.Changed {
		t.Fatal("the second retract claims it changed — LM2's honest idempotence")
	}
	n, _ := s.CountAllLearningChanges()
	if n != 2 {
		t.Fatalf("census %d, want add + one retract line — the no-op wrote nothing", n)
	}
	lines, _ := s.ListLearningChanges(id)
	if lines[1].Op != "retract" || lines[1].ActorKind != "user" || lines[1].ActorID != "1000" {
		t.Fatalf("audit line = %+v, want the peer-stamped retract", lines[1])
	}
}

func TestWireRetractRefusesAgentsAndStrangers(t *testing.T) {
	tbl, s := s52Mount(t, func(context.Context, *Session) Actor { return Actor{Kind: "agent", ID: "loop-1"} })
	id := s52Append(t, tbl, "person", "me", "b", "told")
	_, err := tbl.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`,"reason":"an agent tries"}`))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrLearningsRefused.Code {
		t.Fatalf("agent retract err = %v, want the refusal", err)
	}
	if !strings.Contains(e.Data.What, "learning_retract") {
		t.Fatalf("refusal = %q, want it to name the agent's alternative (the proposal kind, T4)", e.Data.What)
	}
	n, _ := s.CountAllLearningChanges()
	if n != 1 {
		t.Fatalf("census %d — the refused agent act wrote no retract line", n)
	}
	// Unwired seam: closed door, same law as add.
	closed, _ := s52Mount(t, nil)
	if _, err := closed.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":1,"reason":"x"}`)); err == nil {
		t.Fatal("retract through an unwired ActorFor seam answered success")
	}
}

func TestWireUnknownIdAnswersErrLearningUnknown(t *testing.T) {
	tbl, _ := s52Mount(t, s52User)
	for name, call := range map[string]func() error{
		"retract": func() error {
			_, err := tbl.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":9999,"reason":"no row"}`))
			return err
		},
		"restore": func() error {
			_, err := tbl.handleLearningsRestore(context.Background(), nil, json.RawMessage(`{"id":9999}`))
			return err
		},
		"history": func() error {
			_, err := tbl.handleLearningsHistory(context.Background(), nil, json.RawMessage(`{"id":9999}`))
			return err
		},
	} {
		err := call()
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrLearningUnknown.Code {
			t.Fatalf("%s of an unknown id: err = %v, want ErrLearningUnknown (%d)", name, err, ErrLearningUnknown.Code)
		}
		if !strings.Contains(e.Data.What, "9999") {
			t.Fatalf("%s refusal %q must quote the id it looked for", name, e.Data.What)
		}
	}
}

func TestWireBoardAndShowAndListHonourTheRetractionPredicate(t *testing.T) {
	tbl, _ := s52Mount(t, s52User)
	keep := s52Append(t, tbl, "project", "/repo", "kept", "told")
	drop := s52Append(t, tbl, "project", "/repo", "retracted here", "told")
	if _, err := tbl.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(drop, 10)+`,"reason":"gone"}`)); err != nil {
		t.Fatalf("retract: %v", err)
	}
	res, err := tbl.handleLearningsBoard(context.Background(), nil, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	board := res.(LearningsBoardResult)
	if len(board.Learnings) != 1 || board.Learnings[0].ID != keep {
		t.Fatalf("board = %+v, want only the live row", board.Learnings)
	}
	if !strings.Contains(board.Learnings[0].Describe, "a person said it") {
		t.Fatalf("describe = %q, want the daemon's own told sentence (LM6)", board.Learnings[0].Describe)
	}
	res2, err := tbl.handleLearningsBoard(context.Background(), nil, json.RawMessage(`{"includeRetracted":true}`))
	if err != nil {
		t.Fatalf("board includeRetracted: %v", err)
	}
	if got := res2.(LearningsBoardResult); len(got.Learnings) != 2 {
		t.Fatalf("includeRetracted board = %d rows, want the flagged one back", len(got.Learnings))
	}
	shown, err := tbl.handleLearningsShow(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(drop, 10)+`}`))
	if err != nil {
		t.Fatalf("show of a retracted row must answer FLAGGED, not refuse: %v", err)
	}
	if sv := shown.(LearningsItemResult).Learning; !sv.Retracted || sv.RetractedReason != "gone" {
		t.Fatalf("show = %+v, want retracted:true + the stamp words", sv)
	}
	lr, err := tbl.handleLearningsList(context.Background(), nil, json.RawMessage(`{"scope":"project","scopeKey":"/repo"}`))
	if err != nil || len(lr.(LearningsListResult).Learnings) != 1 {
		t.Fatalf("default list = %+v (err %v), want the live row only", lr, err)
	}
	lr, err = tbl.handleLearningsList(context.Background(), nil, json.RawMessage(`{"scope":"project","scopeKey":"/repo","includeRetracted":true}`))
	if err != nil || len(lr.(LearningsListResult).Learnings) != 2 {
		t.Fatalf("includeRetracted list = %+v (err %v)", lr, err)
	}
}

func TestWireVocabularyAndForAgentAndHistory(t *testing.T) {
	tbl, _ := s52Mount(t, s52User)
	id := s52Append(t, tbl, "agent", "golang-engineer", "scoped lesson", "observed")
	res, err := tbl.handleLearningsVocabulary(context.Background(), nil, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	v := res.(LearningsVocabularyResult)
	if strings.Join(v.Provenances, ",") != "told,observed,assumed,relayed" {
		t.Fatalf("provenances = %v — the four in honesty order, from the domain's own list", v.Provenances)
	}
	if v.Limits.ReasonMin != 1 || v.Limits.ReasonMax != 256 || !strings.Contains(v.M1Ordering, "relayed ranks below") {
		t.Fatalf("vocabulary limits/law = %+v", v)
	}
	res, err = tbl.handleLearningsForAgent(context.Background(), nil, json.RawMessage(`{"agent":"golang-engineer"}`))
	if err != nil {
		t.Fatalf("forAgent: %v", err)
	}
	fa := res.(LearningsForAgentResult)
	if len(fa.Hits) != 1 || fa.Hits[0].Learning.ID != id || fa.Hits[0].Why.Rank != 1 {
		t.Fatalf("forAgent = %+v, want the agent's own lesson ranked first", fa.Hits)
	}
	if !strings.Contains(fa.Hits[0].Why.Basis, "no embedding call") {
		t.Fatalf("basis = %q — the honest no-query answer (D5)", fa.Hits[0].Why.Basis)
	}
	if _, err := tbl.handleLearningsForAgent(context.Background(), nil, json.RawMessage(`{}`)); err == nil {
		t.Fatal("forAgent without an agent name answered success")
	}
	if _, err := tbl.handleLearningsRetract(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`,"reason":"x"}`)); err != nil {
		t.Fatalf("retract: %v", err)
	}
	res, err = tbl.handleLearningsHistory(context.Background(), nil, json.RawMessage(`{"id":`+strconv.FormatInt(id, 10)+`}`))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	h := res.(LearningsHistoryResult)
	if len(h.Changes) != 2 || h.Changes[0].Op != "add" || h.Changes[1].Op != "retract" || h.Changes[1].ActorKind != "user" {
		t.Fatalf("history = %+v, want the add then the user retract", h.Changes)
	}
}

func TestWireManagementReadsWriteNothing(t *testing.T) {
	tbl, s := s52Mount(t, s52User)
	s52Append(t, tbl, "person", "me", "b", "told")
	before, _ := s.CountAllLearningChanges()
	calls := map[string]func(json.RawMessage) error{
		"board": func(p json.RawMessage) error {
			_, err := tbl.handleLearningsBoard(context.Background(), nil, p)
			return err
		},
		"history": func(p json.RawMessage) error {
			_, err := tbl.handleLearningsHistory(context.Background(), nil, p)
			return err
		},
		"vocabulary": func(p json.RawMessage) error {
			_, err := tbl.handleLearningsVocabulary(context.Background(), nil, p)
			return err
		},
		"forAgent": func(p json.RawMessage) error {
			_, err := tbl.handleLearningsForAgent(context.Background(), nil, p)
			return err
		},
	}
	for name, call := range calls {
		p := json.RawMessage(`{}`)
		if name == "history" {
			p = json.RawMessage(`{"id":1}`)
		}
		if name == "forAgent" {
			p = json.RawMessage(`{"agent":"golang-engineer"}`)
		}
		if err := call(p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	after, _ := s.CountAllLearningChanges()
	if after != before {
		t.Fatalf("audit census moved %d → %d across the four reads — reads that write are writers wearing read names", before, after)
	}
}
