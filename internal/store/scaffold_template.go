// scaffold_template.go — S61 T1 (GI14): starter templates as records. The
// template row is MUTABLE by design — an operator editing a template is the
// point — while its history is append-only: one revision row per save,
// written in the SAME transaction as the row, so a torn save cannot leave a
// version that nobody ever saw. 'system' origin marks seeded templates; the
// seed applier re-saves through SaveTemplate and a shipped template the
// operator edited then reads diverged (the view compares current vs origin
// revision) — never overwritten by a re-seed and never deleted (the schema
// refuses the delete; there is no delete method to refuse).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TemplateFile is one rendered file of a template (GI14's files JSON). Mode
// is the octal permission the daemon will create the file with; the closed
// allow-list is the scaffold engine's law (T3), the store carries the shape.
type TemplateFile struct {
	Path    string `json:"path"`
	Mode    int    `json:"mode"`
	Content string `json:"content"`
}

// ScaffoldTemplate is one template at its current version.
type ScaffoldTemplate struct {
	ID          string         `json:"id"`
	Slug        string         `json:"slug"`
	Version     int            `json:"version"`
	Origin      string         `json:"origin"` // system | user
	Title       string         `json:"title"`
	Description string         `json:"description"`
	StackTags   []string       `json:"stackTags"`
	Files       []TemplateFile `json:"files"`
	CreatedAt   string         `json:"createdAt"`
	UpdatedAt   string         `json:"updatedAt"`
}

// ScaffoldTemplateRevision is one row of the history.
type ScaffoldTemplateRevision struct {
	Slug      string `json:"slug"`
	Version   int    `json:"version"`
	Files     string `json:"filesJson"` // canonical files JSON as saved
	ActorKind string `json:"actorKind"`
	ActorID   string `json:"actorId"`
	Origin    string `json:"origin"`
	At        string `json:"at"`
}

var templateSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$`)

// NewTemplate is the input to SaveTemplate.
type NewTemplate struct {
	Slug        string
	Title       string
	Description string
	StackTags   []string
	Files       []TemplateFile
	// SeedOrigin is honoured ONLY when the slug does not exist yet: the seed
	// lays 'system' rows, an operator save lays 'user'. Editing an existing
	// template never moves origin — shipped stays shipped.
	SeedOrigin string
}

func templateFileIssues(files []TemplateFile) string {
	if len(files) == 0 {
		return "a template ships at least one file"
	}
	for _, f := range files {
		switch {
		case f.Path == "" || strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, `\`) ||
			strings.Contains(f.Path, "..") || strings.Contains(f.Path, "\x00") ||
			len(f.Path) > 255 || f.Path == ".git" || strings.HasPrefix(f.Path, ".git/") ||
			strings.Contains(f.Path, "//"):
			return fmt.Sprintf("template file path %q is not a plain relative path", f.Path)
		}
	}
	return ""
}

// SaveTemplate writes row+revision or neither. A new slug starts at version
// 1; an existing slug saves at current+1. The path guard here is the row's
// own hygiene (GI9's full guard is the scaffold engine's, at write time, over
// the RENDERED output — this one refuses a template that could not even be
// named safely before a byte is rendered).
func (s *Store) SaveTemplate(in NewTemplate, by Actor) (ScaffoldTemplate, *Refusal, error) {
	if !validTemplateActor(by) {
		return ScaffoldTemplate{}, &Refusal{Kind: "actor-unknown",
			Sentence: fmt.Sprintf("refused: author kind %q is not user, agent, trigger, or system (or the author has no id) — every template save names who wrote it; 'system' is the seed's own (GI14, migration 20261105093000)", by.Kind)}, nil
	}
	if !templateSlugRE.MatchString(in.Slug) {
		return ScaffoldTemplate{}, &Refusal{Kind: "slug-invalid",
			Sentence: fmt.Sprintf("refused: template slug %q is not lowercase letters, digits and hyphens (2–32, not starting or ending with a hyphen)", in.Slug)}, nil
	}
	if in.Title == "" {
		return ScaffoldTemplate{}, &Refusal{Kind: "title-empty",
			Sentence: "refused: a template has a title — the words a person picks it by"}, nil
	}
	if issue := templateFileIssues(in.Files); issue != "" {
		return ScaffoldTemplate{}, &Refusal{Kind: "template-files",
			Sentence: "refused: " + issue}, nil
	}
	filesJSON, err := json.Marshal(in.Files)
	if err != nil {
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: encode template files: %w", err)
	}
	tagsJSON := "[]"
	if len(in.StackTags) > 0 {
		b, err := json.Marshal(in.StackTags)
		if err != nil {
			return ScaffoldTemplate{}, nil, fmt.Errorf("store: encode template tags: %w", err)
		}
		tagsJSON = string(b)
	}

	tx, err := s.w.Begin()
	if err != nil {
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: save template: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	t, ref, err := saveTemplateTx(tx, in, by, s.Now(), tagsJSON, string(filesJSON))
	if err != nil || ref != nil {
		return ScaffoldTemplate{}, ref, err
	}
	if err := tx.Commit(); err != nil {
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: save template: %w", err)
	}
	return t, nil, nil
}

// SaveTemplateTx is the seed engine's twin (S61 T3, SD3): the row and its
// revision ride the CALLER's transaction, so an applied record and its
// ledger row commit together or not at all — the retroseed Upsert twin's
// shape, built the same day for the same reason.
func (s *Store) SaveTemplateTx(tx *sql.Tx, in NewTemplate, by Actor) (ScaffoldTemplate, *Refusal, error) {
	if tx == nil {
		return ScaffoldTemplate{}, nil, errors.New("store: SaveTemplateTx needs the caller's transaction")
	}
	filesJSON, err := json.Marshal(in.Files)
	if err != nil {
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: encode template files: %w", err)
	}
	tagsJSON := "[]"
	if len(in.StackTags) > 0 {
		b, err := json.Marshal(in.StackTags)
		if err != nil {
			return ScaffoldTemplate{}, nil, fmt.Errorf("store: encode template tags: %w", err)
		}
		tagsJSON = string(b)
	}
	return saveTemplateTx(tx, in, by, s.Now(), tagsJSON, string(filesJSON))
}

// saveTemplateTx is the one write body: begin/commit belongs to the caller
// (SaveTemplate owns its transaction; SaveTemplateTx never opens one).
func saveTemplateTx(tx *sql.Tx, in NewTemplate, by Actor, now, tagsJSON, filesJSON string) (ScaffoldTemplate, *Refusal, error) {
	if !validTemplateActor(by) {
		return ScaffoldTemplate{}, &Refusal{Kind: "actor-unknown",
			Sentence: fmt.Sprintf("refused: author kind %q is not user, agent, trigger, or system (or the author has no id) — every template save names who wrote it; 'system' is the seed's own (GI14, migration 20261105093000)", by.Kind)}, nil
	}
	if !templateSlugRE.MatchString(in.Slug) {
		return ScaffoldTemplate{}, &Refusal{Kind: "slug-invalid",
			Sentence: fmt.Sprintf("refused: template slug %q is not lowercase letters, digits and hyphens (2–32, not starting or ending with a hyphen)", in.Slug)}, nil
	}
	if in.Title == "" {
		return ScaffoldTemplate{}, &Refusal{Kind: "title-empty",
			Sentence: "refused: a template has a title — the words a person picks it by"}, nil
	}
	if issue := templateFileIssues(in.Files); issue != "" {
		return ScaffoldTemplate{}, &Refusal{Kind: "template-files",
			Sentence: "refused: " + issue}, nil
	}

	var curID, curOrigin, curCreated string
	var curVersion int
	err := tx.QueryRow(`SELECT id, origin, version, created_at FROM scaffold_template WHERE slug = ?`, in.Slug).
		Scan(&curID, &curOrigin, &curVersion, &curCreated)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		origin := "user"
		if in.SeedOrigin == "system" {
			origin = "system"
		}
		t := ScaffoldTemplate{ID: "tpl-" + newID(), Slug: in.Slug, Version: 1, Origin: origin,
			Title: in.Title, Description: in.Description, StackTags: in.StackTags,
			Files: in.Files, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.Exec(`INSERT INTO scaffold_template (id,slug,version,origin,title,description,stack_tags,files,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, t.ID, t.Slug, t.Version, t.Origin, t.Title, t.Description,
			tagsJSON, filesJSON, t.CreatedAt, t.UpdatedAt); err != nil {
			return ScaffoldTemplate{}, nil, fmt.Errorf("store: create template %s: %w", in.Slug, err)
		}
		if _, err := tx.Exec(`INSERT INTO scaffold_template_revision (slug,version,files,actor_kind,actor_id,origin,at)
			VALUES (?,?,?,?,?,?,?)`, t.Slug, t.Version, filesJSON, by.Kind, by.ID, t.Origin, now); err != nil {
			return ScaffoldTemplate{}, nil, fmt.Errorf("store: first revision of %s: %w", in.Slug, err)
		}
		return t, nil, nil
	case err != nil:
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: read template %s: %w", in.Slug, err)
	}
	// Existing slug: version+1, origin unchanged (a shipped template stays
	// shipped — the VIEW says diverged, the row never lies about origin).
	t := ScaffoldTemplate{ID: curID, Slug: in.Slug, Version: curVersion + 1, Origin: curOrigin,
		Title: in.Title, Description: in.Description, StackTags: in.StackTags,
		Files: in.Files, CreatedAt: curCreated, UpdatedAt: now}
	if _, err := tx.Exec(`UPDATE scaffold_template SET version=?, title=?, description=?,
			stack_tags=?, files=?, updated_at=? WHERE id = ?`,
		t.Version, t.Title, t.Description, tagsJSON, filesJSON, now, curID); err != nil {
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: bump template %s: %w", in.Slug, err)
	}
	if _, err := tx.Exec(`INSERT INTO scaffold_template_revision (slug,version,files,actor_kind,actor_id,origin,at)
		VALUES (?,?,?,?,?,?,?)`, t.Slug, t.Version, filesJSON, by.Kind, by.ID, curOrigin, now); err != nil {
		return ScaffoldTemplate{}, nil, fmt.Errorf("store: revision %d of %s: %w", t.Version, in.Slug, err)
	}
	return t, nil, nil
}

