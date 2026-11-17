package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The negative control (D1/C2), re-run on every build: verify MUST fail on
// every one of the 26 originals. A guard that passes everything — including
// the bodies it was written to reject — proves nothing.

func TestVerifyFailsOnAnOriginalBody(t *testing.T) {
	for _, f := range loopFiles(t) {
		fs, rej := VerifyFile(f, true, nil)
		if rej != nil {
			t.Fatalf("%s: original must load (facts, not errors): %v", filepath.Base(f), rej)
		}
		if len(fs) == 0 {
			t.Fatalf("%s: verify PASSED an original — the guard has stopped measuring anything (D1)", filepath.Base(f))
		}
		// ≥3 distinct forbidden tools named.
		tools := map[string]bool{}
		var kinds []string
		for _, fi := range fs {
			kinds = append(kinds, fi.Kind)
			if fi.Kind == "forbidden-tool" {
				tools[fi.Ref] = true
			}
		}
		if len(tools) < 3 {
			t.Errorf("%s: named %d forbidden tools, want ≥3 — %v", filepath.Base(f), len(tools), mapKeys(tools))
		}
		// The measured 26/26 tokens, specifically.
		for _, must := range []string{"loop_get", "memory_append", "event_append"} {
			if !tools[must] {
				t.Errorf("%s: %q is present in every original (measured) and must be named", filepath.Base(f), must)
			}
		}
		if !slices.Contains(kinds, "skill") {
			t.Errorf("%s: no skill finding — all 26 load the 793-line dev-loop-protocol skill (D2)", filepath.Base(f))
		}
		if !slices.Contains(kinds, "ritual") {
			t.Errorf("%s: no ritual finding — all 26 carry FINAL REPORT", filepath.Base(f))
		}
	}
}

func TestNoForbiddenToolInAnyBody(t *testing.T) {
	dir := filepath.Join(".", "bodies")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatal("bodies/ must exist from T2 onward — T4–T8 land files in it; an absent directory hides a missed step")
	}
	findings, rejects := VerifyAllBodies(dir, nil)
	for _, r := range rejects {
		t.Errorf("%v", r)
	}
	for _, f := range findings {
		t.Errorf("%s", f.String())
	}
	if len(findings) == 0 {
		entries, _ := os.ReadDir(dir)
		n := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				n++
			}
		}
		if n == 0 {
			t.Log("VACUOUSLY TRUE until T4: no rewrites exist yet — this test becomes the guard when bodies/ fills")
		}
	}
}

// TestForbiddenToolSetComesFromRegistry is O1's proof: the same token flips
// its verdict on REGISTRY membership, not on a hardcoded reject-list.
func TestForbiddenToolSetComesFromRegistry(t *testing.T) {
	p := writeHandler(t, `---
name: names-codesearch
description: references the v1 codesearch tool by name.
temperature: 0.3
tools: [read, write]
---
Resolve library docs via codesearch when the API is version-pinned.
`)
	fs, rej := VerifyFile(p, false, nil)
	if rej != nil {
		t.Fatalf("clean front matter: %v", rej)
	}
	named := false
	for _, f := range fs {
		if f.Kind == "forbidden-tool" && f.Ref == "codesearch" {
			named = true
		}
	}
	if !named {
		t.Fatal("codesearch is not in the registry and must be refused by name")
	}
	fs2, rej := VerifyFile(p, false, map[string]bool{"codesearch": true})
	if rej != nil {
		t.Fatalf("recheck: %v", rej)
	}
	for _, f := range fs2 {
		if f.Kind == "forbidden-tool" && f.Ref == "codesearch" {
			t.Fatal("the verdict followed a hardcoded list, not the registry — with codesearch registered the reference is legitimate (S22's search is the real-world case this generalises)")
		}
	}
}

func TestSkillReferenceIsRejected(t *testing.T) {
	p := writeHandler(t, `---
name: skillful
description: loads a skill.
temperature: 0.3
tools: [read]
---
Load the dev-loop-protocol skill first.
`)
	fs, _ := VerifyFile(p, false, nil)
	if !hasFinding(fs, "skill") {
		t.Fatalf("dev-loop-protocol reference passed — nothing loads skills (D2); got %v", fs)
	}
	// Even a generic skill mention: a body instructing skill fetch points
	// the model at instructions the runtime will never read.
	p2 := writeHandler(t, `---
name: skill-generic
description: references some skill.
temperature: 0.3
tools: [read]
---
Consult the playwright skill before screenshots.
`)
	if fs2, _ := VerifyFile(p2, false, nil); !hasFinding(fs2, "skill") {
		t.Fatal("a generic skill reference passed")
	}
}

