package narrow

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// S38 T15 (OL10/OL11): the catalog's projection and the three decide doors
// are SOCKET methods — the narrow mountings (HTTP, MCP-stdio) must not gain
// a second half of either door. The projection enlarges the socket table
// every run a Spec is added; this guard makes the narrow eight a fact a
// reviewer sees widen, never a side effect.

func TestNarrowTableHasNoCatalogOrDecideAction(t *testing.T) {
	catalogMethods := map[string]bool{}
	for _, sp := range records.Catalog() {
		catalogMethods[sp.Method] = true
	}
	acts := Actions()
	if len(acts) != 9 {
		t.Fatalf("narrow mounts %d actions, want 9 (the S21 T6 completion raised 8 to 9) — the projection must not widen the narrow table silently", len(acts))
	}
	// The pre-existing overlaps are reads whose socket doors the narrow
	// mounting carried long before the catalog gave them a twin — the
	// catalog followed the socket, not the other way round. loop.list: S30's
	// read mounted by S19, twin arriving with S38 T9. blueprint.validate:
	// the socket method and its aeon_blueprint_validate action shipped with
	// S48 T1 (blamed 635d2e24, 2026-10-27) and the catalog twin followed on
	// 2026-11-03 with S48 T10 (a8759842), which broke this guard on main;
	// S49 T5's full narrow run caught it and fixes it forward here, in the
	// lane the twin's own slice would have taken. Anything ELSE from the
	// catalog appearing in this table is the drift this guard exists to
	// catch.
	allowedOverlap := map[string]bool{"loop.list": true, "blueprint.validate": true}
	for _, a := range acts {
		m := string(a.SocketMethod)
		if catalogMethods[m] && !allowedOverlap[m] {
			t.Errorf("%s projects %s, a catalog method — the records doors are socket-only (T15)", a.Name, m)
		}
		if strings.HasSuffix(m, ".decide") {
			t.Errorf("%s projects %s — a decision reaches the narrow surface, and a review is a human act on the socket (OL11)", a.Name, m)
		}
		for _, forbidden := range []string{"loop.message", "loop.run", "trigger.add", "trigger.fireNow", "trigger.pause", "trigger.resume"} {
			if m == forbidden {
				t.Errorf("%s projects %s — a write the narrow mountings were ruled out of (N12/N16)", a.Name, m)
			}
		}
	}
}