// GetTemplate reads the current shape; absence is the house-style error.
func (s *Store) GetTemplate(slug string) (ScaffoldTemplate, error) {
	var t ScaffoldTemplate
	var tagsJSON, filesJSON string
	err := s.r.QueryRow(`SELECT id, slug, version, origin, title, description, stack_tags, files,
			created_at, updated_at FROM scaffold_template WHERE slug = ?`, slug).
		Scan(&t.ID, &t.Slug, &t.Version, &t.Origin, &t.Title, &t.Description, &tagsJSON, &filesJSON,
			&t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ScaffoldTemplate{}, fmt.Errorf("store: template %q does not exist", slug)
	}
	if err != nil {
		return ScaffoldTemplate{}, fmt.Errorf("store: read template %s: %w", slug, err)
	}
	if err := json.Unmarshal([]byte(tagsJSON), &t.StackTags); err != nil {
		return ScaffoldTemplate{}, fmt.Errorf("store: template %s tags: %w", slug, err)
	}
	if err := json.Unmarshal([]byte(filesJSON), &t.Files); err != nil {
		return ScaffoldTemplate{}, fmt.Errorf("store: template %s files: %w", slug, err)
	}
	return t, nil
}

// GetTemplateTx reads a template INSIDE the caller's transaction (the seed
// applier's Fingerprint door: applied==live is decided on the same
// snapshot the engine writes into). Absence is (row=false, nil error): a
// fingerprint probe is not a read of something missing.
func (s *Store) GetTemplateTx(tx *sql.Tx, slug string) (ScaffoldTemplate, bool, error) {
	if tx == nil {
		return ScaffoldTemplate{}, false, errors.New("store: GetTemplateTx needs the caller's transaction")
	}
	var t ScaffoldTemplate
	var tagsJSON, filesJSON string
	err := tx.QueryRow(`SELECT id, slug, version, origin, title, description, stack_tags, files,
			created_at, updated_at FROM scaffold_template WHERE slug = ?`, slug).
		Scan(&t.ID, &t.Slug, &t.Version, &t.Origin, &t.Title, &t.Description, &tagsJSON, &filesJSON,
			&t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ScaffoldTemplate{}, false, nil
	}
	if err != nil {
		return ScaffoldTemplate{}, false, fmt.Errorf("store: read template %s: %w", slug, err)
	}
	if err := json.Unmarshal([]byte(tagsJSON), &t.StackTags); err != nil {
		return ScaffoldTemplate{}, false, fmt.Errorf("store: template %s tags: %w", slug, err)
	}
	if err := json.Unmarshal([]byte(filesJSON), &t.Files); err != nil {
		return ScaffoldTemplate{}, false, fmt.Errorf("store: template %s files: %w", slug, err)
	}
	return t, true, nil
}

