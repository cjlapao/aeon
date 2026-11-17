package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Component is ONE node of the component tree (CT1). A node is a root
// (parent_id IS NULL) or a child of a root (parent_id set); the "project"
// tier (a root with at least one non-archived child) is DERIVED in every
// read, never stored — a stored kind would be a second truth (CT1).
//
// The five inheritable columns (queue_advance, sizing_mode,
// max_parallel_agents, default_branch, branch_prefix) are carried as
// pointers on the OWN value: nil on a child means "inherit from the root",
// a value means an override (CT6). A read leaves them nil on purpose — the
// ONLY reader of those columns is effective.go (TestNoReaderBypassesEffective
// pins that); a caller that wants the resolved values goes through
// EffectiveComponent, never through this struct.
type Component struct {
	ID         string `json:"id"`
	ParentID   string `json:"parentId,omitempty"`   // "" for a root; the root's id for a child
	ArchivedAt string `json:"archivedAt,omitempty"` // "" for live
	Kind       string `json:"kind"`                 // "project" | "component", derived (CT1)
	ChildCount int    `json:"childCount"`
	Empty      bool   `json:"empty"`          // a root with no own root and no children (CT3)
	Root       string `json:"root,omitempty"` // may be empty (CT3); the primary root path

	Name             string `json:"name"`
	Slug             string `json:"slug"`
	Remote           string `json:"remote,omitempty"`
	RemoteKey        string `json:"remoteKey,omitempty"`
	RepoKey          string `json:"repoKey,omitempty"`
	Owner            string `json:"owner,omitempty"`
	Icon             string `json:"icon"`
	ShortDescription string `json:"shortDescription"`
	Description      string `json:"description"`

	// Inheritable, on the OWN value (CT6). nil = inherit (a child) or unset
	// (populated only by the write doors that know the value they wrote); a
	// read leaves these nil. EffectiveComponent resolves them.
	QueueAdvance      *string `json:"queueAdvance,omitempty"`
	SizingMode        *string `json:"sizingMode,omitempty"`
	MaxParallelAgents *int    `json:"maxParallelAgents,omitempty"`
	DefaultBranch     *string `json:"defaultBranch,omitempty"`
	BranchPrefix      *string `json:"branchPrefix,omitempty"`

	Tags      []string `json:"tags"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// NewComponent is the create input. With ParentID it is a child of that root;
// without it, a root. A child and a root both need a root path (a NEW pure
// project — a repo-less root — is only ever made by PromoteComponent, CT4).
type NewComponent struct {
	Name, Slug, Root, Remote, RemoteKey, RepoKey, Owner, Icon string
	// ParentID names an EXISTING root to create under; empty means "a root".
	ParentID, ShortDescription, Description string
	// Inheritable fields are pointers: nil on a child = inherit, a value =
	// override. A root with a nil field takes the column default.
	QueueAdvance      *string
	SizingMode        *string
	MaxParallelAgents *int
	DefaultBranch     *string
	BranchPrefix      *string
	Tags              []string
}

// ComponentEdit is the update input. There is NO parent change here (CT4: one
// way to do each thing) and NO ProjectID (CT1/CT5). Inherit names the
// inheritable fields to CLEAR (an override -> inherit); it is refused on a
// root (a root has nothing to inherit from, CT6). EMPTY means keep (the §4c
// discipline) — absent never means unset.
type ComponentEdit struct {
	ID, Name, Slug, Remote, RemoteKey, RepoKey, Owner, Icon string
	ShortDescription, Description                           string
	QueueAdvance                                            *string
	SizingMode                                              *string
	MaxParallelAgents                                       *int
	DefaultBranch                                           *string
	BranchPrefix                                            *string
	// Inherit lists the fields (queueAdvance, sizingMode, maxParallelAgents,
	// defaultBranch, branchPrefix) to clear to inherited on a child.
	Inherit []string
	Tags    []string
}

// ComponentRefusal names why a tree write was refused — the documents.go /
// projects.go shape, so a caller branches on the REASON and the DB19 rule
// (discriminated results; `error` is for infrastructure) holds.
type ComponentRefusal string

const (
	// ComponentRefusalNone means the write was accepted.
	ComponentRefusalNone ComponentRefusal = ""
	// ComponentRefusalNameEmpty — a node with no name names nothing.
	ComponentRefusalNameEmpty ComponentRefusal = "name-empty"
	// ComponentRefusalSlugEmpty — a node with no slug cannot be addressed.
	ComponentRefusalSlugEmpty ComponentRefusal = "slug-empty"
	// ComponentRefusalRootTaken — the root path is already registered.
	ComponentRefusalRootTaken ComponentRefusal = "root-taken"
	// ComponentRefusalParentMissing — the named parent is not a component.
	ComponentRefusalParentMissing ComponentRefusal = "parent-missing"
	// ComponentRefusalParentIsChild — a parent must be a root (CT2, depth 2).
	ComponentRefusalParentIsChild ComponentRefusal = "parent-is-a-child"
	// ComponentRefusalParentArchived — the named parent is archived.
	ComponentRefusalParentArchived ComponentRefusal = "parent-archived"
	// ComponentRefusalParentHasChildren — the parent already has children.
	ComponentRefusalParentHasChildren ComponentRefusal = "parent-has-children"
	// ComponentRefusalNotAChild — detach/attach acted on a root, not a child.
	ComponentRefusalNotAChild ComponentRefusal = "not-a-child"
	// ComponentRefusalHasChildren — attach/archive on a node that has children.
	ComponentRefusalHasChildren ComponentRefusal = "has-children"
	// ComponentRefusalNoRoot — a child-to-be with no primary root (CT3).
	ComponentRefusalNoRoot ComponentRefusal = "no-root"
	// ComponentRefusalEmptyParent — a move that would leave a repo-less root
	// with no children (CT3).
	ComponentRefusalEmptyParent ComponentRefusal = "empty-parent"
	// ComponentRefusalArchived — the node is archived (only restore touches it).
	ComponentRefusalArchived ComponentRefusal = "archived"
	// ComponentRefusalParentArchivedRestore — a restore whose parent is still
	// archived (restore the parent first).
	ComponentRefusalParentArchivedRestore ComponentRefusal = "parent-archived"
)

// ComponentWrite is the outcome of a tree write.
type ComponentWrite struct {
	Component Component
	Refusal   ComponentRefusal
	Reason    string
}

// OK reports whether the write was accepted.
func (w ComponentWrite) OK() bool { return w.Refusal == ComponentRefusalNone }

func refuseComponent(r ComponentRefusal, format string, args ...any) ComponentWrite {
	return ComponentWrite{Refusal: r, Reason: fmt.Sprintf(format, args...)}
}

// CreateComponent stores a named node, its primary workspace, and — with a
// ParentID — its link to a root, ALL in one transaction. There is no implicit
// project (CT1) and no half-created component. A child requires a live root
// parent that is itself a root and has no children (CT2); the depth triggers
// are the backstop when the store is bypassed. A nil inheritable field on a
// root takes the column default; on a child it inherits (NULL).
func (s *Store) CreateComponent(in NewComponent) (ComponentWrite, error) {
	if in.Name == "" {
		return refuseComponent(ComponentRefusalNameEmpty, "a component needs a name"), nil
	}
	if in.Slug == "" {
		return refuseComponent(ComponentRefusalSlugEmpty, "a component needs a slug"), nil
	}
	if in.Root == "" {
		return refuseComponent(ComponentRefusalRootTaken, "a new component needs a workspace root — a repo-less project is made by promote"), nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return ComponentWrite{}, fmt.Errorf("store: create component: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Parent rules (CT2), checked before the row so a bad parent is a named
	// sentence, never a trigger abort the caller cannot read.
	if in.ParentID != "" {
		p, err := s.componentForParent(tx, in.ParentID)
		if err != nil {
			return ComponentWrite{}, err
		}
		switch {
		case !p.exists:
			return refuseComponent(ComponentRefusalParentMissing, "the parent %s is not a component", in.ParentID), nil
		case p.isChild:
			return refuseComponent(ComponentRefusalParentIsChild, "a child's parent must be a root — %s is a child of %s", in.ParentID, p.parentID), nil
		case p.archived:
			return refuseComponent(ComponentRefusalParentArchived, "the parent %s is archived — restore it before adding children", in.ParentID), nil
		}
	}

	// The root path must not already be registered (the existing rule).
	var taken int
	if err := tx.QueryRow(`SELECT 1 FROM component_root WHERE component_root = ?`, in.Root).Scan(&taken); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return ComponentWrite{}, fmt.Errorf("store: check root %s: %w", in.Root, err)
		}
	}
	if taken != 0 {
		return refuseComponent(ComponentRefusalRootTaken, "workspace %s is already registered to a component", in.Root), nil
	}

	now := s.Now()
	c := Component{
		ID: "cmp-" + newID(), ParentID: in.ParentID, Name: in.Name, Slug: in.Slug, Root: in.Root,
		Remote: in.Remote, RemoteKey: in.RemoteKey, RepoKey: in.RepoKey, Owner: in.Owner,
		Icon: in.Icon, ShortDescription: in.ShortDescription, Description: in.Description,
		QueueAdvance: in.QueueAdvance, SizingMode: in.SizingMode, MaxParallelAgents: in.MaxParallelAgents,
		DefaultBranch: in.DefaultBranch, BranchPrefix: in.BranchPrefix,
		Tags: in.Tags, CreatedAt: now, UpdatedAt: now,
	}
	if err := insertComponent(tx, c); err != nil {
		return ComponentWrite{}, err
	}
	if _, err := tx.Exec(`INSERT INTO component_root (component_root, component_id, is_primary, added_at) VALUES (?,?,?,?)`,
		in.Root, c.ID, 1, now); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: map %s to its component: %w", in.Root, err)
	}
	if err := tx.Commit(); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: commit component %s: %w", c.ID, err)
	}
	return s.ComponentWriteFor(c.ID)
}

// UpdateComponent changes a node's metadata and settings. It cannot change
// parent_id (CT4: one way to do each thing) and cannot touch an archived node
// (only Restore does). Inherit clears an override to inherited on a child; on
// a root it is refused (a root has nothing to inherit from, CT6).
func (s *Store) UpdateComponent(in ComponentEdit) (ComponentWrite, error) {
	cur, err := s.ReadComponent(in.ID)
	if err != nil {
		return ComponentWrite{}, err
	}
	if cur.ArchivedAt != "" {
		return refuseComponent(ComponentRefusalArchived, "component %s is archived — restore it before updating", in.ID), nil
	}
	// Inherit on a root is refused; on a child it clears the named overrides.
	if len(in.Inherit) > 0 {
		if cur.ParentID == "" {
			return refuseComponent(ComponentRefusalArchived, "a root's settings have nothing to inherit from — inherit refuses on %s", in.ID), nil
		}
		for _, name := range in.Inherit {
			col, ok := inheritableColumn(name)
			if !ok {
				return refuseComponent(ComponentRefusalNameEmpty, "unknown inherit field %q (want queueAdvance, sizingMode, maxParallelAgents, defaultBranch, branchPrefix)", name), nil
			}
			if _, err := s.w.Exec(`UPDATE component SET `+col+`=NULL, updated_at=? WHERE id=?`, s.Now(), in.ID); err != nil {
				return ComponentWrite{}, fmt.Errorf("store: clear %s on %s: %w", name, in.ID, err)
			}
		}
	}
	if in.Name != "" {
		cur.Name = in.Name
	}
	if in.Slug != "" {
		cur.Slug = in.Slug
	}
	if in.Remote != "" {
		cur.Remote = in.Remote
	}
	if in.RemoteKey != "" {
		cur.RemoteKey = in.RemoteKey
	}
	if in.RepoKey != "" {
		cur.RepoKey = in.RepoKey
	}
	if in.Owner != "" {
		cur.Owner = in.Owner
	}
	if in.Icon != "" {
		cur.Icon = in.Icon
	}
	if in.ShortDescription != "" {
		cur.ShortDescription = in.ShortDescription
	}
	if in.Description != "" {
		cur.Description = in.Description
	}
	if in.QueueAdvance != nil {
		cur.QueueAdvance = in.QueueAdvance
	}
	if in.SizingMode != nil {
		cur.SizingMode = in.SizingMode
	}
	if in.MaxParallelAgents != nil {
		cur.MaxParallelAgents = in.MaxParallelAgents
	}
	if in.DefaultBranch != nil {
		cur.DefaultBranch = in.DefaultBranch
	}
	if in.BranchPrefix != nil {
		cur.BranchPrefix = in.BranchPrefix
	}
	if len(in.Tags) > 0 {
		cur.Tags = in.Tags
	}
	if err := s.writeComponent(cur); err != nil {
		return ComponentWrite{}, err
	}
	return s.ComponentWriteFor(in.ID)
}

// writeComponent updates a node's mutable columns in one write. It writes the
// inheritable columns only when a non-nil override is present (a nil is "do
// not change", not "inherit" — clearing is Inherit's job).
func (s *Store) writeComponent(c Component) error {
	now := s.Now()
	tags, err := json.Marshal(c.Tags)
	if err != nil {
		return fmt.Errorf("store: encode component tags: %w", err)
	}
	if _, err := s.w.Exec(`UPDATE component SET name=?, slug=?, remote=?, remote_key=?, repo_key=?, owner=?, icon=?,
		short_description=?, description=?, tags=?, updated_at=? WHERE id=?`,
		c.Name, c.Slug, c.Remote, c.RemoteKey, c.RepoKey, c.Owner, c.Icon,
		c.ShortDescription, c.Description, string(tags), now, c.ID); err != nil {
		return fmt.Errorf("store: update component %s: %w", c.ID, err)
	}
	// Inheritable overrides: a non-nil value is written; nil is left as-is.
	if c.QueueAdvance != nil {
		if _, err := s.w.Exec(`UPDATE component SET queue_advance=? WHERE id=?`, c.QueueAdvance, c.ID); err != nil {
			return fmt.Errorf("store: update component %s queue: %w", c.ID, err)
		}
	}
	if c.SizingMode != nil {
		if _, err := s.w.Exec(`UPDATE component SET sizing_mode=? WHERE id=?`, c.SizingMode, c.ID); err != nil {
			return fmt.Errorf("store: update component %s sizing: %w", c.ID, err)
		}
	}
	if c.MaxParallelAgents != nil {
		if _, err := s.w.Exec(`UPDATE component SET max_parallel_agents=? WHERE id=?`, c.MaxParallelAgents, c.ID); err != nil {
			return fmt.Errorf("store: update component %s cap: %w", c.ID, err)
		}
	}
	if c.DefaultBranch != nil {
		if _, err := s.w.Exec(`UPDATE component SET default_branch=? WHERE id=?`, c.DefaultBranch, c.ID); err != nil {
			return fmt.Errorf("store: update component %s branch: %w", c.ID, err)
		}
	}
	if c.BranchPrefix != nil {
		if _, err := s.w.Exec(`UPDATE component SET branch_prefix=? WHERE id=?`, c.BranchPrefix, c.ID); err != nil {
			return fmt.Errorf("store: update component %s prefix: %w", c.ID, err)
		}
	}
	return nil
}

// componentForParent is the CT2 parent probe: in one read it answers whether
// the named parent exists, is a child, and is archived. (A parent may hold any
// number of children — the CT2 depth rule forbids a *child* gaining children,
// not a root gaining more; the schema trigger enforces that, not this probe.)
func (s *Store) componentForParent(q querier, parentID string) (parentInfo, error) {
	var p parentInfo
	var archivedAt string
	err := q.QueryRow(`
		SELECT COALESCE(parent_id,''), COALESCE(archived_at,'')
		FROM component WHERE id = ?`, parentID).Scan(&p.parentID, &archivedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return parentInfo{}, nil
		}
		return parentInfo{}, fmt.Errorf("store: probe parent %s: %w", parentID, err)
	}
	p.exists = true
	p.isChild = p.parentID != ""
	p.archived = archivedAt != ""
	return p, nil
}

type parentInfo struct {
	exists   bool
	parentID string
	isChild  bool
	archived bool
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// insertComponent writes a new component row. The inheritable columns are
// written only for a child (a value = override, nil = inherit); a root omits
// them so the column defaults apply (the trigger refuses NULL on a root).
func insertComponent(tx *sql.Tx, c Component) error {
	cols := []string{"id", "name", "slug", "remote", "remote_key", "repo_key", "owner", "tags", "created_at", "updated_at", "icon", "short_description", "description", "parent_id"}
	vals := []any{c.ID, c.Name, c.Slug, c.Remote, c.RemoteKey, c.RepoKey, c.Owner, string(mustJSON(c.Tags)), c.CreatedAt, c.UpdatedAt, c.Icon, c.ShortDescription, c.Description, nullableNil(c.ParentID)}
	if c.ParentID != "" {
		cols = append(cols, "queue_advance", "sizing_mode", "max_parallel_agents", "default_branch", "branch_prefix")
		vals = append(vals, nullableStr(c.QueueAdvance), nullableStr(c.SizingMode), nullableInt(c.MaxParallelAgents), nullableStr(c.DefaultBranch), nullableStr(c.BranchPrefix))
	}
	_, err := tx.Exec(`INSERT INTO component (`+strings.Join(cols, ", ")+`) VALUES (`+placeholders(len(cols))+`)`, vals...)
	if err != nil {
		return fmt.Errorf("store: insert component %s: %w", c.ID, err)
	}
	return nil
}

// insertRoot writes a repo-less root (promote). Its inheritable columns take
// the defaults; a root has no primary root row (it is a pure project).
func insertRoot(tx *sql.Tx, id, name, slug, shortDescription, description, icon, now string) error {
	if _, err := tx.Exec(`INSERT INTO component (id, name, slug, icon, short_description, description, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, id, name, slug, icon, shortDescription, description, now, now); err != nil {
		return fmt.Errorf("store: insert root %s: %w", id, err)
	}
	return nil
}