func TestRitualResidueIsRejected(t *testing.T) {
	cases := []struct{ body, wantSub string }{
		{"End with the FINAL REPORT line.", "FINAL REPORT"},
		{"If any agent other than the reviewer invokes it, it declines.", "agent"},
		{"Append any gotcha to memory.md when you learn something.", "memory"},
	}
	for _, c := range cases {
		p := writeHandler(t, "---\nname: ritual-\n"+"description: clean description here.\ntemperature: 0.3\ntools: [read]\n---\n"+c.body+"\n")
		fs, rej := VerifyFile(p, false, nil)
		if rej != nil {
			t.Fatalf("load: %v", rej)
		}
		found := false
		for _, f := range fs {
			if f.Kind == "ritual" && strings.Contains(strings.ToLower(f.Ref+f.Msg), strings.ToLower(c.wantSub)) {
				found = true
			}
		}
		if !found {
			t.Errorf("body %q did not independently produce a ritual finding naming %q; got %v", c.body, c.wantSub, fs)
		}
	}
}

func TestDescriptionIsChecked(t *testing.T) {
	// Body CLEAN; description carries the caller guard. A20's exact failure
	// mode: strip the body, port the description, ship a handler that
	// declines work from an orchestrator that no longer exists.
	p := writeHandler(t, `---
name: clean-body
description: "implementation micro-agent. If any agent other than dev-loops invokes it, it declines."
temperature: 0.3
tools: [read]
---
Write the failing test first, then the code. Run the suite green.
`)
	fs, rej := VerifyFile(p, false, nil)
	if rej != nil {
		t.Fatalf("load: %v", rej)
	}
	if !hasFinding(fs, "description") {
		t.Fatalf("clean body + declining description passed — the model reads the description too (A20); got %v", fs)
	}
	// And the body really is clean: the ONLY findings are description-kind.
	for _, f := range fs {
		if f.Kind != "description" {
			t.Errorf("body was clean, yet a %s finding appeared: %s", f.Kind, f.String())
		}
	}
}

// ── drop records ───────────────────────────────────────────────────────────

