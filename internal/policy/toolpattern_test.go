package policy

import "testing"

func TestMatchToolAddress(t *testing.T) {
	for _, c := range []struct {
		pattern, address string
		want             bool
	}{
		{"workspace.*.read", "workspace.glob.read", true},
		{"workspace.*.read", "workspace.write.write", false},
		{"*.*.read", "workspace.read.read", true},
		{"*.*.read", "egress.webfetch.write", false},
		{"workspace.*.*", "workspace.edit.write", true},
		{"workspace.read.read", "workspace.read.read", true},
		{"workspace.read.read", "workspace.read.write", false},
		{"*.*.read", "mcp.github.search.read", false}, // three segments never reach an MCP tool
		{"mcp.*.*.*", "mcp.github.search.read", true},
		{"mcp.github.*.read", "mcp.github.search.read", true},
		{"mcp.github.*.read", "mcp.other.search.read", false},
		{"mcp.*.*.*", "workspace.read.read", false}, // four segments only match MCP
		{"workspace.*.read", "", false},
		{"", "workspace.read.read", false},
		{"bash", "workspace.bash.write", false}, // a bare name is not an address pattern
		{"wo*.*.read", "workspace.glob.read", false},
		{"workspace..read", "workspace.glob.read", false},
	} {
		if got := MatchToolAddress(c.pattern, c.address); got != c.want {
			t.Errorf("MatchToolAddress(%q, %q) = %v, want %v", c.pattern, c.address, got, c.want)
		}
	}
}

func TestValidateToolPattern(t *testing.T) {
	for _, ok := range []string{"bash", "server.tool", "workspace.*.read", "*.*.*", "mcp.github.*.write", "workspace.read.read"} {
		if s := ValidateToolPattern(ok); s != "" {
			t.Errorf("%q refused: %s", ok, s)
		}
	}
	for _, bad := range []string{"wo*.*.read", "workspace..read", "workspace.*.rea*", "a.b.c.d.read", "workspace.*.execute", "*", "workspace.*", "x.y.z.read"} {
		if bad == "*.*" {
			// two segments with a star is an address form of the wrong length
			if ValidateToolPattern(bad) == "" {
				t.Errorf("%q accepted", bad)
			}
			continue
		}
		if ValidateToolPattern(bad) == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}

func FuzzMatchToolAddress(f *testing.F) {
	f.Add("workspace.*.read", "workspace.glob.read")
	f.Fuzz(func(t *testing.T, p, a string) {
		got := MatchToolAddress(p, a) // must not panic
		if got && (p == "" || a == "") {
			t.Fatal("empty side matched")
		}
	})
}
