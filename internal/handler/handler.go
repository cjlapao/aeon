package handler

// ════════════════════════════════════════════════════════════════════════════
// The handler definition — S13's loaded shape.
//
// A handler is a markdown file: YAML front matter (name, description,
// temperature, a tools ALLOW-LIST, handoff fields, an optional policy-agent)
// plus a reviewed body. This type is what the loader produces and what T10's
// registry consumes. It is deliberately NOT S8's runtime Handler: that one
// is built from this one at dispatch time. Two shapes, one direction — the
// file loads into a Definition, the Definition builds into a runtime step.
//
// The prompt seam (T1, Stage 5 protection): Stage 5 will record learnings
// while agents work. If retrieved content were CONCATENATED into Body, then
// after six months an agent's prompt would be half unreviewed generated
// text — exactly the context injection this project exists to remove,
// arriving by a route nobody was watching. So:
//
//   - Body is the reviewed artefact and is NEVER appended to. It stays
//     diffable against its original (the drop records exist for that diff).
//   - Retrieved content arrives as Sections — attributed, separate — and
//     this slice always passes none. The seam costs a parameter now so
//     Stage 5's only options are never "concatenate or rewrite every call
//     site".
// ════════════════════════════════════════════════════════════════════════════

import (
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
)

// Definition is one loaded handler file. Field order is the front matter's
// reading order, not an alphabet; provenance is first-class because a
// handler that cannot say where it came from cannot have its policy checked
// against the corpus that names it.
type Definition struct {
	// SourcePath is the file this was read from — named in every refusal.
	SourcePath string

	Name        string
	Description string

	// Temperature ports as-is: a model parameter, not prose (A20 spares it).
	Temperature float64

	// Tools is the TRANSLATED allow-list (A19): aeon tool names only, every
	// one present in the tool registry (O1 — the set is read from there,
	// never hardcoded). No entry was renamed into existence: task is DROPPED
	// (A1: nothing spawns; a rename reintroduces delegation through a config
	// file), todowrite became todo, and codesearch/lsp/patch are retired.
	Tools []string

	// Handoff maps each declared field to its stated purpose. Required by
	// T3's validation; loaded raw here so the schema rules live in one file.
	Handoff map[string]string

	// PolicyAgent names the corpus agent whose CommandAccess rules this
	// handler ships (rewrites: "golang-engineer" ships "golang-engineer-loop"
	// — the corpus is extracted from opencode/, which this slice never
	// writes, so the provenance link is declared, not guessed). Empty means
	// the handler's own name. Byte-identity with the extractor's output for
	// that agent is asserted per handler (A16/D5) — two parsers over the
	// same front matter is how the enforced policy and the reported policy
	// come to disagree, so there is exactly one parser: S11's.
	PolicyAgent string

	// Tech is the closed-vocabulary tags this agent serves, sorted; empty when
	// the agent declares none. It is what `agent: by-tech-tag` dispatches on.
	Tech []string

	// CommandRules is S11's extractor output for PolicyAgent — pattern and
	// verdict, authored order preserved. NEVER parsed here (A16): the loader
	// does not read permission.bash, not even to be helpful.
	CommandRules []policy.CorpusRule

	// Body is the markdown below the front matter. NEVER appended to — the
	// Compile seam is the only place extra prompt content could arrive, and
	// it arrives attributed, separately, never inside this string.
	Body string

	// Mode and Variant are RECORDED, inert. All 26 originals declare
	// mode: subagent — A1 removed the distinction, so it must not error
	// (the originals' own fields would make the loader unusable).
	// Measured Q2 (2026-10-25): `variant` (coder-fast ×21, coder-thinking
	// ×5) appears in no body prose, no skill, and no orchestrator line —
	// nothing in the corpus reads it. Recorded so a future reading has the
	// bytes; nothing branches on it (TestNothingBranchesOnVariantOrMode).
	Mode    string
	Variant string

	// IsOriginal marks a file loaded from opencode/ (the read-only
	// originals). Originals carry the v1 shapes the guards would refuse in
	// a rewrite — a tools MAP with false entries, retired tool names —
	// because those are facts about the originals, not errors in them.
	// Rewrites must be clean (T2's guards); originals must merely parse.
	IsOriginal bool

	// RetiredFound lists, for originals only, the RETIRED tool names the
	// file declared true (task, todowrite, codesearch, lsp, patch,
	// "filesystem_*"): the loader records what it found rather than
	// pretending the translation table read nothing.
	RetiredFound []string

	// DeclaredFalse lists, for originals only, the tools the v1 files
	// declared FALSE — the v1 permission-record shape (`task: false`,
	// `question: false`, `"filesystem_*": false`). They are not retired
	// names and never were: a false entry was a denial statement about a
	// tool that often exists. Conflating the two categories would let a
	// reader of the record mix "this name no longer exists" with "this
	// name was denied", and those are different facts.
	DeclaredFalse []string
}

// Section is retrieved content attributed to its source — the Stage 5 seam.
// Attribution is not decoration: "why did this agent behave differently"
// has to have an answer, and the answer is the Source.
type Section struct {
	Name   string
	Source string
	Text   string
}

// StepRequest is the COMPILED prompt: not one string. Body and Sections are
// fields, so a caller (transcript, cost meter, S9 render) can tell reviewed
// prose from retrieved attribution without parsing anything back apart.
type StepRequest struct {
	Name        string
	System      string // == Body, byte for byte. Compile never edits it.
	Sections    []Section
	Tools       []string
	Temperature float64
}

// Compile assembles a prompt from the reviewed body plus zero or more
// retrieved, attributed sections. Stage 5 fills the second argument; THIS
// slice always passes none (asserted — TestThisSliceAlwaysPassesNoSections).
func Compile(h Definition, retrieved []Section) StepRequest {
	req := StepRequest{
		Name:        h.Name,
		System:      h.Body, // assignment, not concatenation — the seam is the point
		Tools:       append([]string(nil), h.Tools...),
		Temperature: h.Temperature,
	}
	if len(retrieved) > 0 {
		req.Sections = append([]Section(nil), retrieved...)
	}
	return req
}

// toolsOf returns the registry's honest tool-name set for the allow-list
// check. It is a function so a test can hand the loader a REGISTRY EXTENDED
// with a new tool and watch the check accept it — proof the check is not
// hardcoded (O1). Production always calls it with nil.
func toolsOf(extended map[string]bool) map[string]bool {
	set := map[string]bool{}
	for _, n := range tools.ToolNames() {
		set[n] = true
	}
	for n := range extended {
		set[n] = true
	}
	return set
}

// ToolsFor reads a registered body's GRANTED tool allow-list — the same
// entries the resolver builds dispatch from, so validation checks grants
// against what dispatch would actually hand the model (S38 T11, OL9).
// false says the body does not exist: the caller owes the P31 third state,
// not a guessed verdict.
func ToolsFor(name string) ([]string, bool) {
	h, ok := Lookup(name)
	if !ok {
		return nil, false
	}
	return append([]string(nil), h.Tools...), true
}
