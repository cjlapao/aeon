package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// The five inheritable columns' column defaults (the migration's CHECK/DEFAULT).
// effective.go is the ONLY reader; a root with a nil value takes its default
// (unreachable in practice — the depth trigger refuses NULL on a root, so
// "default" is a defensive fallback), a child with a nil value inherits.
const (
	defQueueAdvance = "ask"
	defSizingMode   = "manual"
	defMaxParallel  = 1
)

// effective.go — the ONE reader of the five inheritable columns (CT6).
//
// A child's inheritable column is either NULL (inherit from the root) or a
// value (an override); a root's is never NULL (the migration's depth trigger
// refuses it). The resolver reads the node's five columns and — when the node
// is a child and a column is NULL — the root's five columns, and returns each
// value with its SOURCE: "own" (the node set it), "root" (inherited), or
// "default" (a defensive fallback, unreachable on a live tree because a root
// cannot be NULL). Nothing outside this file reads the five columns; the write
// doors (CreateComponent, UpdateComponent, the two setters) write them, and
// TestNoReaderBypassesEffective pins that no SELECT of them exists here.

// EffectiveField is one resolved string inheritable value with its source.
type EffectiveField struct {
	Value  string `json:"value"`
	Source string `json:"source"` // "own" | "root" | "default"
}

// EffectiveIntField is one resolved integer inheritable value with its source.
type EffectiveIntField struct {
	Value  int    `json:"value"`
	Source string `json:"source"` // "own" | "root" | "default"
}

// EffectiveComponent is the resolved settings of one node: the value a loop
// actually uses, and where it came from. A surface that shows a component's
// settings renders this, never the raw columns.
type EffectiveComponent struct {
	ComponentID       string            `json:"componentId"`
	QueueAdvance      EffectiveField    `json:"queueAdvance"`
	SizingMode        EffectiveField    `json:"sizingMode"`
	MaxParallelAgents EffectiveIntField `json:"maxParallelAgents"`
	DefaultBranch     EffectiveField    `json:"defaultBranch"`
	BranchPrefix      EffectiveField    `json:"branchPrefix"`
}

func (f EffectiveField) ValuePtr() *string { return &f.Value }

func (f EffectiveIntField) ValuePtr() *int { return &f.Value }

// EffectiveComponent resolves the five inheritable settings for one node. It is
// the sole reader of those columns: a child with a NULL column inherits the
// root's value; a value is its own override. A root's value is always its own
// (the trigger refuses NULL on a root), so "default" is a defensive fallback.
func (s *Store) EffectiveComponent(id string) (EffectiveComponent, error) {
	var out EffectiveComponent
	out.ComponentID = id
	var (
		ownQueue, ownSizing, ownBranch, ownPrefix, pQueue, pSizing, pBranch, pPrefix sql.NullString
		ownCap, pCap                                                                 sql.NullInt64
	)
	err := s.r.QueryRow(`
		SELECT c.queue_advance, c.sizing_mode, c.max_parallel_agents, c.default_branch, c.branch_prefix,
			p.queue_advance, p.sizing_mode, p.max_parallel_agents, p.default_branch, p.branch_prefix
		FROM component c
		LEFT JOIN component p ON p.id = c.parent_id
		WHERE c.id = ?`, id).Scan(&ownQueue, &ownSizing, &ownCap, &ownBranch, &ownPrefix,
		&pQueue, &pSizing, &pCap, &pBranch, &pPrefix)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EffectiveComponent{}, fmt.Errorf("store: effective of component %s: %w", id, ErrNotFound)
		}
		return EffectiveComponent{}, fmt.Errorf("store: effective of component %s: %w", id, err)
	}
	out.QueueAdvance = resolveStrField(ownQueue, pQueue, defQueueAdvance)
	out.SizingMode = resolveStrField(ownSizing, pSizing, defSizingMode)
	out.DefaultBranch = resolveStrField(ownBranch, pBranch, "")
	out.BranchPrefix = resolveStrField(ownPrefix, pPrefix, "")
	out.MaxParallelAgents = resolveIntField(ownCap, pCap, defMaxParallel)
	return out, nil
}

// EffectiveForRoot resolves the effective settings for the component that
// registers a working directory — the read the daemon's component view uses.
// A root with no registered component is ErrNotFound, not a fabricated set.
func (s *Store) EffectiveForRoot(root string) (EffectiveComponent, error) {
	var id string
	err := s.r.QueryRow(`SELECT component_id FROM component_root WHERE component_root = ? AND is_primary = 1`, root).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EffectiveComponent{}, fmt.Errorf("store: effective for %s: %w", root, ErrNotFound)
		}
		return EffectiveComponent{}, fmt.Errorf("store: effective for %s: %w", root, err)
	}
	return s.EffectiveComponent(id)
}

// resolveStrField picks the value and its source: own (the node set it), root
// (inherited from the parent), or default (a defensive fallback).
func resolveStrField(own, parent sql.NullString, def string) EffectiveField {
	switch {
	case own.Valid:
		return EffectiveField{Value: own.String, Source: "own"}
	case parent.Valid:
		return EffectiveField{Value: parent.String, Source: "root"}
	default:
		return EffectiveField{Value: def, Source: "default"}
	}
}

// resolveIntField is the integer sibling of resolveStrField.
func resolveIntField(own, parent sql.NullInt64, def int) EffectiveIntField {
	switch {
	case own.Valid:
		return EffectiveIntField{Value: int(own.Int64), Source: "own"}
	case parent.Valid:
		return EffectiveIntField{Value: int(parent.Int64), Source: "root"}
	default:
		return EffectiveIntField{Value: def, Source: "default"}
	}
}
