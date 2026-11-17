package tools

import "testing"

func TestDangerousToolsAreRegistered(t *testing.T) {
	reg := Registry()
	for _, name := range DangerousTools() {
		if _, ok := reg[name]; !ok {
			t.Errorf("%q is in the dangerous set but is not a registered tool — a rename must update danger.go", name)
		}
	}
	if !IsDangerousTool("bash") || !IsDangerousTool("write_file") || IsDangerousTool("read") {
		t.Fatal("bash and the write_file alias are dangerous, read is not")
	}
}

func TestEveryWriteToTheWorkspaceOrOutsideIsEitherDangerousOrADeliberateException(t *testing.T) {
	// A tool that spawns a process, writes the workspace or reaches the network must ask by default,
	// unless it is named here on purpose: a new tool that can do those things must be placed, not forgotten.
	deliberate := map[string]string{}
	for name := range Registry() {
		m, ok := MetaFor(name)
		if !ok {
			t.Errorf("tool %q has no declared meta", name)
			continue
		}
		for _, c := range m.Capabilities {
			if c == CapSpawnProcess || c == CapWorkspaceWrite || c == CapNetworkEgress {
				if !IsDangerousTool(name) && deliberate[name] == "" {
					t.Errorf("tool %q declares %s but is not in the dangerous set — add it to danger.go, or to this test's deliberate list with the reason", name, c)
				}
			}
		}
	}
}
