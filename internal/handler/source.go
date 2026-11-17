package handler

// source.go — S43-extra T5: where a Handler comes from. The database, not the
// embedded markdown, is the source of truth for first-party dispatch (SD1,
// SD8). The handler guards, Compile and BuildSchema are unchanged; what
// changes is the Source a Handler is built from.
//
// Two sources ship: embeddedSource (the current bodyEntries — used by every
// test that builds no database, and by the seeder as its record source) and
// a row source supplied by the daemon (the production source, backed by the
// store). The package-level Lookup/Build/Names/LoadDefinition/Preset/
// RegisteredBodies delegate to the installed source. Production installs the
// row source at serve; no production path falls back to embeddedSource (a
// silent fallback would reintroduce two truths — SD8).
//
// handler imports no store type: a row arrives as an AgentView (fields only);
// the daemon adapts store.Agent to it, so the layering is unchanged (T0's
// import test).

import (
	"encoding/json"

	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/tools"
)

// Source is where a Handler (and its Definition and names) come from. The
// daemon installs one at serve; the package-level readers delegate to it. It
// is the seam that makes the database the source of truth without touching
// the guards, Compile or BuildSchema.
type Source interface {
	// Lookup returns the dispatchable handler for a name. In the row source
	// a custom agent (origin != system) and a missing row return false — the
	// dispatcher's refusal names the reason, and there is no fallback.
	Lookup(name string) (Handler, bool)
	// Definition returns the front matter a caller needs (temperature, tools,
	// handoff) for a name — the seeder's record source, and the daemon's
	// row view. A name with no row/entry is (zero, a *Reject), never a
	// silent empty.
	Definition(name string) (Definition, *Reject)
	// Names are the dispatchable names (system and seeded).
	Names() []string
	// Bodies are the registered (first-party) body names — the seeder's
	// record source and the corpus the verify sweeps. It is NOT Names: the
	// presets (default, read-only) are dispatchable but not bodies (the
	// presets are Names() minus Bodies(), read via Preset), so a verify that
	// swept Names would name two "not a registered body" findings. A row
	// source returns its agent names (no presets); the embedded source
	// returns the 33 bodies.
	Bodies() []string
	// Preset reports whether a name is a preset (one of the built-in
	// entries the registry hard-codes, not a body).
	Preset(name string) bool
}

// AgentView is the row the daemon hands FromRow — the agent row's fields
// only, no store type (handler imports no store). The daemon adapts
// store.Agent to it, so a row arrives here as plain values.
type AgentView struct {
	Slug        string
	Name        string
	Description string
	Prompt      string
	Tools       string // a JSON array (allow-list); "[]"
	Temperature float64
	Handoff     string // a JSON object; "{}"
	PolicyAgent string
	Origin      string // system | user | import
}

// installed is the source the package-level readers delegate to. It defaults
// to the embedded source; production installs the row source at serve (and a
// named CLI offline tool re-installs the embedded source). A double install
// panics (SetSource) so a second source cannot silently win.
var installed Source = embeddedSource{}

// SetSource installs the source the package-level readers use. Production
// calls it once at serve, after the store opens and after the inert seed
// runs. A second call panics: two installs is two truths (SD8), and a
// silent second win would be a second source nobody asked for.
func SetSource(s Source) {
	if _, ok := s.(Source); !ok {
		panic("handler: SetSource got a nil or invalid source")
	}
	_, isEmbedded := s.(embeddedSource)
	if !isEmbedded && firstInstall {
		panic("handler: SetSource was called twice — two sources is two truths (SD8)")
	}
	installed = s
	if !isEmbedded {
		firstInstall = true
	}
}

// firstInstall guards SetSource: the embedded source is the default and is
// not counted, so the first non-embedded install wins and a second panics.
var firstInstall = false

// ReplaceSource installs a source over whatever is installed — last installer wins. The daemon
// uses it: a process that builds many daemons (every test that does) must read through the last one
// built, and production installs once (one binary, one store). It is not SetSource's "first install
// only" rule, which exists to stop a second source from winning by accident.
func ReplaceSource(s Source) {
	if s == nil {
		panic("handler: ReplaceSource got a nil source")
	}
	installed = s
	_, isEmbedded := s.(embeddedSource)
	firstInstall = !isEmbedded
}

// UseEmbeddedForOfflineTools re-installs the embedded source for a CLI verb
// that needs Names()/Preset() without a database (aeon blueprint validate,
// aeon handlers). It is a deliberate, named, documented offline-tool path
// (listed in T5's Notes) — never reachable from serve.
func UseEmbeddedForOfflineTools() {
	installed = embeddedSource{}
	// Back to "nothing installed", so the next source is a first install: the daemon re-arms
	// before it installs, and a process that builds many daemons reads through the last one.
	firstInstall = false
}

// EmbeddedSource is the embedded source, returned for a caller that always
// wants the code's bodies — the seeder's record source (agentseed), which
// must not follow the installed (row) source or it would seed from its own
// output. It is stable and shares the embedded entries.
func EmbeddedSource() Source { return embeddedSource{} }

// embeddedSource is the current source: the hard-coded entries (the two
// built-in defaults) and the registered bodies. It is what every test that
// builds no database uses, and the seeder's record source.
type embeddedSource struct{}

func (embeddedSource) Lookup(name string) (Handler, bool) { return lookupEmbedded(name) }

func (embeddedSource) Definition(name string) (Definition, *Reject) {
	return loadDefinitionEmbedded(name)
}