func writeDrop(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "some-handler.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

const goodDrop = `# some-handler — drop record

original: x/orig.md  (10 lines)
rewrite:  y/rw.md  (6 lines)
delta:    4 lines removed, 0 added

## RITUAL (deleted — the engine does this)
- L1-L2    caller guard              → the blueprint dispatches
- L3       event checkpoint          → the daemon emits events

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4       "exit 137 is OOM"         → bash reports oomKilled (W5)

## DOMAIN (kept)
- L5-L6    idiom and craft

## DEFERRED (kept OUT — capability absent)
- L7       context7 docs             → deferred; an MCP client would carry this

## REWORDED (A17's ledger)
- L8 shortened; meaning unchanged
`

func TestDropRecordAccountsForLineDelta(t *testing.T) {
	if _, rej := ParseDropRecord(writeDrop(t, goodDrop)); rej != nil {
		t.Fatalf("a well-formed record refused: %v", rej)
	}
	broken := strings.Replace(goodDrop, "delta:    4 lines removed, 0 added", "delta:    3 lines removed, 0 added", 1)
	_, rej := ParseDropRecord(writeDrop(t, broken))
	if rej == nil || !strings.Contains(rej.Reason, "does not add up") {
		t.Fatalf("an arithmetic gap must fail (D6): %v", rej)
	}
	for _, want := range []string{"10", "6", "3"} {
		if !strings.Contains(rej.Reason, want) {
			t.Errorf("the arithmetic refusal must name the numbers; missing %q in %q", want, rej.Reason)
		}
	}
}

func TestDropRecordRequiresBecameMechanismSection(t *testing.T) {
	absent := strings.Replace(goodDrop, `## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4       "exit 137 is OOM"         → bash reports oomKilled (W5)

`, "", 1)
	_, rej := ParseDropRecord(writeDrop(t, absent))
	if rej == nil || !strings.Contains(rej.Reason, "BECAME MECHANISM") {
		t.Fatalf("absence of BECAME MECHANISM must fail even when it would be empty (A12): %v", rej)
	}
	// Present-but-empty passes — the claim "nothing became mechanism" is
	// itself recorded.
	empty := strings.Replace(goodDrop, `- L4       "exit 137 is OOM"         → bash reports oomKilled (W5)
`, "", 1)
	if _, rej := ParseDropRecord(writeDrop(t, empty)); rej != nil {
		t.Fatalf("an empty-but-present BECAME MECHANISM is a legitimate claim: %v", rej)
	}
}

func TestDropRecordRequiresDeferredSection(t *testing.T) {
	absent := strings.Replace(goodDrop, `## DEFERRED (kept OUT — capability absent)
- L7       context7 docs             → deferred; an MCP client would carry this

`, "", 1)
	_, rej := ParseDropRecord(writeDrop(t, absent))
	if rej == nil || !strings.Contains(rej.Reason, "DEFERRED") {
		t.Fatalf("absence of DEFERRED must fail (A18): %v", rej)
	}
	// The MCP obligation: an original mentioning MCP forces NON-EMPTY
	// DEFERRED, checked against the real file.
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.md")
	os.WriteFile(orig, []byte(strings.Repeat("line with MCP inside\n", 10)), 0o644)
	rw := filepath.Join(dir, "rw.md")
	os.WriteFile(rw, []byte(strings.Repeat("kept\n", 6)), 0o644)
	text := strings.ReplaceAll(goodDrop, "x/orig.md", orig)
	text = strings.ReplaceAll(text, "y/rw.md", rw)
	text = strings.ReplaceAll(text, "- L7       context7 docs             → deferred; an MCP client would carry this\n", "")
	p := writeDrop(t, text)
	r, rej := ParseDropRecord(p)
	if rej != nil {
		t.Fatalf("parse: %v", rej)
	}
	if rej := CheckDropRecordFiles(r, ""); rej == nil || !strings.Contains(rej.Reason, "MCP") {
		t.Fatalf("MCP original with empty DEFERRED must fail (A18): %v", rej)
	}
}

// TestMCPCountIsTheMeasuredTwentyFour pins A18's factual basis. The
// originals are frozen history; if this count moves, the corpus moved and
// every DEFERRED obligation must be re-read — which is exactly what a
// silent drift would hide.
func TestMCPCountIsTheMeasuredTwentyFour(t *testing.T) {
	withMCP := 0
	for _, f := range loopFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if regexp.MustCompile(`(?i)\bmcp\b`).Match(raw) {
			withMCP++
		}
	}
	if withMCP != 24 {
		t.Fatalf("originals mentioning MCP = %d, want the measured 24 of 26 (memory-steward-loop and qa-test-spec-loop are the named exceptions) — A18's DEFERRED obligation is calibrated on this count", withMCP)
	}
}

func TestDropRecordUsesLineReferences(t *testing.T) {
	noRefs := strings.Replace(goodDrop,
		"- L1-L2    caller guard              → the blueprint dispatches",
		"- removed the ritual preamble", 1)
	_, rej := ParseDropRecord(writeDrop(t, noRefs))
	if rej == nil || !strings.Contains(rej.Reason, "line reference") {
		t.Fatalf("an entry without a line reference must fail (O2): %v", rej)
	}
}

func TestDropRecordHeaderIsEnforced(t *testing.T) {
	_, rej := ParseDropRecord(writeDrop(t, strings.Replace(goodDrop, "original: x/orig.md  (10 lines)", "original: x/orig.md", 1)))
	if rej == nil || !strings.Contains(rej.Reason, "original:") {
		t.Fatalf("a header without the line count makes the delta uncheckable; got %v", rej)
	}
}

// ── CLI exit codes ─────────────────────────────────────────────────────────

// TestVerifyExitCode is the CI contract: non-zero on failure, so no output
// parsing is required. Kept in the handler package via the exported pieces;
// the cmd/aeon twin drives the real binary path.
func TestVerifyExitCode(t *testing.T) {
	orig := filepath.Join(originalsDir(t), "golang-engineer-loop.md")
	fs, _ := VerifyFile(orig, true, nil)
	if len(fs) == 0 {
		t.Fatal("control gone")
	}
	clean := writeHandler(t, `---
name: quiet
description: a reviewed body with no residue at all.
temperature: 0.3
tools: [read, write]
---
Read the failing test, implement the smallest change that satisfies it, run the package suite.
`)
	fs2, rej := VerifyFile(clean, false, nil)
	if rej != nil || len(fs2) != 0 {
		t.Fatalf("the clean rewrite must verify empty: rej=%v findings=%v", rej, fs2)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

func hasFinding(fs []Finding, kind string) bool {
	for _, f := range fs {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
