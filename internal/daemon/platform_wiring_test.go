package daemon

// platform_wiring_test.go — S54 T1: the daemon is where the four
// derived-store operation doors get their person. The registration half
// is V13 (seam AND ActorFor); the audit half is the kernel's uid riding
// the row the store commits. A transport that never recorded the peer is
// uid 0, and uid 0 is refused, not dressed in a real-looking identity.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/protocol"
)

// TestDaemonWiresTheOperationDoorsOverItsPeerCredential — the store-
// backed daemon: ActorFor exists, the three graph operation doors are in
// the table (their seams are the store), graph.build answered by the
// manager, and the audit row it left names user:<the uid the transport
// reported> — the number the kernel said, not one the params carried.
func TestDaemonWiresTheOperationDoorsOverItsPeerCredential(t *testing.T) {
	st := openStoreForGraph(t)
	conns := []localrpc.ConnInfo{{ID: "c-plat", PeerUID: t15PeerUID, PeerPID: 7}}
	d := New(Options{Store: st, Connections: func() []localrpc.ConnInfo { return conns }})
	ctx := context.Background()
	if d.Table.ActorFor == nil {
		t.Fatal("ActorFor unwired on a store-backed daemon — every operation door would be absent")
	}
	for _, name := range []protocol.MethodName{protocol.MethodGraphBuild, protocol.MethodGraphReparse, protocol.MethodGraphDelete} {
		found := false
		for _, m := range append(d.Table.Methods(), d.Table.PublicMethods()...) {
			if m == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s absent from the daemon's table — the human's derived-store door is wired out of the product", name)
		}
	}

	initConn(t, d, ctx, "c-plat", "platform-test")
	root := writeTinyGoProject(t)
	res, err := d.Table.Dispatch(ctx, "c-plat", protocol.MethodGraphBuild,
		mustJSON(t, protocol.GraphBuildParams{ComponentRoot: root}))
	if err != nil {
		t.Fatalf("graph.build refused: %v", err)
	}
	acc := res.(*protocol.GraphBuildAccepted)
	view := waitWorkerTerminal(t, d, acc.WorkerID, 30*time.Second)
	if view.State != "finished" {
		t.Fatalf("worker ended %s (%s)", view.State, view.Error)
	}

	rows, err := st.PlatformChangeList(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d audit rows for one daemon-run build, want 1", len(rows))
	}
	row := rows[0]
	abs, _ := filepath.Abs(root)
	if row.Op != "graph_build" || row.Subject != abs {
		t.Errorf("audit composed wrong end to end: %+v", row)
	}
	if row.ActorKind != "user" || row.ActorID != "4321" {
		t.Errorf("audit actor = %s:%s, want user:4321 — the uid the fake transport reported", row.ActorKind, row.ActorID)
	}
	// The starter answers "started by WHO" off the worker the door named.
	starter, err := st.PlatformChangeStarter(ctx, acc.WorkerID)
	if err != nil || starter == nil || starter.ActorID != "4321" {
		t.Errorf("the starter lookup answered %+v (%v) — the app's already-running line has no name", starter, err)
	}
}

// TestPlatformOperationRefusesAnUnrecordedPeer — the same door on the
// same daemon when the transport never recorded the connection: uid 0 is
// the kernel's silence, and silence does not press buttons. Nothing
// queued, nothing audited.
func TestPlatformOperationRefusesAnUnrecordedPeer(t *testing.T) {
	st := openStoreForGraph(t)
	d := New(Options{Store: st, Connections: func() []localrpc.ConnInfo { return nil }})
	ctx := context.Background()
	initConn(t, d, ctx, "c-zero", "platform-test")
	_, err := d.Table.Dispatch(ctx, "c-zero", protocol.MethodGraphBuild,
		mustJSON(t, protocol.GraphBuildParams{ComponentRoot: writeTinyGoProject(t)}))
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != -31956 {
		t.Fatalf("uid-0 peer dispatch refused with %v, want -31956", err)
	}
	rows, _ := st.PlatformChangeList(ctx, "", 0)
	if len(rows) != 0 {
		t.Fatalf("the refused operation left %d audit rows", len(rows))
	}
}

// testPeerConns is the Connections shim the audited-door fixtures share:
// each named connection gets a recorded peer credential with the same
// honest nonzero uid (the kernel's stand-in — S54 T1). A fixture that
// dispatches graph.build/reparse/delete or retrieval.reindex needs it the
// way any real app connection gets one from the transport; a fixture that
// wants the uid-0 refusal passes no Connections at all.
func testPeerConns(ids ...string) func() []localrpc.ConnInfo {
	conns := make([]localrpc.ConnInfo, 0, len(ids))
	for _, id := range ids {
		conns = append(conns, localrpc.ConnInfo{ID: id, PeerUID: t15PeerUID, PeerPID: 7})
	}
	return func() []localrpc.ConnInfo { return conns }
}