func (embeddedSource) Names() []string { return namesEmbedded() }

// Bodies returns the 33 registered body names — NOT Names (which also lists
// the two presets). The corpus verify and the body-oriented readers sweep
// Bodies, not Names, so a preset is never swept as a body (a preset is not a
// body), and a preset in a Names sweep would name two "not a registered body"
// findings.
func (embeddedSource) Bodies() []string { return embeddedBodies() }

func (embeddedSource) Preset(name string) bool { return presetEmbedded(name) }

// embeddedBodies is the code's 33 registered body names — the seeder's
// record source. It is NOT the installed source's names (in production those
// are the rows); it is always the code.
func embeddedBodies() []string { return append([]string(nil), registeredBodies...) }

// EmbeddedBodies is the seeder's record source: the 33 registered body
// names, always the embedded (code) source. A caller that must seed from the
// code — not the rows the seed wrote — uses it, never the installed source
// (that would seed from its own output).
func EmbeddedBodies() []string { return embeddedBodies() }

// EmbeddedDefinition is the seeder's record source: a body's Definition from
// the embedded (code) source, always. The seeder uses it, never the
// installed source.
func EmbeddedDefinition(name string) (Definition, *Reject) { return loadDefinitionEmbedded(name) }

// FromRow builds a dispatchable Handler from a row's content, re-running the
// existing guards on that content (SD8, defence in depth): a hand-edited
// row cannot smuggle a tool the registry lacks, and a handoff the bridge
// cannot render is refused. It returns a *Reject naming the agent and the
// field on any failure; dispatch refuses that agent rather than running it.
//
// A row the person made or imported (origin user or import) is an agent like
// any other: they brought it in and they answer for it. What stands between
// it and a tool call is policy — the rules written when its tools were chosen
// (internal/protocol/agent_grants.go) — not a class of agent that never runs.
func FromRow(a AgentView) (Handler, *Reject) {
	d := definitionFromRow(a)
	// The guards, re-run on the row's content — the same guards the embedded
	// bodies pass at init. A row that fails them is refused, never served.
	if findings := Verify(d, nil); len(findings) > 0 {
		return Handler{}, &Reject{Path: a.Slug, Reason: joinFindings(findings), IO: false}
	}
	if _, rej := BuildSchema(d.Handoff, a.Slug); rej != nil {
		return Handler{}, rej
	}
	// O1: every tool the row names is a registered tool. The loader already
	// refused an out-of-registry tool at init; a row edited by hand can name
	// one, so the guard is re-run here.
	for _, t := range d.Tools {
		if !tools.IsToolName(t) {
			return Handler{}, &Reject{Path: a.Slug, Reason: "tool " + t + " is not a registered tool — the row names a tool the registry lacks (O1)", IO: false}
		}
	}
	h := Handler{Name: d.Name, SystemPrompt: d.Body, Tools: d.Tools, Handoff: d.Handoff, PolicyAgent: d.PolicyAgent}
	return h, nil
}

// DefinitionFromRow is the row's content as the Definition a reader needs
// (tools, handoff, temperature), without building a handler.
func DefinitionFromRow(a AgentView) Definition { return definitionFromRow(a) }

// definitionFromRow rebuilds a Definition from a row's content. The Tools and
// Handoff are JSON in the row (the applier wrote them); the rest is
// verbatim. This is the row -> Definition move; the source of truth is the
// row, not the embedded body.
func definitionFromRow(a AgentView) Definition {
	// A row is dispatched by its slug — the name a blueprint step and a policy rule use — and
	// the display name is for people. A system row's two are the same word.
	name := a.Slug
	if name == "" {
		name = a.Name
	}
	d := Definition{
		SourcePath:  a.Slug,
		Name:        name,
		Description: a.Description,
		Temperature: a.Temperature,
		Tools:       parseTools(a.Tools),
		Handoff:     parseHandoff(a.Handoff),
		PolicyAgent: a.PolicyAgent,
		Body:        a.Prompt,
	}
	return d
}

// parseTools decodes a row's tools blob (a JSON array; "[]" or "") into the
// allow-list. An empty or malformed blob is an empty list, not a panic — the
// guard re-runs over the result and a malformed blob is a refusal.
func parseTools(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// parseHandoff decodes a row's handoff blob (a JSON object; "{}" or "") into
// the field map. An empty or malformed blob is an empty map, not a panic.
func parseHandoff(s string) map[string]string {
	if s == "" || s == "{}" {
		return map[string]string{}
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return map[string]string{}
	}
	return out
}

// buildStepFrom is Build's delegate: it builds the runtime handler through
// the installed source, so a row-sourced handler is built the same way the
// embedded one is — the guards, Compile and BuildSchema are unchanged, only
// the source differs.
func buildStepFrom(name, prompt string) (runtime.StepHandler, bool) {
	h, ok := installed.Lookup(name)
	if !ok {
		return runtime.StepHandler{}, false
	}
	for _, subject := range h.Policy {
		if tools.IsToolName(subject) {
			return runtime.StepHandler{}, false
		}
	}
	step := runtime.DefaultHandler(prompt, h.Tools)
	step.Name = h.Name
	step.System = h.SystemPrompt + "\n\n" + step.System
	if text, err := HandoffSpecFor(h.Handoff); err != nil {
		return runtime.StepHandler{}, false
	} else if text != "" {
		step.HandoffSchema = text
	}
	return step, true
}
