package handler

// ════════════════════════════════════════════════════════════════════════════
// The loader — one file format, two modes, and exactly one policy parser.
//
// Modes (the originals are facts; rewrites are obligations):
//
//   LoadOriginal(path)     opencode/agents/*-loop.md — read-only inputs
//                          (A10). They carry the v1 shapes: a tools MAP
//                          with true/false entries, retired tool names,
//                          mode/variant keys. None of that is an error in
//                          THEM; all of it would be an error in a rewrite.
//   Load(path)             internal/handler/bodies/*.md — the rewrites.
//                          tools must be a list, retired names refused,
//                          unknown front-matter keys refused (naming them:
//                          an author who typos `temperture` must not get a
//                          silently missing field).
//
// YAML reality, measured 2026-10-25 across all 26 originals (not recalled
// from the design): the front matter IS uniform — every file declares
// name, description, mode ("subagent" ×26), variant (coder-fast ×21 /
// coder-thinking ×5), temperature and tools; 25 of 26 declare permission
// (project-planner-loop declares none). Two files (git-loop,
// qa-engineer-loop) carry an unquoted description whose plain scalar
// contains ": " — strict YAML rejects the whole document even though every
// structured field is well formed. The loader therefore parses strictly
// first, and on failure falls back to per-key extraction for the SCALAR
// fields while parsing the structured blocks (tools, handoff) strictly —
// the same division corpus.go draws for the permission block: fidelity-
// critical structure stays strict, presentation-layer breakage is tolerated
// only in the field that actually broke. The fallback is not general
// leniency and a test pins its two known customers.
//
// A16 lives here: the loader NEVER reads permission.bash. CommandRules come
// from S11's extractor — policy.ParseCorpus over the embedded corpus — and
// from nowhere else.
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cjlapao/aeon/internal/policy"
)

// Reject is the loader's discriminated refusal (DB19): a bad file is not an
// infrastructure failure, and a caller must never guess which it got. It
// names the file, the field, and what would fix it.
type Reject struct {
	Path   string
	Reason string

	// IO distinguishes the two halves of "the load failed" (S4's DB19
	// applied): true means DISK — the file could not be read at all — and
	// false means DOMAIN — the file was read and is not a valid handler.
	// One error type for both makes every caller guess which it got.
	IO bool
}

func (r *Reject) Error() string {
	return "refused: " + r.Path + ": " + r.Reason
}

func reject(path, format string, args ...any) *Reject {
	return &Reject{Path: path, Reason: fmt.Sprintf(format, args...)}
}

var frontMatterRe = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// Retired v1 tool names (W1/A19), stated once. task is listed for the
// record even though its fate is special: DROPPED, never renamed.
var (
	retiredNames = map[string]bool{
		"task": true, "todowrite": true, "codesearch": true,
		"lsp": true, "patch": true,
	}
	// A1's capability vocabulary — the names a registry must never grow,
	// because a tool that starts a step is delegation wearing a tool's
	// clothes (A1: nothing nests; no subtree cost to roll up because there
	// is no tree). TestTaskIsDroppedNotRenamed pins the vocabulary stays
	// non-empty and pinned.
	forbiddenCapabilities = map[string]bool{
		"task": true, "spawn": true, "spawn_step": true, "subagent": true,
		"delegate": true, "agent": true,
	}
)

// Load reads a REWRITE (internal/handler/bodies). Strictest mode.
func Load(path string) (Definition, *Reject) {
	return load(path, false, nil)
}

// LoadOriginal reads an ORIGINAL (opencode/agents). Facts about the v1
// shapes are recorded, not refused.
func LoadOriginal(path string) (Definition, *Reject) {
	return load(path, true, nil)
}

// loadWithTools is the test seam for O1: an extended tool set proves the
// allow-list check reads the registry rather than a constant. Production
// passes nil via Load/LoadOriginal.
func loadWithTools(path string, original bool, extraTools map[string]bool) (Definition, *Reject) {
	return load(path, original, extraTools)
}

func load(path string, original bool, extraTools map[string]bool) (Definition, *Reject) {
	raw, err := os.ReadFile(path)
	if err != nil {
		// Reading the file failed — that IS infrastructure, so it arrives
		// as a Reject naming the os error inside rather than pretending the
		// file is merely wrong.
		r := reject(path, "cannot read: %v", err)
		r.IO = true
		return Definition{}, r
	}
	return parse(path, string(raw), original, extraTools)
}

