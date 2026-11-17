package daemon

// The vocabulary door's own tests (S47 T10, TC3 + TC10).
//
// The claim this file holds the door to is narrow and worth stating exactly:
// `tools.vocabulary` sends the WORDS a catalog screen renders, and every word it
// sends is one the code declares and the list can answer for. Two failures are
// being prevented, and they are opposites: a screen inventing a capability
// sentence the code never said (prevented by the door existing and these tests
// pinning its source), and a door offering a filter value the catalog then
// answers with nothing (prevented by the pair assertions below, which read the
// list for real rather than trusting the vocabulary's good intentions).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/tools"
)

// TestToolsVocabularyIsTheClosedCapabilityVocabulary pins the capability half to
// the code's own vocabulary: same names, same order, one note each, and a
// meaning that is a sentence the code wrote — not the capability's own name
// echoed back, which is what a stub looks like from the wire.
func TestToolsVocabularyIsTheClosedCapabilityVocabulary(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	v, err := env.svc.ToolsVocabulary(context.Background())
	if err != nil {
		t.Fatalf("tools.vocabulary: %v", err)
	}
	want := tools.Capabilities()
	if len(want) < 10 {
		t.Fatalf("the closed vocabulary is %d capabilities — a collapsed vocabulary would let this test pass on nothing", len(want))
	}
	if len(v.Capabilities) != len(want) {
		t.Fatalf("the door sent %d capabilities, the vocabulary declares %d", len(v.Capabilities), len(want))
	}
	for i, c := range want {
		got := v.Capabilities[i]
		if got.Name != string(c) {
			t.Errorf("capability %d is %q, want %q (sorted by name, the same order Capabilities() gives)", i, got.Name, c)
		}
		if strings.TrimSpace(got.Meaning) == "" {
			t.Errorf("%s arrives with no meaning — the screen would render a pill with nothing to read", got.Name)
		}
		if got.Meaning == got.Name || strings.Contains(got.Meaning, "_") {
			t.Errorf("%s: meaning %q echoes the wire name instead of saying what it permits", got.Name, got.Meaning)
		}
		if !strings.HasSuffix(got.Meaning, ".") {
			t.Errorf("%s: meaning %q is not a sentence", got.Name, got.Meaning)
		}
		if got.Meaning != tools.CapabilityMeaning(c) {
			t.Errorf("%s: meaning %q is not the code's %q", got.Name, got.Meaning, tools.CapabilityMeaning(c))
		}
	}
}

// TestToolsVocabularyOffersOnlyFamiliesTheListCanAnswer is the pair property the
// filter depends on, read in both directions against a real list: every family
// the door offers returns at least one row whose family IS that value, and every
// family the unfiltered list carries is offered. The MCP half is asserted by
// name because it is the interesting case — an MCP tool's family is its server,
// so the enabled `acme` is a family and the DISABLED `off` is not (its tools
// cannot be called by anyone, and a filter that offered it would answer nothing).
func TestToolsVocabularyOffersOnlyFamiliesTheListCanAnswer(t *testing.T) {
	ctx := context.Background()
	env := newToolsCatalogEnv(t, true)
	v, err := env.svc.ToolsVocabulary(ctx)
	if err != nil {
		t.Fatalf("tools.vocabulary: %v", err)
	}
	offered := map[string]bool{}
	for i, f := range v.Families {
		if f == "" {
			t.Fatalf("family %d is empty — an empty option is a screen offering \"nothing\" as a choice", i)
		}
		if i > 0 && v.Families[i-1] > f {
			t.Fatalf("families are not sorted (%q after %q) — a select that reorders between refreshes jumps under the cursor", f, v.Families[i-1])
		}
		if offered[f] {
			t.Fatalf("family %q is offered twice", f)
		}
		offered[f] = true
	}

	all := mustList(t, env, protocol.ToolsListFilter{})
	if len(all) == 0 {
		t.Fatal("the unfiltered list is empty — this test would pass on nothing")
	}
	for _, e := range all {
		if !offered[e.Family] {
			t.Errorf("the list returns family %q (%s) which the vocabulary does not offer — a person could never filter to a row they can see", e.Family, e.Name)
		}
	}
	for _, f := range v.Families {
		fam := f
		rows := mustList(t, env, protocol.ToolsListFilter{Family: &fam})
		if len(rows) == 0 {
			t.Errorf("the vocabulary offers family %q and the list answers it with nothing — a filter option that matches no row is a lie in a select", f)
		}
		for _, r := range rows {
			if r.Family != f {
				t.Errorf("family %q returned %s of family %q", f, r.Name, r.Family)
			}
		}
	}
	if !offered["acme"] {
		t.Errorf("the enabled server `acme` is not a family — its tools are in the list under that family (%v)", v.Families)
	}
	if offered["off"] {
		t.Error("the DISABLED server `off` is offered as a family; its tools are in no list, so the filter it feeds would answer nothing")
	}
}

