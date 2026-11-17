package protocol

// S50 T1 (RM2) — the roadmap writes are the person's door: the actor is
// the CONNECTION's peer, never a param, and an unresolvable peer is
// refused before anything moves. The static contracts live in
// conformance/roadmap/add-no-actor.json and add-identity-key-refused.json;
// these are the live gates beside them, the mcp/blueprint pattern.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// roadmapActorMount wires a REAL store behind the roadmap door so the
// write can actually land and its audit row can be read back — the
// stamping law is only proven when something could have been written.
func roadmapActorMount(t *testing.T, stamp func(context.Context, *Session) Actor) (*Table, *store.Store, string) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := t.TempDir()
	if _, err := s.CreateComponent(store.NewComponent{Name: "r", Slug: "r", Root: root}); err != nil {
		t.Fatalf("seed the component: %v", err)
	}
	tbl := newTestTable(t)
	tbl.Roadmap = roadmap.New(s)
	if stamp != nil {
		tbl.ActorFor = stamp
	}
	return tbl, s, root
}

func TestRoadmapWritesRefuseAnActorParam(t *testing.T) {
	// Every write verb, every identity key — refused by KEY, before the
	// lenient decode, without a store and without an identity seam (the
	// refusal must not depend on whether the write COULD succeed).
	cases := []struct {
		name string
		call func(*Table) error
	}{
		{"add actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapAdd(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","title":"t","provenance":"authored","actor":"root"}`))
			return e
		}},
		{"add actorKind", func(tbl *Table) error {
			_, e := tbl.handleRoadmapAdd(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","title":"t","provenance":"authored","actorKind":"user"}`))
			return e
		}},
		{"add who", func(tbl *Table) error {
			_, e := tbl.handleRoadmapAdd(context.Background(), nil, json.RawMessage(`{"componentRoot":"/r","title":"t","provenance":"authored","who":"cjlapao"}`))
			return e
		}},
		{"cancel actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapCancel(context.Background(), nil, json.RawMessage(`{"id":1,"actor":"root"}`))
			return e
		}},
		{"estimate actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapEstimate(context.Background(), nil, json.RawMessage(`{"id":1,"estimateMin":1,"estimateMax":3,"estimateBasis":"two days of pairing","actor":"root"}`))
			return e
		}},
		{"priority actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapPriority(context.Background(), nil, json.RawMessage(`{"id":1,"priorityState":"known","actor":"root"}`))
			return e
		}},
		{"ideate actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapIdeate(context.Background(), nil, json.RawMessage(`{"id":1,"workspaceRoot":"/w","actor":"root"}`))
			return e
		}},
		{"plan actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapPlan(context.Background(), nil, json.RawMessage(`{"id":1,"workspaceRoot":"/w","actor":"root"}`))
			return e
		}},
		{"materialize actor", func(tbl *Table) error {
			_, e := tbl.handleRoadmapMaterialize(context.Background(), nil, json.RawMessage(`{"id":1,"blueprint":"dev-loop","goal":"g","workspaceRoot":"/w","actor":"root"}`))
			return e
		}},
	}
	tbl := newTestTable(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(tbl)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("%s with an actor param: err = %v, want ErrBadParams", tc.name, err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "actor") || !strings.Contains(msg, "(RM2)") {
				t.Fatalf("%s refused with the wrong sentence: %s", tc.name, msg)
			}
		})
	}
}

func TestRoadmapWritesRefuseAnUnresolvablePeer(t *testing.T) {
	// Two honest absences: no identity seam wired at all, and the uid 0 a
	// connection the transport never recorded reads as. Neither is ever
	// stamped as the person "0" — the mcp rule, carried.
	t.Run("no stamp wired", func(t *testing.T) {
		tbl, _, root := roadmapActorMount(t, nil)
		_, err := tbl.handleRoadmapAdd(context.Background(), nil, mustRoadmapParams(t, RoadmapAddParams{
			ComponentRoot: root, Title: "audited?", Provenance: "authored",
			PriorityState: "assumed",
		}))
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrRoadmapNoActor.Code {
			t.Fatalf("add with no identity seam: err = %v, want ErrRoadmapNoActor (-31931)", err)
		}
	})
	t.Run("uid zero is not a person", func(t *testing.T) {
		tbl, _, _ := roadmapActorMount(t, func(context.Context, *Session) Actor {
			return Actor{Kind: "user", ID: "0"}
		})
		_, err := tbl.handleRoadmapCancel(context.Background(), nil, mustRoadmapParams(t, RoadmapCancelParams{ID: 1}))
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrRoadmapNoActor.Code {
			t.Fatalf("cancel over uid 0: err = %v, want ErrRoadmapNoActor (-31931)", err)
		}
	})
}

func TestRoadmapWriteStampsTheConnectionPerson(t *testing.T) {
	// The positive half: the connection's uid lands in the audit row —
	// user kind, the kernel's id, exactly one row, the 'add' op.
	tbl, s, root := roadmapActorMount(t, func(context.Context, *Session) Actor {
		return Actor{Kind: "user", ID: "4242"}
	})
	res, err := tbl.handleRoadmapAdd(context.Background(), nil, mustRoadmapParams(t, RoadmapAddParams{
		ComponentRoot: root, Title: "audited", Provenance: "authored", PriorityState: "known",
	}))
	if err != nil {
		t.Fatalf("add over a stamped connection: %v", err)
	}
	view, ok := res.(RoadmapItemResult)
	if !ok {
		t.Fatalf("add answered %T, want RoadmapItemResult", res)
	}
	changes, err := s.RoadmapChangeList(context.Background(), view.Item.ID, 10)
	if err != nil {
		t.Fatalf("read the audit: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("the add wrote %d audit rows, want exactly 1", len(changes))
	}
	c := changes[0]
	if c.Op != "add" || c.ActorKind != "user" || c.ActorID != "4242" {
		t.Fatalf("the audit names %s/%s op %q, want user/4242 op add", c.ActorKind, c.ActorID, c.Op)
	}
}
