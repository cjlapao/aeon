// Package mcp is where Aeon's agents get tools beyond the core set — the
// S12 MCP client. Three tiers (core, public, private), a client over stdio
// and HTTP, and one rule everything else follows:
//
//	Private is invisible and uncallable to peers — not merely unauthorized.
//
// The registry (registry.go) is the security object: ToolsFor(agent) is the
// only way any caller obtains tools, and there is no "all tools" list to
// filter later — a filter is a thing somebody forgets to apply, and a list
// that never contained the tool is not (MC2, D3).
//
// The configuration (this file) is the only door a server ever arrives by
// (MC3): no scanning, no auto-registration, not even as a convenience flag.
// A tool surface that changes without a config edit is one nobody reviewed —
// and under MC4 it is also one nobody granted.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cjlapao/aeon/internal/tools"
)

// saveMu serialises every SaveConfig call in this process. Q2 measured
// (this session): zero existing writers of mcp.json anywhere in the
// codebase before this slice — SaveConfig is the first and, by design, the
// only one (TD4: config authoring exists as proposal + commit, never a
// bypass verb). A single mutex, not a path-keyed map, because the daemon
// resolves exactly ONE config path per process (paths.MCPConfigFile, one
// machine) — a keyed lock would guard a case that cannot occur today. If a
// second writer or a second path ever arrives, that is the moment to widen
// this, not before.
var saveMu sync.Mutex

// Transport kinds (N7): the same adapter, two mountings — one client, T2,
// speaks both.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

// Class vocabularies. The re-run classes ARE internal/tools' (W16) — the
// registry re-declares nothing; the effect classes are S11's table of L22
// (S16's policy type is still the declared stub, which is why effect is
// carried as data here and enforced by this slice's call gate, T6).
const (
	EffectLocal        = "local"
	EffectOutward      = "outward"
	EffectIrreversible = "irreversible"
)

// Config is the operator's MCP configuration: the servers that exist, and
// the agents they exist for.
//
// # Q3, answered here (the shape is the answer)
//
// **Private servers nest under their agent; public servers live in their own
// top-level section.** There is no `tier` field and no `private_to` field —
// the tier IS the position in the file. A flat list with a `private:` flag
// makes "forgot the flag" mean public, and every leaked private tool in this
// product's history would begin as one forgotten boolean. Nesting makes the
// mistake structurally hard to write:
//
//   - a server under `"agents": {"reddit-agent": {"private": […]}}` is
//     private to reddit-agent and to nobody else — there is no way to ask
//     (MC2, MC12);
//   - a server under `"public"` is available-to-declare, and an agent has it
//     only by naming it in `"declares"` (MC5: public means available, never
//     granted-to-all);
//   - the same name in both places, or under two agents, is a LOAD error —
//     not a last-write-wins merge.
//
// Unknown keys are refused, not ignored: "tier" as a leftover key from a
// misremembered schema is a tier silently not set.
type Config struct {
	// Public holds the servers any declaring agent may use.
	Public []ServerConfig `json:"public"`

	// Agents maps agent name → its private servers and its declarations.
	// An agent absent from this map is valid: it gets the core tier and no
	// MCP tools (MC11's level-0).
	Agents map[string]AgentConfig `json:"agents"`
}

// AgentConfig is one agent's MCP configuration.
type AgentConfig struct {
	// Core is the core-set grant: the built-in tools this agent may have.
	// nil means "the whole registered core set" — the S11 default, since
	// per-step narrowing is S13's handler lists, not this file. A non-nil
	// list narrows it.
	Core []string `json:"core"`

	// Private are the servers configured for exactly this agent. Invisible
	// to every other agent — not merely unauthorized (MC2).
	Private []ServerConfig `json:"private"`

	// Declares names public tools this agent uses, by qualified name
	// ("server.tool"). Declaring is the grant (MC5); "public" never means
	// "everyone has it".
	Declares []string `json:"declares"`
}

// ServerConfig is one MCP server: its namespace, its transport, and the
// tools we have agreed it offers.
//
// The Tools list is OUR declaration (Q4): the classification lives here, in
// reviewable config, not in the server's self-assessment. A tool the server
// offers that this list does not carry exists and is REFUSED (T3) — offered
// is not classified, and classified is not granted.
type ServerConfig struct {
	Name      string          `json:"name"`
	Transport TransportConfig `json:"transport"`
	Tools     []ToolConfig    `json:"tools"`
}

