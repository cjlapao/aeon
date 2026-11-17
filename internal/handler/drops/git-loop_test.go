package drops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/handler"
)

// T9 — git-loop: no handler, but every line accounted for.
//
// A3: 26 *-loop agents, 25 handlers — git-loop becomes NO handler at all,
// because opening a pull request is a mechanical call plus a writing task,
// and splitting them removes the identifier-integrity bug class (O8: the
// largest original in the set, 372 lines, is exactly the one a silent drop
// would lose the most of). The record here disposes of every line; these
// tests make the disposal checkable.

const gitLoopOriginalLines = 372

var (
	secRe    = regexp.MustCompile(`(?m)^## (.+?)\s*$`)
	rangeRe  = regexp.MustCompile(`L(\d+)(?:-L(\d+))?`)
	actionRe = regexp.MustCompile("`(?:git|forge)\\.[a-zA-Z]+`")
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if st, err := os.Stat(filepath.Join(dir, "opencode")); err == nil && st.IsDir() {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no opencode/ ancestor found")
	return ""
}

// sections returns the record's section map: NAME (before any parenthetical)
// → bullet lines.
func sections(t *testing.T, path string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	out := map[string][]string{}
	locs := secRe.FindAllStringSubmatchIndex(text, -1)
	for i, loc := range locs {
		name := strings.ToUpper(strings.TrimSpace(strings.SplitN(text[loc[2]:loc[3]], "(", 2)[0]))
		if _, ok := out[name]; !ok {
			out[name] = []string{}
		}
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		for _, line := range strings.Split(text[loc[1]:end], "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "-") {
				out[name] = append(out[name], strings.TrimSpace(line))
			}
		}
	}
	return out
}

