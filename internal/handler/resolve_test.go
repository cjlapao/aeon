package handler

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// T10 — the resolver, the table, and the closed registry.

// The shipped agents declare their tech in their front matter; dispatch reads
// those declarations (tech.go), so the real bodies are the fixture.
func shippedTechAgents(t *testing.T) []TechAgent {
	t.Helper()
	var out []TechAgent
	for _, name := range EmbeddedBodies() {
		d, rej := EmbeddedDefinition(name)
		if rej != nil {
			t.Fatalf("%s: %s", name, rej.Reason)
		}
		if len(d.Tech) > 0 {
			out = append(out, TechAgent{Slug: name, Tech: d.Tech})
		}
	}
	return out
}

func TestKnownTagsResolve(t *testing.T) {
	// The words the old dispatch table accepted still pick the same agents.
	want := map[string]string{
		"go":          "golang-engineer",
		"python":      "python-engineer",
		"javascript":  "javascript-engineer",
		"rust":        "rust-engineer",
		"sql":         "sql-engineer",
		"react-ts":    "react-typescript-engineer", // the tag is the STACK, the name is the agent
		"vue-ts":      "vue-typescript-engineer",
		"tauri-react": "tauri-react-engineer",
		"tauri-vue":   "tauri-vue-engineer",
		"websocket":   "websocket-engineer",
	}
	agents := shippedTechAgents(t)
	for tag, name := range want {
		got, err := ResolveByTech(tag, agents)
		if err != nil {
			t.Errorf("%s: %v", tag, err)
			continue
		}
		if got != name {
			t.Errorf("%s resolved to %q, want %q", tag, got, name)
		}
	}
}

func TestTheMostSpecificAgentWins(t *testing.T) {
	agents := []TechAgent{
		{"frontend-engineer", []string{"typescript", "react", "angular"}},
		{"react-engineer", []string{"typescript", "react"}},
	}
	got, err := ResolveByTech("react", agents)
	if err != nil || got != "react-engineer" {
		t.Fatalf("react -> %q, %v — the agent with the fewest extra tags is the closest", got, err)
	}
	got, err = ResolveByTech("angular", agents)
	if err != nil || got != "frontend-engineer" {
		t.Fatalf("angular -> %q, %v", got, err)
	}
	got, err = ResolveByTech("typescript, react", agents)
	if err != nil || got != "react-engineer" {
		t.Fatalf("a request for several tags needs an agent that declares all of them: %q, %v", got, err)
	}
}

func TestATieIsRefusedAndNamesTheAgents(t *testing.T) {
	agents := []TechAgent{{"b-engineer", []string{"typescript"}}, {"a-engineer", []string{"typescript"}}}
	_, err := ResolveByTech("typescript", agents)
	var amb *Ambiguity
	if !errors.As(err, &amb) {
		t.Fatalf("a tie is %T, not *Ambiguity — which agent runs must never be picked silently: %v", err, err)
	}
	if !reflect.DeepEqual(amb.Agents, []string{"a-engineer", "b-engineer"}) || amb.Kind() != "ambiguous-tech-tag" {
		t.Errorf("ambiguity = %+v", amb)
	}
}

func TestNoFallbackAgent(t *testing.T) {
	agents := shippedTechAgents(t)
	for _, tag := range []string{"elixir", "", "cobol"} {
		got, err := ResolveByTech(tag, agents)
		if got != "" {
			t.Errorf("%q resolved to %q — there is no fallback agent (A13, D11): a generic agent writing an unfamiliar language reports ok and the wrongness surfaces three steps later", tag, got)
		}
		var r *Refusal
		if !errors.As(err, &r) || r.Kind() != "unknown-tech-tag" {
			t.Fatalf("%q: the refusal is %T, not *handler.Refusal — callers route on the STRUCTURE, not on parsing text: %v", tag, err, err)
		}
		for _, must := range []string{"go", "python", "react"} {
			found := false
			for _, a := range r.Available {
				found = found || a == must
			}
			if !found {
				t.Errorf("%q: available %v is missing %q", tag, r.Available, must)
			}
		}
		if strings.Contains(strings.Join(r.Available, " "), "engineer") {
			t.Errorf("available %v names agents — it lists TAGS (O4)", r.Available)
		}
	}
}