// TestToolsVocabularySourcesFollowTheBuild is V13 on the vocabulary: `mcp` is a
// word the wire knows, but it is only offered when this build can actually
// render an MCP tool. A build with no MCP seam offers `system` alone — and the
// list agrees, which is the half that makes the omission an answer rather than a
// hidden fact.
func TestToolsVocabularySourcesFollowTheBuild(t *testing.T) {
	ctx := context.Background()

	withMCP := newToolsCatalogEnv(t, true)
	v, err := withMCP.svc.ToolsVocabulary(ctx)
	if err != nil {
		t.Fatalf("tools.vocabulary (mcp wired): %v", err)
	}
	if strings.Join(v.Sources, ",") != "mcp,system" {
		t.Errorf("sources with the MCP seam = %v, want [mcp system]", v.Sources)
	}

	without := newToolsCatalogEnv(t, false)
	v2, err := without.svc.ToolsVocabulary(ctx)
	if err != nil {
		t.Fatalf("tools.vocabulary (no mcp seam): %v", err)
	}
	if strings.Join(v2.Sources, ",") != "system" {
		t.Errorf("sources without the MCP seam = %v, want [system] — offering `mcp` here would send the person to the daemon's no-match sentence for a choice the vocabulary itself invented", v2.Sources)
	}
	for _, s := range v2.Sources {
		src := s
		if len(mustList(t, without, protocol.ToolsListFilter{Source: &src})) == 0 {
			t.Errorf("source %q is offered and answers nothing", s)
		}
	}
	mcp := protocol.ToolsSourceMCP
	if rows := mustList(t, without, protocol.ToolsListFilter{Source: &mcp}); len(rows) != 0 {
		t.Errorf("the MCP-less build returned %d MCP tools — the vocabulary was right and the list was not", len(rows))
	}
}

// TestToolsVocabularyRerunClassesAreTheCommittedThree pins the class half to the
// code's closed set rather than to a count: the same three spellings
// `internal/tools` declares, which the committed `rerun_classes.json` pins for
// the rows. A fourth class has to arrive in the code before it can arrive here.
func TestToolsVocabularyRerunClassesAreTheCommittedThree(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	v, err := env.svc.ToolsVocabulary(context.Background())
	if err != nil {
		t.Fatalf("tools.vocabulary: %v", err)
	}
	want := make([]string, 0, 3)
	for _, c := range tools.RerunClasses() {
		want = append(want, string(c))
	}
	if strings.Join(v.RerunClasses, ",") != strings.Join(want, ",") {
		t.Errorf("rerunClasses = %v, want the code's %v", v.RerunClasses, want)
	}
}

// TestToolsVocabularyIsLiveNotMemoised is N8 on this door: enabling a second
// server changes the NEXT answer, with no restart and no pause. The alternative
// is a screen whose filter list is a photograph of the world from whenever the
// first panel opened.
func TestToolsVocabularyIsLiveNotMemoised(t *testing.T) {
	ctx := context.Background()
	env := newToolsCatalogEnv(t, true)
	first, err := env.svc.ToolsVocabulary(ctx)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	for _, f := range first.Families {
		if f == "second" {
			t.Fatal("the fixture world already has a `second` server — the test measures nothing")
		}
	}

	env.admin.servers.Servers = append(env.admin.servers.Servers, protocol.MCPServerView{
		Name: "second", Tier: "public", Enabled: true, Origin: "user",
		Tools: []protocol.MCPToolView{{Name: "ping", Rerun: "safe", Effect: "local", Classified: true}},
	})

	second, err := env.svc.ToolsVocabulary(ctx)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	found := false
	for _, f := range second.Families {
		if f == "second" {
			found = true
		}
	}
	if !found {
		t.Errorf("the newly enabled server is not a family after the enabling — the vocabulary memoised the old world (%v)", second.Families)
	}
	if strings.Join(first.Families, ",") == strings.Join(second.Families, ",") {
		t.Error("both reads answered identically — the second read never saw the change")
	}
}

// TestToolsVocabularyFailsRatherThanAnsweringHalf is V11 on this door: when the
// MCP half cannot be read, the vocabulary ERRORS rather than answering with the
// system half's families and leaving the screen to render a catalogue it believes
// is complete. A half vocabulary is not a smaller vocabulary; it is a wrong one.
func TestToolsVocabularyFailsRatherThanAnsweringHalf(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	env.admin.failWith = errors.New("the mcp half is unreachable")
	v, err := env.svc.ToolsVocabulary(context.Background())
	if err == nil {
		t.Fatalf("the door answered with a broken MCP half: %v", v)
	}
	if len(v.Families) != 0 || len(v.Sources) != 0 {
		t.Errorf("a failed read still carried families/sources: %v / %v — V11 says an error carries nothing", v.Families, v.Sources)
	}
}
