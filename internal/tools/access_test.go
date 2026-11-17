package tools

import "testing"

// Every registered tool has an expected access. A tool added without a row
// here fails the suite rather than arriving with an unexamined answer (TA2).
var expectedAccess = map[string]string{
	"read": AccessRead, "list": AccessRead, "glob": AccessRead, "grep": AccessRead,
	"write": AccessWrite, "edit": AccessWrite, "bash": AccessWrite,
	"webfetch": AccessWrite,
	"question": AccessRead, "todo": AccessRead, "report_size": AccessRead,
	"ask_agent": AccessWrite, "run_start": AccessWrite,
	"search": AccessRead, "graph": AccessRead,
	"doc_write": AccessWrite, "doc_edit": AccessWrite, "doc_read": AccessRead, "doc_list": AccessRead, "loop_context": AccessRead,
	"env_list": AccessRead, "env_get": AccessRead,
}

func TestEveryToolHasAnExpectedAccess(t *testing.T) {
	records := recordMeta()
	for name, meta := range AllMeta() {
		want, ok := expectedAccess[name]
		if !ok {
			// Only the projected records tools are classified by the rule alone;
			// a hand-written tool with no row fails here.
			if _, projected := records[name]; !projected {
				t.Errorf("core tool %q has no expected-access row in access_test.go", name)
			}
			continue
		}
		if got := AccessOf(meta.Capabilities); got != want {
			t.Errorf("tool %q: access %q, want %q (caps %v)", name, got, want, meta.Capabilities)
		}
	}
}

func TestAccessOfEdgeCases(t *testing.T) {
	if AccessOf(nil) != AccessRead {
		t.Error("no capabilities must be read")
	}
	if AccessOf([]Capability{CapAsksHuman}) != AccessRead {
		t.Error("asking a person is a read")
	}
	if AccessOf([]Capability{CapWorkspaceRead, CapNetworkEgress}) != AccessWrite {
		t.Error("network egress is a write (S74 Q1)")
	}
	if got := SystemAddress("workspace", "glob", []Capability{CapWorkspaceRead}); got != "workspace.glob.read" {
		t.Errorf("address: %q", got)
	}
}
