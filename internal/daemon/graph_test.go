package daemon

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// graph_test.go — S30 T4/T5's daemon-side wiring assertions: the ONE
// service identity rule, the seam's single production caller, and the
// write-hook composition at both step-runner literals.

func openStoreForGraph(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestGraphToolAndSocketShareOneService — P34's identity rule for the
// graph door: the `graph` tool's seam and the socket's Table.Graph are
// the SAME value (same pointer through the two interface wrappers),
// wired at the single construction point in New. Two distinct services
// would be two resolvers able to drift, which is exactly what the
// amendment exists to delete.
func TestGraphToolAndSocketShareOneService(t *testing.T) {
	st := openStoreForGraph(t)
	d := New(Options{Store: st})
	if d.Table.Graph == nil {
		t.Fatal("Table.Graph is nil with a store wired — the app has no graph door")
	}
	if !tools.GraphRegistered() {
		t.Fatal("the graph tool is absent with a store wired — the agent has no graph door")
	}
	a := reflect.ValueOf(d.Table.Graph).Pointer()
	b := reflect.ValueOf(tools.GraphService()).Pointer()
	if a != b {
		t.Errorf("socket door holds %x, tool seam holds %x — two services where P34 demands one", a, b)
	}
}

// TestGraphServiceHasExactlyOneProductionCaller — the seam's widening
// guard (search.SetSearchResolver's precedent): one non-test call site
// in the whole tree.
func TestGraphServiceHasExactlyOneProductionCaller(t *testing.T) {
	root := ".."
	var sites []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(src), "tools.SetGraphService(") {
			sites = append(sites, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || !strings.HasSuffix(sites[0], "daemon.go") {
		t.Errorf("SetGraphService production call sites = %v, want exactly daemon.go — the seam widens only in a reviewed diff", sites)
	}
}

// TestGraphWriteHookWiredAtBothStepRunners — the two StepRunner literals
// must BOTH carry the chained hook (the same rule the S22 hook has: a
// resume path that forgot the subscription silently stops updating the
// index). Textual pinning of the composition, the way the policy-gate
// wiring test pins its trio. The hook is the daemon's notifyReindex closure —
// the door that starts retrieval.reindex through the async-worker manager —
// so the grep pins the ASYNC approach at both literals, not the old
// reindexTracker trigger it replaced.
func TestGraphWriteHookWiredAtBothStepRunners(t *testing.T) {
	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "chainHooks(notifyReindex, notifyGraph)"); n != 2 {
		t.Errorf("the retrieval write hook is chained %d times, want 2 (first run + resume)", n)
	}
}

// TestChainHooksIsolatesSubscribers — V18's rule made executable: the
// step never waits (subscribers run async — asserted by returning while
// a subscriber sleeps) and a panicking subscriber poisons nothing.
func TestChainHooksIsolatesSubscribers(t *testing.T) {
	var hits int32
	var mu sync.Mutex
	done := make(chan struct{})
	h := chainHooks(
		nil, // the nil seam (no reindex wired) must not break the chain
		func(root, path string) {
			panic("subscriber with a defect")
		},
		func(root, path string) {
			mu.Lock()
			hits++
			mu.Unlock()
			close(done)
		},
	)
	start := time.Now()
	h("/r", "/r/f.go") // must return promptly despite the panicking subscriber
	if time.Since(start) > time.Second {
		t.Error("the hook waited on its subscribers — the step contract is fire-and-forget (V18)")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the healthy subscriber never ran — the panicking one poisoned the chain")
	}
}
