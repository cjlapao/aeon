package skills

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// parse.go splits SKILL.md into frontmatter and body and decodes the
// frontmatter LENIENTLY. Lenient is not sloppy — it is the documented
// behaviour of both readers this slice must agree with: Claude Code loads a
// skill whose frontmatter it cannot parse with EMPTY metadata (the skill
// still exists), and Codex carries fields it does not model (metadata,
// agents/openai.yaml neighbours) without complaint. A strict loader turns
// every ecosystem extension into a missing skill; a lenient one records the
// disagreement as an issue and keeps the skill visible.
//
// What stays strict is VALIDATION (validate.go), not parsing: the spec's
// constraints are checked and reported, never silently applied by deletion.

// Frontmatter is the decoded frontmatter block. Every field is the spec's;
// Unknown is everything else, kept as display strings so the bench can show
// an operator what a foreign tool put in their file. Warnings are parse-time
// observations (a field had the wrong shape) that validation folds into the
// issue list — decoding never fails the skill, it reports.
type Frontmatter struct {
	// Present is false when the file carried no `---` block at all. That is
	// an invalid skill per the spec (name and description are required);
	// the distinction from "block present, fields missing" is kept because
	// the two need different sentences.
	Present bool

	Name          string
	Description   string
	License       string
	Compatibility string
	AllowedTools  string
	Metadata      map[string]string
	Unknown       map[string]string

	// ParseError is the YAML decoder's own sentence when the block did not
	// parse, or the structural complaint when it did not close.
	ParseError string

	// Notes are shape complaints (a scalar where a map was expected) that
	// did not cost the field's VALUE entirely.
	Notes []string
}

// frontSplit is splitFrontmatter's outcome: where the block was and what
// came after it. Problem is the loader's sentence when the block is
// malformed at the STRUCTURAL level (opened, never closed) — the decoder's
// complaints belong to ParseFrontmatter instead.
type frontSplit struct {
	Present  bool
	FM       string
	Body     string
	Problem  string
	BodyText string
}

// splitFrontmatter extracts the leading `---` … `---` (or `---` … `...`)
// block. BOM and CRLF are tolerated: the ecosystem ships Windows-authored
// skills and editors that add BOMs, and both readers do the same, so
// refusing on them would refuse real skills over an editor habit.
func splitFrontmatter(data []byte) frontSplit {
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")

	lines := strings.Split(text, "\n")

	// The block must OPEN on the first non-blank line. Leading blank lines
	// are tolerated (an editor left one); any other content before `---`
	// is body, not frontmatter — which reads as "no frontmatter", exactly
	// the fact the caller must be told.
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return frontSplit{BodyText: text}
	}

	var fm []string
	closed := false
	for j := i + 1; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "---" || t == "..." {
			closed = true
			return frontSplit{
				Present:  true,
				FM:       strings.Join(fm, "\n"),
				BodyText: strings.Join(lines[j+1:], "\n"),
				Body:     strings.TrimSpace(strings.Join(lines[j+1:], "\n")),
			}
		}
		fm = append(fm, lines[j])
	}
	if !closed {
		return frontSplit{
			Present:  true,
			Problem:  "the frontmatter block opens with --- but never closes — everything after the opening marker is being read as metadata, which is never what the author meant",
			BodyText: text,
		}
	}
	return frontSplit{BodyText: text}
}

// ParseFrontmatter decodes the block. The known keys are read per the spec;
// anything else lands in Unknown. A YAML error is recorded verbatim rather
// than paraphrased — the operator fixes the file, and the decoder's line
// numbers are the fix's map.
func ParseFrontmatter(fmText string) Frontmatter {
	fm := Frontmatter{Present: true, Metadata: map[string]string{}, Unknown: map[string]string{}}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fmText), &raw); err != nil {
		fm.ParseError = err.Error()
		return fm
	}
	if raw == nil { // an empty block parses to nil; it is present and empty
		return fm
	}

	fm.Name = scalarString(raw["name"])
	fm.Description = scalarString(raw["description"])
	fm.License = scalarString(raw["license"])
	fm.Compatibility = scalarString(raw["compatibility"])
	fm.AllowedTools = toolString(raw["allowed-tools"])

	if v, ok := raw["metadata"]; ok && v != nil {
		m, ok := v.(map[string]any)
		if !ok {
			fm.Notes = append(fm.Notes, "metadata is not a mapping — the spec defines it as string keys to string values, and the value is kept as text")
			fm.Metadata["value"] = fmt.Sprint(v)
		} else {
			for k, mv := range m {
				switch mv.(type) {
				case nil, string, int, int64, uint64, float64, bool:
					fm.Metadata[k] = scalarString(mv)
				default:
					// A map/list under metadata keeps the spec's shape
					// promise unmet; the value is preserved for display
					// and the complaint is recorded rather than hidden.
					fm.Notes = append(fm.Notes, fmt.Sprintf("metadata.%s is not a string — the spec maps string keys to string values", k))
					fm.Metadata[k] = displayString(mv)
				}
			}
		}
	}

	known := map[string]bool{"name": true, "description": true, "license": true,
		"compatibility": true, "allowed-tools": true, "metadata": true}
	for k, v := range raw {
		if known[k] {
			continue
		}
		fm.Unknown[k] = displayString(v)
	}
	return fm
}

// scalarString reads a scalar as text. Numbers and bools are accepted and
// stringified (metadata.version: 1.0 is a real YAML shape authors ship; the
// spec's "string values" is the goal, not a gate) — but a map or a list is
// not a scalar, and answering a name field with the rendering of a map
// would be the plausible wrong answer, so those read as "".
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int, int64, uint64, float64, bool:
		return fmt.Sprint(t)
	default:
		return ""
	}
}

// toolString reads allowed-tools. The spec says one space-separated string;
// real skills sometimes ship a YAML list, and Claude Code splits on spaces
// either way — so a list joins with spaces and a scalar passes through.
func toolString(v any) string {
	if list, ok := v.([]any); ok {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			if s := displayString(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return scalarString(v)
}

// displayString is the Unknown-map renderer: scalars verbatim, composites
// via Go's map/slice printing. These values are for a human looking at a
// file their tool wrote, not for a decoder.
func displayString(v any) string {
	if s := scalarString(v); s != "" {
		return s
	}
	switch t := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, displayString(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(t))
		for _, k := range keys {
			parts = append(parts, k+"="+displayString(t[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(v)
	}
}
