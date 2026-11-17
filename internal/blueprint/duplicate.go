package blueprint

// duplicate.go — S48 T2 (BP12): duplicating a blueprint is a TEXT edit, not
// a re-serialization. The client-side `Parse → set ID/Version → yaml.Marshal`
// destroyed comments, key order, blank lines and line endings — a duplicate
// that "preserves comments" cannot go through a struct. This rewrites ONLY
// the top-level `id:` scalar (to the new name) and the top-level `version:`
// scalar (to 1), byte-splicing at the positions the yaml.Node parse reports;
// every other byte of the source survives untouched. The parse is the GUARD:
// both keys must be top-level plain/quoted scalars, unanchored, single-line
// — anchors, aliases, block scalars and multi-line values are refused rather
// than guessed at.

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Duplicate returns src with the top-level id rewritten to newName and the
// top-level version rewritten to 1. It refuses (error, never a wrong
// rewrite): a name outside the grammar; YAML whose root is not a mapping; a
// missing id or version; a key appearing twice; and either value not being a
// plain-or-quoted single-line unanchored scalar. The result is re-parsed and
// must load as ID == newName, Version == 1, or nothing is returned.
func Duplicate(src []byte, newName string) ([]byte, error) {
	if !blueprintName.MatchString(newName) {
		return nil, fmt.Errorf("duplicate: invalid blueprint name %q", newName)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("duplicate: parse: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("duplicate: the YAML root must be a mapping")
	}
	root := doc.Content[0]
	idVal, verVal, err := topScalar(root, "id", "version")
	if err != nil {
		return nil, fmt.Errorf("duplicate of %q: %w", newName, err)
	}
	// Splice the LATER value first: rewriting `version` before `id` cannot
	// shift anything (both edits are same-line spans and lines never move),
	// but going right-to-left by (line, column) makes that immunity
	// structural rather than an argument.
	type edit struct {
		node *yaml.Node
		text string
	}
	edits := []edit{{idVal, newName}, {verVal, "1"}}
	if lessPos(edits[0].node, edits[1].node) {
		edits[0], edits[1] = edits[1], edits[0]
	}
	out := src
	for _, e := range edits {
		out, err = spliceScalar(out, e.node, e.text)
		if err != nil {
			return nil, fmt.Errorf("duplicate of %q: %w", newName, err)
		}
	}

	// The proof, not a hope: the result must load as the copy it claims.
	b, err := Load(out)
	if err != nil {
		return nil, fmt.Errorf("duplicate of %q produced an unloadable copy: %w", newName, err)
	}
	if b.ID != newName || b.Version != 1 {
		return nil, fmt.Errorf("duplicate of %q did not rewrite id/version (got %q@%d)", newName, b.ID, b.Version)
	}
	return out, nil
}

// topScalar locates the two named keys at the TOP level of the mapping and
// returns their value nodes in order, after proving each is a rewritable
// scalar.
func topScalar(root *yaml.Node, first, second string) (*yaml.Node, *yaml.Node, error) {
	found := map[string]*yaml.Node{}
	mark := func(want string, val *yaml.Node) error {
		if _, dup := found[want]; dup {
			return fmt.Errorf("two top-level %q keys — the text would be ambiguous to rewrite", want)
		}
		if err := rewritable(val, want); err != nil {
			return err
		}
		found[want] = val
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch key.Value {
		case first:
			if err := mark(first, val); err != nil {
				return nil, nil, err
			}
		case second:
			if err := mark(second, val); err != nil {
				return nil, nil, err
			}
		}
	}
	a, okA := found[first]
	b, okB := found[second]
	if !okA {
		return nil, nil, fmt.Errorf("no top-level %q key to rewrite", first)
	}
	if !okB {
		return nil, nil, fmt.Errorf("no top-level %q key to rewrite", second)
	}
	return a, b, nil
}

func rewritable(v *yaml.Node, key string) error {
	if v.Kind != yaml.ScalarNode {
		return fmt.Errorf("%s must be a scalar to rewrite, not a %s", key, kindName(v.Kind))
	}
	if v.Anchor != "" {
		return fmt.Errorf("%s carries the anchor %q — rewriting under an anchor would silently change what aliases resolve to; unwrap it first", key, v.Anchor)
	}
	if v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return fmt.Errorf("%s is a block scalar — a line splice cannot rewrite it safely", key)
	}
	if strings.ContainsAny(v.Value, "\n\r") {
		return fmt.Errorf("%s spans more than one line — refusing a rewrite that could not preserve the text", key)
	}
	return nil
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.AliasNode:
		return "alias"
	default:
		return "non-scalar"
	}
}

func lessPos(a, b *yaml.Node) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Column < b.Column
}

// spliceScalar replaces the bytes the value node occupies with text. The
// node's Line/Column point AT the value (1-based); quoted styles include
// their quotes in the span so a quoted scalar stays rewritten in place.
func spliceScalar(src []byte, v *yaml.Node, text string) ([]byte, error) {
	if v.Line < 1 || v.Column < 1 {
		return nil, fmt.Errorf("node carries no position")
	}
	lines := splitKeepingNewlines(string(src))
	if v.Line > len(lines) {
		return nil, fmt.Errorf("node line %d past the file", v.Line)
	}
	line := lines[v.Line-1]
	start := v.Column - 1
	if start > len(line) {
		return nil, fmt.Errorf("node column %d past the line", v.Column)
	}
	end := start + len(v.Value)
	if v.Style == yaml.SingleQuotedStyle || v.Style == yaml.DoubleQuotedStyle {
		quote := byte('\'')
		if v.Style == yaml.DoubleQuotedStyle {
			quote = '"'
		}
		if start >= len(line) || line[start] != quote {
			return nil, fmt.Errorf("the quoted scalar is not where the parse says it is")
		}
		close := strings.IndexByte(line[start+1:], quote)
		if close < 0 {
			return nil, fmt.Errorf("the quoted scalar never closes")
		}
		end = start + 1 + close + 1
	} else if line[start:min(end, len(line))] != v.Value {
		return nil, fmt.Errorf("the scalar is not where the parse says it is")
	}
	if end > len(line) {
		return nil, fmt.Errorf("the scalar runs past its line")
	}
	lines[v.Line-1] = line[:start] + text + line[end:]
	return []byte(strings.Join(lines, "")), nil
}

// splitKeepingNewlines splits on "\n" keeping the terminator on each piece,
// so rejoining is byte-exact (a CRLF file keeps its "\r" inside the pieces).
func splitKeepingNewlines(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			if s != "" {
				out = append(out, s)
			}
			return out
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
}