// PromoteComponent makes a new repo-less root (a pure project) and sets `id`
// as its child, in one transaction (CT4). `id` keeps its id, roots, loops and
// rollups; the new root's rollup becomes `id`'s (CT7). Refusals: id is a
// child, id has children, id is archived.
func (s *Store) PromoteComponent(id string, in PromoteInput) (ComponentWrite, error) {
	c, err := s.ReadComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	if c.ParentID != "" {
		return refuseComponent(ComponentRefusalNotAChild, "promote acts on a root — %s is already a child of %s", id, c.ParentID), nil
	}
	if c.ArchivedAt != "" {
		return refuseComponent(ComponentRefusalArchived, "component %s is archived — restore it before promoting", id), nil
	}
	if c.ChildCount > 0 {
		return refuseComponent(ComponentRefusalHasChildren, "component %s already has children — it is a project, promote acts on a leaf", id), nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return ComponentWrite{}, fmt.Errorf("store: promote component: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	rootID := "cmp-" + newID()
	rootName := orDefault(in.Name, c.Name)
	rootSlug := in.Slug
	if rootSlug == "" {
		rootSlug = c.Slug + "-group"
	}
	if err := insertRoot(tx, rootID, rootName, rootSlug, in.ShortDescription, in.Description, in.Icon, now); err != nil {
		return ComponentWrite{}, err
	}
	if _, err := tx.Exec(`UPDATE component SET parent_id=?, updated_at=? WHERE id=?`, rootID, now, id); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: promote %s: %w", id, err)
	}
	// The rollup moves to the new root (CT7): the new root's row = the child's
	// former row (the child has its own root, so its row was the whole tree).
	if err := s.transferRollups(tx, id, "", rootID, now); err != nil {
		return ComponentWrite{}, err
	}
	if err := tx.Commit(); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: commit promote %s: %w", id, err)
	}
	return s.ComponentWriteFor(id)
}

