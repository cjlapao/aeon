package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ComponentCreateParams struct {
	Name string `json:"name"`
	// ParentID optionally files the component as a CHILD of an existing
	// component; empty makes it a root (its own top of the tree).
	ParentID         string   `json:"parentId,omitempty"`
	ShortDescription string   `json:"shortDescription,omitempty"`
	Description      string   `json:"description,omitempty"`
	DefaultBranch    string   `json:"defaultBranch,omitempty"`
	BranchPrefix     string   `json:"branchPrefix,omitempty"`
	// MaxParallelAgents, when > 0, sets S32's PA14 cap at creation; omitted
	// keeps the schema default of 1.
	MaxParallelAgents int      `json:"maxParallelAgents,omitempty"`
	Slug              string   `json:"slug"`
	Root              string   `json:"root"`
	Remote            string   `json:"remote,omitempty"`
	RemoteKey         string   `json:"remoteKey,omitempty"`
	RepoKey           string   `json:"repoKey,omitempty"`
	Owner             string   `json:"owner,omitempty"`
	Icon              string   `json:"icon,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type ComponentUpdateParams struct {
	ID string `json:"id"`
	// A component edit does not move it in the tree (CT4: a move is an
	// attach/detach, never an update), so there is no parent field here.
	ShortDescription string   `json:"shortDescription,omitempty"`
	Description      string   `json:"description,omitempty"`
	DefaultBranch    string   `json:"defaultBranch,omitempty"`
	BranchPrefix     string   `json:"branchPrefix,omitempty"`
	// MaxParallelAgents, when > 0, changes S32's PA14 cap; omitted (0) keeps
	// the existing one — an update is a partial write, never a reset.
	MaxParallelAgents int    `json:"maxParallelAgents,omitempty"`
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Remote           string   `json:"remote"`
	RemoteKey        string   `json:"remoteKey"`
	RepoKey          string   `json:"repoKey"`
	Owner            string   `json:"owner"`
	Icon             string   `json:"icon"`
	Tags             []string `json:"tags"`
}

// ComponentIDParams is the sole argument of the read and the one-id tree
// moves (show, detach, archive, restore) — a single component id, nothing else.
type ComponentIDParams struct {
	ID string `json:"id"`
}

// ComponentPromoteParams files a standalone component into a new root (CT3):
// the daemon's PromoteComponent keeps the slug and the empty fields, so the
// params mirror the catalog's schema (component_id + the editable fields, no
// slug — the slug is the promoted node's own, never re-set here).
type ComponentPromoteParams struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	ShortDescription string `json:"shortDescription,omitempty"`
	Description      string `json:"description,omitempty"`
	Icon             string `json:"icon,omitempty"`
}

// ComponentAttachParams makes a childless root a child of parentId (CT4).
type ComponentAttachParams struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
}

type ComponentView struct {
	ID               string   `json:"id"`
	ParentID         string   `json:"parentId,omitempty"`
	ShortDescription string   `json:"shortDescription"`
	Description      string   `json:"description"`
	DefaultBranch    string   `json:"defaultBranch"`
	BranchPrefix     string   `json:"branchPrefix"`
	// MaxParallelAgents is S32's PA14 cap the app reads (component.list) and
	// sets (component.update). Default 1 — byte-identical serial (PA1/D8).
	MaxParallelAgents int    `json:"maxParallelAgents"`
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Root             string   `json:"root"`
	Remote           string   `json:"remote,omitempty"`
	RemoteKey        string   `json:"remoteKey,omitempty"`
	RepoKey          string   `json:"repoKey,omitempty"`
	Owner            string   `json:"owner,omitempty"`
	Icon             string   `json:"icon"`
	Tags             []string `json:"tags"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
}

type ComponentListResult struct {
	Components []ComponentView `json:"components"`
}

type ComponentStore interface {
	CreateComponent(ComponentCreateParams) (ComponentView, error)
	UpdateComponent(ComponentUpdateParams) (ComponentView, error)
	ListComponents() ([]ComponentView, error)
	ShowComponent(id string) (ComponentView, error)
	PromoteComponent(ComponentPromoteParams) (ComponentView, error)
	AttachComponent(ComponentAttachParams) (ComponentView, error)
	DetachComponent(id string) (ComponentView, error)
	ArchiveComponent(id string) (ComponentView, error)
	RestoreComponent(id string) (ComponentView, error)
}

func (t *Table) handleComponentUpdate(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID, p.Name, p.Slug = strings.TrimSpace(p.ID), strings.TrimSpace(p.Name), strings.TrimSpace(p.Slug)
	if p.ID == "" || p.Name == "" || p.Slug == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id, name and slug are required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.UpdateComponent(p)
}

func (t *Table) handleComponentCreate(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentCreateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.Name, p.Slug, p.Root = strings.TrimSpace(p.Name), strings.TrimSpace(p.Slug), strings.TrimSpace(p.Root)
	if p.Name == "" || p.Slug == "" || p.Root == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name, slug and root are required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.CreateComponent(p)
}

func (t *Table) handleComponentList(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	components, err := t.Components.ListComponents()
	if err != nil {
		return nil, err
	}
	return ComponentListResult{Components: components}, nil
}

func (t *Table) handleComponentShow(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.ShowComponent(p.ID)
}

func (t *Table) handleComponentPromote(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentPromoteParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.PromoteComponent(p)
}

func (t *Table) handleComponentAttach(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentAttachParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID, p.ParentID = strings.TrimSpace(p.ID), strings.TrimSpace(p.ParentID)
	if p.ID == "" || p.ParentID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id and parentId are required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.AttachComponent(p)
}

func (t *Table) handleComponentDetach(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.DetachComponent(p.ID)
}

func (t *Table) handleComponentArchive(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.ArchiveComponent(p.ID)
}

func (t *Table) handleComponentRestore(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p ComponentIDParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.Components == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Components.RestoreComponent(p.ID)
}
