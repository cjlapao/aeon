package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// A loop is only deleted when the person typed its slug and it is not running; hiding one is refused while it runs.
func TestDeleteNeedsTheSlugAndAStoppedLoop(t *testing.T) {
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.CreateLoop(store.NewLoop{Slug: "alpha", ComponentRoot: "/r", Title: "a", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: s}
	ctx := context.Background()
	actor := protocol.Actor{Kind: "user", ID: "me"}

	if _, err := d.deleteLoop(ctx, actor, protocol.LoopDeleteParams{ID: l.ID, Confirm: "wrong"}); err == nil {
		t.Fatal("a delete whose confirm is not the slug must be refused")
	}
	if _, err := s.ReadLoopByID(l.ID); err != nil {
		t.Fatal("a refused delete must leave the loop")
	}
	sc := store.Scope{LoopSlug: "alpha", ComponentRoot: "/r"}
	if err := s.SetLoopStatus(sc, "running"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.deleteLoop(ctx, actor, protocol.LoopDeleteParams{ID: l.ID, Confirm: "alpha"}); err == nil {
		t.Fatal("a running loop must not be deleted")
	}
	if _, err := d.setLoopArchived(ctx, protocol.LoopSetArchivedParams{ID: l.ID, Archived: true}); err == nil {
		t.Fatal("a running loop must not be archived")
	}
	// A stopped loop passes the running guard (the removal itself is the store's, tested there).
	if err := s.SetLoopStatus(sc, "blocked"); err != nil {
		t.Fatal(err)
	}
	if err := d.notRunning(l); err != nil {
		t.Fatalf("a blocked loop is not running: %v", err)
	}
}

// loop.delete through the real dispatch path, on a daemon built over a real store: the slug confirms it, the loop
// goes, and the door answers with its tombstone.
func TestLoopDeleteThroughTheSocketDoor(t *testing.T) {
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	l, err := st.CreateLoop(store.NewLoop{Slug: "tee-walk", Root: "/repo/tee", ComponentRoot: "/repo/tee", Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := newTestDaemonWithStore(t, st)
	raw, _ := json.Marshal(protocol.LoopDeleteParams{ID: l.ID, Confirm: "tee-walk", Note: "test"})
	got, err := d.Table.Dispatch(context.Background(), "c1", protocol.MethodLoopDelete, raw)
	if err != nil {
		t.Fatalf("the delete door refused: %v", err)
	}
	res, ok := got.(protocol.LoopDeleteResult)
	if !ok || res.Tombstone.Slug != "tee-walk" {
		t.Fatalf("unexpected result %T %+v", got, got)
	}
	if _, err := st.ReadLoopByID(l.ID); err == nil {
		t.Fatal("the loop must be gone")
	}
}
