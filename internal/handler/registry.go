// Package handler owns the small S8 handler registry. Blueprint names select
// entries here; they cannot supply tool grants.
package handler

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/runtime"
)

// bodiesFS carries the rewrites into the binary: a handler a loop dispatches
// to must be IN the process, not read off a path that may or may not be
// there (the daemon's workspace root is the user's repository, not ours).
// `all:` so the directory survives while empty (T2's .gitkeep) — the
// pattern without it fails the build at zero matches.
//
//go:embed all:bodies
var bodiesFS embed.FS

// registeredBodies names which bodies are live handlers. Registration is
// code, not configuration (the resolver-closed-registry reasoning, A5/O4):
// a handler joins by appearing here, in the same diff as its body and its
// drop record, and `git-loop` never appears at all (A3). T4 lands four;
// T10 completes the twenty-five.
var registeredBodies = []string{
	"golang-engineer",
	"python-engineer",
	"javascript-engineer",
	"rust-engineer",
	"sql-engineer",
	"react-typescript-engineer",
	"vue-typescript-engineer",
	"websocket-engineer",
	"tauri-react-engineer",
	"tauri-vue-engineer",
	"code-reviewer",
	"verifier-engineer",
	"qa-engineer",
	"qa-test-spec",
	"debugger-engineer",
	"project-planner",
	"intent-clarifier",
	"investigator",
	"technical-writer",
	"memory-steward",
	"docker-engineer",
	"terraform-engineer",
	"security-engineer",
	"legal-advisor",
	"ui-design-engineer",
	// S39 T6 (2026-10-28): the plain conversation — the loop's voice when a
	// person just wants to talk to a component (CR3). One general body; no
	// specialist is the conversation default (the registry's own rule).
	"chat",
	// S24 T5 (2026-10-29): the queue's sizing arm — one report_size call with
	// its basis (D8/D9), read-only tools, CG5's structural exemption.
	"sizing-estimator",
	"task-coordinator",
	"proposal-author",
	"brief-author",
	"product-manager",
	"project-manager",
	// S38 T10 (2026-10-27): the conversation that files goals — OL layers'
	// front door for an idea. Proposes, records, never decides (OL3: approval
	// is a person's act; the deciding vocabulary is absent from its tools).
	"brainstormer",
	// S61 T9 (GI1/GI15, 2026-11-05): the intake interview's voice — files the
	// charter draft and the plan as INERT proposals, decides nothing. Its
	// tools carry the T8 names (the five reads + two asks) and nothing else.
	"intake-architect",
}

// bodyEntries converts registered bodies into registry entries AT INIT,
// fail-loud: a registered file that does not load, or that fails the
// mechanical guards, PANICS at process start. The alternative — a silent
// missing entry — is a loop that dies at dispatch hours later with an error
// naming the dispatch rather than the file that broke. This is also the
// structural link between the guards and the registry: `make ci` and
// `aeon serve` cannot both be green with a body that fails verify.
var bodyEntries = loadBodyEntries()

func loadBodyEntries() map[string]Handler {
	m := make(map[string]Handler, len(registeredBodies))
	for _, name := range registeredBodies {
		path := "bodies/" + name + ".md"
		raw, err := bodiesFS.ReadFile(path)
		if err != nil {
			panic(fmt.Sprintf("handler %q is registered but %s cannot be read: %v", name, path, err))
		}
		d, rej := parse(path, string(raw), false, nil)
		if rej != nil {
			panic(fmt.Sprintf("registered handler refused: %v", rej))
		}
		if d.Name != name {
			panic(fmt.Sprintf("registered handler %q declares name %q — the file, the front matter and the registry must agree by name, not by proximity", name, d.Name))
		}
		if findings := Verify(d, nil); len(findings) > 0 {
			panic(fmt.Sprintf("registered handler %q fails the mechanical guards — the negative-control checks gate the registry, not just CI:\n%s", name, joinFindings(findings)))
		}
		// The loader already refused any tool outside the registry (O1);
		// the entry's Tools is the translated allow-list itself.
		//
		// T10: the handoff map is validated HERE — a body whose declared
		// fields cannot render a schema is a panic at init, the same
		// defence-in-depth as the guards above: dispatch must never meet a
		// body whose promise it cannot keep.
		if _, rej := BuildSchema(d.Handoff, path); rej != nil {
			panic(fmt.Sprintf("registered handler %q carries a handoff the bridge cannot render: %v", name, rej))
		}
		m[name] = Handler{Name: d.Name, SystemPrompt: d.Body, Tools: d.Tools, Handoff: d.Handoff, PolicyAgent: d.PolicyAgent}
	}
	return m
}

// LoadDefinition returns a body's front matter through the installed source
// (S43-extra T5). The seeder's record source is EmbeddedDefinition (the
// code), not this.
func LoadDefinition(name string) (Definition, *Reject) { return installed.Definition(name) }