// PromoteInput is the promote write: the new root's name and (optional) its
// metadata. Slug, when empty, is the child's slug with a -group suffix.
type PromoteInput struct {
	Name, Slug, ShortDescription, Description, Icon string
}

// AttachComponent makes a childless root a child of parentId (CT4). The node
// keeps its own values (they become overrides). Refusals: id has children, id
// is already a child, id has no primary root, or the parent rules (CT2).
func (s *Store) AttachComponent(id, parentID string) (ComponentWrite, error) {
	c, err := s.ReadComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	if c.ParentID != "" {
		return refuseComponent(ComponentRefusalNotAChild, "attach acts on a root — %s is already a child of %s", id, c.ParentID), nil
	}
	if c.ChildCount > 0 {
		return refuseComponent(ComponentRefusalHasChildren, "component %s has children — detach them before attaching", id), nil
	}
	if c.Empty {
		return refuseComponent(ComponentRefusalNoRoot, "component %s has no repository of its own — attach another component first, or archive it", id), nil
	}
	p, err := s.componentForParent(s.r, parentID)
	if err != nil {
		return ComponentWrite{}, err
	}
	switch {
	case !p.exists:
		return refuseComponent(ComponentRefusalParentMissing, "the parent %s is not a component", parentID), nil
	case p.isChild:
		return refuseComponent(ComponentRefusalParentIsChild, "a child's parent must be a root — %s is a child", parentID), nil
	case p.archived:
		return refuseComponent(ComponentRefusalParentArchived, "the parent %s is archived", parentID), nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return ComponentWrite{}, fmt.Errorf("store: attach component: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	if _, err := tx.Exec(`UPDATE component SET parent_id=?, updated_at=? WHERE id=?`, parentID, now, id); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: attach %s: %w", id, err)
	}
	// The node's rollup moves up to the new parent (CT7).
	if err := s.transferRollups(tx, id, "", parentID, now); err != nil {
		return ComponentWrite{}, err
	}
	if err := tx.Commit(); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: commit attach %s: %w", id, err)
	}
	return s.ComponentWriteFor(id)
}

