package daemon

// asyncworkers_test.go — S64 T4's daemon-side assertions: the manager is
// ONE value behind every door (the P34 rule graph got in S30, carried to
// the ledger), the socket's graph.build starts through it and returns in
// milliseconds, the row reaches its terminal truth, and the events reach
// a subscriber through the daemon's own publish path.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/protocol"
)

// TestAsyncWorkersIsOneValueBehindEveryDoor — P34's identity rule for the
// ledger: the seam on the table and the manager serve reaches are the
// SAME value, wired at the single construction point. Two managers would
// be two ledgers able to disagree about what is running, which is exactly
// the condition S64 exists to delete.
func TestAsyncWorkersIsOneValueBehindEveryDoor(t *testing.T) {
	st := openStoreForGraph(t)
	d := New(Options{Store: st})
	if d.Workers() == nil {
		t.Fatal("Workers() is nil with a store wired — the daemon has no ledger")
	}
	if d.Table.AsyncWorkers == nil {
		t.Fatal("Table.AsyncWorkers is nil with a store wired — the app has no ledger door")
	}
	a := reflect.ValueOf(d.Table.AsyncWorkers).Pointer()
	b := reflect.ValueOf(d.Workers()).Pointer()
	if a != b {
		t.Errorf("socket door holds %x, serve reaches %x — two managers where P34 demands one", a, b)
	}
	if got := d.Workers().Types(); len(got) != 1 || got[0] != "graph.build" {
		t.Errorf("registered worker types = %v, want [graph.build] — the graph runner is the first example of the slice", got)
	}
}

// TestGraphBuildStartsThroughTheSingleManager — the contract change,
// end to end: the call returns a handle promptly; the SAME manager serve
// drains walks the row to finished; the report in the row's result is
// the pre-S64 BuildReport verbatim; and the event log carries the
// lifecycle a subscribed app paints from.
func TestGraphBuildStartsThroughTheSingleManager(t *testing.T) {
	st := openStoreForGraph(t)
	// S54 T1 enumerated adaptation: graph.build is an audited operation
	// door — its dispatch needs a recorded peer credential behind the
	// connection (the manager identity half below is unchanged).
	d := New(Options{Store: st, Connections: testPeerConns("c-aw")})
	ctx := context.Background()
	initConn(t, d, ctx, "c-aw", "aw-test")

	root := writeTinyGoProject(t)
	res, err := d.Table.Dispatch(ctx, "c-aw", protocol.MethodGraphBuild,
		mustJSON(t, protocol.GraphBuildParams{ComponentRoot: root}))
	if err != nil {
		t.Fatalf("graph.build refused: %v", err)
	}
	acc, ok := res.(*protocol.GraphBuildAccepted)
	if !ok {
		t.Fatalf("graph.build answered %T, want *protocol.GraphBuildAccepted — the handle contract", res)
	}
	if !acc.Started || acc.WorkerID == "" {
		t.Fatalf("handle %+v, want a started worker with an id", acc)
	}
	if len(acc.WorkerID) != 35 || acc.WorkerID[:3] != "aw_" {
		t.Errorf("worker id %q does not carry the ledger's shape (aw_ + 32 hex)", acc.WorkerID)
	}

	view := waitWorkerTerminal(t, d, acc.WorkerID, 30*time.Second)
	if view.State != "finished" {
		t.Fatalf("worker ended %s (%s), want finished", view.State, view.Error)
	}
	var rep graph.BuildReport
	if err := json.Unmarshal(view.Result, &rep); err != nil {
		t.Fatalf("row result does not decode as the BuildReport the socket used to return inline: %v", err)
	}
	if rep.Root == "" || rep.Nodes < 1 {
		t.Errorf("report %+v — the worker built nothing", rep)
	}

	// The events a subscribed app paints from, through the daemon's own
	// publish (durable log + fan-out): started then finished, in that
	// order, nothing invented between them.
	kinds := waitWorkerEventKinds(t, d, flightBudget)
	if len(kinds) < 2 || kinds[0] != protocol.KindAsyncWorkerStarted || kinds[len(kinds)-1] != protocol.KindAsyncWorkerFinished {
		t.Errorf("event kinds = %v, want started … finished", kinds)
	}
}

// TestAsyncWorkersAbsentWithoutStore — the storeless mount leaves the
// family out of the table (V13) and graph.build refuses by naming the
// manager, never by running a ten-minute walk on the caller's socket.
func TestAsyncWorkersAbsentWithoutStore(t *testing.T) {
	d := New(Options{})
	if d.Workers() != nil {
		t.Fatal("storeless daemon built a manager — there is no ledger to keep rows in")
	}
	if d.Table.AsyncWorkers != nil {
		t.Fatal("storeless daemon wired the seam")
	}
	for _, name := range []protocol.MethodName{"asyncworkers.list", "asyncworkers.get", "asyncworkers.cancel"} {
		for _, m := range append(d.Table.Methods(), d.Table.PublicMethods()...) {
			if m == name {
				t.Errorf("%s is on a storeless table — absent, not registered-and-refusing (V13)", name)
			}
		}
	}
}

func writeTinyGoProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := "package main\n\nfunc helper() int { return 42 }\n\nfunc main() { _ = helper() }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func waitWorkerTerminal(t *testing.T, d *Daemon, workerID string, budget time.Duration) *asyncworkers.View {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		v, err := d.Workers().GetView(workerID)
		if err != nil {
			t.Fatalf("get %s: %v", workerID, err)
		}
		if v.State != "queued" && v.State != "running" {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker %s still %s after %s", workerID, v.State, budget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func workerEventKinds(d *Daemon) []string {
	var out []string
	for _, e := range d.Table.Events.Since(0, protocol.Filter{Kinds: []string{"asyncworker.*"}}) {
		out = append(out, e.Kind)
	}
	return out
}

// waitWorkerEventKinds reads the worker event log once the terminal event
// can be there. The manager writes the terminal ROW first and publishes the
// event right after (asyncworkers.Manager.finish — the durable order), so a
// test that watched the row reach its state can still land its first read of
// the log inside that publish window. The row and the log are the same
// store's truth a beat apart; the assertions below stay exactly as they were,
// only the read is given the same settling budget waitWorkerTerminal gives
// the row.
func waitWorkerEventKinds(t *testing.T, d *Daemon, budget time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		kinds := workerEventKinds(d)
		if len(kinds) >= 2 || time.Now().After(deadline) {
			return kinds
		}
		time.Sleep(20 * time.Millisecond)
	}
}