func parse(path, content string, original bool, extraTools map[string]bool) (Definition, *Reject) {
	m := frontMatterRe.FindStringSubmatch(content)
	if m == nil {
		return Definition{}, reject(path, "no YAML front matter between leading --- lines — every original declares name, description, mode, variant, temperature and tools, so a file without it is not a handler")
	}
	fm, body := m[1], content[len(m[0]):]

	d := Definition{SourcePath: path, IsOriginal: original, Body: body}

	fields, rej := frontFields(fm, path)
	if rej != nil {
		return Definition{}, rej
	}

	// Front-matter key discipline, BEFORE the required-field checks: an
	// author who writes `temperture` must be refused by name — reporting
	// "temperature missing" instead blames the file for the author's typo
	// and hides the fix.
	if !original {
		known := map[string]bool{"name": true, "description": true, "temperature": true,
			"tools": true, "handoff": true, "policy-agent": true, "tech": true}
		for k := range fields {
			if !known[k] {
				return Definition{}, reject(path, "unknown front-matter key %q — a rewrite's keys are name, description, temperature, tools, handoff, policy-agent and tech; mode and variant do not port (A1), and a typo'd key is a missing field nobody will notice", k)
			}
		}
	}

	// Required fields. All 26 originals carry all four (measured); absence
	// is a hard refusal in BOTH modes — a file that cannot say its name is
	// not a handler with a missing field, it is not a handler.
	d.Name = strings.TrimSpace(fields["name"])
	d.Description = strings.TrimSpace(fields["description"])
	d.Mode = strings.TrimSpace(fields["mode"])
	d.Variant = strings.TrimSpace(fields["variant"])
	if d.Name == "" {
		return Definition{}, reject(path, "front matter needs name")
	}
	if d.Description == "" {
		return Definition{}, reject(path, "front matter needs description")
	}
	tempRaw := strings.TrimSpace(fields["temperature"])
	if tempRaw == "" {
		return Definition{}, reject(path, "front matter needs temperature — it ports as-is (a model parameter, not prose); its absence is a guess about sampling")
	}
	t, err := strconv.ParseFloat(tempRaw, 64)
	if err != nil || t < 0 || t > 2 {
		return Definition{}, reject(path, "temperature %q is not a model temperature (want a number in [0,2])", tempRaw)
	}
	d.Temperature = t

	// tools — the shape split between modes (A6: an allow-list).
	toolsText := fields["tools"]
	if strings.TrimSpace(toolsText) == "" {
		return Definition{}, reject(path, "front matter needs a tools allow-list")
	}
	var declared []string
	var falseEntries []string
	if mapLike(toolsText) {
		if !original {
			return Definition{}, reject(path, "tools is an allow-list, not a permission map — entries %s are configuration errors: a tool this handler does not need is not declared at all (A6); declare tools: [read, write, …]",
				strings.Join(quoteAll(falseEntriesFrom(toolsText)), ", "))
		}
		declared, falseEntries = parseToolsMap(toolsText)
		if len(falseEntries) > 0 {
			// Original mode: v1 declared `task: false`, `question: false`,
			// `"filesystem_*": false` as PERMISSION statements. Recorded
			// separately from retirement — denied and retired are different
			// facts, and a reader of the record is entitled to both.
			d.DeclaredFalse = append(d.DeclaredFalse, falseEntries...)
		}
	} else {
		// A list (flow or block). Strict YAML first so `tools: [read, 3]`
		// is refused as what it is, then the same text is read for names.
		var strict struct {
			Tools []string `yaml:"tools"`
		}
		if err := yaml.Unmarshal([]byte("tools:\n"+indentBlock(toolsText)), &strict); err != nil {
			return Definition{}, reject(path, "tools is not a list of names: %v", err)
		}
		declared = parseToolsList(toolsText)
	}

	// Translation (A19) and the per-mode vocabulary.
	seen := map[string]bool{}
	set := toolsOf(extraTools)
	for _, name := range declared {
		switch {
		case strings.HasPrefix(name, "filesystem_"):
			// The v1 wildcard permission — dead config in this product.
			if original {
				d.RetiredFound = append(d.RetiredFound, name)
			} else {
				return Definition{}, reject(path, "tool %q is retired (W1) — the filesystem is what the tools operate on, not a tool", name)
			}
			continue
		case name == "task":
			// DROPPED. Not renamed — a rename reintroduces delegation
			// through a config file (A1, D4a). In a rewrite it must not
			// even be declared.
			if !original {
				return Definition{}, reject(path, "tool %q is refused: A1 removed spawning — dropping it is a translation, renaming it would reintroduce delegation through a config file", name)
			}
			d.RetiredFound = append(d.RetiredFound, name)
			continue
		case name == "todowrite":
			if original {
				d.RetiredFound = append(d.RetiredFound, name)
			} else {
				return Definition{}, reject(path, "tool %q is the v1 name — the rewrite declares todo (A19 translates; a rewrite keeping the old name hides the translation)", name)
			}
			name = "todo"
		case retiredNames[name]:
			if !original {
				return Definition{}, reject(path, "tool %q is retired (W1): its capability left with the v1 tool surface", name)
			}
			d.RetiredFound = append(d.RetiredFound, name)
			continue
		}
		if forbiddenCapabilities[name] {
			return Definition{}, reject(path, "tool %q names a capability A1 removed (nothing spawns) — no registry entry may be named after starting another step", name)
		}
		if !set[name] {
			return Definition{}, reject(path, "tool %q is not in the tool registry (O1: the set is read from tools.ToolNames, not remembered) — name a real tool or drop the entry", name)
		}
		if !seen[name] {
			seen[name] = true
			d.Tools = append(d.Tools, name)
		}
	}
	if len(d.Tools) == 0 {
		return Definition{}, reject(path, "the tools allow-list translated to nothing — a handler with no tools cannot act; the list may be narrow but never empty")
	}

	// handoff — loaded raw; T3 validates (purposes required, etc.).
	if h := fields["handoff"]; strings.TrimSpace(h) != "" {
		var doc struct {
			Handoff map[string]string `yaml:"handoff"`
		}
		if err := yaml.Unmarshal([]byte("handoff:\n"+indentBlock(h)), &doc); err != nil {
			return Definition{}, reject(path, "handoff must map each field to its purpose: %v", err)
		}
		d.Handoff = doc.Handoff
	}

	// tech — the tags this agent serves (optional; the vocabulary is closed, so
	// a typo is refused by name rather than becoming an agent nobody dispatches).
	if t := fields["tech"]; strings.TrimSpace(t) != "" {
		var doc struct {
			Tech []string `yaml:"tech"`
		}
		if err := yaml.Unmarshal([]byte("tech:\n"+indentBlock(t)), &doc); err != nil {
			return Definition{}, reject(path, "tech is not a list of tags: %v", err)
		}
		tags, err := NormalizeTech(doc.Tech)
		if err != nil {
			return Definition{}, reject(path, "tech: %v", err)
		}
		d.Tech = tags
	}

	d.PolicyAgent = strings.TrimSpace(fields["policy-agent"])
	agent := d.PolicyAgent
	if agent == "" {
		agent = d.Name
	}
	// A16: policy from the extractor only. Absent corpus entry is legal —
	// 25 of 26 originals carry permission (project-planner-loop has none),
	// and the rewrites inherit through policy-agent — but its ABSENCE is
	// recorded as nil, never as an empty slice dressed up as rules.
	corpus, crej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if crej != nil {
		return Definition{}, reject(path, "the embedded corpus refused to parse: %v", crej)
	}
	if rules, found := corpus.RulesFor(agent); found {
		d.CommandRules = rules
	}

	return d, nil
}