// DetachComponent makes a child a root (CT4). Inherited settings are
// materialized first (CT6) so behaviour does not change. Refusals: id is a
// root, or the move would leave a repo-less parent with no children (CT3).
func (s *Store) DetachComponent(id string) (ComponentWrite, error) {
	c, err := s.ReadComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	if c.ParentID == "" {
		return refuseComponent(ComponentRefusalNotAChild, "detach acts on a child — %s is a root", id), nil
	}
	// Materialize the inherited settings (CT6) before the parent link is gone.
	if err := s.materializeInherited(id); err != nil {
		return ComponentWrite{}, err
	}
	// The parent must not be left a repo-less root with no children (CT3).
	empty, err := s.parentWouldBeEmpty(c.ParentID, id)
	if err != nil {
		return ComponentWrite{}, err
	}
	if empty {
		return refuseComponent(ComponentRefusalEmptyParent, "detaching %s would leave %s an empty project — attach another component first, or archive the project", id, c.ParentID), nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return ComponentWrite{}, fmt.Errorf("store: detach component: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := s.Now()
	if _, err := tx.Exec(`UPDATE component SET parent_id=NULL, updated_at=? WHERE id=?`, now, id); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: detach %s: %w", id, err)
	}
	// The node's rollup drops off the parent (CT7): the parent loses the node.
	if err := s.transferRollups(tx, id, c.ParentID, "", now); err != nil {
		return ComponentWrite{}, err
	}
	if err := tx.Commit(); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: commit detach %s: %w", id, err)
	}
	return s.ComponentWriteFor(id)
}