// loadDefinitionEmbedded is the embedded source's Definition: a registered
// body read out of the embedded copy — the same bytes the process booted
// with, so a test of "the body" tests what dispatch uses.
func loadDefinitionEmbedded(name string) (Definition, *Reject) {
	raw, err := bodiesFS.ReadFile("bodies/" + name + ".md")
	if err != nil {
		return Definition{}, &Reject{Path: "bodies/" + name + ".md", Reason: "not a registered body (registered: " + strings.Join(registeredBodies, ", ") + ")"}
	}
	return parse("bodies/"+name+".md", string(raw), false, nil)
}

func joinFindings(fs []Finding) string {
	out := ""
	for _, f := range fs {
		out += "  " + f.String() + "\n"
	}
	return out
}

type Handler struct {
	Name         string
	SystemPrompt string
	Tools        []string
	// Policy declares the policy SUBJECTS the handler ships with — rule ids
	// it expects the operator to have authored (e.g.
	// "command_access/audit-only"). It is a DOCUMENTATION surface: what the
	// handler assumes exists in the store, nothing more.
	//
	// Policy is NOT a grant surface and never will be (T19). A grant is a
	// decision somebody made about TOOLS, and it lives in exactly one place:
	// the template's Tools field. A policy claim that could smuggle a tool
	// in is the substitution gap this field's refusal closes — Build refuses
	// an entry whose Policy names a registered tool, so "I carry the bash
	// policy" can never read as "I may run bash". (The import direction was
	// considered: handler consults tools.IsToolName, a yes/no helper, rather
	// than the Registry map, so the question stays "is this a tool name?" and
	// the tool values never reach this package.)
	Policy []string

	// Handoff carries the body's declared report fields to Build (T10):
	// the declaration must REACH the model through StepHandler.HandoffSchema
	// or it is documentation again (D12). Lookup copies it, so a caller
	// cannot mutate the loaded map through the returned Handler.
	Handoff map[string]string

	// PolicyAgent names the corpus subject whose CommandAccess rules judge
	// this handler's commands (PL5's binding): the blueprint says
	// `golang-engineer`, the corpus rows are keyed `golang-engineer-loop`,
	// and only this field connects the two — the gate asks under the name
	// the corpus was extracted with, not the dispatch name.
	PolicyAgent string
}

var entries = map[string]Handler{
	"default":   {Name: "default", SystemPrompt: "Work from the repository, complete the requested artifact, and report what you verified.", Tools: []string{"read", "list", "write", "question"}},
	"read-only": {Name: "read-only", SystemPrompt: "Inspect the repository and report findings with file evidence. Do not change files.", Tools: []string{"read", "list"}},
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Lookup returns the dispatchable handler for a name through the installed
// source (S43-extra T5): in production the row source, the source of truth
// (SD1, SD8); in a test or offline tool the embedded source.
func Lookup(name string) (Handler, bool) { return installed.Lookup(name) }

// lookupEmbedded is the embedded source's Lookup: the two hard-coded
// entries (the defaults) and the registered bodies.
func lookupEmbedded(name string) (Handler, bool) {
	h, ok := entries[name]
	if !ok {
		h, ok = bodyEntries[name]
	}
	h.Tools = append([]string(nil), h.Tools...)
	h.Policy = append([]string(nil), h.Policy...)
	h.Handoff = copyMap(h.Handoff)
	return h, ok
}

func Names() []string { return installed.Names() }

func namesEmbedded() []string {
	out := make([]string, 0, len(entries)+len(bodyEntries))
	for name := range entries {
		out = append(out, name)
	}
	for name := range bodyEntries {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Build creates the runtime handler through the installed source — the same
// guards, Compile and BuildSchema, only the source differs (S43-extra T5).
func Build(name, prompt string) (runtime.StepHandler, bool) {
	return buildStepFrom(name, prompt)
}

// RegisteredBodies names the dispatchable bodies through the installed source
// (S43-extra T5): in production the rows, in a test or offline tool the
// embedded bodies. The seeder's record source is EmbeddedBodies (the code),
// not this. It is Bodies (the 33), not Names (Bodies + the presets): a preset
// (default, read-only) is dispatchable but not a body, so the corpus verify
// and the body-oriented readers sweep Bodies and never name a "not a
// registered body" finding for a preset.
func RegisteredBodies() []string { return installed.Bodies() }

// Preset reports the pre-S13 preset handlers through the installed source.
func Preset(name string) bool { return installed.Preset(name) }

// presetEmbedded is the embedded source's Preset: the two hard-coded
// entries (the defaults).
func presetEmbedded(name string) bool {
	_, ok := entries[name]
	return ok
}
