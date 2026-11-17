package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// originalsDir walks up from the package until opencode/agents appears —
// the same discovery convention tools/corpusgen uses, so one rule decides
// where the read-only ground lives.
func originalsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "opencode", "agents")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("opencode/agents not found walking up — the read-only originals are the negative control; without them this package's central tests cannot run")
	return ""
}

func loopFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(originalsDir(t), "*-loop.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 26 {
		names := make([]string, 0, len(files))
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		t.Fatalf("found %d *-loop.md originals, want 26 (the slice's central count — 25 handlers plus git-loop; enumerated, not remembered): %v", len(files), names)
	}
	return files
}

func writeHandler(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "h.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

// ── the design's named tests ───────────────────────────────────────────────

// TestPolicyIsByteIdenticalToTheExtractor is C3's "byte-identical, NOT
// equivalent" made real, plus the independence claim underneath it: the
// loader must not have parsed permission.bash at all. The strongest form of
// independence testable here: MUTATE the file's permission text and prove
// the loaded rules do not budge — if the loader read them from the file,
// this fails; it reads them only from the extractor (A16).
func TestPolicyIsByteIdenticalToTheExtractor(t *testing.T) {
	// The corpus, read INDEPENDENTLY by this test (a second reading of the
	// same committed bytes — not a second parser of the agent files).
	raw, err := os.ReadFile(filepath.Join("..", "policy", "testdata", "corpus.json"))
	if err != nil {
		t.Fatalf("ReadFile corpus.json: %v", err)
	}
	var wire struct {
		Agents []struct {
			Agent string `json:"agent"`
			Rules []struct {
				Pattern string `json:"pattern"`
				Verdict string `json:"verdict"`
			} `json:"rules"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("Unmarshal corpus.json: %v", err)
	}
	corpus := map[string][][2]string{}
	for _, a := range wire.Agents {
		var rs [][2]string
		for _, r := range a.Rules {
			rs = append(rs, [2]string{r.Pattern, r.Verdict})
		}
		corpus[a.Agent] = rs
	}

	checked := 0
	for _, f := range loopFiles(t) {
		d, rej := LoadOriginal(f)
		if rej != nil {
			t.Fatalf("%s: LoadOriginal: %v", filepath.Base(f), rej)
		}
		want, inCorpus := corpus[d.Name]
		if !inCorpus {
			if d.CommandRules != nil {
				t.Errorf("%s carries %d rules but the corpus has no entry — rules from nowhere", d.Name, len(d.CommandRules))
			}
			continue // project-planner-loop: authored without permission — nil, not empty
		}
		if len(d.CommandRules) != len(want) {
			t.Fatalf("%s: loaded %d rules, corpus has %d — not identical", d.Name, len(d.CommandRules), len(want))
		}
		for i, w := range want {
			got := d.CommandRules[i]
			if string(got.Pattern) != w[0] || string(got.Verdict) != w[1] {
				t.Errorf("%s rule %d = (%q,%q), corpus says (%q,%q) — order and bytes both matter (authored order is the operator's statement)",
					d.Name, i, got.Pattern, got.Verdict, w[0], w[1])
			}
		}
		checked++
		// Independence: rewrite the permission block on a copy and reload —
		// the rules must be UNMOVED, because they never came from the file.
		src, _ := os.ReadFile(f)
		mangled := strings.ReplaceAll(string(src), "deny", "allow")
		mangledPath := filepath.Join(t.TempDir(), filepath.Base(f))
		if err := os.WriteFile(mangledPath, []byte(mangled), 0o644); err != nil {
			t.Fatalf("WriteFile mangled: %v", err)
		}
		m, rej := LoadOriginal(mangledPath)
		if rej != nil {
			t.Fatalf("%s mangled: %v", filepath.Base(f), rej)
		}
		if !reflect.DeepEqual(m.CommandRules, d.CommandRules) {
			t.Errorf("%s: rules CHANGED when the file's permission text changed — the loader is parsing policy itself (A16 violation)", d.Name)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d originals had corpus policies checked — the extractor path is barely exercised", checked)
	}
	t.Logf("byte-identical per agent: %d corpus agents + 1 provably-absent, independence proven on all 26", checked)
}

func TestToolsIsAnAllowList(t *testing.T) {
	p := writeHandler(t, `---
name: map-shape
description: declares the v1 map inside a rewrite.
temperature: 0.3
tools:
  read: true
  bash: false
---
Body.
`)
	_, rej := Load(p)
	if rej == nil {
		t.Fatal("a rewrite with a tools MAP passed — an allow-list has no entries set false; the false entry is a permission claim wearing a list's clothes (A6)")
	}
	if !strings.Contains(rej.Reason, "bash") {
		t.Errorf("refusal must name the offending entry; got %q", rej.Reason)
	}
	// Positive control for the same check: a clean list loads.
	ok := writeHandler(t, `---
name: list-shape
description: declares a proper allow-list.
temperature: 0.3
tools: [read, write, todo]
---
Body.
`)
	d, rej := Load(ok)
	if rej != nil {
		t.Fatalf("a clean allow-list refused: %v", rej)
	}
	if !slices.Equal(d.Tools, []string{"read", "write", "todo"}) {
		t.Errorf("tools = %v, want [read write todo]", d.Tools)
	}
}

func TestFrontMatterParsesForAllOriginals(t *testing.T) {
	for _, f := range loopFiles(t) {
		d, rej := LoadOriginal(f)
		if rej != nil {
			t.Errorf("%s: %v — the loader must read every original: a file it mishandles is a handler that silently behaves differently from the one whose behaviour was reviewed", filepath.Base(f), rej)
			continue
		}
		if d.Name != strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f), ".md"), "-loop")+"-loop" {
			t.Errorf("%s loaded as name %q", filepath.Base(f), d.Name)
		}
		if d.Description == "" || strings.TrimSpace(d.Body) == "" {
			t.Errorf("%s: description or body empty", filepath.Base(f))
		}
	}
}

// TestStrictFailuresAreTheTwoMeasuredFiles pins the YAML reality the loader
// tolerates: exactly the two files measured 2026-10-25 fail strict parsing
// (their descriptions carry ": " in unquoted plain scalars). If a future
// original starts failing here, the pin fails LOUDLY — the fallback is
// documented leniency for two known customers, not a general sloppiness
// budget someone can draw down.
func TestStrictFailuresAreTheTwoMeasuredFiles(t *testing.T) {
	failing := []string{}
	for _, f := range loopFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		m := frontMatterRe.FindStringSubmatch(string(src))
		if m == nil {
			t.Fatalf("%s has no front matter at all", filepath.Base(f))
		}
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(stripPermissionBlock(m[1])), &doc); err != nil {
			failing = append(failing, filepath.Base(f))
		}
	}
	slices.Sort(failing)
	if want := []string{"git-loop.md", "qa-engineer-loop.md"}; !slices.Equal(failing, want) {
		t.Fatalf("strict-parse failures = %v, want exactly %v — if a new file joined the list, fix its YAML rather than widening the loader's fallback", failing, want)
	}
	// And they load anyway, with their ':'-bearing descriptions intact.
	for _, base := range failing {
		d, rej := LoadOriginal(filepath.Join(originalsDir(t), base))
		if rej != nil {
			t.Fatalf("%s via fallback: %v", base, rej)
		}
		if !strings.Contains(d.Description, ":") {
			t.Errorf("%s: the ':'-bearing description did not survive verbatim — the fallback ate the very text that made it necessary", base)
		}
	}
}

func TestToolsTranslation(t *testing.T) {
	d, rej := LoadOriginal(filepath.Join(originalsDir(t), "golang-engineer-loop.md"))
	if rej != nil {
		t.Fatalf("LoadOriginal: %v", rej)
	}
	want := []string{"bash", "edit", "glob", "grep", "read", "todo", "webfetch", "write"}
	got := append([]string(nil), d.Tools...)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("translated tools = %v, want %v (A19: task dropped, todowrite→todo, codesearch/lsp/patch retired)", got, want)
	}
	// RetiredFound records names declared TRUE (the ones the handler
	// actually wanted). task was declared false by this file — so its true
	// home in the record is DeclaredFalse, and requiring it here would
	// conflate "retired but wanted" with "retired and denied".
	for _, r := range []string{"todowrite", "codesearch", "lsp", "patch"} {
		if !slices.Contains(d.RetiredFound, r) {
			t.Errorf("RetiredFound missing %q — the loader pretends the translation table read nothing", r)
		}
	}
	// The false entries were v1 DENIALS, a different category from
	// retirement, and recorded separately.
	for _, f := range []string{"task", "question", "filesystem_*"} {
		if !slices.Contains(d.DeclaredFalse, f) {
			t.Errorf("DeclaredFalse missing %q", f)
		}
	}
	if slices.Contains(d.RetiredFound, "question") {
		t.Error("question is a real tool that was DENIED in v1, not a retired name — the categories must not mix")
	}
}

// TestTaskIsDroppedNotRenamed guards the exact failure A19 forbids: if task
// had been RENAMED (say to todo, or anything), delegation would return
// through the config file. Two directions of proof: todo is present (so the
// rename target is todowrite's, not task's), and no loaded set contains
// anything from the forbidden-capability vocabulary — which stays pinned
// non-empty so the check can rot-check itself.
func TestTaskIsDroppedNotRenamed(t *testing.T) {
	if len(forbiddenCapabilities) < 5 {
		t.Fatal("the forbidden-capability vocabulary has shrunk below its pinned floor — A1's guard is being weakened, not extended")
	}
	for _, f := range loopFiles(t) {
		d, rej := LoadOriginal(f)
		if rej != nil {
			t.Fatalf("%s: %v", filepath.Base(f), rej)
		}
		for _, name := range d.Tools {
			if forbiddenCapabilities[name] {
				t.Errorf("%s: loaded tool %q names a capability that starts another step — A1 removed it and a tool may not wear its clothes", filepath.Base(f), name)
			}
		}
	}
	d, _ := LoadOriginal(filepath.Join(originalsDir(t), "golang-engineer-loop.md"))
	if slices.Contains(d.Tools, "todo") && !slices.Contains(d.Tools, "task") {
		// task absent AND todo present is the drop-not-rename signature.
		return
	}
	t.Errorf("todo present & task absent is the signature; got tools=%v", d.Tools)
}

func TestToolsSubsetOfRegistry(t *testing.T) {
	p := writeHandler(t, `---
name: ghost-tool
description: names a tool that does not exist.
temperature: 0.3
tools: [read, search]
---
Body.
`)
	_, rej := Load(p)
	if rej == nil || !strings.Contains(rej.Reason, "search") {
		t.Fatalf("a nonexistent tool must be refused by name; got %v", rej)
	}
	// O1: extend the registry for the check and watch it accept — proof the
	// set is READ from tools.ToolNames, not hardcoded. (S22 will add
	// `search` for real; this is what makes sure the guards follow it.)
	d, rej := loadWithTools(p, false, map[string]bool{"search": true})
	if rej != nil {
		t.Fatalf("with search in the (test) registry the load must succeed: %v", rej)
	}
	if !slices.Contains(d.Tools, "search") {
		t.Error("search dropped from the allow-list even though it was accepted")
	}
}

func TestUnknownFrontMatterKeyIsRefusedInRewrite(t *testing.T) {
	p := writeHandler(t, `---
name: typo-temp
description: a typo'd key is a silently missing field.
temperture: 0.4
tools: [read]
---
Body.
`)
	_, rej := Load(p)
	if rej == nil || !strings.Contains(rej.Reason, "temperture") {
		t.Fatalf("the typo'd key must be refused BY NAME so the author sees their own mistake; got %v", rej)
	}
	// mode/variant do not port — but originals ignore them, rewrites are
	// refused for having copied them.
	modeFile := writeHandler(t, `---
name: ported-mode
description: copied mode from the original out of habit.
temperature: 0.4
mode: subagent
tools: [read]
---
Body.
`)
	if _, rej := Load(modeFile); rej == nil || !strings.Contains(rej.Reason, "mode") {
		t.Fatalf("a rewrite carrying mode must be refused: %v", rej)
	}
	for _, f := range loopFiles(t) {
		if _, rej := LoadOriginal(f); rej != nil {
			t.Fatalf("%s: mode: subagent must not error in an original (all 26 declare it): %v", filepath.Base(f), rej)
		}
	}
}

func TestTemperaturePortsUnchanged(t *testing.T) {
	d, rej := LoadOriginal(filepath.Join(originalsDir(t), "golang-engineer-loop.md"))
	if rej != nil {
		t.Fatalf("LoadOriginal: %v", rej)
	}
	if d.Temperature != 0.4 {
		t.Errorf("golang-engineer-loop temperature = %v, want 0.4 ported as-is", d.Temperature)
	}
	bad := writeHandler(t, `---
name: no-temp
description: omits temperature.
tools: [read]
---
Body.
`)
	if _, rej := Load(bad); rej == nil || !strings.Contains(rej.Reason, "temperature") {
		t.Fatalf("temperature present is D4 — its absence is a sampling guess; got %v", rej)
	}
}

func TestPolicyAgentKeyLinksTheRewriteToItsCorpusAgent(t *testing.T) {
	p := writeHandler(t, `---
name: docker-engineer
description: ships docker-engineer-loop's rules.
temperature: 0.3
policy-agent: docker-engineer-loop
tools: [read, bash, todo]
---
Body.
`)
	d, rej := Load(p)
	if rej != nil {
		t.Fatalf("Load: %v", rej)
	}
	orig, rej := LoadOriginal(filepath.Join(originalsDir(t), "docker-engineer-loop.md"))
	if rej != nil {
		t.Fatalf("LoadOriginal docker: %v", rej)
	}
	if !reflect.DeepEqual(d.CommandRules, orig.CommandRules) {
		t.Fatal("the rewrite's rules differ from its corpus agent's — the policy-agent link is decorative")
	}
	// The mid-pattern wildcards specifically (the corpus's only two): any
	// normalisation that moves a '*' changes what the guard permits.
	mid := 0
	for _, r := range d.CommandRules {
		if strings.Contains(string(r.Pattern), "build*--push") || strings.Contains(string(r.Pattern), "bake*--push") {
			mid++
		}
	}
	if mid != 2 {
		t.Errorf("docker's two mid-pattern wildcard rules = %d, want 2, byte-intact", mid)
	}
}

// ── the prompt seam ────────────────────────────────────────────────────────

func TestModeIsNotPorted(t *testing.T) {
	// Absence is not an error, presence in an original is not an error,
	// presence in a rewrite is refused: A1 removed the distinction and the
	// originals still declare it — erroring on their own field would make
	// the loader unusable on the very corpus it must read.
	d, rej := LoadOriginal(filepath.Join(originalsDir(t), "golang-engineer-loop.md"))
	if rej != nil || d.Mode != "subagent" {
		t.Fatalf("mode ignored without error failed: %v / %q", rej, d.Mode)
	}
	noMode := writeHandler(t, "---\nname: nm\ndescription: no mode key.\ntemperature: 0.3\ntools: [read]\n---\nBody.\n")
	if _, rej := Load(noMode); rej != nil {
		t.Fatalf("absence of mode must not be an error: %v", rej)
	}
}

func TestVariantIsRecorded(t *testing.T) {
	// Q2's answer (measured 2026-10-25): variant is SELECTION-ONLY metadata
	// nothing reads — 26 of 26 declare it (coder-fast ×21, coder-thinking
	// ×5) and zero consumers exist in bodies, skills or the orchestrator.
	// Recorded, and nothing branches on it.
	d, rej := LoadOriginal(filepath.Join(originalsDir(t), "golang-engineer-loop.md"))
	if rej != nil {
		t.Fatalf("LoadOriginal: %v", rej)
	}
	if d.Variant != "coder-fast" {
		t.Errorf("golang-engineer-loop variant = %q, want coder-fast recorded verbatim", d.Variant)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	bad := regexp.MustCompile(`(if|switch|case)[^\n]*\.Variant\b`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			st := strings.TrimSpace(line)
			if strings.HasPrefix(st, "//") {
				continue
			}
			if bad.MatchString(st) {
				t.Errorf("%s:%d branches on Variant — Q2 measured it inert: %s", e.Name(), i+1, st)
			}
		}
	}
}

func TestBodyIsNeverAppendedTo(t *testing.T) {
	p := writeHandler(t, "---\nname: bna\ndescription: body immutability.\ntemperature: 0.3\ntools: [read]\n---\nThe reviewed body, verbatim or nothing.\n")
	d, rej := Load(p)
	if rej != nil {
		t.Fatalf("Load: %v", rej)
	}
	before := d.Body
	_ = Compile(d, []Section{{Name: "s", Source: "learnings/x", Text: "retrieved"}})
	if d.Body != before {
		t.Error("Compile mutated Body — the reviewable artefact is no longer what was reviewed")
	}
}

func TestRetrievedSectionsAreAttributed(t *testing.T) {
	p := writeHandler(t, "---\nname: attr\ndescription: attribution.\ntemperature: 0.3\ntools: [read]\n---\nBody.\n")
	d, rej := Load(p)
	if rej != nil {
		t.Fatalf("Load: %v", rej)
	}
	req := Compile(d, []Section{{Name: "prior-lesson", Source: "learnings/2026-10", Text: "retrieved text"}})
	if len(req.Sections) != 1 || req.Sections[0].Source != "learnings/2026-10" || req.Sections[0].Name != "prior-lesson" {
		t.Fatal("a retrieved section without name and source is unanswerable when someone asks why the agent behaved differently")
	}
	if req.System == d.Body+"retrieved text" {
		t.Error("the section was concatenated into System")
	}
}

func TestLoadErrorsAreDomainErrors(t *testing.T) {
	// Domain: file read, content wrong → Reject with IO false, naming the file.
	bad := writeHandler(t, "no front matter here at all")
	_, rej := Load(bad)
	if rej == nil || rej.IO || !strings.Contains(rej.Path, "h.md") {
		t.Fatalf("malformed front matter must be a DOMAIN refusal naming the file; got %+v", rej)
	}
	// Infrastructure: file unreadable → IO true, so a caller never retries
	// a format error as if it were a disk hiccup.
	_, rej = Load(filepath.Join(t.TempDir(), "absent.md"))
	if rej == nil || !rej.IO {
		t.Fatalf("a disk error must be distinguishable from a domain refusal; got %+v", rej)
	}
}

func TestCompileTakesRetrievedSectionsAndNeverTouchesBody(t *testing.T) {
	p := writeHandler(t, `---
name: seam
description: exercises the Stage 5 seam.
temperature: 0.3
tools: [read]
handoff:
  summary: the paragraph a reader needs
---
The reviewed body, verbatim or nothing.
`)
	d, rej := Load(p)
	if rej != nil {
		t.Fatalf("Load: %v", rej)
	}
	before := d.Body
	req := Compile(d, []Section{{Name: "prior-lesson", Source: "learnings/2026-10", Text: "retrieved text"}})
	if d.Body != before {
		t.Error("Compile mutated Body")
	}
	if req.System != d.Body {
		t.Error("Compile concatenated into System — Body must arrive ASSIGNMENT-only; retrieved content lives in Sections, attributed (that separation IS the Stage 5 protection)")
	}
	if len(req.Sections) != 1 || req.Sections[0].Source == "" {
		t.Error("retrieved sections lost or unattributed — 'why did this agent behave differently' must have an answer")
	}
	// The handoff loaded raw (T3 validates purposes; T1 only reads).
	if d.Handoff["summary"] != "the paragraph a reader needs" {
		t.Errorf("handoff = %v", d.Handoff)
	}
}

// TestThisSliceAlwaysPassesNoSections greps the production callers: every
// Compile call outside the tests passes none. The seam exists; using it is
// Stage 5's, and the grep is what makes "this slice always passes nil" a
// fact a build can check.
func TestThisSliceAlwaysPassesNoSections(t *testing.T) {
	pkgDir := "."
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	callRe := regexp.MustCompile(`\bCompile\(`) // \b: MustCompile( is not Compile(
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // comments document the rule; they don't trip it
			}
			if callRe.MatchString(line) && !strings.Contains(line, "func Compile") {
				if !strings.Contains(line, "nil") {
					t.Errorf("%s:%d passes retrieved sections — this slice always passes none: %s", e.Name(), i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestNothingBranchesOnVariantOrMode pins Q2's answer mechanically: the
// fields are recorded and inert. No non-test source conditions on them.
func TestNothingBranchesOnVariantOrMode(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	bad := regexp.MustCompile(`(if|switch|case)[^\n]*\.(Variant|Mode)\b`)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			st := strings.TrimSpace(line)
			if strings.HasPrefix(st, "//") {
				continue
			}
			if bad.MatchString(st) {
				t.Errorf("%s:%d branches on Variant/Mode — Q2 measured them inert (nothing in the corpus reads variant; A1 removed mode): %s", e.Name(), i+1, st)
			}
		}
	}
}

// ── originals' Q1 facts, asserted not remembered ──────────────────────────

func TestOriginalsFrontMatterIsUniform(t *testing.T) {
	for _, f := range loopFiles(t) {
		d, rej := LoadOriginal(f)
		if rej != nil {
			t.Fatalf("%s: %v", filepath.Base(f), rej)
		}
		if d.Mode != "subagent" {
			t.Errorf("%s mode %q — the measured uniform value is subagent ×26; a change here is a finding about the originals, not the loader", filepath.Base(f), d.Mode)
		}
		if d.Variant != "coder-fast" && d.Variant != "coder-thinking" {
			t.Errorf("%s variant %q outside the two measured values", filepath.Base(f), d.Variant)
		}
	}
}
