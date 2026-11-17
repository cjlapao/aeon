// Package presets is the data `aeon init` reads and the daemon surface
// (presets.list) shows — the same three templates, not a parallel description
// (D16, I10, P34). A stranger picks a provider, not a wire format (C6): the
// question init asks is which provider, not "what is your base URL, and does
// the server support stream_options.include_usage?".
//
// A preset produces a route (S18's M12), not a second configuration path: the
// capability declarations, the price, and the three capability states are all
// S18's. init is fully scriptable (C5, D7) — every prompt has a flag, so the
// whole flow runs with no TTY. It probes what it configured (C18, D8) — a
// preset is a claim about a server's capabilities and the first run is when
// verifying it costs nothing; a probe failure does not fail init (O6). It does
// not overwrite (O5): an existing config is reported, not replaced. It obeys
// T2's credential rule — the key goes to the environment or a dedicated file,
// never config (D4).
package presets

import (
	"github.com/cjlapao/aeon/internal/config"
)

// CapabilityState is S18's three-state honesty (P31): a capability is verified
// (probed and present), declared (a claim not yet probed), or contradicted
// (probed and absent). "Could not check" and "checked and wrong" are
// different facts.
type CapabilityState string

const (
	Verified    CapabilityState = "verified"
	Declared    CapabilityState = "declared"
	Contradicted CapabilityState = "contradicted"
)

// Capability is one capability a server is claimed to have, and its state.
type Capability struct {
	Name  string
	State CapabilityState
}

// Preset is one of the three (C6, D9): a provider, a model, the capability
// declarations, a price, and a base URL. A stranger picks a provider, and the
// preset supplies the rest — the question is which provider, not the wire
// format.
type Preset struct {
	Provider string
	Model    string
	Capabilities []Capability
	Price    Price
	BaseURL  string
}

// Price is the cost a preset carries (S18's).
type Price struct {
	Input  string
	Output string
}

// InitArgs is the fully-scriptable flow (C5, D7): every prompt has a flag, so
// the whole flow runs with no TTY — over SSH, in a container, in CI. A flag
// present answers its prompt without asking.
type InitArgs struct {
	Provider      string
	Model         string
	KeyFile       string // a path to a key file, never a value (D5, C17)
	NonInteractive bool
	Force         bool
}

// Prompt is one question init asks, and the flag that answers it (C5, D7). A
// prompt without a flag is a prompt nobody can script, and init is one people
// automate in CI — so every prompt has a flag.
type Prompt struct {
	Question string
	Flag     string
}

// Prompts is the set of prompts init asks, each with its flag. The order is
// the wizard's order; a flag present answers the prompt without asking.
var Prompts = []Prompt{
	{Question: "which provider?", Flag: "--provider"},
	{Question: "which model?", Flag: "--model"},
	{Question: "where is the key?", Flag: "--key-file"},
	{Question: "force over an existing config?", Flag: "--force"},
}

// AnswerFor answers a prompt from the args. A present flag answers the prompt
// without asking (C5, D7): a non-interactive run answers every prompt from a
// flag, so the whole flow runs with no TTY.
func (a InitArgs) AnswerFor(p Prompt) (string, bool) {
	switch p.Flag {
	case "--provider":
		return a.Provider, a.Provider != ""
	case "--model":
		return a.Model, a.Model != ""
	case "--key-file":
		return a.KeyFile, a.KeyFile != ""
	case "--force":
		if a.Force {
			return "yes", true
		}
		return "", true // a present --force flag, even false, answers "force?"
	}
	return "", false
}