// ArchiveComponent sets archived_at (CT4). Hidden from default lists. Refusals:
// id has non-archived children, or the move would leave a repo-less parent
// empty (CT3).
func (s *Store) ArchiveComponent(id string) (ComponentWrite, error) {
	c, err := s.ReadComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	if c.ArchivedAt != "" {
		return refuseComponent(ComponentRefusalArchived, "component %s is already archived", id), nil
	}
	if c.ChildCount > 0 {
		return refuseComponent(ComponentRefusalHasChildren, "component %s has non-archived children — archive or detach them first", id), nil
	}
	if c.ParentID != "" {
		empty, err := s.parentWouldBeEmpty(c.ParentID, id)
		if err != nil {
			return ComponentWrite{}, err
		}
		if empty {
			return refuseComponent(ComponentRefusalEmptyParent, "archiving %s would leave %s an empty project — attach another component first", id, c.ParentID), nil
		}
	}
	now := s.Now()
	if _, err := s.w.Exec(`UPDATE component SET archived_at=?, updated_at=? WHERE id=?`, now, now, id); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: archive component %s: %w", id, err)
	}
	return s.ComponentWriteFor(id)
}

// RestoreComponent clears archived_at (CT4). Refusal: the parent is still
// archived (restore the parent first).
func (s *Store) RestoreComponent(id string) (ComponentWrite, error) {
	c, err := s.ReadComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	if c.ArchivedAt == "" {
		return refuseComponent(ComponentRefusalArchived, "component %s is not archived", id), nil
	}
	if c.ParentID != "" {
		p, err := s.componentForParent(s.r, c.ParentID)
		if err != nil {
			return ComponentWrite{}, err
		}
		if p.archived {
			return refuseComponent(ComponentRefusalParentArchivedRestore, "the parent %s is still archived — restore it first", c.ParentID), nil
		}
	}
	if _, err := s.w.Exec(`UPDATE component SET archived_at=NULL, updated_at=? WHERE id=?`, s.Now(), id); err != nil {
		return ComponentWrite{}, fmt.Errorf("store: restore component %s: %w", id, err)
	}
	return s.ComponentWriteFor(id)
}

