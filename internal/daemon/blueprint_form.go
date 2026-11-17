package daemon

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
)

// blueprint_form.go — the translations a flow form needs: text to structure,
// structure to text, and the vocabulary. Pure functions of the blueprint
// package and the closed action registry; no store, no write.

func (d *Daemon) wireBlueprintForm() {
	d.Table.BlueprintModel = func(p protocol.BlueprintModelParams) (any, error) {
		b, err := blueprint.ParseModel([]byte(p.Content))
		if err != nil {
			return nil, protocol.ErrBadParams{Err: err}
		}
		return map[string]any{"model": b}, nil
	}
	d.Table.BlueprintRender = func(p protocol.BlueprintRenderParams) (any, error) {
		var b blueprint.Blueprint
		if err := json.Unmarshal(p.Model, &b); err != nil {
			return nil, protocol.ErrBadParams{Err: fmt.Errorf("the flow structure is not one: %w", err)}
		}
		text, err := blueprint.RenderModel(b)
		if err != nil {
			return nil, err
		}
		return map[string]any{"content": text}, nil
	}
	d.Table.BlueprintVocabulary = func() any { return BlueprintVocabulary() }
}

// BlueprintVocabulary is the base vocabulary plus the code actions a blueprint
// may name, with the arguments an author supplies (the rest are filled by code).
func BlueprintVocabulary() blueprint.Vocabulary {
	v := blueprint.BaseVocabulary()
	for _, a := range action.Describe() {
		desc := blueprint.ActionDescription{Name: a.Name, Args: []blueprint.ActionArgument{}}
		names := make([]string, 0, len(a.Args))
		for n := range a.Args {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			s := a.Args[n]
			desc.Args = append(desc.Args, blueprint.ActionArgument{Name: n, Type: s.Type, Required: s.Required, Source: s.Source})
		}
		v.Actions = append(v.Actions, desc)
	}
	return v
}

// blueprintLabels is what the library shows for a flow: the title and
// description in its own text win; a shipped flow without them takes the labels
// that ship with the daemon; any other flow's title is its slug read as words. A
// flow whose text cannot be read still gets those fallbacks.
func blueprintLabels(slug, content string) (title, description string) {
	if b, err := blueprint.ParseModel([]byte(content)); err == nil {
		title, description = b.Title, b.Description
	}
	if shipped, ok := blueprint.ShippedLabels(slug); ok {
		if title == "" {
			title = shipped.Title
		}
		if description == "" {
			description = shipped.Description
		}
	}
	if title == "" {
		title = blueprint.TitleFromSlug(slug)
	}
	return title, description
}
