package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
)

// status.go — the two maintenance reads the CRUD doors need (the bench's
// "what exists at all" panel and the delete verb's target listing). They
// are the fourth state's DATA side: not-built is told here from rows, and
// never from a guess.

// Status is one root's accounting: is there a graph, how big is it.
// Built is CountGraphNodes>0 — the same test every query's not-built
// branch uses, so the panel and the explain handler cannot disagree about
// whether a root has been parsed.
type Status struct {
	Root  string `json:"root"`
	Built bool   `json:"built"`
	Files int    `json:"files"`
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
}

// Status reads one root's numbers. A root that was never built answers
// built:false with zeros — an honest empty, told apart from a failed read
// by the error being nil (DB19: the fourth state is data, error is for
// the disk).
func (s *Service) Status(ctx context.Context, root string) (*Status, error) {
	if root == "" {
		return nil, fmt.Errorf("graph status: refused — project_root is required, never defaulted (T5 §5)")
	}
	abs, err := normRoot(root)
	if err != nil {
		return nil, err
	}
	root = abs
	n, err := s.st.CountGraphNodes(root)
	if err != nil {
		return nil, err
	}
	st := &Status{Root: root, Built: n > 0, Nodes: n}
	files, err := s.st.GraphFilesIn(root)
	if err != nil {
		return nil, err
	}
	st.Files = len(files)
	// Edges ride their own count; one extra COUNT is affordable here and
	// the panel would otherwise show nodes with no relations and no way to
	// tell "no edges" from "edges not counted".
	edges, err := s.st.CountGraphEdges(root)
	if err != nil {
		return nil, err
	}
	st.Edges = edges
	return st, nil
}

// Roots lists every project root that has graph rows — the bench's
// root-picker data, so a person never types a path from memory (and the
// listing itself can never be mistaken for one project's answer: it names
// roots, nothing else).
func (s *Service) Roots(ctx context.Context) ([]string, error) {
	roots, err := s.st.GraphRoots()
	if err != nil {
		return nil, err
	}
	sort.Strings(roots)
	return roots, nil
}

// Delete clears one root's whole graph (the CRUD door's delete verb —
// index maintenance, the same trust tier as build: an operator tidying
// THEIR index, not loop content). Returns the node count removed; rows
// gone and edges gone in the store's one transaction, and a root with no
// rows is not an error — deleting what is not there deletes nothing,
// honestly, with a count of zero.
func (s *Service) Delete(ctx context.Context, root string) (int, error) {
	if root == "" {
		return 0, fmt.Errorf("graph delete: refused — project_root is required, never defaulted (T5 §5)")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return 0, fmt.Errorf("graph delete %s: %w", root, err)
	}
	return s.st.DeleteGraph(abs)
}

// RebuildFileAt is RebuildFile under the root-ABSOLUTE discipline Build
// and Delete use, so every write door keys rows identically and a file
// rebuilt under a symlinked spelling of its root cannot strand a second
// copy of rows (measured in T4: the hook and Build had been keying
// different spellings until both went through Abs).
//
// `path` may be project-RELATIVE — the CLI and the bench name one file
// the way a person reads it, `internal/order/process.go` (measured in
// the T8 walk: the first reparse refused its own relative path, because
// this door passed it straight to the absolute-path RebuildFile). A
// relative path is joined under the root, and relTo's `..` check still
// refuses a join that escapes; an absolute path is accepted as-is, which
// is what the daemon's write hook hands in.
func (s *Service) RebuildFileAt(ctx context.Context, root, absPath string) (*BuildReport, error) {
	if root == "" {
		return nil, fmt.Errorf("graph reparse: refused — project_root is required, never defaulted (T5 §5)")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("graph reparse %s: %w", root, err)
	}
	target := absPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(abs, absPath)
	}
	return s.RebuildFile(ctx, abs, target)
}

// StatsForRoot is the write hook's cheap gate: zero nodes means this root
// has never been built, and the hook fills the whole tree instead of a
// single file (the DEMO's "the graph builds itself"). It is a COUNT, not
// a walk — the hook pays it on every write, so it must stay one query.
func (s *Service) StatsForRoot(ctx context.Context, root string) (int, error) {
	abs, err := normRoot(root)
	if err != nil {
		return 0, err
	}
	return s.st.CountGraphNodes(abs)
}