// ReadComponent reads one node with its derived kind/childCount/empty and its
// primary root path. The inheritable columns are NOT read here (effective.go
// is the sole reader); the returned pointers stay nil until a caller resolves
// them through EffectiveComponent.
func (s *Store) ReadComponent(id string) (Component, error) {
	var c Component
	var hasRoot int
	var rawTags []byte
	err := s.r.QueryRow(`
		SELECT c.id, COALESCE(c.parent_id,''), COALESCE(c.archived_at,''),
			c.name, c.slug, COALESCE(cr.component_root,''),
			COALESCE(c.remote,''), COALESCE(c.remote_key,''), COALESCE(c.repo_key,''),
			COALESCE(c.owner,''), c.tags, c.created_at, c.updated_at,
			c.icon, c.short_description, c.description,
			(SELECT count(*) FROM component ch WHERE ch.parent_id = c.id AND ch.archived_at IS NULL),
			(SELECT count(*) FROM component_root cr2 WHERE cr2.component_id = c.id AND cr2.is_primary = 1 AND cr2.component_root != '')
		FROM component c
		LEFT JOIN component_root cr ON cr.component_id = c.id AND cr.is_primary = 1
		WHERE c.id = ?`, id).Scan(
		&c.ID, &c.ParentID, &c.ArchivedAt, &c.Name, &c.Slug, &c.Root,
		&c.Remote, &c.RemoteKey, &c.RepoKey, &c.Owner, &rawTags, &c.CreatedAt, &c.UpdatedAt,
		&c.Icon, &c.ShortDescription, &c.Description, &c.ChildCount, &hasRoot)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Component{}, fmt.Errorf("store: component %s: %w", id, ErrNotFound)
		}
		return Component{}, fmt.Errorf("store: read component %s: %w", id, err)
	}
	if err := json.Unmarshal(rawTags, &c.Tags); err != nil {
		return Component{}, fmt.Errorf("store: decode component %s tags: %w", id, err)
	}
	c.Kind = kindOf(c)
	c.Empty = c.ParentID == "" && hasRoot == 0 && c.ChildCount == 0
	return c, nil
}

