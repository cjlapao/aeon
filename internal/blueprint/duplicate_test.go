package blueprint

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// duplicate_test.go — S48 T2 rule 2: a duplicate is TWO lines, never a
// re-serialization. Comments, ordering, blank lines, CRLF/LF and the
// trailing newline are byte-preserved; anchored or multi-line id/version
// are refused; the copy validates.

const dupSrc = `# author: someone who commented
id: two-step   # the name, right here
version: 3
schemaVersion: 1

lanes:
  - id: author # a commented lane
    step:
      kind: model-step
      agent: author
`

func TestDuplicateChangesOnlyIdAndVersion(t *testing.T) {
	out, err := Duplicate([]byte(dupSrc), "copy")
	if err != nil {
		t.Fatal(err)
	}
	aLines := strings.Split(dupSrc, "\n")
	bLines := strings.Split(string(out), "\n")
	if len(aLines) != len(bLines) {
		t.Fatalf("line count changed: %d → %d", len(aLines), len(bLines))
	}
	changed := 0
	for i := range aLines {
		if aLines[i] != bLines[i] {
			changed++
			if !strings.HasPrefix(aLines[i], "id:") && !strings.HasPrefix(aLines[i], "version:") {
				t.Fatalf("line %d changed but is neither id nor version: %q → %q", i+1, aLines[i], bLines[i])
			}
			if strings.HasPrefix(aLines[i], "id:") && !strings.Contains(bLines[i], "id: copy") {
				t.Fatalf("id line wrong: %q", bLines[i])
			}
			// the trailing comment survived on the changed id line
			if strings.HasPrefix(aLines[i], "id:") && !strings.Contains(bLines[i], "# the name, right here") {
				t.Fatalf("the trailing comment on %q was lost: %q", aLines[i], bLines[i])
			}
		}
	}
	if changed != 2 {
		t.Fatalf("%d lines changed, want exactly 2", changed)
	}
	bp, err := Load(out)
	if err != nil {
		t.Fatalf("the copy does not load: %v", err)
	}
	if bp.ID != "copy" || bp.Version != 1 {
		t.Fatalf("copy is %q@%d, want copy@1", bp.ID, bp.Version)
	}
}

func TestDuplicatePreservesCommentsAndLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(dupSrc, "\n", "\r\n")
	out, err := Duplicate([]byte(crlf), "kepler")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Count(s, "\r\n") != strings.Count(s, "\n") {
		t.Fatal("CRLF was normalized to LF")
	}
	if !strings.HasSuffix(s, "\r\n") {
		t.Fatal("the trailing newline was lost")
	}
	if !strings.Contains(s, "# author: someone who commented") || !strings.Contains(s, "# a commented lane") {
		t.Fatal("a comment was lost")
	}
	// No-name-change idempotence of bytes: everything except the two lines
	// is identical to the source.
	aLines := strings.Split(crlf, "\r\n")
	bLines := strings.Split(s, "\r\n")
	for i := range aLines {
		if aLines[i] != bLines[i] && !strings.HasPrefix(aLines[i], "id:") && !strings.HasPrefix(aLines[i], "version:") {
			t.Fatalf("line %d damaged: %q → %q", i+1, aLines[i], bLines[i])
		}
	}
	// Tab-indented content survives (a Marshal would flatten it).
	tabbed := dupSrc + "\textra: keep\r\n"
	out2, err := Duplicate([]byte(tabbed), "tabbed-copy")
	if err == nil {
		if !strings.Contains(string(out2), "\textra: keep") {
			t.Fatal("tab-indented bytes were altered")
		}
	}
}

func TestDuplicateRefusesAnchoredOrMultilineKeys(t *testing.T) {
	cases := map[string]string{
		"anchored id":      "id: &name two-step\nversion: 1\nschemaVersion: 1\nlanes: []\n",
		"aliased id":       "base: &b two-step\nid: *b\nversion: 1\nschemaVersion: 1\nlanes: []\n",
		"block id":         "id: |\n  two-step\nversion: 1\nschemaVersion: 1\nlanes: []\n",
		"folded id":        "id: >\n  two-step\nversion: 1\nschemaVersion: 1\nlanes: []\n",
		"mapping id":       "id:\n  v: two-step\nversion: 1\nschemaVersion: 1\nlanes: []\n",
		"missing id":       "version: 1\nschemaVersion: 1\nlanes: []\n",
		"missing version":  "id: two-step\nschemaVersion: 1\nlanes: []\n",
		"duplicate id key": "id: a\nid: b\nversion: 1\nschemaVersion: 1\nlanes: []\n",
		"not a mapping":    "- id: two-step\n",
	}
	for name, src := range cases {
		if _, err := Duplicate([]byte(src), "copy"); err == nil {
			t.Fatalf("%s: Duplicate accepted it — the guard is not armed", name)
		}
	}
	// Grammar: a bad name is refused before any byte moves.
	if _, err := Duplicate([]byte(dupSrc), "Bad Name"); err == nil {
		t.Fatal("a name outside the grammar was accepted")
	}
}

func TestDuplicateResultValidates(t *testing.T) {
	for _, name := range StandardNames() {
		raw, err := readShipped(name)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Duplicate(raw, "copy-of-"+name)
		if err != nil {
			t.Fatalf("%s: Duplicate: %v", name, err)
		}
		rep := Analyze(out, ValidationOptions{Mode: "standard", Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: s16ActionSet, AgentTools: handler.ToolsFor})
		if !rep.OK(false) {
			t.Fatalf("%s duplicate invalid: %+v", name, rep.Diagnostics)
		}
	}
}

// readShipped reads a shipped blueprint through the embedded source (the
// seam, exercised where it is cheapest).
func readShipped(name string) ([]byte, error) {
	raw, ok, err := EmbeddedSource().Get(name)
	if err != nil || !ok {
		return nil, errIf(err, name)
	}
	return raw, nil
}

func errIf(err error, name string) error {
	if err == nil {
		return &missingErr{name}
	}
	return err
}

type missingErr struct{ name string }

func (m *missingErr) Error() string { return "shipped blueprint " + m.name + " missing from the embed" }