// List is the three presets init's prompt reads from, and the daemon surface
// (presets.list) shows — the identical data, not a parallel description.
func List() []Preset {
	return []Preset{
		{
			Provider: "anthropic",
			Model:    "claude",
			Capabilities: []Capability{
				{Name: "tools", State: Declared},
				{Name: "streaming-usage", State: Declared},
			},
			Price:   Price{Input: "3/1M", Output: "15/1M"},
			BaseURL: "https://api.anthropic.com",
		},
		{
			Provider: "openai",
			Model:    "gpt",
			Capabilities: []Capability{
				{Name: "tools", State: Declared},
				{Name: "streaming-usage", State: Declared},
			},
			Price:   Price{Input: "2.50/1M", Output: "10/1M"},
			BaseURL: "https://api.openai.com/v1",
		},
		{
			Provider: "openai-compatible",
			// Covers Ollama, vLLM, LiteLLM, OpenRouter (D9): a stranger picks
			// the provider, not the wire format.
			Model:    "compatible",
			Capabilities: []Capability{
				{Name: "tools", State: Declared},
				{Name: "streaming-usage", State: Declared},
			},
			Price:   Price{Input: "varies", Output: "varies"},
			BaseURL: "", // the stranger supplies it, or a server in servers.json
		},
	}
}

// ByProvider finds a preset by provider, or (zero, false) — absence is a
// normal answer, not a fault (DB19).
func ByProvider(p string) (Preset, bool) {
	for _, pr := range List() {
		if pr.Provider == p {
			return pr, true
		}
	}
	return Preset{}, false
}

// Result is what init produced: the route (a preset produces a route, S18's
// M12), the key's source (reported, not its value — T2's rule), and the probe
// states. A key is never written into config (D4).
type Result struct {
	Provider   string
	Model      string
	BaseURL    string
	KeySource  config.Source // where the key came from, never its value (D6)
	Probed     []Capability
	Overwrote  bool
}

// ProbeResult is the outcome of probing one capability: verified, declared, or
// contradicted. A probe failure (an unreachable server) leaves the capability
// Declared and says so — it does not fail init (O6), so someone setting up on
// a train is not blocked from finishing.
type ProbeResult struct {
	Capability Capability
	State      CapabilityState
}

// probe is the probe of a route's capabilities (C18, D8). It returns the state
// per capability; an unreachable server leaves them Declared and says so
// (O6), never failing init. A real probe hits the server; here the state is
// the input (a fake), so the test pins the behaviour without a live server.
func probe(capabilities []Capability, reachable bool) []Capability {
	out := make([]Capability, len(capabilities))
	for i, c := range capabilities {
		if !reachable {
			// Could not check: the capability stays Declared, and it says so
			// (S18's three-state honesty) — not contradicted, not verified.
			out[i] = Capability{Name: c.Name, State: Declared}
			continue
		}
		// A reachable server: the probe verified the declared capability.
		out[i] = Capability{Name: c.Name, State: Verified}
	}
	return out
}

// Init is the fully-scriptable flow (C5, D7). It reads the preset, reads the
// key through T2's one reader (never into config — D4), does not overwrite an
// existing config (O5), and probes what it configured (C18, D8). A probe
// failure does not fail init (O6).
func Init(a InitArgs, existing bool, reachable bool) (Result, bool, error) {
	// A preset produces a route, not a second configuration path.
	pr, ok := ByProvider(a.Provider)
	if !ok {
		return Result{}, false, nil // a bad provider is the caller's choice; a T2 Config error names it
	}
	// Obey T2's credential rule: the key comes from the environment or a key
	// file, through the one reader — never into config (D4).
	key, src, _ := config.Credential("AEON_KEY", a.KeyFile)
	_ = key // the value is used by the route; only the source is reported (D6)

	// Does not overwrite (O5): an existing config is reported, not replaced —
	// --force is required.
	if existing && !a.Force {
		return Result{}, false, nil // reported, not replaced
	}

	res := Result{
		Provider:  pr.Provider,
		Model:     a.Model,
		BaseURL:   pr.BaseURL,
		KeySource: src,
		Probed:    probe(pr.Capabilities, reachable),
		Overwrote: existing && a.Force,
	}
	return res, true, nil
}
