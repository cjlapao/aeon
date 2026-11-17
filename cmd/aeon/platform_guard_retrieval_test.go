//go:build !noretrieval

package main

// platform_guard_retrieval_test.go — S54 T3, the retrieval-wired variant of
// the door matrix. The same test name lives in the noretrieval sibling with
// the opposite halves: the registration claim is about a variant, so it is
// answered in both builds rather than only where it is easy to check.

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
)

// platformWiredDaemon is the daemon serve would build for the retrieval
// plane: the one store, the one keyword-only link (routes nil — the seams
// still set, which is what the doors gate on; the panel prices nothing and
// runs nothing from here).
func platformWiredDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	st := toolsRoadStore(t)
	dopts := daemon.Options{Store: st}
	wireRetrievalSearch(st, nil, &dopts)
	return daemon.New(dopts)
}

func TestPlatformDoorMatrixPerVariant(t *testing.T) {
	d := platformWiredDaemon(t)

	want := []protocol.MethodName{
		protocol.MethodRetrievalReindex,     // the SP3 operation
		protocol.MethodRetrievalReindexPlan, // its preview
		protocol.MethodGraphBuild,
		protocol.MethodGraphReparse,
		protocol.MethodGraphDelete,
		protocol.MethodPlatformStatus,  // SP6 reads, all human-door
		protocol.MethodPlatformChanges,
	}
	for _, m := range want {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Errorf("%s is not registered on the fully wired daemon", m)
			continue
		}
		if !got.RequiresInitialize || got.Debug {
			t.Errorf("%s flags = RequiresInitialize:%v Debug:%v, want the human-door pair", m, got.RequiresInitialize, got.Debug)
		}
	}

	// The full classification claim, unfiltered: on THIS variant every
	// platform door registers, so every one must carry its S47 entry.
	var mine []string
	for _, m := range want {
		mine = append(mine, string(m))
	}
	if bad := unclassifiedMethodOffenses(mine, d.Table.Surface()); len(bad) > 0 {
		t.Errorf("a platform method the wired daemon registers carries no classification:\n%s", strings.Join(bad, "\n"))
	}
}