// frontFields reads the top-level key → raw-text mapping. Strict parse
// first; on failure, per-line scalar extraction while structured blocks
// keep their strict path (the two measured offenders break only their
// description scalars).
func frontFields(fm, path string) (map[string]string, *Reject) {
	// Strip the permission block before any parse: the loader never reads
	// it (A16) and its content is the corpus's to interpret.
	fmStripped := stripPermissionBlock(fm)

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fmStripped), &doc); err == nil && len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		out := map[string]string{}
		root := doc.Content[0]
		for i := 0; i+1 < len(root.Content); i += 2 {
			key, val := root.Content[i], root.Content[i+1]
			if key.Kind != yaml.ScalarNode {
				return nil, reject(path, "front matter carries a non-scalar key")
			}
			out[key.Value] = nodeText(val)
		}
		return out, nil
	}

	// Fallback: top-level lines delimit keys; the value is everything until
	// the next top-level key. Structured values (tools:, handoff:) are still
	// strictly parsed by their readers; only scalars are taken raw —
	// exactly the corpusgen division, and pinned by TestStrictFailuresAreTheTwoMeasuredFiles.
	out := map[string]string{}
	var cur string
	var buf []string
	flush := func() {
		if cur != "" {
			out[cur] = strings.Join(buf, "\n")
		}
		cur, buf = "", nil
	}
	for _, line := range strings.Split(fmStripped, "\n") {
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			if cur != "" {
				buf = append(buf, line)
			}
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			return nil, reject(path, "front matter line %q is neither indented under a key nor a key itself", strings.TrimSpace(line))
		}
		flush()
		cur = strings.TrimSpace(line[:i])
		if rest := strings.TrimSpace(line[i+1:]); rest != "" {
			buf = []string{rest}
		} else {
			buf = []string{}
		}
	}
	flush()
	return out, nil
}