// ListTemplates is every template, slug order.
func (s *Store) ListTemplates() ([]ScaffoldTemplate, error) {
	rows, err := s.r.Query(`SELECT id, slug, version, origin, title, description, stack_tags, files,
			created_at, updated_at FROM scaffold_template ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("store: list templates: %w", err)
	}
	defer rows.Close()
	var out []ScaffoldTemplate
	for rows.Next() {
		var t ScaffoldTemplate
		var tagsJSON, filesJSON string
		if err := rows.Scan(&t.ID, &t.Slug, &t.Version, &t.Origin, &t.Title, &t.Description,
			&tagsJSON, &filesJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan template: %w", err)
		}
		if err := json.Unmarshal([]byte(tagsJSON), &t.StackTags); err != nil {
			return nil, fmt.Errorf("store: template %s tags: %w", t.Slug, err)
		}
		if err := json.Unmarshal([]byte(filesJSON), &t.Files); err != nil {
			return nil, fmt.Errorf("store: template %s files: %w", t.Slug, err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TemplateRevisions is one template's history, oldest first.
func (s *Store) TemplateRevisions(slug string) ([]ScaffoldTemplateRevision, error) {
	rows, err := s.r.Query(`SELECT slug, version, files, actor_kind, actor_id, origin, at
		FROM scaffold_template_revision WHERE slug = ? ORDER BY version`, slug)
	if err != nil {
		return nil, fmt.Errorf("store: revisions of %s: %w", slug, err)
	}
	defer rows.Close()
	var out []ScaffoldTemplateRevision
	for rows.Next() {
		var r ScaffoldTemplateRevision
		if err := rows.Scan(&r.Slug, &r.Version, &r.Files, &r.ActorKind, &r.ActorID, &r.Origin, &r.At); err != nil {
			return nil, fmt.Errorf("store: scan template revision: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// validTemplateActor is the records-tier trio plus the seed's own kind:
// migration 20261105093000 widened scaffold_template_revision's CHECK to
// admit 'system' because the shipped templates are written by the seed
// through this one path (GI14 — actor system:seed). Charter revisions and
// every other records-tier write keep the strict trio; a template is data
// a person edits afterwards, and its history names the machine honestly.
func validTemplateActor(a Actor) bool {
	switch a.Kind {
	case "user", "agent", "trigger", "system":
	default:
		return false
	}
	return a.ID != ""
}
