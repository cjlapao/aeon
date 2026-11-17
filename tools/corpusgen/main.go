// Command corpusgen extracts the command-policy corpus — the permission.bash
// rules this repository's own agent definitions author — into the committed
// corpus the policy engine's tests are written against.
//
// The corpus IS the specification (S11): the matcher's tables, the corpus-driven
// evaluation and the seeding verb all enumerate from the checked-in file, never
// from opencode/agents/ directly. So this generator is written against two
// rules:
//
//   - Properties, never counts. The emitted document carries facts ("some
//     pattern has '*' with text after it", "these patterns carry more than one
//     verdict"), not transcribed numbers. A count assertion goes stale the
//     moment an operator edits an agent file; a property does not.
//   - Read-only ground. The extractor reads <repo>/opencode/agents/*.md with a
//     non-recursive os.ReadDir — never depricated/ or reference-edition/, which
//     also carry agent files with bash blocks and are not the live corpus — and
//     writes nothing under opencode/. A test snapshots the source tree and
//     proves it byte-for-byte.
//
// The shape mirrors tools/protogen: a --check flag, a generate that returns
// bytes, and a main that writes or refuses.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	generatedBy = "aeon/tools/corpusgen"

	// outPath is relative to the working directory, exactly as protogen writes
	// ui/src/proto/contract.ts: both Makefile targets run with cwd = aeon/.
	outPath = "internal/policy/testdata/corpus.json"

	// wildcardCandidates is the probe set for the wildcard_chars property —
	// the literal characters '?', '[' and '{', not a set containing a space.
	// S11's matcher treats '*' as the only wildcard — every other character
	// is literal, deliberately, because measured corpus patterns only ever
	// need '*' — but if an operator authors a '?' or '[' pattern the property
	// surfaces it as corpus data instead of a matcher change nobody noticed.
	wildcardCandidates = "*?[{"

	// generatedBySuffix is the tail a default-discovered agents directory
	// always ends with; Source records it repo-relative so the committed
	// bytes are identical on every checkout regardless of clone location.
	generatedByTail = "opencode/agents"
)

// verdicts is the closed vocabulary an authored rule may carry. A typo'd
// verdict in a guard's source data stops the build; it does not become an
// inert row nobody notices.
var verdicts = map[string]bool{"allow": true, "deny": true, "ask": true}

type rule struct {
	Pattern string `json:"pattern"`
	Verdict string `json:"verdict"` // "allow" | "deny" | "ask", exactly as authored
}

type corpusAgent struct {
	Agent string `json:"agent"` // file base name without .md — e.g. "git-loop"
	Rules []rule `json:"rules"` // AUTHORED order, never sorted
}

type corpusProperties struct {
	WildcardChars       []string `json:"wildcard_chars"`       // sorted set of metacharacters present; today ["*"]
	HasMidStarPattern   bool     `json:"has_mid_star_pattern"` // some pattern has '*' with non-space text after it
	HasExactPattern     bool     `json:"has_exact_pattern"`    // some pattern contains no '*'
	ConflictingPatterns []string `json:"conflicting_patterns"` // sorted patterns carrying >1 verdict across agents
}

