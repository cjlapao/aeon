// Package scaffoldseed ships the `templates.scaffold` set (S61 T3, GI14/
// GI15, P8): the three starter templates a new project picks at intake.
// The bodies are DATA in the scaffold_template ROW (the renderer reads the
// row, never code — the template is editable through the person's
// template.save door from the day it lands), which is why the set is
// inert: laying a template row scaffolds nothing; `repo.scaffold` (T4), a
// person's action, writes files, and only ever into a fresh target.
//
// The spec named `internal/daemon/templates_seed.go` +
// `internal/seed/decl_templates.go`; the shipped shape follows the
// retroseed/verifyseed ruling verbatim — one package (directory
// internal/scaffold/seed, package scaffoldseed to keep the engine import
// unshadowed) owning SetID, Source, the applier and the declaration. A
// decl file inside internal/seed would import this package, which imports
// internal/seed — a cycle, not a style choice.
package scaffoldseed

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/scaffold"
	"github.com/cjlapao/aeon/internal/seed"
)

// SetID is the set's stable name, declared once (SD2).
const SetID = "templates.scaffold"

// FileSpec is one shipped file inside the record's content — the SAME
// {path,mode,content} shape the store's files JSON column carries, so the
// fingerprint rebuilds byte-for-byte from either side.
type FileSpec struct {
	Path    string `json:"path"`
	Mode    int    `json:"mode"`
	Content string `json:"content"`
}

// Content is one template's shipped content: the pick-by words and the
// files. The record hash covers all of it — a title edited under the
// person's door reads as diverged too, because diverged means "not what
// we shipped", not "the files only".
type Content struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	StackTags   []string   `json:"stackTags"`
	Files       []FileSpec `json:"files"`
}

// Source is the set's records (rule 3: never nil). Every shipped file is
// validated HERE, at the source door: a template whose paths break GI9's
// law can not even become a record.
func Source() []seed.Record {
	sets := scaffold.ShippedTemplates()
	out := make([]seed.Record, 0, len(sets))
	for _, s := range sets {
		c := Content{Title: s.Title, Description: s.Description, StackTags: s.StackTags}
		for _, f := range s.Files {
			c.Files = append(c.Files, FileSpec{Path: f.Path, Mode: int(f.Mode), Content: f.Content})
		}
		content, err := json.Marshal(c)
		if err != nil {
			panic("scaffoldseed: marshalling " + s.Slug + ": " + err.Error())
		}
		out = append(out, seed.Record{
			Key:     s.Slug,
			Version: recordVersion(s.Slug),
			Content: content,
		})
	}
	return out
}

// ContentOf decodes a record's shipped content back (the applier's doors
// share one shape).
func ContentOf(r seed.Record) (Content, error) {
	var c Content
	if err := json.Unmarshal(r.Content, &c); err != nil {
		return Content{}, fmt.Errorf("scaffoldseed: record %q: %w", r.Key, err)
	}
	return c, nil
}