func TestTechIsClosedAndNormalized(t *testing.T) {
	got, err := NormalizeTech([]string{" TypeScript", "react", "typescript", ""})
	if err != nil || !reflect.DeepEqual(got, []string{"react", "typescript"}) {
		t.Fatalf("NormalizeTech = %v, %v", got, err)
	}
	if _, err := NormalizeTech([]string{"go", "cobol"}); err == nil || !strings.Contains(err.Error(), `"cobol"`) {
		t.Errorf("an unknown tag must be refused by name: %v", err)
	}
	if v := TechVocabulary(); len(v) < 20 || !IsTechTag("go") || IsTechTag("react-ts") {
		t.Errorf("vocabulary %d entries; react-ts is a legacy word, not a tag", len(v))
	}
	if !reflect.DeepEqual(ParseTechRequest("react-ts"), []string{"typescript", "react"}) {
		t.Errorf("legacy word did not expand: %v", ParseTechRequest("react-ts"))
	}
}

func TestResolverRegistryIsClosed(t *testing.T) {
	got := Resolvers()
	if !reflect.DeepEqual(got, []string{"by-tech-tag"}) {
		t.Errorf("Resolvers() = %v — exactly one named resolver, in code (L5); a resolver expressible in config is a config file that runs code", got)
	}
	// The handout is a copy: sorting the caller's copy must not reorder the
	// registry's answer.
	got[0] = "tampered"
	if !IsResolver("by-tech-tag") || IsResolver("tampered") {
		t.Error("Resolvers() handed out the live slice — the enumeration must be read-only in practice, not in comment")
	}
	if IsResolver("by-cost-budget") || IsResolver("golang-engineer") {
		t.Error("IsResolver accepted a name the registry does not contain — handlers are not resolvers and resolvers are not handlers")
	}
}

func TestTwentyFiveHandlersRegisteredBothWays(t *testing.T) {
	bodies, err := os.ReadDir(filepath.Join(".", "bodies"))
	if err != nil {
		t.Fatal(err)
	}
	fromDisk := map[string]bool{}
	for _, e := range bodies {
		if strings.HasSuffix(e.Name(), ".md") {
			fromDisk[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	// 2026-10-27 (S38 T10): + the brainstormer — Count 30 -> 31 DELIBERATE.
	// 2026-10-28 (S39 T6): + the chat body — Count 31 -> 32 DELIBERATE (the
	// plain conversation's voice, CR3; one general body, no specialist default).
	// 2026-10-29 (S24 T5): + the sizing-estimator — Count 32 -> 33 DELIBERATE
	// (the queue's sizing arm: one report_size call with its basis, read-only).
	// 2026-11-05 (S61 T9): + the intake-architect — Count 33 -> 34 DELIBERATE
	// (the goal-intake interview's voice: two capture documents, two inert
	// asks, decides nothing).
	if len(fromDisk) != 34 { // S13 authored twenty-five; T12–T16 added the full follow-up five; S38 T10 the brainstormer; S39 T6 chat; S24 T5 sizing-estimator; S61 T9 intake-architect (measured 2026-11-05) — any future body re-measures this pin
		t.Errorf("bodies/ holds %d bodies — twenty-five from S13 plus the follow-up bodies; counted from the directory, not remembered (D8/C5)", len(fromDisk))
	}
	// 2026-10-28 (S39 T6): + chat — Count 31 -> 32 DELIBERATE.
	if len(registeredBodies) != 34 { // same re-measurement as above; S24 T5 + sizing-estimator (32 -> 33), S61 T9 + intake-architect (33 -> 34 DELIBERATE)
		t.Errorf("registeredBodies has %d entries, bodies/ has %d — the loader's list and the directory must be one truth", len(registeredBodies), len(fromDisk))
	}
	raw, err := os.ReadFile(filepath.Join(".", "testdata", "handlers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pinned []string
	if err := json.Unmarshal(raw, &pinned); err != nil {
		t.Fatal(err)
	}
	inFile := map[string]bool{}
	for _, n := range pinned {
		inFile[n] = true
	}
	// Both ways. Registered but not in the file is an enumeration the app
	// never offers; in the file but not registered is a promise the registry
	// cannot answer.
	for _, n := range Names() {
		if !inFile[n] {
			t.Errorf("handler %q is registered but not in testdata/handlers.json — the committed list has drifted from the registry", n)
		}
	}
	for _, n := range pinned {
		if n == "git-loop" || n == "git-loop-loop" {
			t.Errorf("handlers.json pins %q — A3: git-loop is absent from every surface (T9)", n)
		}
		if _, ok := Lookup(n); !ok {
			if !fromDisk[n] {
				t.Errorf("handlers.json pins %q, which is neither a registered body nor a preset — the enumeration is a promise, and this one cannot be kept", n)
			}
		}
	}
	// The file is sorted: the pin is reviewable as a diff, and a shuffle is
	// a diff that hides real changes.
	for i := 1; i < len(pinned); i++ {
		if pinned[i-1] > pinned[i] {
			t.Errorf("testdata/handlers.json is not sorted at %q", pinned[i])
			break
		}
	}
}
