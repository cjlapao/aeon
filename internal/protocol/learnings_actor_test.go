package protocol

// S52 T1 — learnings.add is stamped by the connection: the actor is the
// peer (P26), never a param, and an unresolvable peer closes the door
// before anything decodes. The roadmap_actor_test pattern: a real store
// behind the seam, so the write could actually land and the audit line can
// be read back.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/store"
)

func learningsActorMount(t *testing.T, stamp func(context.Context, *Session) Actor) (*Table, *store.Store) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tbl := newTestTable(t)
	tbl.Learnings = learnings.New(s)
	if stamp != nil {
		tbl.ActorFor = stamp
	}
	return tbl, s
}

func addParams(scope, key string) json.RawMessage {
	return json.RawMessage(`{"scope":"` + scope + `","scopeKey":"` + key + `","body":"b","provenance":"observed"}`)
}

func TestLearningsAddWithoutActorSeamRefuses(t *testing.T) {
	tbl, s := learningsActorMount(t, nil)
	_, err := tbl.handleLearningsAdd(context.Background(), nil, addParams("person", "me"))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrLearningsRefused.Code {
		t.Fatalf("err = %v, want the learnings refusal (the door is closed without the seam, not open and anonymous)", err)
	}
	if !strings.Contains(e.Data.What, "cannot resolve who is writing") {
		t.Fatalf("refusal what = %q, want it to name the missing ActorFor seam", e.Data.What)
	}
	n, err := s.CountAllLearningChanges()
	if err != nil || n != 0 {
		t.Fatalf("audit census %d (err %v) after the closed-door call — a refusal writes nothing", n, err)
	}
}

func TestLearningsAddStampsThePeerActor(t *testing.T) {
	tbl, s := learningsActorMount(t, func(context.Context, *Session) Actor {
		return Actor{Kind: "user", ID: "12345"}
	})
	got, err := tbl.handleLearningsAdd(context.Background(), nil, addParams("person", "me"))
	if err != nil {
		t.Fatalf("the peer-stamped add refused: %v", err)
	}
	res, ok := got.(LearningsItemResult)
	if !ok {
		t.Fatalf("result type %T", got)
	}
	lines, err := s.ListLearningChanges(res.Learning.ID)
	if err != nil || len(lines) != 1 || lines[0].ActorKind != "user" || lines[0].ActorID != "12345" {
		t.Fatalf("audit after the peer add = %+v (err %v), want the one add line by user/12345", lines, err)
	}
}

func TestLearningsAddRefusesAnEmptyActor(t *testing.T) {
	// The seam is wired but answers zero (no peer credential resolved):
	// refused by the same sentence's law, BEFORE the params are decoded —
	// a malformed-params call behind a broken identity is an identity
	// failure first.
	tbl, _ := learningsActorMount(t, func(context.Context, *Session) Actor { return Actor{} })
	_, err := tbl.handleLearningsAdd(context.Background(), nil, json.RawMessage(`{"garbage":true`))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrLearningsRefused.Code || !strings.Contains(e.Data.What, "no actor") {
		t.Fatalf("err = %v, want the empty-actor refusal ahead of any decode error", err)
	}
}