type corpusDoc struct {
	GeneratedBy string           `json:"generated_by"`
	Source      string           `json:"source"` // the agentsDir as passed, repo-normalised
	Properties  corpusProperties `json:"properties"`
	Agents      []corpusAgent    `json:"agents"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run holds the whole verb so the --check, write and named-skip paths are all
// testable without os.Exit. It returns the process exit code; diagnostics go
// to the writers it is handed.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("corpusgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	check := fs.Bool("check", false, "fail if the checked-in corpus differs from the agent definitions")
	agents := fs.String("agents", "", "directory of agent .md files to read (default: the repository root's opencode/agents, discovered by walking up from the working directory)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	dir, found := resolveAgentsDir(*agents, cwd)
	if !found {
		// The input lives OUTSIDE aeon/ and after the P24 extraction it may not
		// exist at all. A missing agents directory is therefore a NAMED skip,
		// not a failure — the same convention as `make test` naming why the
		// Rust half did not run.
		fmt.Fprintf(stdout, "corpus: skipped (no agents directory at %s)\n", dir)
		return 0
	}

	if *check {
		want, err := generate(dir, "")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		old, err := os.ReadFile(outPath)
		if err != nil || !bytes.Equal(old, want) {
			fmt.Fprintf(stderr, "corpus.json is stale; run make corpusgen\n")
			return 1
		}
		return 0
	}

	if _, err := generate(dir, outPath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// resolveAgentsDir answers where the corpus sources are. An explicit --agents
// value is used verbatim; otherwise the directory is discovered by walking up
// from cwd until a directory contains BOTH aeon and opencode — the sibling
// relation is computed at run time, never spelled as a plan path. The first
// return is the path tried, so the named-skip message always says what was
// looked for.
func resolveAgentsDir(explicit, cwd string) (string, bool) {
	if explicit != "" {
		return explicit, isDir(explicit)
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		dir = cwd
	}
	for {
		if isDir(filepath.Join(dir, "aeon")) && isDir(filepath.Join(dir, "opencode")) {
			cand := filepath.Join(dir, generatedByTail)
			return cand, isDir(cand)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(cwd, "..", generatedByTail), false
		}
		dir = parent
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// generate renders the corpus from agentsDir. When outPath is non-empty the
// bytes are also written there (parent created); the bytes are always returned
// so callers — main's --check and the tests — compare without a second parse.
func generate(agentsDir, outPath string) ([]byte, error) {
	data, err := render(agentsDir)
	if err != nil {
		return nil, err
	}
	if outPath == "" {
		return data, nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return nil, err
	}
	return data, nil
}

func render(agentsDir string) ([]byte, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, err
	}
	var agents []corpusAgent
	verdictByPattern := map[string]map[string]bool{}
	charsSeen := map[string]bool{}
	var hasMidStar, hasExact bool

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(agentsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		front, ok := frontMatter(raw)
		if !ok {
			continue // no front matter at all: no rules authored
		}
		rules, err := bashRules(front)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if rules == nil {
			// No permission.bash block: OMITTED. "No rules authored" and
			// "rules authored, all of them empty" are different claims and the
			// reader should not have to guess which it got.
			continue
		}
		agents = append(agents, corpusAgent{
			Agent: strings.TrimSuffix(entry.Name(), ".md"),
			Rules: rules,
		})
		for _, r := range rules {
			if verdictByPattern[r.Pattern] == nil {
				verdictByPattern[r.Pattern] = map[string]bool{}
			}
			verdictByPattern[r.Pattern][r.Verdict] = true
			for _, c := range r.Pattern {
				if strings.ContainsRune(wildcardCandidates, c) {
					charsSeen[string(c)] = true
				}
			}
			if star := strings.IndexByte(r.Pattern, '*'); star >= 0 {
				if strings.TrimSpace(r.Pattern[star+1:]) != "" {
					hasMidStar = true
				}
			} else {
				hasExact = true
			}
		}
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Agent < agents[j].Agent })
	if agents == nil {
		agents = []corpusAgent{}
	}

	var wildcardChars []string
	for c := range charsSeen {
		wildcardChars = append(wildcardChars, c)
	}
	sort.Strings(wildcardChars)
	if wildcardChars == nil {
		wildcardChars = []string{}
	}
	var conflicting []string
	for pattern, vs := range verdictByPattern {
		if len(vs) > 1 {
			conflicting = append(conflicting, pattern)
		}
	}
	sort.Strings(conflicting)
	if conflicting == nil {
		conflicting = []string{}
	}

	doc := corpusDoc{
		GeneratedBy: generatedBy,
		Source:      stableSource(agentsDir),
		Properties: corpusProperties{
			WildcardChars:       wildcardChars,
			HasMidStarPattern:   hasMidStar,
			HasExactPattern:     hasExact,
			ConflictingPatterns: conflicting,
		},
		Agents: agents,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// stableSource keeps the committed bytes checkout-location independent: a
// default-discovered directory (anything ending in opencode/agents) is
// recorded repo-relatively; an explicit --agents override is recorded as
// passed.
func stableSource(agentsDir string) string {
	slash := filepath.ToSlash(agentsDir)
	if strings.HasSuffix(slash, generatedByTail) {
		return generatedByTail
	}
	return slash
}

// frontMatter splits the YAML front matter on its leading --- fence. It
// returns ok=false when the file carries none, which is a corpus omission,
// not a parse failure.
func frontMatter(raw []byte) (string, bool) {
	s := strings.TrimPrefix(string(raw), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", false
	}
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" || lines[i] == "..." {
			return strings.Join(lines[1:i], "\n"), true
		}
	}
	return "", false
}

// bashRules decodes the permission.bash mapping of a front-matter document
// IN AUTHOR ORDER. Order is a fidelity property the policy tests assert, so
// the decode goes through yaml.Node and walks Node.Content pairwise — a
// map[string]string decode would silently sort into the operator's intent
// being lost.
//
// The returned slice is nil when no permission.bash block exists (the agent
// is omitted from the corpus); an EMPTY non-nil slice means the block exists
// and carries zero rules, which the document renders as [] on purpose — the
// two claims differ.
//
// It refuses: a malformed permission block, a non-string key, a non-string
// verdict, and any verdict outside allow|deny|ask.
//
// Four live corpus files (git-loop among them) author a long unquoted
// description whose plain scalar contains ": ", which strict YAML rejects
// even though the permission block itself is well formed. Rather than lose
// those rules, a strict parse failure falls back to extracting and strictly
// parsing the permission block ALONE: the fidelity-critical structure stays
// strictly validated, and only presentation-layer breakage in fields the
// corpus never reads is tolerated.
func bashRules(front string) ([]rule, error) {
	root, err := parseFrontMatter(front)
	if err != nil {
		return nil, err
	}
	perm := findPair(root, "permission")
	if perm == nil || perm.Kind != yaml.MappingNode {
		return nil, nil // no permission map (or none at all): no bash authored
	}
	bashIdx := pairIndex(perm, "bash")
	if bashIdx < 0 {
		return nil, nil
	}
	bash := perm.Content[bashIdx]
	if bash.Kind == yaml.ScalarNode && bash.Tag == "!!null" {
		// `bash:` authored with no content — same claim as `bash: {}`:
		// authored, and empty. Emit [], do not omit.
		return []rule{}, nil
	}
	if bash.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("permission.bash must be a mapping, got %s", bash.Tag)
	}
	var rules []rule
	for i := 0; i+1 < len(bash.Content); i += 2 {
		key, value := bash.Content[i], bash.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, fmt.Errorf("permission.bash carries a non-string pattern (kind %d tag %s)", key.Kind, key.Tag)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return nil, fmt.Errorf("permission.bash pattern %q carries a non-string verdict (kind %d tag %s)", key.Value, value.Kind, value.Tag)
		}
		if !verdicts[value.Value] {
			return nil, fmt.Errorf("permission.bash pattern %q carries unknown verdict %q (want allow|deny|ask)", key.Value, value.Value)
		}
		rules = append(rules, rule{Pattern: key.Value, Verdict: value.Value})
	}
	if rules == nil {
		rules = []rule{}
	}
	return rules, nil
}

// parseFrontMatter returns the front-matter document root, strict-first; on a
// strict failure it re-parses just the top-level permission block so a broken
// plain-scalar description elsewhere cannot hide a well-formed rules table.
func parseFrontMatter(front string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err == nil {
		if len(doc.Content) == 0 {
			return nil, nil
		}
		return doc.Content[0], nil
	}
	block, ok := permissionBlock(front)
	if !ok {
		return nil, nil // no permission block anywhere: nothing this corpus reads is broken
	}
	var retry yaml.Node
	if err := yaml.Unmarshal([]byte(block), &retry); err != nil {
		return nil, fmt.Errorf("malformed permission block: %w", err)
	}
	if len(retry.Content) == 0 {
		return nil, nil
	}
	return retry.Content[0], nil
}

// permissionBlock extracts the top-level `permission:` mapping — the line
// itself plus the indented (or blank/comment) lines that follow it, until the
// next top-level key — wrapped so it parses as YAML on its own.
func permissionBlock(fm string) (string, bool) {
	lines := strings.Split(fm, "\n")
	start := -1
	for i, line := range lines {
		if line == "permission:" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	var body []string
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			body = append(body, line)
			continue
		}
		break
	}
	return "permission:\n" + strings.Join(body, "\n"), true
}

func findPair(mapping *yaml.Node, key string) *yaml.Node {
	idx := pairIndex(mapping, key)
	if idx < 0 {
		return nil
	}
	return mapping.Content[idx]
}

func pairIndex(mapping *yaml.Node, key string) int {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key && mapping.Content[i].Kind == yaml.ScalarNode {
			return i + 1
		}
	}
	return -1
}