func TestGitLoopIsNotAHandler(t *testing.T) {
	for _, name := range []string{"git-loop", "git-loop-loop"} {
		if _, ok := handler.Lookup(name); ok {
			t.Errorf("%q is a registered handler — A3: git-loop becomes NO handler; its work is code actions (S16) and prose stays a model concern (L3). A handler that both assembles identifiers and writes prose reintroduces the recorded branch-inheritance bug", name)
		}
		for _, n := range handler.Names() {
			if n == name {
				t.Errorf("%q appears in Names() — it must not", name)
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "handlers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "git-loop" || n == "git-loop-loop" {
			t.Errorf("handlers.json pins %q — the enumeration is the promise, and it must not promise a handler A3 forbids", n)
		}
	}
	if _, err := os.Stat(filepath.Join("..", "bodies", "git-loop.md")); !os.IsNotExist(err) {
		t.Error("bodies/git-loop.md exists — there must be no body for a handler that must not exist")
	}
}

func TestEveryGitLoopLineIsAccountedFor(t *testing.T) {
	rec := filepath.Join(".", "git-loop.md")
	raw, err := os.ReadFile(rec)
	if err != nil {
		t.Fatalf("the drop record must exist: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "handler:  none") {
		t.Error("the record must state `handler:  none` — it is a drop record for a handler that does not exist (A3)")
	}
	want := "delta:    372 lines dispositioned"
	if !strings.Contains(text, want) {
		t.Errorf("the record must declare `%s` — the arithmetic is the review (D6/O8)", want)
	}

	want_sections := []string{"BECOMES A CODE ACTION", "BECAME MECHANISM", "ABSORBED BY ANOTHER HANDLER", "RITUAL", "LOST"}
	segs := sections(t, rec)
	for _, s := range want_sections {
		if _, ok := segs[s]; !ok {
			t.Errorf("missing section %q — every line of the largest original lands in one of the five; a missing section is how a category gets skipped rather than considered", s)
		}
	}

	covered := make([]bool, gitLoopOriginalLines+1)
	var addRange func(from, to int, sec string)
	dupReport := ""
	addRange = func(from, to int, sec string) {
		if from < 1 || to > gitLoopOriginalLines || from > to {
			t.Errorf("%s: range L%d-L%d outside 1..%d", sec, from, to, gitLoopOriginalLines)
			return
		}
		for i := from; i <= to; i++ {
			if covered[i] {
				t.Errorf("L%d is claimed twice (second claim: %s) — double-disposition hides a gap elsewhere", i, sec)
				continue
			}
			covered[i] = true
		}
	}
	_ = dupReport
	for _, s := range want_sections {
		for _, e := range segs[s] {
			ms := rangeRe.FindAllStringSubmatch(e, -1)
			if len(ms) == 0 {
				t.Errorf("%s entry %q has no line reference — unreviewable (O2)", s, e)
				continue
			}
			// Each leading reference forms one span (L<n> or L<n>-L<m>);
			// later references in prose are annotations, not claims.
			first := ms[0]
			from, to := atoi(first[1]), atoi(first[2])
			if to == 0 {
				to = from
			}
			addRange(from, to, s)
		}
	}
	for i := 1; i <= gitLoopOriginalLines; i++ {
		if !covered[i] {
			t.Errorf("L%d of git-loop.md is not dispositioned — O8: dropping the largest original silently loses the most; every line lands somewhere checkable", i)
		}
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func TestLostSectionIsExplicit(t *testing.T) {
	segs := sections(t, filepath.Join(".", "git-loop.md"))
	lost, ok := segs["LOST"]
	if !ok {
		t.Fatal("the LOST section is absent — its absence makes 'nothing was lost' indistinguishable from 'nobody checked' (same reasoning as the mandatory BECAME MECHANISM in handler records)")
	}
	for _, e := range lost {
		lower := strings.ToLower(e)
		if !strings.Contains(lower, "because") && !strings.Contains(lower, "reason") && !strings.Contains(lower, "—") {
			t.Errorf("LOST entry without a reason: %q — each entry is a decision someone should be able to disagree with later, which is only possible if the why is written down", e)
		}
	}
}

func TestCodeActionMappingsNameRealActions(t *testing.T) {
	root := repoRoot(t)
	dsg, err := os.ReadFile(filepath.Join(root, "docs", "aeon", "S16-git-forge", "design.md"))
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]bool{}
	for _, m := range actionRe.FindAllString(string(dsg), -1) {
		planned[strings.Trim(m, "`")] = true
	}
	if len(planned) < 5 {
		t.Fatalf("S16's design names %d actions — too few to cross-check against; the design moved?", len(planned))
	}
	segs := sections(t, filepath.Join(".", "git-loop.md"))
	for _, e := range segs["BECOMES A CODE ACTION"] {
		for _, m := range actionRe.FindAllString(e, -1) {
			name := strings.Trim(m, "`")
			if !planned[name] {
				t.Errorf("the record maps git-loop lines to %q, which S16's design does not name — a range mapped to an action S16 does not plan to build is a finding to raise NOW, while the original is still in front of someone; write findings as findings, not as actions", name)
			}
		}
	}
}

func TestTwentyFiveHandlersTwentySixOriginals(t *testing.T) {
	root := repoRoot(t)
	ents, err := os.ReadDir(filepath.Join(root, "opencode", "agents"))
	if err != nil {
		t.Fatal(err)
	}
	originals := 0
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), "-loop.md") {
			originals++
			names = append(names, e.Name())
		}
	}
	bodies, err := os.ReadDir(filepath.Join("..", "bodies"))
	if err != nil {
		t.Fatal(err)
	}
	nb := 0
	for _, e := range bodies {
		if strings.HasSuffix(e.Name(), ".md") {
			nb++
		}
	}
	// Re-measured 2026-10-26 (S17 follow-up pass): the gap is now FIVE —
	// git-loop still has no body (A3, its name is still the gap), and the
	// five follow-up bodies (task-coordinator, proposal-author, brief-author,
	// product-manager, project-manager) are PRIMARIES with sources under
	// opencode/agents/ that were never loop files: the 26 loopFiles glob
	// stayed 26 through the pass precisely because these five were never in
	// it. The enumeration check still measures the directory, not a memory.
	// Re-measured 2026-10-27 (S38 T10): the gap is now SIX — the brainstormer
	// is the first body with NO original at all (authored fresh; its lineage
	// is its drop record's LINEAGE, naming the ancestor whose discipline it
	// re-expressed). Count 30 -> 31 DELIBERATE; git-loop remains the name
	// with a missing body (A3).
	// Re-measured 2026-10-28 (S39 T6): the gap is SEVEN — chat is the second
	// authored-fresh body (CR3: a plain conversation has no loop-agent
	// ancestor to port; its drop record names project-planner-loop.md as the
	// discipline it INVERTS and states that no line was ported). Count
	// 31 -> 32 DELIBERATE.
	// 2026-10-29 (S24 T5): the gap is EIGHT — sizing-estimator is the third
	// authored-fresh body (the queue's sizing arm; its drop record names
	// investigator-loop.md as the ancestor whose measured-size vocabulary it
	// narrows to one report, and states that no line was ported). Count
	// 32 -> 33 DELIBERATE.
	if nb != originals+7 {
		t.Errorf("enumerated from the directory (D8/C5): %d originals vs %d bodies — want the five follow-up primaries plus the THREE authored-fresh bodies (brainstormer, chat, sizing-estimator), beyond the 26 minus git-loop's missing body (A3 + 2026-10-26 follow-up pass + S38 T10 + S39 T6 + S24 T5)", originals, nb)
	}
	found := false
	for _, n := range names {
		if n == "git-loop.md" {
			found = true
		}
	}
	if !found || !strings.Contains(strings.Join(names, ","), "git-loop.md") {
		t.Error("the originals directory no longer contains git-loop.md — this test's premise is the original's continued existence (opencode/ is read-only, so that should be impossible)")
	}
	got := handler.Names()
	sort.Strings(got)
	if len(got) != nb+2 {
		t.Errorf("registry names %d, bodies %d + two presets expected %d — the enumeration is read from the directory, not remembered", len(got), nb, nb+2)
	}
}
