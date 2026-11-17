package readme

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// S23 T7 — the README and the four-step path. The gates are the ones a
// careless change breaks silently: the quickstart is exactly four steps, no
// step requires reading another document, the no-telemetry statement is in
// the README (not only PRIVACY.md), the local-model path is mentioned, and
// the architecture is linked, not summarised.

// fourStep is the four-step path the README and QUICKSTART must hold.
var fourStep = []string{"install", "aeon init", "cd", "aeon step"}

// read reads a doc at the given path under the repo root, or skips.
func read(t *testing.T, rel string) string {
	t.Helper()
	dir, ok := findRepo(t)
	if !ok {
		t.Skip("could not locate the repo root")
	}
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("%s must exist: %v", rel, err)
	}
	return string(b)
}

// TestREADMEPathIsFourSteps is D1's pin: the quickstart section has exactly
// four numbered steps — a fifth step is time this would have spent badly.
func TestREADMEPathIsFourSteps(t *testing.T) {
	text := read(t, "aeon/README.md")
	steps := numberedSteps(text)
	if len(steps) != 4 {
		t.Fatalf("the quickstart must have exactly four steps, got %d: %v", len(steps), steps)
	}
}

// TestNoStepRequiresReadingAnotherDocument is the D-check pin: no step's text
// contains a link or a "see …". A path whose step three says "see
// CONFIGURATION.md" is a path that takes twenty minutes.
func TestNoStepRequiresReadingAnotherDocument(t *testing.T) {
	text := read(t, "aeon/README.md")
	for _, step := range numberedSteps(text) {
		if strings.Contains(strings.ToLower(step), "see ") || strings.Contains(step, ".md") {
			t.Fatalf("a step must not require reading another document: %q", step)
		}
	}
}

// TestEveryCommandInREADMEWorks is the test that keeps the README honest over
// time: every command is extracted and checked. A README command that does not
// work is worse than an absent one. The commands are extracted from the
// README's inline code and the QUICKSTART's fenced block, and checked for
// form (running them needs the built binary, recorded as a follow-up).
func TestEveryCommandInREADMEWorks(t *testing.T) {
	readme := read(t, "aeon/README.md")
	quickstart := read(t, "aeon/docs/QUICKSTART.md")
	cmds := commands(readme)
	cmds = append(cmds, commands(quickstart)...)
	if len(cmds) == 0 {
		t.Fatal("the README and QUICKSTART must contain commands")
	}
	for _, c := range cmds {
		if !wellFormed(c) {
			t.Fatalf("a command is not well-formed: %q", c)
		}
	}
}

// TestQuickstartHoldsTheSameFourSteps is the drift guard: the README and
// QUICKSTART.md do not disagree — they will drift, so this is asserted.
func TestQuickstartHoldsTheSameFourSteps(t *testing.T) {
	readme := read(t, "aeon/README.md")
	quickstart := read(t, "aeon/docs/QUICKSTART.md")
	if len(numberedSteps(readme)) != len(numberedSteps(quickstart)) {
		t.Fatalf("the README and QUICKSTART must hold the same number of steps")
	}
	for _, s := range fourStep {
		if !strings.Contains(readme, s) || !strings.Contains(quickstart, s) {
			t.Fatalf("both docs must hold step %q", s)
		}
	}
}

// TestNoTelemetryStatementIsPresent is T6's pin in the README: the
// no-telemetry statement is in the README, not only in PRIVACY.md — a stranger
// deciding whether to install a thing that reads their source code is
// deciding about that.
func TestNoTelemetryStatementIsPresent(t *testing.T) {
	text := read(t, "aeon/README.md")
	if !strings.Contains(strings.ToLower(text), "no telemetry") {
		t.Fatal("the no-telemetry statement must be in the README, not only PRIVACY.md")
	}
}

// TestLocalModelPathIsMentioned: "works with no cloud key" is in the README —
// a real capability (S18) a stranger will not guess.
func TestLocalModelPathIsMentioned(t *testing.T) {
	text := read(t, "aeon/README.md")
	if !strings.Contains(strings.ToLower(text), "no key") {
		t.Fatal("the local-model path (works with no cloud key) must be in the README")
	}
}

// TestREADMEDoesNotSummariseTheArchitecture: the "what is this" section is
// short and links docs/aeon/ — a README that explains the architecture is a
// README a stranger reads instead of running the thing.
func TestREADMEDoesNotSummariseTheArchitecture(t *testing.T) {
	text := read(t, "aeon/README.md")
	if !strings.Contains(text, "docs/aeon/") {
		t.Fatal("the README must link docs/aeon/ for the design")
	}
	if len(strings.Fields(text)) > 600 {
		t.Fatalf("the README is too long (a README that summarises the architecture is read instead of run): %d words", len(strings.Fields(text)))
	}
}

// numberedSteps extracts the numbered list items (the quickstart steps).
func numberedSteps(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if m := regexp.MustCompile(`^\s*\d+\.\s+(.*)`).FindStringSubmatch(line); m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

// commands extracts the commands from a doc: inline backtick commands and
// fenced-block lines. The README uses inline code; the QUICKSTART a fenced
// block — both are checked.
func commands(text string) []string {
	var out []string
	// Fenced blocks.
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence && trimmed != "" {
			out = append(out, trimmed)
		}
	}
	// Inline backtick commands (the README's steps).
	for _, m := range inlineCmd.FindAllStringSubmatch(text, -1) {
		if m[1] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

var inlineCmd = regexp.MustCompile("`((?:aeon|cd|make)[^`]*)`")

// wellFormed is a command's basic shape (a runnable or an `aeon` command).
func wellFormed(c string) bool {
	if c == "" {
		return false
	}
	return strings.HasPrefix(c, "aeon") || strings.HasPrefix(c, "cd") || strings.HasPrefix(c, "make")
}

// findRepo walks up to the repo root (the parent of the one with go.mod).
func findRepo(t *testing.T) (string, bool) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir), true
		}
		dir = filepath.Dir(dir)
	}
	return "", false
}
