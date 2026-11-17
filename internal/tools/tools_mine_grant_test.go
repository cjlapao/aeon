package tools

// tools_mine_grant_test.go — S47 T7 (TC9, TC1, 2026-11-02): the two own-view
// tools are REGISTERED and GRANTED TO NO ONE.
//
// `TestRegistryExactNames` owns the exact list and the journal; this test owns the
// half that a name list cannot say. A name in the registry means the model can be
// *offered* the tool once something grants it; a name in `DefaultGrant()` means
// every agent holds it without anybody deciding. The two own-view doors are the
// kind of tool an operator should hand out deliberately — they answer SELF, so
// granting one to an agent tells that agent what it may do, which is exactly the
// knowledge an operator may want to withhold from a sandboxed one — and TC1's
// "S47 ships zero write tools" is checked here at the registry rather than only at
// the catalog, because the registry is what the runtime dispatches against.

import (
	"strings"
	"testing"
)

func TestNewToolsNotInDefaultGrantAndRegistryCounted(t *testing.T) {
	reg := Registry()
	for _, name := range []string{"tools_mine", "tool_show"} {
		tool, ok := reg[name]
		if !ok {
			t.Fatalf("%s is not registered — it is a catalog spec and `recordTools` projects every spec; a missing name means the projection was bypassed", name)
		}
		if tool.Class != RerunSafe {
			t.Errorf("%s class = %q, want safe — TC1: this slice ships no write tool, and the class is what the runtime reruns on", name, tool.Class)
		}
		if strings.TrimSpace(tool.Spec.Description) == "" {
			t.Errorf("%s has no description — the model chooses tools by reading it", name)
		}
		for _, granted := range DefaultGrant() {
			if granted == name {
				t.Errorf("%s is in DefaultGrant() — the own-view doors are handed out by a row, never by the default (the default grant stays read, list, write)", name)
			}
		}
	}
	// The default grant itself is unchanged, stated as bytes: a slice that adds
	// tools must not be able to widen what every agent holds as a side effect.
	if got := strings.Join(DefaultGrant(), ","); got != "read,list,write" {
		t.Fatalf("DefaultGrant() = %q, want read,list,write — S47 adds tools and grants none of them", got)
	}
	// And the count is the one the journal says, measured here rather than
	// remembered: 68 honest names before S47 T7, +2 for the own-view pair.
	if got := len(ToolNames()); got < 70 {
		t.Fatalf("ToolNames() = %d, want at least 70 (68 before S47 T7 + the own-view pair)", got)
	}
}
