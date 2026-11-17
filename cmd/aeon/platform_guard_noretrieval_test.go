//go:build noretrieval

package main

// platform_guard_noretrieval_test.go — S54 T3, the D1 mirror of the door
// matrix. With the retrieval plane compiled out, its two doors (reindex and
// its plan preview) are ABSENT — not refusing — while the panel keeps its
// honest shape: platform.status still registers and renders retrieval as
// unwired, because the store panel exists in every build. That is the SP1
// claim about a variant nobody ships by accident.

import (
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestPlatformDoorMatrixPerVariant(t *testing.T) {
	d := blueprintGuardDaemon(t) // store-wired; no retrieval link exists in this build

	for _, m := range []protocol.MethodName{protocol.MethodRetrievalReindex, protocol.MethodRetrievalReindexPlan} {
		if _, ok := d.Table.Lookup(m); ok {
			t.Errorf("%s registers in a build with no retrieval plane — an absent seam must make the method ABSENT, not a door that can only refuse", m)
		}
	}
	for _, m := range []protocol.MethodName{
		protocol.MethodGraphBuild, protocol.MethodGraphReparse, protocol.MethodGraphDelete,
		protocol.MethodPlatformStatus, protocol.MethodPlatformChanges,
	} {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Errorf("%s is missing in the noretrieval build — the panel and the graph plane do not depend on the retrieval tag", m)
			continue
		}
		if !got.RequiresInitialize || got.Debug {
			t.Errorf("%s flags = RequiresInitialize:%v Debug:%v, want the human-door pair", m, got.RequiresInitialize, got.Debug)
		}
	}
}