// stripPermissionBlock removes the top-level `permission:` key and its
// indented continuation — the loader's A16 boundary made executable.
func stripPermissionBlock(fm string) string {
	var out []string
	skipping := false
	for _, line := range strings.Split(fm, "\n") {
		if line == "permission:" || strings.HasPrefix(line, "permission: ") {
			skipping = true
			continue
		}
		if skipping {
			if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
				continue
			}
			skipping = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func nodeText(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		var b strings.Builder
		for i := 0; i+1 < len(n.Content); i += 2 {
			fmt.Fprintf(&b, "%s: %s\n", n.Content[i].Value, scalarOrBlock(n.Content[i+1]))
		}
		return b.String()
	case yaml.SequenceNode:
		var b strings.Builder
		for _, c := range n.Content {
			fmt.Fprintf(&b, "- %s\n", c.Value)
		}
		return b.String()
	}
	return n.Value
}

func scalarOrBlock(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	return nodeText(n)
}

func indentBlock(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}

// mapLike reports whether the tools text reads as `key: value` entries
// rather than a `- name` list or a flow `[a, b]` list.
func mapLike(toolsText string) bool {
	t := strings.TrimSpace(toolsText)
	if strings.HasPrefix(t, "[") {
		return false
	}
	for _, l := range strings.Split(t, "\n") {
		s := strings.TrimSpace(l)
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "-") {
			continue
		}
		if strings.Contains(s, ":") {
			return true
		}
	}
	return false
}

func parseToolsList(toolsText string) []string {
	var out []string
	for _, l := range strings.Split(toolsText, "\n") {
		s := strings.TrimSpace(l)
		if strings.HasPrefix(s, "[") {
			// flow list
			for _, p := range strings.Split(strings.Trim(s, "[]"), ",") {
				if n := strings.Trim(strings.TrimSpace(p), `"'`); n != "" {
					out = append(out, n)
				}
			}
			continue
		}
		s = strings.TrimPrefix(s, "- ")
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, strings.Trim(s, `"'`))
	}
	return out
}

func parseToolsMap(toolsText string) (declared, falseEntries []string) {
	for _, l := range strings.Split(toolsText, "\n") {
		s := strings.TrimSpace(l)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if i := strings.Index(s, ":"); i >= 0 {
			name := strings.Trim(strings.TrimSpace(s[:i]), `"'`)
			val := strings.TrimSpace(s[i+1:])
			// strip inline comment
			if c := strings.Index(val, "#"); c >= 0 {
				val = strings.TrimSpace(val[:c])
			}
			if name == "" {
				continue
			}
			if val == "true" {
				declared = append(declared, name)
			} else {
				falseEntries = append(falseEntries, name)
			}
		}
	}
	return declared, falseEntries
}

func falseEntriesFrom(toolsText string) []string {
	_, f := parseToolsMap(toolsText)
	return f
}

func quoteAll(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = `"` + x + `"`
	}
	return out
}
