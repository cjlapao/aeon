package blueprint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/cjlapao/aeon/internal/errs"
	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (Blueprint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Blueprint{}, err
	}
	return Load(raw)
}

func Load(raw []byte) (Blueprint, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return Blueprint{}, fmt.Errorf("blueprint YAML: %w", err)
	}
	if len(node.Content) == 0 {
		return Blueprint{}, fmt.Errorf("blueprint is empty")
	}
	if issue := unknownKey(node.Content[0], reflect.TypeOf(Blueprint{}), ""); issue != "" {
		return Blueprint{}, fmt.Errorf("blueprint %s", issue)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var b Blueprint
	if err := dec.Decode(&b); err != nil {
		return Blueprint{}, fmt.Errorf("blueprint YAML: %w", err)
	}
	if b.SchemaVersion != SupportedSchemaVersion {
		return Blueprint{}, fmt.Errorf("unsupported schemaVersion %d; supported: %d", b.SchemaVersion, SupportedSchemaVersion)
	}
	if b.ID == "" {
		return Blueprint{}, fmt.Errorf("blueprint id is required")
	}
	for i, l := range b.Lanes {
		if (l.Step == nil) == (len(l.Ladder) == 0) {
			return Blueprint{}, fmt.Errorf("lanes[%d] must have step or ladder, exactly one", i)
		}
	}
	return b, nil
}

func unknownKey(n *yaml.Node, t reflect.Type, path string) string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() == reflect.Map && n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			child := n.Content[i].Value
			if path != "" {
				child = path + "." + child
			}
			if issue := unknownKey(n.Content[i+1], t.Elem(), child); issue != "" {
				return issue
			}
		}
		return ""
	}
	if t.Kind() != reflect.Struct || n.Kind != yaml.MappingNode {
		return ""
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if tag != "" && tag != "-" {
			fields[tag] = f.Type
		}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		child := key
		if path != "" {
			child = path + "." + key
		}
		ft, ok := fields[key]
		if !ok {
			return fmt.Sprintf("unknown key %s%s", child, suggestion(key, fields))
		}
		if val.Kind == yaml.SequenceNode {
			for j, item := range val.Content {
				if issue := unknownKey(item, ft, fmt.Sprintf("%s[%d]", child, j)); issue != "" {
					return issue
				}
			}
		} else if issue := unknownKey(val, ft, child); issue != "" {
			return issue
		}
	}
	return ""
}

func suggestion(key string, fields map[string]reflect.Type) string {
	best, bestDistance := "", 3
	for candidate := range fields {
		distance := editDistance(key, candidate)
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if best != "" {
		return fmt.Sprintf("; did you mean %q?", best)
	}
	return ""
}
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range a {
		cur := make([]int, len(b)+1)
		cur[0] = i + 1
		for j, cb := range b {
			cost := 0
			if ca != cb {
				cost = 1
			}
			cur[j+1] = min(cur[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// Provenance says where a blueprint came from, on the screen (D14): the
// project's own, or the shipped one. "Why is this loop not doing what the
// documentation says" is answered by that line — someone who copied a
// blueprint six months ago and edited it will not remember.
type Provenance string

const (
	// ProvenanceProject is the project's own, which overrides the shipped by
	// name (C12, D14).
	ProvenanceProject Provenance = "this project"
	// ProvenanceShipped is the shipped one, under the install.
	ProvenanceShipped Provenance = "shipped"
)

// Resolved is a blueprint and its provenance — the fact D14 puts on the screen
// and the app's picker (blueprint.list) carries, read from the same
// resolution, not a second one.
type Resolved struct {
	Name       string
	Blueprint  Blueprint
	Provenance Provenance
	Path       string
}

// ErrUnknownBlueprint is a three-part error (S23 T1) whose Do is the fix:
// list the available blueprints. A refusal that names how to list them, not
// "blueprint not found" (which tells a person nothing).
type ErrUnknownBlueprint struct {
	Err errs.Error
}

// NewErrUnknownBlueprint is the only constructor. The Do is `aeon blueprint
// list` — the one fix, the only way a stranger learns the names.
func NewErrUnknownBlueprint(name string) *ErrUnknownBlueprint {
	return &ErrUnknownBlueprint{
		Err: errs.New(
			"blueprint "+quote(name)+" is not shipped and not in this project",
			"it must be a shipped name or a file in ./aeon/blueprints",
			"aeon blueprint list",
		),
	}
}

// Error renders the three parts; What/Why/Do expose them for a renderer.
func (e *ErrUnknownBlueprint) Error() string { return e.Err.Error() }
func (e *ErrUnknownBlueprint) What() string  { return e.Err.What() }
func (e *ErrUnknownBlueprint) Why() string   { return e.Err.Why() }
func (e *ErrUnknownBlueprint) Do() string    { return e.Err.Do() }

// ResolveWithProject resolves a blueprint by name: the project's own wins over
// the shipped, by name (C12, D14). A running loop keeps its pinned content, so
// an override does not change a loop in flight (S8's L6) — the resolution is
// the same at loop start and later, and the pin is what holds a loop's
// content, not this resolution.
func ResolveWithProject(name, projectDir string) (Resolved, error) {
	// The project's own wins, by name.
	projectFile := filepath.Join(projectDir, "aeon", "blueprints", name+".yaml")
	if raw, err := os.ReadFile(projectFile); err == nil {
		b, err := Load(raw)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Name: name, Blueprint: b, Provenance: ProvenanceProject, Path: projectFile}, nil
	}
	// Else the shipped.
	raw, err := standards.ReadFile("blueprints/" + name + ".yaml")
	if err != nil {
		return Resolved{}, NewErrUnknownBlueprint(name)
	}
	b, err := Load(raw)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Name: name, Blueprint: b, Provenance: ProvenanceShipped, Path: "blueprints/" + name + ".yaml"}, nil
}

// ListWithProvenance lists the resolvable blueprints with their provenance —
// the project's own (override) first, then the shipped. The provenance is the
// same fact D14 puts on the screen and blueprint.list carries.
func ListWithProvenance(projectDir string) []Resolved {
	seen := map[string]bool{}
	var out []Resolved
	for _, name := range standardNames {
		if projectDir != "" {
			if _, err := os.Stat(filepath.Join(projectDir, "aeon", "blueprints", name+".yaml")); err == nil {
				out = append(out, Resolved{Name: name, Provenance: ProvenanceProject})
				seen[name] = true
				continue
			}
		}
		out = append(out, Resolved{Name: name, Provenance: ProvenanceShipped})
		seen[name] = true
	}
	return out
}

// quote renders a name for the message, naming the thing.
func quote(s string) string { return "\"" + s + "\"" }