// kindOf derives the view's kind (CT1): a project is a root with at least one
// non-archived child; everything else is a component.
func kindOf(c Component) string {
	if c.ParentID == "" && c.ChildCount > 0 {
		return "project"
	}
	return "component"
}

// ListComponents is the flat read (CT4). It never reads the inheritable
// columns (effective.go is the sole reader); a caller that needs them resolves
// through EffectiveComponent per row.
func (s *Store) ListComponents(f ComponentFilter) ([]Component, error) {
	var where []string
	var args []any
	if f.ParentID != "" {
		where = append(where, `parent_id = ?`)
		args = append(args, f.ParentID)
	}
	if f.RootsOnly {
		where = append(where, `parent_id IS NULL`)
	}
	if !f.IncludeArchived {
		where = append(where, `archived_at IS NULL`)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + join(where, " AND ")
	}
	rows, err := s.r.Query(`
		SELECT c.id, COALESCE(c.parent_id,''), COALESCE(c.archived_at,''),
			c.name, c.slug, COALESCE(cr.component_root,''),
			COALESCE(c.remote,''), COALESCE(c.remote_key,''), COALESCE(c.repo_key,''),
			COALESCE(c.owner,''), c.tags, c.created_at, c.updated_at,
			c.icon, c.short_description, c.description,
			(SELECT count(*) FROM component ch WHERE ch.parent_id = c.id AND ch.archived_at IS NULL),
			(SELECT count(*) FROM component_root cr2 WHERE cr2.component_id = c.id AND cr2.is_primary = 1 AND cr2.component_root != '')
		FROM component c
		LEFT JOIN component_root cr ON cr.component_id = c.id AND cr.is_primary = 1`+clause+`
		ORDER BY c.name, c.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list components: %w", err)
	}
	defer rows.Close()
	var out []Component
	for rows.Next() {
		var c Component
		var hasRoot int
		var rawTags []byte
		if err := rows.Scan(&c.ID, &c.ParentID, &c.ArchivedAt, &c.Name, &c.Slug, &c.Root,
			&c.Remote, &c.RemoteKey, &c.RepoKey, &c.Owner, &rawTags, &c.CreatedAt, &c.UpdatedAt,
			&c.Icon, &c.ShortDescription, &c.Description, &c.ChildCount, &hasRoot); err != nil {
			return nil, fmt.Errorf("store: scan component: %w", err)
		}
		if err := json.Unmarshal(rawTags, &c.Tags); err != nil {
			return nil, fmt.Errorf("store: decode component %s tags: %w", c.ID, err)
		}
		c.Kind = kindOf(c)
		c.Empty = c.ParentID == "" && hasRoot == 0 && c.ChildCount == 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// ComponentFilter is the list read (CT4).
type ComponentFilter struct {
	ParentID        string
	RootsOnly       bool
	IncludeArchived bool
}

// ComponentForRoot resolves a working directory to its component (S38 T9): the
// records service turns a step's ROOT — the only location the runtime carries
// — into the component identity the catalog stamps. A missing root is an empty
// result, never a fabricated component.
func (s *Store) ComponentForRoot(root string) (Component, error) {
	var id string
	err := s.r.QueryRow(`SELECT component_id FROM component_root WHERE component_root = ? AND is_primary = 1`, root).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Component{}, fmt.Errorf("store: no component registers workspace %s: %w", root, ErrNotFound)
		}
		return Component{}, fmt.Errorf("store: resolve component for %s: %w", root, err)
	}
	return s.ReadComponent(id)
}

// ListChildren returns a root's non-archived children (CT4).
func (s *Store) ListChildren(rootID string) ([]Component, error) {
	return s.ListComponents(ComponentFilter{ParentID: rootID})
}

// RootOf returns the node's own id (a root) or its parent's id (a child) —
// the root of the tree the node belongs to (CT1).
func (s *Store) RootOf(id string) (string, error) {
	var root string
	err := s.r.QueryRow(`SELECT COALESCE(parent_id, id) FROM component WHERE id = ?`, id).Scan(&root)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("store: root of %s: %w", id, ErrNotFound)
		}
		return "", fmt.Errorf("store: root of %s: %w", id, err)
	}
	return root, nil
}

// ComponentWriteFor re-reads a node into a ComponentWrite with its effective
// settings, for the tree operations that change the tree.
func (s *Store) ComponentWriteFor(id string) (ComponentWrite, error) {
	c, err := s.ReadComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	eff, err := s.EffectiveComponent(id)
	if err != nil {
		return ComponentWrite{}, err
	}
	c.QueueAdvance = eff.QueueAdvance.ValuePtr()
	c.SizingMode = eff.SizingMode.ValuePtr()
	c.MaxParallelAgents = eff.MaxParallelAgents.ValuePtr()
	c.DefaultBranch = eff.DefaultBranch.ValuePtr()
	c.BranchPrefix = eff.BranchPrefix.ValuePtr()
	return ComponentWrite{Component: c, Refusal: ComponentRefusalNone}, nil
}

// materializeInherited sets a child's inheritable columns to their resolved
// values (CT6 detach): the same effective value, now "own" rather than
// inherited. effective.go is the sole reader; this is the write.
func (s *Store) materializeInherited(id string) error {
	eff, err := s.EffectiveComponent(id)
	if err != nil {
		return err
	}
	if err := s.writeComponentOverride(id, eff.QueueAdvance.ValuePtr(), eff.SizingMode.ValuePtr(),
		eff.MaxParallelAgents.ValuePtr(), eff.DefaultBranch.ValuePtr(), eff.BranchPrefix.ValuePtr()); err != nil {
		return err
	}
	return nil
}

// writeComponentOverride writes the five inheritable columns of a node as
// explicit values (never NULL). Used by materializeInherited on detach.
func (s *Store) writeComponentOverride(id string, queueAdvance, sizingMode *string, maxParallel *int, defBranch, branchPrefix *string) error {
	if _, err := s.w.Exec(`UPDATE component SET queue_advance=?, sizing_mode=?, max_parallel_agents=?, default_branch=?, branch_prefix=?, updated_at=? WHERE id=?`,
		queueAdvance, sizingMode, maxParallel, defBranch, branchPrefix, s.Now(), id); err != nil {
		return fmt.Errorf("store: materialize %s: %w", id, err)
	}
	return nil
}

// parentWouldBeEmpty answers CT3's backstop: after excluding excludedChild, the
// parent is an empty project (no own root and no remaining non-archived
// children).
func (s *Store) parentWouldBeEmpty(parentID, excludedChild string) (bool, error) {
	var empty int
	err := s.r.QueryRow(`
		SELECT (SELECT count(*) FROM component_root cr WHERE cr.component_id = ? AND cr.is_primary = 1 AND cr.component_root != '')
			= 0 AND (SELECT count(*) FROM component ch WHERE ch.parent_id = ? AND ch.archived_at IS NULL AND ch.id <> ?) = 0
		FROM component WHERE id = ?`, parentID, parentID, excludedChild, parentID).Scan(&empty)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("store: probe parent %s: %w", parentID, err)
	}
	return empty == 1, nil
}

// mustJSON is a tiny marshal helper for the tags write.
func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// nullableNil maps the empty string to a SQL NULL.
func nullableNil(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// nullableStr maps a nil pointer to a SQL NULL.
func nullableStr(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullableInt maps a nil pointer to a SQL NULL.
func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// placeholders renders n "?" separated by commas for a dynamic INSERT.
func placeholders(n int) string {
	return strings.Repeat("?,", n-1) + "?"
}

// inheritableColumn maps the Go-facing name to the column (CT6's five). A
// component edit's Inherit list names these to clear to inherited.
func inheritableColumn(name string) (string, bool) {
	switch name {
	case "queueAdvance":
		return "queue_advance", true
	case "sizingMode":
		return "sizing_mode", true
	case "maxParallelAgents":
		return "max_parallel_agents", true
	case "defaultBranch":
		return "default_branch", true
	case "branchPrefix":
		return "branch_prefix", true
	}
	return "", false
}

// isRoot answers whether a component id names an existing ROOT (a node with no
// parent). CT10's epics and goals hang on a root; CT4's tree ops gate on it.
func (s *Store) isRoot(id string) bool {
	var one int
	err := s.r.QueryRow(`SELECT 1 FROM component WHERE id = ? AND parent_id IS NULL`, id).Scan(&one)
	return err == nil
}
