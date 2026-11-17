package daemon

// S74 T1: the tool address rides every catalog entry, computed from the same
// registry the rows come from.

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func TestEveryCatalogEntryCarriesItsAddress(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	all := mustList(t, env, protocol.ToolsListFilter{})
	byName := map[string]protocol.ToolEntry{}
	for _, e := range all {
		byName[e.Name] = e
		if e.Address == "" || e.Category == "" || e.Access == "" {
			t.Errorf("%s has no address: %+v", e.Name, [3]string{e.Category, e.Access, e.Address})
			continue
		}
		if e.Access != "read" && e.Access != "write" {
			t.Errorf("%s access %q is outside read|write", e.Name, e.Access)
		}
		if !strings.HasSuffix(e.Address, "."+e.Access) || !strings.HasPrefix(e.Address, e.Category+".") {
			t.Errorf("%s address %q disagrees with category %q / access %q", e.Name, e.Address, e.Category, e.Access)
		}
	}
	for name, want := range map[string]string{
		"glob":     "workspace.glob.read",
		"read":     "workspace.read.read",
		"write":    "workspace.write.write",
		"webfetch": "egress.webfetch.write", // S74 Q1: the network is a write
	} {
		if got := byName[name].Address; got != want {
			t.Errorf("%s address %q, want %q", name, got, want)
		}
	}
}
