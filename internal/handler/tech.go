package handler

import (
	"fmt"
	"sort"
	"strings"
)

// tech.go — the tech vocabulary and the by-tech-tag resolver.
//
// An agent declares the tech it works in (`tech: [go]`, `tech: [typescript,
// react, frontend]`); a step that says `agent: by-tech-tag` names the tech it
// needs in `with.tech`, and the resolver picks among the agents that declare it.
// The vocabulary is CLOSED and owned here: the UI offers exactly these tags,
// and an agent cannot carry one nobody compiled. What is NOT here any more is
// a table of which agent serves which tag — that is the agents' own data, so an
// agent a person creates joins the dispatch by declaring its tech.
//
// The choice stays deterministic and never silent: an agent whose tags contain
// every tag asked for is a candidate; the one with the fewest EXTRA tags (the
// most specific) wins; a tie, or nothing, is a structured refusal that names
// what exists. There is no fallback — a generic agent writing Elixir produces
// something plausible and the loop reports ok (A13, D11).

// TechTag is one entry of the vocabulary.
type TechTag struct {
	Tag   string `json:"tag"`
	Label string `json:"label"`
	// Group says what kind of thing the tag is, so the UI can group a long list.
	Group string `json:"group"` // language | framework | platform | area
}

var techVocabulary = []TechTag{
	{"go", "Go", "language"}, {"python", "Python", "language"}, {"javascript", "JavaScript", "language"},
	{"typescript", "TypeScript", "language"}, {"rust", "Rust", "language"}, {"sql", "SQL", "language"},
	{"java", "Java", "language"}, {"kotlin", "Kotlin", "language"}, {"csharp", "C#", "language"},
	{"ruby", "Ruby", "language"}, {"php", "PHP", "language"}, {"elixir", "Elixir", "language"},
	{"react", "React", "framework"}, {"vue", "Vue", "framework"}, {"angular", "Angular", "framework"},
	{"svelte", "Svelte", "framework"}, {"tauri", "Tauri", "framework"}, {"websocket", "WebSocket", "framework"},
	{"docker", "Docker", "platform"}, {"kubernetes", "Kubernetes", "platform"}, {"terraform", "Terraform", "platform"},
	{"frontend", "Frontend", "area"}, {"backend", "Backend", "area"}, {"fullstack", "Full stack", "area"},
	{"mobile", "Mobile", "area"}, {"devops", "DevOps", "area"}, {"security", "Security", "area"},
	{"testing", "Testing", "area"}, {"docs", "Documentation", "area"},
}

// TechVocabulary returns a COPY of the closed vocabulary, in display order.
func TechVocabulary() []TechTag { return append([]TechTag(nil), techVocabulary...) }

// IsTechTag reports whether a tag is in the vocabulary.
func IsTechTag(tag string) bool {
	for _, t := range techVocabulary {
		if t.Tag == tag {
			return true
		}
	}
	return false
}

// legacyTags are the single words the old dispatch table accepted, each
// expanded to the vocabulary tags it means. A blueprint written against the
// table (`with.tech: react-ts`) keeps resolving.
var legacyTags = map[string][]string{
	"react-ts":    {"typescript", "react"},
	"vue-ts":      {"typescript", "vue"},
	"tauri-react": {"tauri", "react", "typescript"},
	"tauri-vue":   {"tauri", "vue", "typescript"},
}

// ParseTechRequest reads a step's `with.tech` (one tag, or several separated by
// commas or spaces) into vocabulary tags, expanding the legacy words. An empty
// request is empty — the caller refuses it; no tag is invented.
func ParseTechRequest(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		tags := []string{raw}
		if l, ok := legacyTags[raw]; ok {
			tags = l
		}
		for _, t := range tags {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// NormalizeTech cleans an agent's declared tech: trimmed, lower-cased,
// de-duplicated, sorted, and every tag checked against the vocabulary. An
// unknown tag is an error naming it and the vocabulary — a typo'd tag is an
// agent that silently never gets dispatched.
func NormalizeTech(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if !IsTechTag(t) {
			return nil, fmt.Errorf("unknown tech tag %q — the tags are: %s", t, strings.Join(techTagNames(), ", "))
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

func techTagNames() []string {
	out := make([]string, 0, len(techVocabulary))
	for _, t := range techVocabulary {
		out = append(out, t.Tag)
	}
	return out
}

// TechAgent is what the resolver needs of an agent: its key and its tags.
type TechAgent struct {
	Slug string
	Tech []string
}

// Refusal is the structured form of "no agent for that tech" — Available lists
// the tags agents DO declare, never agent names (O4: a person asked for elixir;
// they need to know which tags exist).
type Refusal struct {
	Tag       string
	Available []string // sorted
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("no agent declares tech %q\navailable: %s", r.Tag, strings.Join(r.Available, ", "))
}

// Kind is the routing vocabulary: callers route on this, not on parsing text.
func (r *Refusal) Kind() string { return "unknown-tech-tag" }

// Ambiguity is the refusal for a tie: more than one agent is equally specific.
// It names them, because here the person can act on it (tag one more
// specifically, or untag one).
type Ambiguity struct {
	Tag    string
	Agents []string // sorted slugs
}

func (a *Ambiguity) Error() string {
	return fmt.Sprintf("more than one agent matches tech %q equally well: %s\nname one more tag to choose between them, or remove the tag from all but one", a.Tag, strings.Join(a.Agents, ", "))
}

func (a *Ambiguity) Kind() string { return "ambiguous-tech-tag" }

// ResolveByTech picks the agent for a tech request. A candidate declares every
// requested tag; the fewest extra tags wins; a tie is an *Ambiguity and no
// candidate is an *Refusal. The inputs are the request and the agents — no
// config, no environment, no fallback.
func ResolveByTech(request string, agents []TechAgent) (string, error) {
	want := ParseTechRequest(request)
	label := strings.TrimSpace(request)
	if len(want) == 0 {
		return "", &Refusal{Tag: label, Available: availableTech(agents)}
	}
	best, extra := []string(nil), -1
	for _, a := range agents {
		have := map[string]bool{}
		for _, t := range a.Tech {
			have[t] = true
		}
		ok := true
		for _, w := range want {
			if !have[w] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		e := len(have) - len(want)
		switch {
		case extra < 0 || e < extra:
			best, extra = []string{a.Slug}, e
		case e == extra:
			best = append(best, a.Slug)
		}
	}
	switch len(best) {
	case 0:
		return "", &Refusal{Tag: label, Available: availableTech(agents)}
	case 1:
		return best[0], nil
	}
	sort.Strings(best)
	return "", &Ambiguity{Tag: label, Agents: best}
}

func availableTech(agents []TechAgent) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range agents {
		for _, t := range a.Tech {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Strings(out)
	return out
}
