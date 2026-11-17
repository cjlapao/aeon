package scaffold

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed all:templates
var shippedFS embed.FS

// ShippedSet is one starter template as it ships: the words a person picks
// it by, and the files (paths relative to the template root, content as
// TEMPLATE source — the closed variables are still {{...}} at this layer).
type ShippedSet struct {
	Slug        string
	Title       string
	Description string
	StackTags   []string
	Files       []TemplateFile
}

// shippedMeta is the pick-by words per slug, in seed order. The bodies live
// as files under templates/ (a reviewer reads them as files, not as Go
// string escapes); this table only names them.
func shippedMeta() []ShippedSet {
	return []ShippedSet{
		{Slug: "minimal", Title: "Minimal",
			Description: "A directory with a README, an empty-by-design AGENTS.md and a small gitignore — nothing else presumed.",
			StackTags:   nil},
		{Slug: "go-service", Title: "Go service",
			Description: "The minimal set plus a CI workflow that builds and tests a Go module.",
			StackTags:   []string{"go"}},
		{Slug: "node-ts", Title: "Node + TypeScript",
			Description: "The minimal set plus a CI workflow that installs and tests a Node project.",
			StackTags:   []string{"node", "typescript"}},
	}
}

// ShippedTemplates returns the three starters with their files read from
// the embedded tree, sorted by path (the seed record's content is the
// deterministic spelling — TestSeededTemplatesGolden hashes it). A missing
// or unreadable shipped file is a panic: a broken build ships silently
// otherwise, and the seed must not lay a half template.
func ShippedTemplates() []ShippedSet {
	sets := shippedMeta()
	for i := range sets {
		root := "templates/" + sets[i].Slug
		entries, err := fs.ReadDir(shippedFS, root)
		if err != nil || len(entries) == 0 {
			panic("scaffold: shipped template " + sets[i].Slug + " has no files under " + root)
		}
		sets[i].Files = readShipped(root)
	}
	return sets
}

func readShipped(root string) []TemplateFile {
	var out []TemplateFile
	err := fs.WalkDir(shippedFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := shippedFS.ReadFile(p)
		if err != nil {
			return err
		}
		rel := p[len(root)+1:]
		out = append(out, TemplateFile{Path: rel, Mode: 0o644, Content: string(b)})
		return nil
	})
	if err != nil {
		panic("scaffold: reading shipped templates: " + err.Error())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
