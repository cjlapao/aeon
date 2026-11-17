package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T5 — rewrites 5–10: sql, react-ts, vue-ts, websocket, tauri-react, tauri-vue.
//
// The trap specific to this batch (spec): six similar handlers invite a
// shared preamble that nobody wrote. TestNoSharedBoilerplateWasInvented
// holds the line — a line shared across bodies must be traceable to the
// originals sharing it. The tauri pair legitimately shares the Stack
// mandate because BOTH originals carry it near-verbatim; the invented-
// looking shared line nobody can point at is the one that fails.

var t5Batch = []string{
	"sql-engineer",
	"react-typescript-engineer",
	"vue-typescript-engineer",
	"websocket-engineer",
	"tauri-react-engineer",
	"tauri-vue-engineer",
}

func TestSixPassVerifyAndRunOnce(t *testing.T) {
	for _, n := range t5Batch {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if fs := Verify(d, nil); len(fs) > 0 {
			for _, f := range fs {
				t.Errorf("%s", f.String())
			}
		}
		res, _ := dispatch(t, n, "add a health endpoint",
			t4Turn{ToolUses: []model.Block{writeBlock("NOTES.md", "artifact written through the real write tool\n")}},
			okReport(n+" completed the step"),
		)
		mustOK(t, res, n)
	}
}

// ── the boilerplate trap ───────────────────────────────────────────────────

var (
	numPrefixRe  = regexp.MustCompile(`^[\d]+\.\s*|^-\s*|^\s+`)
	trailPunctRe = regexp.MustCompile(`[\s.:\-—]+$`)
	wsCollapseRe = regexp.MustCompile(`\s+`)
)

// normLine lowercases, strips markdown decoration and leading numbering,
// and applies the ONE sanctioned tool rename (todowrite→todo, A19) so a
// traceable rewrite of a line all originals share is recognisable.
func normLine(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "`", ""))
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", " ")
	s = numPrefixRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "todowrite", "todo")
	s = wsCollapseRe.ReplaceAllString(s, " ")
	return trailPunctRe.ReplaceAllString(s, "")
}

// bodyLines returns the body text below the front matter, line by line.
func bodyLines(t *testing.T, name string) []string {
	t.Helper()
	d, rej := LoadDefinition(name)
	if rej != nil {
		t.Fatalf("%s: %v", name, rej)
	}
	return strings.Split(d.Body, "\n")
}

func originalLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	s := string(raw)
	// below the closing front-matter fence
	if i := strings.Index(s[strings.Index(s, "---")+3:], "\n---\n"); i >= 0 {
		s = s[strings.Index(s, "---")+3+i+5:]
	}
	return strings.Split(s, "\n")
}

func TestNoSharedBoilerplateWasInvented(t *testing.T) {
	// lines shared by ≥2 bodies
	seen := map[string][]string{}
	for _, n := range registeredBodies {
		for _, line := range bodyLines(t, n) {
			norm := normLine(line)
			if len(norm) < 40 {
				continue
			}
			seen[norm] = append(seen[norm], n)
		}
	}
	// For each shared line, how many ORIGINALS carry it (normalized)?
	// Cache per original file; a line must be traceable to ≥2 originals
	// when ≥2 bodies share it (A17).
	origFiles := loopFiles(t)
	origSets := make(map[string]map[string]bool, len(origFiles))
	for _, f := range origFiles {
		set := map[string]bool{}
		for _, line := range originalLines(t, f) {
			set[normLine(line)] = true
		}
		origSets[f] = set
	}
	for norm, bodies := range seen {
		if len(bodies) < 2 {
			continue
		}
		count := 0
		for _, set := range origSets {
			if set[norm] {
				count++
			}
		}
		if count < 2 {
			t.Errorf("shared boilerplate invented: %q appears verbatim in %d bodies (%s) but in %d originals — six similar handlers invite a shared preamble nobody wrote; either each handler's own original carries it, or each body says it in its own words (A17)",
				norm, len(bodies), strings.Join(bodies, ", "), count)
		}
	}
}

func TestKeptContentTracesToItsOwnOriginal(t *testing.T) {
	root := repoRoot(t)
	lref := regexp.MustCompile(`- L(\d+)`)
	for _, n := range t5Batch {
		rec, rej := ParseDropRecord(filepath.Join(".", "drops", n+".md"))
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		want := filepath.Join("opencode", "agents", n+"-loop.md")
		if rec.Original != want {
			t.Errorf("%s: drop record traces to %q, not its own original %q — kept content must trace per handler, not per batch", n, rec.Original, want)
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rec.Original))
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		lines := strings.Count(strings.TrimRight(string(raw), "\n"), "\n") + 1
		kept := append(rec.Sections["DOMAIN"], rec.Sections["REWORDED"]...)
		if len(kept) == 0 {
			t.Fatalf("%s: no DOMAIN/REWORDED bullets — kept content is unaccounted", n)
		}
		for _, b := range kept {
			for _, m := range lref.FindAllStringSubmatch(b, -1) {
				var v int
				fmt.Sscanf(m[1], "%d", &v)
				if v < 1 || v > lines {
					t.Errorf("%s: kept bullet references L%s but its own original has %d lines — the reference is to a file it does not trace to", n, m[1], lines)
				}
			}
		}
	}
}

func TestTauriHandlersAreGeneralPurpose(t *testing.T) {
	for _, n := range []string{"tauri-react-engineer", "tauri-vue-engineer"} {
		d, rej := LoadDefinition(n)
		if rej != nil {
			t.Fatalf("%s: %v", n, rej)
		}
		if regexp.MustCompile(`(?i)\baeon\b`).MatchString(d.Body + " " + d.Description) {
			t.Errorf("%s mentions Aeon — these handlers' domain is the Rust/web boundary of ANY user's Tauri project; guidance rewritten into advice about our app stops being usable by anyone else (T5 trap #2)", n)
		}
		for _, must := range []string{"Tauri v2", "capabilit"} {
			if !strings.Contains(d.Body, must) {
				t.Errorf("%s lost %q — the v2 mandate and the capability discipline ARE the domain", n, must)
			}
		}
	}
}
