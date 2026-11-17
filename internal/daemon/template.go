package daemon

// template.go — S61 T7 (GI14): the scaffold-template doors over the store
// (the store owns the row laws — a save is row+revision or neither, and
// an edit never moves origin; T1's law, restated nowhere else). The two
// reads are census and content; the write is the person's palette.

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

type templateDoors struct {
	store *store.Store
}

// newTemplateDoors: storeless mount ⇒ the plane is ABSENT (V13).
func newTemplateDoors(d *Daemon) *templateDoors {
	if d == nil || d.store == nil {
		return nil
	}
	return &templateDoors{store: d.store}
}

func templateSummary(t store.ScaffoldTemplate) protocol.TemplateSummary {
	return protocol.TemplateSummary{
		Slug:        t.Slug,
		Title:       t.Title,
		Description: t.Description,
		StackTags:   append([]string(nil), t.StackTags...),
		Origin:      t.Origin,
		Version:     t.Version,
		FileCount:   len(t.Files),
		// Diverged is derived, never stored: version > 1 means somebody
		// saved over the shipped set (an edit never moves origin, so the
		// origin word stays whatever the seed laid).
		Diverged:  t.Version > 1,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func (t *templateDoors) List(_ context.Context, p protocol.TemplateListParams) (protocol.TemplateListResult, error) {
	rows, err := t.store.ListTemplates()
	if err != nil {
		return protocol.TemplateListResult{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	out := protocol.TemplateListResult{Templates: make([]protocol.TemplateSummary, 0, len(rows))}
	for _, row := range rows {
		if p.Origin != "" && row.Origin != p.Origin {
			continue
		}
		if p.StackTag != "" && !containsString(row.StackTags, p.StackTag) {
			continue
		}
		out.Templates = append(out.Templates, templateSummary(row))
	}
	return out, nil
}

func (t *templateDoors) Get(_ context.Context, p protocol.TemplateGetParams) (protocol.TemplateView, error) {
	row, err := t.store.GetTemplate(p.Slug)
	if err != nil {
		return protocol.TemplateView{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	return templateView(row), nil
}

func templateView(t store.ScaffoldTemplate) protocol.TemplateView {
	v := protocol.TemplateView{TemplateSummary: templateSummary(t)}
	for _, f := range t.Files {
		v.Files = append(v.Files, protocol.TemplateFileWire{Path: f.Path, Mode: f.Mode, Content: f.Content})
	}
	return v
}

func (t *templateDoors) Save(_ context.Context, actor protocol.Actor, p protocol.TemplateSaveParams) (protocol.TemplateView, error) {
	in := store.NewTemplate{
		Slug:        p.Slug,
		Title:       p.Title,
		Description: p.Description,
		StackTags:   p.StackTags,
	}
	for _, f := range p.Files {
		in.Files = append(in.Files, store.TemplateFile{Path: f.Path, Mode: f.Mode, Content: f.Content})
	}
	row, ref, err := t.store.SaveTemplate(in, store.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return protocol.TemplateView{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	if ref != nil {
		return protocol.TemplateView{}, protocol.ErrIntakeRefused.WithWhat("%s", ref.Sentence)
	}
	return templateView(row), nil
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
