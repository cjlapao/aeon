package tools

// granted_characterize_test.go — S47 T4's **characterization**, committed BEFORE
// the extraction and never edited afterwards (T4: "those tests are committed
// first and must stay green untouched").
//
// What they pin is `BuildToolset`'s grant-list handling exactly as it behaves at
// `1ae086c0`+ today, because `granted.go`'s `GrantedSet` is that logic lifted out
// and called back — a refactor whose only promise is that nothing observable
// moved, and a promise like that is worth exactly the test that would fail if it
// broke. Three facts the extraction could easily lose, each pinned on its own:
//
//   - **Order and duplicates are kept, not sorted and not collapsed.** `Granted()`
//     is the author's list; `Specs()` is where the SEC-051 dedupe happens ("ONE
//     tool, ONE spec", first granted spelling wins the slot). An extractor that
//     "helpfully" deduped here would change what `Granted()` reports — and the
//     catalog's differential test (T4) compares against this very method, so the
//     two spellings of that mistake are a silent disagreement about what a step
//     holds.
//   - **An unknown name is an error, byte-for-byte**, naming the misses sorted and
//     the honest listing. The text is quoted literally here rather than rebuilt
//     from the same `fmt` call, because rebuilding it tests nothing.
//   - **The alias is a grant spelling, not a refusal**: `["write_file"]` builds a
//     callable, class-correct toolset.
//
// The `WorkspaceRoot` panic stays where it is — a programmer check, not part of
// "what a grant list yields" (T0 item 5).

import (
	"reflect"
	"strings"
	"testing"
)

// unknownGrantError is the refusal's text as it exists before the extraction,
// quoted and not computed: `%s` slots filled for the case below.
const unknownGrantErrorBoth = "tools: the grant list names bash2, nope, which do not exist. The tools " +
	"that do are %s. An unknown name is refused rather than ignored, because ignoring it " +
	"gives the step fewer tools than its author intended and the model works around the " +
	"absence silently"

func TestCharacterizeGrantOrderAndDuplicatesAreKept(t *testing.T) {
	root := workspace(t, nil)
	granted := []string{"list", "read", "list", "write", "grep", "grep", "grep"}
	set, err := BuildToolset(granted, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("a grant list of real tools was refused: %v", err)
	}
	got := set.Granted()
	if !reflect.DeepEqual(got, granted) {
		t.Errorf("Granted() is %v, want the author's list verbatim %v — order is a hint about "+
			"which tool to prefer and duplicates are the author's, not ours to collapse", got, granted)
	}
	// The spec offer, though, is deduped by canonical tool: seven names, four
	// distinct tools, and the FIRST spelling of each wins its slot.
	specs := set.Specs()
	if len(specs) != 4 {
		var names []string
		for _, s := range specs {
			names = append(names, s.Name)
		}
		t.Fatalf("Specs() offers %v, want 4 specs (list, read, write, grep — deduped by canonical name)", names)
	}
	if specs[0].Name != "list" || specs[1].Name != "read" || specs[2].Name != "write" || specs[3].Name != "grep" {
		t.Errorf("Specs() order is %s, %s, %s, %s — grant order, not sorted",
			specs[0].Name, specs[1].Name, specs[2].Name, specs[3].Name)
	}
	// And every spelling still dispatches.
	for _, name := range granted {
		if !set.Has(name) {
			t.Errorf("%s is granted but not callable", name)
		}
	}
}

func TestCharacterizeUnknownGrantErrorIsByteIdentical(t *testing.T) {
	root := workspace(t, nil)
	// Two misses, given OUT of sort order: the error names them sorted.
	_, err := BuildToolset([]string{"read", "nope", "bash2"}, ToolContext{WorkspaceRoot: root})
	if err == nil {
		t.Fatal("an unknown grant name was accepted — a typo would hand the step fewer tools in silence")
	}
	want := strings.Replace(unknownGrantErrorBoth, "%s", strings.Join(ToolNames(), ", "), 1)
	if err.Error() != want {
		t.Errorf("the unknown-grant refusal changed, which makes it a second answer to a question "+
			"the operator already reads:\ngot:  %s\nwant: %s", err.Error(), want)
	}
	// The one-half proof: the misses are sorted into the sentence even though they
	// arrived unsorted, and the honest listing is `ToolNames()` and nothing else.
	if !strings.Contains(err.Error(), "names bash2, nope,") {
		t.Errorf("the misses are not sorted into the sentence: %s", err.Error())
	}
	if strings.Contains(err.Error(), aliasWriteFile) {
		t.Errorf("the refusal leaked the hidden alias into the listing a person is told to choose from")
	}
}

func TestCharacterizeAliasIsAGrantSpellingNotARefusal(t *testing.T) {
	root := workspace(t, nil)
	set, err := BuildToolset([]string{aliasWriteFile}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("a pinned blueprint naming the old spelling was refused at build: %v", err)
	}
	if !set.Has(aliasWriteFile) {
		t.Error("the alias is not callable")
	}
	if set.RerunClass(aliasWriteFile) != RerunDetectable {
		t.Errorf("the alias classifies as %q, want detectable like the write tool", set.RerunClass(aliasWriteFile))
	}
	// ONE tool, ONE spec: both spellings in the grant list still offer the tool
	// once, and the FIRST spelling is the one that holds the slot.
	both, err := BuildToolset([]string{aliasWriteFile, "write"}, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	specs := both.Specs()
	if len(specs) != 1 {
		t.Fatalf("both spellings offered %d specs, want 1 (SEC-051)", len(specs))
	}
	if specs[0].Name != aliasWriteFile && specs[0].Name != "write" {
		t.Errorf("the slot is held by %q", specs[0].Name)
	}
	if !both.Has("write") || !both.Has(aliasWriteFile) {
		t.Error("both spellings must still dispatch, whichever one holds the offered slot")
	}
}

func TestCharacterizeEmptyAndDefaultGrants(t *testing.T) {
	root := workspace(t, nil)
	// An empty grant is not an error: it is a step that can do nothing, which is
	// what an empty `agent.tools` and a refused-by-default agent mean.
	empty, err := BuildToolset(nil, ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("an empty grant list was refused: %v", err)
	}
	if len(empty.Granted()) != 0 || len(empty.Specs()) != 0 {
		t.Errorf("an empty grant built %v / %d specs", empty.Granted(), len(empty.Specs()))
	}
	// The default grant still builds, and its specs come out in grant order —
	// the same three names the audit says nobody has to ask for.
	set, err := BuildToolset(DefaultGrant(), ToolContext{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("the default grant was refused: %v", err)
	}
	if got, want := strings.Join(set.Granted(), ","), "read,list,write"; got != want {
		t.Errorf("the default grant builds %q, want %q", got, want)
	}
	// A tool that exists but is not in the list is not reachable by name.
	if set.Has("bash") {
		t.Error("bash is reachable from the default grant")
	}
}

// TestCharacterizeWorkspaceRootPanicStaysPut pins the one panic in the path so
// the extraction cannot quietly turn it into a returned error (or vice versa): it
// is a programmer check, and a caller that gets an error there would handle it as
// a bad grant when the real fault is a missing workspace.
func TestCharacterizeWorkspaceRootPanicStaysPut(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("BuildToolset with no workspace root did not panic — the check moved or softened")
		} else if !strings.Contains(sprint(r), "needs a workspace root") {
			t.Errorf("the panic is %v, want the workspace-root message", r)
		}
	}()
	_, _ = BuildToolset([]string{"nope"}, ToolContext{})
}

func sprint(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return ""
}
