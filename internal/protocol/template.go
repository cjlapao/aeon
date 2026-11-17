// template.go — S61 T7 (GI14, GI9, P4): the scaffold-template doors.
//
// Two reads and one human write over ONE seam, registered together or
// absent (V13). The template plane stores what T3 renders: files with
// paths and modes, versions and the two origins (system | user).
//
// The wire states the plane's own laws:
//
//   - A save NEVER moves origin (T1's law on the row, restated at the
//     door): editing a shipped template lays a new USER-visible revision
//     under the same row — the store refuses to relabel shipped as user's,
//     and this door never even asks.
//   - `diverged` is derived, never stored: version > 1 means somebody
//     saved over the shipped set, and the door says which version is
//     current so a person reads the same number the renderer pins.
//   - No identity in params (P4): the saving person is the connection's.
//   - The file guard here is the ROW's hygiene (the store enforces it too);
//     the scaffold engine's full guard is at write time, over the RENDERED
//     output — the two guards are different laws and the plane says so.
//
// `ErrCharterRefused` is NOT this family's voice: template refusals ride
// the intake voice (`ErrIntakeRefused`, -31965) — the templates belong to
// the intake plane (GI14), and one plane refuses with one voice.

package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------

const (
	// MethodTemplateList lists the template set with the facts a wizard
	// page needs: origin, version, stack tags, file count, diverged.
	MethodTemplateList MethodName = "template.list"
	// MethodTemplateGet answers one template whole — files included,
	// contents verbatim (a template is a document, not a secret).
	MethodTemplateGet MethodName = "template.get"
	// MethodTemplateSave writes row+revision as one — a person's door
	// (GI14: the template set is the person's palette).
	MethodTemplateSave MethodName = "template.save"
)

// --- the wire shapes ---------------------------------------------------------

// TemplateFileWire is one file of a template.
type TemplateFileWire struct {
	Path    string `json:"path"`
	Mode    int    `json:"mode"`
	Content string `json:"content"`
}

// TemplateSummary is one list row — the wizard's row shape.
type TemplateSummary struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	StackTags   []string `json:"stackTags"`
	Origin      string   `json:"origin"`
	Version     int      `json:"version"`
	FileCount   int      `json:"fileCount"`
	Diverged    bool     `json:"diverged"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// TemplateView is one template whole.
type TemplateView struct {
	TemplateSummary
	Files []TemplateFileWire `json:"files"`
}

// TemplateListParams filters: a stack tag (templates whose tags include
// it) and an origin ("system" | "user").
type TemplateListParams struct {
	StackTag string `json:"stackTag,omitempty"`
	Origin   string `json:"origin,omitempty"`
}

// TemplateListResult is template.list's whole answer.
type TemplateListResult struct {
	Templates []TemplateSummary `json:"templates"`
}

// TemplateGetParams — `{slug}`.
type TemplateGetParams struct {
	Slug string `json:"slug"`
}

// TemplateSaveParams — `{slug, title, description, stackTags, files, note?}`.
// Files are closed over content strings; no path escapes its template.
type TemplateSaveParams struct {
	Slug        string             `json:"slug"`
	Title       string             `json:"title"`
	Description string             `json:"description,omitempty"`
	StackTags   []string           `json:"stackTags,omitempty"`
	Files       []TemplateFileWire `json:"files"`
	Note        string             `json:"note,omitempty"`
}

// --- the seam -----------------------------------------------------------------

// TemplateService is the daemon's template side: two reads and the one
// human write. Save takes the actor the CONNECTION resolved (P4); the
// store's own row laws answer refusals, voiced by the adapter.
type TemplateService interface {
	List(ctx context.Context, p TemplateListParams) (TemplateListResult, error)
	Get(ctx context.Context, p TemplateGetParams) (TemplateView, error)
	Save(ctx context.Context, actor Actor, p TemplateSaveParams) (TemplateView, error)
}

// --- registration ---------------------------------------------------------------

// RegisterTemplates registers the three template doors over a non-nil
// seam (V13: nil seam ⇒ all three ABSENT).
func (t *Table) RegisterTemplates() bool {
	if t.Templates == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodTemplateList, RequiresInitialize: true, Handle: t.handleTemplateList},
		{Name: MethodTemplateGet, RequiresInitialize: true, Handle: t.handleTemplateGet},
		{Name: MethodTemplateSave, RequiresInitialize: true, Handle: t.handleTemplateSave},
	} {
		t.register(m)
	}
	return true
}

// --- the handlers -------------------------------------------------------------

func rejectTemplateIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams will say it better
	}
	for _, k := range charterIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%s takes no %q — a template revision carries the connection's person, never a typed actor", method, k)}
		}
	}
	return nil
}

func (t *Table) handleTemplateList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectTemplateIdentityKeys(string(MethodTemplateList), raw); err != nil {
		return nil, err
	}
	var p TemplateListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Origin != "" && p.Origin != "system" && p.Origin != "user" {
		return nil, ErrBadParams{Err: fmt.Errorf(`template.list origin is "system" or "user" — not %q`, p.Origin)}
	}
	return t.Templates.List(ctx, p)
}

func (t *Table) handleTemplateGet(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectTemplateIdentityKeys(string(MethodTemplateGet), raw); err != nil {
		return nil, err
	}
	var p TemplateGetParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Slug) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("template.get needs a slug — template.list hands them out")}
	}
	return t.Templates.Get(ctx, p)
}

func (t *Table) handleTemplateSave(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectTemplateIdentityKeys(string(MethodTemplateSave), raw); err != nil {
		return nil, err
	}
	// The person check FIRST (P4/GI14).
	if t.ActorFor == nil {
		return nil, ErrIntakeRefused.WithWhat("template.save is a person's door and no identity is available on this connection — the daemon stamped none")
	}
	a := t.ActorFor(ctx, sess)
	if id := strings.TrimSpace(a.ID); id == "" || id == "0" {
		return nil, ErrIntakeRefused.WithWhat("template.save is a person's door and the peer is unresolvable (uid %q) — the actor is the connection's person, never a guess", a.ID)
	}
	var p TemplateSaveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Slug) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("template.save needs a slug")}
	}
	if len(p.Files) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("template.save needs at least one file — an empty template renders nothing anywhere")}
	}
	return t.Templates.Save(ctx, Actor{Kind: "user", ID: strings.TrimSpace(a.ID)}, p)
}