// TransportConfig is the transport, in the shape its type means.
//
// Env carries environment variable NAMES for stdio servers, never values —
// C17: a credential in a config file is a credential in a backup, a diff and
// a screenshot. The client reads the values from the environment when it
// spawns (T2).
type TransportConfig struct {
	Type    string   `json:"type"` // "stdio" | "http"
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env"`
	// EnvFrom binds each Env NAME to a variable of the variable manager (the value
	// the process receives is that variable's, resolved when the process starts).
	// A name without an entry is an unbound legacy one: it resolves as it always
	// did, and the door that writes a server refuses to leave one unbound.
	EnvFrom map[string]string `json:"envFrom,omitempty"`
	URL     string            `json:"url"`
}

// ToolConfig declares one tool's existence and its classes.
//
// An empty Rerun or Effect is not invalid — it is UNCLASSIFIED, which T3
// refuses at the boundary. It is not defaulted, and the dangerous default is
// exactly the one that looks safe (MC6).
type ToolConfig struct {
	Name   string `json:"name"`
	Rerun  string `json:"rerun"`
	Effect string `json:"effect"`
}

var (
	// serverNameRE pins what a namespace may be: lower-case, no dot — the
	// dot is the namespace join (MC8), so a name that carries one would
	// make qualified names ambiguous about their own provenance.
	serverNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

	// variableNameRE is the variable manager's name grammar, as far as this package
	// needs it: it cannot import the manager, and a binding to a name that could
	// never exist is a refusal at the write, not a failed start later.
	variableNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

	// toolNameRE pins what a server's tool may be called. Dots are allowed
	// INSIDE tool names; the first dot in a qualified name is always the
	// separator, and a tool name never starts or ends with one.
	toolNameRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+)*$`)

	envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ErrNoConfig distinguishes "the file is not there" (MC11's level-0, the
// demo's §1) from "the file is there and broken" (an error the operator must
// see verbatim). The two are different facts and the CLI renders them
// differently.
var ErrNoConfig = errors.New("mcp: no configuration file")

// LoadConfigFile reads and validates the configuration at path. A missing
// file answers ErrNoConfig — absent configuration works (MC11), a corrupt
// one never silently becomes absent.
func LoadConfigFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("%w at %s", ErrNoConfig, path)
		}
		return Config{}, fmt.Errorf("mcp: reading %s: %w", path, err)
	}
	return parseConfig(raw)
}

// parseConfig decodes strictly and validates. Split from LoadConfigFile so
// tests and the daemon's reload path share one parser — a second decoder is
// a second schema.
func parseConfig(raw []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("mcp: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("mcp: trailing data after the configuration object")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// WithServer returns a COPY of c with srv added at tier ("public" or
// "private" — "private" also needs a non-empty agent). It mutates nothing —
// not c, not the file — because it exists for S35 T2's `mcp_server_add`
// proposal: the caller runs Validate() on the result, at create time and
// again at commit (TD3, TD4), through the one builder rather than two
// divergent ways of building a would-be config. Config-shaped work belongs
// here, not in cmd/aeon (this file's own header rule: the configuration is
// the only door a server ever arrives by).
func (c Config) WithServer(tier, agent string, srv ServerConfig) (Config, error) {
	switch tier {
	case "public":
		if agent != "" {
			return Config{}, fmt.Errorf("mcp: tier %q takes no agent (got %q) — a public server belongs to every declaring agent, not one", tier, agent)
		}
		out := c
		out.Public = append(append([]ServerConfig{}, c.Public...), srv)
		return out, nil
	case "private":
		if strings.TrimSpace(agent) == "" {
			return Config{}, fmt.Errorf("mcp: tier %q needs an agent — a private server belongs to exactly one agent (MC2)", tier)
		}
		out := c
		out.Agents = make(map[string]AgentConfig, len(c.Agents))
		maps.Copy(out.Agents, c.Agents)
		ac := out.Agents[agent]
		ac.Private = append(append([]ServerConfig{}, ac.Private...), srv)
		out.Agents[agent] = ac
		return out, nil
	default:
		return Config{}, fmt.Errorf("mcp: tier %q is not %q or %q", tier, "public", "private")
	}
}

// WithoutServer returns a COPY of c with the named server removed from
// wherever it lives — public or any agent's private list. MC12 keeps a
// server name unique across the whole config, so the name alone is enough
// to find it; mirrors WithServer's own copy-never-mutate discipline, for
// S35 T5's `mcp_server_remove` commit path.
func (c Config) WithoutServer(name string) Config {
	out := c
	pub := make([]ServerConfig, 0, len(c.Public))
	for _, s := range c.Public {
		if s.Name != name {
			pub = append(pub, s)
		}
	}
	out.Public = pub
	out.Agents = make(map[string]AgentConfig, len(c.Agents))
	maps.Copy(out.Agents, c.Agents)
	for agent, ac := range out.Agents {
		priv := make([]ServerConfig, 0, len(ac.Private))
		for _, s := range ac.Private {
			if s.Name != name {
				priv = append(priv, s)
			}
		}
		ac.Private = priv
		out.Agents[agent] = ac
	}
	return out
}

// SaveConfig is this file's first and only writer (TD4) — config authoring
// exists as proposal + commit, never a bypass verb; `aeon mcp add`/`remove`
// are not created, here or anywhere (brief §6, this task's own Do NOT).
// Validate first (a bad candidate must never touch disk), then write a
// temp file in the SAME directory and os.Rename over the real path —
// atomic on the same filesystem, so a process killed mid-write leaves the
// original file untouched and a reader never sees a half-written one.
func SaveConfig(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp: encode the config to save: %w", err)
	}
	saveMu.Lock()
	defer saveMu.Unlock()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mcp: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".mcp-*.json.tmp")
	if err != nil {
		return fmt.Errorf("mcp: create a temp file beside %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("mcp: write the temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mcp: close the temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("mcp: rename into place at %s: %w", path, err)
	}
	return nil
}

// Validate is the load-time refusal set. Every rule here is a mistake an
// operator could plausibly make; the alternative is the mistake surfacing at
// call time as an unexplained absence.
func (c Config) Validate() error {
	seen := map[string]string{} // server name → where it was configured
	if err := c.validateServerSet(c.Public, "public", seen); err != nil {
		return err
	}
	for agent, ac := range c.Agents {
		if strings.TrimSpace(agent) == "" {
			return fmt.Errorf("mcp: agents: an agent name must not be empty")
		}
		where := fmt.Sprintf("agents.%s.private", agent)
		if err := c.validateServerSet(ac.Private, where, seen); err != nil {
			return err
		}
		if err := validateCoreGrant(agent, ac.Core); err != nil {
			return err
		}
		if err := c.validateDeclares(agent, ac.Declares); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) validateServerSet(servers []ServerConfig, where string, seen map[string]string) error {
	toolSeen := map[string]map[string]bool{}
	for _, s := range servers {
		if !serverNameRE.MatchString(s.Name) {
			return fmt.Errorf("mcp: %s: server name %q is not a namespace (lower-case letters, digits, _ and -, starting with a letter; no dots — the dot is the separator)", where, s.Name)
		}
		if prev, dup := seen[s.Name]; dup {
			return fmt.Errorf("mcp: server %q is configured twice (%s and %s) — the same server cannot be two tiers or two agents' (MC12)", s.Name, prev, where)
		}
		seen[s.Name] = where
		if err := validateTransport(s); err != nil {
			return fmt.Errorf("mcp: %s: server %q: %w", where, s.Name, err)
		}
		toolSeen[s.Name] = map[string]bool{}
		for _, t := range s.Tools {
			if !toolNameRE.MatchString(t.Name) {
				return fmt.Errorf("mcp: %s: server %q: tool name %q is not a tool name", where, s.Name, t.Name)
			}
			if toolSeen[s.Name][t.Name] {
				return fmt.Errorf("mcp: %s: server %q declares tool %q twice", where, s.Name, t.Name)
			}
			toolSeen[s.Name][t.Name] = true
			if t.Rerun != "" && !validRerun(t.Rerun) {
				return fmt.Errorf("mcp: %s: server %q tool %q: rerun class %q is not safe, detectable or unsafe", where, s.Name, t.Name, t.Rerun)
			}
			if t.Effect != "" && !validEffect(t.Effect) {
				return fmt.Errorf("mcp: %s: server %q tool %q: effect class %q is not local, outward or irreversible", where, s.Name, t.Name, t.Effect)
			}
		}
	}
	return nil
}

// ValidateTransport is the transport's own validation, for a caller that holds
// a candidate transport and no whole config (the inspect door): the same rules a
// stored server is held to, so "inspect passed" and "add passes" cannot disagree.
func ValidateTransport(s ServerConfig) error { return validateTransport(s) }

func validateTransport(s ServerConfig) error {
	switch s.Transport.Type {
	case TransportStdio:
		if s.Transport.Command == "" {
			return fmt.Errorf("stdio transport has no command")
		}
		if s.Transport.URL != "" {
			return fmt.Errorf("stdio transport carries a url — stdio servers do not have one")
		}
		for _, e := range s.Transport.Env {
			if !envNameRE.MatchString(e) {
				return fmt.Errorf("env entry %q is not a variable NAME — values never live in the configuration file (C17)", e)
			}
		}
		inEnv := map[string]bool{}
		for _, e := range s.Transport.Env {
			inEnv[e] = true
		}
		for name, variable := range s.Transport.EnvFrom {
			if !inEnv[name] {
				return fmt.Errorf("envFrom binds %q, which is not one of the server's env names", name)
			}
			if !variableNameRE.MatchString(variable) {
				return fmt.Errorf("envFrom binds %s to %q, which is not a variable name (letters, digits and _, not starting with a digit)", name, variable)
			}
		}
	case TransportHTTP:
		if s.Transport.URL == "" {
			return fmt.Errorf("http transport has no url")
		}
		u, err := url.Parse(s.Transport.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("http transport url %q is not an http(s) URL", s.Transport.URL)
		}
		if s.Transport.Command != "" || len(s.Transport.Args) > 0 || len(s.Transport.Env) > 0 || len(s.Transport.EnvFrom) > 0 {
			return fmt.Errorf("http transport carries stdio fields (command/args/env) — a url is the whole transport")
		}
	default:
		return fmt.Errorf("transport type %q is not %q or %q", s.Transport.Type, TransportStdio, TransportHTTP)
	}
	return nil
}

func validateCoreGrant(agent string, core []string) error {
	if core == nil {
		return nil
	}
	reg := tools.Registry()
	for _, name := range core {
		if name == aliasWriteFile {
			return fmt.Errorf("mcp: agents.%s.core grants %q — the hidden compatibility alias is in no listing (S11)", agent, name)
		}
		if _, ok := reg[name]; !ok {
			return fmt.Errorf("mcp: agents.%s.core grants %q — not a core tool; a typo here is indistinguishable from granting nothing", agent, name)
		}
	}
	return nil
}

// aliasWriteFile mirrors internal/tools' hidden alias so the config refuses
// to grant it. The tools package keeps its own spelling; this copy is
// pinned by a registry test, not shared (tools must not import mcp, and mcp
// importing the constant from a test-visible place inside tools would make
// the alias semi-public).
const aliasWriteFile = "write_file"

func (c Config) validateDeclares(agent string, declares []string) error {
	public := map[string]ServerConfig{}
	for _, s := range c.Public {
		public[s.Name] = s
	}
	seen := map[string]bool{}
	for _, q := range declares {
		if seen[q] {
			return fmt.Errorf("mcp: agents.%s.declares names %q twice", agent, q)
		}
		seen[q] = true
		server, tool, ok := splitQualified(q)
		if !ok {
			return fmt.Errorf("mcp: agents.%s.declares entry %q is not qualified — public tools are declared as \"server.tool\"", agent, q)
		}
		s, isPublic := public[server]
		if !isPublic {
			return fmt.Errorf("mcp: agents.%s.declares %q — no PUBLIC server %q: declaring is naming a public tool, and a private server is named by its owner alone (MC12)", agent, q, server)
		}
		found := false
		for _, t := range s.Tools {
			if t.Name == tool {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("mcp: agents.%s.declares %q — server %q does not carry that tool in its configuration", agent, q, server)
		}
	}
	return nil
}

// splitQualified parses a qualified name at the FIRST dot: the server half
// can never contain one (serverNameRE), so the parse is unambiguous even
// when the tool's own name does.
func splitQualified(q string) (server, tool string, ok bool) {
	i := strings.Index(q, ".")
	if i <= 0 || i == len(q)-1 {
		return "", "", false
	}
	return q[:i], q[i+1:], true
}

func validRerun(s string) bool {
	switch s {
	case "safe", "detectable", "unsafe":
		return true
	}
	return false
}

func validEffect(s string) bool {
	switch s {
	case EffectLocal, EffectOutward, EffectIrreversible:
		return true
	}
	return false
}
